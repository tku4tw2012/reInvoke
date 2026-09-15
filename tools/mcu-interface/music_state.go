// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func validateMusicStateDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("music volume state must be an absolute RAM path")
	}
	directory := filepath.Dir(path)
	for ancestor := directory; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Lstat(ancestor)
		if err != nil {
			return errors.New("music volume state directory unavailable")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return errors.New("music volume state directory unsafe")
		}
		if ancestor == "/" {
			break
		}
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(directory, &fs); err != nil {
		return errors.New("music volume state filesystem unavailable")
	}
	kind := uint64(fs.Type) & 0xffffffff
	if kind != 0x01021994 && kind != 0x858458f6 {
		return errors.New("music volume state must reside in RAM")
	}
	return nil
}

func readMusicVolume(path string) (int, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultVolume, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4 {
		return 0, errors.New("music volume state invalid")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || info.Mode().Perm() != 0600 {
		return 0, errors.New("music volume state unsafe")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, errors.New("music volume state unreadable")
	}
	value, err := strconv.Atoi(strings.TrimSuffix(string(content), "\n"))
	if err != nil || value < 0 || value > 100 || string(content) != strconv.Itoa(value)+"\n" {
		return 0, errors.New("music volume state invalid")
	}
	return value, nil
}

func (controller *dspVolumeController) rememberMusicVolume(percent int) error {
	if controller.musicStatePath == "" {
		return nil
	}
	if err := persistPrivateState(controller.musicStatePath, []byte(strconv.Itoa(percent)+"\n")); err != nil {
		return errors.New("music volume RAM state write failed")
	}
	controller.savedVolume = percent
	controller.hasSavedVolume = true
	return nil
}
