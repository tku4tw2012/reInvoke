//go:build nandbsl

package probewrite

// Selecting nandpilot together with nandbsl deliberately fails compilation
// with duplicate profile constants instead of silently choosing either target.
const (
	Start            int64 = 0x01a20000
	Span             int64 = 0x00500000
	ImageHash              = "91a9dde15fea2959232fc9a0cdafdb739e08a7498c4e8b57d867889c8f358163"
	OriginalHash           = "238187a1d490d43dc9e44c658f88735900d3b6dfc7ed312313b9d267156dd92c"
	BeforeHash             = "b5a41c3758763bbec72769fab4a2533bf2db0b6312d93d25a695f9e4b9e02260"
	AfterHash              = "75c62f48adf907f820d0568cc1966ca5dfa157633851e6556f9adbb42064e6bc"
	TableHash              = "e25ca94fac7c1fac5df425246b9ea147751a6fb1be252e322e547c807f737083"
	InstallAck             = "INSTALL-reinvoke-bsl-v2-01a20000-01f20000"
	RestoreAck             = "RESTORE-UNSUPPORTED-reinvoke-bsl-v2"
	MappingAck             = "MAP-ONLY-reinvoke-bsl-v2-01a20000-01f20000"
	HeaderLast             = true
	ResumeSupported        = false
	RestoreSupported       = false

	PlanProfileName        = "reinvoke-bsl-v2-20260911-slim02"
	FilesystemHash         = "16921ec74f3f88f19bba741319f9a60c2adefbf6da2e655b418f0ebc564d3194"
	FilesystemBytes  int64 = 5111808
	ImageFilename          = "payload.bin"
	OriginalFilename       = "current-bsl.bin"
	FilesystemName         = "bsl.squashfs"
	InstallHelp            = "Approved forward-only BSL v2 install on the exact cleanup-fixed ARM kernel."
	RestoreHelp            = "Unsupported by the forward-only nandbsl profile; rejected before device access."
	TargetHelp             = "BSL blocks 0 through 39; changed filesystem header block written last; rootfs untouched"
)
