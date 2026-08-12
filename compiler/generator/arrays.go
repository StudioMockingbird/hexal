package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedArrayState records the array types that need struct and element
// accessor definitions, in deterministic order.
type generatedArrayState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.ArrayInfo]bool
}

// discoverGeneratedArrays walks every type reachable from the program and
// collects the distinct array types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedArrays(program checker.Program) (*generatedArrayState, error) {
	state := &generatedArrayState{seen: make(map[*compilerTypes.ArrayInfo]bool)}
	seenObjects := make(map[*compilerTypes.ObjectType]bool)
	seenADTs := make(map[*compilerTypes.AdtType]bool)
	var walkType func(compilerTypes.Type) error
	var walkOperand func(checker.Operand) error
	var walkExpression func(checker.Expression) error
	var walkStatements func([]checker.Statement) error
	walkType = func(typ compilerTypes.Type) error {
		if typ.Array != nil {
			if !state.seen[typ.Array] {
				state.seen[typ.Array] = true
				state.order = append(state.order, typ)
			}
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

// writeArrayDefinitions emits one struct plus the two element accessor
// helpers per array type: a read helper over a const array and a write helper
// over a mutable array. Every element access runs the bounds guard first, so
// no invalid index can ever form a data pointer. When a view over the
// element type is used in the program, the array's slice helper is emitted
// after the view definitions so it can reference the view struct.
func writeArrayDefinitions(result *strings.Builder, arrays *generatedArrayState, views *generatedViewState) {
	if arrays == nil {
		return
	}
	for _, array := range arrays.order {
		element := array.Array.Element
		length := array.Array.Length
		fmt.Fprintf(result, "\ntypedef struct %s {\n    %s data[%d];\n} %s;\n", array.CName, pointerSpelling(element), length, array.CName)
		fmt.Fprintf(result, "static inline const %s *hex_array_at_%s(const %s *array, size_t index) {\n", pointerSpelling(element), arrayAccessorSuffix(array), array.CName)
		writeArrayBoundsGuard(result, length)
		result.WriteString("    return &array->data[index];\n}\n")
		fmt.Fprintf(result, "static inline %s *hex_array_at_mut_%s(%s *array, size_t index) {\n", pointerSpelling(element), arrayAccessorSuffix(array), array.CName)
		writeArrayBoundsGuard(result, length)
		result.WriteString("    return &array->data[index];\n}\n")
		if view := matchingView(views, element); view != (compilerTypes.Type{}) {
			writeArraySliceHelper(result, array, view)
		}
	}
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

func writeArrayBoundsGuard(result *strings.Builder, length uint64) {
	fmt.Fprintf(result, "    if (index >= UINT64_C(%d)) {\n", length)
	result.WriteString("        fputs(\"[Runtime Error] array index out of bounds\\n\", stderr);\n        abort();\n    }\n")
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
