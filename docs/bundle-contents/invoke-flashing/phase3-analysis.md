# Phase 3 extracted analysis

This text layer records the small, high-value configuration evidence extracted
from firmware held in the sibling archive. Binary payloads and complete
filesystem trees remain outside Git.

## Boot chain

- `81_IMAGE`: U-Boot legacy ARM uImage, Linux `3.8.13-mrvl`, load/entry
  address `0x02008000`.
- `82_IMAGE`: gzip-wrapped `newc` cpio initrd. Its `etc/init.d/rcS` mounts
  `factory_setting`, `app`, and `localstorage`, enables Android USB
  `acm,adb`, and launches `/home/galois/run.sh`.
- `83_IMAGE`: normal Invoke rootfs container with primary and secondary
  SquashFS members.
- `99_IMAGE`: older LS9 component image containing Berlin configuration and
  Marvell radio firmware.

## OTA evidence

The normal rootfs contains:

```text
etc/otaconfig/ota_rbua_install.sh
etc/otaconfig/rb_ua.conf
etc/otaconfig/rb_recovery.fstab
etc/otaconfig/run.sh
```

`rb_recovery.fstab` names `bootimgs` and `rootfs` as MTD targets. `rb_ua.conf`
sets `in_recovery_kernel=1`, `no_reboot=1`, `set_boot_to_recovery=0`,
`fw_installer_type=9`, and filesystem installer types `11,250,251,252,253,254`.
The installer is staged under `/data/upgrade` and `/lsync/rbua`.

`fw_env.config` references `/dev/mtd/mtd1`, `/dev/mtd/mtd13`, and
`/dev/mtd/mtd14`, but the preserved text layer contains no direct mapping from
those environment sectors to an active/inactive boot slot. Exact slot
selection and rollback behavior remain unknown.

## Radio lineage

The older `99_IMAGE` includes multiple generic Marvell `sd8887`/`w8887`
firmware generations and calibration profiles, including
`WlanCalData_ext-LS9-20160503.conf`. The newer normal rootfs narrows this to
Invoke-era `sd8887` assets (`sd8887_bt_a2.bin`, `sd8887_bt_a2_new.bin`,
`sd8887_wlan_a2_p78.bin`) and July 2016 LS9 calibration profiles.
The calibration files differ in multiple byte fields; this is evidence of
later board/profile tuning, not enough to infer the exact radio package
revision.
