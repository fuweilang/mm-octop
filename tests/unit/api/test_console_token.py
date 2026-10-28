"""Desktop dashboard gets its 中台 Bearer token from the local backend."""

from __future__ import annotations

import time
from pathlib import Path

import httpx
import jwt
import pytest
from tests.support.app import ensure_control_plane_bound, write_octop_config
from tests.support.auth import auth_header, bootstrap_admin, resolve_user_id

from octop.api.app import build_app
from octop.infra.server import OctopServer


@pytest.fixture
async def client(tmp_path: Path):
    write_octop_config(tmp_path, enable_dashboard=False)
    srv = OctopServer(home=tmp_path)
    await srv.start()
    await ensure_control_plane_bound(srv)
    app = build_app(srv)
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://testserver"
    ) as c:
        await bootstrap_admin(c, tmp_path)
        yield c, srv
    await srv.stop()


async def test_console_token_requires_login(client) -> None:
    http, _srv = client
    res = await http.get("/api/auth/mammoth/console-token")
    assert res.status_code == 401


async def test_console_token_returns_handoff(client) -> None:
    http, srv = client
    auth = await auth_header(http)
    user_id = await resolve_user_id(http, auth, "admin")
    srv.services.settings_repo.set(f"mammoth.console_handoff.{user_id}", "handoff-tok")

    res = await http.get("/api/auth/mammoth/console-token", headers=auth)
    assert res.status_code == 200
    assert res.json() == {"token": "handoff-tok"}


async def test_console_token_expired_is_401(client) -> None:
    http, srv = client
    auth = await auth_header(http)
    user_id = await resolve_user_id(http, auth, "admin")
    expired = jwt.encode({"exp": int(time.time()) - 60}, "secret", algorithm="HS256")
    srv.services.settings_repo.set(f"mammoth.console_handoff.{user_id}", expired)

    res = await http.get("/api/auth/mammoth/console-token", headers=auth)
    assert res.status_code == 401
