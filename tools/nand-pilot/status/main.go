// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	data, err := os.ReadFile(path)
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
	Release             string            `json:"release"`
	Kernel              string            `json:"actual_kernel"`
	BootID              string            `json:"boot_id"`
	CommandLine         string            `json:"actual_cmdline"`
	MountInfo           string            `json:"actual_mountinfo"`
	EntryMountInfo      string            `json:"bootstrap_entry_mountinfo"`
	EntryKernel         string            `json:"bootstrap_entry_kernel"`
	OriginEvidence      string            `json:"origin_evidence"`
	ExternalAttestation string            `json:"external_attestation"`
	Phase               string            `json:"phase"`
	Failures            map[string]string `json:"failures"`
	Services            []service         `json:"services"`
	NetworkPolicy       string            `json:"network_policy"`
	Acceptance          string            `json:"acceptance"`
}

func collect(root string) status {
	at := func(path string) string { return filepath.Join(root, path) }
	s := status{
		BuildID:             read(at("/etc/nand-pilot/build-id")),
		Release:             read(at("/etc/reinvoke-release")),
		Kernel:              read(at("/proc/sys/kernel/osrelease")),
		BootID:              read(at("/proc/sys/kernel/random/boot_id")),
		CommandLine:         read(at("/proc/cmdline")),
		MountInfo:           read(at("/proc/self/mountinfo")),
		EntryMountInfo:      read(at("/run/nand-pilot/entry-mountinfo")),
		EntryKernel:         read(at("/run/nand-pilot/entry-kernel")),
		ExternalAttestation: "required: owner power cycle without RAM download plus host observations/readback",
		Phase:               read(at("/run/nand-pilot/phase")),
		Failures:            map[string]string{},
		Services:            []service{},
		NetworkPolicy:       "STA/uAP provisioning; credentials and bonds are RAM-only; no automatic station credentials",
		Acceptance:          "not established by status: process presence is not functional health",
	}
	s.OriginEvidence = classify(mounts(s.MountInfo), at("/sys"))
	failurePaths, _ := filepath.Glob(at("/run/nand-pilot/failure-*"))
	for _, path := range failurePaths {
		s.Failures[strings.TrimPrefix(filepath.Base(path), "failure-")] = read(path)
	}
	for _, dir := range []string{"/run/nand-pilot", "/run/reinvoke"} {
		paths, _ := filepath.Glob(at(dir + "/*.pid"))
		for _, path := range paths {
			pid, err := strconv.Atoi(read(path))
			if err != nil || pid <= 0 {
				continue
			}
			proc := at(fmt.Sprintf("/proc/%d", pid))
			exe, _ := os.Readlink(proc + "/exe")
			_, err = os.Stat(proc)
			s.Services = append(s.Services, service{
				Name: strings.TrimSuffix(filepath.Base(path), ".pid"), PID: pid,
				Process: exe, Present: err == nil,
			})
		}
	}
	if s.Phase == "" {
		s.Phase = "no-bootstrap-evidence"
	}
	return s
}

func main() {
	asJSON := flag.Bool("json", false, "emit diagnostic JSON")
	flag.Parse()
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
	for name, detail := range s.Failures {
		fmt.Printf("FAIL %s: %s\n", name, detail)
	}
	for _, svc := range s.Services {
		fmt.Printf("Service %s pid=%d present=%t exe=%s\n", svc.Name, svc.PID, svc.Present, svc.Process)
	}
	fmt.Printf("Network: %s\nAttestation: %s\nAcceptance: %s\n", s.NetworkPolicy, s.ExternalAttestation, s.Acceptance)
}
