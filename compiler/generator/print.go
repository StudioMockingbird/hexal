package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The `print` builtin: arguments evaluate exactly once from left to right
// into temporaries, then the generated helpers append each one in source
// order to the call's own builder, which commits to standard output exactly
// once. All helpers are length-aware, never use source bytes as format
// strings, and no fragment can reach standard output before the commit.

type generatedPrintState struct {
	used  bool
	types []compilerTypes.Type
	// needsNested records which discovered types actually need their
	// hex_print_nested_X helper defined: every aggregate (used through that
	// same helper even as a bare top-level argument, see
	// printFormatsDirectlyAtTopLevel below) and any type -- aggregate or
	// not -- reached as a structural descendant of some print argument
	// (an object member, ADT payload field, or collection element), since
	// an aggregate's own nested helper recurses into its members through
	// exactly that call. A leaf type (text, a scalar, Error)
	// that only ever appears as a bare top-level argument needs no nested
	// helper at all: writePrintArgument formats it directly.
	needsNested map[string]bool
}

// printFormatsDirectlyAtTopLevel mirrors writePrintArgument's own explicit
// cases exactly: every other type falls to that function's default branch,
// which calls the type's nested helper even for a top-level argument.
func printFormatsDirectlyAtTopLevel(typ compilerTypes.Type) bool {
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool), compilerTypes.Equal(typ, compilerTypes.Nil),
		compilerTypes.IsSignedInteger(typ), compilerTypes.IsUnsignedInteger(typ),
		compilerTypes.Equal(typ, compilerTypes.Float32), compilerTypes.Equal(typ, compilerTypes.Float64),
		compilerTypes.IsText(typ), compilerTypes.IsError(typ):
		return true
	}
	return false
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
		case compilerTypes.IsText(typ), compilerTypes.IsError(typ),
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
		case typ.Slice != nil:
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
	state.needsNested = make(map[string]bool)
	markNested := func(typ compilerTypes.Type) {
		if typ != (compilerTypes.Type{}) {
			state.needsNested[typ.Name] = true
		}
	}
	visitor := &programVisitor{
		// The structural descent from a print argument's type reuses the
		// walker's type walk, keeping print's argument-scoped criteria.
		// walkTypeTree visits in pre-order (the argument's own root type
		// first, then its structural descendants), so the first callback
		// per argument is exactly that argument's own top-level type.
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.PrintExpression {
				state.used = true
				for _, argument := range node.Arguments {
					first := true
					visit := func(typ compilerTypes.Type) error {
						if first {
							first = false
							if !printFormatsDirectlyAtTopLevel(typ) {
								markNested(typ)
							}
						} else {
							markNested(typ)
						}
						return addType(typ)
					}
					if err := walkTypeTree(argument.Type, visit); err != nil {
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

// Print fragment models carry only values Go already decided: canonical C
// names, labels with their emitted byte lengths, and argument expressions
// chosen by type predicates. Templates carry presentation only.
type printHeaderTextModel struct {
	HeaderType string
}

type printNestedForwardModel struct {
	Names []string
}

type printNestedLeafModel struct {
	CName string
}

type printNestedErrorKindModel struct {
	CName      string
	HeaderType string
}

type printNestedBitsModel struct {
	CName string
	Bits  int
}

type printNestedObjectEmptyModel struct {
	CName   string
	Name    string
	TextLen int
}

// printMemberFragment is one already-lowered member line: the separator
// flag, the label with its emitted byte length, and the nested call's
// target helper and argument expression.
type printMemberFragment struct {
	Separator   bool
	Label       string
	LabelLen    int
	NestedCName string
	Arg         string
}

type printNestedObjectModel struct {
	CName   string
	Name    string
	NameLen int
	Members []printMemberFragment
}

type printVariantFragment struct {
	Tag      string
	Label    string
	LabelLen int
	Payload  []printMemberFragment
}

type printNestedAdtModel struct {
	CName    string
	Variants []printVariantFragment
}

type printNestedArrayModel struct {
	CName        string
	Length       uint64
	ElementCName string
	ElementArg   string
}

type printNestedSequenceModel struct {
	CName        string
	ElementCName string
	ElementArg   string
}

type printNestedDictModel struct {
	CName      string
	KeyCName   string
	KeyArg     string
	ValueCName string
	ValueArg   string
}

type printTempModel struct {
	Indent string
	Decl   string
	Value  string
}

type printTransactionModel struct {
	Indent string
	Buffer string
}

type printArgumentTextModel struct {
	Indent string
	Buffer string
	Name   string
}

type printArgumentBitsModel struct {
	Indent string
	Buffer string
	Name   string
	Bits   int
}

type printArgumentNestedModel struct {
	Indent string
	Buffer string
	CName  string
	Arg    string
}

type printArgumentNilModel struct {
	Indent string
	Buffer string
}

// writePrintDefinitions emits the shared print runtime and the per-concrete
// nested aggregate helpers.
func writePrintDefinitions(result *strings.Builder, state *generatedPrintState, tags *tagRegistry) error {
	if state == nil || !state.used {
		return nil
	}
	errorUsedByPrint := false
	errorNestedNeeded := false
	for _, typ := range state.types {
		if compilerTypes.IsError(typ) {
			errorUsedByPrint = true
			errorNestedNeeded = state.needsNested[typ.Name]
			break
		}
	}
	if errorUsedByPrint {
		if err := renderInto(result, "module.h", "print_error_direct", printHeaderTextModel{HeaderType: compilerTypes.ErrorHeaderText.CName}); err != nil {
			return err
		}
	}
	if errorNestedNeeded {
		if err := renderInto(result, "module.h", "print_error_nested", printHeaderTextModel{HeaderType: compilerTypes.ErrorHeaderText.CName}); err != nil {
			return err
		}
	}
	// A container helper calls the helpers of its element and member
	// types, which may follow it in discovery order, so every nested
	// helper is declared before any definition; the generated C must
	// compile warning-free as-is.
	var forwardDeclarations []string
	for _, typ := range state.types {
		if state.needsNested[typ.Name] {
			forwardDeclarations = append(forwardDeclarations, typ.CName)
		}
	}
	if err := renderInto(result, "module.h", "print_nested_forward", printNestedForwardModel{Names: forwardDeclarations}); err != nil {
		return err
	}
	for _, typ := range state.types {
		if !state.needsNested[typ.Name] {
			continue
		}
		if err := writePrintNestedHelper(result, typ, tags); err != nil {
			return err
		}
	}
	return nil
}

// printNestedAddress renders the argument expression for a nested helper
// call: pointer-semantic values (List, Dict, String) pass their pointer
// directly, every other type passes its address.
func printNestedAddress(typ compilerTypes.Type, expression string) string {
	if typ.List != nil || typ.Dict != nil || compilerTypes.IsString(typ) {
		return expression
	}
	return "&(" + expression + ")"
}

// writePrintNestedHelper emits one nested-context helper per concrete type.
// Every helper takes `const void *` and casts internally, so aggregate call
// sites can pass member and element addresses uniformly.
func writePrintNestedHelper(result *strings.Builder, typ compilerTypes.Type, tags *tagRegistry) error {
	block, model, matched := printNestedFragment(typ, tags)
	if !matched {
		return nil
	}
	return renderInto(result, "module.h", block, model)
}

// printNestedFragment selects the template block and decided model for one
// nested helper. A type no case matches contributes no helper, so an
// aggregate kind without a nested form stays silent exactly as before.
func printNestedFragment(typ compilerTypes.Type, tags *tagRegistry) (string, any, bool) {
	switch {
	case compilerTypes.IsString(typ):
		return "print_nested_string", printNestedLeafModel{CName: typ.CName}, true
	case compilerTypes.IsInlineString(typ):
		return "print_nested_inline_string", printNestedLeafModel{CName: typ.CName}, true
	case compilerTypes.IsError(typ):
		return "print_nested_error", printNestedLeafModel{CName: typ.CName}, true
	case compilerTypes.IsErrorKind(typ):
		return "print_nested_error_kind", printNestedErrorKindModel{CName: typ.CName, HeaderType: compilerTypes.ErrorHeaderText.CName}, true
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		return "print_nested_bool", printNestedLeafModel{CName: typ.CName}, true
	case compilerTypes.Equal(typ, compilerTypes.Nil):
		return "print_nested_nil", printNestedLeafModel{CName: typ.CName}, true
	case compilerTypes.IsSignedInteger(typ):
		return "print_nested_int", printNestedBitsModel{CName: typ.CName, Bits: typ.Bits}, true
	case compilerTypes.IsUnsignedInteger(typ):
		if compilerTypes.Equal(typ, compilerTypes.SizeType) {
			return "print_nested_size", printNestedLeafModel{CName: typ.CName}, true
		}
		return "print_nested_uint", printNestedBitsModel{CName: typ.CName, Bits: typ.Bits}, true
	case compilerTypes.Equal(typ, compilerTypes.Float32):
		return "print_nested_float32", printNestedLeafModel{CName: typ.CName}, true
	case compilerTypes.Equal(typ, compilerTypes.Float64):
		return "print_nested_float64", printNestedLeafModel{CName: typ.CName}, true
	case typ.Object != nil:
		if len(typ.Object.Members) == 0 {
			// An empty struct's private byte member is not part of its
			// surface, so its print output is the bare "Name {}" form with
			// no member list and no interior padding.
			return "print_nested_object_empty", printNestedObjectEmptyModel{
				CName:   typ.CName,
				Name:    typ.Name,
				TextLen: len(typ.Name) + 3,
			}, true
		}
		members := make([]printMemberFragment, 0, len(typ.Object.Members))
		for index, member := range typ.Object.Members {
			members = append(members, printMemberFragment{
				Separator:   index > 0,
				Label:       member.Name + " = ",
				LabelLen:    len(member.Name) + 3,
				NestedCName: member.Type.CName,
				Arg:         printNestedAddress(member.Type, "v->"+privateCName(memberName, member.Name, "")),
			})
		}
		return "print_nested_object", printNestedObjectModel{
			CName:   typ.CName,
			Name:    typ.Name,
			NameLen: len(typ.Name) + 3,
			Members: members,
		}, true
	case typ.Adt != nil:
		adt := typ.Adt
		variants := make([]printVariantFragment, 0, len(adt.Variants))
		for variantIndex, variant := range adt.Variants {
			fragment := printVariantFragment{
				Tag:      tags.adtVariantTag(adt, variantIndex),
				Label:    adt.Name + "." + variant.Name,
				LabelLen: len(adt.Name) + 1 + len(variant.Name),
			}
			for index, member := range variant.Payload {
				fragment.Payload = append(fragment.Payload, printMemberFragment{
					Separator:   index > 0,
					Label:       member.Name + " = ",
					LabelLen:    len(member.Name) + 3,
					NestedCName: member.Type.CName,
					Arg:         printNestedAddress(member.Type, "v->payload."+variant.Name+".hex_m_"+member.Name),
				})
			}
			variants = append(variants, fragment)
		}
		return "print_nested_adt", printNestedAdtModel{CName: typ.CName, Variants: variants}, true
	case typ.Array != nil:
		element := typ.Array.Element
		return "print_nested_array", printNestedArrayModel{
			CName:        typ.CName,
			Length:       typ.Array.Length,
			ElementCName: element.CName,
			ElementArg:   printNestedAddress(element, "v->data[index]"),
		}, true
	case typ.Slice != nil:
		element := typ.Slice.Element
		return "print_nested_sequence", printNestedSequenceModel{
			CName:        typ.CName,
			ElementCName: element.CName,
			ElementArg:   printNestedAddress(element, "v->data[index]"),
		}, true
	case typ.List != nil:
		element := typ.List.Element
		return "print_nested_sequence", printNestedSequenceModel{
			CName:        typ.CName,
			ElementCName: element.CName,
			ElementArg:   printNestedAddress(element, "v->data[index]"),
		}, true
	case typ.Dict != nil:
		key := typ.Dict.Key
		valueType := typ.Dict.Value
		return "print_nested_dict", printNestedDictModel{
			CName:      typ.CName,
			KeyCName:   key.CName,
			KeyArg:     printNestedAddress(key, "v->buckets[index].key"),
			ValueCName: valueType.CName,
			ValueArg:   printNestedAddress(valueType, "v->buckets[index].value"),
		}, true
	}
	return "", nil, false
}

// renderPrintStatement lowers one print call: each argument evaluates once
// into a temporary in source order, then the helpers append it to the call's
// own builder, which commits once and releases any grown storage.
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
		declarationText := declaration(argument.Type, name, false)
		if err := renderInto(body, "module.c", "print_statement_temp", printTempModel{Indent: indent, Decl: declarationText, Value: rendered}); err != nil {
			return err
		}
		names = append(names, name)
	}
	types := make([]compilerTypes.Type, 0, len(node.Arguments))
	for _, argument := range node.Arguments {
		types = append(types, argument.Type)
	}
	return writePrintTransaction(body, types, names, state, indent)
}

// writePrintTransaction emits one complete print transaction over already
// evaluated argument temporaries: one builder, every argument appended in
// source order, one commit, one release.
func writePrintTransaction(body *strings.Builder, types []compilerTypes.Type, names []string, state *expressionValidation, indent string) error {
	state.printCounter++
	buffer := fmt.Sprintf("hex_print_out_%d", state.printCounter)
	if err := renderInto(body, "module.c", "print_txn_begin", printTransactionModel{Indent: indent, Buffer: buffer}); err != nil {
		return err
	}
	for index, typ := range types {
		if err := writePrintArgument(body, typ, names[index], "&"+buffer, indent); err != nil {
			return err
		}
	}
	return renderInto(body, "module.c", "print_txn_end", printTransactionModel{Indent: indent, Buffer: buffer})
}

// writePrintArgument emits one argument's direct (top-level) print form,
// appending to the transaction's builder.
func writePrintArgument(body *strings.Builder, typ compilerTypes.Type, name, buffer, indent string) error {
	var block string
	var model any
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		block = "print_arg_bool"
		model = printArgumentTextModel{Indent: indent, Buffer: buffer, Name: name}
	case compilerTypes.Equal(typ, compilerTypes.Nil):
		block = "print_arg_nil"
		model = printArgumentNilModel{Indent: indent, Buffer: buffer}
	case compilerTypes.IsSignedInteger(typ):
		if compilerTypes.Equal(typ, compilerTypes.SizeType) {
			block = "print_arg_size"
			model = printArgumentTextModel{Indent: indent, Buffer: buffer, Name: name}
		} else {
			block = "print_arg_int"
			model = printArgumentBitsModel{Indent: indent, Buffer: buffer, Name: name, Bits: typ.Bits}
		}
	case compilerTypes.IsUnsignedInteger(typ):
		if compilerTypes.Equal(typ, compilerTypes.SizeType) {
			block = "print_arg_size"
			model = printArgumentTextModel{Indent: indent, Buffer: buffer, Name: name}
		} else {
			block = "print_arg_uint"
			model = printArgumentBitsModel{Indent: indent, Buffer: buffer, Name: name, Bits: typ.Bits}
		}
	case compilerTypes.Equal(typ, compilerTypes.Float32):
		block = "print_arg_float32"
		model = printArgumentTextModel{Indent: indent, Buffer: buffer, Name: name}
	case compilerTypes.Equal(typ, compilerTypes.Float64):
		block = "print_arg_float64"
		model = printArgumentTextModel{Indent: indent, Buffer: buffer, Name: name}
	case compilerTypes.IsString(typ):
		block = "print_arg_string"
		model = printArgumentTextModel{Indent: indent, Buffer: buffer, Name: name}
	case compilerTypes.IsInlineString(typ):
		block = "print_arg_inline_string"
		model = printArgumentTextModel{Indent: indent, Buffer: buffer, Name: name}
	case compilerTypes.IsError(typ):
		block = "print_arg_error"
		model = printArgumentTextModel{Indent: indent, Buffer: buffer, Name: name}
	default:
		// Aggregates use their nested helper at the top level too;
		// pointer-semantic values pass their pointer directly.
		block = "print_arg_nested"
		model = printArgumentNestedModel{Indent: indent, Buffer: buffer, CName: typ.CName, Arg: printNestedAddress(typ, name)}
	}
	return renderInto(body, "module.c", block, model)
}

// renderDeferredPrint renders a deferred print action at cleanup time. The
// arguments were captured at registration; the formatting and the single
// commit happen here, so a deferred call is one transaction like any other.
func renderDeferredPrint(body *strings.Builder, action checker.DeferredAction, state *expressionValidation, indent string) error {
	if action.Call == nil || action.Call.Node.Kind != checker.PrintExpression {
		return unknownExpressionDiagnostic("deferred print action without a checked print call")
	}
	node := action.Call.Node
	captured, ok := state.captures[action.Call]
	if !ok || len(captured) != len(node.Arguments) {
		return unknownExpressionDiagnostic("deferred print action without captured arguments")
	}
	types := make([]compilerTypes.Type, 0, len(node.Arguments))
	for _, argument := range node.Arguments {
		types = append(types, argument.Type)
	}
	return writePrintTransaction(body, types, captured, state, indent)
}
