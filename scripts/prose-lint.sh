#!/usr/bin/env bash
# Lint prose with vale-ai-tells. Used by pre-commit (changed files) and by
# `mise run prose` (everything).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Tools here are pinned with mise. `mise activate` installs a shell prompt
# hook, which never fires in a non-interactive shell, so a script run from
# cron, CI or a container would not find them on PATH. Import the environment
# explicitly instead.
if command -v mise >/dev/null 2>&1; then
  eval "$(mise env -s bash 2>/dev/null || true)"
fi

# vale sync writes the style packs; they are gitignored, so a fresh clone
# needs them fetched before the first run.
[ -d .vale-styles/ai-tells ] || vale sync >/dev/null 2>&1

# mapfile needs bash 4+, and macOS ships 3.2, so collect with a plain loop.
status=0
lint() {
  [ -f "$1" ] || return 0
  vale --output=line "$1" || status=1
}

if [ $# -gt 0 ]; then
  for f in "$@"; do lint "$f"; done
else
  while IFS= read -r f; do lint "$f"; done < <(git ls-files --cached --others --exclude-standard '*.md')
fi
exit $status
