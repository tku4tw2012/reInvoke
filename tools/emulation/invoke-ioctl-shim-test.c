/*
 * Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 */

#define _POSIX_C_SOURCE 200809L
#include <linux/spi/spidev.h>
#include <sys/ioctl.h>

#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define I2C_RDWR 0x0707
#define I2C_M_RD 0x0001

struct test_i2c_msg {
    uint16_t addr;
    uint16_t flags;
    uint16_t len;
    uint8_t *buf;
};

struct test_i2c_rdwr_ioctl_data {
    struct test_i2c_msg *msgs;
    uint32_t nmsgs;
};

static int forward_count;

FILE *invoke_test_open_record_log(const char *path)
{
    if (strcmp(path, "reject") == 0) {
        errno = EPERM;
        return NULL;
    }
    return fopen(path, "a");
}

int invoke_test_approve_fd(int fd, unsigned long request)
{
    if (request == I2C_RDWR)
        return fd == 42;
    return fd == 43;
}

int invoke_test_forward_ioctl(int fd, unsigned long request, void *arg)
{
    (void)fd;
    forward_count++;

    if (request == I2C_RDWR) {
        struct test_i2c_rdwr_ioctl_data *data = arg;

        if (!data) {
            errno = EFAULT;
            return -1;
        }
        if (data->nmsgs != 2 || data->msgs[0].len != 2) {
            errno = EPROTO;
            return -1;
        }
        if (data->msgs[0].buf[0] == 0xcc) {
            data->msgs[1].buf[0] = 0xbe;
            data->msgs[1].buf[1] = 0xef;
            return 1;
        }
        if (data->msgs[0].buf[0] != 0xaa ||
            data->msgs[0].buf[1] != 0xbb) {
            errno = EPROTO;
            return -1;
        }
        data->msgs[1].buf[0] = 0xde;
        data->msgs[1].buf[1] = 0xad;
        return 2;
    }

    if (_IOC_TYPE(request) == SPI_IOC_MAGIC && _IOC_NR(request) == 0) {
        struct spi_ioc_transfer *transfers = arg;
        uint8_t *rx = (uint8_t *)(uintptr_t)transfers[0].rx_buf;

        rx[0] = 0x10;
        rx[1] = 0x20;
        rx[2] = 0x30;
        rx[3] = 0x40;
        return 6;
    }

    if (request == 0x1234)
        return 77;

    errno = ENOTTY;
    return -1;
}

static int test_record_mode(const char *log_path)
{
    uint8_t i2c_write[] = {0xaa, 0xbb};
    uint8_t i2c_read[2] = {0};
    uint8_t partial_write[] = {0xcc, 0xdd};
    uint8_t partial_read[2] = {0};
    struct test_i2c_msg messages[] = {
        {.addr = 0x20, .flags = 0, .len = sizeof(i2c_write),
         .buf = i2c_write},
        {.addr = 0x20, .flags = I2C_M_RD, .len = sizeof(i2c_read),
         .buf = i2c_read},
    };
    struct test_i2c_rdwr_ioctl_data i2c_data = {
        .msgs = messages,
        .nmsgs = 2,
    };
    struct test_i2c_msg partial_messages[] = {
        {.addr = 0x20, .flags = 0, .len = sizeof(partial_write),
         .buf = partial_write},
        {.addr = 0x20, .flags = I2C_M_RD, .len = sizeof(partial_read),
         .buf = partial_read},
    };
    struct test_i2c_rdwr_ioctl_data partial_data = {
        .msgs = partial_messages,
        .nmsgs = 2,
    };
    uint8_t spi_tx[] = {1, 2, 3, 4};
    uint8_t spi_rx[4] = {0};
    struct spi_ioc_transfer transfers[] = {
        {
            .tx_buf = (uintptr_t)spi_tx,
            .rx_buf = (uintptr_t)spi_rx,
            .len = sizeof(spi_tx),
            .speed_hz = 1000000,
            .bits_per_word = 8,
        },
        {
            .len = 2,
            .speed_hz = 500000,
            .bits_per_word = 8,
            .cs_change = 1,
        },
    };

    setenv("INVOKE_IOCTL_MODE", "record", 1);
    setenv("INVOKE_IOCTL_LOG", log_path, 1);

    if (ioctl(42, I2C_RDWR, &i2c_data) != 2 ||
        memcmp(i2c_read, (uint8_t[]){0xde, 0xad}, 2) != 0)
        return 1;
    if (ioctl(42, I2C_RDWR, &partial_data) != 1 ||
        memcmp(partial_read, (uint8_t[]){0xbe, 0xef}, 2) != 0)
        return 1;
    errno = 0;
    if (ioctl(99, I2C_RDWR, &i2c_data) != -1 || errno != EPERM)
        return 1;
    if (ioctl(43, SPI_IOC_MESSAGE(2), transfers) != 6 ||
        memcmp(spi_rx, (uint8_t[]){0x10, 0x20, 0x30, 0x40}, 4) != 0)
        return 1;
    if (ioctl(44, 0x1234, NULL) != 77)
        return 1;
    errno = 0;
    if (ioctl(42, I2C_RDWR, NULL) != -1 || errno != EFAULT)
        return 1;
    return 0;
}

static int test_rejected_log(void)
{
    uint8_t write[] = {0xaa, 0xbb};
    struct test_i2c_msg message = {
        .addr = 0x20, .flags = 0, .len = sizeof(write), .buf = write
    };
    struct test_i2c_rdwr_ioctl_data data = {
        .msgs = &message, .nmsgs = 1
    };

    setenv("INVOKE_IOCTL_MODE", "record", 1);
    setenv("INVOKE_IOCTL_LOG", "reject", 1);
    errno = 0;
    if (ioctl(42, I2C_RDWR, &data) != -1 || errno != EPERM)
        return 1;
    return forward_count == 0 ? 0 : 1;
}

static int test_emulation_mode(const char *log_path)
{
    uint8_t write[] = {0x01, 0x5a};
    uint8_t pointer[] = {0x01};
    uint8_t read[1] = {0};
    struct test_i2c_msg write_message = {
        .addr = 0x20, .flags = 0, .len = sizeof(write), .buf = write
    };
    struct test_i2c_msg read_messages[] = {
        {.addr = 0x20, .flags = 0, .len = sizeof(pointer), .buf = pointer},
        {.addr = 0x20, .flags = I2C_M_RD, .len = sizeof(read), .buf = read},
    };
    struct test_i2c_rdwr_ioctl_data write_data = {
        .msgs = &write_message, .nmsgs = 1
    };
    struct test_i2c_rdwr_ioctl_data read_data = {
        .msgs = read_messages, .nmsgs = 2
    };

    unsetenv("INVOKE_IOCTL_MODE");
    setenv("INVOKE_IOCTL_LOG", log_path, 1);

    if (ioctl(42, I2C_RDWR, &write_data) != 1)
        return 1;
    if (ioctl(42, I2C_RDWR, &read_data) != 2 || read[0] != 0x5a)
        return 1;
    return 0;
}

int main(int argc, char **argv)
{
    if (argc != 3) {
        fprintf(stderr, "usage: %s MODE LOG_PATH\n", argv[0]);
        return 2;
    }
    if (strcmp(argv[1], "record") == 0)
        return test_record_mode(argv[2]);
    if (strcmp(argv[1], "emulate") == 0)
        return test_emulation_mode(argv[2]);
    if (strcmp(argv[1], "reject") == 0)
        return test_rejected_log();
    return 2;
}
