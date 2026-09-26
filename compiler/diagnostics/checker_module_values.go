package diagnostics

func ModuleConstantMustBeStatic(name string) Message {
	return message("type.module-constant-must-be-static", CategoryType, StageChecker,
		"module constant "+name+" must be statically initialized")
}

func ModuleConstantCannotContainAtomic(name string) Message {
	return message("type.module-constant-contains-atomic", CategoryType, StageChecker,
		"module constant "+name+" cannot contain mutable Atomic state; pass explicit state instead")
}
