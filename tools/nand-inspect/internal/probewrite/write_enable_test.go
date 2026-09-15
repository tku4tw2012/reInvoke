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

func writeEnableFixture(t *testing.T, restore bool) (*Bundle, *fakeDevice, *Snapshot) {
	t.Helper()
	bundle, device := fixture(t)
	if restore {
		device.data[0] ^= 0x55
		device.eccPage = 0
	}
	snapshot, err := preflight(context.Background(), device, bundle, restore, io.Discard, &mockJournal{})
	if err != nil {
		t.Fatal(err)
	}
	return bundle, device, snapshot
}

func TestWriteKernelGateAllowsOnlyExactARMKernel(t *testing.T) {
	for _, test := range []struct {
		name, arch, banner string
		allowed            bool
	}{
		{"fixed ARM", "arm", mappingKernelBanner, true},
		{"old kernel", "arm", "Linux version 3.8.13-reinvoke-audio-sd8887 #1 SMP PREEMPT", false},
		{"release only", "arm", "3.8.13-reinvoke-audio-sd8887", false},
		{"modified banner", "arm", mappingKernelBanner + " modified", false},
		{"host", "amd64", mappingKernelBanner, false},
		{"arm64", "arm64", mappingKernelBanner, false},
		{"missing banner", "arm", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			bundle, device, snapshot := writeEnableFixture(t, false)
			tracker, initial := snapshot.stats, snapshot.stats.initial
			calls := 0
			err := qualifyWriteKernel(test.arch, test.banner, func() error {
				err := enableWritesPhase(context.Background(), device, bundle, snapshot, &mockJournal{}, func() error {
					calls++
					return nil
				})
				if err == nil {
					device.enabled = true
				}
				return err
			})
			if test.allowed && (err != nil || calls != 1 || !device.enabled) {
				t.Fatalf("qualified enablement failed: calls=%d err=%v", calls, err)
			}
			if !test.allowed && (err == nil || calls != 0 || device.enabled) {
				t.Fatalf("kernel guard permitted enablement: calls=%d err=%v", calls, err)
			}
			if snapshot.stats != tracker || snapshot.stats.initial != initial ||
				len(device.erases) != 0 || len(device.writes) != 0 {
				t.Fatal("enablement rebased statistics or wrote flash")
			}
		})
	}
	injected := errors.New("qualified callback failed")
	if err := qualifyWriteKernel("arm", mappingKernelBanner, func() error { return injected }); !errors.Is(err, injected) {
		t.Fatalf("gate lost callback failure: %v", err)
	}
}

func TestWriteEnablementRejectsStateAndSourceDriftBeforeRW(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Bundle, *fakeDevice, *Snapshot)
	}{
		{"bad inventory", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.BadBlocks++ }},
		{"BBT inventory", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.BBTBlocks++ }},
		{"counter reset", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.Corrected-- }},
		{"failed increment", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.stats.Failed++ }},
		{"bad block", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.bad = 1 }},
		{"target changed", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.data[EraseBytes] ^= 1 }},
		{"original source", func(b *Bundle, _ *fakeDevice, _ *Snapshot) { b.original = bytes.NewReader(make([]byte, Span)) }},
		{"candidate source", func(b *Bundle, _ *fakeDevice, _ *Snapshot) { b.image = bytes.NewReader(make([]byte, Span)) }},
		{"visible OOB", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.unexpectedOOB = true }},
		{"new failed ECC", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.eccPage = 0 }},
		{"identity", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.failIdentity = true }},
		{"geometry", func(_ *Bundle, d *fakeDevice, _ *Snapshot) { d.geometry.Span++ }},
		{"wrong device snapshot", func(_ *Bundle, _ *fakeDevice, s *Snapshot) { s.device = &fakeDevice{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bundle, device, snapshot := writeEnableFixture(t, false)
			device.stats.Corrected = 3
			snapshot.stats.last.Corrected = 3
			journal := &mockJournal{onRecord: func(event Event) {
				if event.Stage == "write-enable-requested" {
					test.mutate(bundle, device, snapshot)
				}
			}}
			if test.name == "wrong device snapshot" {
				test.mutate(bundle, device, snapshot)
			}
			calls := 0
			err := enableWritesPhase(context.Background(), device, bundle, snapshot, journal, func() error { calls++; return nil })
			if err == nil || calls != 0 || device.enabled {
				t.Fatalf("RW acquisition accepted changed state: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestWriteEnablementCancellationAndReportingFailure(t *testing.T) {
	for _, phase := range []string{"before", "write-enable-requested", "last read", "acquire", "write-descriptor-verified"} {
		t.Run("cancel/"+phase, func(t *testing.T) {
			bundle, device, snapshot := writeEnableFixture(t, false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "before" {
				cancel()
			}
			if phase == "last read" {
				device.onRead = func(offset int64) {
					if offset == EraseBytes {
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
			err := enableWritesPhase(ctx, device, bundle, snapshot, journal, func() error {
				calls++
				if phase == "acquire" {
					cancel()
				}
				return nil
			})
			wantCalls := 0
			if phase == "acquire" || phase == "write-descriptor-verified" {
				wantCalls = 1
			}
			if !errors.Is(err, context.Canceled) || calls != wantCalls {
				t.Fatalf("cancellation ignored: calls=%d err=%v", calls, err)
			}
		})
	}
	for _, phase := range []string{"write-enable-requested", "acquire", "write-descriptor-verified"} {
		t.Run("failure/"+phase, func(t *testing.T) {
			bundle, device, snapshot := writeEnableFixture(t, false)
			journal := &mockJournal{failAt: phase}
			calls := 0
			err := enableWritesPhase(context.Background(), device, bundle, snapshot, journal, func() error {
				calls++
				if phase == "acquire" {
					return errors.New("RW acquisition failed")
				}
				return nil
			})
			wantCalls := 1
			if phase == "write-enable-requested" {
				wantCalls = 0
			}
			if err == nil || calls != wantCalls {
				t.Fatalf("enablement failure suppressed: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestRestoreQualificationAllowsOnlyFailedReadsInChangedBlocks(t *testing.T) {
	if !RestoreSupported {
		t.Skip("explicit restoration is disabled for this profile")
	}
	bundle, device, snapshot := writeEnableFixture(t, true)
	initial := snapshot.stats.initial
	failed := snapshot.Failed
	createCalls, rwCalls := 0, 0
	journal := &mockJournal{}
	if err := checkPartitionPhase(context.Background(), device, bundle, snapshot, journal, func() error { createCalls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := enableWritesPhase(context.Background(), device, bundle, snapshot, journal, func() error { rwCalls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if createCalls != 1 || rwCalls != 1 || snapshot.Failed <= failed ||
		snapshot.stats.initial != initial || uint64(device.stats.Failed) != uint64(initial.Failed)+snapshot.Failed {
		t.Fatal("recovery failed-read accounting was lost or rebased")
	}
	calls := 0
	if err := checkMappingPhase(context.Background(), device, bundle, snapshot, &mockJournal{}, func() error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("check-map accepted a recovery preflight")
	}
}

func TestRestoreQualificationRejectsFailedECCOutsideChangedReads(t *testing.T) {
	if !RestoreSupported {
		t.Skip("explicit restoration is disabled for this profile")
	}
	for _, fault := range []string{"preserved block", "outside a read", "counter reset", "inventory"} {
		t.Run(fault, func(t *testing.T) {
			bundle, device := preservedFixture(t)
			snapshot, err := preflight(context.Background(), device, bundle, true, io.Discard, &mockJournal{})
			if err != nil {
				t.Fatal(err)
			}
			device.stats.Corrected = 2
			snapshot.stats.last.Corrected = 2
			journal := &mockJournal{onRecord: func(event Event) {
				if event.Stage != "write-enable-requested" {
					return
				}
				switch fault {
				case "preserved block":
					device.eccPage = 1
				case "outside a read":
					device.stats.Failed++
				case "counter reset":
					device.stats.Corrected--
				case "inventory":
					device.stats.BadBlocks++
				}
			}}
			calls := 0
			err = enableWritesPhase(context.Background(), device, bundle, snapshot, journal, func() error { calls++; return nil })
			if err == nil || calls != 0 {
				t.Fatalf("recovery exception escaped changed-block reads: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestWriteDescriptorRechecksStateAfterAcquisition(t *testing.T) {
	for _, fault := range []string{"inventory", "failed ECC", "target"} {
		t.Run(fault, func(t *testing.T) {
			bundle, device, snapshot := writeEnableFixture(t, false)
			journal := &mockJournal{}
			err := enableWritesPhase(context.Background(), device, bundle, snapshot, journal, func() error {
				switch fault {
				case "inventory":
					device.stats.BBTBlocks++
				case "failed ECC":
					device.stats.Failed++
				case "target":
					device.data[0] ^= 1
				}
				return nil
			})
			var stages []string
			for _, event := range journal.events {
				stages = append(stages, event.Stage)
			}
			if err == nil || !reflect.DeepEqual(stages, []string{"write-enable-requested"}) {
				t.Fatalf("reported unsafe descriptor verified: err=%v stages=%v", err, stages)
			}
		})
	}
}
