package squashmin

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictBundleJSON(t *testing.T) {
	for _, input := range []string{
		`{"x":1,"x":2}`, `{"patch":{"x":1,"x":1}}`,
		`{"x":1} {}`, `{"x":1} !`, `{"x":`, `[1,]`,
		strings.Repeat("[", 18) + "0" + strings.Repeat("]", 18),
	} {
		if _, err := strictJSON([]byte(input)); err == nil {
			t.Fatalf("accepted ambiguous/invalid JSON %q", input)
		}
	}
	want, err := strictJSON([]byte(`{"flag":false,"count":0,"blocks":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"count":0,"blocks":[1,2]}`, `{"flag":false,"blocks":[1,2]}`,
		`{"flag":false,"count":0,"blocks":[2,1]}`,
		`{"flag":false,"count":0,"blocks":[1,2],"extra":true}`,
	} {
		got, err := strictJSON([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if firstMismatch("proposal", want, got) == "" {
			t.Fatalf("accepted missing/altered metadata %q", input)
		}
	}
}

func TestInstalledBundleBinding(t *testing.T) {
	source, capture, dir := os.Getenv("SQUASHMIN_SOURCE"), os.Getenv("SQUASHMIN_CAPTURE"), os.Getenv("SQUASHMIN_BUNDLE")
	if source == "" || capture == "" || dir == "" {
		t.Skip("set SQUASHMIN_SOURCE, SQUASHMIN_CAPTURE and SQUASHMIN_BUNDLE for private bundle tests")
	}
	checked, err := prepareBundle(source, capture, dir, "/usr/bin/gzip")
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	proposal := read("PROPOSAL.json")
	original, candidate := read("changed-blocks-original.bin"), read("changed-blocks-candidate.bin")
	t.Run("correct_bundle", func(t *testing.T) {
		if err := checked.verify(proposal, original, candidate); err != nil {
			t.Fatal(err)
		}
		if checked.blockCount != 2 || checked.changedBytes != 48015 || len(checked.originalBlocks) != 262144 {
			t.Fatal("incorrect recomputed footprint")
		}
	})
	for _, tc := range []struct {
		name                string
		original, candidate []byte
	}{
		{"swapped_roles", candidate, original},
		{"stale_candidate", original, original},
		{"stale_original", candidate, candidate},
		{"truncated_original", original[:len(original)-1], candidate},
		{"extra_candidate_byte", original, append(append([]byte(nil), candidate...), 0)},
		{"reordered_candidate_blocks", original, append(append([]byte(nil), candidate[EraseBytes:]...), candidate[:EraseBytes]...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checked.verify(proposal, tc.original, tc.candidate); err == nil {
				t.Fatal("accepted incorrect full-block payload")
			}
		})
	}
	t.Run("unchanged_block_padding_corruption", func(t *testing.T) {
		bad := append([]byte(nil), candidate...)
		bad[0] ^= 1 // Outside the modified compressed fragment.
		if err := checked.verify(proposal, original, bad); err == nil {
			t.Fatal("accepted corrupted unchanged padding")
		}
	})

	// Alter every proposed leaf independently, including patch compression/
	// location fields and both blocks' first/last offsets and hashes.
	var leaves [][]interface{}
	var collect func(interface{}, []interface{})
	collect = func(node interface{}, path []interface{}) {
		switch value := node.(type) {
		case map[string]interface{}:
			for key, child := range value {
				collect(child, append(append([]interface{}(nil), path...), key))
			}
		case []interface{}:
			for i, child := range value {
				collect(child, append(append([]interface{}(nil), path...), i))
			}
		default:
			leaves = append(leaves, path)
		}
	}
	collect(checked.proposal, nil)
	for _, path := range leaves {
		name := "altered"
		for _, element := range path {
			if key, ok := element.(string); ok {
				name += "_" + key
			}
		}
		t.Run(name, func(t *testing.T) {
			tree, err := strictJSON(proposal)
			if err != nil {
				t.Fatal(err)
			}
			node := tree
			for _, element := range path[:len(path)-1] {
				switch key := element.(type) {
				case string:
					node = node.(map[string]interface{})[key]
				case int:
					node = node.([]interface{})[key]
				}
			}
			parent, key := node.(map[string]interface{}), path[len(path)-1].(string)
			switch parent[key].(type) {
			case bool:
				parent[key] = true
			case json.Number:
				parent[key] = json.Number("999999")
			default:
				parent[key] = "incorrect"
			}
			bad, err := json.Marshal(tree)
			if err != nil {
				t.Fatal(err)
			}
			if err := checked.verify(bad, original, candidate); err == nil {
				t.Fatalf("accepted altered field %v", path)
			}
		})
	}
	t.Logf("rejected alterations to all %d proposal leaf fields", len(leaves))
	for _, mutation := range []struct {
		name string
		edit func(map[string]interface{})
	}{
		{"omitted_false_safety_flag", func(p map[string]interface{}) { delete(p, "write_approved") }},
		{"omitted_zero_offset", func(p map[string]interface{}) {
			delete(p["blocks"].([]interface{})[1].(map[string]interface{}), "first_difference_in_block")
		}},
		{"extra_block", func(p map[string]interface{}) {
			blocks := p["blocks"].([]interface{})
			p["blocks"] = append(blocks, blocks[0])
		}},
		{"omitted_block", func(p map[string]interface{}) { p["blocks"] = p["blocks"].([]interface{})[:1] }},
		{"reordered_block_metadata", func(p map[string]interface{}) {
			blocks := p["blocks"].([]interface{})
			blocks[0], blocks[1] = blocks[1], blocks[0]
		}},
		{"unknown_field", func(p map[string]interface{}) { p["unexpected"] = false }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			tree, err := strictJSON(proposal)
			if err != nil {
				t.Fatal(err)
			}
			mutation.edit(tree.(map[string]interface{}))
			bad, err := json.Marshal(tree)
			if err != nil {
				t.Fatal(err)
			}
			if err := checked.verify(bad, original, candidate); err == nil {
				t.Fatal("accepted incomplete/conflicting proposal")
			}
		})
	}
	t.Run("duplicate_proposal_hash", func(t *testing.T) {
		bad := bytes.Replace(proposal, []byte(`"capture_sha256":`), []byte(`"capture_sha256":"incorrect","capture_sha256":`), 1)
		if err := checked.verify(bad, original, candidate); err == nil {
			t.Fatal("accepted duplicate hash field")
		}
	})
}
