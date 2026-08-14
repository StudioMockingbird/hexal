package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedStringState records the unique String literals in first-use order
// plus whether the String machinery is needed at all.
type generatedStringState struct {
	used       bool
	needStrand bool
	literals   []string
	seen       map[string]int // payload -> literal index + 1
}

// discoverGeneratedStrings walks the program collecting unique String
// literal payloads. The machinery is marked used whenever a String-typed
// value or literal appears.
func discoverGeneratedStrings(program checker.Program) (*generatedStringState, error) {
	state := &generatedStringState{seen: make(map[string]int)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsString(typ) {
				state.used = true
				return nil
			}
			if compilerTypes.IsStrand(typ) {
				state.used = true
				state.needStrand = true
				return nil
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.StringLiteralExpression {
				state.used = true
				if _, exists := state.seen[node.Name]; !exists {
					state.seen[node.Name] = len(state.literals) + 1
					state.literals = append(state.literals, node.Name)
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}

// stringLiteralCName returns the static object base name for one literal.
func stringLiteralCName(index int) string {
	return fmt.Sprintf("hex_lit_%d", index)
}

// writeStringDefinitions emits the hex_string handle and storage typedefs, one
// read-only static object pair per unique literal, and the runtime helpers.
// The byte-view helpers require hex_view_UInt8, which GenerateChecked ensures
// is present whenever the string machinery is used.
func writeStringDefinitions(result *strings.Builder, stringState *generatedStringState) {
	if stringState == nil || !stringState.used {
		return
	}
	result.WriteString("\ntypedef struct hex_string {\n    const uint8_t *data;\n    size_t byte_length;\n} hex_string;\n")
	result.WriteString("typedef struct hex_string_storage {\n    hex_string header;\n    uint8_t bytes[];\n} hex_string_storage;\n")
	if stringState.needStrand {
		// RFC 0044: a Strand is exactly 32 inline bytes; the first zero byte
		// bounds the logical payload and the tail is zero-filled.
		result.WriteString("typedef struct hex_strand {\n    uint8_t data[32];\n} hex_strand;\n")
	}
	for index, payload := range stringState.literals {
		name := stringLiteralCName(index)
		fmt.Fprintf(result, "static const uint8_t %s_bytes[%d] = {", name, len(payload)+1)
		for _, character := range []byte(payload) {
			fmt.Fprintf(result, " %d,", character)
		}
		result.WriteString(" 0 };\n")
		fmt.Fprintf(result, "static const hex_string %s = { %s_bytes, %d };\n", name, name, len(payload))
	}
	result.WriteString("\nstatic inline uint64_t hex_utf8_next(const uint8_t *data, size_t length, size_t *index) {\n")
	result.WriteString("    uint8_t lead = data[*index];\n")
	result.WriteString("    uint64_t width;\n")
	result.WriteString("    if (lead < 0x80) {\n")
	result.WriteString("        width = 1;\n")
	result.WriteString("    } else if (lead < 0xE0) {\n")
	result.WriteString("        width = 2;\n")
	result.WriteString("    } else if (lead < 0xF0) {\n")
	result.WriteString("        width = 3;\n")
	result.WriteString("    } else if (lead < 0xF8) {\n")
	result.WriteString("        width = 4;\n")
	result.WriteString("    } else {\n")
	result.WriteString("        fputs(\"[Runtime Error] invalid UTF-8 in String\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    if (*index + width > length) {\n")
	result.WriteString("        fputs(\"[Runtime Error] invalid UTF-8 in String\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    for (uint64_t continuation = 1; continuation < width; continuation++) {\n")
	result.WriteString("        if ((data[*index + continuation] & 0xC0) != 0x80) {\n")
	result.WriteString("            fputs(\"[Runtime Error] invalid UTF-8 in String\\n\", stderr);\n            abort();\n        }\n")
	result.WriteString("    }\n")
	result.WriteString("    *index += width;\n")
	result.WriteString("    return width;\n}\n")
	result.WriteString("\nstatic inline uint32_t hex_utf8_decode(const uint8_t *data, size_t length, size_t *index) {\n")
	result.WriteString("    size_t start = *index;\n")
	result.WriteString("    hex_utf8_next(data, length, index);\n")
	result.WriteString("    uint8_t lead = data[start];\n")
	result.WriteString("    if (lead < 0x80) {\n")
	result.WriteString("        return lead;\n")
	result.WriteString("    }\n")
	result.WriteString("    if (lead < 0xE0) {\n")
	result.WriteString("        return ((uint32_t)(lead & 0x1F) << 6) | (uint32_t)(data[start + 1] & 0x3F);\n")
	result.WriteString("    }\n")
	result.WriteString("    if (lead < 0xF0) {\n")
	result.WriteString("        return ((uint32_t)(lead & 0x0F) << 12) | ((uint32_t)(data[start + 1] & 0x3F) << 6) | (uint32_t)(data[start + 2] & 0x3F);\n")
	result.WriteString("    }\n")
	result.WriteString("    return ((uint32_t)(lead & 0x07) << 18) | ((uint32_t)(data[start + 1] & 0x3F) << 12) | ((uint32_t)(data[start + 2] & 0x3F) << 6) | (uint32_t)(data[start + 3] & 0x3F);\n")
	result.WriteString("}\n")
	result.WriteString("\nstatic inline size_t hex_utf8_encode(uint8_t *out, uint32_t value) {\n")
	result.WriteString("    if (value < 0x80) {\n")
	result.WriteString("        out[0] = (uint8_t)value;\n        return 1;\n")
	result.WriteString("    }\n")
	result.WriteString("    if (value < 0x800) {\n")
	result.WriteString("        out[0] = (uint8_t)(0xC0 | (value >> 6));\n        out[1] = (uint8_t)(0x80 | (value & 0x3F));\n        return 2;\n")
	result.WriteString("    }\n")
	result.WriteString("    if (value < 0x10000) {\n")
	result.WriteString("        out[0] = (uint8_t)(0xE0 | (value >> 12));\n        out[1] = (uint8_t)(0x80 | ((value >> 6) & 0x3F));\n        out[2] = (uint8_t)(0x80 | (value & 0x3F));\n        return 3;\n")
	result.WriteString("    }\n")
	result.WriteString("    out[0] = (uint8_t)(0xF0 | (value >> 18));\n")
	result.WriteString("    out[1] = (uint8_t)(0x80 | ((value >> 12) & 0x3F));\n")
	result.WriteString("    out[2] = (uint8_t)(0x80 | ((value >> 6) & 0x3F));\n")
	result.WriteString("    out[3] = (uint8_t)(0x80 | (value & 0x3F));\n")
	result.WriteString("    return 4;\n}\n")
	result.WriteString("\nstatic inline const hex_string *hex_string_from_bytes(hex_heap h, const uint8_t *data, size_t length) {\n")
	result.WriteString("    // RFC 0044: the complete sequence validates before any allocation.\n")
	result.WriteString("    size_t index = 0;\n")
	result.WriteString("    while (index < length) {\n")
	result.WriteString("        hex_utf8_next(data, length, &index);\n")
	result.WriteString("    }\n")
	result.WriteString("    hex_string_storage *storage = hex_heap_raw_allocate(h.identity, sizeof(hex_string_storage) + length + 1, _Alignof(hex_string_storage));\n")
	result.WriteString("    storage->header.data = storage->bytes;\n")
	result.WriteString("    storage->header.byte_length = length;\n")
	result.WriteString("    for (size_t index = 0; index < length; index++) {\n")
	result.WriteString("        storage->bytes[index] = data[index];\n")
	result.WriteString("    }\n")
	result.WriteString("    storage->bytes[length] = 0;\n")
	result.WriteString("    return &storage->header;\n}\n")
	result.WriteString("\nstatic inline const hex_string *hex_string_from_runes(hex_heap h, const uint32_t *data, size_t length) {\n")
	result.WriteString("    // RFC 0044: every scalar validates, the byte count is computed with\n")
	result.WriteString("    // checked Size arithmetic, and one allocation encodes directly.\n")
	result.WriteString("    size_t bytes = 0;\n")
	result.WriteString("    for (size_t index = 0; index < length; index++) {\n")
	result.WriteString("        uint32_t value = data[index];\n")
	result.WriteString("        if (value > 0x10FFFF || (value >= 0xD800 && value <= 0xDFFF)) {\n")
	result.WriteString("            fputs(\"[Runtime Error] invalid Unicode scalar value\\n\", stderr);\n            abort();\n")
	result.WriteString("        }\n")
	result.WriteString("        if (value < 0x80) {\n")
	result.WriteString("            bytes += 1;\n")
	result.WriteString("        } else if (value < 0x800) {\n")
	result.WriteString("            bytes += 2;\n")
	result.WriteString("        } else if (value < 0x10000) {\n")
	result.WriteString("            bytes += 3;\n")
	result.WriteString("        } else {\n")
	result.WriteString("            bytes += 4;\n")
	result.WriteString("        }\n")
	result.WriteString("    }\n")
	result.WriteString("    if (bytes > SIZE_MAX - 1) {\n")
	result.WriteString("        fputs(\"[Runtime Error] string allocation size overflow\\n\", stderr);\n        abort();\n")
	result.WriteString("    }\n")
	result.WriteString("    hex_string_storage *storage = hex_heap_raw_allocate(h.identity, sizeof(hex_string_storage) + bytes + 1, _Alignof(hex_string_storage));\n")
	result.WriteString("    size_t out = 0;\n")
	result.WriteString("    for (size_t index = 0; index < length; index++) {\n")
	result.WriteString("        out += hex_utf8_encode(storage->bytes + out, data[index]);\n")
	result.WriteString("    }\n")
	result.WriteString("    storage->header.data = storage->bytes;\n")
	result.WriteString("    storage->header.byte_length = bytes;\n")
	result.WriteString("    storage->bytes[bytes] = 0;\n")
	result.WriteString("    return &storage->header;\n}\n")
	result.WriteString("\nstatic inline const hex_string *hex_string_to_string(hex_heap h, const hex_string *text) {\n")
	result.WriteString("    return hex_string_from_bytes(h, text->data, text->byte_length);\n}\n")
	result.WriteString("\nstatic inline const hex_string *hex_string_concat(hex_heap h, const hex_string *left, const hex_string *right) {\n")
	result.WriteString("    if (left->byte_length > SIZE_MAX - right->byte_length) {\n")
	result.WriteString("        fputs(\"[Runtime Error] string concatenation length overflow\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    size_t length = left->byte_length + right->byte_length;\n")
	result.WriteString("    hex_string_storage *storage = hex_heap_raw_allocate(h.identity, sizeof(hex_string_storage) + length + 1, _Alignof(hex_string_storage));\n")
	result.WriteString("    storage->header.data = storage->bytes;\n")
	result.WriteString("    storage->header.byte_length = length;\n")
	result.WriteString("    for (size_t index = 0; index < left->byte_length; index++) {\n")
	result.WriteString("        storage->bytes[index] = left->data[index];\n")
	result.WriteString("    }\n")
	result.WriteString("    for (size_t index = 0; index < right->byte_length; index++) {\n")
	result.WriteString("        storage->bytes[left->byte_length + index] = right->data[index];\n")
	result.WriteString("    }\n")
	result.WriteString("    storage->bytes[length] = 0;\n")
	result.WriteString("    return &storage->header;\n}\n")
	result.WriteString("\nstatic inline void hex_string_free(hex_heap h, const hex_string *text) {\n")
	result.WriteString("    if (text == NULL) {\n")
	result.WriteString("        fputs(\"[Runtime Error] double deallocation\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    size_t offset = *((size_t *)((unsigned char *)text - sizeof(size_t)));\n")
	result.WriteString("    hex_heap_header *header = (hex_heap_header *)((unsigned char *)text - offset);\n")
	result.WriteString("    if (header->allocator != h.identity) {\n")
	result.WriteString("        fputs(\"[Runtime Error] deallocation used the wrong allocator\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    if (!header->live) {\n")
	result.WriteString("        fputs(\"[Runtime Error] double deallocation\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    header->live = false;\n")
	result.WriteString("    free(header);\n}\n")
	result.WriteString("\nstatic inline hex_view_UInt8 hex_string_bytes(const hex_string *text) {\n")
	result.WriteString("    return (hex_view_UInt8){ text->data, text->byte_length };\n}\n")
	result.WriteString("\nstatic inline hex_view_UInt8 hex_string_slice(const hex_string *text, size_t start, size_t end) {\n")
	result.WriteString("    size_t index = 0;\n")
	result.WriteString("    size_t runes = 0;\n")
	result.WriteString("    while (index < text->byte_length) {\n")
	result.WriteString("        hex_utf8_next(text->data, text->byte_length, &index);\n")
	result.WriteString("        runes++;\n")
	result.WriteString("    }\n")
	result.WriteString("    if (!(start <= end && end <= runes)) {\n")
	result.WriteString("        fputs(\"[Runtime Error] string slice bounds out of range\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    size_t byteStart = 0;\n")
	result.WriteString("    size_t byteEnd = 0;\n")
	result.WriteString("    index = 0;\n")
	result.WriteString("    for (size_t rune = 0; rune < end; rune++) {\n")
	result.WriteString("        hex_utf8_next(text->data, text->byte_length, &index);\n")
	result.WriteString("        if (rune + 1 == start) {\n")
	result.WriteString("            byteStart = index;\n")
	result.WriteString("        }\n")
	result.WriteString("    }\n")
	result.WriteString("    byteEnd = index;\n")
	result.WriteString("    return (hex_view_UInt8){ text->data + byteStart, byteEnd - byteStart };\n}\n")
	result.WriteString("\nstatic inline size_t hex_string_rune_length(const hex_string *text) {\n")
	result.WriteString("    size_t index = 0;\n    size_t runes = 0;\n")
	result.WriteString("    while (index < text->byte_length) {\n")
	result.WriteString("        hex_utf8_next(text->data, text->byte_length, &index);\n")
	result.WriteString("        runes++;\n")
	result.WriteString("    }\n")
	result.WriteString("    return runes;\n}\n")
	result.WriteString("\nstatic inline bool hex_string_is_empty(const hex_string *text) {\n")
	result.WriteString("    return text->byte_length == 0;\n}\n")
	result.WriteString("\nstatic inline uint32_t hex_string_at_rune(const hex_string *text, size_t rune_index) {\n")
	result.WriteString("    size_t index = 0;\n    size_t rune = 0;\n")
	result.WriteString("    for (;;) {\n")
	result.WriteString("        if (index >= text->byte_length) {\n")
	result.WriteString("            fputs(\"[Runtime Error] String index is outside its bounds\\n\", stderr);\n            abort();\n")
	result.WriteString("        }\n")
	result.WriteString("        if (rune == rune_index) {\n")
	result.WriteString("            return hex_utf8_decode(text->data, text->byte_length, &index);\n")
	result.WriteString("        }\n")
	result.WriteString("        hex_utf8_next(text->data, text->byte_length, &index);\n")
	result.WriteString("        rune++;\n")
	result.WriteString("    }\n}\n")
	result.WriteString("\ntypedef struct hex_rune_cursor {\n    const uint8_t *data;\n    size_t length;\n    size_t offset;\n} hex_rune_cursor;\n")
	result.WriteString("\nstatic inline hex_rune_cursor hex_string_rune_cursor(const hex_string *text) {\n")
	result.WriteString("    return (hex_rune_cursor){ text->data, text->byte_length, 0 };\n}\n")
	result.WriteString("\nstatic inline bool hex_rune_cursor_has_next(hex_rune_cursor cursor) {\n")
	result.WriteString("    return cursor.offset < cursor.length;\n}\n")
	result.WriteString("\nstatic inline uint32_t hex_rune_cursor_next(hex_rune_cursor *cursor) {\n")
	result.WriteString("    if (cursor->offset >= cursor->length) {\n")
	result.WriteString("        fputs(\"[Runtime Error] RuneCursor has no next value\\n\", stderr);\n            abort();\n")
	result.WriteString("    }\n")
	result.WriteString("    return hex_utf8_decode(cursor->data, cursor->length, &cursor->offset);\n}\n")
	if stringState.needStrand {
		result.WriteString("\nstatic inline size_t hex_strand_rune_length(hex_strand text) {\n")
		result.WriteString("    size_t index = 0;\n    size_t runes = 0;\n")
		result.WriteString("    while (index < 32 && text.data[index] != 0) {\n")
		result.WriteString("        hex_utf8_next(text.data, 32, &index);\n")
		result.WriteString("        runes++;\n")
		result.WriteString("    }\n")
		result.WriteString("    return runes;\n}\n")
		result.WriteString("\nstatic inline bool hex_strand_is_empty(hex_strand text) {\n")
		result.WriteString("    return text.data[0] == 0;\n}\n")
		result.WriteString("\nstatic inline uint32_t hex_strand_at_rune(hex_strand text, size_t rune_index) {\n")
		result.WriteString("    size_t index = 0;\n    size_t rune = 0;\n")
		result.WriteString("    for (;;) {\n")
		result.WriteString("        if (index >= 32 || text.data[index] == 0) {\n")
		result.WriteString("            fputs(\"[Runtime Error] String index is outside its bounds\\n\", stderr);\n            abort();\n")
		result.WriteString("        }\n")
		result.WriteString("        if (rune == rune_index) {\n")
		result.WriteString("            return hex_utf8_decode(text.data, 32, &index);\n")
		result.WriteString("        }\n")
		result.WriteString("        hex_utf8_next(text.data, 32, &index);\n")
		result.WriteString("        rune++;\n")
		result.WriteString("    }\n}\n")
		result.WriteString("\nstatic inline const hex_string *hex_strand_to_string(hex_heap h, hex_strand text) {\n")
		result.WriteString("    size_t length = 0;\n")
		result.WriteString("    while (length < 32 && text.data[length] != 0) {\n")
		result.WriteString("        length++;\n")
		result.WriteString("    }\n")
		result.WriteString("    return hex_string_from_bytes(h, text.data, length);\n}\n")
	}
}
