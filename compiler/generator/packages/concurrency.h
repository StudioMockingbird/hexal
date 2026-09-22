#ifndef HEXAL_CONCURRENCY_H
#define HEXAL_CONCURRENCY_H

#include "hexal.h"
{{if .Scheduler}}
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
{{define "sched_error_helper"}}
static inline hex_t_Error hex_sched_error(hex_t_ErrorKind kind, size_t line, size_t column, const hex_string *message) {
    return (hex_t_Error){
        .hex_m_file = &{{.FileCName}},
        .hex_m_line = line,
        .hex_m_column = column,
        .hex_m_kind = kind,
        .hex_m_message = hex_error_message(hex_text_heap(message)),
    };
}
{{end}}{{define "task_join_comment"}}
// join copies R out of the result frame and reclaims the task storage.
{{end}}{{define "task_join_void"}}static inline void hex_task_join_{{.Suffix}}(hex_task *task) {
    (void)hex_task_join(task);
    hex_task_release(task);
}
{{end}}{{define "task_join_value"}}static inline {{.ResultSpelling}} hex_task_join_{{.Suffix}}(hex_task *task) {
    {{.ResultSpelling}} *frame = ({{.ResultSpelling}} *)hex_task_join(task);
    {{.ResultSpelling}} value = *frame;
    hex_task_release(task);
    return value;
}
{{end}}{{define "spawn_arg_frame"}}
typedef struct hex_task_args_{{.Key}} {
{{range .Fields}}    {{.}};
{{end}}} hex_task_args_{{.Key}};
{{end}}{{define "chan_new_adapter"}}
static inline {{.UnionCName}} hex_chan_new_{{.Suffix}}(hex_heap h, size_t capacity, size_t line, size_t column, const hex_string *message) {
    (void)h;
    (void)message;
    hex_chan *channel = hex_chan_new(capacity, sizeof({{.ElementSpelling}}));
    if (channel != nullptr) {
        return ({{.UnionCName}}){ .tag = {{.ChannelTag}}, .payload.{{.ChannelField}} = channel };
    }
    return ({{.UnionCName}}){ .tag = {{.ErrorTag}}, .payload.{{.ErrorField}} = hex_sched_error((hex_t_ErrorKind){ .tag = {{.KindTag}} }, line, column, &{{.Message}}) };
}
{{end}}{{define "chan_send_adapter"}}
static inline {{.UnionCName}} hex_chan_send_{{.Suffix}}(hex_chan *channel, {{.ElementSpelling}} value, size_t line, size_t column, const hex_string *message) {
    (void)message;
    if (hex_chan_send(channel, &value)) {
        return ({{.UnionCName}}){ .tag = {{.NilTag}} };
    }
    return ({{.UnionCName}}){ .tag = {{.ErrorTag}}, .payload.{{.ErrorField}} = hex_sched_error((hex_t_ErrorKind){ .tag = {{.KindTag}} }, line, column, &{{.Message}}) };
}
{{end}}{{define "chan_recv_adapter"}}
static inline {{.UnionCName}} hex_chan_recv_{{.Suffix}}(hex_chan *channel) {
    {{.ElementSpelling}} value;
    if (hex_chan_receive(channel, &value)) {
        return ({{.UnionCName}}){ .tag = {{.ElementTag}}, .payload.{{.ElementField}} = value };
    }
    return ({{.UnionCName}}){ .tag = {{.EosTag}} };
}
{{end}}{{define "chan_free_adapter"}}
static inline void hex_chan_free_{{.Suffix}}(hex_heap h, hex_chan *channel) {
    (void)h;
    hex_chan_free(channel);
}
{{end}}{{define "mutex_new_adapter"}}
static inline {{.UnionCName}} hex_mutex_new_mutex(hex_heap h, size_t line, size_t column, const hex_string *message) {
    (void)h;
    (void)message;
    hex_mutex *mutex = hex_mutex_new();
    if (mutex != nullptr) {
        return ({{.UnionCName}}){ .tag = {{.MutexTag}}, .payload.{{.MutexField}} = mutex };
    }
    return ({{.UnionCName}}){ .tag = {{.ErrorTag}}, .payload.{{.ErrorField}} = hex_sched_error((hex_t_ErrorKind){ .tag = {{.KindTag}} }, line, column, &{{.Message}}) };
}
{{end}}{{define "mutex_free_adapter"}}
static inline void hex_mutex_free_hex_mutex(hex_heap h, hex_mutex *mutex) {
    (void)h;
    hex_mutex_free(mutex);
}
{{end}}{{define "atomic_typedef"}}typedef _Atomic({{.ElementSpelling}}) {{.AtomicSpelling}};
{{end}}{{define "atomic_new"}}static inline {{.ElementSpelling}} {{.AtomicSpelling}}_new({{.ElementSpelling}} value) {
    return value;
}
{{end}}{{define "atomic_load"}}static inline {{.ElementSpelling}} {{.AtomicSpelling}}_load({{.AtomicSpelling}} *atomic) {
    return atomic_load_explicit(atomic, memory_order_seq_cst);
}
{{end}}{{define "atomic_store"}}static inline void {{.AtomicSpelling}}_store({{.AtomicSpelling}} *atomic, {{.ElementSpelling}} value) {
    atomic_store_explicit(atomic, value, memory_order_seq_cst);
}
{{end}}{{define "atomic_exchange"}}static inline {{.ElementSpelling}} {{.AtomicSpelling}}_exchange({{.AtomicSpelling}} *atomic, {{.ElementSpelling}} value) {
    return atomic_exchange_explicit(atomic, value, memory_order_seq_cst);
}
{{end}}{{define "atomic_fetch_add"}}static inline {{.ElementSpelling}} {{.AtomicSpelling}}_fetch_add({{.AtomicSpelling}} *atomic, {{.ElementSpelling}} value) {
    return atomic_fetch_add_explicit(atomic, value, memory_order_seq_cst);
}
{{end}}{{define "atomic_fetch_sub"}}static inline {{.ElementSpelling}} {{.AtomicSpelling}}_fetch_sub({{.AtomicSpelling}} *atomic, {{.ElementSpelling}} value) {
    return atomic_fetch_sub_explicit(atomic, value, memory_order_seq_cst);
}
{{end}}{{define "atomic_compare_exchange"}}static inline bool {{.AtomicSpelling}}_compare_exchange({{.AtomicSpelling}} *atomic, {{.ElementSpelling}} expected, {{.ElementSpelling}} desired) {
    return atomic_compare_exchange_strong_explicit(atomic, &expected, desired, memory_order_seq_cst, memory_order_seq_cst);
}
{{end}}