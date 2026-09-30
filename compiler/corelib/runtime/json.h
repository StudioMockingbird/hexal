#ifndef HEXAL_JSON_H
#define HEXAL_JSON_H

#include "hexal.h"
#include "hexal/string.h"
{{if .Adapter}}
#include "hexal/error.h"
{{end}}// The JSON data model and its raw runtime declarations. The Value record is
// spelled exactly as a module-owned translated ADT of the same eight variants:
// the std/json module exercises it through the ordinary ADT tag spellings, and
// the tree helper and adapter own every definition exactly once program-wide.
// The yyjson types stay private to hexal/json_adapter.c; this header and the
// helper unit never name one.

typedef struct hex_list_JsonValue hex_list_JsonValue;
typedef struct hex_list_JsonValueMember hex_list_JsonValueMember;
typedef struct hex_t_JsonValueMember hex_t_JsonValueMember;

typedef struct hex_t_JsonValue {
    hex_tag tag;
    union {
        struct {
            bool hex_m_value;
        } Bool;
        struct {
            int64_t hex_m_value;
        } Int;
        struct {
            uint64_t hex_m_value;
        } UInt;
        struct {
            double hex_m_value;
        } Float;
        struct {
            const hex_string *hex_m_value;
        } Text;
        struct {
            hex_list_JsonValue *hex_m_items;
        } Array;
        struct {
            hex_list_JsonValueMember *hex_m_entries;
        } Object;
    } payload;
} hex_t_JsonValue;

// hex_t_JsonValueMember is the ordered object member record: an owning String
// name and an owning Value payload; the member order is the JSON document's
// written order and nothing sorts it.
struct hex_t_JsonValueMember {
    const hex_string *hex_m_name;
    hex_t_JsonValue hex_m_value;
};

// hex_json_free_raw releases the tree exactly like the explicit cleanup
// contract of every other owning type: every distinct allocation is released
// exactly once, shared subtrees make no attempt, and a cycle terminates. No
// yyjson spelling is involved. The raw suffix is the uniform spelling of
// every raw runtime entry point; the adapter and generated call sites agree
// on it.
extern void hex_json_free_raw(hex_heap h, hex_t_JsonValue value);

{{if .Adapter}}
// hex_json_value_result is the raw result record: the translated tree on
// success, otherwise the stable Hexal kind and an owned message on failure.
typedef struct hex_json_value_result {
    bool ok;
    hex_t_JsonValue value;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_json_value_result;

// hex_json_string_result mirrors it for stringify: an owned UTF-8 Hexal
// string on success, otherwise kind and message. A NULL message would crash
// the wrapper's hex_error_message call, so every failure sets both fields.
typedef struct hex_json_string_result {
    bool ok;
    const hex_string *value;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_json_string_result;

// The raw adapter entry points. Every Hexal byte they touch runs on the
// caller's Heap through one custom yyjson allocator; no C allocator
// participates and neither operation leaks on any path.
extern hex_json_value_result hex_json_parse_raw(hex_heap h, const hex_string *text);
extern hex_json_string_result hex_json_stringify_raw(hex_heap h, hex_t_JsonValue value);

{{end}}#endif
