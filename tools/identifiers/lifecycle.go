// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"time"
)

// The donor published com.harman.ready.NAME once a service could answer and
// com.harman.heartbeat.NAME while it still could, and podium.conf gated each
// dependant on wait_ready and wait_heartbeats. This runtime supervised by pid
// file instead, which cannot tell "forked" from "usable": servicemanager
// crash-looped 1380 times on 05.8.7 while every check reported it up.
const lifecycleHeartbeatInterval = 5 * time.Second

type lifecyclePublisher interface {
	publish(topic string, args []interface{}) error
}

// announceReady says the service can answer now.
func announceReady(client lifecyclePublisher, name string) error {
	return client.publish("com.harman.ready."+name, nil)
}

// runHeartbeat repeats the liveness topic until the context ends. A stopped
// heartbeat is the signal a watchdog acts on, so this returns the publish
// error rather than swallowing it.
func runHeartbeat(
	ctx context.Context,
	client lifecyclePublisher,
	name string,
	interval time.Duration,
) error {
	if interval <= 0 {
		interval = lifecycleHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := client.publish("com.harman.heartbeat."+name, nil); err != nil {
				return err
			}
		}
	}
}
