package integration

import (
	"strings"
	"testing"
)

// Text cursors expose has_next, next, peek, and offset on both text forms, and
// only the Rune cursor selects the utf8proc adapter.
func TestTextCursorSurface(t *testing.T) {
	assertCompiles(t, "fun demo() do\n    let text: String = \"ab\"\n    let mut b: ByteCursor = text.byte_cursor()\n    while b.has_next() do\n        let x: Byte = b.next()\n        let y: Byte = b.peek()\n        let o: Size = b.offset()\n    end\n    let mut r: RuneCursor = text.rune_cursor()\n    while r.has_next() do\n        let x: Rune = r.next()\n        let y: Rune = r.peek()\n        let o: Size = r.offset()\n    end\nend\n")
	assertCompiles(t, "fun demo() do\n    let text: String<8> = \"ab\"\n    let mut b: ByteCursor = text.byte_cursor()\nend\n")
	assertRejects(t, "fun demo() do\n    let text: String = \"ab\"\n    let mut b: ByteCursor = text.byte_cursor()\n    let x: Byte = b.frob()\nend\n", "ByteCursor has no method frob")
	assertRejects(t, "fun demo() do\n    let text: String = \"ab\"\n    let mut b: ByteCursor = text.byte_cursor()\n    let x: Byte = b.next(1)\nend\n", "next expects no arguments")
}

// next advances the binding it is given, so an immutable cursor is rejected.
func TestCursorNextRequiresMutableBinding(t *testing.T) {
	assertRejects(t, "fun demo() do\n    let text: String = \"ab\"\n    let b: ByteCursor = text.byte_cursor()\n    let x: Byte = b.next()\nend\n", "next mutates its cursor")
}

// A ByteCursor-only program selects no utf8proc dependency; a RuneCursor does.
func TestCursorUnicodeDemand(t *testing.T) {
	bytes := assertCompiles(t, "fun demo() do\n    let text: String = \"ab\"\n    let mut b: ByteCursor = text.byte_cursor()\nend\n")
	if strings.Contains(stringC(t, bytes), "utf8proc") {
		t.Fatalf("a ByteCursor-only program selected utf8proc:\n%s", stringC(t, bytes))
	}
	runes := assertCompiles(t, "fun demo() do\n    let text: String = \"ab\"\n    let mut r: RuneCursor = text.rune_cursor()\nend\n")
	if !strings.Contains(stringC(t, runes), "utf8proc_iterate") {
		t.Fatalf("a RuneCursor program did not select the utf8proc adapter:\n%s", stringC(t, runes))
	}
}
