//go:build c23

package compiler

import "testing"

// Smoke-check that RFC 0030 print programs generate C that gcc accepts and
// that a compiled run emits the expected text (newlines translated by the C
// text stream on Windows, so the comparison normalizes line endings).
func TestGeneratedPrintRuns(t *testing.T) {
	source := "type Point = {\n    x: Int32,\n    y: Int32,\n}\nprint(\"count = \", 42, \"\\n\")\nprint(true, false, nil)\nprint(1.5, -2.5)\npoint: Point = Point { x = 10, y = 20 }\nprint(point)\nprint(\"\\n\")"
	normalized := runGeneratedC(t, assertCompiles(t, source))
	want := "count = 42\ntruefalsenil1.5-2.5Point { x = 10, y = 20 }\n"
	if normalized != want {
		t.Fatalf("program output = %q, want %q", normalized, want)
	}
}
