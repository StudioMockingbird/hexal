package checker

import (
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkUnsafeStatement checks one `unsafe do ... end` region. The region is an
// ordinary lexical block: it gets its own child scope, its own deferred
// actions, and every enclosed statement is checked exactly as it would be
// outside, with the single difference that operations classified unsafe-capable
// now find a non-zero lexical depth.
func checkUnsafeStatement(statement parser.UnsafeStatement, ctx checkContext, loopDepth int) (UnsafeStatement, compilerTypes.Diagnostics) {
	bodyScope := ctx.names.child()
	bodyScope.unsafeDepth = ctx.names.unsafeDepth + 1
	body, diagnostics := checkStatements(statement.Body, checkContext{names: bodyScope, typeEnvironment: ctx.typeEnvironment}, loopDepth)
	if len(diagnostics) == 0 {
		ctx.names.recordChildReturnFlows(bodyScope.returnFlows)
	}
	return UnsafeStatement{
		Body:         body,
		BodyDefers:   append([]DeferredAction(nil), bodyScope.defers...),
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
	}, diagnostics
}

// unsafeOperation names one operation whose safety cannot be proved locally.
// Every such operation routes its permission check through requireUnsafe, so
// the classification lives in exactly one place instead of being spelled at
// each call site.
type unsafeOperation string

// The classified unsafe-capable operations. Each string is the operation
// spelling its diagnostic names.
const (
	unsafeSliceFromPointer unsafeOperation = "Slice.from_pointer"
	unsafePointerOffset    unsafeOperation = "Ptr.offset"
	unsafePointerCast      unsafeOperation = "Ptr.cast"
	unsafePointerIndex     unsafeOperation = "pointer indexing"
)

// requireUnsafe reports the permission diagnostic when operation is written
// outside every enclosing unsafe region, and nil when the permission is
// active. Callers run it after ordinary resolution so a program that is
// invalid regardless of safety keeps its earlier diagnostic.
func requireUnsafe(ctx checkContext, token lexer.Token, operation unsafeOperation) *compilerTypes.Diagnostic {
	if ctx.names != nil && ctx.names.unsafeDepth > 0 {
		return nil
	}
	return diagnosticAt(typeErrorAt(token, string(operation)+" requires an unsafe do ... end block"))
}
