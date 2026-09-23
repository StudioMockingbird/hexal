// Package span owns the Hexal source-position model: a Span is a half-open
// byte range in one logical source string, and a Table resolves byte offsets
// to 1-based line and column. A file key is a logical source key, never a host
// path, and the package performs no filesystem access.
//
// The one line and column convention, implemented by Table.Position. It is
// C's translation phase one, the same break set the lexer's counters apply:
//
//   - A line ends at a line feed (0x0A), a carriage return (0x0D), or a
//     carriage-return/line-feed pair; each is exactly one break and lines are
//     numbered from 1. A lone carriage return therefore ends a line, which is
//     why the lexer's raw-string, interpreted-string, whitespace, and comment
//     scanners share this break set. Because no UTF-8 continuation byte
//     (0x80-0xBF) can be a break byte, scanning bytes for line starts is safe
//     for UTF-8 text.
//   - A column is the 1-based count of bytes from the start of its line, so
//     ASCII text and multibyte Unicode advance it by their byte length. This
//     is go/token's byte-based Column and the lexer's own unit.
//   - A zero-width span (Start == End) is an insertion point between bytes;
//     its position is the line and column of the byte it precedes.
package span

import (
	"slices"
	"strings"
)

// Span is one half-open byte range in a logical source file. File is the
// logical source key the compilation supplied and is never a host path; Start
// is inclusive and End exclusive, both byte offsets into that file's text. A
// zero-width span (Start == End) is an insertion point, the shape a diagnostic
// uses to point between two tokens; a span over several tokens simply carries
// the first token's Start and the last token's End.
type Span struct {
	File  string
	Start int
	End   int
}

// Position is a 1-based line and byte column within one logical source file.
type Position struct {
	Line   int
	Column int
}

// Table holds the text of every logical file in one compilation and converts a
// span's byte offsets to line and column. The compilation builds it and reads
// it; it never reads a file itself. A query for a file the table does not hold
// returns the zero Position, which renders as no location rather than an
// invented one.
type Table struct {
	// lines maps each logical file to the byte offset of the first byte of
	// every 1-based line, ascending and beginning at 0.
	lines map[string][]int
}

// NewTable returns an empty table.
func NewTable() *Table {
	return &Table{lines: make(map[string][]int)}
}

// Add records one logical file's text under its logical key. Adding the same
// key again replaces the earlier text, so the table always describes the
// source the compilation supplied last for that key.
func (t *Table) Add(file, text string) {
	// At least one line exists even for empty text, so Position is total.
	starts := make([]int, 1, 1+strings.Count(text, "\n")+strings.Count(text, "\r"))
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '\n':
			starts = append(starts, index+1)
		case '\r':
			if index+1 < len(text) && text[index+1] == '\n' {
				// CRLF is one break; the LF below records it.
				continue
			}
			starts = append(starts, index+1)
		}
	}
	t.lines[file] = starts
}

// Position returns the 1-based line and byte column of s's start offset. The
// zero Position means the file is not in the table or the start is negative.
// An offset at the file's end, or past it, resolves on the final line with a
// column past that line's final byte, which is the shape an EOF diagnostic
// uses.
func (t *Table) Position(s Span) Position {
	if s.Start < 0 {
		return Position{}
	}
	starts, ok := t.lines[s.File]
	if !ok {
		return Position{}
	}
	// BinarySearch yields the first line start greater than or equal to the
	// offset: an exact hit names that line, otherwise the preceding start
	// names the line the offset falls inside. starts[0] is always 0, so the
	// fallthrough index is never 0.
	index, exact := slices.BinarySearch(starts, s.Start)
	line := index
	if exact {
		line = index + 1
	}
	return Position{Line: line, Column: s.Start - starts[line-1] + 1}
}
