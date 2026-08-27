// validation.go owns the generator-side validation layer: checked-program,
// statement, expression, operand, constant, and generated-type validation,
// kept separate from rendering.
package generator

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
	"strconv"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func validateCheckedProgram(program checker.Program, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, stringState *literalRegistry) error {
	typeState := &generatedTypeValidation{declaredObjects: errorDeclaredObjects(program)}
	state := &expressionValidation{
		variables:      make(map[string]generatedBinding),
		bindings:       make(map[checker.BindingID]generatedBinding),
		bindingNames:   make(map[checker.BindingID]string),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
	}
	state.pushScope()
	for _, typeDeclaration := range program.TypeDeclarations {
		if !validSourceName(typeDeclaration.Name) {
			return unknownExpressionDiagnostic("invalid checked type declaration name")
		}
		if !validateGeneratedType(typeDeclaration.Type, typeState, false) {
			return unknownExpressionDiagnostic("unsupported checked type declaration")
		}
	}
	if err := validateStatements(program.Statements, state, typeState); err != nil {
		return err
	}
	for _, function := range program.SpecializedFunctions {
		if err := validateFunctionDeclaration(function, typeState, functions, methods, stringState); err != nil {
			return err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := validateMethodDeclaration(method, typeState, functions, methods, stringState); err != nil {
			return err
		}
	}
	return nil
}

// validateFunctionDeclaration validates one concrete function declaration and
// its body without mutating the main statement state. stringState is the
// shared literal registry: the preflight renders call statements to prove
// them renderable, and a string-literal argument must resolve against the
// same registry the emission pass uses.
func validateFunctionDeclaration(declared checker.FunctionDeclaration, typeState *generatedTypeValidation, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, stringState *literalRegistry) error {
	if !validSourceName(declared.Name) || declared.Type.Signature == nil || !validateGeneratedType(declared.Type, typeState, false) {
		return unknownExpressionDiagnostic("unsupported checked specialized function")
	}
	state := &expressionValidation{
		variables:      make(map[string]generatedBinding, len(declared.Parameters)),
		bindings:       make(map[checker.BindingID]generatedBinding, len(declared.Parameters)),
		bindingNames:   make(map[checker.BindingID]string, len(declared.Parameters)),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
	}
	state.pushScope()
	for _, parameter := range declared.Parameters {
		if _, err := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false); err != nil {
			return err
		}
	}
	return validateStatements(declared.Body, state, typeState)
}

// validateMethodDeclaration validates one concrete method declaration and its
// body. stringState is the shared literal registry, threaded through for the
// same reason as validateFunctionDeclaration.
func validateMethodDeclaration(declared checker.MethodDeclaration, typeState *generatedTypeValidation, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, stringState *literalRegistry) error {
	if declared.Object == nil || !validSourceName(declared.Name) || !validateGeneratedType(declared.SelfType, typeState, false) {
		return unknownExpressionDiagnostic("unsupported checked specialized method")
	}
	state := &expressionValidation{
		variables:      make(map[string]generatedBinding, len(declared.Parameters)),
		bindings:       make(map[checker.BindingID]generatedBinding, len(declared.Parameters)),
		bindingNames:   make(map[checker.BindingID]string, len(declared.Parameters)),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
	}
	state.pushScope()
	if _, err := state.allocateBinding(declared.SelfBinding, "self", declared.SelfType, false); err != nil {
		return err
	}
	for _, parameter := range declared.Parameters {
		if _, err := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false); err != nil {
			return err
		}
	}
	return validateStatements(declared.Body, state, typeState)
}

func validateStatements(statements []checker.Statement, state *expressionValidation, typeState *generatedTypeValidation) error {
	if len(state.activeScopes) == 0 {
		state.pushScope()
		defer state.popScope()
	}
	for _, statement := range statements {
		switch statement := statement.(type) {
		case checker.Declaration:
			if !validSourceName(statement.Name) || !validateGeneratedType(statement.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported checked declaration")
			}
			if statement.Binding == 0 {
				if _, exists := state.variables[statement.Name]; exists {
					return unknownExpressionDiagnostic("duplicate checked declaration name")
				}
			}
			if err := validateCheckedOperandWithState(statement.Source, state); err != nil {
				return err
			}
			if !generatedAssignable(statement.Type, statement.Source.Type) {
				return unknownExpressionDiagnostic("declaration source type does not match its checked type")
			}
			if _, err := state.allocateBinding(statement.Binding, statement.Name, statement.Type, statement.Mutable); err != nil {
				return err
			}
		case checker.Assignment:
			if !validSourceName(statement.Name) || !validateGeneratedType(statement.Type, typeState, false) || !validateGeneratedType(statement.Target.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported checked assignment")
			}
			if err := validateCheckedOperandWithState(statement.Target, state); err != nil {
				return err
			}
			targetPlace, err := checkedPlaceMetadata(statement.Target.Node, state)
			if err != nil {
				return err
			}
			if !targetPlace.addressable || !targetPlace.writable {
				return unknownExpressionDiagnostic("assignment target is not an addressable writable place")
			}
			if err := validateCheckedOperandWithState(statement.Source, state); err != nil {
				return err
			}
			// The target names the declared storage slot, so its checked type
			// is that slot's type exactly, except that null-test branch
			// narrowing may present it as the non-Nil member or as Nil.
			targetMatches := compilerTypes.Equal(statement.Type, statement.Target.Type)
			if !targetMatches {
				if base, nullable := compilerTypes.NullableBase(statement.Type); !nullable ||
					!compilerTypes.Equal(base, statement.Target.Type) && !compilerTypes.IsNil(statement.Target.Type) {
					return unknownExpressionDiagnostic("assignment target type does not match its checked type")
				}
			}
			if !generatedAssignable(statement.Type, statement.Source.Type) {
				return unknownExpressionDiagnostic("assignment operand type does not match its checked type")
			}
		case checker.CallStatement:
			if statement.Call.Node.Kind == checker.PrintExpression {
				// print validates its arguments and produces no value; the
				// statement renderer emits its own statements. Continue so
				// the statements after print still pass preflight.
				continue
			}
			if _, err := renderCallStatement(statement, state); err != nil {
				return err
			}
		case checker.TryStatement:
			// The operand carries the try propagation metadata and validates
			// its own subtree.
			if err := validateCheckedOperandWithState(statement.Expression, state); err != nil {
				return err
			}
		case checker.DeferStatement:
			if statement.Action.IsCall {
				if statement.Action.Call == nil {
					return unknownExpressionDiagnostic("deferred call action without a checked call")
				}
				if statement.Action.Call.Type == (compilerTypes.Type{}) {
					// A no-result call such as Heap.free validates its node
					// directly; it has no value type to check.
					if err := validateExpressionNode(statement.Action.Call.Node, nil, state); err != nil {
						return err
					}
					break
				}
				if err := validateCheckedOperandWithState(*statement.Action.Call, state); err != nil {
					return err
				}
			} else if statement.Action.Value != nil {
				if err := validateCheckedOperandWithState(*statement.Action.Value, state); err != nil {
					return err
				}
			}
		case checker.ReturnStatement:
			// Function return signatures are checked while rendering their
			// definitions; the preflight pass only validates the value shape.
			if statement.Value != nil {
				if err := validateCheckedOperandWithState(*statement.Value, state); err != nil {
					return err
				}
			}
		case checker.IfStatement:
			if err := validateCondition(statement.Condition, state); err != nil {
				return err
			}
			state.pushScope()
			if err := validateStatements(statement.Then, state, typeState); err != nil {
				return err
			}
			state.popScope()
			for _, branch := range statement.ElseIf {
				if err := validateCondition(branch.Condition, state); err != nil {
					return err
				}
				state.pushScope()
				if err := validateStatements(branch.Body, state, typeState); err != nil {
					return err
				}
				state.popScope()
			}
			if statement.Else != nil {
				state.pushScope()
				if err := validateStatements(statement.Else, state, typeState); err != nil {
					return err
				}
				state.popScope()
			}
		case checker.WhileStatement:
			if err := validateCondition(statement.Condition, state); err != nil {
				return err
			}
			state.pushScope()
			previousLoopDepth := state.loopDepth
			state.loopDepth++
			err := validateStatements(statement.Body, state, typeState)
			state.loopDepth = previousLoopDepth
			state.popScope()
			if err != nil {
				return err
			}
		case checker.ForStatement:
			if err := validateCheckedOperandWithState(statement.Source, state); err != nil {
				return err
			}
			state.pushScope()
			for _, binder := range statement.Binders {
				if !validSourceName(binder.Name) || !validateGeneratedType(binder.Type, typeState, false) {
					return unknownExpressionDiagnostic("unsupported checked for binder")
				}
				if _, err := state.allocateBinding(binder.Binding, binder.Name, binder.Type, false); err != nil {
					return err
				}
			}
			previousLoopDepth := state.loopDepth
			state.loopDepth++
			err := validateStatements(statement.Body, state, typeState)
			state.loopDepth = previousLoopDepth
			state.popScope()
			if err != nil {
				return err
			}
		case checker.ErrdeferStatement:
			if statement.Action.IsCall {
				if statement.Action.Call == nil {
					return unknownExpressionDiagnostic("errdeferred call action without a checked call")
				}
				if statement.Action.Call.Type == (compilerTypes.Type{}) {
					return validateExpressionNode(statement.Action.Call.Node, nil, state)
				}
				if err := validateCheckedOperandWithState(*statement.Action.Call, state); err != nil {
					return err
				}
			} else if statement.Action.Value != nil {
				if err := validateCheckedOperandWithState(*statement.Action.Value, state); err != nil {
					return err
				}
			}
		case checker.BreakStatement:
			if state.loopDepth == 0 {
				return unknownExpressionDiagnostic("checked break outside a while loop")
			}
		case checker.ContinueStatement:
			if state.loopDepth == 0 {
				return unknownExpressionDiagnostic("checked continue outside a while loop")
			}
		case checker.FunctionDeclaration:
			if len(state.activeScopes) > 1 {
				return unknownExpressionDiagnostic("function declaration inside a module-level control-flow block")
			}
			continue
		case checker.MethodDeclaration:
			if len(state.activeScopes) > 1 {
				return unknownExpressionDiagnostic("method declaration inside a module-level control-flow block")
			}
			continue
		default:
			return unknownExpressionDiagnostic("unsupported checked statement")
		}
	}
	return nil
}

func validateCondition(condition checker.Operand, state *expressionValidation) error {
	// Nil is always falsey and needs no further validation (the nil
	// literal's other generator paths fail closed).
	switch compilerTypes.Truthiness(condition.Type) {
	case compilerTypes.TruthinessNil:
		return nil
	case compilerTypes.TruthinessInvalid:
		return unknownExpressionDiagnostic("cannot determine the truthiness of a checked control-flow condition")
	}
	return validateCheckedOperandWithState(condition, state)
}

func supportedGeneratedType(typ compilerTypes.Type) bool {
	return validateGeneratedType(typ, &generatedTypeValidation{}, false)
}

type generatedTypeValidation struct {
	activeObjects   map[*compilerTypes.ObjectType]bool
	validObjects    map[*compilerTypes.ObjectType]bool
	declaredObjects map[*compilerTypes.ObjectType]bool
	// arrays is the module's array state, carried here because it is the
	// one per-module channel already threaded into every expression render.
	// Accessor demand is recorded from the render site, which is
	// the only place that knows which accessor a surviving access names:
	// deriving it a second time from the checked tree would be two sources
	// of truth for one fact, and a disagreement would emit generated C
	// naming an undeclared function.
	arrays *generatedArrayState
}

// IsCanonical owns identity and recursive type metadata. This pass keeps only
// generator-specific source-name and declaration checks.
func validateGeneratedType(typ compilerTypes.Type, state *generatedTypeValidation, throughPointer bool) bool {
	if compilerTypes.IsNil(typ) {
		// Nil is not canonical outside union construction, but the checker
		// admits it only where the language allows it (union members, nil
		// operands, narrowed payloads). The generator validates the
		// checker's output, so the singleton passes as-is.
		return true
	}
	if !compilerTypes.IsCanonical(typ) {
		// Unknown is canonical only behind a pointer layer, exactly as the
		// type environment interning rule states: Ptr<Unknown> and
		// MutPtr<Unknown> are the erased object pointer types.
		if compilerTypes.IsUnknown(typ) {
			return throughPointer
		}
		return false
	}
	if typ.Signature != nil {
		// A Fun result, including one that is itself a Fun, lowers through
		// standaloneResultSpelling's C23 typeof wrapping; ordinary recursion
		// is enough here.
		if typ.Signature.Result != nil && !validateGeneratedType(*typ.Signature.Result, state, false) {
			return false
		}
		for _, parameter := range typ.Signature.Parameters {
			if !validateGeneratedType(parameter, state, false) {
				return false
			}
		}
		return true
	}
	if typ.Union != nil {
		if len(typ.Union.Members) < 2 || typ.CName == "" {
			return false
		}
		for _, member := range typ.Union.Members {
			if !validateGeneratedType(member, state, false) {
				return false
			}
		}
		return true
	}
	if typ.Element != nil {
		return validateGeneratedType(*typ.Element, state, true)
	}
	if typ.Array != nil {
		return validateGeneratedType(typ.Array.Element, state, false)
	}
	if typ.View != nil {
		return validateGeneratedType(typ.View.Element, state, false)
	}
	if typ.List != nil {
		return validateGeneratedType(typ.List.Element, state, false)
	}
	if typ.Dict != nil {
		return validateGeneratedType(typ.Dict.Key, state, false) && validateGeneratedType(typ.Dict.Value, state, false)
	}
	if compilerTypes.IsEoS(typ) {
		return true
	}
	if typ.Object == nil {
		return true
	}
	object := typ.Object
	// The built-in Error object is compiler-owned: its C name is the plain
	// hex_t_Error, never owner-encoded, even though its ModuleID is empty.
	if object == compilerTypes.ErrorType.Object {
		if state.declaredObjects != nil && !state.declaredObjects[object] {
			return false
		}
	} else {
		expectedCName := privateCName(typeName, compilerTypes.SanitizeIdentifier(object.Name), object.Owner)
		if state.declaredObjects != nil && !state.declaredObjects[object] || !validSourceName(compilerTypes.SanitizeIdentifier(object.Name)) || object.CName != expectedCName {
			return false
		}
	}
	if state.activeObjects == nil {
		state.activeObjects = make(map[*compilerTypes.ObjectType]bool)
		state.validObjects = make(map[*compilerTypes.ObjectType]bool)
	}
	if state.validObjects[object] {
		return true
	}
	if state.activeObjects[object] {
		return throughPointer
	}
	if len(object.Members) == 0 {
		return false
	}
	state.activeObjects[object] = true
	seenNames := make(map[string]bool, len(object.Members))
	for _, member := range object.Members {
		if !validSourceName(member.Name) || seenNames[member.Name] || !validateGeneratedType(member.Type, state, false) {
			delete(state.activeObjects, object)
			return false
		}
		seenNames[member.Name] = true
	}
	delete(state.activeObjects, object)
	state.validObjects[object] = true
	return true
}

func supportedGeneratedScalarType(typ compilerTypes.Type) bool {
	return typ.Element == nil && typ.Object == nil && typ.Signature == nil && compilerTypes.IsCanonical(typ)
}

type generatedPlace struct {
	typ         compilerTypes.Type
	addressable bool
	writable    bool
}

func declaredObjects(program checker.Program) map[*compilerTypes.ObjectType]bool {
	objects := make(map[*compilerTypes.ObjectType]bool)
	for _, declaration := range program.TypeDeclarations {
		if declaration.Type.Object != nil {
			objects[declaration.Type.Object] = true
		}
	}
	return objects
}

// errorDeclaredObjects augments the declared object table with the built-in
// Error object when the program references it.
func errorDeclaredObjects(program checker.Program) map[*compilerTypes.ObjectType]bool {
	objects := declaredObjects(program)
	// Imported object types are reachable through the module's statements
	// and must validate like local ones; the header emission carries their
	// definitions, so the validator admits them too.
	definitions, err := objectDefinitions(program)
	if err == nil {
		for _, object := range definitions {
			objects[object] = true
		}
	}
	if discoverErrorUsed(program) {
		objects[compilerTypes.ErrorType.Object] = true
	}
	return objects
}

func supportedGeneratedTypeWithState(typ compilerTypes.Type, state *expressionValidation) bool {
	if state != nil && state.generatedTypes != nil {
		return validateGeneratedType(typ, state.generatedTypes, false)
	}
	return supportedGeneratedType(typ)
}

func validateConstantOperand(source checker.Operand) error {
	// Object constants (Error.new results wrapped by union injection)
	// validate their object value.
	if source.Object != nil {
		return validateObjectValue(source.Object, &expressionValidation{})
	}
	// Nil is the singleton type: its one value is nullptr and it carries no
	// go/constant, so it is validated before the constant value is required.
	if compilerTypes.IsNil(source.Type) {
		return nil
	}
	// EoS is a singleton: its one value is a tag-only marker and
	// carries no go/constant, like Nil.
	if compilerTypes.IsEoS(source.Type) {
		return nil
	}
	// Heap is a singleton handle: Heap.new() carries no go/constant.
	if compilerTypes.IsHeap(source.Type) {
		return nil
	}
	if source.Constant == nil {
		return unknownExpressionDiagnostic("constant operand without a checked value")
	}
	switch source.Type.ScalarKind {
	case compilerTypes.ScalarBool:
		if !compilerTypes.Equal(source.Type, compilerTypes.Bool) || source.Constant.Kind() != constant.Bool {
			return unknownExpressionDiagnostic("invalid checked Bool constant")
		}
	case compilerTypes.ScalarUnsignedInteger, compilerTypes.ScalarSignedInteger:
		if !supportedGeneratedScalarType(source.Type) || source.Constant.Kind() != constant.Int {
			return unknownExpressionDiagnostic("invalid checked integer constant")
		}
		if _, err := integerLiteral(source); err != nil {
			return err
		}
	case compilerTypes.ScalarFloat:
		return validateFloatConstant(source)
	default:
		return unknownExpressionDiagnostic("unsupported checked constant type")
	}
	return nil
}

func validateFloatConstant(source checker.Operand) error {
	bitSize := 64
	if compilerTypes.Equal(source.Type, compilerTypes.Float32) {
		bitSize = 32
	} else if !compilerTypes.Equal(source.Type, compilerTypes.Float64) {
		return unknownExpressionDiagnostic("invalid checked float constant")
	}
	if bitSize == 32 && source.FloatBits > math.MaxUint32 {
		return unknownExpressionDiagnostic("Float32 constant has bits outside its declared width")
	}

	bits := source.FloatBits
	if bitSize == 32 {
		bits = uint64(uint32(bits))
	}
	signBit, special := floatSignAndSpecial(bits, bitSize)
	if source.Negative != signBit {
		return unknownExpressionDiagnostic("float sign metadata does not match its checked value")
	}
	if source.Constant == nil {
		return unknownExpressionDiagnostic("float constant without a checked value")
	}

	if special {
		if source.Constant.Kind() != constant.Unknown || source.Literal != "" {
			return unknownExpressionDiagnostic("special float constant has malformed metadata")
		}
		return nil
	}
	if source.Constant.Kind() != constant.Int && source.Constant.Kind() != constant.Float {
		return unknownExpressionDiagnostic("float constant is not numeric")
	}

	if source.Literal != "" {
		literal := strings.ReplaceAll(source.Literal, "_", "")
		if strings.HasPrefix(literal, "+") || strings.HasPrefix(literal, "-") {
			return unknownExpressionDiagnostic("float literal sign is stored in malformed metadata")
		}
		literalValue := constant.MakeFromLiteral(literal, gotoken.FLOAT, 0)
		if literalValue == nil || literalValue.Kind() == constant.Unknown || (literalValue.Kind() != constant.Int && literalValue.Kind() != constant.Float) || constant.Sign(source.Constant) < 0 || !constant.Compare(source.Constant, gotoken.EQL, literalValue) {
			return unknownExpressionDiagnostic("checked float literal does not match its value")
		}
		if floatBitsForConstant(literalValue, bitSize, source.Negative) != bits {
			return unknownExpressionDiagnostic("checked float literal does not match its rounded bits")
		}
		return nil
	}
	if floatBitsForConstant(source.Constant, bitSize, source.Negative) != bits {
		return unknownExpressionDiagnostic("checked float does not match its rounded bits")
	}
	valueSign := constant.Sign(source.Constant)
	if valueSign < 0 && !signBit || valueSign > 0 && signBit {
		return unknownExpressionDiagnostic("float sign metadata does not match its checked value")
	}
	return nil
}

func floatSignAndSpecial(bits uint64, bitSize int) (bool, bool) {
	if bitSize == 32 {
		value := uint32(bits)
		return value>>31 != 0, value&0x7f800000 == 0x7f800000
	}
	return bits>>63 != 0, bits&0x7ff0000000000000 == 0x7ff0000000000000
}

func floatBitsForConstant(value constant.Value, bitSize int, negative bool) uint64 {
	if bitSize == 32 {
		converted, _ := constant.Float32Val(value)
		bits := uint64(math.Float32bits(converted))
		if negative {
			bits |= uint64(1) << 31
		}
		return bits
	}
	converted, _ := constant.Float64Val(value)
	bits := math.Float64bits(converted)
	if negative {
		bits |= uint64(1) << 63
	}
	return bits
}

func validateObjectValue(value *checker.ObjectValue, state *expressionValidation) error {
	if value == nil || value.Type.Object == nil || !supportedGeneratedTypeWithState(value.Type, state) {
		return unknownExpressionDiagnostic("object operand without a checked object value")
	}
	if state.objects == nil {
		state.objects = make(map[*checker.ObjectValue]bool)
	}
	if state.objects[value] {
		return unknownExpressionDiagnostic("cyclic checked object value")
	}
	state.objects[value] = true
	defer delete(state.objects, value)

	seen := make(map[*compilerTypes.ObjectMember]bool, len(value.Initializers))
	for _, initializer := range value.Initializers {
		if initializer.Member == nil {
			return unknownExpressionDiagnostic("object initializer without a checked member")
		}
		canonical, ok := objectMember(value.Type.Object, initializer.Member)
		if !ok || seen[initializer.Member] || !compilerTypes.Equal(canonical.Type, initializer.Member.Type) {
			return unknownExpressionDiagnostic("object initializer has a forged checked member")
		}
		seen[initializer.Member] = true
		if !generatedAssignable(canonical.Type, initializer.Source.Type) {
			return unknownExpressionDiagnostic("object initializer type does not match its checked member")
		}
		if err := validateCheckedOperandWithState(initializer.Source, state); err != nil {
			return err
		}
	}
	if len(seen) != len(value.Type.Object.Members) {
		return unknownExpressionDiagnostic("incomplete checked object value")
	}
	return nil
}

func objectMember(object *compilerTypes.ObjectType, member *compilerTypes.ObjectMember) (*compilerTypes.ObjectMember, bool) {
	if object == nil || member == nil {
		return nil, false
	}
	for index := range object.Members {
		if &object.Members[index] == member {
			return &object.Members[index], true
		}
	}
	return nil, false
}

// generatedAssignable re-validates the checker's assignment relation so the
// generator never accepts a program the checker rejected. It is the complete
// type-level relation: weakening, nullable injection (P or Nil into P | Nil),
// and the one-row Unknown erasure/recovery table.
func generatedAssignable(target, source compilerTypes.Type) bool {
	return compilerTypes.Assignable(target, source)
}

func validateCheckedOperandWithState(source checker.Operand, state *expressionValidation) error {
	if !supportedGeneratedTypeWithState(source.Type, state) {
		return unknownExpressionDiagnostic("operand has an unsupported checked type")
	}
	switch source.Kind {
	case checker.ObjectOperand:
		if source.Object == nil || !compilerTypes.Equal(source.Type, source.Object.Type) {
			return unknownExpressionDiagnostic("object operand has mismatched checked types")
		}
		return validateObjectValue(source.Object, state)
	case checker.VariableOperand, checker.ExpressionOperand:
		if err := validateExpressionNode(source.Node, &source.Type, state); err != nil {
			return err
		}
		if expressionType, ok := expressionResultType(source.Node); ok && !compilerTypes.Equal(source.Type, expressionType) && !compilerTypes.WidensTo(expressionType, source.Type) {
			return unknownExpressionDiagnostic("operand expression type does not match its checked type")
		}
	case checker.ConstantOperand:
		return validateConstantOperand(source)
	default:
		return unknownExpressionDiagnostic("unsupported checked operand")
	}
	return nil
}

func validateExpressionNode(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if expected != nil && !supportedGeneratedTypeWithState(*expected, state) {
		return unknownExpressionDiagnostic("expression has an unsupported expected type")
	}
	switch node.Kind {
	case checker.NilExpression:
		if !compilerTypes.IsNil(node.ResultType) || expected != nil && !compilerTypes.IsNil(*expected) {
			return unknownExpressionDiagnostic("nil expression has invalid checked metadata")
		}
		return nil
	case checker.EosExpression:
		if !compilerTypes.IsEoS(node.ResultType) || expected != nil && !compilerTypes.IsEoS(*expected) {
			return unknownExpressionDiagnostic("eos expression has invalid checked metadata")
		}
		return nil
	case checker.VariableExpression:
		if !validSourceName(node.Name) {
			return unknownExpressionDiagnostic("variable without a source name")
		}
		if state != nil && (state.variables != nil || state.bindings != nil) {
			binding, ok := state.bindingFor(node)
			if !ok {
				return unknownExpressionDiagnostic("variable is not present in checked bindings")
			}
			if expected != nil && !compilerTypes.Equal(binding.typ, *expected) {
				// A null test narrows a local binding's reads to its non-Nil
				// base (or to Nil) inside the branch where the test holds;
				// the binding itself still holds the declared nullable type,
				// so a narrowed read is a stricter type.
				if !compilerTypes.Assignable(binding.typ, *expected) {
					return unknownExpressionDiagnostic("variable type does not match its checked type")
				}
			}
			for _, metadataType := range []compilerTypes.Type{node.OperandType, node.ResultType} {
				if metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(binding.typ, metadataType) {
					return unknownExpressionDiagnostic("variable metadata does not match its checked binding")
				}
			}
		}
		return validateExpressionMetadata(node, expected, state)
	case checker.FunctionReferenceExpression:
		return validateFunctionReference(node, expected, state)
	case checker.FunctionLiteralExpression:
		return validateFunctionLiteralExpression(node, expected, state)
	case checker.CallExpression:
		return validateCallExpression(node, expected, state)
	case checker.MethodCallExpression:
		return validateMethodCallExpression(node, expected, state)
	case checker.AddressOfExpression:
		return validateAddressExpression(node, expected, state)
	case checker.DereferenceExpression:
		return validateDereferenceExpression(node, expected, state)
	case checker.MemberExpression:
		return validateMemberExpression(node, expected, state)
	case checker.ObjectExpression:
		if node.Object == nil {
			return unknownExpressionDiagnostic("object expression without a checked object value")
		}
		if err := validateObjectValue(node.Object, state); err != nil {
			return err
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.Object.Type) {
			return unknownExpressionDiagnostic("object expression type does not match its expected type")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, node.Object.Type) {
			return unknownExpressionDiagnostic("object expression result type does not match its checked object")
		}
		return validateExpressionMetadata(node, expected, state)
	case checker.ConstantExpression:
		if node.Constant == nil || node.Constant.Kind != checker.ConstantOperand && node.Constant.Kind != checker.ObjectOperand ||
			!compilerTypes.Equal(node.ResultType, node.Constant.Type) ||
			!supportedGeneratedScalarType(node.ResultType) && node.Constant.Type.Object == nil && node.Constant.Type.Union == nil {
			detail := ""
			if node.Constant != nil {
				detail = fmt.Sprintf(" result=%s const=%s kind=%d literal=%q object=%v union=%v equal=%v", node.ResultType.Name, node.Constant.Type.Name, node.Constant.Kind, node.Constant.Literal, node.Constant.Type.Object != nil, node.Constant.Type.Union != nil, compilerTypes.Equal(node.ResultType, node.Constant.Type))
			}
			return unknownExpressionDiagnostic("constant expression without a checked constant" + detail)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("constant expression type does not match its expected type")
		}
		return validateConstantOperand(*node.Constant)
	case checker.UnaryOperationExpression:
		if node.Operand == nil {
			return unknownExpressionDiagnostic("unary operation with invalid checked metadata")
		}
		if node.Operator == checker.LogicalNotOperator {
			// not accepts any value-producing operand; the operand is
			// validated through its truthiness.
			if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
				return unknownExpressionDiagnostic("logical not requires a truthy-compatible operand and a Bool result")
			}
			if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic("unary operation result type does not match its expected type")
			}
			return validateTruthinessChild(node.Operand, state)
		}
		if !supportedGeneratedScalarType(node.OperandType) || !supportedGeneratedScalarType(node.ResultType) {
			return unknownExpressionDiagnostic("unary operation with invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("unary operation result type does not match its expected type")
		}
		if err := validateUnaryMetadata(node); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.BinaryOperationExpression:
		if node.Left == nil || node.Right == nil {
			return unknownExpressionDiagnostic("binary operation with invalid checked metadata")
		}
		if node.Operator == checker.LogicalAndOperator || node.Operator == checker.LogicalOrOperator {
			// and/or accept any value-producing operands, mixed types
			// included; each side is validated through its truthiness.
			if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
				return unknownExpressionDiagnostic("logical operation requires a truthy-compatible operand and a Bool result")
			}
			if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic("binary operation result type does not match its expected type")
			}
			if err := validateTruthinessChild(node.Left, state); err != nil {
				return err
			}
			return validateTruthinessChild(node.Right, state)
		}
		if !supportedGeneratedScalarType(node.OperandType) && node.OperandType.Element == nil || !supportedGeneratedScalarType(node.ResultType) {
			return unknownExpressionDiagnostic("binary operation with invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("binary operation result type does not match its expected type")
		}
		if err := validateBinaryMetadata(node); err != nil {
			return err
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		// A shift count keeps its own integer type; it never takes the left
		// operand's type, unlike every other binary operator here.
		rightExpected := node.OperandType
		if node.Operator == checker.ShiftLeftOperator || node.Operator == checker.ShiftRightOperator {
			if rightType, ok := expressionTypeWithState(*node.Right, state); ok {
				rightExpected = rightType
			}
		}
		return validateExpressionChildWithState(node.Right, rightExpected, state)
	case checker.NullTestExpression:
		// == nil and != nil test a nullable operand's active member. The
		// operand carries the pre-test nullable type; the result is Bool.
		if node.Operand == nil {
			return unknownExpressionDiagnostic("null test without a checked operand")
		}
		if node.OperandType == (compilerTypes.Type{}) || !compilerTypes.IsUnion(node.OperandType) || !compilerTypes.ContainsUnionMember(node.OperandType, compilerTypes.Nil) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("null test has an invalid nullable operand type")
		}
		if node.Operator != checker.EqualOperator && node.Operator != checker.NotEqualOperator {
			return unknownExpressionDiagnostic("null test has an invalid operator")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("null test result type is not Bool")
		}
		if expected != nil && !compilerTypes.Equal(*expected, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("null test result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.UnionInjectionExpression:
		return validateUnionInjection(node, expected, state)
	case checker.StreamConstructorExpression:
		return validateStreamConstructor(node, expected, state)
	case checker.BytesOverExpression:
		return validateBytesOverExpression(node, expected, state)
	case checker.StreamMethodCallExpression:
		return validateStreamMethodCall(node, expected, state)
	case checker.UnionWidenExpression:
		return validateUnionWiden(node, expected, state)
	case checker.UnionTestExpression:
		return validateUnionTest(node, expected, state)
	case checker.UnionPayloadExpression:
		return validateUnionPayload(node, expected, state)
	case checker.UnionEqualityExpression:
		return validateUnionEquality(node, expected, state)
	case checker.HeapAllocateExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) || node.Element.Signature != nil || !supportedGeneratedTypeWithState(node.ResultType, state) || node.ResultType.Element == nil || !compilerTypes.Equal(*node.ResultType.Element, node.Element) {
			return unknownExpressionDiagnostic("heap allocation has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("heap allocation result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.HeapFreeExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("heap free has invalid checked metadata")
		}
		if node.Arguments[0].Type.Element == nil {
			return unknownExpressionDiagnostic("heap free operand is not a pointer")
		}
		if expected != nil {
			return unknownExpressionDiagnostic("heap free produces no value")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.AdtConstructExpression:
		adt := node.ResultType.Adt
		if adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("ADT construction has invalid checked metadata")
		}
		variant := &adt.Variants[node.VariantIndex]
		if len(node.Arguments) != len(variant.Payload) {
			return unknownExpressionDiagnostic("ADT construction payload count does not match its variant")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("ADT construction result does not match its expected type")
		}
		for index, member := range variant.Payload {
			if err := validateCheckedOperandWithState(node.Arguments[index], state); err != nil {
				return err
			}
			if !generatedAssignable(member.Type, node.Arguments[index].Type) {
				return unknownExpressionDiagnostic("ADT construction payload does not match its variant field")
			}
		}
		return nil
	case checker.AdtPayloadExpression:
		adt := node.OperandType.Adt
		if node.Operand == nil || adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || node.MemberIndex < 0 || node.MemberIndex >= len(adt.Variants[node.VariantIndex].Payload) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("ADT payload read has invalid checked metadata")
		}
		member := &adt.Variants[node.VariantIndex].Payload[node.MemberIndex]
		if !compilerTypes.Equal(node.ResultType, member.Type) || expected != nil && !compilerTypes.Equal(*expected, member.Type) {
			return unknownExpressionDiagnostic("ADT payload read result does not match its checked field")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.MatchExpression:
		if node.Operand == nil || node.ResultType == (compilerTypes.Type{}) || len(node.Arguments) != len(node.MemberMap) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("match expression has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("match result does not match its expected type")
		}
		for _, arm := range node.Arguments {
			if !generatedAssignable(node.ResultType, arm.Type) {
				return unknownExpressionDiagnostic("match arm does not match its checked result type")
			}
			if err := validateCheckedOperandWithState(arm, state); err != nil {
				return err
			}
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ArrayLiteralExpression, checker.IndexExpression, checker.CollectionMethodCallExpression, checker.CollectionSliceExpression:
		return validateCollectionExpression(node, expected, state)
	case checker.StringLiteralExpression, checker.StringMethodCallExpression, checker.StringFromBytesExpression, checker.StringFromRunesExpression, checker.RuneCursorMethodCallExpression:
		return validateTextExpression(node, expected, state)
	case checker.ListNewExpression, checker.DictNewExpression:
		return validateCollectionConstructor(node, expected, state)
	case checker.WideningExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("widening expression has invalid checked metadata")
		}
		if !compilerTypes.IsInteger(node.ResultType) && !compilerTypes.IsFloat(node.ResultType) {
			return unknownExpressionDiagnostic("widening destination is not numeric")
		}
		if common, ok := compilerTypes.LosslessCommonType(node.OperandType, node.ResultType); !ok || !compilerTypes.Equal(common, node.ResultType) {
			return unknownExpressionDiagnostic("widening is not a proven lossless conversion")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("widening result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.DeepEqualityExpression:
		if node.Left == nil || node.Right == nil || node.OperandType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || node.Operator != checker.EqualOperator && node.Operator != checker.NotEqualOperator {
			return unknownExpressionDiagnostic("deep equality has invalid checked metadata")
		}
		leftType, leftOK := expressionTypeWithState(*node.Left, state)
		rightType, rightOK := expressionTypeWithState(*node.Right, state)
		if !leftOK || !rightOK || !compilerTypes.Equal(leftType, node.OperandType) || !compilerTypes.Equal(rightType, node.OperandType) {
			return unknownExpressionDiagnostic("deep equality operand does not match its compared type")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("deep equality result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Right, node.OperandType, state)
	case checker.ConversionExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.MemberIndex > 2 {
			return unknownExpressionDiagnostic("numeric conversion has invalid checked metadata")
		}
		if !compilerTypes.IsInteger(node.ResultType) && !compilerTypes.IsFloat(node.ResultType) || node.MemberIndex != 0 && (!compilerTypes.IsInteger(node.OperandType) || !compilerTypes.IsInteger(node.ResultType)) || node.MemberIndex == 0 && !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("numeric conversion has invalid checked types")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) && !compilerTypes.WidensTo(node.ResultType, *expected) {
			return unknownExpressionDiagnostic("numeric conversion result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.BitCastExpression:
		if node.Operand == nil || !checker.BitCastEligibleType(node.OperandType) || !checker.BitCastEligibleType(node.ResultType) || node.OperandType.Bits != node.ResultType.Bits {
			return unknownExpressionDiagnostic("bit cast has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("bit cast result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.EndianConversionExpression:
		if node.Operand == nil || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.MemberIndex > 1 {
			return unknownExpressionDiagnostic("endian conversion has invalid checked metadata")
		}
		if node.Name == "from" {
			if len(node.Arguments) != 1 || node.ResultType == (compilerTypes.Type{}) || node.OperandType.Array == nil {
				return unknownExpressionDiagnostic("endian from conversion has invalid checked metadata")
			}
			if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic("endian from result does not match its expected type")
			}
			return validateCheckedOperandWithState(node.Arguments[0], state)
		}
		if len(node.Arguments) != 0 || node.ResultType.Array == nil {
			return unknownExpressionDiagnostic("endian to conversion has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("endian to result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.TryExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.OperandType.Union == nil {
			return unknownExpressionDiagnostic("try expression has invalid checked metadata")
		}
		if unionMemberIndex(node.OperandType, compilerTypes.ErrorType) != node.MemberIndex {
			return unknownExpressionDiagnostic("try expression error member does not match its source union")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression, checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression, checker.MutexConstructorExpression, checker.MutexMethodCallExpression, checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression:
		return validateConcurrencyExpression(node, expected, state)
	case checker.StashConstructorExpression, checker.StashMethodCallExpression:
		return validateStashExpression(node, expected, state)
	case checker.PoolConstructorExpression, checker.PoolMethodCallExpression:
		return validatePoolExpression(node, expected, state)
	case checker.LayoutExpression:
		if node.OperandType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) || node.Name != "size_of" && node.Name != "align_of" {
			return unknownExpressionDiagnostic("layout query has invalid checked metadata")
		}
		if !layoutEligibleGenerated(node.OperandType) {
			return unknownExpressionDiagnostic("layout query has an ineligible type")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("layout query result type does not match its expected type")
		}
		return nil
	case checker.VolatileReadExpression:
		if node.Operand == nil || node.OperandType.Element == nil || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || !compilerTypes.Equal(node.ResultType, node.Element) {
			return unknownExpressionDiagnostic("volatile read has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("volatile read result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.VolatileWriteExpression:
		if node.Operand == nil || node.OperandType.Element == nil || len(node.Arguments) != 1 || !node.OperandType.PointeeWritable || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("volatile write has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("volatile write result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.ViewBridgeExpression:
		return validateViewBridgeExpression(node, expected, state)
	case checker.PrintExpression:
		if len(node.Arguments) == 0 || node.ResultType != (compilerTypes.Type{}) || (expected != nil) {
			return unknownExpressionDiagnostic("print call has invalid checked metadata")
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.StringCompareExpression:
		if node.Left == nil || node.Right == nil || !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("text ordering has invalid checked metadata")
		}
		switch node.Operator {
		case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		default:
			return unknownExpressionDiagnostic("text ordering has an invalid operator")
		}
		leftType, leftOK := expressionTypeWithState(*node.Left, state)
		rightType, rightOK := expressionTypeWithState(*node.Right, state)
		if !leftOK || !rightOK || !compilerTypes.Equal(leftType, node.OperandType) || !compilerTypes.Equal(rightType, node.OperandType) {
			return unknownExpressionDiagnostic("text ordering operand does not match its compared type")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("text ordering result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Right, node.OperandType, state)
	default:
		return unknownExpressionDiagnostic("unsupported checked expression")
	}
}

func validateExpressionMetadata(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	var metadataType compilerTypes.Type
	for _, typ := range []compilerTypes.Type{node.OperandType, node.ResultType} {
		if typ == (compilerTypes.Type{}) {
			continue
		}
		if !supportedGeneratedTypeWithState(typ, state) || expected != nil && !compilerTypes.Equal(*expected, typ) || metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(metadataType, typ) {
			return unknownExpressionDiagnostic("expression metadata does not match its expected type")
		}
		metadataType = typ
	}
	return nil
}

// validateFunctionReference accepts a declared function used as a Fun<...> value.
// A function is not a place, so no addressability metadata is consulted.
func validateFunctionReference(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.ResultType.Signature == nil || !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic("function reference without a checked Fun type")
	}
	if node.LocalHelperOrdinal != 0 {
		// A local named function's generated symbol is the shared
		// hex_fun_<ordinal> stream, never an entry in the module's
		// source-name-keyed declaration table.
	} else if !validSourceName(node.Name) {
		return unknownExpressionDiagnostic("function reference without a source name")
	} else if state.functions != nil && node.Module == "" {
		// A cross-module callee is not in the local declaration table; the
		// checker resolved it against the target module's exported records,
		// and the checked Fun type is authoritative.
		declared, ok := state.functions[node.Name]
		if !ok || !compilerTypes.Equal(declared, node.ResultType) {
			return unknownExpressionDiagnostic("function reference is not a declared checked function")
		}
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, node.ResultType) {
		return unknownExpressionDiagnostic("function reference metadata does not match its checked type")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("function reference type does not match its expected type")
	}
	return nil
}

// validateFunctionLiteralExpression accepts an anonymous function literal
// used as a Fun<...> value. Its own body is not re-validated here: like an
// ordinary named function's body, it is checked directly when
// writeLocalHelperDefinitions emits it, not through this preflight sweep,
// which exists for concrete generic specializations' substituted types.
// This function only proves the value-position metadata is self-consistent.
func validateFunctionLiteralExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Function == nil || node.LocalHelperOrdinal == 0 || node.LocalHelperOrdinal != node.Function.HelperOrdinal {
		return unknownExpressionDiagnostic("function literal has invalid checked metadata")
	}
	if node.ResultType.Signature == nil || !supportedGeneratedTypeWithState(node.ResultType, state) || !compilerTypes.Equal(node.ResultType, node.Function.Type) {
		return unknownExpressionDiagnostic("function literal without a checked Fun type")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("function literal type does not match its expected type")
	}
	return nil
}

// validateCallExpression checks a call against its callee's signature. The
// arguments carry no ordering metadata: C's unspecified argument evaluation
// order is inherited rather than fixed with temporaries.
func validateCallExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("call without a checked callee")
	}
	signature := node.OperandType.Signature
	if signature == nil || !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("call callee is not a checked Fun type")
	}
	if len(signature.Parameters) != len(node.Arguments) {
		return unknownExpressionDiagnostic("call argument count does not match its checked signature")
	}
	if signature.Result == nil {
		if node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("call result type does not match its checked signature")
		}
		if expected != nil {
			return unknownExpressionDiagnostic("a call producing no value has no expected type")
		}
	} else {
		if !compilerTypes.Equal(*signature.Result, node.ResultType) {
			return unknownExpressionDiagnostic("call result type does not match its checked signature")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("call result type does not match its expected type")
		}
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return err
	}
	for index, argument := range node.Arguments {
		if !generatedAssignable(signature.Parameters[index], argument.Type) {
			return unknownExpressionDiagnostic("call argument type does not match its checked parameter")
		}
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

func validateMethodCallExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Owner == nil || !validSourceName(compilerTypes.SanitizeIdentifier(node.Owner.Name)) || !validSourceName(node.Name) || node.Operand == nil {
		return unknownExpressionDiagnostic("method call has incomplete checked metadata")
	}
	// A method whose receiver type another module declares is not in the
	// local declaration table; the checker resolved it against the defining
	// module's exported records, so the checked node is authoritative for a
	// cross-module call (mirroring the cross-module function reference
	// path).
	crossModule := moduleOwner(node.Owner.ModuleID, state.owner) != state.owner
	declared, ok := state.methods[methodKey(node.Owner, node.Name)]
	if !crossModule {
		if !ok || declared.Object != node.Owner {
			return unknownExpressionDiagnostic("method call does not name a declared checked method")
		}
		if !compilerTypes.Equal(node.OperandType, declared.SelfType) || len(node.Arguments) != len(declared.Parameters) {
			return unknownExpressionDiagnostic("method call does not match its checked signature")
		}
		if declared.Result == nil {
			if node.ResultType != (compilerTypes.Type{}) || (expected != nil) {
				return unknownExpressionDiagnostic("method call result type does not match its checked signature")
			}
		} else {
			if node.ResultType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, *declared.Result) || expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
				return unknownExpressionDiagnostic("method call result type does not match its checked signature")
			}
		}
	} else if node.ResultType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic("method call result type is not a supported checked type")
	}
	receiverType, receiverErr := methodReceiverType(*node.Operand, node.OperandType, state)
	if receiverErr != nil {
		return receiverErr
	}
	if !crossModule && !generatedAssignable(node.OperandType, receiverType) {
		return unknownExpressionDiagnostic("method call receiver type does not match its checked receiver")
	}
	if err := validateExpressionChildWithState(node.Operand, receiverType, state); err != nil {
		return err
	}
	if crossModule {
		return nil
	}
	for index, argument := range node.Arguments {
		if !generatedAssignable(declared.Parameters[index].Type, argument.Type) {
			return unknownExpressionDiagnostic("method call argument type does not match its checked parameter")
		}
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

// methodReceiverType recovers the actual checked type of an adapted receiver.
// Address-of receivers carry their interned canonical pointer result from the
// checker, so the Ptr<T>/MutPtr<T> distinction is read metadata, never a
// fresh construction compared against interned identities.
func methodReceiverType(node checker.Expression, target compilerTypes.Type, state *expressionValidation) (compilerTypes.Type, error) {
	if node.Kind == checker.AddressOfExpression {
		if node.ResultType == (compilerTypes.Type{}) || !isPointerType(node.ResultType) {
			return compilerTypes.Type{}, unknownExpressionDiagnostic("method receiver address-of has no checked pointer result")
		}
		return node.ResultType, nil
	}
	if typ, ok := expressionTypeWithState(node, state); ok {
		// The checker only adapted a nullable receiver after a null test
		// narrowed it to its pointer member, so when the binding still holds
		// the declared nullable type and the method's self type is that
		// member, the receiver's effective type is the non-null member.
		if base, nullable := compilerTypes.NullableBase(typ); nullable && compilerTypes.Equal(base, target) {
			return base, nil
		}
		return typ, nil
	}
	return target, nil
}

func validateAddressExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("address-of without an operand")
	}
	if node.OperandType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("address-of has an invalid operand type")
	}
	resultType, hasResult := compilerTypes.Type{}, expected != nil
	if hasResult {
		resultType = *expected
	}
	if node.ResultType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.ResultType, state) || !isPointerType(node.ResultType) {
			return unknownExpressionDiagnostic("address-of result is not a valid pointer type")
		}
		if hasResult && !compilerTypes.Equal(resultType, node.ResultType) {
			return unknownExpressionDiagnostic("address-of result type does not match its expected type")
		}
		resultType, hasResult = node.ResultType, true
	}
	if !hasResult || !isPointerType(resultType) {
		return unknownExpressionDiagnostic("address-of result is not a pointer type")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(*resultType.Element, node.OperandType) {
		return unknownExpressionDiagnostic("address-of operand type does not match its result type")
	}
	if err := validateExpressionChildWithState(node.Operand, *resultType.Element, state); err != nil {
		return err
	}
	place, err := checkedPlaceMetadata(*node.Operand, state)
	if err != nil {
		return err
	}
	if !place.addressable {
		return unknownExpressionDiagnostic("address-of child is not addressable")
	}
	if !compilerTypes.Equal(place.typ, *resultType.Element) {
		return unknownExpressionDiagnostic("address-of child type does not match its result type")
	}
	if place.writable != resultType.PointeeWritable {
		return unknownExpressionDiagnostic("address-of result writability does not match its place")
	}
	return nil
}

func validateDereferenceExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("dereference without an operand")
	}
	resultType, hasResult := compilerTypes.Type{}, expected != nil
	if hasResult {
		resultType = *expected
	}
	if node.ResultType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("dereference result type is not supported")
		}
		if hasResult && !compilerTypes.Equal(resultType, node.ResultType) {
			return unknownExpressionDiagnostic("dereference result type does not match its expected type")
		}
		resultType, hasResult = node.ResultType, true
	}
	if node.OperandType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.OperandType, state) || !isPointerType(node.OperandType) {
			return unknownExpressionDiagnostic("dereference operand is not a valid pointer type")
		}
		if hasResult && !compilerTypes.Equal(*node.OperandType.Element, resultType) {
			return unknownExpressionDiagnostic("dereference result type does not match its operand type")
		}
	}

	receiverType, ok := expressionTypeWithState(*node.Operand, state)
	if !ok && node.OperandType != (compilerTypes.Type{}) {
		receiverType, ok = node.OperandType, true
	}
	if !ok || !supportedGeneratedTypeWithState(receiverType, state) || !isPointerType(receiverType) {
		return unknownExpressionDiagnostic("dereference receiver is not a checked pointer expression")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(receiverType, node.OperandType) {
		return unknownExpressionDiagnostic("dereference receiver type does not match its checked operand type")
	}
	if !hasResult {
		resultType, hasResult = *receiverType.Element, true
	}
	if !hasResult || !compilerTypes.Equal(*receiverType.Element, resultType) {
		return unknownExpressionDiagnostic("dereference receiver element does not match its result type")
	}
	return validateExpressionChildWithState(node.Operand, receiverType, state)
}

func validateMemberExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || node.Member == nil || !validSourceName(node.Member.Name) || !supportedGeneratedTypeWithState(node.Member.Type, state) {
		return unknownExpressionDiagnostic("member selection has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.Member.Type) {
		return unknownExpressionDiagnostic("member type does not match its expected type")
	}
	if node.ResultType != (compilerTypes.Type{}) && (!supportedGeneratedTypeWithState(node.ResultType, state) || !compilerTypes.Equal(node.ResultType, node.Member.Type) || expected != nil && !compilerTypes.Equal(*expected, node.ResultType)) {
		return unknownExpressionDiagnostic("member result type does not match its checked member")
	}
	if node.OperandType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("member receiver has an invalid checked type")
	}
	if err := validateExpressionChildWithState(node.Operand, compilerTypes.Type{}, state); err != nil {
		return err
	}
	receiverType, ok := expressionTypeWithState(*node.Operand, state)
	if !ok && node.OperandType != (compilerTypes.Type{}) {
		receiverType, ok = node.OperandType, true
	}
	if !ok || !supportedGeneratedTypeWithState(receiverType, state) || receiverType.Object == nil {
		return unknownExpressionDiagnostic("member receiver is not a checked object expression")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiverType) {
		return unknownExpressionDiagnostic("member receiver type does not match its checked receiver")
	}
	canonical, pointerOK := objectMember(receiverType.Object, node.Member)
	byName, nameOK := receiverType.Object.Member(node.Member.Name)
	if !pointerOK || !nameOK || canonical != byName || !compilerTypes.Equal(canonical.Type, node.Member.Type) {
		return unknownExpressionDiagnostic("member is not part of its checked object")
	}
	return nil
}

func validateUnaryMetadata(node checker.Expression) error {
	switch node.Operator {
	case checker.NegateOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) || !compilerTypes.IsSignedInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("negation has invalid checked types")
		}
	case checker.LogicalNotOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic("logical not requires a truthy-compatible operand and a Bool result")
		}
	case checker.BitwiseNotOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) || !compilerTypes.IsInteger(node.OperandType) || compilerTypes.IsRune(node.OperandType) {
			return unknownExpressionDiagnostic("complement has invalid checked types")
		}
	default:
		return unknownExpressionDiagnostic("unknown unary operator")
	}
	return nil
}

func validateBinaryMetadata(node checker.Expression) error {
	resultIsBool := false
	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator, checker.DivideOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("arithmetic operation with an unsupported type")
		}
		if !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("arithmetic result type does not match its operand type")
		}
	case checker.RemainderOperator:
		if !compilerTypes.IsInteger(node.OperandType) || !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("remainder operation has invalid checked types")
		}
	case checker.EqualOperator, checker.NotEqualOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("equality operation must produce Bool")
		}
		resultIsBool = true
	case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("ordering operation has invalid checked types")
		}
		resultIsBool = true
	case checker.LogicalAndOperator, checker.LogicalOrOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic("logical operation requires a truthy-compatible operand and a Bool result")
		}
		resultIsBool = true
	case checker.BitwiseAndOperator, checker.BitwiseXorOperator, checker.BitwiseOrOperator,
		checker.ShiftLeftOperator, checker.ShiftRightOperator:
		if !compilerTypes.IsInteger(node.OperandType) || compilerTypes.IsRune(node.OperandType) || !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("bitwise or shift operation has invalid checked types")
		}
	default:
		return unknownExpressionDiagnostic("unknown binary operator")
	}
	if resultIsBool != compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
		return unknownExpressionDiagnostic("binary operation has an invalid result type")
	}
	return nil
}

func validateExpressionChildWithState(child *checker.Expression, expected compilerTypes.Type, state *expressionValidation) error {
	if child == nil {
		return unknownExpressionDiagnostic("operation without a checked child")
	}
	if state.expressions == nil {
		state.expressions = make(map[*checker.Expression]bool)
	}
	if state.expressions[child] {
		return unknownExpressionDiagnostic("cyclic checked expression")
	}
	state.expressions[child] = true
	defer delete(state.expressions, child)
	return validateExpressionNode(*child, optionalType(expected, expected != compilerTypes.Type{}), state)
}

// validateTruthinessChild validates a logical operand through its
// truthiness. The nil literal is checker-supported but its other generator
// paths fail closed; truthiness contexts accept it as the constant false.
func validateTruthinessChild(child *checker.Expression, state *expressionValidation) error {
	if child.Kind == checker.NilExpression {
		return nil
	}
	return validateExpressionChildWithState(child, compilerTypes.Type{}, state)
}

func isPointerType(typ compilerTypes.Type) bool {
	return typ.Element != nil && typ.Object == nil && typ.ScalarKind == compilerTypes.ScalarNone && typ.Bits == 0
}

// checkedPlaceMetadata reconstructs place capabilities from generated bindings
// and nominal type metadata instead of trusting forged operand flags.
func checkedPlaceMetadata(node checker.Expression, state *expressionValidation) (generatedPlace, error) {
	switch node.Kind {
	case checker.VariableExpression:
		if !validSourceName(node.Name) || state == nil || state.variables == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place variable binding metadata is unavailable")
		}
		binding, ok := state.bindingFor(node)
		if !ok {
			return generatedPlace{}, unknownExpressionDiagnostic("place variable is not present in checked bindings")
		}
		for _, metadataType := range []compilerTypes.Type{node.OperandType, node.ResultType} {
			if metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(binding.typ, metadataType) {
				return generatedPlace{}, unknownExpressionDiagnostic("place variable metadata does not match its checked binding")
			}
		}
		return generatedPlace{typ: binding.typ, addressable: true, writable: binding.mutable}, nil
	case checker.MemberExpression:
		if node.Operand == nil || node.Member == nil || !validSourceName(node.Member.Name) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member has invalid checked metadata")
		}
		receiver, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if receiver.typ.Object == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place member receiver is not a checked object")
		}
		canonical, pointerOK := objectMember(receiver.typ.Object, node.Member)
		byName, nameOK := receiver.typ.Object.Member(node.Member.Name)
		if !pointerOK || !nameOK || canonical != byName || !compilerTypes.Equal(canonical.Type, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member is not part of its checked object")
		}
		if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiver.typ) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member receiver type does not match its checked receiver")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member result type does not match its checked member")
		}
		return generatedPlace{
			typ:         node.Member.Type,
			addressable: receiver.addressable,
			writable:    receiver.writable && node.Member.Mutable,
		}, nil
	case checker.DereferenceExpression:
		if node.Operand == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference has no pointer receiver")
		}
		var receiverType compilerTypes.Type
		var ok bool
		switch node.Operand.Kind {
		case checker.VariableExpression, checker.MemberExpression, checker.DereferenceExpression:
			receiver, err := checkedPlaceMetadata(*node.Operand, state)
			if err != nil {
				return generatedPlace{}, err
			}
			receiverType, ok = receiver.typ, true
		default:
			receiverType, ok = expressionTypeWithState(*node.Operand, state)
		}
		if !ok || !supportedGeneratedTypeWithState(receiverType, state) || !isPointerType(receiverType) {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference receiver is not a checked pointer")
		}
		if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiverType) {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference receiver type does not match its checked receiver")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, *receiverType.Element) {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference result type does not match its pointee")
		}
		return generatedPlace{typ: *receiverType.Element, addressable: true, writable: receiverType.PointeeWritable}, nil
	case checker.IndexExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.Array == nil && node.OperandType.View == nil && node.OperandType.List == nil && !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) {
			return generatedPlace{}, unknownExpressionDiagnostic("place index has invalid checked metadata")
		}
		receiver, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if !compilerTypes.Equal(node.OperandType, receiver.typ) {
			return generatedPlace{}, unknownExpressionDiagnostic("place index receiver type does not match its checked receiver")
		}
		var element compilerTypes.Type
		if node.OperandType.Array != nil {
			element = node.OperandType.Array.Element
		} else if node.OperandType.View != nil {
			element = node.OperandType.View.Element
		} else if node.OperandType.List != nil {
			element = node.OperandType.List.Element
		} else {
			element = compilerTypes.Rune
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, element) {
			return generatedPlace{}, unknownExpressionDiagnostic("place index result type does not match its element type")
		}
		// A View element place is never writable; a mutable Array place or
		// any live List reference is. Text indexing is read-only.
		writable := node.OperandType.Array != nil && receiver.writable || node.OperandType.List != nil
		return generatedPlace{typ: element, addressable: receiver.addressable, writable: writable}, nil
	default:
		return generatedPlace{}, unknownExpressionDiagnostic("checked expression is not a place")
	}
}

func unknownExpressionDiagnostic(detail string) error {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.UnknownError,
		Stage:    "generator",
		Message:  detail,
	}
}

// layoutEligibleGenerated is the generator-side layout gate: the type must
// have a settled representation at this point, so a type parameter is a
// generation failure (specialization must have resolved it).
func layoutEligibleGenerated(typ compilerTypes.Type) bool {
	if typ == (compilerTypes.Type{}) || compilerTypes.ContainsTypeParameter(typ) {
		return false
	}
	if compilerTypes.IsUnknown(typ) || typ.Incomplete {
		return false
	}
	if typ.Signature != nil {
		return typ.Signature.Result != nil
	}
	return compilerTypes.IsCompleteValue(typ)
}

// volatileEligibleGenerated mirrors the checker's integer-only volatile set.
func volatileEligibleGenerated(typ compilerTypes.Type) bool {
	return compilerTypes.Equal(typ, compilerTypes.Int8) ||
		compilerTypes.Equal(typ, compilerTypes.Int16) ||
		compilerTypes.Equal(typ, compilerTypes.Int32) ||
		compilerTypes.Equal(typ, compilerTypes.Int64) ||
		compilerTypes.Equal(typ, compilerTypes.UInt8) ||
		compilerTypes.Equal(typ, compilerTypes.UInt16) ||
		compilerTypes.Equal(typ, compilerTypes.UInt32) ||
		compilerTypes.Equal(typ, compilerTypes.UInt64) ||
		compilerTypes.IsSize(typ)
}

func validateIntegerConstant(source checker.Operand) error {
	if source.Kind != checker.ConstantOperand || source.Constant == nil || source.Constant.Kind() != constant.Int || !supportedGeneratedScalarType(source.Type) || !compilerTypes.IsInteger(source.Type) {
		return unknownExpressionDiagnostic("invalid checked integer constant")
	}
	if source.Radix < checker.DecimalRadix || source.Radix > checker.OctalRadix {
		return unknownExpressionDiagnostic("checked integer has an invalid radix")
	}
	value := source.Constant
	sign := constant.Sign(value)
	if compilerTypes.IsUnsignedInteger(source.Type) && (source.Negative || sign < 0) {
		return unknownExpressionDiagnostic("negative value for an unsigned integer constant")
	}
	if source.Negative && sign > 0 || !source.Negative && sign < 0 {
		return unknownExpressionDiagnostic("integer sign metadata does not match its checked value")
	}
	// Folded constants carry no literal text; only original literals are
	// re-validated against their value.
	if source.Literal == "" {
		return nil
	}
	magnitude, literalNegative, ok := parseIntegerLiteral(source.Literal, source.Radix)
	if !ok {
		return unknownExpressionDiagnostic("checked integer has an invalid literal value")
	}
	if literalNegative && !source.Negative {
		return unknownExpressionDiagnostic("integer literal sign does not match its checked metadata")
	}
	if source.Negative {
		literalValue := constant.UnaryOp(gotoken.SUB, magnitude, 0)
		if !constant.Compare(value, gotoken.EQL, literalValue) {
			return unknownExpressionDiagnostic("checked integer literal does not match its value")
		}
	} else if !constant.Compare(value, gotoken.EQL, magnitude) {
		return unknownExpressionDiagnostic("checked integer literal does not match its value")
	}
	return nil
}

func parseIntegerLiteral(literal string, radix checker.LiteralRadix) (constant.Value, bool, bool) {
	literal = strings.ReplaceAll(literal, "_", "")
	if literal == "" {
		return nil, false, false
	}
	negative := strings.HasPrefix(literal, "-")
	if negative {
		literal = literal[1:]
	}
	if literal == "" || strings.HasPrefix(literal, "+") {
		return nil, false, false
	}
	base := 10
	switch radix {
	case checker.DecimalRadix:
		if strings.HasPrefix(literal, "0x") || strings.HasPrefix(literal, "0b") || strings.HasPrefix(literal, "0o") {
			return nil, false, false
		}
	case checker.HexadecimalRadix:
		if !strings.HasPrefix(literal, "0x") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 16
	case checker.BinaryRadix:
		if !strings.HasPrefix(literal, "0b") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 2
	case checker.OctalRadix:
		if !strings.HasPrefix(literal, "0o") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 8
	default:
		return nil, false, false
	}
	if literal == "" {
		return nil, false, false
	}
	value, err := strconv.ParseUint(literal, base, 64)
	if err != nil {
		return nil, false, false
	}
	return constant.MakeUint64(value), negative, true
}
