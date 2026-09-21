package generator

// The libuv-adjacent terminal family's Go side: Terminal.is_attached and
// Terminal.size, and the plain TerminalSize value type they return. Both
// operations reuse checker.NetworkExpression's Kind exactly like Address,
// Dns, Tcp, Process, and Signals; discoverGeneratedNetwork skips them through
// isTerminalOperation, matching isProcessOperation and isSignalOperation.
// Neither operation carries its own native-error classification table:
// failures route through hex_io_error, the same mapper File/IO/Bytes share,
// via the existing streamErrorArm/streamErrorArmWithKind helpers.

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

const (
	terminalMessageDetection    = "terminal detection failed"
	terminalMessageSizeQuery    = "terminal size query failed"
	terminalMessageNotATerminal = "stream is not a terminal"
	terminalMessageInvalidSize  = "terminal dimensions are invalid"
)

// generatedTerminalState records one module's (or the merged program's)
// terminal reachability. used gates the type-definition component
// (TerminalSize as a bare type); operations additionally gates the native
// query declarations and definitions, matching the "constructing a
// TerminalSize value alone selects only the type-definition component" rule.
type generatedTerminalState struct {
	used       bool
	operations bool

	attachedUnions []compilerTypes.Type
	sizeUnions     []compilerTypes.Type

	fileLiteral literalHandle
}

// isTerminalOperation reports whether name is one of the Terminal operations
// sharing checker.NetworkExpression with Address/Dns/Tcp/Process/Signals.
func isTerminalOperation(name string) bool {
	switch name {
	case "terminal_is_attached", "terminal_size":
		return true
	}
	return false
}

// discoverGeneratedTerminal walks one module for TerminalSize and Terminal
// operations.
func discoverGeneratedTerminal(program checker.Program, logicalKey string, literals *literalRegistry) *generatedTerminalState {
	state := &generatedTerminalState{}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsTerminalSize(typ) {
				state.used = true
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.NetworkExpression || !isTerminalOperation(node.Name) {
				return nil
			}
			state.used, state.operations = true, true
			switch node.Name {
			case "terminal_is_attached":
				state.attachedUnions = appendUnionOnce(state.attachedUnions, node.ResultType)
			case "terminal_size":
				state.sizeUnions = appendUnionOnce(state.sizeUnions, node.ResultType)
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	if state.used {
		state.fileLiteral = literals.Intern(logicalKey)
	}
	if state.operations {
		for _, message := range []string{
			terminalMessageDetection, terminalMessageSizeQuery, terminalMessageNotATerminal, terminalMessageInvalidSize,
		} {
			literals.Intern(message)
		}
	}
	return state
}

// elementNeedsTerminal reports whether a collection specialization's element
// spells TerminalSize, whose typedef hexal/terminal.h owns. hexal/terminal.h
// needs hexal/io.h (for hex_io, its two operations' one parameter) whenever
// Operations is reachable, and hexal/io.h itself unconditionally needs
// hexal/list.h; a shared component naming a TerminalSize specialization
// directly (list.h, array.h, and so on) would invert that dependency
// direction exactly like the Signal/slice.h conflict module_collections.go
// documents, so moduleRoutedElement routes it to module-owned rendering
// instead.
func elementNeedsTerminal(element compilerTypes.Type) bool {
	return compilerTypes.IsTerminalSize(element)
}

// mergeTerminalInto unions one module's terminal demand into the program
// state.
func mergeTerminalInto(merged, state *generatedTerminalState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.operations = merged.operations || state.operations
}

// terminalComponents returns hexal/terminal.h and hexal/terminal.c when
// TerminalSize or a Terminal operation is reachable.
func terminalComponents(merged *programEmission, config Config) ([]componentArtifact, error) {
	if merged.terminalState == nil || !merged.terminalState.used {
		return nil, nil
	}
	model := terminalSourceModel{Operations: merged.terminalState.operations, TargetWindows: targetIsWindows(config)}
	return []componentArtifact{
		{key: "hexal/terminal.h", template: "terminal.h", model: model},
		{key: "hexal/terminal.c", template: "terminal.c", model: model},
	}, nil
}

// terminalSourceModel gates hexal/terminal.h and hexal/terminal.c's native
// query declarations and definitions.
type terminalSourceModel struct {
	Operations    bool
	TargetWindows bool
}

// moduleTerminalComponent selects hexal/terminal.h for a module naming
// TerminalSize or a Terminal operation.
func moduleTerminalComponent(emission *moduleEmission) []string {
	if emission == nil || emission.terminalState == nil || !emission.terminalState.used {
		return nil
	}
	return []string{"hexal/terminal.h"}
}

// writeTerminalInlineHelpers emits the module-owned Terminal adapters: each
// wraps the terminal.c core in one structural result union, built with this
// module's file literal and reusing the shared IO native-error mapper.
func writeTerminalInlineHelpers(result *strings.Builder, state *generatedTerminalState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || !state.operations {
		return nil
	}
	file := "&" + literals.CName(state.fileLiteral)

	for _, union := range state.attachedUnions {
		boolTag, boolField := streamMemberRef(tags, union, compilerTypes.Bool)
		failure, err := streamErrorArm(tags, literals, file, union, "terminal detection", "attached.code", terminalMessageDetection)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_terminal_is_attached_%s(hex_io stream, size_t line, size_t column) {\n"+
				"    hex_terminal_attached_result attached = hex_terminal_is_attached(stream);\n"+
				"    if (attached.status == HEX_TERMINAL_OK) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = attached.attached };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, boolTag, boolField,
			failure)
	}

	for _, union := range state.sizeUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.TerminalSizeType)
		notATerminal, err := streamErrorArmWithKind(tags, literals, file, union, "InvalidInput", terminalMessageNotATerminal)
		if err != nil {
			return err
		}
		invalidDimensions, err := streamErrorArmWithKind(tags, literals, file, union, "InvalidInput", terminalMessageInvalidSize)
		if err != nil {
			return err
		}
		failure, err := streamErrorArm(tags, literals, file, union, "terminal size", "queried.code", terminalMessageSizeQuery)
		if err != nil {
			return err
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_terminal_size_%s(hex_io stream, size_t line, size_t column) {\n"+
				"    hex_terminal_size_result queried = hex_terminal_size(stream);\n"+
				"    switch (queried.status) {\n"+
				"    case HEX_TERMINAL_OK: {\n"+
				"        hex_t_TerminalSize value = { .hex_m_columns = queried.columns, .hex_m_rows = queried.rows };\n"+
				"        return (%s){ .tag = %s, .payload.%s = value };\n"+
				"    }\n"+
				"    case HEX_TERMINAL_NOT_A_TERMINAL:\n"+
				"        return %s;\n"+
				"    case HEX_TERMINAL_INVALID_DIMENSIONS:\n"+
				"        return %s;\n"+
				"    default:\n"+
				"        return %s;\n"+
				"    }\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, sizeTag, sizeField,
			notATerminal, invalidDimensions, failure)
	}
	return nil
}
