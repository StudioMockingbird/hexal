// Package checker validates syntax and resolves it into generator-ready data.
package checker

import (
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// Program contains the checked, type-resolved statements handed to the
// generator.
type Program struct {
	TypeDeclarations     []TypeDeclaration
	ModuleValues         []ModuleValueDeclaration
	Statements           []Statement
	SpecializedFunctions []FunctionDeclaration
	SpecializedMethods   []MethodDeclaration
	Defers               []DeferredAction
	// EntryCaptures are the entry-root bindings captured by this entry
	// module's named functions and methods, in first-capture order. It is
	// empty in an imported module.
	EntryCaptures []Capture
	// GenericTypeNames, GenericFunctionNames, and GenericMethodNames name
	// every module-level open generic type/function/method template this
	// module declared: an open template carries no canonical
	// TypeDeclaration/Statement of its own under its bare name (only its
	// eventual specializations do, under a mangled name), so the export
	// block resolves against these instead of checked.TypeDeclarations and
	// checked.Statements.
	GenericTypeNames     []string
	GenericFunctionNames []string
	GenericMethodNames   map[string][]string // owner type name -> method names
	// ForeignHeaders are the C headers this module's foreign declarations
	// require, in first checked-use order, deduplicated by identity.
	ForeignHeaders []ForeignHeader
	// ForeignFunctions, ForeignConstants, and ForeignGlobals are the
	// module-owned handwritten foreign declarations. ForeignRecords are the
	// declarations of program-wide foreign C records; ForeignAliases are
	// transparent aliases declared inside a foreign block and reuse the
	// ordinary alias identity in TypeDeclarations.
	ForeignFunctions []ForeignFunctionDeclaration
	ForeignConstants []ForeignConstantDeclaration
	ForeignGlobals   []ForeignGlobalDeclaration
	ForeignRecords   []ForeignRecordDeclaration
}

// ModuleValueDeclaration is a checked top-level module constant: one immutable
// program-lifetime object in a non-entry module, distinct from Declaration (an
// executable local, valid only in the entrypoint). It is retained outside the
// executable statement list exactly like TypeDeclaration.
type ModuleValueDeclaration struct {
	Name     string
	Binding  BindingID
	Type     compilerTypes.Type
	TypeUse  compilerTypes.TypeUse
	Source   Operand
	Exported bool // stamped later by applyExportFlags
	Span     span.Span
}

// Statement is one generator-ready checked statement.
type Statement interface {
	statementNode()
}

// TypeDeclaration is a checked transparent alias. It is retained outside the
// executable statement list so generation can ignore it without losing proof
// that the declaration was resolved.
type TypeDeclaration struct {
	Name    string
	Type    compilerTypes.Type
	TypeUse compilerTypes.TypeUse
	Span    span.Span
}

// Declaration binds a name to a resolved type, binding mode, and checked
// initializer.
type Declaration struct {
	Name    string
	Binding BindingID
	Type    compilerTypes.Type
	TypeUse compilerTypes.TypeUse
	Source  Operand
	Mutable bool
	Span    span.Span
	// Captured is true when an entry-module named function or method captures
	// this root binding, so the generator lowers it as an entry-environment
	// field rather than an automatic local.
	Captured bool
}

// Capture is one entry-root binding captured by an entry-module named function
// or method.
type Capture struct {
	Name    string
	Binding BindingID
	Type    compilerTypes.Type
	Mutable bool
}

func (Declaration) statementNode() {}

// Assignment writes the checked source expression to a checked place.
type Assignment struct {
	Name   string
	Target Operand
	Type   compilerTypes.Type
	Source Operand
	Span   span.Span
}

func (Assignment) statementNode() {}

// IfStatement is the checked conditional chain. Conditions are complete Bool
// operands and each body contains only checked statements in its own scope.
type IfStatement struct {
	Condition     Operand
	ConditionSpan span.Span
	Then          []Statement
	ElseIf        []IfBranch
	Else          []Statement
	ElseSpan      span.Span
	Span          span.Span
	EndSpan       span.Span
	ThenDefers    []DeferredAction
	ElseIfDefers  [][]DeferredAction
	ElseDefers    []DeferredAction
}

func (IfStatement) statementNode() {}

// IfBranch is one checked elseif clause: its own condition and body.
type IfBranch struct {
	Condition     Operand
	ConditionSpan span.Span
	Body          []Statement
	Span          span.Span
}

// WhileStatement is a checked pre-test loop. Its body was checked with one
// additional loop context and one child lexical scope.
type WhileStatement struct {
	Condition Operand
	// ConditionKnown retains the known-value metadata of a named immutable
	// binding read used as the condition, so the constant-required
	// while-true starvation diagnostic keeps working while the read itself
	// stays in Condition. Nil for every other condition shape.
	ConditionKnown *Operand
	ConditionSpan  span.Span
	Body           []Statement
	Span           span.Span
	EndSpan        span.Span
	BodyDefers     []DeferredAction
}

func (WhileStatement) statementNode() {}

// ForStatement iterates one built-in collection or text source.
// Binders are fresh immutable names typed by the source; Source is evaluated
// once before the loop and never re-evaluated.
type ForStatement struct {
	Binders    []ForBinder
	Source     Operand
	Body       []Statement
	BodyDefers []DeferredAction
	Span       span.Span
}

func (ForStatement) statementNode() {}

// ForBinder is one checked for-in binder: name plus resolved element, key,
// value, or index type.
type ForBinder struct {
	Name    string
	Type    compilerTypes.Type
	Binding BindingID
	Span    span.Span
}

// BreakStatement leaves the innermost enclosing loop. It is valid only inside
// a loop; the checker rejects one outside any loop depth.
type BreakStatement struct {
	Span span.Span
}

func (BreakStatement) statementNode() {}

// ContinueStatement skips to the next iteration of the innermost enclosing
// loop. It is valid only inside a loop; the checker rejects one outside any
// loop depth.
type ContinueStatement struct {
	Span span.Span
}

func (ContinueStatement) statementNode() {}

// DeferredAction is one registered deferred action. A direct call captures
// its callee and arguments at registration; any other expression evaluates
// at scope exit.
type DeferredAction struct {
	IsCall bool
	Call   *Operand
	Value  *Operand
	// Span locates diagnostics emitted when the action is validated at
	// scope exit rather than at registration.
	Span span.Span
	// TrackedFreeAlloc identifies the allocation a deferred Heap.free,
	// Pool.free, Stash.destroy, or Pool.destroy call targets, captured at
	// registration so later rebinding of the same slot does not change which
	// value the action validates against. Zero means the call's target was
	// not a tracked binding at registration.
	TrackedFreeAlloc allocationID
	// Err marks an errdefer action: it runs only when the current
	// function exits by returning Error.
	Err bool
}

// DeferStatement is the checked registration of one deferred action.
type DeferStatement struct {
	Expression Operand
	Action     DeferredAction
	Span       span.Span
}

func (DeferStatement) statementNode() {}

// ReturnStatement leaves the enclosing function. Value is nil for a bare
// return, which only a no-return function accepts.
type ReturnStatement struct {
	Value *Operand
	Span  span.Span
}

func (ReturnStatement) statementNode() {}

// RootReturnStatement exits the entry module after active root defers,
// recording one UInt8 process status. It is a distinct checked statement
// from ReturnStatement, never an overload of a function return with an
// absent result: the entry module has no function result to leave absent.
// Value is nil for a bare return or entry-module fallthrough, both of which
// record status zero.
type RootReturnStatement struct {
	Value *Operand
	Span  span.Span
}

func (RootReturnStatement) statementNode() {}

// CallStatement is a call in statement position. It is the only place a
// no-return call may appear.
type CallStatement struct {
	Call Operand
	Span span.Span
}

func (CallStatement) statementNode() {}

// UnsafeStatement is one checked lexical region that granted permission for
// operations whose preconditions the compiler cannot prove. It carries no
// runtime meaning: the generator lowers Body in source order, and BodyDefers
// holds the actions registered inside the region, which run at its exit
// exactly like any other block scope.
type UnsafeStatement struct {
	Body       []Statement
	BodyDefers []DeferredAction
	Span       span.Span
}

func (UnsafeStatement) statementNode() {}

// TryStatement discards the success value of a try operand. The
// checked Expression is a TryExpression carrying the propagation metadata;
// the generator hoists its prologue and emits no value use.
type TryStatement struct {
	Expression Operand
	Span       span.Span
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
	// restBacked marks a binding whose Slice value is a non-owning descriptor
	// over a rest invocation's backing region (or a fixed alias or derived
	// Slice of one). Reading it yields a rest-backed operand whose uses the
	// checker restricts so the region cannot escape the invocation.
	restBacked bool
	id         BindingID
	// collectionRoot identifies the shared List or Dict state for copied
	// handles. A fresh collection uses its own binding ID as the root.
	collectionRoot BindingID
	// fromRef records that this binding's value originated from a `@`
	// expression in this function body, so Heap.free can reject stack
	// storage derived from it.
	fromRef bool
	// moduleID is the target canonical module of an aliasBinding import.
	// It is empty for every value and function binding.
	moduleID string
	// rootIndex is the source item index of an entry-module root binding; a
	// function body may capture it only when its own index is later.
	rootIndex int
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
	// foreignCName is the exact C symbol, constant, or object spelling of a
	// foreign binding. Empty for every non-foreign binding.
	foreignCName string
	// foreignHeader is the defining header a foreign binding requires.
	foreignHeader ForeignHeader
	// foreignParameters and foreignResult are the exact C spellings a foreign
	// function's signature records, indexed by parameter position and for the
	// result. Empty entries pass without a boundary cast.
	foreignParameters []string
	foreignResult     string
}

// Check resolves declared types, checks initializers, binding modes, pointer
// capabilities, and assignment places. A failed statement never enters the
// environment, so later diagnostics cannot observe invalid declarations.
func Check(program parser.Program) (Program, error) {
	return CheckForTarget(program, "")
}

// CheckForTarget is Check with an explicit target profile. Foreign ABI facts
// are target-dependent, so a compilation containing a foreign declaration
// requires a qualified target and never falls back to host inference.
func CheckForTarget(program parser.Program, target compilerTypes.TargetProfileID) (Program, error) {
	checked, err := CheckModulesForTarget(SingleModuleGraph(program), target, nil)
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
	return CheckModulesForTarget(graph, "", nil)
}

// CheckModulesForTarget is CheckModules with an explicit target profile. The
// target is read by every foreign declaration's ABI mapping; the checker never
// inspects the host. table is the compilation's source table; a nil table,
// which only a direct caller without one has, resolves a carried span to no
// location but never invents one.
func CheckModulesForTarget(graph *ModuleGraph, target compilerTypes.TargetProfileID, table *span.Table) (map[string]Program, error) {
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
		moduleChecked, moduleDiagnostics := checkModule(node.Program, moduleID, key, entrypointCanonical, registry, arena, target, table)
		// One stamping point for the whole stage: module diagnostics are
		// stamped here, where the module identity is known. checkModule
		// receives the logical key only for Error.file provenance, never
		// for diagnostics.
		moduleDiagnostics = moduleDiagnostics.InModule(key)
		diagnostics = append(diagnostics, moduleDiagnostics...)
		if len(moduleDiagnostics) == 0 {
			// The export block names declarations by their checked interface
			// (a qualified entry needs a method's resolved owner name), so it
			// resolves only after the module checks clean. Its result then
			// stamps every exported declaration's Exported flag before this
			// checked program is either stored or published: the generator's
			// exported-prototype and module-value-declaration writers read
			// that flag directly, and the registry's own tables must agree
			// with it.
			exports, exportDiagnostics := resolveExportEntries(node.Program, moduleChecked)
			if len(exportDiagnostics) > 0 {
				diagnostics = append(diagnostics, exportDiagnostics.InModule(key)...)
				checked[key] = moduleChecked
				continue
			}
			applyExportFlags(&moduleChecked, exports)
			checked[key] = moduleChecked
			// A clean module publishes its exported interface before its own
			// closure is validated, so importers see complete records and the
			// walker can prove its own exports against the registry.
			registry.registerExports(moduleID, exports, moduleChecked)
			diagnostics = append(diagnostics, registry.checkExportedClosure(moduleID, moduleChecked, table).InModule(key)...)
		} else {
			checked[key] = moduleChecked
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
func checkModule(program parser.Program, moduleID string, logicalKey string, entrypointCanonical string, registry *ModuleRegistry, arena *compilerTypes.Arena, target compilerTypes.TargetProfileID, table *span.Table) (Program, compilerTypes.Diagnostics) {
	checked := Program{
		TypeDeclarations: make([]TypeDeclaration, 0),
		Statements:       make([]Statement, 0, len(program.Statements)),
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	environment := moduleScope(moduleID, logicalKey, registry, table)
	typeEnvironment := compilerTypes.NewCompilationEnvironment(arena, moduleID)
	ctx := checkContext{names: environment, typeEnvironment: typeEnvironment}
	if moduleID == entrypointCanonical {
		envAnalysis := analyzeEntryEnvironment(program)
		environment.envDependent = envAnalysis.envDependent
		environment.envCaptures = envAnalysis.captures
		environment.initializedRoots = make(map[string]bool)
	}

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

	// The import block, when present, is always the file's leading construct
	// and structurally separate from Items; every alias binds before pass 1
	// reaches any type declaration.
	if program.Import != nil {
		for _, entry := range program.Import.Entries {
			// The target is the graph's resolved edge, recorded in the
			// registry: the checker reads resolution, it never repeats it.
			target, ok := registry.importTarget(moduleID, entry.Alias.Lexeme)
			if !ok {
				// A resolved graph always publishes every edge's target; a
				// missing entry is an internal inconsistency, so it fails
				// closed instead of binding an empty module id.
				diagnostics = append(diagnostics, unknownAt(entry.Alias))
				continue
			}
			if !environment.define(entry.Alias.Lexeme, binding{kind: aliasBinding, moduleID: target}) {
				diagnostics = append(diagnostics, importAliasConflictDiagnostic(entry.Alias, entry.Alias.Lexeme))
			}
		}
	}

	// Foreign blocks are leading and are checked before ordinary declarations
	// so a name they publish collides with an ordinary declaration through the
	// ordinary duplicate-name diagnostic. A block whose declarations fail
	// checking publishes nothing.
	if foreignDiagnostics := checkForeignDeclarations(program.Externs, ctx, &checked, target, moduleID); len(foreignDiagnostics) > 0 {
		diagnostics = append(diagnostics, foreignDiagnostics...)
		return checked, diagnostics
	}

	// Pass 1: type declarations, in source order. Type declarations retain
	// their own existing source-order resolution rules; only function and
	// method visibility becomes order-independent below.
	for index, item := range items {
		switch statement := item.(type) {
		case parser.TypeDeclaration:
			if _, exists := typeIndexByName[statement.Name.Lexeme]; !exists {
				typeIndexByName[statement.Name.Lexeme] = index
			}
			checkedDeclaration, statementDiagnostics := checkTypeDeclaration(statement, ctx, index, functionIndexByName)
			diagnostics = append(diagnostics, statementDiagnostics...)
			// A generic declaration is registered as an open template and
			// carries no canonical type of its own, so it is not an alias.
			if len(statementDiagnostics) == 0 && checkedDeclaration.Type.Name != "" {
				typeEnvironment.DeclareAliasUse(statement.Name.Lexeme, checkedDeclaration.TypeUse)
				checked.TypeDeclarations = append(checked.TypeDeclarations, checkedDeclaration)
			}
		}
	}

	// Pass 1.5 is intentionally absent: imported-module constants are checked
	// after function signatures in pass 2.4, below.

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
				signature, statementDiagnostics := collectFunctionSignature(asFunctionDeclaration(statement.Name, literal), ctx, rootValueNames)
				diagnostics = append(diagnostics, statementDiagnostics...)
				if len(statementDiagnostics) == 0 && signature.functionType != (compilerTypes.Type{}) {
					collectedFunctions[index] = signature
				}
				continue
			}
			if moduleID != entrypointCanonical {
				// An imported module's non-sugar top-level declaration is a
				// module constant, checked in the constant pass below.
				continue
			}
			rootValueNames[statement.Name.Lexeme] = true
		case parser.FunctionDeclaration:
			signature, statementDiagnostics := collectFunctionSignature(statement, ctx, rootValueNames)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 && signature.functionType != (compilerTypes.Type{}) {
				collectedFunctions[index] = signature
			}
		case parser.MethodDeclaration:
			methodChecked, statementDiagnostics := collectMethodSignature(statement, ctx)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 && methodChecked.Object != nil {
				collectedMethods[index] = methodChecked
			}
		}
	}

	// Pass 2.4: imported-module constants, checked after every function
	// signature so a constructor call in an initializer resolves and a plain
	// function call is then rejected by the closed static set. A fixed
	// top-level declaration whose initializer is in the closed static set
	// becomes one immutable program-lifetime object, visible throughout its
	// defining module independent of textual position. A mutable top-level
	// declaration is rejected outright. The entrypoint's own top-level
	// declarations are runtime bindings and are checked in pass 3 instead.
	if moduleID != entrypointCanonical {
		for index, item := range items {
			declaration, ok := item.(parser.Declaration)
			if !ok {
				continue
			}
			if _, isSugar := directFunctionLiteralSugar(declaration); isSugar {
				continue
			}
			checkedValue, statementDiagnostics := checkModuleConstant(declaration, moduleID, ctx, index, typeIndexByName)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				environment.define(declaration.Name.Lexeme, binding{typ: checkedValue.Type, use: checkedValue.TypeUse, kind: moduleValueBinding, id: checkedValue.Binding})
				checked.ModuleValues = append(checked.ModuleValues, checkedValue)
			}
		}
	}

	// Pass 2.45: entry-module root declarations and statements, checked in
	// source order before any body. Defining root bindings first lets a generic
	// body (pass 2.5) and every ordinary body (pass 3) capture them, while
	// keeping declarations and statements interleaved preserves root flow
	// analysis (free, narrowing) in source order. Function bodies stay in pass
	// 3; pass 3 reuses these checked items rather than re-checking them.
	checkedItems := make(map[int]Statement)
	if moduleID == entrypointCanonical {
		for index, item := range items {
			ctx.rootIndex = index
			switch statement := item.(type) {
			case parser.TypeDeclaration:
			case parser.Declaration:
				if _, isSugar := directFunctionLiteralSugar(statement); isSugar {
					continue
				}
				checkedDeclaration, declaredBinding, statementDiagnostics := checkDeclaration(statement, ctx, index, typeIndexByName)
				diagnostics = append(diagnostics, statementDiagnostics...)
				if len(statementDiagnostics) == 0 {
					declaredBinding.rootIndex = index
					environment.define(statement.Name.Lexeme, declaredBinding)
					environment.initializedRoots[statement.Name.Lexeme] = true
					checkedItems[index] = checkedDeclaration
				}
			case parser.FunctionDeclaration, parser.MethodDeclaration:
			default:
				checkedStatement, statementDiagnostics := checkRootExecutable(item, ctx)
				diagnostics = append(diagnostics, statementDiagnostics...)
				if len(statementDiagnostics) == 0 && checkedStatement != nil {
					checkedItems[index] = checkedStatement
					if _, isRoot := checkedStatement.(RootReturnStatement); isRoot {
						environment.recordReturnFlow()
					}
				}
			}
		}
	}

	// Pass 2.5: structurally check every open generic body with its
	// parameters bound to placeholders, now that every module-level
	// signature exists. A failure means the template is invalid for every
	// substitution, so the module cannot continue: its declarations are not
	// published and no specialization of it is emitted.
	if genericDiagnostics := checkOpenGenericDeclarations(items, ctx); len(genericDiagnostics) > 0 {
		diagnostics = append(diagnostics, genericDiagnostics...)
		return checked, diagnostics
	}

	// Pass 3: check every function and method body against the complete
	// signature set pass 2 collected, and every root executable statement
	// in source order, exactly as before order-independent visibility.
	for index, item := range items {
		ctx.rootIndex = index
		// Only the entrypoint module executes statements; an imported
		// module's top level is declarations only. The offending statement is
		// skipped entirely, never partially checked.
		if moduleID != entrypointCanonical {
			// A top-level declaration in an imported module is a module
			// constant or function-literal sugar, both owned elsewhere, so it
			// is never an executable statement.
			if _, isDeclaration := item.(parser.Declaration); !isDeclaration {
				if token, executable := executableItemToken(item); executable {
					diagnostics = append(diagnostics, importedModuleExecutableDiagnostic(token, moduleID))
					continue
				}
			}
		}
		if moduleID == entrypointCanonical {
			// Root declarations and statements were checked in source order in
			// pass 2.45; only function and method bodies remain here.
			switch item.(type) {
			case parser.FunctionDeclaration, parser.MethodDeclaration:
			case parser.Declaration:
				if _, isSugar := directFunctionLiteralSugar(item.(parser.Declaration)); !isSugar {
					continue
				}
			default:
				continue
			}
		}
		switch statement := item.(type) {
		case parser.TypeDeclaration:
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
				checkedStatement, statementDiagnostics := checkFunctionBody(asFunctionDeclaration(statement.Name, literal), signature, ctx, !literal.HasSyntaxErrors)
				diagnostics = append(diagnostics, statementDiagnostics...)
				if len(statementDiagnostics) == 0 {
					checkedItems[index] = checkedStatement
				}
				continue
			}
			if moduleID != entrypointCanonical {
				// Already handled as a module constant in pass 2.4.
				continue
			}
			// An entry-module non-sugar declaration was checked in pass 2.45.
			continue
		case parser.Assignment:
			checkedStatement, statementDiagnostics := checkAssignment(statement, ctx)
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
			checkedStatement, statementDiagnostics := checkFunctionBody(statement, signature, ctx, !statement.HasSyntaxErrors)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checkedItems[index] = checkedStatement
			}
		case parser.CallExpression:
			checkedStatement, statementDiagnostics := checkCallStatement(statement, ctx)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.TryStatement:
			// A try statement reuses the try-expression validation and
			// propagation metadata; only the success value differs, and it is
			// discarded.
			checkedTry := checkTryExpression(parser.TryExpression{Keyword: statement.Keyword, Operand: statement.Operand}, expressionContext{}, ctx)
			if errs := initializerDiagnostics(checkedTry); len(errs) > 0 {
				diagnostics = append(diagnostics, errs...)
				continue
			}
			checked.Statements = append(checked.Statements, TryStatement{
				Expression: checkedTry.source,
				Span:       statement.Keyword.Span,
			})
		case parser.ReturnStatement:
			checkedStatement, statementDiagnostics := checkReturnStatement(statement, ctx)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
				if _, isRoot := checkedStatement.(RootReturnStatement); isRoot {
					environment.recordReturnFlow()
				}
			}
		case parser.IfStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, ctx, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.WhileStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, ctx, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.ForStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, ctx, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.UnsafeStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, ctx, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.BreakStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, ctx, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.ContinueStatement:
			checkedStatement, _, _, statementDiagnostics := checkStatement(statement, ctx, 0)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.DeferStatement:
			checkedStatement, statementDiagnostics := checkDeferStatement(statement, ctx)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checked.Statements = append(checked.Statements, checkedStatement)
			}
		case parser.ErrdeferStatement:
			// errdefer is grammatically a statement at root but is valid only
			// where an enclosing function result accepts Error; the shared
			// check owns that diagnostic. Never append the invalid action.
			_, statementDiagnostics := checkErrdeferStatement(statement, ctx)
			diagnostics = append(diagnostics, statementDiagnostics...)
		case parser.MethodDeclaration:
			// A missing collected declaration means either a generic
			// template (checked lazily at specialization) or a pass-2
			// failure already diagnosed; either way there is no body to
			// check here.
			methodChecked, collected := collectedMethods[index]
			if !collected {
				continue
			}
			checkedStatement, statementDiagnostics := checkMethodBody(statement, methodChecked, ctx, !statement.HasSyntaxErrors)
			diagnostics = append(diagnostics, statementDiagnostics...)
			if len(statementDiagnostics) == 0 {
				checkedItems[index] = checkedStatement
			}
		default:
			// Exhaustive over parser.TopLevelItem today; a new item form
			// reaching this default is a compiler inconsistency and reports
			// [Unknown Error], never a user category.
			diagnostics = append(diagnostics, unknownAt(lexer.Token{Line: 1, Column: 1}))
		}
	}
	for index := range items {
		if checkedStatement, ok := checkedItems[index]; ok {
			checked.Statements = append(checked.Statements, checkedStatement)
		}
	}
	diagnostics = append(diagnostics, validateDeferredActions(environment, !sequenceTerminates(checked.Statements))...)

	checked.TypeDeclarations = append(checked.TypeDeclarations, environment.generics.typeDeclarations...)
	checked.SpecializedFunctions = specializedFunctionList(environment.generics)
	checked.SpecializedMethods = specializedMethodList(environment.generics)
	checked.Defers = append(checked.Defers, environment.defers...)
	for name := range environment.generics.types {
		checked.GenericTypeNames = append(checked.GenericTypeNames, name)
	}
	for name, open := range environment.generics.functions {
		if !open.local {
			checked.GenericFunctionNames = append(checked.GenericFunctionNames, name)
		}
	}
	for _, open := range environment.generics.methods {
		if checked.GenericMethodNames == nil {
			checked.GenericMethodNames = make(map[string][]string)
		}
		checked.GenericMethodNames[open.ObjectName] = append(checked.GenericMethodNames[open.ObjectName], open.Name)
	}

	if len(diagnostics) > 0 {
		return checked, diagnostics
	}
	// The starvation rule runs only after the program checked clean, so its
	// Semantic Errors are never mixed with earlier failures.
	starvationDiagnostics := checkStarvation(checked, table)
	if len(starvationDiagnostics) > 0 {
		return checked, starvationDiagnostics
	}
	if registry != nil {
		// A clean module publishes its generic templates and its own
		// specialization requests, so importers resolve and record against
		// the defining module's collection.
		registry.registerGenerics(moduleID, environment.generics)
		// Retained so a later importer's qualified generic use re-resolves
		// the open template's signature and body against this module's own
		// scope and type environment, never the importer's.
		registry.storeDefiningContext(moduleID, environment, typeEnvironment)
	}
	if moduleID == entrypointCanonical {
		markEntryCaptures(&checked)
		computeEnvironmentDependence(&checked)
	}
	return checked, nil
}

// checkRootExecutable checks one entry-module root statement. It returns a nil
// statement when the item is not an executable statement.
func checkRootExecutable(item parser.TopLevelItem, ctx checkContext) (Statement, compilerTypes.Diagnostics) {
	switch statement := item.(type) {
	case parser.Assignment:
		return checkAssignment(statement, ctx)
	case parser.CallExpression:
		return checkCallStatement(statement, ctx)
	case parser.TryStatement:
		checkedTry := checkTryExpression(parser.TryExpression{Keyword: statement.Keyword, Operand: statement.Operand}, expressionContext{}, ctx)
		if errs := initializerDiagnostics(checkedTry); len(errs) > 0 {
			return nil, errs
		}
		return TryStatement{Expression: checkedTry.source, Span: statement.Keyword.Span}, nil
	case parser.ReturnStatement:
		return checkReturnStatement(statement, ctx)
	case parser.IfStatement, parser.WhileStatement, parser.ForStatement, parser.UnsafeStatement, parser.BreakStatement, parser.ContinueStatement:
		checkedStatement, _, _, statementDiagnostics := checkStatement(statement.(parser.Statement), ctx, 0)
		if len(statementDiagnostics) > 0 {
			return nil, statementDiagnostics
		}
		return checkedStatement, nil
	case parser.DeferStatement:
		return checkDeferStatement(statement, ctx)
	case parser.ErrdeferStatement:
		_, statementDiagnostics := checkErrdeferStatement(statement, ctx)
		return nil, statementDiagnostics
	}
	return nil, nil
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
	case parser.UnsafeStatement:
		return statement.Keyword, true
	case parser.BreakStatement:
		return statement.Keyword, true
	case parser.ContinueStatement:
		return statement.Keyword, true
	case parser.DeferStatement:
		return statement.Keyword, true
	case parser.ErrdeferStatement:
		return statement.Keyword, true
	case parser.Assignment, parser.CallExpression:
		return lexer.Token{Line: 1, Column: 1}, true
	}
	// parser.ReturnStatement is deliberately absent: checkReturnStatement
	// itself rejects one outside the entry module, with the exact
	// entry-module-or-function-body diagnostic instead of this generic one.
	return lexer.Token{}, false
}

// assignable reports whether source may initialize or assign to target. The
// single exception to identical types is outermost-layer weakening: Ptr<mut T>
// is acceptable where Ptr<T> is expected, with every layer below identical.
//
// A check whose target or source contains an open type parameter is
// substitution-dependent: some concrete argument can make the pair identical
// or widenable, so it is deferred to specialization rather than rejected at
// declaration. The concrete specialization carries no type parameter, so its
// own check is unaffected.
func assignable(target, source compilerTypes.Type) bool {
	if compilerTypes.ContainsTypeParameter(target) || compilerTypes.ContainsTypeParameter(source) {
		return true
	}
	return compilerTypes.Assignable(target, source)
}
