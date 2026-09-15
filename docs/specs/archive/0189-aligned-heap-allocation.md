# RFC 0189: Aligned Heap Allocation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: the current `Heap.allocate<T>` and mimalloc backend contracts
- Coordinates with: deferred RFC 0157 (uninitialized allocation)
- Does not update `docs/reference.md`: synchronize only after implementation
  stabilizes and the user explicitly approves the reference edit

## Summary

Add initialized allocation with an explicit alignment:

```hexal
heap: Heap := Heap()
buffer: Ptr<mut Int32> := heap.allocate_aligned<Int32>(13, 64)
defer heap.free(buffer)
```

```text
Heap.allocate_aligned<T>(initial: T, alignment: Size) -> Ptr<mut T>
```

The operation is safe because the compiler and runtime can validate alignment.
It uses mimalloc's aligned allocation directly and ordinary `Heap.free` for
release.

## Goals

- Support over-aligned heap objects without uninitialized storage, pointer
  casts, or compiler-owned over-allocation headers.
- Keep the existing explicit initializer and explicit release model.
- Use C23 `alignof` and mimalloc's native aligned allocation facility.
- Reject invalid constant alignments early and trap invalid dynamic alignments
  before allocation.
- Add one operation and no new syntax, type, ownership rule, or unsafe gate.

## Non-goals

- Uninitialized allocation, zeroed allocation, realloc, or counted buffers.
- Type/member alignment annotations.
- Aligned Stash, Pool, List, Dict, String, Array, or Slice storage.
- Stack alignment controls.
- An alternative allocation backend or compiler-owned alignment shim.
- Automatic cleanup, ownership, or lifetime tracking.

## Semantics

- T must satisfy the same complete finite HeapAllocation eligibility as
  `Heap.allocate<T>`.
- `initial` must be exactly T under the same contextual typing and conversion
  rules as `Heap.allocate<T>`.
- `alignment` must be Size, non-zero, and a power of two. It is the requested
  minimum alignment.
- The effective allocation alignment is
  `max(alignment, align_of<T>())`; requesting less than T's natural alignment
  is valid and still returns storage correctly aligned for T.
- A compile-time-known invalid alignment is rejected by the checker.
- A dynamic invalid alignment traps before calling the allocator.
- Receiver, `initial`, and `alignment` are evaluated exactly once in normal
  call order: receiver, initializer, then alignment.
- The initializer is fully evaluated before a dynamic alignment trap. The
  allocation and store occur only after validation succeeds.
- Success returns `Ptr<mut T>` naming one initialized T object.
- Failure to allocate traps with the existing allocation-failure message.
- The result has the same explicit cleanup and local freed-state behavior as
  ordinary Heap allocation. `heap.free(pointer)` uses ordinary `mi_free` and
  receives no alignment argument.
- Requesting the natural alignment is valid but offers no semantic difference
  from `allocate<T>`.

## Diagnostics and traps

- Non-Size alignment uses the ordinary argument-type diagnostic.
- Invalid constant alignment reports Type Error:
  `alignment must be a non-zero power of two; got <value>`.
- Invalid dynamic alignment traps with Runtime Error:
  `invalid allocation alignment`.
- Invalid T and invalid initializer retain the existing
  `Heap.allocate<T>` diagnostics.
- Backend allocation failure retains Runtime Error: `allocation failed`.

## C23 and mimalloc lowering

The program-wide heap component adds one primitive:

```c
void *hex_heap_allocate_aligned(size_t size, size_t alignment,
                                size_t minimum_alignment);
```

Its contract is:

```c
if (alignment == 0 || (alignment & (alignment - 1)) != 0) {
    hex_runtime_trap("[Runtime Error] invalid allocation alignment\n");
}

size_t effective_alignment = alignment < minimum_alignment
    ? minimum_alignment
    : alignment;
void *pointer = mi_malloc_aligned(size, effective_alignment);
if (pointer == nullptr) {
    hex_runtime_trap("[Runtime Error] allocation failed\n");
}
return pointer;
```

- The typed module helper supplies `sizeof(T)` and `alignof(T)` directly.
- C23 `alignof(T)` remains in generated C because the target C compiler and ABI,
  not the host-neutral Go checker, own T's natural alignment.
- It stores the already-evaluated initializer into the returned T storage.
- `mi_free` releases the result; mimalloc requires no paired aligned-free API.
- No hidden header, manual rounding, extra allocation, memset, or platform
  branch is emitted.
- C23 `aligned_alloc` is not used: it would bypass Hexal's selected mimalloc
  backend and carries a size-multiple contract that `mi_malloc_aligned` does
  not require.
- The Heap component render model emits this declaration and definition only
  when reachable aligned allocation needs them. A program using only ordinary
  Heap operations does not inherit the new helper.

## Required sweep

Inventory and reconcile:

- compiler-owned Heap method recognition and generic T resolution;
- HeapAllocation placement/type eligibility and initializer checking;
- constant Size evaluation and power-of-two checks;
- generated Heap component declarations/definitions and mimalloc headers;
- typed allocation helpers, evaluation order, source mapping, and freed-state
  facts;
- generator demand discovery and component selection;
- allocation tests, snippets, trap fixtures, and manifest entries; and
- `docs/reference.md` Heap surface and C23 backend contract after explicit
  approval.

Do not add uninitialized allocation, a generic allocator interface, aligned
collection constructors, or fallback over-allocation while implementing this
RFC.

## Validation

This section is exhaustive.

- `Heap.allocate_aligned<Int32>(13, align_of<Int32>())` compiles and returns
  `Ptr<mut Int32>` whose dereference is 13.
- A valid larger constant alignment compiles.
- A valid dynamic alignment compiles and is checked before allocation.
- Constant zero and non-power-of-two alignments fail with the exact compile-time
  diagnostic.
- Dynamic zero and non-power-of-two alignments trap with the exact runtime
  diagnostic.
- Constant and dynamic requests below T's natural alignment succeed and use
  T's natural alignment as the effective minimum.
- A non-Size alignment retains the ordinary argument diagnostic.
- Incomplete, non-finite, Atomic, and otherwise non-HeapAllocation T retain the
  ordinary allocation-eligibility diagnostic.
- Missing or incompatible initializers retain the ordinary allocation
  diagnostics.
- Receiver, initializer, and alignment side effects occur exactly once and in
  source order; the initializer occurs before a dynamic alignment trap.
- Successful storage is initialized exactly once.
- `Heap.free` accepts the result, emits `mi_free`, and existing locally proved
  repeated-release/use-after-release diagnostics remain unchanged.
- Generated C contains `alignof(T)`, one demand-selected
  `hex_heap_allocate_aligned` definition, one `mi_malloc_aligned` call, and no
  over-allocation header, rounding formula, memset, or aligned-free wrapper.
- A program without aligned allocation emits none of the new surface and keeps
  every existing manifest hash unchanged.
- New aligned-allocation snippets add only their own manifest entries.
- Ordinary and tagged C23 suites pass; an executable fixture verifies a
  returned address is divisible by its requested alignment.

## Detailed implementation plan

### Phase 1: baseline and checker surface

1. Record current Heap API, component, tests, snippets, and manifest baseline.
2. Add exact recognition of `Heap.allocate_aligned<T>(initial, alignment)` by
   mirroring ordinary allocate generic/type placement checks.
3. Reuse contextual initializer checking and Size checking.
4. Fold constant alignments and reject zero and non-power-of-two values with the exact
   diagnostic.
5. Carry only the requested alignment in the checked allocation node; natural
   alignment remains a target-owned C23 `alignof(T)` expression.

### Phase 2: component primitive

1. Add the declaration and definition shown above to the Heap package.
2. Validate dynamic alignment before evaluating `alignment - 1` and before
   calling mimalloc.
3. Compute `max(requested, minimum_alignment)`, call `mi_malloc_aligned`, and
   retain the existing allocation-failure trap and ordinary `mi_free` path.
4. Add one render-model demand bit so the checked-in `heap.h`/`heap.c`
   templates conditionally include this primitive without creating another
   package or embedding C in Go strings.
5. Extend component tests to assert one definition, demand selection, exact
   validation order, and absence of manual over-allocation.

### Phase 3: typed lowering

1. Evaluate receiver, initializer, and alignment once in source order.
2. Call the primitive with `sizeof(T)`, requested alignment, and `alignof(T)`.
3. Store the initializer once and return the same `Ptr<mut T>` checked shape as
   ordinary allocation.
4. Preserve source mapping and existing freed-state identity.

### Phase 4: conformance

1. Implement every Validation case in focused checker, generator, integration,
   and tagged C23 tests.
2. Add one compact workbench snippet and update only its manifest entries.
3. Run ordinary and tagged C23 suites and inspect all generated-C movement.
4. Synchronize `docs/reference.md` only after behavior stabilizes and the user
   explicitly approves that edit.
5. Update status and close only after code, tests, generated C, and canonical
   documentation agree.
6. Rebuild and restart the workbench before handoff.

## Open questions

None.
