"""Desktop boot diagnostics: receive, persist, rate limit, export."""

from __future__ import annotations

import json
from pathlib import Path

import httpx
import pytest
from tests.support.app import ensure_control_plane_bound, write_octop_config
from tests.support.auth import auth_header, bootstrap_admin

from octop.api.app import build_app
from octop.api.routers import client_log
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
        yield c, tmp_path
    await srv.stop()


@pytest.fixture(autouse=True)
def _reset_rate_limit():
    client_log._hits.clear()
    yield
    client_log._hits.clear()


async def test_boot_log_is_written_and_exported(client) -> None:
    http, home = client
    payload = {
        "shell_version": "1.1.1",
        "runtime_version": "1.0.9",
        "platform": "darwin/arm64",
        "phase": "port_occupied",
        "last_error": "port 8088 is used by another program",
        "probes": {"health": {"status": 503, "body": "ignored", "error": ""}},
        "ts": "2026-10-29T10:00:00Z",
    }
    res = await http.post("/api/client-log", json=payload)
    assert res.status_code == 200
    assert res.json() == {"ok": True}

    log = home / "logs" / "client-boot.jsonl"
    lines = [line for line in log.read_text(encoding="utf-8").splitlines() if line]
    assert len(lines) == 1
    record = json.loads(lines[0])
    assert record["phase"] == "port_occupied"
    assert record["uploaded"] is True
    # Probe bodies are never persisted: status codes only.
    assert record["probes"] == "health=503"
    assert "ignored" not in lines[0]

    # Export is an authenticated route: the JWT gate answers anonymous callers.
    assert (await http.get("/api/client-log/export")).status_code == 401
    exported = await http.get("/api/client-log/export", headers=await auth_header(http))
    assert exported.status_code == 200
    assert "port_occupied" in exported.text


async def test_export_includes_rotated_generation(client) -> None:
    http, home = client
    await http.post("/api/client-log", json={"phase": "ready"})
    (home / "logs" / "client-boot.1.jsonl").write_text('{"phase": "older"}\n', encoding="utf-8")

    response = await http.get("/api/client-log/export", headers=await auth_header(http))
    body = response.text
    assert body.index("older") < body.index('"ready"')
    assert response.headers["content-disposition"].endswith('octop-client-boot.jsonl"')


async def test_secrets_are_scrubbed(client) -> None:
    http, home = client
    await http.post(
        "/api/client-log",
        json={
            "phase": "health_timeout",
            "last_error": "Authorization: Bearer abc.def token=xyz",
            "probes": {},
        },
    )
    text = (home / "logs" / "client-boot.jsonl").read_text(encoding="utf-8")
    assert "abc.def" not in text
    assert "xyz" not in text
    assert "[redacted]" in text


async def test_rate_limit_per_ip(client) -> None:
    http, _home = client
    codes = [
        (await http.post("/api/client-log", json={"phase": "shell_start"})).status_code
        for _ in range(client_log._RATE_LIMIT + 1)
    ]
    assert codes[:-1] == [200] * client_log._RATE_LIMIT
    assert codes[-1] == 429


async def test_oversized_body_is_rejected(client) -> None:
    http, home = client
    padding = "x" * 70_000
    res = await http.post("/api/client-log", json={"phase": "boot", "last_error": padding})
    assert res.status_code == 413
    assert not (home / "logs" / "client-boot.jsonl").exists()


async def test_probe_status_secrets_are_scrubbed(client) -> None:
    http, home = client
    await http.post(
        "/api/client-log",
        json={
            "phase": "probe",
            "probes": {"oauth": {"status": {"access_token": "eyJhbGciOi.payload.sig"}}},
        },
    )
    text = (home / "logs" / "client-boot.jsonl").read_text(encoding="utf-8")
    assert "eyJhbGciOi" not in text
    assert "access_token': '" not in text


async def test_ts_is_validated_and_home_paths_scrubbed(client) -> None:
    http, home = client
    await http.post(
        "/api/client-log",
        json={
            "phase": "boot",
            "last_error": "cannot read /Users/alice/Library/Logs/x.log",
            "ts": "not-a-timestamp-with-a-secret",
        },
    )
    record = json.loads((home / "logs" / "client-boot.jsonl").read_text(encoding="utf-8"))
    assert "alice" not in record["last_error"]
    assert record["ts"].endswith("Z")
    assert "not-a-timestamp" not in record["ts"]


async def test_log_rotates_at_size_cap(client) -> None:
    http, home = client
    log = home / "logs" / "client-boot.jsonl"
    log.parent.mkdir(parents=True, exist_ok=True)
    log.write_text("{" + '"x": "' + "a" * (1024 * 1024) + '"}\n', encoding="utf-8")

    res = await http.post("/api/client-log", json={"phase": "after_rotate"})
    assert res.status_code == 200
    assert (home / "logs" / "client-boot.1.jsonl").exists()
    assert "after_rotate" in log.read_text(encoding="utf-8")
