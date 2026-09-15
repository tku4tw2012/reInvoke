package regularfile

import (
	"fmt"
	"os"
	"syscall"
)

const PathOnly = 0x200000

func Open(path string) (*os.File, error) {
	fd, err := syscall.Open(path, PathOnly|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("pin input: %w", err)
	}
	pinned := os.NewFile(uintptr(fd), path)
	defer pinned.Close()
	return Reopen(pinned)
}

// Reopen uses a validated inode rather than resolving a mutable pathname again.
func Reopen(pinned *os.File) (*os.File, error) {
	before, err := pinned.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect pinned input: %w", err)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("input must be a regular file, not a device, symlink, or pipe: %s", pinned.Name())
	}
	file, err := os.Open(fmt.Sprintf("/proc/self/fd/%d", pinned.Fd()))
	if err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}
	after, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat opened input: %w", err)
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Close()
		return nil, fmt.Errorf("pinned input identity mismatch: %s", pinned.Name())
	}
	return file, nil
}
