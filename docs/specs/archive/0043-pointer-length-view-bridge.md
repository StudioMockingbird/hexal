# RFC 0043: Pointer-Plus-Length View Bridge

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Features: explicit construction of read-only `View<T>` from one pointer and
  element count without source-level pointer arithmetic
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0007 (Ptr and MutPtr), RFC 0020 (`View<T>`), RFC 0033 (no
  pointer arithmetic), RFC 0035 (C-style lifetimes), and RFC 0036 (`Size`)
- Coordinates with: RFC 0039 (C interop), RFC 0040 (I/O), and future platform
  APIs

## Summary

An explicit constructor turns a pointer to the first element and an element
count into a read-only View:

```seawitch
items: View<Int32> = View<Int32>.from_pointer(pointer, count)
```

It creates only View's pointer-and-count descriptor. It does not allocate,
copy, take ownership, mutate storage, or expose source pointer arithmetic.

## Operations

The complete v1 surface is:

```text
View<T>.from_pointer(pointer: Ptr<T>, length: Size): View<T>
View<T>.from_pointer(pointer: MutPtr<T>, length: Size): View<T>
View<T>.empty(): View<T>
```

The explicit `from_pointer` name is the trust-boundary marker. No additional
`unsafe` keyword or block is added.

The MutPtr overload weakens write access. Every resulting View is read-only;
there is no `MutView<T>`.

T must satisfy RFC 0020's existing `View<T>` element rules. This RFC does not
broaden the permitted element set.

## Non-null input and empty Views

`from_pointer` requires a statically non-null Ptr or MutPtr. A nullable pointer
must be narrowed first:

```seawitch
data: Ptr<Byte> | Nil = foreign.data()
length: Size = foreign.length()

if data is Ptr<Byte> then
    bytes: View<Byte> = View<Byte>.from_pointer(data, length)
    consume(bytes)
end
```

`Nil` is rejected even when length is zero. Accepting `Nil` conditionally would
add a runtime rule to a constructor that otherwise only builds a descriptor.

Use `empty()` when there are no elements:

```seawitch
bytes: View<Byte> = View<Byte>.empty()
```

An empty View has count zero and may lower to a null C data pointer. Its data
pointer is never dereferenced. It remains subject to RFC 0020's ordinary View
storage and return restrictions.

## Validity contract

Calling `from_pointer` asserts that:

- pointer names the first of at least length contiguous T objects;
- each object is initialized and valid for read access;
- the region has T's alignment and effective type;
- the region remains live and at the same address while the View is used; and
- foreign or concurrent code does not mutate the region incompatibly or create
  a data race.

The compiler checks pointer type, T eligibility, non-null static type, and that
the length is assignable to `Size` under RFC 0016 and RFC 0036. Under RFC
0035's C-style lifetime model, it does not prove foreign extent, provenance,
initialization, or lifetime.

Breaking this contract is a programmer error at the explicit bridge. Bounds
checks protect every View index and slice against the recorded length, but
cannot discover that the caller supplied a false length.

## Source lifetime

The View is non-owning. Copying it copies only the descriptor. It never frees,
retains, or moves the pointed-to storage.

```seawitch
view: View<Int32> = View<Int32>.from_pointer(pointer, count)
h.free(pointer)
print(view[0]) // Programmer error: view is dangling.
```

The allocator or foreign API that produced the pointer remains responsible for
cleanup. Pointer reassignment does not retarget an existing View; its copied C
pointer remains unchanged.

RFC 0020's restrictions on returning, storing, nesting, and taking the address
of a View remain unchanged. RFC 0035 remains authoritative for manual lifetime
responsibility and adds no hidden borrow registry.

## No pointer arithmetic

Once constructed, ordinary checked View access is used:

```seawitch
value: Int32 = items[1]
part: View<Int32> = items.slice(1, 3)
```

The original pointer still cannot be indexed or advanced:

```seawitch
value: Int32 = pointer[1]        // Error.
second: Ptr<Int32> = pointer + 1 // Error.
```

Generated trusted View helpers may use C indexing after checking the index.
RFC 0033 remains unchanged for Seawitch source.

## Extent and overflow

The constructor stores pointer and element count. It does not calculate
`length * size_of<T>()`, so it needs no multiplication check or runtime trap.

The caller's validity contract guarantees that the declared element extent is
representable by the actual C object region. Checked indexing verifies
`index < length` before generated C forms `data[index]`.

No byte length, end pointer, or one-past pointer becomes visible to source.

## C interop

The bridge is the standard adaptation for a C API that returns a pointer and a
separate element count:

```seawitch
pointer: Ptr<Byte> = packet.data()
length: Size = packet.length()
bytes: View<Byte> = View<Byte>.from_pointer(pointer, length)
```

A complete imported C record is eligible only if RFC 0039 admits it and it
satisfies RFC 0020's View element rules. Opaque or incomplete records are
rejected. The bridge does not infer a length from C metadata or select a
deallocator.

For a nullable C pointer, the program narrows and constructs a View, or chooses
`View<T>.empty()` in its explicit Nil branch. I/O implementations may hide this
adaptation but do not receive broader language privileges.

## Evaluation and C23 lowering

The pointer expression is evaluated first and exactly once. The length
expression is evaluated second and exactly once. The constructor then lowers
to one View descriptor initialization equivalent to:

```c
(sw_view_T){ .data = pointer_value, .length = length_value }
```

The Ptr overload retains read-only pointee qualification. The MutPtr overload
performs existing MutPtr-to-Ptr weakening. Lowering emits no loop, allocation,
copy, ownership token, extent multiplication, or platform branch.

`empty()` lowers to the same descriptor with a null data pointer and zero
length.

## Diagnostics

Representative diagnostics are:

```text
[Type Error] View<Int32>.from_pointer requires Ptr<Int32> or MutPtr<Int32>
[Type Error] nullable pointer must be narrowed before View construction
[Type Error] View element type String is not supported
[Type Error] View length cannot be represented as Size
[Type Error] View element type c.FILE is incomplete
```

Parsing owns malformed type arguments or argument counts. Type checking owns
pointer compatibility, non-nullability, element eligibility, and length type.
Runtime bounds checking remains owned by RFC 0020.

## Non-goals

- writable Views;
- inferred pointer lengths;
- nullable-pointer special cases;
- ownership or lifetime tracking;
- allocation or copying;
- pointer indexing, stepping, subtraction, or address conversion;
- byte-oriented reinterpretation between unrelated element types; and
- automatic foreign cleanup.

## Acceptance criteria

The RFC is implemented when:

1. both Ptr and MutPtr overloads construct the same read-only View shape;
2. T is checked using exactly RFC 0020's View element rules;
3. the pointer is statically non-null and the length is accepted as `Size`
   under ordinary lossless conversion rules;
4. `empty()` constructs an allocation-free zero-length View;
5. pointer and length evaluate once, left to right;
6. construction performs no extent multiplication, allocation, or copy;
7. View indexing and slicing retain RFC 0020 bounds checks;
8. no pointer arithmetic or address operation becomes available;
9. manual source lifetime and cleanup remain programmer responsibility; and
10. invalid uses fail before C generation with structured diagnostics.
