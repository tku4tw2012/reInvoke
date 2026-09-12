#!/bin/busybox sh
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT

pilot_select_kernel() {
  case "$1" in
    3.8.13-yocto-standard|3.8.13-reinvoke-audio-sd8887)
      pilot_module_root="/lib/modules/$1/kernel/arch/arm/mach-berlin/modules"
      return 0
      ;;
    *)
      echo "Unsupported kernel $1; require exact stock or reviewed RC12 release; no forced modules" >&2
      return 1
      ;;
  esac
}

pilot_load_modules() {
  pilot_select_kernel "$(${BB} uname -r)" || {
    pilot_failure kernel "Unsupported actual uname -r; radio/runtime withheld"
    return 1
  }
  sd8887_module_dir="${pilot_module_root}/wlan_sd8887"
  bt_module="${pilot_module_root}/bt_sd8887/bt8xxx.ko"
  for module in "${sd8887_module_dir}/mlan.ko" \
    "${sd8887_module_dir}/sd8xxx.ko" "${bt_module}"; do
    ${BB} test -r "${module}" || {
      pilot_failure kernel "Matching module missing: ${module}"
      return 1
    }
  done
  wifi_dir=/lib/firmware/mrvl
  for firmware in sd8887_wlan_a2_p78.bin sd8887_bt_a2_new.bin \
    WlanCalData_ext-LS9AD-20160725.conf txpwrlimit_cfg_8887.bin; do
    ${BB} test -r "${wifi_dir}/${firmware}" || {
      pilot_failure kernel "Required radio firmware missing: ${firmware}"
      return 1
    }
  done
  echo /etc/hotplug/wifi-fw.sh >/proc/sys/kernel/hotplug || return 1
  ${BB} insmod "${sd8887_module_dir}/mlan.ko" || {
    pilot_failure kernel "mlan load failed; inspect vermagic and kernel symbols"
    return 1
  }
  # Preserve stock calibration/power parameters. mlan naming and STA/uAP mode
  # are RC12's provisioning contract, not an assumption about credentials.
  ${BB} insmod "${sd8887_module_dir}/sd8xxx.ko" \
    fw_name=mrvl/sd8887_wlan_a2_p78.bin \
    cal_data_cfg=mrvl/WlanCalData_ext-LS9AD-20160725.conf \
    txpwrlimit_cfg=mrvl/txpwrlimit_cfg_8887.bin \
    mac_addr=02:52:49:4e:56:01 \
    drv_mode=3 max_sta_bss=1 max_uap_bss=1 \
    sta_name=mlan uap_name=p2p fw_serial=1 cfg80211_wext=0xf \
    auto_ds=2 ps_mode=2 antenna_div=1 module_rev=22 || {
      pilot_failure wifi "sd8xxx load failed; provisioning unavailable"
      return 1
    }
  ${BB} insmod "${bt_module}" fw_name=mrvl/sd8887_bt_a2_new.bin bt_fw_serial=0 || {
    pilot_failure bluetooth "bt8xxx load failed; no forced vermagic"
    return 1
  }
  pilot_phase modules-loaded
}
