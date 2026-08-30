#!/usr/bin/env bash
# Build one wheel per platform, plus the sdist, into ./dist.
#
# Each wheel holds the same Python package and a Go binary cross-compiled for
# that platform, so the hook runs once per target. CI does the same thing
# across a matrix; this is the local equivalent.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Tools here are pinned with mise. `mise activate` installs a shell prompt
# hook, which never fires in a non-interactive shell, so a script run from
# CI or a container would not find them on PATH. Import the environment
# explicitly instead.
if command -v mise >/dev/null 2>&1; then
  eval "$(mise env -s bash 2>/dev/null || true)"
fi

TARGETS=(
  linux-amd64
  linux-arm64
  linux-amd64-musl
  linux-arm64-musl
  darwin-amd64
  darwin-arm64
  windows-amd64
  windows-arm64
)

for target in "${TARGETS[@]}"; do
  echo "==> $target"
  DDT_TARGET="$target" uv build --wheel --out-dir dist
done

echo "==> sdist"
uv build --sdist --out-dir dist

echo
ls -1 dist
