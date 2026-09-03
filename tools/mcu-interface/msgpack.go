// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

func encodeMessagePack(value interface{}) ([]byte, error) {
	var buffer bytes.Buffer
	if err := writeMessagePack(&buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeMessagePack(writer *bytes.Buffer, value interface{}) error {
	switch item := value.(type) {
	case nil:
		return writer.WriteByte(0xc0)
	case bool:
		if item {
			return writer.WriteByte(0xc3)
		}
		return writer.WriteByte(0xc2)
	case int:
		return writeInteger(writer, int64(item))
	case int64:
		return writeInteger(writer, item)
	case uint64:
		return writeUnsigned(writer, item)
	case string:
		return writeString(writer, item)
	case []interface{}:
		return writeArray(writer, item)
	case map[string]interface{}:
		return writeMap(writer, item)
	default:
		return fmt.Errorf("unsupported MessagePack type %T", value)
	}
}

func writeInteger(writer *bytes.Buffer, value int64) error {
	if value >= 0 {
		return writeUnsigned(writer, uint64(value))
	}
	if value >= -32 {
		return writer.WriteByte(byte(value))
	}
	if value >= math.MinInt8 {
		_ = writer.WriteByte(0xd0)
		return writer.WriteByte(byte(value))
	}
	if value >= math.MinInt16 {
		_ = writer.WriteByte(0xd1)
		return binary.Write(writer, binary.BigEndian, int16(value))
	}
	if value >= math.MinInt32 {
		_ = writer.WriteByte(0xd2)
		return binary.Write(writer, binary.BigEndian, int32(value))
	}
	_ = writer.WriteByte(0xd3)
	return binary.Write(writer, binary.BigEndian, value)
}

func writeUnsigned(writer *bytes.Buffer, value uint64) error {
	switch {
	case value <= 0x7f:
		return writer.WriteByte(byte(value))
	case value <= math.MaxUint8:
		_ = writer.WriteByte(0xcc)
		return writer.WriteByte(byte(value))
	case value <= math.MaxUint16:
		_ = writer.WriteByte(0xcd)
		return binary.Write(writer, binary.BigEndian, uint16(value))
	case value <= math.MaxUint32:
		_ = writer.WriteByte(0xce)
		return binary.Write(writer, binary.BigEndian, uint32(value))
	default:
		_ = writer.WriteByte(0xcf)
		return binary.Write(writer, binary.BigEndian, value)
	}
}

func writeString(writer *bytes.Buffer, value string) error {
	length := len(value)
	switch {
	case length <= 31:
		_ = writer.WriteByte(0xa0 | byte(length))
	case length <= math.MaxUint8:
		_ = writer.WriteByte(0xd9)
		_ = writer.WriteByte(byte(length))
	case length <= math.MaxUint16:
		_ = writer.WriteByte(0xda)
		_ = binary.Write(writer, binary.BigEndian, uint16(length))
	default:
		_ = writer.WriteByte(0xdb)
		_ = binary.Write(writer, binary.BigEndian, uint32(length))
	}
	_, err := writer.WriteString(value)
	return err
}

func writeArray(writer *bytes.Buffer, values []interface{}) error {
	length := len(values)
	if length <= 15 {
		_ = writer.WriteByte(0x90 | byte(length))
	} else {
		_ = writer.WriteByte(0xdc)
		_ = binary.Write(writer, binary.BigEndian, uint16(length))
	}
	for _, value := range values {
		if err := writeMessagePack(writer, value); err != nil {
			return err
		}
	}
	return nil
}

func writeMap(writer *bytes.Buffer, values map[string]interface{}) error {
	length := len(values)
	if length <= 15 {
		_ = writer.WriteByte(0x80 | byte(length))
	} else {
		_ = writer.WriteByte(0xde)
		_ = binary.Write(writer, binary.BigEndian, uint16(length))
	}
	for key, value := range values {
		if err := writeString(writer, key); err != nil {
			return err
		}
		if err := writeMessagePack(writer, value); err != nil {
			return err
		}
	}
	return nil
}

type messagePackReader struct {
	reader *bytes.Reader
}

func decodeMessagePack(payload []byte) (interface{}, error) {
	decoder := messagePackReader{reader: bytes.NewReader(payload)}
	value, err := decoder.read()
	if err != nil {
		return nil, err
	}
	if decoder.reader.Len() != 0 {
		return nil, errors.New("trailing MessagePack data")
	}
	return value, nil
}

func (decoder *messagePackReader) read() (interface{}, error) {
	marker, err := decoder.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	switch {
	case marker <= 0x7f:
		return uint64(marker), nil
	case marker >= 0xe0:
		return int64(int8(marker)), nil
	case marker&0xf0 == 0x80:
		return decoder.readMap(uint32(marker & 0x0f))
	case marker&0xf0 == 0x90:
		return decoder.readArray(uint32(marker & 0x0f))
	case marker&0xe0 == 0xa0:
		return decoder.readString(uint32(marker & 0x1f))
	}

	switch marker {
	case 0xc0:
		return nil, nil
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xcc:
		value, err := decoder.reader.ReadByte()
		return uint64(value), err
	case 0xcd:
		var value uint16
		err := binary.Read(decoder.reader, binary.BigEndian, &value)
		return uint64(value), err
	case 0xce:
		var value uint32
		err := binary.Read(decoder.reader, binary.BigEndian, &value)
		return uint64(value), err
	case 0xcf:
		var value uint64
		err := binary.Read(decoder.reader, binary.BigEndian, &value)
		return value, err
	case 0xd0:
		value, err := decoder.reader.ReadByte()
		return int64(int8(value)), err
	case 0xd1:
		var value int16
		err := binary.Read(decoder.reader, binary.BigEndian, &value)
		return int64(value), err
	case 0xd2:
		var value int32
		err := binary.Read(decoder.reader, binary.BigEndian, &value)
		return int64(value), err
	case 0xd3:
		var value int64
		err := binary.Read(decoder.reader, binary.BigEndian, &value)
		return value, err
	case 0xd9:
		length, err := decoder.reader.ReadByte()
		if err != nil {
			return nil, err
		}
		return decoder.readString(uint32(length))
	case 0xda:
		var length uint16
		if err := binary.Read(decoder.reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		return decoder.readString(uint32(length))
	case 0xdb:
		var length uint32
		if err := binary.Read(decoder.reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		return decoder.readString(length)
	case 0xdc:
		var length uint16
		if err := binary.Read(decoder.reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		return decoder.readArray(uint32(length))
	case 0xdd:
		var length uint32
		if err := binary.Read(decoder.reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		return decoder.readArray(length)
	case 0xde:
		var length uint16
		if err := binary.Read(decoder.reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		return decoder.readMap(uint32(length))
	case 0xdf:
		var length uint32
		if err := binary.Read(decoder.reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
		return decoder.readMap(length)
	default:
		return nil, fmt.Errorf("unsupported MessagePack marker 0x%02x", marker)
	}
}

func (decoder *messagePackReader) readString(
	length uint32,
) (string, error) {
	buffer := make([]byte, length)
	if _, err := io.ReadFull(decoder.reader, buffer); err != nil {
		return "", err
	}
	return string(buffer), nil
}

func (decoder *messagePackReader) readArray(
	length uint32,
) ([]interface{}, error) {
	values := make([]interface{}, 0, length)
	for index := uint32(0); index < length; index++ {
		value, err := decoder.read()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (decoder *messagePackReader) readMap(
	length uint32,
) (map[string]interface{}, error) {
	values := make(map[string]interface{}, length)
	for index := uint32(0); index < length; index++ {
		key, err := decoder.read()
		if err != nil {
			return nil, err
		}
		text, ok := key.(string)
		if !ok {
			return nil, errors.New("MessagePack map key is not a string")
		}
		value, err := decoder.read()
		if err != nil {
			return nil, err
		}
		values[text] = value
	}
	return values, nil
}
