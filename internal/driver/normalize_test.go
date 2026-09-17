package driver

// Pure-Go tests for selection and normalization: the line-marker
// index, the decoded-AST selection rules, and the deterministic emitted
// binding text. They invoke no external frontend.

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// testIndex maps preprocessed line 2 onward to adder.h starting at line 1.
func testIndex() *lineIndex {
	return newLineIndex("# 1 \"adder.h\"\n")
}

func normalizeTest(t *testing.T, ast string, request compiler.CImportRequest) string {
	t.Helper()
	source, failure := normalizeHeader(ast, testIndex(), request, headerOptions{})
	if failure != nil {
		t.Fatalf("normalizeHeader failed: %v", failure)
	}
	return source
}

func TestLineIndexResolvesOrigin(t *testing.T) {
	index := newLineIndex("int before;\n# 1 \"adder.h\"\nint32_t adder_add(int32_t);\n# 20 \"other.h\"\nint other;\n")
	if file, line := index.origin(3); file != "adder.h" || line != 1 {
		t.Fatalf("origin(3) = %q:%d, want adder.h:1", file, line)
	}
	if file, line := index.origin(5); file != "other.h" || line != 20 {
		t.Fatalf("origin(5) = %q:%d, want other.h:20", file, line)
	}
	if file, _ := index.origin(1); file != "" {
		t.Fatalf("origin(1) = %q, want no file before the first marker", file)
	}
}

func TestNormalizeAdder(t *testing.T) {
	ast := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"TypedefDecl","loc":{"line":2},"name":"int32_t","type":{"qualType":"int"}},` +
		`{"kind":"FunctionDecl","loc":{"line":3},"name":"adder_add","type":{"qualType":"int32_t (int32_t, int32_t)"},"inner":[` +
		`{"kind":"ParmVarDecl","loc":{"line":3},"name":"left","type":{"qualType":"int32_t"}},` +
		`{"kind":"ParmVarDecl","loc":{"line":3},"name":"right","type":{"qualType":"int32_t"}}]}]}`
	source := normalizeTest(t, ast, compiler.CImportRequest{Header: "adder.h"})
	want := "extern c from \"adder.h\" do\n" +
		"    type int32_t is Int32\n" +
		"    fun adder_add(left: int32_t, right: int32_t): int32_t\n" +
		"end\n" +
		"\nexport\n    adder_add,\n    int32_t,\nend\n"
	if source != want {
		t.Fatalf("binding =\n%s\nwant\n%s", source, want)
	}
}

func TestNormalizeOmissions(t *testing.T) {
	ast := `{"kind":"TranslationUnitDecl","inner":[` +
		// A variadic function is omitted whole.
		`{"kind":"FunctionDecl","loc":{"line":2},"name":"fprintf","variadic":true,"type":{"qualType":"int (const char *, ...)"},"inner":[` +
		`{"kind":"ParmVarDecl","loc":{"line":2},"name":"format","type":{"qualType":"const char *"}}]},` +
		// A function whose parameter is a function pointer is omitted whole.
		`{"kind":"FunctionDecl","loc":{"line":3},"name":"takes_callback","type":{"qualType":"int (int (*)(void))"},"inner":[` +
		`{"kind":"ParmVarDecl","loc":{"line":3},"name":"callback","type":{"qualType":"int (*)(void)"}}]},` +
		// An internal-linkage, non-inline function is omitted.
		`{"kind":"FunctionDecl","loc":{"line":4},"name":"helper","storageClass":"static","type":{"qualType":"int (void)"}},` +
		// A builtin-only declaration with no source location is omitted.
		`{"kind":"FunctionDecl","name":"__builtin_trap","type":{"qualType":"void (void)"}}` +
		`]}`
	source := normalizeTest(t, ast, compiler.CImportRequest{Header: "x.h"})
	if strings.Contains(source, "fprintf") || strings.Contains(source, "takes_callback") || strings.Contains(source, "helper") {
		t.Fatalf("an unsupported declaration was not omitted:\n%s", source)
	}
	// No supported declaration means no export block at all.
	if strings.Contains(source, "export") {
		t.Fatalf("an empty binding must not emit an export block:\n%s", source)
	}
}

func TestNormalizeStaticInlineIsRetained(t *testing.T) {
	ast := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"FunctionDecl","loc":{"line":2},"name":"inline_zero","storageClass":"static","inline":true,"type":{"qualType":"int (void)"}}` +
		`]}`
	source := normalizeTest(t, ast, compiler.CImportRequest{Header: "x.h"})
	if !strings.Contains(source, "fun inline_zero() as") && !strings.Contains(source, "fun inline_zero(") {
		t.Fatalf("an included static inline function must be retained:\n%s", source)
	}
}

func TestNormalizeRecordCoalescing(t *testing.T) {
	ast := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"RecordDecl","loc":{"line":2},"name":"Vector2","tagUsed":"struct","completeDefinition":true,"inner":[` +
		`{"kind":"FieldDecl","loc":{"line":3},"name":"x","type":{"qualType":"double"}},` +
		`{"kind":"FieldDecl","loc":{"line":4},"name":"y","type":{"qualType":"double"}}]},` +
		`{"kind":"TypedefDecl","loc":{"line":5},"name":"Vector2","type":{"qualType":"struct Vector2"}}` +
		`]}`
	source := normalizeTest(t, ast, compiler.CImportRequest{Header: "vec.h"})
	if strings.Count(source, "type Vector2") != 1 {
		t.Fatalf("a typedef-plus-record pair must normalize once:\n%s", source)
	}
	if !strings.Contains(source, "mut x: Float64,") {
		t.Fatalf("record fields must be mutable by default:\n%s", source)
	}
}

func TestNormalizeOpaqueRecordAndPointer(t *testing.T) {
	ast := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"RecordDecl","loc":{"line":2},"name":"Window","tagUsed":"struct"},` +
		`{"kind":"FunctionDecl","loc":{"line":3},"name":"window_new","type":{"qualType":"struct Window *(void)"}}` +
		`]}`
	source := normalizeTest(t, ast, compiler.CImportRequest{Header: "window.h"})
	if !strings.Contains(source, `type Window as "struct Window" is opaque`) {
		t.Fatalf("an incomplete record must be opaque:\n%s", source)
	}
	if !strings.Contains(source, "window_new(): Ptr<mut Window> | Nil") {
		t.Fatalf("an automatic pointer result must be nullable:\n%s", source)
	}
}

func TestNormalizeIsIndependentOfTraversalOrder(t *testing.T) {
	first := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"FunctionDecl","loc":{"line":4},"name":"second","type":{"qualType":"int (void)"}},` +
		`{"kind":"FunctionDecl","loc":{"line":2},"name":"first","type":{"qualType":"int (void)"}}` +
		`]}`
	second := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"FunctionDecl","loc":{"line":2},"name":"first","type":{"qualType":"int (void)"}},` +
		`{"kind":"FunctionDecl","loc":{"line":4},"name":"second","type":{"qualType":"int (void)"}}` +
		`]}`
	left := normalizeTest(t, first, compiler.CImportRequest{Header: "x.h"})
	right := normalizeTest(t, second, compiler.CImportRequest{Header: "x.h"})
	if left != right {
		t.Fatalf("traversal order changed the binding:\n%s\n---\n%s", left, right)
	}
	if strings.Index(left, "first") > strings.Index(left, "second") {
		t.Fatalf("declarations are not ordered by source location:\n%s", left)
	}
}

func TestNormalizeSystemHeaderForm(t *testing.T) {
	ast := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"FunctionDecl","loc":{"line":2},"name":"puts","type":{"qualType":"int (const char *)"},"inner":[` +
		`{"kind":"ParmVarDecl","loc":{"line":2},"name":"text","type":{"qualType":"const char *"}}]}` +
		`]}`
	source := normalizeTest(t, ast, compiler.CImportRequest{Header: "stdio.h", System: true})
	if !strings.HasPrefix(source, "extern c from <stdio.h> do\n") {
		t.Fatalf("a system header must keep its angle-bracket form:\n%s", source)
	}
	if !strings.Contains(source, `text: Ptr<Byte> | Nil as "const char *"`) {
		t.Fatalf("a const char * parameter must keep its exact spelling:\n%s", source)
	}
}

func TestNormalizeEscapedNames(t *testing.T) {
	ast := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"FunctionDecl","loc":{"line":2},"name":"__debugbreak","type":{"qualType":"void (void)"}},` +
		`{"kind":"FunctionDecl","loc":{"line":3},"name":"type","type":{"qualType":"void (void)"}},` +
		`{"kind":"FunctionDecl","loc":{"line":4},"name":"Int32","type":{"qualType":"void (void)"}}` +
		`]}`
	source := normalizeTest(t, ast, compiler.CImportRequest{Header: "x.h"})
	if !strings.Contains(source, `fun hex_cvar___debugbreak as "__debugbreak"()`) {
		t.Fatalf("a leading-underscore name must be escaped:\n%s", source)
	}
	if !strings.Contains(source, `fun hex_cvar_type as "type"()`) {
		t.Fatalf("a Hexal keyword name must be escaped:\n%s", source)
	}
	if !strings.Contains(source, `fun hex_cvar_Int32 as "Int32"()`) {
		t.Fatalf("a protected Hexal type name must be escaped:\n%s", source)
	}
}

func TestNormalizeEnumAndGlobal(t *testing.T) {
	ast := `{"kind":"TranslationUnitDecl","inner":[` +
		`{"kind":"EnumDecl","loc":{"line":2},"name":"Color","inner":[` +
		`{"kind":"EnumConstantDecl","loc":{"line":3},"name":"COLOR_RED"},` +
		`{"kind":"EnumConstantDecl","loc":{"line":4},"name":"COLOR_GREEN"}]},` +
		`{"kind":"VarDecl","loc":{"line":5},"name":"global_counter","storageClass":"extern","type":{"qualType":"int"}},` +
		`{"kind":"VarDecl","loc":{"line":6},"name":"global_limit","storageClass":"extern","type":{"qualType":"const int"}}` +
		`]}`
	source := normalizeTest(t, ast, compiler.CImportRequest{Header: "x.h"})
	if !strings.Contains(source, "type Color is Int32") {
		t.Fatalf("an enum must normalize to a transparent integer alias:\n%s", source)
	}
	if !strings.Contains(source, `constant COLOR_RED as "COLOR_RED": Color`) {
		t.Fatalf("an enumerator must normalize to a foreign constant:\n%s", source)
	}
	if !strings.Contains(source, "global mut global_counter: Int32") {
		t.Fatalf("a mutable global must normalize to global mut:\n%s", source)
	}
	if !strings.Contains(source, "global global_limit: Int32") {
		t.Fatalf("a const global must normalize to a fixed global:\n%s", source)
	}
}

func TestNormalizeMalformedJSONFailsClosed(t *testing.T) {
	if _, failure := normalizeHeader("{not json", testIndex(), compiler.CImportRequest{Header: "x.h"}, headerOptions{}); failure == nil {
		t.Fatal("malformed JSON must fail closed")
	}
	if _, failure := normalizeHeader(`{"kind":"Other"}`, testIndex(), compiler.CImportRequest{Header: "x.h"}, headerOptions{}); failure == nil {
		t.Fatal("a non-translation-unit root must fail closed")
	}
}
