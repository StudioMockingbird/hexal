package generator

// Module-owned collection specializations: a list, dict, view, or array over
// a module-emitted element type is emitted into each consuming module header
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
	if typ.Array != nil {
		return typeIsModuleEmitted(typ.Array.Element)
	}
	if typ.View != nil {
		return typeIsModuleEmitted(typ.View.Element)
	}
	if typ.List != nil {
		return typeIsModuleEmitted(typ.List.Element)
	}
	if typ.Dict != nil {
		return typeIsModuleEmitted(typ.Dict.Value)
	}
	return false
}

// collectionElementModuleTyped reports whether one collection specialization
// spells a module-emitted type: the element of a list, array, or view, or
// the value of a dict (the key is always a builtin Int32 or Strand).
func collectionElementModuleTyped(typ compilerTypes.Type) bool {
	switch {
	case typ.List != nil:
		return typeIsModuleEmitted(typ.List.Element)
	case typ.Array != nil:
		return typeIsModuleEmitted(typ.Array.Element)
	case typ.View != nil:
		return typeIsModuleEmitted(typ.View.Element)
	case typ.Dict != nil:
		return typeIsModuleEmitted(typ.Dict.Value)
	}
	return false
}

// moduleCollectionDependencyOrder orders one module's module-owned collection
// specializations so every specialization precedes any specialization it
// spells: the element of a list, array, or view may itself be a collection
// (a handle, an inline array, or the element's matching view), and a dict
// value may be any of those. Cross-family edges make a single pass over the
// C-name-sorted family orders insufficient; the graph is small per module
// and always acyclic because a specialization can only spell already-interned
// types.
func moduleCollectionDependencyOrder(views, arrays, lists, dicts []compilerTypes.Type, viewState *generatedViewState) []compilerTypes.Type {
	byName := make(map[string]compilerTypes.Type)
	all := make([]compilerTypes.Type, 0)
	for _, order := range [][]compilerTypes.Type{views, arrays, lists, dicts} {
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
		for _, dependency := range spelledCollectionNames(typ, viewState) {
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
// dict value) and the element's matching view. Object, ADT, and union member
// types are not collection dependencies: their definitions live in earlier
// module-header regions.
func spelledCollectionNames(typ compilerTypes.Type, viewState *generatedViewState) []string {
	names := make([]string, 0, 4)
	var walk func(t compilerTypes.Type)
	walk = func(t compilerTypes.Type) {
		switch {
		case t.List != nil:
			names = append(names, t.CName)
			walk(t.List.Element)
		case t.Dict != nil:
			names = append(names, t.CName)
			walk(t.Dict.Value)
		case t.Array != nil:
			names = append(names, t.CName)
			walk(t.Array.Element)
		case t.View != nil:
			names = append(names, t.CName)
			walk(t.View.Element)
		case t.Element != nil:
			walk(*t.Element)
		}
	}
	var element compilerTypes.Type
	switch {
	case typ.List != nil:
		element = typ.List.Element
	case typ.Dict != nil:
		element = typ.Dict.Value
	case typ.Array != nil:
		element = typ.Array.Element
	case typ.View != nil:
		element = typ.View.Element
	}
	walk(element)
	if view := matchingView(viewState, element); view != (compilerTypes.Type{}) {
		names = append(names, view.CName)
	}
	return names
}

// writeModuleCollectionSpecializations emits the module-owned collection
// specializations of one module header, dependency-ordered after the object
// definitions and before the helper families that spell them. Each fragment
// renders the collection body template without the component guard and
// include shell. Duplication across module headers is the existing re-emission
// strategy for module types and cannot collide: no module includes another
// module's header.
func writeModuleCollectionSpecializations(result *strings.Builder, input *moduleHeaderInput) error {
	if input == nil {
		return nil
	}
	views := []compilerTypes.Type(nil)
	if input.views != nil {
		views = input.views.views
	}
	arrays := []compilerTypes.Type(nil)
	if input.arrays != nil {
		arrays = input.arrays.order
	}
	lists := []compilerTypes.Type(nil)
	if input.lists != nil {
		lists = input.lists.order
	}
	dicts := []compilerTypes.Type(nil)
	if input.dicts != nil {
		dicts = input.dicts.order
	}
	ordered := moduleCollectionDependencyOrder(views, arrays, lists, dicts, input.views)
	if len(ordered) == 0 {
		return nil
	}
	result.WriteString("\n/* Module-owned collection specializations. */\n")
	hashEmitted := make(map[string]bool)
	for _, typ := range ordered {
		var artifact componentArtifact
		switch {
		case typ.View != nil:
			artifact = componentArtifact{key: "hexal/view.h", template: "view.h", block: "viewbody", model: viewComponentModel{Views: []viewComponentRecord{viewComponentRecordFor(typ)}}}
		case typ.Array != nil:
			artifact = componentArtifact{key: "hexal/array.h", template: "array.h", block: "arraybody", model: arrayComponentModel{Arrays: []arrayComponentRecord{arrayComponentRecordFor(typ, input.views, input.arrays)}}}
		case typ.List != nil:
			artifact = componentArtifact{key: "hexal/list.h", template: "list.h", block: "listbody", model: listComponentModel{Lists: []listComponentRecord{listComponentRecordFor(typ, input.views)}}}
		case typ.Dict != nil:
			artifact = componentArtifact{key: "hexal/dict.h", template: "dict.h", block: "dictbody", model: dictComponentModel{Dicts: []dictComponentRecord{dictComponentRecordFor(typ, hashEmitted)}}}
		}
		fragment, renderErr := renderComponent(artifact)
		if renderErr != nil {
			return renderErr
		}
		result.WriteString(fragment)
	}
	return nil
}
