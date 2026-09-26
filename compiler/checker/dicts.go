package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveDictTypeUse resolves the built-in Dict<K, V> form written as an
// ordinary generic type expression. K must be exactly Int32 or String<N>; V
// must be a collection element.
func resolveDictTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 2 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diag.DictTypeArgumentCount()))
	}
	keyUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	if !compilerTypes.IsDictKey(keyUse.Type) {
		keyToken := typeExpressionToken(expression.Arguments[0], expression.Name)
		if compilerTypes.IsString(keyUse.Type) {
			return compilerTypes.TypeUse{}, diagnosticAt(messageAt(keyToken, diag.DictKeyStringNotAllowed()))
		}
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(keyToken, diag.DictKeyTypeInvalid()))
	}
	valueUse, diagnostic := resolveTypeUse(expression.Arguments[1], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	dict := typeEnvironment.DictType(keyUse.Type, valueUse.Type)
	if dict == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diag.InvalidDictValueType(valueUse.Type.Name)))
	}
	return compilerTypes.NewTypeUse(dict), nil
}

// checkDictTypeCall resolves Dict<K, V>(heap) into a fresh owning
// dictionary.
func checkDictTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.TypeArguments) != 2 || len(call.Arguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.DictConstructorArgumentShape()))}
	}
	dictUse, diagnostic := resolveDictTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Dict", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := messageAt(heap.token, diag.DictConstructorHeapType(heap.typ.Name))
		return checkedExpression{token: heap.token, diagnostic: &diagnostic}
	}
	node := Expression{
		Kind:        DictNewExpression,
		Operand:     &heap.source.Node,
		Arguments:   []Operand{heap.source},
		OperandType: compilerTypes.Heap,
		ResultType:  dictUse.Type,
		Element:     dictUse.Type.Dict.Value,
	}
	source := Operand{Kind: ExpressionOperand, Type: dictUse.Type, Name: "new", Node: node}
	return checkedExpression{source: source, typ: dictUse.Type, token: callee}
}

// checkDictMethodCall dispatches the built-in Dict methods: insert, get, find,
// contains, remove, and free.
func checkDictMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	dictType := call.receiver.typ
	keyType := dictType.Dict.Key
	valueType := dictType.Dict.Value
	if !hasBuiltinMethod(dictType, name) {
		diagnostic := messageAt(call.callee.Property, diag.CollectionHasNoMethod(dictType.Name, name))
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	switch name {
	case "length":
		// Entry count is not an ordering, so reporting it exposes nothing
		// about the unspecified iteration order Dict deliberately hides.
		if len(call.call.Arguments) != 0 {
			diagnostic := messageAt(call.callee.Property, diag.CollectionMethodNoArguments("length"))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: dictType, ResultType: compilerTypes.SizeType, Element: valueType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: call.callee.Property}
	case "insert":
		if len(call.call.Arguments) != 2 {
			diagnostic := messageAt(call.callee.Property, diag.DictMethodArity("insert", 2, len(call.call.Arguments)))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
		key, diagnostic := checkDictKeyArgument(call.call.Arguments[0], call.callee.Property, keyType, call.ctx)
		if diagnostic != nil {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
		}
		value, diagnostic := listElementArgument(call.call.Arguments[1], call.callee.Property, valueType, call.ctx)
		if diagnostic != nil {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{key, value}, OperandType: dictType, ResultType: compilerTypes.Type{}, Element: valueType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	case "get", "find", "remove":
		if len(call.call.Arguments) != 1 {
			diagnostic := messageAt(call.callee.Property, diag.DictMethodArity(name, 1, len(call.call.Arguments)))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
		key, diagnostic := checkDictKeyArgument(call.call.Arguments[0], call.callee.Property, keyType, call.ctx)
		if diagnostic != nil {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
		}
		resultType := valueType
		if name == "find" {
			resultType = call.ctx.typeEnvironment.UnionType([]compilerTypes.Type{valueType, compilerTypes.Nil})
			if resultType == (compilerTypes.Type{}) {
				diagnostic := messageAt(call.callee.Property, diag.DictFindValueCannotContainNil(valueType.Name))
				return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
			}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{key}, OperandType: dictType, ResultType: resultType, Element: valueType}
		source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
		return checkedExpression{source: source, typ: resultType, token: call.callee.Property}
	case "contains":
		if len(call.call.Arguments) != 1 {
			diagnostic := messageAt(call.callee.Property, diag.DictMethodArity("contains", 1, len(call.call.Arguments)))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
		key, diagnostic := checkDictKeyArgument(call.call.Arguments[0], call.callee.Property, keyType, call.ctx)
		if diagnostic != nil {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{key}, OperandType: dictType, ResultType: compilerTypes.Bool, Element: valueType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Bool, token: call.callee.Property}
	case "free":
		if len(call.call.Arguments) != 1 {
			diagnostic := messageAt(call.callee.Property, diag.DictMethodArity("free", 1, len(call.call.Arguments)))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
		heap := checkValue(call.call.Arguments[0], call.ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := messageAt(heap.token, diag.DictFreeHeapType(heap.typ.Name))
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        CollectionMethodCallExpression,
			Name:        name,
			Operand:     &call.receiver.source.Node,
			Arguments:   []Operand{heap.source},
			OperandType: dictType,
			ResultType:  compilerTypes.Type{},
			Element:     valueType,
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	default:
		return unexpectedBuiltinMethod(dictType, call.callee.Property)
	}
}

// checkDictKeyArgument checks one insert/get/contains/remove key against the
// key type. A string literal in a String<N> position is measured against N at
// compile time; any other text must already be exactly the key type.
func checkDictKeyArgument(expression parser.Expression, fallback lexer.Token, keyType compilerTypes.Type, ctx checkContext) (Operand, *compilerTypes.Diagnostic) {
	checked := checkInitializer(expression, compilerTypes.NewTypeUse(keyType), fallback, ctx)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return Operand{}, &diagnostics[0]
	}
	if !assignable(keyType, checked.typ) {
		diagnostic := messageAt(checked.token, diag.DictKeyTypeMismatch(keyType.Name, checked.typ.Name, textMismatchDetails(keyType, checked.typ)))
		return Operand{}, &diagnostic
	}
	return checked.source, nil
}
