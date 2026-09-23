package generator

import (
	"strings"
	"testing"

	"hexal/compiler/specdata"
)

// The lowering must read each built-in method's recorded RuntimeSymbol rather
// than restate it. The program below emits the list push call through the
// registry; corrupting that record's symbol must move the emitted call site.
func TestBuiltinMethodLoweringReadsTheRegistry(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\nend")
	baseline := generateOne(t, program)
	if !strings.Contains(baseline["modules/app.c"], "hex_list_push_Int32(") {
		t.Fatalf("modules/app.c does not call the recorded list push symbol: %q", baseline["modules/app.c"])
	}

	original := builtinMethodRecord
	builtinMethodRecord = func(owner specdata.TypePattern, name string) (specdata.MethodSpec, bool) {
		if name == "push" {
			return specdata.MethodSpec{Owner: owner, Name: name, RuntimeSymbol: "hex_probe_push_%s"}, true
		}
		return original(owner, name)
	}
	defer func() { builtinMethodRecord = original }()

	corrupted := generateOne(t, program)
	if !strings.Contains(corrupted["modules/app.c"], "hex_probe_push_Int32(") {
		t.Fatalf("modules/app.c ignored the corrupted record: %q", corrupted["modules/app.c"])
	}
	if strings.Contains(corrupted["modules/app.c"], "hex_list_push_Int32(") {
		t.Fatalf("modules/app.c kept the old symbol after the record changed: %q", corrupted["modules/app.c"])
	}
}

// The deferred path lowers through renderDeferredCall, not the direct method
// dispatch. It must read the same recorded RuntimeSymbol: corrupting every
// deferred record must move each deferred call site in the module C file and
// leave none of the old symbols behind.
func TestDeferredBuiltinMethodLoweringReadsTheRegistry(t *testing.T) {
	program := checkedGeneratorSource(t, "fun worker(): Bool do\n"+
		"    return true\n"+
		"end\n"+
		"fun run(): Int32 | Error do\n"+
		"    let h: Heap = Heap()\n"+
		"    let values: List<Int32> = List<Int32>(h)\n"+
		"    defer values.free(h)\n"+
		"    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n"+
		"    defer scores.free(h)\n"+
		"    let channel: Channel<Int32> = try Channel<Int32>(h, 1)\n"+
		"    defer channel.free(h)\n"+
		"    let mutex: Mutex = try Mutex(h)\n"+
		"    defer mutex.lock()\n"+
		"    defer mutex.unlock()\n"+
		"    defer mutex.free(h)\n"+
		"    let first: Task<Bool> = try spawn worker()\n"+
		"    defer first.join()\n"+
		"    let second: Task<Bool> = try spawn worker()\n"+
		"    defer second.detach()\n"+
		"    return 0\n"+
		"end\n")
	baseline := generateOne(t, program)["modules/app.c"]
	for _, want := range []string{
		"hex_list_free_Int32(",
		"hex_dict_free_Int32_Int32(",
		"hex_chan_free_Int32(",
		"hex_mutex_lock(",
		"hex_mutex_unlock(",
		"hex_mutex_free_hex_mutex(",
		"hex_task_join_Bool(",
		"hex_task_detach(",
	} {
		if !strings.Contains(baseline, want) {
			t.Fatalf("modules/app.c does not call the recorded deferred symbol %q:\n%s", want, baseline)
		}
	}

	original := builtinMethodRecord
	probes := map[specdata.TypePattern]map[string]string{
		specdata.ConstructorOwner(specdata.TypeList):    {"free": "hex_probe_list_free_%s"},
		specdata.ConstructorOwner(specdata.TypeDict):    {"free": "hex_probe_dict_free_%s"},
		specdata.ConstructorOwner(specdata.TypeChannel): {"free": "hex_probe_chan_free_%s"},
		specdata.ExactOwner(specdata.TypeMutex):         {"lock": "hex_probe_mutex_lock", "unlock": "hex_probe_mutex_unlock", "free": "hex_probe_mutex_free"},
		specdata.ConstructorOwner(specdata.TypeTask):    {"join": "hex_probe_task_join_%s", "detach": "hex_probe_task_detach"},
	}
	builtinMethodRecord = func(owner specdata.TypePattern, name string) (specdata.MethodSpec, bool) {
		if byName, ok := probes[owner]; ok {
			if symbol, ok := byName[name]; ok {
				return specdata.MethodSpec{Owner: owner, Name: name, RuntimeSymbol: symbol}, true
			}
		}
		return original(owner, name)
	}
	defer func() { builtinMethodRecord = original }()

	corrupted := generateOne(t, program)["modules/app.c"]
	for _, want := range []string{
		"hex_probe_list_free_Int32(",
		"hex_probe_dict_free_Int32_Int32(",
		"hex_probe_chan_free_Int32(",
		"hex_probe_mutex_lock(",
		"hex_probe_mutex_unlock(",
		"hex_probe_mutex_free(",
		"hex_probe_task_join_Bool(",
		"hex_probe_task_detach(",
	} {
		if !strings.Contains(corrupted, want) {
			t.Fatalf("modules/app.c ignored the corrupted deferred record for %q:\n%s", want, corrupted)
		}
	}
	for _, gone := range []string{
		"hex_list_free_Int32(",
		"hex_dict_free_Int32_Int32(",
		"hex_chan_free_Int32(",
		"hex_mutex_lock(",
		"hex_mutex_unlock(",
		"hex_mutex_free_hex_mutex(",
		"hex_task_join_Bool(",
		"hex_task_detach(",
	} {
		if strings.Contains(corrupted, gone) {
			t.Fatalf("modules/app.c kept the old deferred symbol %q after the record changed:\n%s", gone, corrupted)
		}
	}
}

// A hoisted Dict.find declaration names the recorded symbol too. Corrupting
// Dict.find must move the hoisted call site, which is rendered from the
// dict_find_decl template through the Go-side model.
func TestDictFindLoweringReadsTheRegistry(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n"+
		"    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n"+
		"    defer scores.free(h)\n"+
		"    let found: Int32 | Nil = scores.find(1)\n"+
		"    if found == nil then\n"+
		"        let spare: Int32 = 0\n"+
		"    end\n"+
		"end\n")
	baseline := generateOne(t, program)["modules/app.c"]
	if !strings.Contains(baseline, "hex_dict_find_Int32_Int32(") {
		t.Fatalf("modules/app.c does not call the recorded dict find symbol:\n%s", baseline)
	}

	original := builtinMethodRecord
	builtinMethodRecord = func(owner specdata.TypePattern, name string) (specdata.MethodSpec, bool) {
		if owner == specdata.ConstructorOwner(specdata.TypeDict) && name == "find" {
			return specdata.MethodSpec{Owner: owner, Name: name, RuntimeSymbol: "hex_probe_find_%s"}, true
		}
		return original(owner, name)
	}
	defer func() { builtinMethodRecord = original }()

	corrupted := generateOne(t, program)["modules/app.c"]
	if !strings.Contains(corrupted, "hex_probe_find_Int32_Int32(") {
		t.Fatalf("modules/app.c ignored the corrupted dict find record:\n%s", corrupted)
	}
	if strings.Contains(corrupted, "hex_dict_find_Int32_Int32(") {
		t.Fatalf("modules/app.c kept the old dict find symbol after the record changed:\n%s", corrupted)
	}
}

// migratedSymbolProgram reaches every runtime symbol migrated out of a
// handwritten helper-name composer: the Array and List re-slices, the Stash and
// Pool typed operations (direct and deferred), and the heap and inline text
// methods.
const migratedSymbolProgram = "fun demo(h: Heap) do\n" +
	"    let mut data: Array<Int32, 4> = [1, 2, 3, 4]\n" +
	"    let array_read: Slice<Int32> = data.slice(0, 2)\n" +
	"    let array_write: Slice<mut Int32> = data.mut_slice(0, 2)\n" +
	"    array_write[0] = 9\n" +
	"    let values: List<Int32> = List<Int32>(h)\n" +
	"    defer values.free(h)\n" +
	"    let list_read: Slice<Int32> = values.slice(0, 1)\n" +
	"    let list_write: Slice<mut Int32> = values.mut_slice(0, 1)\n" +
	"    list_write[0] = 2\n" +
	"    let stash = Stash<Int32>()\n" +
	"    let stash_node: Ptr<mut Int32> = stash.allocate(1)\n" +
	"    stash.reset()\n" +
	"    defer stash.allocate(2)\n" +
	"    defer stash.reset()\n" +
	"    defer stash.destroy()\n" +
	"    let pool = Pool<Int32>(4)\n" +
	"    let pool_node: Ptr<mut Int32> = pool.allocate(1)\n" +
	"    defer pool.allocate(2)\n" +
	"    defer pool.free(pool_node)\n" +
	"    defer pool.destroy()\n" +
	"    let text: String = \"hello\".copy(h)\n" +
	"    defer text.free(h)\n" +
	"    let heap_runes: Size = text.rune_length()\n" +
	"    let heap_graphemes: Size = text.grapheme_length()\n" +
	"    let heap_bytes_cursor: ByteCursor = text.byte_cursor()\n" +
	"    let heap_rune_cursor: RuneCursor = text.rune_cursor()\n" +
	"    let heap_grapheme_cursor: GraphemeCursor = text.grapheme_cursor()\n" +
	"    let heap_bytes: Slice<UInt8> = text.bytes()\n" +
	"    let heap_slice: Slice<UInt8> = text.slice(0, 1)\n" +
	"    let heap_folded: String | Error = text.casefold(h)\n" +
	"    let heap_normalized: String | Error = text.normalize(h, NormalizationForm.NFC())\n" +
	"    let heap_joined: String | Error = text.concat(h, heap_bytes)\n" +
	"    let label: String<16> = \"hexal\"\n" +
	"    let inline_runes: Size = label.rune_length()\n" +
	"    let inline_graphemes: Size = label.grapheme_length()\n" +
	"    let inline_bytes_cursor: ByteCursor = label.byte_cursor()\n" +
	"    let inline_rune_cursor: RuneCursor = label.rune_cursor()\n" +
	"    let inline_grapheme_cursor: GraphemeCursor = label.grapheme_cursor()\n" +
	"    let inline_bytes: Slice<UInt8> = label.bytes()\n" +
	"    let inline_slice: Slice<UInt8> = label.slice(0, 1)\n" +
	"    let inline_copied: String = label.copy(h)\n" +
	"    let inline_folded: String | Error = label.casefold(h)\n" +
	"    let inline_normalized: String | Error = label.normalize(h, NormalizationForm.NFC())\n" +
	"    let inline_joined: String | Error = label.concat(h, inline_bytes)\n" +
	"    let inline_wide: String<32> = label.widen<32>()\n" +
	"end\n"

// migratedSymbolProbes replaces each migrated record with a distinguishable
// symbol, so a site that still spells the operation by hand keeps the old name
// and fails the absence check below.
var migratedSymbolProbes = map[specdata.TypePattern]map[string]string{
	specdata.ConstructorOwner(specdata.TypeArray): {
		"slice":     "hex_probe_array_slice_%s",
		"mut_slice": "hex_probe_array_mut_slice_%s",
	},
	specdata.ConstructorOwner(specdata.TypeList): {
		"slice":     "hex_probe_list_slice_%s",
		"mut_slice": "hex_probe_list_mut_slice_%s",
	},
	specdata.ConstructorOwner(specdata.TypeStash): {
		"allocate": "hex_probe_stash_alloc_%s",
		"reset":    "hex_probe_stash_reset",
		"destroy":  "hex_probe_stash_destroy",
	},
	specdata.ConstructorOwner(specdata.TypePool): {
		"allocate": "hex_probe_pool_alloc_%s",
		"free":     "hex_probe_pool_free_%s",
		"destroy":  "hex_probe_pool_destroy_%s",
	},
	specdata.ExactOwner(specdata.TypeString): {
		"rune_length":     "hex_probe_heap_rune_length",
		"grapheme_length": "hex_probe_heap_grapheme_length",
		"byte_cursor":     "hex_probe_heap_byte_cursor",
		"rune_cursor":     "hex_probe_heap_rune_cursor",
		"grapheme_cursor": "hex_probe_heap_grapheme_cursor",
		"bytes":           "hex_probe_heap_bytes",
		"slice":           "hex_probe_heap_slice",
		"copy":            "hex_probe_heap_copy",
		"casefold":        "hex_probe_heap_casefold_%s",
		"normalize":       "hex_probe_heap_normalize_%s",
		"concat":          "hex_probe_heap_concat_%s",
		"free":            "hex_probe_heap_free",
	},
	specdata.ConstructorOwner(specdata.TypeInlineString): {
		"rune_length":     "hex_probe_inline_rune_length",
		"grapheme_length": "hex_probe_inline_grapheme_length",
		"byte_cursor":     "hex_probe_inline_byte_cursor",
		"rune_cursor":     "hex_probe_inline_rune_cursor",
		"grapheme_cursor": "hex_probe_inline_grapheme_cursor",
		"bytes":           "hex_probe_inline_bytes",
		"slice":           "hex_probe_inline_slice",
		"copy":            "hex_probe_inline_copy",
		"casefold":        "hex_probe_inline_casefold_%s",
		"normalize":       "hex_probe_inline_normalize_%s",
		"concat":          "hex_probe_inline_concat_%s",
		"widen":           "hex_probe_inline_widen",
	},
}

// Every migrated helper site must read its record. The baseline proves each
// recorded symbol is emitted today; corrupting every record must move each
// emitted call and leave no old symbol behind, which a site that still
// composed its name by hand could not do.
func TestMigratedRuntimeSymbolsReadTheRegistry(t *testing.T) {
	program := checkedGeneratorSource(t, migratedSymbolProgram)
	baseline := generateOne(t, program)["modules/app.c"]
	for _, want := range []string{
		"hex_array_slice_Int32_4(",
		"hex_array_mut_slice_Int32_4(",
		"hex_list_slice_Int32(",
		"hex_list_mut_slice_Int32(",
		"hex_stash_alloc_int32_t(",
		"hex_stash_reset(",
		"hex_stash_destroy(",
		"hex_pool_alloc_Int32(",
		"hex_pool_free_Int32(",
		"hex_pool_destroy_Int32(",
		"hex_text_rune_length(",
		"hex_text_grapheme_length(",
		"hex_text_byte_cursor(",
		"hex_text_rune_cursor(",
		"hex_text_grapheme_cursor(",
		"hex_text_bytes(",
		"hex_text_slice(",
		"hex_string_make(",
		"hex_string_free(",
		"hex_string_casefold_",
		"hex_string_normalize_",
		"hex_string_concat_",
		"hex_text_fill(",
	} {
		if !strings.Contains(baseline, want) {
			t.Fatalf("modules/app.c does not call the recorded symbol %q:\n%s", want, baseline)
		}
	}

	original := builtinMethodRecord
	builtinMethodRecord = func(owner specdata.TypePattern, name string) (specdata.MethodSpec, bool) {
		if byName, ok := migratedSymbolProbes[owner]; ok {
			if symbol, ok := byName[name]; ok {
				return specdata.MethodSpec{Owner: owner, Name: name, RuntimeSymbol: symbol}, true
			}
		}
		return original(owner, name)
	}
	defer func() { builtinMethodRecord = original }()

	corrupted := generateOne(t, program)["modules/app.c"]
	for _, want := range []string{
		"hex_probe_array_slice_Int32_4(",
		"hex_probe_array_mut_slice_Int32_4(",
		"hex_probe_list_slice_Int32(",
		"hex_probe_list_mut_slice_Int32(",
		"hex_probe_stash_alloc_int32_t(",
		"hex_probe_stash_reset(",
		"hex_probe_stash_destroy(",
		"hex_probe_pool_alloc_Int32(",
		"hex_probe_pool_free_Int32(",
		"hex_probe_pool_destroy_Int32(",
		"hex_probe_heap_rune_length(",
		"hex_probe_inline_rune_length(",
		"hex_probe_heap_grapheme_length(",
		"hex_probe_inline_grapheme_length(",
		"hex_probe_heap_byte_cursor(",
		"hex_probe_inline_byte_cursor(",
		"hex_probe_heap_rune_cursor(",
		"hex_probe_inline_rune_cursor(",
		"hex_probe_heap_grapheme_cursor(",
		"hex_probe_inline_grapheme_cursor(",
		"hex_probe_heap_bytes(",
		"hex_probe_inline_bytes(",
		"hex_probe_heap_slice(",
		"hex_probe_inline_slice(",
		"hex_probe_heap_copy(",
		"hex_probe_inline_copy(",
		"hex_probe_heap_free(",
		"hex_probe_heap_casefold_",
		"hex_probe_inline_casefold_",
		"hex_probe_heap_normalize_",
		"hex_probe_inline_normalize_",
		"hex_probe_heap_concat_",
		"hex_probe_inline_concat_",
		"hex_probe_inline_widen(",
	} {
		if !strings.Contains(corrupted, want) {
			t.Fatalf("modules/app.c ignored the corrupted record for %q:\n%s", want, corrupted)
		}
	}
	for _, gone := range []string{
		"hex_array_slice_",
		"hex_array_mut_slice_",
		"hex_list_slice_",
		"hex_list_mut_slice_",
		"hex_stash_alloc_",
		"hex_stash_reset(",
		"hex_stash_destroy(",
		"hex_pool_alloc_",
		"hex_pool_free_",
		"hex_pool_destroy_",
		"hex_text_rune_length(",
		"hex_text_grapheme_length(",
		"hex_text_byte_cursor(",
		"hex_text_rune_cursor(",
		"hex_text_grapheme_cursor(",
		"hex_text_bytes(",
		"hex_text_slice(",
		"hex_string_make(",
		"hex_string_free(",
		"hex_string_casefold_",
		"hex_string_normalize_",
		"hex_string_concat_",
		"hex_text_fill(",
	} {
		if strings.Contains(corrupted, gone) {
			t.Fatalf("modules/app.c kept the old symbol %q after the record changed:\n%s", gone, corrupted)
		}
	}
}

// The shared fill helper's name is recorded once, under the widen method, yet
// Error's text coercion and the inline from_bytes adapter emit the same helper.
// Corrupting the widen record must move those non-method sites too, so the
// helper has exactly one owner.
func TestSharedTextFillSitesReadTheWidenRecord(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n"+
		"    let heap: String = \"x\".copy(h)\n"+
		"    defer heap.free(h)\n"+
		"    let small: String<16> = \"x\"\n"+
		"    let a: Error = Error(ErrorKind.Other(header = small), small)\n"+
		"    let b: Error = Error(ErrorKind.Other(header = heap), heap)\n"+
		"    let header: String<128> = a.header()\n"+
		"    let message: String<256> = b.message\n"+
		"    let raw: Slice<UInt8> = heap.bytes()\n"+
		"    let copied: String<16> | Error = String<16>.from_bytes(raw)\n"+
		"end\n")
	baseline := generateOne(t, program)
	baseText := baseline["modules/app.c"] + baseline["modules/app.h"]
	for _, want := range []string{"hex_text_fill(", "hex_text_fill_checked("} {
		if !strings.Contains(baseText, want) {
			t.Fatalf("generated module does not emit the shared fill symbol %q:\n%s", want, baseText)
		}
	}

	original := builtinMethodRecord
	builtinMethodRecord = func(owner specdata.TypePattern, name string) (specdata.MethodSpec, bool) {
		if owner == specdata.ConstructorOwner(specdata.TypeInlineString) && name == "widen" {
			return specdata.MethodSpec{Owner: owner, Name: name, RuntimeSymbol: "hex_probe_widen"}, true
		}
		return original(owner, name)
	}
	defer func() { builtinMethodRecord = original }()

	corrupted := generateOne(t, program)
	corruptedText := corrupted["modules/app.c"] + corrupted["modules/app.h"]
	if !strings.Contains(corruptedText, "hex_probe_widen(") {
		t.Fatalf("generated module ignored the corrupted widen record:\n%s", corruptedText)
	}
	if strings.Contains(corruptedText, "hex_text_fill(") {
		t.Fatalf("generated module kept the literal hex_text_fill after the record changed:\n%s", corruptedText)
	}
	if !strings.Contains(corruptedText, "hex_text_fill_checked(") {
		t.Fatalf("generated module lost the unrecorded hex_text_fill_checked helper:\n%s", corruptedText)
	}
}
