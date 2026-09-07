// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type processGeneration struct {
	pid       int
	startTime uint64
}

func readRootControlledPID(path string) (int, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return 0, errors.New("capture-owner PID file is not root-controlled")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(content) > 32 {
		return 0, errors.New("capture-owner PID file is invalid")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid < 2 {
		return 0, errors.New("capture-owner PID file is invalid")
	}
	return pid, nil
}

func processStartTime(pid int) (uint64, error) {
	content, err := os.ReadFile(
		filepath.Join("/proc", strconv.Itoa(pid), "stat"),
	)
	if err != nil {
		return 0, err
	}
	closeParen := bytes.LastIndexByte(content, ')')
	if closeParen < 0 {
		return 0, errors.New("invalid capture-owner process stat")
	}
	fields := strings.Fields(string(content[closeParen+1:]))
	if len(fields) <= 19 {
		return 0, errors.New("invalid capture-owner process stat")
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || startTime == 0 {
		return 0, errors.New("invalid capture-owner process start time")
	}
	return startTime, nil
}

func verifyProcessGeneration(
	pidPath string,
	executable string,
	expectedPID int,
	expectedStartTime uint64,
) (processGeneration, error) {
	pid, err := readRootControlledPID(pidPath)
	if err != nil {
		return processGeneration{}, fmt.Errorf("read capture-owner PID: %w", err)
	}
	if pid != expectedPID {
		return processGeneration{}, errors.New(
			"capture-owner PID does not match Unix peer",
		)
	}
	actualExecutable, err := os.Stat(
		filepath.Join("/proc", strconv.Itoa(pid), "exe"),
	)
	if err != nil {
		return processGeneration{}, fmt.Errorf(
			"inspect capture-owner executable: %w",
			err,
		)
	}
	configuredExecutable, err := os.Stat(executable)
	if err != nil {
		return processGeneration{}, fmt.Errorf(
			"inspect expected capture-owner executable: %w",
			err,
		)
	}
	if !os.SameFile(actualExecutable, configuredExecutable) {
		return processGeneration{}, errors.New(
			"capture-owner PID belongs to another executable",
		)
	}
	startTime, err := processStartTime(pid)
	if err != nil {
		return processGeneration{}, fmt.Errorf(
			"inspect capture-owner generation: %w",
			err,
		)
	}
	if expectedStartTime != 0 && startTime != expectedStartTime {
		return processGeneration{}, errors.New(
			"capture-owner process generation changed",
		)
	}
	return processGeneration{pid: pid, startTime: startTime}, nil
}

func validateRootExecutable(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("capture-owner executable path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect capture-owner executable: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 ||
		info.Mode().Perm()&0o111 == 0 {
		return errors.New("capture-owner executable is not root-controlled")
	}
	return nil
}

func terminateVerifiedProcess(
	pidPath string,
	executable string,
	generation processGeneration,
) error {
	return terminateProcessGeneration(
		generation,
		func() (processGeneration, error) {
			return verifyProcessGeneration(
				pidPath,
				executable,
				generation.pid,
				generation.startTime,
			)
		},
		syscall.Kill,
	)
}

func terminateProcessGeneration(
	generation processGeneration,
	verify func() (processGeneration, error),
	signal func(int, syscall.Signal) error,
) error {
	current, err := verify()
	if err != nil {
		return fmt.Errorf("refuse to terminate capture owner: %w", err)
	}
	if current != generation {
		return errors.New("refuse to terminate changed capture-owner generation")
	}
	if err := signal(current.pid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("terminate capture owner: %w", err)
	}
	return nil
}
