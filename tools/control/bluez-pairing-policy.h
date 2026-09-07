/*
 * Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 */

#ifndef REINVOKE_BLUEZ_PAIRING_POLICY_H
#define REINVOKE_BLUEZ_PAIRING_POLICY_H

#include <stdbool.h>

enum reinvoke_bluetooth_state {
  REINVOKE_BLUETOOTH_STATE_UNKNOWN = -1,
  REINVOKE_BLUETOOTH_STATE_OFF = 0,
  REINVOKE_BLUETOOTH_STATE_CONNECTED = 1,
  REINVOKE_BLUETOOTH_STATE_PAIRING = 2,
};

enum reinvoke_connected_update {
  REINVOKE_CONNECTED_UNCHANGED = 0,
  REINVOKE_CONNECTED_VALUE = 1,
  REINVOKE_CONNECTED_REFRESH = 2,
};

static inline enum reinvoke_bluetooth_state
reinvoke_bluetooth_state(bool pairing_active, bool device_connected) {
  if (pairing_active) {
    return REINVOKE_BLUETOOTH_STATE_PAIRING;
  }
  if (device_connected) {
    return REINVOKE_BLUETOOTH_STATE_CONNECTED;
  }
  return REINVOKE_BLUETOOTH_STATE_OFF;
}

static inline bool reinvoke_toggle_opens_window(bool pairing_active) {
  return !pairing_active;
}

static inline enum reinvoke_connected_update
reinvoke_connected_update(bool has_value, bool invalidated) {
  if (has_value) {
    return REINVOKE_CONNECTED_VALUE;
  }
  if (invalidated) {
    return REINVOKE_CONNECTED_REFRESH;
  }
  return REINVOKE_CONNECTED_UNCHANGED;
}

#endif
