# RFC 0027: Arena and Pool Allocators

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; initial design proposed
- Features: explicit Arena allocation, typed Pool allocation, bulk release,
  allocator passing, and collection integration
- Created: 2026-08-11
- Depends on: RFC 0019 (generics), RFC 0020 (collections), RFC 0026
  (allocation, deallocation, and deferred cleanup), and RFC 0035 (C-style
  copying and manual lifetimes), and RFC 0036 (`Size`)
- Coordinates with: RFC 0029 (Error values) and the future concurrency and FFI
  specifications

## Summary

Seawitch adds two concrete allocator families alongside `Heap`:

```seawitch
h: Heap = Heap.new()

arena: Arena = Arena.new(h)
defer arena.destroy(h)

nodes: Pool<Node> = Pool<Node>.new(h, 128)
defer nodes.destroy(h)
```

`Arena` serves variable-size allocations and releases all of its storage as one
region. `Pool<T>` owns a fixed number of reusable slots for one concrete `T`
and supports individual allocation and release.

The allocators are built-in concrete types. This RFC adds no allocator trait,
capability syntax, virtual dispatch, or user-defined allocator implementation.

## Goals

1. Preserve explicit allocator passing.
2. Reuse RFC 0026's initialization, provenance, and cleanup rules.
3. Make region allocation cheap and simple.
4. Make fixed-type, fixed-capacity allocation predictable.
5. Let `Arena` back String, List, and Dict without changing their source APIs.
6. Keep allocator identity available to generated collection helpers.

## Arena

An Arena is a runtime owner backed by another allocator. V1 accepts `Heap` as
its backing allocator:

```seawitch
h: Heap = Heap.new()
arena: Arena = Arena.new(h)
defer arena.destroy(h)
```

`Arena.new(h)` creates arena state but may defer allocating its first block
until the first allocation. The Arena retains the complete backing allocator
descriptor needed to grow and destroy itself.

Allocation uses RFC 0026's explicit initialized form:

```seawitch
node: MutPtr<Node> = arena.allocate<Node>(Node { value = 1 })
```

An Arena allocation cannot be individually released:

```seawitch
arena.free(node)
// Error: Arena allocations are released by reset or destroy
```

`arena.reset()` invalidates every allocation made since construction or the
previous reset while retaining implementation-selected blocks for reuse.
`arena.destroy(h)` invalidates every remaining allocation and releases all
arena storage through its backing Heap. The programmer must ensure that no
allocation or View from the Arena is used after reset or destroy.

Arena allocations are aligned for their concrete allocated type. Requests that
cannot satisfy size or alignment requirements use the allocation-failure policy
eventually finalized with RFC 0029; until then, failure is an unrecoverable
allocation trap as in RFC 0026.

## Arena-backed text and collections

`Arena` is accepted wherever RFC 0018 or RFC 0020 accepts a variable-size
allocator:

```seawitch
text: String = "hello".to_string(arena)
values: List<Int32> = List<Int32>.new(arena)
scores: Dict<Strand, Int32> = Dict<Strand, Int32>.new(arena)
```

The generated collection header retains a concrete allocator descriptor, not
only RFC 0020's current Heap identity token. The descriptor identifies the
allocator family, its context, and the generated allocation, resize, and
release operations required by that concrete collection specialization.

Calling `value.free(arena)` still performs type-directed cleanup and releases
or abandons its backing blocks according to Arena policy. For an Arena,
releasing raw storage may be a no-op until reset or destroy.

If an Arena-backed value contains resources requiring their own explicit
cleanup, the programmer must perform that cleanup before `arena.reset()` or
`arena.destroy(h)`. Bulk byte release does not implicitly run element cleanup.

## Pool<T>

`Pool<T>` is a built-in generic allocator for exactly one complete concrete
type and one fixed runtime `Size` capacity:

```seawitch
h: Heap = Heap.new()
nodes: Pool<Node> = Pool<Node>.new(h, 128)
defer nodes.destroy(h)
```

Capacity must be positive and fit the implementation's supported allocation
size. Pool construction allocates its control state and slot storage through
the supplied Heap.

```seawitch
node: MutPtr<Node> = nodes.allocate(Node { value = 1 })
nodes.free(node)
```

`allocate(initial)` initializes one free slot and returns `MutPtr<T>`. It traps
under RFC 0026's current allocation-failure policy when no slot is available.
`free(pointer)` validates that the pointer names one currently live slot from
that exact Pool, invalidates all aliases to the slot, and makes the slot
available for reuse.

`nodes.destroy(h)` requires every slot to have been freed. It then releases the
Pool's storage through the same backing Heap and invalidates every copied Pool
handle. Destroying a non-empty Pool is a defined runtime allocation-state
failure.

`Pool<T>` is not a general variable-size allocator. It cannot back String,
List, Dict, Arena blocks, or a `Pool<U>` where `U` differs canonically from
`T`. This avoids hidden fallback allocations and variable-size behavior in a
fixed-slot abstraction.

## Copying and lifetime

Arena and Pool are reference-like values under RFC 0035. Assignment, argument
passing, return, and aggregate storage copy the handle; all copies refer to the
same allocator state. No copy owns the state independently.

The programmer must arrange exactly one `destroy` for that state and must stop
using every copied handle and allocation after reset or destroy. Runtime
metadata validates allocator provenance, live Pool slots, and repeated destroy
when practical; the compiler does not track ownership or aliases.

`mut` controls whether an allocator binding can be reassigned. It does not
create ownership and does not change allocator behavior.

## Deferred cleanup

Allocator cleanup uses ordinary RFC 0026 `defer`:

```seawitch
arena: Arena = Arena.new(h)
defer arena.destroy(h)

pool: Pool<Node> = Pool<Node>.new(h, 64)
defer pool.destroy(h)
```

Direct-call capture, reverse ordering, branch scopes, and loop scopes remain
unchanged. Registering deferred destruction and also destroying through any
alias is a programmer error because the deferred call will later destroy the
same state again.

## C23 lowering direction

- Arena lowers to a pointer-sized handle to a C state object containing its
  backing allocator descriptor and block list.
- Arena allocation uses aligned bump allocation and grows by adding blocks.
- Arena reset rewinds or releases blocks without inspecting untyped payloads.
- `Pool<T>` lowers to a monomorphized state containing aligned `T` slots, live
  state, and a free-slot data structure.
- Pool allocation and release use direct generated helpers; no source-level
  virtual dispatch or trait object is emitted.
- Collection allocator descriptors are generated private C structures. They
  are implementation details and not Seawitch values.
- Every size multiplication and alignment calculation is checked before C
  allocation or pointer formation.

## Deferred and open design work

- Recoverable allocation exhaustion through RFC 0029 Error values.
- Arena construction backed by another Arena.
- Dynamically growing Pools.
- Thread-safe Arena and Pool variants.
- Region-owned typed destructor registration.
- User-defined allocators and a public allocator interface.
- Stack-backed Arena storage.
- Pool iteration and diagnostics exposing live slots.

## Draft readiness questions

Before implementation readiness, this RFC must settle:

1. whether Arena reset retains all blocks or may return large blocks to Heap;
2. the exact generated allocator descriptor needed by RFC 0020 growth helpers;
3. whether Pool exhaustion traps first or immediately returns RFC 0029 Error;
4. whether `destroy` is the permanent allocator-owner cleanup spelling; and
5. the runtime strategy for validating stale Arena pointers after reset.
