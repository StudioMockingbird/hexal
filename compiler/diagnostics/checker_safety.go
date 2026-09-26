package diagnostics

type UnsafeOperation uint8

const (
	UnsafeSliceFromPointer UnsafeOperation = iota + 1
	UnsafePointerOffset
	UnsafePointerCast
	UnsafePointerIndex
	UnsafeSlicePointer
	UnsafeStringCPointer
	UnsafeForeignCall
	UnsafeForeignGlobal
)

func RestBackedSliceEscape() Message {
	return message("type.rest-backed-slice-escape", CategoryType, StageChecker,
		"rest-backed Slice cannot escape its function invocation")
}

func GraphemeBytesMissingSliceResult() Message {
	return message("type.grapheme-bytes-missing-slice-result", CategoryType, StageChecker, "Grapheme.bytes has no slice result")
}

func GraphemeMethodNotFound(name string) Message {
	return message("type.grapheme-method-not-found", CategoryType, StageChecker, "Grapheme has no method "+name)
}

func GraphemeMethodNoValueArguments(name string) Message {
	return message("type.grapheme-method-no-value-arguments", CategoryType, StageChecker, name+" expects no arguments")
}

func GraphemeMethodNoTypeArguments(name string) Message {
	return message("type.grapheme-method-no-type-arguments", CategoryType, StageChecker, name+" takes no type arguments")
}

func UnsafeOperationRequiresBlock(operation UnsafeOperation, subject string) Message {
	var name string
	switch operation {
	case UnsafeSliceFromPointer:
		name = "Slice.from_pointer"
	case UnsafePointerOffset:
		name = "Ptr.offset"
	case UnsafePointerCast:
		name = "Ptr.cast"
	case UnsafePointerIndex:
		name = "pointer indexing"
	case UnsafeSlicePointer:
		name = "Slice.pointer"
	case UnsafeStringCPointer:
		name = "String.c_pointer"
	case UnsafeForeignCall:
		name = "foreign call " + subject
	case UnsafeForeignGlobal:
		name = "foreign global " + subject
	default:
		return CheckerFailure()
	}
	return message("type.unsafe-operation-requires-block", CategoryType, StageChecker,
		name+" requires an unsafe do ... end block")
}
