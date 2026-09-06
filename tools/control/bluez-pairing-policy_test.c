/*
 * Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 */

#include "bluez-pairing-policy.h"

#include <assert.h>

int main(void) {
  assert(reinvoke_bluetooth_state(false, false) ==
         REINVOKE_BLUETOOTH_STATE_OFF);
  assert(reinvoke_bluetooth_state(false, true) ==
         REINVOKE_BLUETOOTH_STATE_CONNECTED);
  assert(reinvoke_bluetooth_state(true, false) ==
         REINVOKE_BLUETOOTH_STATE_PAIRING);
  assert(reinvoke_bluetooth_state(true, true) ==
         REINVOKE_BLUETOOTH_STATE_PAIRING);
  assert(reinvoke_toggle_opens_window(false));
  assert(!reinvoke_toggle_opens_window(true));
  assert(reinvoke_connected_update(false, false) ==
         REINVOKE_CONNECTED_UNCHANGED);
  assert(reinvoke_connected_update(true, false) == REINVOKE_CONNECTED_VALUE);
  assert(reinvoke_connected_update(false, true) == REINVOKE_CONNECTED_REFRESH);
  assert(reinvoke_connected_update(true, true) == REINVOKE_CONNECTED_VALUE);
  return 0;
}
