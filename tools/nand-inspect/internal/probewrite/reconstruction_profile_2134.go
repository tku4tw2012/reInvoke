//go:build nand2134

package probewrite

import "fmt"

const (
	ReconstructionSinglePhase         = true
	ReconstructionApplyAck            = "APPLY-2134-EARLY-ADB-01"
	ReconstructionFirstAck            = ""
	ReconstructionCompleteAck         = ""
	reconstructionBaseline            = "12.2134.0 early-ADB"
	reconstructionEnd                 = int64(0x08320000)
	reconstructionFirst               = -1
	reconstructionPartitionPrefix     = "reinvoke-2134"
	reconstruction2134CurrentMainHash = "33f16be0dcbc42b92221471ab5685d84868ebff6f6ce7dabf3b66adcd638489f"
	reconstruction2134CurrentViewHash = "c2be12e94f023b4fa991cff0359ef1b33cc4f4b92573dbb7d347a6d0b0f8b9a7"
	reconstruction2134OOBHash         = "a2df824977591738f4d3ce51bd070308a19a58a3b8827af2751b5069428a9251"
	reconstruction2134FSHash          = "d35bb0c89d2fc6d8188839173b0934b5127c5a751eea4ad68d4980fe76fe710d"
	reconstruction2134InitHash        = "8f1c8a952c9b9498b543bd1b5372214280e48220508a2883aee155641459da84"
	reconstructionLimitation          = "Coherent 12.2134.0 early-ADB code update only. Preserve block0, app, factory, fw_stat, known bad blocks and BBT. Controller-logical readback does not capture hidden physical OOB or guarantee boot/ADB availability. No automatic reboot; parent must start the observer before the owner's normal power cycle."

	// Seals identify the reviewed input; the separate apply acknowledgment is still required.
	reconstructionProfileSealed = true
	ReconstructionManifestHash  = "d142d5d1b0bb795da17dae4aac3162c6b955c3797603bdf23a47dec75fc912b8"
	ReconstructionPayloadHash   = "c8f80dc4e9ae8e86472e032c021ff049364c71d02b4d89f2854d23ab7a788adf"
	reconstructionTargetHash    = "02c36e6675656cee07c5fdaf8aedf91c8e6586c3729baf734672722b8891c6cc"
	reconstructionCount         = 763
	reconstructionPayloadSize   = int64(101570560)
	reconstructionManifestSize  = int64(498997)

	ReconstructionProfileHelp = `Actions: plan, preflight, apply, verify-target
Target: coherent 12.2134.0 early-ADB code update; fixed view [0x00020000,0x08320000).
Sealed inputs: UPDATE-PLAN.json and the 763-record target-blocks-data-oob32.bin; the plan itself is not write approval.
preflight accepts only the pinned current state; apply writes every whitelisted block once in the sealed order; verify-target accepts only the target.
Confirmation after owner approval: APPLY-2134-EARLY-ADB-01
No qualification app write, after-first state, OOB changes, or two-phase stock-ADB acknowledgments.`
)

func validateReconstructionIdentity(p *reconstructionManifest) error {
	if p.CurrentMainSHA256 != reconstruction2134CurrentMainHash || p.CurrentOOBSHA256 != reconstruction2134OOBHash ||
		p.CurrentViewSHA256 != reconstruction2134CurrentViewHash || p.SourceMainSHA256 != reconstruction2134CurrentMainHash ||
		p.TargetMainSHA256 != reconstructionTargetHash || p.TargetOOBSHA256 != reconstruction2134OOBHash {
		return fmt.Errorf("2134 requires its pinned current/target main and unchanged whole OOB")
	}
	if p.Diagnostic.FilesystemSHA256 != reconstruction2134FSHash || p.Diagnostic.InitSHA256 != reconstruction2134InitHash ||
		p.Diagnostic.USBProduct != "RI2134-ADB01" || p.Diagnostic.ChangedInitBytePositions != 7862 {
		return fmt.Errorf("2134 early-ADB diagnostic identity mismatch")
	}
	if p.FirstBlock != 0 || p.AfterFirstMainSHA256 != "" || p.AfterFirstOOBSHA256 != "" || p.AfterFirstViewSHA256 != "" {
		return fmt.Errorf("2134 has no qualification block or after-first state")
	}
	return validate2134PrebootOrder(p.WriteOrder)
}

func validate2134PrebootOrder(order []int) error {
	lastCopy := 9
	for _, block := range order {
		if block >= 1 && block <= 8 {
			if block >= lastCopy {
				return fmt.Errorf("changed preboot copies must be written in descending order")
			}
			lastCopy = block
		} else if lastCopy != 9 {
			return fmt.Errorf("changed preboot copies must be last")
		}
	}
	return nil
}
