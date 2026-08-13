package generator

// walk.go — RFC 0049 item 7: the single fail-closed program walker shared by
// every discovery pass. Adding a new checked statement or expression shape
// requires updating only this file; collectors stay focused on what they
// collect and never re-implement traversal.
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
type programVisitor struct {
	Type       func(compilerTypes.Type) error
	Operand    func(checker.Operand) error
	Expression func(checker.Expression) error
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

// walkProgram visits program.TypeDeclarations, program.Statements, then the
// bodies of every specialized function and method, in that order.
func walkProgram(program checker.Program, visitor *programVisitor) error {
	if visitor == nil {
		// A nil visitor collects nothing; the walk is a pure traversal.
		return nil
	}
	var walkType func(compilerTypes.Type) error
	var walkOperand func(checker.Operand) error
	var walkExpression func(checker.Expression) error
	var walkStatements func([]checker.Statement) error

	// seenAdt and seenObject keep recursion acyclic and idempotent. ADTs are
	// interned and their payloads are complete, but a shared ADT must not be
	// re-descended at every mention; objects may be recursive, so member
	// descent happens once per object. Deduplication is the walker's concern:
	// the Type callback still fires on every mention, and collectors keep
	// their own dedup keys when discovery must see first-mention order.
	seenAdt := make(map[*compilerTypes.AdtType]bool)
	seenObject := make(map[*compilerTypes.ObjectType]bool)

	walkType = func(typ compilerTypes.Type) error {
		if visitor.Type != nil {
			if err := visitor.Type(typ); err != nil {
				return err
			}
		}
		if typ.Adt != nil {
			if seenAdt[typ.Adt] {
				return nil
			}
			seenAdt[typ.Adt] = true
			for _, variant := range typ.Adt.Variants {
				for _, member := range variant.Payload {
					if err := walkType(member.Type); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if typ.Union != nil {
			for _, member := range typ.Union.Members {
				if err := walkType(member); err != nil {
					return err
				}
			}
		}
		if typ.Element != nil {
			if err := walkType(*typ.Element); err != nil {
				return err
			}
		}
		if typ.NullableBase != nil {
			if err := walkType(*typ.NullableBase); err != nil {
				return err
			}
		}
		if typ.Array != nil {
			if err := walkType(typ.Array.Element); err != nil {
				return err
			}
		}
		if typ.View != nil {
			if err := walkType(typ.View.Element); err != nil {
				return err
			}
		}
		if typ.List != nil {
			if err := walkType(typ.List.Element); err != nil {
				return err
			}
		}
		if typ.Dict != nil {
			if err := walkType(typ.Dict.Key); err != nil {
				return err
			}
			if err := walkType(typ.Dict.Value); err != nil {
				return err
			}
		}
		if typ.Signature != nil {
			for _, parameter := range typ.Signature.Parameters {
				if err := walkType(parameter); err != nil {
					return err
				}
			}
			if typ.Signature.Result != nil {
				if err := walkType(*typ.Signature.Result); err != nil {
					return err
				}
			}
		}
		if typ.Object != nil {
			if seenObject[typ.Object] {
				return nil
			}
			seenObject[typ.Object] = true
			for _, member := range typ.Object.Members {
				if err := walkType(member.Type); err != nil {
					return err
				}
			}
		}
		return nil
	}

	walkExpression = func(node checker.Expression) error {
		if visitor.Expression != nil {
			if err := visitor.Expression(node); err != nil {
				return err
			}
		}
		if node.Constant != nil {
			if err := walkOperand(*node.Constant); err != nil {
				return err
			}
		}
		if err := walkType(node.OperandType); err != nil {
			return err
		}
		if err := walkType(node.ResultType); err != nil {
			return err
		}
		if node.Element != (compilerTypes.Type{}) {
			if err := walkType(node.Element); err != nil {
				return err
			}
		}
		if node.TestType != (compilerTypes.Type{}) {
			if err := walkType(node.TestType); err != nil {
				return err
			}
		}
		if node.Operand != nil {
			if err := walkExpression(*node.Operand); err != nil {
				return err
			}
		}
		if node.Left != nil {
			if err := walkExpression(*node.Left); err != nil {
				return err
			}
		}
		if node.Right != nil {
			if err := walkExpression(*node.Right); err != nil {
				return err
			}
		}
		for _, argument := range node.Arguments {
			if err := walkOperand(argument); err != nil {
				return err
			}
		}
		return nil
	}

	walkOperand = func(source checker.Operand) error {
		if err := walkType(source.Type); err != nil {
			return err
		}
		if visitor.Operand != nil {
			if err := visitor.Operand(source); err != nil {
				return err
			}
		}
		// Object literals carry their field initializers on the operand;
		// every discovery pass that sees collection element types relies on
		// walking them.
		if source.Object != nil {
			for _, initializer := range source.Object.Initializers {
				if err := walkOperand(initializer.Source); err != nil {
					return err
				}
			}
		}
		if source.Node.Kind != checker.InvalidExpression {
			return walkExpression(source.Node)
		}
		return nil
	}

	walkStatements = func(statements []checker.Statement) error {
		for _, statement := range statements {
			switch statement := statement.(type) {
			case checker.Declaration:
				if err := walkType(statement.Type); err != nil {
					return err
				}
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
			case checker.Assignment:
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
				if err := walkOperand(statement.Target); err != nil {
					return err
				}
			case checker.CallStatement:
				if err := walkOperand(statement.Call); err != nil {
					return err
				}
			case checker.ReturnStatement:
				if statement.Value != nil {
					if err := walkOperand(*statement.Value); err != nil {
						return err
					}
				}
			case checker.DeferStatement:
				if err := walkOperand(statement.Expression); err != nil {
					return err
				}
			case checker.ErrdeferStatement:
				if err := walkOperand(statement.Expression); err != nil {
					return err
				}
			case checker.TryStatement:
				if err := walkOperand(statement.Expression); err != nil {
					return err
				}
			case checker.BreakStatement, checker.ContinueStatement:
				// No children; the case exists so these shapes are known.
			case checker.IfStatement:
				if err := walkOperand(statement.Condition); err != nil {
					return err
				}
				if err := walkStatements(statement.Then); err != nil {
					return err
				}
				for _, branch := range statement.ElseIf {
					if err := walkOperand(branch.Condition); err != nil {
						return err
					}
					if err := walkStatements(branch.Body); err != nil {
						return err
					}
				}
				if statement.Else != nil {
					if err := walkStatements(statement.Else); err != nil {
						return err
					}
				}
			case checker.ForStatement:
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.WhileStatement:
				if err := walkOperand(statement.Condition); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.FunctionDeclaration:
				if err := walkType(statement.Type); err != nil {
					return err
				}
				for _, parameter := range statement.Parameters {
					if err := walkType(parameter.Type); err != nil {
						return err
					}
				}
				if statement.Result != nil {
					if err := walkType(*statement.Result); err != nil {
						return err
					}
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.MethodDeclaration:
				if err := walkType(statement.SelfType); err != nil {
					return err
				}
				for _, parameter := range statement.Parameters {
					if err := walkType(parameter.Type); err != nil {
						return err
					}
				}
				if statement.Result != nil {
					if err := walkType(*statement.Result); err != nil {
						return err
					}
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			default:
				return fmt.Errorf("Unknown Error: generator walker cannot visit statement of type %T", statement)
			}
		}
		return nil
	}

	for _, declaration := range program.TypeDeclarations {
		if err := walkType(declaration.Type); err != nil {
			return err
		}
	}
	if err := walkStatements(program.Statements); err != nil {
		return err
	}
	for _, function := range program.SpecializedFunctions {
		if err := walkType(function.Type); err != nil {
			return err
		}
		for _, parameter := range function.Parameters {
			if err := walkType(parameter.Type); err != nil {
				return err
			}
		}
		if err := walkStatements(function.Body); err != nil {
			return err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := walkType(method.SelfType); err != nil {
			return err
		}
		for _, parameter := range method.Parameters {
			if err := walkType(parameter.Type); err != nil {
				return err
			}
		}
		if err := walkStatements(method.Body); err != nil {
			return err
		}
	}
	return nil
}
