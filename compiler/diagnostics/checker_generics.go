package diagnostics

import "fmt"

func GenericFunctionMayFallThrough(function, result string) Message {
	return message("type.generic-function-may-fall-through", CategoryType, StageChecker,
		fmt.Sprintf("returning %s may fall through without returning %s", function, result))
}

func ConflictingInferredTypes(parameter string) Message {
	return message("type.generic-inference-conflict", CategoryType, StageChecker,
		"conflicting inferred types for generic parameter "+parameter)
}

func GenericParameterCannotBeInferred(parameter, owner string) Message {
	return message("type.generic-parameter-cannot-be-inferred", CategoryType, StageChecker,
		fmt.Sprintf("cannot infer generic parameter %s for %s", parameter, owner))
}

func GenericMethodParameterCannotBeInferred(parameter, method string) Message {
	return message("type.generic-method-parameter-cannot-be-inferred", CategoryType, StageChecker,
		fmt.Sprintf("cannot infer generic parameter %s for method %s", parameter, method))
}

func GenericCallCannotInferParameter(name string) Message {
	return message("type.generic-call-cannot-infer-parameter", CategoryType, StageChecker,
		"cannot infer generic parameter for "+name)
}

func GenericSpecializationHasUnresolvedArguments(name string) Message {
	return message("type.generic-specialization-unresolved-arguments", CategoryType, StageChecker,
		"cannot specialize "+name+" with unresolved type arguments")
}

func GenericSpecializationChangesArguments() Message {
	return message("type.generic-specialization-changes-arguments", CategoryType, StageChecker,
		"recursive specialization changes generic arguments")
}

func GenericTypeArgumentCount(name string, expected, actual int) Message {
	return message("type.generic-type-argument-count", CategoryType, StageChecker,
		fmt.Sprintf("generic type %s expects %d type arguments; got %d", name, expected, actual))
}

func UnknownGenericType(name string) Message {
	return message("type.unknown-generic-type", CategoryType, StageChecker, "unknown generic type "+name)
}

func DuplicateDeclaration(name string) Message {
	return message("type.duplicate-declaration", CategoryType, StageChecker, name+" is already declared")
}

func GenericMethodRequiresGenericReceiver() Message {
	return message("type.generic-method-requires-generic-receiver", CategoryType, StageChecker, "a generic method requires a generic receiver")
}

func GenericReceiverPatternOverlaps() Message {
	return message("type.generic-receiver-pattern-overlaps", CategoryType, StageChecker, "generic receiver pattern overlaps another implementation")
}

func DuplicateGenericParameter(name string) Message {
	return message("type.duplicate-generic-parameter", CategoryType, StageChecker,
		"generic parameter "+name+" is declared more than once")
}

func ProtectedGenericParameter(name string) Message {
	return message("type.protected-generic-parameter", CategoryType, StageChecker,
		"generic parameter "+name+" is a protected type name")
}
