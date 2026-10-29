"""Cap the anonymous client-log body before FastAPI parses it.

``POST /api/client-log`` is JWT-exempt (the desktop shell reports before anyone
signs in), so the request body is attacker-controlled. FastAPI would parse the
whole JSON first; this guard rejects anything above 64 KB, including chunked
requests that omit ``Content-Length``.

Pure ASGI (not ``BaseHTTPMiddleware``) because the replayed body has to reach the
downstream app on the same ``receive`` channel.
"""

from __future__ import annotations

from typing import Any

from fastapi import FastAPI
from starlette.responses import JSONResponse

GUARDED_PATH = "/api/client-log"
MAX_BODY_BYTES = 64 * 1024


class ClientLogGuard:
    def __init__(self, app: Any) -> None:
        self.app = app

    async def __call__(self, scope: dict[str, Any], receive: Any, send: Any) -> None:
        if (
            scope["type"] != "http"
            or scope.get("method") != "POST"
            or str(scope.get("path", "")).rstrip("/") != GUARDED_PATH
        ):
            await self.app(scope, receive, send)
            return

        body = bytearray()
        while True:
            message = await receive()
            if message["type"] == "http.disconnect":
                return
            if message["type"] != "http.request":
                continue
            body.extend(message.get("body", b""))
            if len(body) > MAX_BODY_BYTES:
                response = JSONResponse({"detail": "payload too large"}, status_code=413)
                await response(scope, receive, send)
                return
            if not message.get("more_body", False):
                break

        payload = bytes(body)
        delivered = False

        async def replay() -> dict[str, Any]:
            nonlocal delivered
            if delivered:
                return {"type": "http.disconnect"}
            delivered = True
            return {"type": "http.request", "body": payload, "more_body": False}

        await self.app(scope, replay, send)


def install(app: FastAPI) -> None:
    app.add_middleware(ClientLogGuard)
