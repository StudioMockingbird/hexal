package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// sequenceSlot is one sub-position of a compound C expression (a call
// argument, a binary operand, a receiver, an aggregate element) that needs
// evaluation-order hoisting relative to its siblings. key is the checked
// tree's own pointer or slice-indexed field for that sub-position, stable
// across every copy of the enclosing checked statement made along the way
// from the hoisting pass to the eventual render call (see hoistedSequencing's
// doc comment on expressionValidation).
type sequenceSlot struct {
	key    *checker.Expression
	render func(state *expressionValidation) (string, compilerTypes.Type, error)
}

// pureAccessorMethodName reports whether a built-in method call's Name is a
// side-effect-free read on an expression kind that also carries mutating or
// allocating names: CollectionMethodCallExpression covers push/clear/pop
// (mutate) alongside length/get/contains (read); StringMethodCallExpression
// covers concat/to_string/free (allocate or deallocate) alongside
// length/bytes/slice/rune_cursor (read); RuneCursorMethodCallExpression's
// has_next reads state that next() advances; ChannelMethodCallExpression's
// length/capacity/is_closed read alongside send/receive/close/free.
func pureAccessorMethodName(kind checker.ExpressionKind, name string) bool {
	switch kind {
	case checker.CollectionMethodCallExpression:
		switch name {
		case "length", "get", "contains", "find":
			return true
		}
	case checker.StringMethodCallExpression:
		switch name {
		case "length", "bytes", "slice", "rune_cursor":
			return true
		}
	case checker.RuneCursorMethodCallExpression:
		return name == "has_next"
	case checker.ChannelMethodCallExpression:
		switch name {
		case "length", "capacity", "is_closed":
			return true
		}
	}
	return false
}

// expressionMayObserve reports whether evaluating node could produce an
// effect that must stay ordered relative to a sibling sharing its C
// compound position: a call, a mutation, an allocation, or anything a
// dedicated earlier hoist (try, spawn, Dict.find) already promoted to its
// own prologue. It walks every reachable child unconditionally, including a
// match arm or a short-circuited operand, because the question here is only
// "could this subtree ever observe", never "does this subtree always run":
// the separate hoisting walk (hoistSequencingInExpression) is the one that
// respects conditional evaluation and must not lift an effect out from
// behind a guard.
func expressionMayObserve(node *checker.Expression, state *expressionValidation) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case checker.CollectionMethodCallExpression, checker.StringMethodCallExpression,
		checker.RuneCursorMethodCallExpression, checker.ChannelMethodCallExpression:
		if !pureAccessorMethodName(node.Kind, node.Name) {
			return true
		}
	case checker.CallExpression, checker.MethodCallExpression,
		checker.StringFromBytesExpression, checker.StringFromRunesExpression,
		checker.ListNewExpression, checker.DictNewExpression, checker.TryExpression, checker.PrintExpression,
		checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression,
		checker.ChannelConstructorExpression,
		checker.MutexConstructorExpression, checker.MutexMethodCallExpression,
		checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression,
		checker.HeapAllocateExpression, checker.HeapFreeExpression, checker.VolatileWriteExpression,
		checker.StreamConstructorExpression, checker.StreamMethodCallExpression,
		checker.MatchExpression:
		return true
	}
	if expressionMayObserve(node.Operand, state) || expressionMayObserve(node.Left, state) || expressionMayObserve(node.Right, state) {
		return true
	}
	for index := range node.Arguments {
		if expressionMayObserve(&node.Arguments[index].Node, state) {
			return true
		}
	}
	if node.Object != nil {
		for index := range node.Object.Initializers {
			if expressionMayObserve(&node.Object.Initializers[index].Source.Node, state) {
				return true
			}
		}
	}
	return false
}

// hoistSequenceSlots hoists a group of sibling sub-positions that share one
// C compound expression, in written order, when at least two are present and
// at least one may observe an effect. Every slot is hoisted, not only the
// effectful ones: the RFC's own aliasing example, f(mutateX(), x), requires
// even a pure-looking sibling to evaluate at its written sequence point,
// since mutateX() may alias x.
//
// Recursing into a slot's own nested structure happens one slot at a time,
// interleaved with that same slot's own hoist, rather than as a separate
// pass over every slot first: a() + f(b(), c()) must emit a()'s temporary
// before f's own b()/c() temporaries, even though a() has no nested
// structure of its own and f does, because a() is written first. Resolving
// every slot's nested structure up front before hoisting any of them would
// reverse that when an earlier, shallower slot has nothing to recurse into
// but a later, deeper slot does.
func hoistSequenceSlots(slots []sequenceSlot, body *strings.Builder, state *expressionValidation, indent string) error {
	if len(slots) == 0 {
		return nil
	}
	if len(slots) == 1 {
		return hoistSequencingInExpression(slots[0].key, body, state, indent)
	}
	observed := false
	for _, slot := range slots {
		if expressionMayObserve(slot.key, state) {
			observed = true
			break
		}
	}
	if !observed {
		return nil
	}
	if state.hoistedSequencing == nil {
		state.hoistedSequencing = make(map[*checker.Expression]string)
	}
	for _, slot := range slots {
		if err := hoistSequencingInExpression(slot.key, body, state, indent); err != nil {
			return err
		}
		rendered, typ, err := slot.render(state)
		if err != nil {
			return err
		}
		state.sequenceCounter++
		temp := fmt.Sprintf("hex_seq_%d", state.sequenceCounter)
		// declaration spells the complete declarator: a bare CName is wrong
		// for String, List, Dict, and Fun<...>, each of which needs its own
		// pointer or function-pointer form, exactly as hoistTry's own
		// result temporary already relies on the same helper.
		fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(typ, temp, false), rendered)
		state.hoistedSequencing[slot.key] = temp
	}
	return nil
}

// hoistOperandSequence hoists a flat list of sibling operands that share one
// C compound position (call arguments with no callee, array elements, an
// ADT/object literal's remaining fields) in written order.
func hoistOperandSequence(operands []checker.Operand, body *strings.Builder, state *expressionValidation, indent string) error {
	slots := make([]sequenceSlot, len(operands))
	for i := range operands {
		index := i
		slots[i] = sequenceSlot{
			key: &operands[index].Node,
			render: func(state *expressionValidation) (string, compilerTypes.Type, error) {
				rendered, err := renderOperandWithState(operands[index], state)
				return rendered, operands[index].Type, err
			},
		}
	}
	return hoistSequenceSlots(slots, body, state, indent)
}

// appendReceiverSlotIfObserving appends a receiver or callee slot only when
// the receiver is itself effectful (a call or similar), never merely because
// a sibling argument is: some render paths take the receiver's address
// directly (the bounds-checked array accessor's "&" + receiver, Atomic's
// "&(" + receiver + ")"), and hoisting a pure receiver by value there would
// copy the place into a temporary and address the copy instead of the
// original storage, so a write or an atomic operation would silently land
// on a throwaway value instead of the real binding: a guaranteed,
// easily-triggered miscompilation for the common case of a mutable-place
// receiver. Leaving a pure receiver unhoisted is safe against that whole
// class of bug and correct for the overwhelmingly common case where nothing
// else in the group can affect the receiver's storage. It is a narrower
// trade-off against the aliasing concern motivating hoisting every operand
// (a sibling call that mutates through an alias of a pure-looking operand)
// in the rarer case where a sibling argument mutates the receiver's storage
// through an explicit reference (e.g. obj.method(f(ref obj))); closing that
// gap would require hoisting the receiver's address into a pointer
// temporary rather than its value, which is not done here. When the
// receiver is not hoisted, its own nested structure is still recursed into
// directly, so a grouped construct further inside it (e.g. a call returning
// the receiver object) is still resolved.
func appendReceiverSlotIfObserving(slots []sequenceSlot, receiver *checker.Expression, state *expressionValidation, body *strings.Builder, indent string, render func(state *expressionValidation) (string, compilerTypes.Type, error)) ([]sequenceSlot, error) {
	if receiver == nil {
		return slots, nil
	}
	if expressionMayObserve(receiver, state) {
		return append(slots, sequenceSlot{key: receiver, render: render}), nil
	}
	return slots, hoistSequencingInExpression(receiver, body, state, indent)
}

// hoistReceiverAndOperandsSequence hoists a receiver or callee together with
// its argument list as one written-order group: they all land in one C call
// or subscript expression, so an effectful receiver must be sequenced ahead
// of the arguments exactly as the arguments are sequenced among themselves.
// It covers CallExpression (receiver is the callee), IndexExpression
// (receiver plus the single index), VolatileWriteExpression (pointer plus
// value), and every built-in collection/atomic/stream method call whose
// receiver renders through the plain renderReceiver path (not the
// nominal-method nullable-narrowing adaptation MethodCallExpression alone
// needs; see hoistMethodCallSequence).
func hoistReceiverAndOperandsSequence(receiver *checker.Expression, receiverType compilerTypes.Type, arguments []checker.Operand, body *strings.Builder, state *expressionValidation, indent string) error {
	slots := make([]sequenceSlot, 0, len(arguments)+1)
	slots, err := appendReceiverSlotIfObserving(slots, receiver, state, body, indent, func(state *expressionValidation) (string, compilerTypes.Type, error) {
		rendered, err := renderReceiver(receiver, receiverType, state)
		return rendered, receiverType, err
	})
	if err != nil {
		return err
	}
	for i := range arguments {
		index := i
		slots = append(slots, sequenceSlot{
			key: &arguments[index].Node,
			render: func(state *expressionValidation) (string, compilerTypes.Type, error) {
				rendered, err := renderOperandWithState(arguments[index], state)
				return rendered, arguments[index].Type, err
			},
		})
	}
	return hoistSequenceSlots(slots, body, state, indent)
}

// hoistMethodCallSequence is hoistReceiverAndOperandsSequence's counterpart
// for MethodCallExpression, whose receiver first resolves through
// methodReceiverType's nullable-narrowing adaptation before rendering, so
// the hoisted temporary's declared type matches what renderReceiver actually
// produces.
func hoistMethodCallSequence(node *checker.Expression, body *strings.Builder, state *expressionValidation, indent string) error {
	slots := make([]sequenceSlot, 0, len(node.Arguments)+1)
	receiver := node.Operand
	operandType := node.OperandType
	slots, err := appendReceiverSlotIfObserving(slots, receiver, state, body, indent, func(state *expressionValidation) (string, compilerTypes.Type, error) {
		receiverType, err := methodReceiverType(*receiver, operandType, state)
		if err != nil {
			return "", compilerTypes.Type{}, err
		}
		rendered, err := renderReceiver(receiver, receiverType, state)
		return rendered, receiverType, err
	})
	if err != nil {
		return err
	}
	for i := range node.Arguments {
		index := i
		slots = append(slots, sequenceSlot{
			key: &node.Arguments[index].Node,
			render: func(state *expressionValidation) (string, compilerTypes.Type, error) {
				rendered, err := renderOperandWithState(node.Arguments[index], state)
				return rendered, node.Arguments[index].Type, err
			},
		})
	}
	return hoistSequenceSlots(slots, body, state, indent)
}

// hoistAdtSequence hoists an ADT variant constructor's payload fields in
// written order (node.EvaluationOrder), each keyed by its declaration-order
// slot in node.Arguments so renderAdtConstruct's declaration-order render
// loop still finds it.
func hoistAdtSequence(node *checker.Expression, body *strings.Builder, state *expressionValidation, indent string) error {
	if len(node.EvaluationOrder) < 2 {
		return nil
	}
	slots := make([]sequenceSlot, len(node.EvaluationOrder))
	for position, declaredIndex := range node.EvaluationOrder {
		index := declaredIndex
		slots[position] = sequenceSlot{
			key: &node.Arguments[index].Node,
			render: func(state *expressionValidation) (string, compilerTypes.Type, error) {
				rendered, err := renderOperandWithState(node.Arguments[index], state)
				return rendered, node.Arguments[index].Type, err
			},
		}
	}
	return hoistSequenceSlots(slots, body, state, indent)
}

// hoistObjectSequence hoists an object literal's member initializers in
// value.Initializers order (written order), each keyed by that initializer's
// own slot so objectLiteralWithState's declaration-order render loop still
// finds it.
func hoistObjectSequence(object *checker.ObjectValue, body *strings.Builder, state *expressionValidation, indent string) error {
	if object == nil || len(object.Initializers) < 2 {
		return nil
	}
	slots := make([]sequenceSlot, len(object.Initializers))
	for i := range object.Initializers {
		index := i
		slots[i] = sequenceSlot{
			key: &object.Initializers[index].Source.Node,
			render: func(state *expressionValidation) (string, compilerTypes.Type, error) {
				rendered, err := renderOperandWithState(object.Initializers[index].Source, state)
				return rendered, object.Initializers[index].Source.Type, err
			},
		}
	}
	return hoistSequenceSlots(slots, body, state, indent)
}

// hoistSequencingInExpression walks one expression subtree, hoisting every
// compound position that needs it. For a grouped kind (see the switch
// below), recursing into each of the group's own slots happens inside
// hoistSequenceSlots, interleaved with that slot's own hoist, in written
// order: this is what makes a nested compound expression like
// f(g(a(), b()), c()) hoist a() and b() into their own temporaries before
// g(...)'s own temporary, which in turn precedes c()'s, while also keeping
// a() + f(b(), c())'s a() ahead of f's own b()/c() temporaries even though
// a() has no nested structure of its own. For every other kind, the default
// case recurses into each reachable child directly, with no group hoist at
// this level, so a nested grouped construct further down still gets
// resolved even where this node's own render site was not individually
// verified to consult hoistedSequencing (see the default case's own
// comment for why that verification gates widening the grouped switch).
//
// Four shapes are deliberately not walked structurally:
//   - TryExpression and SpawnExpression are already fully resolved by the
//     earlier hoistTryInStatement/hoistConcurrencyInStatement passes in this
//     same statement (run first in writeStatementsAt); recursing into their
//     operand here would evaluate it a second time.
//   - A Dict.find CollectionMethodCallExpression is likewise already
//     resolved by hoistDictFindInStatement.
//   - MatchExpression only evaluates its scrutinee unconditionally; its arms
//     are conditional, so only the scrutinee is walked.
//   - A short-circuit BinaryOperationExpression (and/or) only evaluates its
//     right operand conditionally, so only the left is walked; hoisting
//     anything from the right into an unconditional prologue would break
//     short-circuiting.
func hoistSequencingInExpression(node *checker.Expression, body *strings.Builder, state *expressionValidation, indent string) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case checker.TryExpression, checker.SpawnExpression:
		return nil
	case checker.CollectionMethodCallExpression:
		if node.Name == "find" && node.OperandType.Dict != nil {
			return nil
		}
	case checker.MatchExpression:
		return hoistSequencingInExpression(node.Operand, body, state, indent)
	case checker.BinaryOperationExpression:
		if node.Operator == checker.LogicalAndOperator || node.Operator == checker.LogicalOrOperator {
			return hoistSequencingInExpression(node.Left, body, state, indent)
		}
	}

	switch node.Kind {
	case checker.CallExpression:
		return hoistReceiverAndOperandsSequence(node.Operand, node.OperandType, node.Arguments, body, state, indent)
	case checker.IndexExpression, checker.VolatileWriteExpression:
		// Both render their receiver through the plain renderReceiver path
		// (verified against arrays.go and render.go) and combine it with
		// exactly one argument in one C expression.
		if node.Operand == nil || len(node.Arguments) == 0 {
			return nil
		}
		return hoistReceiverAndOperandsSequence(node.Operand, node.OperandType, node.Arguments, body, state, indent)
	case checker.MethodCallExpression:
		return hoistMethodCallSequence(node, body, state, indent)
	case checker.AtomicMethodCallExpression, checker.StreamMethodCallExpression:
		// Both combine their receiver with every argument in one C call
		// (renderAtomicMethod, renderStreamMethod, both verified and
		// updated to consult hoistedSequencing for receiver and arguments
		// alike): compare_exchange's expected/desired pair and read's
		// into/maximum pair each land in one call with no C ordering
		// guarantee between them or against the receiver.
		if node.Operand == nil {
			return hoistOperandSequence(node.Arguments, body, state, indent)
		}
		return hoistReceiverAndOperandsSequence(node.Operand, node.OperandType, node.Arguments, body, state, indent)
	case checker.ChannelConstructorExpression:
		// Channel<T>.new(heap, capacity) has no receiver; renderChannelConstructor
		// was verified and updated to consult hoistedSequencing for both arguments.
		return hoistOperandSequence(node.Arguments, body, state, indent)
	case checker.ArrayLiteralExpression:
		return hoistOperandSequence(node.Arguments, body, state, indent)
	case checker.BinaryOperationExpression, checker.DeepEqualityExpression, checker.StringCompareExpression,
		checker.UnionEqualityExpression:
		// DeepEqualityExpression (== and != on String, Strand, List, and
		// object types), StringCompareExpression (<, <=, >, >= on String
		// and Strand), and UnionEqualityExpression (== and != on two
		// canonical unions) are still binary expressions from the source
		// language's point of view and render their Left/Right through the
		// same render-with-expected-type pattern BinaryOperationExpression
		// uses (verified against each one's own render function); none
		// ever carries a shift operator, so the shift-count special case
		// below is inert for them.
		if node.Left == nil || node.Right == nil {
			return nil
		}
		// A shift count keeps its own integer type rather than the left
		// operand's, mirroring renderBinaryOperationWithState exactly so a
		// hoisted temporary's declared type matches what the unhoisted
		// render path would have produced.
		rightExpected := node.OperandType
		if node.Operator == checker.ShiftLeftOperator || node.Operator == checker.ShiftRightOperator {
			if rightType, ok := expressionTypeWithState(*node.Right, state); ok {
				rightExpected = rightType
			}
		}
		return hoistSequenceSlots([]sequenceSlot{
			{
				key: node.Left,
				render: func(state *expressionValidation) (string, compilerTypes.Type, error) {
					rendered, err := renderExpressionExpectedWithState(*node.Left, &node.OperandType, state)
					return rendered, node.OperandType, err
				},
			},
			{
				key: node.Right,
				render: func(state *expressionValidation) (string, compilerTypes.Type, error) {
					rendered, err := renderExpressionExpectedWithState(*node.Right, &rightExpected, state)
					return rendered, rightExpected, err
				},
			},
		}, body, state, indent)
	case checker.AdtConstructExpression:
		return hoistAdtSequence(node, body, state, indent)
	case checker.ObjectExpression:
		return hoistObjectSequence(node.Object, body, state, indent)
	default:
		// Every remaining call-shaped or mutating kind (built-in
		// collection, string, rune cursor, and mutex methods; Heap, List,
		// Dict, and mutex constructors; print) is intentionally not
		// grouped here: their render functions call renderOperandWithState
		// directly on their operand(s), never consulting
		// hoistedSequencing, and most take at most one argument besides
		// their receiver, so there is no sibling to sequence against
		// regardless. Grouping one of these without first updating its
		// render site would hoist its effect into a prologue and then
		// evaluate it again, unchanged, at the original site. Still
		// recurse into every reachable child with no group decision at
		// this level, so a grouped construct nested inside one of these
		// (e.g. a CallExpression passed as a List.push argument) is still
		// resolved.
		if err := hoistSequencingInExpression(node.Operand, body, state, indent); err != nil {
			return err
		}
		if err := hoistSequencingInExpression(node.Left, body, state, indent); err != nil {
			return err
		}
		if err := hoistSequencingInExpression(node.Right, body, state, indent); err != nil {
			return err
		}
		for index := range node.Arguments {
			if err := hoistSequencingInExpression(&node.Arguments[index].Node, body, state, indent); err != nil {
				return err
			}
		}
		if node.Object != nil {
			for index := range node.Object.Initializers {
				if err := hoistSequencingInExpression(&node.Object.Initializers[index].Source.Node, body, state, indent); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// forceHoistAssignmentTargetIndex closes the one gap the ordinary group
// hoist leaves open for an assignment: "an assignment evaluates the target
// place... before evaluating the source value" is only observable when the
// source is itself effectful, and IndexExpression's own hoist
// (hoistReceiverAndOperandsSequence) does not hoist a lone index argument
// when its receiver is pure, since within the index expression alone there
// is no sibling to sequence against. But an assignment's target and source
// still land in one unsequenced C assignment expression, so a target index
// that may observe must still be captured ahead of an effectful source even
// with no sibling of its own. A target's receiver chain is otherwise always
// statically addressable (a call's result cannot be assigned into), so the
// index is the only place-determining sub-expression that can need this.
func forceHoistAssignmentTargetIndex(target, source *checker.Expression, body *strings.Builder, state *expressionValidation, indent string) error {
	if target.Kind != checker.IndexExpression || len(target.Arguments) == 0 {
		return nil
	}
	key := &target.Arguments[0].Node
	if _, hoisted := hoistedSequenceValue(state, key); hoisted {
		return nil
	}
	if !expressionMayObserve(key, state) || !expressionMayObserve(source, state) {
		return nil
	}
	// The index's own nested structure was already recursed into by the
	// hoistSequencingInExpression(target, ...) call preceding this one (via
	// IndexExpression's hoistReceiverAndOperandsSequence, whose single-slot
	// case still recurses without hoisting); only the top-level wrap into
	// its own temporary remains.
	operand := target.Arguments[0]
	rendered, err := renderOperandWithState(operand, state)
	if err != nil {
		return err
	}
	if state.hoistedSequencing == nil {
		state.hoistedSequencing = make(map[*checker.Expression]string)
	}
	state.sequenceCounter++
	temp := fmt.Sprintf("hex_seq_%d", state.sequenceCounter)
	fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(operand.Type, temp, false), rendered)
	state.hoistedSequencing[key] = temp
	return nil
}

// hoistEvaluationOrderInStatement is the per-statement entry point for
// evaluation-order sequencing, run in writeStatementsAt after the existing
// concurrency/try/Dict.find hoists: it walks each of this statement's own
// top-level operands, in the statement's own evaluation order (target
// before source for an assignment, so the target place, including its
// receiver and index, is evaluated before the source value), hoisting every
// nested compound expression that needs it into a temporary before the
// statement renders.
func hoistEvaluationOrderInStatement(statement checker.Statement, body *strings.Builder, state *expressionValidation, indent string) error {
	switch statement := statement.(type) {
	case checker.Declaration:
		return hoistSequencingInExpression(&statement.Source.Node, body, state, indent)
	case checker.Assignment:
		if err := hoistSequencingInExpression(&statement.Target.Node, body, state, indent); err != nil {
			return err
		}
		if err := forceHoistAssignmentTargetIndex(&statement.Target.Node, &statement.Source.Node, body, state, indent); err != nil {
			return err
		}
		return hoistSequencingInExpression(&statement.Source.Node, body, state, indent)
	case checker.CallStatement:
		return hoistSequencingInExpression(&statement.Call.Node, body, state, indent)
	case checker.TryStatement:
		return hoistSequencingInExpression(&statement.Expression.Node, body, state, indent)
	case checker.ReturnStatement:
		if statement.Value != nil {
			return hoistSequencingInExpression(&statement.Value.Node, body, state, indent)
		}
		return nil
	case checker.DeferStatement:
		return hoistSequencingInExpression(&statement.Expression.Node, body, state, indent)
	case checker.ErrdeferStatement:
		return hoistSequencingInExpression(&statement.Expression.Node, body, state, indent)
	case checker.IfStatement:
		if err := hoistSequencingInExpression(&statement.Condition.Node, body, state, indent); err != nil {
			return err
		}
		for index := range statement.ElseIf {
			if err := hoistSequencingInExpression(&statement.ElseIf[index].Condition.Node, body, state, indent); err != nil {
				return err
			}
		}
		return nil
	case checker.ForStatement:
		return hoistSequencingInExpression(&statement.Source.Node, body, state, indent)
	case checker.WhileStatement:
		return hoistSequencingInExpression(&statement.Condition.Node, body, state, indent)
	case checker.BreakStatement, checker.ContinueStatement, checker.FunctionDeclaration,
		checker.MethodDeclaration:
		return nil
	default:
		return unknownExpressionDiagnostic("unsupported checked statement")
	}
}

// hoistedSequenceValue looks up a checked node's evaluation-order hoisted
// temporary, if hoistEvaluationOrderInStatement already replaced it.
func hoistedSequenceValue(state *expressionValidation, key *checker.Expression) (string, bool) {
	if state == nil || state.hoistedSequencing == nil || key == nil {
		return "", false
	}
	name, ok := state.hoistedSequencing[key]
	return name, ok
}

// renderHoistedOperand renders one operand, preferring its evaluation-order
// hoisted temporary when present so a hoisted operand renders as a bare
// reference instead of evaluating its effect a second time.
func renderHoistedOperand(key *checker.Expression, source checker.Operand, state *expressionValidation) (string, error) {
	if name, ok := hoistedSequenceValue(state, key); ok {
		return name, nil
	}
	return renderOperandWithState(source, state)
}

// renderHoistedReceiver is renderHoistedOperand's counterpart for a receiver
// or callee expression rendered through renderReceiver.
func renderHoistedReceiver(key *checker.Expression, expected compilerTypes.Type, state *expressionValidation) (string, error) {
	if name, ok := hoistedSequenceValue(state, key); ok {
		return name, nil
	}
	return renderReceiver(key, expected, state)
}

// renderHoistedExpressionExpected is renderHoistedOperand's counterpart for
// a bare *checker.Expression child (BinaryOperationExpression's Left/Right).
func renderHoistedExpressionExpected(key *checker.Expression, expected *compilerTypes.Type, state *expressionValidation) (string, error) {
	if name, ok := hoistedSequenceValue(state, key); ok {
		return name, nil
	}
	return renderExpressionExpectedWithState(*key, expected, state)
}

// renderHoistedExpressionNode mirrors renderExpressionNodeWithExpectedState's
// (value, atomic, error) shape: a hoisted temporary is always a bare
// identifier, hence always atomic.
func renderHoistedExpressionNode(key *checker.Expression, expected *compilerTypes.Type, state *expressionValidation) (string, bool, error) {
	if name, ok := hoistedSequenceValue(state, key); ok {
		return name, true, nil
	}
	return renderExpressionNodeWithExpectedState(*key, expected, state)
}
