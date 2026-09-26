package diagnostics

func ExportedFunctionExposesPrivateType(function, privateType string) Message {
	return message("type.exported-function-exposes-private-type", CategoryType, StageChecker,
		"exported function "+function+" exposes private type "+privateType)
}
