package compiler

// RFC 0023: truthiness and boolean contexts. false and nil are falsey; every
// other value is truthy. Conditions and the logical operators accept any
// value-producing expression, and the generator renders truthiness without
// runtime overhead.

import (
	"strings"
	"testing"
)

func TestTruthinessConditions(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"if 0 end", "if ((0, true)) {"},
		{"if nil end", "if (false) {"},
		{"if true end", "if (true) {"},
		{"mut count: Int32 = 1 if count count = count - 1 end", "if ((sw_v_count, true)) {"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
			t.Fatalf("Compile(%q) = %#v, want successful truthiness condition", testCase.source, result)
		}
		if !strings.Contains(result.MainC, testCase.want) {
			t.Fatalf("Compile(%q) main.c = %q, want %q", testCase.source, result.MainC, testCase.want)
		}
	}
}

func TestTruthinessConditionLoweringPreservesBranches(t *testing.T) {
	// A falsey condition still emits its branches: the checker must have
	// type-checked them, and the generated C keeps them for runtime.
	result := Compile("if nil missing: Int32 = 1 end")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile = %#v, want successful nil-condition program", result)
	}
	if !strings.Contains(result.MainC, "if (false) {") || !strings.Contains(result.MainC, "const int32_t sw_v_missing = 1;") {
		t.Fatalf("main.c = %q, want the nil branch emitted verbatim", result.MainC)
	}
}

func TestNullableTruthinessCondition(t *testing.T) {
	// The if and elseif conditions are the bare nullable binding; deref of
	// maybe would need a narrowing null test, so the branches only touch it
	// through truthiness.
	result := Compile("mut value: Int32 = 5 mut maybe: Ptr<Int32> | Nil = ref value if maybe noop: Int32 = 0 elseif maybe result: Int32 = 1 end")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile = %#v, want a successful nullable truthiness program", result)
	}
	for _, want := range []string{
		"int32_t *sw_v_maybe = &sw_v_value;",
		"if ((sw_v_maybe != NULL)) {",
		"} else if ((sw_v_maybe != NULL)) {",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestTruthinessLogicalOperators(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"mut count: Int32 = 1 mut ready: Bool = true flag: Bool = count and ready", "((sw_v_count, true) && sw_v_ready)"},
		{"mut count: Int32 = 1 flag: Bool = count or nil", "((sw_v_count, true) || false)"},
		{"mut count: Int32 = 1 flag: Bool = !count", "(!(sw_v_count, true))"},
		{"mut value: Float64 = 0.0 flag: Bool = !value", "(!(sw_v_value, true))"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
			t.Fatalf("Compile(%q) = %#v, want a successful truthiness operation", testCase.source, result)
		}
		if !strings.Contains(result.MainC, testCase.want) {
			t.Fatalf("Compile(%q) main.c = %q, want %q", testCase.source, result.MainC, testCase.want)
		}
	}
}

func TestTruthinessConstantFolding(t *testing.T) {
	result := Compile("flag: Bool = 1 and 2")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile = %#v, want a folded truthiness constant", result)
	}
	if !strings.Contains(result.MainC, "const bool sw_v_flag = true;") {
		t.Fatalf("main.c = %q, want 1 and 2 folded to true", result.MainC)
	}

	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"flag: Bool = 0 and nil", "const bool sw_v_flag = false;"},
		{"flag: Bool = nil or 0", "const bool sw_v_flag = true;"},
		{"flag: Bool = !0", "const bool sw_v_flag = false;"},
		{"flag: Bool = !nil", "const bool sw_v_flag = true;"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
			t.Fatalf("Compile(%q) = %#v, want a folded truthiness constant", testCase.source, result)
		}
		if !strings.Contains(result.MainC, testCase.want) {
			t.Fatalf("Compile(%q) main.c = %q, want %q", testCase.source, result.MainC, testCase.want)
		}
	}
}

func TestTruthinessShortCircuitSkipsFoldedSide(t *testing.T) {
	// 0 is truthy, so the right side of and is evaluated and its division by
	// zero is a static error; nil is falsey, so the right side is never
	// evaluated and the same expression is fine.
	result := Compile("result: Bool = 0 and (1 / 0 == 0)")
	if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "division by zero") {
		t.Fatalf("Compile = %#v, want the evaluated side to fail statically", result)
	}

	result = Compile("result: Bool = nil and (1 / 0 == 0)")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile = %#v, want the skipped side to never be checked", result)
	}
}

func TestTruthinessRejectsNoResultCalls(t *testing.T) {
	result := Compile("fun reset()\n    return\nend\nif reset() end\n")
	if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "reset produces no value") {
		t.Fatalf("Compile = %#v, want a no-result condition diagnostic", result)
	}
}
