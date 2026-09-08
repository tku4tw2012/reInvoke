// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"errors"
)

const (
	streamMagic          = "RINVOMIC"
	streamVersion        = uint16(1)
	streamHeaderSize     = 32
	recordHeaderSize     = 24
	nativeChannels       = 2
	deliveredChannels    = 1
	sampleRate           = 48000
	bytesPerSample       = 4
	framesPerPeriod      = 256
	nativePeriodBytes    = framesPerPeriod * nativeChannels * bytesPerSample
	deliveredPeriodBytes = framesPerPeriod * deliveredChannels * bytesPerSample
	recordSize           = recordHeaderSize + deliveredPeriodBytes
	formatS32LE          = uint16(1)
)

func encodeStreamHeader(generation uint64) []byte {
	header := make([]byte, streamHeaderSize)
	copy(header[0:8], streamMagic)
	binary.LittleEndian.PutUint16(header[8:10], streamVersion)
	binary.LittleEndian.PutUint16(header[10:12], streamHeaderSize)
	binary.LittleEndian.PutUint32(header[12:16], sampleRate)
	binary.LittleEndian.PutUint16(header[16:18], deliveredChannels)
	binary.LittleEndian.PutUint16(header[18:20], formatS32LE)
	binary.LittleEndian.PutUint32(header[20:24], framesPerPeriod)
	binary.LittleEndian.PutUint64(header[24:32], generation)
	return header
}

func encodeRecord(
	generation uint64,
	sequence uint64,
	timestampNS uint64,
	stereo []byte,
) ([]byte, error) {
	if len(stereo) != nativePeriodBytes {
		return nil, errors.New("capture period has an invalid size")
	}
	record := make([]byte, recordSize)
	binary.LittleEndian.PutUint64(record[0:8], generation)
	binary.LittleEndian.PutUint64(record[8:16], sequence)
	binary.LittleEndian.PutUint64(record[16:24], timestampNS)
	output := record[recordHeaderSize:]
	for frame := 0; frame < framesPerPeriod; frame++ {
		source := frame * nativeChannels * bytesPerSample
		destination := frame * bytesPerSample
		copy(
			output[destination:destination+bytesPerSample],
			stereo[source:source+bytesPerSample],
		)
	}
	return record, nil
}
