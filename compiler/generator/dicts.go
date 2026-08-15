package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedDictState records the dictionary types that need header and
// helper definitions, in deterministic order.
type generatedDictState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.DictInfo]bool
}

// discoverGeneratedDicts walks every type reachable from the program and
// collects the distinct dictionary types. Discovery order is then sorted by
// C name so the generated header is deterministic.
func discoverGeneratedDicts(program checker.Program) (*generatedDictState, error) {
	state := &generatedDictState{seen: make(map[*compilerTypes.DictInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.Dict != nil {
				if !state.seen[typ.Dict] {
					state.seen[typ.Dict] = true
					state.order = append(state.order, typ)
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}

	sort.SliceStable(state.order, func(left, right int) bool {
		return state.order[left].CName < state.order[right].CName
	})
	return state, nil
}
func dictSuffix(dict compilerTypes.Type) string {
	return strings.TrimPrefix(dict.CName, "hex_dict_")
}

// writeDictDefinitions emits the hex_strand value type once, plus one entry
// struct and the hash/probe/insert/get/contains/remove/free helpers per
// dictionary type. Dictionary String values are copied in, borrowed out, and
// moved out on remove exactly like List<String> elements.
func writeDictDefinitions(result *strings.Builder, dicts *generatedDictState) {
	if dicts == nil {
		return
	}
	hashInt32Written := false
	hashStrandWritten := false
	for _, dict := range dicts.order {
		key := dict.Dict.Key
		value := dict.Dict.Value
		keySpelling := typeSpelling(key)
		valueSpelling := typeSpelling(value)
		suffix := dictSuffix(dict)
		entryName := "hex_dict_entry_" + suffix
		fmt.Fprintf(result, "\ntypedef struct %s {\n    bool active;\n    %s key;\n    %s value;\n} %s;\n", entryName, keySpelling, valueSpelling, entryName)
		fmt.Fprintf(result, "typedef struct %s {\n    %s *buckets;\n    size_t length;\n    size_t capacity;\n    uintptr_t allocator;\n} %s;\n", dict.CName, entryName, dict.CName)
		if compilerTypes.IsStrand(key) {
			hashStrandWritten = writeStrandHashHelper(result, hashStrandWritten)
		} else {
			hashInt32Written = writeInt32HashHelper(result, hashInt32Written)
		}
		fmt.Fprintf(result, "static inline uint64_t hex_dict_probe_%s_region(%s *region, uint64_t capacity, %s key) {\n", suffix, entryName, keySpelling)
		if compilerTypes.IsStrand(key) {
			result.WriteString("    uint64_t hash = hex_hash_Strand(key);\n")
		} else {
			result.WriteString("    uint64_t hash = hex_hash_Int32(key);\n")
		}
		result.WriteString("    size_t index = hash & (capacity - 1);\n")
		if compilerTypes.IsStrand(key) {
			// RFC 0069 Amendment 2: the canonical zero-filled 32-byte key
			// representation compares with one direct memcmp; no per-Dict
			// equality wrapper is emitted.
			result.WriteString("    while (region[index].active && memcmp(region[index].key.data, key.data, 32) != 0) {\n        index = (index + 1) & (capacity - 1);\n    }\n")
		} else {
			result.WriteString("    while (region[index].active && region[index].key != key) {\n        index = (index + 1) & (capacity - 1);\n    }\n")
		}
		result.WriteString("    return index;\n}\n")
		fmt.Fprintf(result, "static inline uint64_t hex_dict_probe_%s(const %s *dict, %s key) {\n", suffix, dict.CName, keySpelling)
		if compilerTypes.IsStrand(key) {
			result.WriteString("    uint64_t hash = hex_hash_Strand(key);\n")
		} else {
			result.WriteString("    uint64_t hash = hex_hash_Int32(key);\n")
		}
		result.WriteString("    size_t index = hash & (dict->capacity - 1);\n")
		if compilerTypes.IsStrand(key) {
			result.WriteString("    while (dict->buckets[index].active && memcmp(dict->buckets[index].key.data, key.data, 32) != 0) {\n        index = (index + 1) & (dict->capacity - 1);\n    }\n")
		} else {
			result.WriteString("    while (dict->buckets[index].active && dict->buckets[index].key != key) {\n        index = (index + 1) & (dict->capacity - 1);\n    }\n")
		}
		result.WriteString("    return index;\n}\n")
		fmt.Fprintf(result, "static inline %s *hex_dict_new_%s(hex_heap h) {\n", dict.CName, suffix)
		result.WriteString("    " + dict.CName + " *header = hex_heap_raw_allocate(h.identity, sizeof(" + dict.CName + "), _Alignof(" + dict.CName + "));\n")
		result.WriteString("    header->buckets = nullptr;\n    header->length = 0;\n    header->capacity = 0;\n    header->allocator = h.identity;\n")
		fmt.Fprintf(result, "    return header;\n}\n")
		fmt.Fprintf(result, "static inline void hex_dict_grow_%s(%s *dict) {\n", suffix, dict.CName)
		// RFC 0069: doubling stays in size_t; the multiply is checked so an
		// unrepresentable capacity traps instead of wrapping.
		result.WriteString("    size_t next = 8;\n")
		result.WriteString("    if (dict->capacity != 0) {\n")
		result.WriteString("        if (ckd_mul(&next, dict->capacity, 2)) {\n")
		result.WriteString("            hex_runtime_trap(\"[Runtime Error] dictionary capacity is not representable\\n\");\n")
		result.WriteString("        }\n")
		result.WriteString("    }\n")
		result.WriteString("    size_t bytes;\n")
		result.WriteString("    if (ckd_mul(&bytes, next, sizeof(" + entryName + "))) {\n")
		result.WriteString("        hex_runtime_trap(\"[Runtime Error] dictionary capacity is not representable\\n\");\n")
		result.WriteString("    }\n")
		result.WriteString("    " + entryName + " *region = hex_heap_raw_allocate(dict->allocator, bytes, _Alignof(" + entryName + "));\n")
		// RFC 0069 Amendment 2: a fresh inactive bucket region zeroes with
		// one memset after the checked byte count and successful allocation.
		// Inactive keys and values are never read, so zeroing adds no value
		// semantics beyond a valid active flag.
		result.WriteString("    memset(region, 0, bytes);\n")
		fmt.Fprintf(result, "    for (size_t index = 0; index < dict->capacity; index++) {\n        if (dict->buckets[index].active) {\n            uint64_t probe = hex_dict_probe_%s_region(region, next, dict->buckets[index].key);\n            region[probe] = dict->buckets[index];\n        }\n    }\n", suffix)
		result.WriteString("    if (dict->buckets != nullptr) {\n        hex_heap_free(dict->buckets, dict->allocator);\n    }\n    dict->buckets = region;\n    dict->capacity = next;\n}\n")
		fmt.Fprintf(result, "static inline void hex_dict_insert_%s(%s *dict, %s key, %s value) {\n", suffix, dict.CName, keySpelling, valueSpelling)
		// RFC 0069: the load-factor decision checks every arithmetic operand
		// before comparison; an unrepresentable decision traps as
		// unrepresentable capacity and never wraps to skip required growth.
		result.WriteString("    if (dict->capacity == 0) {\n        hex_dict_grow_" + suffix + "(dict);\n    } else {\n")
		result.WriteString("        size_t length_plus_one;\n        size_t load_times_10;\n        size_t capacity_times_7;\n")
		result.WriteString("        if (ckd_add(&length_plus_one, dict->length, 1) || ckd_mul(&load_times_10, length_plus_one, 10) || ckd_mul(&capacity_times_7, dict->capacity, 7)) {\n")
		result.WriteString("            hex_runtime_trap(\"[Runtime Error] dictionary capacity is not representable\\n\");\n")
		result.WriteString("    }\n")
		result.WriteString("        if (load_times_10 >= capacity_times_7) {\n            hex_dict_grow_" + suffix + "(dict);\n        }\n    }\n")
		result.WriteString("    size_t index = hex_dict_probe_" + suffix + "(dict, key);\n")
		fmt.Fprintf(result, "    if (dict->buckets[index].active) {\n")
		result.WriteString("        dict->buckets[index].value = value;\n")
		result.WriteString("        return;\n    }\n")
		fmt.Fprintf(result, "    dict->buckets[index].active = true;\n    dict->buckets[index].key = key;\n")
		result.WriteString("    dict->buckets[index].value = value;\n")
		result.WriteString("    dict->length++;\n}\n")
		fmt.Fprintf(result, "static inline %s hex_dict_get_%s(const %s *dict, %s key) {\n", valueSpelling, suffix, dict.CName, keySpelling)
		fmt.Fprintf(result, "    if (dict->capacity == 0) {\n        hex_runtime_trap(\"[Runtime Error] dictionary key not found\\n\");\n    }\n")
		result.WriteString("    size_t index = hex_dict_probe_" + suffix + "(dict, key);\n")
		fmt.Fprintf(result, "    if (!dict->buckets[index].active) {\n        hex_runtime_trap(\"[Runtime Error] dictionary key not found\\n\");\n    }\n")
		result.WriteString("    return dict->buckets[index].value;\n}\n")
		fmt.Fprintf(result, "static inline bool hex_dict_contains_%s(const %s *dict, %s key) {\n", suffix, dict.CName, keySpelling)
		fmt.Fprintf(result, "    if (dict->capacity == 0) {\n        return false;\n    }\n")
		result.WriteString("    size_t index = hex_dict_probe_" + suffix + "(dict, key);\n")
		result.WriteString("    return dict->buckets[index].active;\n}\n")
		fmt.Fprintf(result, "static inline %s hex_dict_remove_%s(%s *dict, %s key) {\n", valueSpelling, suffix, dict.CName, keySpelling)
		fmt.Fprintf(result, "    if (dict->capacity == 0) {\n        hex_runtime_trap(\"[Runtime Error] dictionary key not found\\n\");\n    }\n")
		result.WriteString("    size_t index = hex_dict_probe_" + suffix + "(dict, key);\n")
		fmt.Fprintf(result, "    if (!dict->buckets[index].active) {\n        hex_runtime_trap(\"[Runtime Error] dictionary key not found\\n\");\n    }\n")
		result.WriteString("    " + valueSpelling + " value = dict->buckets[index].value;\n")
		result.WriteString("    dict->buckets[index].active = false;\n    dict->length--;\n    return value;\n}\n")
		fmt.Fprintf(result, "static inline void hex_dict_free_%s(hex_heap h, %s *dict) {\n", suffix, dict.CName)
		result.WriteString("    if (dict == nullptr || dict->allocator != h.identity) {\n")
		result.WriteString("        hex_runtime_trap(\"[Runtime Error] deallocation used the wrong allocator\\n\");\n    }\n")
		// Both regions came from hex_heap_raw_allocate; only hex_heap_free can
		// release them (RFC 0048 conformance: freeing the interior pointer
		// directly is heap corruption).
		result.WriteString("    if (dict->buckets != nullptr) {\n        hex_heap_free(dict->buckets, dict->allocator);\n    }\n")
		result.WriteString("    hex_heap_free(dict, h.identity);\n}\n")
	}
}

// writeInt32HashHelper emits the Int32 hash once per header.
func writeInt32HashHelper(result *strings.Builder, written bool) bool {
	if written {
		return true
	}
	result.WriteString("\nstatic inline uint64_t hex_hash_Int32(int32_t key) {\n")
	result.WriteString("    uint64_t x = (size_t)(uint32_t)key + 0x9E3779B97F4A7C15ULL;\n")
	result.WriteString("    x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9ULL;\n")
	result.WriteString("    x = (x ^ (x >> 27)) * 0x94D049BB133111EBULL;\n")
	result.WriteString("    return x ^ (x >> 31);\n}\n")
	return true
}

// writeStrandHashHelper emits the Strand FNV-1a hash once per header. The
// full 32 zero-padded bytes are hashed; the NUL-free payload guarantees the
// padding does not collide distinct values.
func writeStrandHashHelper(result *strings.Builder, written bool) bool {
	if written {
		return true
	}
	result.WriteString("\nstatic inline uint64_t hex_hash_Strand(hex_strand key) {\n")
	result.WriteString("    uint64_t hash = 14695981039346656037ULL;\n")
	result.WriteString("    for (size_t index = 0; index < 32; index++) {\n")
	result.WriteString("        hash ^= key.data[index];\n        hash *= 1099511628211ULL;\n")
	result.WriteString("    }\n")
	result.WriteString("    return hash;\n}\n")
	return true
}
