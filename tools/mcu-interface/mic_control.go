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

// setDSPVolume sets the amplifier level over the same control socket the
// microphone mute uses. Volume used to go through bluealsa-cli, which this
// runtime does not ship.
func setDSPVolume(ctx context.Context, path string, percent int) error {
	return sendDSPControl(ctx, path, fmt.Sprintf("v%d\n", percent))
}

func setDSPMicrophone(
	ctx context.Context,
	path string,
	muted bool,
) error {
	request := "0\n"
	if muted {
		request = "1\n"
	}
	return sendDSPControl(ctx, path, request)
}

// sendDSPControl performs one request/response exchange on the DSP control
// socket. Both the microphone mute and the volume use it.
func sendDSPControl(ctx context.Context, path, request string) error {
	if path == "" {
		return errors.New("DSP control socket is unavailable")
	}
	dialer := net.Dialer{Timeout: dspMicControlTimeout}
	connection, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return fmt.Errorf("connect DSP control: %w", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(
		time.Now().Add(dspMicControlTimeout),
	); err != nil {
		return fmt.Errorf("set DSP control deadline: %w", err)
	}
	if _, err := connection.Write([]byte(request)); err != nil {
		return fmt.Errorf("write DSP control: %w", err)
	}
	response := make([]byte, 3)
	if _, err := io.ReadFull(connection, response); err != nil {
		return fmt.Errorf("read DSP control: %w", err)
	}
	if string(response) != "OK\n" {
		return errors.New("DSP control request was rejected")
	}
	return nil
}
