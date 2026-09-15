# RFC 0158: Debug Allocation Tracking

- Kind: Tooling Proposal
- Status: Open Discussion; not scheduled. Design state: consuming receivers
  withdrawn; allocation tracking retained as an independent idea with the
  decisions below unresolved
- Created: 2026-09-10
- Updated: 2026-09-15
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

## Source attribution

`#line` changes compiler and debug information; it does not pass a filename and
line to `hex_heap_allocate`. Exact source attribution therefore requires one of:

1. explicit source arguments or an allocation macro at every generated call;
2. return-address capture plus target debug-symbol resolution; or
3. a weaker report containing allocation size, count, and native call stack.

The first option changes generated call sites and manifest hashes. The third is
the smallest initial contract. The implementation must not promise source-level
locations merely because generated C contains `#line` directives.

## Selection boundary

- Tracking is a driver/backend development option, not a source-language or
  core `Project` setting.
- The workbench may request that option through the driver, but does not own a
  separate tracking contract.
- Disabled tracking must not add a branch to every generated allocation call.
- Build-mode semantics remain identical. Tracking observes leaks; it does not
  change allocation, cleanup, trap, stdout, or successful-program behavior.

## Diagnostics

- A clean run emits no tracking report.
- A leaking run lists only still-live physical allocations under the selected
  attribution contract.
- Internal tracker corruption or an impossible release is a runtime-tooling
  failure, distinct from a Hexal source diagnostic.

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

## Open questions

1. Whether mimalloc's existing diagnostics satisfy the needed deterministic
   leak report or Hexal needs its own records.
2. Whether v1 tracks only physical allocations (recommended) or also adds
   logical Stash/Pool events.
3. Whether native/debug-symbol attribution is sufficient initially
   (recommended) or exact Hexal source arguments justify generated-C churn.
4. The exact driver option that enables tracking; it must remain outside the
   source language and core compiler `Project`.
5. Whether any leak makes the process exit nonzero. Nonzero is recommended for
   a CI-oriented facility.
