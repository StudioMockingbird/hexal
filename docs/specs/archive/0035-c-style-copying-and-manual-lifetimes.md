# RFC 0035: C-Style Copying and Manual Lifetimes

- Kind: Architecture Decision Record (ADR)
- Status: Implemented; conformance verified 2026-08-11
- Decision: values copy as C values; allocation ownership and cleanup remain
  the programmer's responsibility
- Created: 2026-08-11
- Depends on: RFC 0001 (raw pointers), RFC 0006 (object values), RFC 0007
  (mutability), RFC 0018 (String), RFC 0020 (collections), and RFC 0026
  (allocation, deallocation, and defer)
- Supersedes on implementation: RFC 0018 and RFC 0020 rules that make owning
  String, List, or Dict handles affine, moved, borrowed, or statically tracked
  for exactly-once cleanup; RFC 0026 guarantees that ownership analysis rejects
  every double free or use after free

## Context

Seawitch is intended to remain a direct, understandable layer over C23. Its
existing collection specification introduced affine owners, move states,
compiler-tracked borrows, and mandatory cleanup-path analysis. Those rules are
semantically unlike C and make ordinary assignment depend on hidden compiler
state.

## Decision

Seawitch uses C-style value copying and manual lifetime management.

Assignment, argument passing, return, object construction, ADT construction,
and collection element insertion copy the source value's representation unless
a specific operation explicitly documents a deep copy. The source remains
usable after the copy. There is no general move operation or moved-from state.

For a reference-like value such as Ptr, String, List, Dict, Arena, Pool, or
Stream, copying copies only its handle or descriptor. It does not duplicate the
referenced allocation:

```seawitch
first: List<Int32> = List<Int32>.new(h)
second: List<Int32> = first

first.push(10)
print(second.length()) // 1: both handles refer to the same List
```

Objects and ADTs copy their members under the same rule:

```seawitch
type Group as {
    names: List<String>
}

group: Group = Group { names = names }
copy: Group = group
// group.names and copy.names refer to the same List.
```

The complete copy classification is:

| Value category | Copy behavior |
|---|---|
| Bool, numeric scalar, Byte, Rune, Nil | copy the scalar value |
| Strand, Array, object | copy the complete inline C value member by member |
| ADT or structural union | copy the tag and active payload; payload members follow this table |
| Ptr, MutPtr, function value | copy the C pointer value |
| String, List, Dict, Arena, Pool, Stream | copy the reference-like handle |
| View | copy its pointer-and-length descriptor |
| Heap | copy its compile-time allocator identity; no runtime object is created |

Copying is recursively shallow. An inline object containing a List copies the
List handle, not the List allocation.

## Explicit cleanup

The programmer must arrange exactly one valid cleanup for each runtime
allocation. `defer` is convenient but remains optional:

```seawitch
values: List<Int32> = List<Int32>.new(h)
alias: List<Int32> = values
defer values.free(h)

alias.push(10) // valid before cleanup
```

After one alias frees the allocation, every alias to that allocation is
invalid. The compiler does not track aliases, prove exactly-once cleanup, insert
automatic cleanup, or reject every possible use-after-free or double-free:

```seawitch
values.free(h)
alias.length()  // programmer error: dangling alias
alias.free(h)   // programmer error: double free
```

Use after free, repeated free, and freeing through the wrong allocator are
invalid programs. As in C, no behavior is guaranteed after the invalid
operation. Allocator implementations may trap when their live metadata detects
the error safely, but Seawitch does not require a global allocation registry,
quarantine freed memory, or promise that every invalid free is detectable.

The allocator supplied to `free` must match the allocator used to construct the
allocation. Arena and Pool bulk cleanup similarly invalidates every outstanding
handle into their storage.

## Mutability

`mut` controls whether a binding can be reassigned or a mutable operation can
be requested. It does not communicate ownership and does not affect copying or
cleanup responsibility:

```seawitch
values: List<Int32> = List<Int32>.new(h) // fixed binding, mutable List object
alias: List<Int32> = values              // shallow handle copy

mut current: List<Int32> = values
current = other                          // binding reassignment is allowed
```

Reassigning a handle does not free the allocation previously named by that
binding. The programmer must preserve another alias or free it first.

## Function boundaries

Parameters and results use ordinary value-copy semantics:

```seawitch
fun append_one(values: List<Int32>)
    values.push(1) // affects the shared List object
end

append_one(values)
print(values.length()) // 1
```

A function may call `free` on a passed handle, but doing so invalidates the
caller's aliases. APIs must document such behavior. Seawitch adds no ownership
annotation to distinguish consuming and non-consuming parameters.

## Aggregate and collection cleanup

Cleanup is shallow, matching C. Freeing an object does not recursively free
allocations referenced by its members. Freeing a List or Dict releases only the
collection's own header and storage; it does not free allocations referenced by
stored elements, keys, or values.

The programmer explicitly frees nested allocations before their container:

```seawitch
for name in names do
    name.free(h)
end
names.free(h)
```

`clear`, `pop`, `remove`, replacement, and Dict insertion likewise copy or
discard shallow values without hidden cleanup:

```seawitch
old: String = names.pop() // shallow String handle returned
old.free(h)               // programmer performs cleanup

previous: String = scores.get("alice")
previous.free(h)
scores.insert("alice", replacement)
```

Replacing or clearing the last handle to an allocation without first preserving
or freeing it leaks that allocation. Automatically walking nested values would
add destructor semantics that C does not have and is therefore rejected.

## Views

`View<T>` remains a non-owning pointer-and-length descriptor. Copying a View
copies that descriptor. Bounds checks protect indexing within the recorded
range, but the programmer must ensure that its source storage remains alive and
is not invalidated by resizing:

```seawitch
view: View<Int32> = values.view()
values.free(h)
print(view[0]) // programmer error: the View is dangling
```

No source-level pointer arithmetic is introduced; RFC 0033 remains unchanged.

## C23 lowering direction

- Inline values lower to ordinary C scalar or structure copies.
- Reference-like values lower to pointer-sized handles or small descriptors;
  assignment copies those values directly.
- The compiler emits no retain count, owner flag, move state, hidden clone,
  implicit destructor, or lexical borrow metadata.
- Explicit `free`, `destroy`, Arena reset, and Pool release lower to the
  corresponding generated runtime operation.
- Container cleanup releases only container-owned storage and emits no hidden
  recursive element cleanup.
- Bounds, UTF-8, union-tag, arithmetic, and nullability checks remain separate
  language guarantees; this decision removes only ownership and lifetime
  enforcement.

## Consequences

Benefits:

- assignment and parameter passing map directly to C;
- aggregate types may freely contain String, List, Dict, and other handles;
- no ownership checker or hidden move semantics are required; and
- allocator APIs remain explicit and predictable.

Costs:

- shallow aliases can dangle after cleanup;
- leaks, double frees, use-after-free, allocator mismatch, and invalidated Views
  are the programmer's responsibility;
- APIs must document cleanup responsibility; and
- the language can no longer guarantee that every compiling program is free of
  memory-lifetime errors.

These costs are accepted to preserve C semantics.

## Required conformance updates

Implementing this ADR requires later conformance updates, without editing the
closed predecessor specifications, to remove:

1. live, moved, freed, owner, and borrowed handle states;
2. mandatory all-path cleanup checking;
3. rejection of shallow String, List, and Dict copies;
4. owner-versus-borrow parameter and return distinctions;
5. restrictions against storing allocation-backed handles in objects and ADTs;
6. compiler-tracked View and collection-element borrow lifetimes; and
7. diagnostics based only on the removed ownership model.

## Implementation acceptance criteria

Implementation is complete when:

1. every value category has a stated C-compatible copy representation;
2. every allocating API identifies its matching explicit cleanup operation;
3. the implementation plan lists every closed RFC 0018, RFC 0020, and RFC 0026
   ownership clause and diagnostic removed by this decision;
4. aggregate fields and collection elements containing shallow handles have
   defined copy and cleanup behavior; and
5. invalid free is treated as a programmer error with no guaranteed detection,
   while safe allocator diagnostics remain permitted; and
6. freeing or clearing a container never recursively frees allocations named by
   its shallow elements.
