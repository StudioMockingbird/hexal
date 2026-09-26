package diagnostics

import "fmt"

func InvalidArrayLength() Message {
	return message("type.array-length-invalid", CategoryType, StageChecker, "an array length must be a positive decimal integer")
}

func InvalidArrayElementType(name string) Message {
	return message("type.array-element-invalid", CategoryType, StageChecker, name+" is not an inline array element type")
}

func EmptyArrayLiteral() Message {
	return message("type.array-literal-empty", CategoryType, StageChecker, "an array literal requires at least one element")
}

func ArrayLiteralNeedsContext() Message {
	return message("type.array-literal-needs-context", CategoryType, StageChecker, "an array literal requires an expected Array<T, N> destination type")
}

func ArrayLiteralElementCount(name string, length uint64, got int) Message {
	return message("type.array-literal-element-count", CategoryType, StageChecker,
		fmt.Sprintf("Array<%s, %d> requires exactly %d elements; got %d", name, length, length, got))
}

func ArrayIndexNotInteger(name string) Message {
	return message("type.array-index-not-integer", CategoryType, StageChecker, "an array index must be an integer; got "+name)
}

func ArrayIndexNegative() Message {
	return message("type.array-index-negative", CategoryType, StageChecker, "an array index must be non-negative")
}

func TextCannotBeIndexed(name string) Message {
	return message("type.text-not-indexable", CategoryType, StageChecker, "cannot index "+name+"; use bytes() for indexed byte access")
}

func ValueNotIndexable(name string) Message {
	return message("type.value-not-indexable", CategoryType, StageChecker, "cannot index "+name+"; expected Array<T, N>, Slice<T>, or List<T>")
}

func ArrayIndexOutOfBounds(index uint64, name string) Message {
	return message("type.array-index-out-of-bounds", CategoryType, StageChecker, fmt.Sprintf("array index %d is out of bounds for %s", index, name))
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
