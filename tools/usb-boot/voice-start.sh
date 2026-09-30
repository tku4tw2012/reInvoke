#!/bin/busybox sh
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT

configure_voice_hosts() {
  if ! ${BB} test -e /persist/hosts &&
    ! ${BB} test -L /persist/hosts; then
    return
  fi
  if ${BB} test -s /persist/hosts &&
    ${BB} test -f /persist/hosts &&
    ! ${BB} test -L /persist/hosts; then
    if ${BB} cp /persist/hosts /etc/tmpfs/hosts.next &&
      ${BB} chmod 0644 /etc/tmpfs/hosts.next &&
      ${BB} mv -f /etc/tmpfs/hosts.next /etc/tmpfs/hosts; then
      log "hosts override copied to RAM"
    else
      ${BB} rm -f /etc/tmpfs/hosts.next
      log "hosts override unavailable; retaining baked seed"
    fi
  else
    log "hosts override invalid; retaining baked seed"
  fi
}

start_voice_endpoint() {
  if ! ${BB} test -f /etc/reinvoke-voice/voice.json; then
    if ${BB} test -e /opt/reinvoke/bin/reinvoke-voice; then
      log "voice configuration is missing; voice remains stopped"
    fi
    return
  fi
  for voice_off in voice router mcu dsp mic_capture; do
    if cmdline_has "reinvoke.${voice_off}=off"; then
      log "voice endpoint disabled by kernel command line"
      return
    fi
  done
  if ! ${BB} test -x /opt/reinvoke/bin/reinvoke-voice ||
    ! ${BB} test -x /opt/reinvoke/voice/bin/cortana ||
    ! ${BB} test -x /opt/reinvoke/voice/lib/ld-linux-armhf.so.3 ||
    ! ${BB} test -f /opt/reinvoke/voice/lib/unit-link.so ||
    ! ${BB} test -f /opt/reinvoke/voice/share/handoff-original.table; then
    log "voice endpoint bundle is incomplete; voice remains stopped"
    return
  fi
  supervise voice-endpoint /opt/reinvoke/bin/reinvoke-voice \
    --config /etc/reinvoke-voice/voice.json --bundle /opt/reinvoke/voice \
    --router 127.0.0.1:9999 --realm default
}
