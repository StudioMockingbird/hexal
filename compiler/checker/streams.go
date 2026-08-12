package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// RFC 0031: lazy single-pass pull Stream<T> with explicit next, EoS,
// produce, List sources, filter/map/take adapters, for iteration, and
// explicit free.

// resolveStreamTypeUse resolves the built-in Stream<T> form written as an
// ordinary generic type expression. The element must be complete and
// finite-sized and must not be EoS or a union containing EoS as a top-level
// member.
func resolveStreamTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  "Stream requires exactly one element type",
		}
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	element := elementUse.Type
	if compilerTypes.IsEoS(element) || compilerTypes.UnionContainsEoS(element) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  "Stream element type cannot be EoS or include EoS as a top-level union member",
		}
	}
	stream := typeEnvironment.StreamType(element)
	if stream == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  "Stream State must be complete and finite-sized; " + element.Name + " is not a stream element type",
		}
	}
	return compilerTypes.NewTypeUse(stream), nil
}

// checkStreamTypeCall resolves Stream<T>.new() and Stream<T>.produce(...).
func checkStreamTypeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	streamUse, diagnostic := resolveStreamTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Stream", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	streamType := streamUse.Type
	element := streamType.Stream.Element

	switch property.Lexeme {
	case "new":
		if len(call.Arguments) != 0 {
			return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     property.Line,
				Column:   property.Column,
				Message:  "Stream.new() takes no arguments; the empty Stream allocates nothing",
			}}
		}
		node := Expression{Kind: StreamConstructorExpression, Name: "new", OperandType: streamType, ResultType: streamType, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: streamType, Name: "new", Node: node}
		return checkedExpression{source: source, typ: streamType, token: property}
	case "produce":
		if len(call.Arguments) != 3 {
			return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     property.Line,
				Column:   property.Column,
				Message:  fmt.Sprintf("produce expects 3 arguments (allocator, initial_state, next), got %d", len(call.Arguments)),
			}}
		}
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     heap.token.Line,
				Column:   heap.token.Column,
				Message:  "produce requires a Heap allocator; got " + heap.typ.Name,
			}}
		}
		state := checkInitializer(call.Arguments[1], compilerTypes.TypeUse{}, tokenOf(call.Arguments[1]), names, typeEnvironment)
		if diagnostics := initializerDiagnostics(state); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
		}
		if !compilerTypes.IsCompleteValue(state.typ) || compilerTypes.IsUnknown(state.typ) || compilerTypes.ContainsTypeParameter(state.typ) {
			return checkedExpression{token: state.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     state.token.Line,
				Column:   state.token.Column,
				Message:  "Stream State must be complete and finite-sized",
			}}
		}
		callback, callbackDiagnostic := resolveStreamCallback(call.Arguments[2], property, names, typeEnvironment)
		if callbackDiagnostic != nil {
			return checkedExpression{token: property, diagnostic: callbackDiagnostic}
		}
		// The callback must accept MutPtr<State> and return T | EoS.
		statePointer := typeEnvironment.MutPtrType(state.typ)
		if statePointer == (compilerTypes.Type{}) {
			return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     property.Line,
				Column:   property.Column,
				Message:  "Stream State must be complete and finite-sized",
			}}
		}
		resultUnion := streamStepUnion(typeEnvironment, element)
		if callbackDiagnostic := checkProduceCallback(callback, statePointer, resultUnion, property); callbackDiagnostic != nil {
			return checkedExpression{token: property, diagnostic: callbackDiagnostic}
		}
		node := Expression{
			Kind:        StreamConstructorExpression,
			Name:        "produce",
			Operand:     &state.source.Node,
			OperandType: streamType,
			ResultType:  streamType,
			Element:     element,
		}
		arguments := []Operand{heap.source, state.source, callback}
		node.Arguments = arguments
		source := Operand{Kind: ExpressionOperand, Type: streamType, Name: "produce", Node: node}
		return checkedExpression{source: source, typ: streamType, token: property}
	default:
		return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     property.Line,
			Column:   property.Column,
			Message:  "Stream has no such operation; use Stream<T>.new() or Stream<T>.produce(heap, state, next)",
		}}
	}
}

// checkStreamMethodCall dispatches the built-in Stream receiver methods:
// next, filter, map, take, and free (RFC 0031).
func checkStreamMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	streamType := receiver.typ
	element := streamType.Stream.Element

	switch name {
	case "next":
		if len(call.Arguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     callee.Property.Line,
				Column:   callee.Property.Column,
				Message:  "next expects no arguments",
			}}
		}
		resultUnion := streamStepUnion(typeEnvironment, element)
		node := Expression{Kind: StreamMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: streamType, ResultType: resultUnion, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: resultUnion, Name: name, Node: node}
		return checkedExpression{source: source, typ: resultUnion, token: callee.Property}
	case "filter":
		return checkStreamAdapterCall(call, callee, receiver, environment, typeEnvironment, "filter", element, nil)
	case "map":
		return checkStreamAdapterCall(call, callee, receiver, environment, typeEnvironment, "map", element, nil)
	case "take":
		if len(call.Arguments) != 2 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     callee.Property.Line,
				Column:   callee.Property.Column,
				Message:  fmt.Sprintf("take expects 2 arguments (allocator, count), got %d", len(call.Arguments)),
			}}
		}
		heap := checkValue(call.Arguments[0], environment, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     heap.token.Line,
				Column:   heap.token.Column,
				Message:  "take requires a Heap allocator; got " + heap.typ.Name,
			}}
		}
		count := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[1]), environment, typeEnvironment)
		if diagnostics := initializerDiagnostics(count); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
		}
		if !assignable(compilerTypes.SizeType, count.typ) {
			return checkedExpression{token: count.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     count.token.Line,
				Column:   count.token.Column,
				Message:  "Stream take count must be representable as Size",
			}}
		}
		node := Expression{Kind: StreamMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{heap.source, count.source}, OperandType: streamType, ResultType: streamType, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: streamType, Name: name, Node: node}
		return checkedExpression{source: source, typ: streamType, token: callee.Property}
	case "free":
		if len(call.Arguments) != 1 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     callee.Property.Line,
				Column:   callee.Property.Column,
				Message:  fmt.Sprintf("free expects 1 argument (allocator), got %d", len(call.Arguments)),
			}}
		}
		heap := checkValue(call.Arguments[0], environment, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     heap.token.Line,
				Column:   heap.token.Column,
				Message:  "free requires a Heap; got " + heap.typ.Name,
			}}
		}
		node := Expression{Kind: StreamMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{heap.source}, OperandType: streamType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "Stream has no method " + name + "; use next, filter, map, take, or free",
		}}
	}
}

// checkStreamAdapterCall resolves filter and map, which share the shape:
// (allocator, callback) producing a new Stream node.
func checkStreamAdapterCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, environment *scope, typeEnvironment *compilerTypes.Environment, name string, element compilerTypes.Type, unused *compilerTypes.Type) checkedExpression {
	if len(call.Arguments) != 2 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  fmt.Sprintf("%s expects 2 arguments (allocator, callback), got %d", name, len(call.Arguments)),
		}}
	}
	heap := checkValue(call.Arguments[0], environment, typeEnvironment)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     heap.token.Line,
			Column:   heap.token.Column,
			Message:  name + " requires a Heap allocator; got " + heap.typ.Name,
		}}
	}
	callback, callbackDiagnostic := resolveStreamCallback(call.Arguments[1], callee.Property, environment, typeEnvironment)
	if callbackDiagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: callbackDiagnostic}
	}
	resultType := receiver.typ
	if name == "map" {
		if callback.Type.Signature.Result == nil {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     callee.Property.Line,
				Column:   callee.Property.Column,
				Message:  "Stream mapper must accept T and return a value",
			}}
		}
		resultType = typeEnvironment.StreamType(*callback.Type.Signature.Result)
		if resultType == (compilerTypes.Type{}) {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     callee.Property.Line,
				Column:   callee.Property.Column,
				Message:  "Stream mapper must accept T and return a complete finite-sized element type",
			}}
		}
	}
	if diagnostic := checkAdapterCallback(name, callback, element, resultType, callee.Property); diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	node := Expression{
		Kind:        StreamMethodCallExpression,
		Name:        name,
		Operand:     &receiver.source.Node,
		Arguments:   []Operand{heap.source, callback},
		OperandType: receiver.typ,
		ResultType:  resultType,
		Element:     element,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
	return checkedExpression{source: source, typ: resultType, token: callee.Property}
}

// checkListStreamCall resolves List<T>.stream(heap): a non-owning lazy
// Stream over the List's existing elements (RFC 0031).
func checkListStreamCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.Arguments) != 1 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  fmt.Sprintf("stream expects 1 argument (allocator), got %d", len(call.Arguments)),
		}}
	}
	heap := checkValue(call.Arguments[0], environment, typeEnvironment)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     heap.token.Line,
			Column:   heap.token.Column,
			Message:  "stream requires a Heap allocator; got " + heap.typ.Name,
		}}
	}
	element := receiver.typ.List.Element
	stream := typeEnvironment.StreamType(element)
	if stream == (compilerTypes.Type{}) {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  element.Name + " is not a stream element type",
		}}
	}
	node := Expression{Kind: StreamMethodCallExpression, Name: "list_stream", Operand: &receiver.source.Node, Arguments: []Operand{heap.source}, OperandType: receiver.typ, ResultType: stream, Element: element}
	source := Operand{Kind: ExpressionOperand, Type: stream, Name: "list_stream", Node: node}
	return checkedExpression{source: source, typ: stream, token: callee.Property}
}

// resolveStreamCallback resolves a callback argument to a concrete named
// function value. Seawitch has no closures, so every Stream callback must be
// an ordinary named function reference.
func resolveStreamCallback(argument parser.Expression, fallback lexer.Token, environment *scope, typeEnvironment *compilerTypes.Environment) (Operand, *compilerTypes.Diagnostic) {
	checked := checkValue(argument, environment, typeEnvironment)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return Operand{}, &diagnostics[0]
	}
	if checked.source.Node.Kind != FunctionReferenceExpression || checked.source.Node.Name == "" {
		return Operand{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     fallback.Line,
			Column:   fallback.Column,
			Message:  "a Stream callback must be a named function",
		}
	}
	if checked.source.Node.ResultType.Signature == nil {
		return Operand{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     fallback.Line,
			Column:   fallback.Column,
			Message:  "a Stream callback must be a named function with a known signature",
		}
	}
	return checked.source, nil
}

// checkProduceCallback verifies Fun<(MutPtr<State>) : T | EoS>.
func checkProduceCallback(callback Operand, statePointer compilerTypes.Type, resultUnion compilerTypes.Type, fallback lexer.Token) *compilerTypes.Diagnostic {
	signature := callback.Type.Signature
	if len(signature.Parameters) != 1 || !compilerTypes.Equal(signature.Parameters[0], statePointer) {
		return &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     fallback.Line,
			Column:   fallback.Column,
			Message:  "Stream producer callback must accept MutPtr<State>",
		}
	}
	if signature.Result == nil || !compilerTypes.Equal(*signature.Result, resultUnion) {
		return &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     fallback.Line,
			Column:   fallback.Column,
			Message:  "Stream producer callback must return T | EoS",
		}
	}
	return nil
}

// checkAdapterCallback verifies filter's Fun<(T) : Bool> and map's
// Fun<(T) : U> shapes against the element types.
func checkAdapterCallback(name string, callback Operand, element compilerTypes.Type, resultType compilerTypes.Type, fallback lexer.Token) *compilerTypes.Diagnostic {
	signature := callback.Type.Signature
	if len(signature.Parameters) != 1 || !compilerTypes.Equal(signature.Parameters[0], element) {
		return &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     fallback.Line,
			Column:   fallback.Column,
			Message:  fmt.Sprintf("Stream %s callback must accept %s", name, element.Name),
		}
	}
	if name == "filter" {
		if signature.Result == nil || !compilerTypes.Equal(*signature.Result, compilerTypes.Bool) {
			return &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     fallback.Line,
				Column:   fallback.Column,
				Message:  "Stream predicate must have type Fun<(T) : Bool>",
			}
		}
		return nil
	}
	// map: result union must match the mapper's result exactly (the output
	// Stream element is the mapper's result type, so no union check needed).
	_ = resultType
	return nil
}

// streamStepUnion builds the T | EoS result union of one next() call.
func streamStepUnion(typeEnvironment *compilerTypes.Environment, element compilerTypes.Type) compilerTypes.Type {
	return typeEnvironment.UnionType([]compilerTypes.Type{element, compilerTypes.EoS})
}
