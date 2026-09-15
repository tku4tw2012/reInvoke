package probewrite

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/regularfile"
)

const (
	reconstructionStart       = int64(0x00020000)
	reconstructionSpan        = reconstructionEnd - reconstructionStart
	reconstructionRecordBytes = EraseBytes + EraseBytes/PageBytes*VisibleOOB
)

type reconstructionRecord struct {
	Block             int    `json:"block"`
	Start             int64  `json:"start"`
	Region            string `json:"region"`
	DataDiff          bool   `json:"dataDiff"`
	OOBDiff           bool   `json:"oobDiff"`
	TargetDataAllFF   bool   `json:"targetDataAllFF"`
	TargetOOBAllFF    bool   `json:"targetOobAllFF"`
	CurrentDataSHA256 string `json:"currentDataSHA256"`
	TargetDataSHA256  string `json:"targetDataSHA256"`
	CurrentOOBSHA256  string `json:"currentOobSHA256"`
	TargetOOBSHA256   string `json:"targetOobSHA256"`
	PayloadOffset     int64  `json:"payloadOffset"`
	PayloadBytes      int    `json:"payloadBytes"`
}

type reconstructionManifest struct {
	Schema               int    `json:"schema"`
	Status               string `json:"status"`
	Baseline             string `json:"baseline"`
	DeviceBytes          int64  `json:"deviceBytes"`
	EraseBytes           int    `json:"eraseBytes"`
	PageBytes            int    `json:"pageBytes"`
	OOBBytes             int    `json:"oobBytes"`
	ViewStart            int64  `json:"viewStart"`
	ViewEnd              int64  `json:"viewEnd"`
	CurrentMainSHA256    string `json:"currentMainSHA256"`
	CurrentOOBSHA256     string `json:"currentOobSHA256"`
	TargetMainSHA256     string `json:"targetMainSHA256"`
	TargetOOBSHA256      string `json:"targetOobSHA256"`
	CurrentViewSHA256    string `json:"currentViewSHA256"`
	AfterFirstMainSHA256 string `json:"afterFirstMainSHA256,omitempty"`
	AfterFirstOOBSHA256  string `json:"afterFirstOobSHA256,omitempty"`
	AfterFirstViewSHA256 string `json:"afterFirstViewSHA256,omitempty"`
	SourceMainSHA256     string `json:"sourceMainSHA256"`
	Diagnostic           struct {
		Purpose                  string `json:"purpose"`
		FilesystemSHA256         string `json:"filesystemSHA256"`
		InitSHA256               string `json:"initSHA256"`
		ChangedSourceBlocks      []int  `json:"changedSourceBlocks"`
		ChangedInitLines         []int  `json:"changedInitLines"`
		ChangedInitBytePositions int    `json:"changedInitBytePositions"`
		USBProduct               string `json:"usbProduct"`
	} `json:"diagnostic"`
	Payload struct {
		File   string `json:"file"`
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
		Format string `json:"format"`
	} `json:"payload"`
	FirstBlock             int      `json:"firstBlock,omitempty"`
	WriteOrder             []int    `json:"writeOrder"`
	ProtectedBlocks        []int    `json:"protectedBlocks"`
	KnownBadBlocks         []int    `json:"knownBadBlocks"`
	UncertainCapturedPages []int64  `json:"uncertainCapturedPages"`
	Phases                 []string `json:"phases"`
	PageWrite              struct {
		IOCTL       string `json:"ioctl"`
		Request     uint32 `json:"request"`
		StructBytes int    `json:"structBytes"`
		DataBytes   int    `json:"dataBytes"`
		OOBBytes    int    `json:"oobBytes"`
		Mode        int    `json:"mode"`
		ModeName    string `json:"modeName"`
	} `json:"pageWrite"`
	Limits struct {
		CorrectedPerRead  int  `json:"correctedPerRead"`
		NewFailedECC      int  `json:"newFailedECC"`
		AutomaticRetry    bool `json:"automaticRetry"`
		AutomaticRollback bool `json:"automaticRollback"`
	} `json:"limits"`
	WriteAuthorization bool                   `json:"writeAuthorization"`
	Records            []reconstructionRecord `json:"records"`
}

// ReconstructionBundle retains pinned regular-file descriptors, never a full capsule.
type ReconstructionBundle struct {
	plan      reconstructionManifest
	payload   io.ReaderAt
	files     []*os.File
	records   map[int]reconstructionRecord
	validated bool
}

func CheckReconstructionAction(action, approval string) error {
	if err := checkReconstructionActionMode(action, approval); err != nil {
		return err
	}
	return requireReconstructionProfile()
}

func requireReconstructionProfile() error {
	if !reconstructionProfileSealed {
		return fmt.Errorf("reconstruction profile is unsealed and disabled; final compiled pins/count are required; no device or input accessed")
	}
	for _, pin := range []string{ReconstructionManifestHash, ReconstructionPayloadHash, reconstructionTargetHash} {
		data, err := hex.DecodeString(pin)
		if err != nil || len(data) != sha256.Size {
			return fmt.Errorf("invalid compiled reconstruction seal")
		}
	}
	if reconstructionCount <= 0 || reconstructionCount > int(reconstructionSpan/EraseBytes) ||
		reconstructionManifestSize <= 0 || reconstructionPayloadSize != int64(reconstructionCount)*reconstructionRecordBytes {
		return fmt.Errorf("invalid compiled reconstruction sizes/count")
	}
	return nil
}

func checkReconstructionActionMode(action, approval string) error {
	want := ""
	switch action {
	case "plan", "preflight", "verify-target":
	case "apply":
		if !ReconstructionSinglePhase {
			return fmt.Errorf("apply requires a separately compiled single-phase profile; no device accessed")
		}
		want = ReconstructionApplyAck
	case "qualify-first":
		if ReconstructionSinglePhase {
			return fmt.Errorf("single-phase profile does not permit a qualification write; no device accessed")
		}
		want = ReconstructionFirstAck
	case "complete":
		if ReconstructionSinglePhase {
			return fmt.Errorf("single-phase profile does not support complete; no device accessed")
		}
		want = ReconstructionCompleteAck
	default:
		return fmt.Errorf("unsupported reconstruction action %q; no device accessed", action)
	}
	if approval != want {
		return fmt.Errorf("%s requires exact confirmation %q; no device accessed", action, want)
	}
	return nil
}

func reconstructionProtected(block int) bool {
	return block == 0 || block == 1536 || block == 1537 || block >= 2044
}

func reconstructionTargetBlock(block int) bool {
	return block >= 1 && int64(block+1)*EraseBytes <= reconstructionEnd && !reconstructionProtected(block)
}

func decodeReconstructionManifest(data []byte) (reconstructionManifest, error) {
	var plan reconstructionManifest
	if err := requireReconstructionProfile(); err != nil {
		return plan, err
	}
	if int64(len(data)) != reconstructionManifestSize || digest(data) != ReconstructionManifestHash {
		return plan, fmt.Errorf("%s manifest size/SHA-256 mismatch", reconstructionBaseline)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, fmt.Errorf("manifest decode: %w", err)
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return plan, fmt.Errorf("manifest has trailing content: %v", err)
	}
	return plan, nil
}

func LoadReconstruction(manifestPath, payloadPath string) (_ *ReconstructionBundle, result error) {
	if err := requireReconstructionProfile(); err != nil {
		return nil, err
	}
	b := &ReconstructionBundle{records: make(map[int]reconstructionRecord)}
	defer func() {
		if result != nil {
			if err := b.Close(); err != nil {
				result = fmt.Errorf("%v; close sources: %w", result, err)
			}
		}
	}()
	for _, path := range []string{manifestPath, payloadPath} {
		file, err := regularfile.Open(path)
		if err != nil {
			return nil, err
		}
		b.files = append(b.files, file)
	}
	if err := checkFile(b.files[0], reconstructionManifestSize, ReconstructionManifestHash); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.NewSectionReader(b.files[0], 0, reconstructionManifestSize))
	if err != nil {
		return nil, err
	}
	b.plan, err = decodeReconstructionManifest(data)
	if err != nil {
		return nil, err
	}
	if err := checkFile(b.files[1], reconstructionPayloadSize, ReconstructionPayloadHash); err != nil {
		return nil, err
	}
	b.payload = b.files[1]
	if err := b.validate(); err != nil {
		return nil, err
	}
	b.validated = true
	return b, nil
}

func (b *ReconstructionBundle) Close() error {
	var result error
	for _, file := range b.files {
		if err := file.Close(); err != nil {
			result = fmt.Errorf("close input (previous=%v): %w", result, err)
		}
	}
	b.files = nil
	b.validated = false
	return result
}

func (b *ReconstructionBundle) RequireRAMInputs() error {
	if len(b.files) != 2 {
		return fmt.Errorf("pinned source descriptors unavailable")
	}
	for _, file := range b.files {
		if err := RequireRAMPath(fmt.Sprintf("/proc/self/fd/%d", file.Fd())); err != nil {
			return err
		}
	}
	return nil
}

func (b *ReconstructionBundle) validate() error {
	if err := requireReconstructionProfile(); err != nil {
		return err
	}
	p := &b.plan
	if p.Schema != 1 || p.Baseline != reconstructionBaseline || p.DeviceBytes != DeviceBytes ||
		p.EraseBytes != EraseBytes || p.PageBytes != PageBytes || p.OOBBytes != VisibleOOB ||
		p.ViewStart != reconstructionStart || p.ViewEnd != reconstructionEnd ||
		len(p.Records) != reconstructionCount || len(p.WriteOrder) != reconstructionCount ||
		p.Payload.Bytes != reconstructionPayloadSize || p.Payload.SHA256 != ReconstructionPayloadHash ||
		p.PageWrite.Request != reconstructionMemWrite || p.PageWrite.StructBytes != 48 ||
		p.PageWrite.DataBytes != PageBytes || p.PageWrite.OOBBytes != VisibleOOB || p.PageWrite.Mode != 0 ||
		p.Limits.CorrectedPerRead != 1 || p.Limits.NewFailedECC != 0 ||
		p.Limits.AutomaticRetry || p.Limits.AutomaticRollback || p.WriteAuthorization {
		return fmt.Errorf("manifest does not describe the fixed %s reconstruction", reconstructionBaseline)
	}
	if err := validateReconstructionIdentity(p); err != nil {
		return err
	}
	b.records = make(map[int]reconstructionRecord, len(p.Records))
	previous := -1
	for index, record := range p.Records {
		if !reconstructionTargetBlock(record.Block) || record.Block <= previous ||
			record.Start != int64(record.Block)*EraseBytes ||
			record.PayloadOffset != int64(index)*reconstructionRecordBytes ||
			record.PayloadBytes != reconstructionRecordBytes ||
			record.DataDiff != (record.CurrentDataSHA256 != record.TargetDataSHA256) ||
			record.OOBDiff != (record.CurrentOOBSHA256 != record.TargetOOBSHA256) ||
			(!record.DataDiff && !record.OOBDiff) {
			return fmt.Errorf("invalid reconstruction record %d", index)
		}
		if err := validateReconstructionRecordOOB(record); err != nil {
			return err
		}
		b.records[record.Block] = record
		previous = record.Block
	}
	seen := make(map[int]bool)
	for _, block := range p.WriteOrder {
		if _, ok := b.records[block]; !ok || seen[block] {
			return fmt.Errorf("invalid or repeated write-order block %d", block)
		}
		seen[block] = true
	}
	if !ReconstructionSinglePhase {
		if len(p.WriteOrder) < 8 {
			return fmt.Errorf("sealed update must include all eight preboot copies")
		}
		for i := 0; i < 8; i++ {
			if p.WriteOrder[len(p.WriteOrder)-8+i] != 8-i {
				return fmt.Errorf("preboot copies must be last, in order 8..1")
			}
		}
	}
	hash := sha256.New()
	buffer := make([]byte, reconstructionRecordBytes)
	for _, record := range p.Records {
		if err := b.readTarget(record.Block, buffer); err != nil {
			return err
		}
		hash.Write(buffer)
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != ReconstructionPayloadHash {
		return fmt.Errorf("capsule changed during record validation")
	}
	if !ReconstructionSinglePhase {
		first := b.records[reconstructionFirst]
		if first.TargetDataAllFF || first.TargetOOBAllFF {
			return fmt.Errorf("qualification block must exercise real main data and OOB tags")
		}
	}
	return nil
}

func validateReconstructionRecordOOB(record reconstructionRecord) error {
	if ReconstructionSinglePhase && (record.OOBDiff || !record.TargetOOBAllFF ||
		record.CurrentOOBSHA256 != record.TargetOOBSHA256 ||
		record.TargetOOBSHA256 != "d0ff1b294b5288d1ae1421eadf5b2d38a8752b76d472ff30bed9028e25b1c5b8") {
		return fmt.Errorf("single-phase update must preserve all-FF boot OOB at block %d", record.Block)
	}
	return nil
}

func (b *ReconstructionBundle) readTarget(block int, buffer []byte) error {
	record, ok := b.records[block]
	if !ok || !reconstructionTargetBlock(block) || len(buffer) != reconstructionRecordBytes {
		return fmt.Errorf("target is not a whitelisted complete block: %d", block)
	}
	n, err := b.payload.ReadAt(buffer, record.PayloadOffset)
	if err != nil || n != len(buffer) {
		return fmt.Errorf("capsule block %d: bytes=%d error=%v", block, n, err)
	}
	main, oob := buffer[:EraseBytes], buffer[EraseBytes:]
	if digest(main) != record.TargetDataSHA256 || digest(oob) != record.TargetOOBSHA256 ||
		allFF(main) != record.TargetDataAllFF || allFF(oob) != record.TargetOOBAllFF {
		return fmt.Errorf("capsule block %d data/OOB hash or FF flag mismatch", block)
	}
	if ReconstructionSinglePhase && !allFF(oob) {
		return fmt.Errorf("single-phase capsule must not program OOB tags at block %d", block)
	}
	for page := 0; page < EraseBytes/PageBytes; page++ {
		if oob[page*VisibleOOB] != 0xff {
			return fmt.Errorf("capsule block %d page %d contains a non-FF bad-block marker", block, page)
		}
	}
	return nil
}

func (b *ReconstructionBundle) phaseOrder(action string) ([]int, error) {
	if ReconstructionSinglePhase {
		if action != "apply" {
			return nil, fmt.Errorf("single-phase profile has only the apply write phase")
		}
		return append([]int(nil), b.plan.WriteOrder...), nil
	}
	switch action {
	case "qualify-first":
		return []int{reconstructionFirst}, nil
	case "complete":
		return append([]int(nil), b.plan.WriteOrder[1:]...), nil
	default:
		return nil, fmt.Errorf("not a reconstruction write phase: %q", action)
	}
}

func (b *ReconstructionBundle) PrintPlan(output io.Writer) error {
	if err := requireReconstructionProfile(); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(struct {
		ManifestSHA256 string                 `json:"manifestSHA256"`
		Limitation     string                 `json:"limitation"`
		FirstAck       string                 `json:"qualifyFirstConfirmation,omitempty"`
		CompleteAck    string                 `json:"completeConfirmation,omitempty"`
		ApplyAck       string                 `json:"applyConfirmation,omitempty"`
		Plan           reconstructionManifest `json:"plan"`
	}{ReconstructionManifestHash, reconstructionLimitation, ReconstructionFirstAck, ReconstructionCompleteAck, ReconstructionApplyAck, b.plan})
}
