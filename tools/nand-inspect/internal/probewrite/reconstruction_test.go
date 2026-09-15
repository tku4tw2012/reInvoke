//go:build !nand2134 && !nandstockroot

package probewrite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

type reconstructionLog struct {
	events []Event
	hook   func(Event) error
}

func (l *reconstructionLog) Record(e Event) error {
	l.events = append(l.events, e)
	if l.hook != nil {
		return l.hook(e)
	}
	return nil
}

func generatedPage(block, page int, main, oob []byte) {
	for i := range main {
		main[i] = 0xff
	}
	for i := range oob {
		oob[i] = 0xff
	}
	// Include data-only, tagged, OOB-only-with-FF-main, and completely blank pages.
	if page%4 == 0 || page%4 == 1 {
		for i := range main {
			main[i] = byte((block + page + i) % 251)
		}
	}
	if page%4 == 1 || page%4 == 2 {
		for i := 1; i < len(oob); i++ {
			oob[i] = byte((block + page + i) % 251)
		}
	}
}

type generatedCapsule struct{ blocks []int }

func (r generatedCapsule) ReadAt(out []byte, offset int64) (int, error) {
	index := int(offset / reconstructionRecordBytes)
	if index < 0 || index >= len(r.blocks) || offset%reconstructionRecordBytes != 0 || len(out) != reconstructionRecordBytes {
		return 0, io.ErrUnexpectedEOF
	}
	for page := 0; page < EraseBytes/PageBytes; page++ {
		generatedPage(r.blocks[index], page, out[page*PageBytes:(page+1)*PageBytes],
			out[EraseBytes+page*VisibleOOB:EraseBytes+(page+1)*VisibleOOB])
	}
	return len(out), nil
}

type reconstructionFake struct {
	stats        Stats
	written      map[int]bool
	targetBlocks map[int]bool
	erased       []int
	programmed   []int
	finished     []int
	enabled      int
	closed       int
	mainReads    int
	oobReads     int
	noopErase    bool
	noopProgram  bool
	shortProgram bool
	shortMain    bool
	shortOOB     bool
	swapOOB      bool
	programErr   error
	readErr      error
	eraseHook    func()
	readHook     func(bool)
}

func newReconstructionFake() *reconstructionFake {
	return &reconstructionFake{stats: Stats{Corrected: 17, Failed: 5, BadBlocks: 2, BBTBlocks: 4},
		written: make(map[int]bool), targetBlocks: make(map[int]bool)}
}
func (d *reconstructionFake) Close() error          { d.closed++; return nil }
func (d *reconstructionFake) Check() error          { return nil }
func (d *reconstructionFake) Stats() (Stats, error) { return d.stats, nil }
func (d *reconstructionFake) IsBadBlock(block int) (bool, error) {
	return block == 1536 || block == 1537 || block >= 2044, nil
}
func (d *reconstructionFake) EnableReconstruction(_ context.Context, _ *reconstructionProof, _ Journal) error {
	d.enabled++
	return nil
}
func (d *reconstructionFake) ReadMain(out []byte, absolute int64) (int, error) {
	d.mainReads++
	if d.readHook != nil {
		d.readHook(false)
	}
	if d.readErr != nil {
		return 0, d.readErr
	}
	block, page := int(absolute/EraseBytes), int(absolute%EraseBytes)/PageBytes
	for i := range out {
		out[i] = 0xff
	}
	if d.targetBlocks[block] || d.written[block*64+page] {
		var oob [VisibleOOB]byte
		generatedPage(block, page, out, oob[:])
		if d.shortProgram && len(out) > 10 {
			out[10] ^= 1
		}
	}
	if d.shortMain {
		return len(out) - 1, nil
	}
	return len(out), nil
}
func (d *reconstructionFake) ReadSpare(out []byte, absolute int64) (int, error) {
	d.oobReads++
	if d.readHook != nil {
		d.readHook(true)
	}
	block, page := int(absolute/EraseBytes), int(absolute%EraseBytes)/PageBytes
	for i := range out {
		out[i] = 0xff
	}
	if d.targetBlocks[block] || d.written[block*64+page] {
		var main [PageBytes]byte
		generatedPage(block, page, main[:], out)
		if d.swapOOB {
			copy(out, main[:VisibleOOB])
		}
	}
	if d.shortOOB {
		return len(out) - 1, nil
	}
	return len(out), nil
}
func (d *reconstructionFake) EraseBlock(block int) error {
	d.erased = append(d.erased, block)
	if !d.noopErase {
		delete(d.targetBlocks, block)
		for page := 0; page < 64; page++ {
			delete(d.written, block*64+page)
		}
	}
	if d.eraseHook != nil {
		d.eraseHook()
	}
	return nil
}
func (d *reconstructionFake) ProgramPage(block, page int, main, oob []byte) error {
	d.programmed = append(d.programmed, block*64+page)
	if d.programErr != nil {
		return d.programErr
	}
	if len(main) != PageBytes || len(oob) != VisibleOOB || oob[0] != 0xff {
		return fmt.Errorf("wrong page ABI")
	}
	var expectedMain [PageBytes]byte
	var expectedOOB [VisibleOOB]byte
	generatedPage(block, page, expectedMain[:], expectedOOB[:])
	if !bytes.Equal(main, expectedMain[:]) || !bytes.Equal(oob, expectedOOB[:]) {
		return fmt.Errorf("main/OOB page placement mismatch")
	}
	if !d.noopProgram {
		d.written[block*64+page] = true
	}
	return nil
}
func (d *reconstructionFake) FinishBlock(block int) error {
	d.finished = append(d.finished, block)
	return nil
}

func syntheticReconstruction(t *testing.T) (*ReconstructionBundle, *reconstructionFake) {
	t.Helper()
	blocks := []int{1, reconstructionFirst, 1596}
	b := &ReconstructionBundle{validated: true, payload: generatedCapsule{blocks}, records: make(map[int]reconstructionRecord)}
	b.plan.WriteOrder = []int{reconstructionFirst, 1596, 1}
	blankMain, blankOOB := bytes.Repeat([]byte{255}, EraseBytes), bytes.Repeat([]byte{255}, 2048)
	buffer := make([]byte, reconstructionRecordBytes)
	for index, block := range blocks {
		if _, err := b.payload.ReadAt(buffer, int64(index)*reconstructionRecordBytes); err != nil {
			t.Fatal(err)
		}
		b.records[block] = reconstructionRecord{Block: block, Start: int64(block) * EraseBytes,
			PayloadOffset: int64(index) * reconstructionRecordBytes, PayloadBytes: reconstructionRecordBytes,
			CurrentDataSHA256: digest(blankMain), CurrentOOBSHA256: digest(blankOOB),
			TargetDataSHA256: digest(buffer[:EraseBytes]), TargetOOBSHA256: digest(buffer[EraseBytes:])}
	}
	for _, state := range []string{"current", "after-first", "target"} {
		mainHash, oobHash, viewHash := sha256.New(), sha256.New(), sha256.New()
		for block := 0; block < int(DeviceBytes/EraseBytes); block++ {
			main, oob := blankMain, blankOOB
			r, changed := b.records[block]
			if changed && (state == "target" || (state == "after-first" && block == reconstructionFirst)) {
				b.payload.ReadAt(buffer, r.PayloadOffset)
				main, oob = buffer[:EraseBytes], buffer[EraseBytes:]
			}
			mainHash.Write(main)
			oobHash.Write(oob)
			if int64(block)*EraseBytes >= reconstructionStart && int64(block)*EraseBytes < reconstructionEnd {
				viewHash.Write(main)
			}
		}
		main, oob, view := fmt.Sprintf("%x", mainHash.Sum(nil)), fmt.Sprintf("%x", oobHash.Sum(nil)), fmt.Sprintf("%x", viewHash.Sum(nil))
		switch state {
		case "current":
			b.plan.CurrentMainSHA256, b.plan.CurrentOOBSHA256, b.plan.CurrentViewSHA256 = main, oob, view
		case "after-first":
			b.plan.AfterFirstMainSHA256, b.plan.AfterFirstOOBSHA256, b.plan.AfterFirstViewSHA256 = main, oob, view
		case "target":
			b.plan.TargetMainSHA256, b.plan.TargetOOBSHA256 = main, oob
		}
	}
	return b, newReconstructionFake()
}

func blockProof(b *ReconstructionBundle, d *reconstructionFake) *reconstructionProof {
	return &reconstructionProof{device: d, bundle: b, tracker: &reconstructionTracker{device: d, initial: d.stats, last: d.stats}, action: "qualify-first"}
}

func TestReconstructionABIAndBounds(t *testing.T) {
	var req reconstructionWriteRequest
	if unsafe.Sizeof(req) != 48 || unsafe.Offsetof(req.Mode) != 40 || unsafe.Offsetof(req.Padding) != 41 ||
		unsafe.Offsetof(req.Data) != 24 || unsafe.Offsetof(req.OOB) != 32 || reconstructionMemWrite != 0xc0304d18 {
		t.Fatal("MEMWRITE ABI mismatch")
	}
	main, oob := make([]byte, PageBytes), bytes.Repeat([]byte{255}, VisibleOOB)
	req, err := reconstructionPageRequest(0, main, oob)
	if err != nil || req.Mode != 0 || req.Len != 2048 || req.OOBLen != 32 || req.Padding != [7]byte{} ||
		req.Data != uint64(uintptr(unsafe.Pointer(&main[0]))) || req.OOB != uint64(uintptr(unsafe.Pointer(&oob[0]))) {
		t.Fatalf("wrong PLACE request: %+v %v", req, err)
	}
	for _, offset := range []int64{-1, 1, reconstructionSpan} {
		if _, err := reconstructionPageRequest(offset, main, oob); err == nil {
			t.Fatal("bad offset accepted")
		}
	}
	for _, size := range []int{0, 31, 64} {
		if _, err := reconstructionPageRequest(0, main, make([]byte, size)); err == nil {
			t.Fatal("wrong OOB size accepted")
		}
	}
	oob[0] = 0
	if _, err := reconstructionPageRequest(0, main, oob); err == nil {
		t.Fatal("bad-block mark accepted")
	}
	for _, block := range []int{-1, 0, 1536, 1537, 2041, 2042, 2043, 2044, 2045, 2046, 2047, 2048} {
		if reconstructionTargetBlock(block) {
			t.Fatalf("protected/out-of-view block %d accepted", block)
		}
	}
	_, data := partitionRangeArg(1, "owned", 0, reconstructionStart, reconstructionSpan)
	if binary.LittleEndian.Uint64(data[:8]) != 0x20000 ||
		binary.LittleEndian.Uint64(data[8:16])+binary.LittleEndian.Uint64(data[:8]) != 0xff20000 {
		t.Fatal("wrong partition range")
	}
	d := &ReconstructionMTD{}
	if d.EraseBlock(reconstructionFirst) == nil || d.ProgramPage(reconstructionFirst, 0, main, oob) == nil ||
		d.FinishBlock(reconstructionFirst) == nil {
		t.Fatal("non-owner write accepted")
	}
}

func TestReconstructionTwoPhasesAndStateIsolation(t *testing.T) {
	b, d := syntheticReconstruction(t)
	log := &reconstructionLog{}
	if err := ExecuteReconstruction(context.Background(), d, b, "complete", ReconstructionCompleteAck, log); err == nil || d.enabled != 0 {
		t.Fatal("complete accepted original state")
	}
	if err := ExecuteReconstruction(context.Background(), d, b, "qualify-first", ReconstructionFirstAck, log); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.erased, []int{1573}) || !reflect.DeepEqual(d.finished, []int{1573}) {
		t.Fatalf("first phase wrote %v", d.erased)
	}
	if len(d.programmed) != 48 {
		t.Fatalf("FF/FF skip or tagged FF-main page wrong: %d programs", len(d.programmed))
	}
	if err := ExecuteReconstruction(context.Background(), d, b, "qualify-first", ReconstructionFirstAck, log); err == nil {
		t.Fatal("first phase repeated")
	}
	if err := ExecuteReconstruction(context.Background(), d, b, "complete", ReconstructionCompleteAck, log); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.erased, []int{1573, 1596, 1}) {
		t.Fatalf("wrong explicit write order: %v", d.erased)
	}
	before := len(d.erased)
	if err := ExecuteReconstruction(context.Background(), d, b, "verify-target", "", log); err != nil {
		t.Fatal(err)
	}
	if len(d.erased) != before {
		t.Fatal("verify-target wrote NAND")
	}
}

func TestReconstructionFailuresAndCancellation(t *testing.T) {
	b, _ := syntheticReconstruction(t)
	for _, name := range []string{"ready-fail-noop", "short-program", "swapped-oob", "raw-program-error", "short-main", "short-oob", "raw-read-error", "cancel-before", "cancel-after", "transport-before", "transport-after"} {
		t.Run(name, func(t *testing.T) {
			d := newReconstructionFake()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			log := &reconstructionLog{}
			switch name {
			case "ready-fail-noop":
				d.noopProgram = true // READY|FAIL lost by the kernel: apparent success, unchanged bytes.
			case "short-program":
				d.shortProgram = true
			case "swapped-oob":
				d.swapOOB = true
			case "raw-program-error":
				d.programErr = syscall.EIO
			case "short-main":
				d.shortMain = true
			case "short-oob":
				d.shortOOB = true
			case "raw-read-error":
				d.readErr = syscall.EBADMSG
			case "cancel-before":
				log.hook = func(e Event) error {
					if e.Stage == "before-erase" {
						cancel()
					}
					return nil
				}
			case "cancel-after":
				d.eraseHook = cancel
			case "transport-before":
				log.hook = func(e Event) error {
					if e.Stage == "before-erase" {
						return syscall.EPIPE
					}
					return nil
				}
			case "transport-after":
				log.hook = func(e Event) error {
					if e.Stage == "after-block" {
						return syscall.EPIPE
					}
					return nil
				}
			}
			err := reconstructionWriteBlock(ctx, blockProof(b, d), reconstructionFirst, log)
			if err == nil {
				t.Fatal("failure reported success")
			}
			if name == "cancel-after" || name == "transport-after" {
				if !reflect.DeepEqual(d.finished, []int{1573}) || len(d.programmed) != 48 {
					t.Fatalf("erased block not finished: %+v", d)
				}
			}
			if name == "cancel-before" || name == "transport-before" || strings.Contains(name, "short-main") || name == "short-oob" || name == "raw-read-error" {
				if len(d.erased) != 0 {
					t.Fatal("erase started after pre-erase failure")
				}
			}
			if name == "raw-program-error" && (len(d.programmed) != 1 || len(d.finished) != 0) {
				t.Fatal("hard failure retried/finished")
			}
			_ = CloseReconstruction(d, log, err)
			if d.closed != 1 {
				t.Fatal("failure skipped cleanup")
			}
		})
	}
	d := newReconstructionFake()
	d.targetBlocks[reconstructionFirst], d.noopErase = true, true
	record := b.records[reconstructionFirst]
	record.CurrentDataSHA256, record.CurrentOOBSHA256 = record.TargetDataSHA256, record.TargetOOBSHA256
	b.records[reconstructionFirst] = record
	if err := reconstructionWriteBlock(context.Background(), blockProof(b, d), reconstructionFirst, &reconstructionLog{}); err == nil ||
		len(d.programmed) != 0 || len(d.finished) != 0 {
		t.Fatal("false-success erase was not rejected before programming")
	}
}

func TestReconstructionCorrectionAllowanceIsPerActualRead(t *testing.T) {
	for _, test := range []struct {
		name             string
		main, oob        uint32
		fail, reset, bad bool
		want             bool
	}{
		{"one-each", 1, 1, false, false, false, true},
		{"two-main", 2, 0, false, false, false, false},
		{"two-oob", 0, 2, false, false, false, false},
		{"new-failed", 0, 0, true, false, false, false},
		{"counter-reset", 0, 0, false, true, false, false},
		{"bad-inventory", 0, 0, false, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := newReconstructionFake()
			tracker := &reconstructionTracker{device: d, initial: d.stats, last: d.stats}
			d.readHook = func(oob bool) {
				if oob {
					d.stats.Corrected += test.oob
				} else {
					d.stats.Corrected += test.main
				}
				if test.fail {
					d.stats.Failed++
				}
				if test.reset {
					d.stats.Corrected = 0
				}
				if test.bad {
					d.stats.BadBlocks++
				}
			}
			err := reconstructionReadBlock(context.Background(), d, tracker, 1573, make([]byte, reconstructionRecordBytes))
			if (err == nil) != test.want {
				t.Fatalf("allowance: %v", err)
			}
			if test.want && (d.mainReads != 64 || d.oobReads != 64 || d.stats.Corrected != 145) {
				t.Fatal("reads were not individually counted")
			}
		})
	}
}

type damagedCapsule struct {
	source io.ReaderAt
	mode   string
}

func (r damagedCapsule) ReadAt(out []byte, off int64) (int, error) {
	n, err := r.source.ReadAt(out, off)
	if err != nil {
		return n, err
	}
	switch r.mode {
	case "truncated":
		return n - 1, nil
	case "tamper":
		out[0] ^= 1
	case "swap":
		copy(out[EraseBytes:], out[:2048])
	case "bbm":
		out[EraseBytes] = 0
	}
	return n, nil
}

func TestReconstructionCapsuleTamperAndApprovalNoAccess(t *testing.T) {
	b, _ := syntheticReconstruction(t)
	original := b.payload
	for _, mode := range []string{"truncated", "tamper", "swap", "bbm"} {
		b.payload = damagedCapsule{original, mode}
		if b.readTarget(1573, make([]byte, reconstructionRecordBytes)) == nil {
			t.Fatalf("%s accepted", mode)
		}
	}
	for _, action := range []string{"qualify-first", "complete", "restore", "resume", "unknown"} {
		if ExecuteReconstruction(context.Background(), nil, nil, action, "wrong", nil) == nil {
			t.Fatal("unapproved action accepted")
		}
	}
	if _, err := decodeReconstructionManifest([]byte("{}")); err == nil {
		t.Fatal("un-pinned manifest accepted")
	}
}

func TestReconstructionRealPinnedManifestAndCapsule(t *testing.T) {
	realReconstructionDirectory := privateArchiveFixture(
		t, "build", "artifacts", "september7-stock-adb-reconstruction-20260911",
	)
	b, err := LoadReconstruction(realReconstructionDirectory+"/RESTORE-PLAN.json", realReconstructionDirectory+"/target-blocks-data-oob32.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	first, _ := b.phaseOrder("qualify-first")
	rest, _ := b.phaseOrder("complete")
	if !reflect.DeepEqual(first, []int{1573}) || len(rest) != 865 ||
		!reflect.DeepEqual(rest[len(rest)-8:], []int{8, 7, 6, 5, 4, 3, 2, 1}) {
		t.Fatal("actual phase order mismatch")
	}
	seen := map[int]bool{1573: true}
	for index, block := range rest {
		if _, ok := b.records[block]; !ok || seen[block] || !reconstructionTargetBlock(block) ||
			block != b.plan.WriteOrder[index+1] {
			t.Fatalf("complete phase is not the exact 865-block whitelist/order at %d", index)
		}
		seen[block] = true
	}
	if len(seen) != 866 {
		t.Fatal("phase whitelist incomplete")
	}
	tagged := 0
	for _, r := range b.records {
		if !r.TargetOOBAllFF {
			tagged++
		}
		if !reconstructionTargetBlock(r.Block) {
			t.Fatal("actual protected record")
		}
	}
	if tagged != 81 {
		t.Fatalf("expected 81 tagged app blocks, got %d", tagged)
	}
	if b.plan.CurrentMainSHA256 != "4113074a844a0d8406bdb2ff771b104ae17b1a768bb51755d8e6f75dae8838ad" ||
		b.plan.AfterFirstMainSHA256 != "678e88a949e9d6e70318a0c37d1fa8ba5e4852754c17871bf274985c5c80c00d" ||
		b.plan.TargetMainSHA256 != "33f16be0dcbc42b92221471ab5685d84868ebff6f6ce7dabf3b66adcd638489f" {
		t.Fatal("state pins differ")
	}
	var out bytes.Buffer
	if err := b.PrintPlan(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"writeAuthorization":false`) {
		t.Fatal("plan grants write authorization")
	}
}

type syncBuffer struct {
	bytes.Buffer
	fail   error
	synced int
}

func (b *syncBuffer) Sync() error { b.synced++; return b.fail }

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) { return 0, syscall.EPIPE }

type shortOutput struct{}

func (shortOutput) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestReconstructionJournalDurabilityAndCleanup(t *testing.T) {
	for _, output := range []io.Writer{failingOutput{}, shortOutput{}} {
		file := &syncBuffer{}
		log := &ReconstructionJournal{File: file, Output: output}
		d := newReconstructionFake()
		if err := log.Record(Event{Stage: "before-erase"}); err == nil {
			t.Fatal("transport failure accepted")
		}

		if file.synced != 1 || !strings.Contains(file.String(), "before-erase") {
			t.Fatal("stdout preceded durable journal")
		}
		if err := CloseReconstruction(d, log, errors.New("partial block")); err == nil || d.closed != 1 {
			t.Fatal("cleanup failure semantics")
		}
		if !strings.Contains(file.String(), "cleanup-complete-no-reboot") {
			t.Fatal("cleanup not persisted despite broken transport")
		}
	}
}

func TestReconstructionProtectedBlocksAndCancellationBeforeAccess(t *testing.T) {
	before := &reconstructionSnapshot{blocks: make([]reconstructionBlockHash, 2048)}
	for _, block := range []int{0, 1536, 1537, 1573, 2041, 2042, 2043, 2044, 2045, 2046, 2047} {
		after := &reconstructionSnapshot{blocks: append([]reconstructionBlockHash(nil), before.blocks...)}
		after.blocks[block].oob = "changed"
		if reconstructionUnchanged(before, after, []int{1596, 1}) == nil {
			t.Fatalf("protected/unchanged block %d mutation accepted", block)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := newReconstructionFake()
	if err := ExecuteReconstruction(ctx, d, &ReconstructionBundle{validated: true}, "qualify-first", ReconstructionFirstAck, &reconstructionLog{}); !errors.Is(err, context.Canceled) || d.mainReads != 0 || d.enabled != 0 {
		t.Fatalf("cancellation before access: %v", err)
	}
}
