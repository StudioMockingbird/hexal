// Package diagnostics owns compiler diagnostic identities, categories, stages,
// wording, and rendering. Compiler stages decide which condition occurred and
// provide only typed context; this package owns the user-visible sentence.
package diagnostics

import (
	"fmt"
	"regexp"
)

type ID string
type Category string
type Stage string

const (
	CategorySemantic      Category = "Semantic Error"
	CategoryType          Category = "Type Error"
	CategoryName          Category = "Name Error"
	CategoryModule        Category = "Module Error"
	CategoryConfiguration Category = "Configuration Error"
	CategoryUnknown       Category = "Unknown Error"
	CategorySyntax        Category = "Syntax Error"
)

const (
	StageCompile   Stage = "compile"
	StageLexer     Stage = "lexer"
	StageParser    Stage = "parser"
	StageChecker   Stage = "checker"
	StageGenerator Stage = "generator"
)

type Message struct {
	id       ID
	category Category
	stage    Stage
	text     string
}

func (m Message) ID() ID             { return m.id }
func (m Message) Category() Category { return m.category }
func (m Message) Stage() Stage       { return m.stage }
func (m Message) Text() string       { return m.text }
func (m Message) IsZero() bool       { return m.id == "" }

type record struct {
	category Category
	stage    Stage
}

func message(id ID, category Category, stage Stage, text string) Message {
	registered, ok := declaredRecords[id]
	if !ok || registered.category != category || registered.stage != stage {
		panic("diagnostics: message does not match its declared identity " + string(id))
	}
	return Message{id: id, category: category, stage: stage, text: text}
}

func UnknownCompiler() Message {
	return message("internal.compiler-error", CategoryUnknown, StageCompile, "internal compiler error")
}

func GeneratorFailure() Message {
	return message("internal.generator-invariant", CategoryUnknown, StageGenerator, "internal compiler error")
}

func CheckerFailure() Message {
	return message("internal.checker-invariant", CategoryUnknown, StageChecker, "internal compiler error")
}

func UnknownVariable(name string) Message {
	return message("name.unknown-variable", CategoryName, StageChecker, "unknown variable "+name)
}

func Render(m Message, module string, line, column int) string {
	result := "[" + string(m.category) + " " + string(m.id) + "] " + m.text
	if line > 0 {
		if module != "" {
			result += fmt.Sprintf(" at %s:%d:%d", module, line, column)
		} else {
			result += fmt.Sprintf(" at %d:%d", line, column)
		}
	}
	return result
}

var validID = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)

func Validate() error {
	for id, entry := range declaredRecords {
		if !validID.MatchString(string(id)) {
			return fmt.Errorf("diagnostics: invalid id %q", id)
		}
		if !knownCategory(entry.category) || !knownStage(entry.stage) {
			return fmt.Errorf("diagnostics: incomplete record for %q", id)
		}
	}
	return nil
}

func knownCategory(category Category) bool {
	switch category {
	case CategorySemantic, CategoryType, CategoryName, CategoryModule, CategoryConfiguration, CategoryUnknown, CategorySyntax:
		return true
	default:
		return false
	}
}

func knownStage(stage Stage) bool {
	switch stage {
	case StageCompile, StageLexer, StageParser, StageChecker, StageGenerator:
		return true
	default:
		return false
	}
}
