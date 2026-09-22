package generator

// The libuv ordinary-signal-observation family's Go side: Signals
// construction and its next/close instance methods. Every operation lowers
// through one module-owned inline adapter that translates the checked
// Signal ADT tag into the plain values hexal/signal.c's runtime
// understands, and back, following the same structural pattern
// network.go/process.go use.

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

const (
	signalMessageNew   = "signal subscription failed"
	signalMessageNext  = "signal wait failed"
	signalMessageClose = "signal close failed"
)

// isSignalOperation reports whether name is one of the Signals operations
// sharing checker.NetworkExpression with Address/Dns/Tcp/Process/Pipe.
func isSignalOperation(name string) bool {
	switch name {
	case "signals_new", "signals_next", "signals_close":
		return true
	}
	return false
}

// generatedSignalState records one module's (or the merged program's)
// signal-observation reachability. used gates the type-definition component
// (Signal and Signals as bare types); operations additionally gates the
// native runtime -- the handle registry, event bridge, scheduler, libuv, and
// native bootstrap -- matching the "constructing or matching a Signal
// variant selects only its type-definition component" rule.
// hexal/signal.h's own hex_handle embedding is the same disclosed
// simplification hexal/process.h makes: it always follows used, not
// operations.
type generatedSignalState struct {
	used       bool
	operations bool

	newUnions   []compilerTypes.Type
	nextUnions  []compilerTypes.Type
	closeUnions []compilerTypes.Type

	nameLiteral literalHandle
}

// discoverGeneratedSignal walks one module for Signal/Signals types and
// operations.
func discoverGeneratedSignal(program checker.Program, logicalKey string, literals *literalRegistry) *generatedSignalState {
	state := &generatedSignalState{}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsSignal(typ) || compilerTypes.IsSignals(typ) {
				state.used = true
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.NetworkExpression {
				return nil
			}
			switch node.Name {
			case "signals_new":
				state.used, state.operations = true, true
				state.newUnions = appendUnionOnce(state.newUnions, node.ResultType)
			case "signals_next":
				state.used, state.operations = true, true
				state.nextUnions = appendUnionOnce(state.nextUnions, node.ResultType)
			case "signals_close":
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
		for _, message := range []string{signalMessageNew, signalMessageNext, signalMessageClose} {
			literals.Intern(message)
		}
	}
	return state
}

// mergeSignalInto unions one module's signal demand into the program state.
func mergeSignalInto(merged, state *generatedSignalState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.operations = merged.operations || state.operations
}

// elementNeedsSignal reports whether a collection specialization's element
// spells a signal-observation type, whose typedef hexal/signal.h owns.
func elementNeedsSignal(element compilerTypes.Type) bool {
	return compilerTypes.IsSignal(element) || compilerTypes.IsSignals(element)
}

// signalComponents returns hexal/signal.h and hexal/signal.c when any
// Signal/Signals type or operation is reachable. hexal/signal.c has no
// content at all when no operation is reachable, so it renders empty and
// renderComponentArtifacts omits it.
func signalComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged.signalState == nil || !merged.signalState.used {
		return nil, nil
	}
	model := signalSourceModel{Operations: merged.signalState.operations}
	return []componentArtifact{
		{key: "hexal/signal.h", template: "signal.h", model: model},
		{key: "hexal/signal.c", template: "signal.c", model: model},
	}, nil
}

// signalSourceModel gates hexal/signal.h and hexal/signal.c's runtime
// function declarations and definitions.
type signalSourceModel struct {
	Operations bool
}

// moduleSignalComponent selects hexal/signal.h for a module naming any
// Signal/Signals type or operation.
func moduleSignalComponent(emission *moduleEmission) []string {
	if emission == nil || emission.signalState == nil || !emission.signalState.used {
		return nil
	}
	return []string{"hexal/signal.h"}
}

// signalAdtTag names Signal's three declaration-order variants.
func signalAdtTag(tags *tagRegistry, index int) string {
	return tags.adtVariantTag(compilerTypes.SignalType.Adt, index)
}

// signalErrorArm spells one Error payload construction for a Signals
// adapter whose failure carries a hex_signal_error-classified status.
func signalErrorArm(tags *tagRegistry, literals *literalRegistry, union compilerTypes.Type, status, payload string) (string, error) {
	handle, ok := literals.Lookup(payload)
	if !ok {
		return "", unknownExpressionDiagnostic("signal failure message is missing from the literal registry: " + payload)
	}
	tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = hex_signal_error(line, column, %s, &%s) }",
		union.CName, tag, field, status, literals.CName(handle)), nil
}

// writeSignalInlineHelpers emits the module-owned Signals adapters: each
// wraps the signal.c core in one structural result union, built with this
// module's static operation message.
// signalsNewAdapterModel carries one signals-new adapter's decided union
// type, name suffix, interrupt and hangup arm tags, signals arm tag and
// payload field, and failure arm text.
type signalsNewAdapterModel struct {
	CName     string
	Suffix    string
	Interrupt string
	Hangup    string
	Tag       string
	Field     string
	Failure   string
}

// signalsNextAdapterModel carries one signals-next adapter's decided union
// type, name suffix, three signal arm tags, signal arm tag and payload
// field, end-of-stream arm tag, and failure arm text.
type signalsNextAdapterModel struct {
	CName     string
	Suffix    string
	Interrupt string
	Hangup    string
	Terminate string
	Tag       string
	Field     string
	EosTag    string
	Failure   string
}

func writeSignalInlineHelpers(result *strings.Builder, state *generatedSignalState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || !state.operations {
		return nil
	}

	nilUnionField := func(union compilerTypes.Type) (string, string) {
		return streamMemberRef(tags, union, compilerTypes.Nil)
	}

	// signalTag* are resolved only inside the loop that actually needs
	// them: Signal's tag identities are registered in the program-wide tag
	// registry only when a signals_new/signals_next call reached them by
	// constructing or returning a Signal value, never unconditionally (a
	// program using signals_close alone would otherwise hit a hard
	// registry-miss error resolving a tag nothing ever registered).
	for _, union := range state.newUnions {
		interruptTag := signalAdtTag(tags, 0)
		hangupTag := signalAdtTag(tags, 1)
		signalsTag, signalsField := streamMemberRef(tags, union, compilerTypes.SignalsType)
		failure, err := signalErrorArm(tags, literals, union, "created.status", signalMessageNew)
		if err != nil {
			return err
		}
		if err := renderInto(result, "module.h", "signals_new_adapter", signalsNewAdapterModel{
			CName:     union.CName,
			Suffix:    streamAdapterSuffix(union),
			Interrupt: interruptTag,
			Hangup:    hangupTag,
			Tag:       signalsTag,
			Field:     signalsField,
			Failure:   failure,
		}); err != nil {
			return err
		}
	}

	for _, union := range state.nextUnions {
		interruptTag := signalAdtTag(tags, 0)
		hangupTag := signalAdtTag(tags, 1)
		terminateTag := signalAdtTag(tags, 2)
		signalTag, signalField := streamMemberRef(tags, union, compilerTypes.SignalType)
		eosTag, _ := streamMemberRef(tags, union, compilerTypes.EoS)
		failure, err := signalErrorArm(tags, literals, union, "next.status", signalMessageNext)
		if err != nil {
			return err
		}
		if err := renderInto(result, "module.h", "signals_next_adapter", signalsNextAdapterModel{
			CName:     union.CName,
			Suffix:    streamAdapterSuffix(union),
			Interrupt: interruptTag,
			Hangup:    hangupTag,
			Terminate: terminateTag,
			Tag:       signalTag,
			Field:     signalField,
			EosTag:    eosTag,
			Failure:   failure,
		}); err != nil {
			return err
		}
	}

	for _, union := range state.closeUnions {
		nilTag, _ := nilUnionField(union)
		failure, err := signalErrorArm(tags, literals, union, "status", signalMessageClose)
		if err != nil {
			return err
		}
		if err := renderInto(result, "module.h", "owned_status_adapter", tcpStatusAdapterModel{
			CName:     union.CName,
			Owner:     "signals",
			Operation: "close",
			Suffix:    streamAdapterSuffix(union),
			Params:    "hex_signals receiver",
			Call:      "hex_signals_close(receiver)",
			Success:   nilTag,
			Error:     failure,
		}); err != nil {
			return err
		}
	}
	return nil
}
