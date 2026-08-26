#!/usr/bin/env python3
"""Acquire explicitly listed artifacts without executing or overwriting them."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request
import zipfile
from datetime import datetime, timezone
from pathlib import Path


REPO = Path(__file__).resolve().parent.parent
METADATA = REPO / "metadata"

# Bulk artifacts are stored OUTSIDE this repository (see docs/acquisition/storage-policy.md).
# The archive root must be given explicitly so a stray run can never scatter gigabytes
# into the repository or the user's home directory.
DEFAULT_ARCHIVE_ROOT = REPO.parent / "reinvoke-archive"


def resolve_archive_root(explicit: str | None) -> Path:
    candidate = Path(explicit).expanduser() if explicit else Path(
        os.environ.get("REINVOKE_ARCHIVE", DEFAULT_ARCHIVE_ROOT)
    ).expanduser()
    candidate = candidate.resolve()
    if candidate == Path.home() or candidate == REPO or REPO in candidate.parents:
        raise SystemExit(
            f"refusing to use archive root {candidate}: it must be a dedicated directory "
            f"outside the repository and not the home directory"
        )
    if not candidate.is_dir():
        raise SystemExit(
            f"archive root {candidate} does not exist; create it or pass --archive-root"
        )
    return candidate


ROOT = DEFAULT_ARCHIVE_ROOT


def now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def digests(path: Path) -> tuple[str, str, int]:
    sha256 = hashlib.sha256()
    sha1 = hashlib.sha1()
    size = 0
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            size += len(block)
            sha256.update(block)
            sha1.update(block)
    return sha256.hexdigest(), sha1.hexdigest(), size


def write_metadata(item: dict, metadata: dict) -> None:
    path = METADATA / f"{item['id']}.json"
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        return
    # A sidecar already exists. Only a genuinely new acquisition is worth recording as an
    # additional timestamped record; skips, unchanged discovery pointers, and repeated
    # failures would otherwise accumulate duplicates on every re-run.
    if metadata.get("status") not in ("DOWNLOADED", "MIRRORED"):
        return
    path = METADATA / f"{item['id']}-{datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')}.json"
    path.write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def safe_extract(archive: Path, destination: Path) -> None:
    destination.mkdir(parents=True, exist_ok=True)
    target = destination.resolve()
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as source:
            members = source.infolist()
            for member in members:
                candidate = (destination / member.filename).resolve()
                if target not in candidate.parents and candidate != target:
                    raise ValueError(f"unsafe archive member: {member.filename}")
            source.extractall(destination)
    elif archive.name.endswith((".tar.gz", ".tgz", ".tar")):
        with tarfile.open(archive) as source:
            for member in source.getmembers():
                candidate = (destination / member.name).resolve()
                if target not in candidate.parents and candidate != target:
                    raise ValueError(f"unsafe archive member: {member.name}")
            source.extractall(destination)


def acquire_download(item: dict) -> dict:
    destination = ROOT / item["destination"]
    metadata = {
        "id": item["id"],
        "kind": item["kind"],
        "source_url": item["source_url"],
        "provenance_page": item.get("provenance_page"),
        "retrieved_utc": now(),
        "destination": str(destination.relative_to(ROOT)),
    }
    if destination.exists():
        metadata["status"] = "SKIPPED_EXISTING"
        write_metadata(item, metadata)
        return metadata
    destination.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=".download-", dir=destination.parent)
    os.close(descriptor)
    temporary = Path(temporary_name)
    try:
        request = urllib.request.Request(item["source_url"], headers={"User-Agent": "invoke-berlin-archive/1.0"})
        with urllib.request.urlopen(request, timeout=60) as response, temporary.open("wb") as output:
            shutil.copyfileobj(response, output)
            metadata["final_url"] = response.geturl()
            metadata["http_status"] = response.status
            metadata["mime"] = response.headers.get_content_type()
            metadata["content_type"] = response.headers.get("Content-Type")
            metadata["http_etag"] = response.headers.get("ETag")
            metadata["http_last_modified"] = response.headers.get("Last-Modified")
        temporary.replace(destination)
        sha256, sha1, size = digests(destination)
        metadata.update(status="DOWNLOADED", sha256=sha256, sha1=sha1, size_bytes=size)
        if item.get("extract"):
            extracted = ROOT / "derived" / "extracted" / item["id"]
            safe_extract(destination, extracted)
            metadata["extracted_to"] = str(extracted.relative_to(ROOT))
    except Exception as error:
        metadata.update(status="FAILED", error=f"{type(error).__name__}: {error}")
    finally:
        temporary.unlink(missing_ok=True)
    write_metadata(item, metadata)
    return metadata


def acquire_git(item: dict) -> dict:
    destination = ROOT / item["destination"]
    metadata = {
        "id": item["id"],
        "kind": item["kind"],
        "clone_url": item["source_url"],
        "retrieved_utc": now(),
        "mirror_path": str(destination.relative_to(ROOT)),
    }
    if destination.exists():
        metadata["status"] = "SKIPPED_EXISTING"
        write_metadata(item, metadata)
        return metadata
    destination.parent.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env["GIT_TERMINAL_PROMPT"] = "0"
    try:
        result = subprocess.run(
            ["git", "clone", "--mirror", item["source_url"], str(destination)],
            capture_output=True, text=True, check=False, timeout=120, env=env,
        )
    except subprocess.TimeoutExpired as error:
        metadata.update(status="FAILED", error=f"git clone timeout after 120s: {error}")
        shutil.rmtree(destination, ignore_errors=True)
        write_metadata(item, metadata)
        return metadata
    if result.returncode:
        metadata.update(status="FAILED", error=result.stderr.strip() or f"git exited {result.returncode}")
        # Remove the partial clone, otherwise the next run sees the directory, reports
        # SKIPPED_EXISTING, and silently never retries the failed acquisition.
        shutil.rmtree(destination, ignore_errors=True)
    else:
        head = subprocess.run(["git", "--git-dir", str(destination), "rev-parse", "HEAD"],
                              capture_output=True, text=True, check=True, timeout=30)
        refs = subprocess.run(["git", "--git-dir", str(destination), "for-each-ref"],
                              capture_output=True, text=True, check=True, timeout=30)
        default_branch = subprocess.run(["git", "--git-dir", str(destination), "symbolic-ref", "HEAD"],
                                       capture_output=True, text=True, check=False, timeout=30)
        metadata.update(status="MIRRORED", head_commit=head.stdout.strip(), refs_count=len(refs.stdout.splitlines()))
        if default_branch.returncode == 0:
            metadata["default_branch"] = default_branch.stdout.strip().replace("refs/heads/", "")
        if item.get("required_commit"):
            check = subprocess.run(["git", "--git-dir", str(destination), "cat-file", "-e",
                                    item["required_commit"]], check=False, timeout=30)
            metadata["required_commit"] = item["required_commit"]
            metadata["required_commit_present"] = check.returncode == 0
    write_metadata(item, metadata)
    return metadata


def main() -> int:
    global ROOT
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", default=str(Path(__file__).with_name("acquisitions.json")))
    parser.add_argument(
        "--archive-root",
        default=None,
        help="directory for bulk artifacts (default: $REINVOKE_ARCHIVE or ../reinvoke-archive)",
    )
    args = parser.parse_args()
    ROOT = resolve_archive_root(args.archive_root)
    print(f"archive root: {ROOT}")
    print(f"metadata:     {METADATA}")
    items = json.loads(Path(args.manifest).read_text(encoding="utf-8"))
    for item in items:
        result = acquire_download(item) if item["kind"] == "download" else (
            acquire_git(item) if item["kind"] == "git_mirror" else {
                "id": item["id"], "status": "DISCOVERY_ONLY",
                "source_url": item["source_url"], "retrieved_utc": now(),
            }
        )
        if item["kind"] == "provenance":
            write_metadata(item, result)
        print(f"{result['id']}: {result['status']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
