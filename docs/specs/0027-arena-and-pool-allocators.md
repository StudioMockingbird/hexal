# RFC 0027: Arena and Pool Allocators

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Features: explicit Arena allocation, typed Pool allocation, region release,
  allocator passing, and default-Heap runtime simplification
- Created: 2026-08-11
- Updated: 2026-08-24
- Depends on: the current Heap, pointer, collection, cleanup, and shallow-copy
  contracts in `docs/reference.md`
- Coordinates with: RFC 0039 (C interoperability), RFC 0052 (target profiles),
  RFC 0110 (affine ownership and allocator lifetimes), and RFC 0118
  (concurrency safety)
- Changes the protected type set and allocator APIs; reference synchronization
  is required only after implementation stabilizes

## Summary

Hexal adds two concrete allocator families alongside `Heap`:

```hexal
h: Heap := Heap.new()

arena: Arena := Arena.new(h)
defer arena.destroy(h)

nodes: Pool<Node> := Pool<Node>.new(h, 128)
defer nodes.destroy(h)
```

`Arena` serves variable-size allocations and releases all of its storage as one
region. `Pool<T>` owns a fixed number of reusable slots for one concrete `T`
and supports individual allocation and release.

The allocators are built-in concrete types. This RFC adds no allocator trait,
capability syntax, virtual dispatch, or user-defined allocator implementation.

## Goals

1. Preserve explicit allocator passing.
2. Reuse the reference's current initialization, provenance, and cleanup rules.
3. Make region allocation cheap and simple.
4. Make fixed-type, fixed-capacity allocation predictable.
5. Keep existing String, List, Dict, Channel, and Mutex allocation Heap-only.
6. Remove runtime identity and per-allocation headers from the sole default Heap.

## Current compatibility boundary

The current language has exactly one allocator type, `Heap`. `String`, List,
Dict, Channel, and Mutex signatures accept `Heap` exactly; their generated C
stores only the default Heap identity and calls Heap-owned helpers. There is no
allocator trait, descriptor, union, overload, or allocator type parameter to
which `Arena` can be passed.

The current language also uses shallow copies and local freed-state checking.
It has no affine owner, generation-bearing Pool slot, general unsafe boundary,
or proof that every alias dies before reset, release, or destroy. This RFC must
not claim stronger stale-pointer detection than that model can implement.

`Arena` and `Pool` remain excluded from the language until this RFC is
implemented and synchronized into `docs/reference.md`.

## Arena

An Arena is a runtime owner whose storage comes from the one default `Heap`:

```hexal
h: Heap := Heap.new()
arena: Arena := Arena.new(h)
defer arena.destroy(h)
```

`Arena.new(h)` allocates its control state and defers its first data block until
the first allocation. Heap is a stateless default-allocator token; the Arena
retains no allocator descriptor or function table. The explicit Heap argument
is evaluated and type-checked but has no runtime identity.

Allocation uses the current explicit initialized form:

```hexal
node: MutPtr<Node> := arena.allocate<Node>(Node { value = 1 })
```

An Arena allocation cannot be individually released:

```hexal
arena.free(node)
// Error: Arena allocations are released by reset or destroy
```

`arena.reset()` invalidates every allocation made since construction or the
previous reset. The initial block is rewound and retained; later blocks are
returned to the backing Heap. This bounds retained peak storage without adding
a tuning surface or block cache.
`arena.destroy(h)` invalidates every remaining allocation and releases all
arena storage through its backing Heap. The programmer must ensure that no
allocation or View from the Arena is used after reset or destroy.

Arena allocations are aligned for their concrete allocated type. Construction,
growth, unrepresentable size/alignment, and allocation failure trap under the
same policy as `Heap.allocate`; this RFC adds no recoverable allocation Error.

The checker rejects a reset/destroy followed by a use through the same locally
tracked Arena allocation or View, including all-path branch facts. Aliased,
escaped, parameter-reached, member-reached, and collection-reached pointers use
the current undecided-case policy: they are not proven safe and receive no
guaranteed runtime stale-pointer diagnostic. Arena dereferences gain no runtime
generation check.

## Library-allocation boundary

V1 does not make Arena a general library allocator. These remain Type Errors:

```hexal
List<Int32>.new(arena)
Dict<Int32, Int32>.new(arena)
"text".to_string(arena)
Channel<Int32>.new(arena, 4)
Mutex.new(arena)
```

String, List, Dict, Channel, and Mutex keep their exact Heap signatures and C
representations. No allocator trait, function-pointer descriptor, family tag,
constructor overload, or allocator type parameter is introduced.

An Arena allocation may contain handles to separately owned resources. Reset
and destroy release only Arena bytes; they do not invoke cleanup for stored
values. The programmer cleans those resources before invalidating the region.

## Pool<T>

`Pool<T>` is a built-in generic allocator for exactly one complete concrete
type and one fixed runtime `Size` capacity:

```hexal
h: Heap := Heap.new()
nodes: Pool<Node> := Pool<Node>.new(h, 128)
defer nodes.destroy(h)
```

Capacity must be positive and fit the implementation's supported allocation
size. Pool construction allocates its control state and slot storage through
the supplied Heap.

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

`nodes.destroy(h)` requires every slot to have been freed. It then releases the
Pool's storage through the same backing Heap and invalidates every copied Pool
handle. Destroying a non-empty Pool is a defined runtime allocation-state
failure.

`Pool<T>` is not a general variable-size allocator. It cannot back String,
List, Dict, Channel, Mutex, Arena blocks, or a `Pool<U>` where `U` differs
canonically from `T`. This avoids hidden fallback allocations and variable-size
behavior in a fixed-slot abstraction.

Pool release destroys no T and runs no cleanup. If T contains handles to
separately owned resources, the programmer cleans them before releasing the
slot. Pool destruction requires every slot released and therefore never walks
or destroys stored T values.

## Copying and lifetime

Arena and Pool follow the reference's current shallow-copy rule. Assignment,
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

## Deferred cleanup

Allocator cleanup uses ordinary `defer`:

```hexal
arena: Arena := Arena.new(h)
defer arena.destroy(h)

pool: Pool<Node> := Pool<Node>.new(h, 64)
defer pool.destroy(h)
```

Direct-call capture, reverse ordering, branch scopes, and loop scopes remain
unchanged. Registering deferred destruction and also destroying through any
alias is a programmer error because the deferred call will later destroy the
same state again.

## Exact source API

```text
Arena.new(heap: Heap) -> Arena
Arena.allocate<T>(initial: T) -> MutPtr<T>
Arena.reset() -> no value
Arena.destroy(heap: Heap) -> no value

Pool<T>.new(heap: Heap, capacity: Size) -> Pool<T>
Pool<T>.allocate(initial: T) -> MutPtr<T>
Pool<T>.free(pointer: Ptr<T> | MutPtr<T>) -> no value
Pool<T>.destroy(heap: Heap) -> no value
```

- `Arena` and `Pool` become protected names; `Pool` requires exactly one type
  argument wherever used.
- `Arena.allocate<T>` applies the exact `HeapAllocation` eligibility and
  explicit-initializer rules of `Heap.allocate<T>`.
- `Pool<T>` requires complete, finite, copyable `T` valid for HeapAllocation;
  direct Atomic and function-value slots remain invalid.
- A constant zero Pool capacity is rejected statically. A dynamic zero capacity
  traps. Unrepresentable capacity/size and construction failure trap.
- Arena/Pool handles are pointer-sized, shallow-copyable aliases under the
  current language contract. `mut` changes only binding reassignment.
- `destroy(heap)` ends allocator state. It is distinct from
  `Pool.free(pointer)`, which releases one slot.

## Default Heap simplification

The one default Heap no longer gives each allocation a compiler-owned header.
Generated allocation uses checked `calloc`; release uses C `free` directly.

- `Heap` remains a source value passed explicitly, but has no runtime allocator
  identity. All Heap values select the same default allocator.
- Heap lowers to `typedef unsigned char hex_heap`; `Heap.new()` lowers to
  `(hex_heap)0`. The byte is a C value representation, not an allocator ID.
- The runtime core is exactly
  `void *hex_heap_allocate(size_t bytes)` plus
  `void hex_heap_free(void *pointer)`. The first calls `calloc(1, bytes)` after
  its caller has checked every byte calculation; the second calls `free`.
- `Heap.allocate<T>(initial)` checks the byte count, calls `calloc(1, bytes)`,
  traps on null, and writes the initializer exactly once.
- Heap-backed runtime values use the same checked `calloc` helper for dynamic
  byte counts and C `free` for release.
- Hexal has no over-aligned source type. The supported targets guarantee that
  `calloc` satisfies every alignment expressible by a current Hexal type.
- Remove `hex_heap_header`, offset markers, allocator/live fields, alignment
  rounding, wrong-allocator branches, and the claim that released metadata can
  safely diagnose an escaped double free.
- Existing local freed-state diagnostics remain. Escaped/aliased invalid frees
  retain the current manual-lifetime boundary; this RFC adds no allocation
  registry or generation table.

## C23 representation

- `Arena` is `hex_arena *`. Its control block owns a linked list of blocks and
  the current bump position.
- The first allocation creates a 4096-byte data block or the smallest larger
  checked capacity satisfying that allocation, whichever is greater.
- Later growth doubles the previous block capacity until it satisfies the
  checked request; if doubling is unrepresentable, it uses the exact checked
  required capacity. Every block comes from checked `calloc`.
- Allocation aligns the bump position for `T`, checks every addition, writes
  the initializer exactly once, and advances the bump position.
- Reset rewinds and zeroes the retained first block, then frees every later
  block. Destroy frees every block and the control state.
- `Pool<T>` is a monomorphized pointer-sized handle to state containing its
  fixed capacity, free count, contiguous aligned `T` slots, one live byte per
  slot, and a `Size` free-index stack. Construction initializes the stack so
  allocation and release are O(1).
- Pool allocation pops one index, marks it live, writes `T` once, and returns
  its address. Release converts addresses through `uintptr_t`, validates range,
  slot alignment, and live state before pushing the index.
- Pool destroy traps when `free_count != capacity`; otherwise it frees slot,
  live-state, free-index, and control allocations.
- Arena and Pool runtime cores live in `generator/packages/arena.c/.h` and
  `pool.c/.h`; typed allocation/Pool helpers follow existing constructed-type
  ownership so module-owned `T` never leaks into a program-wide header.
- No allocator descriptor, virtual dispatch, hidden fallback allocation, or
  runtime generation check is emitted.
- Every size multiplication, alignment calculation, block growth, and pointer
  offset is checked before allocation or pointer formation.

## Diagnostics and traps

- Invalid type arguments, missing initializers, wrong allocator families,
  unsupported methods, and locally proved stale uses are Type Errors owned by
  the checker.
- `arena.free(pointer)` reports `Arena allocations are released by reset or
  destroy`.
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
[Runtime Error] pointer does not name a slot in this Pool
[Runtime Error] pool slot is not live
[Runtime Error] pool destroy with live slots
```

Each generated message ends with `\n`. Arena allocation uses the existing Heap
size/failure messages. No allocation, reset, release, or destroy returns Error.

## Deferred work

- Recoverable allocation exhaustion through Error values.
- Arena construction backed by another Arena.
- Dynamically growing Pools.
- Thread-safe Arena and Pool variants.
- Region-owned typed destructor registration.
- User-defined allocators and a public allocator interface.
- Stack-backed Arena storage.
- Pool iteration and diagnostics exposing live slots.

## Required sweep

- Replace the default Heap header allocator with checked `calloc`/`free` and
  remove every identity/header/offset/live-field dependency from Heap,
  collections, IO, concurrency adapters, deferred cleanup, and generated tests.
- Preserve source evaluation of every explicit Heap argument even where its C
  value is operationally irrelevant.
- Do not retain a second Heap allocation path for artifact stability.
- Keep String/List/Dict/Channel/Mutex signatures Heap-only and reject Arena or
  Pool arguments; add no allocator descriptor scaffolding.
- Reuse existing constructed-type interning, C-name collision handling,
  component ownership, position eligibility, freed-state flow, and defer
  capture machinery instead of parallel implementations.
- Extend cleanup flow only with Arena reset/destroy and Pool free/destroy
  invalidation facts. Do not infer aliases beyond the reference's current local
  boundary.
- Keep runtime templates as `.c`/`.h` files under `generator/packages`, never Go
  strings.

## Implementation plan

### Implementation map

| Area | Required work |
|---|---|
| `compiler/types` | Register protected Arena and canonical `Pool<T>` construction, eligibility, identity, C names, and ownership classification. |
| `compiler/checker/alloc.go` and flow state | Check all APIs, reuse HeapAllocation rules, record reset/release/destroy effects, integrate defer and branch merges, and preserve the local-analysis boundary. |
| checker/generator dispatch coverage | Add explicit nodes for every new operation and fail closed for invalid metadata. |
| `compiler/generator/heap_component.go` and `generator/packages/heap.*` | Collapse Heap to stateless checked `calloc`/direct `free` and remove header identity machinery. |
| new Arena/Pool component models and `generator/packages/arena.*`, `pool.*` | Emit demand-driven runtime cores and typed helpers with existing component ownership. |
| collection, text, concurrency, IO, and defer generators | Remove Heap identity/header adaptation while retaining exact Heap-only APIs and evaluation order. |
| unit/integration/component tests | Cover type construction, flow diagnostics, generated layouts, component selection, C ordering, deterministic output, and every Validation item. |
| workbench snippets and manifest | Add focused Arena/Pool snippets and attribute every changed existing hash to the Heap collapse. |

### Phase 0: baseline and migration inventory

1. Record the green test/vet baseline and snippet manifest.
2. Record Heap-only, direct allocation, deferred free, String/List/Dict,
   Channel/Mutex, IO, and no-allocation artifacts.
3. Inventory every `hex_heap_header`, identity, raw allocate/free, allocator
   field, typed helper, adapter, and assertion in checker and generator code.
4. Add focused rejected-source probes for Arena/Pool names before registration,
   then retain them as the before/after language boundary.

### Phase 1: collapse the default Heap runtime

1. Update `compiler/generator/packages/heap.h` and `heap.c` to the stateless Heap
   value plus checked `calloc`/`free` core.
2. Update `heap_component.go`, typed module helpers, collection/runtime package
   templates, concurrency adapters, IO, and deferred cleanup to remove runtime
   identity/header arguments while preserving Heap-expression evaluation.
3. Update `compiler/checker/alloc.go` only where checked metadata no longer
   carries an allocator identity; preserve every current local lifetime error.
4. Remove swept code and regenerate only Heap-affected manifest entries.

### Phase 2: register Arena and Pool types

1. Add protected `Arena` and constructed `Pool<T>` identities in
   `compiler/types`, including canonical keys, display/C names, placement,
   copyability, equality/print rejection, and program/module ownership.
2. Extend type-use resolution for exactly one Pool argument and reserve both
   protected names in every environment.
3. Add checked expression kinds and metadata for constructors, allocate, reset,
   free, and destroy; include them in fail-closed checker/generator dispatch.
4. Reuse HeapAllocation eligibility after generic substitution.

### Phase 3: checker lifetime and diagnostics

1. Extend the existing freed-state lattice with Arena reset/destroy facts and
   Pool slot/pool-destroy facts for directly tracked bindings.
2. Integrate the new effects with branch merges, loops, assignment versions,
   `defer`, and use-after-release checks without adding alias analysis.
3. Add exact diagnostics for invalid methods, wrong arguments, zero capacity,
   Arena individual release, Pool element mismatch, local stale use, repeated
   cleanup, and library constructors receiving non-Heap allocators.
4. Verify copied/escaped aliases retain the documented undecided acceptance.

### Phase 4: Arena runtime and lowering

1. Add `arena.h/.c` templates with control/block lifecycle, checked block growth,
   bump alignment, reset retention, zeroing, and destruction.
2. Add typed Arena allocation helpers at the module/program location selected
   by the allocated type.
3. Add the Arena render model, demand discovery, include ordering, and component
   tests; no Arena use emits Pool or collection allocator machinery.

### Phase 5: Pool runtime and lowering

1. Add `pool.h/.c` templates for non-specialized core validation and ownership.
2. Emit monomorphized `Pool<T>` state/allocate/release helpers using existing
   constructed-type ownership and C-name resolution.
3. Implement checked construction sizes, O(1) free stack, live bytes, address
   validation, exhaustion trap, empty-destroy requirement, and direct `free`.
4. Add demand/include tests for builtin, nominal, imported, and same-named
   module-owned T without duplicate definitions.

### Phase 6: conformance and canonical docs

1. Implement every Validation item below and no additional behavior.
2. Rebuild the snippet manifest once; unchanged no-allocation artifacts remain
   byte-identical and every moved family is attributed to Heap collapse or new
   Arena/Pool snippets.
3. Update `docs/reference.md` once with the grammar/type/API/lifetime/C23 rules;
   remove Arena/Pool from Excluded features and replace superseded Heap
   header/identity statements.
4. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
5. Rebuild and restart the workbench.
6. Remove this RFC from open status, mark it implemented, and archive it only
   after code, tests, artifacts, and canonical docs agree.

## Validation

This section is exhaustive.

### Type and checker validation

- `Arena` and `Pool` are protected; Pool requires exactly one complete finite
  copyable HeapAllocation-valid type after substitution.
- Arena/Pool handles copy shallowly and remain invalid for equality, ordering,
  hashing, and print.
- Arena allocate requires one explicit initializer and rejects Unknown, direct
  Atomic, function values, and every existing HeapAllocation-invalid type.
- Arena has only `allocate`, `reset`, and `destroy`; individual `free(pointer)`
  is rejected with `Arena allocations are released by reset or destroy`.
- Arena reset permits later allocation. Directly tracked old allocations and
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
- String/List/Dict/Channel/Mutex constructors and cleanup reject Arena and Pool
  and continue to accept Heap exactly.
- `defer arena.destroy(h)`, `defer pool.destroy(h)`, and deferred Pool slot free
  capture receiver and arguments once and participate in existing duplicate
  cleanup checking.

### Generated-C validation

- Heap allocation uses checked `calloc`, writes its initializer once, and uses
  direct C `free`; generated artifacts contain no `hex_heap_header`, offset
  marker, allocator identity, or live flag.
- Every explicit Heap source expression is evaluated once even when no runtime
  identity is passed.
- Checked multiplication/addition precedes every `calloc`; null traps through
  the existing allocation-failure message.
- Arena emits one component pair only when selected, uses the specified 4096
  minimum/doubling policy, retains and zeroes only the first block on reset,
  and frees all state on destroy.
- Arena allocation applies `_Alignof(T)` and `sizeof(T)`, checks padding and
  capacity before pointer formation, and evaluates/writes the initializer once.
- Pool emits one core component and exactly one correctly owned specialization
  per canonical T; same-named types from different modules never collide.
- Pool construction uses checked sizes; allocation/release are O(1); release
  validates Pool range, slot boundary, and live byte before changing state.
- Pool exhaustion and non-empty destroy emit distinct defined runtime traps.
- Arena reset/destroy and Pool free/destroy never invoke cleanup for stored T or
  handles contained by T.
- No allocator descriptor, function-pointer dispatch, family tag, Pool
  generation, Arena-backed collection path, or hidden Heap fallback is emitted.
- Heap-only collection, concurrency, and IO behavior is otherwise unchanged.
- Unaffected manifest entries remain byte-identical; repeated compilation is
  byte-identical.
- Ordinary tests remain pure Go. Runtime trap firing remains a known coverage
  gap until RFC 0055 can execute generated programs.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

After behavior stabilizes, update `docs/reference.md` once to:

- add Arena and Pool to protected/core types and remove them from exclusions;
- add the exact APIs, type eligibility, shallow-copy, local invalidation,
  allocator-family restrictions, Pool alias limitation, and trap contracts;
- state that library allocations remain Heap-only;
- replace the Heap runtime-identity/header description with one stateless
  default Heap using checked `calloc` and direct `free`; and
- record Arena and Pool C23 representation contracts without implementation
  walkthroughs or examples.

No canonical documentation changes before implementation.
