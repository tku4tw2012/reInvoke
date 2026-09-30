/* Copyright (c) 2026 tku4tw2012
 * SPDX-License-Identifier: MIT
 *
 * Band-limited decimation of the owned microphone stream from 48 to 16 kHz.
 */
#ifndef REINVOKE_RESAMPLER_H
#define REINVOKE_RESAMPLER_H

#include <math.h>
#include <stdint.h>
#include <string.h>

#define RESAMPLER_TAPS 61
#define RESAMPLER_DECIMATION 3

struct resampler {
    double coefficients[RESAMPLER_TAPS];
    double history[RESAMPLER_TAPS];
    unsigned index;
    unsigned phase;
    int shift;
};

static void resampler_init(struct resampler *state, double cutoff_hz,
                           double rate_hz, int shift)
{
    memset(state, 0, sizeof(*state));
    state->shift = shift;
    const double omega = 2.0 * M_PI * cutoff_hz / rate_hz;
    const int middle = RESAMPLER_TAPS / 2;
    double sum = 0.0;
    for (int i = 0; i < RESAMPLER_TAPS; i++) {
        const int n = i - middle;
        const double sinc = n == 0 ? omega : sin(omega * n) / n;
        const double window = 0.54 - 0.46 * cos(2.0 * M_PI * i / (RESAMPLER_TAPS - 1));
        state->coefficients[i] = sinc * window;
        sum += state->coefficients[i];
    }
    for (int i = 0; i < RESAMPLER_TAPS; i++)
        state->coefficients[i] /= sum;
}

static int resampler_push(struct resampler *state, int32_t sample, int16_t *out)
{
    state->history[state->index] = (double)sample;
    double accumulated = 0.0;
    unsigned index = state->index;
    for (int i = 0; i < RESAMPLER_TAPS; i++) {
        accumulated += state->coefficients[i] * state->history[index];
        index = index == 0 ? RESAMPLER_TAPS - 1 : index - 1;
    }
    state->index = (state->index + 1) % RESAMPLER_TAPS;
    if (state->phase++ % RESAMPLER_DECIMATION)
        return 0;
    long scaled = lrint(accumulated / (double)(1L << state->shift));
    if (scaled > 32767) scaled = 32767;
    if (scaled < -32768) scaled = -32768;
    *out = (int16_t)scaled;
    return 1;
}

#endif
