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
	seenSlices  map[*compilerTypes.SliceInfo]bool
	seenLists   map[*compilerTypes.ListInfo]bool
	seenUnions  map[*compilerTypes.UnionInfo]bool
	needString  bool
	compareNeed bool
}

// equalityIneligibleElement reports whether element is a generation-checked
// handle with no language equality contract (File, TcpConnection,
// TcpListener): a raw C == on its struct is invalid, and the checker's own
// equalityAvailable already refuses to let Hexal source compare a
// collection over it, so no synthesized helper is ever called.
func equalityIneligibleElement(element compilerTypes.Type) bool {
	return compilerTypes.IsFile(element) || compilerTypes.IsTcpConnection(element) || compilerTypes.IsTcpListener(element) ||
		compilerTypes.IsProcess(element) || compilerTypes.IsPipe(element) || compilerTypes.IsSignals(element) ||
		compilerTypes.IsSignal(element)
}

// addComparedType registers typ as needing an equality helper if it is one
// of the aggregate kinds this pass owns and is not already recorded,
// mirroring the exact call sites that reference a compared type's helper by
// name: the top-level operand of a DeepEqualityExpression or
// UnionEqualityExpression (render.go, unions.go's writeUnionEquality), and,
// recursively, a List member of a union that supports equality -- the one
// case writeUnionEquality itself calls another type's helper directly
// (unions.go) rather than inlining the comparison. Every other structural
// descent (Object and Adt members, Array/Slice/List elements) is compared
// inline by writeEqualityComparisons and never calls a helper by name, so it
// is deliberately not walked here: a type only this pass's own inlining ever
// reaches needs no standalone definition, and emitting one anyway is
// exactly the over-emission this pass exists to stop.
func (state *generatedEqualityState) addComparedType(typ compilerTypes.Type) {
	switch {
	case typ.Object != nil:
		if state.seenObjects[typ.Object] {
			return
		}
		state.seenObjects[typ.Object] = true
		if ok, _ := checker.EqualityAvailable(typ); !ok {
			return
		}
		state.order = append(state.order, typ)
	case typ.Adt != nil:
		if state.seenADTs[typ.Adt] {
			return
		}
		state.seenADTs[typ.Adt] = true
		if ok, _ := checker.EqualityAvailable(typ); !ok {
			return
		}
		state.order = append(state.order, typ)
	case typ.Array != nil:
		if state.seenArrays[typ.Array] || equalityIneligibleElement(typ.Array.Element) {
			return
		}
		state.seenArrays[typ.Array] = true
		state.order = append(state.order, typ)
	case typ.Slice != nil:
		if state.seenSlices[typ.Slice] || equalityIneligibleElement(typ.Slice.Element) {
			return
		}
		state.seenSlices[typ.Slice] = true
		state.order = append(state.order, typ)
	case typ.List != nil:
		if state.seenLists[typ.List] || equalityIneligibleElement(typ.List.Element) {
			return
		}
		state.seenLists[typ.List] = true
		state.order = append(state.order, typ)
	case typ.Union != nil:
		if state.seenUnions[typ.Union] {
			return
		}
		state.seenUnions[typ.Union] = true
		state.order = append(state.order, typ)
		if !unionSupportsEquality(typ) {
			return
		}
		members := compilerTypes.UnionMembers(typ)
		for index := 0; index < members.Len(); index++ {
			if member, _ := members.At(index); member.List != nil {
				state.addComparedType(member)
			}
		}
	}
}

// discoverEqualityTypes walks the program collecting exactly the types
// compared by `==`/`!=` (DeepEqualityExpression, UnionEqualityExpression),
// plus their equality-helper dependencies. A type never compared is never
// collected, even if it appears elsewhere in the program.
func discoverEqualityTypes(program checker.Program) *generatedEqualityState {
	state := &generatedEqualityState{
		seenObjects: make(map[*compilerTypes.ObjectType]bool),
		seenADTs:    make(map[*compilerTypes.AdtType]bool),
		seenArrays:  make(map[*compilerTypes.ArrayInfo]bool),
		seenSlices:  make(map[*compilerTypes.SliceInfo]bool),
		seenLists:   make(map[*compilerTypes.ListInfo]bool),
		seenUnions:  make(map[*compilerTypes.UnionInfo]bool),
	}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.DeepEqualityExpression:
				if compilerTypes.IsText(node.OperandType) {
					state.needString = true
					return nil
				}
				state.addComparedType(node.OperandType)
			case checker.UnionEqualityExpression:
				state.addComparedType(node.OperandType)
			case checker.StringCompareExpression:
				if compilerTypes.IsText(node.OperandType) {
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
	if compilerTypes.IsText(typ) {
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
	case typ.Slice != nil:
		return equalityTypeContainsString(typ.Slice.Element, seen)
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
func writeEqualityDefinitions(result *strings.Builder, state *generatedEqualityState, tags *tagRegistry) error {
	if state == nil {
		return nil
	}
	for _, typ := range state.order {
		if isProgramOwnedEqualityType(typ) {
			continue
		}
		if typ.Union != nil {
			if unionSupportsEquality(typ) {
				if err := writeUnionEquality(result, typ, tags); err != nil {
					return err
				}
			}
			continue
		}
		if err := writeEqualityHelper(result, typ, tags); err != nil {
			return err
		}
	}
	return nil
}

// equalityStaticModel carries one static equality helper's decided name, const
// parameter spelling, and pre-rendered comparison body.
type equalityStaticModel struct {
	Name      string
	Parameter string
	Body      string
}

// compareLineModel carries one operand comparison line's decided indent and
// operand spellings; single-operand lines leave the unused side empty.
type compareLineModel struct {
	Indent string
	Left   string
	Right  string
}

// equalityCaseModel carries one switch case line's indent and decided tag.
type equalityCaseModel struct {
	Indent string
	Tag    string
}

// equalityOtherModel carries ErrorKind's flat other_header comparison;
// OtherTag is the decided Other tag spelling.
type equalityOtherModel struct {
	Indent   string
	Left     string
	Right    string
	OtherTag string
}

// writeEqualityHelper emits the equality helper body for one compared type.
// The body compares declared structure in order and returns false at the
// first unequal component; nothing reads padding, capacity, or backing
// addresses.
func writeEqualityHelper(result *strings.Builder, typ compilerTypes.Type, tags *tagRegistry) error {
	var body strings.Builder
	if err := writeEqualityComparisons(&body, "(*left)", "(*right)", typ, "    ", tags); err != nil {
		return err
	}
	// The helper never mutates its operands, so the parameters carry const;
	// call sites pass const-qualified bindings and a non-const parameter
	// would discard the qualifier under -Werror.
	parameter := "const " + typ.CName + " *left, const " + typ.CName + " *right"
	return renderInto(result, "module.h", "equality_helper", equalityStaticModel{
		Name:      equalityHelperName(typ),
		Parameter: parameter,
		Body:      body.String(),
	})
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
func writeEqualityComparisons(body *strings.Builder, left, right string, typ compilerTypes.Type, indent string, tags *tagRegistry) error {
	emit := func(block string, model any) error {
		return renderInto(body, "module.c", block, model)
	}
	switch {
	case typ.Union != nil:
		if err := emit("eq_tag_mismatch", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
		if err := emit("eq_switch_open", compareLineModel{Indent: indent, Left: left}); err != nil {
			return err
		}
		for _, member := range typ.Union.Members {
			field := tags.unionPayloadField(member)
			if err := emit("eq_case", equalityCaseModel{Indent: indent, Tag: tags.unionMemberTag(member)}); err != nil {
				return err
			}
			if compilerTypes.IsNil(member) {
				if err := emit("eq_return_true", indentModel{Indent: indent}); err != nil {
					return err
				}
				continue
			}
			memberLeft := equalityOperand(left+".payload."+field, member)
			memberRight := equalityOperand(right+".payload."+field, member)
			if err := writeEqualityComparisons(body, memberLeft, memberRight, member, indent+"    ", tags); err != nil {
				return err
			}
			if err := emit("eq_return_true", indentModel{Indent: indent}); err != nil {
				return err
			}
		}
		// hex_tag is one enum shared by every ADT and union tag in the whole
		// program, so a switch exhaustive over this union's own members is
		// still missing every other type's tag as far as -Wswitch can tell;
		// default is unreachable in valid checked code.
		if err := emit("eq_default_abort", indentModel{Indent: indent}); err != nil {
			return err
		}
		if err := emit("block_close", indentModel{Indent: indent}); err != nil {
			return err
		}
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			field := privateCName(memberName, member.Name, "")
			memberLeft := equalityOperand(left+"."+field, member.Type)
			memberRight := equalityOperand(right+"."+field, member.Type)
			if err := writeEqualityComparisons(body, memberLeft, memberRight, member.Type, indent, tags); err != nil {
				return err
			}
		}
	case compilerTypes.IsErrorKind(typ):
		// ErrorKind's Other is the only payload-carrying variant among 26 and
		// lives in one flat other_header field, not a per-variant payload
		// union (see hexal/error.h); Other's tag is the only case whose
		// bytes can differ, so equality does not need a full tag switch.
		if err := emit("eq_tag_mismatch", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
		if err := emit("eq_other_header", equalityOtherModel{
			Indent:   indent,
			Left:     left,
			Right:    right,
			OtherTag: errorKindTag(tags, "Other"),
		}); err != nil {
			return err
		}
	case typ.Adt != nil:
		if err := emit("eq_tag_mismatch", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
		if err := emit("eq_switch_open", compareLineModel{Indent: indent, Left: left}); err != nil {
			return err
		}
		for index, variant := range typ.Adt.Variants {
			if err := emit("eq_case", equalityCaseModel{Indent: indent, Tag: tags.adtVariantTag(typ.Adt, index)}); err != nil {
				return err
			}
			if len(variant.Payload) == 0 {
				if err := emit("eq_return_true", indentModel{Indent: indent}); err != nil {
					return err
				}
				continue
			}
			for _, member := range variant.Payload {
				field := ".payload." + compilerTypes.SanitizeIdentifier(variant.Name) + "." + privateCName(memberName, member.Name, "")
				memberLeft := equalityOperand(left+field, member.Type)
				memberRight := equalityOperand(right+field, member.Type)
				if err := writeEqualityComparisons(body, memberLeft, memberRight, member.Type, indent+"    ", tags); err != nil {
					return err
				}
			}
			if err := emit("eq_return_true", indentModel{Indent: indent}); err != nil {
				return err
			}
		}
		// hex_tag is one enum shared by every ADT and union tag in the whole
		// program, so a switch exhaustive over this ADT's own variants is
		// still missing every other type's tag as far as -Wswitch can tell;
		// default is unreachable in valid checked code.
		if err := emit("eq_default_abort", indentModel{Indent: indent}); err != nil {
			return err
		}
		if err := emit("block_close", indentModel{Indent: indent}); err != nil {
			return err
		}
	case typ.Array != nil:
		for index := uint64(0); index < typ.Array.Length; index++ {
			field := ".data[" + fmt.Sprint(index) + "]"
			elementLeft := equalityOperand(left+field, typ.Array.Element)
			elementRight := equalityOperand(right+field, typ.Array.Element)
			if err := writeEqualityComparisons(body, elementLeft, elementRight, typ.Array.Element, indent, tags); err != nil {
				return err
			}
		}
	case typ.Slice != nil:
		if err := emit("eq_length_mismatch", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
		if err := emit("eq_for_open", compareLineModel{Indent: indent, Left: left}); err != nil {
			return err
		}
		elementLeft := equalityOperand(left+".data[index]", typ.Slice.Element)
		elementRight := equalityOperand(right+".data[index]", typ.Slice.Element)
		if err := writeEqualityComparisons(body, elementLeft, elementRight, typ.Slice.Element, indent+"    ", tags); err != nil {
			return err
		}
		if err := emit("block_close", indentModel{Indent: indent}); err != nil {
			return err
		}
	case typ.List != nil:
		if err := emit("eq_length_mismatch", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
		if err := emit("eq_for_open", compareLineModel{Indent: indent, Left: left}); err != nil {
			return err
		}
		elementLeft := equalityOperand(left+".data[index]", typ.List.Element)
		elementRight := equalityOperand(right+".data[index]", typ.List.Element)
		if err := writeEqualityComparisons(body, elementLeft, elementRight, typ.List.Element, indent+"    ", tags); err != nil {
			return err
		}
		if err := emit("block_close", indentModel{Indent: indent}); err != nil {
			return err
		}
	case compilerTypes.IsString(typ):
		if err := emit("eq_text_heap", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
	case compilerTypes.IsInlineString(typ):
		// Inline text compares over its logical bytes only: the length, then
		// that many bytes. Whatever follows the length is never read.
		if err := emit("eq_text_inline", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
	case typ.Element != nil:
		if err := emit("eq_scalar", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
	default:
		if err := emit("eq_scalar", compareLineModel{Indent: indent, Left: left, Right: right}); err != nil {
			return err
		}
	}
	return nil
}
