#ifndef HEXAL_SIGNAL_H
#define HEXAL_SIGNAL_H

#include "hexal.h"
#include "hexal/error.h"
#include "hexal/handle.h"

// hex_t_Signal's tag is an ordinary compilation-assigned discriminant
// (hex_tag_Signal_Interrupt and so on, resolved through the program-wide tag
// registry exactly like any other reachable ADT), reachable through ordinary
// variant construction and match narrowing -- unlike hex_t_Address in
// hexal/network.h, nothing here may assume a fixed numeric tag. Every
// module-owned adapter this header's generated inline helpers build
// translates a checked tag to one of the plain HEX_SIGNAL_* enums below
// before calling into hexal/signal.c, and translates a plain result back
// into the correct tag on the way out; the runtime functions declared past
// this point never read or write a hex_tag.
typedef struct hex_t_Signal {
    hex_tag tag;
} hex_t_Signal;

// Signals wraps the shared generation-checked handle; every operation
// resolves it before touching native state.
typedef struct hex_signals {
    hex_handle handle;
} hex_signals;
{{- if .Operations}}

enum {
    HEX_SIGNAL_INVALID_INPUT = 1,
    HEX_SIGNAL_UNSUPPORTED = 2,
    HEX_SIGNAL_BUSY = 3,
    HEX_SIGNAL_CLOSED = 4,
    HEX_SIGNAL_ALLOCATION_FAILED = 5,
    HEX_SIGNAL_EOS = 6,
};

// The three Signal raw values a module-owned adapter translates a checked
// hex_t_Signal tag into before calling hex_signals_new, and translates a
// raw hex_signals_next_result.signal back from.
enum {
    HEX_SIGNAL_INTERRUPT = 0,
    HEX_SIGNAL_HANGUP = 1,
    HEX_SIGNAL_TERMINATE = 2,
};

typedef struct hex_signals_new_result {
    int status;
    hex_signals signals;
} hex_signals_new_result;

// hex_signals_new parks the calling Task. subscriptions names 1-3 distinct
// HEX_SIGNAL_* raw values; a duplicate, an empty set, more than three
// entries, or a target-unsupported entry (Terminate on the Windows profiles)
// rejects before any native registration.
hex_signals_new_result hex_signals_new(const uint8_t *subscriptions, size_t count);

typedef struct hex_signals_next_result {
    int status;
    uint8_t signal;
} hex_signals_next_result;

// hex_signals_next parks the calling Task until a subscribed Signal is
// pending, or returns immediately with an already-pending one. Only one
// next() may be active per Signals resource; a concurrent second call
// returns HEX_SIGNAL_BUSY without consuming state.
hex_signals_next_result hex_signals_next(hex_signals signals);
// hex_signals_close invalidates the public handle immediately, wakes an
// active next() with HEX_SIGNAL_EOS, and releases every native watcher
// through its own close callback exactly once.
int hex_signals_close(hex_signals signals);

hex_t_Error hex_signal_error(size_t line, size_t column, int status, const hex_string *message);
{{- end}}

#endif
