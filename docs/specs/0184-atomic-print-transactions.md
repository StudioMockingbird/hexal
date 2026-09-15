# RFC 0184: Atomic Print Transactions

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design and execution plan settled,
  implementation not started
- Created: 2026-09-14
- Updated: 2026-09-15
- Scope: make each source `print(...)` call one buffered, serialized standard-
  output transaction
- Depends on: the current print, IO, native-threading, Task-worker, and
  demand-driven component contracts
- Coordinates with: closed RFC 0174's implemented Windows console text sink,
  RFC 0182 for process return, and RFC 0183 Track 5 for print-helper demand
- Does not add: source syntax, user-visible buffering controls, automatic
  flushing, a logging API, or libuv in print-only programs

## Problem

The reference requires one complete `print(...)` call to be atomic relative to
other print and standard-output writes. The runtime does not implement that
contract. Print helpers currently write each fragment immediately through
`hex_io_write_all`. Closed RFC 0174 added correct Windows-console Unicode
conversion, but classification, conversion, and `WriteConsoleW` submission are
still performed per fragment rather than per source print call.

For example, printing an aggregate emits separate writes for its opening
delimiter, members, separators, and closing delimiter. Concurrent Tasks can
therefore interleave fragments, and a Task pays one `uv_queue_work` round trip
per fragment.

This RFC restores the existing contract. It does not introduce a new language
feature.

## Decision

Each source print call:

1. evaluates every argument exactly once in source order;
2. initializes one call-local print builder;
3. appends every formatted fragment to that builder;
4. commits the completed bytes to stdout through one write-all request; and
5. releases any grown builder storage before continuing.

No fragment reaches stdout before commit. Formatting failure or allocation
failure emits no prefix of that print call.

`IO.write` remains byte-exact and does not use the formatting builder. A write
to the standard-output descriptor and a print commit use the same native sink
serialization, so neither can split the other. Writes to unrelated descriptors
do not take the stdout lock.

There is no persistent stdout buffer. A successful print commit has already
submitted the complete call; process return has no compiler-owned print buffer
to flush.

## Builder representation

- `hex_print_buffer` is private generated-runtime state.
- It owns `data`, `length`, and `capacity`, plus 256 bytes of inline storage.
- Initialization points `data` at the inline storage and allocates nothing.
- Growth uses checked arithmetic and geometric capacity growth. The first
  growth allocates with the bundled C library's `malloc` and copies the inline
  bytes; later growth uses `realloc`, and destroy uses `free`. It does not
  select Heap, mimalloc, or libuv.
- A failed growth traps with exact text
  `[Runtime Error] print buffer allocation failed`.
- Appending zero bytes succeeds without allocation.
- Destroy frees only grown storage and is safe after a successful or failed
  commit return. A runtime trap may terminate without cleanup.
- One source call buffers its complete formatted result. Peak temporary memory
  is therefore proportional to that call's output size; no streaming threshold
  or fixed maximum is added in this RFC.

The fixed inline size is an implementation contract for generated-code tests,
not a source-visible capacity guarantee.

## Helper contract

- Every generated `hex_print_*` formatter receives a
  `hex_print_buffer *` and appends only to it.
- Nested aggregate, ADT, union, String, Strand, Rune, Error, numeric, and quoted
  formatting share the same builder passed by the outer print call.
- Helpers never call `hex_io_write_all`, `WriteFile`, `write`, or a terminal
  sink directly.
- UTF-8 bytes remain unchanged. Move closed RFC 0174's implemented console
  classification and conversion behind commit so it receives the complete
  buffer once; do not alter the Terminal source API.
- Deferred print captures its arguments at registration as today, then creates
  and commits one builder when the defer executes.

## Serialization

- Without Task/event support, Hexal source execution is single-threaded and the
  direct stdout path adds no lock or libuv dependency.
- With Task/event support, the completed call becomes one native worker job.
  The worker acquires the existing generated native-mutex abstraction around
  the complete stdout write-all loop and releases it before publishing
  completion.
- On an attached Windows console, that one job performs classification,
  UTF-8-to-UTF-16 conversion, and every `WriteConsoleW` chunk; conversion
  chunks do not become separate jobs.
- A concurrent `IO.write` targets stdout when its resolved native descriptor or
  handle equals the process standard-output descriptor or handle at call time;
  that write uses the same critical section. No source-level IO identity or
  cached construction-time flag decides this.
- The mutex is initialized by the existing native bootstrap before any Task can
  submit output and is destroyed only after no native output job can remain.
- A scheduler worker never blocks on the stdout mutex: it parks after submitting
  the one native job. Contention occurs only among native worker jobs.
- Short native writes remain inside the locked write-all loop.
- The Windows console branch loops `WriteConsoleW` until every converted UTF-16
  unit is written. Zero progress or native failure retains the existing output
  trap; a short successful write is not itself failure.

## Failure behavior

- Formatting/building failure emits no part of the call.
- Native output failure after commit begins may have written a prefix; the
  runtime traps with the existing
  `[Runtime Error] standard output write failed` diagnostic.
- A failed print never retries the complete call, because doing so could
  duplicate an already-written prefix.
- A trap's process status remains unspecified under RFC 0182.

## Required sweep

- direct descriptor writes in `compiler/generator/packages/print.c`;
- per-fragment `GetStdHandle`/`GetConsoleMode`, UTF-8 conversion, and
  `WriteConsoleW` worker submissions left by closed RFC 0174;
- generated aggregate and Error helpers that call fragment sinks;
- `renderPrintStatement` and deferred-print lowering;
- repeated Task-worker submissions per logical print call;
- stdout writes that bypass the shared stdout serialization path;
- false shutdown-flush assumptions in active specs and, after explicit user
  approval, the normative reference; and
- manifest entries for programs that select print.

## Detailed implementation plan

### Phase 1: reproduce and freeze

1. Add generated-text assertions showing that an aggregate print currently
   performs multiple sink calls.
2. Add a repeated two-Task fixture that can detect fragment interleaving.
3. Record print-related manifest hashes and the current print-only dependency
   set.

### Phase 2: builder runtime

1. Add the private builder declaration and append/growth/destroy operations to
   `hexal/print.h` and `hexal/print.c`.
2. Implement inline storage, checked growth, exact allocation failure, and
   zero-length append.
3. Convert scalar, text, quote, Error, and recursive aggregate helpers to accept
   and append to one builder.

### Phase 3: source-call lowering

1. Preserve current argument evaluation temporaries and their source order.
2. Emit one builder initialization after argument evaluation, pass it through
   all argument helpers, commit once, and destroy it.
3. Apply the identical transaction shape to deferred print at defer execution
   time without changing capture timing.
4. Keep non-print expressions and source semantics byte-identical.

### Phase 4: stdout serialization

1. Give stdout one write-all entry that both print commits and byte-exact
   `IO.write` use when their descriptor is stdout.
2. Compare the resolved native descriptor or handle with the current process
   standard-output descriptor or handle before choosing that entry.
3. In Task/event builds, initialize one native stdout mutex and hold it only in
   the native worker around the complete write-all loop.
4. Retain the direct, dependency-free path when Task/event support is absent.
5. Verify short writes, failure publication, shutdown ownership, and absence of
   scheduler-worker blocking.

### Phase 5: coordination and conformance

1. Relocate closed RFC 0174's implemented terminal detection and Unicode
   conversion to consume the completed buffer once, including a
   `WriteConsoleW` write-all loop.
2. Make RFC 0183 Track 5 discover the revised print helpers rather than the
   deleted fragment-writing forms.
3. Add focused generator and public integration tests for every Validation
   item.
4. Run ordinary tests, tagged C23 compilation/runtime fixtures, and the snippet
   manifest; inspect every changed print artifact.
5. Update `docs/reference.md` only after behavior stabilizes and only with
   explicit user approval.

## Validation

This list is exhaustive:

- scalar, multi-argument, String, Strand, Rune, Error, object, ADT, union,
  Array, Slice, List, and Dict printing;
- argument evaluation exactly once in source order;
- one builder and one commit per direct and deferred source print call;
- 0, 255, 256, 257, and large formatted byte lengths;
- checked growth, exact allocation-failure trap, and no partial output before
  commit;
- short-write completion and existing output-failure trap behavior;
- one Windows console classification and conversion sequence per complete
  source print call, including short `WriteConsoleW` completion;
- repeated concurrent Task prints never interleave within one call;
- print and `IO.write` to stdout never interleave within either call;
- writes to unrelated descriptors do not take stdout serialization;
- stdout recognition follows native descriptor/handle equality at call time;
- one Task worker submission per print call, including aggregate printing;
- peak builder memory is permitted to grow with the complete formatted call;
- print-only non-Task programs add no libuv, event, scheduler, mimalloc, or
  native-mutex dependency;
- no persistent print buffer and no shutdown flush operation;
- deterministic generated output and manifest movement confined to programs
  selecting print; and
- RFC 0174 terminal conversion receives one complete UTF-8 call while
  `IO.write` remains byte-exact.

## Reference synchronization

Do not edit `docs/reference.md` from this specification. Approved
implementation preserves whole-call atomicity and no per-call flush, but must
remove the unsupported statement that shutdown flushes a compiler-owned stdout
buffer. Apply that correction only after behavior stabilizes and with explicit
user approval.
