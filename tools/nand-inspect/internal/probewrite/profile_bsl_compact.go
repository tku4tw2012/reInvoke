//go:build nandbslcompact

package probewrite

const (
	Start            int64 = 0x01a20000
	Span             int64 = 0x00500000
	ImageHash              = "606923f27236a1bf86807c19354c772dea65ddefca73a0ba075a59f68c4ffec1"
	OriginalHash           = "91a9dde15fea2959232fc9a0cdafdb739e08a7498c4e8b57d867889c8f358163"
	BeforeHash             = "b5a41c3758763bbec72769fab4a2533bf2db0b6312d93d25a695f9e4b9e02260"
	AfterHash              = "75c62f48adf907f820d0568cc1966ca5dfa157633851e6556f9adbb42064e6bc"
	TableHash              = "e25ca94fac7c1fac5df425246b9ea147751a6fb1be252e322e547c807f737083"
	InstallAck             = "INSTALL-reinvoke-bsl-compact-01a20000-01f20000"
	RestoreAck             = "RESTORE-UNSUPPORTED-reinvoke-bsl-compact"
	MappingAck             = "MAP-ONLY-reinvoke-bsl-compact-01a20000-01f20000"
	HeaderLast             = true
	ResumeSupported        = false
	RestoreSupported       = false

	PlanProfileName        = "reinvoke-bsl-compact-20260911"
	FilesystemHash         = "d23e1844df71cf58b5f1313038116bc1055db9432cfb726a074e43de52bfee88"
	FilesystemBytes  int64 = 2445312
	ImageFilename          = "payload.bin"
	OriginalFilename       = "current-bsl.bin"
	FilesystemName         = "bsl.squashfs"
	InstallHelp            = "Approved compact forward-only BSL install on the exact cleanup-fixed ARM kernel."
	RestoreHelp            = "Unsupported by the forward-only nandbslcompact profile; rejected before device access."
	TargetHelp             = "BSL blocks 0 through 39; compact filesystem header written last; rootfs untouched"
)
