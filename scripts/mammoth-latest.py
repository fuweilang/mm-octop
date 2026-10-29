#!/usr/bin/env python3
"""Patch mammoth-latest.json with freshly built desktop artifacts.

The client decides what to download from this file, so a missing entry silently
disables a whole update path (Windows had only ``shell`` for a while, which meant
Windows users could not update in-app at all). Editing it by hand is how those
gaps happen — this script hashes and sizes the files for you.

Examples
--------
Per-platform runtime (Windows has no portable entry by default)::

    scripts/mammoth-latest.py --file mammoth-latest.json \\
        --platform windows-amd64 --version 1.1.4 \\
        --portable dist/Octop-portable-windows-amd64-1.1.4.zip \\
        --shell dist/mammoth-agent-win.exe

Dashboard-only hotfix (top level, applies to every platform)::

    scripts/mammoth-latest.py --file mammoth-latest.json \\
        --version 1.1.4.1 --dashboard dist/mammoth-dashboard-1.1.4.1.zip

Python-only patch for one runtime version (top level)::

    scripts/mammoth-latest.py --file mammoth-latest.json \\
        --version 2.0.0.1 --code dist/mammoth-code-2.0.0.1.zip --code-runtime 2.0.0

URLs default to ``--url-base`` + the file name; pass ``--portable-url`` /
``--shell-url`` / ``--dashboard-url`` for CDN paths.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from datetime import UTC, datetime
from pathlib import Path

DEFAULT_URL_BASE = "https://www.mmai.vip/downloads/"


def artifact(path: Path, version: str, url: str) -> dict[str, object]:
    data = path.read_bytes()
    return {
        "version": version,
        "url": url,
        "sha256": hashlib.sha256(data).hexdigest(),
        "size": len(data),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--file", type=Path, required=True, help="mammoth-latest.json to patch")
    parser.add_argument("--version", required=True, help="version for the artifacts")
    parser.add_argument("--platform", help="darwin-arm64 / darwin-amd64 / windows-amd64")
    parser.add_argument("--portable", type=Path, help="green portable zip")
    parser.add_argument("--shell", type=Path, help="dmg or exe installer")
    parser.add_argument("--dashboard", type=Path, help="platform independent SPA zip")
    parser.add_argument("--code", type=Path, help="python-only patch zip")
    parser.add_argument("--code-runtime", help="runtime version a code patch requires")
    parser.add_argument("--url-base", default=DEFAULT_URL_BASE)
    parser.add_argument("--portable-url")
    parser.add_argument("--shell-url")
    parser.add_argument("--dashboard-url")
    parser.add_argument("--code-url")
    args = parser.parse_args(argv)

    if not (args.portable or args.shell or args.dashboard or args.code):
        parser.error("nothing to publish: pass --portable, --shell, --dashboard and/or --code")
    if args.code and not args.code_runtime:
        parser.error("--code requires --code-runtime")
    if (args.portable or args.shell) and not args.platform:
        parser.error("--platform is required with --portable / --shell")

    doc: dict[str, object]
    if args.file.exists():
        doc = json.loads(args.file.read_text(encoding="utf-8"))
    else:
        doc = {"schema_version": 1, "platforms": {}}

    doc.setdefault("schema_version", 1)
    platforms = doc.setdefault("platforms", {})
    assert isinstance(platforms, dict)

    if args.platform:
        entry = platforms.setdefault(args.platform, {})
        assert isinstance(entry, dict)
        if args.portable:
            entry["portable"] = artifact(
                args.portable,
                args.version,
                args.portable_url or args.url_base + args.portable.name,
            )
        if args.shell:
            entry["shell"] = artifact(
                args.shell,
                args.version,
                args.shell_url or args.url_base + args.shell.name,
            )

    if args.dashboard:
        doc["dashboard"] = artifact(
            args.dashboard,
            args.version,
            args.dashboard_url or args.url_base + args.dashboard.name,
        )

    if args.code:
        entry = artifact(
            args.code,
            args.version,
            args.code_url or args.url_base + args.code.name,
        )
        entry["runtime"] = args.code_runtime
        doc["code"] = entry

    doc["updated_at"] = datetime.now(UTC).astimezone().isoformat(timespec="seconds")
    args.file.write_text(json.dumps(doc, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

    print(json.dumps(doc, indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
