---
title: Firmware reference
description: Retained vendor inputs, image boundaries and maintenance-relevant firmware differences
ms.date: 2026-09-12
---

## Evidence scope

The vendor images below were acquired on 2026-08-26 and analyzed statically.
Image composition, scripts and binary comparisons do not establish what is
running on an installed reInvoke candidate. Use the
[product contract](current-product-contract.md) for current behavior and the
[native NAND guide](native-nand-platform.md) for the owned image.

## Retained inputs

The original source was
[coggy9/HKHacking releases](https://github.com/coggy9/HKHacking/releases).
Full archives, extracted filesystems and binaries are held privately, not
provided by this repository. Complete original hashes and source URLs live in
the acquisition sidecars.

| Input                                  | Bytes       | Identity record                     |
| -------------------------------------- | ----------: | ----------------------------------- |
| `Harman.Kardon.INVOKE.Flashing.zip`    | 263,308,270 | [P0-004a](../metadata/P0-004a.json) |
| `Harman.Kardon.INVOKE.Driver.OTA2.zip` | 224,985,786 | [P0-004b](../metadata/P0-004b.json) |
| Standalone StockRoot `83_IMAGE`        | 107,934,810 | [P0-004c](../metadata/P0-004c.json) |

The public extraction layer retains original text/configuration, driver
metadata, manuals and ZIP listings under `bundle-contents/`. Original notices
still apply; the project MIT licence does not relicense vendor material.

| Public evidence                                                                    | Listing-derived contents                                    |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| [Flashing listing](bundle-contents/invoke-flashing/LISTING.txt)                    | 49 files, three directories, 321,347,170 uncompressed bytes |
| [OTA2 listing](bundle-contents/invoke-ota2/LISTING.txt)                            | 49 files, three directories, 282,895,354 uncompressed bytes |
| [Flashing manual](bundle-contents/invoke-flashing/Instructions.pdf)                | Original vendor instructions                                |
| [OTA2 manual](bundle-contents/invoke-ota2/Instructions%20OTA2.pdf)                 | Original final-firmware instructions                        |
| [Vendor README](bundle-contents/invoke-flashing/marvell_flash_tool/README)         | Historical USB recovery workflow                            |
| [Command generator](bundle-contents/invoke-flashing/marvell_flash_tool/gen-cmd.sh) | Generic recovery allocation map                             |

A listed member is not necessarily committed here. The byte-identical Windows
driver subtree contributes 27,713,883 bytes to each archive. Files below
128 KiB total 328,621 bytes in Flashing and 330,053 in OTA2, about 0.10% and
0.12% respectively. Earlier extraction summaries used inconsistent totals;
the preserved listings are the authority for bundle accounting.

> [!WARNING]
> Vendor recovery scripts describe destructive operations, not the current
> installation procedure. The excluded filename is exactly `99_IMAGE`.
> Donor variants and the approved native bundle can all use `83_IMAGE`;
> filenames do not establish interchangeable content.

## Image formats

### Numbered image identities

Sizes and SHA-256 values identify raw members, before decompression or carving.
`81_IMAGE`, `82_IMAGE` and `99_IMAGE` match between Flashing and OTA2.

| Member               | Bytes       | Format                                               |
| -------------------- | ----------: | ---------------------------------------------------- |
| `81_IMAGE`           | 3,288,888   | Legacy U-Boot ARM uImage, uncompressed Linux payload |
| `82_IMAGE`           | 35,497,472  | gzip-wrapped `newc` cpio recovery initrd             |
| Flashing `83_IMAGE`  | 107,934,810 | Marvell/Berlin container, stock 11.1842              |
| StockRoot `83_IMAGE` | 107,934,810 | Marvell/Berlin container, rooted 11.1842             |
| OTA2 `83_IMAGE`      | 69,481,562  | Marvell/Berlin container, stock 12.2134              |
| `99_IMAGE`           | 137,694,612 | Older LS9 component container with embedded SquashFS |

```text
81_IMAGE
  dda4f295e037786c5302b91976e6b37d99bdaa108e76bb94d1337181f64c4763
82_IMAGE
  08a8f96a5c476a08ba19441d83637e606f27f442d56c2689dd6b56d2fc72b7a8
83_IMAGE (Flashing)
  90a4f54d7c92f55ea20f6d63f89caae5f7738b62dec4913bded0fd7816ec9a1c
83_IMAGE (StockRoot)
  f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7
83_IMAGE (OTA2)
  b2e12178f98a0c0904cb1e6e2ba933de0c0fef8be7c24e7852bc9933294850e8
99_IMAGE
  bc492f9717d51c7a725ffad679e340b219a3d80989dbf31a01485c748b38c9a9
```

`81_IMAGE` contains a 3,288,824-byte Linux `3.8.13-mrvl` payload; load and entry
addresses are both `0x02008000`. Embedded metadata names `MARVELL BG2CDP A0`
and `berlin2cdp`. Its development/NFS command line is build evidence, not an
observed production boot command or identification of the native kernel.

`82_IMAGE` expands to 84,123,648 bytes. The 908-entry initrd includes
`mount_part`, `flash_custk`, Berlin XML, Wi-Fi scripts and `/home/galois` tools.
These are vendor filesystem paths, not operator locations.

### Container and filesystem bounds

`83_IMAGE` starts with magic `f1a3add2` and partition metadata, including
`block0`, `pre-bootloader` and `post-bootloader` descriptors. It is not a raw
filesystem. Offsets below are from the start of the container; end offsets
are exclusive (`offset + filesystem bytes`), not NAND partition addresses.

| Variant/member                  | Offset      | Filesystem bytes | End         | Inodes |
| ------------------------------- | ----------: | ---------------: | ----------: | -----: |
| StockRoot primary               | 18,998,912  | 81,439,203       | 100,438,115 | 4,110  |
| StockRoot secondary/config      | 104,914,976 | 2,712,646        | 107,627,622 | 405    |
| OTA2 primary                    | 18,998,912  | 46,369,609       | 65,368,521  | 3,812  |
| OTA2 secondary/config           | 66,461,728  | 2,712,646        | 69,174,374  | 405    |
| `99_IMAGE` component filesystem | 10,216,512  | 25,063,940       | 35,280,452  | 1,434  |

All listed filesystems are gzip-compressed SquashFS v4. The StockRoot primary
creation timestamp is 2021-04-15 18:58:27 UTC; OTA2's is
2021-08-23 06:19:32 UTC. The secondary member dates to 2018-01-17 02:52:14 UTC.
Creation timestamps describe packed filesystems, not the full firmware's
release date.

The old stock and StockRoot primary trees each have 2,774 files,
299 directories and 1,037 symlinks. Their secondary members are byte-identical
and extract identically. The OTA2 primary has 2,543 files, 281 directories and
989 symlinks. Its secondary extraction also compared equal to the older
flashing tree; tree equality alone is not a container-byte comparison.

The `99_IMAGE` filesystem has 950 files, 299 directories and 185 symlinks,
created 2016-08-11 09:59:45 UTC. Its version text says `BUILD_DATE:19Jul16`,
`MODULE:LS9`, `VERSION:1.0`. The 25,063,940-byte bound applies to the embedded
filesystem, not the complete 137,694,612-byte container.

### Loader payloads and delivery

OTA2 is a full-image USB recovery/reflash kit, not a RedBend OTA delta package.
It carries host `usb_boot`/ADB tools, RAM loader stages and numbered images.
No RedBend delta, `.sec` blob or RB_UA update container was found in the kit;
the rootfs nevertheless retains RedBend update orchestration. Bundle contents
do not establish how the original update reached end-user units.

The original OTA2 loader identities are:

| Member             | Bytes   | SHA-256                                                            |
| ------------------ | ------: | ------------------------------------------------------------------ |
| `bootloader.img`   | 419,840 | `d8b917517ff7d00e73cd55c8c4858eba9a2838877632bd33ab9593fd528bf86f` |
| `bcm_erom.bin.usb` | 24,576  | `cae85746505ac8b9c1453e9007a7b9bea5c5be422e4f7129284f34d9d8e4e531` |
| `drm_erom.img`     | 36,864  | `36101ba1ebc913ca2da4a2c025405177dce765a3793c71342ba6ed1b0b9f3c50` |
| `sysinit.img`      | 24,576  | `687c70659c274be2773202e787a50d5aeb874557ea8eabda5e416788375633e4` |

Other small members are loader parameters or scripts: `06_IMAGE` is five
bytes of ASCII, `07_IMAGE` four binary bytes, `08_IMAGE` 144 bytes,
`09_IMAGE` 4,096 bytes and `79_IMAGE` a 231-byte U-Boot script. Windows DLLs
and driver files are host dependencies, not device firmware.

### Recovery and update configuration

The extracted initrd `rcS` mounts pseudo-filesystems, populates `/dev`, mounts
`factory_setting`, mounts `app` at `/home/galois` and `localstorage` at
`/home/galois_rwdata`, enables Android USB `acm,adb`, and launches
`/home/galois/run.sh`. This agrees with the vendor generator's `root=/dev/ram`
and initrd use, but does not prove a subsequent pivot into `83_IMAGE`.

The 596-byte `gen-cmd.sh` is identical in the two kits, SHA-256
`2a7770417e055135ff16b64ff19b782e4049817feaacb04d243572d9539e168c`.
It names paired `bootimgs`/`bootimgs-B` and `tz_en`/`tz_en-B`, plus one
192M `rootfs`, in allocations totaling 512 MiB. The observed unit instead has
256 MiB NAND, established by identification and complete logical reads.
This generic script is not its production partition geometry.

| Rootfs evidence                     | Recorded configuration                                                                 |
| ----------------------------------- | -------------------------------------------------------------------------------------- |
| `etc/otaconfig/ota_rbua_install.sh` | Stages into `/data/upgrade` and `/lsync/rbua`; installs engine/client                  |
| `rb_recovery.fstab`                 | MTD targets `bootimgs` and `rootfs`, no B-slot entry                                   |
| `rb_ua.conf`                        | `in_recovery_kernel=1`, `no_reboot=1`, `set_boot_to_recovery=0`, `fw_installer_type=9` |
| Filesystem installer types          | `11,250,251,252,253,254`                                                               |
| `fw_env.config`                     | `/dev/mtd/mtd1`, `/dev/mtd/mtd13`, `/dev/mtd/mtd14`                                    |

The OTA configuration is unchanged in OTA2. No recovered rootfs script maps
those environment sectors to an active/inactive slot or proves rollback.
New `mtd_exec` and changed `flash_bootloader`, `flash_image` and `updater`
binaries do not establish the slot chooser's implementation.
See [boot/update state](emulation/boot-update-state.md) and
[U-Boot access](uboot-access.md) for the operational boundary.

## Vendor runtime

The stock topology is a WAMP router with client daemons, not multiple servers
sharing port 9999. `system-manager /etc/podium/podium.conf` supervises services
and starts `logwrapper bonefish -r default -t 9999 -w 9998 -d`.
The realm is `default`; 9999 is RawSocket and 9998 is WebSocket.

| Component                      | Static role                                | Detailed reference                                            |
| ------------------------------ | ------------------------------------------ | ------------------------------------------------------------- |
| `mcu-interface 127.0.0.1 9999` | WAMP client, I2C/GPIO MCU control          | [MCU boundary](emulation/mcu-boundary.md)                     |
| `audio-ui 127.0.0.1 9999 ...`  | WAMP client, audio/UI coordination         | [Owned speaker control](emulation/owned-speaker-control.md)   |
| `dsp-client`                   | WAMP client, DSP firmware/control over SPI | [DSP boundary](emulation/dsp-boundary.md)                     |
| `bluetooth.sh`, `btmrvl.ko`    | Marvell SDIO Bluetooth integration         | [Bluetooth stack](emulation/bluetooth-stack.md)               |
| `mlan.ko`, `sd8xxx.ko`         | Wi-Fi integration and LS9 calibration      | [Hardware baseline](corpus/01_CANONICAL_HARDWARE_BASELINE.md) |

The owned PCM path uses BlueALSA and ALSA, not the DSP daemon's SPI control
channel. Recovered procedure signatures, transport details and validation
belong in the [control-plane reference](emulation/control-plane-emulation.md#vendor-control-surface),
not a strings-only firmware inventory.

The MCU image is `usr/share/mcu/cortana_mcu.bin`, 13,312 bytes, SHA-256
`af0db96faaa79fcff254c5c95cef858e1fc6543ad73238b740f28f0e9fd98811`.
It is identical in stock 11.1842, StockRoot and OTA2. The retained `cortana`
name and command strings do not prove the firmware currently installed on the
physical MCU, or identify its silicon.

Vendor firewall rules cover SSH, WAMP, HTTPS, UDP 48301, DHCP and mDNS, with
stock/rooted differences below. Rules and listener configuration are not proof
of reachable or authenticated services. `serviceport.sh` uses `172.20.20.20`
as a literal vendor default, not a live operator address.

## Firmware generations

### Stock 11.1842 and StockRoot

Stock self-identifies as `Barracuda_libre-11.1842.0`; StockRoot uses
`Barracuda_rooted_libre-11.1842.0`. Exactly 11 regular files differ, with no
added/removed paths or changed symlink targets.

| Changed files                                             | Rooted variant difference                                                   |
| --------------------------------------------------------- | --------------------------------------------------------------------------- |
| `etc/build.info`, `etc/distro_version`, `etc/version.txt` | Rooted tag; otherwise shared commit/build metadata                          |
| `etc/hosts`                                               | Redirects RedBend domains to `127.0.0.1`                                    |
| `etc/motd`                                                | Rooted welcome banner                                                       |
| `init.rc`                                                 | Enables USB ACM and starts `adbd`, removing its disabled setting            |
| `usr/sbin/firewall.sh`                                    | Allows 22/9998/9999 outside DEBUG; stock otherwise drops them               |
| Three WAV prompts                                         | Rooted 22.05 kHz mono PCM versus stock 44.1 kHz PCM                         |
| `SpeakerRetailDemo44kMono.mp3`                            | Rooted 22.05 kHz joint-stereo 64 kbps versus stock 44.1 kHz stereo 320 kbps |

The WAV basenames are `S_311_d_pluggedin.wav`, `C_403_d_firstupdate.wav` and
`C_406_o_oobeerror.wav`. Stock WAVs include mono and stereo.
The RedBend hosts are `redbend.com`, `saf1.redbend.com`,
`neptune.redbend.com` and `harman-podium.redbend.com`.

83,800,608 container bytes differ (77.64%), mostly from the SquashFS rebuild,
not a broad rewrite of the extracted tree. StockRoot is therefore a
rooted/recovery-oriented variant, not a separate hardware generation.

### Final stock 12.2134

OTA2 self-identifies as `Barracuda_libre-12.2134.0`, product `barracuda`,
timestamp `20210823094432`, clean build state `I0/M0/U0`.
Its build commit is `35137d55d625f3742dc3862b937435b2d8e3c256`, versus
`8bef090501e4f3e28dd47ff36158a811f7614690` for stock 11.1842.
OTA2 lacks the rooted tags, RedBend loopback mappings and welcome banner.

The comparison found 10 added, 242 removed and 373 changed regular files,
with 2,533 common paths. The kernel/initrd/older LS9 numbered images are
unchanged; among `81_IMAGE`, `82_IMAGE`, `83_IMAGE` and `99_IMAGE`, only
`83_IMAGE` differs. This is not a claim that every ZIP member matches.

The maintenance-relevant changes are:

* Cortana, Spotify, Skype integration, crash-upload tooling and many voice
  prompts are removed.
* `oobe-ui`, `wifi-blocker`, `mtd_exec`, `system-normal.sh`, a Boost date/time
  library and four Bluetooth/power/volume sounds are added.
* Podium removes Cortana/Spotify/crash-upload services and adds `oobe-ui` and
  `wifi-blocker`. The `system-normal.sh` process block is commented out.
* `wifi-blocker.conf` contains `{ "date" : "2021-09-11 00:00" }`.
  A dated connectivity gate is a hypothesis, not a proved cutoff behavior.
* `bonefish`, `dsp-client` and `music-source-manager` remain byte-identical;
  `audio-ui`, `mcu-interface`, `system-manager` and `bluetooth` change.
* The MCU firmware file remains identical even though its host client changes.

The two-line `system-normal.sh` calls `com.harman.extStateUpdate` for `system`
with named argument `state=normal`. Its presence does not prove execution.
The static URI comparison reports five additions and fifteen removals, but
the retained public removal list contained only twelve names. Use the
[control-plane reference](emulation/control-plane-emulation.md#vendor-control-surface)
for recovered interfaces rather than reconstructing signatures from strings.

These changes support interpreting 12.2134 as the final local/Bluetooth
firmware generation. They do not establish every new service's behavior.
Broad library and Android-tool changes suggest a component rebuild, not a
measured behavioral result.

### Radio and trust material

The older LS9 `99_IMAGE` includes generic `w8887`/`sd8887` generations and
May 2016 calibration, including `WlanCalData_ext-LS9-20160503.conf`.
The normal Invoke rootfs uses `sd8887_bt_a2.bin`, `sd8887_bt_a2_new.bin`,
`sd8887_wlan_a2_p78.bin` and July 2016 profiles. Byte-field differences support
later tuning, not exact radio-package identification.
`sd8887_bt_a2_new.bin` changes again in OTA2.

`etc/security/otacerts.zip` changes between 11.1842 and OTA2; the latter is
a 1,125-byte ZIP containing the public `testkey.x509.pem` verification
certificate. A certificate chain and standard trust store remain present.
This comparison is not an audit of trust decisions, remaining endpoints or
private-key handling.

## Relationship to reInvoke

The owned native image retains byte-identical vendor bootloader, TrustZone
and encrypted kernel payloads, but installation erased and reprogrammed the
whole good-block set. Retained bytes do not mean untouched boot-chain regions.
Image composition is not native shell inspection of kernel, PID1 or mounts.

Recovery and acceptance are candidate-specific. The
[native guide](native-nand-platform.md), [NAND decision](nand-write-decision.md)
and [current contract](current-product-contract.md) own those results.
The vendor generations above are donor identities and comparison baselines,
not reInvoke release numbers or evidence of a completed assistant.
