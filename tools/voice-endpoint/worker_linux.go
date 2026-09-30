// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxWorkerLogLine = 4096
	workerStopGrace  = time.Second
)

var (
	subreaperOnce sync.Once
	subreaperErr  error
)

func enableSubreaper() error {
	subreaperOnce.Do(func() {
		// PR_SET_CHILD_SUBREAPER lets us reap aplay even if the donor exits
		// before it can wait for its child. Wait4 below is restricted to our
		// worker's process group, never other services or session workers.
		const prSetChildSubreaper = 36
		_, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0, 0, 0, 0)
		if errno != 0 {
			subreaperErr = fmt.Errorf("enable worker descendant reaping: %w", errno)
		}
	})
	return subreaperErr
}

func workerCommand(cfg config, opts options, nonce uint32) *exec.Cmd {
	lib := filepath.Join(opts.Bundle, "lib")
	cmd := exec.Command(
		filepath.Join(lib, "ld-linux-armhf.so.3"),
		"--library-path", lib, filepath.Join(opts.Bundle, "bin/cortana"),
	)
	source, play := "socket", "1"
	if opts.Fixture != "" {
		source = "fixture"
	}
	if opts.NoPlayback {
		play = "0"
	}
	// Use a deliberately small environment: no inherited preload, alternate
	// capture source, or host authentication material reaches the donor.
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"LANG=C",
		"LD_PRELOAD=" + filepath.Join(lib, "unit-link.so"),
		"KWS_MODEL=" + filepath.Join(opts.Bundle, "share/handoff-original.table"),
		"KWS_UNIT_SOURCE=" + source,
		"KWS_UNIT_NONCE=" + strconv.FormatUint(uint64(nonce), 10),
		"KWS_UNIT_IO_MS=" + strconv.Itoa(cfg.IOTimeoutMS),
		"KWS_UNIT_SECONDS=" + strconv.Itoa(opts.Seconds),
		"KWS_UNIT_PLAY=" + play,
		"KWS_UNIT_LOCK=" + filepath.Join(opts.RuntimeDir, "voice-worker.lock"),
	}
	if opts.Fixture != "" {
		cmd.Env = append(cmd.Env, "KWS_UNIT_PCM="+opts.Fixture, "KWS_UNIT_FIXTURE_PACE=1")
	}
	return cmd
}

func runSession(
	ctx context.Context, cfg config, opts options, logger *log.Logger,
	command func(uint32) *exec.Cmd,
) error {
	connection, nonce, err := connectHost(ctx, cfg, opts)
	if err != nil {
		return err
	}
	defer connection.Close()
	controls, err := connectWorkerControls(ctx, opts.Router, opts.Realm)
	if err != nil {
		return err
	}
	defer controls.Close()
	feedback := newFeedback(ctx, microphoneStatePath, logger,
		func(ctx context.Context, procedure string, args []interface{}, kwargs map[string]interface{}) error {
			return callWAMP(ctx, opts.Router, opts.Realm, procedure, args, kwargs)
		})
	defer feedback.close()
	logger.Print("host authenticated; starting isolated voice worker")
	cmd := command(nonce)
	if cmd == nil {
		return errors.New("voice worker command is unavailable")
	}
	allowCleanExit := opts.Fixture != "" || opts.Seconds != 0
	return bridgeWorker(ctx, connection, cmd, allowCleanExit, feedback.publish, logger, controls)
}

type copyResult struct {
	err         error
	afterStop   bool
	afterWorker bool
}

func channelClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

func bridgeWorker(
	ctx context.Context, connection net.Conn, cmd *exec.Cmd, allowCleanExit bool,
	phase func(voicePhase), logger *log.Logger,
	control ...*workerControls,
) error {
	defer connection.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := enableSubreaper(); err != nil {
		return err
	}
	// Own the pipes rather than using Cmd.StdoutPipe: Cmd.Wait must not close
	// stdout before the last receipt has actually been forwarded to TLS.
	var pipes []*os.File
	defer func() {
		for _, pipe := range pipes {
			pipe.Close()
		}
	}()
	for pair := 0; pair < 3; pair++ {
		read, write, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("create worker pipes: %w", err)
		}
		pipes = append(pipes, read, write)
	}
	childInput, input := pipes[0], pipes[1]
	output, childOutput := pipes[2], pipes[3]
	stderr, childStderr := pipes[4], pipes[5]
	cmd.Stdin, cmd.Stdout, cmd.Stderr = childInput, childOutput, childStderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start isolated voice worker: %w", err)
	}
	childInput.Close()
	childOutput.Close()
	childStderr.Close()

	waitDone := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(waitDone)
	}()
	var actions <-chan syscall.Signal
	var controlErrors <-chan error
	if len(control) != 0 && control[0] != nil {
		actions, controlErrors = control[0].actions, control[0].errors
	}
	target := ownedWorker{process: cmd.Process}
	stopping := make(chan struct{})
	incoming := make(chan copyResult, 1)
	outgoing := make(chan copyResult, 1)
	logs := make(chan error, 1)
	go func() {
		_, err := io.Copy(input, connection)
		result := copyResult{err, channelClosed(stopping), channelClosed(waitDone)}
		// Record host EOF BEFORE closing stdin; an EOF-responsive child can
		// otherwise exit zero and disguise the disconnect as fixture success.
		input.Close()
		incoming <- result
	}()
	go func() {
		_, err := io.Copy(connection, output)
		outgoing <- copyResult{err, channelClosed(stopping), channelClosed(waitDone)}
	}()
	go func() { logs <- drainWorkerLogs(stderr, phase, logger) }()

	var sessionErr error
	var inResult, outResult copyResult
	var logErr error
	inDone, outDone, logDone, waited := false, false, false, false
	waitSignal := waitDone
	inSignal, outSignal, logSignal := incoming, outgoing, logs
	var exitTimer *time.Timer
	var exitDeadline <-chan time.Time
	startExitTimer := func() {
		if exitTimer == nil {
			exitTimer = time.NewTimer(workerStopGrace)
			exitDeadline = exitTimer.C
		}
	}
	for sessionErr == nil && !(waited && outDone) {
		select {
		case <-ctx.Done():
			sessionErr = ctx.Err()
		case action := <-actions:
			if err := target.signal(ctx, waitDone, action); err != nil &&
				!errors.Is(err, os.ErrProcessDone) {
				logger.Printf("voice button signal failed: %v", err)
			}
		case err := <-controlErrors:
			sessionErr = err
			if sessionErr == nil {
				sessionErr = errors.New("voice control session ended")
			}
		case <-waitSignal:
			waited, waitSignal = true, nil
			if waitErr != nil {
				sessionErr = fmt.Errorf("voice worker failed: %w", waitErr)
			} else if !allowCleanExit {
				sessionErr = errors.New("voice worker exited unexpectedly during an indefinite session")
			}
			startExitTimer()
		case inResult = <-inSignal:
			inDone, inSignal = true, nil
			if !inResult.afterWorker {
				sessionErr = hostForwardError(inResult.err)
			}
		case outResult = <-outSignal:
			outDone, outSignal = true, nil
			if outResult.err != nil {
				sessionErr = fmt.Errorf("forward worker output: %w", outResult.err)
			}
			startExitTimer()
		case logErr = <-logSignal:
			logDone, logSignal = true, nil
			if logErr != nil {
				sessionErr = fmt.Errorf("read worker stderr: %w", logErr)
			}
		case <-exitDeadline:
			sessionErr = errors.New("voice worker exit or final pipe forwarding did not complete")
		}
	}
	if exitTimer != nil {
		exitTimer.Stop()
	}
	close(stopping)
	connection.Close()
	input.Close()
	output.Close()
	if err := stopWorkerGroup(cmd.Process.Pid, waitDone); err != nil {
		if sessionErr == nil {
			sessionErr = err
		} else {
			logger.Printf("worker cleanup failed: %v", err)
		}
	}
	// All writers in the owned group are gone. Join every goroutine before a
	// reconnect, including stderr and the cancellation-interrupted pipe copies.
	if !inDone {
		inResult = <-incoming
	}
	if !outDone {
		outResult = <-outgoing
	}
	if !logDone {
		logErr = <-logs
	}
	if sessionErr == nil && !inResult.afterStop && !inResult.afterWorker {
		sessionErr = hostForwardError(inResult.err)
	}
	if sessionErr == nil && outResult.err != nil && !outResult.afterStop {
		sessionErr = fmt.Errorf("forward worker output: %w", outResult.err)
	}
	if sessionErr == nil && logErr != nil {
		sessionErr = fmt.Errorf("read worker stderr: %w", logErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return sessionErr
}

type ownedWorker struct {
	process *os.Process
}

// Signals are sent through the managed os.Process, never Kill(-pid, SIGUSR*):
// aplay descendants must not receive the donor worker's private control input.
// os.Process also fences signals against Wait/reaping and subsequent PID reuse.
func (worker ownedWorker) signal(ctx context.Context, done <-chan struct{}, signal syscall.Signal) error {
	if signal != syscall.SIGUSR1 && signal != syscall.SIGUSR2 {
		return errors.New("unsupported worker control signal")
	}
	if worker.process == nil || worker.process.Pid <= 1 {
		return errors.New("voice worker is not a current child")
	}
	deadline := time.NewTimer(controlTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if channelClosed(done) {
			return os.ErrProcessDone
		}
		ready, err := worker.controlsReady()
		if err != nil {
			return err
		}
		if ready {
			return worker.process.Signal(signal)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return os.ErrProcessDone
		case <-deadline.C:
			return errors.New("voice worker did not install its control signal handlers")
		case <-ticker.C:
		}
	}
}

func (worker ownedWorker) controlsReady() (bool, error) {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", worker.process.Pid))
	if err != nil {
		return false, fmt.Errorf("verify voice worker: %w", err)
	}
	parent, caught := 0, uint64(0)
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "PPid:":
			parent, _ = strconv.Atoi(fields[1])
		case "SigCgt:":
			caught, _ = strconv.ParseUint(fields[1], 16, 64)
		}
	}
	if parent != os.Getpid() {
		return false, errors.New("refusing to signal a voice worker outside the current child")
	}
	mask := uint64(1)<<(uint(syscall.SIGUSR1)-1) | uint64(1)<<(uint(syscall.SIGUSR2)-1)
	return caught&mask == mask, nil
}

func hostForwardError(err error) error {
	if err == nil {
		return errors.New("voice host disconnected before worker completion")
	}
	return fmt.Errorf("forward host response before worker completion: %w", err)
}

func drainWorkerLogs(reader io.Reader, phase func(voicePhase), logger *log.Logger) error {
	buffer := bufio.NewReaderSize(reader, maxWorkerLogLine)
	discarding := false
	for {
		line, err := buffer.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			if !discarding {
				logger.Printf("worker stderr line exceeds %d bytes; discarding line", maxWorkerLogLine)
			}
			discarding = true
			continue
		}
		if !discarding && len(line) != 0 {
			text := strings.TrimSuffix(string(line), "\n")
			logger.Printf("worker: %s", text)
			if state, ok := loggedPhase(text); ok {
				phase(state)
			}
		}
		discarding = false
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func stopWorkerGroup(pid int, waitDone <-chan struct{}) error {
	var firstErr error
	signal := func(number syscall.Signal) {
		if err := syscall.Kill(-pid, number); err != nil && err != syscall.ESRCH && firstErr == nil {
			firstErr = fmt.Errorf("signal owned worker group: %w", err)
		}
	}
	gone := func() bool {
		if !channelClosed(waitDone) {
			return false
		}
		if err := reapWorkerChildren(pid); err != nil && firstErr == nil {
			firstErr = err
		}
		err := syscall.Kill(-pid, 0)
		if err != nil && err != syscall.ESRCH && firstErr == nil {
			firstErr = fmt.Errorf("check owned worker group: %w", err)
		}
		return err == syscall.ESRCH
	}
	signal(syscall.SIGTERM)
	for deadline := time.Now().Add(workerStopGrace); time.Now().Before(deadline); {
		if gone() {
			return firstErr
		}
		time.Sleep(10 * time.Millisecond)
	}
	signal(syscall.SIGKILL)
	<-waitDone
	for deadline := time.Now().Add(workerStopGrace); time.Now().Before(deadline); {
		if gone() {
			return firstErr
		}
		time.Sleep(10 * time.Millisecond)
	}
	if firstErr != nil {
		return firstErr
	}
	return errors.New("owned worker group still exists after SIGKILL")
}

func reapWorkerChildren(group int) error {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-group, &status, syscall.WNOHANG, nil)
		if err == syscall.EINTR {
			continue
		}
		if err == syscall.ECHILD {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reap worker descendant: %w", err)
		}
		if pid == 0 {
			return nil
		}
	}
}
