package integration

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/compiler/config"
)

func TestRegexSurfaceAndPrivateAdapterBoundary(t *testing.T) {
	source := "import\n    Regex from std.regex\nend\n" +
		"fun spans_equal(left: Regex.Span, right: Regex.Span): Bool do\n" +
		"    return left == right\nend\n" +
		"fun inspect(h: Heap, pattern: String, subject: String): Bool | Error do\n" +
		"    let compiled: Regex.Pattern = try Regex.compile(h, pattern)\n" +
		"    let matched: Bool = try compiled.test(h, subject)\n" +
		"    let location: Regex.Span | Nil = try compiled.find(h, subject)\n" +
		"    let captured: Regex.Match | Nil = try compiled.capture(h, subject)\n" +
		"    if captured != nil then\n" +
		"        let result: Regex.Match = captured\n" +
		"        let first: Regex.Span | Nil = result.captures[0]\n" +
		"        result.free(h)\n" +
		"    end\n" +
		"    compiled.free(h)\n" +
		"    return matched\nend\n"
	result := assertCompiles(t, source)
	if !slices.Contains(result.Dependencies, compiler.RuntimePcre2) {
		t.Fatalf("regex operations dependencies = %v, want pcre2", result.Dependencies)
	}
	head := moduleFile(t, result, "hexal/regex.h")
	adapter := moduleFile(t, result, "hexal/regex.c")
	if strings.Count(adapter, "#include <pcre2.h>") != 1 {
		t.Fatalf("PCRE2 must be included by exactly one private adapter:\n%s", adapter)
	}
	for name, content := range result.Files {
		if strings.HasSuffix(name, ".h") && (strings.Contains(content, "pcre2.h") || strings.Contains(content, "pcre2_")) {
			t.Errorf("public header %s exposes PCRE2:\n%s", name, content)
		}
		if strings.HasSuffix(name, ".c") && name != "hexal/regex.c" && strings.Contains(content, "pcre2_") {
			t.Errorf("PCRE2 call escaped the private adapter into %s", name)
		}
	}
	if strings.Contains(head, "pcre2") {
		t.Fatalf("regex public header exposes PCRE2:\n%s", head)
	}
	for _, want := range []string{
		"PCRE2_UTF | PCRE2_UCP",
		"pcre2_set_max_pattern_length(compile_context, " + strconv.Itoa(config.RegexMaxPatternBytes) + ")",
		"pcre2_set_max_pattern_compiled_length(compile_context, " + strconv.Itoa(config.RegexMaxCompiledPatternBytes) + ")",
		"pcre2_set_parens_nest_limit(compile_context, " + strconv.Itoa(config.RegexMaxParenthesisDepth) + ")",
		"pcre2_set_match_limit(match_context, " + strconv.Itoa(config.RegexMatchLimit) + ")",
		"pcre2_set_depth_limit(match_context, " + strconv.Itoa(config.RegexMatchDepthLimit) + ")",
		"pcre2_set_heap_limit(match_context, " + strconv.Itoa(config.RegexMatchHeapLimitKiB) + ")",
	} {
		if !strings.Contains(adapter, want) {
			t.Errorf("private regex adapter lacks %q", want)
		}
	}
}

func TestRegexTypeOnlyAndFreeMatchDoNotSelectPCRE2(t *testing.T) {
	for _, source := range []string{
		"import\n    Regex from std.regex\nend\nfun inspect(value: Regex.Pattern) do\nend\n",
		"import\n    Regex from std.regex\nend\nfun release(h: Heap, value: Regex.Match) do\n    value.free(h)\nend\n",
	} {
		result := assertCompiles(t, source)
		if slices.Contains(result.Dependencies, compiler.RuntimePcre2) {
			t.Fatalf("type/free_match-only program selected pcre2: %v", result.Dependencies)
		}
		if strings.Contains(result.Files["hexal/regex.c"], "#include <pcre2.h>") {
			t.Fatalf("type/free_match-only program materialized the PCRE2 adapter")
		}
	}
}

func TestRegexRejectsUnsupportedTypeOperations(t *testing.T) {
	for _, testCase := range []struct{ name, source, want string }{
		{"pattern equality", "import\n Regex from std.regex\nend\nfun f(a: Regex.Pattern, b: Regex.Pattern): Bool do\n return a == b\nend\n", "equality is unavailable for Pattern"},
		{"pattern ordering", "import\n Regex from std.regex\nend\nfun f(a: Regex.Pattern, b: Regex.Pattern): Bool do\n return a < b\nend\n", "ordering is unavailable for Pattern values"},
		{"pattern printing", "import\n Regex from std.regex\nend\nfun f(a: Regex.Pattern) do\n print(a)\nend\n", "print does not support Pattern"},
		{"match equality", "import\n Regex from std.regex\nend\nfun f(a: Regex.Match, b: Regex.Match): Bool do\n return a == b\nend\n", "equality is unavailable for Match"},
		{"match ordering", "import\n Regex from std.regex\nend\nfun f(a: Regex.Match, b: Regex.Match): Bool do\n return a < b\nend\n", "ordering is unavailable for Match values"},
		{"match printing", "import\n Regex from std.regex\nend\nfun f(a: Regex.Match) do\n print(a)\nend\n", "print does not support Match"},
		{"pattern dictionary key", "import\n Regex from std.regex\nend\nfun f(h: Heap) do\n let d: Dict<Regex.Pattern, Int32> = Dict<Regex.Pattern, Int32>(h)\nend\n", "dictionary key type"},
		{"match dictionary key", "import\n Regex from std.regex\nend\nfun f(h: Heap) do\n let d: Dict<Regex.Match, Int32> = Dict<Regex.Match, Int32>(h)\nend\n", "dictionary key type"},
		{"inline source", "import\n Regex from std.regex\nend\nfun short(): String<4> do\n return \"abc\"\nend\nfun f(h: Heap) do\n let source: String<4> = short()\n let p: Regex.Pattern | Error = Regex.compile(h, source)\nend\n", "copy(heap)"},
	} {
		t.Run(testCase.name, func(t *testing.T) { assertRejects(t, testCase.source, testCase.want) })
	}
}

func TestRegexTestMethodSelectsPCRE2(t *testing.T) {
	result := assertCompiles(t, "import\n    Regex from std.regex\nend\n"+
		"fun check(h: Heap, pattern: Regex.Pattern, subject: String): Bool | Error do\n    return pattern.test(h, subject)\nend\n")
	if !slices.Contains(result.Dependencies, compiler.RuntimePcre2) {
		t.Fatalf("pattern.test dependencies = %v, want pcre2", result.Dependencies)
	}
}

func TestRegexModuleFunctionsMovedToMethodsAreRejected(t *testing.T) {
	for _, call := range []string{
		"Regex.test(h, pattern, \"a\")",
		"Regex.find(h, pattern, \"a\")",
		"Regex.capture(h, pattern, \"a\")",
		"Regex.free(h, pattern)",
		"Regex.free_match(h, found)",
	} {
		source := "import\n    Regex from std.regex\nend\n" +
			"fun f(h: Heap, pattern: Regex.Pattern, found: Regex.Match) do\n    " + call + "\nend\n"
		t.Run(call, func(t *testing.T) { assertRejects(t, source, "private to module") })
	}
}
