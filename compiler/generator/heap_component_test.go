package generator

import (
	"strings"
	"testing"

	compilerTypes "hexal/compiler/types"
)

// The aligned allocation primitive is demand-selected: a program that never
// asks for an alignment does not inherit its declaration or definition.
func TestHeapComponentSelectsAlignedPrimitiveOnDemand(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		state   *heapHelpers
		aligned bool
	}{
		{"ordinary allocation only", &heapHelpers{seen: map[string]bool{}, elements: []compilerTypes.Type{compilerTypes.Int32}}, false},
		{"heap token only", &heapHelpers{seen: map[string]bool{}, required: true}, false},
		{"aligned allocation", &heapHelpers{seen: map[string]bool{}, alignedSeen: map[string]bool{}, alignedElements: []compilerTypes.Type{compilerTypes.Int32}}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			components, err := heapComponents(&programEmission{heapState: testCase.state})
			if err != nil {
				t.Fatal(err)
			}
			if len(components) != 2 {
				t.Fatalf("heapComponents produced %d artifacts, want 2", len(components))
			}
			rendered := make(map[string]string, len(components))
			for _, component := range components {
				text, renderErr := renderComponent(component)
				if renderErr != nil {
					t.Fatal(renderErr)
				}
				rendered[component.key] = text
			}
			header, source := rendered["hexal/heap.h"], rendered["hexal/heap.c"]
			declared := strings.Count(header, "void *hex_heap_allocate_aligned(size_t size, size_t alignment,")
			defined := strings.Count(source, "void *hex_heap_allocate_aligned(size_t size, size_t alignment,")
			want := 0
			if testCase.aligned {
				want = 1
			}
			if declared != want || defined != want {
				t.Fatalf("aligned primitive declared %d / defined %d, want %d each:\n%s\n%s", declared, defined, want, header, source)
			}
			if !testCase.aligned {
				return
			}
			// Validation order: the request is checked before it is used in
			// any arithmetic and before mimalloc is reached.
			guard := strings.Index(source, "if (alignment == 0 || (alignment & (alignment - 1)) != 0) {")
			effective := strings.Index(source, "size_t effective_alignment = alignment < minimum_alignment")
			allocate := strings.Index(source, "mi_malloc_aligned(size, effective_alignment)")
			if guard < 0 || effective < 0 || allocate < 0 || !(guard < effective && effective < allocate) {
				t.Fatalf("hexal/heap.c validation order is wrong:\n%s", source)
			}
			if strings.Count(source, "mi_malloc_aligned(") != 1 {
				t.Fatalf("hexal/heap.c must call mi_malloc_aligned exactly once:\n%s", source)
			}
			for _, unwanted := range []string{"memset", "hex_heap_free_aligned", "aligned_alloc("} {
				if strings.Contains(source, unwanted) {
					t.Fatalf("hexal/heap.c emitted %q:\n%s", unwanted, source)
				}
			}
		})
	}
}
