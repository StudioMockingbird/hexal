# RFC 0174: Terminal Detection, Dimensions, and Text Output

- Kind: Feature Specification (Rust-Style RFC)
- Status: Design settled; detailed implementation specification not started
- Created: 2026-09-13
- Updated: 2026-09-14
- Scope: provide portable terminal detection and dimensions, and make `print`
  emit correct Unicode text to an attached Windows console
- Depends on: RFC 0168 and the implemented RFCs 0169, 0170, 0171, 0180,
  and 0181
- Does not add: raw mode, terminal input ownership, resize notification, escape-
  sequence parsing, a full-screen UI framework, or a public libuv handle

## Summary

The first terminal surface is intentionally small:

- determine whether an `IO` value names an attached terminal;
- query the terminal's current dimensions; and
- preserve `print` as UTF-8 text: redirected output receives the original UTF-8
  bytes, while an attached Windows console receives correct Unicode text
  through libuv's UTF-8-to-UTF-16 TTY path.

`IO.write` remains a byte-stream operation. It does not gain text conversion.
Raw mode and resize delivery are independent capabilities and remain deferred.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Terminal attachment detection | Pick up | Needed for color, progress, and terminal-aware output. |
| Terminal columns and rows | Pick up | Small, portable query provided directly by libuv. |
| Correct Unicode `print` on an attached Windows console | Pick up | Current `WriteFile` output depends on the console code page and can corrupt otherwise valid Hexal text. |
| Redirected `print` as exact UTF-8 bytes | Preserve | Pipes and files consume bytes; terminal handling must not change them. |
| Raw input mode | Skip in v1 | Requires restoration, nesting, Task ownership, trap-path cleanup, and exclusive input rules. |
| Normal/IO/RAW_VT mode exposure | Skip in v1 | `IO` is Unix-only and `RAW_VT` is Windows-specific and evolution-prone. |
| Terminal input through `uv_tty_t` | Skip in v1 | It would compete with current `IO.stdin()` reads and requires one exclusive owner. |
| Resize notification | Skip in v1 | Libuv provides a size query but no portable resize event. Signal and Windows behavior differ. |
| Escape-sequence parsing and terminal UI | Skip | These belong in libraries, not the language/runtime core. |
| Arbitrary descriptor-to-terminal conversion | Skip in v1 | Current Windows `IO` stores a `HANDLE`, while libuv terminal APIs consume a CRT file descriptor. |

## Proposed source surface

```text
Terminal.is_attached(stream: IO) -> Bool
Terminal.size(stream: IO) -> TerminalSize | Error

type TerminalSize is struct
    columns: Size,
    rows: Size,
end
```

- `Terminal` is a protected compiler-owned namespace type and has no values.
- `TerminalSize` is a protected complete value type.
- `is_attached` returns false for a valid non-terminal stream. It performs no
  allocation and does not return Error.
- `size` succeeds only for an attached terminal. A valid non-terminal stream
  returns `ErrorKind.InvalidInput()` with message `stream is not a terminal`.
- A closed, stale, or unavailable stream follows the stream's existing Error
  or trap contract; terminal operations do not create a second liveness model.
- Neither operation changes terminal state or takes ownership of the stream.

The source surface is settled for v1.

## Text-output contract

- `String`, `Strand`, Rune, Error, and aggregate formatting still produce UTF-8
  text before reaching the print sink.
- Redirected stdout is written through the current byte-oriented sink without
  transcoding or newline rewriting.
- On Windows, attached-console stdout is written through libuv TTY output so
  valid UTF-8 is converted to UTF-16 and delivered through the console API.
- On POSIX, terminal and redirected output both preserve the same UTF-8 bytes.
- One `print` call remains write-all and source ordered. Short writes and
  failures retain the current runtime-trap contract.
- `IO.write` remains byte-exact on every target, including when its destination
  happens to be a terminal.
- Terminal-aware print selection is cached only after the runtime has proved
  the standard output identity; replacing a process standard handle after
  bootstrap is outside this v1 contract.

## Libuv and runtime ownership

- On POSIX, `uv_guess_handle` owns classification and `uv_tty_get_winsize`
  owns dimensions.
- On Windows, `GetConsoleMode`/`GetFileType` own classification and
  `GetConsoleScreenBufferInfo` owns dimensions. These are deliberate stateless-
  query exceptions because current `IO` stores a `HANDLE` while libuv requires
  a CRT descriptor.
- `uv_tty_init`, `uv_write`, and normal libuv close completion own the private
  attached-console print sink.
- No public `uv_tty_t`, callback, descriptor, `HANDLE`, or platform enum enters
  a generated module header.
- The private TTY sink is process-lifetime runtime state, not a source-visible
  copied handle. Its callbacks execute no Hexal code.
- TTY print completion publishes its result before waking the calling Task.
- A program using only detection or dimensions selects libuv, the native
  bootstrap, and the event component only if its chosen implementation needs a
  live `uv_tty_t`. A program whose reachable surface uses none of this RFC
  emits no terminal component.
- There is no prior terminal backend to remove. The implementation changes only
  the current Windows attached-console print branch that it demonstrably
  replaces.

## Windows `IO` representation decision

Current Windows `IO` stores a native `HANDLE`. Libuv defines `uv_file` as a CRT
`int` and accepts that descriptor in `uv_guess_handle` and `uv_tty_init`.
Passing the current value directly is invalid; creating a temporary descriptor
with `_open_osfhandle` also transfers close responsibility and can accidentally
close the standard handle.

Use `GetConsoleMode`/`GetFileType` for `Terminal.is_attached` and
`GetConsoleScreenBufferInfo` for dimensions. Libuv owns Unicode output through
the standard CRT descriptors.

- Current Windows `IO` remains `{ HANDLE, access, owned }`; no CRT descriptor is
  added to its public or generated representation.
- The native queries preserve the composable `Terminal.is_attached(stream)` and
  `Terminal.size(stream)` signatures for arbitrary valid IO values.
- Libuv still owns stateful attached-console text output through the standard
  stdout CRT descriptor.
- Revisit a CRT-descriptor-based IO representation only if future C interop
  demonstrates that it should become the canonical Windows representation.

## Deferred raw-mode contract

A later focused RFC may add raw input. It must settle all of the following
rather than extending this RFC during implementation:

- one active mode guard per terminal or a serialized process-wide nesting stack;
- exclusive terminal-input ownership and second-reader `Busy` behavior;
- normal return, Error unwind, trap, and process-abandonment restoration;
- the portable mode set (initial evidence supports only Normal and Raw); and
- resize delivery without pretending `uv_tty_get_winsize` is an event source.

The likely source shape is an explicitly closeable restoration guard:

```hexal
mode := try Terminal.raw(input)
defer mode.close()
```

This is explanatory future syntax, not part of v1.

## Required implementation work

1. Add protected Terminal and TerminalSize type metadata without exposing a
   libuv type.
2. Add demand-driven terminal component discovery.
3. Implement attachment detection and dimensions with portable ErrorKind
   mapping and fixed operation messages.
4. Route only attached Windows-console `print` through one private libuv TTY
   sink; retain exact redirected bytes and all existing non-Windows behavior.
5. Preserve `IO.write`, IO representation, Task parking, and print write-all
   behavior.
6. Add generated-C and runtime validation under each qualified target gate.
7. Update `docs/reference.md` only after implementation stabilizes and only
   with explicit user approval.

## Validation direction

The final exhaustive Validation section must cover:

- attached and redirected stdin/stdout/stderr classification;
- exact columns and rows, non-terminal size failure, and ErrorKind/message;
- valid non-ASCII text printed correctly to a Windows console;
- the same text redirected as byte-identical UTF-8;
- `IO.write` remaining byte-exact and unchanged;
- print write-all ordering, Task parking, short writes, and failure behavior;
- no competing terminal-input reader, raw mode, resize surface, public libuv
  type, or alternate TTY backend; and
- no terminal artifact or dependency in a program that does not reach this
  surface or terminal-aware print.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation adds
only the settled Terminal surface and the platform-independent text-output
contract after behavior stabilizes.
