"""RIWAKE03 TLS test backend streaming a supplied, file-backed WAV.

Run with --bind ADDRESS --port PORT --cert CERT --key KEY --token-file TOKEN
--reply-wav WAV_PATH --stub-end-after-ms N (100..60000). This explicit
stub policy counts postwake samples, not wall time; it is not VAD or a device
capture limit. Streamed PCM is hashed incrementally. Per-turn JSONL on stdout
reports those counts/hashes and is bounded to 2 KiB per record;
errors and session summaries go to stderr. No audio is recorded; reply and
capture buffers do not grow with duration. score is the worker's confidence value.
The device pins SHA-256 of the DER leaf certificate and initiates connections.
Hello must explicitly select source="socket" and playback=true by default.
--allow-diagnostics also permits fixture and/or validation-only sessions.
"""

from __future__ import annotations

import argparse
import hmac
import json
import logging
import math
import os
import re
import signal
import socket
import ssl
import stat
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Literal

from catcher_protocol import (
    ProtocolError, Receiver, ReplySource, SocketIO, load_reply_wav, reply_source,
    validate_stub_endpoint,
)

HANDSHAKE_BYTES = 1024
HANDSHAKE_TIMEOUT = 5.0
MAX_TURN_JSON_BYTES = 2048
HANDSHAKE_OK = b'{"ok":true}\n'
HANDSHAKE_FAILURE = b'{"ok":false,"error":"handshake rejected"}\n'
DEVICE_PATTERN = re.compile(r"[A-Za-z0-9._-]{1,64}")
TOKEN_PATTERN = re.compile(r"[0-9A-Fa-f]{64}")
LOG = logging.getLogger("voice-catcher")


@dataclass(frozen=True)
class Hello:
    device: str
    nonce: int
    source: Literal["socket", "fixture"]
    playback: bool


def _unique_fields(pairs: list[tuple[str, object]]) -> dict:
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate field")
        value[key] = item
    return value


def _bad_constant(_: str) -> None:
    raise ValueError("nonfinite JSON")


def parse_hello(raw: bytes, token: bytes, *, allow_diagnostics: bool = False) -> Hello:
    """Return only non-secret metadata; never reflect malformed hello content."""
    try:
        if not 0 < len(raw) <= HANDSHAKE_BYTES or not raw.endswith(b"\n"):
            raise ValueError("invalid line")
        hello = json.loads(
            raw.decode("utf-8"), object_pairs_hook=_unique_fields, parse_constant=_bad_constant,
        )
        if not isinstance(hello, dict) or set(hello) != {
            "protocol", "device", "nonce", "source", "playback", "token",
        }:
            raise ValueError("invalid fields")
        if hello["protocol"] != "RIWAKE03":
            raise ValueError("invalid protocol")
        device, nonce, supplied = (
            hello["device"], hello["nonce"], hello["token"],
        )
        source, playback = hello["source"], hello["playback"]
        if not isinstance(device, str) or not DEVICE_PATTERN.fullmatch(device):
            raise ValueError("invalid device")
        if type(nonce) is not int or not 0 < nonce <= 0xFFFFFFFF:
            raise ValueError("invalid nonce")
        if source not in ("socket", "fixture"):
            raise ValueError("invalid source")
        if type(playback) is not bool:
            raise ValueError("invalid playback")
        if not isinstance(supplied, str) or not TOKEN_PATTERN.fullmatch(supplied):
            raise ValueError("invalid token")
        if len(token) != 32 or not hmac.compare_digest(bytes.fromhex(supplied), token):
            raise ValueError("invalid token")
        if not allow_diagnostics and (source != "socket" or not playback):
            raise ValueError("diagnostics not enabled")
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise ProtocolError("handshake rejected") from None
    return Hello(
        device=device, nonce=nonce, source=source, playback=playback,
    )


def authenticate(
    link: SocketIO, token: bytes, deadline: float, *, allow_diagnostics: bool = False,
) -> Hello:
    try:
        raw = bytearray()
        while len(raw) < HANDSHAKE_BYTES:
            octet = link.read_exact(1, deadline, "handshake")
            assert octet is not None
            raw.extend(octet)
            if octet == b"\n":
                break
        hello = parse_hello(bytes(raw), token, allow_diagnostics=allow_diagnostics)
        link.write_all(HANDSHAKE_OK, deadline, "handshake acknowledgment")
        return hello
    except ProtocolError:
        try:
            link.write_all(HANDSHAKE_FAILURE, time.monotonic() + 0.25, "handshake rejection")
        except ProtocolError:
            pass
        raise ProtocolError("handshake rejected") from None


def _private_file(fd: int) -> None:
    info = os.fstat(fd)
    if (
        not stat.S_ISREG(info.st_mode)
        or stat.S_IMODE(info.st_mode) != 0o600
        or info.st_uid != os.geteuid()
    ):
        raise ProtocolError("key/token must be an owned regular file with mode 0600")


def load_token(path: Path) -> bytes:
    fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as source:
        _private_file(source.fileno())
        raw = source.read(66)
    if not re.fullmatch(rb"[0-9A-Fa-f]{64}\n?", raw):
        raise ProtocolError("token file must contain 64 hex characters and an optional newline")
    return bytes.fromhex(raw.decode("ascii").strip())


def tls_context(cert: Path, key: Path) -> ssl.SSLContext:
    fd = os.open(key, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
    try:
        _private_file(fd)
    finally:
        os.close(fd)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(certfile=cert, keyfile=key, password="")
    return context


def _close(connection: socket.socket) -> None:
    try:
        connection.shutdown(socket.SHUT_RDWR)
    except OSError:
        pass
    connection.close()


class CatcherServer:
    """At most four sequential receivers; no queue or retained client history."""

    def __init__(
        self,
        address: tuple[str, int],
        *,
        context: ssl.SSLContext,
        token: bytes,
        reply_wav: bytes | ReplySource,
        stub_end_after_ms: int,
        io_timeout: float = 30,
        max_clients: int = 4,
        allow_diagnostics: bool = False,
    ) -> None:
        if len(token) != 32:
            raise ValueError("token must be 32 bytes")
        if not 1 <= max_clients <= 4:
            raise ValueError("client limit must be in 1..4")
        if not math.isfinite(io_timeout) or not 0 < io_timeout <= 30:
            raise ValueError("I/O timeout must be positive and <=30 seconds")
        validate_stub_endpoint(stub_end_after_ms)
        self.context = context
        self.token = token
        self.reply_wav = reply_source(reply_wav)
        self.stub_end_after_ms = stub_end_after_ms
        self.io_timeout = io_timeout
        self.max_clients = max_clients
        self.allow_diagnostics = allow_diagnostics
        self.stop = threading.Event()
        self._lock = threading.Lock()
        self._output_lock = threading.Lock()
        self._clients: dict[threading.Thread, socket.socket] = {}
        family = socket.AF_INET6 if ":" in address[0] else socket.AF_INET
        self.listener = socket.create_server(address, family=family, backlog=max_clients)
        self.listener.settimeout(0.25)
        self.address = self.listener.getsockname()

    def _emit_turn(self, device: str, entry: dict) -> None:
        line = json.dumps(
            {**entry, "device": device}, separators=(",", ":"), sort_keys=True, allow_nan=False,
        )
        if len(line.encode("utf-8")) + 1 > MAX_TURN_JSON_BYTES:
            raise ProtocolError("turn metadata exceeds JSONL byte limit")
        with self._output_lock:
            print(line, flush=True)

    def _client(self, connection: socket.socket, deadline: float) -> None:
        current = threading.current_thread()
        try:
            connection = self.context.wrap_socket(
                connection, server_side=True, do_handshake_on_connect=False,
            )
            with self._lock:
                self._clients[current] = connection
                if self.stop.is_set():
                    return
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ProtocolError("TLS handshake deadline expired")
            connection.settimeout(remaining)
            try:
                connection.do_handshake()
            except OSError:
                raise ProtocolError("TLS handshake failed") from None
            link = SocketIO(connection)
            hello = authenticate(
                link, self.token, deadline, allow_diagnostics=self.allow_diagnostics,
            )
            receiver = Receiver(
                nonce=hello.nonce, reply_wav=self.reply_wav, playback=hello.playback,
                source=hello.source, io_timeout=self.io_timeout,
                stub_end_after_ms=self.stub_end_after_ms,
                emit=lambda entry: self._emit_turn(hello.device, entry),
            )
            summary = receiver.run(link)
            statuses = summary["statuses"]
            LOG.info(
                "session closed: %d completed turns, %d cancelled",
                statuses["played"] + statuses["validated_only"], statuses["cancelled"],
            )
        except (ProtocolError, OSError) as error:
            if not self.stop.is_set():
                LOG.warning("client disconnected: %s", error)
        finally:
            _close(connection)
            with self._lock:
                self._clients.pop(current, None)

    def serve_forever(self) -> None:
        try:
            while not self.stop.is_set():
                try:
                    connection, _ = self.listener.accept()
                except TimeoutError:
                    continue
                except OSError:
                    if self.stop.is_set():
                        break
                    raise
                with self._lock:
                    if self.stop.is_set() or len(self._clients) >= self.max_clients:
                        _close(connection)
                        continue
                    thread = threading.Thread(
                        target=self._client,
                        args=(connection, time.monotonic() + HANDSHAKE_TIMEOUT),
                        name="voice-client",
                    )
                    self._clients[thread] = connection
                    try:
                        thread.start()
                    except BaseException:
                        self._clients.pop(thread, None)
                        _close(connection)
                        raise
        finally:
            self.close()

    def close(self) -> None:
        self.stop.set()
        self.listener.close()
        with self._lock:
            clients = list(self._clients.items())
        for _, connection in clients:
            _close(connection)
        for thread, _ in clients:
            if thread is not threading.current_thread():
                thread.join()


def create_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bind", required=True, help="explicit local listening address")
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--cert", required=True, type=Path)
    parser.add_argument("--key", required=True, type=Path)
    parser.add_argument("--token-file", required=True, type=Path)
    parser.add_argument("--reply-wav", required=True, type=Path)
    parser.add_argument(
        "--stub-end-after-ms", required=True, type=int,
        help="explicit simulated endpoint after 100..60000 ms of postwake samples; not VAD",
    )
    parser.add_argument("--io-timeout", type=float, default=30)
    parser.add_argument("--max-clients", type=int, default=4)
    parser.add_argument(
        "--allow-diagnostics", action="store_true",
        help="also accept fixture and/or validation-only sessions (disabled by default)",
    )
    return parser


def main() -> int:
    parser = create_parser()
    args = parser.parse_args()
    if not 1 <= args.port <= 65535:
        parser.error("--port must be in 1..65535")
    try:
        validate_stub_endpoint(args.stub_end_after_ms)
    except ValueError as error:
        parser.error(str(error))
    logging.basicConfig(level=logging.INFO, format="%(name)s: %(message)s")
    try:
        server = CatcherServer(
            (args.bind, args.port), context=tls_context(args.cert, args.key),
            token=load_token(args.token_file), reply_wav=load_reply_wav(args.reply_wav),
            stub_end_after_ms=args.stub_end_after_ms,
            io_timeout=args.io_timeout, max_clients=args.max_clients,
            allow_diagnostics=args.allow_diagnostics,
        )
        previous = signal.signal(signal.SIGTERM, lambda *_: server.stop.set())
        try:
            LOG.info("listening on %s:%d; fixed reply only, no audio recording", args.bind, args.port)
            server.serve_forever()
        finally:
            signal.signal(signal.SIGTERM, previous)
            server.close()
    except KeyboardInterrupt:
        return 0
    except (OSError, ProtocolError, ValueError) as error:
        LOG.error("%s", error)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
