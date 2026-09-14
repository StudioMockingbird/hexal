/* Process and Pipe runtime over libuv: uv_spawn, uv_process_t, and uv_pipe_t
   for child standard streams. Every ADT and union tag (ProcessStream,
   Environment, ExitStatus, the String|Nil and Pipe|Nil structural unions) is
   translated to or from a plain value by the module-owned inline adapter
   hexal/process.h's generated helpers build; nothing below this line ever
   reads or writes a hex_tag. This first implementation admits one active
   read or write at a time per Pipe: a second concurrent call of the same
   kind returns HEX_PROCESS_BUSY immediately rather than joining a FIFO wait
   queue, the same disclosed simplification the TCP connection core makes. */
{{- if .Operations}}
#include "hexal/process.h"
#include "hexal/heap.h"
#include "hexal/list.h"
#include "hexal/event.h"
#include <signal.h>
#include <stdckdint.h>
#include <string.h>
#include <uv.h>

constexpr size_t HEX_PROCESS_MAX_REQUEST = UINT32_MAX;

static bool hex_process_env_name_equal(const uint8_t *a, size_t a_len, const uint8_t *b, size_t b_len) {
    if (a_len != b_len) {
        return false;
    }
#ifdef _WIN32
    // Windows compares environment variable names case-insensitively.
    for (size_t index = 0; index < a_len; index++) {
        uint8_t left = a[index];
        uint8_t right = b[index];
        if (left >= 'A' && left <= 'Z') {
            left = (uint8_t)(left + 32);
        }
        if (right >= 'A' && right <= 'Z') {
            right = (uint8_t)(right + 32);
        }
        if (left != right) {
            return false;
        }
    }
    return true;
#else
    return memcmp(a, b, a_len) == 0;
#endif
}

// hex_process_validate rejects an embedded NUL anywhere, an invalid or
// duplicate environment name, or an embedded '=' in a name, before any
// native submission.
static int hex_process_validate(hex_process_options options) {
    if (memchr(options.program->data, 0, options.program->byte_length) != nullptr) {
        return HEX_PROCESS_INVALID_INPUT;
    }
    for (size_t index = 0; index < options.arguments->length; index++) {
        const hex_string *argument = options.arguments->data[index];
        if (memchr(argument->data, 0, argument->byte_length) != nullptr) {
            return HEX_PROCESS_INVALID_INPUT;
        }
    }
    if (options.working_directory != nullptr &&
        memchr(options.working_directory->data, 0, options.working_directory->byte_length) != nullptr) {
        return HEX_PROCESS_INVALID_INPUT;
    }
    if (options.environment_replace) {
        hex_list_EnvironmentVariable *values = options.environment_values;
        for (size_t index = 0; index < values->length; index++) {
            hex_t_EnvironmentVariable entry = values->data[index];
            if (entry.hex_m_name->byte_length == 0 ||
                memchr(entry.hex_m_name->data, 0, entry.hex_m_name->byte_length) != nullptr ||
                memchr(entry.hex_m_name->data, '=', entry.hex_m_name->byte_length) != nullptr ||
                memchr(entry.hex_m_value->data, 0, entry.hex_m_value->byte_length) != nullptr) {
                return HEX_PROCESS_INVALID_INPUT;
            }
            for (size_t other = 0; other < index; other++) {
                const hex_string *name = values->data[other].hex_m_name;
                if (hex_process_env_name_equal(entry.hex_m_name->data, entry.hex_m_name->byte_length, name->data, name->byte_length)) {
                    return HEX_PROCESS_INVALID_INPUT;
                }
            }
        }
    }
    return 0;
}

static char *hex_process_copy_cstring(const hex_string *value) {
    size_t size;
    if (ckd_add(&size, value->byte_length, 1)) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    char *copy = (char *)hex_heap_allocate(size);
    memcpy(copy, value->data, value->byte_length);
    copy[value->byte_length] = '\0';
    return copy;
}

// hex_process_snapshot owns every NUL-terminated buffer uv_spawn reads
// synchronously; hex_process_start releases it immediately after the spawn
// attempt returns, whether it succeeded or failed.
typedef struct hex_process_snapshot {
    char *program;
    char **argv;
    char **envp;
    char *cwd;
} hex_process_snapshot;

static void hex_process_snapshot_build(hex_process_options options, hex_process_snapshot *snapshot) {
    memset(snapshot, 0, sizeof(*snapshot));
    snapshot->program = hex_process_copy_cstring(options.program);
    size_t argc;
    if (ckd_add(&argc, options.arguments->length, 2)) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    snapshot->argv = (char **)hex_heap_allocate(argc * sizeof(char *));
    snapshot->argv[0] = snapshot->program;
    for (size_t index = 0; index < options.arguments->length; index++) {
        snapshot->argv[index + 1] = hex_process_copy_cstring(options.arguments->data[index]);
    }
    snapshot->argv[argc - 1] = nullptr;
    if (options.environment_replace) {
        hex_list_EnvironmentVariable *values = options.environment_values;
        size_t envc;
        if (ckd_add(&envc, values->length, 1)) {
            hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
        }
        snapshot->envp = (char **)hex_heap_allocate(envc * sizeof(char *));
        for (size_t index = 0; index < values->length; index++) {
            hex_t_EnvironmentVariable entry = values->data[index];
            size_t size;
            if (ckd_add(&size, entry.hex_m_name->byte_length, entry.hex_m_value->byte_length) || ckd_add(&size, size, 2)) {
                hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
            }
            char *pair = (char *)hex_heap_allocate(size);
            memcpy(pair, entry.hex_m_name->data, entry.hex_m_name->byte_length);
            pair[entry.hex_m_name->byte_length] = '=';
            memcpy(pair + entry.hex_m_name->byte_length + 1, entry.hex_m_value->data, entry.hex_m_value->byte_length);
            pair[entry.hex_m_name->byte_length + 1 + entry.hex_m_value->byte_length] = '\0';
            snapshot->envp[index] = pair;
        }
        snapshot->envp[envc - 1] = nullptr;
    }
    if (options.working_directory != nullptr) {
        snapshot->cwd = hex_process_copy_cstring(options.working_directory);
    }
}

static void hex_process_snapshot_free(hex_process_snapshot *snapshot) {
    if (snapshot->argv != nullptr) {
        for (size_t index = 1; snapshot->argv[index] != nullptr; index++) {
            hex_heap_free(snapshot->argv[index]);
        }
        hex_heap_free(snapshot->argv);
    }
    hex_heap_free(snapshot->program);
    if (snapshot->envp != nullptr) {
        for (size_t index = 0; snapshot->envp[index] != nullptr; index++) {
            hex_heap_free(snapshot->envp[index]);
        }
        hex_heap_free(snapshot->envp);
    }
    hex_heap_free(snapshot->cwd);
}

// hex_pipe_control is the capability control block the handle registry pins
// for the lifetime of one child standard-stream pipe. Direction is fixed at
// creation: stdin's parent-facing Pipe is writable only, stdout/stderr's are
// readable only.
typedef struct hex_pipe_control {
    uv_pipe_t pipe;
    bool readable;
    bool writable;
    bool busy_read;
    bool busy_write;
} hex_pipe_control;

static hex_pipe_control *hex_pipe_control_new(bool readable, bool writable) {
    hex_pipe_control *control = (hex_pipe_control *)hex_heap_allocate_zeroed_or_null(sizeof(hex_pipe_control));
    if (control != nullptr) {
        control->readable = readable;
        control->writable = writable;
    }
    return control;
}

// hex_process_waiter is one parked wait() call registered on a still-running
// process's waiter list; closed distinguishes a close-losing wake (the real
// ExitStatus) from a close-winning one (Closed), set by whichever side wakes
// it.
typedef struct hex_process_waiter {
    hex_task *task;
    struct hex_process_waiter *next;
    bool closed;
} hex_process_waiter;

// hex_process_control is the capability control block the handle registry
// pins for the lifetime of one child process. lock is this control's own
// synchronization, separate from the registry slot's internal mutex: exit
// (the loop thread) and close (any Task's thread) both take it, so the
// "exit and close share one slot lock" linearization the RFC describes reads
// directly off this critical section.
typedef struct hex_process_control {
    uv_process_t process;
    hex_handle handle;
    uv_mutex_t lock;
    bool exited;
    bool closed;
    int64_t exit_status;
    int term_signal;
    hex_process_waiter *waiters;
} hex_process_control;

static hex_process_control *hex_process_control_new(void) {
    hex_process_control *control = (hex_process_control *)hex_heap_allocate_zeroed_or_null(sizeof(hex_process_control));
    if (control == nullptr) {
        return nullptr;
    }
    if (uv_mutex_init(&control->lock) != 0) {
        hex_heap_free(control);
        return nullptr;
    }
    return control;
}

static void hex_process_native_closed(uv_handle_t *handle) {
    hex_process_control *control = (hex_process_control *)handle->data;
    hex_handle_close_finish(control->handle);
}

// hex_process_exited is libuv's exit_cb, always registered at spawn: it
// fires exactly once, records the cached ExitStatus, and wakes every waiter
// parked before it ran. When close() already linearized first, those
// waiters are already gone (close woke them with Closed), and this callback
// performs only the private reap-and-release the RFC describes: the native
// close it was deferring until exit runs now.
static void hex_process_exited(uv_process_t *process, int64_t exit_status, int term_signal) {
    hex_process_control *control = (hex_process_control *)process->data;
    uv_mutex_lock(&control->lock);
    control->exited = true;
    control->exit_status = exit_status;
    control->term_signal = term_signal;
    bool already_closed = control->closed;
    hex_process_waiter *waiters = control->waiters;
    control->waiters = nullptr;
    uv_mutex_unlock(&control->lock);
    // waker->next is read before waking waker: a woken wait() call may
    // resume on another thread and return immediately, and its
    // hex_process_waiter -- a local on that Task's own stack -- is not safe
    // to dereference again once that happens.
    for (hex_process_waiter *waker = waiters; waker != nullptr;) {
        hex_process_waiter *next = waker->next;
        hex_task_event_wake(waker->task);
        waker = next;
    }
    if (already_closed) {
        uv_close((uv_handle_t *)&control->process, hex_process_native_closed);
    }
}

hex_process_wait_result hex_process_wait(hex_process process) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] process operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(process.handle, HEX_HANDLE_KIND_PROCESS);
    if (lease.control == nullptr) {
        return (hex_process_wait_result){.status = HEX_PROCESS_CLOSED};
    }
    hex_process_control *control = (hex_process_control *)lease.control;
    uv_mutex_lock(&control->lock);
    if (control->exited) {
        int64_t exit_status = control->exit_status;
        int term_signal = control->term_signal;
        uv_mutex_unlock(&control->lock);
        hex_handle_release(lease);
        return (hex_process_wait_result){.status = 0, .terminated = term_signal != 0, .exit_code = exit_status};
    }
    if (control->closed) {
        uv_mutex_unlock(&control->lock);
        hex_handle_release(lease);
        return (hex_process_wait_result){.status = HEX_PROCESS_CLOSED};
    }
    // Registration and the exited/closed checks above run under the same
    // lock close() and exit_cb take, so whichever of "this wait sees the
    // fact first" or "this wait's registration is visible to that side
    // first" holds -- no wakeup can be lost between the two.
    hex_process_waiter waiter = {.task = task};
    waiter.next = control->waiters;
    control->waiters = &waiter;
    hex_task_event_arm(task, &waiter);
    uv_mutex_unlock(&control->lock);
    hex_handle_release(lease);
    hex_task_event_suspend(task);
    if (waiter.closed) {
        return (hex_process_wait_result){.status = HEX_PROCESS_CLOSED};
    }
    uv_mutex_lock(&control->lock);
    int64_t exit_status = control->exit_status;
    int term_signal = control->term_signal;
    uv_mutex_unlock(&control->lock);
    return (hex_process_wait_result){.status = 0, .terminated = term_signal != 0, .exit_code = exit_status};
}

typedef struct hex_process_terminate_request {
    hex_event_command command;
    hex_process_control *control;
    int status;
} hex_process_terminate_request;

static void hex_process_terminate_start(hex_event_command *command) {
    hex_process_terminate_request *request = (hex_process_terminate_request *)command;
    request->status = uv_process_kill(&request->control->process, SIGTERM);
    hex_task_event_wake(request->command.task);
}

int hex_process_terminate(hex_process process) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] process operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(process.handle, HEX_HANDLE_KIND_PROCESS);
    if (lease.control == nullptr) {
        return HEX_PROCESS_CLOSED;
    }
    hex_process_terminate_request request = {.command = {.task = task, .start = hex_process_terminate_start}, .control = (hex_process_control *)lease.control};
    hex_event_submit(task, &request.command);
    hex_handle_release(lease);
    return request.status;
}

typedef struct hex_process_close_now_request {
    hex_event_command command;
    hex_process_control *control;
} hex_process_close_now_request;

static void hex_process_close_now_start(hex_event_command *command) {
    hex_process_close_now_request *request = (hex_process_close_now_request *)command;
    uv_close((uv_handle_t *)&request->control->process, hex_process_native_closed);
    hex_task_event_wake(request->command.task);
}

int hex_process_close(hex_process process) {
    void *control_ptr = hex_handle_close_begin(process.handle, HEX_HANDLE_KIND_PROCESS);
    if (control_ptr == nullptr) {
        return HEX_PROCESS_CLOSED;
    }
    hex_process_control *control = (hex_process_control *)control_ptr;
    uv_mutex_lock(&control->lock);
    control->closed = true;
    bool already_exited = control->exited;
    hex_process_waiter *waiters = control->waiters;
    control->waiters = nullptr;
    uv_mutex_unlock(&control->lock);
    // next is read before waking, for the same reason hex_process_exited's
    // drain reads it first: a woken wait() call may resume on another
    // thread and return immediately, invalidating its own stack-local node.
    for (hex_process_waiter *waker = waiters; waker != nullptr;) {
        hex_process_waiter *next = waker->next;
        waker->closed = true;
        hex_task_event_wake(waker->task);
        waker = next;
    }
    if (already_exited) {
        // The child has already been reaped; the native handle was only
        // waiting on this close to be releasable.
        hex_task *task = hex_task_current();
        if (task == nullptr) {
            hex_runtime_trap("[Runtime Error] process operation outside a Task\n");
        }
        hex_process_close_now_request request = {.command = {.task = task, .start = hex_process_close_now_start}, .control = control};
        hex_event_submit(task, &request.command);
    }
    // Otherwise hex_process_exited performs the native close itself once the
    // child eventually exits; this call never waits for that to happen.
    return 0;
}

static uv_stdio_container_t hex_process_stdio(int index, uint8_t selector, hex_pipe_control *pipe) {
    switch (selector) {
    case HEX_PROCESS_STREAM_PIPE:
        return (uv_stdio_container_t){
            .flags = (uv_stdio_flags)(UV_CREATE_PIPE | (index == 0 ? UV_READABLE_PIPE : UV_WRITABLE_PIPE)),
            .data = {.stream = (uv_stream_t *)&pipe->pipe},
        };
    case HEX_PROCESS_STREAM_INHERIT:
        return (uv_stdio_container_t){.flags = UV_INHERIT_FD, .data = {.fd = index}};
    default:
        return (uv_stdio_container_t){.flags = UV_IGNORE};
    }
}

typedef struct hex_process_spawn_request {
    hex_event_command command;
    uv_process_options_t options;
    uv_stdio_container_t stdio[3];
    hex_pipe_control *pipes[3];
    bool pipe_initialized[3];
    hex_process_control *control;
    bool spawn_attempted;
    int status;
} hex_process_spawn_request;

static void hex_process_pipe_closed(uv_handle_t *handle) {
    hex_heap_free(handle->data);
}

static void hex_process_spawn_failed_closed(uv_handle_t *handle) {
    hex_process_control *control = (hex_process_control *)handle->data;
    uv_mutex_destroy(&control->lock);
    hex_heap_free(control);
}

// hex_process_spawn_start runs on the loop thread: it is the only place
// uv_pipe_init and uv_spawn may run. On any failure it closes every native
// handle it already initialized itself (spawn_attempted also covers
// uv_spawn's own always-uv__handle_init'd process handle, so it needs
// closing even when uv_spawn itself returned an error) and frees every
// control block that was never published through the handle registry.
static void hex_process_spawn_start(hex_event_command *command) {
    hex_process_spawn_request *request = (hex_process_spawn_request *)command;
    uv_loop_t *loop = (uv_loop_t *)hex_event_loop_handle();
    int status = 0;
    for (int index = 0; index < 3 && status == 0; index++) {
        if (request->pipes[index] != nullptr) {
            status = uv_pipe_init(loop, &request->pipes[index]->pipe, 0);
            if (status == 0) {
                request->pipe_initialized[index] = true;
            }
        }
    }
    if (status == 0) {
        request->options.stdio = request->stdio;
        request->options.stdio_count = 3;
        request->options.exit_cb = hex_process_exited;
        request->control->process.data = request->control;
        request->spawn_attempted = true;
        status = uv_spawn(loop, &request->control->process, &request->options);
    }
    request->status = status;
    if (status != 0) {
        for (int index = 0; index < 3; index++) {
            if (request->pipe_initialized[index]) {
                request->pipes[index]->pipe.data = request->pipes[index];
                uv_close((uv_handle_t *)&request->pipes[index]->pipe, hex_process_pipe_closed);
            } else if (request->pipes[index] != nullptr) {
                hex_heap_free(request->pipes[index]);
            }
        }
        if (request->spawn_attempted) {
            uv_close((uv_handle_t *)&request->control->process, hex_process_spawn_failed_closed);
        } else {
            uv_mutex_destroy(&request->control->lock);
            hex_heap_free(request->control);
        }
    }
    hex_task_event_wake(request->command.task);
}

hex_process_start_result hex_process_start(hex_process_options options) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] process operation outside a Task\n");
    }
    int invalid = hex_process_validate(options);
    if (invalid != 0) {
        return (hex_process_start_result){.status = invalid};
    }
    hex_process_control *control = hex_process_control_new();
    if (control == nullptr) {
        return (hex_process_start_result){.status = HEX_PROCESS_ALLOCATION_FAILED};
    }
    hex_handle process_handle = hex_handle_reserve(HEX_HANDLE_KIND_PROCESS);
    if (process_handle.slot == nullptr) {
        uv_mutex_destroy(&control->lock);
        hex_heap_free(control);
        return (hex_process_start_result){.status = HEX_PROCESS_ALLOCATION_FAILED};
    }
    control->handle = process_handle;

    uint8_t selectors[3] = {options.input, options.output, options.error};
    hex_pipe_control *pipes[3] = {nullptr, nullptr, nullptr};
    hex_handle pipe_handles[3] = {0};
    bool allocation_failed = false;
    for (int index = 0; index < 3 && !allocation_failed; index++) {
        if (selectors[index] != HEX_PROCESS_STREAM_PIPE) {
            continue;
        }
        // Index 0 (stdin) is writable from the parent, readable by the
        // child; indices 1 and 2 (stdout, stderr) are the reverse.
        pipes[index] = hex_pipe_control_new(/*readable=*/index != 0, /*writable=*/index == 0);
        if (pipes[index] == nullptr) {
            allocation_failed = true;
            break;
        }
        pipe_handles[index] = hex_handle_reserve(HEX_HANDLE_KIND_PIPE);
        if (pipe_handles[index].slot == nullptr) {
            allocation_failed = true;
        }
    }
    if (allocation_failed) {
        hex_handle_abandon(process_handle);
        for (int index = 0; index < 3; index++) {
            if (pipe_handles[index].slot != nullptr) {
                hex_handle_abandon(pipe_handles[index]);
            }
            hex_heap_free(pipes[index]);
        }
        uv_mutex_destroy(&control->lock);
        hex_heap_free(control);
        return (hex_process_start_result){.status = HEX_PROCESS_ALLOCATION_FAILED};
    }

    hex_process_snapshot snapshot;
    hex_process_snapshot_build(options, &snapshot);

    hex_process_spawn_request request = {.command = {.task = task, .start = hex_process_spawn_start}, .control = control};
    request.options.file = snapshot.program;
    request.options.args = snapshot.argv;
    request.options.env = snapshot.envp;
    request.options.cwd = snapshot.cwd;
    for (int index = 0; index < 3; index++) {
        request.pipes[index] = pipes[index];
        request.stdio[index] = hex_process_stdio(index, selectors[index], pipes[index]);
    }
    hex_event_submit(task, &request.command);
    hex_process_snapshot_free(&snapshot);

    if (request.status != 0) {
        // hex_process_spawn_start already closed and freed every native
        // handle and control block on this path; only the reservations this
        // function made are still outstanding.
        hex_handle_abandon(process_handle);
        for (int index = 0; index < 3; index++) {
            if (pipe_handles[index].slot != nullptr) {
                hex_handle_abandon(pipe_handles[index]);
            }
        }
        return (hex_process_start_result){.status = request.status};
    }

    hex_handle_publish(process_handle, control);
    hex_process_start_result result = {.status = 0, .process = {.handle = process_handle}};
    hex_process_pipe_result *slots[3] = {&result.input, &result.output, &result.error};
    for (int index = 0; index < 3; index++) {
        if (pipes[index] == nullptr) {
            continue;
        }
        hex_handle_publish(pipe_handles[index], pipes[index]);
        *slots[index] = (hex_process_pipe_result){.pipe = {.handle = pipe_handles[index]}, .present = true};
    }
    return result;
}

typedef struct hex_pipe_read_request {
    hex_event_command command;
    hex_pipe_control *control;
    uv_buf_t buffer;
    ssize_t result;
} hex_pipe_read_request;

static void hex_pipe_read_alloc(uv_handle_t *handle, size_t suggested, uv_buf_t *buf) {
    (void)suggested;
    hex_pipe_read_request *request = (hex_pipe_read_request *)handle->data;
    *buf = request->buffer;
}

static void hex_pipe_read_cb(uv_stream_t *stream, ssize_t nread, const uv_buf_t *buf) {
    (void)buf;
    hex_pipe_read_request *request = (hex_pipe_read_request *)stream->data;
    if (nread == 0) {
        return;
    }
    uv_read_stop(stream);
    request->result = nread;
    hex_task_event_wake(request->command.task);
}

static void hex_pipe_read_start(hex_event_command *command) {
    hex_pipe_read_request *request = (hex_pipe_read_request *)command;
    request->control->pipe.data = request;
    if (uv_read_start((uv_stream_t *)&request->control->pipe, hex_pipe_read_alloc, hex_pipe_read_cb) != 0) {
        request->result = UV_EINVAL;
        hex_task_event_wake(request->command.task);
    }
}

hex_pipe_transfer hex_pipe_read(hex_pipe pipe_value, hex_list_UInt8 *into, size_t max) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] process operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(pipe_value.handle, HEX_HANDLE_KIND_PIPE);
    if (lease.control == nullptr) {
        return (hex_pipe_transfer){.status = HEX_PROCESS_CLOSED};
    }
    hex_pipe_control *control = (hex_pipe_control *)lease.control;
    if (!control->readable) {
        hex_handle_release(lease);
        return (hex_pipe_transfer){.status = HEX_PROCESS_PERMISSION_DENIED};
    }
    if (control->busy_read) {
        hex_handle_release(lease);
        return (hex_pipe_transfer){.status = HEX_PROCESS_BUSY};
    }
    if (max == 0) {
        hex_handle_release(lease);
        return (hex_pipe_transfer){.status = 0, .count = 0};
    }
    control->busy_read = true;
    size_t count = max > HEX_PROCESS_MAX_REQUEST ? HEX_PROCESS_MAX_REQUEST : max;
    size_t needed;
    if (ckd_add(&needed, into->length, count)) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    hex_list_reserve_at_least_UInt8(into, needed);
    hex_pipe_read_request request = {
        .command = {.task = task, .start = hex_pipe_read_start},
        .control = control,
        .buffer = uv_buf_init((char *)(into->data + into->length), (unsigned int)count),
    };
    hex_event_submit(task, &request.command);
    control->busy_read = false;
    hex_handle_release(lease);
    if (request.result == UV_EOF) {
        return (hex_pipe_transfer){.status = HEX_PROCESS_EOS};
    }
    if (request.result < 0) {
        return (hex_pipe_transfer){.status = (int)request.result};
    }
    into->length += (size_t)request.result;
    return (hex_pipe_transfer){.status = 0, .count = (size_t)request.result};
}

typedef struct hex_pipe_write_request {
    hex_event_command command;
    uv_write_t request;
    uv_buf_t buffer;
    hex_pipe_control *control;
    int status;
} hex_pipe_write_request;

static void hex_pipe_write_done(uv_write_t *request, int status) {
    hex_pipe_write_request *write_request = (hex_pipe_write_request *)request->data;
    write_request->status = status;
    hex_task_event_wake(write_request->command.task);
}

static void hex_pipe_write_start(hex_event_command *command) {
    hex_pipe_write_request *request = (hex_pipe_write_request *)command;
    request->request.data = request;
    int status = uv_write(&request->request, (uv_stream_t *)&request->control->pipe, &request->buffer, 1, hex_pipe_write_done);
    if (status < 0) {
        request->status = status;
        hex_task_event_wake(request->command.task);
    }
}

int hex_pipe_write(hex_pipe pipe_value, hex_slice_UInt8 from) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] process operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(pipe_value.handle, HEX_HANDLE_KIND_PIPE);
    if (lease.control == nullptr) {
        return HEX_PROCESS_CLOSED;
    }
    hex_pipe_control *control = (hex_pipe_control *)lease.control;
    if (!control->writable) {
        hex_handle_release(lease);
        return HEX_PROCESS_PERMISSION_DENIED;
    }
    if (control->busy_write) {
        hex_handle_release(lease);
        return HEX_PROCESS_BUSY;
    }
    control->busy_write = true;
    int status = 0;
    size_t offset = 0;
    while (offset < from.length) {
        size_t count = from.length - offset > HEX_PROCESS_MAX_REQUEST ? HEX_PROCESS_MAX_REQUEST : from.length - offset;
        hex_pipe_write_request request = {
            .command = {.task = task, .start = hex_pipe_write_start},
            .control = control,
            .buffer = uv_buf_init((char *)(from.data + offset), (unsigned int)count),
        };
        hex_event_submit(task, &request.command);
        if (request.status < 0) {
            status = request.status;
            break;
        }
        offset += count;
    }
    control->busy_write = false;
    hex_handle_release(lease);
    return status;
}

typedef struct hex_pipe_shutdown_request {
    hex_event_command command;
    uv_shutdown_t request;
    hex_pipe_control *control;
    int status;
} hex_pipe_shutdown_request;

static void hex_pipe_shutdown_done(uv_shutdown_t *request, int status) {
    hex_pipe_shutdown_request *shutdown_request = (hex_pipe_shutdown_request *)request->data;
    shutdown_request->status = status;
    hex_task_event_wake(shutdown_request->command.task);
}

static void hex_pipe_shutdown_start(hex_event_command *command) {
    hex_pipe_shutdown_request *request = (hex_pipe_shutdown_request *)command;
    request->request.data = request;
    int status = uv_shutdown(&request->request, (uv_stream_t *)&request->control->pipe, hex_pipe_shutdown_done);
    if (status < 0) {
        request->status = status;
        hex_task_event_wake(request->command.task);
    }
}

int hex_pipe_shutdown(hex_pipe pipe_value) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] process operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(pipe_value.handle, HEX_HANDLE_KIND_PIPE);
    if (lease.control == nullptr) {
        return HEX_PROCESS_CLOSED;
    }
    hex_pipe_control *control = (hex_pipe_control *)lease.control;
    if (!control->writable) {
        hex_handle_release(lease);
        return HEX_PROCESS_PERMISSION_DENIED;
    }
    hex_pipe_shutdown_request request = {.command = {.task = task, .start = hex_pipe_shutdown_start}, .control = control};
    hex_event_submit(task, &request.command);
    hex_handle_release(lease);
    return request.status;
}

typedef struct hex_pipe_close_request {
    hex_event_command command;
    uv_pipe_t *pipe;
} hex_pipe_close_request;

static void hex_pipe_close_done(uv_handle_t *handle) {
    hex_pipe_close_request *request = (hex_pipe_close_request *)handle->data;
    hex_task_event_wake(request->command.task);
}

static void hex_pipe_close_start(hex_event_command *command) {
    hex_pipe_close_request *request = (hex_pipe_close_request *)command;
    request->pipe->data = request;
    uv_close((uv_handle_t *)request->pipe, hex_pipe_close_done);
}

// hex_pipe_native_close marshals uv_close onto the loop thread and parks the
// calling Task until libuv's close callback confirms the handle fully
// released, mirroring hexal/network.c's hex_tcp_native_close.
static void hex_pipe_native_close(uv_pipe_t *pipe) {
    hex_task *task = hex_task_current();
    hex_pipe_close_request request = {.command = {.task = task, .start = hex_pipe_close_start}, .pipe = pipe};
    hex_event_submit(task, &request.command);
}

int hex_pipe_close(hex_pipe pipe_value) {
    void *control_ptr = hex_handle_close_begin(pipe_value.handle, HEX_HANDLE_KIND_PIPE);
    if (control_ptr == nullptr) {
        return HEX_PROCESS_CLOSED;
    }
    hex_pipe_control *control = (hex_pipe_control *)control_ptr;
    hex_pipe_native_close(&control->pipe);
    hex_handle_close_finish(pipe_value.handle);
    return 0;
}

hex_t_Error hex_process_error(size_t line, size_t column, int status, bool pipe, const hex_string *message) {
    hex_t_ErrorKind kind = {0};
    switch (status) {
    case HEX_PROCESS_INVALID_INPUT:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput};
        break;
    case HEX_PROCESS_PERMISSION_DENIED:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_PermissionDenied};
        break;
    case HEX_PROCESS_CLOSED:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Closed};
        break;
    case HEX_PROCESS_ALLOCATION_FAILED:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted};
        break;
    case HEX_PROCESS_BUSY:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Busy};
        break;
    default: {
        if (!hex_handle_error_kind(status, &kind)) {
            hex_strand header = {0};
            if (pipe) {
                static const char text[] = "pipe error";
                memcpy(header.data, text, sizeof(text) - 1);
            } else {
                static const char text[] = "process error";
                memcpy(header.data, text, sizeof(text) - 1);
            }
            kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Other, .other_header = header};
        }
    }
    }
    return (hex_t_Error){.hex_m_line = line, .hex_m_column = column, .hex_m_kind = kind, .hex_m_message = message};
}
{{- end}}
