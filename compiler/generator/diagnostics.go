package generator

import (
	diagnostics "hexal/compiler/diagnostics"
	compilerTypes "hexal/compiler/types"
)

func generatorDiagnostic() compilerTypes.Diagnostic {
	return compilerTypes.Locationless(diagnostics.GeneratorFailure())
}
