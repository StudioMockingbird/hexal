package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0024 equality and ordering lowering: one equality helper per concrete
// compared type, recursive member-wise bodies, bytewise String and Strand
// comparison, and no storage memcmp anywhere.

// generatedEqualityState records the types needing equality helpers and the
// String/Strand compare helpers, in dependency order.
type generatedEqualityState struct {
	order         []compilerTypes.Type
	seenObjects   map[*compilerTypes.ObjectType]bool
	seenADTs      map[*compilerTypes.AdtType]bool
	seenArrays    map[*compilerTypes.ArrayInfo]bool
	seenViews     map[*compilerTypes.ViewInfo]bool
	seenLists     map[*compilerTypes.ListInfo]bool
	seenUnions    map[*compilerTypes.UnionInfo]bool
	needString    bool
	needStrand    bool
	compareNeed   bool
	seenFileModes bool
}

// discoverEqualityTypes walks the program collecting the compared types and,
// recursively, every nested type their helpers must compare. Types are
// collected dependency-first so emission order is valid.
func discoverEqualityTypes(program checker.Program) (*generatedEqualityState, error) {
	state := &generatedEqualityState{
		seenObjects: make(map[*compilerTypes.ObjectType]bool),
		seenADTs:    make(map[*compilerTypes.AdtType]bool),
		seenArrays:  make(map[*compilerTypes.ArrayInfo]bool),
		seenViews:   make(map[*compilerTypes.ViewInfo]bool),
		seenLists:   make(map[*compilerTypes.ListInfo]bool),
		seenUnions:  make(map[*compilerTypes.UnionInfo]bool),
	}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			switch {
			case typ.Object != nil:
				if state.seenObjects[typ.Object] {
					return nil
				}
				state.seenObjects[typ.Object] = true
				state.order = append(state.order, typ)
			case typ.Adt != nil:
				if state.seenADTs[typ.Adt] {
					return nil
				}
				state.seenADTs[typ.Adt] = true
				state.order = append(state.order, typ)
			case typ.Array != nil:
				if state.seenArrays[typ.Array] {
					return nil
				}
				state.seenArrays[typ.Array] = true
				state.order = append(state.order, typ)
			case typ.View != nil:
				if state.seenViews[typ.View] {
					return nil
				}
				state.seenViews[typ.View] = true
				state.order = append(state.order, typ)
			case typ.List != nil:
				if state.seenLists[typ.List] {
					return nil
				}
				state.seenLists[typ.List] = true
				state.order = append(state.order, typ)
			case typ.Union != nil:
				if state.seenUnions[typ.Union] {
					return nil
				}
				state.seenUnions[typ.Union] = true
				state.order = append(state.order, typ)
			case compilerTypes.IsString(typ):
				state.needString = true
			case compilerTypes.IsStrand(typ):
				state.needStrand = true
			case compilerTypes.IsFileMode(typ):
				if state.seenFileModes {
					return nil
				}
				state.seenFileModes = true
				state.order = append(state.order, typ)
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.StringCompareExpression {
				state.compareNeed = true
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}
func equalityHelperName(typ compilerTypes.Type) string {
	return "hex_equal_" + typ.CName
}

// writeEqualityDefinitions emits one equality helper per collected type plus
// the String and Strand byte-compare helpers. It must run after every struct
// definition because the helper bodies reference the concrete C types.
func writeEqualityDefinitions(result *strings.Builder, state *generatedEqualityState) {
	if state == nil {
		return
	}
	// The byte-compare helpers come first: per-type helpers reference them
	// for String and Strand members.
	if state.needString {
		writeByteCompareHelpers(result, "hex_string", "left->byte_length", "left->data")
	}
	if state.needStrand {
		// RFC 0044: a Strand is 32 zero-padded inline bytes with no NUL in
		// its payload, so full-width comparison is exact.
		result.WriteString("\nstatic bool hex_equal_hex_strand(hex_strand left, hex_strand right) {\n")
		result.WriteString("    for (size_t index = 0; index < 32; index++) {\n")
		result.WriteString("        if (left.data[index] != right.data[index]) {\n            return false;\n        }\n    }\n")
		result.WriteString("    return true;\n}\n")
	}
	for _, typ := range state.order {
		if typ.Union != nil {
			if unionSupportsEquality(typ) {
				writeUnionEquality(result, typ)
			}
			continue
		}
		writeEqualityHelper(result, typ)
	}
	if state.compareNeed {
		writeOrderingHelpers(result, state)
	}
}

func writeByteCompareHelpers(result *strings.Builder, handle, lengthSpelling, dataSpelling string) {
	parameter := handle + " left, " + handle + " right"
	rightLength := strings.Replace(lengthSpelling, "left.", "right.", 1)
	if handle == "hex_string" {
		parameter = "const hex_string *left, const hex_string *right"
		rightLength = "right->byte_length"
	}
	rightData := strings.Replace(dataSpelling, "left.", "right.", 1)
	if handle == "hex_string" {
		rightData = "right->data"
	}
	fmt.Fprintf(result, "\nstatic bool hex_equal_%s(%s) {\n", handle, parameter)
	fmt.Fprintf(result, "    if (%s != %s) {\n        return false;\n    }\n", lengthSpelling, rightLength)
	fmt.Fprintf(result, "    for (size_t index = 0; index < %s; index++) {\n", lengthSpelling)
	fmt.Fprintf(result, "        if (%s[index] != %s[index]) {\n            return false;\n        }\n    }\n", dataSpelling, rightData)
	result.WriteString("    return true;\n}\n")
}

func writeOrderingHelpers(result *strings.Builder, state *generatedEqualityState) {
	if state.needString {
		writeStringCompare(result)
	}
	if state.needStrand {
		writeStrandCompare(result)
	}
}

// writeStringCompare emits the lexicographic unsigned-byte compare for
// String handles.
func writeStringCompare(result *strings.Builder) {
	result.WriteString("\nstatic int hex_compare_hex_string(const hex_string *left, const hex_string *right) {\n")
	result.WriteString("    size_t limit = left->byte_length < right->byte_length ? left->byte_length : right->byte_length;\n")
	result.WriteString("    for (size_t index = 0; index < limit; index++) {\n")
	result.WriteString("        if (left->data[index] != right->data[index]) {\n")
	result.WriteString("            return left->data[index] < right->data[index] ? -1 : 1;\n")
	result.WriteString("        }\n")
	result.WriteString("    }\n")
	result.WriteString("    if (left->byte_length < right->byte_length) return -1;\n")
	result.WriteString("    if (left->byte_length > right->byte_length) return 1;\n")
	result.WriteString("    return 0;\n}\n")
}

// writeStrandCompare emits the lexicographic unsigned-byte compare for Strand
// values. Payloads are NUL-free and zero-padded, so the first zero byte in
// either side bounds the meaningful region.
func writeStrandCompare(result *strings.Builder) {
	result.WriteString("\nstatic int hex_compare_hex_strand(hex_strand left, hex_strand right) {\n")
	result.WriteString("    for (size_t index = 0; index < 32; index++) {\n")
	result.WriteString("        if (left.data[index] == 0 || right.data[index] == 0) {\n")
	result.WriteString("            if (left.data[index] != right.data[index]) {\n")
	result.WriteString("                return left.data[index] == 0 ? -1 : 1;\n")
	result.WriteString("            }\n")
	result.WriteString("            return 0;\n")
	result.WriteString("        }\n")
	result.WriteString("        if (left.data[index] != right.data[index]) {\n")
	result.WriteString("            return left.data[index] < right.data[index] ? -1 : 1;\n")
	result.WriteString("        }\n")
	result.WriteString("    }\n")
	result.WriteString("    return 0;\n}\n")
}

// writeEqualityHelper emits the equality helper body for one compared type.
// The body compares declared structure in order and returns false at the
// first unequal component; nothing reads padding, capacity, or backing
// addresses.
func writeEqualityHelper(result *strings.Builder, typ compilerTypes.Type) {
	var body strings.Builder
	writeEqualityComparisons(&body, "(*left)", "(*right)", typ, "    ")
	// The helper never mutates its operands, so the parameters carry const;
	// call sites pass const-qualified bindings and a non-const parameter
	// would discard the qualifier under -Werror.
	parameter := "const " + typ.CName + " *left, const " + typ.CName + " *right"
	fmt.Fprintf(result, "\nstatic bool %s(%s) {\n", equalityHelperName(typ), parameter)
	if body.Len() > 0 {
		result.WriteString(body.String())
	}
	result.WriteString("    return true;\n}\n")
}

// writeEqualityComparisons emits statements comparing the value spelled left
// against right of the given type, returning false at the first inequality.
func writeEqualityComparisons(body *strings.Builder, left, right string, typ compilerTypes.Type, indent string) {
	switch {
	case typ.Union != nil:
		fmt.Fprintf(body, "%sif (%s.tag != %s.tag) return false;\n", indent, left, right)
		fmt.Fprintf(body, "%sswitch (%s.tag) {\n", indent, left)
		for index, member := range typ.Union.Members {
			fmt.Fprintf(body, "%scase %s:\n", indent, unionTagName(typ, index))
			if compilerTypes.IsNil(member) {
				fmt.Fprintf(body, "%s    return true;\n", indent)
				continue
			}
			writeEqualityComparisons(body, left+".payload.member_"+fmt.Sprint(index), right+".payload.member_"+fmt.Sprint(index), member, indent+"    ")
			fmt.Fprintf(body, "%s    return true;\n", indent)
		}
		fmt.Fprintf(body, "%s}\n", indent)
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			writeEqualityComparisons(body, left+"."+PrivateCName(MemberName, member.Name, ""), right+"."+PrivateCName(MemberName, member.Name, ""), member.Type, indent)
		}
	case typ.Adt != nil:
		fmt.Fprintf(body, "%sif (%s.tag != %s.tag) return false;\n", indent, left, right)
		fmt.Fprintf(body, "%sswitch (%s.tag) {\n", indent, left)
		for index, variant := range typ.Adt.Variants {
			fmt.Fprintf(body, "%scase %s:\n", indent, adtTagName(typ.Adt, index))
			if len(variant.Payload) == 0 {
				fmt.Fprintf(body, "%s    return true;\n", indent)
				continue
			}
			for _, member := range variant.Payload {
				writeEqualityComparisons(body, left+".payload."+compilerTypes.SanitizeIdentifier(variant.Name)+"."+PrivateCName(MemberName, member.Name, ""), right+".payload."+compilerTypes.SanitizeIdentifier(variant.Name)+"."+PrivateCName(MemberName, member.Name, ""), member.Type, indent+"    ")
			}
			fmt.Fprintf(body, "%s    return true;\n", indent)
		}
		fmt.Fprintf(body, "%s}\n", indent)
	case typ.Array != nil:
		for index := uint64(0); index < typ.Array.Length; index++ {
			writeEqualityComparisons(body, left+".data["+fmt.Sprint(index)+"]", right+".data["+fmt.Sprint(index)+"]", typ.Array.Element, indent)
		}
	case typ.View != nil:
		fmt.Fprintf(body, "%sif (%s.length != %s.length) return false;\n", indent, left, right)
		fmt.Fprintf(body, "%sfor (size_t index = 0; index < %s.length; index++) {\n", indent, left)
		writeEqualityComparisons(body, left+".data[index]", right+".data[index]", typ.View.Element, indent+"    ")
		fmt.Fprintf(body, "%s}\n", indent)
	case typ.List != nil:
		fmt.Fprintf(body, "%sif (%s.length != %s.length) return false;\n", indent, left, right)
		fmt.Fprintf(body, "%sfor (size_t index = 0; index < %s.length; index++) {\n", indent, left)
		writeEqualityComparisons(body, left+".data[index]", right+".data[index]", typ.List.Element, indent+"    ")
		fmt.Fprintf(body, "%s}\n", indent)
	case compilerTypes.IsString(typ):
		fmt.Fprintf(body, "%sif (!hex_equal_hex_string(%s, %s)) return false;\n", indent, left, right)
	case compilerTypes.IsStrand(typ):
		fmt.Fprintf(body, "%sif (!hex_equal_hex_strand(%s, %s)) return false;\n", indent, left, right)
	case typ.Element != nil:
		fmt.Fprintf(body, "%sif (!(%s == %s)) return false;\n", indent, left, right)
	default:
		fmt.Fprintf(body, "%sif (!(%s == %s)) return false;\n", indent, left, right)
	}
}
