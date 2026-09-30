"""Offline TLS/authentication tests; generated keys remain in private scratch.

Scratch stays below this project and is removed after tests, never in /tmp.
"""

from __future__ import annotations

import hashlib
import json
import logging
import shutil
import socket
import ssl
import stat
import struct
import subprocess
import sys
import threading
import time
import tracemalloc
import uuid
from pathlib import Path

import pytest

import catcher
import catcher_protocol as host
import init_host
from test_catcher_protocol import (
    CANDIDATE_ID, NONCE, PCM, POST_PCM, REPLY_WAV, STUB_MS, GeneratedIO, capture_turn,
    device_turn, read_control, read_peer, read_reply, receiver, request_header, send_chunk,
    send_fragments, start_turn,
)


@pytest.fixture(scope="module")
def scratch():
    root = Path(__file__).parent / ".voice-catcher-test-state"
    root.mkdir(mode=0o700, exist_ok=True)
    path = root / uuid.uuid4().hex
    path.mkdir(mode=0o700)
    try:
        yield path
    finally:
        shutil.rmtree(path)
        try:
            root.rmdir()
        except OSError:
            if root.exists() and not any(root.iterdir()):
                raise


@pytest.fixture(scope="module")
def identity(scratch):
    directory = scratch / "identity"
    pin = init_host.generate_identity(directory)
    return directory, pin, catcher.load_token(directory / "token")


@pytest.fixture
def server(identity, request):
    directory, _, token = identity
    target = catcher.CatcherServer(
        ("127.0.0.1", 0),
        context=catcher.tls_context(directory / "host-cert.pem", directory / "host-key.pem"),
        token=token, reply_wav=REPLY_WAV, stub_end_after_ms=STUB_MS,
        io_timeout=0.5, max_clients=2,
        allow_diagnostics=getattr(request, "param", False),
    )
    failures = []

    def serve():
        try:
            target.serve_forever()
        except BaseException as error:
            failures.append(error)

    thread = threading.Thread(target=serve)
    thread.start()
    try:
        yield target
    finally:
        target.close()
        thread.join(timeout=2)
        assert not thread.is_alive()
        assert not target._clients
        assert not failures


def connect(server, identity, *, tls12=False):
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    if tls12:
        context.maximum_version = ssl.TLSVersion.TLSv1_2
    raw = socket.create_connection(server.address, timeout=2)
    peer = context.wrap_socket(raw, server_hostname="pinned-host.invalid")
    assert hashlib.sha256(peer.getpeercert(binary_form=True)).hexdigest() == identity[1]
    return peer


def hello(token: bytes, /, **changes) -> bytes:
    fields = {
        "protocol": "RIWAKE03", "device": "reinvoke-test", "nonce": NONCE,
        "source": "socket", "playback": True, "token": token.hex(),
    }
    fields.update(changes)
    return json.dumps(fields, separators=(",", ":")).encode() + b"\n"


def read_line(peer: socket.socket) -> bytes:
    data = bytearray()
    while not data.endswith(b"\n"):
        block = peer.recv(1)
        assert block, "EOF before handshake reply"
        data.extend(block)
        assert len(data) <= 1024
    return bytes(data)


def await_closed(server):
    deadline = time.monotonic() + 2
    while time.monotonic() < deadline:
        with server._lock:
            if not server._clients:
                return
        time.sleep(0.01)
    pytest.fail("server did not release client slot")


def test_tls_fragmented_turns_and_cert_pin(server, identity, caplog):
    caplog.set_level(logging.INFO, logger=catcher.LOG.name)
    with connect(server, identity, tls12=True) as peer:
        assert peer.version() == "TLSv1.2"
        send_fragments(peer, hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        device_turn(peer, status=0, fragment_size=3)
        device_turn(peer, counter=2, status=0)
    await_closed(server)
    assert "session closed: 2 completed turns" in caplog.text
    assert identity[2].hex() not in caplog.text


def read_turn_record(capsys, token: bytes) -> dict:
    output = ""
    deadline = time.monotonic() + 2
    while time.monotonic() < deadline:
        output += capsys.readouterr().out
        if output.endswith("\n"):
            break
        time.sleep(0.01)
    assert output.endswith("\n"), "turn metadata was not flushed"
    lines = output.splitlines()
    assert len(lines) == 1
    assert len(output.encode("utf-8")) <= catcher.MAX_TURN_JSON_BYTES
    assert token.hex() not in output
    assert PCM.hex() not in output
    assert POST_PCM.hex() not in output
    assert REPLY_WAV.hex() not in output
    record = json.loads(lines[0])
    assert set(record) <= {
        "event", "device", "nonce", "candidate_id", "source", "playback", "playback_enabled",
        "audio_bytes", "wake_bytes", "pcm_sha256", "score", "threshold",
        "ok", "outcome", "status_code", "status", "reply_bytes", "reply_sent",
        "expected_reply_fnv1a", "reply_fnv1a", "error",
        "postwake_bytes", "audio_frames", "capture_complete", "followup",
        "endpoint_reason", "endpoint_postwake_bytes", "stub_end_after_ms", "turn_end_sent",
    }
    return record


@pytest.mark.parametrize("server,source,playback", [
    (False, "socket", True), (True, "fixture", False),
], indirect=["server"])
def test_one_turn_jsonl_has_verified_metadata_without_secrets_or_audio(
    server, identity, capsys, source, playback,
):
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2], source=source, playback=playback))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        device_turn(peer, flags=int(source == "fixture"), status=0 if playback else 8)
        record = read_turn_record(capsys, identity[2])
        with server._lock:
            assert len(server._clients) == 1
    await_closed(server)
    assert record["event"] == "turn"
    assert record["device"] == "reinvoke-test"
    assert record["nonce"] == NONCE
    assert record["candidate_id"] == CANDIDATE_ID
    assert record["source"] == source
    assert record["playback"] is playback
    assert record["audio_bytes"] == len(PCM) + len(POST_PCM)
    assert record["wake_bytes"] == len(PCM)
    assert record["postwake_bytes"] == record["endpoint_postwake_bytes"] == len(POST_PCM)
    assert record["audio_frames"] == 1
    assert record["capture_complete"] and record["turn_end_sent"]
    assert not record["followup"]
    assert record["stub_end_after_ms"] == STUB_MS
    assert record["endpoint_reason"] == "stub_sample_limit"
    assert record["pcm_sha256"] == hashlib.sha256(PCM + POST_PCM).hexdigest()
    assert record["score"] == pytest.approx(0.87)
    assert record["threshold"] == pytest.approx(0.42)
    assert record["outcome"] == "completed" and record["ok"] is True
    assert record["status_code"] == (0 if playback else 8)
    assert record["status"] == ("played" if playback else "validated_only")
    assert record["reply_bytes"] == len(REPLY_WAV) and record["reply_sent"] is True
    assert record["expected_reply_fnv1a"] == record["reply_fnv1a"] == host.fnv1a(REPLY_WAV)


@pytest.mark.parametrize("failure", ["receipt", "pcm", "header"])
def test_failed_turn_jsonl_is_explicit_and_does_not_reflect_payload(
    server, identity, capsys, failure,
):
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        if failure == "receipt":
            device_turn(peer, status=0, reply_hash=host.fnv1a(REPLY_WAV) ^ 1)
        elif failure == "pcm":
            peer.sendall(request_header() + PCM[:2])
            read_control(peer, "speech.startDetected")
        else:
            peer.sendall(identity[2].hex().encode("ascii"))
        assert peer.recv(1) == b""
    await_closed(server)
    record = read_turn_record(capsys, identity[2])
    assert record["outcome"] == "failed" and record["ok"] is False
    assert record["nonce"] == NONCE
    if failure == "receipt":
        assert record["candidate_id"] == CANDIDATE_ID
        assert record["pcm_sha256"] == hashlib.sha256(PCM + POST_PCM).hexdigest()
        assert record["status_code"] == 0 and record["reply_sent"] is True
        assert record["reply_fnv1a"] == record["expected_reply_fnv1a"] ^ 1
        assert record["error"] == "reply WAV hash mismatch"
    else:
        assert record["status_code"] is record["reply_fnv1a"] is None
        assert record["reply_sent"] is False
        assert not record["capture_complete"]
        if failure == "pcm":
            assert record["candidate_id"] == CANDIDATE_ID
            assert record["wake_bytes"] == len(PCM) and record["audio_bytes"] == 0
            assert record["pcm_sha256"] == hashlib.sha256(b"").hexdigest()
            assert "wake PCM" in record["error"]
        else:
            assert record["candidate_id"] is record["pcm_sha256"] is record["score"] is None
            assert record["audio_bytes"] == 0
            assert record["error"] == "wrong frame magic"


@pytest.mark.parametrize("source,playback", [
    ("socket", False), ("fixture", True), ("fixture", False),
])
def test_default_listener_rejects_diagnostic_modes(server, identity, source, playback):
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2], source=source, playback=playback))
        assert read_line(peer) == catcher.HANDSHAKE_FAILURE
        assert peer.recv(1) == b""


@pytest.mark.parametrize("server", [True], indirect=True)
@pytest.mark.parametrize("source,playback", [
    ("socket", True), ("socket", False), ("fixture", True), ("fixture", False),
])
def test_diagnostics_opt_in_validates_fragmented_turns(
    server, identity, caplog, source, playback,
):
    caplog.set_level(logging.INFO, logger=catcher.LOG.name)
    with connect(server, identity) as peer:
        send_fragments(peer, hello(identity[2], source=source, playback=playback))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        device_turn(peer, flags=int(source == "fixture"), status=0 if playback else 8)
    await_closed(server)
    assert "session closed: 1 completed turns" in caplog.text
    assert "client disconnected" not in caplog.text


@pytest.mark.parametrize("server", [True], indirect=True)
@pytest.mark.parametrize("source,flags", [("socket", 1), ("fixture", 0)])
def test_diagnostic_request_source_must_match_hello(server, identity, caplog, source, flags):
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2], source=source, playback=False))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        peer.sendall(request_header(flags=flags))
        assert peer.recv(1) == b""
    await_closed(server)
    assert "request source flag mismatch" in caplog.text


@pytest.mark.parametrize("server", [True], indirect=True)
@pytest.mark.parametrize("playback,status", [(False, 0), (True, 8)])
def test_diagnostic_receipt_must_match_playback_hello(server, identity, caplog, playback, status):
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2], source="fixture", playback=playback))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        device_turn(peer, flags=1, status=status)
        assert peer.recv(1) == b""
    await_closed(server)
    assert f"unexpected unit status {status}; expected {0 if playback else 8}" in caplog.text


def test_allow_diagnostics_cli_requires_explicit_opt_in():
    required = [
        "--bind", "127.0.0.1", "--port", "8444", "--cert", "cert.pem", "--key", "key.pem",
        "--token-file", "token", "--reply-wav", "approved.wav",
    ]
    parser = catcher.create_parser()
    with pytest.raises(SystemExit):
        parser.parse_args(required)
    required.extend(["--stub-end-after-ms", "8000"])
    assert parser.parse_args(required).allow_diagnostics is False
    assert parser.parse_args(required).stub_end_after_ms == 8000
    assert parser.parse_args([*required, "--allow-diagnostics"]).allow_diagnostics is True


def test_token_rejection_is_generic_and_later_valid_client_works(server, identity, caplog):
    token = bytes(value ^ 0xFF for value in identity[2])
    raw = hello(token, device="never-reflect-this-device")
    with connect(server, identity) as peer:
        peer.sendall(raw)
        assert read_line(peer) == catcher.HANDSHAKE_FAILURE
        assert peer.recv(1) == b""
    await_closed(server)
    assert "handshake rejected" in caplog.text
    assert token.hex() not in caplog.text
    assert identity[2].hex() not in caplog.text
    assert "never-reflect-this-device" not in caplog.text
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        device_turn(peer, status=0)
    await_closed(server)


@pytest.mark.parametrize("changes", [
    {"protocol": "RIWAKE02"}, {"device": ""}, {"device": "x" * 65},
    {"device": "bad/device"}, {"device": "é"}, {"device": None},
    {"nonce": 0}, {"nonce": 2**32}, {"nonce": -1}, {"nonce": True}, {"nonce": 1.0},
    {"post_ms": 5000},
    {"source": ""}, {"source": "other"}, {"source": None}, {"source": []},
    {"playback": 0}, {"playback": 1}, {"playback": "false"}, {"playback": None},
    {"token": "f" * 63}, {"token": "g" * 64}, {"token": None}, {"extra": 1},
])
def test_malformed_hello_fields_rejected_without_reflection(identity, changes):
    with pytest.raises(host.ProtocolError, match="^handshake rejected$"):
        catcher.parse_hello(hello(identity[2], **changes), identity[2])


@pytest.mark.parametrize("field", ["source", "playback"])
def test_source_and_playback_are_required_even_with_diagnostics(identity, field):
    fields = json.loads(hello(identity[2]))
    del fields[field]
    with pytest.raises(host.ProtocolError, match="^handshake rejected$"):
        catcher.parse_hello(
            json.dumps(fields).encode() + b"\n", identity[2], allow_diagnostics=True,
        )


def test_hello_json_types_duplicates_and_byte_bound(identity):
    token = identity[2]
    raw = hello(token)
    padded = raw[:-1] + b" " * (catcher.HANDSHAKE_BYTES - len(raw)) + b"\n"
    assert len(padded) == 1024
    assert catcher.parse_hello(padded, token).nonce == NONCE
    assert catcher.parse_hello(hello(token, device="A._-09"), token).device == "A._-09"
    for malformed in (
        b"[]\n", b"null\n", b"{broken\n", b"\xff\n", raw[:-1],
        raw[:-2] + b',"nonce":1}\n', padded + b"\n",
        raw.replace(str(NONCE).encode("ascii"), b"NaN"),
    ):
        with pytest.raises(host.ProtocolError, match="^handshake rejected$"):
            catcher.parse_hello(malformed, token)


@pytest.mark.parametrize("raw", [b'{"token":"do-not-reflect"\n', b"x" * 1025 + b"\n"])
def test_bad_handshake_on_tls_is_explicit_and_disconnects(server, identity, raw, caplog):
    with connect(server, identity) as peer:
        peer.sendall(raw)
        assert read_line(peer) == catcher.HANDSHAKE_FAILURE
        assert peer.recv(1) == b""
    assert "do-not-reflect" not in caplog.text


def test_handshake_timeout_is_bounded_and_not_success():
    assert catcher.HANDSHAKE_TIMEOUT == 5.0
    left, right = socket.socketpair()
    with left, right:
        with pytest.raises(host.ProtocolError, match="^handshake rejected$"):
            catcher.authenticate(host.SocketIO(left), b"a" * 32, time.monotonic() + 0.03)
        right.settimeout(1)
        assert read_line(right) == catcher.HANDSHAKE_FAILURE


def test_hello_and_frame_in_one_tls_record_are_not_lost(server, identity):
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]) + request_header() + PCM)
        assert read_line(peer) == catcher.HANDSHAKE_OK
        read_control(peer, "speech.startDetected")
        send_chunk(peer, 1, POST_PCM, fragment_size=3)
        read_control(peer, "speech.endDetected")
        send_chunk(peer, 2, b"", fragment_size=3)
        read_reply(peer)
        send_fragments(peer, host.RESULT.pack(
            host.RESULT_MAGIC, CANDIDATE_ID, 0, host.fnv1a(REPLY_WAV),
        ))
    await_closed(server)


def test_real_tls_superseded_response_does_not_disconnect_session(server, identity, caplog, capsys):
    caplog.set_level(logging.INFO, logger=catcher.LOG.name)
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        capture_turn(peer)
        send_fragments(peer, host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 2, 0))
        device_turn(peer, counter=2, status=0)
    await_closed(server)
    records = [json.loads(line) for line in capsys.readouterr().out.splitlines()]
    assert [record["outcome"] for record in records] == ["cancelled", "completed"]
    assert "session closed: 1 completed turns, 1 cancelled" in caplog.text


def test_bad_frame_is_disconnected_and_next_client_can_authenticate(server, identity, caplog):
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        peer.sendall(request_header(audio_bytes=0xFFFFFFFF))
        assert peer.recv(1) == b""
    await_closed(server)
    assert "audio length" in caplog.text
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2], nonce=NONCE + 1))
        assert read_line(peer) == catcher.HANDSHAKE_OK


def test_client_limit_and_shutdown_close_idle_tls_and_handshaking_sockets(server, identity):
    with connect(server, identity) as idle, socket.create_connection(server.address, timeout=2) as raw:
        idle.sendall(hello(identity[2]))
        assert read_line(idle) == catcher.HANDSHAKE_OK
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline:
            with server._lock:
                if len(server._clients) == 2:
                    break
            time.sleep(0.01)
        else:
            pytest.fail("second client was not admitted")
        with socket.create_connection(server.address, timeout=2) as excess:
            assert excess.recv(1) == b""
        server.close()
        assert idle.recv(1) == b""
        assert raw.recv(1) == b""
        assert not server._clients


@pytest.mark.parametrize("with_device", [False, True])
def test_private_setup_is_exclusive_and_prints_only_pin(scratch, with_device):
    directory = scratch / f"cli-identity-{with_device}"
    command = [sys.executable, str(Path(init_host.__file__)), "--directory", str(directory)]
    expected_files = {"host-key.pem", "host-cert.pem", "token"}
    if with_device:
        command.extend(["--endpoint", "reinvoke-host:8444", "--device", "reinvoke-test"])
        expected_files.add("device.json")
    result = subprocess.run(command, check=True, capture_output=True, text=True, timeout=35)
    assert stat.S_IMODE(directory.stat().st_mode) == 0o700
    assert {path.name for path in directory.iterdir()} == expected_files
    before = {path.name: path.read_bytes() for path in directory.iterdir()}
    assert all(stat.S_IMODE(path.stat().st_mode) == 0o600 for path in directory.iterdir())
    cert = ssl.PEM_cert_to_DER_cert((directory / "host-cert.pem").read_text())
    assert result.stdout == hashlib.sha256(cert).hexdigest() + "\n"
    assert result.stderr == ""
    assert len(catcher.load_token(directory / "token")) == 32
    if with_device:
        assert json.loads(before["device.json"]) == {
            "endpoint": "reinvoke-host:8444",
            "certificate_sha256": hashlib.sha256(cert).hexdigest(),
            "token": before["token"].strip().decode("ascii"),
            "device": "reinvoke-test", "io_timeout_ms": 30000,
        }
    assert before["token"].strip().decode("ascii") not in result.stdout
    retry = subprocess.run(command, capture_output=True, text=True, timeout=5)
    assert retry.returncode != 0 and retry.stdout == ""
    assert {path.name: path.read_bytes() for path in directory.iterdir()} == before
    assert before["token"].strip().decode() not in retry.stderr


@pytest.mark.parametrize("endpoint,device", [
    (None, "reinvoke-test"), ("reinvoke-host:8444", None),
    ("127.0.0.1:8444", "reinvoke-test"), ("reinvoke-host:0", "reinvoke-test"),
    ("reinvoke-host:65536", "reinvoke-test"), ("bad_host:8444", "reinvoke-test"),
    ("reinvoke-host:8444", "bad/name"), ("reinvoke-host:8444", "x" * 65),
    pytest.param("reinvoke-host:" + "0" * 4000 + "8444", "reinvoke-test", id="oversized-port"),
])
def test_device_setup_rejects_invalid_options_before_creating_files(scratch, endpoint, device):
    directory = scratch / "invalid-options"
    with pytest.raises(ValueError):
        init_host.generate_identity(directory, endpoint=endpoint, device=device)
    assert not directory.exists()


def test_token_permissions_format_and_symlink_rejected(scratch, identity):
    path = scratch / "bad-token"
    path.write_text(identity[2].hex() + "\n")
    path.chmod(0o644)
    with pytest.raises(host.ProtocolError, match="0600"):
        catcher.load_token(path)
    path.chmod(0o600)
    for value in ("", "a" * 63, "z" * 64, "a" * 64 + "\n\n"):
        path.write_text(value)
        with pytest.raises(host.ProtocolError, match="64 hex"):
            catcher.load_token(path)
    link = scratch / "token-symlink"
    link.symlink_to(identity[0] / "token")
    with pytest.raises(OSError):
        catcher.load_token(link)


def test_reply_loader_is_bounded_and_rejects_symlinks(scratch):
    path = scratch / "reply.wav"
    path.write_bytes(REPLY_WAV)
    reply = host.load_reply_wav(path)
    assert reply.data is None and b"".join(reply.chunks()) == REPLY_WAV
    assert reply.fnv == host.fnv1a(REPLY_WAV)
    assert reply.sha256 == hashlib.sha256(REPLY_WAV).hexdigest()
    link = scratch / "reply-link.wav"
    link.symlink_to(path)
    with pytest.raises(OSError):
        host.load_reply_wav(link)
    with path.open("wb") as source:
        source.truncate(host.MAX_REPLY_BYTES + 1)
    with pytest.raises(host.ProtocolError, match="length"):
        host.load_reply_wav(path)


def write_long_reply(path: Path, seconds: int) -> str:
    pcm_bytes = seconds * 32000
    header = host.WAV_HEADER.pack(
        b"RIFF", pcm_bytes + 36, b"WAVE", b"fmt ", 16, 1, 1,
        16000, 32000, 2, 16, b"data", pcm_bytes,
    )
    block = b"\xff\x7f\0\x80" * 8000
    digest = hashlib.sha256(header)
    with path.open("wb") as output:
        output.write(header)
        for _ in range(seconds):
            output.write(block)
            digest.update(block)
    return digest.hexdigest()


@pytest.mark.parametrize("seconds", [75, 90])
def test_long_reply_validation_and_transmission_are_file_backed_and_constant_memory(scratch, seconds):
    path = scratch / f"long-{seconds}.wav"
    expected_sha = write_long_reply(path, seconds)
    entries = []
    tracemalloc.start()
    try:
        reply = host.load_reply_wav(path)
        target = receiver(reply_wav=reply, playback=True, emit=entries.append)
        link = GeneratedIO(1, reply_wav=reply, status=0)
        assert target.run(link)["ok"]
        _, peak = tracemalloc.get_traced_memory()
    finally:
        tracemalloc.stop()
    assert reply.data is None
    assert reply.duration_seconds == seconds
    assert peak < 512 * 1024, f"reply retained duration-sized memory: {peak}"
    assert link.largest_write == host.REPLY_CHUNK_BYTES
    assert link.reply_bytes == reply.length == seconds * 32000 + 44
    assert link.reply_sha256.hexdigest() == reply.sha256 == expected_sha
    assert seconds + 5 < link.receipt_budget <= seconds + 5.5
    assert entries[0]["reply_bytes"] == reply.length
    assert entries[0]["reply_fnv1a"] == entries[0]["expected_reply_fnv1a"] == reply.fnv


def test_changed_file_is_rejected_before_or_during_streaming(scratch):
    path = scratch / "changed-reply.wav"
    write_long_reply(path, 3)
    reply = host.load_reply_wav(path)
    iterator = reply.chunks()
    assert len(next(iterator)) == host.REPLY_CHUNK_BYTES
    with path.open("r+b") as changed:
        changed.seek(44)
        changed.write(b"\0\0")
    with pytest.raises(host.ProtocolError, match="changed during transmission"):
        list(iterator)
    with pytest.raises(host.ProtocolError, match="changed after validation"):
        next(reply.chunks())
    path.write_bytes(REPLY_WAV[:-1])
    with pytest.raises(host.ProtocolError):
        host.load_reply_wav(path)


def test_long_file_streams_over_slow_segmented_tls_and_waits_for_receipt(
    server, identity, capsys, scratch,
):
    path = scratch / "long-tls.wav"
    expected_sha = write_long_reply(path, 75)
    server.reply_wav = host.load_reply_wav(path)
    server.io_timeout = 0.2
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        with server._lock:
            for connection in server._clients.values():
                connection.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
        start_turn(peer)
        send_chunk(peer, 1, POST_PCM, fragment_size=7)
        read_control(peer, "speech.endDetected")
        send_chunk(peer, 2, b"")
        magic, ident, verdict, remaining = host.REPLY.unpack(read_peer(peer, host.REPLY.size))
        assert (magic, ident, verdict, remaining) == (
            host.REPLY_MAGIC, CANDIDATE_ID, 0, 75 * 32000 + 44,
        )
        digest, value = hashlib.sha256(), 0x811C9DC5
        started = time.monotonic()
        while remaining:
            block = peer.recv(min(4096, remaining))
            assert block
            remaining -= len(block)
            digest.update(block)
            value = host.fnv1a(block, value)
            time.sleep(0.001)
        assert time.monotonic() - started > server.io_timeout
        assert digest.hexdigest() == expected_sha
        read_control(peer, "turn.end")
        time.sleep(0.3)
        assert capsys.readouterr().out == "", "host invented playback completion before receipt"
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 0, value))
        record = read_turn_record(capsys, identity[2])
        assert record["ok"] and record["reply_sent"]
        assert record["reply_bytes"] == 75 * 32000 + 44
        assert record["reply_fnv1a"] == value == record["expected_reply_fnv1a"]
    await_closed(server)


def test_tls_send_failure_never_reports_played_or_complete_reply(server, identity, capsys, scratch):
    path = scratch / "failed-send.wav"
    write_long_reply(path, 90)
    server.reply_wav = host.load_reply_wav(path)
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        with server._lock:
            for connection in server._clients.values():
                connection.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
        start_turn(peer)
        send_chunk(peer, 1, POST_PCM)
        read_control(peer, "speech.endDetected")
        send_chunk(peer, 2, b"")
        assert read_peer(peer, 8) == host.REPLY_MAGIC
        peer.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
    await_closed(server)
    record = read_turn_record(capsys, identity[2])
    assert not record["ok"] and record["outcome"] == "failed"
    assert not record["reply_sent"] and not record["turn_end_sent"]
    assert record["status_code"] is None and record["status"] is None
    assert "reply WAV" in record["error"]


def test_tls_local_cancel_before_backend_endpoint_can_start_new_empty_wake(server, identity, capsys):
    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        start_turn(peer, flags=4)
        send_chunk(peer, 1, b"\0\0")
        send_fragments(peer, host.AUDIO.pack(host.AUDIO_MAGIC, CANDIDATE_ID, 2, 0), 3)
        send_fragments(peer, host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 2, 0), 3)
        record = read_turn_record(capsys, identity[2])
        assert record["outcome"] == "cancelled" and not record["reply_sent"]
        assert record["endpoint_reason"] is None
        device_turn(peer, counter=2, flags=4, status=0)
        assert read_turn_record(capsys, identity[2])["ok"]
    await_closed(server)


def test_tls_cancel_during_reply_drains_old_length_before_new_turn_control(
    server, identity, capsys, scratch,
):
    path = scratch / "superseded-tls.wav"
    expected_sha = write_long_reply(path, 75)
    server.reply_wav = host.load_reply_wav(path)

    def drain_body(peer, length):
        digest = hashlib.sha256()
        while length:
            block = peer.recv(min(length, 8192))
            assert block
            digest.update(block)
            length -= len(block)
        assert digest.hexdigest() == expected_sha

    with connect(server, identity) as peer:
        peer.sendall(hello(identity[2]))
        assert read_line(peer) == catcher.HANDSHAKE_OK
        with server._lock:
            for connection in server._clients.values():
                connection.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
        start_turn(peer)
        send_chunk(peer, 1, POST_PCM)
        read_control(peer, "speech.endDetected")
        send_chunk(peer, 2, b"")
        header = host.REPLY.unpack(read_peer(peer, host.REPLY.size))
        assert header[:3] == (host.REPLY_MAGIC, CANDIDATE_ID, 0)
        new_id = CANDIDATE_ID + 1
        peer.sendall(
            host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 2, 0)
            + request_header(
                candidate_id=new_id, monotonic_ms=124000, flags=4,
                audio_bytes=0, wake_bytes=0, confidence=0, threshold=0,
                start_ms=0, duration_ms=0,
            )
        )
        drain_body(peer, header[3])
        read_control(peer, "turn.end")
        record = read_turn_record(capsys, identity[2])
        assert record["outcome"] == "cancelled" and not record["ok"]
        read_control(peer, "speech.startDetected", new_id)
        send_chunk(peer, 1, POST_PCM, candidate_id=new_id)
        read_control(peer, "speech.endDetected", new_id)
        send_chunk(peer, 2, b"", candidate_id=new_id)
        header = host.REPLY.unpack(read_peer(peer, host.REPLY.size))
        assert header[:3] == (host.REPLY_MAGIC, new_id, 0)
        drain_body(peer, header[3])
        read_control(peer, "turn.end", new_id)
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, new_id, 0, server.reply_wav.fnv))
        record = read_turn_record(capsys, identity[2])
        assert record["ok"] and record["candidate_id"] == new_id
    await_closed(server)
