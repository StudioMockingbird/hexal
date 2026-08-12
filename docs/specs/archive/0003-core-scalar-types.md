# RFC 0003: Core Fixed-Width Scalar Types and C23 Interchange

- Status: Implemented
- Features: core scalar types, contextual numeric literals, outgoing C23 scalar mapping
- Created: 2026-08-06
- Revised: 2026-08-06
- Related proposals: RFC 0001 (`Ptr<T>`), RFC 0002 (`mut` and access capabilities)

## Summary

Seawitch defines the following core scalar types:

```seawitch
Bool

UInt8
UInt16
UInt32
UInt64

Int8
Int16
Int32
Int64

Float32
Float64
```

All integer and floating types in this RFC have target-independent
mathematical ranges, storage widths, and required numeric formats. Byte order,
alignment, and ABI placement follow the selected target. Their canonical
outgoing C23 mappings are:

| Seawitch | C23 |
|---|---|
| `Bool` | `bool` |
| `UInt8` | `uint8_t` |
| `UInt16` | `uint16_t` |
| `UInt32` | `uint32_t` |
| `UInt64` | `uint64_t` |
| `Int8` | `int8_t` |
| `Int16` | `int16_t` |
| `Int32` | `int32_t` |
| `Int64` | `int64_t` |
| `Float32` | `float` |
| `Float64` | `double` |

`Byte` is removed from the core language. `UInt8` is the sole unsigned 8-bit
integer type. A future binary-data or string RFC may introduce `Byte` as an
alias of `UInt8`, but it must not be a distinct representation-compatible
type with separate literal rules.

`Size`, `ISize`, `Rune`, `String`, `Float16`, 128-bit integers, complex
numbers, native C integer spellings, and aggregate types are outside this
RFC. `Size` and `ISize` remain planned language types, but require a separate
target-profile specification before their ranges can be checked correctly.

## Motivation

Seawitch needs a small scalar surface that covers ordinary application code,
manual memory management, binary data, game assets, graphics APIs, and modern
web targets without inheriting every historical or optional C type.

The chosen set provides:

1. exact-width storage for binary formats and C23 interchange, with explicit
   byte-order handling where a serialized format requires it;
2. signed and unsigned types used by graphics, audio, files, networking, and
   foreign APIs;
3. the two floating formats used by general CPU, game, GPU, and WebAssembly
   code; and
4. direct, readable C23 output without scalar wrapper structs or runtime
   conversion objects.

Replacing the experimental `Byte` type with `UInt8` removes special behavior
inherited from Hexal. An unsigned eight-bit integer now follows the same
literal, range-checking, type-checking, and code-generation rules as every
other fixed-width integer.

The language deliberately omits a target-dependent general `Int` or `UInt`.
Stored widths and public interfaces therefore remain visible in source.
`Int32` is the fallback type of an otherwise uncontextualized integer literal;
it is not an additional source spelling or an implicit conversion target.

Fixed width does not by itself define an encoded byte sequence. For example,
`UInt32` always contains 32 value bits, but its in-memory byte order follows
the target. Portable file, network, and asset formats must choose an explicit
byte order when converting values to or from bytes.

## Removal of experimental `Byte` behavior

Implementing this RFC removes all language behavior associated with the old
experimental byte feature:

- `Byte` is no longer a recognized built-in type;
- `b'A'`, `b'\n'`, `b'\xFF'`, and all other `b'...'` byte literals are no
  longer expressions;
- the parser and checked representation no longer contain a distinct byte
  literal node;
- code generation no longer contains a distinct `Byte` type or literal case;
  and
- unsigned eight-bit values use `UInt8` and ordinary integer literals.

Typical migrations are:

```seawitch
letter: UInt8 = 0x41
newline: UInt8 = 0x0A
maximum: UInt8 = 0xFF
```

The lexer must recognize the complete removed `b'...'` shape sufficiently to
report a focused diagnostic such as:

```text
[Syntax Error] byte literals were removed; use a UInt8 integer literal
```

It must not degrade a removed byte literal into an identifier followed by an
unrelated unexpected-character error.

This RFC also deliberately changes two existing numeric-lexer behaviors:

- a redundant-leading-zero decimal such as `007`, previously accepted as a
  decimal integer, is rejected; and
- a binary spelling such as `0b1010`, previously split into separate tokens
  and rejected later, becomes one valid integer literal.

These are intentional language changes, not compatibility requirements.

## Guide-level explanation

### Boolean values

`Bool` has exactly the values `false` and `true`:

```seawitch
visible: Bool = true
mut running: Bool = false
running = true
```

`Bool` is not an integer. Numeric values do not implicitly convert to `Bool`,
and `Bool` does not implicitly convert to a numeric type.

### Fixed-width unsigned integers

The unsigned integer types have these ranges:

| Type | Minimum | Maximum |
|---|---:|---:|
| `UInt8` | 0 | 255 |
| `UInt16` | 0 | 65,535 |
| `UInt32` | 0 | 4,294,967,295 |
| `UInt64` | 0 | 18,446,744,073,709,551,615 |

```seawitch
channel: UInt8 = 255
mesh_index: UInt16 = 4200
color_bits: UInt32 = 0xFF8040FF
asset_id: UInt64 = 0x8A217BD0041200FF
```

`UInt8` is used for raw bytes, encoded data, buffers, files, packets, pixels,
and other unsigned eight-bit values. It uses ordinary contextual integer
literals; this RFC defines no special byte-literal syntax:

```seawitch
zero: UInt8 = 0
letter: UInt8 = 65
maximum: UInt8 = 0xFF
```

Unsigned types are appropriate when all bits are data, as with masks, hashes,
packed formats, identifiers, and explicitly unsigned foreign interfaces. They
are not the automatic choice merely because a domain normally excludes
negative values.

### Fixed-width signed integers

The signed integer types use two's-complement representation and have these
ranges:

| Type | Minimum | Maximum |
|---|---:|---:|
| `Int8` | -128 | 127 |
| `Int16` | -32,768 | 32,767 |
| `Int32` | -2,147,483,648 | 2,147,483,647 |
| `Int64` | -9,223,372,036,854,775,808 | 9,223,372,036,854,775,807 |

```seawitch
audio_sample: Int16 = 12000
screen_x: Int32 = 640
timestamp: Int64 = 9_000_000_000
```

The sign in a negative integer constant is a separate `-` token rather than
part of the integer token. This RFC introduces negation only when `-` is
applied directly to an untyped integer literal; it does not introduce general
unary arithmetic syntax.

To keep every signed value writable, negation applied directly to an untyped
integer literal is checked contextually as one exact constant. The
checker negates the literal's mathematical value before checking it against
the expected signed integer type; it does not first require the positive
operand to fit that type:

```seawitch
minimum: Int8 = -128
too_small: Int8 = -129 // Error: result is outside the Int8 range.
```

This is a narrow constant-expression rule, not a rule that makes the sign part
of the token. Negation of a typed expression remains owned by the future
operator RFC.

### Integer literal spellings

Seawitch supports integer literals in four bases:

| Base | Prefix | Example |
|---|---|---|
| Decimal | none | `255` |
| Hexadecimal | `0x` | `0xFF` |
| Binary | `0b` | `0b1111_1111` |
| Octal | `0o` | `0o377` |

The prefixes are lowercase and mandatory for non-decimal bases. Hexadecimal
digits may use either letter case after the lowercase `0x` prefix.

Underscores may separate digits for readability:

```seawitch
population: Int64 = 8_000_000_000
color: UInt32 = 0xFF_80_40_FF
flags: UInt8 = 0b1010_0101
permissions: UInt16 = 0o755
```

An underscore must occur between two valid digits of the same literal. It
cannot immediately follow a base prefix, appear twice consecutively, or end a
literal.

A decimal integer other than zero must not begin with `0`. Therefore `0` and
`7` are valid, while `00` and `007` are errors. Octal values use the explicit
`0o` prefix; Seawitch does not inherit C's implicit leading-zero octal syntax.

The spelling changes only how the value is written. It does not select a type
or signedness:

```seawitch
decimal: UInt16 = 255
hexadecimal: UInt16 = 0xFF
binary: UInt16 = 0b1111_1111
octal: UInt16 = 0o377
```

All four declarations contain the same mathematical value and take `UInt16`
from their expected type.

Exponent notation is floating-point syntax, even when the represented value
is mathematically integral. `7e3` and `7E3` are decimal floating literals, not
integer literals, and cannot directly initialize an integer type.

Integer suffixes such as `7u` or `7i64` do not exist. A negative constant such
as `-7` is the `-` token applied to the positive integer literal `7`, not a
single signed-literal token.

### Floating-point values

`Float32` is IEC 60559 binary32. `Float64` is IEC 60559 binary64.

```seawitch
opacity: Float32 = 0.75
temperature: Float32 = -4.5
distance: Float64 = 6.02e23
```

| Type | Storage | Precision | C23 |
|---|---:|---:|---|
| `Float32` | 32 bits | 24 binary significand bits | `float` |
| `Float64` | 64 bits | 53 binary significand bits | `double` |

An integer literal does not initialize a floating type, and a decimal floating
literal does not initialize an integer type. Explicit numeric conversions are
outside this RFC.

As with negative integer constants, `-` may be applied directly to an untyped
decimal floating literal without introducing general unary arithmetic:

```seawitch
temperature: Float32 = -4.5
negative_zero: Float64 = -0.0
```

The complete signed value is rounded contextually to `Float32` or `Float64`.
Without a usable expected floating type, it defaults to `Float64`. The spelling
`-0.0` produces IEEE negative zero. A negative value that underflows to zero
also preserves its negative sign.

`Float16` is deferred. Half precision is important as a packed GPU and asset
format, but it does not have one universally available native arithmetic
mapping across the initial C23 and WebAssembly targets.

### Mandatory annotations and contextual literals

Every declaration requires a type annotation:

```seawitch
count: UInt32 = 10
ratio: Float32 = 0.5
```

An integer literal begins as an untyped exact mathematical integer. It does
not begin as `Int32`. The checker determines its type in this order:

1. If an expected integer type exists, the literal takes that type when its
   value is representable.
2. If no usable expected integer type exists, the literal defaults to
   `Int32`.
3. If the value does not fit the selected type, checking fails. The compiler
   does not silently select a wider or unsigned type.

For example:

```seawitch
small: UInt8 = 7
large: Int64 = 7
ordinary: Int32 = 7
```

All three literals have the same spelling but take their declaration's
expected type. A context that must independently determine the type of `7`
uses `Int32`. A value outside `Int32` in such a context is an error and needs
an explicit typed context, such as an annotated declaration:

```seawitch
large: Int64 = 5_000_000_000
```

Expected types include declaration annotations, assignment destinations,
pointer `.value` assignment places, and, once the corresponding features
exist, typed function parameters, function return types, and other constructs
whose type is known before checking the literal. Generic inference, overload
resolution, operators, and composite literals must specify whether and how
they supply an expected type in their own RFCs.

Decimal floating literals follow the same contextual model for floating
destinations. Without a usable expected floating type, a decimal floating
literal defaults to `Float64`.

Neither fallback removes the mandatory type annotation from a declaration.
There are no numeric suffixes.

### Type identity and implicit conversions

Every listed Seawitch type is distinct. Equal representation does not make
two types identical:

```seawitch
count: UInt32 = 10
small: UInt16 = count // Error: UInt32 is not UInt16.
```

There are no implicit conversions between typed numeric values. In
particular:

- signed and unsigned values do not mix implicitly;
- narrow and wide integer values do not mix implicitly;
- integers and floats do not mix implicitly; and
- `Bool` does not mix implicitly with numeric values.

Untyped literals are the exception: a literal takes the contextual
destination type when its mathematical value is representable.

Explicit conversion syntax, checked narrowing, lossy floating conversion,
bit reinterpretation, and pointer/integer conversion require separate RFCs.
Until explicit conversions exist, a typed value cannot be transferred to a
different numeric type.

## Reference-level explanation

### Scalar categories

| Category | Members |
|---|---|
| Boolean | `Bool` |
| Unsigned fixed-width integer | `UInt8`, `UInt16`, `UInt32`, `UInt64` |
| Signed fixed-width integer | `Int8`, `Int16`, `Int32`, `Int64` |
| Floating | `Float32`, `Float64` |

`Bool` is not an arithmetic type. The exact operator sets for integer and
floating types are specified when those operators are introduced.

All core scalar types are complete, sized object types and may be the element
of `Ptr<T>`:

```seawitch
mut value: UInt8 = 10
reader: Ptr<UInt8> = ref value
writer: Ptr<UInt8> = mut ref value
```

### Resolved type names

The names in this RFC are built-in type names. They are not contextual aliases
and do not change identity by module or target.

`Byte` is not a built-in type or alias. There are also no built-in names
`Int`, `UInt`, `Float`, `Double`, `Char`, or `Long`.

### Integer literal checking

The checker owns mathematical range validation after the parser has produced
an integer-literal expression. Its decision is keyed by the expected
destination type, not by an allow-list of previously implemented types.

For an expected integer type `T`, the literal is accepted exactly when its
mathematical value lies in the inclusive range of `T`. A value outside the
range is a type error. The checker does not truncate, wrap, saturate, or first
evaluate the value using a C integer type.

When there is no expected integer type, the checker applies the same rule with
`Int32` as `T`. It does not automatically widen to `Int64`, choose an unsigned
type, or select a type based on the magnitude of the literal.

When `-` is applied directly to an untyped integer literal, the checker first
selects its destination type:

1. An expected signed integer type supplies `T`.
2. Without a usable expected integer type, `T` is `Int32`.
3. An expected unsigned integer type is rejected with a dedicated diagnostic;
   the checker does not reinterpret the expression as a positive value or a
   wrapping unsigned value.

A floating expected type is not a usable expected integer type. The negated
integer literal therefore defaults to `Int32` under rule 2 and then fails the
ordinary identical-type check against the floating destination; it does not
receive the unsigned-destination diagnostic.

For a signed `T`, the checker computes the exact negated mathematical value
and range-checks that result against `T`. The positive literal is not
independently required to fit `T`. Consequently every signed minimum is
representable, including `Int64` minimum, and an uncontextualized
`-2147483648` is a valid `Int32` constant.

Even `-0` is invalid with an expected unsigned destination. This restricted
constant rule does not define negation of typed expressions.

The compile-time representation must hold the complete `UInt64` range. Bounds
and literal values must not pass through a signed 64-bit host representation.

These rules apply identically to decimal, hexadecimal, binary, and octal
spellings. Spelling does not change signedness or width.

The lexer diagnoses malformed spelling before type checking. Base prefixes
must be lowercase `0x`, `0b`, and `0o`; uppercase `0X`, `0B`, and `0O` are
invalid. A prefixed literal must contain at least one digit valid for its base.
Decimal integers with redundant leading zeros and misplaced underscores are
also invalid.

### Floating literal checking

A decimal floating literal is parsed as an exact compile-time value before
conversion to `Float32` or `Float64`.

The selected value is the correctly rounded nearest representable value using
IEC 60559 round-to-nearest, ties-to-even. A finite source literal that rounds
to infinity is rejected. Underflow to a finite subnormal or zero is permitted.

When `-` is applied directly to an untyped decimal floating literal, an
expected `Float32` or `Float64` supplies its type. Without a usable expected
floating type, the result defaults to `Float64`. The checker applies the sign
to the exact source value before rounding. A zero source value and a negative
nonzero value that underflows to zero both produce negative zero.

NaN and infinity literal syntax is not introduced here. Their future
construction and comparison semantics require a floating-operations RFC.

### Typed expression compatibility

Initialization and assignment between typed scalar expressions require
identical Seawitch types. C's integer promotions and usual arithmetic
conversions are not Seawitch rules.

Future operator checking must not delegate semantic type selection to the C
compiler. An operator RFC must define result types and overflow behavior
before code generation emits arithmetic. Generated C must not expose
Seawitch signed operations to C signed-overflow undefined behavior.

### Outgoing C23 mapping

The generator uses the following canonical mapping everywhere, including
local declarations and future public C interfaces:

| Seawitch type | Outgoing C23 type | Required header |
|---|---|---|
| `Bool` | `bool` | none in C23; `<stdbool.h>` permitted for compatibility |
| `UInt8` | `uint8_t` | `<stdint.h>` |
| `UInt16` | `uint16_t` | `<stdint.h>` |
| `UInt32` | `uint32_t` | `<stdint.h>` |
| `UInt64` | `uint64_t` | `<stdint.h>` |
| `Int8` | `int8_t` | `<stdint.h>` |
| `Int16` | `int16_t` | `<stdint.h>` |
| `Int32` | `int32_t` | `<stdint.h>` |
| `Int64` | `int64_t` | `<stdint.h>` |
| `Float32` | `float` | none |
| `Float64` | `double` | none |

The generator must not substitute assumed native spellings such as `int`,
`long`, or `unsigned long`.

`Ptr<T>` recursively uses the outgoing type of `T`. Examples omit binding and
access `const` qualifiers owned by RFC 0002:

| Seawitch | C23 base shape |
|---|---|
| `Ptr<UInt8>` | `uint8_t *` |
| `Ptr<Int64>` | `int64_t *` |
| `Ptr<Ptr<Float32>>` | `float **` |

Generated C must preserve each checked Seawitch value in a context carrying
its checked Seawitch type. A C initializer token need not itself have the
corresponding narrow C type: for example, the `255` in `uint8_t channel = 255`
has C type `int`, while the declaration supplies the required `uint8_t` type.
The initializer must convert exactly and without undefined, implementation-
defined, wrapping, saturating, or value-changing behavior.

Where the C expression's own type is observable rather than supplied by its
surrounding declaration, assignment, parameter, or return context, the
generator must emit an explicit cast or another construction with the
canonical outgoing type.

The generator may use `<stdint.h>` constant macros, casts, hexadecimal
floating constants, or standard suffixes as appropriate. Values that need
them use `INT64_C` or `UINT64_C`; code generation must not rely on an
overflowing unsuffixed C literal or a value-changing narrowing conversion.
For a negative non-minimum `Int64`, the sign is outside the macro invocation,
as in `-INT64_C(123)`. `INT64_C(-123)` is invalid because the macro argument
must be an unsuffixed integer constant.

A signed minimum must not be emitted by first constructing its unrepresentable
positive magnitude in the corresponding signed C type. The generator uses
`INT8_MIN`, `INT16_MIN`, `INT32_MIN`, or `INT64_MIN`, or an equivalent C23
constant expression whose intermediate values are representable.

Source literal spelling is not part of program semantics. The checker-owned
exact value is authoritative. Code generation must format that value; it must
never copy an unchecked raw source token into generated C.

Except for signed minima, which use the safe `INT*_MIN` lowering above,
generated C preserves readable hexadecimal masks and binary bit layouts. The
generator deterministically formats the checked value as follows, without
constraining which internal compiler representation carries the source radix:

- digit-separator underscores are removed;
- decimal values use decimal C23 syntax;
- hexadecimal values use hexadecimal C23 syntax;
- binary values use binary C23 syntax; and
- octal values use canonical decimal C23 syntax, never C's leading-zero octal
  form.

The generator reconstructs the digits from the checked value and radix rather
than trusting the original token text. Required type macros such as `INT64_C`
and `UINT64_C` still wrap the formatted value when necessary.

The use of `0b` in generated output relies deliberately on Seawitch's C23-only
output contract. A future C17 compatibility mode would need a separate
lowering rule.

Floating output always uses a hexadecimal C floating constant reconstructed
from the checked binary32 or binary64 value. `Float32` output uses the `f`
suffix; `Float64` output is unsuffixed. Negative zero is emitted with an
explicit negative sign, for example `-0x0p+0f`. This single exact output path
does not depend on the C translator's permitted implementation-defined choice
when converting an inexact decimal floating constant.

Scalar type and literal generation is total. Every checked scalar reaches an
explicit code-generation case. An unsupported or internally unknown checked
type is a compiler error; it must never silently emit `0`, omit output, or
choose an arbitrary C type.

No scalar wrapper structure, tagged representation, hidden heap allocation,
or runtime conversion helper is emitted.

### Required target profile

An implementation supports a target under this RFC only when:

1. the downstream C compiler accepts the generated C23 dialect;
2. `CHAR_BIT` is 8;
3. all exact-width `<stdint.h>` types used by this RFC exist with their
   standard ranges and no padding bits; and
4. `float` and `double` provide IEC 60559 binary32 and binary64 respectively,
   including their value encoding rather than only matching precision and
   exponent ranges.

The presence of `uint8_t` and `int8_t` supplies the required unsigned and
signed eight-bit C types. This RFC imposes no separate one-byte layout rule on
`bool`; lowering to C23 `bool` preserves the C target's own compatible layout.

The compiler must establish the floating representation through trusted target
metadata, a target probe, or sufficient implementation-provided conformance
evidence. It may treat either `__STDC_IEC_60559_BFP__` or the legacy
`__STDC_IEC_559__` as sufficient evidence because Annex F conformance entails
the required formats. Neither macro is necessary evidence: its absence does
not prove that the representation is incompatible and must instead cause the
compiler to use trusted target metadata or a probe. Annex F arithmetic,
rounding, exception, and NaN semantics beyond the required value encoding are
not part of this RFC's target contract.

The `FLT_IS_IEC_60559` and `DBL_IS_IEC_60559` macros alone establish matching
precision and exponent ranges and are necessary but not sufficient evidence
for the complete representation contract above.

Generated C validates the value-set and size properties that C constant
expressions can establish with C23 `static_assert`, which requires no header.
These checks supplement rather than replace the compiler's representation
evidence. A generated prelude may include checks equivalent to:

```c
#include <float.h>
#include <limits.h>
#include <stdint.h>

static_assert(CHAR_BIT == 8, "Seawitch requires 8-bit bytes");
static_assert(FLT_RADIX == 2, "Seawitch requires binary floating point");
static_assert(sizeof(float) == 4 && FLT_MANT_DIG == 24 &&
              FLT_MAX_EXP == 128 && FLT_IS_IEC_60559 == 1,
              "Seawitch Float32 requires the binary32 value set");
static_assert(sizeof(double) == 8 && DBL_MANT_DIG == 53 &&
              DBL_MAX_EXP == 1024 && DBL_IS_IEC_60559 == 1,
              "Seawitch Float64 requires the binary64 value set");
```

There is no portable C constant expression that proves the complete byte-level
encoding. The compiler therefore owns that part of target validation and must
not interpret the absence of an Annex F conformance macro as a failed encoding
check. A proven mismatch or an inability to establish the required
representation fails with a clear unsupported-target diagnostic; a property
covered by the generated checks may instead fail through its C static
assertion. Neither case may silently change a Seawitch type's meaning.

## Grammar impact

This RFC adds binary and explicit-octal integer lexical forms, formalizes
decimal floating literals, and adds restricted negated numeric literals. Each
new type name continues to use the existing type-expression production:

```ebnf
type-expression = identifier
                | "Ptr" , "<" , type-expression , ">" ;
```

Numeric literals follow this lexical grammar. The predicates named by
`decimal-digit`, `binary-digit`, `octal-digit`, and `hex-digit` denote the
obvious ASCII digit sets.

```ebnf
integer-literal     = decimal-integer
                    | hexadecimal-integer
                    | binary-integer
                    | octal-integer ;

decimal-integer     = "0"
                    | nonzero-decimal-digit , { decimal-digit
                                              | "_" , decimal-digit } ;
hexadecimal-integer = "0x" , hex-digit , { hex-digit | "_" , hex-digit } ;
binary-integer      = "0b" , binary-digit , { binary-digit
                                            | "_" , binary-digit } ;
octal-integer       = "0o" , octal-digit , { octal-digit
                                           | "_" , octal-digit } ;

nonzero-decimal-digit = "1" | "2" | "3" | "4" | "5"
                      | "6" | "7" | "8" | "9" ;
decimal-digit       = "0" | nonzero-decimal-digit ;
binary-digit        = "0" | "1" ;
octal-digit         = "0" | "1" | "2" | "3"
                   | "4" | "5" | "6" | "7" ;
hex-digit           = decimal-digit
                    | "a" | "b" | "c" | "d" | "e" | "f"
                    | "A" | "B" | "C" | "D" | "E" | "F" ;

decimal-floating-literal
                    = decimal-integer , "." , decimal-digit-sequence
                    , [ exponent-part ]
                    | decimal-integer , exponent-part ;

exponent-part       = ( "e" | "E" ) , [ "+" | "-" ]
                    , decimal-digit-sequence ;
decimal-digit-sequence
                    = decimal-digit
                    , { decimal-digit | "_" , decimal-digit } ;
```

The whole-number part of a decimal floating literal uses `decimal-integer`, so
`.5`, `1.`, `00.5`, and `007.5` are invalid. `0.5`, `1.0`, `1e3`,
`1.0e-3`, and `1_000.25` are valid. The exponent digit sequence may contain
leading zeros. In every digit sequence, `_` may occur only between two digits.
Seawitch source has no hexadecimal floating-literal syntax; hexadecimal
floating constants are an exact generated-C representation only.

The expression grammar attaches restricted negated numeric literals at the
existing unary-expression level:

```ebnf
unary-expression         = negated-numeric-literal
                         | reference-expression
                         | postfix-expression ;
negated-numeric-literal  = "-"
                         , ( integer-literal
                           | decimal-floating-literal ) ;
```

The `-` remains a separate token. Whitespace and comments may separate it from
the literal; "directly" means that the literal is its syntactic operand.
`-name`, `-(1)`, repeated negation, and negation of any other expression are
not introduced by this production.

A future operator RFC may generalize this production while preserving its
exact contextual-literal semantics. Prefix and binary `-` are distinguished
by grammar position, never by spacing: prefix `-` appears where an expression
operand begins, while binary `-` follows a completed left operand. Because a
newline is whitespace rather than a statement terminator, a future `1` then
newline then `-2` sequence inside an expression parses as `1 - 2`.

This rule is unambiguous at statement level while every statement begins with
an identifier or `mut`; no current statement can begin with `-`. Any future
proposal for expression statements or another statement form beginning with
`-` must revisit this invariant in the terminator-free grammar.

`7e3` is tokenized as a decimal floating literal rather than an integer
literal followed by an identifier. The earlier experimental byte-literal
form is recognized only to produce the removal diagnostic specified above; it
does not produce an expression token. No numeric suffix, cast syntax, general
arithmetic operator, or foreign declaration syntax is added.

## Diagnostics

Representative diagnostics are:

```text
[Type Error] Byte was removed; use UInt8
[Type Error] given value is outside the UInt8 range
[Type Error] given value is outside the Int8 range
[Type Error] integer literal without an expected type does not fit Int32
[Type Error] negated integer literal requires a signed destination
[Type Error] expected UInt16 initializer, got UInt32
[Type Error] expected Float32 initializer, got Int32
[Syntax Error] byte literals were removed; use a UInt8 integer literal
[Syntax Error] integer base prefixes must be lowercase
[Syntax Error] malformed binary integer literal
[Syntax Error] decimal integer literals cannot have leading zeros
[Syntax Error] malformed decimal floating literal
[Target Error] target does not provide IEC 60559 binary32 float
```

Diagnostic wording may be refined during implementation, but every failure
must remain structured, source-located where applicable, and owned by the
earliest phase that can prove it.

## Interaction with pointers and mutability

RFC 0001 pointer construction applies uniformly to every scalar type. RFC
0002 binding and pointer-access rules are unchanged:

```seawitch
mut channel: UInt8 = 255
reader: Ptr<UInt8> = ref channel
writer: Ptr<UInt8> = mut ref channel
```

This lowers to the equivalent of:

```c
uint8_t channel = 255;
const uint8_t *const reader = &channel;
uint8_t *const writer = &channel;
```

Scalar signedness and width do not affect pointer capability. Pointer
capability does not authorize numeric conversion.

## Non-normative appendix: future C importer constraints

This appendix records compatibility requirements for a future C importer. It
does not define an importer, foreign declaration syntax, export syntax,
qualifier translation, or implementation acceptance criteria for this RFC.
A future FFI RFC may incorporate and supersede it.

An importer should classify C types by C type compatibility after expanding
typedef names, not by spelling or equal width alone:

| Compatible incoming C type | Intended Seawitch type |
|---|---|
| `bool` or `_Bool` | `Bool` |
| `uint8_t` | `UInt8` |
| `uint16_t` | `UInt16` |
| `uint32_t` | `UInt32` |
| `uint64_t` | `UInt64` |
| `int8_t` | `Int8` |
| `int16_t` | `Int16` |
| `int32_t` | `Int32` |
| `int64_t` | `Int64` |
| `float` | `Float32` |
| `double` | `Float64` |

For example, if `int64_t` is compatible with `long`, incoming `long` may map
to `Int64`; a same-width but incompatible `long long` must not. This matters
for pointers, callbacks, arrays, and structure fields, where C cannot repair
an incompatible type through a by-value arithmetic conversion.

C23 `char8_t` has the same C type as `unsigned char`. It may therefore map to
`UInt8` on a target where that type is compatible with `uint8_t`. This is an
ABI and storage statement, not a claim that arbitrary bytes form valid UTF-8.

Plain `char`, wide character types, `long double`, extended and decimal
floats, complex types, `_BitInt(N)`, pointer-sized integer typedefs, enums,
and atomic scalar types must not be guessed into these core types. Qualifiers,
nullability, array bounds, and function-pointer structure must not be silently
discarded. An importer that cannot represent a property must reject the
declaration as unsupported.

## Drawbacks

1. The core contains eleven scalar names rather than one generic integer and
   floating type.
2. `UInt8` communicates width and signedness but not the programmer's intent
   to treat a value as raw binary data. Removing `b'...'` also temporarily
   removes readable character-spelled bytes, so ASCII values must be written
   numerically until a future text or byte-alias RFC provides another form.
3. Until explicit conversions exist, moving a typed value between different
   numeric types is impossible rather than merely verbose.
4. Canonical fixed-width C lowering cannot represent every same-width native
   C pointer type; some future foreign APIs may require compatibility types or
   adapters.
5. The strict floating target profile excludes unusual but conforming C23
   implementations.
6. Exact hexadecimal floating constants are less familiar to many C readers
   than decimal constants, although they make the checked value unambiguous.

## Alternatives considered

### Keep `Byte` as the unsigned eight-bit type

Rejected. Its special literal behavior came from the earlier, higher-level
Hexal design and made it inconsistent with the other fixed-width integers.
Seawitch uses `UInt8` for the direct `uint8_t` concept.

### Add both `Byte` and `UInt8` as distinct types

Rejected. They would have identical values and C representation while forcing
conversions between two names for the same machine concept.

### Add `Byte` later as an alias of `UInt8`

Deferred to the binary-data or string RFC. An alias could communicate intent
without introducing a second representation or conversion boundary.

### Lower the eight-bit type to `char8_t`

Rejected. C23 `char8_t` communicates a UTF-8 code unit, while `UInt8` also
represents arbitrary binary data. The canonical mapping is `uint8_t`.

### Add target-dependent `Int` and `UInt`

Rejected for the initial core. Fixed-width names make storage and ABI choices
visible. Target-sized memory types are specified separately.

### Infer the narrowest or widest fitting type for an uncontextualized literal

Rejected. Magnitude-dependent inference makes small source edits change types
and ABIs. The fixed `Int32` fallback is simple and deterministic; larger or
unsigned values require an expected typed context.

### Use C-style leading-zero octal literals

Rejected. Treating `0755` as octal makes a visually minor leading zero change
the value and conflicts with decimal expectations. Seawitch requires the
explicit `0o755` spelling and diagnoses redundant leading zeros in decimal
integers.

### Omit binary or octal integer literals

Rejected. Binary integer literals make bit layouts readable, while octal remains the
natural notation for values such as Unix permission masks. Both add only an
explicit base prefix and use the same contextual typing and range checking as
decimal and hexadecimal integer literals.

### Add integer type suffixes

Rejected. Expected types and the `Int32` fallback already select an integer
type. Suffixes would create a second type-selection mechanism.

### Defer negative floating constants to general operators

Rejected. Negative floating constants are fundamental scalar values, and
supporting negative integers while leaving `-4.5` and `-0.0` unwritable would
create an arbitrary asymmetry. Restricted literal negation adds no general
typed-expression arithmetic.

### Emit decimal C floating constants when they appear to round-trip

Rejected. C23 permits an implementation-defined adjacent result when
translating an inexact decimal floating constant. Always emitting the checked
value as a hexadecimal C floating constant provides one exact lowering path.

### Make every integer literal intrinsically `Int32`

Rejected. That would require an implicit conversion even in declarations such
as `value: UInt64 = 5_000_000_000`. Contextual literals allow direct checking
against the intended type without introducing numeric conversions.

### Add `Float16`, `Int128`, or `UInt128`

Deferred. They are useful in specialized domains but are not required for the
initial game, web, memory, and C23 integration goals.

### Map incoming C types by width

Rejected as future FFI guidance. Same-width C types can remain incompatible,
especially behind pointers. Width-only mapping would require hidden casts and
could change aliasing, qualifier, callback, and structure-field behavior.

## Unresolved questions and separate RFCs

This RFC intentionally does not decide:

- `Size`, `ISize`, target selection, or target-profile configuration;
- explicit numeric conversion syntax;
- checked, wrapping, saturating, or trapping arithmetic operators;
- division, remainder, shift, and mixed-expression result rules;
- NaN, infinity, and floating comparison APIs;
- bit reinterpretation;
- pointer arithmetic and pointer/integer conversion syntax;
- `Byte` as a possible alias of `UInt8`;
- `Rune`, `String`, C strings, or text encoding;
- `Float16`, 128-bit integers, complex numbers, or SIMD vectors;
- structure, enum, array, slice, or function ABI lowering;
- C header importing, foreign declarations, and exports; or
- imported pointer access contracts and C qualifiers.

## Implementation acceptance criteria

Implementation is complete when end-to-end compiler tests prove that:

1. every listed scalar name resolves, including `UInt8`, while `Byte` receives
   a focused removal diagnostic recommending `UInt8`;
2. each unsigned integer accepts zero and its maximum representable value;
3. each signed integer accepts zero and its maximum representable value, and
   direct contextual negation accepts its minimum without requiring the
   positive operand to fit first;
4. every integer type rejects a positive literal one above its maximum, and
   every signed integer rejects a negated result one below its minimum,
   without truncation, wrapping, or saturation;
5. contextual integer literals are checked directly against every supported
   expected integer type, including `UInt8` and `UInt64`;
6. declaration annotations, assignment destinations, and pointer `.value`
   assignment places provide their type directly to contextual literal
   checking;
7. decimal, hexadecimal, binary, and octal spellings produce the same checked
   value and type when they denote the same mathematical integer;
8. lowercase `0x`, `0b`, and `0o` prefixes and well-placed underscore
   separators are accepted, while uppercase prefixes, invalid base digits,
   misplaced separators, empty prefixed values, and redundant decimal leading
   zeros are rejected lexically;
9. removed `b'...'` byte literals receive a focused migration diagnostic and
   never reach parsing, checking, or code generation as expressions;
10. valid decimal floating forms, exponents, and well-placed underscores are
    accepted, while `.5`, `1.`, redundant coefficient leading zeros, missing
    exponent digits, and misplaced underscores receive lexical diagnostics;
11. exponent notation is classified as a decimal floating literal and cannot
    directly initialize an integer type;
12. an integer literal with no usable expected type defaults to `Int32`, never
    widens based on magnitude, and fails if it does not fit;
13. a decimal floating literal takes an expected `Float32` or `Float64` and
    otherwise defaults to `Float64`;
14. `-` followed by an integer or decimal floating literal parses at the unary
    expression level regardless of intervening whitespace or comments, while
    negation of other expressions remains unsupported;
15. a negated integer literal takes an expected signed integer type or defaults
    to `Int32`, and an unsigned destination produces the dedicated
    signed-destination diagnostic, including for `-0`;
16. a negated decimal floating literal takes an expected floating type or
    defaults to `Float64`, preserves negative zero, and preserves the negative
    sign when a negative value underflows to zero;
17. scalar initialization and assignment require identical typed values;
18. `Float32` and `Float64` literals are correctly rounded using
    round-to-nearest, ties-to-even; overflow to infinity is rejected and finite
    subnormal or zero underflow is accepted;
19. every scalar and nested `Ptr<T>` lowers to its canonical outgoing C23 type
    while RFC 0002 supplies the recursive pointer capability qualifiers;
20. generated scalar type and literal mapping is total: no unsupported case
    silently emits `0`, omits output, or substitutes another C type;
21. generated integer constants preserve the full checked range, including
    values above signed 64-bit maximum in `UInt64`, and emit signed minima
    without an overflowing positive signed intermediate; negative non-minimum
    `Int64` values place the sign outside `INT64_C`;
22. generated C formats integer constants from checked exact values, preserves
    hexadecimal and binary radix except for signed-minimum macro lowering,
    translates octal values to canonical decimal, and never emits an unchecked
    raw source token;
23. every generated floating constant uses exact hexadecimal C23 syntax,
    `Float32` uses the `f` suffix, `Float64` is unsuffixed, and negative zero
    retains its sign;
24. an integer initializer may rely on an exact, well-defined conversion from
    its C constant to the surrounding canonical type, while a context where
    the expression's own C type is observable receives an explicit canonical
    type;
25. the compiler establishes binary32 and binary64 representation through
    trusted target metadata, a target probe, or sufficient positive
    conformance evidence, and does not treat the absence of an Annex F macro as
    proof of incompatibility;
26. generated C includes every required header and validates the target
    properties expressible through C constant expressions, including byte
    width, floating sizes, radix, precision, exponent ranges, and the
    `FLT_IS_IEC_60559` and `DBL_IS_IEC_60559` value-set requirements;
27. unsupported or unproven targets fail clearly rather than changing a type's
    meaning; and
28. every failure produces structured diagnostics and failure output.

## Implementation handoff requirements

After implementation behavior stabilizes, update `docs/grammar.md`,
`docs/language.md`, and `docs/status.md` once to describe the implemented
language. Documentation completion is a release requirement, not a behavior
proved by compiler tests.
