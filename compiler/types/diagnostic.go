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
	// Position is Span's start resolved through the compilation's source
	// table: the one location the renderer prints and the comparator orders
	// by. It is the zero Position exactly when Span is zero, so a diagnostic
	// with no source file renders no location.
	Span     span.Span
	Position span.Position
	Message  string
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
	if diagnostic.Position.Line > 0 {
		if diagnostic.Module != "" {
			location = fmt.Sprintf(" at %s:%d:%d", diagnostic.Module, diagnostic.Position.Line, diagnostic.Position.Column)
		} else {
			location = fmt.Sprintf(" at %d:%d", diagnostic.Position.Line, diagnostic.Position.Column)
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

// NewDiagnostic builds one structured diagnostic with a resolved position and
// no span: a whole-compilation failure, a project-configuration failure, or
// another condition no logical source file owns. A caller that has a token or
// span sets Span and Position together; line and column zero render no
// location, which is the whole-compilation case.
func NewDiagnostic(category ErrorCategory, stage string, line, column int, message string) Diagnostic {
	return Diagnostic{
		Category: category,
		Stage:    stage,
		Position: span.Position{Line: line, Column: column},
		Message:  message,
	}
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

// HasUnknownError reports whether err carries any diagnostic in the Unknown
// Error category: a compiler defect rather than a rejection of the program.
// It unwraps exactly like ErrorMessages, so a diagnostic wrapped by any stage
// is still classified rather than treated as an opaque message.
func HasUnknownError(err error) bool {
	if err == nil {
		return false
	}
	var diagnostics Diagnostics
	if errors.As(err, &diagnostics) {
		for _, diagnostic := range diagnostics {
			if diagnostic.Category == UnknownError {
				return true
			}
		}
		return false
	}
	var diagnostic Diagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.Category == UnknownError
	}
	return false
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

// CompareDiagnostic orders two diagnostics by their resolved position. It is
// the one position comparator shared by every stage that sorts diagnostics.
func CompareDiagnostic(a, b Diagnostic) int {
	if a.Position.Line != b.Position.Line {
		return a.Position.Line - b.Position.Line
	}
	return a.Position.Column - b.Position.Column
}
