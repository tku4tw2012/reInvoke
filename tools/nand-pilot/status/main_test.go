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
	if archive := os.Getenv("REINVOKE_ARCHIVE"); archive != "" {
		cmd.Args = append(cmd.Args, archive)
	}
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

func TestAdminListenerEvidenceDoesNotRevealAddresses(t *testing.T) {
	root := t.TempDir()
	if got := adminListeners(root); got["ssh"] != "unavailable" ||
		got["network_adb"] != "unavailable" {
		t.Fatal("missing procfs must remain unknown", got)
	}
	dir := filepath.Join(root, "proc/net")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tcp := "sl local_address rem_address st\n" +
		"0: 0100007F:0016 00000000:0000 01\n" +
		"1: 0100007F:15B3 00000000:0000 0A\n" +
		"2: 0100007F:ZZZZ 00000000:0000 0A\n"
	if err := os.WriteFile(filepath.Join(dir, "tcp"), []byte(tcp), 0600); err != nil {
		t.Fatal(err)
	}
	got := adminListeners(root)
	if got["ssh"] != "not-observed-in-bounded-scan" ||
		got["network_adb"] != "listening-observed" {
		t.Fatal("a connected socket is not a listener", got)
	}
	tcp6 := "sl local_address rem_address st\n" +
		"0: 00000000000000000000000001000000:0016 00000000000000000000000000000000:0000 0A\n"
	if err := os.WriteFile(filepath.Join(dir, "tcp6"), []byte(tcp6), 0600); err != nil {
		t.Fatal(err)
	}
	got = adminListeners(root)
	if got["ssh"] != "listening-observed" {
		t.Fatal("IPv6 listener not detected", got)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "0100007F") ||
		strings.Contains(string(data), "00000000000000000000000001000000") {
		t.Fatal("raw network identifiers leaked")
	}
}

func TestNetworkADBStatusIsBoundedAndRedacted(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run/nand-pilot")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("adb-network-state", "closed\n")
	write("adb-network-firewall", "installed\n")
	write("adb-network-result", "expired\n")
	write("adb-network-deadline", "301\n")
	write("failure-adb-network", "SECRET-PRIVATE-CONTEXT")
	write("usb-last-failure", "enable-node-invalid")
	s := collect(root)
	if s.NetworkADB.State != "closed" || s.NetworkADB.Firewall != "installed" ||
		s.NetworkADB.Result != "expired" || s.NetworkADB.MonotonicDeadline != "301" {
		t.Fatal(s.NetworkADB)
	}
	if s.USB.LastFailure != "enable-node-invalid" || s.Failures["adb-network"] == "" {
		t.Fatal("diagnostic failures were not reported")
	}
	write("adb-network-state", "usb-preserved")
	if collect(root).NetworkADB.State != "usb-preserved" {
		t.Fatal("retained USB transport was not reported")
	}
	write("adb-network-state", "SECRET-PRIVATE-CONTEXT")
	write("adb-network-deadline", "SECRET-PRIVATE-CONTEXT")
	s = collect(root)
	if s.NetworkADB.State != "unknown-or-unavailable" ||
		s.NetworkADB.MonotonicDeadline != "not-reported" {
		t.Fatal("unexpected status content was treated as evidence")
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SECRET") {
		t.Fatal("unallowlisted diagnostic details leaked")
	}
}

func TestPersistenceStatusRejectsUnsafeOrPrivateContent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run/reinvoke")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "persistence-status.json")
	uid := uint32(os.Getuid())
	valid := `{"version":1,"phase":"ready","snapshot":"present","wifi_profile":"present","result":"PERSIST_READY"}`
	if err := os.WriteFile(file, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if got := persistenceEvidence(root, uid); got.Phase != "ready" || got.Result != "PERSIST_READY" {
		t.Fatal(got)
	}
	for _, content := range []string{"not JSON", valid + valid, strings.Repeat("x", 513),
		`{"version":1,"phase":"ready","snapshot":"present","wifi_profile":"present","result":"PERSIST_READY","ssid":"SECRET"}`} {
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if persistenceEvidence(root, uid).Phase != "unknown" {
			t.Fatal("invalid status accepted")
		}
	}
	if err := os.WriteFile(file, []byte(strings.ReplaceAll(valid, "PERSIST_READY", "PERSIST_SECRET_DATA")), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(persistenceEvidence(root, uid))
	if err != nil || strings.Contains(string(data), "SECRET") {
		t.Fatal("unknown result data leaked")
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if persistenceEvidence(root, uid).Phase != "unknown" {
		t.Fatal("unsafe status permissions accepted")
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
