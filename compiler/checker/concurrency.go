package checker

import (
	"go/constant"

	"hexal/compiler/corelib"
	diagnosticsPkg "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// Concurrency surface: Task<T>, spawn, join/detach/yield, Channel<T>, Mutex,
// and Atomic<T>.

// resolveTaskTypeUse resolves Task<R>.
func resolveTaskTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.TaskRequiresOneResultType()))
	}
	resultUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	task := typeEnvironment.TaskType(resultUse.Type)
	if task == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.TaskResultNotCopyable(resultUse.Type.Name)))
	}
	return compilerTypes.NewTypeUse(task), nil
}

// resolveChannelTypeUse resolves Channel<T>.
func resolveChannelTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.ChannelRequiresOneElementType()))
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	element := elementUse.Type
	if compilerTypes.IsEoS(element) || compilerTypes.UnionContainsEoS(element) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.ChannelCannotCarryEOS()))
	}
	if compilerTypes.ContainsAtomic(element) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.ChannelContainsNonCopyableAtomic()))
	}
	channel := typeEnvironment.ChannelType(element)
	if channel == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.ChannelElementNotCopyable(element.Name)))
	}
	return compilerTypes.NewTypeUse(channel), nil
}

// resolveAtomicTypeUse resolves Atomic<T>.
func resolveAtomicTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.AtomicRequiresOneElementType()))
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	atomic := typeEnvironment.AtomicType(elementUse.Type)
	if atomic == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.AtomicElementTypeUnsupported()))
	}
	return compilerTypes.NewTypeUse(atomic), nil
}

// checkSpawnExpression resolves `spawn fn(arguments)`: a direct call to a
// named function whose execution becomes a new Task<R>.
func checkSpawnExpression(expression parser.SpawnExpression, ctx checkContext) checkedExpression {
	if ctx.names.cleanupDepth > 0 {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.SpawnForbiddenDuringCleanup()))}
	}
	call, ok := expression.Operand.(parser.CallExpression)
	if !ok {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.SpawnRequiresNamedFunctionCall()))}
	}
	if _, isProperty := call.Callee.(parser.PropertyExpression); isProperty {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.SpawnRequiresNamedFunctionCall()))}
	}
	checked := checkCallValue(call, compilerTypes.Type{}, ctx)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		if checked.typ == (compilerTypes.Type{}) {
			// A no-result callee cannot form a Task<R>, and Hexal does not
			// manufacture a hidden unit value for it.
			return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.SpawnFunctionRequiresResult()))}
		}
		return checkedExpression{token: expression.Keyword, diagnostics: diagnostics}
	}
	if checked.source.Node.Kind != CallExpression || checked.source.Node.Operand == nil || checked.source.Node.Operand.Kind != FunctionReferenceExpression {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.SpawnRequiresNamedFunctionCall()))}
	}
	if ctx.names.envDependent[checked.source.Node.Operand.Name] {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.SpawnEntryEnvironmentFunction(checked.source.Node.Operand.Name)))}
	}
	// Spawn arguments are copied into the task frame and then into the entry
	// function, so each argument must be eligible in both positions.
	for _, argument := range checked.source.Node.Arguments {
		if !compilerTypes.Eligible(argument.Type, compilerTypes.PositionTaskArgument) ||
			!compilerTypes.Eligible(argument.Type, compilerTypes.PositionFunctionParam) {
			return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.TaskEntryArgumentsNotCopyable()))}
		}
	}
	resultType := checked.typ
	if resultType == (compilerTypes.Type{}) {
		resultType = compilerTypes.Nil
	}
	// The spawned task returns its function's result and stores it in the
	// task frame, so it must be eligible in both positions.
	if !compilerTypes.Eligible(resultType, compilerTypes.PositionFunctionResult) ||
		!compilerTypes.Eligible(resultType, compilerTypes.PositionTaskResult) {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.TaskResultMustBeCopyable()))}
	}
	task := ctx.typeEnvironment.TaskType(resultType)
	if task == (compilerTypes.Type{}) {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diagnosticsPkg.TaskResultMustBeCopyable()))}
	}
	spawnError := ctx.typeEnvironment.UnionType([]compilerTypes.Type{task, compilerTypes.ErrorType})
	node := Expression{Kind: SpawnExpression, Operand: &checked.source.Node, OperandType: task, ResultType: spawnError, Element: resultType, Span: expression.Keyword.Span}
	source := Operand{Kind: ExpressionOperand, Type: spawnError, Name: "spawn", Node: node}
	return checkedExpression{source: source, typ: spawnError, token: expression.Keyword}
}

// checkTaskTypeCall resolves Task.yield() (a type-qualified intrinsic).
func checkTaskTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme == "sleep" {
		if hint, moved := corelib.OperationHint("Task", "sleep"); moved {
			diagnostic := coreOperationMigrationDiagnostic(callee, hint)
			return checkedExpression{token: callee, diagnostic: &diagnostic}
		}
	}
	if property.Lexeme != "yield" || len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diagnosticsPkg.UnknownTaskOperation()))}
	}
	if !ctx.names.inFunction() {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diagnosticsPkg.TaskYieldOutsideFunction()))}
	}
	node := Expression{Kind: TaskYieldExpression, ResultType: compilerTypes.Type{}}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.Type{}, token: property}
}

// checkTaskMethodCall resolves the Task handle methods: join and detach.
func checkTaskMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	taskType := call.receiver.typ
	resultType := taskType.Task.Result
	if !hasBuiltinMethod(taskType, name) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownTaskMethod(name)))}
	}
	switch name {
	case "join":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected("join")))}
		}
		node := Expression{Kind: TaskMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: taskType, ResultType: resultType, Element: resultType}
		source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
		return checkedExpression{source: source, typ: resultType, token: call.callee.Property}
	case "detach":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected("detach")))}
		}
		node := Expression{Kind: TaskMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: taskType, ResultType: compilerTypes.Type{}, Element: resultType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	default:
		return unexpectedBuiltinMethod(taskType, call.callee.Property)
	}
}

// checkChannelTypeCall resolves Channel<T>(heap, capacity).
func checkChannelTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	channelUse, diagnostic := resolveChannelTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Channel", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if len(call.Arguments) != 2 || len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diagnosticsPkg.ChannelConstructorUsage()))}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !compilerTypes.IsHeap(heap.typ) {
		return checkedExpression{token: heap.token, diagnostic: diagnosticAt(messageAt(heap.token, diagnosticsPkg.ChannelRequiresHeap(heap.typ.Name)))}
	}
	capacity := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[1]), ctx)
	if diagnostics := initializerDiagnostics(capacity); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
	}
	if !assignable(compilerTypes.SizeType, capacity.typ) {
		return checkedExpression{token: capacity.token, diagnostic: diagnosticAt(messageAt(capacity.token, diagnosticsPkg.ChannelCapacityMustBeSize()))}
	}
	if capacity.known != nil && capacity.known.Constant != nil {
		if value, exact := constant.Uint64Val(capacity.known.Constant); exact && value == 0 {
			return checkedExpression{token: capacity.token, diagnostic: diagnosticAt(messageAt(capacity.token, diagnosticsPkg.ChannelCapacityMustBePositive()))}
		}
	}
	result := ctx.typeEnvironment.UnionType([]compilerTypes.Type{channelUse.Type, compilerTypes.ErrorType})
	node := Expression{Kind: ChannelConstructorExpression, Operand: &capacity.source.Node, Arguments: []Operand{heap.source, capacity.source}, OperandType: channelUse.Type, ResultType: result, Element: channelUse.Type.Channel.Element, Span: callee.Span}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: "new", Node: node}
	return checkedExpression{source: source, typ: result, token: callee}
}

// checkChannelMethodCall resolves Channel handle methods.
func checkChannelMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	channelType := call.receiver.typ
	element := channelType.Channel.Element
	if !hasBuiltinMethod(channelType, name) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownChannelMethod(name)))}
	}
	switch name {
	case "send":
		if len(call.call.Arguments) != 1 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AllocationRequiresOneArgument("send")))}
		}
		value := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.call.Arguments[0]), call.ctx)
		if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.call.Arguments[0]), diagnostics: diagnostics}
		}
		if !assignable(element, value.typ) {
			return checkedExpression{token: value.token, diagnostic: diagnosticAt(messageAt(value.token, diagnosticsPkg.ChannelSendRequires(element.Name, value.typ.Name)))}
		}
		result := call.ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{value.source}, OperandType: channelType, ResultType: result, Element: element, Span: call.callee.Property.Span}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: call.callee.Property}
	case "receive":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected("receive")))}
		}
		result := call.ctx.typeEnvironment.UnionType([]compilerTypes.Type{element, compilerTypes.EoS})
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: channelType, ResultType: result, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: call.callee.Property}
	case "close":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected("close")))}
		}
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: channelType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	case "length", "capacity":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected(name)))}
		}
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: channelType, ResultType: compilerTypes.SizeType, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: call.callee.Property}
	case "is_closed":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected("is_closed")))}
		}
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: channelType, ResultType: compilerTypes.Bool, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Bool, token: call.callee.Property}
	case "free":
		if len(call.call.Arguments) != 1 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.FreeRequiresAllocatorArgument()))}
		}
		heap := checkValue(call.call.Arguments[0], call.ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			return checkedExpression{token: heap.token, diagnostic: diagnosticAt(messageAt(heap.token, diagnosticsPkg.FreeRequiresHeap(heap.typ.Name)))}
		}
		node := Expression{Kind: ChannelMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{heap.source}, OperandType: channelType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	default:
		return unexpectedBuiltinMethod(channelType, call.callee.Property)
	}
}

// checkMutexTypeCall resolves Mutex(heap).
func checkMutexTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diagnosticsPkg.MutexConstructorUsage()))}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		return checkedExpression{token: heap.token, diagnostic: diagnosticAt(messageAt(heap.token, diagnosticsPkg.MutexRequiresHeap(heap.typ.Name)))}
	}
	result := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.MutexType, compilerTypes.ErrorType})
	node := Expression{Kind: MutexConstructorExpression, Arguments: []Operand{heap.source}, OperandType: compilerTypes.MutexType, ResultType: result, Span: callee.Span}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: "new", Node: node}
	return checkedExpression{source: source, typ: result, token: callee}
}

// checkMutexMethodCall resolves Mutex handle methods.
func checkMutexMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	mutexType := call.receiver.typ
	if !hasBuiltinMethod(mutexType, name) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownMutexMethod(name)))}
	}
	switch name {
	case "lock", "unlock":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.NoArgumentsExpected(name)))}
		}
		node := Expression{Kind: MutexMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: compilerTypes.MutexType, ResultType: compilerTypes.Type{}}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	case "free":
		if len(call.call.Arguments) != 1 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.FreeRequiresAllocatorArgument()))}
		}
		heap := checkValue(call.call.Arguments[0], call.ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			return checkedExpression{token: heap.token, diagnostic: diagnosticAt(messageAt(heap.token, diagnosticsPkg.FreeRequiresHeap(heap.typ.Name)))}
		}
		node := Expression{Kind: MutexMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{heap.source}, OperandType: compilerTypes.MutexType, ResultType: compilerTypes.Type{}}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	default:
		return unexpectedBuiltinMethod(mutexType, call.callee.Property)
	}
}

// checkAtomicTypeCall resolves Atomic<T>(initial).
func checkAtomicTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	atomicUse, diagnostic := resolveAtomicTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Atomic", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if len(call.Arguments) != 1 || len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diagnosticsPkg.AtomicConstructorUsage()))}
	}
	element := atomicUse.Type.Atomic.Element
	initial := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.Arguments[0]), ctx)
	if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !assignable(element, initial.typ) {
		return checkedExpression{token: initial.token, diagnostic: diagnosticAt(messageAt(initial.token, diagnosticsPkg.AtomicInitializerType(element.Name, initial.typ.Name)))}
	}
	node := Expression{Kind: AtomicConstructorExpression, Arguments: []Operand{initial.source}, OperandType: atomicUse.Type, ResultType: atomicUse.Type, Element: element}
	source := Operand{Kind: ExpressionOperand, Type: atomicUse.Type, Name: "new", Node: node}
	return checkedExpression{source: source, typ: atomicUse.Type, token: callee}
}

// checkAtomicMethodCall resolves Atomic handle methods.
func checkAtomicMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	atomicType := call.receiver.typ
	element := atomicType.Atomic.Element
	if !hasBuiltinMethod(atomicType, name) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AtomicMethodMissing(atomicType.Name, name)))}
	}
	argumentCount := 1
	if name == "load" {
		argumentCount = 0
	}
	if name == "compare_exchange" {
		argumentCount = 2
	}
	if len(call.call.Arguments) != argumentCount || len(call.call.TypeArguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AtomicMethodArity(name, argumentCount)))}
	}
	if name == "fetch_add" || name == "fetch_sub" {
		if compilerTypes.Equal(element, compilerTypes.Bool) {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AtomicMethodUnavailableForBool(name)))}
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
	for _, argument := range call.call.Arguments {
		value := checkInitializer(argument, compilerTypes.NewTypeUse(element), tokenOf(argument), call.ctx)
		if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(argument), diagnostics: diagnostics}
		}
		if !assignable(element, value.typ) {
			return checkedExpression{token: value.token, diagnostic: diagnosticAt(messageAt(value.token, diagnosticsPkg.AtomicOperationType(name, element.Name, value.typ.Name)))}
		}
		arguments = append(arguments, value.source)
	}
	node := Expression{Kind: AtomicMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: arguments, OperandType: atomicType, ResultType: resultType, Element: element}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node}
	return checkedExpression{source: source, typ: resultType, token: call.callee.Property}
}
