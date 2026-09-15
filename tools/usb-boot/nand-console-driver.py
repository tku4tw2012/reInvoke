#!/usr/bin/env python3
"""Drive the U-Boot console to a finished NAND write.

This is deliberately small. Every guard that lived here before aborted the
flash for a cosmetic reason while the service-mode window was open, which is
far more expensive than the mistake each guard was meant to catch. Correctness
checks belong in the preflight, before the operator touches the speaker.

Behaviours that are here because losing them cost a window:

* reconnect. The client died once on a broken pipe exactly as U-Boot came up,
  and the relay then sat idle with the device waiting at its prompt.
* one client. The relay hands the console to a single telnet client, so a
  stale connection from a previous run silently blocks every later one.
* no build-string match. The banner arrives interleaved with boot chatter
  ("U-Boot 2013.04 ...i*m*g*r*q*y environment in SPI flash is invalid"), so an
  exact compare aborts on noise.
* clear the line before sending. U-Boot's boot script prints progress
  characters, and one landed mid-command on the 05.5 flash: the device
  answered "Unknown command '+l2nand'" and no write started.
* confirm the write, then verify the command was accepted. A mangled command
  leaves U-Boot back at its prompt looking exactly like a finished flash.
* announce success on the same stream that observes it, so the caller never
  has to guess which log to read.
"""
from __future__ import annotations

import argparse
import socket
import sys
import time
from typing import Optional

PROMPT = "MV88DE3100"
SUCCESS = "u2nand succeed"


def strip_telnet(raw: bytes) -> bytes:
    """Drop IAC negotiation without answering it.

    Replying pulls the relay into a DONT/WONT loop that never settles, so the
    negotiation is consumed and discarded.
    """
    out = bytearray()
    i = 0
    while i < len(raw):
        if raw[i] == 0xFF:
            i += 3
        else:
            out.append(raw[i])
            i += 1
    return bytes(out)


def before_deadline(deadline: Optional[float]) -> bool:
    return deadline is None or time.monotonic() < deadline


def connect(port: int, deadline: Optional[float]):
    while before_deadline(deadline):
        try:
            sock = socket.create_connection(("127.0.0.1", port), 2)
            sock.settimeout(0.3)
            return sock
        except OSError:
            time.sleep(0.5)
    return None


def run(port: int, command: str, timeout: float, quiet: bool) -> int:
    deadline = None if timeout <= 0 else time.monotonic() + timeout
    seen = bytearray()
    sent = False
    nudged = 0.0

    def say(message: str) -> None:
        if not quiet:
            print(message, flush=True)

    while before_deadline(deadline):
        sock = connect(port, deadline)
        if sock is None:
            say("FAIL no console relay appeared")
            return 2
        say("console attached")

        try:
            while before_deadline(deadline):
                try:
                    chunk = sock.recv(4096)
                    if chunk:
                        seen.extend(chunk)
                    elif not chunk:
                        raise ConnectionResetError("relay closed")
                except socket.timeout:
                    pass

                text = strip_telnet(bytes(seen)).decode("utf-8", "replace")

                if SUCCESS in text:
                    say(f"OK {SUCCESS}")
                    return 0

                if not sent and PROMPT in text:
                    # Clear the line buffer first. U-Boot's boot script emits
                    # progress characters, and one arrived mid-send on the 05.5
                    # flash: the device reported "Unknown command '+l2nand'"
                    # and the write never started. A bare newline discards
                    # whatever partial input is already queued.
                    sock.sendall(b"\r\n")
                    time.sleep(0.4)
                    sock.sendall(command.encode() + b"\r\n")
                    sent = True
                    say(f"sent {command}")
                elif not sent and time.monotonic() - nudged > 3:
                    nudged = time.monotonic()
                    sock.sendall(b"\r\n")
        except OSError as exc:
            # Reconnect rather than exit: the write may already be running on
            # the device, and the confirmation only arrives on the console.
            say(f"console dropped ({exc}); reattaching")
            try:
                sock.close()
            except OSError:
                pass
            time.sleep(0.5)
            continue

    say("FAIL timed out before the device confirmed the write")
    return 3


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=8141)
    parser.add_argument("--command", default="l2nand 83")
    parser.add_argument(
        "--timeout",
        type=float,
        default=0,
        help="seconds to wait; 0 waits indefinitely",
    )
    parser.add_argument("--quiet", action="store_true")
    args = parser.parse_args()
    return run(args.port, args.command, args.timeout, args.quiet)


if __name__ == "__main__":
    sys.exit(main())
