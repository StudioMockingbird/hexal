# RFC 0123: Stateless Default Heap

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented
- Features: headerless default allocation, component-specific initialization,
  and removal of runtime allocator identity
- Created: 2026-08-25
- Updated: 2026-08-25
- Depends on: the current Heap, allocation, collection, String, concurrency,
  cleanup, and C23 contracts in `docs/reference.md`
- Coordinates with: RFC 0027 (Arena and Pool allocators), RFC 0052 (target
  profiles), and RFC 0055 (filesystem and build driver)
- Changes generated C and weakens only best-effort runtime detection of
  otherwise-invalid cleanup; source syntax and public APIs do not change

## Summary

Hexal has one default Heap. `Heap.new()` selects it but creates no runtime
allocator object. The generated Heap representation becomes a one-byte token,
and default allocations use ordinary C23 `malloc`, `calloc`, and `free`
according to each component's initialization needs.

The current five-field allocation header, alignment offset marker, allocator
identity, and live flag are removed. This RFC does not replace them with a
registry, side table, cookie, universal zeroing policy, or second allocation
layer.

RFC 0027 starts only after this RFC is implemented. It builds Arena and Pool on
these stateless default-allocation operations rather than sharing this RFC's
migration.

## Goals

1. Make the generated default Heap an exact, small wrapper around standard C
   allocation.
2. Remove metadata used only to distinguish instances of a single allocator.
3. Zero storage only when a component's initial representation requires zero.
4. Preserve source-level explicit allocator passing and evaluation order.
5. Preserve every checker-proved lifetime diagnostic.

## Non-goals

- Changing Heap syntax, signatures, type eligibility, or explicit allocator
  passing.
- Adding Arena, Pool, custom allocators, allocator traits, or allocator-family
  dispatch.
- Adding recoverable allocation failure.
- Adding general alias analysis, ownership, generation checks, or runtime
  allocation tracking.
- Supporting C over-aligned types. The current Hexal type surface requires no
  alignment beyond the fundamental alignment guaranteed by `malloc` and
  `calloc`; RFC 0052 must qualify any future over-aligned representation.

## Source contract

The source API is unchanged:

```text
Heap.new() -> Heap
Heap.allocate<T>(initial: T) -> MutPtr<T>
Heap.free<T>(pointer: Ptr<T>) -> no value
Heap.free<T>(pointer: MutPtr<T>) -> no value
```

- Every written Heap expression is evaluated exactly once at its existing
  source-order position, even when its generated value is operationally
  irrelevant.
- Library constructors and cleanup retain their exact Heap parameters.
- HeapAllocation eligibility, explicit initialization, shallow copies, local
  freed-state checking, `defer`, and `errdefer` behavior do not change.
- There remains only one default allocator. Passing a different copied Heap
  token cannot select different runtime state.

## C23 representation

```c
typedef unsigned char hex_heap;
```

`Heap.new()` lowers to `(hex_heap)0`. No generated expression reads an
`identity` member.

The Heap component owns these checked operations:

```c
void *hex_heap_allocate(size_t size);
void *hex_heap_allocate_zeroed(size_t count, size_t size);
void hex_heap_free(void *pointer);
```

- `hex_heap_allocate` calls `malloc(size)`, traps on a null result, and returns
  the storage unchanged.
- `hex_heap_allocate_zeroed` first uses `ckd_mul` to distinguish an
  unrepresentable size, then calls `calloc(count, size)`, traps on a null
  result, and returns storage whose bytes are zero.
- `hex_heap_free` calls `free(pointer)` directly. It adds no null, identity,
  liveness, or ownership check.
- These operations are not delegation-only wrappers: they own Hexal's exact
  overflow and allocation-failure traps. No second raw-allocation API remains.
- Callers check every component-specific addition, multiplication, capacity,
  and flexible-array calculation before calling either allocation operation.

## Allocation selection by component

Zeroing is selected by representation need, never as a universal Heap policy.

| Component/storage | Operation | Reason |
|---|---|---|
| `Heap.allocate<T>` | non-zeroing | The explicit initializer writes the complete T once. |
| List header and element regions | non-zeroing | The constructor initializes the header; only live elements are read, and growth copies only the live prefix. |
| String header, bytes, and terminator | non-zeroing | Construction writes the header, complete payload, and final NUL. |
| Dict header | non-zeroing | Construction writes every header field. |
| Dict bucket region | zeroed | Zero is the required initial empty-bucket representation. Replace the current allocate-plus-`memset` pair with the zeroed operation. |
| Channel control | preserve `calloc` | Its runtime state intentionally begins in the all-zero representation. |
| Channel slot bytes | preserve `malloc` | Slots are read only after a send writes a live element. |
| Mutex control | preserve `calloc` | Its runtime state intentionally begins in the all-zero representation. |
| Task/runtime-internal storage | preserve the component's current choice | It is not allocated through a source Heap and is outside the identity migration. |

Any other migrated allocation site must prove one of two conditions:

- every byte that can be read is explicitly initialized first, so it uses the
  non-zeroing operation; or
- zero is part of the component's initial representation, so it uses the
  zeroed operation.

The implementation records that classification in the owning component test.
It does not choose zeroing merely because the old shared allocator happened to
return zeroed storage or because zeroing is convenient.

## Cleanup and diagnostics

The checker retains all locally provable cleanup diagnostics: freeing a pointer
derived from `ref`, definite repeated free, and definite use after free.

Removing the runtime header also removes these best-effort runtime messages:

```text
[Runtime Error] double deallocation
[Runtime Error] deallocation used the wrong allocator
```

Those operations were already invalid, and the current header could not make
them safe: reading its live flag after the backing allocation had been freed
was itself outside a defined lifetime. Unknown aliases therefore follow the
existing language rule without a runtime promise. A repeated or invalid C
`free` has no guaranteed Hexal diagnostic.

String, List, and Dict cleanup no longer compare Heap identities. They release
their owned allocation(s) directly. `hex_string` is the first member of
`hex_string_storage`; C therefore guarantees that the returned member pointer,
converted back to the storage pointer, names the allocation base passed to
`free`.

## Required sweep

- Delete `hex_heap_header`, `HEX_HEAP_DEFAULT`, the alignment offset marker,
  allocator identity, live flag, and the old raw allocate/free signatures.
- Delete allocator fields from List and Dict representations and every String,
  List, Dict, Channel, Mutex, IO, and cleanup adapter that exists only to pass
  or compare Heap identity.
- Replace String/List/Dict allocation calls according to the component table;
  remove Dict's now-redundant bucket `memset`.
- Keep source Heap operands in checked metadata and generated evaluation where
  required for exactly-once evaluation. Do not delete an operand merely because
  its value is unused.
- Update deferred Heap, collection, Channel, and Mutex cleanup without changing
  capture order or evaluation count.
- Replace tests that require header metadata or removed runtime messages with
  tests for stateless representation, allocation selection, and retained local
  diagnostics. Do not preserve a compatibility allocation path for old hashes.
- Keep runtime templates as `.c`/`.h` files under `generator/packages`; do not
  move their source into Go strings.
- Re-scan generated runtime messages after implementation and update the open
  trap-coverage count in `docs/status.md` from the measured result.
- Update RFC 0052 during implementation so target qualification owns the
  fundamental-alignment assumption instead of a generated runtime assertion.

## Implementation plan

### Migration inventory

Phase 0 was executed against the tree. Every site that spells the removed
metadata is listed here; the migration is complete when none remains.

| File | Sites | Required work |
|---|---|---|
| `generator/packages/heap.h` | `hex_heap` struct, `HEX_HEAP_DEFAULT`, `hex_heap_header`, two raw declarations | Replace with the byte token and the three checked declarations. |
| `generator/packages/heap.c` | `hex_heap_raw_allocate`, `hex_heap_free` | Replace with checked `malloc`, checked `calloc`, and direct `free`. |
| `generator/packages/string.c` | 3 `hex_heap_raw_allocate`, `hex_string_free` header recovery | Non-zeroing allocation; free the storage base recovered from the first member. |
| `generator/packages/list.h` | `allocator` field, 2 growth allocations, 3 frees, new/free identity compare | Drop the field; non-zeroing allocation; direct free. |
| `generator/packages/dict.h` | `allocator` field, header + bucket allocation, 3 frees, bucket `memset`, identity compare | Drop the field; zeroed bucket allocation replacing `memset`; direct free. |
| `generator/heap.go` | typed `hex_heap_allocate_<T>` body, `renderHeapFree` | Body calls the non-zeroing operation; free preserves receiver evaluation. |
| `generator/render.go` | `Heap.new()` lowering | Emit `((hex_heap)0)`. |
| `generator/concurrency.go` | 4 `uintptr_t heap_identity` helper signatures, 4 `.identity` call sites | Take `hex_heap h` and void it; call sites pass the token. |
| `generator/defer.go` | 3 deferred-cleanup call sites | Same token change; capture order unchanged. |
| `generator/emission.go` | 2 comments naming the raw API | Update to the new operation names. |
| `generator/alloc_test.go`, `{dict,list,string}_component_test.go`, `tests/integration/{alloc,concurrency}_test.go` | header/identity expectations | Replace with stateless-representation and allocation-selection expectations. |

### Uniform token rule

Every generated helper that currently takes `uintptr_t` allocator identity
instead takes `hex_heap h` and voids it. This preserves exactly-once evaluation
of the source Heap expression through the ordinary argument position and needs
no comma operator, sequencing helper, or checked-metadata change.

`Heap.free` is the one site with no owning helper. It renders as
`((void)(<receiver>), hex_heap_free(<pointer>))` so the receiver still evaluates
once, before the argument, in source order.

### Removed defensive branches

Beyond the two runtime messages named above, the identity compare in
`hex_list_free_<T>`, `hex_dict_free_<K>_<V>`, and `hex_string_free` also
guarded a null receiver in the same condition. Those guards are removed with the
compare. A null collection or String handle cannot arise from checked source,
and `free(nullptr)` is a defined no-op, so no reachable behavior is lost. Do not
reintroduce them under a different message.

### Preserved trap messages

The component-specific overflow messages are unchanged and keep their existing
spellings and call order:

```text
[Runtime Error] string allocation size overflow
[Runtime Error] list capacity is not representable
[Runtime Error] dictionary capacity is not representable
[Runtime Error] allocation size is not representable
[Runtime Error] heap allocation failed
```

A component that already checks its own size keeps that check and its message,
then calls the Heap operation. The operation's own check is a redundant backstop
in that path, not a replacement for the component's message.

### Phase 1: stateless Heap core

1. Rewrite `packages/heap.h` as the byte token plus the three declarations.
2. Rewrite `packages/heap.c` with checked `malloc`, checked `calloc`, and direct
   `free`.
3. Update `Heap.new()` lowering and the typed allocate helper body.
4. Update the heap component tests to assert the new declarations, the absence
   of every deleted name, and the null-trap and overflow ordering.

### Phase 2: owning components

1. Migrate `string.c` allocation and free.
2. Migrate `list.h`: drop the allocator field, both growth paths, and free.
3. Migrate `dict.h`: drop the allocator field, use the zeroed operation for
   buckets, delete the `memset`, and migrate free.
4. Migrate the Channel and Mutex helper signatures and their call sites.
5. Migrate `Heap.free` and every deferred cleanup site.

### Phase 3: sweep and conformance

1. Run the required sweep until no deleted name, field, offset formula, or
   removed runtime message remains outside historical specs.
2. Update every affected component and integration test to assert the new
   generated text rather than compilation success.
3. Implement every Validation item below and no additional behavior.
4. Rebuild the snippet manifest once. Attribute every moved artifact to the
   representation migration; no-allocation artifacts remain byte-identical.
5. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
6. Re-measure the generated runtime-message count and update that one record in
   `docs/status.md`.
7. Update RFC 0052 and `docs/reference.md`, including the second stale sentence
   requiring "the matching allocator" alongside the metadata promise.
8. Rebuild and restart the workbench.
9. Remove this RFC from open status and unblock RFC 0027 only after code, tests,
   artifacts, and canonical docs agree.

## Validation

This section is exhaustive.

### Source and checker

- All existing Heap, String, List, Dict, Channel, Mutex, IO, and cleanup source
  signatures remain accepted unchanged.
- Each explicit Heap receiver or argument expression is evaluated exactly once
  in source order, including constructor and deferred-cleanup paths.
- HeapAllocation eligibility and all current local `ref`-free, repeated-free,
  and use-after-free diagnostics remain unchanged.
- Programs cannot inspect, compare, order, hash, or print the Heap token through
  a newly exposed operation.

### Generated C

- `hex_heap` is exactly an unsigned-byte token; `Heap.new()` emits its zero
  value. Generated artifacts contain no Heap struct, allocator identity,
  allocation header, offset marker, or live flag.
- `Heap.allocate<T>`, String storage, List headers/regions, and Dict headers use
  the non-zeroing operation and initialize every readable byte before use.
- Dict buckets use the zeroed operation and no following `memset`.
- Channel control and Mutex control retain their required `calloc`; Channel
  slots retain `malloc`. No universal `calloc` policy is introduced.
- Every dynamic size expression is checked before allocation. The zeroed helper
  checks `count * size` before `calloc`; allocation failure uses the existing
  allocation-failure trap.
- Every cleanup passes the allocation base to the one-argument
  `hex_heap_free`; String converts its first-member `hex_string *` back to the
  address-equivalent `hex_string_storage *` base.
- The removed cleanup messages and every identity-adapting helper are absent.
- No registry, side table, cookie, allocator descriptor, family tag, virtual
  dispatch, aligned-allocation shim, or compatibility allocator is emitted.
- Component headers/sources remain demand-driven, self-contained, and ordered;
  repeated compilation is byte-identical.
- No-allocation snippet artifacts remain byte-identical. Every changed manifest
  entry belongs to a snippet selecting Heap or a migrated Heap-backed family.

### Gates

- Focused component and integration tests assert the generated-C properties,
  not only compilation success.
- Ordinary tests remain pure Go; no external compiler is invoked.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

After behavior stabilizes, update `docs/reference.md` once to:

- retain the exact Heap API, explicit allocator passing, HeapAllocation, and
  local lifetime rules;
- state that Heap is one stateless default allocator represented by a generated
  byte token;
- replace the runtime-header/identity wording with component-selected
  non-zeroing or zeroed standard allocation and direct `free`;
- replace the requirement that `h.free(ptr)` receive the matching allocator:
  every Heap value selects the same stateless default allocator, so no runtime
  allocator match exists;
- remove the promise that runtime metadata may diagnose allocator mismatch or
  repeated free, while retaining the checker-owned local diagnostics; and
- record the fundamental-alignment target requirement as a C23 representation
  contract, without implementation walkthroughs or examples.

No canonical documentation changes before implementation.
