//go:build !nandpilot

package probewrite

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type mockJournal struct {
	events   []Event
	failAt   string
	onRecord func(Event)
}

func (j *mockJournal) Record(event Event) error {
	if event.Stage == j.failAt {
		return errors.New("injected journal failure")
	}
	if j.onRecord != nil {
		j.onRecord(event)
	}
	j.events = append(j.events, event)
	return nil
}

type fakeDevice struct {
	data            []byte
	geometry        Geometry
	stats           Stats
	bad             int
	shortRead       bool
	shortOOB        bool
	unexpectedOOB   bool
	failStats       bool
	failIdentity    bool
	failEnable      bool
	failErase       bool
	failProgram     bool
	shortProgram    bool
	failSync        bool
	falseErase      bool
	falseProgram    bool
	eccPage         int
	eccOnProgram    bool
	enabled         bool
	erases          []int
	writes          []int64
	onEnable        func()
	onErase         func()
	onRead          func(int64)
	onProgram       func()
	failBadQuery    bool
	identityChecks  int
	failIdentityEnd bool
}

func fixture(t *testing.T) (*Bundle, *fakeDevice) {
	t.Helper()
	return twoBlockFixture(t, true)
}

func preservedFixture(t *testing.T) (*Bundle, *fakeDevice) {
	t.Helper()
	return twoBlockFixture(t, false)
}

func twoBlockFixture(t *testing.T, changeSecond bool) (*Bundle, *fakeDevice) {
	t.Helper()
	original := bytes.Repeat([]byte{0xff}, int(Span))
	original[0], original[EraseBytes] = 0x11, 0x22
	image := append([]byte(nil), original[:ImageBytes]...)
	image[0] = 0x33
	if changeSecond {
		image[EraseBytes] = 0x44
	}
	bundle := &Bundle{image: bytes.NewReader(image), original: bytes.NewReader(original)}
	if err := bundle.buildPlan(); err != nil {
		t.Fatal(err)
	}
	device := &fakeDevice{
		data:     append([]byte(nil), original...),
		geometry: Geometry{DeviceBytes, Start, Span, PageBytes, EraseBytes, 64},
		bad:      -1, eccPage: -1,
	}
	return bundle, device
}

func (d *fakeDevice) Geometry() (Geometry, error) { return d.geometry, nil }
func (d *fakeDevice) CheckIdentity() error {
	d.identityChecks++
	if d.failIdentity || (d.failIdentityEnd && len(d.erases) != 0) {
		return errors.New("identity check failed")
	}
	return nil
}
func (d *fakeDevice) ReadAt(data []byte, offset int64) (int, error) {
	if offset < 0 || offset+int64(len(data)) > Span {
		panic("fake backend read outside extent")
	}
	if d.onRead != nil {
		d.onRead(offset)
	}
	copy(data, d.data[offset:offset+int64(len(data))])
	if int(offset/EraseBytes) == d.eccPage {
		d.stats.Failed++
	}
	if d.eccOnProgram && len(d.writes) != 0 {
		d.stats.Corrected++
	}
	if d.shortRead {
		return len(data) - 1, nil
	}
	return len(data), nil
}
func (d *fakeDevice) ReadOOB(offset int64) ([]byte, error) {
	if offset < 0 || offset+PageBytes > Span || offset%PageBytes != 0 {
		panic("fake backend OOB outside extent")
	}
	data := bytes.Repeat([]byte{0xff}, VisibleOOB)
	if d.unexpectedOOB {
		data[7] = 0
	}
	if d.shortOOB {
		data = data[:1]
	}
	return data, nil
}
func (d *fakeDevice) IsBad(offset int64) (bool, error) {
	if offset < 0 || offset+EraseBytes > Span || offset%EraseBytes != 0 {
		panic("bad check outside extent")
	}
	if d.failBadQuery {
		return false, errors.New("bad-block query failed")
	}
	return int(offset/EraseBytes) == d.bad, nil
}
func (d *fakeDevice) Stats() (Stats, error) {
	if d.failStats {
		return Stats{}, errors.New("stats failed")
	}
	return d.stats, nil
}
func (d *fakeDevice) EnableWrites(ctx context.Context, bundle *Bundle, snapshot *Snapshot, journal Journal) error {
	if snapshot == nil || snapshot.device != d || snapshot.stats == nil {
		return errors.New("missing original preflight")
	}
	if d.failEnable {
		return errors.New("partition creation failed")
	}
	d.enabled = true
	if d.onEnable != nil {
		d.onEnable()
	}
	return nil
}
func (d *fakeDevice) Erase(offset int64) error {
	if !d.enabled || offset < 0 || offset+EraseBytes > Span || offset%EraseBytes != 0 {
		panic("unbounded erase")
	}
	d.erases = append(d.erases, int(offset/EraseBytes))
	if d.onErase != nil {
		d.onErase()
	}
	if d.failErase {
		return errors.New("erase failed")
	}
	if !d.falseErase {
		for i := offset; i < offset+EraseBytes; i++ {
			d.data[i] = 0xff
		}
	}
	return nil
}
func (d *fakeDevice) WriteAt(data []byte, offset int64) (int, error) {
	if !d.enabled || offset < 0 || offset+int64(len(data)) > Span || offset%PageBytes != 0 || len(data) != PageBytes {
		panic("unbounded program")
	}
	d.writes = append(d.writes, offset)
	if d.onProgram != nil {
		d.onProgram()
	}
	if d.failProgram {
		return 0, errors.New("program failed")
	}
	if d.shortProgram {
		return len(data) - 1, nil
	}
	if !d.falseProgram {
		for i, value := range data {
			d.data[int(offset)+i] &= value
		}
	}
	return len(data), nil
}
func (d *fakeDevice) Sync() error {
	if d.failSync {
		return errors.New("sync failed")
	}
	return nil
}

func execute(d Device, b *Bundle, restore bool, j Journal) error {
	ack := InstallAck
	if restore {
		ack = RestoreAck
	}
	return Execute(context.Background(), d, b, restore, ack, io.Discard, j, func() error { return nil })
}

func TestPlanAndInstallTwoChangedBlocks(t *testing.T) {
	bundle, device := fixture(t)
	wantOrder := []int{0, 1}
	if HeaderLast {
		wantOrder = []int{1, 0}
	}
	if bundle.Plan.ChangedBlocks != 2 || !reflect.DeepEqual(bundle.Plan.WriteOrder, wantOrder) {
		t.Fatalf("unexpected write order: %+v", bundle.Plan.WriteOrder)
	}
	journal := &mockJournal{}
	if err := execute(device, bundle, false, journal); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(device.erases, wantOrder) || len(device.writes) != 2 {
		t.Fatalf("wrote unchanged blocks: erases=%v pages=%v", device.erases, device.writes)
	}
	if journal.events[len(journal.events)-1].Stage != "extent-verified-no-reboot" {
		t.Fatal("missing final verification")
	}
}

func TestPlanAndInstallPreserveEqualBlocks(t *testing.T) {
	bundle, device := preservedFixture(t)
	preserved := append([]byte(nil), device.data[EraseBytes:]...)
	if bundle.Plan.ChangedBlocks != 1 || bundle.Plan.Blocks[1].Changed ||
		!reflect.DeepEqual(bundle.Plan.WriteOrder, []int{0}) {
		t.Fatalf("unexpected preserved-block plan: %+v", bundle.Plan)
	}
	if err := execute(device, bundle, false, &mockJournal{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(device.erases, []int{0}) || !reflect.DeepEqual(device.writes, []int64{0}) ||
		!bytes.Equal(device.data[EraseBytes:], preserved) {
		t.Fatal("wrote or damaged an unchanged block")
	}
}

func TestPreflightFailuresNeverEnableWrites(t *testing.T) {
	tests := []struct {
		name string
		set  func(*fakeDevice)
	}{
		{"geometry", func(d *fakeDevice) { d.geometry.Span++ }},
		{"wrong device", func(d *fakeDevice) { d.failIdentity = true }},
		{"bad first block", func(d *fakeDevice) { d.bad = 0 }},
		{"bad final block", func(d *fakeDevice) { d.bad = Blocks - 1 }},
		{"bad query", func(d *fakeDevice) { d.failBadQuery = true }},
		{"changed first byte", func(d *fakeDevice) { d.data[0] ^= 1 }},
		{"changed last byte", func(d *fakeDevice) { d.data[len(d.data)-1] ^= 1 }},
		{"short read", func(d *fakeDevice) { d.shortRead = true }},
		{"short OOB", func(d *fakeDevice) { d.shortOOB = true }},
		{"visible metadata", func(d *fakeDevice) { d.unexpectedOOB = true }},
		{"ECC failure", func(d *fakeDevice) { d.eccPage = Blocks - 1 }},
		{"ECC stats error", func(d *fakeDevice) { d.failStats = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundle, device := fixture(t)
			test.set(device)
			if err := execute(device, bundle, false, &mockJournal{}); err == nil {
				t.Fatal("preflight failure accepted")
			}
			if device.enabled || len(device.erases) != 0 || len(device.writes) != 0 {
				t.Fatal("preflight failure reached writable backend")
			}
		})
	}
}

func TestPrepareIsReadOnlyAndCapturesOOB(t *testing.T) {
	bundle, device := fixture(t)
	var oob bytes.Buffer
	snapshot, err := Prepare(context.Background(), device, bundle, &oob, &mockJournal{})
	if err != nil {
		t.Fatal(err)
	}
	if device.enabled || snapshot.Failed != 0 || oob.Len() != int(Span/PageBytes*VisibleOOB) || !allFF(oob.Bytes()) {
		t.Fatal("prepare enabled writes or produced invalid OOB evidence")
	}
}

func TestBadApprovalHasNoDeviceEffect(t *testing.T) {
	bundle, device := fixture(t)
	err := Execute(context.Background(), device, bundle, false, RestoreAck, io.Discard, &mockJournal{}, func() error { return nil })
	if err == nil || device.identityChecks != 0 || device.enabled {
		t.Fatal("wrong approval reached device")
	}
}

func TestJournalAndPersistenceFailures(t *testing.T) {
	for _, stage := range []string{"preflight-block", "preflight-complete", "before-erase", "persist"} {
		t.Run(stage, func(t *testing.T) {
			bundle, device := fixture(t)
			journal := &mockJournal{failAt: stage}
			err := Execute(context.Background(), device, bundle, false, InstallAck, io.Discard, journal, func() error {
				if stage == "persist" {
					return errors.New("persist failed")
				}
				return nil
			})
			if err == nil || len(device.erases) != 0 {
				t.Fatal("lost evidence permitted erase")
			}
		})
	}
}

func TestFailuresStopWithoutNextBlockOrAutomaticRollback(t *testing.T) {
	tests := []struct {
		name string
		set  func(*fakeDevice)
	}{
		{"partition", func(d *fakeDevice) { d.failEnable = true }},
		{"erase", func(d *fakeDevice) { d.failErase = true }},
		{"erase lied", func(d *fakeDevice) { d.falseErase = true }},
		{"program", func(d *fakeDevice) { d.failProgram = true }},
		{"short program", func(d *fakeDevice) { d.shortProgram = true }},
		{"program lied", func(d *fakeDevice) { d.falseProgram = true }},
		{"sync", func(d *fakeDevice) { d.failSync = true }},
		{"ECC on readback", func(d *fakeDevice) { d.eccOnProgram = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundle, device := fixture(t)
			test.set(device)
			journal := &mockJournal{}
			if err := execute(device, bundle, false, journal); err == nil {
				t.Fatal("injected write failure was accepted")
			}
			if len(device.erases) > 1 {
				t.Fatal("continued after failed erase block")
			}
			for _, event := range journal.events {
				if event.Stage == "extent-verified-no-reboot" {
					t.Fatal("reported success after failure")
				}
			}
		})
	}
}

func TestChangedBetweenPreflightAndErase(t *testing.T) {
	bundle, device := fixture(t)
	device.onEnable = func() { device.data[bundle.Plan.WriteOrder[0]*EraseBytes] ^= 1 }
	err := execute(device, bundle, false, &mockJournal{})
	if err == nil || !strings.Contains(err.Error(), "since preflight") || len(device.erases) != 0 {
		t.Fatalf("racing data accepted: %v", err)
	}
}

func TestCancellationFinishesCurrentBlock(t *testing.T) {
	bundle, device := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	device.onErase = cancel
	err := Execute(ctx, device, bundle, false, InstallAck, io.Discard, &mockJournal{}, func() error { return nil })
	first, next := bundle.Plan.WriteOrder[0], bundle.Plan.WriteOrder[1]
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(device.erases, []int{first}) ||
		digest(device.data[first*EraseBytes:(first+1)*EraseBytes]) != bundle.Plan.Blocks[first].ProbeSHA256 ||
		digest(device.data[next*EraseBytes:(next+1)*EraseBytes]) != bundle.Plan.Blocks[next].OriginalSHA256 {
		t.Fatalf("did not finish current block and stop: erases=%v err=%v", device.erases, err)
	}
}

func TestRestoreAfterInterruptedProgram(t *testing.T) {
	if !RestoreSupported {
		t.Skip("explicit restoration is disabled for this profile")
	}
	bundle, device := fixture(t)
	device.data[EraseBytes], device.data[EraseBytes+100] = 0, 0x89
	if err := execute(device, bundle, true, &mockJournal{}); err != nil {
		t.Fatal(err)
	}
	if device.data[EraseBytes] != 0x22 || device.data[EraseBytes+100] != 0xff {
		t.Fatal("rollback did not restore original data")
	}
	if !reflect.DeepEqual(device.erases, []int{1}) {
		t.Fatalf("restore unnecessarily erased original block zero: %v", device.erases)
	}
}

func TestRestoreRejectsDamageOutsidePlannedChangedBlocks(t *testing.T) {
	if !RestoreSupported {
		t.Skip("explicit restoration is disabled for this profile")
	}
	bundle, device := preservedFixture(t)
	device.data[EraseBytes] = 0x12
	if err := execute(device, bundle, true, &mockJournal{}); err == nil || device.enabled {
		t.Fatal("restore accepted corruption in preserved block")
	}
}

func TestRestoreCanRepairECCFailedChangedBlock(t *testing.T) {
	if !RestoreSupported {
		t.Skip("explicit restoration is disabled for this profile")
	}
	bundle, device := fixture(t)
	device.eccPage = 1
	device.onErase = func() { device.eccPage = -1 }
	if err := execute(device, bundle, true, &mockJournal{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(device.erases, []int{1}) {
		t.Fatalf("ECC-failed original-looking bytes were not refreshed: %v", device.erases)
	}
}

func TestPostWriteIdentityFailureIsNotSuccess(t *testing.T) {
	bundle, device := fixture(t)
	device.failIdentityEnd = true
	err := execute(device, bundle, false, &mockJournal{})
	if err == nil || !strings.Contains(err.Error(), "post-write") {
		t.Fatalf("missing final identity check: %v", err)
	}
}

func TestArtifactMutationBeforeEraseRejected(t *testing.T) {
	bundle, device := fixture(t)
	device.onEnable = func() {
		changed := bytes.Repeat([]byte{0xff}, int(ImageBytes))
		bundle.image = bytes.NewReader(changed)
	}
	if err := execute(device, bundle, false, &mockJournal{}); err == nil || len(device.erases) != 0 {
		t.Fatal("artifact mutation reached erase")
	}
}

func TestInventoryChangeCannotBecomeNewBaseline(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeDevice, *mockJournal)
		maxErases int
	}{
		{"between preflight reads", func(d *fakeDevice, j *mockJournal) {
			j.onRecord = func(e Event) {
				if e.Stage == "preflight-block" && e.Block == 0 {
					d.stats.BadBlocks++
				}
			}
		}, 0},
		{"during write enablement", func(d *fakeDevice, j *mockJournal) {
			d.onEnable = func() { d.stats.BBTBlocks++ }
		}, 0},
		{"after before-erase journal", func(d *fakeDevice, j *mockJournal) {
			j.onRecord = func(e Event) {
				if e.Stage == "before-erase" {
					d.stats.BadBlocks++
				}
			}
		}, 0},
		{"during erase", func(d *fakeDevice, j *mockJournal) {
			d.onErase = func() { d.stats.BBTBlocks++ }
		}, 1},
		{"during program", func(d *fakeDevice, j *mockJournal) {
			d.onProgram = func() { d.stats.BadBlocks++ }
		}, 1},
		{"before final verification", func(d *fakeDevice, j *mockJournal) {
			j.onRecord = func(e Event) {
				if e.Stage == "block-verified" && e.Block == 1 {
					d.stats.BBTBlocks++
				}
			}
		}, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundle, device := fixture(t)
			journal := &mockJournal{}
			test.configure(device, journal)
			if err := execute(device, bundle, false, journal); err == nil {
				t.Fatal("inventory change was silently adopted")
			}
			if len(device.erases) > test.maxErases {
				t.Fatalf("continued erasing after inventory changed: %v", device.erases)
			}
		})
	}
}

func TestAlreadyTargetStillChecksBadBlock(t *testing.T) {
	if !RestoreSupported {
		t.Skip("this test exercises the restoration already-original shortcut")
	}
	bundle, device := fixture(t)
	// Restore sees original bytes already present but must still check status.
	device.onEnable = func() { device.bad = 0 }
	if err := execute(device, bundle, true, &mockJournal{}); err == nil {
		t.Fatal("already-target shortcut skipped a newly bad block")
	}
	if len(device.erases) != 0 {
		t.Fatal("erase occurred after bad-block detection")
	}
}

func TestFinalPassChecksPreservedBlockStatus(t *testing.T) {
	bundle, device := preservedFixture(t)
	journal := &mockJournal{onRecord: func(e Event) {
		if e.Stage == "block-verified" && e.Block == 0 {
			device.bad = 1
		}
	}}
	if err := execute(device, bundle, false, journal); err == nil {
		t.Fatal("final verification ignored status of an unchanged block")
	}
}

func TestFinalPassChecksPreservedBlockData(t *testing.T) {
	bundle, device := preservedFixture(t)
	journal := &mockJournal{onRecord: func(e Event) {
		if e.Stage == "block-verified" && e.Block == 0 {
			device.data[EraseBytes] ^= 1
		}
	}}
	err := execute(device, bundle, false, journal)
	if err == nil || !strings.Contains(err.Error(), "final verification failed at block 1") {
		t.Fatalf("final verification ignored preserved-block corruption: %v", err)
	}
	for _, event := range journal.events {
		if event.Stage == "extent-verified-no-reboot" {
			t.Fatal("reported success after preserved-block corruption")
		}
	}
}

func TestCancellationImmediatelyBeforeErase(t *testing.T) {
	for _, stage := range []string{"pre-erase read", "before-erase journal"} {
		t.Run(stage, func(t *testing.T) {
			bundle, device := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			journal := &mockJournal{}
			if stage == "pre-erase read" {
				device.onRead = func(offset int64) {
					if device.enabled && offset == int64(bundle.Plan.WriteOrder[0])*EraseBytes {
						cancel()
					}
				}
			} else {
				journal.onRecord = func(e Event) {
					if e.Stage == "before-erase" {
						cancel()
					}
				}
			}
			err := Execute(ctx, device, bundle, false, InstallAck, io.Discard, journal, func() error { return nil })
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancellation, got %v", err)
			}
			if len(device.erases) != 0 {
				t.Fatalf("started mutation after a pre-erase stop request: %v", device.erases)
			}
		})
	}
}

func TestStatsTrackerRejectsResetBetweenChecks(t *testing.T) {
	device := &fakeDevice{stats: Stats{Corrected: 10, Failed: 2, BadBlocks: 2, BBTBlocks: 4}}
	tracker, err := newStatsTracker(device)
	if err != nil {
		t.Fatal(err)
	}
	device.stats.Corrected = 12
	device.stats.Failed = 3
	if _, err := tracker.check(device); err != nil {
		t.Fatal(err)
	}
	device.stats.Corrected = 11
	if _, err := tracker.check(device); err == nil {
		t.Fatal("corrected counter reset between checks accepted")
	}
	device.stats.Corrected = 12
	device.stats.Failed = 2
	if _, err := tracker.check(device); err == nil {
		t.Fatal("failed counter reset between checks accepted")
	}
}
