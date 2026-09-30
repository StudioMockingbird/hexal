#define PCRE2_CODE_UNIT_WIDTH 8
#include "regex.h"
#include "hexal/list.h"

{{if .Adapter}}
// The only generated translation unit that includes <pcre2.h>. Every PCRE2
// allocation runs on the caller's Heap through a general context this unit
// creates and releases per operation; no C allocator participates and no
// PCRE2 pointer, option flag, error code, or English text crosses the
// boundary.

#define PCRE2_STATIC
#include <pcre2.h>
#include <string.h>
#include <stdio.h>

static void *hex_regex_heap_malloc(size_t size, void *context) {
    (void)context;
    return hex_heap_allocate_or_null(size);
}

static void hex_regex_heap_free(void *pointer, void *context) {
    (void)context;
    if (pointer != NULL) {
        hex_heap_free(pointer);
    }
}

{{if or .AnyCompile .AnyMatch}}
// The table-owned constant messages. The three compile-limit messages and
// the syntax message are composed at run time from the rendered
// configuration values and PCRE2's byte offset, so the configuration stays
// their single numeric owner.
static const char hex_regex_oom_message[] = "regular expression machinery ran out of memory";

static inline hex_t_ErrorKind hex_regex_kind_invalid_input(void) {
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_InvalidInput };
}

static inline hex_t_ErrorKind hex_regex_kind_resource(void) {
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_ResourceExhausted };
}

// The Unknown Error row: Other carries the fixed classification header, so
// the wrapper prints the kind itself when no message fits.
static inline hex_t_ErrorKind hex_regex_kind_unknown(void) {
    static const char text[] = "Unknown Error";
    hex_string_128 header = { .byte_length = sizeof(text) - 1 };
    memcpy(header.data, text, sizeof(text) - 1);
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_Other, .other_header = header };
}

static inline const hex_string *hex_regex_message(hex_heap h, const char *text) {
    return hex_string_make(h,
        (hex_text){ .data = (const uint8_t *)text, .length = strlen(text) });
}
{{end}}

{{if .AnyCompile}}
{{if .NeedCompile}}
hex_regex_pattern_result hex_regex_compile_raw(hex_heap h, const hex_string *source) {
    pcre2_general_context *general = pcre2_general_context_create(hex_regex_heap_malloc, hex_regex_heap_free, NULL);
    if (general == NULL) {
        return (hex_regex_pattern_result){ .ok = false,
            .kind = hex_regex_kind_resource(),
            .message = hex_regex_message(h, hex_regex_oom_message) };
    }
    pcre2_compile_context *compile_context = pcre2_compile_context_create(general);
    if (compile_context == NULL) {
        pcre2_general_context_free(general);
        return (hex_regex_pattern_result){ .ok = false,
            .kind = hex_regex_kind_resource(),
            .message = hex_regex_message(h, hex_regex_oom_message) };
    }
    // Both setter calls render the same compiler-owned values every
    // operation respects; a pattern's own LIMIT_* directives may lower but
    // never raise them.
    (void)pcre2_set_max_pattern_length(compile_context, {{.MaxPatternBytes}});
    (void)pcre2_set_max_pattern_compiled_length(compile_context, {{.MaxCompiledPatternBytes}});
    (void)pcre2_set_parens_nest_limit(compile_context, {{.MaxParenthesisDepth}});
    int error_code = 0;
    PCRE2_SIZE error_offset = 0;
    pcre2_code *code = pcre2_compile((PCRE2_SPTR)source->data, (PCRE2_SIZE)source->byte_length,
        PCRE2_UTF | PCRE2_UCP, &error_code, &error_offset, compile_context);
    pcre2_compile_context_free(compile_context);
    pcre2_general_context_free(general);
    if (code != NULL) {
        return (hex_regex_pattern_result){ .ok = true, .pattern = { .code = code } };
    }
    // Native codes and dependency English never cross: the three limit
    // failures keep their stable Hexal message, and every other failure is
    // the syntax row keyed on PCRE2's byte offset.
    hex_regex_pattern_result failure = { .ok = false };
    char buffer[160];
    if (error_code == PCRE2_ERROR_NOMEMORY) {
        failure.kind = hex_regex_kind_resource();
        failure.message = hex_regex_message(h, hex_regex_oom_message);
    } else if (error_code == PCRE2_ERROR_PATTERN_STRING_TOO_LONG) {
        snprintf(buffer, sizeof(buffer), "regular expression source exceeds %d bytes", {{.MaxPatternBytes}});
        failure.kind = hex_regex_kind_resource();
        failure.message = hex_regex_message(h, buffer);
    } else if (error_code == PCRE2_ERROR_PATTERN_COMPILED_SIZE_TOO_BIG ||
               error_code == PCRE2_ERROR_PATTERN_TOO_LARGE) {
        snprintf(buffer, sizeof(buffer), "regular expression compiled pattern exceeds %d bytes", {{.MaxCompiledPatternBytes}});
        failure.kind = hex_regex_kind_resource();
        failure.message = hex_regex_message(h, buffer);
    } else if (error_code == PCRE2_ERROR_PARENTHESES_NEST_TOO_DEEP) {
        snprintf(buffer, sizeof(buffer), "regular expression nests beyond %d parentheses", {{.MaxParenthesisDepth}});
        failure.kind = hex_regex_kind_resource();
        failure.message = hex_regex_message(h, buffer);
    } else {
        snprintf(buffer, sizeof(buffer), "invalid regular expression at byte %zu", (size_t)error_offset);
        failure.kind = hex_regex_kind_invalid_input();
        failure.message = hex_regex_message(h, buffer);
    }
    return failure;
}
{{end}}
{{end}}

{{if .AnyMatch}}
// One match attempt: fresh general context, match context, and match data
// for this call, all released before return on every path, so a Pattern's
// read-only use is safe to interleave. The engine's stable outcomes are the
// pair count (rc >= 0), no match (rc == -1), and the three resource
// ceilings; any other negative code is an impossible state.
typedef struct hex_regex_match_outcome {
    int rc;
    hex_t_Span whole;
    hex_list_regex_capture *captures;
    hex_t_ErrorKind kind;
    const char *message;
} hex_regex_match_outcome;

static const char hex_regex_match_limit_message[] = "regular expression match limit exceeded";
static const char hex_regex_depth_limit_message[] = "regular expression nesting limit exceeded";
static const char hex_regex_heap_limit_message[] = "regular expression heap limit exceeded";
static const char hex_regex_impossible_message[] = "regex matcher reported an impossible state";

static void hex_regex_release_match(pcre2_match_data *match_data, pcre2_match_context *match_context, pcre2_general_context *general) {
    if (match_data != NULL) {
        pcre2_match_data_free(match_data);
    }
    if (match_context != NULL) {
        pcre2_match_context_free(match_context);
    }
    if (general != NULL) {
        pcre2_general_context_free(general);
    }
}

static bool hex_regex_run_match(hex_heap h, hex_regex_pattern pattern, const hex_string *subject,
    bool want_captures, hex_regex_match_outcome *out) {
    out->rc = -1;
    out->whole = (hex_t_Span){ .hex_m_start = 0, .hex_m_end = 0 };
    out->captures = NULL;
    pcre2_general_context *general = pcre2_general_context_create(hex_regex_heap_malloc, hex_regex_heap_free, NULL);
    if (general == NULL) {
        out->kind = hex_regex_kind_resource();
        out->message = hex_regex_oom_message;
        return false;
    }
    pcre2_match_context *match_context = pcre2_match_context_create(general);
    if (match_context == NULL) {
        hex_regex_release_match(NULL, NULL, general);
        out->kind = hex_regex_kind_resource();
        out->message = hex_regex_oom_message;
        return false;
    }
    (void)pcre2_set_match_limit(match_context, {{.MatchLimit}});
    (void)pcre2_set_depth_limit(match_context, {{.MatchDepthLimit}});
    (void)pcre2_set_heap_limit(match_context, {{.MatchHeapLimitKiB}});
    pcre2_match_data *match_data = pcre2_match_data_create_from_pattern((const pcre2_code *)pattern.code, general);
    if (match_data == NULL) {
        hex_regex_release_match(NULL, match_context, general);
        out->kind = hex_regex_kind_resource();
        out->message = hex_regex_oom_message;
        return false;
    }
    int rc = pcre2_match((const pcre2_code *)pattern.code, (PCRE2_SPTR)subject->data,
        (PCRE2_SIZE)subject->byte_length, 0, 0, match_data, match_context);
    if (rc == PCRE2_ERROR_NOMATCH) {
        hex_regex_release_match(match_data, match_context, general);
        out->rc = -1;
        return true;
    }
    if (rc < 0) {
        hex_regex_release_match(match_data, match_context, general);
        if (rc == PCRE2_ERROR_MATCHLIMIT) {
            out->kind = hex_regex_kind_resource();
            out->message = hex_regex_match_limit_message;
        } else if (rc == PCRE2_ERROR_DEPTHLIMIT) {
            out->kind = hex_regex_kind_resource();
            out->message = hex_regex_depth_limit_message;
        } else if (rc == PCRE2_ERROR_HEAPLIMIT) {
            out->kind = hex_regex_kind_resource();
            out->message = hex_regex_heap_limit_message;
        } else if (rc == PCRE2_ERROR_NOMEMORY) {
            out->kind = hex_regex_kind_resource();
            out->message = hex_regex_oom_message;
        } else {
            out->kind = hex_regex_kind_unknown();
            out->message = hex_regex_impossible_message;
        }
        return false;
    }
    PCRE2_SIZE *ovector = pcre2_get_ovector_pointer(match_data);
    out->rc = rc;
    out->whole = (hex_t_Span){ .hex_m_start = (size_t)ovector[0], .hex_m_end = (size_t)ovector[1] };
    if (want_captures) {
        out->captures = hex_list_new_regex_capture(h);
        uint32_t capture_count = 0;
        (void)pcre2_pattern_info((const pcre2_code *)pattern.code, PCRE2_INFO_CAPTURECOUNT, &capture_count);
        for (uint32_t group = 1; group <= capture_count; group++) {
            PCRE2_SIZE start = ovector[2 * group];
            PCRE2_SIZE end = ovector[2 * group + 1];
            hex_t_Span_Nil entry = (hex_t_Span_Nil){ .tag = hex_tag_Nil };
            if (start != PCRE2_UNSET && end != PCRE2_UNSET) {
                entry.tag = hex_tag_Span;
                entry.payload.hex_m_Span = (hex_t_Span){ .hex_m_start = (size_t)start, .hex_m_end = (size_t)end };
            }
            hex_list_push_regex_capture(out->captures, entry);
        }
    }
    hex_regex_release_match(match_data, match_context, general);
    return true;
}
{{end}}

{{if .NeedTest}}
hex_regex_bool_result hex_regex_test_raw(hex_heap h, hex_regex_pattern pattern, const hex_string *subject) {
    hex_regex_match_outcome outcome;
    if (!hex_regex_run_match(h, pattern, subject, false, &outcome)) {
        return (hex_regex_bool_result){ .ok = false,
            .kind = outcome.kind,
            .message = hex_regex_message(h, outcome.message) };
    }
    return (hex_regex_bool_result){ .ok = true, .matched = outcome.rc >= 0 };
}
{{end}}

{{if .NeedFind}}
hex_regex_span_result hex_regex_find_raw(hex_heap h, hex_regex_pattern pattern, const hex_string *subject) {
    hex_regex_match_outcome outcome;
    if (!hex_regex_run_match(h, pattern, subject, false, &outcome)) {
        return (hex_regex_span_result){ .ok = false,
            .kind = outcome.kind,
            .message = hex_regex_message(h, outcome.message) };
    }
    if (outcome.rc < 0) {
        return (hex_regex_span_result){ .ok = true, .not_found = true };
    }
    return (hex_regex_span_result){ .ok = true, .span = outcome.whole };
}
{{end}}

{{if .NeedCapture}}
hex_regex_match_result hex_regex_capture_raw(hex_heap h, hex_regex_pattern pattern, const hex_string *subject) {
    hex_regex_match_outcome outcome;
    if (!hex_regex_run_match(h, pattern, subject, true, &outcome)) {
        return (hex_regex_match_result){ .ok = false,
            .kind = outcome.kind,
            .message = hex_regex_message(h, outcome.message) };
    }
    if (outcome.rc < 0) {
        return (hex_regex_match_result){ .ok = true, .not_found = true };
    }
    return (hex_regex_match_result){ .ok = true,
        .match = { .hex_m_whole = outcome.whole, .hex_m_captures = outcome.captures } };
}
{{end}}

{{if .NeedFree}}
void hex_regex_free_raw(hex_heap h, hex_regex_pattern pattern) {
    (void)h;
    // pcre2_code_free carries the pattern's own allocator metadata back to
    // the Heap slots that allocated it, and accepts a null code.
    if (pattern.code != NULL) {
        pcre2_code_free((pcre2_code *)pattern.code);
    }
}
{{end}}
{{end}}
void hex_regex_free_match_raw(hex_heap h, hex_t_Match match) {
    if (match.hex_m_captures != NULL) {
        hex_list_free_regex_capture(h, match.hex_m_captures);
    }
}
