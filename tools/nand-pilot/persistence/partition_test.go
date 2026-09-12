// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestPrivateDeviceCreationUsesOnlyDiscoveredBlockIdentity(t *testing.T) {
	calls := 0
	create := func(name string, mode uint32, device int) error {
		calls++
		if name != "/dev/reinvoke-persist" || mode != syscall.S_IFBLK|0600 ||
			device != 31<<8|7 {
			t.Fatalf("unexpected device operation: %q %o %d", name, mode, device)
		}
		return nil
	}
	if err := createPrivateDevice(partition{index: 7}, create); err != nil || calls != 1 {
		t.Fatalf("creation failed: %v calls=%d", err, calls)
	}
	for _, index := range []int{-1, 256} {
		if err := createPrivateDevice(partition{index: index}, create); err == nil {
			t.Fatal("invalid index accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid identity reached device creation")
	}
	if err := createPrivateDevice(partition{index: 7},
		func(string, uint32, int) error { return syscall.EPERM }); err == nil {
		t.Fatal("node creation failure hidden")
	}
}

func partitionFixture() (string, map[string]string) {
	return "dev: size erasesize name\nmtd0: 10000000 00020000 \"whole-chip\"\n" +
			"mtd7: 07b00000 00020000 \"app\"\n",
		map[string]string{
			"/sys/class/mtd/mtd7/name": "app\n", "/sys/class/mtd/mtd7/type": "nand\n",
			"/sys/class/mtd/mtd7/size": "128974848\n", "/sys/class/mtd/mtd7/erasesize": "131072\n",
			"/sys/class/mtd/mtd7/writesize": "2048\n", "/sys/class/mtd/mtd7/oobsize": "64\n",
			"/sys/class/mtd/mtd7/dev": "90:14\n", "/sys/class/block/mtdblock7/dev": "31:7\n",
		}
}

func TestMountFailureIsOneBoundedAttemptWithoutFallback(t *testing.T) {
	calls := 0
	err := mountApp("/synthetic/mtdblock7", func(device, target, fs string, flags uintptr, data string) error {
		calls++
		wantFlags := uintptr(syscall.MS_NOSUID | syscall.MS_NODEV | syscall.MS_NOEXEC | syscall.MS_NOATIME)
		if device != "/synthetic/mtdblock7" || target != "/persist" || fs != "yaffs2" ||
			flags != wantFlags || data != "" {
			t.Fatal("mount identity or options changed")
		}
		return errors.New("synthetic unsupported filesystem")
	})
	if calls != 1 || err == nil || err.Error() != "PERSIST_MOUNT_FAILED" {
		t.Fatal("mount failure was hidden or retried through fallback")
	}
}

func TestPartitionSelectionFailsClosed(t *testing.T) {
	tests := []struct {
		name, proc, field, value, filesystems, release, want string
	}{
		{name: "named-app-not-index", want: ""},
		{name: "master-only", proc: "mtd0: 10000000 00020000 \"nand\"\n", want: "PERSIST_APP_PARTITION_ABSENT"},
		{name: "wrong-case", proc: "mtd7: 07b00000 00020000 \"APP\"\n", want: "PERSIST_APP_PARTITION_ABSENT"},
		{name: "duplicate", proc: "mtd7: 07b00000 00020000 \"app\"\nmtd7: 07b00000 00020000 \"app\"\n", want: "PERSIST_APP_PARTITION_AMBIGUOUS"},
		{name: "generic-512-layout", proc: "mtd7: 10000000 00020000 \"app\"\n", want: "PERSIST_GEOMETRY_MISMATCH"},
		{name: "missing-sysfs", field: "writesize", value: "", want: "PERSIST_GEOMETRY_MISMATCH"},
		{name: "wrong-page", field: "writesize", value: "4096", want: "PERSIST_GEOMETRY_MISMATCH"},
		{name: "wrong-erase", field: "erasesize", value: "262144", want: "PERSIST_GEOMETRY_MISMATCH"},
		{name: "wrong-oob", field: "oobsize", value: "128", want: "PERSIST_GEOMETRY_MISMATCH"},
		{name: "not-nand", field: "type", value: "nor", want: "PERSIST_GEOMETRY_MISMATCH"},
		{name: "wrong-sysfs-name", field: "name", value: "rootfs", want: "PERSIST_GEOMETRY_MISMATCH"},
		{name: "wrong-offset", field: "offset", value: "0x08340000", want: "PERSIST_OFFSET_MISMATCH"},
		{name: "exact-offset", field: "offset", value: "0x08320000", want: ""},
		{name: "jffs-only", filesystems: "jffs2\n", want: "PERSIST_FILESYSTEM_UNSUPPORTED"},
		{name: "unknown-kernel", release: "3.8.13-other", want: "PERSIST_KERNEL_UNSUPPORTED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proc, files := partitionFixture()
			if test.proc != "" {
				proc = test.proc
			}
			if test.field != "" {
				files["/sys/class/mtd/mtd7/"+test.field] = test.value
			}
			fs, release := test.filesystems, test.release
			if fs == "" {
				fs = "nodev\ttmpfs\nyaffs2\n"
			}
			if release == "" {
				release = "3.8.13-yocto-standard"
			}
			p, err := selectPartition(proc, fs, release, func(path string) ([]byte, error) {
				value, ok := files[path]
				if !ok {
					return nil, os.ErrNotExist
				}
				return []byte(value), nil
			})
			if test.want == "" {
				if err != nil || p.index != 7 {
					t.Fatalf("expected exact named app selection, error=%v", err)
				}
			} else if err == nil || err.Error() != test.want {
				t.Fatalf("expected %s, error=%v", test.want, err)
			}
		})
	}
}

func TestMountedIdentityAndFlags(t *testing.T) {
	valid := "45 1 31:7 / /persist rw,nosuid,nodev,noexec,noatime - yaffs2 /dev/mtdblock7 rw\n"
	if ok, err := mountRecord(partition{7}, valid); !ok || err != nil {
		t.Fatal("valid mount rejected")
	}
	for _, changed := range []string{
		strings.Replace(valid, "31:7", "31:0", 1),
		strings.Replace(valid, "/persist", "/app", 1),
		strings.Replace(valid, "yaffs2", "jffs2", 1),
		strings.Replace(valid, "rw,nosuid", "ro,nosuid", 1),
		strings.Replace(valid, ",noexec", "", 1),
		strings.Replace(valid, " / /persist", " /subdir /persist", 1),
		valid + valid,
	} {
		if _, err := mountRecord(partition{7}, changed); err == nil {
			t.Fatal("unsafe mount accepted")
		}
	}
	if ok, err := mountRecord(partition{7}, ""); ok || err != nil {
		t.Fatal("absent mount must not appear mounted")
	}
}
