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
// needed to spell both the adapter and its raw call.
type corelibAdapter struct {
	runtime string
	result  corelib.Result
	params  []corelib.Param
	union   compilerTypes.Type
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
				return unknownExpressionDiagnostic("unknown core-library runtime entry point " + node.Name)
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
// core call for a bare Size result, otherwise a call to the module's own
// adapter carrying its source site.
func renderCorelibCallExpression(node checker.Expression, state *expressionValidation) (string, error) {
	_, function, ok := corelib.FunctionByRuntime(node.Name)
	if !ok {
		return "", unknownExpressionDiagnostic("unknown core-library runtime entry point " + node.Name)
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
		arguments = append(arguments, rendered)
	}
	arguments = append(arguments, fmt.Sprintf("%d, %d", node.SourceLine, node.SourceColumn))
	return fmt.Sprintf("%s_%s(%s)", node.Name, streamAdapterSuffix(node.ResultType), strings.Join(arguments, ", ")), nil
}

// validateCorelibCallExpression checks one core-library module call
// fail-closed against the compiler-owned declaration table.
func validateCorelibCallExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	path, function, ok := corelib.FunctionByRuntime(node.Name)
	if !ok {
		return unknownExpressionDiagnostic("unknown core-library runtime entry point " + node.Name)
	}
	_ = path
	if node.Operand != nil {
		return unknownExpressionDiagnostic("core-library module function has an unexpected checked receiver")
	}
	if len(node.Arguments) != len(function.Params) {
		return unknownExpressionDiagnostic("core-library module function has invalid checked arguments")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("core-library module function result does not match its expected type")
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
		return "", unknownExpressionDiagnostic("core-library result union has no Error member")
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

// corelibAdapterParameters spells the rendered adapter parameter list.
func corelibAdapterParameters(params []corelib.Param) (string, error) {
	rendered := make([]string, 0, len(params))
	for _, param := range params {
		switch param {
		case corelib.ParamHeap:
			rendered = append(rendered, "hex_heap heap")
		case corelib.ParamMutByteSlice:
			rendered = append(rendered, "hex_mut_slice_UInt8 into")
		default:
			return "", unknownExpressionDiagnostic("unknown core-library parameter shape")
		}
	}
	return strings.Join(rendered, ", "), nil
}

// corelibAdapterArguments spells the raw call's argument expressions.
func corelibAdapterArguments(params []corelib.Param) (string, error) {
	rendered := make([]string, 0, len(params))
	for _, param := range params {
		switch param {
		case corelib.ParamHeap:
			rendered = append(rendered, "heap")
		case corelib.ParamMutByteSlice:
			rendered = append(rendered, "into.data, into.length")
		default:
			return "", unknownExpressionDiagnostic("unknown core-library parameter shape")
		}
	}
	return strings.Join(rendered, ", "), nil
}

// writeCorelibInlineHelpers emits the module-owned core-library adapters: each
// wraps a raw component result struct in this module's own union, building
// failures from the module file literal and the runtime's stable ErrorKind.
func writeCorelibInlineHelpers(result *strings.Builder, state *generatedCorelibState, literals *literalRegistry, tags *tagRegistry, event bool) error {
	if state == nil || !state.used {
		return nil
	}
	file := "&" + literals.CName(state.fileLiteral)
	for _, adapter := range state.adapters {
		member, ok := corelibMemberType(adapter.union)
		if !ok {
			return unknownExpressionDiagnostic("core-library result union has no success member")
		}
		tag, field := streamMemberRef(tags, adapter.union, member)
		if tag == "" || field == "" {
			return unknownExpressionDiagnostic("core-library result union is missing its success member")
		}
		failure, err := corelibErrorArm(tags, file, adapter.union, "query.kind", "query.message")
		if err != nil {
			return err
		}
		parameters, err := corelibAdapterParameters(adapter.params)
		if err != nil {
			return err
		}
		callArguments, err := corelibAdapterArguments(adapter.params)
		if err != nil {
			return err
		}
		if parameters != "" {
			parameters += ", "
		}
		call := corelibAdapterCall(adapter.runtime, event)
		switch adapter.result {
		case corelib.ResultString:
			fmt.Fprintf(result,
				"\nstatic inline %s %s_%s(%ssize_t line, size_t column) {\n"+
					"    hex_program_string_result query = %s(%s);\n"+
					"    if (query.ok) {\n"+
					"        return (%s){ .tag = %s, .payload.%s = query.value };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				adapter.union.CName, adapter.runtime, streamAdapterSuffix(adapter.union),
				parameters, call, callArguments,
				adapter.union.CName, tag, field, failure)
		case corelib.ResultStringSlice:
			fmt.Fprintf(result,
				"\nstatic inline %s %s_%s(%ssize_t line, size_t column) {\n"+
					"    hex_program_arguments_result query = %s(%s);\n"+
					"    if (query.ok) {\n"+
					"        return (%s){ .tag = %s, .payload.%s = (%s){ .data = query.items, .length = query.count } };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				adapter.union.CName, adapter.runtime, streamAdapterSuffix(adapter.union),
				parameters, call, callArguments,
				adapter.union.CName, tag, field, member.CName, failure)
		case corelib.ResultNil:
			nilTag, _ := streamMemberRef(tags, adapter.union, compilerTypes.Nil)
			fmt.Fprintf(result,
				"\nstatic inline %s %s_%s(%ssize_t line, size_t column) {\n"+
					"    hex_entropy_fill_result query = %s(%s);\n"+
					"    if (query.ok) {\n"+
					"        return (%s){ .tag = %s };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				adapter.union.CName, adapter.runtime, streamAdapterSuffix(adapter.union),
				parameters, call, callArguments,
				adapter.union.CName, nilTag, failure)
		default:
			return unknownExpressionDiagnostic("core-library adapter has an unsupported result shape")
		}
	}
	return nil
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
