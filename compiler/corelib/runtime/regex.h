#ifndef HEXAL_REGEX_H
#define HEXAL_REGEX_H

#include "hexal.h"
{{if .Adapter}}
#include "hexal/error.h"
{{end}}
#include "hexal/string.h"
// std/regex's raw declarations. The Pattern handle, the half-open byte Span,
// the Span | Nil capture member, and the Match record over its own capture
// List are Hexal-owned and live here for every unit that spells them,
// including the component list header; native engine types stay private to
// hexal/regex.c, the only unit that includes the engine header.

typedef struct hex_regex_pattern {
    void *code;
} hex_regex_pattern;

// The capture List's struct body and typed operations ship in the shared
// hexal/list.h; hex_t_Match names the type only behind a pointer, so the
// incomplete typedef is sufficient here.
typedef struct hex_list_regex_capture hex_list_regex_capture;

typedef struct hex_t_Span {
    size_t hex_m_start;
    size_t hex_m_end;
} hex_t_Span;

// The optional capture uses the same flat payload layout as a generated union.
typedef struct hex_t_Span_Nil {
    hex_tag tag;
    union {
        hex_t_Span hex_m_Span;
    } payload;
} hex_t_Span_Nil;

// hex_t_Match owns its capture List; the raw form equals a module-owned
// Match struct's own data.
typedef struct hex_t_Match {
    hex_t_Span hex_m_whole;
    hex_list_regex_capture *hex_m_captures;
} hex_t_Match;

{{if .Adapter}}
typedef struct hex_regex_pattern_result {
    bool ok;
    hex_regex_pattern pattern;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_regex_pattern_result;

typedef struct hex_regex_bool_result {
    bool ok;
    bool matched;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_regex_bool_result;

typedef struct hex_regex_span_result {
    bool ok;
    bool not_found;
    hex_t_Span span;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_regex_span_result;

typedef struct hex_regex_match_result {
    bool ok;
    bool not_found;
    hex_t_Match match;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_regex_match_result;

// The raw entry points. Every engine allocation runs on the caller's Heap
// through one general context the operation creates and releases; no C
// allocator participates and no engine pointer ever escapes as a Hexal value.
extern hex_regex_pattern_result hex_regex_compile_raw(hex_heap h, const hex_string *source);
extern hex_regex_bool_result hex_regex_test_raw(hex_heap h, hex_regex_pattern pattern, const hex_string *subject);
extern hex_regex_span_result hex_regex_find_raw(hex_heap h, hex_regex_pattern pattern, const hex_string *subject);
extern hex_regex_match_result hex_regex_capture_raw(hex_heap h, hex_regex_pattern pattern, const hex_string *subject);
extern void hex_regex_free_raw(hex_heap h, hex_regex_pattern pattern);
{{end}}
// free_match releases only Hexal's capture List, so it exists for every
// program that can hold a Match, adapter or not.
extern void hex_regex_free_match_raw(hex_heap h, hex_t_Match match);

#endif
