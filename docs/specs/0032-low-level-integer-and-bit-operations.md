# RFC 0032: Low-Level Integer and Bit Operations

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Features: integer bitwise operators, defined shifts, scalar bit casting, and
  explicit endian byte conversion
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0003 (scalars), RFC 0009 (operators), RFC 0016 (numeric common
  types and conversions), RFC 0017 (defined integer arithmetic), RFC 0018
  (`Byte`), RFC 0019 (generics), RFC 0020 (`Array<T,N>`), RFC 0033 (no
  source-level pointer arithmetic), and RFC 0036 (`Size`)
- Coordinates with: the future C FFI specification

## Summary

Seawitch provides C-like bit operations with defined, target-independent
semantics and no address exposure:

```seawitch
masked: UInt32 = flags & 0xff
packed: UInt32 = red << 16 | green << 8 | blue
bits: UInt32 = value.bit_cast<UInt32>()
bytes: Array<Byte, 4> = value.to_le_bytes()
```

These operations support flags, protocols, binary formats, hashing, graphics,
compression, cryptography, and embedded work. They do not add pointer
arithmetic, arbitrary object reinterpretation, unchecked casts, selectable
overflow modes, or source-visible host byte order.

## Goals

1. Provide the ordinary C bitwise and shift spellings.
2. Give every accepted operation one deterministic result or defined trap.
3. Reuse RFC 0016 common-type selection and RFC 0017 wrapping bit semantics.
4. Prevent generated C integer promotions, signed shifts, and invalid counts
   from changing Seawitch behavior.
5. Provide explicit representation and endian conversion without exposing
   memory addresses.
6. Keep all methods compiler-owned; do not add user-defined static methods or
   operator overloading.

## Non-goals

- Pointer arithmetic, pointer casts, or address conversion.
- Checked, saturating, or selectable bit-operation modes.
- Compound assignment, increment, or decrement operators.
- Arbitrary object, aggregate, union, or collection bit casting.
- Floating endian methods.
- Bit fields, packed objects, SIMD, or vector operations.

## Grammar and precedence

This RFC replaces the relevant expression ladder with:

```ebnf
expression                 = or-expression ;
or-expression              = and-expression
                           , { "or" , and-expression } ;
and-expression             = bitwise-or-expression
                           , { "and" , bitwise-or-expression } ;
bitwise-or-expression      = bitwise-xor-expression
                           , { "|" , bitwise-xor-expression } ;
bitwise-xor-expression     = bitwise-and-expression
                           , { "^" , bitwise-and-expression } ;
bitwise-and-expression     = equality-expression
                           , { "&" , equality-expression } ;
equality-expression        = type-test-expression
                           , { equality-operator
                             , type-test-expression } ;
type-test-expression       = relational-expression
                           , [ "is" , type-expression ] ;
relational-expression      = shift-expression
                           , { relational-operator
                             , shift-expression } ;
shift-expression           = additive-expression
                           , { shift-operator , additive-expression } ;
additive-expression        = multiplicative-expression
                           , { additive-operator
                             , multiplicative-expression } ;
multiplicative-expression  = unary-expression
                           , { multiplicative-operator
                             , unary-expression } ;
unary-expression           = unary-operator , unary-expression
                           | reference-expression
                           | postfix-expression ;
unary-operator             = "-" | "!" | "~" ;
shift-operator             = "<<" | ">>" ;
```

Binary operators at one level associate left to right. Unary operators associate
right to left. The complete precedence, highest to lowest, is:

| Level | Operators |
|---|---|
| 1 | postfix access, indexing, generic arguments, and calls |
| 2 | unary `-`, `!`, `~`, and `ref` |
| 3 | `*`, `/`, `%` |
| 4 | `+`, `-` |
| 5 | `<<`, `>>` |
| 6 | `<`, `<=`, `>`, `>=` |
| 7 | `is` |
| 8 | `==`, `!=` |
| 9 | `&` |
| 10 | `^` |
| 11 | `|` |
| 12 | `and` |
| 13 | `or` |

The lexer always applies maximal munch and emits one `<<` or `>>` token without
trying to identify parser context. While parsing nested type arguments, the
parser may consume one `>>` token as two consecutive `>` generic closers. Thus
both of these remain valid and equivalent:

```seawitch
compact: Ptr<Ptr<Int32>>
spaced: Ptr<Ptr<Int32> >
```

The parser never treats a `>>` expression token as two relational operators.
`|` is a union separator in type parsing and a bitwise operator in expression
parsing; parser context already separates those grammars.

`~`, `&`, `^`, `|`, `<<`, and `>>` are operator tokens rather than reserved
identifiers. `&=`, `|=`, `^=`, `<<=`, and `>>=` remain absent.

## Eligible integer types

Bitwise and shift operators accept the fixed-width signed and unsigned integer
types plus `Size` after RFC 0036 supplies its selected target width. `Byte` is a
transparent alias of `UInt8` and behaves exactly as UInt8.

`Rune`, Bool, floating types, Nil, Unknown, pointers, functions, objects, ADTs,
unions, arrays, Views, and managed values are rejected. A union must first
narrow to one eligible integer member.

All operations use the selected type's exact width. V1 supports only the 8-,
16-, 32-, and 64-bit fixed integer widths and RFC 0036's 16-, 32-, or 64-bit
`Size` targets.

## Bitwise complement

Unary `~` evaluates its operand once, inverts every bit in its fixed-width
representation, and returns the same canonical type:

```seawitch
mask: UInt8 = 0x0f
inverse: UInt8 = ~mask // 0xf0
```

For a signed type, the resulting bit pattern is interpreted with RFC 0017's
two's-complement value rule. No C signed operation or implementation-defined
conversion determines the result.

## Binary bitwise operations

`&`, `^`, and `|` use RFC 0016's unique least lossless common integer type.
Each operand is converted exactly once to that type, the operation occurs at
that exact width, and the result has that type:

```seawitch
small: UInt8 = 0xf0
wide: UInt16 = 0x0f0f
result: UInt16 = small & wide
```

Untyped integer literals take context from the other operand or the surrounding
expected type under the existing literal rules:

```seawitch
masked: UInt32 = flags & 0xff // 0xff is checked as UInt32.
```

When no unique lossless common integer type exists, the checker rejects the
operation. An expected result type may accept the completed result but never
changes common-type selection inside the operation.

For signed results, the final fixed-width bit pattern is reconstructed as the
corresponding RFC 0017 signed value. There is no separate signed bitwise mode.

## Shift typing

`left << count` and `left >> count` require:

- an eligible integer left operand; and
- any signed or unsigned integer count, including `Size`.

The result type is exactly the left operand's canonical type. The count does
not participate in common-type selection and is interpreted as its exact
mathematical integer value. An untyped count literal uses the ordinary Int32
fallback when no surrounding context gives it another integer type.

Both operands evaluate exactly once. Their relative evaluation order remains
unspecified under RFC 0009. The evaluated count is reused for validation and
the shift.

## Shift-count validity

For an `n`-bit left operand, a valid count is in the inclusive range `0` through
`n - 1`.

- A negative count is invalid.
- A count greater than or equal to `n` is invalid.
- An evaluated compile-time constant outside the range is a Type Error.
- A runtime value outside the range enters the shared unrecoverable numeric
  trap before generated C evaluates any shift.

```seawitch
bad_negative: UInt32 = value << -1 // Type Error
bad_width: UInt32 = value >> 32    // Type Error
```

As with RFC 0017's zero-divisor rule, an unreachable expression is still
type-checked but an unevaluated constant count does not trigger a static
evaluation diagnostic:

```seawitch
safe: Bool = true or (value << 32 == 0)
```

A runtime shift trap terminates the process, produces no result, and is not
recoverable as Error. It uses RFC 0017's shared numeric trap model and may call
C23 `abort`, but it must not execute the invalid C shift or use C
`unreachable`. Deferred actions are not guaranteed to run after the trap.

## Left shift

Left shift moves the left operand's fixed-width bit pattern toward the most
significant end, discards bits shifted beyond the width, and fills low bits with
zero. This is wrapping bit-pattern behavior for both signed and unsigned types:

```seawitch
signed: Int8 = 64
wrapped: Int8 = signed << 1 // -128
```

The operation never invokes C signed-left-shift overflow. For signed results,
the resulting pattern is reconstructed with RFC 0017's signed value rule.

## Right shift

Unsigned right shift fills high bits with zero. Signed right shift is
permanently arithmetic: it fills high bits with the original sign bit.

```seawitch
negative: Int8 = -4
halved: Int8 = negative >> 1 // -2
```

This behavior is independent of the C implementation's handling of negative
signed right shift.

## Scalar bit casting

Bit casting uses one compiler-owned generic instance method:

```seawitch
bits: UInt32 = floating.bit_cast<UInt32>()
again: Float32 = bits.bit_cast<Float32>()
```

The method takes exactly one explicit type argument and no value arguments.
The target is never inferred. It is a protected built-in resolved before user
method lookup and cannot be declared or replaced with `impl`.

V1 permits `bit_cast<T>()` only when source and destination are same-width
fixed-representation scalar types from this table:

| Width | Eligible types |
|---|---|
| 8 | `Int8`, `UInt8`, `Byte` |
| 16 | `Int16`, `UInt16` |
| 32 | `Int32`, `UInt32`, `Float32` |
| 64 | `Int64`, `UInt64`, `Float64` |

Because Byte is canonically UInt8, it does not create another representation.
The same source and destination type is valid and preserves the value.

`Size`, Rune, Bool, pointers, Nil, Unknown, functions, objects, ADTs, unions,
arrays, Views, and managed values are rejected. Pointer rejection in both
directions is required by RFC 0033.

Bit casting:

1. evaluates its receiver exactly once;
2. preserves every source representation bit;
3. performs no numeric conversion or range check;
4. preserves floating NaN payload and signed-zero bits; and
5. interprets signed integer patterns using RFC 0017's representation rule.

Compile-time folding is permitted only when the compiler retains the exact
source representation. If exact floating bits are unavailable, the operation
remains a runtime bit cast rather than canonicalizing a NaN or signed zero.

## Endian byte conversion

Every fixed-width integer type, including Byte through UInt8, provides:

```text
value.to_le_bytes(): Array<Byte, width / 8>
value.to_be_bytes(): Array<Byte, width / 8>
T.from_le_bytes(Array<Byte, width / 8>): T
T.from_be_bytes(Array<Byte, width / 8>): T
```

For example:

```seawitch
little: Array<Byte, 4> = value.to_le_bytes()
big: Array<Byte, 4> = value.to_be_bytes()

from_little: UInt32 = UInt32.from_le_bytes(little)
from_big: UInt32 = UInt32.from_be_bytes(big)
```

The exact array lengths are 1, 2, 4, and 8 for the corresponding fixed widths.
`Size` is excluded because serialized data must not silently change width with
the compilation target. Rune is excluded because it has Unicode-scalar validity
rather than arbitrary 32-bit payload semantics.

Signed integers encode their RFC 0017 two's-complement bit pattern and decode
every input pattern to the corresponding signed value. No input byte pattern is
invalid. Byte order is defined by significance, independent of host endianness:

- little endian places the least-significant byte at index zero;
- big endian places the most-significant byte at index zero.

Each conversion evaluates its receiver or array argument exactly once. The
`from` operation requires the exact `Array<Byte,N>` length for its destination
type; there is no runtime-length overload and no implicit View or pointer
conversion.

Floating byte conversion is deferred. A caller explicitly bit-casts the float
to a same-width integer first.

### Type-qualified intrinsic exemption

`Int8.from_le_bytes(...)` and the corresponding constructors on every eligible
integer type are compiler-owned type-qualified intrinsics. This is the same
narrow exemption used by other built-in constructors. It does not add
user-defined static methods or associated functions.

The instance methods `bit_cast<T>()`, `to_le_bytes()`, and `to_be_bytes()` are
also compiler-owned and cannot be redeclared.

Transparent aliases inherit these operations through their canonical type. A
type-qualified endian constructor written through an alias constructs that
same canonical integer type; it does not create a new representation or a
user-defined static method.

## Generics

Bitwise and shift operators involving an open type parameter are dependent
operations under RFC 0019. The checker records the operation and source span;
each closed specialization must resolve eligible concrete integer types, common
types, result types, and shift width before generation.

`bit_cast<U>()` may remain dependent when its source or explicit destination is
a type parameter. Closed specialization must prove that both concrete types are
eligible and equal-width.

Endian conversion on an unresolved type parameter is rejected in v1 because
its `Array<Byte,N>` result or parameter length depends on the receiver width.
The receiver or type-qualified constructor must resolve to one concrete
fixed-width integer while checking the generic body.

No unresolved low-level operation may reach C generation.

## Constant folding and evaluation

Eligible bitwise operations, valid shifts, exact-representation bit casts, and
endian conversions fold when their operands are evaluated compile-time
constants. Folding uses the same width, common-type, wrapping, shift, and byte
order rules as runtime execution.

Runtime operations evaluate each written source operand exactly once. Generated
temporaries may impose an evaluation order, but programs cannot depend on the
relative order of independent binary operands under RFC 0009.

## C23 lowering

Generated C must implement Seawitch semantics explicitly:

- Bitwise operations convert operands to the selected exact-width unsigned
  representation, perform the operation in a promotion-safe unsigned type,
  reduce to the selected width, and reconstruct a signed result when needed.
- Shift counts are evaluated once and validated for negativity and width before
  conversion to an unsigned C count or execution of a shift.
- Left shift always operates on the unsigned bit representation and reduces to
  the left operand's width.
- Unsigned right shift uses the unsigned bit representation.
- Signed right shift uses unsigned shifting plus explicit sign fill. The
  `count == 0` case is handled separately so the sign-fill expression never
  shifts by the complete width.
- Narrow 8- and 16-bit operations mask or cast after C integer promotion so
  their result cannot accidentally retain wider bits.
- `Size` operations use the target profile's verified width and promotion-safe
  unsigned representation.
- Bit casts involving Float use `memcpy` between equal-size scalar temporaries.
  A signed integer source is first mapped to its corresponding unsigned RFC
  0017 bit pattern; a signed integer destination is reconstructed from the
  unsigned pattern afterward. Integer-to-integer cases may use those unsigned
  value operations directly. Pointer casts, union-punning, inactive union
  reads, dependence on C signed representation, and aliasing violations are
  forbidden.
- Endian conversion uses explicit shifts and masks over unsigned values.
  Decoding signed values uses explicit RFC 0017 reconstruction rather than an
  implementation-defined unsigned-to-signed conversion.
- Generated C never uses pointer arithmetic for source-visible semantics.
  Private `memcpy` and fixed local Array indexing remain implementation details
  under RFC 0033.
- Generated expressions preserve Seawitch precedence and source `#line`
  mappings.

An unsupported type, invalid count, unresolved common type, malformed intrinsic,
or impossible low-level checked node reaching generation is Unknown Error. The
generator never emits a raw C fallback or placeholder.

## Diagnostics

The lexer owns new operator tokens. The parser owns malformed placement,
precedence, generic-closing interpretation, and intrinsic call shape. The
checker owns eligibility, common types, result types, shift counts, method
availability, array widths, constant folding, and specialization.

Required representative diagnostics are:

```text
[Type Error] operator & requires integer operands; got Float32
[Type Error] integer operands have no unique lossless common type
[Type Error] shift count cannot be negative
[Type Error] shift count 32 is outside the valid range for UInt32
[Type Error] bit_cast requires equal-width eligible scalar types; got Float32 and UInt64
[Type Error] bit_cast does not accept pointer source or destination types
[Type Error] to_le_bytes requires a fixed-width integer receiver; got Size
[Type Error] UInt32.from_le_bytes expects Array<Byte, 4>
```

Equivalent source-located wording from an existing shared integer, method, or
initializer diagnostic is acceptable when it identifies the operation and
types. Runtime invalid counts use the numeric trap and are not checker
diagnostics.

## Compatibility and supersession

This RFC implements the bitwise and shift operators deferred by RFC 0009. It
retains RFC 0009's evaluation-order rule, RFC 0016's lossless common-type rule,
and RFC 0017's single wrapping arithmetic model.

RFC 0033 remains authoritative for pointers. The operators and intrinsics here
never make a pointer an integer or a sequence.

## Deferred

- Rotates, population count, leading/trailing zero count, and byte swap methods.
- Floating endian methods.
- Atomic bitwise operations.
- Bit fields and packed object layouts.
- Vector and SIMD operations.
- Compound assignments.
- Arbitrary object, array, union, or managed-value bit casting.

## Implementation direction

1. Implement any outstanding dependency slices required for Byte and Size.
2. Add lexer tokens for `~`, `&`, `^`, `|`, `<<`, and `>>`, including contextual
   splitting of `>>` while parsing nested generic closers.
3. Insert the shift and three bitwise precedence levels into the parser and add
   checked syntax nodes for unary complement, binary bitwise operations, and
   shifts.
4. Reuse RFC 0016 common-type selection and RFC 0017 fixed-width signed
   reconstruction in checker and constant-folding paths.
5. Add exact-once shift-count validation and the shared numeric trap path.
6. Add protected `bit_cast`, endian instance methods, and type-qualified endian
   constructors with exact arity and type checks.
7. Generate promotion-safe unsigned operations, explicit signed reconstruction,
   `memcpy` bit casts, and endian shifts/masks.
8. Add fail-closed dispatch for every new syntax and checked node.
9. Add focused lexer, parser, checker, generator, generic-specialization,
   runtime-trap, and C23-tagged lowering tests.
10. Update canonical grammar, language, and status documents after behavior is
    implemented and verified.

## Required conformance coverage

Implementation is complete only when focused tests establish all of the
following:

1. all six operator tokens lex correctly, preserve the specified precedence,
   and do not break nested generic closers or type unions;
2. unary complement and binary bitwise operations accept every eligible integer
   width, use the required common type, and reject every excluded category;
3. Byte behaves exactly as canonical UInt8, while Rune remains ineligible;
4. Size operations use the selected target width and reject unsupported target
   widths through RFC 0036;
5. shifts preserve the left type, accept independently typed integer counts,
   and evaluate each operand once;
6. constant negative or oversized shift counts are Type Errors, while runtime
   invalid counts trap before any C shift executes;
7. left shift wraps the exact bit pattern for signed and unsigned values,
   unsigned right shift zero-fills, and signed right shift sign-fills;
8. narrow C integer promotions cannot alter 8- or 16-bit results;
9. `bit_cast<T>()` accepts exactly the same-width scalar table, preserves every
   bit including NaN payloads and signed zero, and rejects Size, Rune, pointers,
   aggregates, and managed values;
10. endian conversion produces and consumes exact `Array<Byte,N>` values for
    every fixed integer width, signed pattern, and byte order, independently of
    host endianness;
11. type-qualified endian constructors resolve only as protected compiler
    intrinsics and cannot be declared by users;
12. dependent generic operators and bit casts are rechecked at specialization,
    while width-dependent endian methods reject unresolved receivers;
13. every runtime operand is evaluated exactly once and every folded operation
    matches runtime behavior;
14. generated C uses no signed-overflow shift, invalid shift count,
    implementation-selected negative right shift, pointer punning, inactive
    union read, or source-visible pointer arithmetic; and
15. every failure is structured, source-located, and owned by the earliest
    phase that can prove it.

## Finalized decisions

- Invalid shift counts trap after compile-time constants are rejected.
- Signed right shift is arithmetic.
- Signed and unsigned left shift use the same wrapping bit-pattern rule.
- Bit casting uses the protected instance spelling `value.bit_cast<T>()`.
- `|` is distinguished by type-versus-expression parser context.
- `>>` is contextually split only for nested generic closing.
- Endian `from` constructors are protected type-qualified compiler intrinsics.
- The language adds no compound-assignment or alternative overflow modes.
