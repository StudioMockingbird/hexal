package compiler

// Structured conditionals, loops, lexical scopes, and flow-sensitive returns.

import (
	"strings"
	"testing"
)

func TestConditionalAndLoopLowering(t *testing.T) {
	result := Compile("mut count: Int32 = 0 mut ready: Bool = true if ready mut local: Int32 = 1 count = count + local elseif false count = 9 else count = 8 end while count < 3 do count = count + 1 if count == 2 continue end if count > 2 break end end")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("control-flow compilation failed: %#v", result)
	}
	for _, want := range []string{
		"if (sw_v_ready) {",
		"} else if (false) {",
		"} else {",
		"while ((sw_v_count < 3)) {",
		"continue;",
		"break;",
		"sw_v_count = ((uint64_t)",
		"sw_v_local",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestControlFlowScopesAndShadowing(t *testing.T) {
	result := Compile("mut value: Int32 = 0 if true value = 1 else value: Int32 = 2 end if false value: Int32 = 3 end")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("scoped declarations failed: %#v", result)
	}
	if !strings.Contains(result.MainC, "sw_v_value_2") || !strings.Contains(result.MainC, "sw_v_value_3") {
		t.Fatalf("shadowed C names are not deterministic: %q", result.MainC)
	}

	invalid := Compile("if true local: Int32 = 1 end read: Int32 = local")
	if invalid.ExitCode != ExitFailure || !strings.Contains(strings.Join(invalid.Stderr, "\n"), "unknown variable local") {
		t.Fatalf("out-of-scope binding = %#v", invalid.Stderr)
	}
}

func TestControlFlowDefiniteReturns(t *testing.T) {
	valid := Compile("fun classify(value: Int32): Int32 if value > 0 return 1 elseif value == 0 return 0 else return -1 end end result: Int32 = classify(1)")
	if valid.ExitCode != ExitSuccess || len(valid.Stderr) != 0 {
		t.Fatalf("all-branch return failed: %#v", valid)
	}

	for _, source := range []string{
		"fun partial(value: Int32): Int32 if value > 0 return value end end",
		"fun looping(value: Int32): Int32 while value > 0 do return value end end",
	} {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "may fall through without returning") {
			t.Fatalf("Compile(%q) = %#v, want definite-return diagnostic", source, result)
		}
	}
}

func TestControlFlowEmptyBranchesAndZeroIterationLoops(t *testing.T) {
	result := Compile("if (true and false) or true elseif false else end while false do end")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("empty control-flow blocks failed: %#v", result)
	}
	for _, want := range []string{
		"if (((true && false) || true)) {",
		"} else if (false) {",
		"} else {",
		"while (false) {",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestControlFlowNestedLoopsAndLineMappings(t *testing.T) {
	result := Compile("mut outer: Int32 = 0\nwhile outer < 2 do\n    mut inner: Int32 = 0\n    while inner < 2 do\n        inner = inner + 1\n        if inner == 1\n            continue\n        end\n        break\n    end\n    outer = outer + 1\n    break\nend")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("nested control-flow compilation failed: %#v", result)
	}
	if strings.Count(result.MainC, "while (") != 2 || strings.Count(result.MainC, "break;") != 2 || strings.Count(result.MainC, "continue;") != 1 {
		t.Fatalf("nested loop lowering = %q, want two loops, two breaks, and one continue", result.MainC)
	}
	for _, want := range []string{
		"#line 2 \"main.hexal\"",
		"#line 4 \"main.hexal\"",
		"#line 7 \"main.hexal\"",
		"#line 12 \"main.hexal\"",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestControlFlowConditionLineMappings(t *testing.T) {
	result := Compile("flag: Bool = true\nif\n    flag\nelseif\n    flag\nend\nwhile\n    flag\ndo\nend")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("multiline control-flow conditions failed: %#v", result)
	}
	for _, want := range []string{
		"#line 3 \"main.hexal\"",
		"#line 5 \"main.hexal\"",
		"#line 8 \"main.hexal\"",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want condition mapping %q", result.MainC, want)
		}
	}
}

func TestControlFlowLoopScopeCleanup(t *testing.T) {
	result := Compile("while false do local: Int32 = 1 end read: Int32 = local")
	if result.ExitCode != ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "unknown variable local") {
		t.Fatalf("loop-local binding escaped its scope: %#v", result)
	}
}

func TestControlFlowReturnDiagnosticsDoNotMaskChildErrors(t *testing.T) {
	valid := Compile("fun done(value: Int32): Int32\n    return value\n    ignored: Int32 = 1\nend\nresult: Int32 = done(1)")
	if valid.ExitCode != ExitSuccess || len(valid.Stderr) != 0 {
		t.Fatalf("return followed by unreachable statements failed: %#v", valid)
	}

	invalid := Compile("fun bad(value: Int32): Int32\n    if value > 0\n        return value\n    else\n        missing: Int32 = unknown\n    end\nend")
	message := strings.Join(invalid.Stderr, "\n")
	if invalid.ExitCode != ExitFailure || !strings.Contains(message, "unknown variable unknown") || strings.Contains(message, "may fall through without returning") {
		t.Fatalf("child diagnostic/fallthrough handling = %#v", invalid)
	}

	parsedInvalid := Compile("fun parsed_bad(value: Int32): Int32\n    broken\nend")
	parsedMessage := strings.Join(parsedInvalid.Stderr, "\n")
	if parsedInvalid.ExitCode != ExitFailure || !strings.Contains(parsedMessage, "expected ':' for a declaration or '=' for an assignment") || strings.Contains(parsedMessage, "may fall through without returning") {
		t.Fatalf("parser diagnostic/fallthrough handling = %#v", parsedInvalid)
	}

	unrelatedParserError := Compile("broken\nfun unrelated_bad(value: Int32): Int32\n    local: Int32 = 1\nend")
	unrelatedMessage := strings.Join(unrelatedParserError.Stderr, "\n")
	if unrelatedParserError.ExitCode != ExitFailure || !strings.Contains(unrelatedMessage, "expected ':' for a declaration or '=' for an assignment") || !strings.Contains(unrelatedMessage, "returning unrelated_bad may fall through without returning Int32") {
		t.Fatalf("unrelated parser/return diagnostics = %#v", unrelatedParserError)
	}

	dottedCallRecovery := Compile("if true broken\n    point.step(1)\nend")
	dottedMessage := strings.Join(dottedCallRecovery.Stderr, "\n")
	if dottedCallRecovery.ExitCode != ExitFailure || !strings.Contains(dottedMessage, "expected ':' for a declaration or '=' for an assignment") || !strings.Contains(dottedMessage, "unknown variable point") {
		t.Fatalf("dotted call sibling recovery = %#v", dottedCallRecovery)
	}
}

func TestMethodControlFlowLowering(t *testing.T) {
	result := Compile("type Counter = { mut count: Int32, } impl MutPtr<Counter>.step(amount: Int32): Int32 if amount > 0 self.count = self.count + amount return self.count else return 0 end end mut counter: Counter = Counter { count = 1, } result: Int32 = counter.step(2)")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("method control-flow compilation failed: %#v", result)
	}
	for _, want := range []string{
		"static int32_t sw_f_Counter_step",
		"if ((sw_v_amount > 0)) {",
		"(*sw_v_self).sw_m_count =",
		"sw_f_Counter_step(&sw_v_counter, 2)",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
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
		{"if true else else end", "'else' must be the final clause of an if statement"},
		{"if true else elseif true end", "'elseif' cannot appear after 'else'"},
		{"if", "expected a condition after 'if'"},
		{"if true", "expected end to close if"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}
