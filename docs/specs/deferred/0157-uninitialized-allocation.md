# RFC 0157: Explicit Uninitialized Allocation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: deferred until a
  measured workload shows initialized allocation is a material cost
- Created: 2026-09-10
- Updated: 2026-09-15
- Depends on: RFC 0155 (`unsafe do ... end`)
- Coordinates with: RFC 0156 (raw pointer traversal), RFC 0187 (build modes do
  not change generated C), and RFC 0189 (independent aligned allocation)
- Does not update `docs/reference.md`

## Summary

Consider an explicitly unsafe allocation that leaves one T object
uninitialized:

```text
Heap.allocate_uninit<T>() -> Ptr<mut T>
```

Initialized allocation remains the default. This proposal no longer includes
alignment control; RFC 0189 owns that independently useful, safe operation.

## Motivation gate

Do not schedule this RFC from intuition alone. Before promotion, a benchmark
must show a real Hexal workload materially paying for dummy initialization
immediately overwritten by the program. Record allocation size, repetition,
generated C, compiler/toolchain, target, and measured time.

If ordinary initialized allocation, List growth, or a typed foreign API meets
the workload without material overhead, discard this RFC.

## Candidate contract

- T has the same complete finite allocation eligibility as `Heap.allocate<T>`.
- The call requires active lexical unsafe permission.
- Allocation failure retains the existing allocation trap.
- The returned `Ptr<mut T>` is ordinary non-owning pointer state and is released
  explicitly through the matching Heap operation.
- The programmer asserts that every observed byte of T receives a valid value
  before any read, copy, comparison, print, call argument, return, or release
  path that semantically reads it.
- The compiler performs no field-level definite-initialization analysis.
- No build mode adds a poison fill. A recognizable byte pattern is neither
  initialization nor reliable detection and would change generated C.
- No implicit drop or destructor is introduced.

## Non-goals

- Aligned allocation; RFC 0189 owns it.
- Raw byte-count allocation, realloc, zeroed allocation, or arrays of
  uninitialized T.
- Flow-sensitive partial-initialization tracking.
- Debug allocation tracking or runtime provenance.
- Stash or Pool uninitialized variants.

## Questions before promotion

1. Which measured workload justifies the surface?
2. Is one uninitialized T sufficient, or does the demonstrated workload require
   a counted buffer? Do not add both speculatively.
3. Which operations are considered semantic reads for aggregate values, and
   can the contract be stated without implying checker guarantees that do not
   exist?

## Promotion requirements

A promoted RFC must replace this section with an exhaustive Validation section
and detailed implementation plan covering:

- exact unsafe and allocation diagnostics;
- complete type eligibility;
- write-before-read examples for scalar, struct, and collection-containing T;
- generated C using non-zeroing allocation with no poison fill;
- release behavior and existing local freed-state checks;
- manifest impact; and
- reference synchronization after explicit approval.
