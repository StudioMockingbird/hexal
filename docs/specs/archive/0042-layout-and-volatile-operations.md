# RFC 0042: Layout and Volatile Operations

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Features: `size_of<T>()`, `align_of<T>()`, and explicit volatile integer
  pointer reads and writes
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0003 (scalar layouts), RFC 0006 (objects), RFC 0007
  (Ptr/MutPtr), RFC 0019 (generics), RFC 0020 (arrays), RFC 0032 (bit
  operations), RFC 0033 (no pointer arithmetic), and RFC 0036 (`Size`)
- Coordinates with: RFC 0039 (C interop), RFC 0044 (String and Byte cleanup),
  and future target-profile and embedded-platform specifications

## Summary

Seawitch exposes two type-layout queries and per-access volatile integer
operations:

```seawitch
bytes: Size = size_of<Node>()
alignment: Size = align_of<Node>()

status: UInt32 = register.read_volatile()
register.write_volatile(status | READY)
```

The feature maps directly to C23. It introduces no address integers, pointer
arithmetic, unchecked casts, field-offset operation, or volatile type wrapper.

## Layout queries

The built-ins are:

```text
size_of<T>(): Size
align_of<T>(): Size
```

Each call requires exactly one explicit type argument and no value arguments.
There is no value form such as `size_of(value)` in v1.

`size_of<T>()` returns the number of bytes occupied by one T value in the
selected C23 target representation. `align_of<T>()` returns T's required byte
alignment. Both results have type `Size` and are C integer constant
expressions in generated code.

```seawitch
integer_bytes: Size = size_of<Int32>()
node_alignment: Size = align_of<Node>()
handle_bytes: Size = size_of<String>()
```

For reference-like types such as String and List, `size_of<T>()` reports the
source value's handle size, not the size of its heap allocation.

### Eligible types

T must be a complete, finite-sized value type with a settled generated-C
representation. This includes scalars, pointers, function values, complete
objects and ADTs, fixed Arrays, View descriptors, and heap-backed String or
collection handles.

It excludes `Unknown`, incomplete foreign records, an unspecialized generic
type, and any no-result form.

Transparent aliases produce the layout of their canonical type. A complete
foreign C type is eligible after RFC 0039 verifies its target layout.

Inside a generic declaration, T may be a type parameter. The operation is
validated and lowered after specialization:

```seawitch
fun storage_size<T>(): Size
    return size_of<T>()
end
```

### No new constant-generic expressions

RFC 0020 requires Array length N to be a positive integer literal. This RFC
does not expand that grammar:

```seawitch
buffer: Array<Byte, size_of<Header>()> // Error: length must be a literal.
```

General constant expressions and `offset_of` are deferred.

## Volatile access

Volatile is an access operation on an existing pointer, not a type qualifier
in Seawitch source:

```text
Ptr<T>.read_volatile(): T
MutPtr<T>.read_volatile(): T
MutPtr<T>.write_volatile(value: T)
```

`write_volatile` returns no value. `Ptr<T>` cannot write. `MutPtr<T>` may read
or write. Nullable pointers must be narrowed before either operation.

Each operation accesses exactly one pointed-to T object. The receiver and the
written value are each evaluated exactly once.

### Eligible volatile types

V1 permits volatile access for:

- `Int8`, `Int16`, `Int32`, and `Int64`;
- `UInt8`, `UInt16`, `UInt32`, and `UInt64`;
- `Byte`, which canonicalizes to `UInt8`; and
- `Size`.

This is the small useful set for integer device registers and C volatile
integer objects. Bool, Rune, Float, pointers, functions, objects, ADTs, Arrays,
Views, and collection handles are rejected for volatile access in v1.

An imported C scalar is eligible only when RFC 0039 canonicalizes it to one of
the integer types above. Nominal foreign enums and records are not eligible.

Broader C volatile forms may be added later if a real API requires them. They
are not needed for integer memory-mapped I/O.

### Meaning

Volatile gives the access ordinary C volatile-observable behavior: the
generated load or store occurs and is not replaced with a cached ordinary
value.

Volatile does not provide atomicity, inter-thread synchronization, worker
scheduling, a compiler or CPU memory fence, cache maintenance, or
device-specific ordering beyond C volatile.

Concurrent shared state uses RFC 0037's Atomic or Mutex. Device-specific
barriers require a future platform operation.

## Pointer boundary

These methods operate only on a pointer obtained through `ref`, allocation, or
an imported/platform API. They cannot create or advance a pointer:

```seawitch
next: Ptr<UInt32> = register + 1 // Error: pointer arithmetic is unavailable.
```

Register blocks use a typed object supplied by an imported or platform API.
Contiguous regions use RFC 0043's explicit View bridge. Arbitrary numeric
address construction remains unavailable.

## C23 lowering

Layout queries lower directly:

```c
(size_t)sizeof(T)
(size_t)alignof(T)
```

The C23 compiler is the final authority for selected-target layout. The
Seawitch checker still proves that T is complete and representable before
generation.

A read through `Ptr<T>` uses a `volatile const T *`; a read or write through
`MutPtr<T>` uses a `volatile T *`. Adding volatile qualification must not cast
away pointee constness. Generated helpers or temporaries ensure every source
operand is evaluated once.

The compiler emits no extra fence, lock, allocation, wrapper object, or
platform-specific branch.

## Diagnostics

Representative diagnostics are:

```text
[Type Error] size_of requires one complete finite-sized type
[Type Error] cannot determine the layout of incomplete type c.FILE
[Type Error] align_of cannot use an unspecialized type parameter here
[Type Error] volatile access is supported only for integer storage types
[Type Error] Ptr<UInt32> is read-only; volatile write requires MutPtr<UInt32>
[Type Error] nullable pointer must be narrowed before volatile access
```

Parsing owns malformed generic arguments. Type checking owns completeness,
volatile eligibility, pointer mutability, and nullability. Unsupported checked
operations never reach C generation.

## Deferred

- value-form `size_of(value)`;
- `offset_of`, packed layout, explicit alignment, and field offsets;
- general constant expressions and non-literal Array lengths;
- volatile Bool, Rune, Float, pointer, and aggregate access;
- source-level volatile fields or local bindings;
- memory barriers, cache controls, CPU intrinsics, and inline assembly; and
- arbitrary-address construction.

## Acceptance criteria

The RFC is implemented when:

1. both layout built-ins accept exactly one explicit eligible type;
2. both return `Size` and lower to target-correct C23 constant expressions;
3. generic layout queries resolve only after concrete specialization;
4. this RFC does not expand Array's literal-only length grammar;
5. volatile reads work on Ptr and MutPtr for the exact integer set above;
6. volatile writes work only on MutPtr for that set;
7. receivers and written values are evaluated exactly once;
8. generated C preserves constness and adds only volatile qualification;
9. no volatile operation gains atomic, fence, or pointer-arithmetic semantics;
   and
10. invalid types, nullable pointers, and read-only writes fail before code
    generation with structured diagnostics.

