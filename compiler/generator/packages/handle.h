#ifndef HEXAL_HANDLE_H
#define HEXAL_HANDLE_H

#include "hexal.h"
#include "hexal/error.h"

// One capability kind per public wrapper type. Resolving a handle against the
// wrong kind fails closed without touching its control storage. This enum
// grows as further libuv-backed capabilities are added; it never shrinks or
// renumbers a shipped member.
typedef enum hex_handle_kind : uint8_t {
    HEX_HANDLE_KIND_FILE,
    HEX_HANDLE_KIND_TCP_CONNECTION,
    HEX_HANDLE_KIND_TCP_LISTENER,
    HEX_HANDLE_KIND_PROCESS,
    HEX_HANDLE_KIND_PIPE,
    HEX_HANDLE_KIND_SIGNALS,
} hex_handle_kind;

// hex_handle_slot is opaque here: its lifecycle state, generation, capability
// kind, control-block pointer, and synchronization live only in
// hexal/handle.c. Slot addresses remain valid until process termination.
typedef struct hex_handle_slot hex_handle_slot;

// hex_handle is the copyable public representation embedded in every
// generation-checked wrapper type. A copy names one external resource and
// observes one shared lifecycle; it carries no libuv type, pointer, request,
// callback, or numeric error code.
typedef struct hex_handle {
    hex_handle_slot *slot;
    uint64_t generation;
} hex_handle;

// hex_handle_lease is the private, per-operation pin a successful resolve
// returns. control is nullptr when the handle is closed, stale, or the wrong
// kind. Every non-null lease must be released exactly once.
typedef struct hex_handle_lease {
    hex_handle_slot *slot;
    void *control;
} hex_handle_lease;

void hex_handle_registry_init(void);

// hex_handle_reserve reserves a free slot for kind and returns it with
// lifecycle "opening": not yet resolvable. A nullptr slot means allocation is
// exhausted; the caller publishes nothing and reports failure.
hex_handle hex_handle_reserve(hex_handle_kind kind);

// hex_handle_publish attaches control, the capability's control block, and
// transitions the reserved slot to "live". Called at most once per reserved
// slot, only after the capability's native resource is fully initialized.
void hex_handle_publish(hex_handle handle, void *control);

// hex_handle_abandon releases a reserved slot whose construction failed
// before publication. It runs no capability close path.
void hex_handle_abandon(hex_handle handle);

// hex_handle_resolve validates kind, generation, and liveness under the
// slot's own mutex, pins the lease, and returns the live control-block
// pointer, or a nullptr control when the handle is closed, stale, or the
// wrong kind. Never touches released control storage.
hex_handle_lease hex_handle_resolve(hex_handle handle, hex_handle_kind kind);

// hex_handle_release releases one lease acquired by hex_handle_resolve or
// hex_handle_close_begin. Idempotent on a nullptr slot only, so a rejected
// resolve or close needs no separate no-op check at the call site.
void hex_handle_release(hex_handle_lease lease);

// hex_handle_close_begin locks the slot and linearizes at live -> closing,
// which prevents every new lease immediately and invalidates every public
// copy at this call. Returns the live control-block pointer to run the
// capability's native close against, or nullptr when the handle was already
// closed, stale, or the wrong kind. The caller must eventually call
// hex_handle_close_finish exactly once for a non-null return.
void *hex_handle_close_begin(hex_handle handle, hex_handle_kind kind);

// hex_handle_close_finish is the terminal owner: it recycles or retires the
// slot once every lease resolved before the closing transition has released
// (a lease already in flight when close_begin ran keeps its pinned control
// valid until it releases; this only marks the slot ready to recycle the
// moment the count reaches zero, immediately if it already has).
void hex_handle_close_finish(hex_handle handle);

// hex_handle_error_kind maps a portable libuv status to a stable ErrorKind
// classification. It returns false, leaving *out untouched, for a code this
// table does not classify; the caller then applies its capability's own
// ErrorKind.Other fallback.
bool hex_handle_error_kind(int status, hex_t_ErrorKind *out);

#endif
