// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func testNetworkPlan(t *testing.T) networkPlan {
	t.Helper()
	plan, err := parseNetworkPlan(
		"192.168.43.1/24",
		"192.168.43.100",
		"192.168.43.150",
	)
	if err != nil {
		t.Fatalf("parse network plan: %v", err)
	}
	return plan
}

func TestDecodeOpenRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    time.Duration
		valid   bool
	}{
		{
			name:    "default lifetime",
			content: `{"operation":"OPEN"}`,
			want:    5 * time.Minute,
			valid:   true,
		},
		{
			name:    "maximum lifetime",
			content: `{"operation":"OPEN","duration_seconds":900}`,
			want:    15 * time.Minute,
			valid:   true,
		},
		{
			name:    "wrong operation",
			content: `{"operation":"CLOSE"}`,
		},
		{
			name:    "too short",
			content: `{"operation":"OPEN","duration_seconds":29}`,
		},
		{
			name:    "too long",
			content: `{"operation":"OPEN","duration_seconds":901}`,
		},
		{
			name:    "unknown field",
			content: `{"operation":"OPEN","credential":"not-allowed"}`,
		},
		{
			name:    "second value",
			content: `{"operation":"OPEN"} {}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, err := decodeOpenRequest(
				strings.NewReader(test.content + "\n"),
			)
			if test.valid && err != nil {
				t.Fatalf("valid request: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid request was accepted")
			}
			if test.valid && actual != test.want {
				t.Fatalf("duration = %s, want %s", actual, test.want)
			}
		})
	}
}

func TestDecodeOpenRequestRespondsWithoutClientEOF(t *testing.T) {
	t.Parallel()
	socketPath := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: socketPath, Net: "unix"},
	)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.AcceptUnix()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer connection.Close()
		duration, decodeErr := decodeOpenRequest(connection)
		if decodeErr != nil {
			serverResult <- decodeErr
			return
		}
		if duration != defaultWindowLifetime {
			serverResult <- errors.New("unexpected duration")
			return
		}
		_, writeErr := connection.Write([]byte(`{"accepted":true}` + "\n"))
		serverResult <- writeErr
	}()

	client, err := net.DialUnix(
		"unix",
		nil,
		&net.UnixAddr{Name: socketPath, Net: "unix"},
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := client.Write([]byte(`{"operation":"OPEN"}` + "\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	response, err := bufio.NewReader(client).ReadString('\n')
	if err != nil {
		t.Fatalf("read response while write side remains open: %v", err)
	}
	if response != `{"accepted":true}`+"\n" {
		t.Fatalf("response = %q", response)
	}
	if err := <-serverResult; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestDecodeOpenRequestEnforcesSizeLimit(t *testing.T) {
	t.Parallel()
	content := `{"operation":"OPEN"}` +
		strings.Repeat(" ", maxControlRequestBytes)
	if _, err := decodeOpenRequest(strings.NewReader(content)); err == nil {
		t.Fatal("oversized request was accepted")
	}
}

func TestRootPeerAuthorization(t *testing.T) {
	t.Parallel()
	if !rootPeerAuthorized(0) {
		t.Fatal("root peer was rejected")
	}
	if rootPeerAuthorized(1000) {
		t.Fatal("non-root peer was authorized")
	}
}

func TestChildProcessAttributesPreventOrphans(t *testing.T) {
	t.Parallel()
	attributes := childProcessAttributes()
	if attributes.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("parent-death signal = %v", attributes.Pdeathsig)
	}
	if !attributes.Setpgid {
		t.Fatal("child does not receive a dedicated process group")
	}
}

func createRetainedUnixSocket(
	t *testing.T,
	mode os.FileMode,
	closeListener bool,
) (string, *net.UnixListener) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: path, Net: "unix"},
	)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(path, mode); err != nil {
		listener.Close()
		t.Fatalf("chmod socket: %v", err)
	}
	if closeListener {
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}
		return path, nil
	}
	return path, listener
}

func TestRecoverStaleRootOwnedControlSocket(t *testing.T) {
	t.Parallel()
	path, _ := createRetainedUnixSocket(t, 0600, true)
	err := recoverStaleControlSocket(
		path,
		uint32(os.Geteuid()),
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("recover stale socket: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket remains: %v", err)
	}
}

func TestRefuseLiveControlSocket(t *testing.T) {
	t.Parallel()
	path, listener := createRetainedUnixSocket(t, 0600, false)
	defer listener.Close()
	defer os.Remove(path)
	err := recoverStaleControlSocket(
		path,
		uint32(os.Geteuid()),
		100*time.Millisecond,
	)
	if err == nil {
		t.Fatal("live socket was removed")
	}
	if _, statErr := os.Lstat(path); statErr != nil {
		t.Fatalf("live socket path was removed: %v", statErr)
	}
}

func TestRefuseUnsafeStaleControlSocket(t *testing.T) {
	t.Parallel()
	path, _ := createRetainedUnixSocket(t, 0666, true)
	defer os.Remove(path)
	err := recoverStaleControlSocket(
		path,
		uint32(os.Geteuid()),
		100*time.Millisecond,
	)
	if err == nil {
		t.Fatal("unsafe stale socket was removed")
	}
	if _, statErr := os.Lstat(path); statErr != nil {
		t.Fatalf("unsafe socket path was removed: %v", statErr)
	}
}

func TestWaitForUnixSocketRejectsStaleInodeAfterChildExit(t *testing.T) {
	t.Parallel()
	path, _ := createRetainedUnixSocket(t, 0600, true)
	defer os.Remove(path)
	child := newFakeProcess()
	child.once.Do(func() { close(child.done) })

	err := waitForUnixSocket(
		context.Background(),
		path,
		child,
		time.Second,
	)
	if err == nil || !strings.Contains(err.Error(), "child exited") {
		t.Fatalf("stale socket readiness error = %v", err)
	}
}

func TestWaitForUnixSocketRequiresLiveListener(t *testing.T) {
	t.Parallel()
	path, listener := createRetainedUnixSocket(t, 0600, false)
	defer listener.Close()
	defer os.Remove(path)

	if err := waitForUnixSocket(
		context.Background(),
		path,
		newFakeProcess(),
		time.Second,
	); err != nil {
		t.Fatalf("live socket was not ready: %v", err)
	}
}

func TestSocketLifecycleLockSerializesCallers(t *testing.T) {
	t.Parallel()
	lockPath := filepath.Join(t.TempDir(), "apply.sock.lock")
	entered := make(chan string, 2)
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- withSocketLifecycleLock(
			context.Background(),
			lockPath,
			uint32(os.Geteuid()),
			func() error {
				entered <- "first"
				<-release
				return nil
			},
		)
	}()
	if caller := <-entered; caller != "first" {
		t.Fatalf("first lock caller = %q", caller)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- withSocketLifecycleLock(
			context.Background(),
			lockPath,
			uint32(os.Geteuid()),
			func() error {
				entered <- "second"
				return nil
			},
		)
	}()
	select {
	case caller := <-entered:
		t.Fatalf("%s caller bypassed held lifecycle lock", caller)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case caller := <-entered:
		if caller != "second" {
			t.Fatalf("second lock caller = %q", caller)
		}
	case <-time.After(time.Second):
		t.Fatal("second caller did not continue after lock release")
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestWindowGateSerializesWindows(t *testing.T) {
	t.Parallel()
	gate := &windowGate{}
	if !gate.TryAcquire() {
		t.Fatal("first window was rejected")
	}
	if gate.TryAcquire() {
		t.Fatal("second concurrent window was accepted")
	}
	gate.Release()
	if !gate.TryAcquire() {
		t.Fatal("window was not accepted after release")
	}
	gate.Release()
}

func TestRenderPrivateAPConfigurations(t *testing.T) {
	t.Parallel()
	config := windowConfig{
		runtimeDirectory: "/run/reinvoke/provision-window",
		interfaceName:    "p2p0",
		network:          testNetworkPlan(t),
	}
	paths := pathsFor(config)
	credentials := apCredentials{
		SSID: "installation-ap",
		PSK:  "installation-secret",
	}
	hostapd := string(renderHostapdConfig(config, paths, credentials))
	for _, expected := range []string{
		"interface=p2p0",
		"ssid=installation-ap",
		"wpa=2",
		"wpa_passphrase=installation-secret",
		"ctrl_interface=/run/reinvoke/provision-window/hostapd-control",
	} {
		if !strings.Contains(hostapd, expected) {
			t.Fatalf("hostapd config does not contain %q", expected)
		}
	}
	dhcp := string(renderUDHCPDConfig(config, paths))
	for _, forbidden := range []string{"router", "dns", "gateway"} {
		if strings.Contains(strings.ToLower(dhcp), forbidden) {
			t.Fatalf("DHCP config contains forbidden option %q", forbidden)
		}
	}
	var advertisedOptions []string
	for _, line := range strings.Split(dhcp, "\n") {
		if strings.HasPrefix(line, "option ") ||
			strings.HasPrefix(line, "opt ") {
			advertisedOptions = append(advertisedOptions, line)
		}
	}
	if len(advertisedOptions) != 1 ||
		advertisedOptions[0] != "option subnet 255.255.255.0" {
		t.Fatalf("DHCP advertised options = %#v", advertisedOptions)
	}
	if !strings.Contains(dhcp, "interface p2p0") ||
		!strings.Contains(dhcp, "option subnet 255.255.255.0") {
		t.Fatalf("unexpected DHCP config:\n%s", dhcp)
	}

	directory := t.TempDir()
	path := filepath.Join(directory, "hostapd.conf")
	if err := writePrivateFile(path, []byte(hostapd)); err != nil {
		t.Fatalf("write private config: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat private config: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("private config mode = %#o", info.Mode().Perm())
	}

	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	logger.Printf("provisioning window starting")
	logger.Printf("provisioning window closed")
	if strings.Contains(output.String(), credentials.SSID) ||
		strings.Contains(output.String(), credentials.PSK) {
		t.Fatal("credentials appeared in window logs")
	}
}

func TestValidateAPCredentials(t *testing.T) {
	t.Parallel()
	if err := validateAPCredentials(apCredentials{
		SSID: "valid-ap",
		PSK:  "valid-password",
	}); err != nil {
		t.Fatalf("valid credentials: %v", err)
	}
	for _, credentials := range []apCredentials{
		{SSID: "", PSK: "valid-password"},
		{SSID: "bad\nssid", PSK: "valid-password"},
		{SSID: "valid-ap", PSK: "short"},
		{SSID: "valid-ap", PSK: "bad\npassword"},
	} {
		if err := validateAPCredentials(credentials); err == nil {
			t.Fatalf("invalid credentials accepted: %#v", credentials)
		}
	}
}

func TestReadCredentialFileRejectsSymlink(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	target := filepath.Join(directory, "credential")
	if err := os.WriteFile(target, []byte("private-value\n"), 0600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	value, err := readCredentialFile(target, uint32(os.Geteuid()))
	if err != nil {
		t.Fatalf("read regular credential: %v", err)
	}
	if value != "private-value" {
		t.Fatalf("credential = %q", value)
	}
	link := filepath.Join(directory, "credential-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create credential symlink: %v", err)
	}
	if _, err := readCredentialFile(link, uint32(os.Geteuid())); err == nil {
		t.Fatal("credential symlink was accepted")
	}
}

type fakeRunner struct {
	mu       sync.Mutex
	commands [][]string
}

func (r *fakeRunner) Run(
	_ context.Context,
	name string,
	arguments ...string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	command := append([]string{name}, arguments...)
	r.commands = append(r.commands, command)
	return nil
}

func (r *fakeRunner) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([][]string, len(r.commands))
	for index := range r.commands {
		result[index] = append([]string(nil), r.commands[index]...)
	}
	return result
}

type fakeProcess struct {
	done       chan struct{}
	once       sync.Once
	mu         sync.Mutex
	err        error
	signals    int
	kills      int
	signalSeen os.Signal
	ignoreTERM bool
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{done: make(chan struct{})}
}

func (p *fakeProcess) Done() <-chan struct{} {
	return p.done
}

func (p *fakeProcess) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *fakeProcess) Signal(signal os.Signal) error {
	p.mu.Lock()
	p.signals++
	p.signalSeen = signal
	p.mu.Unlock()
	if !p.ignoreTERM {
		p.once.Do(func() { close(p.done) })
	}
	return nil
}

func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	p.kills++
	p.mu.Unlock()
	p.once.Do(func() { close(p.done) })
	return nil
}

func TestStopProcessesKillsChildThatIgnoresTermination(t *testing.T) {
	t.Parallel()
	child := newFakeProcess()
	child.ignoreTERM = true
	if err := stopProcesses([]process{child}, time.Millisecond); err != nil {
		t.Fatalf("stop process: %v", err)
	}
	child.mu.Lock()
	signals := child.signals
	kills := child.kills
	child.mu.Unlock()
	if signals != 1 || kills != 1 {
		t.Fatalf("signals = %d, kills = %d", signals, kills)
	}
}

type fakeStarter struct {
	mu        sync.Mutex
	commands  [][]string
	processes []*fakeProcess
	all       chan struct{}
	once      sync.Once
}

func (s *fakeStarter) Start(
	name string,
	arguments ...string,
) (process, error) {
	child := newFakeProcess()
	s.mu.Lock()
	s.commands = append(
		s.commands,
		append([]string{name}, arguments...),
	)
	s.processes = append(s.processes, child)
	if len(s.processes) == 5 {
		s.once.Do(func() { close(s.all) })
	}
	s.mu.Unlock()
	return child, nil
}

func TestSuccessfulWindowCleanupPreservesStationState(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	runtimeDirectory := filepath.Join(parent, "window")
	stationConfig := filepath.Join(parent, "wpa_supplicant.conf")
	stationControl := filepath.Join(parent, "wpa_supplicant")
	stationMarker := filepath.Join(stationControl, "station-state")
	if err := os.WriteFile(stationConfig, []byte("existing-station\n"), 0600); err != nil {
		t.Fatalf("write station config: %v", err)
	}
	if err := os.Mkdir(stationControl, 0700); err != nil {
		t.Fatalf("create station control: %v", err)
	}
	if err := os.WriteFile(stationMarker, []byte("existing-control\n"), 0600); err != nil {
		t.Fatalf("write station control marker: %v", err)
	}
	runner := &fakeRunner{}
	starter := &fakeStarter{all: make(chan struct{})}
	var recoveredApply bool
	var lifecycleLockHeld bool
	config := windowConfig{
		runtimeDirectory: runtimeDirectory,
		interfaceName:    "p2p0",
		stationInterface: "mlan0",
		network:          testNetworkPlan(t),
		httpsPort:        8443,
		httpPort:         8080,
		readyTimeout:     time.Second,
		applyTimeout:     25 * time.Second,
		hostapdPath:      "/trusted/hostapd",
		busyboxPath:      "/trusted/busybox",
		applydPath:       "/trusted/reinvoke-wifi-applyd",
		provisiondPath:   "/trusted/reinvoke-provisiond",
		applySocket:      filepath.Join(parent, "wifi-apply.sock"),
		stationConfig:    stationConfig,
		stationControl:   stationControl,
	}
	manager := &windowManager{
		config:  config,
		runner:  runner,
		starter: starter,
		loadCredentials: func() (apCredentials, error) {
			return apCredentials{
				SSID: "private-installation-ap",
				PSK:  "private-installation-password",
			}, nil
		},
		disableForwarding: func() error { return nil },
		waitApply: func(
			context.Context,
			string,
			process,
			time.Duration,
		) error {
			return nil
		},
		waitAP: func(
			context.Context,
			string,
			string,
			process,
			time.Duration,
		) error {
			return nil
		},
		waitDescriptor: func(
			context.Context,
			string,
			process,
			time.Duration,
		) error {
			return nil
		},
		recoverApply: func(string, uint32, time.Duration) error {
			recoveredApply = true
			lockFile, err := os.OpenFile(
				config.applySocket+".lock",
				os.O_RDWR,
				0,
			)
			if err != nil {
				return err
			}
			defer lockFile.Close()
			err = syscall.Flock(
				int(lockFile.Fd()),
				syscall.LOCK_EX|syscall.LOCK_NB,
			)
			lifecycleLockHeld = errors.Is(err, syscall.EWOULDBLOCK)
			if err == nil {
				_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			}
			return nil
		},
		removeAll: os.RemoveAll,
	}

	result := make(chan error, 1)
	go func() {
		result <- manager.Run(context.Background(), 5*time.Minute)
	}()
	select {
	case <-starter.all:
	case <-time.After(time.Second):
		t.Fatal("window children did not start")
	}
	if !recoveredApply {
		t.Fatal("window did not recover the apply socket before launch")
	}
	if !lifecycleLockHeld {
		t.Fatal("apply socket recovery ran without the lifecycle lock")
	}
	starter.processes[1].once.Do(func() {
		close(starter.processes[1].done)
	})
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("successful window cleanup: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("window cleanup did not finish")
	}

	if _, err := os.Lstat(runtimeDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime directory remains after cleanup: %v", err)
	}
	configContent, err := os.ReadFile(stationConfig)
	if err != nil {
		t.Fatalf("station config was removed: %v", err)
	}
	if string(configContent) != "existing-station\n" {
		t.Fatalf("station config changed: %q", configContent)
	}
	controlContent, err := os.ReadFile(stationMarker)
	if err != nil {
		t.Fatalf("station control was removed: %v", err)
	}
	if string(controlContent) != "existing-control\n" {
		t.Fatalf("station control changed: %q", controlContent)
	}
	for index, child := range starter.processes {
		child.mu.Lock()
		signals := child.signals
		signalSeen := child.signalSeen
		child.mu.Unlock()
		wantSignals := 1
		if index == 1 {
			wantSignals = 0
		}
		if signals != wantSignals ||
			(signals != 0 && signalSeen != syscall.SIGTERM) {
			t.Fatalf(
				"child %d cleanup signals = %d, signal = %v",
				index,
				signals,
				signalSeen,
			)
		}
	}
	commands := runner.snapshot()
	if len(commands) != 2 {
		t.Fatalf("network command count = %d, want 2", len(commands))
	}
	if strings.Join(commands[0], " ") !=
		"/trusted/busybox ifconfig p2p0 192.168.43.1 netmask 255.255.255.0 up" {
		t.Fatalf("interface setup command = %#v", commands[0])
	}
	if strings.Join(commands[1], " ") !=
		"/trusted/busybox ifconfig p2p0 0.0.0.0 down" {
		t.Fatalf("interface cleanup command = %#v", commands[1])
	}
	applyCommand := strings.Join(starter.commands[0], " ")
	for _, expected := range []string{
		"-socket " + filepath.Join(parent, "wifi-apply.sock"),
		"-config " + stationConfig,
		"-control-dir " + stationControl,
	} {
		if !strings.Contains(applyCommand, expected) {
			t.Fatalf("apply command %q does not contain %q", applyCommand, expected)
		}
	}
	for _, command := range starter.commands {
		joined := strings.Join(command, " ")
		if strings.Contains(joined, "private-installation-ap") ||
			strings.Contains(joined, "private-installation-password") {
			t.Fatalf("credential appeared in process arguments: %#v", command)
		}
	}
}

func TestHostapdLoaderCommandIsFixed(t *testing.T) {
	t.Parallel()
	config := windowConfig{
		runtimeDirectory: "/run/reinvoke/provision-window",
		hostapdPath:      "/trusted/hostapd",
		hostapdLoader:    "/trusted/loader",
		hostapdLibraries: "/trusted/lib",
	}
	name, arguments := hostapdCommand(config, pathsFor(config))
	if name != "/trusted/loader" {
		t.Fatalf("command = %q", name)
	}
	actual := strings.Join(arguments, " ")
	expected := "--library-path /trusted/lib /trusted/hostapd " +
		"/run/reinvoke/provision-window/hostapd.conf"
	if actual != expected {
		t.Fatalf("arguments = %q, want %q", actual, expected)
	}
}
