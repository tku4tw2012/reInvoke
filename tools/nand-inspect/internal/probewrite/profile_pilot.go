//go:build nandpilot

package probewrite

const (
	Start            int64 = 0x02920000
	Span             int64 = 0x02680000
	ImageHash              = "c580a8ff440fee68bd001777a16e24b6feb4446e84b1e7d44d34cd1fd3f37e17"
	OriginalHash           = "3bb7f29b7961424c9b27d3e908477643840a6cf3967f65a0e7ac33990c26fc6d"
	BeforeHash             = "b5a41c3758763bbec72769fab4a2533bf2db0b6312d93d25a695f9e4b9e02260"
	AfterHash              = "7bab4a26440a3362ada1ea6b6cd9c2a886d46d77763cfa4dbc4a8425a60084e0"
	TableHash              = "53fab213e1be985fae65abaaf1ad9de6fbb9f5f7c66cb2316bd65f2ef88d992f"
	InstallAck             = "INSTALL-rootfs-pilot-pty01-02920000-04fa0000"
	RestoreAck             = "RESTORE-original-rootfs-pilot-pty01-02920000-04fa0000"
	MappingAck             = "MAP-ONLY-rootfs-pilot-pty01-02920000-04fa0000"
	HeaderLast             = true
	ResumeSupported        = true
	RestoreSupported       = true

	PlanProfileName        = "reinvoke-rootfs-pilot-01-pty01"
	FilesystemHash         = "2f1622c5573a1dfe777f5595206d5750a2afb11e22809f8c0e063189fa27624a"
	FilesystemBytes  int64 = 40267776
	ImageFilename          = "payload.bin"
	OriginalFilename       = "rollback.bin"
	FilesystemName         = "rootfs.squashfs"
	InstallHelp            = "Approved complete-rootfs PTY-fixed pilot on the exact cleanup-fixed ARM kernel."
	RestoreHelp            = "Explicit original-data restoration in the same fixed rootfs pilot extent."
	TargetHelp             = "rootfs pilot blocks 0 through 307; changed filesystem header block written last"
)
