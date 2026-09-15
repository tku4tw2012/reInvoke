package probewrite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
)

type Geometry struct {
	DeviceBytes int64
	Start       int64
	Span        int64
	PageBytes   int
	EraseBytes  int
	OOBBytes    int
}

type Stats struct {
	Corrected uint32 `json:"corrected"`
	Failed    uint32 `json:"failed"`
	BadBlocks uint32 `json:"bad_blocks"`
	BBTBlocks uint32 `json:"bbt_blocks"`
}

type Device interface {
	Geometry() (Geometry, error)
	CheckIdentity() error
	ReadAt([]byte, int64) (int, error)
	ReadOOB(int64) ([]byte, error)
	IsBad(int64) (bool, error)
	Stats() (Stats, error)
	EnableWrites(context.Context, *Bundle, *Snapshot, Journal) error
	Erase(int64) error
	WriteAt([]byte, int64) (int, error)
	Sync() error
}

type Event struct {
	Stage     string `json:"stage"`
	Block     int    `json:"block"`
	Absolute  int64  `json:"absolute_offset,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Corrected uint32 `json:"corrected_delta,omitempty"`
	Failed    uint32 `json:"failed_delta,omitempty"`
}

type Journal interface {
	Record(Event) error
}

type Snapshot struct {
	Hashes    []string
	Corrected uint64
	Failed    uint64
	OOBHash   string
	stats     *statsTracker
	device    Device
	restore   bool
	resume    bool
	complete  bool
}

type statsTracker struct {
	initial       Stats
	last          Stats
	resume        bool
	resumeJournal Journal
}

func newStatsTracker(device Device) (*statsTracker, error) {
	stats, err := device.Stats()
	if err != nil {
		return nil, err
	}
	return &statsTracker{initial: stats, last: stats}, nil
}

func (tracker *statsTracker) check(device Device) (Stats, error) {
	return tracker.checkRead(device, 0)
}

func (tracker *statsTracker) checkRead(device Device, correctedLimit uint32) (Stats, error) {
	stats, err := device.Stats()
	if err != nil {
		return Stats{}, err
	}
	if stats.BadBlocks != tracker.initial.BadBlocks || stats.BBTBlocks != tracker.initial.BBTBlocks {
		return Stats{}, fmt.Errorf("bad-block/BBT inventory changed since operation start")
	}
	if stats.Corrected < tracker.last.Corrected || stats.Failed < tracker.last.Failed {
		return Stats{}, fmt.Errorf("ECC statistics reset since previous operation check")
	}
	if tracker.resume && (stats.Failed != tracker.initial.Failed ||
		stats.Corrected-tracker.last.Corrected > correctedLimit) {
		return Stats{}, fmt.Errorf("resume ECC outside the bounded read allowance: corrected=%d limit=%d failed=%d",
			stats.Corrected-tracker.last.Corrected, correctedLimit, stats.Failed-tracker.initial.Failed)
	}
	tracker.last = stats
	return stats, nil
}

func checkedBlockStatus(device Device, index int, tracker *statsTracker) error {
	if _, err := tracker.check(device); err != nil {
		return err
	}
	bad, err := device.IsBad(int64(index) * EraseBytes)
	if err != nil || bad {
		return fmt.Errorf("bad/reserved block check at %d: rejected=%v error=%v", index, bad, err)
	}
	_, err = tracker.check(device)
	return err
}

func checkGeometry(device Device) error {
	actual, err := device.Geometry()
	if err != nil {
		return err
	}
	want := Geometry{DeviceBytes, Start, Span, PageBytes, EraseBytes, 64}
	if actual != want {
		return fmt.Errorf("unexpected MTD geometry: %+v; expected %+v", actual, want)
	}
	return device.CheckIdentity()
}

func checkedRead(device Device, index int, includeOOB bool, tracker *statsTracker) ([]byte, []byte, Stats, error) {
	before, err := tracker.check(device)
	if err != nil {
		return nil, nil, Stats{}, err
	}
	data := make([]byte, EraseBytes)
	offset := int64(index) * EraseBytes
	n, err := device.ReadAt(data, offset)
	if err != nil || n != len(data) {
		return nil, nil, Stats{}, fmt.Errorf("read block %d: bytes=%d error=%v", index, n, err)
	}
	var oob []byte
	if includeOOB {
		oob = make([]byte, 0, EraseBytes/PageBytes*VisibleOOB)
		for page := 0; page < EraseBytes/PageBytes; page++ {
			record, err := device.ReadOOB(offset + int64(page*PageBytes))
			if err != nil || len(record) != VisibleOOB {
				return nil, nil, Stats{}, fmt.Errorf("OOB read block %d page %d: bytes=%d error=%v", index, page, len(record), err)
			}
			oob = append(oob, record...)
		}
	}
	after, err := tracker.check(device)
	if err != nil {
		return nil, nil, Stats{}, err
	}
	if after.Corrected < before.Corrected || after.Failed < before.Failed ||
		after.BadBlocks != before.BadBlocks || after.BBTBlocks != before.BBTBlocks {
		return nil, nil, Stats{}, fmt.Errorf("ECC/BBT statistics reset or bad-block state changed at block %d", index)
	}
	return data, oob, Stats{Corrected: after.Corrected - before.Corrected, Failed: after.Failed - before.Failed}, nil
}

func allFF(data []byte) bool {
	for _, value := range data {
		if value != 0xff {
			return false
		}
	}
	return true
}

func preflight(ctx context.Context, device Device, bundle *Bundle, restore bool, oobOutput io.Writer, journal Journal) (*Snapshot, error) {
	if restore && !RestoreSupported {
		return nil, fmt.Errorf("restore is unsupported by this forward-only profile")
	}
	tracker, err := newStatsTracker(device)
	if err != nil {
		return nil, err
	}
	if err := checkGeometry(device); err != nil {
		return nil, err
	}
	snapshot := &Snapshot{Hashes: make([]string, Blocks), stats: tracker, device: device, restore: restore}
	oobHash := sha256.New()
	for index := 0; index < Blocks; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := checkedBlockStatus(device, index, tracker); err != nil {
			return nil, err
		}
		data, oob, stats, err := checkedRead(device, index, true, tracker)
		if err != nil {
			return nil, err
		}
		record := bundle.Plan.Blocks[index]
		snapshot.Hashes[index] = digest(data)
		snapshot.Corrected += uint64(stats.Corrected)
		snapshot.Failed += uint64(stats.Failed)
		// Recovery explicitly permits damaged data only in our planned changed blocks.
		if !restore || !record.Changed {
			if snapshot.Hashes[index] != record.OriginalSHA256 {
				return nil, fmt.Errorf("original data mismatch at block %d; no erase permitted", index)
			}
			if stats.Failed != 0 || !allFF(oob) {
				return nil, fmt.Errorf("uncorrectable ECC or unexpected visible metadata at block %d", index)
			}
		}
		if _, err := io.CopyN(io.MultiWriter(oobOutput, oobHash), bytes.NewReader(oob), int64(len(oob))); err != nil {
			return nil, fmt.Errorf("save visible OOB: %w", err)
		}
		if err := journal.Record(Event{
			Stage: "preflight-block", Block: index, Absolute: record.AbsoluteOffset,
			Detail: snapshot.Hashes[index], Corrected: stats.Corrected, Failed: stats.Failed,
		}); err != nil {
			return nil, err
		}
	}
	snapshot.OOBHash = fmt.Sprintf("%x", oobHash.Sum(nil))
	if err := journal.Record(Event{Stage: "preflight-complete", Block: -1, Bytes: Span, Detail: snapshot.OOBHash}); err != nil {
		return nil, err
	}
	if _, err := tracker.check(device); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// Prepare never enables a writable descriptor or creates a partition.
func Prepare(ctx context.Context, device Device, bundle *Bundle, oobOutput io.Writer, journal Journal) (*Snapshot, error) {
	return preflight(ctx, device, bundle, false, oobOutput, journal)
}

// Execute requires explicit approval even though the command-line frontend checks it too.
func Execute(ctx context.Context, device Device, bundle *Bundle, restore bool, approval string, oobOutput io.Writer, journal Journal, persistEvidence func() error) error {
	if restore && !RestoreSupported {
		return fmt.Errorf("restore is unsupported by this forward-only profile")
	}
	wantAck := InstallAck
	if restore {
		wantAck = RestoreAck
	}
	if approval != wantAck {
		return fmt.Errorf("explicit %s approval is required", wantAck)
	}
	snapshot, err := preflight(ctx, device, bundle, restore, oobOutput, journal)
	if err != nil {
		return err
	}
	return executePrepared(ctx, device, bundle, snapshot, restore, journal, persistEvidence)
}

func executePrepared(ctx context.Context, device Device, bundle *Bundle, snapshot *Snapshot, restore bool, journal Journal, persistEvidence func() error) error {
	if restore && !RestoreSupported {
		return fmt.Errorf("restore is unsupported by this forward-only profile")
	}
	if snapshot.resume {
		if err := validateSnapshot(device, bundle, snapshot); err != nil {
			return err
		}
		if restore {
			return fmt.Errorf("resume never uses recovery mode")
		}
	}
	if err := persistEvidence(); err != nil {
		return fmt.Errorf("persist preflight evidence: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := device.CheckIdentity(); err != nil {
		return err
	}
	if _, err := snapshot.stats.check(device); err != nil {
		return err
	}
	if err := device.EnableWrites(ctx, bundle, snapshot, journal); err != nil {
		return err
	}
	if _, err := snapshot.stats.check(device); err != nil {
		return err
	}
	for _, index := range bundle.Plan.WriteOrder {
		if snapshot.resume && resumeCandidateBlock(index) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stopped between erase blocks: %w", err)
		}
		offset := int64(index) * EraseBytes
		target, err := bundle.verifiedBlock(index, restore)
		if err != nil {
			return err
		}
		current, oob, stats, err := readForSnapshot(ctx, device, index, snapshot, false)
		if err != nil {
			return err
		}
		if digest(current) != snapshot.Hashes[index] {
			return fmt.Errorf("NAND changed since preflight at block %d; stop", index)
		}
		if !restore && (stats.Failed != 0 || !allFF(oob)) {
			return fmt.Errorf("ECC/metadata changed since preflight at block %d", index)
		}
		if err := checkedBlockStatus(device, index, snapshot.stats); err != nil {
			return err
		}
		if bytes.Equal(current, target) && stats.Failed == 0 && allFF(oob) {
			if err := journal.Record(Event{Stage: "already-target", Block: index}); err != nil {
				return err
			}
			continue
		}
		if err := journal.Record(Event{Stage: "before-erase", Block: index, Absolute: Start + offset}); err != nil {
			return err
		}
		if _, err := snapshot.stats.check(device); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stopped before erasing block %d: %w", index, err)
		}
		if err := device.Erase(offset); err != nil {
			return fmt.Errorf("erase block %d failed; do not reboot automatically: %w", index, err)
		}
		// Finish a block already erased even if cancellation arrived during erase.
		blank, blankOOB, blankStats, err := readForSnapshot(context.Background(), device, index, snapshot, true)
		if err != nil {
			return err
		}
		if !allFF(blank) || !allFF(blankOOB) || blankStats.Failed != 0 {
			return fmt.Errorf("erase verification failed at block %d", index)
		}
		// Once erased, finish this block before honoring a cancellation.
		for page := 0; page < EraseBytes/PageBytes; page++ {
			data := target[page*PageBytes : (page+1)*PageBytes]
			if allFF(data) {
				continue
			}
			n, err := device.WriteAt(data, offset+int64(page*PageBytes))
			if err != nil || n != PageBytes {
				return fmt.Errorf("program block %d page %d: bytes=%d error=%v", index, page, n, err)
			}
			if _, err := snapshot.stats.check(device); err != nil {
				return err
			}
		}
		if err := device.Sync(); err != nil {
			return err
		}
		readback, readbackOOB, readbackStats, err := readForSnapshot(context.Background(), device, index, snapshot, true)
		if err != nil {
			return err
		}
		if !bytes.Equal(readback, target) || !allFF(readbackOOB) ||
			readbackStats.Failed != 0 || (!snapshot.resume && readbackStats.Corrected != 0) {
			return fmt.Errorf("readback or ECC verification failed at block %d; stop", index)
		}
		if err := journal.Record(Event{Stage: "block-verified", Block: index, Absolute: Start + offset, Detail: digest(readback)}); err != nil {
			return err
		}
	}
	finalHash := sha256.New()
	for index := 0; index < Blocks; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkedBlockStatus(device, index, snapshot.stats); err != nil {
			return err
		}
		target, err := bundle.verifiedBlock(index, restore)
		if err != nil {
			return err
		}
		data, oob, stats, err := readForSnapshot(ctx, device, index, snapshot, true)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, target) || !allFF(oob) || stats.Failed != 0 {
			return fmt.Errorf("final verification failed at block %d", index)
		}
		finalHash.Write(data)
	}
	if snapshot.resume && fmt.Sprintf("%x", finalHash.Sum(nil)) != bundle.Plan.ImageSHA256 {
		return fmt.Errorf("resumed extent does not match the pinned payload")
	}
	if err := device.CheckIdentity(); err != nil {
		return fmt.Errorf("post-write boundary/identity check: %w", err)
	}
	if err := device.Sync(); err != nil {
		return err
	}
	if _, err := snapshot.stats.check(device); err != nil {
		return err
	}
	err := journal.Record(Event{Stage: "extent-verified-no-reboot", Block: -1, Bytes: Span, Detail: fmt.Sprintf("%x", finalHash.Sum(nil))})
	if err == nil && snapshot.resume {
		_, err = snapshot.stats.check(device)
	}
	return err
}
