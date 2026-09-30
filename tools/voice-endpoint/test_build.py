# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT

from __future__ import annotations

import hashlib
import json
from pathlib import Path

import pytest

import build as voice_build


@pytest.fixture
def bundle(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    root = tmp_path / "donor"
    labels = {
        **{f"lib/{name}": f"lib/{name}" for name in voice_build.REAL_LIBRARIES},
        **{f"stub/{name}": f"lib/{name}" for name in voice_build.STUB_LIBRARIES},
        "bin/cortana": "bin/cortana",
        "share/heycortana_en-US.table": "share/heycortana_en-US.table",
    }
    records = {}
    for label, relative in labels.items():
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(relative.encode("ascii"))
        records[label] = {"sha256": voice_build.sha256(path)}
    monkeypatch.setattr(voice_build, "DONOR_SHA256", records["bin/cortana"]["sha256"])
    monkeypatch.setattr(
        voice_build, "MODEL_SHA256",
        records["share/heycortana_en-US.table"]["sha256"],
    )
    (root / "manifest.json").write_text(json.dumps({"files": records}))
    return root


def test_runtime_input_selection_and_tamper_failure(bundle: Path) -> None:
    inputs = voice_build.checked_inputs(bundle)
    assert len(inputs) == 20
    assert inputs[-1][1] == Path("voice/share/handoff-original.table")
    library = bundle / "lib/libc.so.6"
    original = library.read_bytes()
    library.write_bytes(original + b"corrupt")
    with pytest.raises(ValueError, match="checksum mismatch"):
        voice_build.checked_inputs(bundle)
    library.write_bytes(original)
    assert voice_build.checked_inputs(bundle) == inputs


def test_donor_pin_rejects_replacement_even_with_updated_manifest(bundle: Path) -> None:
    donor = bundle / "bin/cortana"
    donor.write_bytes(b"a different executable")
    manifest = json.loads((bundle / "manifest.json").read_text())
    manifest["files"]["bin/cortana"]["sha256"] = hashlib.sha256(
        donor.read_bytes()
    ).hexdigest()
    (bundle / "manifest.json").write_text(json.dumps(manifest))
    with pytest.raises(ValueError, match="checksum mismatch"):
        voice_build.checked_inputs(bundle)


def test_build_refuses_to_overwrite_other_output(bundle: Path, tmp_path: Path) -> None:
    output = tmp_path / "output"
    output.mkdir()
    sentinel = output / "keep"
    sentinel.write_text("original")
    with pytest.raises(ValueError, match="absent or empty"):
        voice_build.build(output, bundle, "go", "arm-linux-gnueabihf-gcc")
    assert sentinel.read_text() == "original"


def test_build_refuses_output_inside_donor_bundle(bundle: Path) -> None:
    with pytest.raises(ValueError, match="outside the donor bundle"):
        voice_build.build(bundle / "output", bundle, "go", "arm-linux-gnueabihf-gcc")
