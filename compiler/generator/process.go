package generator

// The libuv process/IPC family's Go side: Process.start and the Process/Pipe
// instance methods. Every operation lowers through one module-owned inline
// adapter that translates the checked ADT/union tags ProcessOptions,
// Environment, ProcessStream, and ExitStatus carry into the plain values
// hexal/process.c's runtime understands, and back, following the same
// structural-union wrapping pattern network.go uses for TCP and DNS.

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

const (
	processMessageStart     = "process start failed"
	processMessageWait      = "process wait failed"
	processMessageTerminate = "process terminate failed"
	processMessageClose     = "process close failed"
	pipeMessageRead         = "pipe read failed"
	pipeMessageWrite        = "pipe write failed"
	pipeMessageShutdown     = "pipe shutdown failed"
	pipeMessageClose        = "pipe close failed"
)

// generatedProcessState records one module's (or the merged program's)
// process/IPC reachability. used gates the type-definition component
// (ProcessOptions, Environment, ProcessStream, ExitStatus, EnvironmentVariable,
// StartedProcess, Process, and Pipe as bare types); operations additionally
// gates the native runtime -- the handle registry, event bridge, scheduler,
// libuv, and native bootstrap -- matching the "constructing or
// inspecting an inline option value selects only the type-definition
// component" rule. hexal/process.h's own hex_handle embedding is a disclosed
// simplification: it always follows used, not operations, since hex_handle
// itself carries no scheduler or libuv dependency of its own.
type generatedProcessState struct {
	used       bool
	operations bool

	startUnions     []compilerTypes.Type
	waitUnions      []compilerTypes.Type
	terminateUnions []compilerTypes.Type
	closeUnions     []compilerTypes.Type
	readUnions      []compilerTypes.Type

	nameLiteral literalHandle
}

// discoverGeneratedProcess walks one module for process/IPC types and
// operations.
func discoverGeneratedProcess(program checker.Program, logicalKey string, literals *literalRegistry) *generatedProcessState {
	state := &generatedProcessState{}
	typeReached := func(typ compilerTypes.Type) {
		if compilerTypes.IsProcessOptions(typ) || compilerTypes.IsEnvironment(typ) || compilerTypes.IsEnvironmentVariable(typ) ||
			compilerTypes.IsProcessStream(typ) || compilerTypes.IsExitStatus(typ) || compilerTypes.IsStartedProcess(typ) ||
			compilerTypes.IsProcess(typ) || compilerTypes.IsPipe(typ) {
			state.used = true
		}
	}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			typeReached(typ)
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.NetworkExpression {
				return nil
			}
			switch node.Name {
			case "process_start":
				state.used, state.operations = true, true
				state.startUnions = appendUnionOnce(state.startUnions, node.ResultType)
			case "process_wait":
				state.used, state.operations = true, true
				state.waitUnions = appendUnionOnce(state.waitUnions, node.ResultType)
			case "process_terminate":
				state.used, state.operations = true, true
				state.terminateUnions = appendUnionOnce(state.terminateUnions, node.ResultType)
			case "process_close", "pipe_shutdown", "pipe_close":
				state.used, state.operations = true, true
				state.closeUnions = appendUnionOnce(state.closeUnions, node.ResultType)
			case "pipe_read":
				state.used, state.operations = true, true
				state.readUnions = appendUnionOnce(state.readUnions, node.ResultType)
			case "pipe_write":
				state.used, state.operations = true, true
				state.closeUnions = appendUnionOnce(state.closeUnions, node.ResultType)
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	if state.used {
		state.nameLiteral = literals.Intern(logicalKey)
	}
	if state.operations {
		for _, message := range []string{
			processMessageStart, processMessageWait, processMessageTerminate, processMessageClose,
			pipeMessageRead, pipeMessageWrite, pipeMessageShutdown, pipeMessageClose,
		} {
			literals.Intern(message)
		}
	}
	return state
}

// elementNeedsProcess reports whether a collection specialization's element
// spells a process/IPC type, whose typedef hexal/process.h owns; a component
// header that includes this element type directly cannot rely on a
// consuming module's own include order to have supplied it first.
func elementNeedsProcess(element compilerTypes.Type) bool {
	return compilerTypes.IsProcess(element) || compilerTypes.IsPipe(element) ||
		compilerTypes.IsProcessOptions(element) || compilerTypes.IsEnvironmentVariable(element) ||
		compilerTypes.IsStartedProcess(element) || compilerTypes.IsEnvironment(element) ||
		compilerTypes.IsProcessStream(element) || compilerTypes.IsExitStatus(element)
}

// mergeProcessInto unions one module's process/IPC demand into the program
// state.
func mergeProcessInto(merged, state *generatedProcessState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.operations = merged.operations || state.operations
}

// processComponents returns hexal/process.h and hexal/process.c when any
// process/IPC type or operation is reachable. hexal/process.c has no content
// at all when no operation is reachable (every process/IPC value type is a
// plain struct with no associated runtime function), so it renders empty and
// renderComponentArtifacts omits it.
func processComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged.processState == nil || !merged.processState.used {
		return nil, nil
	}
	model := processSourceModel{Operations: merged.processState.operations}
	return []componentArtifact{
		{key: "hexal/process.h", template: "process.h", model: model},
		{key: "hexal/process.c", template: "process.c", model: model},
	}, nil
}

// processSourceModel gates hexal/process.h and hexal/process.c's runtime
// function declarations and definitions.
type processSourceModel struct {
	Operations bool
}

// moduleProcessComponent selects hexal/process.h for a module naming any
// process/IPC type or operation.
func moduleProcessComponent(emission *moduleEmission) []string {
	if emission == nil || emission.processState == nil || !emission.processState.used {
		return nil
	}
	return []string{"hexal/process.h"}
}

// processEnvironmentReplaceTag and processEnvironmentInheritTag name
// Environment's two declaration-order variants; processStream* name
// ProcessStream's three.
func processAdtTag(tags *tagRegistry, adt *compilerTypes.AdtType, index int) string {
	return tags.adtVariantTag(adt, index)
}

// processStreamTranslation spells the ternary chain that reads one checked
// hex_t_ProcessStream field and produces the plain HEX_PROCESS_STREAM_*
// value hexal/process.c's raw options struct carries.
func processStreamTranslation(tags *tagRegistry, expr string) string {
	adt := compilerTypes.ProcessStreamType.Adt
	ignore := processAdtTag(tags, adt, 0)
	inherit := processAdtTag(tags, adt, 1)
	return fmt.Sprintf("%s.tag == %s ? HEX_PROCESS_STREAM_IGNORE : %s.tag == %s ? HEX_PROCESS_STREAM_INHERIT : HEX_PROCESS_STREAM_PIPE", expr, ignore, expr, inherit)
}

// processErrorArm spells one Error payload construction for a process/Pipe
// adapter whose failure carries a hex_process_error-classified status. pipe
// selects the "pipe error" fallback header over "process error".
func processErrorArm(tags *tagRegistry, literals *literalRegistry, union compilerTypes.Type, status, payload string, pipe bool) (string, error) {
	handle, ok := literals.Lookup(payload)
	if !ok {
		return "", unknownExpressionDiagnostic("process failure message is missing from the literal registry: " + payload)
	}
	tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = hex_process_error(line, column, %s, %t, &%s) }",
		union.CName, tag, field, status, pipe, literals.CName(handle)), nil
}

// writeProcessInlineHelpers emits the module-owned process/IPC adapters:
// each wraps the process.c core in one structural result union, built with
// this module's static operation message.
func writeProcessInlineHelpers(result *strings.Builder, state *generatedProcessState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || !state.operations {
		return nil
	}

	nilUnionField := func(union compilerTypes.Type) (string, string) {
		return streamMemberRef(tags, union, compilerTypes.Nil)
	}

	// stringNilTag, pipeNilTag, nilPayloadTag, and replaceTag are resolved
	// only inside the startUnions loop (never unconditionally): they name
	// discriminants of Environment, ProcessStream, String | Nil, and
	// Pipe | Nil, which are registered in the program-wide tag registry
	// only when a process_start call actually reached them by constructing
	// a ProcessOptions value. A program using process_close or
	// process_terminate alone, with no process_start in sight, never
	// registers them, and resolving them anyway is a hard registry-miss
	// error, not a harmless no-op.
	for _, union := range state.startUnions {
		stringNilTag := tags.unionMemberTag(compilerTypes.StringType)
		stringNilField := tags.unionPayloadField(compilerTypes.StringType)
		pipeNilTag := tags.unionMemberTag(compilerTypes.PipeType)
		pipeNilField := tags.unionPayloadField(compilerTypes.PipeType)
		nilPayloadTag := tags.unionMemberTag(compilerTypes.Nil)
		replaceTag := processAdtTag(tags, compilerTypes.EnvironmentType.Adt, 1)
		startedTag, startedField := streamMemberRef(tags, union, compilerTypes.StartedProcessType)
		failure, err := processErrorArm(tags, literals, union, "started.status", processMessageStart, false)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_process_start_%s(hex_t_ProcessOptions options, size_t line, size_t column) {\n"+
				"    hex_process_options raw;\n"+
				"    raw.program = options.hex_m_program;\n"+
				"    raw.arguments = options.hex_m_arguments;\n"+
				"    raw.environment_replace = options.hex_m_environment.tag == %s;\n"+
				"    raw.environment_values = raw.environment_replace ? options.hex_m_environment.payload.Replace.hex_m_values : nullptr;\n"+
				"    raw.working_directory = options.hex_m_working_directory.tag == %s ? options.hex_m_working_directory.payload.%s : nullptr;\n"+
				"    raw.input = %s;\n"+
				"    raw.output = %s;\n"+
				"    raw.error = %s;\n"+
				"    hex_process_start_result started = hex_process_start(raw);\n"+
				"    if (started.status == 0) {\n"+
				"        hex_t_StartedProcess value = {\n"+
				"            .hex_m_process = started.process,\n"+
				"            .hex_m_input = started.input.present ? (hex_t_Pipe_Nil){ .tag = %s, .payload.%s = started.input.pipe } : (hex_t_Pipe_Nil){ .tag = %s },\n"+
				"            .hex_m_output = started.output.present ? (hex_t_Pipe_Nil){ .tag = %s, .payload.%s = started.output.pipe } : (hex_t_Pipe_Nil){ .tag = %s },\n"+
				"            .hex_m_error = started.error.present ? (hex_t_Pipe_Nil){ .tag = %s, .payload.%s = started.error.pipe } : (hex_t_Pipe_Nil){ .tag = %s },\n"+
				"        };\n"+
				"        return (%s){ .tag = %s, .payload.%s = value };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			replaceTag,
			stringNilTag, stringNilField,
			processStreamTranslation(tags, "options.hex_m_input"),
			processStreamTranslation(tags, "options.hex_m_output"),
			processStreamTranslation(tags, "options.hex_m_error"),
			pipeNilTag, pipeNilField, nilPayloadTag,
			pipeNilTag, pipeNilField, nilPayloadTag,
			pipeNilTag, pipeNilField, nilPayloadTag,
			union.CName, startedTag, startedField,
			failure)
	}

	// exitedTag and terminatedTag, similarly, are resolved only inside the
	// waitUnions loop: ExitStatus is registered only when a process_wait
	// call actually reached it.
	for _, union := range state.waitUnions {
		exitedTag := processAdtTag(tags, compilerTypes.ExitStatusType.Adt, 0)
		terminatedTag := processAdtTag(tags, compilerTypes.ExitStatusType.Adt, 1)
		exitStatusTag, exitStatusField := streamMemberRef(tags, union, compilerTypes.ExitStatusType)
		failure, err := processErrorArm(tags, literals, union, "waited.status", processMessageWait, false)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_process_wait_%s(hex_process receiver, size_t line, size_t column) {\n"+
				"    hex_process_wait_result waited = hex_process_wait(receiver);\n"+
				"    if (waited.status == 0) {\n"+
				"        hex_t_ExitStatus value = waited.terminated ? (hex_t_ExitStatus){ .tag = %s } : (hex_t_ExitStatus){ .tag = %s, .payload.Exited.hex_m_code = waited.exit_code };\n"+
				"        return (%s){ .tag = %s, .payload.%s = value };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			terminatedTag, exitedTag,
			union.CName, exitStatusTag, exitStatusField,
			failure)
	}

	for _, union := range state.terminateUnions {
		nilTag, _ := nilUnionField(union)
		failure, err := processErrorArm(tags, literals, union, "status", processMessageTerminate, false)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_process_terminate_%s(hex_process receiver, size_t line, size_t column) {\n"+
				"    int status = hex_process_terminate(receiver);\n"+
				"    if (status == 0) {\n"+
				"        return (%s){ .tag = %s };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, nilTag, failure)
	}

	closeAdapter := func(operation, call, payload string, pipe bool) error {
		for _, union := range state.closeUnions {
			nilTag, _ := nilUnionField(union)
			failure, err := processErrorArm(tags, literals, union, "status", payload, pipe)
			if err != nil {
				return err
			}
			receiverType := "hex_process receiver"
			args := "receiver"
			if pipe {
				receiverType = "hex_pipe receiver"
			}
			if operation == "write" {
				receiverType += ", hex_slice_UInt8 from"
				args += ", from"
			}
			fmt.Fprintf(result,
				"\nstatic inline %s hex_%s_%s_%s(%s, size_t line, size_t column) {\n"+
					"    int status = %s(%s);\n"+
					"    if (status == 0) {\n"+
					"        return (%s){ .tag = %s };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				union.CName, map[bool]string{true: "pipe", false: "process"}[pipe], operation, streamAdapterSuffix(union), receiverType,
				call, args,
				union.CName, nilTag, failure)
		}
		return nil
	}
	if err := closeAdapter("close", "hex_process_close", processMessageClose, false); err != nil {
		return err
	}
	if err := closeAdapter("shutdown", "hex_pipe_shutdown", pipeMessageShutdown, true); err != nil {
		return err
	}
	if err := closeAdapter("close", "hex_pipe_close", pipeMessageClose, true); err != nil {
		return err
	}
	if err := closeAdapter("write", "hex_pipe_write", pipeMessageWrite, true); err != nil {
		return err
	}

	for _, union := range state.readUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
		eosTag, _ := streamMemberRef(tags, union, compilerTypes.EoS)
		failure, err := processErrorArm(tags, literals, union, "transfer.status", pipeMessageRead, true)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_pipe_read_%s(hex_pipe receiver, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {\n"+
				"    hex_pipe_transfer transfer = hex_pipe_read(receiver, into, max);\n"+
				"    switch (transfer.status) {\n"+
				"    case 0:\n"+
				"        return (%s){ .tag = %s, .payload.%s = transfer.count };\n"+
				"    case HEX_PROCESS_EOS:\n"+
				"        return (%s){ .tag = %s };\n"+
				"    default:\n"+
				"        return %s;\n"+
				"    }\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, sizeTag, sizeField,
			union.CName, eosTag, failure)
	}
	return nil
}
