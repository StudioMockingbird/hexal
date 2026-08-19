package checker

import (
	"testing"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func requireExactlyOneDiagnostic(t *testing.T, source, want string) {
	t.Helper()
	_, err := checkSource(t, source)
	if err == nil {
		t.Fatalf("Check accepted %q, want one diagnostic %q", source, want)
	}
	diagnostics, ok := err.(compilerTypes.Diagnostics)
	if !ok {
		t.Fatalf("Check error type = %T, want Diagnostics", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Message != want {
		t.Fatalf("Check diagnostics = %v, want exactly one %q", diagnostics, want)
	}
}

func TestCheckHeapNewProducesHeapValue(t *testing.T) {
	checked, err := Check(parseProgram(t, "h: Heap := Heap.new()"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Type != compilerTypes.Heap || !compilerTypes.IsHeap(declaration.Source.Type) {
		t.Fatalf("source = %#v, want Heap value", declaration.Source)
	}
}

func TestCheckHeapAllocateReturnsMutPtr(t *testing.T) {
	checked, err := Check(parseProgram(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0)"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[1].(Declaration)
	if declaration.Source.Node.Kind != HeapAllocateExpression || declaration.Source.Node.ResultType.PointeeWritable == false || declaration.Source.Type.Name != "MutPtr<Int32>" {
		t.Fatalf("source = %#v, want MutPtr<Int32> allocation", declaration.Source)
	}
}

func TestCheckHeapFreeAcceptsPtrAndMutPtr(t *testing.T) {
	requireAccepted(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) h.free(p)")
	requireAccepted(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) defer h.free(p)")
	requireAccepted(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) reader: Ptr<Int32> := p defer h.free(reader)")
}

func TestCheckHeapFreeRejectsLocalStoragePointers(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{
			name:   "direct reference",
			source: "h: Heap := Heap.new() mut x: Int32 := 1 h.free(ref x)",
		},
		{
			name:   "reference binding",
			source: "h: Heap := Heap.new() mut x: Int32 := 1 p: MutPtr<Int32> := ref x h.free(p)",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			requireDiagnostic(t, testCase.source, "free does not accept a pointer into this function's local storage")
		})
	}
}

func TestCheckHeapFreeRejectsFreedStorage(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "double free",
			source: "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) h.free(p) h.free(p)",
			want:   "free releases storage already released on every path to this point",
		},
		{
			name:   "use after free",
			source: "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) h.free(p) value: Int32 := p.value",
			want:   "this pointer's storage was released on every path to this point",
		},
		{
			name:   "both branches free",
			source: "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) flag: Bool := true if flag then h.free(p) else h.free(p) end h.free(p)",
			want:   "free releases storage already released on every path to this point",
		},
		{
			name: "outer defer after terminating branches",
			source: `fun finish(flag: Bool, h: Heap): Int32 do
	p: MutPtr<Int32> := h.allocate<Int32>(0)
	defer h.free(p)
	if flag then
		h.free(p)
		return 1
	else
		h.free(p)
		return 2
	end
end`,
			want: "free releases storage already released on every path to this point",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			requireDiagnostic(t, testCase.source, testCase.want)
		})
	}
}

func TestCheckHeapFreeClearsFreedStateAfterReallocation(t *testing.T) {
	requireAccepted(t, "h: Heap := Heap.new() mut p: MutPtr<Int32> := h.allocate<Int32>(0) h.free(p) p = h.allocate<Int32>(1) h.free(p)")
}

func TestCheckHeapFreeDefersValidationUntilScopeExit(t *testing.T) {
	requireAccepted(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) defer h.free(p)")
	requireAccepted(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) defer h.free(p) value: Int32 := p.value")
}

func TestCheckDeferredExpressionChecksStateAtScopeExit(t *testing.T) {
	requireAccepted(t, "h: Heap := Heap.new() mut p: MutPtr<Int32> := h.allocate<Int32>(0) h.free(p) defer p.value p = h.allocate<Int32>(1)")
	requireDiagnostic(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) h.free(p) defer p.value", "this pointer's storage was released on every path to this point")
	requireDiagnostic(t, "h: Heap := Heap.new() p: MutPtr<UInt32> := h.allocate<UInt32>(0) h.free(p) defer p.read_volatile() + 1", "this pointer's storage was released on every path to this point")
}

func TestCheckDeferredExpressionChecksVolatilePointeeKinds(t *testing.T) {
	state := newFlowState()
	const binding = BindingID(1)
	state.trackFreed(binding)
	state.markFreed(binding)
	receiver := Expression{Kind: VariableExpression, Binding: binding}
	for _, kind := range []ExpressionKind{VolatileReadExpression, VolatileWriteExpression} {
		expression := Expression{Kind: kind, Operand: &receiver}
		diagnostic := checkDeferredExpression(&expression, lexer.Token{Line: 1, Column: 1}, state)
		if diagnostic == nil || diagnostic.Message != "this pointer's storage was released on every path to this point" {
			t.Fatalf("kind %v diagnostic = %#v, want use-after-free", kind, diagnostic)
		}
	}
}

func TestCheckHeapFreeRejectsDeferredFreeAfterExplicitFree(t *testing.T) {
	requireDiagnostic(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) defer h.free(p) h.free(p)", "free releases storage already released on every path to this point")
}

func TestCheckHeapFreeRejectsDeferredCaptureAfterReallocation(t *testing.T) {
	requireDiagnostic(t, "h: Heap := Heap.new() mut p: MutPtr<Int32> := h.allocate<Int32>(0) defer h.free(p) h.free(p) p = h.allocate<Int32>(1)", "free releases storage already released on every path to this point")
}

func TestCheckHeapFreePreservesDeferredReleaseAcrossBranches(t *testing.T) {
	requireDiagnostic(t, `h: Heap := Heap.new()
mut p: MutPtr<Int32> := h.allocate<Int32>(0)
defer h.free(p)
h.free(p)
flag: Bool := true
if flag then
	p = h.allocate<Int32>(1)
else
	p = h.allocate<Int32>(2)
end
`, "free releases storage already released on every path to this point")
}

func TestCheckHeapFreeReportsOneDiagnosticForTerminatingReturnPaths(t *testing.T) {
	requireExactlyOneDiagnostic(t, `fun finish(flag: Bool, h: Heap): Int32 do
	p: MutPtr<Int32> := h.allocate<Int32>(0)
	defer h.free(p)
	if flag then
		h.free(p)
		return 1
	else
		h.free(p)
		return 2
	end
end
`, "free releases storage already released on every path to this point")
}

func TestCheckHeapFreeIgnoresDeferredActionAfterUnreachableReturn(t *testing.T) {
	requireAccepted(t, `fun finish(h: Heap): Int32 do
	p: MutPtr<Int32> := h.allocate<Int32>(0)
	h.free(p)
	return 1
	defer h.free(p)
	return 2
end
`)
}

func TestCheckHeapFreeRejectsFreedPointerMethodReceiver(t *testing.T) {
	requireDiagnostic(t, `type Point = { value: Int32, }
impl Point.read(): Int32 do
	return self.value
end
h: Heap := Heap.new()
p: MutPtr<Point> := h.allocate<Point>(Point { value = 1, })
h.free(p)
value: Int32 := p.read()
`, "this pointer's storage was released on every path to this point")
}

func TestCheckHeapFreeRejectsFreedPointerVolatileAccess(t *testing.T) {
	for _, source := range []string{
		"h: Heap := Heap.new() p: MutPtr<UInt32> := h.allocate<UInt32>(0) h.free(p) value: UInt32 := p.read_volatile()",
		"h: Heap := Heap.new() p: MutPtr<UInt32> := h.allocate<UInt32>(0) h.free(p) p.write_volatile(1)",
	} {
		requireDiagnostic(t, source, "this pointer's storage was released on every path to this point")
	}
}

func TestCheckHeapFreeRequiresHeapReceiver(t *testing.T) {
	requireDiagnostic(t, "h: Heap := Heap.new() mut v: Int32 := 1 h.free(v)", "value is not an allocation produced by this Heap")
}

func TestCheckHeapAllocateRejectsIncompleteAndFunctionTypes(t *testing.T) {
	requireDiagnostic(t, "h: Heap := Heap.new() p: MutPtr<Unknown> := h.allocate<Unknown>(nil)", "allocation requires a complete finite type")
	requireDiagnostic(t, "fun f(): Int32 do return 1 end h: Heap := Heap.new() p: MutPtr<Fun<(Int32) : Int32>> := h.allocate<Fun<(Int32) : Int32>>(f)", "allocation requires a complete finite type")
}

func TestCheckHeapAllocateRequiresExplicitInitializer(t *testing.T) {
	requireDiagnostic(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>()", "allocation requires an explicit initializer")
}

func TestCheckDeferCapturesDirectCall(t *testing.T) {
	checked, err := Check(parseProgram(t, "fun record(value: Int32) do end mut value: Int32 := 1 defer record(value) value = 2"))
	if err != nil {
		t.Fatal(err)
	}
	statement := checked.Statements[2].(DeferStatement)
	if !statement.Action.IsCall {
		t.Fatalf("defer statement = %#v, want captured direct call", statement)
	}
}

func TestCheckDeferDefersNonCallExpression(t *testing.T) {
	checked, err := Check(parseProgram(t, "mut value: Int32 := 1 defer value"))
	if err != nil {
		t.Fatal(err)
	}
	statement := checked.Statements[1].(DeferStatement)
	if statement.Action.IsCall {
		t.Fatalf("defer statement = %#v, want exit-time expression", statement)
	}
}

func TestCheckDeferInsideBranchIsBranchScoped(t *testing.T) {
	checked, err := Check(parseProgram(t, "h: Heap := Heap.new() flag: Bool := true if flag then p: MutPtr<Int32> := h.allocate<Int32>(0) defer h.free(p) end"))
	if err != nil {
		t.Fatal(err)
	}
	branch := checked.Statements[2].(IfStatement)
	if len(branch.ThenDefers) != 1 {
		t.Fatalf("then defers = %d, want 1", len(branch.ThenDefers))
	}
}

func TestCheckDeferLoopBodyIterationScoped(t *testing.T) {
	checked, err := Check(parseProgram(t, "h: Heap := Heap.new() mut flag: Bool := true while flag do p: MutPtr<Int32> := h.allocate<Int32>(0) defer h.free(p) flag = false end"))
	if err != nil {
		t.Fatal(err)
	}
	loop := checked.Statements[2].(WhileStatement)
	if len(loop.BodyDefers) != 1 {
		t.Fatalf("body defers = %d, want 1", len(loop.BodyDefers))
	}
}

func TestCheckDeferRejectsDeclarationBody(t *testing.T) {
	tokens, err := lexer.Lex("h: Heap := Heap.new() defer p: Int32 := 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(tokens); err == nil {
		t.Fatal("Parse accepted a declaration after defer")
	}
}
