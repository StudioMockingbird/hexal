# RFC 0027: Typed Stash and Pool Allocators

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented 2026-08-27. `Stash<T>` and `Pool<T>` are real,
  independent allocator types, built and verified end to end.
  **Types** (`compiler/types`): `StashInfo`/`PoolInfo` plus `Stash`/`Pool`
  fields on `Type`; `Environment.StashType`/`PoolType` constructors reusing
  `Eligible(element, PositionHeapAllocation)` verbatim (no new eligibility
  rule); per-`Environment` arena maps keyed `"stash:"+element.CanonicalKey`
  / `"pool:"+element.CanonicalKey`. `Stash<T>`'s `CName` is the single fixed
  string `"hex_stash"` for every T — the runtime core is type-erased, so
  unlike every other constructed generic in this codebase it deliberately
  does *not* go through `uniqueCollectionCName`'s per-instantiation naming;
  `Pool<T>`'s `CName` does, exactly like `List<T>`, because Pool is fully
  monomorphized (a compile-time-typed slot array, not type-erased). Both
  needed a canonical-signature case added to `isCanonicalForEnvironment`
  (missing this was the first real bug the generator's own validation
  caught: without it, a bare `Stash<Node>` binding fell through to
  `isCanonicalScalar` and failed as "unsupported checked declaration
  type") and a case added everywhere a type-family switch already
  enumerated Task/Channel/Atomic: `nominalModuleOf` (arena.go),
  `typeContainsPlaceholder` (generics.go), `privateTypeInUse` (modules.go),
  and both of the generator's own recursive type walkers (`walk.go`).
  `IsProtectedTypeName` and the generic-type-expression resolver dispatch
  (`type_resolution.go`) gained `"Stash"`/`"Pool"` entries alongside
  `List`/`Channel`/`Atomic`.
  **Checker** (`compiler/checker/stash.go`, `pool.go`, new): constructor and
  method dispatch mirrors `List`/`Channel`/`Atomic`'s established shape —
  `checkStashTypeCall`/`checkPoolTypeCall` for `.new(...)`,
  `checkStashMethodCall`/`checkPoolMethodCall` dispatching `allocate` /
  `reset` / `destroy` / `free` by name — wired into `methods.go`'s existing
  two dispatch points (bare-type-name and value-receiver) exactly like every
  other builtin. Two new `ExpressionKind` pairs
  (`Stash|PoolConstructorExpression`, `Stash|PoolMethodCallExpression`).
  Lifetime tracking reuses the existing `tracked`/`freed`/`version` lattice
  Heap.free already rides (extending `trackablePointerBinding` to admit
  Stash/Pool *handle* bindings themselves, the same way IO's `close`
  already does for a non-pointer handle) plus the existing
  `provenance`/`setProvenance` single-source-borrow mechanism Bytes-over-List
  already uses (extended in `io.go`'s `seedStreamBindingFacts`, which now
  also records a fresh `Stash|PoolMethodCallExpression{Name:"allocate"}`
  result's provenance edge back to its receiver). `Stash.reset()`/`destroy()`
  and `Pool.destroy()`'s precondition check both walk that provenance map
  (two new `flowState` methods, `invalidateAllocationsFrom` and
  `hasLiveTrackedAllocation`) rather than introducing a generation/epoch
  concept: reset eagerly marks every *currently* provenance-linked binding
  freed, so a later `allocate` call — a new binding, a new provenance edge —
  is untouched by an earlier reset's walk, which is what makes "reset, then
  allocate again, then use the new allocation" work correctly (verified;
  see below) without new per-pointer state. The existing deferred-capture
  machinery (`alloc.go`) was generalized rather than duplicated: its
  Heap-only `HeapFreeBinding`/`HeapFreeVersion` fields became
  `TrackedFreeBinding`/`TrackedFreeVersion`, and `captureDeferredHeapFree`/
  `validateDeferredActionsInState` route through a new small
  `trackedReleaseTarget` helper recognizing `HeapFreeExpression`,
  `PoolMethodCallExpression{Name:"free"|"destroy"}`, and
  `StashMethodCallExpression{Name:"destroy"}` alike, so `defer stash.destroy()`
  and `defer pool.free(node)` get the identical registration-time
  binding+version capture Heap.free always had.
  **Generator**: Stash and Pool intentionally use *different* emission
  patterns because their C representations differ, per the RFC's own
  wording (only Pool's validation text says "exactly one correctly owned
  specialization per canonical T"). Stash (`stash.go`, `stash_component.go`,
  `generator/packages/stash.h`/`.c`) is type-erased: one shared,
  program-wide `hex_stash` bump-allocator core (block-chain growth,
  4096-byte-or-larger first block, checked doubling via `ckd_mul`/`ckd_add`,
  lazy reset that only rewinds to the first block and lets
  `hex_stash_allocate` zero each later block's `used` field as it
  re-enters it) plus tiny per-module, per-T typed constructor/allocate
  wrappers (`hex_stash_new_<T CName>`, `hex_stash_alloc_<T CName>`,
  Heap.allocate's own `heap.go` pattern, keyed by `element.CName` — not
  `element.Name`, unlike Heap's own helper, specifically so two
  same-named nominal types from different modules can't collide, closing a
  latent gap Heap.allocate's own naming still has). Pool (`pool.go`,
  `pool_component.go`, `generator/packages/pool.h`) is fully monomorphized
  like `List<T>`: one struct per canonical T (slots/live-bytes/free-index
  stack, each a separate heap allocation sized by the runtime capacity),
  O(1) allocate (pop index, mark live, write T) and free (address range
  and alignment check via `uintptr_t`, live-byte check, push index), reusing
  `List`'s exact ownership-split machinery — `module_collections.go`'s
  `typeIsModuleEmitted`/`collectionElementModuleTyped`/
  `moduleCollectionDependencyOrder`/`writeModuleCollectionSpecializations`
  all gained a `typ.Pool` case as a fifth family alongside
  view/array/list/dict, and the same `componentArtifact{block: "poolbody"}`
  fragment-render trick gives Pool both a shared `hexal/pool.h` (builtin T)
  and a per-module inline fragment (module-owned T) from one template.
  Both families needed `declaration()`/`typeSpelling()` cases (mirroring
  `Mutex`'s, not `List`'s: `Stash`/`Pool` values are bare struct tags
  needing a manually-appended `" *"`, not a pre-typedef'd pointer alias like
  `Channel`/`Task`) — the second real bug the generator's own dispatch and
  a build against real toolchains together caught: without them, a
  `Stash<Node>` or `Pool<Node>` binding rendered with no pointer at all.
  `defer.go`'s separate registration-time-capture rendering path
  (`writeDeferStatement`/`renderDeferredCall`) needed its own new cases for
  the same four method names.
  **Runtime traps**: exactly the RFC's seven messages, verbatim, each at its
  specified site (pool capacity, exhaustion, wrong-pool pointer, non-live
  free, non-empty destroy; allocation-size and heap-allocation-failure reuse
  Heap's own messages unchanged).
  **Verified**: every checker-side rejection the RFC's Validation section
  names — individual Stash free, explicit `allocate<U>` type argument,
  constant-zero Pool capacity, Pool destroy with a directly tracked live
  slot, use after Stash destroy, use of a pre-reset allocation after
  `reset()`, Pool double-destroy, `List<T>.new` rejecting a Stash argument —
  each reproduced with the RFC's exact message text via a scratch probe,
  then formalized as `compiler/tests/integration/allocators_test.go`. A
  union-typed Stash allocating through ordinary contextual injection while
  still returning `MutPtr<Item>` compiles and was spot-checked directly. The
  full generated C for a combined Stash+Pool program (allocate, reset,
  allocate again, deferred destroy in each of two nesting orders) was
  dumped and hand-verified, then compiled clean — zero warnings, zero
  suppressions — under real GCC 16.1, Clang 22.1.8, and `zig cc` 0.16 with
  `-std=c23 -Wall -Wextra -Werror`. Three new workbench snippets
  (`memory-stash-bump-allocate`, `memory-stash-reset-reuse`,
  `memory-pool-fixed-capacity`) were added to
  `06-pointers-and-memory.json`; the manifest rebuild added only their 31
  new hash entries — every one of the pre-existing 140 snippets' hashes is
  byte-identical, confirming zero collateral change to any other generator
  path. The full `-tags c23` catalog sweep (143 snippets × 3 toolchains,
  RFC 0140's now-parallel `TestC23SnippetCatalogCompiles`) passed completely
  in 185s. `docs/reference.md` gained a `Stash<T>` and `Pool<T>` section
  under Allocation and lifetime plus updates to every list this RFC's
  Validation section named (protected types, handle-copy/external-state
  lists, canonical-interning list, valid-Ptr/MutPtr-pointee list) and had
  "Arena, Pool" removed from two now-stale Excluded-features mentions.
  Full gate green: `gofmt -l .`, `go build ./...`, `go vet ./...`,
  `go vet -tags c23 ./...`, `go test -count=1 ./...`.
- Features: independent typed Stash allocation, typed Pool allocation, and
  bulk region release
- Created: 2026-08-11
- Updated: 2026-08-27
- Depends on: the implemented stateless default Heap (`hex_heap`, a one-byte
  token with no runtime identity; see `docs/reference.md`'s Allocation and
  lifetime section) and the current pointer, collection, cleanup, and
  shallow-copy contracts in `docs/reference.md`
- Coordinates with: RFC 0039 (C interoperability), RFC 0052 (target profiles),
  RFC 0110 (affine ownership and allocator lifetimes), and RFC 0118
  (concurrency safety)
- Changes the protected type set and allocator APIs; reference synchronization
  is required only after implementation stabilizes
- Accepted Pool cost: one live byte plus one `Size` free-stack entry per slot,
  in addition to T storage; the uniform O(1) representation is preferred over
  type-dependent reuse of free-slot payload bytes
- Accepted Stash cost: `reset()` retains and rewinds the complete high-water
  block chain; only `destroy()` releases it. A union-typed Stash allocates every
  entry at that union's maximum payload size and alignment.

## Summary

Hexal adds two independent concrete allocator families alongside `Heap`:

```hexal
stash := Stash<Node>.new()
defer stash.destroy()

nodes := Pool<Node>.new(128)
defer nodes.destroy()
```

`Stash<T>` grows as needed, allocates only one canonical `T`, and releases all
of its storage as one region. `Pool<T>` owns a fixed number of reusable slots
for one concrete `T` and supports individual allocation and release.

A Stash becomes heterogeneous only through an explicit sum type:

```hexal
type Item = Node | Edge
items := Stash<Item>.new()
```

Every allocation still stores one `Item` and returns `MutPtr<Item>`. Normal
union injection, narrowing, and matching govern the concrete member. This
keeps the allocator type honest and makes the broader representation cost
visible in the source type.

The allocators are built-in concrete types. This RFC adds no allocator trait,
capability syntax, virtual dispatch, or user-defined allocator implementation.

## Goals

1. Keep Stash and Pool construction independent of an explicit parent Heap.
2. Reuse the reference's current initialization, provenance, and cleanup rules.
3. Make typed bulk-lifetime allocation cheap and simple.
4. Make fixed-type, fixed-capacity allocation predictable.
5. Keep existing String, List, Dict, Channel, and Mutex allocation Heap-only.
6. Build directly on the stateless default Heap's allocation operations
   (`hex_heap_allocate`, `hex_heap_allocate_zeroed`, `hex_heap_free`).

## Current compatibility boundary

The current tree has exactly one allocator type, `Heap`, represented as a
stateless one-byte token (`hex_heap`) with no runtime identity metadata.
`String`, List, Dict, Channel, and Mutex still accept `Heap` exactly. There is
no allocator trait, descriptor, overload, or allocator type parameter to which
`Stash<T>` can be passed.

The current language also uses shallow copies and local freed-state checking.
It has no affine owner, generation-bearing Pool slot, general unsafe boundary,
or proof that every alias dies before reset, release, or destroy. This RFC must
not claim stronger stale-pointer detection than that model can implement.

`Stash<T>` and `Pool<T>` remain excluded from the language until this RFC is
implemented and synchronized into `docs/reference.md`.

## Stash<T>

A Stash is an independent, type-specific runtime owner backed internally by
Hexal's default allocation primitives:

```hexal
stash := Stash<Node>.new()
defer stash.destroy()
```

`Stash<T>.new()` allocates control state for exactly one complete canonical T
and defers its first data block until the first allocation. The state records
T's size and alignment once. The Stash retains no parent Heap, allocator
descriptor, or function table. Default allocation is the constructor's fixed
internal backing policy, not a user-selectable fallback.

Allocation uses the current explicit initialized form:

```hexal
node := stash.allocate(Node { value = 1 })
```

`allocate(initial)` accepts a value assignable to T and returns `MutPtr<T>`.
The method accepts no type arguments; the receiver fixes the allocation type.
For union T, ordinary contextual union injection admits one member value while
the result remains `MutPtr<T>`.

A Stash allocation cannot be individually released:

```hexal
stash.free(node)
// Error: Stash allocations are released by reset or destroy
```

`stash.reset()` invalidates every allocation made since construction or the
previous reset. It rewinds every allocated block and retains all of them for
reuse; it neither zeroes nor releases payload storage. Repeated frame/request
workloads therefore reuse their high-water allocation without a free/reallocate
cycle. Retained peak memory is an accepted Stash cost; `destroy()` is the
operation that releases it.

`stash.destroy()` invalidates every remaining allocation and releases every
block plus control state. The programmer must ensure that no allocation or View
from the Stash is used after reset or destroy.

Stash allocations are aligned for T. Construction, growth, unrepresentable
size/alignment, and allocation failure trap under the same policy as
`Heap.allocate`; this RFC adds no recoverable allocation Error.

The checker rejects a reset/destroy followed by a use through the same locally
tracked Stash allocation or View, including all-path branch facts. Aliased,
escaped, parameter-reached, member-reached, and collection-reached pointers use
the current undecided-case policy: they are not proven safe and receive no
guaranteed runtime stale-pointer diagnostic. Stash dereferences gain no runtime
generation check. Because reset deliberately does not zero retained blocks, an
unchecked stale alias may appear to observe an old value until that storage is
reused; no stale-read result or detection is guaranteed.

## Library-allocation boundary

V1 does not make Stash a general library allocator. These remain Type Errors:

```hexal
List<Int32>.new(stash)
Dict<Int32, Int32>.new(stash)
"text".to_string(stash)
Channel<Int32>.new(stash, 4)
Mutex.new(stash)
```

String, List, Dict, Channel, and Mutex keep their exact Heap signatures and C
representations. No allocator trait, function-pointer descriptor, family tag,
constructor overload, or allocator type parameter is introduced.

A Stash allocation may contain handles to separately owned resources. Reset
and destroy release only Stash bytes; they do not invoke cleanup for stored
values. The programmer cleans those resources before invalidating the region.

## Pool<T>

`Pool<T>` is a built-in generic allocator for exactly one complete concrete
type and one fixed runtime `Size` capacity:

```hexal
nodes := Pool<Node>.new(128)
defer nodes.destroy()
```

Capacity must be positive and fit the implementation's supported allocation
size. Pool construction allocates its control state and slot storage directly
through Hexal's default allocation primitives. It retains no parent Heap.

```hexal
node: MutPtr<Node> := nodes.allocate(Node { value = 1 })
nodes.free(node)
```

`allocate(initial)` initializes one free slot and returns `MutPtr<T>`. It traps
when no slot is available; this RFC adds no recoverable Pool-exhaustion Error.
`free(pointer)` accepts `Ptr<T>` or `MutPtr<T>`. It validates that the address
names an aligned slot in that exact Pool and that the indexed slot is currently
live, then makes it reusable. Wrong-Pool, interior, misaligned, out-of-range,
and currently-free pointers trap.

The result intentionally remains raw `MutPtr<T>` under the current manual
lifetime model. A copied or escaped pointer to an earlier occupant cannot be
distinguished after that slot is reused. Locally proved use/release after
`pool.free` is rejected; aliases beyond those local facts follow the current
undecided-case policy and gain no generation check.

`nodes.destroy()` requires every slot to have been freed. It then releases the
Pool's storage and logically invalidates every copied Pool handle. Destroying a
non-empty Pool is a defined runtime allocation-state failure.

`Pool<T>` is not a general variable-size allocator. It cannot back String,
List, Dict, Channel, Mutex, Stash blocks, or a `Pool<U>` where `U` differs
canonically from `T`. This avoids hidden fallback allocations and variable-size
behavior in a fixed-slot abstraction.

Pool release destroys no T and runs no cleanup. If T contains handles to
separately owned resources, the programmer cleans them before releasing the
slot. Pool destruction requires every slot released and therefore never walks
or destroys stored T values.

## Copying and lifetime

Stash and Pool follow the reference's current shallow-copy rule. Assignment,
argument passing, return, and aggregate storage copy the handle; all copies
refer to the same allocator state. No copy owns the state independently.

The programmer must arrange exactly one `destroy` for that state and must stop
using every copied handle and allocation after reset or destroy. Pool metadata
validates addresses and live slots while its control state remains live. A
destroy through one escaped alias leaves other aliases dangling; repeated
destroy through such an alias has no guaranteed runtime diagnostic. The compiler
tracks only the reference's current local lifetime facts, not general aliases.

`mut` controls whether an allocator binding can be reassigned. It does not
create ownership and does not change allocator behavior.

## Thread safety

- Heap is thread-safe; Stash and Pool are not. One Stash's bump state and one
  Pool's slot/free-stack state must not be mutated concurrently by multiple
  Tasks unless an external synchronization operation orders every access.
- Shallow copying a Stash or Pool handle does not add synchronization.
  Cooperative scheduling does not make a shared handle safe because Tasks may
  run in parallel on different scheduler workers.
- This RFC adds no cross-task alias analysis and no runtime race detector. A
  cross-task conflict that the current checker cannot prove receives no
  diagnostic from this RFC.
- RFC 0118 owns the later concurrency rule: locally provable conflicting
  sharing, reset, destroy, or slot release is rejected, and undecidable sharing
  requires its explicit unsafe boundary. This RFC adds no temporary syntax or
  checker mechanism that RFC 0118 would later remove.

## Deferred cleanup

Allocator cleanup uses ordinary `defer`:

```hexal
stash := Stash<Node>.new()
defer stash.destroy()

pool := Pool<Node>.new(64)
defer pool.destroy()
```

Direct-call capture, reverse ordering, branch scopes, and loop scopes remain
unchanged. Registering deferred destruction and also destroying through any
alias is a programmer error because the deferred call will later destroy the
same state again.

## Exact source API

```text
Stash<T>.new() -> Stash<T>
Stash<T>.allocate(initial: T) -> MutPtr<T>
Stash<T>.reset() -> no value
Stash<T>.destroy() -> no value

Pool<T>.new(capacity: Size) -> Pool<T>
Pool<T>.allocate(initial: T) -> MutPtr<T>
Pool<T>.free(pointer: Ptr<T> | MutPtr<T>) -> no value
Pool<T>.destroy() -> no value
```

- `Stash` and `Pool` become protected names; each requires exactly one type
  argument wherever used. Bare `Stash` and bare `Pool` are invalid types.
- `Stash<T>` and `Pool<T>` require complete, finite, copyable `T` valid for
  HeapAllocation; direct Atomic and function-value elements remain invalid.
- `Stash<T>.allocate` has no explicit type arguments. Its initializer must be T
  or contextually inject into union T, and its result is always `MutPtr<T>`.
- A constant zero Pool capacity is rejected statically. A dynamic zero capacity
  traps. Unrepresentable capacity/size and construction failure trap.
- Stash/Pool handles are pointer-sized, shallow-copyable aliases under the
  current language contract. `mut` changes only binding reassignment.
- `destroy()` ends allocator state. It is distinct from
  `Pool.free(pointer)`, which releases one slot.
- Construction always uses Hexal's default allocation primitives. Stash and
  Pool expose no parent-allocator argument and retain no parent allocator.
- Constructor calls determine their result types, so an explicit binding type
  is unnecessary: `stash := Stash<Node>.new()` and
  `nodes := Pool<Node>.new(capacity)` infer `Stash<Node>` and `Pool<Node>`.

## C23 representation

- Every `Stash<T>` value is `hex_stash *`. Its control block owns a linked list
  of blocks, the current bump position, and the one immutable size/alignment
  pair established for T by the typed constructor.
- The first allocation creates a 4096-byte data block or the smallest larger
  checked capacity satisfying that allocation, whichever is greater.
- Later growth doubles the previous block capacity until it satisfies the
  checked request; if doubling is unrepresentable, it uses the exact checked
  required capacity. Blocks use the default non-zeroing allocation operation
  (`hex_heap_allocate`) because every returned T receives an explicit
  initializer.
- Allocation uses the state’s fixed T alignment and size, checks every
  addition, writes the T initializer exactly once, and advances the bump
  position. No allocation call supplies or changes a runtime type descriptor.
- Reset rewinds every block, sets the current block to the first, and retains
  the complete block chain without zeroing. Allocation reuses retained blocks
  in chain order before growing. Destroy frees every block and the control state.
- `Pool<T>` is a monomorphized pointer-sized handle to state containing its
  fixed capacity, free count, contiguous aligned `T` slots, one live byte per
  slot, and a `Size` free-index stack. Construction initializes the stack so
  allocation and release are O(1).
- Pool control, slot, and free-index storage use non-zeroing allocation followed
  by explicit initialization. The live-byte region uses zeroed allocation
  because zero is its initial all-free state.
- Pool allocation pops one index, marks it live, writes `T` once, and returns
  its address. Release converts addresses through `uintptr_t`, validates range,
  slot alignment, and live state before pushing the index.
- Pool destroy traps when `free_count != capacity`; otherwise it frees slot,
  live-state, free-index, and control allocations.
- Stash and Pool runtime cores live in `generator/packages/stash.c/.h` and
  `pool.c/.h`; typed Stash constructor/allocation helpers and Pool helpers
  follow existing constructed-type ownership so module-owned `T` never leaks
  into a program-wide header.
- No parent-allocator state, allocator descriptor, virtual dispatch, alternate
  backing path, or runtime generation check is emitted.
- Every size multiplication, alignment calculation, block growth, and pointer
  offset is checked before allocation or pointer formation.

## Diagnostics and traps

- Invalid type arguments, missing initializers, wrong allocator families,
  unsupported methods, and locally proved stale uses are Type Errors owned by
  the checker.
- `stash.free(pointer)` reports `Stash allocations are released by reset or
  destroy`.
- Bare or wrongly parameterized Stash reports
  `Stash requires exactly one element type`.
- `stash.allocate<U>(value)` reports
  `Stash allocation accepts no type arguments; its element type is fixed by the receiver`.
- An initializer that cannot initialize T reports the ordinary expected-T type
  mismatch; union T first applies ordinary contextual union injection.
- Constant zero Pool capacity reports `Pool capacity must be positive`.
- Destroy with a directly tracked live Pool slot reports `Pool cannot be
  destroyed while a locally tracked slot is live`; unknown aliases remain a
  runtime concern.
- Dynamic failures use these exact runtime messages:

```text
[Runtime Error] allocation size is not representable
[Runtime Error] heap allocation failed
[Runtime Error] pool capacity must be positive
[Runtime Error] pool exhausted
[Runtime Error] pointer does not name a slot in this pool
[Runtime Error] pool slot is not live
[Runtime Error] pool destroy with live slots
```

Each generated message ends with `\n`. Stash and Pool use the existing default
allocation size/failure messages. No allocation, reset, release, or destroy
returns Error.

## Deferred work

- Recoverable allocation exhaustion through Error values.
- Dynamically growing Pools.
- Thread-safe Stash and Pool variants.
- Region-owned typed destructor registration.
- User-defined allocators and a public allocator interface.
- Stack-backed Stash storage.
- Pool iteration and diagnostics exposing live slots.

`Stash<T>.new()` permanently denotes default-backed construction. Any later
stack-backed Stash uses a distinct constructor name; it does not overload or
change the zero-argument constructor.

## Required sweep

- Treat the stateless Heap (`hex_heap`, a one-byte token; see
  `docs/reference.md`) as this RFC's baseline. Do not repeat, amend, or
  partially reimplement its runtime representation here.
- Reuse the default Heap allocation primitives internally, but expose no Heap
  argument in Stash or Pool construction and store no parent allocator.
- Keep String/List/Dict/Channel/Mutex signatures Heap-only and reject Stash or
  Pool arguments; add no allocator descriptor scaffolding.
- Reuse existing constructed-type interning, C-name collision handling,
  component ownership, position eligibility, freed-state flow, and defer
  capture machinery instead of parallel implementations.
- Extend cleanup flow only with Stash reset/destroy and Pool free/destroy
  invalidation facts. Do not infer aliases beyond the reference's current local
  boundary.
- Keep runtime templates as `.c`/`.h` files under `generator/packages`, never Go
  strings.

## Implementation plan

### Implementation map

| Area | Required work |
|---|---|
| `compiler/types` | Register protected names and canonical `Stash<T>`/`Pool<T>` construction, eligibility, identity, C names, and ownership classification. |
| `compiler/checker/alloc.go` and flow state | Check all APIs, reuse HeapAllocation rules, record reset/release/destroy effects, integrate defer and branch merges, and preserve the local-analysis boundary. |
| checker/generator dispatch coverage | Add explicit nodes for every new operation and fail closed for invalid metadata. |
| new Stash/Pool component models and `generator/packages/stash.*`, `pool.*` | Emit demand-driven runtime cores and typed helpers with existing component ownership. |
| unit/integration/component tests | Cover type construction, flow diagnostics, generated layouts, component selection, C ordering, deterministic output, and every Validation item. |
| workbench snippets and manifest | Add focused Stash/Pool snippets; existing entries must not change. |

### Phase 0: baseline and migration inventory

1. Confirm the tree's stateless-Heap representation and record the green
   test/vet baseline and snippet manifest.
2. Record Heap-only collection, concurrency, IO, and no-allocation artifacts as
   unchanged controls.
3. Inventory allocator type registration, HeapAllocation eligibility,
   constructed-type ownership, lifetime flow, cleanup capture, and component
   discovery paths that Stash or Pool must reuse.
4. Add focused rejected-source probes for Stash/Pool names before registration,
   then retain them as the before/after language boundary.

### Phase 1: register Stash and Pool types

1. Add constructed `Stash<T>` and `Pool<T>` identities in
   `compiler/types`, including canonical keys, display/C names, placement,
   copyability, equality/print rejection, and program/module ownership.
2. Extend type-use resolution for exactly one argument on both families and
   reserve both protected names in every environment. Reject bare Stash/Pool.
3. Add checked expression kinds and metadata for constructors, allocate, reset,
   free, and destroy; include them in fail-closed checker/generator dispatch.
4. Reuse HeapAllocation eligibility after generic substitution.

### Phase 2: checker lifetime and diagnostics

1. Extend the existing freed-state lattice with Stash reset/destroy facts and
   Pool slot/pool-destroy facts for directly tracked bindings.
2. Integrate the new effects with branch merges, loops, assignment versions,
   `defer`, and use-after-release checks without adding alias analysis.
3. Add exact diagnostics for invalid methods, Stash/Pool type-argument and
   constructor errors, explicit type arguments on Stash allocation, Stash or
   Pool element mismatch, zero Pool capacity, Stash individual release, local
   stale use, repeated cleanup, and library constructors receiving non-Heap
   allocators.
4. Verify copied/escaped aliases retain the documented undecided acceptance.

### Phase 3: Stash runtime and lowering

1. Add `stash.h/.c` templates with immutable element size/alignment, control and
   block lifecycle, checked block growth, bump alignment, whole-chain reset
   retention, and destruction.
2. Add typed Stash constructor/allocation helpers at the module/program
   location selected by T. Construction records `sizeof(T)` and `_Alignof(T)`
   once; allocation supplies only the initializer.
3. Add the Stash render model, demand discovery, include ordering, and component
   tests; no Stash use emits Pool or collection allocator machinery.

### Phase 4: Pool runtime and lowering

1. Add `pool.h/.c` templates for non-specialized core validation and ownership.
2. Emit monomorphized `Pool<T>` state/allocate/release helpers using existing
   constructed-type ownership and C-name resolution.
3. Implement checked construction sizes, O(1) free stack, live bytes, address
   validation, exhaustion trap, empty-destroy requirement, and direct `free`.
4. Add demand/include tests for builtin, nominal, imported, and same-named
   module-owned T without duplicate definitions.

### Phase 5: conformance and canonical docs

1. Implement every Validation item below and no additional behavior.
2. Rebuild the snippet manifest once. No existing entry changes hash; the
   manifest gains entries only for new Stash/Pool snippets.
3. Update `docs/reference.md` once with the grammar/type/API/lifetime/C23 rules;
   remove Stash/Pool from Excluded features without restating the Heap
   contract already stated there.
4. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
5. Rebuild and restart the workbench.
6. Remove this RFC from open status, mark it Closed with an execution summary,
   and move it to `docs/specs/archive/` in the same change only after code,
   tests, artifacts, and canonical docs agree.

## Validation

This section is exhaustive.

### Type and checker validation

- `Stash` and `Pool` are protected; each requires exactly one complete, finite,
  copyable HeapAllocation-valid type after substitution. Bare Stash/Pool,
  zero/multiple arguments, Unknown, direct Atomic, and function elements are
  rejected.
- `Stash<T>.new()` accepts no arguments and infers `Stash<T>` for an inferred
  binding; passing a Heap or any other value argument is rejected.
- `Pool<T>.new(capacity)` accepts exactly one `Size` argument and infers
  `Pool<T>` for an inferred binding; the former Heap-plus-capacity shape and
  every other arity are rejected.
- Stash/Pool handles copy shallowly and remain invalid for equality, ordering,
  hashing, and print.
- `Stash<T>.allocate(initial)` requires one initializer assignable to T,
  accepts no explicit method type arguments, and always returns `MutPtr<T>`.
- A union-typed Stash accepts each union member through ordinary contextual
  injection, rejects values outside that union, and still returns a pointer to
  the union rather than a pointer to the injected member.
- `Stash<Int32>` and `Stash<Int64>` are distinct Hexal types; handles and
  allocations cannot cross between them.
- Stash has only `allocate`, `reset`, and `destroy`; individual `free(pointer)`
  is rejected with `Stash allocations are released by reset or destroy`.
- Stash reset permits later allocation. Directly tracked old allocations and
  Views reject use after reset/destroy; directly tracked handles reject use or
  repeated destroy after destroy.
- Pool construction rejects constant zero capacity; dynamic zero,
  unrepresentable capacity, and exhaustion lower to the required traps.
- Pool allocate accepts exactly T; Pool free accepts Ptr/MutPtr of exactly T.
- Free through a pointer directly traceable to another Pool is rejected before
  generation; unknown provenance reaches the runtime address check.
- Directly tracked Pool slots reject use/release after free or Pool destroy.
  Reallocation produces a fresh tracked binding version.
- Pool destroy rejects a directly tracked live slot, otherwise uses the runtime
  empty-slot contract for unknown aliases, and invalidates the directly tracked
  Pool handle.
- Copied, parameter, member, collection, and escaped pointer/handle aliases
  remain outside local tracking and are accepted consistently with current Heap
  policy; no diagnostic claims generation safety.
- This RFC introduces no concurrency-specific rejection: sharing one Stash or
  Pool across Tasks remains governed by the documented unsynchronized-conflict
  boundary until RFC 0118 supplies its static ownership/unsafe rules.
- Stash and Pool operations emit no lock, atomic, or other synchronization
  primitive; adding implicit synchronization violates this RFC.
- String/List/Dict/Channel/Mutex constructors and cleanup reject Stash and Pool
  and continue to accept Heap exactly.
- `defer stash.destroy()`, `defer pool.destroy()`, and deferred Pool slot free
  capture receiver and arguments once and participate in existing duplicate
  cleanup checking.

### Generated-C validation

- Stash and Pool construction calls the default allocation primitives directly;
  neither generated state nor generated constructor signatures contain a Heap
  value or parent-allocator field.
- Each Stash constructor records exactly one immutable `sizeof(T)` and
  `_Alignof(T)` pair in the state. Allocation reuses that pair and emits no
  per-allocation type tag, descriptor, size, or alignment argument.
- Distinct `Stash<T>` source types may share the `hex_stash *` handle
  representation, but typed constructor/allocation helper identities include
  canonical T. Same-named nominal types from different modules never collide.
- Stash emits one component pair only when selected, uses the specified 4096
  minimum/doubling policy, retains and rewinds every block without zeroing on
  reset, reuses retained blocks before growth, and frees all state on destroy.
- Stash allocation uses the recorded `_Alignof(T)` and `sizeof(T)`, checks
  padding and capacity before pointer formation, and evaluates/writes the
  initializer once.
- Pool emits one core component and exactly one correctly owned specialization
  per canonical T; same-named types from different modules never collide.
- Pool construction uses checked sizes; allocation/release are O(1); release
  validates Pool range, slot boundary, and live byte before changing state.
- Pool exhaustion and non-empty destroy emit distinct defined runtime traps.
- Stash reset/destroy and Pool free/destroy never invoke cleanup for stored T or
  handles contained by T.
- No parent-allocator state, allocator descriptor, function-pointer dispatch,
  family tag, Pool generation, Stash-backed collection path, or alternate
  backing path is emitted.
- Existing Heap, collection, concurrency, and IO artifacts remain
  byte-identical. The manifest gains entries only for new Stash/Pool snippets;
  repeated compilation is byte-identical.
- Ordinary tests remain pure Go. Runtime trap firing remains a known coverage
  gap until RFC 0055 can execute generated programs.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

After behavior stabilizes, update `docs/reference.md` once to:

- add `Stash<T>` and `Pool<T>` to protected/core types and remove them from
  exclusions;
- add the exact APIs, type eligibility, shallow-copy, local invalidation,
  allocator-family restrictions, Pool alias limitation, and trap contracts;
- state that each Stash has one canonical T, `allocate` has no type arguments,
  and heterogeneous storage requires an explicit union T;
- state that Stash and Pool constructors take no Heap argument and always use
  the default allocation primitives internally;
- narrow the existing no-hidden-allocator rule to Heap-backed library values;
  Stash and Pool are independent allocator roots, not Heap-backed library
  values;
- state that Stash and Pool are not thread-safe, shallow copies add no
  synchronization, and RFC 0118 owns cross-task rejection/unsafe rules;
- state that library allocations remain Heap-only;
- record Stash and Pool C23 representation contracts without implementation
  walkthroughs or examples.

No canonical documentation changes before implementation.
