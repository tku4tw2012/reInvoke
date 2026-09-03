// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	defaultSocketPath     = "/run/reinvoke/wifi-apply.sock"
	defaultConfigPath     = "/run/reinvoke/wpa_supplicant.conf"
	defaultControlPath    = "/run/reinvoke/wpa_supplicant"
	defaultSupplicant     = "/bin/wpa_supplicant"
	defaultWPAClient      = "/bin/wpa_cli"
	defaultInterface      = "mlan0"
	defaultDriver         = "nl80211,wext"
	defaultLifetime       = 5 * time.Minute
	defaultConnectTimeout = 20 * time.Second
	maxRequestBytes       = 4096
	tmpfsMagic            = 0x01021994
	ramfsMagic            = 0x858458f6
)

var interfacePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)

type wifiRequest struct {
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase"`
	Security   string `json:"security"`
	Hidden     bool   `json:"hidden,omitempty"`
}

type applyResponse struct {
	Accepted bool `json:"accepted"`
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

type wpaManager struct {
	runner         commandRunner
	writeConfig    func(string, []byte) error
	supplicantPath string
	clientPath     string
	configPath     string
	controlPath    string
	interfaceName  string
	driverName     string
	connectTimeout time.Duration
	expectedUID    uint32
}

func (execRunner) Run(
	ctx context.Context,
	name string,
	arguments ...string,
) ([]byte, error) {
	return exec.CommandContext(ctx, name, arguments...).CombinedOutput()
}

func (m wpaManager) Apply(
	ctx context.Context,
	request wifiRequest,
) error {
	applyContext, cancelApply := context.WithTimeout(ctx, m.connectTimeout)
	defer cancelApply()

	if err := m.prepareSupplicant(applyContext); err != nil {
		return err
	}
	config := renderWPAConfig(request, m.controlPath)
	if err := m.writeConfig(m.configPath, config); err != nil {
		return err
	}

	_, err := m.runner.Run(
		applyContext,
		m.supplicantPath,
		"-B",
		"-D",
		m.driverName,
		"-i",
		m.interfaceName,
		"-c",
		m.configPath,
		"-C",
		m.controlPath,
	)
	if err != nil {
		os.Remove(m.configPath)
		return errors.New("wpa_supplicant failed to start")
	}

	for {
		output, commandErr := m.runner.Run(
			applyContext,
			m.clientPath,
			"-p",
			m.controlPath,
			"-i",
			m.interfaceName,
			"status",
		)
		if commandErr == nil && wpaState(output) == "COMPLETED" {
			return nil
		}
		select {
		case <-applyContext.Done():
			m.stopSupplicant()
			os.Remove(m.configPath)
			return applyContext.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (m wpaManager) prepareSupplicant(ctx context.Context) error {
	socketPath := filepath.Join(m.controlPath, m.interfaceName)
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing supplicant: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 ||
		info.Mode()&os.ModeSymlink != 0 {
		return errors.New("supplicant control path is not a Unix socket")
	}
	ownerUID, err := fileOwnerUID(info)
	if err != nil {
		return err
	}
	if ownerUID != m.expectedUID || info.Mode().Perm()&0002 != 0 {
		return errors.New("supplicant control socket is not root-controlled")
	}

	pingContext, cancelPing := context.WithTimeout(
		ctx,
		3*time.Second,
	)
	output, pingErr := m.runner.Run(
		pingContext,
		m.clientPath,
		"-p",
		m.controlPath,
		"-i",
		m.interfaceName,
		"ping",
	)
	cancelPing()
	if pingErr != nil || !strings.Contains(string(output), "PONG") {
		if err := os.Remove(socketPath); err != nil {
			return fmt.Errorf("remove stale supplicant socket: %w", err)
		}
		return nil
	}

	terminateContext, cancelTerminate := context.WithTimeout(
		ctx,
		3*time.Second,
	)
	_, terminateErr := m.runner.Run(
		terminateContext,
		m.clientPath,
		"-p",
		m.controlPath,
		"-i",
		m.interfaceName,
		"terminate",
	)
	cancelTerminate()
	if terminateErr != nil {
		return errors.New("existing supplicant did not accept termination")
	}
	for attempt := 0; attempt < 30; attempt++ {
		if _, err := os.Lstat(socketPath); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("existing supplicant did not release its control socket")
}

func (m wpaManager) stopSupplicant() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = m.runner.Run(
		ctx,
		m.clientPath,
		"-p",
		m.controlPath,
		"-i",
		m.interfaceName,
		"terminate",
	)
}

func wpaState(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && key == "wpa_state" {
			return value
		}
	}
	return ""
}

func validateWiFiRequest(request wifiRequest) error {
	ssidLength := len([]byte(request.SSID))
	if ssidLength < 1 || ssidLength > 32 || !utf8.ValidString(request.SSID) {
		return errors.New("invalid SSID")
	}
	if strings.ContainsAny(request.SSID, "\x00\r\n") {
		return errors.New("invalid SSID")
	}
	if request.Security != "wpa2-psk" {
		return errors.New("unsupported security")
	}
	passphraseLength := len([]byte(request.Passphrase))
	if passphraseLength < 8 || passphraseLength > 63 {
		return errors.New("invalid passphrase")
	}
	if strings.ContainsAny(request.Passphrase, "\x00\r\n") {
		return errors.New("invalid passphrase")
	}
	return nil
}

func deriveWPA2PSK(passphrase string, ssid string) []byte {
	return pbkdf2SHA1([]byte(passphrase), []byte(ssid), 4096, 32)
}

func pbkdf2SHA1(
	password []byte,
	salt []byte,
	iterations int,
	keyLength int,
) []byte {
	hashLength := sha1.Size
	blockCount := (keyLength + hashLength - 1) / hashLength
	output := make([]byte, 0, blockCount*hashLength)
	counter := make([]byte, 4)

	for block := 1; block <= blockCount; block++ {
		binary.BigEndian.PutUint32(counter, uint32(block))
		mac := hmac.New(sha1.New, password)
		mac.Write(salt)
		mac.Write(counter)
		current := mac.Sum(nil)
		combined := append([]byte(nil), current...)

		for iteration := 1; iteration < iterations; iteration++ {
			mac = hmac.New(sha1.New, password)
			mac.Write(current)
			current = mac.Sum(nil)
			for index := range combined {
				combined[index] ^= current[index]
			}
		}
		output = append(output, combined...)
	}
	return output[:keyLength]
}

func renderWPAConfig(request wifiRequest, controlPath string) []byte {
	var builder strings.Builder
	builder.WriteString("ctrl_interface=")
	builder.WriteString(controlPath)
	builder.WriteString("\nupdate_config=0\nnetwork={\n\tssid=")
	builder.WriteString(hex.EncodeToString([]byte(request.SSID)))
	builder.WriteString("\n\tpsk=")
	builder.WriteString(hex.EncodeToString(
		deriveWPA2PSK(request.Passphrase, request.SSID),
	))
	builder.WriteString("\n\tkey_mgmt=WPA-PSK\n")
	if request.Hidden {
		builder.WriteString("\tscan_ssid=1\n")
	}
	builder.WriteString("}\n")
	return []byte(builder.String())
}

func fileOwnerUID(info os.FileInfo) (uint32, error) {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("filesystem does not expose Unix ownership")
	}
	return status.Uid, nil
}

func validateRootDirectory(path string, requireRAM bool) error {
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
	if ownerUID != 0 {
		return errors.New("directory must be owned by root")
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("directory must not grant group or other access")
	}
	if requireRAM {
		var filesystem syscall.Statfs_t
		if err := syscall.Statfs(path, &filesystem); err != nil {
			return fmt.Errorf("inspect directory filesystem: %w", err)
		}
		filesystemType := uint64(filesystem.Type) & 0xffffffff
		if filesystemType != tmpfsMagic && filesystemType != ramfsMagic {
			return errors.New("runtime directory must reside on tmpfs or ramfs")
		}
	}
	return nil
}

func validateRootExecutable(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("executable path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("executable must be a non-symlink regular file")
	}
	ownerUID, err := fileOwnerUID(info)
	if err != nil {
		return err
	}
	if ownerUID != 0 || info.Mode().Perm()&0022 != 0 {
		return errors.New("executable is not root-controlled")
	}
	if info.Mode().Perm()&0111 == 0 {
		return errors.New("executable is not executable")
	}
	return nil
}

func writePrivateFile(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := validateRootDirectory(directory, true); err != nil {
		return fmt.Errorf("validate config directory: %w", err)
	}

	file, err := os.CreateTemp(directory, ".wpa-config-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)

	if err := file.Chmod(0600); err != nil {
		file.Close()
		return fmt.Errorf("restrict temporary config: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("activate config: %w", err)
	}
	return nil
}

func verifyRootPeer(connection *net.UnixConn) error {
	rawConnection, err := connection.SyscallConn()
	if err != nil {
		return fmt.Errorf("access peer descriptor: %w", err)
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
		return fmt.Errorf("inspect peer: %w", err)
	}
	if credentialErr != nil {
		return fmt.Errorf("read peer credentials: %w", credentialErr)
	}
	if credentials == nil || credentials.Uid != 0 {
		return errors.New("peer is not root")
	}
	return nil
}

func decodeRequest(reader io.Reader) (wifiRequest, error) {
	var request wifiRequest
	decoder := json.NewDecoder(io.LimitReader(reader, maxRequestBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return wifiRequest{}, errors.New("invalid request")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return wifiRequest{}, errors.New("invalid request")
	}
	if err := validateWiFiRequest(request); err != nil {
		return wifiRequest{}, err
	}
	return request, nil
}

func handleConnection(
	ctx context.Context,
	connection *net.UnixConn,
	manager *wpaManager,
) bool {
	defer connection.Close()
	if err := connection.SetReadDeadline(
		time.Now().Add(10 * time.Second),
	); err != nil {
		return false
	}
	if err := verifyRootPeer(connection); err != nil {
		return false
	}
	request, err := decodeRequest(connection)
	if err != nil {
		_ = json.NewEncoder(connection).Encode(applyResponse{Accepted: false})
		return false
	}
	if err := connection.SetReadDeadline(time.Time{}); err != nil {
		return false
	}
	if err := manager.Apply(ctx, request); err != nil {
		log.Printf("network apply failed")
		_ = json.NewEncoder(connection).Encode(applyResponse{Accepted: false})
		return false
	}
	if err := json.NewEncoder(connection).Encode(
		applyResponse{Accepted: true},
	); err != nil {
		log.Printf("network applied but acknowledgement failed")
		return false
	}
	return true
}

func run() error {
	var (
		socketPath     string
		configPath     string
		controlPath    string
		supplicantPath string
		clientPath     string
		interfaceName  string
		driverName     string
		lifetime       time.Duration
		connectTimeout time.Duration
	)
	flag.StringVar(&socketPath, "socket", defaultSocketPath, "apply Unix socket")
	flag.StringVar(&configPath, "config", defaultConfigPath, "RAM WPA config")
	flag.StringVar(
		&controlPath,
		"control-dir",
		defaultControlPath,
		"wpa_supplicant control directory",
	)
	flag.StringVar(
		&supplicantPath,
		"wpa-supplicant",
		defaultSupplicant,
		"trusted wpa_supplicant binary",
	)
	flag.StringVar(
		&clientPath,
		"wpa-cli",
		defaultWPAClient,
		"trusted wpa_cli binary",
	)
	flag.StringVar(
		&interfaceName,
		"interface",
		defaultInterface,
		"station network interface",
	)
	flag.StringVar(&driverName, "driver", defaultDriver, "supplicant driver list")
	flag.DurationVar(&lifetime, "lifetime", defaultLifetime, "apply window")
	flag.DurationVar(
		&connectTimeout,
		"connect-timeout",
		defaultConnectTimeout,
		"station association timeout",
	)
	flag.Parse()

	if os.Geteuid() != 0 {
		return errors.New("Wi-Fi apply daemon must run as root")
	}
	if lifetime < 30*time.Second || lifetime > 15*time.Minute {
		return errors.New("lifetime must be from 30s through 15m")
	}
	if connectTimeout < time.Second || connectTimeout > 2*time.Minute {
		return errors.New("connect timeout must be from 1s through 2m")
	}
	if !interfacePattern.MatchString(interfaceName) {
		return errors.New("invalid interface name")
	}
	if driverName != "nl80211,wext" &&
		driverName != "nl80211" &&
		driverName != "wext" {
		return errors.New("unsupported supplicant driver")
	}

	runtimeDirectory := filepath.Dir(socketPath)
	if filepath.Dir(configPath) != runtimeDirectory ||
		filepath.Dir(controlPath) != runtimeDirectory {
		return errors.New("socket, config, and control paths must share a directory")
	}
	if err := validateRootDirectory(runtimeDirectory, true); err != nil {
		return err
	}
	if err := validateRootExecutable(supplicantPath); err != nil {
		return fmt.Errorf("validate wpa_supplicant: %w", err)
	}
	if err := validateRootExecutable(clientPath); err != nil {
		return fmt.Errorf("validate wpa_cli: %w", err)
	}
	if _, err := os.Lstat(socketPath); err == nil {
		return errors.New("apply socket already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect apply socket: %w", err)
	}
	if err := os.MkdirAll(controlPath, 0700); err != nil {
		return fmt.Errorf("create supplicant control directory: %w", err)
	}
	if err := validateRootDirectory(controlPath, true); err != nil {
		return fmt.Errorf("validate supplicant control directory: %w", err)
	}

	listener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: socketPath, Net: "unix"},
	)
	if err != nil {
		return fmt.Errorf("listen on apply socket: %w", err)
	}
	listener.SetUnlinkOnClose(true)
	defer listener.Close()
	if err := os.Chmod(socketPath, 0600); err != nil {
		return fmt.Errorf("restrict apply socket: %w", err)
	}

	manager := &wpaManager{
		runner:         execRunner{},
		writeConfig:    writePrivateFile,
		supplicantPath: supplicantPath,
		clientPath:     clientPath,
		configPath:     configPath,
		controlPath:    controlPath,
		interfaceName:  interfaceName,
		driverName:     driverName,
		connectTimeout: connectTimeout,
		expectedUID:    0,
	}

	signalContext, stopSignals := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stopSignals()
	windowContext, cancelWindow := context.WithTimeout(
		signalContext,
		lifetime,
	)
	defer cancelWindow()
	go func() {
		<-windowContext.Done()
		listener.Close()
	}()

	log.Printf(
		"Wi-Fi apply socket=%s interface=%s lifetime=%s",
		socketPath,
		interfaceName,
		lifetime,
	)
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			if windowContext.Err() != nil {
				log.Printf("Wi-Fi apply window closed")
				return nil
			}
			return fmt.Errorf("accept apply connection: %w", err)
		}
		if handleConnection(windowContext, connection, manager) {
			log.Printf("Wi-Fi configuration applied")
			return nil
		}
	}
}

func main() {
	if err := run(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}
