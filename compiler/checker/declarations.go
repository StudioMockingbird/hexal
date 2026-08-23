package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func trackablePointerBinding(bound binding) bool {
	// IO bindings ride the freed machinery so close records a versioned
	// closed state with the same rebinding and branch-merge semantics as
	// Heap.free.
	return bound.kind == dataBinding && !bound.parameter && !bound.loopBinder && (bound.typ.Element != nil || compilerTypes.IsIO(bound.typ))
}

func directPointerBinding(source Operand, target compilerTypes.Type) BindingID {
	if target.Element == nil || source.Type.Element == nil || source.Node.Kind != VariableExpression || source.Node.Binding == 0 {
		return 0
	}
	return source.Node.Binding
}

func checkTypeDeclaration(declaration parser.TypeDeclaration, typeEnvironment *compilerTypes.Environment, names *scope) (TypeDeclaration, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	name := declaration.Name.Lexeme
	previousType, hadPreviousType := typeEnvironment.Lookup(name)
	previousUse, hadPreviousUse := typeEnvironment.LookupUse(name)
	if compilerTypes.IsProtectedTypeName(name) {
		message := "built-in type " + name + " cannot be redeclared"
		if name == "Ptr" || name == "MutPtr" {
			message = "built-in type constructor " + name + " cannot be redeclared"
		}
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, message))
	} else if typeEnvironment.Contains(name) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "type "+name+" is already declared"))
	} else if names.declaredHere(name) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "type "+name+" is already declared as a value"))
	}

	if len(declaration.Parameters) > 0 {
		genericDiagnostics := registerGenericTypeDeclaration(declaration, typeEnvironment, names)
		return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, append(diagnostics, genericDiagnostics...)
	}

	if adt, isADT := declaration.Target.(parser.AdtDefinitionExpression); isADT {
		checked, adtDiagnostics := checkADTDeclaration(declaration, adt, typeEnvironment, names)
		return checked, append(diagnostics, adtDiagnostics...)
	}

	if object, ok := declaration.Target.(parser.ObjectTypeExpression); ok {
		if len(object.Members) == 0 {
			diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "object type "+name+" must declare at least one member"))
		}
		// Publish a provisional nominal identity before resolving members so a
		// member may reach this object behind at least one pointer layer. The
		// identity is abandoned if any member fails and finalized only on
		// complete success. BeginObject stamps the declaring module's
		// canonical id and encoded owner.
		beginResult := typeEnvironment.BeginObject(name, declaration.Name.Line, declaration.Name.Column)
		members, memberDiagnostics := resolveObjectMembers(name, object, typeEnvironment, names.generics)
		diagnostics = append(diagnostics, memberDiagnostics...)
		if len(diagnostics) == 0 {
			resolved := typeEnvironment.CompleteObject(name, members)
			if !compilerTypes.Equal(resolved, beginResult) {
				return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, compilerTypes.Diagnostics{{
					Category: compilerTypes.UnknownError,
					Stage:    "checker",
					Message:  "object identity mismatch after member resolution",
				}}
			}
			return TypeDeclaration{
				Name:         name,
				Type:         resolved,
				TypeUse:      compilerTypes.NewTypeUse(resolved),
				SourceLine:   declaration.Name.Line,
				SourceColumn: declaration.Name.Column,
			}, nil
		}
		if hadPreviousType {
			if hadPreviousUse {
				typeEnvironment.DeclareAliasUse(name, previousUse)
			} else {
				typeEnvironment.DeclareAlias(name, previousType)
			}
		} else {
			typeEnvironment.AbandonObject(name)
		}
		return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, diagnostics
	}

	if containsTypeName(declaration.Target, name) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "type alias "+name+" cannot reference itself"))
	} else if resolvedUse, diagnostic := resolveTypeUse(declaration.Target, declaration.Name, typeEnvironment, names.generics); diagnostic != nil {
		diagnostics = append(diagnostics, *diagnostic)
	} else if len(diagnostics) == 0 {
		return TypeDeclaration{
			Name:         name,
			Type:         resolvedUse.Type,
			TypeUse:      resolvedUse,
			SourceLine:   declaration.Name.Line,
			SourceColumn: declaration.Name.Column,
		}, nil
	}

	return TypeDeclaration{
		Name:         name,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}, diagnostics
}

func resolveObjectMembers(objectName string, expression parser.ObjectTypeExpression, typeEnvironment *compilerTypes.Environment, generics *genericTable) ([]compilerTypes.ObjectMember, compilerTypes.Diagnostics) {
	members := make([]compilerTypes.ObjectMember, 0, len(expression.Members))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(expression.Members))
	for _, declaration := range expression.Members {
		if seen[declaration.Name.Lexeme] {
			diagnostics = append(diagnostics, typeErrorAt(declaration.Name, fmt.Sprintf("object type %s declares member %s more than once", objectName, declaration.Name.Lexeme)))
			continue
		}
		seen[declaration.Name.Lexeme] = true

		if containsTypeName(declaration.Type, objectName) && !containsPointerType(declaration.Type) {
			diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "object type "+objectName+" cannot contain itself by value"))
			continue
		}

		resolvedUse, diagnostic := resolveTypeUse(declaration.Type, declaration.Name, typeEnvironment, generics)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		resolved := resolvedUse.Type
		if diagnostic := valueTypeDiagnostic(declaration.Type, declaration.Name, resolved); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if resolved.Signature != nil {
			// Supported-position whitelist: a Fun<...> member would be
			// callback data in the object layout, which is not supported.
			diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "Fun<…> object members are not supported"))
			continue
		}
		// Any complete, finitely sized value may be an object member except
		// Fun, Unknown, and Atomic at non-construction positions. An open type
		// parameter defers to specialization rechecking.
		if !compilerTypes.ContainsTypeParameter(resolved) && !compilerTypes.Storable(resolved, compilerTypes.PositionObjectMember) {
			diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "unsupported object member type "+resolved.Name))
			continue
		}
		members = append(members, compilerTypes.ObjectMember{
			Name:         declaration.Name.Lexeme,
			Type:         resolved,
			Use:          resolvedUse,
			Mutable:      declaration.Mutable,
			SourceLine:   declaration.Name.Line,
			SourceColumn: declaration.Name.Column,
		})
	}
	return members, diagnostics
}

func containsPointerType(expression parser.TypeExpression) bool {
	switch expression := expression.(type) {
	case parser.PtrTypeExpression:
		return true
	case parser.UnionTypeExpression:
		for _, member := range expression.Members {
			if containsPointerType(member) {
				return true
			}
		}
		return false
	case parser.GroupedTypeExpression:
		return containsPointerType(expression.Inner)
	case parser.FunctionTypeExpression:
		for _, parameter := range expression.Parameters {
			if containsPointerType(parameter) {
				return true
			}
		}
		return expression.Return != nil && containsPointerType(expression.Return)
	case parser.ObjectTypeExpression:
		for _, member := range expression.Members {
			if containsPointerType(member.Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func containsTypeName(expression parser.TypeExpression, name string) bool {
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		return expression.Name.Lexeme == name
	case parser.GenericTypeExpression:
		return expression.Name.Lexeme == name
	case parser.PtrTypeExpression:
		return containsTypeName(expression.Element, name)
	case parser.UnionTypeExpression:
		for _, member := range expression.Members {
			if containsTypeName(member, name) {
				return true
			}
		}
		return false
	case parser.GroupedTypeExpression:
		return containsTypeName(expression.Inner, name)
	case parser.FunctionTypeExpression:
		for _, parameter := range expression.Parameters {
			if containsTypeName(parameter, name) {
				return true
			}
		}
		return expression.Return != nil && containsTypeName(expression.Return, name)
	case parser.ObjectTypeExpression:
		for _, member := range expression.Members {
			if containsTypeName(member.Type, name) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// registerGenericTypeDeclaration validates and stores one generic type or
// alias declaration as an open template. The target is not resolved yet:
// parameters are placeholders until a concrete specialization is requested.
func registerGenericTypeDeclaration(declaration parser.TypeDeclaration, typeEnvironment *compilerTypes.Environment, names *scope) compilerTypes.Diagnostics {
	name := declaration.Name.Lexeme
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(declaration.Parameters))
	parameterNames := make([]string, 0, len(declaration.Parameters))
	for _, parameter := range declaration.Parameters {
		if seen[parameter.Lexeme] {
			diagnostics = append(diagnostics, typeErrorAt(parameter, "generic parameter "+parameter.Lexeme+" is declared more than once"))
			continue
		}
		seen[parameter.Lexeme] = true
		if compilerTypes.IsProtectedTypeName(parameter.Lexeme) {
			diagnostics = append(diagnostics, typeErrorAt(parameter, "generic parameter "+parameter.Lexeme+" is a protected type name"))
			continue
		}
		parameterNames = append(parameterNames, parameter.Lexeme)
	}
	if len(diagnostics) > 0 {
		return diagnostics
	}
	generic := typeEnvironment.DeclareGeneric(name, len(declaration.Parameters), parameterNames)
	if generic == nil {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, "type "+name+" is already declared")}
	}
	if _, objectTarget := declaration.Target.(parser.ObjectTypeExpression); objectTarget {
		if containsTypeName(declaration.Target, name) && !containsPointerType(declaration.Target) {
			return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, "object type "+name+" cannot contain itself by value")}
		}
	} else if containsTypeName(declaration.Target, name) {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, "type alias "+name+" cannot reference itself")}
	}
	names.generics.types[name] = &openGenericType{
		Name:        name,
		Parameters:  append([]lexer.Token(nil), declaration.Parameters...),
		Target:      declaration.Target,
		Declaration: generic,
	}
	return nil
}

func checkDeclaration(declaration parser.Declaration, names *scope, typeEnvironment *compilerTypes.Environment) (Declaration, binding, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	// A declaration without a type expression takes its type from the
	// initializer below.
	var declaredUse compilerTypes.TypeUse
	if declaration.Type != nil {
		resolved, typeDiagnostic := resolveTypeUse(declaration.Type, declaration.Name, typeEnvironment, names.generics)
		declaredUse = resolved
		if typeDiagnostic != nil {
			diagnostics = append(diagnostics, *typeDiagnostic)
		} else if diagnostic := valueTypeDiagnostic(declaration.Type, declaration.Name, resolved.Type); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	declaredType := declaredUse.Type
	if declaration.Name.Lexeme == "print" {
		// The protected builtin name cannot be bound by a local or
		// module declaration.
		diagnostics = append(diagnostics, nameErrorAt(declaration.Name, "print is a protected built-in name"))
	}
	if layoutBuiltins[declaration.Name.Lexeme] {
		// The layout query names cannot be bound by a local or
		// module declaration.
		diagnostics = append(diagnostics, nameErrorAt(declaration.Name, declaration.Name.Lexeme+" is a protected built-in name"))
	}
	if compilerTypes.IsProtectedTypeName(declaration.Name.Lexeme) {
		message := "value " + declaration.Name.Lexeme + " is already declared as a type"
		if declaration.Name.Lexeme == "Ptr" || declaration.Name.Lexeme == "MutPtr" {
			message = "built-in type constructor " + declaration.Name.Lexeme + " cannot be redeclared"
		}
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, message))
	} else if typeEnvironment.Contains(declaration.Name.Lexeme) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "value "+declaration.Name.Lexeme+" is already declared as a type"))
	}
	if names.declaredHere(declaration.Name.Lexeme) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "variable "+declaration.Name.Lexeme+" is already declared in this scope; use '=' for reassignment"))
	}

	// An inferred binding has no destination context. A contextual initializer
	// would otherwise receive a silent literal default, so reject it before type
	// checking can select one.
	inferred := declaration.Type == nil
	if inferred && isContextualForInference(declaration.Initializer) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Operator,
			"`:=` requires an initializer whose type does not depend on context; annotate the binding instead"))
	}

	initializer := checkInitializer(declaration.Initializer, declaredUse, declaration.Name, names, typeEnvironment)
	for _, diagnostic := range initializerDiagnostics(initializer) {
		diagnostics = append(diagnostics, diagnostic)
	}
	if inferred && len(diagnostics) == 0 {
		// The binding takes the initializer's resolved type and use, which is
		// exactly what an annotation would have produced for the same source.
		declaredUse = initializer.use
		declaredType = initializer.typ
		if declaredUse.Type.Name == "" {
			declaredUse = compilerTypes.NewTypeUse(declaredType)
		}
	}
	if len(diagnostics) == 0 {
		if diagnostic := atomicCopyDiagnostic(initializer.source, declaration.Name); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	if len(diagnostics) == 0 && declaredType.Name != "" && !assignable(declaredType, initializer.typ) {
		diagnostics = append(diagnostics, bindingMismatchDiagnostic(declaration.Name.Lexeme, declaredType, initializer.typ, initializer.token))
	}

	declaredBinding := binding{
		typ:     declaredType,
		use:     declaredUse,
		mutable: declaration.Mutable,
		id:      names.newBindingID(),
	}
	if initializer.source.Node.Kind == AddressOfExpression || nodeTracesToRef(&initializer.source.Node, names) {
		declaredBinding.fromRef = true
	}
	if declaredType.View != nil {
		declaredBinding.viewRoots = initializer.source.Node.ViewRoots
		declaredBinding.viewRootKind = initializer.source.Node.RootKind
	}
	if len(diagnostics) == 0 && !declaration.Mutable && initializer.known != nil {
		// The initializer's known value becomes this binding's known-value
		// metadata, whether the initializer is a literal constant or a read
		// of another named immutable binding (reads keep their variable
		// operand in the checked program, so the metadata must be copied
		// explicitly).
		known := *initializer.known
		declaredBinding.known = &known
	}
	if len(diagnostics) == 0 && names.flow != nil && trackablePointerBinding(declaredBinding) {
		names.flow.trackFreed(declaredBinding.id)
		if sourceBinding := directPointerBinding(initializer.source, declaredType); sourceBinding != 0 {
			names.flow.dropFreed(sourceBinding)
			names.flow.dropFreed(declaredBinding.id)
		}
	}
	if len(diagnostics) == 0 && names.flow != nil {
		seedStreamBindingFacts(names.flow, declaredBinding.id, declaredType, initializer.source)
	}
	return Declaration{
		Name:         declaration.Name.Lexeme,
		Binding:      declaredBinding.id,
		Type:         declaredType,
		TypeUse:      declaredUse,
		Source:       initializer.source,
		Mutable:      declaration.Mutable,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}, declaredBinding, diagnostics
}

func checkAssignment(assignment parser.Assignment, names *scope, typeEnvironment *compilerTypes.Environment) (Assignment, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	target := checkPlace(assignment.Target, names, typeEnvironment)
	switch {
	case target.diagnostic != nil:
		diagnostics = append(diagnostics, *target.diagnostic)
	case target.self:
		// Method rule 3: only the binding itself is fixed. A write through
		// self, such as self.x, is a member place and is checked as one.
		diagnostics = append(diagnostics, typeErrorAt(assignment.Name, "cannot assign to self; self is a fixed binding"))
	case target.function:
		// A function declaration names code, not a replaceable storage slot.
		diagnostics = append(diagnostics, typeErrorAt(assignment.Name, "cannot assign to function "+assignment.Name.Lexeme))
	case target.parameter:
		diagnostics = append(diagnostics, typeErrorAt(assignment.Name,
			"cannot assign to parameter "+assignment.Name.Lexeme+"; parameters are fixed bindings"))
	case target.loopBinder:
		diagnostics = append(diagnostics, typeErrorAt(assignment.Name,
			"loop binder "+assignment.Name.Lexeme+" is immutable"))
	case !target.source.Writable:
		diagnostics = append(diagnostics, assignmentTargetDiagnostic(assignment.Target, assignment.Name))
	}

	// Assignment writes to the binding's declared storage slot, never to a
	// branch-local narrowed type, and an accepted assignment invalidates any
	// narrowing: the slot may hold nil again.
	targetType := target.typ
	targetBinding := BindingID(0)
	if variable, ok := assignment.Target.(parser.VariableExpression); ok && target.source.Binding != 0 {
		targetBinding = target.source.Binding
		if bound, status := names.lookup(variable.Name.Lexeme); status == nameFound {
			targetType = bound.typ
			target.source.Type = bound.typ
			target.use = bound.use
		}
	}
	targetUse := target.use
	if targetUse.Type == (compilerTypes.Type{}) {
		targetUse = compilerTypes.NewTypeUse(targetType)
	}
	initializer := checkInitializer(assignment.Initializer, targetUse, assignment.Name, names, typeEnvironment)
	for _, diagnostic := range initializerDiagnostics(initializer) {
		diagnostics = append(diagnostics, diagnostic)
	}
	if len(diagnostics) == 0 {
		if diagnostic := atomicCopyDiagnostic(initializer.source, assignment.Name); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	if len(diagnostics) == 0 && initializer.typ != (compilerTypes.Type{}) && !assignable(targetType, initializer.typ) {
		diagnostics = append(diagnostics, bindingMismatchDiagnostic(assignment.Name.Lexeme, targetType, initializer.typ, initializer.token))
	}
	if len(diagnostics) == 0 && names.flow != nil && targetBinding != 0 {
		names.flow.invalidateNarrowing(targetBinding)
		if sourceBinding := directPointerBinding(initializer.source, targetType); sourceBinding != 0 {
			names.flow.dropFreed(sourceBinding)
			names.flow.dropFreed(targetBinding)
		} else {
			names.flow.clearFreed(targetBinding)
		}
		seedStreamBindingFacts(names.flow, targetBinding, targetType, initializer.source)
	}
	if len(diagnostics) == 0 && targetBinding != 0 {
		// Assignment re-sources the slot: the binding now holds the ref-derived
		// value exactly when the assigned initializer traces to a ref, so the
		// flag is both set and cleared by the same check.
		names.setFromRef(targetBinding, nodeTracesToRef(&initializer.source.Node, names))
	}

	return Assignment{
		Name:         assignment.Name.Lexeme,
		Target:       target.source,
		Type:         targetType,
		Source:       initializer.source,
		SourceLine:   assignment.Name.Line,
		SourceColumn: assignment.Name.Column,
	}, diagnostics
}

func assignmentTargetDiagnostic(target parser.Expression, fallback lexer.Token) compilerTypes.Diagnostic {
	if variable, ok := target.(parser.VariableExpression); ok {
		return typeErrorAt(variable.Name, "cannot assign to constant "+variable.Name.Lexeme)
	}
	if property, ok := target.(parser.PropertyExpression); ok && property.Property.Lexeme != "value" {
		return typeErrorAt(property.Property, "cannot assign to read-only member "+placeDescription(target))
	}
	return typeErrorAt(fallback, "cannot write through a read-only pointer "+placeDescription(target))
}

// bindingMismatchDiagnostic names the binding for a function-pointer slot,
// where "expected X initializer" reads poorly against two Fun<...> spellings.
func bindingMismatchDiagnostic(name string, declaredType, actualType compilerTypes.Type, token lexer.Token) compilerTypes.Diagnostic {
	if declaredType.Signature != nil || actualType.Signature != nil {
		return typeErrorAt(token, fmt.Sprintf("%s requires %s; got %s", name, declaredType.Name, actualType.Name))
	}
	return typeMismatchDiagnostic(declaredType, actualType, token)
}

func typeMismatchDiagnostic(declaredType, actualType compilerTypes.Type, token lexer.Token) compilerTypes.Diagnostic {
	message := assignabilityMismatchMessage(declaredType, actualType)
	if message == "" {
		message = fmt.Sprintf("expected %s initializer; got %s", declaredType.Name, actualType.Name)
	}
	return typeErrorAt(token, message)
}

func assignabilityMismatchMessage(target, source compilerTypes.Type) string {
	if compilerTypes.IsNil(source) || compilerTypes.IsNullable(source) {
		if !compilerTypes.IsNullable(target) {
			return fmt.Sprintf("expected %s; got %s", target.Name, source.Name)
		}
	}
	if target.Element != nil && source.Element != nil {
		if target.PointeeWritable && !source.PointeeWritable && compilerTypes.IsUnknown(*source.Element) && !compilerTypes.IsUnknown(*target.Element) {
			return fmt.Sprintf("%s cannot recover writable access as %s", source.Name, target.Name)
		}
		if target.Element.Element != nil && compilerTypes.IsUnknown(*target.Element.Element) && source.Element.Element != nil {
			return fmt.Sprintf("cannot erase a nested pointer slot as %s", target.Name)
		}
		if target.PointeeWritable && source.PointeeWritable &&
			!compilerTypes.IsUnknown(*target.Element) && !compilerTypes.IsUnknown(*source.Element) &&
			!compilerTypes.Equal(*target.Element, *source.Element) {
			erased := "Ptr<Unknown>"
			if source.PointeeWritable {
				erased = "MutPtr<Unknown>"
			}
			return fmt.Sprintf("expected %s; got %s; erasure and recovery do not compose, bind %s first", target.Name, source.Name, erased)
		}
	}
	return ""
}
