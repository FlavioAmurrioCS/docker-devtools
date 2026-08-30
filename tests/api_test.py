from __future__ import annotations

from typing import TYPE_CHECKING

import pytest

from docker_devtools import IMAGE_SCHEMA_VERSION
from docker_devtools import image_ls
from docker_devtools import image_update

if TYPE_CHECKING:
    from pathlib import Path


def test_image_ls_finds_dockerfile_and_compose_references(fixture_dir: Path) -> None:
    result = image_ls(str(fixture_dir / "imageref"))
    assert result.schema == IMAGE_SCHEMA_VERSION
    raws = {r.raw for r in result.resolved()}
    assert "golang:1.26-alpine" in raws
    assert "nginx:1.29-alpine" in raws


def test_image_ls_marks_unresolvable_references(fixture_dir: Path) -> None:
    result = image_ls(str(fixture_dir / "imageref" / "multistage"))
    by_raw = {r.raw: r for r in result.refs}

    # A stage name is not an image.
    stage_ref = by_raw["builder"]
    assert not stage_ref.resolved
    assert stage_ref.note is not None
    assert "stage" in stage_ref.note

    # Neither is a base built from a build argument.
    arg_based = by_raw["golang:${GO_VERSION}-alpine"]
    assert not arg_based.resolved
    assert arg_based.note is not None
    assert "argument" in arg_based.note


def test_image_ls_parses_reference_parts(fixture_dir: Path) -> None:
    result = image_ls(str(fixture_dir / "imageref" / "compose"))
    nginx = next(r for r in result.refs if r.raw.startswith("nginx"))
    assert nginx.repository == "library/nginx"
    assert nginx.tag == "1.29-alpine"
    assert nginx.stage == "web"


def test_image_update_defaults_to_dry_run(fixture_dir: Path, tmp_path: Path) -> None:
    # Nothing should be written even if the plan is non-empty, so point at a
    # copy and assert it is untouched.
    src = fixture_dir / "imageref" / "edge" / "Dockerfile"
    target = tmp_path / "Dockerfile"
    target.write_text(src.read_text())
    before = target.read_text()

    report = image_update(str(tmp_path), pin_digest=True)
    assert report.schema == IMAGE_SCHEMA_VERSION
    assert target.read_text() == before


def test_image_update_needs_something_to_do(tmp_path: Path) -> None:
    with pytest.raises(RuntimeError, match="nothing to do"):
        image_update(str(tmp_path))
