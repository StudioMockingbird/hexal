package integration

// Handwritten C interoperability end to end through the exported compiler API:
// `extern c` declaration checking, the unsafe gate, direct symbol lowering, and
// foreign header emission.

import (
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

const windowsTarget = compilerTypes.TargetX86_64WindowsGNU

func compileForeign(t *testing.T, source string) compiler.CompilationResult {
	t.Helper()
	return compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{Target: windowsTarget})
}

func TestForeignFunctionCallLowersToExactSymbol(t *testing.T) {
	source := "extern c from <adder.h> do\n" +
		"    fun c_add as \"adder_add\"(left: Int32 as \"int\", right: Int32 as \"int\"): Int32 as \"int\"\n" +
		"end\n" +
		"fun main() do\n" +
		"    unsafe do\n" +
		"        total: Int32 := c_add(1, 2)\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("compile failed: %v", result.Stderr)
	}
	header := result.Files["modules/app.h"]
	if !strings.Contains(header, "#include <adder.h>") {
		t.Fatalf("module header must include the foreign header once:\n%s", header)
	}
	body := result.Files["modules/app.c"]
	if !strings.Contains(body, "adder_add((int)(1), (int)(2))") {
		t.Fatalf("call must lower to the exact C symbol with boundary casts:\n%s", body)
	}
	if strings.Contains(body, "adder_add(") && strings.Contains(body, "hex_fun_adder_add") {
		t.Fatalf("a forwarding wrapper must not be generated:\n%s", body)
	}
}

func TestForeignCallRequiresUnsafe(t *testing.T) {
	source := "extern c from <adder.h> do\n" +
		"    fun c_add as \"adder_add\"(left: Int32, right: Int32): Int32\n" +
		"end\n" +
		"fun main() do\n" +
		"    total: Int32 := c_add(1, 2)\n" +
		"end\n"
	result := compileForeign(t, source)
	assertStderrContains(t, result, "foreign call c_add requires an unsafe do ... end block")
}

func TestForeignDeclarationRequiresQualifiedTarget(t *testing.T) {
	source := "extern c from <adder.h> do\n" +
		"    fun c_add as \"adder_add\"(left: Int32, right: Int32): Int32\n" +
		"end\n" +
		"value: Int32 := 1\n"
	result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "C interoperability requires a qualified target")
}

func TestForeignUnprovableSpellingRejected(t *testing.T) {
	source := "extern c from <adder.h> do\n" +
		"    fun c_add(left: Int32 as \"my_typedef_t\", right: Int32): Int32\n" +
		"end\n" +
		"value: Int32 := 1\n"
	result := compileForeign(t, source)
	assertStderrContains(t, result, "C spelling my_typedef_t cannot be proven in a handwritten binding")
}

func TestForeignFunctionReadsItsCSpelling(t *testing.T) {
	source := "extern c from <adder.h> do\n" +
		"    fun c_add as \"adder_add\"(left: Int32 as \"long\", right: Int32): Int32\n" +
		"end\n" +
		"value: Int32 := 1\n"
	// `long` is 32-bit on the LLP64 Windows target, so the spelling proves.
	if result := compileForeign(t, source); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("long must map to Int32 on the Windows target: %v", result.Stderr)
	}
}

func TestForeignConstantReadsWithoutUnsafe(t *testing.T) {
	source := "extern c from <limits.h> do\n" +
		"    constant int_max as \"INT_MAX\": Int32\n" +
		"end\n" +
		"fun main() do\n" +
		"    highest: Int32 := int_max\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a foreign constant read needs no unsafe region: %v", result.Stderr)
	}
	if body := result.Files["modules/app.c"]; !strings.Contains(body, "INT_MAX") {
		t.Fatalf("constant must lower to its C identifier:\n%s", body)
	}
}

func TestForeignGlobalRequiresUnsafe(t *testing.T) {
	declaration := "extern c from <state.h> do\n" +
		"    global mut counter as \"global_counter\": Int32\n" +
		"end\n"
	withoutUnsafe := declaration + "fun main() do\n    value: Int32 := counter\nend\n"
	assertStderrContains(t, compileForeign(t, withoutUnsafe), "foreign global counter requires an unsafe do ... end block")

	withUnsafe := declaration + "fun main() do\n    unsafe do\n        counter = 5\n        value: Int32 := counter\n    end\nend\n"
	result := compileForeign(t, withUnsafe)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a global access inside unsafe must succeed: %v", result.Stderr)
	}
	body := result.Files["modules/app.c"]
	if !strings.Contains(body, "global_counter = 5") || !strings.Contains(body, "global_counter;") {
		t.Fatalf("a foreign global must lower to its exact C object:\n%s", body)
	}
}

func TestForeignCompleteRecordByValue(t *testing.T) {
	source := "extern c from <vector.h> do\n" +
		"    type Vector2 as \"vector2\" is struct\n" +
		"        mut x: Float32,\n" +
		"        mut y: Float32,\n" +
		"    end\n" +
		"    fun length as \"vector2_length\"(value: Vector2): Float32\n" +
		"end\n" +
		"fun main() do\n" +
		"    mut value: Vector2 := Vector2(x = 1.0, y = 2.0)\n" +
		"    value.x = 3.0\n" +
		"    unsafe do\n" +
		"        magnitude: Float32 := length(value)\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a complete foreign record must construct and pass by value: %v", result.Stderr)
	}
	body := result.Files["modules/app.c"]
	if !strings.Contains(body, "vector2_length(") {
		t.Fatalf("a foreign record must pass by value to the exact symbol:\n%s", body)
	}
	if !strings.Contains(body, "sizeof(vector2)") || !strings.Contains(body, "alignof(vector2)") {
		// size_of and align_of are the C compiler's facts, never copied.
		sizeSource := "extern c from <vector.h> do\n" +
			"    type Vector2 as \"vector2\" is struct\n" +
			"        mut x: Float32,\n" +
			"        mut y: Float32,\n" +
			"    end\n" +
			"end\n" +
			"fun main() do\n" +
			"    size: Size := size_of<Vector2>()\n" +
			"    align: Size := align_of<Vector2>()\n" +
			"end\n"
		sizeResult := compileForeign(t, sizeSource)
		if sizeResult.ExitCode != compiler.ExitSuccess {
			t.Fatalf("size_of and align_of must use C-owned layout: %v", sizeResult.Stderr)
		}
		sizeBody := sizeResult.Files["modules/app.c"]
		if !strings.Contains(sizeBody, "sizeof(vector2)") || !strings.Contains(sizeBody, "alignof(vector2)") {
			t.Fatalf("layout queries must lower to the C type:\n%s", sizeBody)
		}
	}
}

func TestForeignOpaqueRecordOnlyBehindPointer(t *testing.T) {
	source := "extern c from <window.h> do\n" +
		"    type Window as \"struct Window\" is opaque\n" +
		"    fun window_new as \"WindowNew\"(): Ptr<Window>\n" +
		"end\n" +
		"fun main() do\n" +
		"    unsafe do\n" +
		"        window: Ptr<Window> := window_new()\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("an opaque record must succeed behind a pointer: %v", result.Stderr)
	}
	if body := result.Files["modules/app.c"]; !strings.Contains(body, "struct Window *") {
		t.Fatalf("an opaque pointer must spell the exact C tag:\n%s", body)
	}

	byValue := "extern c from <window.h> do\n" +
		"    type Window as \"struct Window\" is opaque\n" +
		"    fun bad(value: Window)\n" +
		"end\n" +
		"value: Int32 := 1\n"
	assertStderrContains(t, compileForeign(t, byValue), "foreign type Window is incomplete in a value position")
}

func TestForeignBytePointerBridge(t *testing.T) {
	source := "extern c from <stdio.h> do\n" +
		"    fun c_puts as \"puts\"(text: Ptr<Byte> | Nil as \"const char *\"): Int32 as \"int\"\n" +
		"end\n" +
		"fun main() do\n" +
		"    unsafe do\n" +
		"        status: Int32 := c_puts(nil)\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a const char * parameter must accept a Byte pointer: %v", result.Stderr)
	}
	body := result.Files["modules/app.c"]
	if !strings.Contains(body, "puts((const char *)(nullptr))") {
		t.Fatalf("the boundary cast must emit the exact C spelling:\n%s", body)
	}
}

func TestForeignVoidPointerMapsThroughUnknown(t *testing.T) {
	source := "extern c from <stdlib.h> do\n" +
		"    fun c_free as \"free\"(memory: Ptr<mut Unknown> | Nil as \"void *\")\n" +
		"end\n" +
		"fun main() do\n" +
		"    unsafe do\n" +
		"        c_free(nil)\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("void * must map through Unknown: %v", result.Stderr)
	}
	if body := result.Files["modules/app.c"]; !strings.Contains(body, "free(") {
		t.Fatalf("the call must lower to the exact symbol:\n%s", body)
	}
}

func TestForeignConflictingCSpellingRejected(t *testing.T) {
	source := "extern c from <adder.h> do\n" +
		"    fun first as \"adder_add\"(left: Int32): Int32\n" +
		"    fun second as \"adder_add\"(left: Int32): Int32\n" +
		"end\n" +
		"value: Int32 := 1\n"
	assertStderrContains(t, compileForeign(t, source), "conflicting foreign declarations for C symbol adder_add")
}

func TestForeignCrossModuleBinding(t *testing.T) {
	binding := "extern c from <adder.h> do\n" +
		"    fun c_add as \"adder_add\"(left: Int32 as \"int\", right: Int32 as \"int\"): Int32 as \"int\"\n" +
		"end\n" +
		"export\n" +
		"    c_add\n" +
		"end\n"
	app := "import\n    Binding from \"./binding\"\nend\n" +
		"fun main() do\n" +
		"    unsafe do\n" +
		"        total: Int32 := Binding.c_add(1, 2)\n" +
		"    end\n" +
		"end\n"
	result := compiler.Compile(map[string]string{"app.hex": app, "binding.hex": binding}, "app.hex", compiler.Project{Target: windowsTarget})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a handwritten binding module must be callable through its alias: %v", result.Stderr)
	}
	if body := result.Files["modules/app.c"]; !strings.Contains(body, "adder_add((int)(1), (int)(2))") {
		t.Fatalf("the importer must lower directly to the C symbol:\n%s", body)
	}
	if header := result.Files["modules/app.h"]; !strings.Contains(header, "#include <adder.h>") {
		t.Fatalf("the importer must include the defining header:\n%s", header)
	}
	if body := result.Files["modules/binding.c"]; strings.Contains(body, "adder_add") {
		t.Fatalf("a binding module must not emit a foreign definition:\n%s", body)
	}
}

func TestStringCPointerBridge(t *testing.T) {
	declaration := "extern c from <stdio.h> do\n" +
		"    fun c_puts as \"puts\"(text: Ptr<Byte> | Nil as \"const char *\"): Int32 as \"int\"\n" +
		"end\n"

	withoutUnsafe := declaration +
		"fun main() do\n" +
		"    text: String := \"hello\"\n" +
		"    raw: Ptr<Byte> := text.c_pointer()\n" +
		"end\n"
	assertStderrContains(t, compileForeign(t, withoutUnsafe), "String.c_pointer requires an unsafe do ... end block")

	withUnsafe := declaration +
		"fun main() do\n" +
		"    text: String := \"hello\"\n" +
		"    unsafe do\n" +
		"        status: Int32 := c_puts(text.c_pointer())\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, withUnsafe)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("String.c_pointer must succeed inside unsafe: %v", result.Stderr)
	}
	if body := result.Files["modules/app.c"]; !strings.Contains(body, "->data") {
		t.Fatalf("String.c_pointer must expose the existing data address:\n%s", body)
	}
}

func TestSlicePointerBridge(t *testing.T) {
	source := "fun main() do\n" +
		"    values: Slice<Int32> := Slice<Int32>.empty()\n" +
		"    unsafe do\n" +
		"        raw: Ptr<Int32> | Nil := values.pointer()\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Slice.pointer must succeed inside unsafe: %v", result.Stderr)
	}
	body := result.Files["modules/app.c"]
	if !strings.Contains(body, ".data") {
		t.Fatalf("Slice.pointer must expose the existing data address:\n%s", body)
	}

	withoutUnsafe := "fun main() do\n" +
		"    values: Slice<Int32> := Slice<Int32>.empty()\n" +
		"    raw: Ptr<Int32> | Nil := values.pointer()\n" +
		"end\n"
	assertStderrContains(t, compileForeign(t, withoutUnsafe), "Slice.pointer requires an unsafe do ... end block")
}

func TestForeignTransparentAlias(t *testing.T) {
	source := "extern c from <trace.h> do\n" +
		"    type TraceLevel is Int32\n" +
		"    fun set_level as \"trace_set_level\"(level: TraceLevel): TraceLevel\n" +
		"end\n" +
		"fun main() do\n" +
		"    unsafe do\n" +
		"        level: TraceLevel := set_level(3)\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a transparent foreign alias must cross the ABI: %v", result.Stderr)
	}
	if body := result.Files["modules/app.c"]; !strings.Contains(body, "trace_set_level(") {
		t.Fatalf("an alias-typed argument must lower to the exact symbol:\n%s", body)
	}
}

func TestForeignRecordSharedAcrossHeaders(t *testing.T) {
	// An opaque declaration and a compatible complete definition of the same C
	// record coalesce to the complete definition.
	source := "extern c from <common.h> do\n" +
		"    type Common as \"struct Common\" is opaque\n" +
		"end\n" +
		"extern c from <common_fields.h> do\n" +
		"    type CommonFull as \"struct Common\" is struct\n" +
		"        mut id: Int32,\n" +
		"    end\n" +
		"end\n" +
		"fun main() do\n" +
		"    mut value: CommonFull := CommonFull(id = 7)\n" +
		"    value.id = 8\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("an opaque and complete declaration must coalesce: %v", result.Stderr)
	}
	if body := result.Files["modules/app.c"]; !strings.Contains(body, "struct Common") {
		t.Fatalf("the shared record must keep the exact C tag:\n%s", body)
	}
}

func TestForeignRecordConflictRejected(t *testing.T) {
	source := "extern c from <a.h> do\n" +
		"    type PointA as \"point\" is struct\n" +
		"        mut x: Int32,\n" +
		"    end\n" +
		"end\n" +
		"extern c from <b.h> do\n" +
		"    type PointB as \"point\" is struct\n" +
		"        mut y: Int32,\n" +
		"    end\n" +
		"end\n" +
		"value: Int32 := 1\n"
	assertStderrContains(t, compileForeign(t, source), "conflicting foreign declarations for C symbol point")
}

func TestForeignNoResultAndRecordOperationRejections(t *testing.T) {
	noResult := "extern c from <x.h> do\n" +
		"    fun reset as \"x_reset\"()\n" +
		"end\n" +
		"fun main() do\n" +
		"    unsafe do\n" +
		"        reset()\n" +
		"    end\n" +
		"end\n"
	if result := compileForeign(t, noResult); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a void C function is a no-result foreign call: %v", result.Stderr)
	}

	record := "extern c from <vector.h> do\n" +
		"    type Vector2 as \"vector2\" is struct\n" +
		"        mut x: Float32,\n" +
		"        mut y: Float32,\n" +
		"    end\n" +
		"end\n"
	equality := record + "fun main() do\n" +
		"    a: Vector2 := Vector2(x = 1.0, y = 2.0)\n" +
		"    b: Vector2 := Vector2(x = 1.0, y = 2.0)\n" +
		"    same: Bool := a == b\n" +
		"end\n"
	assertStderrContains(t, compileForeign(t, equality), "equality is unavailable for foreign record Vector2")

	printing := record + "fun main() do\n" +
		"    value: Vector2 := Vector2(x = 1.0, y = 2.0)\n" +
		"    print(value)\n" +
		"end\n"
	assertStderrContains(t, compileForeign(t, printing), "print does not support foreign record Vector2")
}

func TestForeignMutableBufferBridge(t *testing.T) {
	// A `char *` parameter is checked as a writable Byte pointer and receives a
	// Slice's address; the const form receives the String's address. One
	// boundary cast per position, no general pointer conversion.
	source := "extern c from <string.h> do\n" +
		"    fun c_strncpy as \"strncpy\"(destination: Ptr<mut Byte> | Nil as \"char *\", source: Ptr<Byte> | Nil as \"const char *\", count: Size as \"size_t\"): Ptr<mut Byte> | Nil as \"char *\"\n" +
		"end\n" +
		"fun demo(text: String, buffer: Slice<mut Byte>) do\n" +
		"    unsafe do\n" +
		"        copied: Ptr<mut Byte> | Nil := c_strncpy(buffer.pointer(), text.c_pointer(), text.length())\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a mutable Byte buffer must reach a char * parameter: %v", result.Stderr)
	}
	body := result.Files["modules/app.c"]
	if !strings.Contains(body, "strncpy((char *)(") || !strings.Contains(body, "const char *)") {
		t.Fatalf("the buffer bridge must lower directly with its boundary casts:\n%s", body)
	}
}

func TestForeignVoidPointerFromForeignResult(t *testing.T) {
	source := "extern c from <stdlib.h> do\n" +
		"    fun c_malloc as \"malloc\"(size: Size as \"size_t\"): Ptr<mut Unknown> | Nil as \"void *\"\n" +
		"    fun c_free as \"free\"(memory: Ptr<mut Unknown> | Nil as \"void *\")\n" +
		"end\n" +
		"fun demo() do\n" +
		"    unsafe do\n" +
		"        region: Ptr<mut Unknown> | Nil := c_malloc(16)\n" +
		"        if region != nil then\n" +
		"            c_free(region)\n" +
		"        end\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("void * must round-trip through Unknown: %v", result.Stderr)
	}
	body := result.Files["modules/app.c"]
	if !strings.Contains(body, "malloc(16)") || !strings.Contains(body, "free(hex_v_region)") {
		t.Fatalf("void * calls must lower to the exact symbols:\n%s", body)
	}
}

func TestForeignMutableOutputRejectsReadOnlySource(t *testing.T) {
	// A read-only String pointer can never satisfy a mutable Byte pointer.
	source := "extern c from <string.h> do\n" +
		"    fun c_strncpy as \"strncpy\"(destination: Ptr<mut Byte> | Nil as \"char *\", source: Ptr<Byte> | Nil as \"const char *\", count: Size as \"size_t\"): Ptr<mut Byte> | Nil as \"char *\"\n" +
		"end\n" +
		"fun demo(text: String) do\n" +
		"    unsafe do\n" +
		"        copied: Ptr<mut Byte> | Nil := c_strncpy(text.c_pointer(), text.c_pointer(), text.length())\n" +
		"    end\n" +
		"end\n"
	result := compileForeign(t, source)
	if result.ExitCode == compiler.ExitSuccess {
		t.Fatalf("a read-only String pointer must not satisfy a mutable output parameter")
	}
}
