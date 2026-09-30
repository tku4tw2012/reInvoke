"""Create a fresh private host identity; print only its DER certificate pin.

Usage: python3 init_host.py --directory NEW_PRIVATE_DIRECTORY
Add --endpoint HOSTNAME:PORT --device NAME to create a private device.json
with io_timeout_ms=30000 for the device connector. Endpoint policy belongs
to the backend, never to device configuration.
Requires the existing openssl command; never overwrites an existing directory.
No certificate authority, discovery service, or device action is involved.
"""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import os
import re
import secrets
import ssl
import subprocess
import sys
from pathlib import Path

from catcher import DEVICE_PATTERN


def generate_identity(
    directory: Path, *, endpoint: str | None = None, device: str | None = None,
) -> str:
    if (endpoint is None) != (device is None):
        raise ValueError("--endpoint and --device must be supplied together")
    if endpoint is not None and device is not None:
        if not DEVICE_PATTERN.fullmatch(device):
            raise ValueError("--device must be 1..64 ASCII letters, digits, dots, underscores or hyphens")
        hostname, _, port = endpoint.rpartition(":")
        host = hostname.removesuffix(".")
        if (
            not 0 < len(host) <= 253
            or any(
                not re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?", label)
                for label in host.split(".")
            )
            or not re.fullmatch(r"[0-9]{1,5}", port)
            or not 1 <= int(port) <= 65535
        ):
            raise ValueError("--endpoint must be a hostname:port with port 1..65535")
        try:
            ipaddress.ip_address(hostname)
        except ValueError:
            pass
        else:
            raise ValueError("--endpoint must use a hostname, not an IP address")
    directory.mkdir(mode=0o700)
    previous = os.umask(0o077)
    try:
        subprocess.run(
            [
                "openssl", "req", "-x509", "-newkey", "ec",
                "-pkeyopt", "ec_paramgen_curve:prime256v1",
                "-sha256", "-days", "3650", "-nodes", "-subj", "/CN=reinvoke-voice",
                "-keyout", str(directory / "host-key.pem"),
                "-out", str(directory / "host-cert.pem"),
            ],
            check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            timeout=30,
        )
        token = secrets.token_hex(32)
        with (directory / "token").open("x", encoding="ascii") as output:
            output.write(token + "\n")
        for name in ("host-key.pem", "host-cert.pem", "token"):
            (directory / name).chmod(0o600)
        der = ssl.PEM_cert_to_DER_cert((directory / "host-cert.pem").read_text(encoding="ascii"))
        pin = hashlib.sha256(der).hexdigest()
        if endpoint is not None:
            config = {
                "endpoint": endpoint, "certificate_sha256": pin, "token": token,
                "device": device, "io_timeout_ms": 30000,
            }
            path = directory / "device.json"
            with path.open("x", encoding="ascii") as output:
                json.dump(config, output, separators=(",", ":"), sort_keys=True)
                output.write("\n")
            path.chmod(0o600)
        return pin
    finally:
        os.umask(previous)


def create_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", required=True, type=Path)
    parser.add_argument("--endpoint", help="hostname:port for private device.json (requires --device)")
    parser.add_argument("--device", help="device identifier for private device.json (requires --endpoint)")
    return parser


def main() -> int:
    parser = create_parser()
    args = parser.parse_args()
    try:
        pin = generate_identity(args.directory, endpoint=args.endpoint, device=args.device)
    except ValueError as error:
        parser.error(str(error))
    except (OSError, subprocess.SubprocessError):
        print("Host identity creation failed; use a fresh private directory.", file=sys.stderr)
        return 1
    print(pin)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
