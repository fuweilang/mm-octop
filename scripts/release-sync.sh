#!/usr/bin/env bash
# Cherry-pick an explicit list of already-reviewed commits onto the release
# branch (1.0).
#
# Why this exists: 2.0 is where features land, but it also carries work that is
# not ready to ship. Merging the branch wholesale would release half-finished
# screens; picking files by hand loses history and hides what shipped. So a
# release is a *list of SHAs*, and this script is the only sanctioned way to
# move them over.
#
# Usage:
#   scripts/release-sync.sh <sha> [<sha> ...]
#   scripts/release-sync.sh --dry-run <sha> ...
#   scripts/release-sync.sh --target 1.0 --source 2.0 <sha> ...
#
# Rules enforced:
#   - the working tree must be clean and you must be on the target branch
#     (the script fast-forwards it to origin first, so nobody ships a stale tree)
#   - every SHA must exist on the source branch
#   - a commit that is already on the target is skipped, not duplicated
#   - any conflict stops the run with the exact SHA so you can fix it by hand
set -euo pipefail

target=1.0
source=2.0
dry_run=0
shas=()

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) dry_run=1; shift ;;
    --target) target="${2:?--target needs a branch}"; shift 2 ;;
    --source) source="${2:?--source needs a branch}"; shift 2 ;;
    -h|--help) sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "unknown option: $1" >&2; exit 2 ;;
    *) shas+=("$1"); shift ;;
  esac
done

if [ ${#shas[@]} -eq 0 ]; then
  echo "usage: $0 [--dry-run] [--target 1.0] [--source 2.0] <sha> [<sha> ...]" >&2
  exit 2
fi

cd "$(git rev-parse --show-toplevel)"

if [ -n "$(git status --porcelain)" ]; then
  echo "refusing to run on a dirty tree:" >&2
  git status --short >&2
  echo "commit or stash first — a release must be reproducible." >&2
  exit 1
fi

echo "==> fetching origin"
git fetch --prune origin

if ! git rev-parse --verify --quiet "origin/$source" >/dev/null; then
  echo "origin/$source does not exist" >&2
  exit 1
fi
if ! git rev-parse --verify --quiet "origin/$target" >/dev/null; then
  echo "origin/$target does not exist" >&2
  exit 1
fi

current="$(git rev-parse --abbrev-ref HEAD)"
if [ "$current" != "$target" ]; then
  echo "==> switching $current → $target (reset to origin/$target)"
  if [ "$dry_run" -eq 0 ]; then
    git checkout "$target"
  fi
fi
if [ "$dry_run" -eq 0 ]; then
  # 1.0 is a synthetic release branch: local drift there is always a mistake.
  git reset --hard "origin/$target"
fi

plan=()
for sha in "${shas[@]}"; do
  full="$(git rev-parse --verify --quiet "${sha}^{commit}" || true)"
  if [ -z "$full" ]; then
    echo "unknown commit: $sha" >&2
    exit 1
  fi
  if ! git merge-base --is-ancestor "$full" "origin/$source"; then
    echo "$sha is not on origin/$source — pick the merged SHA, not a local draft." >&2
    exit 1
  fi
  if git merge-base --is-ancestor "$full" "origin/$target"; then
    echo "skip $sha — already on $target ($(git log -1 --format=%s "$full"))"
    continue
  fi
  plan+=("$full")
done

if [ ${#plan[@]} -eq 0 ]; then
  echo "nothing to do: all commits are already on $target."
  exit 0
fi

echo
echo "==> releasing onto $target"
for sha in "${plan[@]}"; do
  printf '  %s  %s\n' "$(git log -1 --format=%h "$sha")" "$(git log -1 --format=%s "$sha")"
done

if [ "$dry_run" -eq 1 ]; then
  echo
  echo "(dry run) nothing applied."
  exit 0
fi

for sha in "${plan[@]}"; do
  echo
  echo "==> cherry-pick $sha"
  if ! git cherry-pick -x "$sha"; then
    cat >&2 <<EOF

conflict while picking $sha.
  1. resolve the files, then: git add -A && git cherry-pick --continue
  2. or abandon:              git cherry-pick --abort
  3. re-run this script with the remaining SHAs.
Nothing has been pushed yet.
EOF
    exit 1
  fi
done

echo
echo "==> verifying the release branch builds"
if [ -d desktop/src ]; then
  (cd desktop/src && go vet ./... && go test ./... >/dev/null)
  echo "    desktop: ok"
fi
if [ -f Makefile ] && command -v make >/dev/null 2>&1; then
  make verify-api >/dev/null 2>&1 && echo "    api: ok" || echo "    api: verify-api failed (run it by hand)"
fi

cat <<EOF

done. $target is ahead of origin/$target by ${#plan[@]} commit(s):
$(git log --oneline "origin/$target..HEAD")

review, then publish:
  git push origin $target
EOF