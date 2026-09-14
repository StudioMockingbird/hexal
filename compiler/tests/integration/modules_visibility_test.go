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

// A direct inferred fixed function-literal declaration is declaration sugar,
// not executable data, so an imported declaration-only module may contain
// one. It stays private: export prefixes only the named function-declaration
// form, never this spelling.
func TestDeclarationOnlyModuleAcceptsDirectFunctionLiteral(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Lib from \"./lib\"\nend\nresult: Int32 := Lib.add(2, 3)\n",
		"lib.hex": "add := fun (x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatal("unexported declaration sugar must stay private, like the equivalent named form")
	}
	for _, message := range result.Stderr {
		if strings.Contains(message, "executable statements") {
			t.Fatalf("declaration sugar misclassified as an executable statement: %v", result.Stderr)
		}
	}
}

func TestQualifiedCallToExportedFunctionResolves(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\nresult: Int32 := Math.add(2, 3)\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\nexport\n    add\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

// A qualified use through an alias of a nested (directory) import resolves
// to the full canonical module identity, not the basename: "graphics/shapes"
// is one identity.
func TestQualifiedUseThroughNestedPathAlias(t *testing.T) {
	sources := map[string]string{
		"app.hex":             "import\n    Shapes from \"./graphics/shapes\"\nend\np: Shapes.Point := Shapes.origin()\n",
		"graphics/shapes.hex": "type Point is struct x: Int32, y: Int32 end\nfun origin(): Point do\n    return Point(x = 0, y = 0)\nend\nexport\n    Point,\n    origin\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "graphics/shapes")
}

// Same-basename modules in different directories are distinct canonical
// identities: both aliases resolve, the qualified calls hit their own
// module's declarations, and the generated symbols stay distinct.
func TestSameBasenameModulesAreDistinct(t *testing.T) {
	sources := map[string]string{
		"app.hex":             "import\n    Graphics from \"./graphics/shapes\"\n,\n    Audio from \"./audio/shapes\"\nend\ng: Graphics.Shape := Graphics.make()\na: Audio.Shape := Audio.make()\n",
		"graphics/shapes.hex": "type Shape is struct kind: Int32 end\nfun make(): Shape do\n    return Shape(kind = 1)\nend\nexport\n    Shape,\n    make\nend\n",
		"audio/shapes.hex":    "type Shape is struct kind: Int32 end\nfun make(): Shape do\n    return Shape(kind = 2)\nend\nexport\n    Shape,\n    make\nend\n",
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
		"app.hex":              "import\n    Apps from \"./apps/tools\"\nend\nresult: Int32 := Apps.value()\n",
		"apps/tools.hex":       "import\n    Shared from \"../shared/constants\"\nend\nfun value(): Int32 do\n    return Shared.answer()\nend\nexport\n    value\nend\n",
		"shared/constants.hex": "fun answer(): Int32 do\n    return 42\nend\nexport\n    answer\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "apps/tools", "shared/constants")
}

func TestQualifiedCallToPrivateFunctionRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\nresult: Int32 := Math.add(2, 3)\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "declaration add is private to module math")
}

func TestUnqualifiedUseOfExportedNameRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\nresult: Int32 := add(2, 3)\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\nexport\n    add\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "unknown function add; functions must be declared before use")
}

func TestQualifiedTypeResolvesThroughAlias(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\nshape: Math.Shape := 0\n",
		"math.hex": "type Shape is Int32\nexport\n    Shape\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

func TestQualifiedVariantResolvesExportedADT(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\ns: Math.Shape := Math.Circle(x = 1)\n",
		"math.hex": "type Shape is union | Circle as x: Int32 end | Square end\nexport\n    Shape\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

func TestPrivateTypeInExportedSignatureRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\n",
		"math.hex": "type Secret is struct x: Int32 end\nfun f(): Secret do\n    return Secret(x = 1)\nend\nexport\n    f\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "exported function f exposes private type Secret")
}

func TestExportedMethodRequiresExportedReceiver(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\n",
		"math.hex": "type Point is struct x: Int32 end\nmethod Point.getX(): Int32 do\n    return self.x\nend\nexport\n    Point.getX\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "exported function getX exposes private type Point")
}

func TestPrivateTypeBehindNestedContainersRejected(t *testing.T) {
	sources := map[string]string{
		"app.hex": "import\n    Math from \"./math\"\nend\n",
		// Secret is declared before Node (source order); Node's exported
		// interface reaches the private Secret through List and Ptr, so the
		// closure walk reports Node first.
		"math.hex": "type Secret is struct x: Int32 end\ntype Node is struct items: List<Secret>, next: Ptr<mut Node> | Nil end\nfun f(): Node do\n    return Node(items = List<Secret>(Heap()), next = nil)\nend\nexport\n    Node,\n    f\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "exported function Node exposes private type Secret")
}

func TestPrivateTypeInsideExportedGenericBodyAccepted(t *testing.T) {
	// The closure walks interfaces only: a private helper type used inside
	// an exported generic's body is fine.
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\n",
		"math.hex": "type Secret is struct x: Int32 end\nfun wrap<T>(value: T): T do\n    secret: Secret := Secret(x = 1)\n    return value\nend\nexport\n    wrap\nend\n",
	}
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

// A local (non-static) root value is not exportable: it is an executable
// entrypoint local, not module storage, and the export block's own name
// resolution rejects it as unknown.
func TestExportOnValueBindingRejected(t *testing.T) {
	result := compiler.Compile(map[string]string{"app.hex": "x: Int32 := 1\nexport\n    x\nend\n"}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "unknown declaration x in this module")
}

// A private type must not pass as exported just because an unrelated module
// happens to export a same-named type: the export-flag name lookup is scoped
// to the private type's defining module, so an unrelated module's export set
// says nothing about it.
func TestPrivateTypeNotMadeExportedByUnrelatedModule(t *testing.T) {
	aPrivate := "type Secret is struct x: Int32 end\nfun wrap(): Secret do\n    return Secret(x = 1)\nend\nexport\n    wrap\nend\n"
	bExportsSameName := "type Secret is struct y: Int32 end\nexport\n    Secret\nend\n"
	cases := []struct {
		name string
		a    string
		b    string
	}{
		{"private in a, unrelated public in b", aPrivate, bExportsSameName},
		{"public in a, unrelated private exposed by b", "type Secret is struct x: Int32 end\nexport\n    Secret\nend\n", "type Secret is struct y: Int32 end\nfun wrap(): Secret do\n    return Secret(y = 2)\nend\nexport\n    wrap\nend\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sources := map[string]string{
				"app.hex": "import\n    A from \"./a\"\n,\n    B from \"./b\"\nend\n",
				"a.hex":   testCase.a,
				"b.hex":   testCase.b,
			}
			result := compiler.Compile(sources, "app.hex", compiler.Project{})
			assertStderrContains(t, result, "exposes private type Secret")
		})
	}
	// Single-module control: the same private type alone is still rejected, so
	// the unrelated module is not what changed the outcome.
	result := compiler.Compile(map[string]string{"app.hex": "import\n    A from \"./a\"\nend\nvalue: Int32 := 1\n", "a.hex": aPrivate}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "exposes private type Secret")
}
