package generator

// walk.go is the single fail-closed program walker shared by every discovery
// pass. Adding a new checked statement or expression shape requires updating
// only this file; collectors stay focused on what they collect and never
// re-implement traversal.
//
// The walk is a deterministic pre-order: every callback fires at node entry,
// before the node's children. Discovery passes relying on first-visit order
// (declaration order, dependency order) see the same sequence the previous
// per-collector walkers produced.
//
// The visitor's callbacks are optional; nil callbacks are ignored. A
// statement or expression shape this walker does not know is a generator
// error, never a silent skip.

import (
	"fmt"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// programVisitor receives every visited node of a checked program.
//
//   - Type fires for every type the walk reaches, including operand types and
//     the types of literal constants whose checked node is InvalidExpression.
//   - Operand fires for every operand, before its node is walked. It is the
//     only hook that sees operand-level metadata (Constant, Literal,
//     Addressable) rather than just a type and a node.
//   - Expression fires for every non-invalid checked expression node.
//   - Statement fires for every checked statement, at every nesting depth,
//     before that statement's own type-directed descent.
//
// Callbacks are optional; nil callbacks are ignored. A callback error stops
// the walk and surfaces through walkProgram, so discovery reports a
// checker-contract break as an [Unknown Error] instead of panicking.
type programVisitor struct {
	Type       func(compilerTypes.Type) error
	Operand    func(checker.Operand) error
	Expression func(checker.Expression) error
	Statement  func(checker.Statement) error
}

// walkTypeTree visits typ and every type structurally reachable from it
// (ADT variant payloads, union members, pointer pointees, function
// signatures, object members), firing visit for each in pre-order.
// walkProgram's Type callback fires through the same structural rules, so a
// collector that needs a narrower scope (for example print, which only
// collects types reachable from print arguments) can reuse this descent.
// Recursion is deduplicated per ADT and per object, so cyclic objects
// terminate; the visit still fires on every mention of the dedup key.
func walkTypeTree(typ compilerTypes.Type, visit func(compilerTypes.Type) error) error {
	if visit == nil {
		return nil
	}
	return walkTypeTreeSeen(typ, visit, make(map[*compilerTypes.AdtType]bool), make(map[*compilerTypes.ObjectType]bool))
}

func walkTypeTreeSeen(typ compilerTypes.Type, visit func(compilerTypes.Type) error, seenAdt map[*compilerTypes.AdtType]bool, seenObject map[*compilerTypes.ObjectType]bool) error {
	if err := visit(typ); err != nil {
		return err
	}
	if typ.Adt != nil {
		if seenAdt[typ.Adt] {
			return nil
		}
		seenAdt[typ.Adt] = true
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if err := walkTypeTreeSeen(member.Type, visit, seenAdt, seenObject); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if typ.Union != nil {
		for _, member := range typ.Union.Members {
			if err := walkTypeTreeSeen(member, visit, seenAdt, seenObject); err != nil {
				return err
			}
		}
	}
	if typ.Element != nil {
		if err := walkTypeTreeSeen(*typ.Element, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.NullableBase != nil {
		if err := walkTypeTreeSeen(*typ.NullableBase, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.Signature != nil {
		for _, parameter := range typ.Signature.Parameters {
			if err := walkTypeTreeSeen(parameter, visit, seenAdt, seenObject); err != nil {
				return err
			}
		}
		if typ.Signature.Result != nil {
			if err := walkTypeTreeSeen(*typ.Signature.Result, visit, seenAdt, seenObject); err != nil {
				return err
			}
		}
	}
	if typ.Array != nil {
		if err := walkTypeTreeSeen(typ.Array.Element, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.View != nil {
		if err := walkTypeTreeSeen(typ.View.Element, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.List != nil {
		if err := walkTypeTreeSeen(typ.List.Element, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.Dict != nil {
		if err := walkTypeTreeSeen(typ.Dict.Key, visit, seenAdt, seenObject); err != nil {
			return err
		}
		if err := walkTypeTreeSeen(typ.Dict.Value, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.Task != nil {
		if err := walkTypeTreeSeen(typ.Task.Result, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.Channel != nil {
		if err := walkTypeTreeSeen(typ.Channel.Element, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.Atomic != nil {
		if err := walkTypeTreeSeen(typ.Atomic.Element, visit, seenAdt, seenObject); err != nil {
			return err
		}
	}
	if typ.Object != nil {
		if seenObject[typ.Object] {
			return nil
		}
		seenObject[typ.Object] = true
		for _, member := range typ.Object.Members {
			if err := walkTypeTreeSeen(member.Type, visit, seenAdt, seenObject); err != nil {
				return err
			}
		}
	}
	return nil
}

// walkStatementExpressions visits every expression reachable directly from
// one statement in pre-order: the statement's operands and their nested
// sub-expressions (Operand, Left, Right, Arguments), in written operand and
// argument order. It does not descend into nested statement bodies; each
// statement list's own render hoists its prologues, so a nested statement's
// hoisted prologue stays at that statement's indentation. The statement is
// traversed by value so hoisting never escapes an address of a field on a
// type-switch copy; hoist keys are the checked tree's own child pointers,
// which the visited copies still carry. An unknown statement shape is a
// generator error, never a silent skip.
func walkStatementExpressions(statement checker.Statement, visit func(checker.Expression) error) error {
	if visit == nil {
		return nil
	}
	switch statement := statement.(type) {
	case checker.Declaration:
		return walkStatementOperand(statement.Source, visit)
	case checker.Assignment:
		if err := walkStatementOperand(statement.Source, visit); err != nil {
			return err
		}
		return walkStatementOperand(statement.Target, visit)
	case checker.CallStatement:
		return walkStatementExpression(statement.Call.Node, visit)
	case checker.TryStatement:
		return walkStatementOperand(statement.Expression, visit)
	case checker.ReturnStatement:
		if statement.Value != nil {
			return walkStatementOperand(*statement.Value, visit)
		}
	case checker.DeferStatement:
		return walkStatementOperand(statement.Expression, visit)
	case checker.ErrdeferStatement:
		return walkStatementOperand(statement.Expression, visit)
	case checker.IfStatement:
		if err := walkStatementOperand(statement.Condition, visit); err != nil {
			return err
		}
		for _, branch := range statement.ElseIf {
			if err := walkStatementOperand(branch.Condition, visit); err != nil {
				return err
			}
		}
	case checker.ForStatement:
		return walkStatementOperand(statement.Source, visit)
	case checker.WhileStatement:
		return walkStatementOperand(statement.Condition, visit)
	case checker.BreakStatement, checker.ContinueStatement, checker.FunctionDeclaration, checker.MethodDeclaration:
		// No expressions reachable directly from these shapes; nested
		// bodies are the caller's recursion.
	default:
		// A statement kind reaching this default is a checker-to-generator
		// contract break; the typed diagnostic keeps its [Unknown Error]
		// category intact through Stderr rendering.
		return compilerTypes.NewDiagnostic(compilerTypes.UnknownError, "generator", 0, 0,
			fmt.Sprintf("generator walker cannot visit statement of type %T", statement))
	}
	return nil
}

// walkStatementExpression visits one expression and its nested sub-expressions
// in pre-order. It is package-level and takes the callback as a parameter so a
// per-statement walk allocates no closures.
func walkStatementExpression(node checker.Expression, visit func(checker.Expression) error) error {
	if err := visit(node); err != nil {
		return err
	}
	if node.Operand != nil {
		if err := walkStatementExpression(*node.Operand, visit); err != nil {
			return err
		}
	}
	if node.Left != nil {
		if err := walkStatementExpression(*node.Left, visit); err != nil {
			return err
		}
	}
	if node.Right != nil {
		if err := walkStatementExpression(*node.Right, visit); err != nil {
			return err
		}
	}
	for index := range node.Arguments {
		if err := walkStatementOperand(node.Arguments[index], visit); err != nil {
			return err
		}
	}
	return nil
}

// walkStatementOperand visits one operand's expression; an invalid operand
// carries no reachable expression.
func walkStatementOperand(source checker.Operand, visit func(checker.Expression) error) error {
	if source.Node.Kind != checker.InvalidExpression {
		return walkStatementExpression(source.Node, visit)
	}
	return nil
}

// walkState is one traversal's working set: the visitor plus the dedup maps.
// It lives only for the duration of one walk, so no state is shared across
// compilations and no per-call closures or callback wrappers are allocated.
type walkState struct {
	visitor *programVisitor
	// seenAdt and seenObject keep recursion acyclic and idempotent. ADTs are
	// interned and their payloads are complete, but a shared ADT must not be
	// re-descended at every mention; objects may be recursive, so member
	// descent happens once per object. Deduplication is the walker's concern:
	// the Type callback still fires on every mention, and collectors keep
	// their own dedup keys when discovery must see first-mention order.
	seenAdt    map[*compilerTypes.AdtType]bool
	seenObject map[*compilerTypes.ObjectType]bool
}

// walkProgram visits program.TypeDeclarations, program.Statements, then the
// bodies of every specialized function and method, in that order. Every
// checked statement shape is dispatched explicitly; a shape this walker does
// not know returns an Unknown Error, never a silent skip. Checked
// expressions need no shape dispatch: the structural descent visits Operand,
// Left, Right, and Arguments regardless of kind. The walk stops on the first
// callback error.
func walkProgram(program checker.Program, visitor *programVisitor) error {
	countTraversal()
	if visitor == nil {
		// A nil visitor collects nothing; the walk is a pure traversal.
		return nil
	}
	state := &walkState{
		visitor:    visitor,
		seenAdt:    make(map[*compilerTypes.AdtType]bool),
		seenObject: make(map[*compilerTypes.ObjectType]bool),
	}
	for _, declaration := range program.TypeDeclarations {
		if err := state.walkType(declaration.Type); err != nil {
			return err
		}
	}
	if err := state.walkStatements(program.Statements); err != nil {
		return err
	}
	for _, function := range program.SpecializedFunctions {
		if err := state.walkFunctionBody(function.Type, function.Parameters, function.Result, function.Body); err != nil {
			return err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := state.walkMethodBody(method.SelfType, method.Parameters, method.Result, method.Body); err != nil {
			return err
		}
	}
	return nil
}

func (state *walkState) walkType(typ compilerTypes.Type) error {
	if state.visitor.Type != nil {
		if err := state.visitor.Type(typ); err != nil {
			return err
		}
	}
	if typ.Adt != nil {
		if state.seenAdt[typ.Adt] {
			return nil
		}
		state.seenAdt[typ.Adt] = true
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if err := state.walkType(member.Type); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if typ.Union != nil {
		for _, member := range typ.Union.Members {
			if err := state.walkType(member); err != nil {
				return err
			}
		}
	}
	if typ.Element != nil {
		if err := state.walkType(*typ.Element); err != nil {
			return err
		}
	}
	if typ.NullableBase != nil {
		if err := state.walkType(*typ.NullableBase); err != nil {
			return err
		}
	}
	if typ.Array != nil {
		if err := state.walkType(typ.Array.Element); err != nil {
			return err
		}
	}
	if typ.View != nil {
		if err := state.walkType(typ.View.Element); err != nil {
			return err
		}
	}
	if typ.List != nil {
		if err := state.walkType(typ.List.Element); err != nil {
			return err
		}
	}
	if typ.Dict != nil {
		if err := state.walkType(typ.Dict.Key); err != nil {
			return err
		}
		if err := state.walkType(typ.Dict.Value); err != nil {
			return err
		}
	}
	if typ.Task != nil {
		if err := state.walkType(typ.Task.Result); err != nil {
			return err
		}
	}
	if typ.Channel != nil {
		if err := state.walkType(typ.Channel.Element); err != nil {
			return err
		}
	}
	if typ.Atomic != nil {
		if err := state.walkType(typ.Atomic.Element); err != nil {
			return err
		}
	}
	if typ.Signature != nil {
		for _, parameter := range typ.Signature.Parameters {
			if err := state.walkType(parameter); err != nil {
				return err
			}
		}
		if typ.Signature.Result != nil {
			if err := state.walkType(*typ.Signature.Result); err != nil {
				return err
			}
		}
	}
	if typ.Object != nil {
		if state.seenObject[typ.Object] {
			return nil
		}
		state.seenObject[typ.Object] = true
		for _, member := range typ.Object.Members {
			if err := state.walkType(member.Type); err != nil {
				return err
			}
		}
	}
	return nil
}

func (state *walkState) walkExpression(node checker.Expression) error {
	countNode()
	if state.visitor.Expression != nil {
		if err := state.visitor.Expression(node); err != nil {
			return err
		}
	}
	if node.Constant != nil {
		if err := state.walkOperand(*node.Constant); err != nil {
			return err
		}
	}
	if err := state.walkType(node.OperandType); err != nil {
		return err
	}
	if err := state.walkType(node.ResultType); err != nil {
		return err
	}
	if node.Element != (compilerTypes.Type{}) {
		if err := state.walkType(node.Element); err != nil {
			return err
		}
	}
	if node.TestType != (compilerTypes.Type{}) {
		if err := state.walkType(node.TestType); err != nil {
			return err
		}
	}
	if node.Operand != nil {
		if err := state.walkExpression(*node.Operand); err != nil {
			return err
		}
	}
	if node.Left != nil {
		if err := state.walkExpression(*node.Left); err != nil {
			return err
		}
	}
	if node.Right != nil {
		if err := state.walkExpression(*node.Right); err != nil {
			return err
		}
	}
	for _, argument := range node.Arguments {
		if err := state.walkOperand(argument); err != nil {
			return err
		}
	}
	if node.Function != nil {
		if err := state.walkCallable(node.Function.Type, node.Function.Parameters, node.Function.Result, node.Function.Body); err != nil {
			return err
		}
	}
	return nil
}

func (state *walkState) walkOperand(source checker.Operand) error {
	if err := state.walkType(source.Type); err != nil {
		return err
	}
	if state.visitor.Operand != nil {
		if err := state.visitor.Operand(source); err != nil {
			return err
		}
	}
	// Object literals carry their field initializers on the operand;
	// every discovery pass that sees collection element types relies on
	// walking them.
	if source.Object != nil {
		for _, initializer := range source.Object.Initializers {
			if err := state.walkOperand(initializer.Source); err != nil {
				return err
			}
		}
	}
	if source.Node.Kind != checker.InvalidExpression {
		return state.walkExpression(source.Node)
	}
	return nil
}

// walkFunctionBody visits one specialized function's signature surface then
// its body; walkMethodBody does the same for a method's receiver form.
func (state *walkState) walkFunctionBody(typ compilerTypes.Type, parameters []checker.FunctionParameter, result *compilerTypes.Type, body []checker.Statement) error {
	return state.walkCallable(typ, parameters, result, body)
}

func (state *walkState) walkMethodBody(selfType compilerTypes.Type, parameters []checker.FunctionParameter, result *compilerTypes.Type, body []checker.Statement) error {
	return state.walkCallable(selfType, parameters, result, body)
}

func (state *walkState) walkCallable(signature compilerTypes.Type, parameters []checker.FunctionParameter, result *compilerTypes.Type, body []checker.Statement) error {
	if err := state.walkType(signature); err != nil {
		return err
	}
	for _, parameter := range parameters {
		if err := state.walkType(parameter.Type); err != nil {
			return err
		}
	}
	if result != nil {
		if err := state.walkType(*result); err != nil {
			return err
		}
	}
	return state.walkStatements(body)
}

func (state *walkState) walkStatements(statements []checker.Statement) error {
	for _, statement := range statements {
		if state.visitor.Statement != nil {
			if err := state.visitor.Statement(statement); err != nil {
				return err
			}
		}
		switch statement := statement.(type) {
		case checker.Declaration:
			if err := state.walkType(statement.Type); err != nil {
				return err
			}
			if err := state.walkOperand(statement.Source); err != nil {
				return err
			}
		case checker.Assignment:
			if err := state.walkOperand(statement.Source); err != nil {
				return err
			}
			if err := state.walkOperand(statement.Target); err != nil {
				return err
			}
		case checker.CallStatement:
			if err := state.walkOperand(statement.Call); err != nil {
				return err
			}
		case checker.ReturnStatement:
			if statement.Value != nil {
				if err := state.walkOperand(*statement.Value); err != nil {
					return err
				}
			}
		case checker.DeferStatement:
			if err := state.walkOperand(statement.Expression); err != nil {
				return err
			}
		case checker.ErrdeferStatement:
			if err := state.walkOperand(statement.Expression); err != nil {
				return err
			}
		case checker.TryStatement:
			if err := state.walkOperand(statement.Expression); err != nil {
				return err
			}
		case checker.BreakStatement, checker.ContinueStatement:
			// No children; the case exists so these shapes are known.
		case checker.IfStatement:
			if err := state.walkOperand(statement.Condition); err != nil {
				return err
			}
			if err := state.walkStatements(statement.Then); err != nil {
				return err
			}
			for _, branch := range statement.ElseIf {
				if err := state.walkOperand(branch.Condition); err != nil {
					return err
				}
				if err := state.walkStatements(branch.Body); err != nil {
					return err
				}
			}
			if statement.Else != nil {
				if err := state.walkStatements(statement.Else); err != nil {
					return err
				}
			}
		case checker.ForStatement:
			if err := state.walkOperand(statement.Source); err != nil {
				return err
			}
			if err := state.walkStatements(statement.Body); err != nil {
				return err
			}
		case checker.WhileStatement:
			if err := state.walkOperand(statement.Condition); err != nil {
				return err
			}
			if err := state.walkStatements(statement.Body); err != nil {
				return err
			}
		case checker.FunctionDeclaration:
			if err := state.walkFunctionBody(statement.Type, statement.Parameters, statement.Result, statement.Body); err != nil {
				return err
			}
		case checker.MethodDeclaration:
			if err := state.walkMethodBody(statement.SelfType, statement.Parameters, statement.Result, statement.Body); err != nil {
				return err
			}
		default:
			// A statement kind reaching this default is a checker-to-
			// generator contract break; the typed diagnostic keeps its
			// [Unknown Error] category through Stderr rendering.
			return compilerTypes.NewDiagnostic(compilerTypes.UnknownError, "generator", 0, 0,
				fmt.Sprintf("generator walker cannot visit statement of type %T", statement))
		}
	}
	return nil
}
