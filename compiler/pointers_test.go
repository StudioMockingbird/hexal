package compiler

// Pointers and mutability: ref typing by place writability, MutPtr weakening,
// declarator lowering, recursive pointer members, and member auto-dereference.
// Specs 0007 and 0008.

import (
	"strings"
	"testing"
)

func TestRefIsTypedByPlaceWritability(t *testing.T) {
	result := Compile("mut score: Int32 = 0 answer: Int32 = 42 writer: MutPtr<Int32> = ref score look: Ptr<Int32> = ref answer")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"int32_t *const hex_v_writer = &hex_v_score;",
		"const int32_t *const hex_v_look = &hex_v_answer;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestPointeeWritabilityControlsValueAssignment(t *testing.T) {
	valid := Compile("mut score: Int32 = 0 writer: MutPtr<Int32> = ref score writer.value = 1")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("MutPtr pointee write failed: %#v", valid.Stderr)
	}

	invalid := Compile("answer: Int32 = 42 look: Ptr<Int32> = ref answer look.value = 1")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "cannot write through a read-only pointer look.value") {
		t.Fatalf("Ptr pointee write = %#v, want read-only-pointer diagnostic", invalid)
	}
}

func TestFixedMutPtrBindingWritesPointeeButRejectsRepointing(t *testing.T) {
	valid := Compile("mut first: Int32 = 1 fixed: MutPtr<Int32> = ref first fixed.value = 2")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("fixed MutPtr pointee write failed: %#v", valid)
	}
	for _, want := range []string{
		"int32_t *const hex_v_fixed = &hex_v_first;",
		"*hex_v_fixed = 2;",
	} {
		if !strings.Contains(valid.MainC, want) {
			t.Fatalf("main.c = %q, want %q", valid.MainC, want)
		}
	}

	invalid := Compile("mut first: Int32 = 1 mut second: Int32 = 2 fixed: MutPtr<Int32> = ref first fixed = ref second")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "cannot assign to constant fixed") {
		t.Fatalf("fixed MutPtr repointing = %#v, want constant-binding diagnostic", invalid)
	}
}

func TestFixedMutPtrMemberWritesPointeeButRejectsMutableReference(t *testing.T) {
	valid := Compile("type Holder = { fixedMember: Int32, pointer: MutPtr<Int32>, } mut value: Int32 = 0 holder: Holder = Holder { fixedMember = 1, pointer = ref value, } holder.pointer.value = 2")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("fixed MutPtr member pointee write failed: %#v", valid)
	}
	if !strings.Contains(valid.MainC, "*hex_v_holder.hex_m_pointer = 2;") {
		t.Fatalf("main.c = %q, want fixed member pointee write", valid.MainC)
	}

	invalid := Compile("type Holder = { fixedMember: Int32, pointer: MutPtr<Int32>, } mut value: Int32 = 0 holder: Holder = Holder { fixedMember = 1, pointer = ref value, } bad: MutPtr<Int32> = ref holder.fixedMember")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "expected MutPtr<Int32> initializer, got Ptr<Int32>") {
		t.Fatalf("fixed member reference = %#v, want MutPtr mismatch", invalid)
	}
}

func TestObjectCopyRetainsMemberMutabilityContract(t *testing.T) {
	valid := Compile("type Player = { id: Int32, mut health: Int32, } mut source: Player = Player { id = 1, health = 100, } mut copy: Player = source copy.health = 50")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("mutable object copy failed: %#v", valid)
	}
	if !strings.Contains(valid.MainC, "hex_v_copy.hex_m_health = 50;") {
		t.Fatalf("main.c = %q, want mutable member assignment in copy", valid.MainC)
	}

	invalid := Compile("type Player = { id: Int32, mut health: Int32, } mut source: Player = Player { id = 1, health = 100, } mut copy: Player = source copy.id = 2")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "cannot assign to read-only member copy.id") {
		t.Fatalf("fixed member through mutable object copy = %#v, want read-only-member diagnostic", invalid)
	}
}

func TestFixedObjectBindingRejectsMutableMemberWrite(t *testing.T) {
	result := Compile("type Player = { id: Int32, mut health: Int32, } mut source: Player = Player { id = 1, health = 100, } copy: Player = source copy.health = 50")
	if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "cannot assign to read-only member copy.health") {
		t.Fatalf("fixed object binding = %#v, want read-only-member diagnostic", result)
	}
}

func TestWholeObjectReplacementRespectsBindingMutability(t *testing.T) {
	valid := Compile("type Player = { id: Int32, mut health: Int32, } mut first: Player = Player { id = 1, health = 100, } second: Player = Player { id = 2, health = 200, } first = second")
	if valid.ExitCode != ExitSuccess || !strings.Contains(valid.MainC, "hex_v_first = hex_v_second;") {
		t.Fatalf("mutable object replacement = %#v, want complete object assignment", valid)
	}

	invalid := Compile("type Player = { id: Int32, mut health: Int32, } first: Player = Player { id = 1, health = 100, } mut second: Player = Player { id = 2, health = 200, } first = second")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "cannot assign to constant first") {
		t.Fatalf("fixed object replacement = %#v, want constant-binding diagnostic", invalid)
	}
}

func TestFixedObjectAndReferenceLowerToConst(t *testing.T) {
	result := Compile("type Point = { x: Int32, mut y: Int32, } point: Point = Point { x = 1, y = 2, } view: Ptr<Point> = ref point")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("fixed object compilation failed: %#v", result)
	}
	for _, want := range []string{
		"const hex_t_Point hex_v_point = (hex_t_Point){",
		"const hex_t_Point *const hex_v_view = &hex_v_point;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestWeakeningDeclarationAndAssignment(t *testing.T) {
	valid := Compile("mut score: Int32 = 0 writer: MutPtr<Int32> = ref score observer: Ptr<Int32> = writer mut reader: Ptr<Int32> = ref score reader = writer")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("weakening compilation failed: %#v", valid.Stderr)
	}
	for _, want := range []string{
		"const int32_t *const hex_v_observer = hex_v_writer;",
		"const int32_t *hex_v_reader = &hex_v_score;",
		"hex_v_reader = hex_v_writer;",
	} {
		if !strings.Contains(valid.MainC, want) {
			t.Fatalf("main.c = %q, want %q", valid.MainC, want)
		}
	}

	invalid := Compile("answer: Int32 = 42 look: Ptr<Int32> = ref answer promoted: MutPtr<Int32> = look")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) != 1 || invalid.Stderr[0] != "[Type Error] expected MutPtr<Int32> initializer, got Ptr<Int32> at 1:76" {
		t.Fatalf("reverse weakening = %#v, want type mismatch", invalid.Stderr)
	}
}

func TestWeakeningIsOutermostLayerOnly(t *testing.T) {
	valid := Compile("mut value: Int32 = 0 mut inner: MutPtr<Int32> = ref value mut outer: MutPtr<MutPtr<Int32>> = ref inner ok: Ptr<MutPtr<Int32>> = outer")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("outermost weakening compilation failed: %#v", valid.Stderr)
	}
	if !strings.Contains(valid.MainC, "int32_t *const *const hex_v_ok = hex_v_outer;") {
		t.Fatalf("main.c = %q, want weakened outermost pointer copy", valid.MainC)
	}

	invalid := Compile("mut value: Int32 = 0 mut inner: MutPtr<Int32> = ref value mut outer: MutPtr<MutPtr<Int32>> = ref inner no: Ptr<Ptr<Int32>> = outer")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "expected Ptr<Ptr<Int32>> initializer, got MutPtr<MutPtr<Int32>>") {
		t.Fatalf("deep weakening = %#v, want inner-layer mismatch", invalid.Stderr)
	}
}

func TestWeakeningThroughObjectMemberInitializer(t *testing.T) {
	valid := Compile("type Config = { name: Ptr<UInt8>, } mut buffer: UInt8 = 65 config: Config = Config { name = ref buffer, }")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("member weakening compilation failed: %#v", valid.Stderr)
	}
	if !strings.Contains(valid.MainC, ".hex_m_name = &hex_v_buffer,") {
		t.Fatalf("main.c = %q, want weakened member pointer initializer", valid.MainC)
	}

	invalid := Compile("type Config = { name: MutPtr<UInt8>, } buffer: UInt8 = 65 config: Config = Config { name = ref buffer, }")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) == 0 {
		t.Fatalf("reverse member weakening = %#v, want type mismatch", invalid)
	}
}

func TestPointerObjectMembers(t *testing.T) {
	// RFC 0010: nil cannot initialize a non-nullable pointer member, and a
	// nullable member takes the null value freely.
	invalid := Compile("type Node = { value: Int32, mut next: MutPtr<Node>, } mut first: Node = Node { value = 1, next = nil, }")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) == 0 || !strings.Contains(strings.Join(invalid.Stderr, "\n"), "expected MutPtr<Node>, got Nil") {
		t.Fatalf("nil into non-nullable member = %#v, want type mismatch", invalid)
	}

	valid := Compile("type Node = { value: Int32, mut next: MutPtr<Node> | Nil, } mut first: Node = Node { value = 1, next = nil, }")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("nil into nullable member failed: %#v", valid.Stderr)
	}
}

func TestSelfRecursiveObjectLowersSplitStruct(t *testing.T) {
	result := Compile("type Node = { value: Int32, mut next: MutPtr<Node>, }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("self-recursive object compilation failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"typedef struct hex_t_Node hex_t_Node;",
		"struct hex_t_Node {",
		"int32_t hex_m_value;",
		"hex_t_Node *hex_m_next;",
	} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want %q", result.MainH, want)
		}
	}
}

func TestSelfRecursiveReadOnlyPointerMemberLowers(t *testing.T) {
	result := Compile("type Node = { value: Int32, next: Ptr<Node>, }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("read-only self-recursive object compilation failed: %v", result.Stderr)
	}
	for _, want := range []string{
		"typedef struct hex_t_Node hex_t_Node;",
		"const hex_t_Node *hex_m_next;",
	} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want %q", result.MainH, want)
		}
	}
}

func TestRejectsByValueAndForwardRecursion(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"type Impossible = { child: Impossible, }", "cannot contain itself by value"},
		{"type A = { b: Ptr<B>, } type B = { a: Ptr<A>, }", "unknown type B"},
		{"type P = { next: Ptr<P>, } type Q = P", ""},
	} {
		result := Compile(testCase.source)
		if testCase.want == "" {
			if result.ExitCode != ExitSuccess {
				t.Fatalf("Compile(%q) = %#v, want success", testCase.source, result.Stderr)
			}
			continue
		}
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPointersAndAliasing(t *testing.T) {
	result := Compile("mut x: Int32 = 42 writer: MutPtr<Int32> = ref x alias: MutPtr<Int32> = writer alias.value = 100 y: Int32 = writer.value")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d: %v", result.ExitCode, result.Stderr, ExitSuccess, result.Stderr)
	}
	for _, want := range []string{
		"int32_t hex_v_x = 42;",
		"int32_t *const hex_v_writer = &hex_v_x;",
		"int32_t *const hex_v_alias = hex_v_writer;",
		"*hex_v_alias = 100;",
		"const int32_t hex_v_y = *hex_v_writer;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
	if strings.Contains(result.MainC, "hexal_alloc") || strings.Contains(result.MainC, "free(") || strings.Contains(result.MainC, "Hexal_Ref") || strings.Contains(result.MainH, "Hexal_Ref") {
		t.Fatalf("generated output contains removed Ref machinery: C=%q H=%q", result.MainC, result.MainH)
	}
}

func TestNestedPointers(t *testing.T) {
	result := Compile("mut x: Int32 = 42 writer: MutPtr<Int32> = ref x writer_pointer: Ptr<MutPtr<Int32>> = ref writer z: Int32 = writer_pointer.value.value")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d: %v", result.ExitCode, result.Stderr, ExitSuccess, result.Stderr)
	}
	for _, want := range []string{"int32_t *const *const hex_v_writer_pointer = &hex_v_writer;", "const int32_t hex_v_z = *(*hex_v_writer_pointer);"} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestAddressOfDereferencePlace(t *testing.T) {
	result := Compile("mut x: Int32 = 42 mut p: MutPtr<Int32> = ref x mut pp: MutPtr<MutPtr<Int32>> = ref p q: Ptr<MutPtr<Int32>> = ref pp.value pp.value = p")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{"int32_t *const *const hex_v_q = &(*hex_v_pp);", "*hex_v_pp = hex_v_p;"} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestPointerAccessAndRebindingModes(t *testing.T) {
	result := Compile("mut y: Int32 = 1 mut z: Int32 = 2 mut reader: Ptr<Int32> = ref y reader = ref z")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d: %v", result.ExitCode, result.Stderr, ExitSuccess, result.Stderr)
	}
	for _, want := range []string{"const int32_t *hex_v_reader = &hex_v_y;", "hex_v_reader = &hex_v_z;"} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestPointerValuedStorePreservesWritability(t *testing.T) {
	result := Compile("mut value: Int32 = 1 mut slot: MutPtr<Int32> = ref value mut slot_pointer: MutPtr<MutPtr<Int32>> = ref slot mut other: Int32 = 2 other_writer: MutPtr<Int32> = ref other slot_pointer.value = other_writer")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d: %v", result.ExitCode, result.Stderr, ExitSuccess, result.Stderr)
	}
	for _, want := range []string{"int32_t **hex_v_slot_pointer = &hex_v_slot;", "*hex_v_slot_pointer = hex_v_other_writer;"} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestBindingsAreConstantUnlessMutable(t *testing.T) {
	result := Compile("mut x: Int32 = 13 x = 14")
	if result.ExitCode != ExitSuccess || !strings.Contains(result.MainC, "int32_t hex_v_x = 13;") || !strings.Contains(result.MainC, "hex_v_x = 14;") {
		t.Fatalf("Compile returned %#v, want mutable binding", result)
	}

	result = Compile("x: Int32 = 13 x = 14")
	if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || result.Stderr[0] != "[Type Error] cannot assign to constant x at 1:15" {
		t.Fatalf("Compile returned %#v, want constant-binding diagnostic", result)
	}
}

func TestPointerDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"p: Ptr<Int32> = 13", "[Type Error] expected Ptr<Int32> initializer, got Int32 at 1:17"},
		{"x: Int32 = 13 p: Ptr<Int32> = x.value", "[Type Error] cannot access .value on Int32; expected Ptr<T> at 1:33"},
		{"mut x: Int32 = 13 p: Ptr<Int32> = ref x q: Ptr<Bool> = p", "[Type Error] expected Ptr<Bool> initializer, got Ptr<Int32> at 1:56"},
		{"mut x: Int32 = 13 p: Ptr<Int32> = ref x q: Ptr<Int32> = ref 42", "[Syntax Error] expected a place identifier at 1:61"},
		{"mut x: Int32 = 13 p: Ptr<Int32> = ref x p.value = 42", "[Type Error] cannot write through a read-only pointer p.value at 1:41"},
		{"x: Int32 = 13 p: Ptr<Int32> = mut ref x", "[Syntax Error] mut is not valid on the right-hand side; use ref value at 1:31"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
			t.Fatalf("Compile(%q) = %#v, want [%q]", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPointerNestingCombinations(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		source       string
		want         string
		runtimeCheck string
	}{
		{name: "Ptr", source: "mut value: Int32 = 0 pointer: Ptr<Int32> = ref value", want: "const int32_t *const hex_v_pointer = &hex_v_value;", runtimeCheck: "hex_v_pointer == &hex_v_value && *hex_v_pointer == hex_v_value"},
		{name: "MutPtr", source: "mut value: Int32 = 0 pointer: MutPtr<Int32> = ref value", want: "int32_t *const hex_v_pointer = &hex_v_value;", runtimeCheck: "hex_v_pointer == &hex_v_value && *hex_v_pointer == hex_v_value"},
		{name: "PtrPtr", source: "mut value: Int32 = 0 inner: Ptr<Int32> = ref value outer: Ptr<Ptr<Int32>> = ref inner", want: "const int32_t *const *const hex_v_outer = &hex_v_inner;", runtimeCheck: "hex_v_outer == &hex_v_inner && *hex_v_outer == hex_v_inner && **hex_v_outer == hex_v_value"},
		{name: "MutPtrPtr", source: "mut value: Int32 = 0 mut inner: Ptr<Int32> = ref value outer: MutPtr<Ptr<Int32>> = ref inner", want: "const int32_t **const hex_v_outer = &hex_v_inner;", runtimeCheck: "hex_v_outer == &hex_v_inner && *hex_v_outer == hex_v_inner && **hex_v_outer == hex_v_value"},
		{name: "PtrMutPtr", source: "mut value: Int32 = 0 inner: MutPtr<Int32> = ref value outer: Ptr<MutPtr<Int32>> = ref inner", want: "int32_t *const *const hex_v_outer = &hex_v_inner;", runtimeCheck: "hex_v_outer == &hex_v_inner && *hex_v_outer == hex_v_inner && **hex_v_outer == hex_v_value"},
		{name: "MutPtrMutPtr", source: "mut value: Int32 = 0 mut inner: MutPtr<Int32> = ref value outer: MutPtr<MutPtr<Int32>> = ref inner", want: "int32_t **const hex_v_outer = &hex_v_inner;", runtimeCheck: "hex_v_outer == &hex_v_inner && *hex_v_outer == hex_v_inner && **hex_v_outer == hex_v_value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := Compile(testCase.source)
			if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
				t.Fatalf("Compile returned %#v, want successful pointer program", result)
			}
			if !strings.Contains(result.MainC, testCase.want) {
				t.Fatalf("main.c = %q, want %q", result.MainC, testCase.want)
			}
			result.MainC = strings.Replace(result.MainC, "    return EXIT_SUCCESS;\n", "    return ("+testCase.runtimeCheck+") ? EXIT_SUCCESS : EXIT_FAILURE;\n", 1)
		})
	}
}
func TestRecursivePtrAndMutPtrObjects(t *testing.T) {
	result := Compile("type Node = { next: Ptr<Node>, mut child: MutPtr<Node>, }")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful recursive object program", result)
	}
	for _, want := range []string{
		"typedef struct hex_t_Node hex_t_Node;",
		"const hex_t_Node *hex_m_next;",
		"hex_t_Node *hex_m_child;",
	} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want %q", result.MainH, want)
		}
	}
}

func TestPointerMemberAutoDereferences(t *testing.T) {
	result := Compile("type Point = { x: Int32, mut y: Int32, } mut pt: Point = Point { x = 1, y = 2, } p: MutPtr<Point> = ref pt a: Int32 = p.y p.y = 5")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("auto-dereference compilation failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const int32_t hex_v_a = (*hex_v_p).hex_m_y;",
		"(*hex_v_p).hex_m_y = 5;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestAutoDereferenceWritabilityFollowsPointeeAndMember(t *testing.T) {
	readOnlyPointee := Compile("type Point = { x: Int32, mut y: Int32, } pt: Point = Point { x = 1, y = 2, } p: Ptr<Point> = ref pt a: Int32 = p.y p.y = 5")
	if readOnlyPointee.ExitCode != ExitFailure || len(readOnlyPointee.Stderr) != 1 || !strings.Contains(readOnlyPointee.Stderr[0], "cannot assign to read-only member p.y") {
		t.Fatalf("Ptr member write = %#v, want read-only-member diagnostic", readOnlyPointee.Stderr)
	}

	fixedMember := Compile("type Point = { x: Int32, mut y: Int32, } mut pt: Point = Point { x = 1, y = 2, } p: MutPtr<Point> = ref pt p.x = 5")
	if fixedMember.ExitCode != ExitFailure || len(fixedMember.Stderr) != 1 || !strings.Contains(fixedMember.Stderr[0], "cannot assign to read-only member p.x") {
		t.Fatalf("MutPtr fixed member write = %#v, want read-only-member diagnostic", fixedMember.Stderr)
	}
}

func TestAutoDereferenceAppliesOneLayerOnly(t *testing.T) {
	direct := Compile("type Point = { x: Int32, mut y: Int32, } pt: Point = Point { x = 1, y = 2, } inner: Ptr<Point> = ref pt outer: Ptr<Ptr<Point>> = ref inner a: Int32 = outer.x")
	if direct.ExitCode != ExitFailure || len(direct.Stderr) != 1 || !strings.Contains(direct.Stderr[0], "cannot access .x on Ptr<Ptr<Point>>") {
		t.Fatalf("two-layer auto-dereference = %#v, want access diagnostic", direct.Stderr)
	}

	explicit := Compile("type Point = { x: Int32, mut y: Int32, } pt: Point = Point { x = 1, y = 2, } inner: Ptr<Point> = ref pt outer: Ptr<Ptr<Point>> = ref inner a: Int32 = outer.value.x")
	if explicit.ExitCode != ExitSuccess {
		t.Fatalf("explicit two-layer dereference failed: %#v", explicit.Stderr)
	}
	if !strings.Contains(explicit.MainC, "const int32_t hex_v_a = (*(*hex_v_outer)).hex_m_x;") {
		t.Fatalf("main.c = %q, want explicit two-layer member read", explicit.MainC)
	}
}

func TestPointerValuePropertyWinsOverMember(t *testing.T) {
	result := Compile("type Box = { value: Int32, } box: Box = Box { value = 7, } p: Ptr<Box> = ref box whole: Box = p.value inner: Int32 = p.value.value")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("value member behind pointer failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const hex_t_Box hex_v_whole = *hex_v_p;",
		"const int32_t hex_v_inner = (*hex_v_p).hex_m_value;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestRefThroughAutoDereferencedMember(t *testing.T) {
	writable := Compile("type Point = { x: Int32, mut y: Int32, } mut pt: Point = Point { x = 1, y = 2, } p: MutPtr<Point> = ref pt q: MutPtr<Int32> = ref p.y")
	if writable.ExitCode != ExitSuccess {
		t.Fatalf("ref through auto-dereference failed: %#v", writable.Stderr)
	}
	if !strings.Contains(writable.MainC, "int32_t *const hex_v_q = &(*hex_v_p).hex_m_y;") {
		t.Fatalf("main.c = %q, want reference to auto-dereferenced member", writable.MainC)
	}

	readOnly := Compile("type Point = { x: Int32, mut y: Int32, } pt: Point = Point { x = 1, y = 2, } p: Ptr<Point> = ref pt q: MutPtr<Int32> = ref p.y")
	if readOnly.ExitCode != ExitFailure || len(readOnly.Stderr) != 1 || !strings.Contains(readOnly.Stderr[0], "expected MutPtr<Int32> initializer, got Ptr<Int32>") {
		t.Fatalf("ref through read-only pointer = %#v, want MutPtr mismatch", readOnly.Stderr)
	}
}

func TestAutoDereferenceRejectsNonObjectPointee(t *testing.T) {
	result := Compile("mut score: Int32 = 0 p: MutPtr<Int32> = ref score a: Int32 = p.x")
	if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "cannot access .x on MutPtr<Int32>; expected Ptr<T> or an object member") {
		t.Fatalf("non-object pointee = %#v, want access diagnostic", result.Stderr)
	}
}

func TestAutoDereferenceMissingMemberNamesSourceSpelling(t *testing.T) {
	result := Compile("type Point = { x: Int32, mut y: Int32, } pt: Point = Point { x = 1, y = 2, } p: Ptr<Point> = ref pt a: Int32 = p.z")
	if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "Point has no member z") {
		t.Fatalf("missing member behind pointer = %#v, want no-member diagnostic", result.Stderr)
	}
}
