package integration

import (
	"slices"
	"strings"
	"testing"

	"hexal/compiler"
)

func TestJSONSurfaceAndGeneratedAdapterBoundary(t *testing.T) {
	source := "import\n    Json from std.json\nend\n" +
		"fun inspect(h: Heap, text: String): String | Error do\n" +
		"    let value: Json.Value = try Json.parse(h, text)\n" +
		"    let encoded: String = try Json.stringify(h, value)\n" +
		"    Json.free(h, value)\n" +
		"    return encoded\nend\n"
	result := assertCompiles(t, source)
	if !slices.Contains(result.Dependencies, compiler.RuntimeYyjson) {
		t.Fatalf("JSON adapter dependencies = %v, want yyjson", result.Dependencies)
	}
	adapter := moduleFile(t, result, "hexal/json_adapter.c")
	if strings.Count(adapter, "#include <yyjson.h>") != 1 {
		t.Fatalf("yyjson must be included by exactly one private adapter")
	}
	for name, content := range result.Files {
		if strings.HasSuffix(name, ".h") && (strings.Contains(content, "#include <yyjson.h>") || strings.Contains(content, "yyjson_")) {
			t.Errorf("public header %s exposes yyjson", name)
		}
		if strings.HasSuffix(name, ".c") && name != "hexal/json_adapter.c" && strings.Contains(content, "yyjson_") {
			t.Errorf("yyjson call escaped the private adapter into %s", name)
		}
	}
}

func TestJSONFreeOnlyDoesNotSelectYyjson(t *testing.T) {
	source := "import\n    Json from std.json\nend\n" +
		"fun release(h: Heap, value: Json.Value) do\n    Json.free(h, value)\nend\n"
	result := assertCompiles(t, source)
	if slices.Contains(result.Dependencies, compiler.RuntimeYyjson) {
		t.Fatalf("free-only program selected yyjson: %v", result.Dependencies)
	}
	for name, content := range result.Files {
		if strings.Contains(content, "#include <yyjson.h>") || strings.Contains(content, "yyjson_") {
			t.Errorf("free-only program materialized yyjson support in %s", name)
		}
	}
}

func TestJSONParseAndStringifySelectYyjsonIndividually(t *testing.T) {
	for _, source := range []string{
		"import\n Json from std.json\nend\nfun parse_only(h: Heap, text: String): Json.Value | Error do\n return Json.parse(h, text)\nend\n",
		"import\n Json from std.json\nend\nfun stringify_only(h: Heap, value: Json.Value): String | Error do\n return Json.stringify(h, value)\nend\n",
	} {
		result := assertCompiles(t, source)
		if !slices.Contains(result.Dependencies, compiler.RuntimeYyjson) {
			t.Fatalf("parse/stringify program did not select yyjson: %v", result.Dependencies)
		}
	}
}

func TestJSONRecursiveValueSurfaceAndRestrictions(t *testing.T) {
	source := "import\n    Json from std.json\nend\n" +
		"fun classify(value: Json.Value): Int32 do\n" +
		"    return match value is\n" +
		"    | Json.Value.Null then 0\n" +
		"    | Json.Value.Bool then 1\n" +
		"    | Json.Value.Int then 2\n" +
		"    | Json.Value.UInt then 3\n" +
		"    | Json.Value.Float then 4\n" +
		"    | Json.Value.Text then 5\n" +
		"    | Json.Value.Array then value.items.length().to<Int32>()\n" +
		"    | Json.Value.Object then value.entries.length().to<Int32>()\n" +
		"    end\nend\n" +
		"fun wrap(value: Json.Value): Json.Value do\n" +
		"    let items: List<Json.Value> = List<Json.Value>(Heap())\n" +
		"    items.push(value)\n" +
		"    return Json.Value.Array(items = items)\nend\n" +
		"fun inspect_task(value: Json.Value): Bool do\n" +
		"    return true\nend\n" +
		"fun pass_through_task(value: Json.Value): Bool | Error do\n" +
		"    let task: Task<Bool> = try spawn inspect_task(value)\n" +
		"    return task.join()\nend\n" +
		"fun store_value(h: Heap, value: Json.Value) do\n" +
		"    let values: Dict<Int32, Json.Value> = Dict<Int32, Json.Value>(h)\n" +
		"    values.insert(1, value)\nend\n"
	result := assertCompiles(t, source)
	if slices.Contains(result.Dependencies, compiler.RuntimeYyjson) {
		t.Fatalf("type-only Value program selected yyjson: %v", result.Dependencies)
	}
	for _, testCase := range []struct{ name, source, want string }{
		{"value equality", "import\n Json from std.json\nend\nfun f(a: Json.Value, b: Json.Value): Bool do\n return a == b\nend\n", "equality is unavailable"},
		{"value printing", "import\n Json from std.json\nend\nfun f(a: Json.Value) do\n print(a)\nend\n", "print does not support"},
		{"value dictionary key", "import\n Json from std.json\nend\nfun f(h: Heap) do\n let values: Dict<Json.Value, Int32> = Dict<Json.Value, Int32>(h)\nend\n", "dictionary key type"},
	} {
		t.Run(testCase.name, func(t *testing.T) { assertRejects(t, testCase.source, testCase.want) })
	}
}

func TestJSONTypeModeMatchRequiresEveryVariant(t *testing.T) {
	source := "import\n Json from std.json\nend\n" +
		"fun classify(value: Json.Value): Int32 do\n" +
		"    return match value is\n" +
		"    | Json.Value.Null then 0\n" +
		"    | Json.Value.Bool then 1\n" +
		"    | Json.Value.Int then 2\n" +
		"    | Json.Value.UInt then 3\n" +
		"    | Json.Value.Float then 4\n" +
		"    | Json.Value.Text then 5\n" +
		"    end\nend\n"
	assertRejects(t, source, "match is not exhaustive; missing Value.Array")
}

func TestJSONInlineStringRequiresExplicitCopy(t *testing.T) {
	source := "import\n Json from std.json\nend\n" +
		"fun text(): String<16> do\n return \"{}\"\nend\n" +
		"fun f(h: Heap) do\n let input: String<16> = text()\n let result: Json.Value | Error = Json.parse(h, input)\nend\n"
	assertRejects(t, source, "copy(heap)")
}
