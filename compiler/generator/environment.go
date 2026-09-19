package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
)

// entryEnvironmentName is the generated C name of the stack-owned entry
// environment type. It is private to the entry module's C artifact and never
// appears in a generated header.
const entryEnvironmentName = "hex_entry_env"

// writeEntryEnvironmentType emits the private environment struct type when the
// entry module captures at least one root binding. It precedes every prototype
// and definition that names it.
func writeEntryEnvironmentType(body *strings.Builder, program checker.Program) {
	if len(program.EntryCaptures) == 0 {
		return
	}
	body.WriteString("typedef struct {\n")
	for _, capture := range program.EntryCaptures {
		// A fixed captured binding still needs a writable field for its one
		// runtime initialization; the Hexal checker rejects later assignment.
		fmt.Fprintf(body, "    %s;\n", declaration(capture.Type, privateCName(valueName, capture.Name, ""), true))
	}
	body.WriteString("} " + entryEnvironmentName + ";\n\n")
}

// entryEnvironmentFunctions returns the names of the entry module's
// environment-dependent functions, whose calls must pass the environment.
func entryEnvironmentFunctions(program checker.Program) map[string]bool {
	functions := make(map[string]bool)
	for _, statement := range program.Statements {
		if declaration, ok := statement.(checker.FunctionDeclaration); ok && declaration.EnvDependent {
			functions[declaration.Name] = true
		}
	}
	for _, declaration := range program.SpecializedFunctions {
		if declaration.EnvDependent {
			functions[declaration.Name] = true
		}
	}
	return functions
}

// entryEnvironmentMethods returns the names of the entry module's
// environment-dependent methods, whose calls must pass the environment.
func entryEnvironmentMethods(program checker.Program) map[string]bool {
	methods := make(map[string]bool)
	for _, statement := range program.Statements {
		if declaration, ok := statement.(checker.MethodDeclaration); ok && declaration.EnvDependent {
			methods[declaration.Name] = true
		}
	}
	for _, declaration := range program.SpecializedMethods {
		if declaration.EnvDependent {
			methods[declaration.Name] = true
		}
	}
	return methods
}

// registerEnvironment registers an environment-dependent declaration's captured
// bindings under the hidden environment pointer and marks the current
// environment pointer.
func registerEnvironment(state *expressionValidation, captures []checker.Capture) error {
	state.envPointer = "env"
	for _, capture := range captures {
		if err := state.registerCapture(capture, "env->"+privateCName(valueName, capture.Name, "")); err != nil {
			return err
		}
	}
	return nil
}
