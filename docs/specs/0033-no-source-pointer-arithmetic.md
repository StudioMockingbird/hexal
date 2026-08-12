# RFC 0033: No Source-Level Pointer Arithmetic

- Kind: Architecture Decision Record (ADR)
- Status: Implemented; conformance verified 2026-08-12
- Decision: Seawitch pointers support reference and dereference, not arithmetic
  or address exposure
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0001 (raw pointers), RFC 0007 (pointer mutability), RFC 0009
  (core operators), RFC 0010 (nullability and Unknown), RFC 0016 (explicit
  numeric conversions), RFC 0020 (Array and View), RFC 0024 (pointer identity
  equality), and RFC 0026 (allocation)
- Coordinates with: RFC 0032 (low-level integer and bit operations) and the
  future C FFI specification

## Context

C pointer arithmetic requires the programmer to preserve array bounds,
provenance, lifetime, alignment, element-size, and one-past-the-end rules.
Adding it to Seawitch would also create a second unchecked sequence API beside
Array, View, List, String, and Strand.

Seawitch instead treats each `Ptr<T>` or `MutPtr<T>` value as a reference to one
typed T object:

```seawitch
node: Node = Node { value = 10 }
pointer: Ptr<Node> = ref node
copy: Node = pointer.value
```

The pointer representation remains a plain C pointer. This ADR restricts the
operations exposed by Seawitch source; it does not add a pointer wrapper,
ownership tracking, or runtime pointer validation.

## Decision

Seawitch has no source-level pointer arithmetic and no general way to expose a
pointer's numeric address. There is no `unsafe` block, cast, method, generic
specialization, or operator overload that enables either operation.

The rule applies equally to `Ptr<T>`, `MutPtr<T>`, transparent aliases of those
types, nested pointer types, and pointers obtained through allocation or future
FFI.

## Permitted pointer operations

Pointers retain only the operations defined by their owning RFCs:

- construction with `ref`, allocation, RFC 0010's one-layer Unknown erasure and
  contextual recovery, and future FFI boundaries;
- ordinary pointer-value copying and reassignment of a `mut` binding;
- outermost `MutPtr<T>` to `Ptr<T>` weakening;
- read dereference through `.value` on either pointer constructor;
- write dereference through `.value` only on `MutPtr<T>`;
- existing pointee method receiver adaptation;
- exact pointer identity equality and inequality under RFC 0024;
- nullable-union comparison, type testing, and narrowing under RFC 0010; and
- explicit deallocation where the allocator contract permits it.

Truthiness and logical operations remain governed by RFC 0023. They do not
reveal or modify an address.

## Prohibited source operations

The checker rejects every well-formed expression in the following classes:

| Class | Examples |
|---|---|
| pointer offset | `pointer + count`, `count + pointer`, `pointer - count` |
| pointer distance | `left - right` |
| pointer indexing | `pointer[index]` |
| pointer ordering | `left < right`, `left <= right`, `left > right`, `left >= right` |
| pointer-to-integer conversion | `pointer.to<UInt64>()` |
| integer-to-pointer conversion | assigning or converting an integer to `Ptr<T>` |
| pointer bit cast | `pointer.bit_cast<UInt64>()`, `bits.bit_cast<Ptr<Node>>()` |
| arbitrary-address construction | constructing `Ptr<T>` from a numeric address |

The numeric, remainder, bitwise, and shift operators accept only the value
types defined by their owning RFCs. A pointer operand never participates in
numeric widening or generic numeric inference.

`++`, `--`, `+=`, and `-=` are not Seawitch operators for any type. They remain
absent from the grammar rather than becoming special pointer forms. Source that
uses them receives the ordinary parser-owned Syntax Error; the parser cannot
and need not infer a pointer type for syntax that is not an expression.

There is no source-visible one-past pointer state. Future FFI may import an
address supplied by C, but Seawitch cannot advance it, order it, convert it to
an integer, or index from it.

## Indexing and dereference boundary

The general postfix grammar parses `value[index]` before the checker knows the
receiver type. Pointer indexing is therefore a checker-owned Type Error, not a
grammar error.

Dereferencing a pointer to a bounds-carrying object and then indexing that
object is valid:

```seawitch
mut values: Array<Int32, 4> = [10, 20, 30, 40]
array_pointer: MutPtr<Array<Int32, 4>> = ref values

item: Int32 = array_pointer.value[2] // Valid: checked Array indexing.
element: MutPtr<Int32> = ref values[2]
copy: Int32 = element.value          // Valid: one-object dereference.
bad: Int32 = element[1]              // Error: pointer indexing.
```

Taking `ref` of an addressable collection element does not create an array
pointer. The result refers only to that element and gains no operation for
reaching adjacent elements. Existing addressability and mutability rules decide
whether the result is `Ptr<T>` or `MutPtr<T>`.

## Contiguous data

Programs use bounds-carrying types for multiple elements:

```seawitch
fixed: Array<Int32, 4> = [10, 20, 30, 40]
borrowed: View<Int32> = fixed.slice(0, 4)
dynamic: List<Int32> = List<Int32>.new(h)

value: Int32 = borrowed[index]
```

There is no ordinary source constructor that turns `Ptr<T>` plus a count into
`View<T>`. Current Views must originate from the stable sources listed by RFC
0020.

A future FFI may create a View only from an explicitly described pointer and
element count. The FFI specification owns the annotation and lifetime contract,
but it must preserve these invariants:

1. the count is explicit rather than inferred from a pointer;
2. every Seawitch access is checked against that count;
3. the pointer's external lifetime remains the programmer's FFI responsibility;
   and
4. the raw pointer itself gains no indexing or arithmetic operations.

A C API that requires pointer stepping must otherwise expose a typed imported
operation or perform the stepping inside its C wrapper.

## Unknown pointers

RFC 0010's explicit one-layer conversions between a concrete pointer and
`Ptr<Unknown>` or `MutPtr<Unknown>` remain available. Unknown is incomplete and
cannot be dereferenced. Erasure and recovery change only the statically known
pointee type; they do not expose an address or grant arithmetic, ordering,
indexing, or bit casting.

```seawitch
erased: Ptr<Unknown> = pointer
recovered: Ptr<Node> = erased
copy: Node = recovered.value
```

Recovery occurs only in RFC 0010's expected-type contexts. This ADR adds no
method or second cast mechanism.

## Generated runtime code

The prohibition applies to Seawitch source semantics, not trusted generated C
or compiler-owned runtime source. Private String, View, List, Dict, allocator,
UTF-8, Stream, and scheduler helpers may use C pointer arithmetic when all of
the following hold:

1. an owning feature establishes the allocation, element layout, and bounds;
2. each operation stays within C's valid object and one-past rules;
3. alignment, provenance, lifetime, and const qualification are preserved;
4. no computed raw address is returned as a new Seawitch operation; and
5. unsupported or unverified lowering fails closed.

This permission is an implementation detail, like generated bounds checks. It
does not authorize the generator to translate a rejected source pointer
operation. A checked syntax tree reaching C generation with source-level
pointer arithmetic is an internal compiler failure and produces Unknown Error,
never unchecked C.

## Grammar and diagnostic ownership

This ADR adds no grammar production or token.

- The parser owns malformed expressions and unsupported `++`, `--`, `+=`, and
  `-=` spellings.
- The checker owns every syntactically valid operator, index, ordering,
  conversion, member-call, and bit-cast attempt involving a pointer.
- Pointer aliases are classified by canonical type, so an alias cannot bypass
  these checks.
- A nullable pointer union must first narrow to its pointer member before using
  `.value`; narrowing does not add any operation prohibited here.
- The generator accepts only the permitted checked pointer operations and
  fails closed on every other pointer node.

Required representative diagnostics are:

```text
[Type Error] operator + is unavailable for Ptr<Int32>
[Type Error] cannot index Ptr<Int32>; pointers refer to one object; use View<T> for a sequence
[Type Error] ordering is unavailable for Ptr<Int32> values
[Type Error] pointer conversion is unavailable
[Type Error] expected Ptr<Int32> initializer, got UInt64
[Type Error] bit_cast does not accept pointer source or destination types
```

Equivalent source-located wording from an existing shared operator, method, or
initializer diagnostic is acceptable when it identifies the rejected operation
and pointer type. The compiler must not report Unsupported Error for a
well-formed prohibited operation: the language understands it and rejects it
as a type or name error.

## Consequences

Benefits:

- one bounds-checked indexing model;
- no source-created one-past pointer state;
- no pointer/integer round trips;
- fewer C undefined-behavior paths;
- simpler alias and lifetime reasoning; and
- raw pointers remain understandable as references to one object.

Costs:

- allocators, container internals, intrusive structures, memory-mapped binary
  layouts, and some device interfaces cannot be implemented entirely in
  ordinary Seawitch;
- those facilities require compiler-owned helpers, trusted runtime source, or
  a future C FFI wrapper; and
- Seawitch cannot literally express every C pointer algorithm in source.

These costs are accepted. C interoperability does not require reproducing every
unsafe C operation in Seawitch syntax.

## Deferred work

- The C FFI's exact pointer-plus-length annotation and lifetime contract.
- A dedicated volatile register abstraction for memory-mapped I/O.
- Opaque address formatting for diagnostics without numeric conversion.

None changes this ADR's no-pointer-arithmetic decision.

## Implementation direction

1. Keep the grammar unchanged and add parser tests proving C increment and
   compound-assignment spellings remain syntax errors.
2. Ensure arithmetic, ordering, and indexing classify canonical pointer types
   before generation and return source-located diagnostics.
3. Ensure ordinary method lookup and initializer checking reject pointer/numeric
   conversions without introducing casts.
4. When RFC 0032 is implemented, reject pointers as either source or destination
   of `bit_cast`.
5. Add an explicit generator fail-closed guard for any impossible checked
   pointer arithmetic or indexing node.
6. Add focused end-to-end tests for both pointer constructors, transparent
   aliases, nested pointers, nullable narrowing, and valid dereference-then-index
   behavior.
7. Update the canonical grammar, language guide, and status documents once the
   behavior is implemented and verified.

## Required conformance coverage

Implementation is complete only when tests establish all of the following:

1. `Ptr<T>` and `MutPtr<T>` retain reference, dereference, outermost weakening,
   identity equality, nullable narrowing, and valid allocator operations;
2. pointer-plus-integer, integer-plus-pointer, pointer-minus-integer, and
   pointer-minus-pointer expressions are rejected for both pointer constructors;
3. pointer indexing is rejected after parsing, including through aliases and
   after nullable narrowing;
4. dereferencing `Ptr<Array<T,N>>` or `MutPtr<Array<T,N>>` and then using checked
   Array indexing remains valid;
5. pointer equality remains valid under RFC 0024 while all four ordering
   operators are rejected;
6. pointer-to-integer and integer-to-pointer conversions and arbitrary-address
   construction are rejected;
7. `++`, `--`, `+=`, and `-=` remain absent from the grammar for every type;
8. Unknown erasure and recovery add no forbidden pointer capability;
9. RFC 0032 rejects a pointer as either side of `bit_cast` when that RFC is
   implemented;
10. generated user C contains no lowering path for a rejected source pointer
    operation, while trusted bounded runtime helpers may use valid private C
    pointer arithmetic; and
11. every failure is structured, source-located, and owned by the earliest
    phase that can prove it.

## Final decision

The decision is complete. Ordinary Seawitch source can refer to and dereference
one typed object through a pointer. Bounds-carrying abstractions own sequence
access. Only trusted generated C, runtime source, or a future explicit FFI
operation may perform pointer stepping.
