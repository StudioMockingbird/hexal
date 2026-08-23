package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The `print` builtin: arguments evaluate exactly once from left to right
// into temporaries, then the generated helpers write each one in source
// order. All helpers are length-aware, never use source bytes as format
// strings, and check every write result.

type generatedPrintState struct {
	used  bool
	types []compilerTypes.Type
}

// discoverGeneratedPrint collects the argument types print needs helpers
// for, including every recursively nested aggregate type. Only types
// reachable from print arguments are collected: print's helpers must exist
// exactly when a print argument could reference them, and not otherwise.
func discoverGeneratedPrint(program checker.Program) (*generatedPrintState, error) {
	state := &generatedPrintState{}
	seen := make(map[string]bool)
	addType := func(typ compilerTypes.Type) error {
		if typ == (compilerTypes.Type{}) {
			return nil
		}
		key := typ.Name
		switch {
		case compilerTypes.IsString(typ), compilerTypes.IsStrand(typ), compilerTypes.IsRune(typ), compilerTypes.IsError(typ),
			compilerTypes.IsInteger(typ), compilerTypes.IsFloat(typ), compilerTypes.Equal(typ, compilerTypes.Bool):
			if !seen[key] {
				seen[key] = true
				state.types = append(state.types, typ)
			}
		case typ.Object != nil:
			if !seen[key] {
				seen[key] = true
				state.types = append(state.types, typ)
			}
		case typ.Adt != nil:
			if !seen[key] {
				seen[key] = true
				state.types = append(state.types, typ)
			}
		case typ.Array != nil:
			if !seen[key] {
				seen[key] = true
				state.types = append(state.types, typ)
			}
		case typ.View != nil:
			if !seen[key] {
				seen[key] = true
				state.types = append(state.types, typ)
			}
		case typ.List != nil:
			if !seen[key] {
				seen[key] = true
				state.types = append(state.types, typ)
			}
		case typ.Dict != nil:
			if !seen[key] {
				seen[key] = true
				state.types = append(state.types, typ)
			}
		}
		return nil
	}
	visitor := &programVisitor{
		// The structural descent from a print argument's type reuses the
		// walker's type walk, keeping print's argument-scoped criteria.
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.PrintExpression {
				state.used = true
				for _, argument := range node.Arguments {
					if err := walkTypeTree(argument.Type, addType); err != nil {
						return err
					}
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

// writePrintDefinitions emits the shared print runtime and the per-concrete
// nested aggregate helpers.
func writePrintDefinitions(result *strings.Builder, state *generatedPrintState, tags *tagRegistry) {
	if state == nil || !state.used {
		return
	}
	errorUsedByPrint := false
	for _, typ := range state.types {
		if compilerTypes.IsError(typ) {
			errorUsedByPrint = true
			break
		}
	}
	if errorUsedByPrint {
		result.WriteString("static void hex_print_error_direct(const hex_t_Error *value) {\n")
		result.WriteString("    hex_print_text(value->hex_m_file->data, value->hex_m_file->byte_length);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\":\", 1);\n    hex_print_size(value->hex_m_line);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\":\", 1);\n    hex_print_size(value->hex_m_column);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\": \", 2);\n")
		result.WriteString("    hex_print_text(value->hex_m_header.data, value->hex_m_header.length);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\": \", 2);\n")
		result.WriteString("    hex_print_text(value->hex_m_message->data, value->hex_m_message->byte_length);\n}\n")
		result.WriteString("static void hex_print_error_nested(const hex_t_Error *value) {\n")
		result.WriteString("    hex_print_text((const uint8_t *)\"Error { file = \", 15);\n    hex_print_quoted_text(value->hex_m_file->data, value->hex_m_file->byte_length);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\", line = \", 9);\n    hex_print_size(value->hex_m_line);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\", column = \", 11);\n    hex_print_size(value->hex_m_column);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\", header = \", 11);\n    hex_print_quoted_text(value->hex_m_header.data, value->hex_m_header.length);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\", message = \", 12);\n    hex_print_quoted_text(value->hex_m_message->data, value->hex_m_message->byte_length);\n")
		result.WriteString("    hex_print_text((const uint8_t *)\" }\", 2);\n}\n")
	}
	for _, typ := range state.types {
		// A container helper calls the helpers of its element and member
		// types, which may follow it in discovery order, so every nested
		// helper is declared before any definition; the generated C must
		// compile warning-free as-is.
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value);\n", typ.CName)
	}
	for _, typ := range state.types {
		writePrintNestedHelper(result, typ, tags)
	}
}

// writePrintNestedHelper emits one nested-context helper per concrete type.
// Every helper takes `const void *` and casts internally, so aggregate call
// sites can pass member and element addresses uniformly.

// printNestedAddress renders the argument expression for a nested helper
// call: pointer-semantic values (List, Dict, String) pass their pointer
// directly, every other type passes its address.
func printNestedAddress(typ compilerTypes.Type, expression string) string {
	if typ.List != nil || typ.Dict != nil || compilerTypes.IsString(typ) {
		return expression
	}
	return "&(" + expression + ")"
}

func writePrintNestedHelper(result *strings.Builder, typ compilerTypes.Type, tags *tagRegistry) {
	switch {
	case compilerTypes.IsString(typ):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    const hex_string *text = value;\n    hex_print_quoted_text(text->data, text->byte_length);\n}\n", typ.CName)
	case compilerTypes.IsStrand(typ):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_strand text = *(const hex_strand *)value;\n    size_t length = 0;\n    while (length < 32 && text.data[length] != 0) {\n        length++;\n    }\n    hex_print_quoted_text(text.data, length);\n}\n", typ.CName)
	case compilerTypes.IsRune(typ):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_print_quoted_rune(*(const uint32_t *)value);\n}\n", typ.CName)
	case compilerTypes.IsError(typ):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_print_error_nested(value);\n}\n", typ.CName)
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_print_bool(*(const bool *)value);\n}\n", typ.CName)
	case compilerTypes.Equal(typ, compilerTypes.Nil):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    (void)value;\n    hex_print_nil();\n}\n", typ.CName)
	case compilerTypes.IsSignedInteger(typ):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_print_int%d(*(const int%d_t *)value);\n}\n", typ.CName, typ.Bits, typ.Bits)
	case compilerTypes.IsUnsignedInteger(typ) && !compilerTypes.IsRune(typ):
		if compilerTypes.Equal(typ, compilerTypes.SizeType) {
			fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_print_size(*(const size_t *)value);\n}\n", typ.CName)
		} else {
			fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_print_uint%d(*(const uint%d_t *)value);\n}\n", typ.CName, typ.Bits, typ.Bits)
		}
	case compilerTypes.Equal(typ, compilerTypes.Float32):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_print_float32(*(const float *)value);\n}\n", typ.CName)
	case compilerTypes.Equal(typ, compilerTypes.Float64):
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    hex_print_float64(*(const double *)value);\n}\n", typ.CName)
	case typ.Object != nil:
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    const %s *v = value;\n", typ.CName, typ.CName)
		fmt.Fprintf(result, "    hex_print_text((const uint8_t *)\"%s { \", %d);\n", typ.Name, len(typ.Name)+3)
		for index, member := range typ.Object.Members {
			if index > 0 {
				fmt.Fprintf(result, "    hex_print_text((const uint8_t *)\", \", 2);\n")
			}
			fmt.Fprintf(result, "    hex_print_text((const uint8_t *)\"%s = \", %d);\n", member.Name, len(member.Name)+3)
			fmt.Fprintf(result, "    hex_print_nested_%s(%s);\n", member.Type.CName, printNestedAddress(member.Type, "v->"+privateCName(memberName, member.Name, "")))
		}
		fmt.Fprintf(result, "    hex_print_text((const uint8_t *)\" }\", 2);\n}\n")
	case typ.Adt != nil:
		adt := typ.Adt
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    const %s *v = value;\n    switch (v->tag) {\n", typ.CName, typ.CName)
		for variantIndex, variant := range adt.Variants {
			fmt.Fprintf(result, "    case %s:\n", tags.adtVariantTag(adt, variantIndex))
			fmt.Fprintf(result, "        hex_print_text((const uint8_t *)\"%s.%s\", %d);\n", adt.Name, variant.Name, len(adt.Name)+1+len(variant.Name))
			if len(variant.Payload) > 0 {
				fmt.Fprintf(result, "        hex_print_text((const uint8_t *)\" { \", 3);\n")
				for index, member := range variant.Payload {
					if index > 0 {
						fmt.Fprintf(result, "        hex_print_text((const uint8_t *)\", \", 2);\n")
					}
					fmt.Fprintf(result, "        hex_print_text((const uint8_t *)\"%s = \", %d);\n", member.Name, len(member.Name)+3)
					fmt.Fprintf(result, "        hex_print_nested_%s(%s);\n", member.Type.CName, printNestedAddress(member.Type, "v->payload."+variant.Name+".hex_m_"+member.Name))
				}
				fmt.Fprintf(result, "        hex_print_text((const uint8_t *)\" }\", 2);\n")
			}
			fmt.Fprintf(result, "        break;\n")
		}
		fmt.Fprintf(result, "    default:\n        abort();\n    }\n}\n")
	case typ.Array != nil:
		element := typ.Array.Element
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    const %s *v = value;\n    hex_print_text((const uint8_t *)\"[\", 1);\n", typ.CName, typ.CName)
		fmt.Fprintf(result, "    for (size_t index = 0; index < %d; index++) {\n        if (index > 0) { hex_print_text((const uint8_t *)\", \", 2); }\n", typ.Array.Length)
		fmt.Fprintf(result, "        hex_print_nested_%s(%s);\n    }\n", element.CName, printNestedAddress(element, "v->data[index]"))
		fmt.Fprintf(result, "    hex_print_text((const uint8_t *)\"]\", 1);\n}\n")
	case typ.View != nil:
		element := typ.View.Element
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    const %s *v = value;\n    hex_print_text((const uint8_t *)\"[\", 1);\n", typ.CName, typ.CName)
		fmt.Fprintf(result, "    for (size_t index = 0; index < v->length; index++) {\n        if (index > 0) { hex_print_text((const uint8_t *)\", \", 2); }\n")
		fmt.Fprintf(result, "        hex_print_nested_%s(%s);\n    }\n", element.CName, printNestedAddress(element, "v->data[index]"))
		fmt.Fprintf(result, "    hex_print_text((const uint8_t *)\"]\", 1);\n}\n")
	case typ.List != nil:
		element := typ.List.Element
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    const %s *v = value;\n    hex_print_text((const uint8_t *)\"[\", 1);\n", typ.CName, typ.CName)
		fmt.Fprintf(result, "    for (size_t index = 0; index < v->length; index++) {\n        if (index > 0) { hex_print_text((const uint8_t *)\", \", 2); }\n")
		fmt.Fprintf(result, "        hex_print_nested_%s(%s);\n    }\n", element.CName, printNestedAddress(element, "v->data[index]"))
		fmt.Fprintf(result, "    hex_print_text((const uint8_t *)\"]\", 1);\n}\n")
	case typ.Dict != nil:
		key := typ.Dict.Key
		valueType := typ.Dict.Value
		fmt.Fprintf(result, "static void hex_print_nested_%s(const void *value) {\n    const %s *v = value;\n    hex_print_text((const uint8_t *)\"{\", 1);\n    bool first = true;\n", typ.CName, typ.CName)
		fmt.Fprintf(result, "    for (size_t index = 0; index < v->capacity; index++) {\n        if (!v->buckets[index].active) { continue; }\n        if (!first) { hex_print_text((const uint8_t *)\", \", 2); }\n        first = false;\n")
		fmt.Fprintf(result, "        hex_print_nested_%s(%s);\n", key.CName, printNestedAddress(key, "v->buckets[index].key"))
		fmt.Fprintf(result, "        hex_print_text((const uint8_t *)\": \", 2);\n")
		fmt.Fprintf(result, "        hex_print_nested_%s(%s);\n    }\n", valueType.CName, printNestedAddress(valueType, "v->buckets[index].value"))
		fmt.Fprintf(result, "    hex_print_text((const uint8_t *)\"}\", 1);\n}\n")
	}
}

// renderPrintStatement lowers one print call: each argument evaluates once
// into a temporary in source order, then the helpers write it.
func renderPrintStatement(body *strings.Builder, node checker.Expression, state *expressionValidation, indent string) error {
	if len(node.Arguments) == 0 {
		return unknownExpressionDiagnostic("print without arguments")
	}
	names := make([]string, 0, len(node.Arguments))
	for _, argument := range node.Arguments {
		rendered, err := renderOperandWithState(argument, state)
		if err != nil {
			return err
		}
		state.printCounter++
		name := fmt.Sprintf("hex_print_arg_%d", state.printCounter)
		fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(argument.Type, name, false), rendered)
		names = append(names, name)
	}
	for index, argument := range node.Arguments {
		if err := writePrintArgument(body, argument.Type, names[index], indent); err != nil {
			return err
		}
	}
	return nil
}

// writePrintArgument emits one argument's direct (top-level) print form.
func writePrintArgument(body *strings.Builder, typ compilerTypes.Type, name, indent string) error {
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		fmt.Fprintf(body, "%shex_print_bool(%s);\n", indent, name)
	case compilerTypes.Equal(typ, compilerTypes.Nil):
		fmt.Fprintf(body, "%shex_print_nil();\n", indent)
	case compilerTypes.IsSignedInteger(typ):
		width := typ.Bits
		if compilerTypes.Equal(typ, compilerTypes.SizeType) {
			fmt.Fprintf(body, "%shex_print_size(%s);\n", indent, name)
		} else {
			fmt.Fprintf(body, "%shex_print_int%d(%s);\n", indent, width, name)
		}
	case compilerTypes.IsUnsignedInteger(typ) && !compilerTypes.IsRune(typ):
		if compilerTypes.Equal(typ, compilerTypes.SizeType) {
			fmt.Fprintf(body, "%shex_print_size(%s);\n", indent, name)
		} else {
			fmt.Fprintf(body, "%shex_print_uint%d(%s);\n", indent, typ.Bits, name)
		}
	case compilerTypes.IsRune(typ):
		fmt.Fprintf(body, "%shex_print_rune(%s);\n", indent, name)
	case compilerTypes.Equal(typ, compilerTypes.Float32):
		fmt.Fprintf(body, "%shex_print_float32(%s);\n", indent, name)
	case compilerTypes.Equal(typ, compilerTypes.Float64):
		fmt.Fprintf(body, "%shex_print_float64(%s);\n", indent, name)
	case compilerTypes.IsString(typ):
		fmt.Fprintf(body, "%shex_print_text(%s->data, %s->byte_length);\n", indent, name, name)
	case compilerTypes.IsStrand(typ):
		// A Strand's logical payload ends at the first NUL byte of its
		// 32-byte inline storage.
		fmt.Fprintf(body, "%s{\n", indent)
		fmt.Fprintf(body, "%s    size_t length = 0;\n", indent)
		fmt.Fprintf(body, "%s    while (length < 32 && %s.data[length] != 0) {\n", indent, name)
		fmt.Fprintf(body, "%s        length++;\n", indent)
		fmt.Fprintf(body, "%s    }\n", indent)
		fmt.Fprintf(body, "%s    hex_print_text(%s.data, length);\n", indent, name)
		fmt.Fprintf(body, "%s}\n", indent)
	case compilerTypes.IsError(typ):
		fmt.Fprintf(body, "%shex_print_error_direct(%s);\n", indent, name)
	default:
		// Aggregates use their nested helper at the top level too;
		// pointer-semantic values pass their pointer directly.
		fmt.Fprintf(body, "%shex_print_nested_%s(%s);\n", indent, typ.CName, printNestedAddress(typ, name))
	}
	return nil
}

// renderDeferredPrint renders a deferred print action at cleanup time; the
// arguments were captured at registration, so only the writing helpers run.
func renderDeferredPrint(body *strings.Builder, action checker.DeferredAction, state *expressionValidation, indent string) error {
	if action.Call == nil || action.Call.Node.Kind != checker.PrintExpression {
		return unknownExpressionDiagnostic("deferred print action without a checked print call")
	}
	node := action.Call.Node
	captured, ok := state.captures[action.Call]
	if !ok || len(captured) != len(node.Arguments) {
		return unknownExpressionDiagnostic("deferred print action without captured arguments")
	}
	for index, argument := range node.Arguments {
		if err := writePrintArgument(body, argument.Type, captured[index], indent); err != nil {
			return err
		}
	}
	return nil
}
