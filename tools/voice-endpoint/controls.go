// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

const voiceActionTopic = "com.harman.vui.action"

type workerControls struct {
	connection net.Conn
	actions    chan syscall.Signal
	errors     chan error
	done       chan struct{}
}

func connectWorkerControls(ctx context.Context, address, realm string) (*workerControls, error) {
	setup, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	connection, limit, err := openControlWAMP(setup, address, realm, "subscriber")
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			connection.Close()
		}
	}()
	stop := closeOnCancel(setup, connection)
	defer stop()
	if err := writeWAMP(connection, limit, []interface{}{
		32, uint64(1), map[string]interface{}{}, voiceActionTopic,
	}); err != nil {
		return nil, fmt.Errorf("subscribe voice actions: %w", err)
	}
	var subscription uint64
	for skipped := 0; skipped < maxControlSkip; skipped++ {
		message, err := readWAMP(connection, limit)
		if err != nil {
			return nil, fmt.Errorf("read voice subscription: %w", err)
		}
		if controlNumber(message[0]) == 33 {
			if len(message) != 3 || controlNumber(message[1]) != 1 ||
				controlNumber(message[2]) == 0 {
				return nil, errors.New("invalid voice SUBSCRIBED response")
			}
			subscription = controlNumber(message[2])
			break
		}
		if controlNumber(message[0]) == 6 || controlNumber(message[0]) == 8 {
			return nil, errors.New("WAMP router rejected voice subscription")
		}
	}
	if subscription == 0 {
		return nil, errors.New("too many unrelated voice subscription responses")
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear voice subscription deadline: %w", err)
	}
	controls := &workerControls{
		connection: connection,
		actions:    make(chan syscall.Signal, 8),
		errors:     make(chan error, 1),
		done:       make(chan struct{}),
	}
	ready = true
	go func() {
		defer close(controls.done)
		defer connection.Close()
		stop := closeOnCancel(ctx, connection)
		defer stop()
		controls.errors <- controls.read(ctx, limit, subscription)
	}()
	return controls, nil
}

func voiceAction(message []interface{}, subscription uint64) syscall.Signal {
	if len(message) < 5 || len(message) > 6 || controlNumber(message[0]) != 36 ||
		controlNumber(message[1]) != subscription || controlNumber(message[2]) == 0 {
		return 0
	}
	if _, ok := message[3].(map[string]interface{}); !ok {
		return 0
	}
	args, ok := message[4].([]interface{})
	if !ok || len(args) != 2 {
		return 0
	}
	if len(message) == 6 {
		if _, ok := message[5].(map[string]interface{}); !ok {
			return 0
		}
	}
	switch {
	case args[0] == "voice-trigger" && args[1] == "action-long":
		return syscall.SIGUSR1
	case args[0] == "voice-cancel" && args[1] == "action":
		return syscall.SIGUSR2
	case args[0] == "micmute" && args[1] == "micmute":
		return syscall.SIGUSR2
	default:
		return 0
	}
}

func (controls *workerControls) read(ctx context.Context, limit int, subscription uint64) error {
	for {
		message, err := readWAMP(controls.connection, limit)
		if err != nil {
			return fmt.Errorf("voice action subscription lost: %w", err)
		}
		if controlNumber(message[0]) == 6 {
			return errors.New("WAMP router closed the voice action session")
		}
		if signal := voiceAction(message, subscription); signal != 0 {
			select {
			case controls.actions <- signal:
			case <-ctx.Done():
				return ctx.Err()
			default:
				return errors.New("voice action queue is full")
			}
		}
	}
}

func (controls *workerControls) Close() {
	if controls != nil {
		controls.connection.Close()
		<-controls.done
	}
}
