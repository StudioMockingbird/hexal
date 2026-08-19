package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// wantMultiSuccess asserts a clean multi-module program compiles: no
// diagnostics, and exactly hexal.h plus one C/header pair per reachable
// module.
func assertMultiModuleSuccess(t *testing.T, result compiler.CompilationResult, modules ...string) {
	t.Helper()
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("stderr = %#v, want success with no diagnostics", result.Stderr)
	}
	wantKeys := map[string]bool{"hexal.h": true}
	for _, module := range modules {
		wantKeys["modules/"+module+".c"] = true
		wantKeys["modules/"+module+".h"] = true
	}
	for key := range result.Files {
		if !wantKeys[key] {
			// The demand-driven component artifacts under hexal/
			// are legitimate additions selected by the reachable families;
			// every other key must be one of the mandatory artifacts.
			if strings.HasPrefix(key, "hexal/") {
				continue
			}
			t.Fatalf("Files contains unexpected key %q", key)
		}
	}
	for key := range wantKeys {
		if _, exists := result.Files[key]; !exists {
			t.Fatalf("Files lacks mandatory key %q", key)
		}
	}
}

func TestQualifiedCallToExportedFunctionResolves(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 := Math.add(2, 3)\n",
		"math.hex": "export fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

// A qualified use through an alias of a nested (directory) import resolves
// to the full canonical module identity, not the basename: "graphics/shapes"
// is one identity.
func TestQualifiedUseThroughNestedPathAlias(t *testing.T) {
	sources := map[string]string{
		"app.hex":             "module Shapes = import \"./graphics/shapes\"\np: Shapes.Point := Shapes.origin()\n",
		"graphics/shapes.hex": "export type Point = { x: Int32, y: Int32 }\nexport fun origin(): Point do\n    return Point { x = 0, y = 0 }\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "graphics/shapes")
}

// Same-basename modules in different directories are distinct canonical
// identities: both aliases resolve, the qualified calls hit their own
// module's declarations, and the generated symbols stay distinct.
func TestSameBasenameModulesAreDistinct(t *testing.T) {
	sources := map[string]string{
		"app.hex":             "module Graphics = import \"./graphics/shapes\"\nmodule Audio = import \"./audio/shapes\"\ng: Graphics.Shape := Graphics.make()\na: Audio.Shape := Audio.make()\n",
		"graphics/shapes.hex": "export type Shape = { kind: Int32 }\nexport fun make(): Shape do\n    return Shape { kind = 1 }\nend\n",
		"audio/shapes.hex":    "export type Shape = { kind: Int32 }\nexport fun make(): Shape do\n    return Shape { kind = 2 }\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "graphics/shapes", "audio/shapes")
	appC := result.Files["modules/app.c"]
	if !strings.Contains(appC, "hex_f_m8_graphics6_shapes_make") {
		t.Fatalf("app.c lacks the graphics symbol:\n%s", appC)
	}
	if !strings.Contains(appC, "hex_f_m5_audio6_shapes_make") {
		t.Fatalf("app.c lacks the audio symbol:\n%s", appC)
	}
}

// A parent-relative import from a nested module resolves and its qualified
// uses compile.
func TestQualifiedUseThroughParentRelativeImport(t *testing.T) {
	sources := map[string]string{
		"app.hex":              "module Apps = import \"./apps/tools\"\nresult: Int32 := Apps.value()\n",
		"apps/tools.hex":       "module Shared = import \"../shared/constants\"\nexport fun value(): Int32 do\n    return Shared.answer()\nend\n",
		"shared/constants.hex": "export fun answer(): Int32 do\n    return 42\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "apps/tools", "shared/constants")
}

func TestQualifiedCallToPrivateFunctionRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 := Math.add(2, 3)\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "declaration add is private to module math")
}

func TestUnqualifiedUseOfExportedNameRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 := add(2, 3)\n",
		"math.hex": "export fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "unknown function add; functions must be declared before use")
}

func TestQualifiedTypeResolvesThroughAlias(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nshape: Math.Shape := 0\n",
		"math.hex": "export type Shape = Int32\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

func TestQualifiedVariantResolvesExportedADT(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\ns: Math.Shape := Math.Circle { x = 1 }\n",
		"math.hex": "export type Shape = | Circle as { x: Int32 } | Square\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

func TestPrivateTypeInExportedSignatureRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "type Secret = { x: Int32 }\nexport fun f(): Secret do\n    return Secret { x = 1 }\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "exported function f exposes private type Secret")
}

func TestExportedMethodRequiresExportedReceiver(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "type Point = { x: Int32 }\nexport impl Point.getX(): Int32 do\n    return self.x\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "exported function getX exposes private type Point")
}

func TestPrivateTypeBehindNestedContainersRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module Math = import \"./math\"\n",
		// Secret is declared before Node (source order); Node's exported
		// interface reaches the private Secret through List and Ptr, so the
		// closure walk reports Node first.
		"math.hex": "type Secret = { x: Int32 }\nexport type Node = { items: List<Secret>, next: MutPtr<Node> | Nil }\nexport fun f(): Node do\n    return Node { items = List<Secret>.new(Heap.new()), next = nil }\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "exported function Node exposes private type Secret")
}

func TestPrivateTypeInsideExportedGenericBodyAccepted(t *testing.T) {
	// The closure walks interfaces only: a private helper type used inside
	// an exported generic's body is fine.
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "type Secret = { x: Int32 }\nexport fun wrap<T>(value: T): T do\n    secret: Secret := Secret { x = 1 }\n    return value\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

func TestExportOnValueBindingRejected(t *testing.T) {
	result := compiler.Compile(map[string]string{"app.hex": "export x: Int32 := 1\n"}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "export may prefix only a module-level type, function, or implementation declaration")
}

// A private type must not pass as exported just because an unrelated module
// happens to export a same-named type: the export-flag name lookup is scoped
// to the private type's defining module, so an unrelated module's export set
// says nothing about it.
func TestPrivateTypeNotMadeExportedByUnrelatedModule(t *testing.T) {
	aPrivate := "type Secret = { x: Int32 }\nexport fun wrap(): Secret do\n    return Secret { x = 1 }\nend\n"
	bExportsSameName := "export type Secret = { y: Int32 }\n"
	cases := []struct {
		name string
		a    string
		b    string
	}{
		{"private in a, unrelated public in b", aPrivate, bExportsSameName},
		{"public in a, unrelated private exposed by b", "export type Secret = { x: Int32 }\n", "type Secret = { y: Int32 }\nexport fun wrap(): Secret do\n    return Secret { y = 2 }\nend\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sources := map[string]string{
				"app.hex": "module A = import \"./a\"\nmodule B = import \"./b\"\n",
				"a.hex":   testCase.a,
				"b.hex":   testCase.b,
			}
			result := compiler.Compile(sources, "app.hex", compiler.Project{})
			assertStderrContains(t, result, "exposes private type Secret")
		})
	}
	// Single-module control: the same private type alone is still rejected, so
	// the unrelated module is not what changed the outcome.
	result := compiler.Compile(map[string]string{"app.hex": "module A = import \"./a\"\nvalue: Int32 := 1\n", "a.hex": aPrivate}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "exposes private type Secret")
}
