//go:build !nandpilot

package probewrite

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
)

func mappingFixture(t *testing.T) (*Bundle, *fakeDevice, *Snapshot) {
	t.Helper()
	bundle, device := fixture(t)
	device.stats = Stats{Corrected: 10, Failed: 2, BadBlocks: 1, BBTBlocks: 2}
	device.onRead = func(int64) { device.stats.Corrected++ }
	snapshot, err := Prepare(context.Background(), device, bundle, io.Discard, &mockJournal{})
	if err != nil {
		t.Fatal(err)
	}
	device.onRead = nil
	return bundle, device, snapshot
}

func TestMappingPhaseRetainsOriginalStatsAndReadOnlyState(t *testing.T) {
	bundle, device, snapshot := mappingFixture(t)
	tracker, initial := snapshot.stats, snapshot.stats.initial
	calls := 0
	journal := &mockJournal{}
	device.onRead = func(int64) { device.stats.Corrected++ }
	err := checkMappingPhase(context.Background(), device, bundle, snapshot, journal, func() error {
		calls++
		if device.identityChecks != 2 || len(journal.events) != 1 || journal.events[0].Stage != "mapping-requested" {
			t.Fatal("creation preceded the requested event or original-data rechecks")
		}
		return nil
	})
	if err != nil || calls != 1 || device.identityChecks != 3 {
		t.Fatalf("mapping qualification failed: calls=%d identities=%d err=%v", calls, device.identityChecks, err)
	}
	if snapshot.stats != tracker || tracker.initial != initial || tracker.last.Corrected != device.stats.Corrected {
		t.Fatal("mapping rebased the operation-wide statistics")
	}
	if device.enabled || len(device.erases) != 0 || len(device.writes) != 0 {
		t.Fatal("mapping reached a write operation")
	}
	var stages []string
	for _, event := range journal.events {
		stages = append(stages, event.Stage)
	}
	if !reflect.DeepEqual(stages, []string{"mapping-requested", "mapping-readback-verified"}) {
		t.Fatalf("wrong mapping phases: %v", stages)
	}
}

func TestMappingRechecksChangesAcrossJournalAndCreation(t *testing.T) {
	for _, phase := range []string{"mapping-requested", "creation"} {
		for _, test := range []struct {
			name   string
			mutate func(*Bundle, *fakeDevice, *Snapshot)
		}{
			{"bad inventory", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.BadBlocks++ }},
			{"BBT inventory", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.BBTBlocks++ }},
			{"corrected reset", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.Corrected-- }},
			{"failed reset", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.Failed-- }},
			{"failed increment", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.Failed++ }},
			{"bad first block", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.bad = 0 }},
			{"bad last block", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.bad = Blocks - 1 }},
			{"bad query", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.failBadQuery = true }},
			{"geometry", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.geometry.Span++ }},
			{"identity", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.failIdentity = true }},
			{"first byte", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.data[0] ^= 1 }},
			{"last byte", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.data[len(d.data)-1] ^= 1 }},
			{"short data", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.shortRead = true }},
			{"short OOB", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.shortOOB = true }},
			{"metadata", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.unexpectedOOB = true }},
			{"ECC", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.eccPage = 1 }},
			{"stats error", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.failStats = true }},
			{"snapshot hashes", func(_ *Bundle, _ *fakeDevice, s *Snapshot) { s.Hashes[1] = digest(nil) }},
			{"source mutation", func(b *Bundle, _ *fakeDevice, _ *Snapshot) { b.original = bytes.NewReader(make([]byte, Span)) }},
		} {
			t.Run(phase+"/"+test.name, func(t *testing.T) {
				bundle, device, snapshot := mappingFixture(t)
				calls := 0
				journal := &mockJournal{onRecord: func(event Event) {
					if event.Stage == phase {
						test.mutate(bundle, device, snapshot)
					}
				}}
				err := checkMappingPhase(context.Background(), device, bundle, snapshot, journal, func() error {
					calls++
					if phase == "creation" {
						test.mutate(bundle, device, snapshot)
					}
					return nil
				})
				wantCalls := 0
				if phase == "creation" {
					wantCalls = 1
				}
				if err == nil || calls != wantCalls {
					t.Fatalf("mapping adopted changed state: calls=%d err=%v", calls, err)
				}
				for _, event := range journal.events {
					if event.Stage == "mapping-readback-verified" {
						t.Fatal("reported mapping verified after a failed recheck")
					}
				}
			})
		}
	}
}

func TestMappingCancellationStopsCreationOrVerification(t *testing.T) {
	for _, phase := range []string{"before", "mapping-requested", "last precreation read", "creation", "mapping-readback-verified"} {
		t.Run(phase, func(t *testing.T) {
			bundle, device, snapshot := mappingFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "before" {
				cancel()
			}
			if phase == "last precreation read" {
				device.onRead = func(offset int64) {
					if offset == Span-EraseBytes {
						cancel()
					}
				}
			}
			journal := &mockJournal{onRecord: func(event Event) {
				if event.Stage == phase {
					cancel()
				}
			}}
			calls := 0
			err := checkMappingPhase(ctx, device, bundle, snapshot, journal, func() error {
				calls++
				if phase == "creation" {
					cancel()
				}
				return nil
			})
			wantCalls := 0
			if phase == "creation" || phase == "mapping-readback-verified" {
				wantCalls = 1
			}
			if !errors.Is(err, context.Canceled) || calls != wantCalls {
				t.Fatalf("cancellation ignored: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestMappingJournalAndCreationFailuresPropagate(t *testing.T) {
	for _, phase := range []string{"mapping-requested", "creation", "mapping-readback-verified"} {
		t.Run(phase, func(t *testing.T) {
			bundle, device, snapshot := mappingFixture(t)
			journal := &mockJournal{failAt: phase}
			injected := errors.New("creation failed")
			calls := 0
			err := checkMappingPhase(context.Background(), device, bundle, snapshot, journal, func() error {
				calls++
				if phase == "creation" {
					return injected
				}
				return nil
			})
			wantCalls := 1
			if phase == "mapping-requested" {
				wantCalls = 0
			}
			if err == nil || calls != wantCalls || (phase == "creation" && !errors.Is(err, injected)) {
				t.Fatalf("mapping failure suppressed: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestMappingRequiresCompleteOriginalPreflight(t *testing.T) {
	for _, snapshot := range []*Snapshot{nil, {}, {Hashes: make([]string, Blocks)}, {stats: &statsTracker{}},
		{stats: &statsTracker{}, Hashes: make([]string, Blocks), Failed: 1}} {
		bundle, device := fixture(t)
		calls := 0
		journal := &mockJournal{}
		err := checkMappingPhase(context.Background(), device, bundle, snapshot, journal, func() error { calls++; return nil })
		if err == nil || calls != 0 || device.identityChecks != 0 || len(journal.events) != 0 {
			t.Fatalf("incomplete preflight reached mapping: calls=%d err=%v", calls, err)
		}
	}
}

func TestMappingStatsCannotRebaseBetweenChecks(t *testing.T) {
	_, device, snapshot := mappingFixture(t)
	device.stats.Corrected++
	if _, err := mappingStats(device, snapshot.stats, snapshot.Failed); err != nil {
		t.Fatal(err)
	}
	device.stats.Corrected--
	if _, err := mappingStats(device, snapshot.stats, snapshot.Failed); err == nil {
		t.Fatal("mapping adopted an ECC reset between comparisons")
	}
	device.stats.Corrected++
	device.stats.BadBlocks++
	if _, err := mappingStats(device, snapshot.stats, snapshot.Failed); err == nil {
		t.Fatal("mapping adopted changed inventory between comparisons")
	}
}
