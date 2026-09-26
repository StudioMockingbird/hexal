package diagnostics

import "fmt"

func NumericOperandsNoCommonType() Message {
	return message("type.numeric-operands-no-common-type", CategoryType, StageChecker, "numeric values have no unique lossless common type")
}
func OperatorRequiresIntegerOperands(operator, left, right string) Message {
	return message("type.operator-requires-integer-operands", CategoryType, StageChecker, fmt.Sprintf("operator %s requires integer operands; got %s and %s", operator, left, right))
}
func IntegerOperandsNoCommonType() Message {
	return message("type.integer-operands-no-common-type", CategoryType, StageChecker, "integer operands have no unique lossless common type")
}
func OperatorRequiresIntegerLeft(operator, actual string) Message {
	return message("type.operator-requires-integer-left", CategoryType, StageChecker, fmt.Sprintf("operator %s requires an integer left operand; got %s", operator, actual))
}
func ShiftCountRequiresInteger(actual string) Message {
	return message("type.shift-count-requires-integer", CategoryType, StageChecker, "shift count must be an integer; got "+actual)
}
func ShiftCountOutOfRange(value int64, typ string) Message {
	return message("type.shift-count-out-of-range", CategoryType, StageChecker, fmt.Sprintf("shift count %d is outside the valid range for %s", value, typ))
}
func OperatorRequiresIdenticalTypes(operator, left, right string) Message {
	return message("type.operator-requires-identical-types", CategoryType, StageChecker, fmt.Sprintf("operator %s requires identical operand types; got %s and %s", operator, left, right))
}
func TypeIsNeverNil(name, verdict string) Message {
	return message("type.type-is-never-nil", CategoryType, StageChecker, fmt.Sprintf("%s is never Nil; the test is always %s", name, verdict))
}
func DivisionByZero() Message {
	return message("type.division-by-zero", CategoryType, StageChecker, "division by zero")
}
func UnsupportedOperator(name string) Message {
	return message("type.unsupported-operator", CategoryType, StageChecker, "unsupported operator "+name)
}
func UnaryOperatorRequiresBool(operator, actual string) Message {
	return message("type.unary-operator-requires-bool", CategoryType, StageChecker, fmt.Sprintf("operator %s requires Bool operands; got %s", operator, actual))
}
func NegationRequiresSignedType(actual string) Message {
	return message("type.negation-requires-signed-type", CategoryType, StageChecker, "negation requires a signed type; got "+actual)
}
func BitwiseNotRequiresInteger(actual string) Message {
	return message("type.bitwise-not-requires-integer", CategoryType, StageChecker, "operator ~ requires an integer operand; got "+actual)
}
func OperatorRequiresNumericOperands(operator, actual string) Message {
	return message("type.operator-requires-numeric-operands", CategoryType, StageChecker, fmt.Sprintf("operator %s requires numeric operands; got %s", operator, actual))
}
func RemainderRequiresInteger(actual string) Message {
	return message("type.remainder-requires-integer", CategoryType, StageChecker, "operator % requires integer operands; got "+actual)
}
func OperatorRequiresOrderedOperands(operator, actual string) Message {
	return message("type.operator-requires-ordered-operands", CategoryType, StageChecker, fmt.Sprintf("operator %s requires ordered operands; got %s", operator, actual))
}
func LogicalOperatorRequiresBool(operator, actual string) Message {
	return message("type.logical-operator-requires-bool", CategoryType, StageChecker, fmt.Sprintf("operator %s requires Bool operands; got %s", operator, actual))
}
func EqualityOperatorRequiresScalar(operator, actual string) Message {
	return message("type.equality-operator-requires-scalar", CategoryType, StageChecker, fmt.Sprintf("operator %s requires scalar operands; got %s", operator, actual))
}
