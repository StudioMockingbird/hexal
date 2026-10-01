package integration

import (
	"fmt"
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

func TestDictEntryTypeIsCompilerOwnedAndSourceSpellable(t *testing.T) {
	result := compileSource("fun inspect(entry: DictEntry<Int32, Int32>): Int32 do\n    return entry.key + entry.value\nend\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	types := moduleFile(t, result, "hexal/types.h")
	if !strings.Contains(types, "struct hex_t_DictEntry_Int32_Int32 {") || strings.Contains(result.Files["modules/app.h"], "struct hex_t_DictEntry_Int32_Int32 {") {
		t.Fatalf("generated shared types header must define the source-spellable entry aggregate exactly once:\n%s\n%s", types, result.Files["modules/app.h"])
	}
}

func TestDictProjectionsAndEntryMaterialization(t *testing.T) {
	source := "fun entry_value(entry: DictEntry<Int32, Int32>): Int32 do\n    return entry.value\nend\n" +
		"fun run(heap: Heap) do\n" +
		"    let values: Dict<Int32, Int32> = Dict<Int32, Int32>(heap)\n" +
		"    values.insert(1, 10)\n" +
		"    for key in values.keys() do\n        print(key)\n    end\n" +
		"    for value in values.values() do\n        print(value)\n    end\n" +
		"    for entry in values.entries() do\n        print(entry.value)\n    end\n" +
		"    let entries = values.entries().map(entry_value).to_list(heap)\n" +
		"    let flat: List<Int32> = values.to_list(heap)\n" +
		"    entries.free(heap)\n    flat.free(heap)\n    values.free(heap)\nend\n"
	result := assertCompiles(t, source)
	c := rootC(t, result)
	if strings.Count(c, "for (size_t hex_pipeline_") != 5 || !strings.Contains(c, ".hex_m_key =") || strings.Count(c, "hex_list_push_Int32(hex_pipeline_") != 3 {
		t.Fatalf("loops=%d entry-init=%t flat-pushes=%d; generated C = %q", strings.Count(c, "for (size_t hex_pipeline_"), strings.Contains(c, ".hex_m_key ="), strings.Count(c, "hex_list_push_Int32(hex_pipeline_"), c)
	}
}

func TestDictEntryUsesOrdinaryAggregateOperations(t *testing.T) {
	result := assertCompiles(t, "fun demo(heap: Heap) do\n"+
		"    let dict: Dict<Int32, Int32> = Dict<Int32, Int32>(heap)\n"+
		"    dict.insert(1, 7)\n"+
		"    let entries: List<DictEntry<Int32, Int32>> = dict.entries().to_list(heap)\n"+
		"    let same: Bool = entries[0] == entries[0]\n"+
		"    print(entries[0])\n"+
		"end\n")
	if !strings.Contains(rootC(t, result), "hex_equal_hex_t_DictEntry_Int32_Int32") || !strings.Contains(rootH(t, result), "hex_print_nested_hex_t_DictEntry_Int32_Int32") {
		t.Fatalf("DictEntry did not use ordinary equality and printing support:\n%s\n%s", rootC(t, result), rootH(t, result))
	}
}

func TestDictEntryCannotBeConstructedOrUsedAsDictKey(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"let value: DictEntry<Int32, Int32> = DictEntry<Int32, Int32>(key = 1, value = 2)", "DictEntry is not a constructible type"},
		{"let values: Dict<DictEntry<Int32, Int32>, Int32> = Dict<DictEntry<Int32, Int32>, Int32>(Heap())", "dictionary key type must be Bool, an integer, Size, Rune, or String<N>"},
		{"type DictEntry<T, U> is struct key: T, value: U end", "built-in type DictEntry cannot be redeclared"},
	} {
		t.Run(testCase.want, func(t *testing.T) {
			assertRejects(t, testCase.source, testCase.want)
		})
	}
}

func TestSharedDictEntryDefinitionIsProgramWide(t *testing.T) {
	result := compiler.Compile(map[string]string{
		"app.hex": "import\n    M from \"./m\"\nend\n" +
			"let heap: Heap = Heap()\n" +
			"let dict: Dict<Int32, Int32> = Dict<Int32, Int32>(heap)\n" +
			"dict.insert(1, 7)\n" +
			"let entries: List<DictEntry<Int32, Int32>> = dict.entries().to_list(heap)\n" +
			"let answer: Int32 = entries[0].value\n",
		"m.hex": "fun read(entry: DictEntry<Int32, Int32>): Int32 do\n    return entry.value\nend\n",
	}, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile stderr = %#v", result.Stderr)
	}
	types := moduleFile(t, result, "hexal/types.h")
	if strings.Count(types, "struct hex_t_DictEntry_Int32_Int32 {") != 1 {
		t.Fatalf("shared types header must define the specialization once:\n%s", types)
	}
	for _, header := range []string{"modules/app.h", "modules/m.h"} {
		content := moduleFile(t, result, header)
		if !strings.Contains(content, "#include \"hexal/types.h\"") || strings.Contains(content, "struct hex_t_DictEntry_Int32_Int32 {") {
			t.Fatalf("%s must use the shared definition without redefining it:\n%s", header, content)
		}
	}
}

func TestModuleOwnedDictEntryDefinitionStaysWithConsumer(t *testing.T) {
	for _, body := range []string{
		"fun inspect(entry: DictEntry<Int32, M.Point>): Int32 do\n    return entry.value.x\nend\n",
		"let heap: Heap = Heap()\nlet dict: Dict<Int32, M.Point> = Dict<Int32, M.Point>(heap)\n",
		"let heap: Heap = Heap()\nlet dict: Dict<Int32, M.Point> = Dict<Int32, M.Point>(heap)\nfor entry in dict.entries() do\n    let x: Int32 = entry.value.x\nend\n",
	} {
		result := compiler.Compile(map[string]string{
			"app.hex": "import\n    M from \"./m\"\nend\n" + body,
			"m.hex":   "type Point is struct x: Int32 end\nexport\n    Point\nend\n",
		}, "app.hex", compiler.Project{})
		if result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) stderr = %#v", body, result.Stderr)
		}
	}
	result := compiler.Compile(map[string]string{
		"app.hex": "import\n    M from \"./m\"\nend\n" +
			"let heap: Heap = Heap()\n" +
			"let dict: Dict<Int32, M.Point> = Dict<Int32, M.Point>(heap)\n" +
			"for entry in dict.entries() do\n    let x: Int32 = entry.value.x\nend\n",
		"m.hex": "type Point is struct x: Int32 end\nexport\n    Point\nend\n",
	}, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile stderr = %#v", result.Stderr)
	}
	if _, exists := result.Files["hexal/types.h"]; exists {
		t.Fatalf("module-owned entry specialization must not enter the shared type header: %v", result.Files)
	}
	header := moduleFile(t, result, "modules/app.h")
	if !strings.Contains(header, "struct hex_t_DictEntry_Int32_m1_m_Point {") {
		t.Fatalf("consumer module header lacks its module-owned DictEntry specialization:\n%s", header)
	}
}

func TestDictFindReturnsOptionalAndProbesOnce(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    let hit: Int32 | Nil = scores.find(1)\n    if hit != nil then\n        let value: Int32 = hit\n    end\n    let miss: Int32 | Nil = scores.find(2)\n    if miss == nil then\n        let absent: Int32 = 0\n    end\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	root := rootC(t, result)
	if strings.Count(root, "hex_dict_find_Int32_Int32(hex_v_scores") != 2 {
		t.Fatalf("generated modules/app.c = %q, want one find helper call per source find", root)
	}
	if strings.Contains(root, "hex_dict_contains_Int32_Int32(hex_v_scores") {
		t.Fatalf("generated modules/app.c = %q, find must not lower to contains", root)
	}
	if !strings.Contains(dictH(t, result), "static inline const int32_t *hex_dict_find_Int32_Int32") {
		t.Fatalf("generated hexal/dict.h = %q, want pointer-returning find helper", dictH(t, result))
	}
	if !strings.Contains(root, "hex_dict_find_1 == nullptr") || !strings.Contains(root, "hex_dict_find_2 == nullptr") {
		t.Fatalf("generated modules/app.c = %q, want Nil construction from hoisted find results", root)
	}
}

func TestDictFindRequiresNarrowing(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    let bad: Int32 = scores.find(1)\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "expected Int32 initializer; got Int32 | Nil") {
		t.Fatalf("Compile stderr = %#v, want narrowing diagnostic", result.Stderr)
	}
}

func TestDictFindPreservesUnionValues(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32 | Bool> = Dict<Int32, Int32 | Bool>(h)\n    defer scores.free(h)\n    let value: Int32 | Bool = 10\n    scores.insert(1, value)\n    let found: Int32 | Bool | Nil = scores.find(1)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestDictInt32Lifecycle(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    let present: Bool = scores.contains(1)\n    let first: Int32 = scores.get(1)\n    let removed: Int32 = scores.remove(2)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_dict_entry_Int32_Int32 {",
		"bool active;",
		"int32_t key;",
		"int32_t value;",
		"hex_v_scores = hex_dict_new_Int32_Int32(hex_v_h);",
		"hex_dict_insert_Int32_Int32(hex_v_scores, 1, 10);",
		"hex_dict_contains_Int32_Int32(hex_v_scores, 1)",
		"hex_v_first = hex_dict_get_Int32_Int32(hex_v_scores, 1);",
		"hex_v_removed = hex_dict_remove_Int32_Int32(hex_v_scores, 2);",
		"hex_dict_free_Int32_Int32(hex_defer_capture_2, hex_defer_capture_1);",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) && !strings.Contains(dictH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
	// The dict machinery lives in the dict component; hexal.h
	// owns none of it.
	if strings.Contains(hexalH(t, result), "hex_dict_") {
		t.Fatalf("hexal.h = %q, dict definitions must live in hexal/dict.h", hexalH(t, result))
	}
	// Capacity doubling and bucket-region byte sizing stay in size_t with
	// checked multiply, and the load-factor growth decision checks every
	// operand before comparison; the manual SIZE_MAX guard and the uint64_t
	// temporary are gone.
	// A fresh inactive bucket region zeroes with one memset, every diagnostic
	// reports through hex_runtime_trap, and the Dict helpers carry no raw
	// fputs or compiler-owned NULL.
	header := dictH(t, result)
	for _, want := range []string{
		"size_t next = 8;",
		"ckd_mul(&next, dict->capacity, 2)",
		"ckd_mul(&bytes, next, sizeof(hex_dict_entry_Int32_Int32))",
		"ckd_add(&length_plus_one, dict->length, 1)",
		"ckd_mul(&load_times_10, length_plus_one, 10)",
		"ckd_mul(&capacity_times_7, dict->capacity, 7)",
		"hex_heap_allocate_zeroed(1, bytes);",
		"hex_runtime_trap(\"[Runtime Error] dictionary key not found\\n\")",
		"hex_runtime_trap(\"[Runtime Error] dictionary capacity is not representable\\n\")",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal/dict.h does not contain %q:\n%s", want, header)
		}
	}
	for _, forbid := range []string{"uint64_t next", "SIZE_MAX /", "(dict->length + 1) * 10 >= dict->capacity * 7", "fputs(", "NULL", "region[index].active = false", "memset(region", "->allocator", "h.identity"} {
		if strings.Contains(header, forbid) {
			t.Fatalf("hexal/dict.h retains %q:\n%s", forbid, header)
		}
	}
}

func TestDictClearRetainsStorageAndInvalidatesTraversal(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer d.free(h)\n    d.insert(1, 10)\n    for key: Int32, value: Int32 in d do\n        d.clear()\n    end\nend")
	if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "cannot mutate collection during iteration") {
		t.Fatalf("Compile stderr = %#v, want active traversal invalidation diagnostic", result.Stderr)
	}
	result = compileSource("fun demo(h: Heap) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer d.free(h)\n    d.insert(1, 10)\n    d.clear()\n    let empty: Size = d.length()\n    d.insert(2, 20)\n    let value: Int32 = d.get(2)\n    d.clear()\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	header := dictH(t, result)
	clearAt := strings.Index(header, "static inline void hex_dict_clear_Int32_Int32")
	if clearAt < 0 {
		t.Fatalf("hexal/dict.h lacks clear helper:\\n%s", header)
	}
	clearEnd := strings.Index(header[clearAt:], "\n}\n")
	if clearEnd < 0 {
		t.Fatalf("cannot find the end of the generated clear helper: %s", header[clearAt:])
	}
	clear := header[clearAt : clearAt+clearEnd+2]
	for _, want := range []string{"index < dict->capacity", "dict->buckets[index].active = false;", "dict->length = 0;", "dict->version++;"} {
		if !strings.Contains(clear, want) {
			t.Fatalf("clear helper lacks %q:\\n%s", want, clear)
		}
	}
	if strings.Contains(clear, "hex_heap_") || strings.Contains(clear, "free(") || strings.Contains(clear, "dict->buckets =") || strings.Contains(clear, "dict->capacity =") {
		t.Fatalf("clear helper releases storage or referents:\\n%s", clear)
	}
	stringValues := compileSource(`fun demo(h: Heap) do
    let d: Dict<Int32, String> = Dict<Int32, String>(h)
    defer d.free(h)
    let value: String = "owned".copy(h)
    defer value.free(h)
    d.insert(1, value)
    d.clear()
end`)
	if stringValues.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile String-valued Dict exit code = %d (%v), want %d", stringValues.ExitCode, stringValues.Stderr, compiler.ExitSuccess)
	}
	if strings.Contains(dictH(t, stringValues), "hex_string_free") {
		t.Fatalf("Dict.clear emitted cleanup for its String referents:\\n%s", dictH(t, stringValues))
	}
}

func TestDictScalarKeyFamilies(t *testing.T) {
	type testKey struct {
		name, literal string
	}
	keys := []testKey{
		{"Bool", "true"}, {"Int8", "-1"}, {"Int16", "-2"}, {"Int32", "-3"}, {"Int64", "-4"},
		{"UInt8", "5"}, {"UInt16", "6"}, {"UInt32", "7"}, {"UInt64", "8"}, {"Size", "9"}, {"Rune", "'x'"},
		{"String<16>", `"key"`},
	}
	var builder strings.Builder
	builder.WriteString("fun retrieve<K, V>(dict: Dict<K, V>, key: K): V do\n    return dict.get(key)\nend\nfun demo(h: Heap) do\n")
	for index, key := range keys {
		name := fmt.Sprintf("d%d", index)
		fmt.Fprintf(&builder, "    let %s: Dict<%s, Int32> = Dict<%s, Int32>(h)\n", name, key.name, key.name)
		fmt.Fprintf(&builder, "    defer %s.free(h)\n", name)
		fmt.Fprintf(&builder, "    let key%d: %s = %s\n", index, key.name, key.literal)
		fmt.Fprintf(&builder, "    %s.insert(key%d, %d)\n", name, index, index)
		fmt.Fprintf(&builder, "    let generic%d: Int32 = retrieve<%s, Int32>(%s, key%d)\n", index, key.name, name, index)
		fmt.Fprintf(&builder, "    let found%d: Int32 | Nil = %s.find(key%d)\n", index, name, index)
		fmt.Fprintf(&builder, "    let present%d: Bool = %s.contains(key%d)\n", index, name, index)
		fmt.Fprintf(&builder, "    let value%d: Int32 = %s.get(key%d)\n", index, name, index)
		fmt.Fprintf(&builder, "    let removed%d: Int32 = %s.remove(key%d)\n", index, name, index)
	}
	builder.WriteString("    let byte_key: Dict<Byte, Int32> = Dict<Byte, Int32>(h)\n    defer byte_key.free(h)\n    let u8_key: Dict<UInt8, Int32> = byte_key\n    let alias: Int32 | Nil = u8_key.find(1)\n    let generic_alias: Int32 = retrieve<Byte, Int32>(u8_key, 1)\nend\n")
	source := builder.String()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	header := dictH(t, result)
	if strings.Count(header, "static inline uint64_t hex_hash_scalar(") != 1 {
		t.Fatalf("scalar hash helper must be emitted once:\\n%s", header)
	}
	for _, invalid := range []string{"Float32", "Float64", "EoS"} {
		if strings.Contains(header, "hex_dict_entry_"+invalid) {
			t.Fatalf("unexpected Dict specialization for %q:\\n%s", invalid, header)
		}
	}
	wrongWidth := compileSource("fun demo(h: Heap) do\n    let d: Dict<Int16, Int32> = Dict<Int16, Int32>(h)\n    let key: Int8 = 1\n    let x: Int32 = d.get(key)\nend\n")
	if wrongWidth.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(wrongWidth.Stderr, "\n"), "dictionary key requires Int16; got Int8") {
		t.Fatalf("cross-width lookup stderr = %#v, want exact key-type rejection", wrongWidth.Stderr)
	}
	invalidSpecialization := compileSource(`type Point is struct x: Int32 end
fun make_dict<K>(h: Heap, key: K) do
    let d: Dict<K, Int32> = Dict<K, Int32>(h)
    defer d.free(h)
    d.insert(key, 1)
end
fun demo(h: Heap) do
    make_dict<Point>(h, Point(x = 1))
end`)
	if invalidSpecialization.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(invalidSpecialization.Stderr, "\n"), "dictionary key type must be Bool, an integer, Size, Rune, or String<N>") {
		t.Fatalf("invalid generic key specialization stderr = %#v, want key-type rejection", invalidSpecialization.Stderr)
	}
}

func TestFreedCollectionBindingsAreRejectedLocally(t *testing.T) {
	for _, operation := range []string{"length()", "clear()", "insert(1, 2)", "get(1)", "find(1)", "contains(1)", "remove(1)"} {
		source := "fun demo(h: Heap) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    d.free(h)\n    d." + operation + "\nend"
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "released on every path") {
			t.Fatalf("Compile(%q) stderr = %#v, want use-after-free diagnostic", source, result.Stderr)
		}
	}
	for _, operation := range []string{"free(h)", "length()", "slice(0, 0)", "mut_slice(0, 0)", "push(1)", "clear()", "pop()"} {
		source := "fun demo(h: Heap) do\n    let xs: List<Int32> = List<Int32>(h)\n    xs.free(h)\n    xs." + operation + "\nend"
		result := compileSource(source)
		want := "released on every path"
		if operation == "free(h)" {
			want = "free releases storage already released"
		}
		if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", source, result.Stderr, want)
		}
	}
	for _, source := range []string{
		"fun demo(h: Heap) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    d.free(h)\n    d.free(h)\nend",
		"fun demo(h: Heap) do\n    let xs: List<Int32> = List<Int32>(h)\n    xs.free(h)\n    let value: Int32 = xs[0]\nend",
		"fun demo(h: Heap) do\n    let xs: List<Int32> = List<Int32>(h)\n    xs.free(h)\n    for value: Int32 in xs do\n    end\nend",
		"fun demo(h: Heap) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    d.free(h)\n    for k: Int32, v: Int32 in d do\n    end\nend",
		"fun demo(h: Heap, release: Bool) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    if release then\n        d.free(h)\n    else\n        d.free(h)\n    end\n    d.length()\nend",
	} {
		result := compileSource(source)
		want := "released on every path"
		if strings.Contains(source, ".free(h)\n    d.free(h)") {
			want = "free releases storage already released"
		}
		if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", source, result.Stderr, want)
		}
	}
	for _, source := range []string{
		"fun demo(h: Heap, release: Bool) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    if release then\n        d.free(h)\n    end\n    d.length()\nend",
		"fun demo(h: Heap) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer d.free(h)\n    d.length()\nend",
		"fun demo(h: Heap) do\n    let mut d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    d.free(h)\n    d = Dict<Int32, Int32>(h)\n    d.length()\n    d.free(h)\nend",
		"fun demo(h: Heap) do\n    let d: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    let alias: Dict<Int32, Int32> = d\n    d.free(h)\n    alias.length()\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestDictStringKeys(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let labels: Dict<String<128>, Int32> = Dict<String<128>, Int32>(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    labels.insert(\"bob\", 2)\n    let present: Bool = labels.contains(\"alice\")\n    let score: Int32 = labels.get(\"bob\")\n    let key: String<128> = \"carol\"\n    labels.insert(key, 3)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct " + compilerTypes.ErrorHeaderText.CName + " {",
		"uint8_t data[128];",
		"hex_dict_insert_String_128__Int32(hex_v_labels, (" + compilerTypes.ErrorHeaderText.CName + "){ .byte_length = 5, .data = { 97, 108, 105, 99, 101, } }, 1);",
		"hex_dict_contains_String_128__Int32(hex_v_labels, (" + compilerTypes.ErrorHeaderText.CName + "){ .byte_length = 5, .data = { 97, 108, 105, 99, 101, } })",
		"hex_v_score = hex_dict_get_String_128__Int32(hex_v_labels, (" + compilerTypes.ErrorHeaderText.CName + "){ .byte_length = 3, .data = { 98, 111, 98, } });",
		"hex_dict_insert_String_128__Int32(hex_v_labels, hex_v_key, 3);",
		"hex_hash_text",
	} {
		// The key struct lives in the string component; the dict call sites
		// stay in the module files and the probing lives in the dict component.
		all := rootC(t, result) + rootH(t, result) + hexalH(t, result) + result.Files["hexal/string.h"] + dictH(t, result)
		if !strings.Contains(all, want) {
			t.Fatalf("generated output = %q, want %q", all, want)
		}
	}
	// Text Dict probing compares the logical bytes through the shared helper,
	// hashes only those bytes, and emits no per-Dict key-equality wrapper or
	// per-key-type hash; diagnostics report through hex_runtime_trap and no
	// compiler-owned NULL or raw fputs remains.
	header := dictH(t, result)
	for _, want := range []string{
		"hex_hash_text(hex_text_inline(&key))",
		"!hex_equal_text(hex_text_inline(&region[index].key), hex_text_inline(&key))",
		"!hex_equal_text(hex_text_inline(&dict->buckets[index].key), hex_text_inline(&key))",
		"hex_runtime_trap(\"[Runtime Error] dictionary key not found\\n\")",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal/dict.h does not contain %q:\n%s", want, header)
		}
	}
	for _, forbid := range []string{"hex_dict_key_equal_", "fputs(", "NULL", "hex_hash_Strand", "memcmp("} {
		if strings.Contains(header, forbid) {
			t.Fatalf("hexal/dict.h retains %q:\n%s", forbid, header)
		}
	}
}

// Text keys of any capacity are valid, and different capacities are different
// types with separate specializations in one program.
func TestDictKeyCapacitiesAreDistinctTypes(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let a: Dict<String<16>, Int32> = Dict<String<16>, Int32>(h)\n    defer a.free(h)\n    let b: Dict<String<128>, Int32> = Dict<String<128>, Int32>(h)\n    defer b.free(h)\n    a.insert(\"x\", 1)\n    b.insert(\"x\", 2)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	header := dictH(t, result)
	for _, want := range []string{"hex_dict_insert_String_16__Int32", "hex_dict_insert_String_128__Int32"} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal/dict.h lacks %s:\n%s", want, header)
		}
	}
	// The shared helpers exist once however many capacities are keyed.
	for _, once := range []string{"hex_hash_text(const", "hex_equal_text(const"} {
		if count := strings.Count(hexalH(t, result)+dictH(t, result)+result.Files["hexal/string.h"], once); count > 2 {
			t.Fatalf("%s is defined %d times", once, count)
		}
	}
}

// Only scalar values and String<N> are keys; the heap String, which does not own its
// bytes, and every other type report their own diagnostic. Literal keys are
// measured against the key capacity and other capacities are converted by hand.
func TestDictKeyDiagnostics(t *testing.T) {
	long128 := strings.Repeat("a", 128)
	assertKey := func(source, want string) {
		t.Helper()
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", source, result.Stderr, want)
		}
	}
	assertKey("fun demo(h: Heap) do\n    let d: Dict<String, Int32> = Dict<String, Int32>(h)\nend", "dictionary key type String is not allowed: a Dict stores its keys, and String does not own its bytes; use String<N>")
	assertKey("fun demo(h: Heap) do\n    let d: Dict<Float64, Int32> = Dict<Float64, Int32>(h)\nend", "dictionary key type must be Bool, an integer, Size, Rune, or String<N>")
	for _, invalid := range []string{
		"Float32", "EoS", "Nil", "Heap", "Ptr<Int32>", "Slice<Int32>", "List<Int32>",
		"Dict<Int32, Int32>", "Fun<(Int32) : Int32>", "Int32 | Bool",
	} {
		source := "fun demo(h: Heap) do\n    let d: Dict<" + invalid + ", Int32> = Dict<" + invalid + ", Int32>(h)\nend"
		assertKey(source, "type.dict-key-type-invalid] dictionary key type must be Bool, an integer, Size, Rune, or String<N>")
	}
	assertKey("type Point is struct x: Int32 end\nfun demo(h: Heap) do\n    let d: Dict<Point, Int32> = Dict<Point, Int32>(h)\nend", "type.dict-key-type-invalid]")
	assertKey("type Choice is union | A | B end\nfun demo(h: Heap) do\n    let d: Dict<Choice, Int32> = Dict<Choice, Int32>(h)\nend", "type.dict-key-type-invalid]")
	assertKey("fun demo(h: Heap) do\n    let d: Dict<String<128>, Int32> = Dict<String<128>, Int32>(h)\n    d.insert(\""+long128+"b\", 1)\nend", "String<128> literal exceeds 128 UTF-8 bytes")
	for _, call := range []string{"insert(key, 1)", "get(key)", "find(key)", "contains(key)", "remove(key)"} {
		assertKey("fun demo(h: Heap) do\n    let d: Dict<String<128>, Int32> = Dict<String<128>, Int32>(h)\n    let key: String<16> = \"x\"\n    d."+call+"\nend", "dictionary key requires String<128>; got String<16>; use widen<128>()")
	}
	assertKey("fun demo(h: Heap) do\n    let d: Dict<String<16>, Int32> = Dict<String<16>, Int32>(h)\n    let key: String<128> = \"x\"\n    d.insert(key, 1)\nend", "dictionary key requires String<16>; got String<128>; use bytes().to<String<16>>() for a checked conversion")
	// The exact-capacity literal and the widened key are accepted.
	if result := compileSource("fun demo(h: Heap) do\n    let d: Dict<String<128>, Int32> = Dict<String<128>, Int32>(h)\n    defer d.free(h)\n    d.insert(\"" + long128 + "\", 1)\n    let key: String<16> = \"x\"\n    d.insert(key.widen<128>(), 2)\nend"); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

// Iterating a text-keyed Dict binds the key at its capacity; the annotation
// must agree with it, and the traversal is the same version-checked one.
func TestDictTextKeyIteration(t *testing.T) {
	prefix := "fun demo(h: Heap) do\n    let d: Dict<String<128>, Int32> = Dict<String<128>, Int32>(h)\n    defer d.free(h)\n"
	result := compileSource(prefix + "    for k: String<128>, v: Int32 in d do\n    end\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_dict_version") && !strings.Contains(rootC(t, result), "version") {
		t.Fatalf("the Dict traversal lost its version check:\n%s", rootC(t, result))
	}
	result = compileSource(prefix + "    for k: String<16>, v: Int32 in d do\n    end\nend")
	if result.ExitCode != compiler.ExitFailure || !strings.Contains(strings.Join(result.Stderr, "\n"), "for binder k is annotated String<16>, but Dict<String<128>, Int32> yields String<128> there") {
		t.Fatalf("stderr = %#v, want the agreement diagnostic", result.Stderr)
	}
}

func TestDictStringValues(t *testing.T) {
	// A stored literal is never freed by the collection or by a remove; a
	// runtime String removed from the dict is freed explicitly.
	result := compileSource("fun demo(h: Heap) do\n    let people: Dict<Int32, String> = Dict<Int32, String>(h)\n    defer people.free(h)\n    people.insert(1, \"alice\")\n    let runtime: String = \"bob\".copy(h)\n    people.insert(2, runtime)\n    let removed: String = people.remove(2)\n    removed.free(h)\n    people.insert(1, \"carol\")\n    let name: String = people.get(1)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_dict_insert_Int32_String(hex_v_people, 1, &hex_lit_0);",
		"hex_dict_insert_Int32_String(hex_v_people, 2, hex_v_runtime);",
		"hex_v_name = hex_dict_get_Int32_String(hex_v_people, 1);",
		"hex_v_removed = hex_dict_remove_Int32_String(hex_v_people, 2);",
		"hex_string_free(hex_v_h, hex_v_removed);",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestDictFindSupportsNullableHandleValues(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let people: Dict<Int32, String> = Dict<Int32, String>(h)\n    defer people.free(h)\n    people.insert(1, \"alice\")\n    let maybe: String | Nil = people.find(1)\n    if maybe != nil then\n        let name: String = maybe\n    end\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_dict_find_Int32_String(hex_v_people, 1)") {
		t.Fatalf("generated modules/app.c = %q, want String find helper call", rootC(t, result))
	}
}

// A Dict<String> read returns a shallow String handle; mutation while a
// read handle is live is the programmer's responsibility.
func TestDictMutationAfterReadIsValid(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    let people: Dict<Int32, String> = Dict<Int32, String>(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    let name: String = people.get(1)\n    people.insert(2, \"b\")\nend",
		"fun demo(h: Heap) do\n    let people: Dict<Int32, String> = Dict<Int32, String>(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    let name: String = people.get(1)\n    let removed: String = people.remove(1)\nend",
		"fun demo(h: Heap) do\n    let people: Dict<Int32, String> = Dict<Int32, String>(h)\n    people.insert(1, \"a\")\n    let name: String = people.get(1)\n    people.free(h)\nend",
		"fun demo(h: Heap) do\n    let mut people: Dict<Int32, String> = Dict<Int32, String>(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    let name: String = people.get(1)\n    people = Dict<Int32, String>(h)\nend",
		"fun inspect(people: Dict<Int32, String>) do\nend\nfun demo(h: Heap) do\n    let people: Dict<Int32, String> = Dict<Int32, String>(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    let name: String = people.get(1)\n    inspect(people)\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestDictBorrowAllowsLookups(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let people: Dict<Int32, String> = Dict<Int32, String>(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    let name: String = people.get(1)\n    let present: Bool = people.contains(1)\n    let other: String = people.get(1)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestDictShallowCopySemantics(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\nend",
		"fun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer scores.free(h)\n    let other: Dict<Int32, Int32> = scores\nend",
		"fun demo(h: Heap, scores: Dict<Int32, Int32>) do\n    scores.free(h)\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo(h: Heap) do\n    let scores: Dict<Float64, Int32> = Dict<Float64, Int32>(h)\nend", "dictionary key type must be Bool, an integer, Size, Rune, or String<N>"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestDictReturnHandoff(t *testing.T) {
	result := compileSource("fun make_scores(h: Heap): Dict<Int32, Int32> do\n    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    scores.insert(1, 10)\n    return scores\nend\nfun demo(h: Heap) do\n    let scores: Dict<Int32, Int32> = make_scores(h)\n    scores.free(h)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// dictH returns the generated hexal/dict.h component artifact.
