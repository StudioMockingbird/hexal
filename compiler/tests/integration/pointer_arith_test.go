package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// Safe Hexal has no pointer arithmetic: pointers refer to and dereference one
// typed object, and bounds-carrying types own sequence access. The fenced
// offset/index/cast surface is the only way to traverse raw addresses, and it
// requires an explicit permission region.

func TestPointerArithmeticOperatorsRejected(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"plus count", "fun demo() do\n    value: Int32 := 1\n    pointer: Ptr<Int32> := @value\n    bad: Ptr<Int32> := pointer + 1\nend", "operator + requires numeric operands"},
		{"count plus", "fun demo() do\n    value: Int32 := 1\n    pointer: Ptr<Int32> := @value\n    bad: Ptr<Int32> := 1 + pointer\nend", "operator + requires numeric operands"},
		{"minus count", "fun demo() do\n    value: Int32 := 1\n    pointer: Ptr<Int32> := @value\n    bad: Ptr<Int32> := pointer - 1\nend", "operator - requires numeric operands"},
		{"distance", "fun demo() do\n    value: Int32 := 1\n    other: Int32 := 2\n    left: Ptr<Int32> := @value\n    right: Ptr<Int32> := @other\n    bad: Int32 := left - right\nend", "operator - requires numeric operands"},
		{"mut pointer plus", "fun demo() do\n    mut value: Int32 := 1\n    pointer: Ptr<mut Int32> := @value\n    bad: Ptr<mut Int32> := pointer + 1\nend", "operator + requires numeric operands"},
		{"alias plus", "type Handle is Ptr<Int32>\nfun demo() do\n    value: Int32 := 1\n    pointer: Handle := @value\n    bad: Handle := pointer + 1\nend", "operator + requires numeric operands"},
		{"nested pointer", "type Node is struct next: Ptr<Node>, end\nfun demo(node: Ptr<Node>) do\n    bad: Ptr<Node> := node.next + 1\nend", "operator + requires numeric operands"},
		{"inside unsafe", "fun demo() do\n    value: Int32 := 1\n    pointer: Ptr<Int32> := @value\n    unsafe do\n        bad: Ptr<Int32> := pointer + 1\n    end\nend", "operator + requires numeric operands"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

func TestPointerOrderingRejected(t *testing.T) {
	for _, operator := range []string{"<", "<=", ">", ">="} {
		source := "fun demo() do\n    value: Int32 := 1\n    other: Int32 := 2\n    left: Ptr<Int32> := @value\n    right: Ptr<Int32> := @other\n    bad: Bool := left " + operator + " right\nend"
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ordering is unavailable for Ptr<Int32>") {
			t.Fatalf("Compile(%q) stderr = %#v, want ordering rejection", source, result.Stderr)
		}
	}
}

// Each fenced operation names itself in its own permission diagnostic.
func TestFencedPointerOperationsRequireUnsafe(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"offset", "fun demo(p: Ptr<Byte>) do\n    bad: Ptr<Byte> := p.offset(1)\nend\n", "Ptr.offset requires an unsafe do ... end block"},
		{"mut offset", "fun demo(p: Ptr<mut Byte>) do\n    bad: Ptr<mut Byte> := p.offset(1)\nend\n", "Ptr.offset requires an unsafe do ... end block"},
		{"index read", "fun demo(p: Ptr<Byte>) do\n    bad: Byte := p[0]\nend\n", "pointer indexing requires an unsafe do ... end block"},
		{"index write", "fun demo(p: Ptr<mut Byte>) do\n    p[0] = 1\nend\n", "pointer indexing requires an unsafe do ... end block"},
		{"cast", "fun demo(p: Ptr<Byte>) do\n    bad: Ptr<UInt32> := p.cast<UInt32>()\nend\n", "Ptr.cast requires an unsafe do ... end block"},
		{"mut cast", "fun demo(p: Ptr<mut Byte>) do\n    bad: Ptr<mut UInt32> := p.cast<UInt32>()\nend\n", "Ptr.cast requires an unsafe do ... end block"},
		{"narrowed nullable index", "fun demo() do\n    value: Int32 := 1\n    mut pointer: Ptr<Int32> | Nil := @value\n    if pointer != nil then\n        bad: Int32 := pointer[0]\n    end\nend", "pointer indexing requires an unsafe do ... end block"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

// Inside the region the three operations preserve the receiver's access mode
// exactly and lower to plain C.
func TestFencedPointerOperationsPreserveAccessMode(t *testing.T) {
	writable := assertCompiles(t, "fun demo(p: Ptr<mut Byte>) do\n    unsafe do\n        next: Ptr<mut Byte> := p.offset(1)\n        value: Byte := p[1]\n        words: Ptr<mut UInt32> := p.cast<UInt32>()\n        p[0] = 5\n    end\nend\n")
	for _, want := range []string{
		"uint8_t *const hex_v_next = (hex_v_p + 1);",
		"const uint8_t hex_v_value = hex_v_p[1];",
		"uint32_t *const hex_v_words = (uint32_t *)hex_v_p;",
		"hex_v_p[0] = 5;",
	} {
		if !strings.Contains(rootC(t, writable), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, writable), want)
		}
	}
	readOnly := assertCompiles(t, "fun demo(p: Ptr<Byte>) do\n    unsafe do\n        next: Ptr<Byte> := p.offset(1)\n        value: Byte := p[1]\n        words: Ptr<UInt32> := p.cast<UInt32>()\n    end\nend\n")
	for _, want := range []string{
		"const uint8_t *const hex_v_next = (hex_v_p + 1);",
		"const uint32_t *const hex_v_words = (const uint32_t *)hex_v_p;",
	} {
		if !strings.Contains(rootC(t, readOnly), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, readOnly), want)
		}
	}
	// No helper, checked-arithmetic formula, trap, or metadata is emitted.
	for _, unwanted := range []string{"hex_pointer_", "hex_ptr_offset", "hex_ptr_cast"} {
		if strings.Contains(rootC(t, writable), unwanted) {
			t.Fatalf("modules/app.c emitted %q:\n%s", unwanted, rootC(t, writable))
		}
	}
}

// A read-only pointer can never produce a writable one through offset or cast.
func TestFencedPointerOperationsCannotUpgradeAccess(t *testing.T) {
	assertRejects(t,
		"fun demo(p: Ptr<Byte>) do\n    unsafe do\n        bad: Ptr<mut UInt32> := p.cast<UInt32>()\n    end\nend\n",
		"expected Ptr<mut UInt32> initializer; got Ptr<UInt32>")
	assertRejects(t,
		"fun demo(p: Ptr<Byte>) do\n    unsafe do\n        bad: Ptr<mut Byte> := p.offset(1)\n    end\nend\n",
		"expected Ptr<mut UInt8> initializer; got Ptr<UInt8>")
	assertRejects(t,
		"fun demo(p: Ptr<Byte>) do\n    unsafe do\n        p[0] = 5\n    end\nend\n",
		"cannot write through a read-only pointer")
	// A writable cast result still weakens to the read-only outer form.
	assertCompiles(t, "fun demo(p: Ptr<mut Byte>) do\n    unsafe do\n        weak: Ptr<UInt32> := p.cast<UInt32>()\n    end\nend\n")
}

// Pointer indexing whose pointee is itself a collection is refused outright:
// the two possible intents get two distinct spellings instead.
func TestPointerIndexingOfCollectionPointeeIsAmbiguous(t *testing.T) {
	assertRejects(t,
		"fun demo(p: Ptr<Array<Int32, 4>>) do\n    unsafe do\n        bad: Int32 := p[0]\n    end\nend\n",
		"pointer indexing of Ptr<Array<Int32, 4>> is ambiguous; use (^pointer)[index] to index the collection or pointer.offset(index) to advance the pointer")
	// Both explicit spellings remain available and keep their own meaning.
	assertCompiles(t, "fun demo(p: Ptr<Array<Int32, 4>>) do\n    item: Int32 := (^p)[2]\n    unsafe do\n        next: Ptr<Array<Int32, 4>> := p.offset(1)\n    end\nend\n")
	assertRejects(t,
		"fun demo(p: Ptr<Array<Int32, 4>>) do\n    bad: Int32 := (^p)[9]\nend\n",
		"out of bounds")
}

// Address traversal needs a pointee C can scale the step by; a cast is the
// only operation valid at an erased or incomplete boundary.
func TestFencedPointerOperationsRequireACompletePointee(t *testing.T) {
	assertRejects(t,
		"fun demo(p: Ptr<Unknown>) do\n    unsafe do\n        bad: Ptr<Unknown> := p.offset(1)\n    end\nend\n",
		"pointer arithmetic requires a complete pointee type; got Unknown")
	assertRejects(t,
		"fun demo(p: Ptr<Unknown>) do\n    unsafe do\n        bad: Int32 := p[0]\n    end\nend\n",
		"pointer arithmetic requires a complete pointee type; got Unknown")
	assertRejects(t,
		"fun demo(p: Ptr<Unknown>) do\n    bad: Int32 := ^p\nend\n",
		"Ptr<Unknown> cannot be dereferenced; recover a concrete pointer type first")
	// Casting to and from an erased pointee succeeds; the recovered concrete
	// pointer then traverses normally.
	result := assertCompiles(t, "fun demo(p: Ptr<Unknown>) do\n    unsafe do\n        typed: Ptr<Int32> := p.cast<Int32>()\n        value: Int32 := typed[0]\n        erased: Ptr<Unknown> := typed.cast<Unknown>()\n    end\nend\n")
	if !strings.Contains(rootC(t, result), "(const int32_t *)hex_v_p") {
		t.Fatalf("modules/app.c = %q, want the recovered pointer cast", rootC(t, result))
	}
}

// Counts and indices are forward-only Size values; no signed or implicitly
// converted numeric operand is admitted.
func TestFencedPointerOperandsRequireSize(t *testing.T) {
	assertRejects(t,
		"fun demo(p: Ptr<Byte>, n: Int32) do\n    unsafe do\n        bad: Ptr<Byte> := p.offset(n)\n    end\nend\n",
		"offset requires Size; got Int32")
	assertRejects(t,
		"fun demo(p: Ptr<Byte>, n: Int64) do\n    unsafe do\n        bad: Byte := p[n]\n    end\nend\n",
		"pointer indexing requires Size; got Int64")
	assertCompiles(t, "fun demo(p: Ptr<Byte>, n: Size) do\n    unsafe do\n        ok: Ptr<Byte> := p.offset(n)\n        value: Byte := p[n]\n    end\nend\n")
}

// A nullable pointer must be narrowed first: the permission region does not
// make Nil a valid address.
func TestFencedPointerOperationsRequireNarrowing(t *testing.T) {
	for _, source := range []string{
		"fun demo(p: Ptr<Byte> | Nil) do\n    unsafe do\n        bad: Ptr<Byte> := p.offset(1)\n    end\nend\n",
		"fun demo(p: Ptr<Byte> | Nil) do\n    unsafe do\n        bad: Byte := p[0]\n    end\nend\n",
		"fun demo(p: Ptr<Byte> | Nil) do\n    unsafe do\n        bad: Ptr<UInt32> := p.cast<UInt32>()\n    end\nend\n",
	} {
		assertRejects(t, source, "may be Nil; narrow it before dereferencing")
	}
	assertCompiles(t, "fun demo(p: Ptr<Byte> | Nil) do\n    if p != nil then\n        unsafe do\n            ok: Ptr<Byte> := p.offset(1)\n            value: Byte := p[0]\n            words: Ptr<UInt32> := p.cast<UInt32>()\n        end\n    end\nend\n")
}

// Released storage keeps its ordinary locally proved diagnostic.
func TestFencedPointerOperationsRejectReleasedStorage(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    p: Ptr<mut Int32> := h.allocate<Int32>(1)\n    h.free(p)\n    unsafe do\n        bad: Ptr<mut Int32> := p.offset(1)\n    end\nend\n",
		"fun demo(h: Heap) do\n    p: Ptr<mut Int32> := h.allocate<Int32>(1)\n    h.free(p)\n    unsafe do\n        bad: Int32 := p[0]\n    end\nend\n",
	} {
		assertRejects(t, source, "released")
	}
}

// The receiver and its one operand each evaluate exactly once, in source
// order, even though C sequences neither `+` nor `[]`.
func TestFencedPointerOperandsEvaluateOnceInOrder(t *testing.T) {
	source := "fun bump(): Size do\n    return 1\nend\nfun source(p: Ptr<Byte>): Ptr<Byte> do\n    return p\nend\nfun demo(p: Ptr<Byte>) do\n    unsafe do\n        moved: Ptr<Byte> := source(p).offset(bump())\n        value: Byte := source(p)[bump()]\n    end\nend\n"
	result := assertCompiles(t, source)
	generated := rootC(t, result)
	if got := strings.Count(generated, "hex_f_m3_app_bump()"); got != 2 {
		t.Fatalf("modules/app.c = %q, want exactly 2 bump evaluations; got %d", generated, got)
	}
	if got := strings.Count(generated, "hex_f_m3_app_source(hex_v_p)"); got != 2 {
		t.Fatalf("modules/app.c = %q, want exactly 2 source evaluations; got %d", generated, got)
	}
	// The receiver's temporary must be declared before the operand's.
	receiverAt := strings.Index(generated, "hex_f_m3_app_source(hex_v_p)")
	countAt := strings.Index(generated, "hex_f_m3_app_bump()")
	if receiverAt < 0 || countAt < 0 || receiverAt > countAt {
		t.Fatalf("modules/app.c = %q, want the receiver hoisted before its count", generated)
	}
}

// A one-past pointer may be formed and compared; nothing new claims it is
// dereferenceable.
func TestOnePastPointerMayBeFormedAndCompared(t *testing.T) {
	assertCompiles(t, "fun demo(p: Ptr<Byte>, n: Size): Bool do\n    unsafe do\n        limit: Ptr<Byte> := p.offset(n)\n        return limit == p\n    end\nend\n")
}

func TestPointerDereferenceThenCheckedIndexIsValid(t *testing.T) {
	result := compileSource("fun demo() do\n    mut values: Array<Int32, 4> := [10, 20, 30, 40]\n    array_pointer: Ptr<mut Array<Int32, 4>> := @values\n    item: Int32 := (^array_pointer)[2]\n    element: Ptr<mut Int32> := @values[2]\n    copy: Int32 := ^element\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestPointerIdentityEqualityRemainsValid(t *testing.T) {
	result := compileSource("fun demo() do\n    value: Int32 := 1\n    other: Int32 := 2\n    left: Ptr<Int32> := @value\n    right: Ptr<Int32> := @other\n    same: Bool := left == right\n    different: Bool := left != right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestPointerIntegerConversionsRejected(t *testing.T) {
	result := compileSource("fun demo() do\n    value: Int32 := 1\n    pointer: Ptr<Int32> := @value\n    bad: UInt64 := pointer\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expected UInt64 initializer; got Ptr<Int32>") {
		t.Fatalf("Compile stderr = %#v, want pointer-to-integer rejection", result.Stderr)
	}
	result = compileSource("fun demo() do\n    address: UInt64 := 42\n    bad: Ptr<Int32> := address\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expected Ptr<Int32> initializer; got UInt64") {
		t.Fatalf("Compile stderr = %#v, want integer-to-pointer rejection", result.Stderr)
	}
}

func TestPointerCompoundAssignmentsAreSyntaxErrors(t *testing.T) {
	for _, source := range []string{
		"fun demo()\n    value: Int32 := 1\n    pointer: Ptr<Int32> := @value\n    pointer += 1\nend",
		"fun demo()\n    pointer: Ptr<Int32> := nil\n    pointer++\nend",
		"fun demo()\n    count: Int32 := 1\n    count++\nend",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("Compile(%q) stderr = %#v, want syntax error", source, result.Stderr)
		}
	}
}

func TestPointerUnknownErasureAddsNoCapability(t *testing.T) {
	result := compileSource("fun demo() do\n    value: Int32 := 1\n    pointer: Ptr<Int32> := @value\n    erased: Ptr<Unknown> := pointer\n    unsafe do\n        bad: Int32 := erased[0]\n    end\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "pointer arithmetic requires a complete pointee type; got Unknown") {
		t.Fatalf("Compile stderr = %#v, want complete-pointee rejection on erased pointer", result.Stderr)
	}
}
