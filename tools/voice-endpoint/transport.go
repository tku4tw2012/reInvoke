// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	helloLimit       = 1024
	handshakeTimeout = 5 * time.Second
)

type hello struct {
	Protocol string `json:"protocol"`
	Device   string `json:"device"`
	Nonce    uint32 `json:"nonce"`
	Token    string `json:"token"`
	Source   string `json:"source"`
	Playback bool   `json:"playback"`
}

func newNonce() (uint32, error) {
	for {
		var data [4]byte
		if _, err := io.ReadFull(rand.Reader, data[:]); err != nil {
			return 0, errors.New("cannot generate session nonce")
		}
		if nonce := binary.LittleEndian.Uint32(data[:]); nonce != 0 {
			return nonce, nil
		}
	}
}

func pinnedTLS(host string, pin [sha256.Size]byte) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: host,
		// The unit has no reliable wall clock. A mandatory, exact DER leaf pin
		// replaces CA/name/date verification; there is no unpinned mode.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("host did not provide a pinned certificate")
			}
			actual := sha256.Sum256(state.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(actual[:], pin[:]) != 1 {
				return errors.New("host certificate pin mismatch")
			}
			return nil
		},
	}
}

// The raw socket close interrupts blocked TLS reads AND writes without waiting
// for a TLS close_notify write to a peer that has stopped reading.
type hostConnection struct {
	net.Conn
	raw      net.Conn
	stop     func()
	closeOne sync.Once
}

func (connection *hostConnection) Close() error {
	var err error
	connection.closeOne.Do(func() {
		err = connection.raw.Close()
		connection.stop()
	})
	return err
}

func closeOnCancel(ctx context.Context, connection net.Conn) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			connection.Close()
		case <-stop:
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func connectHost(ctx context.Context, cfg config, opts options) (*hostConnection, uint32, error) {
	nonce, err := newNonce()
	if err != nil {
		return nil, 0, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", cfg.Endpoint)
	if err != nil {
		return nil, 0, fmt.Errorf("connect voice host: %w", err)
	}
	host, _, _ := net.SplitHostPort(cfg.Endpoint)
	secure := tls.Client(raw, pinnedTLS(host, cfg.Pin))
	connection := &hostConnection{Conn: secure, raw: raw, stop: closeOnCancel(ctx, raw)}
	ok := false
	defer func() {
		if !ok {
			connection.Close()
		}
	}()
	deadline, _ := dialCtx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, 0, fmt.Errorf("set TLS handshake deadline: %w", err)
	}
	if err := secure.HandshakeContext(dialCtx); err != nil {
		return nil, 0, fmt.Errorf("authenticate pinned TLS host: %w", err)
	}
	message := hello{
		Protocol: "RIWAKE03", Device: cfg.Device, Nonce: nonce,
		Token: cfg.Token, Source: "socket", Playback: !opts.NoPlayback,
	}
	if opts.Fixture != "" {
		message.Source = "fixture"
	}
	if err := exchangeHello(connection, message); err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, err
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return nil, 0, fmt.Errorf("clear host idle deadline: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	ok = true
	return connection, nonce, nil
}

func exchangeHello(connection net.Conn, message hello) error {
	data, err := json.Marshal(message)
	if err != nil || len(data)+1 > helloLimit {
		return errors.New("host hello exceeds the JSON line limit")
	}
	if err := connection.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return fmt.Errorf("set host hello deadline: %w", err)
	}
	if err := writeAll(connection, append(data, '\n')); err != nil {
		return fmt.Errorf("send host hello: %w", err)
	}
	reader := bufio.NewReaderSize(connection, helloLimit)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > helloLimit {
		return errors.New("host hello acknowledgment is missing, incomplete or exceeds 1024 bytes")
	}
	if reader.Buffered() != 0 {
		return errors.New("host sent unexpected bytes after hello acknowledgment")
	}
	fields, err := jsonObject(line)
	if err != nil {
		return errors.New("host hello acknowledgment is invalid")
	}
	// Do not reflect host-supplied error text: it could echo the hello token.
	if len(fields) != 1 || !bytes.Equal(bytes.TrimSpace(fields["ok"]), []byte("true")) {
		return errors.New("host rejected hello or sent an invalid acknowledgment")
	}
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) != 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
