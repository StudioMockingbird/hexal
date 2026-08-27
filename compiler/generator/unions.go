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

func (state *generatedUnionState) addWideningTypes(source, destination compilerTypes.Type) {
	for _, existing := range state.widenings {
		if compilerTypes.Equal(existing.source, source) && compilerTypes.Equal(existing.destination, destination) {
			return
		}
	}
	sourceMembers := compilerTypes.UnionMembers(source)
	destinationMembers := compilerTypes.UnionMembers(destination)
	memberMap := make([]int, 0, sourceMembers.Len())
	for index := 0; index < sourceMembers.Len(); index++ {
		sourceMember, _ := sourceMembers.At(index)
		destinationIndex := -1
		for candidateIndex := 0; candidateIndex < destinationMembers.Len(); candidateIndex++ {
			if destinationMember, _ := destinationMembers.At(candidateIndex); compilerTypes.Equal(destinationMember, sourceMember) || compilerTypes.Assignable(destinationMember, sourceMember) {
				destinationIndex = candidateIndex
				break
			}
		}
		memberMap = append(memberMap, destinationIndex)
	}
	state.widenings = append(state.widenings, unionWidening{
		source:      source,
		destination: destination,
		memberMap:   memberMap,
	})
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
			if node.Kind == checker.CollectionMethodCallExpression && node.Name == "find" && node.Element.Union != nil && node.ResultType.Union != nil {
				state.addWideningTypes(node.Element, node.ResultType)
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}
func writeUnionDefinitions(result *strings.Builder, state *generatedUnionState, tags *tagRegistry) {
	if state == nil {
		return
	}
	for _, union := range state.order {
		writeUnionDefinition(result, union, tags)
	}
	for _, widening := range state.widenings {
		writeUnionWidening(result, widening, tags)
	}
	for _, union := range state.order {
		writeUnionTruthiness(result, union, tags)
		// Equality helpers are emitted by writeEqualityDefinitions after
		// every struct definition, so recursive member compares resolve.
	}
}

// writeUnionDefinition emits one union's wrapper struct: the shared hex_tag
// discriminant and an unnamed payload-union type. Nil and EoS are tag-only
// alternatives and spell no payload field.
func writeUnionDefinition(result *strings.Builder, union compilerTypes.Type, tags *tagRegistry) {
	name := union.CName
	fmt.Fprintf(result, "\ntypedef struct %s {\n    hex_tag tag;\n    union {\n", name)
	for _, member := range union.Union.Members {
		if compilerTypes.IsNil(member) || compilerTypes.IsEoS(member) {
			continue
		}
		if member.Signature != nil {
			fmt.Fprintf(result, "        %s;\n", funDeclaration(member, tags.unionPayloadField(member), true))
			continue
		}
		fmt.Fprintf(result, "        %s %s;\n", typeSpelling(member), tags.unionPayloadField(member))
	}
	fmt.Fprintf(result, "    } payload;\n} %s;\n", name)
}

func unionWidenHelperName(source, destination compilerTypes.Type) string {
	return "hex_internal_widen_" + source.CName + "_to_" + destination.CName
}

func writeUnionWidening(result *strings.Builder, widening unionWidening, tags *tagRegistry) {
	name := unionWidenHelperName(widening.source, widening.destination)
	fmt.Fprintf(result, "\nstatic %s %s(%s value) {\n    switch (value.tag) {\n", widening.destination.CName, name, widening.source.CName)
	sourceMembers := compilerTypes.UnionMembers(widening.source)
	destinationMembers := compilerTypes.UnionMembers(widening.destination)
	for sourceIndex, destinationIndex := range widening.memberMap {
		if sourceIndex >= sourceMembers.Len() || destinationIndex < 0 || destinationIndex >= destinationMembers.Len() {
			continue
		}
		sourceMember, _ := sourceMembers.At(sourceIndex)
		destinationMember, _ := destinationMembers.At(destinationIndex)
		fmt.Fprintf(result, "    case %s:\n", tags.unionMemberTag(sourceMember))
		if compilerTypes.IsNil(sourceMember) || compilerTypes.IsEoS(sourceMember) {
			fmt.Fprintf(result, "        return (%s){ .tag = value.tag };\n", widening.destination.CName)
			continue
		}
		fmt.Fprintf(result, "        return (%s){ .tag = value.tag, .payload.%s = value.payload.%s };\n", widening.destination.CName, tags.unionPayloadField(destinationMember), tags.unionPayloadField(sourceMember))
	}
	fmt.Fprintf(result, "    default:\n        abort();\n    }\n}\n")
}

func unionSupportsEquality(union compilerTypes.Type) bool {
	members := compilerTypes.UnionMembers(union)
	for index := 0; index < members.Len(); index++ {
		if member, _ := members.At(index); !compilerTypes.IsNil(member) && !unionMemberEqualityAvailable(member) {
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

func writeUnionEquality(result *strings.Builder, union compilerTypes.Type, tags *tagRegistry) {
	name := union.CName + "_equal"
	fmt.Fprintf(result, "\nstatic bool %s(%s left, %s right) {\n    if (left.tag != right.tag) return false;\n    switch (left.tag) {\n", name, union.CName, union.CName)
	members := compilerTypes.UnionMembers(union)
	for index := 0; index < members.Len(); index++ {
		member, _ := members.At(index)
		field := tags.unionPayloadField(member)
		fmt.Fprintf(result, "    case %s:\n", tags.unionMemberTag(member))
		if compilerTypes.IsNil(member) {
			fmt.Fprintln(result, "        return true;")
			continue
		}
		if member.List != nil {
			// A List union member is a pointer-sized handle; the per-type
			// deep helper compares through the handle directly.
			fmt.Fprintf(result, "        if (!%s(left.payload.%s, right.payload.%s)) return false;\n", equalityHelperName(member), field, field)
			fmt.Fprintln(result, "        return true;")
			continue
		}
		writeEqualityComparisons(result, "left.payload."+field, "right.payload."+field, member, "        ", tags)
		fmt.Fprintln(result, "        return true;")
	}
	fmt.Fprintln(result, "    default:")
	fmt.Fprintln(result, "        abort();")
	fmt.Fprintln(result, "    }")
	fmt.Fprintln(result, "}")
}

func writeUnionTruthiness(result *strings.Builder, union compilerTypes.Type, tags *tagRegistry) {
	fmt.Fprintf(result, "\nstatic bool %s_truthy(%s value) {\n    switch (value.tag) {\n", union.CName, union.CName)
	members := compilerTypes.UnionMembers(union)
	for index := 0; index < members.Len(); index++ {
		member, _ := members.At(index)
		fmt.Fprintf(result, "    case %s:\n", tags.unionMemberTag(member))
		if compilerTypes.IsNil(member) {
			fmt.Fprintln(result, "        return false;")
		} else if compilerTypes.Equal(member, compilerTypes.Bool) {
			fmt.Fprintf(result, "        return value.payload.%s;\n", tags.unionPayloadField(member))
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
	members := compilerTypes.UnionMembers(union)
	for index := 0; index < members.Len(); index++ {
		if candidate, _ := members.At(index); compilerTypes.Equal(candidate, member) {
			return index
		}
	}
	return -1
}

func validateUnionInjection(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	resultMembers := compilerTypes.UnionMembers(node.ResultType)
	if node.Operand == nil || !compilerTypes.IsUnion(node.ResultType) || !supportedGeneratedTypeWithState(node.ResultType, state) || node.MemberIndex < 0 || node.MemberIndex >= resultMembers.Len() {
		return unknownExpressionDiagnostic("union injection has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("union injection result does not match its expected type")
	}
	member, _ := resultMembers.At(node.MemberIndex)
	if !compilerTypes.Assignable(member, node.OperandType) {
		return unknownExpressionDiagnostic("union injection member does not match its checked source")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateUnionWiden(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsUnion(node.OperandType) || !compilerTypes.IsUnion(node.ResultType) || !supportedGeneratedTypeWithState(node.OperandType, state) || !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic("union widening has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("union widening result does not match its expected type")
	}
	sourceMembers := compilerTypes.UnionMembers(node.OperandType)
	destinationMembers := compilerTypes.UnionMembers(node.ResultType)
	if len(node.MemberMap) != sourceMembers.Len() {
		return unknownExpressionDiagnostic("union widening map does not match its source members")
	}
	for index, destinationIndex := range node.MemberMap {
		if destinationIndex == -1 {
			// A physical-representation widen (source is a flow-narrowed
			// binding's real, pre-narrowing storage type) legitimately maps
			// a member with no destination counterpart when the narrowing
			// that produced this binding already proved it unreachable; the
			// generated switch's own default: abort() covers it.
			continue
		}
		sourceMember, _ := sourceMembers.At(index)
		if destinationIndex < -1 || destinationIndex >= destinationMembers.Len() {
			return unknownExpressionDiagnostic("union widening map contains an invalid member conversion")
		}
		destinationMember, _ := destinationMembers.At(destinationIndex)
		if !compilerTypes.Assignable(destinationMember, sourceMember) {
			return unknownExpressionDiagnostic("union widening map contains an invalid member conversion")
		}
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateUnionTest(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsUnion(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.IsNil(node.TestType) || compilerTypes.IsUnion(node.TestType) {
		return unknownExpressionDiagnostic("union test has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, compilerTypes.Bool) {
		return unknownExpressionDiagnostic("union test result does not match its expected type")
	}
	if unionMemberIndex(node.OperandType, node.TestType) != node.MemberIndex {
		return unknownExpressionDiagnostic("union test member does not match its checked union")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateUnionPayload(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	operandMembers := compilerTypes.UnionMembers(node.OperandType)
	if node.Operand == nil || !compilerTypes.IsUnion(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) || node.MemberIndex < 0 || node.MemberIndex >= operandMembers.Len() {
		return unknownExpressionDiagnostic("union payload has invalid checked metadata")
	}
	member, _ := operandMembers.At(node.MemberIndex)
	if !compilerTypes.Equal(node.ResultType, member) || expected != nil && !compilerTypes.Equal(*expected, member) {
		return unknownExpressionDiagnostic("union payload result does not match its checked member")
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

func validateUnionEquality(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Left == nil || node.Right == nil || !compilerTypes.IsUnion(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || expected != nil && !compilerTypes.Equal(*expected, compilerTypes.Bool) {
		return unknownExpressionDiagnostic("union equality has invalid checked metadata")
	}
	operandMembers := compilerTypes.UnionMembers(node.OperandType)
	for index := 0; index < operandMembers.Len(); index++ {
		if member, _ := operandMembers.At(index); !compilerTypes.IsNil(member) && !unionMemberEqualityAvailable(member) {
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
	child, err := renderExpressionExpectedWithState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	if compilerTypes.IsNullable(node.ResultType) {
		return child, nil
	}
	injectionMembers := compilerTypes.UnionMembers(node.ResultType)
	member, _ := injectionMembers.At(node.MemberIndex)
	tag := state.tags.unionMemberTag(member)
	if compilerTypes.IsNil(member) || compilerTypes.IsEoS(member) {
		return fmt.Sprintf("(%s){ .tag = %s }", node.ResultType.CName, tag), nil
	}
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = %s }", node.ResultType.CName, tag, state.tags.unionPayloadField(member), child), nil
}

func renderUnionWiden(node checker.Expression, state *expressionValidation) (string, error) {
	child, err := renderExpressionExpectedWithState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	return unionWidenCall(node.OperandType, node.ResultType, child), nil
}

func renderUnionTest(node checker.Expression, state *expressionValidation) (string, error) {
	child, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
	testMember, _ := compilerTypes.UnionMembers(representation).At(index)
	return child + ".tag == " + state.tags.unionMemberTag(testMember), nil
}

func renderUnionPayload(node checker.Expression, state *expressionValidation) (string, error) {
	payloadOperandMembers := compilerTypes.UnionMembers(node.OperandType)
	if nilCheckMember, _ := payloadOperandMembers.At(node.MemberIndex); compilerTypes.IsNil(nilCheckMember) {
		return "nullptr", nil
	}
	child, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
	payloadRepresentationMember, _ := compilerTypes.UnionMembers(representation).At(index)
	return child + ".payload." + state.tags.unionPayloadField(payloadRepresentationMember), nil
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
	if memberIndex < 0 || memberIndex >= members.Len() {
		return compilerTypes.Type{}, 0, false
	}
	narrowedMember, _ := members.At(memberIndex)
	bindingMembers := compilerTypes.UnionMembers(binding.typ)
	for index := 0; index < bindingMembers.Len(); index++ {
		if member, _ := bindingMembers.At(index); compilerTypes.Equal(member, narrowedMember) {
			return binding.typ, index, true
		}
	}
	return compilerTypes.Type{}, 0, false
}

func renderUnionEquality(node checker.Expression, state *expressionValidation) (string, error) {
	left, err := renderHoistedExpressionExpected(node.Left, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	right, err := renderHoistedExpressionExpected(node.Right, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	result := unionEqualityCall(node.OperandType, left, right)
	if node.Operator == checker.NotEqualOperator {
		return "(!" + result + ")", nil
	}
	return result, nil
}
