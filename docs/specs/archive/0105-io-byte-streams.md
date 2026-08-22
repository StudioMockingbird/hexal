# RFC 0105: IO — Byte Streams on Descriptors

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; discarded in favor of RFC 0108
- Created: 2026-08-21
- Scope: byte streams on an open OS descriptor for the compiled program —
  files, pipes, terminals, and the standard handles
- Ancestry: RFC 0065 (byte stream I/O). Its scope boundary, primitive-set
  convergence evidence, and verified language probes remain valid and are
  inherited. This RFC supersedes 0065's deferrals with decisions; 0065 stays
  untouched until the author reconciles them.
- Coordinates with: RFC 0091 (blocking-syscall boundary), RFC 0039 (C interop),
  RFC 0055 (filesystem/build driver), `docs/reference.md`, `docs/status.md`

## Summary

One non-generic handle type `IO` over a raw OS descriptor — POSIX `int` file
descriptor, Windows `HANDLE` — stored in an `intptr_t`. No buffering layer, no
`FILE *`, no hidden state. Operations lower to one thin generated-C wrapper per
operation over `read`/`ReadFile`, `write`/`WriteFile`, `lseek`/`SetFilePointer`,
and `close`/`CloseHandle`.

The design takes the four positions RFC 0065 left open:

| 0065 question | Position taken here |
|---|---|
| Blocking × scheduler | Blocking is the contract for this scope; accepted and documented. Readiness is deferred with sockets, where it pays. |
| Descriptor or `FILE *` | Descriptor. No buffering means no flush, no interleaving hazard, and the descriptor is directly visible. |
| Non-generic `IO` | Confirmed. |
| One type or several | One type. Environmental capability mismatch (seek on a pipe) returns `Error`; only contract violations trap. |

Plus one deliberate departure from 0065: `open` is included, because a stream
type with no way to obtain a stream is not testable or useful on its own.

## Why a descriptor, not `FILE *`

RFC 0065 identified the two objections (stdio buffering hides descriptor-level
events; `fileno` is not standard C). This RFC resolves them by going below
`stdio` entirely:

- **No flush.** A write that returned has been handed to the kernel. Process
  shutdown needs no stdio drain, and `print` cannot interleave unpredictably
  with program writes to the same descriptor.
- **No hidden state.** Hexal passes allocators explicitly and stores no ambient
  configuration; a bare descriptor is the same ethic applied to I/O.
- **The readiness escape hatch stays open.** Pollable descriptors can be handed
  to `poll`/`WSAPoll` by a future evented layer without unwrapping anything,
  because there is nothing to unwrap.
- **It is what C programmers ship.** Serious C I/O code uses unbuffered
  descriptors and buffers explicitly when profiling says so. Hexal should offer
  the same floor, not a softer ceiling.

Cost, accepted: every copy boundary is explicit, and short writes are visible
at every call site. Both are the point.

## The type

```hexal
type-free builtin: IO
```

`IO` is a protected nominal handle following the ownership rule: it refers to
external state, so copies alias one descriptor, and closing through any alias
affects all of them. Representation in generated C:

```c
typedef struct hex_io {
    intptr_t desc;   /* POSIX: file descriptor; Windows: HANDLE value */
} hex_io;
```

One spelling on both platforms; no `#ifdef` reaches user-visible signatures.
A single-pointer struct passes in a register; the wrapper is the metal, spelled
once.

There is no invalid or zero `IO`. Every `IO` binding is initialized from a
constructor; there is no null-analog state to check.

## Constructors

```text
IO.stdin()  -> IO | Error
IO.stdout() -> IO | Error
IO.stderr() -> IO | Error
IO.open(path: String, mode: OpenMode) -> IO | Error
```

Standard handles report Error when the platform cannot supply them (a Windows
process launched without a console has no stdin). They return borrowed
descriptors: the process does not own them.

`OpenMode` is a small ADT owned by the IO library, replacing C's bit-flag
combinatorics with five named intents covering the overwhelming majority of
real `open(2)` usage:

```hexal
type OpenMode = ReadOnly        /* O_RDONLY                        */
              | WriteCreateNew  /* O_WRONLY | O_CREAT | O_EXCL     */
              | WriteTruncate   /* O_WRONLY | O_CREAT | O_TRUNC    */
              | Append          /* O_WRONLY | O_CREAT | O_APPEND   */
              | ReadWrite       /* O_RDWR                          */
```

No bit flags (Hexal has no bitwise operations on ADTs anyway), no boolean
parameters disguising modes, no combinatorial constructor explosion. The
generated mapping table per platform lives beside the wrapper. Paths are
validated by the OS at open time; richer path types belong to the future
filesystem specification, which will extend construction rather than replace
it.

## Operations

```text
IO.read(into: MutPtr<Array<Byte, N>>) -> Size | EoS | Error
IO.write(from: View<Byte>)            -> Size | Error
IO.position()                         -> Size | Error
IO.seek(offset: Size)                 -> Size | Error
IO.skip(delta: Int64)                 -> Size | Error
IO.close()                            -> Error
```

- **Partial transfers are ordinary.** A short read is a smaller `Size`; a short
  write is a smaller `Size`. Neither is a failure. Convenience loops are
  library code layered above, not primitives.
- **End of input is `eos`**, applying the established
  `Channel<T>.receive() -> T | EoS` precedent: drained is a completion, not an
  error. `Size | EoS | Error` — count, completion, failure — with no new
  result type and no out-parameters.
- **Capability mismatch is `Error`, not a trap.** Seeking a pipe returns Error
  (C's `ESPIPE`), because the mismatch is a property of the environment, not of
  the program's logic. Traps are reserved for contract violations (below).
- **`close` returns `Error`** because POSIX `close` can report delayed write
  failure. This makes the canonical cleanup shape work with existing
  machinery, results discarded per the cleanup rule:

  ```hexal
  fun process(path: String): Nil | Error do
      f: IO = try IO.open(path, OpenMode.ReadOnly)
      errdefer f.close()
      ...
      return nil
  end
  ```

- **Buffers use the types that already exist.** Reads take
  `MutPtr<Array<Byte, N>>` — writable, stack-friendly, bounds known at compile
  time. Writes take `View<Byte>` — read-only, and already produced by
  `Array.slice` and `String.bytes`, so `out.write(text.bytes())` composes on
  day one. The `N` in read's parameter is quantified implicitly: `read` is a
  protected operation (same register as `print` and `size_of`), so one
  implementation serves every array size, lowering to `(pointer, N)` at the
  call site. Growable-buffer reads (`List<Byte>`) are a noted future extension,
  not initial surface.
- **Deliberately absent:** half-close (meaningless for files; the sockets
  successor extends this family), flush (nothing to flush), text variants
  (compose from bytes), EOF query (it is `eos` in the result), buffering
  configuration (there is no buffer).

## Ownership, lifetime, and misuse

Compile-time rejection reuses the existing freed-state machinery unchanged —
the same local analysis that guards `Heap.free` guards `close`:

- closing a traceable-to-`ref` alias chain is rejected;
- using a locally-closed binding (read/write/seek/close again) is rejected;
- undecided cases (parameter-passed handles, members, collections) are
  accepted, and the OS reports `EBADF`-class failure as `Error` — the same
  documented envelope as heap lifetime analysis.

Closing a standard handle **traps**. The three borrowed descriptors are
reserved values; the close wrapper compares against them before issuing the
system call. This is the Mutex precedent — invalid states detectable from live
metadata trap — and it costs one compare. Double-close of an owned handle in
the undecidable cases surfaces as `Error` from the OS, never silent success.

Concurrent use of one `IO` by several Tasks is allowed. Each operation is as
atomic as its system call; ordering between concurrent operations is
unspecified, matching C.

## Blocking — the position

**Within this scope (files, pipes, terminals, std handles), operations block
the OS worker, and that is the documented contract.**

- Regular files are not pollable on any supported platform; there is no
  readiness to wait for. C blocks. Go, the only surveyed runtime handling both
  classes, runs file I/O on a dedicated blocking thread pool. The premise that
  file I/O could be made readiness-based is false, so the fork RFC 0091 worried
  about largely dissolves for this scope.
- A blocked operation parks one worker; the scheduler continues on the rest.
  A program whose every task blocks stalls, exactly as a C program whose every
  thread blocks. This is stated as an accepted limitation, not solved.
- Readiness-based, non-blocking operations arrive with the sockets
  specification, where pollability pays for its complexity. That layer will
  add operations to this family rather than replacing it; `poll`-style waiting
  consumes bare descriptors, which is why the representation choice matters.

This RFC therefore does not depend on RFC 0091 for its core; 0091 remains
owner of the general scheduler-blocking contract and of whatever the evented
layer needs.

## `print` coherence

`print` currently lowers through `fwrite(stdout)`. Mixing buffered `stdio`
output with raw descriptor writes on the same descriptor invites interleaving
surprises, so `print`'s writer migrates to the same `write`/`WriteFile` path on
the stdout descriptor. Required sweep: replace the `fwrite` in the print
component with the shared write-all helper; per-call atomicity is preserved or
improved (single `write(2)` per chunk; pipe writes up to `PIPE_BUF` are
atomic). The shutdown-flush paragraph in the reference loses its stdio clause.

## Generated C23 contract

Component artifacts `hexal/io.h` (types, declarations, thin inline wrappers)
and `hexal/io.c` (platform dispatch, error construction). Modern C23
facilities used directly, per the standing rule:

- `intptr_t` (`<stdint.h>`) holds either platform's descriptor;
- `constexpr` for the reserved standard-descriptor constants;
- `<stdckdint.h>` guards offset arithmetic in `skip` against overflow before
  the system call;
- `[[noreturn]]` trap via the one program-wide `hex_runtime_trap`;
- `static_assert` pins the descriptor-width assumptions per platform;
- platform selection is `#ifdef _WIN32` inside `io.c` only — the same pattern
  the concurrency component already owns for fibers — never in signatures.

Error objects carry the failing operation, the path or descriptor class, and
the platform error text (`strerror(errno)` / `GetLastError` string), preserving
C diagnostic information through the existing `Error` type. No new error
category is introduced.

Sketch of the read wrapper:

```c
/* hexal/io.c */
Size_or_EoS_or_Error hex_io_read(hex_io h, uint8_t *data, size_t cap) {
#ifdef _WIN32
    DWORD got = 0;
    if (!ReadFile((HANDLE)h.desc, data, (DWORD)cap, &got, nullptr)) {
        return hex_io_error("read");
    }
#else
    ssize_t got = read((int)h.desc, data, cap);
    if (got < 0) { return hex_io_error("read"); }
#endif
    if (got == 0) { return eos_member; }
    return size_member(got);
}
```

## Example

```hexal
fun cat(path: String): Nil | Error do
    in: IO = try IO.open(path, OpenMode.ReadOnly)
    errdefer in.close()
    out: IO = try IO.stdout()
    mut buf: Array<Byte, 4096>
    loop: while true do
        n: Size | EoS = try in.read(ref buf)
        if n is EoS then
            break
        end
        written: Size = try out.write(buf.slice(0, n))
        if written != n then
            return Error.new("cat", "short write")
        end
    end
    return nil
end
```

Every line is the C a programmer would have written — minus the manual errno
plumbing, the close-on-error goto ladder, and the buffer-size arithmetic.

## Testing direction

Ordinary tests stay pure Go: signature checking, ownership diagnostics,
emitted-C text assertions on the wrappers, `OpenMode` mapping table checks.
Runtime behavior (real descriptors, pipes, redirection) requires the future
driver/toolchain lifecycle and is out of scope for the ordinary suite.
Standard-handle tests must never close or mutate host process handles.

## Non-goals

- Sockets, readiness/poll, non-blocking operations, timeouts — the evented
  successor extends this family.
- Directory operations, path manipulation, stat — the filesystem specification.
- Buffered or framed layers (line scanning, length-prefixing) — library code
  above the primitives.
- Memory-mapped files, async I/O, overlapped structures.
- Mutable views: read buffers are `MutPtr<Array<Byte, N>>` for now; reopening
  the mutable-View exclusion is a separate language decision this RFC does not
  force, because fixed arrays cover the file-shaped cases.

## Open decisions

1. **`OpenMode` variant set** — are the five intents right, or is
   `ReadWriteTruncate` missing enough to matter?
2. **`skip` versus a whence-parameter** — three explicit seek forms are
   proposed; a single `seek(from: SeekFrom, offset: Int64)` is the alternative.
3. **Error text shape** — structured fields versus rendered message only.
4. **`List<Byte>` read buffers** — initial surface or fast-follow.

## Readiness

Not implementation-ready: this is a discussion draft. It is, however,
deliberately unblocked — unlike RFC 0065, no question here waits on another
specification. The blocking position taken is self-contained; if the author
rejects it in favor of a readiness-first contract, the affected sections are
Operations, Blocking, and the sockets hand-off, not the foundation.
