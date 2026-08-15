// Package checker validates syntax and resolves it into generator-ready data.
package checker

import (
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// Program contains the checked, type-resolved statements handed to the
// generator.
type Program struct {
	TypeDeclarations     []TypeDeclaration
	Statements           []Statement
	SpecializedFunctions []FunctionDeclaration
	SpecializedMethods   []MethodDeclaration
	Defers               []DeferredAction
}

// Statement is one generator-ready checked statement.
type Statement interface {
	statementNode()
}

// TypeDeclaration is a checked transparent alias. It is retained outside the
// executable statement list so generation can ignore it without losing proof
// that the declaration was resolved.
type TypeDeclaration struct {
	Name         string
	Type         compilerTypes.Type
	TypeUse      compilerTypes.TypeUse
	SourceLine   int
	SourceColumn int
}

// Declaration binds a name to a resolved type, binding mode, and checked
// initializer.
type Declaration struct {
	Name         string
	Binding      BindingID
	Type         compilerTypes.Type
	TypeUse      compilerTypes.TypeUse
	Source       Operand
	Mutable      bool
	SourceLine   int
	SourceColumn int
}

func (Declaration) statementNode() {}

// Assignment writes the checked source expression to a checked place.
type Assignment struct {
	Name         string
	Target       Operand
	Type         compilerTypes.Type
	Source       Operand
	SourceLine   int
	SourceColumn int
}

func (Assignment) statementNode() {}

// IfStatement is the checked conditional chain. Conditions are complete Bool
// operands and each body contains only checked statements in its own scope.
type IfStatement struct {
	Condition       Operand
	ConditionLine   int
	ConditionColumn int
	Then            []Statement
	ElseIf          []IfBranch
	Else            []Statement
	ElseLine        int
	ElseColumn      int
	SourceLine      int
	SourceColumn    int
	EndLine         int
	EndColumn       int
	ThenDefers      []DeferredAction
	ElseIfDefers    [][]DeferredAction
	ElseDefers      []DeferredAction
}

func (IfStatement) statementNode() {}

type IfBranch struct {
	Condition       Operand
	ConditionLine   int
	ConditionColumn int
	Body            []Statement
	SourceLine      int
	SourceColumn    int
}

// WhileStatement is a checked pre-test loop. Its body was checked with one
// additional loop context and one child lexical scope.
type WhileStatement struct {
	Condition Operand
	// ConditionKnown retains the known-value metadata of a named immutable
	// binding read used as the condition, so the constant-required
	// while-true starvation diagnostic keeps working while the read itself
	// stays in Condition. Nil for every other condition shape.
	ConditionKnown  *Operand
	ConditionLine   int
	ConditionColumn int
	Body            []Statement
	SourceLine      int
	SourceColumn    int
	EndLine         int
	EndColumn       int
	BodyDefers      []DeferredAction
}

func (WhileStatement) statementNode() {}

// ForStatement iterates one built-in collection or text source (RFC 0028).
// Binders are fresh immutable names typed by the source; Source is evaluated
// once before the loop and never re-evaluated.
type ForStatement struct {
	Binders      []ForBinder
	Source       Operand
	Body         []Statement
	BodyDefers   []DeferredAction
	SourceLine   int
	SourceColumn int
}

func (ForStatement) statementNode() {}

// ForBinder is one checked for-in binder: name plus resolved element, key,
// value, or index type.
type ForBinder struct {
	Name         string
	Type         compilerTypes.Type
	Binding      BindingID
	SourceLine   int
	SourceColumn int
}

type BreakStatement struct {
	SourceLine   int
	SourceColumn int
}

func (BreakStatement) statementNode() {}

type ContinueStatement struct {
	SourceLine   int
	SourceColumn int
}

func (ContinueStatement) statementNode() {}

// DeferredAction is one registered deferred action. A direct call captures
// its callee and arguments at registration; any other expression evaluates
// at scope exit.
type DeferredAction struct {
	IsCall bool
	Call   *Operand
	Value  *Operand
	// Err marks an RFC 0029 errdefer action: it runs only when the current
	// function exits by returning Error.
	Err bool
}

// DeferStatement is the checked registration of one deferred action.
type DeferStatement struct {
	Expression   Operand
	Action       DeferredAction
	SourceLine   int
	SourceColumn int
}

func (DeferStatement) statementNode() {}

// ReturnStatement leaves the enclosing function. Value is nil for a bare
// return, which only a no-return function accepts.
type ReturnStatement struct {
	Value        *Operand
	SourceLine   int
	SourceColumn int
}

func (ReturnStatement) statementNode() {}

// CallStatement is a call in statement position. It is the only place a
// no-return call may appear.
type CallStatement struct {
	Call         Operand
	SourceLine   int
	SourceColumn int
}

func (CallStatement) statementNode() {}

// TryStatement discards the success value of an RFC 0029 try operand. The
// checked Expression is a TryExpression carrying the propagation metadata;
// the generator hoists its prologue and emits no value use.
type TryStatement struct {
	Expression   Operand
	SourceLine   int
	SourceColumn int
}

func (TryStatement) statementNode() {}

type binding struct {
	typ        compilerTypes.Type
	use        compilerTypes.TypeUse
	mutable    bool
	known      *Operand
	kind       bindingKind
	parameter  bool // fixed function parameter: readable, never assignable
	loopBinder bool // a for-in binder: fresh and immutable (RFC 0028)
	id         BindingID
	// viewRoots and viewRootKind record a View binding's root so a later
	// return of the binding can classify it (RFC 0046 item 4).
	viewRoots    []BindingID
	viewRootKind ViewRootKind
	// fromRef records that this binding's value originated from a `ref`
	// expression in this function body, so from_pointer can reject it
	// (RFC 0046 item 4).
	fromRef bool
	// moduleID is the target canonical module of an aliasBinding import
	// (RFC 0034). It is empty for every value and function binding.
	moduleID string
}

// Check resolves declared types, checks initializers, binding modes, pointer
// capabilities, and assignment places. A failed statement never enters the
// environment, so later diagnostics cannot observe invalid declarations.
func Check(program parser.Program) (Program, error) {
	checked, err := CheckModules(map[string]parser.Program{entrypointLogicalKey: program}, []string{canonicalEntrypoint}, canonicalEntrypoint)
	// The partially checked program is returned alongside diagnostics: clean
	// statements survive failed ones, and later diagnostics cannot observe
	// invalid declarations.
	return checked[entrypointLogicalKey], err
}

// entrypointLogicalKey is the synthetic logical key of a single-module
// compilation; canonicalEntrypoint is its canonical module identity.
const (
	entrypointLogicalKey = "app.hex"
	canonicalEntrypoint  = "app"
)

// CheckModules checks every reachable module in dependency-first order (the
// order slice, canonical module ids, dependencies first). Each module is
// checked in its own scope; the returned map is keyed by logical source key
// and holds only modules that checked clean. Diagnostics are merged sorted by
// module order, then line, then column.
func CheckModules(programs map[string]parser.Program, order []string, entrypointCanonical string) (map[string]Program, error) {
	checked := make(map[string]Program, len(programs))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	registry := buildModuleRegistry(programs, order, entrypointCanonical)
	for _, moduleID := range order {
		key := moduleID + ".hex"
		program, ok := programs[key]
		if !ok {
			key = moduleID
			program, ok = programs[key]
		}
		if !ok {
			continue
		}
		moduleChecked, moduleDiagnostics := checkModule(program, moduleID, entrypointCanonical, registry)
		diagnostics = append(diagnostics, moduleDiagnostics...)
		checked[key] = moduleChecked
		if len(moduleDiagnostics) == 0 {
			// RFC 0034 Task 5: a clean module publishes its exported
			// interface before its own closure is validated, so importers see
			// complete records and the walker can prove its own exports
			// against the registry.
			registry.registerExports(moduleID, moduleChecked)
			diagnostics = append(diagnostics, registry.checkExportedClosure(moduleID, moduleChecked)...)
		}
	}
	// RFC 0034 Task 6: after every module checks, fold each defining module's
	// specialization collection -- its own requests plus every importer's --
	// into its checked program, deduplicated by key and deterministically
	// ordered. Requests recorded while a later module failed are harmless:
	// the compilation already reports diagnostics.
	for _, moduleID := range order {
		key := moduleID + ".hex"
		program, ok := checked[key]
		if !ok {
			key = moduleID
			program, ok = checked[key]
		}
		if !ok {
			continue
		}
		registry.assembleSpecializations(moduleID, &program)
		checked[key] = program
	}
	if len(diagnostics) > 0 {
		return checked, diagnostics
	}
	return checked, nil
}

// checkModule checks one module in its own scope. moduleID is the module's
// canonical identity; entrypointCanonical is the root module's canonical
// identity, the only module allowed to execute statements. registry carries
// the import aliases every module scope sees (RFC 0034).
func checkModule(program parser.Program, moduleID string, entrypointCanonical string, registry *ModuleRegistry) (Program, compilerTypes.Diagnostics) {
	checked := Program{
		TypeDeclarations: make([]TypeDeclaration, 0),
		Statements:       make([]Statement, 0, len(program.Statements)),
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	environment := moduleScope(moduleID, registry)
	typeEnvironment := compilerTypes.NewEnvironmentWithOwner(moduleID)

	items := program.Items
	if items == nil {
		items = make([]parser.TopLevelItem, 0, len(program.Statements))
		for _, statement := range program.Statements {
			items = append(items, statement)
		}
	}
	sawNonImportItem := false
	for _, item := range items {
		// RFC 0034: only the entrypoint module executes statements; an
		// imported module's top level is declarations only. The offending
		// statement is skipped entirely, never partially checked.
		if moduleID != entrypointCanonical {
			if token, executable := executableItemToken(item); executable {
				diagnostics = append(diagnostics, compilerTypes.Diagnostic{
					Category: compilerTypes.ModuleError,
					Stage:    "checker",
					Line:     token.Line,
					Column:   token.Column,
					Message:  "imported module " + moduleID + " contains executable statements",
				})
				continue
			}
		}
		// RFC 0034: imports must form the module's prefix; the first item
		// that is not a declaration ends it.
		if !declarationItem(item) {
			sawNonImportItem = true
		}
		switch statement := item.(type) {
		case parser.TypeDeclaration:
			checkedDeclaration, statementDiagnostics := checkTypeDeclaration(statement, typeEnvironment, environment)
			diagnostics = append(diagnostics, statementDiagnostics...)
			// A generic declaration is registered as an open template and
			// carries no canonical type of its own, so it is not an alias.
			if len(statementDiagnostics) == 0 && checkedDeclaration.Type.Name != "" {
				typeEnvironment.DeclareAliasUse(statement.Name.Lexeme, checkedDeclaration.TypeUse)
				checked.TypeDeclarations = append(checked.TypeDeclarations, checkedDeclaration)
			}
		case parser.Declaration:
			checkedStatement, declaredBinding, statementDiagnostics := checkDeclaration(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				environment.define(statement.Name.Lexeme, declaredBinding)
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.Assignment:
			checkedStatement, statementDiagnostics := checkAssignment(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.FunctionDeclaration:
			// The signature is bound inside checkFunctionDeclaration, before the
			// body is checked, so self-recursion resolves and a later call
			// cannot see this declaration early.
			checkedStatement, statementDiagnostics := checkFunctionDeclaration(statement, environment, typeEnvironment, !statement.HasSyntaxErrors)
			diagnostics = append(diagnostics, statementDiagnostics...)
			// A generic declaration is an open template with no concrete
			// function of its own and emits nothing.
			if len(statementDiagnostics) == 0 && checkedStatement.Type != (compilerTypes.Type{}) {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.CallExpression:
			checkedStatement, statementDiagnostics := checkCallStatement(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.TryStatement:
			// RFC 0049 item 8.3: a try statement reuses the try-expression
			// validation and propagation metadata; only the success value
			// differs, and it is discarded.
			checkedTry := checkTryExpression(parser.TryExpression{Keyword: statement.Keyword, Operand: statement.Operand}, expressionContext{}, environment, typeEnvironment)
			if errs := initializerDiagnostics(checkedTry); len(errs) > 0 {
				diagnostics = append(diagnostics, errs...)
				continue
			}
			checked.Statements = append(checked.Statements, TryStatement{
				Expression:   checkedTry.source,
				SourceLine:   statement.Keyword.Line,
				SourceColumn: statement.Keyword.Column,
			})
		case parser.ReturnStatement:
			_, statementDiagnostics := checkReturnStatement(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
		case parser.IfStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, environment, typeEnvironment, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.WhileStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, environment, typeEnvironment, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.BreakStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, environment, typeEnvironment, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.ContinueStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, environment, typeEnvironment, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.DeferStatement:
			checkedStatement, statementDiagnostics := checkDeferStatement(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.ErrdeferStatement:
			// RFC 0049: errdefer is grammatically a statement at root but is
			// valid only where an enclosing function result accepts Error;
			// the shared check owns that diagnostic. Never append the invalid
			// action.
			_, statementDiagnostics := checkErrdeferStatement(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
		case parser.ImplDeclaration:
			// Like a function, the method is bound before its body is checked,
			// so it sees itself and nothing declared after it.
			checkedStatement, statementDiagnostics := checkImplDeclaration(statement, environment, typeEnvironment, !statement.HasSyntaxErrors)
			diagnostics = append(diagnostics, statementDiagnostics...)
			// A generic method is an open template with no concrete method of
			// its own and emits nothing.
			if len(statementDiagnostics) == 0 && checkedStatement.Object != nil {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.ImportDeclaration:
			if sawNonImportItem {
				diagnostics = append(diagnostics, compilerTypes.Diagnostic{
					Category: compilerTypes.ModuleError,
					Stage:    "checker",
					Line:     statement.ModuleKeyword.Line,
					Column:   statement.ModuleKeyword.Column,
					Message:  "imports must precede all other items",
				})
			}
			// RFC 0034 Task 4: the alias is a fixed module identity, not a
			// value; it is registered as an alias record and name lookup skips
			// it, so resolution to the module's names arrives with the module
			// phase.
			target := canonicalModuleID(moduleID, strings.Trim(statement.Path.Lexeme, "\""))
			if !environment.define(statement.Alias.Lexeme, binding{kind: aliasBinding, moduleID: target}) {
				diagnostics = append(diagnostics, compilerTypes.Diagnostic{
					Category: compilerTypes.NameError,
					Stage:    "checker",
					Line:     statement.Alias.Line,
					Column:   statement.Alias.Column,
					Message:  "import alias " + statement.Alias.Lexeme + " conflicts with an existing name",
				})
			}
		default:
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.UnknownError,
				Stage:    "checker",
				Message:  "unsupported top-level item",
			})
		}
	}

	checked.TypeDeclarations = append(checked.TypeDeclarations, environment.generics.typeDeclarations...)
	checked.SpecializedFunctions = specializedFunctionList(environment.generics)
	checked.SpecializedMethods = specializedMethodList(environment.generics)
	checked.Defers = append(checked.Defers, environment.defers...)

	if len(diagnostics) > 0 {
		return checked, diagnostics
	}
	// RFC 0037: the starvation rule runs only after the program checked
	// clean, so its Semantic Errors are never mixed with earlier failures.
	starvationDiagnostics := checkStarvation(checked)
	if len(starvationDiagnostics) > 0 {
		return checked, starvationDiagnostics
	}
	if registry != nil {
		// RFC 0034 Task 6: a clean module publishes its generic templates and
		// its own specialization requests, so importers resolve and record
		// against the defining module's collection.
		registry.registerGenerics(moduleID, environment.generics)
	}
	return checked, nil
}

// declarationItem reports whether item is one of the four module-level
// declaration forms. Only these may follow the import prefix without ending
// it (RFC 0034).
func declarationItem(item parser.TopLevelItem) bool {
	switch item.(type) {
	case parser.TypeDeclaration, parser.FunctionDeclaration, parser.ImplDeclaration, parser.ImportDeclaration:
		return true
	}
	return false
}

// executableItemToken classifies one top-level item as an executable
// statement, returning the token diagnostics point at: the declared name for
// a data declaration, the statement keyword when one exists, or 1,1. An
// imported module may contain none of these (RFC 0034).
func executableItemToken(item parser.TopLevelItem) (lexer.Token, bool) {
	switch statement := item.(type) {
	case parser.Declaration:
		return statement.Name, true
	case parser.TryStatement:
		return statement.Keyword, true
	case parser.IfStatement:
		return statement.Keyword, true
	case parser.WhileStatement:
		return statement.Keyword, true
	case parser.ForStatement:
		return statement.Keyword, true
	case parser.BreakStatement:
		return statement.Keyword, true
	case parser.ContinueStatement:
		return statement.Keyword, true
	case parser.DeferStatement:
		return statement.Keyword, true
	case parser.ErrdeferStatement:
		return statement.Keyword, true
	case parser.ReturnStatement:
		return statement.Keyword, true
	case parser.Assignment, parser.CallExpression:
		return lexer.Token{Line: 1, Column: 1}, true
	}
	return lexer.Token{}, false
}

// assignable reports whether source may initialize or assign to target. The
// single exception to identical types is outermost-layer weakening: MutPtr<T>
// is acceptable where Ptr<T> is expected, with every layer below identical.
func assignable(target, source compilerTypes.Type) bool {
	return compilerTypes.Assignable(target, source)
}
