/* Ordinary process-signal observation over libuv: up to three uv_signal_t
   watchers per Signals resource, one pending bit per subscribed Signal, and
   at most one active next() at a time. Every ADT tag (Signal) is translated
   to or from a plain value by the module-owned inline adapter
   hexal/signal.h's generated helpers build; nothing below this line ever
   reads or writes a hex_tag. Synchronous faults (stack overflow, SIGSEGV,
   SIGILL) are handled by the existing guarded-stack and vectored-exception
   machinery, structurally separate from this file, and never routed here. */
{{- if .Operations}}
#include "hexal/signal.h"
#include "hexal/heap.h"
#include "hexal/event.h"
#include <signal.h>
#include <string.h>
#include <uv.h>

// hex_signal_supported reports whether the host libuv build can deliver raw:
// Interrupt and Hangup are portable, but Windows has no ordinary delivery
// path for Terminate (libuv's Windows console control handler dispatches
// only SIGINT and the console-close-emulated SIGHUP).
static bool hex_signal_supported(uint8_t raw) {
#ifdef _WIN32
    return raw == HEX_SIGNAL_INTERRUPT || raw == HEX_SIGNAL_HANGUP;
#else
    return raw <= HEX_SIGNAL_TERMINATE;
#endif
}

static int hex_signal_number(uint8_t raw) {
    switch (raw) {
    case HEX_SIGNAL_INTERRUPT:
        return SIGINT;
    case HEX_SIGNAL_HANGUP:
        return SIGHUP;
    default:
        return SIGTERM;
    }
}

// hex_signals_validate rejects an empty, oversized (necessarily containing a
// duplicate, since only three raw values exist), duplicate, or
// target-unsupported subscription set before any native registration.
static int hex_signals_validate(const uint8_t *subscriptions, size_t count) {
    if (count == 0 || count > 3) {
        return HEX_SIGNAL_INVALID_INPUT;
    }
    for (size_t index = 0; index < count; index++) {
        if (subscriptions[index] > HEX_SIGNAL_TERMINATE) {
            return HEX_SIGNAL_INVALID_INPUT;
        }
        for (size_t other = 0; other < index; other++) {
            if (subscriptions[other] == subscriptions[index]) {
                return HEX_SIGNAL_INVALID_INPUT;
            }
        }
        if (!hex_signal_supported(subscriptions[index])) {
            return HEX_SIGNAL_UNSUPPORTED;
        }
    }
    return 0;
}

// hex_signals_control is the capability control block the handle registry
// pins for the lifetime of one Signals resource. lock is this control's own
// synchronization, separate from the registry slot's internal mutex: a
// watcher callback (the loop thread) and close() (any Task's thread) both
// take it, so the "event selection and close linearize under the same
// slot synchronization" reads directly off this critical section. waiter is
// the one active next() caller, or nullptr; whichever of a watcher callback
// or close() finds it non-null and clears it to nullptr is the sole waker,
// closing the exact race window the linearization rule describes.
typedef struct hex_signals_control {
    hex_handle handle;
    uv_signal_t watchers[3];
    bool subscribed[3];
    uv_mutex_t lock;
    uint8_t pending;
    bool closed;
    bool published;
    int closing_count;
    hex_task *waiter;
} hex_signals_control;

static void hex_signals_watcher_closed(uv_handle_t *watcher_handle) {
    hex_signals_control *control = (hex_signals_control *)watcher_handle->data;
    uv_mutex_lock(&control->lock);
    control->closing_count--;
    bool done = control->closing_count == 0;
    uv_mutex_unlock(&control->lock);
    if (!done) {
        return;
    }
    if (control->published) {
        hex_handle_close_finish(control->handle);
    } else {
        uv_mutex_destroy(&control->lock);
        hex_heap_free(control);
    }
}

static void hex_signals_fired(uv_signal_t *watcher, int signum) {
    (void)signum;
    hex_signals_control *control = (hex_signals_control *)watcher->data;
    uint8_t index = (uint8_t)(watcher - control->watchers);
    uv_mutex_lock(&control->lock);
    control->pending = (uint8_t)(control->pending | (1u << index));
    hex_task *waiter = control->waiter;
    control->waiter = nullptr;
    uv_mutex_unlock(&control->lock);
    if (waiter != nullptr) {
        hex_task_event_wake(waiter);
    }
}

typedef struct hex_signals_new_request {
    hex_event_command command;
    hex_signals_control *control;
    uint8_t subscriptions[3];
    size_t count;
    int status;
} hex_signals_new_request;

static void hex_signals_new_start(hex_event_command *command) {
    hex_signals_new_request *request = (hex_signals_new_request *)command;
    hex_signals_control *control = request->control;
    uv_loop_t *loop = (uv_loop_t *)hex_event_loop_handle();
    int status = 0;
    for (size_t index = 0; index < request->count && status == 0; index++) {
        uint8_t raw = request->subscriptions[index];
        status = uv_signal_init(loop, &control->watchers[raw]);
        if (status == 0) {
            control->subscribed[raw] = true;
            control->watchers[raw].data = control;
            status = uv_signal_start(&control->watchers[raw], hex_signals_fired, hex_signal_number(raw));
        }
    }
    request->status = status;
    if (status != 0) {
        int closing = 0;
        for (int index = 0; index < 3; index++) {
            if (control->subscribed[index]) {
                uv_close((uv_handle_t *)&control->watchers[index], hex_signals_watcher_closed);
                closing++;
            }
        }
        if (closing == 0) {
            uv_mutex_destroy(&control->lock);
            hex_heap_free(control);
        } else {
            control->closing_count = closing;
        }
    }
    hex_task_event_wake(request->command.task);
}

hex_signals_new_result hex_signals_new(const uint8_t *subscriptions, size_t count) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] signal operation outside a Task\n");
    }
    int invalid = hex_signals_validate(subscriptions, count);
    if (invalid != 0) {
        return (hex_signals_new_result){.status = invalid};
    }
    hex_signals_control *control = (hex_signals_control *)hex_heap_allocate_zeroed_or_null(sizeof(hex_signals_control));
    if (control == nullptr) {
        return (hex_signals_new_result){.status = HEX_SIGNAL_ALLOCATION_FAILED};
    }
    if (uv_mutex_init(&control->lock) != 0) {
        hex_heap_free(control);
        return (hex_signals_new_result){.status = HEX_SIGNAL_ALLOCATION_FAILED};
    }
    hex_handle handle = hex_handle_reserve(HEX_HANDLE_KIND_SIGNALS);
    if (handle.slot == nullptr) {
        uv_mutex_destroy(&control->lock);
        hex_heap_free(control);
        return (hex_signals_new_result){.status = HEX_SIGNAL_ALLOCATION_FAILED};
    }
    control->handle = handle;
    hex_signals_new_request request = {.command = {.task = task, .start = hex_signals_new_start}, .control = control, .count = count};
    for (size_t index = 0; index < count; index++) {
        request.subscriptions[index] = subscriptions[index];
    }
    hex_event_submit(task, &request.command);
    if (request.status != 0) {
        // hex_signals_new_start already closed and freed every native
        // watcher and control block on this path; only the reservation this
        // function made is still outstanding.
        hex_handle_abandon(handle);
        return (hex_signals_new_result){.status = request.status};
    }
    control->published = true;
    hex_handle_publish(handle, control);
    return (hex_signals_new_result){.status = 0, .signals = {.handle = handle}};
}

hex_signals_next_result hex_signals_next(hex_signals signals) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] signal operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(signals.handle, HEX_HANDLE_KIND_SIGNALS);
    if (lease.control == nullptr) {
        return (hex_signals_next_result){.status = HEX_SIGNAL_CLOSED};
    }
    hex_signals_control *control = (hex_signals_control *)lease.control;
    uv_mutex_lock(&control->lock);
    if (control->waiter != nullptr) {
        uv_mutex_unlock(&control->lock);
        hex_handle_release(lease);
        return (hex_signals_next_result){.status = HEX_SIGNAL_BUSY};
    }
    for (int index = 0; index < 3; index++) {
        if (control->pending & (1u << index)) {
            control->pending = (uint8_t)(control->pending & ~(1u << index));
            uv_mutex_unlock(&control->lock);
            hex_handle_release(lease);
            return (hex_signals_next_result){.status = 0, .signal = (uint8_t)index};
        }
    }
    if (control->closed) {
        uv_mutex_unlock(&control->lock);
        hex_handle_release(lease);
        return (hex_signals_next_result){.status = HEX_SIGNAL_EOS};
    }
    control->waiter = task;
    hex_task_event_arm(task, control);
    uv_mutex_unlock(&control->lock);
    hex_handle_release(lease);
    hex_task_event_suspend(task);
    // Whichever of a watcher callback or close() cleared waiter to wake this
    // Task recorded its own fact (a pending bit or closed) before doing so;
    // checking pending first here matches the "event selected first
    // wins" linearization exactly, since a close that also ran afterward
    // changes nothing this waiter still needs from it.
    uv_mutex_lock(&control->lock);
    for (int index = 0; index < 3; index++) {
        if (control->pending & (1u << index)) {
            control->pending = (uint8_t)(control->pending & ~(1u << index));
            uv_mutex_unlock(&control->lock);
            return (hex_signals_next_result){.status = 0, .signal = (uint8_t)index};
        }
    }
    uv_mutex_unlock(&control->lock);
    return (hex_signals_next_result){.status = HEX_SIGNAL_EOS};
}

typedef struct hex_signals_close_request {
    hex_event_command command;
    hex_signals_control *control;
} hex_signals_close_request;

static void hex_signals_close_start(hex_event_command *command) {
    hex_signals_close_request *request = (hex_signals_close_request *)command;
    hex_signals_control *control = request->control;
    int closing = 0;
    for (int index = 0; index < 3; index++) {
        if (control->subscribed[index]) {
            uv_close((uv_handle_t *)&control->watchers[index], hex_signals_watcher_closed);
            closing++;
        }
    }
    control->closing_count = closing;
    hex_task_event_wake(request->command.task);
}

int hex_signals_close(hex_signals signals) {
    void *control_ptr = hex_handle_close_begin(signals.handle, HEX_HANDLE_KIND_SIGNALS);
    if (control_ptr == nullptr) {
        return HEX_SIGNAL_CLOSED;
    }
    hex_signals_control *control = (hex_signals_control *)control_ptr;
    uv_mutex_lock(&control->lock);
    control->closed = true;
    hex_task *waiter = control->waiter;
    control->waiter = nullptr;
    uv_mutex_unlock(&control->lock);
    if (waiter != nullptr) {
        hex_task_event_wake(waiter);
    }
    // The native teardown is marshaled onto the loop thread and fired off
    // without waiting for the watchers' own close callbacks: this call
    // returns once the uv_close requests are issued, matching
    // hexal/process.c's hex_process_close_now_start.
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] signal operation outside a Task\n");
    }
    hex_signals_close_request request = {.command = {.task = task, .start = hex_signals_close_start}, .control = control};
    hex_event_submit(task, &request.command);
    return 0;
}

hex_t_Error hex_signal_error(size_t line, size_t column, int status, const hex_string *message) {
    hex_t_ErrorKind kind = {0};
    switch (status) {
    case HEX_SIGNAL_INVALID_INPUT:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput};
        break;
    case HEX_SIGNAL_UNSUPPORTED:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Unsupported};
        break;
    case HEX_SIGNAL_BUSY:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Busy};
        break;
    case HEX_SIGNAL_CLOSED:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Closed};
        break;
    case HEX_SIGNAL_ALLOCATION_FAILED:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted};
        break;
    default: {
        if (!hex_handle_error_kind(status, &kind)) {
            static const char text[] = "signal error";
            hex_string_128 header = { .byte_length = sizeof(text) - 1 };
            memcpy(header.data, text, sizeof(text) - 1);
            kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Other, .other_header = header};
        }
    }
    }
    return (hex_t_Error){.hex_m_line = line, .hex_m_column = column, .hex_m_kind = kind, .hex_m_message = hex_error_message(hex_text_heap(message))};
}
{{- end}}
