{{define "module_header_open"}}#ifndef {{.Guard}}
#define {{.Guard}}

#include "hexal.h"
{{range .Components}}#include "{{.}}"
{{end}}{{if .ForeignHeaders}}
{{range .ForeignHeaders}}{{.}}
{{end}}{{end}}{{end}}{{define "module_header_close"}}{{if .Prototypes}}
/* Exported and foreign function prototypes. */
{{.Prototypes}}{{end}}{{if .ExtraFrames}}
/* Spawn entry adapter argument frames. */
{{.ExtraFrames}}{{end}}
#endif
{{end}}{{define "object_forward"}}
{{if .Line}}#line {{.Line}} "{{.Filename}}"
{{end}}typedef struct {{.CName}} {{.CName}};
{{end}}{{define "object_body"}}
{{if .Line}}#line {{.Line}} "{{.Filename}}"
{{end}}struct {{.CName}} {
{{if .Empty}}    unsigned char hex_empty;
{{end}}{{range .Members}}{{if .Line}}#line {{.Line}} "{{.Filename}}"
{{end}}    {{.Declaration}};
{{end}}};
{{end}}
{{define "print_error_direct"}}static void hex_print_error_direct(hex_print_buffer *out, const hex_t_Error *value) {
    hex_print_text(out, value->hex_m_file->data, value->hex_m_file->byte_length);
    hex_print_text(out, (const uint8_t *)":", 1);
    hex_print_size(out, value->hex_m_line);
    hex_print_text(out, (const uint8_t *)":", 1);
    hex_print_size(out, value->hex_m_column);
    hex_print_text(out, (const uint8_t *)": ", 2);
    {{.HeaderType}} header = hex_error_kind_header(value->hex_m_kind);
    hex_print_text(out, header.data, header.byte_length);
    hex_print_text(out, (const uint8_t *)": ", 2);
    hex_print_text(out, value->hex_m_message.data, value->hex_m_message.byte_length);
}
{{end}}{{define "print_error_nested"}}static void hex_print_error_nested(hex_print_buffer *out, const hex_t_Error *value) {
    hex_print_text(out, (const uint8_t *)"Error { file = ", 15);
    hex_print_quoted_text(out, value->hex_m_file->data, value->hex_m_file->byte_length);
    hex_print_text(out, (const uint8_t *)", line = ", 9);
    hex_print_size(out, value->hex_m_line);
    hex_print_text(out, (const uint8_t *)", column = ", 11);
    hex_print_size(out, value->hex_m_column);
    hex_print_text(out, (const uint8_t *)", kind = ", 9);
    {{.HeaderType}} nested_header = hex_error_kind_header(value->hex_m_kind);
    hex_print_quoted_text(out, nested_header.data, nested_header.byte_length);
    hex_print_text(out, (const uint8_t *)", message = ", 12);
    hex_print_quoted_text(out, value->hex_m_message.data, value->hex_m_message.byte_length);
    hex_print_text(out, (const uint8_t *)" }", 2);
}
{{end}}{{define "print_nested_forward"}}{{range .Names}}static void hex_print_nested_{{.}}(hex_print_buffer *out, const void *value);
{{end}}{{end}}{{define "print_nested_string"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    const hex_string *text = value;
    hex_print_quoted_text(out, text->data, text->byte_length);
}
{{end}}{{define "print_nested_inline_string"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    hex_text text = hex_text_inline(value);
    hex_print_quoted_text(out, text.data, text.length);
}
{{end}}{{define "print_nested_error"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    hex_print_error_nested(out, value);
}
{{end}}{{define "print_nested_error_kind"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    {{.HeaderType}} text = hex_error_kind_header(*(const hex_t_ErrorKind *)value);
    hex_print_text(out, text.data, text.byte_length);
}
{{end}}{{define "print_nested_bool"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    hex_print_bool(out, *(const bool *)value);
}
{{end}}{{define "print_nested_nil"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    (void)value;
    hex_print_nil(out);
}
{{end}}{{define "print_nested_int"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    hex_print_int{{.Bits}}(out, *(const int{{.Bits}}_t *)value);
}
{{end}}{{define "print_nested_size"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    hex_print_size(out, *(const size_t *)value);
}
{{end}}{{define "print_nested_uint"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    hex_print_uint{{.Bits}}(out, *(const uint{{.Bits}}_t *)value);
}
{{end}}{{define "print_nested_float32"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    hex_print_float32(out, *(const float *)value);
}
{{end}}{{define "print_nested_float64"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    hex_print_float64(out, *(const double *)value);
}
{{end}}{{define "print_nested_object_empty"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    (void)value;
    hex_print_text(out, (const uint8_t *)"{{.Name}} {}", {{.TextLen}});
}
{{end}}{{define "print_nested_object"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    const {{.CName}} *v = value;
    hex_print_text(out, (const uint8_t *)"{{.Name}} { ", {{.NameLen}});
{{range .Members}}{{if .Separator}}    hex_print_text(out, (const uint8_t *)", ", 2);
{{end}}    hex_print_text(out, (const uint8_t *)"{{.Label}}", {{.LabelLen}});
    hex_print_nested_{{.NestedCName}}(out, {{.Arg}});
{{end}}    hex_print_text(out, (const uint8_t *)" }", 2);
}
{{end}}{{define "print_nested_adt"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    const {{.CName}} *v = value;
    switch (v->tag) {
{{range .Variants}}    case {{.Tag}}:
        hex_print_text(out, (const uint8_t *)"{{.Label}}", {{.LabelLen}});
{{if .Payload}}        hex_print_text(out, (const uint8_t *)" { ", 3);
{{range .Payload}}{{if .Separator}}        hex_print_text(out, (const uint8_t *)", ", 2);
{{end}}        hex_print_text(out, (const uint8_t *)"{{.Label}}", {{.LabelLen}});
        hex_print_nested_{{.NestedCName}}(out, {{.Arg}});
{{end}}        hex_print_text(out, (const uint8_t *)" }", 2);
{{end}}        break;
{{end}}    default:
        abort();
    }
}
{{end}}{{define "print_nested_array"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    const {{.CName}} *v = value;
    hex_print_text(out, (const uint8_t *)"[", 1);
    for (size_t index = 0; index < {{.Length}}; index++) {
        if (index > 0) { hex_print_text(out, (const uint8_t *)", ", 2); }
        hex_print_nested_{{.ElementCName}}(out, {{.ElementArg}});
    }
    hex_print_text(out, (const uint8_t *)"]", 1);
}
{{end}}{{define "print_nested_sequence"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    const {{.CName}} *v = value;
    hex_print_text(out, (const uint8_t *)"[", 1);
    for (size_t index = 0; index < v->length; index++) {
        if (index > 0) { hex_print_text(out, (const uint8_t *)", ", 2); }
        hex_print_nested_{{.ElementCName}}(out, {{.ElementArg}});
    }
    hex_print_text(out, (const uint8_t *)"]", 1);
}
{{end}}{{define "print_nested_dict"}}static void hex_print_nested_{{.CName}}(hex_print_buffer *out, const void *value) {
    const {{.CName}} *v = value;
    hex_print_text(out, (const uint8_t *)"{", 1);
    bool first = true;
    for (size_t index = 0; index < v->capacity; index++) {
        if (!v->buckets[index].active) { continue; }
        if (index > 0) { hex_print_text(out, (const uint8_t *)", ", 2); }
        first = false;
        hex_print_nested_{{.KeyCName}}(out, {{.KeyArg}});
        hex_print_text(out, (const uint8_t *)": ", 2);
        hex_print_nested_{{.ValueCName}}(out, {{.ValueArg}});
    }
    hex_print_text(out, (const uint8_t *)"}", 1);
}
{{end}}
{{define "shift_helper"}}
static inline {{.CType}} {{.Name}}({{.CType}} left, uint64_t count) {
    if (!(count < {{.Width}}ULL)) {
        hex_runtime_trap("[Runtime Error] numeric operation failed\n");
    }
    return {{.Shifted}};
}
{{end}}{{define "bitcast_helper"}}
static inline {{.Target}} {{.Name}}({{.Source}} value) {
    {{.Target}} result;
    memcpy(&result, &value, sizeof(result));
    return result;
}
{{end}}{{define "endian_to_bytes"}}
static inline {{.ArrayType}} {{.Name}}({{.CType}} value) {
    {{.ArrayType}} result = ( {{.ArrayType}} ){{.DesignatedInit}};
{{$unsigned := .Unsigned}}{{range .Bytes}}    result.data[{{.Index}}] = (uint8_t)(({{$unsigned}})value >> {{.Shift}});
{{end}}    return result;
}
{{end}}{{define "endian_from_bytes"}}
static inline {{.CType}} {{.Name}}(const {{.ArrayType}} *bytes) {
    {{.Unsigned}} value = 0;
{{$unsigned := .Unsigned}}{{range .Bytes}}    value |= ({{$unsigned}})(bytes->data[{{.Index}}]) << {{.Shift}};
{{end}}    {{.ReturnLine}}
}
{{end}}{{define "c_prototype"}}{{.Linkage}}{{.Result}} {{.Name}}({{.Params}});
{{end}}{{define "definition_open"}}{{.Linkage}}{{.Result}} {{.Name}}({{.Params}}) {
{{end}}{{define "definition_close"}}}

{{end}}{{define "section_blank"}}
{{end}}{{define "extern_value_decl"}}extern {{.Declarator}};
{{end}}{{define "heap_allocate_helper"}}
static {{.Return}} {{.Helper}}(hex_heap h, {{.Element}} initial) {
    (void)h;
    {{.Element}} *pointer = hex_heap_allocate(sizeof({{.Element}}));
    *pointer = initial;
    return pointer;
}
{{end}}{{define "heap_allocate_aligned_helper"}}
static {{.Return}} {{.Helper}}(hex_heap h, {{.Element}} initial, size_t alignment) {
    (void)h;
    {{.Element}} *pointer = hex_heap_allocate_aligned(sizeof({{.Element}}), alignment, alignof({{.Element}}));
    *pointer = initial;
    return pointer;
}
{{end}}{{define "stash_new_helper"}}
static inline hex_stash *{{.Name}}(void) {
    return hex_stash_new(sizeof({{.Spelling}}), _Alignof({{.Spelling}}));
}
{{end}}{{define "stash_allocate_helper"}}
static inline {{.Spelling}} *{{.Name}}(hex_stash *stash, {{.Spelling}} initial) {
    {{.Spelling}} *slot = ({{.Spelling}} *)hex_stash_allocate(stash);
    *slot = initial;
    return slot;
}
{{end}}{{define "nominal_forward"}}
typedef struct {{.Name}} {{.Name}};
{{end}}{{define "union_body"}}
struct {{.Name}} {
    hex_tag tag;
    union {
{{range .Members}}        {{.Declaration}};
{{end}}    } payload;
};
{{end}}{{define "union_widen"}}
static {{.Destination}} {{.Name}}({{.Source}} value) {
    switch (value.tag) {
{{range .Cases}}    case {{.Tag}}:
        return ({{$.Destination}}){ .tag = value.tag{{if .Payload}}, {{.Payload}}{{end}} };
{{end}}    default:
        abort();
    }
}
{{end}}{{define "union_equal"}}
static bool {{.Name}}({{.CName}} left, {{.CName}} right) {
    if (left.tag != right.tag) return false;
    switch (left.tag) {
{{range .Cases}}    case {{.Tag}}:
{{.Comparisons}}        return true;
{{end}}    default:
        abort();
    }
}
{{end}}{{define "union_truthy"}}
static bool {{.CName}}_truthy({{.CName}} value) {
    switch (value.tag) {
{{range .Cases}}    case {{.Tag}}:
{{.Return}}
{{end}}    default:
        abort();
    }
}
{{end}}{{define "adt_body"}}
struct {{.Name}} {
    hex_tag tag;
{{if .HasPayload}}    union {
{{range .Variants}}        struct {
{{range .Members}}            {{.}};
{{end}}        } {{.Name}};
{{end}}    } payload;
{{end}}};
{{end}}{{define "equality_helper"}}
static bool {{.Name}}({{.Parameter}}) {
{{.Body}}    return true;
}
{{end}}{{define "rune_utf8_length_helper"}}
// hex_rune_utf8_length returns the encoded UTF-8 byte length of one
// Unicode scalar: 1 through 4. The value is already a valid scalar, so
// only the encoding range decides the width.
static inline size_t hex_rune_utf8_length(uint32_t value) {
    if (value < 0x80) {
        return 1;
    }
    if (value < 0x800) {
        return 2;
    }
    if (value < 0x10000) {
        return 3;
    }
    return 4;
}
{{end}}{{define "rune_from_adapter"}}
// hex_rune_from_{{.Suffix}} turns a UInt32 into a checked Unicode scalar. It
// rejects surrogates and values above U+10FFFF and reports, never traps.
static inline {{.CName}} hex_rune_from_{{.Suffix}}(uint32_t value, size_t line, size_t column) {
    if (value <= 0x10FFFF && !(value >= 0xD800 && value <= 0xDFFF)) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = value };
    }
    return {{.Error}};
}
{{end}}{{define "string_from_runes_adapter"}}
// hex_string_from_runes_{{.Suffix}} encodes a scalar sequence into text. It
// rejects surrogates and values above U+10FFFF and reports, never traps.
static inline {{.CName}} hex_string_from_runes_{{.Suffix}}(hex_heap h, hex_slice_Rune runes, size_t line, size_t column) {
    for (size_t index = 0; index < runes.length; index++) {
        if (!hex_rune_valid(runes.data[index])) {
            return {{.Error}};
        }
    }
    return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = hex_string_from_runes(h, runes.data, runes.length) };
}
{{end}}{{define "rune_categories_open"}}
// hex_rune_categories maps the runtime's general-category ordinal to the
// program-wide UnicodeCategory tag, in declaration order.
static const hex_tag hex_rune_categories[{{.Count}}] = {
{{end}}{{define "category_entry"}}    {{.Tag}},
{{end}}{{define "category_close"}};
{{end}}{{define "casefold_adapter"}}
// hex_string_casefold_{{.Suffix}} folds text and reports a failed transform as
// an Error. The transform buffer never escapes the runtime core.
static inline {{.CName}} hex_string_casefold_{{.Suffix}}(hex_heap h, hex_text text, size_t line, size_t column) {
    const hex_string *folded = hex_text_casefold(h, text);
    if (folded == nullptr) {
        return {{.Error}};
    }
    return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = folded };
}
{{end}}{{define "normalize_form_open"}}
// hex_normalize_form maps a NormalizationForm tag to the runtime's fixed
// form index (0 NFC, 1 NFD, 2 NFKC, 3 NFKD); an unknown tag reports -1.
static inline int32_t hex_normalize_form(hex_tag tag) {
{{end}}{{define "form_case"}}    if (tag == {{.Tag}}) {
        return {{.Index}};
    }
{{end}}{{define "normalize_form_close"}}    return -1;
}
{{end}}{{define "normalize_adapter"}}
// hex_string_normalize_{{.Suffix}} normalizes text and reports a failed transform
// as an Error. The transform buffer never escapes the runtime core.
static inline {{.CName}} hex_string_normalize_{{.Suffix}}(hex_heap h, hex_text text, hex_tag form, size_t line, size_t column) {
    const hex_string *normalized = hex_text_normalize(h, text, hex_normalize_form(form));
    if (normalized == nullptr) {
        return {{.Error}};
    }
    return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = normalized };
}
{{end}}{{define "from_bytes_adapter"}}
static inline {{.CName}} hex_string_from_bytes_{{.Suffix}}(hex_heap h, hex_slice_UInt8 bytes, size_t line, size_t column) {
    if (hex_utf8_valid(bytes.data, bytes.length)) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = hex_string_make(h, hex_text_view(bytes)) };
    }
    return {{.Error}};
}
{{end}}{{define "string_concat_adapter"}}
static inline {{.CName}} hex_string_concat_{{.Suffix}}(hex_heap h, hex_text left, hex_slice_UInt8 right, size_t line, size_t column) {
    if (hex_utf8_valid(right.data, right.length)) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = hex_string_join(h, left, hex_text_view(right)) };
    }
    return {{.Error}};
}
{{end}}{{define "inline_from_bytes_adapter"}}
static inline {{.CName}} hex_text_from_bytes_{{.Suffix}}(hex_slice_UInt8 bytes, size_t line, size_t column) {
    if (bytes.length > {{.Capacity}}) {
        return {{.Overflow}};
    }
    if (!hex_utf8_valid(bytes.data, bytes.length)) {
        return {{.Invalid}};
    }
    return ({{.CName}}){ .tag = {{.Tag}}, .payload.{{.Field}} = {{.Fill}} };
}
{{end}}{{define "inline_concat_adapter"}}
static inline {{.CName}} hex_text_concat_{{.Suffix}}(hex_slice_UInt8 left, hex_slice_UInt8 right, size_t line, size_t column) {
    size_t total;
    if (ckd_add(&total, left.length, right.length) || total > {{.Capacity}}) {
        return {{.Overflow}};
    }
    {{.Destination}} value = { .byte_length = total };
    if (left.length != 0) {
        memcpy(value.data, left.data, left.length);
    }
    if (right.length != 0) {
        memcpy(value.data + left.length, right.data, right.length);
    }
    if (!hex_utf8_valid(value.data, total)) {
        return {{.Invalid}};
    }
    return ({{.CName}}){ .tag = {{.Tag}}, .payload.{{.Field}} = value };
}
{{end}}{{define "address_parse_adapter"}}
static inline {{.CName}} hex_address_parse_{{.Suffix}}(const hex_string *text, uint16_t port, size_t line, size_t column) {
    hex_address_parsed parsed = hex_address_parse(text, port);
    if (parsed.status == 0) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = parsed.address };
    }
    return {{.Error}};
}
{{end}}{{define "dns_resolve_adapter"}}
static inline {{.CName}} hex_dns_resolve_{{.Suffix}}(const hex_string *host, const hex_string *service, hex_heap heap, size_t line, size_t column) {
    hex_dns_result resolved = hex_dns_resolve(host, service, heap);
    if (resolved.status == 0) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = resolved.list };
    }
    return {{.Error}};
}
{{end}}{{define "tcp_connect_adapter"}}
static inline {{.CName}} hex_tcp_connect_{{.Suffix}}(hex_t_Address address, size_t line, size_t column) {
    hex_tcp_connect_result connected = hex_tcp_connect(address);
    if (connected.status == 0) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = connected.connection };
    }
    return {{.Error}};
}
{{end}}{{define "tcp_listen_adapter"}}
static inline {{.CName}} hex_tcp_listen_{{.Suffix}}(hex_t_Address address, size_t backlog, size_t line, size_t column) {
    hex_tcp_listen_result listened = hex_tcp_listen(address, backlog);
    if (listened.status == 0) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = listened.listener };
    }
    return {{.Error}};
}
{{end}}{{define "tcp_accept_adapter"}}
static inline {{.CName}} hex_tcp_accept_{{.Suffix}}(hex_tcp_listener listener, size_t line, size_t column) {
    hex_tcp_accept_result accepted = hex_tcp_accept(listener);
    if (accepted.status == 0) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = accepted.connection };
    }
    return {{.Error}};
}
{{end}}{{define "tcp_read_adapter"}}
static inline {{.CName}} hex_tcp_read_{{.Suffix}}(hex_tcp_connection connection, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {
    hex_tcp_transfer transfer = hex_tcp_read(connection, into, max);
    switch (transfer.status) {
    case 0:
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = transfer.count };
    case HEX_NETWORK_EOS:
        return ({{.CName}}){ .tag = {{.EosTag}} };
    default:
        return {{.Error}};
    }
}
{{end}}{{define "tcp_status_adapter"}}
static inline {{.CName}} hex_tcp_{{.Operation}}_{{.Suffix}}({{.Params}}, size_t line, size_t column) {
    int status = {{.Call}};
    if (status == 0) {
        return ({{.CName}}){ .tag = {{.Success}} };
    }
    return {{.Error}};
}
{{end}}{{define "io_open_adapter"}}
static inline {{.CName}} hex_io_open_{{.Name}}(size_t line, size_t column) {
    hex_io_open opened = hex_io_{{.Name}}();
    if (opened.status == HEX_IO_OK) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = opened.stream };
    }
    return {{.Error}};
}
{{end}}{{define "io_read_adapter"}}
static inline {{.CName}} hex_io_read_{{.Suffix}}(hex_io stream, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {
    hex_io_transfer transfer = hex_io_read(stream, into, max);
    switch (transfer.status) {
    case HEX_IO_OK:
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = transfer.count };
    case HEX_IO_EOS:
        return ({{.CName}}){ .tag = {{.EosTag}} };
    case HEX_IO_NOT_READABLE:
        return {{.Guard}};
    default:
        return {{.Error}};
    }
}
{{end}}{{define "bytes_read_adapter"}}
static inline {{.CName}} hex_bytes_read_{{.Suffix}}(hex_bytes *stream, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {
    hex_io_transfer transfer = hex_bytes_read(stream, into, max);
    switch (transfer.status) {
    case HEX_IO_OK:
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = transfer.count };
    case HEX_IO_EOS:
        return ({{.CName}}){ .tag = {{.EosTag}} };
    case HEX_IO_SELF_READ:
        return {{.Guard}};
    default:
        return {{.Error}};
    }
}
{{end}}{{define "io_write_adapter"}}
static inline {{.CName}} hex_io_write_{{.Suffix}}(hex_io stream, hex_slice_UInt8 from, size_t line, size_t column) {
    hex_io_transfer transfer = hex_io_write(stream, from);
    switch (transfer.status) {
    case HEX_IO_OK:
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = transfer.count };
    case HEX_IO_NOT_WRITABLE:
        return {{.Guard}};
    default:
        return {{.Error}};
    }
}
{{end}}{{define "bytes_write_adapter"}}
static inline {{.CName}} hex_bytes_write_{{.Suffix}}(hex_bytes *stream, hex_slice_UInt8 from, size_t line, size_t column) {
    hex_io_transfer transfer = hex_bytes_write(stream, from);
    switch (transfer.status) {
    case HEX_IO_OK:
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = transfer.count };
    case HEX_IO_OVERLAP:
        return {{.Guard}};
    default:
        return {{.Error}};
    }
}
{{end}}{{define "seek_adapter"}}
static inline {{.CName}} {{.Core}}seek_{{.Suffix}}({{.Receiver}} stream, hex_t_Seek to, size_t line, size_t column) {
    hex_io_position moved;
    switch (to.tag) {
    case {{.StartTag}}:
        moved = {{.StartCall}};
        break;
    case {{.CurrentTag}}:
        moved = {{.CurrentCall}};
        break;
    case {{.EndTag}}:
        moved = {{.EndCall}};
        break;
    default:
        abort();
    }
    if (moved.status == HEX_IO_OK) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = (size_t)moved.position };
    }
    return {{.Error}};
}
{{end}}{{define "io_close_adapter"}}
static inline {{.CName}} hex_io_close_{{.Suffix}}(hex_io stream, size_t line, size_t column) {
    hex_io_status_only closed = hex_io_close(stream);
    if (closed.status == HEX_IO_OK) {
        return ({{.CName}}){ .tag = {{.Success}} };
    }
    return {{.Error}};
}
{{end}}{{define "division_helper_open"}}
static inline {{.CName}} hex_{{.Suffix}}_{{.CName}}({{.CName}} left, {{.CName}} right) {
{{end}}{{define "division_zero_guard"}}    if (right == 0) {
        hex_runtime_trap("[Runtime Error] numeric operation failed\n");
    }
{{end}}{{define "division_min_guard"}}    if (left == {{.Minimum}} && right == -1) {
{{end}}{{define "division_min_return"}}        return {{.Minimum}};
    }
{{end}}{{define "division_rem_close"}}        return 0;
    }
{{end}}{{define "division_helper_close"}}    return left {{.Operation}} right;
}
{{end}}{{define "process_start_adapter"}}
static inline {{.CName}} hex_process_start_{{.Suffix}}(hex_t_ProcessOptions options, size_t line, size_t column) {
    hex_process_options raw;
    raw.program = options.hex_m_program;
    raw.arguments = options.hex_m_arguments;
    raw.environment_replace = options.hex_m_environment.tag == {{.ReplaceTag}};
    raw.environment_values = raw.environment_replace ? options.hex_m_environment.payload.Replace.hex_m_values : nullptr;
    raw.working_directory = options.hex_m_working_directory.tag == {{.WorkingTag}} ? options.hex_m_working_directory.payload.{{.WorkingField}} : nullptr;
    raw.input = {{.Input}};
    raw.output = {{.Output}};
    raw.error = {{.Error}};
    hex_process_start_result started = hex_process_start(raw);
    if (started.status == 0) {
        hex_t_StartedProcess value = {
            .hex_m_process = started.process,
            .hex_m_input = started.input.present ? (hex_t_Pipe_Nil){ .tag = {{.PipeTag}}, .payload.{{.PipeField}} = started.input.pipe } : (hex_t_Pipe_Nil){ .tag = {{.NilTag}} },
            .hex_m_output = started.output.present ? (hex_t_Pipe_Nil){ .tag = {{.PipeTag}}, .payload.{{.PipeField}} = started.output.pipe } : (hex_t_Pipe_Nil){ .tag = {{.NilTag}} },
            .hex_m_error = started.error.present ? (hex_t_Pipe_Nil){ .tag = {{.PipeTag}}, .payload.{{.PipeField}} = started.error.pipe } : (hex_t_Pipe_Nil){ .tag = {{.NilTag}} },
        };
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = value };
    }
    return {{.Failure}};
}
{{end}}{{define "process_wait_adapter"}}
static inline {{.CName}} hex_process_wait_{{.Suffix}}(hex_process receiver, size_t line, size_t column) {
    hex_process_wait_result waited = hex_process_wait(receiver);
    if (waited.status == 0) {
        hex_t_ExitStatus value = waited.terminated ? (hex_t_ExitStatus){ .tag = {{.Terminated}} } : (hex_t_ExitStatus){ .tag = {{.Exited}}, .payload.Exited.hex_m_code = waited.exit_code };
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = value };
    }
    return {{.Failure}};
}
{{end}}{{define "owned_status_adapter"}}
static inline {{.CName}} hex_{{.Owner}}_{{.Operation}}_{{.Suffix}}({{.Params}}, size_t line, size_t column) {
    int status = {{.Call}};
    if (status == 0) {
        return ({{.CName}}){ .tag = {{.Success}} };
    }
    return {{.Error}};
}
{{end}}{{define "pipe_read_adapter"}}
static inline {{.CName}} hex_pipe_read_{{.Suffix}}(hex_pipe receiver, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {
    hex_pipe_transfer transfer = hex_pipe_read(receiver, into, max);
    switch (transfer.status) {
    case 0:
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = transfer.count };
    case HEX_PROCESS_EOS:
        return ({{.CName}}){ .tag = {{.EosTag}} };
    default:
        return {{.Error}};
    }
}
{{end}}{{define "file_mode_case"}}    case {{.Tag}}:
        variant = {{.Index}};
        break;
{{end}}{{define "file_open_adapter"}}
static inline {{.CName}} hex_file_open_{{.Suffix}}(const hex_string *path, hex_t_FileMode mode, size_t line, size_t column) {
    uint8_t variant;
    switch (mode.tag) {
{{.Cases}}    default:
        abort();
    }
    hex_file_opened opened = hex_file_open(path, variant);
    if (opened.status == 0) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = opened.file };
    }
    return {{.Failure}};
}
{{end}}{{define "file_read_adapter"}}
static inline {{.CName}} hex_file_read_{{.Suffix}}(hex_file file, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {
    hex_file_transfer transfer = hex_file_read(file, into, max);
    switch (transfer.status) {
    case 0:
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = transfer.count };
    case HEX_FILE_EOS:
        return ({{.CName}}){ .tag = {{.EosTag}} };
    case HEX_FILE_NOT_READABLE:
        return {{.Guard}};
    default:
        return {{.Error}};
    }
}
{{end}}{{define "file_size_adapter"}}
static inline {{.CName}} hex_file_{{.Operation}}_{{.Suffix}}(hex_file file, {{.Parameters}}, size_t line, size_t column) {
    hex_file_transfer transfer = {{.Call}};
    if (transfer.status == 0) {
        return ({{.CName}}){ .tag = {{.Success}}, .payload.{{.Field}} = transfer.count };
    }
{{.Gate}}    return {{.Failure}};
}
{{end}}{{define "file_status_adapter"}}
static inline {{.CName}} hex_file_{{.Operation}}_{{.Suffix}}(hex_file file, size_t line, size_t column) {
    int status = hex_file_{{.Operation}}(file);
    if (status == 0) {
        return ({{.CName}}){ .tag = {{.Success}} };
    }
{{.Gate}}    return {{.Error}};
}
{{end}}{{define "helper_prototype"}}static {{.Result}} {{.Name}}({{.Params}});
{{end}}{{define "blank_line"}}
{{end}}{{define "helper_open"}}static {{.Result}} {{.Name}}({{.Params}}) {
{{end}}{{define "helper_close"}}}

{{end}}{{define "conversion_open"}}
static inline {{.Target}} {{.Name}}({{.Source}} value) {
{{end}}{{define "raw_text"}}{{.Text}}{{end}}{{define "env_struct_open"}}typedef struct {
{{end}}{{define "env_field"}}    {{.Field}};
{{end}}{{define "env_struct_close"}}} {{.Name}};

{{end}}{{define "corelib_string_adapter"}}
static inline {{.CName}} {{.Runtime}}_{{.Suffix}}({{.Parameters}}size_t line, size_t column) {
    hex_program_string_result query = {{.Call}}({{.Arguments}});
    if (query.ok) {
        return ({{.CName}}){ .tag = {{.Tag}}, .payload.{{.Field}} = query.value };
    }
    return {{.Failure}};
}
{{end}}{{define "corelib_slice_adapter"}}
static inline {{.CName}} {{.Runtime}}_{{.Suffix}}({{.Parameters}}size_t line, size_t column) {
    hex_program_arguments_result query = {{.Call}}({{.Arguments}});
    if (query.ok) {
        return ({{.CName}}){ .tag = {{.Tag}}, .payload.{{.Field}} = ({{.Member}}){ .data = query.items, .length = query.count } };
    }
    return {{.Failure}};
}
{{end}}{{define "corelib_nil_adapter"}}
static inline {{.CName}} {{.Runtime}}_{{.Suffix}}({{.Parameters}}size_t line, size_t column) {
    hex_entropy_fill_result query = {{.Call}}({{.Arguments}});
    if (query.ok) {
        return ({{.CName}}){ .tag = {{.Tag}} };
    }
    return {{.Failure}};
}
{{end}}{{define "signals_new_adapter"}}
static inline {{.CName}} hex_signals_new_{{.Suffix}}(hex_slice_Signal subscriptions, size_t line, size_t column) {
    uint8_t raw[3];
    size_t count = subscriptions.length;
    for (size_t index = 0; index < count && index < 3; index++) {
        hex_t_Signal item = subscriptions.data[index];
        raw[index] = item.tag == {{.Interrupt}} ? HEX_SIGNAL_INTERRUPT : item.tag == {{.Hangup}} ? HEX_SIGNAL_HANGUP : HEX_SIGNAL_TERMINATE;
    }
    hex_signals_new_result created = hex_signals_new(raw, count);
    if (created.status == 0) {
        return ({{.CName}}){ .tag = {{.Tag}}, .payload.{{.Field}} = created.signals };
    }
    return {{.Failure}};
}
{{end}}{{define "signals_next_adapter"}}
static inline {{.CName}} hex_signals_next_{{.Suffix}}(hex_signals receiver, size_t line, size_t column) {
    hex_signals_next_result next = hex_signals_next(receiver);
    switch (next.status) {
    case 0: {
        hex_tag tag = next.signal == HEX_SIGNAL_INTERRUPT ? {{.Interrupt}} : next.signal == HEX_SIGNAL_HANGUP ? {{.Hangup}} : {{.Terminate}};
        return ({{.CName}}){ .tag = {{.Tag}}, .payload.{{.Field}} = (hex_t_Signal){ .tag = tag } };
    }
    case HEX_SIGNAL_EOS:
        return ({{.CName}}){ .tag = {{.EosTag}} };
    default:
        return {{.Failure}};
    }
}
{{end}}{{define "terminal_attached_adapter"}}
static inline {{.CName}} hex_terminal_is_attached_{{.Suffix}}(hex_io stream, size_t line, size_t column) {
    hex_terminal_attached_result attached = hex_terminal_is_attached(stream);
    if (attached.status == HEX_TERMINAL_OK) {
        return ({{.CName}}){ .tag = {{.Tag}}, .payload.{{.Field}} = attached.attached };
    }
    return {{.Failure}};
}
{{end}}{{define "terminal_size_adapter"}}
static inline {{.CName}} hex_terminal_size_{{.Suffix}}(hex_io stream, size_t line, size_t column) {
    hex_terminal_size_result queried = hex_terminal_size(stream);
    switch (queried.status) {
    case HEX_TERMINAL_OK: {
        hex_t_TerminalSize value = { .hex_m_columns = queried.columns, .hex_m_rows = queried.rows };
        return ({{.CName}}){ .tag = {{.Tag}}, .payload.{{.Field}} = value };
    }
    case HEX_TERMINAL_NOT_A_TERMINAL:
        return {{.NotATerminal}};
    case HEX_TERMINAL_INVALID_DIMENSIONS:
        return {{.InvalidDimensions}};
    default:
        return {{.Failure}};
    }
}
{{end}}{{define "wall_time_adapter"}}
static inline {{.CName}} hex_wall_time_now_{{.Suffix}}(size_t line, size_t column) {
    hex_wall_time now;
    if (hex_wall_time_now(&now)) {
        return ({{.CName}}){ .tag = {{.WallTag}}, .payload.{{.WallField}} = now };
    }
    return ({{.CName}}){ .tag = {{.ErrorTag}}, .payload.{{.ErrorField}} = (hex_t_Error){ .hex_m_file = &{{.File}}, .hex_m_line = line, .hex_m_column = column, .hex_m_kind = (hex_t_ErrorKind){ .tag = {{.ErrorKind}} }, .hex_m_message = hex_error_message(hex_text_heap(&{{.Message}})) } };
}
{{end}}{{define "collection_banner"}}
/* Module-owned collection specializations. */
{{end}}