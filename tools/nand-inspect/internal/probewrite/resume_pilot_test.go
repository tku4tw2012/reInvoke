//go:build nandpilot

package probewrite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type resumeTestDevice struct {
	*pilotDevice
	onRead                        func(int64, int, bool)
	onStatus                      func()
	onEnable                      func()
	onErase                       func()
	onWrite                       func()
	shortMain, shortOOB, metadata bool
}

func (d *resumeTestDevice) ReadAt(data []byte, offset int64) (int, error) {
	n, err := d.pilotDevice.ReadAt(data, offset)
	if d.onRead != nil {
		d.onRead(offset, len(data), false)
	}
	if d.shortMain {
		n--
	}
	return n, err
}

func (d *resumeTestDevice) ReadOOB(offset int64) ([]byte, error) {
	data, err := d.pilotDevice.ReadOOB(offset)
	if d.onRead != nil {
		d.onRead(offset, len(data), true)
	}
	if d.metadata {
		data = append([]byte(nil), data...)
		data[0] = 0
	}
	if d.shortOOB {
		data = data[:1]
	}
	return data, err
}

func (d *resumeTestDevice) IsBad(offset int64) (bool, error) {
	if d.onStatus != nil {
		d.onStatus()
	}
	return d.pilotDevice.IsBad(offset)
}

func (d *resumeTestDevice) EnableWrites(ctx context.Context, bundle *Bundle, snapshot *Snapshot, journal Journal) error {
	if d.onEnable != nil {
		d.onEnable()
	}
	if err := recheckMappingState(ctx, d, bundle, snapshot); err != nil {
		return err
	}
	d.enabled = true
	d.enables++
	return nil
}

func (d *resumeTestDevice) Erase(offset int64) error {
	if err := d.pilotDevice.Erase(offset); err != nil {
		return err
	}
	if d.onErase != nil {
		d.onErase()
	}
	return nil
}

func (d *resumeTestDevice) WriteAt(data []byte, offset int64) (int, error) {
	n, err := d.pilotDevice.WriteAt(data, offset)
	if d.onWrite != nil {
		d.onWrite()
	}
	return n, err
}

type resumeTestJournal struct {
	events   []Event
	onRecord func(Event) error
}

func (j *resumeTestJournal) Record(event Event) error {
	j.events = append(j.events, event)
	if j.onRecord != nil {
		return j.onRecord(event)
	}
	return nil
}

var resumeFixtureTemplate struct {
	sync.Once
	bundle *Bundle
	err    error
}

func resumeFixture(t *testing.T) (*Bundle, *resumeTestDevice) {
	t.Helper()
	resumeFixtureTemplate.Do(func() {
		original, candidate := pilotBytes{}, pilotBytes{}
		for index := 0; index < Blocks; index++ {
			offset := int64(index) * EraseBytes
			original[offset], candidate[offset] = 0x11, 0x22
		}
		resumeFixtureTemplate.bundle = &Bundle{image: candidate, original: original}
		resumeFixtureTemplate.err = resumeFixtureTemplate.bundle.buildPlan()
	})
	if resumeFixtureTemplate.err != nil {
		t.Fatal(resumeFixtureTemplate.err)
	}
	template := resumeFixtureTemplate.bundle
	bundle := &Bundle{Plan: template.Plan}
	bundle.Plan.Blocks = append([]Block(nil), template.Plan.Blocks...)
	bundle.Plan.WriteOrder = append([]int(nil), template.Plan.WriteOrder...)
	original, candidate, mixed := pilotBytes{}, pilotBytes{}, pilotBytes{}
	for offset, value := range template.original.(pilotBytes) {
		original[offset], mixed[offset] = value, value
	}
	for offset, value := range template.image.(pilotBytes) {
		candidate[offset] = value
		if resumeCandidateBlock(int(offset / EraseBytes)) {
			mixed[offset] = value
		}
	}
	bundle.image, bundle.original = candidate, original
	d := &resumeTestDevice{pilotDevice: &pilotDevice{data: mixed, eccBlock: -1, correctedBlock: -1}}
	return bundle, d
}

func resumeTracker(t *testing.T, d Device) *statsTracker {
	t.Helper()
	tracker, err := newStatsTracker(d)
	if err != nil {
		t.Fatal(err)
	}
	tracker.resume = true
	return tracker
}

func knownCorrections(d *resumeTestDevice, count uint32) {
	d.onRead = func(offset int64, size int, oob bool) {
		if offset == resumeLastCandidate*EraseBytes+resumeKnownPage*PageBytes {
			d.stats.Corrected += count
		}
	}
}

func TestResumePerActualReadAllowance(t *testing.T) {
	for _, test := range []struct {
		name             string
		block, page      int
		main, oob        uint32
		original, reject bool
	}{
		{"known-zero", 136, 62, 0, 0, false, false},
		{"known-main-one", 136, 62, 1, 0, false, false},
		{"known-oob-one", 136, 62, 0, 1, false, false},
		{"known-both-one", 136, 62, 1, 1, false, false},
		{"known-main-two", 136, 62, 2, 0, false, true},
		{"known-oob-two", 136, 62, 0, 2, false, true},
		{"other-page-main", 136, 61, 1, 0, false, false},
		{"other-page-oob", 136, 63, 0, 1, false, false},
		{"previous-candidate", 107, 9, 1, 0, false, false},
		{"previous-candidate-two", 107, 9, 2, 0, false, true},
		{"newly-programmed", 137, 3, 1, 0, false, false},
		{"newly-programmed-two", 137, 3, 2, 0, false, true},
		{"new-header", 0, 20, 0, 1, false, false},
		{"new-header-two", 0, 20, 0, 2, false, true},
		{"original-main", 137, 0, 2, 0, true, false},
		{"original-oob", 0, 0, 0, 2, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := &resumeTestDevice{pilotDevice: &pilotDevice{data: pilotBytes{}, eccBlock: -1, correctedBlock: -1}}
			d.onRead = func(offset int64, size int, oob bool) {
				if offset != int64(test.block)*EraseBytes+int64(test.page*PageBytes) {
					return
				}
				if !oob && size != PageBytes {
					t.Fatal("main read was not split by actual page")
				}
				if oob {
					d.stats.Corrected += test.oob
				} else {
					d.stats.Corrected += test.main
				}
			}
			_, _, stats, err := checkedResumeRead(context.Background(), d, test.block, test.original, resumeTracker(t, d))
			if (err != nil) != test.reject {
				t.Fatalf("reject=%v error=%v", test.reject, err)
			}
			if !test.reject && stats.Corrected != test.main+test.oob {
				t.Fatalf("lost attributed corrections: %+v", stats)
			}
		})
	}
}

func TestResumeStatsAndReadFailures(t *testing.T) {
	for _, failure := range []string{"between-read-failed", "failed", "bad-inventory", "bbt-inventory", "reset", "between-read-correction", "metadata", "short-main", "short-oob", "status-correction"} {
		t.Run(failure, func(t *testing.T) {
			d := &resumeTestDevice{pilotDevice: &pilotDevice{data: pilotBytes{}, eccBlock: -1, correctedBlock: -1, stats: Stats{Corrected: 4}}}
			tracker := resumeTracker(t, d)
			initial := tracker.initial
			d.onRead = func(_ int64, _ int, _ bool) {
				switch failure {
				case "failed":
					d.stats.Failed++
				case "bad-inventory":
					d.stats.BadBlocks++
				case "bbt-inventory":
					d.stats.BBTBlocks++
				case "reset":
					d.stats.Corrected = 0
				}
			}
			switch failure {
			case "between-read-failed":
				d.stats.Failed++
			case "between-read-correction":
				d.stats.Corrected++
			case "metadata":
				d.metadata = true
			case "short-main":
				d.shortMain = true
			case "short-oob":
				d.shortOOB = true
			case "status-correction":
				d.onStatus = func() { d.stats.Corrected++ }
				if err := checkedBlockStatus(d, 136, tracker); err == nil {
					t.Fatal("status absorbed correction")
				}
				return
			}
			if _, _, _, err := checkedResumeRead(context.Background(), d, 136, false, tracker); err == nil {
				t.Fatal("read accepted injected failure")
			}
			if tracker.initial != initial {
				t.Fatal("initial operation baseline changed")
			}
		})
	}
}

func TestResumePreservesHistoricalFailedCounter(t *testing.T) {
	bundle, d := resumeFixture(t)
	d.stats.Failed = 10
	snapshot, err := prepareResume(context.Background(), d, bundle, io.Discard, &resumeTestJournal{})
	if err != nil {
		t.Fatalf("historical counter blocked a healthy target: %v", err)
	}
	if snapshot.Failed != 0 || snapshot.stats.initial.Failed != 10 ||
		snapshot.stats.last.Failed != 10 || d.stats.Failed != 10 {
		t.Fatal("historical failed counter was cleared or counted as a new target error")
	}
	if err := validateSnapshot(d, bundle, snapshot); err != nil {
		t.Fatal(err)
	}
	m := &MTD{resume: true}
	mapped := *snapshot
	mapped.device = m
	if err := m.validateSnapshotMode(&mapped); err != nil {
		t.Fatal(err)
	}
	if err := executePrepared(context.Background(), d, bundle, snapshot, false, &resumeTestJournal{}, func() error { return nil }); err != nil {
		t.Fatalf("historical counter blocked completion: %v", err)
	}
	if len(d.erases) != 172 || d.erases[0] != 137 || d.erases[171] != 0 ||
		d.stats.Failed != 10 || snapshot.stats.initial.Failed != 10 {
		t.Fatal("resume changed its write scope or historical counter")
	}
	if _, _, _, err := checkedResumeRead(context.Background(), d, 136, false, snapshot.stats); err != nil {
		t.Fatal(err)
	}
	d.stats.Failed++
	if _, _, _, err := checkedResumeRead(context.Background(), d, 136, false, snapshot.stats); err == nil {
		t.Fatal("new failed ECC increment was accepted")
	}
	d.stats.Failed = 9
	if _, err := snapshot.stats.check(d); err == nil {
		t.Fatal("failed counter decrease was accepted")
	}
}

func TestResumePreflightMixedStateAndDrift(t *testing.T) {
	bundle, _ := resumeFixture(t)
	for _, block := range []int{0, 1, 135, 136, 137, 307} {
		t.Run(fmt.Sprint(block), func(t *testing.T) {
			_, d := resumeFixture(t)
			d.data[int64(block)*EraseBytes] ^= 1
			if _, err := prepareResume(context.Background(), d, bundle, io.Discard, &resumeTestJournal{}); err == nil || d.enabled || len(d.erases) != 0 {
				t.Fatal("modified mixed state accepted")
			}
		})
	}
	for _, failure := range []string{"candidate-drift", "future-drift", "header-drift", "candidate-source", "original-source", "incomplete", "restore-mode", "tracker-mode", "different-device", "journal-failed-ecc"} {
		t.Run(failure, func(t *testing.T) {
			bundle, d := resumeFixture(t)
			snapshot, err := prepareResume(context.Background(), d, bundle, io.Discard, &resumeTestJournal{})
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "candidate-drift":
				d.data[136*EraseBytes] ^= 1
			case "future-drift":
				d.data[137*EraseBytes] ^= 1
			case "header-drift":
				d.data[0] ^= 1
			case "candidate-source":
				bundle.image.(pilotBytes)[0] ^= 1
			case "original-source":
				bundle.original.(pilotBytes)[EraseBytes] ^= 1
			case "incomplete":
				snapshot.complete = false
			case "restore-mode":
				snapshot.restore = true
			case "tracker-mode":
				snapshot.stats.resume = false
			case "different-device":
				snapshot.device = nil
			}
			created := false
			journal := &resumeTestJournal{onRecord: func(Event) error {
				if failure == "journal-failed-ecc" {
					d.stats.Failed++
				}
				return nil
			}}
			if err := checkPartitionPhase(context.Background(), d, bundle, snapshot, journal, func() error { created = true; return nil }); err == nil || created {
				t.Fatal("mapping accepted drift or incomplete/wrong snapshot")
			}
		})
	}
}

func TestResumeWritesOnlyFutureBlocksThenHeader(t *testing.T) {
	bundle, d := resumeFixture(t)
	knownCorrections(d, 1)
	journal := &resumeTestJournal{}
	var oob pilotByteCount
	snapshot, err := prepareResume(context.Background(), d, bundle, &oob, journal)
	if err != nil {
		t.Fatal(err)
	}
	initial, tracker := snapshot.stats.initial, snapshot.stats
	if snapshot.Corrected != 2 || snapshot.Failed != 0 || d.enabled ||
		int64(oob) != Span/PageBytes*VisibleOOB {
		t.Fatalf("bad readonly preflight: %+v", snapshot)
	}
	if err := executePrepared(context.Background(), d, bundle, snapshot, false, journal, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	var want []int
	for block := 137; block < Blocks; block++ {
		want = append(want, block)
	}
	want = append(want, 0)
	if !reflect.DeepEqual(d.erases, want) || d.writes != 172 ||
		snapshot.stats != tracker || snapshot.stats.initial != initial ||
		d.stats.Corrected != 6 || journal.events[len(journal.events)-1].Stage != "extent-verified-no-reboot" {
		t.Fatalf("wrong resume result: erases=%v writes=%d stats=%+v", d.erases, d.writes, d.stats)
	}
	for block := 1; block <= 136; block++ {
		if d.data[int64(block)*EraseBytes] != 0x22 {
			t.Fatal("already-verified block changed")
		}
	}
	mainReads, oobReads := 0, 0
	for _, event := range journal.events {
		switch event.Stage {
		case "resume-page-main":
			mainReads++
		case "resume-page-oob":
			oobReads++
		default:
			continue
		}
		if event.Absolute != 29822*PageBytes || event.Corrected != 1 || event.Failed != 0 {
			t.Fatalf("wrong pagewise evidence: %+v", event)
		}
	}
	if mainReads != 3 || oobReads != 3 {
		t.Fatalf("missing initial/mapping/final known-page evidence: main=%d oob=%d", mainReads, oobReads)
	}
}

func TestResumeFirstFailureAndCancellation(t *testing.T) {
	for _, failure := range []string{"new-readback-correction", "new-final-correction", "known-final-two", "write-failed-ecc", "before-erase-cancel", "during-erase-cancel", "persist", "journal", "enable-drift", "final-journal-ecc"} {
		t.Run(failure, func(t *testing.T) {
			bundle, d := resumeFixture(t)
			journal := &resumeTestJournal{}
			snapshot, err := prepareResume(context.Background(), d, bundle, io.Discard, journal)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d.onRead = func(offset int64, size int, oob bool) {
				if failure == "new-readback-correction" && d.writes > 0 && !oob {
					d.stats.Corrected += 2
				}
				if len(d.erases) == 172 {
					if failure == "new-final-correction" && offset == EraseBytes && !oob {
						d.stats.Corrected += 2
					}
					if failure == "known-final-two" && offset == 136*EraseBytes+62*PageBytes && !oob {
						d.stats.Corrected += 2
					}
				}
			}
			if failure == "during-erase-cancel" {
				d.onErase = cancel
			}
			if failure == "write-failed-ecc" {
				d.onWrite = func() { d.stats.Failed++ }
			}
			if failure == "enable-drift" {
				d.onEnable = func() { d.data[136*EraseBytes] ^= 1 }
			}
			journal.onRecord = func(event Event) error {
				if failure == "final-journal-ecc" && event.Stage == "extent-verified-no-reboot" {
					d.stats.Failed++
				}
				if event.Stage == "before-erase" {
					if failure == "before-erase-cancel" {
						cancel()
					}
					if failure == "journal" {
						return errors.New("journal failure")
					}
				}
				return nil
			}
			err = executePrepared(ctx, d, bundle, snapshot, false, journal, func() error {
				if failure == "persist" {
					return errors.New("persist failure")
				}
				return nil
			})
			if err == nil {
				t.Fatal("failure accepted")
			}
			wantErases := 0
			switch failure {
			case "new-readback-correction", "write-failed-ecc", "during-erase-cancel":
				wantErases = 1
			case "new-final-correction", "known-final-two", "final-journal-ecc":
				wantErases = 172
			}
			if len(d.erases) != wantErases {
				t.Fatalf("erases=%v error=%v", d.erases, err)
			}
			if failure == "during-erase-cancel" && (d.writes != 1 || d.data[137*EraseBytes] != 0x22) {
				t.Fatal("cancellation interrupted the erased block")
			}
			for _, event := range journal.events {
				if event.Stage == "extent-verified-no-reboot" && failure != "final-journal-ecc" {
					t.Fatal("failure reported success")
				}
			}
		})
	}
}

type resumeReaderFunc func([]byte, int64) (int, error)

func (f resumeReaderFunc) ReadAt(data []byte, offset int64) (int, error) { return f(data, offset) }

func TestResumeMappingQualifiesEveryActualRead(t *testing.T) {
	bundle, d := resumeFixture(t)
	snapshot, err := prepareResume(context.Background(), d, bundle, io.Discard, &resumeTestJournal{})
	if err != nil {
		t.Fatal(err)
	}
	knownCorrections(d, 1)
	master := resumeReaderFunc(func(data []byte, offset int64) (int, error) { return d.ReadAt(data, offset-Start) })
	if err := compareResumePartition(context.Background(), d, master, d, bundle, snapshot); err != nil || d.stats.Corrected != 2 {
		t.Fatalf("bounded master/partition reads failed: stats=%+v error=%v", d.stats, err)
	}
	knownCorrections(d, 2)
	if err := compareResumePartition(context.Background(), d, master, d, bundle, snapshot); err == nil {
		t.Fatal("combined mapping budget accepted two corrections in one actual read")
	}
	// A failed tracker is not reused. Check the independently bracketed second
	// descriptor with a new operation and one valid main read before it.
	snapshot.stats = resumeTracker(t, d)
	knownCorrections(d, 1)
	partition := resumeReaderFunc(func(data []byte, offset int64) (int, error) {
		n, err := d.ReadAt(data, offset)
		if offset == 136*EraseBytes+62*PageBytes {
			d.stats.Corrected++
		}
		return n, err
	})
	if err := compareResumePartition(context.Background(), d, master, partition, bundle, snapshot); err == nil {
		t.Fatal("partition read borrowed the master read allowance")
	}
}

func TestResumeLegacyPolicyStillRejectsCorrectedReadback(t *testing.T) {
	bundle, d := pilotFixture(t, true)
	d.correctedBlock = Blocks - 1
	err := Execute(context.Background(), d, bundle, false, InstallAck, io.Discard, &pilotJournal{}, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "readback or ECC verification") || len(d.erases) != 1 {
		t.Fatalf("original policy did not reject one corrected readback: %v", err)
	}
	t.Log("existing install policy rejects one corrected readback and does not write the header")
}

func TestResumeActualPinnedSourcesAndMixedImage(t *testing.T) {
	bundle, err := Load(os.Getenv("NAND_PILOT_PAYLOAD"), os.Getenv("NAND_PILOT_ORIGINAL"))
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	plan, err := PlanResume(bundle)
	if err != nil || plan.KnownAbsolutePage != 29822 || len(plan.WriteOrder) != 172 {
		t.Fatalf("actual resume plan: %+v error=%v", plan, err)
	}
	// No full-image allocation: the target overlays exact pinned candidate blocks
	// onto the original reader, matching only this interrupted install state.
	for _, corrupt := range []int{-1, 0, 136, 137, 307} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			d := &resumeArtifactDevice{resumeTestDevice: &resumeTestDevice{pilotDevice: &pilotDevice{data: pilotBytes{}, eccBlock: -1, correctedBlock: -1}},
				bundle: bundle, corrupt: corrupt, erased: map[int]bool{}, programmed: map[int64]bool{}}
			journal := &resumeTestJournal{}
			snapshot, err := PrepareResume(context.Background(), d, bundle, ResumePreflightAck, io.Discard, journal)
			if corrupt == -1 {
				if err != nil || snapshot.Corrected != 2 || d.enabled {
					t.Fatalf("actual mixed-state preflight failed: %+v %v", snapshot, err)
				}
				if err := ExecuteResume(context.Background(), d, bundle, ResumeAck, io.Discard, journal, func() error { return nil }); err != nil {
					t.Fatal(err)
				}
				if len(d.erases) != 172 || d.erases[0] != 137 || d.erases[171] != 0 ||
					journal.events[len(journal.events)-1].Detail != ImageHash {
					t.Fatal("full actual-artifact resume did not write only the approved remainder")
				}
			} else if err == nil || d.enabled {
				t.Fatal("actual mixed-image corruption accepted")
			}
		})
	}
	for _, mutate := range []func(){
		func() { bundle.Plan.WriteOrder[0] = 136 },
		func() { bundle.Plan.Profile = "other" },
		func() { bundle.Plan.Blocks[136].ProbeSHA256 = strings.Repeat("0", 64) },
		func() { bundle.Plan.ImageSHA256 = OriginalHash },
		func() { bundle.Plan.OriginalSHA256 = ImageHash },
		func() { bundle.image = bytes.NewReader(nil) },
	} {
		if err := bundle.buildPlan(); err != nil {
			t.Fatal(err)
		}
		mutate()
		if err := ExecuteResume(context.Background(), nil, bundle, ResumeAck, io.Discard, nil, nil); err == nil {
			t.Fatal("source/plan drift accepted before device access")
		}
	}
}

type resumeArtifactDevice struct {
	*resumeTestDevice
	bundle     *Bundle
	corrupt    int
	erased     map[int]bool
	programmed map[int64]bool
}

func (d *resumeArtifactDevice) ReadAt(data []byte, offset int64) (int, error) {
	block := int(offset / EraseBytes)
	reader := d.bundle.original
	if resumeCandidateBlock(block) {
		reader = d.bundle.image
	}
	n, err := reader.ReadAt(data, offset)
	if d.erased[block] {
		copy(data, pilotFF)
		for position := 0; position < len(data); position += PageBytes {
			page := offset + int64(position)
			if d.programmed[page] {
				if _, err := d.bundle.image.ReadAt(data[position:position+PageBytes], page); err != nil {
					return 0, err
				}
			}
		}
	}
	if block == d.corrupt {
		data[0] ^= 1
	}
	if offset == 136*EraseBytes+62*PageBytes {
		d.stats.Corrected++
	}
	return n, err
}

func (d *resumeArtifactDevice) EnableWrites(ctx context.Context, bundle *Bundle, snapshot *Snapshot, journal Journal) error {
	if err := recheckMappingState(ctx, d, bundle, snapshot); err != nil {
		return err
	}
	d.enabled = true
	d.enables++
	return nil
}

func (d *resumeArtifactDevice) Erase(offset int64) error {
	block := int(offset / EraseBytes)
	if resumeCandidateBlock(block) || (block == 0 && len(d.erases) != 171) {
		return fmt.Errorf("erase of protected/early header block")
	}
	if err := d.pilotDevice.Erase(offset); err != nil {
		return err
	}
	d.erased[block] = true
	return nil
}

func (d *resumeArtifactDevice) WriteAt(data []byte, offset int64) (int, error) {
	if !d.enabled || !d.erased[int(offset/EraseBytes)] || len(data) != PageBytes ||
		d.programmed[offset] || bounds(offset, len(data), PageBytes) != nil {
		return 0, fmt.Errorf("unapproved or duplicate page program")
	}
	expected := make([]byte, PageBytes)
	if _, err := d.bundle.image.ReadAt(expected, offset); err != nil || !bytes.Equal(expected, data) {
		return 0, fmt.Errorf("program is not exact candidate: %v", err)
	}
	d.programmed[offset] = true
	d.writes++
	return len(data), nil
}

func TestResumeMTDSnapshotModeIsolation(t *testing.T) {
	for _, failure := range []string{"valid", "ordinary-mtd", "recovery-mtd", "ordinary-snapshot", "restore-snapshot", "incomplete", "failed", "initial-failed", "ordinary-tracker"} {
		t.Run(failure, func(t *testing.T) {
			m := &MTD{resume: true}
			s := &Snapshot{device: m, Hashes: make([]string, Blocks), resume: true, complete: true,
				stats: &statsTracker{resume: true}}
			switch failure {
			case "ordinary-mtd":
				m.resume = false
			case "recovery-mtd":
				m.recovery = true
			case "ordinary-snapshot":
				s.resume = false
			case "restore-snapshot":
				s.restore = true
			case "incomplete":
				s.complete = false
			case "failed":
				s.Failed = 1
			case "initial-failed":
				s.stats.initial.Failed = 1
			case "ordinary-tracker":
				s.stats.resume = false
			}

			err := m.validateSnapshotMode(s)
			if (err == nil) != (failure == "valid") {
				t.Fatalf("wrong mode outcome: %v", err)
			}
		})
	}
}
func TestResumeExactKernelGate(t *testing.T) {
	for _, test := range []struct{ arch, banner string }{
		{"arm", mappingKernelBanner},
		{"amd64", mappingKernelBanner},
		{"arm64", mappingKernelBanner},
		{"arm", ""},
		{"arm", "3.8.13-reinvoke-audio-sd8887"},
		{"arm", mappingKernelBanner + " modified"},
	} {
		called := false
		err := qualifyWriteKernel(test.arch, test.banner, func() error { called = true; return nil })
		want := test.arch == "arm" && test.banner == mappingKernelBanner
		if called != want || (err == nil) != want {
			t.Fatalf("unsupported kernel accepted: %+v %v", test, err)
		}
	}
}

func (d *resumeArtifactDevice) ReadOOB(offset int64) ([]byte, error) {
	if offset == 136*EraseBytes+62*PageBytes {
		d.stats.Corrected++
	}
	return pilotFF[:VisibleOOB], nil
}
