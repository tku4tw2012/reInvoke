package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/probewrite"
)

func usage(out io.Writer) {
	fmt.Fprintf(out, `Usage: nand-baseline-reconstruct ACTION --manifest FILE --capsule FILE [--evidence NEW-TMPFS-DIR] [--confirm TEXT]
%s
No offsets, modes, pin overrides, arbitrary resume, retry, rollback, reboot or bad-block marking.
Run in the foreground with a live stdout transport. SIGHUP/INT/TERM stop before the next erase; after erase the current block is completed and verified unless hard I/O fails.
Clean logical readback cannot prove the five uncertain captured fwstat pages or guarantee normal boot/ADB availability.
`, probewrite.ReconstructionProfileHelp)
}

func run(args []string, stdout, stderr io.Writer) (exit int) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		usage(stdout)
		return 0
	}
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	action := args[0]
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("manifest", "", "exact pinned RESTORE-PLAN.json")
	capsule := flags.String("capsule", "", "exact pinned target-blocks-data-oob32.bin")
	evidence := flags.String("evidence", "", "new private tmpfs evidence directory")
	confirm := flags.String("confirm", "", "fixed phase confirmation")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return 2
	}
	if err := probewrite.CheckReconstructionAction(action, *confirm); err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 2
	}
	if *manifest == "" || *capsule == "" || (action == "plan" && *evidence != "") ||
		(action != "plan" && *evidence == "") {
		usage(stderr)
		return 2
	}
	if action != "plan" {
		if err := probewrite.CheckReconstructionRuntime(); err != nil {
			fmt.Fprintln(stderr, "ERROR:", err)
			return 1
		}
	}
	bundle, err := probewrite.LoadReconstruction(*manifest, *capsule)
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		return 1
	}
	defer func() {
		if err := bundle.Close(); err != nil {
			fmt.Fprintln(stderr, "ERROR: close sources:", err)
			exit = 1
		}
	}()
	if action == "plan" {
		err = bundle.PrintPlan(stdout)
	} else {
		err = hardware(action, *confirm, *evidence, bundle, stdout)
	}
	if err != nil {
		fmt.Fprintln(stderr, "ERROR:", err)
		fmt.Fprintln(stderr, "STOP. No automatic retry/rollback/reboot. An erased block may be partial; successful ioctl status is not proof.")
		return 1
	}
	return 0
}

func hardware(action, approval, evidence string, bundle *probewrite.ReconstructionBundle, output io.Writer) (result error) {
	if err := probewrite.CheckReconstructionAction(action, approval); err != nil {
		return err
	}
	if err := bundle.RequireRAMInputs(); err != nil {
		return err
	}
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
	file, err := os.OpenFile(filepath.Join(evidence, "events.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			result = fmt.Errorf("result=%v; close journal: %w", result, err)
		}
	}()
	log := &probewrite.ReconstructionJournal{File: file, Output: output}
	if err := log.Record(probewrite.Event{Stage: "sources-verified", Block: -1,
		Detail: "manifest=" + probewrite.ReconstructionManifestHash + " capsule=" + probewrite.ReconstructionPayloadHash}); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if err := ctx.Err(); err != nil {
		return err
	}
	device, err := probewrite.OpenReconstructionMTD(evidence)
	if err != nil {
		if logErr := log.Record(probewrite.Event{Stage: "open-failed-cleanup-attempted", Block: -1, Detail: err.Error()}); logErr != nil {
			return fmt.Errorf("open: %v; journal: %w", err, logErr)
		}
		return err
	}
	defer func() { result = probewrite.CloseReconstruction(device, log, result) }()
	return probewrite.ExecuteReconstruction(ctx, device, bundle, action, approval, log)
}

func main() {
	signal.Ignore(syscall.SIGPIPE)
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
