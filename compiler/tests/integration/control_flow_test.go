package integration

// Structured conditionals, loops, lexical scopes, and flow-sensitive returns.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestConditionalAndLoopLowering(t *testing.T) {
	result := compileSource("mut count: Int32 := 0 mut ready: Bool := true if ready then mut local: Int32 := 1 count = count + local elseif false then count = 9 else count = 8 end while count < 3 do count = count + 1 if count == 2 then continue end if count > 2 then break end end")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("control-flow compilation failed: %#v", result)
	}
	for _, want := range []string{
		"if (hex_v_ready) {",
		"} else if (false) {",
		"} else {",
		"while (hex_v_count < 3) {",
		"continue;",
		"break;",
		"hex_v_count = hex_wrap_add_int32_t(",
		"hex_v_local",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestControlFlowScopesAndShadowing(t *testing.T) {
	result := compileSource("mut value: Int32 := 0 if true then value = 1 else value: Int32 := 2 end if false then value: Int32 := 3 end")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("scoped declarations failed: %#v", result)
	}
	if !strings.Contains(rootC(t, result), "hex_v_value_2") || !strings.Contains(rootC(t, result), "hex_v_value_3") {
		t.Fatalf("shadowed C names are not deterministic: %q", rootC(t, result))
	}

	invalid := compileSource("if true then local: Int32 := 1 end read: Int32 := local")
	if invalid.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(invalid.Stderr, "\n"), "unknown variable local") {
		t.Fatalf("out-of-scope binding = %#v", invalid.Stderr)
	}
}

func TestControlFlowDefiniteReturns(t *testing.T) {
	valid := compileSource("fun classify(value: Int32): Int32 do if value > 0 then return 1 elseif value == 0 then return 0 else return -1 end end result: Int32 := classify(1)")
	if valid.ExitCode != compiler.ExitSuccess || len(valid.Stderr) != 0 {
		t.Fatalf("all-branch return failed: %#v", valid)
	}

	for _, source := range []string{
		"fun partial(value: Int32): Int32 do if value > 0 then return value end end",
		"fun looping(value: Int32): Int32 do while value > 0 do return value end end",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "may fall through without returning") {
			t.Fatalf("Compile(%q) = %#v, want definite-return diagnostic", source, result)
		}
	}
}

func TestControlFlowEmptyBranchesAndZeroIterationLoops(t *testing.T) {
	result := compileSource("if (true and false) or true then elseif false then else end while false do end")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("empty control-flow blocks failed: %#v", result)
	}
	for _, want := range []string{
		"if ((true && false) || true) {",
		"} else if (false) {",
		"} else {",
		"while (false) {",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestControlFlowNestedLoopsAndLineMappings(t *testing.T) {
	result := compileSource("mut outer: Int32 := 0\nwhile outer < 2 do\n    mut inner: Int32 := 0\n    while inner < 2 do\n        inner = inner + 1\n        if inner == 1 then\n            continue\n        end\n        break\n    end\n    outer = outer + 1\n    break\nend")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("nested control-flow compilation failed: %#v", result)
	}
	if strings.Count(rootC(t, result), "while (") != 2 || strings.Count(rootC(t, result), "break;") != 2 || strings.Count(rootC(t, result), "continue;") != 1 {
		t.Fatalf("nested loop lowering = %q, want two loops, two breaks, and one continue", rootC(t, result))
	}
	for _, want := range []string{
		"#line 2 \"app.hex\"",
		"#line 4 \"app.hex\"",
		"#line 7 \"app.hex\"",
		"#line 12 \"app.hex\"",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestControlFlowConditionLineMappings(t *testing.T) {
	result := compileSource("flag: Bool := true\nif\n    flag then\nelseif\n    flag then\nend\nwhile\n    flag\ndo\nend")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("multiline control-flow conditions failed: %#v", result)
	}
	for _, want := range []string{
		"#line 3 \"app.hex\"",
		"#line 5 \"app.hex\"",
		"#line 8 \"app.hex\"",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want condition mapping %q", rootC(t, result), want)
		}
	}
}

func TestControlFlowLoopScopeCleanup(t *testing.T) {
	result := compileSource("while false do local: Int32 := 1 end read: Int32 := local")
	if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "unknown variable local") {
		t.Fatalf("loop-local binding escaped its scope: %#v", result)
	}
}

func TestControlFlowReturnDiagnosticsDoNotMaskChildErrors(t *testing.T) {
	valid := compileSource("fun done(value: Int32): Int32 do\n    return value\n    ignored: Int32 := 1\nend\nresult: Int32 := done(1)")
	if valid.ExitCode != compiler.ExitSuccess || len(valid.Stderr) != 0 {
		t.Fatalf("return followed by unreachable statements failed: %#v", valid)
	}

	invalid := compileSource("fun bad(value: Int32): Int32 do\n    if value > 0 then\n        return value\n    else\n        missing: Int32 := unknown\n    end\nend")
	message := strings.Join(invalid.Stderr, "\n")
	if invalid.ExitCode != compiler.ExitFailure || !strings.Contains(message, "unknown variable unknown") || strings.Contains(message, "may fall through without returning") {
		t.Fatalf("child diagnostic/fallthrough handling = %#v", invalid)
	}

	parsedInvalid := compileSource("fun parsed_bad(value: Int32): Int32 do\n    broken\nend")
	parsedMessage := strings.Join(parsedInvalid.Stderr, "\n")
	if parsedInvalid.ExitCode != compiler.ExitFailure || !strings.Contains(parsedMessage, "expected ':' or ':=' for a declaration, or '=' for an assignment") || strings.Contains(parsedMessage, "may fall through without returning") {
		t.Fatalf("parser diagnostic/fallthrough handling = %#v", parsedInvalid)
	}

	// A parse failure aborts before checking: the unrelated parse diagnostic
	// appears alone, and the checker's fallthrough diagnostic never joins it.
	unrelatedParserError := compileSource("broken\nfun unrelated_bad(value: Int32): Int32 do\n    local: Int32 := 1\nend")
	unrelatedMessage := strings.Join(unrelatedParserError.Stderr, "\n")
	if unrelatedParserError.ExitCode != compiler.ExitFailure || !strings.Contains(unrelatedMessage, "expected ':' or ':=' for a declaration, or '=' for an assignment") || strings.Contains(unrelatedMessage, "may fall through without returning") {
		t.Fatalf("unrelated parser/return diagnostics = %#v", unrelatedParserError)
	}

	// Same contract: the parse error appears alone; the checker never runs,
	// so the sibling's unknown-variable diagnostic does not join it.
	dottedCallRecovery := compileSource("if true then broken\n    point.step(1)\nend")
	dottedMessage := strings.Join(dottedCallRecovery.Stderr, "\n")
	if dottedCallRecovery.ExitCode != compiler.ExitFailure || !strings.Contains(dottedMessage, "expected ':' or ':=' for a declaration, or '=' for an assignment") || strings.Contains(dottedMessage, "unknown variable point") {
		t.Fatalf("dotted call sibling recovery = %#v", dottedCallRecovery)
	}
}

func TestMethodControlFlowLowering(t *testing.T) {
	result := compileSource("type Counter = { mut count: Int32, } impl MutPtr<Counter>.step(amount: Int32): Int32 do if amount > 0 then self.count = self.count + amount return self.count else return 0 end end mut counter: Counter := Counter { count = 1, } result: Int32 := counter.step(2)")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("method control-flow compilation failed: %#v", result)
	}
	for _, want := range []string{
		"static int32_t hex_f_m3_app_Counter_step",
		"if (hex_v_amount > 0) {",
		"(*hex_v_self).hex_m_count =",
		"hex_f_m3_app_Counter_step(&hex_v_counter, 2)",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestControlFlowDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"break", "break is only valid inside a loop"},
		{"continue", "continue is only valid inside a loop"},
		{"while true do else end", "'else' cannot appear inside a while body"},
		{"if true then else else end", "'else' must be the final clause of an if statement"},
		{"if true then else elseif true end", "'elseif' cannot appear after 'else'"},
		{"if", "expected a condition after 'if'"},
		{"if true then", "expected end to close if"},
		{"return", "return is only valid inside a function or method body"},
		{"return 1", "return is only valid inside a function or method body"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

// A top-level for statement is a legal top-level item and must lower through
// the same path as the identical loop inside a function; the checker's
// top-level dispatch must not fall through to the fail-closed default.
func TestTopLevelForStatementCompiles(t *testing.T) {
	source := "mut total: Int32 := 0\nfixed: Array<Int32, 3> := [1, 2, 3]\nfor value in fixed do\n    total = total + value\nend\nvalues: List<Int32> := List<Int32>.new(Heap.new())\nfor value in values do\n    total = total + value\nend\ntext: String := \"hey\"\nfor rune in text do\n    total = total + rune.to<Int32>()\nend\n"
	result := assertCompiles(t, source)
	for _, want := range []string{
		"const hex_array_Int32_3 *const hex_for_1 = &(hex_v_fixed);",
		"for (size_t hex_for_1_index = 0; hex_for_1_index < (size_t)(3); hex_for_1_index++) {",
		"const hex_list_Int32 *const hex_for_2 = hex_v_values;",
		"const hex_string *const hex_for_3 = hex_v_text;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}
