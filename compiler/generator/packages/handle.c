/* The shared generation-checked handle registry every long-lived libuv-backed
   capability resolves through. One private mutex per slot guards its own
   lifecycle, generation, capability kind, control-block pointer, and
   in-flight operation count; the registry mutex protects only chunk growth
   and free-slot selection, so ordinary resolve/release/close traffic never
   contends on one program-wide lock. */
#include "hexal/handle.h"
#include "hexal/heap.h"
#include <string.h>
#include <uv.h>

enum {
    HEX_HANDLE_FREE,
    HEX_HANDLE_OPENING,
    HEX_HANDLE_LIVE,
    HEX_HANDLE_CLOSING,
    HEX_HANDLE_RETIRED,
};

struct hex_handle_slot {
    uv_mutex_t mutex;
    uint64_t generation;
    hex_handle_kind kind;
    uint8_t state;
    bool finish_requested;
    uint32_t in_flight;
    void *control;
    hex_handle_slot *free_next;
};

// Slots live inside fixed-size chunks that are never moved or freed once
// allocated; only the chunk directory (this array of chunk pointers) grows,
// so a published slot address stays valid until process termination.
constexpr size_t HEX_HANDLE_CHUNK_SLOTS = 64;

typedef struct hex_handle_chunk {
    hex_handle_slot slots[HEX_HANDLE_CHUNK_SLOTS];
} hex_handle_chunk;

static uv_mutex_t hex_handle_registry_mutex;
static hex_handle_chunk **hex_handle_chunks;
static size_t hex_handle_chunk_count;
static size_t hex_handle_chunk_capacity;
static hex_handle_slot *hex_handle_free_list;

void hex_handle_registry_init(void) {
    if (uv_mutex_init(&hex_handle_registry_mutex) != 0) {
        hex_runtime_trap("[Runtime Error] handle registry initialization failed\n");
    }
}

// hex_handle_grow_and_reserve runs with the registry mutex held: it adds one
// chunk, links every one of its slots onto the free list, and pops the first
// for the caller. Returns nullptr on allocation exhaustion without publishing
// any partial state.
static hex_handle_slot *hex_handle_grow_and_reserve(void) {
    hex_handle_chunk *chunk = (hex_handle_chunk *)hex_heap_allocate_zeroed_or_null(sizeof(hex_handle_chunk));
    if (chunk == nullptr) {
        return nullptr;
    }
    if (hex_handle_chunk_count == hex_handle_chunk_capacity) {
        size_t capacity = hex_handle_chunk_capacity == 0 ? 4 : hex_handle_chunk_capacity * 2;
        hex_handle_chunk **grown = (hex_handle_chunk **)hex_heap_allocate_or_null(capacity * sizeof(hex_handle_chunk *));
        if (grown == nullptr) {
            hex_heap_free(chunk);
            return nullptr;
        }
        if (hex_handle_chunks != nullptr) {
            memcpy(grown, hex_handle_chunks, hex_handle_chunk_count * sizeof(hex_handle_chunk *));
            hex_heap_free(hex_handle_chunks);
        }
        hex_handle_chunks = grown;
        hex_handle_chunk_capacity = capacity;
    }
    hex_handle_chunks[hex_handle_chunk_count++] = chunk;
    for (size_t index = 0; index < HEX_HANDLE_CHUNK_SLOTS; index++) {
        hex_handle_slot *slot = &chunk->slots[index];
        if (uv_mutex_init(&slot->mutex) != 0) {
            hex_runtime_trap("[Runtime Error] handle registry initialization failed\n");
        }
        slot->state = HEX_HANDLE_FREE;
        slot->free_next = hex_handle_free_list;
        hex_handle_free_list = slot;
    }
    hex_handle_slot *reserved = hex_handle_free_list;
    hex_handle_free_list = reserved->free_next;
    return reserved;
}

hex_handle hex_handle_reserve(hex_handle_kind kind) {
    uv_mutex_lock(&hex_handle_registry_mutex);
    hex_handle_slot *slot = hex_handle_free_list;
    if (slot != nullptr) {
        hex_handle_free_list = slot->free_next;
    } else {
        slot = hex_handle_grow_and_reserve();
    }
    uv_mutex_unlock(&hex_handle_registry_mutex);
    if (slot == nullptr) {
        return (hex_handle){0};
    }
    uv_mutex_lock(&slot->mutex);
    slot->kind = kind;
    slot->state = HEX_HANDLE_OPENING;
    slot->finish_requested = false;
    slot->in_flight = 0;
    slot->control = nullptr;
    uint64_t generation = slot->generation;
    uv_mutex_unlock(&slot->mutex);
    return (hex_handle){.slot = slot, .generation = generation};
}

// hex_handle_recycle clears control, retires a slot whose generation would
// wrap instead of reusing it, and otherwise bumps the generation before
// returning the slot to the free list -- reuse always increments the
// generation before the next publish can expose it.
static void hex_handle_recycle(hex_handle_slot *slot) {
    uv_mutex_lock(&slot->mutex);
    void *control = slot->control;
    slot->control = nullptr;
    slot->finish_requested = false;
    bool retire = slot->generation == UINT64_MAX;
    if (!retire) {
        slot->generation++;
    }
    slot->state = retire ? HEX_HANDLE_RETIRED : HEX_HANDLE_FREE;
    uv_mutex_unlock(&slot->mutex);
    hex_heap_free(control);
    if (retire) {
        return;
    }
    uv_mutex_lock(&hex_handle_registry_mutex);
    slot->free_next = hex_handle_free_list;
    hex_handle_free_list = slot;
    uv_mutex_unlock(&hex_handle_registry_mutex);
}

void hex_handle_publish(hex_handle handle, void *control) {
    hex_handle_slot *slot = handle.slot;
    uv_mutex_lock(&slot->mutex);
    slot->control = control;
    slot->state = HEX_HANDLE_LIVE;
    uv_mutex_unlock(&slot->mutex);
}

void hex_handle_abandon(hex_handle handle) {
    hex_handle_recycle(handle.slot);
}

hex_handle_lease hex_handle_resolve(hex_handle handle, hex_handle_kind kind) {
    hex_handle_slot *slot = handle.slot;
    if (slot == nullptr) {
        return (hex_handle_lease){0};
    }
    uv_mutex_lock(&slot->mutex);
    if (slot->state != HEX_HANDLE_LIVE || slot->generation != handle.generation || slot->kind != kind) {
        uv_mutex_unlock(&slot->mutex);
        return (hex_handle_lease){0};
    }
    slot->in_flight++;
    void *control = slot->control;
    uv_mutex_unlock(&slot->mutex);
    return (hex_handle_lease){.slot = slot, .control = control};
}

void hex_handle_release(hex_handle_lease lease) {
    hex_handle_slot *slot = lease.slot;
    if (slot == nullptr) {
        return;
    }
    uv_mutex_lock(&slot->mutex);
    slot->in_flight--;
    bool finish = slot->finish_requested && slot->in_flight == 0;
    uv_mutex_unlock(&slot->mutex);
    if (finish) {
        hex_handle_recycle(slot);
    }
}

void *hex_handle_close_begin(hex_handle handle, hex_handle_kind kind) {
    hex_handle_slot *slot = handle.slot;
    if (slot == nullptr) {
        return nullptr;
    }
    uv_mutex_lock(&slot->mutex);
    if (slot->state != HEX_HANDLE_LIVE || slot->generation != handle.generation || slot->kind != kind) {
        uv_mutex_unlock(&slot->mutex);
        return nullptr;
    }
    slot->state = HEX_HANDLE_CLOSING;
    void *control = slot->control;
    uv_mutex_unlock(&slot->mutex);
    return control;
}

void hex_handle_close_finish(hex_handle handle) {
    hex_handle_slot *slot = handle.slot;
    uv_mutex_lock(&slot->mutex);
    slot->finish_requested = true;
    bool finish = slot->in_flight == 0;
    uv_mutex_unlock(&slot->mutex);
    if (finish) {
        hex_handle_recycle(slot);
    }
}

bool hex_handle_error_kind(int status, hex_t_ErrorKind *out) {
    switch (status) {
    case UV_ENOENT:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_NotFound};
        return true;
    case UV_EACCES:
    case UV_EPERM:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_PermissionDenied};
        return true;
    case UV_EEXIST:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_AlreadyExists};
        return true;
    case UV_EINVAL:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput};
        return true;
    case UV_ENOMEM:
    case UV_ENOBUFS:
    case UV_EMFILE:
    case UV_ENFILE:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted};
        return true;
    case UV_ENOSYS:
    case UV_ENOTSUP:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Unsupported};
        return true;
    case UV_ECANCELED:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Cancelled};
        return true;
    case UV_EINTR:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Interrupted};
        return true;
    case UV_ETIMEDOUT:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_TimedOut};
        return true;
    case UV_EADDRINUSE:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_AddressInUse};
        return true;
    case UV_EADDRNOTAVAIL:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_AddressUnavailable};
        return true;
    case UV_ECONNREFUSED:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ConnectionRefused};
        return true;
    case UV_ECONNRESET:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ConnectionReset};
        return true;
    case UV_ECONNABORTED:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ConnectionAborted};
        return true;
    case UV_EHOSTUNREACH:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_HostUnreachable};
        return true;
    case UV_ENETUNREACH:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_NetworkUnreachable};
        return true;
    case UV_EPIPE:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_BrokenPipe};
        return true;
    case UV_ENOTCONN:
        *out = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_NotConnected};
        return true;
    default:
        return false;
    }
}
