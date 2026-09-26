package diagnostics

import "fmt"

func HeapConstructorNoArguments() Message {
	return message("type.heap-constructor-no-arguments", CategoryType, StageChecker, "Heap takes no arguments; use Heap()")
}

func AllocationInitializerRequired() Message {
	return message("type.allocation-initializer-required", CategoryType, StageChecker, "allocation requires an explicit initializer")
}

func AllocateArgumentCount(got int) Message {
	return message("type.allocate-argument-count", CategoryType, StageChecker, fmt.Sprintf("allocate expects 1 argument; got %d", got))
}

func AllocateTypeArgumentCount() Message {
	return message("type.allocate-type-argument-count", CategoryType, StageChecker, "allocate requires exactly one type argument")
}

func AllocationTypeIncomplete() Message {
	return message("type.allocation-type-incomplete", CategoryType, StageChecker, "allocation requires a complete finite type")
}

func AllocationInitializerMismatch(expected, actual string) Message {
	return message("type.allocation-initializer-mismatch", CategoryType, StageChecker,
		fmt.Sprintf("allocation initializer requires %s; got %s", expected, actual))
}

func AlignedAllocationArgumentCount() Message {
	return message("type.aligned-allocation-argument-count", CategoryType, StageChecker, "allocate_aligned expects 2 arguments (initial, alignment)")
}

func AlignedAllocationTypeArgumentCount() Message {
	return message("type.aligned-allocation-type-argument-count", CategoryType, StageChecker, "allocate_aligned requires exactly one type argument")
}

func AlignedAllocationAlignmentType(actual string) Message {
	return message("type.aligned-allocation-alignment-type", CategoryType, StageChecker,
		fmt.Sprintf("allocate_aligned requires Size; got %s", actual))
}

func InvalidAllocationAlignment(value uint64) Message {
	return message("type.allocation-alignment-invalid", CategoryType, StageChecker,
		fmt.Sprintf("alignment must be a non-zero power of two; got %d", value))
}

func HeapFreeArgumentCount() Message {
	return message("type.heap-free-argument-count", CategoryType, StageChecker, "free expects exactly one pointer argument")
}

func HeapFreeNonAllocation() Message {
	return message("type.heap-free-non-allocation", CategoryType, StageChecker, "value is not an allocation produced by this Heap")
}
