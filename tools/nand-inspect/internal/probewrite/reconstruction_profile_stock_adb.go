//go:build !nand2134 && !nandstockroot

package probewrite

import "fmt"

const (
	ReconstructionSinglePhase     = false
	ReconstructionManifestHash    = "7f8b2939b8aaf2064648d5a254df502c061be8d4c25fda509f9d6cd837dbbb53"
	ReconstructionPayloadHash     = "170e7aa3a734b8ff9ac20b4269e52e1970c074c31b40c338fcad2fc82a0076ed"
	ReconstructionFirstAck        = "RECONSTRUCT-STOCK-ADB-FIRST-1573"
	ReconstructionCompleteAck     = "RECONSTRUCT-STOCK-ADB-REMAINING-865"
	ReconstructionApplyAck        = ""
	reconstructionProfileSealed   = true
	reconstructionBaseline        = "September7 source with minimal stock-adb diagnostic"
	reconstructionSourceHash      = "2fac4159fe23aa25581c29f6c90033af3a1126a02593db0bd47e2c10d2c09f19"
	reconstructionTargetHash      = "33f16be0dcbc42b92221471ab5685d84868ebff6f6ce7dabf3b66adcd638489f"
	reconstructionFSHash          = "597b869df383f13b7e129d031c2e884b0b707d23b8279e4affdb40b9eaf3499a"
	reconstructionInitHash        = "db83188488d5a8bc73f26dbd9333dd6479c51ec50d1c80cfd97822da8ad8ee62"
	reconstructionEnd             = int64(0x0ff20000)
	reconstructionFirst           = 1573
	reconstructionCount           = 866
	reconstructionPayloadSize     = int64(115281920)
	reconstructionManifestSize    = int64(558479)
	reconstructionPartitionPrefix = "reinvoke-stock-adb"
	reconstructionLimitation      = "Controller-logical September7 original firmware with minimal stock ADB enablement, not the superseded unmodified baseline or full reInvoke runtime. Hidden physical OOB was not captured. Captured fwstat pages 0x0fe40800,0x0fe43000,0x0fe45000,0x0fe45800,0x0fe46000 remain unproven even after clean readback. No boot or ADB availability guarantee."
	ReconstructionProfileHelp     = `Actions: plan, preflight, qualify-first, complete, verify-target
Target: September7 original firmware with minimal stock ADB enablement, not an unmodified baseline or full reInvoke runtime.
plan only reads pinned regular files and writes stdout.
preflight accepts only the pinned current or after-first state; verify-target only the final target.
qualify-first writes only physical block 1573. complete requires the exact mixed state and writes the remaining 865 blocks in the pinned order.
Confirmations: RECONSTRUCT-STOCK-ADB-FIRST-1573 / RECONSTRUCT-STOCK-ADB-REMAINING-865
Superseded unmodified-baseline plans, capsules and acknowledgments are not accepted.`
)

func validateReconstructionIdentity(p *reconstructionManifest) error {
	if p.FirstBlock != reconstructionFirst || len(p.WriteOrder) == 0 || p.WriteOrder[0] != reconstructionFirst {
		return fmt.Errorf("stock-ADB requires its original qualification block and write order")
	}
	if p.SourceMainSHA256 != reconstructionSourceHash || p.TargetMainSHA256 != reconstructionTargetHash ||
		p.Diagnostic.FilesystemSHA256 != reconstructionFSHash || p.Diagnostic.InitSHA256 != reconstructionInitHash ||
		p.Diagnostic.USBProduct != "reInvoke-ADB" || p.Diagnostic.ChangedInitBytePositions != 29 ||
		len(p.Diagnostic.ChangedSourceBlocks) != 2 ||
		p.Diagnostic.ChangedSourceBlocks[0] != 357 || p.Diagnostic.ChangedSourceBlocks[1] != 358 ||
		len(p.Diagnostic.ChangedInitLines) != 3 || p.Diagnostic.ChangedInitLines[0] != 85 ||
		p.Diagnostic.ChangedInitLines[1] != 135 || p.Diagnostic.ChangedInitLines[2] != 235 {
		return fmt.Errorf("stock-ADB source/target identity or minimal diagnostic patch metadata mismatch")
	}
	return nil
}
