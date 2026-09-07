// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthorityProtocolBlocksBeforeAuthorizing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	const (
		generation = uint64(0x1122)
		epoch      = "00112233445566778899aabbccddeeff"
	)
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		hello, err := reader.ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}
		if hello != fmt.Sprintf("HELLO %016x\n", generation) {
			serverDone <- fmt.Errorf("hello = %q", hello)
			return
		}
		for _, exchange := range []struct {
			command string
			reply   string
		}{
			{"BLOCK\n", "BLOCKED\n"},
			{"DRAIN\n", "DRAINED\n"},
			{"STATE " + epoch + " UNMUTED\n", ""},
			{"ALLOW " + epoch + "\n", "ALLOWED\n"},
		} {
			if _, err := connection.Write([]byte(exchange.command)); err != nil {
				serverDone <- err
				return
			}
			if exchange.reply != "" {
				reply, err := reader.ReadString('\n')
				if err != nil {
					serverDone <- err
					return
				}
				if reply != exchange.reply {
					serverDone <- fmt.Errorf(
						"reply = %q, want %q",
						reply,
						exchange.reply,
					)
					return
				}
			}
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithCancel(context.Background())
	session, err := startAuthoritySession(ctx, path, generation)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		session.close()
	}()
	var kinds []string
	for len(kinds) < 4 {
		select {
		case event := <-session.events:
			kinds = append(kinds, event.kind)
			event.result <- nil
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for authority event")
		}
	}
	if got := fmt.Sprint(kinds); got != "[block drain state allow]" {
		t.Fatalf("events = %s", got)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestAuthorityProtocolRejectsMalformedState(t *testing.T) {
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan authorityEvent)
	done := make(chan error, 1)
	go runAuthorityProtocol(ctx, client, events, done)
	if _, err := server.Write([]byte("STATE bad UNMUTED\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("malformed state was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("malformed state did not stop the protocol")
	}
	server.Close()
}
