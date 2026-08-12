package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Smoke-check that a representative array/view program generates C that gcc
// accepts with -std=c23. Not part of the default suite gate (needs gcc).
func TestGeneratedArrayViewCCompiles(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "type Pair = { mut values: Array<Int32, 2>, }\nfun sum(values: View<Int32>): Int32\n    return values[0] + values[1]\nend\nfun demo()\n    mut pair: Pair = Pair { values = [3, 4], }\n    view: View<Int32> = pair.values.slice(0, 2)\n    total: Int32 = sum(view)\n    last: Int32 = view.at(1)\n    pair.values[0] = 9\nend"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	dir := t.TempDir()
	mainC := filepath.Join(dir, "main.c")
	mainH := filepath.Join(dir, "main.h")
	if err := os.WriteFile(mainC, []byte(result.MainC), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainH, []byte(result.MainH), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("gcc", "-std=c23", "-Wall", "-Wextra", "-c", mainC, "-o", filepath.Join(dir, "main.o"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
}

// Smoke-check that an owning List program generates C that gcc accepts with
// -std=c23: growth, bounds traps, nested String copy-in/move-out/destruction.
func TestGeneratedListCCompiles(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "fun demo(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    values.set(0, 9)\n    first: Int32 = values.at(0)\n    values[1] = 5\n    last: Int32 = values.pop()\n    values.clear()\n    values.push(7)\n    view: View<Int32> = values.slice(0, 1)\n    total: Int32 = view[0]\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\n    names.push(\"alice\")\n    names.push(\"bob\")\n    popped: String = names.pop()\n    popped.free(h)\n    name: String = names.at(0)\nend"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	dir := t.TempDir()
	mainC := filepath.Join(dir, "main.c")
	mainH := filepath.Join(dir, "main.h")
	if err := os.WriteFile(mainC, []byte(result.MainC), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainH, []byte(result.MainH), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("gcc", "-std=c23", "-Wall", "-Wextra", "-c", mainC, "-o", filepath.Join(dir, "main.o"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
}

// Smoke-check that an owning Dict program generates C that gcc accepts with
// -std=c23: hashing, probing, growth, String copy-in/move-out/destruction.
func TestGeneratedDictCCompiles(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "fun demo(h: Heap)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    present: Bool = scores.contains(1)\n    first: Int32 = scores.get(1)\n    removed: Int32 = scores.remove(2)\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    score: Int32 = labels.get(\"alice\")\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"bob\")\n    name: String = people.get(1)\nend"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	dir := t.TempDir()
	mainC := filepath.Join(dir, "main.c")
	mainH := filepath.Join(dir, "main.h")
	if err := os.WriteFile(mainC, []byte(result.MainC), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainH, []byte(result.MainH), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("gcc", "-std=c23", "-Wall", "-Wextra", "-c", mainC, "-o", filepath.Join(dir, "main.o"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
}

// Smoke-check that an RFC 0024 comparison program generates C that gcc
// accepts with -std=c23: lossless widening, deep object/sequence equality,
// pointer identity, and String ordering.
func TestGeneratedEqualityCCompiles(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "type Point = { x: Int32, y: Int32, }\ntype Shape = | Circle as { r: Int32, } | Square as { a: Int32, }\nfun demo(h: Heap)\n    left: Point = Point { x = 1, y = 2, }\n    right: Point = Point { x = 1, y = 2, }\n    same: Bool = left == right\n    different: Bool = left != right\n    i32: Int32 = 1\n    i64: Int64 = 2\n    widened: Bool = i32 == i64\n    text: String = \"abc\"\n    other: String = \"abd\"\n    textOrder: Bool = text < other\n    fixed: Array<Int32, 2> = [1, 2]\n    twin: Array<Int32, 2> = [1, 2]\n    arrays: Bool = fixed == twin\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    lists: Bool = values == values\n    circle: Shape = Shape.Circle { r = 1, }\n    square: Shape = Shape.Square { a = 1, }\n    shapes: Bool = circle == square\n    mut value: Int32 = 3\n    pointer: Ptr<Int32> = ref value\n    twinPointer: Ptr<Int32> = pointer\n    pointers: Bool = pointer == twinPointer\nend"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	dir := t.TempDir()
	mainC := filepath.Join(dir, "main.c")
	mainH := filepath.Join(dir, "main.h")
	if err := os.WriteFile(mainC, []byte(result.MainC), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainH, []byte(result.MainH), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("gcc", "-std=c23", "-Wall", "-Wextra", "-c", mainC, "-o", filepath.Join(dir, "main.o"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
}

// Smoke-check that an owning String program generates C that gcc accepts
// with -std=c23: literals, affine ownership, concat, and byte views.
func TestGeneratedStringCCompiles(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "fun make_text(h: Heap): String\n    return \"ready\".to_string(h)\nend\nfun demo(h: Heap)\n    text: String = make_text(h)\n    defer text.free(h)\n    loud: String = text.concat(h, \"!\")\n    raw: View<UInt8> = text.bytes()\n    first: UInt8 = raw[0]\n    part: View<UInt8> = text.slice(0, 2)\n    second: UInt8 = part[1]\n    loud.free(h)\nend"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	dir := t.TempDir()
	mainC := filepath.Join(dir, "main.c")
	mainH := filepath.Join(dir, "main.h")
	if err := os.WriteFile(mainC, []byte(result.MainC), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainH, []byte(result.MainH), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("gcc", "-std=c23", "-Wall", "-Wextra", "-c", mainC, "-o", filepath.Join(dir, "main.o"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
}
