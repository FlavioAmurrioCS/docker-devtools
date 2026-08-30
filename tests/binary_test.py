"""Checks on the shipped binary itself rather than the Python wrapper."""

from __future__ import annotations

import json
import subprocess

from docker_devtools import find_binary


def test_help_exits_cleanly() -> None:
    result = subprocess.run(  # noqa: S603
        [find_binary(), "--help"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    assert "docker-devtools" in result.stdout


def test_version_is_reported() -> None:
    result = subprocess.run(  # noqa: S603
        [find_binary(), "version"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip()


def test_unknown_command_exits_two() -> None:
    result = subprocess.run(  # noqa: S603
        [find_binary(), "nope"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 2


def test_docker_plugin_metadata_is_valid() -> None:
    """Docker runs this subcommand to decide whether to load the plugin."""
    result = subprocess.run(  # noqa: S603
        [find_binary(), "docker-cli-plugin-metadata"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    meta = json.loads(result.stdout)
    assert meta["SchemaVersion"] == "0.1.0"
    assert meta["Vendor"]
