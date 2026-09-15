//go:build nandpilot

package probewrite

import (
	"context"
	"io"
	"testing"
)

func TestResumeUniformCorrectionsOnPreservedAndNewPages(t *testing.T) {
	bundle, d := resumeFixture(t)
	d.stats.Failed = 10
	d.onRead = func(offset int64, _ int, _ bool) {
		if offset == 107*EraseBytes+9*PageBytes ||
			(offset == 137*EraseBytes+3*PageBytes && d.data[137*EraseBytes] == 0x22) {
			d.stats.Corrected++
		}
	}
	journal := &resumeTestJournal{}
	snapshot, err := prepareResume(context.Background(), d, bundle, io.Discard, journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := executePrepared(context.Background(), d, bundle, snapshot, false, journal, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(d.erases) != 172 || d.erases[0] != 137 || d.erases[171] != 0 || d.stats.Failed != 10 {
		t.Fatal("uniform correction policy changed scope or accepted a new failure")
	}
	for _, index := range d.erases {
		if resumeCandidateBlock(index) {
			t.Fatal("rewrote an already-installed block")
		}
	}
	preserved, programmed := false, false
	for _, e := range journal.events {
		if e.Stage != "resume-page-main" && e.Stage != "resume-page-oob" {
			continue
		}
		if e.Corrected != 1 || e.Failed != 0 {
			t.Fatal("incorrect correction evidence")
		}
		preserved = preserved || e.Block == 107
		programmed = programmed || e.Block == 137
	}
	if !preserved || !programmed {
		t.Fatal("did not journal corrections on both preserved and freshly written pages")
	}
}
