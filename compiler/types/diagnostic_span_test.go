package types

import (
	"testing"

	"hexal/compiler/span"
)

// A diagnostic anchored to a token carries the span beside the retained line
// and column, and the renderer reads the same location it always has.
func TestDiagnosticCarriesSpanBesideRetainedLocation(t *testing.T) {
	source := span.Span{File: "app.hex", Start: 23, End: 24}
	diagnostic := Diagnostic{
		Category: TypeError,
		Stage:    "checker",
		Span:     source,
		Line:     1,
		Column:   24,
		Message:  "division by zero",
	}
	if got, want := diagnostic.Error(), "[Type Error] division by zero at 1:24"; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
	if diagnostic.Span != source {
		t.Fatalf("span = %+v, want %+v", diagnostic.Span, source)
	}
}

// A purely source-less diagnostic keeps the zero span, which renders exactly as
// it did before a span field existed: no invented location.
func TestLocationlessDiagnosticKeepsZeroSpan(t *testing.T) {
	diagnostic := NewDiagnostic(UnknownError, "compile", 0, 0, "internal compiler error")
	if diagnostic.Span != (span.Span{}) {
		t.Fatalf("span = %+v, want the zero span", diagnostic.Span)
	}
	if got, want := diagnostic.Error(), "[Unknown Error] internal compiler error"; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
}

// Module stamping copies the whole diagnostic, so a span survives attribution
// and never loses its file.
func TestDiagnosticSpanSurvivesModuleStamping(t *testing.T) {
	source := span.Span{File: "graphics/shapes.hex", Start: 5, End: 9}
	diagnostic := Diagnostic{Category: NameError, Span: source, Line: 2, Column: 1, Message: "unknown"}
	stamped := diagnostic.InModule("graphics/shapes.hex")
	if stamped.Span != source {
		t.Fatalf("stamped span = %+v, want %+v", stamped.Span, source)
	}
	set := Diagnostics{diagnostic}.InModule("graphics/shapes.hex")
	if set[0].Span != source {
		t.Fatalf("set span = %+v, want %+v", set[0].Span, source)
	}
}

// Diagnostic ordering stays on the retained line and column: a span never
// silently reorders a stage's output, which is why the comparator is untouched
// by the span migration.
func TestDiagnosticOrderingIgnoresSpan(t *testing.T) {
	first := Diagnostic{Line: 1, Column: 5, Span: span.Span{File: "app.hex", Start: 100, End: 101}}
	second := Diagnostic{Line: 2, Column: 1, Span: span.Span{File: "app.hex", Start: 4, End: 8}}
	if CompareDiagnostic(first, second) >= 0 {
		t.Fatalf("CompareDiagnostic = %d, want a negative result ordered by line then column", CompareDiagnostic(first, second))
	}
}
