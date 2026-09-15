package main

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPinnedInputSurvivesPathReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture")
	if err := os.WriteFile(path, []byte("original capture"), 0600); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Open(path, linuxOPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	pinned := os.NewFile(uintptr(fd), path)
	defer pinned.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", path); err != nil {
		t.Fatal(err)
	}
	file, err := reopenRegularInput(pinned)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original capture" {
		t.Fatalf("reopened replacement rather than pinned inode: %q", data)
	}
}

func TestOpenRegularInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.bin")
	if err := os.WriteFile(path, []byte("capture"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openRegularInput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write([]byte("not allowed")); err == nil {
		t.Fatal("input descriptor unexpectedly allowed writing")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "capture" {
		t.Fatalf("input changed: %q", got)
	}
}

func TestRejectNonRegularInput(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "regular")
	if err := os.WriteFile(filePath, []byte("capture"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "symlink")
	if err := os.Symlink(filePath, link); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(pipe, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, link, pipe, "/dev/null", filepath.Join(dir, "missing")} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			file, err := openRegularInput(path)
			if err == nil {
				file.Close()
				t.Fatalf("accepted non-regular input %s", path)
			}
		})
	}
}
