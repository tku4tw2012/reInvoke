package probewrite

import (
	"os"
	"path/filepath"
	"testing"
)

func privateArchiveFixture(t *testing.T, elements ...string) string {
	t.Helper()
	archive := os.Getenv("REINVOKE_ARCHIVE")
	if archive == "" {
		t.Skip("REINVOKE_ARCHIVE is not set; private hardware fixture unavailable")
	}
	return filepath.Join(append([]string{archive}, elements...)...)
}
