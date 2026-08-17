#ifndef HEXAL_WRAP_H
#define HEXAL_WRAP_H
#include "hexal.h"

/* Signed wrapping +, -, *, and unary - lower through ckd_* helpers. Hexal's
 * contract is modulo-width wrapping with defined two's-complement results;
 * the return value of each ckd_* call is intentionally discarded: wrapping
 * is the defined result, and this is the only generated call that discards
 * a checked-arithmetic flag. */
{{range .Helpers}}{{if eq .Name "neg"}}
static inline {{.CName}} {{.Helper}}({{.CName}} a) {
    {{.CName}} r;
    ckd_sub(&r, 0, a);
    return r;
}
{{else if eq .Name "add"}}
static inline {{.CName}} {{.Helper}}({{.CName}} a, {{.CName}} b) {
    {{.CName}} r;
    ckd_add(&r, a, b);
    return r;
}
{{else if eq .Name "sub"}}
static inline {{.CName}} {{.Helper}}({{.CName}} a, {{.CName}} b) {
    {{.CName}} r;
    ckd_sub(&r, a, b);
    return r;
}
{{else}}
static inline {{.CName}} {{.Helper}}({{.CName}} a, {{.CName}} b) {
    {{.CName}} r;
    ckd_mul(&r, a, b);
    return r;
}
{{end}}{{end}}
#endif
