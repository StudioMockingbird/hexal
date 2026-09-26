package checker

import (
	diag "hexal/compiler/diagnostics"
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
		compilerTypes.IsText(typ),
		compilerTypes.IsNil(typ), compilerTypes.IsError(typ):
		return true
	case typ.Object != nil:
		if compilerTypes.IsForeignRecord(typ) {
			return false
		}
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
	case typ.Slice != nil:
		return printable(typ.Slice.Element)
	case typ.List != nil:
		return printable(typ.List.Element)
	case typ.Dict != nil:
		return printable(typ.Dict.Key) && printable(typ.Dict.Value)
	}
	return false
}

// printUnsupportedPath describes the first unsupported member path of an
// aggregate argument for the print diagnostic.
func printUnsupportedDetails(typ compilerTypes.Type) diag.PrintUnsupportedDetails {
	switch {
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if !printable(member.Type) {
				return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedObjectMember, Member: member.Name, ValueType: member.Type.Name}
			}
		}
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if !printable(member.Type) {
					return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedObjectMember, Member: member.Name, ValueType: member.Type.Name}
				}
			}
		}
	case typ.Array != nil:
		return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedAggregateElement, ValueType: typ.Array.Element.Name}
	case typ.Slice != nil:
		return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedAggregateElement, ValueType: typ.Slice.Element.Name}
	case typ.List != nil:
		return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedAggregateElement, ValueType: typ.List.Element.Name}
	case typ.Dict != nil:
		if !printable(typ.Dict.Key) {
			return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedDictKey, ValueType: typ.Dict.Key.Name}
		}
		return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedDictValue, ValueType: typ.Dict.Value.Name}
	case compilerTypes.IsUnion(typ):
		return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedUnion}
	}
	return diag.PrintUnsupportedDetails{Kind: diag.PrintUnsupportedDirect}
}

// checkPrintCall resolves the protected builtin `print(...)` call. It is
// recognized before ordinary free-function lookup, requires at least one
// argument, takes no type arguments, and produces no value.
func checkPrintCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.PrintTypeArgumentsNotAllowed()))}
	}
	if len(call.Arguments) == 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.PrintRequiresArgument()))}
	}
	arguments := make([]Operand, 0, len(call.Arguments))
	for _, argument := range call.Arguments {
		// A print argument is the sole position admitting
		// standalone Nil, so arguments check under allowStandaloneNil.
		checked := checkExpression(argument, expressionContext{foldConstants: true, allowStandaloneNil: true}, ctx)
		if checked.token.Line == 0 {
			checked.token = tokenOf(argument)
		}
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(argument), diagnostics: diagnostics}
		}
		if checked.typ.Object != nil && compilerTypes.IsForeignRecord(checked.typ) {
			// A foreign record's layout is the C compiler's; printing it would
			// require a Hexal-owned rendering the language does not define.
			return checkedExpression{token: checked.token, diagnostic: diagnosticAt(messageAt(checked.token, diag.PrintForeignRecordNotSupported(checked.typ.Name)))}
		}
		if !printable(checked.typ) {
			return checkedExpression{token: checked.token, diagnostic: diagnosticAt(messageAt(checked.token, diag.PrintValueNotSupported(checked.typ.Name, printUnsupportedDetails(checked.typ))))}
		}
		arguments = append(arguments, checked.source)
	}
	node := Expression{Kind: PrintExpression, Arguments: arguments, ResultType: compilerTypes.Type{}}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee}
}
