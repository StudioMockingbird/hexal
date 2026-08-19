{{- define "arraybody" -}}
{{range .Arrays}}
typedef struct {{.CName}} {
    {{.ElementSpelling}} data[{{.Length}}];
} {{.CName}};
static inline const {{.ElementSpelling}} *hex_array_at_{{.Suffix}}(const {{.CName}} *array, size_t index) {
    if (index >= UINT64_C({{.Length}})) {
        hex_runtime_trap("[Runtime Error] array index out of bounds\n");
    }
    return &array->data[index];
}
static inline {{.ElementSpelling}} *hex_array_at_mut_{{.Suffix}}({{.CName}} *array, size_t index) {
    if (index >= UINT64_C({{.Length}})) {
        hex_runtime_trap("[Runtime Error] array index out of bounds\n");
    }
    return &array->data[index];
}
{{if .ViewCName}}
static inline {{.ViewCName}} hex_array_slice_{{.Suffix}}(const {{.CName}} *array, uint64_t start, uint64_t end) {
    if (!(start <= end && end <= UINT64_C({{.Length}}))) {
        hex_runtime_trap("[Runtime Error] array slice bounds out of range\n");
    }
    return ({{.ViewCName}}){&array->data[start], end - start};
}
{{end}}{{end}}
{{- end -}}
#ifndef HEXAL_ARRAY_H
#define HEXAL_ARRAY_H

#include "hexal.h"
{{if .NeedsView}}#include "hexal/view.h"
{{end}}{{template "arraybody" .}}
#endif
