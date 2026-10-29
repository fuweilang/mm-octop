#!/usr/bin/env bash
# Build a code-only patch for the desktop client.
#
# A normal release ships the whole 220 MB runtime even when only Python code
# changed. A code patch carries just ``packages/octop`` (no SPA, no interpreter,
# no dependencies) and is applied by the client only when the installed runtime
# version matches ``--runtime`` exactly.
#
# Usage:
#   scripts/mammoth-code-release.sh 2.0.0.1 --runtime 2.0.0
#
# The zip is written to dist/mammoth-code-<version>.zip and the printed JSON goes
# into mammoth-latest.json at the top level as "code".
#
# Rules:
#   * version must sort above the runtime version (2.0.0.1 > 2.0.0).
#   * bump the runtime version instead when dependencies changed.
#   * publish the dashboard bundle too when the SPA changed; the code patch
#     deliberately leaves packages/octop/dashboard untouched.
set -euo pipefail

version="${1:?usage: $0 <version> --runtime <runtime-version>}"
shift
runtime=""
while [ $# -gt 0 ]; do
  case "$1" in
    --runtime) runtime="${2:?--runtime needs a value}"; shift 2 ;;
    -h|--help) sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "error: unknown option: $1" >&2; exit 1 ;;
  esac
done
[ -n "$runtime" ] || { echo "error: --runtime is required (e.g. 2.0.0)" >&2; exit 1; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
src="$root/src/octop"
dist="$root/dist"

[ -d "$src" ] || { echo "error: $src missing" >&2; exit 1; }
[ -f "$src/__main__.py" ] || { echo "error: $src does not look like the octop package" >&2; exit 1; }

# The client strips the first path segment, so wrap the files in "octop/".
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT
mkdir -p "$staging/octop"
cp -R "$src/." "$staging/octop/"
# The SPA has its own channel and the runtime ships the interpreter/packages.
rm -rf "$staging/octop/dashboard"
find "$staging/octop" -name '__pycache__' -type d -prune -exec rm -rf {} +
find "$staging/octop" -name '*.pyc' -delete

mkdir -p "$dist"
zip_path="$dist/mammoth-code-${version}.zip"
rm -f "$zip_path"
(cd "$staging" && zip -qr "$zip_path" octop)

size="$(wc -c < "$zip_path" | tr -d ' ')"
if command -v sha256sum >/dev/null 2>&1; then
  sha="$(sha256sum "$zip_path" | awk '{print $1}')"
else
  sha="$(shasum -a 256 "$zip_path" | awk '{print $1}')"
fi

cat <<JSON
wrote $zip_path ($size bytes)

Add this to mammoth-latest.json at the top level:

  "code": {
    "version": "$version",
    "runtime": "$runtime",
    "url": "https://www.mmai.vip/downloads/mammoth-code-${version}.zip",
    "sha256": "$sha",
    "size": $size
  }
JSON