#ifndef HEXAL_TIME_H
#define HEXAL_TIME_H

#include "hexal.h"

typedef uint64_t hex_duration;
typedef uint64_t hex_instant;
typedef struct hex_wall_time {
    int64_t seconds;
    uint32_t nanosecond;
} hex_wall_time;

hex_duration hex_duration_from(uint64_t value, uint64_t scale);
hex_duration hex_duration_add(hex_duration left, hex_duration right);
hex_duration hex_duration_sub(hex_duration left, hex_duration right);
hex_duration hex_instant_since(hex_instant later, hex_instant earlier);
{{- if .Instant}}
hex_instant hex_instant_now(void);
hex_duration hex_instant_elapsed(hex_instant start);
{{- end}}
bool hex_wall_time_now(hex_wall_time *out);
int hex_wall_time_compare(hex_wall_time left, hex_wall_time right);
{{- if .Sleep}}
void hex_task_sleep(hex_duration duration);
{{- end}}

#endif
