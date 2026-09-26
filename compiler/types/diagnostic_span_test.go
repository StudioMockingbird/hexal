package types

import (
	"testing"

	diagnostics "hexal/compiler/diagnostics"
	"hexal/compiler/span"
)

// A diagnostic anchored to a token carries the span and its resolved
// position, and the renderer reads the position to print the location.
func TestDiagnosticCarriesSpanBesideRetainedLocation(t *testing.T) {
	source := span.Span{File: "app.hex", Start: 23, End: 24}
	diagnostic := At(diagnostics.DivisionByZero(), source, span.Position{Line: 1, Column: 24})
	if got, want := diagnostic.Error(), "[Type Error type.division-by-zero] division by zero at 1:24"; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
	if diagnostic.Span != source {
		t.Fatalf("span = %+v, want %+v", diagnostic.Span, source)
	}
}

// A purely source-less diagnostic keeps the zero span and zero position, which
// renders exactly as it did before: no invented location.
func TestLocationlessDiagnosticKeepsZeroSpan(t *testing.T) {
	diagnostic := Locationless(diagnostics.UnknownCompiler())
	if diagnostic.Span != (span.Span{}) {
		t.Fatalf("span = %+v, want the zero span", diagnostic.Span)
	}
	if diagnostic.Position != (span.Position{}) {
		t.Fatalf("position = %+v, want the zero position", diagnostic.Position)
	}
	if got, want := diagnostic.Error(), "[Unknown Error internal.compiler-error] internal compiler error"; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
}

// Module stamping copies the whole diagnostic, so a span survives attribution
// and never loses its file.
func TestDiagnosticSpanSurvivesModuleStamping(t *testing.T) {
	source := span.Span{File: "graphics/shapes.hex", Start: 5, End: 9}
	diagnostic := At(diagnostics.UnknownVariable("unknown"), source, span.Position{Line: 2, Column: 1})
	stamped := diagnostic.InModule("graphics/shapes.hex")
	if stamped.Span != source {
		t.Fatalf("stamped span = %+v, want %+v", stamped.Span, source)
	}
	set := Diagnostics{diagnostic}.InModule("graphics/shapes.hex")
	if set[0].Span != source {
		t.Fatalf("set span = %+v, want %+v", set[0].Span, source)
	}
}

// Diagnostic ordering stays on the resolved position, not the raw span
// offsets: a span whose byte offsets would sort differently must not silently
// reorder a stage's output.
func TestDiagnosticOrderingUsesPositionNotSpan(t *testing.T) {
	first := Diagnostic{Position: span.Position{Line: 1, Column: 5}, Span: span.Span{File: "app.hex", Start: 100, End: 101}}
	second := Diagnostic{Position: span.Position{Line: 2, Column: 1}, Span: span.Span{File: "app.hex", Start: 4, End: 8}}
	if CompareDiagnostic(first, second) >= 0 {
		t.Fatalf("CompareDiagnostic = %d, want a negative result ordered by position", CompareDiagnostic(first, second))
	}
}
