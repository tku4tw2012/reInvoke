// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

#include <errno.h>
#include <poll.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <time.h>
#include <unistd.h>

#define BUFFER_SIZE 16384
#define DEFAULT_SOCKET "/data/misc/bluedroid/.a2dp_data"
#define CONTROL_SOCKET "/data/misc/bluedroid/.a2dp_ctrl"
#define A2DP_CTRL_CMD_CHECK_READY 1
#define A2DP_CTRL_CMD_START 2
#define A2DP_CTRL_ACK_SUCCESS 0

static int64_t monotonic_milliseconds(void) {
  struct timespec now;

  if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) {
    perror("clock_gettime");
    exit(EXIT_FAILURE);
  }
  return (int64_t)now.tv_sec * 1000 + now.tv_nsec / 1000000;
}

static int connect_abstract_socket(const char *name) {
  struct sockaddr_un address;
  size_t name_length = strlen(name);
  socklen_t address_length;
  int socket_fd;

  if (name_length + 1 > sizeof(address.sun_path)) {
    fprintf(stderr, "socket name is too long: %s\n", name);
    return -1;
  }

  socket_fd = socket(AF_UNIX, SOCK_STREAM, 0);
  if (socket_fd < 0) {
    perror("socket");
    return -1;
  }

  memset(&address, 0, sizeof(address));
  address.sun_family = AF_UNIX;
  memcpy(address.sun_path + 1, name, name_length);
  address_length =
      (socklen_t)(offsetof(struct sockaddr_un, sun_path) + 1 + name_length);

  if (connect(socket_fd, (struct sockaddr *)&address, address_length) != 0) {
    fprintf(stderr, "connect(%s): %s\n", name, strerror(errno));
    close(socket_fd);
    return -1;
  }
  return socket_fd;
}

static int exchange_control_command(int socket_fd, unsigned char command) {
  struct pollfd descriptor = {
      .fd = socket_fd,
      .events = POLLIN,
  };
  unsigned char acknowledgement = 0xff;

  if (write(socket_fd, &command, 1) != 1) {
    perror("control write");
    return -1;
  }
  if (poll(&descriptor, 1, 3000) != 1) {
    fprintf(stderr, "control command %u timed out\n", command);
    return -1;
  }
  if (read(socket_fd, &acknowledgement, 1) != 1) {
    perror("control read");
    return -1;
  }

  fprintf(stderr, "control_command=%u acknowledgement=%u\n", command,
          acknowledgement);
  return acknowledgement == A2DP_CTRL_ACK_SUCCESS ? 0 : -1;
}

int main(int argc, char **argv) {
  const char *socket_name = argc > 1 ? argv[1] : DEFAULT_SOCKET;
  int duration_seconds = argc > 2 ? atoi(argv[2]) : 5;
  const char *output_path = argc > 3 ? argv[3] : NULL;
  int start_decoder = argc > 4 && strcmp(argv[4], "--start") == 0;
  unsigned char buffer[BUFFER_SIZE];
  unsigned char first_bytes[32];
  size_t first_byte_count = 0;
  uint64_t total_bytes = 0;
  int64_t deadline;
  int socket_fd;
  int control_fd = -1;
  FILE *output = NULL;

  if (duration_seconds < 1 || duration_seconds > 300) {
    fprintf(stderr, "duration must be between 1 and 300 seconds\n");
    return EXIT_FAILURE;
  }

  if (start_decoder) {
    control_fd = connect_abstract_socket(CONTROL_SOCKET);
    if (control_fd < 0 ||
        exchange_control_command(control_fd, A2DP_CTRL_CMD_CHECK_READY) != 0 ||
        exchange_control_command(control_fd, A2DP_CTRL_CMD_START) != 0) {
      if (control_fd >= 0) {
        close(control_fd);
      }
      return EXIT_FAILURE;
    }
  }

  socket_fd = connect_abstract_socket(socket_name);
  if (socket_fd < 0) {
    if (control_fd >= 0) {
      close(control_fd);
    }
    return EXIT_FAILURE;
  }

  if (output_path != NULL) {
    output = fopen(output_path, "wb");
    if (output == NULL) {
      fprintf(stderr, "fopen(%s): %s\n", output_path, strerror(errno));
      close(socket_fd);
      if (control_fd >= 0) {
        close(control_fd);
      }
      return EXIT_FAILURE;
    }
  }

  fprintf(stderr, "connected=%s duration_seconds=%d\n", socket_name,
          duration_seconds);
  deadline = monotonic_milliseconds() + duration_seconds * 1000;

  while (monotonic_milliseconds() < deadline) {
    struct pollfd descriptor = {
        .fd = socket_fd,
        .events = POLLIN,
    };
    int64_t remaining = deadline - monotonic_milliseconds();
    int timeout = remaining > 1000 ? 1000 : (int)remaining;
    int poll_result = poll(&descriptor, 1, timeout);
    ssize_t count;

    if (poll_result < 0) {
      if (errno == EINTR) {
        continue;
      }
      perror("poll");
      break;
    }
    if (poll_result == 0) {
      continue;
    }
    if ((descriptor.revents & (POLLERR | POLLHUP | POLLNVAL)) != 0) {
      fprintf(stderr, "socket_revents=0x%x\n", descriptor.revents);
      break;
    }
    if ((descriptor.revents & POLLIN) == 0) {
      continue;
    }

    count = read(socket_fd, buffer, sizeof(buffer));
    if (count < 0) {
      if (errno == EINTR) {
        continue;
      }
      perror("read");
      break;
    }
    if (count == 0) {
      fprintf(stderr, "socket_closed=true\n");
      break;
    }

    if (first_byte_count < sizeof(first_bytes)) {
      size_t remaining_first = sizeof(first_bytes) - first_byte_count;
      size_t copy_count =
          (size_t)count < remaining_first ? (size_t)count : remaining_first;
      memcpy(first_bytes + first_byte_count, buffer, copy_count);
      first_byte_count += copy_count;
    }
    if (output != NULL &&
        fwrite(buffer, 1, (size_t)count, output) != (size_t)count) {
      perror("fwrite");
      break;
    }
    total_bytes += (uint64_t)count;
  }

  if (output != NULL && fclose(output) != 0) {
    perror("fclose");
  }
  close(socket_fd);
  if (control_fd >= 0) {
    close(control_fd);
  }

  fprintf(stderr, "total_bytes=%llu\n", (unsigned long long)total_bytes);
  fprintf(stderr, "first_bytes=");
  for (size_t index = 0; index < first_byte_count; ++index) {
    fprintf(stderr, "%02x", first_bytes[index]);
  }
  fprintf(stderr, "\n");

  return total_bytes > 0 ? EXIT_SUCCESS : 2;
}
