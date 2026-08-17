#ifndef HEXAL_CONCURRENCY_H
#define HEXAL_CONCURRENCY_H

#include "hexal.h"
{{if .Scheduler}}
/* Task, channel, and mutex handle typedefs. */
typedef struct hex_task hex_task;
typedef struct hex_chan hex_chan;
typedef struct hex_mutex_control hex_mutex;
typedef void (*hex_task_entry)(hex_task *task);
struct hex_task {
    hex_task *ready_next;
    hex_task *wait_next;
    int64_t id;
    uint8_t state;
    uint8_t wake_error;
    uint8_t flags;
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
