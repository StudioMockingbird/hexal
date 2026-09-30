package generator

import (
	"fmt"
	"os"
	"runtime/debug"

	diagnostics "hexal/compiler/diagnostics"
	compilerTypes "hexal/compiler/types"
)

func generatorDiagnostic() compilerTypes.Diagnostic {
	fmt.Fprintln(os.Stderr, "=== GENERATOR INVARIANT TRACE ===")
	fmt.Fprint(os.Stderr, string(debug.Stack()))
	fmt.Fprintln(os.Stderr, "=== END ===")
	return compilerTypes.Locationless(diagnostics.GeneratorFailure())
}

func scopeStackUnderflowDiagnostic() error {
	return compilerTypes.Locationless(diagnostics.GeneratorScopeStackUnderflow())
}

func scopeDepthMismatchDiagnostic(owner string) error {
	return compilerTypes.Locationless(diagnostics.GeneratorScopeDepthMismatch(owner))
}
