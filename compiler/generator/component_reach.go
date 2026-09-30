package generator

import compilerTypes "hexal/compiler/types"

// The enforcing reachability the JSON/regex adapters rely on: the component
// units reference these List specializations and their element records, so the
// generation discovery must have them registered even when no source
// expression names one. They are the adapter machinery's own compile-time
// dependencies, not a user program's; forcing them here is the discovery pass
// linking the list component to what the runtime adapter actually spells.

func forcedJSONLists(listState *generatedListState) {
	if listState == nil {
		return
	}
	for _, special := range []compilerTypes.Type{
		compilerTypes.JsonValueListType(),
		compilerTypes.JsonMemberListType(),
	} {
		if special.List != nil && !listState.seen[special.List] {
			listState.seen[special.List] = true
			listState.order = append(listState.order, special)
		}
	}
}

func forcedRegexCapture(listState *generatedListState) {
	if listState == nil {
		return
	}
	capture := compilerTypes.RegexCaptureListType()
	if capture.List != nil && !listState.seen[capture.List] {
		listState.seen[capture.List] = true
		listState.order = append(listState.order, capture)
	}
	if capture.List != nil {
		// hexal_regex.c is a core unit with no include path to any module
		// header, and hex_t_Span_Nil's own body ships in the core
		// hexal/regex.h, so the shared list artifact can hold both the
		// specialization and its element.
		if listState.componentOwned == nil {
			listState.componentOwned = make(map[*compilerTypes.ListInfo]bool)
		}
		listState.componentOwned[capture.List] = true
	}
}
