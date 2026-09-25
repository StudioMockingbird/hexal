package types

import (
	"errors"
	"fmt"
	"strings"

	"hexal/compiler/span"
)

// Structured compilation errors: the category and diagnostic types every
// stage reports through, module stamping, and the shared position
// comparator.

type ErrorCategory string

// The concrete ErrorCategory values. Each string is the exact bracketed
// label a Diagnostic renders in its Error() text, so renaming one changes
// every diagnostic of that category.
const (
	SemanticError      ErrorCategory = "Semantic Error"
	TypeError          ErrorCategory = "Type Error"
	NameError          ErrorCategory = "Name Error"
	ModuleError        ErrorCategory = "Module Error"
	ConfigurationError ErrorCategory = "Configuration Error"
	UnknownError       ErrorCategory = "Unknown Error"
	SyntaxError        ErrorCategory = "Syntax Error"
)

// Diagnostic is one structured compilation error.
type Diagnostic struct {
	Category ErrorCategory
	Stage    string
	// Module is the logical source key the diagnostic belongs to, the same
	// key the caller supplied in the sources map. Construction sites do not
	// set it: each stage stamps its diagnostics at the one point where it
	// knows which module it was checking, so a message can never claim the
	// wrong module. It is empty only for a diagnostic about the compilation
	// as a whole, such as a missing entrypoint.
	Module string
	// Span is the authoritative source range this diagnostic points at: the
	// logical file key and byte offsets the compilation supplied. It is the
	// zero Span for a diagnostic whose construction site records no span,
	// such as a whole-compilation failure. A zero-width Span (Start == End)
	// is an insertion point between two tokens, the shape a missing-token
	// diagnostic and an EOF diagnostic share.
	//
	// Line and Column are the same start position in the legacy integer form
	// the renderer still consumes during migration; a token's Line and Column
	// are the location the lexer derived beside its span, so the two are not
	// computed independently at a construction site that has the token. The
	// offset-to-line/column convention itself lives in compiler/span.
	Span    span.Span
	Line    int
	Column  int
	Message string
}

// InModule returns a copy of the diagnostic stamped with its module, leaving
// an already-stamped diagnostic alone so an inner stage's attribution wins
// over an outer one's.
func (diagnostic Diagnostic) InModule(module string) Diagnostic {
	if diagnostic.Module == "" {
		diagnostic.Module = module
	}
	return diagnostic
}

// InModule stamps every diagnostic of the set with its module.
func (diagnostics Diagnostics) InModule(module string) Diagnostics {
	stamped := make(Diagnostics, len(diagnostics))
	for index, diagnostic := range diagnostics {
		stamped[index] = diagnostic.InModule(module)
	}
	return stamped
}

// Diagnostics is the ordered set of diagnostics one stage produced.
type Diagnostics []Diagnostic

// Error renders the diagnostic's bracketed category, message, and source
// location, satisfying the standard error interface.
func (diagnostic Diagnostic) Error() string {
	// An empty category is a construction defect in the compiler: every site
	// must name a category, so an empty one renders as "[]" instead of being
	// masked as a compiler Unknown Error that a user error never merits.
	// The module qualifies the position: in a multi-module build "at 5:3"
	// names no file, and two modules' messages read as one interleaved list.
	location := ""
	if diagnostic.Line > 0 {
		if diagnostic.Module != "" {
			location = fmt.Sprintf(" at %s:%d:%d", diagnostic.Module, diagnostic.Line, diagnostic.Column)
		} else {
			location = fmt.Sprintf(" at %d:%d", diagnostic.Line, diagnostic.Column)
		}
	}
	return "[" + string(diagnostic.Category) + "] " + diagnostic.Message + location
}

// Error joins every diagnostic's own Error() text with a newline, satisfying
// the standard error interface for a whole diagnostic set.
func (diagnostics Diagnostics) Error() string {
	if len(diagnostics) == 0 {
		return ""
	}
	messages := make([]string, len(diagnostics))
	for index, diagnostic := range diagnostics {
		messages[index] = diagnostic.Error()
	}
	return strings.Join(messages, "\n")
}

// NewDiagnostic builds one structured diagnostic that has no source span: a
// whole-compilation failure, a project-configuration failure, or another
// condition no logical source file owns. A diagnostic anchored to a token sets
// Span and the token's derived Line and Column, so its source identity is the
// span.
func NewDiagnostic(category ErrorCategory, stage string, line, column int, message string) Diagnostic {
	return Diagnostic{Category: category, Stage: stage, Line: line, Column: column, Message: message}
}

// ErrorMessages renders an error into the message lines of a failed build. It
// traverses wrappers with errors.As, so a diagnostic wrapped by any stage still
// renders as itself rather than as an opaque message.
func ErrorMessages(err error) []string {
	if err == nil {
		return nil
	}
	var diagnostics Diagnostics
	if errors.As(err, &diagnostics) {
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Error())
		}
		return messages
	}
	var diagnostic Diagnostic
	if errors.As(err, &diagnostic) {
		return []string{diagnostic.Error()}
	}
	return []string{err.Error()}
}

// StampModule attributes a stage error to a module's logical source key.
func StampModule(err error, logicalKey string) error {
	if err == nil {
		return nil
	}
	var diagnostics Diagnostics
	if errors.As(err, &diagnostics) {
		return diagnostics.InModule(logicalKey)
	}
	var diagnostic Diagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.InModule(logicalKey)
	}
	return err
}

// CompareDiagnostic orders two diagnostics by line, then column. It is the
// one position comparator shared by every stage that sorts diagnostics.
func CompareDiagnostic(a, b Diagnostic) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Column - b.Column
}
