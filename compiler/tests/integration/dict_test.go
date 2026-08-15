package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0020 Phase E: Dict<K, V> with Int32 and Strand keys, owning
// dictionaries, and String-value shallow-copy and explicit-cleanup rules.

func TestDictInt32Lifecycle(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    present: Bool = scores.contains(1)\n    first: Int32 = scores.get(1)\n    removed: Int32 = scores.remove(2)\nend")
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
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
	// RFC 0069: capacity doubling and bucket-region byte sizing stay in
	// size_t with checked multiply, and the load-factor growth decision
	// checks every operand before comparison; the manual SIZE_MAX guard and
	// the uint64_t temporary are gone.
	// RFC 0069 Amendment 2: a fresh inactive bucket region zeroes with one
	// memset, every diagnostic reports through hex_runtime_trap, and the
	// Dict helpers carry no raw fputs or compiler-owned NULL.
	header := hexalH(t, result)
	for _, want := range []string{
		"size_t next = 8;",
		"ckd_mul(&next, dict->capacity, 2)",
		"ckd_mul(&bytes, next, sizeof(hex_dict_entry_Int32_Int32))",
		"memset(region, 0, bytes);",
		"ckd_add(&length_plus_one, dict->length, 1)",
		"ckd_mul(&load_times_10, length_plus_one, 10)",
		"ckd_mul(&capacity_times_7, dict->capacity, 7)",
		"hex_runtime_trap(\"[Runtime Error] dictionary key not found\\n\")",
		"hex_runtime_trap(\"[Runtime Error] dictionary capacity is not representable\\n\")",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal.h does not contain %q:\n%s", want, header)
		}
	}
	for _, forbid := range []string{"uint64_t next", "SIZE_MAX /", "(dict->length + 1) * 10 >= dict->capacity * 7", "fputs(", "NULL", "region[index].active = false"} {
		if strings.Contains(header, forbid) {
			t.Fatalf("hexal.h retains %q:\n%s", forbid, header)
		}
	}
}

func TestDictStrandKeys(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    labels.insert(\"bob\", 2)\n    present: Bool = labels.contains(\"alice\")\n    score: Int32 = labels.get(\"bob\")\n    key: Strand = \"carol\"\n    labels.insert(key, 3)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_strand {",
		"uint8_t data[32];",
		"hex_dict_insert_Strand_Int32(hex_v_labels, (hex_strand){{ 97, 108, 105, 99, 101, 0 }}, 1);",
		"hex_dict_contains_Strand_Int32(hex_v_labels, (hex_strand){{ 97, 108, 105, 99, 101, 0 }})",
		"hex_v_score = hex_dict_get_Strand_Int32(hex_v_labels, (hex_strand){{ 98, 111, 98, 0 }});",
		"hex_dict_insert_Strand_Int32(hex_v_labels, hex_v_key, 3);",
		"hex_hash_Strand",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
	// RFC 0069 Amendment 2: Strand Dict probing compares the canonical
	// zero-filled 32-byte key representation with one direct memcmp and
	// emits no per-Dict key-equality wrapper; diagnostics report through
	// hex_runtime_trap and no compiler-owned NULL or raw fputs remains.
	header := hexalH(t, result)
	for _, want := range []string{
		"memcmp(region[index].key.data, key.data, 32) != 0",
		"memcmp(dict->buckets[index].key.data, key.data, 32) != 0",
		"hex_runtime_trap(\"[Runtime Error] dictionary key not found\\n\")",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal.h does not contain %q:\n%s", want, header)
		}
	}
	for _, forbid := range []string{"hex_dict_key_equal_", "fputs(", "NULL"} {
		if strings.Contains(header, forbid) {
			t.Fatalf("hexal.h retains %q:\n%s", forbid, header)
		}
	}
}

func TestDictStringValues(t *testing.T) {
	// RFC 0048: a stored literal is never freed by the collection or by a
	// remove; a runtime String removed from the dict is freed explicitly.
	result := compileSource("fun demo(h: Heap) do\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"alice\")\n    runtime: String = \"bob\".to_string(h)\n    people.insert(2, runtime)\n    removed: String = people.remove(2)\n    removed.free(h)\n    people.insert(1, \"carol\")\n    name: String = people.get(1)\nend")
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

// RFC 0035: a Dict<String> read returns a shallow String handle; mutation
// while a read handle is live is now the programmer's responsibility.
func TestDictMutationAfterReadIsValid(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    people.insert(2, \"b\")\nend",
		"fun demo(h: Heap) do\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    removed: String = people.remove(1)\nend",
		"fun demo(h: Heap) do\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    people.free(h)\nend",
		"fun demo(h: Heap) do\n    mut people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    people = Dict<Int32, String>.new(h)\nend",
		"fun inspect(people: Dict<Int32, String>) do\nend\nfun demo(h: Heap) do\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    inspect(people)\nend",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestDictBorrowAllowsLookups(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    present: Bool = people.contains(1)\n    other: String = people.get(1)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestDictShallowCopySemantics(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\nend",
		"fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    scores.free(h)\n    scores.free(h)\nend",
		"fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    other: Dict<Int32, Int32> = scores\nend",
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
		{"fun demo(h: Heap) do\n    scores: Dict<Bool, Int32> = Dict<Bool, Int32>.new(h)\nend", "dictionary key type must be Int32 or Strand"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestDictReturnHandoff(t *testing.T) {
	result := compileSource("fun make_scores(h: Heap): Dict<Int32, Int32> do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    scores.insert(1, 10)\n    return scores\nend\nfun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = make_scores(h)\n    scores.free(h)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}
