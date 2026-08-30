from __future__ import annotations

import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

from docker_devtools._find import BINARY_ENV_VAR
from docker_devtools._find import BINARY_NAME
from docker_devtools._find import BinaryNotFoundError
from docker_devtools._find import find_binary

REPO_ROOT = Path(__file__).resolve().parent.parent


def _resolve_binary() -> str:
    """Choose which binary the whole session exercises.

    Order matters. In a source checkout the Go sources are the thing under
    test, so build them: an installed copy in .venv is whatever `uv sync` last
    cached, and uv keys that cache on the project version, which does not
    change between commits. Testing it would silently exercise stale code.

    Set DOCKER_DEVTOOLS_BINARY to override, which is how the fresh-clone
    check points the suite at the binary inside a built wheel.
    """
    override = os.environ.get(BINARY_ENV_VAR)
    if override:
        return override

    go = shutil.which("go")
    if go and (REPO_ROOT / "cmd" / "docker-devtools").is_dir():
        out = REPO_ROOT / "build" / BINARY_NAME
        out.parent.mkdir(exist_ok=True)
        subprocess.run(  # noqa: S603
            [go, "build", "-o", str(out), "./cmd/docker-devtools"],
            cwd=REPO_ROOT,
            check=True,
        )
        return str(out)

    try:
        return find_binary()
    except BinaryNotFoundError:
        pytest.skip(f"no {BINARY_NAME} available and no Go toolchain to build one")


@pytest.fixture(scope="session", autouse=True)
def binary() -> str:
    """Pin the binary for the session so every test agrees on which one runs."""
    path = _resolve_binary()
    os.environ[BINARY_ENV_VAR] = path
    return path


@pytest.fixture
def fixture_dir() -> Path:
    """Return the directory holding the Go test fixtures."""
    return REPO_ROOT / "testdata"


@pytest.fixture(scope="session")
def python() -> str:
    return sys.executable
