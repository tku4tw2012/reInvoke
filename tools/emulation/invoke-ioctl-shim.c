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
 *
 * The default mode remains synthetic emulation. Setting
 * INVOKE_IOCTL_MODE=record disables every synthetic handler, logs byte-exact
 * I2C_RDWR and SPI_IOC_MESSAGE requests and responses, and forwards each ioctl
 * unchanged to the kernel.
 */

#define _GNU_SOURCE
#include <linux/magic.h>
#include <linux/spi/spidev.h>
#include <sound/asound.h>
#include <sys/stat.h>
#include <sys/vfs.h>
#include <sys/ioctl.h>
#include <sys/syscall.h>

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <sched.h>
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
#define RECORD_BUFFER_SIZE (64 * 1024)
#define RECORD_FLUSH_INTERVAL 256

enum shim_mode {
    SHIM_MODE_EMULATE,
    SHIM_MODE_RECORD
};

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
static int shim_init_errno;
static enum shim_mode shim_mode;
static unsigned long record_sequence;
static uint8_t registers[REGISTER_COUNT][REGISTER_COUNT];
static uint8_t last_register[REGISTER_COUNT];
static const char *const control_names[CONTROL_COUNT] = {
    "music", "call", "voice", "system", "timer", "mic"
};
static long control_values[CONTROL_COUNT] = {255, 255, 255, 255, 255, 255};

static int forward_ioctl(int fd, unsigned long request, void *arg)
{
#ifdef INVOKE_IOCTL_SHIM_TEST
    extern int invoke_test_forward_ioctl(int, unsigned long, void *);

    return invoke_test_forward_ioctl(fd, request, arg);
#else
    /*
     * dlsym(RTLD_NEXT) from the host toolchain requires GLIBC_2.34. A direct
     * syscall keeps this library compatible with the firmware's glibc 2.23.
     */
    return syscall(SYS_ioctl, fd, request, arg);
#endif
}

#ifndef INVOKE_IOCTL_SHIM_TEST
static int kernel_fstat(int fd, struct stat64 *status)
{
#ifdef __arm__
    return syscall(SYS_fstat64, fd, status);
#else
    return fstat64(fd, status);
#endif
}
#endif

static FILE *open_record_log(const char *path)
{
#ifdef INVOKE_IOCTL_SHIM_TEST
    extern FILE *invoke_test_open_record_log(const char *);

    return invoke_test_open_record_log(path);
#else
    char parent[PATH_MAX];
    char proc_path[64];
    const char *name;
    struct stat64 path_status;
    struct stat64 status;
    struct statfs filesystem;
    int directory_fd;
    int log_fd;
    int path_fd;
    size_t parent_length;
    FILE *stream;

    if (!path || path[0] != '/' || strlen(path) >= sizeof(parent)) {
        errno = EINVAL;
        return NULL;
    }

    name = strrchr(path, '/');
    if (!name || name[1] == '\0') {
        errno = EINVAL;
        return NULL;
    }
    parent_length = (size_t)(name - path);
    if (parent_length == 0)
        parent_length = 1;
    memcpy(parent, path, parent_length);
    parent[parent_length] = '\0';
    name++;

    directory_fd = open(parent, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (directory_fd < 0)
        return NULL;
    if (fstatfs(directory_fd, &filesystem) < 0 ||
        ((uint32_t)filesystem.f_type != (uint32_t)TMPFS_MAGIC &&
         (uint32_t)filesystem.f_type != (uint32_t)RAMFS_MAGIC)) {
        close(directory_fd);
        errno = EPERM;
        return NULL;
    }

    path_fd = openat(directory_fd, name, O_PATH | O_CLOEXEC | O_NOFOLLOW);
    if (path_fd < 0 && errno == ENOENT) {
        log_fd = openat(directory_fd, name,
                        O_WRONLY | O_APPEND | O_CREAT | O_EXCL | O_CLOEXEC |
                        O_NOFOLLOW,
                        S_IRUSR | S_IWUSR);
        close(directory_fd);
        if (log_fd < 0)
            return NULL;
    } else if (path_fd >= 0) {
        if (kernel_fstat(path_fd, &path_status) < 0 ||
            !S_ISREG(path_status.st_mode)) {
            close(path_fd);
            close(directory_fd);
            errno = EPERM;
            return NULL;
        }
        snprintf(proc_path, sizeof(proc_path), "/proc/self/fd/%d", path_fd);
        log_fd = open(proc_path, O_WRONLY | O_APPEND | O_CLOEXEC);
        close(path_fd);
        close(directory_fd);
        if (log_fd < 0)
            return NULL;
        if (kernel_fstat(log_fd, &status) < 0 ||
            status.st_dev != path_status.st_dev ||
            status.st_ino != path_status.st_ino) {
            close(log_fd);
            errno = EPERM;
            return NULL;
        }
    } else {
        int saved_errno = errno;

        close(directory_fd);
        errno = saved_errno;
        return NULL;
    }
    if (kernel_fstat(log_fd, &status) < 0 || !S_ISREG(status.st_mode) ||
        fstatfs(log_fd, &filesystem) < 0 ||
        ((uint32_t)filesystem.f_type != (uint32_t)TMPFS_MAGIC &&
         (uint32_t)filesystem.f_type != (uint32_t)RAMFS_MAGIC)) {
        close(log_fd);
        errno = EPERM;
        return NULL;
    }

    stream = fdopen(log_fd, "a");
    if (!stream)
        close(log_fd);
    return stream;
#endif
}

static void initialize_shim(void)
{
    const char *path;

    if (!__sync_bool_compare_and_swap(&shim_ready, 0, 1)) {
        while (__sync_fetch_and_add(&shim_ready, 0) != 2)
            sched_yield();
        return;
    }

    path = getenv("INVOKE_IOCTL_MODE");
    shim_mode = path && strcmp(path, "record") == 0
                    ? SHIM_MODE_RECORD
                    : SHIM_MODE_EMULATE;
    path = getenv("INVOKE_IOCTL_LOG");
    if (shim_mode == SHIM_MODE_RECORD) {
        log_file = open_record_log(path);
        if (!log_file) {
            shim_init_errno = errno ? errno : EPERM;
            fprintf(stderr,
                    "invoke-ioctl-shim: record mode requires a regular "
                    "tmpfs/ramfs INVOKE_IOCTL_LOG\n");
            __sync_synchronize();
            shim_ready = 2;
            return;
        }
    } else {
        log_file = path ? fopen(path, "a") : stderr;
        if (!log_file)
            log_file = stderr;
    }
    setvbuf(log_file, NULL,
            shim_mode == SHIM_MODE_RECORD ? _IOFBF : _IOLBF,
            shim_mode == SHIM_MODE_RECORD ? RECORD_BUFFER_SIZE : 0);
    fprintf(log_file, "IOCTL_SHIM mode=%s\n",
            shim_mode == SHIM_MODE_RECORD ? "record" : "emulate");
    __sync_synchronize();
    shim_ready = 2;
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

static void log_bytes(const uint8_t *bytes, uint32_t length, int zero_fill)
{
    uint32_t index;

    if (!bytes && !zero_fill) {
        fputs("-", log_file);
        return;
    }
    for (index = 0; index < length; index++)
        fprintf(log_file, "%02x", zero_fill ? 0 : bytes[index]);
}

static void record_i2c_messages(unsigned long sequence, int fd,
                                const struct i2c_rdwr_ioctl_data *data,
                                int after, int result, int result_errno)
{
    uint32_t index;

    flockfile(log_file);
    fprintf(log_file,
            "I2C_RDWR seq=%lu phase=%s fd=%d nmsgs=%u result=%d errno=%d\n",
            sequence, after ? "result" : "request", fd, data->nmsgs,
            result, result_errno);
    for (index = 0; index < data->nmsgs; index++) {
        const struct i2c_msg *message = &data->msgs[index];
        int is_read = (message->flags & I2C_M_RD) != 0;

        if (after && (!is_read || result < 0 || index >= (uint32_t)result))
            continue;
        fprintf(log_file,
                "I2C_RDWR seq=%lu phase=%s msg=%u addr=0x%04x "
                "flags=0x%04x len=%u data=",
                sequence, after ? "response" : "request", index,
                message->addr, message->flags, message->len);
        if (!after && is_read)
            fputs("<read>", log_file);
        else
            log_bytes(message->buf, message->len, 0);
        fputc('\n', log_file);
    }
    funlockfile(log_file);
}

static int is_spi_message(unsigned long request)
{
    size_t size = _IOC_SIZE(request);

    return _IOC_TYPE(request) == SPI_IOC_MAGIC &&
           _IOC_NR(request) == _IOC_NR(SPI_IOC_MESSAGE(1)) &&
           _IOC_DIR(request) == _IOC_WRITE &&
           size > 0 &&
           size % sizeof(struct spi_ioc_transfer) == 0;
}

static int is_approved_capture_fd(int fd, unsigned long request)
{
#ifdef INVOKE_IOCTL_SHIM_TEST
    extern int invoke_test_approve_fd(int, unsigned long);

    return invoke_test_approve_fd(fd, request);
#else
    const char *expected = request == I2C_RDWR
                               ? "/dev/i2c-0"
                               : "/dev/spidev0.0";
    char link_path[64];
    char target[PATH_MAX];
    struct stat64 expected_status;
    struct stat64 fd_status;
    int expected_fd;
    ssize_t length;

    if (kernel_fstat(fd, &fd_status) < 0 || !S_ISCHR(fd_status.st_mode))
        return 0;
    expected_fd = open(expected, O_PATH | O_CLOEXEC | O_NOFOLLOW);
    if (expected_fd < 0)
        return 0;
    if (kernel_fstat(expected_fd, &expected_status) < 0 ||
        !S_ISCHR(expected_status.st_mode) ||
        expected_status.st_rdev != fd_status.st_rdev) {
        close(expected_fd);
        return 0;
    }
    close(expected_fd);

    snprintf(link_path, sizeof(link_path), "/proc/self/fd/%d", fd);
    length = readlink(link_path, target, sizeof(target) - 1);
    if (length < 0 || (size_t)length >= sizeof(target))
        return 0;
    target[length] = '\0';
    return strcmp(target, expected) == 0;
#endif
}

static void record_spi_transfers(unsigned long sequence, int fd,
                                 unsigned long request,
                                 const struct spi_ioc_transfer *transfers,
                                 int after, int result, int result_errno)
{
    size_t count = _IOC_SIZE(request) / sizeof(*transfers);
    size_t index;

    flockfile(log_file);
    fprintf(log_file,
            "SPI_IOC_MESSAGE seq=%lu phase=%s fd=%d transfers=%zu "
            "result=%d errno=%d\n",
            sequence, after ? "result" : "request", fd, count,
            result, result_errno);
    for (index = 0; index < count; index++) {
        const struct spi_ioc_transfer *transfer = &transfers[index];
        const uint8_t *bytes = (const uint8_t *)(uintptr_t)
                                   (after ? transfer->rx_buf
                                          : transfer->tx_buf);

        if (after && result < 0)
            continue;
        fprintf(log_file,
                "SPI_IOC_MESSAGE seq=%lu phase=%s transfer=%zu len=%u "
                "speed_hz=%u delay_usecs=%u bits_per_word=%u cs_change=%u "
                "tx_nbits=%u rx_nbits=%u word_delay_usecs=%u %s=",
                sequence, after ? "response" : "request", index,
                transfer->len, transfer->speed_hz, transfer->delay_usecs,
                transfer->bits_per_word, transfer->cs_change,
                transfer->tx_nbits, transfer->rx_nbits,
                transfer->word_delay_usecs, after ? "rx" : "tx");
        log_bytes(bytes, transfer->len, !after && !bytes);
        fputc('\n', log_file);
    }
    funlockfile(log_file);
}

static int record_needs_flush(unsigned long sequence, unsigned long request,
                              const void *arg)
{
    const struct spi_ioc_transfer *transfers;
    size_t count;
    size_t index;

    if (request == I2C_RDWR || sequence % RECORD_FLUSH_INTERVAL == 0)
        return 1;
    if (!arg || !is_spi_message(request))
        return 0;

    transfers = arg;
    count = _IOC_SIZE(request) / sizeof(*transfers);
    for (index = 0; index < count; index++)
        if (transfers[index].len != 4)
            return 1;
    return 0;
}

static int record_and_forward_ioctl(int fd, unsigned long request, void *arg)
{
    unsigned long sequence;
    int result;
    int forward_errno;
    int result_errno;

    if (request != I2C_RDWR && !is_spi_message(request))
        return forward_ioctl(fd, request, arg);
    if (!is_approved_capture_fd(fd, request)) {
        errno = EPERM;
        return -1;
    }

    sequence = __sync_add_and_fetch(&record_sequence, 1);
    if (!arg) {
        flockfile(log_file);
        fprintf(log_file,
                "IOCTL_RECORD seq=%lu phase=request fd=%d request=0x%08lx "
                "capture=skipped-null-argument\n",
                sequence, fd, request);
        funlockfile(log_file);
    } else if (request == I2C_RDWR) {
        const struct i2c_rdwr_ioctl_data *data = arg;

        if (data->msgs)
            record_i2c_messages(sequence, fd, data, 0, 0, 0);
        else {
            flockfile(log_file);
            fprintf(log_file,
                    "I2C_RDWR seq=%lu phase=request fd=%d nmsgs=%u "
                    "capture=skipped-null-messages\n",
                    sequence, fd, data->nmsgs);
            funlockfile(log_file);
        }
    } else {
        record_spi_transfers(sequence, fd, request, arg, 0, 0, 0);
    }

    result = forward_ioctl(fd, request, arg);
    forward_errno = errno;
    result_errno = result < 0 ? forward_errno : 0;

    if (arg && request == I2C_RDWR &&
        ((struct i2c_rdwr_ioctl_data *)arg)->msgs)
        record_i2c_messages(sequence, fd, arg, 1, result, result_errno);
    else if (arg && is_spi_message(request))
        record_spi_transfers(sequence, fd, request, arg, 1, result,
                             result_errno);
    else {
        flockfile(log_file);
        fprintf(log_file,
                "IOCTL_RECORD seq=%lu phase=result fd=%d request=0x%08lx "
                "result=%d errno=%d\n",
                sequence, fd, request, result, result_errno);
        funlockfile(log_file);
    }

    if (record_needs_flush(sequence, request, arg)) {
        flockfile(log_file);
        fflush(log_file);
        funlockfile(log_file);
    }
    errno = forward_errno;
    return result;
}

__attribute__((destructor))
static void flush_record_log(void)
{
    if (__sync_fetch_and_add(&shim_ready, 0) == 2 &&
        shim_mode == SHIM_MODE_RECORD && log_file) {
        flockfile(log_file);
        fflush(log_file);
        funlockfile(log_file);
    }
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

    if (shim_mode == SHIM_MODE_RECORD) {
        if (shim_init_errno) {
            errno = shim_init_errno;
            return -1;
        }
        return record_and_forward_ioctl(fd, request, arg);
    }

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
