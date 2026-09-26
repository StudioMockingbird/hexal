package checker

import (
	"go/constant"

	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// Ordinary Error values, `T | Error` results, `try` propagation, and
// error-only deferred cleanup.

func freeLocalStorageDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diag.FreeLocalStoragePointer())
}

func freeStashAllocatedDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diag.FreeStashAllocatedPointer())
}

func freePoolAllocatedDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diag.FreePoolAllocatedPointer())
}

func poolFreeHeapAllocatedDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diag.PoolFreeHeapAllocatedPointer())
}

func poolFreeStashAllocatedDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diag.PoolFreeStashAllocatedPointer())
}

func doubleFreeDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diag.DoubleFree())
}

func useAfterFreeDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diag.UseAfterFree())
}

// resultAcceptsError reports whether a function result type can carry an
// Error value: exactly Error, or a union containing an Error member.
func resultAcceptsError(result compilerTypes.Type) bool {
	if compilerTypes.IsError(result) {
		return true
	}
	if !compilerTypes.IsUnion(result) {
		return false
	}
	members := compilerTypes.UnionMembers(result)
	for index := 0; index < members.Len(); index++ {
		if member, _ := members.At(index); compilerTypes.IsError(member) {
			return true
		}
	}
	return false
}

// checkErrorNewCall resolves the built-in `Error(kind, message)`
// construction. The compiler supplies file, line, and column from the Error
// token; only kind and message are source arguments.
func checkErrorNewCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.ErrorConstructorTypeArguments()))}
	}
	if len(call.Arguments) != 2 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.ErrorConstructorArity(len(call.Arguments))))}
	}
	kind := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.ErrorKindType), tokenOf(call.Arguments[0]), ctx)
	if diagnostics := initializerDiagnostics(kind); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !compilerTypes.IsErrorKind(kind.typ) {
		return checkedExpression{token: kind.token, diagnostic: diagnosticAt(messageAt(kind.token, diag.ErrorConstructorKindType()))}
	}
	// The message is the one text argument that coerces into a bounded
	// capacity implicitly: a literal is measured at compile time, any other
	// text when it is copied.
	message := checkBoundedText(call.Arguments[1], compilerTypes.ErrorMessageText, "Error message", ctx)
	if diagnostics := initializerDiagnostics(message); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
	}

	object := compilerTypes.ErrorType.Object
	member := func(name string) *compilerTypes.ObjectMember {
		found, _ := object.Member(name)
		return found
	}
	fileNode := Expression{Kind: StringLiteralExpression, Name: ctx.names.logicalKey, ResultType: compilerTypes.StringType}
	fileOperand := Operand{Kind: ExpressionOperand, Type: compilerTypes.StringType, Node: fileNode}
	lineOperand := constantOperand(compilerTypes.SizeType, constant.MakeUint64(uint64(callee.Line)), "")
	lineOperand.Node = constantNode(lineOperand)
	columnOperand := constantOperand(compilerTypes.SizeType, constant.MakeUint64(uint64(callee.Column)), "")
	columnOperand.Node = constantNode(columnOperand)

	value := ObjectValue{
		Type: compilerTypes.ErrorType,
		Initializers: []ObjectMemberValue{
			{Member: member("file"), Source: fileOperand},
			{Member: member("line"), Source: lineOperand},
			{Member: member("column"), Source: columnOperand},
			{Member: member("kind"), Source: kind.source},
			{Member: member("message"), Source: message.source},
		},
	}
	source := Operand{Kind: ObjectOperand, Type: compilerTypes.ErrorType, Name: "new", Object: &value}
	return checkedExpression{source: source, typ: compilerTypes.ErrorType, token: callee}
}

// checkErrorMethodCall resolves Error.header(), the allocation-free derived
// display header. It is the only Error method: every other field is read as
// an ordinary object member.
func checkErrorMethodCall(call methodCall) checkedExpression {
	if call.callee.Property.Lexeme != "header" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.ErrorMethodNotFound(call.callee.Property.Lexeme)))}
	}
	if len(call.call.TypeArguments) != 0 || len(call.call.Arguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.ErrorHeaderMethodArguments()))}
	}
	call.receiver = valueFromPlace(call.receiver)
	node := Expression{
		Kind:        ErrorHeaderExpression,
		Operand:     &call.receiver.source.Node,
		OperandType: compilerTypes.ErrorType,
		ResultType:  compilerTypes.ErrorHeaderText,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.ErrorHeaderText, Name: "header", Node: node}
	return checkedExpression{source: source, typ: compilerTypes.ErrorHeaderText, token: call.callee.Property}
}

// checkErrorKindMethodCall resolves ErrorKind.header(), the same allocation-
// free derived display header method on the classification value itself.
func checkErrorKindMethodCall(call methodCall) checkedExpression {
	if call.callee.Property.Lexeme != "header" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.ErrorKindMethodNotFound(call.callee.Property.Lexeme)))}
	}
	if len(call.call.TypeArguments) != 0 || len(call.call.Arguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.ErrorHeaderMethodArguments()))}
	}
	call.receiver = valueFromPlace(call.receiver)
	node := Expression{
		Kind:        ErrorKindHeaderExpression,
		Operand:     &call.receiver.source.Node,
		OperandType: compilerTypes.ErrorKindType,
		ResultType:  compilerTypes.ErrorHeaderText,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.ErrorHeaderText, Name: "header", Node: node}
	return checkedExpression{source: source, typ: compilerTypes.ErrorHeaderText, token: call.callee.Property}
}

// checkTryExpression resolves the `try` form: the operand must be a
// union containing Error and at least one success member, the enclosing
// function's result must accept Error, and the try yields the normalized
// success value or union.
func checkTryExpression(expression parser.TryExpression, context expressionContext, ctx checkContext) checkedExpression {
	if context.inCleanup || ctx.names.cleanupDepth > 0 {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diag.TryInsideCleanup()))}
	}
	if !ctx.names.inFunction() || ctx.names.result == nil || !resultAcceptsError(*ctx.names.result) {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diag.TryRequiresErrorResult()))}
	}
	operand := checkExpression(expression.Operand, expressionContext{foldConstants: true}, ctx)
	if diagnostics := initializerDiagnostics(operand); len(diagnostics) > 0 {
		return checkedExpression{token: expression.Keyword, diagnostics: diagnostics}
	}
	operandMembers := compilerTypes.UnionMembers(operand.typ)
	if !compilerTypes.IsUnion(operand.typ) || operandMembers.Len() < 2 {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diag.TryOperandMustContainErrorAndSuccess(operand.typ.Name)))}
	}
	memberIndex := -1
	for index := 0; index < operandMembers.Len(); index++ {
		if member, _ := operandMembers.At(index); compilerTypes.IsError(member) {
			memberIndex = index
			break
		}
	}
	if memberIndex < 0 {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diag.TryOperandMustContainErrorAndSuccess(operand.typ.Name)))}
	}
	success, ok := compilerTypes.RemoveUnionMember(ctx.typeEnvironment, operand.typ, compilerTypes.ErrorType)
	if !ok {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(messageAt(expression.Keyword, diag.TryOperandMustContainErrorAndSuccess(operand.typ.Name)))}
	}
	node := Expression{
		Kind:               TryExpression,
		Operand:            &operand.source.Node,
		OperandType:        operand.typ,
		OperandStorageType: operand.storageType,
		ResultType:         success,
		Element:            *ctx.names.result,
		MemberIndex:        memberIndex,
	}
	source := Operand{Kind: ExpressionOperand, Type: success, Name: "try", Node: node}
	return checkedExpression{source: source, typ: success, token: expression.Keyword}
}

// checkErrdeferStatement registers an error-only cleanup action.
// Registration and capture follow `defer` exactly; the action runs only when
// the current function exits by returning Error.
func checkErrdeferStatement(statement parser.ErrdeferStatement, ctx checkContext) (ErrdeferStatement, compilerTypes.Diagnostics) {
	if !ctx.names.inFunction() || ctx.names.result == nil || !resultAcceptsError(*ctx.names.result) {
		return ErrdeferStatement{}, compilerTypes.Diagnostics{messageAt(statement.Keyword, diag.ErrdeferRequiresErrorResult())}
	}
	ctx.names.cleanupDepth++
	defer func() { ctx.names.cleanupDepth-- }()
	action := DeferredAction{Err: true, Span: statement.Keyword.Span}
	var source Operand
	if call, isCall := statement.Expression.(parser.CallExpression); isCall {
		checked := checkCall(call, compilerTypes.Type{}, ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return ErrdeferStatement{}, diagnostics
		}
		action.IsCall = true
		action.Call = &checked.source
		captureDeferredHeapFree(&action, ctx.names)
		source = checked.source
	} else {
		checked := checkExitTimeExpression(statement.Expression, ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return ErrdeferStatement{}, diagnostics
		}
		action.Value = &checked.source
		source = checked.source
	}
	ctx.names.defers = append(ctx.names.defers, action)
	return ErrdeferStatement{
		Expression: source,
		Action:     action,
		Span:       statement.Keyword.Span,
	}, nil
}

// ErrdeferStatement is the checked registration of one error-only cleanup
// action.
type ErrdeferStatement struct {
	Expression Operand
	Action     DeferredAction
	Span       span.Span
}

func (ErrdeferStatement) statementNode() {}
