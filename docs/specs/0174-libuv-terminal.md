# RFC 0174: libuv Terminal

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: define portable TTY classification, terminal IO, modes, dimensions,
  and cleanup over libuv
- Depends on: ADR 0145, RFC 0168, RFC 0169, and RFC 0170
- Does not add: a full-screen UI framework or terminal escape-sequence parser

## Summary

Use `uv_guess_handle` and `uv_tty_t` as the sole portable terminal backend.
Ordinary redirected standard streams remain pipes or files rather than being
misclassified as terminals.

Conceptual source shape:

```hexal
if Terminal.is_attached(output) then
    size := try Terminal.size(output)
end
```

Raw-mode spelling remains open.

## Semantic direction

- Classify a standard handle before selecting TTY, pipe, or file behavior.
- Terminal reads and writes use the shared stream and Task bridge.
- Normal, raw, IO, and virtual-terminal modes expose only capabilities that
  have coherent cross-platform contracts.
- Terminal dimensions are queried through libuv.
- Every mode change has an explicit restoration owner.
- Runtime shutdown attempts restoration without hiding an earlier program
  failure.
- Terminal-specific behavior never changes redirected byte streams.

## Libuv ownership

- `uv_guess_handle` owns handle classification.
- `uv_tty_init` and stream operations own terminal IO.
- `uv_tty_set_mode` owns supported mode changes.
- `uv_tty_get_winsize` owns terminal dimensions.
- `uv_tty_reset_mode` participates in process-wide restoration.

No parallel Win32 console or termios implementation remains for these
operations. A native exception requires a demonstrated missing libuv contract.

## Detailed implementation outline

1. Settle Terminal capability, mode, and size types.
2. Route standard-handle classification through libuv.
3. Integrate TTY streams with common IO and Task parking.
4. Implement dimensions and the minimal accepted mode set.
5. Define nesting, ownership, failure, and shutdown restoration.
6. Delete superseded console and termios paths.
7. Validate interactive and redirected behavior on each qualified target.

## Open design questions

1. Is terminal mode changed through a scoped block or explicit set/reset?
2. Which libuv modes are portable Hexal guarantees?
3. How do nested mode changes restore correctly?
4. Which Task owns input when multiple Tasks attempt terminal reads?
5. How are resize notifications exposed without callbacks?

## Validation direction

The final exhaustive Validation section must cover TTY/pipe/file
classification, redirected streams, read/write, supported modes, nested and
failure restoration, dimensions, resize delivery, shutdown, unsupported
targets, and absence of direct console/termios alternatives.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation adds only settled terminal contracts.
