"""Route-level test: folder uploads keep their relative path in ``inbound/``."""

from __future__ import annotations

import tempfile
from types import SimpleNamespace

import pytest
from deepagents.backends.local_shell import LocalShellBackend
from octop_harness.backends.workspace import BackendWorkspace

from octop.api.routers import uploads as uploads_router


class _FakeUpload:
    def __init__(self, filename: str, data: bytes, content_type: str = "text/plain") -> None:
        self.filename = filename
        self.content_type = content_type
        self.size = len(data)
        self._data = data

    async def read(self, size: int = -1) -> bytes:
        if size is None or size < 0:
            data, self._data = self._data, b""
            return data
        data, self._data = self._data[:size], self._data[size:]
        return data


def _workspace(root: str) -> BackendWorkspace:
    return BackendWorkspace(LocalShellBackend(root_dir=root, virtual_mode=False), root)


@pytest.mark.asyncio
async def test_upload_route_keeps_folder_tree(monkeypatch: pytest.MonkeyPatch) -> None:
    with tempfile.TemporaryDirectory() as ws_dir:
        workspace = _workspace(ws_dir)

        async def fake_require_running_workspace(*args: object, **kwargs: object) -> BackendWorkspace:
            return workspace

        monkeypatch.setattr(
            uploads_router, "require_running_workspace", fake_require_running_workspace
        )

        payload = await uploads_router.upload_attachment(
            agent_id="a1",
            file=_FakeUpload("a.ts", b"export const a = 1;"),
            relative_path="site/src/a.ts",
            user=SimpleNamespace(id=7),
            server=SimpleNamespace(services=None),
        )

        assert payload["path"] == "inbound/site/src/a.ts"
        assert payload["filename"] == "a.ts"
        on_disk = await workspace.adownload_bytes("inbound/site/src/a.ts")
        assert on_disk == b"export const a = 1;"


@pytest.mark.asyncio
async def test_upload_route_flat_upload_still_timestamped(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    with tempfile.TemporaryDirectory() as ws_dir:
        workspace = _workspace(ws_dir)

        async def fake_require_running_workspace(*args: object, **kwargs: object) -> BackendWorkspace:
            return workspace

        monkeypatch.setattr(
            uploads_router, "require_running_workspace", fake_require_running_workspace
        )

        payload = await uploads_router.upload_attachment(
            agent_id="a1",
            file=_FakeUpload("notes.txt", b"hi"),
            relative_path=None,
            user=SimpleNamespace(id=7),
            server=SimpleNamespace(services=None),
        )

        assert payload["path"].startswith("inbound/")
        assert payload["path"].endswith("_notes.txt")
