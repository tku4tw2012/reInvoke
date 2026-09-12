// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type mount struct {
	Device, Point, Options, FSType, Source string
}

func mounts(data string) []mount {
	var result []mount
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		for i := 6; i+3 < len(fields); i++ {
			if fields[i] == "-" {
				result = append(result, mount{fields[2], fields[4], fields[5], fields[i+1], fields[i+2]})
				break
			}
		}
	}
	return result
}

func read(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 8192))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func classify(ms []mount, sys string) string {
	var source *mount
	for i := range ms {
		if ms[i].Point == "/" && source == nil {
			source = &ms[i]
		}
		if ms[i].Point == "/nand-source" {
			source = &ms[i]
			break
		}
	}
	if source == nil {
		return "unknown-unattested"
	}
	if source.FSType != "squashfs" {
		return "ram-or-other-unattested"
	}
	if !strings.Contains(","+source.Options+",", ",ro,") {
		return "source-not-readonly-unattested"
	}
	if !strings.HasPrefix(source.Device, "31:") {
		return "non-nand-squashfs-unattested"
	}
	name := ""
	for _, line := range strings.Split(read(filepath.Join(sys, "dev/block", source.Device, "uevent")), "\n") {
		if strings.HasPrefix(line, "DEVNAME=mtdblock") {
			name = strings.TrimPrefix(line, "DEVNAME=mtdblock")
		}
	}
	if _, err := strconv.ParseUint(name, 10, 32); err != nil {
		return "unknown-mtd-squashfs-unattested"
	}
	if read(filepath.Join(sys, "class/mtd", "mtd"+name, "type")) != "nand" {
		return "unknown-mtd-squashfs-unattested"
	}
	return "nand-squashfs-observed-unattested"
}

type service struct {
	Name       string `json:"name"`
	PID        int    `json:"pid"`
	Process    string `json:"process_executable,omitempty"`
	Present    bool   `json:"process_present"`
	Generation string `json:"generation,omitempty"`
}

type status struct {
	BuildID             string            `json:"build_id"`
	Kernel              string            `json:"actual_kernel"`
	EntryKernel         string            `json:"bootstrap_entry_kernel"`
	OriginEvidence      string            `json:"origin_evidence"`
	ExternalAttestation string            `json:"external_attestation"`
	Phase               string            `json:"phase"`
	Failures            map[string]string `json:"failures"`
	Services            []service         `json:"services"`
	NetworkPolicy       string            `json:"network_policy"`
	Acceptance          string            `json:"acceptance"`
	USB                 usbStatus         `json:"usb"`
	PTY                 nodeStatus        `json:"pty"`
	DevptsListed        bool              `json:"devpts_listed"`
	SSHFirewall         string            `json:"ssh_firewall"`
	KernelConfig        map[string]string `json:"kernel_reported_config"`
}

type nodeStatus struct {
	State string `json:"state"`
	Major uint64 `json:"major,omitempty"`
	Minor uint64 `json:"minor,omitempty"`
	Mode  string `json:"mode,omitempty"`
	Owner string `json:"owner,omitempty"`
}

func node(path string) nodeStatus {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nodeStatus{State: "absent"}
		}
		return nodeStatus{State: "unreadable"}
	}
	s := nodeStatus{State: "not-character-device", Mode: fmt.Sprintf("%04o", info.Mode().Perm())}
	if info.Mode()&os.ModeCharDevice == 0 {
		return s
	}
	raw, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		s.State = "metadata-unavailable"
		return s
	}
	dev := uint64(raw.Rdev)
	s.State = "character-device-observed"
	s.Major = (dev >> 8 & 0xfff) | (dev >> 32 & 0xfffff000)
	s.Minor = (dev & 0xff) | (dev >> 12 & 0xffffff00)
	s.Owner = fmt.Sprintf("%d:%d", raw.Uid, raw.Gid)
	return s
}

type usbStatus struct {
	Gadget          string     `json:"gadget"`
	Enable          string     `json:"requested_enable"`
	State           string     `json:"kernel_state"`
	Functions       string     `json:"functions"`
	LegacyDevice    string     `json:"kernel_misc_device"`
	Node            nodeStatus `json:"node"`
	DaemonUSBFD     string     `json:"daemon_usb_fd"`
	SupervisorState string     `json:"supervisor_state"`
	LastFailure     string     `json:"recent_failure"`
	FailureUptime   string     `json:"failure_uptime_seconds,omitempty"`
}

func token(value string, allowed ...string) string {
	for _, known := range allowed {
		if value == known {
			return value
		}
	}
	return "unknown-or-unavailable"
}

func usbFD(root string, pid int) string {
	if pid < 2 {
		return "daemon-not-observed"
	}
	expected := node(filepath.Join(root, "/dev/android_adb"))
	if expected.State != "character-device-observed" {
		return "device-unavailable"
	}
	if read(filepath.Join(root, "/sys/class/misc/android_adb/dev")) !=
		fmt.Sprintf("%d:%d", expected.Major, expected.Minor) {
		return "device-number-mismatch"
	}
	dir, err := os.Open(filepath.Join(root, fmt.Sprintf("/proc/%d/fd", pid)))
	if err != nil {
		return "unreadable-or-exited"
	}
	defer dir.Close()
	names, err := dir.Readdirnames(128)
	if err != nil && err != io.EOF {
		return "unreadable"
	}
	for _, name := range names {
		observed := node(filepath.Join(dir.Name(), name))
		if observed.State == "absent" || observed.State == "unreadable" ||
			observed.State == "metadata-unavailable" {
			return "unreadable-or-changing"
		}
		if observed.State == "character-device-observed" &&
			observed.Major == expected.Major && observed.Minor == expected.Minor {
			return "open-observed"
		}
	}
	if len(names) >= 128 {
		return "scan-limit-reached"
	}
	return "not-open-observed"
}

func kernelConfig(root string) map[string]string {
	result := map[string]string{}
	for _, name := range []string{"CONFIG_USB_PHY", "CONFIG_BERLIN_USBPHY",
		"CONFIG_USB_GADGET", "CONFIG_USB_LIBCOMPOSITE", "CONFIG_USB_G_ANDROID",
		"CONFIG_USB_ANDROID", "CONFIG_USB_ANDROID_ADB", "CONFIG_USB_U_SERIAL",
		"CONFIG_USB_F_ACM", "CONFIG_USB_FUNCTIONFS", "CONFIG_USB_MV_UDC",
		"CONFIG_UNIX98_PTYS", "CONFIG_DEVPTS_FS", "CONFIG_DEVTMPFS"} {
		result[name] = "not-reported"
	}
	file, err := os.Open(filepath.Join(root, "/proc/config.gz"))
	if err != nil {
		return result
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return result
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, 512*1024))
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		for name := range result {
			if line == "# "+name+" is not set" {
				result[name] = "n"
			}
			if line == name+"=y" || line == name+"=m" {
				result[name] = strings.TrimPrefix(line, name+"=")
			}
		}
	}
	return result
}

func collect(root string) status {
	at := func(path string) string { return filepath.Join(root, path) }
	s := status{
		BuildID:             read(at("/etc/nand-pilot/build-id")),
		Kernel:              read(at("/proc/sys/kernel/osrelease")),
		EntryKernel:         read(at("/run/nand-pilot/entry-kernel")),
		ExternalAttestation: "required: owner power cycle without RAM download plus host observations/readback",
		Phase:               read(at("/run/nand-pilot/phase")),
		Failures:            map[string]string{},
		Services:            []service{},
		NetworkPolicy:       "STA/uAP provisioning; credentials and bonds are RAM-only; no automatic station credentials",
		Acceptance:          "not established by status: process presence is not functional health",
		PTY:                 node(at("/dev/ptmx")),
		DevptsListed:        strings.Contains(read(at("/proc/filesystems")), "devpts"),
		SSHFirewall:         token(read(at("/run/nand-pilot/ssh-firewall")), "installed"),
		KernelConfig:        kernelConfig(root),
	}
	s.OriginEvidence = classify(mounts(read(at("/proc/self/mountinfo"))), at("/sys"))
	for _, name := range []string{"bootstrap", "kernel", "wifi", "bluetooth", "runtime", "pty", "usb", "usb-owner", "ssh"} {
		if read(at("/run/nand-pilot/failure-"+name)) != "" {
			s.Failures[name] = "failure-record-present; contents withheld"
		}
	}
	for _, dir := range []string{"/run/nand-pilot", "/run/reinvoke"} {
		for _, name := range []string{"adbd", "adbd-supervisor", "sshd", "sshd-native",
			"sshd-supervisor", "bluetoothd", "bluealsa", "mcu-interface", "dsp-interface",
			"mic-capture", "provision-windowd", "bonefish", "syslogd", "pairing-agent"} {
			if read(at("/run/nand-pilot/failure-service-"+name)) != "" {
				s.Failures["service-"+name] = "failure-record-present; contents withheld"
			}
			path := at(dir + "/" + name + ".pid")
			pid, err := strconv.Atoi(read(path))
			if err != nil || pid <= 0 {
				continue
			}
			proc := at(fmt.Sprintf("/proc/%d", pid))
			exe, _ := os.Readlink(proc + "/exe")
			_, err = os.Stat(proc)
			s.Services = append(s.Services, service{
				Name: strings.TrimSuffix(filepath.Base(path), ".pid"), PID: pid,
				Process: filepath.Base(exe), Present: err == nil,
			})
		}
	}
	gadget := "/sys/class/android_usb/android0"
	pid, _ := strconv.Atoi(read(at("/run/nand-pilot/adbd.pid")))
	misc := read(at("/sys/class/misc/android_adb/dev"))
	fields := strings.Split(misc, ":")
	if len(fields) != 2 {
		misc = "not-reported"
	} else {
		for _, value := range fields {
			if _, err := strconv.ParseUint(value, 10, 20); err != nil {
				misc = "invalid-or-unreadable"
			}
		}
	}
	s.USB = usbStatus{
		Gadget:       "absent-or-unreadable",
		Enable:       token(read(at(gadget+"/enable")), "0", "1"),
		State:        token(read(at(gadget+"/state")), "DISCONNECTED", "CONNECTED", "CONFIGURED"),
		Functions:    token(read(at(gadget+"/functions")), "adb", "acm,adb"),
		LegacyDevice: misc, Node: node(at("/dev/android_adb")), DaemonUSBFD: usbFD(root, pid),
		SupervisorState: token(read(at("/run/nand-pilot/adb-transport")), "pending", "open", "stopped",
			"gadget-absent", "gadget-config-failed", "node-invalid-or-absent", "fd-unreadable", "usb-fd-not-open", "retry-budget-exhausted"),
		LastFailure: token(read(at("/run/nand-pilot/usb-last-failure")), "gadget-absent",
			"gadget-config-failed", "node-invalid-or-absent", "fd-unreadable", "usb-fd-not-open", "retry-budget-exhausted"),
	}
	if value := read(at("/run/nand-pilot/usb-failure-uptime")); value != "" {
		if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
			s.USB.FailureUptime = value
		}
	}
	if info, err := os.Stat(at(gadget)); err == nil && info.IsDir() {
		s.USB.Gadget = "present"
	}
	if s.Phase == "" {
		s.Phase = "no-bootstrap-evidence"
	}
	return s
}

func main() {
	asJSON := flag.Bool("json", false, "emit diagnostic JSON")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "only --json is accepted; no commands, paths or PIDs")
		os.Exit(2)
	}
	s := collect("/")
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(s); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	fmt.Printf("Build: %s\nKernel: %s\nSource: %s\nPhase: %s\n",
		s.BuildID, s.Kernel, s.OriginEvidence, s.Phase)
	fmt.Printf("USB: gadget=%s requested=%s kernel=%s fd=%s supervisor=%s recent=%s\nPTY: %s %d:%d mode=%s devpts=%t\nSSH firewall: %s\n",
		s.USB.Gadget, s.USB.Enable, s.USB.State, s.USB.DaemonUSBFD, s.USB.SupervisorState, s.USB.LastFailure,
		s.PTY.State, s.PTY.Major, s.PTY.Minor, s.PTY.Mode, s.DevptsListed, s.SSHFirewall)
	for name, detail := range s.Failures {
		fmt.Printf("FAIL %s: %s\n", name, detail)
	}
	for _, svc := range s.Services {
		fmt.Printf("Service %s pid=%d present=%t exe=%s\n", svc.Name, svc.PID, svc.Present, svc.Process)
	}
	fmt.Printf("Network: %s\nAttestation: %s\nAcceptance: %s\n", s.NetworkPolicy, s.ExternalAttestation, s.Acceptance)
}
