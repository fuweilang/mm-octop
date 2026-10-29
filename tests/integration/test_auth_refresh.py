"""tests/integration/test_auth_refresh.py — desktop session renewal."""

from __future__ import annotations

import pytest

from octop.infra.auth.tokens import sign_token
from tests.support.auth import bootstrap_admin


@pytest.fixture
async def client(app_client):
    yield app_client


async def _admin_token(c, srv, home) -> tuple[str, int]:
    await bootstrap_admin(c, home, username="a", password="TestPass12")
    r = await c.post("/api/auth/login", json={"username": "a", "password": "TestPass12"})
    assert r.status_code == 200
    token = r.json()["access_token"]
    me = await c.get("/api/auth/me", headers={"Authorization": f"Bearer {token}"})
    return token, me.json()["id"]


async def test_refresh_accepts_recently_expired_token(client):
    c, srv, home = client
    _, user_id = await _admin_token(c, srv, home)
    secret = srv.services.secret_repo.get("jwt")

    expired = sign_token(secret, sub=user_id, uname="a", role="admin", ttl_seconds=-60)
    r = await c.post("/api/auth/refresh", headers={"Authorization": f"Bearer {expired}"})
    assert r.status_code == 200
    fresh = r.json()["access_token"]
    assert fresh != expired

    me = await c.get("/api/auth/me", headers={"Authorization": f"Bearer {fresh}"})
    assert me.status_code == 200 and me.json()["username"] == "a"


async def test_refresh_keeps_a_valid_token_working(client):
    c, srv, home = client
    token, _ = await _admin_token(c, srv, home)
    r = await c.post("/api/auth/refresh", headers={"Authorization": f"Bearer {token}"})
    assert r.status_code == 200
    me = await c.get(
        "/api/auth/me", headers={"Authorization": f"Bearer {r.json()['access_token']}"}
    )
    assert me.status_code == 200


async def test_refresh_rejects_token_expired_beyond_grace(client):
    c, srv, home = client
    _, user_id = await _admin_token(c, srv, home)
    secret = srv.services.secret_repo.get("jwt")
    grace = srv.services.config.token_refresh_grace_seconds

    too_old = sign_token(secret, sub=user_id, uname="a", role="admin", ttl_seconds=-(grace + 60))
    r = await c.post("/api/auth/refresh", headers={"Authorization": f"Bearer {too_old}"})
    assert r.status_code == 401


async def test_refresh_requires_a_token(client):
    c, srv, home = client
    await _admin_token(c, srv, home)
    r = await c.post("/api/auth/refresh")
    assert r.status_code == 401


async def test_refresh_rejects_garbage(client):
    c, srv, home = client
    await _admin_token(c, srv, home)
    r = await c.post("/api/auth/refresh", headers={"Authorization": "Bearer not-a-jwt"})
    assert r.status_code == 401
