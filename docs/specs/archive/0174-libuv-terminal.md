# RFC 0174: Terminal Detection, Dimensions, and Text Output

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented as written, with no documented deviation.
  `Terminal.is_attached`/`.size()` and `TerminalSize` are in place, backed by
  `GetConsoleMode`/`GetFileType`/`GetConsoleScreenBufferInfo` on Windows and
  `uv_guess_handle`/`ioctl(TIOCGWINSZ)` on POSIX (unqualified, uncompiled by
  the current toolchain gate like every other POSIX branch in this
  codebase), reusing the shared IO native-error mapper rather than a second
  classification table. `print`'s attached-Windows-console path now converts
  and writes complete UTF-8 scalar chunks through `WriteConsoleW` via a
  fixed-size stack buffer, Task-aware through the existing worker bridge;
  redirected output and every POSIX destination keep the exact byte sink.
  Fixed in the same change: quoted `String`/`Rune` rendering and bare `Rune`
  printing previously emitted a non-ASCII multibyte scalar one byte at a
  time through the byte-exact sink, which this RFC's own text called out as
  the thing to avoid -- both now batch through the text sink instead, with
  no change to escaping decisions or byte-for-byte output on every
  redirected/POSIX destination the tagged suite verifies. A collection
  specialized over `TerminalSize` is routed to module-owned rendering
  instead of the shared collection component, the same accommodation
  Signal's closure made, with no effect on program behavior or on
  `TerminalSize`'s own equality support. Verified by `go test ./...`,
  `go vet ./...`, and the tagged C23 suite (including attached-vs-redirected
  classification and the exact `InvalidInput` size-query failure) running
  under GCC, Clang, and `zig cc`. `docs/reference.md` documents the public
  surface, the text-output contract, the error table, and component
  selection
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
  through native UTF-8-to-UTF-16 conversion and `WriteConsoleW`.

`IO.write` remains a byte-stream operation. It does not gain text conversion.
Raw mode and resize delivery are independent capabilities and remain deferred.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Terminal attachment detection | Pick up | Needed for color, progress, and terminal-aware output. |
| Terminal columns and rows | Pick up | Small native query that needs no retained terminal handle or mode change. |
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
Terminal.is_attached(stream: IO) -> Bool | Error
Terminal.size(stream: IO) -> TerminalSize | Error

type TerminalSize is struct
    columns: Size,
    rows: Size,
end
```

- `Terminal` is a protected compiler-owned namespace type and has no values.
- `TerminalSize` is a protected complete value type.
- `is_attached` returns false for a valid non-terminal stream. It performs no
  source-visible allocation. Native query failure or invalid external state
  returns Error through the ordinary ErrorKind mapping.
- `size` succeeds only for an attached terminal. A valid non-terminal stream
  returns `ErrorKind.InvalidInput()` with message `stream is not a terminal`.
- A closed, stale, or unavailable stream returns the existing operation's
  Error classification; terminal operations do not create a second liveness
  model.
- Neither operation changes terminal state or takes ownership of the stream.

`TerminalSize` is an ordinary immutable value with readable `columns` and
`rows`, ordinary struct equality/printing/placement, and call-shaped
construction. `Terminal` and `TerminalSize` are protected names; existing user
declarations with either name become invalid.

## Text-output contract

- `String`, `Strand`, Rune, Error, and aggregate formatting still produce UTF-8
  text before reaching the print sink.
- Redirected stdout is written through the current byte-oriented sink without
  transcoding or newline rewriting.
- On Windows, attached-console stdout is converted and delivered as one logical
  `print` call. Every implementation chunk ends at a UTF-8 scalar boundary, so
  a multibyte scalar is never split or corrupted.
- On POSIX, terminal and redirected output both preserve the same UTF-8 bytes.
- One `print` call remains write-all and source ordered. Short writes and
  failures retain the current runtime-trap contract.
- `IO.write` remains byte-exact on every target, including when its destination
  happens to be a terminal.
- Runtime traps and internal diagnostics continue to write their current ASCII
  bytes to stderr; this RFC does not route them through the text sink.
- Windows classifies the current stdout handle at each logical print call.
  Replacing a process standard handle therefore affects the next call and adds
  no process-lifetime terminal cache.

Before classification, POSIX validates the descriptor with `fcntl(F_GETFD)`
and Windows distinguishes a valid non-console handle from `FILE_TYPE_UNKNOWN`
with a non-zero `GetLastError()`. An invalid descriptor or handle is therefore
Error, not false. POSIX retries only an interrupted `fcntl`; it does not retry
terminal queries whose state may have changed.

## Libuv and runtime ownership

- On POSIX, `uv_guess_handle` owns classification and the native
  `ioctl(TIOCGWINSZ)` query owns dimensions. `uv_tty_get_winsize` is not a
  stateless descriptor query: it requires a live `uv_tty_t`, which would add
  loop and handle ownership merely to read two values.
- On Windows, `GetConsoleMode`/`GetFileType` own classification and
  `GetConsoleScreenBufferInfo` owns dimensions. Columns and rows come from the
  visible `srWindow` rectangle, never the potentially much larger backing
  buffer. These are deliberate stateless-
  query exceptions because current `IO` stores a `HANDLE` while libuv requires
  a CRT descriptor.
- No public `uv_tty_t`, callback, descriptor, `HANDLE`, or platform enum enters
  a generated module header.
- Detection and dimensions select only their small platform query component:
  POSIX classification links the already-pinned libuv helper and therefore the
  native bootstrap, while Windows classification and both native size queries
  select neither the event component nor scheduler.
- Windows attached-console print uses direct native UTF-8 conversion and
  `WriteConsoleW`; redirected output keeps the current byte sink. Outside a
  Task the native entry runs directly; inside a Task that same entry runs
  through the existing worker bridge.
- A program whose reachable surface uses none of this RFC and contains no print
  emits no terminal component.
- There is no prior terminal backend to remove. The implementation changes only
  the current Windows attached-console print branch that it demonstrably
  replaces.

## Selected Windows print backend

- Use `MultiByteToWideChar` with strict UTF-8 validation and `WriteConsoleW`
  for attached-console text.
- Convert through a fixed-size stack buffer. Every input chunk ends on a UTF-8
  scalar boundary and every output chunk preserves UTF-16 surrogate pairs;
  no heap allocation or whole-value size limit is introduced.
- Keep redirected output on the existing byte write-all sink.
- Outside a Task, call the native entry directly. Inside a Task, run the same
  entry through the existing `uv_queue_work` bridge so a slow console does not
  block a scheduler worker. Execution placement changes; the backend does not.
- Print alone selects no libuv loop, scheduler, mimalloc, or native bootstrap.
  If another selected feature already supplies a Task context, print reuses its
  existing worker bridge.
- Conversion or console-write failure retains the exact runtime trap
  `[Runtime Error] standard output write failed`.
- `hex_print_text` owns terminal-aware conversion. Every textual helper routes
  complete UTF-8 scalar sequences through it; `hex_print_bytes` and `IO.write`
  remain byte-exact. In particular, quoted String rendering must not emit a
  multibyte scalar one byte at a time.

## Settled query and error contract

- `size` returns the visible positive column and row counts converted to Size.
  A native zero or negative dimension is `ErrorKind.InvalidInput()` with fixed
  message `terminal dimensions are invalid`.
- A valid non-terminal passed to `size` returns `ErrorKind.InvalidInput()` with
  fixed message `stream is not a terminal`.
- Any other classification failure uses the common native ErrorKind mapping
  with fixed message `terminal detection failed`; any other size-query failure
  uses fixed message `terminal size query failed`.
- Reuse the existing IO native-error mapper for `errno` and `GetLastError()`.
  Promote that private runtime helper as needed; do not create a second native-
  error classification table for Terminal.
- Detection and size are stateless calls. They initialize no loop, retain no
  terminal handle, change no terminal mode, and take no ownership.

## Windows `IO` representation decision

Current Windows `IO` stores a native `HANDLE`. Libuv defines `uv_file` as a CRT
`int` and accepts that descriptor in `uv_guess_handle` and `uv_tty_init`.
Passing the current value directly is invalid; creating a temporary descriptor
with `_open_osfhandle` also transfers close responsibility and can accidentally
close the standard handle.

Use `GetConsoleMode`/`GetFileType` for `Terminal.is_attached` and
`GetConsoleScreenBufferInfo` for dimensions. Native UTF-8-to-UTF-16 conversion
and `WriteConsoleW` own Unicode output independently of those stateless queries.

- Current Windows `IO` remains `{ HANDLE, access, owned }`; no CRT descriptor is
  added to its public or generated representation.
- The native queries preserve the composable `Terminal.is_attached(stream)` and
  `Terminal.size(stream)` signatures for arbitrary valid IO values.
- Attached-console text output uses `MultiByteToWideChar` and `WriteConsoleW`;
  the IO representation already supplies the required native handle.
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

## Detailed implementation plan

### Phase 1: metadata and checked calls

1. Add protected `Terminal` and `TerminalSize` identities in `compiler/types`.
2. Register `Terminal.is_attached` and `Terminal.size` in the checker with exact
   receiver-free static-call identities, arguments, results, and diagnostics.
3. Give TerminalSize its two Size fields and ordinary immutable-struct
   construction, placement, equality, and printing behavior.

### Phase 2: terminal query component

1. Add focused generator discovery and
   `compiler/generator/packages/terminal.h`/`terminal.c` templates.
2. Implement Windows HANDLE classification and visible-window size, and POSIX
   descriptor validation, classification, and `ioctl(TIOCGWINSZ)`, without
   retaining or owning IO.
3. Route failures through the existing source-located Error construction and
   shared IO native-error mapping. Move that mapper out of file-private scope
   rather than duplicating it. Emit no terminal component without a reachable
   Terminal operation.

### Phase 3: Windows text sink

1. Split terminal-aware text from byte-exact writes in `print.c`/`io.c` without
   changing the public IO API.
2. Implement per-call stdout classification, scalar-safe UTF-8 chunking,
   `MultiByteToWideChar`, UTF-16-pair-safe output chunking, and WriteConsoleW
   write-all behavior.
3. Audit every print helper, especially quoted String/Rune and aggregate
   helpers, so text never bypasses `hex_print_text` or splits a scalar.
4. Preserve one logical print-call serialization across every helper write;
   Task execution uses the existing worker bridge around the same native sink.

### Phase 4: validation and handoff

1. Add focused checker/generator tests and public integration tests for every
   Validation item.
2. Add Windows attached-console and redirected tagged fixtures, plus POSIX
   query fixtures only on qualified POSIX targets.
3. Measure generated size, dependencies, startup, and one-call print cost
   against the current path; print-only Windows output must add no libuv files
   or dependency.
4. Regenerate only manifest artifacts whose terminal-aware text lowering
   intentionally changes.
5. Update `docs/reference.md` only after behavior stabilizes and only with
   explicit user approval.

## Validation

This list is exhaustive:

- attached and redirected stdin/stdout/stderr classification;
- exact columns and rows, non-terminal size failure, and ErrorKind/message;
- valid non-ASCII text printed correctly to a Windows console;
- multibyte text crossing internal chunk boundaries and quoted Unicode text;
- the same text redirected as byte-identical UTF-8;
- `IO.write` remaining byte-exact and unchanged;
- print write-all ordering, Task-aware nonblocking execution when the scheduler
  is selected, short writes, and failure behavior;
- no competing terminal-input reader, raw mode, resize surface, public libuv
  type, or alternate TTY backend; and
- no terminal artifact or dependency in a program that reaches neither a
  Terminal operation nor terminal-aware print;
- a Windows print-only program adds no libuv, event, scheduler, or mimalloc
  dependency; and
- protected-name migration, TerminalSize construction/fields/equality/printing,
  and exact generated source mapping.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation adds
only the settled Terminal surface and the platform-independent text-output
contract after behavior stabilizes.
