package integration

import (
	"testing"

	"hexal/compiler"
)

// D4 from_pointer provenance: reject every pointer locally traceable to ref,
// including through one and two copies and through a mut-pointer assignment;
// keep heap- and parameter-derived pointers accepted.
func TestProbeD4FromPointerProvenance(t *testing.T) {
	reject := []string{
		"fun f() do\n    mut a: Int32 = 1\n    view: View<Int32> = View<Int32>.from_pointer(ref a, 1)\nend\n",
		"fun f() do\n    mut a: Int32 = 1\n    p: Ptr<Int32> = ref a\n    view: View<Int32> = View<Int32>.from_pointer(p, 1)\nend\n",
		"fun f() do\n    mut a: Int32 = 1\n    p: Ptr<Int32> = ref a\n    q: Ptr<Int32> = p\n    view: View<Int32> = View<Int32>.from_pointer(q, 1)\nend\n",
		"fun f() do\n    mut a: Int32 = 1\n    p: Ptr<Int32> = ref a\n    mut q: Ptr<Int32> = p\n    q = ref a\n    view: View<Int32> = View<Int32>.from_pointer(q, 1)\nend\n",
		"fun f() do\n    mut a: Int32 = 1\n    p: Ptr<Int32> = ref a\n    q: Ptr<Int32> = p\n    r: Ptr<Int32> = q\n    view: View<Int32> = View<Int32>.from_pointer(r, 1)\nend\n",
	}
	for _, source := range reject {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure {
			t.Errorf("want reject:\n%s", source)
		} else {
			t.Logf("rejected: %v", result.Stderr)
		}
	}
	accept := []string{
		"fun f(h: Heap) do\n    p: Ptr<Int32> = h.allocate<Int32>(1)\n    view: View<Int32> = View<Int32>.from_pointer(p, 1)\nend\n",
		"fun f(p: Ptr<Int32>) do\n    view: View<Int32> = View<Int32>.from_pointer(p, 1)\nend\n",
	}
	for _, source := range accept {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitSuccess {
			t.Errorf("want accept, got %v:\n%s", result.Stderr, source)
		}
	}
}
