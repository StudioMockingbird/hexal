/* Concurrency runtime: the fiber platform layer, the M:N scheduler, the
   Task/join/yield machinery, and the Channel and Mutex cores. Process-wide
   state and the externally linked core definitions live here exactly once;
   the program-wide declarations are in the matching header. */
{{if .Scheduler}}
// The platform layer uses POSIX extensions (ucontext, mmap, sigaltstack)
// whose declarations glibc and musl hide in strict C23 mode; _GNU_SOURCE
// must precede every system include.
#define _GNU_SOURCE
{{end}}
#include "hexal/concurrency.h"
{{if .Scheduler}}
#if defined(_WIN32)
#include <windows.h>
#else
#include <ucontext.h>
#include <signal.h>
#include <sys/mman.h>
#include <unistd.h>
#endif
#if defined(__STDC_NO_THREADS__)
#error "Hexal Task runtime requires C23 threads (<threads.h>); this toolchain defines __STDC_NO_THREADS__"
#endif
#include <threads.h>
#include <stdatomic.h>
#include <string.h>

#define HEX_TASK_READY 1
#define HEX_TASK_RUNNING 2
#define HEX_TASK_PARKED 3
#define HEX_TASK_DONE 4
#define HEX_TASK_ROOT 1u
#define HEX_TASK_DETACH 2u

// hex_stack_overflow_message is the one structured diagnostic emitted from a
// signal or exception context: the handler cannot call hex_runtime_trap,
// whose fputs and abort are not async-signal-safe.
static const char hex_stack_overflow_message[] = "[Runtime Error] task stack overflow\n";

#if defined(_WIN32)
typedef LPVOID hex_context;

// The Windows backend uses the verified Fiber APIs. Worker threads convert
// themselves once; every Task gets a fresh CreateFiberEx stack. x64 uses one
// calling convention, so the CALLBACK cast is exact.
static int hex_logical_processors(void) {
    SYSTEM_INFO info;
    GetSystemInfo(&info);
    DWORD count = info.dwNumberOfProcessors;
    return count > 0 ? (int)count : 1;
}
static hex_context hex_context_create(void (*entry)(void *), void *param) {
    return CreateFiberEx({{.FiberCommit}}, {{.FiberReserve}}, FIBER_FLAG_FLOAT_SWITCH, (LPFIBER_START_ROUTINE)entry, param);
}
static hex_context hex_context_current(void) {
    // The calling thread must already be a fiber; the scheduler establishes
    // that before any task runs.
    return GetCurrentFiber();
}
static hex_context hex_context_thread(void) {
    return ConvertThreadToFiberEx(nullptr, FIBER_FLAG_FLOAT_SWITCH);
}
static void hex_context_switch(hex_context from, hex_context to) {
    (void)from;
    SwitchToFiber(to);
}
static void hex_context_destroy(hex_context context) {
    DeleteFiber(context);
}
// hex_stack_overflow_handler turns the fiber stack overflow into the
// structured trap. The exception code identifies the fault, so no guard-range
// bookkeeping exists here; the handler runs on a reserved system stack and
// must not call into the CRT.
static LONG WINAPI hex_stack_overflow_handler(EXCEPTION_POINTERS *exception) {
    if (exception->ExceptionRecord->ExceptionCode == EXCEPTION_STACK_OVERFLOW) {
        HANDLE stderr_handle = GetStdHandle(STD_ERROR_HANDLE);
        DWORD written = 0;
        (void)WriteFile(stderr_handle, hex_stack_overflow_message, (DWORD)(sizeof(hex_stack_overflow_message) - 1), &written, nullptr);
        ExitProcess(1);
    }
    return EXCEPTION_CONTINUE_SEARCH;
}
static void hex_worker_guard_setup(void) {
    static _Atomic int installed;
    if (atomic_exchange(&installed, 1)) {
        return;
    }
    if (AddVectoredExceptionHandler(1, hex_stack_overflow_handler) == nullptr) {
        hex_runtime_trap("[Runtime Error] stack overflow handler installation failed\n");
    }
}
#else
typedef struct hex_context_impl hex_context_impl;
struct hex_context_impl {
    ucontext_t context;
    void *stack;
    size_t stack_mapping_size;
    void *guard_end;
};

// The signal handler runs on an alternate stack of this size: it only reads
// a TLS range, compares, writes, and exits, so a handful of pages suffices.
#define HEX_GUARD_HANDLER_STACK (64 << 10)

// The guard range of the context the worker is currently running: a task
// fiber's guard page, or nothing while the worker is on its own stack. The
// signal handler reads this instead of a locked global registry, which would
// not be async-signal-safe; the scheduler updates it on every switch.
typedef struct hex_guard_range {
    const void *base;
    const void *end;
} hex_guard_range;
static _Thread_local hex_guard_range hex_current_guard;

// hex_guard_handler turns a fault inside the current Task's guard page into
// the structured stack-overflow trap and re-raises every other fault
// unchanged, so unrelated memory bugs keep their ordinary fatal behavior. It
// is async-signal-safe: one TLS read, one range comparison, write, _exit.
static void hex_guard_handler(int sig, siginfo_t *info, void *unused) {
    (void)unused;
    hex_guard_range guard = hex_current_guard;
    if (guard.base != nullptr && info->si_addr >= guard.base && info->si_addr < guard.end) {
        (void)write(STDERR_FILENO, hex_stack_overflow_message, sizeof(hex_stack_overflow_message) - 1);
        _exit(1);
    }
    signal(sig, SIG_DFL);
    raise(sig);
}
static void hex_worker_guard_setup(void) {
    // The handler is process-wide, the alt stack per-thread: every worker
    // installs its own stack, one installs the handler. The alt stack is
    // never freed because workers run until the process exits on root
    // completion.
    stack_t alt = {0};
    alt.ss_sp = malloc(HEX_GUARD_HANDLER_STACK);
    if (alt.ss_sp == nullptr) {
        hex_runtime_trap("[Runtime Error] stack overflow handler stack allocation failed\n");
    }
    alt.ss_size = HEX_GUARD_HANDLER_STACK;
    if (sigaltstack(&alt, nullptr) != 0) {
        hex_runtime_trap("[Runtime Error] stack overflow handler stack installation failed\n");
    }
    static _Atomic int installed;
    if (atomic_exchange(&installed, 1)) {
        return;
    }
    struct sigaction action = {0};
    action.sa_sigaction = hex_guard_handler;
    action.sa_flags = SA_SIGINFO | SA_ONSTACK;
    sigemptyset(&action.sa_mask);
    if (sigaction(SIGSEGV, &action, nullptr) != 0 || sigaction(SIGBUS, &action, nullptr) != 0) {
        hex_runtime_trap("[Runtime Error] stack overflow handler installation failed\n");
    }
}

// The POSIX backend uses System V ucontext with one caller-allocated stack
// per Task. The scheduler thread's own context is captured once and reused
// for every switch back into the worker loop.
static int hex_logical_processors(void) {
    long count = sysconf(_SC_NPROCESSORS_ONLN);
    return count > 0 ? (int)count : 1;
}
static hex_context_impl *hex_context_create(void (*entry)(void *), void *param) {
    const size_t stack_size = {{.StackSizeExpression}};
    const size_t page_size = (size_t)sysconf(_SC_PAGESIZE);
    hex_context_impl *context = (hex_context_impl *)malloc(sizeof(hex_context_impl));
    if (context == nullptr) {
        return nullptr;
    }
    // The fiber stack maps the whole reserve read-write with one PROT_NONE
    // guard page at its low end: demand-zero paging grows it, an overflow
    // faults instead of corrupting adjacent heap, and untouched pages hold no
    // physical memory. The initial commit (the TaskStackCommit project
    // setting) is a Windows-only knob and is deliberately unused here;
    // nothing on POSIX needs pre-committing. ss_sp/ss_size name the usable
    // region above the guard, and the whole mapping is torn down with
    // munmap.
    void *region = mmap(nullptr, stack_size, PROT_READ | PROT_WRITE,
                        MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE, -1, 0);
    if (region == MAP_FAILED) {
        free(context);
        return nullptr;
    }
    if (mprotect(region, page_size, PROT_NONE) != 0) {
        munmap(region, stack_size);
        free(context);
        return nullptr;
    }
    context->stack = region;
    context->stack_mapping_size = stack_size;
    context->guard_end = (char *)region + page_size;
    if (getcontext(&context->context) != 0) {
        munmap(context->stack, context->stack_mapping_size);
        free(context);
        return nullptr;
    }
    context->context.uc_stack.ss_sp = (char *)region + page_size;
    context->context.uc_stack.ss_size = stack_size - page_size;
    context->context.uc_link = nullptr;
    makecontext(&context->context, (void (*)(void))entry, 1, param);
    return context;
}
static hex_context_impl *hex_context_current(void) {
    hex_context_impl *context = (hex_context_impl *)malloc(sizeof(hex_context_impl));
    if (context != nullptr) {
        context->stack = nullptr;
        context->guard_end = nullptr;
        if (getcontext(&context->context) != 0) {
            free(context);
            return nullptr;
        }
    }
    return context;
}
static hex_context_impl *hex_context_thread(void) {
    // The thread's ordinary context is its own scheduler context.
    return hex_context_current();
}
static void hex_context_switch(hex_context_impl *from, hex_context_impl *to) {
    if (from == nullptr || to == nullptr) {
        abort();
    }
    // The worker is about to run `to`: name its guard range so a fault is
    // attributed to the right Task. Loop and thread contexts carry no guard,
    // so switching back to the worker's own stack clears the range.
    hex_current_guard.base = to->stack;
    hex_current_guard.end = to->guard_end;
    if (swapcontext(&from->context, &to->context) != 0) {
        abort();
    }
}
static void hex_context_destroy(hex_context_impl *context) {
    if (context != nullptr) {
        if (context->stack != nullptr) {
            munmap(context->stack, context->stack_mapping_size);
        }
        free(context);
    }
}
typedef hex_context_impl *hex_context;
#endif

static _Thread_local hex_task *hex_current_task;
static hex_task *hex_ready_head;
static hex_task *hex_ready_tail;
static mtx_t hex_ready_mutex;
static cnd_t hex_ready_cond;
static _Atomic int hex_shutdown;
static _Atomic int64_t hex_next_task_id;
hex_task *hex_root_task;

static void hex_ready_push(hex_task *task) {
    mtx_lock(&hex_ready_mutex);
    task->ready_next = nullptr;
    if (hex_ready_tail != nullptr) {
        hex_ready_tail->ready_next = task;
    } else {
        hex_ready_head = task;
    }
    hex_ready_tail = task;
    cnd_signal(&hex_ready_cond);
    mtx_unlock(&hex_ready_mutex);
}

static hex_task *hex_ready_pop(void) {
    hex_task *task = hex_ready_head;
    if (task != nullptr) {
        hex_ready_head = task->ready_next;
        if (hex_ready_head == nullptr) {
            hex_ready_tail = nullptr;
        }
    }
    return task;
}

void hex_task_release(hex_task *task) {
    if (task->flags & HEX_TASK_ROOT) {
        return;
    }
    if (task->fiber != nullptr) {
        hex_context_destroy((hex_context)task->fiber);
    }
    free(task->args);
    free(task->result);
    free(task);
}

// hex_task_complete marks the task done, wakes one joiner, stops the
// scheduler when the root completes, and hands the worker back to its
// dispatch loop. The trampoline and the root epilogue both end here; the
// task's fiber never returns through an invalid stack.
void hex_task_complete(hex_task *task) {
    task->state = HEX_TASK_DONE;
    if (task->joiner != nullptr) {
        hex_task *joiner = task->joiner;
        task->joiner = nullptr;
        joiner->state = HEX_TASK_READY;
        hex_ready_push(joiner);
    }
    if (task->flags & HEX_TASK_ROOT) {
        atomic_store(&hex_shutdown, 1);
        cnd_broadcast(&hex_ready_cond);
    }
    hex_current_task = nullptr;
    hex_context_switch((hex_context)task->fiber, (hex_context)task->scheduler_fiber);
}

// hex_task_trampoline is the one shared Task entry: every fiber begins here,
// runs the typed adapter, and never returns (a fiber function that returns
// would terminate its thread).
static void hex_task_trampoline(void *param) {
    hex_task *task = (hex_task *)param;
    task->entry(task);
    abort();
}

// hex_worker_loop is the dispatcher of one worker. Worker zero (the initial
// process thread) switches back into the root fiber when the scheduler stops
// so generated main returns normally; other workers return from their thread
// function.
static void hex_worker_loop(void *param) {
    const int is_worker_zero = (param != nullptr);
    hex_context loop_context = hex_context_current();
    for (;;) {
        mtx_lock(&hex_ready_mutex);
        while (hex_ready_head == nullptr && !atomic_load(&hex_shutdown)) {
            cnd_wait(&hex_ready_cond, &hex_ready_mutex);
        }
        if (atomic_load(&hex_shutdown)) {
            mtx_unlock(&hex_ready_mutex);
            if (is_worker_zero) {
                hex_context_switch(loop_context, (hex_context)hex_root_task->fiber);
            }
            return;
        }
        hex_task *task = hex_ready_pop();
        mtx_unlock(&hex_ready_mutex);
        hex_current_task = task;
        task->state = HEX_TASK_RUNNING;
        task->scheduler_fiber = (void *)loop_context;
        hex_context_switch(loop_context, (hex_context)task->fiber);
        if (task->state == HEX_TASK_DONE && (task->flags & HEX_TASK_DETACH)) {
            hex_task_release(task);
        }
    }
}

static int hex_worker_thread(void *unused) {
    (void)unused;
    hex_current_task = nullptr;
    hex_worker_guard_setup();
    (void)hex_context_thread();
    hex_worker_loop(nullptr);
    return 0;
}

// hex_scheduler_init establishes the root task on the initial process thread
// (worker zero), creates the remaining workers, and starts dispatch. The
// root fiber is the converted main thread context; its statements run as the
// Hexal entry point.
void hex_scheduler_init(void) {
    // Worker zero is the initial process thread; its overflow handler and
    // alternate signal stack are established before any Task runs.
    hex_worker_guard_setup();
    if (mtx_init(&hex_ready_mutex, mtx_plain) != thrd_success) {
        hex_runtime_trap("[Runtime Error] scheduler mutex initialization failed\n");
    }
    if (cnd_init(&hex_ready_cond) != thrd_success) {
        hex_runtime_trap("[Runtime Error] scheduler condition variable initialization failed\n");
    }
    hex_root_task = (hex_task *)calloc(1, sizeof(hex_task));
    if (hex_root_task == nullptr) {
        hex_runtime_trap("[Runtime Error] scheduler allocation failed\n");
    }
    hex_root_task->fiber = (void *)hex_context_thread();
    if (hex_root_task->fiber == nullptr) {
        hex_runtime_trap("[Runtime Error] scheduler fiber initialization failed\n");
    }
    hex_root_task->id = atomic_fetch_add(&hex_next_task_id, 1);
    hex_root_task->state = HEX_TASK_READY;
    hex_root_task->flags = HEX_TASK_ROOT;
    hex_root_task->scheduler_fiber = (void *)hex_context_create(hex_worker_loop, (void *)1);
    if (hex_root_task->scheduler_fiber == nullptr) {
        hex_runtime_trap("[Runtime Error] scheduler worker-zero context creation failed\n");
    }
    hex_current_task = hex_root_task;
    int logical = hex_logical_processors();
    if (logical < 1) {
        logical = 1;
    }
    for (int index = 1; index < logical; index++) {
        thrd_t thread;
        if (thrd_create(&thread, hex_worker_thread, nullptr) != thrd_success) {
            hex_runtime_trap("[Runtime Error] scheduler worker creation failed\n");
        }
        thrd_detach(thread);
    }
    hex_context_switch((hex_context)hex_root_task->fiber, (hex_context)hex_root_task->scheduler_fiber);
}

void hex_task_yield(void) {
    hex_task *self = hex_current_task;
    self->state = HEX_TASK_READY;
    hex_ready_push(self);
    hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
}

// hex_task_spawn allocates the argument frame and task control block,
// shallow-copies the arguments, creates the Task fiber, and publishes the
// task to the ready queue. Any partial allocation failure releases every
// resource and returns nullptr, which the spawn site turns into Error.
hex_task *hex_task_spawn(hex_task_entry entry, size_t args_size, size_t args_align, const void *args, size_t result_size, size_t result_align) {
    (void)args_align;
    (void)result_align;
    void *args_frame = nullptr;
    if (args_size > 0) {
        args_frame = malloc(args_size);
        if (args_frame == nullptr) {
            return nullptr;
        }
        memcpy(args_frame, args, args_size);
    }
    hex_task *task = (hex_task *)calloc(1, sizeof(hex_task));
    if (task == nullptr) {
        free(args_frame);
        return nullptr;
    }
    void *result_frame = nullptr;
    if (result_size > 0) {
        result_frame = malloc(result_size);
        if (result_frame == nullptr) {
            free(task);
            free(args_frame);
            return nullptr;
        }
    }
    task->fiber = (void *)hex_context_create(hex_task_trampoline, task);
    if (task->fiber == nullptr) {
        free(result_frame);
        free(task);
        free(args_frame);
        return nullptr;
    }
    task->id = atomic_fetch_add(&hex_next_task_id, 1);
    task->state = HEX_TASK_READY;
    task->entry = entry;
    task->args = args_frame;
    task->result = result_frame;
    hex_ready_push(task);
    return task;
}

// hex_task_join waits for the task to finish and returns its result frame.
// The joining task parks on the target's joiner slot while waiting. Joining
// the current task through its own alias is a cheaply detectable misuse and
// traps.
void *hex_task_join(hex_task *task) {
    for (;;) {
        if (task->state == HEX_TASK_DONE) {
            return task->result;
        }
        hex_task *self = hex_current_task;
        if (task == self) {
            hex_runtime_trap("[Runtime Error] cannot join the current task\n");
        }
        self->state = HEX_TASK_PARKED;
        task->joiner = self;
        hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
    }
}

void hex_task_detach(hex_task *task) {
    task->flags |= HEX_TASK_DETACH;
    if (task->state == HEX_TASK_DONE) {
        hex_task_release(task);
    }
}
{{end}}{{if .Channels}}
typedef struct hex_chan {
    mtx_t mutex;
    hex_task *wait_send;
    hex_task *wait_recv;
    size_t capacity;
    size_t length;
    size_t head;
    size_t element_size;
    uint8_t closed;
    uint8_t *slots;
} hex_chan;

hex_chan *hex_chan_new(size_t capacity, size_t element_size) {
    if (capacity == 0) {
        return nullptr;
    }
    size_t slots_bytes;
    if (ckd_mul(&slots_bytes, element_size, capacity)) {
        return nullptr;
    }
    hex_chan *channel = (hex_chan *)calloc(1, sizeof(hex_chan));
    if (channel == nullptr) {
        return nullptr;
    }
    channel->slots = (uint8_t *)malloc(slots_bytes);
    if (channel->slots == nullptr) {
        free(channel);
        return nullptr;
    }
    if (mtx_init(&channel->mutex, mtx_plain) != thrd_success) {
        free(channel->slots);
        free(channel);
        return nullptr;
    }
    channel->capacity = capacity;
    channel->element_size = element_size;
    return channel;
}

// send shallow-copies one element into the ring, parking while the channel
// is full and open. A wake from close, or a send after close, fails.
bool hex_chan_send(hex_chan *channel, const void *value) {
    hex_task *self = hex_current_task;
    for (;;) {
        mtx_lock(&channel->mutex);
        if (channel->closed) {
            mtx_unlock(&channel->mutex);
            return false;
        }
        if (channel->length < channel->capacity) {
            break;
        }
        self->state = HEX_TASK_PARKED;
        self->wake_error = 0;
        self->wait_next = channel->wait_send;
        channel->wait_send = self;
        mtx_unlock(&channel->mutex);
        hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
        if (self->wake_error) {
            return false;
        }
    }
    size_t tail = (channel->head + channel->length) % channel->capacity;
    memcpy(channel->slots + tail * channel->element_size, value, channel->element_size);
    channel->length++;
    hex_task *waiter = channel->wait_recv;
    if (waiter != nullptr) {
        channel->wait_recv = waiter->wait_next;
        waiter->state = HEX_TASK_READY;
        hex_ready_push(waiter);
    }
    mtx_unlock(&channel->mutex);
    return true;
}

// receive copies the oldest element out, parking while the channel is empty
// and open. Closed-and-drained is the one recoverable failure (EoS).
bool hex_chan_receive(hex_chan *channel, void *out) {
    hex_task *self = hex_current_task;
    for (;;) {
        mtx_lock(&channel->mutex);
        if (channel->length > 0) {
            break;
        }
        if (channel->closed) {
            mtx_unlock(&channel->mutex);
            return false;
        }
        self->state = HEX_TASK_PARKED;
        self->wake_error = 0;
        self->wait_next = channel->wait_recv;
        channel->wait_recv = self;
        mtx_unlock(&channel->mutex);
        hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
    }
    memcpy(out, channel->slots + channel->head * channel->element_size, channel->element_size);
    channel->head = (channel->head + 1) % channel->capacity;
    channel->length--;
    hex_task *waiter = channel->wait_send;
    if (waiter != nullptr) {
        channel->wait_send = waiter->wait_next;
        waiter->wake_error = 0;
        waiter->state = HEX_TASK_READY;
        hex_ready_push(waiter);
    }
    mtx_unlock(&channel->mutex);
    return true;
}

// close is idempotent: it wakes every blocked sender (with an Error) and
// receiver (with EoS) and never discards queued values.
void hex_chan_close(hex_chan *channel) {
    mtx_lock(&channel->mutex);
    channel->closed = true;
    hex_task *waiter = channel->wait_send;
    while (waiter != nullptr) {
        hex_task *next = waiter->wait_next;
        waiter->wake_error = 1;
        waiter->state = HEX_TASK_READY;
        hex_ready_push(waiter);
        waiter = next;
    }
    channel->wait_send = nullptr;
    waiter = channel->wait_recv;
    while (waiter != nullptr) {
        hex_task *next = waiter->wait_next;
        waiter->state = HEX_TASK_READY;
        hex_ready_push(waiter);
        waiter = next;
    }
    channel->wait_recv = nullptr;
    mtx_unlock(&channel->mutex);
}

size_t hex_chan_length(hex_chan *channel) {
    mtx_lock(&channel->mutex);
    size_t length = channel->length;
    mtx_unlock(&channel->mutex);
    return length;
}

size_t hex_chan_capacity(hex_chan *channel) {
    return channel->capacity;
}

bool hex_chan_is_closed(hex_chan *channel) {
    mtx_lock(&channel->mutex);
    bool closed = channel->closed;
    mtx_unlock(&channel->mutex);
    return closed;
}

// free requires a closed, empty channel with no blocked tasks; any other
// state is a cheaply detectable programmer error and traps.
void hex_chan_free(hex_chan *channel) {
    if (channel->wait_send != nullptr || channel->wait_recv != nullptr) {
        hex_runtime_trap("[Runtime Error] channel free while tasks are blocked on it\n");
    }
    if (!channel->closed || channel->length > 0) {
        hex_runtime_trap("[Runtime Error] channel free requires a closed, empty channel\n");
    }
    mtx_destroy(&channel->mutex);
    free(channel->slots);
    free(channel);
}
{{end}}{{if .Mutex}}
struct hex_mutex_control {
    mtx_t mutex;
    hex_task *owner;
    hex_task *wait_list;
};

hex_mutex *hex_mutex_new(void) {
    hex_mutex *mutex = (hex_mutex *)calloc(1, sizeof(hex_mutex));
    if (mutex == nullptr) {
        return nullptr;
    }
    if (mtx_init(&mutex->mutex, mtx_plain) != thrd_success) {
        free(mutex);
        return nullptr;
    }
    return mutex;
}

void hex_mutex_lock(hex_mutex *mutex) {
    hex_task *self = hex_current_task;
    for (;;) {
        mtx_lock(&mutex->mutex);
        if (mutex->owner == nullptr) {
            mutex->owner = self;
            mtx_unlock(&mutex->mutex);
            return;
        }
        if (mutex->owner == self) {
            mtx_unlock(&mutex->mutex);
            hex_runtime_trap("[Runtime Error] recursive mutex lock\n");
        }
        self->state = HEX_TASK_PARKED;
        self->wait_next = mutex->wait_list;
        mutex->wait_list = self;
        mtx_unlock(&mutex->mutex);
        hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
    }
}

void hex_mutex_unlock(hex_mutex *mutex) {
    mtx_lock(&mutex->mutex);
    if (mutex->owner != hex_current_task) {
        mtx_unlock(&mutex->mutex);
        hex_runtime_trap("[Runtime Error] mutex unlock by a non-owner\n");
    }
    hex_task *waiter = mutex->wait_list;
    if (waiter != nullptr) {
        mutex->wait_list = waiter->wait_next;
        mutex->owner = waiter;
        waiter->state = HEX_TASK_READY;
        hex_ready_push(waiter);
    } else {
        mutex->owner = nullptr;
    }
    mtx_unlock(&mutex->mutex);
}

void hex_mutex_free(hex_mutex *mutex) {
    if (mutex->owner != nullptr || mutex->wait_list != nullptr) {
        hex_runtime_trap("[Runtime Error] mutex free while locked or awaited\n");
    }
    mtx_destroy(&mutex->mutex);
    free(mutex);
}
{{end}}