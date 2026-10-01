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
	return message("type.rune-operation-unsupported", CategoryType, StageChecker, "Rune has no such operation; use value.to<Rune>()")
}
