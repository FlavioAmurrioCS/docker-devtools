# pyright: reportUnknownMemberType=false
# hatchling's BuildHookInterface leaves ProjectMetadata's type parameter
# unbound upstream, so every access through self is "partially unknown". The
# other five checkers are fine with it.
"""Hatchling build hook that compiles the Go binary into the wheel.

There is no maturin for Go, so this reproduces what maturin's ``bindings =
"bin"`` does for Rust: cross-compile one static binary and drop it into the
wheel's ``.data/scripts`` directory, alongside the hand-written Python package.
The binary therefore installs onto PATH as a direct executable, with no
``console_scripts`` shim and no interpreter startup on every invocation.

One wheel is produced per target. ``DDT_TARGET`` selects it; without it the
hook builds for the host, so a plain ``uv build`` works locally.
"""

from __future__ import annotations

import os
import platform
import shutil
import subprocess
import sys
import tempfile
from typing import TYPE_CHECKING
from typing import Any

from hatchling.builders.hooks.plugin.interface import BuildHookInterface

if TYPE_CHECKING:
    from typing_extensions import override
else:
    # The build environment holds only what build-system.requires names, so
    # typing_extensions is not importable here. Type checkers still see the
    # real decorator through the branch above.
    def override(method: Any) -> Any:  # noqa: ANN401
        return method


BINARY_NAME = "docker-devtools"

# target -> (GOOS, GOARCH, wheel platform tag)
#
# CGO is off, so the Linux binaries are fully static and the glibc and musl
# wheels ship identical bytes under different tags. Both are published so that
# pip on Alpine resolves a wheel at all.
TARGETS: dict[str, tuple[str, str, str]] = {
    "linux-amd64": ("linux", "amd64", "manylinux_2_17_x86_64"),
    "linux-arm64": ("linux", "arm64", "manylinux_2_17_aarch64"),
    "linux-amd64-musl": ("linux", "amd64", "musllinux_1_2_x86_64"),
    "linux-arm64-musl": ("linux", "arm64", "musllinux_1_2_aarch64"),
    "darwin-amd64": ("darwin", "amd64", "macosx_10_9_x86_64"),
    "darwin-arm64": ("darwin", "arm64", "macosx_11_0_arm64"),
    "windows-amd64": ("windows", "amd64", "win_amd64"),
    "windows-arm64": ("windows", "arm64", "win_arm64"),
}

_HOST_OS = {"Linux": "linux", "Darwin": "darwin", "Windows": "windows"}
_HOST_ARCH = {
    "x86_64": "amd64",
    "amd64": "amd64",
    "AMD64": "amd64",
    "aarch64": "arm64",
    "arm64": "arm64",
}


def host_target() -> str:
    """Return the TARGETS key for the machine running the build."""
    goos = _HOST_OS.get(platform.system())
    goarch = _HOST_ARCH.get(platform.machine())
    if goos is None or goarch is None:
        msg = (
            f"cannot infer a build target for {platform.system()}/{platform.machine()}; "
            f"set DDT_TARGET to one of: {', '.join(sorted(TARGETS))}"
        )
        raise RuntimeError(msg)
    key = f"{goos}-{goarch}"
    if key not in TARGETS:
        msg = f"no wheel target for {key}; set DDT_TARGET explicitly"
        raise RuntimeError(msg)
    return key


class GoBinaryBuildHook(BuildHookInterface[Any]):
    """Compile ./cmd/docker-devtools into the wheel."""

    PLUGIN_NAME = "custom"

    #: Holds the cross-compiled binary until the wheel has been written.
    _tmpdir: str | None = None

    @override
    def initialize(self, version: str, build_data: dict[str, Any]) -> None:  # noqa: ARG002
        target = os.environ.get("DDT_TARGET") or host_target()
        if target not in TARGETS:
            msg = f"unknown DDT_TARGET {target!r}; expected one of: {', '.join(sorted(TARGETS))}"
            raise RuntimeError(msg)
        goos, goarch, plat_tag = TARGETS[target]

        go = shutil.which("go")
        if go is None:
            msg = (
                "the Go toolchain is required to build this package. "
                "Install Go 1.26+ (https://go.dev/dl/), or install a prebuilt wheel."
            )
            raise RuntimeError(msg)

        self._tmpdir = tempfile.mkdtemp(prefix="dbc-build-")
        exe = BINARY_NAME + (".exe" if goos == "windows" else "")
        out = os.path.join(self._tmpdir, exe)

        env = {
            **os.environ,
            "GOOS": goos,
            "GOARCH": goarch,
            "CGO_ENABLED": "0",
        }
        cmd = [
            go,
            "build",
            "-trimpath",
            "-ldflags",
            f"-s -w -X main.version={self.metadata.version}",
            "-o",
            out,
            "./cmd/docker-devtools",
        ]
        print(f"hatch_build: {target} -> {plat_tag}", file=sys.stderr)
        subprocess.run(cmd, cwd=self.root, env=env, check=True)  # noqa: S603

        build_data["tag"] = f"py3-none-{plat_tag}"
        build_data["pure_python"] = False
        build_data["shared_scripts"] = {out: exe}

    @override
    def finalize(
        self,
        version: str,  # noqa: ARG002
        build_data: dict[str, Any],  # noqa: ARG002
        artifact_path: str,  # noqa: ARG002
    ) -> None:
        if self._tmpdir:
            shutil.rmtree(self._tmpdir, ignore_errors=True)
            self._tmpdir = None
