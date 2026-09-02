/*
 * Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 *
 * Guest-side ioctl emulation for the Harman Kardon Invoke userland.
 *
 * qemu-user does not implement the ALSA and HCI management ioctls used by the
 * firmware, and Linux i2c-stub cannot serve mcu-interface raw I2C_RDWR
 * messages. Preloading this ARM library handles those boundaries without
 * exposing a real host I2C bus or Bluetooth controller.
 */

#define _GNU_SOURCE
#include <sound/asound.h>
#include <sys/ioctl.h>
#include <sys/syscall.h>

#include <errno.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define I2C_RETRIES 0x0701
#define I2C_TIMEOUT 0x0702
#define I2C_SLAVE 0x0703
#define I2C_FUNCS 0x0705
#define I2C_SLAVE_FORCE 0x0706
#define I2C_RDWR 0x0707

#define I2C_M_RD 0x0001
#define I2C_FUNC_I2C 0x00000001
#define I2C_FUNC_SMBUS_EMUL 0x0eff0008

#define HCIDEVUP _IOW('H', 201, int)
#define HCIGETDEVLIST _IOR('H', 210, int)
#define HCIGETDEVINFO _IOR('H', 211, int)

#define HCI_DEVICE_ID 0
#define HCI_DEVICE_UP 0x00000001

#define CONTROL_COUNT 6
#define REGISTER_COUNT 128

struct hci_dev_req {
    uint16_t dev_id;
    uint32_t dev_opt;
};

struct hci_dev_list_req {
    uint16_t dev_num;
    struct hci_dev_req dev_req[];
};

struct hci_dev_stats {
    uint32_t err_rx;
    uint32_t err_tx;
    uint32_t cmd_tx;
    uint32_t evt_rx;
    uint32_t acl_tx;
    uint32_t acl_rx;
    uint32_t sco_tx;
    uint32_t sco_rx;
    uint32_t byte_rx;
    uint32_t byte_tx;
};

struct hci_dev_info {
    uint16_t dev_id;
    char name[8];
    uint8_t bdaddr[6];
    uint32_t flags;
    uint8_t type;
    uint8_t features[8];
    uint32_t pkt_type;
    uint32_t link_policy;
    uint32_t link_mode;
    uint16_t acl_mtu;
    uint16_t acl_pkts;
    uint16_t sco_mtu;
    uint16_t sco_pkts;
    struct hci_dev_stats stat;
};

struct i2c_msg {
    uint16_t addr;
    uint16_t flags;
    uint16_t len;
    uint8_t *buf;
};

struct i2c_rdwr_ioctl_data {
    struct i2c_msg *msgs;
    uint32_t nmsgs;
};

static FILE *log_file;
static int shim_ready;
static uint8_t registers[REGISTER_COUNT][REGISTER_COUNT];
static uint8_t last_register[REGISTER_COUNT];
static const char *const control_names[CONTROL_COUNT] = {
    "music", "call", "voice", "system", "timer", "mic"
};
static long control_values[CONTROL_COUNT] = {255, 255, 255, 255, 255, 255};

static int forward_ioctl(int fd, unsigned long request, void *arg)
{
    /*
     * dlsym(RTLD_NEXT) from the host toolchain requires GLIBC_2.34. A direct
     * syscall keeps this library compatible with the firmware's glibc 2.23.
     */
    return syscall(SYS_ioctl, fd, request, arg);
}

static void initialize_shim(void)
{
    const char *path;

    if (shim_ready)
        return;

    shim_ready = 1;
    path = getenv("INVOKE_IOCTL_LOG");
    log_file = path ? fopen(path, "a") : stderr;
    if (!log_file)
        log_file = stderr;
    setvbuf(log_file, NULL, _IOLBF, 0);
}

static unsigned control_index(const struct snd_ctl_elem_id *id)
{
    unsigned index;

    if (id->numid >= 1 && id->numid <= CONTROL_COUNT)
        return id->numid - 1;

    for (index = 0; index < CONTROL_COUNT; index++)
        if (strcmp((const char *)id->name, control_names[index]) == 0)
            return index;

    return CONTROL_COUNT;
}

static void set_control_id(struct snd_ctl_elem_id *id, unsigned index)
{
    memset(id, 0, sizeof(*id));
    id->numid = index + 1;
    id->iface = SNDRV_CTL_ELEM_IFACE_MIXER;
    strncpy((char *)id->name, control_names[index], sizeof(id->name) - 1);
}

static void log_i2c_message(const struct i2c_msg *message)
{
    unsigned index;
    int is_read = (message->flags & I2C_M_RD) != 0;

    fprintf(log_file, "I2C addr=0x%02x %s len=%u data=", message->addr,
            is_read ? "READ " : "WRITE", message->len);
    for (index = 0; index < message->len; index++)
        fprintf(log_file, "%02x", message->buf[index]);
    fprintf(log_file, "\n");
}

static void serve_i2c_message(struct i2c_msg *message)
{
    uint8_t address = message->addr & 0x7f;
    unsigned index;

    if (message->flags & I2C_M_RD) {
        for (index = 0; index < message->len; index++)
            message->buf[index] =
                registers[address][(last_register[address] + index) & 0x7f];
        return;
    }

    if (message->len == 1) {
        last_register[address] = message->buf[0];
    } else if (message->len >= 2) {
        last_register[address] = message->buf[0];
        for (index = 1; index < message->len; index++)
            registers[address][(message->buf[0] + index - 1) & 0x7f] =
                message->buf[index];
    }
}

static int serve_hci_device_list(struct hci_dev_list_req *list)
{
    if (!list) {
        errno = EINVAL;
        return -1;
    }

    if (list->dev_num > 0) {
        list->dev_req[0].dev_id = HCI_DEVICE_ID;
        list->dev_req[0].dev_opt = HCI_DEVICE_UP;
    }
    list->dev_num = 1;
    fprintf(log_file, "HCI device-list hci%d\n", HCI_DEVICE_ID);
    return 0;
}

static int serve_hci_device_info(struct hci_dev_info *info)
{
    if (!info) {
        errno = EINVAL;
        return -1;
    }

    if (info->dev_id != HCI_DEVICE_ID) {
        errno = ENODEV;
        return -1;
    }

    memset(info, 0, sizeof(*info));
    info->dev_id = HCI_DEVICE_ID;
    strcpy(info->name, "hci0");
    info->flags = HCI_DEVICE_UP;
    fprintf(log_file, "HCI device-info hci%d\n", HCI_DEVICE_ID);
    return 0;
}

int ioctl(int fd, unsigned long request, ...)
{
    va_list arguments;
    void *arg;

    initialize_shim();
    va_start(arguments, request);
    arg = va_arg(arguments, void *);
    va_end(arguments);

    switch (request) {
    case HCIGETDEVLIST:
        return serve_hci_device_list(arg);

    case HCIGETDEVINFO:
        return serve_hci_device_info(arg);

    case HCIDEVUP:
        if ((unsigned)(uintptr_t)arg != HCI_DEVICE_ID) {
            errno = ENODEV;
            return -1;
        }
        fprintf(log_file, "HCI device-up hci%d\n", HCI_DEVICE_ID);
        return 0;

    case I2C_FUNCS:
        *(unsigned long *)arg = I2C_FUNC_I2C | I2C_FUNC_SMBUS_EMUL;
        return 0;

    case I2C_SLAVE:
    case I2C_SLAVE_FORCE:
        fprintf(log_file, "I2C set-slave 0x%02x\n",
                (unsigned)(uintptr_t)arg);
        return 0;

    case I2C_RETRIES:
    case I2C_TIMEOUT:
        return 0;

    case I2C_RDWR: {
        struct i2c_rdwr_ioctl_data *data = arg;
        unsigned index;

        if (!data || !data->msgs) {
            errno = EINVAL;
            return -1;
        }

        for (index = 0; index < data->nmsgs; index++) {
            serve_i2c_message(&data->msgs[index]);
            log_i2c_message(&data->msgs[index]);
        }
        return (int)data->nmsgs;
    }

    case SNDRV_CTL_IOCTL_PVERSION:
        *(int *)arg = (2 << 16) | 7;
        return 0;

    case SNDRV_CTL_IOCTL_CARD_INFO: {
        struct snd_ctl_card_info *info = arg;

        memset(info, 0, sizeof(*info));
        info->card = 0;
        strcpy((char *)info->id, "Invoke");
        strcpy((char *)info->driver, "Berlin");
        strcpy((char *)info->name, "Invoke DSP");
        strcpy((char *)info->longname, "Invoke DSP");
        strcpy((char *)info->mixername, "Invoke");
        return 0;
    }

    case SNDRV_CTL_IOCTL_ELEM_LIST: {
        struct snd_ctl_elem_list *list = arg;

        list->count = CONTROL_COUNT;
        list->used = 0;
        if (list->pids && list->offset < CONTROL_COUNT) {
            unsigned available = CONTROL_COUNT - list->offset;
            unsigned index;

            list->used = list->space < available ? list->space : available;
            for (index = 0; index < list->used; index++)
                set_control_id(&list->pids[index], list->offset + index);
        }
        return 0;
    }

    case SNDRV_CTL_IOCTL_ELEM_INFO: {
        struct snd_ctl_elem_info *info = arg;
        unsigned index = control_index(&info->id);

        if (index >= CONTROL_COUNT) {
            errno = ENOENT;
            return -1;
        }

        set_control_id(&info->id, index);
        info->type = SNDRV_CTL_ELEM_TYPE_INTEGER;
        info->access = SNDRV_CTL_ELEM_ACCESS_READWRITE;
        info->count = 2;
        info->value.integer.min = 0;
        info->value.integer.max = 255;
        info->value.integer.step = 1;
        return 0;
    }

    case SNDRV_CTL_IOCTL_ELEM_READ: {
        struct snd_ctl_elem_value *value = arg;
        unsigned index = control_index(&value->id);

        if (index >= CONTROL_COUNT) {
            errno = ENOENT;
            return -1;
        }

        set_control_id(&value->id, index);
        value->value.integer.value[0] = control_values[index];
        value->value.integer.value[1] = control_values[index];
        return 0;
    }

    case SNDRV_CTL_IOCTL_ELEM_WRITE: {
        struct snd_ctl_elem_value *value = arg;
        unsigned index = control_index(&value->id);

        if (index >= CONTROL_COUNT) {
            errno = ENOENT;
            return -1;
        }

        control_values[index] = value->value.integer.value[0];
        fprintf(log_file, "ALSA control=%s value=%ld\n",
                control_names[index], control_values[index]);
        return 0;
    }

    case SNDRV_CTL_IOCTL_SUBSCRIBE_EVENTS:
        return 0;
    }

    {
        int result = forward_ioctl(fd, request, arg);

        if (result < 0)
            fprintf(log_file, "IOCTL fd=%d request=0x%08lx error=%d\n",
                    fd, request, errno);
        return result;
    }
}
