"""tests/unit/api/test_health_version.py"""

from __future__ import annotations

from types import SimpleNamespace

from octop import __version__
from octop.api.routers.health import health


def _server(**overrides: object) -> SimpleNamespace:
    fields: dict[str, object] = {
        "database_bound": True,
        "user_manager": SimpleNamespace(count=lambda: 2),
        "app_runtime": None,
        "_started_at": 1_700_000_000.0,
    }
    fields.update(overrides)
    return SimpleNamespace(**fields)


async def test_health_reports_runtime_version() -> None:
    """The desktop shell compares this with the portable tree on disk before
    reusing a running instance, so the field has to stay in the payload."""
    payload = await health(server=_server())

    assert payload["ok"] is True
    assert payload["version"] == __version__
    assert payload["users_loaded"] == 2
    assert payload["agents_running"] == 0


async def test_health_still_works_without_user_manager() -> None:
    payload = await health(server=_server(user_manager=None))

    assert payload["version"] == __version__
    assert payload["users_loaded"] == 0
