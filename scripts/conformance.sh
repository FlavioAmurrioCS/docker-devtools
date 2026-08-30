#!/usr/bin/env bash
# Compare `docker-devtools context ls` against what Docker really sends.
#
# For each fixture the script builds `FROM scratch` + `COPY . /` with the
# tar exporter, so the resulting tarball is exactly the build context Docker
# assembled, then diffs its members against our listing. This is the only
# check that proves conformance rather than asserting it.
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

if ! docker version >/dev/null 2>&1; then
  echo "conformance: docker is not available, skipping" >&2
  exit 0
fi

BIN="$(mktemp -d)/docker-devtools"
go build -o "$BIN" ./cmd/docker-devtools

status=0
for dir in testdata/dctx/*/; do
  name="$(basename "$dir")"
  ctx="${dir}context"
  [ -d "$ctx" ] || continue

  # A fixture may pin a non-default Dockerfile name, which also selects
  # "<name>.dockerignore" over ".dockerignore".
  dockerfile=""
  if [ -f "${dir}case.json" ]; then
    dockerfile="$(sed -n 's/.*"dockerfile"[[:space:]]*:[[:space:]]*"\(.*\)".*/\1/p' "${dir}case.json")"
  fi

  tarball="$(mktemp -d)/ctx.tar"
  if [ -n "$dockerfile" ]; then
    docker build --no-cache -f "$ctx/$dockerfile" \
      --output "type=tar,dest=$tarball" "$ctx" >/dev/null 2>&1
    ours="$("$BIN" context ls -f "$dockerfile" "$ctx")"
  else
    docker build --no-cache \
      --output "type=tar,dest=$tarball" "$ctx" >/dev/null 2>&1
    ours="$("$BIN" context ls "$ctx")"
  fi

  # The tar exporter marks directories with a trailing slash; our listing
  # names them plainly. Normalise and sort both sides.
  theirs="$(tar -tf "$tarball" | sed 's:/$::' | LC_ALL=C sort)"
  ours="$(printf '%s\n' "$ours" | LC_ALL=C sort)"

  if [ "$theirs" = "$ours" ]; then
    printf 'ok    %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"
    diff <(printf '%s\n' "$theirs") <(printf '%s\n' "$ours") | sed 's/^/      /' || true
    status=1
  fi
done
exit $status
