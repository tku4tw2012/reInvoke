// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import "github.com/tku4tw2012/reinvoke/tools/control/msgpack"

const maxMessagePackDepth = msgpack.MaxDepth

func encodeMessagePack(value interface{}) ([]byte, error) {
	return msgpack.Encode(value)
}

func decodeMessagePack(payload []byte) (interface{}, error) {
	return msgpack.Decode(payload)
}
