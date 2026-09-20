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

**V1 is the leak report alone, built on mimalloc's exported block
enumeration, with no per-allocation attribution and no change to generated
C.** Double-free detection, quarantine, and stale-access detection are
separate tracks that do not gate it; source attribution is an upgrade path
with a known cost. The one problem that must be solved before V1 can be a CI
gate is telling a genuine leak apart from the runtime's own process-lifetime
allocations — see Baseline separation.

The original RFC also proposed consuming method receivers. That direction is
withdrawn: it requires affine moves and automatic-drop disarming, and conflicts
with Hexal's implemented struct-only value receivers and explicit-cleanup model.

This RFC is a runtime-debugging proposal, not a static leak-proofing proposal.
The checker may reject locally provable cleanup misuse, but this facility owns
physical allocations that survive process shutdown, including allocations that
escaped through calls or containers.

The debug backend may also maintain short-lived allocation state for live,
freed, and quarantined blocks. That state is for diagnostics only; it is not a
language-visible ownership, lifetime, or provenance system.

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
- Making allocation tracking or freed-state checks an always-on runtime cost.
- Claiming an exact Hexal source site when the runtime received no call-site
  metadata.

## Verified facts about the vendored allocator

Probed against `lib/*/mimalloc_v3.5.1` on 2026-09-20, so the sweep below
starts from evidence rather than from assumption:

- `mi_heap_visit_blocks`, `mi_heap_visit_abandoned_blocks`,
  `mi_debug_show_arenas`, `mi_check_owned`, and the `mi_stats_print` family
  are all **exported from the shipped release archive**. Enumerating live
  blocks at exit therefore needs no rebuilt runtime pack.
- mimalloc captures **no per-allocation call stack**. There is no `backtrace`,
  `CaptureStackBackTrace`, or `RtlCaptureStack` reference anywhere in the
  vendored headers or archive. This makes the two Decided items below
  mutually exclusive as written: mimalloc's facilities give enumeration with
  no attribution, and attribution requires the Hexal-owned record layer.
  Resolve that before implementation, do not discover it during.
- The existing `MIMALLOC_SHOW_STATS=1` usage in
  `compiler/tests/c23validation/interop_measurement_test.go` yields aggregate
  peak/total counters only. It is a measurement, not a leak tracker, and
  cannot become one.

## Tracking boundary

- V1 should track physical allocation and release calls made through the shared
  mimalloc-backed runtime.
- **Two shipped allocation paths do not reach that boundary**, and the sweep
  must classify both rather than assume coverage:
  - `generator/packages/print.c` calls libc `malloc`, `realloc`, and `free`
    directly for its output buffer — outside mimalloc and outside
    `hex_heap_*`.
  - `generator/packages/concurrency.c` allocates every Task fiber stack with
    `mmap`/`munmap` — outside mimalloc and outside `hex_heap_*`. This is the
    same fiber design that keeps RFC 0185 deferred, so LSan does not cover it
    either; the "do not duplicate what ASan/LSan already covers" instruction
    below has no force for Task stacks, because nothing covers them.
- libuv allocates through mimalloc via `uv_replace_allocator` in
  `generator/packages/runtime.c`, so its blocks are visible to a mimalloc
  facility but invisible to a Hexal-owned table at `hex_heap_*`. Which
  mechanism is chosen changes what the report contains; say so in the report
  contract rather than leaving it to the implementation.
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
- A debug allocation record may contain an allocation identity, size, native
  stack, live/freed state, and quarantine state. It must not be exposed to
  Hexal code or used to change ordinary program semantics.

## Baseline separation

This is the decisive design problem and it has no answer yet. Everything else
in this RFC is tractable once it is settled.

A live-block enumeration at process exit reports **every** block still held,
which includes the runtime's own process-lifetime allocations: the libuv loop
and its handles (routed into mimalloc by `uv_replace_allocator`), handle-table
chunks, interned string storage, and anything a component allocates once and
never releases because the process is ending. None of those is a leak. Without
a way to separate them from genuine ones, the report is a false-positive flood
and can never be the deterministic CI failure this RFC's first Goal promises.

The candidate mechanisms, none yet selected:

1. **Baseline snapshot.** Record live blocks after runtime initialization and
   subtract. Cheap, but wrong for anything the runtime allocates lazily after
   the snapshot, which is most of libuv.
2. **Ownership tag.** Route runtime-internal allocations through a distinct
   entry point so the tracker can exclude them by construction. Precise, but
   touches every runtime component and grows the tracked boundary.
3. **Orderly shutdown.** Require the runtime to release everything it owns
   before the report runs, so anything left is by definition a program leak.
   Cleanest contract, largest amount of new shutdown work, and it interacts
   with detached Tasks that may still be running.

A promoted spec must pick one and say what the report does with the blocks the
chosen mechanism cannot classify.

## Separable tracks

Leak reporting, double-free detection, and stale-access detection are three
features with three different implementation requirements, and bundling them
is why this RFC reads as larger than its first useful increment. They should
graduate independently:

| Track | Needs | Status here |
| --- | --- | --- |
| Leak report | live-block enumeration + baseline separation | **V1** |
| Source attribution | Hexal-owned record layer; changes generated C | after V1 |
| Double-free detection | a freed-state record per allocation identity | after V1 |
| Stale-access (use-after-free) detection | page protection, shadow memory, or access instrumentation | not this RFC |

The third row is the one to be careful about. **Quarantine alone does not
detect use-after-free.** Withholding a freed block from reuse makes a stale
read return recognizable bytes instead of a live object; it does not trap,
because the page is still mapped and writable. Detecting the access itself
requires `mprotect`-style page protection, ASan shadow memory, or
instrumented loads and stores — none of which this RFC proposes. The
Diagnostics section's "access to a quarantined allocation may trap or report"
overstates what quarantine buys and should be read as aspirational until one
of those mechanisms is selected.

## Source attribution

**V1 has none.** It reports allocation size and count, with no per-allocation
location of any kind.

That is a direct consequence of selecting mimalloc's own facilities: the
vendored build captures no per-allocation call stack, so there is nothing to
attribute with. The report says how much leaked, not where it was allocated.
That is a real limitation and the first thing a user will ask for; it is
accepted for V1 because it ships without changing a line of generated C, and
because "N blocks, M bytes still live at exit, in CI, deterministically" is
already the difference between catching a leak and not.

The implementation must not print a Hexal source location, a generated-C
coordinate, or a symbol name it did not resolve.

### Why attribution is available later, and what it costs

Recorded so the upgrade path is not rediscovered from scratch.

Attribution requires the Hexal-owned record layer, which captures a return
address at allocation and resolves it against the target's debug symbols. The
resolution lands on Hexal source, not on generated C, because `#line` rewrites
the debug information for the generated call site and user allocations are
emitted directly under it:

```c
#line 2 "app.hex"
    int32_t *const hex_v_p = hex_heap_allocate_Int32(hex_v_h, 7);
```

An earlier revision of this section argued the opposite — that `#line` cannot
yield Hexal locations because it passes no filename to `hex_heap_allocate`.
The premise is right and the conclusion does not follow: nothing needs to be
passed, because the debug information already carries it.

Three costs, all of which is why V1 does not take this route:

- It changes `compiler/generator/packages/heap.c`, so generated C moves and
  the snippet manifest moves with it. See Selection boundary.
- It holds only where debug information survives. Closed RFC 0187's release
  mode strips it (`-g0`, linker `-s`), so it would tie tracking to debug mode.
- The immediate frame is the per-type shim in the module header
  (`hex_heap_allocate_Int32` in `modules/app.h`), which carries no `#line`.
  The Hexal location is its *caller's* frame, so the resolver must walk past
  the shim rather than reporting the first frame it finds.

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
- **The fallback conflicts with a closed invariant, and the conflict is not
  yet resolved.** `hex_heap_allocate` lives in `hexal/heap.c`, which the
  compiler *generates* from `compiler/generator/packages/heap.c`. A
  Hexal-owned record layer at that boundary therefore changes generated C and
  moves snippet-manifest hashes — colliding with closed RFC 0187's
  "byte-identical generated C in every mode" and with this RFC's own goal of
  preserving generated C. A mimalloc-only V1 avoids the collision entirely
  because nothing generated changes. If the Hexal-owned table is selected
  instead, the spec must say which of the two invariants gives way, and the
  likely answer is that tracking is a driver option rather than a build mode,
  so RFC 0187's mode rule does not reach it. Say it explicitly either way.

## Diagnostics

- A clean run emits no tracking report.
- A leaking run lists only still-live physical allocations under the selected
  attribution contract.
- Internal tracker corruption or an impossible release is a runtime-tooling
  failure, distinct from a Hexal source diagnostic.
- The report format, output stream, ordering key, and exit status are part of
  the implementation contract; they are intentionally unresolved below. The
  stream must be stderr: closed RFC 0184 owns stdout as atomic print
  transactions, and interleaving a tracker report into it would corrupt them.
- When debug freed-state checking is enabled, a second release of a known
  allocation may trap or report a runtime-tool diagnostic. Unknown or foreign
  addresses remain outside this guarantee. Access to a quarantined allocation
  is **not** diagnosed by quarantine alone — see Separable tracks.
- **A trap is not available to a mode.** Closed RFC 0187 requires stdout,
  stderr, exit status, and every trap message to be identical across build
  modes. A tracking build that traps where an ordinary build does not is only
  consistent with that if tracking is a driver option outside the mode
  system. This RFC currently asserts both "build-mode semantics remain
  identical" and "may trap", which cannot both hold until that is settled.
  Note also that RFC 0187 already uses "freed-state check" for the runtime
  handle-generation checks it calls language semantics; this RFC's
  allocator-level use of the same phrase needs a distinct name.

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
- Decide whether the selected mimalloc facility can provide freed-state,
  quarantine, and invalid-release diagnostics. If not, add the smallest
  Hexal-owned debug record layer at the shared runtime boundary.

## Acceptance sketch

Non-exhaustive. A promoted implementation spec must replace this with an
exhaustive Validation section.

- One deliberately leaked physical allocation is reported exactly once.
- A released allocation is absent, and a clean program emits nothing.
- A known double release is diagnosed when debug freed-state checking is
  enabled.
- A known quarantined allocation access is diagnosed when quarantine checking
  is enabled; unknown and foreign addresses remain undecided.
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

- V1 tracks physical allocations only. Logical Stash/Pool events are a
  separate future extension.
- V1 is the leak report alone. Double-free detection and stale-access
  detection are separate tracks (see Separable tracks) and do not gate it.
- **V1 uses mimalloc's own facilities and accepts no attribution.**
  `mi_heap_visit_blocks` and the abandoned-block walk are exported from the
  shipped archive, so enumeration needs no rebuilt runtime pack, no
  Hexal-owned record layer, and no change to generated C. The report is size
  and count only. The incompatible pairing in the previous revision — mimalloc
  facilities *and* native call-stack attribution — is resolved in favor of
  shipping something, since the vendored allocator captures no stacks.
- The Hexal-owned table is not the fallback for V1; it is the upgrade path for
  attribution, and it carries the generated-C cost described in Selection
  boundary. Nothing in V1 should be built in a way that blocks it.
- Report output goes to stderr.

### Remaining questions

1. Baseline separation: which of the three mechanisms, and what the report
   does with blocks it cannot classify. This blocks the CI contract.
2. The exact driver option that enables tracking; it must remain outside the
   source language and core compiler `Project`.
3. Whether any leak makes the process exit nonzero. Nonzero is recommended for
   a CI-oriented facility. The spec must also say what happens after a runtime
   trap or abort, and what a still-running detached Task's allocations count
   as at exit.
4. Cross-thread enumeration. mimalloc keeps per-thread heaps, and Hexal Tasks
   are fibers that migrate across a worker pool, so one Task's allocations can
   land on any worker's heap. `mi_heap_visit_abandoned_blocks` and the subproc
   stats entry points exist; the report contract must say which heaps it walks
   and in what order, since "Report ordering is deterministic" depends on it.
5. Classification for `mi_realloc`, aligned allocation (`allocate_aligned`
   reaches `mi_malloc_aligned` directly), zero-size allocation, failed
   allocation, and allocator mismatch. None is implied by tracking `malloc`
   and `free`.
