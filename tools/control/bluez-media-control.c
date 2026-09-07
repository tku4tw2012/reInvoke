/*
 * Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 */

#include <dbus/dbus.h>

#include <ctype.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>

#define BLUEZ_BUS "org.bluez"
#define MEDIA_CONTROL_INTERFACE "org.bluez.MediaControl1"
#define METHOD_TIMEOUT_MS 3000

static int append_device_component(char *path, size_t size, const char *address) {
  size_t used = (size_t)snprintf(path, size, "/org/bluez/hci0/dev_");

  if (used >= size || strlen(address) != 17) {
    return -1;
  }
  for (size_t index = 0; index < 17; index++) {
    unsigned char value = (unsigned char)address[index];
    if (index % 3 == 2) {
      if (value != ':') {
        return -1;
      }
      value = '_';
    } else if (!isxdigit(value)) {
      return -1;
    } else {
      value = (unsigned char)toupper(value);
    }
    if (used + 1 >= size) {
      return -1;
    }
    path[used++] = (char)value;
  }
  path[used] = '\0';
  return 0;
}

int main(int argc, char **argv) {
  DBusConnection *connection;
  DBusError error;
  DBusMessage *message;
  DBusMessage *reply;
  char device_path[64];
  const char *method;

  if (argc != 3 ||
      (strcasecmp(argv[2], "play") != 0 &&
       strcasecmp(argv[2], "pause") != 0)) {
    fprintf(stderr, "Usage: bluez-media-control ADDRESS play|pause\n");
    return EXIT_FAILURE;
  }
  if (append_device_component(device_path, sizeof(device_path), argv[1]) != 0) {
    fprintf(stderr, "invalid Bluetooth address\n");
    return EXIT_FAILURE;
  }
  method = strcasecmp(argv[2], "play") == 0 ? "Play" : "Pause";

  dbus_error_init(&error);
  connection = dbus_bus_get(DBUS_BUS_SYSTEM, &error);
  if (connection == NULL) {
    fprintf(stderr, "system bus: %s\n",
            dbus_error_is_set(&error) ? error.message : "connection failed");
    dbus_error_free(&error);
    return EXIT_FAILURE;
  }

  message = dbus_message_new_method_call(BLUEZ_BUS, device_path,
                                         MEDIA_CONTROL_INTERFACE, method);
  if (message == NULL) {
    fprintf(stderr, "could not allocate D-Bus message\n");
    return EXIT_FAILURE;
  }
  reply = dbus_connection_send_with_reply_and_block(
      connection, message, METHOD_TIMEOUT_MS, &error);
  dbus_message_unref(message);
  if (reply == NULL) {
    fprintf(stderr, "%s: %s\n", method,
            dbus_error_is_set(&error) ? error.message : "call failed");
    dbus_error_free(&error);
    return EXIT_FAILURE;
  }
  dbus_message_unref(reply);
  return EXIT_SUCCESS;
}
