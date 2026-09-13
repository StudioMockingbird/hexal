#include "hexal/event.h"
#include <uv.h>

enum {
    HEX_EVENT_WORK,
    HEX_EVENT_TIMER,
    HEX_EVENT_POLL,
    HEX_EVENT_DNS
};

typedef struct hex_event_command {
    struct hex_event_command *next;
    hex_task *task;
    uint8_t kind;
} hex_event_command;

typedef struct hex_event_work {
    hex_event_command command;
    uv_work_t request;
    hex_event_work_entry entry;
    hex_event_work_failure failure;
    void *context;
} hex_event_work;

typedef struct hex_event_timer {
    hex_event_command command;
    uv_timer_t handle;
    uint64_t timeout_ms;
    hex_event_timer_entry entry;
    hex_event_work_failure failure;
    void *context;
    bool failed;
} hex_event_timer;

typedef struct hex_event_poll {
    hex_event_command command;
    uv_poll_t handle;
    intptr_t descriptor;
    int events;
    hex_event_poll_entry entry;
    void *context;
    int status;
    int ready_events;
} hex_event_poll;

typedef struct hex_event_dns {
    hex_event_command command;
    uv_getaddrinfo_t request;
    const char *node;
    const char *service;
    int family;
    hex_event_dns_entry entry;
    hex_event_work_failure failure;
    void *context;
} hex_event_dns;

static uv_loop_t hex_event_loop;
static uv_async_t hex_event_async;
static uv_thread_t hex_event_thread;
static uv_mutex_t hex_event_queue_mutex;
static uv_mutex_t hex_event_ready_mutex;
static uv_cond_t hex_event_ready_cond;
static hex_event_command *hex_event_head;
static hex_event_command *hex_event_tail;
static size_t hex_event_active;
static bool hex_event_ready;
static bool hex_event_stopped;
static bool hex_event_accepting;
static bool hex_event_shutdown_requested;
static bool hex_event_async_closing;

static void hex_event_maybe_close(void) {
    uv_mutex_lock(&hex_event_queue_mutex);
    bool close = hex_event_shutdown_requested &&
                 hex_event_head == nullptr &&
                 hex_event_active == 0 &&
                 !hex_event_async_closing;
    if (close) {
        hex_event_async_closing = true;
    }
    uv_mutex_unlock(&hex_event_queue_mutex);
    if (close) {
        uv_close((uv_handle_t *)&hex_event_async, nullptr);
    }
}

static void hex_event_complete(hex_task *task) {
    uv_mutex_lock(&hex_event_queue_mutex);
    hex_event_active--;
    uv_mutex_unlock(&hex_event_queue_mutex);
    hex_task_event_wake(task);
    hex_event_maybe_close();
}

static void hex_event_work_run(uv_work_t *request) {
    hex_event_work *work = (hex_event_work *)request->data;
    work->entry(work->context);
}

static void hex_event_work_complete(uv_work_t *request, int status) {
    hex_event_work *work = (hex_event_work *)request->data;
    if (status != 0) {
        work->failure(work->context);
    }
    hex_event_complete(work->command.task);
}

static void hex_event_timer_closed(uv_handle_t *handle) {
    hex_event_timer *timer = (hex_event_timer *)handle->data;
    if (timer->failed) {
        timer->failure(timer->context);
    } else {
        timer->entry(timer->context);
    }
    hex_event_complete(timer->command.task);
}

static void hex_event_timer_fired(uv_timer_t *handle) {
    uv_timer_stop(handle);
    uv_close((uv_handle_t *)handle, hex_event_timer_closed);
}

static void hex_event_poll_closed(uv_handle_t *handle) {
    hex_event_poll *poll = (hex_event_poll *)handle->data;
    poll->entry(poll->context, poll->status, poll->ready_events);
    hex_event_complete(poll->command.task);
}

static void hex_event_poll_ready(uv_poll_t *handle, int status, int events) {
    hex_event_poll *poll = (hex_event_poll *)handle->data;
    poll->status = status;
    poll->ready_events = events;
    uv_poll_stop(handle);
    uv_close((uv_handle_t *)handle, hex_event_poll_closed);
}

static void hex_event_dns_complete(uv_getaddrinfo_t *request, int status, struct addrinfo *result) {
    hex_event_dns *dns = (hex_event_dns *)request->data;
    if (status == 0) {
        dns->entry(dns->context, result, status);
    } else {
        dns->failure(dns->context);
    }
    if (result != nullptr) {
        uv_freeaddrinfo(result);
    }
    hex_event_complete(dns->command.task);
}

static bool hex_event_enqueue(hex_event_command *command) {
    uv_mutex_lock(&hex_event_queue_mutex);
    if (!hex_event_accepting) {
        uv_mutex_unlock(&hex_event_queue_mutex);
        return false;
    }
    command->next = nullptr;
    if (hex_event_tail != nullptr) {
        hex_event_tail->next = command;
    } else {
        hex_event_head = command;
    }
    hex_event_tail = command;
    uv_mutex_unlock(&hex_event_queue_mutex);
    if (uv_async_send(&hex_event_async) != 0) {
        hex_runtime_trap("[Runtime Error] event loop wake failed\n");
    }
    return true;
}

static void hex_event_async_callback(uv_async_t *handle) {
    (void)handle;
    for (;;) {
        uv_mutex_lock(&hex_event_queue_mutex);
        hex_event_command *command = hex_event_head;
        if (command != nullptr) {
            hex_event_head = command->next;
            if (hex_event_head == nullptr) {
                hex_event_tail = nullptr;
            }
        }
        uv_mutex_unlock(&hex_event_queue_mutex);
        if (command == nullptr) {
            hex_event_maybe_close();
            return;
        }
        uv_mutex_lock(&hex_event_queue_mutex);
        hex_event_active++;
        uv_mutex_unlock(&hex_event_queue_mutex);
        switch (command->kind) {
        case HEX_EVENT_WORK: {
            hex_event_work *work = (hex_event_work *)command;
            work->request.data = work;
            if (uv_queue_work(&hex_event_loop, &work->request, hex_event_work_run, hex_event_work_complete) != 0) {
                work->failure(work->context);
                hex_event_complete(work->command.task);
            }
            break;
        }
        case HEX_EVENT_TIMER: {
            hex_event_timer *timer = (hex_event_timer *)command;
            if (uv_timer_init(&hex_event_loop, &timer->handle) != 0) {
                timer->failed = true;
                timer->failure(timer->context);
                hex_event_complete(timer->command.task);
                break;
            }
            timer->handle.data = timer;
            if (uv_timer_start(&timer->handle, hex_event_timer_fired, timer->timeout_ms, 0) != 0) {
                timer->failed = true;
                uv_close((uv_handle_t *)&timer->handle, hex_event_timer_closed);
            }
            break;
        }
        case HEX_EVENT_POLL: {
            hex_event_poll *poll = (hex_event_poll *)command;
            int status = uv_poll_init_socket(&hex_event_loop, &poll->handle, (uv_os_sock_t)poll->descriptor);
            if (status != 0) {
                poll->status = status;
                poll->ready_events = 0;
                poll->entry(poll->context, poll->status, poll->ready_events);
                hex_event_complete(poll->command.task);
                break;
            }
            poll->handle.data = poll;
            status = uv_poll_start(&poll->handle, poll->events, hex_event_poll_ready);
            if (status != 0) {
                poll->status = status;
                poll->ready_events = 0;
                uv_close((uv_handle_t *)&poll->handle, hex_event_poll_closed);
            }
            break;
        }
        case HEX_EVENT_DNS: {
            hex_event_dns *dns = (hex_event_dns *)command;
            dns->request.data = dns;
            struct addrinfo hints = {0};
            hints.ai_family = dns->family;
            if (uv_getaddrinfo(&hex_event_loop, &dns->request, hex_event_dns_complete, dns->node, dns->service, &hints) != 0) {
                dns->failure(dns->context);
                hex_event_complete(dns->command.task);
            }
            break;
        }
        default:
            hex_runtime_trap("[Runtime Error] unknown event command\n");
        }
    }
}

static void hex_event_loop_thread(void *unused) {
    (void)unused;
    if (uv_loop_init(&hex_event_loop) != 0 ||
        uv_loop_configure(&hex_event_loop, UV_METRICS_IDLE_TIME) != 0 ||
        uv_async_init(&hex_event_loop, &hex_event_async, hex_event_async_callback) != 0) {
        abort();
    }
    uv_mutex_lock(&hex_event_ready_mutex);
    hex_event_ready = true;
    uv_cond_signal(&hex_event_ready_cond);
    uv_mutex_unlock(&hex_event_ready_mutex);
    uv_run(&hex_event_loop, UV_RUN_DEFAULT);
    uv_loop_close(&hex_event_loop);
    uv_mutex_lock(&hex_event_ready_mutex);
    hex_event_stopped = true;
    uv_cond_signal(&hex_event_ready_cond);
    uv_mutex_unlock(&hex_event_ready_mutex);
}

void hex_event_runtime_init(void) {
    if (uv_mutex_init(&hex_event_queue_mutex) != 0 || uv_mutex_init(&hex_event_ready_mutex) != 0 || uv_cond_init(&hex_event_ready_cond) != 0) {
        hex_runtime_trap("[Runtime Error] event runtime initialization failed\n");
    }
    hex_event_accepting = true;
    if (uv_thread_create(&hex_event_thread, hex_event_loop_thread, nullptr) != 0) {
        hex_runtime_trap("[Runtime Error] event loop thread creation failed\n");
    }
    uv_mutex_lock(&hex_event_ready_mutex);
    while (!hex_event_ready) {
        uv_cond_wait(&hex_event_ready_cond, &hex_event_ready_mutex);
    }
    uv_mutex_unlock(&hex_event_ready_mutex);
}

void hex_event_runtime_shutdown(void) {
    uv_mutex_lock(&hex_event_queue_mutex);
    hex_event_accepting = false;
    hex_event_shutdown_requested = true;
    uv_mutex_unlock(&hex_event_queue_mutex);
    if (uv_async_send(&hex_event_async) != 0) {
        hex_runtime_trap("[Runtime Error] event loop shutdown wake failed\n");
    }
    uv_mutex_lock(&hex_event_ready_mutex);
    while (!hex_event_stopped) {
        uv_cond_wait(&hex_event_ready_cond, &hex_event_ready_mutex);
    }
    uv_mutex_unlock(&hex_event_ready_mutex);
    uv_thread_join(&hex_event_thread);
    uv_mutex_destroy(&hex_event_queue_mutex);
    uv_mutex_destroy(&hex_event_ready_mutex);
    uv_cond_destroy(&hex_event_ready_cond);
}

void hex_event_work_call(hex_event_work_entry entry, hex_event_work_failure failure, void *context) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        entry(context);
        return;
    }
    hex_event_work work = {.command = {.task = task, .kind = HEX_EVENT_WORK}, .entry = entry, .failure = failure, .context = context};
    hex_task_event_arm(task, &work);
    if (!hex_event_enqueue(&work.command)) {
        hex_task_event_cancel(task);
        failure(context);
        return;
    }
    hex_task_event_suspend(task);
}

void hex_event_timer_wait(uint64_t timeout_ms, hex_event_timer_entry entry, hex_event_work_failure failure, void *context) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        failure(context);
        return;
    }
    hex_event_timer timer = {.command = {.task = task, .kind = HEX_EVENT_TIMER}, .timeout_ms = timeout_ms, .entry = entry, .failure = failure, .context = context};
    hex_task_event_arm(task, &timer);
    if (!hex_event_enqueue(&timer.command)) {
        hex_task_event_cancel(task);
        failure(context);
        return;
    }
    hex_task_event_suspend(task);
}

void hex_event_poll_wait(intptr_t descriptor, int events, hex_event_poll_entry entry, void *context) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        entry(context, UV_EINVAL, 0);
        return;
    }
    hex_event_poll poll = {.command = {.task = task, .kind = HEX_EVENT_POLL}, .descriptor = descriptor, .events = events, .entry = entry, .context = context};
    hex_task_event_arm(task, &poll);
    if (!hex_event_enqueue(&poll.command)) {
        hex_task_event_cancel(task);
        entry(context, UV_ECANCELED, 0);
        return;
    }
    hex_task_event_suspend(task);
}

void hex_event_dns_lookup(const char *node, const char *service, int family, hex_event_dns_entry entry, hex_event_work_failure failure, void *context) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        failure(context);
        return;
    }
    hex_event_dns dns = {.command = {.task = task, .kind = HEX_EVENT_DNS}, .node = node, .service = service, .family = family, .entry = entry, .failure = failure, .context = context};
    hex_task_event_arm(task, &dns);
    if (!hex_event_enqueue(&dns.command)) {
        hex_task_event_cancel(task);
        failure(context);
        return;
    }
    hex_task_event_suspend(task);
}

uint64_t hex_event_idle_time(void) {
    return uv_metrics_idle_time(&hex_event_loop);
}
