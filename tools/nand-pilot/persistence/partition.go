// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

const (
	appBytes          = 123 * 1024 * 1024
	eraseBytes        = 128 * 1024
	pageBytes         = 2048
	appOffset         = 0x08320000
	yaffsMagic        = 0x5941ff53
	mountPoint        = "/persist"
	storePath         = "/persist/reinvoke"
	runtimeDir        = "/run/reinvoke"
	bondsPath         = "/usr/var/lib/bluetooth"
	socketPath        = "/run/reinvoke/persistence.sock"
	privateDevicePath = "/dev/reinvoke-persist"
)

var mtdLine = regexp.MustCompile(`^mtd([0-9]+): ([0-9a-fA-F]+) ([0-9a-fA-F]+) "([^"]+)"$`)

type partition struct {
	index int
}

// Selection never derives an index from the vendor table or kernel command line.
func selectPartition(procMTD, filesystems, release string, read func(string) ([]byte, error)) (partition, error) {
	fail := func(code string) (partition, error) { return partition{}, errors.New(code) }
	if release != "3.8.13-yocto-standard" && release != "3.8.13-reinvoke-audio-sd8887" {
		return fail("PERSIST_KERNEL_UNSUPPORTED")
	}
	supported := false
	for _, line := range strings.Split(filesystems, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[len(fields)-1] == "yaffs2" {
			supported = true
		}
	}
	if !supported {
		return fail("PERSIST_FILESYSTEM_UNSUPPORTED")
	}
	var found []partition
	for _, line := range strings.Split(procMTD, "\n") {
		match := mtdLine.FindStringSubmatch(line)
		if match == nil || match[4] != "app" {
			continue
		}
		index, err := strconv.Atoi(match[1])
		size, sizeErr := strconv.ParseUint(match[2], 16, 64)
		erase, eraseErr := strconv.ParseUint(match[3], 16, 64)
		if err != nil || sizeErr != nil || eraseErr != nil || index > 255 ||
			size != appBytes || erase != eraseBytes {
			return fail("PERSIST_GEOMETRY_MISMATCH")
		}
		base := fmt.Sprintf("/sys/class/mtd/mtd%d/", index)
		for name, want := range map[string]string{
			"name": "app", "type": "nand", "size": strconv.Itoa(appBytes),
			"erasesize": strconv.Itoa(eraseBytes), "writesize": strconv.Itoa(pageBytes),
			"oobsize": "64", "dev": fmt.Sprintf("90:%d", index*2),
		} {
			value, err := read(base + name)
			if err != nil || strings.TrimSpace(string(value)) != want {
				return fail("PERSIST_GEOMETRY_MISMATCH")
			}
		}
		// Old vendor kernels do not necessarily publish partition offsets.
		if value, err := read(base + "offset"); err == nil {
			offset, parseErr := strconv.ParseUint(strings.TrimSpace(string(value)), 0, 64)
			if parseErr != nil || offset != appOffset {
				return fail("PERSIST_OFFSET_MISMATCH")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail("PERSIST_OFFSET_UNREADABLE")
		}
		value, err := read(fmt.Sprintf("/sys/class/block/mtdblock%d/dev", index))
		if err != nil || strings.TrimSpace(string(value)) != fmt.Sprintf("31:%d", index) {
			return fail("PERSIST_BLOCK_IDENTITY_MISMATCH")
		}
		found = append(found, partition{index: index})
	}
	if len(found) == 0 {
		return fail("PERSIST_APP_PARTITION_ABSENT")
	}
	if len(found) != 1 {
		return fail("PERSIST_APP_PARTITION_AMBIGUOUS")
	}
	return found[0], nil
}

func discover() (partition, error) {
	mtd, err := os.ReadFile("/proc/mtd")
	if err != nil {
		return partition{}, errors.New("PERSIST_MTD_UNAVAILABLE")
	}
	fs, err := os.ReadFile("/proc/filesystems")
	if err != nil {
		return partition{}, errors.New("PERSIST_FILESYSTEM_UNAVAILABLE")
	}
	release, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return partition{}, errors.New("PERSIST_KERNEL_UNAVAILABLE")
	}
	return selectPartition(string(mtd), string(fs), strings.TrimSpace(string(release)), os.ReadFile)
}

func devicePath(p partition) (string, error) {
	for _, path := range []string{
		privateDevicePath,
		fmt.Sprintf("/dev/mtdblock%d", p.index),
		fmt.Sprintf("/dev/block/mtdblock%d", p.index),
	} {
		if err := trustedParents(filepath.Dir(path), 0); err != nil {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok && info.Mode()&os.ModeType == os.ModeDevice && stat.Uid == 0 &&
			info.Mode().Perm()&0022 == 0 && uint64(stat.Rdev) == uint64(31<<8|p.index) {
			return path, nil
		}
	}
	return "", errors.New("PERSIST_BLOCK_DEVICE_UNSAFE")
}

func createPrivateDevice(p partition, mknod func(string, uint32, int) error) error {
	if p.index < 0 || p.index > 255 {
		return errors.New("PERSIST_BLOCK_IDENTITY_MISMATCH")
	}
	if err := trustedParents(filepath.Dir(privateDevicePath), 0); err != nil {
		return errors.New("PERSIST_BLOCK_DEVICE_UNSAFE")
	}
	if err := mknod(privateDevicePath, syscall.S_IFBLK|0600, 31<<8|p.index); err != nil &&
		!errors.Is(err, syscall.EEXIST) {
		return errors.New("PERSIST_BLOCK_NODE_FAILED")
	}
	return nil
}

func mountRecord(p partition, content string) (bool, error) {
	found := false
	bluedroidBindFound := false
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		sameDevice := fields[2] == fmt.Sprintf("31:%d", p.index)
		samePath := fields[4] == mountPoint
		// /data resolves here in the runtime; Bluedroid binds only this
		// subdirectory, not a second root mount of the app filesystem.
		bluedroidPath := fields[4] == "/home/galois_rwdata/misc/bluedroid"
		if !sameDevice && !samePath && !bluedroidPath {
			continue
		}
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
			}
		}
		options := "," + fields[5] + ","
		primary := samePath && fields[3] == "/"
		bluedroidBind := bluedroidPath && fields[3] == "/reinvoke/bluedroid"
		// Repeated Bluedroid binds are tolerated on purpose. Each one is
		// provably the same subtree, on the same device, over the same target,
		// so stacking them changes nothing a caller can observe. Treating the
		// second as a conflict made one leaked mount disable persistence for
		// the rest of the boot, which is a far worse failure than the leak.
		if !sameDevice || (!primary && !bluedroidBind) ||
			(primary && found) ||
			separator < 6 || separator+3 >= len(fields) || fields[separator+1] != "yaffs2" {
			return false, errors.New("PERSIST_MOUNT_CONFLICT")
		}
		for _, flag := range []string{"rw", "nosuid", "nodev", "noexec", "noatime"} {
			if !strings.Contains(options, ","+flag+",") {
				return false, errors.New("PERSIST_MOUNT_OPTIONS_UNSAFE")
			}
		}
		if primary {
			found = true
		} else {
			bluedroidBindFound = true
		}
	}
	if bluedroidBindFound && !found {
		return false, errors.New("PERSIST_MOUNT_CONFLICT")
	}
	return found, nil
}

func mounted(p partition) (bool, error) {
	content, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false, errors.New("PERSIST_MOUNTINFO_UNAVAILABLE")
	}
	return mountRecord(p, string(content))
}

func verifyMounted() error {
	p, err := discover()
	if err != nil {
		return err
	}
	ok, err := mounted(p)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("PERSIST_NOT_MOUNTED")
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPoint, &stat); err != nil || uint64(stat.Type)&0xffffffff != yaffsMagic {
		return errors.New("PERSIST_FILESYSTEM_IDENTITY_MISMATCH")
	}
	return nil
}

// The vendor app image ships its YAFFS2 root group-writable (0775). A store
// parent carrying 0022 is refused by trustedParents, so preparation could never
// succeed on stock media. Remove only those write bits once the mount is
// confirmed to be the verified app partition; nothing is granted.
// tightenedMode reports the permissions a vendor mount point must be changed
// to, or ok=false when it is already private. Special bits are refused rather
// than silently dropped, so an unexpected image fails closed.
func tightenedMode(mode os.FileMode, forbidden os.FileMode) (os.FileMode, bool, error) {
	if mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return 0, false, errors.New("PERSIST_MOUNTPOINT_UNSAFE")
	}
	perm := mode.Perm()
	if perm&forbidden == 0 {
		return 0, false, nil
	}
	return perm &^ forbidden, true, nil
}

func tightenMountPoint(path string, uid uint32, chmod func(string, os.FileMode) error) error {
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("PERSIST_MOUNTPOINT_UNSAFE")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		(stat.Uid != uid && stat.Uid != 0) {
		return errors.New("PERSIST_MOUNTPOINT_UNSAFE")
	}
	// Mirror trustedParents so tightening clears exactly what it forbids.
	forbidden := os.FileMode(0022)
	if uid != 0 {
		forbidden = 0002
	}
	wanted, change, err := tightenedMode(info.Mode(), forbidden)
	if err != nil {
		return err
	}
	if !change {
		return nil
	}
	if err := chmod(path, wanted); err != nil {
		return errors.New("PERSIST_MOUNTPOINT_NOT_TIGHTENED")
	}
	return nil
}

func prepareMount() error {
	p, err := discover()
	if err != nil {
		return err
	}
	device, err := devicePath(p)
	if err != nil {
		// Runtime startup removes generic MTD nodes. Recreate only the already
		// identified app block device; no partition mapping or flash operation.
		if err := createPrivateDevice(p, syscall.Mknod); err != nil {
			return err
		}
		device, err = devicePath(p)
		if err != nil {
			return err
		}
	}
	ok, err := mounted(p)
	if err != nil {
		return err
	}
	if ok {
		return errors.New("PERSIST_ALREADY_MOUNTED")
	}
	if err := ensurePrivateDirectory(mountPoint, 0); err != nil {
		return errors.New("PERSIST_MOUNTPOINT_UNSAFE")
	}
	entries, err := os.ReadDir(mountPoint)
	if err != nil || len(entries) != 0 {
		return errors.New("PERSIST_MOUNTPOINT_NOT_EMPTY")
	}
	if err := mountApp(device, syscall.Mount); err != nil {
		return err
	}
	if err := verifyMounted(); err != nil {
		return err
	}
	if err := tightenMountPoint(mountPoint, 0, os.Chmod); err != nil {
		return err
	}
	// Propagate the specific cause. Collapsing every reason into one opaque
	// code made a real preparation failure undiagnosable on hardware.
	if err := ensurePrivateDirectory(storePath, 0); err != nil {
		return err
	}
	return nil
}

func mountApp(device string, mount func(string, string, string, uintptr, string) error) error {
	flags := uintptr(syscall.MS_NOSUID | syscall.MS_NODEV | syscall.MS_NOEXEC | syscall.MS_NOATIME)
	if err := mount(device, mountPoint, "yaffs2", flags, ""); err != nil {
		return errors.New("PERSIST_MOUNT_FAILED")
	}
	return nil
}
