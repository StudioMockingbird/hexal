package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0037: Task<T>, spawn, join/detach/yield, Channel<T>, Mutex, and
// Atomic<T>.
//
// The scheduler runtime is a single C23 block emitted into the generated
// header: one global ready queue guarded by a native mutex and condition
// variable, a fixed set of worker threads created with C23 <threads.h>, and
// platform fiber contexts. Windows uses the verified Fiber APIs; POSIX uses
// ucontext with one caller-allocated stack per task. Task control blocks and
// argument frames are scheduler-owned malloc storage; user payloads keep
// their explicit allocators.

// generatedConcurrencyState records every RFC 0037 feature the program uses,
// so the emitted runtime contains exactly the families the program needs.
type generatedConcurrencyState struct {
	used        bool // Task, Channel, or Mutex linked the scheduler runtime
	taskTypes   map[string]compilerTypes.Type
	joinTypes   map[string]compilerTypes.Type // Task<R> types whose join is called
	detach      bool
	yield       bool
	spawns      []spawnSite
	channels    map[string]compilerTypes.Type
	mutexNew    bool
	mutexLock   bool
	mutexUnlock bool
	mutexFree   bool
	atomics     map[string]compilerTypes.Type
	// The checked result unions per operation: the generator reuses the
	// checker's exact union identities so helper results match the C types
	// the callers declare.
	channelNewUnions     map[string]compilerTypes.Type
	channelSendUnions    map[string]compilerTypes.Type
	channelReceiveUnions map[string]compilerTypes.Type
	mutexNewUnion        compilerTypes.Type
	// failure kinds whose literals the Error helper must reference.
	spawnFail     bool
	channelNew    bool
	channelSend   bool
	mutexCreate   bool
	fileLiteral   string
	headerLiteral string
}

// spawnSite is one spawned function: its checked signature drives the
// argument frame and the entry adapter. module is the canonical id of the
// module that owns the spawned function; the adapter is emitted beside that
// function's definition (RFC 0034 per-module generation).
type spawnSite struct {
	name     string
	function string
	module   string
	params   []compilerTypes.Type
	result   compilerTypes.Type // zero Type means Nil
}

// errorMessagePayloads are the RFC 0037 recoverable-failure messages. Each
// is registered as a String literal only when the matching operation is used.
const (
	taskCreationFailed    = "task creation failed"
	channelCreationFailed = "channel creation failed"
	channelSendFailed     = "channel send failed"
	mutexCreationFailed   = "mutex creation failed"
)

// discoverGeneratedConcurrency walks the checked program collecting the
// concrete Task, Channel, Mutex, and Atomic operations, plus the spawn
// entries. Literals needed by the failure-Error helper are registered in the
// string literal registry so the Error object's String members lower through
// the ordinary literal machinery.
func discoverGeneratedConcurrency(program checker.Program, functions map[string]compilerTypes.Type, stringState *generatedStringState, moduleID, owner string) (*generatedConcurrencyState, error) {
	state := &generatedConcurrencyState{
		taskTypes:            make(map[string]compilerTypes.Type),
		joinTypes:            make(map[string]compilerTypes.Type),
		channels:             make(map[string]compilerTypes.Type),
		atomics:              make(map[string]compilerTypes.Type),
		channelNewUnions:     make(map[string]compilerTypes.Type),
		channelSendUnions:    make(map[string]compilerTypes.Type),
		channelReceiveUnions: make(map[string]compilerTypes.Type),
	}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.SpawnExpression:
				state.used = true
				state.spawnFail = true
				if node.OperandType != (compilerTypes.Type{}) {
					state.taskTypes[node.OperandType.CName] = node.OperandType
				}
				if node.Operand != nil {
					site, err := spawnSiteFor(*node.Operand, functions, moduleID, owner)
					if err != nil {
						return err
					}
					state.spawns = append(state.spawns, site)
				}
			case checker.TaskYieldExpression:
				state.used = true
				state.yield = true
			case checker.TaskMethodCallExpression:
				state.used = true
				if node.OperandType != (compilerTypes.Type{}) {
					state.taskTypes[node.OperandType.CName] = node.OperandType
				}
				switch node.Name {
				case "join":
					if node.OperandType != (compilerTypes.Type{}) {
						state.joinTypes[node.OperandType.CName] = node.OperandType
					}
				case "detach":
					state.detach = true
				}
			case checker.ChannelConstructorExpression:
				state.used = true
				state.channelNew = true
				if node.OperandType != (compilerTypes.Type{}) {
					state.channels[node.OperandType.CName] = node.OperandType
					if node.ResultType != (compilerTypes.Type{}) {
						state.channelNewUnions[node.OperandType.CName] = node.ResultType
					}
				}
			case checker.ChannelMethodCallExpression:
				state.used = true
				if node.OperandType != (compilerTypes.Type{}) {
					state.channels[node.OperandType.CName] = node.OperandType
				}
				switch node.Name {
				case "send":
					state.channelSend = true
					if node.OperandType != (compilerTypes.Type{}) && node.ResultType != (compilerTypes.Type{}) {
						state.channelSendUnions[node.OperandType.CName] = node.ResultType
					}
				case "receive":
					if node.OperandType != (compilerTypes.Type{}) && node.ResultType != (compilerTypes.Type{}) {
						state.channelReceiveUnions[node.OperandType.CName] = node.ResultType
					}
				}
			case checker.MutexConstructorExpression:
				state.used = true
				state.mutexNew = true
				state.mutexCreate = true
				if node.ResultType != (compilerTypes.Type{}) {
					state.mutexNewUnion = node.ResultType
				}
			case checker.MutexMethodCallExpression:
				state.used = true
				switch node.Name {
				case "lock":
					state.mutexLock = true
				case "unlock":
					state.mutexUnlock = true
				case "free":
					state.mutexFree = true
				}
			case checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression:
				if node.OperandType != (compilerTypes.Type{}) {
					state.atomics[node.OperandType.CName] = node.OperandType
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	if state.used && stringState != nil {
		stringState.used = true
		stringState.needStrand = true
		state.fileLiteral = registerConcurrencyLiteral(stringState, sourceFilename)
		state.headerLiteral = registerConcurrencyLiteral(stringState, "Scheduler")
		for _, payload := range []string{taskCreationFailed, channelCreationFailed, channelSendFailed, mutexCreationFailed} {
			needed := false
			switch payload {
			case taskCreationFailed:
				needed = state.spawnFail
			case channelCreationFailed:
				needed = state.channelNew
			case channelSendFailed:
				needed = state.channelSend
			case mutexCreationFailed:
				needed = state.mutexCreate
			}
			if needed {
				registerConcurrencyLiteral(stringState, payload)
			}
		}
	}
	return state, nil
}

// registerConcurrencyLiteral interns one payload in the string literal
// registry and returns the C literal object name.
func registerConcurrencyLiteral(stringState *generatedStringState, payload string) string {
	if index, ok := stringState.seen[payload]; ok {
		return stringLiteralCName(index - 1)
	}
	stringState.literals = append(stringState.literals, payload)
	stringState.seen[payload] = len(stringState.literals)
	return stringLiteralCName(len(stringState.literals) - 1)
}

// spawnSiteFor derives one spawn site from the checked call node: the named
// callee and the parameter and result types that shape the entry adapter.
// localModule is the canonical id and localOwner the encoded owner of the
// module being generated; a local callee falls back to them so its C name
// matches the definition in its own module pair.
func spawnSiteFor(node checker.Expression, functions map[string]compilerTypes.Type, localModule, localOwner string) (spawnSite, error) {
	if node.Kind != checker.CallExpression || node.Operand == nil || node.Operand.Kind != checker.FunctionReferenceExpression || node.Operand.Name == "" {
		return spawnSite{}, unknownExpressionDiagnostic("spawn without a direct checked function call")
	}
	signature, ok := functions[node.Operand.Name]
	if !ok || signature.Signature == nil {
		return spawnSite{}, unknownExpressionDiagnostic("spawn target is not a checked function: " + node.Operand.Name)
	}
	module := node.Operand.Module
	if module == "" {
		module = localModule
	}
	site := spawnSite{
		name:     node.Operand.Name,
		module:   module,
		function: PrivateCName(FunctionName, node.Operand.Name, moduleOwner(node.Operand.Module, localOwner)),
		params:   append([]compilerTypes.Type(nil), signature.Signature.Parameters...),
	}
	if signature.Signature.Result != nil {
		site.result = *signature.Signature.Result
	}
	return site, nil
}

// taskSuffix returns the per-result C suffix of a Task<R> type.
func taskSuffix(task compilerTypes.Type) string {
	return strings.TrimPrefix(task.CName, "hex_task_")
}

// channelSuffix returns the per-element C suffix of a Channel<T> type.
func channelSuffix(channel compilerTypes.Type) string {
	return strings.TrimPrefix(channel.CName, "hex_channel_")
}

// atomicSuffix returns the per-element C suffix of an Atomic<T> type.
func atomicSuffix(atomic compilerTypes.Type) string {
	return strings.TrimPrefix(atomic.CName, "hex_atomic_")
}

// messageLiteral returns the C literal object name of one failure message.
func (state *generatedConcurrencyState) messageLiteral(stringState *generatedStringState, payload string) string {
	if index, ok := stringState.seen[payload]; ok {
		return stringLiteralCName(index - 1)
	}
	return state.fileLiteral
}

// hex_sched_error_spelling is the runtime Error-construction helper, emitted
// once before any operation family that can fail.
func (state *generatedConcurrencyState) writeErrorHelper(result *strings.Builder) {
	fmt.Fprintf(result, "\nstatic inline hex_t_Error hex_sched_error(size_t line, size_t column, const hex_string *message) {\n")
	fmt.Fprintf(result, "    return (hex_t_Error){\n")
	fmt.Fprintf(result, "        .hex_m_file = &%s,\n", state.fileLiteral)
	fmt.Fprintf(result, "        .hex_m_line = line,\n")
	fmt.Fprintf(result, "        .hex_m_column = column,\n")
	fmt.Fprintf(result, "        .hex_m_header = (hex_strand){{")
	for _, character := range []byte("Scheduler") {
		fmt.Fprintf(result, " %d,", character)
	}
	fmt.Fprintf(result, " 0 }},\n")
	fmt.Fprintf(result, "        .hex_m_message = message,\n")
	fmt.Fprintf(result, "    };\n}\n")
}

// writeConcurrencyTypePrelude emits, at the top of the generated header, the
// forward declarations every later definition may reference: the Task,
// Channel, Mutex, and Atomic handle typedefs that union, object, and ADT
// payloads may contain, and the spawn entry prototypes that function bodies
// call before their definitions appear in the root module's C file. An
// atomic-only program uses the Atomic typedefs without linking the scheduler
// runtime, so the prelude runs when either the scheduler or the Atomic
// family is selected.
func writeConcurrencyTypePrelude(result *strings.Builder, state *generatedConcurrencyState) {
	if state == nil || !state.used && len(state.atomics) == 0 {
		return
	}
	if state.used {
		// RFC 0037: the task control block is a compiler-owned type
		// definition every translation unit needs: the spawn entry adapters
		// and the join helpers dereference hex_task in the module pairs, so
		// the complete struct lives in hexal.h, never in the root C runtime
		// alone.
		result.WriteString("\n/* RFC 0037 handle typedefs */\n")
		result.WriteString("typedef struct hex_task hex_task;\n")
		result.WriteString("typedef struct hex_chan hex_chan;\n")
		result.WriteString("typedef struct hex_mutex_control hex_mutex;\n")
		result.WriteString("typedef void (*hex_task_entry)(hex_task *task);\n")
		result.WriteString("struct hex_task {\n    hex_task *ready_next;\n    hex_task *wait_next;\n    int64_t id;\n    uint8_t state;\n    uint8_t wake_error;\n    uint8_t flags;\n    hex_task *joiner;\n    void *fiber;\n    void *scheduler_fiber;\n    hex_task_entry entry;\n    void *args;\n    void *result;\n};\n")
		taskNames := make([]string, 0, len(state.taskTypes))
		for name := range state.taskTypes {
			taskNames = append(taskNames, name)
		}
		sort.Strings(taskNames)
		for _, name := range taskNames {
			task := state.taskTypes[name]
			fmt.Fprintf(result, "typedef hex_task *hex_task_%s;\n", taskSuffix(task))
		}
		channelNames := make([]string, 0, len(state.channels))
		for name := range state.channels {
			channelNames = append(channelNames, name)
		}
		sort.Strings(channelNames)
		for _, name := range channelNames {
			channel := state.channels[name]
			fmt.Fprintf(result, "typedef hex_chan *hex_channel_%s;\n", channelSuffix(channel))
		}
	}
	if len(state.atomics) > 0 {
		// The _Atomic typedefs depend on <stdatomic.h>, contributed to the
		// hexal.h umbrella by requirement discovery (RFC 0062).
		atomicNames := make([]string, 0, len(state.atomics))
		for name := range state.atomics {
			atomicNames = append(atomicNames, name)
		}
		sort.Strings(atomicNames)
		for _, name := range atomicNames {
			atomic := state.atomics[name]
			fmt.Fprintf(result, "typedef _Atomic(%s) hex_atomic_%s;\n", typeSpelling(atomic.Atomic.Element), atomicSuffix(atomic))
		}
	}
	if !state.used {
		return
	}
	// RFC 0037: the spawn entry adapters are emitted in the module that owns
	// the spawned function, so hexal.h declares them with external linkage
	// for every translation unit that contains a spawn prologue.
	for _, site := range state.spawns {
		fmt.Fprintf(result, "void hex_task_entry_%s(hex_task *task);\n", site.function)
	}
}

// writeConcurrencyRuntime emits the RFC 0037 runtime into the root module's
// C file: the fiber platform layer, the M:N scheduler, the task/join/yield
// machinery, the Channel and Mutex cores. Everything here holds process-wide
// state or is a non-inline static function, so it exists exactly once per
// process, never in a header every translation unit includes.
func writeConcurrencyRuntime(result *strings.Builder, state *generatedConcurrencyState, stringState *generatedStringState) {
	if state == nil || !state.used {
		return
	}
	writeSchedulerRuntime(result)
	writeChannelCore(result, state)
	writeMutexCore(result, state)
}

// writeConcurrencyExterns emits, into hexal.h, the declarations of the
// runtime core entry points the module headers' inline helpers call. The core
// lives in the root module's C file with external linkage; declaring it here
// lets every translation unit agree on the interface.
func writeConcurrencyExterns(result *strings.Builder, state *generatedConcurrencyState) {
	if state == nil || !state.used {
		return
	}
	result.WriteString("\n/* RFC 0037 runtime entry points, defined in the root module's C file */\n")
	result.WriteString("hex_task *hex_task_spawn(void (*entry)(hex_task *), size_t args_size, size_t args_align, const void *args, size_t result_size, size_t result_align);\n")
	result.WriteString("void *hex_task_join(hex_task *task);\n")
	result.WriteString("void hex_task_yield(void);\n")
	result.WriteString("void hex_task_detach(hex_task *task);\n")
	result.WriteString("void hex_task_release(hex_task *task);\n")
	result.WriteString("void hex_task_complete(hex_task *task);\n")
	result.WriteString("hex_chan *hex_chan_new(size_t capacity, size_t element_size);\n")
	result.WriteString("bool hex_chan_send(hex_chan *channel, const void *value);\n")
	result.WriteString("bool hex_chan_receive(hex_chan *channel, void *out);\n")
	result.WriteString("void hex_chan_close(hex_chan *channel);\n")
	result.WriteString("size_t hex_chan_length(hex_chan *channel);\n")
	result.WriteString("size_t hex_chan_capacity(hex_chan *channel);\n")
	result.WriteString("bool hex_chan_is_closed(hex_chan *channel);\n")
	result.WriteString("void hex_chan_free(hex_chan *channel);\n")
	result.WriteString("hex_mutex *hex_mutex_new(void);\n")
	result.WriteString("void hex_mutex_lock(hex_mutex *mutex);\n")
	result.WriteString("void hex_mutex_unlock(hex_mutex *mutex);\n")
	result.WriteString("void hex_mutex_free(hex_mutex *mutex);\n")
}

// writeConcurrencyInlineHelpers emits the per-element, per-result inline
// wrappers into the module header: the spawn argument frames (which name user
// parameter types), the Task join helpers, the Channel and Mutex operation
// families, and the Atomic family. They are state-free and only call the
// runtime core through its hexal.h declarations.
func writeConcurrencyInlineHelpers(result *strings.Builder, state *generatedConcurrencyState, stringState *generatedStringState) {
	if state == nil || !state.used && len(state.atomics) == 0 {
		return
	}
	if state.used {
		if state.spawnFail || state.channelNew || state.channelSend || state.mutexNew {
			// RFC 0037: every recoverable operation constructs its failure
			// Error through hex_sched_error, spawn prologues included. One
			// helper precedes every family that references it; the
			// per-family writers must not re-emit it.
			state.writeErrorHelper(result)
		}
		writeSpawnArgFrames(result, state.spawns)
		writeTaskTypeHelpers(result, state)
		writeChannelInlineHelpers(result, state, stringState)
		writeMutexInlineHelpers(result, state, stringState)
	}
	writeAtomicHelpers(result, state)
}

// writeSchedulerRuntime emits the platform context layer and the shared M:N
// scheduler. The spec's platform surface is deliberately small: context
// create/switch/destroy plus a logical-processor query. Everything else is
// shared ISO C23 (<threads.h>, <stdatomic.h>, <string.h>).
func writeSchedulerRuntime(result *strings.Builder) {
	result.WriteString(`
#if defined(_WIN32)
#include <windows.h>
#else
#include <ucontext.h>
#endif
#include <threads.h>
#include <stdatomic.h>
#include <string.h>

#if defined(__STDC_NO_THREADS__)
#error "Hexal Task runtime requires C23 threads (<threads.h>); this toolchain defines __STDC_NO_THREADS__"
#endif

#define HEX_TASK_READY 1
#define HEX_TASK_RUNNING 2
#define HEX_TASK_PARKED 3
#define HEX_TASK_DONE 4
#define HEX_TASK_ROOT 1u
#define HEX_TASK_DETACH 2u

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
    return CreateFiberEx(0, 0, FIBER_FLAG_FLOAT_SWITCH, (LPFIBER_START_ROUTINE)entry, param);
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
#else
typedef struct hex_context_impl hex_context_impl;
struct hex_context_impl {
    ucontext_t context;
    void *stack;
};

// The POSIX backend uses System V ucontext with one caller-allocated stack
// per Task. The scheduler thread's own context is captured once and reused
// for every switch back into the worker loop.
static int hex_logical_processors(void) {
    long count = sysconf(_SC_NPROCESSORS_ONLN);
    return count > 0 ? (int)count : 1;
}
static hex_context_impl *hex_context_create(void (*entry)(void *), void *param) {
    const size_t stack_size = 1u << 20;
    hex_context_impl *context = (hex_context_impl *)malloc(sizeof(hex_context_impl));
    if (context == nullptr) {
        return nullptr;
    }
    context->stack = malloc(stack_size);
    if (context->stack == nullptr) {
        free(context);
        return nullptr;
    }
    if (getcontext(&context->context) != 0) {
        free(context->stack);
        free(context);
        return nullptr;
    }
    context->context.uc_stack.ss_sp = context->stack;
    context->context.uc_stack.ss_size = stack_size;
    context->context.uc_link = nullptr;
    makecontext(&context->context, (void (*)(void))entry, 1, param);
    return context;
}
static hex_context_impl *hex_context_current(void) {
    hex_context_impl *context = (hex_context_impl *)malloc(sizeof(hex_context_impl));
    if (context != nullptr) {
        context->stack = nullptr;
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
    if (swapcontext(&from->context, &to->context) != 0) {
        abort();
    }
}
static void hex_context_destroy(hex_context_impl *context) {
    if (context != nullptr) {
        free(context->stack);
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
static hex_task *hex_root_task;

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
    (void)hex_context_thread();
    hex_worker_loop(nullptr);
    return 0;
}

// hex_scheduler_init establishes the root task on the initial process thread
// (worker zero), creates the remaining workers, and starts dispatch. The
// root fiber is the converted main thread context; its statements run as the
// Hexal entry point.
static void hex_scheduler_init(void) {
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
`)
}

// writeTaskTypeHelpers emits the per-result join helper that copies R out of
// the task frame and reclaims the task. The handle typedefs themselves are
// emitted in the header prelude.
func writeTaskTypeHelpers(result *strings.Builder, state *generatedConcurrencyState) {
	if len(state.joinTypes) == 0 {
		return
	}
	result.WriteString("\n// join copies R out of the result frame and reclaims the task storage.\n")
	names := make([]string, 0, len(state.joinTypes))
	for name := range state.joinTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		task := state.joinTypes[name]
		suffix := taskSuffix(task)
		if task.Task.Result == (compilerTypes.Type{}) || compilerTypes.Equal(task.Task.Result, compilerTypes.Nil) {
			fmt.Fprintf(result, "static inline void hex_task_join_%s(hex_task *task) {\n    (void)hex_task_join(task);\n    hex_task_release(task);\n}\n", suffix)
			continue
		}
		resultType := task.Task.Result
		fmt.Fprintf(result, "static inline %s hex_task_join_%s(hex_task *task) {\n    %s *frame = (%s *)hex_task_join(task);\n    %s value = *frame;\n    hex_task_release(task);\n    return value;\n}\n", typeSpelling(resultType), suffix, typeSpelling(resultType), typeSpelling(resultType), typeSpelling(resultType))
	}
}

// writeSpawnAdapters emits, in the spawned function's own module C file, one
// entry adapter per spawned function. The adapter reads the shallow-copied
// arguments from the task frame, calls the named function directly, stores R
// in the result frame, and completes the task. It is emitted after the
// function definitions it calls, so the call never crosses a translation
// unit; the external linkage satisfies the hexal.h prototypes and the spawn
// prologues in other modules. The argument frame structs themselves are
// declared in the module header (writeSpawnArgFrames) so the spawn prologues
// inside function bodies can fill them.
func writeSpawnAdapters(result *strings.Builder, sites []spawnSite) {
	if len(sites) == 0 {
		return
	}
	for _, site := range sites {
		fmt.Fprintf(result, "\nvoid hex_task_entry_%s(hex_task *task) {\n", site.function)
		if len(site.params) > 0 {
			fmt.Fprintf(result, "    hex_task_args_%s *args = (hex_task_args_%s *)task->args;\n", site.function, site.function)
		}
		if site.result != (compilerTypes.Type{}) && !compilerTypes.Equal(site.result, compilerTypes.Nil) {
			fmt.Fprintf(result, "    %s result = %s(", typeSpelling(site.result), site.function)
			writeSpawnArguments(result, site)
			fmt.Fprintf(result, ");\n    *(%s *)task->result = result;\n", typeSpelling(site.result))
		} else {
			fmt.Fprintf(result, "    %s(", site.function)
			writeSpawnArguments(result, site)
			fmt.Fprintf(result, ");\n")
		}
		fmt.Fprintf(result, "    hex_task_complete(task);\n}\n")
	}
}

func writeSpawnArguments(result *strings.Builder, site spawnSite) {
	for index := range site.params {
		if index > 0 {
			result.WriteString(", ")
		}
		fmt.Fprintf(result, "args->a%d", index+1)
	}
}

// writeSpawnArgFrames emits one argument-frame struct per spawned function
// into the module header, before any function body that fills one.
func writeSpawnArgFrames(result *strings.Builder, sites []spawnSite) {
	if len(sites) == 0 {
		return
	}
	for _, site := range sites {
		if len(site.params) == 0 {
			continue
		}
		fmt.Fprintf(result, "\ntypedef struct hex_task_args_%s {\n", site.function)
		for index, parameter := range site.params {
			fmt.Fprintf(result, "    %s;\n", declaration(parameter, fmt.Sprintf("a%d", index+1), true))
		}
		fmt.Fprintf(result, "} hex_task_args_%s;\n", site.function)
	}
}

// writeChannelCore emits the shared bounded ring-buffer Channel control block
// and the core operations into the root module's C file. send and receive
// park the current task while full or empty and open; parking never blocks
// the worker thread. The core is non-inline and holds no file-scope state
// beyond what the caller passes, but it must exist once per process because
// its wait lists carry task pointers.
func writeChannelCore(result *strings.Builder, state *generatedConcurrencyState) {
	if len(state.channels) == 0 {
		return
	}
	result.WriteString(`
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
`)

}

// writeChannelInlineHelpers emits the per-element Channel adapters into the
// module header: new, send, and receive build the checked result unions
// (Channel | Error, Nil | Error, and T | EoS) around the shared core, and
// free adapts the checked Heap identity argument. close, length, capacity,
// and is_closed lower directly to the core (RFC 0069 Amendment 2 Item B) and
// need no inline wrapper.
func writeChannelInlineHelpers(result *strings.Builder, state *generatedConcurrencyState, stringState *generatedStringState) {
	if len(state.channels) == 0 {
		return
	}
	channelNames := make([]string, 0, len(state.channels))
	for name := range state.channels {
		channelNames = append(channelNames, name)
	}
	sort.Strings(channelNames)
	for _, name := range channelNames {
		channel := state.channels[name]
		suffix := channelSuffix(channel)
		element := channel.Channel.Element
		elementSpelling := typeSpelling(element)
		if state.channelNew {
			union := state.channelNewUnions[channel.CName]
			if union != (compilerTypes.Type{}) {
				channelIndex := unionMemberIndex(union, channel)
				errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
				message := state.messageLiteral(stringState, channelCreationFailed)
				fmt.Fprintf(result, "\nstatic inline %s hex_chan_new_%s(uintptr_t heap_identity, size_t capacity, size_t line, size_t column, const hex_string *message) {\n    (void)heap_identity;\n    (void)message;\n    hex_chan *channel = hex_chan_new(capacity, sizeof(%s));\n    if (channel != nullptr) {\n        return (%s){ .tag = %s, .payload.member_%d = channel };\n    }\n    return (%s){ .tag = %s, .payload.member_%d = hex_sched_error(line, column, &%s) };\n}\n",
					union.CName, suffix, elementSpelling, union.CName, unionTagName(union, channelIndex), channelIndex, union.CName, unionTagName(union, errorIndex), errorIndex, message)
			}
		}
		if state.channelSend {
			union := state.channelSendUnions[channel.CName]
			if union != (compilerTypes.Type{}) {
				nilIndex := unionMemberIndex(union, compilerTypes.Nil)
				errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
				message := state.messageLiteral(stringState, channelSendFailed)
				fmt.Fprintf(result, "\nstatic inline %s hex_chan_send_%s(hex_chan *channel, %s value, size_t line, size_t column, const hex_string *message) {\n    (void)message;\n    if (hex_chan_send(channel, &value)) {\n        return (%s){ .tag = %s };\n    }\n    return (%s){ .tag = %s, .payload.member_%d = hex_sched_error(line, column, &%s) };\n}\n",
					union.CName, suffix, elementSpelling, union.CName, unionTagName(union, nilIndex), union.CName, unionTagName(union, errorIndex), errorIndex, message)
			}
		}
		// The receive union is emitted for every used Channel<T>: receive
		// needs the T | EoS union. close, length, capacity, is_closed, and
		// free lower directly to the core (RFC 0069 Amendment 2 Item B); the
		// free adapter is emitted here because it adapts the checked Heap
		// identity argument.
		receiveUnion := state.channelReceiveUnions[channel.CName]
		if receiveUnion != (compilerTypes.Type{}) {
			elementIndex := unionMemberIndex(receiveUnion, element)
			eosIndex := unionMemberIndex(receiveUnion, compilerTypes.EoS)
			fmt.Fprintf(result, "\nstatic inline %s hex_chan_recv_%s(hex_chan *channel) {\n    %s value;\n    if (hex_chan_receive(channel, &value)) {\n        return (%s){ .tag = %s, .payload.member_%d = value };\n    }\n    return (%s){ .tag = %s };\n}\n",
				receiveUnion.CName, suffix, elementSpelling, receiveUnion.CName, unionTagName(receiveUnion, elementIndex), elementIndex, receiveUnion.CName, unionTagName(receiveUnion, eosIndex))
		}
		fmt.Fprintf(result, "\nstatic inline void hex_chan_free_%s(uintptr_t heap_identity, hex_chan *channel) {\n    (void)heap_identity;\n    hex_chan_free(channel);\n}\n", suffix)
	}
}

// writeMutexCore emits the scheduler-aware Mutex control block and core into
// the root module's C file: a heap-backed control block whose wait list parks
// tasks instead of blocking workers. Ownership follows Task identity, so a
// Task that migrates between workers keeps every Mutex it has not unlocked.
func writeMutexCore(result *strings.Builder, state *generatedConcurrencyState) {
	if !state.mutexNew && !state.mutexLock && !state.mutexUnlock && !state.mutexFree {
		return
	}
	result.WriteString(`
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
`)

}

// writeMutexInlineHelpers emits the Mutex adapters into the module header:
// the constructor/Error union adapter and the free adapter, which evaluates
// and accepts the checked Heap identity even though the current runtime
// ignores it. lock and unlock lower directly to the core (RFC 0069 Amendment
// 2 Item B) and need no inline wrapper.
func writeMutexInlineHelpers(result *strings.Builder, state *generatedConcurrencyState, stringState *generatedStringState) {
	if !state.mutexNew && !state.mutexLock && !state.mutexUnlock && !state.mutexFree {
		return
	}
	if state.mutexNew {
		union := state.mutexNewUnion
		if union != (compilerTypes.Type{}) {
			mutexIndex := unionMemberIndex(union, compilerTypes.MutexType)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			message := state.messageLiteral(stringState, mutexCreationFailed)
			fmt.Fprintf(result, "\nstatic inline %s hex_mutex_new_mutex(uintptr_t heap_identity, size_t line, size_t column, const hex_string *message) {\n    (void)heap_identity;\n    (void)message;\n    hex_mutex *mutex = hex_mutex_new();\n    if (mutex != nullptr) {\n        return (%s){ .tag = %s, .payload.member_%d = mutex };\n    }\n    return (%s){ .tag = %s, .payload.member_%d = hex_sched_error(line, column, &%s) };\n}\n",
				union.CName, union.CName, unionTagName(union, mutexIndex), mutexIndex, union.CName, unionTagName(union, errorIndex), errorIndex, message)
		}
	}
	fmt.Fprintf(result, "\nstatic inline void hex_mutex_free_hex_mutex(uintptr_t heap_identity, hex_mutex *mutex) {\n    (void)heap_identity;\n    hex_mutex_free(mutex);\n}\n")
}

// writeAtomicHelpers emits the inline Atomic<T> wrapper: a typedef over C23
// _Atomic(T) plus one sequentially consistent operation family per used
// element. Bool excludes fetch_add and fetch_sub. The receiver methods take
// the Atomic's address; the helper never copies an Atomic value. The
// <stdatomic.h> prerequisite arrives through the hexal.h umbrella (RFC
// 0062).
func writeAtomicHelpers(result *strings.Builder, state *generatedConcurrencyState) {
	if len(state.atomics) == 0 {
		return
	}
	atomicNames := make([]string, 0, len(state.atomics))
	for name := range state.atomics {
		atomicNames = append(atomicNames, name)
	}
	sort.Strings(atomicNames)
	for _, name := range atomicNames {
		atomic := state.atomics[name]
		suffix := atomicSuffix(atomic)
		element := atomic.Atomic.Element
		elementSpelling := typeSpelling(element)
		atomicSpelling := "hex_atomic_" + suffix
		fmt.Fprintf(result, "typedef _Atomic(%s) %s;\n", elementSpelling, atomicSpelling)
		// The constructor returns the element value, not the _Atomic type:
		// C ignores qualifiers on function return types, so an _Atomic
		// return would warn under -Werror.
		fmt.Fprintf(result, "static inline %s %s_new(%s value) {\n    return (%s)value;\n}\n", elementSpelling, atomicSpelling, elementSpelling, atomicSpelling)
		fmt.Fprintf(result, "static inline %s %s_load(%s *atomic) {\n    return atomic_load_explicit(atomic, memory_order_seq_cst);\n}\n", elementSpelling, atomicSpelling, atomicSpelling)
		fmt.Fprintf(result, "static inline void %s_store(%s *atomic, %s value) {\n    atomic_store_explicit(atomic, value, memory_order_seq_cst);\n}\n", atomicSpelling, atomicSpelling, elementSpelling)
		fmt.Fprintf(result, "static inline %s %s_exchange(%s *atomic, %s value) {\n    return atomic_exchange_explicit(atomic, value, memory_order_seq_cst);\n}\n", elementSpelling, atomicSpelling, atomicSpelling, elementSpelling)
		if !compilerTypes.Equal(element, compilerTypes.Bool) {
			fmt.Fprintf(result, "static inline %s %s_fetch_add(%s *atomic, %s value) {\n    return atomic_fetch_add_explicit(atomic, value, memory_order_seq_cst);\n}\n", elementSpelling, atomicSpelling, atomicSpelling, elementSpelling)
			fmt.Fprintf(result, "static inline %s %s_fetch_sub(%s *atomic, %s value) {\n    return atomic_fetch_sub_explicit(atomic, value, memory_order_seq_cst);\n}\n", elementSpelling, atomicSpelling, atomicSpelling, elementSpelling)
		}
		fmt.Fprintf(result, "static inline bool %s_compare_exchange(%s *atomic, %s expected, %s desired) {\n    return atomic_compare_exchange_strong_explicit(atomic, &expected, desired, memory_order_seq_cst, memory_order_seq_cst);\n}\n", atomicSpelling, atomicSpelling, elementSpelling, elementSpelling)
	}
}

// hoistConcurrencyInStatement emits the RFC 0037 spawn prologues for one
// statement before it renders: each spawn's argument frame is filled in
// source order and the task is created, so the later render only names the
// task handle. It runs before the try hoister, whose operand render may
// reference the spawned task handle.
func hoistConcurrencyInStatement(statement checker.Statement, body *strings.Builder, state *expressionValidation, indent string) error {
	// RFC 0057 Item 5: expression traversal lives in the shared
	// walkStatementExpressions; this hoister only acts on spawn nodes and
	// recurses into nested statement bodies itself.
	if err := walkStatementExpressions(statement, func(node *checker.Expression) error {
		if node.Kind == checker.SpawnExpression {
			return hoistSpawn(node, body, state, indent)
		}
		return nil
	}); err != nil {
		return err
	}
	switch statement := statement.(type) {
	case checker.IfStatement:
		for _, nested := range statement.Then {
			if err := hoistConcurrencyInStatement(nested, body, state, indent); err != nil {
				return err
			}
		}
		for _, branch := range statement.ElseIf {
			for _, nested := range branch.Body {
				if err := hoistConcurrencyInStatement(nested, body, state, indent); err != nil {
					return err
				}
			}
		}
		if statement.Else != nil {
			for _, nested := range statement.Else {
				if err := hoistConcurrencyInStatement(nested, body, state, indent); err != nil {
					return err
				}
			}
		}
	case checker.ForStatement:
		for _, nested := range statement.Body {
			if err := hoistConcurrencyInStatement(nested, body, state, indent); err != nil {
				return err
			}
		}
	case checker.WhileStatement:
		for _, nested := range statement.Body {
			if err := hoistConcurrencyInStatement(nested, body, state, indent); err != nil {
				return err
			}
		}
	}
	return nil
}

// hoistSpawn emits one spawn prologue: the argument frame is filled with the
// evaluated arguments in source order, then the task is created through the
// scheduler. The checked call node is recorded as the hoisted handle so the
// spawn expression renders as the task handle.
func hoistSpawn(node *checker.Expression, body *strings.Builder, state *expressionValidation, indent string) error {
	if node.Operand == nil || node.OperandType.Task == nil {
		return unknownExpressionDiagnostic("spawn expression has invalid checked metadata")
	}
	call := node.Operand
	site, err := spawnSiteFor(*call, state.functions, state.moduleID, state.owner)
	if err != nil {
		return err
	}
	state.spawnCounter++
	temp := fmt.Sprintf("hex_spawn_args_%d", state.spawnCounter)
	taskTemp := fmt.Sprintf("hex_spawn_task_%d", state.spawnCounter)
	if state.hoistedSpawns == nil {
		state.hoistedSpawns = make(map[*checker.Expression]string)
	}
	if len(site.params) > 0 {
		argsType := "hex_task_args_" + site.function
		fmt.Fprintf(body, "%s%s %s;\n", indent, argsType, temp)
		for index, argument := range call.Arguments {
			rendered, renderErr := renderOperandWithState(argument, state)
			if renderErr != nil {
				return renderErr
			}
			fmt.Fprintf(body, "%s%s.a%d = %s;\n", indent, temp, index+1, rendered)
		}
		spawnArgs := fmt.Sprintf("sizeof(%s), _Alignof(%s), &%s", argsType, argsType, temp)
		resultArgs := "0, 0"
		if site.result != (compilerTypes.Type{}) && !compilerTypes.Equal(site.result, compilerTypes.Nil) {
			resultArgs = fmt.Sprintf("sizeof(%s), _Alignof(%s)", typeSpelling(site.result), typeSpelling(site.result))
		}
		fmt.Fprintf(body, "%shex_task *%s = hex_task_spawn(hex_task_entry_%s, %s, %s);\n", indent, taskTemp, site.function, spawnArgs, resultArgs)
	} else {
		resultArgs := "0, 0"
		if site.result != (compilerTypes.Type{}) && !compilerTypes.Equal(site.result, compilerTypes.Nil) {
			resultArgs = fmt.Sprintf("sizeof(%s), _Alignof(%s)", typeSpelling(site.result), typeSpelling(site.result))
		}
		fmt.Fprintf(body, "%shex_task *%s = hex_task_spawn(hex_task_entry_%s, 0, 0, nullptr, %s);\n", indent, taskTemp, site.function, resultArgs)
	}
	state.hoistedSpawns[node.Operand] = taskTemp
	return nil
}

// renderSpawnExpression renders a hoisted spawn as its Task | Error union:
// the handle on success, the constructed Error on creation failure.
func renderSpawnExpression(node checker.Expression, state *expressionValidation) (string, error) {
	if state.hoistedSpawns == nil {
		return "", unknownExpressionDiagnostic("spawn expression reached generation without hoisting")
	}
	taskTemp, ok := state.hoistedSpawns[node.Operand]
	if !ok {
		return "", unknownExpressionDiagnostic("spawn expression reached generation without hoisting")
	}
	union := node.ResultType
	if union == (compilerTypes.Type{}) || union.Union == nil {
		return "", unknownExpressionDiagnostic("spawn expression has no checked result union")
	}
	taskIndex := unionMemberIndex(union, node.OperandType)
	errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
	if taskIndex < 0 || errorIndex < 0 {
		return "", unknownExpressionDiagnostic("spawn result union is missing its Task or Error member")
	}
	message, err := errorMessageLiteral(state, taskCreationFailed)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("(%s ? (%s){ .tag = %s, .payload.member_%d = %s } : (%s){ .tag = %s, .payload.member_%d = hex_sched_error(%d, %d, &%s) })",
		taskTemp, union.CName, unionTagName(union, taskIndex), taskIndex, taskTemp,
		union.CName, unionTagName(union, errorIndex), errorIndex, node.SourceLine, node.SourceColumn, message), nil
}

// errorMessageLiteral resolves one failure message literal registered during
// discovery.
func errorMessageLiteral(state *expressionValidation, payload string) (string, error) {
	if state.strings == nil {
		return "", unknownExpressionDiagnostic("concurrency failure without the string literal registry")
	}
	index, ok := state.strings.seen[payload]
	if !ok {
		return "", unknownExpressionDiagnostic("concurrency failure message is missing from the literal registry: " + payload)
	}
	return stringLiteralCName(index - 1), nil
}

// renderTaskMethod renders one Task handle method call.
func renderTaskMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Task == nil {
		return "", unknownExpressionDiagnostic("task method without a checked receiver")
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "join":
		suffix := taskSuffix(node.OperandType)
		return "hex_task_join_" + suffix + "(" + receiver + ")", nil
	case "detach":
		return "hex_task_detach(" + receiver + ")", nil
	}
	return "", unknownExpressionDiagnostic("unknown task method " + node.Name)
}

// renderChannelConstructor renders Channel<T>.new(heap, capacity) as its
// Channel | Error union.
func renderChannelConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if node.OperandType.Channel == nil || len(node.Arguments) != 2 {
		return "", unknownExpressionDiagnostic("channel constructor without a checked channel type")
	}
	heap, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	capacity, err := renderOperandWithState(node.Arguments[1], state)
	if err != nil {
		return "", err
	}
	message, err := errorMessageLiteral(state, channelCreationFailed)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_chan_new_%s((%s).identity, (size_t)(%s), %d, %d, &%s)",
		channelSuffix(node.OperandType), heap, capacity, node.SourceLine, node.SourceColumn, message), nil
}

// renderChannelMethod renders one Channel handle method call.
func renderChannelMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Channel == nil {
		return "", unknownExpressionDiagnostic("channel method without a checked receiver")
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if err != nil {
		return "", err
	}
	suffix := channelSuffix(node.OperandType)
	switch node.Name {
	case "send":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("channel send without a checked value")
		}
		value, valueErr := renderOperandWithState(node.Arguments[0], state)
		if valueErr != nil {
			return "", valueErr
		}
		message, messageErr := errorMessageLiteral(state, channelSendFailed)
		if messageErr != nil {
			return "", messageErr
		}
		return fmt.Sprintf("hex_chan_send_%s(%s, %s, %d, %d, &%s)", suffix, receiver, value, node.SourceLine, node.SourceColumn, message), nil
	case "receive":
		return "hex_chan_recv_" + suffix + "(" + receiver + ")", nil
	case "close":
		return "hex_chan_close(" + receiver + ")", nil
	case "length":
		return "hex_chan_length(" + receiver + ")", nil
	case "capacity":
		return "hex_chan_capacity(" + receiver + ")", nil
	case "is_closed":
		return "hex_chan_is_closed(" + receiver + ")", nil
	case "free":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("channel free without a checked heap")
		}
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		return fmt.Sprintf("hex_chan_free_%s((%s).identity, %s)", suffix, heap, receiver), nil
	}
	return "", unknownExpressionDiagnostic("unknown channel method " + node.Name)
}

// renderMutexConstructor renders Mutex.new(heap) as its Mutex | Error union.
func renderMutexConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if len(node.Arguments) != 1 {
		return "", unknownExpressionDiagnostic("mutex constructor without a checked heap")
	}
	heap, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	message, err := errorMessageLiteral(state, mutexCreationFailed)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_mutex_new_mutex((%s).identity, %d, %d, &%s)", heap, node.SourceLine, node.SourceColumn, message), nil
}

// renderMutexMethod renders one Mutex handle method call.
func renderMutexMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || !compilerTypes.IsMutex(node.OperandType) {
		return "", unknownExpressionDiagnostic("mutex method without a checked receiver")
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "lock":
		return "hex_mutex_lock(" + receiver + ")", nil
	case "unlock":
		return "hex_mutex_unlock(" + receiver + ")", nil
	case "free":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("mutex free without a checked heap")
		}
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		return fmt.Sprintf("hex_mutex_free_hex_mutex((%s).identity, %s)", heap, receiver), nil
	}
	return "", unknownExpressionDiagnostic("unknown mutex method " + node.Name)
}

// renderAtomicConstructor renders Atomic<T>.new(initial).
func renderAtomicConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if node.OperandType.Atomic == nil || len(node.Arguments) != 1 {
		return "", unknownExpressionDiagnostic("atomic constructor without a checked element")
	}
	initial, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return "hex_atomic_" + atomicSuffix(node.OperandType) + "_new(" + initial + ")", nil
}

// renderAtomicMethod renders one Atomic method on the receiver's address.
func renderAtomicMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Atomic == nil {
		return "", unknownExpressionDiagnostic("atomic method without a checked receiver")
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if err != nil {
		return "", err
	}
	helper := "hex_atomic_" + atomicSuffix(node.OperandType) + "_" + node.Name
	switch node.Name {
	case "load":
		return helper + "(&(" + receiver + "))", nil
	case "store", "exchange", "fetch_add", "fetch_sub":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("atomic method without a checked value")
		}
		value, valueErr := renderOperandWithState(node.Arguments[0], state)
		if valueErr != nil {
			return "", valueErr
		}
		return helper + "(&(" + receiver + "), " + value + ")", nil
	case "compare_exchange":
		if len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic("atomic compare_exchange without checked operands")
		}
		expected, expectedErr := renderOperandWithState(node.Arguments[0], state)
		if expectedErr != nil {
			return "", expectedErr
		}
		desired, desiredErr := renderOperandWithState(node.Arguments[1], state)
		if desiredErr != nil {
			return "", desiredErr
		}
		return helper + "(&(" + receiver + "), " + expected + ", " + desired + ")", nil
	}
	return "", unknownExpressionDiagnostic("unknown atomic method " + node.Name)
}
