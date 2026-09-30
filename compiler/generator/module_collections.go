package generator

// Module-owned collection specializations: a list, dict, slice, or pool
// over a module-emitted element type is emitted into each consuming module
// header
// immediately after that module's type definitions. A component artifact is
// program-wide: it cannot re-emit a per-module type and has no include path
// to the module that owns one, so module headers are the only translation
// unit where the element type is available. Builtin-element specializations
// keep their component artifacts byte-identical.

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// typeIsModuleEmitted reports whether typ's C definition lives only in module
// headers: module-owned objects and ADTs (the checker stamps ModuleID on
// every object it creates in a module scope), structural unions (every union
// is re-emitted per module), and anything containing or pointing to them.
// Task, Channel, and Atomic handles are opaque component-wide typedefs, so
// their payloads never appear in a collection body and do not count.
func typeIsModuleEmitted(typ compilerTypes.Type) bool {
	if compilerTypes.IsDictEntry(typ) {
		for _, member := range typ.Object.Members {
			if typeIsModuleEmitted(member.Type) {
				return true
			}
		}
		return false
	}
	if typ.Object != nil {
		return typ.Object.ModuleID != ""
	}
	if typ.Adt != nil {
		return typ.Adt.ModuleID != ""
	}
	if typ.Union != nil {
		return true
	}
	if typ.NullableBase != nil {
		return typeIsModuleEmitted(*typ.NullableBase)
	}
	if typ.Element != nil {
		return typeIsModuleEmitted(*typ.Element)
	}
	if typ.InlineList != nil {
		return typeIsModuleEmitted(typ.InlineList.Element)
	}
	if typ.Slice != nil {
		return typeIsModuleEmitted(typ.Slice.Element)
	}
	if typ.List != nil {
		return typeIsModuleEmitted(typ.List.Element)
	}
	if typ.Dict != nil {
		return typeIsModuleEmitted(typ.Dict.Value)
	}
	if typ.Pool != nil {
		return typeIsModuleEmitted(typ.Pool.Element)
	}
	if typ.Signature != nil {
		if typ.Signature.Result != nil && typeIsModuleEmitted(*typ.Signature.Result) {
			return true
		}
		for _, parameter := range typ.Signature.Parameters {
			if typeIsModuleEmitted(parameter) {
				return true
			}
		}
		return false
	}
	return false
}

// moduleRoutedElement reports whether element forces its own collection
// specialization to the consuming module's own header rather than the
// shared component: a module-emitted type, or Signal. hexal/signal.h needs
// hexal/error.h, which needs hexal/string.h, which needs hex_slice_UInt8 --
// hexal/slice.h is positioned ahead of all of them precisely so they can
// rely on it already being complete, so no shared component naming a Signal
// specialization directly (slice.h itself, or another component's own
// "matching slice" cross-reference to hex_slice_Signal)
// can include hexal/signal.h without inverting that dependency direction.
// The module header's own body renders after every component #include,
// where hexal/signal.h -- pulled in by moduleSignalComponent -- is already
// complete, so every Signal-element specialization (List, Dict,
// Slice, Pool alike, not just the one directly reaching Signal) is routed
// there together for one consistent, always-available definition. File,
// TcpConnection, and Process have the identical layering conflict and are
// not fixed here.
func moduleRoutedElement(element compilerTypes.Type) bool {
	return typeIsModuleEmitted(element) || elementNeedsSignal(element) || elementNeedsTerminal(element)
}

// collectionElementModuleTyped reports whether one collection specialization
// spells a module-emitted type: the element of a List, Slice, or Pool,
// or the value of a Dict (keys are scalar or inline String<N> values).
func collectionElementModuleTyped(typ compilerTypes.Type) bool {
	switch {
	case typ.List != nil:
		return moduleRoutedElement(typ.List.Element)
	case typ.InlineList != nil:
		return moduleRoutedElement(typ.InlineList.Element)
	case typ.Slice != nil:
		return moduleRoutedElement(typ.Slice.Element)
	case typ.Dict != nil:
		return moduleRoutedElement(typ.Dict.Value)
	case typ.Pool != nil:
		return moduleRoutedElement(typ.Pool.Element)
	}
	return false
}

// moduleCollectionDependencyOrder orders one module's module-owned collection
// specializations so every specialization precedes any specialization it
// spells: the element of a list, array, or slice may itself be a collection
// (a handle, an inline List, or the element's matching Slice), and a dict
// value may be any of those. Cross-family edges make a single pass over the
// C-name-sorted family orders insufficient; the graph is small per module
// and always acyclic because a specialization can only spell already-interned
// types.
func moduleCollectionDependencyOrder(slices, lists, dicts, pools []compilerTypes.Type, sliceState *generatedSliceState) []compilerTypes.Type {
	byName := make(map[string]compilerTypes.Type)
	all := make([]compilerTypes.Type, 0)
	for _, order := range [][]compilerTypes.Type{slices, lists, dicts, pools} {
		for _, typ := range order {
			if collectionElementModuleTyped(typ) {
				byName[typ.CName] = typ
				all = append(all, typ)
			}
		}
	}
	visited := make(map[string]bool)
	result := make([]compilerTypes.Type, 0, len(all))
	var visit func(typ compilerTypes.Type)
	visit = func(typ compilerTypes.Type) {
		if visited[typ.CName] {
			return
		}
		visited[typ.CName] = true
		for _, dependency := range spelledCollectionNames(typ, sliceState) {
			if inner, ok := byName[dependency]; ok {
				visit(inner)
			}
		}
		result = append(result, typ)
	}
	for _, typ := range all {
		visit(typ)
	}
	return result
}

// spelledCollectionNames returns the C names of every collection type typ's
// body spells beyond typ itself: the collection types inside its element (or
// dict value) and the element's matching slice. Object, ADT, and union member
// types are not collection dependencies: their definitions live in earlier
// module-header regions.
func spelledCollectionNames(typ compilerTypes.Type, sliceState *generatedSliceState) []string {
	names := make([]string, 0, 4)
	var walk func(t compilerTypes.Type)
	walk = func(t compilerTypes.Type) {
		switch {
		case t.List != nil:
			names = append(names, t.CName)
			walk(t.List.Element)
		case t.InlineList != nil:
			names = append(names, t.CName)
			walk(t.InlineList.Element)
		case t.Dict != nil:
			names = append(names, t.CName)
			walk(t.Dict.Value)
		case t.Slice != nil:
			names = append(names, t.CName)
			walk(t.Slice.Element)
		case t.Pool != nil:
			names = append(names, t.CName)
			walk(t.Pool.Element)
		case t.Element != nil:
			walk(*t.Element)
		}
	}
	var element compilerTypes.Type
	switch {
	case typ.List != nil:
		element = typ.List.Element
	case typ.InlineList != nil:
		element = typ.InlineList.Element
	case typ.Dict != nil:
		element = typ.Dict.Value
	case typ.Slice != nil:
		element = typ.Slice.Element
	case typ.Pool != nil:
		element = typ.Pool.Element
	}
	walk(element)
	if slice := matchingSlice(sliceState, element, false); slice != (compilerTypes.Type{}) {
		names = append(names, slice.CName)
	}
	if slice := matchingSlice(sliceState, element, true); slice != (compilerTypes.Type{}) {
		names = append(names, slice.CName)
	}
	return names
}

// moduleOwnedCollectionOrder returns the module's collection specializations
// in body-dependency order, with each family's nil-mode state guarded.
func moduleOwnedCollectionOrder(input *moduleHeaderInput) []compilerTypes.Type {
	slices := []compilerTypes.Type(nil)
	if input.slices != nil {
		slices = input.slices.slices
	}
	lists := []compilerTypes.Type(nil)
	if input.lists != nil {
		for _, typ := range input.lists.order {
			if input.lists.componentOwned[typ.List] {
				continue
			}
			lists = append(lists, typ)
		}
	}
	dicts := []compilerTypes.Type(nil)
	if input.dicts != nil {
		dicts = input.dicts.order
	}
	pools := []compilerTypes.Type(nil)
	if input.pools != nil {
		pools = input.pools.order
	}
	return moduleCollectionDependencyOrder(slices, lists, dicts, pools, input.slices)
}

// writeCollectionForwardDeclarations emits one incomplete typedef per
// module-owned collection specialization ahead of the nominal type bodies.
// A nominal body that holds a collection handle needs the specialization's
// name in scope, while the bodies define the specializations only after the
// nominal bodies because a Dict entry stores its value by value. The nominal
// side stores only a pointer, so an incomplete type suffices, and C11 and
// later permit repeating a typedef of the same type.
func writeCollectionForwardDeclarations(result *strings.Builder, input *moduleHeaderInput) error {
	if input == nil {
		return nil
	}
	ordered := moduleOwnedCollectionOrder(input)
	if len(ordered) == 0 {
		return nil
	}
	handles := make([]compilerTypes.Type, 0, len(ordered))
	for _, typ := range ordered {
		// A Slice descriptor is stored by value, so an incomplete typedef is
		// not sufficient for it; its complete fragment renders pre-nominal
		// instead and needs no forward name of its own.
		if typ.Slice != nil {
			continue
		}
		handles = append(handles, typ)
	}
	if len(handles) == 0 {
		return nil
	}
	var text strings.Builder
	for _, typ := range handles {
		text.WriteString("typedef struct ")
		text.WriteString(typ.CName)
		text.WriteString(" ")
		text.WriteString(typ.CName)
		text.WriteString(";\n")
	}
	return renderInto(result, "module.h", "raw_text", rawTextModel{Text: text.String()})
}

// writePreNominalSlices emits each module-owned Slice descriptor ahead of the
// nominal bodies. A Slice descriptor is stored by value inside a nominal
// body, so an incomplete typedef cannot serve it; its data field spells only
// a pointer to the element, and just the nominal forward names above. The
// descriptor's inline helpers stay with the post-nominal collection bodies:
// they subscript the element pointer and need its complete type.
func writePreNominalSlices(result *strings.Builder, input *moduleHeaderInput) error {
	if input == nil {
		return nil
	}
	var slices []compilerTypes.Type
	for _, typ := range moduleOwnedCollectionOrder(input) {
		if typ.Slice != nil {
			slices = append(slices, typ)
		}
	}
	if len(slices) == 0 {
		return nil
	}
	for _, typ := range slices {
		fragment, renderErr := renderComponent(componentArtifact{key: "hexal/slice.h", template: "slice.h", block: "slicedesc", model: sliceComponentModel{Slices: []sliceComponentRecord{sliceComponentRecordFor(typ)}}})
		if renderErr != nil {
			return renderErr
		}
		if err := renderInto(result, "module.h", "raw_text", rawTextModel{Text: fragment}); err != nil {
			return err
		}
	}
	return nil
}

// writeModuleCollectionSpecializations emits the module-owned collection
// specializations of one module header, dependency-ordered after the object
// definitions and before the helper families that spell them. Module-owned
// Slices re-appear here only for their inline helpers: the descriptors
// rendered as pre-nominal fragments, and helpers need the complete element
// type the nominal bodies provide. Each fragment
// renders the collection body template without the component guard and
// include shell. Duplication across module headers is the existing re-emission
// strategy for module types and cannot collide: no module includes another
// module's header.
func writeModuleCollectionSpecializations(result *strings.Builder, input *moduleHeaderInput) error {
	if input == nil {
		return nil
	}
	ordered := moduleOwnedCollectionOrder(input)
	if len(ordered) == 0 {
		return nil
	}
	if err := renderInto(result, "module.h", "collection_banner", struct{}{}); err != nil {
		return err
	}
	hashEmitted := make(map[string]bool)
	for _, typ := range ordered {
		if typ.Slice != nil {
			// The descriptor rendered pre-nominal; only its helpers need
			// the complete element type that follows the nominal bodies.
			fragment, renderErr := renderComponent(componentArtifact{key: "hexal/slice.h", template: "slice.h", block: "slicemethod", model: sliceComponentModel{Slices: []sliceComponentRecord{sliceComponentRecordFor(typ)}}})
			if renderErr != nil {
				return renderErr
			}
			if err := renderInto(result, "module.h", "raw_text", rawTextModel{Text: fragment}); err != nil {
				return err
			}
			continue
		}
		var artifact componentArtifact
		switch {
		case typ.List != nil || typ.InlineList != nil:
			artifact = componentArtifact{key: "hexal/list.h", template: "list.h", block: "listbody", model: listComponentModel{Lists: []listComponentRecord{listComponentRecordFor(typ, input.slices)}}}
		case typ.Dict != nil:
			artifact = componentArtifact{key: "hexal/dict.h", template: "dict.h", block: "dictbody", model: dictComponentModel{Dicts: []dictComponentRecord{dictComponentRecordFor(typ, hashEmitted)}}}
		case typ.Pool != nil:
			artifact = componentArtifact{key: "hexal/pool.h", template: "pool.h", block: "poolbody", model: poolComponentModel{Pools: []poolComponentRecord{poolComponentRecordFor(typ)}}}
		}
		fragment, renderErr := renderComponent(artifact)
		if renderErr != nil {
			return renderErr
		}
		if err := renderInto(result, "module.h", "raw_text", rawTextModel{Text: fragment}); err != nil {
			return err
		}
	}
	return nil
}
