package generator

import (
	"strings"
	"testing"

	"hexal/compiler/specdata"
)

// The lowering must read each built-in method's recorded RuntimeSymbol rather
// than restate it. The program below emits the list push call through the
// registry; corrupting that record's symbol must move the emitted call site.
func TestBuiltinMethodLoweringReadsTheRegistry(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\nend")
	baseline := generateOne(t, program)
	if !strings.Contains(baseline["modules/app.c"], "hex_list_push_Int32(") {
		t.Fatalf("modules/app.c does not call the recorded list push symbol: %q", baseline["modules/app.c"])
	}

	original := builtinMethodRecord
	builtinMethodRecord = func(owner specdata.TypePattern, name string) (specdata.MethodSpec, bool) {
		if name == "push" {
			return specdata.MethodSpec{Owner: owner, Name: name, RuntimeSymbol: "hex_probe_push_%s"}, true
		}
		return original(owner, name)
	}
	defer func() { builtinMethodRecord = original }()

	corrupted := generateOne(t, program)
	if !strings.Contains(corrupted["modules/app.c"], "hex_probe_push_Int32(") {
		t.Fatalf("modules/app.c ignored the corrupted record: %q", corrupted["modules/app.c"])
	}
	if strings.Contains(corrupted["modules/app.c"], "hex_list_push_Int32(") {
		t.Fatalf("modules/app.c kept the old symbol after the record changed: %q", corrupted["modules/app.c"])
	}
}
