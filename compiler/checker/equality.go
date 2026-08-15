package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

// RFC 0024: equality and ordering eligibility, the lossless numeric
// comparison widening, and the deep-comparison nodes for non-scalar values.

// equalityAvailable reports whether typ supports == and !=, returning the
// first member name that makes it unavailable. Pointers compare identity,
// so any pointer type is available; functions and allocator handles are not.
func equalityAvailable(typ compilerTypes.Type) (bool, string) {
	switch {
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if ok, _ := equalityAvailable(member.Type); !ok {
				return false, member.Name
			}
		}
		return true, ""
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if ok, _ := equalityAvailable(member.Type); !ok {
					return false, member.Name
				}
			}
		}
		return true, ""
	case typ.Union != nil:
		for _, member := range typ.Union.Members {
			if ok, reason := equalityAvailable(member); !ok {
				return false, reason
			}
		}
		return true, ""
	case typ.NullableBase != nil:
		return equalityAvailable(*typ.NullableBase)
	case typ.Array != nil:
		return equalityAvailable(typ.Array.Element)
	case typ.View != nil:
		return equalityAvailable(typ.View.Element)
	case typ.List != nil:
		return equalityAvailable(typ.List.Element)
	case typ.Element != nil:
		// Pointer identity equality never dereferences the pointee, so it
		// stays finite and always available.
		return true, ""
	case compilerTypes.IsString(typ), compilerTypes.IsStrand(typ),
		compilerTypes.IsInteger(typ), compilerTypes.IsFloat(typ),
		compilerTypes.Equal(typ, compilerTypes.Bool), compilerTypes.IsNil(typ):
		return true, ""
	case typ.Signature != nil:
		return false, ""
	case typ.Dict != nil, compilerTypes.IsHeap(typ), compilerTypes.IsUnknown(typ):
		return false, ""
	}
	return false, ""
}

// equalityUnavailableDiagnostic reports why equality is unavailable for one
// operand of the comparison.
func equalityUnavailableDiagnostic(typ compilerTypes.Type, reason string, token lexer.Token) *compilerTypes.Diagnostic {
	diagnostic := typeErrorAt(token, "equality is unavailable because member "+reason+" does not support ==")
	if reason == "" {
		switch {
		case typ.Signature != nil:
			diagnostic = typeErrorAt(token, "function values are not equality-comparable")
		case compilerTypes.IsHeap(typ):
			diagnostic = typeErrorAt(token, "allocator handles are not equality-comparable")
		case typ.Dict != nil:
			diagnostic = typeErrorAt(token, "dictionary equality is not available in v1")
		}
	}
	return &diagnostic
}

// orderingAvailable reports whether typ supports the ordering operators.
func orderingAvailable(typ compilerTypes.Type) bool {
	return compilerTypes.IsString(typ) || compilerTypes.IsStrand(typ)
}

// checkDeepComparison resolves ==, !=, and the ordering operators that the
// ordinary identical-scalar path does not own: lossless numeric widening,
// pointer identity, String and Strand text comparison, and the recursive
// equality helpers for objects, ADTs, and sequences. It returns nil when
// the plain binary path should continue.
func checkDeepComparison(operator Operator, left, right checkedExpression, token lexer.Token, environment *scope) *checkedExpression {
	// Nil and nullable-union pairs are owned by the null-test and
	// union-equality paths above.
	if compilerTypes.IsNil(left.typ) || compilerTypes.IsNil(right.typ) ||
		compilerTypes.IsUnion(left.typ) && compilerTypes.Equal(left.typ, right.typ) {
		return nil
	}
	ordering := operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator

	leftNumeric := compilerTypes.IsInteger(left.typ) || compilerTypes.IsFloat(left.typ)
	rightNumeric := compilerTypes.IsInteger(right.typ) || compilerTypes.IsFloat(right.typ)
	if leftNumeric && rightNumeric {
		if compilerTypes.Equal(left.typ, right.typ) {
			return nil
		}
		common, ok := compilerTypes.LosslessCommonType(left.typ, right.typ)
		if !ok {
			diagnostic := typeErrorAt(token, "comparison has no lossless common numeric type")
			return &checkedExpression{token: token, diagnostic: &diagnostic}
		}
		leftNode := widenNode(expressionNode(left.source), left.typ, common)
		rightNode := widenNode(expressionNode(right.source), right.typ, common)
		node := operationBinaryNode(operator, leftNode, rightNode, common, compilerTypes.Bool)
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
		return &checkedExpression{source: source, typ: compilerTypes.Bool, token: token}
	}
	if !compilerTypes.Equal(left.typ, right.typ) {
		message := fmt.Sprintf("operator %s requires identical operand types; got %s and %s", operator, left.typ.Name, right.typ.Name)
		if left.typ.Element != nil && right.typ.Element != nil {
			message = "pointer equality requires identical pointer types"
		} else if !ordering {
			message = "equality requires identical canonical non-numeric operand types"
		}
		diagnostic := typeErrorAt(token, message)
		return &checkedExpression{token: token, diagnostic: &diagnostic}
	}
	typ := left.typ
	if typ.ScalarKind != compilerTypes.ScalarNone && !ordering {
		// Bool and identical numeric scalars stay on the ordinary scalar
		// equality path; ordering eligibility below owns Bool rejection.
		return nil
	}
	if ordering {
		if !orderingAvailable(typ) {
			diagnostic := typeErrorAt(token, "ordering is unavailable for "+typ.Name+" values")
			return &checkedExpression{token: token, diagnostic: &diagnostic}
		}
		leftNode := expressionNode(left.source)
		rightNode := expressionNode(right.source)
		node := Expression{
			Kind:        StringCompareExpression,
			Left:        &leftNode,
			Right:       &rightNode,
			Operator:    operator,
			OperandType: typ,
			ResultType:  compilerTypes.Bool,
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
		return &checkedExpression{source: source, typ: compilerTypes.Bool, token: token}
	}
	if typ.Element != nil {
		// Pointer identity equality lowers through the ordinary scalar path
		// with the identical pointer type as the operand type.
		leftNode := expressionNode(left.source)
		rightNode := expressionNode(right.source)
		node := operationBinaryNode(operator, leftNode, rightNode, typ, compilerTypes.Bool)
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
		return &checkedExpression{source: source, typ: compilerTypes.Bool, token: token}
	}
	if ok, reason := equalityAvailable(typ); !ok {
		return &checkedExpression{token: token, diagnostic: equalityUnavailableDiagnostic(typ, reason, token)}
	}
	leftNode := expressionNode(left.source)
	rightNode := expressionNode(right.source)
	node := Expression{
		Kind:        DeepEqualityExpression,
		Left:        &leftNode,
		Right:       &rightNode,
		Operator:    operator,
		OperandType: typ,
		ResultType:  compilerTypes.Bool,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
	return &checkedExpression{source: source, typ: compilerTypes.Bool, token: token}
}

// widenNode wraps one operand in a proven lossless widening cast to the
// comparison's common numeric type. Identity is not wrapped.
func widenNode(operand Expression, source, destination compilerTypes.Type) Expression {
	if compilerTypes.Equal(source, destination) {
		return operand
	}
	return Expression{
		Kind:        WideningExpression,
		Operand:     &operand,
		OperandType: source,
		ResultType:  destination,
	}
}
