//go:build !nandpilot

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/probewrite"
)

func TestCLIRejectsUnsafeOrIncompleteActions(t *testing.T) {
	cases := []struct {
		args []string
		code int
		want string
	}{
		{nil, 2, "Usage"},
		{[]string{"erase"}, 2, "Usage"},
		{[]string{"plan"}, 2, "Usage"},
		{[]string{"install", "--image", "/dev/null", "--original", "/dev/null"}, 2, "exact confirmation"},
		{[]string{"restore", "--image", "/dev/null", "--original", "/dev/null", "--confirm", "yes"}, 2, "exact confirmation"},
		{[]string{"plan", "--image", "/dev/null", "--original", "/dev/null"}, 1, "regular file"},
		{[]string{"plan", "--image", "/dev/null", "--original", "/dev/null", "--evidence", "/tmp/no"}, 2, "stdout"},
		{[]string{"preflight", "--image", "/dev/null", "--original", "/dev/null"}, 1, "require root on the ARM"},
		{[]string{"check-map", "--image", "/dev/null", "--original", "/dev/null"}, 2, "exact confirmation"},
		{[]string{"check-map", "--image", "/dev/null", "--original", "/dev/null", "--confirm", mappingAck}, 1, "require root on the ARM"},
		{[]string{"check-map", "--image", "/dev/null", "--original", "/dev/null", "--confirm", "MAP-ONLY-rootfs-02920000-057c0000"}, 2, "exact confirmation"},
		{[]string{"install", "--image", "/dev/null", "--original", "/dev/null", "--confirm", "INSTALL-rootfs-probe-02920000-057c0000"}, 2, "exact confirmation"},
		{[]string{"restore", "--image", "/dev/null", "--original", "/dev/null", "--confirm", "RESTORE-original-rootfs-02920000-057c0000"}, 2, "exact confirmation"},
		{[]string{"install", "--image", "/dev/null", "--original", "/dev/null", "--confirm", probewrite.RestoreAck}, 2, "exact confirmation"},
		{[]string{"restore", "--image", "/dev/null", "--original", "/dev/null", "--confirm", probewrite.InstallAck}, 2, "exact confirmation"},
		{[]string{"plan", "--image", "/dev/null", "--original", "/dev/null", "--offset", "0x02920000"}, 2, "flag provided but not defined"},
	}
	for _, test := range cases {
		if len(test.args) != 0 && test.args[0] == "restore" && !probewrite.RestoreSupported {
			test.want = "restore is unsupported"
		}
		var stdout, stderr bytes.Buffer
		if got := run(test.args, &stdout, &stderr); got != test.code ||
			stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", test.args, got, &stdout, &stderr)
		}
	}
}

func TestHelpIsNonoperational(t *testing.T) {
	var out, errs bytes.Buffer
	if run([]string{"--help"}, &out, &errs) != 0 || !strings.Contains(out.String(), "No action reboots") ||
		!strings.Contains(out.String(), fmt.Sprintf("%s (%d bytes)", probewrite.ImageFilename, probewrite.ImageBytes)) ||
		!strings.Contains(out.String(), fmt.Sprintf("0x%08x..0x%08x", probewrite.Start, probewrite.End)) ||
		!strings.Contains(out.String(), "Read-only mapping qualification on the exact cleanup-fixed ARM kernel") ||
		!strings.Contains(out.String(), probewrite.InstallHelp) {
		t.Fatal("help failed")
	}
}

func TestCLIPlanRetainedTwoBlockArtifacts(t *testing.T) {
	if probewrite.HeaderLast {
		t.Skip("retained two-block artifacts belong only to the default profile")
	}
	dir := os.Getenv("NAND_TWO_BLOCK_CANDIDATE_DIR")
	if dir == "" {
		t.Skip("set NAND_TWO_BLOCK_CANDIDATE_DIR for retained-artifact CLI verification")
	}
	var out, errs bytes.Buffer
	if code := run([]string{"plan", "--image", filepath.Join(dir, "changed-blocks-candidate.bin"),
		"--original", filepath.Join(dir, "changed-blocks-original.bin")}, &out, &errs); code != 0 {
		t.Fatalf("plan failed: code=%d stderr=%s", code, &errs)
	}
	var plan probewrite.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Start != 0x02ca0000 || plan.End != 0x02ce0000 || plan.Span != 262144 ||
		plan.ChangedBlocks != 2 || len(plan.Blocks) != 2 || plan.WriteAuthorized ||
		plan.ImageSHA256 != probewrite.ImageHash || plan.OriginalSHA256 != probewrite.OriginalHash ||
		!reflect.DeepEqual(plan.WriteOrder, []int{0, 1}) || errs.Len() != 0 {
		t.Fatalf("unexpected retained CLI plan: %s stderr=%s", &out, &errs)
	}
}

type journalFile struct {
	bytes.Buffer
	synced     int
	writeError error
	syncError  error
	shortWrite bool
}

func (f *journalFile) Write(data []byte) (int, error) {
	if f.writeError != nil {
		return 0, f.writeError
	}
	if f.shortWrite {
		return f.Buffer.Write(data[:len(data)-1])
	}
	return f.Buffer.Write(data)
}

func (f *journalFile) Sync() error {
	if f.syncError != nil {
		return f.syncError
	}
	f.synced = f.Len()
	return nil
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(data []byte) (int, error) { return f(data) }

type closeFunc func() error

func (f closeFunc) Close() error { return f() }

func journalStages(t *testing.T, data []byte) []string {
	t.Helper()
	var stages []string
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var record struct {
			Time string `json:"time"`
			probewrite.Event
		}
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record.Time == "" {
			t.Fatal("missing journal timestamp")
		}
		stages = append(stages, record.Stage)
	}
	return stages
}

func TestJournalMirrorsOnlyDurableRecords(t *testing.T) {
	for _, fault := range []string{"none", "write", "short write", "sync"} {
		t.Run(fault, func(t *testing.T) {
			file := &journalFile{}
			switch fault {
			case "write":
				file.writeError = errors.New("injected write failure")
			case "short write":
				file.shortWrite = true
			case "sync":
				file.syncError = errors.New("injected sync failure")
			}
			var output bytes.Buffer
			log := &journal{file: file, output: writerFunc(func(data []byte) (int, error) {
				if file.synced != file.Len() || !bytes.Equal(file.Bytes(), data) {
					t.Fatal("output preceded durable journal event")
				}
				return output.Write(data)
			})}
			err := log.Record(probewrite.Event{Stage: "preflight-complete", Block: -1})
			if fault == "none" {
				if err != nil || !bytes.Equal(output.Bytes(), file.Bytes()) {
					t.Fatalf("durable event was not mirrored exactly: %v", err)
				}
			} else if err == nil || output.Len() != 0 {
				t.Fatalf("non-durable event reported on output: err=%v output=%s", err, &output)
			}
		})
	}
}

func TestClosePhasesAreMirroredInOrder(t *testing.T) {
	for _, phases := range [][]string{
		{"read-checks-complete"},
		{"mapping-requested", "mapping-readback-verified", "read-checks-complete"},
	} {
		t.Run(phases[0], func(t *testing.T) {
			file := &journalFile{}
			var output bytes.Buffer
			log := &journal{file: file, output: &output}
			for _, phase := range phases {
				if err := log.Record(probewrite.Event{Stage: phase, Block: -1, Detail: "cleanup pending"}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			beforeClose := append(append([]string(nil), phases...), "before-close")
			device := closeFunc(func() error {
				calls++
				if !reflect.DeepEqual(journalStages(t, output.Bytes()), beforeClose) ||
					!bytes.Equal(file.Bytes(), output.Bytes()) || file.synced != file.Len() {
					t.Fatal("close started without durable, mirrored before-close phase")
				}
				return nil
			})
			if err := closeWithJournal(device, log, nil); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !bytes.Equal(output.Bytes(), file.Bytes()) ||
				!reflect.DeepEqual(journalStages(t, output.Bytes()), append(beforeClose, "closed-no-reboot")) {
				t.Fatalf("incorrect lifecycle order or close count: calls=%d output=%s", calls, &output)
			}
		})
	}
}

func TestLoggingFailureNeverSkipsCloseOrReturnsSuccess(t *testing.T) {
	for _, priorFailure := range []bool{false, true} {
		for _, fault := range []string{"output", "short output", "journal write", "journal sync", "output after close"} {
			t.Run(fault+"/"+map[bool]string{false: "normal", true: "operation-failed"}[priorFailure], func(t *testing.T) {
				file := &journalFile{}
				outputError := errors.New("injected output failure")
				switch fault {
				case "journal write":
					file.writeError = errors.New("injected journal failure")
				case "journal sync":
					file.syncError = errors.New("injected sync failure")
				}
				calls := 0
				log := &journal{file: file, output: writerFunc(func(data []byte) (int, error) {
					if fault == "output" || (fault == "output after close" && calls > 0) {
						return 0, outputError
					}
					if fault == "short output" {
						return len(data) - 1, nil
					}
					return len(data), nil
				})}
				var result error
				if priorFailure {
					result = errors.New("original operation failure")
				}
				err := closeWithJournal(closeFunc(func() error { calls++; return nil }), log, result)
				if err == nil || calls != 1 {
					t.Fatalf("logging failure suppressed or cleanup skipped: calls=%d err=%v", calls, err)
				}
				if priorFailure && !strings.Contains(err.Error(), "original operation failure") {
					t.Fatalf("original failure lost: %v", err)
				}
				if (fault == "output" || fault == "output after close") && !errors.Is(err, outputError) {
					t.Fatalf("output error not propagated: %v", err)
				}
				if fault == "short output" && !errors.Is(err, io.ErrShortWrite) {
					t.Fatalf("short output not rejected: %v", err)
				}
			})
		}
	}
}

func TestCloseFailureCannotReportClosed(t *testing.T) {
	file := &journalFile{}
	var output bytes.Buffer
	log := &journal{file: file, output: &output}
	closeError := errors.New("injected cleanup failure")
	err := closeWithJournal(closeFunc(func() error { return closeError }), log, nil)
	if !errors.Is(err, closeError) || !bytes.Equal(output.Bytes(), file.Bytes()) ||
		!reflect.DeepEqual(journalStages(t, output.Bytes()), []string{"before-close", "close-failed"}) {
		t.Fatalf("cleanup failure reported as closed: err=%v output=%s", err, &output)
	}
}
