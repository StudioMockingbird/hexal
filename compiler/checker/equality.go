package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// Equality and ordering eligibility, the lossless numeric comparison
// widening, and the deep-comparison nodes for non-scalar values.

// typeFacts resolves one compiler-owned type to its recorded facts. It is a
// variable so a test can replace it and prove the comparison consumer reads the
// registry rather than restating the fact.
var typeFacts = compilerTypes.TypeFactsOf

// EqualityAvailable reports whether typ supports == and !=, returning the
// first member name that makes it unavailable. Pointers compare identity,
// so any pointer type is available; functions and allocator handles are not.
func EqualityAvailable(typ compilerTypes.Type) (bool, string) {
	switch {
	case typ.Object != nil:
		if compilerTypes.IsForeignRecord(typ) {
			// A foreign record's layout belongs to the C compiler; recursive
			// equality over it would need a Hexal-owned definition.
			return false, "foreign record " + typ.Name
		}
		for _, member := range typ.Object.Members {
			if ok, _ := EqualityAvailable(member.Type); !ok {
				return false, "member " + member.Name
			}
		}
		return true, ""
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if ok, _ := EqualityAvailable(member.Type); !ok {
					return false, "member " + member.Name
				}
			}
		}
		return true, ""
	case typ.Union != nil:
		for _, member := range typ.Union.Members {
			if ok, reason := EqualityAvailable(member); !ok {
				return false, reason
			}
		}
		return true, ""
	case typ.NullableBase != nil:
		return EqualityAvailable(*typ.NullableBase)
	case typ.Element != nil:
		// Pointer identity equality never dereferences the pointee, so it
		// stays finite and always available.
		return true, ""
	case compilerTypes.IsInteger(typ), compilerTypes.IsFloat(typ),
		compilerTypes.Equal(typ, compilerTypes.Bool):
		return true, ""
	}
	facts, ok := typeFacts(typ)
	if !ok {
		// A type the registry does not describe has no equality contract:
		// function values, allocator handles, and every other non-scalar
		// identity without a record.
		return false, ""
	}
	switch facts.Comparison {
	case specdata.ComparisonAlways:
		return true, ""
	case specdata.ComparisonNever:
		return false, ""
	case specdata.ComparisonStructural:
		return structuralEqualityAvailable(typ)
	}
	return false, ""
}

// structuralEqualityAvailable recurses over the components of one value whose
// record declares the structural comparison form. Only Array, Slice, and List
// declare it today, and each stores one element component.
func structuralEqualityAvailable(typ compilerTypes.Type) (bool, string) {
	var element compilerTypes.Type
	switch {
	case typ.Array != nil:
		element = typ.Array.Element
	case typ.Slice != nil:
		element = typ.Slice.Element
	case typ.List != nil:
		element = typ.List.Element
	default:
		return false, ""
	}
	if ok, _ := EqualityAvailable(element); !ok {
		return false, "element type " + element.Name
	}
	return true, ""
}

// equalityUnavailableDiagnostic reports why equality is unavailable for one
// operand of the comparison. reason already names its own path - "member
// name" for an object/ADT field, "element type Name" for an Array/Slice/List
// - so the template never manufactures an empty description; it is empty
// only when typ itself is the direct cause, which the fallback below covers
// by kind instead.
func equalityUnavailableDiagnostic(typ compilerTypes.Type, reason string, token lexer.Token) *compilerTypes.Diagnostic {
	if reason != "" {
		diagnostic := typeErrorAt(token, "equality is unavailable because "+reason+" does not support ==")
		return &diagnostic
	}
	switch {
	case typ.Signature != nil:
		diagnostic := typeErrorAt(token, "function values are not equality-comparable")
		return &diagnostic
	case compilerTypes.IsHeap(typ):
		diagnostic := typeErrorAt(token, "allocator handles are not equality-comparable")
		return &diagnostic
	case typ.Dict != nil:
		diagnostic := typeErrorAt(token, "dictionary equality is not available in v1")
		return &diagnostic
	}
	diagnostic := typeErrorAt(token, "equality is unavailable for "+typ.Name)
	return &diagnostic
}

// orderingAvailable reports whether typ supports the ordering operators. The
// registry records the fact for each text form; no other compiler-owned type is
// ordered.
func orderingAvailable(typ compilerTypes.Type) bool {
	facts, ok := typeFacts(typ)
	return ok && facts.Ordered
}

// checkDeepComparison resolves ==, !=, and the ordering operators that the
// ordinary identical-scalar path does not own: lossless numeric widening,
// pointer identity, text comparison, and the recursive
// equality helpers for objects, ADTs, and sequences. It returns nil when
// the plain binary path should continue.
func checkDeepComparison(operator Operator, left, right checkedExpression, token lexer.Token, names *scope) *checkedExpression {
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
	if compilerTypes.IsText(left.typ) && compilerTypes.IsText(right.typ) {
		// Every text form is one family: two operands compare bytewise
		// whichever forms hold them, and capacity never participates.
		leftNode := expressionNode(left.source)
		rightNode := expressionNode(right.source)
		kind := DeepEqualityExpression
		if ordering {
			kind = StringCompareExpression
		}
		node := Expression{
			Kind:        kind,
			Left:        &leftNode,
			Right:       &rightNode,
			Operator:    operator,
			OperandType: left.typ,
			RightType:   right.typ,
			ResultType:  compilerTypes.Bool,
		}
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
	if typ.Object != nil && compilerTypes.IsForeignRecord(typ) {
		// A foreign record has no Hexal equality contract: C record
		// compatibility does not imply one.
		diagnostic := typeErrorAt(token, "equality is unavailable for foreign record "+typ.Name)
		return &checkedExpression{token: token, diagnostic: &diagnostic}
	}
	if ok, reason := EqualityAvailable(typ); !ok {
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
