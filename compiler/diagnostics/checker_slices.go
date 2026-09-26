package diagnostics

import "fmt"

func InvalidSliceElementType(name string) Message {
	return message("type.slice-element-invalid", CategoryType, StageChecker, name+" is not a valid Slice element type")
}

func SliceMethodArgumentCount(method string, got int) Message {
	return message("type.slice-method-argument-count", CategoryType, StageChecker,
		fmt.Sprintf("%s expects 2 arguments; got %d", method, got))
}

func SliceRootedInTemporary(kind string) Message {
	return message("type.slice-rooted-in-temporary", CategoryType, StageChecker, "a Slice cannot be rooted in a temporary "+kind)
}

func MutSliceRequiresWritableArray() Message {
	return message("type.mut-slice-requires-writable-array", CategoryType, StageChecker, "mut_slice requires a writable Array place")
}

func SliceRangeOutOfBounds(start, end int64, array string) Message {
	return message("type.slice-range-out-of-bounds", CategoryType, StageChecker,
		fmt.Sprintf("slice range [%d, %d) is out of bounds for %s", start, end, array))
}

func PointerReturnBorrowsLocal(kind string) Message {
	return message("type.pointer-return-borrows-local", CategoryType, StageChecker,
		fmt.Sprintf("a %s cannot be returned when it borrows a local of this function", kind))
}

func SliceTypeArgumentCount() Message {
	return message("type.slice-type-argument-count", CategoryType, StageChecker, "Slice requires exactly one element type")
}

func SliceFromPointerArgumentCount(writable bool) Message {
	constructor := "Slice"
	if writable {
		constructor = "Slice<mut T>"
	}
	return message("type.slice-from-pointer-argument-count", CategoryType, StageChecker,
		constructor+".from_pointer expects 2 arguments (pointer, length)")
}

func SlicePointerMustBeNarrowed() Message {
	return message("type.slice-pointer-must-be-narrowed", CategoryType, StageChecker, "nullable pointer must be narrowed before Slice construction")
}

func SliceFromPointerTypeMismatch(writable bool, element, actual string) Message {
	constructor := "Slice"
	required := "Ptr<" + element + "> or Ptr<mut " + element + ">"
	if writable {
		constructor = "Slice<mut T>"
		required = "Ptr<mut " + element + ">"
	}
	return message("type.slice-from-pointer-type-mismatch", CategoryType, StageChecker,
		fmt.Sprintf("%s.from_pointer requires %s; got %s", constructor, required, actual))
}

func SliceLengthNotRepresentableAsSize() Message {
	return message("type.slice-length-not-size", CategoryType, StageChecker, "Slice length cannot be represented as Size")
}

func SliceEmptyArguments(writable bool) Message {
	constructor := "Slice"
	if writable {
		constructor = "Slice<mut T>"
	}
	return message("type.slice-empty-arguments", CategoryType, StageChecker, constructor+".empty expects no arguments")
}

func SliceBridgeOperationNotFound(writable bool) Message {
	constructor := "Slice"
	if writable {
		constructor = "Slice<mut T>"
	}
	return message("type.slice-bridge-operation-not-found", CategoryType, StageChecker,
		constructor+" has no such operation; use from_pointer or empty")
}

func SlicePointerMethodNotFound(receiver string) Message {
	return message("type.slice-pointer-method-not-found", CategoryType, StageChecker, receiver+" has no method pointer")
}

func SlicePointerMethodArguments() Message {
	return message("type.slice-pointer-method-arguments", CategoryType, StageChecker, "pointer expects no arguments")
}

func SlicePointerElementInvalid(element string) Message {
	return message("type.slice-pointer-element-invalid", CategoryType, StageChecker, element+" is not a valid pointer element type")
}
