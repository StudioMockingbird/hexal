// validation.go owns validation entry: the checked-program walk and its
// function, method, statement, and condition layers.
package generator

import (
	"hexal/compiler/checker"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

func validateCheckedProgram(program checker.Program, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, stringState *literalRegistry, table *span.Table) error {
	typeState := &generatedTypeValidation{declaredObjects: errorDeclaredObjects(program)}
	state := newExpressionValidation()
	state.functions = functions
	state.methods = methods
	state.generatedTypes = typeState
	state.strings = stringState
	state.table = table
	state.pushScope()
	for _, typeDeclaration := range program.TypeDeclarations {
		if !validSourceName(typeDeclaration.Name) {
			return unknownExpressionDiagnostic()
		}
		if !validateGeneratedType(typeDeclaration.Type, typeState, false) {
			return unknownExpressionDiagnostic()
		}
	}
	if err := validateStatements(program.Statements, state, typeState); err != nil {
		return err
	}
	for _, function := range program.SpecializedFunctions {
		if err := validateFunctionDeclaration(function, typeState, functions, methods, stringState, table); err != nil {
			return err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := validateMethodDeclaration(method, typeState, functions, methods, stringState, table); err != nil {
			return err
		}
	}
	return state.requireRootScope("validated project")
}

// validateFunctionDeclaration validates one concrete function declaration and
// its body without mutating the main statement state. stringState is the
// shared literal registry: the preflight renders call statements to prove
// them renderable, and a string-literal argument must resolve against the
// same registry the emission pass uses.
func validateFunctionDeclaration(declared checker.FunctionDeclaration, typeState *generatedTypeValidation, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, stringState *literalRegistry, table *span.Table) error {
	if !validSourceName(declared.Name) || declared.Type.Signature == nil || !validateGeneratedType(declared.Type, typeState, false) {
		return unknownExpressionDiagnostic()
	}
	state := newExpressionValidation()
	state.functions = functions
	state.methods = methods
	state.generatedTypes = typeState
	state.strings = stringState
	state.table = table
	state.pushScope()
	if declared.EnvDependent {
		if err := registerEnvironment(state, declared.Captures); err != nil {
			return err
		}
	}
	for _, parameter := range declared.Parameters {
		if _, err := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false); err != nil {
			return err
		}
	}
	if err := validateStatements(declared.Body, state, typeState); err != nil {
		return err
	}
	return state.requireRootScope("validated function")
}

// validateMethodDeclaration validates one concrete method declaration and its
// body. stringState is the shared literal registry, threaded through for the
// same reason as validateFunctionDeclaration.
func validateMethodDeclaration(declared checker.MethodDeclaration, typeState *generatedTypeValidation, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, stringState *literalRegistry, table *span.Table) error {
	if declared.Object == nil || !validSourceName(declared.Name) || !validateGeneratedType(declared.SelfType, typeState, false) {
		return unknownExpressionDiagnostic()
	}
	state := newExpressionValidation()
	state.functions = functions
	state.methods = methods
	state.generatedTypes = typeState
	state.strings = stringState
	state.table = table
	state.pushScope()
	if declared.EnvDependent {
		if err := registerEnvironment(state, declared.Captures); err != nil {
			return err
		}
	}
	if _, err := state.allocateBinding(declared.SelfBinding, "self", declared.SelfType, false); err != nil {
		return err
	}
	for _, parameter := range declared.Parameters {
		if _, err := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false); err != nil {
			return err
		}
	}
	if err := validateStatements(declared.Body, state, typeState); err != nil {
		return err
	}
	return state.requireRootScope("validated method")
}

func validateStatements(statements []checker.Statement, state *expressionValidation, typeState *generatedTypeValidation) error {
	for _, statement := range statements {
		switch statement := statement.(type) {
		case checker.Declaration:
			if !validSourceName(statement.Name) || !validateGeneratedType(statement.Type, typeState, false) {
				return unknownExpressionDiagnostic()
			}
			if statement.Binding == 0 {
				if _, exists := state.variables[statement.Name]; exists {
					return unknownExpressionDiagnostic()
				}
			}
			if err := validateCheckedOperandWithState(statement.Source, state); err != nil {
				return err
			}
			if !generatedAssignable(statement.Type, statement.Source.Type) {
				return unknownExpressionDiagnostic()
			}
			if _, err := state.allocateBinding(statement.Binding, statement.Name, statement.Type, statement.Mutable); err != nil {
				return err
			}
		case checker.Assignment:
			// An operator-opened target (such as ^pointer) carries the
			// operator spelling as its name: the target node kind owns the
			// validity check instead of the source-name gate.
			nameValid := validSourceName(statement.Name) ||
				(statement.Target.Node.Kind == checker.DereferenceExpression && statement.Name == "^")
			if !nameValid || !validateGeneratedType(statement.Type, typeState, false) || !validateGeneratedType(statement.Target.Type, typeState, false) {
				return unknownExpressionDiagnostic()
			}
			if err := validateCheckedOperandWithState(statement.Target, state); err != nil {
				return err
			}
			targetPlace, err := checkedPlaceMetadata(statement.Target.Node, state)
			if err != nil {
				return err
			}
			if !targetPlace.addressable || !targetPlace.writable {
				return unknownExpressionDiagnostic()
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
					return unknownExpressionDiagnostic()
				}
			}
			if !generatedAssignable(statement.Type, statement.Source.Type) {
				return unknownExpressionDiagnostic()
			}
		case checker.CallStatement:
			if statement.Call.Node.Kind == checker.PrintExpression {
				// print validates its arguments and produces no value; the
				// statement renderer emits its own statements. Continue so
				// the statements after print still pass preflight.
				continue
			}
			if err := validateCallStatement(statement, state); err != nil {
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
					return unknownExpressionDiagnostic()
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
		case checker.RootReturnStatement:
			// The entry status type is checked while rendering the root
			// return; the preflight pass only validates the value shape.
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
			if err := state.popScope(); err != nil {
				return err
			}
			for _, branch := range statement.ElseIf {
				if err := validateCondition(branch.Condition, state); err != nil {
					return err
				}
				state.pushScope()
				if err := validateStatements(branch.Body, state, typeState); err != nil {
					return err
				}
				if err := state.popScope(); err != nil {
					return err
				}
			}
			if statement.Else != nil {
				state.pushScope()
				if err := validateStatements(statement.Else, state, typeState); err != nil {
					return err
				}
				if err := state.popScope(); err != nil {
					return err
				}
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
			popErr := state.popScope()
			if popErr != nil {
				return popErr
			}
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
					return unknownExpressionDiagnostic()
				}
				if _, err := state.allocateBinding(binder.Binding, binder.Name, binder.Type, false); err != nil {
					return err
				}
			}
			previousLoopDepth := state.loopDepth
			state.loopDepth++
			err := validateStatements(statement.Body, state, typeState)
			state.loopDepth = previousLoopDepth
			if err := state.popScope(); err != nil {
				return err
			}
			if err != nil {
				return err
			}
		case checker.UnsafeStatement:
			state.pushScope()
			err := validateStatements(statement.Body, state, typeState)
			if err := state.popScope(); err != nil {
				return err
			}
			if err != nil {
				return err
			}
		case checker.ErrdeferStatement:
			if statement.Action.IsCall {
				if statement.Action.Call == nil {
					return unknownExpressionDiagnostic()
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
				return unknownExpressionDiagnostic()
			}
		case checker.ContinueStatement:
			if state.loopDepth == 0 {
				return unknownExpressionDiagnostic()
			}
		case checker.FunctionDeclaration:
			if len(state.activeScopes) > 1 {
				return unknownExpressionDiagnostic()
			}
			continue
		case checker.MethodDeclaration:
			if len(state.activeScopes) > 1 {
				return unknownExpressionDiagnostic()
			}
			continue
		default:
			return unknownExpressionDiagnostic()
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
		return unknownExpressionDiagnostic()
	}
	return validateCheckedOperandWithState(condition, state)
}
