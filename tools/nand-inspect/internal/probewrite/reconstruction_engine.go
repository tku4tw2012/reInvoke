package probewrite

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
)

type ReconstructionDevice interface {
	io.Closer
	Check() error
	ReadMain([]byte, int64) (int, error)
	ReadSpare([]byte, int64) (int, error)
	IsBadBlock(int) (bool, error)
	Stats() (Stats, error)
	EnableReconstruction(context.Context, *reconstructionProof, Journal) error
	EraseBlock(int) error
	ProgramPage(int, int, []byte, []byte) error
	FinishBlock(int) error
}

type reconstructionTracker struct {
	device        ReconstructionDevice
	initial, last Stats
}

func (t *reconstructionTracker) check(limit uint32) error {
	now, err := t.device.Stats()
	if err != nil {
		return fmt.Errorf("ECCGETSTATS: %w", err)
	}
	if now.Failed != t.initial.Failed || now.Corrected < t.last.Corrected ||
		now.Corrected-t.last.Corrected > limit ||
		now.BadBlocks != t.initial.BadBlocks || now.BBTBlocks != t.initial.BBTBlocks {
		return fmt.Errorf("ECC/BBT violation: initial=%+v previous=%+v now=%+v correction-limit=%d",
			t.initial, t.last, now, limit)
	}
	t.last = now
	return nil
}

func (t *reconstructionTracker) read(operation func() error) error {
	if err := t.check(0); err != nil {
		return err
	}
	raw := operation()
	if err := t.check(1); err != nil {
		return fmt.Errorf("read error=%v; %w", raw, err)
	}
	return raw
}

type reconstructionBlockHash struct{ main, oob string }
type reconstructionSnapshot struct {
	main, oob, view string
	blocks          []reconstructionBlockHash
}

type reconstructionProof struct {
	device   ReconstructionDevice
	bundle   *ReconstructionBundle
	tracker  *reconstructionTracker
	snapshot *reconstructionSnapshot
	action   string
}

func reconstructionReadBlock(ctx context.Context, d ReconstructionDevice, t *reconstructionTracker, block int, buffer []byte) error {
	if block < 0 || block >= int(DeviceBytes/EraseBytes) || len(buffer) != reconstructionRecordBytes {
		return fmt.Errorf("invalid whole-block read %d", block)
	}
	for page := 0; page < EraseBytes/PageBytes; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		offset := int64(block)*EraseBytes + int64(page*PageBytes)
		main := buffer[page*PageBytes : (page+1)*PageBytes]
		if err := t.read(func() error {
			n, err := d.ReadMain(main, offset)
			if err != nil || n != len(main) {
				return fmt.Errorf("main read block=%d page=%d offset=%#x bytes=%d raw-error=%v", block, page, offset, n, err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("main block=%d page=%d absolute=%#x: %w", block, page, offset, err)
		}
		oob := buffer[EraseBytes+page*VisibleOOB : EraseBytes+(page+1)*VisibleOOB]
		if err := t.read(func() error {
			n, err := d.ReadSpare(oob, offset)
			if err != nil || n != len(oob) {
				return fmt.Errorf("OOB read block=%d page=%d offset=%#x bytes=%d raw-error=%v", block, page, offset, n, err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("OOB block=%d page=%d absolute=%#x: %w", block, page, offset, err)
		}
	}
	return nil
}

func reconstructionBadInventory(d ReconstructionDevice, t *reconstructionTracker) error {
	if err := t.check(0); err != nil {
		return err
	}
	for block := 0; block < int(DeviceBytes/EraseBytes); block++ {
		bad, err := d.IsBadBlock(block)
		want := block == 1536 || block == 1537 || block >= 2044
		if err != nil || bad != want {
			return fmt.Errorf("bad/reserved inventory block=%d got=%v want=%v error=%v", block, bad, want, err)
		}
	}
	return t.check(0)
}

func reconstructionScan(ctx context.Context, d ReconstructionDevice, t *reconstructionTracker, log Journal) (*reconstructionSnapshot, error) {
	if err := d.Check(); err != nil {
		return nil, err
	}
	if err := reconstructionBadInventory(d, t); err != nil {
		return nil, err
	}
	s := &reconstructionSnapshot{blocks: make([]reconstructionBlockHash, int(DeviceBytes/EraseBytes))}
	mainHash, oobHash, viewHash := sha256.New(), sha256.New(), sha256.New()
	buffer := make([]byte, reconstructionRecordBytes)
	for block := range s.blocks {
		if err := reconstructionReadBlock(ctx, d, t, block, buffer); err != nil {
			return nil, err
		}
		main, oob := buffer[:EraseBytes], buffer[EraseBytes:]
		mainHash.Write(main)
		oobHash.Write(oob)
		offset := int64(block) * EraseBytes
		if offset >= reconstructionStart && offset < reconstructionEnd {
			viewHash.Write(main)
		}
		s.blocks[block] = reconstructionBlockHash{digest(main), digest(oob)}
		if block%32 == 31 {
			if err := log.Record(Event{Stage: "scan-progress", Block: block,
				Corrected: t.last.Corrected - t.initial.Corrected,
				Detail:    "whole main + visible OOB; independent <=1 correction per actual main/OOB page read"}); err != nil {
				return nil, err
			}
		}
	}
	s.main, s.oob, s.view = fmt.Sprintf("%x", mainHash.Sum(nil)), fmt.Sprintf("%x", oobHash.Sum(nil)), fmt.Sprintf("%x", viewHash.Sum(nil))
	return s, t.check(0)
}

func (b *ReconstructionBundle) matchSnapshot(s *reconstructionSnapshot, state string) error {
	if s == nil || len(s.blocks) != int(DeviceBytes/EraseBytes) {
		return fmt.Errorf("incomplete reconstruction fingerprint")
	}
	p := &b.plan
	main, oob, view := p.CurrentMainSHA256, p.CurrentOOBSHA256, p.CurrentViewSHA256
	switch state {
	case "current":
	case "after-first":
		if ReconstructionSinglePhase {
			return fmt.Errorf("single-phase profile has no after-first fingerprint state")
		}
		main, oob, view = p.AfterFirstMainSHA256, p.AfterFirstOOBSHA256, p.AfterFirstViewSHA256
	case "target":
		main, oob, view = p.TargetMainSHA256, p.TargetOOBSHA256, ""
	default:
		return fmt.Errorf("unsupported fingerprint state %q", state)
	}
	if s.main != main || s.oob != oob || (view != "" && s.view != view) {
		return fmt.Errorf("%s fingerprint mismatch: main=%s OOB=%s view=%s", state, s.main, s.oob, s.view)
	}
	for block, r := range b.records {
		want := reconstructionBlockHash{r.CurrentDataSHA256, r.CurrentOOBSHA256}
		if state == "target" || (state == "after-first" && block == reconstructionFirst) {
			want = reconstructionBlockHash{r.TargetDataSHA256, r.TargetOOBSHA256}
		}
		if s.blocks[block] != want {
			return fmt.Errorf("%s per-block fingerprint mismatch at %d", state, block)
		}
	}
	return nil
}

func reconstructionUnchanged(before, after *reconstructionSnapshot, order []int) error {
	changed := make(map[int]bool, len(order))
	for _, block := range order {
		if !reconstructionTargetBlock(block) || changed[block] {
			return fmt.Errorf("invalid changed-block set: %d", block)
		}
		changed[block] = true
	}
	if len(before.blocks) != int(DeviceBytes/EraseBytes) || len(after.blocks) != len(before.blocks) {
		return fmt.Errorf("incomplete protected-region verification")
	}
	for block := range before.blocks {
		if !changed[block] && before.blocks[block] != after.blocks[block] {
			return fmt.Errorf("unchanged/protected block %d changed", block)
		}
	}
	return nil
}

func reconstructionWriteBlock(ctx context.Context, proof *reconstructionProof, block int, log Journal) error {
	d, b, t := proof.device, proof.bundle, proof.tracker
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.Check(); err != nil {
		return err
	}
	if err := t.check(0); err != nil {
		return err
	}
	record, ok := b.records[block]
	if !ok || !reconstructionTargetBlock(block) {
		return fmt.Errorf("block %d not in fixed whitelist", block)
	}
	bad, err := d.IsBadBlock(block)
	if err != nil || bad {
		return fmt.Errorf("refuse erase of bad/reserved block %d: %v", block, err)
	}
	target, actual := make([]byte, reconstructionRecordBytes), make([]byte, reconstructionRecordBytes)
	if err := b.readTarget(block, target); err != nil {
		return err
	}
	if err := reconstructionReadBlock(ctx, d, t, block, actual); err != nil {
		return err
	}
	if digest(actual[:EraseBytes]) != record.CurrentDataSHA256 || digest(actual[EraseBytes:]) != record.CurrentOOBSHA256 {
		return fmt.Errorf("current data/OOB changed before erase of block %d", block)
	}
	if err := t.check(0); err != nil {
		return err
	}
	if err := log.Record(Event{Stage: "before-erase", Block: block, Absolute: record.Start,
		Bytes: EraseBytes, Detail: "current and target main/OOB verified; no retry or rollback"}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	before := t.last
	if err := d.EraseBlock(block); err != nil {
		return fmt.Errorf("erase block %d raw-error=%w; block may be partial", block, err)
	}
	// Once erase starts, finish this block even after cancellation/transport loss.
	// Hard I/O errors still stop immediately; there is no repair or retry.
	blockContext := context.Background()
	if err := t.check(0); err != nil {
		return err
	}
	if err := reconstructionReadBlock(blockContext, d, t, block, actual); err != nil {
		return err
	}
	if !allFF(actual) {
		return fmt.Errorf("erase readback failed at block %d (including false-success/no-op erase)", block)
	}
	for page := 0; page < EraseBytes/PageBytes; page++ {
		main := target[page*PageBytes : (page+1)*PageBytes]
		oob := target[EraseBytes+page*VisibleOOB : EraseBytes+(page+1)*VisibleOOB]
		if allFF(main) && allFF(oob) {
			continue
		}
		if err := d.ProgramPage(block, page, main, oob); err != nil {
			return fmt.Errorf("MEMWRITE block=%d page=%d absolute=%#x raw-error=%w; block may be partial",
				block, page, record.Start+int64(page*PageBytes), err)
		}
		if err := t.check(0); err != nil {
			return err
		}
	}
	if err := reconstructionReadBlock(blockContext, d, t, block, actual); err != nil {
		return err
	}
	if digest(actual[:EraseBytes]) != record.TargetDataSHA256 || digest(actual[EraseBytes:]) != record.TargetOOBSHA256 {
		return fmt.Errorf("program readback failed at block %d (including false-success/no-op/short write)", block)
	}
	if err := d.FinishBlock(block); err != nil {
		return err
	}
	if err := t.check(0); err != nil {
		return err
	}
	if err := log.Record(Event{Stage: "after-block", Block: block, Absolute: record.Start,
		Corrected: t.last.Corrected - before.Corrected, Failed: t.last.Failed - before.Failed,
		Detail: "fresh full main/OOB verified; main=" + record.TargetDataSHA256 + " OOB=" + record.TargetOOBSHA256}); err != nil {
		return err
	}
	return ctx.Err()
}

func reconstructionRequiredState(action string, matchesCurrent bool) (string, error) {
	switch action {
	case "preflight":
		if !ReconstructionSinglePhase && !matchesCurrent {
			return "after-first", nil
		}
		return "current", nil
	case "verify-target":
		return "target", nil
	case "apply":
		if ReconstructionSinglePhase {
			return "current", nil
		}
	case "qualify-first":
		if !ReconstructionSinglePhase {
			return "current", nil
		}
	case "complete":
		if !ReconstructionSinglePhase {
			return "after-first", nil
		}
	}
	return "", fmt.Errorf("unsupported reconstruction state action %q", action)
}

func ExecuteReconstruction(ctx context.Context, d ReconstructionDevice, b *ReconstructionBundle, action, approval string, log Journal) error {
	if err := CheckReconstructionAction(action, approval); err != nil {
		return err
	}
	if action == "plan" || d == nil || b == nil || !b.validated || log == nil {
		return fmt.Errorf("hardware execution requires a validated bundle, device and journal")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	initial, err := d.Stats()
	if err != nil {
		return err
	}
	t := &reconstructionTracker{device: d, initial: initial, last: initial}
	if err := log.Record(Event{Stage: "phase-start", Block: -1,
		Detail: fmt.Sprintf("%s; initial-stats=%+v; %s", action, initial, reconstructionLimitation)}); err != nil {
		return err
	}
	s, err := reconstructionScan(ctx, d, t, log)
	if err != nil {
		return err
	}
	state, err := reconstructionRequiredState(action, !ReconstructionSinglePhase && b.matchSnapshot(s, "current") == nil)
	if err != nil {
		return err
	}
	if err := b.matchSnapshot(s, state); err != nil {
		return err
	}
	if err := log.Record(Event{Stage: "fingerprints-verified", Block: -1,
		Detail: state + " main=" + s.main + " OOB=" + s.oob + " view=" + s.view}); err != nil {
		return err
	}
	if action == "preflight" || action == "verify-target" {
		return log.Record(Event{Stage: "phase-complete", Block: -1, Detail: action + " read-only; cleanup pending"})
	}
	proof := &reconstructionProof{d, b, t, s, action}
	if err := d.EnableReconstruction(ctx, proof, log); err != nil {
		return err
	}
	order, err := b.phaseOrder(action)
	if err != nil {
		return err
	}
	for _, block := range order {
		if err := reconstructionWriteBlock(ctx, proof, block, log); err != nil {
			return err
		}
	}
	after, err := reconstructionScan(ctx, d, t, log)
	if err != nil {
		return err
	}
	state = "target"
	if action == "qualify-first" {
		state = "after-first"
	}
	if err := b.matchSnapshot(after, state); err != nil {
		return err
	}
	if err := reconstructionUnchanged(s, after, order); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return log.Record(Event{Stage: "phase-complete", Block: -1,
		Corrected: t.last.Corrected - initial.Corrected, Failed: t.last.Failed - initial.Failed,
		Detail: action + " main=" + after.main + " OOB=" + after.oob + "; unchanged blocks verified; cleanup pending; " + reconstructionLimitation})
}
