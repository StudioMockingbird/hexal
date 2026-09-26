package types

import (
	"testing"

	diagnostics "hexal/compiler/diagnostics"
	"hexal/compiler/span"
)

// TestDiagnosticLocationAgreesWithItsSpan pins the C6 invariant after the
// migration: a diagnostic's authoritative Span resolves, under the
// compilation's source table, to exactly the Position the renderer prints and
// the comparator orders by. A zero span is the whole-compilation case, whose
// zero Position renders no location.
func TestDiagnosticLocationAgreesWithItsSpan(t *testing.T) {
	table := span.NewTable()
	table.Add("app.hex", "alpha\nbeta\ngamma")

	for _, testCase := range []struct {
		name         string
		s            span.Span
		position     span.Position
		wantLine     int
		wantColumn   int
		wantLocation bool
	}{
		{
			name:         "start of the second line",
			s:            span.Span{File: "app.hex", Start: 6},
			position:     span.Position{Line: 2, Column: 1},
			wantLine:     2,
			wantColumn:   1,
			wantLocation: true,
		},
		{
			name:         "third line, offset four",
			s:            span.Span{File: "app.hex", Start: 14},
			position:     span.Position{Line: 3, Column: 4},
			wantLine:     3,
			wantColumn:   4,
			wantLocation: true,
		},
		{
			name:         "whole-compilation has no location",
			s:            span.Span{},
			position:     span.Position{},
			wantLine:     0,
			wantColumn:   0,
			wantLocation: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resolved := table.Position(testCase.s)
			if testCase.wantLocation && resolved != testCase.position {
				t.Fatalf("span resolves to %+v, but the diagnostic names %+v", resolved, testCase.position)
			}
			diagnostic := At(diagnostics.UnknownCompiler(), testCase.s, testCase.position)
			if diagnostic.Position.Line != testCase.wantLine || diagnostic.Position.Column != testCase.wantColumn {
				t.Fatalf("diagnostic position = %d:%d, want %d:%d",
					diagnostic.Position.Line, diagnostic.Position.Column, testCase.wantLine, testCase.wantColumn)
			}
			if rendersLocation := diagnostic.Position.Line > 0; rendersLocation != testCase.wantLocation {
				t.Fatalf("renders location = %v, want %v", rendersLocation, testCase.wantLocation)
			}
		})
	}
}
