// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"net"
	"path/filepath"
	"testing"
)

// newStubDSPSocket stands in for the DSP control socket.
//
// The volume controller reaches the amplifier over a unix socket rather than
// an external command, so tests need a listener that answers the same way the
// DSP service does. It accepts every request and replies OK, which is enough
// to exercise the controller's own state handling; the protocol itself is
// covered on the DSP side.
func newStubDSPSocket(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsp.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("stub DSP socket: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			buffer := make([]byte, 16)
			_, _ = connection.Read(buffer)
			_, _ = connection.Write([]byte("OK\n"))
			_ = connection.Close()
		}
	}()
	return path
}
