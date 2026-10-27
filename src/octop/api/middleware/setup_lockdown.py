"""Lock down all non-/setup endpoints while no users exist.

Returns ``503 {"setup_required": true}`` so the SPA can hard-redirect
to ``/setup``.
"""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from typing import Any

from fastapi import Request
from fastapi.responses import JSONResponse

from octop.infra.auth.sso.service import desktop_platform_origin

_OPEN_PREFIXES = (
    "/api/setup/",
    "/api/health/",
)
# Fresh desktop has no local user. These two calls are how it signs in.
_DESKTOP_OPEN = (
    "/api/auth/oauth/status",
    "/api/auth/mammoth/desktop-login",
    "/api/auth/mammoth/desktop-register",
)


def install(app: Any, server: Any) -> None:
    @app.middleware("http")  # type: ignore[untyped-decorator]
    async def _lockdown(
        request: Request,
        call_next: Callable[[Request], Awaitable[Any]],
    ) -> Any:
        path = request.url.path
        if not path.startswith("/api/"):
            return await call_next(request)
        cfg = server.services.config if server.services else getattr(server, "config", None)
        open_exact = ["/api/health"]
        if cfg and cfg.enable_api_docs:
            open_exact.extend(("/api/docs", "/api/openapi.json"))
        if path in open_exact or any(path.startswith(p) for p in _OPEN_PREFIXES):
            return await call_next(request)
        origin = desktop_platform_origin()
        if origin and path in _DESKTOP_OPEN:
            return await call_next(request)
        if server.user_manager is None or server.user_manager.count() == 0:
            content: dict[str, object] = {"setup_required": True}
            if origin:
                content["platform_origin"] = origin
            return JSONResponse(status_code=503, content=content)
        return await call_next(request)
