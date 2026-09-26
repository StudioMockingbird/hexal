package diagnostics

func RuneMethodNotFound(name string) Message {
	return message("type.rune-method-not-found", CategoryType, StageChecker, "Rune has no method "+name)
}

func RuneMethodNoValueArguments(name string) Message {
	return message("type.rune-method-no-value-arguments", CategoryType, StageChecker, name+" expects no arguments")
}

func RuneMethodNoTypeArguments(name string) Message {
	return message("type.rune-method-no-type-arguments", CategoryType, StageChecker, name+" takes no type arguments")
}

func RuneOperationUnsupported() Message {
	return message("type.rune-operation-unsupported", CategoryType, StageChecker, "Rune has no such operation; use Rune.from(value)")
}

func RuneFromArgumentCount() Message {
	return message("type.rune-from-argument-count", CategoryType, StageChecker, "Rune.from expects 1 argument")
}

func RuneFromTypeArguments() Message {
	return message("type.rune-from-type-arguments", CategoryType, StageChecker, "Rune.from takes no type arguments")
}

func RuneFromValueType(actual string) Message {
	return message("type.rune-from-value-type", CategoryType, StageChecker, "Rune.from requires a UInt32; got "+actual)
}
