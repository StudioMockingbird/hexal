package diagnostics

import "fmt"

func DictTypeArgumentCount() Message {
	return message("type.dict-type-argument-count", CategoryType, StageChecker, "Dict requires exactly two type arguments")
}

func DictKeyStringNotAllowed() Message {
	return message("type.dict-key-string-not-allowed", CategoryType, StageChecker,
		"dictionary key type String is not allowed: a Dict stores its keys, and String does not own its bytes; use String<N>")
}

func DictKeyTypeInvalid() Message {
	return message("type.dict-key-type-invalid", CategoryType, StageChecker, "dictionary key type must be Int32 or String<N>")
}

func InvalidDictValueType(name string) Message {
	return message("type.dict-value-type-invalid", CategoryType, StageChecker, name+" is not a dictionary value type")
}

func DictConstructorArgumentShape() Message {
	return message("type.dict-constructor-argument-shape", CategoryType, StageChecker, "Dict requires exactly two type arguments and a Heap; use Dict<K, V>(heap)")
}

func DictConstructorHeapType(actual string) Message {
	return message("type.dict-constructor-heap-type", CategoryType, StageChecker, "Dict<K, V>.new requires a Heap; got "+actual)
}

func DictMethodArity(name string, expected, got int) Message {
	unit := "argument"
	if expected != 1 {
		unit = "arguments"
	}
	return message("type.dict-method-arity", CategoryType, StageChecker,
		fmt.Sprintf("%s expects %d %s; got %d", name, expected, unit, got))
}

func DictFindValueCannotContainNil(name string) Message {
	return message("type.dict-find-value-nil-invalid", CategoryType, StageChecker, name+" cannot be combined with Nil")
}

func DictFreeHeapType(actual string) Message {
	return message("type.dict-free-heap-type", CategoryType, StageChecker, "free requires a Heap; got "+actual)
}

func DictKeyTypeMismatch(expected, actual string, hint TextMismatch) Message {
	text := "dictionary key requires " + expected + "; got " + actual
	switch hint.Kind {
	case CopyTextToString:
		text += "; use copy(heap)"
	case WidenInlineText:
		text += fmt.Sprintf("; use widen<%d>()", hint.Capacity)
	case ConvertTextFromBytes:
		text += "; use " + hint.Destination + ".from_bytes(...) for a checked conversion"
	}
	return message("type.dict-key-type-mismatch", CategoryType, StageChecker, text)
}
