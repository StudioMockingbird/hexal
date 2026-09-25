package generator

import (
	"testing"

	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// TestSourceAnchoredGeneratorFailureRendersItsLocation pins B5: a generator
// failure that names a checked node carrying a real span reports the module
// and the position the compilation's source table resolves, while a failure
// with no node keeps the historical zero location rather than inventing one.
func TestSourceAnchoredGeneratorFailureRendersItsLocation(t *testing.T) {
	table, at := appSourceAtLine(3)
	state := newExpressionValidation()
	state.table = table

	diagnostic, ok := unknownExpressionDiagnosticAt(state, at, "spawn expression has invalid checked metadata").(compilerTypes.Diagnostic)
	if !ok {
		t.Fatal("source-anchored generator failure did not build a Diagnostic")
	}
	if diagnostic.Module != "app.hex" || diagnostic.Position.Line != 3 || diagnostic.Position.Column != 1 {
		t.Fatalf("source location = %s:%d:%d, want app.hex:3:1", diagnostic.Module, diagnostic.Position.Line, diagnostic.Position.Column)
	}
	rendered := diagnostic.Error()
	want := "[Unknown Error] spawn expression has invalid checked metadata at app.hex:3:1"
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}

	// A node with no span keeps the historical whole-compilation location.
	whole, ok := unknownExpressionDiagnosticAt(state, span.Span{}, "whole compilation").(compilerTypes.Diagnostic)
	if !ok {
		t.Fatal("zero-span generator failure did not build a Diagnostic")
	}
	if whole.Position.Line != 0 || whole.Module != "" {
		t.Fatalf("zero-span location = %q:%d, want empty:0", whole.Module, whole.Position.Line)
	}
	if got := whole.Error(); got != "[Unknown Error] whole compilation" {
		t.Fatalf("zero-span rendered = %q, want no location", got)
	}
}
