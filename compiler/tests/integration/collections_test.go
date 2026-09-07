package integration

import (
	"hexal/compiler"
	"testing"
)

func TestStorabilityRule(t *testing.T) {
	accepted := []string{
		"names: Array<String, 4> := [\"a\", \"b\", \"c\", \"d\"]\n",
		"outer: List<List<Int32>> := List<List<Int32>>(Heap())\n",
		"views: List<View<Int32>> := List<View<Int32>>(Heap())\n",
		"lookup: Dict<Strand, List<Int32>> := Dict<Strand, List<Int32>>(Heap())\n",
		"value: String | Nil := nil\n",
		"value: List<Int32> | Nil := nil\n",
		"value: View<Int32> | Nil := nil\n",
		"v: View<View<Int32>> := View<View<Int32>>.empty()\n",
		"v: View<String> := View<String>.empty()\n",
		"fun s(xs: List<String>): View<String> do\n    return xs.slice(0, 1)\nend\n",
		"v: View<Int32> := View<Int32>.empty()\n",
		"type Row is struct t: Task<Int32>, c: Channel<Int32>, m: Mutex, e: EoS end\n",
		"type Box is struct f: Fun<(Int32) : Int32> end\n",
		// An ADT payload field also accepts Fun<...>: the expanded position
		// matrix admits it in every structural position, not only object
		// members.
		"fun helper(x: Int32): Int32 do return x end\ntype Wrapper is union | A as f: Fun<(Int32) : Int32> end | B as x: Int32 end end\nw: Wrapper := Wrapper.B(x = 1)\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
	// Array/List/Dict/Task/Channel elements now accept Fun.
	for _, source := range []string{
		"fun identity(x: Int32): Int32 do\n    return x\nend\nfuns: Array<Fun<(Int32) : Int32>, 1> := [identity]\n",
		"fun helper(x: Int32): Int32 do return x end\nfun f(h: Heap) do\n    values: List<Fun<(Int32) : Int32>> := List<Fun<(Int32) : Int32>>(h)\nend\n",
		"fun helper(x: Int32): Int32 do return x end\nfun f(h: Heap) do\n    d: Dict<Int32, Fun<(Int32) : Int32>> := Dict<Int32, Fun<(Int32) : Int32>>(h)\nend\n",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept for expanded Fun position; got %v:\n%s", result.Stderr, source)
		}
	}
	// A Fun inside a union member stays accepted, exactly like every other
	// structural position.
	for _, source := range []string{
		"fun helper(x: Int32): Int32 do return x end\ntype Wrapper is union Fun<(Int32) : Int32> | Int32 end\nw: Wrapper := 1\n",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}
