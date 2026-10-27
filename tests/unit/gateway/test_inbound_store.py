"""Unit tests for inbound attachment storage."""

from __future__ import annotations

import re
import tempfile

import pytest
from deepagents.backends.local_shell import LocalShellBackend
from octop_harness.backends.workspace import BackendWorkspace

from octop.infra.gateway.media.inbound_store import (
    build_timestamped_inbound_name,
    display_name_from_stored,
    inbound_extension,
    read_inbound_bytes,
    sanitize_inbound_filename,
    sanitize_inbound_rel_path,
    split_inbound_rel_path,
    write_inbound,
)


def _workspace(root: str) -> BackendWorkspace:
    backend = LocalShellBackend(root_dir=root, virtual_mode=False)
    return BackendWorkspace(backend, root)


def test_inbound_extension_from_filename() -> None:
    assert inbound_extension("report.pdf", "application/pdf") == ".pdf"


def test_sanitize_inbound_filename_keeps_cjk() -> None:
    assert sanitize_inbound_filename("我的报告.pdf") == "我的报告.pdf"
    assert sanitize_inbound_filename(r"C:\Users\me\地球介绍.pptx") == "地球介绍.pptx"
    assert sanitize_inbound_filename('say "hi".txt') == "say _hi_.txt"
    assert sanitize_inbound_filename("../evil.pdf") == "evil.pdf"


def test_timestamped_name_and_display_roundtrip() -> None:
    stored = build_timestamped_inbound_name("地球介绍.pptx", now=1783510288)
    assert stored == "1783510288_地球介绍.pptx"
    assert display_name_from_stored(stored) == "地球介绍.pptx"


@pytest.mark.asyncio
async def test_write_inbound_uses_timestamp_prefix_keeps_chinese() -> None:
    with tempfile.TemporaryDirectory() as ws_dir:
        workspace = _workspace(ws_dir)
        stored = await write_inbound(
            workspace,
            b"%PDF-1.4",
            filename="地球介绍.pdf",
            media_type="application/pdf",
        )
        assert stored.filename == "地球介绍.pdf"
        assert re.fullmatch(r"inbound/\d{10,}_地球介绍\.pdf", stored.path)
        on_disk = await read_inbound_bytes(workspace, stored.path)
        assert on_disk == b"%PDF-1.4"


@pytest.mark.asyncio
async def test_write_inbound_collision_adds_suffix() -> None:
    with tempfile.TemporaryDirectory() as ws_dir:
        workspace = _workspace(ws_dir)
        first = await write_inbound(
            workspace,
            b"a",
            filename="note.txt",
            media_type="text/plain",
        )
        twin_name = first.path.rsplit("/", 1)[-1]
        from octop.infra.gateway.media.inbound_store import _unique_inbound_path

        path = await _unique_inbound_path(workspace, twin_name)
        assert path.endswith("-2.txt")
        assert path.startswith("inbound/")


@pytest.mark.asyncio
async def test_write_inbound_no_meta_sidecar() -> None:
    with tempfile.TemporaryDirectory() as ws_dir:
        workspace = _workspace(ws_dir)
        stored = await write_inbound(
            workspace,
            b"%PDF-1.4",
            filename="report.pdf",
            media_type="application/pdf",
        )
        assert stored.path.endswith(".pdf")
        assert stored.path.startswith("inbound/")
        on_disk = await read_inbound_bytes(workspace, stored.path)
        assert on_disk == b"%PDF-1.4"


@pytest.mark.asyncio
async def test_write_inbound_flat_docx_path() -> None:
    with tempfile.TemporaryDirectory() as ws_dir:
        workspace = _workspace(ws_dir)
        stored = await write_inbound(
            workspace,
            b"PK-docx",
            filename="report.docx",
            media_type=("application/vnd.openxmlformats-officedocument.wordprocessingml.document"),
        )
        assert stored.path.endswith(".docx")
        assert stored.filename == "report.docx"


async def test_write_inbound_subdir_keeps_tree_and_dotfiles() -> None:
    with tempfile.TemporaryDirectory() as ws_dir:
        workspace = _workspace(ws_dir)
        stored = await write_inbound(
            workspace,
            b"build/",
            filename=".gitignore",
            media_type="text/plain",
            subdir="site/src",
        )
        assert stored.path == "inbound/site/src/.gitignore"
        assert stored.filename == ".gitignore"
        assert await read_inbound_bytes(workspace, stored.path) == b"build/"


async def test_write_inbound_subdir_rejects_traversal() -> None:
    with tempfile.TemporaryDirectory() as ws_dir:
        workspace = _workspace(ws_dir)
        stored = await write_inbound(
            workspace,
            b"secret",
            filename="id_rsa",
            media_type="text/plain",
            subdir="../../etc",
        )
        assert stored.path == "inbound/etc/id_rsa"


def test_sanitize_inbound_rel_path_drops_escapes() -> None:
    assert sanitize_inbound_rel_path("site/src/a.ts") == "site/src/a.ts"
    assert sanitize_inbound_rel_path(r"site\src\b.ts") == "site/src/b.ts"
    assert sanitize_inbound_rel_path("/abs/evil.ts") == "abs/evil.ts"
    assert sanitize_inbound_rel_path("../../etc/passwd") == "etc/passwd"
    assert sanitize_inbound_rel_path("C:/Windows/x.dll") == "Windows/x.dll"
    assert sanitize_inbound_rel_path("a//b/./c.py") == "a/b/c.py"
    assert sanitize_inbound_rel_path("") == ""
    assert sanitize_inbound_rel_path(None) == ""
    assert split_inbound_rel_path("site/src/a.ts") == ("site/src", "a.ts")
    assert split_inbound_rel_path("") == ("", "")
