// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"time"
)

const mcuHeartbeatInterval = 5 * time.Second

type heartbeatWriter interface {
	WriteMCUCommand([6]byte) error
}

func runMCUHeartbeat(
	ctx context.Context,
	writer heartbeatWriter,
	interval time.Duration,
) error {
	send := func() error {
		if err := writer.WriteMCUCommand([6]byte{0x24}); err != nil {
			return fmt.Errorf("send MCU heartbeat: %w", err)
		}
		return nil
	}
	if err := send(); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := send(); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
		}
	}
}
