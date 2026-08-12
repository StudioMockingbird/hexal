package compiler

// Nominal object types, members, member modes, and header layout. Spec 0006.

import (
	"strings"
	"testing"
)

func TestObjectValuesAndMembers(t *testing.T) {
	result := Compile("type Point = { x: Int32, mut y: Int32, } mut point: Point = Point { y = 2, x = 1, } point.y = 3 read: Int32 = point.x")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_t_Point hex_t_Point;",
		"struct hex_t_Point {",
		"int32_t hex_m_x;",
		"int32_t hex_m_y;",
		"const int32_t hex_v_read = hex_v_point.hex_m_x;",
		"hex_v_point.hex_m_y = 3;",
		".hex_m_x = 1,",
		".hex_m_y = 2,",
	} {
		if !strings.Contains(result.MainH+result.MainC, want) {
			t.Fatalf("generated output = %q, want %q", result.MainH+result.MainC, want)
		}
	}
	if strings.Index(result.MainH, "hex_m_x") > strings.Index(result.MainH, "hex_m_y") {
		t.Fatalf("member definitions are not in declaration order: %q", result.MainH)
	}
	if strings.Index(result.MainC, ".hex_m_x") > strings.Index(result.MainC, ".hex_m_y") {
		t.Fatalf("literal designators are not in declaration order: %q", result.MainC)
	}
}

func TestNominalObjectsAndAliases(t *testing.T) {
	valid := Compile("type Point = { x: Int32, y: Int32, } type Position = Point point: Position = Point { x = 1, y = 2, }")
	if valid.ExitCode != ExitSuccess {
		t.Fatalf("alias-to-object compilation failed: %#v", valid)
	}

	invalid := Compile("type Point = { x: Int32, y: Int32, } type Offset = { x: Int32, y: Int32, } point: Point = Point { x = 1, y = 2, } offset: Offset = point")
	if invalid.ExitCode != ExitFailure || len(invalid.Stderr) != 1 || !strings.Contains(invalid.Stderr[0], "expected Offset initializer, got Point") {
		t.Fatalf("nominal mismatch = %#v, want object identity error", invalid)
	}
}

func TestNestedObjectsAndPointers(t *testing.T) {
	result := Compile("type Point = { mut x: Int32, y: Int32, } type Box = { mut point: Point, } mut box: Box = Box { point = Point { x = 1, y = 2, }, } box.point.x = 3 reader: Ptr<Box> = ref box read: Int32 = reader.value.point.x")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("nested object compilation failed: %#v", result)
	}
	for _, want := range []string{
		"hex_v_box.hex_m_point.hex_m_x = 3;",
		"const int32_t hex_v_read = (*hex_v_reader).hex_m_point.hex_m_x;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestObjectMemberReferencesAndPointerWrites(t *testing.T) {
	result := Compile("type Point = { mut x: Int32, y: Int32, } mut point: Point = Point { x = 1, y = 2, } writer: MutPtr<Point> = ref point writer.value.x = 10 x_pointer: MutPtr<Int32> = ref point.x")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("object member pointer compilation failed: %#v", result)
	}
	for _, want := range []string{
		"(*hex_v_writer).hex_m_x = 10;",
		"int32_t *const hex_v_x_pointer = &hex_v_point.hex_m_x;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestCompleteObjectReplacement(t *testing.T) {
	result := Compile("type Player = { maximum_health: Int32, mut health: Int32, } mut first: Player = Player { maximum_health = 100, health = 80, } mut second: Player = Player { maximum_health = 120, health = 90, } first = second")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("complete object replacement failed: %#v", result)
	}
	if !strings.Contains(result.MainC, "hex_v_first = hex_v_second;") {
		t.Fatalf("main.c = %q, want complete object assignment", result.MainC)
	}

	invalid := Compile("type Player = { maximum_health: Int32, mut health: Int32, } mut player: Player = Player { maximum_health = 100, health = 80, } player.maximum_health = 200")
	if invalid.ExitCode != ExitFailure || !strings.Contains(strings.Join(invalid.Stderr, "\n"), "cannot assign to read-only member") {
		t.Fatalf("read-only member replacement = %#v, want focused member diagnostic", invalid)
	}
}

func TestObjectFloatDependency(t *testing.T) {
	result := Compile("type Metrics = { ratio: Float32, } metrics: Metrics = Metrics { ratio = 3.14, }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("object float compilation failed: %#v", result)
	}
	for _, want := range []string{
		"static_assert(sizeof(float) == 4",
		"FLT_MANT_DIG == 24",
		"float hex_m_ratio;",
	} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want %q", result.MainH, want)
		}
	}
}

func TestObjectDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"type Empty = {}", "object type must declare at least one member"},
		{"type Point = { x: Int32, x: Int32, }", "declares member x more than once"},
		{"type Impossible = { child: Impossible, }", "cannot contain itself by value"},
		{"type Box = { point: Later, } type Later = { x: Int32, }", "unknown type Later"},
		{"type Point = { x: Int32, y: Int32, } point: Point = Point { x = 1, }", "Point literal is missing member y"},
		{"type Point = { x: Int32, } point: Point = Point { x = 1, x = 2, }", "literal initializes member x more than once"},
		{"type Point = { x: Int32, } point: Point = Point { z = 1, }", "Point has no member z"},
		{"type Point = { x: Int32, } point: Point = Point { x = 1, } point.x = 2", "cannot assign to read-only member"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestAddrMemberAndTemporaryRead(t *testing.T) {
	result := Compile("type Point = { x: Int32, addr: Int32, } x: Int32 = Point { x = 1, addr = 2, }.x addr: Int32 = Point { x = 1, addr = 2, }.addr")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("object member named addr failed: %#v", result)
	}
	if !strings.Contains(result.MainC, "hex_m_addr") {
		t.Fatalf("main.c = %q, want ordinary addr member access", result.MainC)
	}

	legacy := Compile("x: Int32 = 1 y: Int32 = x.addr")
	if legacy.ExitCode != ExitFailure || len(legacy.Stderr) != 1 || legacy.Stderr[0] != "[Type Error] '.addr' is no longer supported; use 'ref' at 1:27" {
		t.Fatalf("legacy .addr diagnostic = %#v", legacy.Stderr)
	}
}

func TestObjectHeaderOrdering(t *testing.T) {
	source := "type First = { value: Int32, } type Second = { first: Ptr<First>, } type Third = { second: Ptr<Second>, }"
	result := Compile(source)
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful multi-object program", result)
	}
	repeat := Compile(source)
	if result.MainH != repeat.MainH {
		t.Fatalf("header generation is not deterministic:\nfirst=%q\nrepeat=%q", result.MainH, repeat.MainH)
	}

	forwards := []string{
		"typedef struct hex_t_First hex_t_First;",
		"typedef struct hex_t_Second hex_t_Second;",
		"typedef struct hex_t_Third hex_t_Third;",
	}
	definitions := []string{
		"struct hex_t_First {",
		"struct hex_t_Second {",
		"struct hex_t_Third {",
	}
	firstDefinition := strings.Index(result.MainH, definitions[0])
	previous := -1
	for _, want := range forwards {
		index := strings.Index(result.MainH, want)
		if index <= previous || index >= firstDefinition {
			t.Fatalf("forward typedefs are not source-ordered before definitions: %q", result.MainH)
		}
		previous = index
	}
	previous = firstDefinition - 1
	for _, want := range definitions {
		index := strings.Index(result.MainH, want)
		if index <= previous {
			t.Fatalf("object definitions are not source-ordered: %q", result.MainH)
		}
		previous = index
	}
}
