package generator

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// Concurrency lowering emits the Task<T>, spawn, join/detach/yield,
// Channel<T>, Mutex, and Atomic<T> families.
//
// The scheduler runtime is a single C23 block owned by the generated
// hexal/concurrency.c component: one global ready queue guarded by a native
// mutex and condition variable, a fixed set of worker threads created with
// C23 <threads.h>, and platform fiber contexts. Windows uses the Fiber APIs;
// POSIX uses ucontext with one caller-allocated stack per task. Task control
// blocks and argument frames are scheduler-owned malloc storage; user
// payloads keep their explicit allocators. The program-wide handle typedefs
// and runtime entry-point declarations live in hexal/concurrency.h; this
// file emits only the module-owned typed helpers, argument frames, spawn
// adapters, and statement lowering that depend on module types.

// generatedConcurrencyState records every concurrency feature the program
// uses, so the emitted runtime contains exactly the families the program
// needs.
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
	spawnFail             bool
	channelNew            bool
	channelSend           bool
	mutexCreate           bool
	fileLiteral           literalHandle
	taskCreationFailed    literalHandle
	channelCreationFailed literalHandle
	channelSendFailed     literalHandle
	mutexCreationFailed   literalHandle
}

// spawnSite is one spawned function: its checked signature drives the
// argument frame and the entry adapter. module is the canonical id of the
// module that owns the spawned function; the adapter is emitted beside that
// function's definition.
type spawnSite struct {
	name     string
	function string
	module   string
	params   []compilerTypes.Type
	result   compilerTypes.Type // zero Type means Nil
}

// errorMessagePayloads are the recoverable-failure messages of the Task,
// Channel, and Mutex operations. Each is registered as a String literal only
// when the matching operation is used.
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
// the ordinary literal machinery. logicalKey is the module's source-map
// filename, recorded as the failure Errors' file.
func discoverGeneratedConcurrency(program checker.Program, functions map[string]compilerTypes.Type, literals *literalRegistry, moduleID, owner, logicalKey string) *generatedConcurrencyState {
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
		// Declaration-only reachability links the runtime cores exactly like
		// an operation: naming a handle type selects the scheduler support it
		// needs, and naming an Atomic<T> selects the atomic typedefs. The
		// collect-only handle flags (channelNew, channelSend, ...) stay
		// operation-driven: their failure literals and adapters are emitted
		// only where a module actually performs the operation.
		Type: func(typ compilerTypes.Type) {
			switch {
			case typ.Task != nil:
				state.used = true
				state.taskTypes[typ.CName] = typ
			case typ.Channel != nil:
				state.used = true
				state.channels[typ.CName] = typ
			case compilerTypes.IsMutex(typ):
				state.used = true
			case typ.Atomic != nil:
				state.atomics[typ.CName] = typ
			}
		},
		Expression: func(node checker.Expression) {
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
						panic(err)
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
		},
	}
	walkProgram(program, visitor)
	if state.used {
		literals.used = true
		literals.strand = true
		state.fileLiteral = literals.Intern(logicalKey)
		if state.spawnFail {
			state.taskCreationFailed = literals.Intern(taskCreationFailed)
		}
		if state.channelNew {
			state.channelCreationFailed = literals.Intern(channelCreationFailed)
		}
		if state.channelSend {
			state.channelSendFailed = literals.Intern(channelSendFailed)
		}
		if state.mutexCreate {
			state.mutexCreationFailed = literals.Intern(mutexCreationFailed)
		}
	}
	return state
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
		function: privateCName(functionNameKind, node.Operand.Name, moduleOwner(node.Operand.Module, localOwner)),
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
func (state *generatedConcurrencyState) messageLiteral(literals *literalRegistry, handle literalHandle) string {
	return literals.CName(handle)
}

// writeErrorHelper emits the runtime Error-construction helper hex_sched_error
// once, before any operation family that can fail.
func (state *generatedConcurrencyState) writeErrorHelper(result *strings.Builder, literals *literalRegistry) {
	fmt.Fprintf(result, "\nstatic inline hex_t_Error hex_sched_error(size_t line, size_t column, const hex_string *message) {\n")
	fmt.Fprintf(result, "    return (hex_t_Error){\n")
	fmt.Fprintf(result, "        .hex_m_file = &%s,\n", literals.CName(state.fileLiteral))
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

// writeConcurrencyInlineHelpers emits the per-element, per-result inline
// wrappers into the module header: the spawn argument frames (which name user
// parameter types), the Task join helpers, the Channel and Mutex operation
// families, and the Atomic family. They are state-free and only call the
// runtime core through the hexal/concurrency.h declarations.
func writeConcurrencyInlineHelpers(result *strings.Builder, state *generatedConcurrencyState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || !state.used && len(state.atomics) == 0 {
		return nil
	}
	if state.used {
		if state.spawnFail || state.channelNew || state.channelSend || state.mutexNew {
			// Every recoverable operation constructs its failure Error
			// through hex_sched_error, spawn prologues included. One helper
			// precedes every family that references it; the per-family
			// writers must not re-emit it.
			state.writeErrorHelper(result, literals)
		}
		writeSpawnArgFrames(result, state.spawns)
		writeTaskTypeHelpers(result, state)
		if err := writeChannelInlineHelpers(result, state, literals, tags); err != nil {
			return err
		}
		if err := writeMutexInlineHelpers(result, state, literals, tags); err != nil {
			return err
		}
	}
	writeAtomicHelpers(result, state)
	return nil
}

// writeTaskTypeHelpers emits the per-result join helper that copies R out of
// the task frame and reclaims the task. The handle typedefs themselves are
// emitted in the header prelude.
func writeTaskTypeHelpers(result *strings.Builder, state *generatedConcurrencyState) {
	if len(state.joinTypes) == 0 {
		return
	}
	result.WriteString("\n// join copies R out of the result frame and reclaims the task storage.\n")
	names := slices.Sorted(maps.Keys(state.joinTypes))
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

// writeChannelInlineHelpers emits the per-element Channel adapters into the
// module header: new, send, and receive build the checked result unions
// (Channel | Error, Nil | Error, and T | EoS) around the shared core, and
// free adapts the checked Heap identity argument. close, length, capacity,
// and is_closed lower directly to the core and need no inline wrapper.
func writeChannelInlineHelpers(result *strings.Builder, state *generatedConcurrencyState, literals *literalRegistry, tags *tagRegistry) error {
	if len(state.channels) == 0 {
		return nil
	}
	channelNames := slices.Sorted(maps.Keys(state.channels))
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
				message := state.messageLiteral(literals, state.channelCreationFailed)
				fmt.Fprintf(result, "\nstatic inline %s hex_chan_new_%s(uintptr_t heap_identity, size_t capacity, size_t line, size_t column, const hex_string *message) {\n    (void)heap_identity;\n    (void)message;\n    hex_chan *channel = hex_chan_new(capacity, sizeof(%s));\n    if (channel != nullptr) {\n        return (%s){ .tag = %s, .payload.%s = channel };\n    }\n    return (%s){ .tag = %s, .payload.%s = hex_sched_error(line, column, &%s) };\n}\n",
					union.CName, suffix, elementSpelling, union.CName, tags.unionMemberTag(compilerTypes.UnionMembers(union)[channelIndex]), tags.unionPayloadField(compilerTypes.UnionMembers(union)[channelIndex]), union.CName, tags.unionMemberTag(compilerTypes.UnionMembers(union)[errorIndex]), tags.unionPayloadField(compilerTypes.UnionMembers(union)[errorIndex]), message)
			}
		}
		if state.channelSend {
			union := state.channelSendUnions[channel.CName]
			if union != (compilerTypes.Type{}) {
				nilIndex := unionMemberIndex(union, compilerTypes.Nil)
				errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
				message := state.messageLiteral(literals, state.channelSendFailed)
				fmt.Fprintf(result, "\nstatic inline %s hex_chan_send_%s(hex_chan *channel, %s value, size_t line, size_t column, const hex_string *message) {\n    (void)message;\n    if (hex_chan_send(channel, &value)) {\n        return (%s){ .tag = %s };\n    }\n    return (%s){ .tag = %s, .payload.%s = hex_sched_error(line, column, &%s) };\n}\n",
					union.CName, suffix, elementSpelling, union.CName, tags.unionMemberTag(compilerTypes.UnionMembers(union)[nilIndex]), union.CName, tags.unionMemberTag(compilerTypes.UnionMembers(union)[errorIndex]), tags.unionPayloadField(compilerTypes.UnionMembers(union)[errorIndex]), message)
			}
		}
		// The receive union is emitted for every used Channel<T>: receive
		// needs the T | EoS union. close, length, capacity, is_closed, and
		// free lower directly to the core; the free adapter is emitted here
		// because it adapts the checked Heap identity argument.
		receiveUnion := state.channelReceiveUnions[channel.CName]
		if receiveUnion != (compilerTypes.Type{}) {
			elementIndex := unionMemberIndex(receiveUnion, element)
			eosIndex := unionMemberIndex(receiveUnion, compilerTypes.EoS)
			fmt.Fprintf(result, "\nstatic inline %s hex_chan_recv_%s(hex_chan *channel) {\n    %s value;\n    if (hex_chan_receive(channel, &value)) {\n        return (%s){ .tag = %s, .payload.%s = value };\n    }\n    return (%s){ .tag = %s };\n}\n",
				receiveUnion.CName, suffix, elementSpelling, receiveUnion.CName, tags.unionMemberTag(compilerTypes.UnionMembers(receiveUnion)[elementIndex]), tags.unionPayloadField(compilerTypes.UnionMembers(receiveUnion)[elementIndex]), receiveUnion.CName, tags.unionMemberTag(compilerTypes.UnionMembers(receiveUnion)[eosIndex]))
		}
		fmt.Fprintf(result, "\nstatic inline void hex_chan_free_%s(uintptr_t heap_identity, hex_chan *channel) {\n    (void)heap_identity;\n    hex_chan_free(channel);\n}\n", suffix)
	}
	return nil
}

// writeMutexInlineHelpers emits the Mutex adapters into the module header:
// the constructor/Error union adapter and the free adapter, which evaluates
// and accepts the checked Heap identity even though the current runtime
// ignores it. lock and unlock lower directly to the core and need no inline
// wrapper.
func writeMutexInlineHelpers(result *strings.Builder, state *generatedConcurrencyState, literals *literalRegistry, tags *tagRegistry) error {
	if !state.mutexNew && !state.mutexLock && !state.mutexUnlock && !state.mutexFree {
		return nil
	}
	if state.mutexNew {
		union := state.mutexNewUnion
		if union != (compilerTypes.Type{}) {
			mutexIndex := unionMemberIndex(union, compilerTypes.MutexType)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			message := state.messageLiteral(literals, state.mutexCreationFailed)
			fmt.Fprintf(result, "\nstatic inline %s hex_mutex_new_mutex(uintptr_t heap_identity, size_t line, size_t column, const hex_string *message) {\n    (void)heap_identity;\n    (void)message;\n    hex_mutex *mutex = hex_mutex_new();\n    if (mutex != nullptr) {\n        return (%s){ .tag = %s, .payload.%s = mutex };\n    }\n    return (%s){ .tag = %s, .payload.%s = hex_sched_error(line, column, &%s) };\n}\n",
				union.CName, union.CName, tags.unionMemberTag(compilerTypes.UnionMembers(union)[mutexIndex]), tags.unionPayloadField(compilerTypes.UnionMembers(union)[mutexIndex]), union.CName, tags.unionMemberTag(compilerTypes.UnionMembers(union)[errorIndex]), tags.unionPayloadField(compilerTypes.UnionMembers(union)[errorIndex]), message)
		}
	}
	fmt.Fprintf(result, "\nstatic inline void hex_mutex_free_hex_mutex(uintptr_t heap_identity, hex_mutex *mutex) {\n    (void)heap_identity;\n    hex_mutex_free(mutex);\n}\n")
	return nil
}

// writeAtomicHelpers emits the inline Atomic<T> wrapper: a typedef over C23
// _Atomic(T) plus one sequentially consistent operation family per used
// element. Bool excludes fetch_add and fetch_sub. The receiver methods take
// the Atomic's address; the helper never copies an Atomic value. The
// <stdatomic.h> prerequisite arrives through the hexal.h umbrella.
func writeAtomicHelpers(result *strings.Builder, state *generatedConcurrencyState) {
	if len(state.atomics) == 0 {
		return
	}
	atomicNames := slices.Sorted(maps.Keys(state.atomics))
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

// hoistConcurrencyInStatement emits the spawn prologues for one statement
// before it renders: each spawn's argument frame is filled in source order
// and the task is created, so the later render only names the task handle.
// It runs before the try hoister, whose operand render may reference the
// spawned task handle.
func hoistConcurrencyInStatement(statement checker.Statement, body *strings.Builder, state *expressionValidation, indent string) error {
	// Expression traversal lives in the shared walkStatementExpressions,
	// which visits only this statement's own expressions: a spawn inside a
	// nested statement body is hoisted when that body's own statement list
	// renders, so the prologue stays inside the block that contains it.
	if err := walkStatementExpressions(statement, func(node *checker.Expression) error {
		if node.Kind == checker.SpawnExpression {
			return hoistSpawn(node, body, state, indent)
		}
		return nil
	}); err != nil {
		return err
	}
	switch statement.(type) {
	case checker.IfStatement, checker.ForStatement, checker.WhileStatement,
		checker.Declaration, checker.Assignment, checker.CallStatement, checker.TryStatement,
		checker.ReturnStatement, checker.BreakStatement, checker.ContinueStatement,
		checker.DeferStatement, checker.ErrdeferStatement, checker.FunctionDeclaration,
		checker.MethodDeclaration:
		// Block statements carry no expressions beyond their own operands,
		// and leaf statements none; nested bodies hoist at their own
		// statement list.
	default:
		return unknownExpressionDiagnostic("unsupported checked statement")
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
	taskMember := compilerTypes.UnionMembers(union)[taskIndex]
	errorMember := compilerTypes.UnionMembers(union)[errorIndex]
	return fmt.Sprintf("(%s ? (%s){ .tag = %s, .payload.%s = %s } : (%s){ .tag = %s, .payload.%s = hex_sched_error(%d, %d, &%s) })",
		taskTemp, union.CName, state.tags.unionMemberTag(taskMember), state.tags.unionPayloadField(taskMember), taskTemp,
		union.CName, state.tags.unionMemberTag(errorMember), state.tags.unionPayloadField(errorMember), node.SourceLine, node.SourceColumn, message), nil
}

// errorMessageLiteral resolves one failure message literal registered during
// discovery.
func errorMessageLiteral(state *expressionValidation, payload string) (string, error) {
	handle, ok := state.strings.Lookup(payload)
	if !ok {
		return "", unknownExpressionDiagnostic("concurrency failure message is missing from the literal registry: " + payload)
	}
	return state.strings.CName(handle), nil
}

// renderTaskMethod renders one Task handle method call.
func renderTaskMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Task == nil {
		return "", unknownExpressionDiagnostic("task method without a checked receiver")
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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

func validateConcurrencyExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.SpawnExpression:
		if node.Operand == nil || node.OperandType.Task == nil || node.OperandType.Task.Result == (compilerTypes.Type{}) || node.ResultType.Union == nil || !compilerTypes.Equal(node.Element, node.OperandType.Task.Result) || node.SourceLine <= 0 {
			return unknownExpressionDiagnostic("spawn expression has invalid checked metadata")
		}
		if unionMemberIndex(node.ResultType, node.OperandType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnostic("spawn result union is missing its Task or Error member")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("spawn result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Type{}, state); err != nil {
			return err
		}
		return nil
	case checker.TaskYieldExpression:
		if node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("Task.yield() result type is not zero")
		}
		return nil
	case checker.TaskMethodCallExpression:
		if node.Operand == nil || node.OperandType.Task == nil {
			return unknownExpressionDiagnostic("task method has invalid checked metadata")
		}
		switch node.Name {
		case "join":
			if !compilerTypes.Equal(node.Element, node.OperandType.Task.Result) || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic("task join result does not match its Task result")
			}
		case "detach":
			if node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("task detach result type is not zero")
			}
		default:
			return unknownExpressionDiagnostic("unknown task method " + node.Name)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("task method result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ChannelConstructorExpression:
		if node.OperandType.Channel == nil || len(node.Arguments) != 2 || !compilerTypes.Equal(node.Element, node.OperandType.Channel.Element) || node.ResultType.Union == nil || node.SourceLine <= 0 {
			return unknownExpressionDiagnostic("channel constructor has invalid checked metadata")
		}
		if unionMemberIndex(node.ResultType, node.OperandType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnostic("channel constructor union is missing its Channel or Error member")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("channel constructor result type does not match its expected type")
		}
		if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[1], state)
	case checker.ChannelMethodCallExpression:
		if node.Operand == nil || node.OperandType.Channel == nil || !compilerTypes.Equal(node.Element, node.OperandType.Channel.Element) {
			return unknownExpressionDiagnostic("channel method has invalid checked metadata")
		}
		switch node.Name {
		case "send":
			if len(node.Arguments) != 1 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, compilerTypes.Nil) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 || node.SourceLine <= 0 {
				return unknownExpressionDiagnostic("channel send has invalid checked metadata")
			}
		case "receive":
			if len(node.Arguments) != 0 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, node.Element) < 0 || unionMemberIndex(node.ResultType, compilerTypes.EoS) < 0 {
				return unknownExpressionDiagnostic("channel receive has invalid checked metadata")
			}
		case "close":
			if len(node.Arguments) != 0 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("channel close has invalid checked metadata")
			}
		case "length", "capacity":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
				return unknownExpressionDiagnostic("channel " + node.Name + " has invalid checked metadata")
			}
		case "is_closed":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("channel is_closed has invalid checked metadata")
			}
		case "free":
			if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("channel free has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown channel method " + node.Name)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("channel method result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.MutexConstructorExpression:
		if len(node.Arguments) != 1 || !compilerTypes.IsMutex(node.OperandType) || node.ResultType.Union == nil || node.SourceLine <= 0 {
			return unknownExpressionDiagnostic("mutex constructor has invalid checked metadata")
		}
		if unionMemberIndex(node.ResultType, compilerTypes.MutexType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnostic("mutex constructor union is missing its Mutex or Error member")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("mutex constructor result type does not match its expected type")
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.MutexMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsMutex(node.OperandType) || node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("mutex method has invalid checked metadata")
		}
		switch node.Name {
		case "lock", "unlock":
			if len(node.Arguments) != 0 {
				return unknownExpressionDiagnostic("mutex " + node.Name + " expects no arguments")
			}
		case "free":
			if len(node.Arguments) != 1 {
				return unknownExpressionDiagnostic("mutex free expects one argument")
			}
		default:
			return unknownExpressionDiagnostic("unknown mutex method " + node.Name)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("mutex method result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.AtomicConstructorExpression:
		if node.OperandType.Atomic == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.Element, node.OperandType.Atomic.Element) || !compilerTypes.Equal(node.ResultType, node.OperandType) {
			return unknownExpressionDiagnostic("atomic constructor has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("atomic constructor result type does not match its expected type")
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.AtomicMethodCallExpression:
		if node.Operand == nil || node.OperandType.Atomic == nil || !compilerTypes.Equal(node.Element, node.OperandType.Atomic.Element) {
			return unknownExpressionDiagnostic("atomic method has invalid checked metadata")
		}
		switch node.Name {
		case "load":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic("atomic load has invalid checked metadata")
			}
		case "store":
			if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("atomic store has invalid checked metadata")
			}
		case "exchange", "fetch_add", "fetch_sub":
			if len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic("atomic " + node.Name + " has invalid checked metadata")
			}
		case "compare_exchange":
			if len(node.Arguments) != 2 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("atomic compare_exchange has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown atomic method " + node.Name)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("atomic method result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	}
	return unknownExpressionDiagnostic("unsupported concurrency expression")
}
