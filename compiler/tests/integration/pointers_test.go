package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func assertRejectsExactlyOne(t *testing.T, source, want string) {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("expected rejection, but the source compiled:\n%s", source)
	}
	if len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], want) {
		t.Fatalf("diagnostics = %v, want exactly one containing %q:\n%s", result.Stderr, want, source)
	}
}

func TestRefIsTypedByPlaceWritability(t *testing.T) {
	result := compileSource("let mut score: Int32 = 0 let answer: Int32 = 42 let writer: Ptr<mut Int32> = @score let look: Ptr<Int32> = @answer")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"int32_t *const hex_v_writer = &hex_v_score;",
		"const int32_t *const hex_v_look = &hex_v_answer;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestPointeeWritabilityControlsValueAssignment(t *testing.T) {
	valid := compileSource("let mut score: Int32 = 0 let writer: Ptr<mut Int32> = @score ^writer = 1")
	if valid.ExitCode != compiler.ExitSuccess {
		t.Fatalf("MutPtr pointee write failed: %#v", valid.Stderr)
	}

	invalid := compileSource("let answer: Int32 = 42 let look: Ptr<Int32> = @answer ^look = 1")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "cannot write through a read-only pointer ^look") {
		t.Fatalf("Ptr pointee write = %#v, want read-only-pointer diagnostic", invalid)
	}
}

func TestFixedMutPtrBindingWritesPointeeButRejectsRepointing(t *testing.T) {
	valid := compileSource("let mut first: Int32 = 1 let fixed: Ptr<mut Int32> = @first ^fixed = 2")
	if valid.ExitCode != compiler.ExitSuccess {
		t.Fatalf("fixed MutPtr pointee write failed: %#v", valid)
	}
	for _, want := range []string{
		"int32_t *const hex_v_fixed = &hex_v_first;",
		"*hex_v_fixed = 2;",
	} {
		if !strings.Contains(rootC(t, valid), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, valid), want)
		}
	}

	invalid := compileSource("let mut first: Int32 = 1 let mut second: Int32 = 2 let fixed: Ptr<mut Int32> = @first fixed = @second")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "cannot assign to constant fixed") {
		t.Fatalf("fixed MutPtr repointing = %#v, want constant-binding diagnostic", invalid)
	}
}

func TestFixedMutPtrMemberWritesPointeeButRejectsMutableReference(t *testing.T) {
	valid := compileSource("type Holder is struct fixedMember: Int32, pointer: Ptr<mut Int32>, end let mut value: Int32 = 0 let holder: Holder = Holder(fixedMember = 1, pointer = @value, ) ^(holder.pointer) = 2")
	if valid.ExitCode != compiler.ExitSuccess {
		t.Fatalf("fixed MutPtr member pointee write failed: %#v", valid)
	}
	if !strings.Contains(rootC(t, valid), "*hex_v_holder.hex_m_pointer = 2;") {
		t.Fatalf("modules/app.c = %q, want fixed member pointee write", rootC(t, valid))
	}

	invalid := compileSource("type Holder is struct fixedMember: Int32, pointer: Ptr<mut Int32>, end let mut value: Int32 = 0 let holder: Holder = Holder(fixedMember = 1, pointer = @value, ) let bad: Ptr<mut Int32> = @holder.fixedMember")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "expected Ptr<mut Int32> initializer; got Ptr<Int32>") {
		t.Fatalf("fixed member reference = %#v, want MutPtr mismatch", invalid)
	}
}

func TestObjectCopyRetainsMemberMutabilityContract(t *testing.T) {
	valid := compileSource("type Player is struct id: Int32, mut health: Int32, end let mut source: Player = Player(id = 1, health = 100, ) let mut copy: Player = source copy.health = 50")
	if valid.ExitCode != compiler.ExitSuccess {
		t.Fatalf("mutable object copy failed: %#v", valid)
	}
	if !strings.Contains(rootC(t, valid), "hex_v_copy.hex_m_health = 50;") {
		t.Fatalf("modules/app.c = %q, want mutable member assignment in copy", rootC(t, valid))
	}

	invalid := compileSource("type Player is struct id: Int32, mut health: Int32, end let mut source: Player = Player(id = 1, health = 100, ) let mut copy: Player = source copy.id = 2")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "cannot assign to read-only member copy.id") {
		t.Fatalf("fixed member through mutable object copy = %#v, want read-only-member diagnostic", invalid)
	}
}

func TestFixedObjectBindingRejectsMutableMemberWrite(t *testing.T) {
	result := compileSource("type Player is struct id: Int32, mut health: Int32, end let mut source: Player = Player(id = 1, health = 100, ) let copy: Player = source copy.health = 50")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "cannot assign to read-only member copy.health") {
		t.Fatalf("fixed object binding = %#v, want read-only-member diagnostic", result)
	}
}

func TestWholeObjectReplacementRespectsBindingMutability(t *testing.T) {
	valid := compileSource("type Player is struct id: Int32, mut health: Int32, end let mut first: Player = Player(id = 1, health = 100, ) let second: Player = Player(id = 2, health = 200, ) first = second")
	if valid.ExitCode != compiler.ExitSuccess || !strings.Contains(rootC(t, valid), "hex_v_first = hex_v_second;") {
		t.Fatalf("mutable object replacement = %#v, want complete object assignment", valid)
	}

	invalid := compileSource("type Player is struct id: Int32, mut health: Int32, end let first: Player = Player(id = 1, health = 100, ) let mut second: Player = Player(id = 2, health = 200, ) first = second")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "cannot assign to constant first") {
		t.Fatalf("fixed object replacement = %#v, want constant-binding diagnostic", invalid)
	}
}

func TestFixedObjectAndReferenceLowerToConst(t *testing.T) {
	result := compileSource("type Point is struct x: Int32, mut y: Int32, end let point: Point = Point(x = 1, y = 2, ) let view: Ptr<Point> = @point")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("fixed object compilation failed: %#v", result)
	}
	for _, want := range []string{
		"const hex_t_m3_app_Point hex_v_point = (hex_t_m3_app_Point){",
		"const hex_t_m3_app_Point *const hex_v_view = &hex_v_point;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestWeakeningDeclarationAndAssignment(t *testing.T) {
	valid := compileSource("let mut score: Int32 = 0 let writer: Ptr<mut Int32> = @score let observer: Ptr<Int32> = writer let mut reader: Ptr<Int32> = @score reader = writer")
	if valid.ExitCode != compiler.ExitSuccess {
		t.Fatalf("weakening compilation failed: %#v", valid.Stderr)
	}
	for _, want := range []string{
		"const int32_t *const hex_v_observer = hex_v_writer;",
		"const int32_t *hex_v_reader = &hex_v_score;",
		"hex_v_reader = hex_v_writer;",
	} {
		if !strings.Contains(rootC(t, valid), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, valid), want)
		}
	}

	invalid := compileSource("let answer: Int32 = 42 let look: Ptr<Int32> = @answer let promoted: Ptr<mut Int32> = look")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) != 1 || invalid.Stderr[0] != "[Type Error] expected Ptr<mut Int32> initializer; got Ptr<Int32> at app.hex:1:86" {
		t.Fatalf("reverse weakening = %#v, want type mismatch", invalid.Stderr)
	}
}

func TestWeakeningIsOutermostLayerOnly(t *testing.T) {
	valid := compileSource("let mut value: Int32 = 0 let mut inner: Ptr<mut Int32> = @value let mut outer: Ptr<mut Ptr<mut Int32>> = @inner let ok: Ptr<Ptr<mut Int32>> = outer")
	if valid.ExitCode != compiler.ExitSuccess {
		t.Fatalf("outermost weakening compilation failed: %#v", valid.Stderr)
	}
	if !strings.Contains(rootC(t, valid), "int32_t *const *const hex_v_ok = hex_v_outer;") {
		t.Fatalf("modules/app.c = %q, want weakened outermost pointer copy", rootC(t, valid))
	}

	invalid := compileSource("let mut value: Int32 = 0 let mut inner: Ptr<mut Int32> = @value let mut outer: Ptr<mut Ptr<mut Int32>> = @inner let no: Ptr<Ptr<Int32>> = outer")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "expected Ptr<Ptr<Int32>> initializer; got Ptr<mut Ptr<mut Int32>>") {
		t.Fatalf("deep weakening = %#v, want inner-layer mismatch", invalid.Stderr)
	}
}

func TestWeakeningThroughObjectMemberInitializer(t *testing.T) {
	valid := compileSource("type Config is struct name: Ptr<UInt8>, end let mut buffer: UInt8 = 65 let config: Config = Config(name = @buffer, )")
	if valid.ExitCode != compiler.ExitSuccess {
		t.Fatalf("member weakening compilation failed: %#v", valid.Stderr)
	}
	if !strings.Contains(rootC(t, valid), ".hex_m_name = &hex_v_buffer,") {
		t.Fatalf("modules/app.c = %q, want weakened member pointer initializer", rootC(t, valid))
	}

	invalid := compileSource("type Config is struct name: Ptr<mut UInt8>, end let buffer: UInt8 = 65 let config: Config = Config(name = @buffer, )")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) == 0 {
		t.Fatalf("reverse member weakening = %#v, want type mismatch", invalid)
	}
}

func TestPointerObjectMembers(t *testing.T) {
	invalid := compileSource("type Node is struct value: Int32, mut next: Ptr<mut Node>, end let mut first: Node = Node(value = 1, next = nil, )")
	if invalid.ExitCode != compiler.ExitFailure || len(invalid.Stderr) == 0 || !strings.Contains(strings.Join(invalid.Stderr, "\n"), "nil requires an expected union containing Nil") {
		t.Fatalf("nil into non-nullable member = %#v, want standalone-nil diagnostic", invalid)
	}

	valid := compileSource("type Node is struct value: Int32, mut next: Ptr<mut Node> | Nil, end let mut first: Node = Node(value = 1, next = nil, )")
	if valid.ExitCode != compiler.ExitSuccess {
		t.Fatalf("nil into nullable member failed: %#v", valid.Stderr)
	}
}

func TestSelfRecursiveObjectLowersSplitStruct(t *testing.T) {
	result := compileSource("type Node is struct value: Int32, mut next: Ptr<mut Node>, end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("self-recursive object compilation failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"typedef struct hex_t_m3_app_Node hex_t_m3_app_Node;",
		"struct hex_t_m3_app_Node {",
		"int32_t hex_m_value;",
		"hex_t_m3_app_Node *hex_m_next;",
	} {
		if !strings.Contains(rootH(t, result), want) {
			t.Fatalf("modules/app.h = %q, want %q", rootH(t, result), want)
		}
	}
}

func TestSelfRecursiveReadOnlyPointerMemberLowers(t *testing.T) {
	result := compileSource("type Node is struct value: Int32, next: Ptr<Node>, end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("read-only self-recursive object compilation failed: %v", result.Stderr)
	}
	for _, want := range []string{
		"typedef struct hex_t_m3_app_Node hex_t_m3_app_Node;",
		"const hex_t_m3_app_Node *hex_m_next;",
	} {
		if !strings.Contains(rootH(t, result), want) {
			t.Fatalf("modules/app.h = %q, want %q", rootH(t, result), want)
		}
	}
}

func TestRejectsByValueAndForwardRecursion(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"type Impossible is struct child: Impossible, end", "cannot contain itself by value"},
		{"type A is struct b: Ptr<B>, end type B is struct a: Ptr<A>, end", "unknown type B"},
		{"type P is struct next: Ptr<P>, end type Q is P", ""},
	} {
		result := compileSource(testCase.source)
		if testCase.want == "" {
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("Compile(%q) = %#v, want success", testCase.source, result.Stderr)
			}
			continue
		}
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPointersAndAliasing(t *testing.T) {
	result := compileSource("let mut x: Int32 = 42 let writer: Ptr<mut Int32> = @x let alias: Ptr<mut Int32> = writer ^alias = 100 let y: Int32 = ^writer")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d: %v", result.ExitCode, result.Stderr, compiler.ExitSuccess, result.Stderr)
	}
	for _, want := range []string{
		"int32_t hex_v_x = 42;",
		"int32_t *const hex_v_writer = &hex_v_x;",
		"int32_t *const hex_v_alias = hex_v_writer;",
		"*hex_v_alias = 100;",
		"const int32_t hex_v_y = *hex_v_writer;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
	if strings.Contains(rootC(t, result), "hexal_alloc") || strings.Contains(rootC(t, result), "free(") || strings.Contains(rootC(t, result), "Hexal_Ref") || strings.Contains(rootH(t, result), "Hexal_Ref") {
		t.Fatalf("generated output contains removed Ref machinery: C := %q H=%q", rootC(t, result), rootH(t, result))
	}
}

func TestNestedPointers(t *testing.T) {
	result := compileSource("let mut x: Int32 = 42 let writer: Ptr<mut Int32> = @x let writer_pointer: Ptr<Ptr<mut Int32>> = @writer let z: Int32 = ^(^writer_pointer)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d: %v", result.ExitCode, result.Stderr, compiler.ExitSuccess, result.Stderr)
	}
	for _, want := range []string{"int32_t *const *const hex_v_writer_pointer = &hex_v_writer;", "const int32_t hex_v_z = *(*hex_v_writer_pointer);"} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestAddressOfDereferencePlace(t *testing.T) {
	result := compileSource("let mut x: Int32 = 42 let mut p: Ptr<mut Int32> = @x let mut pp: Ptr<mut Ptr<mut Int32>> = @p let q: Ptr<Ptr<mut Int32>> = @^pp ^pp = p")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{"int32_t *const *const hex_v_q = &(*hex_v_pp);", "*hex_v_pp = hex_v_p;"} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestPointerAccessAndRebindingModes(t *testing.T) {
	result := compileSource("let mut y: Int32 = 1 let mut z: Int32 = 2 let mut reader: Ptr<Int32> = @y reader = @z")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d: %v", result.ExitCode, result.Stderr, compiler.ExitSuccess, result.Stderr)
	}
	for _, want := range []string{"const int32_t *hex_v_reader = &hex_v_y;", "hex_v_reader = &hex_v_z;"} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestPointerValuedStorePreservesWritability(t *testing.T) {
	result := compileSource("let mut value: Int32 = 1 let mut slot: Ptr<mut Int32> = @value let mut slot_pointer: Ptr<mut Ptr<mut Int32>> = @slot let mut other: Int32 = 2 let other_writer: Ptr<mut Int32> = @other ^slot_pointer = other_writer")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d: %v", result.ExitCode, result.Stderr, compiler.ExitSuccess, result.Stderr)
	}
	for _, want := range []string{"int32_t **hex_v_slot_pointer = &hex_v_slot;", "*hex_v_slot_pointer = hex_v_other_writer;"} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestBindingsAreConstantUnlessMutable(t *testing.T) {
	result := compileSource("let mut x: Int32 = 13 x = 14")
	if result.ExitCode != compiler.ExitSuccess || !strings.Contains(rootC(t, result), "int32_t hex_v_x = 13;") || !strings.Contains(rootC(t, result), "hex_v_x = 14;") {
		t.Fatalf("Compile returned %#v, want mutable binding", result)
	}

	result = compileSource("let x: Int32 = 13 x = 14")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || result.Stderr[0] != "[Type Error] cannot assign to constant x at app.hex:1:19" {
		t.Fatalf("Compile returned %#v, want constant-binding diagnostic", result)
	}
}

func TestPointerDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"let p: Ptr<Int32> = 13", "[Type Error] expected Ptr<Int32> initializer; got Int32 at app.hex:1:21"},
		{"let x: Int32 = 13 let p: Ptr<Int32> = ^x", "[Type Error] cannot dereference Int32; ^ requires Ptr<T> at app.hex:1:39"},
		{"let mut x: Int32 = 13 let p: Ptr<Int32> = @x let q: Ptr<Bool> = p", "[Type Error] expected Ptr<Bool> initializer; got Ptr<Int32> at app.hex:1:65"},
		{"let mut x: Int32 = 13 let p: Ptr<Int32> = @x let q: Ptr<Int32> = @42", "[Syntax Error] expected a place identifier at app.hex:1:67"},
		{"let mut x: Int32 = 13 let p: Ptr<Int32> = @x ^p = 42", "[Type Error] cannot write through a read-only pointer ^p at app.hex:1:46"},
		{"let x: Int32 = 13 let p: Ptr<Int32> = mut @x", "[Syntax Error] mut is not valid on the right-hand side; use @value at app.hex:1:39"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
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
		{name: "Ptr", source: "let mut value: Int32 = 0 let pointer: Ptr<Int32> = @value", want: "const int32_t *const hex_v_pointer = &hex_v_value;", runtimeCheck: "hex_v_pointer == &hex_v_value && *hex_v_pointer == hex_v_value"},
		{name: "MutPtr", source: "let mut value: Int32 = 0 let pointer: Ptr<mut Int32> = @value", want: "int32_t *const hex_v_pointer = &hex_v_value;", runtimeCheck: "hex_v_pointer == &hex_v_value && *hex_v_pointer == hex_v_value"},
		{name: "PtrPtr", source: "let mut value: Int32 = 0 let inner: Ptr<Int32> = @value let outer: Ptr<Ptr<Int32>> = @inner", want: "const int32_t *const *const hex_v_outer = &hex_v_inner;", runtimeCheck: "hex_v_outer == &hex_v_inner && *hex_v_outer == hex_v_inner && **hex_v_outer == hex_v_value"},
		{name: "MutPtrPtr", source: "let mut value: Int32 = 0 let mut inner: Ptr<Int32> = @value let outer: Ptr<mut Ptr<Int32>> = @inner", want: "const int32_t **const hex_v_outer = &hex_v_inner;", runtimeCheck: "hex_v_outer == &hex_v_inner && *hex_v_outer == hex_v_inner && **hex_v_outer == hex_v_value"},
		{name: "PtrMutPtr", source: "let mut value: Int32 = 0 let inner: Ptr<mut Int32> = @value let outer: Ptr<Ptr<mut Int32>> = @inner", want: "int32_t *const *const hex_v_outer = &hex_v_inner;", runtimeCheck: "hex_v_outer == &hex_v_inner && *hex_v_outer == hex_v_inner && **hex_v_outer == hex_v_value"},
		{name: "MutPtrMutPtr", source: "let mut value: Int32 = 0 let mut inner: Ptr<mut Int32> = @value let outer: Ptr<mut Ptr<mut Int32>> = @inner", want: "int32_t **const hex_v_outer = &hex_v_inner;", runtimeCheck: "hex_v_outer == &hex_v_inner && *hex_v_outer == hex_v_inner && **hex_v_outer == hex_v_value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
				t.Fatalf("Compile returned %#v, want successful pointer program", result)
			}
			if !strings.Contains(rootC(t, result), testCase.want) {
				t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), testCase.want)
			}
			root := rootC(t, result)
			root = strings.Replace(root, "    return EXIT_SUCCESS;\n", "    return ("+testCase.runtimeCheck+") ? EXIT_SUCCESS : EXIT_FAILURE;\n", 1)
		})
	}
}
func TestRecursivePtrAndMutPtrObjects(t *testing.T) {
	result := compileSource("type Node is struct next: Ptr<Node>, mut child: Ptr<mut Node>, end")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful recursive object program", result)
	}
	for _, want := range []string{
		"typedef struct hex_t_m3_app_Node hex_t_m3_app_Node;",
		"const hex_t_m3_app_Node *hex_m_next;",
		"hex_t_m3_app_Node *hex_m_child;",
	} {
		if !strings.Contains(rootH(t, result), want) {
			t.Fatalf("modules/app.h = %q, want %q", rootH(t, result), want)
		}
	}
}

func TestPointerMemberAutoDereferences(t *testing.T) {
	result := compileSource("type Point is struct x: Int32, mut y: Int32, end let mut pt: Point = Point(x = 1, y = 2, ) let p: Ptr<mut Point> = @pt let a: Int32 = p.y p.y = 5")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("auto-dereference compilation failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const int32_t hex_v_a = (*hex_v_p).hex_m_y;",
		"(*hex_v_p).hex_m_y = 5;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestAutoDereferenceWritabilityFollowsPointeeAndMember(t *testing.T) {
	readOnlyPointee := compileSource("type Point is struct x: Int32, mut y: Int32, end let pt: Point = Point(x = 1, y = 2, ) let p: Ptr<Point> = @pt let a: Int32 = p.y p.y = 5")
	if readOnlyPointee.ExitCode != compiler.ExitFailure || len(readOnlyPointee.Stderr) != 1 || !strings.Contains(readOnlyPointee.Stderr[0], "cannot assign to read-only member p.y") {
		t.Fatalf("Ptr member write = %#v, want read-only-member diagnostic", readOnlyPointee.Stderr)
	}

	fixedMember := compileSource("type Point is struct x: Int32, mut y: Int32, end let mut pt: Point = Point(x = 1, y = 2, ) let p: Ptr<mut Point> = @pt p.x = 5")
	if fixedMember.ExitCode != compiler.ExitFailure || len(fixedMember.Stderr) != 1 || !strings.Contains(fixedMember.Stderr[0], "cannot assign to read-only member p.x") {
		t.Fatalf("MutPtr fixed member write = %#v, want read-only-member diagnostic", fixedMember.Stderr)
	}
}

func TestAutoDereferenceAppliesOneLayerOnly(t *testing.T) {
	direct := compileSource("type Point is struct x: Int32, mut y: Int32, end let pt: Point = Point(x = 1, y = 2, ) let inner: Ptr<Point> = @pt let outer: Ptr<Ptr<Point>> = @inner let a: Int32 = outer.x")
	if direct.ExitCode != compiler.ExitFailure || len(direct.Stderr) != 1 || !strings.Contains(direct.Stderr[0], "cannot access .x on Ptr<Ptr<Point>>") {
		t.Fatalf("two-layer auto-dereference = %#v, want access diagnostic", direct.Stderr)
	}

	explicit := compileSource("type Point is struct x: Int32, mut y: Int32, end let pt: Point = Point(x = 1, y = 2, ) let inner: Ptr<Point> = @pt let outer: Ptr<Ptr<Point>> = @inner let a: Int32 = (^outer).x")
	if explicit.ExitCode != compiler.ExitSuccess {
		t.Fatalf("explicit two-layer dereference failed: %#v", explicit.Stderr)
	}
	if !strings.Contains(rootC(t, explicit), "const int32_t hex_v_a = (*(*hex_v_outer)).hex_m_x;") {
		t.Fatalf("modules/app.c = %q, want explicit two-layer member read", rootC(t, explicit))
	}
}

func TestPointerValuePropertyWinsOverMember(t *testing.T) {
	result := compileSource("type Box is struct value: Int32, end let box: Box = Box(value = 7, ) let p: Ptr<Box> = @box let whole: Box = ^p let inner: Int32 = (^p).value")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("value member behind pointer failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const hex_t_m3_app_Box hex_v_whole = *hex_v_p;",
		"const int32_t hex_v_inner = (*hex_v_p).hex_m_value;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestRefThroughAutoDereferencedMember(t *testing.T) {
	writable := compileSource("type Point is struct x: Int32, mut y: Int32, end let mut pt: Point = Point(x = 1, y = 2, ) let p: Ptr<mut Point> = @pt let q: Ptr<mut Int32> = @p.y")
	if writable.ExitCode != compiler.ExitSuccess {
		t.Fatalf("@through auto-dereference failed: %#v", writable.Stderr)
	}
	if !strings.Contains(rootC(t, writable), "int32_t *const hex_v_q = &(*hex_v_p).hex_m_y;") {
		t.Fatalf("modules/app.c = %q, want reference to auto-dereferenced member", rootC(t, writable))
	}

	readOnly := compileSource("type Point is struct x: Int32, mut y: Int32, end let pt: Point = Point(x = 1, y = 2, ) let p: Ptr<Point> = @pt let q: Ptr<mut Int32> = @p.y")
	if readOnly.ExitCode != compiler.ExitFailure || len(readOnly.Stderr) != 1 || !strings.Contains(readOnly.Stderr[0], "expected Ptr<mut Int32> initializer; got Ptr<Int32>") {
		t.Fatalf("@through read-only pointer = %#v, want MutPtr mismatch", readOnly.Stderr)
	}
}

func TestAutoDereferenceRejectsNonObjectPointee(t *testing.T) {
	result := compileSource("let mut score: Int32 = 0 let p: Ptr<mut Int32> = @score let a: Int32 = p.x")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "cannot access .x on Ptr<mut Int32>; use ^p to access the pointee") {
		t.Fatalf("non-object pointee = %#v, want access diagnostic", result.Stderr)
	}
}

func TestAutoDereferenceMissingMemberNamesSourceSpelling(t *testing.T) {
	result := compileSource("type Point is struct x: Int32, mut y: Int32, end let pt: Point = Point(x = 1, y = 2, ) let p: Ptr<Point> = @pt let a: Int32 = p.z")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "Point has no member z") {
		t.Fatalf("missing member behind pointer = %#v, want no-member diagnostic", result.Stderr)
	}
}

func TestRefAcceptsMixedMemberIndexPlaces(t *testing.T) {
	accepted := []string{
		"type Row is struct value: Int32 end\nfun f() do\n    let rows: Array<Row, 2> = [Row(value = 1), Row(value = 2)]\n    let p: Ptr<Int32> = @rows[0].value\nend\n",
		"type Row is struct mut value: Int32 end\nfun f() do\n    let mut rows: Array<Row, 2> = [Row(value = 1), Row(value = 2)]\n    let p: Ptr<mut Int32> = @rows[0].value\nend\n",
		"type Cell is struct mut value: Int32 end\ntype Box is struct mut cells: Array<Cell, 2> end\nfun f() do\n    let mut grid: Array<Box, 2> = [Box(cells = [Cell(value = 1), Cell(value = 2)]), Box(cells = [Cell(value = 3), Cell(value = 4)])]\n    let p: Ptr<mut Int32> = @grid[0].cells[1].value\nend\n",
		"type Row is struct mut values: Array<Int32, 2> end\nfun f() do\n    let mut pair: Row = Row(values = [1, 2])\n    let p: Ptr<mut Int32> = @pair.values[0]\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
	// A fixed member downgrades the final place to Ptr even under a writable root.
	rejected := "type Row is struct value: Int32 end\nfun f() do\n    let mut rows: Array<Row, 2> = [Row(value = 1), Row(value = 2)]\n    let p: Ptr<mut Int32> = @rows[0].value\nend\n"
	if result := compileSource(rejected); result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want fixed-member @downgraded to Ptr; got accept:\n%s", rejected)
	}
}

func TestHeapFreeRejectsRFCBoundaries(t *testing.T) {
	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "direct reference",
			source: `let h: Heap = Heap()
let mut x: Int32 = 1
h.free(@x)
`,
			want: "free does not accept a pointer into this function's local storage",
		},
		{
			name: "reference binding",
			source: `let h: Heap = Heap()
let mut x: Int32 = 1
let p: Ptr<mut Int32> = @x
h.free(p)
`,
			want: "free does not accept a pointer into this function's local storage",
		},
		{
			name: "double free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "double free through alias",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
h.free(p)
h.free(q)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "double free through alias of alias",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
let r: Ptr<mut Int32> = q
h.free(p)
h.free(r)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "use after free through alias",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
h.free(p)
let value: Int32 = ^q
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "use after free through alias of freed alias",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
h.free(q)
let value: Int32 = ^p
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "alias free inside defer",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
defer h.free(p)
h.free(q)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "alias on every branch",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let flag: Bool = true
if flag then
    let q: Ptr<mut Int32> = p
    h.free(q)
else
    let q: Ptr<mut Int32> = p
    h.free(q)
end
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "member read double free",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
let h: Heap = Heap()
let holder: Holder = Holder(pointer = h.allocate<Int32>(0), )
let q: Ptr<mut Int32> = holder.pointer
h.free(q)
h.free(q)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "union-typed target keeps original tracking",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> | Nil = p
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "use after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
let value: Int32 = ^p
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "deferred free after explicit free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "both branches free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let flag: Bool = true
if flag then
    h.free(p)
else
    h.free(p)
end
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "outer defer after terminating branches",
			source: `fun finish(flag: Bool, h: Heap): Int32 do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    defer h.free(p)
    if flag then
        h.free(p)
        return 1
    else
        h.free(p)
        return 2
    end
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "deferred capture after reallocation",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
h.free(p)
p = h.allocate<Int32>(1)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "deferred capture after branch reallocation",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
h.free(p)
let flag: Bool = true
if flag then
    p = h.allocate<Int32>(1)
else
    p = h.allocate<Int32>(2)
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "method receiver after free",
			source: `type Point is struct value: Int32, end
method Point.read(): Int32 do
    return self.value
end
let h: Heap = Heap()
let p: Ptr<mut Point> = h.allocate<Point>(Point(value = 1, ))
h.free(p)
let value: Int32 = p.read()
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "volatile read after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut UInt32> = h.allocate<UInt32>(0)
h.free(p)
let value: UInt32 = p.read_volatile()
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "volatile write after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut UInt32> = h.allocate<UInt32>(0)
h.free(p)
p.write_volatile(1)
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "deferred expression after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
defer ^p
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "deferred compound volatile read after free",
			source: `let h: Heap = Heap()
let p: Ptr<mut UInt32> = h.allocate<UInt32>(0)
h.free(p)
defer p.read_volatile() + 1
`,
			want: "this pointer's storage was released on every path to this point",
		},
		{
			name: "spawn preserves identity",
			source: `fun worker(p: Ptr<mut Int32>): Bool do
    return true
end
fun run(h: Heap): Int32 | Error do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    let task: Task<Bool> = try spawn worker(p)
    task.join()
    h.free(p)
    h.free(p)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "channel send preserves identity",
			source: `fun run(h: Heap): Int32 | Error do
    let channel: Channel<Ptr<mut Int32>> = try Channel<Ptr<mut Int32>>(h, 1)
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    try channel.send(p)
    h.free(p)
    h.free(p)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "return preserves identity",
			source: `fun pick(h: Heap): Ptr<mut Int32> do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    return p
end
fun run(h: Heap): Int32 | Error do
    let p: Ptr<mut Int32> = pick(h)
    h.free(p)
    h.free(p)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "call argument preserves identity",
			source: `fun consume(p: Ptr<mut Int32>) do
end
let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
consume(p)
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "member store preserves identity",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let holder: Holder = Holder(pointer = p, )
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "collection store preserves identity",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let slots: Array<Ptr<mut Int32>, 1> = [p]
h.free(p)
h.free(p)
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "join result receives fresh local fact",
			source: `fun make(h: Heap): Ptr<mut Int32> do
    return h.allocate<Int32>(0)
end
fun run(h: Heap): Int32 | Error do
    let task: Task<Ptr<mut Int32>> = try spawn make(h)
    let p: Ptr<mut Int32> = task.join()
    h.free(p)
    h.free(p)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
		{
			name: "channel receive receives fresh local fact",
			source: `fun run(h: Heap): Int32 | Error do
    let channel: Channel<Ptr<mut Int32>> = try Channel<Ptr<mut Int32>>(h, 1)
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    try channel.send(p)
    let step: Ptr<mut Int32> | EoS = channel.receive()
    if step is EoS then
        return 0
    end
    let received: Ptr<mut Int32> = step
    h.free(received)
    h.free(received)
    return 0
end
`,
			want: "free releases storage already released on every path to this point",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, testCase.want)
		})
	}
}

func TestHeapFreeReportsOneDiagnosticForTerminatingReturnPaths(t *testing.T) {
	assertRejectsExactlyOne(t, `fun finish(flag: Bool, h: Heap): Int32 do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
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

func TestHeapFreeAcceptsUntrackedAndSafeCases(t *testing.T) {
	testCases := []struct {
		name   string
		source string
	}{
		{
			name: "allocator pointer",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(5)
h.free(p)
`,
		},
		{
			name: "parameter",
			source: `fun release(h: Heap, p: Ptr<mut Int32>) do
    h.free(p)
end
`,
		},
		{
			name: "object member",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
fun release(h: Heap, holder: Holder) do
    h.free(holder.pointer)
end
`,
		},
		{
			name: "collection element",
			source: `fun release(h: Heap, pointers: Array<Ptr<mut Int32>, 1>) do
    h.free(pointers[0])
end
`,
		},
		{
			name: "reallocation after free",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
p = h.allocate<Int32>(1)
h.free(p)
`,
		},
		{
			name: "reassignment of alias relabels",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let mut q: Ptr<mut Int32> = p
q = h.allocate<Int32>(2)
h.free(p)
h.free(q)
`,
		},
		{
			name: "reassignment of original relabels",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
let q: Ptr<mut Int32> = p
p = h.allocate<Int32>(2)
h.free(q)
h.free(p)
`,
		},
		{
			name: "alias on one branch only",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let flag: Bool = true
if flag then
    let q: Ptr<mut Int32> = p
    h.free(q)
end
h.free(p)
`,
		},
		{
			name: "loop preserves loop-head state",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
let mut i: Int32 = 0
while i < 3 do
    let q: Ptr<mut Int32> = p
    i = i + 1
end
h.free(p)
`,
		},
		{
			name: "member read independent of heap pointer",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let holder: Holder = Holder(pointer = p, )
let q: Ptr<mut Int32> = holder.pointer
h.free(p)
h.free(q)
`,
		},
		{
			name: "copied stash handle forms no alias",
			source: `let st: Stash<Int32> = Stash<Int32>()
let st2: Stash<Int32> = st
st.destroy()
st2.destroy()
`,
		},
		{
			name: "copied pool handle forms no alias",
			source: `let pl: Pool<Int32> = Pool<Int32>(4)
let pl2: Pool<Int32> = pl
pl.destroy()
pl2.destroy()
`,
		},
		{
			name: "reallocation after free",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
p = h.allocate<Int32>(1)
h.free(p)
`,
		},
		{
			name: "one branch free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
let flag: Bool = true
if flag then
    h.free(p)
end
`,
		},
		{
			name: "leak",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
`,
		},
		{
			name: "defer-only cleanup",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
`,
		},
		{
			name: "defer timing before action",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
let value: Int32 = ^p
`,
		},
		{
			name: "deferred expression after reallocation",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
defer ^p
p = h.allocate<Int32>(1)
`,
		},
		{
			name: "deferred capture with branch-reassigned pointer",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
defer h.free(p)
let flag: Bool = true
if flag then
    p = h.allocate<Int32>(1)
else
    p = h.allocate<Int32>(2)
end
h.free(p)
`,
		},
		{
			name: "deferred action after unreachable return",
			source: `fun finish(h: Heap): Int32 do
    let p: Ptr<mut Int32> = h.allocate<Int32>(0)
    h.free(p)
    return 1
    defer h.free(p)
end
`,
		},
		{
			name: "passing freed pointer",
			source: `fun consume(p: Ptr<mut Int32>) do
end
let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
h.free(p)
consume(p)
`,
		},
		{
			name: "offset and cast stay fresh identities",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
unsafe do
    let q: Ptr<mut Int32> = p.offset(0)
    let r: Ptr<mut Int32> = p.cast<Int32>()
    h.free(p)
    h.free(q)
    h.free(r)
end
`,
		},
		{
			name: "escaped binding accepts double free",
			source: `let h: Heap = Heap()
let mut p: Ptr<mut Int32> = h.allocate<Int32>(0)
let slot: Ptr<mut Ptr<mut Int32>> = @p
h.free(p)
h.free(p)
`,
		},
		{
			name: "heap allocate then heap free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate<Int32>(1)
h.free(p)
`,
		},
		{
			name: "aligned heap allocate then heap free",
			source: `let h: Heap = Heap()
let p: Ptr<mut Int32> = h.allocate_aligned<Int32>(1, 64)
h.free(p)
`,
		},
		{
			name: "same pool allocate then free",
			source: `let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
pl.free(p)
`,
		},
		{
			name: "heap free of parameter",
			source: `fun release(h: Heap, p: Ptr<mut Int32>) do
    h.free(p)
end
`,
		},
		{
			name: "heap free of member read",
			source: `type Holder is struct pointer: Ptr<mut Int32>, end
fun release(h: Heap, holder: Holder) do
    h.free(holder.pointer)
end
`,
		},
		{
			name: "heap free of collection element",
			source: `fun release(h: Heap, pointers: Array<Ptr<mut Int32>, 1>) do
    h.free(pointers[0])
end
`,
		},
		{
			name: "heap free of call result",
			source: `fun make(h: Heap): Ptr<mut Int32> do
    return h.allocate<Int32>(0)
end
fun release(h: Heap) do
    let p: Ptr<mut Int32> = make(h)
    h.free(p)
end
`,
		},
		{
			name: "pool free of parameter",
			source: `fun release(pl: Pool<Int32>, p: Ptr<mut Int32>) do
    pl.free(p)
end
`,
		},
		{
			name: "offset and cast release through any allocator",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = h.allocate<Int32>(0)
unsafe do
    let q: Ptr<mut Int32> = p.offset(0)
    let r: Ptr<mut Int32> = p.cast<Int32>()
    pl.free(q)
    h.free(r)
end
`,
		},
		{
			name: "conditional alias does not carry kind past join",
			source: `let h: Heap = Heap()
let mut q: Ptr<mut Int32> = h.allocate<Int32>(0)
let pl: Pool<Int32> = Pool<Int32>(4)
let flag: Bool = true
if flag then
    q = pl.allocate(1)
end
h.free(q)
`,
		},
		{
			name: "alias of pool pointer released through same pool",
			source: `let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
let q: Ptr<mut Int32> = p
pl.free(q)
`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertCompiles(t, testCase.source)
		})
	}
}

// A release through an allocator that provably did not produce the pointer is
// rejected with the real source allocator; unknown kinds stay accepted.
func TestCrossAllocatorReleaseRejects(t *testing.T) {
	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "stash allocation through Heap free",
			source: `let h: Heap = Heap()
let st: Stash<Int32> = Stash<Int32>()
let p: Ptr<mut Int32> = st.allocate(1)
h.free(p)
`,
			want: "free does not accept a pointer allocated from a Stash",
		},
		{
			name: "pool allocation through Heap free",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
h.free(p)
`,
			want: "free does not accept a pointer allocated from a Pool",
		},
		{
			name: "heap allocation through Pool free",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = h.allocate<Int32>(1)
pl.free(p)
`,
			want: "Pool free does not accept a pointer allocated from the Heap",
		},
		{
			name: "aligned heap allocation through Pool free",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = h.allocate_aligned<Int32>(1, 64)
pl.free(p)
`,
			want: "Pool free does not accept a pointer allocated from the Heap",
		},
		{
			name: "stash allocation through Pool free",
			source: `let st: Stash<Int32> = Stash<Int32>()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = st.allocate(1)
pl.free(p)
`,
			want: "Pool free does not accept a pointer allocated from a Stash",
		},
		{
			name: "allocation from a different Pool",
			source: `let a: Pool<Int32> = Pool<Int32>(4)
let b: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = a.allocate(1)
b.free(p)
`,
			want: "pointer was allocated from a different Pool",
		},
		{
			name: "aliased pool allocation through Heap free",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
let q: Ptr<mut Int32> = p
h.free(q)
`,
			want: "free does not accept a pointer allocated from a Pool",
		},
		{
			name: "deferred Heap free of pool allocation",
			source: `let h: Heap = Heap()
let pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
defer h.free(p)
`,
			want: "free does not accept a pointer allocated from a Pool",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejectsExactlyOne(t, testCase.source, testCase.want)
		})
	}
}

// The pointee matrix: managed collections, text, slices, functions,
// and Nil cannot be pointed to; Tasks, Channels, Mutexes, and ordinary types
// can. (Direct Atomic pointees are covered in concurrency_test.go.)
func TestPointeeEligibilityMatrix(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"String", "fun f(h: Heap) do\n    let s: String = \"x\".copy(h)\n    let p: Ptr<String> = @s\nend\n"},
		{"List", "fun f(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    let p: Ptr<List<Int32>> = @values\nend\n"},
		{"Dict", "fun f(h: Heap) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    let p: Ptr<Dict<Int32, Int32>> = @d\nend\n"},
		{"View", "fun f() do\n    let v: Slice<Int32> = Slice<Int32>.empty()\n    let p: Ptr<Slice<Int32>> = @v\nend\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, "could not construct pointer type")
		})
	}
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"Task", "fun worker(): Bool do\n    return true\nend\nfun f(h: Heap): Int32 | Error do\n    let task: Task<Bool> = try spawn worker()\n    let p: Ptr<Task<Bool>> = @task\n    return 0\nend\n"},
		{"Channel", "fun f(h: Heap): Int32 | Error do\n    let channel: Channel<Int32> = try Channel<Int32>(h, 4)\n    let p: Ptr<Channel<Int32>> = @channel\n    return 0\nend\n"},
		{"Mutex", "fun f(h: Heap): Int32 | Error do\n    let mutex: Mutex = try Mutex(h)\n    let p: Ptr<Mutex> = @mutex\n    return 0\nend\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertCompiles(t, testCase.source)
		})
	}
}
