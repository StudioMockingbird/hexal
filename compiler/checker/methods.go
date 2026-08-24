package checker

import (
	"fmt"
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

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
	Exported     bool // external linkage + prototype in this module's header
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
	// spelling. The hex_f_ encoding is not injective, so a collision between
	// a free function and a method is reported against both declarations.
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

// isNullableFun reports whether typ is a nullable function pointer
// (Fun<...> | Nil) that can be used as a dispatch-table member. The
// nullable form reuses the base's Signature, so IsNullable must be checked
// before the direct Signature test.
func isNullableFun(typ compilerTypes.Type) bool {
	if compilerTypes.IsNullable(typ) {
		if base, ok := compilerTypes.NullableBase(typ); ok && base.Signature != nil {
			return true
		}
	}
	if typ.Signature != nil {
		return false
	}
	if typ.Union != nil {
		hasFun, hasNil := false, false
		for _, member := range typ.Union.Members {
			if member.Signature != nil {
				hasFun = true
			}
			if compilerTypes.IsNil(member) {
				hasNil = true
			}
		}
		if hasFun && hasNil && len(typ.Union.Members) == 2 {
			return true
		}
	}
	return false
}

// checkFunMemberCall handles a call through an explicit dispatch-table
// member such as `table.operation(args)` where operation is a Fun<...> or
// `Fun<...>|Nil` field. It validates arity and argument types against the
// stored Fun signature and lowers to an indirect call through the member.
func checkFunMemberCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, member *compilerTypes.ObjectMember, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	funType := member.Type
	if isNullableFun(funType) {
		// A nullable dispatch member must be narrowed before the call. The
		// checker only narrows bare bindings, so a direct member call cannot
		// prove non-nil. Require the caller to bind the member to a local
		// and narrow that local (e.g. `cb := table.cb; if cb != nil { cb() }`).
		diagnostic := typeErrorAt(callee.Property, member.Type.Name+" may be Nil; narrow it before calling it")
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	if funType.Signature == nil {
		diagnostic := typeErrorAt(callee.Property, member.Name+" is not callable")
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	signature := funType.Signature
	if len(call.Arguments) != len(signature.Parameters) {
		diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("%s expects %d arguments; got %d", member.Name, len(signature.Parameters), len(call.Arguments)))
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	parameterUses := make([]compilerTypes.TypeUse, len(signature.Parameters))
	for index, parameter := range signature.Parameters {
		parameterUses[index] = compilerTypes.NewTypeUse(parameter)
	}
	arguments, diagnostics := checkArguments(member.Name, parameterUses, call.Arguments, callee.Property, names, typeEnvironment)
	if len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	// Build the member-access node for the Fun value. When the receiver is
	// a pointer to the object, insert an explicit dereference so the member
	// access lowers to `(*ptr).member` rather than `ptr.member`.
	var memberAccessNode Expression
	if receiver.typ.Element != nil && receiver.typ.Element.Object != nil {
		dereferenced := dereferencePlace(receiver, callee.Property, names.flow)
		if dereferenced.diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: dereferenced.diagnostic}
		}
		memberAccessNode = Expression{
			Kind:        MemberExpression,
			Operand:     &dereferenced.source.Node,
			Member:      member,
			OperandType: dereferenced.typ,
			ResultType:  funType,
		}
	} else {
		memberAccessNode = Expression{
			Kind:        MemberExpression,
			Operand:     &receiver.source.Node,
			Member:      member,
			OperandType: receiver.typ,
			ResultType:  funType,
		}
	}
	var resultType compilerTypes.Type
	if signature.Result != nil {
		resultType = *signature.Result
	}
	node := Expression{
		Kind:        CallExpression,
		Operand:     &memberAccessNode,
		Arguments:   arguments,
		OperandType: funType,
		ResultType:  resultType,
	}
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: member.Name, Node: node},
		typ:    resultType,
		token:  callee.Property,
	}
}

func nonCallableMemberDiagnostic(token lexer.Token, member *compilerTypes.ObjectMember) compilerTypes.Diagnostic {
	return typeErrorAt(token, fmt.Sprintf("member %s is not callable; its type is %s", member.Name, member.Type.Name))
}

// collectMethodSignature resolves a method's receiver and signature and
// registers it in names.methods before any module-level body is checked:
// this is what lets a forward or mutually recursive method or function call
// resolve, since every method this pass registers is visible to every body
// checkMethodBody later checks, in either source-order direction. The
// returned MethodDeclaration carries every collected field except
// SelfBinding, Body, and Defers, which checkMethodBody fills in. A generic
// method is registered as an open template by registerGenericMethod before
// any receiver resolution and returns a zero declaration; its body is
// checked only lazily at specialization time.
func collectMethodSignature(declaration parser.ImplDeclaration, names *scope, typeEnvironment *compilerTypes.Environment) (MethodDeclaration, compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	checked := MethodDeclaration{
		Name:         name,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
		Exported:     declaration.Exported,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)

	// A generic method is registered as an open template before any receiver
	// resolution: the receiver names the generic owner's parameters.
	if len(declaration.TypeParameters) > 0 || isGenericReceiver(declaration.SelfType) {
		return MethodDeclaration{}, registerGenericMethod(declaration, names, typeEnvironment)
	}

	targetUse, targetDiagnostic := resolveTypeUse(declaration.SelfType, declaration.Keyword, typeEnvironment, names.generics)
	if targetDiagnostic != nil {
		return MethodDeclaration{}, compilerTypes.Diagnostics{*targetDiagnostic}
	}
	target := targetUse.Type
	// Method rule 1 admits T, Ptr<T>, and MutPtr<T>. A nullable union is not
	// a receiver form: `self` would hold the union and every use would need a
	// narrowing the method body can never prove, so the target is rejected.
	if compilerTypes.IsNullable(target) {
		return MethodDeclaration{}, compilerTypes.Diagnostics{typeErrorAt(declaration.Keyword,
			fmt.Sprintf("impl requires T, Ptr<T>, or MutPtr<T>; got %s", target.Name))}
	}
	// Method rule 1: the target is T, Ptr<T>, or MutPtr<T> for a declared
	// nominal object T. Alias resolution already happened in resolveTypeUse.
	object := target.Object
	if object == nil && target.Element != nil {
		object = target.Element.Object
	}
	if object == nil {
		return MethodDeclaration{}, compilerTypes.Diagnostics{typeErrorAt(declaration.Keyword,
			target.Name+" is not a nominal object type; impl requires an object")}
	}
	// Only the type's defining module may declare its methods. An imported
	// receiver -- or a transparent alias of one -- resolves to the defining
	// module's identity, so the ModuleID comparison rejects every spelling of
	// it, qualified or not. Builtins carry an empty id and keep their
	// compiler-owned behavior.
	if object.ModuleID != "" && object.ModuleID != names.moduleID {
		return MethodDeclaration{}, compilerTypes.Diagnostics{typeErrorAt(receiverSpellingToken(declaration.SelfType, declaration.Keyword),
			"cannot declare methods for imported type "+receiverSpelling(declaration.SelfType, object.Name))}
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
		return MethodDeclaration{}, diagnostics
	}

	names.methods.define(&checked)
	return checked, nil
}

// checkMethodBody checks a method's body against its already-collected
// signature (collectMethodSignature). It runs in the module's second pass,
// after every module-level function and method signature is registered, so
// a call to any other module function or method - earlier, later, or
// mutually recursive - resolves.
func checkMethodBody(declaration parser.ImplDeclaration, checked MethodDeclaration, names *scope, typeEnvironment *compilerTypes.Environment, analyzeReturns bool) (MethodDeclaration, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	parameters := checked.Parameters

	selfID := names.newBindingID()
	checked.SelfBinding = selfID
	body := &scope{
		module:     names.module,
		local:      make(map[string]binding, len(parameters)),
		owner:      checked.Name,
		result:     checked.Result,
		resultUse:  checked.ResultUse,
		methods:    names.methods,
		self:       &checked.SelfType,
		selfID:     selfID,
		function:   true,
		nextID:     names.nextID,
		flow:       newFlowState(),
		generics:   names.generics,
		registry:   names.registry,
		moduleID:   names.moduleID,
		logicalKey: names.logicalKey,
	}
	for index := range parameters {
		// No nested scope may shadow an import alias; a conflicting
		// parameter is rejected like any other redeclaration.
		if names.importAlias(parameters[index].Name) {
			token := lexer.Token{Line: parameters[index].SourceLine, Column: parameters[index].SourceColumn, Lexeme: parameters[index].Name}
			diagnostics = append(diagnostics, nameErrorAt(token, "import alias "+parameters[index].Name+" conflicts with an existing name"))
			continue
		}
		parameters[index].Binding = names.newBindingID()
		bound := binding{typ: parameters[index].Type, use: parameters[index].TypeUse, parameter: true, id: parameters[index].Binding}
		body.local[parameters[index].Name] = bound
	}
	checked.Parameters = parameters

	statements, bodyDiagnostics := checkBody(declaration.Body, body, typeEnvironment)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = statements
	checked.Defers = append(checked.Defers, body.defers...)

	if analyzeReturns && checked.Result != nil && len(bodyDiagnostics) == 0 && FallsThrough(checked.Body) {
		diagnostics = append(diagnostics, typeErrorAt(declaration.End,
			fmt.Sprintf("returning %s may fall through without returning %s", checked.Name, checked.Result.Name)))
	}
	return checked, diagnostics
}

// receiverSpelling renders the written impl receiver for ownership
// diagnostics: the qualified name as written (Geometry.Point), the alias
// name, or the plain type name. Pointer forms resolve to their element's
// spelling, so Ptr<Geometry.Point> and Geometry.Point read the same.
func receiverSpelling(expression parser.TypeExpression, fallback string) string {
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		return expression.Name.Lexeme
	case parser.QualifiedTypeExpression:
		names := make([]string, 0, len(expression.Names))
		for _, name := range expression.Names {
			names = append(names, name.Lexeme)
		}
		return expression.Module.Lexeme + "." + strings.Join(names, ".")
	case parser.PtrTypeExpression:
		return receiverSpelling(expression.Element, fallback)
	default:
		return fallback
	}
}

// receiverSpellingToken locates the ownership diagnostic at the written
// receiver where one exists, falling back to the impl keyword.
func receiverSpellingToken(expression parser.TypeExpression, fallback lexer.Token) lexer.Token {
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		return expression.Name
	case parser.QualifiedTypeExpression:
		return expression.Module
	case parser.PtrTypeExpression:
		return receiverSpellingToken(expression.Element, fallback)
	default:
		return fallback
	}
}

// checkMethodCall resolves receiver.method(...). The receiver is checked as a
// place -- adaptation may need its address -- and the method is found by the
// receiver's nominal object identity, so all three receiver forms reach the
// one method declared on that object.
func checkMethodCall(call parser.CallExpression, callee parser.PropertyExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	// Alias.name(...) where Alias is an import alias calls the target
	// module's exported function. This precedes every builtin receiver check
	// so an alias always wins over a same-named builtin receiver, and a
	// dangling alias falls through to the ordinary path.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable {
		if target, ok := names.importAliasTarget(variable.Name.Lexeme); ok {
			return checkQualifiedFunctionCall(call, callee.Property, target, names, typeEnvironment)
		}
	}
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
	// View<T>.from_pointer() and View<T>.empty() name the built-in generic
	// type, not a View value binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "View" {
		return checkViewBridgeCall(call, variable.Name, names, typeEnvironment)
	}
	// Task.yield() names the built-in Task type, not a Task value binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Task" {
		return checkTaskTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Channel<T>.new() names the built-in generic Channel type, not a
	// Channel value binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Channel" {
		return checkChannelTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Mutex.new() names the built-in Mutex type, not a Mutex value binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Mutex" {
		return checkMutexTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// IO.stdin()/stdout()/stderr() name the built-in IO type.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "IO" {
		return checkIOTypeCall(call, variable, names, typeEnvironment)
	}
	// Bytes.over(buffer) names the built-in Bytes type.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Bytes" {
		return checkBytesTypeCall(call, variable, names, typeEnvironment)
	}
	// Atomic<T>.new() names the built-in generic Atomic type, not an Atomic
	// value binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable && variable.Name.Lexeme == "Atomic" {
		return checkAtomicTypeCall(call, variable.Name, names, typeEnvironment)
	}
	// Int32.from_le_bytes(...) names a fixed-width integer type, not an
	// integer value binding.
	if variable, isVariable := callee.Receiver.(parser.VariableExpression); isVariable &&
		(callee.Property.Lexeme == "from_le_bytes" || callee.Property.Lexeme == "from_be_bytes") {
		return checkEndianFromBytesCall(call, variable.Name, typeEnvironment, names)
	}
	// Error.new(...) names the built-in Error type, not an Error value
	// binding.
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
	// The compiler-owned `to<Dest>()` conversion resolves on eligible scalar
	// receivers before user method lookup. A receiver depending on a generic
	// parameter defers to specialization.
	if name == "to" &&
		(compilerTypes.IsInteger(receiver.typ) || compilerTypes.IsFloat(receiver.typ) ||
			compilerTypes.IsRune(receiver.typ) || compilerTypes.ContainsTypeParameter(receiver.typ)) {
		return checkConversionCall(call, callee, receiver, names, typeEnvironment)
	}
	// The compiler-owned `bit_cast<T>()` reinterprets same-width
	// fixed-representation scalar bits.
	if name == "bit_cast" && (bitCastEligibleType(receiver.typ) || compilerTypes.ContainsTypeParameter(receiver.typ)) {
		return checkBitCastCall(call, callee, receiver, names, typeEnvironment)
	}
	// Explicit-endian byte conversion instance methods on fixed-width
	// integer receivers.
	if (name == "to_le_bytes" || name == "to_be_bytes") && (compilerTypes.IsInteger(receiver.typ) || compilerTypes.ContainsTypeParameter(receiver.typ)) {
		return checkEndianToBytesCall(call, callee, receiver, names, typeEnvironment)
	}
	// Volatile integer accesses dispatch on pointer receivers. A nullable
	// receiver is excluded so the nullable-narrowing diagnostic below owns
	// it.
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
	// List methods dispatch on the built-in list receiver type.
	if receiver.typ.List != nil {
		return checkListMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// Dict methods dispatch on the built-in dictionary receiver type.
	if receiver.typ.Dict != nil {
		return checkDictMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// Task, Channel, Mutex, and Atomic methods dispatch on their built-in
	// handle receiver types.
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
	// Stream operations dispatch on the built-in stream receivers: IO
	// methods take the value; Bytes state-changing methods reach MutPtr<Bytes>
	// either through a pointer value or through the mutable-binding rule.
	if compilerTypes.IsIO(receiver.typ) {
		return checkIOStreamMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	if receiver.typ.Element != nil && compilerTypes.IsBytes(*receiver.typ.Element) {
		return checkBytesStreamMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	if compilerTypes.IsBytes(receiver.typ) {
		return checkBytesStreamMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// String methods dispatch on the built-in String receiver type.
	if compilerTypes.IsString(receiver.typ) {
		return checkStringMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// Strand methods dispatch on the built-in Strand receiver type; the
	// surface is deliberately smaller than String's.
	if compilerTypes.IsStrand(receiver.typ) {
		return checkStrandMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// RuneCursor methods dispatch on the cursor descriptor type.
	if compilerTypes.IsRuneCursor(receiver.typ) {
		return checkRuneCursorMethodCall(call, callee, receiver, names, typeEnvironment)
	}
	// A nullable receiver reaches no method until a null test narrowed it to
	// its pointer member.
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
	// A receiver whose type another module defines routes its method lookup
	// to that module's recorded exported methods. Builtin receivers carry an
	// empty id and keep the local path.
	if object.ModuleID != "" && object.ModuleID != names.moduleID {
		if member, ok := object.Member(name); ok {
			if member.Type.Signature != nil || isNullableFun(member.Type) {
				return checkFunMemberCall(call, callee, receiver, member, names, typeEnvironment)
			}
			return checkedExpression{token: callee.Property, diagnostic: func() *compilerTypes.Diagnostic {
				diagnostic := nonCallableMemberDiagnostic(callee.Property, member)
				return &diagnostic
			}()}
		}
		return checkImportedMethodCall(call, callee, name, object, receiver, names, typeEnvironment)
	}
	method := names.methods.lookup(object, name)
	if method == nil {
		if genericMethod := lookupGenericMethod(names, object, name); genericMethod != nil {
			return checkGenericMethodCall(call, callee, genericMethod, object, receiver, names, typeEnvironment)
		}
		if member, ok := object.Member(name); ok {
			if member.Type.Signature != nil || isNullableFun(member.Type) {
				return checkFunMemberCall(call, callee, receiver, member, names, typeEnvironment)
			}
			diagnostic := nonCallableMemberDiagnostic(callee.Property, member)
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		diagnostic := typeErrorAt(callee.Property, object.Name+" has no method named "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}

	adapted, diagnostic := adaptReceiver(receiver, *method, callee, typeEnvironment, names.flow)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}

	if len(call.Arguments) != len(method.Parameters) {
		diagnostic := typeErrorAt(callee.Property,
			fmt.Sprintf("%s expects %d arguments; got %d", name, len(method.Parameters), len(call.Arguments)))
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

// checkImportedMethodCall resolves a method call whose receiver type another
// module defines. The lookup routes to the defining module's recorded
// methods, and only exported methods on exported receiver types are visible
// to importers; anything else is the visibility failure at the method. The
// checked call mirrors a local method call: the same receiver adaptation,
// argument checking, and node shape, with the defining module's resolved
// signature.
func checkImportedMethodCall(call parser.CallExpression, callee parser.PropertyExpression, name string, object *compilerTypes.ObjectType, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	method, ok := names.registry.exportedMethod(object.ModuleID, object.Name, name)
	if !ok {
		diagnostic := privateToModuleDiagnostic(callee.Property, name, object.ModuleID)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	adapted, diagnostic := adaptReceiver(receiver, method, callee, typeEnvironment, names.flow)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	if len(call.Arguments) != len(method.Parameters) {
		diagnostic := typeErrorAt(callee.Property,
			fmt.Sprintf("%s expects %d arguments; got %d", name, len(method.Parameters), len(call.Arguments)))
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
		Kind:             MethodCallExpression,
		Name:             name,
		Owner:            object,
		Operand:          &adapted.Node,
		Arguments:        arguments,
		OperandType:      method.SelfType,
		ResultType:       resultType,
		MethodParameters: methodParameterTypes(&method),
	}
	return checkedExpression{
		source: Operand{Kind: ExpressionOperand, Type: resultType, Name: name, Node: node},
		typ:    resultType,
		token:  callee.Property,
	}
}

// methodParameterTypes extracts the declared parameter types of a method
// record for the foreign-prototype emission in the generator.
func methodParameterTypes(method *MethodDeclaration) []compilerTypes.Type {
	if method == nil {
		return nil
	}
	types := make([]compilerTypes.Type, 0, len(method.Parameters))
	for _, parameter := range method.Parameters {
		types = append(types, parameter.Type)
	}
	return types
}

// adaptReceiver applies the ordered receiver rules and returns the
// receiver already converted to the method's target form:
//
//  1. an exact target type is passed directly;
//  2. MutPtr<T> weakens to a Ptr<T> target;
//  3. Ptr<T> or MutPtr<T> dereferences to a copied T target; or
//  4. an addressable T uses ref for a Ptr<T> or MutPtr<T> target.
//
// Rule 4 is `ref` exactly as written source would be: a fixed place yields
// Ptr<T> and so cannot reach a MutPtr<T> method.
func adaptReceiver(receiver checkedExpression, method MethodDeclaration, callee parser.PropertyExpression, typeEnvironment *compilerTypes.Environment, flow *flowState) (Operand, *compilerTypes.Diagnostic) {
	target := method.SelfType
	switch {
	case compilerTypes.Equal(target, receiver.typ), assignable(target, receiver.typ):
		return valueFromPlace(receiver).source, nil
	case receiver.typ.Element != nil && compilerTypes.Equal(*receiver.typ.Element, target):
		dereferenced := dereferencePlace(receiver, callee.Property, flow)
		if dereferenced.diagnostic != nil {
			return Operand{}, dereferenced.diagnostic
		}
		return valueFromPlace(dereferenced).source, nil
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
		// The checked node carries its interned result type: generation compares
		// identities against it and never reconstructs a fresh pointer type.
		addressNode := unaryNode(AddressOfExpression, receiver.source.Node)
		addressNode.ResultType = pointer
		return Operand{
			Kind: VariableOperand,
			Type: pointer,
			Node: addressNode,
		}, nil
	}
	diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("%s needs %s; %s is %s",
		method.Name, target.Name, placeDescription(callee.Receiver), receiver.typ.Name))
	return Operand{}, &diagnostic
}
