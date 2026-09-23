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
// truthy separately records which of those unions are actually evaluated in
// a boolean context (an if/while condition, or an operand of not/and/or):
// the wrapper struct stays type-driven (declared, needed regardless), but
// its _truthy helper is operation-driven, matching the exact call sites
// truthinessExpression itself renders from.
type generatedUnionState struct {
	names     map[*compilerTypes.UnionInfo]string
	order     []compilerTypes.Type
	widenings []unionWidening
	truthy    map[*compilerTypes.UnionInfo]bool
}

// markTruthy records that typ was evaluated in a boolean context, if it is a
// union; every other type is ignored, since truthiness for non-union values
// is either a raw Bool or an unconditional constant with no helper to emit.
func (state *generatedUnionState) markTruthy(typ compilerTypes.Type) {
	if typ.Union != nil {
		state.truthy[typ.Union] = true
	}
}

// unionConditionsNeedingTruthy visits the checked statement forms that
// evaluate a condition's truthiness -- if/elseif and while -- marking each
// condition's own type. Logical not/and/or are handled in
// discoverGeneratedUnions's Expression callback instead, since they are
// checked expression nodes rather than statements.
func (state *generatedUnionState) markStatementTruthy(statement checker.Statement) {
	switch typed := statement.(type) {
	case checker.IfStatement:
		state.markTruthy(typed.Condition.Type)
		for _, branch := range typed.ElseIf {
			state.markTruthy(branch.Condition.Type)
		}
	case checker.WhileStatement:
		state.markTruthy(typed.Condition.Type)
	}
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
	state := &generatedUnionState{names: make(map[*compilerTypes.UnionInfo]string), truthy: make(map[*compilerTypes.UnionInfo]bool)}
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
		Statement: func(statement checker.Statement) error {
			state.markStatementTruthy(statement)
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.UnionWidenExpression {
				state.addWidening(node)
			}
			if node.Kind == checker.CollectionMethodCallExpression && node.Name == "find" && node.Element.Union != nil && node.ResultType.Union != nil {
				state.addWideningTypes(node.Element, node.ResultType)
			}
			if node.Kind == checker.UnaryOperationExpression && node.Operator == checker.LogicalNotOperator {
				state.markTruthy(node.OperandType)
				if node.Operand != nil {
					state.markTruthy(node.Operand.ResultType)
				}
			}
			if node.Kind == checker.BinaryOperationExpression && (node.Operator == checker.LogicalAndOperator || node.Operator == checker.LogicalOrOperator) {
				state.markTruthy(node.OperandType)
				if node.Left != nil {
					state.markTruthy(node.Left.ResultType)
				}
				if node.Right != nil {
					state.markTruthy(node.Right.ResultType)
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}

// nominalForwardModel carries one nominal type's forward typedef name, shared
// by union and ADT forward declarations.
type nominalForwardModel struct {
	Name string
}

// unionBodyModel carries one union's struct body. Each member's declarator
// arrives decided: a function-pointer declaration or a value declaration.
type unionBodyModel struct {
	Name    string
	Members []unionPayloadModel
}

// unionPayloadModel is one payload-union member's decided declarator text.
type unionPayloadModel struct {
	Declaration string
}

// unionWidenModel carries one widening helper's decided names and per-tag
// cases. Payload is the decided payload assignment, empty for a tag-only
// alternative.
type unionWidenModel struct {
	Destination string
	Name        string
	Source      string
	Cases       []unionWidenCaseModel
}

// unionWidenCaseModel is one widening case's tag and payload assignment.
type unionWidenCaseModel struct {
	Tag     string
	Payload string
}

// unionEqualModel carries one union equality helper's decided names and
// per-tag cases. Comparisons is the decided comparison text preceding the
// shared return.
type unionEqualModel struct {
	Name  string
	CName string
	Cases []unionEqualCaseModel
}

// unionEqualCaseModel is one equality case's tag and comparison text.
type unionEqualCaseModel struct {
	Tag         string
	Comparisons string
}

// unionTruthyModel carries one truthiness helper's decided cases. Return is
// the full decided return statement for the case.
type unionTruthyModel struct {
	CName string
	Cases []unionTruthyCaseModel
}

// unionTruthyCaseModel is one truthiness case's tag and return statement.
type unionTruthyCaseModel struct {
	Tag    string
	Return string
}

// writeUnionForwardDeclarations emits `typedef struct CName CName;` for every
// discovered union, ahead of every full body, mirroring
// writeAdtForwardDeclarations.
func writeUnionForwardDeclarations(result *strings.Builder, state *generatedUnionState) error {
	if state == nil {
		return nil
	}
	for _, union := range state.order {
		if compilerTypes.IsBuiltinUnion(union) {
			continue
		}
		if err := renderInto(result, "module.h", "nominal_forward", nominalForwardModel{Name: union.CName}); err != nil {
			return err
		}
	}
	return nil
}

// writeUnionDefinitions emits every union's widening and truthiness helpers.
// Full struct bodies are emitted by the dependency-ordered driver in
// emission.go, through writeOneUnionBody; by the time this runs, every
// union's own body already exists.
func writeUnionDefinitions(result *strings.Builder, state *generatedUnionState, tags *tagRegistry) error {
	if state == nil {
		return nil
	}
	for _, widening := range state.widenings {
		if compilerTypes.IsNullable(widening.source) && compilerTypes.IsNullable(widening.destination) {
			// A niche-to-niche widening is the identity (see unionWidenCall),
			// so it emits no helper.
			continue
		}
		if err := writeUnionWidening(result, widening, tags); err != nil {
			return err
		}
	}
	for _, union := range state.order {
		if state.truthy[union.Union] {
			if err := writeUnionTruthiness(result, union, tags); err != nil {
				return err
			}
		}
		// Equality helpers are emitted by writeEqualityDefinitions after
		// every struct definition, so recursive member compares resolve.
	}
	return nil
}

// writeOneUnionBody emits one union's full struct body: the shared hex_tag
// discriminant and an unnamed payload-union type. Nil and EoS are tag-only
// alternatives and spell no payload field. Its own forward typedef must
// already be in scope; a payload member naming another nominal type by value
// additionally needs that type's own full body already written, which the
// dependency-ordered driver in emission.go guarantees before calling this.
func writeOneUnionBody(result *strings.Builder, union compilerTypes.Type, tags *tagRegistry) error {
	model := unionBodyModel{Name: union.CName}
	for _, member := range union.Union.Members {
		if compilerTypes.IsNil(member) || compilerTypes.IsEoS(member) {
			continue
		}
		declaration := typeSpelling(member) + " " + tags.unionPayloadField(member)
		if member.Signature != nil {
			declaration = funDeclaration(member, tags.unionPayloadField(member), true)
		}
		model.Members = append(model.Members, unionPayloadModel{Declaration: declaration})
	}
	return renderInto(result, "module.h", "union_body", model)
}

func unionWidenHelperName(source, destination compilerTypes.Type) string {
	return "hex_internal_widen_" + source.CName + "_to_" + destination.CName
}

func writeUnionWidening(result *strings.Builder, widening unionWidening, tags *tagRegistry) error {
	sourceMembers := compilerTypes.UnionMembers(widening.source)
	destinationMembers := compilerTypes.UnionMembers(widening.destination)
	model := unionWidenModel{
		Destination: widening.destination.CName,
		Name:        unionWidenHelperName(widening.source, widening.destination),
		Source:      widening.source.CName,
	}
	for sourceIndex, destinationIndex := range widening.memberMap {
		if sourceIndex >= sourceMembers.Len() || destinationIndex < 0 || destinationIndex >= destinationMembers.Len() {
			continue
		}
		sourceMember, _ := sourceMembers.At(sourceIndex)
		destinationMember, _ := destinationMembers.At(destinationIndex)
		caseModel := unionWidenCaseModel{Tag: tags.unionMemberTag(sourceMember)}
		if !compilerTypes.IsNil(sourceMember) && !compilerTypes.IsEoS(sourceMember) {
			caseModel.Payload = ".payload." + tags.unionPayloadField(destinationMember) + " = value.payload." + tags.unionPayloadField(sourceMember)
		}
		model.Cases = append(model.Cases, caseModel)
	}
	return renderInto(result, "module.h", "union_widen", model)
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

// unionMemberEqualityAvailable reports whether every member's comparison is
// valid C, so the helper is only generated when it is. It delegates to the
// checker's rule instead of restating the recursion, so the eligibility fact
// has one owner and cannot drift from the answer the checker accepted.
func unionMemberEqualityAvailable(typ compilerTypes.Type) bool {
	available, _ := checker.EqualityAvailable(typ)
	return available
}

func writeUnionEquality(result *strings.Builder, union compilerTypes.Type, tags *tagRegistry) error {
	members := compilerTypes.UnionMembers(union)
	model := unionEqualModel{Name: union.CName + "_equal", CName: union.CName}
	for index := 0; index < members.Len(); index++ {
		member, _ := members.At(index)
		field := tags.unionPayloadField(member)
		caseModel := unionEqualCaseModel{Tag: tags.unionMemberTag(member)}
		switch {
		case compilerTypes.IsNil(member):
			// A tag-only alternative carries no payload; the tag equality
			// checked in the header is the whole comparison.
		case member.List != nil:
			// A List union member is a pointer-sized handle; the per-type
			// deep helper compares through the handle directly.
			caseModel.Comparisons = fmt.Sprintf("        if (!%s(left.payload.%s, right.payload.%s)) return false;\n", equalityHelperName(member), field, field)
		default:
			var comparisons strings.Builder
			if err := writeEqualityComparisons(&comparisons, "left.payload."+field, "right.payload."+field, member, "        ", tags); err != nil {
				return err
			}
			caseModel.Comparisons = comparisons.String()
		}
		model.Cases = append(model.Cases, caseModel)
	}
	return renderInto(result, "module.h", "union_equal", model)
}

func writeUnionTruthiness(result *strings.Builder, union compilerTypes.Type, tags *tagRegistry) error {
	members := compilerTypes.UnionMembers(union)
	model := unionTruthyModel{CName: union.CName}
	for index := 0; index < members.Len(); index++ {
		member, _ := members.At(index)
		caseModel := unionTruthyCaseModel{Tag: tags.unionMemberTag(member), Return: "        return true;"}
		if compilerTypes.IsNil(member) {
			caseModel.Return = "        return false;"
		} else if compilerTypes.Equal(member, compilerTypes.Bool) {
			caseModel.Return = "        return value.payload." + tags.unionPayloadField(member) + ";"
		}
		model.Cases = append(model.Cases, caseModel)
	}
	return renderInto(result, "module.h", "union_truthy", model)
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
	if compilerTypes.IsNullable(source) && compilerTypes.IsNullable(destination) {
		// Both sides are the pointer-null niche: the union value is the
		// pointer itself, and a source member already converts implicitly to
		// the destination's wider pointer, so widening is the identity and no
		// helper (whose name would embed a pointer CName) is emitted.
		return rendered
	}
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
	field := "hex_m_unvalidated"
	if state != nil && state.tags != nil {
		field = state.tags.unionPayloadField(payloadRepresentationMember)
	}
	return child + ".payload." + field, nil
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
