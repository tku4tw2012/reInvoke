package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/probewrite"
)

const mappingAck = probewrite.MappingAck

type journal struct {
	file interface {
		io.Writer
		Sync() error
	}
	output io.Writer
}

func (j *journal) Record(event probewrite.Event) error {
	record := struct {
		Time string `json:"time"`
		probewrite.Event
	}{time.Now().UTC().Format(time.RFC3339Nano), event}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode journal event: %w", err)
	}
	data = append(data, '\n')
	if n, err := j.file.Write(data); err != nil {
		return fmt.Errorf("journal write failed: %w", err)
	} else if n != len(data) {
		return fmt.Errorf("journal write failed: %w", io.ErrShortWrite)
	}
	if err := j.file.Sync(); err != nil {
		return fmt.Errorf("journal sync failed: %w", err)
	}
	if n, err := j.output.Write(data); err != nil {
		return fmt.Errorf("journal output failed: %w", err)
	} else if n != len(data) {
		return fmt.Errorf("journal output failed: %w", io.ErrShortWrite)
	}
	return nil
}

func closeWithJournal(device io.Closer, log probewrite.Journal, result error) error {
	record := func(stage string) {
		if err := log.Record(probewrite.Event{Stage: stage, Block: -1, Detail: fmt.Sprint(result)}); err != nil {
			result = fmt.Errorf("operation result=%v; record %s: %w", result, stage, err)
		}
	}
	if result != nil {
		record("operation-failed")
	}
	record("before-close")
	// Reporting failure must never prevent the actual device cleanup.
	if err := device.Close(); err != nil {
		result = fmt.Errorf("operation result=%v; close: %w", result, err)
		record("close-failed")
	} else {
		record("closed-no-reboot")
	}
	return result
}

func usage(output io.Writer) {
	fmt.Fprintf(output, `Usage: nand-probe-write ACTION --image FILE --original FILE [OPTIONS]

Actions:
  plan        Offline, regular-file inspection only; no device access.
  preflight   Future attended ARM/RAM run: read target and save visible OOB.
  check-map   Read-only mapping qualification on the exact cleanup-fixed ARM kernel.
  install     %s
  restore     %s

Options:
  --image FILE       Pinned %s (%d bytes)
  --original FILE    Pinned %s (%d bytes)
  --evidence DIR     New directory on /tmp (hardware actions only)
  --confirm TEXT     Action-specific literal approval (check-map/install/restore)

No action reboots, retries failed writes, marks bad blocks or expands the target.
Fixed target: 0x%08x..0x%08x (end exclusive), %s.
The full %s is a validation artifact, not a writer input.
Hardware operations require the supported ARM RAM kernel and explicit unit approval.
`, probewrite.InstallHelp, probewrite.RestoreHelp, probewrite.ImageFilename, probewrite.ImageBytes,
		probewrite.OriginalFilename, probewrite.Span, probewrite.Start, probewrite.End,
		probewrite.TargetHelp, probewrite.FilesystemName)
	if probewrite.ResumeSupported {
		fmt.Fprintln(output, `
Fixed partial-pilot actions (no offsets, indexes or ECC overrides):
  resume-plan       Offline source validation and exact resume write order.
  resume-preflight  Read-only mixed-state/ECC checks; no partition mapping.
  resume            Preserve candidate blocks 1..136; write 137..307 then 0.
Only block 136 page 62 (absolute page 29822) permits <=1 correction per main
read and <=1 per visible-OOB read. Every other candidate read requires zero.
resume-preflight and resume each require their own exact --confirm literal.`)
	}
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		usage(stdout)
		return 0
	}
	if len(args) < 1 {
		usage(stderr)
		return 2
	}
	action := args[0]
	if action == "restore" && !probewrite.RestoreSupported {
		fmt.Fprintln(stderr, "ERROR: restore is unsupported by this forward-only profile; no device accessed")
		return 2
	}
	resume := action == "resume" || action == "resume-preflight" || action == "resume-plan"
	if resume && !probewrite.ResumeSupported {
		fmt.Fprintln(stderr, "ERROR: resume requires the explicit nandpilot build profile; no device accessed")
		return 2
	}
	if action != "plan" && action != "preflight" && action != "check-map" && action != "install" && action != "restore" && !resume {
		usage(stderr)
		return 2
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	image := flags.String("image", "", "pinned candidate payload")
	original := flags.String("original", "", "pinned preflight original-state payload")
	evidence := flags.String("evidence", "", "new tmpfs evidence directory")
	approval := flags.String("confirm", "", "action-specific confirmation")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *image == "" || *original == "" {
		usage(stderr)
		return 2
	}
	wantAck := ""
	switch action {
	case "check-map":
		wantAck = mappingAck
	case "install":
		wantAck = probewrite.InstallAck
	case "restore":
		wantAck = probewrite.RestoreAck
	case "resume":
		wantAck = probewrite.ResumeAck
	case "resume-preflight":
		wantAck = probewrite.ResumePreflightAck
	}
	if *approval != wantAck {
		fmt.Fprintf(stderr, "ERROR: %s requires exact confirmation %q; no device accessed\n", action, wantAck)
		return 2
	}
	offline := action == "plan" || action == "resume-plan"
	if !offline {
		check := probewrite.CheckRuntime
		if resume {
			check = probewrite.CheckResumeRuntime
		}
		if err := check(); err != nil {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
		for _, path := range []string{*image, *original} {
			if err := probewrite.RequireRAMPath(path); err != nil {
				fmt.Fprintln(stderr, "ERROR:", err)
				return 1
			}
		}
		if *evidence == "" {
			fmt.Fprintln(stderr, "ERROR: --evidence is required")
			return 2
		}
	} else if *evidence != "" {
		fmt.Fprintln(stderr, "ERROR: plan writes only to stdout; --evidence is not accepted")
		return 2
	}
	bundle, err := probewrite.Load(*image, *original)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	defer bundle.Close()
	var plan interface{} = bundle.Plan
	if resume {
		plan, err = probewrite.PlanResume(bundle)
		if err != nil {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
	}
	if offline {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(plan); err != nil {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
		return 0
	}
	if err := hardware(action, *approval, *evidence, bundle, stdout); err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		fmt.Fprintln(stderr, "STOP. No automatic rollback or reboot. If an erase began, target contents may be partial.")
		return 1
	}
	return 0
}

func hardware(action, approval, evidence string, bundle *probewrite.Bundle, output io.Writer) (result error) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(evidence))
	if err != nil {
		return err
	}
	if err := probewrite.RequireRAMPath(parent); err != nil {
		return err
	}
	evidence = filepath.Join(parent, filepath.Base(evidence))
	if err := os.Mkdir(evidence, 0700); err != nil {
		return err
	}
	record, err := os.OpenFile(filepath.Join(evidence, "events.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err := record.Close(); err != nil {
			result = fmt.Errorf("operation result=%v; close journal: %w", result, err)
		}
	}()
	log := &journal{file: record, output: output}
	oob, err := os.OpenFile(filepath.Join(evidence, "visible-oob-before.bin"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer oob.Close()
	planFile, err := os.OpenFile(filepath.Join(evidence, "plan.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	var plan interface{} = bundle.Plan
	resume := action == "resume" || action == "resume-preflight"
	if resume {
		plan, err = probewrite.PlanResume(bundle)
	}
	if err == nil {
		err = json.NewEncoder(planFile).Encode(plan)
	}
	if err == nil {
		err = planFile.Sync()
	}
	closeErr := planFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	var device *probewrite.MTD
	if resume {
		device, err = probewrite.OpenResumeMTD(evidence)
	} else {
		device, err = probewrite.OpenMTD(evidence, action == "restore")
	}
	if err != nil {
		return err
	}
	defer func() {
		result = closeWithJournal(device, log, result)
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	persist := func() error {
		if err := oob.Sync(); err != nil {
			return err
		}
		return record.Sync()
	}
	return dispatchHardware(action, func() error {
		var snapshot *probewrite.Snapshot
		if action == "resume-preflight" {
			snapshot, err = probewrite.PrepareResume(ctx, device, bundle, approval, oob, log)
		} else {
			snapshot, err = probewrite.Prepare(ctx, device, bundle, oob, log)
		}
		if err != nil {
			return err
		}
		if err := persist(); err != nil {
			return err
		}
		if action == "check-map" {
			if err := device.CheckMapping(ctx, bundle, snapshot, log); err != nil {
				return err
			}
		}
		return log.Record(probewrite.Event{Stage: "read-checks-complete", Block: -1,
			Detail: fmt.Sprintf("cleanup pending; preflight ECC failures=%d corrected=%d visible-OOB SHA256=%s",
				snapshot.Failed, snapshot.Corrected, snapshot.OOBHash)})
	}, func(restore bool) error {
		return probewrite.Execute(ctx, device, bundle, restore, approval, oob, log, persist)
	}, func() error {
		return probewrite.ExecuteResume(ctx, device, bundle, approval, oob, log, persist)
	})
}

func dispatchHardware(action string, readOnly func() error, execute func(bool) error, resume func() error) error {
	if action == "restore" && !probewrite.RestoreSupported {
		return fmt.Errorf("restore is unsupported by this forward-only profile")
	}
	if (action == "resume" || action == "resume-preflight") && !probewrite.ResumeSupported {
		return fmt.Errorf("resume requires the explicit nandpilot build profile")
	}
	switch action {
	case "resume":
		return resume()
	case "preflight", "check-map", "resume-preflight":
		return readOnly()
	case "install", "restore":
		return execute(action == "restore")
	default:
		return fmt.Errorf("unsupported hardware action %q", action)
	}
}

func main() {
	// Broken host output must return an error and unwind through device cleanup.
	signal.Ignore(syscall.SIGPIPE)
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
