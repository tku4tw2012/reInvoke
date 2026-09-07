// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	capturePrivacyTimeout = 3 * time.Second
	maxCapturePrivacyLine = 160
	privacyEpochBytes     = 16
)

var captureGenerationPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

type capturePrivacyGate interface {
	Fence(context.Context) error
	Drain(context.Context) error
	State(context.Context, bool) error
	Allow(context.Context) error
	Live() bool
	TerminateVerified() error
}

type capturePrivacySession struct {
	connection net.Conn
	generation string
	epoch      string
	timeout    time.Duration
	terminate  func() error

	writeMu sync.Mutex
	command sync.Mutex

	responseMu sync.Mutex
	expected   string
	response   chan error

	done      chan struct{}
	closeOnce sync.Once
}

func newCapturePrivacySession(
	connection net.Conn,
	generation string,
	epoch string,
	timeout time.Duration,
	terminate func() error,
) *capturePrivacySession {
	session := &capturePrivacySession{
		connection: connection,
		generation: generation,
		epoch:      epoch,
		timeout:    timeout,
		terminate:  terminate,
		done:       make(chan struct{}),
	}
	go session.readResponses()
	return session
}

func (session *capturePrivacySession) readResponses() {
	reader := bufio.NewReaderSize(session.connection, maxCapturePrivacyLine+1)
	for {
		line, err := readBoundedPrivacyLine(reader)
		if err != nil {
			session.Close()
			return
		}
		session.responseMu.Lock()
		expected := session.expected
		response := session.response
		if response == nil || line != expected {
			session.responseMu.Unlock()
			session.Close()
			return
		}
		session.expected = ""
		session.response = nil
		session.responseMu.Unlock()
		response <- nil
	}
}

func (session *capturePrivacySession) Fence(ctx context.Context) error {
	return session.exchange(ctx, "BLOCK\n", "BLOCKED")
}

func (session *capturePrivacySession) Drain(ctx context.Context) error {
	return session.exchange(ctx, "DRAIN\n", "DRAINED")
}

func (session *capturePrivacySession) State(
	ctx context.Context,
	muted bool,
) error {
	state := "UNMUTED"
	if muted {
		state = "MUTED"
	}
	session.command.Lock()
	defer session.command.Unlock()
	if !session.Live() {
		return errors.New("capture privacy authority connection is closed")
	}
	return session.write(
		ctx,
		fmt.Sprintf("STATE %s %s\n", session.epoch, state),
	)
}

func (session *capturePrivacySession) Allow(ctx context.Context) error {
	return session.exchange(
		ctx,
		fmt.Sprintf("ALLOW %s\n", session.epoch),
		"ALLOWED",
	)
}

func (session *capturePrivacySession) exchange(
	ctx context.Context,
	request string,
	expected string,
) error {
	session.command.Lock()
	defer session.command.Unlock()
	if !session.Live() {
		return errors.New("capture privacy authority connection is closed")
	}
	response := make(chan error, 1)
	session.responseMu.Lock()
	if session.response != nil {
		session.responseMu.Unlock()
		return errors.New("capture privacy command is already outstanding")
	}
	session.expected = expected
	session.response = response
	session.responseMu.Unlock()

	if err := session.write(ctx, request); err != nil {
		session.clearExpected(response)
		session.Close()
		return err
	}
	timer := time.NewTimer(session.timeout)
	defer timer.Stop()
	select {
	case err := <-response:
		return err
	case <-session.done:
		session.clearExpected(response)
		return errors.New("capture privacy authority connection was lost")
	case <-ctx.Done():
		session.clearExpected(response)
		session.Close()
		return ctx.Err()
	case <-timer.C:
		session.clearExpected(response)
		session.Close()
		return errors.New("capture privacy fence timed out")
	}
}

func (session *capturePrivacySession) clearExpected(response chan error) {
	session.responseMu.Lock()
	if session.response == response {
		session.expected = ""
		session.response = nil
	}
	session.responseMu.Unlock()
}

func (session *capturePrivacySession) write(
	ctx context.Context,
	content string,
) error {
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(session.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok &&
		contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := session.connection.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("set capture privacy deadline: %w", err)
	}
	err := writeAllString(session.connection, content)
	clearErr := session.connection.SetWriteDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("write capture privacy command: %w", err)
	}
	if clearErr != nil {
		return fmt.Errorf("clear capture privacy deadline: %w", clearErr)
	}
	return nil
}

func (session *capturePrivacySession) Live() bool {
	select {
	case <-session.done:
		return false
	default:
		return true
	}
}

func (session *capturePrivacySession) TerminateVerified() error {
	if session.terminate == nil {
		return errors.New("capture-owner termination is unavailable")
	}
	return session.terminate()
}

func (session *capturePrivacySession) Close() {
	session.closeOnce.Do(func() {
		close(session.done)
		_ = session.connection.Close()
	})
}

type capturePrivacyServer struct {
	socketPath     string
	epochPath      string
	pidPath        string
	executable     string
	epoch          string
	timeout        time.Duration
	privacy        *microphonePrivacyController
	listener       *net.UnixListener
	socketIdentity syscall.Stat_t
	epochIdentity  syscall.Stat_t
	lock           *os.File

	verifyPeer func(*net.UnixConn) (processGeneration, error)

	currentMu sync.Mutex
	current   *capturePrivacySession
	handlers  sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

func openCapturePrivacyServer(
	socketPath string,
	epochPath string,
	pidPath string,
	executable string,
	privacy *microphonePrivacyController,
) (*capturePrivacyServer, error) {
	listener, identity, lock, err := listenPrivateUnix(socketPath)
	if err != nil {
		return nil, err
	}
	cleanupListener := func() {
		_ = listener.Close()
		_ = removeSocketIfSame(socketPath, identity)
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}
	epoch, err := createPrivacyAuthorityEpoch(epochPath)
	if err != nil {
		cleanupListener()
		return nil, err
	}
	epochInfo, err := os.Lstat(epochPath)
	if err != nil {
		cleanupListener()
		return nil, fmt.Errorf("inspect privacy authority epoch: %w", err)
	}
	epochIdentity, ok := epochInfo.Sys().(*syscall.Stat_t)
	if !ok {
		cleanupListener()
		return nil, errors.New("privacy authority epoch identity is unavailable")
	}
	server := &capturePrivacyServer{
		socketPath:     socketPath,
		epochPath:      epochPath,
		pidPath:        pidPath,
		executable:     executable,
		epoch:          epoch,
		timeout:        capturePrivacyTimeout,
		privacy:        privacy,
		listener:       listener,
		socketIdentity: identity,
		epochIdentity:  *epochIdentity,
		lock:           lock,
	}
	server.verifyPeer = server.authenticatePeer
	return server, nil
}

func (server *capturePrivacyServer) Run(ctx context.Context) (runErr error) {
	defer func() {
		closeErr := server.Close()
		server.handlers.Wait()
		if closeErr != nil {
			if runErr == nil {
				runErr = closeErr
			} else {
				runErr = fmt.Errorf("%v; cleanup capture privacy owner: %w", runErr, closeErr)
			}
		}
	}()
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = server.listener.Close()
		case <-stopped:
		}
	}()
	defer close(stopped)
	for {
		connection, err := server.listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept capture privacy owner: %w", err)
		}
		server.handlers.Add(1)
		go func() {
			defer server.handlers.Done()
			server.handleConnection(ctx, connection)
		}()
	}
}

func (server *capturePrivacyServer) handleConnection(
	ctx context.Context,
	connection *net.UnixConn,
) {
	defer connection.Close()
	generation, err := server.verifyPeer(connection)
	if err != nil {
		_ = writePrivacyError(connection, server.timeout, err)
		return
	}
	if err := connection.SetReadDeadline(
		time.Now().Add(server.timeout),
	); err != nil {
		return
	}
	reader := bufio.NewReaderSize(connection, maxCapturePrivacyLine+1)
	line, err := readBoundedPrivacyLine(reader)
	_ = connection.SetReadDeadline(time.Time{})
	if err != nil {
		_ = writePrivacyError(connection, server.timeout, err)
		return
	}
	captureGeneration, err := parseCaptureHello(line)
	if err != nil {
		_ = writePrivacyError(
			connection,
			server.timeout,
			err,
		)
		return
	}
	session := newCapturePrivacySession(
		connection,
		captureGeneration,
		server.epoch,
		server.timeout,
		func() error {
			return terminateVerifiedProcess(
				server.pidPath,
				server.executable,
				generation,
			)
		},
	)
	if !server.reserve(session) {
		session.Close()
		return
	}
	defer func() {
		server.release(session)
		server.privacy.DetachCaptureAuthority(session)
		session.Close()
	}()
	_, err = server.privacy.Synchronize(ctx, session)
	if err != nil {
		session.Close()
		return
	}
	select {
	case <-ctx.Done():
	case <-session.done:
	}
}

func parseCaptureHello(line string) (string, error) {
	fields := strings.Split(line, " ")
	if len(fields) != 2 || fields[0] != "HELLO" ||
		!captureGenerationPattern.MatchString(fields[1]) {
		return "", errors.New("invalid capture privacy request")
	}
	return fields[1], nil
}

func (server *capturePrivacyServer) reserve(
	session *capturePrivacySession,
) bool {
	server.currentMu.Lock()
	defer server.currentMu.Unlock()
	if server.current != nil && server.current.Live() {
		return false
	}
	server.current = session
	return true
}

func (server *capturePrivacyServer) release(
	session *capturePrivacySession,
) {
	server.currentMu.Lock()
	if server.current == session {
		server.current = nil
	}
	server.currentMu.Unlock()
}

func (server *capturePrivacyServer) authenticatePeer(
	connection *net.UnixConn,
) (processGeneration, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return processGeneration{}, fmt.Errorf(
			"inspect capture privacy peer: %w",
			err,
		)
	}
	var credentials *syscall.Ucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, socketErr = syscall.GetsockoptUcred(
			int(fd),
			syscall.SOL_SOCKET,
			syscall.SO_PEERCRED,
		)
	}); err != nil {
		return processGeneration{}, fmt.Errorf(
			"inspect capture privacy peer: %w",
			err,
		)
	}
	if socketErr != nil {
		return processGeneration{}, fmt.Errorf(
			"inspect capture privacy credentials: %w",
			socketErr,
		)
	}
	return authenticateCapturePeer(
		credentials,
		func(pid int) (processGeneration, error) {
			return verifyProcessGeneration(
				server.pidPath,
				server.executable,
				pid,
				0,
			)
		},
	)
}

func authenticateCapturePeer(
	credentials *syscall.Ucred,
	verify func(int) (processGeneration, error),
) (processGeneration, error) {
	if credentials == nil || credentials.Uid != 0 || credentials.Pid < 2 {
		return processGeneration{}, errors.New(
			"capture privacy peer is not a root process",
		)
	}
	return verify(int(credentials.Pid))
}

func (server *capturePrivacyServer) Close() error {
	server.closeOnce.Do(func() {
		_ = server.listener.Close()
		server.currentMu.Lock()
		if server.current != nil {
			server.current.Close()
		}
		server.currentMu.Unlock()
		socketErr := removeSocketIfSame(
			server.socketPath,
			server.socketIdentity,
		)
		epochErr := removeFileIfSame(server.epochPath, server.epochIdentity)
		if server.lock != nil {
			_ = syscall.Flock(int(server.lock.Fd()), syscall.LOCK_UN)
			_ = server.lock.Close()
		}
		if socketErr != nil {
			server.closeErr = socketErr
		} else {
			server.closeErr = epochErr
		}
	})
	return server.closeErr
}

func createPrivacyAuthorityEpoch(path string) (string, error) {
	random := make([]byte, privacyEpochBytes)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return "", fmt.Errorf("generate privacy authority epoch: %w", err)
	}
	epoch := hex.EncodeToString(random)
	if err := persistPrivateState(path, []byte(epoch+"\n")); err != nil {
		return "", fmt.Errorf("persist privacy authority epoch: %w", err)
	}
	return epoch, nil
}

func readBoundedPrivacyLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxCapturePrivacyLine {
			return "", errors.New("capture privacy record is too large")
		}
		return "", fmt.Errorf("read capture privacy record: %w", err)
	}
	if len(line) > maxCapturePrivacyLine {
		return "", errors.New("capture privacy record is too large")
	}
	line = strings.TrimSuffix(line, "\n")
	if strings.ContainsRune(line, '\r') || strings.ContainsRune(line, '\x00') {
		return "", errors.New("capture privacy record contains invalid bytes")
	}
	return line, nil
}

func writePrivacyError(
	connection net.Conn,
	timeout time.Duration,
	cause error,
) error {
	_ = connection.SetWriteDeadline(time.Now().Add(timeout))
	return writeAllString(connection, "ERROR "+cause.Error()+"\n")
}

func writeAllString(writer io.Writer, content string) error {
	for len(content) > 0 {
		written, err := io.WriteString(writer, content)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}

func listenPrivateUnix(
	path string,
) (*net.UnixListener, syscall.Stat_t, *os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, syscall.Stat_t{}, nil, errors.New(
			"capture privacy socket path must be absolute",
		)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return nil, syscall.Stat_t{}, nil, fmt.Errorf(
			"inspect capture privacy socket directory: %w",
			err,
		)
	}
	parentStat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || !parent.IsDir() || parent.Mode().Perm()&0o022 != 0 {
		return nil, syscall.Stat_t{}, nil, errors.New(
			"capture privacy socket directory is not private",
		)
	}
	lock, err := openSocketLifecycleLock(path + ".lock")
	if err != nil {
		return nil, syscall.Stat_t{}, nil, err
	}
	cleanupLock := func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}
	if err := recoverStaleUnixSocket(path); err != nil {
		cleanupLock()
		return nil, syscall.Stat_t{}, nil, err
	}
	address := &net.UnixAddr{Name: path, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		cleanupLock()
		return nil, syscall.Stat_t{}, nil, fmt.Errorf(
			"listen on capture privacy socket: %w",
			err,
		)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		cleanupLock()
		return nil, syscall.Stat_t{}, nil, fmt.Errorf(
			"protect capture privacy socket: %w",
			err,
		)
	}
	info, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()
		cleanupLock()
		return nil, syscall.Stat_t{}, nil, err
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 ||
		identity.Uid != parentStat.Uid {
		_ = listener.Close()
		_ = os.Remove(path)
		cleanupLock()
		return nil, syscall.Stat_t{}, nil, errors.New(
			"capture privacy socket ownership is invalid",
		)
	}
	return listener, *identity, lock, nil
}

func openSocketLifecycleLock(path string) (*os.File, error) {
	fd, err := syscall.Open(
		path,
		syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open capture privacy lifecycle lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), path)
	info, statErr := lock.Stat()
	if statErr != nil {
		_ = lock.Close()
		return nil, fmt.Errorf(
			"inspect capture privacy lifecycle lock: %w",
			statErr,
		)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != uint32(os.Geteuid()) {
		_ = lock.Close()
		return nil, errors.New("capture privacy lifecycle lock is not owned")
	}
	if err := lock.Chmod(0o600); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("protect capture privacy lifecycle lock: %w", err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errors.New("capture privacy owner is already running")
	}
	return lock, nil
}

func recoverStaleUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect capture privacy socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("capture privacy path is not a Unix socket")
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("capture privacy socket identity is unavailable")
	}
	connection, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("capture privacy socket is already active")
	}
	var operationError *net.OpError
	if !errors.As(dialErr, &operationError) ||
		!errors.Is(operationError.Err, syscall.ECONNREFUSED) {
		return fmt.Errorf("probe capture privacy socket: %w", dialErr)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reinspect capture privacy socket: %w", err)
	}
	currentIdentity, ok := current.Sys().(*syscall.Stat_t)
	if !ok || currentIdentity.Dev != identity.Dev ||
		currentIdentity.Ino != identity.Ino {
		return errors.New("capture privacy socket changed during recovery")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale capture privacy socket: %w", err)
	}
	return nil
}

func removeSocketIfSame(path string, expected syscall.Stat_t) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect capture privacy socket during cleanup: %w", err)
	}
	current, ok := info.Sys().(*syscall.Stat_t)
	if !ok || current.Dev != expected.Dev || current.Ino != expected.Ino {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove capture privacy socket: %w", err)
	}
	return nil
}

func removeFileIfSame(path string, expected syscall.Stat_t) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect privacy authority epoch during cleanup: %w", err)
	}
	current, ok := info.Sys().(*syscall.Stat_t)
	if !ok || current.Dev != expected.Dev || current.Ino != expected.Ino ||
		!info.Mode().IsRegular() {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove privacy authority epoch: %w", err)
	}
	return nil
}
