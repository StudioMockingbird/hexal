package checker

import (
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// isCompilerOwnedPointerOperation reports whether name is one of the two
// compiler-owned Ptr surfaces. It is the single recognition point: a pointer
// receiver reaches these operations and nothing else claims the spellings.
func isCompilerOwnedPointerOperation(name string) bool {
	return name == "offset" || name == "cast"
}

// checkPointerArithmeticCall resolves p.offset(count) and p.cast<U>() on a
// non-nullable pointer receiver. Both preserve the receiver's access mode:
// a read-only pointer can never produce a writable one.
func checkPointerArithmeticCall(call methodCall) checkedExpression {
	if call.callee.Property.Lexeme == "cast" {
		return checkPointerCastCall(call)
	}
	return checkPointerOffsetCall(call)
}

func checkPointerOffsetCall(call methodCall) checkedExpression {
	property := call.callee.Property
	if len(call.call.TypeArguments) != 0 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, "offset takes no type arguments"))}
	}
	if len(call.call.Arguments) != 1 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, "offset expects 1 argument (count)"))}
	}
	element := *call.receiver.typ.Element
	if diagnostic := completePointeeDiagnostic(element, property); diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	if diagnostic := freedPointeeDiagnostic(call.receiver, property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	count, countDiagnostics := checkForwardCount(call.call.Arguments[0], property, "offset", call.ctx)
	if len(countDiagnostics) > 0 {
		return checkedExpression{token: property, diagnostics: countDiagnostics, diagnostic: &countDiagnostics[0]}
	}
	// C proves nothing about staying inside one array object; the programmer
	// asserts it, so the region must be explicit.
	if diagnostic := requireUnsafe(call.ctx, property, unsafePointerOffset); diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	node := Expression{
		Kind:        PointerOffsetExpression,
		Name:        "offset",
		Operand:     &call.receiver.source.Node,
		Arguments:   []Operand{count},
		OperandType: call.receiver.typ,
		ResultType:  call.receiver.typ,
		Element:     element,
	}
	source := Operand{Kind: ExpressionOperand, Type: call.receiver.typ, Name: "offset", Node: node}
	return checkedExpression{source: source, typ: call.receiver.typ, token: property}
}

func checkPointerCastCall(call methodCall) checkedExpression {
	property := call.callee.Property
	if len(call.call.Arguments) != 0 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, "cast expects no arguments"))}
	}
	if len(call.call.TypeArguments) != 1 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, "cast requires exactly one type argument"))}
	}
	destinationUse, diagnostic := resolveTypeUse(call.call.TypeArguments[0], property, call.ctx.typeEnvironment, call.ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	// A cast inspects no object, so an erased or incomplete pointee is valid
	// on either side; offset, indexing, and `^` stay rejected until the
	// pointee is complete.
	result := call.ctx.typeEnvironment.PtrType(destinationUse.Type)
	if call.receiver.typ.PointeeWritable {
		result = call.ctx.typeEnvironment.MutPtrType(destinationUse.Type)
	}
	if result == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: typeErrorPointerConstruction(property)}
	}
	if diagnostic := requireUnsafe(call.ctx, property, unsafePointerCast); diagnostic != nil {
		return checkedExpression{token: property, diagnostic: diagnostic}
	}
	node := Expression{
		Kind:        PointerCastExpression,
		Name:        "cast",
		Operand:     &call.receiver.source.Node,
		OperandType: call.receiver.typ,
		ResultType:  result,
		Element:     destinationUse.Type,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: "cast", Node: node}
	return checkedExpression{source: source, typ: result, token: property}
}

// checkPointerIndexPlace resolves pointer[index] as a place: readable through
// either mode, writable only through Ptr<mut T>.
func checkPointerIndexPlace(expression parser.IndexExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	bracket := expression.OpenBracket
	if compilerTypes.IsNullable(receiver.typ) {
		diagnostic := nullableAccessDiagnostic(receiver, bracket, placeDescription(expression.Receiver))
		return checkedExpression{token: bracket, diagnostic: &diagnostic}
	}
	element := *receiver.typ.Element
	// `pointer[index]` reading like ordinary collection indexing while
	// meaning "the next collection" is the one confusion worth refusing
	// outright: the two intents get their own spellings instead.
	if element.Array != nil || element.Slice != nil || element.List != nil {
		diagnostic := typeErrorAt(bracket, "pointer indexing of "+receiver.typ.Name+
			" is ambiguous; use (^pointer)[index] to index the collection or pointer.offset(index) to advance the pointer")
		return checkedExpression{token: bracket, diagnostic: &diagnostic}
	}
	if diagnostic := completePointeeDiagnostic(element, bracket); diagnostic != nil {
		return checkedExpression{token: bracket, diagnostic: diagnostic}
	}
	if diagnostic := freedPointeeDiagnostic(receiver, bracket, ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: bracket, diagnostic: diagnostic}
	}
	index, indexDiagnostics := checkForwardCount(expression.Index, bracket, "pointer indexing", ctx)
	if len(indexDiagnostics) > 0 {
		return checkedExpression{token: bracket, diagnostics: indexDiagnostics, diagnostic: &indexDiagnostics[0]}
	}
	if diagnostic := requireUnsafe(ctx, bracket, unsafePointerIndex); diagnostic != nil {
		return checkedExpression{token: bracket, diagnostic: diagnostic}
	}
	return checkedExpression{
		source: Operand{
			Kind: VariableOperand,
			Type: element,
			Node: Expression{
				Kind:        PointerIndexExpression,
				Operand:     &receiver.source.Node,
				Arguments:   []Operand{index},
				OperandType: receiver.typ,
				ResultType:  element,
				Element:     element,
			},
			Addressable: true,
			Writable:    receiver.typ.PointeeWritable,
		},
		typ:   element,
		token: bracket,
	}
}

// checkForwardCount types one forward-only Size operand. Offsets and indices
// are unsigned in this version, so no signed or implicitly converted numeric
// operand is admitted.
func checkForwardCount(expression parser.Expression, token lexer.Token, operation string, ctx checkContext) (Operand, compilerTypes.Diagnostics) {
	checked := checkInitializer(expression, compilerTypes.NewTypeUse(compilerTypes.SizeType), token, ctx)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return Operand{}, diagnostics
	}
	if !assignable(compilerTypes.SizeType, checked.typ) {
		return Operand{}, compilerTypes.Diagnostics{typeErrorAt(checked.token, operation+" requires Size; got "+checked.typ.Name)}
	}
	return checked.source, nil
}

// completePointeeDiagnostic rejects address traversal through a pointee whose
// object layout is unknown, erased Unknown included: C cannot scale the step
// without a complete type.
func completePointeeDiagnostic(element compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	if compilerTypes.IsCompleteValue(element) {
		return nil
	}
	return diagnosticAt(typeErrorAt(token, "pointer arithmetic requires a complete pointee type; got "+element.Name))
}
