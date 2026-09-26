package diagnostics

import "fmt"

func BreakOutsideLoop() Message {
	return message("type.break-outside-loop", CategoryType, StageChecker, "break is only valid inside a loop")
}

func ContinueOutsideLoop() Message {
	return message("type.continue-outside-loop", CategoryType, StageChecker, "continue is only valid inside a loop")
}

func ForBinderCountMismatch() Message {
	return message("type.for-binder-count-mismatch", CategoryType, StageChecker, "for-in binder count does not match the source type")
}

func SequenceIterationBinderCount() Message {
	return message("type.sequence-iteration-binder-count", CategoryType, StageChecker,
		"sequence iteration requires one value binder or index and value binders")
}

func DictIterationBinderCount() Message {
	return message("type.dict-iteration-binder-count", CategoryType, StageChecker,
		"dictionary iteration requires key and value binders or index, key, and value binders")
}

func ValueNotIterable(name string) Message {
	return message("type.value-not-iterable", CategoryType, StageChecker, "value of type "+name+" is not iterable")
}

type IterationMutationKind uint8

const (
	MutateCollectionDuringIteration IterationMutationKind = iota
	FreeCollectionDuringIteration
	PassCollectionDuringIteration
)

func IterationMutation(kind IterationMutationKind) Message {
	text := "cannot mutate collection during iteration"
	switch kind {
	case FreeCollectionDuringIteration:
		text = "cannot free collection during iteration"
	case PassCollectionDuringIteration:
		text = "cannot pass traversed collection to call during iteration"
	}
	return message("type.iteration-mutation", CategoryType, StageChecker, text)
}

func ReturnOutsideFunctionContext() Message {
	return message("type.return-outside-function-context", CategoryType, StageChecker, "return is valid only in the entry module or a function body")
}

func ReturnValueRequired(owner, result string) Message {
	return message("type.return-value-required", CategoryType, StageChecker, fmt.Sprintf("return requires a value; %s declares %s", owner, result))
}

func FunctionReturnsNoValue(owner string) Message {
	return message("type.function-returns-no-value", CategoryType, StageChecker, owner+" returns no value; use a bare return")
}

func EntryReturnTypeMismatch(actual string) Message {
	return message("type.entry-return-type-mismatch", CategoryType, StageChecker, "entry-module return requires UInt8; got "+actual)
}

func TextForBinderTypeAmbiguous(name, source string) Message {
	return message("type.text-for-binder-ambiguous", CategoryType, StageChecker,
		fmt.Sprintf("for binder %s over %s has an ambiguous element type; annotate it, for example for %s: Byte in ...", name, source, name))
}

func ForBinderAnnotationMismatch(name, annotation, source, yielded string) Message {
	return message("type.for-binder-annotation-mismatch", CategoryType, StageChecker,
		fmt.Sprintf("for binder %s is annotated %s, but %s yields %s there", name, annotation, source, yielded))
}
