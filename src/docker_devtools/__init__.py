"""Work on the Dockerfiles, Compose files and build context in a repository.

This package bundles the ``docker-devtools`` binary and wraps its JSON output in
typed dataclasses. The work happens in Go, against the same libraries BuildKit
uses, so the results match what ``docker build`` would do.

    >>> from docker_devtools import image_ls
    >>> for ref in image_ls("testdata").refs:
    ...     print(ref.path, ref.line, ref.raw)
"""

from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass
from dataclasses import field
from typing import TYPE_CHECKING
from typing import Any
from typing import Literal

from docker_devtools._find import BINARY_ENV_VAR
from docker_devtools._find import BINARY_NAME
from docker_devtools._find import BinaryNotFoundError
from docker_devtools._find import find_binary

if TYPE_CHECKING:
    from collections.abc import Sequence

__all__ = [
    "BINARY_ENV_VAR",
    "BINARY_NAME",
    "IMAGE_SCHEMA_VERSION",
    "BinaryNotFoundError",
    "Change",
    "ImageRef",
    "ImageResult",
    "UpdateReport",
    "find_binary",
    "image_ls",
    "image_update",
]

#: The image-scan schema this wrapper understands. A mismatch means the binary
#: and the Python half are out of step.
IMAGE_SCHEMA_VERSION = 1

TagPolicy = Literal["same-pattern", "minor", "patch", "latest"]


@dataclass(frozen=True)
class ImageRef:
    """One image reference and where it sits."""

    path: str
    line: int
    kind: str
    raw: str
    resolved: bool
    registry: str | None = None
    repository: str | None = None
    tag: str | None = None
    digest: str | None = None
    stage: str | None = None
    note: str | None = None


@dataclass(frozen=True)
class ImageResult:
    """Every reference found by a scan."""

    schema: int
    refs: tuple[ImageRef, ...]
    warnings: tuple[str, ...] = ()

    def resolved(self) -> tuple[ImageRef, ...]:
        """Return only the references that name a real image."""
        return tuple(r for r in self.refs if r.resolved)


@dataclass(frozen=True)
class Change:
    """One reference an update would rewrite."""

    path: str
    line: int
    old: str
    new: str
    reason: str


@dataclass(frozen=True)
class UpdateReport:
    """The plan an update produced."""

    schema: int
    changes: tuple[Change, ...]
    skipped: int = 0
    warnings: tuple[str, ...] = field(default=())


def image_ls(*paths: str) -> ImageResult:
    """List every image reference under ``paths``.

    Args:
        paths: files or directories. Defaults to the working directory.

    Returns:
        The parsed scan.
    """
    data = _run(["image", "ls", "--json", *paths])
    _check_schema(data.get("schema"))
    return ImageResult(
        schema=data["schema"],
        refs=tuple(
            ImageRef(
                path=r["path"],
                line=r["line"],
                kind=r["kind"],
                raw=r["raw"],
                resolved=r["resolved"],
                registry=r.get("registry"),
                repository=r.get("repository"),
                tag=r.get("tag"),
                digest=r.get("digest"),
                stage=r.get("stage"),
                note=r.get("note"),
            )
            for r in data["refs"]
        ),
        warnings=tuple(data.get("warnings") or ()),
    )


def image_update(
    *paths: str,
    pin_digest: bool = False,
    tag_policy: TagPolicy | None = None,
    dry_run: bool = True,
) -> UpdateReport:
    """Plan, and optionally apply, changes to image references.

    ``dry_run`` defaults to True so that calling this by accident cannot
    rewrite a repository.

    Args:
        paths: files or directories. Defaults to the working directory.
        pin_digest: append or refresh the ``@sha256`` digest.
        tag_policy: how far a tag may move. None leaves tags alone.
        dry_run: report without writing.

    Returns:
        The plan, whether or not it was applied.
    """
    args = ["image", "update", "--json"]
    if pin_digest:
        args.append("--pin-digest")
    if tag_policy is not None:
        args += ["--tag-policy", tag_policy]
    if dry_run:
        args.append("--dry-run")
    args += list(paths)

    data = _run(args)
    _check_schema(data.get("schema"))
    return UpdateReport(
        schema=data["schema"],
        changes=tuple(
            Change(path=c["path"], line=c["line"], old=c["old"], new=c["new"], reason=c["reason"])
            for c in data["changes"]
        ),
        skipped=data.get("skipped", 0),
        warnings=tuple(data.get("warnings") or ()),
    )


def _run(args: Sequence[str]) -> dict[str, Any]:
    binary = find_binary()
    proc = subprocess.run(  # noqa: S603
        [binary, *args],
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0:
        msg = f"{BINARY_NAME} {' '.join(args)} failed: {proc.stderr.strip()}"
        raise RuntimeError(msg)
    result: dict[str, Any] = json.loads(proc.stdout)
    return result


def _check_schema(schema: object) -> None:
    if schema != IMAGE_SCHEMA_VERSION:
        msg = (
            f"{BINARY_NAME} emitted schema {schema!r}, but this package understands "
            f"{IMAGE_SCHEMA_VERSION}. The binary and the Python wrapper are out of step."
        )
        raise RuntimeError(msg)
