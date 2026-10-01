package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	"hexal/compiler/corelib"
	compilerTypes "hexal/compiler/types"
)

// Core-library module calls: Alias.name(args) where Alias is bound to a
// "std/..." core-library module (std/program, std/entropy). The checker
// resolves them to CorelibCallExpression carrying the emitted runtime entry
// point in Name. The raw entry points live in the compiler-owned component
// pair (hexal/program.h/.c, hexal/entropy.h/.c) and return fixed raw result
// structs; each module that uses one gets its own static inline adapter that
// wraps the raw result in that module's own structural union, because a union
// type is module-owned and cannot be declared in a shared component.

const (
	corelibRuntimeArguments            = "hex_program_arguments"
	corelibRuntimeCurrentDirectory     = "hex_program_current_directory"
	corelibRuntimeHomeDirectory        = "hex_program_home_directory"
	corelibRuntimeTemporaryDirectory   = "hex_program_temporary_directory"
	corelibRuntimeExecutablePath       = "hex_program_executable_path"
	corelibRuntimeAvailableParallelism = "hex_program_available_parallelism"
	corelibRuntimeEntropyFill          = "hex_entropy_fill"
)

// corelibAdapter is one reachable core-library function in one module: the
// module-owned result union its adapter produces, plus the declaration facts
// needed to spell both the adapter and its raw call. query names the raw
// result record the component pair owns; the shape's template block reads it.
type corelibAdapter struct {
	runtime string
	result  corelib.Result
	params  []corelib.Param
	union   compilerTypes.Type
	query   string
	// app is the application type of a std/http route registration, whose
	// invoke thunk restores the handler's own C signature; zero elsewhere.
	app compilerTypes.Type
}

// addonAdapterSuffix names one adapter by its result union, and by the
// application too for a route registration: two applications share the
// Nil | Error union but not the handler signature the thunk casts back to.
func addonAdapterSuffix(runtime string, union, app compilerTypes.Type) string {
	suffix := streamAdapterSuffix(union)
	if runtime == "hex_http_router_route" {
		suffix += "_" + strings.TrimPrefix(app.CName, "hex_t_")
	}
	return suffix
}

// routeApplication is the application type a route call's receiver Router
// carries, or the zero Type for every other call.
func routeApplication(node checker.Expression) compilerTypes.Type {
	if node.Name != "hex_http_router_route" || len(node.Arguments) == 0 {
		return compilerTypes.Type{}
	}
	app, _ := compilerTypes.HttpGenericApp(node.Arguments[0].Type)
	return app
}

// generatedCorelibState records one module's (or the merged program's)
// core-library reachability. paths covers every std/program path query and
// available_parallelism; blocking is the subset that must route through the
// event bridge inside a Task (the path queries and entropy fill; the
// stateless parallelism query never parks). adapters is module-local.
type generatedCorelibState struct {
	used       bool
	program    bool
	entropy    bool
	paths      bool
	blocking   bool
	arguments  bool
	executable bool
	adapters   []corelibAdapter
	// fileLiteral is the module source key's Error file literal, demanded only
	// when a failing (unioned) result is reachable.
	fileLiteral literalHandle
}

// corelibPathsRuntime reports whether runtime is one of std/program's path or
// parallelism entry points, the whole of the program component's native
// surface.
func corelibPathsRuntime(runtime string) bool {
	switch runtime {
	case corelibRuntimeCurrentDirectory, corelibRuntimeHomeDirectory,
		corelibRuntimeTemporaryDirectory, corelibRuntimeExecutablePath,
		corelibRuntimeAvailableParallelism:
		return true
	}
	return false
}

// discoverGeneratedCorelib walks one module for core-library module calls.
func discoverGeneratedCorelib(program checker.Program, logicalKey string, literals *literalRegistry) *generatedCorelibState {
	state := &generatedCorelibState{}
	seen := make(map[string]bool)
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.CorelibCallExpression {
				return nil
			}
			path, function, ok := corelib.FunctionByRuntime(node.Name)
			if !ok {
				return unknownExpressionDiagnostic()
			}
			// std/json and std/regex calls own their component demand
			// through the dedicated json/regex discovery states and their
			// raw adapters; the program/entropy adapters never spell them.
			switch path {
			case "std/json", "std/regex", "std/http":
				return nil
			}
			state.used = true
			if path == "std/entropy" {
				state.entropy = true
			} else {
				state.program = true
			}
			if corelibPathsRuntime(node.Name) {
				state.paths = true
			}
			switch node.Name {
			case corelibRuntimeArguments:
				state.arguments = true
			case corelibRuntimeExecutablePath:
				state.executable = true
				state.blocking = true
			case corelibRuntimeCurrentDirectory, corelibRuntimeHomeDirectory,
				corelibRuntimeTemporaryDirectory, corelibRuntimeEntropyFill:
				state.blocking = true
			}
			// Only a unioned result needs an adapter; available_parallelism
			// returns a bare Size and renders as one direct core call.
			if function.Result != corelib.ResultSize {
				key := node.Name + "|" + node.ResultType.CName
				if !seen[key] {
					seen[key] = true
					state.adapters = append(state.adapters, corelibAdapter{
						runtime: node.Name,
						result:  function.Result,
						params:  function.Params,
						union:   node.ResultType,
						query:   corelibRawResultName(function.Result),
					})
				}
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	if state.used {
		state.fileLiteral = literals.Intern(logicalKey)
	}
	return state
}

// renderCorelibCallExpression renders one core-library module call: a direct
// core call for a bare Size result, a raw cleanup call for a no-value result,
// a raw call for a std/http value the call builds without a failure path,
// otherwise a call to the module's own adapter carrying its source site.
func renderCorelibCallExpression(node checker.Expression, state *expressionValidation) (string, error) {
	path, function, ok := corelib.FunctionByRuntime(node.Name)
	if !ok {
		return "", unknownExpressionDiagnostic()
	}
	if function.Result == corelib.ResultSize {
		return node.Name + "()", nil
	}
	arguments := make([]string, 0, len(node.Arguments)+1)
	for index := range node.Arguments {
		rendered, err := renderHoistedOperand(&node.Arguments[index].Node, node.Arguments[index], state)
		if err != nil {
			return "", err
		}
		if index < len(function.Params) && function.Params[index] == corelib.ParamHandler {
			// A handler's C function type is the application's own; the
			// runtime stores it behind one erased function pointer and the
			// application's invoke thunk restores it.
			rendered = "(hex_http_handler)(" + rendered + ")"
		}
		arguments = append(arguments, rendered)
	}
	if function.Result == corelib.ResultNoValue || corelibDirectResult(path, function.Result) {
		// value.free, pattern.free, match.free, and the std/http cleanup and
		// builder entries produce a value with no failure arm: the raw entry
		// point renders directly with exactly the checked arguments. The
		// addon families suffix every raw entry point with _raw so the
		// registry name stays free for the adapter form.
		return corelibRawName(path, node.Name) + "(" + strings.Join(arguments, ", ") + ")", nil
	}
	arguments = append(arguments, fmt.Sprintf("%d, %d", state.line(node.Span), state.column(node.Span)))
	return fmt.Sprintf("%s_%s(%s)", node.Name, addonAdapterSuffix(node.Name, node.ResultType, routeApplication(node)), strings.Join(arguments, ", ")), nil
}

// corelibRawName spells a std module's raw entry point: the addon families
// suffix every one with _raw so the registry name stays free for the adapter
// form.
func corelibRawName(path, runtime string) string {
	switch path {
	case "std/json", "std/regex", "std/http":
		return runtime + "_raw"
	}
	return runtime
}

// corelibDirectResult reports whether a std/http result shape is produced
// directly: the router, the config record, and the borrowed byte and header
// views, none of which can fail.
func corelibDirectResult(path string, result corelib.Result) bool {
	if path != "std/http" {
		return false
	}
	switch result {
	case corelib.ResultRouter, corelib.ResultConfig, corelib.ResultBytes, corelib.ResultHeaders:
		return true
	}
	return false
}

// validateCorelibCallExpression checks one core-library module call
// fail-closed against the compiler-owned declaration table.
func validateCorelibCallExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	path, function, ok := corelib.FunctionByRuntime(node.Name)
	if !ok {
		return unknownExpressionDiagnostic()
	}
	_ = path
	if node.Operand != nil {
		return unknownExpressionDiagnostic()
	}
	if len(node.Arguments) != len(function.Params) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

// corelibErrorArm spells the Error payload construction of one core-library
// adapter: the runtime already produced the stable ErrorKind and the fixed
// message, so the adapter adds only this module's source site.
func corelibErrorArm(tags *tagRegistry, file string, union compilerTypes.Type, kind, message string) (string, error) {
	tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
	if tag == "" || field == "" {
		return "", unknownExpressionDiagnostic()
	}
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = (hex_t_Error){ .hex_m_file = %s, .hex_m_line = line, .hex_m_column = column, .hex_m_kind = %s, .hex_m_message = hex_error_message(hex_text_heap(%s)) } }",
		union.CName, tag, field, file, kind, message), nil
}

// corelibMemberType returns a failure union's one non-Error member.
func corelibMemberType(union compilerTypes.Type) (compilerTypes.Type, bool) {
	members := compilerTypes.UnionMembers(union)
	for index := 0; index < members.Len(); index++ {
		member, _ := members.At(index)
		if !compilerTypes.IsError(member) {
			return member, true
		}
	}
	return compilerTypes.Type{}, false
}

// corelibAdapterCall spells the raw component entry point for one adapter,
// selecting the Task-parking form program-wide when the event bridge is
// selected.
func corelibAdapterCall(runtime string, event bool) string {
	if event {
		return runtime + "_task"
	}
	return runtime
}

// corelibParamSpelling is one adapter parameter's C declaration and the name
// its raw call argument uses. The name is unique within the list: a second
// parameter of one shape takes a numeric suffix, so a two-String signature
// declares distinct names while a single-parameter signature keeps the plain
// name every other family already generates.
type corelibParamSpelling struct {
	declaration string
	name        string
	argument    string
}

// corelibParamSpellings spells every parameter of one adapter signature.
func corelibParamSpellings(params []corelib.Param) ([]corelibParamSpelling, error) {
	spellings := make([]corelibParamSpelling, 0, len(params))
	seen := make(map[string]int)
	for _, param := range params {
		var spelling corelibParamSpelling
		var declaration, base string
		argument := ""
		switch param {
		case corelib.ParamHeap:
			declaration, base = "hex_heap %s", "heap"
		case corelib.ParamMutByteSlice:
			// The raw entry takes the slice's data and length separately.
			declaration, base, argument = "hex_mut_slice_UInt8 %s", "into", "%s.data, %[1]s.length"
		case corelib.ParamString:
			// A String value is a pointer in generated C; the raw entries
			// and every wrapper argument agree on that spelling.
			declaration, base = "const hex_string *%s", "text"
		case corelib.ParamValue:
			declaration, base = "hex_t_JsonValue %s", "value"
		case corelib.ParamPattern:
			declaration, base = "hex_regex_pattern %s", "pattern"
		case corelib.ParamSpan:
			declaration, base = "hex_t_Span %s", "span"
		case corelib.ParamMatch:
			declaration, base = "hex_t_Match %s", "match"
		case corelib.ParamUInt16:
			declaration, base = "uint16_t %s", "port"
		case corelib.ParamSize:
			declaration, base = "size_t %s", "count"
		case corelib.ParamByteList:
			declaration, base = "hex_list_UInt8 *%s", "into"
		case corelib.ParamBytes:
			declaration, base = "hex_slice_UInt8 %s", "bytes"
		case corelib.ParamConfig:
			declaration, base = "hex_t_ServerConfig %s", "config"
		case corelib.ParamRouter:
			declaration, base = "hex_http_router %s", "router"
		case corelib.ParamServer:
			declaration, base = "hex_http_server %s", "server"
		case corelib.ParamRequest:
			declaration, base = "hex_http_request %s", "request"
		case corelib.ParamWriter:
			declaration, base = "hex_http_writer %s", "writer"
		case corelib.ParamAppPtr:
			declaration, base = "const void *%s", "app"
		case corelib.ParamHandler:
			declaration, base = "hex_http_handler %s", "handler"
		default:
			return nil, unknownExpressionDiagnostic()
		}
		seen[base]++
		name := base
		if seen[base] > 1 {
			name = fmt.Sprintf("%s%d", base, seen[base])
		}
		spelling.name = name
		spelling.declaration = fmt.Sprintf(declaration, name)
		if argument == "" {
			spelling.argument = name
		} else {
			spelling.argument = fmt.Sprintf(argument, name)
		}
		spellings = append(spellings, spelling)
	}
	return spellings, nil
}

// corelibAdapterParameters spells the rendered adapter parameter list.
func corelibAdapterParameters(params []corelib.Param) (string, error) {
	spellings, err := corelibParamSpellings(params)
	if err != nil {
		return "", err
	}
	rendered := make([]string, 0, len(spellings))
	for _, spelling := range spellings {
		rendered = append(rendered, spelling.declaration)
	}
	return strings.Join(rendered, ", "), nil
}

// corelibAdapterArguments spells the raw call's argument expressions.
func corelibAdapterArguments(params []corelib.Param) (string, error) {
	spellings, err := corelibParamSpellings(params)
	if err != nil {
		return "", err
	}
	rendered := make([]string, 0, len(spellings))
	for _, spelling := range spellings {
		rendered = append(rendered, spelling.argument)
	}
	return strings.Join(rendered, ", "), nil
}

// writeCorelibInlineHelpers emits the module-owned core-library adapters: each
// wraps a raw component result struct in this module's own union, building
// failures from the module file literal and the runtime's stable ErrorKind.
// corelibAdapterModel carries one core-library adapter's decided union
// type, runtime name, name suffix, parameter list, call and argument
// expressions, success arm tag, payload field and member type, and
// failure arm text; each result-shape template reads the fields it spells.
type corelibAdapterModel struct {
	CName      string
	Runtime    string
	Suffix     string
	Parameters string
	Call       string
	Arguments  string
	Query      string
	Tag        string
	Field      string
	NilTag     string
	EosTag     string
	Member     string
	Failure    string
	// AppPointer spells the handler's context parameter in a route adapter.
	AppPointer string
}

func writeCorelibInlineHelpers(result *strings.Builder, state *generatedCorelibState, literals *literalRegistry, tags *tagRegistry, event bool) error {
	if state == nil || !state.used {
		return nil
	}
	file := "&" + literals.CName(state.fileLiteral)
	return writeAdapterHelpers(result, state.adapters, file, tags, func(adapter corelibAdapter) string {
		return corelibAdapterCall(adapter.runtime, event)
	})
}

// writeAdapterHelpers renders one family's reachable adapters: each wraps a
// raw component result struct in this module's own union, building failures
// from the module file literal and the runtime's stable ErrorKind. The
// shared corelibAdapterModel carries every shape's decided fields and each
// result-shape template block spells the arm its result takes. call spells
// the raw entry point one family exposes, which differs between the
// program/entropy pair and the std/json and std/regex pairs.
func writeAdapterHelpers(result *strings.Builder, adapters []corelibAdapter, file string, tags *tagRegistry, call func(corelibAdapter) string) error {
	for _, adapter := range adapters {
		if adapter.result == corelib.ResultBytesNil {
			if err := writeBytesNilAdapter(result, adapter, tags, call); err != nil {
				return err
			}
			continue
		}
		member, ok := corelibMemberType(adapter.union)
		if !ok {
			return unknownExpressionDiagnostic()
		}
		tag, field := streamMemberRef(tags, adapter.union, member)
		if tag == "" || field == "" {
			return unknownExpressionDiagnostic()
		}
		if adapter.query == "" {
			return unknownExpressionDiagnostic()
		}
		failure, err := corelibErrorArm(tags, file, adapter.union, "query.kind", "query.message")
		if err != nil {
			return err
		}
		parameters, err := corelibAdapterParameters(adapter.params)
		if err != nil {
			return err
		}
		if parameters != "" {
			parameters += ", "
		}
		callArguments, err := corelibAdapterArguments(adapter.params)
		if err != nil {
			return err
		}
		model := corelibAdapterModel{
			CName:      adapter.union.CName,
			Runtime:    adapter.runtime,
			Suffix:     addonAdapterSuffix(adapter.runtime, adapter.union, adapter.app),
			Parameters: parameters,
			Call:       call(adapter),
			Arguments:  callArguments,
			Query:      adapter.query,
			Tag:        tag,
			Field:      field,
			Failure:    failure,
		}
		switch adapter.result {
		case corelib.ResultString, corelib.ResultValue, corelib.ResultPattern, corelib.ResultBool:
			if err := renderInto(result, "module.h", adapterBlock(adapter.result), model); err != nil {
				return err
			}
		case corelib.ResultStringSlice:
			model.Member = member.CName
			if err := renderInto(result, "module.h", adapterBlock(adapter.result), model); err != nil {
				return err
			}
		case corelib.ResultNil:
			nilTag, _ := streamMemberRef(tags, adapter.union, compilerTypes.Nil)
			model.Tag = nilTag
			block := adapterBlock(adapter.result)
			if adapter.runtime == "hex_http_router_route" {
				// A route registration also builds the thunk that restores the
				// handler's own C signature, which only this module can spell.
				block = "corelib_route_adapter"
				model.AppPointer = typeSpelling(compilerTypes.NewEnvironment().PtrType(adapter.app))
				model.Arguments += ", " + adapter.runtime + "_invoke_" + model.Suffix
			}
			if err := renderInto(result, "module.h", block, model); err != nil {
				return err
			}
		case corelib.ResultServer:
			if err := renderInto(result, "module.h", adapterBlock(adapter.result), model); err != nil {
				return err
			}
		case corelib.ResultReadBody:
			// The success member is the Size; corelibMemberType names the first
			// non-Error member, which in this union is EoS.
			model.Tag, model.Field = streamMemberRef(tags, adapter.union, compilerTypes.SizeType)
			eosTag, _ := streamMemberRef(tags, adapter.union, compilerTypes.EoS)
			model.EosTag = eosTag
			if err := renderInto(result, "module.h", adapterBlock(adapter.result), model); err != nil {
				return err
			}
		case corelib.ResultSpanNil, corelib.ResultMatchNil:
			nilTag, _ := streamMemberRef(tags, adapter.union, compilerTypes.Nil)
			model.NilTag = nilTag
			if err := renderInto(result, "module.h", adapterBlock(adapter.result), model); err != nil {
				return err
			}
		default:
			return unknownExpressionDiagnostic()
		}
	}
	return nil
}

// writeBytesNilAdapter renders the one std/http adapter whose union has no
// Error arm: a header lookup builds a Slice<Byte> | Nil from the raw found
// flag. It takes the source site its siblings take so every adapter call
// renders alike.
func writeBytesNilAdapter(result *strings.Builder, adapter corelibAdapter, tags *tagRegistry, call func(corelibAdapter) string) error {
	tag, field := streamMemberRef(tags, adapter.union, compilerTypes.HttpByteSliceType())
	nilTag, _ := streamMemberRef(tags, adapter.union, compilerTypes.Nil)
	parameters, err := corelibAdapterParameters(adapter.params)
	if err != nil {
		return err
	}
	if parameters != "" {
		parameters += ", "
	}
	arguments, err := corelibAdapterArguments(adapter.params)
	if err != nil {
		return err
	}
	model := corelibAdapterModel{
		CName:      adapter.union.CName,
		Runtime:    adapter.runtime,
		Suffix:     streamAdapterSuffix(adapter.union),
		Parameters: parameters,
		Call:       call(adapter),
		Arguments:  arguments,
		Query:      adapter.query,
		Tag:        tag,
		Field:      field,
		NilTag:     nilTag,
	}
	return renderInto(result, "module.h", "corelib_bytes_nil_adapter", model)
}

// adapterBlock selects the module.h template block name one raw result shape
// renders through; the shared corelibAdapterModel carries each shape's
// decided fields and the template spells the arm the shape's value takes.
func adapterBlock(result corelib.Result) string {
	switch result {
	case corelib.ResultString:
		return "corelib_string_adapter"
	case corelib.ResultStringSlice:
		return "corelib_slice_adapter"
	case corelib.ResultNil:
		return "corelib_nil_adapter"
	case corelib.ResultValue:
		return "corelib_value_adapter"
	case corelib.ResultPattern:
		return "corelib_pattern_adapter"
	case corelib.ResultBool:
		return "corelib_bool_adapter"
	case corelib.ResultSpanNil:
		return "corelib_span_nil_adapter"
	case corelib.ResultMatchNil:
		return "corelib_match_nil_adapter"
	case corelib.ResultServer:
		return "corelib_server_adapter"
	case corelib.ResultReadBody:
		return "corelib_read_adapter"
	default:
		return ""
	}
}

// mergeCorelibInto unions one module's core-library demand into the program
// state.
func mergeCorelibInto(merged, state *generatedCorelibState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.program = merged.program || state.program
	merged.entropy = merged.entropy || state.entropy
	merged.paths = merged.paths || state.paths
	merged.blocking = merged.blocking || state.blocking
	merged.arguments = merged.arguments || state.arguments
	merged.executable = merged.executable || state.executable
}

// corelibComponents returns the std/program and std/entropy component pairs
// when their operations are reachable. The Filesystem gate keeps arguments-
// only programs from compiling the libuv path functions they never call.
func corelibComponents(merged *programEmission, config Config) ([]componentArtifact, error) {
	state := merged.corelibState
	if state == nil || !state.used {
		return nil, nil
	}
	event := eventSelected(merged)
	var artifacts []componentArtifact
	if state.program {
		model := programSourceModel{Paths: state.paths, Arguments: state.arguments, Event: event}
		artifacts = append(artifacts,
			componentArtifact{key: "hexal/program.h", template: "program.h", model: model},
			componentArtifact{key: "hexal/program.c", template: "program.c", model: model})
	}
	if state.entropy {
		model := entropySourceModel{Event: event}
		artifacts = append(artifacts,
			componentArtifact{key: "hexal/entropy.h", template: "entropy.h", model: model},
			componentArtifact{key: "hexal/entropy.c", template: "entropy.c", model: model})
	}
	return artifacts, nil
}

// programSourceModel gates program.c's path/parallelism surface, its argument
// snapshot surface, and the Task-parking forms.
type programSourceModel struct {
	Paths     bool
	Arguments bool
	Event     bool
}

// entropySourceModel gates entropy.c's Task-parking form.
type entropySourceModel struct {
	Event bool
}

// moduleCorelibComponent selects the core-library component headers one
// module's adapters name.
func moduleCorelibComponent(emission *moduleEmission) []string {
	state := emission.corelibState
	if state == nil || !state.used {
		return nil
	}
	var components []string
	if state.program {
		components = append(components, "hexal/program.h")
	}
	if state.entropy {
		components = append(components, "hexal/entropy.h")
	}
	return components
}
