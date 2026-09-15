//go:build nandstockroot

package probewrite

import (
	"fmt"
	"strings"
)

const (
	ReconstructionSinglePhase          = true
	ReconstructionApplyAck             = "APPLY-STOCKROOT-NATIVE-01"
	ReconstructionFirstAck             = ""
	ReconstructionCompleteAck          = ""
	reconstructionBaseline             = "coggy9 StockRoot native V11 (unmodified)"
	reconstructionEnd                  = int64(0x08320000)
	reconstructionFirst                = -1
	reconstructionPartitionPrefix      = "reinvoke-stockroot"
	reconstructionProfileSealed        = true
	ReconstructionManifestHash         = "3e1952eeda2c50167ddba35d7ef76d45f89c2e7b707c04cf7de54409bb6f6908"
	ReconstructionPayloadHash          = "3af718605f1989acfc65e72e8506d17750a53297c528b91ee794b02ff0e07507"
	reconstructionTargetHash           = "8c476a9f53448e86800ebb55affd80a9af854ff816e505e4caa1e8240b1be154"
	reconstructionCount                = 737
	reconstructionPayloadSize          = int64(98109440)
	reconstructionManifestSize         = int64(475504)
	reconstructionStockRootCurrentHash = "02c36e6675656cee07c5fdaf8aedf91c8e6586c3729baf734672722b8891c6cc"
	reconstructionStockRootViewHash    = "37ac0446e8eb2ec10ab0e4cc6648b53608257745b43f6b812f5605e914447d58"
	reconstructionStockRootOOBHash     = "a2df824977591738f4d3ce51bd070308a19a58a3b8827af2751b5069428a9251"
	reconstructionStockRootImageHash   = "f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7"
	reconstructionStockRootFSHash      = "22b38af582856aa96df7ec0deab459b730f728553109282cf271602902dd705d"
	reconstructionStockRootInitHash    = "dd9411e8e656892e1f7d64f140ddacb6068a68f6e67da858bf5398bd91d4fb4d"
	reconstructionLimitation           = "Unmodified published coggy9 StockRoot native V11 code only, with current user data and all exposed OOB retained in the prepared target. No source filesystem/init edits, donor app/ZIP import or block0 rewrite. Changed startup audio/useful BT or WiFi are first boot acceptance; native ADB is optional. Hidden physical OOB and subsequent firmware startup behavior are not guaranteed. No automatic reboot or retry."
	ReconstructionProfileHelp          = `Actions: plan, preflight, apply, verify-target
Target: unmodified published coggy9 StockRoot native V11 code; fixed view [0x00020000,0x08320000).
Sealed inputs: UPDATE-PLAN.json and 737-record target-blocks-data-oob32.bin.
apply includes its own full current-fingerprint check before any erase; it writes changed non-preboot blocks ascending, then preboot 8..1.
Matching TZ/BSL, block0, app, factory, fw_stat, unselected allocations and all OOB are preserved.
Confirmation for the owner-approved trial: APPLY-STOCKROOT-NATIVE-01
Native ADB is optional; changed startup audio/useful BT or WiFi are first boot acceptance.`
)

func validateReconstructionIdentity(p *reconstructionManifest) error {
	if p.CurrentMainSHA256 != reconstructionStockRootCurrentHash || p.SourceMainSHA256 != reconstructionStockRootCurrentHash ||
		p.CurrentViewSHA256 != reconstructionStockRootViewHash || p.TargetMainSHA256 != reconstructionTargetHash ||
		p.CurrentOOBSHA256 != reconstructionStockRootOOBHash || p.TargetOOBSHA256 != reconstructionStockRootOOBHash {
		return fmt.Errorf("StockRoot current/target fingerprint mismatch")
	}
	if p.Diagnostic.FilesystemSHA256 != reconstructionStockRootFSHash || p.Diagnostic.InitSHA256 != reconstructionStockRootInitHash ||
		p.Diagnostic.USBProduct != "MRVL USB SDK" || p.Diagnostic.ChangedInitBytePositions != 0 ||
		len(p.Diagnostic.ChangedInitLines) != 0 || len(p.Diagnostic.ChangedSourceBlocks) != 0 ||
		!strings.Contains(p.Diagnostic.Purpose, reconstructionStockRootImageHash) {
		return fmt.Errorf("StockRoot must use the unmodified published filesystem/init")
	}
	if p.FirstBlock != 0 || p.AfterFirstMainSHA256 != "" || p.AfterFirstOOBSHA256 != "" || p.AfterFirstViewSHA256 != "" {
		return fmt.Errorf("StockRoot is a single apply, not a qualification/resume")
	}
	allowed := map[string][2]int{
		"pre-bootloader": {1, 9}, "post-bootloader": {9, 25},
		"bootimgs": {249, 329}, "bootimgs_B": {129, 209}, "rootfs": {329, 1049},
	}
	for _, r := range p.Records {
		bounds, ok := allowed[r.Region]
		if !ok || r.Block < bounds[0] || r.Block >= bounds[1] {
			return fmt.Errorf("StockRoot record outside changed native code allocations: %d %s", r.Block, r.Region)
		}
	}
	if len(p.WriteOrder) < 8 {
		return fmt.Errorf("StockRoot requires eight final preboot copies")
	}
	last := 8
	for _, block := range p.WriteOrder[:len(p.WriteOrder)-8] {
		if block <= last {
			return fmt.Errorf("StockRoot non-preboot write order must be strictly ascending")
		}
		last = block
	}
	for i, block := range p.WriteOrder[len(p.WriteOrder)-8:] {
		if block != 8-i {
			return fmt.Errorf("StockRoot preboot copies must be last in order 8..1")
		}
	}
	return nil
}
