"""Run the bundled binary via ``python -m docker_devtools``.

The binary is normally on PATH already, since the wheel installs it into the
environment's scripts directory. This module exists for the cases where it is
not, such as an environment whose scripts directory is not on PATH.
"""

from __future__ import annotations

import os
import subprocess
import sys

from docker_devtools._find import find_binary


def _run() -> None:
    binary = find_binary()
    if sys.platform == "win32":
        # Windows has no exec that replaces the process cleanly, and a
        # KeyboardInterrupt here would print a traceback over the child's own
        # output.
        try:
            completed = subprocess.run([binary, *sys.argv[1:]], check=False)  # noqa: S603
        except KeyboardInterrupt:
            sys.exit(130)
        sys.exit(completed.returncode)
    else:
        os.execv(binary, [binary, *sys.argv[1:]])  # noqa: S606


if __name__ == "__main__":
    _run()
