#include "json.h"
#include "hexal/list.h"

// The Hexal-tree helper unit: every allocation it touches is Hexal-owned, and
// every distinct one releases exactly once. No yyjson spelling appears here;
// the adapter unit owns the private include.

// hex_json_seen is one visited allocation identity of the free walk: a plain
// intrusive chain over the caller's heap. It holds the set of container
// pointers and String pointers the walk has scheduled to release, so shared
// subtrees and aliased names release once and any cycle terminates. The chain
// itself is released before return, so no visited-set allocation escapes the
// operation.
typedef struct hex_json_seen {
    const void *pointer;
    struct hex_json_seen *next;
} hex_json_seen;

static bool hex_json_seen_add(hex_json_seen **head, hex_heap h, const void *pointer) {
    for (const hex_json_seen *cursor = *head; cursor != NULL; cursor = cursor->next) {
        if (cursor->pointer == pointer) {
            return false;
        }
    }
    hex_json_seen *node = hex_heap_allocate(sizeof(hex_json_seen));
    node->pointer = pointer;
    node->next = *head;
    *head = node;
    return true;
}

static void hex_json_seen_release(hex_json_seen *head, hex_heap h) {
    (void)h;
    while (head != NULL) {
        hex_json_seen *next = head->next;
        hex_heap_free(head);
        head = next;
    }
}

// The configured depth governs this traversal exactly as it governs the
// reader and writer: a value entered below the budget is unreachable from a
// parsed tree and only a user-built tree reaches it, where the walk traps
// instead of recursing without bound. Containers check after the seen set so
// a cycle at any depth terminates through the set rather than the guard.
static void hex_json_free_tree(hex_t_JsonValue value, hex_json_seen **seen, hex_heap h, int depth_left) {
    switch (value.tag) {
    case {{.TagText}}: {
        const hex_string *text = value.payload.Text.hex_m_value;
        if (text != nullptr) {
            if (depth_left < 0) {
                hex_runtime_trap("[Runtime Error] JSON value nests too deeply\n");
            }
            if (hex_json_seen_add(seen, h, text)) {
                hex_string_free(h, text);
            }
        }
        return;
    }
    case {{.TagArray}}: {
        hex_list_JsonValue *items = value.payload.Array.hex_m_items;
        if (items == nullptr || !hex_json_seen_add(seen, h, items)) {
            // An already-scheduled alias releases no second time; returning
            // leaves the walk's remaining siblings untouched.
            return;
        }
        if (depth_left < 0) {
            hex_runtime_trap("[Runtime Error] JSON value nests too deeply\n");
        }
        for (size_t index = 0; index < items->length; index++) {
            hex_json_free_tree(items->data[index], seen, h, depth_left - 1);
        }
        hex_list_free_JsonValue(h, items);
        return;
    }
    case {{.TagObject}}: {
        hex_list_JsonValueMember *entries = value.payload.Object.hex_m_entries;
        if (entries == nullptr || !hex_json_seen_add(seen, h, entries)) {
            return;
        }
        if (depth_left < 0) {
            hex_runtime_trap("[Runtime Error] JSON value nests too deeply\n");
        }
        for (size_t index = 0; index < entries->length; index++) {
            const hex_string *name = entries->data[index].hex_m_name;
            if (name != nullptr && hex_json_seen_add(seen, h, name)) {
                hex_string_free(h, name);
            }
            hex_json_free_tree(entries->data[index].hex_m_value, seen, h, depth_left - 1);
        }
        hex_list_free_JsonValueMember(h, entries);
        return;
    }
    default:
        if (depth_left < 0) {
            hex_runtime_trap("[Runtime Error] JSON value nests too deeply\n");
        }
        return;
    }
}

void hex_json_free_raw(hex_heap h, hex_t_JsonValue value) {
    hex_json_seen *seen = NULL;
    hex_json_free_tree(value, &seen, h, {{.Depth}});
    hex_json_seen_release(seen, h);
}
