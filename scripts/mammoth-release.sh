#!/usr/bin/env bash
# One command for a desktop artifact release: build → upload → patch
# mammoth-latest.json → verify the public URLs.
#
# It covers the client-update half of a release (dashboard zip, portable zip,
# shell installer) because that is the half that is easy to get wrong: a
# missing "portable" entry silently disables in-app updates for a platform.
# The api/web images still ship through the compose flow in the baota-release
# skill; run that separately.
#
# Usage:
#   scripts/mammoth-release.sh --version 1.1.4.1 --dashboard
#   scripts/mammoth-release.sh --version 1.1.4 --artifacts dist --portable --shell
#   scripts/mammoth-release.sh --version 1.1.4 --artifacts ~/Downloads/octop-1.1.4 \
#       --platform darwin-arm64 --portable --dashboard
#
# Flags:
#   --version V        artifact version written into latest.json (required)
#   --artifacts DIR    where portable/shell artifacts live (default: dist)
#   --dashboard        build + publish the platform-independent SPA bundle
#   --portable         publish portable zip(s) from --artifacts
#   --shell            publish shell installer(s) from --artifacts
#   --platform P       restrict to one platform (repeatable);
#                      default: publish every platform found on disk
#   --latest FILE      local copy of mammoth-latest.json (default: dist/mammoth-latest.json)
#   --host HOST        server (default: root@124.223.179.250)
#   --remote-dir DIR   downloads dir on the server (default: /www/mammoth/downloads)
#   --public-base URL  where clients fetch artifacts from (default: https://www.mmai.vip/downloads)
#   --skip-build       reuse dist/mammoth-dashboard-<version>.zip instead of rebuilding
#   --dry-run          print every step, touch nothing (no build, no scp, no ssh)
set -euo pipefail

version=""
artifacts="dist"
want_dashboard=0
want_portable=0
want_shell=0
platform_filter=""
latest=""
host="root@124.223.179.250"
remote_dir="/www/mammoth/downloads"
public_base="https://www.mmai.vip/downloads"
skip_build=0
dry_run=0

die() { echo "error: $*" >&2; exit 1; }
run() {
  if [ "$dry_run" -eq 1 ]; then
    printf '  [dry-run] %s\n' "$*"
  else
    printf '  $ %s\n' "$*"
    "$@"
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) version="${2:?--version needs a value}"; shift 2 ;;
    --artifacts) artifacts="${2:?--artifacts needs a dir}"; shift 2 ;;
    --dashboard) want_dashboard=1; shift ;;
    --portable) want_portable=1; shift ;;
    --shell) want_shell=1; shift ;;
    --platform) platform_filter="$platform_filter${2:?--platform needs a value} "; shift 2 ;;
    --latest) latest="${2:?--latest needs a path}"; shift 2 ;;
    --host) host="${2:?--host needs a value}"; shift 2 ;;
    --remote-dir) remote_dir="${2:?--remote-dir needs a value}"; shift 2 ;;
    --public-base) public_base="${2:?--public-base needs a value}"; shift 2 ;;
    --skip-build) skip_build=1; shift ;;
    --dry-run) dry_run=1; shift ;;
    -h|--help) sed -n '2,33p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
done

[ -n "$version" ] || die "--version is required"
[ $((want_dashboard + want_portable + want_shell)) -gt 0 ] || \
  die "nothing to publish: pass --dashboard, --portable and/or --shell"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
[ -n "$latest" ] || latest="$root/dist/mammoth-latest.json"
base="${public_base%/}"

echo "==> preflight"
# 1.0 is the release branch. A release cut from a feature branch ships whatever
# happened to be on that branch, which is how half-finished work leaked before.
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "1.0" ] || die "on '$branch', release only from 1.0 (scripts/release-sync.sh moves commits there)"
[ -z "$(git status --porcelain)" ] || { git status --short >&2; die "working tree is dirty"; }
if [ "$dry_run" -eq 0 ]; then
  git fetch --quiet origin 1.0
  [ "$(git rev-parse HEAD)" = "$(git rev-parse origin/1.0)" ] || \
    die "HEAD != origin/1.0 — push or reset before releasing"
fi
echo "    branch 1.0 @ $(git rev-parse --short HEAD)"

# Version must match what the build actually stamps, or latest.json lies.
if [ -f pyproject.toml ]; then
  built="$(sed -n 's/^version = "\(.*\)"/\1/p' pyproject.toml | head -1)"
  if [ -n "$built" ] && [ "$built" != "$version" ]; then
    die "pyproject.toml says $built but --version is $version"
  fi
  echo "    pyproject.toml version: ${built:-unknown}"
fi

mkdir -p "$root/dist"
# Patch specs are collected in a temp file, not arrays: macOS ships bash 3.2,
# where empty-array expansion under `set -u` is an error.
spec="$(mktemp)"
trap 'rm -f "$spec"' EXIT

platform_wanted() {
  # No --platform at all means "publish every platform found on disk".
  [ -z "$platform_filter" ] && return 0
  case "$platform_filter" in
    *" $1 "*) return 0 ;;
    *) return 1 ;;
  esac
}

upload_artifact() {
  run scp "$1" "$host:$remote_dir/"
}

echo "==> artifacts"
if [ "$want_dashboard" -eq 1 ]; then
  zip="dist/mammoth-dashboard-${version}.zip"
  if [ "$skip_build" -eq 1 ] && [ -f "$zip" ]; then
    echo "    reuse $zip"
  else
    echo "    building the SPA bundle (pnpm build)…"
    run scripts/mammoth-dashboard-release.sh "$version"
  fi
  [ "$dry_run" -eq 1 ] || [ -f "$zip" ] || die "dashboard build produced no $zip"
  printf 'dashboard\t%s\n' "$zip" >>"$spec"
  upload_artifact "$zip"
fi

shopt -s nullglob
portable_found=0
for f in "$artifacts"/*portable*.zip; do
  name="$(basename "$f")"
  plat="$(printf '%s' "$name" | sed -n 's/.*portable-\([a-z0-9]*-[a-z0-9]*\)-[0-9].*/\1/p')"
  [ -n "$plat" ] || { echo "    skip $name (cannot read platform)"; continue; }
  platform_wanted "$plat" || { echo "    skip $name (platform filtered out)"; continue; }
  [ "$want_portable" -eq 1 ] || { echo "    skip $name (--portable not set)"; continue; }
  echo "    portable $plat ← $name"
  portable_found=1
  printf 'platform\t%s\t--portable\t%s\n' "$plat" "$f" >>"$spec"
  upload_artifact "$f"
done
for f in "$artifacts"/*.dmg "$artifacts"/*win*.exe; do
  [ -f "$f" ] || continue
  name="$(basename "$f")"
  case "$name" in
    *darwin-arm64*) plat="darwin-arm64" ;;
    *darwin-amd64*) plat="darwin-amd64" ;;
    *windows*|*win*) plat="windows-amd64" ;;
    *) echo "    skip $name (cannot read platform)"; continue ;;
  esac
  platform_wanted "$plat" || { echo "    skip $name (platform filtered out)"; continue; }
  [ "$want_shell" -eq 1 ] || { echo "    skip $name (--shell not set)"; continue; }
  echo "    shell $plat ← $name"
  printf 'platform\t%s\t--shell\t%s\n' "$plat" "$f" >>"$spec"
  upload_artifact "$f"
done
shopt -u nullglob

if [ "$want_portable" -eq 1 ] && [ "$portable_found" -eq 0 ]; then
  die "no portable zip matched in $artifacts — refusing to publish a half update (that platform would lose in-app update)"
fi

echo "==> patch latest.json"
if [ ! -f "$latest" ]; then
  # Seed from production so a partial release does not drop other platforms.
  echo "    local latest.json missing; seeding from $base/mammoth-latest.json"
  run curl -fsSL "$base/mammoth-latest.json" -o "$latest"
fi
if [ "$dry_run" -eq 0 ]; then
  cp "$latest" "$latest.bak"
  echo "    rollback copy: $latest.bak"
fi

patch_latest() {
  run python3 scripts/mammoth-latest.py --file "$latest" --version "$version" \
    --url-base "$base/" "$@"
}

while IFS="$(printf '\t')" read -r kind a b c; do
  case "$kind" in
    dashboard) patch_latest --dashboard "$a" ;;
    platform) patch_latest --platform "$a" "$b" "$c" ;;
    *) ;;
  esac
done <"$spec"

echo "==> upload latest.json"
run scp "$latest" "$host:$remote_dir/mammoth-latest.json"

echo "==> verify"
if [ "$dry_run" -eq 1 ]; then
  echo "  [dry-run] curl $base/mammoth-latest.json"
  echo "  [dry-run] HEAD every artifact listed in latest.json"
else
  ok=0
  for _ in 1 2 3 4 5; do
    body="$(curl -fsSL "$base/mammoth-latest.json" || true)"
    if printf '%s' "$body" | grep -q "\"version\": \"$version\""; then ok=1; break; fi
    sleep 2
  done
  [ "$ok" -eq 1 ] || die "latest.json does not advertise $version yet — check the upload"
  echo "    latest.json advertises $version"

  python3 - "$latest" <<'PY'
import json, sys, urllib.request

doc = json.load(open(sys.argv[1]))
items = []
if doc.get("dashboard"):
    items.append(("dashboard", doc["dashboard"]))
for plat, entry in (doc.get("platforms") or {}).items():
    for kind in ("portable", "shell"):
        if entry.get(kind):
            items.append((f"{plat}/{kind}", entry[kind]))

bad = 0
for label, art in items:
    try:
        req = urllib.request.Request(art["url"], method="HEAD")
        with urllib.request.urlopen(req, timeout=20) as resp:
            size = int(resp.headers.get("content-length") or 0)
    except Exception as exc:  # noqa: BLE001
        print(f"    FAIL {label}: {exc}")
        bad += 1
        continue
    if art.get("size") and size and size != art["size"]:
        print(f"    FAIL {label}: size {size} != latest.json {art['size']}")
        bad += 1
    else:
        print(f"    ok   {label} {art['version']} ({size} bytes)")
sys.exit(1 if bad else 0)
PY
fi

cat <<EOF

==> done: $version

rollback:
  scp "${latest}.bak" "$host:$remote_dir/mammoth-latest.json"
  # clients see the previous entries again; uploaded artifacts are harmless.

next: ship the API/web through the compose flow (baota-release skill), or stop
here for a dashboard-only hotfix.
EOF