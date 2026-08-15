// declarations.go owns C declaration, definition, prototype, and symbol-name
// emission: file-scope function and method definitions, specialization,
// exported and foreign prototypes, and private name mangling.
package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// NameKind identifies the Hexal-owned declaration namespace lowered by the
// generator. The mapping is deliberately stateless and never consults a C
// keyword list or a name registry.
type NameKind uint8

const (
	ValueName NameKind = iota
	TypeName
	MemberName
	FunctionName
)

// PrivateCName applies the private C prefix exactly once at the
// declaration/reference rendering boundary. owner is the encoded module owner
// of the declaring module ("" for compiler-owned names such as locals and
// members, which stay unencoded): hex_f_m8_graphics6_shapes_draw names module
// "graphics/shapes".
func PrivateCName(kind NameKind, source, owner string) string {
	prefix := "hex_v_"
	switch kind {
	case TypeName:
		prefix = "hex_t_"
	case MemberName:
		prefix = "hex_m_"
	case FunctionName:
		prefix = "hex_f_"
	}
	if owner != "" && (kind == TypeName || kind == FunctionName) {
		return prefix + owner + "_" + source
	}
	return prefix + source
}

func validSourceName(name string) bool {
	if name == "" || !isASCIILetter(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if !isASCIILetter(character) && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func isASCIILetter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

// moduleOwner resolves the encoded owner of a callee: cross-module callees
// carry the target module's canonical id (checked operand Module); a local
// callee falls back to the module being generated.
func moduleOwner(targetModule, fallbackOwner string) string {
	if targetModule == "" {
		return fallbackOwner
	}
	return compilerTypes.EncodeModuleOwner(targetModule)
}

// declaredFunctions collects every module-level function so a body can name
// itself and the callees around it. Declaration order is the checker's rule,
// not the generator's, so the whole table is visible everywhere here.
func declaredFunctions(program checker.Program) (map[string]compilerTypes.Type, error) {
	functions := make(map[string]compilerTypes.Type)
	for _, statement := range program.Statements {
		declared, ok := statement.(checker.FunctionDeclaration)
		if !ok {
			continue
		}
		if !validSourceName(declared.Name) {
			return nil, unknownExpressionDiagnostic("invalid checked function declaration name")
		}
		if _, exists := functions[declared.Name]; exists {
			return nil, unknownExpressionDiagnostic("duplicate checked function declaration name")
		}
		if declared.Type.Signature == nil {
			return nil, unknownExpressionDiagnostic("function declaration without a checked Fun type")
		}
		functions[declared.Name] = declared.Type
	}
	for _, declared := range program.SpecializedFunctions {
		if !validSourceName(declared.Name) {
			return nil, unknownExpressionDiagnostic("invalid checked function declaration name")
		}
		if _, exists := functions[declared.Name]; exists {
			return nil, unknownExpressionDiagnostic("duplicate checked function declaration name")
		}
		if declared.Type.Signature == nil {
			return nil, unknownExpressionDiagnostic("function declaration without a checked Fun type")
		}
		functions[declared.Name] = declared.Type
	}
	return functions, nil
}

// declaredMethods collects checked methods by the same source-derived stem
// used by the checker for C-name collision diagnostics. Method calls carry the
// owning nominal object, so this table lets generation validate that a call
// refers to a checked method with the expected receiver and signature.
func declaredMethods(program checker.Program) (map[string]checker.MethodDeclaration, error) {
	methods := make(map[string]checker.MethodDeclaration)
	for _, statement := range program.Statements {
		declared, ok := statement.(checker.MethodDeclaration)
		if !ok {
			continue
		}
		if declared.Object == nil || !validSourceName(compilerTypes.SanitizeIdentifier(declared.Object.Name)) || !validSourceName(declared.Name) {
			return nil, unknownExpressionDiagnostic("invalid checked method declaration name")
		}
		key := methodKey(declared.Object, declared.Name)
		if _, exists := methods[key]; exists {
			return nil, unknownExpressionDiagnostic("duplicate checked method declaration")
		}
		methods[key] = declared
	}
	for _, declared := range program.SpecializedMethods {
		if declared.Object == nil || !validSourceName(compilerTypes.SanitizeIdentifier(declared.Object.Name)) || !validSourceName(declared.Name) {
			return nil, unknownExpressionDiagnostic("invalid checked method declaration name")
		}
		key := methodKey(declared.Object, declared.Name)
		if _, exists := methods[key]; exists {
			return nil, unknownExpressionDiagnostic("duplicate checked method declaration")
		}
		methods[key] = declared
	}
	return methods, nil
}

func methodKey(object *compilerTypes.ObjectType, name string) string {
	if object == nil {
		return ""
	}
	return compilerTypes.SanitizeIdentifier(object.Name) + "_" + name
}

func methodCName(object *compilerTypes.ObjectType, name, owner string) string {
	return PrivateCName(FunctionName, methodKey(object, name), owner)
}

// writeFunctionDefinition emits one C function. Parameters are fixed
// bindings, so their declarators carry top-level const. external is true for
// functions the generated spawn adapters must call; those keep external
// linkage, everything else is static to its module C file.
func writeFunctionDefinition(body *strings.Builder, declared checker.FunctionDeclaration, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, typeState *generatedTypeValidation, stringState *generatedStringState, owner, filename string, external bool) error {
	signature := declared.Type.Signature
	if signature == nil || !validateGeneratedType(declared.Type, typeState, false) {
		return unknownExpressionDiagnostic("function declaration without a checked Fun type")
	}
	if len(signature.Parameters) != len(declared.Parameters) {
		return unknownExpressionDiagnostic("function declaration parameter count does not match its checked type")
	}
	resultSpelling := "void"
	if declared.Result != nil {
		if declared.Result.Signature != nil {
			return unknownExpressionDiagnostic("Fun function results are not supported")
		}
		if !validateGeneratedType(*declared.Result, typeState, false) {
			return unknownExpressionDiagnostic("unsupported checked function result type")
		}
		if signature.Result == nil || !compilerTypes.Equal(*signature.Result, *declared.Result) {
			return unknownExpressionDiagnostic("function result does not match its checked type")
		}
		resultSpelling = typeSpelling(*declared.Result)
	} else if signature.Result != nil {
		return unknownExpressionDiagnostic("function result does not match its checked type")
	}
	if declared.Result != nil && checker.FallsThrough(declared.Body) {
		return unknownExpressionDiagnostic("checked returning function may fall through without returning")
	}

	state := &expressionValidation{
		variables:      make(map[string]generatedBinding, len(declared.Parameters)),
		bindings:       make(map[checker.BindingID]generatedBinding, len(declared.Parameters)),
		bindingNames:   make(map[checker.BindingID]string, len(declared.Parameters)),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
		owner:          owner,
		filename:       filename,
	}
	state.pushScope()
	parameters := make([]string, len(declared.Parameters))
	for index, parameter := range declared.Parameters {
		if !validSourceName(parameter.Name) {
			return unknownExpressionDiagnostic("invalid checked function parameter name")
		}
		if !validateGeneratedType(parameter.Type, typeState, false) {
			return unknownExpressionDiagnostic("unsupported checked function parameter type")
		}
		if !compilerTypes.Equal(signature.Parameters[index], parameter.Type) {
			return unknownExpressionDiagnostic("function parameter does not match its checked type")
		}
		name, nameErr := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false)
		if nameErr != nil {
			return nameErr
		}
		parameters[index] = declaration(parameter.Type, name, false)
	}

	writeLineDirective(body, declared.SourceLine, filename)
	// Exported declarations and spawn targets keep external linkage;
	// everything else is static to the module translation unit.
	linkage := ""
	if !external && !declared.Exported {
		linkage = "static "
	}
	fmt.Fprintf(body, "%s%s %s(%s) {\n", linkage, resultSpelling, PrivateCName(FunctionName, declared.Name, owner), parameterList(parameters))
	if err := writeStatements(body, declared.Body, state, declared.Result, true, declared.Defers); err != nil {
		return err
	}
	body.WriteString("}\n\n")
	return nil
}

// writeMethodDefinition emits a checked impl method as a file-scope C
// function. The implicit receiver is the first fixed parameter; its written
// receiver type determines whether C receives a structure copy, a read-only
// pointer, or a writable pointer.
func writeMethodDefinition(body *strings.Builder, declared checker.MethodDeclaration, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, typeState *generatedTypeValidation, stringState *generatedStringState, owner, filename string) error {
	if declared.Object == nil || declared.SelfBinding == 0 || !validSourceName(declared.Name) {
		return unknownExpressionDiagnostic("method declaration is missing checked receiver metadata")
	}
	if !validateGeneratedType(declared.SelfType, typeState, false) {
		return unknownExpressionDiagnostic("unsupported checked method receiver type")
	}
	if declared.SelfType.Object != declared.Object && (declared.SelfType.Element == nil || declared.SelfType.Element.Object != declared.Object) {
		return unknownExpressionDiagnostic("method receiver does not match its checked owner")
	}
	resultSpelling := "void"
	if declared.Result != nil {
		if declared.Result.Signature != nil || !validateGeneratedType(*declared.Result, typeState, false) {
			return unknownExpressionDiagnostic("unsupported checked method result type")
		}
		resultSpelling = typeSpelling(*declared.Result)
	}
	if declared.Result != nil && checker.FallsThrough(declared.Body) {
		return unknownExpressionDiagnostic("checked returning method may fall through without returning")
	}

	state := &expressionValidation{
		variables:      make(map[string]generatedBinding, len(declared.Parameters)+1),
		bindings:       make(map[checker.BindingID]generatedBinding, len(declared.Parameters)+1),
		bindingNames:   make(map[checker.BindingID]string, len(declared.Parameters)+1),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
		owner:          owner,
		filename:       filename,
	}
	state.pushScope()
	selfName, selfErr := state.allocateBinding(declared.SelfBinding, "self", declared.SelfType, false)
	if selfErr != nil {
		return selfErr
	}
	parameters := []string{declaration(declared.SelfType, selfName, false)}
	for _, parameter := range declared.Parameters {
		if !validSourceName(parameter.Name) || parameter.Binding == 0 || !validateGeneratedType(parameter.Type, typeState, false) {
			return unknownExpressionDiagnostic("invalid checked method parameter")
		}
		name, nameErr := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false)
		if nameErr != nil {
			return nameErr
		}
		parameters = append(parameters, declaration(parameter.Type, name, false))
	}

	writeLineDirective(body, declared.SourceLine, filename)
	linkage := "static "
	if declared.Exported {
		linkage = ""
	}
	fmt.Fprintf(body, "%s%s %s(%s) {\n", linkage, resultSpelling, methodCName(declared.Object, declared.Name, owner), parameterList(parameters))
	if err := writeStatements(body, declared.Body, state, declared.Result, true, declared.Defers); err != nil {
		return err
	}
	body.WriteString("}\n\n")
	return nil
}

func parameterList(parameters []string) string {
	if len(parameters) == 0 {
		return "void"
	}
	return strings.Join(parameters, ", ")
}

// writeExportedPrototypes emits one external prototype per exported function
// and method of the module. Importers render calls against these encoded
// symbols, so every exporting module's own header declares them (the module
// .c file includes only its own header, which includes hexal.h).
func writeExportedPrototypes(result *strings.Builder, program checker.Program, owner string) {
	for _, statement := range program.Statements {
		switch declared := statement.(type) {
		case checker.FunctionDeclaration:
			if !declared.Exported {
				continue
			}
			resultSpelling := "void"
			if declared.Result != nil {
				resultSpelling = typeSpelling(*declared.Result)
			}
			parameters := make([]string, len(declared.Parameters))
			for index, parameter := range declared.Parameters {
				parameters[index] = typeSpelling(parameter.Type)
			}
			fmt.Fprintf(result, "%s %s(%s);\n", resultSpelling, PrivateCName(FunctionName, declared.Name, owner), parameterList(parameters))
		case checker.MethodDeclaration:
			if !declared.Exported {
				continue
			}
			resultSpelling := "void"
			if declared.Result != nil {
				resultSpelling = typeSpelling(*declared.Result)
			}
			parameters := []string{typeSpelling(declared.SelfType)}
			for _, parameter := range declared.Parameters {
				parameters = append(parameters, typeSpelling(parameter.Type))
			}
			fmt.Fprintf(result, "%s %s(%s);\n", resultSpelling, methodCName(declared.Object, declared.Name, owner), parameterList(parameters))
		}
	}
}

// writeForeignPrototypes emits one prototype per imported (cross-module)
// function and method the module references. C23 forbids calling an external
// function without a prior declaration, and an importer never includes the
// dependency's header, so its own header must declare every foreign symbol
// it calls. Deduplicated by symbol; deterministic by visit order.
func writeForeignPrototypes(result *strings.Builder, program checker.Program, state *expressionValidation) {
	emitted := make(map[string]bool)
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.CallExpression:
				callee := node.Operand
				if callee == nil || callee.Module == "" || callee.ResultType.Signature == nil {
					return nil
				}
				symbol := PrivateCName(FunctionName, callee.Name, moduleOwner(callee.Module, state.owner))
				if emitted[symbol] {
					return nil
				}
				emitted[symbol] = true
				parameters := make([]string, 0, len(callee.ResultType.Signature.Parameters))
				for _, parameter := range callee.ResultType.Signature.Parameters {
					parameters = append(parameters, typeSpelling(parameter))
				}
				resultSpelling := "void"
				if callee.ResultType.Signature.Result != nil {
					resultSpelling = typeSpelling(*callee.ResultType.Signature.Result)
				}
				fmt.Fprintf(result, "%s %s(%s);\n", resultSpelling, symbol, parameterList(parameters))
			case checker.MethodCallExpression:
				if node.Owner == nil || node.Owner.ModuleID == "" || node.Owner.ModuleID == state.moduleID {
					return nil
				}
				symbol := methodCName(node.Owner, node.Name, moduleOwner(node.Owner.ModuleID, state.owner))
				if emitted[symbol] {
					return nil
				}
				emitted[symbol] = true
				parameters := []string{typeSpelling(node.OperandType)}
				for _, parameter := range node.MethodParameters {
					parameters = append(parameters, typeSpelling(parameter))
				}
				resultSpelling := "void"
				if node.ResultType != (compilerTypes.Type{}) {
					resultSpelling = typeSpelling(node.ResultType)
				}
				fmt.Fprintf(result, "%s %s(%s);\n", resultSpelling, symbol, parameterList(parameters))
			}
			return nil
		},
	}
	_ = walkProgram(program, visitor)
}

// writeSpecializedPrototypes emits one static prototype per concrete
// specialization so definitions can reference later specializations.
func writeSpecializedPrototypes(body *strings.Builder, functions []checker.FunctionDeclaration, methods []checker.MethodDeclaration, typeState *generatedTypeValidation, owner string) error {
	emitted := 0
	for _, declared := range functions {
		signature := declared.Type.Signature
		if signature == nil || !validateGeneratedType(declared.Type, typeState, false) {
			return unknownExpressionDiagnostic("specialized function without a checked Fun type")
		}
		resultSpelling := "void"
		if declared.Result != nil {
			if declared.Result.Signature != nil || !validateGeneratedType(*declared.Result, typeState, false) {
				return unknownExpressionDiagnostic("unsupported specialized function result type")
			}
			resultSpelling = typeSpelling(*declared.Result)
		}
		parameters := make([]string, len(declared.Parameters))
		for index, parameter := range declared.Parameters {
			if !validateGeneratedType(parameter.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported specialized function parameter type")
			}
			parameters[index] = typeSpelling(parameter.Type)
		}
		fmt.Fprintf(body, "static %s %s(%s);\n", resultSpelling, PrivateCName(FunctionName, declared.Name, owner), parameterList(parameters))
		emitted++
	}
	for _, declared := range methods {
		if declared.Object == nil || !validateGeneratedType(declared.SelfType, typeState, false) {
			return unknownExpressionDiagnostic("specialized method without checked receiver metadata")
		}
		resultSpelling := "void"
		if declared.Result != nil {
			if declared.Result.Signature != nil || !validateGeneratedType(*declared.Result, typeState, false) {
				return unknownExpressionDiagnostic("unsupported specialized method result type")
			}
			resultSpelling = typeSpelling(*declared.Result)
		}
		parameters := make([]string, 0, len(declared.Parameters)+1)
		parameters = append(parameters, typeSpelling(declared.SelfType))
		for _, parameter := range declared.Parameters {
			if !validateGeneratedType(parameter.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported specialized method parameter type")
			}
			parameters = append(parameters, typeSpelling(parameter.Type))
		}
		fmt.Fprintf(body, "static %s %s(%s);\n", resultSpelling, methodCName(declared.Object, declared.Name, owner), parameterList(parameters))
		emitted++
	}
	if emitted > 0 {
		body.WriteString("\n")
	}
	return nil
}

// writeSpecializedDefinitions emits the concrete bodies of every
// specialization in cache order.
func writeSpecializedDefinitions(body *strings.Builder, functions []checker.FunctionDeclaration, methods []checker.MethodDeclaration, functionsTable map[string]compilerTypes.Type, methodsTable map[string]checker.MethodDeclaration, typeState *generatedTypeValidation, stringState *generatedStringState, owner, filename string) error {
	for _, declared := range functions {
		if definitionErr := writeFunctionDefinition(body, declared, functionsTable, methodsTable, typeState, stringState, owner, filename, false); definitionErr != nil {
			return definitionErr
		}
	}
	for _, declared := range methods {
		if definitionErr := writeMethodDefinition(body, declared, functionsTable, methodsTable, typeState, stringState, owner, filename); definitionErr != nil {
			return definitionErr
		}
	}
	return nil
}
