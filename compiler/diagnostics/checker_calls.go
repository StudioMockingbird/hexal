package diagnostics

import "fmt"

type TextMismatchKind uint8

const (
	NoTextMismatch TextMismatchKind = iota
	CopyTextToString
	WidenInlineText
	ConvertTextFromBytes
)

type TextMismatch struct {
	Kind        TextMismatchKind
	Capacity    uint64
	Destination string
}

func UnknownFunction(name string) Message {
	return message("type.unknown-function", CategoryType, StageChecker, "unknown function "+name+"; functions must be declared before use")
}

func FunctionCapturesUninitializedBinding(function, binding string) Message {
	return message("type.function-captures-uninitialized-binding", CategoryType, StageChecker,
		"function "+function+" may access entry binding "+binding+" before "+binding+" is initialized")
}

func ValueNotCallable(name string) Message {
	return message("type.value-not-callable", CategoryType, StageChecker, name+" is not callable")
}

func NullableFunctionNeedsNarrowing(name string) Message {
	return message("type.nullable-function-needs-narrowing", CategoryType, StageChecker, name+" may be Nil; narrow it before calling it")
}

func FunctionArity(name string, expected, got int, rest bool) Message {
	if rest {
		return message("type.function-arity", CategoryType, StageChecker,
			fmt.Sprintf("%s expects at least %d arguments; got %d", name, expected, got))
	}
	return message("type.function-arity", CategoryType, StageChecker,
		fmt.Sprintf("%s expects %d arguments; got %d", name, expected, got))
}

func IndirectCalleeInvalid() Message {
	return message("type.indirect-callee-invalid", CategoryType, StageChecker, "a call's callee must be a function name or a method selection")
}

func ExplicitGenericArgumentCountMismatch() Message {
	return message("type.generic-argument-count-mismatch", CategoryType, StageChecker, "explicit generic argument count does not match declaration")
}

func NamedArgumentsRequireConstructor() Message {
	return message("type.named-arguments-require-constructor", CategoryType, StageChecker, "named arguments are valid only for struct and ADT constructors")
}

func CallProducesNoValue(name string) Message {
	return message("type.call-produces-no-value", CategoryType, StageChecker, name+" produces no value")
}

func FunctionArgumentTypeMismatch(callee string, index int, expected, actual string, hint TextMismatch) Message {
	text := fmt.Sprintf("%s argument %d requires %s; got %s", callee, index, expected, actual)
	switch hint.Kind {
	case CopyTextToString:
		text += "; use copy(heap)"
	case WidenInlineText:
		text += fmt.Sprintf("; use widen<%d>()", hint.Capacity)
	case ConvertTextFromBytes:
		text += "; use " + hint.Destination + ".from_bytes(...) for a checked conversion"
	}
	return message("type.function-argument-mismatch", CategoryType, StageChecker, text)
}

func CorelibFunctionArity(name string, expected, got int) Message {
	return message("type.corelib-function-arity", CategoryType, StageChecker,
		fmt.Sprintf("%s expects %d argument(s); got %d", name, expected, got))
}
