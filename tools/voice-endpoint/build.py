#!/usr/bin/env python3
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT

"""Build an ARM endpoint with explicitly supplied, checksum-verified donor inputs."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
DONOR_SHA256 = "a7f56930102a1bad225c90b0ca53a67a18313574d53ac57c1dbe4a15c1801e3d"
MODEL_SHA256 = "6f089879c333500af74e22f685411b50fd533067f57a32ee88a9b39f53e247bc"
REAL_LIBRARIES = (
    "ld-linux-armhf.so.3", "libc.so.6", "libm.so.6",
    "libpthread.so.0", "libgcc_s.so.1", "libstdc++.so.6",
)
STUB_LIBRARIES = (
    "libskype_call.so", "libCDP_host.so", "libgstreamer-1.0.so.0",
    "libcurl.so.4", "libssl.so.1.0.0", "libcrypto.so.1.0.0",
    "libasound.so.2", "libglib-2.0.so.0", "libgobject-2.0.so.0",
    "libboost_system.so.1.65.1", "libboost_thread.so.1.65.1", "libuuid.so.1",
)


def sha256(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1 << 20), b""):
            value.update(block)
    return value.hexdigest()


def require_digest(path: Path, expected: str) -> None:
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"input must be a regular, non-symlink file: {path.name}")
    if sha256(path) != expected.lower():
        raise ValueError(f"input checksum mismatch: {path.name}")


def checked_inputs(bundle: Path) -> list[tuple[Path, Path]]:
    manifest_path = bundle / "manifest.json"
    if not manifest_path.is_file() or manifest_path.stat().st_size > 128 * 1024:
        raise ValueError("missing or oversized donor bundle manifest")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict) or not isinstance(manifest.get("files"), dict):
        raise ValueError("donor manifest must contain a files object")
    files = manifest["files"]
    labels = {
        **{f"lib/{name}": Path("lib") / name for name in REAL_LIBRARIES},
        **{f"stub/{name}": Path("lib") / name for name in STUB_LIBRARIES},
        "bin/cortana": Path("bin/cortana"),
        "share/heycortana_en-US.table": Path("share/heycortana_en-US.table"),
    }
    result: list[tuple[Path, Path]] = []
    for label, relative in labels.items():
        record = files.get(label)
        if not isinstance(record, dict):
            raise ValueError(f"donor manifest lacks {label}")
        expected = record.get("sha256")
        if not isinstance(expected, str) or len(expected) != 64:
            raise ValueError(f"invalid checksum record for {label}")
        source = bundle / relative
        require_digest(source, expected)
        target = Path("voice") / relative
        if label == "bin/cortana":
            require_digest(source, DONOR_SHA256)
        elif label == "share/heycortana_en-US.table":
            require_digest(source, MODEL_SHA256)
            target = Path("voice/share/handoff-original.table")
        result.append((source, target))
    return result


def build(output: Path, bundle: Path, go: str, cc: str) -> None:
    output = output.absolute()
    if output.is_symlink():
        raise ValueError("output directory must not be a symlink")
    output = output.resolve()
    bundle = bundle.resolve(strict=True)
    if output == bundle or bundle in output.parents:
        raise ValueError("output must be outside the donor bundle")
    if output.exists() and (not output.is_dir() or any(output.iterdir())):
        raise ValueError("output directory must be absent or empty")
    inputs = checked_inputs(bundle)
    go_program = shutil.which(go)
    cc_program = shutil.which(cc)
    if go_program is None or cc_program is None:
        raise ValueError("Go and ARM hard-float GCC are required; use --go and --cc")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".voice-build-", dir=output.parent) as temporary:
        stage = Path(temporary) / "payload"
        (stage / "bin").mkdir(parents=True, mode=0o700)
        for source, relative in inputs:
            target = stage / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
        environment = dict(os.environ)
        environment.update(
            GOOS="linux", GOARCH="arm", GOARM="7", CGO_ENABLED="0",
            GO111MODULE="on", GOWORK="off",
        )
        subprocess.run(
            [
                go_program, "build", "-trimpath", "-buildvcs=false",
                "-ldflags=-s -w", "-o", str(stage / "bin/reinvoke-voice"), ".",
            ],
            cwd=HERE, env=environment, check=True,
        )
        subprocess.run(
            [
                cc_program, "-O2", "-fPIC", "-shared", "-Wall", "-Wextra", "-Werror",
                str(HERE / "worker/unit_link.c"), f"-L{stage / 'voice/lib'}",
                "-Wl,--no-as-needed", "-l:libc.so.6", "-l:libm.so.6",
                "-o", str(stage / "voice/lib/unit-link.so"),
            ],
            check=True,
        )
        for executable in (stage / "bin/reinvoke-voice", stage / "voice/bin/cortana",
                           stage / "voice/lib/ld-linux-armhf.so.3"):
            executable.chmod(0o755)
        records = {
            path.relative_to(stage).as_posix(): {
                "bytes": path.stat().st_size, "sha256": sha256(path),
            }
            for path in sorted(stage.rglob("*")) if path.is_file()
        }
        (stage / "manifest.json").write_text(
            json.dumps({"schema": 1, "files": records}, indent=2) + "\n",
            encoding="utf-8",
        )
        if output.exists():
            output.rmdir()
        stage.rename(output)
    print(f"Voice endpoint built: {output}")


def create_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--donor-bundle", type=Path, required=True)
    parser.add_argument("--go", default=os.environ.get("GO", "go"))
    parser.add_argument("--cc", default=os.environ.get("CC", "arm-linux-gnueabihf-gcc"))
    return parser


def main() -> int:
    args = create_parser().parse_args()
    try:
        build(args.output_dir, args.donor_bundle, args.go, args.cc)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"Voice build failed: {error}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        return 130
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
