// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
)

const maximumSourceLogBytes = 4096

type boundedBuffer struct {
	mu      sync.Mutex
	content []byte
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	remaining := maximumSourceLogBytes - len(buffer.content)
	if remaining > 0 {
		if remaining > len(content) {
			remaining = len(content)
		}
		buffer.content = append(buffer.content, content[:remaining]...)
	}
	return len(content), nil
}

func (buffer *boundedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(bytes.TrimSpace(buffer.content))
}

type sourceConfig struct {
	loader      string
	libraryPath string
	arecord     string
	device      string
}

type captureSource struct {
	cancel   context.CancelFunc
	pid      int
	periods  <-chan []byte
	done     <-chan error
	stopOnce sync.Once
}

func startCaptureSource(
	parent context.Context,
	config sourceConfig,
) (*captureSource, error) {
	ctx, cancel := context.WithCancel(parent)
	command := exec.CommandContext(
		ctx,
		config.loader,
		"--library-path",
		config.libraryPath,
		config.arecord,
		"-D",
		config.device,
		"-t",
		"raw",
		"-f",
		"S32_LE",
		"-r",
		"48000",
		"-c",
		"2",
		"--period-size=256",
		"--buffer-size=4096",
		"-",
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
		Setpgid:   true,
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open capture stdout: %w", err)
	}
	stderr := &boundedBuffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start capture helper: %w", err)
	}

	periods := make(chan []byte, 8)
	done := make(chan error, 1)
	go func() {
		defer close(periods)
		var readErr error
		for {
			period := make([]byte, nativePeriodBytes)
			if _, err := io.ReadFull(stdout, period); err != nil {
				if errors.Is(err, io.EOF) ||
					errors.Is(err, io.ErrUnexpectedEOF) {
					readErr = err
				} else {
					readErr = fmt.Errorf("read capture period: %w", err)
				}
				break
			}
			select {
			case periods <- period:
			case <-ctx.Done():
				readErr = ctx.Err()
				break
			}
			if readErr != nil {
				break
			}
		}
		waitErr := command.Wait()
		if ctx.Err() != nil {
			done <- ctx.Err()
			close(done)
			return
		}
		if readErr != nil {
			done <- fmt.Errorf(
				"capture helper stopped: %v; stderr=%q",
				readErr,
				stderr.String(),
			)
			close(done)
			return
		}
		if waitErr != nil {
			done <- fmt.Errorf(
				"capture helper exited: %v; stderr=%q",
				waitErr,
				stderr.String(),
			)
			close(done)
			return
		}
		done <- errors.New("capture helper exited")
		close(done)
	}()
	return &captureSource{
		cancel:  cancel,
		pid:     command.Process.Pid,
		periods: periods,
		done:    done,
	}, nil
}

func (source *captureSource) stop() {
	source.stopOnce.Do(func() {
		source.cancel()
		// The helper runs in its own process group. Kill the whole group so a
		// loader or wrapper cannot leave a descendant holding stdout open.
		_ = syscall.Kill(-source.pid, syscall.SIGKILL)
	})
	<-source.done
}
