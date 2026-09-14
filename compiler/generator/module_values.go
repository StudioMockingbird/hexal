package generator

// Static module values: program-lifetime storage lowered directly to C
// static storage. One definition lives in its owning module's C file; an
// exported value additionally gets one extern declaration in that module's
// header. No module-init function or accessor wrapper exists.

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
)

// moduleValueCName is the module value's owner-qualified generated symbol,
// used identically for its definition, its extern declaration, and every
// qualified read, write, or address-of.
func moduleValueCName(name, owner string) string {
	return "hex_v_" + owner + "_" + name
}

// moduleValueDefinition renders one module value's C definition. declaration
// already carries `const` for a fixed value and omits it for `mut`; the
// direct fixed Atomic exception is handled the same way declaration handles
// every other Atomic-typed binding, keeping its `_Atomic` spelling without
// `const`.
func moduleValueDefinition(value checker.ModuleValueDeclaration, owner string, state *expressionValidation) (string, error) {
	name := moduleValueCName(value.Name, owner)
	initializer, err := renderOperandWithState(value.Source, state)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s = %s;\n", declaration(value.Type, name, value.Mutable), initializer), nil
}

// moduleValueExternDeclaration renders one module value's header
// declaration: the same storage, `extern`-qualified.
func moduleValueExternDeclaration(value checker.ModuleValueDeclaration, owner string) string {
	name := moduleValueCName(value.Name, owner)
	return "extern " + declaration(value.Type, name, value.Mutable) + ";\n"
}

// writeModuleValueDefinitions emits every module value's C definition, in
// checked declaration order, into the owning module's C file.
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

// writeExportedModuleValueDeclarations emits every exported module value's
// extern declaration, in checked declaration order, into the owning
// module's header.
func writeExportedModuleValueDeclarations(result *strings.Builder, values []checker.ModuleValueDeclaration, owner string) {
	for _, value := range values {
		if !value.Exported {
			continue
		}
		result.WriteString(moduleValueExternDeclaration(value, owner))
	}
}
