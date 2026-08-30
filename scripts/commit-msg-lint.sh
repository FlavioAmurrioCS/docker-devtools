#!/usr/bin/env bash
# Lint a commit message with the ai-tells-commits style. Invoked by the
# pre-commit commit-msg hook, which passes the message file path.
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

[ $# -ge 1 ] || exit 0

[ -d .vale-commits/styles/ai-tells-commits ] ||
  vale --config=.vale-commits/vale.ini sync >/dev/null 2>&1

# vale keys rules off the extension, so give it a .md copy of the message
cp "$1" .vale-commits/msg.md
vale --config=.vale-commits/vale.ini --output=line .vale-commits/msg.md
