#!/usr/bin/env python3
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
"""Exercise RIWAKE03 with the unchanged private donor under QEMU.

Run with REINVOKE_WORKER_LAB pointing at the retained lab containing bundle/
and hey-cortana-padded.pcm: python -m unittest discover -s tools/voice-endpoint/worker.
Only project-local scratch and isolated bwrap namespaces are changed; no audio device
or hardware is exposed. The retained version-2 lab and tests are never modified.
"""

from __future__ import annotations

import contextlib
import fcntl
import math
import os
from pathlib import Path
import queue
import resource
import selectors
import shutil
import signal
import socket
import struct
import subprocess
import tempfile
import threading
import time
import unittest

HERE = Path(__file__).resolve().parent
WAKE = struct.Struct("<8sHHQQdfiIIHHII")
FRAME = struct.Struct("<8sQII")
NONCE = 171
REPLY_RING = 65536


def read_exact(fd: int, length: int, timeout: float = 8) -> bytes:
    data = bytearray()
    deadline = time.monotonic() + timeout
    with selectors.DefaultSelector() as selector:
        selector.register(fd, selectors.EVENT_READ)
        while len(data) < length:
            left = deadline - time.monotonic()
            if left <= 0 or not selector.select(left):
                raise TimeoutError(f"missing {length - len(data)} bytes")
            part = os.read(fd, length - len(data))
            if not part:
                raise EOFError(f"missing {length - len(data)} bytes")
            data.extend(part)
    return bytes(data)


def write_all(fd: int, data: bytes, timeout: float = 8) -> None:
    deadline = time.monotonic() + timeout
    view = memoryview(data)
    with selectors.DefaultSelector() as selector:
        selector.register(fd, selectors.EVENT_WRITE)
        while view:
            left = deadline - time.monotonic()
            if left <= 0 or not selector.select(left):
                raise TimeoutError("worker did not accept host bytes")
            try:
                count = os.write(fd, view[:4096])
            except BlockingIOError:
                continue
            view = view[count:]


def wave(samples: int = 1600, alternate: bool = False) -> bytes:
    pcm = b"".join(
        struct.pack("<h", (-417 if i % 2 else 623) if alternate else int(math.sin(i * 0.06) * 100))
        for i in range(samples)
    )
    return pcm_wave(pcm)


def pcm_wave(pcm: bytes) -> bytes:
    return (
        b"RIFF" + struct.pack("<I", 36 + len(pcm)) + b"WAVEfmt "
        + struct.pack("<IHHIIHH", 16, 1, 1, 16000, 32000, 2, 16)
        + b"data" + struct.pack("<I", len(pcm)) + pcm
    )


def long_wave(seconds: int) -> bytes:
    return pcm_wave(b"\xff\x7f\0\x80" * (seconds * 8000))


def chunked_wave() -> tuple[bytes, bytes]:
    pcm = b"\xff\x7f\0\x80" * 2000
    chunks = [
        (b"JUNK", b"arbitrary text, not a reply policy"),
        (b"fmt ", struct.pack("<HHIIHHH", 1, 1, 16000, 32000, 2, 16, 0)),
        (b"LIST", b"uninspected transcript metadata"),
        (b"data", pcm), (b"JUNK", b"tail!"),
    ]
    body = b"WAVE" + b"".join(
        name + struct.pack("<I", len(data)) + data + (b"\0" if len(data) % 2 else b"")
        for name, data in chunks
    )
    return b"RIFF" + struct.pack("<I", len(body)) + body, pcm


def fnv(data: bytes) -> int:
    result = 2166136261
    for byte in data:
        result = ((result ^ byte) * 16777619) & 0xFFFFFFFF
    return result


class Capture:
    def __init__(self, path: Path, initial: bytes, *, header_only: bool = False) -> None:
        self.listener = socket.socket(socket.AF_UNIX)
        self.listener.bind(str(path))
        self.listener.listen(1)
        self.listener.settimeout(5)
        self.stopped = threading.Event()
        self.connected = threading.Event()
        self.gap_next = threading.Event()
        self.segments: queue.Queue[bytes] = queue.Queue()
        self.feed(initial)
        self.header_only = header_only
        self.interval = 0.001
        self.error: BaseException | None = None
        self.connection: socket.socket | None = None
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.thread.start()

    def feed(self, pcm: bytes) -> None:
        wide = b"".join(struct.pack("<i", sample[0] * 65536) * 3 for sample in struct.iter_unpack("<h", pcm))
        self.segments.put(wide)

    def run(self) -> None:
        try:
            self.connection, _ = self.listener.accept()
            with self.connection as connection:
                connection.settimeout(2)
                connection.sendall(struct.pack("<8sHHIHHIQ", b"RINVOMIC", 1, 32, 48000, 1, 1, 256, 17))
                self.connected.set()
                pending = bytearray()
                sequence = 0
                while not self.stopped.is_set():
                    if self.header_only:
                        self.stopped.wait(0.01)
                        continue
                    while len(pending) < 1024:
                        try:
                            pending.extend(self.segments.get_nowait())
                        except queue.Empty:
                            pending.extend(bytes(1024 - len(pending)))
                    if self.gap_next.is_set():
                        self.gap_next.clear()
                        sequence += 1
                    connection.sendall(struct.pack("<QQQ", 17, sequence, sequence * 5333333) + pending[:1024])
                    del pending[:1024]
                    sequence += 1
                    self.stopped.wait(self.interval)
        except (BrokenPipeError, ConnectionResetError):
            pass  # The worker closes capture on an explicit session failure.
        except (OSError, TimeoutError) as error:
            if not self.stopped.is_set():
                self.error = error

    def close(self) -> None:
        self.stopped.set()
        self.listener.close()
        if self.connection is not None:
            with contextlib.suppress(OSError):
                self.connection.shutdown(socket.SHUT_RDWR)
        self.thread.join(timeout=3)
        if self.thread.is_alive():
            raise AssertionError("capture test thread leaked")
        if self.error is not None:
            raise AssertionError(f"capture test failed: {self.error}")


class Unit:
    def __init__(
        self, test: WorkerStreamingTests, *, pcm: bytes | None = None,
        live: bool = False, play: bool = False, fake_player: bool = False,
        muted: bool = False, header_only: bool = False, clock: bool = False,
        io_ms: int = 3000, seconds: int = 0, read_output: bool = True,
        extra_env: dict[str, str] | None = None,
    ) -> None:
        self.test = test
        self.directory_owner = tempfile.TemporaryDirectory(prefix="case-", dir=test.build)
        self.directory = Path(self.directory_owner.name)
        self.state = self.directory / "microphone-state"
        self.state.write_text("muted\n" if muted else "unmuted\n", encoding="ascii")
        original = pcm if pcm is not None else test.original
        (self.directory / "input.pcm").write_bytes(original)
        self.capture = Capture(self.directory / "capture.sock", original, header_only=header_only) if live else None
        command = [
            "bwrap", "--unshare-all", "--die-with-parent", "--new-session",
            "--tmpfs", "/", "--proc", "/proc", "--dev", "/dev",
            "--ro-bind", str(test.lab / "bundle"), "/model",
            "--ro-bind", str(test.worker), "/unit-link.so",
            "--bind", str(self.directory), "/test",
            "--dir", "/run", "--ro-bind", str(self.directory), "/run/reinvoke",
            "--ro-bind", "/usr/bin/qemu-arm-static", "/qemu",
        ]
        if clock:
            command += ["--ro-bind", str(test.clock), "/unit-clock.so"]
        if fake_player:
            command += [
                "--dir", "/opt/reinvoke/lib",
                "--ro-bind", str(test.player), "/opt/reinvoke/lib/ld-linux-armhf.so.3",
            ]
        command += ["--clearenv"]
        environment = {
            "KWS_MODEL": "/model/share/heycortana_en-US.table",
            "KWS_UNIT_SOURCE": "socket" if live else "fixture",
            "KWS_UNIT_PCM": "/test/input.pcm", "KWS_UNIT_SOCKET": "/test/capture.sock",
            "KWS_UNIT_NONCE": str(NONCE), "KWS_UNIT_IO_MS": str(io_ms),
            "KWS_UNIT_SECONDS": str(seconds), "KWS_UNIT_PLAY": str(int(play)),
            "KWS_UNIT_LOCK": "/test/unit-link.lock", "TMPDIR": "/test",
        }
        environment.update(extra_env or {})
        for key, value in environment.items():
            command += ["--setenv", key, value]
        preload = "/unit-link.so:/unit-clock.so" if clock else "/unit-link.so"
        command += [
            "/qemu", "-E", f"LD_PRELOAD={preload}", "/model/lib/ld-linux-armhf.so.3",
            "--library-path", "/model/lib", "/model/bin/cortana",
        ]
        self.process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        assert self.process.stdin is not None and self.process.stdout is not None and self.process.stderr is not None
        os.set_blocking(self.process.stdin.fileno(), False)
        self.lines: list[str] = []
        self.log_changed = threading.Condition()
        self.frames: queue.Queue[tuple[tuple, bytes] | BaseException] = queue.Queue()
        self.writers: list[threading.Thread] = []
        self.log_thread = threading.Thread(target=self.drain_logs, daemon=True)
        self.log_thread.start()
        self.output_thread = threading.Thread(target=self.drain_output, daemon=True) if read_output else None
        if self.output_thread is not None:
            self.output_thread.start()
        self.closed = False
        test.addCleanup(self.close)

    def drain_logs(self) -> None:
        assert self.process.stderr is not None
        for raw in self.process.stderr:
            with self.log_changed:
                self.lines.append(raw.decode("utf-8", errors="replace").rstrip("\n"))
                self.log_changed.notify_all()

    def drain_output(self) -> None:
        assert self.process.stdout is not None
        try:
            while True:
                magic = read_exact(self.process.stdout.fileno(), 8, timeout=15)
                if magic == b"RIWAKE03":
                    header = WAKE.unpack(magic + read_exact(self.process.stdout.fileno(), 56))
                    if header[12] > 160000 or header[12] != header[13]:
                        raise AssertionError(f"unbounded or batched wake header: {header}")
                    data = read_exact(self.process.stdout.fileno(), header[12])
                elif magic in (b"RIAUDIO3", b"RIRESULT"):
                    header = FRAME.unpack(magic + read_exact(self.process.stdout.fileno(), 16))
                    if magic == b"RIAUDIO3" and (header[3] > 3200 or header[3] % 2):
                        raise AssertionError(f"invalid streaming audio frame: {header}")
                    data = read_exact(self.process.stdout.fileno(), header[3]) if magic == b"RIAUDIO3" else b""
                else:
                    raise AssertionError(f"torn output frame: {magic!r}")
                self.frames.put((header, data))
        except (OSError, EOFError, TimeoutError, AssertionError) as error:
            self.frames.put(error)

    def read(self, timeout: float = 8) -> tuple[tuple, bytes]:
        try:
            result = self.frames.get(timeout=timeout)
        except queue.Empty as error:
            raise AssertionError("worker produced no frame:\n" + "\n".join(self.lines)) from error
        if isinstance(result, BaseException):
            raise AssertionError(f"worker output ended: {result}\n" + "\n".join(self.lines)) from result
        return result

    def wake(self, previous: int | None = None) -> tuple[tuple, bytes]:
        while True:
            header, data = self.read()
            if header[0] == b"RIWAKE03":
                self.test.assertEqual(header[1], 3)
                self.test.assertEqual(header[3] >> 32, NONCE)
                self.test.assertEqual(header[9:12], (16000, 1, 1))
                if previous is not None:
                    self.test.assertEqual(header[3], previous + 1)
                return header, data
            self.test.assertEqual(header[0], b"RIAUDIO3")

    def audio_end(self, ident: int, sequence: int = 1) -> bytes:
        audio = bytearray()
        while True:
            header, data = self.read()
            self.test.assertEqual(header[:3], (b"RIAUDIO3", ident, sequence))
            sequence += 1
            if not data:
                return bytes(audio)
            audio.extend(data)

    def receipt(self, ident: int, status: int, reply: bytes | None = None) -> tuple:
        while True:
            header, _ = self.read()
            if header[0] == b"RIAUDIO3":
                continue
            self.test.assertEqual(header[:3], (b"RIRESULT", ident, status))
            if reply is not None:
                self.test.assertEqual(header[3], fnv(reply))
            return header

    def send(self, data: bytes) -> None:
        assert self.process.stdin is not None
        write_all(self.process.stdin.fileno(), data)

    def control(self, ident: int, code: int) -> None:
        self.send(FRAME.pack(b"RICTRL03", ident, code, 0))

    def reply(self, ident: int, data: bytes, verdict: int = 0) -> None:
        self.send(FRAME.pack(b"RIREPLY3", ident, verdict, len(data)) + data)

    def start_send(self, data: bytes) -> tuple[threading.Thread, list[BaseException]]:
        failures: list[BaseException] = []

        def send() -> None:
            try:
                self.send(data)
            except BaseException as error:
                failures.append(error)

        thread = threading.Thread(target=send, daemon=True)
        self.writers.append(thread)
        thread.start()
        return thread, failures

    def wait_log(self, text: str, count: int = 1, timeout: float = 6) -> None:
        deadline = time.monotonic() + timeout
        with self.log_changed:
            while sum(text in line for line in self.lines) < count:
                left = deadline - time.monotonic()
                if left <= 0:
                    raise AssertionError(f"missing worker log {text!r}:\n" + "\n".join(self.lines))
                self.log_changed.wait(left)

    def wait_file(self, name: str, timeout: float = 5) -> bytes:
        path = self.directory / name
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if path.exists():
                return path.read_bytes()
            time.sleep(0.01)
        raise AssertionError(f"fake player did not produce {name}:\n" + "\n".join(self.lines))

    def set_clock(self, milliseconds: int) -> None:
        temporary = self.directory / "clock-new"
        temporary.write_text(str(milliseconds), encoding="ascii")
        temporary.replace(self.directory / "clock-ms")

    def worker_pid(self) -> int:
        self.wait_log("LOCAL signals ready")
        pending = [self.process.pid]
        while pending:
            pid = pending.pop()
            process = Path(f"/proc/{pid}")
            arguments = (process / "cmdline").read_bytes().split(b"\0")
            if arguments[0] == b"/qemu":
                return pid
            pending.extend(int(child) for child in (
                process / "task" / str(pid) / "children"
            ).read_text().split())
        raise AssertionError("QEMU worker PID not found below its bwrap supervisor")

    def signal(self, number: int) -> None:
        os.kill(self.worker_pid(), number)

    def rss_kib(self) -> int:
        status = Path(f"/proc/{self.worker_pid()}/status").read_text()
        return int(next(line.split()[1] for line in status.splitlines() if line.startswith("VmRSS:")))

    def ended(self, expected: int = 0) -> str:
        self.test.assertEqual(self.process.wait(timeout=6), expected)
        self.log_thread.join(timeout=2)
        self.test.assertFalse(self.log_thread.is_alive(), "stderr thread leaked")
        logs = "\n".join(self.lines)
        self.test.assertIn(f"DONE status={expected}", logs)
        self.test.assertEqual(self.lines[-1], "UNIT_LINK PHASE idle")
        return logs

    def close(self) -> None:
        if self.closed:
            return
        self.closed = True
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=2)
        if self.capture is not None:
            self.capture.close()
        self.log_thread.join(timeout=2)
        if self.output_thread is not None:
            self.output_thread.join(timeout=2)
            self.test.assertFalse(self.output_thread.is_alive(), "stdout thread leaked")
        for pipe in (self.process.stdin, self.process.stdout, self.process.stderr):
            if pipe is not None:
                pipe.close()
        for thread in self.writers:
            thread.join(timeout=2)
            self.test.assertFalse(thread.is_alive(), "reply writer leaked")
        self.directory_owner.cleanup()


class WorkerStreamingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        lab = os.environ.get("REINVOKE_WORKER_LAB")
        if not lab:
            raise unittest.SkipTest("set REINVOKE_WORKER_LAB to the retained private donor lab")
        cls.lab = Path(lab).resolve(strict=True)
        cls.original = (cls.lab / "hey-cortana-padded.pcm").read_bytes()
        for name in ("arm-linux-gnueabihf-gcc", "gcc", "bwrap", "qemu-arm-static"):
            if shutil.which(name) is None:
                raise RuntimeError(f"required existing test tool is missing: {name}")
        cls.build_owner = tempfile.TemporaryDirectory(prefix=".stream-test-", dir=HERE.parents[2])
        cls.addClassCleanup(cls.build_owner.cleanup)
        cls.build = Path(cls.build_owner.name)
        cls.worker, cls.clock, cls.player = (
            cls.build / "unit-link.so", cls.build / "clock.so", cls.build / "fake-player",
        )
        for source, output in ((HERE / "unit_link.c", cls.worker), (HERE / "test_clock.c", cls.clock)):
            subprocess.run(
                ["arm-linux-gnueabihf-gcc", "-O2", "-fPIC", "-shared", "-Wall", "-Wextra", "-Werror",
                 str(source), f"-L{cls.lab / 'bundle/lib'}", "-Wl,--no-as-needed",
                 "-l:libc.so.6", "-l:libm.so.6", "-o", str(output)],
                check=True, capture_output=True, text=True,
                env={**os.environ, "TMPDIR": str(cls.build)},
            )
        subprocess.run(
            ["gcc", "-O2", "-static", "-Wall", "-Wextra", "-Werror",
             str(HERE / "test_player.c"), "-o", str(cls.player)],
            check=True, capture_output=True, text=True,
            env={**os.environ, "TMPDIR": str(cls.build)},
        )

    def test_actual_donor_wake_and_continuing_fixture_pcm_are_exact(self) -> None:
        original = self.original + bytes(32000 * 7)
        unit = Unit(self, pcm=original)
        header, wake_pcm = unit.wake()
        self.assertEqual(header[2], 1)
        self.assertAlmostEqual(header[5], 0.550815582, places=8)
        self.assertAlmostEqual(header[6], 0.42, places=6)
        self.assertEqual(header[7:9], (60, 1345))
        self.assertEqual(header[12:], (47360, 47360))
        continuing = unit.audio_end(header[3])
        self.assertGreater(len(continuing), 5 * 32000)
        self.assertEqual(wake_pcm + continuing, original)
        self.assertIsNone(unit.process.poll(), "fixture EOF must wait for the backend")
        unit.control(header[3], 3)
        logs = unit.ended()
        self.assertIn("clips=0", logs)
        self.assertNotIn("RESULT", logs)
        self.assertIn("UNIT_LINK PHASE listening", unit.lines)
        self.assertNotIn("UNIT_LINK PHASE listening-quiet", unit.lines)

    def test_paced_fixture_matches_16khz_preserves_bytes_and_eof(self) -> None:
        before = resource.getrusage(resource.RUSAGE_CHILDREN)
        started = time.monotonic()
        unit = Unit(self, extra_env={"KWS_UNIT_FIXTURE_PACE": "1"})
        header, wake_pcm = unit.wake()
        self.assertGreaterEqual(time.monotonic() - started, header[12] / 32000 - 0.03)
        continuing = unit.audio_end(header[3])
        elapsed = time.monotonic() - started
        duration = len(self.original) / 32000
        self.assertGreaterEqual(elapsed, duration - 0.02)
        self.assertLess(elapsed, duration + 3)
        self.assertEqual(wake_pcm + continuing, self.original)
        self.assertIsNone(unit.process.poll(), "paced EOF must still wait for turn.end")
        unit.control(header[3], 3)
        unit.ended()
        after = resource.getrusage(resource.RUSAGE_CHILDREN)
        cpu = after.ru_utime + after.ru_stime - before.ru_utime - before.ru_stime
        self.assertLess(cpu, duration * 0.7, "fixture pacing busy-looped instead of polling")

    def test_paced_fixture_controls_and_player_remain_responsive(self) -> None:
        unit = Unit(
            self, pcm=self.original + bytes(32000 * 20), play=True, fake_player=True,
            extra_env={"KWS_UNIT_FIXTURE_PACE": "1"},
        )
        header, _ = unit.wake()
        ident = header[3]
        started = time.monotonic()
        unit.control(ident, 6)
        unit.control(ident, 2)
        unit.audio_end(ident)
        self.assertLess(time.monotonic() - started, 0.5)
        unit.wait_log(f"CONTROL id={ident} code=6")
        clip = wave()
        unit.reply(ident, clip)
        unit.wait_file("player-consumed")
        started = time.monotonic()
        unit.control(ident, 3)
        unit.wait_log(f"CONTROL id={ident} code=3")
        self.assertLess(time.monotonic() - started, 0.5)
        self.assertFalse((unit.directory / "player-stopped").exists())
        (unit.directory / "player-release").touch()
        unit.receipt(ident, 0, clip)
        unit.wait_log(f"TURN_END id={ident}")
        self.assertIsNone(unit.process.poll(), "fixture should still have unread diagnostic input")
        self.assertNotIn("output backpressure", "\n".join(unit.lines))

    def test_fixture_pacing_option_is_strict_and_does_not_pace_socket(self) -> None:
        for value in ("2", "-1", "true", " 1", ""):
            with self.subTest(value=value):
                unit = Unit(self, extra_env={"KWS_UNIT_FIXTURE_PACE": value})
                self.assertIn("ERROR", unit.ended(2))
        # This existing synthetic socket producer runs faster than real time.
        # The diagnostic option must not impose fixture timing on that source.
        started = time.monotonic()
        unit = Unit(self, live=True, extra_env={"KWS_UNIT_FIXTURE_PACE": "1"})
        header, _ = unit.wake()
        self.assertLess(time.monotonic() - started, 1.3)
        unit.control(header[3], 4)
        unit.receipt(header[3], 2)

    def test_paced_fixture_transport_timeout_is_not_delayed(self) -> None:
        unit = Unit(
            self, pcm=self.original + bytes(32000 * 20), io_ms=200,
            extra_env={"KWS_UNIT_FIXTURE_PACE": "1"},
        )
        header, _ = unit.wake()
        ident = header[3]
        unit.control(ident, 2)
        unit.audio_end(ident)
        started = time.monotonic()
        unit.send(FRAME.pack(b"RIREPLY3", ident, 0, 1000) + b"RIFF")
        unit.receipt(ident, 3)
        self.assertLess(time.monotonic() - started, 0.7)
        self.assertIn("input frame transport deadline", unit.ended(3))

    def test_live_audio_runs_past_five_seconds_until_backend_end(self) -> None:
        unit = Unit(self, live=True)
        header, _ = unit.wake()
        ident = header[3]
        total, sequence = 0, 1
        unit.control(ident, 1)
        while total <= 6 * 32000:
            frame, pcm = unit.read()
            self.assertEqual(frame[:3], (b"RIAUDIO3", ident, sequence))
            self.assertTrue(pcm, "device invented an endpoint")
            total += len(pcm)
            sequence += 1
        unit.control(ident, 2)
        unit.audio_end(ident, sequence)
        unit.wait_log("KWS rearmed phase=2")
        unit.control(ident, 3)
        unit.wait_log("TURN_END")
        self.assertIsNone(unit.process.poll())

    def test_progress_extends_recording_beyond_fifteen_seconds(self) -> None:
        unit = Unit(self, live=True, clock=True, io_ms=180000)
        header, _ = unit.wake()
        ident = header[3]
        for count, milliseconds in enumerate((12000, 24000, 36000), start=1):
            unit.set_clock(milliseconds)
            time.sleep(0.04)
            unit.control(ident, 6)
            unit.wait_log(f"CONTROL id={ident} code=6", count)
            self.assertIsNone(unit.process.poll())
        unit.control(ident, 2)
        audio = unit.audio_end(ident)
        self.assertGreater(len(audio), 0)
        unit.control(ident, 3)
        unit.wait_log("TURN_END")
        self.assertNotIn("session liveness deadline", "\n".join(unit.lines))

    def test_missing_backend_activity_hits_liveness_not_audio_cutoff(self) -> None:
        unit = Unit(self, live=True, clock=True, io_ms=180000)
        header, _ = unit.wake()
        unit.set_clock(16000)
        unit.receipt(header[3], 3)
        self.assertIn("session liveness deadline", unit.ended(3))

    def test_two_arbitrary_reply_clips_and_actual_hashes(self) -> None:
        unit = Unit(self)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        first, second = wave(), wave(2000, True)
        self.assertNotEqual(fnv(first), fnv(second))
        for clip in (first, second):
            unit.reply(ident, clip)
            unit.receipt(ident, 8, clip)
            self.assertIsNone(unit.process.poll(), "clip completion is not turn.end")
        unit.control(ident, 3)
        logs = unit.ended()
        self.assertIn("clips=2", logs)
        self.assertNotIn("PHASE speaking", logs)
        self.assertNotIn("PLAYER pid=", logs)

    def test_long_fullscale_replies_are_validated_without_playback(self) -> None:
        unit = Unit(self, io_ms=10000)
        header, _ = unit.wake()
        unit.audio_end(header[3])
        for seconds in (75, 90):
            clip = long_wave(seconds)
            self.assertEqual(len(clip), seconds * 32000 + 44)
            unit.reply(header[3], clip)
            unit.receipt(header[3], 8, clip)
        unit.control(header[3], 3)
        self.assertNotIn("PLAYER pid=", unit.ended())

    def test_inconsistent_riff_sizes_and_format_fail_without_success(self) -> None:
        for failure in ("u32-overflow", "odd-length", "bad-format", "data-overflow", "late-chunk"):
            with self.subTest(failure=failure):
                bad = Unit(self)
                header, _ = bad.wake()
                bad.audio_end(header[3])
                if failure == "odd-length":
                    bad.send(FRAME.pack(b"RIREPLY3", header[3], 0, 47))
                else:
                    clip = bytearray(wave())
                    if failure == "bad-format":
                        clip[0] ^= 1
                    elif failure == "u32-overflow":
                        struct.pack_into("<I", clip, 4, 0xFFFFFFFE)
                    elif failure == "data-overflow":
                        struct.pack_into("<I", clip, 40, 0xFFFFFFFE)
                    else:
                        clip.extend(b"JUNK" + struct.pack("<I", 10))
                        struct.pack_into("<I", clip, 4, len(clip) - 8)
                    bad.reply(header[3], bytes(clip))
                bad.receipt(header[3], 4)
                self.assertNotIn("PLAYER pid=", bad.ended(3))

    def test_long_playback_starts_before_last_chunk_with_constant_worker_memory(self) -> None:
        unit = Unit(self, play=True, fake_player=True, io_ms=10000)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        baseline = unit.rss_kib()
        for seconds in (75, 90):
            clip = long_wave(seconds)
            unit.send(FRAME.pack(b"RIREPLY3", ident, 0, len(clip)) + clip[:1024])
            unit.wait_file("player-started")
            self.assertFalse((unit.directory / "player-consumed").exists())
            self.assertNotIn(f"REPLY validated bytes={len(clip)}", "\n".join(unit.lines))
            (unit.directory / "player-release").touch()
            unit.send(clip[1024:])
            unit.receipt(ident, 0, clip)
            self.assertEqual(
                unit.wait_file("player-consumed").decode().strip(),
                f"{seconds * 32000} {fnv(clip[44:]):08x}",
            )
            self.assertLess(unit.rss_kib() - baseline, 1024, "worker retained reply-sized memory")
            for name in ("player-started", "player-consumed", "player-completed"):
                (unit.directory / name).unlink()
        self.assertEqual(unit.wait_file("player-arguments").decode().splitlines(), [
            "ld-linux-armhf.so.3", "--library-path", "/opt/reinvoke/lib",
            "/opt/reinvoke/bin/aplay", "-D", "voice", "-q", "-t", "raw",
            "-f", "S16_LE", "-r", "16000", "-c", "1", "-",
        ])
        unit.control(ident, 3)
        logs = unit.ended()
        self.assertIn("limit=65536", logs)
        self.assertIn("pcm_bytes=2880000", logs)

    def test_normal_riff_chunks_are_streamed_as_exact_pcm_not_text(self) -> None:
        unit = Unit(self, play=True, fake_player=True)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        clip, pcm = chunked_wave()
        (unit.directory / "player-release").touch()
        for offset in range(0, len(clip), 7):
            if not offset:
                unit.send(FRAME.pack(b"RIREPLY3", ident, 0, len(clip)))
            unit.send(clip[offset:offset + 7])
        unit.receipt(ident, 0, clip)
        self.assertEqual(
            unit.wait_file("player-consumed").decode().strip(), f"{len(pcm)} {fnv(pcm):08x}",
        )
        unit.control(ident, 3)
        unit.ended()

    def test_truncated_or_bad_tail_stops_streamed_player_without_played_receipt(self) -> None:
        for failure in ("truncated", "bad-tail"):
            with self.subTest(failure=failure):
                unit = Unit(self, play=True, fake_player=True)
                header, _ = unit.wake()
                ident = header[3]
                unit.audio_end(ident)
                clip = bytearray(wave())
                if failure == "bad-tail":
                    clip.extend(b"data" + struct.pack("<I", 2) + b"\0\0")
                    struct.pack_into("<I", clip, 4, len(clip) - 8)
                unit.send(FRAME.pack(b"RIREPLY3", ident, 0, len(clip)) + clip[:100])
                unit.wait_file("player-started")
                if failure == "truncated":
                    assert unit.process.stdin is not None
                    unit.process.stdin.close()
                else:
                    unit.send(clip[100:])
                unit.receipt(ident, 7 if failure == "truncated" else 4)
                self.assertNotIn(f"RESULT id={ident} status=0", unit.ended(3))
                self.assertTrue((unit.directory / "player-stopped").exists())

    def test_reply_input_progress_not_total_transfer_time_is_bounded(self) -> None:
        unit = Unit(self, play=True, fake_player=True, io_ms=200)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        clip = wave()
        (unit.directory / "player-release").touch()
        unit.send(FRAME.pack(b"RIREPLY3", ident, 0, len(clip)))
        started = time.monotonic()
        for offset in range(0, len(clip), 128):
            unit.send(clip[offset:offset + 128])
            time.sleep(0.025)
        unit.receipt(ident, 0, clip)
        self.assertGreater(time.monotonic() - started, 0.6)
        unit.control(ident, 3)
        unit.ended()

    def test_player_backpressure_keeps_capture_and_progress_alive_past_thirty_seconds(self) -> None:
        unit = Unit(
            self, live=True, play=True, fake_player=True, clock=True, io_ms=30000,
            extra_env={"KWS_TEST_PLAYER_DELAY_US": "5000"},
        )
        header, _ = unit.wake()
        ident = header[3]
        unit.control(ident, 2)
        unit.audio_end(ident)
        clip = long_wave(90)
        (unit.directory / "player-release").touch()
        thread, failures = unit.start_send(FRAME.pack(b"RIREPLY3", ident, 0, len(clip)) + clip)
        unit.wait_log("REPLY ring full; input paused")
        for milliseconds in range(8000, 88001, 8000):
            unit.set_clock(milliseconds)
            time.sleep(0.035)
            self.assertIsNone(unit.process.poll())
        thread.join(timeout=7)
        self.assertFalse(thread.is_alive(), "healthy player did not relieve backpressure")
        self.assertEqual(failures, [])
        unit.control(ident, 3)
        unit.receipt(ident, 0, clip)
        unit.wait_log(f"TURN_END id={ident}")
        self.assertEqual(
            unit.wait_file("player-consumed").decode().strip(), f"2880000 {fnv(clip[44:]):08x}",
        )
        self.assertIn("ring_peak=65536", "\n".join(unit.lines))
        self.assertNotIn("deadline", "\n".join(unit.lines))

    def test_stalled_player_is_not_misreported_as_stalled_backend(self) -> None:
        unit = Unit(self, play=True, fake_player=True, io_ms=200)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        (unit.directory / "player-hold").touch()
        clip = long_wave(75)
        unit.start_send(FRAME.pack(b"RIREPLY3", ident, 0, len(clip)) + clip)
        unit.wait_log("REPLY ring full; input paused")
        unit.wait_file("player-started")
        unit.receipt(ident, 3)
        logs = unit.ended(3)
        self.assertIn("player progress deadline", logs)
        self.assertNotIn("input frame transport deadline", logs)
        self.assertTrue((unit.directory / "player-stopped").exists())

    def test_local_signals_trigger_supersede_cancel_and_refuse_muted(self) -> None:
        unit = Unit(self, live=True, pcm=bytes(32000))
        unit.wait_log("READY source=socket")
        unit.signal(signal.SIGUSR1)
        header, pcm = unit.wake()
        self.assertEqual(header[2], 4)
        self.assertEqual(header[5:9], (0.0, 0.0, 0, 0))
        self.assertEqual(header[12:], (0, 0))
        self.assertEqual(pcm, b"")
        unit.signal(signal.SIGUSR1)
        unit.receipt(header[3], 2)
        next_header, _ = unit.wake(header[3])
        self.assertEqual(next_header[2], 4)
        rearmed = sum("KWS rearmed phase=0" in line for line in unit.lines)
        unit.signal(signal.SIGUSR2)
        unit.receipt(next_header[3], 2)
        unit.wait_log("reason=local-cancel")
        unit.wait_log("KWS rearmed phase=0", count=rearmed + 1)
        self.assertEqual(unit.lines.count("UNIT_LINK PHASE listening"), 2)
        self.assertNotIn("UNIT_LINK PHASE listening-quiet", unit.lines)
        self.assertIsNone(unit.process.poll())

        muted = Unit(self, live=True, muted=True, header_only=True, pcm=bytes(32000))
        muted.wait_log("READY source=socket")
        muted.signal(signal.SIGUSR1)
        muted.wait_log("LOCAL trigger ignored microphone muted")
        self.assertTrue(muted.frames.empty())
        muted.state.write_text("unmuted\n", encoding="ascii")
        muted.signal(signal.SIGUSR1)
        fresh, _ = muted.wake()
        self.assertEqual(fresh[2], 4)
        muted.signal(signal.SIGUSR2)
        muted.receipt(fresh[3], 2)
        self.assertIsNone(muted.process.poll())

    def test_local_cancel_clears_full_ring_and_drains_old_body(self) -> None:
        unit = Unit(self, play=True, fake_player=True)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        (unit.directory / "player-hold").touch()
        clip = long_wave(75)
        thread, failures = unit.start_send(FRAME.pack(b"RIREPLY3", ident, 0, len(clip)) + clip)
        unit.wait_log("REPLY ring full; input paused")
        unit.wait_file("player-started")
        before = time.monotonic()
        unit.signal(signal.SIGUSR2)
        result = unit.receipt(ident, 2)
        self.assertEqual(result[3], 0, "partial reply must not claim a complete hash")
        self.assertLess(time.monotonic() - before, 0.8)
        thread.join(timeout=3)
        self.assertEqual(failures, [])
        self.assertFalse(thread.is_alive())
        self.assertNotIn("REPLY validated", unit.ended())
        self.assertTrue((unit.directory / "player-stopped").exists())
        self.assertFalse((unit.directory / "player-completed").exists())

    def test_new_signal_or_keyword_during_full_ring_cannot_splice_old_reply(self) -> None:
        for keyword in (False, True):
            with self.subTest(keyword=keyword):
                unit = Unit(self, live=True, play=True, fake_player=True)
                header, _ = unit.wake()
                old = header[3]
                unit.control(old, 2)
                unit.audio_end(old)
                (unit.directory / "player-hold").touch()
                clip = long_wave(75)
                thread, failures = unit.start_send(FRAME.pack(b"RIREPLY3", old, 0, len(clip)) + clip)
                unit.wait_log("REPLY ring full; input paused")
                unit.wait_file("player-started")
                if keyword:
                    assert unit.capture is not None
                    unit.capture.feed(self.original)
                else:
                    unit.signal(signal.SIGUSR1)
                unit.receipt(old, 2)
                current, _ = unit.wake(old)
                if keyword:
                    self.assertIn(current[2], (0, 2))  # The unchanged donor can clip the retained wake.
                else:
                    self.assertEqual(current[2], 4)
                thread.join(timeout=3)
                self.assertFalse(thread.is_alive())
                self.assertEqual(failures, [])
                unit.wait_file("player-stopped")
                (unit.directory / "player-hold").unlink()
                (unit.directory / "player-release").touch()
                unit.control(old, 3)
                unit.control(current[3], 2)
                unit.audio_end(current[3])
                fresh = wave(1000, True)
                unit.reply(current[3], fresh)
                unit.control(current[3], 3)
                unit.receipt(current[3], 0, fresh)
                unit.wait_log(f"TURN_END id={current[3]}")
                self.assertIn(f"STALE control id={old}", "\n".join(unit.lines))

    def test_mute_and_sigterm_stop_player_even_with_full_ring(self) -> None:
        for terminate in (False, True):
            with self.subTest(terminate=terminate):
                unit = Unit(self, play=True, fake_player=True)
                header, _ = unit.wake()
                ident = header[3]
                unit.audio_end(ident)
                (unit.directory / "player-hold").touch()
                clip = long_wave(75)
                unit.start_send(FRAME.pack(b"RIREPLY3", ident, 0, len(clip)) + clip)
                unit.wait_log("REPLY ring full; input paused")
                unit.wait_file("player-started")
                if terminate:
                    unit.signal(signal.SIGTERM)
                else:
                    unit.state.write_text("muted\n", encoding="ascii")
                    unit.receipt(ident, 2)
                self.assertNotIn(f"RESULT id={ident} status=0", unit.ended(130 if terminate else 3))
                self.assertTrue((unit.directory / "player-stopped").exists())

    def test_turn_end_does_not_truncate_pending_playback(self) -> None:
        unit = Unit(self, play=True, fake_player=True)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        clip = wave(5000, True)
        unit.reply(ident, clip)
        consumed = unit.wait_file("player-consumed")
        unit.control(ident, 3)
        unit.wait_log(f"CONTROL id={ident} code=3")
        time.sleep(0.05)
        self.assertIsNone(unit.process.poll())
        self.assertFalse((unit.directory / "player-stopped").exists())
        self.assertNotIn("TURN_END", "\n".join(unit.lines))
        (unit.directory / "player-release").touch()
        unit.receipt(ident, 0, clip)
        unit.ended()
        self.assertEqual(consumed.decode().strip(), f"{len(clip) - 44} {fnv(clip[44:]):08x}")
        self.assertTrue((unit.directory / "player-completed").exists())

    def test_sequential_arbitrary_clips_report_only_completed_playback(self) -> None:
        unit = Unit(self, play=True, fake_player=True)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        (unit.directory / "player-release").touch()
        for clip in (wave(400), wave(1000, True)):
            unit.reply(ident, clip)
            unit.receipt(ident, 0, clip)
            consumed = unit.wait_file("player-completed").decode().strip()
            self.assertEqual(consumed, f"{len(clip) - 44} {fnv(clip[44:]):08x}")
            (unit.directory / "player-completed").unlink()
            self.assertIsNone(unit.process.poll())
        unit.control(ident, 3)
        self.assertIn("clips=2", unit.ended())

    def test_backend_followup_starts_fresh_id_without_a_wake(self) -> None:
        unit = Unit(self)
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        unit.control(ident, 5)
        unit.control(ident, 3)
        next_header, audio = unit.wake(ident)
        self.assertEqual(next_header[2], 5)
        self.assertEqual(next_header[5:9], (0.0, 0.0, 0, 0))
        self.assertEqual(next_header[12:], (0, 0))
        self.assertEqual(audio, b"")
        self.assertEqual(unit.audio_end(next_header[3]), b"")
        unit.control(next_header[3], 3)
        self.assertIn("turns=2", unit.ended())
        self.assertEqual(unit.lines.count("UNIT_LINK PHASE listening"), 2)
        self.assertNotIn("UNIT_LINK PHASE listening-quiet", unit.lines)

    def test_live_followup_streams_pcm_without_another_keyword(self) -> None:
        unit = Unit(self, live=True)
        header, _ = unit.wake()
        old = header[3]
        unit.control(old, 2)
        unit.audio_end(old)
        unit.control(old, 5)
        unit.control(old, 3)
        next_header, audio = unit.wake(old)
        current = next_header[3]
        self.assertEqual(next_header[2], 4)
        self.assertEqual(next_header[5:9], (0.0, 0.0, 0, 0))
        self.assertEqual(next_header[12:], (0, 0))
        self.assertEqual(audio, b"")
        for sequence in range(1, 4):
            frame, pcm = unit.read()
            self.assertEqual(frame[:3], (b"RIAUDIO3", current, sequence))
            self.assertTrue(pcm)
        unit.control(current, 2)
        unit.audio_end(current, 4)
        unit.control(current, 3)
        unit.wait_log(f"TURN_END id={current}")

    def test_kws_barge_in_thinking_skips_fragmented_old_reply(self) -> None:
        unit = Unit(self, live=True)
        header, _ = unit.wake()
        old = header[3]
        unit.control(old, 2)
        unit.audio_end(old)
        unit.wait_log("KWS rearmed phase=2")
        old_clip = wave()
        unit.send(FRAME.pack(b"RIREPLY3", old, 0, len(old_clip)) + old_clip[:100])
        assert unit.capture is not None
        unit.capture.feed(self.original)
        unit.receipt(old, 2)
        next_header, _ = unit.wake(old)
        current = next_header[3]
        unit.send(old_clip[100:] + FRAME.pack(b"RICTRL03", old, 3, 0))
        unit.control(current, 2)
        unit.audio_end(current)
        clip = wave(1000, True)
        unit.reply(current, clip)
        unit.receipt(current, 8, clip)
        unit.control(current, 3)
        unit.wait_log(f"TURN_END id={current}")
        self.assertIn(f"STALE control id={old}", "\n".join(unit.lines))

    def test_kws_barge_in_playback_stops_and_reaps_player(self) -> None:
        unit = Unit(self, live=True, play=True, fake_player=True)
        header, _ = unit.wake()
        old = header[3]
        unit.control(old, 2)
        unit.audio_end(old)
        unit.reply(old, wave(4000))
        unit.wait_file("player-consumed")
        assert unit.capture is not None
        unit.capture.feed(self.original)
        unit.receipt(old, 2)
        next_header, _ = unit.wake(old)
        unit.wait_file("player-stopped")
        unit.wait_log("PLAYER stopped")
        self.assertFalse((unit.directory / "player-completed").exists())
        unit.control(next_header[3], 4)
        unit.receipt(next_header[3], 2)
        unit.wait_log(f"TURN_END id={next_header[3]}")

    def test_cancel_remains_readable_during_streaming(self) -> None:
        unit = Unit(self, live=True)
        header, _ = unit.wake()
        unit.control(header[3], 4)
        unit.audio_end(header[3])
        unit.receipt(header[3], 2)
        unit.wait_log("reason=backend")
        self.assertIsNone(unit.process.poll())

    def test_mute_erases_audio_and_stops_active_player(self) -> None:
        for playback in (False, True):
            with self.subTest(playback=playback):
                unit = Unit(self, play=playback, fake_player=playback)
                header, _ = unit.wake()
                unit.audio_end(header[3])
                if playback:
                    unit.reply(header[3], wave())
                    unit.wait_file("player-consumed")
                unit.state.write_text("muted\n", encoding="ascii")
                unit.receipt(header[3], 2)
                unit.ended(3)
                if playback:
                    self.assertTrue((unit.directory / "player-stopped").exists())
                    self.assertFalse((unit.directory / "player-completed").exists())

    def test_mute_mid_frame_closes_instead_of_splicing_a_receipt(self) -> None:
        unit = Unit(self, read_output=False)
        assert unit.process.stdout is not None
        fcntl.fcntl(unit.process.stdout.fileno(), fcntl.F_SETPIPE_SZ, 4096)
        header = WAKE.unpack(read_exact(unit.process.stdout.fileno(), 64))
        self.assertEqual(header[0], b"RIWAKE03")
        unit.state.write_text("muted\n", encoding="ascii")
        logs = unit.ended(3)
        remaining = unit.process.stdout.read()
        self.assertLess(len(remaining), header[12])
        self.assertIn("partial outgoing frame", logs)
        self.assertNotIn("RESULT id=", logs)

    def test_cancel_and_disconnect_stop_playback_without_success(self) -> None:
        for disconnect in (False, True):
            with self.subTest(disconnect=disconnect):
                unit = Unit(self, play=True, fake_player=True)
                header, _ = unit.wake()
                ident = header[3]
                unit.audio_end(ident)
                unit.reply(ident, wave())
                unit.wait_file("player-consumed")
                if disconnect:
                    assert unit.process.stdin is not None
                    unit.process.stdin.close()
                else:
                    unit.control(ident, 4)
                unit.receipt(ident, 7 if disconnect else 2)
                unit.ended(3 if disconnect else 0)
                self.assertTrue((unit.directory / "player-stopped").exists())
                self.assertFalse((unit.directory / "player-completed").exists())
                self.assertNotIn(f"RESULT id={ident} status=0", "\n".join(unit.lines))

    def test_unresponsive_player_is_killed_and_reaped_on_cancel(self) -> None:
        unit = Unit(self, play=True, fake_player=True)
        (unit.directory / "player-ignore-term").touch()
        header, _ = unit.wake()
        ident = header[3]
        unit.audio_end(ident)
        unit.reply(ident, wave())
        unit.wait_file("player-consumed")
        unit.control(ident, 4)
        unit.receipt(ident, 2)
        logs = unit.ended()
        self.assertIn("PLAYER stopped", logs)
        self.assertIn("status=9", logs)
        self.assertFalse((unit.directory / "player-completed").exists())

    def test_capture_loss_gap_and_stall_cancel_the_turn(self) -> None:
        for failure in ("socket-closed", "sequence-gap", "stall"):
            with self.subTest(failure=failure):
                unit = Unit(self, live=True)
                header, _ = unit.wake()
                ident = header[3]
                unit.control(ident, 2)
                unit.audio_end(ident)
                assert unit.capture is not None
                if failure == "socket-closed":
                    unit.capture.close()
                elif failure == "sequence-gap":
                    unit.capture.gap_next.set()
                else:
                    unit.capture.header_only = True
                unit.receipt(ident, 2)
                self.assertIn("ERROR capture", unit.ended(3))

    def test_initial_muted_header_without_audio_has_no_capture_timeout(self) -> None:
        unit = Unit(self, live=True, muted=True, header_only=True)
        unit.wait_log("READY source=socket")
        time.sleep(2.2)
        self.assertIsNone(unit.process.poll())
        self.assertNotIn("capture stall", "\n".join(unit.lines))
        self.assertTrue(unit.frames.empty())

    def test_host_disconnect_and_partial_reply_timeout_are_failures(self) -> None:
        for disconnect in (True, False):
            with self.subTest(disconnect=disconnect):
                unit = Unit(self, io_ms=200)
                header, _ = unit.wake()
                unit.audio_end(header[3])
                unit.send(FRAME.pack(b"RIREPLY3", header[3], 0, 1000) + b"RIFF")
                if disconnect:
                    assert unit.process.stdin is not None
                    unit.process.stdin.close()
                unit.receipt(header[3], 7 if disconnect else 3)
                logs = unit.ended(3)
                self.assertNotIn("PHASE speaking", logs)

    def test_reply_requires_endpoint_and_sequential_clip_completion(self) -> None:
        streaming = Unit(self, live=True)
        header, _ = streaming.wake()
        streaming.send(FRAME.pack(b"RIREPLY3", header[3], 0, len(wave())))
        streaming.receipt(header[3], 4)
        self.assertIn("reply before audio end", streaming.ended(3))
        playing = Unit(self, play=True, fake_player=True)
        header, _ = playing.wake()
        playing.audio_end(header[3])
        playing.reply(header[3], wave())
        playing.wait_file("player-consumed")
        playing.send(FRAME.pack(b"RIREPLY3", header[3], 0, len(wave())))
        playing.receipt(header[3], 4)
        playing.ended(3)
        self.assertTrue((playing.directory / "player-stopped").exists())
        self.assertFalse((playing.directory / "player-completed").exists())

    def test_malformed_foreign_future_and_stale_frames(self) -> None:
        for failure in ("future", "foreign", "zero-counter", "bad-code", "control-payload", "v2-magic"):
            with self.subTest(failure=failure):
                unit = Unit(self)
                header, _ = unit.wake()
                ident = header[3]
                unit.audio_end(ident)
                bad_id, code, length, magic = ident, 6, 0, b"RICTRL03"
                if failure == "future":
                    bad_id += 1
                elif failure == "foreign":
                    bad_id ^= 1 << 32
                elif failure == "zero-counter":
                    bad_id = NONCE << 32
                elif failure == "bad-code":
                    code = 7
                elif failure == "control-payload":
                    length = 2
                else:
                    magic = b"RIREPLY2"
                unit.send(FRAME.pack(magic, bad_id, code, length))
                unit.receipt(ident, 4)
                unit.ended(3)
        unit = Unit(self)
        header, _ = unit.wake()
        old = header[3]
        unit.audio_end(old)
        unit.control(old, 5)
        unit.control(old, 3)
        next_header, _ = unit.wake(old)
        current = next_header[3]
        unit.audio_end(current)
        unit.reply(old, wave())
        unit.control(old, 4)
        clip = wave(500, True)
        unit.reply(current, clip)
        unit.receipt(current, 8, clip)
        unit.control(current, 3)
        logs = unit.ended()
        self.assertIn("STALE reply", logs)
        self.assertIn("STALE control", logs)

    def test_unread_output_is_explicit_bounded_backpressure(self) -> None:
        for live in (False, True):
            with self.subTest(live=live):
                unit = Unit(
                    self, pcm=self.original + bytes(32000 * 30), live=live,
                    read_output=False, io_ms=10000,
                )
                if unit.capture is not None:
                    # Exercise queue exhaustion, not a scheduling-dependent
                    # race against the separate frame transport deadline.
                    unit.capture.interval = 0
                logs = unit.ended(3)
                self.assertIn("output backpressure", logs)
                self.assertIn("limit=320000", logs)
                self.assertNotIn("status=0", logs)

    def test_missing_player_is_never_reported_as_played(self) -> None:
        unit = Unit(self, play=True)
        header, _ = unit.wake()
        unit.audio_end(header[3])
        unit.reply(header[3], wave())
        unit.receipt(header[3], 5)
        self.assertIn("player exec", unit.ended(3))

    def test_no_candidate_and_numeric_environment_guards(self) -> None:
        unit = Unit(self, pcm=bytes(32000))
        self.assertIn("turns=0", unit.ended())
        for field, value in (
            ("KWS_UNIT_NONCE", "0"), ("KWS_UNIT_NONCE", "4294967296"),
            ("KWS_UNIT_SECONDS", "-1"), ("KWS_UNIT_SECONDS", "86401"),
            ("KWS_UNIT_SECONDS", " 1"), ("KWS_UNIT_IO_MS", "199"),
            ("KWS_UNIT_PLAY", "true"),
        ):
            with self.subTest(field=field, value=value):
                bad = Unit(self, extra_env={field: value})
                self.assertIn("ERROR", bad.ended(2))


if __name__ == "__main__":
    unittest.main()
