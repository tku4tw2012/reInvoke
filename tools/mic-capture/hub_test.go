// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestBlockedHubRejectsClient(t *testing.T) {
	hub := newClientHub(1)
	server, client := net.Pipe()
	defer client.Close()
	if hub.add(server) {
		t.Fatal("blocked hub accepted a client")
	}
	if _, err := client.Write([]byte{1}); err == nil {
		t.Fatal("rejected client connection remained open")
	}
}

func TestEnabledHubDeliversHeaderAndRecord(t *testing.T) {
	hub := newClientHub(1)
	hub.enable(42)
	server, client := net.Pipe()
	defer client.Close()
	if !hub.add(server) {
		t.Fatal("enabled hub rejected a client")
	}
	record := make([]byte, recordSize)
	hub.broadcast(record)

	content := make([]byte, streamHeaderSize+recordSize)
	if _, err := io.ReadFull(client, content); err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if string(content[:8]) != streamMagic {
		t.Fatalf("stream magic = %q", content[:8])
	}
}

func TestBlockClosesBackloggedClientBeforeReturning(t *testing.T) {
	hub := newClientHub(8)
	hub.enable(42)
	server, client := net.Pipe()
	defer client.Close()
	if !hub.add(server) {
		t.Fatal("enabled hub rejected a client")
	}

	// Do not read. The writer blocks on its header while records accumulate.
	for index := 0; index < 4; index++ {
		hub.broadcast(make([]byte, recordSize))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := hub.block(ctx); err != nil {
		t.Fatalf("block: %v", err)
	}
	enabled, _, clients := hub.state()
	if enabled || clients != 0 {
		t.Fatalf("state enabled=%v clients=%d", enabled, clients)
	}
	if _, err := client.Write([]byte{1}); err == nil {
		t.Fatal("client remained writable after block")
	}
}

func TestSlowClientDoesNotDisableOtherClients(t *testing.T) {
	hub := newClientHub(1)
	hub.enable(7)
	slowServer, slowClient := net.Pipe()
	defer slowClient.Close()
	fastServer, fastClient := net.Pipe()
	defer fastClient.Close()
	hub.add(slowServer)
	hub.add(fastServer)

	headerRead := make(chan error, 1)
	firstRecordRead := make(chan error, 1)
	fastContent := make(chan error, 1)
	go func() {
		header := make([]byte, streamHeaderSize)
		_, err := io.ReadFull(fastClient, header)
		headerRead <- err
		if err != nil {
			return
		}

		first := make([]byte, recordSize)
		_, err = io.ReadFull(fastClient, first)
		firstRecordRead <- err
		if err != nil {
			return
		}
		second := make([]byte, recordSize)
		_, err = io.ReadFull(fastClient, second)
		fastContent <- err
	}()

	select {
	case err := <-headerRead:
		if err != nil {
			t.Fatalf("fast client header: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fast client did not receive its header")
	}
	hub.broadcast(make([]byte, recordSize))
	select {
	case err := <-firstRecordRead:
		if err != nil {
			t.Fatalf("fast client first record: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fast client did not receive its first record")
	}
	hub.broadcast(make([]byte, recordSize))
	select {
	case err := <-fastContent:
		if err != nil {
			t.Fatalf("fast client read: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow client blocked the fast client")
	}
}

func TestFullQueueDisconnectsSlowClient(t *testing.T) {
	hub := newClientHub(1)
	hub.enable(42)
	server, client := net.Pipe()
	defer client.Close()
	if !hub.add(server) {
		t.Fatal("enabled hub rejected a client")
	}

	// Nothing reads, so the writer stays blocked on its header and never
	// drains the queue. One record fills it; the next must evict the client.
	hub.broadcast(make([]byte, recordSize))
	// Eviction by full queue is synchronous inside broadcast, while the write
	// deadline fires asynchronously later. Asserting the client survives the
	// first record keeps a slow machine from passing this test via the
	// deadline path instead of the queue path under test.
	if _, _, clients := hub.state(); clients != 1 {
		t.Fatalf("client left before the queue could fill: clients=%d", clients)
	}
	hub.broadcast(make([]byte, recordSize))
	if _, _, clients := hub.state(); clients != 0 {
		t.Fatalf("full queue did not disconnect the slow client: clients=%d", clients)
	}

	// A closed net.Pipe rejects deadline calls as well, so the deadline is a
	// best-effort guard against hanging; the read below is the real assertion.
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("evicted slow client connection remained open")
	}
}

func TestWriteDeadlineDisconnectsStalledClient(t *testing.T) {
	hub := newClientHub(4)
	hub.enable(42)
	server, client := net.Pipe()
	defer client.Close()
	if !hub.add(server) {
		t.Fatal("enabled hub rejected a client")
	}

	// No broadcast, so the queue can never fill: the outstanding header write
	// makes the write deadline the only path that can evict this client.
	if _, _, clients := hub.state(); clients != 1 {
		t.Fatalf("client left before any write could stall: clients=%d", clients)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, clients := hub.state(); clients == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("write deadline never disconnected the stalled client")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReplaceGenerationDisconnectsExistingClients(t *testing.T) {
	hub := newClientHub(1)
	hub.enable(7)
	server, client := net.Pipe()
	defer client.Close()
	if !hub.add(server) {
		t.Fatal("enabled hub rejected a client")
	}
	header := make([]byte, streamHeaderSize)
	if _, err := io.ReadFull(client, header); err != nil {
		t.Fatalf("read first header: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := hub.replaceGeneration(ctx, 8); err != nil {
		t.Fatalf("replace generation: %v", err)
	}
	if _, err := client.Write([]byte{1}); err == nil {
		t.Fatal("old-generation client remained connected")
	}
	enabled, generation, clients := hub.state()
	if !enabled || generation != 8 || clients != 0 {
		t.Fatalf(
			"enabled=%v generation=%d clients=%d",
			enabled,
			generation,
			clients,
		)
	}
}
