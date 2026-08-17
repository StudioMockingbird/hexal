#ifndef HEXAL_VIEW_H
#define HEXAL_VIEW_H

#include "hexal.h"
{{range .Views}}
typedef struct {{.CName}} {
    const {{.ElementSpelling}} *data;
    size_t length;
} {{.CName}};
static inline const {{.ElementSpelling}} *hex_view_at_{{.Suffix}}({{.CName}} view, size_t index) {
    if (index >= view.length) {
        hex_runtime_trap("[Runtime Error] view index out of bounds\n");
    }
    return &view.data[index];
}
static inline {{.CName}} hex_view_slice_{{.Suffix}}({{.CName}} view, uint64_t start, uint64_t end) {
    if (!(start <= end && end <= view.length)) {
        hex_runtime_trap("[Runtime Error] view slice bounds out of range\n");
    }
    return ({{.CName}}){&view.data[start], end - start};
}
{{end}}
#endif
