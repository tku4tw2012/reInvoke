// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	authorityConnectTimeout = 2 * time.Second
	authorityCommandTimeout = 3 * time.Second
	authorityInitialTimeout = 15 * time.Second
	maximumAuthorityLine    = 160
)

type authorityEvent struct {
	kind    string
	epoch   string
	muted   bool
	result  chan error
	initial bool
}

type authoritySession struct {
	connection net.Conn
	events     <-chan authorityEvent
	done       <-chan error
}

func startAuthoritySession(
	ctx context.Context,
	path string,
	generation uint64,
) (*authoritySession, error) {
	dialer := net.Dialer{Timeout: authorityConnectTimeout}
	connection, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect privacy authority: %w", err)
	}
	hello := fmt.Sprintf("HELLO %016x\n", generation)
	if err := writeAll(connection, []byte(hello), authorityCommandTimeout); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("send privacy hello: %w", err)
	}
	events := make(chan authorityEvent)
	done := make(chan error, 1)
	go runAuthorityProtocol(ctx, connection, events, done)
	return &authoritySession{
		connection: connection,
		events:     events,
		done:       done,
	}, nil
}

func runAuthorityProtocol(
	ctx context.Context,
	connection net.Conn,
	events chan<- authorityEvent,
	done chan<- error,
) {
	defer close(events)
	defer close(done)
	defer connection.Close()
	reader := bufio.NewReaderSize(connection, maximumAuthorityLine)
	initial := true
	for {
		if initial {
			if err := connection.SetReadDeadline(
				time.Now().Add(authorityInitialTimeout),
			); err != nil {
				done <- err
				return
			}
		}
		line, err := readBoundedLine(reader, maximumAuthorityLine)
		if err != nil {
			done <- fmt.Errorf("read privacy authority: %w", err)
			return
		}
		fields := strings.Fields(line)
		event := authorityEvent{result: make(chan error, 1)}
		switch {
		case len(fields) == 1 && fields[0] == "BLOCK":
			event.kind = "block"
		case len(fields) == 1 && fields[0] == "DRAIN":
			event.kind = "drain"
		case len(fields) == 3 && fields[0] == "STATE":
			event.kind = "state"
			event.epoch = fields[1]
			switch fields[2] {
			case "MUTED":
				event.muted = true
			case "UNMUTED":
				event.muted = false
			default:
				done <- errors.New("privacy authority returned invalid state")
				return
			}
			if _, err := parseEpoch(event.epoch); err != nil {
				done <- err
				return
			}
			event.initial = initial
			initial = false
			if err := connection.SetReadDeadline(time.Time{}); err != nil {
				done <- err
				return
			}
		case len(fields) == 2 && fields[0] == "ALLOW":
			event.kind = "allow"
			event.epoch = fields[1]
			if _, err := parseEpoch(event.epoch); err != nil {
				done <- err
				return
			}
		default:
			done <- errors.New("privacy authority command is invalid")
			return
		}

		select {
		case events <- event:
		case <-ctx.Done():
			done <- nil
			return
		}
		var response string
		select {
		case err := <-event.result:
			if err != nil {
				if event.kind == "state" {
					done <- err
					return
				}
				response = "DENIED\n"
			} else {
				switch event.kind {
				case "block":
					response = "BLOCKED\n"
				case "drain":
					response = "DRAINED\n"
				case "allow":
					response = "ALLOWED\n"
				case "state":
					// STATE is a one-way notification. Sending an
					// unsolicited response would revoke the MCU session.
					continue
				}
			}
		case <-ctx.Done():
			done <- nil
			return
		}
		if err := writeAll(
			connection,
			[]byte(response),
			authorityCommandTimeout,
		); err != nil {
			done <- fmt.Errorf("reply to privacy authority: %w", err)
			return
		}
	}
}

func readBoundedLine(reader *bufio.Reader, maximum int) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > maximum {
		return "", errors.New("privacy authority command is too large")
	}
	return strings.TrimSuffix(line, "\n"), nil
}

func parseEpoch(value string) (uint64, error) {
	if len(value) != 32 {
		return 0, errors.New("privacy authority epoch has invalid length")
	}
	// Checking both halves validates all 32 hexadecimal characters.
	if _, err := strconv.ParseUint(value[:16], 16, 64); err != nil {
		return 0, errors.New("privacy authority epoch is invalid")
	}
	return strconv.ParseUint(value[16:], 16, 64)
}

func (session *authoritySession) close() {
	_ = session.connection.Close()
}
