# RFC 0038: Generic Conversion Syntax

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Features: one generic destination syntax for explicit checked scalar
  conversion
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0016 (numeric conversion semantics), RFC 0018 (Byte and
  Rune), RFC 0019 (generic method-call syntax), and RFC 0036 (`Size`)
- Coordinates with: RFC 0032 (`bit_cast<T>()`) and RFC 0033 (no pointer casts)
- Supersedes on implementation: RFC 0016's former destination-encoded method
  names and the corresponding Rune and Size spellings

## Summary

An explicit scalar conversion names its destination as one type argument:

```seawitch
small: Int32 = wide.to<Int32>()
```

This replaces destination-encoded names such as:

```seawitch
small: Int32 = wide.to_int32()
```

`to<T>()` is the language's only explicit scalar conversion. It uses RFC
0016's checked behavior. A known-invalid constant is a compile-time error; an
invalid runtime value traps before C performs an unsafe conversion.

Seawitch has no wrapping, saturating, unchecked, or mode-selecting conversion
forms. Ordinary integer arithmetic independently follows RFC 0017's one defined
wrapping rule.

Seawitch calls `to<T>()` a conversion rather than an unchecked cast.
Representation reinterpretation remains the separate `bit_cast<T>()`
operation from RFC 0032.

## Goals

1. Use one regular destination spelling for every explicit scalar conversion.
2. Reuse RFC 0019's implemented generic method-call grammar.
3. Preserve RFC 0016's checked conversion behavior.
4. Keep conversion distinct from bit reinterpretation and pointer operations.
5. Do not implement the old destination-encoded method family or compatibility
   aliases.
6. Generate plain C23 plus only the checks needed to avoid undefined or
   implementation-defined C conversion behavior.

## Non-goals

- Changing RFC 0016's implicit lossless-widening relation.
- Wrapping, saturating, unchecked, or selectable conversion modes.
- User-defined conversions, conversion traits, or operator overloading.
- Context-inferred conversion destinations.
- Pointer, object, ADT, union, collection, String, or Strand casts.
- Recoverable conversion failure.
- Representation or endian conversion, which remain owned by RFC 0032.

Alternative conversion modes are intentionally absent, not deferred work.
When a program needs explicit bit manipulation, it uses RFC 0032's ordinary
bit operations rather than selecting hidden cast behavior.

## Syntax

The complete explicit conversion syntax is:

```text
source.to<Destination>()
```

It uses RFC 0019's existing generic method-call grammar. This RFC adds no
token, keyword, precedence rule, expression production, or conversion-specific
parser node.

The operation:

- requires exactly one explicit type argument;
- accepts no value argument;
- requires final call parentheses;
- evaluates its receiver exactly once;
- never infers Destination from an expected result type; and
- cannot be extracted as a method value.

```seawitch
target: Int32 = source.to<Int32>()  // Valid.
target: Int32 = source.to()         // Type Error: type argument required.
target: Int32 = source.to(1)        // Type Error: type argument required.
target: Int32 = source.to<Int32>(1) // Type Error: no value arguments.
method := source.to<Int32>           // Type Error: not a method value.
```

Malformed generic syntax such as `source.to<>()` is a parser-owned Syntax Error
under RFC 0019. An unknown destination name uses the ordinary type-name
diagnostic before conversion eligibility is considered.

## Name resolution and protection

For an eligible scalar receiver, `to` is a compiler-owned method resolved
before ordinary method lookup. It cannot be declared, overloaded, or replaced
for a built-in scalar type.

The protection is receiver-scoped. An unrelated nominal object may declare an
ordinary method named `to`; that method receives no built-in conversion
behavior:

```seawitch
value.to<Int32>() // Built-in when value is an eligible scalar.
box.to<Int32>()   // Ordinary method lookup when box has nominal type Box.
```

An unsupported scalar source still resolves the built-in name far enough to
produce a conversion Type Error rather than falling through to unrelated
lookup. Transparent aliases are classified by canonical receiver type.

## Exact source and destination matrix

Aliases are canonicalized before this table is applied. `Integer` means the
eight fixed-width integer types plus target-sized Size. Byte is accepted
through its canonical UInt8 identity.

| Source | Destination | `to<T>()` behavior |
|---|---|---|
| Integer | Integer | preserve value after destination range check |
| Integer | Float32 or Float64 | RFC 0016 round-to-nearest, ties-to-even conversion; finite overflow traps |
| Float32 or Float64 | Float32 or Float64 | RFC 0016 round-to-nearest, ties-to-even conversion; finite overflow traps |
| Float32 or Float64 | Integer | truncate toward zero, then destination range-check |
| Integer | Rune | check Unicode scalar validity |
| Rune | Integer | preserve scalar value after destination range check |
| every other pair | any | rejected |

The same canonical numeric type is a valid no-op conversion so generic code
does not need a special case. Rune-to-Rune is not added; ordinary use already
has the required type.

There is no alternative result for an out-of-range conversion:

```seawitch
value: Int32 = 300
small: Int8 = value.to<Int8>() // Runtime trap.
```

The compiler does not silently wrap this to `44`, clamp it to `127`, or inherit
the result of a C implementation-selected signed narrowing conversion.

## Rune, Size, and Byte

Rune is not a numeric arithmetic type. It participates only in checked integer
conversion:

```seawitch
letter: Rune = code.to<Rune>()
code: UInt32 = letter.to<UInt32>()
index: Size = letter.to<Size>()
```

Integer-to-Rune rejects negative values, values above `0x10ffff`, and surrogate
values `0xd800` through `0xdfff`. Rune-to-Integer preserves the scalar value
only when the destination range contains it. Float-to-Rune and Rune-to-Float
are rejected.

Size is an Integer for this RFC. Its range is the selected target's 16-, 32-,
or 64-bit `size_t` range, fixed before source using it is checked:

```seawitch
count: Size = value.to<Size>()
portable: UInt64 = count.to<UInt64>()
```

Byte may be written as the destination:

```seawitch
byte: Byte = value.to<Byte>()
```

It has canonical UInt8 behavior and does not create a separate runtime
conversion.

## Constants, literals, and expected types

The checker folds a conversion when its receiver is a compile-time constant.
A known-invalid conversion is rejected before generation:

```seawitch
bad: Int8 = (128).to<Int8>()
// Type Error: value 128 is outside the range of Int8.
```

The destination argument does not contextually type the receiver. The receiver
first receives its normal expected or fallback type; conversion is checked
afterward:

```seawitch
small: Int8 = (200).to<Int8>()
// 200 first receives fallback Int32, then conversion fails.

too_large: Int64 = (5_000_000_000).to<Int64>()
// Error: the receiver literal does not fit fallback Int32.
```

To use a larger literal source, type it explicitly first:

```seawitch
source: Int64 = 5_000_000_000
same: Int64 = source.to<Int64>()
```

An expected result never supplies an omitted destination:

```seawitch
small: Int8 = value.to() // Type Error: explicit destination required.
```

## Transparent aliases

A transparent alias is accepted wherever its canonical scalar type is:

```seawitch
type Count = Int32

count: Count = value.to<Count>()
```

Checking and generated helpers use canonical identity. An alias does not create
new conversion behavior or an alias-specific runtime helper.

## Generics

A source or destination may depend on a generic type parameter:

```seawitch
fun convert<Source, Destination>(value: Source): Destination
    return value.to<Destination>()
end
```

The checker records the receiver, destination type expression, and source span
as an RFC 0019 dependent operation. Every closed specialization must resolve
one concrete source and destination pair permitted by the matrix:

```seawitch
good: Int32 = convert<Int64, Int32>(10)
bad: Bool = convert<Int32, Bool>(10)
// Type Error in specialization: numeric values do not convert to Bool.
```

No runtime type object, dynamic dispatch, unresolved destination, or generic
conversion operation reaches C generation. Generic inference does not infer a
destination omitted from `to()`.

## Conversion versus bit casting

`to<T>()` converts the mathematical value. `bit_cast<T>()` preserves source
representation bits and requires an equal-width pair allowed by RFC 0032:

```seawitch
numeric: UInt32 = floating.to<UInt32>()
bits: UInt32 = floating.bit_cast<UInt32>()
```

Neither operation accepts a pointer source or destination:

```seawitch
address: Ptr<Node> = bits.to<Ptr<Node>>()       // Type Error.
address: Ptr<Node> = bits.bit_cast<Ptr<Node>>() // Type Error.
```

Unknown pointer erasure and recovery remain the specific pointer operations
owned by RFC 0010. They are not spelled with `to<T>()`.

## Supersession

Only `to<T>()` is accepted. The former specification examples used these
unimplemented names:

```text
to_int8() through to_int64()
to_uint8() through to_uint64()
to_float32() and to_float64()
to_rune()
to_size()
```

They were never implemented. The compiler therefore carries no compatibility
table, migration path, alias, or special diagnostic for them. Every old or
invented name receives the ordinary missing-method diagnostic. Closed RFC 0018
remains immutable; this RFC supersedes only its coordinated conversion
spelling.

## Diagnostics and phase ownership

The parser owns malformed generic call syntax. Type-name resolution owns an
unknown destination. The checker owns the method contract, source and
destination eligibility, constant failures, and generic specialization
failures. The generator receives only a concrete checked conversion.

Required representative diagnostics are:

```text
[Type Error] to requires exactly 1 explicit type argument
[Type Error] to accepts no value arguments
[Type Error] built-in conversion methods cannot be used as values
[Type Error] numeric conversion requires a supported scalar source and destination
[Type Error] pointer conversion is unavailable
[Type Error] value 128 is outside the range of Int8
[Name Error] unknown type Int128
[Name Error] Int32 has no method to_int32
```

Equivalent source-located wording from an existing shared diagnostic is
acceptable when it identifies the same violation. An unresolved source or
destination reaching generation is an Unknown Error and must not emit a C cast
or placeholder.

## Evaluation and C23 lowering

The checked representation records the receiver expression and one concrete
canonical source and destination type.

The receiver evaluates exactly once. When a range check or multi-step lowering
would otherwise repeat it, generated C first stores it in one correctly typed
temporary.

The generic spelling introduces no runtime generic operation. C23 generation
reuses RFC 0016's range checks, numeric trap, floating conversion, and
constant-folding results. Rune validity uses RFC 0018 and Size limits use RFC
0036.

A direct C cast may be used only where C23 guarantees the required result. The
generator checks first where C signed narrowing is implementation-defined or a
floating-to-integer conversion could be undefined. Generated C therefore stays
close to C without inheriting C's unsafe conversion edges.

No conversion helper is emitted merely because an open generic declaration
contains a dependent conversion. Helpers are emitted once for concrete
operations required by reachable closed specializations.

## Implementation direction

1. Reuse RFC 0019's generic method-call form; add no conversion syntax node.
2. Recognize compiler-owned `to` on canonical eligible scalar receivers before
   ordinary method lookup.
3. Require exactly one explicit destination type, zero value arguments, and a
   source/destination pair from the complete matrix.
4. Record concrete and dependent conversions with exact source spans.
5. Lower the one checked operation through RFC 0016's existing constant,
   range, floating, Rune, Size, and trap rules.
6. Let old and invented conversion names fail through ordinary method lookup;
   add no compatibility table or migration aliases.
7. Recheck dependent conversions after specialization and reject unresolved
   operations before generation.
8. Add fail-closed generator handling and focused end-to-end tests.
9. Update canonical language and status documentation after implementation and
   verification; generic method-call grammar itself requires no change.

## Required conformance coverage

Implementation is complete only when focused tests establish all of the
following:

1. `to<T>()` parses through ordinary RFC 0019 generic method-call syntax and
   adds no token, keyword, precedence rule, or conversion AST form;
2. exactly one explicit type argument, zero value arguments, and final call
   parentheses are required, and `to<T>` is not a method value;
3. the destination is never inferred from assignment, return, argument, or
   other expected-type context;
4. fixed integers, Byte, floats, Rune, and every supported Size target obey the
   exact source/destination matrix;
5. integer conversions preserve values or fail before producing an invalid
   destination, including same-type conversions;
6. floating conversions preserve RFC 0016 rounding, truncation, non-finite,
   finite-overflow, and trap behavior;
7. Rune conversion checks Unicode scalar validity and destination range, while
   Float/Rune pairs are rejected;
8. transparent aliases canonicalize for checking and do not create distinct
   runtime helpers;
9. receiver expressions evaluate exactly once and destination arguments do not
   change literal fallback typing;
10. open generic conversions are recorded as dependent operations, every
    closed specialization is rechecked, and no unresolved conversion reaches
    generation;
11. `to<T>()` and `bit_cast<T>()` remain semantically and diagnostically
    distinct, and neither permits a pointer source or destination;
12. former and invented conversion names receive ordinary missing-method
    diagnostics with no compatibility lookup;
13. no wrapping, saturating, unchecked, or mode-selecting conversion syntax or
    checked representation exists;
14. constants fold or fail before generation and runtime failures trap before
    an unsafe C conversion;
15. generated C contains one concrete checked operation with no runtime generic
    type, unsafe conversion, or alias-specific duplicate helper; and
16. every malformed, unsupported, unresolved, or impossible state fails at its
    earliest proving phase.

## Finalized decisions

- Explicit scalar conversion uses only `to<T>()`.
- Every explicit conversion is checked; invalid constants fail compilation and
  invalid runtime values trap.
- Wrapping, saturating, unchecked, and selectable conversion modes do not
  exist.
- Rune supports checked integer conversion only, including Size as an integer.
- Byte is canonical UInt8 and accepts `to<Byte>()` without a distinct runtime
  conversion.
- The destination is explicit and never inferred from context.
- Former destination and mode names have no compatibility aliases or special
  migration diagnostics.
- `to<T>()` converts values, while `bit_cast<T>()` reinterprets bits.
- Pointer conversion remains unavailable.
