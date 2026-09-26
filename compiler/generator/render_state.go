// render_state.go owns the shared rendering state: expressionValidation,
// generatedBinding, and their scope, binding, and name-resolution methods.
package generator

import (
	"fmt"

	"hexal/compiler/checker"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

type expressionValidation struct {
	expressions    map[*checker.Expression]bool
	objects        map[*checker.ObjectValue]bool
	variables      map[string]generatedBinding
	bindings       map[checker.BindingID]generatedBinding
	bindingNames   map[checker.BindingID]string
	activeScopes   []map[checker.BindingID]bool
	loopDepth      int
	usedNames      map[string]bool
	functions      map[string]compilerTypes.Type
	methods        map[string]checker.MethodDeclaration
	generatedTypes *generatedTypeValidation
	deferStack     [][]checker.DeferredAction
	// owner is the encoded module owner of the module being generated;
	// filename is its logical source key for #line directives; moduleID is
	// the module's canonical identity, used to distinguish foreign method
	// calls from local ones. table resolves a checked node's carried span to
	// the line and column #line directives and runtime failure sites embed.
	owner    string
	filename string
	moduleID string
	table    *span.Table
	// envPointer is the current entry-environment pointer expression ("&env"
	// in generated main, "env" inside an environment-dependent declaration),
	// or empty when no environment exists. envFunctions names the
	// environment-dependent functions whose calls pass it.
	envPointer       string
	envFunctions     map[string]bool
	envMethods       map[string]bool
	loopDepths       []int
	captureCounter   int
	returnCounter    int
	captures         map[*checker.Operand][]string
	matchCounter     int
	loopCounter      int // unique hex_for_N stem for for-in lowering
	tryCounter       int // unique hex_try_N stems for try prologue hoisting
	hoistedTries     map[*checker.Expression]string
	findCounter      int
	hoistedDictFinds map[*checker.Expression]string
	// interpolationCounter and hoistedInterpolations carry String.interpolate
	// prologues: each call's Heap and value captures, length measurement,
	// allocation, and ordered writes are emitted before the statement
	// renders, keyed by the interpolate node's own Operand (the Heap
	// sub-expression pointer), which is unique per call site and stable
	// across the value-copy the walker passes to each visit.
	interpolationCounter  int
	hoistedInterpolations map[*checker.Expression]string
	// hoistedInlineInterpolations holds the result of each String<N>.interpolate
	// call, keyed by its first segment's operand, which has no Heap to key on.
	hoistedInlineInterpolations map[*checker.Expression]string
	// spawnCounter and hoistedSpawns carry spawn prologues: each spawn's
	// argument frame and task handle are declared before the statement
	// renders, and the expression renders as the task handle.
	spawnCounter  int
	hoistedSpawns map[*checker.Expression]string
	// sequenceCounter and hoistedSequencing carry evaluation-order hoisting:
	// a compound C expression whose sub-parts would otherwise evaluate in an
	// unspecified order gets each sub-part hoisted into a named temporary in
	// written order, keyed by the checked tree's own pointer or
	// slice-indexed field so the later render lookup resolves to the same
	// node the hoist pass computed the key from.
	sequenceCounter   int
	hoistedSequencing map[*checker.Expression]string
	// registeredDefers records the deferred actions whose statements were
	// processed, in registration order. A try error branch may render
	// earlier than a later defer statement, and must not run it.
	registeredDefers []checker.DeferredAction
	printCounter     int              // unique hex_print_arg_N stems for print temporaries
	strings          *literalRegistry // payload lookup for checked string rendering
	tags             *tagRegistry     // program-wide discriminant lookups
}

type generatedBinding struct {
	typ     compilerTypes.Type
	mutable bool
}

// newExpressionValidation returns a rendering/validation state with every
// per-walk map created. Constructing through this function makes the state
// total: no production path can write to a nil map, so the lazy
// initialization that used to sit before each first write is not
// load-bearing. The caller sets the injected fields (functions, methods,
// generatedTypes, strings, tags, table, owner, filename, moduleID,
// envFunctions, envMethods) afterward. A state that must stay deliberately
// partial is built as a struct literal, which makes it an explicit
// malformed-state fixture rather than an accidental one.
func newExpressionValidation() *expressionValidation {
	return &expressionValidation{
		expressions:                 make(map[*checker.Expression]bool),
		objects:                     make(map[*checker.ObjectValue]bool),
		variables:                   make(map[string]generatedBinding),
		bindings:                    make(map[checker.BindingID]generatedBinding),
		bindingNames:                make(map[checker.BindingID]string),
		usedNames:                   make(map[string]bool),
		envFunctions:                make(map[string]bool),
		envMethods:                  make(map[string]bool),
		captures:                    make(map[*checker.Operand][]string),
		hoistedTries:                make(map[*checker.Expression]string),
		hoistedDictFinds:            make(map[*checker.Expression]string),
		hoistedInterpolations:       make(map[*checker.Expression]string),
		hoistedInlineInterpolations: make(map[*checker.Expression]string),
		hoistedSpawns:               make(map[*checker.Expression]string),
		hoistedSequencing:           make(map[*checker.Expression]string),
	}
}

// position resolves one carried span through the compilation's source table.
// A nil table yields the zero position, so a hand-built checked program with
// no source keeps the historical 0:0 site and suppresses a #line directive.
func (state *expressionValidation) position(s span.Span) span.Position {
	if state == nil || state.table == nil {
		return span.Position{}
	}
	return state.table.Position(s)
}

func (state *expressionValidation) line(s span.Span) int   { return state.position(s).Line }
func (state *expressionValidation) column(s span.Span) int { return state.position(s).Column }

func (state *expressionValidation) pushScope() {
	state.activeScopes = append(state.activeScopes, make(map[checker.BindingID]bool))
}

func (state *expressionValidation) popScope() {
	if len(state.activeScopes) > 0 {
		state.activeScopes = state.activeScopes[:len(state.activeScopes)-1]
	}
}

func (state *expressionValidation) bindingActive(id checker.BindingID) bool {
	for index := len(state.activeScopes) - 1; index >= 0; index-- {
		if state.activeScopes[index][id] {
			return true
		}
	}
	return false
}

func (state *expressionValidation) allocateBinding(id checker.BindingID, sourceName string, typ compilerTypes.Type, mutable bool) (string, error) {
	if id == 0 {
		state.variables[sourceName] = generatedBinding{typ: typ, mutable: mutable}
		return privateCName(valueName, sourceName, ""), nil
	}
	if _, exists := state.bindings[id]; exists {
		return "", unknownExpressionDiagnostic()
	}
	base := privateCName(valueName, sourceName, "")
	name := base
	for suffix := 2; state.usedNames[name]; suffix++ {
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
	state.usedNames[name] = true
	state.bindings[id] = generatedBinding{typ: typ, mutable: mutable}
	state.bindingNames[id] = name
	if len(state.activeScopes) == 0 {
		// Every production owner pushes its root scope before allocating a
		// binding; reaching here without one is a structural generator defect,
		// so it fails closed instead of inventing a scope the caller never
		// balances.
		return "", unknownExpressionDiagnostic()
	}
	state.activeScopes[len(state.activeScopes)-1][id] = true
	return name, nil
}

// registerCapture registers one captured entry-root binding under an explicit
// C name, so a function body's reads and writes render against the environment
// field rather than a local.
func (state *expressionValidation) registerCapture(capture checker.Capture, cName string) error {
	if _, exists := state.bindings[capture.Binding]; exists {
		return unknownExpressionDiagnostic()
	}
	state.bindings[capture.Binding] = generatedBinding{typ: capture.Type, mutable: capture.Mutable}
	state.bindingNames[capture.Binding] = cName
	if len(state.activeScopes) == 0 {
		state.pushScope()
	}
	state.activeScopes[len(state.activeScopes)-1][capture.Binding] = true
	return nil
}

func (state *expressionValidation) bindingFor(node checker.Expression) (generatedBinding, bool) {
	if node.Binding != 0 {
		if !state.bindingActive(node.Binding) {
			return generatedBinding{}, false
		}
		binding, ok := state.bindings[node.Binding]
		return binding, ok
	}
	binding, ok := state.variables[node.Name]
	return binding, ok
}

func (state *expressionValidation) cNameFor(node checker.Expression) (string, bool) {
	if node.Binding != 0 {
		name, ok := state.bindingNames[node.Binding]
		if !ok || !state.bindingActive(node.Binding) {
			return "", false
		}
		return name, true
	}
	return privateCName(valueName, node.Name, ""), true
}
