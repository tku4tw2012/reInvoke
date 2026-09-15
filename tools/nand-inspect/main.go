package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const usage = `Usage:
  nand-inspect container IMAGE
  nand-inspect capture DATA_AREA_CAPTURE
  nand-inspect compare IMAGE DATA_AREA_CAPTURE

Offline inspection only. Inputs must be regular files, not device nodes,
symlinks or pipes. JSON contains metadata, not payloads or flash commands.
`

type Source struct {
	Path   string `json:"path"`
	Bytes  uint64 `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Report struct {
	InspectionOnly bool        `json:"inspection_only"`
	Sources        []Source    `json:"sources"`
	Image          *Image      `json:"container,omitempty"`
	Capture        *Capture    `json:"capture,omitempty"`
	Comparison     *Comparison `json:"comparison,omitempty"`
}

func inspectFile(path string, container bool) (Source, *Image, *Capture, error) {
	file, err := openRegularInput(path)
	if err != nil {
		return Source{}, nil, nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return Source{}, nil, nil, err
	}
	size := uint64(stat.Size())
	var image *Image
	var capture *Capture
	if container {
		image, err = inspectImage(file, size)
	} else {
		capture, err = inspectCapture(file, size)
	}
	if err != nil {
		return Source{}, nil, nil, err
	}
	hash := sha256.New()
	copied, err := io.Copy(hash, file)
	if err != nil {
		return Source{}, nil, nil, fmt.Errorf("hash input: %w", err)
	}
	if uint64(copied) != size {
		return Source{}, nil, nil, fmt.Errorf("input size changed while hashing")
	}
	after, err := file.Stat()
	if err != nil {
		return Source{}, nil, nil, err
	}
	if after.Size() != stat.Size() || after.ModTime() != stat.ModTime() {
		return Source{}, nil, nil, fmt.Errorf("input changed during inspection")
	}
	return Source{path, size, hex.EncodeToString(hash.Sum(nil))}, image, capture, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if len(args) < 2 || (args[0] != "container" && args[0] != "capture" && args[0] != "compare") ||
		(args[0] == "compare" && len(args) != 3) || (args[0] != "compare" && len(args) != 2) {
		fmt.Fprint(stderr, usage)
		return 2
	}
	report := Report{InspectionOnly: true}
	source, image, capture, err := inspectFile(args[1], args[0] != "capture")
	if err == nil {
		report.Sources = append(report.Sources, source)
		report.Image, report.Capture = image, capture
	}
	if err == nil && args[0] == "compare" {
		source, _, report.Capture, err = inspectFile(args[2], false)
		if err == nil {
			report.Sources = append(report.Sources, source)
			report.Comparison, err = compareAllocations(report.Image, report.Capture)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "ERROR: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "ERROR: encode report: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
