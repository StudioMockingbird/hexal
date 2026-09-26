package checker

// The time builtins: Duration construction and unit accessors,
// Instant.now and its differences, WallTime.now and its fields, the admitted
// comparison and Duration arithmetic operators, and Task.sleep. Every checked
// operation is one TimeExpression whose Name selects the operation and whose
// Arguments carry every operand in written order, receiver first.

import (
	diagnosticsPkg "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// durationUnits maps each Duration unit spelling to its nanosecond scale.
var durationUnits = map[string]uint64{
	"nanoseconds":  1,
	"microseconds": 1_000,
	"milliseconds": 1_000_000,
	"seconds":      1_000_000_000,
}

// DurationUnitScale reports the nanosecond scale of one Duration unit name.
func DurationUnitScale(unit string) (uint64, bool) {
	scale, ok := durationUnits[unit]
	return scale, ok
}

// checkTimeTypeCall resolves Duration.<unit>(value), Instant.now(), and
// WallTime.now().
func checkTimeTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	name := property.Lexeme
	if len(call.TypeArguments) != 0 {
		return timeMessage(property, diagnosticsPkg.TimeOperationsNoTypeArguments(variable.Name.Lexeme))
	}
	switch variable.Name.Lexeme {
	case "Duration":
		if _, ok := durationUnits[name]; !ok {
			return timeMessage(property, diagnosticsPkg.UnknownDurationOperation())
		}
		if len(call.Arguments) != 1 {
			return timeMessage(property, diagnosticsPkg.DurationConstructorArity(name, len(call.Arguments)))
		}
		value, diagnostics := checkTimeArgument(call.Arguments[0], compilerTypes.UInt64, ctx)
		if len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		return timeNode("duration_"+name, []Operand{value}, compilerTypes.DurationType, compilerTypes.DurationType, property)
	case "Instant":
		if name != "now" || len(call.Arguments) != 0 {
			return timeMessage(property, diagnosticsPkg.UnknownInstantOperation())
		}
		return timeNode("instant_now", nil, compilerTypes.InstantType, compilerTypes.InstantType, property)
	default:
		if name != "now" || len(call.Arguments) != 0 {
			return timeMessage(property, diagnosticsPkg.UnknownWallTimeOperation())
		}
		result := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.WallTimeType, compilerTypes.ErrorType})
		if result == (compilerTypes.Type{}) {
			return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property))}
		}
		checked := timeNode("wall_now", nil, compilerTypes.WallTimeType, result, property)
		checked.source.Node.Span = property.Span
		return checked
	}
}

// checkTaskSleepCall resolves Task.sleep(duration). Sleep parks only the
// current Task; it is a blocking operation but never an explicit yield.
func checkTaskSleepCall(call parser.CallExpression, property lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return timeMessage(property, diagnosticsPkg.TaskSleepArity(len(call.Arguments)))
	}
	duration, diagnostics := checkTimeArgument(call.Arguments[0], compilerTypes.DurationType, ctx)
	if len(diagnostics) > 0 {
		return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	return timeNode("task_sleep", []Operand{duration}, compilerTypes.DurationType, compilerTypes.Type{}, property)
}

// checkTimeMethodCall resolves the value operations of one time receiver.
func checkTimeMethodCall(call methodCall) checkedExpression {
	property := call.callee.Property
	name := property.Lexeme
	if len(call.call.TypeArguments) != 0 {
		return timeMessage(property, diagnosticsPkg.GenericTimeMethodUnsupported(call.receiver.typ.Name, name))
	}
	// A union binding narrowed to its time member reads through its payload.
	call.receiver = valueFromPlace(call.receiver)
	switch {
	case compilerTypes.IsDuration(call.receiver.typ):
		unit, ok := durationAccessorUnit(name)
		if !ok {
			return timeMessage(property, diagnosticsPkg.UnknownDurationMethod(name))
		}
		if len(call.call.Arguments) != 0 {
			return timeMessage(property, diagnosticsPkg.NoArgumentsExpected(name))
		}
		return timeNode("duration_as_"+unit, []Operand{call.receiver.source}, compilerTypes.DurationType, compilerTypes.UInt64, property)
	case compilerTypes.IsInstant(call.receiver.typ):
		switch name {
		case "elapsed":
			if len(call.call.Arguments) != 0 {
				return timeMessage(property, diagnosticsPkg.NoArgumentsExpected("elapsed"))
			}
			return timeNode("instant_elapsed", []Operand{call.receiver.source}, compilerTypes.InstantType, compilerTypes.DurationType, property)
		case "duration_since":
			if len(call.call.Arguments) != 1 {
				return timeMessage(property, diagnosticsPkg.InstantDurationSinceArity(len(call.call.Arguments)))
			}
			earlier, diagnostics := checkTimeArgument(call.call.Arguments[0], compilerTypes.InstantType, call.ctx)
			if len(diagnostics) > 0 {
				return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			return timeNode("instant_since", []Operand{call.receiver.source, earlier}, compilerTypes.InstantType, compilerTypes.DurationType, property)
		}
		return timeMessage(property, diagnosticsPkg.UnknownInstantMethod(name))
	default:
		if len(call.call.Arguments) != 0 {
			return timeMessage(property, diagnosticsPkg.NoArgumentsExpected(name))
		}
		switch name {
		case "seconds":
			return timeNode("wall_seconds", []Operand{call.receiver.source}, compilerTypes.WallTimeType, compilerTypes.Int64, property)
		case "nanosecond":
			return timeNode("wall_nanosecond", []Operand{call.receiver.source}, compilerTypes.WallTimeType, compilerTypes.UInt32, property)
		}
		return timeMessage(property, diagnosticsPkg.UnknownWallTimeMethod(name))
	}
}

// checkTimeBinary owns every binary operator with a time operand. Operands
// must share one time type. Every time type compares; Duration adds and
// subtracts to Duration; Instant subtracts to Duration.
func checkTimeBinary(operator Operator, left, right checkedExpression, token lexer.Token) checkedExpression {
	if !compilerTypes.Equal(left.typ, right.typ) {
		return timeMessage(token, diagnosticsPkg.TimeOperatorRequiresIdenticalTypes(operator.String(), left.typ.Name, right.typ.Name))
	}
	operands := []Operand{left.source, right.source}
	switch operator {
	case EqualOperator, NotEqualOperator, LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator:
		checked := timeNode("compare", operands, left.typ, compilerTypes.Bool, token)
		checked.source.Node.Operator = operator
		return checked
	case AddOperator:
		if compilerTypes.IsDuration(left.typ) {
			return timeNode("duration_add", operands, left.typ, compilerTypes.DurationType, token)
		}
	case SubtractOperator:
		if compilerTypes.IsDuration(left.typ) {
			return timeNode("duration_sub", operands, left.typ, compilerTypes.DurationType, token)
		}
		if compilerTypes.IsInstant(left.typ) {
			return timeNode("instant_since", operands, left.typ, compilerTypes.DurationType, token)
		}
	}
	return timeMessage(token, diagnosticsPkg.TimeOperatorUndefined(operator.String(), left.typ.Name))
}

// durationAccessorUnit maps as_<unit> to its unit spelling.
func durationAccessorUnit(name string) (string, bool) {
	const prefix = "as_"
	if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
		return "", false
	}
	unit := name[len(prefix):]
	_, ok := durationUnits[unit]
	return unit, ok
}

// checkTimeArgument checks one argument against its exact expected type; no
// implicit numeric conversion is admitted.
func checkTimeArgument(argument parser.Expression, expected compilerTypes.Type, ctx checkContext) (Operand, compilerTypes.Diagnostics) {
	checked := checkInitializer(argument, compilerTypes.NewTypeUse(expected), tokenOf(argument), ctx)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return Operand{}, diagnostics
	}
	if !compilerTypes.Equal(expected, checked.typ) {
		return Operand{}, compilerTypes.Diagnostics{typeMismatchDiagnostic(expected, checked.typ, checked.token)}
	}
	return checked.source, nil
}

func timeNode(name string, arguments []Operand, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	node := Expression{Kind: TimeExpression, Name: name, Arguments: arguments, OperandType: operandType, ResultType: resultType}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

func timeMessage(token lexer.Token, message diagnosticsPkg.Message) checkedExpression {
	return checkedExpression{token: token, diagnostic: diagnosticAt(messageAt(token, message))}
}
