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
