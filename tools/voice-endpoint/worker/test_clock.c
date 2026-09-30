/* Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 *
 * QEMU-only test preload: advance monotonic time on a capture record, so the
 * source timestamp and session watchdog see the same jump without a false gap.
 */
#define _GNU_SOURCE
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/syscall.h>
#include <time.h>
#include <unistd.h>

static uint64_t offset_ms;

ssize_t read(int fd, void *buffer, size_t length)
{
    ssize_t result = syscall(SYS_read, fd, buffer, length);
    if (fd > 2 && length == 1048 && result > 0) {
        int control = syscall(SYS_openat, AT_FDCWD, "/test/clock-ms", O_RDONLY | O_CLOEXEC, 0);
        if (control >= 0) {
            char value[32] = {0};
            ssize_t count = syscall(SYS_read, control, value, sizeof(value) - 1);
            if (count > 0) offset_ms = strtoull(value, NULL, 10);
            syscall(SYS_close, control);
        }
    }
    return result;
}

int clock_gettime(clockid_t clock, struct timespec *value)
{
    int result = syscall(SYS_clock_gettime, clock, value);
    if (!result && clock == CLOCK_MONOTONIC) {
        value->tv_sec += offset_ms / 1000;
        value->tv_nsec += (offset_ms % 1000) * 1000000;
        if (value->tv_nsec >= 1000000000) {
            value->tv_sec++;
            value->tv_nsec -= 1000000000;
        }
    }
    return result;
}
