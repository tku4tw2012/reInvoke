// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestConsumeGenerationReadsExactFraming(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	var log bytes.Buffer
	var raw bytes.Buffer
	go func() {
		defer server.Close()
		header := make([]byte, streamHeaderSize)
		copy(header[:8], "RINVOMIC")
		binary.LittleEndian.PutUint16(header[8:10], 1)
		binary.LittleEndian.PutUint16(header[10:12], streamHeaderSize)
		binary.LittleEndian.PutUint32(header[12:16], 48000)
		binary.LittleEndian.PutUint16(header[16:18], 1)
		binary.LittleEndian.PutUint16(header[18:20], 1)
		binary.LittleEndian.PutUint32(header[20:24], framesPerRecord)
		binary.LittleEndian.PutUint64(header[24:32], 9)
		record := make([]byte, recordSize)
		binary.LittleEndian.PutUint64(record[0:8], 9)
		binary.LittleEndian.PutUint64(record[8:16], 2)
		binary.LittleEndian.PutUint32(record[recordHeaderSize:], 123)
		_, _ = server.Write(append(header, record...))
	}()
	var records, samples, nonzero uint64
	err := consumeGeneration(
		client,
		time.Now().Add(time.Second),
		&raw,
		json.NewEncoder(&log),
		&records,
		&samples,
		&nonzero,
	)
	if err != nil && err != io.EOF && records == 0 {
		t.Fatalf("consume: %v", err)
	}
	if records != 1 || samples != framesPerRecord || nonzero != 1 {
		t.Fatalf(
			"records=%d samples=%d nonzero=%d",
			records,
			samples,
			nonzero,
		)
	}
	if raw.Len() != framesPerRecord*bytesPerSample {
		t.Fatalf("raw bytes = %d", raw.Len())
	}
}

func TestConsumeGenerationRejectsIncompatibleHeader(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		header := make([]byte, streamHeaderSize)
		copy(header[:8], "NOTAMIC!")
		_, _ = server.Write(header)
	}()
	var records, samples, nonzero uint64
	if err := consumeGeneration(
		client,
		time.Now().Add(time.Second),
		io.Discard,
		json.NewEncoder(io.Discard),
		&records,
		&samples,
		&nonzero,
	); err == nil {
		t.Fatal("incompatible header was accepted")
	}
}

func TestConsumeGenerationTreatsDurationDeadlineAsSuccess(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() {
		header := make([]byte, streamHeaderSize)
		copy(header[:8], "RINVOMIC")
		binary.LittleEndian.PutUint16(header[8:10], 1)
		binary.LittleEndian.PutUint16(header[10:12], streamHeaderSize)
		binary.LittleEndian.PutUint32(header[12:16], 48000)
		binary.LittleEndian.PutUint16(header[16:18], 1)
		binary.LittleEndian.PutUint16(header[18:20], 1)
		binary.LittleEndian.PutUint32(header[20:24], framesPerRecord)
		binary.LittleEndian.PutUint64(header[24:32], 9)
		_, _ = server.Write(header)
	}()
	var records, samples, nonzero uint64
	if err := consumeGeneration(
		client,
		time.Now().Add(20*time.Millisecond),
		io.Discard,
		json.NewEncoder(io.Discard),
		&records,
		&samples,
		&nonzero,
	); err != nil {
		t.Fatalf("deadline was not treated as normal completion: %v", err)
	}
}

func TestReconnectBacksOffAfterImmediateEOF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan int, 1)
	go func() {
		count := 0
		deadline := time.Now().Add(450 * time.Millisecond)
		for time.Now().Before(deadline) {
			_ = listener.(*net.UnixListener).SetDeadline(deadline)
			connection, err := listener.Accept()
			if err != nil {
				break
			}
			count++
			connection.Close()
		}
		accepted <- count
	}()
	if err := run(path, 350*time.Millisecond, "", true); err != nil {
		t.Fatalf("reconnecting client: %v", err)
	}
	count := <-accepted
	if count < 2 || count > 5 {
		t.Fatalf("accepted %d connections, want bounded retry", count)
	}
}
