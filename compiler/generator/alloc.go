package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// heapHelpers records the element types allocated through Heap so the
// generator can emit exactly one checked allocate helper per type.
type heapHelpers struct {
	elements []compilerTypes.Type
	seen     map[string]bool
	required bool // base helpers needed even without typed allocations (Strings)
}

func discoverHeapHelpers(program checker.Program) (*heapHelpers, error) {
	state := &heapHelpers{seen: make(map[string]bool)}
	var walkOperand func(checker.Operand) error
	var walkExpression func(checker.Expression) error
	var walkStatements func([]checker.Statement) error
	walkExpression = func(node checker.Expression) error {
		if node.Kind == checker.HeapAllocateExpression {
			if node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) {
				return unknownExpressionDiagnostic("heap allocation without a complete checked element type")
			}
			if !state.seen[node.Element.Name] {
				state.seen[node.Element.Name] = true
				state.elements = append(state.elements, node.Element)
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
	walkOperand = func(operand checker.Operand) error {
		return walkExpression(operand.Node)
	}
	walkStatements = func(statements []checker.Statement) error {
		for _, statement := range statements {
			switch statement := statement.(type) {
			case checker.Declaration:
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
			case checker.Assignment:
				if err := walkOperand(statement.Target); err != nil {
					return err
				}
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
			case checker.CallStatement:
				if err := walkOperand(statement.Call); err != nil {
					return err
				}
			case checker.ReturnStatement:
				if statement.Value != nil {
					if err := walkOperand(*statement.Value); err != nil {
						return err
					}
				}
			case checker.DeferStatement:
				if err := walkOperand(statement.Expression); err != nil {
					return err
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
				if err := walkStatements(statement.Else); err != nil {
					return err
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
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.MethodDeclaration:
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walkStatements(program.Statements); err != nil {
		return nil, err
	}
	for _, function := range program.SpecializedFunctions {
		if err := walkStatements(function.Body); err != nil {
			return nil, err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := walkStatements(method.Body); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func writeHeapDefinitions(result *strings.Builder, state *heapHelpers) {
	if state == nil || len(state.elements) == 0 && !state.required {
		return
	}
	result.WriteString("\n")
	result.WriteString("typedef struct sw_heap {\n    uintptr_t identity;\n} sw_heap;\n\n")
	result.WriteString("#define SW_HEAP_DEFAULT 0\n\n")
	// The header's last size_t slot is the offset marker every free reads
	// at (pointer - sizeof(size_t)). The live flag must not share that
	// region: the marker write at base + offset - 8 would clobber it.
	result.WriteString("typedef struct sw_heap_header {\n    uintptr_t allocator;\n    size_t size;\n    size_t offset;\n    bool live;\n    size_t marker;\n} sw_heap_header;\n\n")
	result.WriteString("static void *sw_heap_raw_allocate(uintptr_t allocator, size_t size, size_t align) {\n")
	result.WriteString("    if (size == 0 || (align & (align - 1)) != 0) {\n")
	result.WriteString("        fputs(\"[Runtime Error] allocation size is not representable\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    size_t offset = (sizeof(sw_heap_header) + align - 1) & ~(align - 1);\n")
	result.WriteString("    size_t total = offset + size;\n")
	result.WriteString("    if (total < size) {\n")
	result.WriteString("        fputs(\"[Runtime Error] allocation size is not representable\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    unsigned char *base = (unsigned char *)malloc(total);\n")
	result.WriteString("    if (base == NULL) {\n")
	result.WriteString("        fputs(\"[Runtime Error] heap allocation failed\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    sw_heap_header *header = (sw_heap_header *)base;\n")
	result.WriteString("    header->allocator = allocator;\n")
	result.WriteString("    header->size = size;\n")
	result.WriteString("    header->offset = offset;\n")
	result.WriteString("    header->live = true;\n")
	result.WriteString("    *((size_t *)(base + offset - sizeof(size_t))) = offset;\n")
	result.WriteString("    return base + offset;\n")
	result.WriteString("}\n\n")
	result.WriteString("static void sw_heap_free(void *pointer, uintptr_t allocator) {\n")
	result.WriteString("    if (pointer == NULL) {\n")
	result.WriteString("        fputs(\"[Runtime Error] double deallocation\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    size_t offset = *((size_t *)((unsigned char *)pointer - sizeof(size_t)));\n")
	result.WriteString("    sw_heap_header *header = (sw_heap_header *)((unsigned char *)pointer - offset);\n")
	result.WriteString("    if (header->allocator != allocator) {\n")
	result.WriteString("        fputs(\"[Runtime Error] deallocation used the wrong allocator\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    if (!header->live) {\n")
	result.WriteString("        fputs(\"[Runtime Error] double deallocation\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    header->live = false;\n")
	result.WriteString("    free(header);\n")
	result.WriteString("}\n")
	for _, element := range state.elements {
		helper := "sw_heap_allocate_" + compilerTypes.SanitizeIdentifier(element.Name)
		fmt.Fprintf(result, "\nstatic %s %s(sw_heap h, %s initial) {\n", typeSpelling(compilerTypes.MutPtrType(element)), helper, typeSpelling(element))
		fmt.Fprintf(result, "    %s *pointer = sw_heap_raw_allocate(h.identity, sizeof(%s), _Alignof(%s));\n", typeSpelling(element), typeSpelling(element), typeSpelling(element))
		fmt.Fprintf(result, "    *pointer = initial;\n")
		fmt.Fprintf(result, "    return pointer;\n}\n")
	}
}

func heapAllocateHelper(element compilerTypes.Type) string {
	return "sw_heap_allocate_" + compilerTypes.SanitizeIdentifier(element.Name)
}

func renderHeapAllocate(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 1 || node.Element == (compilerTypes.Type{}) {
		return "", unknownExpressionDiagnostic("heap allocation has invalid checked metadata")
	}
	receiver, err := renderExpressionExpectedWithState(*node.Operand, compilerTypes.Heap, true, state)
	if err != nil {
		return "", err
	}
	initial, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return heapAllocateHelper(node.Element) + "(" + receiver + ", " + initial + ")", nil
}

func renderHeapFree(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 1 {
		return "", unknownExpressionDiagnostic("heap free has invalid checked metadata")
	}
	receiver, err := renderExpressionExpectedWithState(*node.Operand, compilerTypes.Heap, true, state)
	if err != nil {
		return "", err
	}
	value, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return "sw_heap_free(" + value + ", " + receiver + ".identity)", nil
}
