// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

func prepareRuntimeDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return errors.New("runtime directory must be a clean absolute path")
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("clear runtime directory: %w", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	return os.Chmod(path, 0o700)
}

func validateRootExecutable(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o002 != 0 ||
		(stat.Gid != 0 && info.Mode().Perm()&0o020 != 0) ||
		info.Mode().Perm()&0o111 == 0 {
		return errors.New("executable is not root-controlled")
	}
	return nil
}

func validateRootLibraryPath(value string) error {
	if value == "" {
		return errors.New("library path is empty")
	}
	for _, path := range filepath.SplitList(value) {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || !info.IsDir() ||
			info.Mode()&os.ModeSymlink != 0 ||
			info.Mode().Perm()&0o002 != 0 ||
			(stat.Gid != 0 && info.Mode().Perm()&0o020 != 0) {
			return errors.New("library path is not root-controlled")
		}
	}
	return nil
}

func runAudioServer(
	ctx context.Context,
	socketPath string,
	hub *clientHub,
	ready chan<- struct{},
) error {
	listener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: socketPath, Net: "unix"},
	)
	if err != nil {
		return fmt.Errorf("listen on audio socket: %w", err)
	}
	listener.SetUnlinkOnClose(true)
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return fmt.Errorf("protect audio socket: %w", err)
	}
	close(ready)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept audio client: %w", err)
		}
		uid, err := unixPeerUID(connection)
		if err != nil || uid != 0 {
			_ = connection.Close()
			continue
		}
		hub.add(connection)
	}
}

func unixPeerUID(connection *net.UnixConn) (uint32, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return 0, err
	}
	var (
		credential *syscall.Ucred
		controlErr error
	)
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = syscall.GetsockoptUcred(
			int(fd),
			syscall.SOL_SOCKET,
			syscall.SO_PEERCRED,
		)
	}); err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, controlErr
	}
	if credential == nil {
		return 0, errors.New("peer credentials unavailable")
	}
	return credential.Uid, nil
}
