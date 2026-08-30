"""Locate the bundled docker-devtools binary.

The search order is adapted from Astral's ``uv`` package (``python/uv/_find_uv.py``,
MIT OR Apache-2.0). The binary ships in the wheel's ``.data/scripts`` directory,
which installs into the environment's scripts directory rather than next to this
module, and that directory moves depending on how the wheel was installed --
``pip install --target``, ``--prefix``, the user scheme, or ``uv run --with``.
A naive ``os.path.dirname(__file__)`` lookup gets all of those wrong.
"""

from __future__ import annotations

import os
import sys
import sysconfig
from fnmatch import fnmatch

BINARY_NAME = "docker-devtools"

#: Set this to an absolute path to use a specific binary instead of searching.
#: Useful when running against a locally built binary, and as an escape hatch
#: for install layouts the search below does not anticipate.
BINARY_ENV_VAR = "DOCKER_DEVTOOLS_BINARY"


class BinaryNotFoundError(FileNotFoundError):
    """The bundled binary could not be located."""


def find_binary() -> str:
    """Return the path to the bundled docker-devtools binary.

    Raises:
        BinaryNotFoundError: if no candidate directory holds the binary.
    """
    override = os.environ.get(BINARY_ENV_VAR)
    if override:
        if not os.path.isfile(override):
            msg = f"{BINARY_ENV_VAR} points at {override!r}, which is not a file"
            raise BinaryNotFoundError(msg)
        return override

    exe = BINARY_NAME + (sysconfig.get_config_var("EXE") or "")

    targets = [
        # The scripts directory for the current interpreter.
        sysconfig.get_path("scripts"),
        # The scripts directory for the base prefix, for virtualenvs.
        sysconfig.get_path("scripts", vars={"base": sys.base_prefix}),
        # Above the package root, e.g. `pip install --prefix` or `uv run --with`.
        (
            _join(
                _matching_parents(_module_path(), "Lib/site-packages/docker_devtools"),
                "Scripts",
            )
            if sys.platform == "win32"
            else _join(
                _matching_parents(_module_path(), "lib/python*/site-packages/docker_devtools"),
                "bin",
            )
        ),
        # Adjacent to the package root, e.g. `pip install --target`.
        _join(_matching_parents(_module_path(), "docker_devtools"), "bin"),
        _matching_parents(_module_path(), "docker_devtools"),
        # The user scheme's scripts directory, e.g. `~/.local/bin`.
        sysconfig.get_path("scripts", scheme=_user_scheme()),
    ]

    seen: list[str] = []
    for target in targets:
        if not target or target in seen:
            continue
        seen.append(target)
        path = os.path.join(target, exe)
        if os.path.isfile(path):
            return path

    locations = "\n".join(f" - {target}" for target in seen)
    msg = f"Could not find the {BINARY_NAME} binary in any of:\n{locations}\n"
    raise BinaryNotFoundError(msg)


def _module_path() -> str:
    return os.path.dirname(__file__)


def _matching_parents(path: str | None, match: str) -> str | None:
    """Trim ``match`` off the end of ``path`` and return what is left.

    ``match`` uses ``/`` separators and may contain ``*`` wildcards; ``path``
    uses the platform separator. Components compare case-insensitively.
    """
    if not path:
        return None
    parts = path.split(os.sep)
    match_parts = match.split("/")
    if len(parts) < len(match_parts):
        return None
    if not all(
        fnmatch(part, match_part)
        for part, match_part in zip(reversed(parts), reversed(match_parts), strict=False)
    ):
        return None
    return os.sep.join(parts[: -len(match_parts)])


def _join(path: str | None, *parts: str) -> str | None:
    if not path:
        return None
    return os.path.join(path, *parts)


def _user_scheme() -> str:
    # uv's original branches on sys.version_info here because it supports 3.8.
    # This package requires 3.10, where get_preferred_scheme always exists.
    return sysconfig.get_preferred_scheme("user")
