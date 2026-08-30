#!/usr/bin/env bash
# Generate shell completions, markdown docs and a man page from the CLI's own
# usage spec.
#
# kong ships no completion of its own, so the binary emits a KDL usage spec
# (kong-usage walks kong's model) and the usage CLI turns that into scripts.
# One definition, five shells, plus docs.
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

BIN="build/docker-devtools"
go build -o "$BIN" ./cmd/docker-devtools

SPEC="build/docker-devtools.usage.kdl"
"./$BIN" --usage-spec >"$SPEC"

OUT="build/completions"
mkdir -p "$OUT"
for shell in bash zsh fish powershell nu; do
  usage generate completion "$shell" docker-devtools -f "$SPEC" >"$OUT/docker-devtools.$shell"
  echo "  $OUT/docker-devtools.$shell"
done

usage generate markdown -f "$SPEC" --out-file build/cli.md 2>/dev/null ||
  usage generate md -f "$SPEC" --out-file build/cli.md
echo "  build/cli.md"

echo
echo "To try zsh completion now:"
echo "  fpath+=\"\$PWD/$OUT\"; autoload -Uz compinit && compinit"
echo
echo "Note: the generated scripts call back to the usage CLI at runtime, so"
echo "users need 'usage' on PATH (mise use usage) for completion to work."
