package generator

import (
	"fmt"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// literalHandle identifies one payload in a literalRegistry.
type literalHandle struct{ index int }

// literalRegistry owns the program-wide literal order and the only valid
// mapping from a payload to its generated C object.
type literalRegistry struct {
	payloads []string
	seen     map[string]literalHandle
	used     bool
	strand   bool
}

func newLiteralRegistry() *literalRegistry {
	return &literalRegistry{seen: make(map[string]literalHandle)}
}

func (registry *literalRegistry) Intern(payload string) literalHandle {
	if handle, exists := registry.seen[payload]; exists {
		return handle
	}
	handle := literalHandle{index: len(registry.payloads)}
	registry.payloads = append(registry.payloads, payload)
	registry.seen[payload] = handle
	return handle
}

func (registry *literalRegistry) CName(handle literalHandle) string {
	return stringLiteralCName(handle.index)
}

func (registry *literalRegistry) Lookup(payload string) (literalHandle, bool) {
	handle, exists := registry.seen[payload]
	return handle, exists
}

func (registry *literalRegistry) All() []string {
	return registry.payloads
}

// discoverGeneratedStrings interns checked string payloads and reports
// whether this module needs the String component.
func discoverGeneratedStrings(program checker.Program, registry *literalRegistry) bool {
	used := false
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) {
			if compilerTypes.IsString(typ) {
				used = true
				registry.used = true
				return
			}
			if compilerTypes.IsStrand(typ) {
				used = true
				registry.used = true
				registry.strand = true
				return
			}
		},
		Expression: func(node checker.Expression) {
			if node.Kind == checker.StringLiteralExpression {
				used = true
				registry.used = true
				registry.Intern(node.Name)
			}
		},
	}
	walkProgram(program, visitor)
	return used
}

// stringLiteralCName returns the object base name of one literal.
func stringLiteralCName(index int) string {
	return fmt.Sprintf("hex_lit_%d", index)
}
