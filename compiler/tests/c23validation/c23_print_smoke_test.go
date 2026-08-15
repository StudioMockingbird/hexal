//go:build c23

package c23validation

import "testing"

// Smoke-check that print programs generate C that gcc accepts and
// that a compiled run emits the expected text (newlines translated by the C
// text stream on Windows, so the comparison normalizes line endings).
func c23GeneratedPrintRuns(t *testing.T) {
	source := "type Point = {\n    x: Int32,\n    y: Int32,\n}\nprint(\"count = \", 42, \"\\n\")\nprint(true, false, nil)\nprint(1.5, -2.5)\npoint: Point = Point { x = 10, y = 20 }\nprint(point)\nprint(\"\\n\")"
	normalized := runGeneratedC(t, assertCompiles(t, source))
	want := "count = 42\ntruefalsenil1.5-2.5Point { x = 10, y = 20 }\n"
	if normalized != want {
		t.Fatalf("program output = %q, want %q", normalized, want)
	}
}

// Runtime conformance: arguments evaluate once, left to right, and
// the writes follow after all evaluation completes.
func c23GeneratedPrintEvaluationOrderRuns(t *testing.T) {
	source := "fun a(): Int32 do\n    print(\"a\")\n    return 1\nend\nfun b(): Int32 do\n    print(\"b\")\n    return 2\nend\nprint(a(), b(), a())\n"
	if got := runGeneratedC(t, assertCompiles(t, source)); got != "aba121" {
		t.Fatalf("program output = %q, want %q", got, "aba121")
	}
}

// Runtime conformance: collection and String arguments print their
// element formats (brackets, quoted text, braces) through the nested
// helpers.
func c23GeneratedPrintCollectionsRuns(t *testing.T) {
	source := "fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    print(values)\n    text: String = \"hi\".to_string(h)\n    defer text.free(h)\n    print(text)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    print(scores)\nend\ndemo(Heap.new())\n"
	if got := runGeneratedC(t, assertCompiles(t, source)); got != `[1, 2]hi{1: 10}` {
		t.Fatalf("program output = %q, want %q", got, `[1, 2]hi{1: 10}`)
	}
}
