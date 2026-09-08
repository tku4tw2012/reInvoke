// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const maximumStateBytes = 32

func readMicrophoneMuted(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return true, err
	}
	defer file.Close()
	content := make([]byte, maximumStateBytes+1)
	count, err := file.Read(content)
	if err != nil {
		return true, err
	}
	if count > maximumStateBytes {
		return true, errors.New("microphone state is too large")
	}
	switch strings.TrimSpace(string(content[:count])) {
	case "muted":
		return true, nil
	case "unmuted":
		return false, nil
	default:
		return true, errors.New("microphone state is invalid")
	}
}

type processGeneration struct {
	pid       int
	startTime string
	socketDev uint64
	socketIno uint64
}

type processIdentity struct {
	pid       int
	startTime string
}

func readProcessIdentity(
	pidPath string,
	executable string,
) (processIdentity, error) {
	pidContent, err := os.ReadFile(pidPath)
	if err != nil {
		return processIdentity{}, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidContent)))
	if err != nil || pid < 1 {
		return processIdentity{}, errors.New("service PID is invalid")
	}
	actualExecutable, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return processIdentity{}, err
	}
	if actualExecutable != executable {
		return processIdentity{}, errors.New("service executable does not match")
	}
	statContent, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return processIdentity{}, err
	}
	closeParen := strings.LastIndexByte(string(statContent), ')')
	if closeParen < 0 {
		return processIdentity{}, errors.New("service process stat is invalid")
	}
	fields := strings.Fields(string(statContent)[closeParen+1:])
	if len(fields) < 20 {
		return processIdentity{}, errors.New("service process stat is incomplete")
	}
	return processIdentity{pid: pid, startTime: fields[19]}, nil
}

func readDSPGeneration(
	pidPath string,
	executable string,
	socketPath string,
) (processGeneration, error) {
	process, err := readProcessIdentity(pidPath, executable)
	if err != nil {
		return processGeneration{}, err
	}
	info, err := os.Lstat(socketPath)
	if err != nil {
		return processGeneration{}, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return processGeneration{}, errors.New("DSP microphone path is not a socket")
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return processGeneration{}, errors.New("DSP socket metadata unavailable")
	}
	return processGeneration{
		pid:       process.pid,
		startTime: process.startTime,
		socketDev: uint64(status.Dev),
		socketIno: uint64(status.Ino),
	}, nil
}
