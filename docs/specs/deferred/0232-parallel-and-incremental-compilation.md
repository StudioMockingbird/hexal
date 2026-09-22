# RFC 0232: Parallel and Incremental Compilation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. No design work has begun. This RFC exists
  to hold a question that was blocking RFC 0221's completion, not to propose
  an implementation
- Created: 2026-09-22
- Origin: split out of RFC 0221 Track 5, which deferred `golang.org/x/sync`
  until a concurrency specification existed — and then kept a validation item
  referring to that specification, so RFC 0221 could not be marked complete
  for a reason unrelated to any of its own work
- Depends on: nothing yet. A design here depends on an ownership and
  determinism model that does not exist
- Coordinates with: RFC 0221 (which no longer carries this), RFC 0230 (arc
  foundations)
- Does not authorize: adding `golang.org/x/sync`, parallel module checking,
  specialization deduplication, or any persistent cache

## Why this is deferred rather than designed

The compiler is sequential today and correct. Nothing measured says it is too
slow, and adding concurrency before a determinism model exists would make
races and diagnostic ordering harder to reason about than they are now — for
a benefit nobody has quantified.

Deferring is not indecision. The compiler's output is deterministic for equal
inputs, and that property is worth more than wall-clock time until a real
workload says otherwise.

## The question that has to be answered first

**What measurable threshold justifies the dependency?**

There is no threshold today because there is no measurement. Before any
design:

- record compile wall-clock across the catalog snippets and a realistic
  multi-module program;
- identify where the time actually goes — module resolution, checking,
  specialization, generation, or the external C compile;
- establish what share is even parallelizable, given that the external C
  compile may dominate.

A dependency added to reduce a share of time that turns out to be small is
pure cost. If the external toolchain dominates, the answer is that this RFC
stays deferred permanently.

## What a design would have to settle

Recorded so the eventual spec does not start from nothing:

- which compiler state is immutable and shareable, and which is per-module or
  per-specialization;
- cancellation and failure propagation across workers;
- **deterministic diagnostic and artifact ordering**, which today comes free
  from sequential post-order traversal and would not;
- duplicate-work behavior when two modules request one specialization;
- cache lifetime and invalidation, if incremental compilation is in scope.

Candidate `golang.org/x/sync` uses, none justified yet: `errgroup` for bounded
concurrent work with cancellation, `singleflight` for deduplicating identical
in-flight work, and a semaphore for bounded workers.

## Incremental compilation needs its cache specified first

Answering the second question RFC 0221 carried: **yes.** Any `singleflight`
work presupposes a definition of what is cached, keyed by what, invalidated
when, and living where.

The core compiler is filesystem-free and must stay so, so a persistent cache
belongs to a driver layer, not to `compiler.Compile`. That boundary is a
constraint on the design, not a detail of it.

## Non-goals

- Adding concurrency to reduce wall-clock time in small builds.
- Any change to the string-in/string-out compiler boundary.
- Trading deterministic output for speed.
