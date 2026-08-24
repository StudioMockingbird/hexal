package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// localHelper is one discovered anonymous function literal, normalized to
// one shape so prototype and definition emission need only one code path. A
// source name is checker metadata carried only for diagnostics and is never
// a C symbol.
type localHelper struct {
	ordinal    checker.BindingID
	parameters []checker.FunctionParameter
	result     *compilerTypes.Type
	body       []checker.Statement
	defers     []checker.DeferredAction
	typ        compilerTypes.Type
	sourceLine int
	sourceName string
}

// localHelperCName is the one file-scope C symbol an anonymous literal ever
// lowers to. It carries no owner encoding: local helpers are module-local by
// construction, and the ordinal alone is unique within the module.
func localHelperCName(ordinal checker.BindingID) string {
	return fmt.Sprintf("hex_fun_%d", ordinal)
}

// collectLocalHelpers walks the whole checked program - module statements
// and every specialized function and method body, at every nesting depth -
// and returns every anonymous function literal it finds, sorted by ordinal.
// Checking assigns ordinals in exactly one linear preorder pass, so the sort
// only defends determinism; it never reorders relative to a second,
// independent counter.
func collectLocalHelpers(program checker.Program) ([]localHelper, error) {
	var helpers []localHelper
	seen := make(map[checker.BindingID]bool)
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind != checker.FunctionLiteralExpression || node.Function == nil {
				return nil
			}
			literal := node.Function
			if seen[literal.HelperOrdinal] {
				return unknownExpressionDiagnostic("anonymous function literal helper discovered more than once")
			}
			seen[literal.HelperOrdinal] = true
			helpers = append(helpers, localHelper{
				ordinal:    literal.HelperOrdinal,
				parameters: literal.Parameters,
				result:     literal.Result,
				body:       literal.Body,
				defers:     literal.Defers,
				typ:        literal.Type,
				sourceLine: literal.SourceLine,
				sourceName: "function literal",
			})
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	sort.Slice(helpers, func(left, right int) bool { return helpers[left].ordinal < helpers[right].ordinal })
	return helpers, nil
}

// writeLocalHelperPrototypes emits one static prototype per discovered
// helper, in ordinal order, before any ordinary function or method
// definition references one. This is what lets an enclosing function call a
// helper declared later in the file, and what lets one helper call another
// regardless of declaration order.
func writeLocalHelperPrototypes(body *strings.Builder, helpers []localHelper, typeState *generatedTypeValidation) error {
	if len(helpers) == 0 {
		return nil
	}
	for _, helper := range helpers {
		if !validateGeneratedType(helper.typ, typeState, false) {
			return unknownExpressionDiagnostic("local function helper without a checked Fun type")
		}
		resultSpelling := "void"
		if helper.result != nil {
			if !validateGeneratedType(*helper.result, typeState, false) {
				return unknownExpressionDiagnostic("unsupported local function helper result type")
			}
			resultSpelling = standaloneResultSpelling(*helper.result)
		}
		parameters := make([]string, len(helper.parameters))
		for index, parameter := range helper.parameters {
			if !validateGeneratedType(parameter.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported local function helper parameter type")
			}
			parameters[index] = typeSpelling(parameter.Type)
		}
		fmt.Fprintf(body, "static %s %s(%s);\n", resultSpelling, localHelperCName(helper.ordinal), parameterList(parameters))
	}
	body.WriteString("\n")
	return nil
}

// writeLocalHelperDefinitions emits one file-scope static C function per
// discovered helper, in ordinal order, after ordinary definitions and
// concrete specializations and before the root main body. Every helper is
// static: a local named function or literal has no source-level export
// surface and never gains external linkage.
func writeLocalHelperDefinitions(ctx definitionContext, helpers []localHelper) error {
	for _, helper := range helpers {
		signature := helper.typ.Signature
		if signature == nil || !validateGeneratedType(helper.typ, ctx.typeState, false) {
			return unknownExpressionDiagnostic("local function helper without a checked Fun type")
		}
		if len(signature.Parameters) != len(helper.parameters) {
			return unknownExpressionDiagnostic("local function helper parameter count does not match its checked type")
		}
		resultSpelling := "void"
		if helper.result != nil {
			if !validateGeneratedType(*helper.result, ctx.typeState, false) {
				return unknownExpressionDiagnostic("unsupported local function helper result type")
			}
			if signature.Result == nil || !compilerTypes.Equal(*signature.Result, *helper.result) {
				return unknownExpressionDiagnostic("local function helper result does not match its checked type")
			}
			resultSpelling = standaloneResultSpelling(*helper.result)
		} else if signature.Result != nil {
			return unknownExpressionDiagnostic("local function helper result does not match its checked type")
		}
		if helper.result != nil && checker.FallsThrough(helper.body) {
			return unknownExpressionDiagnostic("checked returning local function helper may fall through without returning")
		}
		state := &expressionValidation{
			variables:      make(map[string]generatedBinding, len(helper.parameters)),
			bindings:       make(map[checker.BindingID]generatedBinding, len(helper.parameters)),
			bindingNames:   make(map[checker.BindingID]string, len(helper.parameters)),
			usedNames:      make(map[string]bool),
			functions:      ctx.functions,
			methods:        ctx.methods,
			generatedTypes: ctx.typeState,
			strings:        ctx.strings,
			owner:          ctx.owner,
			filename:       ctx.filename,
			tags:           ctx.tags,
		}
		state.pushScope()
		parameters := make([]string, len(helper.parameters))
		for index, parameter := range helper.parameters {
			if !validSourceName(parameter.Name) {
				return unknownExpressionDiagnostic("invalid checked local function helper parameter name")
			}
			if !validateGeneratedType(parameter.Type, ctx.typeState, false) {
				return unknownExpressionDiagnostic("unsupported local function helper parameter type")
			}
			if !compilerTypes.Equal(signature.Parameters[index], parameter.Type) {
				return unknownExpressionDiagnostic("local function helper parameter does not match its checked type")
			}
			name, nameErr := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false)
			if nameErr != nil {
				return nameErr
			}
			parameters[index] = declaration(parameter.Type, name, false)
		}
		writeLineDirective(ctx.body, helper.sourceLine, ctx.filename)
		fmt.Fprintf(ctx.body, "static %s %s(%s) {\n", resultSpelling, localHelperCName(helper.ordinal), parameterList(parameters))
		if err := writeStatements(ctx.body, helper.body, state, helper.result, true, helper.defers); err != nil {
			return err
		}
		ctx.body.WriteString("}\n\n")
	}
	return nil
}
