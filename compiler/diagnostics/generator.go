package diagnostics

func GeneratorScopeStackUnderflow() Message {
	return message("internal.generator-scope-underflow", CategoryUnknown, StageGenerator,
		"generator scope stack has no nested scope to remove")
}

func GeneratorScopeDepthMismatch(owner string) Message {
	return message("internal.generator-scope-depth-mismatch", CategoryUnknown, StageGenerator,
		"generator scope depth mismatch after "+owner)
}
