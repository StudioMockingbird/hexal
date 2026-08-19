# RFC 0065: Byte Stream I/O

> Filed as `0065-typed-io.md` when a generic `IO<T>` was the expected shape.
> The number is the identity; the parameter is now expected to be dropped.

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; direction and scope settled, surface not yet designed
- Created: 2026-08-15
- Scope: byte streams over an open handle for the compiled program
- Depends on: RFC 0039 (C interop compiler core) and **RFC 0091**
  (blocking-syscall boundary) — see Prerequisite
- Coordinates with: RFC 0055 (filesystem/build driver), RFC 0064 (removal of
  current File builtins), a future sockets specification, target profiles,
  `docs/reference.md`, and `docs/status.md`

## Summary

Introduce a program-runtime type for byte streams.

**Scope, settled:** *I/O is byte streams on an open handle — read, write, seek,
close, and whatever readiness story sits under them. It does not care where the
handle came from.*

Two consequences follow from that sentence and are recorded here so they are not
re-derived:

- **Directory enumeration is not I/O.** `stat`, `mkdir`, `unlink`, `rename`, and
  reading directory entries operate on *names* and return structured data, not
  bytes. They belong to a filesystem specification. `open` is the seam: a
  filesystem call takes a path and yields a handle, after which the filesystem
  API is no longer involved.
- **Sockets are not in this RFC.** Their addressing half is not I/O either, and
  their representation is not `FILE *` — a Windows `SOCKET` is not a file
  descriptor. Their *stream* half is governed by the same operation set defined
  here, and a sockets specification should adopt it rather than restate it.

This RFC does not yet define constructors, ownership, errors, or standard
handles.

## Prerequisite — the blocking-syscall boundary

**Nothing here can be designed until the scheduler's contract with a blocking
call is settled. That is now RFC 0091, which owns the question and records the
options.**

The scheduler is cooperative: a worker is freed only when a task yields, parks,
or finishes. Channel and Mutex waits park the *task*. A blocking `read` parks
the *OS thread*, so N workers blocked in `read` stall the whole scheduler, and
no Hexal-level construct prevents it.

The answer determines this RFC's shape. If it is readiness-based parking, every
operation signature carries a non-blocking handle and a readiness registration.
If it is thread hand-off, the surface looks conventional. The two are not
refinements of each other.

**And there are two answers to give, not one** — see Blocking class below.

## Goals

- One cohesive input/output abstraction rather than compiler-special-cased
  `File`, `FileMode`, and `Stdio` concepts.
- Direct, readable C23 lowering over one handle representation, chosen after the
  blocking-syscall boundary settles — see C representation direction.
- No hidden filesystem access by the core compiler.
- Compatibility with generated bindings and C libraries through RFC 0039.
- Explicit ownership and failure contracts.
- A small operation surface with one obvious way to perform each supported I/O
  action.
- No lazy collection-transformation API.

## Compiler boundary

This RFC concerns runtime behavior of the compiled Hexal program. It does not
authorize the core compiler to access the host filesystem.

The compiler remains string-in/string-out:

```text
Compile(sources: map[string]string, entrypoint: string)
    -> generated C/header strings and diagnostics
```

RFC 0055 separately owns reading Hexal/C inputs, writing generated artifacts,
and invoking external toolchains.

## C representation direction

The intended value representation is C `FILE *`:

```c
FILE *
```

The detailed design must preserve that direct representation unless a later
decision explicitly accepts wrapper metadata and its ABI/runtime cost.

Consequences of direct `FILE *` representation that require resolution:

- ownership is not encoded in the pointer;
- read/write capability is not encoded in the pointer;
- buffering and text/binary mode are C-library state;
- standard handles are borrowed but share the same representation as owned
  handles;
- shallow copies alias one C stream; and
- closing one alias invalidates every alias.

**`FILE *` is now a narrower commitment than when this RFC was written.** With
sockets excluded and directories excluded, what remains is files and the
standard handles — which `FILE *` does represent. But two of its properties
fight the operation set above:

- **`FILE *` carries its own buffering.** A protocol wants to know when bytes
  reach the descriptor; `stdio` buffering means it does not, and `flush`
  discipline becomes the caller's problem. This is the same objection that
  excluded sockets, applied to files.
- **`FILE *` hides the descriptor**, and the descriptor is what a readiness
  mechanism needs. `fileno` recovers it on POSIX and is absent from standard C.

A raw descriptor (`int` on POSIX, `HANDLE` on Windows) with no buffering layer
is the alternative, and it is what the blocking-boundary answer will most likely
require. **Decide this after the prerequisite, not before** — the buffering and
readiness questions resolve together or not at all.

## Relationship to removed concepts

RFC 0064 removes the current compiler-owned `File`, `FileMode`, and `Stdio`
surface. This is a new design, not a compatibility rename, and need not
preserve those APIs or policies.

It is unrelated to the removed lazy `Stream<T>` collection pipeline. It
does not imply producer State, List sources, `filter`, `map`, `take`, adapter
ownership, or generic `for` iteration.

## Required design areas

### Whether there is a type parameter at all

Define what the type parameter represents. Candidate interpretations include:

- the value unit read or written, such as Byte or text;
- an I/O capability or mode marker; or
- another compile-time protocol.

Do not expose `IO<T>` until T changes valid operations, representation, or
static checking in a useful and coherent way. If T adds no semantic value, use
a non-generic `IO` type instead.

**Deferred, not open-ended — two of the three candidates already have answers,
recorded here so the next pass starts from them rather than re-deriving them:**

- **The value unit is not a real distinction.** Representation is exactly
  `FILE *`, and a `FILE *` can be read as bytes or as runes. Whether a read
  yields Byte or Rune is a property of the *operation*, not of the stream, so
  `IO<Byte>` would carry no information the call site does not already state.
- **A capability marker is a real distinction but collides with the absence of
  subtyping.** `IO<Read>` versus `IO<Write>` would catch a genuine error class
  statically and at zero cost. But Hexal has no subtyping, so a function taking
  `IO<Write>` could not accept an `IO<ReadWrite>`; the options are to add
  subtyping (large, and unrelated to I/O) or to duplicate every API. `Read` and
  `Write` would also be marker types, a category the language has no other use
  for.

On that basis the expected outcome is a **non-generic `IO`**, and the burden of
the next design pass is to show why that is wrong rather than why it is right.
Reopen the parameter only if a third interpretation appears that changes
representation or checking without requiring subtyping.

### Construction and standard handles

Define:

- how paths or foreign handles create an IO value;
- whether opening belongs to a core library rather than compiler syntax;
- how stdin, stdout, and stderr are obtained;
- whether foreign `FILE *` values may wrap or unwrap without allocation; and
- whether construction is fallible and how failure is represented.

### Ownership and lifetime

Define:

- owned versus borrowed handles;
- whether ownership is encoded statically or enforced by runtime policy;
- whether IO values copy as aliases;
- who may close a handle;
- behavior after one alias closes the stream;
- cleanup integration; and
- whether close/flush failures return Error or trap.

### Operations

The primitive set, narrowed:

- **read** into a caller buffer, returning bytes-read or end-of-input;
- **write** from a caller buffer, returning bytes-written;
- **seek** — position within the stream, where the handle supports it;
- **close**;
- **half-close** — "done writing, still reading". Socket-only, with no file
  equivalent, and required by every request/response protocol. It belongs in the
  primitive set even though this RFC does not cover sockets, because omitting it
  forces the sockets specification to invent a parallel surface.

**Partial transfers are normal, not failures.** `read` returning fewer bytes than
the buffer holds and `write` accepting fewer than offered are ordinary outcomes.
An API that hides this behind a loop makes correct protocol code impossible;
one that surfaces it without a convenience wrapper makes every caller write the
same loop. Both are needed, and the primitive is the one that reports the count.

Deliberately **not** primitives: read-text, write-text, flush, end-of-input as a
separate query, and buffering configuration. Text framing, line splitting,
length-prefixed messages, and bulk copy all compose from read, write, and seek.
None should be built in, but the buffer type must make them expressible without
a copy per call — which makes the buffer representation (`View<Byte>` in, a
writable region out) part of this design rather than an afterthought.

Do not add an operation merely because `FILE *` exposes one. Each operation
needs an exact Hexal type, ownership effect, failure result, and C lowering.

### Capability shapes — what decides the type count

|  | read | write | seek | half-close |
|---|---|---|---|---|
| file | yes | yes | **yes** | — |
| socket | yes | yes | no | yes |
| pipe end | one or the other | | no | — |
| stdin / stdout / stderr | one or the other | | sometimes | — |

"Sometimes" is literal: `stdout` redirected to a file is seekable, to a pipe is
not — the same handle, a different capability at run time.

**Hexal has no interfaces**, so this table cannot be collapsed behind a shared
`Reader`/`Seeker`. Either one type carries `seek` and calling it on a
non-seekable handle traps at run time, or the shapes are separate types and the
checker rejects it. That choice is the central open decision of this RFC, and it
is a language-surface question, not an implementation one.

### Blocking class — the axis that does not line up with the others

- **Pollable:** sockets, pipes, terminals. `epoll`/`kqueue`/IOCP report
  readiness, so a task can park and its worker stays free.
- **Not pollable: regular files.** `epoll` rejects them and `select` always
  reports them ready. There is no readiness to wait for — a read either returns
  from page cache immediately or blocks the thread inside the kernel.

**So a netpoller solves sockets and does not solve files.** Go runs file I/O on a
separate thread pool for exactly this reason. The prerequisite specification must
therefore answer twice: readiness-parking for pollable handles, and something
else — thread hand-off, or an accepted block — for regular files.

This is the single sharpest constraint on the design, and it is why this RFC
cannot proceed on its own.

### Modes and text

Define:

- readable, writable, append, and update capabilities;
- binary versus text behavior;
- path representation and validation ownership;
- UTF-8 validation and malformed-input behavior;
- newline translation; and
- partial read/write semantics.

### Errors and completion

Define:

- the distinction between end-of-input and failure;
- whether reads return EoS, Error, counts, partial values, or a new result type;
- preservation of C error information;
- partial side effects before failure; and
- which failures are recoverable versus traps.

### Concurrency

Define:

- whether an IO handle may be used by several Tasks;
- required synchronization;
- interaction with the M:N scheduler when C stdio blocks an OS worker thread;
- atomicity relative to `print`; and
- buffering/flush behavior at process shutdown.

Blocking C `FILE *` operations can block a scheduler worker, not merely one
fiber. This must be an explicit accepted limitation or addressed by a runtime
I/O strategy before implementation.

## Prior art, and what Hexal can take from it

Four languages with comparable goals, surveyed for the operation set and the
polymorphism mechanism. Treat the Zig detail as approximate: its I/O surface was
being reworked when this was written.

| | Abstraction | Concurrency |
|---|---|---|
| **Go** | `io.Reader`/`io.Writer`, two methods; fat pointer + vtable | netpoller parks the goroutine on socket readiness; **files are not pollable**, so the runtime detaches the P and lets the thread block |
| **Crystal** | abstract `IO` class; subclasses implement `read(Bytes)`/`write(Bytes)` | fibers plus an event loop, blocking-looking API |
| **Zig** | historically `comptime`-generic readers/writers monomorphized per type; more recently concrete buffered structs plus an explicit `Io` parameter | chosen by the caller through that parameter |
| **Odin** | `io.Stream` is `{ data, procedure }` — one function pointer switching on a mode enum | blocking only |

**All four agree on the primitive set**: read into a caller buffer, write from
one, both returning a count, with partial transfers ordinary. Four independent
designs reaching the same two operations is the strongest available evidence
that the Operations section above is right.

**Go's split confirms Blocking class.** It is the only one of the four that
handles both pollable and non-pollable handles, and it needs two mechanisms to
do it. That is the same fork this RFC faces.

### Three of the four mechanisms are unavailable

| Mechanism | Hexal |
|---|---|
| Go interfaces | none |
| Crystal inheritance | none |
| Odin function pointer in a struct | **`Fun` cannot be an object member** (`reference.md:459`) |
| Zig `comptime` | none |

### What is available: duck-typed generics

Verified — a generic function may call a method on its type parameter, resolved
per instantiation:

```hexal
fun use<T>(v: T): Int32 do
    return v.get()
end
```

So `fun copy<S, D>(source: S, dest: D)` calling `source.read(…)` and
`dest.write(…)` monomorphizes per instantiation. Generic helpers work across
File, Pipe, and Socket with no vtable and no indirect call. This is Zig's older
model and needs no new language feature.

**The cost, which must be stated in the surface design: no type erasure.** There
is no `List<IO>` of mixed stream kinds and no function that accepts "some
stream" decided at run time. Every use site knows the concrete type. Go,
Crystal, and Odin all support that; Hexal would not.

### The mechanism carries a real signature, not just a toy

The `v.get()` probe above proves the mechanism exists. This one compiles a
reader with a fallible read and a generic consumer over it, which is the shape
the surface will actually have:

```hexal
impl MutPtr<MemReader>.read(buf: MutPtr<Array<Byte, 4>>): Size | EoS | Error do
    if self.cursor >= self.data.length() then
        return eos
    end
    ...
    return copied
end

fun drain<R>(src: MutPtr<R>): Size | Error do
    ...
    outcome: Size | EoS | Error = src.read(ref buf)
    n: Size | EoS = try outcome
    if n is EoS then
        return total
    end
    total = total + n
    ...
end
```

Three facts fall out, none of them decisions, all of them constraints the
surface design inherits:

- **A method may take a `MutPtr<T>` receiver.** `impl MutPtr<MemReader>.read`
  is accepted, which is what a stream needs: reading advances a cursor.
- **The count is the return value; there are no out-parameters.** A C-shaped
  `read(buf, bytes_read: MutPtr<Size>): IoError` is unnecessary here, because a
  union return carries both. Any proposed signature with an out-parameter for
  the transferred count is importing a C constraint the language does not have.
- **`match` cannot express error control flow.** It is an expression
  (`reference.md:525`) and its arms are expressions, so `match err | Case then
  return …` does not parse and neither does `match` in statement position.
  Branching on a failed operation goes through `try`, `errdefer`, or
  `if … is …`. A surface designed around matching an error enum would not
  compile.

### The idea most worth taking

**Zig's explicit `Io` parameter**, because Hexal already passes allocators
explicitly rather than ambiently. Passing the I/O implementation is the same
pattern one level up, and it may dissolve the blocking-boundary question rather
than answering it: a blocking implementation and an evented one behind one
surface, selected by the caller instead of by the runtime.

That is the strongest lead available, and it comes from the language whose
ethos this one already shares. It should be the first thing the detailed design
pass evaluates.

## Architecture direction

Prefer implementing IO through ordinary Hexal bindings/core-library code over
adding dedicated parser AST nodes or checker expression kinds. Compiler support
should be limited to foreign ABI facts that an ordinary library cannot express.

If direct `FILE *` cannot be represented safely through RFC 0039, extend the
C-interop type model rather than creating an unrelated compiler-only I/O
pipeline.

## Testing direction

- Ordinary compiler tests remain pure Go and do not invoke an external
  compiler or perform host file I/O.
- Checker/generator tests validate signatures, ownership restrictions,
  diagnostics, and emitted C strings.
- Runtime read/write/error behavior requires the future driver/toolchain test
  lifecycle.
- Standard-handle tests must not close or mutate the host process's real
  handles from ordinary Go tests.

## Non-goals

- Restoring the current File/FileMode/Stdio API unchanged.
- Reintroducing lazy `Stream<T>`.
- **Element iteration, at any level.** I/O moves bytes across a boundary; a
  lazy sequence is control flow over values, and the two only look alike
  because standard libraries put them next to each other. A line iterator over
  a handle is not I/O with a nicer surface — it is a separate layer that
  happens to pull from one, and it belongs to whatever specification owns
  iteration. This is why the operation set here is bytes-in-a-caller-buffer and
  nothing else: an I/O contract that grows `each_line` has stopped being an I/O
  contract. Recorded as a distinct entry from the one above, which is about a
  removed type; this one is about a boundary that holds however iteration is
  eventually spelled.
- Giving the core compiler filesystem access.
- Defining project discovery, artifact writing, compilation, or linking.
- Committing to async I/O, sockets, networking, or filesystem path types in the
  initial version.
- Treating C `FILE *` as a typed generic value without defining what T means.

## Open decisions

**Blocked on the prerequisite — do not attempt before it:**

1. **How does a blocking read interact with the M:N scheduler**, separately for
   pollable handles and for regular files? Owned by the blocking-boundary
   specification, not by this one, but nothing else here can be settled first.
2. **Descriptor or `FILE *`?** Buffering and readiness resolve together.

**Settled in direction, needing only the surface written:**

3. **Non-generic `IO`, expected.** The value-unit reading of `T` is not real — a
   handle can be read as bytes or as runes, which is a property of the
   *operation*. The capability reading (`IO<Read>` versus `IO<Write>`) is real
   but collides with the absence of subtyping: a function taking `IO<Write>`
   could not accept an `IO<ReadWrite>` without adding subtyping or duplicating
   every API. Reopen only if a third interpretation appears that changes
   representation or checking without requiring subtyping.
4. **One type or several**, per the capability table. Without interfaces this is
   a choice between a run-time trap on `seek` and a larger type surface. It is a
   language-surface decision and should be taken deliberately.

**Ordinary design work, once the above are answered:**

5. How are owned and borrowed handles distinguished? The standard handles are
   borrowed and must not be closable by an ordinary `close`.
6. How does a read distinguish end-of-input, a partial transfer, and an Error?
   Partial is not a failure and must not be reported as one.

   **The language may already answer this.** `EoS` is a builtin zero-state
   value (`reference.md:394`) and `Channel<T>.receive() -> T | EoS`
   (`:917`) is the established precedent for a source that has run dry — a
   drained receive returns `eos` rather than an error. `Size | EoS | Error`
   applies the same shape: the count is the success value, `eos` is
   end-of-input, `Error` is failure, and a short count is an ordinary `Size`
   that needs no separate signal. Verified to compile, above.

   This is evidence, not a decision. It settles nothing about whether the
   operation is blocking, which is still the prerequisite. But it means the
   answer is likely a spelling the language already has rather than a new
   error enum, and any proposal for one should have to argue against this
   precedent first.
7. What opens a handle? Path types and open modes belong to the filesystem
   specification; this RFC consumes the handle it produces.

## Readiness

**Not implementation-ready, and not merely under-specified: it is blocked.**

Settled: the scope boundary (byte streams on a handle, with filesystem and
sockets excluded), the primitive operation set, the capability table, and the
pollable/non-pollable split.

Blocked: everything downstream of the blocking-syscall boundary, which is
unowned. The representation question rides on it, and the surface rides on the
representation.

The next action for this RFC is not to write more of it. It is to settle
RFC 0091.
