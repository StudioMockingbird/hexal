package checker

import (
	"hexal/compiler/corelib"
	"hexal/compiler/lexer"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// bindingKind separates storage from a declared function and from an import
// alias. A function name is not a place: it can be read as a Fun<...> value
// and nothing else. An import alias is not a value at all: name lookup skips
// it, and only qualified resolution reaches its target module.
type bindingKind uint8

const (
	dataBinding bindingKind = iota
	functionBinding
	genericFunctionBinding
	aliasBinding
	// moduleValueBinding is an imported-module constant: program-lifetime module
	// storage visible throughout its defining module independent of textual
	// position, unlike an ordinary dataBinding root value (main-local,
	// unreachable from a function body).
	moduleValueBinding
	// foreignFunctionBinding, foreignConstantBinding, and foreignGlobalBinding
	// name handwritten foreign declarations. They are module-owned like a
	// function or module value, but lower to the exact C symbol the binding
	// records instead of a source-derived spelling.
	foreignFunctionBinding
	foreignConstantBinding
	foreignGlobalBinding
)

// scope is one lexical name frame. Module bindings remain in module and are
// intentionally hidden from function bodies; control-flow frames chain to
// their enclosing frame so branch declarations do not escape their block.
type scope struct {
	module     map[string]binding
	local      map[string]binding // nil only at module level
	parent     *scope
	moduleID   string // the enclosing module's canonical identity
	logicalKey string // the enclosing module's source-map filename
	// table resolves a carried span to the line and column a diagnostic
	// renders. It is the compilation's one table, shared by reference with
	// every child scope, and nil for a lone module checked outside Compile.
	table        *span.Table
	owner        string // enclosing function or method name, for diagnostics
	result       *compilerTypes.Type
	resultUse    *compilerTypes.TypeUse
	methods      *methodTable
	self         *compilerTypes.Type // the method receiver struct; nil outside a method body
	selfID       BindingID
	function     bool
	nextID       *BindingID
	flow         *flowState // branch-local narrowing facts
	generics     *genericTable
	defers       []DeferredAction
	returnFlows  []returnFlow // states and active actions reaching a return
	cleanupDepth int          // checking a defer or errdefer action
	// unsafeDepth counts the lexical unsafe regions enclosing this frame. It
	// is inherited by every nested block frame and deliberately not by a
	// function-body frame: the permission is lexical and never travels
	// through a call.
	unsafeDepth int
	// registry is the compilation's module graph: it resolves import aliases
	// against the target modules' exported records.
	// It is shared by reference with every child scope.
	registry *ModuleRegistry
	// closureRoot marks the body scope of a local named function or
	// anonymous function literal. lookup treats every data or
	// parameter binding beyond this scope's own local map as an invalid
	// capture, while functions declared at or beyond it remain visible: a
	// non-capturing function may call any function visible at its source
	// position, but may not read an enclosing function's data.
	closureRoot bool
	// capture records the entry-root bindings this named entry function or
	// method captures. It is shared by reference with every nested block
	// frame, and nil for an anonymous or local function literal, which never
	// captures.
	capture *captureState
	// envDependent names the entry module's environment-dependent functions
	// and methods; envCaptures maps each to its captured root binding names;
	// initializedRoots tracks the root bindings already initialized at the
	// current point in the root script. All three are shared by reference.
	envDependent     map[string]bool
	envCaptures      map[string]map[string]bool
	initializedRoots map[string]bool
	// rootIndex is this body's source item index, for source-order capture
	// visibility against a root binding's own index.
	rootIndex int
}

// captureState records the root bindings one entry-module named function or
// method captures, in first-use order. allowed is false for a scope that may
// not capture at all.
type captureState struct {
	allowed  bool
	bindings map[string]binding
	order    []string
}

func (state *captureState) record(name string, bound binding) {
	if state == nil || !state.allowed {
		return
	}
	if _, seen := state.bindings[name]; !seen {
		state.order = append(state.order, name)
	}
	state.bindings[name] = bound
}

// capturesOf returns one scope's captures in first-use order.
func capturesOf(state *captureState) []Capture {
	if state == nil {
		return nil
	}
	var captures []Capture
	for _, name := range state.order {
		bound := state.bindings[name]
		captures = append(captures, Capture{Name: name, Binding: bound.id, Type: bound.typ, Mutable: bound.mutable})
	}
	return captures
}

// markEntryCaptures records the entry module's captured root bindings in
// source order and flags their checked declarations, so the generator lowers
// them as entry-environment fields.
func markEntryCaptures(checked *Program) {
	captured := make(map[BindingID]bool)
	byBinding := make(map[BindingID]Capture)
	for _, statement := range checked.Statements {
		switch declaration := statement.(type) {
		case FunctionDeclaration:
			for _, capture := range declaration.Captures {
				captured[capture.Binding] = true
				byBinding[capture.Binding] = capture
			}
		case MethodDeclaration:
			for _, capture := range declaration.Captures {
				captured[capture.Binding] = true
				byBinding[capture.Binding] = capture
			}
		}
	}
	for _, declaration := range checked.SpecializedFunctions {
		for _, capture := range declaration.Captures {
			captured[capture.Binding] = true
			byBinding[capture.Binding] = capture
		}
	}
	for _, declaration := range checked.SpecializedMethods {
		for _, capture := range declaration.Captures {
			captured[capture.Binding] = true
			byBinding[capture.Binding] = capture
		}
	}
	for index, statement := range checked.Statements {
		declaration, ok := statement.(Declaration)
		if !ok || !captured[declaration.Binding] {
			continue
		}
		declaration.Captured = true
		checked.Statements[index] = declaration
		checked.EntryCaptures = append(checked.EntryCaptures, byBinding[declaration.Binding])
	}
}

// moduleScope builds the root frame of one module. Import aliases are read
// from the registry through importTarget; the scope keeps no copy of its own.
func moduleScope(moduleID string, logicalKey string, registry *ModuleRegistry, table *span.Table) *scope {
	next := BindingID(0)
	generics := newGenericTable()
	if registry != nil {
		// Qualified type references resolve through the module's import
		// graph, which the generic table carries for the type resolver.
		generics.registry = registry
		generics.moduleID = moduleID
	}
	return &scope{module: make(map[string]binding), moduleID: moduleID, logicalKey: logicalKey, table: table, methods: newMethodTable(), nextID: &next, flow: newFlowState(), generics: generics, registry: registry}
}

// lookupBinding resolves a binding record by identity through the scope
// chain.
func (names *scope) lookupBinding(id BindingID) (binding, bool) {
	for frame := names; frame != nil; frame = frame.parent {
		for _, bound := range frame.local {
			if bound.id == id {
				return bound, true
			}
		}
		for _, bound := range frame.module {
			if bound.id == id {
				return bound, true
			}
		}
	}
	return binding{}, false
}

// setFromRef updates the fromRef flag of a binding by identity, writing the
// change back into the frame that owns the binding. It reports whether the
// binding was found.
func (names *scope) setFromRef(id BindingID, fromRef bool) bool {
	for frame := names; frame != nil; frame = frame.parent {
		for name, bound := range frame.local {
			if bound.id == id {
				bound.fromRef = fromRef
				frame.local[name] = bound
				return true
			}
		}
		for name, bound := range frame.module {
			if bound.id == id {
				bound.fromRef = fromRef
				frame.module[name] = bound
				return true
			}
		}
	}
	return false
}

// setCollectionRoot records the shared List or Dict state for one binding.
// The binding is updated in its owning lexical frame so later reads and
// nested loop checks see the same copied-handle identity.
func (names *scope) setCollectionRoot(id BindingID, root BindingID) bool {
	for frame := names; frame != nil; frame = frame.parent {
		for name, bound := range frame.local {
			if bound.id == id {
				bound.collectionRoot = root
				frame.local[name] = bound
				return true
			}
		}
		for name, bound := range frame.module {
			if bound.id == id {
				bound.collectionRoot = root
				frame.module[name] = bound
				return true
			}
		}
	}
	return false
}

// selfPlace resolves the implicit receiver. `self` is a keyword, so it can
// never be declared or shadowed; it exists exactly when a scope carries a
// method receiver. The place is never writable: the receiver is a fixed copy
// of the caller's struct, so neither assigning to self nor writing a member
// through it can reach caller storage.
func selfPlace(names *scope, token lexer.Token) checkedExpression {
	if names.self == nil {
		return checkedExpression{token: token, diagnostic: selfNotBoundDiagnostic(token)}
	}
	return checkedExpression{
		source: Operand{
			Kind:        VariableOperand,
			Type:        *names.self,
			Name:        "self",
			Binding:     names.selfID,
			Node:        variableNodeWithBinding("self", names.selfID),
			Addressable: true,
		},
		typ:   *names.self,
		use:   compilerTypes.NewTypeUse(*names.self),
		token: token,
		self:  true,
	}
}

func (names *scope) inFunction() bool { return names.function }

func (names *scope) newBindingID() BindingID {
	if names.nextID == nil {
		return 0
	}
	*names.nextID = *names.nextID + 1
	return *names.nextID
}

// lookupStatus distinguishes a resolved name from the two failures that carry
// different diagnostics.
type lookupStatus uint8

const (
	nameFound lookupStatus = iota
	nameMissing
	nameModuleData
)

// lookup resolves a name for the current scope. Inside a function body a
// module-level data binding is deliberately unreachable; only previously
// declared functions remain visible. An import alias is never a value and is
// skipped entirely, so a bare alias reference fails as an unknown variable.
//
// Crossing a closureRoot scope narrows this further: a local
// function or anonymous literal's own body may read its own parameters, its
// own self-recursion binding, and bindings it declares itself, but nothing
// data-shaped from an enclosing function. dataVisible starts true (this
// scope's own bindings are ordinary) and turns off for good the moment the
// walk steps out of a closureRoot scope into its parent, so every binding
// beyond that point counts only if it is itself a function. A rejected
// capture is reported as nameMissing, not a distinct status: a closed
// function has no environment, so an enclosing local is, from its
// perspective, simply not there - the same ordinary unknown-name diagnostic
// that a genuine typo gets.
func (names *scope) lookup(name string) (binding, lookupStatus) {
	dataVisible := true
	for current := names; current != nil && current.local != nil; current = current.parent {
		if bound, ok := current.local[name]; ok {
			if dataVisible || bound.kind == functionBinding || bound.kind == genericFunctionBinding {
				return bound, nameFound
			}
			return binding{}, nameMissing
		}
		if current.closureRoot {
			dataVisible = false
		}
	}
	if bound, ok := names.module[name]; ok && bound.kind != aliasBinding {
		if names.inFunction() && bound.kind == dataBinding && names.capture != nil && names.capture.allowed && bound.rootIndex < names.rootIndex {
			// An entry-module named function or method captures an earlier
			// root binding by reference.
			names.capture.record(name, bound)
			return bound, nameFound
		}
		if names.inFunction() && bound.kind != functionBinding && bound.kind != genericFunctionBinding &&
			bound.kind != moduleValueBinding && bound.kind != foreignFunctionBinding &&
			bound.kind != foreignConstantBinding && bound.kind != foreignGlobalBinding {
			return bound, nameModuleData
		}
		return bound, nameFound
	}
	return binding{}, nameMissing
}

// importAlias reports whether name is an import alias of the enclosing
// module. The module frame is shared by reference through every child scope,
// so one lookup reaches it at any depth; shadowing an alias is forbidden.
// isEntryModule reports whether this scope's module is the compilation's own
// entrypoint, the only module where a root return is valid. A nil registry
// means a single-module compilation outside the module-graph pipeline, where
// the one module being checked is trivially its own entrypoint.
func (names *scope) isEntryModule() bool {
	return names.registry == nil || names.moduleID == names.registry.entrypoint
}

func (names *scope) importAlias(name string) bool {
	bound, exists := names.module[name]
	return exists && bound.kind == aliasBinding
}

// importAliasTarget returns the canonical id of the module an import alias
// names, when that module's source is present in this compilation. The alias
// binding lives in the shared module frame and records its target; a dangling
// alias whose target has no source resolves nowhere and keeps failing as an
// unknown variable until the missing path is reported.
func (names *scope) importAliasTarget(name string) (string, bool) {
	bound, exists := names.module[name]
	if !exists || bound.kind != aliasBinding || bound.moduleID == "" {
		return "", false
	}
	if corelib.IsModule(bound.moduleID) {
		// A core-library module publishes no registry entry -- it has no
		// Hexal source and no defining scope -- but its alias still resolves
		// to the compiler-owned declaration table.
		return bound.moduleID, true
	}
	if names.registry == nil {
		return "", false
	}
	_, present := names.registry.modules[bound.moduleID]
	return bound.moduleID, present
}

// declaredHere reports a duplicate in the innermost scope only, so a local may
// shadow a module-level value. An import alias is the exception: it is fixed
// for the whole module, so no nested scope may shadow one.
func (names *scope) declaredHere(name string) bool {
	if names.local != nil {
		if _, exists := names.local[name]; exists {
			return true
		}
		return names.importAlias(name)
	}
	_, exists := names.module[name]
	return exists
}

// define installs one binding in the current frame and reports whether it was
// accepted. At module level an import alias may not collide with any existing
// module binding; in a nested scope a name may not shadow an import alias.
// Other duplicates are validated before define is called, so they never reach
// a rejection here.
func (names *scope) define(name string, bound binding) bool {
	if names.local != nil {
		if names.importAlias(name) {
			return false
		}
		names.local[name] = bound
		return true
	}
	if bound.kind == aliasBinding {
		if _, exists := names.module[name]; exists {
			return false
		}
	}
	names.module[name] = bound
	return true
}

// closureRootScope builds the body scope for a local named function or
// anonymous function literal. Unlike child, it does not inherit
// self, result, or resultUse - a nested function has neither the enclosing
// method's receiver nor its result type - and it starts a fresh flow state
// and defer list, exactly like a module function's own body: it is a
// distinct call frame, not a continuation of the enclosing one. Its parent
// is still the declaring scope, so lookup can find every function visible at
// this source position while closureRoot blocks everything else beyond this
// scope's own local map.
func (names *scope) closureRootScope(owner string, captureAllowed bool) *scope {
	body := &scope{
		module:           names.module,
		local:            make(map[string]binding),
		parent:           names,
		owner:            owner,
		methods:          names.methods,
		function:         true,
		nextID:           names.nextID,
		flow:             newFlowState(),
		generics:         names.generics,
		registry:         names.registry,
		moduleID:         names.moduleID,
		logicalKey:       names.logicalKey,
		table:            names.table,
		closureRoot:      true,
		envDependent:     names.envDependent,
		envCaptures:      names.envCaptures,
		initializedRoots: names.initializedRoots,
	}
	if captureAllowed {
		body.capture = &captureState{allowed: true, bindings: make(map[string]binding)}
	}
	return body
}

func (names *scope) child() *scope {
	return &scope{
		module:           names.module,
		local:            make(map[string]binding),
		parent:           names,
		owner:            names.owner,
		result:           names.result,
		resultUse:        names.resultUse,
		methods:          names.methods,
		self:             names.self,
		selfID:           names.selfID,
		function:         names.function,
		nextID:           names.nextID,
		flow:             names.flow,
		generics:         names.generics,
		registry:         names.registry,
		moduleID:         names.moduleID,
		logicalKey:       names.logicalKey,
		table:            names.table,
		unsafeDepth:      names.unsafeDepth,
		capture:          names.capture,
		envDependent:     names.envDependent,
		envCaptures:      names.envCaptures,
		initializedRoots: names.initializedRoots,
	}
}

func (names *scope) activeDeferredActions() []DeferredAction {
	if names == nil || len(names.defers) == 0 {
		return nil
	}
	return append([]DeferredAction(nil), names.defers...)
}

func (names *scope) recordReturnFlow() {
	if names == nil || names.flow == nil {
		return
	}
	names.returnFlows = append(names.returnFlows, returnFlow{
		state:   names.flow.clone(),
		actions: names.activeDeferredActions(),
	})
}

func (names *scope) recordChildReturnFlows(flows []returnFlow) {
	if names == nil || len(flows) == 0 {
		return
	}
	active := names.activeDeferredActions()
	for _, flow := range flows {
		names.returnFlows = append(names.returnFlows, returnFlow{
			state:   flow.state,
			actions: append([]DeferredAction(nil), active...),
		})
	}
}
