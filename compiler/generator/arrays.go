package generator

import (
	"fmt"
	"slices"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedArrayState records the array types that need struct and element
// accessor definitions, in deterministic order.
type generatedArrayState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.ArrayInfo]bool
	// demand records, per specialization, which accessor directions some
	// access actually reaches after RFC 0088's elision. An Array whose only
	// uses are for-in and constant indices reaches neither and gets no
	// accessor at all.
	demand map[*compilerTypes.ArrayInfo]arrayAccessorDemand
}

// arrayAccessorDemand is one specialization's surviving accessor need. read
// selects hex_array_at_, write selects hex_array_at_mut_; the renderer picks
// between them by receiver mutability, not by whether the access writes, so
// a read through a mut binding still demands the mutable accessor.
type arrayAccessorDemand struct {
	read  bool
	write bool
}

// arrayIndexCheckSurvives reports whether an index access still needs a
// bounds check, and therefore an accessor. A constant index is one the
// checker already proved in range — it rejects the failing half outright — so
// the check is dead by construction.
//
// This is deliberately narrower than the checker's proof: the checker
// resolves constants through immutable bindings, so `n: Size := 0` followed by
// `a[n]` is proven at check time but arrives here as a variable reference.
// Emitting a check that cannot fire is always correct, so the narrow rule is
// safe; RFC 0088 promises only the literal case.
func arrayIndexCheckSurvives(node checker.Expression) bool {
	if node.Kind != checker.IndexExpression || node.OperandType.Array == nil || len(node.Arguments) != 1 {
		return false
	}
	return node.Arguments[0].Kind != checker.ConstantOperand
}

// recordArrayAccessorDemand marks the accessor direction one surviving access
// needs. Callers filter with arrayIndexCheckSurvives first.
func (state *generatedArrayState) recordArrayAccessorDemand(array *compilerTypes.ArrayInfo, writable bool) {
	if state == nil || array == nil {
		return
	}
	if state.demand == nil {
		state.demand = make(map[*compilerTypes.ArrayInfo]arrayAccessorDemand)
	}
	entry := state.demand[array]
	if writable {
		entry.write = true
	} else {
		entry.read = true
	}
	state.demand[array] = entry
}

// accessorDemandFor returns one specialization's surviving accessor need.
func (state *generatedArrayState) accessorDemandFor(array compilerTypes.Type) arrayAccessorDemand {
	if state == nil || state.demand == nil || array.Array == nil {
		return arrayAccessorDemand{}
	}
	return state.demand[array.Array]
}

// discoverGeneratedArrays walks every type reachable from the program and
// collects the distinct array types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedArrays(program checker.Program) *generatedArrayState {
	state := &generatedArrayState{seen: make(map[*compilerTypes.ArrayInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) {
			if typ.Array != nil {
				if !state.seen[typ.Array] {
					state.seen[typ.Array] = true
					state.order = append(state.order, typ)
				}
			}
		},
	}
	walkProgram(program, visitor)

	slices.SortStableFunc(state.order, func(left, right compilerTypes.Type) int {
		return strings.Compare(left.CName, right.CName)
	})
	return state
}

// arrayDependencyOrder orders array types so every element-array appears
// before the array embedding it, preserving the discovery order otherwise.
func arrayDependencyOrder(order []compilerTypes.Type) []compilerTypes.Type {
	byName := make(map[string]compilerTypes.Type, len(order))
	for _, array := range order {
		byName[array.CName] = array
	}
	visited := make(map[string]bool)
	result := make([]compilerTypes.Type, 0, len(order))
	var visit func(array compilerTypes.Type)
	visit = func(array compilerTypes.Type) {
		if visited[array.CName] {
			return
		}
		visited[array.CName] = true
		if element := array.Array.Element; element.Array != nil {
			if inner, ok := byName[element.CName]; ok {
				visit(inner)
			}
		}
		result = append(result, array)
	}
	for _, array := range order {
		visit(array)
	}
	return result
}

// matchingView returns the discovered view type over one element, or the zero
// Type when no such view is used.
func matchingView(views *generatedViewState, element compilerTypes.Type) compilerTypes.Type {
	if views == nil {
		return compilerTypes.Type{}
	}
	for _, view := range views.views {
		if compilerTypes.Equal(view.View.Element, element) {
			return view
		}
	}
	return compilerTypes.Type{}
}

func arrayAccessorSuffix(array compilerTypes.Type) string {
	return strings.TrimPrefix(array.CName, "hex_array_")
}

// arrayAccessorCName selects the read or write accessor for one array type;
// writable selects the mutable variant.
func arrayAccessorCName(array compilerTypes.Type, writable bool) string {
	name := "hex_array_at_" + arrayAccessorSuffix(array)
	if writable {
		name = "hex_array_at_mut_" + arrayAccessorSuffix(array)
	}
	return name
}

func validateCollectionConstructor(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.ListNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType.List == nil || !compilerTypes.Equal(node.Element, node.ResultType.List.Element) || !compilerTypes.IsHeap(node.OperandType) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("List<T>.new has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("List<T>.new result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.DictNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType.Dict == nil || !compilerTypes.Equal(node.Element, node.ResultType.Dict.Value) || !compilerTypes.IsHeap(node.OperandType) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("Dict<K, V>.new has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("Dict<K, V>.new result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	}
	return unknownExpressionDiagnostic("unsupported collection constructor")
}

func validateCollectionExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.ArrayLiteralExpression:
		if node.ResultType.Array == nil || !compilerTypes.Equal(node.OperandType, node.ResultType.Array.Element) || len(node.Arguments) != int(node.ResultType.Array.Length) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("array literal has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("array literal type does not match its expected type")
		}
		for _, element := range node.Arguments {
			if err := validateCheckedOperandWithState(element, state); err != nil {
				return err
			}
			if !generatedAssignable(node.OperandType, element.Type) {
				return unknownExpressionDiagnostic("array literal element does not match its element type")
			}
		}
		return nil
	case checker.IndexExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.Array == nil && node.OperandType.View == nil && node.OperandType.List == nil && !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("index expression has invalid checked metadata")
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
		if !compilerTypes.Equal(node.ResultType, element) {
			return unknownExpressionDiagnostic("index expression has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("index expression type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.CollectionMethodCallExpression:
		if node.Operand == nil || node.OperandType.Array == nil && node.OperandType.View == nil && node.OperandType.List == nil && node.OperandType.Dict == nil || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("collection method call has invalid checked metadata")
		}
		element := node.Element
		if node.OperandType.Array != nil {
			element = node.OperandType.Array.Element
		} else if node.OperandType.View != nil {
			element = node.OperandType.View.Element
		} else if node.OperandType.List != nil {
			element = node.OperandType.List.Element
		} else {
			element = node.OperandType.Dict.Value
		}
		switch node.Name {
		case "length":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) && !compilerTypes.Equal(node.ResultType, compilerTypes.UInt64) {
				return unknownExpressionDiagnostic("collection length call has invalid checked metadata")
			}
		case "push":
			if node.OperandType.List == nil || len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("list push call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "clear":
			if node.OperandType.List == nil || len(node.Arguments) != 0 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("list clear call has invalid checked metadata")
			}
		case "pop":
			if node.OperandType.List == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, element) {
				return unknownExpressionDiagnostic("list pop call has invalid checked metadata")
			}
		case "free":
			if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) || node.OperandType.List == nil && node.OperandType.Dict == nil {
				return unknownExpressionDiagnostic("collection free call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
			if expected != nil {
				return unknownExpressionDiagnostic("collection free produces no value")
			}
		case "insert":
			if node.OperandType.Dict == nil || len(node.Arguments) != 2 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("dictionary insert call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "get", "remove":
			if node.OperandType.Dict == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, element) {
				return unknownExpressionDiagnostic("dictionary lookup call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "contains":
			if node.OperandType.Dict == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("dictionary contains call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		default:
			return unknownExpressionDiagnostic("unknown collection method")
		}
		if node.Name != "free" && node.Name != "insert" && expected != nil && !compilerTypes.Equal(*expected, node.ResultType) && !compilerTypes.Assignable(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("collection method result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.CollectionSliceExpression:
		if node.Operand == nil || len(node.Arguments) != 2 || node.ResultType.View == nil || !compilerTypes.Equal(node.ResultType.View.Element, node.Element) || node.OperandType.Array == nil && node.OperandType.View == nil && node.OperandType.List == nil || !supportedGeneratedTypeWithState(node.OperandType, state) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("collection slice has invalid checked metadata")
		}
		var element compilerTypes.Type
		if node.OperandType.Array != nil {
			element = node.OperandType.Array.Element
		} else if node.OperandType.View != nil {
			element = node.OperandType.View.Element
		} else {
			element = node.OperandType.List.Element
		}
		if !compilerTypes.Equal(node.Element, element) {
			return unknownExpressionDiagnostic("collection slice element does not match its receiver")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("collection slice result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	}
	return unknownExpressionDiagnostic("unsupported collection expression")
}

func renderCollectionConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.ListNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("List<T>.new without a checked heap")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, &compilerTypes.Heap, state)
		if heapErr != nil {
			return "", heapErr
		}
		return "hex_list_new_" + listSuffix(node.ResultType) + "(" + heap + ")", nil
	case checker.DictNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("Dict<K, V>.new without a checked heap")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, &compilerTypes.Heap, state)
		if heapErr != nil {
			return "", heapErr
		}
		return "hex_dict_new_" + dictSuffix(node.ResultType) + "(" + heap + ")", nil
	}
	return "", unknownExpressionDiagnostic("unsupported collection constructor")
}

func renderCollectionExpression(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.IndexExpression:
		place, placeErr := checkedPlaceMetadata(node, state)
		if placeErr != nil {
			return "", placeErr
		}
		receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		index, indexErr := renderOperandWithState(node.Arguments[0], state)
		if indexErr != nil {
			return "", indexErr
		}
		if node.OperandType.View != nil {
			return "*hex_view_at_" + strings.TrimPrefix(node.OperandType.CName, "hex_view_") + "(" + receiver + ", (size_t)(" + index + "))", nil
		}
		if node.OperandType.List != nil {
			if place.writable {
				return "*hex_list_at_mut_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + index + "))", nil
			}
			return "*hex_list_at_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + index + "))", nil
		}
		// RFC 0088: an index the checker already proved in range needs no
		// check, so it needs no accessor call either.
		if !arrayIndexCheckSurvives(node) {
			return receiver + ".data[" + index + "]", nil
		}
		// This call is the accessor's only demand, so it is recorded here
		// rather than derived again from the checked tree.
		if state != nil && state.generatedTypes != nil {
			state.generatedTypes.arrays.recordArrayAccessorDemand(node.OperandType.Array, place.writable)
		}
		return "*" + arrayAccessorCName(node.OperandType, place.writable) + "(&" + receiver + ", (size_t)(" + index + "))", nil
	case checker.ArrayLiteralExpression:
		if node.ResultType.Array == nil {
			return "", unknownExpressionDiagnostic("array literal without a checked array type")
		}
		elements := make([]string, len(node.Arguments))
		for index, element := range node.Arguments {
			rendered, elementErr := renderOperandWithState(element, state)
			if elementErr != nil {
				return "", elementErr
			}
			elements[index] = rendered
		}
		// The element region is the struct's single data member, so the
		// compound literal carries one extra brace layer.
		return "(" + node.ResultType.CName + "){{" + strings.Join(elements, ", ") + "}}", nil
	case checker.CollectionMethodCallExpression:
		switch node.Name {
		case "length":
			if node.OperandType.Array != nil {
				return fmt.Sprintf("(size_t)(%d)", node.OperandType.Array.Length), nil
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			if node.OperandType.List != nil || node.OperandType.Dict != nil {
				// List and Dict bindings are pointer-sized handles.
				return "(" + receiver + ")->length", nil
			}
			return "(" + receiver + ").length", nil
		case "push", "clear", "pop":
			if node.Operand == nil || node.OperandType.List == nil {
				return "", unknownExpressionDiagnostic("list mutation without a checked list receiver")
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			suffix := listSuffix(node.OperandType)
			switch node.Name {
			case "push":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic("list push without a checked value")
				}
				value, valueErr := renderOperandWithState(node.Arguments[0], state)
				if valueErr != nil {
					return "", valueErr
				}
				return "hex_list_push_" + suffix + "(" + receiver + ", " + value + ")", nil
			case "clear":
				return "hex_list_clear_" + suffix + "(" + receiver + ")", nil
			case "pop":
				return "hex_list_pop_" + suffix + "(" + receiver + ")", nil
			}
		case "free":
			if node.Operand == nil || len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic("collection free without a checked heap")
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			heap, heapErr := renderOperandWithState(node.Arguments[0], state)
			if heapErr != nil {
				return "", heapErr
			}
			if node.OperandType.List != nil {
				return "hex_list_free_" + listSuffix(node.OperandType) + "(" + heap + ", " + receiver + ")", nil
			}
			if node.OperandType.Dict != nil {
				return "hex_dict_free_" + dictSuffix(node.OperandType) + "(" + heap + ", " + receiver + ")", nil
			}
			return "", unknownExpressionDiagnostic("collection free without a list or dictionary receiver")
		case "insert", "get", "contains", "remove":
			if node.Operand == nil || node.OperandType.Dict == nil {
				return "", unknownExpressionDiagnostic("dictionary operation without a checked dictionary receiver")
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			suffix := dictSuffix(node.OperandType)
			switch node.Name {
			case "insert":
				if len(node.Arguments) != 2 {
					return "", unknownExpressionDiagnostic("dictionary insert without checked operands")
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				value, valueErr := renderOperandWithState(node.Arguments[1], state)
				if valueErr != nil {
					return "", valueErr
				}
				return "hex_dict_insert_" + suffix + "(" + receiver + ", " + key + ", " + value + ")", nil
			case "get", "remove":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic("dictionary lookup without a checked key")
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				return "hex_dict_" + node.Name + "_" + suffix + "(" + receiver + ", " + key + ")", nil
			case "contains":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic("dictionary contains without a checked key")
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				return "hex_dict_contains_" + suffix + "(" + receiver + ", " + key + ")", nil
			}
		}
		return "", unknownExpressionDiagnostic("unknown collection method")
	case checker.CollectionSliceExpression:
		if node.Operand == nil || len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic("collection slice without checked bounds")
		}
		receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		start, startErr := renderOperandWithState(node.Arguments[0], state)
		if startErr != nil {
			return "", startErr
		}
		end, endErr := renderOperandWithState(node.Arguments[1], state)
		if endErr != nil {
			return "", endErr
		}
		if node.OperandType.View != nil {
			return "hex_view_slice_" + strings.TrimPrefix(node.OperandType.CName, "hex_view_") + "(" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
		}
		if node.OperandType.List != nil {
			return "hex_list_slice_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
		}
		return "hex_array_slice_" + arrayAccessorSuffix(node.OperandType) + "(&" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
	}
	return "", unknownExpressionDiagnostic("unsupported collection expression")
}

// collectionsNeedView reports whether any reachable Array or List
// specialization has a matching View, which is the only reason either
// component header names the view component. A program with arrays but no
// slicing needs no view artifact and must not declare a dependency on one:
// a component's declared dependencies are exactly what its emitted content
// uses.
func collectionsNeedView(arrays *generatedArrayState, lists *generatedListState, views *generatedViewState) bool {
	if views == nil {
		return false
	}
	if arrays != nil {
		for _, array := range arrays.order {
			if matchingView(views, array.Array.Element) != (compilerTypes.Type{}) {
				return true
			}
		}
	}
	if lists != nil {
		for _, list := range lists.order {
			if matchingView(views, list.List.Element) != (compilerTypes.Type{}) {
				return true
			}
		}
	}
	return false
}
