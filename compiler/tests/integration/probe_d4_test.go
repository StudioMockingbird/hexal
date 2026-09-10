package integration

import (
	"testing"

	"hexal/compiler"
)

// from_pointer performs no provenance analysis: every pointer mode the
// signature accepts constructs, including pointers into locals. Backing
// storage validity is the programmer's responsibility.
func TestProbeD4FromPointerProvenance(t *testing.T) {
	accept := []string{
		"fun f() do\n    mut a: Int32 := 1\n    view: Slice<Int32> := Slice<Int32>.from_pointer(@a, 1)\nend\n",
		"fun f() do\n    mut a: Int32 := 1\n    p: Ptr<Int32> := @a\n    view: Slice<Int32> := Slice<Int32>.from_pointer(p, 1)\nend\n",
		"fun f() do\n    mut a: Int32 := 1\n    p: Ptr<Int32> := @a\n    q: Ptr<Int32> := p\n    view: Slice<Int32> := Slice<Int32>.from_pointer(q, 1)\nend\n",
		"fun f() do\n    mut a: Int32 := 1\n    p: Ptr<Int32> := @a\n    mut q: Ptr<Int32> := p\n    q = @a\n    view: Slice<Int32> := Slice<Int32>.from_pointer(q, 1)\nend\n",
		"fun f() do\n    mut a: Int32 := 1\n    p: Ptr<Int32> := @a\n    q: Ptr<Int32> := p\n    r: Ptr<Int32> := q\n    view: Slice<Int32> := Slice<Int32>.from_pointer(r, 1)\nend\n",
		"fun f(h: Heap) do\n    p: Ptr<Int32> := h.allocate<Int32>(1)\n    view: Slice<Int32> := Slice<Int32>.from_pointer(p, 1)\nend\n",
		"fun f(p: Ptr<Int32>) do\n    view: Slice<Int32> := Slice<Int32>.from_pointer(p, 1)\nend\n",
	}
	for _, source := range accept {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitSuccess {
			t.Errorf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}
