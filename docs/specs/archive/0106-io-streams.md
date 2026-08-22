# RFC 0106: IO — Two Stream Types and Duck-Typed Generics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; discarded in favor of RFC 0108
- Created: 2026-08-21
- Scope: byte streams for the compiled program — an OS descriptor stream and an
  in-memory stream, with the operation set they share
- Ancestry: RFC 0065 established the scope boundary, the four-language
  primitive-set convergence, and the mechanism-availability probes; RFC 0105
  established that files are not pollable and that blocking is therefore the
  only honest contract for this scope. Both are inherited. This RFC differs on
  the type count, the read buffer, and the seek surface.
- Coordinates with: RFC 0091 (scheduler blocking), RFC 0039 (C interop),
  RFC 0055 (filesystem), `docs/reference.md`, `docs/status.md`
- Does not change: allocation, the scheduler, `Error`, `EoS`, or any existing
  language surface. **No new protected operation, no new builtin generic, no
  mutable View.**

## Summary

Two concrete stream types with the same operation names:

```text
IO      a byte stream on an OS descriptor  -- files, pipes, terminals, std handles
Bytes   a byte stream over a List<Byte>    -- in memory
```

Code that works over either is an ordinary generic function:

```hexal
fun drain<S>(source: S, buf: List<Byte>): Size | Error do
    mut total: Size := 0
    while true do
        n: Size | EoS := try source.read(buf)
        if n is EoS then
            break
        end
        total = total + n
    end
    return total
end
```

`drain<IO>` and `drain<Bytes>` are separate monomorphized functions with no
indirect call, no vtable, and no runtime tag. This is the mechanism RFC 0065
identified as the one available to Hexal and then did not use.

**Three consequences follow, and they are the argument for this design.**

**Every I/O-shaped program becomes unit-testable.** A test constructs a `Bytes`
over a `List<Byte>`, runs the same generic function the program runs, and
inspects the result — with no descriptor, no filesystem, and no toolchain. The
project cannot execute generated C, so a design whose only stream is an OS
descriptor is a design whose user code cannot be tested at all.

**No language extension is needed.** RFC 0105 reached
`read(into: MutPtr<Array<Byte, N>>)` and had to make `read` a *protected
operation* to quantify `N`, because user generic parameters are types and `N`
is a value. Reading into `List<Byte>` needs none of that: it is one concrete
type, `List` already carries its own allocator and capacity, and a live `List`
reference already permits mutation without `mut` or `MutPtr`.

**The metal stays visible.** `IO` is a bare descriptor with no buffering, no
`FILE *`, and no hidden state. `Bytes` is a `List<Byte>` and a cursor. Neither
carries a tag; the branch a single tagged type would need on every operation
does not exist.

## The types

```c
typedef struct Hex_io    { intptr_t desc;  } Hex_io;
typedef struct Hex_bytes { Hex_list_Byte *data; size_t cursor; } Hex_bytes;
```

`IO` follows the ownership rule for external state: copies alias one descriptor
and closing through any alias affects all of them. `Bytes` borrows its `List`
and owns nothing; the caller frees the list.

One spelling per platform; `#ifdef _WIN32` appears only inside `io.c`, never in
a signature. `intptr_t` holds a POSIX `int` descriptor or a Windows `HANDLE`.

## Constructors

```text
IO.stdin()   -> IO | Error
IO.stdout()  -> IO | Error
IO.stderr()  -> IO | Error
IO.open(path: String, mode: OpenMode) -> IO | Error
Bytes.over(buffer: List<Byte>) -> Bytes
```

`OpenMode` is a unit ADT, not bit flags:

```hexal
type OpenMode =
    | ReadOnly
    | WriteTruncate
    | WriteCreateNew
    | Append
    | ReadWrite
```

Standard handles are borrowed: the process does not own them, and closing one
traps (see Misuse). `Bytes.over` cannot fail.

## Operations

```text
S.read(into: List<Byte>)   -> Size | EoS | Error
S.write(from: View<Byte>)  -> Size | Error
S.seek(to: Seek)           -> Size | Error
S.close()                  -> Error          -- IO only
```

```hexal
type Seek =
    | Start(Size)
    | Current(Int64)
    | End(Int64)
```

**One seek, not three.** `position()` is `seek(Seek.Current(0))`, and `skip` is
`seek(Seek.Current(delta))`. RFC 0105 proposed all three as primitives; two of
them are the third with a constant argument, and the language's goal is one
obvious way.

**`read` appends into the list's spare capacity** and returns how many bytes it
added. The caller controls the ceiling by the list's capacity, which it already
owns. `Bytes.read` copies from the cursor; `IO.read` issues one system call.

**Partial transfers are ordinary.** A short read or write is a smaller `Size`,
not a failure. Loops that insist on completion are library code above these
primitives.

**End of input is `eos`**, following `Channel<T>.receive() -> T | EoS`: a
drained source is a completion, not an error. `Size | EoS | Error` carries
count, completion, and failure with no new result type and no out-parameter.

**Capability mismatch is `Error`.** Seeking a pipe returns `ESPIPE` as an
`Error`, because the mismatch is a property of the environment rather than of
the program's logic. `Bytes.close()` does not exist: `Bytes` has no descriptor,
and the asymmetry is deliberate — a generic function that closes is written
over `IO`, not over both.

**`close` returns `Error`** because POSIX `close` reports delayed write failure.

## Ownership and misuse

Compile-time rejection reuses the existing freed-state machinery unchanged —
the same local analysis that guards `Heap.free`:

- closing an alias chain traceable to `ref` is rejected;
- using a locally-closed binding is rejected;
- undecidable cases (handles in parameters, members, collections) are accepted,
  and the OS reports `EBADF` as an `Error`.

**Closing a standard handle traps.** The close wrapper compares against the
three reserved descriptors first. One known consequence, which RFC 0105 asserts
the Mutex precedent over without noting: a program that closes descriptor 1 and
later opens a file may receive descriptor 1 again, and closing that legitimately
owned handle would trap. The alternative — no check — turns a common mistake
into silent corruption of the process's own output. The trap is the better
trade, and the case is recorded rather than hidden.

Concurrent use of one `IO` from several Tasks is allowed. Each operation is as
atomic as its system call; ordering between concurrent operations is
unspecified, matching C.

## Blocking

**Operations block the OS worker. That is the contract for this scope.**

Regular files are not pollable on any supported platform: `epoll` rejects them
and `select` always reports them ready. There is no readiness to register, so
the fork RFC 0091 describes does not exist here — only one branch of it does.

A blocked operation parks one worker and the scheduler runs the rest. A program
whose every task blocks stalls, exactly as a C program whose every thread does.

This is stated as a limitation, not solved, and **the fix is additive when it
is wanted**: RFC 0091's blocking-pool option is a runtime change that parks the
task instead of the worker without altering one signature here. Readiness-based
operations arrive with sockets, where pollability pays for its complexity, and
they extend this family rather than replacing it — which is why the
representation is a bare descriptor.

## `print`

`print` lowers through `fwrite(stdout)` today (`packages/print.c:4`). Mixing
buffered `stdio` with raw descriptor writes to the same descriptor produces
unpredictable interleaving, so `print` moves to the same `write` path.

**This is a cost, not only a coherence win, and the spec states it as one.**
Every `print` becomes a system call where previously several could coalesce in
the stdio buffer. A program printing in a loop gets measurably slower. The
compensation is that batching becomes explicit and available: build a
`List<Byte>` and issue one `write`. That is the "better C" trade — C's stdio
buffering is a well-known source of surprise, and an explicit buffer is more
honest than an implicit one — but it is a regression on a benchmark and should
be measured, not asserted.

## Generated C23

Component artifacts `hexal/io.h` and `hexal/io.c`. C23 facilities used
directly rather than worked around:

- **`enum : uint8_t`** for `OpenMode` and `Seek` discriminants — a fixed
  underlying type, so the generated tag has a pinned width instead of an
  implementation-defined one;
- **`constexpr`** for the three reserved standard descriptors;
- **`unreachable()`** (`<stddef.h>`) in the default arm of every exhaustive
  mode switch, which is exactly what the checker has already proven;
- **`static_assert`** pinning `sizeof(intptr_t) >= sizeof(int)` on POSIX and
  `>= sizeof(HANDLE)` on Windows;
- **`<stdckdint.h>`** guarding `Seek.Current` and `Seek.End` offset arithmetic
  before the system call, matching the existing checked-arithmetic contract;
- **`nullptr`** at every pointer sentinel;
- **`[[noreturn]]`** on the trap path through the one program-wide
  `hex_runtime_trap`.

```c
static constexpr intptr_t HEX_IO_STDIN = 0;

Hex_io_read_result hex_io_read(Hex_io h, Hex_list_Byte *into) {
    size_t room = into->capacity - into->length;
    if (room == 0) { return hex_io_size(0); }
#ifdef _WIN32
    DWORD got = 0;
    if (!ReadFile((HANDLE)h.desc, into->data + into->length,
                  (DWORD)room, &got, nullptr)) {
        return hex_io_error("read");
    }
#else
    ssize_t got = read((int)h.desc, into->data + into->length, room);
    if (got < 0) { return hex_io_error("read"); }
#endif
    if (got == 0) { return hex_io_eos(); }
    into->length += (size_t)got;
    return hex_io_size((size_t)got);
}
```

`Error` values carry the failing operation and the platform error text
(`strerror` / `FormatMessage`), through the existing `Error` type. No new error
category.

## Example

```hexal
fun copy<S>(source: S, sink: IO, buf: List<Byte>): Size | Error do
    mut total: Size := 0
    while true do
        n: Size | EoS := try source.read(buf)
        if n is EoS then
            break
        end
        written: Size := try sink.write(buf.slice(0, n))
        total = total + written
        buf.clear()
    end
    return total
end

fun cat(h: Heap, path: String): Size | Error do
    buf: List<Byte> := List<Byte>.new(h)
    defer buf.free(h)
    source: IO := try IO.open(path, OpenMode.ReadOnly)
    errdefer source.close()
    out: IO := try IO.stdout()
    n: Size := try copy<IO>(source, out, buf)
    _: Error := source.close()
    return n
end
```

Every construct here was compiled against the current checker before being
written down: `List<Byte>` with `new`/`push`/`length`/`free`, `while true` with
`break`, `is EoS` narrowing, `try`, `errdefer`, a generic body calling a method
on its type parameter, and a unit ADT matched by name.

## Blocked on a compiler defect

The canonical loop above works because `total` is a plain `mut Size` and
`total = total + n` is arithmetic. **Returning a narrowed binding directly from
a fallible function does not compile today**:

```hexal
fun demo(): Size | Error do
    r: Size | EoS := s()
    if r is EoS then
        return 0
    end
    return r        -- [Unknown Error] union injection has invalid checked metadata
end
```

Narrowed into a plain `Size` compiles; narrowed into `Size | Bool` compiles;
an un-narrowed `Size` into `Size | Error` compiles. Only a narrowed binding
injected into a union containing `Error` fails, and an `Unknown Error` is by
definition a compiler defect. Recorded in `docs/status.md`.

This does not block the design, but it blocks the most natural user code
written against it, so it should be fixed before this surface ships.

## Validation

- `IO` and `Bytes` each expose `read`, `write`, and `seek` with identical
  signatures; only `IO` exposes `close`.
- A generic function calling `read` compiles and monomorphizes for both, with
  no indirect call and no shared dispatch helper in the generated C.
- `Bytes.over` a `List<Byte>` reads back exactly the bytes written, reaches
  `eos` at the end, and seeks to `Start`, `Current`, and `End` positions —
  exercised entirely in the ordinary Go suite with no descriptor.
- A short read returns a smaller `Size`; a drained source returns `eos`;
  neither is an `Error`.
- `seek` on a `Bytes` past the end and `seek` on a pipe each return `Error`.
- Closing a standard handle traps; closing an owned handle twice through a
  locally-visible binding is rejected at compile time.
- `OpenMode` and `Seek` lower to `enum : uint8_t`, every exhaustive switch ends
  in `unreachable()`, and `Seek.Current`/`Seek.End` arithmetic is guarded by
  `ckd_add` before the call.
- The three standard-descriptor constants are `constexpr`.
- `print` emits a `write` on the stdout descriptor and no `fwrite`; the print
  component names no `stdio` buffering.
- No signature in `hexal/io.h` contains `#ifdef`.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Sockets, readiness, non-blocking operations, timeouts.
- Directory operations, path types, `stat` — the filesystem specification.
- Buffered or framed layers, line scanning, text decoding — library code above
  these primitives.
- Memory-mapped files, async I/O, overlapped structures.
- A third stream type, or any attempt to abstract `IO` and `Bytes` behind one
  value. Without interfaces that requires a runtime tag, and generics already
  cover the cases that matter.
- Stack-allocated read buffers. They require either a protected `read` or
  mutable Views; both are language changes, and `List<Byte>` covers the
  file-shaped cases. Recorded so the question is answered rather than reopened.

## Drawbacks

- **Two types where most languages have one.** A function that must accept
  either at run time cannot be written; it must be generic, which means the
  choice is made at the call site. Go, Crystal, and Odin all permit the runtime
  choice. This is the same no-type-erasure cost RFC 0065 recorded for the
  mechanism, paid deliberately in exchange for no dispatch and full testability.
- **Every read allocates a buffer once.** `List<Byte>` is heap-backed, so a
  program that would have used a 4 KiB stack array pays one allocation. It is
  one allocation per buffer, not per read, and the buffer is reused.
- **`print` gets slower.** See above.
- **`Bytes` has no `close`,** so the two types are not perfectly symmetric and a
  generic function that closes will not accept `Bytes`. The alternative — a
  no-op `close` on `Bytes` — makes a resource operation silently meaningless,
  which is worse.

## Open decisions

1. **Is `Bytes` worth its weight?** It exists for testability and for in-memory
   composition. If the answer is that user code will always be written against
   `IO` directly, the generic machinery earns nothing and RFC 0105's single type
   is simpler.
2. **`read` ceiling.** Reading into spare capacity means the caller controls the
   ceiling by sizing the list. An explicit `max: Size` parameter is the
   alternative and is more obvious at the call site.
3. **`Seek` variant payload types.** `Start(Size)` with `Current(Int64)` and
   `End(Int64)` is asymmetric but honest: an absolute position is unsigned and a
   relative one is signed.
4. **Should `print` keep a buffer** that the runtime flushes at exit, trading
   the interleaving guarantee back for throughput?
