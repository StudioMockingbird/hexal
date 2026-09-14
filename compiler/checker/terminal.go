package checker

// The libuv-adjacent terminal builtins: Terminal.is_attached(stream) and
// Terminal.size(stream), reusing the NetworkExpression Kind exactly like
// Address, Dns, Tcp, Process, and Signals -- see checkDnsTypeCall and
// checkTcpTypeCall in network.go for the identical dispatch shape.

import (
	"fmt"

	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkTerminalTypeCall resolves Terminal.is_attached(stream) and
// Terminal.size(stream).
func checkTerminalTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(typeErrorAt(variable.Name, "Terminal operations take no type arguments"))}
	}
	switch property.Lexeme {
	case "is_attached":
		if len(call.Arguments) != 1 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("is_attached expects 1 argument (stream: IO); got %d", len(call.Arguments))))}
		}
		stream := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.IOType), tokenOf(call.Arguments[0]), ctx)
		if diagnostics := initializerDiagnostics(stream); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(compilerTypes.IOType, stream.typ) {
			return checkedExpression{token: stream.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(compilerTypes.IOType, stream.typ, stream.token))}
		}
		resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Bool, compilerTypes.ErrorType})
		if resultUnion == (compilerTypes.Type{}) {
			return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the Bool | Error result union"))}
		}
		return networkNode("terminal_is_attached", nil, []Operand{stream.source}, compilerTypes.Type{}, resultUnion, property)
	case "size":
		if len(call.Arguments) != 1 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("size expects 1 argument (stream: IO); got %d", len(call.Arguments))))}
		}
		stream := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.IOType), tokenOf(call.Arguments[0]), ctx)
		if diagnostics := initializerDiagnostics(stream); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(compilerTypes.IOType, stream.typ) {
			return checkedExpression{token: stream.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(compilerTypes.IOType, stream.typ, stream.token))}
		}
		resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.TerminalSizeType, compilerTypes.ErrorType})
		if resultUnion == (compilerTypes.Type{}) {
			return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the TerminalSize | Error result union"))}
		}
		return networkNode("terminal_size", nil, []Operand{stream.source}, compilerTypes.Type{}, resultUnion, property)
	default:
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(typeErrorAt(variable.Name, "Terminal has no such operation; use Terminal.is_attached or Terminal.size"))}
	}
}
