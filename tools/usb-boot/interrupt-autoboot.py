#!/usr/bin/env python3
"""Send harmless keystrokes into the U-Boot console during the boot window.

The device requests image type 0x08, which is what U-Boot's `usbload 8` would
produce. If U-Boot is running an autoboot countdown, a keypress interrupts it
and drops to the prompt. The USB window is only a few seconds, so keys are fed
continuously and are waiting whenever the device attaches.

Only newlines are sent. At a U-Boot prompt a bare newline is an empty command,
and during an autoboot countdown any key stops it.
"""
import os
import sys
import time

FIFO = "/tmp/uboot_cmd"
DURATION = float(sys.argv[1]) if len(sys.argv) > 1 else 180.0
INTERVAL = 0.15


def main():
    if not os.path.exists(FIFO):
        print(f"FATAL: {FIFO} does not exist. Start the console client first.")
        return 1

    deadline = time.time() + DURATION
    sent = 0

    # Opening write-only would block until the reader is present; the console
    # client holds it open, so this returns immediately when it is running.
    try:
        fd = os.open(FIFO, os.O_WRONLY | os.O_NONBLOCK)
    except OSError as exc:
        print(f"FATAL: cannot open {FIFO} for writing: {exc}")
        return 1

    print(f"feeding newlines for {DURATION:.0f}s -- power cycle now")
    while time.time() < deadline:
        try:
            os.write(fd, b"\n")
            sent += 1
        except BlockingIOError:
            pass
        except OSError:
            break
        time.sleep(INTERVAL)

    os.close(fd)
    print(f"done, {sent} keystrokes sent")
    return 0


if __name__ == "__main__":
    sys.exit(main())
