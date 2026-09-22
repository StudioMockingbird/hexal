package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The time family's Go side: discovery, call-site rendering, checked
// metadata validation, and the module-owned WallTime.now adapter that builds
// the structural result union.

const timeMessageUnavailable = "wall clock acquisition failed"

// generatedTimeState records one module's (or the merged program's) time
// demand. used selects hexal/time.h; instant selects the libuv monotonic
// clock; sleep selects the scheduler and the event bridge's timer command.
type generatedTimeState struct {
	used        bool
	instant     bool
	sleep       bool
	wallUnions  []compilerTypes.Type
	fileLiteral literalHandle
}

// discoverGeneratedTime walks one module for time types and operations.
func discoverGeneratedTime(program checker.Program, logicalKey string, literals *literalRegistry) *generatedTimeState {
	state := &generatedTimeState{}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsTime(typ) {
				state.used = true
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.TimeExpression {
				return nil
			}
			state.used = true
			switch node.Name {
			case "instant_now", "instant_elapsed":
				state.instant = true
			case "task_sleep":
				state.sleep = true
			case "wall_now":
				state.wallUnions = appendUnionOnce(state.wallUnions, node.ResultType)
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	if len(state.wallUnions) > 0 {
		state.fileLiteral = literals.Intern(logicalKey)
		literals.Intern(timeMessageUnavailable)
	}
	return state
}

// timeOperationArity is the exact operand count of every time operation.
func timeOperationArity(name string) (int, bool) {
	switch name {
	case "instant_now", "wall_now":
		return 0, true
	case "instant_elapsed", "wall_seconds", "wall_nanosecond", "task_sleep":
		return 1, true
	case "duration_add", "duration_sub", "instant_since", "compare":
		return 2, true
	}
	if unit, ok := strings.CutPrefix(name, "duration_as_"); ok {
		_, known := checker.DurationUnitScale(unit)
		return 1, known
	}
	if unit, ok := strings.CutPrefix(name, "duration_"); ok {
		_, known := checker.DurationUnitScale(unit)
		return 1, known
	}
	return 0, false
}

// renderTimeExpression renders one time operation from its hoisted operands.
func renderTimeExpression(node checker.Expression, state *expressionValidation) (string, error) {
	arguments := make([]string, 0, len(node.Arguments))
	for index := range node.Arguments {
		rendered, err := renderHoistedOperand(&node.Arguments[index].Node, node.Arguments[index], state)
		if err != nil {
			return "", err
		}
		arguments = append(arguments, rendered)
	}
	return timeCall(node, arguments)
}

// timeCall spells one time operation over already-rendered operands. The
// deferred-cleanup path shares it with the ordinary render path.
func timeCall(node checker.Expression, arguments []string) (string, error) {
	if arity, ok := timeOperationArity(node.Name); !ok || arity != len(arguments) {
		return "", unknownExpressionDiagnostic("time operation has invalid rendered operands")
	}
	switch node.Name {
	case "instant_now":
		return "hex_instant_now()", nil
	case "instant_elapsed":
		return "hex_instant_elapsed(" + arguments[0] + ")", nil
	case "instant_since":
		return "hex_instant_since(" + arguments[0] + ", " + arguments[1] + ")", nil
	case "duration_add":
		return "hex_duration_add(" + arguments[0] + ", " + arguments[1] + ")", nil
	case "duration_sub":
		return "hex_duration_sub(" + arguments[0] + ", " + arguments[1] + ")", nil
	case "wall_now":
		return fmt.Sprintf("hex_wall_time_now_%s(%d, %d)", streamAdapterSuffix(node.ResultType), node.SourceLine, node.SourceColumn), nil
	case "wall_seconds":
		return "(" + arguments[0] + ").seconds", nil
	case "wall_nanosecond":
		return "(" + arguments[0] + ").nanosecond", nil
	case "task_sleep":
		return "hex_task_sleep(" + arguments[0] + ")", nil
	case "compare":
		operator, err := timeComparisonOperator(node.Operator)
		if err != nil {
			return "", err
		}
		if compilerTypes.IsWallTime(node.OperandType) {
			return "(hex_wall_time_compare(" + arguments[0] + ", " + arguments[1] + ") " + operator + " 0)", nil
		}
		return "(" + arguments[0] + " " + operator + " " + arguments[1] + ")", nil
	}
	if unit, ok := strings.CutPrefix(node.Name, "duration_as_"); ok {
		scale, _ := checker.DurationUnitScale(unit)
		return fmt.Sprintf("(%s / %du)", arguments[0], scale), nil
	}
	unit, _ := strings.CutPrefix(node.Name, "duration_")
	scale, _ := checker.DurationUnitScale(unit)
	return fmt.Sprintf("hex_duration_from(%s, %du)", arguments[0], scale), nil
}

func timeComparisonOperator(operator checker.Operator) (string, error) {
	switch operator {
	case checker.EqualOperator:
		return "==", nil
	case checker.NotEqualOperator:
		return "!=", nil
	case checker.LessOperator:
		return "<", nil
	case checker.LessEqualOperator:
		return "<=", nil
	case checker.GreaterOperator:
		return ">", nil
	case checker.GreaterEqualOperator:
		return ">=", nil
	}
	return "", unknownExpressionDiagnostic("time comparison has no comparison operator")
}

// validateTimeExpression checks one time operation fail-closed: a known
// operation, its exact operand count and types, and its exact result type.
func validateTimeExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	arity, ok := timeOperationArity(node.Name)
	if !ok || arity != len(node.Arguments) || node.Operand != nil {
		return unknownExpressionDiagnostic("time operation has invalid checked metadata")
	}
	operand := node.OperandType
	var operandsOK, resultOK bool
	sameOperands := func(typ compilerTypes.Type) bool {
		for _, argument := range node.Arguments {
			if !compilerTypes.Equal(argument.Type, typ) {
				return false
			}
		}
		return compilerTypes.Equal(operand, typ)
	}
	switch node.Name {
	case "instant_now":
		operandsOK, resultOK = compilerTypes.IsInstant(operand), compilerTypes.IsInstant(node.ResultType)
	case "instant_elapsed", "instant_since":
		operandsOK, resultOK = sameOperands(compilerTypes.InstantType), compilerTypes.IsDuration(node.ResultType)
	case "duration_add", "duration_sub":
		operandsOK, resultOK = sameOperands(compilerTypes.DurationType), compilerTypes.IsDuration(node.ResultType)
	case "task_sleep":
		operandsOK, resultOK = sameOperands(compilerTypes.DurationType), node.ResultType == (compilerTypes.Type{})
	case "wall_now":
		members := compilerTypes.UnionMembers(node.ResultType)
		operandsOK = compilerTypes.IsWallTime(operand) && node.SourceLine > 0
		resultOK = node.ResultType.Union != nil && members.Len() == 2 && unionHasMember(members, compilerTypes.WallTimeType) && unionHasMember(members, compilerTypes.ErrorType)
	case "wall_seconds":
		operandsOK, resultOK = sameOperands(compilerTypes.WallTimeType), compilerTypes.Equal(node.ResultType, compilerTypes.Int64)
	case "wall_nanosecond":
		operandsOK, resultOK = sameOperands(compilerTypes.WallTimeType), compilerTypes.Equal(node.ResultType, compilerTypes.UInt32)
	case "compare":
		_, err := timeComparisonOperator(node.Operator)
		operandsOK, resultOK = compilerTypes.IsTime(operand) && sameOperands(operand) && err == nil, compilerTypes.Equal(node.ResultType, compilerTypes.Bool)
	default:
		if strings.HasPrefix(node.Name, "duration_as_") {
			operandsOK, resultOK = sameOperands(compilerTypes.DurationType), compilerTypes.Equal(node.ResultType, compilerTypes.UInt64)
		} else {
			operandsOK = compilerTypes.IsDuration(operand) && compilerTypes.Equal(node.Arguments[0].Type, compilerTypes.UInt64)
			resultOK = compilerTypes.IsDuration(node.ResultType)
		}
	}
	if !operandsOK || !resultOK {
		return unknownExpressionDiagnostic("time operation " + node.Name + " has mismatched checked types")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("time operation result does not match its expected type")
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

// wallTimeAdapterModel carries one WallTime.now adapter's decided union type,
// name suffix, wall-time and error arm tags and payload fields, the file
// literal, the unsupported error-kind tag, and the failure message literal.
type wallTimeAdapterModel struct {
	CName      string
	Suffix     string
	WallTag    string
	WallField  string
	ErrorTag   string
	ErrorField string
	File       string
	ErrorKind  string
	Message    string
}

// writeTimeInlineHelpers emits the module-owned WallTime.now adapters, one per
// distinct result union, constructing failures with this module's file
// literal and the static header and message.
func writeTimeInlineHelpers(result *strings.Builder, state *generatedTimeState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || len(state.wallUnions) == 0 {
		return nil
	}
	message, ok := literals.Lookup(timeMessageUnavailable)
	if !ok {
		return unknownExpressionDiagnostic("time failure message is missing from the literal registry")
	}
	for _, union := range state.wallUnions {
		wallTag, wallField := streamMemberRef(tags, union, compilerTypes.WallTimeType)
		errorTag, errorField := streamMemberRef(tags, union, compilerTypes.ErrorType)
		if err := renderInto(result, "module.h", "wall_time_adapter", wallTimeAdapterModel{
			CName:      union.CName,
			Suffix:     streamAdapterSuffix(union),
			WallTag:    wallTag,
			WallField:  wallField,
			ErrorTag:   errorTag,
			ErrorField: errorField,
			File:       literals.CName(state.fileLiteral),
			ErrorKind:  errorKindTag(tags, "Unsupported"),
			Message:    literals.CName(message),
		}); err != nil {
			return err
		}
	}
	return nil
}

// mergeTimeInto unions one module's time demand into the program state.
func mergeTimeInto(merged, state *generatedTimeState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.instant = merged.instant || state.instant
	merged.sleep = merged.sleep || state.sleep
}

// timeComponents returns hexal/time.h and hexal/time.c when any time type or
// operation is reachable.
func timeComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged.timeState == nil || !merged.timeState.used {
		return nil, nil
	}
	model := timeComponentModel{Instant: merged.timeState.instant, Sleep: merged.timeState.sleep}
	return []componentArtifact{
		{key: "hexal/time.h", template: "time.h", model: model},
		{key: "hexal/time.c", template: "time.c", model: model},
	}, nil
}

// timeComponentModel gates the libuv monotonic clock and the Task sleep
// declaration; the arithmetic and wall-clock helpers need neither.
type timeComponentModel struct {
	Instant bool
	Sleep   bool
}

// moduleTimeComponent selects hexal/time.h for a module naming a time type.
func moduleTimeComponent(emission *moduleEmission) []string {
	if emission == nil || emission.timeState == nil || !emission.timeState.used {
		return nil
	}
	return []string{"hexal/time.h"}
}
