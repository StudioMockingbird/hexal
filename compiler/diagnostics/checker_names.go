package diagnostics

func ProtectedBindingName(name string) Message {
	return message("name.protected-binding", CategoryName, StageChecker, name+" is a protected built-in name")
}

func ImportAliasConflict(name string) Message {
	return message("name.import-alias-conflict", CategoryName, StageChecker, "import alias "+name+" conflicts with an existing name")
}

func DuplicateLoopBinder(name string) Message {
	return message("name.duplicate-loop-binder", CategoryName, StageChecker, "duplicate loop binder name "+name)
}

func MissingCImportDeclaration(header, name string) Message {
	return message("name.c-import-declaration-missing", CategoryName, StageChecker, "C import "+header+" has no automatically imported declaration "+name+"; check the C name, use a handwritten binding, or expose a C wrapper")
}

func MappedCDeclaration(name, mapped string) Message {
	return message("name.c-declaration-mapped", CategoryName, StageChecker, "C declaration "+name+" is imported as "+mapped)
}

func DuplicateExportEntry(key string) Message {
	return message("name.duplicate-export-entry", CategoryName, StageChecker, "export entry "+key+" is listed more than once")
}

func UnknownExportMethod(key string) Message {
	return message("name.unknown-export-method", CategoryName, StageChecker, "unknown method "+key+" in this module")
}

func EntryEnvironmentFunction(name string) Message {
	return message("name.entry-environment-function", CategoryName, StageChecker, "function "+name+" uses the entry environment and is valid only as a direct entry-module call")
}

func ExportedImportAlias(name string) Message {
	return message("name.export-import-alias", CategoryName, StageChecker, "cannot export import alias "+name+"; re-exports are not supported")
}

func UnknownDeclaration(name string) Message {
	return message("name.unknown-declaration", CategoryName, StageChecker, "unknown declaration "+name+" in this module")
}

func DeclarationPrivateToModule(name, module string) Message {
	return message("name.declaration-private-to-module", CategoryName, StageChecker, "declaration "+name+" is private to module "+module)
}
