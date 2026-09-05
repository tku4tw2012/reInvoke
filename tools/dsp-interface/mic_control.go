// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const micControlTimeout = 5 * time.Second

func runMicControlServer(
	ctx context.Context,
	path string,
	service *wampService,
	ready chan<- struct{},
) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("DSP microphone control path is not a socket: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale DSP microphone socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect DSP microphone socket: %w", err)
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listen on DSP microphone socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(path)
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect DSP microphone socket: %w", err)
	}
	close(ready)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept DSP microphone control: %w", err)
		}
		err = handleMicControlConnection(ctx, connection, service)
		_ = connection.Close()
		if err != nil {
			service.log("DSP microphone control: %v", err)
		}
	}
}

func handleMicControlConnection(
	ctx context.Context,
	connection net.Conn,
	service *wampService,
) error {
	if err := connection.SetDeadline(time.Now().Add(micControlTimeout)); err != nil {
		return err
	}
	request := make([]byte, 2)
	if _, err := io.ReadFull(connection, request); err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	var muted uint64
	switch string(request) {
	case "0\n":
		muted = 0
	case "1\n":
		muted = 1
	default:
		return errors.New("invalid request")
	}
	service.micControlMu.Lock()
	defer service.micControlMu.Unlock()
	if err := service.dispatch(
		ctx,
		micMuteSpec,
		[]interface{}{muted},
	); err != nil {
		_, _ = connection.Write([]byte("ERR\n"))
		return err
	}
	if _, err := connection.Write([]byte("OK\n")); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}
