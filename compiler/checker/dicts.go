package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveDictTypeUse resolves the built-in Dict<K, V> form written as an
// ordinary generic type expression. K must be exactly Int32 or Strand; V
// must be a collection element.
func resolveDictTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 2 {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, "Dict requires exactly two type arguments"))
	}
	keyUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	if !compilerTypes.IsDictKey(keyUse.Type) {
		keyToken := typeExpressionToken(expression.Arguments[0], expression.Name)
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(keyToken, "dictionary key type must be Int32 or Strand"))
	}
	valueUse, diagnostic := resolveTypeUse(expression.Arguments[1], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	dict := typeEnvironment.DictType(keyUse.Type, valueUse.Type)
	if dict == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, valueUse.Type.Name+" is not a dictionary value type"))
	}
	return compilerTypes.NewTypeUse(dict), nil
}

// checkDictTypeCall resolves Dict<K, V>.new(heap) into a fresh owning
// dictionary.
func checkDictTypeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "new" || len(call.TypeArguments) != 2 || len(call.Arguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "Dict has no such operation; use Dict<K, V>.new(heap)"))}
	}
	dictUse, diagnostic := resolveDictTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Dict", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	heap := checkValue(call.Arguments[0], names, typeEnvironment)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := typeErrorAt(heap.token, "Dict<K, V>.new requires a Heap; got "+heap.typ.Name)
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
func checkDictMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	dictType := receiver.typ
	keyType := dictType.Dict.Key
	valueType := dictType.Dict.Value
	switch name {
	case "length":
		// Entry count is not an ordering, so reporting it exposes nothing
		// about the unspecified iteration order Dict deliberately hides.
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "length expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: dictType, ResultType: compilerTypes.SizeType, Element: valueType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: callee.Property}
	case "insert":
		if len(call.Arguments) != 2 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("insert expects 2 arguments; got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		key, diagnostic := checkDictKeyArgument(call.Arguments[0], callee.Property, keyType, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		value, diagnostic := listElementArgument(call.Arguments[1], callee.Property, valueType, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{key, value}, OperandType: dictType, ResultType: compilerTypes.Type{}, Element: valueType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	case "get", "find", "remove":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("%s expects 1 argument; got %d", name, len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		key, diagnostic := checkDictKeyArgument(call.Arguments[0], callee.Property, keyType, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		resultType := valueType
		if name == "find" {
			resultType = typeEnvironment.UnionType([]compilerTypes.Type{valueType, compilerTypes.Nil})
			if resultType == (compilerTypes.Type{}) {
				diagnostic := typeErrorAt(callee.Property, valueType.Name+" cannot be combined with Nil")
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
			}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{key}, OperandType: dictType, ResultType: resultType, Element: valueType}
		source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
		return checkedExpression{source: source, typ: resultType, token: callee.Property}
	case "contains":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("contains expects 1 argument; got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		key, diagnostic := checkDictKeyArgument(call.Arguments[0], callee.Property, keyType, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{key}, OperandType: dictType, ResultType: compilerTypes.Bool, Element: valueType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Bool, token: callee.Property}
	case "free":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("free expects 1 argument; got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := typeErrorAt(heap.token, "free requires a Heap; got "+heap.typ.Name)
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        CollectionMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{heap.source},
			OperandType: dictType,
			ResultType:  compilerTypes.Type{},
			Element:     valueType,
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		diagnostic := typeErrorAt(callee.Property, dictType.Name+" has no method "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
}

// checkDictKeyArgument checks one insert/get/contains/remove key against the
// key type. A String literal in a Strand position retains Strand's
// literal-only construction.
func checkDictKeyArgument(expression parser.Expression, fallback lexer.Token, keyType compilerTypes.Type, names *scope, typeEnvironment *compilerTypes.Environment) (Operand, *compilerTypes.Diagnostic) {
	checked := checkInitializer(expression, compilerTypes.NewTypeUse(keyType), fallback, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return Operand{}, &diagnostics[0]
	}
	if !assignable(keyType, checked.typ) {
		diagnostic := typeErrorAt(checked.token, "dictionary key requires "+keyType.Name+"; got "+checked.typ.Name)
		return Operand{}, &diagnostic
	}
	return checked.source, nil
}
