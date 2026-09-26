package generator

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"hexal/compiler/checker"
	"hexal/compiler/specdata"
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
// blocks and argument frames are scheduler-owned allocator storage; user
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
	// rest marks a spawn of a rest signature. fixed is the fixed-parameter
	// count, element and slice are the rest element T and Slice<T>, and
	// restCount is the statically known number of rest elements at this site.
	// The frame stores every fixed value and every rest element inline; the
	// adapter builds one Slice over them.
	rest      bool
	fixed     int
	element   compilerTypes.Type
	slice     compilerTypes.Type
	restCount int
}

// key names this site's argument frame and entry adapter. A rest site's rest
// count is part of the identity because the frame layout depends on it: two
// sites targeting one function with different rest counts are distinct.
func (site spawnSite) key() string {
	if site.rest {
		return fmt.Sprintf("%s_r%d", site.function, site.restCount)
	}
	return site.function
}

// hasFrame reports whether the site needs a task argument frame. A rest site
// with no fixed parameters and no elements passes the empty Slice directly and
// needs no frame, which also avoids an empty C struct.
func (site spawnSite) hasFrame() bool {
	if site.rest {
		return site.fixed > 0 || site.restCount > 0
	}
	return len(site.params) > 0
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
func discoverGeneratedConcurrency(program checker.Program, functions map[string]compilerTypes.Type, literals *literalRegistry, moduleID, owner, logicalKey string) (*generatedConcurrencyState, error) {
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
		Type: func(typ compilerTypes.Type) error {
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
			return nil
		},
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
			case checker.TimeExpression:
				if node.Name == "task_sleep" {
					// Sleep parks the current Task, so it selects the scheduler.
					state.used = true
				}
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
	if state.used {
		literals.used = true
		literals.requireErrorText()
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
	return state, nil
}

// spawnSiteFor derives one spawn site from the checked call node: the named
// callee and the parameter and result types that shape the entry adapter.
// localModule is the canonical id and localOwner the encoded owner of the
// module being generated; a local callee falls back to them so its C name
// matches the definition in its own module pair.
func spawnSiteFor(node checker.Expression, functions map[string]compilerTypes.Type, localModule, localOwner string) (spawnSite, error) {
	if node.Kind != checker.CallExpression || node.Operand == nil || node.Operand.Kind != checker.FunctionReferenceExpression || node.Operand.Name == "" {
		return spawnSite{}, unknownExpressionDiagnostic()
	}
	signature, ok := functions[node.Operand.Name]
	if !ok || signature.Signature == nil {
		return spawnSite{}, unknownExpressionDiagnostic()
	}
	if node.Rest != signature.Signature.Rest {
		return spawnSite{}, unknownExpressionDiagnostic()
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
		fixed:    len(signature.Signature.Parameters),
	}
	if node.Rest {
		site.rest = true
		site.fixed = node.RestStart
		site.element = node.RestElement
		site.slice = node.RestSlice
		site.restCount = len(node.Arguments) - node.RestStart
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

// Concurrency fragment models carry only decided values: canonical C names,
// union tags, literal names, and pre-rendered expressions. Templates carry
// presentation only.
type schedErrorHelperModel struct {
	FileCName string
}

type taskJoinModel struct {
	Suffix string
}

type taskJoinValueModel struct {
	Suffix         string
	ResultSpelling string
}

type spawnFrameModel struct {
	Key    string
	Fields []string
}

type chanNewModel struct {
	UnionCName      string
	Suffix          string
	ElementSpelling string
	ChannelTag      string
	ChannelField    string
	ErrorTag        string
	ErrorField      string
	KindTag         string
	Message         string
}

type chanSendModel struct {
	UnionCName      string
	Suffix          string
	ElementSpelling string
	NilTag          string
	ErrorTag        string
	ErrorField      string
	KindTag         string
	Message         string
}

type chanRecvModel struct {
	UnionCName      string
	Suffix          string
	ElementSpelling string
	ElementTag      string
	ElementField    string
	EosTag          string
}

type suffixModel struct {
	Suffix string
}

type mutexNewModel struct {
	UnionCName string
	MutexTag   string
	MutexField string
	ErrorTag   string
	ErrorField string
	KindTag    string
	Message    string
}

type atomicHelperModel struct {
	ElementSpelling string
	AtomicSpelling  string
}

type spawnAdapterModel struct {
	Key            string
	HasFrame       bool
	Function       string
	Arguments      string
	ResultSpelling string
}

type spawnFrameDeclModel struct {
	Indent   string
	ArgsType string
	Temp     string
}

type spawnFrameFillModel struct {
	Indent string
	Temp   string
	Index  int
	Value  string
}

type spawnCallFrameModel struct {
	Indent     string
	TaskTemp   string
	Key        string
	ArgsType   string
	Temp       string
	ResultArgs string
}

type spawnCallBareModel struct {
	Indent     string
	TaskTemp   string
	Key        string
	ResultArgs string
}

// writeErrorHelper emits the runtime Error-construction helper hex_sched_error
// once, before any operation family that can fail. The caller supplies the
// exact ErrorKind for its own failure reason: Task, Channel, and Mutex
// creation report ResourceExhausted; Channel send after close reports Closed.
func (state *generatedConcurrencyState) writeErrorHelper(result *strings.Builder, literals *literalRegistry) error {
	return renderInto(result, "concurrency.h", "sched_error_helper", schedErrorHelperModel{FileCName: literals.CName(state.fileLiteral)})
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
			if err := state.writeErrorHelper(result, literals); err != nil {
				return err
			}
		}
		if err := writeSpawnArgFrames(result, state.spawns); err != nil {
			return err
		}
		if err := writeTaskTypeHelpers(result, state); err != nil {
			return err
		}
		if err := writeChannelInlineHelpers(result, state, literals, tags); err != nil {
			return err
		}
		if err := writeMutexInlineHelpers(result, state, literals, tags); err != nil {
			return err
		}
	}
	return writeAtomicHelpers(result, state)
}

// writeTaskTypeHelpers emits the per-result join helper that copies R out of
// the task frame and reclaims the task. The handle typedefs themselves are
// emitted in the header prelude.
func writeTaskTypeHelpers(result *strings.Builder, state *generatedConcurrencyState) error {
	if len(state.joinTypes) == 0 {
		return nil
	}
	if err := renderInto(result, "concurrency.h", "task_join_comment", struct{}{}); err != nil {
		return err
	}
	names := slices.Sorted(maps.Keys(state.joinTypes))
	for _, name := range names {
		task := state.joinTypes[name]
		suffix := taskSuffix(task)
		if task.Task.Result == (compilerTypes.Type{}) || compilerTypes.Equal(task.Task.Result, compilerTypes.Nil) {
			if err := renderInto(result, "concurrency.h", "task_join_void", taskJoinModel{Suffix: suffix}); err != nil {
				return err
			}
			continue
		}
		if err := renderInto(result, "concurrency.h", "task_join_value", taskJoinValueModel{
			Suffix:         suffix,
			ResultSpelling: typeSpelling(task.Task.Result),
		}); err != nil {
			return err
		}
	}
	return nil
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
func writeSpawnAdapters(result *strings.Builder, sites []spawnSite) error {
	if len(sites) == 0 {
		return nil
	}
	for _, site := range sites {
		model := spawnAdapterModel{
			Key:       site.key(),
			HasFrame:  site.hasFrame(),
			Function:  site.function,
			Arguments: spawnArguments(site),
		}
		block := "spawn_adapter_void"
		if site.result != (compilerTypes.Type{}) && !compilerTypes.Equal(site.result, compilerTypes.Nil) {
			block = "spawn_adapter_value"
			model.ResultSpelling = typeSpelling(site.result)
		}
		if err := renderInto(result, "module.c", block, model); err != nil {
			return err
		}
	}
	return nil
}

// spawnArguments is the adapter's comma-joined call argument list: the
// frame's fixed fields in source order, then the rest Slice over the
// inline element fields.
func spawnArguments(site spawnSite) string {
	if !site.hasFrame() {
		if site.rest {
			return restFrameSlice(site)
		}
		return ""
	}
	arguments := make([]string, 0, site.fixed+1)
	for index := 0; index < site.fixed; index++ {
		arguments = append(arguments, fmt.Sprintf("args->a%d", index+1))
	}
	if site.rest {
		arguments = append(arguments, restFrameSlice(site))
	}
	return strings.Join(arguments, ", ")
}

// restFrameSlice renders the adapter's Slice over the frame's inline rest
// element fields: the canonical empty Slice for zero elements, a
// compound-literal array otherwise.
func restFrameSlice(site spawnSite) string {
	data := "nullptr"
	if site.restCount > 0 {
		fields := make([]string, site.restCount)
		for index := 0; index < site.restCount; index++ {
			fields[index] = fmt.Sprintf("args->a%d", site.fixed+index+1)
		}
		data = "(" + typeSpelling(site.element) + "[]){" + strings.Join(fields, ", ") + "}"
	}
	return "(" + typeSpelling(site.slice) + "){.data = " + data + ", .length = " + strconv.Itoa(site.restCount) + "}"
}

// writeSpawnArgFrames emits one argument-frame struct per spawned function
// into the module header, before any function body that fills one.
func writeSpawnArgFrames(result *strings.Builder, sites []spawnSite) error {
	if len(sites) == 0 {
		return nil
	}
	for _, site := range sites {
		if !site.hasFrame() {
			continue
		}
		fields := make([]string, 0, len(site.params))
		for index, parameter := range site.params {
			if site.rest && index == len(site.params)-1 {
				// The rest parameter stores each element inline, one field each.
				for offset := 0; offset < site.restCount; offset++ {
					fields = append(fields, declaration(site.element, fmt.Sprintf("a%d", index+1+offset), true))
				}
				continue
			}
			fields = append(fields, declaration(parameter, fmt.Sprintf("a%d", index+1), true))
		}
		if err := renderInto(result, "concurrency.h", "spawn_arg_frame", spawnFrameModel{Key: site.key(), Fields: fields}); err != nil {
			return err
		}
	}
	return nil
}

// writeChannelInlineHelpers emits the per-element Channel adapters into the
// module header: new, send, and receive build the checked result unions
// (Channel | Error, Nil | Error, and T | EoS) around the shared core, and
// free adapts the checked Heap token argument. close, length, capacity,
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
				unionMembers := compilerTypes.UnionMembers(union)
				channelIndex := unionMemberIndex(union, channel)
				errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
				channelMember, _ := unionMembers.At(channelIndex)
				errorMember, _ := unionMembers.At(errorIndex)
				message := state.messageLiteral(literals, state.channelCreationFailed)
				if err := renderInto(result, "concurrency.h", "chan_new_adapter", chanNewModel{
					UnionCName:      union.CName,
					Suffix:          suffix,
					ElementSpelling: elementSpelling,
					ChannelTag:      tags.unionMemberTag(channelMember),
					ChannelField:    tags.unionPayloadField(channelMember),
					ErrorTag:        tags.unionMemberTag(errorMember),
					ErrorField:      tags.unionPayloadField(errorMember),
					KindTag:         errorKindTag(tags, "ResourceExhausted"),
					Message:         message,
				}); err != nil {
					return err
				}
			}
		}
		if state.channelSend {
			union := state.channelSendUnions[channel.CName]
			if union != (compilerTypes.Type{}) {
				unionMembers := compilerTypes.UnionMembers(union)
				nilIndex := unionMemberIndex(union, compilerTypes.Nil)
				errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
				nilMember, _ := unionMembers.At(nilIndex)
				errorMember, _ := unionMembers.At(errorIndex)
				message := state.messageLiteral(literals, state.channelSendFailed)
				if err := renderInto(result, "concurrency.h", "chan_send_adapter", chanSendModel{
					UnionCName:      union.CName,
					Suffix:          suffix,
					ElementSpelling: elementSpelling,
					NilTag:          tags.unionMemberTag(nilMember),
					ErrorTag:        tags.unionMemberTag(errorMember),
					ErrorField:      tags.unionPayloadField(errorMember),
					KindTag:         errorKindTag(tags, "Closed"),
					Message:         message,
				}); err != nil {
					return err
				}
			}
		}
		// The receive union is emitted for every used Channel<T>: receive
		// needs the T | EoS union. close, length, capacity, is_closed, and
		// free lower directly to the core; the free adapter is emitted here
		// because it adapts the checked Heap token argument.
		receiveUnion := state.channelReceiveUnions[channel.CName]
		if receiveUnion != (compilerTypes.Type{}) {
			receiveMembers := compilerTypes.UnionMembers(receiveUnion)
			elementIndex := unionMemberIndex(receiveUnion, element)
			eosIndex := unionMemberIndex(receiveUnion, compilerTypes.EoS)
			elementMember, _ := receiveMembers.At(elementIndex)
			eosMember, _ := receiveMembers.At(eosIndex)
			if err := renderInto(result, "concurrency.h", "chan_recv_adapter", chanRecvModel{
				UnionCName:      receiveUnion.CName,
				Suffix:          suffix,
				ElementSpelling: elementSpelling,
				ElementTag:      tags.unionMemberTag(elementMember),
				ElementField:    tags.unionPayloadField(elementMember),
				EosTag:          tags.unionMemberTag(eosMember),
			}); err != nil {
				return err
			}
		}
		if err := renderInto(result, "concurrency.h", "chan_free_adapter", suffixModel{Suffix: suffix}); err != nil {
			return err
		}
	}
	return nil
}

// writeMutexInlineHelpers emits the Mutex adapters into the module header:
// the constructor/Error union adapter and the free adapter, which evaluates
// and accepts the checked Heap token even though the current runtime
// ignores it. lock and unlock lower directly to the core and need no inline
// wrapper.
func writeMutexInlineHelpers(result *strings.Builder, state *generatedConcurrencyState, literals *literalRegistry, tags *tagRegistry) error {
	if !state.mutexNew && !state.mutexLock && !state.mutexUnlock && !state.mutexFree {
		return nil
	}
	if state.mutexNew {
		union := state.mutexNewUnion
		if union != (compilerTypes.Type{}) {
			mutexMembers := compilerTypes.UnionMembers(union)
			mutexIndex := unionMemberIndex(union, compilerTypes.MutexType)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			mutexMember, _ := mutexMembers.At(mutexIndex)
			errorMember, _ := mutexMembers.At(errorIndex)
			message := state.messageLiteral(literals, state.mutexCreationFailed)
			if err := renderInto(result, "concurrency.h", "mutex_new_adapter", mutexNewModel{
				UnionCName: union.CName,
				MutexTag:   tags.unionMemberTag(mutexMember),
				MutexField: tags.unionPayloadField(mutexMember),
				ErrorTag:   tags.unionMemberTag(errorMember),
				ErrorField: tags.unionPayloadField(errorMember),
				KindTag:    errorKindTag(tags, "ResourceExhausted"),
				Message:    message,
			}); err != nil {
				return err
			}
		}
	}
	if err := renderInto(result, "concurrency.h", "mutex_free_adapter", struct{}{}); err != nil {
		return err
	}
	return nil
}

// writeAtomicHelpers emits the inline Atomic<T> wrapper: a typedef over C23
// _Atomic(T) plus one sequentially consistent operation family per used
// element. Bool excludes fetch_add and fetch_sub. The receiver methods take
// the Atomic's address; the helper never copies an Atomic value. The
// <stdatomic.h> prerequisite arrives through the hexal.h umbrella.
func writeAtomicHelpers(result *strings.Builder, state *generatedConcurrencyState) error {
	if len(state.atomics) == 0 {
		return nil
	}
	atomicNames := slices.Sorted(maps.Keys(state.atomics))
	for _, name := range atomicNames {
		atomic := state.atomics[name]
		suffix := atomicSuffix(atomic)
		element := atomic.Atomic.Element
		model := atomicHelperModel{
			ElementSpelling: typeSpelling(element),
			AtomicSpelling:  "hex_atomic_" + suffix,
		}
		if err := renderInto(result, "concurrency.h", "atomic_typedef", model); err != nil {
			return err
		}
		// The constructor returns the element value, not the _Atomic type:
		// C ignores qualifiers on function return types, so an _Atomic
		// return would warn under -Werror. value already has that type, so
		// no cast is needed; casting it to the _Atomic type here was
		// returning exactly the mismatched type this comment says to avoid,
		// which GCC tolerated but Clang and zig cc correctly rejected.
		if err := renderInto(result, "concurrency.h", "atomic_new", model); err != nil {
			return err
		}
		blocks := []string{"atomic_load", "atomic_store", "atomic_exchange"}
		if !compilerTypes.Equal(element, compilerTypes.Bool) {
			blocks = append(blocks, "atomic_fetch_add", "atomic_fetch_sub")
		}
		blocks = append(blocks, "atomic_compare_exchange")
		for _, block := range blocks {
			if err := renderInto(result, "concurrency.h", block, model); err != nil {
				return err
			}
		}
	}
	return nil
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
	if err := walkStatementExpressions(statement, func(node checker.Expression) error {
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
		checker.ReturnStatement, checker.RootReturnStatement, checker.BreakStatement, checker.ContinueStatement,
		checker.DeferStatement, checker.ErrdeferStatement, checker.FunctionDeclaration,
		checker.MethodDeclaration, checker.UnsafeStatement:
		// Block statements carry no expressions beyond their own operands,
		// and leaf statements none; nested bodies hoist at their own
		// statement list.
	default:
		return unknownExpressionDiagnostic()
	}
	return nil
}

// hoistSpawn emits one spawn prologue: the argument frame is filled with the
// evaluated arguments in source order, then the task is created through the
// scheduler. The checked call node is recorded as the hoisted handle so the
// spawn expression renders as the task handle.
func hoistSpawn(node checker.Expression, body *strings.Builder, state *expressionValidation, indent string) error {
	if node.Operand == nil || node.OperandType.Task == nil {
		return unknownExpressionDiagnostic()
	}
	call := node.Operand
	site, err := spawnSiteFor(*call, state.functions, state.moduleID, state.owner)
	if err != nil {
		return err
	}
	state.spawnCounter++
	temp := fmt.Sprintf("hex_spawn_args_%d", state.spawnCounter)
	taskTemp := fmt.Sprintf("hex_spawn_task_%d", state.spawnCounter)
	if site.hasFrame() {
		argsType := "hex_task_args_" + site.key()
		if err := renderInto(body, "module.c", "spawn_frame_decl", spawnFrameDeclModel{Indent: indent, ArgsType: argsType, Temp: temp}); err != nil {
			return err
		}
		for index, argument := range call.Arguments {
			rendered, renderErr := renderOperandWithState(argument, state)
			if renderErr != nil {
				return renderErr
			}
			if err := renderInto(body, "module.c", "spawn_frame_fill", spawnFrameFillModel{Indent: indent, Temp: temp, Index: index + 1, Value: rendered}); err != nil {
				return err
			}
		}
		resultArgs := "0, 0"
		if site.result != (compilerTypes.Type{}) && !compilerTypes.Equal(site.result, compilerTypes.Nil) {
			resultArgs = fmt.Sprintf("sizeof(%s), _Alignof(%s)", typeSpelling(site.result), typeSpelling(site.result))
		}
		if err := renderInto(body, "module.c", "spawn_call_frame", spawnCallFrameModel{
			Indent:     indent,
			TaskTemp:   taskTemp,
			Key:        site.key(),
			ArgsType:   argsType,
			Temp:       temp,
			ResultArgs: resultArgs,
		}); err != nil {
			return err
		}
	} else {
		resultArgs := "0, 0"
		if site.result != (compilerTypes.Type{}) && !compilerTypes.Equal(site.result, compilerTypes.Nil) {
			resultArgs = fmt.Sprintf("sizeof(%s), _Alignof(%s)", typeSpelling(site.result), typeSpelling(site.result))
		}
		if err := renderInto(body, "module.c", "spawn_call_bare", spawnCallBareModel{
			Indent:     indent,
			TaskTemp:   taskTemp,
			Key:        site.key(),
			ResultArgs: resultArgs,
		}); err != nil {
			return err
		}
	}
	state.hoistedSpawns[node.Operand] = taskTemp
	return nil
}

// renderSpawnExpression renders a hoisted spawn as its Task | Error union:
// the handle on success, the constructed Error on creation failure.
func renderSpawnExpression(node checker.Expression, state *expressionValidation) (string, error) {
	taskTemp, ok := state.hoistedSpawns[node.Operand]
	if !ok {
		return "", unknownExpressionDiagnostic()
	}
	union := node.ResultType
	if union == (compilerTypes.Type{}) || union.Union == nil {
		return "", unknownExpressionDiagnostic()
	}
	taskIndex := unionMemberIndex(union, node.OperandType)
	errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
	if taskIndex < 0 || errorIndex < 0 {
		return "", unknownExpressionDiagnostic()
	}
	message, err := errorMessageLiteral(state, taskCreationFailed)
	if err != nil {
		return "", err
	}
	spawnMembers := compilerTypes.UnionMembers(union)
	taskMember, _ := spawnMembers.At(taskIndex)
	errorMember, _ := spawnMembers.At(errorIndex)
	return fmt.Sprintf("(%s ? (%s){ .tag = %s, .payload.%s = %s } : (%s){ .tag = %s, .payload.%s = hex_sched_error((hex_t_ErrorKind){ .tag = %s }, %d, %d, &%s) })",
		taskTemp, union.CName, state.tags.unionMemberTag(taskMember), state.tags.unionPayloadField(taskMember), taskTemp,
		union.CName, state.tags.unionMemberTag(errorMember), state.tags.unionPayloadField(errorMember), errorKindTag(state.tags, "ResourceExhausted"), state.line(node.Span), state.column(node.Span), message), nil
}

// errorMessageLiteral resolves one failure message literal registered during
// discovery.
func errorMessageLiteral(state *expressionValidation, payload string) (string, error) {
	if state.strings == nil {
		// A registry-less state reaching concurrency rendering is a generator
		// defect; it fails closed here instead of dereferencing nil.
		return "", unknownExpressionDiagnostic()
	}
	handle, ok := state.strings.Lookup(payload)
	if !ok {
		return "", unknownExpressionDiagnostic()
	}
	return state.strings.CName(handle), nil
}

// renderTaskMethod renders one Task handle method call.
func renderTaskMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Task == nil {
		return "", unknownExpressionDiagnostic()
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "join", "detach":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeTask), node.Name, taskSuffix(node.OperandType))
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
	}
	return "", unknownExpressionDiagnostic()
}

// renderChannelConstructor renders Channel<T>.new(heap, capacity) as its
// Channel | Error union.
func renderChannelConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if node.OperandType.Channel == nil || len(node.Arguments) != 2 {
		return "", unknownExpressionDiagnostic()
	}
	heap, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	capacity, err := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
	if err != nil {
		return "", err
	}
	message, err := errorMessageLiteral(state, channelCreationFailed)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_chan_new_%s(%s, (size_t)(%s), %d, %d, &%s)",
		channelSuffix(node.OperandType), heap, capacity, state.line(node.Span), state.column(node.Span), message), nil
}

// renderChannelMethod renders one Channel handle method call.
func renderChannelMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Channel == nil {
		return "", unknownExpressionDiagnostic()
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	suffix := channelSuffix(node.OperandType)
	switch node.Name {
	case "send":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic()
		}
		value, valueErr := renderOperandWithState(node.Arguments[0], state)
		if valueErr != nil {
			return "", valueErr
		}
		message, messageErr := errorMessageLiteral(state, channelSendFailed)
		if messageErr != nil {
			return "", messageErr
		}
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeChannel), "send", suffix)
		if symbolErr != nil {
			return "", symbolErr
		}
		return fmt.Sprintf(symbol+"(%s, %s, %d, %d, &%s)", receiver, value, state.line(node.Span), state.column(node.Span), message), nil
	case "receive":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeChannel), "receive", suffix)
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
	case "close":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeChannel), "close", suffix)
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
	case "length":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeChannel), "length", suffix)
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
	case "capacity":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeChannel), "capacity", suffix)
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
	case "is_closed":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeChannel), "is_closed", suffix)
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
	case "free":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic()
		}
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeChannel), "free", suffix)
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + heap + ", " + receiver + ")", nil
	}
	return "", unknownExpressionDiagnostic()
}

// renderMutexConstructor renders Mutex.new(heap) as its Mutex | Error union.
func renderMutexConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if len(node.Arguments) != 1 {
		return "", unknownExpressionDiagnostic()
	}
	heap, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	message, err := errorMessageLiteral(state, mutexCreationFailed)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_mutex_new_mutex(%s, %d, %d, &%s)", heap, state.line(node.Span), state.column(node.Span), message), nil
}

// renderMutexMethod renders one Mutex handle method call.
func renderMutexMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || !compilerTypes.IsMutex(node.OperandType) {
		return "", unknownExpressionDiagnostic()
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "lock", "unlock":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ExactOwner(specdata.TypeMutex), node.Name, "")
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
	case "free":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic()
		}
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ExactOwner(specdata.TypeMutex), "free", "")
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + heap + ", " + receiver + ")", nil
	}
	return "", unknownExpressionDiagnostic()
}

// renderAtomicConstructor renders Atomic<T>.new(initial).
func renderAtomicConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if node.OperandType.Atomic == nil || len(node.Arguments) != 1 {
		return "", unknownExpressionDiagnostic()
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
		return "", unknownExpressionDiagnostic()
	}
	receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	helper, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeAtomic), node.Name, atomicSuffix(node.OperandType))
	if symbolErr != nil {
		return "", symbolErr
	}
	switch node.Name {
	case "load":
		return helper + "(&(" + receiver + "))", nil
	case "store", "exchange", "fetch_add", "fetch_sub":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic()
		}
		value, valueErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if valueErr != nil {
			return "", valueErr
		}
		return helper + "(&(" + receiver + "), " + value + ")", nil
	case "compare_exchange":
		if len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic()
		}
		expected, expectedErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if expectedErr != nil {
			return "", expectedErr
		}
		desired, desiredErr := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
		if desiredErr != nil {
			return "", desiredErr
		}
		return helper + "(&(" + receiver + "), " + expected + ", " + desired + ")", nil
	}
	return "", unknownExpressionDiagnostic()
}

func validateConcurrencyExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.SpawnExpression:
		if node.Operand == nil || node.OperandType.Task == nil || node.OperandType.Task.Result == (compilerTypes.Type{}) || node.ResultType.Union == nil || !compilerTypes.Equal(node.Element, node.OperandType.Task.Result) || state.line(node.Span) <= 0 {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		if unionMemberIndex(node.ResultType, node.OperandType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Type{}, state); err != nil {
			return err
		}
		return nil
	case checker.TaskYieldExpression:
		if node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic()
		}
		return nil
	case checker.TaskMethodCallExpression:
		if node.Operand == nil || node.OperandType.Task == nil {
			return unknownExpressionDiagnostic()
		}
		switch node.Name {
		case "join":
			if !compilerTypes.Equal(node.Element, node.OperandType.Task.Result) || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic()
			}
		case "detach":
			if node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic()
			}
		default:
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ChannelConstructorExpression:
		if node.OperandType.Channel == nil || len(node.Arguments) != 2 || !compilerTypes.Equal(node.Element, node.OperandType.Channel.Element) || node.ResultType.Union == nil || state.line(node.Span) <= 0 {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		if unionMemberIndex(node.ResultType, node.OperandType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[1], state)
	case checker.ChannelMethodCallExpression:
		if node.Operand == nil || node.OperandType.Channel == nil || !compilerTypes.Equal(node.Element, node.OperandType.Channel.Element) {
			return unknownExpressionDiagnostic()
		}
		switch node.Name {
		case "send":
			if len(node.Arguments) != 1 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, compilerTypes.Nil) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 || state.line(node.Span) <= 0 {
				return unknownExpressionDiagnosticAt(state, node.Span)
			}
		case "receive":
			if len(node.Arguments) != 0 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, node.Element) < 0 || unionMemberIndex(node.ResultType, compilerTypes.EoS) < 0 {
				return unknownExpressionDiagnostic()
			}
		case "close":
			if len(node.Arguments) != 0 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic()
			}
		case "length", "capacity":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
				return unknownExpressionDiagnostic()
			}
		case "is_closed":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic()
			}
		case "free":
			if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic()
			}
		default:
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
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
		if len(node.Arguments) != 1 || !compilerTypes.IsMutex(node.OperandType) || node.ResultType.Union == nil || state.line(node.Span) <= 0 {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		if unionMemberIndex(node.ResultType, compilerTypes.MutexType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnosticAt(state, node.Span)
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.MutexMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsMutex(node.OperandType) || node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic()
		}
		switch node.Name {
		case "lock", "unlock":
			if len(node.Arguments) != 0 {
				return unknownExpressionDiagnostic()
			}
		case "free":
			if len(node.Arguments) != 1 {
				return unknownExpressionDiagnostic()
			}
		default:
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
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
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.AtomicMethodCallExpression:
		if node.Operand == nil || node.OperandType.Atomic == nil || !compilerTypes.Equal(node.Element, node.OperandType.Atomic.Element) {
			return unknownExpressionDiagnostic()
		}
		switch node.Name {
		case "load":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic()
			}
		case "store":
			if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic()
			}
		case "exchange", "fetch_add", "fetch_sub":
			if len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic()
			}
		case "compare_exchange":
			if len(node.Arguments) != 2 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic()
			}
		default:
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
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
	return unknownExpressionDiagnostic()
}
