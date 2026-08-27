package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// Equality and ordering lowering emits one equality helper per concrete
// compared type with recursive member-wise bodies, and compares Strings
// through memcmp; no storage memcmp is used for values with padding, NaNs,
// pointers, inactive union bytes, or capacity state.

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
func discoverEqualityTypes(program checker.Program) *generatedEqualityState {
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
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.DeepEqualityExpression:
				if compilerTypes.IsString(node.OperandType) {
					state.needString = true
				}
			case checker.StringCompareExpression:
				if compilerTypes.IsString(node.OperandType) {
					state.compareNeed = true
				}
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	for _, typ := range state.order {
		if equalityTypeContainsString(typ, make(map[string]bool)) {
			state.needString = true
			break
		}
	}
	return state
}

// equalityTypeContainsString reports whether a generated equality helper for
// typ recursively needs the String component. Pointer identity and
// non-equality-capable aggregates stop the walk because their pointees are not
// compared.
func equalityTypeContainsString(typ compilerTypes.Type, seen map[string]bool) bool {
	if compilerTypes.IsString(typ) {
		return true
	}
	if typ.Element != nil || typ.Dict != nil {
		return false
	}
	key := typ.CanonicalKey
	if key == "" {
		key = typ.CName + "|" + typ.Name
	}
	if seen[key] {
		return false
	}
	seen[key] = true
	switch {
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if equalityTypeContainsString(member.Type, seen) {
				return true
			}
		}
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if equalityTypeContainsString(member.Type, seen) {
					return true
				}
			}
		}
	case typ.Union != nil:
		for _, member := range typ.Union.Members {
			if equalityTypeContainsString(member, seen) {
				return true
			}
		}
	case typ.NullableBase != nil:
		return false
	case typ.Array != nil:
		return equalityTypeContainsString(typ.Array.Element, seen)
	case typ.View != nil:
		return equalityTypeContainsString(typ.View.Element, seen)
	case typ.List != nil:
		return equalityTypeContainsString(typ.List.Element, seen)
	}
	return false
}
func equalityHelperName(typ compilerTypes.Type) string {
	return "hex_equal_" + typ.CName
}

// writeEqualityDefinitions emits one equality helper per collected type. It
// must run after every struct definition because the helper bodies reference
// the concrete C types.
func writeEqualityDefinitions(result *strings.Builder, state *generatedEqualityState, tags *tagRegistry) {
	if state == nil {
		return
	}
	for _, typ := range state.order {
		if isProgramOwnedEqualityType(typ) {
			continue
		}
		if typ.Union != nil {
			if unionSupportsEquality(typ) {
				writeUnionEquality(result, typ, tags)
			}
			continue
		}
		writeEqualityHelper(result, typ, tags)
	}
}

// writeEqualityHelper emits the equality helper body for one compared type.
// The body compares declared structure in order and returns false at the
// first unequal component; nothing reads padding, capacity, or backing
// addresses.
func writeEqualityHelper(result *strings.Builder, typ compilerTypes.Type, tags *tagRegistry) {
	var body strings.Builder
	writeEqualityComparisons(&body, "(*left)", "(*right)", typ, "    ", tags)
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

// equalityOperand adapts a raw field-access expression -- as spelled by a
// parent aggregate reading one of its members -- into the expression
// writeEqualityComparisons (or hex_equal_hex_string) expects to receive for
// a value of the given type. A stored handle (List, Dict, Mutex) is spelled
// as a pointer in its parent, exactly like a source binding of that type,
// but writeEqualityComparisons's own List/Dict cases below read their
// operand's .length/.data as a value; dereferencing here bridges that gap
// regardless of how deep the member is nested. String is also stored as a
// pointer, but its dedicated case calls hex_equal_hex_string with the
// pointer directly, so it passes through unchanged. Every other member is
// already spelled as a value in its parent aggregate.
func equalityOperand(expr string, typ compilerTypes.Type) string {
	if compilerTypes.IsList(typ) || compilerTypes.IsDict(typ) || compilerTypes.IsMutex(typ) {
		return "(*(" + expr + "))"
	}
	return expr
}

// writeEqualityComparisons emits statements comparing the value spelled left
// against right of the given type, returning false at the first inequality.
func writeEqualityComparisons(body *strings.Builder, left, right string, typ compilerTypes.Type, indent string, tags *tagRegistry) {
	switch {
	case typ.Union != nil:
		fmt.Fprintf(body, "%sif (%s.tag != %s.tag) return false;\n", indent, left, right)
		fmt.Fprintf(body, "%sswitch (%s.tag) {\n", indent, left)
		for _, member := range typ.Union.Members {
			field := tags.unionPayloadField(member)
			fmt.Fprintf(body, "%scase %s:\n", indent, tags.unionMemberTag(member))
			if compilerTypes.IsNil(member) {
				fmt.Fprintf(body, "%s    return true;\n", indent)
				continue
			}
			memberLeft := equalityOperand(left+".payload."+field, member)
			memberRight := equalityOperand(right+".payload."+field, member)
			writeEqualityComparisons(body, memberLeft, memberRight, member, indent+"    ", tags)
			fmt.Fprintf(body, "%s    return true;\n", indent)
		}
		// hex_tag is one enum shared by every ADT and union tag in the whole
		// program, so a switch exhaustive over this union's own members is
		// still missing every other type's tag as far as -Wswitch can tell;
		// default is unreachable in valid checked code.
		fmt.Fprintf(body, "%sdefault:\n%s    abort();\n", indent, indent)
		fmt.Fprintf(body, "%s}\n", indent)
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			field := privateCName(memberName, member.Name, "")
			memberLeft := equalityOperand(left+"."+field, member.Type)
			memberRight := equalityOperand(right+"."+field, member.Type)
			writeEqualityComparisons(body, memberLeft, memberRight, member.Type, indent, tags)
		}
	case typ.Adt != nil:
		fmt.Fprintf(body, "%sif (%s.tag != %s.tag) return false;\n", indent, left, right)
		fmt.Fprintf(body, "%sswitch (%s.tag) {\n", indent, left)
		for index, variant := range typ.Adt.Variants {
			fmt.Fprintf(body, "%scase %s:\n", indent, tags.adtVariantTag(typ.Adt, index))
			if len(variant.Payload) == 0 {
				fmt.Fprintf(body, "%s    return true;\n", indent)
				continue
			}
			for _, member := range variant.Payload {
				field := ".payload." + compilerTypes.SanitizeIdentifier(variant.Name) + "." + privateCName(memberName, member.Name, "")
				memberLeft := equalityOperand(left+field, member.Type)
				memberRight := equalityOperand(right+field, member.Type)
				writeEqualityComparisons(body, memberLeft, memberRight, member.Type, indent+"    ", tags)
			}
			fmt.Fprintf(body, "%s    return true;\n", indent)
		}
		// hex_tag is one enum shared by every ADT and union tag in the whole
		// program, so a switch exhaustive over this ADT's own variants is
		// still missing every other type's tag as far as -Wswitch can tell;
		// default is unreachable in valid checked code.
		fmt.Fprintf(body, "%sdefault:\n%s    abort();\n", indent, indent)
		fmt.Fprintf(body, "%s}\n", indent)
	case typ.Array != nil:
		for index := uint64(0); index < typ.Array.Length; index++ {
			field := ".data[" + fmt.Sprint(index) + "]"
			elementLeft := equalityOperand(left+field, typ.Array.Element)
			elementRight := equalityOperand(right+field, typ.Array.Element)
			writeEqualityComparisons(body, elementLeft, elementRight, typ.Array.Element, indent, tags)
		}
	case typ.View != nil:
		fmt.Fprintf(body, "%sif (%s.length != %s.length) return false;\n", indent, left, right)
		fmt.Fprintf(body, "%sfor (size_t index = 0; index < %s.length; index++) {\n", indent, left)
		elementLeft := equalityOperand(left+".data[index]", typ.View.Element)
		elementRight := equalityOperand(right+".data[index]", typ.View.Element)
		writeEqualityComparisons(body, elementLeft, elementRight, typ.View.Element, indent+"    ", tags)
		fmt.Fprintf(body, "%s}\n", indent)
	case typ.List != nil:
		fmt.Fprintf(body, "%sif (%s.length != %s.length) return false;\n", indent, left, right)
		fmt.Fprintf(body, "%sfor (size_t index = 0; index < %s.length; index++) {\n", indent, left)
		elementLeft := equalityOperand(left+".data[index]", typ.List.Element)
		elementRight := equalityOperand(right+".data[index]", typ.List.Element)
		writeEqualityComparisons(body, elementLeft, elementRight, typ.List.Element, indent+"    ", tags)
		fmt.Fprintf(body, "%s}\n", indent)
	case compilerTypes.IsString(typ):
		fmt.Fprintf(body, "%sif (!hex_equal_hex_string(%s, %s)) return false;\n", indent, left, right)
	case compilerTypes.IsStrand(typ):
		// A Strand's NUL-free payload and mandatory zero-filled tail make the
		// complete 32-byte representation canonical, so the whole value
		// compares with one direct memcmp.
		fmt.Fprintf(body, "%sif (memcmp(%s.data, %s.data, 32) != 0) return false;\n", indent, left, right)
	case typ.Element != nil:
		fmt.Fprintf(body, "%sif (!(%s == %s)) return false;\n", indent, left, right)
	default:
		fmt.Fprintf(body, "%sif (!(%s == %s)) return false;\n", indent, left, right)
	}
}
