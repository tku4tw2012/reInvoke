//go:build !nandpilot && !nandbsl && !nandbslcompact

package probewrite

const (
	Start            int64 = 0x02ca0000
	Span             int64 = 0x00040000
	ImageHash              = "a64b76e8de7bc2c1471dd40668d627d3ddc9726abc0d09be264518e9c7e497e9"
	OriginalHash           = "005be33d9ebaf5c698ebf26c93e8509c24d6e1e2e775bfb9f8a960b237a093a0"
	BeforeHash             = "7c4a5981443fcb1b6ced74ac8687274edbd3ad770eac0226a1dc1ea1189ed173"
	AfterHash              = "0bcffebd13a1d0b5ea610e1fcbb1e264a9fe921a33b8ff5f038c0740974217be"
	TableHash              = "53fab213e1be985fae65abaaf1ad9de6fbb9f5f7c66cb2316bd65f2ef88d992f"
	InstallAck             = "INSTALL-minimal-probe-02ca0000-02ce0000"
	RestoreAck             = "RESTORE-original-two-blocks-02ca0000-02ce0000"
	MappingAck             = "MAP-ONLY-two-blocks-02ca0000-02ce0000"
	HeaderLast             = false
	ResumeSupported        = false
	RestoreSupported       = true

	PlanProfileName  = ""
	FilesystemHash   = ""
	ImageFilename    = "changed-blocks-candidate.bin"
	OriginalFilename = "changed-blocks-original.bin"
	FilesystemName   = "minimal-probe.squashfs"
	InstallHelp      = "Approved two-block install on the exact cleanup-fixed ARM kernel."
	RestoreHelp      = "Explicit original-data restoration in the same two changed blocks."
	TargetHelp       = "rootfs blocks 28 and 29"
)
