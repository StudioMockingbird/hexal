package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// builtinMethodRecord resolves one recorded built-in method for lowering. It is
// a variable so a test can prove the lowering reads the registry: corrupting a
// recorded symbol must move the emitted C.
var builtinMethodRecord = specdata.Method

// builtinMethodCallSymbol resolves one recorded built-in method's emitted C
// symbol and expands its per-specialization suffix. The registry owns the
// symbol fact; the lowering switch still owns the call's argument shape. A
// missing record or an empty symbol is a compiler defect, because the checker
// admits only methods the registry declares.
func builtinMethodCallSymbol(owner specdata.TypePattern, name, suffix string) (string, error) {
	method, ok := builtinMethodRecord(owner, name)
	if !ok || method.RuntimeSymbol == "" {
		return "", unknownExpressionDiagnostic()
	}
	// The registry writes the generator's own %s placeholder exactly where
	// the generator writes a suffix, so this is a literal substitution, never
	// a format string: a recorded symbol may contain other percent signs.
	return strings.ReplaceAll(method.RuntimeSymbol, "%s", suffix), nil
}

// matchingSlice returns the discovered slice type over one element in the
// requested access mode, or the zero Type when no such slice is used.
func matchingSlice(slices *generatedSliceState, element compilerTypes.Type, writable bool) compilerTypes.Type {
	if slices == nil {
		return compilerTypes.Type{}
	}
	for _, slice := range slices.slices {
		if slice.Slice.Writable == writable && compilerTypes.Equal(slice.Slice.Element, element) {
			return slice
		}
	}
	return compilerTypes.Type{}
}

// sliceAtHelper selects the read or writable element-access helper for one
// reachable slice type.
func sliceAtHelper(slice compilerTypes.Type) string {
	if slice.Slice.Writable {
		return "hex_mut_slice_at_" + strings.TrimPrefix(slice.CName, "hex_mut_slice_")
	}
	return "hex_slice_at_" + strings.TrimPrefix(slice.CName, "hex_slice_")
}

// sliceSliceHelper selects the read or writable re-slice helper for one
// reachable slice type. Slice.slice's record deliberately carries no
// RuntimeSymbol: the emitted symbol follows the receiver's access mode, so it
// is composed from the receiver's C name here.
func sliceSliceHelper(slice compilerTypes.Type) string {
	if slice.Slice.Writable {
		return "hex_mut_slice_slice_" + strings.TrimPrefix(slice.CName, "hex_mut_slice_")
	}
	return "hex_slice_slice_" + strings.TrimPrefix(slice.CName, "hex_slice_")
}

func validateCollectionConstructor(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.ListNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType.List == nil || !compilerTypes.Equal(node.Element, node.ResultType.List.Element) || !compilerTypes.IsHeap(node.OperandType) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.DictNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType.Dict == nil || !compilerTypes.Equal(node.Element, node.ResultType.Dict.Value) || !compilerTypes.IsHeap(node.OperandType) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	}
	return unknownExpressionDiagnostic()
}

func validateCollectionExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.InlineListLiteralExpression:
		capacity := uint64(0)
		element := compilerTypes.Type{}
		if node.ResultType.InlineList != nil {
			capacity = node.ResultType.InlineList.Capacity
			element = node.ResultType.InlineList.Element
		}
		if element == (compilerTypes.Type{}) || !compilerTypes.Equal(node.OperandType, element) || uint64(len(node.Arguments)) > capacity || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		for _, element := range node.Arguments {
			if err := validateCheckedOperandWithState(element, state); err != nil {
				return err
			}
			if !generatedAssignable(node.OperandType, element.Type) {
				return unknownExpressionDiagnostic()
			}
		}
		return nil
	case checker.IndexExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.InlineList == nil && node.OperandType.Slice == nil && node.OperandType.List == nil || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic()
		}
		var element compilerTypes.Type
		if node.OperandType.InlineList != nil {
			element = node.OperandType.InlineList.Element
		} else if node.OperandType.Slice != nil {
			element = node.OperandType.Slice.Element
		} else {
			element = node.OperandType.List.Element
		}
		if !compilerTypes.Equal(node.ResultType, element) {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.CollectionMethodCallExpression:
		if node.Operand == nil || node.OperandType.InlineList == nil && node.OperandType.Slice == nil && node.OperandType.List == nil && node.OperandType.Dict == nil || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic()
		}
		element := node.Element
		if node.OperandType.InlineList != nil {
			element = node.OperandType.InlineList.Element
		} else if node.OperandType.Slice != nil {
			element = node.OperandType.Slice.Element
		} else if node.OperandType.List != nil {
			element = node.OperandType.List.Element
		} else {
			element = node.OperandType.Dict.Value
		}
		switch node.Name {
		case "pointer":
			// Slice only: the result is the receiver's element pointer, made
			// nullable, with the receiver's access mode preserved.
			if node.OperandType.Slice == nil || len(node.Arguments) != 0 {
				return unknownExpressionDiagnostic()
			}
			nullable := node.ResultType
			if !compilerTypes.IsNullable(nullable) || nullable.Element == nil || !compilerTypes.Equal(*nullable.Element, element) {
				return unknownExpressionDiagnostic()
			}
		case "length":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) && !compilerTypes.Equal(node.ResultType, compilerTypes.UInt64) {
				return unknownExpressionDiagnostic()
			}
		case "push":
			if node.OperandType.List == nil && node.OperandType.InlineList == nil || len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic()
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "clear":
			if node.OperandType.List == nil && node.OperandType.InlineList == nil || len(node.Arguments) != 0 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic()
			}
		case "pop":
			if node.OperandType.List == nil && node.OperandType.InlineList == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, element) {
				return unknownExpressionDiagnostic()
			}
		case "free":
			if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) || node.OperandType.List == nil && node.OperandType.Dict == nil {
				return unknownExpressionDiagnostic()
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
			if expected != nil {
				return unknownExpressionDiagnostic()
			}
		case "insert":
			if node.OperandType.Dict == nil || len(node.Arguments) != 2 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic()
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "get", "remove":
			if node.OperandType.Dict == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, element) {
				return unknownExpressionDiagnostic()
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "find":
			if node.OperandType.Dict == nil || len(node.Arguments) != 1 || !compilerTypes.IsUnion(node.ResultType) || !compilerTypes.ContainsUnionMember(node.ResultType, compilerTypes.Nil) || !findValueFitsResult(element, node.ResultType) {
				return unknownExpressionDiagnostic()
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "contains":
			if node.OperandType.Dict == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic()
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		default:
			return unknownExpressionDiagnostic()
		}
		if node.Name != "free" && node.Name != "insert" && expected != nil && !compilerTypes.Equal(*expected, node.ResultType) && !compilerTypes.Assignable(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.CollectionSliceExpression:
		if node.Operand == nil || len(node.Arguments) != 2 || node.ResultType.Slice == nil || !compilerTypes.Equal(node.ResultType.Slice.Element, node.Element) || node.OperandType.InlineList == nil && node.OperandType.Slice == nil && node.OperandType.List == nil || !supportedGeneratedTypeWithState(node.OperandType, state) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic()
		}
		var element compilerTypes.Type
		if node.OperandType.InlineList != nil {
			element = node.OperandType.InlineList.Element
		} else if node.OperandType.Slice != nil {
			element = node.OperandType.Slice.Element
		} else {
			element = node.OperandType.List.Element
		}
		if !compilerTypes.Equal(node.Element, element) {
			return unknownExpressionDiagnostic()
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic()
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
	return unknownExpressionDiagnostic()
}

func findValueFitsResult(value, result compilerTypes.Type) bool {
	if value.Union == nil {
		return compilerTypes.ContainsUnionMember(result, value)
	}
	members := compilerTypes.UnionMembers(value)
	for index := 0; index < members.Len(); index++ {
		if member, _ := members.At(index); !compilerTypes.ContainsUnionMember(result, member) {
			return false
		}
	}
	return true
}

func renderCollectionConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.ListNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic()
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, &compilerTypes.Heap, state)
		if heapErr != nil {
			return "", heapErr
		}
		return "hex_list_new_" + listSuffix(node.ResultType) + "(" + heap + ")", nil
	case checker.DictNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic()
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, &compilerTypes.Heap, state)
		if heapErr != nil {
			return "", heapErr
		}
		return "hex_dict_new_" + dictSuffix(node.ResultType) + "(" + heap + ")", nil
	}
	return "", unknownExpressionDiagnostic()
}

func renderCollectionExpression(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.IndexExpression:
		place, placeErr := checkedPlaceMetadata(node, state)
		if placeErr != nil {
			return "", placeErr
		}
		receiver, receiverErr := renderHoistedReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		index, indexErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if indexErr != nil {
			return "", indexErr
		}
		if node.OperandType.Slice != nil {
			return "*" + sliceAtHelper(node.OperandType) + "(" + receiver + ", (size_t)(" + index + "))", nil
		}
		if node.OperandType.List != nil {
			if place.writable {
				return "*hex_list_at_mut_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + index + "))", nil
			}
			return "*hex_list_at_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + index + "))", nil
		}
		if node.OperandType.InlineList != nil {
			accessor := "hex_list_inline_at_"
			if place.writable {
				accessor = "hex_list_inline_at_mut_"
			}
			return "*" + accessor + listSuffix(node.OperandType) + "(&(" + receiver + "), (size_t)(" + index + "))", nil
		}
		return "", unknownExpressionDiagnostic()
	case checker.InlineListLiteralExpression:
		if node.ResultType.InlineList != nil {
			elements := make([]string, len(node.Arguments))
			for index, element := range node.Arguments {
				rendered, elementErr := renderHoistedOperand(&node.Arguments[index].Node, element, state)
				if elementErr != nil {
					return "", elementErr
				}
				elements[index] = rendered
			}
			initializer := fmt.Sprintf(".length = %d, .version = 0", len(elements))
			if len(elements) > 0 {
				initializer += ", .data = {" + strings.Join(elements, ", ") + "}"
			}
			return "(" + node.ResultType.CName + "){ " + initializer + " }", nil
		}
		return "", unknownExpressionDiagnostic()
	case checker.CollectionMethodCallExpression:
		switch node.Name {
		case "pointer":
			if node.Operand == nil || node.OperandType.Slice == nil {
				return "", unknownExpressionDiagnostic()
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			// A Slice is a value whose data pointer is already the address;
			// no allocation or copy occurs, and an empty Slice's null data
			// pointer is exactly the Nil result.
			return "(" + receiver + ").data", nil
		case "length":
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
			if node.Operand == nil || node.OperandType.List == nil && node.OperandType.InlineList == nil {
				return "", unknownExpressionDiagnostic()
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			suffix := listSuffix(node.OperandType)
			owner := specdata.TypeList
			receiverArgument := receiver
			if node.OperandType.InlineList != nil {
				owner = specdata.TypeInlineList
				receiverArgument = "&( " + receiver + " )"
			}
			symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(owner), node.Name, suffix)
			if symbolErr != nil {
				return "", symbolErr
			}
			switch node.Name {
			case "push":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic()
				}
				value, valueErr := renderOperandWithState(node.Arguments[0], state)
				if valueErr != nil {
					return "", valueErr
				}
				return symbol + "(" + receiverArgument + ", " + value + ")", nil
			case "clear":
				return symbol + "(" + receiverArgument + ")", nil
			case "pop":
				return symbol + "(" + receiverArgument + ")", nil
			}
		case "free":
			if node.Operand == nil || len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic()
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
				symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeList), "free", listSuffix(node.OperandType))
				if symbolErr != nil {
					return "", symbolErr
				}
				return symbol + "(" + heap + ", " + receiver + ")", nil
			}
			if node.OperandType.Dict != nil {
				symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeDict), "free", dictSuffix(node.OperandType))
				if symbolErr != nil {
					return "", symbolErr
				}
				return symbol + "(" + heap + ", " + receiver + ")", nil
			}
			return "", unknownExpressionDiagnostic()
		case "insert", "get", "find", "contains", "remove":
			if node.Operand == nil || node.OperandType.Dict == nil {
				return "", unknownExpressionDiagnostic()
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			suffix := dictSuffix(node.OperandType)
			switch node.Name {
			case "insert":
				if len(node.Arguments) != 2 {
					return "", unknownExpressionDiagnostic()
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				value, valueErr := renderOperandWithState(node.Arguments[1], state)
				if valueErr != nil {
					return "", valueErr
				}
				symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeDict), "insert", suffix)
				if symbolErr != nil {
					return "", symbolErr
				}
				return symbol + "(" + receiver + ", " + key + ", " + value + ")", nil
			case "get", "remove":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic()
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeDict), node.Name, suffix)
				if symbolErr != nil {
					return "", symbolErr
				}
				return symbol + "(" + receiver + ", " + key + ")", nil
			case "find":
				return renderDictFindExpression(node, state)
			case "contains":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic()
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeDict), "contains", suffix)
				if symbolErr != nil {
					return "", symbolErr
				}
				return symbol + "(" + receiver + ", " + key + ")", nil
			}
		}
		return "", unknownExpressionDiagnostic()
	case checker.CollectionSliceExpression:
		if node.Operand == nil || len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic()
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
		if node.OperandType.Slice != nil {
			return sliceSliceHelper(node.OperandType) + "(" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
		}
		// A List re-slice dispatches the recorded slice or
		// mut_slice method; the registry owns the emitted symbol and the
		// receiver type supplies the per-specialization suffix.
		if node.OperandType.List != nil || node.OperandType.InlineList != nil {
			operation := "slice"
			if node.ResultType.Slice != nil && node.ResultType.Slice.Writable {
				operation = "mut_slice"
			}
			owner := specdata.TypeList
			receiverArgument := receiver
			if node.OperandType.InlineList != nil {
				owner = specdata.TypeInlineList
				receiverArgument = "&( " + receiver + " )"
			}
			symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(owner), operation, listSuffix(node.OperandType))
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + receiverArgument + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
		}
		return "", unknownExpressionDiagnostic()
	}
	return "", unknownExpressionDiagnostic()
}

// collectionsNeedSlice reports whether any reachable List
// specialization has a matching Slice in either access mode, which is the
// only reason the component header names the slice component.
func collectionsNeedSlice(lists *generatedListState, slices *generatedSliceState) bool {
	if slices == nil {
		return false
	}
	if lists != nil {
		for _, list := range lists.order {
			element := compilerTypes.Type{}
			if list.List != nil {
				element = list.List.Element
			} else if list.InlineList != nil {
				element = list.InlineList.Element
			}
			if matchingSlice(slices, element, false) != (compilerTypes.Type{}) ||
				matchingSlice(slices, element, true) != (compilerTypes.Type{}) {
				return true
			}
		}
	}
	return false
}
