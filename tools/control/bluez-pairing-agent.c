/*
 * Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 */

#define _POSIX_C_SOURCE 200809L

#include <dbus/dbus.h>

#include <ctype.h>
#include <errno.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <time.h>

#define AGENT_PATH "/org/reinvoke/PairingAgent"
#define AGENT_INTERFACE "org.bluez.Agent1"
#define BLUEZ_BUS "org.bluez"
#define BLUEZ_PATH "/org/bluez"
#define BLUEZ_AGENT_MANAGER "org.bluez.AgentManager1"
#define AGENT_BUS "org.reinvoke.PairingAgent"

static char allowed_device[64];
static volatile sig_atomic_t reopen_window;

static void request_pairing_window(int signal_number) {
  (void)signal_number;
  reopen_window = 1;
}

static bool is_allowed_device(const char *device) {
  return device != NULL && strcmp(device, allowed_device) == 0;
}

static bool is_allowed_service(const char *uuid) {
  return uuid != NULL &&
         (strcasecmp(uuid, "0000110a-0000-1000-8000-00805f9b34fb") == 0 ||
          strcasecmp(uuid, "0000110b-0000-1000-8000-00805f9b34fb") == 0 ||
          strcasecmp(uuid, "0000110c-0000-1000-8000-00805f9b34fb") == 0 ||
          strcasecmp(uuid, "0000110d-0000-1000-8000-00805f9b34fb") == 0 ||
          strcasecmp(uuid, "0000110e-0000-1000-8000-00805f9b34fb") == 0);
}

static DBusHandlerResult reject(DBusConnection *connection, DBusMessage *message,
                                const char *reason) {
  DBusMessage *reply = dbus_message_new_error(
      message, "org.bluez.Error.Rejected", reason);

  if (reply == NULL) {
    return DBUS_HANDLER_RESULT_NEED_MEMORY;
  }
  dbus_connection_send(connection, reply, NULL);
  dbus_message_unref(reply);
  return DBUS_HANDLER_RESULT_HANDLED;
}

static DBusHandlerResult reply_success(DBusConnection *connection,
                                       DBusMessage *message) {
  DBusMessage *reply = dbus_message_new_method_return(message);

  if (reply == NULL) {
    return DBUS_HANDLER_RESULT_NEED_MEMORY;
  }
  dbus_connection_send(connection, reply, NULL);
  dbus_message_unref(reply);
  return DBUS_HANDLER_RESULT_HANDLED;
}

static bool get_device_path(DBusMessage *message, const char **device) {
  DBusMessageIter arguments;

  if (!dbus_message_iter_init(message, &arguments) ||
      dbus_message_iter_get_arg_type(&arguments) != DBUS_TYPE_OBJECT_PATH) {
    return false;
  }
  dbus_message_iter_get_basic(&arguments, device);
  return true;
}

static bool get_service_uuid(DBusMessage *message, const char **uuid) {
  DBusMessageIter arguments;

  if (!dbus_message_iter_init(message, &arguments) ||
      !dbus_message_iter_next(&arguments) ||
      dbus_message_iter_get_arg_type(&arguments) != DBUS_TYPE_STRING) {
    return false;
  }
  dbus_message_iter_get_basic(&arguments, uuid);
  return true;
}

static DBusHandlerResult handle_agent(DBusConnection *connection,
                                      DBusMessage *message, void *user_data) {
  const char *member = dbus_message_get_member(message);
  const char *device = NULL;

  (void)user_data;
  if (member == NULL) {
    return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
  }

  if (strcmp(member, "Release") == 0 || strcmp(member, "Cancel") == 0) {
    return reply_success(connection, message);
  }

  if (!get_device_path(message, &device)) {
    return reject(connection, message, "invalid agent request");
  }
  printf("request method=%s device=%s\n", member, device);
  fflush(stdout);
  if (!is_allowed_device(device)) {
    return reject(connection, message, "device is not allowlisted");
  }

  if (strcmp(member, "AuthorizeService") == 0) {
    const char *uuid = NULL;

    if (!get_service_uuid(message, &uuid) || !is_allowed_service(uuid)) {
      fprintf(stderr, "rejected service uuid=%s\n",
              uuid == NULL ? "<invalid>" : uuid);
      fflush(stderr);
      return reject(connection, message, "service is not allowlisted");
    }
    return reply_success(connection, message);
  }

  if (strcmp(member, "RequestAuthorization") == 0 ||
      strcmp(member, "RequestConfirmation") == 0 ||
      strcmp(member, "DisplayPinCode") == 0 ||
      strcmp(member, "DisplayPasskey") == 0) {
    return reply_success(connection, message);
  }

  return reject(connection, message, "interactive pairing is not supported");
}

static bool call_agent_manager(DBusConnection *connection, const char *method,
                               bool include_capability) {
  DBusMessage *request;
  DBusMessage *reply;
  DBusError error;
  const char *path = AGENT_PATH;
  const char *capability = "NoInputNoOutput";

  request = dbus_message_new_method_call(BLUEZ_BUS, BLUEZ_PATH,
                                         BLUEZ_AGENT_MANAGER, method);
  if (request == NULL) {
    return false;
  }
  if (!dbus_message_append_args(request, DBUS_TYPE_OBJECT_PATH, &path,
                                DBUS_TYPE_INVALID) ||
      (include_capability &&
       !dbus_message_append_args(request, DBUS_TYPE_STRING, &capability,
                                 DBUS_TYPE_INVALID))) {
    dbus_message_unref(request);
    return false;
  }

  dbus_error_init(&error);
  reply = dbus_connection_send_with_reply_and_block(connection, request, 5000,
                                                    &error);
  dbus_message_unref(request);
  if (reply == NULL) {
    fprintf(stderr, "%s failed: %s\n", method,
            dbus_error_is_set(&error) ? error.message : "out of memory");
    dbus_error_free(&error);
    return false;
  }
  dbus_message_unref(reply);
  return true;
}

static bool set_adapter_boolean(DBusConnection *connection,
                                const char *property, dbus_bool_t value) {
  DBusMessage *request;
  DBusMessage *reply;
  DBusMessageIter arguments;
  DBusMessageIter variant;
  DBusError error;
  const char *interface = "org.bluez.Adapter1";

  request = dbus_message_new_method_call(
      BLUEZ_BUS, "/org/bluez/hci0", "org.freedesktop.DBus.Properties", "Set");
  if (request == NULL) {
    return false;
  }
  dbus_message_iter_init_append(request, &arguments);
  if (!dbus_message_iter_append_basic(&arguments, DBUS_TYPE_STRING,
                                      &interface) ||
      !dbus_message_iter_append_basic(&arguments, DBUS_TYPE_STRING,
                                      &property) ||
      !dbus_message_iter_open_container(&arguments, DBUS_TYPE_VARIANT, "b",
                                        &variant) ||
      !dbus_message_iter_append_basic(&variant, DBUS_TYPE_BOOLEAN, &value) ||
      !dbus_message_iter_close_container(&arguments, &variant)) {
    dbus_message_unref(request);
    return false;
  }

  dbus_error_init(&error);
  reply = dbus_connection_send_with_reply_and_block(connection, request, 5000,
                                                    &error);
  dbus_message_unref(request);
  if (reply == NULL) {
    fprintf(stderr, "disable %s failed: %s\n", property,
            dbus_error_is_set(&error) ? error.message : "out of memory");
    dbus_error_free(&error);
    return false;
  }
  dbus_message_unref(reply);
  return true;
}

static bool close_pairing_window(DBusConnection *connection) {
  const struct timespec delay = {.tv_sec = 0, .tv_nsec = 250000000};
  bool discoverable_closed = false;
  bool pairable_closed = false;
  unsigned int attempt;

  for (attempt = 0; attempt < 4; attempt++) {
    if (!discoverable_closed) {
      discoverable_closed =
          set_adapter_boolean(connection, "Discoverable", false);
    }
    if (!pairable_closed) {
      pairable_closed = set_adapter_boolean(connection, "Pairable", false);
    }
    if (discoverable_closed && pairable_closed) {
      return true;
    }
    nanosleep(&delay, NULL);
  }
  return false;
}

static bool set_allowed_device(const char *address) {
  size_t index;

  if (strlen(address) != 17) {
    return false;
  }
  strcpy(allowed_device, "/org/bluez/hci0/dev_");
  for (index = 0; index < 17; index++) {
    const unsigned char character = (unsigned char)address[index];

    if ((index + 1) % 3 == 0) {
      if (character != ':') {
        return false;
      }
      allowed_device[20 + index] = '_';
    } else {
      if (!isxdigit(character)) {
        return false;
      }
      allowed_device[20 + index] = (char)toupper(character);
    }
  }
  allowed_device[37] = '\0';
  return true;
}

static bool run_pairing_window(DBusConnection *connection,
                               unsigned int seconds) {
  const time_t deadline = time(NULL) + seconds;
  bool dispatch_succeeded = true;

  if (seconds == 0) {
    return true;
  }
  if (!set_adapter_boolean(connection, "Pairable", true) ||
      !set_adapter_boolean(connection, "Discoverable", true)) {
    return false;
  }
  printf("pairing window=%u seconds\n", seconds);
  fflush(stdout);
  while (time(NULL) < deadline) {
    if (!dbus_connection_read_write_dispatch(connection, 250)) {
      dispatch_succeeded = false;
      break;
    }
  }
  return close_pairing_window(connection) && dispatch_succeeded;
}

static bool wait_for_powered_adapter(DBusConnection *connection) {
  const struct timespec delay = {.tv_sec = 0, .tv_nsec = 250000000};
  unsigned int attempt;

  for (attempt = 0; attempt < 40; attempt++) {
    if (set_adapter_boolean(connection, "Discoverable", false) &&
        set_adapter_boolean(connection, "Pairable", false)) {
      return true;
    }
    nanosleep(&delay, NULL);
  }
  return false;
}

int main(int argc, char **argv) {
  DBusConnection *connection;
  DBusError error;
  char *end = NULL;
  unsigned long parsed_seconds = 0;
  unsigned long parsed_reopen_seconds = 0;
  unsigned int pairing_seconds;
  unsigned int reopen_seconds;
  DBusObjectPathVTable vtable = {
      .message_function = handle_agent,
  };
  struct sigaction reopen_action = {
      .sa_handler = request_pairing_window,
  };

  if (argc < 2 || argc > 4 || !set_allowed_device(argv[1])) {
    fprintf(stderr,
            "usage: %s AA:BB:CC:DD:EE:FF [initial-seconds] [reopen-seconds]\n",
            argv[0]);
    return EXIT_FAILURE;
  }
  if (argc >= 3) {
    errno = 0;
    parsed_seconds = strtoul(argv[2], &end, 10);
    if (errno != 0 || end == argv[2] || *end != '\0' ||
        parsed_seconds > 300) {
      fprintf(stderr, "pair-seconds must be from 0 through 300\n");
      return EXIT_FAILURE;
    }
  }
  pairing_seconds = (unsigned int)parsed_seconds;
  parsed_reopen_seconds = parsed_seconds;
  if (argc == 4) {
    errno = 0;
    end = NULL;
    parsed_reopen_seconds = strtoul(argv[3], &end, 10);
    if (errno != 0 || end == argv[3] || *end != '\0' ||
        parsed_reopen_seconds > 300) {
      fprintf(stderr, "reopen-seconds must be from 0 through 300\n");
      return EXIT_FAILURE;
    }
  }
  reopen_seconds = (unsigned int)parsed_reopen_seconds;
  sigemptyset(&reopen_action.sa_mask);
  if (sigaction(SIGUSR1, &reopen_action, NULL) != 0) {
    perror("register pairing-window signal");
    return EXIT_FAILURE;
  }

  dbus_error_init(&error);
  connection = dbus_bus_get(DBUS_BUS_SYSTEM, &error);
  if (connection == NULL) {
    fprintf(stderr, "system bus: %s\n", error.message);
    dbus_error_free(&error);
    return EXIT_FAILURE;
  }
  if (dbus_bus_request_name(connection, AGENT_BUS,
                            DBUS_NAME_FLAG_DO_NOT_QUEUE, &error) !=
      DBUS_REQUEST_NAME_REPLY_PRIMARY_OWNER) {
    fprintf(stderr, "request agent bus name: %s\n",
            dbus_error_is_set(&error) ? error.message : "name is unavailable");
    dbus_error_free(&error);
    return EXIT_FAILURE;
  }
  if (!dbus_connection_register_object_path(connection, AGENT_PATH, &vtable,
                                             NULL)) {
    fprintf(stderr, "register agent path failed\n");
    return EXIT_FAILURE;
  }
  if (!call_agent_manager(connection, "RegisterAgent", true) ||
      !call_agent_manager(connection, "RequestDefaultAgent", false) ||
      !wait_for_powered_adapter(connection)) {
    return EXIT_FAILURE;
  }

  printf("pairing agent allowlist=%s\n", allowed_device);
  fflush(stdout);
  if (!run_pairing_window(connection, pairing_seconds)) {
    return EXIT_FAILURE;
  }
  while (dbus_connection_read_write_dispatch(connection, 250)) {
    if (reopen_window != 0) {
      reopen_window = 0;
      printf("pairing window requested by physical control\n");
      fflush(stdout);
      if (!run_pairing_window(connection, reopen_seconds)) {
        return EXIT_FAILURE;
      }
    }
  }
  return EXIT_FAILURE;
}
