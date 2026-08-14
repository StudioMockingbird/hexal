package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

// bindingKind separates storage from a declared function and from an import
// alias. A function name is not a place: it can be read as a Fun<...> value
// and nothing else. An import alias is not a value at all: name lookup skips
// it, and only the module phase resolves it (RFC 0034).
type bindingKind uint8

const (
	dataBinding bindingKind = iota
	functionBinding
	genericFunctionBinding
	aliasBinding
)

// scope is one lexical name frame. Module bindings remain in module and are
// intentionally hidden from function bodies; control-flow frames chain to
// their enclosing frame so branch declarations do not escape their block.
type scope struct {
	module       map[string]binding
	local        map[string]binding // nil only at module level
	parent       *scope
	moduleID     string            // the enclosing module's canonical identity (RFC 0034)
	imports      map[string]string // import alias -> canonical module id (RFC 0034)
	owner        string            // enclosing function or method name, for diagnostics
	result       *compilerTypes.Type
	resultUse    *compilerTypes.TypeUse
	methods      *methodTable
	self         *compilerTypes.Type // the impl target type; nil outside an impl body
	selfID       BindingID
	function     bool
	nextID       *BindingID
	flow         *flowState // RFC 0010 branch-local narrowing facts
	generics     *genericTable
	defers       []DeferredAction
	cleanupDepth int // checking a defer or errdefer action (RFC 0029)
	// registry is the compilation's module graph (RFC 0034 Task 5): it
	// resolves import aliases against the target modules' exported records.
	// It is shared by reference with every child scope.
	registry *ModuleRegistry
}

// moduleScope builds the root frame of one module. The scope copies its
// import table from the registry so the table is never shared mutable state;
// a module with no registry entry (the single-module path) carries an empty
// table.
func moduleScope(moduleID string, registry *ModuleRegistry) *scope {
	next := BindingID(0)
	imports := make(map[string]string)
	if registry != nil {
		if entry, ok := registry.modules[moduleID]; ok {
			for alias, target := range entry.imports {
				imports[alias] = target
			}
		}
	}
	generics := newGenericTable()
	if registry != nil {
		// RFC 0034 Task 5: qualified type references resolve through the
		// module's import graph, which the generic table carries for the
		// type resolver.
		generics.registry = registry
		generics.moduleID = moduleID
	}
	return &scope{module: make(map[string]binding), moduleID: moduleID, imports: imports, methods: newMethodTable(), nextID: &next, flow: newFlowState(), generics: generics, registry: registry}
}

// flowFact records the branch-local treatment of one binding. A binding is
// either narrowed to a proven effective read type, or marked escaped because
// a writable address of its storage left the checker's sight. The two never
// coexist: escape clears any narrowing, and an escaped binding cannot be
// re-narrowed.
type flowFact struct {
	typ     compilerTypes.Type // effective read type; zero Type when not narrowed
	escaped bool
	variant *compilerTypes.AdtVariant // active ADT variant when variant-narrowed
}

// ownerState, ownerFact, and the flowState owner table implemented RFC 0020's
// affine collection ownership. RFC 0035 removes ownership tracking: cleanup is
// the programmer's responsibility, so only narrowing facts remain.

// flowState is the narrowing table for one function body (or module scope),
// keyed by binding identity. Branch checking clones the state, narrows the
// clone, and merges only invalidation effects back; the narrowings themselves
// never survive the closing end.
type flowState struct {
	facts map[BindingID]flowFact
}

func newFlowState() *flowState {
	return &flowState{facts: make(map[BindingID]flowFact)}
}

func (state *flowState) clone() *flowState {
	cloned := newFlowState()
	for id, fact := range state.facts {
		cloned.facts[id] = fact
	}
	return cloned
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

// narrowedType returns the branch-local effective read type of a binding, if
// a narrowing covers it. An escaped binding has no narrowing.
func (state *flowState) narrowedType(id BindingID) (compilerTypes.Type, bool) {
	if state == nil {
		return compilerTypes.Type{}, false
	}
	fact, ok := state.facts[id]
	if !ok || fact.escaped {
		return compilerTypes.Type{}, false
	}
	return fact.typ, fact.typ != (compilerTypes.Type{})
}

// narrow records that a null test proved the binding holds typ. An escaped
// binding is never narrowable: a write through the escaped address could
// replace the slot at any time.
func (state *flowState) narrow(id BindingID, typ compilerTypes.Type) {
	if fact, ok := state.facts[id]; ok && fact.escaped {
		return
	}
	state.facts[id] = flowFact{typ: typ}
}

// narrowVariant records that a match arm proved the binding holds one ADT
// variant. An escaped binding is never narrowable.
func (state *flowState) narrowVariant(id BindingID, variant *compilerTypes.AdtVariant) {
	if fact, ok := state.facts[id]; ok && fact.escaped {
		return
	}
	state.facts[id] = flowFact{variant: variant}
}

// narrowedVariant returns the binding's active ADT variant, if any.
func (state *flowState) narrowedVariant(id BindingID) (*compilerTypes.AdtVariant, bool) {
	if state == nil {
		return nil, false
	}
	fact, ok := state.facts[id]
	if !ok || fact.escaped || fact.variant == nil {
		return nil, false
	}
	return fact.variant, true
}

// invalidateNarrowing drops a binding's narrowing without escaping it. This
// is the effect of assignment: the slot may now hold nil again.
func (state *flowState) invalidateNarrowing(id BindingID) {
	if fact, ok := state.facts[id]; ok {
		fact.typ = compilerTypes.Type{}
		state.facts[id] = fact
	}
}

// escape records that a writable address of the binding escaped. It clears
// any narrowing and marks the binding permanently non-narrowable.
func (state *flowState) escape(id BindingID) {
	state.facts[id] = flowFact{escaped: true}
}

// mergeBranch propagates a branch's invalidation effects into the continuing
// state: any binding the branch assigned to or escaped keeps no narrowing
// after the construct, and escape marks persist. Narrowings the branch added
// are deliberately not propagated -- no narrowing survives the closing end.
func (state *flowState) mergeBranch(branch *flowState) {
	for id, fact := range branch.facts {
		if fact.escaped {
			state.facts[id] = flowFact{escaped: true}
			continue
		}
		parent, exists := state.facts[id]
		if !exists {
			continue
		}
		if !compilerTypes.Equal(parent.typ, fact.typ) {
			parent.typ = compilerTypes.Type{}
			state.facts[id] = parent
		}
	}
}

// adopt replaces the continuing state with one continuing branch's facts
// wholesale. It is used only when every other branch terminates, so the
// adopted branch's narrowings are provably the only continuation.
func (state *flowState) adopt(branch *flowState) {
	if state == nil || branch == nil {
		return
	}
	for id, fact := range branch.facts {
		state.facts[id] = fact
	}
}

// selfPlace resolves the implicit receiver. `self` is a keyword, so it can
// never be declared or shadowed; it exists exactly when a scope carries an
// impl target. The place is never writable: rule 3 makes the binding fixed,
// while a write through it -- self.x on a MutPtr target -- gets its
// writability from the pointee, not from this binding.
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
// module-level data binding is deliberately unreachable (RFC 0008 scope rule
// 5); only previously declared functions remain visible. An import alias is
// never a value and is skipped entirely, so a bare alias reference fails as
// an unknown variable (RFC 0034).
func (names *scope) lookup(name string) (binding, lookupStatus) {
	for current := names; current != nil && current.local != nil; current = current.parent {
		if bound, ok := current.local[name]; ok {
			return bound, nameFound
		}
	}
	if bound, ok := names.module[name]; ok && bound.kind != aliasBinding {
		if names.inFunction() && bound.kind != functionBinding && bound.kind != genericFunctionBinding {
			return bound, nameModuleData
		}
		return bound, nameFound
	}
	return binding{}, nameMissing
}

// importAlias reports whether name is an import alias of the enclosing
// module. The module frame is shared by reference through every child scope,
// so one lookup reaches it at any depth; shadowing an alias is forbidden
// (RFC 0034).
func (names *scope) importAlias(name string) bool {
	bound, exists := names.module[name]
	return exists && bound.kind == aliasBinding
}

// importAliasTarget returns the canonical id of the module an import alias
// names, when that module's source is present in this compilation. The alias
// binding lives in the shared module frame and records its target (RFC
// 0034); a dangling alias whose target has no source resolves nowhere and
// keeps failing as an unknown variable until the module phase reports the
// missing path.
func (names *scope) importAliasTarget(name string) (string, bool) {
	bound, exists := names.module[name]
	if !exists || bound.kind != aliasBinding || bound.moduleID == "" {
		return "", false
	}
	if names.registry == nil {
		return "", false
	}
	_, present := names.registry.modules[bound.moduleID]
	return bound.moduleID, present
}

// declaredHere reports a duplicate in the innermost scope only, so a local may
// shadow a module-level value. An import alias is the exception: it is fixed
// for the whole module, so no nested scope may shadow one (RFC 0034).
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

func (names *scope) child() *scope {
	return &scope{
		module:   names.module,
		local:    make(map[string]binding),
		parent:   names,
		owner:    names.owner,
		result:   names.result,
		methods:  names.methods,
		self:     names.self,
		selfID:   names.selfID,
		function: names.function,
		nextID:   names.nextID,
		flow:     names.flow,
		generics: names.generics,
		registry: names.registry,
		moduleID: names.moduleID,
	}
}

func typeErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func moduleDataDiagnostic(owner, name string, token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, fmt.Sprintf("function %s cannot access module data binding %s; pass it as a parameter", owner, name))
}

func selfNotBoundDiagnostic(token lexer.Token) *compilerTypes.Diagnostic {
	diagnostic := typeErrorAt(token, "self is not bound outside an impl body")
	return &diagnostic
}
