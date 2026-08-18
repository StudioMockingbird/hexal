package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkDeferStatement types a deferred expression and registers its action on
// the current lexical scope. A direct call is captured at registration; every
// other expression evaluates when the scope exits.
func checkDeferStatement(statement parser.DeferStatement, names *scope, typeEnvironment *compilerTypes.Environment) (DeferStatement, compilerTypes.Diagnostics) {
	names.cleanupDepth++
	defer func() { names.cleanupDepth-- }()
	action := DeferredAction{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}
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
	if !compilerTypes.Eligible(element, compilerTypes.PositionHeapAllocation) {
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
	if nodeTracesToRef(&value.source.Node, names) {
		diagnostic := freeLocalStorageDiagnostic(callee.Property)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	if names.cleanupDepth == 0 {
		if diagnostic := checkTrackedHeapFree(value.source, callee.Property, names); diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
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

func checkTrackedHeapFree(value Operand, token lexer.Token, names *scope) *compilerTypes.Diagnostic {
	if names == nil || names.flow == nil || value.Node.Kind != VariableExpression || value.Node.Binding == 0 {
		return nil
	}
	if names.flow.freed(value.Node.Binding) {
		diagnostic := doubleFreeDiagnostic(token)
		return &diagnostic
	}
	names.flow.markFreed(value.Node.Binding)
	return nil
}

func validateDeferredActions(names *scope) compilerTypes.Diagnostics {
	if names == nil || names.flow == nil {
		return nil
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for index := len(names.defers) - 1; index >= 0; index-- {
		action := names.defers[index]
		var source *Operand
		if action.IsCall {
			source = action.Call
		} else {
			source = action.Value
		}
		if source == nil || source.Node.Kind != HeapFreeExpression || len(source.Node.Arguments) != 1 {
			continue
		}
		token := lexer.Token{Line: action.SourceLine, Column: action.SourceColumn}
		if diagnostic := checkTrackedHeapFree(source.Node.Arguments[0], token, names); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	return diagnostics
}
