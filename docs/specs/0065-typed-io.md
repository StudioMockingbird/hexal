# RFC 0065: Typed `IO<T>`

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; initial direction only
- Created: 2026-08-15
- Scope: future program-runtime input/output abstraction backed by C `FILE *`
- Depends on: RFC 0039 (C interop compiler core)
- Coordinates with: RFC 0055 (filesystem/build driver), RFC 0064 (removal of
  current File builtins), target profiles, `docs/reference.md`, and
  `docs/status.md`

## Summary

Introduce a future `IO<T>` type for program-runtime input/output operations.
Its C representation should lower directly to `FILE *`.

This RFC currently records direction only. It does not yet define the meaning
of T, constructors, operations, ownership, errors, or standard handles.

## Goals

- One cohesive input/output abstraction rather than compiler-special-cased
  `File`, `FileMode`, and `Stdio` concepts.
- Direct, readable C23 lowering centered on `FILE *`.
- No hidden filesystem access by the core compiler.
- Compatibility with generated bindings and C libraries through RFC 0039.
- Explicit ownership and failure contracts.
- A small operation surface with one obvious way to perform each supported I/O
  action.
- No lazy collection-transformation API.

## Compiler boundary

`IO<T>` concerns runtime behavior of the compiled Hexal program. It does not
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

## Relationship to removed concepts

RFC 0064 removes the current compiler-owned `File`, `FileMode`, and `Stdio`
surface. `IO<T>` is a new design, not a compatibility rename, and need not
preserve those APIs or policies.

`IO<T>` is unrelated to the removed lazy `Stream<T>` collection pipeline. It
does not imply producer State, List sources, `filter`, `map`, `take`, adapter
ownership, or generic `for` iteration.

## Required design areas

### Meaning of T

Define what the type parameter represents. Candidate interpretations include:

- the value unit read or written, such as Byte or text;
- an I/O capability or mode marker; or
- another compile-time protocol.

Do not expose `IO<T>` until T changes valid operations, representation, or
static checking in a useful and coherent way. If T adds no semantic value, use
a non-generic `IO` type instead.

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

Select the minimum coherent set from:

- read bytes;
- read text;
- write bytes;
- write text;
- flush;
- close;
- end-of-input detection;
- seek/tell; and
- buffering configuration.

Do not add an operation merely because `FILE *` exposes one. Each operation
needs an exact Hexal type, ownership effect, failure result, and C lowering.

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
- Giving the core compiler filesystem access.
- Defining project discovery, artifact writing, compilation, or linking.
- Committing to async I/O, sockets, networking, or filesystem path types in the
  initial version.
- Treating C `FILE *` as a typed generic value without defining what T means.

## Open decisions

1. What does T represent, and is generic `IO<T>` better than non-generic `IO`?
2. Is IO a core-library abstraction over RFC 0039, a protected builtin, or a
   combination with the smallest possible compiler primitive?
3. How are owned and borrowed `FILE *` values distinguished while preserving
   direct pointer representation?
4. What is the minimal read/write/flush/close surface?
5. How do reads distinguish EoS, partial success, and Error?
6. What path and mode types open an IO value?
7. How do blocking `FILE *` calls interact with M:N scheduler workers?

## Readiness

Not implementation-ready. The `FILE *` representation direction and compiler
boundary are established; the semantic surface, ownership model, generic
parameter, error model, and scheduler interaction remain open.
