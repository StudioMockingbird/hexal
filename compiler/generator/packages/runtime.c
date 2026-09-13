/* Runtime support: the one program-wide diagnostic trap{{if .Native}} and the
   native dependency bootstrap{{end}}. */
#include "hexal.h"
{{- if .Native}}
#include <mimalloc.h>
#include <uv.h>
{{- end}}

[[noreturn]] void hex_runtime_trap(const char *message) {
    fputs(message, stderr);
    abort();
}
{{- if .Native}}

// Libuv and Hexal share one allocator so memory ownership stays paired across
// the runtime boundary. Root main calls this once, before any module statement
// and before any other libuv operation, and the callbacks never change while
// libuv owns allocations.
void hex_runtime_native_init(void) {
    if (uv_replace_allocator(mi_malloc, mi_realloc, mi_calloc, mi_free) != 0) {
        hex_runtime_trap("[Runtime Error] libuv allocator installation failed\n");
    }
}
{{- end}}
