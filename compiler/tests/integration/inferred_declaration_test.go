package integration

import (
	"strings"
	"testing"
)

// An inferred binding omits the annotation exactly where the initializer
// already determines the type. A declaration must still state its type once;
// a binding where neither side does is rejected.

const inferRejection = "`:=` requires an initializer whose type does not depend on context"

// The accepted forms, each verified by the type in the generated C rather than
// by compiling alone. Int64 throughout is the load-bearing detail: an
// initializer that fell back to the literal default would spell int32_t here.
func TestInferredDeclarationTakesTheInitializersType(t *testing.T) {
	for _, testCase := range []struct{ name, source, want string }{
		{
			"constructor",
			"fun demo(h: Heap) do\n    names := Dict<Int32, Strand>.new(h)\n    names.free(h)\nend",
			"hex_dict_Int32_Strand *const hex_v_names = hex_dict_new_Int32_Strand(",
		},
		{
			"named object literal",
			"type Entry = { x: Int32, }\nfun demo() do\n    e := Entry { x = 1, }\nend",
			"const hex_t_m3_app_Entry hex_v_e =",
		},
		{
			"declared function result",
			"fun compute(): Int64 do\n    return 7\nend\nfun demo() do\n    total := compute()\nend",
			"const int64_t hex_v_total = hex_f_m3_app_compute();",
		},
		{
			"another typed binding",
			"fun demo() do\n    a: Int64 := 1\n    b := a\nend",
			"const int64_t hex_v_b = hex_v_a;",
		},
		{
			"qualified variant",
			"type Shape = | Circle as { r: Int32 } | Square as { a: Int32 }\nfun demo() do\n    s := Shape.Circle { r = 10 }\nend",
			"const hex_t_m3_app_Shape hex_v_s =",
		},
		{
			"function reference",
			"fun identity(x: Int32): Int32 do\n    return x\nend\nfun demo() do\n    f := identity\nend",
			"int32_t (*const hex_v_f)(int32_t) = hex_f_m3_app_identity;",
		},
		{
			"typed value with an untyped literal",
			"fun demo() do\n    count: Int64 := 1\n    sum := count + 1\nend",
			"const int64_t hex_v_sum =",
		},
		{
			"match with typed arms",
			"fun compute(): Int64 do\n    return 7\nend\nfun demo(ready: Bool) do\n    x := match ready | true then compute() | false then compute() end\nend",
			"int64_t hex_v_x",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := assertCompiles(t, testCase.source)
			if !strings.Contains(rootC(t, result), testCase.want) {
				t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), testCase.want)
			}
		})
	}
}

// The rejected forms. Every one of these would otherwise take a type from the
// literal defaults, which is the silent mistyping the inferred-declaration
// rule exists to prevent.
func TestInferredDeclarationRejectsContextualInitializers(t *testing.T) {
	for _, source := range []string{
		"fun demo() do\n    total := 0\nend",
		"fun demo() do\n    ratio := 1.5\nend",
		"fun demo() do\n    label := \"hexal\"\nend",
		"fun demo() do\n    empty := nil\nend",
		"fun demo() do\n    grid := [1, 2, 3]\nend",
		"fun demo() do\n    sum := 1 + 2\nend",
		"fun demo() do\n    negated := -1\nend",
	} {
		assertRejects(t, source, inferRejection)
	}
}

func TestInferredDeclarationAcceptsDistinctLiteralTypes(t *testing.T) {
	result := assertCompiles(t, "fun demo() do\n    flag := true\n    done := eos\n    byte := b'A'\n    rune := 'A'\nend")
	for _, want := range []string{
		"const bool hex_v_flag = true;",
		"const hex_eos hex_v_done =",
		"const uint8_t hex_v_byte = 65;",
		"const uint32_t hex_v_rune = 65;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

// The silent-acceptance case. A match forwards the expected type into every
// arm, so a match of bare literals has no type of its own and would default to
// Int32 without a word from the source. Without this case the predicate passes
// every other test here.
func TestInferredDeclarationRejectsMatchOverBareLiterals(t *testing.T) {
	assertRejects(t, "fun demo(ready: Bool) do\n    x := match ready | true then 1 | false then 0 end\nend", inferRejection)
	// Every arm typed and agreeing: the match determines itself. Covered by
	// TestInferredDeclarationTakesTheInitializersType, which also checks it
	// lands as int64_t.
	//
	// One typed arm beside a bare literal is still rejected, but by the arm
	// agreement rule rather than the predicate: with no expected type the
	// literal defaults to Int32 and disagrees with the Int64 arm. The
	// Validation calls this case accepted, which is wrong: it is rejected,
	// and for a reason that is if anything more precise.
	assertRejects(t,
		"fun compute(): Int64 do\n    return 7\nend\nfun demo(ready: Bool) do\n    x := match ready | true then compute() | false then 0 end\nend",
		"match arm result types do not agree")
}

// `:=` is one token, not Colon followed by Equal. This is the test that
// distinguishes the two implementations: under a lookahead parser the spaced
// form would be a second, accidental spelling of the feature.
func TestInferredDeclarationIsOneTokenNotALookahead(t *testing.T) {
	assertRejects(t, "fun demo() do\n    x : = 5\nend", "expected a type name")
}

// mut governs rebinding, not the annotation; both forms accept it.
func TestInferredDeclarationCarriesMutability(t *testing.T) {
	assertCompiles(t, "fun compute(): Int64 do\n    return 7\nend\nfun demo() do\n    mut total := compute()\n    total = 9\nend")
	assertRejects(t,
		"fun compute(): Int64 do\n    return 7\nend\nfun demo() do\n    total := compute()\n    total = 9\nend",
		"cannot assign to constant")
}

// Invariant 3: a `:=` binding and its annotated equivalent generate identical
// C. The form is a source convenience and reaches the generator as the same
// checked declaration.
func TestInferredAndAnnotatedDeclarationsGenerateIdenticalC(t *testing.T) {
	const prelude = "fun compute(): Int64 do\n    return 7\nend\ntype Entry = { x: Int32, }\nfun demo(h: Heap) do\n"
	inferred := assertCompiles(t, prelude+"    total := compute()\n    e := Entry { x = 1, }\n    names := Dict<Int32, Strand>.new(h)\n    names.free(h)\nend")
	annotated := assertCompiles(t, prelude+"    total: Int64 := compute()\n    e: Entry := Entry { x = 1, }\n    names: Dict<Int32, Strand> := Dict<Int32, Strand>.new(h)\n    names.free(h)\nend")
	for _, key := range []string{"modules/app.c", "modules/app.h", "hexal.h"} {
		if inferred.Files[key] != annotated.Files[key] {
			t.Fatalf("%s differs between the inferred and annotated forms:\n--- inferred ---\n%s\n--- annotated ---\n%s", key, inferred.Files[key], annotated.Files[key])
		}
	}
}

// A bare variant literal is contextual: it resolves its name against the
// expected ADT, but it is syntactically identical to a named object literal,
// which is not. No syntactic predicate can separate them, so this one is left
// to the type checker, which rejects it. Recorded as a test because the
// rejection is what keeps the case from being silently mistyped, and the
// message is the one an author sees.
func TestInferredDeclarationLeavesBareVariantsToTheTypeChecker(t *testing.T) {
	assertRejects(t,
		"type Shape = | Circle as { r: Int32 } | Square as { a: Int32 }\nfun demo() do\n    s := Circle { r = 10 }\nend",
		"unknown type Circle")
}
