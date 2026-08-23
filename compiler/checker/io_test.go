package checker

import (
	"testing"

	compilerTypes "hexal/compiler/types"
)

// The standard-handle constructors are fallible and carry their capability
// through try narrowing into the binding's flow facts.
func TestCheckStreamConstructorsSeedCapability(t *testing.T) {
	checked := requireAccepted(t,
		"fun demo(): Nil | Error do\n"+
			"    out: IO := try IO.stdout()\n"+
			"    err: IO | Error := IO.stderr()\n"+
			"    return nil\nend\n")
	var demo FunctionDeclaration
	for _, statement := range checked.Statements {
		if function, ok := statement.(FunctionDeclaration); ok && function.Name == "demo" {
			demo = function
		}
	}
	if demo.Name != "demo" || len(demo.Body) != 3 {
		t.Fatalf("demo body = %#v, want three statements", demo.Body)
	}
	outcome := demo.Body[0].(Declaration)
	node := outcome.Source.Node
	if node.Kind != TryExpression || node.Operand == nil || node.Operand.Kind != StreamConstructorExpression || node.Operand.Name != "stdout" {
		t.Fatalf("initializer = %#v, want a try over the stdout constructor", outcome.Source.Node)
	}
}

// A proven read/write mismatch is a checker error at the call; an unknown
// capability defers to the runtime access-mask check and checks clean.
func TestCheckStreamCapabilityTiers(t *testing.T) {
	requireDiagnostic(t,
		"fun relay(text: String): Nil | Error do\n"+
			"    input: IO := try IO.stdin()\n"+
			"    input.write(text.bytes())\n"+
			"    return nil\nend\n",
		"stream is not writable")
	requireDiagnostic(t,
		"fun echo(h: Heap): Nil | Error do\n"+
			"    buf: List<Byte> := List<Byte>.new(h)\n"+
			"    out: IO := try IO.stdout()\n"+
			"    out.read(buf, 8)\n"+
			"    return nil\nend\n",
		"stream is not readable")
	requireAccepted(t,
		"fun relay(handle: IO, text: String): Nil | Error do\n"+
			"    handle.write(text.bytes())\n"+
			"    return nil\nend\n")
}

// Bytes state-changing operations take MutPtr<Bytes>: a mutable binding
// auto-addresses through the shared receiver rule and a fixed binding is
// rejected.
func TestCheckBytesReceiverForms(t *testing.T) {
	requireDiagnostic(t,
		"fun demo(h: Heap): Nil | Error do\n"+
			"    data: List<Byte> := List<Byte>.new(h)\n"+
			"    dest: List<Byte> := List<Byte>.new(h)\n"+
			"    fixed: Bytes := Bytes.over(data)\n"+
			"    return fixed.read(dest, 4)\nend\n",
		"read needs MutPtr<Bytes>; ref fixed is Ptr<Bytes>")
	requireAccepted(t,
		"fun demo(h: Heap, text: String): Nil | Error do\n"+
			"    data: List<Byte> := List<Byte>.new(h)\n"+
			"    dest: List<Byte> := List<Byte>.new(h)\n"+
			"    mut live: Bytes := Bytes.over(data)\n"+
			"    r: Size | EoS | Error := live.read(dest, 4)\n"+
			"    w: Size | Error := live.write(text.bytes())\n"+
			"    s: Size | Error := live.seek(Seek.Start { position = 0 })\n"+
			"    return nil\nend\n")
}

// Close records a versioned closed fact: use after close rejects, rebinding
// after close checks clean again, and both branches closing keeps the proof
// after the merge.
func TestCheckIOCloseFacts(t *testing.T) {
	requireDiagnostic(t,
		"fun demo(): Nil | Error do\n"+
			"    out: IO := try IO.stdout()\n"+
			"    first: Nil | Error := out.close()\n"+
			"    second: Nil | Error := out.close()\n"+
			"    return nil\nend\n",
		"this stream was closed on every path to this point")
	requireAccepted(t,
		"fun demo(flag: Bool): Nil | Error do\n"+
			"    mut out: IO := try IO.stdout()\n"+
			"    if flag then\n"+
			"        closed: Nil | Error := out.close()\n"+
			"        out = try IO.stderr()\n"+
			"    end\n"+
			"    final: Nil | Error := out.close()\n"+
			"    return nil\nend\n")
}

// The borrow edge survives copies and rejects use after the source list was
// freed where the local facts still hold.
func TestCheckBytesProvenance(t *testing.T) {
	requireDiagnostic(t,
		"fun demo(h: Heap): Nil | Error do\n"+
			"    src: List<Byte> := List<Byte>.new(h)\n"+
			"    dst: List<Byte> := List<Byte>.new(h)\n"+
			"    mut stream: Bytes := Bytes.over(src)\n"+
			"    src.free(h)\n"+
			"    r: Size | EoS | Error := stream.read(dst, 4)\n"+
			"    return nil\nend\n",
		"memory stream outlives its source list, freed on every path to this point")
	requireDiagnostic(t,
		"fun demo(h: Heap): Nil | Error do\n"+
			"    src: List<Byte> := List<Byte>.new(h)\n"+
			"    src.free(h)\n"+
			"    late: Bytes := Bytes.over(src)\n"+
			"    return nil\nend\n",
		"memory stream outlives its source list, freed on every path to this point")
	requireAccepted(t,
		"fun demo(h: Heap): Nil | Error do\n"+
			"    src: List<Byte> := List<Byte>.new(h)\n"+
			"    dst: List<Byte> := List<Byte>.new(h)\n"+
			"    mut stream: Bytes := Bytes.over(src)\n"+
			"    mut copy: Bytes := stream\n"+
			"    r: Size | EoS | Error := copy.read(dst, 4)\n"+
			"    return nil\nend\n")
}

// Placement follows the bootstrap matrix exactly.
func TestCheckStreamPlacementMatrix(t *testing.T) {
	rejections := []struct{ name, source string }{
		{"object member", "type Box = { stream: IO, }"},
		{"ADT payload", "type Held = | Carry as { stream: IO }"},
		{"array element", "type Box = { pair: Array<IO, 2>, }"},
		{"bytes object member", "type Box = { stream: Bytes, }"},
	}
	for _, testCase := range rejections {
		if _, err := checkSource(t, testCase.source); err == nil {
			t.Fatalf("%s placement was accepted", testCase.name)
		}
	}
	listElement := "fun demo(h: Heap): Nil | Error do\n    streams: List<IO> := List<IO>.new(h)\n    return nil\nend"
	if _, err := checkSource(t, listElement); err == nil {
		t.Fatal("list element placement was accepted")
	}
	channelElement := "fun demo(h: Heap): Nil | Error do\n    pipe: Channel<IO> := Channel<IO>.new(h, 1)\n    return nil\nend"
	if _, err := checkSource(t, channelElement); err == nil {
		t.Fatal("channel element placement was accepted")
	}
	requireAccepted(t,
		"fun worker(stream: IO): Nil | Error do\n    return nil\nend\n"+
			"fun demo(): Nil | Error do\n"+
			"    task: Task<Nil | Error> := try spawn worker(try IO.stdout())\n"+
			"    outcome: Nil | Error := task.join()\n"+
			"    return outcome\nend\n")
	requireDiagnostic(t,
		"fun take(stream: Bytes): Size do\n    return 0\nend\n"+
			"fun demo(h: Heap): Nil | Error do\n"+
			"    data: List<Byte> := List<Byte>.new(h)\n"+
			"    task: Task<Size> := spawn take(Bytes.over(data))\n"+
			"    joined: Size | Error := task.join()\n"+
			"    return nil\nend\n",
		"task entry arguments must be complete and shallow-copyable")
}

// Seek variants construct through the ordinary qualified path with named
// payloads, and the unqualified variant names stay free.
func TestCheckSeekConstruction(t *testing.T) {
	requireAccepted(t,
		"fun demo(): Nil | Error do\n"+
			"    start: Seek := Seek.Start { position = 4096 }\n"+
			"    offset: Seek := Seek.Current { offset = -1 }\n"+
			"    back: Seek := Seek.End { offset = -8 }\n"+
			"    return nil\nend\n")
	requireAccepted(t, "Start: Int32 := 0 Current: Int32 := 0 End: Int32 := 0")
}

// The three stream type names are protected; Start/Current/End are not, and
// IO carries no generic parameter.
func TestCheckStreamNamesAreReserved(t *testing.T) {
	requireDiagnostic(t, "type IO = { x: Int32, }", "built-in type IO cannot be redeclared")
	requireDiagnostic(t, "type Bytes = { x: Int32, }", "built-in type Bytes cannot be redeclared")
	requireDiagnostic(t, "type Seek = | North | South", "built-in type Seek cannot be redeclared")
	requireDiagnostic(t, "fun f(x: IO<Byte>): Nil | Error do\n    return nil\nend\n", "unknown generic type IO")
}

// Every stream operation shares one canonical structural result union per
// shape, carrying exactly the members its contract names.
func TestCheckStreamResultUnionsAreCanonical(t *testing.T) {
	checked := requireAccepted(t,
		"h: Heap := Heap.new()\n"+
			"src: List<Byte> := List<Byte>.new(h)\n"+
			"mut stream: Bytes := Bytes.over(src)\n"+
			"r: Size | EoS | Error := stream.read(src, 4)\n")
	read := checked.Statements[3].(Declaration)
	node := read.Source.Node
	if node.Kind != StreamMethodCallExpression || node.Name != "read" {
		t.Fatalf("initializer node = %#v, want a stream read call", node)
	}
	hasSize, hasEoS, hasError := false, false, false
	readMembers := compilerTypes.UnionMembers(node.ResultType)
	for index := 0; index < readMembers.Len(); index++ {
		member, _ := readMembers.At(index)
		switch {
		case compilerTypes.Equal(member, compilerTypes.SizeType):
			hasSize = true
		case compilerTypes.IsEoS(member):
			hasEoS = true
		case compilerTypes.IsError(member):
			hasError = true
		}
	}
	if readMembers.Len() != 3 || !hasSize || !hasEoS || !hasError {
		t.Fatalf("read union = %s, want exactly Size, EoS, and Error", node.ResultType.Name)
	}
}
