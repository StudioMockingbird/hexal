package integration

// Automatic C-import name resolution: the guidance diagnostic for a missing
// member through a direct C-import alias, and the mapped-name diagnostic for
// one the importer escaped. Both are pure over prepared source strings; no
// frontend runs.

import (
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

func TestAutomaticImportMissingDeclarationGuidance(t *testing.T) {
	target := compilerTypes.TargetX86_64WindowsGNU
	key := compiler.CBindingKey(string(target), compiler.CImportRequest{Header: "adder.h"})
	binding := "extern c from \"adder.h\" do\n" +
		"    fun hex_cvar___debugbreak as \"__debugbreak\"()\n" +
		"    fun hex_cvar_Int32 as \"Int32\"()\n" +
		"end\n" +
		"export\n    hex_cvar___debugbreak,\n    hex_cvar_Int32\nend\n"

	missing := "import\n    C from c \"adder.h\"\nend\n" +
		"fun main() do\n    unsafe do\n        C.not_present()\n    end\nend\n"
	result := compiler.Compile(map[string]string{"app.hex": missing, key: binding}, "app.hex", compiler.Project{Target: target})
	assertStderrContains(t, result, `C import "adder.h" has no automatically imported declaration not_present; check the C name, use a handwritten binding, or expose a C wrapper`)

	mapped := "import\n    C from c \"adder.h\"\nend\n" +
		"fun main() do\n    unsafe do\n        C.Int32()\n    end\nend\n"
	result = compiler.Compile(map[string]string{"app.hex": mapped, key: binding}, "app.hex", compiler.Project{Target: target})
	assertStderrContains(t, result, "C declaration Int32 is imported as hex_cvar_Int32")
}

func TestOrdinaryModuleKeepsUnknownExportDiagnostic(t *testing.T) {
	sources := map[string]string{
		"app.hex":    "import\n    Helper from \"./helper\"\nend\nfun main() do\n    Helper.not_present()\nend\n",
		"helper.hex": "fun present(): Int32 do\n    return 1\nend\nexport\n    present\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("an unknown member of an ordinary module must fail")
	}
	joined := strings.Join(result.Stderr, "\n")
	if !strings.Contains(joined, "not_present") {
		t.Fatalf("ordinary unknown-export diagnostic missing: %v", result.Stderr)
	}
	if strings.Contains(joined, "has no automatically imported declaration") {
		t.Fatalf("an ordinary module must not receive C-import guidance: %v", result.Stderr)
	}
}
