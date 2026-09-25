package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// Diagnostic construction: the span-to-token adapter, the five category
// constructors every checker site reports through, and the pointer and
// module-stamping helpers.

// tokenAt builds the diagnostic token for a span. A nil table, which only a
// lone module checked outside Compile has, resolves to the zero position and
// so renders as no location rather than inventing one.
func tokenAt(table *span.Table, s span.Span) lexer.Token {
	position := span.Position{}
	if table != nil {
		position = table.Position(s)
	}
	return lexer.Token{Span: s, Line: position.Line, Column: position.Column}
}

// checkerDiagnostic is the one construction path for every checker
// diagnostic: the four category builders below differ only in category, so a
// token's span and its resolved position are recorded in exactly one place.
func checkerDiagnostic(category compilerTypes.ErrorCategory, token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: category,
		Stage:    "checker",
		Span:     token.Span,
		Position: span.Position{Line: token.Line, Column: token.Column},
		Message:  message,
	}
}

// typeErrorAt is the checker's single Type Error constructor: every site
// reports through it rather than expanding a composite literal.
func typeErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return checkerDiagnostic(compilerTypes.TypeError, token, message)
}

func moduleDataDiagnostic(owner, name string, token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, fmt.Sprintf("function %s cannot access module data binding %s; pass it as a parameter", owner, name))
}

func selfNotBoundDiagnostic(token lexer.Token) *compilerTypes.Diagnostic {
	diagnostic := typeErrorAt(token, "self is not bound outside a method body")
	return &diagnostic
}

// diagnosticAt returns an addressable copy of one constructed diagnostic. The
// checker reports a diagnostic by pointer in the many places where absence is
// meaningful, and a constructor's result is not addressable; this adapter
// keeps every construction going through one of the *At builders instead of
// re-expanding a composite literal at each pointer site.
func diagnosticAt(diagnostic compilerTypes.Diagnostic) *compilerTypes.Diagnostic {
	return &diagnostic
}

// diagnosticInDefiningModule stamps a diagnostic returned from specializing
// an imported generic template with the defining module's own logical key,
// before it propagates back up through the requesting module's own
// checkModule call: CheckModules's own InModule stamp only ever applies to
// an unstamped diagnostic, so a body or signature failure inside the
// template still names the module that declares it, never the importer
// that merely triggered the specialization. A nil diagnostic (success)
// passes through unchanged.
func diagnosticInDefiningModule(diagnostic *compilerTypes.Diagnostic, logicalKey string) *compilerTypes.Diagnostic {
	if diagnostic == nil {
		return nil
	}
	stamped := diagnostic.InModule(logicalKey)
	return &stamped
}

// nameErrorAt, moduleErrorAt, semanticErrorAt, and unknownAt are
// typeErrorAt's siblings for the checker's other categories. Every diagnostic
// the checker reports is built by one of these five, so a category is never
// spelled at a call site.
func nameErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return checkerDiagnostic(compilerTypes.NameError, token, message)
}

func moduleErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return checkerDiagnostic(compilerTypes.ModuleError, token, message)
}

// semanticErrorAt reports a whole-program semantic contract failure, such as
// scheduler starvation, that is neither a type nor a name error.
func semanticErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return checkerDiagnostic(compilerTypes.SemanticError, token, message)
}

func unknownAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return checkerDiagnostic(compilerTypes.UnknownError, token, message)
}
