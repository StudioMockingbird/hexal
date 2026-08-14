// Package checker validates syntax and resolves it into generator-ready data.
package checker

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
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
	Condition       Operand
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
			target := canonicalModuleID(strings.Trim(statement.Path.Lexeme, "\""))
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

func checkTypeDeclaration(declaration parser.TypeDeclaration, typeEnvironment *compilerTypes.Environment, environment *scope) (TypeDeclaration, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	name := declaration.Name.Lexeme
	if name == "Stdio" {
		// RFC 0040: the intrinsic qualifier is not a type and cannot be
		// redeclared.
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.NameError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "Stdio is a protected intrinsic qualifier",
		})
	}
	previousType, hadPreviousType := typeEnvironment.Lookup(name)
	previousUse, hadPreviousUse := typeEnvironment.LookupUse(name)
	if compilerTypes.IsProtectedTypeName(name) {
		message := "built-in type " + name + " cannot be redeclared"
		if name == "Ptr" || name == "MutPtr" {
			message = "built-in type constructor " + name + " cannot be redeclared"
		}
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  message,
		})
	} else if typeEnvironment.Contains(name) {
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "type " + name + " is already declared",
		})
	} else if environment.declaredHere(name) {
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "type " + name + " is already declared as a value",
		})
	}

	if len(declaration.Parameters) > 0 {
		genericDiagnostics := registerGenericTypeDeclaration(declaration, typeEnvironment, environment)
		return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, genericDiagnostics
	}

	if adt, isADT := declaration.Target.(parser.AdtDefinitionExpression); isADT {
		return checkADTDeclaration(declaration, adt, typeEnvironment, environment)
	}

	if object, ok := declaration.Target.(parser.ObjectTypeExpression); ok {
		if len(object.Members) == 0 {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     declaration.Name.Line,
				Column:   declaration.Name.Column,
				Message:  "object type " + name + " must declare at least one member",
			})
		}
		// Publish a provisional nominal identity before resolving members so a
		// member may reach this object behind at least one pointer layer. The
		// identity is abandoned if any member fails and finalized only on
		// complete success. The object is stamped with the declaring module's
		// canonical id: that id is what owns its methods (RFC 0034 Task 6).
		beginResult := typeEnvironment.BeginObject(name, declaration.Name.Line, declaration.Name.Column)
		beginResult.Object.ModuleID = environment.moduleID
		members, memberDiagnostics := resolveObjectMembers(name, object, typeEnvironment, environment.generics)
		diagnostics = append(diagnostics, memberDiagnostics...)
		if len(diagnostics) == 0 {
			resolved := typeEnvironment.CompleteObject(name, members)
			if !compilerTypes.Equal(resolved, beginResult) {
				return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, compilerTypes.Diagnostics{{
					Category: compilerTypes.UnknownError,
					Stage:    "checker",
					Message:  "object identity mismatch after member resolution",
				}}
			}
			return TypeDeclaration{
				Name:         name,
				Type:         resolved,
				TypeUse:      compilerTypes.NewTypeUse(resolved),
				SourceLine:   declaration.Name.Line,
				SourceColumn: declaration.Name.Column,
			}, nil
		}
		if hadPreviousType {
			if hadPreviousUse {
				typeEnvironment.DeclareAliasUse(name, previousUse)
			} else {
				typeEnvironment.DeclareAlias(name, previousType)
			}
		} else {
			typeEnvironment.AbandonObject(name)
		}
		return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, diagnostics
	}

	if containsTypeName(declaration.Target, name) {
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "type alias " + name + " cannot reference itself",
		})
	} else if resolvedUse, diagnostic := resolveTypeUse(declaration.Target, declaration.Name, typeEnvironment, environment.generics); diagnostic != nil {
		diagnostics = append(diagnostics, *diagnostic)
	} else if len(diagnostics) == 0 {
		return TypeDeclaration{
			Name:         name,
			Type:         resolvedUse.Type,
			TypeUse:      resolvedUse,
			SourceLine:   declaration.Name.Line,
			SourceColumn: declaration.Name.Column,
		}, nil
	}

	return TypeDeclaration{
		Name:         name,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}, diagnostics
}

func resolveObjectMembers(objectName string, expression parser.ObjectTypeExpression, typeEnvironment *compilerTypes.Environment, generics *genericTable) ([]compilerTypes.ObjectMember, compilerTypes.Diagnostics) {
	members := make([]compilerTypes.ObjectMember, 0, len(expression.Members))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(expression.Members))
	for _, declaration := range expression.Members {
		if seen[declaration.Name.Lexeme] {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     declaration.Name.Line,
				Column:   declaration.Name.Column,
				Message:  fmt.Sprintf("object type %s declares member %s more than once", objectName, declaration.Name.Lexeme),
			})
			continue
		}
		seen[declaration.Name.Lexeme] = true

		if containsTypeName(declaration.Type, objectName) && !containsPointerType(declaration.Type) {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     declaration.Name.Line,
				Column:   declaration.Name.Column,
				Message:  "object type " + objectName + " cannot contain itself by value",
			})
			continue
		}

		resolvedUse, diagnostic := resolveTypeUse(declaration.Type, declaration.Name, typeEnvironment, generics)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		resolved := resolvedUse.Type
		if diagnostic := valueTypeDiagnostic(declaration.Type, declaration.Name, resolved); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if resolved.Signature != nil {
			// Supported-position whitelist: a Fun<...> member would be callback
			// data in the object layout, which this RFC defers.
			diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "Fun<…> object members are not supported"))
			continue
		}
		// RFC 0046 item 2: any complete, finitely sized value may be an object
		// member except Fun, Unknown, and Atomic at non-construction positions.
		// An open type parameter defers to specialization rechecking.
		if !compilerTypes.ContainsTypeParameter(resolved) && !compilerTypes.Storable(resolved, compilerTypes.PositionObjectMember) {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     declaration.Name.Line,
				Column:   declaration.Name.Column,
				Message:  "unsupported object member type " + resolved.Name,
			})
			continue
		}
		members = append(members, compilerTypes.ObjectMember{
			Name:         declaration.Name.Lexeme,
			Type:         resolved,
			Use:          resolvedUse,
			Mutable:      declaration.Mutable,
			SourceLine:   declaration.Name.Line,
			SourceColumn: declaration.Name.Column,
		})
	}
	return members, diagnostics
}

func containsPointerType(expression parser.TypeExpression) bool {
	switch expression := expression.(type) {
	case parser.PtrTypeExpression:
		return true
	case parser.UnionTypeExpression:
		for _, member := range expression.Members {
			if containsPointerType(member) {
				return true
			}
		}
		return false
	case parser.GroupedTypeExpression:
		return containsPointerType(expression.Inner)
	case parser.FunctionTypeExpression:
		for _, parameter := range expression.Parameters {
			if containsPointerType(parameter) {
				return true
			}
		}
		return expression.Return != nil && containsPointerType(expression.Return)
	case parser.ObjectTypeExpression:
		for _, member := range expression.Members {
			if containsPointerType(member.Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func containsTypeName(expression parser.TypeExpression, name string) bool {
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		return expression.Name.Lexeme == name
	case parser.GenericTypeExpression:
		return expression.Name.Lexeme == name
	case parser.PtrTypeExpression:
		return containsTypeName(expression.Element, name)
	case parser.UnionTypeExpression:
		for _, member := range expression.Members {
			if containsTypeName(member, name) {
				return true
			}
		}
		return false
	case parser.GroupedTypeExpression:
		return containsTypeName(expression.Inner, name)
	case parser.FunctionTypeExpression:
		for _, parameter := range expression.Parameters {
			if containsTypeName(parameter, name) {
				return true
			}
		}
		return expression.Return != nil && containsTypeName(expression.Return, name)
	case parser.ObjectTypeExpression:
		for _, member := range expression.Members {
			if containsTypeName(member.Type, name) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// registerGenericTypeDeclaration validates and stores one generic type or
// alias declaration as an open template. The target is not resolved yet:
// parameters are placeholders until a concrete specialization is requested.
func registerGenericTypeDeclaration(declaration parser.TypeDeclaration, typeEnvironment *compilerTypes.Environment, environment *scope) compilerTypes.Diagnostics {
	name := declaration.Name.Lexeme
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(declaration.Parameters))
	parameterNames := make([]string, 0, len(declaration.Parameters))
	for _, parameter := range declaration.Parameters {
		if seen[parameter.Lexeme] {
			diagnostics = append(diagnostics, typeErrorAt(parameter, "generic parameter "+parameter.Lexeme+" is declared more than once"))
			continue
		}
		seen[parameter.Lexeme] = true
		if compilerTypes.IsProtectedTypeName(parameter.Lexeme) {
			diagnostics = append(diagnostics, typeErrorAt(parameter, "generic parameter "+parameter.Lexeme+" is a protected type name"))
			continue
		}
		parameterNames = append(parameterNames, parameter.Lexeme)
	}
	if len(diagnostics) > 0 {
		return diagnostics
	}
	generic := typeEnvironment.DeclareGeneric(name, len(declaration.Parameters), parameterNames)
	if generic == nil {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, "type "+name+" is already declared")}
	}
	if _, objectTarget := declaration.Target.(parser.ObjectTypeExpression); objectTarget {
		if containsTypeName(declaration.Target, name) && !containsPointerType(declaration.Target) {
			return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, "object type "+name+" cannot contain itself by value")}
		}
	} else if containsTypeName(declaration.Target, name) {
		return compilerTypes.Diagnostics{typeErrorAt(declaration.Name, "type alias "+name+" cannot reference itself")}
	}
	environment.generics.types[name] = &openGenericType{
		Name:        name,
		Parameters:  append([]lexer.Token(nil), declaration.Parameters...),
		Target:      declaration.Target,
		Declaration: generic,
	}
	return nil
}

func checkDeclaration(declaration parser.Declaration, environment *scope, typeEnvironment *compilerTypes.Environment) (Declaration, binding, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	declaredUse, typeDiagnostic := resolveTypeUse(declaration.Type, declaration.Name, typeEnvironment, environment.generics)
	declaredType := declaredUse.Type
	if typeDiagnostic != nil {
		diagnostics = append(diagnostics, *typeDiagnostic)
	} else if diagnostic := valueTypeDiagnostic(declaration.Type, declaration.Name, declaredType); diagnostic != nil {
		diagnostics = append(diagnostics, *diagnostic)
	}
	if declaration.Name.Lexeme == "print" {
		// RFC 0030: the protected builtin name cannot be bound by a local or
		// module declaration.
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.NameError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "print is a protected built-in name",
		})
	}
	if layoutBuiltins[declaration.Name.Lexeme] {
		// RFC 0042: the layout query names cannot be bound by a local or
		// module declaration.
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.NameError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  declaration.Name.Lexeme + " is a protected built-in name",
		})
	}
	if declaration.Name.Lexeme == "Stdio" {
		// RFC 0040: the intrinsic qualifier is not a value and cannot be
		// redeclared.
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.NameError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "Stdio is a protected intrinsic qualifier",
		})
	}
	if compilerTypes.IsProtectedTypeName(declaration.Name.Lexeme) {
		message := "value " + declaration.Name.Lexeme + " is already declared as a type"
		if declaration.Name.Lexeme == "Ptr" || declaration.Name.Lexeme == "MutPtr" {
			message = "built-in type constructor " + declaration.Name.Lexeme + " cannot be redeclared"
		}
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  message,
		})
	} else if typeEnvironment.Contains(declaration.Name.Lexeme) {
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "value " + declaration.Name.Lexeme + " is already declared as a type",
		})
	}
	if environment.declaredHere(declaration.Name.Lexeme) {
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "variable " + declaration.Name.Lexeme + " is already declared; reassignment must omit the type annotation",
		})
	}

	initializer := checkInitializer(declaration.Initializer, declaredUse, declaration.Name, environment, typeEnvironment)
	for _, diagnostic := range initializerDiagnostics(initializer) {
		diagnostics = append(diagnostics, diagnostic)
	}
	if len(diagnostics) == 0 {
		if diagnostic := atomicCopyDiagnostic(initializer.source, declaration.Name); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	if len(diagnostics) == 0 && declaredType.Name != "" && !assignable(declaredType, initializer.typ) {
		diagnostics = append(diagnostics, bindingMismatchDiagnostic(declaration.Name.Lexeme, declaredType, initializer.typ, initializer.token))
	}

	declaredBinding := binding{
		typ:     declaredType,
		use:     declaredUse,
		mutable: declaration.Mutable,
		id:      environment.newBindingID(),
	}
	if initializer.source.Node.Kind == AddressOfExpression {
		declaredBinding.fromRef = true
	}
	if declaredType.View != nil {
		declaredBinding.viewRoots = initializer.source.Node.ViewRoots
		declaredBinding.viewRootKind = initializer.source.Node.RootKind
	}
	if len(diagnostics) == 0 && !declaration.Mutable && initializer.source.Kind == ConstantOperand {
		known := initializer.source
		declaredBinding.known = &known
	}
	return Declaration{
		Name:         declaration.Name.Lexeme,
		Binding:      declaredBinding.id,
		Type:         declaredType,
		TypeUse:      declaredUse,
		Source:       initializer.source,
		Mutable:      declaration.Mutable,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}, declaredBinding, diagnostics
}

func checkAssignment(assignment parser.Assignment, environment *scope, typeEnvironment *compilerTypes.Environment) (Assignment, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	target := checkPlace(assignment.Target, environment, typeEnvironment)
	switch {
	case target.diagnostic != nil:
		diagnostics = append(diagnostics, *target.diagnostic)
	case target.self:
		// Method rule 3: only the binding itself is fixed. A write through
		// self, such as self.x, is a member place and is checked as one.
		diagnostics = append(diagnostics, typeErrorAt(assignment.Name, "cannot assign to self; self is a fixed binding"))
	case target.function:
		// A function declaration names code, not a replaceable storage slot.
		diagnostics = append(diagnostics, typeErrorAt(assignment.Name, "cannot assign to function "+assignment.Name.Lexeme))
	case target.parameter:
		diagnostics = append(diagnostics, typeErrorAt(assignment.Name,
			"cannot assign to parameter "+assignment.Name.Lexeme+"; parameters are fixed bindings"))
	case target.loopBinder:
		diagnostics = append(diagnostics, typeErrorAt(assignment.Name,
			"loop binder "+assignment.Name.Lexeme+" is immutable"))
	case !target.source.Writable:
		diagnostics = append(diagnostics, assignmentTargetDiagnostic(assignment.Target, assignment.Name))
	}

	// Assignment writes to the binding's declared storage slot, never to a
	// branch-local narrowed type, and an accepted assignment invalidates any
	// narrowing: the slot may hold nil again.
	targetType := target.typ
	targetBinding := BindingID(0)
	if variable, ok := assignment.Target.(parser.VariableExpression); ok && target.source.Binding != 0 {
		targetBinding = target.source.Binding
		if bound, status := environment.lookup(variable.Name.Lexeme); status == nameFound {
			targetType = bound.typ
			target.source.Type = bound.typ
			target.use = bound.use
		}
	}
	targetUse := target.use
	if targetUse.Type == (compilerTypes.Type{}) {
		targetUse = compilerTypes.NewTypeUse(targetType)
	}
	initializer := checkInitializer(assignment.Initializer, targetUse, assignment.Name, environment, typeEnvironment)
	for _, diagnostic := range initializerDiagnostics(initializer) {
		diagnostics = append(diagnostics, diagnostic)
	}
	if len(diagnostics) == 0 {
		if diagnostic := atomicCopyDiagnostic(initializer.source, assignment.Name); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	if len(diagnostics) == 0 && initializer.typ != (compilerTypes.Type{}) && !assignable(targetType, initializer.typ) {
		diagnostics = append(diagnostics, bindingMismatchDiagnostic(assignment.Name.Lexeme, targetType, initializer.typ, initializer.token))
	}
	if len(diagnostics) == 0 && environment.flow != nil && targetBinding != 0 {
		environment.flow.invalidateNarrowing(targetBinding)
	}

	return Assignment{
		Name:         assignment.Name.Lexeme,
		Target:       target.source,
		Type:         targetType,
		Source:       initializer.source,
		SourceLine:   assignment.Name.Line,
		SourceColumn: assignment.Name.Column,
	}, diagnostics
}

func assignmentTargetDiagnostic(target parser.Expression, fallback lexer.Token) compilerTypes.Diagnostic {
	if variable, ok := target.(parser.VariableExpression); ok {
		return compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     variable.Name.Line,
			Column:   variable.Name.Column,
			Message:  "cannot assign to constant " + variable.Name.Lexeme,
		}
	}
	if property, ok := target.(parser.PropertyExpression); ok && property.Property.Lexeme != "value" {
		return compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     property.Property.Line,
			Column:   property.Property.Column,
			Message:  "cannot assign to read-only member " + placeDescription(target),
		}
	}
	return compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     fallback.Line,
		Column:   fallback.Column,
		Message:  "cannot write through a read-only pointer " + placeDescription(target),
	}
}

// bindingMismatchDiagnostic names the binding for a function-pointer slot,
// where "expected X initializer" reads poorly against two Fun<...> spellings.
func bindingMismatchDiagnostic(name string, declaredType, actualType compilerTypes.Type, token lexer.Token) compilerTypes.Diagnostic {
	if declaredType.Signature != nil || actualType.Signature != nil {
		return typeErrorAt(token, fmt.Sprintf("%s requires %s, got %s", name, declaredType.Name, actualType.Name))
	}
	return typeMismatchDiagnostic(declaredType, actualType, token)
}

func typeMismatchDiagnostic(declaredType, actualType compilerTypes.Type, token lexer.Token) compilerTypes.Diagnostic {
	message := assignabilityMismatchMessage(declaredType, actualType)
	if message == "" {
		message = fmt.Sprintf("expected %s initializer, got %s", declaredType.Name, actualType.Name)
	}
	return compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func assignabilityMismatchMessage(target, source compilerTypes.Type) string {
	if compilerTypes.IsNil(source) || compilerTypes.IsNullable(source) {
		if !compilerTypes.IsNullable(target) {
			return fmt.Sprintf("expected %s, got %s", target.Name, source.Name)
		}
	}
	if target.Element != nil && source.Element != nil {
		if target.PointeeWritable && !source.PointeeWritable && compilerTypes.IsUnknown(*source.Element) && !compilerTypes.IsUnknown(*target.Element) {
			return fmt.Sprintf("%s cannot recover writable access as %s", source.Name, target.Name)
		}
		if target.Element.Element != nil && compilerTypes.IsUnknown(*target.Element.Element) && source.Element.Element != nil {
			return fmt.Sprintf("cannot erase a nested pointer slot as %s", target.Name)
		}
		if target.PointeeWritable && source.PointeeWritable &&
			!compilerTypes.IsUnknown(*target.Element) && !compilerTypes.IsUnknown(*source.Element) &&
			!compilerTypes.Equal(*target.Element, *source.Element) {
			erased := "Ptr<Unknown>"
			if source.PointeeWritable {
				erased = "MutPtr<Unknown>"
			}
			return fmt.Sprintf("expected %s, got %s; erasure and recovery do not compose, bind %s first", target.Name, source.Name, erased)
		}
	}
	return ""
}

// resolveType is the canonical-only compatibility wrapper around the
// contextual type-use resolver.
func resolveType(expression parser.TypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.Type, *compilerTypes.Diagnostic) {
	use, diagnostic := resolveTypeUse(expression, fallback, typeEnvironment, generics)
	return use.Type, diagnostic
}

// resolveTypeUse resolves syntax into canonical identity plus the written
// candidate order retained for contextual expression checking.
func resolveTypeUse(expression parser.TypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	switch expression := expression.(type) {
	case parser.NilTypeExpression:
		// RFC 0049 item 8.1: standalone Nil is invalid in every written type
		// position (aliases, bindings, parameters, results, members,
		// payloads, collection positions, generic arguments). Union members
		// and match type patterns resolve through resolveUnionMemberUse.
		diagnostic := typeErrorAt(expression.Token, "Nil is valid only as a member of a union with a non-Nil type")
		return compilerTypes.TypeUse{}, &diagnostic
	case parser.UnknownTypeExpression:
		return compilerTypes.NewTypeUse(compilerTypes.Unknown), nil
	case parser.NamedTypeExpression:
		if generics != nil && generics.frame != nil {
			if bound, ok := generics.frame[expression.Name.Lexeme]; ok {
				return compilerTypes.NewTypeUse(bound), nil
			}
		}
		resolved, ok := typeEnvironment.LookupUse(expression.Name.Lexeme)
		if !ok {
			message := "unknown type " + expression.Name.Lexeme
			return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     expression.Name.Line,
				Column:   expression.Name.Column,
				Message:  message,
			}
		}
		return resolved, nil
	case parser.QualifiedTypeExpression:
		// RFC 0034 Task 5: a dotted type name whose leftmost name is an
		// import alias resolves against the target module's exported type
		// records. A known alias whose name is absent or private reports the
		// visibility failure; an unknown leftmost name keeps the Task 3
		// Module Error.
		if generics != nil && generics.registry != nil {
			if target, ok := generics.registry.importTarget(generics.moduleID, expression.Module.Lexeme); ok {
				use, found := generics.registry.exportedType(target, expression.Names[0].Lexeme)
				if !found {
					diagnostic := privateToModuleDiagnostic(expression.Names[0], expression.Names[0].Lexeme, target)
					return compilerTypes.TypeUse{}, &diagnostic
				}
				return use, nil
			}
		}
		message := "unknown module alias " + expression.Module.Lexeme
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.ModuleError,
			Stage:    "checker",
			Line:     expression.Module.Line,
			Column:   expression.Module.Column,
			Message:  message,
		}
	case parser.GenericTypeExpression:
		if expression.Name.Lexeme == "View" {
			return resolveViewTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "List" {
			return resolveListTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Dict" {
			return resolveDictTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Stream" {
			return resolveStreamTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Task" {
			return resolveTaskTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Channel" {
			return resolveChannelTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Atomic" {
			return resolveAtomicTypeUse(expression, fallback, typeEnvironment, generics)
		}
		return specializeTypeUse(expression, fallback, typeEnvironment, generics)
	case parser.GroupedTypeExpression:
		return resolveTypeUse(expression.Inner, fallback, typeEnvironment, generics)
	case parser.ArrayTypeExpression:
		return resolveArrayTypeUse(expression, fallback, typeEnvironment, generics)
	case parser.PtrTypeExpression:
		elementUse, diagnostic := resolveTypeUse(expression.Element, expression.Keyword, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		element := elementUse.Type
		if element.Signature != nil {
			// Supported-position whitelist: a pointer to a function pointer
			// needs C declarator and FFI rules this RFC defers.
			diagnostic := typeErrorAt(expression.Keyword, expression.Keyword.Lexeme+"<"+element.Name+"> is not supported")
			return compilerTypes.TypeUse{}, &diagnostic
		}
		var pointer compilerTypes.Type
		if expression.Writable {
			pointer = typeEnvironment.MutPtrType(element)
		} else {
			pointer = typeEnvironment.PtrType(element)
		}
		if pointer == (compilerTypes.Type{}) {
			return compilerTypes.TypeUse{}, typeErrorPointerConstruction(expression.Keyword)
		}
		return compilerTypes.PointerTypeUse(pointer, elementUse), nil
	case parser.FunctionTypeExpression:
		return resolveFunctionTypeUse(expression, typeEnvironment, generics)
	case parser.UnionTypeExpression:
		members := make([]compilerTypes.TypeUse, 0, len(expression.Members))
		canonical := make([]compilerTypes.Type, 0, len(expression.Members))
		for _, memberExpression := range expression.Members {
			member, diagnostic := resolveUnionMemberUse(memberExpression, fallback, typeEnvironment, generics)
			if diagnostic != nil {
				return compilerTypes.TypeUse{}, diagnostic
			}
			for _, candidate := range typeUseCandidates(member) {
				duplicate := false
				for _, existing := range members {
					if compilerTypes.Equal(existing.Type, candidate.Type) {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				members = append(members, candidate)
				canonical = append(canonical, candidate.Type)
			}
		}
		// RFC 0049 item 8.1: alias resolution and generic substitution run
		// before the member count, so a written union that collapses to fewer
		// than two distinct canonical members is an error, never an alias for
		// the survivor.
		if len(canonical) < 2 {
			names := make([]string, 0, len(expression.Members))
			for _, memberExpression := range expression.Members {
				use, _ := resolveUnionMemberUse(memberExpression, fallback, typeEnvironment, generics)
				names = append(names, use.Type.Name)
			}
			diagnostic := typeErrorAt(typeExpressionToken(expression, fallback), fmt.Sprintf("a union requires at least two distinct members; %s has one", strings.Join(names, " | ")))
			return compilerTypes.TypeUse{}, &diagnostic
		}
		union := typeEnvironment.UnionType(canonical)
		if union == (compilerTypes.Type{}) {
			for _, member := range members {
				if compilerTypes.IsUnknown(member.Type) {
					diagnostic := typeErrorAt(typeExpressionToken(expression, fallback), "Unknown | Nil is not a value type; use Ptr<Unknown> | Nil")
					return compilerTypes.TypeUse{}, &diagnostic
				}
			}
			diagnostic := typeErrorAt(typeExpressionToken(expression, fallback), "could not construct union type")
			return compilerTypes.TypeUse{}, &diagnostic
		}
		if len(members) == 1 {
			return members[0], nil
		}
		return compilerTypes.UnionTypeUse(union, members), nil
	default:
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     fallback.Line,
			Column:   fallback.Column,
			Message:  "unsupported type expression",
		}
	}
}

func typeUseCandidates(use compilerTypes.TypeUse) []compilerTypes.TypeUse {
	if len(use.Candidates) == 0 {
		return []compilerTypes.TypeUse{use}
	}
	return append([]compilerTypes.TypeUse(nil), use.Candidates...)
}

// resolveUnionMemberUse resolves a type in a Nil-admitting context: a union
// member, a match type pattern, or an is-test query. Everywhere else Nil is
// rejected by resolveTypeUse (RFC 0049 item 8.1). Parenthesized members and
// nested written unions recurse so `Int32 | (Nil | Float32)` flattens
// correctly.
func resolveUnionMemberUse(expression parser.TypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	switch expression := expression.(type) {
	case parser.NilTypeExpression:
		return compilerTypes.NewTypeUse(compilerTypes.Nil), nil
	case parser.GroupedTypeExpression:
		return resolveUnionMemberUse(expression.Inner, fallback, typeEnvironment, generics)
	default:
		return resolveTypeUse(expression, fallback, typeEnvironment, generics)
	}
}

func typeErrorPointerConstruction(token lexer.Token) *compilerTypes.Diagnostic {
	diagnostic := typeErrorAt(token, "could not construct pointer type")
	return &diagnostic
}

func valueTypeDiagnostic(expression parser.TypeExpression, fallback lexer.Token, typ compilerTypes.Type) *compilerTypes.Diagnostic {
	if !compilerTypes.IsUnknown(typ) {
		return nil
	}
	diagnostic := typeErrorAt(typeExpressionToken(expression, fallback), "Unknown has no known size or layout; it may only be used behind a pointer")
	return &diagnostic
}

func typeExpressionToken(expression parser.TypeExpression, fallback lexer.Token) lexer.Token {
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		return expression.Name
	case parser.NilTypeExpression:
		return expression.Token
	case parser.UnknownTypeExpression:
		return expression.Token
	case parser.PtrTypeExpression:
		return expression.Keyword
	case parser.FunctionTypeExpression:
		return expression.Keyword
	case parser.UnionTypeExpression:
		if len(expression.Members) > 0 {
			return typeExpressionToken(expression.Members[0], fallback)
		}
	case parser.GroupedTypeExpression:
		return expression.OpenParen
	default:
		return fallback
	}
	return fallback
}

// resolveFunctionTypeUse resolves Fun<(T, U) : R> while retaining nested type
// views for contextual arguments and results.
func resolveFunctionTypeUse(expression parser.FunctionTypeExpression, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	parameterUses := make([]compilerTypes.TypeUse, 0, len(expression.Parameters))
	parameters := make([]compilerTypes.Type, 0, len(expression.Parameters))
	for _, parameter := range expression.Parameters {
		resolvedUse, diagnostic := resolveTypeUse(parameter, expression.Keyword, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		if diagnostic := valueTypeDiagnostic(parameter, expression.Keyword, resolvedUse.Type); diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		parameterUses = append(parameterUses, resolvedUse)
		parameters = append(parameters, resolvedUse.Type)
	}
	var result *compilerTypes.Type
	var resultUse *compilerTypes.TypeUse
	if expression.Return != nil {
		resolvedUse, diagnostic := resolveTypeUse(expression.Return, expression.Keyword, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		if diagnostic := valueTypeDiagnostic(expression.Return, expression.Keyword, resolvedUse.Type); diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		if resolvedUse.Type.Signature != nil {
			diagnostic := typeErrorAt(expression.Keyword, "returning "+resolvedUse.Type.Name+" is not supported")
			return compilerTypes.TypeUse{}, &diagnostic
		}
		resolved := resolvedUse.Type
		result = &resolved
		resultUse = &resolvedUse
	}
	functionType := typeEnvironment.FunType(parameters, result)
	if functionType.Signature == nil {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Line:     expression.Keyword.Line,
			Column:   expression.Keyword.Column,
			Message:  "could not construct a Fun type",
		}
	}
	return compilerTypes.FunctionTypeUse(functionType, parameterUses, resultUse), nil
}

type initializerValue = checkedExpression

func initializerDiagnostics(initializer initializerValue) compilerTypes.Diagnostics {
	if len(initializer.diagnostics) > 0 {
		return initializer.diagnostics
	}
	if initializer.diagnostic != nil {
		return compilerTypes.Diagnostics{*initializer.diagnostic}
	}
	return nil
}

type expressionContext struct {
	expected           compilerTypes.TypeUse
	foldConstants      bool
	inCleanup          bool // checking a defer or errdefer action expression (RFC 0029)
	allowStandaloneNil bool // print arguments admit standalone Nil (RFC 0049 item 8.1)
}

type expressionTypeHint struct {
	typ        compilerTypes.Type
	contextual bool
	token      lexer.Token
	diagnostic *compilerTypes.Diagnostic
}

type checkedExpression struct {
	source      Operand
	typ         compilerTypes.Type
	use         compilerTypes.TypeUse
	storageType compilerTypes.Type
	variant     *compilerTypes.AdtVariant
	token       lexer.Token
	diagnostic  *compilerTypes.Diagnostic
	diagnostics compilerTypes.Diagnostics
	known       *Operand
	function    bool // the name of a declared function, which is not storage
	parameter   bool // a fixed function parameter binding
	self        bool // the implicit method receiver, a fixed binding
	loopBinder  bool // a for-in binder: fresh and immutable (RFC 0028)
}

// checkInitializer resolves a syntax expression into one checked operand.
// Numeric literals use the expected primitive type as context; operation trees
// carry that context only into untyped literals.
func checkInitializer(initializer parser.Expression, expectedUse compilerTypes.TypeUse, fallback lexer.Token, environment *scope, typeEnvironment *compilerTypes.Environment) initializerValue {
	// Initializers are the boundary where exact constants can be retained. A
	// later mutable read still becomes an expression through valueFromPlace.
	checked := checkExpression(initializer, expressionContext{expected: expectedUse, foldConstants: true}, environment, typeEnvironment)
	if len(initializerDiagnostics(checked)) == 0 && expectedUse.Type.Name != "" && compilerTypes.IsUnion(expectedUse.Type) && !compilerTypes.Equal(expectedUse.Type, checked.typ) {
		checked = injectIntoUnion(checked, expectedUse.Type)
	}
	if checked.token.Line == 0 {
		checked.token = fallback
	}
	return checked
}

func checkObjectLiteral(expression parser.ObjectLiteral, expectedType compilerTypes.Type, environment *scope, typeEnvironment *compilerTypes.Environment) initializerValue {
	literalType, ok := typeEnvironment.Lookup(expression.TypeName.Lexeme)
	if compilerTypes.IsError(literalType) {
		// RFC 0029: Error is built-in and constructed only through
		// Error.new(header, message); a raw object initializer is rejected.
		return initializerValue{token: expression.TypeName, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.TypeName.Line,
			Column:   expression.TypeName.Column,
			Message:  "Error must be created with Error.new(header, message)",
		}}
	}
	if !ok && environment.generics != nil {
		// A generic object literal names an open generic template. With
		// explicit type arguments it specializes directly; otherwise the
		// arguments are inferred from the expected destination type when it
		// is a specialization of the same template.
		if open, generic := environment.generics.types[expression.TypeName.Lexeme]; generic {
			if len(expression.TypeArguments) > 0 {
				specializedUse, diagnostic := specializeTypeUse(parser.GenericTypeExpression{Name: expression.TypeName, Arguments: expression.TypeArguments}, expression.TypeName, typeEnvironment, environment.generics)
				if diagnostic != nil {
					return initializerValue{token: expression.TypeName, diagnostic: diagnostic}
				}
				literalType = specializedUse.Type
				ok = literalType.Object != nil
			} else if expectedType.Object != nil {
				if expectedOpen := environment.generics.objectOpen[expectedType.Object]; expectedOpen == open {
					specializedUse, diagnostic := specializeTypeUseArguments(open, environment.generics.objectArguments[expectedType.Object], expression.TypeName, typeEnvironment, environment.generics)
					if diagnostic != nil {
						return initializerValue{token: expression.TypeName, diagnostic: diagnostic}
					}
					literalType = specializedUse.Type
					ok = literalType.Object != nil
				}
			}
			if !ok {
				return initializerValue{token: expression.TypeName, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError,
					Stage:    "checker",
					Line:     expression.TypeName.Line,
					Column:   expression.TypeName.Column,
					Message:  fmt.Sprintf("cannot infer generic parameter for %s", expression.TypeName.Lexeme),
				}}
			}
		}
	}
	if !ok {
		return initializerValue{token: expression.TypeName, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.TypeName.Line,
			Column:   expression.TypeName.Column,
			Message:  "unknown type " + expression.TypeName.Lexeme,
		}}
	}
	if literalType.Object == nil {
		return initializerValue{typ: literalType, token: expression.TypeName, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.TypeName.Line,
			Column:   expression.TypeName.Column,
			Message:  expression.TypeName.Lexeme + " is not an object type",
		}}
	}
	if expectedType.Name != "" && !compilerTypes.Assignable(expectedType, literalType) {
		return initializerValue{typ: literalType, token: expression.TypeName, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.TypeName.Line,
			Column:   expression.TypeName.Column,
			Message:  fmt.Sprintf("expected %s, got %s", expectedType.Name, literalType.Name),
		}}
	}

	values := make([]ObjectMemberValue, 0, len(expression.Initializers))
	seen := make(map[string]bool, len(expression.Initializers))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, initializer := range expression.Initializers {
		member, exists := literalType.Object.Member(initializer.Name.Lexeme)
		if !exists {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     initializer.Name.Line,
				Column:   initializer.Name.Column,
				Message:  fmt.Sprintf("%s has no member %s", literalType.Name, initializer.Name.Lexeme),
			})
			continue
		}
		if seen[member.Name] {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     initializer.Name.Line,
				Column:   initializer.Name.Column,
				Message:  fmt.Sprintf("%s literal initializes member %s more than once", literalType.Name, member.Name),
			})
			continue
		}
		seen[member.Name] = true
		memberUse := member.Use
		if memberUse.Type == (compilerTypes.Type{}) {
			memberUse = compilerTypes.NewTypeUse(member.Type)
		}
		checked := checkInitializer(initializer.Value, memberUse, initializer.Name, environment, typeEnvironment)
		if nestedDiagnostics := initializerDiagnostics(checked); len(nestedDiagnostics) > 0 {
			diagnostics = append(diagnostics, nestedDiagnostics...)
			continue
		}
		if !assignable(member.Type, checked.typ) {
			diagnostics = append(diagnostics, typeMismatchDiagnostic(member.Type, checked.typ, checked.token))
			continue
		}
		values = append(values, ObjectMemberValue{Member: member, Source: checked.source})
	}
	for index := range literalType.Object.Members {
		member := &literalType.Object.Members[index]
		if !seen[member.Name] {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     expression.TypeName.Line,
				Column:   expression.TypeName.Column,
				Message:  fmt.Sprintf("%s literal is missing member %s", literalType.Name, member.Name),
			})
		}
	}
	value := &ObjectValue{Type: literalType, Initializers: values}
	return initializerValue{
		source:      Operand{Kind: ObjectOperand, Type: literalType, Object: value, Node: Expression{Kind: ObjectExpression, Object: value}},
		typ:         literalType,
		token:       expression.TypeName,
		diagnostics: diagnostics,
		diagnostic: func() *compilerTypes.Diagnostic {
			if len(diagnostics) == 0 {
				return nil
			}
			return &diagnostics[0]
		}(),
	}
}

// checkValue resolves an expression in value context. Assignment and ref call
// checkPlace instead to retain place mode.
func checkValue(expression parser.Expression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	return checkExpression(expression, expressionContext{}, environment, typeEnvironment)
}

func checkExpression(expression parser.Expression, context expressionContext, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(context.expected.Candidates) > 1 && isContextualExpression(expression) {
		return checkContextualUnion(expression, context.expected, environment, typeEnvironment)
	}
	switch expression := expression.(type) {
	case parser.IntegerLiteral:
		return checkedFromInitializer(integerInitializer(expression.Token, contextualIntegerType(context.expected.Type), false))
	case parser.DecimalLiteral:
		return checkedFromInitializer(floatInitializer(expression.Token, contextualFloatType(context.expected.Type), false))
	case parser.NegatedNumericLiteral:
		return checkedFromInitializer(negatedInitializer(expression, context.expected.Type))
	case parser.BooleanLiteral:
		source := constantOperand(compilerTypes.Bool, constant.MakeBool(expression.Token.Kind == lexer.True), expression.Token.Lexeme)
		source.Node = constantNode(source)
		return checkedExpression{source: source, typ: compilerTypes.Bool, token: expression.Token, known: &source}
	case parser.NilLiteral:
		source := nilOperand(expression.Token.Lexeme)
		known := source
		expected := context.expected.Type
		// RFC 0049 item 8.1: the nil literal requires a contextual union
		// containing Nil, except as a print argument (allowStandaloneNil),
		// which is the sole position admitting standalone Nil. A Nil expected
		// type arises only from the nil == nil / nil != nil equality path.
		if context.allowStandaloneNil || compilerTypes.IsNil(expected) ||
			(compilerTypes.IsUnion(expected) && compilerTypes.ContainsUnionMember(expected, compilerTypes.Nil)) {
			return checkedExpression{source: source, typ: compilerTypes.Nil, token: expression.Token, known: &known}
		}
		diagnostic := typeErrorAt(expression.Token, "nil requires an expected union containing Nil")
		return checkedExpression{token: expression.Token, diagnostic: &diagnostic}
	case parser.EosLiteral:
		source := eosOperand(expression.Token.Lexeme)
		known := source
		return checkedExpression{source: source, typ: compilerTypes.EoS, token: expression.Token, known: &known}
	case parser.StringLiteral:
		return checkStringLiteral(expression, context.expected.Type)
	case parser.ByteLiteral:
		return checkByteLiteral(expression)
	case parser.RuneLiteral:
		return checkRuneLiteral(expression)
	case parser.ObjectLiteral:
		return checkObjectLiteral(expression, context.expected.Type, environment, typeEnvironment)
	case parser.ArrayLiteralExpression:
		return checkArrayLiteral(expression, context.expected.Type, environment, typeEnvironment)
	case parser.QualifiedVariantExpression:
		return checkQualifiedVariant(expression, context.expected.Type, environment, typeEnvironment)
	case parser.MatchExpression:
		return checkMatchExpression(expression, context, environment, typeEnvironment)
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		if property, isProperty := expression.(parser.PropertyExpression); isProperty {
			if variable, isVariable := property.Receiver.(parser.VariableExpression); isVariable && property.Property.Kind == lexer.Identifier {
				if variable.Name.Lexeme == "FileMode" {
					// RFC 0040: the protected FileMode ADT's unit variants
					// resolve before any other lookup.
					if reference, diagnostic := checkFileModeVariant(variable.Name, property.Property); reference != nil || diagnostic != nil {
						if diagnostic != nil {
							return checkedExpression{token: property.Property, diagnostic: diagnostic}
						}
						return *reference
					}
				}
				if reference, diagnostic := checkUnitVariant(variable.Name, property.Property, context.expected.Type, environment, typeEnvironment); reference != nil || diagnostic != nil {
					if diagnostic != nil {
						return checkedExpression{token: property.Property, diagnostic: diagnostic}
					}
					return *reference
				}
			}
		}
		if variable, isVariable := expression.(parser.VariableExpression); isVariable && context.expected.Type.Signature != nil {
			if reference, diagnostic := checkGenericFunctionReference(variable.Name, context.expected.Type, environment, typeEnvironment); reference != nil || diagnostic != nil {
				if diagnostic != nil {
					return checkedExpression{token: variable.Name, diagnostic: diagnostic}
				}
				return *reference
			}
		}
		place := checkPlace(expression, environment, typeEnvironment)
		if place.diagnostic != nil {
			return place
		}
		return valueFromPlace(place)
	case parser.RefExpression:
		return checkReference(expression, environment, typeEnvironment)
	case parser.CallExpression:
		return checkCallValue(expression, environment, typeEnvironment)
	case parser.UnaryExpression:
		return checkUnaryExpression(expression, context, environment, typeEnvironment)
	case parser.SpawnExpression:
		return checkSpawnExpression(expression, environment, typeEnvironment)
	case parser.TryExpression:
		return checkTryExpression(expression, context, environment, typeEnvironment)
	case parser.BinaryExpression:
		return checkBinaryExpression(expression, context, environment, typeEnvironment)
	case parser.TypeTestExpression:
		return checkUnionTypeTest(expression, environment, typeEnvironment)
	default:
		return checkedExpression{
			diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Message:  "unsupported expression",
			},
		}
	}
}

func checkedFromInitializer(initializer initializerValue) checkedExpression {
	if initializer.known == nil && initializer.source.Kind == ConstantOperand {
		known := initializer.source
		initializer.known = &known
	}
	return initializer
}

func checkUnaryExpression(expression parser.UnaryExpression, context expressionContext, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	operator, ok := operatorFromToken(expression.Operator)
	if expression.Operator.Kind == lexer.Minus {
		operator = NegateOperator
		ok = true
	}
	if !ok {
		return unsupportedOperatorExpression(expression.Operator)
	}

	hint := inferExpressionType(expression.Operand, operandContextType(operator, context.expected.Type), environment, typeEnvironment)
	if hint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: hint.diagnostic}
	}
	operandType := hint.typ
	if hint.contextual {
		if expected := operandContextType(operator, context.expected.Type); expected.Name != "" {
			operandType = expected
		}
	}
	operand := checkExpression(expression.Operand, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: context.foldConstants}, environment, typeEnvironment)
	if diagnostics := initializerDiagnostics(operand); len(diagnostics) > 0 {
		return checkedExpression{token: expression.Operator, diagnostics: diagnostics}
	}
	if environment.generics != nil && environment.generics.open && compilerTypes.ContainsTypeParameter(operand.typ) {
		// RFC 0019: an operation whose validity depends on a substituted type
		// is deferred during open generic checking and rechecked at
		// specialization with concrete types.
		return operationUnaryResult(operator, operand, operand.typ, operand.typ, expression.Operator)
	}
	if !operatorAllowsType(operator, operand.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: unaryOperatorDiagnostic(operator, operand.typ, expression.Operator),
		}
	}

	resultType := operand.typ
	if operator == LogicalNotOperator {
		resultType = compilerTypes.Bool
	}
	return foldUnary(operator, operand, operand.typ, resultType, expression.Operator, context.foldConstants)
}

func checkBinaryExpression(expression parser.BinaryExpression, context expressionContext, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	operator, ok := operatorFromToken(expression.Operator)
	if !ok || operator == NegateOperator || operator == LogicalNotOperator {
		return unsupportedOperatorExpression(expression.Operator)
	}

	expected := operandContextType(operator, context.expected.Type)
	leftHint := inferExpressionType(expression.Left, expected, environment, typeEnvironment)
	rightHint := inferExpressionType(expression.Right, expected, environment, typeEnvironment)
	if leftHint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: leftHint.diagnostic}
	}
	if rightHint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: rightHint.diagnostic}
	}
	operandType := binaryOperandType(operator, expected, leftHint, rightHint)
	left := checkExpression(expression.Left, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: context.foldConstants}, environment, typeEnvironment)
	rightEvaluation := context.foldConstants
	if rightEvaluation && (operator == LogicalAndOperator || operator == LogicalOrOperator) {
		if leftValue, known := knownTruthiness(left); known {
			rightEvaluation = (operator == LogicalAndOperator && leftValue) || (operator == LogicalOrOperator && !leftValue)
		}
	}
	right := checkExpression(expression.Right, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: rightEvaluation}, environment, typeEnvironment)
	diagnostics := append(initializerDiagnostics(left), initializerDiagnostics(right)...)
	if len(diagnostics) > 0 {
		return checkedExpression{token: expression.Operator, diagnostics: diagnostics}
	}

	// RFC 0010 null tests own the == and != pairs that mention Nil: a null
	// test yields Bool, while pairs without a Nil side stay with ordinary
	// scalar equality below.
	if operator == EqualOperator || operator == NotEqualOperator {
		// RFC 0031: EoS is a singleton, so eos == eos is provably true and
		// eos != eos is provably false, matching nil == nil.
		if compilerTypes.IsEoS(left.typ) && compilerTypes.IsEoS(right.typ) {
			result := foldedBoolResult(operator == EqualOperator, expression.Operator)
			return result
		}
		if result := checkNullTest(operator, left, right, expression.Operator); result != nil {
			return *result
		}
		if result := checkUnionEquality(operator, left, right, expression.Operator); result != nil {
			return *result
		}
	}

	if environment.generics != nil && environment.generics.open &&
		(compilerTypes.ContainsTypeParameter(left.typ) || compilerTypes.ContainsTypeParameter(right.typ)) {
		// RFC 0019: an operation whose validity depends on a substituted type
		// is deferred during open generic checking and rechecked at
		// specialization with concrete types.
		resultType := left.typ
		if operator == EqualOperator || operator == NotEqualOperator ||
			operator == LessOperator || operator == LessEqualOperator ||
			operator == GreaterOperator || operator == GreaterEqualOperator ||
			operator == LogicalAndOperator || operator == LogicalOrOperator {
			resultType = compilerTypes.Bool
		}
		return operationBinaryResult(operator, left, right, left.typ, resultType, expression.Operator)
	}

	// RFC 0050: Rune is not an arithmetic operand. Reject any binary
	// arithmetic with a Rune operand before folding or common-type selection,
	// owning same-type and mixed cases with one diagnostic at the operator.
	if (operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator ||
		operator == DivideOperator || operator == RemainderOperator) &&
		(compilerTypes.IsRune(left.typ) || compilerTypes.IsRune(right.typ)) {
		diagnostic := typeErrorAt(expression.Operator, fmt.Sprintf("operator %s requires numeric operands; got %s and %s", operator, left.typ.Name, right.typ.Name))
		return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
	}

	// RFC 0016/0017: mixed numeric arithmetic selects the unique least
	// lossless common type before the operation; the result has that type
	// and wraps at it.
	if (operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator ||
		operator == DivideOperator || operator == RemainderOperator) &&
		(compilerTypes.IsInteger(left.typ) || compilerTypes.IsFloat(left.typ)) &&
		(compilerTypes.IsInteger(right.typ) || compilerTypes.IsFloat(right.typ)) &&
		!compilerTypes.Equal(left.typ, right.typ) {
		common, ok := compilerTypes.LosslessCommonType(left.typ, right.typ)
		if !ok {
			diagnostic := typeErrorAt(expression.Operator, "numeric values have no unique lossless common type")
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			return foldWidenedArithmetic(operator, left.source, right.source, common, expression.Operator)
		}
		leftNode := widenNode(expressionNode(left.source), left.typ, common)
		rightNode := widenNode(expressionNode(right.source), right.typ, common)
		if operator == DivideOperator || operator == RemainderOperator {
			if diagnostic := staticDivisionDiagnostic(operator, left.source, right.source, common, expression.Operator); diagnostic != nil {
				return checkedExpression{typ: common, token: expression.Operator, diagnostic: diagnostic}
			}
		}
		node := operationBinaryNode(operator, leftNode, rightNode, common, common)
		source := Operand{Kind: ExpressionOperand, Type: common, Node: node}
		return checkedExpression{source: source, typ: common, token: expression.Operator}
	}

	// RFC 0032: bitwise &, ^, and | use the unique least lossless common
	// integer type; the operation happens at that exact width and wraps at
	// it.
	if isBitwiseArithmetic(operator) {
		if !isBitwiseEligible(left.typ) || !isBitwiseEligible(right.typ) {
			diagnostic := typeErrorAt(expression.Operator, fmt.Sprintf("operator %s requires integer operands; got %s and %s", operator, left.typ.Name, right.typ.Name))
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		common := left.typ
		if !compilerTypes.Equal(left.typ, right.typ) {
			var ok bool
			common, ok = compilerTypes.LosslessCommonType(left.typ, right.typ)
			if !ok {
				diagnostic := typeErrorAt(expression.Operator, "integer operands have no unique lossless common type")
				return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
			}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			return foldWidenedArithmetic(operator, left.source, right.source, common, expression.Operator)
		}
		leftNode := widenNode(expressionNode(left.source), left.typ, common)
		rightNode := widenNode(expressionNode(right.source), right.typ, common)
		node := operationBinaryNode(operator, leftNode, rightNode, common, common)
		source := Operand{Kind: ExpressionOperand, Type: common, Node: node}
		return checkedExpression{source: source, typ: common, token: expression.Operator}
	}

	// RFC 0032: shifts preserve the left operand's type; the count is any
	// integer and never participates in common-type selection.
	if isShiftOperator(operator) {
		if !isBitwiseEligible(left.typ) {
			diagnostic := typeErrorAt(expression.Operator, fmt.Sprintf("operator %s requires an integer left operand; got %s", operator, left.typ.Name))
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		if !compilerTypes.IsInteger(right.typ) {
			diagnostic := typeErrorAt(expression.Operator, "shift count must be an integer; got "+right.typ.Name)
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		if right.source.Kind == ConstantOperand && right.source.Constant != nil {
			count, exact := constant.Int64Val(right.source.Constant)
			if exact && (count < 0 || count >= int64(left.typ.Bits)) {
				diagnostic := typeErrorAt(expression.Operator, fmt.Sprintf("shift count %d is outside the valid range for %s", count, left.typ.Name))
				return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
			}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			operation, ok := integerConstantOperator(operator)
			if ok && left.source.Constant != nil && right.source.Constant != nil {
				// go/constant shifts require a uint count; the range
				// validation above guarantees the value fits.
				countValue, _ := constant.Uint64Val(right.source.Constant)
				value := constant.Shift(left.source.Constant, operation, uint(countValue))
				value = wrapIntegerConstant(value, left.typ)
				return foldedIntegerResult(value, left.typ, expression.Operator)
			}
		}
		node := operationBinaryNode(operator, expressionNode(left.source), expressionNode(right.source), left.typ, left.typ)
		source := Operand{Kind: ExpressionOperand, Type: left.typ, Node: node}
		return checkedExpression{source: source, typ: left.typ, token: expression.Operator}
	}

	// RFC 0024: equality and ordering resolve through the lossless numeric
	// widening and the recursive deep-comparison rules before the ordinary
	// identical-scalar path.
	if operator == EqualOperator || operator == NotEqualOperator ||
		operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator {
		if result := checkDeepComparison(operator, left, right, expression.Operator, environment); result != nil {
			return *result
		}
	}

	if !operatorAllowsType(operator, left.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: binaryOperatorDiagnostic(operator, left.typ, expression.Operator),
		}
	}
	if !operatorAllowsType(operator, right.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: binaryOperatorDiagnostic(operator, right.typ, expression.Operator),
		}
	}
	if !compilerTypes.Equal(left.typ, right.typ) && operator != LogicalAndOperator && operator != LogicalOrOperator {
		return checkedExpression{
			token: expression.Operator,
			diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     expression.Operator.Line,
				Column:   expression.Operator.Column,
				Message:  fmt.Sprintf("operator %s requires identical operand types; got %s and %s", operator, left.typ.Name, right.typ.Name),
			},
		}
	}
	resultType := left.typ
	if operator == EqualOperator || operator == NotEqualOperator ||
		operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator ||
		operator == LogicalAndOperator || operator == LogicalOrOperator {
		resultType = compilerTypes.Bool
	}
	return foldBinary(operator, left, right, left.typ, resultType, expression.Operator, context.foldConstants)
}

// checkNullTest resolves RFC 0010/0014 null tests: == and != accept exactly the
// operand pairs where one side is Nil and the other is any union containing
// Nil. The result is Bool, and the checked node is normalized so the union
// operand always sits in the node's Operand slot. Pairs without a Nil side
// return nil so ordinary equality keeps its own rules.
func checkNullTest(operator Operator, left, right checkedExpression, token lexer.Token) *checkedExpression {
	if !compilerTypes.IsNil(left.typ) && !compilerTypes.IsNil(right.typ) {
		return nil
	}
	var operand checkedExpression
	switch {
	case compilerTypes.IsNil(left.typ) && compilerTypes.IsNil(right.typ):
		// Nil is a singleton, so nil == nil is provably true and nil != nil
		// is provably false. This is the only folded null-test case; a test
		// against a nullable operand always stays runtime.
		result := foldedBoolResult(operator == EqualOperator, token)
		return &result
	case compilerTypes.IsEoS(left.typ) && compilerTypes.IsEoS(right.typ):
		// RFC 0031: EoS is a singleton, so eos == eos is provably true and
		// eos != eos is provably false, matching nil == nil.
		result := foldedBoolResult(operator == EqualOperator, token)
		return &result
	case compilerTypes.IsNil(left.typ):
		operand = right
	default:
		operand = left
	}
	if !compilerTypes.IsUnion(operand.typ) || !compilerTypes.ContainsUnionMember(operand.typ, compilerTypes.Nil) {
		// The C habit of null-checking every pointer produces this diagnostic
		// often during migration, so it names the reason: the operand's type
		// can never hold Nil, which makes the test a constant result.
		verdict := "true"
		if operator == EqualOperator {
			verdict = "false"
		}
		diagnostic := typeErrorAt(token, fmt.Sprintf("%s is never Nil; the test is always %s", operand.typ.Name, verdict))
		return &checkedExpression{token: token, diagnostic: &diagnostic}
	}
	operandNode := expressionNode(operand.source)
	node := Expression{
		Kind:        NullTestExpression,
		Operand:     &operandNode,
		Operator:    operator,
		OperandType: operand.typ,
		ResultType:  compilerTypes.Bool,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
	return &checkedExpression{source: source, typ: compilerTypes.Bool, token: token}
}

func foldUnary(operator Operator, operand checkedExpression, operandType, resultType compilerTypes.Type, token lexer.Token, evaluate bool) checkedExpression {
	runtime := operationUnaryResult(operator, operand, operandType, resultType, token)
	if !evaluate || operand.source.Kind != ConstantOperand {
		return runtime
	}

	switch operator {
	case NegateOperator:
		if compilerTypes.IsInteger(operandType) && operand.source.Constant != nil {
			value := constant.UnaryOp(gotoken.SUB, operand.source.Constant, 0)
			minimum, maximum := integerBounds(operandType)
			if constant.Compare(value, gotoken.LSS, minimum) || constant.Compare(value, gotoken.GTR, maximum) {
				return checkedExpression{typ: resultType, token: token, diagnostic: valueOutOfRangeDiagnostic(token, operandType)}
			}
			return foldedIntegerResult(value, resultType, token)
		}
		if compilerTypes.IsFloat(operandType) {
			bits := operand.source.FloatBits
			if compilerTypes.Equal(operandType, compilerTypes.Float32) {
				bits ^= uint64(1) << 31
			} else {
				bits ^= uint64(1) << 63
			}
			return foldedFloatResult(resultType, bits, token)
		}
	case LogicalNotOperator:
		if value, known := knownTruthiness(operand); known {
			return foldedBoolResult(!value, token)
		}
	case BitwiseNotOperator:
		// RFC 0032: complement inverts every bit of the fixed-width
		// representation and reconstructs the signed value when needed.
		if compilerTypes.IsInteger(operandType) && operand.source.Constant != nil {
			width := uint(operandType.Bits)
			mask := constant.MakeUint64(0)
			if width >= 64 {
				mask = constant.MakeUint64(^uint64(0))
			} else {
				mask = constant.MakeUint64(uint64(1)<<width - 1)
			}
			value := constant.BinaryOp(operand.source.Constant, gotoken.XOR, mask)
			value = wrapIntegerConstant(value, operandType)
			return foldedIntegerResult(value, resultType, token)
		}
	}
	return runtime
}

func foldBinary(operator Operator, left, right checkedExpression, operandType, resultType compilerTypes.Type, token lexer.Token, evaluate bool) checkedExpression {
	runtime := operationBinaryResult(operator, left, right, operandType, resultType, token)
	if !evaluate {
		return runtime
	}
	if diagnostic := staticDivisionDiagnostic(operator, left.source, right.source, operandType, token); diagnostic != nil {
		return checkedExpression{typ: resultType, token: token, diagnostic: diagnostic}
	}

	if operator == LogicalAndOperator || operator == LogicalOrOperator {
		if leftValue, known := knownTruthiness(left); known {
			if operator == LogicalAndOperator && !leftValue {
				return foldedBoolResult(false, token)
			}
			if operator == LogicalOrOperator && leftValue {
				return foldedBoolResult(true, token)
			}
			if rightValue, known := knownTruthiness(right); known {
				return foldedBoolResult(rightValue, token)
			}
		}
		return runtime
	}

	if left.source.Kind != ConstantOperand || right.source.Kind != ConstantOperand {
		return runtime
	}

	switch {
	case compilerTypes.IsInteger(operandType) && (isIntegerArithmetic(operator) || isBitwiseArithmetic(operator) || isShiftOperator(operator)):
		operation, ok := integerConstantOperator(operator)
		if !ok || left.source.Constant == nil || right.source.Constant == nil {
			return runtime
		}
		value := left.source.Constant
		if isShiftOperator(operator) {
			// go/constant shifts require a uint count; the checker
			// validated the count range before folding.
			countValue, _ := constant.Uint64Val(right.source.Constant)
			value = constant.Shift(left.source.Constant, operation, uint(countValue))
		} else {
			value = constant.BinaryOp(left.source.Constant, operation, right.source.Constant)
		}
		// RFC 0017: integer arithmetic wraps to the result type; the
		// signed-minimum/-1 division and remainder pairs fold to their
		// defined values.
		if operator == DivideOperator || operator == RemainderOperator {
			if compilerTypes.IsSignedInteger(operandType) {
				minimum, _ := integerBounds(operandType)
				if constant.Compare(left.source.Constant, gotoken.EQL, minimum) &&
					constant.Compare(right.source.Constant, gotoken.EQL, constant.MakeInt64(-1)) {
					if operator == RemainderOperator {
						value = constant.MakeInt64(0)
					} else {
						value = minimum
					}
				}
			}
		}
		value = wrapIntegerConstant(value, operandType)
		return foldedIntegerResult(value, resultType, token)
	case compilerTypes.IsFloat(operandType) && isFloatArithmetic(operator):
		return foldedFloatResultFromBinary(operator, left.source, right.source, resultType, token)
	case operator == EqualOperator || operator == NotEqualOperator ||
		operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator:
		value, ok := compareConstantOperands(operator, left.source, right.source, operandType)
		if ok {
			return foldedBoolResult(value, token)
		}
	}
	return runtime
}

func operationUnaryResult(operator Operator, operand checkedExpression, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	node := operationUnaryNode(operator, expressionNode(operand.source), operandType, resultType)
	source := Operand{Kind: ExpressionOperand, Type: resultType, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

func operationBinaryResult(operator Operator, left, right checkedExpression, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	node := operationBinaryNode(operator, expressionNode(left.source), expressionNode(right.source), operandType, resultType)
	source := Operand{Kind: ExpressionOperand, Type: resultType, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

func foldedIntegerResult(value constant.Value, typ compilerTypes.Type, token lexer.Token) checkedExpression {
	source := constantOperand(typ, value, value.ExactString())
	source.Negative = constant.Sign(value) < 0
	source.Node = constantNode(source)
	known := source
	return checkedExpression{source: source, typ: typ, token: token, known: &known}
}

// wrapIntegerConstant reduces an exact integer result to the result type's
// range using the defined two's-complement-style wrapping rule (RFC 0017).
func wrapIntegerConstant(value constant.Value, typ compilerTypes.Type) constant.Value {
	minimum, maximum := integerBounds(typ)
	if constant.Compare(value, gotoken.GEQ, minimum) && constant.Compare(value, gotoken.LEQ, maximum) {
		return value
	}
	width := uint(typ.Bits)
	modulus := constant.MakeUint64(0)
	if width >= 64 {
		modulus = constant.MakeUint64(^uint64(0))
	} else {
		modulus = constant.MakeUint64(uint64(1) << width)
	}
	// Reduce modulo 2^n into [0, 2^n).
	reduced := constant.BinaryOp(value, gotoken.REM, modulus)
	if constant.Compare(reduced, gotoken.LSS, constant.MakeInt64(0)) {
		reduced = constant.BinaryOp(reduced, gotoken.ADD, modulus)
	}
	if !compilerTypes.IsSignedInteger(typ) {
		return reduced
	}
	half := constant.MakeUint64(0)
	if width >= 64 {
		half = constant.MakeUint64(uint64(1) << 63)
	} else {
		half = constant.MakeUint64(uint64(1) << (width - 1))
	}
	if constant.Compare(reduced, gotoken.LSS, half) {
		return reduced
	}
	return constant.BinaryOp(reduced, gotoken.SUB, modulus)
}

// foldWidenedArithmetic folds a mixed-type constant arithmetic operation
// after selecting the common numeric type.
func foldWidenedArithmetic(operator Operator, left, right Operand, common compilerTypes.Type, token lexer.Token) checkedExpression {
	if operator == DivideOperator || operator == RemainderOperator {
		if diagnostic := staticDivisionDiagnostic(operator, left, right, common, token); diagnostic != nil {
			return checkedExpression{typ: common, token: token, diagnostic: diagnostic}
		}
	}
	operation, ok := integerConstantOperator(operator)
	if !ok || left.Constant == nil || right.Constant == nil {
		return checkedExpression{typ: common, token: token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "unfoldable widened arithmetic",
		}}
	}
	value := constant.BinaryOp(left.Constant, operation, right.Constant)
	if operator == DivideOperator || operator == RemainderOperator && compilerTypes.IsSignedInteger(common) {
		minimum, _ := integerBounds(common)
		if constant.Compare(left.Constant, gotoken.EQL, minimum) && constant.Compare(right.Constant, gotoken.EQL, constant.MakeInt64(-1)) {
			if operator == RemainderOperator {
				value = constant.MakeInt64(0)
			} else {
				value = minimum
			}
		}
	}
	value = wrapIntegerConstant(value, common)
	return foldedIntegerResult(value, common, token)
}

func foldedBoolResult(value bool, token lexer.Token) checkedExpression {
	literal := "false"
	if value {
		literal = "true"
	}
	source := constantOperand(compilerTypes.Bool, constant.MakeBool(value), literal)
	source.Node = constantNode(source)
	known := source
	return checkedExpression{source: source, typ: compilerTypes.Bool, token: token, known: &known}
}

func foldedFloatResult(typ compilerTypes.Type, bits uint64, token lexer.Token) checkedExpression {
	var value constant.Value
	var negative bool
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		floatValue := math.Float32frombits(uint32(bits))
		negative = math.Signbit(float64(floatValue))
		if math.IsNaN(float64(floatValue)) || math.IsInf(float64(floatValue), 0) {
			value = constant.MakeUnknown()
		} else {
			value = constant.MakeFloat64(float64(floatValue))
		}
	} else {
		floatValue := math.Float64frombits(bits)
		negative = math.Signbit(floatValue)
		if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
			value = constant.MakeUnknown()
		} else {
			value = constant.MakeFloat64(floatValue)
		}
	}
	source := constantOperand(typ, value, "")
	source.FloatBits = bits
	source.Negative = negative
	source.Node = constantNode(source)
	known := source
	return checkedExpression{source: source, typ: typ, token: token, known: &known}
}

func foldedFloatResultFromBinary(operator Operator, left, right Operand, typ compilerTypes.Type, token lexer.Token) checkedExpression {
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		leftValue := math.Float32frombits(uint32(left.FloatBits))
		rightValue := math.Float32frombits(uint32(right.FloatBits))
		var result float32
		switch operator {
		case AddOperator:
			result = leftValue + rightValue
		case SubtractOperator:
			result = leftValue - rightValue
		case MultiplyOperator:
			result = leftValue * rightValue
		case DivideOperator:
			result = leftValue / rightValue
		default:
			return checkedExpression{source: Operand{Kind: ExpressionOperand, Type: typ}, typ: typ, token: token}
		}
		return foldedFloatResult(typ, uint64(math.Float32bits(result)), token)
	}

	leftValue := math.Float64frombits(left.FloatBits)
	rightValue := math.Float64frombits(right.FloatBits)
	var result float64
	switch operator {
	case AddOperator:
		result = leftValue + rightValue
	case SubtractOperator:
		result = leftValue - rightValue
	case MultiplyOperator:
		result = leftValue * rightValue
	case DivideOperator:
		result = leftValue / rightValue
	default:
		return checkedExpression{source: Operand{Kind: ExpressionOperand, Type: typ}, typ: typ, token: token}
	}
	return foldedFloatResult(typ, math.Float64bits(result), token)
}

// knownTruthiness reports whether the operand's truthiness is decided at
// compile time (RFC 0023): a constant Bool carries its value, nil is falsey,
// and a constant of an always-truthy type is truthy. Non-constant operands
// are never folded here — their evaluation must survive in the checked AST.
func knownTruthiness(expression checkedExpression) (bool, bool) {
	if expression.source.Kind != ConstantOperand {
		return false, false
	}
	switch compilerTypes.Truthiness(expression.typ) {
	case compilerTypes.TruthinessBool:
		if expression.source.Constant == nil || expression.source.Constant.Kind() != constant.Bool {
			return false, false
		}
		return constant.BoolVal(expression.source.Constant), true
	case compilerTypes.TruthinessAlwaysTrue:
		return true, true
	case compilerTypes.TruthinessNil:
		return false, true
	}
	return false, false
}

func isIntegerArithmetic(operator Operator) bool {
	return operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator || operator == DivideOperator || operator == RemainderOperator
}

func isBitwiseArithmetic(operator Operator) bool {
	return operator == BitwiseAndOperator || operator == BitwiseXorOperator || operator == BitwiseOrOperator
}

func isShiftOperator(operator Operator) bool {
	return operator == ShiftLeftOperator || operator == ShiftRightOperator
}

func isFloatArithmetic(operator Operator) bool {
	return operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator || operator == DivideOperator
}

func integerConstantOperator(operator Operator) (gotoken.Token, bool) {
	switch operator {
	case AddOperator:
		return gotoken.ADD, true
	case SubtractOperator:
		return gotoken.SUB, true
	case MultiplyOperator:
		return gotoken.MUL, true
	case DivideOperator:
		return gotoken.QUO_ASSIGN, true
	case RemainderOperator:
		return gotoken.REM, true
	case BitwiseAndOperator:
		return gotoken.AND, true
	case BitwiseXorOperator:
		return gotoken.XOR, true
	case BitwiseOrOperator:
		return gotoken.OR, true
	case ShiftLeftOperator:
		return gotoken.SHL, true
	case ShiftRightOperator:
		return gotoken.SHR, true
	default:
		return gotoken.ILLEGAL, false
	}
}

func compareConstantOperands(operator Operator, left, right Operand, typ compilerTypes.Type) (bool, bool) {
	if compilerTypes.Equal(typ, compilerTypes.Bool) {
		if left.Constant == nil || right.Constant == nil || left.Constant.Kind() != constant.Bool || right.Constant.Kind() != constant.Bool {
			return false, false
		}
		leftValue := constant.BoolVal(left.Constant)
		rightValue := constant.BoolVal(right.Constant)
		switch operator {
		case EqualOperator:
			return leftValue == rightValue, true
		case NotEqualOperator:
			return leftValue != rightValue, true
		default:
			return false, false
		}
	}
	if compilerTypes.IsFloat(typ) {
		return compareFloatOperands(operator, left.FloatBits, right.FloatBits, typ), true
	}
	if !compilerTypes.IsInteger(typ) || left.Constant == nil || right.Constant == nil {
		return false, false
	}
	comparison := func(token gotoken.Token) bool {
		return constant.Compare(left.Constant, token, right.Constant)
	}
	switch operator {
	case EqualOperator:
		return comparison(gotoken.EQL), true
	case NotEqualOperator:
		return comparison(gotoken.NEQ), true
	case LessOperator:
		return comparison(gotoken.LSS), true
	case LessEqualOperator:
		return comparison(gotoken.LEQ), true
	case GreaterOperator:
		return comparison(gotoken.GTR), true
	case GreaterEqualOperator:
		return comparison(gotoken.GEQ), true
	default:
		return false, false
	}
}

func compareFloatOperands(operator Operator, leftBits, rightBits uint64, typ compilerTypes.Type) bool {
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		left := math.Float32frombits(uint32(leftBits))
		right := math.Float32frombits(uint32(rightBits))
		switch operator {
		case EqualOperator:
			return left == right
		case NotEqualOperator:
			return left != right
		case LessOperator:
			return left < right
		case LessEqualOperator:
			return left <= right
		case GreaterOperator:
			return left > right
		case GreaterEqualOperator:
			return left >= right
		}
		return false
	}
	left := math.Float64frombits(leftBits)
	right := math.Float64frombits(rightBits)
	switch operator {
	case EqualOperator:
		return left == right
	case NotEqualOperator:
		return left != right
	case LessOperator:
		return left < right
	case LessEqualOperator:
		return left <= right
	case GreaterOperator:
		return left > right
	case GreaterEqualOperator:
		return left >= right
	default:
		return false
	}
}

func staticDivisionDiagnostic(operator Operator, left, right Operand, operandType compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	if (operator != DivideOperator && operator != RemainderOperator) || !compilerTypes.IsInteger(operandType) || right.Kind != ConstantOperand || right.Constant == nil || right.Constant.Kind() == constant.Unknown {
		return nil
	}
	if constant.Sign(right.Constant) == 0 {
		return &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     token.Line,
			Column:   token.Column,
			Message:  "division by zero",
		}
	}
	// RFC 0017 supersedes RFC 0009's rejection of signed minimum divided by
	// -1: the quotient wraps to the signed minimum and the remainder is
	// zero, both at compile time and at runtime.
	return nil
}

func isSignedMinimum(source Operand, typ compilerTypes.Type) bool {
	if !compilerTypes.IsSignedInteger(typ) || source.Kind != ConstantOperand || source.Constant == nil || source.Constant.Kind() == constant.Unknown {
		return false
	}
	minimum, _ := integerBounds(typ)
	return constant.Compare(source.Constant, gotoken.EQL, minimum)
}

func isNegativeOne(source Operand) bool {
	return source.Kind == ConstantOperand && source.Constant != nil && source.Constant.Kind() != constant.Unknown && constant.Compare(source.Constant, gotoken.EQL, constant.MakeInt64(-1))
}

func inferExpressionType(expression parser.Expression, expected compilerTypes.Type, environment *scope, typeEnvironment *compilerTypes.Environment) expressionTypeHint {
	switch expression := expression.(type) {
	case parser.IntegerLiteral:
		return expressionTypeHint{typ: contextualIntegerType(expected), contextual: true, token: expression.Token}
	case parser.DecimalLiteral:
		return expressionTypeHint{typ: contextualFloatType(expected), contextual: true, token: expression.Token}
	case parser.NegatedNumericLiteral:
		return expressionTypeHint{typ: negatedLiteralType(expression, expected), contextual: true, token: expression.Minus}
	case parser.BooleanLiteral:
		return expressionTypeHint{typ: compilerTypes.Bool, token: expression.Token}
	case parser.NilLiteral:
		return expressionTypeHint{typ: compilerTypes.Nil, token: expression.Token}
	case parser.StringLiteral:
		// RFC 0044: a literal in an expression position is String unless the
		// context demands a Strand.
		typ := compilerTypes.StringType
		if compilerTypes.IsStrand(expected) {
			typ = compilerTypes.StrandType
		}
		return expressionTypeHint{typ: typ, token: expression.Token}
	case parser.ByteLiteral:
		return expressionTypeHint{typ: compilerTypes.UInt8, token: expression.Token}
	case parser.RuneLiteral:
		return expressionTypeHint{typ: compilerTypes.Rune, token: expression.Token}
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		if property, isProperty := expression.(parser.PropertyExpression); isProperty {
			if variable, isVariable := property.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "FileMode" {
				// RFC 0040: the FileMode variants resolve before place
				// lookup in every expression position.
				if reference, _ := checkFileModeVariant(variable.Name, property.Property); reference != nil {
					return expressionTypeHint{typ: reference.typ, token: reference.token}
				}
			}
		}
		place := checkPlace(expression, environment, typeEnvironment)
		return expressionTypeHint{typ: place.typ, token: place.token, diagnostic: place.diagnostic}
	case parser.ObjectLiteral:
		typ, ok := typeEnvironment.Lookup(expression.TypeName.Lexeme)
		if !ok {
			return expressionTypeHint{token: expression.TypeName, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     expression.TypeName.Line,
				Column:   expression.TypeName.Column,
				Message:  "unknown type " + expression.TypeName.Lexeme,
			}}
		}
		return expressionTypeHint{typ: typ, token: expression.TypeName}
	case parser.RefExpression:
		checked := checkReference(expression, environment, typeEnvironment)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.CallExpression:
		checked := checkCallValue(expression, environment, typeEnvironment)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.UnaryExpression:
		operator, ok := operatorFromToken(expression.Operator)
		if expression.Operator.Kind == lexer.Minus {
			operator = NegateOperator
			ok = true
		}
		if !ok {
			return expressionTypeHint{token: expression.Operator, diagnostic: unsupportedOperatorDiagnostic(expression.Operator)}
		}
		hint := inferExpressionType(expression.Operand, operandContextType(operator, expected), environment, typeEnvironment)
		if hint.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: hint.diagnostic}
		}
		return expressionTypeHint{
			typ:        unaryResultType(operator, hint.typ),
			contextual: hint.contextual,
			token:      expression.Operator,
		}
	case parser.SpawnExpression:
		checked := checkSpawnExpression(expression, environment, typeEnvironment)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.TryExpression:
		// The try's true result is its checked success type; for literal
		// contextual typing the operand's hint is the closest estimate.
		hint := inferExpressionType(expression.Operand, expected, environment, typeEnvironment)
		if hint.diagnostic != nil {
			return expressionTypeHint{token: expression.Keyword, diagnostic: hint.diagnostic}
		}
		return expressionTypeHint{typ: hint.typ, token: expression.Keyword}
	case parser.BinaryExpression:
		operator, ok := operatorFromToken(expression.Operator)
		if !ok || operator == NegateOperator || operator == LogicalNotOperator {
			return expressionTypeHint{token: expression.Operator, diagnostic: unsupportedOperatorDiagnostic(expression.Operator)}
		}
		operandExpected := operandContextType(operator, expected)
		left := inferExpressionType(expression.Left, operandExpected, environment, typeEnvironment)
		right := inferExpressionType(expression.Right, operandExpected, environment, typeEnvironment)
		if left.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: left.diagnostic}
		}
		if right.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: right.diagnostic}
		}
		operandType := binaryOperandType(operator, operandExpected, left, right)
		return expressionTypeHint{
			typ:        binaryResultType(operator, operandType),
			contextual: left.contextual && right.contextual,
			token:      expression.Operator,
		}
	default:
		return expressionTypeHint{diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Message:  "unsupported expression",
		}}
	}
}

func operatorFromToken(token lexer.Token) (Operator, bool) {
	switch token.Kind {
	case lexer.Minus:
		return SubtractOperator, true
	case lexer.Plus:
		return AddOperator, true
	case lexer.Star:
		return MultiplyOperator, true
	case lexer.Slash:
		return DivideOperator, true
	case lexer.Percent:
		return RemainderOperator, true
	case lexer.EqualEqual:
		return EqualOperator, true
	case lexer.BangEqual:
		return NotEqualOperator, true
	case lexer.Less:
		return LessOperator, true
	case lexer.LessEqual:
		return LessEqualOperator, true
	case lexer.Greater:
		return GreaterOperator, true
	case lexer.GreaterEqual:
		return GreaterEqualOperator, true
	case lexer.And:
		return LogicalAndOperator, true
	case lexer.Or:
		return LogicalOrOperator, true
	case lexer.Bang:
		return LogicalNotOperator, true
	case lexer.Tilde:
		return BitwiseNotOperator, true
	case lexer.Amp:
		return BitwiseAndOperator, true
	case lexer.Caret:
		return BitwiseXorOperator, true
	case lexer.Pipe:
		return BitwiseOrOperator, true
	case lexer.ShiftLeft:
		return ShiftLeftOperator, true
	case lexer.ShiftRight:
		return ShiftRightOperator, true
	default:
		return InvalidOperator, false
	}
}

func operatorAllowsType(operator Operator, typ compilerTypes.Type) bool {
	switch operator {
	case AddOperator, SubtractOperator, MultiplyOperator, DivideOperator:
		return compilerTypes.IsInteger(typ) || compilerTypes.IsFloat(typ)
	case RemainderOperator:
		return compilerTypes.IsInteger(typ)
	case BitwiseAndOperator, BitwiseXorOperator, BitwiseOrOperator:
		return isBitwiseEligible(typ)
	case ShiftLeftOperator, ShiftRightOperator:
		return isBitwiseEligible(typ)
	case BitwiseNotOperator:
		return isBitwiseEligible(typ)
	case EqualOperator, NotEqualOperator:
		return typ.ScalarKind != compilerTypes.ScalarNone
	case LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator:
		return compilerTypes.IsInteger(typ) || compilerTypes.IsFloat(typ)
	case LogicalAndOperator, LogicalOrOperator, LogicalNotOperator:
		// RFC 0023: any value-producing operand is allowed; truthiness is
		// contextual, never a Bool requirement.
		return true
	case NegateOperator:
		return compilerTypes.IsSignedInteger(typ) || compilerTypes.IsFloat(typ)
	default:
		return false
	}
}

// isBitwiseEligible reports whether typ may participate in RFC 0032 bitwise
// and shift operations: a fixed-width integer or Size, excluding Rune.
func isBitwiseEligible(typ compilerTypes.Type) bool {
	return compilerTypes.IsInteger(typ) && !compilerTypes.IsRune(typ)
}

func operandContextType(operator Operator, expected compilerTypes.Type) compilerTypes.Type {
	if expected.Name == "" {
		return compilerTypes.Type{}
	}
	switch operator {
	case EqualOperator, NotEqualOperator, LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator, LogicalAndOperator, LogicalOrOperator, LogicalNotOperator:
		// A result Bool is not a numeric operand context, and RFC 0023 makes
		// logical operands truthiness contexts. Nested arithmetic therefore
		// keeps RFC 0003's fallback instead of becoming Bool.
		return compilerTypes.Type{}
	default:
		if operatorAllowsType(operator, expected) {
			return expected
		}
		return compilerTypes.Type{}
	}
}

func binaryOperandType(operator Operator, expected compilerTypes.Type, left, right expressionTypeHint) compilerTypes.Type {
	if left.contextual && !right.contextual && operatorAllowsType(operator, right.typ) {
		return right.typ
	}
	if right.contextual && !left.contextual && operatorAllowsType(operator, left.typ) {
		return left.typ
	}
	if left.contextual && right.contextual && operatorAllowsType(operator, expected) {
		return expected
	}
	return left.typ
}

func unaryResultType(operator Operator, operandType compilerTypes.Type) compilerTypes.Type {
	if operator == LogicalNotOperator {
		return compilerTypes.Bool
	}
	return operandType
}

func binaryResultType(operator Operator, operandType compilerTypes.Type) compilerTypes.Type {
	switch operator {
	case EqualOperator, NotEqualOperator, LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator, LogicalAndOperator, LogicalOrOperator:
		return compilerTypes.Bool
	default:
		return operandType
	}
}

func negatedLiteralType(expression parser.NegatedNumericLiteral, expected compilerTypes.Type) compilerTypes.Type {
	switch expression.Literal.(type) {
	case parser.IntegerLiteral:
		return contextualIntegerType(expected)
	case parser.DecimalLiteral:
		return contextualFloatType(expected)
	default:
		return compilerTypes.Int32
	}
}

func expressionNode(source Operand) Expression {
	if source.Node.Kind != InvalidExpression {
		return source.Node
	}
	return constantNode(source)
}

func unsupportedOperatorExpression(token lexer.Token) checkedExpression {
	return checkedExpression{token: token, diagnostic: unsupportedOperatorDiagnostic(token)}
}

func unsupportedOperatorDiagnostic(token lexer.Token) *compilerTypes.Diagnostic {
	return &compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  "unsupported operator " + token.Lexeme,
	}
}

func unaryOperatorDiagnostic(operator Operator, typ compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	message := fmt.Sprintf("operator %s requires Bool operands; got %s", operator, typ.Name)
	if operator == NegateOperator {
		message = fmt.Sprintf("negation requires a signed type; got %s", typ.Name)
	}
	if operator == BitwiseNotOperator {
		message = fmt.Sprintf("operator ~ requires an integer operand; got %s", typ.Name)
	}
	return &compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func binaryOperatorDiagnostic(operator Operator, typ compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	message := fmt.Sprintf("operator %s requires numeric operands; got %s", operator, typ.Name)
	switch operator {
	case RemainderOperator:
		message = fmt.Sprintf("operator %% requires integer operands; got %s", typ.Name)
	case LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator:
		message = fmt.Sprintf("operator %s requires ordered operands; got %s", operator, typ.Name)
	case LogicalAndOperator, LogicalOrOperator:
		message = fmt.Sprintf("operator %s requires Bool operands; got %s", operator, typ.Name)
	case EqualOperator, NotEqualOperator:
		message = fmt.Sprintf("operator %s requires scalar operands; got %s", operator, typ.Name)
	}
	return &compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

// checkPlace resolves only a syntactic place, tracking writability for the
// three-place walk so assignment and ref can read the binding and member modes.
func checkPlace(expression parser.Expression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	switch expression := expression.(type) {
	case parser.VariableExpression:
		if expression.Name.Kind == lexer.Self {
			return selfPlace(environment, expression.Name)
		}
		binding, status := environment.lookup(expression.Name.Lexeme)
		switch status {
		case nameMissing:
			return checkedExpression{
				token: expression.Name,
				diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError,
					Stage:    "checker",
					Line:     expression.Name.Line,
					Column:   expression.Name.Column,
					Message:  "unknown variable " + expression.Name.Lexeme,
				},
			}
		case nameModuleData:
			// RFC 0008 closed scopes: module storage lives in generated main
			// and is unreachable from a function body, Fun<...> bindings
			// included.
			diagnostic := moduleDataDiagnostic(environment.owner, expression.Name.Lexeme, expression.Name)
			return checkedExpression{token: expression.Name, diagnostic: &diagnostic}
		}
		if binding.kind == functionBinding {
			// A declared function is not storage: it is neither addressable nor
			// writable, and its name reads as the matching Fun<...> value.
			return checkedExpression{
				source: Operand{
					Kind: VariableOperand,
					Type: binding.typ,
					Name: expression.Name.Lexeme,
					Node: Expression{Kind: FunctionReferenceExpression, Name: expression.Name.Lexeme, ResultType: binding.typ},
				},
				typ:      binding.typ,
				token:    expression.Name,
				function: true,
			}
		}
		if binding.kind == genericFunctionBinding {
			// A generic function is not a value without a Fun<...> target to
			// infer its arguments from.
			return checkedExpression{token: expression.Name, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     expression.Name.Line,
				Column:   expression.Name.Column,
				Message:  "cannot infer generic parameter for " + expression.Name.Lexeme,
			}}
		}
		// Ordinary reads use the branch-local narrowed type when a null test
		// proved it; assignment and ref re-derive the declared storage type.
		placeType := binding.typ
		if narrowed, ok := environment.flow.narrowedType(binding.id); ok {
			placeType = narrowed
		}
		var narrowedVariant *compilerTypes.AdtVariant
		if variant, ok := environment.flow.narrowedVariant(binding.id); ok {
			narrowedVariant = variant
		}
		node := variableNodeWithBinding(expression.Name.Lexeme, binding.id)
		if binding.viewRootKind != ViewRootNone {
			node.ViewRoots = binding.viewRoots
			node.RootKind = binding.viewRootKind
		}
		return checkedExpression{
			source: Operand{
				Kind:        VariableOperand,
				Type:        placeType,
				Name:        expression.Name.Lexeme,
				Binding:     binding.id,
				Node:        node,
				Addressable: true,
				Writable:    binding.mutable,
			},
			typ:         placeType,
			use:         binding.use,
			storageType: binding.typ,
			variant:     narrowedVariant,
			token:       expression.Name,
			known:       binding.known,
			parameter:   binding.parameter,
			loopBinder:  binding.loopBinder,
		}
	case parser.PropertyExpression:
		// RFC 0034 Task 5: Alias.x with an import-alias receiver resolves x
		// against the target module's exported frame instead of the
		// property path: an exported unit variant first, then an exported
		// function reference.
		if variable, isVariable := expression.Receiver.(parser.VariableExpression); isVariable {
			if target, ok := environment.importAliasTarget(variable.Name.Lexeme); ok {
				return checkModuleQualifiedReference(expression, target, environment)
			}
		}
		var receiver checkedExpression
		if _, temporary := expression.Receiver.(parser.ObjectLiteral); temporary {
			receiver = checkValue(expression.Receiver, environment, typeEnvironment)
		} else {
			receiver = checkPlace(expression.Receiver, environment, typeEnvironment)
		}
		if receiver.diagnostic != nil {
			return receiver
		}
		if receiver.variant != nil {
			return variantPayloadPlace(receiver, expression.Property)
		}
		// RFC 0010: a nullable receiver has no members or .value until a null
		// test narrowed it to its pointer member. A bare binding names the
		// failing narrowing; a member path is never narrowable at all.
		if compilerTypes.IsNullable(receiver.typ) {
			diagnostic := nullableAccessDiagnostic(receiver, expression.Property, placeDescription(expression.Receiver))
			return checkedExpression{token: expression.Property, diagnostic: &diagnostic}
		}
		// RFC 0008 auto-dereference: on a pointer to an object, pointer.m means
		// pointer.value.m. One layer only, and the built-in .value property
		// wins, so an object member named value is reached as p.value.value.
		if receiver.typ.Element != nil && receiver.typ.Element.Object != nil && expression.Property.Lexeme != "value" {
			receiver = dereferencePlace(receiver, expression.Property)
		}
		if receiver.typ.Object != nil {
			member, ok := receiver.typ.Object.Member(expression.Property.Lexeme)
			if !ok {
				// Method rule 6: a method is code, not a member, so naming one
				// in a value position is a distinct error from a typo.
				if environment.methods.lookup(receiver.typ.Object, expression.Property.Lexeme) != nil {
					diagnostic := typeErrorAt(expression.Property,
						fmt.Sprintf("%s is a method on %s; methods are not values", expression.Property.Lexeme, receiver.typ.Object.Name))
					return checkedExpression{token: expression.Property, diagnostic: &diagnostic}
				}
				return checkedExpression{
					token:      expression.Property,
					diagnostic: missingMemberDiagnostic(receiver.typ, expression.Property),
				}
			}
			return checkedExpression{
				source: Operand{
					Kind:        VariableOperand,
					Type:        member.Type,
					Node:        memberNode(receiver.source.Node, member),
					Addressable: receiver.source.Addressable,
					Writable:    receiver.source.Writable && member.Mutable,
				},
				typ: member.Type,
				use: func() compilerTypes.TypeUse {
					if member.Use.Type == (compilerTypes.Type{}) {
						return compilerTypes.NewTypeUse(member.Type)
					}
					return member.Use
				}(),
				token: expression.Property,
			}
		}
		if receiver.typ.Element == nil || expression.Property.Lexeme != "value" {
			message := fmt.Sprintf("cannot access .%s on %s; expected Ptr<T> or an object member", expression.Property.Lexeme, receiver.typ.Name)
			if expression.Property.Lexeme == "value" {
				message = fmt.Sprintf("cannot access .value on %s; expected Ptr<T>", receiver.typ.Name)
			}
			if expression.Property.Lexeme == "addr" {
				message = "'.addr' is no longer supported; use 'ref'"
			}
			return checkedExpression{
				token: expression.Property,
				diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError,
					Stage:    "checker",
					Line:     expression.Property.Line,
					Column:   expression.Property.Column,
					Message:  message,
				},
			}
		}
		return dereferencePlace(receiver, expression.Property)
	case parser.IndexExpression:
		return checkIndexPlace(expression, environment, typeEnvironment)
	case parser.IntegerLiteral:
		initializer := integerInitializer(expression.Token, compilerTypes.Int32)
		return checkedExpression{source: initializer.source, typ: initializer.typ, token: initializer.token, diagnostic: initializer.diagnostic}
	case parser.DecimalLiteral:
		initializer := floatInitializer(expression.Token, compilerTypes.Float64)
		return checkedExpression{source: initializer.source, typ: initializer.typ, token: initializer.token, diagnostic: initializer.diagnostic}
	case parser.NegatedNumericLiteral:
		initializer := negatedInitializer(expression, compilerTypes.Type{})
		return checkedExpression{source: initializer.source, typ: initializer.typ, token: initializer.token, diagnostic: initializer.diagnostic}
	case parser.BooleanLiteral:
		return checkedExpression{source: constantOperand(compilerTypes.Bool, constant.MakeBool(expression.Token.Kind == lexer.True), expression.Token.Lexeme), typ: compilerTypes.Bool, token: expression.Token}
	case parser.ObjectLiteral:
		literal := checkObjectLiteral(expression, compilerTypes.Type{}, environment, typeEnvironment)
		return checkedExpression{source: literal.source, typ: literal.typ, token: literal.token, diagnostic: literal.diagnostic}
	default:
		return checkedExpression{
			diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Message:  "unsupported place",
			},
		}
	}
}

// checkModuleQualifiedReference resolves Alias.x in a value position where
// Alias is an import alias. The property resolves to the target module's
// exported unit variant when one exists, then to its exported function; any
// other name is the RFC 0034 Task 5 visibility failure.
func checkModuleQualifiedReference(expression parser.PropertyExpression, target string, environment *scope) checkedExpression {
	if adtType, variant, ok := environment.registry.findExportedADTVariant(target, expression.Property.Lexeme); ok {
		return adtUnitVariant(adtType, variant, expression.Property)
	}
	if function, ok := environment.registry.exportedFunction(target, expression.Property.Lexeme); ok {
		return checkedExpression{
			source: Operand{
				Kind: VariableOperand,
				Type: function.Type,
				Name: function.Name,
				Node: Expression{Kind: FunctionReferenceExpression, Name: function.Name, ResultType: function.Type, Module: target},
			},
			typ:      function.Type,
			token:    expression.Property,
			function: true,
		}
	}
	diagnostic := privateToModuleDiagnostic(expression.Property, expression.Property.Lexeme, target)
	return checkedExpression{token: expression.Property, diagnostic: &diagnostic}
}

// dereferencePlace walks one pointer layer, for both the explicit .value
// spelling and RFC 0008's inserted auto-dereference. Place rule case 3: the
// pointee is writable exactly when the receiver's pointer type has a writable
// pointee. Read the type, never a place mode carried by the pointer value.
func dereferencePlace(receiver checkedExpression, token lexer.Token) checkedExpression {
	if receiver.typ.Element != nil && compilerTypes.IsUnknown(*receiver.typ.Element) {
		diagnostic := typeErrorAt(token, receiver.typ.Name+" cannot be dereferenced; recover a concrete pointer type first")
		return checkedExpression{token: token, diagnostic: &diagnostic}
	}
	use := compilerTypes.NewTypeUse(*receiver.typ.Element)
	if receiver.use.Element != nil {
		use = *receiver.use.Element
	}
	return checkedExpression{
		source: Operand{
			Kind:        VariableOperand,
			Type:        *receiver.typ.Element,
			Node:        unaryNode(DereferenceExpression, receiver.source.Node),
			Addressable: true,
			Writable:    receiver.typ.PointeeWritable,
		},
		typ:   *receiver.typ.Element,
		use:   use,
		token: token,
	}
}

func valueFromPlace(place checkedExpression) checkedExpression {
	if place.storageType.Union != nil && !compilerTypes.IsUnion(place.typ) {
		memberIndex := -1
		for index, member := range compilerTypes.UnionMembers(place.storageType) {
			if compilerTypes.Equal(member, place.typ) {
				memberIndex = index
				break
			}
		}
		if memberIndex >= 0 {
			operandNode := place.source.Node
			node := Expression{
				Kind:        UnionPayloadExpression,
				Operand:     &operandNode,
				OperandType: place.storageType,
				ResultType:  place.typ,
				MemberIndex: memberIndex,
			}
			source := Operand{Kind: ExpressionOperand, Type: place.typ, Node: node}
			return checkedExpression{source: source, typ: place.typ, use: place.use, token: place.token}
		}
	}
	if place.known != nil {
		source := *place.known
		source.Addressable = false
		source.Writable = false
		return checkedExpression{source: source, typ: place.typ, use: place.use, token: place.token, known: place.known}
	}
	source := place.source
	source.Addressable = false
	source.Writable = false
	return checkedExpression{source: source, typ: place.typ, use: place.use, token: place.token}
}

// nullableAccessDiagnostic reports member or .value access through a nullable
// receiver that no null test narrowed. A bare local binding names the failing
// narrowing; a member path states the one-line workaround because member
// storage can be replaced through aliases the checker cannot see.
func nullableAccessDiagnostic(receiver checkedExpression, token lexer.Token, path string) compilerTypes.Diagnostic {
	if receiver.source.Node.Kind == VariableExpression {
		return typeErrorAt(token, fmt.Sprintf("%s may be Nil; narrow it before using .value", receiver.typ.Name))
	}
	return typeErrorAt(token, fmt.Sprintf("only a local binding can be narrowed; bind %s before testing it", path))
}

// checkReference types ref by the place's writability: a writable place
// yields MutPtr<T>, a fixed place yields Ptr<T>. There is no writability
// requirement; taking a read-only pointer to fixed storage is valid.
func checkReference(expression parser.RefExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	place := checkPlace(expression.Place, environment, typeEnvironment)
	if place.diagnostic != nil {
		return place
	}
	// Neither a function declaration nor a Fun<...> binding is addressable in
	// this RFC; the function's name already supplies the callable pointer.
	if place.function {
		diagnostic := typeErrorAt(place.token, "function declarations are not addressable; use "+place.token.Lexeme+" as a Fun value")
		return checkedExpression{token: place.token, diagnostic: &diagnostic}
	}
	if place.typ.Signature != nil {
		diagnostic := typeErrorAt(place.token, place.typ.Name+" bindings are not addressable")
		return checkedExpression{token: place.token, diagnostic: &diagnostic}
	}
	if place.typ.View != nil {
		diagnostic := typeErrorAt(place.token, "ref cannot take the address of a View binding")
		return checkedExpression{token: place.token, diagnostic: &diagnostic}
	}
	if place.typ.Atomic != nil {
		diagnostic := typeErrorAt(place.token, "Atomic values cannot be copied, assigned, addressed, or stored here")
		return checkedExpression{token: place.token, diagnostic: &diagnostic}
	}
	// ref names the binding's declared storage slot, not a narrowed read
	// type: the pointer must be able to observe every value the slot can
	// hold. A writable ref lets the slot's contents be replaced behind the
	// checker's back, so it escapes the binding and clears any narrowing.
	storageType := place.typ
	storageUse := place.use
	if variable, ok := expression.Place.(parser.VariableExpression); ok && place.source.Binding != 0 {
		if bound, status := environment.lookup(variable.Name.Lexeme); status == nameFound {
			storageType = bound.typ
			storageUse = bound.use
		}
	}
	if storageUse.Type == (compilerTypes.Type{}) {
		storageUse = compilerTypes.NewTypeUse(storageType)
	}
	ptrType := typeEnvironment.PtrType(storageType)
	if place.source.Writable {
		ptrType = typeEnvironment.MutPtrType(storageType)
		// ponytail: escape commits even when the surrounding statement later
		// fails. Over-conservative only inside an already-failing program;
		// deferring it would need escape to thread through every statement
		// shape. Safe direction: it can only block a later narrowing.
		if environment.flow != nil && place.source.Binding != 0 {
			environment.flow.escape(place.source.Binding)
		}
	}
	return checkedExpression{
		source: Operand{
			Kind:        VariableOperand,
			Type:        ptrType,
			Node:        unaryNode(AddressOfExpression, place.source.Node),
			Addressable: true,
		},
		typ:   ptrType,
		use:   compilerTypes.PointerTypeUse(ptrType, storageUse),
		token: expression.Keyword,
	}
}

func placeDescription(expression parser.Expression) string {
	switch expression := expression.(type) {
	case parser.VariableExpression:
		return expression.Name.Lexeme
	case parser.PropertyExpression:
		return placeDescription(expression.Receiver) + "." + expression.Property.Lexeme
	default:
		return "place"
	}
}

func missingMemberDiagnostic(typ compilerTypes.Type, property lexer.Token) *compilerTypes.Diagnostic {
	return &compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     property.Line,
		Column:   property.Column,
		Message:  fmt.Sprintf("%s has no member %s", typ.Name, property.Lexeme),
	}
}

func variableNode(name string) Expression {
	return Expression{Kind: VariableExpression, Name: name}
}

func variableNodeWithBinding(name string, binding BindingID) Expression {
	return Expression{Kind: VariableExpression, Name: name, Binding: binding}
}

func unaryNode(kind ExpressionKind, operand Expression) Expression {
	return Expression{Kind: kind, Operand: &operand}
}

func memberNode(operand Expression, member *compilerTypes.ObjectMember) Expression {
	return Expression{Kind: MemberExpression, Operand: &operand, Member: member}
}

// baseBindingID walks a place expression's checked node chain back to its
// root variable binding. Member, pointer-dereference, and index steps all
// keep the same storage root. It returns 0 for temporaries and foreign roots.
func baseBindingID(node *Expression) BindingID {
	for node != nil {
		switch node.Kind {
		case VariableExpression:
			return node.Binding
		case MemberExpression, DereferenceExpression, IndexExpression:
			node = node.Operand
		default:
			return 0
		}
	}
	return 0
}

// assignable reports whether source may initialize or assign to target. The
// single exception to identical types is outermost-layer weakening: MutPtr<T>
// is acceptable where Ptr<T> is expected, with every layer below identical.
func assignable(target, source compilerTypes.Type) bool {
	return compilerTypes.Assignable(target, source)
}

func contextualIntegerType(expected compilerTypes.Type) compilerTypes.Type {
	if compilerTypes.IsInteger(expected) {
		return expected
	}
	return compilerTypes.Int32
}

func contextualFloatType(expected compilerTypes.Type) compilerTypes.Type {
	if compilerTypes.IsFloat(expected) {
		return expected
	}
	return compilerTypes.Float64
}

func negatedInitializer(expression parser.NegatedNumericLiteral, expected compilerTypes.Type) initializerValue {
	switch literal := expression.Literal.(type) {
	case parser.IntegerLiteral:
		if compilerTypes.IsUnsignedInteger(expected) {
			return initializerValue{typ: expected, token: expression.Minus, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     expression.Minus.Line,
				Column:   expression.Minus.Column,
				Message:  "negated integer literal requires a signed destination",
			}}
		}
		return integerInitializer(literal.Token, contextualIntegerType(expected), true)
	case parser.DecimalLiteral:
		return floatInitializer(literal.Token, contextualFloatType(expected), true)
	default:
		return initializerValue{typ: compilerTypes.Int32, token: expression.Minus, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Minus.Line,
			Column:   expression.Minus.Column,
			Message:  "unsupported negated literal",
		}}
	}
}

func integerInitializer(token lexer.Token, typ compilerTypes.Type, negative ...bool) initializerValue {
	isNegative := len(negative) > 0 && negative[0]
	normalized := strings.ReplaceAll(token.Lexeme, "_", "")
	value := constant.MakeFromLiteral(normalized, gotoken.INT, 0)
	if value == nil || value.Kind() == constant.Unknown {
		return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
	}
	if isNegative {
		value = constant.UnaryOp(gotoken.SUB, value, 0)
	}
	minimum, maximum := integerBounds(typ)
	if constant.Compare(value, gotoken.LSS, minimum) || constant.Compare(value, gotoken.GTR, maximum) {
		return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
	}
	source := constantOperand(typ, value, normalized)
	source.Radix = literalRadix(token.Kind)
	source.Negative = isNegative
	return initializerValue{source: source, typ: typ, token: token}
}

func integerBounds(typ compilerTypes.Type) (constant.Value, constant.Value) {
	zero := constant.MakeInt64(0)
	if compilerTypes.IsUnsignedInteger(typ) {
		return zero, constant.MakeUint64(^uint64(0) >> (64 - typ.Bits))
	}
	if typ.Bits == 64 {
		return constant.MakeInt64(math.MinInt64), constant.MakeInt64(math.MaxInt64)
	}
	maximum := int64(1<<(typ.Bits-1) - 1)
	return constant.MakeInt64(-maximum - 1), constant.MakeInt64(maximum)
}

func literalRadix(kind lexer.TokenKind) LiteralRadix {
	switch kind {
	case lexer.HexInteger:
		return HexadecimalRadix
	case lexer.BinaryInteger:
		return BinaryRadix
	case lexer.OctalInteger:
		return OctalRadix
	default:
		return DecimalRadix
	}
}

func floatInitializer(token lexer.Token, typ compilerTypes.Type, negative ...bool) initializerValue {
	isNegative := len(negative) > 0 && negative[0]
	normalized := strings.ReplaceAll(token.Lexeme, "_", "")
	value := constant.MakeFromLiteral(normalized, gotoken.FLOAT, 0)
	if value == nil || value.Kind() == constant.Unknown {
		return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
	}
	source := constantOperand(typ, value, normalized)
	source.Negative = isNegative
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		converted, _ := constant.Float32Val(value)
		if math.IsInf(float64(converted), 0) {
			return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
		}
		if isNegative {
			converted = -converted
		}
		source.FloatBits = uint64(math.Float32bits(converted))
	} else {
		converted, _ := constant.Float64Val(value)
		if math.IsInf(converted, 0) {
			return initializerValue{typ: typ, token: token, diagnostic: valueOutOfRangeDiagnostic(token, typ)}
		}
		if isNegative {
			converted = -converted
		}
		source.FloatBits = math.Float64bits(converted)
	}
	return initializerValue{source: source, typ: typ, token: token}
}

func valueOutOfRangeDiagnostic(token lexer.Token, valueType compilerTypes.Type) *compilerTypes.Diagnostic {
	return &compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  fmt.Sprintf("given value is outside the %s range", valueType.Name),
	}
}
