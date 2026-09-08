// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"testing"
)

func TestStreamHeaderDescribesDeliveredFormat(t *testing.T) {
	const generation = uint64(0x1122334455667788)
	header := encodeStreamHeader(generation)
	if len(header) != streamHeaderSize {
		t.Fatalf("header length = %d, want %d", len(header), streamHeaderSize)
	}
	if string(header[:8]) != streamMagic {
		t.Fatalf("magic = %q", header[:8])
	}
	if got := binary.LittleEndian.Uint16(header[8:10]); got != streamVersion {
		t.Fatalf("version = %d", got)
	}
	if got := binary.LittleEndian.Uint32(header[12:16]); got != sampleRate {
		t.Fatalf("sample rate = %d", got)
	}
	if got := binary.LittleEndian.Uint16(header[16:18]); got != deliveredChannels {
		t.Fatalf("channels = %d", got)
	}
	if got := binary.LittleEndian.Uint16(header[18:20]); got != formatS32LE {
		t.Fatalf("format = %d", got)
	}
	if got := binary.LittleEndian.Uint32(header[20:24]); got != framesPerPeriod {
		t.Fatalf("frames per packet = %d", got)
	}
	if got := binary.LittleEndian.Uint64(header[24:32]); got != generation {
		t.Fatalf("generation = %#x", got)
	}
}

func TestEncodeRecordSelectsDonorVoiceRecognitionChannel(t *testing.T) {
	stereo := make([]byte, nativePeriodBytes)
	for frame := 0; frame < framesPerPeriod; frame++ {
		offset := frame * nativeChannels * bytesPerSample
		binary.LittleEndian.PutUint32(
			stereo[offset:offset+bytesPerSample],
			uint32(frame+1),
		)
		binary.LittleEndian.PutUint32(
			stereo[offset+bytesPerSample:offset+2*bytesPerSample],
			uint32(0x70000000+frame),
		)
	}
	record, err := encodeRecord(7, 11, 13, stereo)
	if err != nil {
		t.Fatalf("encode record: %v", err)
	}
	if len(record) != recordSize {
		t.Fatalf("record size = %d, want %d", len(record), recordSize)
	}
	if got := binary.LittleEndian.Uint64(record[0:8]); got != 7 {
		t.Fatalf("generation = %d", got)
	}
	if got := binary.LittleEndian.Uint64(record[8:16]); got != 11 {
		t.Fatalf("sequence = %d", got)
	}
	if got := binary.LittleEndian.Uint64(record[16:24]); got != 13 {
		t.Fatalf("timestamp = %d", got)
	}
	for frame := 0; frame < framesPerPeriod; frame++ {
		offset := recordHeaderSize + frame*bytesPerSample
		if got := binary.LittleEndian.Uint32(
			record[offset : offset+bytesPerSample],
		); got != uint32(frame+1) {
			t.Fatalf("frame %d = %#x, want left channel %#x", frame, got, frame+1)
		}
	}
}

func TestEncodeRecordRejectsPartialPeriod(t *testing.T) {
	if _, err := encodeRecord(1, 1, 1, make([]byte, nativePeriodBytes-1)); err == nil {
		t.Fatal("partial period must be rejected")
	}
}
