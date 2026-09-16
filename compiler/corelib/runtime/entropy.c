#include "hexal/entropy.h"
#include "hexal/handle.h"

#include <uv.h>

// The pinned libuv implementation rejects a request larger than this many
// bytes with UV_E2BIG (src/random.c), although its public size_t signature
// does not state that limit. This chunk size is a Hexal runtime rule, not a
// source-visible one: a larger fill is sequential requests of at most this
// many bytes each.
#define HEX_ENTROPY_MAX_CHUNK ((size_t)0x7fffffff)

static const uint8_t hex_entropy_msg_bytes[] = "secure random fill failed";
static const hex_string hex_entropy_msg = {
    .data = hex_entropy_msg_bytes,
    .byte_length = sizeof(hex_entropy_msg_bytes) - 1,
    .rune_length = sizeof(hex_entropy_msg_bytes) - 1,
    .storage_kind = HEX_STRING_STATIC,
};

// hex_entropy_fill runs the synchronous uv_random form directly. It never
// retains data after returning and performs no allocation of its own.
hex_entropy_fill_result hex_entropy_fill(uint8_t *data, size_t length) {
    if (length == 0) {
        return (hex_entropy_fill_result){.ok = true};
    }
    size_t offset = 0;
    while (offset < length) {
        size_t remaining = length - offset;
        size_t chunk = remaining > HEX_ENTROPY_MAX_CHUNK ? HEX_ENTROPY_MAX_CHUNK : remaining;
        uv_random_t request;
        int status = uv_random(nullptr, &request, data + offset, chunk, 0, nullptr);
        if (status != 0) {
            hex_t_ErrorKind kind;
            if (!hex_handle_error_kind(status, &kind)) {
                kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Other};
            }
            return (hex_entropy_fill_result){.ok = false, .kind = kind, .message = &hex_entropy_msg};
        }
        offset += chunk;
    }
    return (hex_entropy_fill_result){.ok = true};
}

{{if .Event}}
#include "hexal/event.h"

// hex_entropy_fill_job carries one chunked fill request across the Task
// park/resume boundary; it lives on the parked Task's own stack frame.
typedef struct hex_entropy_fill_job {
    uint8_t *data;
    size_t length;
    hex_entropy_fill_result result;
} hex_entropy_fill_job;

static void hex_entropy_fill_entry(void *raw) {
    hex_entropy_fill_job *job = (hex_entropy_fill_job *)raw;
    job->result = hex_entropy_fill(job->data, job->length);
}

static void hex_entropy_fill_failure(void *raw) {
    hex_entropy_fill_job *job = (hex_entropy_fill_job *)raw;
    job->result = (hex_entropy_fill_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Other}, .message = &hex_entropy_msg};
}

// hex_entropy_fill_task parks the current Task while the fill (already
// chunked internally) runs on the shared libuv thread pool.
hex_entropy_fill_result hex_entropy_fill_task(uint8_t *data, size_t length) {
    hex_entropy_fill_job job = {.data = data, .length = length};
    hex_event_work_call(hex_entropy_fill_entry, hex_entropy_fill_failure, &job);
    return job.result;
}
{{end}}
