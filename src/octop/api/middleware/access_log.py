"""HTTP access log with client IP + authenticated user (no secrets)."""

from __future__ import annotations

import logging
import time
from collections.abc import Awaitable, Callable
from typing import Any

from fastapi import Request

from octop.api.common.client_ip import resolve_client_ip

_INSTALL_ATTR = "_octop_access_log_installed"
logger = logging.getLogger("octop.access")

# Paths where failures matter most for multi-user / first-boot debugging.
_HIGHLIGHT_PREFIXES = (
    "/api/auth/",
    "/api/setup/",
    "/api/health",
)


def _user_label(request: Request) -> str:
    user = getattr(request.state, "octop_user", None)
    if user is None:
        return "-"
    uid = getattr(user, "id", None)
    username = getattr(user, "username", None) or getattr(user, "name", None)
    if uid is not None and username:
        return f"{uid}/{username}"
    if uid is not None:
        return str(uid)
    if username:
        return str(username)
    return "-"


def install(app: Any) -> None:
    if getattr(app, _INSTALL_ATTR, False):
        return
    setattr(app, _INSTALL_ATTR, True)

    # uvicorn 只配置自己的 logger，root 默认停在 WARNING，
    # 不显式挂 handler 时 logger.info 会被静默丢掉。
    if not logger.handlers:
        handler = logging.StreamHandler()
        handler.setFormatter(logging.Formatter("%(asctime)s %(levelname)s %(name)s %(message)s"))
        logger.addHandler(handler)
        logger.propagate = False
    logger.setLevel(logging.INFO)

    @app.middleware("http")  # type: ignore[untyped-decorator]
    async def _access_log(
        request: Request,
        call_next: Callable[[Request], Awaitable[Any]],
    ) -> Any:
        started = time.perf_counter()
        status = 500
        try:
            response = await call_next(request)
            status = int(getattr(response, "status_code", 500) or 500)
            return response
        except Exception:
            status = 500
            raise
        finally:
            duration_ms = (time.perf_counter() - started) * 1000.0
            path = request.url.path
            client_ip = resolve_client_ip(request)
            # Prefer X-Real-IP when present (nginx) for the human-readable field;
            # resolve_client_ip already applies the trusted-proxy rules.
            real_ip = (request.headers.get("x-real-ip") or "").strip()
            forwarded = (request.headers.get("x-forwarded-for") or "").strip()
            ip_display = client_ip
            if real_ip and client_ip in {"unknown", ""}:
                ip_display = real_ip
            elif forwarded and client_ip in {"unknown", ""}:
                ip_display = forwarded.split(",")[0].strip() or client_ip

            line = (
                f"access method={request.method} path={path} status={status} "
                f"duration_ms={duration_ms:.1f} ip={ip_display} "
                f"user={_user_label(request)}"
            )
            highlight = path.startswith(_HIGHLIGHT_PREFIXES) or status >= 400
            if highlight and status >= 500:
                logger.error(line)
            elif highlight and status >= 400:
                logger.warning(line)
            else:
                logger.info(line)
