// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const zlib = require('zlib');
const { patchRuntime } = require('./patch-runtime');
const { readConfig, installConfig } = require('./private-config');
const lib = require('./build-lib');
const { run, json, hashFile, pins, inventory, verify } = lib;
const here = __dirname;
const [mode, archiveArg, outputArg] = process.argv.slice(2);
const archive = path.resolve(archiveArg || '../../reinvoke-archive');
const output = path.resolve(outputArg || '.');
const input = key => path.join(archive, pins[key].path);
const install = (src, dst, mode = '0755') => {
  fs.mkdirSync(path.dirname(dst), { recursive: true });
  run('install', ['-m', mode, src, dst]);
};
const write = (file, value, mode = 0o644) => {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, value, { mode });
};
const link = (target, file) => {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.rmSync(file, { force: true }); fs.symlinkSync(target, file);
};
function normalize(tree) {
  run('chmod', ['0755', tree]);
  run('chown', ['-hR', '0:0', tree]);
  run('find', [tree, '-exec', 'touch', '-h', '-d', '@0', '{}', '+']);
}
function cpioPack(root, out) {
  run('bash', ['-o', 'pipefail', '-c',
    'cd "$1" && find . -print0 | LC_ALL=C sort -z | cpio --null -o -H newc --reproducible --quiet | gzip -n -9 >"$2"',
    'pack', root, out]);
}
function prepare() {
  const privateConfig = readConfig(process.env.PILOT_PRIVATE_CONFIG);
  for (const key of Object.keys(pins)) verify(input(key), pins[key]);
  const original = path.join(output, 'source-rc12'), stock = path.join(output, 'source-stock');
  fs.mkdirSync(original);
  run('bash', ['-o', 'pipefail', '-c',
    'gzip -dc "$1" | (cd "$2" && cpio -idm --quiet --no-absolute-filenames)', 'extract', input('rc12'), original]);
  run('unsquashfs', ['-processors', '1', '-no-progress', '-d', stock, input('stock')]);
  json(path.join(output, 'source-rc12-manifest.json'), inventory(original));
  const originalInit = fs.readFileSync(path.join(original, 'init'));
  const patchedInit = patchRuntime(originalInit);
  if (hashFile(path.join(original, 'sbin/adbd-root')) !== lib.ADB_SHA256)
    throw new Error('unexpected RC12 adbd-root');
  const qemu = path.join(archive, 'emulation/qemu-arm-static');
  const applets = run(qemu, [path.join(original, 'bin/busybox'), '--list']).trim().split('\n');
  const originalBB = fs.readFileSync(path.join(original, 'bin/busybox'));
  if (lib.sha(originalBB) !== lib.BB_SHA256) throw new Error('unreviewed static BusyBox');
  json(path.join(output, 'busybox-provenance.json'), {
    sourceSha256: lib.BB_SHA256, packagedSha256: lib.BB_SHA256, binaryUnmodified: true,
    genericStorageApplets: 'retained exactly as RC12; trusted-root DIY system, not a security sandbox',
    startupPolicy: 'no installer, flash_custk, OTA/autoflash or storage-writing startup invocation',
  });
  const root = path.join(output, 'runtime'), boot = path.join(output, 'bootstrap');
  run('cp', ['-a', original, root]);
  write(path.join(root, 'init'), patchedInit, 0o755);
  // Replace dormant recovery boot entry points, not owned RC12 services.
  for (const file of ['sbin/init', 'etc/inittab', 'etc/init.d',
    'usr/bin/aserver', 'usr/bin/curl', 'usr/bin/glib-genmarshal',
    'usr/bin/gobject-query', 'usr/bin/gtester', 'usr/bin/xmlwf',
    'usr/bin/c_rehash', 'usr/bin/glib-gettextize', 'usr/bin/glib-mkenums',
    'usr/bin/gtester-report']) fs.rmSync(path.join(root, file), { force: true, recursive: true });
  link('/init', path.join(root, 'sbin/init'));
  link('busybox', path.join(root, 'bin/sh'));
  link('busybox', path.join(root, 'bin/ash'));
  write(path.join(root, 'etc/profile'), 'export PATH=/sbin:/bin:/usr/sbin:/usr/bin\nexport HOME=/root\numask 022\n');
  installConfig(privateConfig, root);
  fs.rmSync(path.join(root, 'lib/modules'), { recursive: true });
  const modules = [];
  const suffixes = ['wlan_sd8887/mlan.ko', 'wlan_sd8887/sd8xxx.ko', 'bt_sd8887/bt8xxx.ko'];
  for (const [release, source] of [['3.8.13-yocto-standard', stock], ['3.8.13-reinvoke-audio-sd8887', original]]) {
    for (const suffix of suffixes) {
      const relative = `lib/modules/${release}/kernel/arch/arm/mach-berlin/modules/${suffix}`;
      const from = path.join(source, relative), to = path.join(root, relative);
      const strings = run('strings', [from]).split('\n');
      const vermagic = strings.find(s => s.startsWith('vermagic='));
      if (!vermagic?.startsWith(`vermagic=${release} `)) throw new Error(`mismatched module ${relative}`);
      install(from, to, '0644');
      modules.push({ path: relative, sha256: hashFile(from), vermagic,
        depends: strings.find(s => s.startsWith('depends=')),
        parameters: strings.filter(s => s.startsWith('parm=')) });
    }
  }
  json(path.join(output, 'module-manifest.json'), modules);
  const builtins = fs.readFileSync(path.join(stock, 'lib/modules/3.8.13-yocto-standard/modules.builtin'), 'utf8')
    .trim().split('\n');
  for (const expected of ['kernel/sound/drivers/snd-aloop.ko',
    'kernel/sound/soc/codecs/snd-soc-wm8904.ko', 'kernel/fs/squashfs/squashfs.ko']) {
    if (!builtins.includes(expected)) throw new Error(`stock built-in inventory missing ${expected}`);
  }
  json(path.join(output, 'kernel-compatibility.json'), {
    acceptedReleases: ['3.8.13-yocto-standard', '3.8.13-reinvoke-audio-sd8887'],
    stockBuiltins: builtins.filter(s => s.includes('sound/') || s.includes('squashfs')),
    stockStartupSources: ['sbin/wpa_supplicant_setup.sh', 'usr/bin/bluetooth.sh'].map(name =>
      ({ path: name, sha256: hashFile(path.join(stock, name)),
        moduleCommands: fs.readFileSync(path.join(stock, name), 'utf8').split('\n').filter(s => s.includes('insmod')) })),
    stockKernelRuntime: 'UNTESTED; matching vermagic/parameters and builtin inventory are prerequisites, not proof',
    gadget: 'legacy android_usb with android_adb misc ABI checked at boot; unavailable diagnostics are recorded and do not block the hardware runtime',
    parameters: 'stock calibration/power and BT parameters retained; RC12 mlan naming/MAC and STA-uAP mode selected',
  });
  for (const dir of ['proc', 'sys', 'dev/pts', 'run', 'tmp', 'runtime', 'root', 'nand-source',
    'etc/nand-pilot', 'usr/var/lib/bluetooth', 'home/galois_rwdata/local/tmp']) {
    fs.mkdirSync(path.join(root, dir), { recursive: true });
    fs.mkdirSync(path.join(boot, dir), { recursive: true });
  }
  install(path.join(root, 'bin/busybox'), path.join(boot, 'bin/busybox'));
  for (const tree of [root, boot]) {
    for (const applet of applets) {
      const name = path.join(tree, 'bin', applet);
      if (!fs.existsSync(name)) link('/bin/busybox', name);
    }
    link('busybox', path.join(tree, 'bin/sh'));
    link('busybox', path.join(tree, 'bin/ash'));
    for (const file of ['common.sh', 'kernel.sh', 'ssh-start.sh'])
      install(path.join(here, file), path.join(tree, 'usr/libexec/nand-pilot', file), '0644');
    install(path.join(path.dirname(output), 'reinvoke-status'), path.join(tree, 'usr/bin/reinvoke-status'));
    link('/usr/bin/reinvoke-status', path.join(tree, 'usr/sbin/reinvoke-status'));
    write(path.join(tree, 'etc/nand-pilot/build-id'), lib.BUILD_ID + '\n');
    link('/opt/reinvoke/lib/ld-linux-armhf.so.3', path.join(tree, 'lib/ld-linux-armhf.so.3'));
  }
  // Preserve the independently checked soft-float adbd loader family.
  for (const name of ['ld-linux.so.3', 'libdl.so.2', 'librt.so.1',
    'libpthread.so.0', 'libm.so.6', 'libc.so.6']) {
    const from = lib.rooted(original, `/lib/${name}`);
    install(from, path.join(boot, 'lib', path.basename(from)));
    link(path.basename(from), path.join(boot, 'lib', name));
  }
  fs.rmSync(path.join(boot, 'lib/ld-linux-armhf.so.3'), { force: true });
  install(path.join(original, 'sbin/adbd-root'), path.join(boot, 'sbin/adbd-root'));
  install(path.join(here, 'bootstrap.sh'), path.join(boot, 'init'));
  link('/init', path.join(boot, 'sbin/init'));
  link('/run/adb', path.join(boot, 'data'));
  write(path.join(boot, 'etc/profile'), 'export PATH=/sbin:/bin:/usr/sbin:/usr/bin\nexport HOME=/root\n');
  for (const [name, major, minor, mode] of [
    ['console', 5, 1, '0600'], ['null', 1, 3, '0666'], ['ptmx', 5, 2, '0666'],
  ])
    run('mknod', ['-m', mode, path.join(boot, 'dev', name), 'c', String(major), String(minor)]);
  normalize(root); normalize(boot);
  // Documentation and transient test fixtures are not executable build inputs.
  const sourceCode = inventory(here).filter(v => v.type === 'f' &&
    !v.path.split('/').some(part => part.startsWith('.')) &&
    /\.(?:sh|js|go|c)$/.test(v.path));
  const components = inventory(root).filter(v => (v.type === 'f' || v.type === 'l') &&
    v.path !== 'etc/reinvoke-release' && !v.path.startsWith('etc/nand-pilot/'));
  const componentManifest = JSON.stringify(components, null, 2) + '\n';
  const manifestHash = lib.sha(componentManifest);
  const release = [
    'reInvoke NAND 03 (OFFLINE CANDIDATE; normal boot acceptance unproven)',
    `build-id: ${lib.BUILD_ID}`, `source-rc12-sha256: ${pins.rc12.sha256}`,
    `source-stock-rootfs-sha256: ${pins.stock.sha256}`, `original-init-sha256: ${lib.sha(originalInit)}`,
    `runtime-components-sha256: ${manifestHash}`, `bootstrap-source-manifest-sha256: ${lib.sha(JSON.stringify(sourceCode))}`,
    `busybox-sha256: ${lib.BB_SHA256}`,
    'busybox-policy: exact RC12 binary; generic applets retained; no storage-writing startup commands',
    'storage-policy: read-only NAND source; mutable runtime/config/bonds/logs only in RAM',
    'network-policy: existing private RC12 config retained; STA/uAP; provision station each boot; no saved Wi-Fi',
    'identity-policy: no image self-hash; release and component manifest read-only bind-mounted from source',
    '', 'Original RC12 release (historical metadata; not a claim about this boot):',
    fs.readFileSync(path.join(original, 'etc/reinvoke-release'), 'utf8'),
  ].join('\n');
  for (const tree of [root, boot]) {
    write(path.join(tree, 'etc/reinvoke-release'), release, 0o444);
    write(path.join(tree, 'etc/nand-pilot/runtime-components.json'), componentManifest, 0o444);
    json(path.join(tree, 'etc/nand-pilot/module-manifest.json'), modules);
    json(path.join(tree, 'etc/nand-pilot/source-manifest.json'), sourceCode);
  }
  normalize(root); normalize(boot);
  json(path.join(output, 'runtime-elf-closure.json'), lib.elfClosure(root));
  json(path.join(output, 'bootstrap-elf-closure.json'), lib.elfClosure(boot));
  const before = inventory(original), after = inventory(root);
  const beforeMap = new Map(before.map(v => [v.path, v]));
  json(path.join(output, 'runtime-delta.json'), {
    removed: before.filter(v => !after.some(a => a.path === v.path)),
    changedOrAdded: after.filter(v => JSON.stringify(v) !== JSON.stringify(beforeMap.get(v.path))),
  });
  fs.mkdirSync(path.join(boot, 'payload'));
  const payload = path.join(boot, 'payload/runtime.cpio.gz');
  cpioPack(root, payload);
  write(path.join(boot, 'etc/nand-pilot/payload.conf'),
    `PAYLOAD_SHA256=${hashFile(payload)}\nPAYLOAD_BYTES=${fs.statSync(payload).size}\n`, 0o444);
  normalize(boot);
  json(path.join(output, 'runtime-manifest.json'), inventory(root));
  json(path.join(output, 'bootstrap-manifest.json'), inventory(boot));
  run('mksquashfs', [boot, path.join(output, 'rootfs.squashfs'), '-noappend', '-no-progress',
    '-processors', '1', '-comp', 'gzip', '-b', '131072', '-no-xattrs', '-no-recovery',
    '-all-time', '0', '-mkfs-time', '0', '-all-root', '-root-mode', '0755']);
  const extracted = path.join(output, 'verified-bootstrap');
  run('unsquashfs', ['-processors', '1', '-no-progress', '-d', extracted, path.join(output, 'rootfs.squashfs')]);
  const bootstrapVerification = lib.compareTrees(boot, extracted);
  const extractedRuntime = path.join(output, 'verified-runtime'); fs.mkdirSync(extractedRuntime);
  run('bash', ['-o', 'pipefail', '-c',
    'gzip -dc "$1" | (cd "$2" && cpio -idm --quiet --no-absolute-filenames)',
    'verify', path.join(extracted, 'payload/runtime.cpio.gz'), extractedRuntime]);
  const runtimeVerification = lib.compareTrees(root, extractedRuntime);
  const armRuntime = path.join(output, 'verified-runtime-arm');
  fs.mkdirSync(armRuntime, { mode: 0o755 });
  const packagedBB = path.join(extracted, 'bin/busybox');
  run('bash', ['-o', 'pipefail', '-c',
    '"$1" "$2" gzip -dc "$3" | (cd "$4" && "$1" "$2" cpio -idm)',
    'arm-extract', qemu, packagedBB, path.join(extracted, 'payload/runtime.cpio.gz'), armRuntime]);
  const armExpected = inventory(root), armActual = inventory(armRuntime);
  for (const tree of [armExpected, armActual]) for (const entry of tree) {
    delete entry.uid; delete entry.gid;
  }
  if (JSON.stringify(armExpected) !== JSON.stringify(armActual))
    throw new Error('actual ARM BusyBox extraction differs in content/mode/links');
  const loaderReport = run(qemu, ['-L', extracted, path.join(extracted, 'lib/ld-linux.so.3'), '--list', path.join(extracted, 'sbin/adbd-root')]);
  if (loaderReport.includes('not found')) throw new Error('adbd loader portability check failed');
  write(path.join(output, 'adbd-loader-check.txt'), loaderReport);
  const activeLoaderChecks = {};
  for (const name of ['arecord', 'dbus-daemon', 'bonefish', 'hostapd', 'iptables']) {
    const executable = path.join(extractedRuntime, 'opt/reinvoke/bin', name);
    const loader = path.join(extractedRuntime, 'opt/reinvoke/lib/ld-linux-armhf.so.3');
    const libs = name === 'hostapd' ? '/opt/reinvoke/lib/hostapd:/opt/reinvoke/lib' : '/opt/reinvoke/lib';
    const report = run(qemu, ['-L', extractedRuntime, loader, '--library-path', libs, '--list', executable]);
    if (report.includes('not found')) throw new Error(`runtime loader failed: ${name}`);
    activeLoaderChecks[name] = report;
  }
  json(path.join(output, 'runtime-loader-checks.json'), activeLoaderChecks);
  const shellCheck = run(qemu, [path.join(extracted, 'bin/busybox'), 'sh', '-c',
    'set -o pipefail; false | true; test "$?" = 1']);
  for (const tree of [root, boot, extracted, extractedRuntime, armRuntime]) {
    if (hashFile(path.join(tree, 'bin/busybox')) !== lib.BB_SHA256)
      throw new Error('packaged BusyBox differs from exact RC12 baseline');
  }
  json(path.join(output, 'validation.json'), { bootstrapVerification, runtimeVerification,
    actualARMBusyBoxFullExtraction: { entries: armActual.length, bytesModesLinksMatch: true,
      ownership: 'checked separately using fakeroot/GNU extraction' },
    elfClosure: 'every packaged ARM ELF interpreter/DT_NEEDED resolves in its declared loader family',
    adbd: 'retained binary hash verified; QEMU loader --list passed; no daemon or gadget run',
    busybox: { sha256: lib.BB_SHA256, binaryUnmodified: true, testedActualRetainedARM: true,
      genericStorageAppletsRetained: true, shellPipefail: shellCheck === '' },
    normalNANDBoot: 'UNTESTED', stockKernelRuntime: 'UNTESTED', noMountOrDeviceOperations: true });
  console.log(`Built and independently extracted ${output}`);
}
function finalize() {
  const a = path.join(output, 'build-a/rootfs.squashfs'), b = path.join(output, 'build-b/rootfs.squashfs');
  const image = fs.readFileSync(a);
  if (!image.equals(fs.readFileSync(b))) throw new Error('independent builds are not byte-identical');
  for (const key of Object.keys(pins)) verify(input(key), pins[key]);
  fs.copyFileSync(a, path.join(output, 'rootfs.squashfs'));
  const proposal = lib.proposal(image, fs.readFileSync(input('capture')), output);
  proposal.reproducibility = { buildASha256: hashFile(a), buildBSha256: hashFile(b),
    independentlyExtractedAndPackedTwice: true, byteIdentical: true, timestamps: 0 };
  let unsquashfsVersion;
  try { unsquashfsVersion = run('unsquashfs', ['-version']); }
  catch (err) {
    if (err.status !== 1 || !String(err.stdout).startsWith('unsquashfs version')) throw err;
    unsquashfsVersion = String(err.stdout);
  }
  proposal.versions = { node: process.version, mksquashfs: run('mksquashfs', ['-version']).split('\n')[0],
    cpio: run('cpio', ['--version']).split('\n')[0], gzip: run('gzip', ['--version']).split('\n')[0],
    go: run(process.env.PILOT_GO, ['version']).trim(),
    fakeroot: run('fakeroot', ['-v']).trim(),
    unsquashfs: unsquashfsVersion.split('\n')[0],
    qemu: run(path.join(archive, 'emulation/qemu-arm-static'), ['--version']).split('\n')[0] };
  for (const name of ['source-rc12-manifest.json', 'module-manifest.json', 'runtime-manifest.json',
    'bootstrap-manifest.json', 'runtime-elf-closure.json', 'bootstrap-elf-closure.json',
    'runtime-delta.json', 'validation.json', 'busybox-provenance.json', 'adbd-loader-check.txt',
    'runtime-loader-checks.json', 'kernel-compatibility.json'])
    fs.copyFileSync(path.join(output, 'build-a', name), path.join(output, name));
  json(path.join(output, 'PROPOSAL.json'), proposal);
  const manifest = inventory(output).filter(v => !v.path.startsWith('build-') && !v.path.startsWith('work/') &&
    !v.path.startsWith('source-') && v.type === 'f');
  json(path.join(output, 'ARTIFACT-MANIFEST.json'), manifest);
  console.log(JSON.stringify({ image: proposal.image, extent: proposal.extent, payload: proposal.payload, rollback: proposal.rollback }, null, 2));
}
try {
  if (mode === 'prepare') prepare();
  else if (mode === 'finalize') finalize();
  else throw new Error('usage: build.js prepare|finalize ARCHIVE OUTPUT');
} catch (err) { console.error(err.stack); process.exit(1); }
