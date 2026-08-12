package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedViewState records the view types and the array slice helpers that
// need definitions, in deterministic order.
type generatedViewState struct {
	views []compilerTypes.Type
	seen  map[*compilerTypes.ViewInfo]bool
}

// discoverGeneratedViews walks every type reachable from the program and
// collects the distinct view types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedViews(program checker.Program) (*generatedViewState, error) {
	state := &generatedViewState{seen: make(map[*compilerTypes.ViewInfo]bool)}
	seenObjects := make(map[*compilerTypes.ObjectType]bool)
	seenADTs := make(map[*compilerTypes.AdtType]bool)
	var walkType func(compilerTypes.Type) error
	var walkOperand func(checker.Operand) error
	var walkExpression func(checker.Expression) error
	var walkStatements func([]checker.Statement) error
	walkType = func(typ compilerTypes.Type) error {
		if typ.View != nil {
			if !state.seen[typ.View] {
				state.seen[typ.View] = true
				state.views = append(state.views, typ)
			}
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
	sort.SliceStable(state.views, func(left, right int) bool {
		return state.views[left].CName < state.views[right].CName
	})
	return state, nil
}

// writeViewDefinitions emits one struct per view type plus its element
// accessor and slice helpers. Views are small pointer-plus-count values, so
// the helpers take and return them by value.
func writeViewDefinitions(result *strings.Builder, views *generatedViewState) {
	if views == nil {
		return
	}
	for _, view := range views.views {
		element := view.View.Element
		suffix := strings.TrimPrefix(view.CName, "hex_view_")
		fmt.Fprintf(result, "\ntypedef struct %s {\n    const %s *data;\n    size_t length;\n} %s;\n", view.CName, pointerSpelling(element), view.CName)
		fmt.Fprintf(result, "static inline const %s *hex_view_at_%s(%s view, size_t index) {\n", pointerSpelling(element), suffix, view.CName)
		writeViewBoundsGuard(result)
		result.WriteString("    return &view.data[index];\n}\n")
		fmt.Fprintf(result, "static inline %s hex_view_slice_%s(%s view, uint64_t start, uint64_t end) {\n", view.CName, suffix, view.CName)
		writeViewSliceGuard(result)
		fmt.Fprintf(result, "    return (%s){&view.data[start], end - start};\n}\n", view.CName)
	}
}

func writeViewBoundsGuard(result *strings.Builder) {
	result.WriteString("    if (index >= view.length) {\n")
	result.WriteString("        fputs(\"[Runtime Error] view index out of bounds\\n\", stderr);\n        abort();\n    }\n")
}

func writeViewSliceGuard(result *strings.Builder) {
	result.WriteString("    if (!(start <= end && end <= view.length)) {\n")
	result.WriteString("        fputs(\"[Runtime Error] view slice bounds out of range\\n\", stderr);\n        abort();\n    }\n")
}

// writeArraySliceHelper emits the slice helper for one array type, which
// bounds-checks the range against the compile-time length and returns a view
// into the array's inline storage.
func writeArraySliceHelper(result *strings.Builder, array compilerTypes.Type, view compilerTypes.Type) {
	length := array.Array.Length
	suffix := arrayAccessorSuffix(array)
	fmt.Fprintf(result, "\nstatic inline %s hex_array_slice_%s(const %s *array, uint64_t start, uint64_t end) {\n", view.CName, suffix, array.CName)
	fmt.Fprintf(result, "    if (!(start <= end && end <= UINT64_C(%d))) {\n", length)
	result.WriteString("        fputs(\"[Runtime Error] array slice bounds out of range\\n\", stderr);\n        abort();\n    }\n")
	fmt.Fprintf(result, "    return (%s){&array->data[start], end - start};\n}\n", view.CName)
}

// ensureViewUInt8 adds the byte view type to the view state if missing; the
// String helpers always reference it.
func ensureViewUInt8(state *generatedViewState) {
	if state == nil {
		return
	}
	for _, view := range state.views {
		if view.CName == "hex_view_UInt8" {
			return
		}
	}
	view := compilerTypes.NewEnvironment().ViewType(compilerTypes.UInt8)
	state.seen[view.View] = true
	state.views = append(state.views, view)
}

// viewCName returns the C struct name of the view type over one element.
func viewCName(element compilerTypes.Type) string {
	return "hex_view_" + compilerTypes.SanitizeIdentifier(element.Name)
}
