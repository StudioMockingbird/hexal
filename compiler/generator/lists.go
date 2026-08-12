package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedListState records the list types that need header and helper
// definitions, in deterministic order.
type generatedListState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.ListInfo]bool
}

// discoverGeneratedLists walks every type reachable from the program and
// collects the distinct list types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedLists(program checker.Program) (*generatedListState, error) {
	state := &generatedListState{seen: make(map[*compilerTypes.ListInfo]bool)}
	seenObjects := make(map[*compilerTypes.ObjectType]bool)
	seenADTs := make(map[*compilerTypes.AdtType]bool)
	var walkType func(compilerTypes.Type) error
	var walkOperand func(checker.Operand) error
	var walkExpression func(checker.Expression) error
	var walkStatements func([]checker.Statement) error
	walkType = func(typ compilerTypes.Type) error {
		if typ.List != nil {
			if !state.seen[typ.List] {
				state.seen[typ.List] = true
				state.order = append(state.order, typ)
			}
			return walkType(typ.List.Element)
		}
		if typ.View != nil {
			return walkType(typ.View.Element)
		}
		if typ.Array != nil {
			return walkType(typ.Array.Element)
		}
		if typ.Union != nil {
			for _, member := range typ.Union.Members {
				if err := walkType(member); err != nil {
					return err
				}
			}
		}
		if typ.NullableBase != nil {
			return walkType(*typ.NullableBase)
		}
		if typ.Element != nil {
			return walkType(*typ.Element)
		}
		if typ.Signature != nil {
			for _, parameter := range typ.Signature.Parameters {
				if err := walkType(parameter); err != nil {
					return err
				}
			}
			if typ.Signature.Result != nil {
				return walkType(*typ.Signature.Result)
			}
		}
		if typ.Object != nil {
			if seenObjects[typ.Object] {
				return nil
			}
			seenObjects[typ.Object] = true
			for _, member := range typ.Object.Members {
				if err := walkType(member.Type); err != nil {
					return err
				}
			}
		}
		if typ.Adt != nil {
			if seenADTs[typ.Adt] {
				return nil
			}
			seenADTs[typ.Adt] = true
			for _, variant := range typ.Adt.Variants {
				for _, member := range variant.Payload {
					if err := walkType(member.Type); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	walkExpression = func(node checker.Expression) error {
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
		switch source.Kind {
		case checker.ObjectOperand:
			if source.Object != nil {
				for _, initializer := range source.Object.Initializers {
					if err := walkOperand(initializer.Source); err != nil {
						return err
					}
				}
			}
		case checker.VariableOperand, checker.ExpressionOperand:
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
				if err := walkType(statement.Type); err != nil {
					return err
				}
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
				if err := walkOperand(statement.Target); err != nil {
					return err
				}
			case checker.CallStatement:
				if err := walkExpression(statement.Call.Node); err != nil {
					return err
				}
			case checker.ReturnStatement:
				if statement.Value != nil {
					if err := walkOperand(*statement.Value); err != nil {
						return err
					}
				}
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
			}
		}
		return nil
	}
	for _, declaration := range program.TypeDeclarations {
		if err := walkType(declaration.Type); err != nil {
			return nil, err
		}
	}
	if err := walkStatements(program.Statements); err != nil {
		return nil, err
	}
	for _, function := range program.SpecializedFunctions {
		if err := walkType(function.Type); err != nil {
			return nil, err
		}
		for _, parameter := range function.Parameters {
			if err := walkType(parameter.Type); err != nil {
				return nil, err
			}
		}
		if err := walkStatements(function.Body); err != nil {
			return nil, err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := walkType(method.SelfType); err != nil {
			return nil, err
		}
		for _, parameter := range method.Parameters {
			if err := walkType(parameter.Type); err != nil {
				return nil, err
			}
		}
		if err := walkStatements(method.Body); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(state.order, func(left, right int) bool {
		return state.order[left].CName < state.order[right].CName
	})
	return state, nil
}

// listSuffix returns the element-derived suffix of one list type's C names.
func listSuffix(list compilerTypes.Type) string {
	return strings.TrimPrefix(list.CName, "hex_list_")
}

// writeListDefinitions emits one header struct plus the element helpers per
// list type. List<String> helpers copy Strings in, borrow them out, move
// them out on pop, and destroy them on set, clear, and free. Growth and
// destruction always go through the retained allocator identity.
func writeListDefinitions(result *strings.Builder, lists *generatedListState, views *generatedViewState) {
	if lists == nil {
		return
	}
	for _, list := range lists.order {
		element := list.List.Element
		elementSpelling := typeSpelling(element)
		stringElement := compilerTypes.IsString(element)
		suffix := listSuffix(list)
		fmt.Fprintf(result, "\ntypedef struct %s {\n    %s *data;\n    size_t length;\n    size_t capacity;\n    uintptr_t allocator;\n} %s;\n", list.CName, elementSpelling, list.CName)
		writeListGrowHelper(result, list, elementSpelling, stringElement)
		fmt.Fprintf(result, "static inline %s *hex_list_new_%s(hex_heap h) {\n", list.CName, suffix)
		result.WriteString("    " + list.CName + " *header = hex_heap_raw_allocate(h.identity, sizeof(" + list.CName + "), _Alignof(" + list.CName + "));\n")
		result.WriteString("    header->data = NULL;\n    header->length = 0;\n    header->capacity = 0;\n    header->allocator = h.identity;\n")
		fmt.Fprintf(result, "    return header;\n}\n")
		fmt.Fprintf(result, "static inline void hex_list_push_%s(%s *list, %s value) {\n", suffix, list.CName, elementSpelling)
		fmt.Fprintf(result, "    if (list->length == list->capacity) {\n        hex_list_grow_%s(list);\n    }\n", suffix)
		if stringElement {
			result.WriteString("    const hex_string *copy = hex_string_from_bytes((hex_heap){ list->allocator }, value->data, value->byte_length);\n")
			result.WriteString("    list->data[list->length++] = copy;\n")
		} else {
			result.WriteString("    list->data[list->length++] = value;\n")
		}
		result.WriteString("}\n")
		fmt.Fprintf(result, "static inline void hex_list_set_%s(%s *list, size_t index, %s value) {\n", suffix, list.CName, elementSpelling)
		writeListBoundsGuard(result, list)
		if stringElement {
			result.WriteString("    const hex_string *copy = hex_string_from_bytes((hex_heap){ list->allocator }, value->data, value->byte_length);\n")
			result.WriteString("    hex_string_free((hex_heap){ list->allocator }, list->data[index]);\n")
			result.WriteString("    list->data[index] = copy;\n")
		} else {
			result.WriteString("    list->data[index] = value;\n")
		}
		result.WriteString("}\n")
		fmt.Fprintf(result, "static inline %s hex_list_pop_%s(%s *list) {\n", elementSpelling, suffix, list.CName)
		fmt.Fprintf(result, "    if (list->length == 0) {\n        fputs(\"[Runtime Error] list index out of bounds\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    " + elementSpelling + " value = list->data[list->length - 1];\n")
		if stringElement {
			// Move-out: the slot is cleared without destroying the String.
			result.WriteString("    list->data[list->length - 1] = NULL;\n")
		}
		result.WriteString("    list->length--;\n    return value;\n}\n")
		fmt.Fprintf(result, "static inline void hex_list_clear_%s(%s *list) {\n", suffix, list.CName)
		if stringElement {
			result.WriteString("    for (size_t index = 0; index < list->length; index++) {\n")
			result.WriteString("        hex_string_free((hex_heap){ list->allocator }, list->data[index]);\n")
			result.WriteString("    }\n")
		}
		result.WriteString("    list->length = 0;\n}\n")
		// The at-read returns a pointer to the slot; for pointer elements the
		// element spelling already carries its pointee const, so no extra
		// leading const is added.
		atReadReturn := "const " + elementSpelling + " *"
		if strings.Contains(elementSpelling, "*") {
			atReadReturn = elementSpelling + " *"
		}
		fmt.Fprintf(result, "static inline %s hex_list_at_%s(const %s *list, size_t index) {\n", atReadReturn, suffix, list.CName)
		writeListBoundsGuard(result, list)
		result.WriteString("    return &list->data[index];\n}\n")
		fmt.Fprintf(result, "static inline %s *hex_list_at_mut_%s(%s *list, size_t index) {\n", elementSpelling, suffix, list.CName)
		writeListBoundsGuard(result, list)
		result.WriteString("    return &list->data[index];\n}\n")
		fmt.Fprintf(result, "static inline void hex_list_free_%s(hex_heap h, %s *list) {\n", suffix, list.CName)
		result.WriteString("    if (list == NULL || list->allocator != h.identity) {\n")
		result.WriteString("        fputs(\"[Runtime Error] deallocation used the wrong allocator\\n\", stderr);\n        abort();\n    }\n")
		if stringElement {
			result.WriteString("    for (size_t index = 0; index < list->length; index++) {\n")
			result.WriteString("        hex_string_free((hex_heap){ list->allocator }, list->data[index]);\n")
			result.WriteString("    }\n")
		}
		result.WriteString("    free(list->data);\n")
		result.WriteString("    free(list);\n}\n")
		if view := matchingView(views, element); view != (compilerTypes.Type{}) {
			fmt.Fprintf(result, "static inline %s hex_list_slice_%s(const %s *list, uint64_t start, uint64_t end) {\n", view.CName, suffix, list.CName)
			fmt.Fprintf(result, "    if (!(start <= end && end <= list->length)) {\n        fputs(\"[Runtime Error] list slice bounds out of range\\n\", stderr);\n        abort();\n    }\n")
			fmt.Fprintf(result, "    return (%s){&list->data[start], end - start};\n}\n", view.CName)
		}
	}
}

func writeListBoundsGuard(result *strings.Builder, list compilerTypes.Type) {
	fmt.Fprintf(result, "    if (index >= list->length) {\n        fputs(\"[Runtime Error] list index out of bounds\\n\", stderr);\n        abort();\n    }\n")
}

// writeListGrowHelper emits the growth helper for one list type: capacity
// doubling with overflow checks, a fresh region through the retained
// allocator, pointer-slot relocation (which never moves or destroys nested
// String objects), and release of the old region.
func writeListGrowHelper(result *strings.Builder, list compilerTypes.Type, elementSpelling string, stringElement bool) {
	suffix := listSuffix(list)
	fmt.Fprintf(result, "static inline void hex_list_grow_%s(%s *list) {\n", suffix, list.CName)
	result.WriteString("    uint64_t next = list->capacity == 0 ? 1 : list->capacity * 2;\n")
	result.WriteString("    if (next < list->capacity || next > SIZE_MAX / sizeof(" + elementSpelling + ")) {\n")
	result.WriteString("        fputs(\"[Runtime Error] list capacity is not representable\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    " + elementSpelling + " *region = hex_heap_raw_allocate(list->allocator, next * sizeof(" + elementSpelling + "), _Alignof(" + elementSpelling + "));\n")
	result.WriteString("    for (size_t index = 0; index < list->length; index++) {\n")
	result.WriteString("        region[index] = list->data[index];\n")
	result.WriteString("    }\n")
	result.WriteString("    free(list->data);\n")
	result.WriteString("    list->data = region;\n")
	result.WriteString("    list->capacity = next;\n}\n")
}
