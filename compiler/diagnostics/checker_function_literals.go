package diagnostics

func FunctionLiteralMayFallThrough(result string) Message {
	return message("type.function-literal-may-fall-through", CategoryType, StageChecker,
		"returning function literal may fall through without returning "+result)
}

func GenericParameterShadowsEnclosingFunction(name string) Message {
	return message("type.generic-parameter-shadows-enclosing-function", CategoryType, StageChecker,
		"generic parameter "+name+" is already declared by an enclosing function")
}

func CannotInferGenericFunctionLiteral() Message {
	return message("type.generic-function-literal-needs-context", CategoryType, StageChecker,
		"cannot infer generic parameter for function literal")
}
