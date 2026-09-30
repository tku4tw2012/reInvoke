// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fakeRequest() []byte {
	return append([]byte("RIWAKE03"), bytes.Repeat([]byte{0, 255, 17, 10}, 40000)...)
}

func fakeReply() []byte {
	return append([]byte("RIREPLY3"), bytes.Repeat([]byte{0, 255, 10, 128, 1, 2, 3, 4}, 131072)...)
}

func fakeReceipt() []byte {
	return append([]byte("RIRESULT"), bytes.Repeat([]byte{0xa5}, 16)...)
}

func helperCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestVoiceWorkerProcess$")
	cmd.Env = []string{
		"REINVOKE_TEST_WORKER=1", "REINVOKE_TEST_MODE=" + mode,
		"GORACE=atexit_sleep_ms=0", "GOMAXPROCS=2",
	}
	return cmd
}

// This helper is the only executable used by the supervisor tests. It never
// loads donor code, opens audio devices, or contacts an external service.
func TestVoiceWorkerProcess(t *testing.T) {
	if os.Getenv("REINVOKE_TEST_WORKER") != "1" {
		return
	}
	emitPhase := func(phase string) {
		if os.Getenv("REINVOKE_TEST_QUIET_PHASES") != "1" {
			fmt.Fprintln(os.Stderr, "UNIT_LINK PHASE "+phase)
		}
	}
	switch os.Getenv("REINVOKE_TEST_MODE") {
	case "exchange":
		emitPhase("listening")
		emitPhase("thinking")
		if err := writeAll(os.Stdout, fakeRequest()); err != nil {
			os.Exit(31)
		}
		reply := make([]byte, len(fakeReply()))
		if _, err := io.ReadFull(os.Stdin, reply); err != nil || !bytes.Equal(reply, fakeReply()) {
			os.Exit(32)
		}
		emitPhase("speaking")
		if err := writeAll(os.Stdout, fakeReceipt()); err != nil {
			os.Exit(33)
		}
		emitPhase("idle")
		os.Exit(0)
	case "eof-zero":
		fmt.Fprint(os.Stdout, "READY\n")
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			os.Exit(34)
		}
		os.Exit(0)
	case "graceful":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		fmt.Fprint(os.Stdout, "READY\n")
		<-signals
		fmt.Fprintln(os.Stderr, "test worker graceful termination")
		os.Exit(0)
	case "controls":
		signals := make(chan os.Signal, 4)
		signal.Notify(signals, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM)
		fmt.Fprint(os.Stdout, "READY\n")
		for {
			switch <-signals {
			case syscall.SIGUSR1:
				emitPhase("listening")
				fmt.Fprint(os.Stdout, "TRIGGER\n")
			case syscall.SIGUSR2:
				emitPhase("idle")
				fmt.Fprint(os.Stdout, "CANCEL\n")
			case syscall.SIGTERM:
				os.Exit(0)
			}
		}
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		fmt.Fprint(os.Stdout, "READY\n")
		time.Sleep(time.Hour)
	case "block-output":
		fmt.Fprintln(os.Stderr, "UNIT_LINK PHASE listening")
		for {
			if err := writeAll(os.Stdout, bytes.Repeat([]byte{0xa5}, 65536)); err != nil {
				os.Exit(35)
			}
		}
	case "block-input":
		fmt.Fprint(os.Stdout, "READY\n")
		time.Sleep(time.Hour)
	case "close-stdout":
		os.Stdout.Close()
		time.Sleep(time.Hour)
	case "zero":
		os.Exit(0)
	case "fail":
		fmt.Fprintln(os.Stderr, "test worker deliberate failure")
		os.Exit(7)
	case "orphan":
		executable, err := os.Executable()
		if err != nil {
			os.Exit(36)
		}
		read, write, err := os.Pipe()
		if err != nil {
			os.Exit(37)
		}
		child := exec.Command(executable, "-test.run=^TestVoiceWorkerProcess$")
		child.Env = []string{
			"REINVOKE_TEST_WORKER=1", "REINVOKE_TEST_MODE=orphan-child",
			"GORACE=atexit_sleep_ms=0", "GOMAXPROCS=2",
		}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		child.ExtraFiles = []*os.File{write}
		if err := child.Start(); err != nil {
			os.Exit(38)
		}
		write.Close()
		var ready [1]byte
		if _, err := io.ReadFull(read, ready[:]); err != nil {
			os.Exit(39)
		}
		read.Close()
		if err := os.WriteFile(os.Getenv("REINVOKE_TEST_CHILD_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			os.Exit(40)
		}
		// Deliberately do not wait: the supervisor must adopt and reap it.
		os.Exit(7)
	case "orphan-child":
		signal.Ignore(syscall.SIGTERM)
		ready := os.NewFile(3, "ready")
		if _, err := ready.Write([]byte{1}); err != nil {
			os.Exit(41)
		}
		ready.Close()
		time.Sleep(time.Hour)
	}
	os.Exit(42)
}

type bridgeHarness struct {
	host     net.Conn
	cancel   context.CancelFunc
	cmd      *exec.Cmd
	done     <-chan error
	finished <-chan struct{}
	output   *bytes.Buffer
}

func startBridge(t *testing.T, cmd *exec.Cmd, allowClean bool, phase func(voicePhase), controls ...*workerControls) *bridgeHarness {
	t.Helper()
	connection, host := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done, finished := make(chan error, 1), make(chan struct{})
	output := &bytes.Buffer{}
	if phase == nil {
		phase = func(voicePhase) {}
	}
	go func() {
		defer close(finished)
		done <- bridgeWorker(ctx, connection, cmd, allowClean, phase, log.New(output, "", 0), controls...)
	}()
	t.Cleanup(func() {
		cancel()
		host.Close()
		connection.Close()
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Error("worker/copy cleanup did not finish")
		}
	})
	if err := host.SetDeadline(time.Now().Add(4 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return &bridgeHarness{host, cancel, cmd, done, finished, output}
}

func (harness *bridgeHarness) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-harness.done:
		<-harness.finished
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("worker session failed to stop within its bounded cleanup interval")
		return nil
	}
}

func readWorkerReady(t *testing.T, host net.Conn) {
	t.Helper()
	var ready [6]byte
	if _, err := io.ReadFull(host, ready[:]); err != nil || string(ready[:]) != "READY\n" {
		t.Fatal("fake worker did not become ready:", err)
	}
}

func assertReaped(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("owned process %d is still present (including possible zombie): %v", pid, err)
	}
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); err != syscall.ECHILD {
		t.Fatalf("owned process %d was not already reaped: %v", pid, err)
	}
}

func TestWorkerCommandContractAndIsolatedEnvironment(t *testing.T) {
	t.Setenv("LD_LIBRARY_PATH", "/unrelated/lib")
	t.Setenv("LD_PRELOAD", "/unrelated/preload.so")
	t.Setenv("KWS_UNIT_SOCKET", "/unrelated/audio.sock")
	t.Setenv("KWS_UNIT_FIXTURE_PACE", "99")
	t.Setenv("VOICE_TOKEN", testToken)
	cfg := config{IOTimeoutMS: 30000, Token: testToken}
	for _, fixture := range []bool{false, true} {
		opts := options{Bundle: "/isolated/voice", RuntimeDir: "/runtime", NoPlayback: fixture, Seconds: 0}
		if fixture {
			opts.Fixture = "/fixtures/input.pcm"
			opts.Seconds = 86400
		}
		cmd := workerCommand(cfg, opts, 0xf1234567)
		if !reflect.DeepEqual(cmd.Args, []string{
			"/isolated/voice/lib/ld-linux-armhf.so.3", "--library-path", "/isolated/voice/lib", "/isolated/voice/bin/cortana",
		}) {
			t.Fatal("worker command does not use the isolated explicit loader")
		}
		env := map[string]string{}
		for _, entry := range cmd.Env {
			pair := strings.SplitN(entry, "=", 2)
			if len(pair) != 2 {
				t.Fatal("invalid worker environment")
			}
			env[pair[0]] = pair[1]
			if strings.Contains(entry, testToken) || strings.Contains(entry, "/unrelated/") {
				t.Fatal("worker inherited authentication or donor overrides")
			}
		}
		if env["KWS_MODEL"] != "/isolated/voice/share/handoff-original.table" ||
			env["LD_PRELOAD"] != "/isolated/voice/lib/unit-link.so" ||
			env["KWS_UNIT_NONCE"] != strconv.FormatUint(0xf1234567, 10) ||
			env["KWS_UNIT_POST_MS"] != "" || env["KWS_UNIT_IO_MS"] != "30000" ||
			env["KWS_UNIT_LOCK"] != "/runtime/voice-worker.lock" {
			t.Fatal("worker environment differs from the C worker contract")
		}
		if fixture {
			if env["KWS_UNIT_SOURCE"] != "fixture" || env["KWS_UNIT_PCM"] != opts.Fixture ||
				env["KWS_UNIT_PLAY"] != "0" || env["KWS_UNIT_SECONDS"] != "86400" ||
				env["KWS_UNIT_FIXTURE_PACE"] != "1" {
				t.Fatal("fixture worker environment is incorrect")
			}
		} else {
			if env["KWS_UNIT_SOURCE"] != "socket" || env["KWS_UNIT_PLAY"] != "1" ||
				env["KWS_UNIT_SECONDS"] != "0" {
				t.Fatal("normal worker environment is incorrect")
			}
			if _, exists := env["KWS_UNIT_PCM"]; exists {
				t.Fatal("normal worker received a fixture path")
			}
			if _, exists := env["KWS_UNIT_FIXTURE_PACE"]; exists {
				t.Fatal("normal worker received a fixture pacing setting")
			}
		}
	}
}

func TestWorkerActualPipeForwardingAndCleanFixtureExit(t *testing.T) {
	var phases []voicePhase
	harness := startBridge(t, helperCommand(t, "exchange"), true, func(phase voicePhase) {
		phases = append(phases, phase)
	})
	request := make([]byte, len(fakeRequest()))
	if _, err := io.ReadFull(harness.host, request); err != nil || !bytes.Equal(request, fakeRequest()) {
		t.Fatal("worker stdout was not forwarded byte for byte:", err)
	}
	if err := writeAll(harness.host, fakeReply()); err != nil {
		t.Fatal("host reply did not traverse the worker stdin pipe:", err)
	}
	receipt := make([]byte, len(fakeReceipt()))
	if _, err := io.ReadFull(harness.host, receipt); err != nil || !bytes.Equal(receipt, fakeReceipt()) {
		t.Fatal("final worker receipt was lost or altered:", err)
	}
	if err := harness.wait(t); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(phases, []voicePhase{phaseListening, phaseThinking, phaseSpeaking, phaseIdle}) {
		t.Fatal("stderr phase order was not preserved:", phases)
	}
	assertReaped(t, harness.cmd.Process.Pid)
	var data [1]byte
	if _, err := harness.host.Read(data[:]); err != io.EOF {
		t.Fatal("transport was not closed after fixture completion")
	}
}

func TestHostEOFBeforeWorkerExitCannotBecomeSuccess(t *testing.T) {
	for attempt := 0; attempt < 8; attempt++ {
		harness := startBridge(t, helperCommand(t, "eof-zero"), true, nil)
		readWorkerReady(t, harness.host)
		harness.host.Close()
		err := harness.wait(t)
		if err == nil || !strings.Contains(err.Error(), "host disconnected") {
			t.Fatal("host EOF was disguised by the child's EOF-triggered zero exit:", err)
		}
		assertReaped(t, harness.cmd.Process.Pid)
	}
}

func TestWorkerExitClassification(t *testing.T) {
	for _, test := range []struct {
		mode  string
		clean bool
		error string
	}{
		{"zero", true, ""},
		{"zero", false, "exited unexpectedly"},
		{"fail", true, "voice worker failed"},
		{"close-stdout", true, "did not complete"},
	} {
		t.Run(test.mode+"-"+strconv.FormatBool(test.clean), func(t *testing.T) {
			harness := startBridge(t, helperCommand(t, test.mode), test.clean, nil)
			err := harness.wait(t)
			if test.error == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.error) {
				t.Fatal("unexpected worker exit classification:", err)
			}
			assertReaped(t, harness.cmd.Process.Pid)
		})
	}
}

func TestCancellationGracefulAndSIGKILLFallback(t *testing.T) {
	for _, mode := range []string{"graceful", "ignore-term"} {
		t.Run(mode, func(t *testing.T) {
			harness := startBridge(t, helperCommand(t, mode), true, nil)
			readWorkerReady(t, harness.host)
			harness.cancel()
			if err := harness.wait(t); !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation did not remain an explicit cancellation:", err)
			}
			status, ok := harness.cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok {
				t.Fatal("worker was not waited")
			}
			if mode == "graceful" && (!status.Exited() || status.ExitStatus() != 0) {
				t.Fatal("worker was not allowed to shut down gracefully")
			}
			if mode == "ignore-term" && (!status.Signaled() || status.Signal() != syscall.SIGKILL) {
				t.Fatal("unresponsive worker did not receive bounded SIGKILL fallback")
			}
			assertReaped(t, harness.cmd.Process.Pid)
		})
	}
}

func TestCancellationUnblocksBothCopyDirections(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		ready := make(chan struct{})
		harness := startBridge(t, helperCommand(t, "block-output"), true, func(voicePhase) { close(ready) })
		<-ready
		harness.cancel()
		if err := harness.wait(t); !errors.Is(err, context.Canceled) {
			t.Fatal("blocked worker-to-host copy survived cancellation:", err)
		}
		assertReaped(t, harness.cmd.Process.Pid)
	})
	t.Run("input", func(t *testing.T) {
		harness := startBridge(t, helperCommand(t, "block-input"), true, nil)
		readWorkerReady(t, harness.host)
		writer := make(chan error, 1)
		go func() { writer <- writeAll(harness.host, bytes.Repeat([]byte{0x5a}, 2*1024*1024)) }()
		select {
		case <-writer:
			t.Fatal("non-reading fake worker unexpectedly consumed the input stream")
		case <-time.After(20 * time.Millisecond):
		}
		harness.cancel()
		if err := harness.wait(t); !errors.Is(err, context.Canceled) {
			t.Fatal("blocked host-to-worker copy survived cancellation:", err)
		}
		select {
		case err := <-writer:
			if err == nil {
				t.Fatal("cancelled host writer unexpectedly succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("host writer was left blocked")
		}
		assertReaped(t, harness.cmd.Process.Pid)
	})
}

func TestWorkerFailureKillsAndReapsOrphanDescendant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "child.pid")
	cmd := helperCommand(t, "orphan")
	cmd.Env = append(cmd.Env, "REINVOKE_TEST_CHILD_PID_FILE="+path)
	harness := startBridge(t, cmd, true, nil)
	err := harness.wait(t)
	if err == nil || !strings.Contains(err.Error(), "voice worker failed") {
		t.Fatal("worker failure was not reported:", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("fake orphan was not started:", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 1 {
		t.Fatal("invalid owned descendant PID")
	}
	assertReaped(t, pid)
	assertReaped(t, cmd.Process.Pid)
}

type failingOutputConn struct{ net.Conn }

func (failingOutputConn) Write([]byte) (int, error) {
	return 0, errors.New("test output failure")
}

func TestForwardingFailureTerminatesWorker(t *testing.T) {
	connection, host := net.Pipe()
	defer connection.Close()
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := helperCommand(t, "exchange")
	err := bridgeWorker(ctx, failingOutputConn{connection}, cmd, true, func(voicePhase) {}, log.New(io.Discard, "", 0))
	if err == nil || !strings.Contains(err.Error(), "forward worker output") {
		t.Fatal("forwarding failure was not reported:", err)
	}
	assertReaped(t, cmd.Process.Pid)
}

func TestBoundedStderrDrainAndExactPhases(t *testing.T) {
	var output bytes.Buffer
	var phases []voicePhase
	input := "UNIT_LINK PHASE idle\n" +
		strings.Repeat("x", maxWorkerLogLine*3) + "\n" +
		"UNIT_LINK PHASE listening extra\nprefix UNIT_LINK PHASE speaking\n" +
		"UNIT_LINK PHASE thinking\r\nUNIT_LINK PHASE listening\n" +
		"UNIT_LINK PHASE thinking\nUNIT_LINK PHASE speaking\nUNIT_LINK PHASE idle\n"
	err := drainWorkerLogs(strings.NewReader(input), func(phase voicePhase) {
		phases = append(phases, phase)
	}, log.New(&output, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(phases, []voicePhase{phaseIdle, phaseListening, phaseThinking, phaseSpeaking, phaseIdle}) {
		t.Fatal("stderr parser accepted an inexact phase or lost later lines:", phases)
	}
	if len(output.String()) > 1024 || !strings.Contains(output.String(), "discarding line") {
		t.Fatal("oversized stderr line was not bounded and reported")
	}
}

func TestOnceAndCancelledReconnect(t *testing.T) {
	sentinel := errors.New("test session failure")
	for _, result := range []error{nil, sentinel} {
		calls := 0
		err := supervise(context.Background(), true, log.New(io.Discard, "", 0), func(context.Context) error {
			calls++
			return result
		})
		if !errors.Is(err, result) || calls != 1 {
			t.Fatal("--once retried or hid the session result")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- supervise(ctx, false, log.New(io.Discard, "", 0), func(context.Context) error {
			started <- struct{}{}
			return sentinel
		})
	}()
	<-started
	select {
	case <-started:
		t.Fatal("reconnect did not back off")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("reconnect backoff hid cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("reconnect backoff ignored cancellation")
	}
}
