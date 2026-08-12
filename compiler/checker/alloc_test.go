package checker

import (
	"testing"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func TestCheckHeapNewProducesHeapValue(t *testing.T) {
	checked, err := Check(parseProgram(t, "h: Heap = Heap.new()"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Type != compilerTypes.Heap || !compilerTypes.IsHeap(declaration.Source.Type) {
		t.Fatalf("source = %#v, want Heap value", declaration.Source)
	}
}

func TestCheckHeapAllocateReturnsMutPtr(t *testing.T) {
	checked, err := Check(parseProgram(t, "h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0)"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[1].(Declaration)
	if declaration.Source.Node.Kind != HeapAllocateExpression || declaration.Source.Node.ResultType.PointeeWritable == false || declaration.Source.Type.Name != "MutPtr<Int32>" {
		t.Fatalf("source = %#v, want MutPtr<Int32> allocation", declaration.Source)
	}
}

func TestCheckHeapFreeAcceptsPtrAndMutPtr(t *testing.T) {
	requireAccepted(t, "h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) defer h.free(p)")
	requireAccepted(t, "h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) reader: Ptr<Int32> = p defer h.free(reader)")
}

func TestCheckHeapFreeRequiresHeapReceiver(t *testing.T) {
	requireDiagnostic(t, "h: Heap = Heap.new() mut v: Int32 = 1 h.free(v)", "value is not an allocation produced by this Heap")
}

func TestCheckHeapAllocateRejectsIncompleteAndFunctionTypes(t *testing.T) {
	requireDiagnostic(t, "h: Heap = Heap.new() p: MutPtr<Unknown> = h.allocate<Unknown>(nil)", "allocation requires a complete finite type")
	requireDiagnostic(t, "fun f(): Int32 return 1 end h: Heap = Heap.new() p: MutPtr<Fun<(Int32) : Int32>> = h.allocate<Fun<(Int32) : Int32>>(f)", "allocation requires a complete finite type")
}

func TestCheckHeapAllocateRequiresExplicitInitializer(t *testing.T) {
	requireDiagnostic(t, "h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>()", "allocation requires an explicit initializer")
}

func TestCheckDeferCapturesDirectCall(t *testing.T) {
	checked, err := Check(parseProgram(t, "fun record(value: Int32) end mut value: Int32 = 1 defer record(value) value = 2"))
	if err != nil {
		t.Fatal(err)
	}
	statement := checked.Statements[2].(DeferStatement)
	if !statement.Action.IsCall {
		t.Fatalf("defer statement = %#v, want captured direct call", statement)
	}
}

func TestCheckDeferDefersNonCallExpression(t *testing.T) {
	checked, err := Check(parseProgram(t, "mut value: Int32 = 1 defer value"))
	if err != nil {
		t.Fatal(err)
	}
	statement := checked.Statements[1].(DeferStatement)
	if statement.Action.IsCall {
		t.Fatalf("defer statement = %#v, want exit-time expression", statement)
	}
}

func TestCheckDeferInsideBranchIsBranchScoped(t *testing.T) {
	checked, err := Check(parseProgram(t, "h: Heap = Heap.new() flag: Bool = true if flag p: MutPtr<Int32> = h.allocate<Int32>(0) defer h.free(p) end"))
	if err != nil {
		t.Fatal(err)
	}
	branch := checked.Statements[2].(IfStatement)
	if len(branch.ThenDefers) != 1 {
		t.Fatalf("then defers = %d, want 1", len(branch.ThenDefers))
	}
}

func TestCheckDeferLoopBodyIterationScoped(t *testing.T) {
	checked, err := Check(parseProgram(t, "h: Heap = Heap.new() mut flag: Bool = true while flag do p: MutPtr<Int32> = h.allocate<Int32>(0) defer h.free(p) flag = false end"))
	if err != nil {
		t.Fatal(err)
	}
	loop := checked.Statements[2].(WhileStatement)
	if len(loop.BodyDefers) != 1 {
		t.Fatalf("body defers = %d, want 1", len(loop.BodyDefers))
	}
}

func TestCheckDeferRejectsDeclarationBody(t *testing.T) {
	tokens, err := lexer.Lex("h: Heap = Heap.new() defer p: Int32 = 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(tokens); err == nil {
		t.Fatal("Parse accepted a declaration after defer")
	}
}
