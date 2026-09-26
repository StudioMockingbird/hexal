package diagnostics

type PointerCountOperation uint8

const (
	PointerOffsetCount PointerCountOperation = iota + 1
	PointerIndexCount
)

func PointerOffsetTypeArguments() Message {
	return message("type.pointer-offset-type-arguments", CategoryType, StageChecker, "offset takes no type arguments")
}

func PointerOffsetArgumentCount() Message {
	return message("type.pointer-offset-argument-count", CategoryType, StageChecker, "offset expects 1 argument (count)")
}

func PointerCastValueArguments() Message {
	return message("type.pointer-cast-value-arguments", CategoryType, StageChecker, "cast expects no arguments")
}

func PointerCastTypeArgumentCount() Message {
	return message("type.pointer-cast-type-argument-count", CategoryType, StageChecker, "cast requires exactly one type argument")
}

func PointerIndexAmbiguous(pointer string) Message {
	return message("type.pointer-index-ambiguous", CategoryType, StageChecker,
		"pointer indexing of "+pointer+" is ambiguous; use (^pointer)[index] to index the collection or pointer.offset(index) to advance the pointer")
}

func PointerCountRequiresSize(operation PointerCountOperation, actual string) Message {
	name := "offset"
	if operation == PointerIndexCount {
		name = "pointer indexing"
	}
	return message("type.pointer-count-requires-size", CategoryType, StageChecker, name+" requires Size; got "+actual)
}

func PointerArithmeticRequiresCompletePointee(name string) Message {
	return message("type.pointer-arithmetic-incomplete-pointee", CategoryType, StageChecker,
		"pointer arithmetic requires a complete pointee type; got "+name)
}
