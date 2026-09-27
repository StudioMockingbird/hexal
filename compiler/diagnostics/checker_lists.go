package diagnostics

import "fmt"

func ArrayTypeRemoved() Message {
	return message("type.array-removed", CategoryType, StageChecker, "Array<T, N> was removed; use List<T, N>")
}

func AddressBytesLiteralLength(variant string, want, got int) Message {
	return message("type.address-bytes-literal-length", CategoryType, StageChecker,
		fmt.Sprintf("Address.%s bytes literal requires %d elements; got %d", variant, want, got))
}

func ListTypeArgumentCount() Message {
	return message("type.list-type-argument-count", CategoryType, StageChecker, "List requires one element type and optionally one positive integer capacity")
}

func InvalidListCapacity() Message {
	return message("type.list-capacity-invalid", CategoryType, StageChecker, "List capacity must be a positive integer literal")
}

func InvalidListArguments() Message {
	return message("type.list-arguments-invalid", CategoryType, StageChecker, "List requires one element type and optionally one positive integer capacity")
}

func InlineListEstimatedStorage(limit uint64) Message {
	return message("type.inline-list-storage-limit", CategoryType, StageChecker, fmt.Sprintf("inline List estimated storage reaches the compiler limit of %d bytes", limit))
}

func InlineListLiteralNeedsContext() Message {
	return message("type.inline-list-literal-needs-context", CategoryType, StageChecker, "an inline List literal requires an expected List<T, N> destination type")
}

func InlineListLiteralCapacity(count int, capacity uint64) Message {
	return message("type.inline-list-literal-capacity", CategoryType, StageChecker, fmt.Sprintf("List<T, N> literal has %d elements but capacity is %d", count, capacity))
}

func InlineListConstructorArguments() Message {
	return message("type.inline-list-constructor-arguments", CategoryType, StageChecker, "List<T, N>() takes no arguments; use a contextual [value, ...] literal")
}

func InlineListMutationRequiresWritable() Message {
	return message("type.inline-list-mutation-requires-writable", CategoryType, StageChecker, "inline List mutation requires a writable receiver")
}

func InlineListCannotBeFreed() Message {
	return message("type.inline-list-cannot-be-freed", CategoryType, StageChecker, "inline List storage cannot be freed")
}

func InvalidListElementType(name string) Message {
	return message("type.list-element-type-invalid", CategoryType, StageChecker, name+" is not a list element type")
}

func ListConstructorArgumentShape() Message {
	return message("type.list-constructor-argument-shape", CategoryType, StageChecker, "List<T> construction requires exactly one Heap")
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
