#!/usr/bin/env bash
# Build the platform-independent dashboard bundle used by the desktop
# "layered update": the client downloads only this small zip instead of the
# ~220 MB portable runtime when the runtime version is unchanged.
#
# Usage:
#   scripts/mammoth-dashboard-release.sh <version>
#
# <version> must sort *above* the installed runtime version, e.g. runtime 1.1.4
# ships dashboard 1.1.4.1 for a UI-only hotfix. The zip is written to
# dist/mammoth-dashboard-<version>.zip; upload it next to the other downloads
# and copy the printed JSON into mammoth-latest.json (top level, not per
# platform).
set -euo pipefail

version="${1:?usage: $0 <version>  e.g. 1.1.4.1}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dashboard_dir="$root/dashboard"
dist="$root/dist"

if ! command -v pnpm >/dev/null 2>&1; then
  echo "pnpm not found; install pnpm first" >&2
  exit 1
fi

cd "$dashboard_dir"
[ -d node_modules ] || pnpm install --frozen-lockfile
pnpm build

build_dir="$dashboard_dir/dist"
[ -f "$build_dir/index.html" ] || {
  echo "dashboard build missing index.html under $build_dir" >&2
  exit 1
}

mkdir -p "$dist"
zip_path="$dist/mammoth-dashboard-${version}.zip"
rm -f "$zip_path"

# The client strips the first path segment, so the zip must wrap the files in a
# single top-level folder named "dashboard".
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT
mkdir -p "$staging/dashboard"
cp -R "$build_dir/." "$staging/dashboard/"
(cd "$staging" && zip -qr "$zip_path" dashboard)

size="$(wc -c < "$zip_path" | tr -d ' ')"
if command -v sha256sum >/dev/null 2>&1; then
  sha="$(sha256sum "$zip_path" | awk '{print $1}')"
else
  sha="$(shasum -a 256 "$zip_path" | awk '{print $1}')"
fi

cat <<JSON
wrote $zip_path ($size bytes)

Add this to mammoth-latest.json at the top level:

  "dashboard": {
    "version": "$version",
    "url": "https://www.mmai.vip/downloads/mammoth-dashboard-${version}.zip",
    "sha256": "$sha",
    "size": $size
  }
JSON