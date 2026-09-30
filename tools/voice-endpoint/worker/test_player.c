/* Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 *
 * Namespace-only fake player. Consume stdin, then await an explicit test
 * release. It never opens an audio device or changes any audio setting.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

static volatile sig_atomic_t stopped;
static void stop(int number) { (void)number; stopped = 1; }

static void record(const char *path, const char *text)
{
    FILE *file = fopen(path, "w");
    if (!file || fputs(text, file) < 0 || fclose(file)) _exit(90);
}

int main(int argc, char **argv)
{
    struct sigaction action = {0};
    action.sa_handler = access("/test/player-ignore-term", F_OK) == 0 ? SIG_IGN : stop;
    sigemptyset(&action.sa_mask);
    if (sigaction(SIGTERM, &action, NULL)) return 91;
    FILE *arguments = fopen("/test/player-arguments", "w");
    if (!arguments) return 90;
    for (int i = 0; i < argc; i++)
        if (fprintf(arguments, "%s\n", argv[i]) < 0) return 90;
    if (fclose(arguments)) return 90;
    const char *delay = getenv("KWS_TEST_PLAYER_DELAY_US");
    unsigned delay_us = delay ? (unsigned)strtoul(delay, NULL, 10) : 0;
    if (delay_us > 1000000) return 94;
    char text[80];
    snprintf(text, sizeof(text), "%ld\n", (long)getpid());
    record("/test/player-started", text);
    uint32_t fnv = 2166136261u;
    size_t total = 0;
    while (!stopped) {
        while (!stopped && access("/test/player-hold", F_OK) == 0) usleep(5000);
        if (stopped) break;
        uint8_t buffer[4096];
        ssize_t count = read(STDIN_FILENO, buffer, sizeof(buffer));
        if (count < 0 && errno == EINTR) continue;
        if (count < 0) return 92;
        if (!count) break;
        for (ssize_t i = 0; i < count; i++) fnv = (fnv ^ buffer[i]) * 16777619u;
        total += (size_t)count;
        snprintf(text, sizeof(text), "%zu %08x\n", total, fnv);
        record("/test/player-progress", text);
        if (delay_us) usleep(delay_us);
    }
    snprintf(text, sizeof(text), "%zu %08x\n", total, fnv);
    record("/test/player-consumed", text);
    while (!stopped && access("/test/player-release", F_OK)) usleep(10000);
    record(stopped ? "/test/player-stopped" : "/test/player-completed", text);
    return stopped ? 93 : 0;
}
