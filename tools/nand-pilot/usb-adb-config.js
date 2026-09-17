// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
//
// USB ADB payload: the gadget modules, an adbd property area that selects the
// USB transport, and the bring-up and teardown scripts.
//
// The vendor kernel ships no USB device controller module, which an earlier
// note in this project read as "USB ADB is impossible without replacing the
// kernel". Nothing ships one, but one can be built: the SoC has the
// controller, the device tree declares it, and a driver for it exists in the
// vendor's own source. docs/usb-adb.md records how these modules were built
// and what had to match.
//
// Modules are pinned by hash for the same reason every other retained binary
// is. A module that does not match the kernel's struct module layout does not
// fail cleanly: it either panics on load or loads with its init pointer read
// as NULL, so nothing runs and nothing reports an error. Both were observed on
// hardware while these were being built.
'use strict';
const fs = require('fs');
const path = require('path');
const { hashFile } = require('./build-lib');

// The retained adbd reads its property area from an inherited descriptor and
// expects a fixed 32 KiB region.
const PROPERTY_BYTES = 32768;

// The adbd in this image is the vendor's. It is pinned because the property
// area layout below was recovered from it, and a different build would not
// necessarily read it the same way.
const ADBD_SHA256 =
  '62593dfe9580443dca064e28c38cb647f1b719fe275666d5fb4b615781864ed6';

// Load order is a dependency order, not a preference.
const GADGET_MODULES = [
  ['udc-core.ko', 'b1084820b97c93a0c0e9952c37db0029025f12f7b4dad328a444062b334d74a6'],
  ['mv_udc.ko', 'b9428e32b06ce79304a4e3373aa8963a173894a2fb9eaf5a85a2d1f68a81cfe4'],
  ['libcomposite.ko', '9f4e0e72301d51676a2b7f2fb150a309443145263b0780fce180993795c0eed8'],
  ['u_serial.ko', '8adf7716bdaf689cca143dd72f2496d4964e78c7eef013bb99287189c7819574'],
  ['usb_f_acm.ko', '02eea4de20ecd0eb641b6c403c233888406be0ab07dfe46838e2ffc37778db4c'],
  ['g_android.ko', 'd10d33d8cd2d4dedc6e020a4b931343374bb1ab4dac303a6a6d224df7b0460c2'],
];

function validateUsbAdb(value) {
  if (value === undefined) return { enabled: false };
  if (value === null || typeof value !== 'object' || Array.isArray(value))
    throw new Error('usbAdb must be an object');
  if (Object.keys(value).some(key => !['enabled', 'modules'].includes(key)))
    throw new Error('unknown usbAdb setting');
  if (value.enabled !== true && value.enabled !== false)
    throw new Error('usbAdb.enabled must be boolean');
  if (!value.enabled) {
    if (value.modules !== undefined)
      throw new Error('disabled usbAdb must not contain latent settings');
    return { enabled: false };
  }
  if (typeof value.modules !== 'string' || !value.modules)
    throw new Error('usbAdb requires a modules directory');
  return { enabled: true, modules: value.modules };
}

// usbPropertyWorkspace builds an adbd property area that selects USB.
//
// adbd picks its transport from service.adb.tcp.port. The network payload
// writes that property as 5555, which sends adbd down the TCP path so it never
// opens the gadget. An area with no properties at all leaves the lookup
// returning its default and adbd falls through to USB.
function usbPropertyWorkspace() {
  const blob = Buffer.alloc(PROPERTY_BYTES);
  blob.writeUInt32LE(0, 0);
  blob.writeUInt32LE(0x504f5250, 8);
  blob.writeUInt32LE(0x45434f76, 12);
  return blob;
}

function installUsbAdb(privateConfig, root, scriptSource) {
  const config = validateUsbAdb(privateConfig.usbAdb);
  if (!config.enabled) return config;

  const target = path.join(root, 'opt/reinvoke/usb-adb');
  if (fs.existsSync(target)) throw new Error('usb-adb payload already exists');
  fs.mkdirSync(target, { recursive: true, mode: 0o755 });

  for (const [name, expected] of GADGET_MODULES) {
    const from = path.join(config.modules, name);
    const actual = hashFile(from);
    if (actual !== expected)
      throw new Error(`usb-adb module ${name} is ${actual}, expected ${expected}`);
    fs.writeFileSync(path.join(target, name), fs.readFileSync(from), { mode: 0o644 });
  }

  // The property layout below was recovered from this exact adbd, so a
  // different build is not assumed to read it the same way.
  const adbd = path.join(root, 'sbin/adbd-root');
  if (!fs.existsSync(adbd)) throw new Error('usb-adb requires the retained adbd');
  const adbdHash = hashFile(adbd);
  if (adbdHash !== ADBD_SHA256)
    throw new Error(`usb-adb adbd is ${adbdHash}, expected ${ADBD_SHA256}`);

  // adbd is handed this as an inherited descriptor, never by path.
  fs.writeFileSync(path.join(target, 'properties'), usbPropertyWorkspace(), { mode: 0o600 });

  // Teardown is shipped so USB ADB can be stopped without a reflash. Bring-up
  // lives in the init script itself, sourced the way the network payload is.
  const from = path.join(scriptSource, 'usb-adb-down.sh');
  fs.writeFileSync(path.join(target, 'usb-adb-down.sh'), fs.readFileSync(from), { mode: 0o755 });
  fs.chmodSync(path.join(target, 'usb-adb-down.sh'), 0o755);
  return config;
}

module.exports = { validateUsbAdb, installUsbAdb, usbPropertyWorkspace, GADGET_MODULES };
