// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	defaultListenAddress = "192.168.43.1:8443"
	defaultApplySocket   = "/run/reinvoke/wifi-apply.sock"
	defaultDescriptor    = "/run/reinvoke/provisioning.json"
	defaultLifetime      = 5 * time.Minute
	maxLifetime          = 15 * time.Minute
	maxRequestBytes      = 4096
)

type wifiRequest struct {
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase"`
	Security   string `json:"security"`
	Hidden     bool   `json:"hidden,omitempty"`
}

type applyResponse struct {
	Accepted bool `json:"accepted"`
}

type descriptor struct {
	URL                 string     `json:"url"`
	Token               string     `json:"token"`
	CertificateSHA256   string     `json:"certificate_sha256"`
	ExpiresAfterSeconds int64      `json:"expires_after_seconds"`
	ExpiresUTC          *time.Time `json:"expires_utc,omitempty"`
}

type requestApplier interface {
	Apply(context.Context, wifiRequest) error
}

type unixApplier struct {
	socketPath  string
	timeout     time.Duration
	expectedUID uint32
}

type provisioningHandler struct {
	applier    requestApplier
	token      string
	expiresAt  time.Time
	expiresUTC *time.Time
	applied    chan struct{}
	mu         sync.Mutex
	complete   bool
}

func (a unixApplier) Apply(ctx context.Context, request wifiRequest) error {
	if err := validateUnixSocketPath(a.socketPath, a.expectedUID); err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: a.timeout}
	connection, err := dialer.DialContext(ctx, "unix", a.socketPath)
	if err != nil {
		return fmt.Errorf("connect apply socket: %w", err)
	}
	defer connection.Close()
	if err := verifyUnixPeer(connection, a.expectedUID); err != nil {
		return err
	}

	deadline := time.Now().Add(a.timeout)
	if err := connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set apply socket deadline: %w", err)
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return fmt.Errorf("send apply request: %w", err)
	}

	var response applyResponse
	decoder := json.NewDecoder(io.LimitReader(connection, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("read apply response: %w", err)
	}
	if !response.Accepted {
		return errors.New("apply request was rejected")
	}
	return nil
}

func fileOwnerUID(info os.FileInfo) (uint32, error) {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("filesystem does not expose Unix ownership")
	}
	return status.Uid, nil
}

func validateOwnedDirectory(
	path string,
	expectedUID uint32,
	rootOnly bool,
) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path must be a non-symlink directory")
	}
	ownerUID, err := fileOwnerUID(info)
	if err != nil {
		return err
	}
	if ownerUID != expectedUID {
		return fmt.Errorf(
			"directory owner UID %d does not match required UID %d",
			ownerUID,
			expectedUID,
		)
	}
	if rootOnly {
		if info.Mode().Perm()&0077 != 0 {
			return errors.New("directory must not grant group or other access")
		}
	} else if info.Mode().Perm()&0022 != 0 {
		return errors.New("directory must not be group or world writable")
	}
	return nil
}

func validateUnixSocketPath(path string, expectedUID uint32) error {
	if !filepath.IsAbs(path) {
		return errors.New("apply socket path must be absolute")
	}
	if err := validateOwnedDirectory(
		filepath.Dir(filepath.Clean(path)),
		expectedUID,
		false,
	); err != nil {
		return fmt.Errorf("validate apply socket directory: %w", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect apply socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 ||
		info.Mode()&os.ModeSymlink != 0 {
		return errors.New("apply path must be a non-symlink Unix socket")
	}
	ownerUID, err := fileOwnerUID(info)
	if err != nil {
		return err
	}
	if ownerUID != expectedUID {
		return fmt.Errorf(
			"apply socket owner UID %d does not match required UID %d",
			ownerUID,
			expectedUID,
		)
	}
	if info.Mode().Perm()&0022 != 0 {
		return errors.New("apply socket must not be group or world writable")
	}
	return nil
}

func verifyUnixPeer(connection net.Conn, expectedUID uint32) error {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return errors.New("apply connection is not a Unix socket")
	}
	rawConnection, err := unixConnection.SyscallConn()
	if err != nil {
		return fmt.Errorf("access apply socket descriptor: %w", err)
	}

	var (
		credentials   *syscall.Ucred
		credentialErr error
	)
	if err := rawConnection.Control(func(fileDescriptor uintptr) {
		credentials, credentialErr = syscall.GetsockoptUcred(
			int(fileDescriptor),
			syscall.SOL_SOCKET,
			syscall.SO_PEERCRED,
		)
	}); err != nil {
		return fmt.Errorf("inspect apply peer: %w", err)
	}
	if credentialErr != nil {
		return fmt.Errorf("read apply peer credentials: %w", credentialErr)
	}
	if credentials == nil || credentials.Uid != expectedUID {
		return errors.New("apply peer UID is not trusted")
	}
	return nil
}

func (h *provisioningHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if request.URL.Path != "/v1/status" && request.URL.Path != "/v1/wifi" {
		http.NotFound(writer, request)
		return
	}
	if !constantTimeTokenMatch(
		request.Header.Get("Authorization"),
		h.token,
	) {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return
	}

	switch request.URL.Path {
	case "/v1/status":
		h.handleStatus(writer, request)
	case "/v1/wifi":
		h.handleWiFi(writer, request)
	}
}

func (h *provisioningHandler) handleStatus(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	h.mu.Lock()
	ready := !h.complete
	h.mu.Unlock()
	remaining := time.Until(h.expiresAt)
	if remaining < 0 {
		remaining = 0
	}
	response := map[string]interface{}{
		"ready":                 ready,
		"expires_after_seconds": int64(remaining.Seconds()),
	}
	if h.expiresUTC != nil {
		response["expires_utc"] = h.expiresUTC
	}
	writeJSON(writer, http.StatusOK, response)
}

func (h *provisioningHandler) handleWiFi(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(
			writer,
			http.StatusUnsupportedMediaType,
			"content type must be application/json",
		)
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	var wifi wifiRequest
	if err := decoder.Decode(&wifi); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid request")
		return
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid request")
		return
	}
	if err := validateWiFiRequest(wifi); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.complete {
		writeError(writer, http.StatusConflict, "provisioning already completed")
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	if err := h.applier.Apply(ctx, wifi); err != nil {
		log.Printf("provisioning adapter rejected the request")
		writeError(
			writer,
			http.StatusBadGateway,
			"network adapter rejected the request",
		)
		return
	}

	h.complete = true
	writeJSON(writer, http.StatusAccepted, map[string]bool{"accepted": true})
	select {
	case h.applied <- struct{}{}:
	default:
	}
}

func validateWiFiRequest(request wifiRequest) error {
	ssidLength := len([]byte(request.SSID))
	if ssidLength < 1 || ssidLength > 32 || !utf8.ValidString(request.SSID) {
		return errors.New("ssid must contain 1 through 32 UTF-8 bytes")
	}
	if containsUnsafeControl(request.SSID) {
		return errors.New("ssid contains an unsupported control character")
	}
	if request.Security != "wpa2-psk" {
		return errors.New("security must be wpa2-psk")
	}

	passphraseLength := len([]byte(request.Passphrase))
	if passphraseLength < 8 || passphraseLength > 63 {
		return errors.New("passphrase must contain 8 through 63 bytes")
	}
	if containsUnsafeControl(request.Passphrase) {
		return errors.New("passphrase contains an unsupported control character")
	}
	return nil
}

func containsUnsafeControl(value string) bool {
	return strings.ContainsAny(value, "\x00\r\n")
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func constantTimeTokenMatch(header string, token string) bool {
	expected := "Bearer " + token
	if len(header) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(expected)) == 1
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func writeJSON(writer http.ResponseWriter, status int, value interface{}) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		log.Printf("response write failed: %v", err)
	}
}

func generateToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate bearer token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func generateCertificate(
	ipAddress net.IP,
	now time.Time,
	expiresAt time.Time,
) (tls.Certificate, string, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf(
			"generate TLS private key: %w",
			err,
		)
	}

	serialBytes := make([]byte, 16)
	if _, err := rand.Read(serialBytes); err != nil {
		return tls.Certificate{}, "", fmt.Errorf(
			"generate certificate serial: %w",
			err,
		)
	}
	serial := new(big.Int).SetBytes(serialBytes)

	notBefore := now.UTC().Add(-time.Minute)
	notAfter := expiresAt.UTC()
	if !wallClockIsSane(now) {
		notBefore = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		notAfter = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "reInvoke ephemeral provisioning",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{ipAddress},
	}

	certificateDER, err := x509.CreateCertificate(
		rand.Reader,
		&template,
		&template,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf(
			"create TLS certificate: %w",
			err,
		)
	}
	privateKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf(
			"marshal TLS private key: %w",
			err,
		)
	}

	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificateDER,
	})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyDER,
	})
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf(
			"load generated TLS key pair: %w",
			err,
		)
	}

	fingerprint := sha256.Sum256(certificateDER)
	return certificate, hex.EncodeToString(fingerprint[:]), nil
}

func wallClockIsSane(now time.Time) bool {
	return now.Year() >= 2020 && now.Year() <= 2100
}

func ensurePrivateDirectory(path string, expectedUID uint32) error {
	directory := filepath.Dir(path)
	_, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return fmt.Errorf("create descriptor directory: %w", err)
		}
		err = nil
	}
	if err != nil {
		return fmt.Errorf("inspect descriptor directory: %w", err)
	}
	if err := validateOwnedDirectory(directory, expectedUID, true); err != nil {
		return fmt.Errorf("validate descriptor directory: %w", err)
	}
	return nil
}

func writeDescriptor(
	path string,
	value descriptor,
	expectedUID uint32,
) error {
	if err := ensurePrivateDirectory(path, expectedUID); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create descriptor: %w", err)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		file.Close()
		os.Remove(path)
		return fmt.Errorf("write descriptor: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(path)
		return fmt.Errorf("sync descriptor: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return fmt.Errorf("close descriptor: %w", err)
	}
	return nil
}

func parseListenAddress(address string) (net.IP, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse listen address: %w", err)
	}
	ipAddress := net.ParseIP(host)
	if ipAddress == nil || ipAddress.IsUnspecified() {
		return nil, errors.New("listen address must contain an explicit IP")
	}
	return ipAddress, nil
}

func run() error {
	var (
		listenAddress  string
		applySocket    string
		descriptorPath string
		lifetime       time.Duration
	)
	flag.StringVar(
		&listenAddress,
		"listen",
		defaultListenAddress,
		"explicit IP and port for the provisioning listener",
	)
	flag.StringVar(
		&applySocket,
		"apply-socket",
		defaultApplySocket,
		"Unix socket for the network adapter",
	)
	flag.StringVar(
		&descriptorPath,
		"descriptor",
		defaultDescriptor,
		"root-only ephemeral connection descriptor",
	)
	flag.DurationVar(
		&lifetime,
		"lifetime",
		defaultLifetime,
		"maximum provisioning window",
	)
	flag.Parse()

	if os.Geteuid() != 0 {
		return errors.New("provisioning daemon must run as root")
	}
	if lifetime < 30*time.Second || lifetime > maxLifetime {
		return errors.New("lifetime must be from 30s through 15m")
	}
	ipAddress, err := parseListenAddress(listenAddress)
	if err != nil {
		return err
	}
	if applySocket == "" || descriptorPath == "" {
		return errors.New("apply socket and descriptor paths are required")
	}

	now := time.Now()
	expiresAt := now.Add(lifetime)
	var expiresUTC *time.Time
	if wallClockIsSane(now) {
		value := expiresAt.UTC()
		expiresUTC = &value
	}
	token, err := generateToken()
	if err != nil {
		return err
	}
	certificate, fingerprint, err := generateCertificate(
		ipAddress,
		now,
		expiresAt,
	)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	actualAddress := listener.Addr().String()
	connectionDescriptor := descriptor{
		URL:                 "https://" + actualAddress,
		Token:               token,
		CertificateSHA256:   fingerprint,
		ExpiresAfterSeconds: int64(lifetime.Seconds()),
		ExpiresUTC:          expiresUTC,
	}
	if err := writeDescriptor(
		descriptorPath,
		connectionDescriptor,
		0,
	); err != nil {
		return err
	}
	defer os.Remove(descriptorPath)

	applied := make(chan struct{}, 1)
	handler := &provisioningHandler{
		applier: unixApplier{
			socketPath:  applySocket,
			timeout:     3 * time.Second,
			expectedUID: 0,
		},
		token:      token,
		expiresAt:  expiresAt,
		expiresUTC: expiresUTC,
		applied:    applied,
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    8192,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS13,
		},
	}

	tlsListener := tls.NewListener(listener, server.TLSConfig)
	serverErrors := make(chan error, 1)
	go func() {
		err := server.Serve(tlsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
		close(serverErrors)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	log.Printf(
		"provisioning listener=%s lifetime=%s descriptor=%s",
		actualAddress,
		lifetime,
		descriptorPath,
	)

	timer := time.NewTimer(lifetime)
	defer timer.Stop()
	select {
	case <-applied:
		log.Printf("provisioning request accepted; closing window")
	case <-timer.C:
		log.Printf("provisioning window expired")
	case received := <-signals:
		log.Printf("received %s; closing provisioning window", received)
	case err := <-serverErrors:
		if err != nil {
			return fmt.Errorf("serve provisioning API: %w", err)
		}
	}

	shutdownContext, cancel := context.WithTimeout(
		context.Background(),
		3*time.Second,
	)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown provisioning API: %w", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}
