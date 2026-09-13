/* Concurrency runtime: the fiber platform layer, the M:N scheduler, the
   Task/join/yield machinery, and the Channel and Mutex cores. Process-wide
   state and the externally linked core definitions live here exactly once;
   the program-wide declarations are in the matching header. */
{{if .Scheduler}}{{if not .TargetWindows}}
// The platform layer uses POSIX extensions (ucontext, mmap, sigaltstack)
// whose declarations glibc and musl hide in strict C23 mode; _GNU_SOURCE
// must precede every system include.
#define _GNU_SOURCE
{{end}}{{end}}
#include "hexal/concurrency.h"
#include "hexal/heap.h"
#include <uv.h>
{{if .Event}}#include "hexal/event.h"
{{end -}}
{{if .Scheduler}}
#if defined(_WIN32)
#include <windows.h>
{{if not .TargetWindows -}}
#else
#include <ucontext.h>
#include <signal.h>
#include <sys/mman.h>
#include <unistd.h>
{{end -}}
#endif

static bool hex_mutex_raw_init(hex_mutex_raw *mutex) {
    uv_mutex_t *native = (uv_mutex_t *)hex_heap_allocate_or_null(sizeof(uv_mutex_t));
    if (native == nullptr || uv_mutex_init(native) != 0) {
        hex_heap_free(native);
        return false;
    }
    mutex->native = native;
    return true;
}
static void hex_mutex_raw_lock(hex_mutex_raw *mutex) {
    uv_mutex_lock((uv_mutex_t *)mutex->native);
}
static void hex_mutex_raw_unlock(hex_mutex_raw *mutex) {
    uv_mutex_unlock((uv_mutex_t *)mutex->native);
}
static void hex_mutex_raw_destroy(hex_mutex_raw *mutex) {
    if (mutex->native != nullptr) {
        uv_mutex_destroy((uv_mutex_t *)mutex->native);
        hex_heap_free(mutex->native);
        mutex->native = nullptr;
    }
}
static void hex_cond_init(hex_cond *cond) {
    uv_cond_t *native = (uv_cond_t *)hex_heap_allocate_or_null(sizeof(uv_cond_t));
    if (native == nullptr || uv_cond_init(native) != 0) {
        hex_heap_free(native);
        hex_runtime_trap("[Runtime Error] condition initialization failed\n");
    }
    cond->native = native;
}
static void hex_cond_wait(hex_cond *cond, hex_mutex_raw *mutex) {
    uv_cond_wait((uv_cond_t *)cond->native, (uv_mutex_t *)mutex->native);
}
static void hex_cond_signal(hex_cond *cond) {
    uv_cond_signal((uv_cond_t *)cond->native);
}
static void hex_cond_broadcast(hex_cond *cond) {
    uv_cond_broadcast((uv_cond_t *)cond->native);
}
[[maybe_unused]] static void hex_cond_destroy(hex_cond *cond) {
    if (cond->native != nullptr) {
        uv_cond_destroy((uv_cond_t *)cond->native);
        hex_heap_free(cond->native);
        cond->native = nullptr;
    }
}

static void hex_worker_thread(void *argument);
static bool hex_thread_spawn_detached(void (*entry)(void *), void *argument) {
    uv_thread_t thread;
    if (uv_thread_create(&thread, entry, argument) != 0) {
        return false;
    }
    if (uv_thread_detach(&thread) != 0) {
        hex_runtime_trap("[Runtime Error] thread detach failed\n");
    }
    return true;
}
#include <stdatomic.h>
#include <string.h>

// hex_task's park_phase: the common suspend/wake protocol shared by yield,
// join, Channel, and Mutex. running has no pending park; parking is
// registered but its fiber has not yet switched out; parked means the
// dispatcher confirmed the switch with no waker racing ahead; notified means
// a waker recorded an early wake before that confirmation; ready means the
// task is published on the ready queue exactly once and may resume. Values
// are chosen so calloc's zero-initialization starts a task at running.
#define HEX_PARK_RUNNING 0
#define HEX_PARK_PARKING 1
#define HEX_PARK_PARKED 2
#define HEX_PARK_NOTIFIED 3
#define HEX_PARK_READY 4

// hex_task's life: completion status, guarded entirely by the task's own
// lifecycle_mutex and independent of park_phase. running is the default;
// completing means the task's own fiber has switched out after finishing but
// before the dispatcher records done, so it is not yet reclaimable; done
// means the target fiber will never run again.
#define HEX_LIFE_RUNNING 0
#define HEX_LIFE_COMPLETING 1
#define HEX_LIFE_DONE 2

#define HEX_TASK_CLAIM_NONE 0
#define HEX_TASK_CLAIM_JOIN 1
#define HEX_TASK_CLAIM_DETACH 2

#define HEX_TASK_ROOT 1u

// hex_stack_overflow_message is the one structured diagnostic emitted from a
// signal or exception context: the handler cannot call hex_runtime_trap,
// whose fputs and abort are not async-signal-safe.
static const char hex_stack_overflow_message[] = "[Runtime Error] task stack overflow\n";

#if defined(_WIN32)
typedef LPVOID hex_context;

// The Windows backend uses the verified Fiber APIs. Worker threads convert
// themselves once; every Task gets a fresh CreateFiberEx stack. x64 uses one
// calling convention, so the CALLBACK cast is exact.
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

{{if not .TargetWindows -}}
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
    alt.ss_sp = hex_heap_allocate_or_null(HEX_GUARD_HANDLER_STACK);
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
static hex_context_impl *hex_context_create(void (*entry)(void *), void *param) {
    const size_t stack_size = {{.StackSizeExpression}};
    const size_t page_size = (size_t)sysconf(_SC_PAGESIZE);
    hex_context_impl *context = (hex_context_impl *)hex_heap_allocate_or_null(sizeof(hex_context_impl));
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
        hex_heap_free(context);
        return nullptr;
    }
    if (mprotect(region, page_size, PROT_NONE) != 0) {
        munmap(region, stack_size);
        hex_heap_free(context);
        return nullptr;
    }
    context->stack = region;
    context->stack_mapping_size = stack_size;
    context->guard_end = (char *)region + page_size;
    if (getcontext(&context->context) != 0) {
        munmap(context->stack, context->stack_mapping_size);
        hex_heap_free(context);
        return nullptr;
    }
    context->context.uc_stack.ss_sp = (char *)region + page_size;
    context->context.uc_stack.ss_size = stack_size - page_size;
    context->context.uc_link = nullptr;
    makecontext(&context->context, (void (*)(void))entry, 1, param);
    return context;
}
static hex_context_impl *hex_context_current(void) {
    hex_context_impl *context = (hex_context_impl *)hex_heap_allocate_or_null(sizeof(hex_context_impl));
    if (context != nullptr) {
        context->stack = nullptr;
        context->guard_end = nullptr;
        if (getcontext(&context->context) != 0) {
            hex_heap_free(context);
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
        hex_heap_free(context);
    }
}
typedef hex_context_impl *hex_context;

{{end -}}
#endif


static _Thread_local hex_task *hex_current_task;
static hex_task *hex_ready_head;
static hex_task *hex_ready_tail;
static hex_mutex_raw hex_ready_mutex;
static hex_cond hex_ready_cond;
static _Atomic int hex_shutdown;
static _Atomic int64_t hex_next_task_id;
hex_task *hex_root_task;

hex_task *hex_task_current(void) {
    return hex_current_task;
}

void hex_task_event_arm(hex_task *task, void *pending) {
    task->pending_park = pending;
    atomic_store_explicit(&task->park_phase, HEX_PARK_PARKING, memory_order_release);
}

// hex_ready_publish is the one shared ready-queue publication: it appends
// to the existing FIFO under the ready mutex, then signals one worker for
// an ordinary Task or broadcasts for root so worker zero necessarily wakes.
// A bare signal for root could wake only a non-zero worker that must skip
// it, losing the wake until an unrelated broadcast.
static void hex_ready_publish(hex_task *task) {
    hex_mutex_raw_lock(&hex_ready_mutex);
    task->ready_next = nullptr;
    if (hex_ready_tail != nullptr) {
        hex_ready_tail->ready_next = task;
    } else {
        hex_ready_head = task;
    }
    hex_ready_tail = task;
    if ((task->flags & HEX_TASK_ROOT) != 0) {
        hex_cond_broadcast(&hex_ready_cond);
    } else {
        hex_cond_signal(&hex_ready_cond);
    }
    hex_mutex_raw_unlock(&hex_ready_mutex);
}

// hex_ready_pop_head removes the FIFO head. Only worker zero uses it, so a
// child queued before a yielding root runs first on a one-worker target
// instead of root resuming itself immediately.
static hex_task *hex_ready_pop_head(void) {
    hex_task *task = hex_ready_head;
    if (task != nullptr) {
        hex_ready_head = task->ready_next;
        if (hex_ready_head == nullptr) {
            hex_ready_tail = nullptr;
        }
        task->ready_next = nullptr;
    }
    return task;
}

// hex_ready_pop_eligible removes the first non-root Task, skipping a queued
// root in place without removing or reordering it. It returns nullptr when
// no eligible non-root Task is queued, even if root is present. Every
// caller holds the ready mutex from its wait predicate through this call,
// so the check and the removal are one atomic selection.
static hex_task *hex_ready_pop_eligible(void) {
    hex_task *previous = nullptr;
    hex_task *task = hex_ready_head;
    while (task != nullptr && (task->flags & HEX_TASK_ROOT) != 0) {
        previous = task;
        task = task->ready_next;
    }
    if (task == nullptr) {
        return nullptr;
    }
    if (previous == nullptr) {
        hex_ready_head = task->ready_next;
    } else {
        previous->ready_next = task->ready_next;
    }
    if (task->ready_next == nullptr) {
        hex_ready_tail = previous;
    }
    task->ready_next = nullptr;
    return task;
}

// hex_ready_has_eligible reports whether a non-root Task is queued. Non-zero
// workers wait on this rather than queue emptiness so they sleep while only
// root is queued instead of spinning or touching root.
static bool hex_ready_has_eligible(void) {
    for (hex_task *task = hex_ready_head; task != nullptr; task = task->ready_next) {
        if ((task->flags & HEX_TASK_ROOT) == 0) {
            return true;
        }
    }
    return false;
}

// hex_task_wake applies the common wake transition to one parked waiter: the
// shared operation behind Channel wake, Mutex handoff, join completion, and
// the dispatcher's own yield notification. The caller writes any wake
// payload (wake_result, transferred Mutex ownership) before calling this, so
// every store here is a release that the resumed task's later acquire sees.
// Compare-exchanging parking to notified always succeeds unless the
// dispatcher already committed the fiber switch to parked, in which case
// this function performs the ready-queue publication itself; if the
// dispatcher has not yet committed, its own commit will observe notified and
// publish instead. Either way exactly one side publishes.
static void hex_task_wake(hex_task *waiter) {
    uint8_t expected = HEX_PARK_PARKING;
    if (atomic_compare_exchange_strong_explicit(&waiter->park_phase, &expected, HEX_PARK_NOTIFIED,
                                                 memory_order_release, memory_order_relaxed)) {
        return;
    }
    if (expected == HEX_PARK_PARKED) {
        uint8_t parked = HEX_PARK_PARKED;
        if (atomic_compare_exchange_strong_explicit(&waiter->park_phase, &parked, HEX_PARK_READY,
                                                     memory_order_release, memory_order_relaxed)) {
            hex_ready_publish(waiter);
        }
        // A losing retry here means the dispatcher's own commit already
        // published this task; the stale attempt does nothing further.
    }
    // notified, ready, or running: the wake is already recorded or this
    // waker is stale; either way nothing more to do.
}

void hex_task_event_wake(hex_task *waiter) {
    hex_task_wake(waiter);
}

// hex_task_commit_park runs on the dispatcher immediately after a parked
// task's fiber switches back, the mirror of hex_task_wake. Its own
// compare-exchange from parking to parked wins when no waker raced ahead,
// leaving the task genuinely suspended; it loses only to a waker that
// already recorded notified, in which case the dispatcher itself publishes.
static void hex_task_commit_park(hex_task *task) {
    uint8_t expected = HEX_PARK_PARKING;
    if (atomic_compare_exchange_strong_explicit(&task->park_phase, &expected, HEX_PARK_PARKED,
                                                 memory_order_acq_rel, memory_order_acquire)) {
        return;
    }
    if (expected != HEX_PARK_NOTIFIED) {
        hex_runtime_trap("[Runtime Error] invalid Task park phase during commit\n");
    }
    expected = HEX_PARK_NOTIFIED;
    if (!atomic_compare_exchange_strong_explicit(&task->park_phase, &expected, HEX_PARK_READY,
                                                  memory_order_acq_rel, memory_order_acquire)) {
        hex_runtime_trap("[Runtime Error] Task park phase changed during commit\n");
    }
    hex_ready_publish(task);
}

// hex_task_resume_commit runs on a resumed task's own fiber, immediately
// after its hex_context_switch call returns control to it. The acquire load
// synchronizes with whichever waker or dispatcher released this task into
// ready, making every write before that release (a wake payload, transferred
// Mutex ownership) visible here before the caller reads it.
static void hex_task_resume_commit(hex_task *self) {
    uint8_t expected = HEX_PARK_READY;
    if (!atomic_compare_exchange_strong_explicit(&self->park_phase, &expected, HEX_PARK_RUNNING,
                                                  memory_order_acquire, memory_order_relaxed)) {
        hex_runtime_trap("[Runtime Error] invalid Task park phase during resume\n");
    }
    self->pending_park = nullptr;
}

void hex_task_event_suspend(hex_task *task) {
    hex_context_switch((hex_context)task->fiber, (hex_context)task->scheduler_fiber);
    hex_task_resume_commit(task);
}

void hex_task_release(hex_task *task) {
    if (task->flags & HEX_TASK_ROOT) {
        return;
    }
    if (task->fiber != nullptr) {
        hex_context_destroy((hex_context)task->fiber);
    }
    hex_mutex_raw_destroy(&task->lifecycle_mutex);
    hex_heap_free(task->args);
    hex_heap_free(task->result);
    hex_heap_free(task);
}

// hex_task_complete is step one of the two-step completion transition,
// running on the completing task's own fiber. It records completing under
// the lifecycle mutex with no mutex held across the switch, publishes no
// joiner, and is not reclaimable yet: the dispatcher's own commit (in
// hex_worker_loop, after switch-back) records done, extracts the joiner and
// terminal disposition, and performs the root-shutdown and joiner-wake or
// detached-reclamation steps. The trampoline and the root epilogue both end
// here; the task's fiber never returns through an invalid stack.
void hex_task_complete(hex_task *task) {
    hex_mutex_raw_lock(&task->lifecycle_mutex);
    task->life = HEX_LIFE_COMPLETING;
    hex_mutex_raw_unlock(&task->lifecycle_mutex);
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

// hex_dispatch_commit runs on the dispatcher immediately after a dispatched
// fiber switches back: a non-null pending link commits an ordinary park, a
// null link completes hex_task_complete's two-step transition. Every park
// path sets a non-null link before switching, so a null link is unambiguous.
static void hex_dispatch_commit(hex_task *task) {
    if (task->pending_park != nullptr) {
        hex_task_commit_park(task);
        return;
    }
    hex_mutex_raw_lock(&task->lifecycle_mutex);
    task->life = HEX_LIFE_DONE;
    hex_task *joiner = task->joiner;
    task->joiner = nullptr;
    bool root = (task->flags & HEX_TASK_ROOT) != 0;
    bool detached = task->terminal_claim == HEX_TASK_CLAIM_DETACH;
    hex_mutex_raw_unlock(&task->lifecycle_mutex);
    if (root) {
        // The root shutdown switch-back is handled by the ordinary
        // shutdown check at the top of this loop, not here: recording
        // shutdown and broadcasting is this branch's only job, and it is
        // not a ready publication or reclamation path.
        atomic_store(&hex_shutdown, 1);
        hex_mutex_raw_lock(&hex_ready_mutex);
        hex_cond_broadcast(&hex_ready_cond);
        hex_mutex_raw_unlock(&hex_ready_mutex);
    } else if (joiner != nullptr) {
        hex_task_wake(joiner);
    } else if (detached) {
        hex_task_release(task);
    }
    // A joined, non-detached, non-root task is destroyed by its resumed
    // joiner after it copies the result out; this dispatcher performs no
    // further access to it.
}

// hex_worker_loop is the dispatcher of one worker. Worker zero (the initial
// process thread) switches back into the root fiber when the scheduler stops
// so generated main returns normally; other workers return from their thread
// function.
static void hex_worker_loop(void *param) {
    const int is_worker_zero = (param != nullptr);
    hex_context loop_context = hex_context_current();
    for (;;) {
        hex_mutex_raw_lock(&hex_ready_mutex);
        if (is_worker_zero) {
            while (hex_ready_head == nullptr && !atomic_load(&hex_shutdown)) {
                hex_cond_wait(&hex_ready_cond, &hex_ready_mutex);
            }
        } else {
            while (!hex_ready_has_eligible() && !atomic_load(&hex_shutdown)) {
                hex_cond_wait(&hex_ready_cond, &hex_ready_mutex);
            }
        }
        if (atomic_load(&hex_shutdown)) {
            hex_mutex_raw_unlock(&hex_ready_mutex);
            if (is_worker_zero) {
                hex_context_switch(loop_context, (hex_context)hex_root_task->fiber);
            }
            return;
        }
        hex_task *task;
        if (is_worker_zero) {
            task = hex_ready_pop_head();
        } else {
            task = hex_ready_pop_eligible();
        }
        hex_mutex_raw_unlock(&hex_ready_mutex);
        if (task == nullptr) {
            continue;
        }
        hex_current_task = task;
        task->scheduler_fiber = (void *)loop_context;
        hex_context_switch(loop_context, (hex_context)task->fiber);
        hex_dispatch_commit(task);
    }
}

static void hex_worker_thread(void *unused) {
    (void)unused;
    hex_current_task = nullptr;
    hex_worker_guard_setup();
    (void)hex_context_thread();
    hex_worker_loop(nullptr);
}

// hex_worker_zero_bootstrap is the entry of the worker-zero dispatcher
// context: it runs once, on root's first later switch into it, commits that
// switch through the shared helper, then enters the ordinary worker loop.
// The created context starts suspended, so root's first yield, wait, or
// completion is what begins execution here with root already switched out.
static void hex_worker_zero_bootstrap(void *param) {
    hex_task *root = (hex_task *)param;
    root->scheduler_fiber = (void *)hex_context_current();
    hex_dispatch_commit(root);
    hex_worker_loop((void *)1);
}

{{if .Event}}void hex_event_runtime_init(void);
{{end}}// hex_scheduler_init establishes the root task on the initial process thread
// (worker zero), creates the remaining workers, and returns: generated main
// runs the root statements next, and dispatch begins on root's first switch
// into the worker-zero bootstrap. The root fiber is the converted main
// thread context; its statements run as the Hexal entry point.
// The program bootstrap has already installed the libuv allocator.
void hex_scheduler_init(void) {
    // Worker zero is the initial process thread; its overflow handler and
    // alternate signal stack are established before any Task runs.
    hex_worker_guard_setup();
    if (!hex_mutex_raw_init(&hex_ready_mutex)) {
        hex_runtime_trap("[Runtime Error] scheduler mutex initialization failed\n");
    }
    hex_cond_init(&hex_ready_cond);
    hex_root_task = (hex_task *)hex_heap_allocate_zeroed_or_null(sizeof(hex_task));
    if (hex_root_task == nullptr) {
        hex_runtime_trap("[Runtime Error] scheduler allocation failed\n");
    }
    hex_root_task->fiber = (void *)hex_context_thread();
    if (hex_root_task->fiber == nullptr) {
        hex_runtime_trap("[Runtime Error] scheduler fiber initialization failed\n");
    }
    if (!hex_mutex_raw_init(&hex_root_task->lifecycle_mutex)) {
        hex_runtime_trap("[Runtime Error] scheduler lifecycle mutex initialization failed\n");
    }
    hex_root_task->id = atomic_fetch_add(&hex_next_task_id, 1);
    hex_root_task->flags = HEX_TASK_ROOT;
    hex_root_task->scheduler_fiber = (void *)hex_context_create(hex_worker_zero_bootstrap, hex_root_task);
    if (hex_root_task->scheduler_fiber == nullptr) {
        hex_runtime_trap("[Runtime Error] scheduler worker-zero context creation failed\n");
    }
    hex_current_task = hex_root_task;
    int logical = (int)uv_available_parallelism();
    if (logical < 1) {
        logical = 1;
    }
    for (int index = 1; index < logical; index++) {
        if (!hex_thread_spawn_detached(hex_worker_thread, nullptr)) {
            hex_runtime_trap("[Runtime Error] scheduler worker creation failed\n");
        }
    }
{{if .Event}}    hex_event_runtime_init();
{{end}}}

// hex_task_yield is the source-less park: it has no wait-source mutex to
// register under, so it release-stores notified directly instead of parking
// first, and the yielding fiber never publishes itself. The dispatcher alone
// observes notified after switch-back and publishes through the common
// commit path.
void hex_task_yield(void) {
    hex_task *self = hex_current_task;
    self->pending_park = self;
    atomic_store_explicit(&self->park_phase, HEX_PARK_NOTIFIED, memory_order_release);
    hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
    hex_task_resume_commit(self);
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
        args_frame = hex_heap_allocate_or_null(args_size);
        if (args_frame == nullptr) {
            return nullptr;
        }
        memcpy(args_frame, args, args_size);
    }
    hex_task *task = (hex_task *)hex_heap_allocate_zeroed_or_null(sizeof(hex_task));
    if (task == nullptr) {
        hex_heap_free(args_frame);
        return nullptr;
    }
    void *result_frame = nullptr;
    if (result_size > 0) {
        result_frame = hex_heap_allocate_or_null(result_size);
        if (result_frame == nullptr) {
            hex_heap_free(task);
            hex_heap_free(args_frame);
            return nullptr;
        }
    }
    task->fiber = (void *)hex_context_create(hex_task_trampoline, task);
    if (task->fiber == nullptr) {
        hex_heap_free(result_frame);
        hex_heap_free(task);
        hex_heap_free(args_frame);
        return nullptr;
    }
    if (!hex_mutex_raw_init(&task->lifecycle_mutex)) {
        hex_context_destroy((hex_context)task->fiber);
        hex_heap_free(result_frame);
        hex_heap_free(task);
        hex_heap_free(args_frame);
        return nullptr;
    }
    task->id = atomic_fetch_add(&hex_next_task_id, 1);
    task->entry = entry;
    task->args = args_frame;
    task->result = result_frame;
    hex_ready_publish(task);
    return task;
}

// hex_task_join waits for the task to finish and returns its result frame.
// The target's lifecycle mutex serializes the done check against completion
// recording the same fact, so a join arriving during completing always
// registers before the dispatcher's step two can extract it, and a join
// observing done never races a still-running fiber. Joining the current task
// through its own alias is a cheaply detectable misuse and traps. The
// generated per-result-type wrapper copies the result out and releases the
// target; this function performs neither.
void *hex_task_join(hex_task *task) {
    hex_task *self = hex_current_task;
    if (task == self) {
        hex_runtime_trap("[Runtime Error] cannot join the current task\n");
    }
    hex_mutex_raw_lock(&task->lifecycle_mutex);
    if (task->terminal_claim != HEX_TASK_CLAIM_NONE) {
        hex_mutex_raw_unlock(&task->lifecycle_mutex);
        hex_runtime_trap("[Runtime Error] Task already joined or detached\n");
    }
    task->terminal_claim = HEX_TASK_CLAIM_JOIN;
    if (task->life == HEX_LIFE_DONE) {
        hex_mutex_raw_unlock(&task->lifecycle_mutex);
        return task->result;
    }
    self->pending_park = task;
    atomic_store_explicit(&self->park_phase, HEX_PARK_PARKING, memory_order_release);
    task->joiner = self;
    hex_mutex_raw_unlock(&task->lifecycle_mutex);
    hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
    hex_task_resume_commit(self);
    return task->result;
}

// hex_task_detach claims terminal ownership under the target lifecycle mutex.
// Completion either observes that claim and reclaims after its final switch,
// or a completed target is reclaimed here. A later join or detach traps before
// touching a reclaimed target because the claim is serialized first.
void hex_task_detach(hex_task *task) {
    hex_mutex_raw_lock(&task->lifecycle_mutex);
    if (task->terminal_claim != HEX_TASK_CLAIM_NONE) {
        hex_mutex_raw_unlock(&task->lifecycle_mutex);
        hex_runtime_trap("[Runtime Error] Task already joined or detached\n");
    }
    task->terminal_claim = HEX_TASK_CLAIM_DETACH;
    bool done = task->life == HEX_LIFE_DONE;
    hex_mutex_raw_unlock(&task->lifecycle_mutex);
    if (done) {
        hex_task_release(task);
    }
}
{{if .Channels}}
typedef struct hex_chan {
    hex_mutex_raw mutex;
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
    hex_chan *channel = (hex_chan *)hex_heap_allocate_zeroed_or_null(sizeof(hex_chan));
    if (channel == nullptr) {
        return nullptr;
    }
    channel->slots = (uint8_t *)hex_heap_allocate_or_null(slots_bytes);
    if (channel->slots == nullptr) {
        hex_heap_free(channel);
        return nullptr;
    }
    if (!hex_mutex_raw_init(&channel->mutex)) {
        hex_heap_free(channel->slots);
        hex_heap_free(channel);
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
        hex_mutex_raw_lock(&channel->mutex);
        if (channel->closed) {
            hex_mutex_raw_unlock(&channel->mutex);
            return false;
        }
        if (channel->length < channel->capacity) {
            break;
        }
        self->wake_result = 0;
        self->pending_park = self;
        atomic_store_explicit(&self->park_phase, HEX_PARK_PARKING, memory_order_release);
        self->wait_next = channel->wait_send;
        channel->wait_send = self;
        hex_mutex_raw_unlock(&channel->mutex);
        hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
        hex_task_resume_commit(self);
        if (self->wake_result) {
            return false;
        }
    }
    size_t tail = (channel->head + channel->length) % channel->capacity;
    memcpy(channel->slots + tail * channel->element_size, value, channel->element_size);
    channel->length++;
    hex_task *waiter = channel->wait_recv;
    if (waiter != nullptr) {
        channel->wait_recv = waiter->wait_next;
        waiter->wake_result = 0;
        hex_task_wake(waiter);
    }
    hex_mutex_raw_unlock(&channel->mutex);
    return true;
}

// receive copies the oldest element out, parking while the channel is empty
// and open. Closed-and-drained is the one recoverable failure (EoS).
bool hex_chan_receive(hex_chan *channel, void *out) {
    hex_task *self = hex_current_task;
    for (;;) {
        hex_mutex_raw_lock(&channel->mutex);
        if (channel->length > 0) {
            break;
        }
        if (channel->closed) {
            hex_mutex_raw_unlock(&channel->mutex);
            return false;
        }
        self->wake_result = 0;
        self->pending_park = self;
        atomic_store_explicit(&self->park_phase, HEX_PARK_PARKING, memory_order_release);
        self->wait_next = channel->wait_recv;
        channel->wait_recv = self;
        hex_mutex_raw_unlock(&channel->mutex);
        hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
        hex_task_resume_commit(self);
    }
    memcpy(out, channel->slots + channel->head * channel->element_size, channel->element_size);
    channel->head = (channel->head + 1) % channel->capacity;
    channel->length--;
    hex_task *waiter = channel->wait_send;
    if (waiter != nullptr) {
        channel->wait_send = waiter->wait_next;
        waiter->wake_result = 0;
        hex_task_wake(waiter);
    }
    hex_mutex_raw_unlock(&channel->mutex);
    return true;
}

// close is idempotent: it wakes every blocked sender (with an Error) and
// receiver (with EoS) and never discards queued values. Each dequeued
// waiter's wake transition happens under channel->mutex, serialized with any
// concurrent send or receive registration, so no waiter is ever left
// notified without its dispatcher eventually observing and publishing it.
void hex_chan_close(hex_chan *channel) {
    hex_mutex_raw_lock(&channel->mutex);
    channel->closed = true;
    hex_task *waiter = channel->wait_send;
    while (waiter != nullptr) {
        hex_task *next = waiter->wait_next;
        waiter->wake_result = 1;
        hex_task_wake(waiter);
        waiter = next;
    }
    channel->wait_send = nullptr;
    waiter = channel->wait_recv;
    while (waiter != nullptr) {
        hex_task *next = waiter->wait_next;
        waiter->wake_result = 0;
        hex_task_wake(waiter);
        waiter = next;
    }
    channel->wait_recv = nullptr;
    hex_mutex_raw_unlock(&channel->mutex);
}

size_t hex_chan_length(hex_chan *channel) {
    hex_mutex_raw_lock(&channel->mutex);
    size_t length = channel->length;
    hex_mutex_raw_unlock(&channel->mutex);
    return length;
}

size_t hex_chan_capacity(hex_chan *channel) {
    return channel->capacity;
}

bool hex_chan_is_closed(hex_chan *channel) {
    hex_mutex_raw_lock(&channel->mutex);
    bool closed = channel->closed;
    hex_mutex_raw_unlock(&channel->mutex);
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
    hex_mutex_raw_destroy(&channel->mutex);
    hex_heap_free(channel->slots);
    hex_heap_free(channel);
}
{{end}}{{if .Mutex}}
struct hex_mutex_control {
    hex_mutex_raw mutex;
    hex_task *owner;
    hex_task *wait_list;
};

hex_mutex *hex_mutex_new(void) {
    hex_mutex *mutex = (hex_mutex *)hex_heap_allocate_zeroed_or_null(sizeof(hex_mutex));
    if (mutex == nullptr) {
        return nullptr;
    }
    if (!hex_mutex_raw_init(&mutex->mutex)) {
        hex_heap_free(mutex);
        return nullptr;
    }
    return mutex;
}

// lock retains direct ownership transfer: unlock assigns mutex->owner to the
// selected waiter itself before waking it, so a woken waiter here always
// finds wake_result set and returns directly instead of re-entering
// acquisition, which is what the recursive-lock check below would otherwise
// wrongly reject.
void hex_mutex_lock(hex_mutex *mutex) {
    hex_task *self = hex_current_task;
    for (;;) {
        hex_mutex_raw_lock(&mutex->mutex);
        if (mutex->owner == nullptr) {
            mutex->owner = self;
            hex_mutex_raw_unlock(&mutex->mutex);
            return;
        }
        if (mutex->owner == self) {
            hex_mutex_raw_unlock(&mutex->mutex);
            hex_runtime_trap("[Runtime Error] recursive mutex lock\n");
        }
        self->wake_result = 0;
        self->pending_park = self;
        atomic_store_explicit(&self->park_phase, HEX_PARK_PARKING, memory_order_release);
        self->wait_next = mutex->wait_list;
        mutex->wait_list = self;
        hex_mutex_raw_unlock(&mutex->mutex);
        hex_context_switch((hex_context)self->fiber, (hex_context)self->scheduler_fiber);
        hex_task_resume_commit(self);
        if (self->wake_result) {
            return;
        }
    }
}

// unlock hands ownership directly to the selected waiter under mutex->mutex,
// serialized with the wake transition per the common protocol; a waiter
// consumes that transfer in lock() above rather than re-entering acquisition.
void hex_mutex_unlock(hex_mutex *mutex) {
    hex_mutex_raw_lock(&mutex->mutex);
    if (mutex->owner != hex_current_task) {
        hex_mutex_raw_unlock(&mutex->mutex);
        hex_runtime_trap("[Runtime Error] mutex unlock by a non-owner\n");
    }
    hex_task *waiter = mutex->wait_list;
    if (waiter != nullptr) {
        mutex->wait_list = waiter->wait_next;
        mutex->owner = waiter;
        waiter->wake_result = 1;
        hex_task_wake(waiter);
    } else {
        mutex->owner = nullptr;
    }
    hex_mutex_raw_unlock(&mutex->mutex);
}

void hex_mutex_free(hex_mutex *mutex) {
    if (mutex->owner != nullptr || mutex->wait_list != nullptr) {
        hex_runtime_trap("[Runtime Error] mutex free while locked or awaited\n");
    }
    hex_mutex_raw_destroy(&mutex->mutex);
    hex_heap_free(mutex);
}
{{end -}}
{{end -}}
