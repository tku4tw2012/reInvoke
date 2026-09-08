// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const (
	streamHeaderSize = 32
	recordHeaderSize = 24
	framesPerRecord  = 256
	bytesPerSample   = 4
	recordSize       = recordHeaderSize + framesPerRecord*bytesPerSample
)

type streamEvent struct {
	Type       string `json:"type"`
	Generation uint64 `json:"generation,omitempty"`
	Sequence   uint64 `json:"sequence,omitempty"`
	Records    uint64 `json:"records,omitempty"`
	Samples    uint64 `json:"samples,omitempty"`
	Nonzero    uint64 `json:"nonzero,omitempty"`
	Error      string `json:"error,omitempty"`
}

func main() {
	socket := flag.String(
		"socket",
		"/run/reinvoke/mic-capture/audio.sock",
		"microphone stream socket",
	)
	duration := flag.Duration("duration", 10*time.Second, "capture duration")
	output := flag.String("output", "", "optional raw mono S32_LE output")
	reconnect := flag.Bool("reconnect", false, "reconnect after generation loss")
	flag.Parse()
	if *duration <= 0 {
		fmt.Fprintln(os.Stderr, "duration must be positive")
		os.Exit(2)
	}
	if err := run(*socket, *duration, *output, *reconnect); err != nil {
		fmt.Fprintf(os.Stderr, "mic-capture-client: %v\n", err)
		os.Exit(1)
	}
}

func run(socket string, duration time.Duration, output string, reconnect bool) error {
	var writer io.Writer = io.Discard
	if output != "" {
		file, err := os.OpenFile(
			output,
			os.O_CREATE|os.O_EXCL|os.O_WRONLY,
			0o600,
		)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = file
	}
	encoder := json.NewEncoder(os.Stdout)
	deadline := time.Now().Add(duration)
	var (
		records uint64
		samples uint64
		nonzero uint64
	)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			if !reconnect {
				return err
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		err = consumeGeneration(
			connection,
			deadline,
			writer,
			encoder,
			&records,
			&samples,
			&nonzero,
		)
		connection.Close()
		if err != nil && !reconnect && !errors.Is(err, io.EOF) {
			return err
		}
		if !reconnect {
			break
		}
		// A blocked capture owner accepts and immediately closes connections.
		// Back off here just as we do after a failed dial so a muted client
		// cannot spin on connect/EOF.
		time.Sleep(100 * time.Millisecond)
	}
	return encoder.Encode(streamEvent{
		Type:    "summary",
		Records: records,
		Samples: samples,
		Nonzero: nonzero,
	})
}

func consumeGeneration(
	connection net.Conn,
	deadline time.Time,
	writer io.Writer,
	encoder *json.Encoder,
	records *uint64,
	samples *uint64,
	nonzero *uint64,
) error {
	header := make([]byte, streamHeaderSize)
	if _, err := io.ReadFull(connection, header); err != nil {
		return err
	}
	if string(header[:8]) != "RINVOMIC" ||
		binary.LittleEndian.Uint16(header[8:10]) != 1 ||
		binary.LittleEndian.Uint16(header[10:12]) != streamHeaderSize ||
		binary.LittleEndian.Uint32(header[12:16]) != 48000 ||
		binary.LittleEndian.Uint16(header[16:18]) != 1 ||
		binary.LittleEndian.Uint16(header[18:20]) != 1 ||
		binary.LittleEndian.Uint32(header[20:24]) != framesPerRecord {
		return errors.New("stream header is incompatible")
	}
	generation := binary.LittleEndian.Uint64(header[24:32])
	if err := encoder.Encode(streamEvent{
		Type:       "generation",
		Generation: generation,
	}); err != nil {
		return err
	}

	record := make([]byte, recordSize)
	for time.Now().Before(deadline) {
		if err := connection.SetReadDeadline(deadline); err != nil {
			return err
		}
		if _, err := io.ReadFull(connection, record); err != nil {
			if !time.Now().Before(deadline) {
				return nil
			}
			return err
		}
		if binary.LittleEndian.Uint64(record[0:8]) != generation {
			return errors.New("record generation changed in one stream")
		}
		sequence := binary.LittleEndian.Uint64(record[8:16])
		payload := record[recordHeaderSize:]
		if _, err := writer.Write(payload); err != nil {
			return err
		}
		*records++
		*samples += framesPerRecord
		for offset := 0; offset < len(payload); offset += bytesPerSample {
			if binary.LittleEndian.Uint32(
				payload[offset:offset+bytesPerSample],
			) != 0 {
				*nonzero++
			}
		}
		if sequence%188 == 0 {
			if err := encoder.Encode(streamEvent{
				Type:       "progress",
				Generation: generation,
				Sequence:   sequence,
				Records:    *records,
				Samples:    *samples,
				Nonzero:    *nonzero,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
