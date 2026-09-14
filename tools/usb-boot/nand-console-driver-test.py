#!/usr/bin/env python3
"""Offline tests for the NAND console driver.

Each case stands up a fake relay that speaks the bytes the real device sent,
including the interleaved banner that used to abort the flash, and checks the
driver reaches the right verdict. Negative cases prove a pass means something.
"""
from __future__ import annotations

import os
import socket
import subprocess
import sys
import threading
import time

HERE = os.path.dirname(os.path.abspath(__file__))
DRIVER = os.path.join(HERE, "nand-console-driver.py")

# Taken from evidence/native054-flash-20260914: the banner arrives spliced with
# boot chatter, which is why an exact build-string compare cannot be used.
BANNER = (
    b"img: jump to f00000\r\n\r\nU-Boot 2013.04 (Apr 11 2016 - 10:10:25)"
    b"i*m*g*r*q*y environment in SPI flash is invalid.\r\n"
    b"No ethernet found.\r\ndo_usball done.\r\n"
)
PROMPT = b"MV88DE3100|> "
WRITING = b"Reading NAND at address 0x001C60000\r\n"
SUCCESS = b"Congratulations! u2nand succeed!\r\nMV88DE3100|> "


class Relay:
    """Serves exactly one client, like the real helper does."""

    def __init__(self, script, drop_after=None):
        self.script = script
        self.drop_after = drop_after
        self.received = bytearray()
        self.sock = socket.socket()
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.sock.bind(("127.0.0.1", 0))
        self.sock.listen(4)
        self.port = self.sock.getsockname()[1]
        self.clients = 0
        self.thread = threading.Thread(target=self._serve, daemon=True)
        self.thread.start()

    def _serve(self):
        while True:
            try:
                conn, _ = self.sock.accept()
            except OSError:
                return
            self.clients += 1
            threading.Thread(target=self._session, args=(conn,), daemon=True).start()

    def _session(self, conn):
        conn.settimeout(0.2)
        try:
            for delay, payload, needs_cmd in self.script:
                waited = 0.0
                while needs_cmd and waited < 20:
                    try:
                        data = conn.recv(4096)
                        if data:
                            self.received.extend(data)
                    except socket.timeout:
                        pass
                    if b"l2nand" in bytes(self.received):
                        break
                    waited += 0.2
                else:
                    if needs_cmd:
                        return
                time.sleep(delay)
                conn.sendall(payload)
                # Drop once only. Dropping on every reconnect would make the
                # scenario unsurvivable and test nothing about the driver.
                if self.drop_after and payload is self.drop_after:
                    self.drop_after = None
                    conn.close()
                    return
            # Keep draining so the driver's nudges do not error.
            end = time.time() + 5
            while time.time() < end:
                try:
                    data = conn.recv(4096)
                    if data:
                        self.received.extend(data)
                except socket.timeout:
                    pass
        except OSError:
            pass
        finally:
            try:
                conn.close()
            except OSError:
                pass

    def close(self):
        try:
            self.sock.close()
        except OSError:
            pass


def drive(port, timeout=25):
    out = subprocess.run(
        [sys.executable, DRIVER, "--port", str(port), "--timeout", str(timeout)],
        capture_output=True, text=True, timeout=timeout + 20,
    )
    return out.returncode, out.stdout + out.stderr


def case(name, script, drop_after=None, expect_rc=0, expect_cmd=True, timeout=25):
    relay = Relay(script, drop_after)
    try:
        rc, out = drive(relay.port, timeout)
        sent = b"l2nand 83" in bytes(relay.received)
        ok = rc == expect_rc and sent == expect_cmd
        print(f"{'PASS' if ok else 'FAIL'}  {name}"
              f"  (rc={rc} expected={expect_rc}, sent_cmd={sent} expected={expect_cmd})")
        if not ok:
            print("      driver said:", out.strip().replace("\n", " | ")[:300])
        return ok
    finally:
        relay.close()


def main():
    results = []

    # Happy path: banner, prompt, command, write chatter, confirmation.
    results.append(case(
        "flashes when the device confirms the write",
        [(0.2, BANNER, False), (0.1, PROMPT, False),
         (0.2, WRITING, True), (0.3, SUCCESS, False)],
    ))

    # The failure that cost a window: the console dies right as U-Boot appears.
    # The driver must reconnect, not exit, because the confirmation is only
    # ever delivered on the console.
    results.append(case(
        "reconnects when the console drops mid-session",
        [(0.2, BANNER, False), (0.1, PROMPT, False),
         (0.2, WRITING, True), (0.3, SUCCESS, False)],
        drop_after=WRITING,
    ))

    # Negative: a device that never reaches a prompt must not be called a
    # success, and no command may be sent into the dark.
    results.append(case(
        "does not claim success without a prompt",
        [(0.2, BANNER, False), (0.2, b"boot chatter only\r\n", False)],
        expect_rc=3, expect_cmd=False, timeout=8,
    ))

    # Negative: prompt reached and command sent, but the device never confirms.
    # This must fail, otherwise a pass would mean nothing.
    results.append(case(
        "does not claim success without the confirmation",
        [(0.2, BANNER, False), (0.1, PROMPT, False), (0.2, WRITING, True)],
        expect_rc=3, expect_cmd=True, timeout=8,
    ))

    # Negative: no relay at all.
    rc, _ = drive(1, timeout=4)
    ok = rc != 0
    print(f"{'PASS' if ok else 'FAIL'}  reports failure when no relay exists  (rc={rc})")
    results.append(ok)

    print()
    if all(results):
        print(f"ALL {len(results)} CASES PASSED")
        return 0
    print(f"{results.count(False)} of {len(results)} FAILED")
    return 1


if __name__ == "__main__":
    sys.exit(main())
