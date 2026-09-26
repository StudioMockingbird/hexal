package diagnostics

import "fmt"

func ListTypeArgumentCount() Message {
	return message("type.list-type-argument-count", CategoryType, StageChecker, "List requires exactly one element type")
}

func InvalidListElementType(name string) Message {
	return message("type.list-element-type-invalid", CategoryType, StageChecker, name+" is not a list element type")
}

func ListConstructorArgumentShape() Message {
	return message("type.list-constructor-argument-shape", CategoryType, StageChecker, "List requires exactly one type argument and a Heap; use List<T>(heap)")
}

func ListConstructorHeapType(actual string) Message {
	return message("type.list-constructor-heap-type", CategoryType, StageChecker, "List<T>.new requires a Heap; got "+actual)
}

func ListMethodArgumentCount(method string, expected, got int) Message {
	return message("type.list-method-argument-count", CategoryType, StageChecker,
		fmt.Sprintf("%s expects %d argument; got %d", method, expected, got))
}

func ListFreeHeapType(actual string) Message {
	return message("type.list-free-heap-type", CategoryType, StageChecker, "free requires a Heap; got "+actual)
}

func ListElementTypeMismatch(expected, actual string, hint TextMismatch) Message {
	text := "list element requires " + expected + "; got " + actual
	switch hint.Kind {
	case CopyTextToString:
		text += "; use copy(heap)"
	case WidenInlineText:
		text += fmt.Sprintf("; use widen<%d>()", hint.Capacity)
	case ConvertTextFromBytes:
		text += "; use " + hint.Destination + ".from_bytes(...) for a checked conversion"
	}
	return message("type.list-element-type-mismatch", CategoryType, StageChecker, text)
}
