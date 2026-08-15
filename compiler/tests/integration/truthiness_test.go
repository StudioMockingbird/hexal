package integration

// RFC 0023: truthiness and boolean contexts. false and nil are falsey; every
// other value is truthy. Conditions and the logical operators accept any
// value-producing expression, and the generator renders truthiness without
// runtime overhead.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestTruthinessConditions(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"if 0 then end", "if (((void)(0), true)) {"},
		{"p: Ptr<Int32> | Nil = nil if p then end", "if ((hex_v_p != NULL)) {"},
		{"if true then end", "if (true) {"},
		{"mut count: Int32 = 1 if count then count = count - 1 end", "if (((void)(hex_v_count), true)) {"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
			t.Fatalf("Compile(%q) = %#v, want successful truthiness condition", testCase.source, result)
		}
		if !strings.Contains(rootC(t, result), testCase.want) {
			t.Fatalf("Compile(%q) modules/app.c = %q, want %q", testCase.source, rootC(t, result), testCase.want)
		}
	}
}

func TestTruthinessConditionLoweringPreservesBranches(t *testing.T) {
	// A falsey condition still emits its branches: the checker must have
	// type-checked them, and the generated C keeps them for runtime.
	result := compileSource("p: Ptr<Int32> | Nil = nil if p then missing: Int32 = 1 end")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile = %#v, want successful nil-condition program", result)
	}
	if !strings.Contains(rootC(t, result), "if ((hex_v_p != NULL)) {") || !strings.Contains(rootC(t, result), "const int32_t hex_v_missing = 1;") {
		t.Fatalf("modules/app.c = %q, want the nil branch emitted verbatim", rootC(t, result))
	}
}

func TestNullableTruthinessCondition(t *testing.T) {
	// The if and elseif conditions are the bare nullable binding; deref of
	// maybe would need a narrowing null test, so the branches only touch it
	// through truthiness.
	result := compileSource("mut value: Int32 = 5 mut maybe: Ptr<Int32> | Nil = ref value if maybe then noop: Int32 = 0 elseif maybe then result: Int32 = 1 end")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile = %#v, want a successful nullable truthiness program", result)
	}
	for _, want := range []string{
		"int32_t *hex_v_maybe = &hex_v_value;",
		"if ((hex_v_maybe != NULL)) {",
		"} else if ((hex_v_maybe != NULL)) {",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestTruthinessLogicalOperators(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"mut count: Int32 = 1 mut ready: Bool = true flag: Bool = count and ready", "((void)(hex_v_count), true) && hex_v_ready"},
		{"mut count: Int32 = 1 flag: Bool = count or false", "((void)(hex_v_count), true) || false"},
		{"mut count: Int32 = 1 flag: Bool = !count", "(!((void)(hex_v_count), true))"},
		{"mut value: Float64 = 0.0 flag: Bool = !value", "(!((void)(hex_v_value), true))"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
			t.Fatalf("Compile(%q) = %#v, want a successful truthiness operation", testCase.source, result)
		}
		if !strings.Contains(rootC(t, result), testCase.want) {
			t.Fatalf("Compile(%q) modules/app.c = %q, want %q", testCase.source, rootC(t, result), testCase.want)
		}
	}
}

func TestTruthinessConstantFolding(t *testing.T) {
	result := compileSource("flag: Bool = 1 and 2")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile = %#v, want a folded truthiness constant", result)
	}
	if !strings.Contains(rootC(t, result), "const bool hex_v_flag = true;") {
		t.Fatalf("modules/app.c = %q, want 1 and 2 folded to true", rootC(t, result))
	}

	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"flag: Bool = 0 and false", "const bool hex_v_flag = false;"},
		{"flag: Bool = false or 0", "const bool hex_v_flag = true;"},
		{"flag: Bool = !0", "const bool hex_v_flag = false;"},
		{"flag: Bool = !false", "const bool hex_v_flag = true;"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
			t.Fatalf("Compile(%q) = %#v, want a folded truthiness constant", testCase.source, result)
		}
		if !strings.Contains(rootC(t, result), testCase.want) {
			t.Fatalf("Compile(%q) modules/app.c = %q, want %q", testCase.source, rootC(t, result), testCase.want)
		}
	}
}

func TestTruthinessShortCircuitSkipsFoldedSide(t *testing.T) {
	// 0 is truthy, so the right side of and is evaluated and its division by
	// zero is a static error; false is falsey, so the right side is never
	// evaluated and the same expression is fine.
	result := compileSource("result: Bool = 0 and (1 / 0 == 0)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "division by zero") {
		t.Fatalf("Compile = %#v, want the evaluated side to fail statically", result)
	}

	result = compileSource("result: Bool = false and (1 / 0 == 0)")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile = %#v, want the skipped side to never be checked", result)
	}
}

func TestTruthinessRejectsNoResultCalls(t *testing.T) {
	result := compileSource("fun reset() do\n    return\nend\nif reset() then end\n")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "reset produces no value") {
		t.Fatalf("Compile = %#v, want a no-result condition diagnostic", result)
	}
}
