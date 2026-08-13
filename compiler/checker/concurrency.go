package checker

import (
	"fmt"
	"go/constant"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// RFC 0037: Task<T>, spawn, join/detach/yield, Channel<T>, Mutex, and
// Atomic<T>.

// resolveTaskTypeUse resolves Task<R>.
func resolveTaskTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Name.Line, Column: expression.Name.Column,
			Message: "Task requires exactly one result type",
		}
	}
	resultUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	task := typeEnvironment.TaskType(resultUse.Type)
	if task == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Name.Line, Column: expression.Name.Column,
			Message: "Task result type must be complete and shallow-copyable; got " + resultUse.Type.Name,
		}
	}
	return compilerTypes.NewTypeUse(task), nil
}

// resolveChannelTypeUse resolves Channel<T>.
func resolveChannelTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Name.Line, Column: expression.Name.Column,
			Message: "Channel requires exactly one element type",
		}
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	element := elementUse.Type
	if compilerTypes.IsEoS(element) || compilerTypes.UnionContainsEoS(element) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Name.Line, Column: expression.Name.Column,
			Message: "Channel element cannot be or include EoS as a top-level member",
		}
	}
	if compilerTypes.ContainsAtomic(element) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Name.Line, Column: expression.Name.Column,
			Message: "Channel element contains a non-copyable Atomic value",
		}
	}
	channel := typeEnvironment.ChannelType(element)
	if channel == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Name.Line, Column: expression.Name.Column,
			Message: "Channel element must be complete and shallow-copyable; got " + element.Name,
		}
	}
	return compilerTypes.NewTypeUse(channel), nil
}

// resolveAtomicTypeUse resolves Atomic<T>.
func resolveAtomicTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Name.Line, Column: expression.Name.Column,
			Message: "Atomic requires exactly one element type",
		}
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	atomic := typeEnvironment.AtomicType(elementUse.Type)
	if atomic == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Name.Line, Column: expression.Name.Column,
			Message: "Atomic element type is not supported; use Bool, Int32, UInt32, Int64, UInt64, or Size",
		}
	}
	return compilerTypes.NewTypeUse(atomic), nil
}

// checkSpawnExpression resolves `spawn fn(arguments)`: a direct call to a
// named function whose execution becomes a new Task<R>.
func checkSpawnExpression(expression parser.SpawnExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if environment.cleanupDepth > 0 {
		return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Keyword.Line, Column: expression.Keyword.Column,
			Message: "spawn is not permitted inside defer or errdefer",
		}}
	}
	call, ok := expression.Operand.(parser.CallExpression)
	if !ok {
		return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Keyword.Line, Column: expression.Keyword.Column,
			Message: "spawn requires a direct call to a named function",
		}}
	}
	if _, isProperty := call.Callee.(parser.PropertyExpression); isProperty {
		return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Keyword.Line, Column: expression.Keyword.Column,
			Message: "spawn requires a direct call to a named function",
		}}
	}
	checked := checkCallValue(call, environment, typeEnvironment)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return checkedExpression{token: expression.Keyword, diagnostics: diagnostics}
	}
	if checked.source.Node.Kind != CallExpression || checked.source.Node.Operand == nil || checked.source.Node.Operand.Kind != FunctionReferenceExpression {
		return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Keyword.Line, Column: expression.Keyword.Column,
			Message: "spawn requires a direct call to a named function",
		}}
	}
	for _, argument := range checked.source.Node.Arguments {
		if !compilerTypes.IsCompleteValue(argument.Type) || compilerTypes.IsUnknown(argument.Type) || compilerTypes.ContainsAtomic(argument.Type) {
			return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: expression.Keyword.Line, Column: expression.Keyword.Column,
				Message: "task entry arguments must be complete and shallow-copyable",
			}}
		}
	}
	resultType := checked.typ
	if resultType == (compilerTypes.Type{}) {
		resultType = compilerTypes.Nil
	}
	if !compilerTypes.IsCompleteValue(resultType) || compilerTypes.ContainsAtomic(resultType) {
		return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Keyword.Line, Column: expression.Keyword.Column,
			Message: "Task result type must be complete and shallow-copyable",
		}}
	}
	task := typeEnvironment.TaskType(resultType)
	if task == (compilerTypes.Type{}) {
		return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Keyword.Line, Column: expression.Keyword.Column,
			Message: "Task result type must be complete and shallow-copyable",
		}}
	}
	spawnError := typeEnvironment.UnionType([]compilerTypes.Type{task, compilerTypes.ErrorType})
	node := Expression{Kind: SpawnExpression, Operand: &checked.source.Node, OperandType: task, ResultType: spawnError, Element: resultType, SourceLine: expression.Keyword.Line, SourceColumn: expression.Keyword.Column}
	source := Operand{Kind: ExpressionOperand, Type: spawnError, Name: "spawn", Node: node}
	return checkedExpression{source: source, typ: spawnError, token: expression.Keyword}
}

// checkTaskTypeCall resolves Task.yield() (a type-qualified intrinsic).
func checkTaskTypeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "yield" || len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Line, Column: callee.Column,
			Message: "Task has no such operation; use Task.yield()",
		}}
	}
	if !names.inFunction() {
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Line, Column: callee.Column,
			Message: "Task.yield() is valid only inside a function",
		}}
	}
	node := Expression{Kind: TaskYieldExpression, ResultType: compilerTypes.Type{}}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.Type{}, token: property}
}

// checkTaskMethodCall resolves the Task handle methods: join and detach.
func checkTaskMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	taskType := receiver.typ
	resultType := taskType.Task.Result
	switch name {
	case "join":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "join expects no arguments",
			}}
		}
		node := Expression{Kind: TaskMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: taskType, ResultType: resultType, Element: resultType}
		source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
		return checkedExpression{source: source, typ: resultType, token: callee.Property}
	case "detach":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "detach expects no arguments",
			}}
		}
		node := Expression{Kind: TaskMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: taskType, ResultType: compilerTypes.Type{}, Element: resultType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Property.Line, Column: callee.Property.Column,
			Message: "Task has no method " + name + "; use join or detach",
		}}
	}
}

// checkChannelTypeCall resolves Channel<T>.new(heap, capacity).
func checkChannelTypeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	channelUse, diagnostic := resolveChannelTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Channel", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if property.Lexeme != "new" || len(call.Arguments) != 2 || len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Line, Column: callee.Column,
			Message: "Channel has no such operation; use Channel<T>.new(heap, capacity)",
		}}
	}
	heap := checkValue(call.Arguments[0], names, typeEnvironment)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !compilerTypes.IsHeap(heap.typ) {
		return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: heap.token.Line, Column: heap.token.Column,
			Message: "Channel.new requires a Heap allocator; got " + heap.typ.Name,
		}}
	}
	capacity := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[1]), names, typeEnvironment)
	if diagnostics := initializerDiagnostics(capacity); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
	}
	if !assignable(compilerTypes.SizeType, capacity.typ) {
		return checkedExpression{token: capacity.token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: capacity.token.Line, Column: capacity.token.Column,
			Message: "Channel capacity must be a Size",
		}}
	}
	if capacity.source.Kind == ConstantOperand && capacity.source.Constant != nil {
		if value, exact := constant.Uint64Val(capacity.source.Constant); exact && value == 0 {
			return checkedExpression{token: capacity.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: capacity.token.Line, Column: capacity.token.Column,
				Message: "compile-time Channel capacity must be positive",
			}}
		}
	}
	result := typeEnvironment.UnionType([]compilerTypes.Type{channelUse.Type, compilerTypes.ErrorType})
	node := Expression{Kind: ChannelConstructorExpression, Operand: &capacity.source.Node, Arguments: []Operand{heap.source, capacity.source}, OperandType: channelUse.Type, ResultType: result, Element: channelUse.Type.Channel.Element, SourceLine: callee.Line, SourceColumn: callee.Column}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: "new", Node: node}
	return checkedExpression{source: source, typ: result, token: property}
}

// checkChannelMethodCall resolves Channel handle methods.
func checkChannelMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	channelType := receiver.typ
	element := channelType.Channel.Element
	switch name {
	case "send":
		if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "send expects 1 argument",
			}}
		}
		value := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.Arguments[0]), names, typeEnvironment)
		if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if !assignable(element, value.typ) {
			return checkedExpression{token: value.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: value.token.Line, Column: value.token.Column,
				Message: fmt.Sprintf("Channel send requires %s; got %s", element.Name, value.typ.Name),
			}}
		}
		result := typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{value.source}, OperandType: channelType, ResultType: result, Element: element, SourceLine: callee.Property.Line, SourceColumn: callee.Property.Column}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: callee.Property}
	case "receive":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "receive expects no arguments",
			}}
		}
		result := typeEnvironment.UnionType([]compilerTypes.Type{element, compilerTypes.EoS})
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: channelType, ResultType: result, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: callee.Property}
	case "close":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "close expects no arguments",
			}}
		}
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: channelType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	case "length", "capacity":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: name + " expects no arguments",
			}}
		}
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: channelType, ResultType: compilerTypes.SizeType, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: callee.Property}
	case "is_closed":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "is_closed expects no arguments",
			}}
		}
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: channelType, ResultType: compilerTypes.Bool, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Bool, token: callee.Property}
	case "free":
		if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "free expects 1 argument (allocator)",
			}}
		}
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: heap.token.Line, Column: heap.token.Column,
				Message: "free requires a Heap; got " + heap.typ.Name,
			}}
		}
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{heap.source}, OperandType: channelType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Property.Line, Column: callee.Property.Column,
			Message: "Channel has no method " + name + "; use send, receive, close, length, capacity, is_closed, or free",
		}}
	}
}

// checkMutexTypeCall resolves Mutex.new(heap).
func checkMutexTypeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "new" || len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Line, Column: callee.Column,
			Message: "Mutex has no such operation; use Mutex.new(heap)",
		}}
	}
	heap := checkValue(call.Arguments[0], names, typeEnvironment)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: heap.token.Line, Column: heap.token.Column,
			Message: "Mutex.new requires a Heap allocator; got " + heap.typ.Name,
		}}
	}
	result := typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.MutexType, compilerTypes.ErrorType})
	node := Expression{Kind: MutexConstructorExpression, Arguments: []Operand{heap.source}, OperandType: compilerTypes.MutexType, ResultType: result, SourceLine: callee.Line, SourceColumn: callee.Column}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: "new", Node: node}
	return checkedExpression{source: source, typ: result, token: property}
}

// checkMutexMethodCall resolves Mutex handle methods.
func checkMutexMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "lock", "unlock":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: name + " expects no arguments",
			}}
		}
		node := Expression{Kind: MutexMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: compilerTypes.MutexType, ResultType: compilerTypes.Type{}}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	case "free":
		if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "free expects 1 argument (allocator)",
			}}
		}
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: heap.token.Line, Column: heap.token.Column,
				Message: "free requires a Heap; got " + heap.typ.Name,
			}}
		}
		node := Expression{Kind: MutexMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{heap.source}, OperandType: compilerTypes.MutexType, ResultType: compilerTypes.Type{}}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Property.Line, Column: callee.Property.Column,
			Message: "Mutex has no method " + name + "; use lock, unlock, or free",
		}}
	}
}

// checkAtomicTypeCall resolves Atomic<T>.new(initial).
func checkAtomicTypeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	atomicUse, diagnostic := resolveAtomicTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Atomic", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if property.Lexeme != "new" || len(call.Arguments) != 1 || len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Line, Column: callee.Column,
			Message: "Atomic has no such operation; use Atomic<T>.new(initial)",
		}}
	}
	element := atomicUse.Type.Atomic.Element
	initial := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.Arguments[0]), names, typeEnvironment)
	if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !assignable(element, initial.typ) {
		return checkedExpression{token: initial.token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: initial.token.Line, Column: initial.token.Column,
			Message: fmt.Sprintf("Atomic.new requires %s; got %s", element.Name, initial.typ.Name),
		}}
	}
	node := Expression{Kind: AtomicConstructorExpression, Arguments: []Operand{initial.source}, OperandType: atomicUse.Type, ResultType: atomicUse.Type, Element: element}
	source := Operand{Kind: ExpressionOperand, Type: atomicUse.Type, Name: "new", Node: node}
	return checkedExpression{source: source, typ: atomicUse.Type, token: property}
}

// checkAtomicMethodCall resolves Atomic handle methods.
func checkAtomicMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	atomicType := receiver.typ
	element := atomicType.Atomic.Element
	argumentCount := 1
	if name == "load" {
		argumentCount = 0
	}
	if name == "compare_exchange" {
		argumentCount = 2
	}
	if len(call.Arguments) != argumentCount || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Property.Line, Column: callee.Property.Column,
			Message: name + " expects " + fmt.Sprint(argumentCount) + " argument(s)",
		}}
	}
	if name == "fetch_add" || name == "fetch_sub" {
		if compilerTypes.Equal(element, compilerTypes.Bool) {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: name + " is unavailable for Bool",
			}}
		}
	}
	resultType := element
	if name == "compare_exchange" {
		resultType = compilerTypes.Bool
	}
	if name == "store" {
		resultType = compilerTypes.Type{}
	}
	var arguments []Operand
	for _, argument := range call.Arguments {
		value := checkInitializer(argument, compilerTypes.NewTypeUse(element), tokenOf(argument), names, typeEnvironment)
		if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(argument), diagnostics: diagnostics}
		}
		if !assignable(element, value.typ) {
			return checkedExpression{token: value.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: value.token.Line, Column: value.token.Column,
				Message: fmt.Sprintf("%s requires %s; got %s", name, element.Name, value.typ.Name),
			}}
		}
		arguments = append(arguments, value.source)
	}
	node := Expression{Kind: AtomicMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: arguments, OperandType: atomicType, ResultType: resultType, Element: element}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
	return checkedExpression{source: source, typ: resultType, token: callee.Property}
}
