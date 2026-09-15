#ifndef HEXAL_EVENT_H
#define HEXAL_EVENT_H

#include "hexal/concurrency.h"

typedef void (*hex_event_work_entry)(void *context);
typedef void (*hex_event_work_failure)(void *context);
void hex_event_runtime_init(void);
void hex_event_work_call(hex_event_work_entry entry, hex_event_work_failure failure, void *context);
// The standard-output critical section. Only a native worker job holds it,
// around one complete write-all or console loop; a scheduler worker has
// already parked by then, so it never blocks here.
void hex_stdout_lock(void);
void hex_stdout_unlock(void);
{{- if .Handle}}

// A component request embeds this intrusive command first. start runs on the
// loop thread, which alone may call libuv request and handle functions; the
// loop handle is opaque here so no libuv name reaches this header.
typedef struct hex_event_command {
    struct hex_event_command *next;
    hex_task *task;
    void (*start)(struct hex_event_command *command);
} hex_event_command;
void hex_event_submit(hex_task *task, hex_event_command *command);
void *hex_event_loop_handle(void);
{{- end}}

#endif
