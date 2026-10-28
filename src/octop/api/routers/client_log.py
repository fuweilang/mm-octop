"""Desktop client boot diagnostics: receive, log, and export.

MAMMOTH-PATCH: 桌面启动报错回传.

The shell writes its own phases to ``~/.octop/logs/client-boot.jsonl`` and
uploads them here once the local service is healthy. The dashboard posts the
first-boot probes (health / setup / oauth) through the same route. Nothing
here needs the internet: the destination is the loopback API, so this keeps
working when the 中台 is unreachable.

Never log bodies, tokens, or cookies: only versions, phase, probe status and a
clipped error line. The access-log middleware records the real IP and the
authenticated user for this exact request.
"""

from __future__ import annotations

import json
import logging
import re
import time
from pathlib import Path
from typing import Any, cast

from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import PlainTextResponse
from pydantic import BaseModel, Field

from octop.api.common.client_ip import resolve_client_ip
from octop.infra.utils.paths import PathLayout

router = APIRouter()
logger = logging.getLogger("octop.access")

LOG_NAME = "client-boot.jsonl"
LOG_ROTATED = "client-boot.1.jsonl"
_MAX_ERROR = 400
_MAX_VERSION = 64
_MAX_PLATFORM = 32
_MAX_PHASE = 32
_MAX_PROBES = 12
_RATE_LIMIT = 12
_RATE_WINDOW = 60.0
_hits: dict[str, tuple[float, int]] = {}

_SECRET_RE = re.compile(
    r"(?i)\b(authorization|token|cookie|password|secret)"
    r"(\s*[:=]\s*)(\S+)"
)
# "Bearer abc.def" has no colon after the scheme; drop the credential itself.
_BEARER_RE = re.compile(r"(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+")


class ClientLogBody(BaseModel):
    shell_version: str = Field("", max_length=_MAX_VERSION)
    runtime_version: str = Field("", max_length=_MAX_VERSION)
    platform: str = Field("", max_length=_MAX_PLATFORM)
    phase: str = Field("", max_length=_MAX_PHASE)
    last_error: str = Field("", max_length=2000)
    probes: dict[str, Any] | list[Any] | None = None
    ts: str | None = Field(None, max_length=64)


def _clip(text: str, limit: int) -> str:
    cleaned = _BEARER_RE.sub("Bearer [redacted]", " ".join(str(text).split()))
    cleaned = _SECRET_RE.sub(r"\1\2[redacted]", cleaned)
    if len(cleaned) > limit:
        return cleaned[: limit - 1] + "…"
    return cleaned


def _probe_summary(probes: dict[str, Any] | list[Any] | None) -> str:
    """``health=200,oauth/status=503/user_manager`` — status only, never bodies."""
    if not probes:
        return ""
    items: list[tuple[str | int, Any]] = (
        list(probes.items()) if isinstance(probes, dict) else list(enumerate(probes))
    )
    parts: list[str] = []
    for name, value in items[:_MAX_PROBES]:
        if not isinstance(value, dict):
            continue
        status = value.get("status")
        err = _clip(str(value.get("error") or ""), 80)
        parts.append(f"{_clip(str(name), 32)}={status if status is not None else '-'}")
        if err:
            parts.append(f"{_clip(str(name), 32)}_err={err}")
    return _clip(",".join(parts), 400)


def _user_label(request: Request) -> str:
    user = getattr(request.state, "octop_user", None)
    if user is None:
        user = _try_authenticate(request)
    if user is None:
        return "-"
    uid = getattr(user, "id", None)
    name = getattr(user, "username", None) or getattr(user, "name", None)
    if uid is not None and name:
        return f"{uid}/{name}"
    return str(uid if uid is not None else name or "-")


def _try_authenticate(request: Request) -> Any:
    """Best-effort user for an exempt route: the dashboard sends its JWT."""
    from octop.api.deps import authenticate_request
    from octop.infra.errors import OctopError

    server = getattr(request.app.state, "octop_server", None)
    if server is None or getattr(server, "services", None) is None:
        return None
    try:
        return authenticate_request(request, server)
    except OctopError:
        return None
    except Exception:  # noqa: BLE001 - diagnostics must never fail the request
        return None


def _rate_limited(ip: str) -> bool:
    now = time.monotonic()
    start, count = _hits.get(ip, (now, 0))
    if now - start >= _RATE_WINDOW:
        start, count = now, 0
    count += 1
    if len(_hits) > 2048:
        _hits.clear()
    _hits[ip] = (start, count)
    return count > _RATE_LIMIT


def _logs_dir(request: Request) -> Path:
    """Server layout when available: ``OCTOP_HOME`` and ``--home`` agree there."""
    server = getattr(request.app.state, "octop_server", None)
    paths = getattr(server, "paths", None)
    if paths is not None:
        return cast(Path, paths.logs_dir)
    return PathLayout.from_env().logs_dir


@router.post("/client-log", summary="Receive desktop boot diagnostics")
async def upload_client_log(body: ClientLogBody, request: Request) -> dict[str, bool]:
    """Store one boot record and log its summary. Unauthenticated by design.

    The shell reports before anyone signs in; the per-IP rate limit and the
    strict field caps keep that safe.
    """
    ip = resolve_client_ip(request)
    if _rate_limited(ip):
        logger.info("client-boot rate limited ip=%s", ip)
        raise HTTPException(status_code=429, detail="too many requests")

    phase = _clip(body.phase, _MAX_PHASE) or "-"
    error = _clip(body.last_error, _MAX_ERROR)
    probes = _probe_summary(body.probes)
    shell = _clip(body.shell_version, _MAX_VERSION) or "-"
    runtime = _clip(body.runtime_version, _MAX_VERSION) or "-"

    record = {
        "ts": body.ts or time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "shell_version": shell,
        "runtime_version": runtime,
        "platform": _clip(body.platform, _MAX_PLATFORM),
        "phase": phase,
        "last_error": error,
        "probes": probes,
        # Delivered to the local service in this very request.
        "uploaded": True,
    }
    try:
        logs = _logs_dir(request)
        logs.mkdir(parents=True, exist_ok=True)
        with (logs / LOG_NAME).open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(record, ensure_ascii=False) + "\n")
    except OSError as exc:  # disk full / read-only home: logging still matters
        logger.warning("client-boot append failed: %s", exc)

    logger.info(
        "client-boot ip=%s user=%s shell=%s runtime=%s platform=%s phase=%s error=%s probes=%s",
        ip,
        _user_label(request),
        shell,
        runtime,
        record["platform"],
        phase,
        error or "-",
        probes or "-",
    )
    return {"ok": True}


@router.get(
    "/client-log/export",
    summary="Download the desktop boot log",
    response_class=PlainTextResponse,
)
async def export_client_log(request: Request) -> PlainTextResponse:
    """Rotated file first, then the live one, as one ``.jsonl`` download."""
    logs = _logs_dir(request)
    chunks: list[str] = []
    for name in (LOG_ROTATED, LOG_NAME):
        path = logs / name
        try:
            chunks.append(path.read_text(encoding="utf-8"))
        except OSError:
            continue
    body = "".join(chunks) or ""
    return PlainTextResponse(
        body,
        media_type="application/x-ndjson",
        headers={"Content-Disposition": 'attachment; filename="octop-client-boot.jsonl"'},
    )
