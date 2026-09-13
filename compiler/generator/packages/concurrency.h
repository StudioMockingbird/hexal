#ifndef HEXAL_CONCURRENCY_H
#define HEXAL_CONCURRENCY_H

#include "hexal.h"
{{if .Scheduler}}
#if defined(_WIN32)
#include <windows.h>
{{if not .TargetWindows -}}
#else
{{end -}}
#endif
/* Native guards are opaque here; concurrency.c owns their libuv storage. */
typedef struct hex_mutex_raw {
    void *native;
} hex_mutex_raw;
typedef struct hex_cond {
    void *native;
} hex_cond;
/* Task, channel, and mutex handle typedefs. */
typedef struct hex_task hex_task;
typedef struct hex_chan hex_chan;
typedef struct hex_mutex_control hex_mutex;
typedef void (*hex_task_entry)(hex_task *task);
struct hex_task {
    hex_task *ready_next;
    hex_task *wait_next;
    int64_t id;
    // park_phase is the common suspend/wake protocol's atomic state (see
    // hexal/concurrency.c: HEX_PARK_*). life is the completion status,
    // guarded entirely by lifecycle_mutex and independent of park_phase (see
    // HEX_LIFE_*). pending_park is a nullable opaque wait-registration link
    // the dispatcher tests for null but never dereferences; each wait family
    // owns its own interpretation.
    _Atomic(uint8_t) park_phase;
    uint8_t life;
    uint8_t terminal_claim;
    uint8_t wake_result;
    uint8_t flags;
    void *pending_park;
    hex_mutex_raw lifecycle_mutex;
    hex_task *joiner;
    void *fiber;
    void *scheduler_fiber;
    hex_task_entry entry;
    void *args;
    void *result;
};
{{range .Tasks}}typedef hex_task *hex_task_{{.}};
{{end}}{{range .Channels}}typedef hex_chan *hex_channel_{{.}};
{{end}}
{{end}}{{range .Atomics}}typedef _Atomic({{.Element}}) hex_atomic_{{.Suffix}};
{{end}}{{if .Scheduler}}
{{range .SpawnEntries}}void hex_task_entry_{{.}}(hex_task *task);
{{end}}
/* Runtime core entry points, defined in hexal/concurrency.c. */
hex_task *hex_task_spawn(void (*entry)(hex_task *), size_t args_size, size_t args_align, const void *args, size_t result_size, size_t result_align);
void *hex_task_join(hex_task *task);
void hex_task_yield(void);
void hex_task_detach(hex_task *task);
void hex_task_release(hex_task *task);
void hex_task_complete(hex_task *task);
extern hex_task *hex_root_task;
hex_task *hex_task_current(void);
void hex_task_event_arm(hex_task *task, void *pending);
void hex_task_event_cancel(hex_task *task);
void hex_task_event_suspend(hex_task *task);
void hex_task_event_wake(hex_task *task);
void hex_scheduler_init(void);
hex_chan *hex_chan_new(size_t capacity, size_t element_size);
bool hex_chan_send(hex_chan *channel, const void *value);
bool hex_chan_receive(hex_chan *channel, void *out);
void hex_chan_close(hex_chan *channel);
size_t hex_chan_length(hex_chan *channel);
size_t hex_chan_capacity(hex_chan *channel);
bool hex_chan_is_closed(hex_chan *channel);
void hex_chan_free(hex_chan *channel);
hex_mutex *hex_mutex_new(void);
void hex_mutex_lock(hex_mutex *mutex);
void hex_mutex_unlock(hex_mutex *mutex);
void hex_mutex_free(hex_mutex *mutex);
{{end}}
#endif
