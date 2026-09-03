// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

// Minimal MessagePack codec, enough for the WAMP RawSocket messages this
// service exchanges with Bonefish. No third-party module is used anywhere in
// this program.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

func encodeMessagePack(value interface{}) ([]byte, error) {
	return appendValue(nil, value)
}

func appendValue(out []byte, value interface{}) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return append(out, 0xc0), nil
	case bool:
		if typed {
			return append(out, 0xc3), nil
		}
		return append(out, 0xc2), nil
	case string:
		return appendString(out, typed), nil
	case []byte:
		return appendBinary(out, typed)
	case int:
		return appendInteger(out, int64(typed)), nil
	case int64:
		return appendInteger(out, typed), nil
	case uint64:
		if typed > math.MaxInt64 {
			return nil, errors.New("unsigned value is too large")
		}
		return appendInteger(out, int64(typed)), nil
	case uint32:
		return appendInteger(out, int64(typed)), nil
	case float64:
		out = append(out, 0xcb)
		var buffer [8]byte
		binary.BigEndian.PutUint64(buffer[:], math.Float64bits(typed))
		return append(out, buffer[:]...), nil
	case []interface{}:
		return appendArray(out, typed)
	case map[string]interface{}:
		return appendMap(out, typed)
	default:
		return nil, fmt.Errorf("unsupported MessagePack value %T", value)
	}
}

func appendInteger(out []byte, value int64) []byte {
	switch {
	case value >= 0 && value <= 0x7f:
		return append(out, byte(value))
	case value < 0 && value >= -32:
		return append(out, byte(0x100+value))
	case value >= 0 && value <= math.MaxUint8:
		return append(out, 0xcc, byte(value))
	case value >= 0 && value <= math.MaxUint16:
		var buffer [2]byte
		binary.BigEndian.PutUint16(buffer[:], uint16(value))
		return append(append(out, 0xcd), buffer[:]...)
	case value >= 0 && value <= math.MaxUint32:
		var buffer [4]byte
		binary.BigEndian.PutUint32(buffer[:], uint32(value))
		return append(append(out, 0xce), buffer[:]...)
	case value >= 0:
		var buffer [8]byte
		binary.BigEndian.PutUint64(buffer[:], uint64(value))
		return append(append(out, 0xcf), buffer[:]...)
	case value >= math.MinInt8:
		return append(out, 0xd0, byte(value))
	case value >= math.MinInt16:
		var buffer [2]byte
		binary.BigEndian.PutUint16(buffer[:], uint16(value))
		return append(append(out, 0xd1), buffer[:]...)
	case value >= math.MinInt32:
		var buffer [4]byte
		binary.BigEndian.PutUint32(buffer[:], uint32(value))
		return append(append(out, 0xd2), buffer[:]...)
	default:
		var buffer [8]byte
		binary.BigEndian.PutUint64(buffer[:], uint64(value))
		return append(append(out, 0xd3), buffer[:]...)
	}
}

func appendString(out []byte, value string) []byte {
	length := len(value)
	switch {
	case length <= 31:
		out = append(out, 0xa0|byte(length))
	case length <= math.MaxUint8:
		out = append(out, 0xd9, byte(length))
	case length <= math.MaxUint16:
		var buffer [2]byte
		binary.BigEndian.PutUint16(buffer[:], uint16(length))
		out = append(append(out, 0xda), buffer[:]...)
	default:
		var buffer [4]byte
		binary.BigEndian.PutUint32(buffer[:], uint32(length))
		out = append(append(out, 0xdb), buffer[:]...)
	}
	return append(out, value...)
}

func appendBinary(out []byte, value []byte) ([]byte, error) {
	length := len(value)
	switch {
	case length <= math.MaxUint8:
		out = append(out, 0xc4, byte(length))
	case length <= math.MaxUint16:
		var buffer [2]byte
		binary.BigEndian.PutUint16(buffer[:], uint16(length))
		out = append(append(out, 0xc5), buffer[:]...)
	default:
		return nil, errors.New("binary value is too long")
	}
	return append(out, value...), nil
}

func appendArray(out []byte, values []interface{}) ([]byte, error) {
	length := len(values)
	switch {
	case length <= 15:
		out = append(out, 0x90|byte(length))
	case length <= math.MaxUint16:
		var buffer [2]byte
		binary.BigEndian.PutUint16(buffer[:], uint16(length))
		out = append(append(out, 0xdc), buffer[:]...)
	default:
		return nil, errors.New("array is too long")
	}
	var err error
	for _, value := range values {
		if out, err = appendValue(out, value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func appendMap(out []byte, values map[string]interface{}) ([]byte, error) {
	length := len(values)
	switch {
	case length <= 15:
		out = append(out, 0x80|byte(length))
	case length <= math.MaxUint16:
		var buffer [2]byte
		binary.BigEndian.PutUint16(buffer[:], uint16(length))
		out = append(append(out, 0xde), buffer[:]...)
	default:
		return nil, errors.New("map is too large")
	}
	var err error
	for key, value := range values {
		out = appendString(out, key)
		if out, err = appendValue(out, value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func decodeMessagePack(payload []byte) (interface{}, error) {
	value, rest, err := decodeValue(payload)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("trailing MessagePack bytes")
	}
	return value, nil
}

func decodeValue(payload []byte) (interface{}, []byte, error) {
	if len(payload) == 0 {
		return nil, nil, errors.New("truncated MessagePack value")
	}
	tag := payload[0]
	rest := payload[1:]

	switch {
	case tag <= 0x7f:
		return uint64(tag), rest, nil
	case tag >= 0xe0:
		return int64(int8(tag)), rest, nil
	case tag&0xf0 == 0x80:
		return decodeMap(rest, int(tag&0x0f))
	case tag&0xf0 == 0x90:
		return decodeArray(rest, int(tag&0x0f))
	case tag&0xe0 == 0xa0:
		return decodeString(rest, int(tag&0x1f))
	}

	switch tag {
	case 0xc0:
		return nil, rest, nil
	case 0xc2:
		return false, rest, nil
	case 0xc3:
		return true, rest, nil
	case 0xc4, 0xc5:
		width := 1
		if tag == 0xc5 {
			width = 2
		}
		length, rest, err := decodeLength(rest, width)
		if err != nil {
			return nil, nil, err
		}
		if len(rest) < length {
			return nil, nil, errors.New("truncated MessagePack binary")
		}
		return append([]byte(nil), rest[:length]...), rest[length:], nil
	case 0xca:
		if len(rest) < 4 {
			return nil, nil, errors.New("truncated MessagePack float")
		}
		bits := binary.BigEndian.Uint32(rest[:4])
		return float64(math.Float32frombits(bits)), rest[4:], nil
	case 0xcb:
		if len(rest) < 8 {
			return nil, nil, errors.New("truncated MessagePack double")
		}
		bits := binary.BigEndian.Uint64(rest[:8])
		return math.Float64frombits(bits), rest[8:], nil
	case 0xcc, 0xcd, 0xce, 0xcf:
		width := 1 << (tag - 0xcc)
		value, rest, err := decodeUnsigned(rest, width)
		if err != nil {
			return nil, nil, err
		}
		return value, rest, nil
	case 0xd0, 0xd1, 0xd2, 0xd3:
		width := 1 << (tag - 0xd0)
		value, rest, err := decodeUnsigned(rest, width)
		if err != nil {
			return nil, nil, err
		}
		return signed(value, width), rest, nil
	case 0xd9, 0xda, 0xdb:
		width := 1 << (tag - 0xd9)
		length, rest, err := decodeLength(rest, width)
		if err != nil {
			return nil, nil, err
		}
		return decodeString(rest, length)
	case 0xdc, 0xdd:
		width := 2
		if tag == 0xdd {
			width = 4
		}
		length, rest, err := decodeLength(rest, width)
		if err != nil {
			return nil, nil, err
		}
		return decodeArray(rest, length)
	case 0xde, 0xdf:
		width := 2
		if tag == 0xdf {
			width = 4
		}
		length, rest, err := decodeLength(rest, width)
		if err != nil {
			return nil, nil, err
		}
		return decodeMap(rest, length)
	}
	return nil, nil, fmt.Errorf("unsupported MessagePack tag 0x%02x", tag)
}

func signed(value uint64, width int) int64 {
	switch width {
	case 1:
		return int64(int8(value))
	case 2:
		return int64(int16(value))
	case 4:
		return int64(int32(value))
	default:
		return int64(value)
	}
}

func decodeLength(payload []byte, width int) (int, []byte, error) {
	value, rest, err := decodeUnsigned(payload, width)
	if err != nil {
		return 0, nil, err
	}
	maxInt := uint64(^uint(0) >> 1)
	if value > maxInt {
		return 0, nil, errors.New("MessagePack length exceeds platform limit")
	}
	return int(value), rest, nil
}

func decodeUnsigned(payload []byte, width int) (uint64, []byte, error) {
	if len(payload) < width {
		return 0, nil, errors.New("truncated MessagePack length")
	}
	var value uint64
	for _, part := range payload[:width] {
		value = value<<8 | uint64(part)
	}
	return value, payload[width:], nil
}

func decodeString(payload []byte, length int) (interface{}, []byte, error) {
	if len(payload) < length {
		return nil, nil, errors.New("truncated MessagePack string")
	}
	return string(payload[:length]), payload[length:], nil
}

func decodeArray(payload []byte, length int) (interface{}, []byte, error) {
	if length > len(payload) {
		return nil, nil, errors.New("MessagePack array length exceeds payload")
	}
	values := make([]interface{}, 0, length)
	rest := payload
	for index := 0; index < length; index++ {
		value, remainder, err := decodeValue(rest)
		if err != nil {
			return nil, nil, err
		}
		values = append(values, value)
		rest = remainder
	}
	return values, rest, nil
}

func decodeMap(payload []byte, length int) (interface{}, []byte, error) {
	if length > len(payload)/2 {
		return nil, nil, errors.New("MessagePack map length exceeds payload")
	}
	values := make(map[string]interface{}, length)
	rest := payload
	for index := 0; index < length; index++ {
		key, remainder, err := decodeValue(rest)
		if err != nil {
			return nil, nil, err
		}
		value, next, err := decodeValue(remainder)
		if err != nil {
			return nil, nil, err
		}
		values[fmt.Sprint(key)] = value
		rest = next
	}
	return values, rest, nil
}
