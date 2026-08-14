package tests

// RFC 0034 Task 5: visibility, qualification, and exported-interface
// closure, end to end through Compile. The Task 4 gate still rejects clean
// multi-module programs, so a resolved qualified use is asserted as "the
// gate and nothing else" — user-program diagnostics never accompany it;
// Task 7 removes the gate and these become plain successes.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// wantGateOnly asserts the multi-module gate fired alone, which proves the
// program resolved and checked clean.
func wantGateOnly(t *testing.T, result compiler.CompilationResult) {
	t.Helper()
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 {
		t.Fatalf("stderr = %#v, want only the gate diagnostic", result.Stderr)
	}
	if !strings.Contains(result.Stderr[0], "multi-module compilation is not yet implemented") {
		t.Fatalf("stderr = %#v, want the gate diagnostic", result.Stderr)
	}
}

func TestQualifiedCallToExportedFunctionResolves(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n",
		"math.hex": "export fun add(x: Int32, y: Int32): Int32\n    return x + y\nend\n",
	}
	wantGateOnly(t, compileMulti(sources, "app.hex"))
}

func TestQualifiedCallToPrivateFunctionRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32\n    return x + y\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "declaration add is private to module math")
}

func TestUnqualifiedUseOfExportedNameRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = add(2, 3)\n",
		"math.hex": "export fun add(x: Int32, y: Int32): Int32\n    return x + y\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "unknown function add; functions must be declared before use")
}

func TestQualifiedTypeResolvesThroughAlias(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nshape: Math.Shape = 0\n",
		"math.hex": "export type Shape = Int32\n",
	}
	wantGateOnly(t, compileMulti(sources, "app.hex"))
}

func TestQualifiedVariantResolvesExportedADT(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\ns: Math.Shape = Math.Circle { x = 1 }\n",
		"math.hex": "export type Shape = | Circle as { x: Int32 } | Square\n",
	}
	wantGateOnly(t, compileMulti(sources, "app.hex"))
}

func TestPrivateTypeInExportedSignatureRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "type Secret = { x: Int32 }\nexport fun f(): Secret\n    return Secret { x = 1 }\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "exported function f exposes private type Secret")
}

func TestExportedMethodRequiresExportedReceiver(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "type Point = { x: Int32 }\nexport impl Point.getX(): Int32\n    return self.x\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "exported function getX exposes private type Point")
}

func TestPrivateTypeBehindNestedContainersRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module Math = import \"./math\"\n",
		// Secret is declared before Node (source order); Node's exported
		// interface reaches the private Secret through List and Ptr, so the
		// closure walk reports Node first.
		"math.hex": "type Secret = { x: Int32 }\nexport type Node = { items: List<Secret>, next: MutPtr<Node> | Nil }\nexport fun f(): Node\n    return Node { items = List<Secret>.new(Heap.new()), next = nil }\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "exported function Node exposes private type Secret")
}

func TestPrivateTypeInsideExportedGenericBodyAccepted(t *testing.T) {
	// The closure walks interfaces only: a private helper type used inside
	// an exported generic's body is fine.
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "type Secret = { x: Int32 }\nexport fun wrap<T>(value: T): T\n    secret: Secret = Secret { x = 1 }\n    return value\nend\n",
	}
	wantGateOnly(t, compileMulti(sources, "app.hex"))
}

func TestExportOnValueBindingRejected(t *testing.T) {
	result := compileMulti(map[string]string{"app.hex": "export x: Int32 = 1\n"}, "app.hex")
	wantStderr(t, result, "export may prefix only a module-level type, function, or implementation declaration")
}
