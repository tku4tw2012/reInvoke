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

const provisioningControlTimeout = 3 * time.Second

type provisioningController struct {
	socketPath string
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
		Accepted bool `json:"accepted"`
	}
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		return fmt.Errorf("read provisioning response: %w", err)
	}
	if !response.Accepted {
		return errors.New("provisioning window request was rejected")
	}
	return nil
}
