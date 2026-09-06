/*
 * Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 */

#define _GNU_SOURCE

#include <dbus/dbus.h>

#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#include "bluez-pairing-policy.h"

#define AGENT_PATH "/org/reinvoke/PairingAgent"
#define AGENT_INTERFACE "org.bluez.Agent1"
#define BLUEZ_BUS "org.bluez"
#define BLUEZ_PATH "/org/bluez"
#define BLUEZ_AGENT_MANAGER "org.bluez.AgentManager1"
#define AGENT_BUS "org.reinvoke.PairingAgent"
#define DEFAULT_STATE_PATH "/run/reinvoke/bluetooth-state"
#define DEVICE_INTERFACE "org.bluez.Device1"
#define PROPERTIES_INTERFACE "org.freedesktop.DBus.Properties"

static char allowed_device[64];
static volatile sig_atomic_t reopen_window;
static volatile sig_atomic_t toggle_window;
static volatile sig_atomic_t stop_requested;

struct pairing_state {
  const char *path;
  bool pairing_active;
  bool device_connected;
  bool connected_refresh_required;
  struct timespec connected_refresh_after;
  bool write_failed;
  enum reinvoke_bluetooth_state published;
};

static void request_pairing_window(int signal_number) {
  (void)signal_number;
  reopen_window = 1;
}

static void toggle_pairing_window(int signal_number) {
  (void)signal_number;
  toggle_window = 1;
}

static void request_stop(int signal_number) {
  (void)signal_number;
  stop_requested = 1;
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

static const char *state_name(enum reinvoke_bluetooth_state state) {
  switch (state) {
  case REINVOKE_BLUETOOTH_STATE_OFF:
    return "off";
  case REINVOKE_BLUETOOTH_STATE_CONNECTED:
    return "connected";
  case REINVOKE_BLUETOOTH_STATE_PAIRING:
    return "pairing";
  case REINVOKE_BLUETOOTH_STATE_UNKNOWN:
    break;
  }
  return NULL;
}

static bool valid_state_path(const char *path) {
  static const char prefix[] = "/run/reinvoke/";
  const char *name;

  if (path == NULL || strncmp(path, prefix, sizeof(prefix) - 1) != 0 ||
      strlen(path) >= PATH_MAX) {
    return false;
  }
  name = path + sizeof(prefix) - 1;
  return *name != '\0' && strcmp(name, ".") != 0 && strcmp(name, "..") != 0 &&
         strchr(name, '/') == NULL;
}

static bool write_all(int descriptor, const char *content, size_t length) {
  size_t written = 0;

  while (written < length) {
    const ssize_t result =
        write(descriptor, content + written, length - written);

    if (result < 0) {
      if (errno == EINTR) {
        continue;
      }
      return false;
    }
    if (result == 0) {
      return false;
    }
    written += (size_t)result;
  }
  return true;
}

static bool publish_state(struct pairing_state *state,
                          enum reinvoke_bluetooth_state value, bool force) {
  const char *name = state_name(value);
  const size_t path_length = strlen(state->path);
  char *partial;
  char content[16];
  int content_length;
  int descriptor = -1;
  bool succeeded = false;

  if (name == NULL) {
    return false;
  }
  if (!force && state->published == value) {
    return true;
  }
  partial = malloc(path_length + sizeof(".partial"));
  if (partial == NULL) {
    fprintf(stderr, "allocate Bluetooth state path failed\n");
    return false;
  }
  memcpy(partial, state->path, path_length);
  memcpy(partial + path_length, ".partial", sizeof(".partial"));
  content_length = snprintf(content, sizeof(content), "%s\n", name);
  if (content_length < 0 || (size_t)content_length >= sizeof(content)) {
    goto out;
  }

  descriptor =
      open(partial, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC | O_NOFOLLOW,
           S_IRUSR | S_IWUSR);
  if (descriptor < 0) {
    fprintf(stderr, "open Bluetooth state: %s\n", strerror(errno));
    goto out;
  }
  if (fchmod(descriptor, S_IRUSR | S_IWUSR) != 0 ||
      !write_all(descriptor, content, (size_t)content_length) ||
      fsync(descriptor) != 0) {
    fprintf(stderr, "write Bluetooth state: %s\n", strerror(errno));
    goto out;
  }
  if (close(descriptor) != 0) {
    descriptor = -1;
    fprintf(stderr, "close Bluetooth state: %s\n", strerror(errno));
    goto out;
  }
  descriptor = -1;
  if (rename(partial, state->path) != 0) {
    fprintf(stderr, "publish Bluetooth state: %s\n", strerror(errno));
    goto out;
  }
  state->published = value;
  printf("Bluetooth state=%s\n", name);
  fflush(stdout);
  succeeded = true;

out:
  if (descriptor >= 0) {
    close(descriptor);
  }
  if (!succeeded) {
    unlink(partial);
  }
  free(partial);
  return succeeded;
}

static bool publish_authoritative_state(struct pairing_state *state) {
  return publish_state(
      state,
      reinvoke_bluetooth_state(state->pairing_active, state->device_connected),
      false);
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

  if (strcmp(member, "Release") == 0) {
    stop_requested = 1;
    return reply_success(connection, message);
  }
  if (strcmp(member, "Cancel") == 0) {
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

static bool get_device_connected(DBusConnection *connection, bool *connected) {
  DBusMessage *request;
  DBusMessage *reply;
  DBusMessageIter value;
  DBusMessageIter variant;
  DBusError error;
  const char *interface = DEVICE_INTERFACE;
  const char *property = "Connected";
  dbus_bool_t result;

  request = dbus_message_new_method_call(
      BLUEZ_BUS, allowed_device, PROPERTIES_INTERFACE, "Get");
  if (request == NULL) {
    fprintf(stderr, "create Device1.Connected request failed\n");
    return false;
  }
  if (!dbus_message_append_args(request, DBUS_TYPE_STRING, &interface,
                                DBUS_TYPE_STRING, &property,
                                DBUS_TYPE_INVALID)) {
    dbus_message_unref(request);
    fprintf(stderr, "encode Device1.Connected request failed\n");
    return false;
  }
  dbus_error_init(&error);
  reply = dbus_connection_send_with_reply_and_block(connection, request, 5000,
                                                     &error);
  dbus_message_unref(request);
  if (reply == NULL) {
    fprintf(stderr, "read Device1.Connected: %s\n",
            dbus_error_is_set(&error) ? error.message : "out of memory");
    dbus_error_free(&error);
    return false;
  }
  if (!dbus_message_iter_init(reply, &value) ||
      dbus_message_iter_get_arg_type(&value) != DBUS_TYPE_VARIANT) {
    fprintf(stderr, "Device1.Connected reply is invalid\n");
    dbus_message_unref(reply);
    return false;
  }
  dbus_message_iter_recurse(&value, &variant);
  if (dbus_message_iter_get_arg_type(&variant) != DBUS_TYPE_BOOLEAN) {
    fprintf(stderr, "Device1.Connected is not Boolean\n");
    dbus_message_unref(reply);
    return false;
  }
  dbus_message_iter_get_basic(&variant, &result);
  *connected = result != FALSE;
  dbus_message_unref(reply);
  return true;
}

static bool parse_connected_change(
    DBusMessage *message, enum reinvoke_connected_update *update,
    bool *connected) {
  DBusMessageIter arguments;
  DBusMessageIter changed;
  const char *interface;
  bool has_value = false;
  bool invalidated = false;

  *update = REINVOKE_CONNECTED_UNCHANGED;
  if (!dbus_message_iter_init(message, &arguments) ||
      dbus_message_iter_get_arg_type(&arguments) != DBUS_TYPE_STRING) {
    return false;
  }
  dbus_message_iter_get_basic(&arguments, &interface);
  if (strcmp(interface, DEVICE_INTERFACE) != 0) {
    return true;
  }
  if (!dbus_message_iter_next(&arguments) ||
      dbus_message_iter_get_arg_type(&arguments) != DBUS_TYPE_ARRAY) {
    return false;
  }
  dbus_message_iter_recurse(&arguments, &changed);
  while (dbus_message_iter_get_arg_type(&changed) ==
         DBUS_TYPE_DICT_ENTRY) {
    DBusMessageIter entry;
    DBusMessageIter variant;
    const char *property;

    dbus_message_iter_recurse(&changed, &entry);
    if (dbus_message_iter_get_arg_type(&entry) != DBUS_TYPE_STRING) {
      return false;
    }
    dbus_message_iter_get_basic(&entry, &property);
    if (!dbus_message_iter_next(&entry) ||
        dbus_message_iter_get_arg_type(&entry) != DBUS_TYPE_VARIANT) {
      return false;
    }
    if (strcmp(property, "Connected") == 0) {
      dbus_bool_t value;

      if (has_value) {
        return false;
      }
      dbus_message_iter_recurse(&entry, &variant);
      if (dbus_message_iter_get_arg_type(&variant) != DBUS_TYPE_BOOLEAN) {
        return false;
      }
      dbus_message_iter_get_basic(&variant, &value);
      *connected = value != FALSE;
      has_value = true;
    }
    dbus_message_iter_next(&changed);
  }

  if (!dbus_message_iter_next(&arguments) ||
      dbus_message_iter_get_arg_type(&arguments) != DBUS_TYPE_ARRAY) {
    return false;
  }
  dbus_message_iter_recurse(&arguments, &changed);
  while (dbus_message_iter_get_arg_type(&changed) == DBUS_TYPE_STRING) {
    const char *property;

    dbus_message_iter_get_basic(&changed, &property);
    if (strcmp(property, "Connected") == 0) {
      invalidated = true;
    }
    dbus_message_iter_next(&changed);
  }
  *update = reinvoke_connected_update(has_value, invalidated);
  return true;
}

static DBusHandlerResult handle_bluez_property(DBusConnection *connection,
                                               DBusMessage *message,
                                               void *user_data) {
  struct pairing_state *state = user_data;
  const char *path = dbus_message_get_path(message);
  bool connected;
  enum reinvoke_connected_update update;

  (void)connection;
  if (!dbus_message_is_signal(message, PROPERTIES_INTERFACE,
                              "PropertiesChanged") ||
      path == NULL || strcmp(path, allowed_device) != 0) {
    return DBUS_HANDLER_RESULT_NOT_YET_HANDLED;
  }
  if (!parse_connected_change(message, &update, &connected)) {
    fprintf(stderr, "invalid Device1 PropertiesChanged signal\n");
    return DBUS_HANDLER_RESULT_HANDLED;
  }
  if (update == REINVOKE_CONNECTED_REFRESH) {
    state->connected_refresh_required = true;
    state->connected_refresh_after = (struct timespec){0};
  } else if (update == REINVOKE_CONNECTED_VALUE) {
    state->connected_refresh_required = false;
    if (state->device_connected != connected) {
      state->device_connected = connected;
      if (!publish_authoritative_state(state)) {
        state->write_failed = true;
      }
    }
  }
  return DBUS_HANDLER_RESULT_HANDLED;
}

static bool watch_allowed_device(DBusConnection *connection,
                                 struct pairing_state *state) {
  DBusError error;
  char match[256];
  int length;

  if (!dbus_connection_add_filter(connection, handle_bluez_property, state,
                                  NULL)) {
    fprintf(stderr, "register Device1 property filter failed\n");
    return false;
  }
  length = snprintf(
      match, sizeof(match),
      "type='signal',sender='" BLUEZ_BUS "',path='%s',interface='"
      PROPERTIES_INTERFACE "',member='PropertiesChanged',arg0='"
      DEVICE_INTERFACE "'",
      allowed_device);
  if (length < 0 || (size_t)length >= sizeof(match)) {
    dbus_connection_remove_filter(connection, handle_bluez_property, state);
    return false;
  }
  dbus_error_init(&error);
  dbus_bus_add_match(connection, match, &error);
  dbus_connection_flush(connection);
  if (dbus_error_is_set(&error)) {
    fprintf(stderr, "watch Device1.Connected: %s\n", error.message);
    dbus_error_free(&error);
    dbus_connection_remove_filter(connection, handle_bluez_property, state);
    return false;
  }
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

static bool open_pairing_window(DBusConnection *connection,
                                struct pairing_state *state,
                                unsigned int seconds,
                                struct timespec *deadline) {
  if (seconds == 0) {
    return true;
  }
  if (!set_adapter_boolean(connection, "Pairable", true) ||
      !set_adapter_boolean(connection, "Discoverable", true)) {
    close_pairing_window(connection);
    return false;
  }
  state->pairing_active = true;
  if (clock_gettime(CLOCK_MONOTONIC, deadline) != 0) {
    state->pairing_active = false;
    close_pairing_window(connection);
    return false;
  }
  deadline->tv_sec += (time_t)seconds;
  printf("pairing window=%u seconds\n", seconds);
  fflush(stdout);
  return publish_authoritative_state(state);
}

static bool monotonic_deadline_reached(const struct timespec *deadline) {
  struct timespec current;

  if (clock_gettime(CLOCK_MONOTONIC, &current) != 0) {
    return true;
  }
  return current.tv_sec > deadline->tv_sec ||
         (current.tv_sec == deadline->tv_sec &&
          current.tv_nsec >= deadline->tv_nsec);
}

static bool end_pairing_window(DBusConnection *connection,
                               struct pairing_state *state) {
  if (!state->pairing_active) {
    return true;
  }
  if (!close_pairing_window(connection)) {
    return false;
  }
  state->pairing_active = false;
  return publish_authoritative_state(state);
}

static bool wait_for_powered_adapter(DBusConnection *connection) {
  const struct timespec delay = {.tv_sec = 0, .tv_nsec = 250000000};
  unsigned int attempt;

  for (attempt = 0; attempt < 40; attempt++) {
    if (stop_requested) {
      return false;
    }
    if (set_adapter_boolean(connection, "Discoverable", false) &&
        set_adapter_boolean(connection, "Pairable", false)) {
      return true;
    }
    nanosleep(&delay, NULL);
  }
  return false;
}

int main(int argc, char **argv) {
  DBusConnection *connection = NULL;
  DBusError error;
  char *end = NULL;
  unsigned long parsed_seconds = 0;
  unsigned long parsed_reopen_seconds = 0;
  unsigned int pairing_seconds;
  unsigned int reopen_seconds;
  struct timespec deadline = {0};
  bool property_watch_registered = false;
  bool agent_registered = false;
  bool initial_connected;
  int exit_status = EXIT_FAILURE;
  struct pairing_state state = {
      .path = DEFAULT_STATE_PATH,
      .published = REINVOKE_BLUETOOTH_STATE_UNKNOWN,
  };
  DBusObjectPathVTable vtable = {
      .message_function = handle_agent,
  };
  struct sigaction reopen_action = {
      .sa_handler = request_pairing_window,
  };
  struct sigaction toggle_action = {
      .sa_handler = toggle_pairing_window,
  };
  struct sigaction stop_action = {
      .sa_handler = request_stop,
  };

  if (argc < 2 || argc > 5 || !set_allowed_device(argv[1])) {
    fprintf(stderr,
            "usage: %s AA:BB:CC:DD:EE:FF [initial-seconds] [reopen-seconds] "
            "[state-path]\n",
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
  if (argc >= 4) {
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
  if (argc == 5) {
    state.path = argv[4];
  }
  if (!valid_state_path(state.path)) {
    fprintf(stderr, "state-path must be a direct child of /run/reinvoke\n");
    return EXIT_FAILURE;
  }
  sigemptyset(&reopen_action.sa_mask);
  sigemptyset(&toggle_action.sa_mask);
  sigemptyset(&stop_action.sa_mask);
  reopen_action.sa_flags = SA_RESTART;
  toggle_action.sa_flags = SA_RESTART;
  stop_action.sa_flags = SA_RESTART;
  if (sigaction(SIGUSR1, &reopen_action, NULL) != 0 ||
      sigaction(SIGUSR2, &toggle_action, NULL) != 0 ||
      sigaction(SIGTERM, &stop_action, NULL) != 0 ||
      sigaction(SIGINT, &stop_action, NULL) != 0) {
    perror("register pairing-window signal");
    return EXIT_FAILURE;
  }
  if (!publish_state(&state, REINVOKE_BLUETOOTH_STATE_OFF, true)) {
    return EXIT_FAILURE;
  }

  dbus_error_init(&error);
  connection = dbus_bus_get(DBUS_BUS_SYSTEM, &error);
  if (connection == NULL) {
    fprintf(stderr, "system bus: %s\n", error.message);
    dbus_error_free(&error);
    goto cleanup;
  }
  dbus_connection_set_exit_on_disconnect(connection, FALSE);
  if (dbus_bus_request_name(connection, AGENT_BUS,
                            DBUS_NAME_FLAG_DO_NOT_QUEUE, &error) !=
      DBUS_REQUEST_NAME_REPLY_PRIMARY_OWNER) {
    fprintf(stderr, "request agent bus name: %s\n",
            dbus_error_is_set(&error) ? error.message : "name is unavailable");
    dbus_error_free(&error);
    goto cleanup;
  }
  if (!dbus_connection_register_object_path(connection, AGENT_PATH, &vtable,
                                             NULL)) {
    fprintf(stderr, "register agent path failed\n");
    goto cleanup;
  }
  if (!call_agent_manager(connection, "RegisterAgent", true)) {
    goto cleanup;
  }
  agent_registered = true;
  if (!call_agent_manager(connection, "RequestDefaultAgent", false) ||
      !wait_for_powered_adapter(connection)) {
    goto cleanup;
  }
  if (!watch_allowed_device(connection, &state)) {
    goto cleanup;
  }
  property_watch_registered = true;
  if (get_device_connected(connection, &initial_connected)) {
    state.device_connected = initial_connected;
    if (!publish_authoritative_state(&state)) {
      goto cleanup;
    }
  } else {
    fprintf(stderr,
            "retaining safe initial Bluetooth state until Device1 reports\n");
  }

  printf("pairing agent allowlist=%s\n", allowed_device);
  fflush(stdout);
  if (!open_pairing_window(connection, &state, pairing_seconds, &deadline)) {
    goto cleanup;
  }
  while (!stop_requested) {
    if (state.write_failed ||
        !dbus_connection_read_write_dispatch(connection, 250)) {
      break;
    }
    if (state.write_failed) {
      break;
    }
    if (state.connected_refresh_required &&
        monotonic_deadline_reached(&state.connected_refresh_after)) {
      bool refreshed_connected;

      if (!get_device_connected(connection, &refreshed_connected)) {
        fprintf(stderr,
                "retaining last known Device1.Connected after refresh failure\n");
        if (clock_gettime(CLOCK_MONOTONIC,
                          &state.connected_refresh_after) == 0) {
          state.connected_refresh_after.tv_sec++;
        }
      } else {
        state.connected_refresh_required = false;
        if (state.device_connected != refreshed_connected) {
          state.device_connected = refreshed_connected;
          if (!publish_authoritative_state(&state)) {
            break;
          }
        }
      }
    }
    if (toggle_window != 0) {
      toggle_window = 0;
      if (reinvoke_toggle_opens_window(state.pairing_active)) {
        printf("pairing window toggled open by physical control\n");
        fflush(stdout);
        if (!open_pairing_window(connection, &state, reopen_seconds,
                                 &deadline)) {
          break;
        }
      } else {
        printf("pairing window cancelled by physical control\n");
        fflush(stdout);
        if (!end_pairing_window(connection, &state)) {
          break;
        }
      }
    }
    if (reopen_window != 0) {
      reopen_window = 0;
      printf("pairing window requested by physical control\n");
      fflush(stdout);
      if (!open_pairing_window(connection, &state, reopen_seconds, &deadline)) {
        break;
      }
    }
    if (state.pairing_active && monotonic_deadline_reached(&deadline) &&
        !end_pairing_window(connection, &state)) {
      break;
    }
  }
  if (stop_requested && !state.write_failed) {
    exit_status = EXIT_SUCCESS;
  }

cleanup:
  if (connection != NULL && state.pairing_active) {
    close_pairing_window(connection);
  }
  state.pairing_active = false;
  state.device_connected = false;
  if (!publish_state(&state, REINVOKE_BLUETOOTH_STATE_OFF, true)) {
    exit_status = EXIT_FAILURE;
  }
  if (connection != NULL && property_watch_registered) {
    dbus_connection_remove_filter(connection, handle_bluez_property, &state);
  }
  if (connection != NULL && agent_registered) {
    call_agent_manager(connection, "UnregisterAgent", false);
  }
  if (connection != NULL) {
    dbus_connection_unref(connection);
  }
  return exit_status;
}
