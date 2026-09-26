package checker

import (
	"hexal/compiler/corelib"
	"hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// Diagnostic construction: the span-to-token adapter and provenance helpers.

// tokenAt builds the diagnostic token for a span. A nil table, which only a
// lone module checked outside Compile has, resolves to the zero position and
// so renders as no location rather than inventing one.
func tokenAt(table *span.Table, s span.Span) lexer.Token {
	position := span.Position{}
	if table != nil {
		position = table.Position(s)
	}
	return lexer.Token{Span: s, Line: position.Line, Column: position.Column}
}

func messageAt(token lexer.Token, message diagnostics.Message) compilerTypes.Diagnostic {
	return compilerTypes.At(message, token.Span, span.Position{Line: token.Line, Column: token.Column})
}

func protectedBindingNameDiagnostic(token lexer.Token, name string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.ProtectedBindingName(name))
}

func importAliasConflictDiagnostic(token lexer.Token, name string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.ImportAliasConflict(name))
}

func duplicateLoopBinderDiagnostic(token lexer.Token, name string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.DuplicateLoopBinder(name))
}

func missingCImportDeclarationDiagnostic(token lexer.Token, header, name string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.MissingCImportDeclaration(header, name))
}

func mappedCDeclarationDiagnostic(token lexer.Token, name, mapped string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.MappedCDeclaration(name, mapped))
}

func duplicateExportEntryDiagnostic(token lexer.Token, key string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.DuplicateExportEntry(key))
}

func unknownExportMethodDiagnostic(token lexer.Token, key string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.UnknownExportMethod(key))
}

func entryEnvironmentFunctionDiagnostic(token lexer.Token, name string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.EntryEnvironmentFunction(name))
}

func exportedImportAliasDiagnostic(token lexer.Token, name string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.ExportedImportAlias(name))
}

func unknownDeclarationDiagnostic(token lexer.Token, name string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.UnknownDeclaration(name))
}

func declarationPrivateToModuleDiagnostic(token lexer.Token, name, module string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.DeclarationPrivateToModule(name, module))
}

func importedModuleExecutableDiagnostic(token lexer.Token, module string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.ImportedModuleHasExecutableStatements(module))
}

func importedModuleMutableBindingDiagnostic(token lexer.Token, module, name string) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.ImportedModuleMutableBinding(module, name))
}

func checkerCInteropTargetDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.CheckerCInteropNeedsTarget())
}

func loopMustYieldDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.LoopMustYield())
}

func coreTypeMigrationDiagnostic(token lexer.Token, hint corelib.MigrationHint, moduleError bool) compilerTypes.Diagnostic {
	if moduleError {
		if hint.Kind == corelib.MovedTypeHint {
			return messageAt(token, diagnostics.MovedCoreTypeAsModule(hint.Name, hint.Module, hint.Alias))
		}
		return messageAt(token, diagnostics.RemovedCoreNamespaceAsModule(hint.Name, hint.Function, hint.Module))
	}
	if hint.Kind == corelib.MovedTypeHint {
		return messageAt(token, diagnostics.MovedCoreTypeAsName(hint.Name, hint.Module, hint.Alias))
	}
	return messageAt(token, diagnostics.RemovedCoreNamespaceAsName(hint.Name, hint.Function, hint.Module))
}

func coreOperationMigrationDiagnostic(token lexer.Token, hint corelib.MigrationHint) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.MovedCoreOperation(hint.Owner, hint.Operation, hint.Function, hint.Module, hint.Alias))
}

func coreConstructorMigrationDiagnostic(token lexer.Token, hint corelib.MigrationHint) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.MovedCoreConstructor(hint.Name, hint.Function, hint.Module, hint.Alias))
}

func moduleDataDiagnostic(owner, name string, token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.FunctionCannotAccessModuleBinding(owner, name))
}

func selfNotBoundDiagnostic(token lexer.Token) *compilerTypes.Diagnostic {
	diagnostic := messageAt(token, diagnostics.SelfNotBoundOutsideMethod())
	return &diagnostic
}

// diagnosticAt returns an addressable copy of one constructed diagnostic. The
// checker reports a diagnostic by pointer in the many places where absence is
// meaningful, and a constructor's result is not addressable; this adapter
// keeps every construction going through one of the *At builders instead of
// re-expanding a composite literal at each pointer site.
func diagnosticAt(diagnostic compilerTypes.Diagnostic) *compilerTypes.Diagnostic {
	return &diagnostic
}

// diagnosticInDefiningModule stamps a diagnostic returned from specializing
// an imported generic template with the defining module's own logical key,
// before it propagates back up through the requesting module's own
// checkModule call: CheckModules's own InModule stamp only ever applies to
// an unstamped diagnostic, so a body or signature failure inside the
// template still names the module that declares it, never the importer
// that merely triggered the specialization. A nil diagnostic (success)
// passes through unchanged.
func diagnosticInDefiningModule(diagnostic *compilerTypes.Diagnostic, logicalKey string) *compilerTypes.Diagnostic {
	if diagnostic == nil {
		return nil
	}
	stamped := diagnostic.InModule(logicalKey)
	return &stamped
}

func unknownAt(token lexer.Token) compilerTypes.Diagnostic {
	return messageAt(token, diagnostics.CheckerFailure())
}
