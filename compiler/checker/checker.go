// Package checker validates syntax and resolves it into generator-ready data.
package checker

import (
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

// ForStatement iterates one built-in collection or text source.
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
	// SourceLine and SourceColumn locate diagnostics emitted when the action
	// is validated at scope exit rather than at registration.
	SourceLine   int
	SourceColumn int
	// HeapFreeBinding and HeapFreeVersion identify the pointer value captured
	// by a deferred Heap.free call across later rebinding of the same slot.
	HeapFreeBinding BindingID
	HeapFreeVersion uint64
	// Err marks an errdefer action: it runs only when the current
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

// TryStatement discards the success value of a try operand. The
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
	loopBinder bool // a for-in binder: fresh and immutable
	id         BindingID
	// collectionRoot identifies the shared List or Dict state for copied
	// handles. A fresh collection uses its own binding ID as the root.
	collectionRoot BindingID
	// viewRoots and viewRootKind record a View binding's root so a later
	// return of the binding can classify it.
	viewRoots    []BindingID
	viewRootKind ViewRootKind
	// fromRef records that this binding's value originated from a `ref`
	// expression in this function body, so from_pointer can reject it.
	fromRef bool
	// moduleID is the target canonical module of an aliasBinding import.
	// It is empty for every value and function binding.
	moduleID string
	// genericFunction is the open template a genericFunctionBinding refers
	// to. Resolution reads it directly from the binding rather than a
	// name-keyed lookup, so a local generic's binding can be found through
	// its own lexical block without touching any module-wide, name-keyed
	// table that a same-named sibling in another scope could collide with.
	genericFunction *openGenericFunction
	// localHelperOrdinal is nonzero for a functionBinding that names a local
	// named function: a reference built from this binding carries the
	// ordinal instead of the source name, so two same-named local functions
	// in disjoint scopes generate distinct symbols.
	localHelperOrdinal BindingID
}

// Check resolves declared types, checks initializers, binding modes, pointer
// capabilities, and assignment places. A failed statement never enters the
// environment, so later diagnostics cannot observe invalid declarations.
func Check(program parser.Program) (Program, error) {
	checked, err := CheckModules(SingleModuleGraph(program))
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

// CheckModules checks every module of the graph in its dependency-first
// order. Each module is checked in its own scope; the returned map is keyed by
// the graph's logical source keys and holds one entry per node, so a later
// consumer's lookup is total. Diagnostics are merged sorted by module order,
// then line, then column.
func CheckModules(graph *ModuleGraph) (map[string]Program, error) {
	checked := make(map[string]Program, len(graph.Order))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	registry := buildModuleRegistry(graph)
	entrypointCanonical := graph.Root
	// One arena per compilation: every module shares it so constructed
	// types intern once across module boundaries.
	arena := compilerTypes.NewArena()
	reserveNominalDefinitionNames(graph, arena)
	for _, moduleID := range graph.Order {
		node := graph.Modules[moduleID]
		key := node.LogicalKey
		moduleChecked, moduleDiagnostics := checkModule(node.Program, moduleID, key, entrypointCanonical, registry, arena)
		// One stamping point for the whole stage: module diagnostics are
		// stamped here, where the module identity is known. checkModule
		// receives the logical key only for Error.file provenance, never
		// for diagnostics.
		moduleDiagnostics = moduleDiagnostics.InModule(key)
		diagnostics = append(diagnostics, moduleDiagnostics...)
		checked[key] = moduleChecked
		if len(moduleDiagnostics) == 0 {
			// A clean module publishes its exported interface before its own
			// closure is validated, so importers see complete records and the
			// walker can prove its own exports against the registry.
			registry.registerExports(moduleID, moduleChecked)
			diagnostics = append(diagnostics, registry.checkExportedClosure(moduleID, moduleChecked).InModule(key)...)
		}
	}
	// After every module checks, fold each defining module's specialization
	// collection -- its own requests plus every importer's -- into its checked
	// program, deduplicated by key and deterministically ordered. Requests
	// recorded while a later module failed are harmless: the compilation
	// already reports diagnostics.
	for _, moduleID := range graph.Order {
		key := graph.Modules[moduleID].LogicalKey
		program := checked[key]
		registry.assembleSpecializations(moduleID, &program)
		checked[key] = program
	}
	if len(diagnostics) > 0 {
		return checked, diagnostics
	}
	return checked, nil
}

// reserveNominalDefinitionNames pre-reserves the definition-keying C name of
// every concrete nominal declaration in the graph before any module checks,
// so a structural union constructed anywhere can never claim one: a nominal
// name is fixed by its declaring module's owner, while a union's name derives
// from member spellings, so the overlap is real only when a member spells
// exactly the owner-qualified name of another nominal. Concrete means
// non-generic; aliases introduce no C typedef and reserve nothing.
// BeginObject and BeginADT re-reserve the same name with the completed type,
// which is idempotent.
func reserveNominalDefinitionNames(graph *ModuleGraph, arena *compilerTypes.Arena) {
	for _, moduleID := range graph.Order {
		node := graph.Modules[moduleID]
		for _, item := range node.Program.Items {
			declaration, ok := item.(parser.TypeDeclaration)
			if !ok || len(declaration.Parameters) > 0 {
				continue
			}
			switch declaration.Target.(type) {
			case parser.AdtDefinitionExpression, parser.ObjectTypeExpression:
				name := "hex_t_" + compilerTypes.EncodeModuleOwner(moduleID) + "_" + compilerTypes.SanitizeIdentifier(declaration.Name.Lexeme)
				arena.ReserveDefinitionName(name, compilerTypes.Type{
					Name:         declaration.Name.Lexeme,
					CanonicalKey: compilerTypes.CanonicalNominalKey(declaration.Name.Lexeme, moduleID),
				})
			}
		}
	}
}

// checkModule checks one module in its own scope. moduleID is the module's
// canonical identity; logicalKey is its source-map filename; entrypointCanonical
// is the root module's canonical identity, the only module allowed to execute
// statements. registry carries the import aliases every module scope sees.
func checkModule(program parser.Program, moduleID string, logicalKey string, entrypointCanonical string, registry *ModuleRegistry, arena *compilerTypes.Arena) (Program, compilerTypes.Diagnostics) {
	checked := Program{
		TypeDeclarations: make([]TypeDeclaration, 0),
		Statements:       make([]Statement, 0, len(program.Statements)),
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	environment := moduleScope(moduleID, logicalKey, registry)
	typeEnvironment := compilerTypes.NewCompilationEnvironment(arena, moduleID)

	items := program.Items
	if items == nil {
		items = make([]parser.TopLevelItem, 0, len(program.Statements))
		for _, statement := range program.Statements {
			items = append(items, statement)
		}
	}

	// functionIndexByName records each module-level function's earliest
	// source position before pass 1 runs, so a type declaration processed in
	// pass 1 can tell whether a same-named function is actually earlier in
	// source, even though functions are not otherwise collected until pass
	// 2. Without this, a function appearing before a colliding type would
	// wrongly have the type register successfully (function-collection has
	// not happened yet) and then blame the function's own, earlier
	// declaration once pass 2 reaches it, backwards from the rule that the
	// later declaration always owns the diagnostic.
	functionIndexByName := make(map[string]int)
	for index, item := range items {
		var name string
		switch statement := item.(type) {
		case parser.FunctionDeclaration:
			name = statement.Name.Lexeme
		case parser.Declaration:
			if _, isSugar := directFunctionLiteralSugar(statement); isSugar {
				name = statement.Name.Lexeme
			}
		}
		if name == "" {
			continue
		}
		if _, exists := functionIndexByName[name]; !exists {
			functionIndexByName[name] = index
		}
	}

	// typeIndexByName records each type declaration's source position as
	// pass 1 reaches it, so pass 3 can tell a root declaration whether its
	// own type annotation names a type declared later: root declarations
	// keep the pre-existing restriction against that, even though pass 1
	// fully populating typeEnvironment ahead of pass 3 would otherwise make
	// every type look available regardless of position.
	typeIndexByName := make(map[string]int)

	// Pass 1: imports, then type declarations, in source order. Imports are
	// always a contiguous prefix (enforced by the parser), so every one is
	// always reached before any type here. Type declarations retain their
	// own existing source-order resolution rules; only function and method
	// visibility becomes order-independent below.
	for index, item := range items {
		switch statement := item.(type) {
		case parser.ImportDeclaration:
			// The target is the graph's resolved edge, recorded in the
			// registry: the checker reads resolution, it never repeats it.
			target, ok := registry.importTarget(moduleID, statement.Alias.Lexeme)
			if !ok {
				// A resolved graph always publishes every edge's target; a
				// missing entry is an internal inconsistency, so it fails
				// closed instead of binding an empty module id.
				diagnostics = append(diagnostics, unknownAt(statement.Alias, "import alias "+statement.Alias.Lexeme+" has no resolved module target"))
				continue
			}
			if !environment.define(statement.Alias.Lexeme, binding{kind: aliasBinding, moduleID: target}) {
				diagnostics = append(diagnostics, nameErrorAt(statement.Alias, "import alias "+statement.Alias.Lexeme+" conflicts with an existing name"))
			}
		case parser.TypeDeclaration:
			if _, exists := typeIndexByName[statement.Name.Lexeme]; !exists {
				typeIndexByName[statement.Name.Lexeme] = index
			}
			checkedDeclaration, statementDiagnostics := checkTypeDeclaration(statement, typeEnvironment, environment, index, functionIndexByName)
			diagnostics = append(diagnostics, statementDiagnostics...)
			// A generic declaration is registered as an open template and
			// carries no canonical type of its own, so it is not an alias.
			if len(statementDiagnostics) == 0 && checkedDeclaration.Type.Name != "" {
				typeEnvironment.DeclareAliasUse(statement.Name.Lexeme, checkedDeclaration.TypeUse)
				checked.TypeDeclarations = append(checked.TypeDeclarations, checkedDeclaration)
			}
		}
	}

	// Pass 2: collect every module-level function and method signature from
	// the completed type environment, before any body or root executable
	// statement is checked. This is what makes a forward call and mutual
	// recursion between module-level functions and methods resolve: every
	// signature bound here is visible to every body pass 3 checks, in
	// either source-order direction. rootValueNames tracks each root
	// value's name, in source order, as this pass reaches it, even though
	// root values are not otherwise touched until pass 3: it is what keeps
	// a function-vs-root-value name collision attributed to whichever
	// declaration is actually later in source, regardless of which pass
	// reaches it first.
	collectedFunctions := make(map[int]functionSignature, len(items))
	collectedMethods := make(map[int]MethodDeclaration, len(items))
	rootValueNames := make(map[string]bool)
	for index, item := range items {
		switch statement := item.(type) {
		case parser.Declaration:
			if literal, isSugar := directFunctionLiteralSugar(statement); isSugar {
				signature, statementDiagnostics := collectFunctionSignature(asFunctionDeclaration(statement.Name, literal), environment, typeEnvironment, rootValueNames)
				diagnostics = append(diagnostics, statementDiagnostics...)
				if len(statementDiagnostics) == 0 && signature.functionType != (compilerTypes.Type{}) {
					collectedFunctions[index] = signature
				}
				continue
			}
			rootValueNames[statement.Name.Lexeme] = true
		case parser.FunctionDeclaration:
			signature, statementDiagnostics := collectFunctionSignature(statement, environment, typeEnvironment, rootValueNames)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 && signature.functionType != (compilerTypes.Type{}) {
				collectedFunctions[index] = signature
			}
		case parser.ImplDeclaration:
			methodChecked, statementDiagnostics := collectMethodSignature(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 && methodChecked.Object != nil {
				collectedMethods[index] = methodChecked
			}
		}
	}

	// Pass 3: check every function and method body against the complete
	// signature set pass 2 collected, and every root executable statement
	// in source order, exactly as before order-independent visibility.
	for index, item := range items {
		// Only the entrypoint module executes statements; an imported
		// module's top level is declarations only. The offending statement is
		// skipped entirely, never partially checked.
		if moduleID != entrypointCanonical {
			if token, executable := executableItemToken(item); executable {
				diagnostics = append(diagnostics, moduleErrorAt(token, "imported module "+moduleID+" contains executable statements"))
				continue
			}
		}
		switch statement := item.(type) {
		case parser.TypeDeclaration, parser.ImportDeclaration:
			// Already fully handled in pass 1.
		case parser.Declaration:
			if literal, isSugar := directFunctionLiteralSugar(statement); isSugar {
				// A direct inferred fixed literal declaration is checked as
				// the equivalent named function declaration; it is
				// declaration sugar, not runtime data, and emits no
				// initializer statement or function-pointer object. A
				// missing collected signature means either a generic
				// template (checked lazily at specialization, nothing more
				// to do here) or a pass-2 failure already diagnosed.
				signature, collected := collectedFunctions[index]
				if !collected {
					continue
				}
				checkedStatement, statementDiagnostics := checkFunctionBody(asFunctionDeclaration(statement.Name, literal), signature, environment, typeEnvironment, !literal.HasSyntaxErrors)
				diagnostics = append(diagnostics, statementDiagnostics...)
				if len(statementDiagnostics) == 0 {
					checked.Statements = append(checked.Statements, checkedStatement)
				}
				continue
			}
			checkedStatement, declaredBinding, statementDiagnostics := checkDeclaration(statement, environment, typeEnvironment, index, typeIndexByName)
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
			// A missing collected signature means either a generic
			// template (checked lazily at specialization) or a pass-2
			// failure already diagnosed; either way there is no body to
			// check here.
			signature, collected := collectedFunctions[index]
			if !collected {
				continue
			}
			checkedStatement, statementDiagnostics := checkFunctionBody(statement, signature, environment, typeEnvironment, !statement.HasSyntaxErrors)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.CallExpression:
			checkedStatement, statementDiagnostics := checkCallStatement(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.TryStatement:
			// A try statement reuses the try-expression validation and
			// propagation metadata; only the success value differs, and it is
			// discarded.
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
		case parser.ForStatement:
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
			// errdefer is grammatically a statement at root but is valid only
			// where an enclosing function result accepts Error; the shared
			// check owns that diagnostic. Never append the invalid action.
			_, statementDiagnostics := checkErrdeferStatement(statement, environment, typeEnvironment)
			diagnostics = append(diagnostics, statementDiagnostics...)
		case parser.ImplDeclaration:
			// A missing collected declaration means either a generic
			// template (checked lazily at specialization) or a pass-2
			// failure already diagnosed; either way there is no body to
			// check here.
			methodChecked, collected := collectedMethods[index]
			if !collected {
				continue
			}
			checkedStatement, statementDiagnostics := checkMethodBody(statement, methodChecked, environment, typeEnvironment, !statement.HasSyntaxErrors)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		default:
			// Exhaustive over parser.TopLevelItem today; a new item form
			// reaching this default is a compiler inconsistency and reports
			// [Unknown Error], never a user category.
			diagnostics = append(diagnostics, unknownAt(lexer.Token{Line: 1, Column: 1}, "unsupported top-level item"))
		}
	}
	diagnostics = append(diagnostics, validateDeferredActions(environment, !sequenceTerminates(checked.Statements))...)

	checked.TypeDeclarations = append(checked.TypeDeclarations, environment.generics.typeDeclarations...)
	checked.SpecializedFunctions = specializedFunctionList(environment.generics)
	checked.SpecializedMethods = specializedMethodList(environment.generics)
	checked.Defers = append(checked.Defers, environment.defers...)

	if len(diagnostics) > 0 {
		return checked, diagnostics
	}
	// The starvation rule runs only after the program checked clean, so its
	// Semantic Errors are never mixed with earlier failures.
	starvationDiagnostics := checkStarvation(checked)
	if len(starvationDiagnostics) > 0 {
		return checked, starvationDiagnostics
	}
	if registry != nil {
		// A clean module publishes its generic templates and its own
		// specialization requests, so importers resolve and record against
		// the defining module's collection.
		registry.registerGenerics(moduleID, environment.generics)
	}
	return checked, nil
}

func topLevelItemToken(item parser.TopLevelItem) (lexer.Token, bool) {
	switch node := item.(type) {
	case parser.TypeDeclaration:
		return node.Name, true
	case parser.ImportDeclaration:
		return node.Alias, true
	case parser.FunctionDeclaration:
		return node.Name, true
	case parser.ImplDeclaration:
		return node.Name, true
	case parser.Declaration:
		return node.Name, true
	case parser.Assignment:
		if token, ok := assignmentTargetToken(node.Target); ok {
			return token, true
		}
		return lexer.Token{Line: 1, Column: 1}, true
	case parser.CallExpression:
		return tokenOf(node), true
	case parser.ReturnStatement:
		return node.Keyword, true
	case parser.IfStatement:
		return node.Keyword, true
	case parser.WhileStatement:
		return node.Keyword, true
	case parser.ForStatement:
		return node.Keyword, true
	case parser.BreakStatement:
		return node.Keyword, true
	case parser.ContinueStatement:
		return node.Keyword, true
	case parser.DeferStatement:
		return node.Keyword, true
	case parser.ErrdeferStatement:
		return node.Keyword, true
	case parser.TryStatement:
		return node.Keyword, true
	}
	return lexer.Token{}, false
}

func assignmentTargetToken(target parser.Expression) (lexer.Token, bool) {
	switch node := target.(type) {
	case parser.VariableExpression:
		return node.Name, true
	case parser.PropertyExpression:
		return node.Property, true
	case parser.IndexExpression:
		return tokenOf(node.Receiver), true
	}
	return lexer.Token{}, false
}

// executableItemToken classifies one top-level item as an executable
// statement, returning the token diagnostics point at: the declared name for
// a data declaration, the statement keyword when one exists, or 1,1. An
// imported module may contain none of these.
func executableItemToken(item parser.TopLevelItem) (lexer.Token, bool) {
	switch statement := item.(type) {
	case parser.Declaration:
		if _, isSugar := directFunctionLiteralSugar(statement); isSugar {
			// Declaration sugar over a function form is a declaration, not
			// an executable statement, exactly like the named spelling it is
			// equivalent to.
			return lexer.Token{}, false
		}
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
