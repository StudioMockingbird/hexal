package compiler

import (
	"strings"
	"testing"
)

// RFC 0020 Phase E: Dict<K, V> with Int32 and Strand keys, owning
// dictionaries, and String-value shallow-copy and explicit-cleanup rules.

func TestDictInt32Lifecycle(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    scores.insert(2, 20)\n    present: Bool = scores.contains(1)\n    first: Int32 = scores.get(1)\n    removed: Int32 = scores.remove(2)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
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
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestDictStrandKeys(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"alice\", 1)\n    labels.insert(\"bob\", 2)\n    present: Bool = labels.contains(\"alice\")\n    score: Int32 = labels.get(\"bob\")\n    key: Strand = \"carol\"\n    labels.insert(key, 3)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
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
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestDictStringValues(t *testing.T) {
	// RFC 0048: a stored literal is never freed by the collection or by a
	// remove; a runtime String removed from the dict is freed explicitly.
	result := Compile("fun demo(h: Heap)\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"alice\")\n    runtime: String = \"bob\".to_string(h)\n    people.insert(2, runtime)\n    removed: String = people.remove(2)\n    removed.free(h)\n    people.insert(1, \"carol\")\n    name: String = people.get(1)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"hex_dict_insert_Int32_String(hex_v_people, 1, &hex_lit_0);",
		"hex_dict_insert_Int32_String(hex_v_people, 2, hex_v_runtime);",
		"hex_v_name = hex_dict_get_Int32_String(hex_v_people, 1);",
		"hex_v_removed = hex_dict_remove_Int32_String(hex_v_people, 2);",
		"hex_string_free(hex_v_h, hex_v_removed);",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

// RFC 0035: a Dict<String> read returns a shallow String handle; mutation
// while a read handle is live is now the programmer's responsibility.
func TestDictMutationAfterReadIsValid(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap)\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    people.insert(2, \"b\")\nend",
		"fun demo(h: Heap)\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    removed: String = people.remove(1)\nend",
		"fun demo(h: Heap)\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    people.free(h)\nend",
		"fun demo(h: Heap)\n    mut people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    people = Dict<Int32, String>.new(h)\nend",
		"fun inspect(people: Dict<Int32, String>)\nend\nfun demo(h: Heap)\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    inspect(people)\nend",
	} {
		if result := Compile(source); result.ExitCode != ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
}

func TestDictBorrowAllowsLookups(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    people: Dict<Int32, String> = Dict<Int32, String>.new(h)\n    defer people.free(h)\n    people.insert(1, \"a\")\n    name: String = people.get(1)\n    present: Bool = people.contains(1)\n    other: String = people.get(1)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}

func TestDictShallowCopySemantics(t *testing.T) {
	for _, source := range []string{
		"fun demo(h: Heap)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\nend",
		"fun demo(h: Heap)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    scores.free(h)\n    scores.free(h)\nend",
		"fun demo(h: Heap)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    other: Dict<Int32, Int32> = scores\nend",
		"fun demo(h: Heap, scores: Dict<Int32, Int32>)\n    scores.free(h)\nend",
	} {
		if result := Compile(source); result.ExitCode != ExitSuccess {
			t.Fatalf("Compile(%q) exit code = %d (%v), want 0", source, result.ExitCode, result.Stderr)
		}
	}
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo(h: Heap)\n    scores: Dict<Bool, Int32> = Dict<Bool, Int32>.new(h)\nend", "dictionary key type must be Int32 or Strand"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestDictReturnHandoff(t *testing.T) {
	result := Compile("fun make_scores(h: Heap): Dict<Int32, Int32>\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    scores.insert(1, 10)\n    return scores\nend\nfun demo(h: Heap)\n    scores: Dict<Int32, Int32> = make_scores(h)\n    scores.free(h)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}
