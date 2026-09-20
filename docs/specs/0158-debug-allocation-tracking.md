# RFC 0158: Debug Allocation Tracking

- Kind: Tooling Proposal
- Status: Open Discussion; not scheduled. Design state: consuming receivers
  withdrawn; allocation tracking retained as an independent idea
- Created: 2026-09-10
- Updated: 2026-09-20
- Coordinates with: the mimalloc-backed runtime, RFC 0183 (runtime validation
  and sanitizer coverage), RFC 0185 (ASan fiber coverage), and RFC 0187 (driver
  build modes)
- Does not update `docs/reference.md`: this is tooling, not language semantics

## Summary

Provide an opt-in development backend that reports physical allocations still
live at process exit. It adds no keyword, type, method, move, ownership,
automatic cleanup, or generated-program semantic change.

The original RFC also proposed consuming method receivers. That direction is
withdrawn: it requires affine moves and automatic-drop disarming, and conflicts
with Hexal's implemented struct-only value receivers and explicit-cleanup model.

This RFC is a runtime-debugging proposal, not a static leak-proofing proposal.
The checker may reject locally provable cleanup misuse, but this facility owns
physical allocations that survive process shutdown, including allocations that
escaped through calls or containers.

## Goals

- Turn physical memory leaks exercised by runtime tests into a deterministic CI
  failure.
- Reuse mimalloc facilities where they meet the required contract before
  creating a Hexal-owned tracking table.
- Keep ordinary builds and pure-Go tests unchanged.
- Preserve generated C when selection can remain a driver/backend concern.
- Work correctly when Tasks allocate concurrently.

## Non-goals

- Language-level ownership, affinity, moves, consuming receivers, destructors,
  or exactly-once cleanup.
- Proving that every File, socket, process, or foreign handle was closed.
- Treating each Stash value or Pool slot as a physical heap allocation.
- Production profiling, retain counting, cycle collection, or automatic leak
  repair.
- Claiming an exact Hexal source site when the runtime received no call-site
  metadata.

## Tracking boundary

- V1 should track physical allocation and release calls made through the shared
  mimalloc-backed runtime.
- A Stash block and Pool backing region are physical allocations. Individual
  values carved from those regions are not independently visible to a heap
  tracker.
- Logical Stash/Pool events require explicit component instrumentation and are
  a separate extension.
- A memory report may reveal memory retained by an unclosed resource, but it is
  not a resource-lifecycle proof.
- The tracker or selected mimalloc facility must be thread-safe for ordinary
  Task programs. A single-thread-only tracker must say so and is not the
  recommended general implementation.
- `realloc`, aligned allocation, zero-size allocation, failed allocation, and
  allocator-mismatch behavior must each be classified before promotion. They
  are not implied by tracking `malloc` and `free` alone.
- Foreign allocations are excluded unless they cross a Hexal-owned release
  boundary. The report must not claim to prove ownership of arbitrary C memory.

## Source attribution

`#line` changes compiler and debug information; it does not pass a filename and
line to `hex_heap_allocate`. V1 uses native call-stack/debug-symbol
attribution. Exact Hexal source attribution is deferred because it would
change generated call sites and manifest hashes. The alternatives are:

1. explicit source arguments or an allocation macro at every generated call;
2. return-address capture plus target debug-symbol resolution; or
3. a weaker report containing allocation size, count, and native call stack.

The first option changes generated call sites and manifest hashes. The third is
the smallest initial contract. V1 selects the third option. The implementation
must not promise Hexal source-level locations merely because generated C
contains `#line` directives.

## Selection boundary

- Tracking is a driver/backend development option, not a source-language or
  core `Project` setting.
- The workbench may request that option through the driver, but does not own a
  separate tracking contract.
- Disabled tracking must not add a branch to every generated allocation call.
- Build-mode semantics remain identical. Tracking observes leaks; it does not
  change allocation, cleanup, trap, stdout, or successful-program behavior.
- The eventual option must be driver-owned and must state whether it changes
  generated C, the runtime pack, the build cache key, or only the linked
  runtime. No option is added to the language or core compiler `Project` by
  this RFC.

## Diagnostics

- A clean run emits no tracking report.
- A leaking run lists only still-live physical allocations under the selected
  attribution contract.
- Internal tracker corruption or an impossible release is a runtime-tooling
  failure, distinct from a Hexal source diagnostic.
- The report format, output stream, ordering key, and exit status are part of
  the implementation contract; they are intentionally unresolved below.

## Required sweep before promotion

- Inventory every runtime allocation path and verify whether it reaches the
  shared mimalloc boundary.
- Inventory mimalloc's available tracking, statistics, heap-visit, and report
  facilities in the exact vendored version and selected build configuration.
- Compare the resulting coverage with RFC 0183's sanitizer lanes; do not add a
  second mechanism for a case ASan/LSan already covers adequately.
- Classify Stash, Pool, libuv, foreign-library, and direct dependency
  allocations as included or excluded.
- Define concurrency, shutdown order, report ordering, and process-exit
  behavior before adding tests.

## Acceptance sketch

Non-exhaustive. A promoted implementation spec must replace this with an
exhaustive Validation section.

- One deliberately leaked physical allocation is reported exactly once.
- A released allocation is absent, and a clean program emits nothing.
- Concurrent Task allocations and releases do not race or corrupt tracking.
- Report ordering is deterministic.
- Tracking disabled preserves the existing generated C and runtime behavior.
- Ordinary pure-Go tests invoke no external process or tracker.

## Detailed investigation plan

1. Establish what the vendored mimalloc version can report without changing
   generated calls.
2. Compare that output with the minimum deterministic CI contract.
3. If mimalloc is sufficient, specify only its build/driver integration.
4. Otherwise design the smallest thread-safe table at the shared physical
   allocation boundary.
5. Settle attribution, driver selection, and exit behavior.
6. Write a new exhaustive Validation section from those decisions before any
   implementation begins.

## Decisions and remaining questions

### Decided

- V1 uses mimalloc's existing reporting facilities first. A Hexal-owned table
  is the fallback only if the vendored version cannot meet the contract.
- V1 tracks physical allocations only. Logical Stash/Pool events are a
  separate future extension.
- V1 uses native call-stack/debug-symbol attribution. Exact Hexal source
  arguments are deferred because they would change generated C.

### Remaining questions

1. The exact driver option that enables tracking; it must remain outside the
   source language and core compiler `Project`.
2. Whether any leak makes the process exit nonzero. Nonzero is recommended for
   a CI-oriented facility.
