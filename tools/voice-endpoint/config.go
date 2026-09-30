// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const maxConfigBytes = 4096

type config struct {
	Endpoint    string
	Pin         [sha256.Size]byte
	Token       string
	Device      string
	IOTimeoutMS int
}

func loadConfig(path string) (config, error) {
	file, err := openRegular(path, syscall.O_RDONLY, 0)
	if err != nil {
		return config{}, errors.New("cannot open private config as a regular, non-symlink file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return config{}, errors.New("cannot stat private config")
	}
	if info.Mode().Perm()&0077 != 0 {
		return config{}, errors.New("config permissions must exclude group and other access (use 0600)")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return config{}, errors.New("cannot read private config")
	}
	return parseConfig(data)
}

func parseConfig(data []byte) (config, error) {
	cfg := config{Device: "reinvoke", IOTimeoutMS: 30000}
	if len(data) > maxConfigBytes {
		return config{}, errors.New("config exceeds 4096 bytes")
	}
	fields, err := jsonObject(data)
	if err != nil {
		return config{}, errors.New("config must be one valid JSON object with unique fields")
	}
	for key, raw := range fields {
		var target interface{}
		switch key {
		case "endpoint":
			target = &cfg.Endpoint
		case "certificate_sha256", "token":
			var text string
			if err := json.Unmarshal(raw, &text); err != nil || len(text) != 64 {
				return config{}, fmt.Errorf("config %s must contain 64 hex characters", key)
			}
			decoded, err := hex.DecodeString(text)
			if err != nil {
				return config{}, fmt.Errorf("config %s must contain 64 hex characters", key)
			}
			if key == "token" {
				cfg.Token = text
			} else {
				copy(cfg.Pin[:], decoded)
			}
			continue
		case "device":
			target = &cfg.Device
		case "io_timeout_ms":
			target = &cfg.IOTimeoutMS
		default:
			// Field names and decoder errors can themselves contain a secret.
			return config{}, errors.New("config contains an unknown field")
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, target) != nil {
			return config{}, fmt.Errorf("config %s has an invalid type", key)
		}
	}
	for _, required := range []string{"endpoint", "certificate_sha256", "token"} {
		if _, ok := fields[required]; !ok {
			return config{}, fmt.Errorf("config requires %s", required)
		}
	}
	host, _, err := splitAddress(cfg.Endpoint)
	if err != nil || net.ParseIP(host) != nil || !validHostname(host) {
		return config{}, errors.New("config endpoint must be a hostname:port with port 1..65535")
	}
	if !validDevice(cfg.Device) {
		return config{}, errors.New("config device must be 1..64 ASCII letters, digits, dots, underscores or hyphens")
	}
	if cfg.IOTimeoutMS < 200 || cfg.IOTimeoutMS > 180000 {
		return config{}, errors.New("config io_timeout_ms must be 200..180000")
	}
	return cfg, nil
}

func jsonObject(data []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("invalid JSON encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("expected JSON object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, errors.New("invalid JSON field")
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("invalid JSON field")
		}
		if _, exists := fields[name]; exists {
			return nil, errors.New("duplicate JSON field")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, errors.New("invalid JSON value")
		}
		fields[name] = raw
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, errors.New("invalid JSON object")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing JSON data")
	}
	return fields, nil
}

func splitAddress(address string) (string, string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return "", "", errors.New("expected host:port")
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return "", "", errors.New("expected numeric port")
		}
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", "", errors.New("invalid port")
	}
	return host, port, nil
}

func validHostname(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !asciiAlphaNumeric(ch) && ch != '-' {
				return false
			}
		}
	}
	return true
}

func validDevice(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, ch := range name {
		if !asciiAlphaNumeric(ch) && ch != '.' && ch != '_' && ch != '-' {
			return false
		}
	}
	return true
}

func asciiAlphaNumeric(ch rune) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
}

func openRegular(path string, flags int, mode uint32) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, mode)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("not a readable regular file")
	}
	return file, nil
}
