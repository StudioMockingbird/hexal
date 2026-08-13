package compiler

import "testing"

// RFC 0049 item 8.2: commands that produce no value are valid only as
// statements. The matrix below proves the reject-in-value-position and
// accept-as-statement sides for every no-result command family: collections,
// text, streams, and files. (Concurrency commands already have coverage in
// concurrency_test.go, and print in print_test.go.)

func TestNoResultCommandsRejectedInValuePositions(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"list push", "fun f(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    bad: Int32 = values.push(1)\nend\n", "push produces no value"},
		{"list set", "fun f(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    bad: Int32 = values.set(0, 1)\nend\n", "set produces no value"},
		{"list clear", "fun f(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    bad: Int32 = values.clear()\nend\n", "clear produces no value"},
		{"list free", "fun f(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    bad: Int32 = values.free(h)\nend\n", "free produces no value"},
		{"dict insert", "fun f(h: Heap)\n    d: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    bad: Int32 = d.insert(1, 2)\nend\n", "insert produces no value"},
		{"dict free", "fun f(h: Heap)\n    d: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    bad: Int32 = d.free(h)\nend\n", "free produces no value"},
		{"string free", "fun f(h: Heap)\n    s: String = \"x\".to_string(h)\n    bad: Int32 = s.free(h)\nend\n", "free produces no value"},
		{"stream free", "fun f(h: Heap)\n    s: Stream<Int32> = Stream<Int32>.new()\n    bad: Int32 = s.free(h)\nend\n", "free produces no value"},
		{"file close", "fun f(): Int32 | Error\n    file: File = try File.open(\"x\", FileMode.Read)\n    bad: Int32 = file.close()\n    return 0\nend\n", "close produces no value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, testCase.want)
		})
	}
}

func TestNoResultCommandsValidAsStatements(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"list push", "fun f(h: Heap)\n    values: List<Int32> = List<Int32>.new(h)\n    values.push(1)\n    values.set(0, 2)\n    values.clear()\n    values.free(h)\nend\n"},
		{"dict insert", "fun f(h: Heap)\n    d: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    d.insert(1, 2)\n    d.free(h)\nend\n"},
		{"string free", "fun f(h: Heap)\n    s: String = \"x\".to_string(h)\n    s.free(h)\nend\n"},
		{"stream free", "fun f(h: Heap)\n    s: Stream<Int32> = Stream<Int32>.new()\n    s.free(h)\nend\n"},
		{"file close", "fun f(): Int32 | Error\n    file: File = try File.open(\"x\", FileMode.Read)\n    file.close()\n    return 0\nend\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertCompiles(t, testCase.source)
		})
	}
}
