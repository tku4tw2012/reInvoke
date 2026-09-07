// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

const (
	defaultClientQueuePeriods = 4
	clientWriteTimeout        = 250 * time.Millisecond
)

type captureClient struct {
	connection net.Conn
	queue      chan []byte
	done       chan struct{}
	stopOnce   sync.Once
}

func (client *captureClient) stop() {
	client.stopOnce.Do(func() {
		_ = client.connection.Close()
		close(client.queue)
	})
}

func (client *captureClient) run(
	header []byte,
	onExit func(*captureClient),
) {
	defer close(client.done)
	defer onExit(client)
	if err := writeAll(client.connection, header, clientWriteTimeout); err != nil {
		return
	}
	for record := range client.queue {
		if err := writeAll(
			client.connection,
			record,
			clientWriteTimeout,
		); err != nil {
			return
		}
	}
}

func writeAll(connection net.Conn, content []byte, timeout time.Duration) error {
	if err := connection.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	for len(content) > 0 {
		written, err := connection.Write(content)
		if err != nil {
			return err
		}
		if written == 0 {
			return errors.New("zero-length socket write")
		}
		content = content[written:]
	}
	return nil
}

type clientHub struct {
	mu         sync.Mutex
	enabled    bool
	generation uint64
	queueSize  int
	clients    map[*captureClient]struct{}
}

func newClientHub(queueSize int) *clientHub {
	if queueSize <= 0 {
		queueSize = defaultClientQueuePeriods
	}
	return &clientHub{
		queueSize: queueSize,
		clients:   make(map[*captureClient]struct{}),
	}
}

func (hub *clientHub) add(connection net.Conn) bool {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if !hub.enabled {
		_ = connection.Close()
		return false
	}
	client := &captureClient{
		connection: connection,
		queue:      make(chan []byte, hub.queueSize),
		done:       make(chan struct{}),
	}
	hub.clients[client] = struct{}{}
	header := encodeStreamHeader(hub.generation)
	go client.run(header, hub.remove)
	return true
}

func (hub *clientHub) remove(client *captureClient) {
	hub.mu.Lock()
	delete(hub.clients, client)
	hub.mu.Unlock()
	client.stop()
}

func (hub *clientHub) enable(generation uint64) {
	hub.mu.Lock()
	hub.generation = generation
	hub.enabled = true
	hub.mu.Unlock()
}

func (hub *clientHub) block(ctx context.Context) error {
	hub.mu.Lock()
	hub.enabled = false
	clients := make([]*captureClient, 0, len(hub.clients))
	for client := range hub.clients {
		clients = append(clients, client)
		delete(hub.clients, client)
		client.stop()
	}
	hub.mu.Unlock()

	for _, client := range clients {
		select {
		case <-client.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (hub *clientHub) broadcast(record []byte) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if !hub.enabled {
		return
	}
	for client := range hub.clients {
		select {
		case client.queue <- record:
		default:
			delete(hub.clients, client)
			client.stop()
		}
	}
}

func (hub *clientHub) state() (bool, uint64, int) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return hub.enabled, hub.generation, len(hub.clients)
}
