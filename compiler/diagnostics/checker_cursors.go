package diagnostics

func CursorMethodNotFound(cursor, method string) Message {
	return message("type.cursor-method-not-found", CategoryType, StageChecker, cursor+" has no method "+method)
}

func CursorMethodNoValueArguments(method string) Message {
	return message("type.cursor-method-no-value-arguments", CategoryType, StageChecker, method+" expects no arguments")
}

func CursorMethodNoTypeArguments(method string) Message {
	return message("type.cursor-method-no-type-arguments", CategoryType, StageChecker, method+" takes no type arguments")
}

func CursorMethodRequiresMutableBinding(method string) Message {
	return message("type.cursor-method-requires-mutable-binding", CategoryType, StageChecker,
		method+" mutates its cursor, so the receiver must be a mutable binding")
}
