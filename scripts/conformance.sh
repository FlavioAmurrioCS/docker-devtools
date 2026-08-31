#!/usr/bin/env bash
# Compare `docker-devtools build-context ls` against what Docker really sends.
#
# For each fixture the script builds it with the tar exporter, so the resulting
# tarball is exactly what Docker read from the context, then diffs its members
# against our listing. This is the only check that proves conformance rather
# than asserting it.
#
# Most fixtures use `FROM scratch` + `COPY . /`, which copies the context whole.
# A fixture that copies selectively must mirror its destinations onto its source
# paths (`COPY sub /sub`), because the tar holds image paths and the listing
# holds context paths, and only then are the two comparable.
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

  # A fixture may pin a non-default Dockerfile, which also selects
  # "<path>.dockerignore" over ".dockerignore". case.json records it relative to
  # the fixture directory rather than to the context, so that the identical -f
  # string can go to docker and to us: -f is resolved from the working
  # directory, and a fixture Dockerfile may sit outside its own context.
  dockerfile=""
  if [ -f "${dir}case.json" ]; then
    dockerfile="$(sed -n 's/.*"dockerfile"[[:space:]]*:[[:space:]]*"\(.*\)".*/\1/p' "${dir}case.json")"
  fi

  # A fixture may also pin a --target, which changes which stages are reached
  # and so which COPY sources are read from the context.
  target=""
  if [ -f "${dir}case.json" ]; then
    target="$(sed -n 's/.*"target"[[:space:]]*:[[:space:]]*"\(.*\)".*/\1/p' "${dir}case.json")"
  fi

  set -- --no-cache
  ours_args=""
  if [ -n "$dockerfile" ]; then
    set -- "$@" -f "${dir}${dockerfile}"
    ours_args="$ours_args -f ${dir}${dockerfile}"
  fi
  if [ -n "$target" ]; then
    set -- "$@" --target "$target"
    ours_args="$ours_args --target $target"
  fi

  tarball="$(mktemp -d)/ctx.tar"
  docker build "$@" --output "type=tar,dest=$tarball" "$ctx" >/dev/null 2>&1
  # shellcheck disable=SC2086 # the flags are built above and are never quoted paths
  ours="$("$BIN" build-context ls $ours_args "$ctx" 2>/dev/null)"

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
