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
// processStartAdapterModel carries one process start adapter's decided
// union type and name suffix, environment and working-directory tags and
// field, stream translations, pipe and nil tags, started arm tag and
// payload field, and failure arm text.
type processStartAdapterModel struct {
	CName        string
	Suffix       string
	ReplaceTag   string
	WorkingTag   string
	WorkingField string
	Input        string
	Output       string
	Error        string
	PipeTag      string
	PipeField    string
	NilTag       string
	Success      string
	Field        string
	Failure      string
}

// processWaitAdapterModel carries one process wait adapter's decided union
// type and name suffix, terminated and exited arm tags, exit-status arm
// tag and payload field, and failure arm text.
type processWaitAdapterModel struct {
	CName      string
	Suffix     string
	Terminated string
	Exited     string
	Success    string
	Field      string
	Failure    string
}

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
		if err := renderInto(result, "module.h", "process_start_adapter", processStartAdapterModel{
			CName:        union.CName,
			Suffix:       streamAdapterSuffix(union),
			ReplaceTag:   replaceTag,
			WorkingTag:   stringNilTag,
			WorkingField: stringNilField,
			Input:        processStreamTranslation(tags, "options.hex_m_input"),
			Output:       processStreamTranslation(tags, "options.hex_m_output"),
			Error:        processStreamTranslation(tags, "options.hex_m_error"),
			PipeTag:      pipeNilTag,
			PipeField:    pipeNilField,
			NilTag:       nilPayloadTag,
			Success:      startedTag,
			Field:        startedField,
			Failure:      failure,
		}); err != nil {
			return err
		}
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
		if err := renderInto(result, "module.h", "process_wait_adapter", processWaitAdapterModel{
			CName:      union.CName,
			Suffix:     streamAdapterSuffix(union),
			Terminated: terminatedTag,
			Exited:     exitedTag,
			Success:    exitStatusTag,
			Field:      exitStatusField,
			Failure:    failure,
		}); err != nil {
			return err
		}
	}

	for _, union := range state.terminateUnions {
		nilTag, _ := nilUnionField(union)
		failure, err := processErrorArm(tags, literals, union, "status", processMessageTerminate, false)
		if err != nil {
			return err
		}
		if err := renderInto(result, "module.h", "owned_status_adapter", tcpStatusAdapterModel{
			CName:     union.CName,
			Owner:     "process",
			Operation: "terminate",
			Suffix:    streamAdapterSuffix(union),
			Params:    "hex_process receiver",
			Call:      "hex_process_terminate(receiver)",
			Success:   nilTag,
			Error:     failure,
		}); err != nil {
			return err
		}
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
			if err := renderInto(result, "module.h", "owned_status_adapter", tcpStatusAdapterModel{
				CName:     union.CName,
				Owner:     map[bool]string{true: "pipe", false: "process"}[pipe],
				Operation: operation,
				Suffix:    streamAdapterSuffix(union),
				Params:    receiverType,
				Call:      call + "(" + args + ")",
				Success:   nilTag,
				Error:     failure,
			}); err != nil {
				return err
			}
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
		if err := renderInto(result, "module.h", "pipe_read_adapter", streamReadAdapterModel{
			Suffix:  streamAdapterSuffix(union),
			CName:   union.CName,
			Success: sizeTag,
			Field:   sizeField,
			EosTag:  eosTag,
			Error:   failure,
		}); err != nil {
			return err
		}
	}
	return nil
}
