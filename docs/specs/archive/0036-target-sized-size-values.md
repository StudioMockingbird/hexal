# RFC 0036: Target-Sized `Size` Values

- Kind: Language Semantics Specification (ISO/IEC Language Standard Format)
- Status: Implemented; conformance verified 2026-08-11
- Features: C-compatible `Size`, in-memory length and capacity types, numeric
  operations and conversions, and exact `size_t` lowering
- Created: 2026-08-11
- Depends on: RFC 0003 (core scalar types), RFC 0016 (numeric conversions), RFC
  0017 (integer arithmetic), and RFC 0020 (collections)
- Coordinates with: RFC 0027 (Arena and Pool), RFC 0028 (`for` iteration), RFC
  0030 (`print`), RFC 0031 (`Stream<T>`), RFC 0032 (low-level integer
  operations), RFC 0038 (generic conversion syntax), and the future
  build-target specification
- Supersedes on implementation: RFC 0018 and RFC 0020 signatures and layouts
  that use `UInt64` for in-memory text or collection lengths and index
  normalization

## Summary

`Size` is Seawitch's unsigned type for in-memory lengths, capacities, indices,
and allocation-size calculations. It maps exactly to C23 `size_t`:

```seawitch
count: Size = values.length()

for i, value in values do
    print(i, ": ", value)
end
```

`Size` is not a pointer and gains no pointer arithmetic. V1 does not add
`ISize`; Seawitch has no negative indexing or pointer subtraction requiring a
signed counterpart.

## Type and target contract

`Size` is a distinct canonical unsigned integer type. Its width, alignment,
range, and C representation are exactly those of the selected target's
`size_t`. Its range is zero through `SIZE_MAX` for that target.

V1 supports targets whose C `size_t` width is 16, 32, or 64 bits. A target with
another width is rejected before checking source that depends on `Size`. The
compiler target profile supplies this width before type checking; a native
build may default it from the compiler's supported host target. Generated C
contains a compile-time assertion that its `size_t` width matches the checked
target profile, preventing generated code from being compiled silently for a
different ABI.

Type aliases preserve canonical `Size` identity:

```seawitch
type Count = Size
```

`Size` is not canonically equal to `UInt16`, `UInt32`, or `UInt64`, even when
their widths happen to match on one target.

## Literals and initialization

An untyped non-negative integer literal may initialize `Size` when its value is
within the selected target's range:

```seawitch
zero: Size = 0
capacity: Size = 128
```

A negative or out-of-range constant is a compile-time error. Runtime conversion
uses the numeric conversion rules below.

## Operators

`Size` supports the ordinary unsigned integer operations already defined by
the language:

- arithmetic `+`, `-`, `*`, `/`, and `%` under RFC 0017;
- equality and ordering under RFC 0024; and
- bitwise `~`, `&`, `^`, `|`, `<<`, and `>>` when RFC 0032 is implemented.

Arithmetic wraps at the target's `Size` width. Division and remainder by zero
use RFC 0017's defined diagnostics and trap. Unary `-` is rejected because
`Size` is unsigned.

## Numeric conversion

RFC 0016's lossless-widening rule applies using the selected target's concrete
`Size` range. A typed conversion is implicit only when every source value fits
the destination on that target.

When one operand has type `Size` and the other is a fixed-width integer:

1. use `Size` when the complete fixed-width range fits `Size`;
2. otherwise use the fixed-width type when the complete `Size` range fits it;
3. if both ranges are identical, prefer `Size`; and
4. otherwise reject the operation because no lossless common integer type
   exists.

Examples on a 64-bit `Size` target:

```seawitch
small: UInt32 = 10
count: Size = 20
total := small + count       // Size

signed: Int64 = 10
bad := signed + count        // Error: neither full range fits the other
```

`to<Size>()` extends RFC 0016's one checked conversion under RFC 0038. A Size
receiver uses the same generic spelling for fixed-width destinations, such as
`to<UInt64>()` and `to<Int32>()`. Size has no wrapping, saturating, or unchecked
conversion mode.

On every supported target, `Size` widens losslessly to `UInt64`, so assignment
to `UInt64` may be implicit under RFC 0016. An explicit conversion remains
useful when documenting a serialization or FFI boundary.

## In-memory API conformance

The following return `Size`:

```text
Array<T,N>.length()
View<T>.length()
List<T>.length()
String.length()
Strand.length()
String.bytes().length()
Stream<T>.length()
Stream<T>.capacity()
```

String and Strand lengths continue to count Runes. A byte View length counts
bytes. Only the result type changes.

Collection and text indices, slice bounds, runtime capacities, and allocation
element counts normalize to non-negative `Size`. Existing APIs may continue to
accept any integer source type; normalization performs the same checked
conversion and bounds validation, now targeting `Size` rather than `UInt64`.

Pool and Stream construction capacities have type `Size`:

```seawitch
pool: Pool<Node> = Pool<Node>.new(h, 128)
events: Stream<Event> = Stream<Event>.new(h, 64)
```

`Size` does not become a Dict key in v1. RFC 0020's exact `Int32` or `Strand`
key restriction remains unchanged.

## C23 lowering

- `Size` lowers directly to `size_t`.
- `SIZE_MAX` or an equivalent generated constant supplies its maximum.
- C `_Static_assert` verifies the checked target width.
- Source arithmetic is generated under RFC 0017's defined unsigned wrapping
  rules rather than relying accidentally on C integer promotions.
- Length, capacity, index, and byte-count fields lower to `size_t`.
- Conversion checks evaluate their operand once and compare without C signed
  conversion surprises.
- Formatting uses a type-correct `size_t` facility or generated decimal helper;
  it never passes `size_t` to a mismatched variadic format.

## Diagnostics

Representative diagnostics are:

```text
[Type Error] Size literal is outside the selected target range
[Type Error] value cannot be represented as Size
[Type Error] Size and Int64 have no lossless common type on this target
[Target Error] Seawitch requires a 16-, 32-, or 64-bit C size_t
[Target Error] generated C size_t width disagrees with the checked target
```

The target loader owns missing or unsupported target-width diagnostics. The
checker owns literals, conversions, common types, and operator eligibility.
Generated C rejects an ABI mismatch before executing the program.

## Deferred

- `ISize` and signed offsets.
- Pointer subtraction and pointer arithmetic, which remain rejected by RFC
  0033 regardless of future signed types.
- Targets with a non-16/32/64-bit `size_t`.
- Public `SIZE_MAX`, `size_of`, and `align_of` source operations.
- General target-feature introspection.

## Acceptance criteria

Implementation is complete when:

1. `Size` has the exact selected C `size_t` representation and range;
2. supported target widths are limited to 16, 32, or 64 bits and generated C
   rejects a mismatched ABI;
3. literals, arithmetic, comparisons, bitwise operations, and conversions use
   the selected width and existing integer semantics;
4. mixed numeric operations use the deterministic lossless relation above;
5. all in-memory text and collection lengths, capacities, indices, and slice
   bounds use `Size`;
6. closed RFC 0018 and RFC 0020 documents remain unchanged while their exact
   `UInt64` API and layout clauses are superseded through this RFC;
7. Pool and Stream capacities use `Size`;
8. `print` formats `Size` without a C variadic mismatch;
9. Dict keys remain restricted to `Int32` and `Strand`; and
10. `ISize`, pointer arithmetic, and pointer subtraction remain unavailable.
