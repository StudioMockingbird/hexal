package compiler

import "testing"

func TestStorabilityRule(t *testing.T) {
	accepted := []string{
		"names: Array<String, 4> = [\"a\", \"b\", \"c\", \"d\"]\n",
		"outer: List<List<Int32>> = List<List<Int32>>.new(Heap.new())\n",
		"views: List<View<Int32>> = List<View<Int32>>.new(Heap.new())\n",
		"lookup: Dict<Strand, List<Int32>> = Dict<Strand, List<Int32>>.new(Heap.new())\n",
		"value: String | Nil = nil\n",
		"value: List<Int32> | Nil = nil\n",
		"value: View<Int32> | Nil = nil\n",
		"v: View<View<Int32>> = View<View<Int32>>.empty()\n",
		"v: View<String> = View<String>.empty()\n",
		"fun s(xs: List<String>): View<String>\n    return xs.slice(0, 1)\nend\n",
		"v: View<Int32> = View<Int32>.empty()\n",
		"type Row = { f: File, t: Task<Int32>, c: Channel<Int32>, m: Mutex, s: Stream<Int32>, e: EoS }\n",
	}
	for _, source := range accepted {
		if result := Compile(source); result.ExitCode != ExitSuccess {
			t.Fatalf("want accept, got %v:\n%s", result.Stderr, source)
		}
	}
	rejected := []string{
		"type Box = { f: Fun<(Int32) : Int32> }\n",
		"funs: Array<Fun<(Int32) : Int32>, 1> = [identity]\nfun identity(x: Int32): Int32\n    return x\nend\n",
	}
	for _, source := range rejected {
		if result := Compile(source); result.ExitCode != ExitFailure {
			t.Fatalf("want reject, got accept:\n%s", source)
		}
	}
}
