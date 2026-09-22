{{define "spawn_adapter_void"}}
void hex_task_entry_{{.Key}}(hex_task *task) {
{{if .HasFrame}}    hex_task_args_{{.Key}} *args = (hex_task_args_{{.Key}} *)task->args;
{{end}}    {{.Function}}({{.Arguments}});
    hex_task_complete(task);
}
{{end}}{{define "spawn_adapter_value"}}
void hex_task_entry_{{.Key}}(hex_task *task) {
{{if .HasFrame}}    hex_task_args_{{.Key}} *args = (hex_task_args_{{.Key}} *)task->args;
{{end}}    {{.ResultSpelling}} result = {{.Function}}({{.Arguments}});
    *({{.ResultSpelling}} *)task->result = result;
    hex_task_complete(task);
}
{{end}}{{define "spawn_frame_decl"}}{{.Indent}}{{.ArgsType}} {{.Temp}};
{{end}}{{define "spawn_frame_fill"}}{{.Indent}}{{.Temp}}.a{{.Index}} = {{.Value}};
{{end}}{{define "spawn_call_frame"}}{{.Indent}}hex_task *{{.TaskTemp}} = hex_task_spawn(hex_task_entry_{{.Key}}, sizeof({{.ArgsType}}), _Alignof({{.ArgsType}}), &{{.Temp}}, {{.ResultArgs}});
{{end}}{{define "spawn_call_bare"}}{{.Indent}}hex_task *{{.TaskTemp}} = hex_task_spawn(hex_task_entry_{{.Key}}, 0, 0, nullptr, {{.ResultArgs}});
{{end}}{{define "module_include"}}#include "{{.Stem}}.h"

{{end}}{{define "root_uv_setup_decl"}}extern char **uv_setup_args(int argc, char **argv);

{{end}}{{define "root_entry_void"}}int main(void) {
{{end}}{{define "root_entry_args"}}int main(int argc, char **argv) {
{{end}}{{define "root_entry_dual"}}#if defined(_WIN32)
int main(void) {
#else
int main(int argc, char **argv) {
#endif
{{end}}{{define "root_arg_windows"}}    {{.Windows}}
{{end}}{{define "root_arg_posix"}}    {{.POSIX}}
{{end}}{{define "root_arg_dual"}}#if defined(_WIN32)
    {{.Windows}}
#else
    {{.POSIX}}
#endif
{{end}}{{define "root_native_init"}}    hex_runtime_native_init();
{{end}}{{define "root_exit_status"}}    uint8_t hex_exit_status = 0;
{{end}}{{define "root_handle_init"}}    hex_handle_registry_init();
{{end}}{{define "root_scheduler_init"}}    hex_scheduler_init();
{{end}}{{define "root_env_decl"}}    {{.Name}} env;
{{end}}{{define "root_exit_label"}}hex_exit:
{{end}}{{define "root_task_complete"}}    hex_task_complete(hex_root_task);
{{end}}{{define "root_return_status"}}    return (int)hex_exit_status;
}
{{end}}{{define "root_return_ok"}}    return 0;
}
{{end}}
{{define "print_statement_temp"}}{{.Indent}}{{.Decl}} = {{.Value}};
{{end}}{{define "print_txn_begin"}}{{.Indent}}hex_print_buffer {{.Buffer}};
{{.Indent}}hex_print_begin(&{{.Buffer}});
{{end}}{{define "print_txn_end"}}{{.Indent}}hex_print_commit(&{{.Buffer}});
{{.Indent}}hex_print_destroy(&{{.Buffer}});
{{end}}{{define "print_arg_bool"}}{{.Indent}}hex_print_bool({{.Buffer}}, {{.Name}});
{{end}}{{define "print_arg_nil"}}{{.Indent}}hex_print_nil({{.Buffer}});
{{end}}{{define "print_arg_size"}}{{.Indent}}hex_print_size({{.Buffer}}, {{.Name}});
{{end}}{{define "print_arg_int"}}{{.Indent}}hex_print_int{{.Bits}}({{.Buffer}}, {{.Name}});
{{end}}{{define "print_arg_uint"}}{{.Indent}}hex_print_uint{{.Bits}}({{.Buffer}}, {{.Name}});
{{end}}{{define "print_arg_float32"}}{{.Indent}}hex_print_float32({{.Buffer}}, {{.Name}});
{{end}}{{define "print_arg_float64"}}{{.Indent}}hex_print_float64({{.Buffer}}, {{.Name}});
{{end}}{{define "print_arg_string"}}{{.Indent}}hex_print_text({{.Buffer}}, {{.Name}}->data, {{.Name}}->byte_length);
{{end}}{{define "print_arg_inline_string"}}{{.Indent}}hex_print_text({{.Buffer}}, {{.Name}}.data, {{.Name}}.byte_length);
{{end}}{{define "print_arg_error"}}{{.Indent}}hex_print_error_direct({{.Buffer}}, &{{.Name}});
{{end}}{{define "print_arg_nested"}}{{.Indent}}hex_print_nested_{{.CName}}({{.Buffer}}, {{.Arg}});
{{end}}{{define "match_assign"}}{{.Indent}}{{.Target}}{{if .Value}} = {{.Value}}{{end}};
{{end}}{{define "match_open"}}{{.Indent}}{{.Prefix}}({{.Condition}}) {
{{end}}{{define "match_else"}}{{.Indent}}else {
{{end}}{{define "block_close"}}{{.Indent}}}
{{end}}{{define "adt_construct"}}({{.CName}}){ .tag = {{.Tag}}{{if .OtherHeader}}, .other_header = {{.OtherHeader}}{{end}}{{if .PayloadOpen}}{{.PayloadOpen}}{{range .Fields}}{{.}}{{end}} }{{end}} }{{end}}{{define "eq_tag_mismatch"}}{{.Indent}}if ({{.Left}}.tag != {{.Right}}.tag) return false;
{{end}}{{define "eq_switch_open"}}{{.Indent}}switch ({{.Left}}.tag) {
{{end}}{{define "eq_case"}}{{.Indent}}case {{.Tag}}:
{{end}}{{define "eq_return_true"}}{{.Indent}}    return true;
{{end}}{{define "eq_default_abort"}}{{.Indent}}default:
{{.Indent}}    abort();
{{end}}{{define "eq_other_header"}}{{.Indent}}if ({{.Left}}.tag == {{.OtherTag}} && !hex_equal_text(hex_text_inline(&{{.Left}}.other_header), hex_text_inline(&{{.Right}}.other_header))) return false;
{{end}}{{define "eq_length_mismatch"}}{{.Indent}}if ({{.Left}}.length != {{.Right}}.length) return false;
{{end}}{{define "eq_for_open"}}{{.Indent}}for (size_t index = 0; index < {{.Left}}.length; index++) {
{{end}}{{define "eq_text_heap"}}{{.Indent}}if (!hex_equal_text(hex_text_heap({{.Left}}), hex_text_heap({{.Right}}))) return false;
{{end}}{{define "eq_text_inline"}}{{.Indent}}if (!hex_equal_text(hex_text_inline(&({{.Left}})), hex_text_inline(&({{.Right}})))) return false;
{{end}}{{define "eq_scalar"}}{{.Indent}}if (!({{.Left}} == {{.Right}})) return false;
{{end}}{{define "const_decl"}}{{.Indent}}const {{.Type}} {{.Name}} = {{.Value}};
{{end}}{{define "const_ptr_decl"}}{{.Indent}}const {{.Type}} *const {{.Name}} = {{.Value}};
{{end}}{{define "const_addr_decl"}}{{.Indent}}const {{.Type}} *const {{.Name}} = &({{.Value}});
{{end}}{{define "version_shadow"}}{{.Indent}}const size_t {{.Name}}_version = {{.Name}}->version;
{{end}}{{define "version_guard_open"}}{{.Indent}}    if ({{.Name}}->version != {{.Name}}_version) {
{{end}}{{define "runtime_trap"}}{{.Indent}}        hex_runtime_trap("[Runtime Error] collection modified during iteration\n");
{{end}}{{define "inner_close"}}{{.Indent}}    }
{{end}}{{define "ordinal_decl"}}{{.Indent}}size_t {{.Name}} = {{.Value}};
{{end}}{{define "ordinal_preinc"}}{{.Indent}}    {{.Name}}++;
{{end}}{{define "for_assign"}}{{.Indent}}    {{.Name}} = {{.Value}};
{{end}}{{define "grapheme_cursor_init"}}{{.Indent}}hex_grapheme_cursor {{.Name}} = hex_text_grapheme_cursor({{.Value}});
{{end}}{{define "grapheme_while_open"}}{{.Indent}}while (hex_grapheme_cursor_has_next({{.Name}})) {
{{end}}{{define "grapheme_next_decl"}}{{.Indent}}    const hex_grapheme {{.Name}} = hex_grapheme_cursor_next(&{{.Value}});
{{end}}{{define "bucket_active_open"}}{{.Indent}}    if (!{{.Dict}}->buckets[{{.Var}}].active) {
{{end}}{{define "continue_stmt"}}{{.Indent}}continue;
{{end}}{{define "for_index_open"}}{{.Indent}}for (size_t {{.Var}} = 0; {{.Var}} < {{.Limit}}; {{.Var}}++) {
{{end}}{{define "for_width_open"}}{{.Indent}}for (size_t {{.Var}} = 0; {{.Var}} < {{.Limit}}; {{.Var}} += {{.Width}}) {
{{end}}{{define "bucket_bind"}}{{.Indent}}    {{.Target}} = {{.Dict}}->buckets[{{.Var}}].{{.Field}};
{{end}}{{define "text_index_assign"}}{{.Indent}}    {{.Target}} = {{.Data}}[{{.Index}}];
{{end}}{{define "utf8_decode_assign"}}{{.Indent}}    {{.Target}} = hex_utf8_decode_step({{.Data}}, {{.Length}}, {{.Offset}}, &{{.Width}});
{{end}}{{define "raw_text"}}{{.Text}}{{end}}{{define "bool_decl"}}{{.Indent}}bool {{.Name}} = false;
{{end}}{{define "ckd_add_open"}}{{.Indent}}if (ckd_add(&{{.Name}}, {{.Name}}, {{.Extra}})) {
{{end}}{{define "assign_true"}}{{.Indent}}    {{.Name}} = true;
{{end}}{{define "var_decl"}}{{.Indent}}{{.Type}} {{.Name}};
{{end}}{{define "capacity_guard_open"}}{{.Indent}}if ({{.Over}} || {{.Total}} > {{.Capacity}}) {
{{end}}{{define "else_open"}}{{.Indent}}} else {
{{end}}{{define "value_init"}}{{.Indent}}    {{.Type}} {{.Name}} = { .byte_length = {{.Value}} };
{{end}}{{define "nonzero_guard_open"}}{{.Indent}}if ({{.Value}} != 0) {
{{end}}{{define "memcpy_inline"}}{{.Indent}}        memcpy({{.Dest}}.data + {{.Offset}}, {{.Data}}, {{.Len}});
{{end}}{{define "offset_add"}}{{.Indent}}{{.Name}} += {{.Value}};
{{end}}{{define "result_assign"}}{{.Indent}}    {{.Name}} = ({{.Type}}){ .tag = {{.Tag}}, .payload.{{.Field}} = {{.Value}} };
{{end}}{{define "void_use"}}{{.Indent}}(void){{.Name}};
{{end}}{{define "overflow_trap"}}{{.Indent}}    hex_runtime_trap("[Runtime Error] string allocation size overflow\n");
{{end}}{{define "storage_size_open"}}{{.Indent}}if (ckd_add(&{{.Name}}, sizeof(hex_string_storage), {{.Extra}}) || ckd_add(&{{.Name}}, {{.Name}}, 1)) {
{{end}}{{define "storage_alloc"}}{{.Indent}}hex_string_storage *{{.Name}} = hex_heap_allocate({{.Size}});
{{end}}{{define "memcpy_owned"}}{{.Indent}}    memcpy({{.Dest}}->bytes + {{.Offset}}, {{.Data}}, {{.Len}});
{{end}}{{define "nul_terminate"}}{{.Indent}}{{.Name}}->bytes[{{.Total}}] = 0;
{{end}}{{define "header_init"}}{{.Indent}}{{.Name}}->header = (hex_string){ .data = {{.Name}}->bytes, .byte_length = {{.Total}}, .storage_kind = HEX_STRING_OWNED };
{{end}}{{define "result_header"}}{{.Indent}}const hex_string *const {{.Name}} = &{{.Storage}}->header;
{{end}}{{define "bool_text"}}{{.Indent}}const uint8_t *{{.Name}} = {{.Value}} ? (const uint8_t *)"true" : (const uint8_t *)"false";
{{end}}{{define "bool_len"}}{{.Indent}}size_t {{.Name}} = {{.Value}} ? 4 : 5;
{{end}}{{define "buf_decl"}}{{.Indent}}char {{.Name}}[{{.Size}}];
{{end}}{{define "format_len"}}{{.Indent}}size_t {{.Name}} = {{.Function}}({{.Buf}}, {{.Value}});
{{end}}{{define "control_keyword"}}{{.Indent}}{{.Prefix}} (
{{end}}{{define "control_condition"}}{{.Indent}}    {{.Condition}}) {
{{end}}{{define "control_open"}}{{.Indent}}{{.Prefix}} ({{.Condition}}) {
{{end}}{{define "call_stmt"}}{{.Indent}}{{.Call}};
{{end}}{{define "break_stmt"}}{{.Indent}}break;
{{end}}{{define "return_stmt"}}{{.Indent}}return {{.Value}};
{{end}}{{define "error_exit_decl"}}{{.Indent}}const bool {{.Name}} = {{.Value}};
{{end}}{{define "exit_status_zero"}}{{.Indent}}hex_exit_status = 0;
{{end}}{{define "exit_status_value"}}{{.Indent}}hex_exit_status = (uint8_t)({{.Value}});
{{end}}{{define "goto_exit"}}{{.Indent}}goto hex_exit;
{{end}}{{define "line_directive"}}#line {{.Line}} "{{.File}}"
{{end}}{{define "object_literal"}}({{.Type}}){{"{"}}{{range .Fields}}
        .{{.Name}} = {{.Value}},{{end}}
    }{{end}}{{define "tag_test_open"}}{{.Indent}}if ({{.Temp}}.tag == {{.Tag}}) {
{{end}}{{define "payload_return"}}{{.Indent}}    return {{.Temp}}.payload.{{.Field}};
{{end}}{{define "union_return"}}{{.Indent}}    return ({{.Type}}){ .tag = {{.Tag}}, .payload.{{.Field}} = {{.Temp}}.payload.{{.Payload}} };
{{end}}{{define "switch_tag_open"}}{{.Indent}}switch ({{.Temp}}.tag) {
{{end}}{{define "case_tag"}}{{.Indent}}case {{.Tag}}:
{{end}}{{define "tag_only_assign"}}{{.Indent}}    {{.Name}} = ({{.Type}}){ .tag = {{.Tag}} };
{{end}}{{define "payload_assign"}}{{.Indent}}    {{.Name}} = ({{.Type}}){ .tag = {{.Tag}}, .payload.{{.Field}} = {{.Temp}}.payload.{{.Payload}} };
{{end}}{{define "default_abort"}}{{.Indent}}default:
{{.Indent}}    abort();
{{.Indent}}}
{{end}}{{define "inline_literal"}}({{.Type}}){ .byte_length = {{.Length}}, .data = {{"{"}}{{range .Bytes}} {{.}},{{end}} } }{{end}}{{define "errdef_guard_open"}}{{.Indent}}if ({{.Value}}) {
{{end}}{{define "defer_discard"}}{{.Indent}}(void)({{.Value}});
{{end}}{{define "dict_find_decl"}}{{.Indent}}const {{.Type}} *{{.Temp}} = hex_dict_find_{{.Suffix}}({{.Receiver}}, {{.Key}});
{{end}}