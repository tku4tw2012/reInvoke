#!/usr/bin/env python3
"""Drive the U-Boot console to a finished NAND write.

Keep artifact checks in preflight, before the operator touches the speaker.
This driver attempts the approved command at most once and observes completion;
that does not replace full image, program/readback, or native-boot verification.
Exit 4 means completion was observed but command delivery or evidence recording
is incomplete. Exit 5 rejects an unsolicited completion before our command.
Recording errors after a command attempt retain transport until completion.

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
from pathlib import Path
from typing import BinaryIO, Optional

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


def run(
    port: int, command: str, timeout: float, quiet: bool,
    transcript: BinaryIO | None = None,
) -> int:
    deadline = None if timeout <= 0 else time.monotonic() + timeout
    seen = bytearray()
    attempted = False
    sent = False
    recording_error: str | None = None

    def recording_failed(message: str, cause: OSError | None = None) -> None:
        nonlocal recording_error
        if not attempted:
            raise RuntimeError(message) from cause
        if recording_error is None:
            recording_error = message

    def say(message: str) -> None:
        if not quiet:
            # The helper shares this log; delimit each status in one write.
            record = f"\n{message}\n"
            try:
                written = sys.stdout.write(record)
                sys.stdout.flush()
            except OSError as exc:
                recording_failed("console status log write failed", exc)
            else:
                if written != len(record):
                    recording_failed("console status log write was incomplete")

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
                        if transcript is not None:
                            try:
                                written = transcript.write(chunk)
                            except OSError as exc:
                                recording_failed("console transcript write failed", exc)
                            else:
                                if written != len(chunk):
                                    recording_failed("console transcript write was incomplete")
                            if recording_error is not None:
                                transcript = None
                                say("WARNING evidence recording failed; retaining transport until completion")
                        seen.extend(chunk)
                    elif not chunk:
                        raise ConnectionResetError("relay closed")
                except socket.timeout:
                    pass

                text = strip_telnet(bytes(seen)).decode("utf-8", "replace")

                if SUCCESS in text:
                    if not attempted:
                        say("FAIL unsolicited completion before this invocation's command")
                        return 5
                    if not sent or recording_error is not None:
                        say("FAIL device reported completion but command/evidence verification is incomplete")
                        return 4
                    say(f"OK {SUCCESS}")
                    return 4 if recording_error is not None else 0

                if not attempted and PROMPT in text:
                    # Clear the line buffer first. U-Boot's boot script emits
                    # progress characters, and one arrived mid-send on the 05.5
                    # flash: the device reported "Unknown command '+l2nand'"
                    # and the write never started. A bare newline discards
                    # whatever partial input is already queued.
                    sock.sendall(b"\r\n")
                    time.sleep(0.4)
                    seen.clear()
                    # A partially delivered command must never be retried.
                    attempted = True
                    sock.sendall(command.encode() + b"\r\n")
                    sent = True
                    say(f"sent {command}")
                # Nothing is sent while waiting. This used to nudge the
                # device with a newline every three seconds, which puts bytes
                # into the recovery handshake at moments nobody chose. The
                # tooling that was reliable for the whole RAM-boot era never
                # did it: uboot-console.py forwards what an operator writes
                # and nothing else. Of the runs recorded here, the only one
                # that reached iROM without a single unsolicited byte reached
                # it in two attempts; the runs that nudged took nine, or never
                # arrived at all. That is correlation rather than proof, but
                # the nudge buys nothing, and seize-then-flash.sh probes
                # deliberately once the console is already talking.
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
    parser.add_argument(
        "--console-log",
        type=Path,
        help="new file for complete raw relay bytes, including across reconnects",
    )
    args = parser.parse_args()
    if args.console_log is not None:
        with args.console_log.open("xb", buffering=0) as transcript:
            return run(args.port, args.command, args.timeout, args.quiet, transcript)
    return run(args.port, args.command, args.timeout, args.quiet)


if __name__ == "__main__":
    sys.exit(main())
