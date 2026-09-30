// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func configJSON(t *testing.T, changes map[string]interface{}) []byte {
	t.Helper()
	fields := map[string]interface{}{
		"endpoint": "reinvoke-host:24443", "certificate_sha256": strings.Repeat("ab", 32), "token": testToken,
	}
	for key, value := range changes {
		fields[key] = value
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConfigDefaultsAndBoundaries(t *testing.T) {
	cfg, err := parseConfig(configJSON(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Device != "reinvoke" || cfg.IOTimeoutMS != 30000 {
		t.Fatal("missing optional config fields did not receive the specified defaults")
	}
	if cfg.Token != testToken || hex.EncodeToString(cfg.Pin[:]) != strings.Repeat("ab", 32) {
		t.Fatal("config credentials were not decoded correctly")
	}
	for _, changes := range []map[string]interface{}{
		{"io_timeout_ms": 200, "device": "A._-9", "endpoint": "localhost:1"},
		{"io_timeout_ms": 180000, "device": strings.Repeat("a", 64), "endpoint": "reinvoke-host.:65535"},
		{"certificate_sha256": strings.Repeat("AB", 32), "token": strings.ToUpper(testToken)},
	} {
		if _, err := parseConfig(configJSON(t, changes)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConfigRejectsInvalidWithoutSecrets(t *testing.T) {
	cases := map[string]interface{}{
		"endpoint_empty": "", "endpoint_scheme": "tls://reinvoke-host:443",
		"endpoint_ip": "192.0.2.1:443", "endpoint_ipv6": "[::1]:443",
		"endpoint_missing_port": "reinvoke-host", "endpoint_zero": "reinvoke-host:0",
		"endpoint_large_port": "reinvoke-host:65536", "endpoint_service": "reinvoke-host:https",
		"endpoint_signed_port": "reinvoke-host:+443", "endpoint_whitespace": "host name:443",
		"endpoint_bad_label": "-hostname:443", "endpoint_long_label": strings.Repeat("h", 64) + ":443",
		"endpoint_double_dot": "host..name:443", "endpoint_null": nil,
		"device_empty": "", "device_large": strings.Repeat("a", 65),
		"device_unicode": "\u00e9", "device_slash": "name/path", "device_null": nil,
		"post_ms_obsolete":    5000,
		"io_timeout_ms_small": 199, "io_timeout_ms_large": 180001, "io_timeout_ms_null": nil,
		"certificate_sha256_short": "abc", "certificate_sha256_bad": strings.Repeat("z", 64),
		"certificate_sha256_null": nil, "token_short": "abc", "token_space": testToken + " ",
		"token_bad": strings.Repeat("g", 64), "token_null": nil, "token_number": 123,
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			var field string
			for _, candidate := range []string{"certificate_sha256", "io_timeout_ms", "post_ms", "endpoint", "device", "token"} {
				if strings.HasPrefix(name, candidate+"_") {
					field = candidate
					break
				}
			}
			_, err := parseConfig(configJSON(t, map[string]interface{}{field: value}))
			requirePrivateError(t, err)
		})
	}
	valid := configJSON(t, nil)
	malformed := [][]byte{
		nil, []byte("null"), []byte("[]"), []byte("{}"), []byte(`{"endpoint":`),
		append(append([]byte{}, valid...), []byte(` {}`)...),
		append(append([]byte{}, valid...), []byte(` garbage`)...),
		[]byte(`{"token":"` + testToken + `","token":"` + testToken + `"}`),
		[]byte(`{"` + testToken + `":true}`),
		[]byte(`{"ENDPOINT":"reinvoke-host:443"}`),
		[]byte(`{"token":"` + testToken + `","device":NaN}`),
		append(valid[:len(valid)-1:len(valid)-1], []byte(",\"device\":\"\xff\"}")...),
		bytes.Repeat([]byte(" "), maxConfigBytes+1),
	}
	for i, data := range malformed {
		_, err := parseConfig(data)
		if err == nil {
			t.Fatalf("malformed config %d was accepted", i)
		}
		requirePrivateError(t, err)
	}
	for _, field := range []string{"endpoint", "certificate_sha256", "token"} {
		var fields map[string]interface{}
		if err := json.Unmarshal(valid, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, field)
		data, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		_, err = parseConfig(data)
		requirePrivateError(t, err)
	}
}

func requirePrivateError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), `{"`) {
		t.Fatal("validation error reflected private config content")
	}
}

func TestLoadConfigPrivateRegularBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "voice.json")
	data := configJSON(t, nil)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0600, 0400} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []os.FileMode{0644, 0640, 0604, 0620, 0601} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfig(path)
		requirePrivateError(t, err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	bounded := append(append([]byte{}, data...), bytes.Repeat([]byte(" "), maxConfigBytes-len(data))...)
	if err := os.WriteFile(path, bounded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(bounded, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig(path)
	requirePrivateError(t, err)
	link := filepath.Join(dir, "symlink")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{link, fifo, dir, filepath.Join(dir, "missing")} {
		_, err := loadConfig(invalid)
		requirePrivateError(t, err)
	}
}

func TestCLIOptions(t *testing.T) {
	opts, err := parseOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.ConfigPath != "/etc/reinvoke-voice/voice.json" || opts.Bundle != "/opt/reinvoke/voice" ||
		opts.Router != "127.0.0.1:9999" || opts.Realm != "default" ||
		opts.RuntimeDir != "/run/reinvoke" || opts.Seconds != 0 || opts.NoPlayback || opts.Once || opts.Fixture != "" {
		t.Fatal("CLI defaults differ from the endpoint contract")
	}
	opts, err = parseOptions([]string{"--fixture", "/tmp/fixture.pcm", "--once", "--no-playback", "--seconds", "86400"}, io.Discard)
	if err != nil || opts.Fixture != "/tmp/fixture.pcm" || !opts.Once || !opts.NoPlayback || opts.Seconds != 86400 {
		t.Fatal("diagnostic options were not accepted")
	}
	for _, args := range [][]string{
		{"--fixture", "relative.pcm", "--once"}, {"--fixture", "/tmp/fixture.pcm"},
		{"--seconds", "-1"}, {"--seconds", "86401"}, {"--seconds", "1.5"},
		{"--router", "localhost"}, {"--router", "localhost:0"}, {"--router", "bad name:99"},
		{"--realm", ""}, {"--realm", strings.Repeat("a", 256)},
		{"--config", ""}, {"--runtime-dir", "relative"}, {"--bundle", "relative"},
		{"--unknown"}, {"positional"},
	} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatal("invalid CLI options accepted")
		}
	}
}

func TestSingletonRetainsLockInode(t *testing.T) {
	dir := t.TempDir()
	first, err := singleton(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	before, err := first.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if second, err := singleton(dir); err == nil {
		second.Close()
		t.Fatal("duplicate supervisor lock succeeded")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := singleton(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	after, err := second.Stat()
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("singleton replaced its lock inode")
	}
}

func TestInvalidStartupConfigReturnsBeforeRetryOrRuntimeCreation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "voice.json")
	runtimeDir := filepath.Join(dir, "runtime")
	if err := os.WriteFile(path, []byte(`{"token":"`+testToken+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var output bytes.Buffer
	err := run(ctx, []string{"--config", path, "--runtime-dir", runtimeDir}, &output)
	requirePrivateError(t, err)
	if ctx.Err() != nil {
		t.Fatal("invalid startup configuration entered the reconnect loop")
	}
	if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
		t.Fatal("invalid config produced runtime side effects")
	}
	if strings.Contains(output.String(), testToken) {
		t.Fatal("startup logged the secret")
	}
}
