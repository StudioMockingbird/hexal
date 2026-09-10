{{- define "slicebody" -}}
{{range .Slices}}
typedef struct {{.CName}} {
    {{if .Writable}}{{else}}const {{end}}{{.ElementSpelling}} *data;
    size_t length;
} {{.CName}};
static inline {{if .Writable}}{{else}}const {{end}}{{.ElementSpelling}} *{{.HelperPrefix}}at_{{.Suffix}}({{.CName}} slice, size_t index) {
    if (index >= slice.length) {
        hex_runtime_trap("[Runtime Error] slice index out of bounds\n");
    }
    return &slice.data[index];
}
static inline {{.CName}} {{.HelperPrefix}}slice_{{.Suffix}}({{.CName}} slice, uint64_t start, uint64_t end) {
    if (!(start <= end && end <= slice.length)) {
        hex_runtime_trap("[Runtime Error] slice slice bounds out of range\n");
    }
    return ({{.CName}}){slice.data == nullptr ? nullptr : &slice.data[start], end - start};
}
{{end}}
{{- end -}}
#ifndef HEXAL_SLICE_H
#define HEXAL_SLICE_H

#include "hexal.h"
{{if .NeedsHeapString}}typedef struct hex_string hex_string;
{{end}}{{template "slicebody" .}}
#endif
