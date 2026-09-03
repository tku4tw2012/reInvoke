/*
 * Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 */

#include <bluetooth/bluetooth.h>
#include <bluetooth/hci.h>
#include <bluetooth/hci_lib.h>
#include <bluetooth/mgmt.h>

#include <errno.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>

static int unpair_device(int device_id, const char *address) {
  struct {
    struct mgmt_hdr header;
    struct mgmt_cp_unpair_device parameters;
  } __attribute__((packed)) command;
  struct sockaddr_hci socket_address;
  struct timeval timeout = {.tv_sec = 2, .tv_usec = 0};
  unsigned char response[512];
  int descriptor;

  memset(&command, 0, sizeof(command));
  command.header.opcode = htobs(MGMT_OP_UNPAIR_DEVICE);
  command.header.index = htobs((uint16_t)device_id);
  command.header.len = htobs(sizeof(command.parameters));
  if (str2ba(address, &command.parameters.addr.bdaddr) != 0) {
    fprintf(stderr, "invalid Bluetooth address: %s\n", address);
    return -1;
  }
  command.parameters.addr.type = BDADDR_BREDR;
  command.parameters.disconnect = 1;

  descriptor =
      socket(AF_BLUETOOTH, SOCK_RAW | SOCK_CLOEXEC, BTPROTO_HCI);
  if (descriptor < 0) {
    return -1;
  }
  memset(&socket_address, 0, sizeof(socket_address));
  socket_address.hci_family = AF_BLUETOOTH;
  socket_address.hci_dev = HCI_DEV_NONE;
  socket_address.hci_channel = HCI_CHANNEL_CONTROL;
  if (bind(descriptor, (struct sockaddr *)&socket_address,
           sizeof(socket_address)) != 0 ||
      setsockopt(descriptor, SOL_SOCKET, SO_RCVTIMEO, &timeout,
                 sizeof(timeout)) != 0 ||
      write(descriptor, &command, sizeof(command)) != sizeof(command)) {
    close(descriptor);
    return -1;
  }

  while (true) {
    const ssize_t received = read(descriptor, response, sizeof(response));
    const struct mgmt_hdr *header = (const struct mgmt_hdr *)response;

    if (received < (ssize_t)sizeof(*header)) {
      close(descriptor);
      return -1;
    }
    if (btohs(header->opcode) == MGMT_EV_CMD_COMPLETE) {
      const struct mgmt_ev_cmd_complete *complete =
          (const struct mgmt_ev_cmd_complete *)(response + sizeof(*header));

      if (received <
              (ssize_t)(sizeof(*header) + sizeof(*complete)) ||
          btohs(complete->opcode) != MGMT_OP_UNPAIR_DEVICE) {
        continue;
      }
      close(descriptor);
      if (complete->status != MGMT_STATUS_SUCCESS &&
          complete->status != MGMT_STATUS_NOT_PAIRED) {
        errno = EIO;
        return -1;
      }
      return complete->status == MGMT_STATUS_SUCCESS ? 1 : 0;
    }
  }
}

static void print_address(const bdaddr_t *address) {
  printf("%02X:%02X:%02X:%02X:%02X:%02X", address->b[5], address->b[4],
         address->b[3], address->b[2], address->b[1], address->b[0]);
}

int main(int argc, char **argv) {
  bool reset = false;
  bool have_device_id = false;
  const char *unpair_address = NULL;
  int device_id = 0;
  struct hci_dev_info info;
  bdaddr_t any_address = {{0, 0, 0, 0, 0, 0}};
  int deleted_keys = 0;
  int descriptor;
  int index;

  for (index = 1; index < argc; index++) {
    char *end = NULL;
    long parsed;

    if (strcmp(argv[index], "--reset") == 0 && !reset) {
      reset = true;
      continue;
    }
    if (strcmp(argv[index], "--unpair") == 0 && unpair_address == NULL &&
        index + 1 < argc) {
      unpair_address = argv[++index];
      continue;
    }
    errno = 0;
    parsed = strtol(argv[index], &end, 10);
    if (have_device_id || errno != 0 || end == argv[index] || *end != '\0' ||
        parsed < 0 || parsed > UINT16_MAX) {
      fprintf(stderr,
              "usage: %s [--reset] [--unpair ADDRESS] [hci-device-id]\n",
              argv[0]);
      return EXIT_FAILURE;
    }
    device_id = (int)parsed;
    have_device_id = true;
  }
  if (argc > 5) {
    fprintf(stderr,
            "usage: %s [--reset] [--unpair ADDRESS] [hci-device-id]\n",
            argv[0]);
    return EXIT_FAILURE;
  }

  descriptor = socket(AF_BLUETOOTH, SOCK_RAW, BTPROTO_HCI);
  if (descriptor < 0) {
    perror("socket(AF_BLUETOOTH, BTPROTO_HCI)");
    return EXIT_FAILURE;
  }

  if (reset && ioctl(descriptor, HCIDEVDOWN, device_id) != 0 &&
      errno != EALREADY) {
    perror("ioctl(HCIDEVDOWN)");
    close(descriptor);
    return EXIT_FAILURE;
  }
  if (ioctl(descriptor, HCIDEVUP, device_id) != 0 && errno != EALREADY) {
    perror("ioctl(HCIDEVUP)");
    close(descriptor);
    return EXIT_FAILURE;
  }
  if (reset) {
    int hci_descriptor = hci_open_dev(device_id);

    if (hci_descriptor < 0) {
      perror("hci_open_dev");
      close(descriptor);
      return EXIT_FAILURE;
    }
    if (unpair_address != NULL) {
      const int unpaired = unpair_device(device_id, unpair_address);

      if (unpaired < 0) {
        perror("MGMT_OP_UNPAIR_DEVICE");
        close(hci_descriptor);
        close(descriptor);
        return EXIT_FAILURE;
      }
      deleted_keys += unpaired;
    }
    {
      const int controller_keys =
          hci_delete_stored_link_key(hci_descriptor, &any_address, 1, 1000);

      if (controller_keys < 0) {
        perror("hci_delete_stored_link_key");
        close(hci_descriptor);
        close(descriptor);
        return EXIT_FAILURE;
      }
      deleted_keys += controller_keys;
    }
    if (close(hci_descriptor) != 0) {
      perror("close(hci_descriptor)");
      close(descriptor);
      return EXIT_FAILURE;
    }
  }

  memset(&info, 0, sizeof(info));
  info.dev_id = (uint16_t)device_id;
  if (ioctl(descriptor, HCIGETDEVINFO, &info) != 0) {
    perror("ioctl(HCIGETDEVINFO)");
    close(descriptor);
    return EXIT_FAILURE;
  }

  printf("hci%d address=", device_id);
  print_address(&info.bdaddr);
  printf(" flags=0x%08X deleted_keys=%d\n", info.flags, deleted_keys);

  if (close(descriptor) != 0) {
    perror("close");
    return EXIT_FAILURE;
  }
  return EXIT_SUCCESS;
}
