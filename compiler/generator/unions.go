package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedUnionState records tagged unions in dependency order. Union C names
// are already compilation-local canonical metadata; this registry prevents
// duplicate helper declarations when aliases or nested uses repeat a union.
type generatedUnionState struct {
	names     map[*compilerTypes.UnionInfo]string
	order     []compilerTypes.Type
	widenings []unionWidening
}

type unionWidening struct {
	source      compilerTypes.Type
	destination compilerTypes.Type
	memberMap   []int
}

func (state *generatedUnionState) addWidening(node checker.Expression) {
	for _, existing := range state.widenings {
		if compilerTypes.Equal(existing.source, node.OperandType) && compilerTypes.Equal(existing.destination, node.ResultType) {
			return
		}
	}
	state.widenings = append(state.widenings, unionWidening{
		source:      node.OperandType,
		destination: node.ResultType,
		memberMap:   append([]int(nil), node.MemberMap...),
	})
}

func unionHelperName(ordinal int) string {
	return fmt.Sprintf("hex_internal_union_%d", ordinal)
}

func discoverGeneratedUnions(program checker.Program) (*generatedUnionState, error) {
	state := &generatedUnionState{names: make(map[*compilerTypes.UnionInfo]string)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.Union != nil {
				if _, seen := state.names[typ.Union]; seen {
					return nil
				}
				if typ.CName == "" {
					return unknownExpressionDiagnostic("union has no generated C name")
				}
				state.names[typ.Union] = typ.CName
				state.order = append(state.order, typ)
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.UnionWidenExpression {
				state.addWidening(node)
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}
func writeUnionDefinitions(result *strings.Builder, state *generatedUnionState) {
	if state == nil {
		return
	}
	for _, union := range state.order {
		writeUnionDefinition(result, union)
	}
	for _, widening := range state.widenings {
		writeUnionWidening(result, widening)
	}
	for _, union := range state.order {
		writeUnionTruthiness(result, union)
		// Equality helpers are emitted by writeEqualityDefinitions after
		// every struct definition, so recursive member compares resolve.
	}
}

// writeUnionDefinition emits one union's tag enum, payload union, and struct
// typedef.
func writeUnionDefinition(result *strings.Builder, union compilerTypes.Type) {
	name := union.CName
	fmt.Fprintf(result, "\ntypedef enum %s_tag {\n", name)
	for index := range union.Union.Members {
		fmt.Fprintf(result, "    %s_tag_member_%d,\n", name, index)
	}
	fmt.Fprintf(result, "} %s_tag;\n", name)
	fmt.Fprintf(result, "typedef union %s_payload {\n", name)
	for index, member := range union.Union.Members {
		if compilerTypes.IsNil(member) || compilerTypes.IsEoS(member) {
			// Nil and EoS are tag-only alternatives: Nil is a nullable
			// niche and EoS (RFC 0031) has no payload of its own.
			continue
		}
		if member.Signature != nil {
			fmt.Fprintf(result, "    %s;\n", funDeclaration(member, fmt.Sprintf("member_%d", index), true))
			continue
		}
		fmt.Fprintf(result, "    %s member_%d;\n", typeSpelling(member), index)
	}
	fmt.Fprintf(result, "} %s_payload;\n", name)
	fmt.Fprintf(result, "typedef struct %s {\n    %s_tag tag;\n    %s_payload payload;\n} %s;\n", name, name, name, name)
}

func unionTagName(union compilerTypes.Type, memberIndex int) string {
	return fmt.Sprintf("%s_tag_member_%d", union.CName, memberIndex)
}

func unionWidenHelperName(source, destination compilerTypes.Type) string {
	return "hex_internal_widen_" + source.CName + "_to_" + destination.CName
}

func writeUnionWidening(result *strings.Builder, widening unionWidening) {
	name := unionWidenHelperName(widening.source, widening.destination)
	fmt.Fprintf(result, "\nstatic %s %s(%s value) {\n    switch (value.tag) {\n", widening.destination.CName, name, widening.source.CName)
	sourceMembers := compilerTypes.UnionMembers(widening.source)
	destinationMembers := compilerTypes.UnionMembers(widening.destination)
	for sourceIndex, destinationIndex := range widening.memberMap {
		if sourceIndex >= len(sourceMembers) || destinationIndex < 0 || destinationIndex >= len(destinationMembers) {
			continue
		}
		fmt.Fprintf(result, "    case %s:\n", unionTagName(widening.source, sourceIndex))
		if compilerTypes.IsNil(sourceMembers[sourceIndex]) || compilerTypes.IsEoS(sourceMembers[sourceIndex]) {
			fmt.Fprintf(result, "        return (%s){ .tag = %s };\n", widening.destination.CName, unionTagName(widening.destination, destinationIndex))
			continue
		}
		fmt.Fprintf(result, "        return (%s){ .tag = %s, .payload.member_%d = value.payload.member_%d };\n", widening.destination.CName, unionTagName(widening.destination, destinationIndex), destinationIndex, sourceIndex)
	}
	fmt.Fprintf(result, "    default:\n        abort();\n    }\n}\n")
}

func unionSupportsEquality(union compilerTypes.Type) bool {
	for _, member := range compilerTypes.UnionMembers(union) {
		if !compilerTypes.IsNil(member) && !unionMemberEqualityAvailable(member) {
			return false
		}
	}
	return true
}

// unionMemberEqualityAvailable mirrors the checker's recursive equality
// eligibility for helper emission: the helper must only be generated when
// every member's compare is valid C.
func unionMemberEqualityAvailable(typ compilerTypes.Type) bool {
	switch {
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if !unionMemberEqualityAvailable(member.Type) {
				return false
			}
		}
		return true
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if !unionMemberEqualityAvailable(member.Type) {
					return false
				}
			}
		}
		return true
	case typ.Union != nil:
		return unionSupportsEquality(typ)
	case typ.NullableBase != nil:
		return unionMemberEqualityAvailable(*typ.NullableBase)
	case typ.Array != nil:
		return unionMemberEqualityAvailable(typ.Array.Element)
	case typ.View != nil:
		return unionMemberEqualityAvailable(typ.View.Element)
	case typ.List != nil:
		return unionMemberEqualityAvailable(typ.List.Element)
	case typ.Element != nil, compilerTypes.IsString(typ), compilerTypes.IsStrand(typ),
		compilerTypes.IsInteger(typ), compilerTypes.IsFloat(typ),
		compilerTypes.Equal(typ, compilerTypes.Bool):
		return true
	}
	return false
}

func writeUnionEquality(result *strings.Builder, union compilerTypes.Type) {
	name := union.CName + "_equal"
	fmt.Fprintf(result, "\nstatic bool %s(%s left, %s right) {\n    if (left.tag != right.tag) return false;\n    switch (left.tag) {\n", name, union.CName, union.CName)
	for index, member := range compilerTypes.UnionMembers(union) {
		fmt.Fprintf(result, "    case %s:\n", unionTagName(union, index))
		if compilerTypes.IsNil(member) {
			fmt.Fprintln(result, "        return true;")
			continue
		}
		if member.List != nil {
			// A List union member is a pointer-sized handle; the per-type
			// deep helper compares through the handle directly.
			fmt.Fprintf(result, "        if (!%s(left.payload.member_%d, right.payload.member_%d)) return false;\n", equalityHelperName(member), index, index)
			fmt.Fprintln(result, "        return true;")
			continue
		}
		writeEqualityComparisons(result, "left.payload.member_"+fmt.Sprint(index), "right.payload.member_"+fmt.Sprint(index), member, "        ")
		fmt.Fprintln(result, "        return true;")
	}
	fmt.Fprintln(result, "    default:")
	fmt.Fprintln(result, "        abort();")
	fmt.Fprintln(result, "    }")
	fmt.Fprintln(result, "}")
}

func writeUnionTruthiness(result *strings.Builder, union compilerTypes.Type) {
	fmt.Fprintf(result, "\nstatic bool %s_truthy(%s value) {\n    switch (value.tag) {\n", union.CName, union.CName)
	for index, member := range compilerTypes.UnionMembers(union) {
		fmt.Fprintf(result, "    case %s:\n", unionTagName(union, index))
		if compilerTypes.IsNil(member) {
			fmt.Fprintln(result, "        return false;")
		} else if compilerTypes.Equal(member, compilerTypes.Bool) {
			fmt.Fprintf(result, "        return value.payload.member_%d;\n", index)
		} else {
			fmt.Fprintln(result, "        return true;")
		}
	}
	fmt.Fprintln(result, "    default:")
	fmt.Fprintln(result, "        abort();")
	fmt.Fprintln(result, "    }")
	fmt.Fprintln(result, "}")
}

func unionMemberIndex(union, member compilerTypes.Type) int {
	for index, candidate := range compilerTypes.UnionMembers(union) {
		if compilerTypes.Equal(candidate, member) {
			return index
		}
	}
	return -1
}

func validateUnionInjection(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsUnion(node.ResultType) || !supportedGeneratedTypeWithState(node.ResultType, state) || node.MemberIndex < 0 || node.MemberIndex >= len(compilerTypes.UnionMembers(node.ResultType)) {
		return unknownExpressionDiagnostic("union injection has invalid checked metadata")
	}
	if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
		return unknownExpressionDiagnostic("union injection result does not match its expected type")
	}
	member := compilerTypes.UnionMembers(node.ResultType)[node.MemberIndex]
	if !compilerTypes.Assignable(member, node.OperandType) {
		return unknownExpressionDiagnostic("union injection member does not match its checked source")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateUnionWiden(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsUnion(node.OperandType) || !compilerTypes.IsUnion(node.ResultType) || !supportedGeneratedTypeWithState(node.OperandType, state) || !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic("union widening has invalid checked metadata")
	}
	if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
		return unknownExpressionDiagnostic("union widening result does not match its expected type")
	}
	sourceMembers := compilerTypes.UnionMembers(node.OperandType)
	destinationMembers := compilerTypes.UnionMembers(node.ResultType)
	if len(node.MemberMap) != len(sourceMembers) {
		return unknownExpressionDiagnostic("union widening map does not match its source members")
	}
	for index, destinationIndex := range node.MemberMap {
		if destinationIndex < 0 || destinationIndex >= len(destinationMembers) || !compilerTypes.Assignable(destinationMembers[destinationIndex], sourceMembers[index]) {
			return unknownExpressionDiagnostic("union widening map contains an invalid member conversion")
		}
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateUnionTest(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsUnion(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.IsNil(node.TestType) || compilerTypes.IsUnion(node.TestType) {
		return unknownExpressionDiagnostic("union test has invalid checked metadata")
	}
	if hasExpected && !compilerTypes.Equal(expected, compilerTypes.Bool) {
		return unknownExpressionDiagnostic("union test result does not match its expected type")
	}
	if unionMemberIndex(node.OperandType, node.TestType) != node.MemberIndex {
		return unknownExpressionDiagnostic("union test member does not match its checked union")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateUnionPayload(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsUnion(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) || node.MemberIndex < 0 || node.MemberIndex >= len(compilerTypes.UnionMembers(node.OperandType)) {
		return unknownExpressionDiagnostic("union payload has invalid checked metadata")
	}
	member := compilerTypes.UnionMembers(node.OperandType)[node.MemberIndex]
	if !compilerTypes.Equal(node.ResultType, member) || hasExpected && !compilerTypes.Equal(expected, member) {
		return unknownExpressionDiagnostic("union payload result does not match its checked member")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateUnionEquality(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Left == nil || node.Right == nil || !compilerTypes.IsUnion(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || hasExpected && !compilerTypes.Equal(expected, compilerTypes.Bool) {
		return unknownExpressionDiagnostic("union equality has invalid checked metadata")
	}
	for _, member := range compilerTypes.UnionMembers(node.OperandType) {
		if !compilerTypes.IsNil(member) && !unionMemberEqualityAvailable(member) {
			return unknownExpressionDiagnostic("union equality contains an unsupported member")
		}
	}
	if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
		return err
	}
	return validateExpressionChildWithState(node.Right, node.OperandType, state)
}

func unionTruthinessCall(typ compilerTypes.Type, rendered string) string {
	return typ.CName + "_truthy(" + rendered + ")"
}

func unionEqualityCall(typ compilerTypes.Type, left, right string) string {
	return typ.CName + "_equal(" + left + ", " + right + ")"
}

func unionWidenCall(source, destination compilerTypes.Type, rendered string) string {
	return "hex_internal_widen_" + source.CName + "_to_" + destination.CName + "(" + rendered + ")"
}

func renderUnionInjection(node checker.Expression, state *expressionValidation) (string, error) {
	child, err := renderExpressionExpectedWithState(*node.Operand, node.OperandType, true, state)
	if err != nil {
		return "", err
	}
	if compilerTypes.IsNullable(node.ResultType) {
		return child, nil
	}
	member := compilerTypes.UnionMembers(node.ResultType)[node.MemberIndex]
	tag := unionTagName(node.ResultType, node.MemberIndex)
	if compilerTypes.IsNil(member) || compilerTypes.IsEoS(member) {
		return fmt.Sprintf("(%s){ .tag = %s }", node.ResultType.CName, tag), nil
	}
	return fmt.Sprintf("(%s){ .tag = %s, .payload.member_%d = %s }", node.ResultType.CName, tag, node.MemberIndex, child), nil
}

func renderUnionWiden(node checker.Expression, state *expressionValidation) (string, error) {
	child, err := renderExpressionExpectedWithState(*node.Operand, node.OperandType, true, state)
	if err != nil {
		return "", err
	}
	return unionWidenCall(node.OperandType, node.ResultType, child), nil
}

func renderUnionTest(node checker.Expression, state *expressionValidation) (string, error) {
	child, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if err != nil {
		return "", err
	}
	if !atomic {
		child = "(" + child + ")"
	}
	representation, index, ok := remapUnionMember(node.Operand, node.OperandType, node.MemberIndex, state)
	if !ok {
		representation, index = node.OperandType, node.MemberIndex
	}
	return child + ".tag == " + unionTagName(representation, index), nil
}

func renderUnionPayload(node checker.Expression, state *expressionValidation) (string, error) {
	if compilerTypes.IsNil(compilerTypes.UnionMembers(node.OperandType)[node.MemberIndex]) {
		return "nullptr", nil
	}
	child, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if err != nil {
		return "", err
	}
	if !atomic {
		child = "(" + child + ")"
	}
	_, index, ok := remapUnionMember(node.Operand, node.OperandType, node.MemberIndex, state)
	if !ok {
		index = node.MemberIndex
	}
	return child + ".payload.member_" + fmt.Sprint(index), nil
}

// remapUnionMember maps a member index in the operand's (possibly narrowed)
// union type to the index of the same member in the binding's declared union
// representation. The checker narrows a union-typed binding to a reduced
// union after an `is` or Nil test, but the generated C value keeps the
// declared union's struct, so tags and payloads must address the declared
// union. Returns false when the operand is not a union binding or the member
// does not exist in the declared union; the caller then falls back to the
// operand's own type.
func remapUnionMember(operand *checker.Expression, operandType compilerTypes.Type, memberIndex int, state *expressionValidation) (compilerTypes.Type, int, bool) {
	if operand == nil || operand.Kind != checker.VariableExpression || operand.Binding == 0 {
		return compilerTypes.Type{}, 0, false
	}
	binding, ok := state.bindings[operand.Binding]
	if !ok || binding.typ.Union == nil {
		return compilerTypes.Type{}, 0, false
	}
	if compilerTypes.Equal(binding.typ, operandType) {
		return binding.typ, memberIndex, true
	}
	members := compilerTypes.UnionMembers(operandType)
	if memberIndex < 0 || memberIndex >= len(members) {
		return compilerTypes.Type{}, 0, false
	}
	for index, member := range compilerTypes.UnionMembers(binding.typ) {
		if compilerTypes.Equal(member, members[memberIndex]) {
			return binding.typ, index, true
		}
	}
	return compilerTypes.Type{}, 0, false
}

func renderUnionEquality(node checker.Expression, state *expressionValidation) (string, error) {
	left, err := renderExpressionExpectedWithState(*node.Left, node.OperandType, true, state)
	if err != nil {
		return "", err
	}
	right, err := renderExpressionExpectedWithState(*node.Right, node.OperandType, true, state)
	if err != nil {
		return "", err
	}
	result := unionEqualityCall(node.OperandType, left, right)
	if node.Operator == checker.NotEqualOperator {
		return "(!" + result + ")", nil
	}
	return result, nil
}
