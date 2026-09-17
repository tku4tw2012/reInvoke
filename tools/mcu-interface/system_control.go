// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// System control the donor kept in system-manager.
//
// This runtime has no system-manager. mcu-interface already owns the hardware
// a reboot acts on and already holds a WAMP session, so the two system
// procedures the donor exposed live here rather than in a service that would
// exist only to hold them.

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// timezoneLinkPath is what libc reads. Writing the link rather than copying the
// zone file keeps the name recoverable, which is what a later getter needs.
var timezoneLinkPath = "/etc/localtime"

// zoneinfoRoot holds the compiled zone files.
var zoneinfoRoot = "/usr/share/zoneinfo"

// timezoneStatePath records the chosen name across restarts. /etc is read-only
// on this platform, so the name is kept where the rest of our state lives.
var timezoneStatePath = "/persist/reinvoke/timezone"

// applyTimezone points /etc/localtime at the requested zone.
//
// The name is validated against the installed zone files rather than trusted,
// and rejected if it escapes the zoneinfo tree: this procedure is reachable
// from the WAMP router, so "America/New_York" and "../../etc/shadow" arrive by
// the same path.
func applyTimezone(zone string) error {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		return errors.New("timezone is required")
	}
	if strings.HasPrefix(zone, "/") || strings.Contains(zone, "..") {
		return fmt.Errorf("timezone %q is not a zone name", zone)
	}
	target := filepath.Join(zoneinfoRoot, filepath.Clean(zone))
	if !strings.HasPrefix(target, zoneinfoRoot+string(os.PathSeparator)) {
		return fmt.Errorf("timezone %q escapes the zone directory", zone)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("unknown timezone %q", zone)
	}
	if info.IsDir() {
		return fmt.Errorf("timezone %q is a region, not a zone", zone)
	}

	// Replace by rename so a reader never observes a missing link.
	temporary := timezoneLinkPath + ".new"
	_ = os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return fmt.Errorf("stage timezone link: %w", err)
	}
	if err := os.Rename(temporary, timezoneLinkPath); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("install timezone link: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(timezoneStatePath), 0o755); err == nil {
		_ = os.WriteFile(timezoneStatePath, []byte(zone+"\n"), 0o644)
	}
	// TZ is consulted before /etc/localtime by this process's own libc.
	_ = os.Setenv("TZ", zone)
	return nil
}

// currentTimezone reports the configured zone, preferring the recorded name
// because the link target is an absolute path rather than a zone name.
func currentTimezone() string {
	if recorded, err := os.ReadFile(timezoneStatePath); err == nil {
		if name := strings.TrimSpace(string(recorded)); name != "" {
			return name
		}
	}
	target, err := os.Readlink(timezoneLinkPath)
	if err != nil {
		return ""
	}
	if strings.HasPrefix(target, zoneinfoRoot+string(os.PathSeparator)) {
		return strings.TrimPrefix(target, zoneinfoRoot+string(os.PathSeparator))
	}
	return ""
}

// rebootDelay gives the router time to deliver the reply and the event before
// the system goes down.
const rebootDelay = 500 * time.Millisecond

// rebootFunc is replaced in tests. Production performs the real reboot.
var rebootFunc = func() error {
	syscall.Sync()
	return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}

// requestReboot flushes filesystems and restarts the system.
func requestReboot(logf func(string, ...interface{})) {
	time.Sleep(rebootDelay)
	if logf != nil {
		logf("REBOOTING")
	}
	if err := rebootFunc(); err != nil && logf != nil {
		logf("REBOOT_FAILED: %v", err)
	}
}

// networkConfiguration reports the active network interface.
//
// The donor's connection-manager owned this. That service does not exist here,
// but the question it answered — what address is this speaker on — is the one
// asked when a speaker stops responding, so it is answered live rather than
// from anything cached at startup.
func networkConfiguration() map[string]interface{} {
	report := map[string]interface{}{
		"interface": "",
		"address":   "",
		"mac":       "",
		"connected": false,
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return report
	}
	for _, candidate := range interfaces {
		if candidate.Flags&net.FlagLoopback != 0 {
			continue
		}
		if candidate.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := candidate.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			network, ok := address.(*net.IPNet)
			if !ok || network.IP.To4() == nil {
				continue
			}
			ones, _ := network.Mask.Size()
			report["interface"] = candidate.Name
			report["address"] = network.IP.String()
			report["prefix"] = uint64(ones)
			report["mac"] = candidate.HardwareAddr.String()
			report["connected"] = true
			return report
		}
	}
	return report
}
