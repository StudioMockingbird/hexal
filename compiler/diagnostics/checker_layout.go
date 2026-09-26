package diagnostics

import "fmt"

func LayoutQueryTypeArgumentCount(name string) Message {
	return message("type.layout-query-type-argument-count", CategoryType, StageChecker, name+" requires exactly one type argument")
}

func LayoutQueryValueArguments(name string) Message {
	return message("type.layout-query-value-arguments", CategoryType, StageChecker, name+" takes no value arguments")
}

func LayoutQueryTypeIncomplete(name, typ string) Message {
	return message("type.layout-query-type-incomplete", CategoryType, StageChecker,
		name+" requires one complete finite-sized type; got "+typ)
}

func VolatileTypeUnsupported(typ string) Message {
	return message("type.volatile-type-unsupported", CategoryType, StageChecker,
		"volatile access is supported only for integer storage types; got "+typ)
}

func VolatileReadArguments() Message {
	return message("type.volatile-read-arguments", CategoryType, StageChecker, "read_volatile expects no arguments")
}

func VolatileWriteReadOnlyPointer(element string) Message {
	return message("type.volatile-write-read-only-pointer", CategoryType, StageChecker,
		"Ptr<"+element+"> is read-only; volatile write requires Ptr<mut "+element+">")
}

func VolatileWriteArgumentCount() Message {
	return message("type.volatile-write-argument-count", CategoryType, StageChecker, "write_volatile expects 1 argument")
}

func VolatileWriteTypeMismatch(expected, actual string) Message {
	return message("type.volatile-write-type-mismatch", CategoryType, StageChecker,
		fmt.Sprintf("write_volatile requires %s; got %s", expected, actual))
}

func VolatileOperationUnsupported() Message {
	return message("type.volatile-operation-unsupported", CategoryType, StageChecker,
		"volatile access supports read_volatile and write_volatile only")
}
