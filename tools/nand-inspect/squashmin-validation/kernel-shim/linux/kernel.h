/* Userspace-only shim for compiling the unchanged archived inflate algorithm. */
#include <stddef.h>
#include <stdint.h>
#define min(a, b) ((a) < (b) ? (a) : (b))
#define likely(x) (x)
#define unlikely(x) (x)
