package generator

import (
	diagnostics "hexal/compiler/diagnostics"
	compilerTypes "hexal/compiler/types"
)

func generatorDiagnostic() compilerTypes.Diagnostic {
	return compilerTypes.Locationless(diagnostics.GeneratorFailure())
}

func scopeStackUnderflowDiagnostic() error {
	return compilerTypes.Locationless(diagnostics.GeneratorScopeStackUnderflow())
}

func scopeDepthMismatchDiagnostic(owner string) error {
	return compilerTypes.Locationless(diagnostics.GeneratorScopeDepthMismatch(owner))
}
