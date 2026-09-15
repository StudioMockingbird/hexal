#include "hexal/event.h"
{{- if .Sleep}}
#include "hexal/time.h"
{{- end}}
#include <uv.h>
{{- if not .Handle}}

// One command is one intrusive request record owned by a parked Task's fiber
// stack. The record stays live until its terminal loop-thread callback has
// woken the Task, so submitting it allocates nothing. start runs on the loop
// thread and owns every libuv call the request makes.
typedef struct hex_event_command {
    struct hex_event_command *next;
    hex_task *task;
    void (*start)(struct hex_event_command *command);
} hex_event_command;
{{- end}}

typedef struct hex_event_work {
    hex_event_command command;
    uv_work_t request;
    hex_event_work_entry entry;
    hex_event_work_failure failure;
    void *context;
} hex_event_work;

static uv_loop_t hex_event_loop;
static uv_async_t hex_event_async;
static uv_thread_t hex_event_thread;
static uv_mutex_t hex_event_queue_mutex;
static uv_mutex_t hex_event_stdout_mutex;
static uv_mutex_t hex_event_ready_mutex;
static uv_cond_t hex_event_ready_cond;
static hex_event_command *hex_event_head;
static hex_event_command *hex_event_tail;
static bool hex_event_ready;

static void hex_event_enqueue(hex_event_command *command) {
    uv_mutex_lock(&hex_event_queue_mutex);
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
            return;
        }
        command->start(command);
    }
}

// The loop thread publishes readiness only after the loop and its wake handle
// exist. Initialization failure terminates the process through the runtime
// trap, so the scheduler thread waiting for readiness never deadlocks.
static void hex_event_loop_thread(void *unused) {
    (void)unused;
    if (uv_loop_init(&hex_event_loop) != 0) {
        hex_runtime_trap("[Runtime Error] event runtime initialization failed\n");
    }
    if (uv_async_init(&hex_event_loop, &hex_event_async, hex_event_async_callback) != 0) {
        // The loop has no handle yet, so closing it releases its state.
        (void)uv_loop_close(&hex_event_loop);
        hex_runtime_trap("[Runtime Error] event runtime initialization failed\n");
    }
    uv_mutex_lock(&hex_event_ready_mutex);
    hex_event_ready = true;
    uv_cond_signal(&hex_event_ready_cond);
    uv_mutex_unlock(&hex_event_ready_mutex);
    uv_run(&hex_event_loop, UV_RUN_DEFAULT);
}

// The standard-output guard is established here, before any Task can submit
// output, and is never destroyed: the loop thread outlives every Hexal scope,
// so there is no point at which no output job can still be in flight.
void hex_stdout_lock(void) {
    uv_mutex_lock(&hex_event_stdout_mutex);
}

void hex_stdout_unlock(void) {
    uv_mutex_unlock(&hex_event_stdout_mutex);
}

void hex_event_runtime_init(void) {
    if (uv_mutex_init(&hex_event_queue_mutex) != 0 || uv_mutex_init(&hex_event_stdout_mutex) != 0 ||
        uv_mutex_init(&hex_event_ready_mutex) != 0 || uv_cond_init(&hex_event_ready_cond) != 0) {
        hex_runtime_trap("[Runtime Error] event runtime initialization failed\n");
    }
    if (uv_thread_create(&hex_event_thread, hex_event_loop_thread, nullptr) != 0) {
        hex_runtime_trap("[Runtime Error] event loop thread creation failed\n");
    }
    uv_mutex_lock(&hex_event_ready_mutex);
    while (!hex_event_ready) {
        uv_cond_wait(&hex_event_ready_cond, &hex_event_ready_mutex);
    }
    uv_mutex_unlock(&hex_event_ready_mutex);
}

// hex_event_park publishes one armed request and parks its Task. The Task is
// armed before publication, so a completion racing the park commit resumes
// it exactly once through the common Task transition.
static void hex_event_park(hex_task *task, hex_event_command *command) {
    hex_task_event_arm(task, command);
    hex_event_enqueue(command);
    hex_task_event_suspend(task);
}
{{- if .Handle}}

void hex_event_submit(hex_task *task, hex_event_command *command) {
    hex_event_park(task, command);
}

void *hex_event_loop_handle(void) {
    return &hex_event_loop;
}
{{- end}}

static void hex_event_work_run(uv_work_t *request) {
    hex_event_work *work = (hex_event_work *)request->data;
    work->entry(work->context);
}

static void hex_event_work_complete(uv_work_t *request, int status) {
    hex_event_work *work = (hex_event_work *)request->data;
    if (status != 0) {
        work->failure(work->context);
    }
    hex_task_event_wake(work->command.task);
}

static void hex_event_work_start(hex_event_command *command) {
    hex_event_work *work = (hex_event_work *)command;
    work->request.data = work;
    if (uv_queue_work(&hex_event_loop, &work->request, hex_event_work_run, hex_event_work_complete) != 0) {
        work->failure(work->context);
        hex_task_event_wake(work->command.task);
    }
}

// hex_event_work_call runs one blocking native entry. Outside a Task it runs
// directly. Inside a Task the request record lives on the parked fiber stack
// and the terminal callback writes the result before the one wake.
void hex_event_work_call(hex_event_work_entry entry, hex_event_work_failure failure, void *context) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        entry(context);
        return;
    }
    hex_event_work work = {.command = {.task = task, .start = hex_event_work_start}, .entry = entry, .failure = failure, .context = context};
    hex_event_park(task, &work.command);
}
{{- if .Sleep}}

// One Task sleep: a one-shot timer on the loop thread. libuv timers count
// whole loop milliseconds, so every arm rounds the remaining interval up and
// an early expiry, judged by the monotonic clock, re-arms instead of waking.
typedef struct hex_event_sleep {
    hex_event_command command;
    uv_timer_t timer;
    uint64_t start;
    uint64_t duration;
} hex_event_sleep;

static uint64_t hex_event_sleep_millis(uint64_t nanoseconds) {
    uint64_t millis = nanoseconds / 1000000u;
    return nanoseconds % 1000000u == 0 ? millis : millis + 1;
}

// The close callback is the terminal owner: the timer is fully released
// before the one wake, so the resumed Task may reclaim its stack record.
static void hex_event_sleep_closed(uv_handle_t *handle) {
    hex_event_sleep *sleep = (hex_event_sleep *)handle->data;
    hex_task_event_wake(sleep->command.task);
}

static void hex_event_sleep_fired(uv_timer_t *timer) {
    hex_event_sleep *sleep = (hex_event_sleep *)timer->data;
    // Unsigned subtraction of two monotonic readings cannot reverse: the
    // accepted duration is below half the counter range.
    uint64_t elapsed = uv_hrtime() - sleep->start;
    if (elapsed < sleep->duration) {
        if (uv_timer_start(timer, hex_event_sleep_fired, hex_event_sleep_millis(sleep->duration - elapsed), 0) != 0) {
            hex_runtime_trap("[Runtime Error] task sleep failed\n");
        }
        return;
    }
    uv_close((uv_handle_t *)timer, hex_event_sleep_closed);
}

static void hex_event_sleep_start(hex_event_command *command) {
    hex_event_sleep *sleep = (hex_event_sleep *)command;
    if (uv_timer_init(&hex_event_loop, &sleep->timer) != 0) {
        hex_runtime_trap("[Runtime Error] task sleep failed\n");
    }
    sleep->timer.data = sleep;
    if (uv_timer_start(&sleep->timer, hex_event_sleep_fired, hex_event_sleep_millis(sleep->duration), 0) != 0) {
        hex_runtime_trap("[Runtime Error] task sleep failed\n");
    }
}

void hex_task_sleep(hex_duration duration) {
    if (duration == 0) {
        return;
    }
    if (duration > (uint64_t)INT64_MAX) {
        hex_runtime_trap("[Runtime Error] sleep duration too large\n");
    }
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] task sleep failed\n");
    }
    hex_event_sleep sleep = {.command = {.task = task, .start = hex_event_sleep_start}, .start = uv_hrtime(), .duration = duration};
    hex_event_park(task, &sleep.command);
}
{{- end}}
