package generator

// Module constants: one immutable owner-qualified object per constant. A
// private constant is `static const` in its owning module's C file; an exported
// constant is `const` there plus one `extern const` declaration in that
// module's header. No module-init function or accessor wrapper exists.

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
)

// moduleValueCName is the constant's owner-qualified generated symbol, used
// identically for its definition, its extern declaration, and every qualified
// read or address-of.
func moduleValueCName(name, owner string) string {
	return "hex_v_" + owner + "_" + name
}

// moduleValueDefinition renders one constant's C definition. declaration
// already carries `const`; a private constant additionally has internal
// linkage. Every accepted initializer is a C constant expression, so it
// renders directly.
func moduleValueDefinition(value checker.ModuleValueDeclaration, owner string, state *expressionValidation) (string, error) {
	name := moduleValueCName(value.Name, owner)
	initializer, err := renderOperandWithState(value.Source, state)
	if err != nil {
		return "", err
	}
	linkage := ""
	if !value.Exported {
		linkage = "static "
	}
	return fmt.Sprintf("%s%s = %s;\n", linkage, declaration(value.Type, name, false), initializer), nil
}

// moduleValueExternDeclaration renders one exported constant's header
// declaration: the same storage, `extern`-qualified.
func moduleValueExternDeclaration(value checker.ModuleValueDeclaration, owner string) string {
	name := moduleValueCName(value.Name, owner)
	return "extern " + declaration(value.Type, name, false) + ";\n"
}

// writeModuleValueDefinitions emits every constant's C definition, in checked
// declaration order, into the owning module's C file.
func writeModuleValueDefinitions(result *strings.Builder, values []checker.ModuleValueDeclaration, owner string, state *expressionValidation) error {
	for _, value := range values {
		definition, err := moduleValueDefinition(value, owner, state)
		if err != nil {
			return err
		}
		result.WriteString(definition)
	}
	return nil
}

// writeExportedModuleValueDeclarations emits every exported constant's extern
// declaration, in checked declaration order, into the owning module's header.
func writeExportedModuleValueDeclarations(result *strings.Builder, values []checker.ModuleValueDeclaration, owner string) {
	for _, value := range values {
		if !value.Exported {
			continue
		}
		result.WriteString(moduleValueExternDeclaration(value, owner))
	}
}
