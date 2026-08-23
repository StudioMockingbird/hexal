package checker

import (
	"fmt"
	"go/constant"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkPlace resolves only a syntactic place, tracking writability for the
// three-place walk so assignment and ref can read the binding and member modes.
func checkPlace(expression parser.Expression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	switch expression := expression.(type) {
	case parser.VariableExpression:
		if expression.Name.Kind == lexer.Self {
			return selfPlace(names, expression.Name)
		}
		binding, status := names.lookup(expression.Name.Lexeme)
		switch status {
		case nameMissing:
			return checkedExpression{
				token:      expression.Name,
				diagnostic: diagnosticAt(typeErrorAt(expression.Name, "unknown variable "+expression.Name.Lexeme)),
			}
		case nameModuleData:
			// Module storage lives in generated main and is unreachable from
			// a function body, Fun<...> bindings included.
			diagnostic := moduleDataDiagnostic(names.owner, expression.Name.Lexeme, expression.Name)
			return checkedExpression{token: expression.Name, diagnostic: &diagnostic}
		}
		if binding.kind == functionBinding {
			// A declared function is not storage: it is neither addressable nor
			// writable, and its name reads as the matching Fun<...> value.
			return checkedExpression{
				source: Operand{
					Kind: VariableOperand,
					Type: binding.typ,
					Name: expression.Name.Lexeme,
					Node: Expression{Kind: FunctionReferenceExpression, Name: expression.Name.Lexeme, LocalHelperOrdinal: binding.localHelperOrdinal, ResultType: binding.typ},
				},
				typ:      binding.typ,
				token:    expression.Name,
				function: true,
			}
		}
		if binding.kind == genericFunctionBinding {
			// A generic function is not a value without a Fun<...> target to
			// infer its arguments from.
			return checkedExpression{token: expression.Name, diagnostic: diagnosticAt(typeErrorAt(expression.Name, "cannot infer generic parameter for "+expression.Name.Lexeme))}
		}
		// Ordinary reads use the branch-local narrowed type when a null test
		// proved it; assignment and ref re-derive the declared storage type.
		placeType := binding.typ
		if narrowed, ok := names.flow.narrowedType(binding.id); ok {
			placeType = narrowed
		}
		var narrowedVariant *compilerTypes.AdtVariant
		if variant, ok := names.flow.narrowedVariant(binding.id); ok {
			narrowedVariant = variant
		}
		node := variableNodeWithBinding(expression.Name.Lexeme, binding.id)
		node.CollectionRoot = bindingCollectionRoot(binding)
		if binding.viewRootKind != ViewRootNone {
			node.ViewRoots = binding.viewRoots
			node.RootKind = binding.viewRootKind
		}
		return checkedExpression{
			source: Operand{
				Kind:        VariableOperand,
				Type:        placeType,
				Name:        expression.Name.Lexeme,
				Binding:     binding.id,
				Node:        node,
				Addressable: true,
				Writable:    binding.mutable,
			},
			typ:         placeType,
			use:         binding.use,
			storageType: binding.typ,
			variant:     narrowedVariant,
			token:       expression.Name,
			known:       binding.known,
			parameter:   binding.parameter,
			loopBinder:  binding.loopBinder,
		}
	case parser.PropertyExpression:
		// Alias.x with an import-alias receiver resolves x against the
		// target module's exported frame instead of the property path: an
		// exported unit variant first, then an exported function reference.
		if variable, isVariable := expression.Receiver.(parser.VariableExpression); isVariable {
			if target, ok := names.importAliasTarget(variable.Name.Lexeme); ok {
				return checkModuleQualifiedReference(expression, target, names)
			}
		}
		var receiver checkedExpression
		if _, temporary := expression.Receiver.(parser.ObjectLiteral); temporary {
			receiver = checkValue(expression.Receiver, names, typeEnvironment)
		} else {
			receiver = checkPlace(expression.Receiver, names, typeEnvironment)
		}
		if receiver.diagnostic != nil {
			return receiver
		}
		if receiver.variant != nil {
			return variantPayloadPlace(receiver, expression.Property)
		}
		// A nullable receiver has no members or .value until a null
		// test narrowed it to its pointer member. A bare binding names the
		// failing narrowing; a member path is never narrowable at all.
		if compilerTypes.IsNullable(receiver.typ) {
			diagnostic := nullableAccessDiagnostic(receiver, expression.Property, placeDescription(expression.Receiver))
			return checkedExpression{token: expression.Property, diagnostic: &diagnostic}
		}
		// On a pointer to an object, pointer.m means
		// pointer.value.m. One layer only, and the built-in .value property
		// wins, so an object member named value is reached as p.value.value.
		if receiver.typ.Element != nil && receiver.typ.Element.Object != nil && expression.Property.Lexeme != "value" {
			receiver = dereferencePlace(receiver, expression.Property, names.flow)
			if receiver.diagnostic != nil {
				return receiver
			}
		}
		if receiver.typ.Object != nil {
			member, ok := receiver.typ.Object.Member(expression.Property.Lexeme)
			if !ok {
				// Method rule 6: a method is code, not a member, so naming one
				// in a value position is a distinct error from a typo.
				if names.methods.lookup(receiver.typ.Object, expression.Property.Lexeme) != nil {
					diagnostic := typeErrorAt(expression.Property,
						fmt.Sprintf("%s is a method on %s; methods are not values", expression.Property.Lexeme, receiver.typ.Object.Name))
					return checkedExpression{token: expression.Property, diagnostic: &diagnostic}
				}
				return checkedExpression{
					token:      expression.Property,
					diagnostic: missingMemberDiagnostic(receiver.typ, expression.Property),
				}
			}
			return checkedExpression{
				source: Operand{
					Kind:        VariableOperand,
					Type:        member.Type,
					Node:        memberNode(receiver.source.Node, member),
					Addressable: receiver.source.Addressable,
					Writable:    receiver.source.Writable && member.Mutable,
				},
				typ: member.Type,
				use: func() compilerTypes.TypeUse {
					if member.Use.Type == (compilerTypes.Type{}) {
						return compilerTypes.NewTypeUse(member.Type)
					}
					return member.Use
				}(),
				token: expression.Property,
			}
		}
		if receiver.typ.Element == nil || expression.Property.Lexeme != "value" {
			message := fmt.Sprintf("cannot access .%s on %s; expected Ptr<T> or an object member", expression.Property.Lexeme, receiver.typ.Name)
			if expression.Property.Lexeme == "value" {
				message = fmt.Sprintf("cannot access .value on %s; expected Ptr<T>", receiver.typ.Name)
			}
			return checkedExpression{
				token:      expression.Property,
				diagnostic: diagnosticAt(typeErrorAt(expression.Property, message)),
			}
		}
		return dereferencePlace(receiver, expression.Property, names.flow)
	case parser.IndexExpression:
		return checkIndexPlace(expression, names, typeEnvironment)
	case parser.IntegerLiteral:
		initializer := integerInitializer(expression.Token, compilerTypes.Int32)
		return checkedExpression{source: initializer.source, typ: initializer.typ, token: initializer.token, diagnostic: initializer.diagnostic}
	case parser.DecimalLiteral:
		initializer := floatInitializer(expression.Token, compilerTypes.Float64)
		return checkedExpression{source: initializer.source, typ: initializer.typ, token: initializer.token, diagnostic: initializer.diagnostic}
	case parser.NegatedNumericLiteral:
		initializer := negatedInitializer(expression, compilerTypes.Type{})
		return checkedExpression{source: initializer.source, typ: initializer.typ, token: initializer.token, diagnostic: initializer.diagnostic}
	case parser.BooleanLiteral:
		return checkedExpression{source: constantOperand(compilerTypes.Bool, constant.MakeBool(expression.Token.Kind == lexer.True), expression.Token.Lexeme), typ: compilerTypes.Bool, token: expression.Token}
	case parser.ObjectLiteral:
		literal := checkObjectLiteral(expression, compilerTypes.Type{}, names, typeEnvironment)
		return checkedExpression{source: literal.source, typ: literal.typ, token: literal.token, diagnostic: literal.diagnostic}
	default:
		return checkedExpression{
			diagnostic: diagnosticAt(unknownAt(lexer.Token{Line: 1, Column: 1}, "unsupported place")),
		}
	}
}

// checkModuleQualifiedReference resolves Alias.x in a value position where
// Alias is an import alias. The property resolves to the target module's
// exported unit variant when one exists, then to its exported function; any
// other name is the visibility failure.
func checkModuleQualifiedReference(expression parser.PropertyExpression, target string, names *scope) checkedExpression {
	if adtType, variant, ok := names.registry.findExportedADTVariant(target, expression.Property.Lexeme); ok {
		return adtUnitVariant(adtType, variant, expression.Property)
	}
	if function, ok := names.registry.exportedFunction(target, expression.Property.Lexeme); ok {
		return checkedExpression{
			source: Operand{
				Kind: VariableOperand,
				Type: function.Type,
				Name: function.Name,
				Node: Expression{Kind: FunctionReferenceExpression, Name: function.Name, ResultType: function.Type, Module: target},
			},
			typ:      function.Type,
			token:    expression.Property,
			function: true,
		}
	}
	diagnostic := privateToModuleDiagnostic(expression.Property, expression.Property.Lexeme, target)
	return checkedExpression{token: expression.Property, diagnostic: &diagnostic}
}

// dereferencePlace walks one pointer layer, for both the explicit .value
// spelling and the inserted auto-dereference. The optional flow state lets
// place construction reject a known released local without affecting value
// arguments, which are not dereferences.
func dereferencePlace(receiver checkedExpression, token lexer.Token, states ...*flowState) checkedExpression {
	var state *flowState
	if len(states) > 0 {
		state = states[0]
	}
	if diagnostic := freedPointeeDiagnostic(receiver, token, state); diagnostic != nil {
		return checkedExpression{token: token, diagnostic: diagnostic}
	}
	if receiver.typ.Element != nil && compilerTypes.IsUnknown(*receiver.typ.Element) {
		diagnostic := typeErrorAt(token, receiver.typ.Name+" cannot be dereferenced; recover a concrete pointer type first")
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	use := compilerTypes.NewTypeUse(*receiver.typ.Element)
	if receiver.use.Element != nil {
		use = *receiver.use.Element
	}
	return checkedExpression{
		source: Operand{
			Kind:        VariableOperand,
			Type:        *receiver.typ.Element,
			Node:        unaryNode(DereferenceExpression, receiver.source.Node),
			Addressable: true,
			Writable:    receiver.typ.PointeeWritable,
		},
		typ:   *receiver.typ.Element,
		use:   use,
		token: token,
	}
}

func freedPointeeDiagnostic(receiver checkedExpression, token lexer.Token, state *flowState) *compilerTypes.Diagnostic {
	if state == nil || receiver.source.Node.Kind != VariableExpression || receiver.source.Node.Binding == 0 || !state.freed(receiver.source.Node.Binding) {
		return nil
	}
	diagnostic := useAfterFreeDiagnostic(token)
	return &diagnostic
}

func valueFromPlace(place checkedExpression) checkedExpression {
	if place.storageType.Union != nil && !compilerTypes.IsUnion(place.typ) {
		memberIndex := -1
		members := compilerTypes.UnionMembers(place.storageType)
		for index := 0; index < members.Len(); index++ {
			if member, _ := members.At(index); compilerTypes.Equal(member, place.typ) {
				memberIndex = index
				break
			}
		}
		if memberIndex >= 0 {
			operandNode := place.source.Node
			node := Expression{
				Kind:        UnionPayloadExpression,
				Operand:     &operandNode,
				OperandType: place.storageType,
				ResultType:  place.typ,
				MemberIndex: memberIndex,
			}
			source := Operand{Kind: ExpressionOperand, Type: place.typ, Node: node}
			return checkedExpression{source: source, typ: place.typ, use: place.use, token: place.token}
		}
	}
	// An ordinary read of a named binding keeps the binding's variable read
	// in the checked program: `if enabled then` renders the generated
	// binding, never its initializer literal. The initializer value stays
	// available as known-value metadata so compile-time diagnostics and
	// constant-required checks keep working.
	source := place.source
	source.Addressable = false
	source.Writable = false
	return checkedExpression{source: source, typ: place.typ, use: place.use, token: place.token, known: place.known}
}

// nullableAccessDiagnostic reports member or .value access through a nullable
// receiver that no null test narrowed. A bare local binding names the failing
// narrowing; a member path states the one-line workaround because member
// storage can be replaced through aliases the checker cannot see.
func nullableAccessDiagnostic(receiver checkedExpression, token lexer.Token, path string) compilerTypes.Diagnostic {
	if receiver.source.Node.Kind == VariableExpression {
		return typeErrorAt(token, fmt.Sprintf("%s may be Nil; narrow it before using .value", receiver.typ.Name))
	}
	return typeErrorAt(token, fmt.Sprintf("only a local binding can be narrowed; bind %s before testing it", path))
}

// checkReference types ref by the place's writability: a writable place
// yields MutPtr<T>, a fixed place yields Ptr<T>. There is no writability
// requirement; taking a read-only pointer to fixed storage is valid.
func checkReference(expression parser.RefExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	place := checkPlace(expression.Place, names, typeEnvironment)
	if place.diagnostic != nil {
		return place
	}
	// Neither a function declaration nor a Fun<...> binding is addressable;
	// the function's name already supplies the callable pointer.
	if place.function {
		diagnostic := typeErrorAt(place.token, "function declarations are not addressable; use "+place.token.Lexeme+" as a Fun value")
		return checkedExpression{token: place.token, diagnostic: &diagnostic}
	}
	if place.typ.Signature != nil {
		diagnostic := typeErrorAt(place.token, place.typ.Name+" bindings are not addressable")
		return checkedExpression{token: place.token, diagnostic: &diagnostic}
	}
	if place.typ.View != nil {
		diagnostic := typeErrorAt(place.token, "ref cannot take the address of a View binding")
		return checkedExpression{token: place.token, diagnostic: &diagnostic}
	}
	if place.typ.Atomic != nil {
		diagnostic := typeErrorAt(place.token, "Atomic values cannot be copied, assigned, addressed, or stored here")
		return checkedExpression{token: place.token, diagnostic: &diagnostic}
	}
	// ref names the binding's declared storage slot, not a narrowed read
	// type: the pointer must be able to observe every value the slot can
	// hold. A writable ref lets the slot's contents be replaced behind the
	// checker's back, so it escapes the binding and clears any narrowing.
	storageType := place.typ
	storageUse := place.use
	if variable, ok := expression.Place.(parser.VariableExpression); ok && place.source.Binding != 0 {
		if bound, status := names.lookup(variable.Name.Lexeme); status == nameFound {
			storageType = bound.typ
			storageUse = bound.use
		}
	}
	if storageUse.Type == (compilerTypes.Type{}) {
		storageUse = compilerTypes.NewTypeUse(storageType)
	}
	ptrType := typeEnvironment.PtrType(storageType)
	if place.source.Writable {
		ptrType = typeEnvironment.MutPtrType(storageType)
		// ponytail: escape commits even when the surrounding statement later
		// fails. Over-conservative only inside an already-failing program;
		// deferring it would need escape to thread through every statement
		// shape. Safe direction: it can only block a later narrowing.
		if names.flow != nil && place.source.Binding != 0 {
			names.flow.escape(place.source.Binding)
		}
	}
	// The checked node carries its interned result type: generation compares
	// identities against it and never reconstructs a fresh pointer type.
	addressNode := unaryNode(AddressOfExpression, place.source.Node)
	addressNode.ResultType = ptrType
	return checkedExpression{
		source: Operand{
			Kind:        VariableOperand,
			Type:        ptrType,
			Node:        addressNode,
			Addressable: true,
		},
		typ:   ptrType,
		use:   compilerTypes.PointerTypeUse(ptrType, storageUse),
		token: expression.Keyword,
	}
}

func placeDescription(expression parser.Expression) string {
	switch expression := expression.(type) {
	case parser.VariableExpression:
		return expression.Name.Lexeme
	case parser.PropertyExpression:
		return placeDescription(expression.Receiver) + "." + expression.Property.Lexeme
	default:
		return "place"
	}
}

func missingMemberDiagnostic(typ compilerTypes.Type, property lexer.Token) *compilerTypes.Diagnostic {
	return diagnosticAt(typeErrorAt(property, fmt.Sprintf("%s has no member %s", typ.Name, property.Lexeme)))
}

// baseBindingID walks a place expression's checked node chain back to its
// root variable binding. Member, pointer-dereference, and index steps all
// keep the same storage root. It returns 0 for temporaries and foreign roots.
func baseBindingID(node *Expression) BindingID {
	for node != nil {
		switch node.Kind {
		case VariableExpression:
			return node.Binding
		case MemberExpression, DereferenceExpression, IndexExpression:
			node = node.Operand
		default:
			return 0
		}
	}
	return 0
}

func collectionRootOfNode(node *Expression) BindingID {
	if node == nil {
		return 0
	}
	if node.CollectionRoot != 0 {
		return node.CollectionRoot
	}
	switch node.Kind {
	case VariableExpression:
		return node.Binding
	case MemberExpression, DereferenceExpression, IndexExpression:
		return collectionRootOfNode(node.Operand)
	default:
		return 0
	}
}

func bindingCollectionRoot(bound binding) BindingID {
	if bound.collectionRoot != 0 {
		return bound.collectionRoot
	}
	if isTrackedCollection(bound.typ) {
		return bound.id
	}
	return 0
}

func collectionRootForOperand(source Operand, names *scope, fallback BindingID) BindingID {
	if !isTrackedCollection(source.Type) {
		return 0
	}
	if root := collectionRootOfNode(&source.Node); root != 0 {
		if bound, ok := names.lookupBinding(root); ok && bound.collectionRoot != 0 {
			return bound.collectionRoot
		}
		return root
	}
	return fallback
}

func isTrackedCollection(typ compilerTypes.Type) bool {
	return typ.List != nil || typ.Dict != nil
}
