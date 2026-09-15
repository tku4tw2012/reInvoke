package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectRepositoryArtifacts(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := outsideRepository(filepath.Join(cwd, "private-output")); err == nil {
		t.Fatal("accepted private artifacts inside repository")
	}
	if err := outsideRepository(cwd); err == nil {
		t.Fatal("accepted current directory")
	}
}

func TestRejectNonImage(t *testing.T) {
	if _, err := readImage("main.go"); err == nil {
		t.Fatal("accepted wrong-size input")
	}
	if _, err := readImage("."); err == nil {
		t.Fatal("accepted directory input")
	}
	if _, err := readAllocation("main.go", nil); err == nil {
		t.Fatal("accepted wrong-size capture")
	}
}
