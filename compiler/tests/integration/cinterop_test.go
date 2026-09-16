package integration

// C interoperability's compiler-owned half: `from c` imports, the pure
// reachable-request discovery helper, the deterministic reserved binding key,
// and the Configuration Errors that gate prepared bindings.

import (
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

func TestCImportRequiresQualifiedTarget(t *testing.T) {
	source := "import\n    Adder from c \"adder.h\"\nend\nvalue: Int32 := 1\n"
	result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "[Configuration Error] C interoperability requires a qualified target")
}

func TestCImportPreparedBindingMissing(t *testing.T) {
	source := "import\n    Adder from c \"adder.h\"\nend\nvalue: Int32 := 1\n"
	result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	assertStderrContains(t, result, `prepared C binding missing for "adder.h"`)
}

func TestCImportPreparedBindingResolves(t *testing.T) {
	source := "import\n    Adder from c \"adder.h\"\nend\nvalue: Int32 := 1\n"
	key := compiler.CBindingKey(string(compilerTypes.TargetX86_64WindowsGNU), compiler.CImportRequest{Header: "adder.h"})
	// A prepared binding always names the header its key was derived from,
	// even when it exposes no declaration.
	binding := "extern c from \"adder.h\" do\nend\n"
	result := compiler.Compile(map[string]string{"app.hex": source, key: binding}, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a present prepared binding must resolve: %v", result.Stderr)
	}
}

func TestCImportMissingDeclarationGuidance(t *testing.T) {
	source := "import\n    Adder from c \"adder.h\"\nend\nfun main() do\n    value: Int32 := Adder.missing_call()\nend\n"
	key := compiler.CBindingKey(string(compilerTypes.TargetX86_64WindowsGNU), compiler.CImportRequest{Header: "adder.h"})
	binding := "extern c from \"adder.h\" do\n    fun add(left: Int32, right: Int32): Int32\nend\n"
	result := compiler.Compile(map[string]string{"app.hex": source, key: binding}, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	assertStderrContains(t, result, `C import "adder.h" has no automatically imported declaration missing_call; check the C name, use a handwritten binding, or expose a C wrapper`)
}

func TestCImportPreparedBindingHeaderMismatch(t *testing.T) {
	source := "import\n    Adder from c \"adder.h\"\nend\nvalue: Int32 := 1\n"
	key := compiler.CBindingKey(string(compilerTypes.TargetX86_64WindowsGNU), compiler.CImportRequest{Header: "adder.h"})
	// A prepared binding whose key claims `adder.h` but which declares a
	// different header is a mismatch, not a silent alias.
	binding := "extern c from <other.h> do\nend\n"
	result := compiler.Compile(map[string]string{"app.hex": source, key: binding}, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	assertStderrContains(t, result, `prepared C binding missing for "adder.h"`)
}

// A foreign declaration whose type has no supported C ABI mapping fails closed
// rather than emitting a guessed contract.
func TestExternBlockFailsClosed(t *testing.T) {
	source := "extern c from <adder.h> do\n    fun add(left: Array<Int32, 4>): Int32\nend\nvalue: Int32 := 1\n"
	result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	assertStderrContains(t, result, "has no supported C ABI mapping for target x86_64-windows-gnu")
}

func TestDiscoverCImports(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Adder from c \"adder.h\",\n    Std from c <stdio.h>\nend\nvalue: Int32 := 1\n",
		"junk.hex": "import\n    Lost from c \"lost.h\"\nend\n",
	}
	requests, err := compiler.DiscoverCImports(sources, "app.hex")
	if err != nil {
		t.Fatalf("DiscoverCImports error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %v, want two (unreachable junk.hex ignored)", requests)
	}
	if requests[0] != (compiler.CImportRequest{Header: "adder.h"}) || requests[1] != (compiler.CImportRequest{Header: "stdio.h", System: true}) {
		t.Fatalf("requests = %v, want [quoted adder.h, system stdio.h]", requests)
	}
	if _, err := compiler.DiscoverCImports(map[string]string{"app.hex": "import\n    A from c \"x.h\"\n,\n    B from c \"x.h\"\nend\n"}, "app.hex"); err != nil {
		t.Fatalf("equal request discovery error = %v", err)
	}
}

func TestCBindingKeyIdentity(t *testing.T) {
	target := string(compilerTypes.TargetX86_64WindowsGNU)
	quoted := compiler.CBindingKey(target, compiler.CImportRequest{Header: "adder.h"})
	system := compiler.CBindingKey(target, compiler.CImportRequest{Header: "adder.h", System: true})
	other := compiler.CBindingKey(target, compiler.CImportRequest{Header: "other.h"})
	otherTarget := compiler.CBindingKey("x86_64-other", compiler.CImportRequest{Header: "adder.h"})
	if quoted == system || quoted == other || quoted == otherTarget {
		t.Fatalf("distinct header identities must derive distinct keys: %q %q %q %q", quoted, system, other, otherTarget)
	}
	if compiler.CBindingKey(target, compiler.CImportRequest{Header: "adder.h"}) != quoted {
		t.Fatal("key derivation must be deterministic")
	}
	// The leading `h` keeps the digest component a legal Hexal identifier even
	// when the digest begins with a decimal digit.
	if len(quoted) < 3 || quoted[:8] != "hexalc/h" {
		t.Fatalf("key = %q, want the hexalc/h prefix", quoted)
	}
}

func TestUserHexalcKeyRejected(t *testing.T) {
	result := compiler.Compile(map[string]string{
		"app.hex":         "import\n    H from \"./hexalc/habc\"\nend\nvalue: Int32 := 1\n",
		"hexalc/habc.hex": "value: Int32 := 1\nexport\n    value\nend\n",
	}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, `the "hexalc" path prefix is reserved for prepared C bindings`)
}

func TestCHeaderFormsParse(t *testing.T) {
	for _, source := range []string{
		"import\n    Adder from c \"adder.h\"\nend\nvalue: Int32 := 1\n",
		"import\n    Std from c <stdio.h>\nend\nvalue: Int32 := 1\n",
		"import\n    Nested from c <vendor/widget.h>\nend\nvalue: Int32 := 1\n",
	} {
		if _, err := compiler.DiscoverCImports(map[string]string{"app.hex": source}, "app.hex"); err != nil {
			t.Errorf("DiscoverCImports(%q) error = %v, want no error", source, err)
		}
	}
	for _, testCase := range []struct{ header, want string }{
		{"c \"\"", "invalid C header name"},
		{"c \"/abs.h\"", "invalid C header name"},
		{"c \"../up.h\"", "invalid C header name"},
		{"c <../up.h>", "invalid C header name"},
	} {
		source := "import\n    H from " + testCase.header + "\nend\nvalue: Int32 := 1\n"
		result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{})
		assertStderrContains(t, result, testCase.want)
	}
}
