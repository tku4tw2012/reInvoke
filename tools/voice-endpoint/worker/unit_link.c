/* Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 *
 * Isolated donor KWS adapter. The original Cortana application never starts.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <math.h>
#include <poll.h>
#include <signal.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#include "resampler.h"

_Static_assert(sizeof(void *) == 4, "The retained donor uses the ARM32 ABI");

extern int keyword_spotter_open(void **, const char *);
extern void keyword_spotter_setalloc(void *(*)(size_t), void (*)(void *));
extern int keyword_spotter_setcallbacks(void *, const void *, void *);
extern int keyword_spotter_write(void *, const void *, unsigned);
extern void keyword_spotter_close(void *);

enum {
    MAX_QUEUE = 320000, MAX_WAKE = 160000, MAX_AUDIO_FRAME = 3200,
    REPLY_RING = 65536, RECORD = 1048, SESSION_MS = 15000
};
enum phase { WAIT_WAKE, STREAM, THINK, PLAY };
enum input_kind { HEADER, REPLY, DISCARD };
enum wav_kind { WAV_RIFF, WAV_CHUNK, WAV_FMT, WAV_DATA, WAV_SKIP, WAV_PAD };
enum outcome {
    PLAYED, REJECTED, CANCELLED, TIMED_OUT, BAD_REPLY, PLAY_ERROR, SEND_ERROR,
    TRANSPORT_CLOSED, VALIDATED_ONLY
};
struct event {
    uint32_t kind, flags;
    double score;
    int32_t start, end;
    const uint8_t *pcm;
    uint32_t bytes, locale, major, minor, model;
    float threshold;
};
struct callbacks {
    uint32_t version_size;
    void (*state)(void *, int);
    void (*event)(void *, const void *);
};
struct link {
    void *detector;
    const char *model;
    int output, source, live, play, muted, lock, exit_code, exit_after_output;
    int in_detector, reset_detector, audio_ended, end_pending, followup;
    enum phase phase;
    uint32_t nonce, counter, io_ms, audio_sequence;
    uint64_t started, stop_at, session_deadline, mute_due, last_record;
    uint64_t generation, sequence, samples, records, drained, turns, id, announced_id;
    uint64_t stream_bytes, clips, exit_deadline;
    int have_sequence, source_header, fixture_eof, fixture_pace;
    uint64_t fixture_started, fixture_samples;
    uint8_t record[RECORD];
    size_t record_fill;
    int16_t chunk[160];
    unsigned filled;
    struct resampler resampler;

    uint8_t output_queue[MAX_QUEUE];
    size_t output_head, output_fill, frame_left, frame_sent;
    uint64_t output_deadline, frame_id;
    int frame_is_wake;

    uint8_t input_header[24], reply_ring[REPLY_RING], wav_header[18];
    size_t header_fill, input_left, input_size, ring_head, ring_fill, ring_peak, wav_fill;
    enum input_kind input_kind;
    enum wav_kind wav_kind;
    uint64_t input_id, input_deadline;
    uint32_t reply_hash, reply_digest, wav_offset, chunk_left, data_bytes;
    int input_paused, reply_active, reply_complete, fmt_seen, data_seen, chunk_pad;
    pid_t player;
    int player_input;
    size_t played_bytes;
    uint64_t player_deadline;
};

static volatile sig_atomic_t interrupted;
static volatile sig_atomic_t trigger_requested, cancel_requested;
static struct link *running_link;
static void signal_stop(int number) { (void)number; interrupted = 1; }
static void signal_control(int number)
{
    if (number == SIGUSR1) trigger_requested = 1;
    if (number == SIGUSR2) cancel_requested = 1;
}
static void stop_player(struct link *);
static void close_detector(struct link *);
static void shutdown_link(struct link *, int) __attribute__((noreturn));

static uint16_t get16(const uint8_t *p) { return (uint16_t)(p[0] | p[1] << 8); }
static uint32_t get32(const uint8_t *p)
{
    return (uint32_t)p[0] | (uint32_t)p[1] << 8 | (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24;
}
static uint64_t get64(const uint8_t *p) { return get32(p) | (uint64_t)get32(p + 4) << 32; }
static void put16(uint8_t *p, uint16_t v) { p[0] = (uint8_t)v; p[1] = (uint8_t)(v >> 8); }
static void put32(uint8_t *p, uint32_t v)
{
    for (unsigned i = 0; i < 4; i++) p[i] = (uint8_t)(v >> (i * 8));
}
static void put64(uint8_t *p, uint64_t v) { put32(p, (uint32_t)v); put32(p + 4, (uint32_t)(v >> 32)); }
static uint32_t hash_more(uint32_t value, const uint8_t *p, size_t n)
{
    for (size_t i = 0; i < n; i++) value = (value ^ p[i]) * 16777619u;
    return value;
}
static uint64_t now_ms(void)
{
    struct timespec now;
    if (clock_gettime(CLOCK_MONOTONIC, &now)) {
        perror("unit-link clock");
        if (running_link && running_link->player > 0) {
            kill(running_link->player, SIGKILL);
            while (waitpid(running_link->player, NULL, 0) < 0 && errno == EINTR) {}
        }
        fputs("UNIT_LINK PHASE idle\n", stderr);
        _exit(2);
    }
    return (uint64_t)now.tv_sec * 1000 + (uint64_t)now.tv_nsec / 1000000;
}
static void log_message(const char *format, ...)
{
    va_list arguments;
    va_start(arguments, format);
    fputs("UNIT_LINK ", stderr);
    vfprintf(stderr, format, arguments);
    fputc('\n', stderr);
    va_end(arguments);
}
static void feedback(const char *state) { log_message("PHASE %s", state); }
static void fatal(const char *message) __attribute__((noreturn));
static void fatal(const char *message)
{
    log_message("ERROR %s errno=%d", message, errno);
    shutdown_link(running_link, 2);
}
static uint32_t number(const char *name, uint32_t fallback, uint32_t low, uint32_t high)
{
    const char *text = getenv(name);
    if (!text) return fallback;
    if (!*text) fatal("invalid unsigned option");
    for (const char *p = text; *p; p++)
        if (*p < '0' || *p > '9') fatal("invalid unsigned option");
    char *end;
    errno = 0;
    unsigned long value = strtoul(text, &end, 10);
    if (errno || *end || value < low || value > high) fatal(name);
    return (uint32_t)value;
}
static void nonblocking(int fd)
{
    int enabled = 1;
    if (ioctl(fd, FIONBIO, &enabled)) fatal("nonblocking descriptor");
}
static int would_block(void) { return errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR; }

static int microphone_muted(void)
{
    int fd = open("/run/reinvoke/microphone-state", O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK);
    if (fd < 0) { log_message("MUTE authority unreadable errno=%d", errno); return 1; }
    char content[33];
    ssize_t count = read(fd, content, sizeof(content));
    int saved = errno;
    close(fd);
    if (count == 8 && !memcmp(content, "unmuted\n", 8)) return 0;
    if (count == 7 && !memcmp(content, "unmuted", 7)) return 0;
    if ((count == 6 && !memcmp(content, "muted\n", 6)) ||
        (count == 5 && !memcmp(content, "muted", 5))) return 1;
    log_message("MUTE invalid authority bytes=%ld errno=%d", (long)count, saved);
    return 1;
}

static void close_detector(struct link *link)
{
    /* Never destroy the donor wrapper from inside its accepted-event callback. */
    if (link->in_detector) { link->reset_detector = 1; return; }
    if (link->detector) keyword_spotter_close(link->detector);
    link->detector = NULL;
    link->reset_detector = 0;
}

static void stop_player(struct link *link)
{
    if (link->player_input >= 0) close(link->player_input);
    link->player_input = -1;
    if (link->player <= 0) return;
    pid_t child = link->player;
    if (kill(child, SIGTERM) && errno != ESRCH) log_message("PLAYER terminate errno=%d", errno);
    uint64_t deadline = now_ms() + 500;
    int status = 0;
    pid_t result = 0;
    while (result == 0 && now_ms() < deadline) {
        result = waitpid(child, &status, WNOHANG);
        if (!result) usleep(10000);
        if (result < 0 && errno == EINTR) result = 0;
    }
    if (result == 0) {
        if (kill(child, SIGKILL) && errno != ESRCH) log_message("PLAYER force terminate errno=%d", errno);
        do { result = waitpid(child, &status, 0); } while (result < 0 && errno == EINTR);
    }
    if (result < 0) log_message("PLAYER reap error=%d", errno);
    else log_message("PLAYER stopped pid=%ld status=%d", (long)child, status);
    link->player = 0;
}

static size_t queued(const struct link *link) { return link->output_fill - link->output_head; }
static void clear_output(struct link *link)
{
    memset(link->output_queue, 0, sizeof(link->output_queue));
    link->output_head = link->output_fill = link->frame_left = link->frame_sent = 0;
    link->output_deadline = 0;
}
static void queue_frame(struct link *link, const uint8_t *header, size_t header_size,
                        const void *payload, size_t payload_size)
{
    size_t size = header_size + payload_size, pending = queued(link);
    if (size > MAX_QUEUE || pending > MAX_QUEUE - size) {
        log_message("ERROR output backpressure queued=%zu frame=%zu limit=%u", pending, size, MAX_QUEUE);
        shutdown_link(link, 3);
    }
    if (link->output_fill + size > MAX_QUEUE) {
        memmove(link->output_queue, link->output_queue + link->output_head, pending);
        memset(link->output_queue + pending, 0, MAX_QUEUE - pending);
        link->output_head = 0;
        link->output_fill = pending;
    }
    memcpy(link->output_queue + link->output_fill, header, header_size);
    if (payload_size) memcpy(link->output_queue + link->output_fill + header_size, payload, payload_size);
    link->output_fill += size;
    if (!pending) link->output_deadline = now_ms() + link->io_ms;
}
static void receipt(struct link *link, uint64_t id, enum outcome status, uint32_t reply_hash)
{
    uint8_t header[24];
    memcpy(header, "RIRESULT", 8);
    put64(header + 8, id);
    put32(header + 16, status);
    put32(header + 20, reply_hash);
    queue_frame(link, header, sizeof(header), NULL, 0);
    log_message("RESULT id=%llu status=%u reply_fnv=%08x",
                (unsigned long long)id, status, reply_hash);
}
static void audio_frame(struct link *link, const int16_t *samples, size_t bytes)
{
    if (bytes > MAX_AUDIO_FRAME || bytes % 2 || !link->id || link->audio_ended)
        fatal("invalid outgoing audio frame");
    if (link->audio_sequence == UINT32_MAX) fatal("audio sequence exhausted");
    uint8_t header[24];
    memcpy(header, "RIAUDIO3", 8);
    put64(header + 8, link->id);
    put32(header + 16, ++link->audio_sequence);
    put32(header + 20, (uint32_t)bytes);
    queue_frame(link, header, sizeof(header), samples, bytes);
    link->stream_bytes += bytes;
}
static void refresh_session(struct link *link) { link->session_deadline = now_ms() + SESSION_MS; }
static void reset_input(struct link *link)
{
    memset(link->input_header, 0, sizeof(link->input_header));
    link->header_fill = link->input_left = link->input_size = 0;
    link->input_id = link->input_deadline = 0;
    link->input_kind = HEADER;
}
static void discard_old_reply(struct link *link)
{
    if (link->input_kind == REPLY) link->input_kind = DISCARD;
    memset(link->reply_ring, 0, sizeof(link->reply_ring));
    memset(link->wav_header, 0, sizeof(link->wav_header));
    link->ring_head = link->ring_fill = link->played_bytes = link->wav_fill = 0;
    link->ring_peak = 0;
    link->reply_active = link->reply_complete = link->fmt_seen = link->data_seen = 0;
    link->wav_offset = link->chunk_left = link->data_bytes = 0;
    link->wav_kind = WAV_RIFF;
    link->chunk_pad = 0;
    link->player_deadline = 0;
    if (link->input_paused) link->input_deadline = now_ms() + link->io_ms;
    link->input_paused = 0;
}
static void reset_capture(struct link *link)
{
    close_detector(link);
    memset(link->chunk, 0, sizeof(link->chunk));
    link->filled = 0;
    resampler_init(&link->resampler, 7000.0, 48000.0, 16);
}
static void fail_session(struct link *link, enum outcome status, const char *reason)
{
    if (link->exit_after_output) return;
    log_message("ERROR %s", reason);
    stop_player(link);
    reset_capture(link);
    discard_old_reply(link);
    int partial = link->frame_sent != 0;
    clear_output(link);
    link->exit_code = 3;
    link->followup = 0;
    if (partial) {
        log_message("ERROR partial outgoing frame; closing session without splicing a receipt");
        interrupted = 1;
    } else if (link->id && link->announced_id == link->id) {
        receipt(link, link->id, status, link->reply_hash);
        link->exit_after_output = 1;
        link->exit_deadline = now_ms() + link->io_ms;
    } else {
        interrupted = 1;
    }
}

static void begin_turn(struct link *, const struct event *, uint32_t, uint32_t, unsigned);
static void end_audio(struct link *link, const char *reason)
{
    if (link->audio_ended) return;
    if (link->filled) {
        link->samples += link->filled;
        audio_frame(link, link->chunk, link->filled * 2);
        memset(link->chunk, 0, sizeof(link->chunk));
        link->filled = 0;
    }
    audio_frame(link, NULL, 0);
    link->audio_ended = 1;
    link->phase = THINK;
    close_detector(link);
    feedback("thinking");
    log_message("AUDIO_END id=%llu sequence=%u bytes=%llu reason=%s KWS=enabled",
                (unsigned long long)link->id, link->audio_sequence,
                (unsigned long long)link->stream_bytes, reason);
}
static void finish_turn(struct link *link)
{
    uint64_t old = link->id;
    int followup = link->followup && !link->muted;
    link->turns++;
    log_message("TURN_END id=%llu clips=%llu followup=%d",
                (unsigned long long)old, (unsigned long long)link->clips, followup);
    link->id = 0;
    link->end_pending = link->followup = 0;
    link->phase = WAIT_WAKE;
    if (followup) {
        begin_turn(link, NULL, 0, 0, (link->live ? 0 : 1) | 4);
    } else {
        feedback("idle");
    }
}
static void cancel_turn(struct link *link, const char *reason)
{
    if (!link->id) return;
    log_message("CANCEL id=%llu reason=%s", (unsigned long long)link->id, reason);
    if (!link->audio_ended) end_audio(link, "cancel");
    stop_player(link);
    discard_old_reply(link);
    close_detector(link);
    receipt(link, link->id, CANCELLED, link->reply_hash);
    link->followup = 0;
    finish_turn(link);
}
static void begin_turn(struct link *link, const struct event *event,
                       uint32_t start_ms, uint32_t duration_ms, unsigned flags)
{
    if (link->id) cancel_turn(link, "superseded-wake");
    if (link->counter == UINT32_MAX) fatal("turn counter exhausted");
    link->id = (uint64_t)link->nonce << 32 | ++link->counter;
    link->audio_sequence = 0;
    link->stream_bytes = link->clips = 0;
    link->reply_hash = 0;
    link->audio_ended = link->end_pending = link->followup = 0;
    link->phase = STREAM;
    refresh_session(link);
    uint8_t header[64] = {0};
    memcpy(header, "RIWAKE03", 8);
    put16(header + 8, 3);
    put16(header + 10, (uint16_t)flags);
    put64(header + 12, link->id);
    put64(header + 20, now_ms());
    if (event) {
        memcpy(header + 28, &event->score, 8);
        memcpy(header + 36, &event->threshold, 4);
        put32(header + 40, start_ms);
        put32(header + 44, duration_ms);
        put32(header + 56, event->bytes);
        put32(header + 60, event->bytes);
    }
    put32(header + 48, 16000);
    put16(header + 52, 1);
    put16(header + 54, 1);
    queue_frame(link, header, sizeof(header), event ? event->pcm : NULL, event ? event->bytes : 0);
    close_detector(link);
    feedback("listening");
    log_message("TURN id=%llu sample=%llu score=%.9f wake_bytes=%u followup=%d",
                (unsigned long long)link->id, (unsigned long long)link->samples,
                event ? event->score : 0.0, event ? event->bytes : 0, !!(flags & 4));
    if (link->fixture_eof) end_audio(link, "fixture-eof");
}

static void on_state(void *user, int value) { (void)user; log_message("DONOR state=%d", value); }
static void on_event(void *user, const void *raw)
{
    struct link *link = user;
    struct event event;
    memcpy(&event, raw, sizeof(event));
    if (event.kind != 1) return;
    if (link->phase == STREAM || link->muted) fatal("unexpected donor acceptance state");
    if (!event.pcm || !event.bytes || event.bytes > MAX_WAKE || event.bytes % 2 ||
        !isfinite(event.score) || event.score < 0 || event.score > 1 ||
        !isfinite(event.threshold) || event.threshold < 0 || event.threshold > 1)
        fatal("invalid donor candidate");
    int64_t first = (int64_t)event.bytes / 2 + event.start;
    int64_t last = (int64_t)event.bytes / 2 + event.end;
    unsigned flags = link->live ? 0 : 1;
    if (first < 0) {
        first = 0;
        flags |= 2;
        log_message("TURN keyword start clipped to retained window");
    }
    if (last <= first || last > (int64_t)event.bytes / 2) fatal("invalid keyword offsets");
    begin_turn(link, &event, (uint32_t)(first / 16), (uint32_t)((last - first) / 16), flags);
}
static void open_detector(struct link *link)
{
    close_detector(link);
    if (keyword_spotter_open(&link->detector, link->model) || !link->detector) fatal("donor model open");
    /* The donor retains this table rather than copying it. */
    static const struct callbacks callbacks = {0x1000c, on_state, on_event};
    if (keyword_spotter_setcallbacks(link->detector, &callbacks, link)) fatal("donor callbacks");
    log_message("KWS rearmed phase=%u", link->phase);
}
static void feed_chunk(struct link *link, const int16_t *samples, unsigned count)
{
    link->samples += count;
    if (link->muted) { link->drained += count; return; }
    if (link->phase == STREAM) {
        audio_frame(link, samples, count * 2);
        return;
    }
    if (!link->detector) open_detector(link);
    link->in_detector = 1;
    int result = keyword_spotter_write(link->detector, samples, count * 2);
    link->in_detector = 0;
    if (link->reset_detector) close_detector(link);
    if (result) fatal("donor write");
}

static void clip_complete(struct link *link, enum outcome status)
{
    log_message("REPLY complete pcm_bytes=%u ring_peak=%zu limit=%u",
                link->data_bytes, link->ring_peak, REPLY_RING);
    receipt(link, link->id, status, link->reply_hash);
    link->clips++;
    discard_old_reply(link);
    link->phase = THINK;
    refresh_session(link);
    if (link->end_pending) finish_turn(link);
    else feedback("thinking");
}
static void start_player(struct link *link)
{
    if (!link->play) return;
    int pipefd[2];
    if (pipe2(pipefd, O_CLOEXEC)) { fail_session(link, PLAY_ERROR, "player pipe"); return; }
    pid_t child = fork();
    if (child < 0) {
        close(pipefd[0]); close(pipefd[1]); fail_session(link, PLAY_ERROR, "player fork"); return;
    }
    if (child == 0) {
        if (dup2(pipefd[0], STDIN_FILENO) < 0) _exit(125);
        close(pipefd[0]); close(pipefd[1]);
        close(link->output); close(link->source); close(link->lock);
        if (unsetenv("LD_PRELOAD") || unsetenv("LD_LIBRARY_PATH")) _exit(125);
        execl("/opt/reinvoke/lib/ld-linux-armhf.so.3", "ld-linux-armhf.so.3",
              "--library-path", "/opt/reinvoke/lib", "/opt/reinvoke/bin/aplay",
              "-D", "voice", "-q", "-t", "raw", "-f", "S16_LE", "-r", "16000",
              "-c", "1", "-", (char *)NULL);
        perror("unit-link player exec"); _exit(126);
    }
    close(pipefd[0]);
    link->player = child;
    link->player_input = pipefd[1];
    nonblocking(pipefd[1]);
    link->played_bytes = 0;
    link->phase = PLAY;
    link->player_deadline = now_ms() + link->io_ms;
    feedback("speaking");
    log_message("PLAYER pid=%ld bytes=%u ring=%u", (long)child, link->data_bytes, REPLY_RING);
}
static void poll_player(struct link *link)
{
    if (!link->player) return;
    int status = 0;
    pid_t result = waitpid(link->player, &status, WNOHANG);
    if (result < 0 && errno != EINTR) {
        fail_session(link, PLAY_ERROR, "player wait");
    } else if (result > 0) {
        link->player = 0;
        if (link->player_input >= 0) close(link->player_input);
        link->player_input = -1;
        int ok = WIFEXITED(status) && WEXITSTATUS(status) == 0 &&
            link->reply_complete && link->played_bytes == link->data_bytes && !link->ring_fill;
        log_message("PLAYER exit=%d bytes=%zu", status, link->played_bytes);
        if (ok) clip_complete(link, PLAYED);
        else fail_session(link, PLAY_ERROR, "player failed or incomplete");
    } else if ((link->ring_fill || link->player_input < 0) && now_ms() >= link->player_deadline) {
        fail_session(link, TIMED_OUT, "player progress deadline");
    }
}
static void player_eof(struct link *link)
{
    if (link->player_input < 0 || !link->reply_complete || link->ring_fill) return;
    close(link->player_input);
    link->player_input = -1;
    /* Once EOF is delivered, only the pipe/ALSA tail remains, not the whole
     * answer. Before EOF every successful write renews the stall allowance. */
    link->player_deadline = now_ms() + link->io_ms + 5000;
}
static void write_player(struct link *link)
{
    if (!link->ring_fill) return;
    size_t remaining = REPLY_RING - link->ring_head;
    if (remaining > link->ring_fill) remaining = link->ring_fill;
    ssize_t count = write(link->player_input, link->reply_ring + link->ring_head,
                         remaining < 4096 ? remaining : 4096);
    if (count < 0 && !would_block()) {
        fail_session(link, PLAY_ERROR, "player pipe write");
    } else if (count > 0) {
        memset(link->reply_ring + link->ring_head, 0, (size_t)count);
        link->ring_head = (link->ring_head + (size_t)count) % REPLY_RING;
        link->ring_fill -= (size_t)count;
        link->played_bytes += (size_t)count;
        link->player_deadline = now_ms() + link->io_ms;
        refresh_session(link);
        player_eof(link);
    }
}

static void next_chunk(struct link *link)
{
    link->wav_kind = link->chunk_pad ? WAV_PAD : WAV_CHUNK;
    link->wav_fill = 0;
}
static int wave_bytes(struct link *link, const uint8_t *data, size_t length)
{
    while (length) {
        if (link->wav_kind == WAV_RIFF || link->wav_kind == WAV_CHUNK || link->wav_kind == WAV_FMT) {
            size_t goal = link->wav_kind == WAV_RIFF ? 12 :
                link->wav_kind == WAV_CHUNK ? 8 : link->chunk_left;
            size_t count = goal - link->wav_fill;
            if (count > length) count = length;
            memcpy(link->wav_header + link->wav_fill, data, count);
            link->wav_fill += count;
            link->wav_offset += count;
            data += count;
            length -= count;
            if (link->wav_fill != goal) continue;
            const uint8_t *header = link->wav_header;
            if (link->wav_kind == WAV_RIFF) {
                if (memcmp(header, "RIFF", 4) || memcmp(header + 8, "WAVE", 4) ||
                    (uint64_t)get32(header + 4) + 8 != link->input_size) return -1;
                link->wav_kind = WAV_CHUNK;
            } else if (link->wav_kind == WAV_FMT) {
                if (get16(header) != 1 || get16(header + 2) != 1 || get32(header + 4) != 16000 ||
                    get32(header + 8) != 32000 || get16(header + 12) != 2 || get16(header + 14) != 16 ||
                    (goal == 18 && get16(header + 16))) return -1;
                link->fmt_seen = 1;
                link->chunk_left = 0;
                next_chunk(link);
            } else {
                uint32_t size = get32(header + 4);
                if ((uint64_t)size + (size & 1u) > link->input_size - link->wav_offset) return -1;
                link->chunk_left = size;
                link->chunk_pad = size & 1u;
                if (!memcmp(header, "fmt ", 4)) {
                    if (link->fmt_seen || (size != 16 && size != 18)) return -1;
                    link->wav_kind = WAV_FMT;
                } else if (!memcmp(header, "data", 4)) {
                    if (!link->fmt_seen || link->data_seen || !size || size % 2) return -1;
                    link->data_seen = 1;
                    link->data_bytes = size;
                    link->wav_kind = WAV_DATA;
                    start_player(link);
                    if (link->exit_after_output || interrupted) return -1;
                } else {
                    link->wav_kind = WAV_SKIP;
                }
                if (!size) next_chunk(link);
            }
            link->wav_fill = 0;
        } else if (link->wav_kind == WAV_PAD) {
            link->wav_offset++;
            data++;
            length--;
            link->wav_kind = WAV_CHUNK;
        } else {
            size_t count = length < link->chunk_left ? length : link->chunk_left;
            if (link->wav_kind == WAV_DATA && link->play) {
                if (count > REPLY_RING - link->ring_fill) return -1;
                size_t tail = (link->ring_head + link->ring_fill) % REPLY_RING;
                size_t first = count < REPLY_RING - tail ? count : REPLY_RING - tail;
                memcpy(link->reply_ring + tail, data, first);
                memcpy(link->reply_ring, data + first, count - first);
                if (!link->ring_fill) link->player_deadline = now_ms() + link->io_ms;
                link->ring_fill += count;
                if (link->ring_fill > link->ring_peak) {
                    link->ring_peak = link->ring_fill;
                    if (link->ring_peak == REPLY_RING) log_message("REPLY ring full; input paused");
                }
            }
            link->chunk_left -= count;
            link->wav_offset += count;
            data += count;
            length -= count;
            if (!link->chunk_left) next_chunk(link);
        }
    }
    return 0;
}

/* Only issued counters in this connection can be stale. No ID history grows. */
static int input_identity(const struct link *link, uint64_t id)
{
    uint32_t counter = (uint32_t)id;
    if (id >> 32 != link->nonce || !counter || counter > link->counter) return -1;
    return link->id == id;
}
static void control(struct link *link, uint32_t code)
{
    refresh_session(link);
    log_message("CONTROL id=%llu code=%u", (unsigned long long)link->id, code);
    switch (code) {
    case 1: break;
    case 2: end_audio(link, "speech.endDetected"); break;
    case 3:
        link->end_pending = 1;
        end_audio(link, "turn.end");
        /* A turn may have no audio response. Do not invent a PLAYED receipt. */
        if (!link->reply_active && !link->player) finish_turn(link);
        break;
    case 4: cancel_turn(link, "backend"); break;
    case 5: link->followup = 1; break;
    case 6: break;
    }
}
static void input_header(struct link *link)
{
    uint8_t *header = link->input_header;
    uint64_t id = get64(header + 8);
    uint32_t value = get32(header + 16), length = get32(header + 20);
    int identity = input_identity(link, id);
    if (identity < 0) { fail_session(link, BAD_REPLY, "foreign or future turn ID"); return; }
    if (!memcmp(header, "RICTRL03", 8)) {
        if (length || value < 1 || value > 6) {
            fail_session(link, BAD_REPLY, "invalid control frame"); return;
        }
        reset_input(link);
        if (identity) control(link, value);
        else log_message("STALE control id=%llu ignored", (unsigned long long)id);
        return;
    }
    if (memcmp(header, "RIREPLY3", 8) || value > 2 ||
        (value && length) || (!value && (length < 46 || length % 2))) {
        fail_session(link, BAD_REPLY, "invalid reply header"); return;
    }
    if (!identity) {
        log_message("STALE reply id=%llu bytes=%u discarded", (unsigned long long)id, length);
        if (!length) { reset_input(link); return; }
        link->input_kind = DISCARD;
    } else {
        if (!link->audio_ended || link->reply_active || link->player) {
            fail_session(link, BAD_REPLY, "reply before audio end or previous clip completion"); return;
        }
        link->reply_hash = 0;
        refresh_session(link);
        if (value) {
            reset_input(link);
            if (value == 1) clip_complete(link, REJECTED);
            else cancel_turn(link, "reply-verdict");
            return;
        }
        discard_old_reply(link);
        link->reply_active = 1;
        link->reply_digest = 2166136261u;
        link->input_kind = REPLY;
    }
    link->input_id = id;
    link->input_size = link->input_left = length;
}
static void read_input(struct link *link)
{
    uint8_t buffer[4096];
    uint8_t *target;
    size_t wanted;
    if (link->input_kind == HEADER) {
        target = link->input_header + link->header_fill;
        wanted = sizeof(link->input_header) - link->header_fill;
    } else {
        wanted = link->input_left < sizeof(buffer) ? link->input_left : sizeof(buffer);
        if (link->input_kind == REPLY && link->play && wanted > REPLY_RING - link->ring_fill)
            wanted = REPLY_RING - link->ring_fill;
        if (!wanted) return;
        target = buffer;
    }
    ssize_t count = read(STDIN_FILENO, target, wanted);
    if (count < 0 && would_block()) return;
    if (count <= 0) { fail_session(link, TRANSPORT_CLOSED, "host transport closed"); return; }
    link->input_deadline = now_ms() + link->io_ms;
    if (link->input_kind == HEADER) {
        link->header_fill += (size_t)count;
        if (link->header_fill == sizeof(link->input_header)) input_header(link);
        return;
    }
    link->input_left -= (size_t)count;
    if (link->input_kind == REPLY) {
        refresh_session(link);
        link->reply_digest = hash_more(link->reply_digest, buffer, (size_t)count);
        int invalid = wave_bytes(link, buffer, (size_t)count);
        memset(buffer, 0, sizeof(buffer));
        if (invalid) { fail_session(link, BAD_REPLY, "reply RIFF/PCM format or chunk size"); return; }
    }
    if (link->input_left) return;
    int current = link->input_kind == REPLY && link->input_id == link->id;
    size_t length = link->input_size;
    reset_input(link);
    if (!current) return;
    if (!link->fmt_seen || !link->data_seen || link->wav_offset != length ||
        link->wav_kind != WAV_CHUNK || link->wav_fill) {
        fail_session(link, BAD_REPLY, "incomplete reply RIFF chunks"); return;
    }
    link->reply_complete = 1;
    link->reply_hash = link->reply_digest;
    log_message("REPLY validated bytes=%zu fnv=%08x", length, link->reply_hash);
    if (!link->play) clip_complete(link, VALIDATED_ONLY);
    else player_eof(link);
}

static void write_output(struct link *link)
{
    if (!link->frame_left) {
        const uint8_t *header = link->output_queue + link->output_head;
        link->frame_is_wake = !memcmp(header, "RIWAKE03", 8);
        link->frame_id = get64(header + (link->frame_is_wake ? 12 : 8));
        if (link->frame_is_wake) link->frame_left = 64 + get32(header + 56);
        else if (!memcmp(header, "RIAUDIO3", 8)) link->frame_left = 24 + get32(header + 20);
        else if (!memcmp(header, "RIRESULT", 8)) link->frame_left = 24;
        else fatal("output queue framing");
        if (link->frame_left > queued(link)) fatal("output queue incomplete frame");
    }
    size_t wanted = link->frame_left < 4096 ? link->frame_left : 4096;
    ssize_t count = write(link->output, link->output_queue + link->output_head, wanted);
    if (count < 0 && would_block()) return;
    if (count <= 0) {
        log_message("ERROR output transport closed errno=%d", errno);
        link->exit_code = 3; interrupted = 1; return;
    }
    memset(link->output_queue + link->output_head, 0, (size_t)count);
    link->output_head += (size_t)count;
    link->frame_left -= (size_t)count;
    link->frame_sent += (size_t)count;
    link->output_deadline = now_ms() + link->io_ms;
    if (!link->frame_left) {
        if (link->frame_is_wake) link->announced_id = link->frame_id;
        link->frame_sent = 0;
    }
    if (!queued(link)) {
        link->output_head = link->output_fill = 0;
        link->output_deadline = 0;
    }
}

static void source_read(struct link *link)
{
    if (!link->live) {
        int16_t samples[160];
        ssize_t count = read(link->source, samples, sizeof(samples));
        if (count < 0 && would_block()) return;
        if (count < 0 || count % 2) fatal("fixture read");
        if (!count) {
            link->fixture_eof = 1;
            if (link->id && !link->audio_ended) end_audio(link, "fixture-eof");
            log_message("INPUT fixture-eof");
            return;
        }
        link->fixture_samples += (unsigned)count / 2;
        feed_chunk(link, samples, (unsigned)count / 2);
        return;
    }
    size_t goal = link->source_header ? RECORD : 32;
    ssize_t count = read(link->source, link->record + link->record_fill, goal - link->record_fill);
    if (count < 0 && would_block()) return;
    if (count <= 0) { fail_session(link, CANCELLED, "capture socket closed"); return; }
    link->record_fill += (size_t)count;
    if (link->record_fill != goal) return;
    link->record_fill = 0;
    const uint8_t *record = link->record;
    if (!link->source_header) {
        if (memcmp(record, "RINVOMIC", 8) || get16(record + 8) != 1 || get16(record + 10) != 32 ||
            get32(record + 12) != 48000 || get16(record + 16) != 1 ||
            get16(record + 18) != 1 || get32(record + 20) != 256)
            fatal("capture stream header");
        link->generation = get64(record + 24);
        link->source_header = 1;
        link->last_record = now_ms();
        log_message("READY source=socket generation=%llu", (unsigned long long)link->generation);
        return;
    }
    uint64_t generation = get64(record), sequence = get64(record + 8);
    if (generation != link->generation ||
        (link->have_sequence && (link->sequence == UINT64_MAX || sequence != link->sequence + 1))) {
        fail_session(link, CANCELLED, "capture generation or sequence gap"); return;
    }
    link->generation = generation;
    link->sequence = sequence;
    link->have_sequence = 1;
    link->last_record = now_ms();
    link->records++;
    for (unsigned i = 0; i < 256; i++) {
        int16_t converted;
        if (!resampler_push(&link->resampler, (int32_t)get32(record + 24 + 4 * i), &converted)) continue;
        link->chunk[link->filled++] = converted;
        if (link->filled == 160) {
            link->filled = 0;
            feed_chunk(link, link->chunk, 160);
        }
    }
}
static void handle_mute(struct link *link)
{
    int muted = microphone_muted();
    if (muted == link->muted) return;
    link->muted = muted;
    link->last_record = now_ms();
    log_message("MUTE state=%s", muted ? "muted" : "unmuted");
    reset_capture(link);
    if (muted) {
        feedback("idle");
        if (link->id) fail_session(link, CANCELLED, "microphone muted during turn");
    }
}

static void local_controls(struct link *link)
{
    if (cancel_requested) {
        cancel_requested = trigger_requested = 0;
        log_message("LOCAL cancel");
        cancel_turn(link, "local-cancel");
        reset_capture(link);
    } else if (trigger_requested) {
        trigger_requested = 0;
        handle_mute(link);
        if (link->muted) {
            log_message("LOCAL trigger ignored microphone muted");
        } else if (!link->exit_after_output) {
            log_message("LOCAL trigger");
            cancel_turn(link, "local-trigger");
            reset_capture(link);
            begin_turn(link, NULL, 0, 0, (link->live ? 0 : 1) | 4);
        }
    }
}

static int loop(struct link *link)
{
    while (!interrupted) {
        uint64_t now = now_ms();
        if (now >= link->mute_due) {
            handle_mute(link);
            link->mute_due = now + 50;
        }
        if (interrupted) break;
        if (!link->exit_after_output) local_controls(link);
        int paused = link->input_kind == REPLY && link->play && link->ring_fill == REPLY_RING;
        /* Time spent behind our player is not a backend input stall. On
         * resuming reads the sender gets a fresh per-progress allowance. */
        if (link->input_paused && !paused) link->input_deadline = now_ms() + link->io_ms;
        link->input_paused = paused;
        if (link->exit_after_output) {
            if (!queued(link)) return link->exit_code;
            if (now >= link->exit_deadline) {
                log_message("ERROR failure receipt transport deadline");
                return 3;
            }
        } else {
            if (now >= link->stop_at) {
                if (link->id || queued(link)) log_message("STOP active turn interrupted by run limit");
                return link->id || queued(link) ? 3 : 0;
            }
            if (link->live && !link->source_header && now - link->started > link->io_ms)
                fail_session(link, CANCELLED, "capture header deadline");
            else if (link->live && link->source_header && !link->muted && now - link->last_record > 2000)
                fail_session(link, CANCELLED, "capture stall");
            else if (!paused && link->input_deadline && now >= link->input_deadline)
                fail_session(link, TIMED_OUT, "input frame transport deadline");
            else if (link->output_deadline && now >= link->output_deadline)
                fail_session(link, TIMED_OUT, "output frame transport deadline");
            /* Reply input and player progress have their own stall clocks. */
            else if (link->id && !link->reply_active && !link->player &&
                     now >= link->session_deadline)
                fail_session(link, TIMED_OUT, "session liveness deadline");
            if (interrupted || link->exit_after_output) continue;
            poll_player(link);
            if (link->exit_after_output) continue;
            if (link->fixture_eof && !link->id && !queued(link) &&
                link->input_kind == HEADER && !link->header_fill) return 0;
        }
        int source_fd = link->exit_after_output || link->fixture_eof ? -1 : link->source;
        int poll_ms = 20;
        if (source_fd >= 0 && !link->live && link->fixture_pace) {
            uint64_t due = link->fixture_started + (link->fixture_samples + 15) / 16;
            now = now_ms();
            if (now < due) {
                /* A regular file is always ready. Remove it while waiting,
                 * rather than sleeping the backend control/player loop. */
                source_fd = -1;
                if (due - now < (uint64_t)poll_ms) poll_ms = (int)(due - now);
            }
        }
        struct pollfd pollfd[4] = {
            {link->exit_after_output || paused ? -1 : STDIN_FILENO, POLLIN, 0},
            {link->output, queued(link) ? POLLOUT : 0, 0},
            {source_fd, source_fd >= 0 ? POLLIN : 0, 0},
            {link->player_input, link->ring_fill ? POLLOUT : 0, 0},
        };
        int polled = poll(pollfd, 4, poll_ms);
        if (polled < 0) { if (errno == EINTR) continue; fatal("poll"); }
        if (pollfd[0].revents & (POLLIN | POLLHUP)) read_input(link);
        if (interrupted) break;
        if (!link->exit_after_output && (pollfd[2].revents & (POLLIN | POLLHUP))) source_read(link);
        if (interrupted) break;
        if ((pollfd[1].revents & POLLOUT) && queued(link)) write_output(link);
        if (!link->exit_after_output && link->player_input >= 0 && (pollfd[3].revents & POLLOUT))
            write_player(link);
        if (pollfd[0].revents & (POLLERR | POLLNVAL))
            fail_session(link, TRANSPORT_CLOSED, "host descriptor failed");
        if (pollfd[1].revents & (POLLERR | POLLNVAL | POLLHUP)) {
            log_message("ERROR output descriptor failed");
            link->exit_code = 3; interrupted = 1;
        }
        if (pollfd[2].revents & (POLLERR | POLLNVAL))
            fail_session(link, CANCELLED, "capture descriptor failed");
        if (link->player_input >= 0 && (pollfd[3].revents & (POLLERR | POLLNVAL | POLLHUP)))
            fail_session(link, PLAY_ERROR, "player descriptor failed");
    }
    return link->exit_code ? link->exit_code : 130;
}

static void shutdown_link(struct link *link, int status)
{
    if (link) {
        stop_player(link);
        close_detector(link);
        log_message("DONE status=%d source=%s turns=%llu records=%llu samples=%llu drained=%llu",
                    status, link->live ? "socket" : "fixture",
                    (unsigned long long)link->turns, (unsigned long long)link->records,
                    (unsigned long long)link->samples, (unsigned long long)link->drained);
        if (link->source >= 0) close(link->source);
        if (link->output >= 0) close(link->output);
        if (link->lock >= 0) close(link->lock);
        memset(link, 0, sizeof(*link));
        free(link);
    }
    feedback("idle");
    _exit(status);
}

__attribute__((constructor))
static void run(void)
{
    struct link *link = calloc(1, sizeof(*link));
    if (!link) fatal("allocation");
    running_link = link;
    link->output = link->source = link->lock = link->player_input = -1;
    link->output = dup(STDOUT_FILENO);
    if (link->output < 0 || dup2(STDERR_FILENO, STDOUT_FILENO) < 0) fatal("protocol fd");
    /* Keep donor printf diagnostics out of the binary protocol stream. */
    setvbuf(stdout, NULL, _IONBF, 0);
    setvbuf(stderr, NULL, _IONBF, 0);
    signal(SIGPIPE, SIG_IGN);
    signal(SIGTERM, signal_stop);
    signal(SIGINT, signal_stop);
    signal(SIGUSR1, signal_control);
    signal(SIGUSR2, signal_control);
    log_message("LOCAL signals ready");
    const char *lock_path = getenv("KWS_UNIT_LOCK");
    if (!lock_path) lock_path = "/tmp/spotter/unit-link.lock";
    link->lock = open(lock_path, O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0600);
    if (link->lock < 0 || flock(link->lock, LOCK_EX | LOCK_NB)) fatal("another voice worker is active");
    link->model = getenv("KWS_MODEL");
    if (!link->model) fatal("KWS_MODEL required");
    const char *source = getenv("KWS_UNIT_SOURCE");
    if (!source || (strcmp(source, "socket") && strcmp(source, "fixture"))) fatal("source required");
    link->live = !strcmp(source, "socket");
    link->nonce = number("KWS_UNIT_NONCE", 0, 1, UINT32_MAX);
    if (!link->nonce) fatal("nonzero session nonce required");
    link->io_ms = number("KWS_UNIT_IO_MS", 30000, 200, 180000);
    link->play = number("KWS_UNIT_PLAY", 0, 0, 1);
    link->fixture_pace = number("KWS_UNIT_FIXTURE_PACE", 0, 0, 1);
    link->muted = 1;
    link->started = link->last_record = now_ms();
    uint32_t seconds = number("KWS_UNIT_SECONDS", 0, 0, 86400);
    link->stop_at = seconds ? link->started + seconds * 1000ULL : UINT64_MAX;
    resampler_init(&link->resampler, 7000.0, 48000.0, 16);
    if (link->live) {
        const char *path = getenv("KWS_UNIT_SOCKET");
        if (!path) path = "/run/reinvoke/mic-capture/audio.sock";
        struct sockaddr_un address = {0};
        if (strlen(path) >= sizeof(address.sun_path)) fatal("capture socket path too long");
        address.sun_family = AF_UNIX;
        strcpy(address.sun_path, path);
        link->source = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
        if (link->source < 0 || connect(link->source, (struct sockaddr *)&address, sizeof(address)))
            fatal("capture connect");
    } else {
        const char *path = getenv("KWS_UNIT_PCM");
        if (!path || (link->source = open(path, O_RDONLY | O_NONBLOCK | O_CLOEXEC | O_NOFOLLOW)) < 0)
            fatal("PCM fixture open");
        off_t length = lseek(link->source, 0, SEEK_END);
        if (length <= 0 || length > 16000000 || length % 2 || lseek(link->source, 0, SEEK_SET) != 0)
            fatal("invalid bounded PCM fixture");
        log_message("READY source=fixture bytes=%lld pace=%d", (long long)length, link->fixture_pace);
    }
    nonblocking(STDIN_FILENO);
    nonblocking(link->output);
    nonblocking(link->source);
    keyword_spotter_setalloc(malloc, free);
    handle_mute(link);
    link->fixture_started = now_ms();
    shutdown_link(link, loop(link));
}
