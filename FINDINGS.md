# Phase 3 Firmware Findings

## 83_IMAGE format and extraction

The preserved standalone `83_IMAGE` has SHA-256
`f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7` and is
107,934,810 bytes. It is not a raw filesystem: it begins with an outer
Marvell/Berlin image header containing partition metadata (magic
`f1a3add2`), followed by embedded filesystem material.

Magic inspection and `unsquashfs -stat` identified two SquashFS v4 members:

| Member | Offset | Size | Compression | Created | Inodes |
|---|---:|---:|---|---|---:|
| primary rootfs | 18,998,912 | 81,439,203 | gzip | 2021-04-15 18:58:27 UTC | 4,110 |
| secondary/config | 104,914,976 | 2,712,646 | gzip | 2018-01-17 02:52:14 UTC | 405 |

Both members were extracted with `unsquashfs` into the sibling archive
directory at `reinvoke-archive/extracted/phase3/`. Originals were not
modified. The secondary member is byte-identical in the two variants at both
the filesystem and extracted-tree level.

## StockRoot versus Flashing.zip

The second image was extracted from
`Harman.Kardon.INVOKE.Flashing.zip` member `marvell_flash_tool/83_IMAGE`; its
SHA-256 is
`90a4f54d7c92f55ea20f6d63f89caae5f7738b62dec4913bded0fd7816ec9a1c`.
The primary SquashFS has the same 4,110-inode tree shape in both images, with
2,774 files, 299 directories, and 1,037 symlinks. There are no path or
symlink-target additions/removals. Only these 11 regular files have different
contents:

- `etc/build.info`: `Barracuda_rooted_libre-11.1842.0` versus
  `Barracuda_libre-11.1842.0`; the rooted build keeps the same commit and
  build metadata but changes the build tag.
- `etc/distro_version` and `etc/version.txt`: the same `rooted_libre` marker.
- `etc/hosts`: four OTA service hostnames resolve to `127.0.0.1`
  (`redbend.com`, `saf1.redbend.com`, `neptune.redbend.com`, and
  `harman-podium.redbend.com`).
- `etc/motd`: `welcome to your rooted podium`.
- `init.rc`: enables the Android USB ACM instance, starts `adbd`, and comments
  out the `disabled` directive for the `adbd` service.
- `usr/sbin/firewall.sh`: the rooted variant places SSH/WAMP allow rules
  outside the debug conditional and removes the three corresponding DROP
  rules; the Flashing.zip variant allows them only when `DEBUG` is set and
  otherwise drops ports 9998, 9999, and 22.
- `usr/share/sounds/cortana/S_311_d_pluggedin.wav`,
  `usr/share/sounds/tts_en-US/C_403_d_firstupdate.wav`, and
  `usr/share/sounds/tts_en-US/C_406_o_oobeerror.wav`: rooted copies are
  mono 22.05 kHz PCM, while the stock copies are stereo or mono 44.1 kHz PCM.
- `usr/share/sounds/tts_en-US/SpeakerRetailDemo44kMono.mp3`: rooted copy is
  22.05 kHz joint-stereo 64 kbps; stock copy is 44.1 kHz stereo 320 kbps.

Thus the 83,800,608 differing raw bytes do not represent a broad source-tree
rewrite. They are primarily a different SquashFS rebuild plus 11 deliberate
file-content changes. The extracted evidence supports interpreting
`StockRoot` as a rooted/recovery-oriented variant, not as a separately
reorganized root filesystem.

## 81_IMAGE, 82_IMAGE, and 99_IMAGE

The three images from `Harman.Kardon.INVOKE.Flashing.zip` were preserved under
`reinvoke-archive/extracted/phase3/images/` and classified without execution:

| Image | SHA-256 | Format / result |
|---|---|---|
| `81_IMAGE` | `dda4f295e037786c5302b91976e6b37d99bdaa108e76bb94d1337181f64c4763` | U-Boot legacy `uImage`, uncompressed ARM Linux kernel |
| `82_IMAGE` | `08a8f96a5c476a08ba19441d83637e606f27f442d56c2689dd6b56d2fc72b7a8` | gzip-wrapped ASCII `cpio` (`newc`) initrd |
| `99_IMAGE` | `bc492f9717d51c7a725ffad679e340b219a3d80989dbf31a01485c748b38c9a9` | Marvell/Berlin container with embedded SquashFS |

`81_IMAGE` is a 3,288,824-byte Linux 3.8.13-mrvl ARM kernel image. Its
uImage load address and entry point are both `0x02008000`; the header describes
an uncompressed 3,288,824-byte kernel payload.

`82_IMAGE` is a 35,497,472-byte gzip stream with an original size of
84,123,648 bytes. Its extracted initrd contains 908 entries, including
`mount_part`, `flash_custk`, Berlin configuration XML, Wi-Fi setup scripts,
and the `/home/galois` runtime/tooling tree. The initrd's `etc/init.d/rcS`
mounts `factory_setting`, `app`, and `localstorage`, then explicitly enables
the Android USB `acm,adb` functions before launching `/home/galois/run.sh`.
This confirms its role as a boot/initrd environment rather than the normal
rootfs.

`99_IMAGE` contains a SquashFS v4 member at byte offset 10,216,512. The member
is 25,063,940 bytes, gzip-compressed, has 1,434 inodes, and was created
2016-08-11 09:59:45 UTC. Its extracted tree has 950 files, 299 directories,
and 185 symlinks. The tree includes `version.txt` (`BUILD_DATE:19Jul16`,
`MODULE:LS9`, `VERSION:1.0`), Berlin hardware/software configuration XML,
factory/update tools, and Marvell Wi-Fi/Bluetooth firmware including
`w8887`, `sd8887`, and calibration files. It is therefore an older LS9
software/component image, distinct from the 2021 primary `83_IMAGE` rootfs.

## Boot and OTA correlation

The `81_IMAGE` uImage embeds Linux 3.8.13-mrvl platform metadata identifying
the `MARVELL BG2CDP A0` / `berlin2cdp` board and Berlin drivers including
USB, NAND, audio, GPU, and SDIO subsystems. The image also contains a
development/NFS command line, so those strings are kernel build evidence and
not the production boot command.

The `82_IMAGE` initrd startup script provides the recovery/bring-up path:

1. Mount kernel pseudo-filesystems and populate `/dev`.
2. Mount `factory_setting` at `/tmp/factory_setting`.
3. Mount `app` at `/home/galois` and `localstorage` at
   `/home/galois_rwdata`.
4. Enable Android USB functions `acm,adb`.
5. Launch `/home/galois/run.sh`.

The script is consistent with the `gen-cmd.sh` command line, which boots with
`root=/dev/ram` and supplies `82_IMAGE` as the initrd. This is analysis of
script contents only; no firmware or binaries were executed.

The normal `83_IMAGE` rootfs includes a RedBend RB_UA OTA installation path.
`etc/otaconfig/ota_rbua_install.sh` stages the installer into `/data/upgrade`
and `/lsync/rbua`, installs `rb_ua`, and starts the OTA engine/client. Its
`rb_recovery.fstab` names `bootimgs` and `rootfs` as MTD update targets. The
configuration sets `in_recovery_kernel=1`, `fw_installer_type=9`, filesystem
installer types `11,250–254`, and `no_reboot=1`. A staging marker can rewrite
the product identity in `tree.xml` from `Harman_Invoke` to `Harman_Podium`.

This establishes a documented update chain: recovery initrd and kernel provide
the low-level boot environment, while the normal rootfs carries the OTA
orchestration and RedBend configuration. It does not by itself establish the
exact boot-slot selection algorithm or prove that every listed installer type
is used on Invoke hardware.

The rootfs includes `fw_env.config` entries for `/dev/mtd/mtd1`,
`/dev/mtd/mtd13`, and `/dev/mtd/mtd14`, but no preserved script directly maps
those environment entries to an active/inactive boot slot. The available
evidence therefore confirms redundant partition names and OTA targets, while
the exact boot-slot selection and rollback algorithm remain unresolved.

The older `99_IMAGE` radio inventory contains generic Marvell `sd8887`/`w8887`
firmware and a May 2016 LS9 calibration profile. The newer normal rootfs
contains Invoke-era `sd8887` assets and July 2016 LS9 calibration profiles.
The calibration profiles differ at multiple byte fields, supporting a later
board/profile-tuning revision while not proving an exact radio package version.

## Discovery-only sources

The P0-002 Drive URL is currently reachable as a Google Drive folder titled
`Chromecast Opensource Code`, but its page exposes folder metadata rather than
a downloadable child-file listing through the unauthenticated request used for
this analysis. No artifact was acquired from it.

The P0-005 historical Harman URL is currently denied with HTTP 403 and
redirects toward an `opensource.html` path. No archived page or downloadable
artifact was acquired. These remain discovery-only items and are not promoted
to hardware evidence.
