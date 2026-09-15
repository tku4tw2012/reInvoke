package probewrite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

const (
	ResumeAck           = "RESUME-rootfs-pilot-pty01-02920000-04fa0000-page-ecc1"
	ResumePreflightAck  = "READ-ONLY-RESUME-rootfs-pilot-pty01-02920000-04fa0000-page-ecc1"
	resumeLastCandidate = 136
	resumeKnownPage     = 62
	resumeKnownHash     = "4c4df6cd8ad6f6f7e384ad3d54a760d36d6134539cdc2bbdeece9e783edb197c"
)

func resumeCandidateBlock(index int) bool {
	return index >= 1 && index <= resumeLastCandidate
}

func checkResumeApproval(approval, want string) error {
	if !ResumeSupported {
		return fmt.Errorf("resume requires the explicit nandpilot build profile; no device accessed")
	}
	if approval != want {
		return fmt.Errorf("explicit %s approval is required; no device accessed", want)
	}
	return nil
}

// Resume is deliberately not a parameterized recovery operation. Even an
// otherwise valid pilot plan must still refer to the exact open source files.
func validateResumeSources(bundle *Bundle) error {
	if !ResumeSupported || bundle == nil || len(bundle.files) != 2 ||
		Start != 0x02920000 || End != 0x04fa0000 || Blocks != 308 {
		return fmt.Errorf("resume requires the pinned PTY01 pilot source descriptors")
	}
	image, imageOK := bundle.image.(*os.File)
	original, originalOK := bundle.original.(*os.File)
	if !imageOK || !originalOK || image != bundle.files[0] || original != bundle.files[1] {
		return fmt.Errorf("resume source descriptor mismatch")
	}
	if err := checkFile(image, Span, ImageHash); err != nil {
		return err
	}
	if err := checkFile(original, Span, OriginalHash); err != nil {
		return err
	}
	plan := bundle.Plan
	if plan.Start != Start || plan.End != End || plan.Span != Span ||
		plan.Profile != PlanProfileName || plan.FilesystemSHA256 != FilesystemHash ||
		plan.ChangedBlocks != Blocks || len(plan.Blocks) != Blocks || len(plan.WriteOrder) != Blocks ||
		plan.WriteAuthorized {
		return fmt.Errorf("resume plan is not the complete pinned pilot")
	}
	if err := validatePlanHashes(plan); err != nil {
		return err
	}
	for index, record := range plan.Blocks {
		if record.Index != index || record.AbsoluteOffset != Start+int64(index)*EraseBytes ||
			!record.Changed || plan.WriteOrder[index] != (index+1)%Blocks {
			return fmt.Errorf("resume plan geometry/order mismatch at block %d", index)
		}
		for _, restore := range []bool{false, true} {
			if _, err := bundle.verifiedBlock(index, restore); err != nil {
				return err
			}
		}
	}
	if plan.Blocks[resumeLastCandidate].ProbeSHA256 != resumeKnownHash {
		return fmt.Errorf("resume block 136 candidate pin mismatch")
	}
	return nil
}

type ResumePlan struct {
	Plan                Plan   `json:"source_plan"`
	WriteOrder          []int  `json:"resume_write_order"`
	CandidateBlocks     string `json:"required_candidate_blocks"`
	OriginalBlocks      string `json:"required_original_blocks"`
	KnownAbsolutePage   int64  `json:"initial_observed_corrected_page"`
	CorrectionScope     string `json:"correction_scope"`
	MainCorrectionLimit uint32 `json:"per_candidate_page_main_read_limit"`
	OOBCorrectionLimit  uint32 `json:"per_candidate_page_oob_read_limit"`
	Approval            string `json:"resume_approval"`
}

// PlanResume is offline and retains the full source plan alongside the smaller
// fixed write order. It authorizes neither mapping nor writing.
func PlanResume(bundle *Bundle) (*ResumePlan, error) {
	if err := validateResumeSources(bundle); err != nil {
		return nil, err
	}
	plan := &ResumePlan{Plan: bundle.Plan, CandidateBlocks: "1..136", OriginalBlocks: "0,137..307",
		KnownAbsolutePage:   (Start+resumeLastCandidate*EraseBytes)/PageBytes + resumeKnownPage,
		CorrectionScope:     "every candidate page; original bytes retain the existing preflight policy",
		MainCorrectionLimit: 1, OOBCorrectionLimit: 1, Approval: ResumeAck}
	for index := resumeLastCandidate + 1; index < Blocks; index++ {
		plan.WriteOrder = append(plan.WriteOrder, index)
	}
	plan.WriteOrder = append(plan.WriteOrder, 0)
	return plan, nil
}

// resumeIO brackets one actual read. The ordinary tracker remains strict
// between reads; journaling, status checks, mapping and writes cannot absorb ECC.
func resumeIO(device Device, tracker *statsTracker, limit uint32, read func() error) (Stats, error) {
	if tracker == nil || !tracker.resume {
		return Stats{}, fmt.Errorf("resume read requires its operation-wide tracker")
	}
	before, err := tracker.check(device)
	if err != nil {
		return Stats{}, err
	}
	readErr := read()
	after, err := tracker.checkRead(device, limit)
	if err != nil {
		return Stats{}, err
	}
	if readErr != nil {
		return Stats{}, readErr
	}
	return Stats{Corrected: after.Corrected - before.Corrected}, nil
}

func resumeLimit(index, _ int, original bool) uint32 {
	if original && !resumeCandidateBlock(index) {
		// The unchanged original-data preflight policy already permits corrected
		// reads. This never applies to candidate or newly programmed blocks.
		return ^uint32(0)
	}
	return 1
}

func recordResumeRead(tracker *statsTracker, index, page int, oob bool, stats Stats, limit uint32) error {
	if stats.Corrected == 0 || tracker.resumeJournal == nil {
		return nil
	}
	stage, size := "resume-page-main", PageBytes
	if oob {
		stage, size = "resume-page-oob", VisibleOOB
	}
	return tracker.resumeJournal.Record(Event{Stage: stage, Block: index,
		Absolute: Start + int64(index)*EraseBytes + int64(page*PageBytes),
		Bytes:    int64(size), Corrected: stats.Corrected, Detail: fmt.Sprintf("one actual read; corrected limit=%d; failed=0", limit)})
}

func resumeReadMain(ctx context.Context, device Device, reader io.ReaderAt, base int64, index int, original bool, tracker *statsTracker) ([]byte, Stats, error) {
	if index < 0 || index >= Blocks || (base != 0 && base != Start) {
		return nil, Stats{}, fmt.Errorf("resume read outside fixed target")
	}
	data := make([]byte, EraseBytes)
	step := PageBytes
	var total Stats
	for position := 0; position < EraseBytes; position += step {
		if err := ctx.Err(); err != nil {
			return nil, Stats{}, err
		}
		part := data[position : position+step]
		stats, err := resumeIO(device, tracker, resumeLimit(index, position/PageBytes, original), func() error {
			n, err := reader.ReadAt(part, base+int64(index)*EraseBytes+int64(position))
			if err != nil || n != len(part) {
				return fmt.Errorf("resume main read block %d page %d: bytes=%d error=%v", index, position/PageBytes, n, err)
			}
			return nil
		})
		if err != nil {
			return nil, Stats{}, fmt.Errorf("resume main block %d page %d: %w", index, position/PageBytes, err)
		}
		if err := recordResumeRead(tracker, index, position/PageBytes, false, stats, resumeLimit(index, position/PageBytes, original)); err != nil {
			return nil, Stats{}, err
		}
		total.Corrected += stats.Corrected
	}
	return data, total, nil
}

func checkedResumeRead(ctx context.Context, device Device, index int, original bool, tracker *statsTracker) ([]byte, []byte, Stats, error) {
	data, stats, err := resumeReadMain(ctx, device, device, 0, index, original, tracker)
	if err != nil {
		return nil, nil, Stats{}, err
	}
	oob := make([]byte, 0, EraseBytes/PageBytes*VisibleOOB)
	for page := 0; page < EraseBytes/PageBytes; page++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, Stats{}, err
		}
		delta, err := resumeIO(device, tracker, resumeLimit(index, page, original), func() error {
			record, err := device.ReadOOB(int64(index)*EraseBytes + int64(page*PageBytes))
			if err != nil || len(record) != VisibleOOB || !allFF(record) {
				return fmt.Errorf("resume OOB read/metadata block %d page %d: bytes=%d error=%v", index, page, len(record), err)
			}
			oob = append(oob, record...)
			return nil
		})
		if err != nil {
			return nil, nil, Stats{}, fmt.Errorf("resume OOB block %d page %d: %w", index, page, err)
		}
		if err := recordResumeRead(tracker, index, page, true, delta, resumeLimit(index, page, original)); err != nil {
			return nil, nil, Stats{}, err
		}
		stats.Corrected += delta.Corrected
	}
	return data, oob, stats, nil
}

func readForSnapshot(ctx context.Context, device Device, index int, snapshot *Snapshot, programmed bool) ([]byte, []byte, Stats, error) {
	if snapshot.resume {
		return checkedResumeRead(ctx, device, index, !programmed && !resumeCandidateBlock(index), snapshot.stats)
	}
	return checkedRead(device, index, true, snapshot.stats)
}

func prepareResume(ctx context.Context, device Device, bundle *Bundle, output io.Writer, journal Journal) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m, ok := device.(*MTD); ok && (!m.resume || m.recovery) {
		return nil, fmt.Errorf("resume requires a non-recovery resume MTD handle")
	}
	tracker, err := newStatsTracker(device)
	if err != nil {
		return nil, err
	}
	tracker.resume = true
	tracker.resumeJournal = journal
	snapshot := &Snapshot{Hashes: make([]string, Blocks), device: device, stats: tracker, resume: true}
	if err := journal.Record(Event{Stage: "resume-start", Block: -1,
		Detail: fmt.Sprintf("immutable initial stats=%+v; candidate=1..136 original=0,137..307; each candidate page main<=1 OOB<=1", tracker.initial)}); err != nil {
		return nil, err
	}
	if err := checkGeometry(device); err != nil {
		return nil, err
	}
	if _, err := tracker.check(device); err != nil {
		return nil, err
	}
	hash := sha256.New()
	for index := 0; index < Blocks; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := checkedBlockStatus(device, index, tracker); err != nil {
			return nil, err
		}
		expected, err := bundle.verifiedBlock(index, !resumeCandidateBlock(index))
		if err != nil {
			return nil, err
		}
		data, oob, stats, err := readForSnapshot(ctx, device, index, snapshot, false)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(data, expected) {
			return nil, fmt.Errorf("resume mixed-state mismatch at block %d; no erase permitted", index)
		}
		snapshot.Hashes[index] = digest(data)
		snapshot.Corrected += uint64(stats.Corrected)
		if _, err := io.CopyN(io.MultiWriter(output, hash), bytes.NewReader(oob), int64(len(oob))); err != nil {
			return nil, err
		}
		if err := journal.Record(Event{Stage: "resume-preflight-block", Block: index,
			Absolute: Start + int64(index)*EraseBytes, Detail: snapshot.Hashes[index], Corrected: stats.Corrected}); err != nil {
			return nil, err
		}
	}
	snapshot.OOBHash = fmt.Sprintf("%x", hash.Sum(nil))
	if err := journal.Record(Event{Stage: "resume-preflight-complete", Block: -1, Bytes: Span, Detail: snapshot.OOBHash}); err != nil {
		return nil, err
	}
	if _, err := tracker.check(device); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot.complete = true
	return snapshot, nil
}

// PrepareResume performs the fixed mixed-state check without mapping or writes.
func PrepareResume(ctx context.Context, device Device, bundle *Bundle, approval string, output io.Writer, journal Journal) (*Snapshot, error) {
	if err := checkResumeApproval(approval, ResumePreflightAck); err != nil {
		return nil, err
	}
	if err := validateResumeSources(bundle); err != nil {
		return nil, err
	}
	return prepareResume(ctx, device, bundle, output, journal)
}

func ExecuteResume(ctx context.Context, device Device, bundle *Bundle, approval string, output io.Writer, journal Journal, persist func() error) error {
	if err := checkResumeApproval(approval, ResumeAck); err != nil {
		return err
	}
	if err := validateResumeSources(bundle); err != nil {
		return err
	}
	snapshot, err := prepareResume(ctx, device, bundle, output, journal)
	if err != nil {
		return err
	}
	return executePrepared(ctx, device, bundle, snapshot, false, journal, persist)
}

func validateSnapshot(device Device, bundle *Bundle, snapshot *Snapshot) error {
	if snapshot == nil || snapshot.stats == nil || snapshot.device != device ||
		len(snapshot.Hashes) != Blocks || bundle == nil || len(bundle.Plan.Blocks) != Blocks ||
		(snapshot.Failed != 0 && !snapshot.restore) {
		return fmt.Errorf("mapping requires this device's complete successful preflight")
	}
	if snapshot.resume != snapshot.stats.resume ||
		(snapshot.resume && (!ResumeSupported || !snapshot.complete || snapshot.restore ||
			snapshot.Failed != 0 || snapshot.stats.last.Failed != snapshot.stats.initial.Failed)) {
		return fmt.Errorf("resume preflight mode or completeness mismatch")
	}
	if m, ok := device.(*MTD); ok {
		return m.validateSnapshotMode(snapshot)
	}
	return nil
}

func recheckResumeMapping(ctx context.Context, device Device, bundle *Bundle, snapshot *Snapshot) error {
	if _, err := snapshot.stats.check(device); err != nil {
		return err
	}
	if err := checkGeometry(device); err != nil {
		return err
	}
	for index := 0; index < Blocks; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Pin both sources, including candidate data for blocks not yet written.
		candidate, err := bundle.verifiedBlock(index, false)
		if err != nil {
			return err
		}
		original, err := bundle.verifiedBlock(index, true)
		if err != nil {
			return err
		}
		expected := original
		if resumeCandidateBlock(index) {
			expected = candidate
		}
		if digest(expected) != snapshot.Hashes[index] {
			return fmt.Errorf("resume mapping preflight hash mismatch at block %d", index)
		}
		if err := checkedBlockStatus(device, index, snapshot.stats); err != nil {
			return err
		}
		data, _, _, err := readForSnapshot(ctx, device, index, snapshot, false)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, expected) {
			return fmt.Errorf("resume mapping mixed-state changed at block %d", index)
		}
	}
	_, err := snapshot.stats.check(device)
	return err
}

func compareResumePartition(ctx context.Context, device Device, master, partition io.ReaderAt, bundle *Bundle, snapshot *Snapshot) error {
	if err := validateSnapshot(device, bundle, snapshot); err != nil || !snapshot.resume {
		return fmt.Errorf("resume partition comparison requires a resume snapshot: %v", err)
	}
	for index := 0; index < Blocks; index++ {
		if err := checkedBlockStatus(device, index, snapshot.stats); err != nil {
			return err
		}
		original := !resumeCandidateBlock(index)
		main, _, err := resumeReadMain(ctx, device, master, Start, index, original, snapshot.stats)
		if err != nil {
			return err
		}
		relative, _, err := resumeReadMain(ctx, device, partition, 0, index, original, snapshot.stats)
		if err != nil {
			return err
		}
		if !bytes.Equal(main, relative) || digest(main) != snapshot.Hashes[index] {
			return fmt.Errorf("resume partition mapping mismatch at block %d", index)
		}
	}
	return ctx.Err()
}
