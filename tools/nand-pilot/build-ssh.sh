#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
set -euo pipefail
archive="$(realpath "${1:?archive required}")"
source="${archive}/sources/upstream/dropbear-2026.94.tar.bz2"
output="${2:?new output required}"
[[ ! -e "${output}" ]] || { echo "SSH output already exists" >&2; exit 1; }
printf '%s  %s\n' e098034a843699200c8c977a991fff73159735bf795d5f72ef672c41a6b1ae81 "${source}" | sha256sum -c
mkdir -m 0700 "${output}"
output="$(realpath "${output}")"
mkdir "${output}/work"
export TMPDIR="${output}/work"
tar -xjf "${source}" -C "${output}"
cd "${output}/dropbear-2026.94"
cat >localoptions.h <<'EOF'
#define DROPBEAR_SVR_PASSWORD_AUTH 0
#define DROPBEAR_SVR_PAM_AUTH 0
#define DROPBEAR_SVR_LOCALTCPFWD 0
#define DROPBEAR_SVR_REMOTETCPFWD 0
#define DROPBEAR_SVR_AGENTFWD 0
#define DROPBEAR_SFTPSERVER 0
#define DROPBEAR_X11FWD 0
#define DROPBEAR_REEXEC 0
#define DROPBEAR_DEFAULT_PATH "/sbin:/bin:/usr/sbin:/usr/bin"
EOF
nice -n 10 ./configure --host=arm-linux-gnueabihf --enable-static \
  --disable-zlib --disable-shadow --disable-utmp --disable-utmpx \
  --disable-wtmp --disable-wtmpx --disable-lastlog \
  CC=arm-linux-gnueabihf-gcc CFLAGS="-Os -ffunction-sections -fdata-sections" \
  LDFLAGS="-Wl,--gc-sections" >"${output}/configure.log" 2>&1
nice -n 10 make -j1 PROGRAMS="dropbear dropbearkey dropbearconvert" MULTI=1 \
  >"${output}/build.log" 2>&1
install -m 0755 dropbearmulti "${output}/dropbearmulti"
arm-linux-gnueabihf-strip "${output}/dropbearmulti"
install -m 0644 LICENSE "${output}/LICENSE"
for library in libtomcrypt libtommath; do
  install -m 0644 "${library}/LICENSE" "${output}/${library}-LICENSE"
done
install -m 0644 /usr/share/common-licenses/LGPL-2.1 "${output}/libc-LICENSE"
install -m 0644 /usr/share/doc/libc6-dev-armhf-cross/copyright "${output}/libc-copyright"
install -m 0644 /usr/share/common-licenses/GPL-3 "${output}/libgcc-GPL"
install -m 0644 /usr/share/doc/gcc-11-base/copyright "${output}/libgcc-copyright"
arm-linux-gnueabihf-gcc --version | head -1 >"${output}/compiler.txt"
sha256sum "${output}/dropbearmulti" >"${output}/SHA256SUMS"
"${archive}/emulation/qemu-arm-static" "${output}/dropbearmulti" dropbear -V
