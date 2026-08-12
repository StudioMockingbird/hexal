package checker

// Function declarations, method declarations, calls, returns, and the closed
// function scope described by RFC 0008.

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// FunctionDeclaration is a checked module-level function. Type is the Fun<...>
// type its name produces in a value position; Result is nil when the function
// returns no value.
type FunctionDeclaration struct {
	Name         string
	Parameters   []FunctionParameter
	Result       *compilerTypes.Type
	ResultUse    *compilerTypes.TypeUse
	Type         compilerTypes.Type
	Body         []Statement
	Defers       []DeferredAction
	SourceLine   int
	SourceColumn int
}

func (FunctionDeclaration) statementNode() {}

// FunctionParameter is one resolved parameter. Parameters are fixed bindings in
// this RFC, so no mutability field exists.
type FunctionParameter struct {
	Name         string
	Binding      BindingID
	Type         compilerTypes.Type
	TypeUse      compilerTypes.TypeUse
	SourceLine   int
	SourceColumn int
}

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

// bindingKind separates storage from a declared function. A function name is
// not a place: it can be read as a Fun<...> value and nothing else.
type bindingKind uint8

const (
	dataBinding bindingKind = iota
	functionBinding
	genericFunctionBinding
)

// scope is one lexical name frame. Module bindings remain in module and are
// intentionally hidden from function bodies; control-flow frames chain to
// their enclosing frame so branch declarations do not escape their block.
type scope struct {
	module       map[string]binding
	local        map[string]binding // nil only at module level
	parent       *scope
	owner        string // enclosing function or method name, for diagnostics
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
}

func moduleScope() *scope {
	next := BindingID(0)
	return &scope{module: make(map[string]binding), methods: newMethodTable(), nextID: &next, flow: newFlowState(), generics: newGenericTable()}
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
// 5); only previously declared functions remain visible.
func (names *scope) lookup(name string) (binding, lookupStatus) {
	for current := names; current != nil && current.local != nil; current = current.parent {
		if bound, ok := current.local[name]; ok {
			return bound, nameFound
		}
	}
	if bound, ok := names.module[name]; ok {
		if names.inFunction() && bound.kind != functionBinding && bound.kind != genericFunctionBinding {
			return bound, nameModuleData
		}
		return bound, nameFound
	}
	return binding{}, nameMissing
}

// declaredHere reports a duplicate in the innermost scope only, so a local may
// shadow a module-level value.
func (names *scope) declaredHere(name string) bool {
	if names.local != nil {
		_, exists := names.local[name]
		return exists
	}
	_, exists := names.module[name]
	return exists
}

func (names *scope) define(name string, bound binding) {
	if names.local != nil {
		names.local[name] = bound
		return
	}
	names.module[name] = bound
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

// checkFunctionDeclaration follows RFC 0008's single-pass order exactly:
// resolve the complete signature, bind the name, then check the body with
// everything visible at this source position. Binding before the body is what
// makes direct self-recursion work; no later signature is collected.
func checkFunctionDeclaration(declaration parser.FunctionDeclaration, names *scope, typeEnvironment *compilerTypes.Environment, analyzeReturns bool) (FunctionDeclaration, compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	checked := FunctionDeclaration{
		Name:         name,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)

	if name == "print" {
		// RFC 0030: the protected builtin name cannot be bound by a
		// function declaration.
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.NameError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "print is a protected built-in name",
		})
	}
	if layoutBuiltins[name] {
		// RFC 0042: the layout query names cannot be bound by a function
		// declaration.
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.NameError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  name + " is a protected built-in name",
		})
	}
	if name == "Stdio" {
		// RFC 0040: the intrinsic qualifier is not a function and cannot be
		// redeclared.
		diagnostics = append(diagnostics, compilerTypes.Diagnostic{
			Category: compilerTypes.NameError,
			Stage:    "checker",
			Line:     declaration.Name.Line,
			Column:   declaration.Name.Column,
			Message:  "Stdio is a protected intrinsic qualifier",
		})
	}
	if compilerTypes.IsProtectedTypeName(name) || typeEnvironment.Contains(name) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "value "+name+" is already declared as a type"))
	} else if names.declaredHere(name) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, name+" is already declared"))
	} else if method, taken := names.methods.cNames[name]; taken {
		// hex_f_ is not injective: Point_translate and impl Point.translate
		// share one private C spelling, so one of them has to go.
		diagnostics = append(diagnostics, collisionDiagnostic(name, method, declaration.Name))
	}

	if len(diagnostics) == 0 && len(declaration.TypeParameters) > 0 {
		return checked, registerGenericFunction(declaration, names, typeEnvironment)
	}

	parameters, parameterDiagnostics := checkParameters(declaration.Parameters, typeEnvironment, names.generics)
	diagnostics = append(diagnostics, parameterDiagnostics...)
	parameterTypes := make([]compilerTypes.Type, 0, len(parameters))
	for _, parameter := range parameters {
		parameterTypes = append(parameterTypes, parameter.Type)
	}

	result, resultUse, resultDiagnostics := checkResultType(declaration.Return, declaration.Name, typeEnvironment, names.generics)
	diagnostics = append(diagnostics, resultDiagnostics...)

	// An incomplete signature cannot be bound, so the body is not checked
	// either: every name in it would resolve against a fiction.
	if len(diagnostics) > 0 {
		return checked, diagnostics
	}

	functionType := typeEnvironment.FunType(parameterTypes, result)
	if functionType.Signature == nil {
		return checked, compilerTypes.Diagnostics{{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "could not construct the function type for " + name,
		}}
	}
	checked.Parameters = parameters
	checked.Result = result
	checked.ResultUse = resultUse
	checked.Type = functionType

	parameterUses := make([]compilerTypes.TypeUse, 0, len(parameters))
	for _, parameter := range parameters {
		parameterUses = append(parameterUses, parameter.TypeUse)
	}
	functionUse := compilerTypes.FunctionTypeUse(functionType, parameterUses, resultUse)
	names.module[name] = binding{typ: functionType, use: functionUse, kind: functionBinding}

	body := &scope{
		module:    names.module,
		local:     make(map[string]binding, len(parameters)),
		owner:     name,
		result:    result,
		resultUse: resultUse,
		methods:   names.methods,
		function:  true,
		nextID:    names.nextID,
		flow:      newFlowState(),
		generics:  names.generics,
	}
	for index := range parameters {
		parameters[index].Binding = names.newBindingID()
		bound := binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
		body.local[parameters[index].Name] = bound
	}
	checked.Parameters = parameters

	statements, bodyDiagnostics := checkBody(declaration.Body, body, typeEnvironment)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = statements
	checked.Defers = append(checked.Defers, body.defers...)

	if analyzeReturns && result != nil && len(bodyDiagnostics) == 0 && FallsThrough(checked.Body) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.End,
			fmt.Sprintf("returning %s may fall through without returning %s", name, result.Name)))
	}
	return checked, diagnostics
}

// MethodDeclaration is a checked `impl` method. Object is the nominal object
// the method is associated with, whichever receiver form was written; SelfType
// is that written form and is the type of the implicit `self` binding.
// A method is not a value, so unlike a function it carries no Fun<...> type.
type MethodDeclaration struct {
	Name         string
	Object       *compilerTypes.ObjectType
	SelfType     compilerTypes.Type
	SelfBinding  BindingID
	Parameters   []FunctionParameter
	Result       *compilerTypes.Type
	ResultUse    *compilerTypes.TypeUse
	Body         []Statement
	Defers       []DeferredAction
	SourceLine   int
	SourceColumn int
}

func (MethodDeclaration) statementNode() {}

// methodTable holds every method declared so far. Methods are keyed by the
// canonical *ObjectType -- the nominal identity itself -- and never by display
// name: aliases share one ObjectType, so `impl Point.f` and `impl Coord.f` are
// one method, while two distinct objects that happen to print the same name
// stay separate.
type methodTable struct {
	byObject map[*compilerTypes.ObjectType]map[string]*MethodDeclaration
	// cNames maps the private C spelling stem a method produces
	// (Point_translate for impl Point.translate) to that method's source
	// spelling. The hex_f_ encoding is not injective, so this is what lets a
	// colliding free function name both declarations (RFC 0008, C23 lowering).
	cNames map[string]string
}

func newMethodTable() *methodTable {
	return &methodTable{
		byObject: make(map[*compilerTypes.ObjectType]map[string]*MethodDeclaration),
		cNames:   make(map[string]string),
	}
}

func (table *methodTable) lookup(object *compilerTypes.ObjectType, name string) *MethodDeclaration {
	if table == nil || object == nil {
		return nil
	}
	return table.byObject[object][name]
}

func (table *methodTable) define(method *MethodDeclaration) {
	methods, ok := table.byObject[method.Object]
	if !ok {
		methods = make(map[string]*MethodDeclaration)
		table.byObject[method.Object] = methods
	}
	methods[method.Name] = method
	table.cNames[method.Object.Name+"_"+method.Name] = method.Object.Name + "." + method.Name
}

func collisionDiagnostic(functionName, methodName string, token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, fmt.Sprintf("free function %s collides with impl %s", functionName, methodName))
}

func methodCollisionDiagnostic(objectName, methodName, existing string, token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, fmt.Sprintf("impl %s.%s collides with impl %s", objectName, methodName, existing))
}

// checkImplDeclaration follows the same single-pass order as a function:
// resolve the target and the signature, register the method, then check the
// body. Registering first is what makes self-recursion resolve while keeping a
// later method invisible.
func checkImplDeclaration(declaration parser.ImplDeclaration, names *scope, typeEnvironment *compilerTypes.Environment, analyzeReturns bool) (MethodDeclaration, compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	checked := MethodDeclaration{
		Name:         name,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)

	// A generic method is registered as an open template before any receiver
	// resolution: the receiver names the generic owner's parameters.
	if len(declaration.TypeParameters) > 0 || isGenericReceiver(declaration.SelfType) {
		return checked, registerGenericMethod(declaration, names, typeEnvironment)
	}

	target, targetDiagnostic := resolveType(declaration.SelfType, declaration.Keyword, typeEnvironment, names.generics)
	if targetDiagnostic != nil {
		return checked, compilerTypes.Diagnostics{*targetDiagnostic}
	}
	// Method rule 1 admits T, Ptr<T>, and MutPtr<T>. A nullable union is not
	// a receiver form: `self` would hold the union and every use would need a
	// narrowing the method body can never prove, so the target is rejected.
	if compilerTypes.IsNullable(target) {
		return checked, compilerTypes.Diagnostics{typeErrorAt(declaration.Keyword,
			fmt.Sprintf("impl requires T, Ptr<T>, or MutPtr<T>; got %s", target.Name))}
	}
	// Method rule 1: the target is T, Ptr<T>, or MutPtr<T> for a declared
	// nominal object T. Alias resolution already happened in resolveType.
	object := target.Object
	if object == nil && target.Element != nil {
		object = target.Element.Object
	}
	if object == nil {
		return checked, compilerTypes.Diagnostics{typeErrorAt(declaration.Keyword,
			target.Name+" is not a nominal object type; impl requires an object")}
	}
	checked.Object = object
	checked.SelfType = target

	// Method rules 4 and 5, then the non-injective C name rule.
	if names.methods.lookup(object, name) != nil {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, object.Name+" already has a method named "+name))
	} else if _, exists := object.Member(name); exists {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, object.Name+" already has a member named "+name))
	} else if bound, declared := names.module[object.Name+"_"+name]; declared && bound.kind == functionBinding {
		diagnostics = append(diagnostics, collisionDiagnostic(object.Name+"_"+name, object.Name+"."+name, declaration.Name))
	} else if existing, taken := names.methods.cNames[object.Name+"_"+name]; taken {
		diagnostics = append(diagnostics, methodCollisionDiagnostic(object.Name, name, existing, declaration.Name))
	}

	parameters, parameterDiagnostics := checkParameters(declaration.Parameters, typeEnvironment, names.generics)
	diagnostics = append(diagnostics, parameterDiagnostics...)

	result, resultUse, resultDiagnostics := checkResultType(declaration.Return, declaration.Name, typeEnvironment, names.generics)
	diagnostics = append(diagnostics, resultDiagnostics...)
	checked.Result = result
	checked.ResultUse = resultUse
	checked.Parameters = parameters

	if len(diagnostics) > 0 {
		return checked, diagnostics
	}

	names.methods.define(&checked)

	selfID := names.newBindingID()
	checked.SelfBinding = selfID
	body := &scope{
		module:    names.module,
		local:     make(map[string]binding, len(parameters)),
		owner:     name,
		result:    result,
		resultUse: resultUse,
		methods:   names.methods,
		self:      &checked.SelfType,
		selfID:    selfID,
		function:  true,
		nextID:    names.nextID,
		flow:      newFlowState(),
		generics:  names.generics,
	}
	for index := range parameters {
		parameters[index].Binding = names.newBindingID()
		bound := binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
		body.local[parameters[index].Name] = bound
	}
	checked.Parameters = parameters

	statements, bodyDiagnostics := checkBody(declaration.Body, body, typeEnvironment)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = statements
	checked.Defers = append(checked.Defers, body.defers...)

	if analyzeReturns && result != nil && len(bodyDiagnostics) == 0 && FallsThrough(checked.Body) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.End,
			fmt.Sprintf("returning %s may fall through without returning %s", name, result.Name)))
	}
	return checked, diagnostics
}

// checkParameters resolves one written parameter list. Functions and methods
// share it; `self` is implicit and never appears here.
func checkParameters(written []parser.Parameter, typeEnvironment *compilerTypes.Environment, generics *genericTable) ([]FunctionParameter, compilerTypes.Diagnostics) {
	parameters := make([]FunctionParameter, 0, len(written))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(written))
	for _, parameter := range written {
		parameterName := parameter.Name.Lexeme
		if compilerTypes.IsProtectedTypeName(parameterName) || typeEnvironment.Contains(parameterName) {
			diagnostics = append(diagnostics, typeErrorAt(parameter.Name, "value "+parameterName+" is already declared as a type"))
			continue
		}
		if seen[parameterName] {
			diagnostics = append(diagnostics, typeErrorAt(parameter.Name, "parameter "+parameterName+" is declared more than once"))
			continue
		}
		resolvedUse, diagnostic := resolveTypeUse(parameter.Type, parameter.Name, typeEnvironment, generics)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		resolved := resolvedUse.Type
		if diagnostic := valueTypeDiagnostic(parameter.Type, parameter.Name, resolved); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		seen[parameterName] = true
		parameters = append(parameters, FunctionParameter{
			Name:         parameterName,
			Type:         resolved,
			TypeUse:      resolvedUse,
			SourceLine:   parameter.Name.Line,
			SourceColumn: parameter.Name.Column,
		})
	}
	return parameters, diagnostics
}

// checkResultType resolves an optional return clause. A nil expression is a
// no-return declaration; a Fun<...> result is a deferred position.
func checkResultType(written parser.TypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (*compilerTypes.Type, *compilerTypes.TypeUse, compilerTypes.Diagnostics) {
	if written == nil {
		return nil, nil, nil
	}
	resolvedUse, diagnostic := resolveTypeUse(written, fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return nil, nil, compilerTypes.Diagnostics{*diagnostic}
	}
	resolved := resolvedUse.Type
	if unknownDiagnostic := valueTypeDiagnostic(written, fallback, resolved); unknownDiagnostic != nil {
		return nil, nil, compilerTypes.Diagnostics{*unknownDiagnostic}
	}
	if resolved.Signature != nil {
		// Supported-position whitelist: a Fun<...> result needs C declarator
		// rules this RFC defers.
		return nil, nil, compilerTypes.Diagnostics{typeErrorAt(fallback, "returning "+resolved.Name+" is not supported")}
	}
	return &resolved, &resolvedUse, nil
}

// FallsThrough reports whether a checked statement sequence has a normal path
// that can reach its end. The generator reuses this conservative proof when it
// validates forged checked programs.
func FallsThrough(statements []Statement) bool {
	for _, statement := range statements {
		if !statementFallsThrough(statement) {
			return false
		}
	}
	return true
}

func statementFallsThrough(statement Statement) bool {
	switch statement := statement.(type) {
	case ReturnStatement:
		return false
	case IfStatement:
		if statement.Else == nil || FallsThrough(statement.Then) {
			return true
		}
		for _, branch := range statement.ElseIf {
			if FallsThrough(branch.Body) {
				return true
			}
		}
		return FallsThrough(statement.Else)
	case WhileStatement, ForStatement:
		return true
	case BreakStatement, ContinueStatement:
		return true
	default:
		return true
	}
}

// checkBody checks a function or method body with no active loop at entry.
func checkBody(statements []parser.Statement, names *scope, typeEnvironment *compilerTypes.Environment) ([]Statement, compilerTypes.Diagnostics) {
	return checkStatements(statements, names, typeEnvironment, 0)
}

// checkStatements recursively checks one lexical statement sequence. A child
// scope is supplied by the control-flow handlers; declarations are installed
// only in the current frame after their own diagnostics have cleared.
func checkStatements(statements []parser.Statement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) ([]Statement, compilerTypes.Diagnostics) {
	checked := make([]Statement, 0, len(statements))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, statement := range statements {
		checkedStatement, declaredBinding, define, statementDiagnostics := checkStatement(statement, names, typeEnvironment, loopDepth)
		diagnostics = append(diagnostics, statementDiagnostics...)
		if len(statementDiagnostics) == 0 {
			if define {
				declaration := statement.(parser.Declaration)
				names.define(declaration.Name.Lexeme, declaredBinding)
			}
			checked = append(checked, checkedStatement)
		}
	}
	return checked, diagnostics
}

func checkStatement(statement parser.Statement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) (Statement, binding, bool, compilerTypes.Diagnostics) {
	switch statement := statement.(type) {
	case parser.Declaration:
		checked, declared, diagnostics := checkDeclaration(statement, names, typeEnvironment)
		return checked, declared, true, diagnostics
	case parser.Assignment:
		checked, diagnostics := checkAssignment(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.CallExpression:
		checked, diagnostics := checkCallStatement(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.ReturnStatement:
		checked, diagnostics := checkReturnStatement(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.IfStatement:
		checked, diagnostics := checkIfStatement(statement, names, typeEnvironment, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.WhileStatement:
		checked, diagnostics := checkWhileStatement(statement, names, typeEnvironment, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.ForStatement:
		checked, diagnostics := checkForStatement(statement, names, typeEnvironment, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.BreakStatement:
		if loopDepth == 0 {
			return BreakStatement{}, binding{}, false, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "break is only valid inside a loop")}
		}
		return BreakStatement{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}, binding{}, false, nil
	case parser.ContinueStatement:
		if loopDepth == 0 {
			return ContinueStatement{}, binding{}, false, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "continue is only valid inside a loop")}
		}
		return ContinueStatement{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}, binding{}, false, nil
	case parser.DeferStatement:
		checked, diagnostics := checkDeferStatement(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.ErrdeferStatement:
		checked, diagnostics := checkErrdeferStatement(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	default:
		return nil, binding{}, false, compilerTypes.Diagnostics{{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "unsupported checked control-flow statement",
		}}
	}
}

func checkCondition(expression parser.Expression, names *scope, typeEnvironment *compilerTypes.Environment) (Operand, lexer.Token, compilerTypes.Diagnostics) {
	checked := checkValue(expression, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return checked.source, checked.token, diagnostics
	}
	// RFC 0023: every value-producing expression is a valid condition; its
	// truthiness decides the branch. No-result calls are rejected by
	// checkValue before this point.
	return checked.source, checked.token, nil
}

// narrowingFact is the branch-local fact a checked null test proves about one
// binding: typ holds in the true branch, other holds in the false branch.
type narrowingFact struct {
	binding BindingID
	typ     compilerTypes.Type
	other   compilerTypes.Type
}

// conditionNarrowing extracts a branch-local fact from an explicit Nil or
// exact-member test. Only a bare local binding narrows; member paths and
// logical combinations remain non-narrowable.
func conditionNarrowing(condition Operand, state *flowState, typeEnvironment *compilerTypes.Environment) *narrowingFact {
	if state == nil || condition.Kind != ExpressionOperand {
		return nil
	}
	node := condition.Node
	if node.Kind != NullTestExpression && node.Kind != UnionTestExpression {
		return nil
	}
	operand := node.Operand
	if operand == nil || operand.Kind != VariableExpression || operand.Binding == 0 {
		return nil
	}
	if fact, exists := state.facts[operand.Binding]; exists && fact.escaped {
		return nil
	}
	if node.Kind == UnionTestExpression {
		other, ok := compilerTypes.RemoveUnionMember(typeEnvironment, node.OperandType, node.TestType)
		if !ok {
			return nil
		}
		return &narrowingFact{binding: operand.Binding, typ: node.TestType, other: other}
	}
	other, ok := compilerTypes.RemoveUnionMember(typeEnvironment, node.OperandType, compilerTypes.Nil)
	if !ok {
		return nil
	}
	if node.Operator == NotEqualOperator {
		return &narrowingFact{binding: operand.Binding, typ: other, other: compilerTypes.Nil}
	}
	if node.Operator == EqualOperator {
		return &narrowingFact{binding: operand.Binding, typ: compilerTypes.Nil, other: other}
	}
	return nil
}

func checkIfStatement(statement parser.IfStatement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) (IfStatement, compilerTypes.Diagnostics) {
	checked := IfStatement{
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
		EndLine:      statement.End.Line,
		EndColumn:    statement.End.Column,
		ElseLine:     statement.ElseKeyword.Line,
		ElseColumn:   statement.ElseKeyword.Column,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	condition, conditionToken, conditionDiagnostics := checkCondition(statement.Condition, names, typeEnvironment)
	diagnostics = append(diagnostics, conditionDiagnostics...)
	checked.Condition = condition
	checked.ConditionLine = conditionToken.Line
	checked.ConditionColumn = conditionToken.Column

	// Each branch checks a clone of the pre-test flow carrying its own
	// narrowing fact. Invalidations from a clean branch merge into the
	// pre-test flow only after every branch is checked, so the else side of
	// the construct never observes the then side's effects. Cloning is
	// unconditional so owning-state transitions inside one branch can never
	// leak into a sibling or the continuing path; the strict owner merge
	// then detects disagreements exactly.
	var fact *narrowingFact
	if len(conditionDiagnostics) == 0 {
		fact = conditionNarrowing(condition, names.flow, typeEnvironment)
	}
	parentState := names.flow
	thenState := parentState
	elseState := parentState
	if parentState != nil {
		thenState = parentState.clone()
		elseState = parentState.clone()
		if fact != nil {
			thenState.narrow(fact.binding, fact.typ)
			elseState.narrow(fact.binding, fact.other)
		}
	}

	thenScope := names.child()
	thenScope.flow = thenState
	thenBody, thenDiagnostics := checkStatements(statement.Then, thenScope, typeEnvironment, loopDepth)
	diagnostics = append(diagnostics, thenDiagnostics...)
	checked.Then = thenBody
	checked.ThenDefers = append(checked.ThenDefers, thenScope.defers...)

	for _, branch := range statement.ElseIf {
		// An elseif condition is checked where every previous condition was
		// false, so its state is the else-side chain; each body narrows a
		// clone of that chain and only its invalidations merge onward.
		conditionScope := names.child()
		conditionScope.flow = elseState
		branchCondition, branchToken, branchConditionDiagnostics := checkCondition(branch.Condition, conditionScope, typeEnvironment)
		diagnostics = append(diagnostics, branchConditionDiagnostics...)
		// Always clone the else-side chain for the branch body: its own
		// invalidations must not leak into the next elseif condition, and they
		// must still merge into the pre-test flow even when this condition
		// narrows nothing (otherwise a missing final else would drop them).
		branchState := elseState
		if elseState != nil {
			branchState = elseState.clone()
			if len(branchConditionDiagnostics) == 0 {
				if branchFact := conditionNarrowing(branchCondition, elseState, typeEnvironment); branchFact != nil {
					branchState.narrow(branchFact.binding, branchFact.typ)
					nextElseState := elseState.clone()
					nextElseState.narrow(branchFact.binding, branchFact.other)
					elseState = nextElseState
				}
			}
		}
		branchScope := names.child()
		branchScope.flow = branchState
		branchBody, branchDiagnostics := checkStatements(branch.Body, branchScope, typeEnvironment, loopDepth)
		diagnostics = append(diagnostics, branchDiagnostics...)
		checked.ElseIfDefers = append(checked.ElseIfDefers, append([]DeferredAction(nil), branchScope.defers...))
		checked.ElseIf = append(checked.ElseIf, IfBranch{
			Condition:       branchCondition,
			ConditionLine:   branchToken.Line,
			ConditionColumn: branchToken.Column,
			Body:            branchBody,
			SourceLine:      branch.Keyword.Line,
			SourceColumn:    branch.Keyword.Column,
		})
		if len(branchDiagnostics) == 0 && elseState != nil && parentState != nil {
			parentState.mergeBranch(branchState)
		}
	}
	if statement.Else != nil {
		elseScope := names.child()
		elseScope.flow = elseState
		elseBody, elseDiagnostics := checkStatements(statement.Else, elseScope, typeEnvironment, loopDepth)
		diagnostics = append(diagnostics, elseDiagnostics...)
		checked.Else = elseBody
		checked.ElseDefers = append(checked.ElseDefers, elseScope.defers...)
		if len(elseDiagnostics) == 0 && elseState != parentState && parentState != nil {
			parentState.mergeBranch(elseState)
		}
	}
	if len(thenDiagnostics) == 0 && thenState != parentState && parentState != nil {
		parentState.mergeBranch(thenState)
	}
	// Early-exit narrowing: when the then chain terminates the current path
	// (break, continue, or return), the else-side chain is the only
	// continuation, so its narrowings become the continuing state. This is
	// the pattern behind `if step is EoS break end` followed by an element
	// use (RFC 0031).
	if len(thenDiagnostics) == 0 && thenState != parentState && parentState != nil &&
		sequenceTerminates(checked.Then) && elseState != nil {
		parentState.adopt(elseState)
	}
	return checked, diagnostics
}

// sequenceTerminates reports whether a checked statement sequence provably
// ends the current path with break, continue, or return before its end.
func sequenceTerminates(statements []Statement) bool {
	for _, statement := range statements {
		if statementTerminates(statement) {
			return true
		}
	}
	return false
}

func statementTerminates(statement Statement) bool {
	switch statement := statement.(type) {
	case ReturnStatement, BreakStatement, ContinueStatement:
		return true
	case IfStatement:
		// Every branch must terminate for the if itself to terminate.
		if statement.Else == nil {
			return false
		}
		if !sequenceTerminates(statement.Then) {
			return false
		}
		for _, branch := range statement.ElseIf {
			if !sequenceTerminates(branch.Body) {
				return false
			}
		}
		return sequenceTerminates(statement.Else)
	default:
		return false
	}
}

// checkForStatement checks the RFC 0028 for-in form: the source must be one
// iterable concrete type, the binder arity must match the source kind, and
// every binder is a fresh immutable binding in a fresh body scope.
func checkForStatement(statement parser.ForStatement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) (ForStatement, compilerTypes.Diagnostics) {
	checked := ForStatement{
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)

	seen := make(map[string]bool, len(statement.Binders))
	for _, binder := range statement.Binders {
		if seen[binder.Lexeme] {
			diagnostics = append(diagnostics, compilerTypes.Diagnostic{
				Category: compilerTypes.NameError,
				Stage:    "checker",
				Line:     binder.Line,
				Column:   binder.Column,
				Message:  "duplicate loop binder name " + binder.Lexeme,
			})
		}
		seen[binder.Lexeme] = true
	}

	// The source is read as a value but keeps its place addressability when
	// it names storage: the generator iterates an Array place in place and
	// only materializes genuine temporaries (RFC 0028 source stabilization).
	var source checkedExpression
	switch statement.Source.(type) {
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		source = checkPlace(statement.Source, names, typeEnvironment)
	default:
		source = checkExpression(statement.Source, expressionContext{foldConstants: false}, names, typeEnvironment)
	}
	if source.diagnostic != nil {
		return checked, append(diagnostics, *source.diagnostic)
	}
	if diagnosticsFromSource := initializerDiagnostics(source); len(diagnosticsFromSource) > 0 {
		return checked, append(diagnostics, diagnosticsFromSource...)
	}

	binderTypes, arityDiagnostic := forBinderTypes(source.typ, statement.Binders)
	if arityDiagnostic != nil {
		return checked, append(diagnostics, *arityDiagnostic)
	}
	if len(binderTypes) != len(statement.Binders) {
		return checked, append(diagnostics, typeErrorAt(statement.Keyword, "for-in binder count does not match the source type"))
	}

	bodyScope := names.child()
	for index, binder := range statement.Binders {
		binderType := binderTypes[index]
		bound := binding{typ: binderType, use: compilerTypes.NewTypeUse(binderType), loopBinder: true, id: names.newBindingID()}
		bodyScope.local[binder.Lexeme] = bound
		checked.Binders = append(checked.Binders, ForBinder{
			Name:         binder.Lexeme,
			Type:         binderType,
			Binding:      bound.id,
			SourceLine:   binder.Line,
			SourceColumn: binder.Column,
		})
	}

	body, bodyDiagnostics := checkStatements(statement.Body, bodyScope, typeEnvironment, loopDepth+1)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = body
	checked.BodyDefers = append(checked.BodyDefers, bodyScope.defers...)
	checked.Source = source.source
	return checked, diagnostics
}

// forBinderTypes resolves the binder list for one iterable source type. The
// returned slice matches the written binders one to one; a count mismatch
// reports the RFC 0028 arity diagnostic.
func forBinderTypes(source compilerTypes.Type, binders []lexer.Token) ([]compilerTypes.Type, *compilerTypes.Diagnostic) {
	switch {
	case source.Array != nil || source.View != nil || source.List != nil:
		var element compilerTypes.Type
		if source.Array != nil {
			element = source.Array.Element
		} else if source.View != nil {
			element = source.View.Element
		} else {
			element = source.List.Element
		}
		switch len(binders) {
		case 1:
			return []compilerTypes.Type{element}, nil
		case 2:
			return []compilerTypes.Type{compilerTypes.SizeType, element}, nil
		default:
			diagnostic := typeErrorAt(binders[0], "sequence iteration requires one value binder or index and value binders")
			return nil, &diagnostic
		}
	case compilerTypes.IsString(source) || compilerTypes.IsStrand(source):
		switch len(binders) {
		case 1:
			return []compilerTypes.Type{compilerTypes.Rune}, nil
		case 2:
			return []compilerTypes.Type{compilerTypes.SizeType, compilerTypes.Rune}, nil
		default:
			diagnostic := typeErrorAt(binders[0], "sequence iteration requires one value binder or index and value binders")
			return nil, &diagnostic
		}
	case source.Dict != nil:
		switch len(binders) {
		case 2:
			return []compilerTypes.Type{source.Dict.Key, source.Dict.Value}, nil
		case 3:
			return []compilerTypes.Type{compilerTypes.SizeType, source.Dict.Key, source.Dict.Value}, nil
		default:
			diagnostic := typeErrorAt(binders[0], "dictionary iteration requires key and value binders or index, key, and value binders")
			return nil, &diagnostic
		}
	case source.Stream != nil:
		switch len(binders) {
		case 1:
			return []compilerTypes.Type{source.Stream.Element}, nil
		case 2:
			return []compilerTypes.Type{compilerTypes.SizeType, source.Stream.Element}, nil
		default:
			diagnostic := typeErrorAt(binders[0], "stream iteration requires one value binder or index and value binders")
			return nil, &diagnostic
		}
	default:
		diagnostic := typeErrorAt(binders[0], "value of type "+source.Name+" is not iterable")
		return nil, &diagnostic
	}
}

func checkWhileStatement(statement parser.WhileStatement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) (WhileStatement, compilerTypes.Diagnostics) {
	checked := WhileStatement{
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
		EndLine:      statement.End.Line,
		EndColumn:    statement.End.Column,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	condition, conditionToken, conditionDiagnostics := checkCondition(statement.Condition, names, typeEnvironment)
	diagnostics = append(diagnostics, conditionDiagnostics...)
	checked.Condition = condition
	checked.ConditionLine = conditionToken.Line
	checked.ConditionColumn = conditionToken.Column

	// The condition's narrowing holds for the body; nothing survives the
	// loop, so a cloned body state merges only its invalidations back.
	// Cloning is unconditional so owning-state transitions inside the body
	// cannot leak onto the continuing path and the back-edge invariant can
	// be checked exactly.
	bodyState := names.flow
	if names.flow != nil {
		bodyState = names.flow.clone()
		if len(conditionDiagnostics) == 0 {
			if fact := conditionNarrowing(condition, names.flow, typeEnvironment); fact != nil {
				bodyState.narrow(fact.binding, fact.typ)
			}
		}
	}
	bodyScope := names.child()
	bodyScope.flow = bodyState
	body, bodyDiagnostics := checkStatements(statement.Body, bodyScope, typeEnvironment, loopDepth+1)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = body
	checked.BodyDefers = append(checked.BodyDefers, bodyScope.defers...)
	if len(bodyDiagnostics) == 0 && bodyState != names.flow && names.flow != nil {
		names.flow.mergeBranch(bodyState)
	}
	return checked, diagnostics
}

func checkReturnStatement(statement parser.ReturnStatement, names *scope, typeEnvironment *compilerTypes.Environment) (ReturnStatement, compilerTypes.Diagnostics) {
	checked := ReturnStatement{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}
	if !names.inFunction() {
		return checked, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "return is only valid inside a function body")}
	}
	if statement.Value == nil {
		if names.result != nil {
			return checked, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword,
				fmt.Sprintf("return requires a value; %s declares %s", names.owner, names.result.Name))}
		}
		return checked, nil
	}
	if names.result == nil {
		return checked, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, names.owner+" returns no value; use a bare return")}
	}

	resultUse := compilerTypes.NewTypeUse(*names.result)
	if names.resultUse != nil {
		resultUse = *names.resultUse
	}
	value := checkInitializer(statement.Value, resultUse, statement.Keyword, names, typeEnvironment)
	if valueDiagnostics := initializerDiagnostics(value); len(valueDiagnostics) > 0 {
		return checked, valueDiagnostics
	}
	if value.typ != (compilerTypes.Type{}) && !assignable(*names.result, value.typ) {
		return checked, compilerTypes.Diagnostics{typeErrorAt(value.token,
			fmt.Sprintf("%s returns %s; got %s", names.owner, names.result.Name, value.typ.Name))}
	}
	if value.typ != (compilerTypes.Type{}) {
		if diagnostic := atomicCopyDiagnostic(value.source, statement.Keyword); diagnostic != nil {
			return checked, compilerTypes.Diagnostics{*diagnostic}
		}
	}
	source := value.source
	checked.Value = &source
	// RFC 0035: a collection return value is an ordinary shallow copy; the
	// caller accepts the cleanup responsibility the function documents.
	return checked, nil
}

func checkCallStatement(call parser.CallExpression, names *scope, typeEnvironment *compilerTypes.Environment) (CallStatement, compilerTypes.Diagnostics) {
	checked := checkCall(call, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return CallStatement{}, diagnostics
	}
	return CallStatement{
		Call:         checked.source,
		SourceLine:   checked.token.Line,
		SourceColumn: checked.token.Column,
	}, nil
}

// checkCall resolves a callee, checks arity, and checks each argument in its
// parameter's expected-type position so RFC 0003/0009 contextual literals and
// RFC 0007's MutPtr-to-Ptr weakening both apply. The returned type is the zero
// Type for a no-return callee; only a call statement accepts that.
func checkCall(call parser.CallExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if property, isMethod := call.Callee.(parser.PropertyExpression); isMethod {
		return checkMethodCall(call, property, names, typeEnvironment)
	}
	callee, ok := call.Callee.(parser.VariableExpression)
	if !ok {
		return checkedExpression{token: call.OpenParen, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     call.OpenParen.Line,
			Column:   call.OpenParen.Column,
			Message:  "a call's callee must be a function name or a method selection",
		}}
	}
	if callee.Name.Kind == lexer.Self {
		return checkedExpression{token: callee.Name, diagnostic: selfNotBoundDiagnostic(callee.Name)}
	}
	// RFC 0030: the protected builtin `print` resolves before ordinary
	// free-function lookup and cannot be redeclared or referenced as a
	// value.
	if callee.Name.Lexeme == "print" {
		return checkPrintCall(call, callee.Name, names, typeEnvironment)
	}
	// RFC 0042: the protected layout queries resolve before ordinary
	// free-function lookup.
	if layoutBuiltins[callee.Name.Lexeme] {
		return checkLayoutCall(call, callee.Name, names, typeEnvironment)
	}

	name := callee.Name.Lexeme
	bound, status := names.lookup(name)
	switch status {
	case nameMissing:
		diagnostic := typeErrorAt(callee.Name, "unknown function "+name+"; functions must be declared before use")
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	case nameModuleData:
		diagnostic := moduleDataDiagnostic(names.owner, name, callee.Name)
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	if bound.kind == genericFunctionBinding {
		return checkGenericCall(call, bound, name, callee.Name, names, typeEnvironment)
	}
	// RFC 0010: a call resolves the callee's effective type from the
	// branch-local flow facts, so a null test can narrow a nullable Fun<...>
	// binding to its callable member. The declared binding itself still holds
	// the nullable storage type.
	calleeType := bound.typ
	if bound.kind != functionBinding {
		if narrowed, ok := names.flow.narrowedType(bound.id); ok {
			calleeType = narrowed
		}
	}
	signature := calleeType.Signature
	if signature == nil {
		diagnostic := typeErrorAt(callee.Name, name+" is not callable")
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	if compilerTypes.IsNullable(calleeType) {
		// A nullable function pointer holds a Fun member only after a null
		// test proved it; calling the union itself could jump through nil.
		diagnostic := typeErrorAt(callee.Name, calleeType.Name+" may be Nil; narrow it before calling it")
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}
	if len(call.Arguments) != len(signature.Parameters) {
		diagnostic := typeErrorAt(callee.Name,
			fmt.Sprintf("%s expects %d arguments, got %d", name, len(signature.Parameters), len(call.Arguments)))
		return checkedExpression{token: callee.Name, diagnostic: &diagnostic}
	}

	parameterUses := bound.use.Parameters
	if len(parameterUses) != len(signature.Parameters) {
		parameterUses = make([]compilerTypes.TypeUse, len(signature.Parameters))
		for index, parameter := range signature.Parameters {
			parameterUses[index] = compilerTypes.NewTypeUse(parameter)
		}
	}
	arguments, diagnostics := checkArguments(name, parameterUses, call.Arguments, callee.Name, names, typeEnvironment)
	if len(diagnostics) > 0 {
		return checkedExpression{token: callee.Name, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}

	calleeNode := variableNodeWithBinding(name, bound.id)
	if bound.kind == functionBinding {
		calleeNode = Expression{Kind: FunctionReferenceExpression, Name: name, ResultType: bound.typ}
	}
	var resultType compilerTypes.Type
	if signature.Result != nil {
		resultType = *signature.Result
	}
	node := Expression{
		Kind:        CallExpression,
		Operand:     &calleeNode,
		Arguments:   arguments,
		OperandType: calleeType,
		ResultType:  resultType,
	}
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node},
		typ:    resultType,
		token:  callee.Name,
	}
}

// checkArguments checks each written argument in its parameter's expected-type
// position, so contextual literals and RFC 0007 weakening both apply. Callee
// is only used to spell diagnostics.
func checkArguments(callee string, expected []compilerTypes.TypeUse, written []parser.Expression, token lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) ([]Operand, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	arguments := make([]Operand, 0, len(written))
	for index, argument := range written {
		want := expected[index]
		checked := checkInitializer(argument, want, token, names, typeEnvironment)
		if argumentDiagnostics := initializerDiagnostics(checked); len(argumentDiagnostics) > 0 {
			diagnostics = append(diagnostics, argumentDiagnostics...)
			continue
		}
		if checked.typ != (compilerTypes.Type{}) && !assignable(want.Type, checked.typ) {
			diagnostics = append(diagnostics, typeErrorAt(checked.token,
				fmt.Sprintf("%s argument %d requires %s, got %s", callee, index+1, want.Type.Name, checked.typ.Name)))
			continue
		}
		if diagnostic := atomicCopyDiagnostic(checked.source, token); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		arguments = append(arguments, checked.source)
	}
	return arguments, diagnostics
}

// checkMethodCall resolves receiver.method(...). The receiver is checked as a
// place -- adaptation may need its address -- and the method is found by the
// receiver's nominal object identity, so all three receiver forms reach the
// one method declared on that object.
func checkMethodCall(call parser.CallExpression, callee parser.PropertyExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	// Heap.new() names the built-in type, not a Heap value binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Heap" {
		return checkHeapTypeCall(call, variable, names, typeEnvironment)
	}
	// String.from_bytes() names the built-in type, not a String value
	// binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "String" {
		return checkStringTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// List<T>.new() names the built-in generic type, not a List value
	// binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "List" {
		return checkListTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Dict<K, V>.new() names the built-in generic type, not a Dict value
	// binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Dict" {
		return checkDictTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Stream<T>.new() and Stream<T>.produce() name the built-in generic
	// type, not a Stream value binding (RFC 0031).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Stream" {
		return checkStreamTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// View<T>.from_pointer() and View<T>.empty() name the built-in generic
	// type, not a View value binding (RFC 0043).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "View" {
		return checkViewBridgeCall(call, variable.Name, names, typeEnvironment)
	}
	// File.open() names the built-in File type, not a File value binding
	// (RFC 0040).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "File" {
		return checkFileOpenCall(call, variable.Name, names, typeEnvironment)
	}
	// Stdio.stdin(), Stdio.stdout(), and Stdio.stderr() name the protected
	// intrinsic qualifier, not a value binding (RFC 0040).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Stdio" {
		return checkStdioCall(call, variable.Name, names, typeEnvironment)
	}
	// Task.yield() names the built-in Task type, not a Task value binding
	// (RFC 0037).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Task" {
		return checkTaskTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Channel<T>.new() names the built-in generic Channel type, not a
	// Channel value binding (RFC 0037).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Channel" {
		return checkChannelTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Mutex.new() names the built-in Mutex type, not a Mutex value binding
	// (RFC 0037).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Mutex" {
		return checkMutexTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Atomic<T>.new() names the built-in generic Atomic type, not an Atomic
	// value binding (RFC 0037).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Atomic" {
		return checkAtomicTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Int32.from_le_bytes(...) names a fixed-width integer type, not an
	// integer value binding (RFC 0032).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable &&
		(callee.Property.Lexeme == "from_le_bytes" || callee.Property.Lexeme == "from_be_bytes") {
		return checkEndianFromBytesCall(call, variable.Name, typeEnvironment, names)
	}
	// Error.new(...) names the built-in Error type, not an Error value
	// binding (RFC 0029).
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Error" {
		return checkErrorNewCall(call, variable.Name, names, typeEnvironment)
	}
	receiver := checkedExpression{}
	switch callee.Receiver.(type) {
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		receiver = checkPlace(callee.Receiver, names, typeEnvironment)
	default:
		// A call or literal receiver has no addressable storage of its own;
		// check it as a value so collection methods can reject temporary
		// roots with their own diagnostics.
		receiver = checkValue(callee.Receiver, names, typeEnvironment)
	}
	if receiverDiagnostics := initializerDiagnostics(receiver); len(receiverDiagnostics) > 0 {
		return receiver
	}
	// RFC 0038: the compiler-owned `to<Dest>()` conversion resolves on
	// eligible scalar receivers before user method lookup. A receiver
	// depending on a generic parameter defers to specialization.
	if name == "to" &&
		(compilerTypes.IsInteger(receiver.typ) || compilerTypes.IsFloat(receiver.typ) ||
			compilerTypes.IsRune(receiver.typ) || compilerTypes.ContainsTypeParameter(receiver.typ)) {
		return checkConversionCall(call, callee, receiver, names, typeEnvironment)
	}
	// RFC 0032: the compiler-owned `bit_cast<T>()` reinterprets same-width
	// fixed-representation scalar bits.
	if name == "bit_cast" && (bitCastEligibleType(receiver.typ) || compilerTypes.ContainsTypeParameter(receiver.typ)) {
		return checkBitCastCall(call, callee, receiver, names, typeEnvironment)
	}
	// RFC 0032: explicit-endian byte conversion instance methods on
	// fixed-width integer receivers.
	if (name == "to_le_bytes" || name == "to_be_bytes") && (compilerTypes.IsInteger(receiver.typ) || compilerTypes.ContainsTypeParameter(receiver.typ)) {
		return checkEndianToBytesCall(call, callee, receiver, names, typeEnvironment)
	}
	// RFC 0042: volatile integer accesses dispatch on pointer receivers. A
	// nullable receiver is excluded so the nullable-narrowing diagnostic
	// below owns it.
	if receiver.typ.Element != nil && !compilerTypes.IsNullable(receiver.typ) && (name == "read_volatile" || name == "write_volatile") {
		return checkVolatileCall(call, callee, receiver, names, typeEnvironment)
	}
	// Heap operations dispatch on the built-in receiver type.
	if compilerTypes.IsHeap(receiver.typ) {
		switch name {
		case "allocate":
			return checkHeapAllocate(call, callee, receiver, names, typeEnvironment)
		case "free":
			return checkHeapFree(call, callee, receiver, names, typeEnvironment)
		}
	}
	// Array and View methods dispatch on the built-in collection receiver
	// types.
	if receiver.typ.Array != nil || receiver.typ.View != nil {
		return checkCollectionMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// List.stream(h) builds a non-owning lazy Stream over the List (RFC
	// 0031); it dispatches before ordinary List methods.
	if receiver.typ.List != nil && name == "stream" {
		return checkListStreamCall(call, callee, receiver, names, typeEnvironment)
	}
	// List methods dispatch on the built-in list receiver type.
	if receiver.typ.List != nil {
		return checkListMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// Dict methods dispatch on the built-in dictionary receiver type.
	if receiver.typ.Dict != nil {
		return checkDictMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// Stream methods dispatch on the built-in stream receiver type (RFC
	// 0031): next, filter, map, take, and free.
	if receiver.typ.Stream != nil {
		return checkStreamMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// Task, Channel, Mutex, and Atomic methods dispatch on their built-in
	// handle receiver types (RFC 0037).
	if receiver.typ.Task != nil {
		return checkTaskMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	if receiver.typ.Channel != nil {
		return checkChannelMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	if compilerTypes.IsMutex(receiver.typ) {
		return checkMutexMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	if receiver.typ.Atomic != nil {
		return checkAtomicMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// String methods dispatch on the built-in String receiver type.
	if compilerTypes.IsString(receiver.typ) {
		return checkStringMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// Strand methods dispatch on the built-in Strand receiver type; the
	// surface is deliberately smaller than String's (RFC 0044).
	if compilerTypes.IsStrand(receiver.typ) {
		return checkStrandMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// RuneCursor methods dispatch on the RFC 0044 cursor descriptor type.
	if compilerTypes.IsRuneCursor(receiver.typ) {
		return checkRuneCursorMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// File methods dispatch on the RFC 0040 File handle type.
	if compilerTypes.IsFile(receiver.typ) {
		return checkFileMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// RFC 0010: a nullable receiver reaches no method until a null test
	// narrowed it to its pointer member.
	if compilerTypes.IsNullable(receiver.typ) {
		diagnostic := nullableAccessDiagnostic(receiver, callee.Property, placeDescription(callee.Receiver))
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}

	// Rule 2: one nominal object owns the method, reached through T, Ptr<T>,
	// or MutPtr<T>. Deeper pointers have no object at this layer and fail here.
	object := receiver.typ.Object
	if object == nil && receiver.typ.Element != nil {
		object = receiver.typ.Element.Object
	}
	if object == nil {
		diagnostic := typeErrorAt(callee.Property, receiver.typ.Name+" has no method named "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	method := names.methods.lookup(object, name)
	if method == nil {
		if genericMethod := lookupGenericMethod(names, object, name); genericMethod != nil {
			return checkGenericMethodCall(call, callee, genericMethod, object, receiver, names, typeEnvironment)
		}
		diagnostic := typeErrorAt(callee.Property, object.Name+" has no method named "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}

	adapted, diagnostic := adaptReceiver(receiver, *method, callee, typeEnvironment)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}

	if len(call.Arguments) != len(method.Parameters) {
		diagnostic := typeErrorAt(callee.Property,
			fmt.Sprintf("%s expects %d arguments, got %d", name, len(method.Parameters), len(call.Arguments)))
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	expected := make([]compilerTypes.TypeUse, 0, len(method.Parameters))
	for _, parameter := range method.Parameters {
		use := parameter.TypeUse
		if use.Type == (compilerTypes.Type{}) {
			use = compilerTypes.NewTypeUse(parameter.Type)
		}
		expected = append(expected, use)
	}
	arguments, diagnostics := checkArguments(name, expected, call.Arguments, callee.Property, names, typeEnvironment)
	if len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}

	var resultType compilerTypes.Type
	if method.Result != nil {
		resultType = *method.Result
	}
	node := Expression{
		Kind:        MethodCallExpression,
		Name:        name,
		Owner:       object,
		Operand:     &adapted.Node,
		Arguments:   arguments,
		OperandType: method.SelfType,
		ResultType:  resultType,
	}
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node},
		typ:    resultType,
		token:  callee.Property,
	}
}

// adaptReceiver applies RFC 0008's ordered receiver rules and returns the
// receiver already converted to the method's target form:
//
//  1. an exact target type is passed directly;
//  2. MutPtr<T> weakens to a Ptr<T> target;
//  3. Ptr<T> or MutPtr<T> dereferences to a copied T target; or
//  4. an addressable T uses ref for a Ptr<T> or MutPtr<T> target.
//
// Rule 4 is `ref` exactly as written source would be: a fixed place yields
// Ptr<T> and so cannot reach a MutPtr<T> method.
func adaptReceiver(receiver checkedExpression, method MethodDeclaration, callee parser.PropertyExpression, typeEnvironment *compilerTypes.Environment) (Operand, *compilerTypes.Diagnostic) {
	target := method.SelfType
	switch {
	case compilerTypes.Equal(target, receiver.typ), assignable(target, receiver.typ):
		return valueFromPlace(receiver).source, nil
	case receiver.typ.Element != nil && compilerTypes.Equal(*receiver.typ.Element, target):
		return valueFromPlace(dereferencePlace(receiver, callee.Property)).source, nil
	case target.Element != nil && compilerTypes.Equal(*target.Element, receiver.typ):
		pointer := typeEnvironment.PtrType(receiver.typ)
		if receiver.source.Writable {
			pointer = typeEnvironment.MutPtrType(receiver.typ)
		}
		if !assignable(target, pointer) {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("%s needs %s; ref %s is %s",
				method.Name, target.Name, placeDescription(callee.Receiver), pointer.Name))
			return Operand{}, &diagnostic
		}
		return Operand{
			Kind: VariableOperand,
			Type: pointer,
			Node: unaryNode(AddressOfExpression, receiver.source.Node),
		}, nil
	}
	diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("%s needs %s; %s is %s",
		method.Name, target.Name, placeDescription(callee.Receiver), receiver.typ.Name))
	return Operand{}, &diagnostic
}

// checkCallValue is the value-position wrapper: a callee that returns nothing
// has no value to bind, so it is rejected here rather than reaching an
// initializer with a zero type.
func checkCallValue(call parser.CallExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	checked := checkCall(call, names, typeEnvironment)
	if len(initializerDiagnostics(checked)) > 0 {
		return checked
	}
	if checked.typ.Name == "" {
		diagnostic := typeErrorAt(checked.token, checked.token.Lexeme+" produces no value")
		return checkedExpression{token: checked.token, diagnostic: &diagnostic}
	}
	return checked
}
