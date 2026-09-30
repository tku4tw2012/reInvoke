"""RIWAKE03 command capture and length-delimited, streaming WAV replies.

The explicit stub endpoint counts postwake samples; it is not VAD or a device
capture limit. PCM is hashed incrementally, never recorded or accumulated.
One Receiver handles an ongoing authenticated stream with bounded turn state.
"""

from __future__ import annotations

import hashlib
import io
import math
import os
import socket
import stat
import struct
import time
from collections import deque
from collections.abc import Callable, Generator, Iterator
from contextlib import closing
from dataclasses import dataclass, field
from pathlib import Path
from typing import BinaryIO, Literal

REQUEST_MAGIC = b"RIWAKE03"
REPLY_MAGIC = b"RIREPLY3"
RESULT_MAGIC = b"RIRESULT"
AUDIO_MAGIC = b"RIAUDIO3"
CONTROL_MAGIC = b"RICTRL03"
REQUEST = struct.Struct("<8sHHQQdfiIIHHII")
REPLY = struct.Struct("<8sQII")
RESULT = struct.Struct("<8sQII")
AUDIO = struct.Struct("<8sQII")
CONTROL = struct.Struct("<8sQII")
WAV_HEADER = struct.Struct("<4sI4s4sIHHIIHH4sI")
RIFF_HEADER = struct.Struct("<4sI4s")
CHUNK_HEADER = struct.Struct("<4sI")
PCM_FORMAT = struct.Struct("<HHIIHH")
RATE = 16_000
BYTES_PER_MS = 32
MAX_WAKE_BYTES = 160_000
MAX_AUDIO_FRAME_BYTES = 3200
MAX_REPLY_BYTES = 0xFFFFFFFF  # Existing RIREPLY3 u32 wire length, not an answer limit.
REPLY_CHUNK_BYTES = 65536
MAX_MONOTONIC_MS = 10 * 366 * 24 * 60 * 60 * 1000
MAX_SUPERSEDED_IDS = 4
PROGRESS_SECONDS = 4.0
VERDICTS = {"accept": 0, "reject": 1, "cancel": 2}
CONTROL_CODES = {
    "speech.startDetected": 1,
    "speech.endDetected": 2,
    "turn.end": 3,
    "cancel": 4,
    "followup-listen": 5,
    "speech-progress": 6,
}
STATUS_NAMES = {
    0: "played",
    1: "rejected",
    2: "cancelled",
    3: "timeout",
    4: "bad_reply",
    5: "playback_error",
    6: "send_error",
    7: "transport_closed",
    8: "validated_only",
}


class ProtocolError(Exception):
    """A malformed, inconsistent, or failed handoff; never a successful turn."""


class TransportTimeout(ProtocolError):
    """An absolute monotonic I/O deadline expired."""


class UnitFailure(ProtocolError):
    """The unit reported an error, rather than successful playback."""


def fnv1a(data: bytes, value: int = 0x811C9DC5) -> int:
    for octet in data:
        value = ((value ^ octet) * 0x01000193) & 0xFFFFFFFF
    return value


def pcm_wav(pcm: bytes) -> bytes:
    if not 0 < len(pcm) <= MAX_REPLY_BYTES - WAV_HEADER.size or len(pcm) % 2:
        raise ProtocolError("reply PCM must be positive, even, and fit the u32 WAV wire length")
    return WAV_HEADER.pack(
        b"RIFF", len(pcm) + 36, b"WAVE", b"fmt ", 16, 1, 1,
        RATE, RATE * 2, 2, 16, b"data", len(pcm),
    ) + pcm


def _file_bytes(source: BinaryIO, count: int) -> bytes:
    data = source.read(count)
    if len(data) != count:
        raise ProtocolError("truncated reply WAV")
    return data


def _wav_metadata(source: BinaryIO, length: int) -> int:
    """Validate RIFF sizes and PCM metadata without reading or changing samples."""
    if not 46 <= length <= MAX_REPLY_BYTES or length % 2:
        raise ProtocolError("reply WAV length must be even and fit the u32 wire length")
    source.seek(0)
    magic, riff_size, kind = RIFF_HEADER.unpack(_file_bytes(source, RIFF_HEADER.size))
    if (magic, kind) != (b"RIFF", b"WAVE") or riff_size + 8 != length:
        raise ProtocolError("inconsistent reply RIFF size or format")
    offset, fmt_seen, pcm_bytes = RIFF_HEADER.size, False, 0
    while offset < length:
        if length - offset < CHUNK_HEADER.size:
            raise ProtocolError("incomplete reply RIFF chunk header")
        name, size = CHUNK_HEADER.unpack(_file_bytes(source, CHUNK_HEADER.size))
        offset += CHUNK_HEADER.size
        padded = size + (size & 1)
        if padded > length - offset:
            raise ProtocolError("reply RIFF chunk exceeds declared size")
        if name == b"fmt ":
            if fmt_seen or size not in (16, 18):
                raise ProtocolError("reply WAV requires one PCM fmt chunk (16 or 18 bytes)")
            raw = _file_bytes(source, size)
            if PCM_FORMAT.unpack(raw[:16]) != (1, 1, RATE, RATE * 2, 2, 16) or (
                size == 18 and raw[16:] != b"\0\0"
            ):
                raise ProtocolError("reply WAV must be 16000 Hz mono S16_LE PCM")
            fmt_seen = True
        else:
            if name == b"data":
                if not fmt_seen or pcm_bytes or not size or size % 2:
                    raise ProtocolError("reply WAV requires one positive even data chunk after fmt")
                pcm_bytes = size
            source.seek(padded, os.SEEK_CUR)
        offset += padded
    if not fmt_seen or not pcm_bytes:
        raise ProtocolError("reply WAV is missing fmt or data")
    return pcm_bytes


def validate_reply_wav(wav: bytes) -> None:
    """Validate an in-memory fixture; production replies use a file-backed source."""
    _wav_metadata(io.BytesIO(wav), len(wav))


def _file_identity(info: os.stat_result) -> tuple[int, ...]:
    return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns


def _open_reply(path: Path) -> BinaryIO:
    fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
    source = os.fdopen(fd, "rb")
    if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
        source.close()
        raise ProtocolError("reply WAV must be a regular file")
    return source


def _blocks(source: BinaryIO, length: int) -> Iterator[bytes]:
    remaining = length
    while remaining:
        block = source.read(min(remaining, REPLY_CHUNK_BYTES))
        if not block:
            raise ProtocolError("truncated reply WAV")
        remaining -= len(block)
        yield block
    if source.read(1):
        raise ProtocolError("reply WAV grew beyond declared length")


def _reply_hashes(source: BinaryIO, length: int) -> tuple[int, str]:
    source.seek(0)
    value, digest = 0x811C9DC5, hashlib.sha256()
    for block in _blocks(source, length):
        value = fnv1a(block, value)
        digest.update(block)
    return value, digest.hexdigest()


@dataclass(frozen=True)
class ReplySource:
    """Known-size WAV with bounded reads, immutable metadata, and no PCM policy.

    File identity and streamed hashes are rechecked for each send. Bytes-backed
    sources are only a convenience for synthetic fixtures.
    """

    length: int
    pcm_bytes: int
    fnv: int
    sha256: str
    path: Path | None = None
    identity: tuple[int, ...] | None = None
    data: bytes | None = field(default=None, repr=False)

    def __len__(self) -> int:
        return self.length

    @property
    def duration_seconds(self) -> float:
        return self.pcm_bytes / (RATE * 2)

    def chunks(self) -> Generator[bytes, None, None]:
        if self.data is not None:
            for offset in range(0, self.length, REPLY_CHUNK_BYTES):
                yield self.data[offset:offset + REPLY_CHUNK_BYTES]
            return
        assert self.path is not None
        try:
            with _open_reply(self.path) as source:
                if _file_identity(os.fstat(source.fileno())) != self.identity:
                    raise ProtocolError("reply WAV changed after validation")
                yield from _blocks(source, self.length)
                if _file_identity(os.fstat(source.fileno())) != self.identity:
                    raise ProtocolError("reply WAV changed during transmission")
        except OSError as error:
            raise ProtocolError("reply WAV file read failed") from error


def load_reply_wav(path: Path) -> ReplySource:
    with _open_reply(path) as source:
        before = _file_identity(os.fstat(source.fileno()))
        length = before[2]
        pcm_bytes = _wav_metadata(source, length)
        value, digest = _reply_hashes(source, length)
        if before != _file_identity(os.fstat(source.fileno())):
            raise ProtocolError("reply WAV changed during validation")
    return ReplySource(length, pcm_bytes, value, digest, path.resolve(), before)


def reply_source(wav: bytes | ReplySource) -> ReplySource:
    if isinstance(wav, ReplySource):
        return wav
    with io.BytesIO(wav) as source:
        pcm_bytes = _wav_metadata(source, len(wav))
        value, digest = _reply_hashes(source, len(wav))
    return ReplySource(len(wav), pcm_bytes, value, digest, data=wav)


@dataclass(frozen=True)
class Candidate:
    flags: int
    candidate_id: int
    monotonic_ms: int
    confidence: float
    threshold: float
    start_ms: int
    duration_ms: int
    sample_rate: int
    audio_format: int
    channels: int
    audio_bytes: int
    wake_bytes: int


def parse_request(
    raw: bytes,
    *,
    nonce: int,
    counter: int,
    previous_monotonic_ms: int = 0,
    source: str | None = None,
) -> Candidate:
    if len(raw) != REQUEST.size:
        raise ProtocolError("request header must be exactly 64 bytes")
    magic, version, *fields = REQUEST.unpack(raw)
    if magic != REQUEST_MAGIC or version != 3:
        raise ProtocolError("wrong request magic or version")
    candidate = Candidate(*fields)
    if candidate.flags & ~7:
        raise ProtocolError("unknown request flags")
    if not 0 < nonce <= 0xFFFFFFFF or not 0 < counter <= 0xFFFFFFFF:
        raise ProtocolError("invalid expected nonce/counter")
    if candidate.candidate_id != (nonce << 32) | counter:
        raise ProtocolError("request identity/order mismatch (nonce or counter)")
    if not 0 < candidate.monotonic_ms <= MAX_MONOTONIC_MS:
        raise ProtocolError("timestamp must be bounded positive monotonic uptime, not epoch")
    if candidate.monotonic_ms < previous_monotonic_ms:
        raise ProtocolError("monotonic timestamp moved backwards")
    if not math.isfinite(candidate.confidence) or not 0 <= candidate.confidence <= 1:
        raise ProtocolError("confidence must be finite and in [0, 1]")
    if not math.isfinite(candidate.threshold) or not 0 <= candidate.threshold <= 1:
        raise ProtocolError("threshold must be finite and in [0, 1]")
    if (candidate.sample_rate, candidate.audio_format, candidate.channels) != (RATE, 1, 1):
        raise ProtocolError("unsupported request PCM format")
    if candidate.flags & 4:
        if any((
            candidate.audio_bytes, candidate.wake_bytes, candidate.confidence,
            candidate.threshold, candidate.start_ms, candidate.duration_ms,
        )):
            raise ProtocolError("followup must have no wake PCM or keyword measurements")
    else:
        if not 0 < candidate.wake_bytes <= MAX_WAKE_BYTES or candidate.wake_bytes % 2:
            raise ProtocolError("wake length must be positive, even, and <=160000")
        if candidate.audio_bytes != candidate.wake_bytes:
            raise ProtocolError("initial audio length must equal wake length")
        if candidate.start_ms < 0 or candidate.duration_ms == 0:
            raise ProtocolError("keyword start must be nonnegative and duration positive")
        if candidate.flags & 2 and candidate.start_ms != 0:
            raise ProtocolError("clipped keyword start must be normalized to zero")
        if (candidate.start_ms + candidate.duration_ms) * BYTES_PER_MS > (
            candidate.wake_bytes + BYTES_PER_MS
        ):
            raise ProtocolError("keyword interval exceeds wake window (1 ms rounding slack)")
    if source is not None:
        if source not in ("fixture", "socket"):
            raise ProtocolError("invalid expected source")
        if bool(candidate.flags & 1) != (source == "fixture"):
            raise ProtocolError("request source flag mismatch")
    return candidate


@dataclass(frozen=True)
class AudioFrame:
    candidate_id: int
    sequence: int
    length: int


def parse_audio(raw: bytes) -> AudioFrame:
    if len(raw) != AUDIO.size:
        raise ProtocolError("audio header must be exactly 24 bytes")
    magic, candidate_id, sequence, length = AUDIO.unpack(raw)
    if magic != AUDIO_MAGIC:
        raise ProtocolError("wrong audio magic")
    if sequence == 0:
        raise ProtocolError("audio sequence must be positive")
    if length > MAX_AUDIO_FRAME_BYTES or length % 2:
        raise ProtocolError("audio chunk length must be even and <=3200")
    return AudioFrame(candidate_id, sequence, length)


def validate_stub_endpoint(value: int) -> None:
    if type(value) is not int or not 100 <= value <= 60000:
        raise ValueError("stub endpoint must be an integer in 100..60000 ms")


class SocketIO:
    """Sequential socket/SSLSocket I/O with bounded buffers and stall clocks.

    Always use recv/send: reading an SSLSocket's file descriptor would bypass
    TLS and ignore already-decrypted bytes buffered inside OpenSSL.
    Capture/control frames retain absolute deadlines. Only reply body writes
    renew their deadline on progress; playback may legitimately take minutes.
    """

    def __init__(self, connection: socket.socket) -> None:
        self.connection = connection

    def _timeout(self, deadline: float | None, label: str) -> None:
        remaining = None if deadline is None else deadline - time.monotonic()
        if remaining is not None and remaining <= 0:
            raise TransportTimeout(f"{label}: deadline expired")
        self.connection.settimeout(remaining)

    def read_some(self, count: int, deadline: float | None, label: str) -> bytes:
        if not 0 < count <= MAX_WAKE_BYTES:
            raise ValueError("read size outside bounded transport range")
        try:
            self._timeout(deadline, label)
            return self.connection.recv(min(count, REPLY_CHUNK_BYTES))
        except TimeoutError as error:
            raise TransportTimeout(f"{label}: deadline expired") from error
        except OSError as error:
            raise ProtocolError(f"{label}: read failed") from error

    def read_exact(
        self, count: int, deadline: float | None, label: str, *, allow_eof: bool = False,
    ) -> bytes | None:
        if not 0 < count <= MAX_WAKE_BYTES:
            raise ValueError("frame size outside bounded transport range")
        # Only the first byte of a new request may wait indefinitely.
        if deadline is None and not (count == 1 and allow_eof):
            raise ValueError("only an idle request may have no deadline")
        data = bytearray()
        while len(data) < count:
            block = self.read_some(count - len(data), deadline, label)
            if not block:
                if not data and allow_eof:
                    return None
                raise ProtocolError(f"{label}: truncated EOF ({len(data)}/{count} bytes)")
            data.extend(block)
        return bytes(data)

    def write_all(
        self, data: bytes, deadline: float, label: str, *, stall_timeout: float | None = None,
    ) -> None:
        if len(data) > REPLY_CHUNK_BYTES:
            raise ValueError("write size outside bounded transport range")
        offset = 0
        view = memoryview(data)
        while offset < len(data):
            try:
                self._timeout(deadline, label)
                written = self.connection.send(view[offset:offset + REPLY_CHUNK_BYTES])
            except TimeoutError as error:
                raise TransportTimeout(f"{label}: deadline expired") from error
            except OSError as error:
                raise ProtocolError(f"{label}: write failed") from error
            if written == 0:
                raise ProtocolError(f"{label}: write returned zero")
            offset += written
            if stall_timeout is not None:
                deadline = time.monotonic() + stall_timeout


class _Turn:
    def __init__(self, candidate: Candidate, entry: dict[str, object]) -> None:
        self.candidate = candidate
        self.entry = entry
        self.digest = hashlib.sha256()
        self.audio_bytes = 0
        self.postwake_bytes = 0
        self.next_sequence = 1
        self.audio_frames = 0
        self.phase: Literal["capture", "drain", "receipt", "cancel_pending"] = "capture"
        self.deadline: float | None = None
        self.progress_at = time.monotonic() + PROGRESS_SECONDS

    def add_pcm(self, pcm: bytes, *, postwake: bool) -> None:
        self.digest.update(pcm)
        self.audio_bytes += len(pcm)
        if postwake:
            self.postwake_bytes += len(pcm)
            self.audio_frames += 1


class Receiver:
    def __init__(
        self,
        *,
        nonce: int,
        reply_wav: bytes | ReplySource,
        stub_end_after_ms: int,
        playback: bool = False,
        source: str | None = None,
        io_timeout: float = 30,
        emit: Callable[[dict], None] | None = None,
    ) -> None:
        if not 0 < nonce <= 0xFFFFFFFF:
            raise ValueError("invalid nonce")
        if not math.isfinite(io_timeout) or not 0 < io_timeout <= 30:
            raise ValueError("I/O timeout must be positive and <=30 seconds")
        if source not in (None, "fixture", "socket"):
            raise ValueError("invalid source")
        validate_stub_endpoint(stub_end_after_ms)
        self.nonce = nonce
        self.reply_wav = reply_source(reply_wav)
        self.reply_hash = self.reply_wav.fnv
        self.playback = playback
        self.source = source
        self.stub_end_after_ms = stub_end_after_ms
        self.io_timeout = io_timeout
        self.emit = emit
        self.count = 0
        self.result_count = 0
        self.previous_monotonic_ms = 0
        self.status_counts = dict.fromkeys(STATUS_NAMES.values(), 0)
        self.active: _Turn | None = None
        self.superseded: deque[int] = deque(maxlen=MAX_SUPERSEDED_IDS)
        self._started = False

    def summary(self) -> dict:
        return {
            "event": "summary", "ok": True, "count": self.count,
            "result_count": self.result_count, "statuses": self.status_counts.copy(),
            "no_detections": self.count == 0,
        }

    def _entry(self) -> dict[str, object]:
        return {
            "event": "turn", "ok": False, "outcome": "failed", "nonce": self.nonce,
            "candidate_id": None, "source": self.source, "followup": None,
            "playback": self.playback, "playback_enabled": self.playback,
            "audio_bytes": 0, "wake_bytes": None, "postwake_bytes": 0, "audio_frames": 0,
            "pcm_sha256": None, "capture_complete": False, "score": None, "threshold": None,
            "stub_end_after_ms": self.stub_end_after_ms, "endpoint_reason": None,
            "endpoint_postwake_bytes": None, "status_code": None, "status": None,
            "expected_reply_fnv1a": self.reply_hash, "reply_fnv1a": None,
            "reply_bytes": len(self.reply_wav), "reply_sent": False, "turn_end_sent": False,
        }

    def _finish(self, outcome: str, *, error: str | None = None) -> None:
        turn = self.active
        entry = turn.entry if turn is not None else self._entry()
        if turn is not None:
            entry.update(
                audio_bytes=turn.audio_bytes, postwake_bytes=turn.postwake_bytes,
                audio_frames=turn.audio_frames, pcm_sha256=turn.digest.hexdigest(),
            )
            if outcome in ("cancelled", "superseded"):
                self.superseded.append(turn.candidate.candidate_id)
        entry.update(outcome=outcome, ok=outcome == "completed")
        if error is not None:
            entry["error"] = error[:512]
        self.active = None
        if self.emit is not None:
            self.emit(entry)

    def _control(self, link: SocketIO, code: str, deadline: float) -> None:
        assert self.active is not None
        link.write_all(
            CONTROL.pack(CONTROL_MAGIC, self.active.candidate.candidate_id, CONTROL_CODES[code], 0),
            deadline, "backend control",
        )
        self.active.progress_at = time.monotonic() + PROGRESS_SECONDS

    def _read(
        self, link: SocketIO, count: int, deadline: float | None, label: str,
        *, allow_eof: bool = False,
    ) -> bytes | None:
        # Poll only to send progress on this same SSL thread. Partial bytes stay
        # here across polls; neither progress nor fragments reset the deadline.
        data = bytearray()
        while len(data) < count:
            now = time.monotonic()
            if deadline is not None and now >= deadline:
                raise TransportTimeout(f"{label}: deadline expired")
            wait_until = deadline
            turn = self.active
            if turn is not None and turn.phase in ("capture", "drain"):
                assert deadline is not None
                if now >= turn.progress_at:
                    self._control(link, "speech-progress", deadline)
                wait_until = min(deadline, turn.progress_at)
            try:
                block = link.read_some(count - len(data), wait_until, label)
            except TransportTimeout:
                if deadline is not None and wait_until is not None and wait_until < deadline:
                    continue
                raise
            if not block:
                if not data and allow_eof:
                    return None
                raise ProtocolError(f"{label}: truncated EOF ({len(data)}/{count} bytes)")
            data.extend(block)
        return bytes(data)

    def _frame_start(self, link: SocketIO) -> tuple[bytes, float] | None:
        turn = self.active
        label = "unit result" if turn is not None and turn.phase == "receipt" else "frame header"
        if turn is not None and turn.phase == "cancel_pending":
            label = "audio ended before backend endpoint (awaiting cancellation)"
        wait_until = None if turn is None else (
            turn.deadline if turn.deadline is not None else time.monotonic() + self.io_timeout
        )
        first = self._read(link, 1, wait_until, label, allow_eof=turn is None)
        if first is None:
            return None
        deadline = time.monotonic() + self.io_timeout
        if turn is not None and turn.deadline is not None:
            deadline = min(deadline, turn.deadline)
        rest = self._read(link, 7, deadline, label)
        assert rest is not None
        return first + rest, deadline

    def _current(self, candidate_id: int) -> _Turn | None:
        if candidate_id in self.superseded:
            return None
        if self.active is None or candidate_id != self.active.candidate.candidate_id:
            raise ProtocolError("foreign, future, or unknown stale frame identity")
        return self.active

    def _begin(self, link: SocketIO, magic: bytes, deadline: float) -> None:
        if self.count == 0xFFFFFFFF:
            raise ProtocolError("request counter exhausted; reconnect with a new nonce")
        rest = self._read(link, REQUEST.size - len(magic), deadline, "request header")
        assert rest is not None
        candidate = parse_request(
            magic + rest, nonce=self.nonce, counter=self.count + 1,
            previous_monotonic_ms=self.previous_monotonic_ms, source=self.source,
        )
        if self.active is not None:
            self._finish("superseded")
        entry = self._entry()
        entry.update(
            candidate_id=candidate.candidate_id,
            source="fixture" if candidate.flags & 1 else "socket",
            followup=bool(candidate.flags & 4), wake_bytes=candidate.wake_bytes,
            score=candidate.confidence, threshold=candidate.threshold,
        )
        turn = _Turn(candidate, entry)
        self.active = turn
        self.count += 1
        self.previous_monotonic_ms = candidate.monotonic_ms
        self._control(link, "speech.startDetected", deadline)
        remaining = candidate.wake_bytes
        while remaining:
            pcm = self._read(link, min(remaining, MAX_AUDIO_FRAME_BYTES), deadline, "wake PCM")
            assert pcm is not None
            turn.add_pcm(pcm, postwake=False)
            remaining -= len(pcm)

    def _speech_end(self, link: SocketIO, reason: str, deadline: float) -> None:
        assert self.active is not None
        self._control(link, "speech.endDetected", deadline)
        self.active.phase = "drain"
        self.active.deadline = time.monotonic() + self.io_timeout
        self.active.entry.update(
            endpoint_reason=reason, endpoint_postwake_bytes=self.active.postwake_bytes,
        )

    def _reply(self, link: SocketIO) -> None:
        assert self.active is not None
        turn = self.active
        deadline = time.monotonic() + self.io_timeout
        link.write_all(
            REPLY.pack(REPLY_MAGIC, turn.candidate.candidate_id, VERDICTS["accept"], len(self.reply_wav)),
            deadline, "reply header",
        )
        value, digest = 0x811C9DC5, hashlib.sha256()
        sent = 0
        with closing(self.reply_wav.chunks()) as chunks:
            for block in chunks:
                link.write_all(
                    block, time.monotonic() + self.io_timeout, "reply WAV",
                    stall_timeout=self.io_timeout,
                )
                sent += len(block)
                value = fnv1a(block, value)
                digest.update(block)
        if sent != len(self.reply_wav) or value != self.reply_hash or (
            digest.hexdigest() != self.reply_wav.sha256
        ):
            raise ProtocolError("reply WAV changed during transmission")
        turn.entry["reply_sent"] = True
        self._control(link, "turn.end", time.monotonic() + self.io_timeout)
        turn.entry["turn_end_sent"] = True
        turn.phase = "receipt"
        extra = self.reply_wav.duration_seconds + 5 if self.playback else 0
        turn.deadline = time.monotonic() + self.io_timeout + extra

    def _audio(self, link: SocketIO, raw: bytes, deadline: float) -> None:
        frame = parse_audio(raw)
        turn = self._current(frame.candidate_id)
        if turn is not None:
            if turn.phase in ("receipt", "cancel_pending"):
                raise ProtocolError("audio after end marker")
            if frame.sequence != turn.next_sequence:
                raise ProtocolError("audio sequence/order mismatch")
            turn.next_sequence += 1
        if frame.length:
            pcm = self._read(link, frame.length, deadline, "stream PCM")
            assert pcm is not None
            if turn is not None:
                turn.add_pcm(pcm, postwake=True)
                if turn.phase == "capture" and turn.postwake_bytes >= self.stub_end_after_ms * BYTES_PER_MS:
                    self._speech_end(link, "stub_sample_limit", time.monotonic() + self.io_timeout)
        elif turn is not None:
            if turn.phase == "capture":
                if not turn.candidate.flags & 1:
                    # Local controls close audio before their CANCELLED
                    # receipt. This is not device VAD and must not elicit a reply.
                    turn.phase = "cancel_pending"
                    turn.deadline = time.monotonic() + self.io_timeout
                    return
                self._speech_end(link, "fixture_eof", time.monotonic() + self.io_timeout)
            turn.entry["capture_complete"] = True
            self._reply(link)

    def _result(self, raw: bytes) -> None:
        magic, candidate_id, status, reply_hash = RESULT.unpack(raw)
        if magic != RESULT_MAGIC:
            raise ProtocolError("wrong result magic")
        turn = self._current(candidate_id)
        if status not in STATUS_NAMES:
            raise ProtocolError("unknown unit result status")
        if turn is not None:
            turn.entry.update(status_code=status, status=STATUS_NAMES[status], reply_fnv1a=reply_hash)
        if status in (0, 8):
            if reply_hash != self.reply_hash:
                raise ProtocolError("reply WAV hash mismatch")
        elif reply_hash not in (0, self.reply_hash):
            raise ProtocolError("failed result must have zero or matching complete reply hash")
        if turn is None:
            return
        if status == 2:
            self.result_count += 1
            self.status_counts["cancelled"] += 1
            self._finish("cancelled")
            return
        if status in (3, 4, 5, 6, 7):
            self.result_count += 1
            self.status_counts[STATUS_NAMES[status]] += 1
            raise UnitFailure(f"unit reported {STATUS_NAMES[status]} (status {status})")
        if turn.phase != "receipt":
            raise ProtocolError("unit result arrived before reply and turn.end")
        expected = 0 if self.playback else 8
        if status != expected:
            raise ProtocolError(f"unexpected unit status {status}; expected {expected}")
        self.result_count += 1
        self.status_counts[STATUS_NAMES[status]] += 1
        self._finish("completed")

    def run(self, link: SocketIO) -> dict:
        if self._started:
            raise ValueError("a receiver handles exactly one nonce-scoped stream")
        self._started = True
        while True:
            try:
                frame = self._frame_start(link)
                if frame is None:
                    return self.summary()
                magic, deadline = frame
                if magic == REQUEST_MAGIC:
                    self._begin(link, magic, deadline)
                elif magic in (AUDIO_MAGIC, RESULT_MAGIC):
                    rest = self._read(link, AUDIO.size - len(magic), deadline, "frame header")
                    assert rest is not None
                    if magic == AUDIO_MAGIC:
                        self._audio(link, magic + rest, deadline)
                    else:
                        self._result(magic + rest)
                else:
                    raise ProtocolError("wrong frame magic")
            except ProtocolError as error:
                self._finish("failed", error=str(error))
                raise
