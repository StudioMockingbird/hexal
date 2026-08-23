package checker

import (
	"fmt"
	"go/constant"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// Ordinary Error values, `T | Error` results, `try` propagation, and
// error-only deferred cleanup.

func freeLocalStorageDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, "free does not accept a pointer into this function's local storage")
}

func doubleFreeDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, "free releases storage already released on every path to this point")
}

func useAfterFreeDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, "this pointer's storage was released on every path to this point")
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

// checkErrorNewCall resolves the built-in `Error.new(header, message)`
// construction. The compiler supplies file, line, and column from the Error
// token; only header and message are source arguments.
func checkErrorNewCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "new" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "Error must be created with Error.new(header, message)"))}
	}
	if len(call.Arguments) != 2 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("Error.new expects 2 arguments (header, message); got %d", len(call.Arguments))))}
	}
	header := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.StrandType), tokenOf(call.Arguments[0]), names, typeEnvironment)
	if diagnostics := initializerDiagnostics(header); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !compilerTypes.IsStrand(header.typ) {
		return checkedExpression{token: header.token, diagnostic: diagnosticAt(typeErrorAt(header.token, "Error.new expects header: Strand and message: String; got "+header.typ.Name))}
	}
	message := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.StringType), tokenOf(call.Arguments[1]), names, typeEnvironment)
	if diagnostics := initializerDiagnostics(message); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
	}
	if !compilerTypes.IsString(message.typ) {
		return checkedExpression{token: message.token, diagnostic: diagnosticAt(typeErrorAt(message.token, "Error.new expects header: Strand and message: String; got "+message.typ.Name))}
	}

	object := compilerTypes.ErrorType.Object
	member := func(name string) *compilerTypes.ObjectMember {
		found, _ := object.Member(name)
		return found
	}
	fileNode := Expression{Kind: StringLiteralExpression, Name: names.logicalKey, ResultType: compilerTypes.StringType}
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
			{Member: member("header"), Source: header.source},
			{Member: member("message"), Source: message.source},
		},
	}
	source := Operand{Kind: ObjectOperand, Type: compilerTypes.ErrorType, Name: "new", Object: &value}
	return checkedExpression{source: source, typ: compilerTypes.ErrorType, token: property}
}

// checkTryExpression resolves the `try` form: the operand must be a
// union containing Error and at least one success member, the enclosing
// function's result must accept Error, and the try yields the normalized
// success value or union.
func checkTryExpression(expression parser.TryExpression, context expressionContext, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if context.inCleanup || names.cleanupDepth > 0 {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "try is not permitted inside defer or errdefer"))}
	}
	if !names.inFunction() || names.result == nil || !resultAcceptsError(*names.result) {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "try requires an enclosing function whose result accepts Error"))}
	}
	operand := checkExpression(expression.Operand, expressionContext{foldConstants: true}, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(operand); len(diagnostics) > 0 {
		return checkedExpression{token: expression.Keyword, diagnostics: diagnostics}
	}
	operandMembers := compilerTypes.UnionMembers(operand.typ)
	if !compilerTypes.IsUnion(operand.typ) || operandMembers.Len() < 2 {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "try requires a union containing Error and a success member; got "+operand.typ.Name))}
	}
	memberIndex := -1
	for index := 0; index < operandMembers.Len(); index++ {
		if member, _ := operandMembers.At(index); compilerTypes.IsError(member) {
			memberIndex = index
			break
		}
	}
	if memberIndex < 0 {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "try requires a union containing Error and a success member; got "+operand.typ.Name))}
	}
	success, ok := compilerTypes.RemoveUnionMember(typeEnvironment, operand.typ, compilerTypes.ErrorType)
	if !ok {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "try requires a union containing Error and a success member; got "+operand.typ.Name))}
	}
	node := Expression{
		Kind:        TryExpression,
		Operand:     &operand.source.Node,
		OperandType: operand.typ,
		ResultType:  success,
		Element:     *names.result,
		MemberIndex: memberIndex,
	}
	source := Operand{Kind: ExpressionOperand, Type: success, Name: "try", Node: node}
	return checkedExpression{source: source, typ: success, token: expression.Keyword}
}

// checkErrdeferStatement registers an error-only cleanup action.
// Registration and capture follow `defer` exactly; the action runs only when
// the current function exits by returning Error.
func checkErrdeferStatement(statement parser.ErrdeferStatement, names *scope, typeEnvironment *compilerTypes.Environment) (ErrdeferStatement, compilerTypes.Diagnostics) {
	if !names.inFunction() || names.result == nil || !resultAcceptsError(*names.result) {
		return ErrdeferStatement{}, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "errdefer requires an enclosing function whose result accepts Error")}
	}
	names.cleanupDepth++
	defer func() { names.cleanupDepth-- }()
	action := DeferredAction{Err: true, SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}
	var source Operand
	if call, isCall := statement.Expression.(parser.CallExpression); isCall {
		checked := checkCall(call, names, typeEnvironment)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return ErrdeferStatement{}, diagnostics
		}
		action.IsCall = true
		action.Call = &checked.source
		captureDeferredHeapFree(&action, names)
		source = checked.source
	} else {
		checked := checkExitTimeExpression(statement.Expression, names, typeEnvironment)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return ErrdeferStatement{}, diagnostics
		}
		action.Value = &checked.source
		source = checked.source
	}
	names.defers = append(names.defers, action)
	return ErrdeferStatement{
		Expression:   source,
		Action:       action,
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
	}, nil
}

// ErrdeferStatement is the checked registration of one error-only cleanup
// action.
type ErrdeferStatement struct {
	Expression   Operand
	Action       DeferredAction
	SourceLine   int
	SourceColumn int
}

func (ErrdeferStatement) statementNode() {}
