package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedViewState records the view types and the array slice helpers that
// need definitions, in deterministic order.
type generatedViewState struct {
	views []compilerTypes.Type
	seen  map[*compilerTypes.ViewInfo]bool
}

// discoverGeneratedViews walks every type reachable from the program and
// collects the distinct view types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedViews(program checker.Program) (*generatedViewState, error) {
	state := &generatedViewState{seen: make(map[*compilerTypes.ViewInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.View != nil {
				if !state.seen[typ.View] {
					state.seen[typ.View] = true
					state.views = append(state.views, typ)
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}

	sort.SliceStable(state.views, func(left, right int) bool {
		return state.views[left].CName < state.views[right].CName
	})
	return state, nil
}
func writeViewDefinitions(result *strings.Builder, views *generatedViewState) {
	if views == nil {
		return
	}
	for _, view := range views.views {
		element := view.View.Element
		suffix := strings.TrimPrefix(view.CName, "hex_view_")
		fmt.Fprintf(result, "\ntypedef struct %s {\n    const %s *data;\n    size_t length;\n} %s;\n", view.CName, pointerSpelling(element), view.CName)
		fmt.Fprintf(result, "static inline const %s *hex_view_at_%s(%s view, size_t index) {\n", pointerSpelling(element), suffix, view.CName)
		writeViewBoundsGuard(result)
		result.WriteString("    return &view.data[index];\n}\n")
		fmt.Fprintf(result, "static inline %s hex_view_slice_%s(%s view, uint64_t start, uint64_t end) {\n", view.CName, suffix, view.CName)
		writeViewSliceGuard(result)
		fmt.Fprintf(result, "    return (%s){&view.data[start], end - start};\n}\n", view.CName)
	}
}

func writeViewBoundsGuard(result *strings.Builder) {
	result.WriteString("    if (index >= view.length) {\n")
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] view index out of bounds\\n\");\n    }\n")
}

func writeViewSliceGuard(result *strings.Builder) {
	result.WriteString("    if (!(start <= end && end <= view.length)) {\n")
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] view slice bounds out of range\\n\");\n    }\n")
}

// writeArraySliceHelper emits the slice helper for one array type, which
// bounds-checks the range against the compile-time length and returns a view
// into the array's inline storage.
func writeArraySliceHelper(result *strings.Builder, array compilerTypes.Type, view compilerTypes.Type) {
	length := array.Array.Length
	suffix := arrayAccessorSuffix(array)
	fmt.Fprintf(result, "\nstatic inline %s hex_array_slice_%s(const %s *array, uint64_t start, uint64_t end) {\n", view.CName, suffix, array.CName)
	fmt.Fprintf(result, "    if (!(start <= end && end <= UINT64_C(%d))) {\n", length)
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] array slice bounds out of range\\n\");\n    }\n")
	fmt.Fprintf(result, "    return (%s){&array->data[start], end - start};\n}\n", view.CName)
}

// ensureViewUInt8 adds the byte view type to the view state if missing; the
// String helpers always reference it.
func ensureViewUInt8(state *generatedViewState) {
	if state == nil {
		return
	}
	for _, view := range state.views {
		if view.CName == "hex_view_UInt8" {
			return
		}
	}
	view := compilerTypes.NewEnvironment().ViewType(compilerTypes.UInt8)
	state.seen[view.View] = true
	state.views = append(state.views, view)
}

// viewCName returns the C struct name of the view type over one element.
func viewCName(element compilerTypes.Type) string {
	return "hex_view_" + compilerTypes.SanitizeIdentifier(element.Name)
}
