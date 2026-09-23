package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
)

// A String-using program emits the hexal/string.h and hexal/string.c pair:
// the header owns the representation and the literal declarations, the source
// owns the definitions, hexal.h keeps none of the family, and the using
// module header includes only the component it needs.
func TestStringComponentEmitsHeaderAndSource(t *testing.T) {
	program := checkedGeneratorSource(t, "let greeting: String = \"hello\"\nlet farewell: String = \"bye\"\n")
	files := generateOne(t, program)
	header, exists := files["hexal/string.h"]
	if !exists {
		t.Fatalf("String program emitted no hexal/string.h: %v", files)
	}
	source, exists := files["hexal/string.c"]
	if !exists {
		t.Fatalf("String program emitted no hexal/string.c: %v", files)
	}
	if !strings.HasPrefix(header, "#ifndef HEXAL_STRING_H\n#define HEXAL_STRING_H\n") || !strings.HasSuffix(header, "\n#endif\n") {
		t.Fatalf("hexal/string.h lost its guard: %q", header)
	}
	if !strings.HasPrefix(source, "#include \"hexal/string.h\"\n") {
		t.Fatalf("hexal/string.c must include its matching header first: %q", source)
	}
	for _, include := range []string{"#include \"hexal.h\"", "#include \"hexal/heap.h\"", "#include \"hexal/slice.h\""} {
		if !strings.Contains(header, include) {
			t.Fatalf("hexal/string.h lacks declared dependency %q: %q", include, header)
		}
	}
	// Every literal declares once in the header with external const linkage
	// and defines once in the source with identical names and payload bytes,
	// in the canonical program-wide order. The header carries the byte length
	// and nothing else about the text.
	for _, declaration := range []string{
		"extern const uint8_t hex_lit_0_bytes[6];\nextern const hex_string hex_lit_0;",
		"extern const uint8_t hex_lit_1_bytes[4];\nextern const hex_string hex_lit_1;",
	} {
		if strings.Count(header, declaration) != 1 {
			t.Fatalf("hexal/string.h declares %q %d times, want once: %q", declaration, strings.Count(header, declaration), header)
		}
	}
	for _, definition := range []string{
		"const uint8_t hex_lit_0_bytes[6] = { 104, 101, 108, 108, 111, 0 };\nconst hex_string hex_lit_0 = { .data = hex_lit_0_bytes, .byte_length = 5 };",
		"const uint8_t hex_lit_1_bytes[4] = { 98, 121, 101, 0 };\nconst hex_string hex_lit_1 = { .data = hex_lit_1_bytes, .byte_length = 3 };",
	} {
		if strings.Count(source, definition) != 1 {
			t.Fatalf("hexal/string.c defines %q %d times, want once: %q", definition, strings.Count(source, definition), source)
		}
	}
	// Every read operation is a byte view, and the allocating operations
	// declare in the header and define in the source.
	if !strings.Contains(header, "static inline hex_slice_UInt8 hex_text_bytes(hex_text text) {") {
		t.Fatalf("hexal/string.h lost the inline byte-view helper: %q", header)
	}
	if !strings.Contains(header, "const hex_string *hex_string_make(hex_heap h, hex_text text);") ||
		!strings.Contains(header, "void hex_string_free(hex_heap h, const hex_string *text);") {
		t.Fatalf("hexal/string.h lost an operation declaration: %q", header)
	}
	// The UTF-8 validator is demand-driven: a literal-only program emits
	// neither its declaration nor its definition, so it acquires no utf8proc
	// dependency.
	if strings.Contains(header, "hex_utf8_valid") || strings.Contains(source, "hex_utf8_valid") {
		t.Fatalf("literal-only program emitted the UTF-8 validator: %q %q", header, source)
	}
	// The module header includes the component; the module C references the
	// program-wide literal objects.
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/string.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/string.h component include", files["modules/app.h"])
	}
	if !strings.Contains(files["modules/app.c"], "&hex_lit_0") || !strings.Contains(files["modules/app.c"], "&hex_lit_1") {
		t.Fatalf("modules/app.c = %q, want the program-wide literal references", files["modules/app.c"])
	}
	// hexal.h owns none of the String family.
	for _, forbidden := range []string{"hex_string", "hex_lit_", "hex_utf8_"} {
		if strings.Contains(files["hexal.h"], forbidden) {
			t.Fatalf("hexal.h retains String text %q: %q", forbidden, files["hexal.h"])
		}
	}
}

// The heap handle stores a byte length and a storage kind and nothing else: no
// rune count, and no generated helper computes or carries one. None of the
// removed text machinery reaches any output.
func TestStringRepresentationCarriesNoRuneState(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    let text: String = String.interpolate(h, \"n={{ 1 }}\")\n    text.free(h)\n    let inline: String<16> = \"hi\"\nend\n")
	files := generateOne(t, program)
	header := files["hexal/string.h"]
	if !strings.Contains(header, "typedef struct hex_string {\n    const uint8_t *data;\n    size_t byte_length;\n") {
		t.Fatalf("hexal/string.h lost the byte-length handle: %q", header)
	}
	for name, content := range files {
		for _, forbidden := range []string{"rune_length", "hex_rune_cursor", "hex_strand", "hex_utf8_decode", "hex_utf8_encode", "hex_utf8_next"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s carries removed text machinery %q", name, forbidden)
			}
		}
	}
}

// The storage-kind discriminator separates static literals from owned
// allocations: literals omit the zero-valued field, both allocation sites mark
// owned, and free traps on anything else.
func TestStringStorageKindDiscriminator(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    let text: String = String.interpolate(h, \"n={{ 1 }}\")\n    text.free(h)\nend\n")
	files := generateOne(t, program)
	header, exists := files["hexal/string.h"]
	if !exists {
		t.Fatalf("String program emitted no hexal/string.h: %v", files)
	}
	source, exists := files["hexal/string.c"]
	if !exists {
		t.Fatalf("String program emitted no hexal/string.c: %v", files)
	}
	for _, fragment := range []string{"HEX_STRING_STATIC = 0", "HEX_STRING_NONOWNING = 1", "HEX_STRING_OWNED = 2", "hex_string_storage_kind storage_kind;"} {
		if !strings.Contains(header, fragment) {
			t.Fatalf("hexal/string.h lacks storage-kind fragment %q", fragment)
		}
	}
	if strings.Count(source, ".storage_kind = HEX_STRING_OWNED") != 1 {
		t.Fatalf("hexal/string.c marks %d owned allocation sites, want the one shared join", strings.Count(source, ".storage_kind = HEX_STRING_OWNED"))
	}
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(line, "const hex_string hex_lit_") && strings.Contains(line, "storage_kind") {
			t.Fatalf("literal header must omit the zero-valued discriminator: %q", line)
		}
	}
	rootC, exists := files["modules/app.c"]
	if !exists {
		t.Fatalf("String program emitted no modules/app.c: %v", files)
	}
	if strings.Count(rootC, ".storage_kind = HEX_STRING_OWNED") != 1 {
		t.Fatalf("modules/app.c marks %d interpolation allocations owned, want one", strings.Count(rootC, ".storage_kind = HEX_STRING_OWNED"))
	}
	if !strings.Contains(source, "hex_runtime_trap(\"[Runtime Error] cannot free a String literal\\n\");") {
		t.Fatalf("hex_string_free lost the literal trap: %q", source)
	}
}

// A scalar-only program selects no string artifact and no module includes
// the component.
func TestStringComponentAbsentWithoutStrings(t *testing.T) {
	program := checkedGeneratorSource(t, "let x: Int32 = 1\n")
	files := generateOne(t, program)
	for _, key := range []string{"hexal/string.h", "hexal/string.c"} {
		if _, exists := files[key]; exists {
			t.Fatalf("scalar-only program emitted %s: %v", key, files)
		}
	}
	if strings.Contains(files["modules/app.h"], "hexal/string.h") {
		t.Fatalf("modules/app.h = %q, must not include an unselected component", files["modules/app.h"])
	}
}

// String use in one module selects the component for that module only; the
// unrelated module's header stays clean while the program-wide pair carries
// the machinery.
func TestStringComponentSelectionIsModuleLocal(t *testing.T) {
	parsed := make(map[string]parser.Program, 2)
	for key, source := range map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\nlet result: Int32 = Math.compute()\n",
		"math.hex": "fun compute(): Int32 do\n    let text: String = \"hello\"\n    return 1\nend\nexport\n    compute\nend\n",
	} {
		testSpanTable.Add(key, source)
		tokens, err := lexer.Lex(key, source)
		if err != nil {
			t.Fatalf("Lex(%q) error = %v", key, err)
		}
		program, err := parser.Parse(tokens)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", key, err)
		}
		parsed[key] = program
	}
	graph := moduleGraphOf("app", []string{"math", "app"}, parsed, map[string][]checker.ModuleEdge{"app": {{Alias: "Math", Target: "math"}}})
	programs, err := checker.CheckModules(graph)
	if err != nil {
		t.Fatalf("CheckModules() error = %v", err)
	}
	files, err := GenerateChecked(graph, programs, Config{SourceTable: testSpanTable})
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if _, exists := files["hexal/string.c"]; !exists {
		t.Fatalf("program-wide string pair missing: %v", files)
	}
	if !strings.Contains(files["modules/math.h"], "#include \"hexal/string.h\"") {
		t.Fatalf("modules/math.h = %q, want the component include", files["modules/math.h"])
	}
	if strings.Contains(files["modules/app.h"], "hexal/string.h") {
		t.Fatalf("modules/app.h = %q, must not include a component selected only by math", files["modules/app.h"])
	}
}

// The templates render structurally from the typed model: literal records
// become one header declaration pair and one source definition pair with the
// model's exact payload bytes, and each demanded capacity becomes one struct.
func TestStringTemplatesRenderModel(t *testing.T) {
	model := stringRenderModel{
		Inline: []inlineStringModel{{CName: "hex_string_16", Capacity: 16}},
		Literals: []stringLiteralModel{
			{Name: "hex_lit_0", Payload: []uint8{104, 105}, ArraySize: 3, PayloadLength: 2},
		},
	}
	header, err := renderComponent(componentArtifact{key: "hexal/string.h", template: "string.h", model: model})
	if err != nil {
		t.Fatalf("string.h render error = %v", err)
	}
	if !strings.Contains(header, "extern const uint8_t hex_lit_0_bytes[3];\nextern const hex_string hex_lit_0;\n") {
		t.Fatalf("hexal/string.h = %q, want the literal declarations", header)
	}
	if !strings.Contains(header, "typedef struct hex_string_16 {\n    size_t byte_length;\n    uint8_t data[16];\n} hex_string_16;\n") {
		t.Fatalf("hexal/string.h = %q, want the String<16> struct", header)
	}
	if !strings.HasSuffix(header, "\n#endif\n") {
		t.Fatalf("hexal/string.h must end with exactly one trailing newline: %q", header)
	}
	source, err := renderComponent(componentArtifact{key: "hexal/string.c", template: "string.c", model: model})
	if err != nil {
		t.Fatalf("string.c render error = %v", err)
	}
	if !strings.Contains(source, "const uint8_t hex_lit_0_bytes[3] = { 104, 105, 0 };\nconst hex_string hex_lit_0 = { .data = hex_lit_0_bytes, .byte_length = 2 };\n") {
		t.Fatalf("hexal/string.c = %q, want the literal definitions", source)
	}
	if !strings.HasSuffix(strings.TrimRight(source, "\n"), "}") {
		t.Fatalf("hexal/string.c must end with a closing brace: %q", source)
	}
	// A model with no capacity emits no struct.
	header, err = renderComponent(componentArtifact{key: "hexal/string.h", template: "string.h", model: stringRenderModel{}})
	if err != nil {
		t.Fatalf("string.h render error = %v", err)
	}
	if strings.Contains(header, "typedef struct hex_string_1") || strings.Contains(header, "typedef struct hex_string_2") || strings.Contains(header, "typedef struct hex_string_6") {
		t.Fatalf("hexal/string.h = %q, must not emit an undemanded capacity", header)
	}
}

// Equality, ordering, and hashing render into the string component when the
// model requires them, each one shared helper over the byte view; without the
// flags the helpers are absent.
func TestStringTemplatesEqualityOrderingAndHash(t *testing.T) {
	model := stringRenderModel{NeedEquality: true, NeedOrdering: true, NeedHash: true}
	header, err := renderComponent(componentArtifact{key: "hexal/string.h", template: "string.h", model: model})
	if err != nil {
		t.Fatalf("string.h render error = %v", err)
	}
	source, err := renderComponent(componentArtifact{key: "hexal/string.c", template: "string.c", model: model})
	if err != nil {
		t.Fatalf("string.c render error = %v", err)
	}
	for _, declaration := range []string{"bool hex_equal_text(hex_text left, hex_text right);", "int hex_compare_text(hex_text left, hex_text right);", "uint64_t hex_hash_text(hex_text text);"} {
		if strings.Count(header, declaration) != 1 {
			t.Fatalf("hexal/string.h declares %q %d times, want once: %q", declaration, strings.Count(header, declaration), header)
		}
	}
	for _, body := range []string{"bool hex_equal_text(hex_text left, hex_text right) {", "int hex_compare_text(hex_text left, hex_text right) {", "uint64_t hex_hash_text(hex_text text) {"} {
		if strings.Count(source, body) != 1 {
			t.Fatalf("hexal/string.c defines %q %d times, want once: %q", body, strings.Count(source, body), source)
		}
	}
	without := stringRenderModel{}
	header, _ = renderComponent(componentArtifact{key: "hexal/string.h", template: "string.h", model: without})
	source, _ = renderComponent(componentArtifact{key: "hexal/string.c", template: "string.c", model: without})
	for _, name := range []string{"hex_equal_text", "hex_compare_text", "hex_hash_text"} {
		if strings.Contains(header, name) || strings.Contains(source, name) {
			t.Fatalf("%s must be absent without its need flag", name)
		}
	}
}

// A render model missing a field referenced by the string templates fails
// closed under missingkey=error.
func TestStringTemplateMissingFieldFailsClosed(t *testing.T) {
	_, err := renderComponent(componentArtifact{
		key:      "hexal/string.h",
		template: "string.h",
		model:    struct{ Missing string }{Missing: "x"},
	})
	if err == nil {
		t.Fatal("string.h render with a model missing the template field must fail")
	}
}

// One text equality helper and one ordering helper serve every form and every
// capacity, emitted once each and never as a per-type or per-capacity variant.
func TestTextEqualityAndOrderingUseOneSharedHelper(t *testing.T) {
	program := checkedGeneratorSource(t, "let a: String = \"hello\"\nlet b: String<16> = \"world\"\nlet c: String<64> = \"world\"\nlet eq: Bool = a == b\nlet same: Bool = b == c\nlet less: Bool = a < c\nlet more: Bool = b >= c\n")
	files := generateOne(t, program)
	header, source := files["hexal/string.h"], files["hexal/string.c"]
	if strings.Count(header, "bool hex_equal_text(hex_text left, hex_text right);") != 1 ||
		strings.Count(source, "bool hex_equal_text(hex_text left, hex_text right) {") != 1 {
		t.Fatalf("hex_equal_text is not declared and defined exactly once:\n%s\n%s", header, source)
	}
	if strings.Count(header, "int hex_compare_text(hex_text left, hex_text right);") != 1 ||
		strings.Count(source, "int hex_compare_text(hex_text left, hex_text right) {") != 1 {
		t.Fatalf("hex_compare_text is not declared and defined exactly once:\n%s\n%s", header, source)
	}
	for name, content := range files {
		for _, forbidden := range []string{"hex_equal_hex_string", "hex_compare_hex_string", "hex_equal_hex_string_", "memcmp(hex_v_"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s carries a per-type text helper %q", name, forbidden)
			}
		}
	}
	if strings.Contains(files["modules/app.h"], "static bool hex_equal_text") || strings.Contains(files["modules/app.h"], "static int hex_compare_text") {
		t.Fatalf("modules/app.h must not define a static text helper: %q", files["modules/app.h"])
	}
}

// String use without a comparison emits none of the comparison helpers.
func TestStringComponentNoComparisonHelpersWithoutComparison(t *testing.T) {
	program := checkedGeneratorSource(t, "let text: String = \"hello\"\nlet inline: String<8> = \"hi\"\n")
	files := generateOne(t, program)
	for _, name := range []string{"hex_equal_text", "hex_compare_text", "hex_hash_text"} {
		if strings.Contains(files["hexal/string.h"], name) || strings.Contains(files["hexal/string.c"], name) {
			t.Fatalf("%s emitted without a comparison or a Dict key", name)
		}
	}
}
