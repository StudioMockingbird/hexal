package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0024 equality and ordering lowering: one equality helper per concrete
// compared type, recursive member-wise bodies, and String comparison through
// memcmp; no storage memcmp is used for values with padding, NaNs, pointers,
// inactive union bytes, or capacity state.

// generatedEqualityState records the types needing equality helpers, in
// dependency order.
type generatedEqualityState struct {
	order       []compilerTypes.Type
	seenObjects map[*compilerTypes.ObjectType]bool
	seenADTs    map[*compilerTypes.AdtType]bool
	seenArrays  map[*compilerTypes.ArrayInfo]bool
	seenViews   map[*compilerTypes.ViewInfo]bool
	seenLists   map[*compilerTypes.ListInfo]bool
	seenUnions  map[*compilerTypes.UnionInfo]bool
	needString  bool
	compareNeed bool
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

// writeEqualityDefinitions emits one equality helper per collected type. It
// must run after every struct definition because the helper bodies reference
// the concrete C types.
func writeEqualityDefinitions(result *strings.Builder, state *generatedEqualityState) {
	if state == nil {
		return
	}
	// The String equality helper comes first: per-type helpers reference it
	// for String members. Strand members compare directly with memcmp.
	if state.needString {
		writeStringEqualityHelper(result)
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

// writeStringEqualityHelper emits the String equality helper: equal byte
// lengths first, then one memcmp over the shared length. A zero-length
// String returns equal without calling memcmp on a possibly invalid pointer.
func writeStringEqualityHelper(result *strings.Builder) {
	result.WriteString("\nstatic bool hex_equal_hex_string(const hex_string *left, const hex_string *right) {\n")
	result.WriteString("    if (left->byte_length != right->byte_length) {\n        return false;\n    }\n")
	result.WriteString("    if (left->byte_length != 0) {\n")
	result.WriteString("        if (memcmp(left->data, right->data, left->byte_length) != 0) {\n            return false;\n        }\n")
	result.WriteString("    }\n")
	result.WriteString("    return true;\n}\n")
}

func writeOrderingHelpers(result *strings.Builder, state *generatedEqualityState) {
	if state.needString {
		writeStringCompare(result)
	}
}

// writeStringCompare emits the lexicographic unsigned-byte compare for
// String handles: memcmp over the shorter nonzero byte length, then the
// length comparison when the byte sequences are equal. The sign of the
// memcmp result is the ordering result.
func writeStringCompare(result *strings.Builder) {
	result.WriteString("\nstatic int hex_compare_hex_string(const hex_string *left, const hex_string *right) {\n")
	result.WriteString("    size_t limit = left->byte_length < right->byte_length ? left->byte_length : right->byte_length;\n")
	result.WriteString("    if (limit != 0) {\n")
	result.WriteString("        int result = memcmp(left->data, right->data, limit);\n")
	result.WriteString("        if (result != 0) {\n            return result;\n        }\n")
	result.WriteString("    }\n")
	result.WriteString("    if (left->byte_length < right->byte_length) return -1;\n")
	result.WriteString("    if (left->byte_length > right->byte_length) return 1;\n")
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
		// RFC 0069 Amendment 2: a Strand's NUL-free payload and mandatory
		// zero-filled tail make the complete 32-byte representation
		// canonical, so the whole value compares with one direct memcmp.
		fmt.Fprintf(body, "%sif (memcmp(%s.data, %s.data, 32) != 0) return false;\n", indent, left, right)
	case typ.Element != nil:
		fmt.Fprintf(body, "%sif (!(%s == %s)) return false;\n", indent, left, right)
	default:
		fmt.Fprintf(body, "%sif (!(%s == %s)) return false;\n", indent, left, right)
	}
}
