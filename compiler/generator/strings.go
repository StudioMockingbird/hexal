package generator

import (
	"fmt"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedStringState records the unique String literals in first-use order
// plus whether the String machinery is needed at all.
type generatedStringState struct {
	used       bool
	needStrand bool
	literals   []string
	seen       map[string]int // payload -> literal index + 1
}

// discoverGeneratedStrings walks the program collecting unique String
// literal payloads. The machinery is marked used whenever a String-typed
// value or literal appears.
func discoverGeneratedStrings(program checker.Program) (*generatedStringState, error) {
	state := &generatedStringState{seen: make(map[string]int)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsString(typ) {
				state.used = true
				return nil
			}
			if compilerTypes.IsStrand(typ) {
				state.used = true
				state.needStrand = true
				return nil
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.StringLiteralExpression {
				state.used = true
				if _, exists := state.seen[node.Name]; !exists {
					state.seen[node.Name] = len(state.literals) + 1
					state.literals = append(state.literals, node.Name)
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}

// stringLiteralCName returns the object base name of one literal.
func stringLiteralCName(index int) string {
	return fmt.Sprintf("hex_lit_%d", index)
}
