package diagnostics

func ParserExpectedDeclaration() Message {
	return message("syntax.expected-declaration", CategorySyntax, StageParser, "expected a declaration")
}
func ParserExpectedToken(expected string) Message {
	return message("syntax.expected-token", CategorySyntax, StageParser, "expected "+expected)
}
func ParserExpectedDelimiter(detail string) Message {
	return message("syntax.expected-delimiter", CategorySyntax, StageParser, "expected "+detail)
}

func ParserExpectedFunctionBody() Message {
	return ParserExpectedDelimiter("'do' after function signature")
}
func ParserExpectedMethodBody() Message {
	return ParserExpectedDelimiter("'do' after method signature")
}
func ParserExpectedIfThen() Message { return ParserExpectedDelimiter("'then' after if condition") }
func ParserExpectedElseIfThen() Message {
	return ParserExpectedDelimiter("'then' after elseif condition")
}
func ParserExpectedEnd(owner string) Message {
	return message("syntax.expected-end", CategorySyntax, StageParser, "expected end to close "+owner)
}
func ParserExpectedValue() Message {
	return message("syntax.expected-value", CategorySyntax, StageParser, "expected a value")
}
func ParserPatternNegativeInteger() Message {
	return message("syntax.pattern-negative-literal", CategorySyntax, StageParser, "expected a scalar literal after '-' in a pattern")
}
func ParserPatternLiteral() Message {
	return message("syntax.pattern-literal-expected", CategorySyntax, StageParser, "expected a scalar literal in a pattern")
}
func ParserMixedBinaryOperators(found, after string) Message {
	return message("syntax.mixed-binary-operators", CategorySyntax, StageParser, "mixed binary operators require parentheses; found '"+found+"' after '"+after+"'")
}
func ParserUnknownError() Message {
	return message("internal.parser-error", CategoryUnknown, StageParser, "internal compiler error")
}
func ParserMaxNesting() Message {
	return message("syntax.parser-nesting-limit", CategorySyntax, StageParser, "nesting exceeds the maximum depth of 128")
}
func ParserMissingCHeader() Message {
	return message("syntax.foreign-header-required", CategorySyntax, StageParser, "foreign declaration requires a C header")
}
func ParserUnsupportedForeignDeclaration() Message {
	return message("syntax.unsupported-foreign-declaration", CategorySyntax, StageParser, "unsupported foreign declaration")
}
func ParserModuleLiteralAfterFrom() Message {
	return message("syntax.module-literal-after-from", CategorySyntax, StageParser, "a module path literal after 'from'")
}
func ParserDeclarationTypeForm() Message {
	return message("syntax.type-declaration-form", CategorySyntax, StageParser, "type declarations use 'is', not '='")
}
func ParserAdtDeclarationForm() Message {
	return message("syntax.adt-declaration-form", CategorySyntax, StageParser, "ADT declarations use 'type Name is union ... end'")
}
func ParserStructDeclarationForm() Message {
	return message("syntax.struct-declaration-form", CategorySyntax, StageParser, "struct declarations use 'struct ... end'")
}
func ParserIsTestChained() Message {
	return message("syntax.is-test-chained", CategorySyntax, StageParser, "is tests cannot be chained")
}
func ParserMutRightHandSide() Message {
	return message("syntax.mut-right-hand-side", CategorySyntax, StageParser, "mut is not valid on the right-hand side; use @value")
}
func ParserNamedConstructorArguments() Message {
	return message("syntax.named-constructor-arguments", CategorySyntax, StageParser, "constructors use named arguments in parentheses")
}
func ParserSpreadArguments() Message {
	return message("syntax.spread-arguments", CategorySyntax, StageParser, "spread arguments are not supported; pass explicit values")
}
func ParserNegativeNumber() Message {
	return message("syntax.negative-number", CategorySyntax, StageParser, "expected an integer or decimal floating literal after '-'")
}
func ParserPayloadNeedsField() Message {
	return message("syntax.adt-payload-needs-field", CategorySyntax, StageParser, "a payload must declare at least one field")
}
func ParserTypeAfterColon() Message {
	return message("syntax.type-after-colon", CategorySyntax, StageParser, "expected a type after ':' in a 'let' declaration")
}
func ParserMissingEqualsLet() Message {
	return message("syntax.missing-equals-let", CategorySyntax, StageParser, "expected '=' in a 'let' declaration")
}
func ParserInvalidCSpelling(spelling string) Message {
	return message("syntax.invalid-c-spelling", CategorySyntax, StageParser, "invalid C spelling "+spelling)
}
func ParserInvalidCHeaderName(name string) Message {
	return message("syntax.invalid-c-header-name", CategorySyntax, StageParser, "invalid C header name "+name)
}
func ParserExpectedCondition(keyword string) Message {
	return message("syntax.condition-after-keyword", CategorySyntax, StageParser, "expected a condition after '"+keyword+"'")
}
func ParserExternBeforeTopLevel() Message {
	return message("syntax.extern-block-order", CategorySyntax, StageParser, "extern blocks must precede ordinary top-level items")
}
func ParserExportLast() Message {
	return message("syntax.export-block-order", CategorySyntax, StageParser, "export block must be the final top-level construct")
}
func ParserImportFirst() Message {
	return message("syntax.import-block-order", CategorySyntax, StageParser, "import block must be the first top-level construct")
}
func ParserImportNeedsEntry() Message {
	return message("syntax.import-block-empty", CategorySyntax, StageParser, "import block requires at least one entry")
}
func ParserImportFrom() Message {
	return message("syntax.import-from", CategorySyntax, StageParser, "expected 'from' after an import alias")
}
func ParserImportRelative() Message {
	return message("syntax.import-relative", CategorySyntax, StageParser, "quoted import paths must begin with ./ or ../")
}
func ParserImportSameLine() Message {
	return message("syntax.import-same-line", CategorySyntax, StageParser, "module reference must begin on the same line as 'from'")
}
func ParserImportDots() Message {
	return message("syntax.import-dots", CategorySyntax, StageParser, "standard-library imports use dots between components")
}
func ParserImportDotted(dotted string) Message {
	return message("syntax.import-dotted", CategorySyntax, StageParser, "standard-library imports use dotted paths; write "+dotted)
}
func ParserImportComponent() Message {
	return message("syntax.import-component", CategorySyntax, StageParser, "standard-library import requires a component after std.")
}
func ParserImportComponentAfterDot() Message {
	return message("syntax.import-component-after-dot", CategorySyntax, StageParser, "expected a standard-library module component after '.'")
}
func ParserExportNeedsEntry() Message {
	return message("syntax.export-block-empty", CategorySyntax, StageParser, "export block requires at least one entry")
}
func ParserModuleScopeOnly(kind string) Message {
	return message("syntax.module-scope-only", CategorySyntax, StageParser, kind+" are module-level only")
}
func ParserNamedFunctionScope() Message {
	return message("syntax.named-function-scope", CategorySyntax, StageParser, "named function declarations are only valid at module scope")
}
func ParserAnonymousStatement() Message {
	return message("syntax.anonymous-function-statement", CategorySyntax, StageParser, "anonymous functions cannot begin statements; bind the function first")
}
func ParserAnonymousFunForm() Message {
	return message("syntax.anonymous-function-form", CategorySyntax, StageParser, "anonymous function requires '(' or '<' after 'fun'")
}
func ParserCallSameLine() Message {
	return message("syntax.call-same-line", CategorySyntax, StageParser, "a call's ( must follow its callee on the same line")
}
func ParserElseifInWhile() Message {
	return message("syntax.elseif-in-while", CategorySyntax, StageParser, "'elseif' cannot appear inside a while body")
}
func ParserElseifAfterElse() Message {
	return message("syntax.elseif-after-else", CategorySyntax, StageParser, "'elseif' cannot appear after 'else'")
}
func ParserElseifOutsideIf() Message {
	return message("syntax.elseif-outside-if", CategorySyntax, StageParser, "unexpected 'elseif' outside an if statement")
}
func ParserExportPrefix() Message {
	return message("syntax.export-prefix", CategorySyntax, StageParser, "export may prefix only a module-level type, function, or implementation declaration")
}
func ParserElseInWhile() Message {
	return message("syntax.else-in-while", CategorySyntax, StageParser, "'else' cannot appear inside a while body")
}
func ParserElseFinal() Message {
	return message("syntax.else-final", CategorySyntax, StageParser, "'else' must be the final clause of an if statement")
}
func ParserElseOutsideIf() Message {
	return message("syntax.else-outside-if", CategorySyntax, StageParser, "unexpected 'else' outside an if statement")
}
func ParserEndOutsideBlock() Message {
	return message("syntax.end-outside-block", CategorySyntax, StageParser, "unexpected 'end' outside a block")
}
func ParserMutAfterLet() Message {
	return message("syntax.mut-after-let", CategorySyntax, StageParser, "'mut' appears only immediately after 'let' in a declaration")
}
func ParserReturnSameLine() Message {
	return message("syntax.return-same-line", CategorySyntax, StageParser, "a return value must begin on the same line as return")
}
func ParserAssignmentEquals() Message {
	return message("syntax.assignment-equals", CategorySyntax, StageParser, "expected '=' for an assignment")
}
func ParserRestFinal() Message {
	return message("syntax.rest-parameter-final", CategorySyntax, StageParser, "rest parameter must be final")
}
func ParserParameterAnnotation() Message {
	return message("syntax.parameter-annotation", CategorySyntax, StageParser, "function parameters require type annotations")
}
func ParserForMaxBinders() Message {
	return message("syntax.for-max-binders", CategorySyntax, StageParser, "a for-in loop takes at most 3 binders")
}
func ParserForNeedsBinder() Message {
	return message("syntax.for-needs-binder", CategorySyntax, StageParser, "a for-in loop needs at least one binder")
}
func ParserConditionAfterKeyword(keyword string) Message {
	return message("syntax.condition-after-keyword", CategorySyntax, StageParser, "expected a condition after '"+keyword+"'")
}
func ParserDeprecatedColonEquals() Message {
	return message("syntax.deprecated-colon-equals", CategorySyntax, StageParser, "':=' is not a declaration operator; use 'let name = value'")
}
func ParserDeclarationNeedsLet() Message {
	return message("syntax.declaration-needs-let", CategorySyntax, StageParser, "declarations require 'let'")
}
func ParserAdtPayloadMutable() Message {
	return message("syntax.adt-payload-mutable", CategorySyntax, StageParser, "ADT payload fields cannot be mutable")
}
func ParserMutInsidePtr() Message {
	return message("syntax.mut-inside-ptr", CategorySyntax, StageParser, "mut is only allowed immediately inside Ptr<...> or Slice<...>")
}
func ParserSumUnionForm() Message {
	return message("syntax.sum-union-form", CategorySyntax, StageParser, "structural sum declarations use 'union ... end'")
}
func ParserSumNeedsBar() Message {
	return message("syntax.sum-needs-bar", CategorySyntax, StageParser, "structural sum declarations require at least one '|'")
}
func ParserAdtPayloadForm() Message {
	return message("syntax.adt-payload-form", CategorySyntax, StageParser, "ADT payloads use 'as ... end', not braces")
}
func ParserMatchArmAfterBar() Message {
	return message("syntax.match-arm-after-bar", CategorySyntax, StageParser, "expected a match arm after '|'")
}
func ParserInterpolationExpression() Message {
	return message("syntax.interpolation-expression", CategorySyntax, StageParser, "string interpolation requires an expression")
}
func ParserExternC() Message {
	return message("syntax.extern-c", CategorySyntax, StageParser, "expected 'c' after 'extern'")
}
func ParserExternDo() Message {
	return message("syntax.extern-do", CategorySyntax, StageParser, "expected 'do' after the foreign header")
}
