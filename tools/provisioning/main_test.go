// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type recordingApplier struct {
	request wifiRequest
	err     error
}

type blockingApplier struct {
	started chan struct{}
	release chan struct{}
}

func (a *blockingApplier) Apply(ctx context.Context, _ wifiRequest) error {
	close(a.started)
	select {
	case <-a.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *recordingApplier) Apply(
	_ context.Context,
	request wifiRequest,
) error {
	a.request = request
	return a.err
}

func TestValidateWiFiRequest(t *testing.T) {
	t.Parallel()

	valid := wifiRequest{
		SSID:       "test-network",
		Passphrase: "correct horse battery staple",
		Security:   "wpa2-psk",
	}
	tests := []struct {
		name    string
		request wifiRequest
		valid   bool
	}{
		{name: "valid", request: valid, valid: true},
		{
			name: "empty ssid",
			request: wifiRequest{
				Passphrase: valid.Passphrase,
				Security:   valid.Security,
			},
		},
		{
			name: "long ssid",
			request: wifiRequest{
				SSID:       "123456789012345678901234567890123",
				Passphrase: valid.Passphrase,
				Security:   valid.Security,
			},
		},
		{
			name: "short passphrase",
			request: wifiRequest{
				SSID:       valid.SSID,
				Passphrase: "short",
				Security:   valid.Security,
			},
		},
		{
			name: "newline passphrase",
			request: wifiRequest{
				SSID:       valid.SSID,
				Passphrase: "password\n",
				Security:   valid.Security,
			},
		},
		{
			name: "unsupported security",
			request: wifiRequest{
				SSID:       valid.SSID,
				Passphrase: valid.Passphrase,
				Security:   "open",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateWiFiRequest(test.request)
			if test.valid && err != nil {
				t.Fatalf("valid request failed: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
}

func TestProvisioningHandler(t *testing.T) {
	t.Parallel()

	applier := &recordingApplier{}
	handler := &provisioningHandler{
		applier:      applier,
		token:        "test-token",
		expiresAt:    time.Now().Add(time.Minute),
		applied:      make(chan struct{}, 1),
		applyTimeout: time.Second,
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	requestBody := []byte(
		`{"ssid":"test-network","passphrase":"test-password","security":"wpa2-psk"}`,
	)
	unauthorized, err := server.Client().Post(
		server.URL+"/v1/wifi",
		"application/json",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("unauthorized request failed: %v", err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}

	request, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/v1/wifi",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("authorized request failed: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("authorized status = %d", response.StatusCode)
	}
	if applier.request.SSID != "test-network" ||
		applier.request.Passphrase != "test-password" {
		t.Fatalf("unexpected apply request: %#v", applier.request)
	}

	secondRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/v1/wifi",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("create second request: %v", err)
	}
	secondRequest.Header.Set("Authorization", "Bearer test-token")
	secondRequest.Header.Set("Content-Type", "application/json")
	second, err := server.Client().Do(secondRequest)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("second status = %d", second.StatusCode)
	}
}

func TestProvisioningHandlerDoesNotBlockStatusDuringApply(t *testing.T) {
	t.Parallel()

	applier := &blockingApplier{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	handler := &provisioningHandler{
		applier:      applier,
		token:        "test-token",
		expiresAt:    time.Now().Add(time.Minute),
		applied:      make(chan struct{}, 1),
		applyTimeout: time.Second,
	}
	applyRequest := httptest.NewRequest(
		http.MethodPost,
		"https://127.0.0.1/v1/wifi",
		bytes.NewBufferString(
			`{"ssid":"test","passphrase":"test-password","security":"wpa2-psk"}`,
		),
	)
	applyRequest.Header.Set("Authorization", "Bearer test-token")
	applyRequest.Header.Set("Content-Type", "application/json")
	applyResponse := httptest.NewRecorder()
	applyDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(applyResponse, applyRequest)
		close(applyDone)
	}()
	<-applier.started

	statusRequest := httptest.NewRequest(
		http.MethodGet,
		"https://127.0.0.1/v1/status",
		nil,
	)
	statusRequest.Header.Set("Authorization", "Bearer test-token")
	statusResponse := httptest.NewRecorder()
	statusDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(statusResponse, statusRequest)
		close(statusDone)
	}()
	select {
	case <-statusDone:
	case <-time.After(time.Second):
		t.Fatal("status request blocked during apply")
	}
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status = %d", statusResponse.Code)
	}
	var status struct {
		Ready bool `json:"ready"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Ready {
		t.Fatal("status reported ready while apply was in progress")
	}

	secondRequest := httptest.NewRequest(
		http.MethodPost,
		"https://127.0.0.1/v1/wifi",
		bytes.NewBufferString(
			`{"ssid":"test","passphrase":"test-password","security":"wpa2-psk"}`,
		),
	)
	secondRequest.Header.Set("Authorization", "Bearer test-token")
	secondRequest.Header.Set("Content-Type", "application/json")
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("second status = %d", secondResponse.Code)
	}

	close(applier.release)
	select {
	case <-applyDone:
	case <-time.After(time.Second):
		t.Fatal("apply request did not complete")
	}
	if applyResponse.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d", applyResponse.Code)
	}
}

func TestRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	handler := &provisioningHandler{
		applier:      &recordingApplier{},
		token:        "test-token",
		expiresAt:    time.Now().Add(time.Minute),
		applied:      make(chan struct{}, 1),
		applyTimeout: time.Second,
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"https://127.0.0.1/v1/wifi",
		bytes.NewBufferString(
			`{"ssid":"test","passphrase":"test-password","security":"wpa2-psk","extra":true}`,
		),
	)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestGenerateCertificate(t *testing.T) {
	t.Parallel()

	ipAddress := net.ParseIP("192.168.43.1")
	now := time.Now()
	expires := time.Now().UTC().Add(time.Minute)
	certificate, fingerprint, err := generateCertificate(
		ipAddress,
		now,
		expires,
	)
	if err != nil {
		t.Fatalf("generate certificate: %v", err)
	}
	if len(fingerprint) != 64 {
		t.Fatalf("fingerprint length = %d", len(fingerprint))
	}

	parsed, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	if len(parsed.IPAddresses) != 1 ||
		!parsed.IPAddresses[0].Equal(ipAddress) {
		t.Fatalf("unexpected certificate IPs: %v", parsed.IPAddresses)
	}
}

func TestGenerateCertificateWithUnsetClock(t *testing.T) {
	t.Parallel()

	ipAddress := net.ParseIP("192.168.43.1")
	now := time.Unix(3600, 0)
	certificate, _, err := generateCertificate(
		ipAddress,
		now,
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("generate certificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	clientTime := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	if clientTime.Before(parsed.NotBefore) || clientTime.After(parsed.NotAfter) {
		t.Fatalf(
			"certificate range %s through %s excludes client time",
			parsed.NotBefore,
			parsed.NotAfter,
		)
	}
}

func TestUnixApplier(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatalf("restrict Unix socket directory: %v", err)
	}
	socketPath := filepath.Join(directory, "wifi-apply.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0600); err != nil {
		t.Fatalf("restrict Unix socket: %v", err)
	}

	received := make(chan wifiRequest, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()

		var request wifiRequest
		decoder := json.NewDecoder(connection)
		if decodeErr := decoder.Decode(&request); decodeErr != nil {
			return
		}
		var extra interface{}
		if decodeErr := decoder.Decode(&extra); !errors.Is(decodeErr, io.EOF) {
			return
		}
		received <- request
		_ = json.NewEncoder(connection).Encode(applyResponse{Accepted: true})
	}()

	request := wifiRequest{
		SSID:       "test-network",
		Passphrase: "test-password",
		Security:   "wpa2-psk",
	}
	applier := unixApplier{
		socketPath:  socketPath,
		timeout:     time.Second,
		expectedUID: uint32(os.Geteuid()),
	}
	if err := applier.Apply(context.Background(), request); err != nil {
		t.Fatalf("apply request: %v", err)
	}

	select {
	case actual := <-received:
		if actual != request {
			t.Fatalf("request = %#v", actual)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter did not receive request")
	}
}

func TestUnixApplierHonorsCancellationDuringResponseRead(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatalf("restrict Unix socket directory: %v", err)
	}
	socketPath := filepath.Join(directory, "wifi-apply.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0600); err != nil {
		t.Fatalf("restrict Unix socket: %v", err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	applier := unixApplier{
		socketPath:  socketPath,
		timeout:     10 * time.Second,
		expectedUID: uint32(os.Geteuid()),
	}
	applyDone := make(chan error, 1)
	go func() {
		applyDone <- applier.Apply(ctx, wifiRequest{
			SSID:       "test-network",
			Passphrase: "test-password",
			Security:   "wpa2-psk",
		})
	}()

	var connection net.Conn
	select {
	case connection = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("adapter did not accept connection")
	}
	defer connection.Close()
	cancel()
	select {
	case err := <-applyDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("apply error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("apply did not stop after cancellation")
	}
}
func TestUnixApplierRejectsWritableDirectory(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "writable")
	if err := os.Mkdir(directory, 0777); err != nil {
		t.Fatalf("create writable directory: %v", err)
	}
	if err := os.Chmod(directory, 0777); err != nil {
		t.Fatalf("make directory writable: %v", err)
	}
	socketPath := filepath.Join(directory, "wifi-apply.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	defer listener.Close()

	applier := unixApplier{
		socketPath:  socketPath,
		timeout:     time.Second,
		expectedUID: uint32(os.Geteuid()),
	}
	err = applier.Apply(context.Background(), wifiRequest{
		SSID:       "test-network",
		Passphrase: "test-password",
		Security:   "wpa2-psk",
	})
	if err == nil {
		t.Fatal("writable adapter directory was accepted")
	}
}

func TestWriteDescriptorPermissions(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatalf("create private directory: %v", err)
	}
	path := filepath.Join(directory, "provisioning.json")
	value := descriptor{
		URL:                 "https://192.168.43.1:8443",
		Token:               "secret-token",
		CertificateSHA256:   "fingerprint",
		ExpiresAfterSeconds: 60,
	}
	if err := writeDescriptor(path, value, uint32(os.Geteuid())); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat descriptor: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("descriptor mode = %#o", info.Mode().Perm())
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open descriptor: %v", err)
	}
	defer file.Close()
	var decoded descriptor
	if err := json.NewDecoder(file).Decode(&decoded); err != nil {
		t.Fatalf("decode descriptor: %v", err)
	}
	if decoded.Token != value.Token {
		t.Fatal("descriptor token mismatch")
	}
}
