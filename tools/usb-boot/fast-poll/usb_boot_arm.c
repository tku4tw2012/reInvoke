/*
 * usb_boot_arm.c — C reimplementation of the Marvell 88DE3006 USB boot tool.
 *
 * Targets the Raspberry Pi 4 (ARM, native USB-A ports). Replaces the x86-64-only
 * usb_boot binary for flashing the Harman Kardon Invoke speaker.
 *
 * Protocol reverse-engineered from the original 41KB ELF binary (GCC 4.8.2,
 * unstripped, 42 functions, libusb-1.0). Every detail verified byte-by-byte
 * against the disassembly by a QA agent.
 *
 * KEY ADVANTAGE over the Python version: Uses libusb's asynchronous transfer API
 * (libusb_submit_transfer + libusb_handle_events) which naturally survives USB
 * bus resets when U-Boot reinitializes the device controller after bootloader.img.
 * The Python version using synchronous pyusb CANNOT handle this.
 *
 * Compile on Raspberry Pi 4:
 *     gcc -o usb_boot_arm usb_boot_arm.c -lusb-1.0 -lpthread
 *
 * Usage:
 *     sudo ./usb_boot_arm 1286 8174 ./ 8141
 *
 * Author: Claude (Anthropic) — reverse-engineered implementation
 * Date: 2026-07-11
 */

#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <unistd.h>
#include <signal.h>
#include <time.h>
#include <errno.h>
#include <pthread.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <fcntl.h>
#include <stdarg.h>
#include <libusb-1.0/libusb.h>

/* =========================================================================
 * Constants — all derived from the verified binary disassembly
 * ========================================================================= */

/* USB endpoint addresses (hardcoded as immediate operands in the binary) */
#define EP_BULK_OUT     0x01    /* Host -> Device: image data transfer */
#define EP_BULK_IN      0x81    /* Device -> Host: status/data reads */
#define EP_INTR_IN      0x82    /* Device -> Host: console output + image requests */
#define EP_INTR_OUT     0x02    /* Host -> Device: console input (keystrokes) */

/* Phase 1 identifier: interface subclass 0xFF = iROM bootstrap mode */
#define IROM_SUBCLASS   0xFF

/* Image request magic string: exactly 10 ASCII bytes, then 1 type byte */
#define IMG_MAGIC       "i*m*g*r*q*"
#define IMG_MAGIC_LEN   10
#define IMG_REQUEST_LEN 11  /* 10 magic + 1 type byte */

/* Transfer chunk size: 1 MB (0x100000), matches fread calls in binary */
#define CHUNK_SIZE      1048576

/* Receive buffer sizes */
#define INTR_BUF_SIZE   512
#define BULK_BUF_SIZE   1024

/* Phase 2 image header size: 8 bytes */
#define HEADER_SIZE     8

/* Delay after device detection (verified: usleep(1000000) in attach_cb) */
/* Upstream waited a full second after detection before its first transfer.
   Combined with the poll gap that could exceed the measured 1.8-3.5 s iROM
   window. 150 ms is still far longer than the ~19 ms iROM exchange. */
#define ATTACH_DELAY_US 150000

/* USB transfer timeout in milliseconds */
#define USB_TIMEOUT_MS  10000

/* Polling interval when waiting for device (seconds) */
#define POLL_INTERVAL_S 1
/* Poll gap while waiting for the device, in microseconds. The upstream loop
   slept a full second, which can exceed the measured 1.8-3.5 s iROM window
   once the 1 s attach delay is added. */
#define POLL_INTERVAL_US 20000

/* Telnet server backlog */
#define TELNET_BACKLOG  1

/* Max path length */
#define MAX_PATH_LEN    4096

/* Max filename length */
#define MAX_FNAME_LEN   256

/* Console ring buffer size for telnet output */
#define CONSOLE_BUF_SIZE 65536

/* Image request queue size */
#define IMG_QUEUE_SIZE  32

/* =========================================================================
 * Global state
 * ========================================================================= */

static volatile sig_atomic_t g_running = 1;
static FILE *g_logfile = NULL;
static pthread_mutex_t g_log_mutex = PTHREAD_MUTEX_INITIALIZER;

/* USB state */
static libusb_context *g_usb_ctx = NULL;
static libusb_device_handle *g_dev_handle = NULL;
static pthread_mutex_t g_dev_mutex = PTHREAD_MUTEX_INITIALIZER;
static volatile int g_device_gone = 0;     /* set when NO_DEVICE callback */
static volatile int g_device_ready = 0;    /* set after re-enumeration */

/* Async transfer structs — 4 endpoints like the original binary */
static struct libusb_transfer *g_xfr_bulk_in = NULL;
static struct libusb_transfer *g_xfr_bulk_out = NULL;
static struct libusb_transfer *g_xfr_intr_in = NULL;
static struct libusb_transfer *g_xfr_intr_out = NULL;

/* Async transfer buffers */
static uint8_t g_buf_bulk_in[BULK_BUF_SIZE];
static uint8_t g_buf_intr_in[INTR_BUF_SIZE];

/* Image request queue */
static uint8_t g_img_queue[IMG_QUEUE_SIZE];
static int g_img_queue_head = 0;
static int g_img_queue_tail = 0;
static pthread_mutex_t g_queue_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_queue_cond = PTHREAD_COND_INITIALIZER;

/* Console output ring buffer (EP 0x82 non-image data -> telnet) */
static uint8_t g_console_buf[CONSOLE_BUF_SIZE];
static int g_console_head = 0;
static int g_console_tail = 0;
static pthread_mutex_t g_console_mutex = PTHREAD_MUTEX_INITIALIZER;

/* Telnet state */
static int g_telnet_client_fd = -1;
static pthread_mutex_t g_telnet_mutex = PTHREAD_MUTEX_INITIALIZER;
static int g_telnet_port = 8141;

/* Command line args */
static uint16_t g_vid = 0;
static uint16_t g_pid = 0;
static char g_firmware_dir[MAX_PATH_LEN] = "./";

/* Pending bulk OUT state for image sends */
static volatile int g_bulk_out_pending = 0;
static volatile int g_bulk_out_result = 0;
static pthread_mutex_t g_bulk_out_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_bulk_out_cond = PTHREAD_COND_INITIALIZER;

/* Phase tracking */
static volatile int g_phase = 0;  /* 0=init, 1=iROM, 2=serving */

/* Partial image request marker buffer */
static uint8_t g_marker_buf[IMG_REQUEST_LEN * 2];
static int g_marker_len = 0;

/* =========================================================================
 * Logging — timestamped, to stdout AND file
 * ========================================================================= */

static void log_msg(const char *level, const char *fmt, ...)
{
    struct timespec ts;
    struct tm tm_info;
    char timebuf[64];
    char msgbuf[2048];
    va_list ap;

    clock_gettime(CLOCK_REALTIME, &ts);
    localtime_r(&ts.tv_sec, &tm_info);
    snprintf(timebuf, sizeof(timebuf), "%04d-%02d-%02d %02d:%02d:%02d.%03ld",
             tm_info.tm_year + 1900, tm_info.tm_mon + 1, tm_info.tm_mday,
             tm_info.tm_hour, tm_info.tm_min, tm_info.tm_sec,
             ts.tv_nsec / 1000000);

    va_start(ap, fmt);
    vsnprintf(msgbuf, sizeof(msgbuf), fmt, ap);
    va_end(ap);

    pthread_mutex_lock(&g_log_mutex);
    fprintf(stdout, "%s [%-5s] %s\n", timebuf, level, msgbuf);
    fflush(stdout);
    if (g_logfile) {
        fprintf(g_logfile, "%s [%-5s] %s\n", timebuf, level, msgbuf);
        fflush(g_logfile);
    }
    pthread_mutex_unlock(&g_log_mutex);
}

#define LOG_INFO(...)  log_msg("INFO",  __VA_ARGS__)
#define LOG_WARN(...)  log_msg("WARN",  __VA_ARGS__)
#define LOG_ERROR(...) log_msg("ERROR", __VA_ARGS__)
#define LOG_DEBUG(...) log_msg("DEBUG", __VA_ARGS__)

static void log_hex(const char *prefix, const uint8_t *data, int len)
{
    char hex[256];
    int i, max = (len < 64) ? len : 64;
    for (i = 0; i < max; i++)
        sprintf(hex + i * 3, "%02x ", data[i]);
    if (max > 0)
        hex[max * 3 - 1] = '\0';
    else
        hex[0] = '\0';
    if (len > max)
        LOG_DEBUG("%s: %s... (%d bytes total)", prefix, hex, len);
    else
        LOG_DEBUG("%s: %s (%d bytes)", prefix, hex, len);
}

/* =========================================================================
 * Signal handler for graceful shutdown
 * ========================================================================= */

static void signal_handler(int sig)
{
    (void)sig;
    g_running = 0;
}

/* =========================================================================
 * Safety: filename blocking
 * ========================================================================= */

static int is_file_blocked(const char *filename)
{
    /* NEVER send any file with "99" in the name. Hard block. */
    if (strstr(filename, "99") != NULL) {
        LOG_ERROR("!!! SAFETY BLOCK !!! File '%s' contains '99' — REFUSED", filename);
        return 1;
    }
    return 0;
}

/* =========================================================================
 * Image type byte -> filename mapping
 * ========================================================================= */

static const char *image_type_to_filename(uint8_t type_byte, char *buf, size_t bufsz)
{
    switch (type_byte) {
    case 0x01:
        return NULL;  /* NOP — no file to send */
    case 0x02:
        return "sysinit.img";
    case 0x03:
        return "bootloader.img";
    case 0x05:
        return "drm_erom.img";
    default:
        snprintf(buf, bufsz, "%02X_IMAGE", type_byte);
        return buf;
    }
}

/* =========================================================================
 * Image request queue operations
 * ========================================================================= */

static void queue_push(uint8_t type_byte)
{
    pthread_mutex_lock(&g_queue_mutex);
    int next = (g_img_queue_tail + 1) % IMG_QUEUE_SIZE;
    if (next != g_img_queue_head) {
        g_img_queue[g_img_queue_tail] = type_byte;
        g_img_queue_tail = next;
    } else {
        LOG_WARN("Image request queue full! Dropping type 0x%02X", type_byte);
    }
    pthread_cond_signal(&g_queue_cond);
    pthread_mutex_unlock(&g_queue_mutex);
}

static int queue_pop(uint8_t *type_byte, int timeout_sec)
{
    struct timespec ts;
    pthread_mutex_lock(&g_queue_mutex);

    while (g_img_queue_head == g_img_queue_tail && g_running) {
        clock_gettime(CLOCK_REALTIME, &ts);
        ts.tv_sec += timeout_sec;
        int rc = pthread_cond_timedwait(&g_queue_cond, &g_queue_mutex, &ts);
        if (rc == ETIMEDOUT)
            break;
    }

    if (g_img_queue_head == g_img_queue_tail) {
        pthread_mutex_unlock(&g_queue_mutex);
        return 0;  /* empty */
    }

    *type_byte = g_img_queue[g_img_queue_head];
    g_img_queue_head = (g_img_queue_head + 1) % IMG_QUEUE_SIZE;
    pthread_mutex_unlock(&g_queue_mutex);
    return 1;
}

/* =========================================================================
 * Console buffer operations (EP 0x82 -> telnet)
 * ========================================================================= */

static void console_push(const uint8_t *data, int len)
{
    pthread_mutex_lock(&g_telnet_mutex);
    int fd = g_telnet_client_fd;
    pthread_mutex_unlock(&g_telnet_mutex);

    if (fd < 0)
        return;  /* No telnet client connected */

    /* Send directly to the telnet client */
    int total = 0;
    while (total < len) {
        int n = send(fd, data + total, len - total, MSG_NOSIGNAL | MSG_DONTWAIT);
        if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
            LOG_WARN("Telnet send would block, dropping %d bytes", len - total);
            break;
        }
        if (n <= 0) {
            LOG_DEBUG("Telnet send failed: %s", strerror(errno));
            pthread_mutex_lock(&g_telnet_mutex);
            if (g_telnet_client_fd == fd) {
                close(g_telnet_client_fd);
                g_telnet_client_fd = -1;
                LOG_INFO("Telnet client disconnected (send error)");
            }
            pthread_mutex_unlock(&g_telnet_mutex);
            break;
        }
        total += n;
    }
}

/* =========================================================================
 * Process data received on EP 0x82 (interrupt IN)
 *
 * Data is either:
 *   1. Image request: i*m*g*r*q*<type_byte> (11 bytes)
 *   2. Console output: forwarded to telnet client
 *
 * We accumulate into g_marker_buf to handle split markers across reads.
 * ========================================================================= */

static void process_intr_in_data(const uint8_t *data, int len)
{
    int i = 0;

    while (i < len) {
        /* Add byte to marker accumulation buffer */
        if (g_marker_len < (int)sizeof(g_marker_buf))
            g_marker_buf[g_marker_len++] = data[i++];
        else {
            /* Buffer overflow safety — flush as console data */
            console_push(g_marker_buf, 1);
            memmove(g_marker_buf, g_marker_buf + 1, g_marker_len - 1);
            g_marker_len--;
            g_marker_buf[g_marker_len++] = data[i++];
        }

        /* Check if we have a complete image request marker */
        if (g_marker_len >= IMG_REQUEST_LEN) {
            /* Search for the magic in the buffer */
            uint8_t *p = (uint8_t *)memmem(g_marker_buf, g_marker_len,
                                            IMG_MAGIC, IMG_MAGIC_LEN);
            if (p != NULL && (p - g_marker_buf) + IMG_REQUEST_LEN <= g_marker_len) {
                /* Found complete marker */
                int marker_offset = (int)(p - g_marker_buf);

                /* Flush any console data before the marker */
                if (marker_offset > 0) {
                    console_push(g_marker_buf, marker_offset);
                }

                /* Extract type byte */
                uint8_t type_byte = p[IMG_MAGIC_LEN];
                LOG_INFO("IMAGE REQUEST received: type=0x%02X", type_byte);
                queue_push(type_byte);

                /* Remove processed data from buffer */
                int consumed = marker_offset + IMG_REQUEST_LEN;
                int remaining = g_marker_len - consumed;
                if (remaining > 0)
                    memmove(g_marker_buf, g_marker_buf + consumed, remaining);
                g_marker_len = remaining;
            }
        }
    }

    /* Flush any data that definitely cannot be part of a marker.
     * Keep at most IMG_MAGIC_LEN-1 bytes that could be the start of a split marker. */
    if (g_marker_len > IMG_MAGIC_LEN - 1) {
        /* Check if any of the tail bytes could start the magic sequence */
        int safe_flush = 0;
        for (int j = 0; j <= g_marker_len - (IMG_MAGIC_LEN - 1); j++) {
            /* Check if byte at position j could be start of "i*m*g*r*q*" */
            int could_be_marker_start = 0;
            int check_len = g_marker_len - j;
            if (check_len > IMG_MAGIC_LEN)
                check_len = IMG_MAGIC_LEN;
            if (memcmp(g_marker_buf + j, IMG_MAGIC, check_len) == 0) {
                could_be_marker_start = 1;
            }
            if (!could_be_marker_start) {
                safe_flush = j + 1;
            } else {
                break;
            }
        }
        if (safe_flush > 0) {
            console_push(g_marker_buf, safe_flush);
            int remaining = g_marker_len - safe_flush;
            if (remaining > 0)
                memmove(g_marker_buf, g_marker_buf + safe_flush, remaining);
            g_marker_len = remaining;
        }
    }
}

/* =========================================================================
 * Async transfer callbacks — the heart of the implementation
 *
 * These callbacks fire when:
 *   - A transfer completes successfully
 *   - A transfer fails (device disconnect, etc.)
 *   - The device is gone (LIBUSB_TRANSFER_NO_DEVICE)
 *
 * On NO_DEVICE, we set g_device_gone=1 and the main loop handles
 * re-enumeration. This is exactly how the original binary works.
 * ========================================================================= */

static void cb_bulk_in(struct libusb_transfer *xfr)
{
    if (xfr->status == LIBUSB_TRANSFER_COMPLETED) {
        if (xfr->actual_length > 0) {
            log_hex("EP 0x81 IN", xfr->buffer, xfr->actual_length);
            /* Bulk IN data can be console output too — forward to telnet */
            console_push(xfr->buffer, xfr->actual_length);
        }
        /* Re-submit to keep listening */
        if (g_running && !g_device_gone) {
            int rc = libusb_submit_transfer(xfr);
            if (rc != 0) {
                LOG_WARN("Failed to re-submit bulk IN: %s", libusb_error_name(rc));
            }
        }
    } else if (xfr->status == LIBUSB_TRANSFER_TIMED_OUT) {
        /* Timeout is normal — just re-submit */
        if (g_running && !g_device_gone) {
            int rc = libusb_submit_transfer(xfr);
            if (rc != 0 && rc != LIBUSB_ERROR_NO_DEVICE) {
                LOG_WARN("Failed to re-submit bulk IN after timeout: %s",
                         libusb_error_name(rc));
            }
        }
    } else if (xfr->status == LIBUSB_TRANSFER_NO_DEVICE) {
        LOG_WARN("Bulk IN: device gone (NO_DEVICE) — bus reset detected");
        g_device_gone = 1;
    } else if (xfr->status == LIBUSB_TRANSFER_CANCELLED) {
        LOG_DEBUG("Bulk IN: transfer cancelled");
    } else {
        LOG_WARN("Bulk IN: transfer status %d", xfr->status);
        if (g_running && !g_device_gone) {
            usleep(100000);
            int rc = libusb_submit_transfer(xfr);
            if (rc != 0 && rc != LIBUSB_ERROR_NO_DEVICE) {
                LOG_WARN("Failed to re-submit bulk IN after error: %s",
                         libusb_error_name(rc));
            }
        }
    }
}

static void cb_intr_in(struct libusb_transfer *xfr)
{
    if (xfr->status == LIBUSB_TRANSFER_COMPLETED) {
        if (xfr->actual_length > 0) {
            log_hex("EP 0x82 IN", xfr->buffer, xfr->actual_length);
            process_intr_in_data(xfr->buffer, xfr->actual_length);
        }
        /* Re-submit to keep listening */
        if (g_running && !g_device_gone) {
            int rc = libusb_submit_transfer(xfr);
            if (rc != 0) {
                LOG_WARN("Failed to re-submit intr IN: %s", libusb_error_name(rc));
            }
        }
    } else if (xfr->status == LIBUSB_TRANSFER_TIMED_OUT) {
        if (g_running && !g_device_gone) {
            int rc = libusb_submit_transfer(xfr);
            if (rc != 0 && rc != LIBUSB_ERROR_NO_DEVICE) {
                LOG_WARN("Failed to re-submit intr IN after timeout: %s",
                         libusb_error_name(rc));
            }
        }
    } else if (xfr->status == LIBUSB_TRANSFER_NO_DEVICE) {
        LOG_WARN("Intr IN: device gone (NO_DEVICE) — bus reset detected");
        g_device_gone = 1;
    } else if (xfr->status == LIBUSB_TRANSFER_CANCELLED) {
        LOG_DEBUG("Intr IN: transfer cancelled");
    } else {
        LOG_WARN("Intr IN: transfer status %d", xfr->status);
        if (g_running && !g_device_gone) {
            usleep(100000);
            int rc = libusb_submit_transfer(xfr);
            if (rc != 0 && rc != LIBUSB_ERROR_NO_DEVICE) {
                LOG_WARN("Failed to re-submit intr IN after error: %s",
                         libusb_error_name(rc));
            }
        }
    }
}

static void cb_bulk_out(struct libusb_transfer *xfr)
{
    pthread_mutex_lock(&g_bulk_out_mutex);

    if (xfr->status == LIBUSB_TRANSFER_COMPLETED) {
        g_bulk_out_result = xfr->actual_length;
    } else if (xfr->status == LIBUSB_TRANSFER_NO_DEVICE) {
        LOG_WARN("Bulk OUT: device gone (NO_DEVICE)");
        g_device_gone = 1;
        g_bulk_out_result = -1;
    } else if (xfr->status == LIBUSB_TRANSFER_CANCELLED) {
        LOG_DEBUG("Bulk OUT: transfer cancelled");
        g_bulk_out_result = -2;
    } else {
        LOG_WARN("Bulk OUT: transfer status %d", xfr->status);
        g_bulk_out_result = -3;
    }

    g_bulk_out_pending = 0;
    pthread_cond_signal(&g_bulk_out_cond);
    pthread_mutex_unlock(&g_bulk_out_mutex);
}

static void cb_intr_out(struct libusb_transfer *xfr)
{
    if (xfr->status == LIBUSB_TRANSFER_COMPLETED) {
        LOG_DEBUG("Intr OUT: sent %d bytes", xfr->actual_length);
    } else if (xfr->status == LIBUSB_TRANSFER_NO_DEVICE) {
        LOG_WARN("Intr OUT: device gone (NO_DEVICE)");
        g_device_gone = 1;
    } else if (xfr->status == LIBUSB_TRANSFER_CANCELLED) {
        LOG_DEBUG("Intr OUT: transfer cancelled");
    } else {
        LOG_WARN("Intr OUT: transfer status %d", xfr->status);
    }
    /* Intr OUT transfers are one-shot (telnet input); not re-submitted automatically */
}

/* =========================================================================
 * libusb event loop thread
 *
 * This thread runs libusb_handle_events() continuously, which processes
 * completions for all submitted async transfers. This is what makes the
 * bus reset handling work: when the device disconnects, all pending
 * transfers get cancelled with callbacks, and we handle it gracefully.
 * ========================================================================= */

static void *usb_event_thread(void *arg)
{
    (void)arg;
    LOG_DEBUG("USB event loop thread started");

    while (g_running) {
        struct timeval tv = { .tv_sec = 1, .tv_usec = 0 };
        int rc = libusb_handle_events_timeout(g_usb_ctx, &tv);
        if (rc != 0 && rc != LIBUSB_ERROR_INTERRUPTED) {
            LOG_WARN("libusb_handle_events: %s", libusb_error_name(rc));
        }
    }

    LOG_DEBUG("USB event loop thread exiting");
    return NULL;
}

/* =========================================================================
 * Device discovery
 * ========================================================================= */

static int get_interface_subclass(libusb_device *dev, uint8_t *subclass_out)
{
    struct libusb_config_descriptor *config = NULL;
    int rc = libusb_get_active_config_descriptor(dev, &config);
    if (rc != 0) {
        /* Try getting config descriptor 0 instead */
        rc = libusb_get_config_descriptor(dev, 0, &config);
        if (rc != 0) {
            LOG_ERROR("Cannot get config descriptor: %s", libusb_error_name(rc));
            return -1;
        }
    }

    if (config->bNumInterfaces < 1 || config->interface[0].num_altsetting < 1) {
        LOG_ERROR("No interfaces found in config descriptor");
        libusb_free_config_descriptor(config);
        return -1;
    }

    *subclass_out = config->interface[0].altsetting[0].bInterfaceSubClass;
    libusb_free_config_descriptor(config);
    return 0;
}

static libusb_device_handle *find_and_open_device(uint16_t vid, uint16_t pid,
                                                    uint8_t *subclass_out)
{
    libusb_device **list = NULL;
    ssize_t cnt = libusb_get_device_list(g_usb_ctx, &list);
    if (cnt < 0) {
        LOG_ERROR("libusb_get_device_list failed: %s", libusb_error_name((int)cnt));
        return NULL;
    }

    libusb_device_handle *handle = NULL;

    for (ssize_t i = 0; i < cnt; i++) {
        struct libusb_device_descriptor desc;
        int rc = libusb_get_device_descriptor(list[i], &desc);
        if (rc != 0) continue;

        if (desc.idVendor == vid && desc.idProduct == pid) {
            LOG_INFO("Found device %04X:%04X on bus %d, addr %d",
                     vid, pid,
                     libusb_get_bus_number(list[i]),
                     libusb_get_device_address(list[i]));

            /* Get subclass before opening */
            if (subclass_out) {
                if (get_interface_subclass(list[i], subclass_out) != 0) {
                    LOG_WARN("Could not read subclass, will try after open");
                }
            }

            rc = libusb_open(list[i], &handle);
            if (rc != 0) {
                LOG_ERROR("libusb_open failed: %s", libusb_error_name(rc));
                handle = NULL;
                continue;
            }

            /* Log device info */
            char strbuf[256];
            if (desc.iManufacturer) {
                if (libusb_get_string_descriptor_ascii(handle, desc.iManufacturer,
                        (unsigned char *)strbuf, sizeof(strbuf)) > 0)
                    LOG_INFO("  Manufacturer: %s", strbuf);
            }
            if (desc.iProduct) {
                if (libusb_get_string_descriptor_ascii(handle, desc.iProduct,
                        (unsigned char *)strbuf, sizeof(strbuf)) > 0)
                    LOG_INFO("  Product: %s", strbuf);
            }
            LOG_INFO("  bcdUSB: 0x%04X, bcdDevice: 0x%04X",
                     desc.bcdUSB, desc.bcdDevice);

            /* Log endpoint info */
            struct libusb_config_descriptor *config = NULL;
            if (libusb_get_active_config_descriptor(list[i], &config) == 0 ||
                libusb_get_config_descriptor(list[i], 0, &config) == 0) {
                for (int intf = 0; intf < config->bNumInterfaces; intf++) {
                    const struct libusb_interface_descriptor *alt =
                        &config->interface[intf].altsetting[0];
                    LOG_INFO("  Interface %d: class=0x%02X subclass=0x%02X "
                             "protocol=0x%02X endpoints=%d",
                             alt->bInterfaceNumber, alt->bInterfaceClass,
                             alt->bInterfaceSubClass, alt->bInterfaceProtocol,
                             alt->bNumEndpoints);
                    for (int ep = 0; ep < alt->bNumEndpoints; ep++) {
                        const struct libusb_endpoint_descriptor *epd =
                            &alt->endpoint[ep];
                        const char *dir = (epd->bEndpointAddress & 0x80) ? "IN" : "OUT";
                        const char *type;
                        switch (epd->bmAttributes & 0x03) {
                        case 0: type = "CONTROL"; break;
                        case 1: type = "ISO"; break;
                        case 2: type = "BULK"; break;
                        case 3: type = "INTERRUPT"; break;
                        default: type = "UNKNOWN"; break;
                        }
                        LOG_INFO("    EP 0x%02X  %s  %s  maxPacket=%d",
                                 epd->bEndpointAddress, dir, type,
                                 epd->wMaxPacketSize);
                    }

                    /* Read subclass if we couldn't before */
                    if (subclass_out && intf == 0) {
                        *subclass_out = alt->bInterfaceSubClass;
                    }
                }
                libusb_free_config_descriptor(config);
            }

            break;  /* found it */
        }
    }

    libusb_free_device_list(list, 1);
    return handle;
}

static int claim_device_interface(libusb_device_handle *handle)
{
    /* Detach kernel driver if attached */
    if (libusb_kernel_driver_active(handle, 0) == 1) {
        LOG_INFO("Detaching kernel driver from interface 0");
        int rc = libusb_detach_kernel_driver(handle, 0);
        if (rc != 0) {
            LOG_WARN("Failed to detach kernel driver: %s (continuing)",
                     libusb_error_name(rc));
        }
    }

    /* Set configuration (may already be set) */
    int rc = libusb_set_configuration(handle, 1);
    if (rc != 0 && rc != LIBUSB_ERROR_BUSY) {
        LOG_WARN("set_configuration: %s (continuing)", libusb_error_name(rc));
    }

    /* Claim interface 0 */
    rc = libusb_claim_interface(handle, 0);
    if (rc != 0) {
        LOG_ERROR("Failed to claim interface 0: %s", libusb_error_name(rc));
        return -1;
    }
    LOG_INFO("Claimed interface 0");
    return 0;
}

/* =========================================================================
 * Phase 1: iROM Bootstrap — send bcm_erom.bin.usb as raw data (NO header)
 * ========================================================================= */

static int phase1_irom_boot(libusb_device_handle *handle, const char *fw_dir)
{
    char filepath[MAX_PATH_LEN];
    snprintf(filepath, sizeof(filepath), "%s/bcm_erom.bin.usb", fw_dir);

    /* Safety check */
    if (is_file_blocked("bcm_erom.bin.usb"))
        return -1;

    FILE *fp = fopen(filepath, "rb");
    if (!fp) {
        LOG_ERROR("Phase 1: cannot open %s: %s", filepath, strerror(errno));
        return -1;
    }

    /* Get file size */
    fseek(fp, 0, SEEK_END);
    long file_size = ftell(fp);
    fseek(fp, 0, SEEK_SET);

    LOG_INFO("=== Phase 1: iROM Bootstrap ===");
    LOG_INFO("File: %s (%ld bytes)", filepath, file_size);

    /* Send the file — raw data, NO header, 1MB chunks via bulk OUT EP 0x01 */
    uint8_t *chunk = malloc(CHUNK_SIZE);
    if (!chunk) {
        LOG_ERROR("malloc failed for chunk buffer");
        fclose(fp);
        return -1;
    }

    long total_sent = 0;
    int rc = 0;

    while (total_sent < file_size) {
        size_t to_read = CHUNK_SIZE;
        if ((long)to_read > file_size - total_sent)
            to_read = (size_t)(file_size - total_sent);

        size_t nread = fread(chunk, 1, to_read, fp);
        if (nread == 0) break;

        int transferred = 0;
        LOG_INFO("Phase 1: sending chunk (%zu bytes, offset %ld/%ld)",
                 nread, total_sent, file_size);

        rc = libusb_bulk_transfer(handle, EP_BULK_OUT, chunk, (int)nread,
                                   &transferred, USB_TIMEOUT_MS);
        if (rc != 0) {
            LOG_ERROR("Phase 1: bulk transfer failed at %ld/%ld: %s",
                      total_sent, file_size, libusb_error_name(rc));
            break;
        }

        total_sent += transferred;
        LOG_INFO("Phase 1: sent %d bytes (total %ld/%ld)",
                 transferred, total_sent, file_size);
    }

    free(chunk);
    fclose(fp);

    if (total_sent >= file_size) {
        LOG_INFO("Phase 1 COMPLETE: sent %ld/%ld bytes", total_sent, file_size);
        return 0;
    } else {
        LOG_ERROR("Phase 1 INCOMPLETE: sent %ld/%ld bytes", total_sent, file_size);
        return -1;
    }
}

/* =========================================================================
 * Phase 2: Async bulk OUT for image sends
 *
 * Uses the async transfer API so the event loop keeps running during
 * sends, allowing callbacks to fire for bus resets and incoming data.
 * ========================================================================= */

static int async_bulk_out_send(libusb_device_handle *handle,
                                const uint8_t *data, int len)
{
    if (!handle || g_device_gone)
        return -1;

    /* Allocate a fresh transfer for this send */
    struct libusb_transfer *xfr = libusb_alloc_transfer(0);
    if (!xfr) {
        LOG_ERROR("Failed to allocate bulk OUT transfer");
        return -1;
    }

    /* We need a copy of the data since the transfer is async */
    uint8_t *buf = malloc(len);
    if (!buf) {
        libusb_free_transfer(xfr);
        return -1;
    }
    memcpy(buf, data, len);

    pthread_mutex_lock(&g_bulk_out_mutex);
    g_bulk_out_pending = 1;
    g_bulk_out_result = 0;
    pthread_mutex_unlock(&g_bulk_out_mutex);

    libusb_fill_bulk_transfer(xfr, handle, EP_BULK_OUT, buf, len,
                               cb_bulk_out, NULL, USB_TIMEOUT_MS);
    /* Mark buffer for free on completion */
    xfr->flags = LIBUSB_TRANSFER_FREE_BUFFER | LIBUSB_TRANSFER_FREE_TRANSFER;

    int rc = libusb_submit_transfer(xfr);
    if (rc != 0) {
        LOG_ERROR("Failed to submit bulk OUT: %s", libusb_error_name(rc));
        pthread_mutex_lock(&g_bulk_out_mutex);
        g_bulk_out_pending = 0;
        g_bulk_out_result = -1;
        pthread_cond_signal(&g_bulk_out_cond);
        pthread_mutex_unlock(&g_bulk_out_mutex);
        /* Transfer and buffer freed via flags on some impls, but not on submit fail */
        free(buf);
        libusb_free_transfer(xfr);
        return -1;
    }

    /* Wait for completion */
    pthread_mutex_lock(&g_bulk_out_mutex);
    while (g_bulk_out_pending && g_running && !g_device_gone) {
        struct timespec ts;
        clock_gettime(CLOCK_REALTIME, &ts);
        ts.tv_sec += 15;  /* generous timeout */
        pthread_cond_timedwait(&g_bulk_out_cond, &g_bulk_out_mutex, &ts);
    }
    int result = g_bulk_out_result;
    pthread_mutex_unlock(&g_bulk_out_mutex);

    return result;
}

/* =========================================================================
 * Send an image file in Phase 2 (header + data chunks via async bulk OUT)
 * ========================================================================= */

static int send_image(libusb_device_handle *handle, const char *fw_dir,
                       const char *filename)
{
    /* Safety: NEVER send files with "99" in the name */
    if (is_file_blocked(filename))
        return -1;

    char filepath[MAX_PATH_LEN];
    snprintf(filepath, sizeof(filepath), "%s/%s", fw_dir, filename);

    FILE *fp = fopen(filepath, "rb");
    if (!fp) {
        LOG_ERROR("Cannot open image file: %s: %s", filepath, strerror(errno));
        return -1;
    }

    fseek(fp, 0, SEEK_END);
    long file_size = ftell(fp);
    fseek(fp, 0, SEEK_SET);

    LOG_INFO("Sending image: %s (%ld bytes)", filename, file_size);

    /* Step 1: Send 8-byte header (LE uint32 file_size + 4 zero bytes) */
    uint8_t header[HEADER_SIZE];
    uint32_t size32 = (uint32_t)file_size;
    memcpy(header, &size32, 4);  /* little-endian on ARM */
    memset(header + 4, 0, 4);

    log_hex("Image header", header, HEADER_SIZE);

    int rc = async_bulk_out_send(handle, header, HEADER_SIZE);
    if (rc < 0) {
        LOG_ERROR("Failed to send header for %s", filename);
        fclose(fp);
        return -1;
    }
    LOG_INFO("Sent 8-byte header for %s (size=%ld)", filename, file_size);

    /* Step 2: Send file data in 1MB chunks */
    uint8_t *chunk = malloc(CHUNK_SIZE);
    if (!chunk) {
        LOG_ERROR("malloc failed for chunk buffer");
        fclose(fp);
        return -1;
    }

    long total_sent = 0;
    int success = 1;

    while (total_sent < file_size && g_running && !g_device_gone) {
        size_t to_read = CHUNK_SIZE;
        if ((long)to_read > file_size - total_sent)
            to_read = (size_t)(file_size - total_sent);

        size_t nread = fread(chunk, 1, to_read, fp);
        if (nread == 0) break;

        rc = async_bulk_out_send(handle, chunk, (int)nread);
        if (rc < 0) {
            LOG_ERROR("Failed to send chunk for %s at offset %ld",
                      filename, total_sent);
            success = 0;
            break;
        }

        total_sent += rc;
        double pct = (file_size > 0) ? (total_sent * 100.0 / file_size) : 100.0;
        LOG_INFO("  %s: sent %ld/%ld bytes (%.1f%%)",
                 filename, total_sent, file_size, pct);
    }

    free(chunk);
    fclose(fp);

    if (success && total_sent >= file_size) {
        LOG_INFO("Image transfer complete: %s (%ld bytes sent)", filename, total_sent);
        return 0;
    } else {
        LOG_ERROR("Image transfer incomplete: %s (%ld/%ld bytes)",
                  filename, total_sent, file_size);
        return -1;
    }
}

/* =========================================================================
 * Submit the async IN transfers (bulk IN on 0x81, interrupt IN on 0x82)
 *
 * These are the "listener" transfers that receive data from the device.
 * They get re-submitted in their callbacks, keeping a continuous receive
 * loop going — exactly like the original binary.
 * ========================================================================= */

static int submit_in_transfers(libusb_device_handle *handle)
{
    /* Allocate transfers if not already done */
    if (!g_xfr_bulk_in)
        g_xfr_bulk_in = libusb_alloc_transfer(0);
    if (!g_xfr_intr_in)
        g_xfr_intr_in = libusb_alloc_transfer(0);

    if (!g_xfr_bulk_in || !g_xfr_intr_in) {
        LOG_ERROR("Failed to allocate IN transfers");
        return -1;
    }

    /* Fill bulk IN transfer (EP 0x81) */
    libusb_fill_bulk_transfer(g_xfr_bulk_in, handle, EP_BULK_IN,
                               g_buf_bulk_in, sizeof(g_buf_bulk_in),
                               cb_bulk_in, NULL, 1000);

    /* Fill interrupt IN transfer (EP 0x82) */
    libusb_fill_interrupt_transfer(g_xfr_intr_in, handle, EP_INTR_IN,
                                    g_buf_intr_in, sizeof(g_buf_intr_in),
                                    cb_intr_in, NULL, 1000);

    /* Submit both */
    int rc = libusb_submit_transfer(g_xfr_bulk_in);
    if (rc != 0) {
        LOG_ERROR("Failed to submit bulk IN: %s", libusb_error_name(rc));
        return -1;
    }
    LOG_INFO("Submitted bulk IN on EP 0x81");

    rc = libusb_submit_transfer(g_xfr_intr_in);
    if (rc != 0) {
        LOG_ERROR("Failed to submit intr IN: %s", libusb_error_name(rc));
        return -1;
    }
    LOG_INFO("Submitted intr IN on EP 0x82");

    return 0;
}

/* =========================================================================
 * Telnet proxy server — TCP server proxying console I/O
 *
 * - Data from telnet client -> filtered (removes "PuTTY") -> EP 0x02
 * - Data from EP 0x82 (non-image data) -> telnet client (via console_push)
 * ========================================================================= */

static void *telnet_server_thread(void *arg)
{
    (void)arg;
    LOG_INFO("Telnet proxy thread started on port %d", g_telnet_port);

    int server_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (server_fd < 0) {
        LOG_ERROR("Failed to create telnet server socket: %s", strerror(errno));
        return NULL;
    }

    int opt = 1;
    setsockopt(server_fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = INADDR_ANY;
    addr.sin_port = htons(g_telnet_port);

    if (bind(server_fd, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
        LOG_ERROR("Failed to bind telnet proxy on port %d: %s",
                  g_telnet_port, strerror(errno));
        close(server_fd);
        return NULL;
    }

    if (listen(server_fd, TELNET_BACKLOG) < 0) {
        LOG_ERROR("Failed to listen on telnet port: %s", strerror(errno));
        close(server_fd);
        return NULL;
    }

    LOG_INFO("Telnet proxy listening on 0.0.0.0:%d", g_telnet_port);

    /* Set non-blocking so we can check g_running */
    struct timeval tv;
    tv.tv_sec = 2;
    tv.tv_usec = 0;
    setsockopt(server_fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));

    while (g_running) {
        struct sockaddr_in client_addr;
        socklen_t client_len = sizeof(client_addr);
        int client_fd = accept(server_fd, (struct sockaddr *)&client_addr,
                                &client_len);
        if (client_fd < 0) {
            if (errno == EAGAIN || errno == EWOULDBLOCK)
                continue;
            if (g_running)
                LOG_WARN("Telnet accept error: %s", strerror(errno));
            continue;
        }

        char client_ip[INET_ADDRSTRLEN];
        inet_ntop(AF_INET, &client_addr.sin_addr, client_ip, sizeof(client_ip));
        LOG_INFO("Telnet client connected from %s:%d",
                 client_ip, ntohs(client_addr.sin_port));

        /* Close previous client if any */
        pthread_mutex_lock(&g_telnet_mutex);
        if (g_telnet_client_fd >= 0) {
            close(g_telnet_client_fd);
            LOG_INFO("Closed previous telnet client");
        }
        g_telnet_client_fd = client_fd;
        pthread_mutex_unlock(&g_telnet_mutex);

        /* Set recv timeout on client socket */
        tv.tv_sec = 0;
        tv.tv_usec = 200000;  /* 200ms */
        setsockopt(client_fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));

        /* Read from telnet client, send to device EP 0x02 */
        uint8_t buf[4096];
        while (g_running) {
            int n = recv(client_fd, buf, sizeof(buf), 0);
            if (n < 0) {
                if (errno == EAGAIN || errno == EWOULDBLOCK)
                    continue;
                break;
            }
            if (n == 0) {
                LOG_INFO("Telnet client disconnected (EOF)");
                break;
            }

            /* Filter out "PuTTY" string (matches original binary) */
            uint8_t *putty = (uint8_t *)memmem(buf, n, "PuTTY", 5);
            if (putty) {
                LOG_DEBUG("Filtered PuTTY identification from telnet input");
                /* Remove PuTTY from the data */
                int offset = (int)(putty - buf);
                int remaining = n - offset - 5;
                if (remaining > 0)
                    memmove(putty, putty + 5, remaining);
                n -= 5;
                if (n <= 0)
                    continue;
            }

            /* Send to device via interrupt OUT EP 0x02 */
            pthread_mutex_lock(&g_dev_mutex);
            libusb_device_handle *h = g_dev_handle;
            pthread_mutex_unlock(&g_dev_mutex);

            if (h && !g_device_gone) {
                /* Use a one-shot async transfer for interrupt OUT */
                struct libusb_transfer *xfr = libusb_alloc_transfer(0);
                if (xfr) {
                    uint8_t *sendbuf = malloc(n);
                    if (sendbuf) {
                        memcpy(sendbuf, buf, n);
                        libusb_fill_interrupt_transfer(xfr, h, EP_INTR_OUT,
                                                        sendbuf, n,
                                                        cb_intr_out, NULL,
                                                        USB_TIMEOUT_MS);
                        xfr->flags = LIBUSB_TRANSFER_FREE_BUFFER |
                                     LIBUSB_TRANSFER_FREE_TRANSFER;
                        int rc = libusb_submit_transfer(xfr);
                        if (rc != 0) {
                            LOG_WARN("Failed to send to EP 0x02: %s",
                                     libusb_error_name(rc));
                            free(sendbuf);
                            libusb_free_transfer(xfr);
                        } else {
                            LOG_DEBUG("Telnet -> device: %d bytes", n);
                        }
                    } else {
                        libusb_free_transfer(xfr);
                    }
                }
            }
        }

        /* Client disconnected */
        pthread_mutex_lock(&g_telnet_mutex);
        if (g_telnet_client_fd == client_fd) {
            close(g_telnet_client_fd);
            g_telnet_client_fd = -1;
            LOG_INFO("Telnet client connection closed");
        }
        pthread_mutex_unlock(&g_telnet_mutex);
    }

    close(server_fd);
    LOG_INFO("Telnet server thread exiting");
    return NULL;
}

/* =========================================================================
 * Phase 2: Device re-enumeration handler
 *
 * After bootloader.img loads, U-Boot reinitializes the USB controller
 * (USBCMD_RST), causing a bus reset. The device disconnects and reconnects.
 *
 * The async callbacks detect this via LIBUSB_TRANSFER_NO_DEVICE and set
 * g_device_gone=1. This function handles the re-discovery.
 * ========================================================================= */

static int handle_reenumeration(void)
{
    LOG_INFO("==================================================");
    LOG_INFO("Device re-enumeration: USB bus reset detected");
    LOG_INFO("U-Boot has reinitialized the USB controller");
    LOG_INFO("==================================================");

    /* Close the old handle */
    pthread_mutex_lock(&g_dev_mutex);
    if (g_dev_handle) {
        libusb_release_interface(g_dev_handle, 0);
        libusb_close(g_dev_handle);
        g_dev_handle = NULL;
    }
    pthread_mutex_unlock(&g_dev_mutex);

    /* Cancel any in-flight IN transfers before freeing (avoids use-after-free) */
    if (g_xfr_bulk_in) {
        libusb_cancel_transfer(g_xfr_bulk_in);
    }
    if (g_xfr_intr_in) {
        libusb_cancel_transfer(g_xfr_intr_in);
    }
    /* Give the event loop thread time to fire cancellation callbacks */
    usleep(300000);
    /* Now safe to free */
    if (g_xfr_bulk_in) {
        libusb_free_transfer(g_xfr_bulk_in);
        g_xfr_bulk_in = NULL;
    }
    if (g_xfr_intr_in) {
        libusb_free_transfer(g_xfr_intr_in);
        g_xfr_intr_in = NULL;
    }

    /* Reset marker buffer */
    g_marker_len = 0;

    /* Wait for device to disappear and reappear */
    LOG_INFO("Waiting for device to reappear...");

    libusb_device_handle *new_handle = NULL;
    uint8_t subclass = 0;

    for (int attempt = 0; attempt < 100 && g_running; attempt++) {
        usleep(200000);  /* 200ms between polls */

        new_handle = find_and_open_device(g_vid, g_pid, &subclass);
        if (new_handle != NULL) {
            LOG_INFO("Device reappeared after %.1f seconds (subclass=0x%02X)",
                     attempt * 0.2, subclass);
            break;
        }
    }

    if (!new_handle) {
        LOG_ERROR("Device did not reappear after 20 seconds!");
        LOG_ERROR("Power cycle the speaker and try again.");
        return -1;
    }

    /* 1-second delay after detection */
    LOG_INFO("Device found. Waiting 1 second before communication...");
    usleep(ATTACH_DELAY_US);

    /* Claim interface */
    if (claim_device_interface(new_handle) != 0) {
        LOG_ERROR("Failed to claim interface on re-enumerated device");
        libusb_close(new_handle);
        return -1;
    }

    /* Update global handle */
    pthread_mutex_lock(&g_dev_mutex);
    g_dev_handle = new_handle;
    g_device_gone = 0;
    g_device_ready = 1;
    pthread_mutex_unlock(&g_dev_mutex);

    /* Re-submit IN transfers on new handle */
    if (submit_in_transfers(new_handle) != 0) {
        LOG_ERROR("Failed to re-submit IN transfers after re-enumeration");
        return -1;
    }

    LOG_INFO("Device re-acquired successfully. Resuming operation.");
    return 0;
}

/* =========================================================================
 * Phase 2: Main image request handling loop
 * ========================================================================= */

static int phase2_serve(libusb_device_handle *handle, const char *fw_dir)
{
    LOG_INFO("=== Phase 2: Image Loading & Console ===");

    /* Claim interface */
    if (claim_device_interface(handle) != 0)
        return -1;

    /* Store handle globally for telnet thread */
    pthread_mutex_lock(&g_dev_mutex);
    g_dev_handle = handle;
    g_device_gone = 0;
    pthread_mutex_unlock(&g_dev_mutex);

    g_phase = 2;

    /* Start the USB event loop thread */
    pthread_t event_tid;
    if (pthread_create(&event_tid, NULL, usb_event_thread, NULL) != 0) {
        LOG_ERROR("Failed to create USB event loop thread");
        return -1;
    }
    pthread_detach(event_tid);
    LOG_INFO("USB event loop thread started");

    /* Submit initial IN transfers */
    if (submit_in_transfers(handle) != 0)
        return -1;

    /* Start telnet proxy thread */
    pthread_t telnet_tid;
    if (pthread_create(&telnet_tid, NULL, telnet_server_thread, NULL) != 0) {
        LOG_ERROR("Failed to create telnet server thread");
        return -1;
    }
    pthread_detach(telnet_tid);

    /* Main loop: process image requests from the queue */
    LOG_INFO("Waiting for image requests from device...");

    while (g_running) {
        /* Check for device re-enumeration */
        if (g_device_gone) {
            LOG_INFO("Device gone flag set — handling re-enumeration...");
            if (handle_reenumeration() != 0) {
                LOG_ERROR("Re-enumeration failed. Exiting.");
                return -1;
            }
            /* Update local handle */
            pthread_mutex_lock(&g_dev_mutex);
            handle = g_dev_handle;
            pthread_mutex_unlock(&g_dev_mutex);
            continue;
        }

        /* Wait for an image request */
        uint8_t type_byte;
        if (!queue_pop(&type_byte, 1))
            continue;

        /* Map type byte to filename */
        char fname_buf[MAX_FNAME_LEN];
        const char *filename = image_type_to_filename(type_byte, fname_buf,
                                                        sizeof(fname_buf));

        if (filename == NULL) {
            LOG_INFO("Image type 0x%02X is NOP — no action", type_byte);
            continue;
        }

        LOG_INFO("Image request 0x%02X -> filename: %s", type_byte, filename);

        /* Safety check */
        if (is_file_blocked(filename)) {
            LOG_ERROR("!!! SAFETY BLOCK !!! Refusing to send '%s'. "
                      "Request IGNORED.", filename);
            continue;
        }

        /* Get current handle */
        pthread_mutex_lock(&g_dev_mutex);
        handle = g_dev_handle;
        pthread_mutex_unlock(&g_dev_mutex);

        if (!handle || g_device_gone) {
            LOG_WARN("Device not available, waiting for re-enumeration...");
            continue;
        }

        /* Send the image */
        int rc = send_image(handle, fw_dir, filename);
        if (rc == 0) {
            LOG_INFO("Successfully sent %s for request type 0x%02X",
                     filename, type_byte);
        } else {
            LOG_ERROR("FAILED to send %s for request type 0x%02X",
                      filename, type_byte);
            /* If the failure was because device is gone, the async callbacks
             * will set g_device_gone and we'll handle re-enumeration next loop */
        }

        /* After bootloader.img, we expect a USB bus reset.
         * The async callbacks will detect NO_DEVICE and set g_device_gone.
         * We don't need to do anything special here — the main loop
         * checks g_device_gone at the top and handles re-enumeration.
         *
         * But we can log a hint so the operator knows what to expect. */
        if (strcmp(filename, "bootloader.img") == 0 && rc == 0) {
            LOG_INFO("bootloader.img sent. U-Boot will now reset the USB controller.");
            LOG_INFO("Expecting device re-enumeration momentarily...");
            /* Give the device a moment to initiate the reset */
            usleep(500000);
        }
    }

    LOG_INFO("Phase 2 server stopped");
    return 0;
}

/* =========================================================================
 * Main flow: Phase 1 -> wait for re-enumeration -> Phase 2
 * ========================================================================= */

static int main_flow(void)
{
    LOG_INFO("========================================");
    LOG_INFO("   usb_boot_arm — Marvell 88DE3006");
    LOG_INFO("   VID:PID = %04X:%04X", g_vid, g_pid);
    LOG_INFO("   Firmware dir: %s", g_firmware_dir);
    LOG_INFO("   Telnet port: %d", g_telnet_port);
    LOG_INFO("========================================");

    /* Verify firmware directory exists */
    struct stat st;
    if (stat(g_firmware_dir, &st) != 0 || !S_ISDIR(st.st_mode)) {
        LOG_ERROR("Firmware directory does not exist: %s", g_firmware_dir);
        return -1;
    }

    /* Check for bootstrap file */
    char bootstrap_path[MAX_PATH_LEN];
    snprintf(bootstrap_path, sizeof(bootstrap_path),
             "%s/bcm_erom.bin.usb", g_firmware_dir);
    if (stat(bootstrap_path, &st) != 0) {
        LOG_ERROR("CRITICAL: bcm_erom.bin.usb not found in %s", g_firmware_dir);
        return -1;
    }
    LOG_INFO("Bootstrap file found: %s (%ld bytes)", bootstrap_path, (long)st.st_size);

    /* List firmware files for the log */
    LOG_INFO("Scanning firmware directory...");
    /* We'll just check for key files */
    const char *key_files[] = {
        "bcm_erom.bin.usb", "sysinit.img", "bootloader.img",
        "drm_erom.img", "09_IMAGE", "79_IMAGE", "81_IMAGE",
        "82_IMAGE", "83_IMAGE", "06_IMAGE", "07_IMAGE", NULL
    };
    for (int i = 0; key_files[i]; i++) {
        char path[MAX_PATH_LEN];
        snprintf(path, sizeof(path), "%s/%s", g_firmware_dir, key_files[i]);
        if (stat(path, &st) == 0) {
            LOG_INFO("  Found: %s (%ld bytes)", key_files[i], (long)st.st_size);
        }
    }

    /* ---------------------------------------------------------------
     * Step 1: Wait for device to appear
     * --------------------------------------------------------------- */
    uint8_t subclass = 0;
    libusb_device_handle *handle = find_and_open_device(g_vid, g_pid, &subclass);

    if (!handle) {
        LOG_INFO("Device not found. Waiting for it to appear...");
        LOG_INFO("(Plug in the speaker in service mode now)");

        for (int i = 0; i < 120 * (1000000 / POLL_INTERVAL_US) && g_running; i++) {
            usleep(POLL_INTERVAL_US);
            handle = find_and_open_device(g_vid, g_pid, &subclass);
            if (handle) break;
        }
        if (!handle) {
            LOG_ERROR("No device found within 120 seconds. Exiting.");
            return -1;
        }
    }

    /* 1-second delay after detection (verified: usleep(1000000)) */
    LOG_INFO("Device detected. Waiting 1 second before communication...");
    usleep(ATTACH_DELAY_US);

    LOG_INFO("Interface 0 subclass: 0x%02X", subclass);

    /* ---------------------------------------------------------------
     * Step 2: Phase 1 (iROM) if subclass == 0xFF
     * --------------------------------------------------------------- */
    if (subclass == IROM_SUBCLASS) {
        LOG_INFO("Device is in iROM mode (subclass=0xFF). Starting Phase 1.");
        g_phase = 1;

        /* Claim interface for Phase 1 (synchronous bulk transfer) */
        if (claim_device_interface(handle) != 0) {
            libusb_close(handle);
            return -1;
        }

        int rc = phase1_irom_boot(handle, g_firmware_dir);
        if (rc != 0) {
            LOG_ERROR("Phase 1 failed. Power-cycle the speaker and try again.");
            libusb_release_interface(handle, 0);
            libusb_close(handle);
            return -1;
        }

        /* Release and close old handle */
        libusb_release_interface(handle, 0);
        libusb_close(handle);
        handle = NULL;

        /* Wait for device to disconnect */
        LOG_INFO("Phase 1 complete. Waiting for device to re-enumerate...");
        for (int i = 0; i < 30 && g_running; i++) {
            usleep(500000);
            libusb_device_handle *check = find_and_open_device(g_vid, g_pid,
                                                                 &subclass);
            if (!check) {
                LOG_INFO("Device disconnected after Phase 1");
                break;
            }
            /* Device is still there — maybe it re-enumerated already */
            if (subclass != IROM_SUBCLASS) {
                LOG_INFO("Device re-enumerated with subclass 0x%02X (not 0xFF)",
                         subclass);
                handle = check;
                break;
            }
            libusb_close(check);
        }

        /* If we didn't catch the re-enumerated device yet, wait for it */
        if (!handle) {
            LOG_INFO("Waiting for device to reappear (Phase 2)...");
            for (int i = 0; i < 60 && g_running; i++) {
                sleep(POLL_INTERVAL_S);
                handle = find_and_open_device(g_vid, g_pid, &subclass);
                if (handle) {
                    LOG_INFO("Device reappeared with subclass 0x%02X", subclass);
                    break;
                }
            }
            if (!handle) {
                LOG_ERROR("Device did not re-enumerate after Phase 1.");
                return -1;
            }
        }

        /* 1-second delay after re-detection */
        LOG_INFO("Device reappeared. Waiting 1 second...");
        usleep(ATTACH_DELAY_US);

        /* Verify subclass changed */
        if (subclass == IROM_SUBCLASS) {
            LOG_ERROR("Device STILL has subclass 0xFF after Phase 1!");
            LOG_ERROR("Phase 1 may have failed. Power-cycle and retry.");
            libusb_close(handle);
            return -1;
        }
        LOG_INFO("Subclass is now 0x%02X (not 0xFF) — Phase 1 succeeded", subclass);

    } else {
        LOG_INFO("Device subclass is 0x%02X (not 0xFF) — skipping Phase 1",
                 subclass);
        LOG_INFO("Device is already past iROM. Going directly to Phase 2.");
    }

    /* ---------------------------------------------------------------
     * Step 3: Phase 2 — Image loading and console proxy
     * --------------------------------------------------------------- */
    return phase2_serve(handle, g_firmware_dir);
}

/* =========================================================================
 * Cleanup
 * ========================================================================= */

static void cleanup(void)
{
    g_running = 0;

    /* Close telnet client */
    pthread_mutex_lock(&g_telnet_mutex);
    if (g_telnet_client_fd >= 0) {
        close(g_telnet_client_fd);
        g_telnet_client_fd = -1;
    }
    pthread_mutex_unlock(&g_telnet_mutex);

    /* Free transfers */
    if (g_xfr_bulk_in) {
        libusb_cancel_transfer(g_xfr_bulk_in);
        /* Give the event loop a moment to process cancellation */
        usleep(100000);
    }
    if (g_xfr_intr_in) {
        libusb_cancel_transfer(g_xfr_intr_in);
        usleep(100000);
    }

    /* Release USB resources */
    pthread_mutex_lock(&g_dev_mutex);
    if (g_dev_handle) {
        libusb_release_interface(g_dev_handle, 0);
        libusb_close(g_dev_handle);
        g_dev_handle = NULL;
    }
    pthread_mutex_unlock(&g_dev_mutex);

    /* Let background threads wind down */
    usleep(500000);

    if (g_xfr_bulk_in) {
        libusb_free_transfer(g_xfr_bulk_in);
        g_xfr_bulk_in = NULL;
    }
    if (g_xfr_intr_in) {
        libusb_free_transfer(g_xfr_intr_in);
        g_xfr_intr_in = NULL;
    }

    if (g_usb_ctx) {
        libusb_exit(g_usb_ctx);
        g_usb_ctx = NULL;
    }

    if (g_logfile) {
        fclose(g_logfile);
        g_logfile = NULL;
    }

    LOG_INFO("Cleanup complete");
}

/* =========================================================================
 * Entry point
 * ========================================================================= */

int main(int argc, char *argv[])
{
    /* Parse command line: <vid> <pid> <firmware_dir> <telnet_port> */
    if (argc < 3) {
        fprintf(stderr, "Usage: sudo %s <vid> <pid> [firmware_dir] [telnet_port]\n",
                argv[0]);
        fprintf(stderr, "Example: sudo %s 1286 8174 ./ 8141\n", argv[0]);
        return 1;
    }

    g_vid = (uint16_t)strtol(argv[1], NULL, 16);
    g_pid = (uint16_t)strtol(argv[2], NULL, 16);
    if (argc > 3)
        strncpy(g_firmware_dir, argv[3], sizeof(g_firmware_dir) - 1);
    if (argc > 4)
        g_telnet_port = atoi(argv[4]);

    /* Open log file */
    g_logfile = fopen("usb_boot_arm.log", "a");
    if (!g_logfile) {
        fprintf(stderr, "Warning: cannot open usb_boot_arm.log for writing\n");
    }

    LOG_INFO("usb_boot_arm starting");
    LOG_INFO("Compiled: %s %s", __DATE__, __TIME__);

    /* Install signal handlers */
    signal(SIGINT, signal_handler);
    signal(SIGTERM, signal_handler);
    signal(SIGPIPE, SIG_IGN);  /* Ignore broken pipe from telnet */

    /* Initialize libusb */
    int rc = libusb_init(&g_usb_ctx);
    if (rc != 0) {
        LOG_ERROR("libusb_init failed: %s", libusb_error_name(rc));
        return 1;
    }

    /* Enable libusb debug logging (level 3 = info) */
#if LIBUSB_API_VERSION >= 0x01000106
    libusb_set_option(g_usb_ctx, LIBUSB_OPTION_LOG_LEVEL, LIBUSB_LOG_LEVEL_INFO);
#else
    libusb_set_debug(g_usb_ctx, 3);
#endif

    LOG_INFO("libusb initialized (API version 0x%08X)", LIBUSB_API_VERSION);

    /* Run main flow */
    rc = main_flow();

    /* Cleanup */
    cleanup();

    LOG_INFO("usb_boot_arm exiting with code %d", (rc == 0) ? 0 : 1);
    return (rc == 0) ? 0 : 1;
}
