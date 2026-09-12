// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
