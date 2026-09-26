package diagnostics

import "strings"

func MovedCoreTypeAsModule(name, module, alias string) Message {
	return message("module.moved-core-type", CategoryModule, StageChecker,
		name+" is declared in "+module+"; add `"+alias+" from "+dottedModule(module)+"` to the import block")
}

func RemovedCoreNamespaceAsModule(name, function, module string) Message {
	return message("module.removed-core-namespace", CategoryModule, StageChecker,
		name+" is removed; "+function+" is a function in "+module)
}

func MovedCoreTypeAsName(name, module, alias string) Message {
	return message("name.moved-core-type", CategoryName, StageChecker,
		name+" is declared in "+module+"; add `"+alias+" from "+dottedModule(module)+"` to the import block")
}

func RemovedCoreNamespaceAsName(name, function, module string) Message {
	return message("name.removed-core-namespace", CategoryName, StageChecker,
		name+" is removed; "+function+" is a function in "+module)
}

func MovedCoreOperation(owner, operation, function, module, alias string) Message {
	return message("name.moved-core-operation", CategoryName, StageChecker,
		owner+"."+operation+" is now "+function+" in "+module+"; add `"+alias+" from "+dottedModule(module)+"` and call `"+alias+"."+function+"`")
}

func MovedCoreConstructor(name, function, module, alias string) Message {
	return message("name.moved-core-constructor", CategoryName, StageChecker,
		name+" is now "+function+" in "+module+"; add `"+alias+" from "+dottedModule(module)+"` and call `"+alias+"."+function+"`")
}

func dottedModule(module string) string {
	return strings.ReplaceAll(module, "/", ".")
}
