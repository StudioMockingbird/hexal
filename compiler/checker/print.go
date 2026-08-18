package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// The compiler-owned `print` builtin writes the textual form of each argument
// to standard output in source order with no separators or implicit newline;
// it has no result and requires at least one argument.

// printable reports whether typ has a compiler-defined print form.
// Aggregates are printable only when every recursively visited
// member, payload field, or element is printable.
func printable(typ compilerTypes.Type) bool {
	switch {
	case compilerTypes.IsInteger(typ), compilerTypes.IsFloat(typ),
		compilerTypes.Equal(typ, compilerTypes.Bool),
		compilerTypes.IsRune(typ), compilerTypes.IsString(typ), compilerTypes.IsStrand(typ),
		compilerTypes.IsNil(typ), compilerTypes.IsError(typ):
		return true
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if !printable(member.Type) {
				return false
			}
		}
		return true
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if !printable(member.Type) {
					return false
				}
			}
		}
		return true
	case typ.Array != nil:
		return printable(typ.Array.Element)
	case typ.View != nil:
		return printable(typ.View.Element)
	case typ.List != nil:
		return printable(typ.List.Element)
	case typ.Dict != nil:
		return printable(typ.Dict.Key) && printable(typ.Dict.Value)
	}
	return false
}

// printUnsupportedPath describes the first unsupported member path of an
// aggregate argument for the print diagnostic.
func printUnsupportedPath(typ compilerTypes.Type) string {
	switch {
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if !printable(member.Type) {
				return fmt.Sprintf("because %s is %s", member.Name, member.Type.Name)
			}
		}
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if !printable(member.Type) {
					return fmt.Sprintf("because %s is %s", member.Name, member.Type.Name)
				}
			}
		}
	case typ.Array != nil:
		return "because its element is " + typ.Array.Element.Name
	case typ.View != nil:
		return "because its element is " + typ.View.Element.Name
	case typ.List != nil:
		return "because its element is " + typ.List.Element.Name
	case typ.Dict != nil:
		if !printable(typ.Dict.Key) {
			return "because its key is " + typ.Dict.Key.Name
		}
		return "because its value is " + typ.Dict.Value.Name
	}
	return ""
}

// checkPrintCall resolves the protected builtin `print(...)` call. It is
// recognized before ordinary free-function lookup, requires at least one
// argument, takes no type arguments, and produces no value.
func checkPrintCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "print does not take type arguments"))}
	}
	if len(call.Arguments) == 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "print expects at least 1 argument"))}
	}
	arguments := make([]Operand, 0, len(call.Arguments))
	for _, argument := range call.Arguments {
		// A print argument is the sole position admitting
		// standalone Nil, so arguments check under allowStandaloneNil.
		checked := checkExpression(argument, expressionContext{foldConstants: true, allowStandaloneNil: true}, names, typeEnvironment)
		if checked.token.Line == 0 {
			checked.token = tokenOf(argument)
		}
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(argument), diagnostics: diagnostics}
		}
		if !printable(checked.typ) {
			message := "print does not support " + checked.typ.Name
			if path := printUnsupportedPath(checked.typ); path != "" {
				message += " " + path
			} else if compilerTypes.IsUnion(checked.typ) {
				message += "; narrow or match it first"
			}
			return checkedExpression{token: checked.token, diagnostic: diagnosticAt(typeErrorAt(checked.token, message))}
		}
		arguments = append(arguments, checked.source)
	}
	node := Expression{Kind: PrintExpression, Arguments: arguments, ResultType: compilerTypes.Type{}}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee}
}
