// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	defaultSocketPath       = "/run/reinvoke/provision-window.sock"
	defaultRuntimeDirectory = "/run/reinvoke/provision-window"
	defaultInterface        = "p2p0"
	defaultStationInterface = "mlan0"
	defaultAddress          = "192.168.43.1/24"
	defaultDHCPStart        = "192.168.43.100"
	defaultDHCPEnd          = "192.168.43.150"
	defaultHTTPSPort        = 8443
	defaultHTTPPort         = 8080
	defaultWindowLifetime   = 5 * time.Minute
	maxWindowLifetime       = 15 * time.Minute
	minWindowLifetime       = 30 * time.Second
	defaultReadyTimeout     = 15 * time.Second
	defaultApplyTimeout     = 25 * time.Second
	defaultHostapdPath      = "/usr/sbin/hostapd"
	defaultBusyBoxPath      = "/bin/busybox"
	defaultApplydPath       = "/bin/reinvoke-wifi-applyd"
	defaultProvisiondPath   = "/bin/reinvoke-provisiond"
	defaultApplySocket      = "/run/reinvoke/wifi-apply.sock"
	defaultStationConfig    = "/run/reinvoke/wpa_supplicant.conf"
	defaultStationControl   = "/run/reinvoke/wpa_supplicant"
	maxControlRequestBytes  = 1024
	maxSecretFileBytes      = 256
	tmpfsMagic              = 0x01021994
	ramfsMagic              = 0x858458f6
)

var interfacePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)

type openRequest struct {
	Operation       string `json:"operation"`
	DurationSeconds *int64 `json:"duration_seconds,omitempty"`
}

type openResponse struct {
	Accepted       bool  `json:"accepted"`
	DurationSecond int64 `json:"duration_seconds,omitempty"`
}

type apCredentials struct {
	SSID string
	PSK  string
}

type networkPlan struct {
	ServerIP net.IP
	Prefix   int
	CIDR     string
	StartIP  net.IP
	EndIP    net.IP
	Netmask  net.IP
}

type windowConfig struct {
	runtimeDirectory string
	interfaceName    string
	stationInterface string
	network          networkPlan
	httpsPort        int
	httpPort         int
	readyTimeout     time.Duration
	applyTimeout     time.Duration
	hostapdPath      string
	hostapdLoader    string
	hostapdLibraries string
	busyboxPath      string
	applydPath       string
	provisiondPath   string
	applySocket      string
	stationConfig    string
	stationControl   string
	apSSIDFile       string
	apPSKFile        string
}

type runtimePaths struct {
	hostapdConfig  string
	hostapdControl string
	dhcpConfig     string
	dhcpLease      string
	dhcpPID        string
	applySocket    string
	applyConfig    string
	applyControl   string
	bootstrap      string
	descriptor     string
}

type commandRunner interface {
	Run(context.Context, string, ...string) error
}

type process interface {
	Done() <-chan struct{}
	Err() error
	Signal(os.Signal) error
	Kill() error
}

type processStarter interface {
	Start(string, ...string) (process, error)
}

type execRunner struct{}

type execStarter struct{}

type execProcess struct {
	command *exec.Cmd
	done    chan struct{}
	mu      sync.Mutex
	err     error
}

type windowGate struct {
	mu     sync.Mutex
	active bool
}

type windowManager struct {
	config            windowConfig
	runner            commandRunner
	starter           processStarter
	loadCredentials   func() (apCredentials, error)
	disableForwarding func() error
	waitApply         func(context.Context, string, process, time.Duration) error
	waitAP            func(context.Context, string, string, process, time.Duration) error
	waitDescriptor    func(context.Context, string, process, time.Duration) error
	removeAll         func(string) error
}

type daemon struct {
	manager *windowManager
	gate    *windowGate
	logger  *log.Logger
	wg      sync.WaitGroup
	clients sync.WaitGroup
}

func (execRunner) Run(
	ctx context.Context,
	name string,
	arguments ...string,
) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = childProcessAttributes()
	return command.Run()
}

func (execStarter) Start(name string, arguments ...string) (process, error) {
	command := exec.Command(name, arguments...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = childProcessAttributes()
	if err := command.Start(); err != nil {
		return nil, err
	}
	child := &execProcess{command: command, done: make(chan struct{})}
	go func() {
		err := command.Wait()
		child.mu.Lock()
		child.err = err
		child.mu.Unlock()
		close(child.done)
	}()
	return child, nil
}

func childProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
		Setpgid:   true,
	}
}

func (p *execProcess) Done() <-chan struct{} {
	return p.done
}

func (p *execProcess) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *execProcess) Signal(signal os.Signal) error {
	unixSignal, ok := signal.(syscall.Signal)
	if !ok {
		return errors.New("unsupported process signal")
	}
	return signalProcessGroup(p.command.Process.Pid, unixSignal)
}

func (p *execProcess) Kill() error {
	return signalProcessGroup(p.command.Process.Pid, syscall.SIGKILL)
}

func signalProcessGroup(pid int, signal syscall.Signal) error {
	err := syscall.Kill(-pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func (g *windowGate) TryAcquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active {
		return false
	}
	g.active = true
	return true
}

func (g *windowGate) Release() {
	g.mu.Lock()
	g.active = false
	g.mu.Unlock()
}

func decodeOpenRequest(reader io.Reader) (time.Duration, error) {
	buffered := bufio.NewReaderSize(reader, maxControlRequestBytes+1)
	content, err := buffered.ReadSlice('\n')
	if err != nil || len(content) > maxControlRequestBytes {
		return 0, errors.New("invalid request")
	}
	content = bytes.TrimSuffix(content, []byte("\n"))
	content = bytes.TrimSuffix(content, []byte("\r"))
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var request openRequest
	if err := decoder.Decode(&request); err != nil {
		return 0, errors.New("invalid request")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return 0, errors.New("invalid request")
	}
	if request.Operation != "OPEN" {
		return 0, errors.New("unsupported operation")
	}
	duration := defaultWindowLifetime
	if request.DurationSeconds != nil {
		if *request.DurationSeconds > int64(maxWindowLifetime/time.Second) ||
			*request.DurationSeconds < int64(minWindowLifetime/time.Second) {
			return 0, errors.New("invalid duration")
		}
		duration = time.Duration(*request.DurationSeconds) * time.Second
	}
	return duration, nil
}

func rootPeerAuthorized(uid uint32) bool {
	return uid == 0
}

func verifyRootPeer(connection *net.UnixConn) error {
	rawConnection, err := connection.SyscallConn()
	if err != nil {
		return errors.New("cannot access peer credentials")
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
	}); err != nil || credentialErr != nil {
		return errors.New("cannot read peer credentials")
	}
	if credentials == nil || !rootPeerAuthorized(credentials.Uid) {
		return errors.New("peer is not root")
	}
	return nil
}

func writeOpenResponse(connection net.Conn, response openResponse) {
	_ = connection.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = json.NewEncoder(connection).Encode(response)
}

func (d *daemon) handleConnection(
	ctx context.Context,
	connection *net.UnixConn,
) {
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := verifyRootPeer(connection); err != nil {
		return
	}
	duration, err := decodeOpenRequest(connection)
	if err != nil {
		writeOpenResponse(connection, openResponse{Accepted: false})
		return
	}
	if ctx.Err() != nil {
		writeOpenResponse(connection, openResponse{Accepted: false})
		return
	}
	if !d.gate.TryAcquire() {
		writeOpenResponse(connection, openResponse{Accepted: false})
		return
	}

	writeOpenResponse(connection, openResponse{
		Accepted:       true,
		DurationSecond: int64(duration / time.Second),
	})
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer d.gate.Release()
		d.logger.Printf("provisioning window starting")
		if err := d.manager.Run(ctx, duration); err != nil &&
			!errors.Is(err, context.Canceled) {
			d.logger.Printf("provisioning window closed after an error")
			return
		}
		d.logger.Printf("provisioning window closed")
	}()
}

func (d *daemon) Serve(ctx context.Context, listener *net.UnixListener) error {
	serveContext, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	go func() {
		<-serveContext.Done()
		_ = listener.Close()
	}()
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			cancelServe()
			d.clients.Wait()
			d.wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept control connection: %w", err)
		}
		d.clients.Add(1)
		go func() {
			defer d.clients.Done()
			d.handleConnection(serveContext, connection)
		}()
	}
}

func pathsFor(config windowConfig) runtimePaths {
	bootstrap := filepath.Join(config.runtimeDirectory, "bootstrap")
	return runtimePaths{
		hostapdConfig:  filepath.Join(config.runtimeDirectory, "hostapd.conf"),
		hostapdControl: filepath.Join(config.runtimeDirectory, "hostapd-control"),
		dhcpConfig:     filepath.Join(config.runtimeDirectory, "udhcpd.conf"),
		dhcpLease:      filepath.Join(config.runtimeDirectory, "udhcpd.leases"),
		dhcpPID:        filepath.Join(config.runtimeDirectory, "udhcpd.pid"),
		applySocket:    config.applySocket,
		applyConfig:    config.stationConfig,
		applyControl:   config.stationControl,
		bootstrap:      bootstrap,
		descriptor:     filepath.Join(bootstrap, "provisioning.json"),
	}
}

func (m *windowManager) Run(parent context.Context, lifetime time.Duration) (
	runErr error,
) {
	ctx, cancel := context.WithTimeout(parent, lifetime)
	defer cancel()

	paths := pathsFor(m.config)
	if err := prepareRuntimeDirectory(
		m.config.runtimeDirectory,
		m.removeAll,
	); err != nil {
		return err
	}
	var (
		children            []process
		interfaceConfigured bool
	)
	defer func() {
		cleanupErr := m.cleanup(children, interfaceConfigured)
		if runErr == nil && cleanupErr != nil {
			runErr = cleanupErr
		}
	}()

	credentials, err := m.loadCredentials()
	if err != nil {
		return errors.New("load AP credentials")
	}
	defer func() {
		credentials.SSID = ""
		credentials.PSK = ""
	}()
	if err := validateAPCredentials(credentials); err != nil {
		return err
	}
	if err := os.Mkdir(paths.hostapdControl, 0700); err != nil {
		return errors.New("create hostapd control directory")
	}
	if err := os.Mkdir(paths.bootstrap, 0700); err != nil {
		return errors.New("create bootstrap directory")
	}
	if err := writePrivateFile(
		paths.hostapdConfig,
		renderHostapdConfig(m.config, paths, credentials),
	); err != nil {
		return errors.New("write hostapd configuration")
	}
	if err := writePrivateFile(
		paths.dhcpConfig,
		renderUDHCPDConfig(m.config, paths),
	); err != nil {
		return errors.New("write DHCP configuration")
	}
	if err := writePrivateFile(paths.dhcpLease, nil); err != nil {
		return errors.New("create DHCP lease file")
	}
	if err := m.disableForwarding(); err != nil {
		return errors.New("disable packet forwarding")
	}

	commandContext, cancelCommand := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	interfaceConfigured = true
	err = m.runner.Run(
		commandContext,
		m.config.busyboxPath,
		"ifconfig",
		m.config.interfaceName,
		m.config.network.ServerIP.String(),
		"netmask",
		m.config.network.Netmask.String(),
		"up",
	)
	cancelCommand()
	if err != nil {
		return errors.New("configure AP interface")
	}

	applyTimeout := m.config.applyTimeout
	if maximum := lifetime - 5*time.Second; applyTimeout > maximum {
		applyTimeout = maximum
	}
	applyProcess, err := m.starter.Start(
		m.config.applydPath,
		"-socket", paths.applySocket,
		"-config", paths.applyConfig,
		"-control-dir", paths.applyControl,
		"-interface", m.config.stationInterface,
		"-lifetime", lifetime.String(),
	)
	if err != nil {
		return errors.New("start Wi-Fi apply service")
	}
	children = append(children, applyProcess)
	if err := m.waitApply(
		ctx,
		paths.applySocket,
		applyProcess,
		m.config.readyTimeout,
	); err != nil {
		return errors.New("Wi-Fi apply service did not become ready")
	}

	provisionProcess, err := m.starter.Start(
		m.config.provisiondPath,
		"-listen", net.JoinHostPort(
			m.config.network.ServerIP.String(),
			strconv.Itoa(m.config.httpsPort),
		),
		"-apply-socket", paths.applySocket,
		"-descriptor", paths.descriptor,
		"-lifetime", lifetime.String(),
		"-apply-timeout", applyTimeout.String(),
	)
	if err != nil {
		return errors.New("start provisioning service")
	}
	children = append(children, provisionProcess)

	hostapdName, hostapdArguments := hostapdCommand(m.config, paths)
	hostapdProcess, err := m.starter.Start(hostapdName, hostapdArguments...)
	if err != nil {
		return errors.New("start access point")
	}
	children = append(children, hostapdProcess)
	if err := m.waitAP(
		ctx,
		filepath.Join(paths.hostapdControl, m.config.interfaceName),
		m.config.interfaceName,
		hostapdProcess,
		m.config.readyTimeout,
	); err != nil {
		return errors.New("access point did not become ready")
	}
	if err := m.waitDescriptor(
		ctx,
		paths.descriptor,
		provisionProcess,
		m.config.readyTimeout,
	); err != nil {
		return errors.New("provisioning descriptor did not become ready")
	}

	dhcpProcess, err := m.starter.Start(
		m.config.busyboxPath,
		"udhcpd",
		"-f",
		paths.dhcpConfig,
	)
	if err != nil {
		return errors.New("start DHCP service")
	}
	children = append(children, dhcpProcess)
	httpProcess, err := m.starter.Start(
		m.config.busyboxPath,
		"httpd",
		"-f",
		"-p",
		net.JoinHostPort(
			m.config.network.ServerIP.String(),
			strconv.Itoa(m.config.httpPort),
		),
		"-h",
		paths.bootstrap,
	)
	if err != nil {
		return errors.New("start bootstrap HTTP service")
	}
	children = append(children, httpProcess)

	return m.monitor(ctx, children)
}

func hostapdCommand(
	config windowConfig,
	paths runtimePaths,
) (string, []string) {
	if config.hostapdLoader != "" {
		return config.hostapdLoader, []string{
			"--library-path",
			config.hostapdLibraries,
			config.hostapdPath,
			paths.hostapdConfig,
		}
	}
	return config.hostapdPath, []string{paths.hostapdConfig}
}

func (m *windowManager) monitor(
	ctx context.Context,
	children []process,
) error {
	exits := make(chan int, len(children))
	for index, child := range children {
		index := index
		child := child
		go func() {
			<-child.Done()
			exits <- index
		}()
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil
			}
			return ctx.Err()
		case <-ticker.C:
			if err := m.disableForwarding(); err != nil {
				return errors.New("could not keep packet forwarding disabled")
			}
		case index := <-exits:
			switch index {
			case 1:
				if children[index].Err() != nil {
					return errors.New("provisioning service exited unsuccessfully")
				}
				return nil
			case 0:
				grace := time.NewTimer(5 * time.Second)
				select {
				case <-children[1].Done():
					stopTimer(grace)
					if children[1].Err() != nil {
						return errors.New("provisioning service exited unsuccessfully")
					}
					return nil
				case <-grace.C:
					return errors.New("Wi-Fi apply service exited early")
				case <-ctx.Done():
					stopTimer(grace)
					if errors.Is(ctx.Err(), context.DeadlineExceeded) {
						return nil
					}
					return ctx.Err()
				}
			default:
				return errors.New("window service exited early")
			}
		}
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (m *windowManager) cleanup(
	children []process,
	interfaceConfigured bool,
) error {
	var firstError error
	if err := stopProcesses(children, 3*time.Second); err != nil {
		firstError = err
	}
	if interfaceConfigured {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := m.runner.Run(
			ctx,
			m.config.busyboxPath,
			"ifconfig",
			m.config.interfaceName,
			"0.0.0.0",
			"down",
		)
		cancel()
		if err != nil && firstError == nil {
			firstError = errors.New("remove AP address")
		}
	}
	if err := m.removeAll(m.config.runtimeDirectory); err != nil &&
		firstError == nil {
		firstError = errors.New("remove window runtime")
	}
	return firstError
}

func stopProcesses(children []process, timeout time.Duration) error {
	var firstError error
	for index := len(children) - 1; index >= 0; index-- {
		select {
		case <-children[index].Done():
			continue
		default:
		}
		if err := children[index].Signal(syscall.SIGTERM); err != nil &&
			!errors.Is(err, os.ErrProcessDone) &&
			firstError == nil {
			firstError = errors.New("signal window process")
		}
	}
	if waitForProcesses(children, timeout) {
		return firstError
	}
	for _, remaining := range children {
		select {
		case <-remaining.Done():
		default:
			if err := remaining.Kill(); err != nil &&
				!errors.Is(err, os.ErrProcessDone) &&
				firstError == nil {
				firstError = errors.New("kill window process")
			}
		}
	}
	if !waitForProcesses(children, 2*time.Second) {
		return errors.New("window process cleanup timed out")
	}
	return firstError
}

func waitForProcesses(children []process, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for _, child := range children {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		timer := time.NewTimer(remaining)
		select {
		case <-child.Done():
			stopTimer(timer)
		case <-timer.C:
			return false
		}
	}
	return true
}

func renderHostapdConfig(
	config windowConfig,
	paths runtimePaths,
	credentials apCredentials,
) []byte {
	var builder strings.Builder
	builder.WriteString("interface=")
	builder.WriteString(config.interfaceName)
	builder.WriteString("\ndriver=nl80211\nssid=")
	builder.WriteString(credentials.SSID)
	builder.WriteString("\nhw_mode=g\nchannel=6\n")
	builder.WriteString("auth_algs=1\nwmm_enabled=1\n")
	builder.WriteString("wpa=2\nwpa_key_mgmt=WPA-PSK\n")
	builder.WriteString("rsn_pairwise=CCMP\nwpa_passphrase=")
	builder.WriteString(credentials.PSK)
	builder.WriteString("\nctrl_interface=")
	builder.WriteString(paths.hostapdControl)
	builder.WriteString("\n")
	return []byte(builder.String())
}

func renderUDHCPDConfig(config windowConfig, paths runtimePaths) []byte {
	var builder strings.Builder
	builder.WriteString("start ")
	builder.WriteString(config.network.StartIP.String())
	builder.WriteString("\nend ")
	builder.WriteString(config.network.EndIP.String())
	builder.WriteString("\ninterface ")
	builder.WriteString(config.interfaceName)
	builder.WriteString("\noption subnet ")
	builder.WriteString(config.network.Netmask.String())
	builder.WriteString("\nlease_file ")
	builder.WriteString(paths.dhcpLease)
	builder.WriteString("\npidfile ")
	builder.WriteString(paths.dhcpPID)
	builder.WriteString("\nauto_time 0\n")
	return []byte(builder.String())
}

func validateAPCredentials(credentials apCredentials) error {
	ssidLength := len([]byte(credentials.SSID))
	if ssidLength < 1 || ssidLength > 32 ||
		!utf8.ValidString(credentials.SSID) ||
		strings.ContainsAny(credentials.SSID, "\x00\r\n") {
		return errors.New("invalid AP SSID")
	}
	psk := []byte(credentials.PSK)
	if len(psk) < 8 || len(psk) > 63 {
		return errors.New("invalid AP passphrase")
	}
	for _, value := range psk {
		if value < 32 || value > 126 {
			return errors.New("invalid AP passphrase")
		}
	}
	return nil
}

func readCredentialFile(path string, expectedUID uint32) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("credential path must be absolute")
	}
	fileDescriptor, err := syscall.Open(
		path,
		syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return "", errors.New("open credential file")
	}
	file := os.NewFile(uintptr(fileDescriptor), path)
	if file == nil {
		syscall.Close(fileDescriptor)
		return "", errors.New("open credential file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", errors.New("inspect credential file")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("credential must be a regular file")
	}
	uid, err := fileOwnerUID(info)
	if err != nil || uid != expectedUID || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("credential file is not root-only")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxSecretFileBytes+1))
	if err != nil || len(content) > maxSecretFileBytes {
		return "", errors.New("read credential file")
	}
	content = bytes.TrimSuffix(content, []byte("\n"))
	content = bytes.TrimSuffix(content, []byte("\r"))
	return string(content), nil
}

func loadCredentialFiles(
	ssidPath string,
	pskPath string,
	expectedUID uint32,
) (apCredentials, error) {
	ssid, err := readCredentialFile(ssidPath, expectedUID)
	if err != nil {
		return apCredentials{}, err
	}
	psk, err := readCredentialFile(pskPath, expectedUID)
	if err != nil {
		return apCredentials{}, err
	}
	return apCredentials{SSID: ssid, PSK: psk}, nil
}

func writePrivateFile(path string, content []byte) error {
	file, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0600,
	)
	if err != nil {
		return err
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

func waitForUnixSocket(
	ctx context.Context,
	path string,
	child process,
	timeout time.Duration,
) error {
	return waitForCondition(ctx, child, timeout, func() bool {
		info, err := os.Lstat(path)
		return err == nil &&
			info.Mode()&os.ModeSocket != 0 &&
			info.Mode()&os.ModeSymlink == 0
	})
}

func waitForPrivateFile(
	ctx context.Context,
	path string,
	child process,
	timeout time.Duration,
) error {
	return waitForCondition(ctx, child, timeout, func() bool {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() ||
			info.Mode()&os.ModeSymlink != 0 ||
			info.Mode().Perm() != 0600 ||
			info.Size() == 0 {
			return false
		}
		uid, err := fileOwnerUID(info)
		return err == nil && uid == 0
	})
}

func waitForHostapd(
	ctx context.Context,
	path string,
	_ string,
	child process,
	timeout time.Duration,
) error {
	localPath := filepath.Join(filepath.Dir(filepath.Dir(path)), ".hostapd-query")
	return waitForCondition(ctx, child, timeout, func() bool {
		_ = os.Remove(localPath)
		local := &net.UnixAddr{Name: localPath, Net: "unixgram"}
		remote := &net.UnixAddr{Name: path, Net: "unixgram"}
		connection, err := net.DialUnix("unixgram", local, remote)
		if err != nil {
			return false
		}
		defer connection.Close()
		defer os.Remove(localPath)
		_ = connection.SetDeadline(time.Now().Add(250 * time.Millisecond))
		if _, err := connection.Write([]byte("STATUS")); err != nil {
			return false
		}
		response := make([]byte, 4096)
		count, err := connection.Read(response)
		if err != nil {
			return false
		}
		status := "\n" + string(response[:count])
		return strings.Contains(status, "\nstate=ENABLED\n") ||
			strings.HasSuffix(status, "\nstate=ENABLED")
	})
}

func waitForCondition(
	ctx context.Context,
	child process,
	timeout time.Duration,
	condition func() bool,
) error {
	readyTimer := time.NewTimer(timeout)
	defer readyTimer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-child.Done():
			return errors.New("child exited before readiness")
		case <-readyTimer.C:
			return errors.New("readiness deadline exceeded")
		case <-ticker.C:
		}
	}
}

func disableKernelForwarding() error {
	for _, path := range []string{
		"/proc/sys/net/ipv4/ip_forward",
		"/proc/sys/net/ipv6/conf/all/forwarding",
	} {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		_, writeErr := file.Write([]byte("0\n"))
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		content, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(content)) != "0" {
			return errors.New("forwarding remains enabled")
		}
	}
	return nil
}

func parseNetworkPlan(
	cidr string,
	startText string,
	endText string,
) (networkPlan, error) {
	serverIP, network, err := net.ParseCIDR(cidr)
	if err != nil || serverIP.To4() == nil {
		return networkPlan{}, errors.New("address must be an IPv4 CIDR")
	}
	ones, bits := network.Mask.Size()
	if bits != 32 || ones < 16 || ones > 30 {
		return networkPlan{}, errors.New("AP prefix must be from 16 through 30")
	}
	startIP := net.ParseIP(startText).To4()
	endIP := net.ParseIP(endText).To4()
	serverIP = serverIP.To4()
	if startIP == nil || endIP == nil ||
		!network.Contains(startIP) ||
		!network.Contains(endIP) {
		return networkPlan{}, errors.New("DHCP range must be inside AP network")
	}
	serverValue := ipv4Value(serverIP)
	startValue := ipv4Value(startIP)
	endValue := ipv4Value(endIP)
	networkValue := ipv4Value(network.IP.To4())
	broadcastValue := networkValue | ^binary.BigEndian.Uint32(network.Mask)
	if startValue > endValue ||
		startValue == networkValue ||
		endValue == broadcastValue ||
		(serverValue >= startValue && serverValue <= endValue) ||
		serverValue == networkValue ||
		serverValue == broadcastValue {
		return networkPlan{}, errors.New("invalid DHCP range")
	}
	netmask := net.IP(append([]byte(nil), network.Mask...))
	return networkPlan{
		ServerIP: serverIP,
		Prefix:   ones,
		CIDR:     serverIP.String() + "/" + strconv.Itoa(ones),
		StartIP:  startIP,
		EndIP:    endIP,
		Netmask:  netmask,
	}, nil
}

func ipv4Value(ip net.IP) uint32 {
	return binary.BigEndian.Uint32(ip.To4())
}

func fileOwnerUID(info os.FileInfo) (uint32, error) {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("filesystem does not expose Unix ownership")
	}
	return status.Uid, nil
}

func validateRootDirectory(
	path string,
	rootOnly bool,
	requireRAM bool,
) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a directory")
	}
	uid, err := fileOwnerUID(info)
	if err != nil || uid != 0 {
		return errors.New("directory is not root-owned")
	}
	if rootOnly && info.Mode().Perm()&0077 != 0 {
		return errors.New("directory is not root-only")
	}
	if !rootOnly && info.Mode().Perm()&0022 != 0 {
		return errors.New("directory is writable by an untrusted user")
	}
	if requireRAM {
		var filesystem syscall.Statfs_t
		if err := syscall.Statfs(path, &filesystem); err != nil {
			return err
		}
		filesystemType := uint64(filesystem.Type) & 0xffffffff
		if filesystemType != tmpfsMagic && filesystemType != ramfsMagic {
			return errors.New("directory is not on RAM-backed storage")
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
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("executable is not a regular file")
	}
	uid, err := fileOwnerUID(info)
	if err != nil || uid != 0 || info.Mode().Perm()&0022 != 0 ||
		info.Mode().Perm()&0111 == 0 {
		return errors.New("executable is not root-controlled")
	}
	return nil
}

func validateLibraryPath(value string) error {
	if value == "" {
		return errors.New("hostapd library path is empty")
	}
	for _, path := range strings.Split(value, ":") {
		if !filepath.IsAbs(path) {
			return errors.New("hostapd library path must be absolute")
		}
		if err := validateRootDirectory(path, false, false); err != nil {
			return errors.New("hostapd library path is not root-controlled")
		}
	}
	return nil
}

func prepareRuntimeDirectory(
	path string,
	removeAll func(string) error,
) error {
	if !filepath.IsAbs(path) {
		return errors.New("runtime directory must be absolute")
	}
	if info, err := os.Lstat(path); err == nil {
		uid, uidErr := fileOwnerUID(info)
		if uidErr != nil || !info.IsDir() ||
			info.Mode()&os.ModeSymlink != 0 ||
			uid != 0 ||
			info.Mode().Perm()&0077 != 0 {
			return errors.New("existing runtime directory is unsafe")
		}
		if err := removeAll(path); err != nil {
			return errors.New("remove stale runtime directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("inspect runtime directory")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		return errors.New("create runtime directory")
	}
	if err := os.Chmod(path, 0700); err != nil {
		_ = removeAll(path)
		return errors.New("restrict runtime directory")
	}
	return nil
}

func ensureSocketParent(path string) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	return validateRootDirectory(parent, false, true)
}

func recoverStaleControlSocket(
	path string,
	expectedUID uint32,
	probeTimeout time.Duration,
) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("inspect existing control socket")
	}
	if info.Mode()&os.ModeSocket == 0 ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0600 {
		return errors.New("existing control socket is unsafe")
	}
	uid, err := fileOwnerUID(info)
	if err != nil || uid != expectedUID {
		return errors.New("existing control socket is unsafe")
	}

	connection, dialErr := net.DialTimeout("unix", path, probeTimeout)
	if dialErr == nil {
		connection.Close()
		return errors.New("control socket already has a live listener")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) &&
		!errors.Is(dialErr, os.ErrNotExist) {
		return errors.New("could not prove control socket is stale")
	}
	if errors.Is(dialErr, os.ErrNotExist) {
		return nil
	}

	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !os.SameFile(info, current) {
		return errors.New("control socket changed during stale recovery")
	}
	if err := os.Remove(path); err != nil {
		return errors.New("remove stale control socket")
	}
	return nil
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("port must be from 1 through 65535")
	}
	return nil
}

func run() error {
	var (
		socketPath       string
		runtimeDirectory string
		interfaceName    string
		stationInterface string
		address          string
		dhcpStart        string
		dhcpEnd          string
		apSSIDFile       string
		apPSKFile        string
		hostapdPath      string
		hostapdLoader    string
		hostapdLibraries string
		busyboxPath      string
		applydPath       string
		provisiondPath   string
		applySocket      string
		stationConfig    string
		stationControl   string
		httpsPort        int
		httpPort         int
		readyTimeout     time.Duration
		applyTimeout     time.Duration
	)
	flag.StringVar(&socketPath, "socket", defaultSocketPath, "root-only MCU control socket")
	flag.StringVar(
		&runtimeDirectory,
		"runtime-dir",
		defaultRuntimeDirectory,
		"root-only RAM window directory",
	)
	flag.StringVar(&interfaceName, "interface", defaultInterface, "AP interface")
	flag.StringVar(
		&stationInterface,
		"station-interface",
		defaultStationInterface,
		"station interface",
	)
	flag.StringVar(&address, "address", defaultAddress, "AP IPv4 CIDR")
	flag.StringVar(&dhcpStart, "dhcp-start", defaultDHCPStart, "first DHCP address")
	flag.StringVar(&dhcpEnd, "dhcp-end", defaultDHCPEnd, "last DHCP address")
	flag.StringVar(&apSSIDFile, "ap-ssid-file", "", "required root-only AP SSID file")
	flag.StringVar(&apPSKFile, "ap-psk-file", "", "required root-only AP WPA2 passphrase file")
	flag.StringVar(&hostapdPath, "hostapd", defaultHostapdPath, "trusted hostapd executable")
	flag.StringVar(&hostapdLoader, "hostapd-loader", "", "optional trusted ELF loader")
	flag.StringVar(
		&hostapdLibraries,
		"hostapd-library-path",
		"",
		"root-controlled loader library path",
	)
	flag.StringVar(&busyboxPath, "busybox", defaultBusyBoxPath, "trusted BusyBox executable")
	flag.StringVar(&applydPath, "wifi-applyd", defaultApplydPath, "trusted Wi-Fi apply daemon")
	flag.StringVar(&provisiondPath, "provisiond", defaultProvisiondPath, "trusted provisioning daemon")
	flag.StringVar(&applySocket, "apply-socket", defaultApplySocket, "Wi-Fi apply Unix socket")
	flag.StringVar(&stationConfig, "station-config", defaultStationConfig, "RAM station configuration")
	flag.StringVar(
		&stationControl,
		"station-control-dir",
		defaultStationControl,
		"RAM station control directory",
	)
	flag.IntVar(&httpsPort, "https-port", defaultHTTPSPort, "provisioning TLS port")
	flag.IntVar(&httpPort, "http-port", defaultHTTPPort, "descriptor HTTP port")
	flag.DurationVar(&readyTimeout, "ready-timeout", defaultReadyTimeout, "child readiness deadline")
	flag.DurationVar(&applyTimeout, "apply-timeout", defaultApplyTimeout, "station apply deadline")
	flag.Parse()

	if os.Geteuid() != 0 {
		return errors.New("provisioning window daemon must run as root")
	}
	if apSSIDFile == "" || apPSKFile == "" {
		return errors.New("AP SSID and passphrase files are required")
	}
	if !filepath.IsAbs(socketPath) || !filepath.IsAbs(runtimeDirectory) {
		return errors.New("control socket and runtime directory must be absolute")
	}
	if !filepath.IsAbs(applySocket) ||
		!filepath.IsAbs(stationConfig) ||
		!filepath.IsAbs(stationControl) {
		return errors.New("station paths must be absolute")
	}
	socketPath = filepath.Clean(socketPath)
	runtimeDirectory = filepath.Clean(runtimeDirectory)
	applySocket = filepath.Clean(applySocket)
	stationConfig = filepath.Clean(stationConfig)
	stationControl = filepath.Clean(stationControl)
	if !interfacePattern.MatchString(interfaceName) ||
		!interfacePattern.MatchString(stationInterface) ||
		interfaceName == stationInterface {
		return errors.New("invalid interface configuration")
	}
	network, err := parseNetworkPlan(address, dhcpStart, dhcpEnd)
	if err != nil {
		return err
	}
	if err := validatePort(httpsPort); err != nil {
		return err
	}
	if err := validatePort(httpPort); err != nil || httpPort == httpsPort {
		return errors.New("invalid HTTP port")
	}
	if readyTimeout < time.Second || readyTimeout > time.Minute {
		return errors.New("ready timeout must be from 1s through 1m")
	}
	if applyTimeout < 5*time.Second ||
		applyTimeout > minWindowLifetime-5*time.Second {
		return errors.New("apply timeout must be from 5s through 25s")
	}
	cleanRuntime := runtimeDirectory
	socketRelative, err := filepath.Rel(cleanRuntime, socketPath)
	if err != nil ||
		cleanRuntime == filepath.Dir(cleanRuntime) ||
		socketRelative == "." ||
		(socketRelative != ".." &&
			!strings.HasPrefix(socketRelative, ".."+string(filepath.Separator))) {
		return errors.New("runtime directory must not contain the control socket")
	}
	if err := ensureSocketParent(socketPath); err != nil {
		return errors.New("validate control socket parent")
	}
	if err := validateRootDirectory(
		filepath.Dir(runtimeDirectory),
		false,
		true,
	); err != nil {
		return errors.New("validate runtime parent")
	}
	stationParent := filepath.Dir(stationConfig)
	if filepath.Dir(applySocket) != stationParent ||
		filepath.Dir(stationControl) != stationParent ||
		applySocket == stationConfig ||
		applySocket == stationControl ||
		stationConfig == stationControl {
		return errors.New("station paths must be distinct and share a directory")
	}
	for _, stationPath := range []string{
		applySocket,
		stationConfig,
		stationControl,
	} {
		relative, relativeErr := filepath.Rel(cleanRuntime, stationPath)
		if relativeErr != nil ||
			relative == "." ||
			(relative != ".." &&
				!strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			return errors.New("station paths must be outside the window runtime")
		}
	}
	if err := ensureSocketParent(applySocket); err != nil {
		return errors.New("validate station path parent")
	}
	for _, executable := range []string{
		hostapdPath,
		busyboxPath,
		applydPath,
		provisiondPath,
	} {
		if err := validateRootExecutable(executable); err != nil {
			return errors.New("validate configured executable")
		}
	}
	if (hostapdLoader == "") != (hostapdLibraries == "") {
		return errors.New("hostapd loader and library path must be set together")
	}
	if hostapdLoader != "" {
		if err := validateRootExecutable(hostapdLoader); err != nil {
			return errors.New("validate hostapd loader")
		}
		if err := validateLibraryPath(hostapdLibraries); err != nil {
			return err
		}
	}
	if _, err := loadCredentialFiles(apSSIDFile, apPSKFile, 0); err != nil {
		return errors.New("validate AP credential files")
	}

	if err := recoverStaleControlSocket(socketPath, 0, 500*time.Millisecond); err != nil {
		return err
	}
	listener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: socketPath, Net: "unix"},
	)
	if err != nil {
		return fmt.Errorf("listen on control socket: %w", err)
	}
	listener.SetUnlinkOnClose(true)
	defer listener.Close()
	if err := os.Chmod(socketPath, 0600); err != nil {
		return errors.New("restrict control socket")
	}

	config := windowConfig{
		runtimeDirectory: runtimeDirectory,
		interfaceName:    interfaceName,
		stationInterface: stationInterface,
		network:          network,
		httpsPort:        httpsPort,
		httpPort:         httpPort,
		readyTimeout:     readyTimeout,
		applyTimeout:     applyTimeout,
		hostapdPath:      hostapdPath,
		hostapdLoader:    hostapdLoader,
		hostapdLibraries: hostapdLibraries,
		busyboxPath:      busyboxPath,
		applydPath:       applydPath,
		provisiondPath:   provisiondPath,
		applySocket:      applySocket,
		stationConfig:    stationConfig,
		stationControl:   stationControl,
		apSSIDFile:       apSSIDFile,
		apPSKFile:        apPSKFile,
	}
	logger := log.New(os.Stderr, "reinvoke-windowd: ", log.LstdFlags)
	manager := &windowManager{
		config:  config,
		runner:  execRunner{},
		starter: execStarter{},
		loadCredentials: func() (apCredentials, error) {
			return loadCredentialFiles(apSSIDFile, apPSKFile, 0)
		},
		disableForwarding: disableKernelForwarding,
		waitApply:         waitForUnixSocket,
		waitAP:            waitForHostapd,
		waitDescriptor:    waitForPrivateFile,
		removeAll:         os.RemoveAll,
	}
	service := &daemon{
		manager: manager,
		gate:    &windowGate{},
		logger:  logger,
	}
	ctx, stopSignals := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stopSignals()
	logger.Printf("control socket ready")
	return service.Serve(ctx, listener)
}

func main() {
	if err := run(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}
