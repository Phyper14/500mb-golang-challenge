#!/usr/bin/env bash
# setup-branches.sh — configures this repository's branch layout per the
# 500MB Club Challenge submission rules (docs/pt-br/submitting.md):
#
#   - `main`: the full API implementation (this entire tree).
#   - `implementation`: ONLY the files needed to run the benchmark
#     (docker-compose.yml at the repo root, deploy/nginx/*, me.json).
#
# Usage (run once, after `git init` + first commit on main, and after
# setting the `origin` remote):
#
#   git init
#   git add .
#   git commit -m "feat: initial 500MB Club Go submission"
#   git branch -M main
#   git remote add origin git@github.com:<you>/<repo>.git
#   ./scripts/setup-branches.sh
#   git push -u origin main
#   git push -u origin implementation
#
# The script is idempotent: re-running it after new commits on main
# re-syncs `implementation`'s tracked file set without touching history
# you don't want touched (it uses a fresh orphan-free branch off main and
# prunes everything not on the allowlist).
set -euo pipefail

# Files/directories that MUST exist on the `implementation` branch, per
# the challenge rules. Adjust ALLOWLIST if you add new deploy assets.
ALLOWLIST=(
  "docker-compose.yml"
  "deploy"
  "me.json"
)

current_branch="$(git symbolic-ref --short HEAD)"
if [[ "$current_branch" != "main" ]]; then
  echo "error: run this from the 'main' branch (currently on '$current_branch')" >&2
  exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "error: working tree has uncommitted changes; commit or stash first" >&2
  exit 1
fi

echo "==> creating/updating 'implementation' from 'main'"
git branch -f implementation main
git checkout implementation

echo "==> pruning everything except the allowlist"
# List all tracked files, then remove anything not under an allowlisted
# path. `git rm` (not `rm`) keeps the removal staged and history-clean.
mapfile -t tracked < <(git ls-tree -r --name-only HEAD)
to_remove=()
for f in "${tracked[@]}"; do
  keep=false
  for allowed in "${ALLOWLIST[@]}"; do
    if [[ "$f" == "$allowed" || "$f" == "$allowed/"* ]]; then
      keep=true
      break
    fi
  done
  if [[ "$keep" == false ]]; then
    to_remove+=("$f")
  fi
done

if [[ ${#to_remove[@]} -gt 0 ]]; then
  git rm -q --cached -- "${to_remove[@]}"
  rm -f -- "${to_remove[@]}"
fi

if ! git diff --cached --quiet; then
  git commit -q -m "chore: sync implementation branch with main (compose + me.json only)"
  echo "==> committed pruned tree on 'implementation'"
else
  echo "==> 'implementation' already matches the allowlist, nothing to commit"
fi

echo "==> verifying required files are present"
for allowed in "${ALLOWLIST[@]}"; do
  if [[ ! -e "$allowed" ]]; then
    echo "warning: '$allowed' is in the allowlist but missing from the tree" >&2
  fi
done

git checkout main
echo
echo "Done. Review with: git diff main implementation --stat"
echo "Then push both branches:"
echo "  git push -u origin main"
echo "  git push -u origin implementation"
