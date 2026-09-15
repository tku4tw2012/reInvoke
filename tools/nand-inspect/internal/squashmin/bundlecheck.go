package squashmin

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/regularfile"
)

type checkedBundle struct {
	proposal        interface{}
	originalBlocks  []byte
	candidateBlocks []byte
	blockCount      int
	changedBytes    int
	imageHash       string
}

func readBundleFile(path string, max int64) ([]byte, error) {
	f, err := regularfile.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > max {
		return nil, fmt.Errorf("%s exceeds size limit", filepath.Base(path))
	}
	return io.ReadAll(io.LimitReader(f, max+1))
}

func pinnedAllocation(path string, source []byte) ([]byte, error) {
	f, err := regularfile.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() != 256*1024*1024 {
		return nil, fmt.Errorf("wrong capture size")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", digest.Sum(nil)) != CaptureSHA256 {
		return nil, fmt.Errorf("wrong capture SHA-256")
	}
	allocation := make([]byte, RootfsAllocation)
	if _, err := f.ReadAt(allocation, RootfsOffset); err != nil {
		return nil, err
	}
	if !bytes.Equal(allocation[:len(source)], source) {
		return nil, fmt.Errorf("source image does not match pinned capture")
	}
	return allocation, nil
}

// Derive the footprint from byte positions, not the proposal's block list or
// the builder's footprint calculation. Every complete block includes the
// original capture padding, even where it lies outside the SquashFS image.
func deriveBundle(source, candidate, allocation []byte, patch Result) (checkedBundle, error) {
	var checked checkedBundle
	if len(source) != ImageBytes || len(candidate) != ImageBytes || len(allocation) != RootfsAllocation {
		return checked, fmt.Errorf("invalid image/allocation sizes")
	}
	type differences struct{ count, first, last int }
	diff := make(map[int]*differences)
	for pos := range source {
		if source[pos] == candidate[pos] {
			continue
		}
		block, offset := pos/EraseBytes, pos%EraseBytes
		if diff[block] == nil {
			diff[block] = &differences{first: offset}
		}
		diff[block].count++
		diff[block].last = offset
		checked.changedBytes++
	}
	if len(diff) == 0 {
		return checked, fmt.Errorf("candidate has no changed blocks")
	}
	var indexes []int
	for index := range diff {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	blocks := make([]map[string]interface{}, 0, len(indexes))
	for _, index := range indexes {
		start := index * EraseBytes
		original := allocation[start : start+EraseBytes]
		replacement := append([]byte(nil), original...)
		end := start + EraseBytes
		if end > len(candidate) {
			end = len(candidate)
		}
		copy(replacement, candidate[start:end])
		checked.originalBlocks = append(checked.originalBlocks, original...)
		checked.candidateBlocks = append(checked.candidateBlocks, replacement...)
		blocks = append(blocks, map[string]interface{}{
			"rootfs_index": index, "capture_index": (RootfsOffset + start) / EraseBytes,
			"capture_start": RootfsOffset + start, "changed_bytes": diff[index].count,
			"first_difference_in_block": diff[index].first, "last_difference_in_block": diff[index].last,
			"original_sha256": hash(original), "candidate_sha256": hash(replacement),
		})
	}
	candidateAllocationHash := sha256.New()
	candidateAllocationHash.Write(candidate)
	candidateAllocationHash.Write(allocation[len(candidate):])
	purpose, err := Purpose(patch)
	if err != nil {
		return checked, err
	}
	expected := map[string]interface{}{
		"purpose":        purpose,
		"write_approved": false, "runtime_verified": false,
		"signature_status": "unknown; no boot/signature acceptance claim",
		"image_bytes":      len(candidate), "rootfs_offset": RootfsOffset,
		"allocation_bytes": len(allocation), "erase_bytes": EraseBytes,
		"capture_sha256": CaptureSHA256, "allocation_original_sha256": hash(allocation),
		"allocation_candidate_sha256":      fmt.Sprintf("%x", candidateAllocationHash.Sum(nil)),
		"unchanged_allocation_tail_sha256": hash(allocation[len(source):]),
		"changed_bytes":                    checked.changedBytes, "patch": patch, "blocks": blocks,
	}
	encoded, err := json.Marshal(expected)
	if err != nil {
		return checked, err
	}
	checked.proposal, err = strictJSON(encoded)
	checked.blockCount, checked.imageHash = len(blocks), hash(candidate)
	return checked, err
}

func prepareBundle(sourcePath, capturePath, candidateDir, gzipPath string) (checkedBundle, error) {
	var checked checkedBundle
	source, err := readBundleFile(sourcePath, ImageBytes)
	if err != nil {
		return checked, err
	}
	if len(source) != ImageBytes || hash(source) != ImageSHA256 {
		return checked, fmt.Errorf("wrong source image size or SHA-256")
	}
	allocation, err := pinnedAllocation(capturePath, source)
	if err != nil {
		return checked, err
	}
	candidate, err := readBundleFile(filepath.Join(candidateDir, "minimal-probe.squashfs"), ImageBytes)
	if err != nil {
		return checked, err
	}
	// Replay only the reviewed file/fragment transformation, not its manifest
	// generation. This binds every patch.location/hash/compression field too.
	rebuilt, patch, err := Patch(source, gzipPath)
	if err != nil {
		return checked, err
	}
	if !bytes.Equal(candidate, rebuilt) {
		rebuilt, patch, err = PatchStockADB(source, gzipPath)
		if err != nil {
			return checked, err
		}
		if !bytes.Equal(candidate, rebuilt) {
			return checked, fmt.Errorf("full candidate differs from canonical reviewed patch")
		}
	}
	return deriveBundle(source, candidate, allocation, patch)
}

// Read every key explicitly: encoding/json's usual map/struct decode silently
// accepts duplicate keys. Comparing complete trees also rejects omitted false/
// zero fields, unknown fields, nulls, extra blocks and incorrect block ordering.
func strictJSON(data []byte) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) (interface{}, error)
	value = func(depth int) (interface{}, error) {
		if depth > 16 {
			return nil, fmt.Errorf("JSON nesting too deep")
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch token {
		case json.Delim('{'):
			object := make(map[string]interface{})
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("non-string JSON key")
				}
				if _, found := object[name]; found {
					return nil, fmt.Errorf("duplicate JSON key %q", name)
				}
				object[name], err = value(depth + 1)
				if err != nil {
					return nil, err
				}
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("invalid JSON object")
			}
			return object, nil
		case json.Delim('['):
			var array []interface{}
			for decoder.More() {
				item, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				array = append(array, item)
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("invalid JSON array")
			}
			return array, nil
		default:
			if _, bad := token.(json.Delim); bad {
				return nil, fmt.Errorf("unexpected JSON delimiter")
			}
			return token, nil
		}
	}
	out, err := value(0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON value or invalid suffix")
	}
	return out, nil
}

func firstMismatch(path string, want, got interface{}) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	switch w := want.(type) {
	case map[string]interface{}:
		if g, ok := got.(map[string]interface{}); ok {
			var keys []string
			for key := range w {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if mismatch := firstMismatch(path+"."+key, w[key], g[key]); mismatch != "" {
					return mismatch
				}
			}
			for key := range g {
				if _, ok := w[key]; !ok {
					return path + "." + key + " (unexpected field)"
				}
			}
		}
	case []interface{}:
		if g, ok := got.([]interface{}); ok && len(w) == len(g) {
			for i := range w {
				if mismatch := firstMismatch(fmt.Sprintf("%s[%d]", path, i), w[i], g[i]); mismatch != "" {
					return mismatch
				}
			}
		}
	}
	return path
}

func (checked checkedBundle) verify(proposal, original, candidate []byte) error {
	actual, err := strictJSON(proposal)
	if err != nil {
		return fmt.Errorf("PROPOSAL.json: %w", err)
	}
	if mismatch := firstMismatch("proposal", checked.proposal, actual); mismatch != "" {
		return fmt.Errorf("%s does not match recomputed capture/image evidence", mismatch)
	}
	if !bytes.Equal(original, checked.originalBlocks) {
		return fmt.Errorf("changed-blocks-original.bin does not equal complete pinned-capture erase blocks in recomputed order")
	}
	if !bytes.Equal(candidate, checked.candidateBlocks) {
		return fmt.Errorf("changed-blocks-candidate.bin does not equal full-candidate erase blocks with pinned-capture padding in recomputed order")
	}
	return nil
}

// VerifyBundle validates, but never writes, the bundle or its source inputs.
// The externally supplied metadata never selects which bytes are compared.
func VerifyBundle(sourcePath, capturePath, candidateDir, gzipPath string, output io.Writer) error {
	checked, err := prepareBundle(sourcePath, capturePath, candidateDir, gzipPath)
	if err != nil {
		return err
	}
	metadata, err := readBundleFile(filepath.Join(candidateDir, "PROPOSAL.json"), 65536)
	if err != nil {
		return err
	}
	original, err := readBundleFile(filepath.Join(candidateDir, "changed-blocks-original.bin"), int64(len(checked.originalBlocks)))
	if err != nil {
		return err
	}
	candidate, err := readBundleFile(filepath.Join(candidateDir, "changed-blocks-candidate.bin"), int64(len(checked.candidateBlocks)))
	if err != nil {
		return err
	}
	if err := checked.verify(metadata, original, candidate); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "PASS pinned capture/image bundle binding: %d blocks, %d changed bytes, %d complete payload bytes\nPASS every proposal field, block order/offset/hash/content and allocation padding recomputed\nCandidate SHA-256: %s\n",
		checked.blockCount, checked.changedBytes, len(checked.originalBlocks), checked.imageHash)
	return err
}
