# RFC 0068: Direct Scalar Lowering and Source-Faithful Generated C

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-16. Classification,
  direct rendering, checked-helper retention, shared-trap selection, exact
  Float-to-integer guards and constant folding, Size conservatism, exact
  readable float rendering, and named-binding preservation are implemented.
  `docs/reference.md` was reviewed and required no edit: its explicit
  conversion and constant contracts are semantic-only and already match this
  RFC's lowering; no reference rule promised helper-based conversion lowering
  or substitution of named immutable reads.
- Created: 2026-08-15
- Updated: 2026-08-16
- Scope: explicit scalar conversion lowering and constant folding, generated C
  float literals, and immutable binding reads
- Depends on: RFC 0038 (explicit `to<T>()` conversions); RFC 0069 (one
  program-wide runtime trap and demand-driven C23 support)
- Coordinates with: RFC 0049 (`Size` target behavior), RFC 0062
  (demand-driven `hexal.h`), RFC 0070 (comment contract),
  `docs/reference.md`, workbench snippets, and generated-C manifests

## Summary

Lower an explicit conversion directly when the conversion cannot trap. Keep a
deduplicated helper only when runtime validation is required. Emit readable,
bit-preserving floating constants, correct Float-to-integer folding and runtime
bounds, and retain named source bindings in ordinary generated expressions.

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

This RFC primarily changes generated-C structure. It also corrects
Float-to-integer constant folding that currently routes every result through
Int64 and can disagree with the already-normative truncate-then-range-check
contract. Hexal syntax, conversion pairs, rounding, traps, and evaluation order
remain unchanged. One lowering optimization also changes: an ordinary read of
a named immutable binding is no longer replaced by its initializer literal.

## Motivation

The current generator:

1. discovers every dynamic `ConversionExpression` source/destination pair;
2. emits one helper for every pair;
3. routes every call through that helper; and
4. selects runtime-trap support whenever any conversion pair exists.

Many helpers contain only one cast. They add indirection and generated noise
without enforcing a language contract. A safe-only program may also receive an
unneeded program-wide runtime-trap definition and its header requirements.

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
8. Checked helpers remain deduplicated per concrete pair and call RFC 0069's
   one program-wide `hex_runtime_trap` with the existing exact diagnostic.
9. `Size` is target-sized; no classifier may use its synthetic Go `Bits` field
   as proof that a conversion is portable or non-trapping.
10. Every emitted finite float literal reparses to exactly the checked IEEE
    bits of its Hexal value, including signed zero.
11. An ordinary read of a named binding remains a read of that generated C
    binding. Known-value metadata may inform diagnostics and constant-required
    checks but must not replace the operand rendered for that read.
12. A dynamic Float-to-integer conversion truncates exactly once, checks the
    truncated result against exact mathematical destination bounds, and casts
    only after the check proves that the C conversion is defined.
13. A known Float-to-integer conversion applies the same operation to the
    source type's already-rounded IEEE value, without routing through Int64 or
    losing the UInt64 half above `INT64_MAX`.

## Sequencing after RFC 0069

Implement this RFC only after RFC 0069 is complete and its generated-output
fixtures are stable. Use that completed tree as the baseline.

RFC 0069 changes conversion-adjacent infrastructure:

- all generated diagnostic traps use one program-wide `hex_runtime_trap`;
- the trap declaration and root definition own `<stdio.h>` and `<stdlib.h>`;
- a feature that calls the trap selects semantic trap support rather than
  claiming those headers independently; and
- generated-C fixtures already include RFC 0069's C23 arithmetic, memory,
  null, and trap changes.

RFC 0068 must preserve those contracts. Do not restore `hex_numeric_trap`, a
conversion-owned trap definition, per-family trap headers, `NULL`, or any
lowering removed by RFC 0069. Keep RFC 0068 fixture changes attributable to
conversion classification, float rendering, named-binding rendering, and the
corrected Float-to-integer guard.

Any source comment added or modified during implementation must follow RFC
0070's CARE contract and must not cite an RFC, ADR, plan, or internal spec.

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
        hex_runtime_trap("[Runtime Error] numeric operation failed\n");
    }
    return (int8_t)value;
}
```

### Correct Float-to-integer bounds

RFC 0069's C23 audit found that the existing Float-to-integer guard is unsafe
for 64-bit destinations. Comparing a Float64 value with `INT64_MAX` or
`UINT64_MAX` first converts that macro to Float64. The converted upper bound
rounds to `2^63` or `2^64`, so the guard can admit the first unrepresentable
value and execute an undefined C conversion.

This RFC owns the correction because it is the focused conversion-lowering
change. It does not change Hexal semantics: `docs/reference.md` already
requires Float-to-integer conversion to truncate toward zero and then check the
destination range.

The checker and generator must implement the same order. The current checker
first converts every Float constant through Go `float64` and `int64`; that
incorrectly rejects valid UInt64 results in `[2^63, 2^64)`, and it can reason
from lexical precision instead of the value already rounded to the declared
Float32 or Float64 source type. Remove that Int64 bottleneck as part of this
correction.

For a known Float-to-integer conversion, the checker must:

1. reconstruct or otherwise use the exact value represented by the source
   operand's checked Float32 or Float64 bits;
2. truncate that value toward zero without an Int64 intermediate;
3. compare the arbitrary-precision integral result with the destination's
   mathematical range; and
4. fold the valid destination value or diagnose a target-independent failure.

A known Float-to-Size result above the guaranteed portable minimum remains a
Size constant and follows the existing target-dependent `SIZE_MAX`
`static_assert` path. The checker must not use the placeholder `SizeType.Bits`
as target evidence.

For each dynamic Float-to-integer checked helper:

1. Reject NaN and positive or negative infinity through the shared runtime
   trap.
2. Evaluate `truncf(value)` for Float32 or `trunc(value)` for Float64 exactly
   once into a temporary of the source floating type.
3. Check that temporary against exact bounds expressed as powers of two.
4. Cast the temporary, not the original value, only after the check succeeds.

For a fixed-width signed N-bit destination, accept exactly:

```text
-2^(N-1) <= truncated < 2^(N-1)
```

For a fixed-width unsigned N-bit destination, accept exactly:

```text
0 <= truncated < 2^N
```

Use exactly representable floating power-of-two constants. Do not compare with
an integer maximum macro converted to Float32 or Float64, and do not use a
rounded inclusive upper bound.

Required signed shape:

```c
static inline int64_t hex_convert_double_int64_t(double value) {
    if (!isfinite(value)) {
        hex_runtime_trap("[Runtime Error] numeric operation failed\n");
    }
    double truncated = trunc(value);
    if (!(truncated >= -0x1p63 && truncated < 0x1p63)) {
        hex_runtime_trap("[Runtime Error] numeric operation failed\n");
    }
    return (int64_t)truncated;
}
```

For `Size`, whose width is selected by the C target, derive the exclusive upper
threshold without assuming a width:

```text
(source-floating-type)SIZE_MAX + 1.0 in the source floating type
```

The threshold is the target `size_t` range limit `2^N`. Check:

```text
0 <= truncated < target threshold
```

The addition occurs after converting `SIZE_MAX` to the source floating type;
it must not occur in `size_t`. Do not use `truncated <= SIZE_MAX`.

Truncation precedes the range check. Therefore a value strictly between `-1`
and `0`, including `-0.5`, truncates to signed zero and is valid for an
unsigned destination; `-1.0` and every value whose truncated result is
negative are invalid. This follows the existing reference contract and fixes
the current pre-truncation rejection.

Do not use C23 `fromfp` or `ufromfp`. Their domain-error result does not provide
the direct, deterministic success test Hexal requires and would add floating
environment or `errno` machinery.

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

Select RFC 0069's program-wide `hex_runtime_trap` only when at least one
checked conversion or another selected generated operation calls it. A program
containing only direct or identity conversions must not receive the trap
declaration, root definition, or conversion-originated trap requirement.

Conversion discovery marks the shared semantic trap requirement; it does not
claim `<stdio.h>` or `<stdlib.h>` directly. The shared trap definition owns
those headers. Division, shifts, bounds checks, allocation, or another
independently checked operation may still select the same trap.

### Checked helpers

Preserve existing helper names, diagnostics, and deduplication for checked
pairs unless a mechanical rename is required by current generator ownership.
Preserve existing guards except for the Float-to-integer correction specified
above. Do not inline checked guards at every call site.

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
- Float64-to-Int32 rejects non-finite values, truncates once, checks exact
  destination bounds, and casts the truncated temporary.
- Float64-to-Float32 retains finite-overflow validation.
- Repeated checked pairs emit one helper.
- Mixed safe and checked conversions emit helpers only for checked pairs.

### Float-to-integer correctness

- Float64-to-Int64 uses the inclusive `-2^63` lower bound and exclusive `2^63`
  upper bound; it does not compare against converted `INT64_MAX`.
- Float64-to-UInt64 uses the inclusive zero lower bound and exclusive `2^64`
  upper bound; it does not compare against converted `UINT64_MAX`.
- Float32 helpers use `truncf`; Float64 helpers use `trunc`.
- Every helper evaluates truncation once and casts the truncated temporary.
- Constant folding uses the checked source Float's rounded IEEE value and an
  arbitrary-precision integral result; it does not pass through Int64.
- A known Float64 value of `2^63` converts successfully to UInt64, while
  `2^64` is rejected as outside UInt64.
- A Float32 constant whose lexical value rounds before conversion uses the
  rounded Float32 value.
- Dynamic values in `(-1, 0)` are accepted for unsigned destinations after
  truncating to zero; a truncated negative result is rejected.
- Known values in `(-1, 0)` fold to unsigned zero under the same rule.
- Float-to-Size uses an exclusive target-derived threshold formed by converting
  `SIZE_MAX` to the source floating type before adding floating one.
- No Float-to-integer helper uses `fromfp`, `ufromfp`, `errno`, or floating
  environment state.
- The generated guards never execute a C Float-to-integer cast for a value
  outside the destination domain.

### Trap and includes

- A safe-conversion-only program does not select `hex_runtime_trap` unless
  another generated operation needs it.
- Safe conversions alone add no trap-related standard headers.
- A checked conversion selects exactly one usable program-wide runtime-trap
  declaration and root definition.
- Conversion trap support deduplicates correctly with guarded division,
  allocation, bounds checks, or another runtime-trap user.
- A conversion family does not independently claim `<stdio.h>` or
  `<stdlib.h>`; those requirements belong to the selected shared trap.
- Checked Float-to-integer helpers select `<math.h>` for `isfinite`, `truncf`,
  or `trunc` through the existing demand-driven header model.

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

Take the generated-output baseline only after RFC 0069 is complete. Do not
regenerate RFC 0068 expectations against a partially migrated trap, C23
arithmetic, memory, or null model.

Update:

- generator conversion tests;
- generator floating-literal tests;
- checker tests that currently require immutable identifier substitution;
- checker constant-conversion tests, including rounded Float32 sources,
  UInt64 values at and above `2^63`, negative fractions, and Size's
  target-dependent path;
- integration conversion tests;
- integration tests for named immutable conditions and arithmetic;
- exact generated-C expectations;
- workbench generated-C hashes; and
- dormant C23 canary expectations where their source strings inspect helpers.

Exact-output changes must retain RFC 0069's one `hex_runtime_trap`,
program-wide header discovery, C23 `nullptr`, and standard-memory lowering.
Do not accept a regenerated baseline that restores removed helper families.

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
- Changing compile-time constant reasoning or accepted conversion pairs,
  except for correcting Float-to-integer folding to apply the existing
  truncate-then-range-check contract to the already-rounded source value.
- Preserving identifier source spelling in C; existing C name mangling remains.
- Changing conversion rounding, truncation, range, Rune, NaN, infinity, or
  overflow semantics. Correcting Float-to-integer guards and truncation order
  to match those existing semantics is required work, not a semantic change.
- Inlining runtime guards for checked conversions.
- Optimizing bit casts, endian conversion, arithmetic, division, remainder, or
  shifts.
- Assigning Size a fixed width or implementing target profiles.
- Invoking GCC or Clang from tests.

## Acceptance criteria

1. Identity conversions emit no operation.
2. Proven non-trapping conversions emit one direct C cast and no helper.
3. Potentially invalid conversions retain one deduplicated checked helper.
4. Safe conversions alone do not select program-wide runtime-trap support.
5. Every conversion operand evaluates exactly once.
6. All existing Hexal conversion semantics remain unchanged.
7. Size receives no fixed-width assumption.
8. Every finite float literal round-trips to its checked IEEE bits; ordinary
   values use readable decimal C spelling and `0.75` Float32 emits `0.75f`.
9. Ordinary immutable identifier reads render the generated C binding rather
   than its initializer literal, without losing compile-time diagnostics.
10. Tests, workbench hashes, and any affected generated-C fixtures are updated.
11. Float-to-integer helpers truncate once, check exact inclusive-lower and
    exclusive-upper bounds, accept a negative fraction that truncates to
    unsigned zero, and never cast an out-of-domain value.
12. Constant Float-to-integer folding uses the rounded source Float value,
    supports valid UInt64 results above `INT64_MAX`, preserves Size target
    dependence, and matches the dynamic truncate-then-check order.
13. Conversion traps use only RFC 0069's `hex_runtime_trap`; no
    `hex_numeric_trap` or conversion-owned trap headers return.
14. `go test ./...`, `go vet ./...`, `go test -tags c23 ./...`, and
   `go vet -tags c23 ./...` pass without invoking an external toolchain.
15. The workbench is rebuilt into `bin/` and restarted after its generated-C
    hashes are updated.
16. `docs/reference.md` is reviewed and synchronized or explicitly verified
    unchanged before closure.

## Readiness

Implementation-ready. RFC 0069 is implemented, so its shared-trap and C23
output form is the stable baseline. Classification, direct rendering, helper
retention, shared-trap selection, exact Float-to-integer guards and constant
folding, Size conservatism, exact readable float rendering, named-binding
preservation, fixtures, and validation are fully specified; no language-design
decision remains.
