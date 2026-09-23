package span

import (
	"strings"
	"testing"
)

// A single-line file numbers every byte of its only line, and the offset just
// past the last byte is the EOF insertion point.
func TestPositionSingleLineAndEOF(t *testing.T) {
	const file = "unit.hex"
	table := NewTable()
	table.Add(file, "let x: Int32 = 13")

	cases := []struct {
		offset int
		want   Position
	}{
		{0, Position{Line: 1, Column: 1}},
		{4, Position{Line: 1, Column: 5}},
		{13, Position{Line: 1, Column: 14}},
		{17, Position{Line: 1, Column: 18}}, // one past the final byte
	}
	for _, testCase := range cases {
		got := table.Position(Span{File: file, Start: testCase.offset, End: testCase.offset})
		if got != testCase.want {
			t.Errorf("Position(%d) = %+v, want %+v", testCase.offset, got, testCase.want)
		}
	}

	// An empty file still has its first line, so the EOF offset is line 1.
	empty := NewTable()
	empty.Add(file, "")
	if got := empty.Position(Span{File: file, Start: 0}); got != (Position{Line: 1, Column: 1}) {
		t.Errorf("empty-file Position(0) = %+v, want 1:1", got)
	}

	// A trailing newline opens a new, empty final line.
	trailing := NewTable()
	trailing.Add(file, "abc\n")
	if got := trailing.Position(Span{File: file, Start: 4}); got != (Position{Line: 2, Column: 1}) {
		t.Errorf("trailing-newline Position(4) = %+v, want 2:1", got)
	}
	if got := trailing.Position(Span{File: file, Start: 3}); got != (Position{Line: 1, Column: 4}) {
		t.Errorf("Position of the newline byte = %+v, want 1:4", got)
	}
}

// A carriage return ends a line by itself, exactly like a line feed; a
// carriage-return/line-feed pair is one break, not two.
func TestPositionCarriageReturnConvention(t *testing.T) {
	const file = "unit.hex"
	table := NewTable()
	table.Add(file, "a\rb\nc")

	if got := table.Position(Span{File: file, Start: 2}); got != (Position{Line: 2, Column: 1}) {
		t.Errorf("byte after a lone CR = %+v, want 2:1 (a CR ends a line)", got)
	}
	if got := table.Position(Span{File: file, Start: 4}); got != (Position{Line: 3, Column: 1}) {
		t.Errorf("byte after the LF = %+v, want 3:1", got)
	}

	crlf := NewTable()
	crlf.Add(file, "a\r\nb")
	if got := crlf.Position(Span{File: file, Start: 3}); got != (Position{Line: 2, Column: 1}) {
		t.Errorf("byte after CRLF = %+v, want 2:1 (a CRLF is one break)", got)
	}
}

// A multiline comment crosses lines without emitting a token; the position of
// the token after it is a line and column in the whole file, not the comment.
func TestPositionAcrossMultilineComment(t *testing.T) {
	const file = "unit.hex"
	source := "x: Int32 --[ comment\n   across lines ]-- = 13"
	table := NewTable()
	table.Add(file, source)

	equalOffset := strings.Index(source, "=")
	got := table.Position(Span{File: file, Start: equalOffset})
	if want := (Position{Line: 2, Column: 21}); got != want {
		t.Errorf("Position of '=' = %+v, want %+v", got, want)
	}
}

// Columns count bytes, the same unit the lexer's incremental counter uses, so
// multibyte Unicode does not collapse to one column.
func TestPositionCountsBytesForUnicode(t *testing.T) {
	const file = "unit.hex"
	sources := []struct {
		name   string
		source string
		want   Position
	}{
		// e-acute is two bytes; x follows a four-byte opening literal and one
		// space, so its column is 6.
		{"two-byte-rune", "\"\u00e9\" x", Position{Line: 1, Column: 6}},
		// The crab emoji is four bytes; x is at column 8.
		{"four-byte-rune", "\"\U0001F980\" x", Position{Line: 1, Column: 8}},
	}
	for _, testCase := range sources {
		table := NewTable()
		table.Add(file, testCase.source)
		offset := strings.Index(testCase.source, "x")
		if got := table.Position(Span{File: file, Start: offset}); got != testCase.want {
			t.Errorf("%s: Position(%d) = %+v, want %+v", testCase.name, offset, got, testCase.want)
		}
	}
}

// A zero-width span is an insertion point: End never affects the position, and
// a zero-width span at EOF is a legal diagnostic location.
func TestPositionZeroWidthInsertion(t *testing.T) {
	const file = "unit.hex"
	table := NewTable()
	table.Add(file, "abc")

	widthless := table.Position(Span{File: file, Start: 1, End: 1})
	bounded := table.Position(Span{File: file, Start: 1, End: 3})
	if widthless != bounded {
		t.Errorf("zero-width position %+v differs from bounded %+v", widthless, bounded)
	}
	if widthless != (Position{Line: 1, Column: 2}) {
		t.Errorf("insertion at offset 1 = %+v, want 1:2", widthless)
	}
	if got := table.Position(Span{File: file, Start: 3, End: 3}); got != (Position{Line: 1, Column: 4}) {
		t.Errorf("zero-width EOF insertion = %+v, want 1:4", got)
	}
}

// A span over several tokens is the first token's Start through the last
// token's End; the table resolves both endpoints and the span is one value.
func TestSpanCoversMultipleTokens(t *testing.T) {
	const file = "unit.hex"
	table := NewTable()
	table.Add(file, "let x")

	first := Span{File: file, Start: 0, End: 3}  // let
	second := Span{File: file, Start: 4, End: 5} // x
	covered := Span{File: file, Start: first.Start, End: second.End}

	if covered.End != 5 {
		t.Fatalf("covered span = %+v, want End 5", covered)
	}
	if got := table.Position(covered); got != (Position{Line: 1, Column: 1}) {
		t.Errorf("covered start position = %+v, want 1:1", got)
	}
	if got := table.Position(Span{File: file, Start: covered.End}); got != (Position{Line: 1, Column: 6}) {
		t.Errorf("covered end position = %+v, want 1:6", got)
	}
}

// Embedded stdlib and prepared C bindings are logical keys like any other; the
// table resolves them without a host path and without filesystem access.
func TestPositionForCompilerOwnedSources(t *testing.T) {
	table := NewTable()
	table.Add("stdlib/ascii.hex", "module ascii\n")
	table.Add("hexalc/x86_64-linux-gnu/c/stddef.hex", "extern c from <stddef.h>\nend\n")

	if got := table.Position(Span{File: "stdlib/ascii.hex", Start: 7}); got != (Position{Line: 1, Column: 8}) {
		t.Errorf("stdlib position = %+v, want 1:8", got)
	}
	if got := table.Position(Span{File: "hexalc/x86_64-linux-gnu/c/stddef.hex", Start: 17}); got != (Position{Line: 1, Column: 18}) {
		t.Errorf("prepared-binding position = %+v, want 1:18", got)
	}
}

// A missing file and a negative offset are the two ways a position cannot be
// proven; both report the zero Position rather than fabricating a location.
func TestPositionUnknownFileAndNegativeOffset(t *testing.T) {
	table := NewTable()
	table.Add("known.hex", "abc")

	if got := table.Position(Span{File: "missing.hex", Start: 1}); got != (Position{}) {
		t.Errorf("unknown file = %+v, want zero Position", got)
	}
	if got := table.Position(Span{File: "known.hex", Start: -1}); got != (Position{}) {
		t.Errorf("negative offset = %+v, want zero Position", got)
	}
}
