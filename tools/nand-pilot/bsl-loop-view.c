// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
#define _GNU_SOURCE
#include <fcntl.h>
#include <linux/loop.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <unistd.h>

#if !defined(PILOT_ROOTFS_BYTES) || PILOT_ROOTFS_BYTES <= 0 || PILOT_ROOTFS_BYTES > 0x05a00000
#error Require an image extent within the fixed main rootfs allocation
#endif

int main(void)
{
    const char *backing = "/run/reinvoke-bsl/loop-view/mtd";
    struct stat st;
    struct loop_info64 info;
    int fd = open("/run/reinvoke-bsl/loop-view/loop", O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
    if (fd < 0) {
        perror("open private read-only loop");
        return 1;
    }
    if (fstat(fd, &st) || !S_ISBLK(st.st_mode) ||
        major(st.st_rdev) != 7 || minor(st.st_rdev) != 0)
        goto fail;
    memset(&info, 0, sizeof(info));
    if (ioctl(fd, LOOP_GET_STATUS64, &info) ||
        strncmp((char *)info.lo_file_name, backing, LO_NAME_SIZE) ||
        !(info.lo_flags & LO_FLAGS_READ_ONLY) || info.lo_offset != 0)
        goto fail;
    info.lo_offset = 0x02920000;
    info.lo_sizelimit = PILOT_ROOTFS_BYTES;
    if (ioctl(fd, LOOP_SET_STATUS64, &info))
        goto fail;
    memset(&info, 0, sizeof(info));
    if (ioctl(fd, LOOP_GET_STATUS64, &info) ||
        info.lo_offset != 0x02920000 || info.lo_sizelimit != PILOT_ROOTFS_BYTES ||
        !(info.lo_flags & LO_FLAGS_READ_ONLY))
        goto fail;
    if (close(fd))
        return 1;
    puts("PRIVATE_LOOP_RO_OFFSET_AND_LENGTH_VERIFIED");
    return 0;
fail:
    fputs("private read-only loop identity or bounds failed\n", stderr);
    close(fd);
    return 1;
}
