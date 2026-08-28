# Invoke OTA2 Driver Bundle Analysis

Static analysis of `Harman.Kardon.INVOKE.Driver.OTA2.zip`. Nothing in this
bundle was executed. Every claim below traces to a file path, byte offset, or
SHA-256 recorded during extraction. Inferences are labeled as such.

## Source and provenance

The analyzed archive is
`reinvoke-archive/originals/harman/invoke/Harman.Kardon.INVOKE.Driver.OTA2.zip`,
224,985,786 bytes, SHA-256
`f138fb1ea1175830181ca9e2f20509d3d991e709a52b9a970185b128314052cd`.
It was extracted read-only into
`reinvoke-archive/extracted/ota2/`. The originals tree was not modified.

The zip holds 52 entries in three groups: an `Instructions OTA2.pdf`, a
Marvell WinUSB driver set (`Mrvl_WinUSB_Driver_040114/`), and the `OTA2/`
payload directory. The full listing is preserved at
`docs/bundle-contents/invoke-ota2/LISTING.txt`.

## Delivery format

This distributable is a USB device-recovery and reflash kit, not a RedBend
RB_UA over-the-air delta. The `OTA2/README` documents the Marvell Berlin USB
download flow: `usb_boot` pushes `bootloader.img`, `bcm_erom.bin.usb`,
`drm_erom.img`, and `sysinit.img` into RAM, boots the `82_IMAGE` ramdisk, and
flashes the numbered `*_IMAGE` members. The bundle carries the host-side tools
for that flow (`usb_boot`, `usb_boot.exe`, `adb`, `putty.exe`, the Windows
WinUSB DLLs, `run.sh`, `run.bat`, `gen-cmd.sh`). No RedBend delta package,
`.sec` blob, or RB_UA update container is present. "OTA2" is therefore a
release label on a full-image reflash kit. The device rootfs still contains the
RedBend RB_UA install path, but no RedBend delta is shipped here. This is an
observation about the bundle contents, not a claim about how the update reached
end-user units.

## Payload inventory

Sizes are on-disk member sizes. SHA-256 values are of the raw members as
stored in the zip.

| Member | Size | SHA-256 | Format |
|---|---:|---|---|
| `OTA2/81_IMAGE` | 3,288,888 | `dda4f295e037786c5302b91976e6b37d99bdaa108e76bb94d1337181f64c4763` | U-Boot legacy uImage, Linux-3.8.13-mrvl ARM kernel, uncompressed, load/entry 0x02008000 |
| `OTA2/82_IMAGE` | 35,497,472 | `08a8f96a5c476a08ba19441d83637e606f27f442d56c2689dd6b56d2fc72b7a8` | gzip stream, original 84,123,648 bytes, wraps a newc cpio initrd |
| `OTA2/83_IMAGE` | 69,481,562 | `b2e12178f98a0c0904cb1e6e2ba933de0c0fef8be7c24e7852bc9933294850e8` | Marvell/Berlin container (magic `f1a3add2`) carrying two SquashFS v4 members |
| `OTA2/99_IMAGE` | 137,694,612 | `bc492f9717d51c7a725ffad679e340b219a3d80989dbf31a01485c748b38c9a9` | Marvell/Berlin container with embedded SquashFS (older LS9 component image) |
| `OTA2/06_IMAGE` | 5 | `4feafd8518cf536cb4a19393dcf72b615dee40a87324ee58f87da3ad9525f72c` | ASCII text `1-12`, USB loader parameter |
| `OTA2/07_IMAGE` | 4 | `bb1a016a48897d56e0865da88d54fd6d613f8277428141329bce4030cc6f9015` | 4-byte binary loader parameter |
| `OTA2/08_IMAGE` | 144 | `ee8a837d7c4f26cd0030856cdfbbca41df954c99f4972f7cd848740091466adc` | 144-byte binary loader blob |
| `OTA2/09_IMAGE` | 4,096 | `c666463722d52207a570fc8e2446a686c064609edb3cf6de2506536938296e24` | 4,096-byte binary loader blob |
| `OTA2/79_IMAGE` | 231 | `1680a0ba42f5366b7c8a04ebf91cfdda338de9429d3a795cf86fa18fb73eb861` | U-Boot autoexec script (text) |
| `OTA2/bootloader.img` | 419,840 | `d8b917517ff7d00e73cd55c8c4858eba9a2838877632bd33ab9593fd528bf86f` | Marvell boot loader image |
| `OTA2/bcm_erom.bin.usb` | 24,576 | `cae85746505ac8b9c1453e9007a7b9bea5c5be422e4f7129284f34d9d8e4e531` | eROM stage loader |
| `OTA2/drm_erom.img` | 36,864 | `36101ba1ebc913ca2da4a2c025405177dce765a3793c71342ba6ed1b0b9f3c50` | DRM eROM image |
| `OTA2/sysinit.img` | 24,576 | `687c70659c274be2773202e787a50d5aeb874557ea8eabda5e416788375633e4` | System init loader |
| `OTA2/adb` | 4,581,967 | `275ce7bab45b0e9d68dd159592dc8dff47091266331dd06db44d41a69cf10b16` | ELF 32-bit x86 host adb binary |
| `OTA2/usb_boot` | 41,685 | `aabbdde5bd1949df602ca8af414b948fdc6fb9418f15283348736cfefe0bb5dc` | ELF 64-bit x86-64 host flashing tool |
| `OTA2/usb_boot.exe` | 780,288 | `c33fb5ef23bfa0bf9c2f7fb4236e015f5a5fbcb6b7b94cfe5de764f12f4a6e49` | PE32 host flashing tool (Windows) |
| `OTA2/gen-cmd.sh` | 596 | `2a7770417e055135ff16b64ff19b782e4049817feaacb04d243572d9539e168c` | Bourne-Again shell, emits U-Boot boot args and NAND `mtdparts` |

Host-side Windows helper binaries (`MSVCP120D.dll`, `MSVCR120D.dll`,
`pthreadVC2.dll`, `putty.exe`) and the WinUSB driver DLLs are recorded with
full hashes in the per-member manifest and are not device firmware.

### 83_IMAGE internal members

The `83_IMAGE` container begins with the Marvell/Berlin partition header
(`f1a3add2`) listing `block0`, `pre-bootloader`, and `post-bootloader`
descriptors. Two SquashFS v4 members follow, carved to
`reinvoke-archive/extracted/ota2/83_members/`.

| Member | Offset | FS size | Compression | Created | Inodes |
|---|---:|---:|---|---|---:|
| primary rootfs | 18,998,912 | 46,369,609 | gzip | 2021-08-23 06:19:32 UTC | 3,812 |
| secondary/config | 66,461,728 | 2,712,646 | gzip | 2018-01-17 02:52:14 UTC | 405 |

The primary tree extracts to 2,543 files, 281 directories, and 989 symlinks.
The secondary tree is byte-identical to the phase 3 flashing secondary member
(`diff -rq` reports no differences), so the config/secondary member is
unchanged across the 11.x and 12.x builds.

## Version relationship

The OTA2 primary rootfs self-identifies as `Barracuda_libre-12.2134.0`.

| Field | OTA2 value | Prior stock (phase 3) |
|---|---|---|
| `etc/build.info` tag | `Barracuda_libre-12.2134.0` | `Barracuda_libre-11.1842.0` |
| `etc/distro_version` | `Barracuda_libre-12.2134.0` | `Barracuda_libre-11.1842.0` |
| `etc/version.txt` | `Barracuda_libre-12.2134.0` | `Barracuda_libre-11.1842.0` |
| `etc/version` timestamp | `20210823094432` | prior build date |
| `BUILD_GIT_COMMIT` | `35137d55d625f3742dc3862b937435b2d8e3c256` | `8bef090501e4f3e28dd47ff36158a811f7614690` |
| `BUILD_GIT_DIRTY` | `I0/M0/U0` (clean) | `I0/M0/U0` (clean) |
| `BUILD_PRODUCT` | `barracuda` | `barracuda` |

OTA2 is a newer build on the same `barracuda` product line and the same stock
(non-rooted) lineage. The version tag encodes year and ISO week as an
inference: `11.1842` reads as 2018 week 42, and `12.2134` reads as 2021 week
34, which is consistent with the 2021-08-23 primary SquashFS creation date. The
rooted StockRoot variant analyzed in phase 3 was
`Barracuda_rooted_libre-11.1842.0`; OTA2 carries no rooted markers. Its
`etc/hosts` is clean stock (no RedBend hostnames redirected to `127.0.0.1`) and
`etc/motd` is empty, unlike the rooted build.

The kernel, initrd, and older component images are byte-identical to the phase
3 Flashing bundle. `81_IMAGE`, `82_IMAGE`, and `99_IMAGE` SHA-256 values match
the values recorded in `FINDINGS.md` exactly. Only the `83_IMAGE` rootfs
container differs between the two distributables.

## Rootfs diff against the 11.1842 baseline

The comparison baseline is the phase 3 flashing rootfs
(`Barracuda_libre-11.1842.0`), which shares the same tree shape as the rooted
StockRoot tree, so the added and removed counts are identical against either
11.x baseline. Full lists are preserved under
`reinvoke-archive/extracted/ota2/diff-vs-11.1842/`.

| Class | Count |
|---|---:|
| Files added in OTA2 | 10 |
| Files removed in OTA2 | 242 |
| Files changed (same path, different content) | 373 |
| Common paths | 2,533 |

The dominant change is a Cortana and cloud-service teardown paired with a
standalone local-mode addition. The 242 removals are concentrated in
`usr/share` (136), `usr/lib` (57), and `usr/bin` (45), and the removed set
includes the Cortana voice assistant, Spotify, Skype call support, the crash
uploaders, and developer tooling.

### Added files

```
etc/podium/wifi-blocker.conf
usr/bin/mtd_exec
usr/bin/oobe-ui
usr/bin/system-normal.sh
usr/bin/wifi-blocker
usr/lib/libboost_date_time.so.1.65.1
usr/share/sounds/podium/BT_Connected.wav
usr/share/sounds/podium/BT_Pairing.wav
usr/share/sounds/podium/Power_On.wav
usr/share/sounds/podium/Volume_Max.wav
```

### Representative removals

```
usr/bin/cortana
usr/bin/cortana-harness
usr/bin/cortana.sh
usr/bin/spotify
usr/bin/proxy
usr/lib/libskype_call.so
usr/share/cortana/heycortana_en-US.table
usr/share/cortana/heycortana_en-GB.table
usr/share/sounds/cortana/*      (S_1xx/S_2xx/S_3xx call and listening prompts)
etc/skype/media.json
etc/spotify_client_id.txt
usr/bin/dpkg*                   (full dpkg toolchain)
usr/bin/xz*, lzmadec, lzmainfo (compression tooling)
usr/bin/bugreport, send-bugreport, upload-bugreport.sh, snap-logs.sh
usr/bin/crash-uploader-HK.sh, crash-uploader-MS.sh
```

### Process supervision change

`etc/podium/podium.conf` is a changed file. The diff shows the runtime service
list moving off the cloud assistant and onto a local out-of-box and Wi-Fi
gating mode.

- Removes the `cortana-harness` process (`command=cortana.sh $boot_mode`).
- Removes the `spotify` process and the `crash-uploader-HK.sh` process.
- Adds an `oobe-ui` process (`command=logwrapper oobe-ui`).
- Adds a `wifi-blocker` process (`command=logwrapper /usr/bin/wifi-blocker`),
  paired with `etc/podium/wifi-blocker.conf` whose only content is
  `{ "date" : "2021-09-11 00:00" }`.
- Leaves a `system-normal.sh` process block present but commented out.

`usr/bin/oobe-ui` and `usr/bin/wifi-blocker` are new stripped ARM EABI5 ELF
binaries. `usr/bin/system-normal.sh` is a two-line shell script that calls
`test-wamp-client -c com.harman.extStateUpdate` with
`{"pos_args":["system"],"nam_args":{"state":"normal"}}`.

The interpretation, labeled as inference, is that OTA2 is the end-of-life
conversion that retires Cortana and repurposes the Invoke toward a local
out-of-box and Bluetooth-speaker mode with Wi-Fi gated by a dated blocker. The
`wifi-blocker.conf` date of 2021-09-11 and the primary rootfs build date of
2021-08-23 are consistent with that reading. This is inference from file
contents, not a decompiled behavior proof.

## WAMP service and URI changes

The core WAMP router and several clients are unchanged byte-for-byte between
11.1842 and 12.2134.

| Binary | Status | Evidence |
|---|---|---|
| `usr/bin/bonefish` | unchanged | SHA-256 identical (`f8ca28a9...`) in both trees |
| `usr/bin/dsp-client` | unchanged | SHA-256 identical (`a6ce3ff8...`) in both trees |
| `usr/bin/music-source-manager` | unchanged | SHA-256 identical (`5b2743a8...`) in both trees |
| `usr/bin/audio-ui` | changed | present in both, different hash |
| `usr/bin/mcu-interface` | changed | present in both, different hash |
| `usr/bin/system-manager` | changed | present in both, different hash |
| `usr/bin/bluetooth` | changed | present in both, different hash |
| `usr/bin/cortana-harness` | removed | present in 11.1842, absent in OTA2 |

The WAMP URI universe was extracted from `usr/bin`, `usr/lib`, and `system` in
both trees. The net change is five URIs added and fifteen removed.

Added in OTA2:

```
com.harman.heartbeat.oobe
com.harman.heartbeat.wifi
com.harman.oobe
com.harman.vui.setFactoryResetMode
com.harman.wifi
```

Removed in OTA2:

```
com.cortana.device
com.cortana.gstreamer
com.harman.cortana
com.harman.cortana.alertState
com.harman.spotify.auth
com.harman.spotify.authFailed
com.harman.spotify.becameInactive
com.harman.spotify.deviceInfoChanged
com.harman.spotify.deviceInfoGet
com.harman.spotify.isLoggedIn
com.harman.spotify.logout
com.harman.spotify.lostPermission
```

So the control-plane vocabulary does change between versions, and the direction
matches the file-tree teardown: cloud voice and music procedures are dropped,
and local out-of-box, Wi-Fi, and factory-reset procedures are introduced. The
bus transport itself is unchanged, since `bonefish` is byte-identical.

## MCU firmware

The OTA does not carry a different MCU firmware. `usr/share/mcu/cortana_mcu.bin`
has SHA-256 `af0db96faaa79fcff254c5c95cef858e1fc6543ad73238b740f28f0e9fd98811`
in the OTA2 rootfs, and that value is byte-for-byte identical to both the phase
3 flashing and phase 3 StockRoot trees. It does not appear in the added,
removed, or changed lists. A `strings` comparison is therefore unnecessary to
establish equality, and the MCU command vocabulary (`cortana_mcu #`, `rgb`,
`led`, `ver`, `up app`, `flash_libre`) is unchanged by construction. The MCU
firmware file name still contains `cortana` even though the host-side Cortana
stack is removed.

## Boot-slot evidence

The bundled `gen-cmd.sh` prints the NAND partition map used for USB-dongle
boot. It enumerates redundant A and B slots for the boot images and TrustZone,
and a single rootfs partition.

```
mtdparts=mv_nand:128K(block0),1M(pre-bootloader),1408K(env),512K(aligned),
2M(post-bootloader),2M(post-bootloader),16M(factory_setting),
16M(tz_en),16M(tz_en-B),16M(bootimgs),16M(bootimgs-B),
192M(rootfs),152M(app),16M(localstorage),64M(BDlocalstorage),1M(bbt)
```

This confirms `bootimgs` and `bootimgs-B` as paired boot-image slots and
`tz_en` and `tz_en-B` as paired TrustZone slots, while `rootfs` is a single
192M partition. That is new confirmation of the on-NAND slot layout named in
`FINDINGS.md`. This `gen-cmd.sh` is byte-identical to the flashing bundle
(SHA-256 `2a7770417e...`), and its `mtdparts` string is the USB recovery boot
argument, so it documents the partition table rather than the production boot
command.

The OTA rootfs does not resolve the slot-selection algorithm. The RedBend OTA
config is unchanged: `etc/otaconfig/rb_recovery.fstab` still names only
`bootimgs` and `rootfs` as MTD update targets with no B-slot entry, and no
`etc/otaconfig` file appears in the changed list. `etc/fw_env.config` still maps
`/dev/mtd/mtd1`, `/dev/mtd/mtd13`, and `/dev/mtd/mtd14` without a script that
binds those environment sectors to an active or inactive slot. A new
`usr/bin/mtd_exec` binary is added and `system/bin/flash_bootloader`,
`system/bin/flash_image`, and `system/bin/updater` are changed, but confirming
that any of them implements slot selection would require binary analysis and is
not asserted here. The chooser most likely lives in U-Boot or the boot
environment rather than the rootfs, which is an inference.

## Certificate and endpoint notes

Presence only, no secret material extracted. `etc/security/otacerts.zip` is a
changed file between builds; it is a 1,125-byte zip whose single entry is the
public `testkey.x509.pem` OTA verification certificate. `etc/cert-chain.pem`
and the standard `etc/ssl/certs` trust store are present. No private keys were
opened or recorded. The removal of `etc/spotify_client_id.txt`,
`etc/skype/media.json`, and the Cortana and Spotify URIs is the only
cloud-endpoint-relevant change observed, and it is a net removal of cloud
integration rather than an added endpoint.

## Bulk rebuild changes

Most of the 373 changed files are an expected cross-version recompilation:
245 changed files under `usr/lib`, 48 under `system/bin`, and 28 under
`system/lib`, plus `init`, `sbin/adbd`, `openssl`, `wpa_supplicant`,
`hostapd`, `dnsmasq`, and the Android system server and service manager. The
Marvell Bluetooth firmware `lib/firmware/mrvl/sd8887_bt_a2_new.bin` changed
content (OTA2 `c04b50f8...` versus 11.x `c861c0d9...`). These are consistent
with a normal toolchain and component refresh between 11.1842 and 12.2134 and
do not by themselves indicate a behavioral change.

## Unresolved items

- The exact boot-slot selection and rollback algorithm remains unresolved. The
  A/B partition names for `bootimgs` and `tz_en` are confirmed, and `rootfs` is
  single, but no rootfs script maps `fw_env.config` sectors to an active slot.
- The internal behavior of the new `oobe-ui`, `wifi-blocker`, and `mtd_exec`
  binaries is not established. Their roles are inferred from `podium.conf`,
  file names, and the `wifi-blocker.conf` date, not from decompilation.
- Whether `wifi-blocker` enforces a hard connectivity cutoff, and what the
  2021-09-11 date gates, is an inference from the config value and is not proven
  by executing or disassembling the binary.
- The delivery path of this release to end-user units is not evidenced here.
  This bundle is a full-image USB reflash kit; no RedBend delta package is
  present, even though the rootfs retains the RB_UA install path.
- The `99_IMAGE` older LS9 component image is byte-identical to the phase 3
  copy and was not re-extracted; its contents are already covered in
  `FINDINGS.md`.
