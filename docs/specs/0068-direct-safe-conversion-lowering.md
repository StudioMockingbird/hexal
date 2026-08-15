# RFC 0068: Direct Scalar Lowering and Source-Faithful Generated C

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-15
- Scope: generated C for explicit scalar conversions, float literals, and
  immutable binding reads
- Depends on: RFC 0038 (explicit `to<T>()` conversions)
- Coordinates with: RFC 0049 (`Size` target behavior), RFC 0062
  (demand-driven `hexal.h`), `docs/reference.md`, workbench snippets, and
  generated-C manifests

## Summary

Lower an explicit conversion directly when the conversion cannot trap. Keep a
deduplicated helper only when runtime validation is required. Emit readable,
bit-preserving floating constants and retain named source bindings in ordinary
generated expressions.

For:

```hexal
mut adjusted: Float64 = balance.to<Float64>() + power_level.to<Float64>()
```

with UInt8 operands, generate the equivalent of:

```c
double adjusted = (double)balance + (double)power_level;
```

Do not generate:

```c
static inline double hex_convert_uint8_t_double(uint8_t value) {
    return (double)value;
}
```

This RFC changes generated-C structure only. Hexal syntax, typing, rounding,
traps, compile-time reasoning, and evaluation order remain unchanged. It does
change one lowering optimization: an ordinary read of a named immutable
binding is no longer replaced by its initializer literal.

## Motivation

The current generator:

1. discovers every dynamic `ConversionExpression` source/destination pair;
2. emits one helper for every pair;
3. routes every call through that helper; and
4. emits `hex_numeric_trap` whenever any conversion pair exists.

Many helpers contain only one cast. They add indirection and generated noise
without enforcing a language contract. A safe-only program may also receive an
unused numeric-trap definition and its header requirements.

Hexal requires human-readable C23 and demand-driven support. A direct C cast is
the clearest lowering when no validation is necessary.

Two adjacent lowering behaviors currently undermine that goal:

- finite floats are emitted through a custom hexadecimal formatter; its
  Float32 fraction is misaligned, so `0.75` currently becomes `0x1.4p-1f`
  (`0.625f`) instead of `0x1.8p-1f`; and
- immutable binding reads are replaced with known literals, so
  `if enabled then` becomes `if (true)` even though the generated C still
  declares `hex_v_enabled`.

The first is a correctness bug. The second preserves runtime behavior but
damages source correspondence, creates unused declarations, and introduces an
optimization Hexal does not need at this stage.

## Invariants

1. No conversion changes its accepted source/destination pairs.
2. Compile-time-invalid constants remain checker diagnostics.
3. Literal-only constant expressions continue to fold and emit neither a
   helper nor a runtime cast unless their surrounding lowering independently
   requires one.
4. Dynamic invalid values trap before the unsafe C conversion.
5. Integer/float rounding, truncation, Rune validation, and finite-overflow
   behavior remain exactly as specified in `docs/reference.md`.
6. Every source expression evaluates exactly once.
7. Direct and helper lowering preserve the source expression's C sequencing
   behavior; no temporary or helper call may add a second evaluation.
8. Checked helpers and `hex_numeric_trap` remain deduplicated program-wide.
9. `Size` is target-sized; no classifier may use its synthetic Go `Bits` field
   as proof that a conversion is portable or non-trapping.
10. Every emitted finite float literal reparses to exactly the checked IEEE
    bits of its Hexal value, including signed zero.
11. An ordinary read of a named binding remains a read of that generated C
    binding. Known-value metadata may inform diagnostics and constant-required
    checks but must not replace the operand rendered for that read.

## Lowering classification

Classify every checked dynamic conversion as exactly one of:

### Identity

Source and destination are the same canonical type. Byte and UInt8 identity
follows their existing canonical aliasing.

Lower to the operand itself:

```hexal
value.to<Int32>()
```

```c
value
```

No helper, cast, or numeric trap is emitted.

### Direct cast

The conversion may round according to its existing contract but cannot produce
an invalid Hexal value or require a runtime guard.

Current direct families are:

- fixed-width integer or Rune to Float32/Float64;
- Float32 to Float64; and
- fixed-width integer/Rune to fixed-width integer when the complete source
  domain is contained in the destination domain.

Examples:

```text
UInt8   -> Float64
UInt64  -> Float32
Float32 -> Float64
Int8    -> Int32
UInt16  -> Int32
Rune    -> UInt32
```

Lower to one parenthesized C cast over one rendered operand:

```c
(double)value
```

Precision loss allowed by the Hexal conversion contract does not itself
require a helper. For example, UInt64 to Float32 rounds but remains within the
Float32 finite exponent range.

### Checked helper

Retain one helper per concrete source/destination pair when runtime validation
is required.

Current checked families include:

- integer narrowing or signedness changes whose source domain does not fit the
  destination;
- every integer-to-Rune conversion, which validates Unicode scalar range and
  excludes surrogates;
- every Float-to-integer conversion, which rejects NaN/infinity, truncates, and
  validates the destination range; and
- Float64-to-Float32, which traps when a finite input overflows to infinity.

Examples:

```text
Int64   -> Int8
Int32   -> UInt32
UInt32  -> Rune
Float64 -> Int32
Float64 -> Float32
```

The existing checked helper form remains appropriate:

```c
static inline int8_t hex_convert_int64_t_int8_t(int64_t value) {
    if (!(value >= INT8_MIN && value <= INT8_MAX)) {
        hex_numeric_trap();
    }
    return (int8_t)value;
}
```

## `Size` rule

Except for identity, do not newly direct-lower a conversion involving Size in
this RFC. Keep it on the existing checked/target-dependent path until the
compiler can prove the conversion safe without assigning Size a fixed width.

The implementation must audit `integerRangeFits`: `SizeType.Bits == 64` is an
internal placeholder and is not representation evidence. RFC 0068 must not use
that field to classify a Size conversion as direct.

This restriction is conservative and does not change the existing `Size`
semantic contract. A later target-profile implementation may direct-lower a
Size pair when selected-target evidence proves it safe.

## Floating-literal rendering

Render each finite Float32 or Float64 value as the shortest readable decimal C
literal that round-trips to the same already-rounded IEEE bits:

- Float32 receives the `f` suffix;
- Float64 has no suffix;
- an integral-looking mantissa receives `.0` unless an exponent already makes
  it a floating constant;
- negative zero retains its sign; and
- NaN and infinity retain their existing standard-macro lowering and sign
  handling.

For example:

```hexal
balance: Float32 = 0.75
```

must generate the equivalent of:

```c
const float hex_v_balance = 0.75f;
```

It must not generate the current incorrect value:

```c
const float hex_v_balance = 0x1.4p-1f;
```

Use a standard, bit-round-tripping formatter. Do not maintain a custom finite
hexadecimal-float encoder solely for generated literals. Formatting must start
from the checked rounded bits, not by reparsing the original source spelling.

## Named immutable bindings

Keep constant knowledge separate from operand identity. The checker may retain
the initializer value as metadata for range checks, exhaustiveness, bounds, or
another context that requires a compile-time-known value. It must not replace
an ordinary identifier read with that value in the checked expression passed
to generation.

For:

```hexal
enabled: Bool = true
if enabled then
end
```

generate:

```c
const bool hex_v_enabled = true;
if (hex_v_enabled) {
}
```

Do not generate `if (true)` while still emitting `hex_v_enabled`. The same rule
applies to named numeric, Rune, EoS, and other immutable scalar bindings.

This rule does not prohibit folding an expression written entirely from
literals, nor does it weaken diagnostics that consult the binding's known-value
metadata.

## Generator design

### Classification API

Introduce one generator-side classification function or equivalent direct
switch. The exact Go names are non-normative. It must return enough information
to distinguish:

```text
identity
direct cast
checked helper
```

Do not classify by rendering a helper and inspecting its C text.

### Discovery

Change conversion discovery to collect only checked-helper pairs for helper
emission. Direct and identity conversions remain in the checked program but do
not enter the helper set.

If header-requirement discovery needs conversion information, it must derive
requirements from the same semantic classification rather than assuming every
conversion needs trap/math/limit support.

### Rendering

`renderConversion` must:

- render the operand once;
- return the operand for identity;
- return one correctly parenthesized cast for direct conversion; and
- call the deduplicated helper for checked conversion.

Do not introduce a temporary solely for a direct cast. Existing surrounding
lowering may still introduce a temporary for its own evaluation-order contract.

### Trap support

Emit `hex_numeric_trap` only when at least one selected numeric operation calls
it. A program containing only direct or identity conversions must not receive
the trap definition or conversion-only `<stdio.h>`/`<stdlib.h>` requirements.

Division, shifts, or another independently checked numeric operation may still
require the same deduplicated trap.

### Checked helpers

Preserve existing helper names, guards, diagnostics, and deduplication for
checked pairs unless a mechanical rename is required by current generator
ownership. Do not inline checked guards at every call site.

## Examples

### Safe integer to Float

```hexal
fun widen(value: UInt8): Float64 do
    return value.to<Float64>()
end
```

Required shape:

```c
return (double)value;
```

Forbidden:

```c
return hex_convert_uint8_t_double(value);
```

### Safe widening

```hexal
fun widen(value: Int16): Int64 do
    return value.to<Int64>()
end
```

Required shape:

```c
return (int64_t)value;
```

### Checked narrowing

```hexal
fun narrow(value: Int64): Int8 do
    return value.to<Int8>()
end
```

The call must continue to use one range-checking helper.

### Checked Float conversion

```hexal
fun integer(value: Float64): Int32 do
    return value.to<Int32>()
end
```

The helper must continue rejecting NaN, infinity, and out-of-range values
before the C cast.

## Required tests

### Direct lowering

- Dynamic UInt8-to-Float64 emits a direct cast and no conversion helper.
- Two uses of the same safe pair emit two direct casts and no helper.
- Dynamic Int16-to-Int64 emits a direct cast.
- Dynamic UInt64-to-Float32 emits a direct cast and retains allowed rounding.
- Dynamic Float32-to-Float64 emits a direct cast with no overflow helper.
- Dynamic Rune-to-UInt32 emits a direct cast.
- Identity conversion emits neither helper nor cast.
- A non-atomic operand expression appears exactly once inside the direct cast.

### Checked lowering

- Int64-to-Int8 retains one deduplicated range-checking helper.
- signed-to-unsigned conversion retains lower/upper checks as applicable.
- integer-to-Rune retains Unicode scalar validation.
- Float64-to-Int32 retains NaN, infinity, truncation, and range behavior.
- Float64-to-Float32 retains finite-overflow validation.
- Repeated checked pairs emit one helper.
- Mixed safe and checked conversions emit helpers only for checked pairs.

### Trap and includes

- A safe-conversion-only program contains no `hex_numeric_trap` unless another
  selected numeric operation needs it.
- Safe conversions alone add no trap-related standard headers.
- A checked conversion emits exactly one usable numeric trap.
- Conversion trap support deduplicates correctly with guarded division or
  another numeric-trap user.

### Existing contracts

- Literal-only known valid constants remain folded.
- Known invalid constants remain checker errors.
- Decimal Float32 and Float64 output reparses to the exact checked bits.
- Positive zero, negative zero, minimum subnormal, maximum finite, NaN, and
  positive/negative infinity preserve their existing values and signs.
- `0.75` as Float32 emits `0.75f`, never the incorrect `0x1.4p-1f`.
- A read of an immutable Bool binding remains a generated binding read.
- Immutable numeric binding reads remain generated binding reads through
  arithmetic and explicit conversions.
- Known-value metadata still supports every compile-time diagnostic and
  constant-required check that used it before this RFC.
- Size conversion tests continue to enforce target-dependent behavior and do
  not gain direct lowering from `SizeType.Bits`.
- Generic specializations classify their closed concrete conversion pair.
- Multi-module discovery and generated output remain deterministic.
- Source mapping, diagnostics, artifact keys, linkage, and public compiler API
  remain unchanged.

## Test and fixture updates

Update:

- generator conversion tests;
- generator floating-literal tests;
- checker tests that currently require immutable identifier substitution;
- integration conversion tests;
- integration tests for named immutable conditions and arithmetic;
- exact generated-C expectations;
- workbench generated-C hashes; and
- dormant C23 canary expectations where their source strings inspect helpers.

Do not add runnable C23 tests or invoke an external compiler from ordinary
tests.

## Reference synchronization

No language-semantic edit is expected. During implementation, verify that
`docs/reference.md` still states the same explicit-conversion and constant
contracts and does not promise helper-based lowering or substitution of named
immutable reads.

If the reference contains a generated-C rule requiring helpers for safe
conversions, replace it with the semantic rule that non-trapping conversions
lower directly. Otherwise record that no reference edit was required before
closure.

## Non-goals

- Changing `value.to<T>()` syntax.
- Adding implicit, wrapping, saturating, unchecked, or mode-selected
  conversions.
- Changing compile-time constant reasoning or accepted conversion pairs.
- Preserving identifier source spelling in C; existing C name mangling remains.
- Changing conversion rounding, truncation, range, Rune, NaN, infinity, or
  overflow semantics.
- Inlining runtime guards for checked conversions.
- Optimizing bit casts, endian conversion, arithmetic, division, remainder, or
  shifts.
- Assigning Size a fixed width or implementing target profiles.
- Invoking GCC or Clang from tests.

## Acceptance criteria

1. Identity conversions emit no operation.
2. Proven non-trapping conversions emit one direct C cast and no helper.
3. Potentially invalid conversions retain one deduplicated checked helper.
4. Safe conversions alone do not emit numeric-trap support.
5. Every conversion operand evaluates exactly once.
6. All existing Hexal conversion semantics remain unchanged.
7. Size receives no fixed-width assumption.
8. Every finite float literal round-trips to its checked IEEE bits; ordinary
   values use readable decimal C spelling and `0.75` Float32 emits `0.75f`.
9. Ordinary immutable identifier reads render the generated C binding rather
   than its initializer literal, without losing compile-time diagnostics.
10. Tests, workbench hashes, and any affected generated-C fixtures are updated.
11. `go test ./...`, `go vet ./...`, `go test -tags c23 ./...`, and
   `go vet -tags c23 ./...` pass without invoking an external toolchain.
12. `docs/reference.md` is reviewed and synchronized or explicitly verified
    unchanged before closure.

## Readiness

Implementation-ready. Classification, direct rendering, helper retention,
trap emission, Size conservatism, exact readable float rendering, named-binding
preservation, affected fixtures, and validation are fully specified; no
language-design decision remains.
