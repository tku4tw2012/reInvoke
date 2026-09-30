"""Offline RIWAKE03 streaming controls; synthetic PCM only, never playback."""

from __future__ import annotations

import hashlib
import select
import socket
import struct
import threading
import time
import tracemalloc

import pytest

import catcher_protocol as host

NONCE = 0x1234ABCD
CANDIDATE_ID = (NONCE << 32) | 1
PCM = b"\x01\x00" * 32
POST_PCM = b"\x25\x00\xdb\xff" * 800
STUB_MS = 100
REPLY_WAV = host.pcm_wav(b"\x64\x00" * 80)
OTHER_REPLY_WAV = host.pcm_wav(b"\x2c\x01\xd4\xfe" * 127)


def request_header(**changes) -> bytes:
    fields = {
        "magic": host.REQUEST_MAGIC, "version": 3, "flags": 0,
        "candidate_id": CANDIDATE_ID, "monotonic_ms": 123_000,
        "confidence": 0.87, "threshold": 0.42, "start_ms": 0,
        "duration_ms": 2, "sample_rate": 16000, "audio_format": 1,
        "channels": 1, "audio_bytes": len(PCM), "wake_bytes": len(PCM),
    }
    fields.update(changes)
    return host.REQUEST.pack(*fields.values())


def receiver(**changes) -> host.Receiver:
    options = {
        "nonce": NONCE, "reply_wav": REPLY_WAV, "io_timeout": 0.5,
        "source": "socket", "stub_end_after_ms": STUB_MS,
    }
    options.update(changes)
    return host.Receiver(**options)


def read_peer(peer: socket.socket, count: int) -> bytes:
    data = bytearray()
    while len(data) < count:
        block = peer.recv(min(65536, count - len(data)))
        assert block, f"peer EOF after {len(data)}/{count} bytes"
        data.extend(block)
    return bytes(data)


def send_fragments(peer: socket.socket, data: bytes, size: int = 3) -> None:
    for offset in range(0, len(data), size):
        peer.sendall(data[offset:offset + size])


def read_control(peer: socket.socket, code: str, candidate_id: int = CANDIDATE_ID) -> None:
    while True:
        magic, identity, value, reserved = host.CONTROL.unpack(read_peer(peer, host.CONTROL.size))
        assert (magic, identity, reserved) == (host.CONTROL_MAGIC, candidate_id, 0)
        if value == host.CONTROL_CODES["speech-progress"] and code != "speech-progress":
            continue
        assert value == host.CONTROL_CODES[code]
        return


def start_turn(
    peer: socket.socket, *, counter: int = 1, flags: int = 0, fragment_size: int = 65536,
) -> int:
    candidate_id = (NONCE << 32) | counter
    changes = {
        "candidate_id": candidate_id, "monotonic_ms": 123000 + counter, "flags": flags,
    }
    if flags & 4:
        changes.update(
            audio_bytes=0, wake_bytes=0, confidence=0, threshold=0, start_ms=0, duration_ms=0,
        )
    send_fragments(peer, request_header(**changes) + (b"" if flags & 4 else PCM), fragment_size)
    read_control(peer, "speech.startDetected", candidate_id)
    return candidate_id


def send_chunk(
    peer: socket.socket, sequence: int, pcm: bytes, *,
    candidate_id: int = CANDIDATE_ID, fragment_size: int = 65536,
) -> None:
    send_fragments(
        peer, host.AUDIO.pack(host.AUDIO_MAGIC, candidate_id, sequence, len(pcm)) + pcm,
        fragment_size,
    )


def read_reply(
    peer: socket.socket, candidate_id: int = CANDIDATE_ID, reply_wav: bytes = REPLY_WAV,
) -> None:
    magic, received_id, verdict, length = host.REPLY.unpack(read_peer(peer, host.REPLY.size))
    assert (magic, received_id, verdict, length) == (
        host.REPLY_MAGIC, candidate_id, 0, len(reply_wav),
    )
    assert read_peer(peer, length) == reply_wav
    # The backend closes the turn before the device finishes (or validates)
    # playback. A receipt cannot be used to trigger sending turn.end.
    read_control(peer, "turn.end", candidate_id)


def capture_turn(
    peer: socket.socket, *, counter: int = 1, flags: int = 0,
    endpoint_ms: int = STUB_MS, post_ms: int | None = None,
    reply_wav: bytes = REPLY_WAV, fragment_size: int = 65536, inflight_chunks: int = 0,
) -> int:
    candidate_id = start_turn(peer, counter=counter, flags=flags, fragment_size=fragment_size)
    remaining = (endpoint_ms if post_ms is None else post_ms) * host.BYTES_PER_MS
    before_endpoint = remaining < endpoint_ms * host.BYTES_PER_MS
    sequence = 1
    while remaining:
        pcm = POST_PCM[:min(remaining, len(POST_PCM))]
        send_chunk(peer, sequence, pcm, candidate_id=candidate_id, fragment_size=fragment_size)
        sequence += 1
        remaining -= len(pcm)
    if not before_endpoint:
        read_control(peer, "speech.endDetected", candidate_id)
    for _ in range(inflight_chunks):
        send_chunk(peer, sequence, POST_PCM, candidate_id=candidate_id)
        sequence += 1
    send_chunk(peer, sequence, b"", candidate_id=candidate_id, fragment_size=fragment_size)
    if before_endpoint:
        read_control(peer, "speech.endDetected", candidate_id)
    read_reply(peer, candidate_id, reply_wav)
    return candidate_id


def device_turn(
    peer: socket.socket, *, counter: int = 1, status: int = 8,
    reply_hash: int | None = None, result_id: int | None = None,
    result_magic: bytes = host.RESULT_MAGIC, fragment_size: int = 65536, flags: int = 0,
    endpoint_ms: int = STUB_MS, post_ms: int | None = None, reply_wav: bytes = REPLY_WAV,
    inflight_chunks: int = 0,
) -> None:
    candidate_id = capture_turn(
        peer, counter=counter, flags=flags, endpoint_ms=endpoint_ms, post_ms=post_ms,
        reply_wav=reply_wav, fragment_size=fragment_size, inflight_chunks=inflight_chunks,
    )
    send_fragments(peer, host.RESULT.pack(
        result_magic, candidate_id if result_id is None else result_id, status,
        host.fnv1a(reply_wav) if reply_hash is None else reply_hash,
    ), fragment_size)


def exchange(target: host.Receiver, device) -> dict:
    left, right = socket.socketpair()
    right.settimeout(3)
    failures: list[BaseException] = []

    def run_device() -> None:
        try:
            with right:
                device(right)
        except BaseException as error:
            failures.append(error)

    thread = threading.Thread(target=run_device)
    thread.start()
    try:
        with left:
            return target.run(host.SocketIO(left))
    finally:
        thread.join(timeout=4)
        assert not thread.is_alive(), "fake unit remained blocked"
        if failures:
            raise failures[0]


def run_packet(payload: bytes) -> dict:
    assert len(payload) < 4096
    left, right = socket.socketpair()
    with left, right:
        right.sendall(payload)
        right.shutdown(socket.SHUT_WR)
        return receiver().run(host.SocketIO(left))


def test_struct_sizes_magics_fnv_and_rounding_contract():
    assert host.REQUEST.size == 64
    assert host.REPLY.size == host.RESULT.size == host.AUDIO.size == host.CONTROL.size == 24
    assert host.WAV_HEADER.size == 44
    assert host.REQUEST_MAGIC == b"RIWAKE03" and host.REPLY_MAGIC == b"RIREPLY3"
    assert host.AUDIO_MAGIC == b"RIAUDIO3" and host.CONTROL_MAGIC == b"RICTRL03"
    assert host.fnv1a(b"hello") == 0x4F9F2CAB
    assert list(host.CONTROL_CODES.values()) == list(range(1, 7))
    parsed = host.parse_request(request_header(duration_ms=3), nonce=NONCE, counter=1)
    assert parsed.audio_bytes == parsed.wake_bytes == len(PCM)
    host.parse_request(request_header(flags=2), nonce=NONCE, counter=1)
    host.parse_request(
        request_header(audio_bytes=160000, wake_bytes=160000, duration_ms=5000),
        nonce=NONCE, counter=1,
    )


@pytest.mark.parametrize("changes,reason", [
    ({"magic": b"RIWAKE02"}, "magic"), ({"version": 2}, "version"),
    ({"flags": 8}, "flags"), ({"flags": 1}, "source flag"),
    ({"candidate_id": CANDIDATE_ID - 1}, "identity/order"),
    ({"candidate_id": ((NONCE + 1) << 32) | 1}, "identity/order"),
    ({"candidate_id": CANDIDATE_ID + 1}, "identity/order"),
    ({"monotonic_ms": 0}, "monotonic"),
    ({"monotonic_ms": 1_800_000_000_000}, "epoch"),
    ({"confidence": float("nan")}, "confidence"),
    ({"confidence": float("inf")}, "confidence"),
    ({"confidence": -0.1}, "confidence"), ({"confidence": 1.1}, "confidence"),
    ({"threshold": float("nan")}, "threshold"),
    ({"threshold": float("-inf")}, "threshold"),
    ({"threshold": -0.1}, "threshold"), ({"threshold": 1.1}, "threshold"),
    ({"sample_rate": 48000}, "format"), ({"audio_format": 2}, "format"),
    ({"channels": 2}, "format"),
    ({"audio_bytes": 0}, "audio length"), ({"audio_bytes": 63}, "audio length"),
    ({"audio_bytes": 0xFFFFFFFF}, "audio length"),
    ({"wake_bytes": 0}, "wake length"), ({"wake_bytes": 63}, "wake length"),
    ({"wake_bytes": 66}, "audio length"),
    ({"audio_bytes": 160002, "wake_bytes": 160002}, "wake length"),
    ({"start_ms": -1}, "nonnegative"),
    ({"flags": 2, "start_ms": 1, "duration_ms": 1}, "normalized"),
    ({"duration_ms": 0}, "duration"), ({"duration_ms": 4}, "wake window"),
    ({"duration_ms": 0xFFFFFFFF}, "wake window"),
    ({"start_ms": 0x7FFFFFFF}, "wake window"),
])
def test_malformed_headers_refused_before_body(changes, reason):
    with pytest.raises(host.ProtocolError, match=reason):
        run_packet(request_header(**changes))


@pytest.mark.parametrize("field", [
    "audio_bytes", "wake_bytes", "confidence", "threshold", "start_ms", "duration_ms",
])
def test_followup_requires_zero_pcm_and_measurements(field):
    changes = dict(
        flags=4, audio_bytes=0, wake_bytes=0, confidence=0, threshold=0, start_ms=0, duration_ms=0,
    )
    changes[field] = 1
    with pytest.raises(host.ProtocolError, match="followup"):
        host.parse_request(request_header(**changes), nonce=NONCE, counter=1)


@pytest.mark.parametrize("flags", [4, 5])
def test_followup_stream_has_no_initial_pcm(flags):
    entries = []
    exchange(
        receiver(source="fixture" if flags & 1 else "socket", emit=entries.append),
        lambda peer: device_turn(peer, flags=flags),
    )
    assert entries[0]["followup"] is True and entries[0]["wake_bytes"] == 0
    assert entries[0]["pcm_sha256"] == hashlib.sha256(POST_PCM).hexdigest()


@pytest.mark.parametrize("endpoint_ms", [2000, 8000, 12000])
def test_sample_endpoint_varies_past_five_seconds_and_drains_inflight_audio(endpoint_ms):
    entries = []
    target = receiver(stub_end_after_ms=endpoint_ms, emit=entries.append, io_timeout=2)

    def device(peer):
        start_turn(peer)
        frames = endpoint_ms // 100
        for sequence in range(1, frames + 1):
            send_chunk(peer, sequence, POST_PCM)
            if sequence == 50 and endpoint_ms > 5000:
                assert not select.select([peer], [], [], 0.01)[0], "backend stopped at five seconds"
        read_control(peer, "speech.endDetected")
        send_chunk(peer, frames + 1, POST_PCM)
        send_chunk(peer, frames + 2, POST_PCM)
        assert not select.select([peer], [], [], 0.01)[0], "reply preceded stream end marker"
        send_chunk(peer, frames + 3, b"")
        read_reply(peer)
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 8, host.fnv1a(REPLY_WAV)))

    assert exchange(target, device)["ok"]
    record = entries[0]
    expected_post = (endpoint_ms + 200) * host.BYTES_PER_MS
    assert record["endpoint_postwake_bytes"] == endpoint_ms * host.BYTES_PER_MS
    assert record["endpoint_reason"] == "stub_sample_limit"
    assert record["postwake_bytes"] == expected_post
    assert record["audio_bytes"] == len(PCM) + expected_post
    assert record["pcm_sha256"] == hashlib.sha256(PCM + POST_PCM * (expected_post // 3200)).hexdigest()
    assert record["capture_complete"] and record["turn_end_sent"]


@pytest.mark.parametrize("post_ms", [0, 200])
def test_fixture_eof_before_endpoint_sends_speech_end_then_reply(post_ms):
    entries = []
    exchange(
        receiver(source="fixture", stub_end_after_ms=8000, emit=entries.append),
        lambda peer: device_turn(peer, flags=1, endpoint_ms=8000, post_ms=post_ms),
    )
    assert entries[0]["ok"]
    assert entries[0]["endpoint_reason"] == "fixture_eof"
    assert entries[0]["postwake_bytes"] == post_ms * host.BYTES_PER_MS


@pytest.mark.parametrize("value", [0, 99, 60001, True, 100.0])
def test_endpoint_policy_is_explicit_and_bounded(value):
    with pytest.raises(ValueError, match="100..60000"):
        receiver(stub_end_after_ms=value)


@pytest.mark.parametrize("changes,reason", [
    ({"magic": b"RIAUDIO2"}, "magic"), ({"sequence": 0}, "sequence"),
    ({"sequence": 2}, "sequence"), ({"length": 1}, "length"),
    ({"length": 3202}, "length"), ({"length": 0xFFFFFFFF}, "length"),
    ({"length": 0}, "before backend endpoint"),
    ({"candidate_id": CANDIDATE_ID + 1}, "identity"),
    ({"candidate_id": CANDIDATE_ID - 1}, "identity"),
    ({"candidate_id": ((NONCE + 1) << 32) | 1}, "identity"),
])
def test_malformed_or_mixed_stream_headers_are_rejected_before_payload(changes, reason):
    fields = {
        "magic": host.AUDIO_MAGIC, "candidate_id": CANDIDATE_ID, "sequence": 1, "length": 3200,
    }
    fields.update(changes)

    def device(peer):
        start_turn(peer)
        peer.sendall(host.AUDIO.pack(*fields.values()))

    with pytest.raises(host.ProtocolError, match=reason):
        exchange(receiver(), device)


def test_chunk_and_end_marker_sequences_are_contiguous():
    def device(peer):
        start_turn(peer)
        send_chunk(peer, 1, POST_PCM)
        read_control(peer, "speech.endDetected")
        send_chunk(peer, 1, b"")

    with pytest.raises(host.ProtocolError, match="sequence/order"):
        exchange(receiver(), device)


@pytest.mark.parametrize("payload", [
    b"R", request_header()[:63], request_header() + PCM[:-2],
    request_header() + PCM + host.AUDIO.pack(host.AUDIO_MAGIC, CANDIDATE_ID, 1, 3200) + b"\0\0",
    request_header() + PCM
    + host.AUDIO.pack(host.AUDIO_MAGIC, CANDIDATE_ID, 1, len(POST_PCM)) + POST_PCM
    + host.AUDIO.pack(host.AUDIO_MAGIC, CANDIDATE_ID, 2, 0)
    + host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 8, host.fnv1a(REPLY_WAV))[:23],
])
def test_truncated_frame_is_not_success(payload):
    with pytest.raises(host.ProtocolError, match="truncated EOF"):
        run_packet(payload)


def test_empty_idle_stream_is_distinct_from_truncation():
    summary = run_packet(b"")
    assert summary["ok"] and summary["no_detections"]
    assert summary["count"] == summary["result_count"] == 0


@pytest.mark.parametrize("reply_wav", [REPLY_WAV, OTHER_REPLY_WAV])
def test_arbitrary_quiet_waveforms_and_fragmented_protocol(reply_wav):
    entries = []
    summary = exchange(
        receiver(reply_wav=reply_wav, emit=entries.append),
        lambda peer: device_turn(peer, reply_wav=reply_wav, fragment_size=3),
    )
    assert summary["statuses"]["validated_only"] == 1
    assert summary["statuses"]["played"] == 0
    assert entries[0]["reply_fnv1a"] == host.fnv1a(reply_wav)


def test_played_requires_matching_hash_and_expected_status():
    assert exchange(receiver(playback=True), lambda peer: device_turn(peer, status=0))["ok"]
    with pytest.raises(host.ProtocolError, match="unexpected unit status"):
        exchange(receiver(playback=True), device_turn)


@pytest.mark.parametrize("options,reason", [
    ({"result_id": CANDIDATE_ID - 1}, "identity"),
    ({"result_id": CANDIDATE_ID + 1}, "identity"),
    ({"result_magic": b"RIREPLY2"}, "magic"),
    ({"status": 99}, "unknown"),
    ({"reply_hash": host.fnv1a(REPLY_WAV) ^ 1}, "hash mismatch"),
    ({"status": 0}, "unexpected unit status"),
    ({"status": 1, "reply_hash": 0}, "unexpected unit status"),
    ({"status": 2, "reply_hash": 5}, "zero or matching complete reply hash"),
])
def test_invalid_receipt_cannot_be_success(options, reason):
    entries = []
    target = receiver(emit=entries.append)
    with pytest.raises(host.ProtocolError, match=reason):
        exchange(target, lambda peer: device_turn(peer, **options))
    assert entries[0]["ok"] is False
    assert target.result_count == 0


@pytest.mark.parametrize("status", [3, 4, 5, 6, 7])
def test_unit_error_statuses_remain_errors(status):
    target = receiver()
    value = host.fnv1a(REPLY_WAV) if status == 5 else 0
    with pytest.raises(host.UnitFailure, match=f"status {status}"):
        exchange(target, lambda peer: device_turn(peer, status=status, reply_hash=value))
    assert target.status_counts[host.STATUS_NAMES[status]] == 1


def test_success_receipt_cannot_arrive_before_stream_end_and_reply():
    def device(peer):
        start_turn(peer)
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 8, host.fnv1a(REPLY_WAV)))

    with pytest.raises(host.ProtocolError, match="before reply"):
        exchange(receiver(), device)


def test_local_cancel_end_marker_is_not_mistaken_for_device_vad():
    entries = []

    def device(peer):
        start_turn(peer)
        send_chunk(peer, 1, POST_PCM[:32])
        send_chunk(peer, 2, b"")
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 2, 0))
        device_turn(peer, counter=2, flags=4)

    summary = exchange(receiver(emit=entries.append), device)
    assert summary["count"] == 2
    assert summary["statuses"]["cancelled"] == summary["statuses"]["validated_only"] == 1
    assert entries[0]["outcome"] == "cancelled"
    assert not entries[0]["reply_sent"]
    assert entries[0]["endpoint_reason"] is None
    assert entries[1]["followup"] and entries[1]["ok"]


def test_cancel_after_full_reply_accepts_actual_complete_hash_but_not_success():
    entries = []
    summary = exchange(receiver(emit=entries.append), lambda peer: device_turn(peer, status=2))
    assert summary["statuses"]["cancelled"] == 1
    assert not entries[0]["ok"] and entries[0]["outcome"] == "cancelled"
    assert entries[0]["reply_fnv1a"] == host.fnv1a(REPLY_WAV)


def test_audio_after_end_marker_is_not_another_capture():
    def device(peer):
        capture_turn(peer)
        send_chunk(peer, 3, POST_PCM)

    with pytest.raises(host.ProtocolError, match="after end marker"):
        exchange(receiver(), device)


@pytest.mark.parametrize("cancel_first", [False, True])
def test_superseding_wake_cancels_old_wait_and_skips_only_known_old_frames(cancel_first):
    entries = []
    target = receiver(emit=entries.append)

    def device(peer):
        capture_turn(peer)
        if cancel_first:
            peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 2, 0))
        new_id = start_turn(peer, counter=2)
        send_chunk(peer, 3, b"\x01\0", candidate_id=CANDIDATE_ID)
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 2, 0))
        send_chunk(peer, 1, POST_PCM, candidate_id=new_id)
        read_control(peer, "speech.endDetected", new_id)
        send_chunk(peer, 2, b"", candidate_id=new_id)
        read_reply(peer, new_id)
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, new_id, 8, host.fnv1a(REPLY_WAV)))

    summary = exchange(target, device)
    assert summary["ok"] and summary["count"] == 2
    assert summary["statuses"]["validated_only"] == 1
    assert entries[0]["outcome"] == ("cancelled" if cancel_first else "superseded")
    assert not entries[0]["ok"]
    assert entries[1]["ok"]
    assert list(target.superseded) == [CANDIDATE_ID]


def test_superseded_identity_retention_is_bounded_and_evicted_ids_are_rejected():
    target = receiver()

    def device(peer):
        for counter in range(1, host.MAX_SUPERSEDED_IDS + 2):
            candidate_id = start_turn(peer, counter=counter)
            peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, candidate_id, 2, 0))
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 2, 0))

    with pytest.raises(host.ProtocolError, match="unknown stale"):
        exchange(target, device)
    assert len(target.superseded) == host.MAX_SUPERSEDED_IDS
    assert CANDIDATE_ID not in target.superseded


@pytest.mark.parametrize("next_header,reason", [
    (request_header(), "identity/order"),
    (request_header(candidate_id=CANDIDATE_ID + 2), "identity/order"),
    (request_header(candidate_id=CANDIDATE_ID + 1, monotonic_ms=1), "backwards"),
])
def test_replay_skip_and_timestamp_regression_fail(next_header, reason):
    def device(peer):
        capture_turn(peer)
        peer.sendall(next_header)

    with pytest.raises(host.ProtocolError, match=reason):
        exchange(receiver(), device)


def test_idle_between_turns_is_unbounded_but_state_is_not_history():
    target = receiver(io_timeout=0.1)
    original_fields = set(vars(target))

    def device(peer):
        time.sleep(0.2)
        device_turn(peer)
        time.sleep(0.2)
        for counter in range(2, 141):
            device_turn(peer, counter=counter)

    summary = exchange(target, device)
    assert summary["count"] == summary["result_count"] == 140
    assert set(vars(target)) == original_fields
    assert target.active is None and not target.superseded
    assert len(target.status_counts) == len(host.STATUS_NAMES)
    assert not any(isinstance(value, list) for value in vars(target).values())


def test_progress_is_sent_while_waiting_and_across_partial_frame_reads(monkeypatch):
    assert 0 < host.PROGRESS_SECONDS <= 5
    monkeypatch.setattr(host, "PROGRESS_SECONDS", 0.02)

    def device(peer):
        start_turn(peer)
        read_control(peer, "speech-progress")
        header = host.AUDIO.pack(host.AUDIO_MAGIC, CANDIDATE_ID, 1, len(POST_PCM))
        peer.sendall(header + POST_PCM[:2])
        read_control(peer, "speech-progress")
        peer.sendall(POST_PCM[2:])
        read_control(peer, "speech.endDetected")
        send_chunk(peer, 2, b"")
        read_reply(peer)
        peer.sendall(host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, 8, host.fnv1a(REPLY_WAV)))

    assert exchange(receiver(io_timeout=0.3), device)["ok"]


@pytest.mark.parametrize("prefix,label", [
    (b"R", "frame header"), (request_header() + PCM[:2], "wake PCM"),
    (request_header() + PCM, "frame header"),
    (
        request_header() + PCM + host.AUDIO.pack(host.AUDIO_MAGIC, CANDIDATE_ID, 1, 3200) + b"\0\0",
        "stream PCM",
    ),
])
def test_mid_frame_stalls_keep_absolute_deadlines_despite_progress(prefix, label, monkeypatch):
    monkeypatch.setattr(host, "PROGRESS_SECONDS", 0.01)

    def device(peer):
        peer.sendall(prefix)
        time.sleep(0.12)

    with pytest.raises(host.TransportTimeout, match=label):
        exchange(receiver(io_timeout=0.03), device)


def test_receipt_timeout_is_an_error():
    def device(peer):
        capture_turn(peer)
        time.sleep(0.15)

    with pytest.raises(host.TransportTimeout, match="unit result"):
        exchange(receiver(io_timeout=0.05), device)


def test_blocked_reply_write_times_out():
    left, right = socket.socketpair()
    with left, right, pytest.raises(host.TransportTimeout, match="reply WAV"):
        left.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
        host.SocketIO(left).write_all(
            b"x" * host.REPLY_CHUNK_BYTES, time.monotonic() + 0.03, "reply WAV",
        )


def test_deadlines_do_not_reset_for_fragments():
    left, right = socket.socketpair()
    stop = threading.Event()

    def trickle():
        with right:
            while not stop.wait(0.015):
                try:
                    right.sendall(b"x")
                except OSError:
                    return

    thread = threading.Thread(target=trickle)
    thread.start()
    try:
        with left, pytest.raises(host.TransportTimeout):
            host.SocketIO(left).read_exact(100, time.monotonic() + 0.07, "slow frame")
    finally:
        stop.set()
        thread.join(timeout=1)
        assert not thread.is_alive()


def test_reply_write_stall_deadline_renews_on_real_socket_progress():
    left, right = socket.socketpair()
    data = bytes(range(256)) * (host.REPLY_CHUNK_BYTES // 256)
    received = hashlib.sha256()
    errors = []

    def drain():
        try:
            with right:
                while block := right.recv(2048):
                    received.update(block)
                    time.sleep(0.015)
        except BaseException as error:
            errors.append(error)

    thread = threading.Thread(target=drain)
    thread.start()
    before = time.monotonic()
    try:
        with left:
            left.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
            host.SocketIO(left).write_all(
                data, before + 0.2, "reply WAV", stall_timeout=0.2,
            )
    finally:
        thread.join(timeout=2)
    assert not thread.is_alive() and not errors
    assert time.monotonic() - before > 0.4
    assert received.hexdigest() == hashlib.sha256(data).hexdigest()


def test_indefinite_deadline_only_allowed_for_idle_first_byte():
    left, right = socket.socketpair()
    with left, right, pytest.raises(ValueError, match="idle request"):
        host.SocketIO(left).read_exact(64, None, "request header")


class GeneratedIO(host.SocketIO):
    """Generate frames on demand to measure receiver, not fixture, retention."""

    def __init__(self, frames: int, reply_wav: bytes | host.ReplySource = REPLY_WAV, status: int = 8):
        reply_hash = reply_wav.fnv if isinstance(reply_wav, host.ReplySource) else host.fnv1a(reply_wav)

        def packets():
            yield request_header() + PCM
            for sequence in range(1, frames + 1):
                yield host.AUDIO.pack(host.AUDIO_MAGIC, CANDIDATE_ID, sequence, len(POST_PCM)) + POST_PCM
            yield host.AUDIO.pack(host.AUDIO_MAGIC, CANDIDATE_ID, frames + 1, 0)
            assert self.turn_end_seen, "backend waited for receipt before sending turn.end"
            yield host.RESULT.pack(host.RESULT_MAGIC, CANDIDATE_ID, status, reply_hash)

        self.packets = iter(packets())
        self.pending = b""
        self.turn_end_seen = False
        self.receipt_budget = 0.0
        self.reply_bytes = 0
        self.reply_sha256 = hashlib.sha256()
        self.largest_write = 0

    def read_some(self, count: int, deadline: float | None, label: str) -> bytes:
        if not self.pending:
            self.pending = next(self.packets, b"")
        if label == "unit result" and deadline is not None and not self.receipt_budget:
            self.receipt_budget = deadline - time.monotonic()
        result, self.pending = self.pending[:count], self.pending[count:]
        return result

    def write_all(
        self, data: bytes, deadline: float, label: str, *, stall_timeout: float | None = None,
    ) -> None:
        assert len(data) <= host.REPLY_CHUNK_BYTES and deadline > time.monotonic()
        self.largest_write = max(self.largest_write, len(data))
        if label == "reply WAV":
            self.reply_bytes += len(data)
            self.reply_sha256.update(data)
        if label == "backend control" and host.CONTROL.unpack(data)[2] == 3:
            self.turn_end_seen = True


def test_stream_digest_and_memory_do_not_accumulate_whole_command():
    frames = 2400
    entries = []
    target = receiver(stub_end_after_ms=60000, io_timeout=10, emit=entries.append)
    link = GeneratedIO(frames)
    tracemalloc.start()
    try:
        assert target.run(link)["ok"]
        _, peak = tracemalloc.get_traced_memory()
    finally:
        tracemalloc.stop()
    digest = hashlib.sha256(PCM)
    for _ in range(frames):
        digest.update(POST_PCM)
    assert entries[0]["audio_bytes"] == len(PCM) + frames * len(POST_PCM)
    assert entries[0]["audio_bytes"] > 7_000_000
    assert entries[0]["pcm_sha256"] == digest.hexdigest()
    assert peak < 256 * 1024
    assert target.active is None


def long_reply(seconds: int = 75) -> bytes:
    return host.pcm_wav(b"\xff\x7f\0\x80" * (seconds * host.RATE // 2))


@pytest.mark.parametrize("seconds", [75, 90])
def test_replies_longer_than_sixty_seconds_and_fullscale_pcm_are_valid(seconds):
    wav = long_reply(seconds)
    assert len(wav) == seconds * host.RATE * 2 + 44
    host.validate_reply_wav(wav)
    with pytest.raises(host.ProtocolError, match="length"):
        host.validate_reply_wav(wav + b"\0")
    for sample in (9191, -9191, 32767, -32768):
        host.validate_reply_wav(host.pcm_wav(struct.pack("<h", sample)))


@pytest.mark.parametrize("offset,replacement", [
    (0, b"RIFX"), (4, struct.pack("<I", len(REPLY_WAV))),
    (8, b"NOPE"), (12, b"JUNK"), (16, struct.pack("<I", 18)),
    (20, struct.pack("<H", 3)), (22, struct.pack("<H", 2)),
    (24, struct.pack("<I", 22050)), (28, struct.pack("<I", 64000)),
    (32, struct.pack("<H", 4)), (34, struct.pack("<H", 8)),
    (36, b"LIST"), (40, struct.pack("<I", 2)),
])
def test_noncanonical_reply_headers_rejected(offset, replacement):
    changed = bytearray(REPLY_WAV)
    changed[offset:offset + len(replacement)] = replacement
    with pytest.raises(host.ProtocolError):
        host.validate_reply_wav(bytes(changed))


@pytest.mark.parametrize("wav", [
    b"", REPLY_WAV[:44], REPLY_WAV[:-1], REPLY_WAV + b"\0\0",
])
def test_inconsistent_reply_lengths_are_rejected(wav):
    with pytest.raises(host.ProtocolError):
        host.validate_reply_wav(wav)


def test_early_turn_end_keeps_full_wav_playback_receipt_deadline():
    wav = long_reply()
    target = receiver(playback=True, reply_wav=wav, io_timeout=0.5)
    link = GeneratedIO(1, wav, status=0)
    assert target.run(link)["ok"]
    assert link.turn_end_seen
    assert 80.0 < link.receipt_budget <= 80.5


def test_riff_padding_extended_fmt_and_text_metadata_are_not_pcm_policy():
    pcm = b"\xff\x7f\0\x80" * 10
    parts = [
        (b"JUNK", b"text does not control playback"),
        (b"fmt ", struct.pack("<HHIIHHH", 1, 1, 16000, 32000, 2, 16, 0)),
        (b"data", pcm), (b"LIST", b"unrelated arbitrary transcript"),
    ]
    body = b"WAVE" + b"".join(
        name + struct.pack("<I", len(payload)) + payload + (b"\0" if len(payload) % 2 else b"")
        for name, payload in parts
    )
    wav = b"RIFF" + struct.pack("<I", len(body)) + body
    reply = host.reply_source(wav)
    assert reply.pcm_bytes == len(pcm)
    assert reply.duration_seconds == len(pcm) / 32000
    assert reply.fnv == host.fnv1a(wav)
    assert exchange(
        receiver(reply_wav=reply), lambda peer: device_turn(peer, reply_wav=wav),
    )["ok"]


@pytest.mark.parametrize("tail", [
    b"JUNK" + struct.pack("<I", 0xFFFFFFFF),
    b"data" + struct.pack("<I", 2) + b"\0\0",
    b"fmt " + struct.pack("<IHHIIHH", 16, 1, 1, 16000, 32000, 2, 16),
    b"JUNK" + struct.pack("<I", 1) + b"x",
])
def test_late_riff_errors_and_overflows_are_rejected(tail):
    wav = bytearray(REPLY_WAV + tail)
    struct.pack_into("<I", wav, 4, len(wav) - 8)
    with pytest.raises(host.ProtocolError):
        host.validate_reply_wav(bytes(wav))
