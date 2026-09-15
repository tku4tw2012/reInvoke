#!/usr/bin/env python3
"""Console client for usb_boot's telnet relay.

Connects to the usb_boot TCP console, strips telnet IAC negotiation, appends all
received bytes to a log file, and forwards commands written to a FIFO.
"""
import os
import select
import socket
import sys
import time

HOST, PORT = "127.0.0.1", 8141
LOG = "/tmp/uboot.log"
FIFO = "/tmp/uboot_cmd"

IAC, DONT, DO, WONT, WILL, SB, SE = 255, 254, 253, 252, 251, 250, 240


def negotiate(data, sock):
    """Strip telnet control sequences without replying.

    usb_boot answers every DONT/WONT with another negotiation command, so any
    reply produces an endless loop. Silently consuming the sequences avoids it.
    """
    out = bytearray()
    i = 0
    while i < len(data):
        if data[i] == IAC and i + 1 < len(data):
            cmd = data[i + 1]
            if cmd in (DO, DONT, WILL, WONT):
                i += 3
                continue
            if cmd == SB:
                end = data.find(bytes([IAC, SE]), i)
                i = end + 2 if end != -1 else len(data)
                continue
            i += 2
            continue
        out.append(data[i])
        i += 1
    return bytes(out)


def main():
    if not os.path.exists(FIFO):
        os.mkfifo(FIFO)

    # Open the FIFO before the socket. Arming happens before yellow mode, so
    # the helper's relay port does not exist yet and the connect below will be
    # refused for as long as the operator takes to enter service mode. Exiting
    # on that leaves the command FIFO with no reader, and a loader watching for
    # a reader concludes the capture session died and stops driving the boot.
    fifo = os.open(FIFO, os.O_RDONLY | os.O_NONBLOCK)

    sock = None
    while sock is None:
        try:
            sock = socket.create_connection((HOST, PORT), timeout=10)
        except OSError:
            time.sleep(0.5)
    sock.setblocking(False)

    log = open(LOG, "ab", buffering=0)
    log.write(f"\n=== connected {time.strftime('%H:%M:%S')} ===\n".encode())

    while True:
        readable, _, _ = select.select([sock, fifo], [], [], 1.0)

        if sock in readable:
            try:
                data = sock.recv(65536)
            except BlockingIOError:
                data = b""
            if data == b"":
                log.write(b"\n=== console closed ===\n")
                break
            clean = negotiate(data, sock)
            if clean:
                log.write(clean)

        if fifo in readable:
            cmd = os.read(fifo, 4096)
            if cmd:
                sock.sendall(cmd)
                log.write(b"\n[SENT] " + cmd)
            else:
                os.close(fifo)
                fifo = os.open(FIFO, os.O_RDONLY | os.O_NONBLOCK)

    log.close()
    sock.close()


if __name__ == "__main__":
    sys.exit(main())
