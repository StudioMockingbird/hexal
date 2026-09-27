package diagnostics

import "fmt"

func SequenceIndexNotInteger(name string) Message {
	return message("type.list-index-not-integer", CategoryType, StageChecker, "an index must be an integer; got "+name)
}

func SequenceIndexNegative() Message {
	return message("type.list-index-negative", CategoryType, StageChecker, "an index must be non-negative")
}

func TextCannotBeIndexed(name string) Message {
	return message("type.text-not-indexable", CategoryType, StageChecker, "cannot index "+name+"; use bytes() for indexed byte access")
}

func ValueNotIndexable(name string) Message {
	return message("type.value-not-indexable", CategoryType, StageChecker, "cannot index "+name+"; expected List<T, N>, Slice<T>, or List<T>")
}

func SequenceIndexOutOfBounds(index uint64, name string) Message {
	return message("type.list-index-out-of-bounds", CategoryType, StageChecker, fmt.Sprintf("index %d is out of bounds for %s", index, name))
}

func SliceHasNoMutSlice() Message {
	return message("type.slice-no-mut-slice", CategoryType, StageChecker, "Slice has no method mut_slice; re-slicing preserves the receiver's access mode through slice")
}

func CollectionHasNoMethod(collection, method string) Message {
	return message("type.collection-no-method", CategoryType, StageChecker, collection+" has no method "+method)
}

func CollectionMethodNoArguments(method string) Message {
	return message("type.collection-method-no-arguments", CategoryType, StageChecker, method+" expects no arguments")
}
