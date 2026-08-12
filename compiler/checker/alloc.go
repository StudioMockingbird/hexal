package checker

import (
	"fmt"

	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkDeferStatement types a deferred expression and registers its action on
// the current lexical scope. A direct call is captured at registration; every
// other expression evaluates when the scope exits.
func checkDeferStatement(statement parser.DeferStatement, names *scope, typeEnvironment *compilerTypes.Environment) (DeferStatement, compilerTypes.Diagnostics) {
	names.cleanupDepth++
	defer func() { names.cleanupDepth-- }()
	action := DeferredAction{}
	var source Operand
	if call, isCall := statement.Expression.(parser.CallExpression); isCall {
		checked := checkCall(call, names, typeEnvironment)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return DeferStatement{}, diagnostics
		}
		action.IsCall = true
		action.Call = &checked.source
		source = checked.source
	} else {
		checked := checkExpression(statement.Expression, expressionContext{inCleanup: true}, names, typeEnvironment)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return DeferStatement{}, diagnostics
		}
		action.Value = &checked.source
		source = checked.source
	}
	names.defers = append(names.defers, action)
	return DeferStatement{
		Expression:   source,
		Action:       action,
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
	}, nil
}

// checkHeapTypeCall resolves a call written as Heap.<name>(...) where the
// receiver names the built-in type itself rather than a Heap value.
func checkHeapTypeCall(call parser.CallExpression, token parser.VariableExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := call.Callee.(parser.PropertyExpression).Property.Lexeme
	if name != "new" || len(call.Arguments) != 0 {
		return checkedExpression{token: token.Name, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     token.Name.Line,
			Column:   token.Name.Column,
			Message:  "Heap has no such operation; use Heap.new()",
		}}
	}
	source := constantOperand(compilerTypes.Heap, nil, "")
	source.Node = constantNode(source)
	return checkedExpression{source: source, typ: compilerTypes.Heap, token: token.Name, known: &source}
}

// checkHeapAllocate resolves h.allocate<T>(initial) into a checked
// HeapAllocateExpression returning MutPtr<T>.
func checkHeapAllocate(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.Arguments) != 1 {
		message := "allocation requires an explicit initializer"
		if len(call.Arguments) > 1 {
			message = "allocate expects 1 argument, got " + fmt.Sprint(len(call.Arguments))
		}
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  message,
		}}
	}
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "allocate requires exactly one type argument",
		}}
	}
	elementUse, diagnostic := resolveTypeUse(call.TypeArguments[0], callee.Property, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	element := elementUse.Type
	if !compilerTypes.IsCompleteValue(element) || compilerTypes.IsUnknown(element) || element.Signature != nil || compilerTypes.IsManaged(element) || compilerTypes.ContainsAtomic(element) {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "allocation requires a complete finite type",
		}}
	}
	initial := checkInitializer(call.Arguments[0], elementUse, callee.Property, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if initial.typ != (compilerTypes.Type{}) && !assignable(element, initial.typ) {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  fmt.Sprintf("allocation initializer requires %s, got %s", element.Name, initial.typ.Name),
		}}
	}
	receiverNode := expressionNode(receiver.source)
	result := typeEnvironment.MutPtrType(element)
	node := Expression{
		Kind:        HeapAllocateExpression,
		Operand:     &receiverNode,
		Arguments:   []Operand{initial.source},
		OperandType: compilerTypes.Heap,
		ResultType:  result,
		Element:     element,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Node: node}
	return checkedExpression{source: source, typ: result, token: callee.Property}
}

// checkHeapFree resolves h.free(value) into a no-result checked
// HeapFreeExpression. The value may be Ptr<T> or MutPtr<T>.
func checkHeapFree(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "free expects exactly one pointer argument",
		}}
	}
	value := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Type{}), callee.Property, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if value.typ.Element == nil {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "value is not an allocation produced by this Heap",
		}}
	}
	receiverNode := expressionNode(receiver.source)
	node := Expression{
		Kind:        HeapFreeExpression,
		Operand:     &receiverNode,
		Arguments:   []Operand{value.source},
		OperandType: compilerTypes.Heap,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
}
