// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	provisioningControlTimeout = 3 * time.Second
	provisioningOutcomeTimeout = 6 * time.Minute // window max + buffer
)

type provisioningController struct {
	socketPath string
	lights     *ledPlayer
}

func (controller provisioningController) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	if event.Name != "micmute-long" {
		return nil
	}
	dialer := net.Dialer{Timeout: provisioningControlTimeout}
	connection, err := dialer.DialContext(
		ctx,
		"unix",
		controller.socketPath,
	)
	if err != nil {
		return fmt.Errorf("connect provisioning window: %w", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(
		time.Now().Add(provisioningControlTimeout),
	); err != nil {
		return fmt.Errorf("set provisioning deadline: %w", err)
	}
	if err := json.NewEncoder(connection).Encode(map[string]interface{}{
		"operation": "OPEN",
	}); err != nil {
		return fmt.Errorf("request provisioning window: %w", err)
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return errors.New("provisioning connection is not a Unix socket")
	}
	if err := unixConnection.CloseWrite(); err != nil {
		return fmt.Errorf("finish provisioning request: %w", err)
	}
	var response struct {
		Accepted        bool  `json:"accepted"`
		DurationSeconds int64 `json:"duration_seconds,omitempty"`
	}
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		return fmt.Errorf("read provisioning response: %w", err)
	}
	if !response.Accepted {
		return errors.New("provisioning window request was rejected")
	}
	if controller.lights != nil {
		_ = controller.lights.Start(ctx, "L_302_d_wifisetup", false)
	}
	// Wait for the outcome message. The deadline covers the window lifetime.
	waitSeconds := response.DurationSeconds
	if waitSeconds <= 0 {
		waitSeconds = int64(provisioningOutcomeTimeout / time.Second)
	}
	deadline := time.Now().Add(
		time.Duration(waitSeconds)*time.Second + 30*time.Second,
	)
	if err := connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set provisioning outcome deadline: %w", err)
	}
	var outcome struct {
		Outcome string `json:"outcome"`
	}
	if err := json.NewDecoder(connection).Decode(&outcome); err != nil {
		// Connection closed without an outcome (e.g. daemon shutdown).
		return nil
	}
	if controller.lights == nil {
		return nil
	}
	switch outcome.Outcome {
	case "applied":
		_ = controller.lights.Start(ctx, "L_303_d_wificonnected", false)
		_ = controller.lights.Start(ctx, "L_404_o_oobesuccess", false)
	case "failed":
		_ = controller.lights.Start(ctx, "L_108_c_error", false)
	}
	return nil
}
