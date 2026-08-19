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
	program := checkedGeneratorSource(t, "greeting: String = \"hello\"\nfarewell: String = \"bye\"\n")
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
	for _, include := range []string{"#include \"hexal.h\"", "#include \"hexal/heap.h\"", "#include \"hexal/view.h\""} {
		if !strings.Contains(header, include) {
			t.Fatalf("hexal/string.h lacks declared dependency %q: %q", include, header)
		}
	}
	// Every literal declares once in the header with external const linkage
	// and defines once in the source with identical names and payload bytes,
	// in the canonical program-wide order.
	for _, declaration := range []string{
		"extern const uint8_t hex_lit_0_bytes[6];\nextern const hex_string hex_lit_0;",
		"extern const uint8_t hex_lit_1_bytes[4];\nextern const hex_string hex_lit_1;",
	} {
		if strings.Count(header, declaration) != 1 {
			t.Fatalf("hexal/string.h declares %q %d times, want once: %q", declaration, strings.Count(header, declaration), header)
		}
	}
	for _, definition := range []string{
		"const uint8_t hex_lit_0_bytes[6] = { 104, 101, 108, 108, 111, 0 };\nconst hex_string hex_lit_0 = { .data = hex_lit_0_bytes, .byte_length = 5, .rune_length = 5 };",
		"const uint8_t hex_lit_1_bytes[4] = { 98, 121, 101, 0 };\nconst hex_string hex_lit_1 = { .data = hex_lit_1_bytes, .byte_length = 3, .rune_length = 3 };",
	} {
		if strings.Count(source, definition) != 1 {
			t.Fatalf("hexal/string.c defines %q %d times, want once: %q", definition, strings.Count(source, definition), source)
		}
	}
	// The byte-view helpers are typed through the View specialization, so
	// they stay inline in the header; the non-specialized operations declare
	// there and define in the source.
	if !strings.Contains(header, "static inline hex_view_UInt8 hex_string_bytes(const hex_string *text) {") {
		t.Fatalf("hexal/string.h lost the inline byte-view helper: %q", header)
	}
	if !strings.Contains(header, "const hex_string *hex_string_from_bytes(hex_heap h, const uint8_t *data, size_t length);") ||
		!strings.Contains(header, "void hex_string_free(hex_heap h, const hex_string *text);") {
		t.Fatalf("hexal/string.h lost an operation declaration: %q", header)
	}
	if !strings.Contains(source, "const hex_string *hex_string_from_bytes(hex_heap h, const uint8_t *data, size_t length) {") {
		t.Fatalf("hexal/string.c lost the from_bytes body: %q", source)
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
	for _, forbidden := range []string{"hex_string", "hex_strand", "hex_lit_", "hex_utf8_", "hex_rune_cursor"} {
		if strings.Contains(files["hexal.h"], forbidden) {
			t.Fatalf("hexal.h retains String text %q: %q", forbidden, files["hexal.h"])
		}
	}
}

// Strand selection adds the hex_strand representation and the strand
// operations to the pair; a String-only program keeps the strand surface out.
func TestStringComponentStrandSurface(t *testing.T) {
	program := checkedGeneratorSource(t, "label: Strand = \"hexal\"\n")
	files := generateOne(t, program)
	header, source := files["hexal/string.h"], files["hexal/string.c"]
	if !strings.Contains(header, "typedef struct hex_strand {\n    uint8_t data[32];\n} hex_strand;") {
		t.Fatalf("hexal/string.h = %q, want the hex_strand representation", header)
	}
	if !strings.Contains(header, "size_t hex_strand_rune_length(hex_strand text);") {
		t.Fatalf("hexal/string.h = %q, want the strand operation declarations", header)
	}
	if !strings.Contains(source, "size_t hex_strand_rune_length(hex_strand text) {") ||
		!strings.Contains(source, "const hex_string *hex_strand_to_string(hex_heap h, hex_strand text) {") {
		t.Fatalf("hexal/string.c = %q, want the strand operation bodies", source)
	}

	program = checkedGeneratorSource(t, "text: String = \"x\"\n")
	files = generateOne(t, program)
	if strings.Contains(files["hexal/string.h"], "hex_strand") {
		t.Fatalf("String-only hexal/string.h carries the strand surface: %q", files["hexal/string.h"])
	}
	if strings.Contains(files["hexal/string.c"], "hex_strand_") {
		t.Fatalf("String-only hexal/string.c carries the strand surface: %q", files["hexal/string.c"])
	}
}

// A scalar-only program selects no string artifact and no module includes
// the component.
func TestStringComponentAbsentWithoutStrings(t *testing.T) {
	program := checkedGeneratorSource(t, "x: Int32 = 1\n")
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
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.compute()\n",
		"math.hex": "export fun compute(): Int32 do\n    text: String = \"hello\"\n    return 1\nend\n",
	} {
		tokens, err := lexer.Lex(source)
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
	files, err := GenerateChecked(graph, programs, Config{})
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

// Equivalent compilations render identical string artifacts.
// The templates render structurally from the typed model: literal records
// become one header declaration pair and one source definition pair with the
// model's exact payload bytes, and the Strand requirement drives the
// conditional sections.
func TestStringTemplatesRenderModel(t *testing.T) {
	model := stringRenderModel{
		NeedStrand: true,
		Literals: []stringLiteralModel{
			{Name: "hex_lit_0", Payload: []uint8{104, 105}, ArraySize: 3, PayloadLength: 2, RuneLength: 2},
		},
	}
	header, err := renderComponent(componentArtifact{key: "hexal/string.h", template: "string.h", model: model})
	if err != nil {
		t.Fatalf("string.h render error = %v", err)
	}
	if !strings.Contains(header, "extern const uint8_t hex_lit_0_bytes[3];\nextern const hex_string hex_lit_0;\n") {
		t.Fatalf("hexal/string.h = %q, want the literal declarations", header)
	}
	if !strings.Contains(header, "typedef struct hex_strand {") {
		t.Fatalf("hexal/string.h = %q, want the strand typedef", header)
	}
	if !strings.HasSuffix(header, "\n#endif\n") {
		t.Fatalf("hexal/string.h must end with exactly one trailing newline: %q", header)
	}
	source, err := renderComponent(componentArtifact{key: "hexal/string.c", template: "string.c", model: model})
	if err != nil {
		t.Fatalf("string.c render error = %v", err)
	}
	if !strings.Contains(source, "const uint8_t hex_lit_0_bytes[3] = { 104, 105, 0 };\nconst hex_string hex_lit_0 = { .data = hex_lit_0_bytes, .byte_length = 2, .rune_length = 2 };\n") {
		t.Fatalf("hexal/string.c = %q, want the literal definitions", source)
	}
	if !strings.Contains(source, "size_t hex_strand_rune_length(hex_strand text) {") {
		t.Fatalf("hexal/string.c = %q, want the strand bodies", source)
	}
	// The same model without the strand requirement drops both sections.
	withoutStrand := model
	withoutStrand.NeedStrand = false
	header, err = renderComponent(componentArtifact{key: "hexal/string.h", template: "string.h", model: withoutStrand})
	if err != nil {
		t.Fatalf("string.h render error = %v", err)
	}
	if strings.Contains(header, "hex_strand") {
		t.Fatalf("hexal/string.h = %q, must not spell the strand surface", header)
	}
	source, err = renderComponent(componentArtifact{key: "hexal/string.c", template: "string.c", model: withoutStrand})
	if err != nil {
		t.Fatalf("string.c render error = %v", err)
	}
	if strings.Contains(source, "hex_strand_") {
		t.Fatalf("hexal/string.c = %q, must not spell the strand surface", source)
	}
	if !strings.HasSuffix(source, "}\n") {
		t.Fatalf("hexal/string.c must end with exactly one trailing newline: %q", source)
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
