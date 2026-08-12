# RFC 0016: Explicit Numeric Conversions

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-11
- Features: language-wide lossless numeric widening and one explicit checked
  scalar conversion
- Created: 2026-08-09
- Depends on: RFC 0003 (core scalar types), RFC 0009 (core operators)
- Coordinates with: RFC 0010 (nil and explicit nullability), RFC 0014
  (general type expressions and union types), RFC 0017 (defined integer
  arithmetic), RFC 0018 (String and Rune values), RFC 0019
  (generic specialization), RFC 0022 (match expressions), RFC 0024 (equality,
  ordering, and internal dictionary hashing), RFC 0026 (deferred cleanup), and
  RFC 0038 (generic conversion syntax)

## Summary

Seawitch currently has no syntax for converting one typed numeric value to
another. RFC 0003 and RFC 0009 intentionally reject implicit promotions and
mixed-width arithmetic. This RFC supersedes that restriction with one narrow
rule: a typed numeric value may widen implicitly only when every possible
source value is represented exactly by the destination type. Every narrowing,
potentially rounding, or signed-to-unsigned conversion remains explicit.

```seawitch
count: UInt8 = 200
wide: Int64 = count
small: UInt8 = 20
narrow: Int8 = small.to<Int8>()
```

The one explicit conversion is checked. A conversion known to be invalid is a
compile-time error; an invalid conversion whose value is only known at runtime
terminates through the defined numeric trap described by this RFC. Seawitch
does not provide wrapping, saturating, unchecked, or other selectable
conversion modes.

This RFC adds no implicit narrowing, value-losing numeric conversion, numeric
truthiness, pointer cast, pointer/integer conversion, or bit reinterpretation.

## Motivation

The existing exact literal rules are sufficient for constants but cannot move
a typed value across a numeric boundary:

```seawitch
mut total: Int64 = 0
mut step: Int32 = 1

total = total + step       // valid: step widens to Int64
```

Lossless widening needs no source ceremony because it cannot change the
mathematical value. Every other conversion must remain visible at the source
boundary. Delegating those choices to C would reintroduce signed-to-unsigned
surprises, implementation-defined narrowing, rounded comparisons, and
undefined floating-to-integer conversions.

## Design principles

1. Typed numeric values convert implicitly only through the exact lossless
   widening relation in this RFC.
2. Every other conversion uses the built-in `to<T>()` method.
3. The conversion is checked and never relies on C undefined or
   implementation-defined behavior.
4. Compile-time constants are checked before code generation.
5. Conversion does not change mutability, pointer writability, or object
   identity.
6. A conversion is an expression and evaluates its operand exactly once.

## Implicit lossless widening

The checker may insert a numeric conversion only when every value of the source
type is exactly representable by the destination. Excluding identity, the v1
relation is:

| Source | Permitted implicit targets |
|---|---|
| `Int8` | `Int16`, `Int32`, `Int64`, `Float32`, `Float64` |
| `Int16` | `Int32`, `Int64`, `Float32`, `Float64` |
| `Int32` | `Int64`, `Float64` |
| `Int64` | none |
| `UInt8` / `Byte` | `UInt16`, `UInt32`, `UInt64`, `Int16`, `Int32`, `Int64`, `Float32`, `Float64` |
| `UInt16` | `UInt32`, `UInt64`, `Int32`, `Int64`, `Float32`, `Float64` |
| `UInt32` | `UInt64`, `Int64`, `Float64` |
| `UInt64` | none |
| `Float32` | `Float64` |
| `Float64` | none |

`Byte` follows `UInt8` because it is a transparent alias. `Rune` is distinct
from `UInt32` and does not widen implicitly. The floating rows follow binary32
and binary64 precision: all 8- and 16-bit integers are exact in `Float32`; all
8-, 16-, and 32-bit integers are exact in `Float64`; wider integer types are
not.

The relation applies to initialization, assignment, function and method
arguments, returns, object and ADT field initialization, Array elements, and
collection operation arguments whenever one concrete expected numeric type is
known:

```seawitch
small: Int32 = read_count()
wide: Int64 = small
consume_int64(small)

fun widen(value: Int32): Int64
    return value
end
```

For a binary numeric arithmetic or comparison operator, the checker chooses
the unique least common type to which both operands widen. The result of an
arithmetic operator has that common type. If no unique least type exists, the
operation requires an explicit conversion:

```seawitch
i32: Int32 = read_i32()
i64: Int64 = read_i64()
u32: UInt32 = read_u32()
u64: UInt64 = read_u64()
f32: Float32 = read_f32()

i32 + i64       // Int64
i32 + u32       // Int64
i32 + f32       // Float64
i64 + u64       // Error: no lossless common built-in type
```

Formally, identity counts as a widening step. For operand types `A` and `B`,
the checker forms the set of numeric types reachable from both operands by
zero or one table entry above. A candidate is least when it can itself widen
to every other candidate in that set. The operation uses the candidate only
when exactly one least candidate exists. An empty set or multiple incomparable
least candidates is an error. Declaration order, source spelling, numeric
magnitude, and C's usual arithmetic conversions never break a tie.

The operator's overflow, division, remainder, and evaluation rules apply after
conversion to the common type. RFC 0017 defines one wrapping integer-arithmetic
model without changing this type selection.

An exact union member match remains preferred before numeric widening. If no
exact member matches, an ordinary scalar may widen into a union only when one
unique least eligible member exists. A union source is never converted member
by member; it must first be narrowed under RFC 0014.

Implicit widening requires a concrete expected numeric type or a binary
numeric operation whose operand types determine a common type. It does not
invent a wider type for an inferred binding:

```seawitch
small: Int32 = read_count()
copy := small                  // Int32
wide: Int64 = small            // Int64 is expected, so widening applies
```

An expected destination does not change the common type selected inside a
binary operation. The operation is checked first, and its completed result may
then widen to the destination if the table permits it.

RFC 0022 continues to own match-result inference. A concrete expected match
result type reaches each arm, so an arm value may widen to that type. Without
an expected result type, match arms must still produce one identical canonical
type; this RFC does not infer a common numeric match result:

```seawitch
result: Int64 = match value is
    | Left then int32_value    // widens to the expected Int64
    | Right then int64_value
end

inferred := match value is
    | Left then int32_value
    | Right then int64_value
end
// Error: match arm result types do not agree
```

## Explicit conversion

Explicit conversion uses RFC 0038's generic postfix method syntax and adds no
conversion grammar or reserved word:

```seawitch
narrow: Int8 = value.to<Int8>()
```

`to<T>()` is the only explicit scalar conversion operation. The compiler
recognizes it canonically on eligible scalar receivers before user method
lookup. It requires exactly one explicit destination type, takes no value
arguments, cannot be redeclared for built-in scalars, and cannot be extracted
as a method value. RFC 0038 owns its complete syntax and diagnostics; the
removed destination-encoded specification examples were never implemented.

There is deliberately no wrapping, saturating, unchecked, or mode-selecting
variant. A programmer either uses the one checked conversion or performs an
explicit arithmetic/bit operation whose ordinary semantics already define the
desired value.

Parentheses select a larger receiver expression:

```seawitch
per_item: Int32 = count.to<Int32>()
total: Int64 = (count + 1).to<Int64>()
```

## Conversion matrix

Let `S` be the source scalar and `T` the destination scalar. Aliases are
resolved before this table is applied.

| Source and destination | `to<T>()` |
|---|---|
| integer to integer | checked range conversion |
| integer to float | IEC conversion; finite overflow traps |
| float to float | IEC conversion; finite overflow traps |
| float to integer | truncate toward zero, then checked range conversion |
| `Bool` to numeric or numeric to `Bool` | rejected |

The same canonical type is a valid no-op conversion. It remains an explicit
expression so generic code can name its destination without a special case.

### `Rune` conversion extension

`Rune` is not a numeric operand and is not included in the ordinary numeric
conversion matrix. RFC 0018 extends the checked conversion with two
text-specific directions:

- integer `.to<Rune>()` checks the Unicode scalar range and rejects surrogate
  values; and
- `Rune.to<T>()` accepts an integer T and checks its destination range.

The conversion is value-preserving, not arithmetic and not a reinterpretation
of storage. RFC 0038 includes Size among the integer types in these directions.

## Integer-to-integer conversions

`value.to<T>()` preserves the mathematical value if and only if it is
representable by T.

```seawitch
small: Int8 = 12
wide: Int64 = small.to<Int64>() // Valid but unnecessary widening.
bad: Int8 = (200).to<Int8>()    // Compile-time error.

mut input: Int32 = read_input()
result: Int8 = input.to<Int8>() // Traps if input is outside Int8.
```

Widening is exact. Narrowing and signedness changes perform a range check. The
check is based on the mathematical value, not on the source bit pattern. An
unsigned value therefore cannot become a negative signed value through a
conversion.

## Floating conversions

Floating conversions use the same binary32/binary64 and IEC 60559 target
contract established by RFC 0003. Every specified ties-to-even result is
independent of C's implementation-selected adjacent value and of any active C
floating-point rounding mode. Generated helpers must produce the specified
binary32 or binary64 value explicitly when a plain C conversion cannot prove
that result.

### Integer to float

An integer converts to the nearest representable destination floating value,
with ties-to-even. Precision loss is permitted because the conversion is
explicit. A finite source that cannot produce a finite destination value
traps; all current core integer types fit within the exponent range of the
current `Float32` and `Float64` types.

```seawitch
count: Int64 = 9_007_199_254_740_993
approx: Float64 = count.to<Float64>() // Explicit precision loss is permitted.
```

### Float to float

The source value is rounded to the destination format using round-to-nearest,
ties-to-even. A finite source whose rounded result is infinite traps. Finite
underflow to a subnormal or signed zero is valid. NaN, positive infinity,
negative infinity, and signed zero preserve their corresponding IEC meaning
when the destination supports them.

### Float to integer

The source is truncated toward zero before the destination range check:

```seawitch
fraction: Float64 = 3.75
whole: Int32 = fraction.to<Int32>() // 3
```

NaN and infinities are invalid. A finite value is valid when its truncated
mathematical value is representable by the destination integer. Otherwise the
conversion traps. The generator must check before performing any C floating-
to-integer cast.

## Constants and folding

The checker evaluates a conversion during constant folding whenever the
operand is a compile-time constant expression under the existing folding
rules. A non-`mut` binding initialized from runtime data is not a compile-time
constant merely because it cannot be reassigned. The checker reports a source
diagnostic for a known-invalid checked conversion:

```seawitch
too_large: Int8 = (128).to<Int8>()
// Error: value 128 is outside the range of Int8
```

A conversion whose operand is not statically known remains a checked conversion
operation for generation.

An untyped literal is still assigned a type by its expected context before
this RFC is considered. This RFC does not change RFC 0003 contextual literal
typing or RFC 0009 operator propagation.

The explicit destination fixes the method's result type but does not flow that
type backward into an untyped receiver. A receiver without another usable
expected type therefore uses RFC 0003's `Int32` or `Float64` fallback before
method resolution:

```seawitch
small: Int8 = (200).to<Int8>()
// `200` first becomes Int32; the known-invalid conversion is then diagnosed.

too_large: Int64 = (5_000_000_000).to<Int64>()
// Error: the receiver literal does not fit fallback Int32.

source: Int64 = 5_000_000_000
wide: Int64 = source.to<Int64>() // Valid same-type conversion.
```

## Interaction with unions and pointers

Numeric conversion applies only when the checked source expression has one
exact numeric scalar type. A union is not converted by trying every member:

```seawitch
value: Int32 | Float32 = 1
bad: Int64 = value.to<Int64>() // Error: narrow the union first.
```

After RFC 0014 narrowing proves the active member, the resulting scalar may be
converted normally. RFC 0014 continues to own union identity, exact injection,
and union-to-union widening; this RFC owns only the additional selection of one
unique lossless numeric destination member for a non-union scalar source.

Pointer weakening, nullable-pointer conversion, and erased-pointer recovery
remain owned by RFCs 0010 and 0014. This RFC adds no pointer conversion and
does not make `.to<T>()` a general unsafe cast.

## Interaction with generics

RFC 0019 inference binds type parameters from the arguments' existing
canonical types. Implicit numeric widening does not help infer, merge, or
replace generic type arguments. Ordinary argument widening is considered only
after inference has selected one concrete specialization:

```seawitch
fun pair<T>(left: T, right: T): Pair<T>
    return Pair<T> { left = left, right = right }
end

a: Int8 = 1
b: Int16 = 2
bad := pair(a, b)
// Error: T cannot be both Int8 and Int16; inference does not widen a to Int16.
```

A built-in conversion whose receiver type depends on a generic parameter is a
dependent operation under RFC 0019. Open generic checking records the exact
destination. Closed specialization then rechecks the concrete receiver against
this RFC before generation:

```seawitch
fun convert<T>(value: T): Int32
    return value.to<Int32>()
end

good: Int32 = convert<Int64>(10)
bad: Int32 = convert<Bool>(true)
// Error in specialization convert<Bool>: numeric conversion requires a
// numeric scalar source.
```

Dependent arithmetic and comparison operations likewise select their common
numeric type only after substitution. No unresolved conversion or common-type
operation may reach C generation.

## Runtime conversion trap

A failed checked runtime conversion terminates the current program without
returning an invalid value. The generated support region supplies one defined
numeric trap helper, shared with RFC 0017's arithmetic trap where practical,
which may call the C23 standard `abort` facility. The helper must not use C23
`unreachable`, execute the invalid conversion, or trigger another undefined or
implementation-defined operation as its implementation.

The trap is unrecoverable in this RFC. Consistent with RFC 0026, process
termination or another unrecoverable runtime failure is not promised to run
deferred actions. Recoverable conversion failure remains deferred.

## Diagnostics

The checker owns implicit-widening classification, conversion-method lookup,
constant range failures, and invalid source/destination categories. Conversion
methods use existing postfix call syntax, so no new parser node is required.
The generator must reject a checked conversion that is missing its resolved
source or destination type as an `Unknown Error` rather than emitting a C cast.

Representative diagnostics are:

```text
[Type Error] to requires exactly 1 explicit type argument
[Type Error] to accepts no value arguments
[Type Error] numeric conversion requires a numeric scalar source and destination
[Type Error] numeric values have no unique lossless common type
[Type Error] numeric source matches multiple incomparable union destinations
[Type Error] value 128 is outside the range of Int8
[Type Error] numeric values do not convert to Bool
```

## C23 lowering

Every conversion lowers from a checked source and destination type. The
generator may use small inline helpers in the generated support region, but it
must not rely on a plain C cast for a potentially invalid conversion.

- Implicit widening emits the checker-selected destination conversion on each
  value exactly once. Mixed numeric operators must not rely on C's integer
  promotions or usual arithmetic conversions to select their result type.
- Checked integer conversions compare against destination limits before
  converting.
- Floating conversions use explicit target-format operations and checks for
  finite overflow, NaN, and infinity where required. Inexact integer-to-float
  and float-to-float conversions must produce the specified ties-to-even result
  independently of the active C rounding mode.
- Float-to-integer conversion checks the truncated value before the C cast.

Generated C must remain readable and must contain no implementation-defined
conversion whose result is observable as a Seawitch value.

## Evaluation order

The operand is evaluated exactly once. This RFC does not change RFC 0008's
unspecified relative order among independent expression operands or RFC 0009's
short-circuit guarantee for `and` and `or`.

## Compatibility and supersession

On acceptance, this RFC supersedes RFC 0003 and RFC 0009 only where they forbid
implicit conversion of typed numeric values. The replacement is exactly the
lossless widening table and common-type algorithm above. Their literal typing,
operator precedence, evaluation order, overflow behavior, and all
non-numeric restrictions remain authoritative.

This RFC also supersedes RFC 0018's references to checked `as` conversion for
integer/Rune conversion. The replacement is `.to<T>()` under RFC 0038; RFC
0018 remains immutable.

RFC 0024 consumes this RFC's widening relation for numeric equality and
ordering. RFC 0017 consumes the selected common arithmetic type before applying
its defined wrapping and divisor behavior. Neither coordinating RFC may define
a second promotion table.

RFC 0019 owns generic inference and specialization. This RFC adds numeric
widening only after generic inference and supplies the concrete rules used to
recheck dependent conversion, arithmetic, and comparison operations. RFC 0022
retains its identical-arm rule when a match expression has no expected type.

## Deferred

- Recoverable checked conversions returning a union such as `T | Error` or
  `T | Nil`.
- Exact-only integer-to-float conversion distinct from ordinary rounded
  conversion.
- Bit reinterpretation and byte-order conversions.
- Pointer casts, pointer/integer conversions, and C foreign conversions.
- User-defined conversion methods or operator overloading.

Wrapping, saturating, unchecked, and other selectable conversion modes are
intentionally absent, not deferred work.

## Acceptance criteria

Implementation is complete when focused end-to-end tests prove that:

1. `to<T>()` resolves through ordinary generic postfix call syntax without
   introducing reserved words or a conversion parser node;
2. implicit lossless widening applies consistently to expected numeric types,
   arithmetic, and RFC 0024 comparisons, while narrowing and value-losing
   conversions remain explicit;
3. explicit integer widening succeeds and narrowing rejects out-of-range
   constants and traps for invalid runtime values;
4. integer-to-float and float-to-float conversions use the specified rounding
   and finite-overflow rules;
5. float-to-integer conversion truncates toward zero and rejects NaN,
   infinity, and out-of-range values;
6. `Bool`, pointers, union sources, and object values are rejected unless a
   later specification supplies their conversion rule, while scalar widening
   into a union requires one unique least eligible numeric member;
7. inferred bindings retain the completed source expression's type, expected
   match results permit arm widening, and uncontextualized match arms retain
   RFC 0022's identical-type requirement;
8. `to<T>()` does not push its destination type into an untyped receiver, which
   retains RFC 0003's fallback behavior;
9. constants fold or diagnose before generation, while runtime-derived
    immutable bindings remain runtime values;
10. integer-to-Rune conversions reject surrogate and out-of-range values, and
    Rune-to-integer conversions check the destination range;
11. generic inference never widens arguments to infer a type parameter, and
    dependent conversions and common-type operations are rechecked after
    concrete substitution;
12. every failed runtime conversion reaches the defined unrecoverable
    trap before executing an invalid C conversion;
13. generated C contains no unsafe or implementation-defined numeric cast for
    a conversion case and produces specified ties-to-even floating results
    independently of the active C rounding mode;
14. wrapping, saturating, unchecked, and other conversion modes are absent; and
15. every implicit widening, built-in conversion, and generator case is handled
    explicitly under the fail-closed architecture.
