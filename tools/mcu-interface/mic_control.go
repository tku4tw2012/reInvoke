// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const dspMicControlTimeout = 5 * time.Second

func setDSPMicrophone(
	ctx context.Context,
	path string,
	muted bool,
) error {
	if path == "" {
		return errors.New("DSP microphone control socket is unavailable")
	}
	dialer := net.Dialer{Timeout: dspMicControlTimeout}
	connection, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return fmt.Errorf("connect DSP microphone control: %w", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(
		time.Now().Add(dspMicControlTimeout),
	); err != nil {
		return fmt.Errorf("set DSP microphone deadline: %w", err)
	}
	request := []byte("0\n")
	if muted {
		request = []byte("1\n")
	}
	if _, err := connection.Write(request); err != nil {
		return fmt.Errorf("write DSP microphone control: %w", err)
	}
	response := make([]byte, 3)
	if _, err := io.ReadFull(connection, response); err != nil {
		return fmt.Errorf("read DSP microphone control: %w", err)
	}
	if string(response) != "OK\n" {
		return errors.New("DSP microphone control was rejected")
	}
	return nil
}
