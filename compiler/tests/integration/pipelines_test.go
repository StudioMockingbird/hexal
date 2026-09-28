package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
)

func TestEveryPipelineSourceSupportsAdaptorsAndTerminals(t *testing.T) {
	callbacks := "fun keep(value: Int32): Bool do\n    return value > 0\nend\n" +
		"fun map_value(value: Int32): Int32 do\n    return value + 1\nend\n" +
		"fun keep_entry(value: DictEntry<Int32, Int32>): Bool do\n    return value.value > 0\nend\n" +
		"fun map_entry(value: DictEntry<Int32, Int32>): Int32 do\n    return value.value\nend\n" +
		"fun add(total: Int32, value: Int32): Int32 do\n    return total + value\nend\n" +
		"fun make_fixed(): List<Int32, 2> do\n    return [1, 2]\nend\n"
	tests := []struct {
		name, source, filter, mapper string
	}{
		{"allocated-list", "values", "keep", "map_value"},
		{"inline-list-place", "fixed", "keep", "map_value"},
		{"inline-list-temporary", "make_fixed()", "keep", "map_value"},
		{"slice", "view", "keep", "map_value"},
		{"mutable-slice-copy", "mutable_view", "keep", "map_value"},
		{"dict-keys", "dict.keys()", "keep", "map_value"},
		{"dict-values", "dict.values()", "keep", "map_value"},
		{"dict-entries", "dict.entries()", "keep_entry", "map_entry"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			setup := "fun demo(heap: Heap) do\n" +
				"    let values: List<Int32> = List<Int32>(heap)\n    values.push(1)\n" +
				"    let fixed: List<Int32, 2> = [1, 2]\n" +
				"    let view: Slice<Int32> = fixed.slice(0, 2)\n" +
				"    let mut mutable_fixed: List<Int32, 2> = [1, 2]\n" +
				"    let mutable_view: Slice<mut Int32> = mutable_fixed.mut_slice(0, 2)\n" +
				"    let dict: Dict<Int32, Int32> = Dict<Int32, Int32>(heap)\n    dict.insert(1, 2)\n"
			operations := "    let copied = " + testCase.source + ".filter(" + testCase.filter + ").map(" + testCase.mapper + ").to_list(heap)\n" +
				"    let reduced = " + testCase.source + ".map(" + testCase.mapper + ").filter(keep).reduce(0, add)\n" +
				"    for item in " + testCase.source + ".filter(" + testCase.filter + ").map(" + testCase.mapper + ") do\n        let observed: Int32 = item\n    end\nend\n"
			result := compileSource(callbacks + setup + operations)
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("Compile stderr = %#v", result.Stderr)
			}
			if got := strings.Count(rootC(t, result), "hex_pipeline_"); got == 0 {
				t.Fatal("generated C has no fused pipeline traversal")
			}
		})
	}
}

func TestPipelineConsumptionAndCallbackDiagnostics(t *testing.T) {
	base := "fun identity(value: Int32): Int32 do\n    return value\nend\n" +
		"fun wrong(value: Int16): Int16 do\n    return value\nend\n" +
		"fun no_result(value: Int32) do\n    print(value)\nend\n" +
		"fun consume(value: Int32) do\nend\n" +
		"fun demo(heap: Heap) do\n    let values: List<Int32, 1> = [1]\n"
	tests := []struct {
		name, body, diagnostic string
	}{
		{"binding escape", "    let escaped = values.map(identity)\n", "lazy pipeline must end in to_list(heap), reduce(initial, combine), or direct for iteration"},
		{"argument escape", "    consume(values.map(identity))\n", "lazy pipeline must end in to_list(heap), reduce(initial, combine), or direct for iteration"},
		{"nested terminal", "    consume(values.map(identity).to_list(heap).length().to<Int32>())\n", "pipeline terminal must be a complete binding initializer, assignment source, or return expression"},
		{"wrong callback parameter", "    let copied = values.map(wrong)\n", "pipeline callback must be a concrete non-rest Fun value"},
		{"callback without result", "    let copied = values.map(no_result)\n", "pipeline callback must be a concrete non-rest Fun value"},
		{"multiple loop binders", "    for key, value in values.map(identity) do\n    end\n", "pipeline iteration requires exactly one binder"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, base+testCase.body+"end\n", testCase.diagnostic)
		})
	}
}
