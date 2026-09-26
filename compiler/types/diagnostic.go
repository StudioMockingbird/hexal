package types

import (
	"errors"
	"strings"

	"hexal/compiler/diagnostics"
	"hexal/compiler/span"
)

type ErrorCategory = diagnostics.Category

const (
	SemanticError      = diagnostics.CategorySemantic
	TypeError          = diagnostics.CategoryType
	NameError          = diagnostics.CategoryName
	ModuleError        = diagnostics.CategoryModule
	ConfigurationError = diagnostics.CategoryConfiguration
	UnknownError       = diagnostics.CategoryUnknown
	SyntaxError        = diagnostics.CategorySyntax
)

type Diagnostic struct {
	Message  diagnostics.Message
	Module   string
	Span     span.Span
	Position span.Position
}

func (diagnostic Diagnostic) InModule(module string) Diagnostic {
	if diagnostic.Module == "" {
		diagnostic.Module = module
	}
	return diagnostic
}

type Diagnostics []Diagnostic

func (diagnostics Diagnostics) InModule(module string) Diagnostics {
	stamped := make(Diagnostics, len(diagnostics))
	for index, diagnostic := range diagnostics {
		stamped[index] = diagnostic.InModule(module)
	}
	return stamped
}

func (diagnostic Diagnostic) Error() string {
	return diagnostics.Render(diagnostic.Message, diagnostic.Module, diagnostic.Position.Line, diagnostic.Position.Column)
}

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

func At(message diagnostics.Message, source span.Span, position span.Position) Diagnostic {
	return Diagnostic{Message: message, Span: source, Position: position}
}

func Locationless(message diagnostics.Message) Diagnostic {
	return Diagnostic{Message: message}
}

func ErrorMessages(err error) []string {
	if err == nil {
		return nil
	}
	var set Diagnostics
	if errors.As(err, &set) {
		messages := make([]string, len(set))
		for index, diagnostic := range set {
			messages[index] = diagnostic.Error()
		}
		return messages
	}
	var diagnostic Diagnostic
	if errors.As(err, &diagnostic) {
		return []string{diagnostic.Error()}
	}
	return []string{diagnostics.Render(diagnostics.UnknownCompiler(), "", 0, 0)}
}

func HasUnknownError(err error) bool {
	if err == nil {
		return false
	}
	var set Diagnostics
	if errors.As(err, &set) {
		for _, diagnostic := range set {
			if diagnostic.Message.Category() == UnknownError {
				return true
			}
		}
		return false
	}
	var diagnostic Diagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.Message.Category() == UnknownError
	}
	return true
}

func StampModule(err error, logicalKey string) error {
	if err == nil {
		return nil
	}
	var set Diagnostics
	if errors.As(err, &set) {
		return set.InModule(logicalKey)
	}
	var diagnostic Diagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.InModule(logicalKey)
	}
	return err
}

func CompareDiagnostic(a, b Diagnostic) int {
	if a.Position.Line != b.Position.Line {
		return a.Position.Line - b.Position.Line
	}
	return a.Position.Column - b.Position.Column
}
