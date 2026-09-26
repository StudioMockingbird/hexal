package diagnostics

import "fmt"

func FreeLocalStoragePointer() Message {
	return message("type.free-local-storage-pointer", CategoryType, StageChecker, "free does not accept a pointer into this function's local storage")
}

func FreeStashAllocatedPointer() Message {
	return message("type.free-stash-allocated-pointer", CategoryType, StageChecker, "free does not accept a pointer allocated from a Stash")
}

func FreePoolAllocatedPointer() Message {
	return message("type.free-pool-allocated-pointer", CategoryType, StageChecker, "free does not accept a pointer allocated from a Pool")
}

func PoolFreeHeapAllocatedPointer() Message {
	return message("type.pool-free-heap-allocated-pointer", CategoryType, StageChecker, "Pool free does not accept a pointer allocated from the Heap")
}

func PoolFreeStashAllocatedPointer() Message {
	return message("type.pool-free-stash-allocated-pointer", CategoryType, StageChecker, "Pool free does not accept a pointer allocated from a Stash")
}

func DoubleFree() Message {
	return message("type.double-free", CategoryType, StageChecker, "free releases storage already released on every path to this point")
}

func UseAfterFree() Message {
	return message("type.use-after-free", CategoryType, StageChecker, "this pointer's storage was released on every path to this point")
}

func ErrorConstructorTypeArguments() Message {
	return message("type.error-constructor-type-arguments", CategoryType, StageChecker, "Error must be created with Error(kind, message)")
}

func ErrorConstructorArity(got int) Message {
	return message("type.error-constructor-arity", CategoryType, StageChecker,
		fmt.Sprintf("Error expects 2 arguments (kind, message); got %d", got))
}

func ErrorConstructorKindType() Message {
	return message("type.error-constructor-kind-type", CategoryType, StageChecker,
		"Error requires ErrorKind as its first argument; use Error(ErrorKind.Other(header = ...), message)")
}

func ErrorMethodNotFound(name string) Message {
	return message("type.error-method-not-found", CategoryType, StageChecker, "Error has no method named "+name)
}

func ErrorKindMethodNotFound(name string) Message {
	return message("type.error-kind-method-not-found", CategoryType, StageChecker, "ErrorKind has no method named "+name)
}

func ErrorHeaderMethodArguments() Message {
	return message("type.error-header-method-arguments", CategoryType, StageChecker, "header takes no arguments")
}

func TryInsideCleanup() Message {
	return message("type.try-inside-cleanup", CategoryType, StageChecker, "try is not permitted inside defer or errdefer")
}

func TryRequiresErrorResult() Message {
	return message("type.try-requires-error-result", CategoryType, StageChecker,
		"try requires an enclosing function whose result accepts Error")
}

func TryOperandMustContainErrorAndSuccess(actual string) Message {
	return message("type.try-operand-invalid", CategoryType, StageChecker,
		"try requires a union containing Error and a success member; got "+actual)
}

func ErrdeferRequiresErrorResult() Message {
	return message("type.errdefer-requires-error-result", CategoryType, StageChecker,
		"errdefer requires an enclosing function whose result accepts Error")
}
