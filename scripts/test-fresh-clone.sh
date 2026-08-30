#!/usr/bin/env bash
# Verify that a clone of this repo can be set up from scratch, by doing it
# inside a container. A temp directory on the development machine inherits an
# already-working environment and hides the interesting failures: a Go
# toolchain already on PATH, a warm module cache, a uv that happens to exist.
#
# Clones from the local repo rather than GitHub, so no credentials enter the
# container.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${IMAGE:-debian:bookworm-slim}"

command -v docker >/dev/null 2>&1 || {
  echo "docker is required" >&2
  exit 1
}

echo "==> testing a fresh clone in $IMAGE"

# MISE_AQUA_GITHUB_ATTESTATIONS: uv and golangci-lint now publish through
# GitHub's immutable releases, so their attestation carries the identity
# "https://dotcom.releases.github.com" while mise's aqua registry still expects
# the project's own release workflow. Every install of those two fails as a
# result, on any machine, which is unrelated to anything in this repo. Checksum
# verification still runs. Drop this once the registry catches up.
docker run --rm -i \
  -v "$REPO_ROOT:/src:ro" \
  -e MISE_YES=1 \
  -e MISE_AQUA_GITHUB_ATTESTATIONS=false \
  "$IMAGE" bash -s <<'INNER'
set -euo pipefail
step() { printf '\n\033[1m--- %s\033[0m\n' "$1"; }

step "base packages"
apt-get update -qq
apt-get install -y -qq git curl ca-certificates >/dev/null

step "install mise"
curl -fsSL https://mise.run | sh >/dev/null
export PATH="$HOME/.local/bin:$PATH"
eval "$(mise activate bash)"
mise --version

step "clone"
git clone -q /src /work
cd /work
mise trust >/dev/null
echo "files: $(git ls-files | wc -l)"

step "mise install (the documented first step)"
mise install 2>&1 | tail -3

step "go build and go test"
mise x -- go build ./...
mise x -- go test ./...

step "golangci-lint"
mise x -- golangci-lint run ./...

step "shell scripts parse and pass shellcheck/shfmt"
for f in scripts/*.sh; do bash -n "$f"; done && echo "syntax ok"
mise x -- shellcheck scripts/*.sh && echo "shellcheck ok"
mise x -- shfmt -i 2 -ci -d scripts/*.sh && echo "shfmt ok"

step "build a wheel for this platform"
mise x -- uv build --wheel --out-dir /tmp/dist
ls -1 /tmp/dist

step "install the wheel into a clean venv and use it three ways"
mise x -- uv venv --quiet /tmp/venv
mise x -- uv pip install --quiet --python /tmp/venv/bin/python /tmp/dist/*.whl
/tmp/venv/bin/docker-devtools version
/tmp/venv/bin/python -m docker_devtools image-refs ls testdata/imageref
/tmp/venv/bin/python -c "import docker_devtools as d; print(len(d.image_ls('testdata/imageref').refs), 'refs')"

step "the binary is a direct executable, not a python shim"
head -c 4 /tmp/venv/bin/docker-devtools | od -c | head -1

step "pytest against the installed wheel"
# Point the suite at the wheel's binary; left alone it would rebuild from
# source and never touch what we just packaged.
DOCKER_DEVTOOLS_BINARY=/tmp/venv/bin/docker-devtools \
  mise x -- uv run --quiet --with pytest --python /tmp/venv/bin/python python -m pytest tests -q

step "vale sync + prose lint on every tracked markdown file"
if ./scripts/prose-lint.sh; then echo "PROSE OK"; else echo "PROSE FAILED"; exit 1; fi

step "pre-commit hooks install and run"
mise x -- pre-commit install >/dev/null
mise x -- pre-commit install --hook-type commit-msg >/dev/null
mise x -- pre-commit run --all-files

printf '\n\033[1mALL CHECKS PASSED\033[0m\n'
INNER
