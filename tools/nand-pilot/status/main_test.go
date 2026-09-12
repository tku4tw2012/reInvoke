// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
package main

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineBuilderAndRetainedShell(t *testing.T) {
	cmd := exec.Command("node", "../test.js")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestBoundedRedactedUSBStatus(t *testing.T) {
	root, err := os.MkdirTemp(".", ".status-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	write := func(name, value string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := collect(root)
	if s.USB.DaemonUSBFD != "daemon-not-observed" || s.USB.Enable != "unknown-or-unavailable" {
		t.Fatal("missing evidence is not readiness", s.USB)
	}
	write("/run/nand-pilot/adbd.pid", "42")
	write("/run/nand-pilot/failure-runtime", "SECRET-CREDENTIALS")
	write("/proc/cmdline", "SECRET-COMMANDLINE")
	write("/sys/class/android_usb/android0/iSerial", "SECRET-SERIAL")
	write("/proc/self/mountinfo", "SECRET-PATH")
	write("/run/nand-pilot/usb-last-failure", "fd-unreadable")
	write("/run/nand-pilot/usb-failure-uptime", "12.34")
	write("/sys/class/android_usb/android0/enable", "1")
	write("/sys/class/misc/android_adb/dev", "1:5")
	if err := os.MkdirAll(filepath.Join(root, "dev"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(root, "dev/android_adb")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "/proc/42/fd")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", filepath.Join(dir, "0")); err != nil {
		t.Fatal(err)
	}
	s = collect(root)
	if s.USB.DaemonUSBFD != "not-open-observed" || s.USB.LastFailure != "fd-unreadable" {
		t.Fatal(s.USB)
	}
	if err := os.Symlink("/dev/android_adb", filepath.Join(dir, "3")); err != nil {
		t.Fatal(err)
	}
	s = collect(root)
	if s.USB.DaemonUSBFD == "open-observed" {
		t.Fatal("an ADB-looking path alone was accepted as an open device")
	}
	if err := os.Remove(filepath.Join(dir, "3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(dir, "3")); err != nil {
		t.Fatal(err)
	}
	s = collect(root)
	if s.USB.DaemonUSBFD != "open-observed" || s.USB.LegacyDevice != "1:5" {
		t.Fatal(s.USB)
	}
	write("/sys/class/misc/android_adb/dev", "1:3")
	if collect(root).USB.DaemonUSBFD != "device-number-mismatch" {
		t.Fatal("wrong device number accepted as the kernel's ADB node")
	}
	write("/sys/class/misc/android_adb/dev", "1:5")
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "SECRET") {
		t.Fatal("status leaked nonallowlisted facts")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if collect(root).USB.DaemonUSBFD != "unreadable-or-exited" {
		t.Fatal("FD error reported healthy")
	}
}

func TestReportedBerlinKernelOptions(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "proc"), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(root, "proc/config.gz"))
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	if _, err := writer.Write([]byte("CONFIG_USB_G_ANDROID=y\nCONFIG_BERLIN_USBPHY=y\n" +
		"CONFIG_USB_MV_UDC=m\n# CONFIG_USB_FUNCTIONFS is not set\nCONFIG_OTHER_PRIVATE_VALUE=yes\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	values := kernelConfig(root)
	if values["CONFIG_USB_G_ANDROID"] != "y" || values["CONFIG_BERLIN_USBPHY"] != "y" ||
		values["CONFIG_USB_MV_UDC"] != "m" || values["CONFIG_USB_FUNCTIONFS"] != "n" ||
		values["CONFIG_USB_PHY"] != "not-reported" {
		t.Fatal(values)
	}
	if _, exists := values["CONFIG_OTHER_PRIVATE_VALUE"]; exists {
		t.Fatal("unrequested kernel configuration leaked")
	}
}

func TestOriginRequiresActualMountAndNAND(t *testing.T) {
	// Test artifacts stay under the project; do not use the system temporary dir.
	root, err := os.MkdirTemp(".", ".status-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	write := func(path, value string) {
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("dev/block/31:5/uevent", "DEVNAME=mtdblock5\n")
	write("class/mtd/mtd5/type", "nand\n")
	for _, tc := range []struct{ name, data, want string }{
		{"empty", "", "unknown-unattested"},
		{"RAM", "1 0 0:1 / / rw - rootfs rootfs rw", "ram-or-other-unattested"},
		{"USB rehearsal squashfs", "1 0 7:0 / / ro - squashfs /dev/loop0 ro", "non-nand-squashfs-unattested"},
		{"compiled name is not provenance", "1 0 0:1 / / rw - tmpfs reInvoke-NAND-pilot-01 rw", "ram-or-other-unattested"},
		{"NAND device name alone", "1 0 7:0 / / ro - squashfs /dev/mtdblock5 ro", "non-nand-squashfs-unattested"},
		{"read-write", "1 0 31:5 / / rw - squashfs /dev/root rw", "source-not-readonly-unattested"},
		{"NAND", "1 0 31:5 / / ro - squashfs /dev/root ro", "nand-squashfs-observed-unattested"},
		{"chroot", "1 0 0:1 / / rw - tmpfs tmpfs rw\n2 0 31:5 / /nand-source ro - squashfs /dev/root ro", "nand-squashfs-observed-unattested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(mounts(tc.data), root); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	write("class/mtd/mtd5/type", "nor")
	if got := classify(mounts("1 0 31:5 / / ro - squashfs /dev/root ro"), root); got != "unknown-mtd-squashfs-unattested" {
		t.Fatal(got)
	}
}
