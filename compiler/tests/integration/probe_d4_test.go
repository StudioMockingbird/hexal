package integration

import (
	"testing"

	"hexal/compiler"
)

// from_pointer performs no provenance analysis: every pointer mode the
// signature accepts constructs, including pointers into locals. Backing
// storage validity is the programmer's assertion inside the permission region.
func TestProbeD4FromPointerProvenance(t *testing.T) {
	accept := []string{
		"fun f() do\n    let mut a: Int32 = 1\n    unsafe do\n        let view: Slice<Int32> = Slice<Int32>.from_pointer(@a, 1)\n    end\nend\n",
		"fun f() do\n    let mut a: Int32 = 1\n    let p: Ptr<Int32> = @a\n    unsafe do\n        let view: Slice<Int32> = Slice<Int32>.from_pointer(p, 1)\n    end\nend\n",
		"fun f() do\n    let mut a: Int32 = 1\n    let p: Ptr<Int32> = @a\n    let q: Ptr<Int32> = p\n    unsafe do\n        let view: Slice<Int32> = Slice<Int32>.from_pointer(q, 1)\n    end\nend\n",
		"fun f() do\n    let mut a: Int32 = 1\n    let p: Ptr<Int32> = @a\n    let mut q: Ptr<Int32> = p\n    q = @a\n    unsafe do\n        let view: Slice<Int32> = Slice<Int32>.from_pointer(q, 1)\n    end\nend\n",
		"fun f() do\n    let mut a: Int32 = 1\n    let p: Ptr<Int32> = @a\n    let q: Ptr<Int32> = p\n    let r: Ptr<Int32> = q\n    unsafe do\n        let view: Slice<Int32> = Slice<Int32>.from_pointer(r, 1)\n    end\nend\n",
		"fun f(h: Heap) do\n    let p: Ptr<Int32> = h.allocate<Int32>(1)\n    unsafe do\n        let view: Slice<Int32> = Slice<Int32>.from_pointer(p, 1)\n    end\nend\n",
		"fun f(p: Ptr<Int32>) do\n    unsafe do\n        let view: Slice<Int32> = Slice<Int32>.from_pointer(p, 1)\n    end\nend\n",
	}
	for _, source := range accept {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitSuccess {
			t.Errorf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}
