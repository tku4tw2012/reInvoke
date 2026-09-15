/*
 * Host harness for the exact archived kernel zlib_uncompress function and
 * lib/zlib_inflate sources. Only buffer I/O, locks and allocation are mocked.
 * This is not evidence of a target boot or on-device execution.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <linux/zlib.h>
#include <linux/kernel.h>

#define PAGE_CACHE_SIZE 4096
#define EIO 5
#define ERROR(...) fprintf(stderr, __VA_ARGS__)
struct buffer_head { unsigned char *b_data; int released; };
struct squashfs_sb_info {
    z_stream *stream;
    int read_data_mutex;
    int devblksize;
};
static void mutex_lock(int *mutex) { (void)mutex; }
static void mutex_unlock(int *mutex) { (void)mutex; }
static void wait_on_buffer(struct buffer_head *bh) { (void)bh; }
static int buffer_uptodate(struct buffer_head *bh) { (void)bh; return 1; }
static void put_bh(struct buffer_head *bh) { bh->released++; }

/* Extracted without modification from the caller-specified archived source. */
#include "zlib_uncompress.inc"

static unsigned char *read_file(const char *path, size_t *len)
{
    FILE *f = fopen(path, "rb");
    if (!f || fseek(f, 0, SEEK_END) || (*len = ftell(f)) > 131072 ||
        fseek(f, 0, SEEK_SET)) {
        fprintf(stderr, "cannot read bounded input: %s\n", path);
        exit(1);
    }
    unsigned char *data = calloc(*len + 1, 1);
    if (!data || fread(data, 1, *len, f) != *len || fclose(f))
        exit(1);
    return data;
}

static void check(unsigned char *src, size_t size, unsigned char *expected,
                  size_t expected_size, unsigned long start, int devsize,
                  int expect_success)
{
    int offset = start % devsize;
    int blocks = (offset + size + devsize - 1) / devsize;
    unsigned char *disk = calloc(blocks, devsize);
    struct buffer_head **bh = calloc(blocks, sizeof(*bh));
    unsigned char *out = calloc(32, PAGE_CACHE_SIZE);
    void *pages[32];
    z_stream stream = {0};
    if (!disk || !bh || !out) exit(1);
    memcpy(disk + offset, src, size);
    for (int i = 0; i < blocks; i++) {
        bh[i] = calloc(1, sizeof(**bh));
        if (!bh[i]) exit(1);
        bh[i]->b_data = disk + i * devsize;
    }
    for (int i = 0; i < 32; i++) pages[i] = out + i * PAGE_CACHE_SIZE;
    stream.workspace = calloc(1, zlib_inflate_workspacesize());
    if (!stream.workspace) exit(1);
    struct squashfs_sb_info sb = { &stream, 0, devsize };
    int result = zlib_uncompress(&sb, pages, bh, blocks, offset, size, 131072, 32);
    if (expect_success) {
        if (result != (int)expected_size || stream.total_in != size ||
            stream.avail_in != 0 || memcmp(out, expected, expected_size)) {
            fprintf(stderr, "target-source roundtrip/consumption failed\n");
            exit(1);
        }
        printf("PASS kernel-source wrapper: devblock=%d offset=%d input=%lu output=%d all input consumed\n",
               devsize, offset, (unsigned long)size, result);
    } else if (result != -EIO) {
        fprintf(stderr, "target-source wrapper accepted invalid input\n");
        exit(1);
    } else {
        printf("PASS kernel-source wrapper rejects invalid stream (devblock=%d)\n", devsize);
    }
    for (int i = 0; i < blocks; i++) {
        if (bh[i]->released != 1) {
            fprintf(stderr, "buffer release mismatch\n");
            exit(1);
        }
        free(bh[i]);
    }
    free(stream.workspace);
    free(out);
    free(bh);
    free(disk);
}

int main(int argc, char **argv)
{
    if (argc != 4) {
        fprintf(stderr, "usage: kernel-inflate-check ZLIB EXPANDED FRAGMENT_START\n");
        return 1;
    }
    size_t size, expected_size;
    unsigned char *src = read_file(argv[1], &size);
    unsigned char *expected = read_file(argv[2], &expected_size);
    if (size < 6) return 1;
    unsigned long start = strtoul(argv[3], NULL, 0);
    for (int devsize = 1024; devsize <= 4096; devsize *= 4) {
        check(src, size, expected, expected_size, start, devsize, 1);
        check(src, size + 1, expected, expected_size, start, devsize, 0);
        src[size - 1] ^= 1;
        check(src, size, expected, expected_size, start, devsize, 0);
        src[size - 1] ^= 1;
        check(src, size - 1, expected, expected_size, start, devsize, 0);
    }
    free(src);
    free(expected);
    return 0;
}
