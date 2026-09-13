/* Time runtime: checked Duration arithmetic, Instant differences, and wall
   time through C23 timespec_get{{if .Instant}}; the monotonic clock is libuv's
   uv_hrtime{{end}}. Task sleep lives with the event loop that owns its timer. */
#include "hexal/time.h"
#include <stdckdint.h>
#include <time.h>
{{- if .Instant}}
#include <uv.h>
{{- end}}

hex_duration hex_duration_from(uint64_t value, uint64_t scale) {
    hex_duration result;
    if (ckd_mul(&result, value, scale)) {
        hex_runtime_trap("[Runtime Error] duration overflow\n");
    }
    return result;
}

hex_duration hex_duration_add(hex_duration left, hex_duration right) {
    hex_duration result;
    if (ckd_add(&result, left, right)) {
        hex_runtime_trap("[Runtime Error] duration overflow\n");
    }
    return result;
}

hex_duration hex_duration_sub(hex_duration left, hex_duration right) {
    if (right > left) {
        hex_runtime_trap("[Runtime Error] duration underflow\n");
    }
    return left - right;
}

hex_duration hex_instant_since(hex_instant later, hex_instant earlier) {
    if (later < earlier) {
        hex_runtime_trap("[Runtime Error] invalid instant subtraction\n");
    }
    return later - earlier;
}
{{- if .Instant}}

hex_instant hex_instant_now(void) {
    return uv_hrtime();
}

hex_duration hex_instant_elapsed(hex_instant start) {
    return hex_instant_since(hex_instant_now(), start);
}
{{- end}}

// timespec_get is the exact C23 UTC clock. The conversion is checked: a
// time_t outside Int64 or an unnormalized fraction reports failure rather
// than a wrapped timestamp.
bool hex_wall_time_now(hex_wall_time *out) {
    struct timespec now;
    if (timespec_get(&now, TIME_UTC) != TIME_UTC || now.tv_nsec < 0 || now.tv_nsec > 999999999) {
        return false;
    }
    int64_t seconds;
    if (ckd_add(&seconds, now.tv_sec, 0)) {
        return false;
    }
    out->seconds = seconds;
    out->nanosecond = (uint32_t)now.tv_nsec;
    return true;
}

int hex_wall_time_compare(hex_wall_time left, hex_wall_time right) {
    if (left.seconds != right.seconds) {
        return left.seconds < right.seconds ? -1 : 1;
    }
    if (left.nanosecond != right.nanosecond) {
        return left.nanosecond < right.nanosecond ? -1 : 1;
    }
    return 0;
}
