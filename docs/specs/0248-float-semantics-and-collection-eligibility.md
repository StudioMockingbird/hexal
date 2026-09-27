# RFC 0248: Float Semantics and Collection Eligibility

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Open Discussion; problem inventory recorded, surface decisions not
  yet authorized
- Created: 2026-09-27
- Origin: RFC 0223 Float-key review
- Coordinates with: RFC 0223 (Dict correctness), RFC 0233 (JSON), archived
  RFC 0187 (build modes), and `docs/reference.md`
- Reference synchronization: none until the decisions below are settled and
  implemented

## Purpose

Keep every Float32 and Float64 edge under one policy before floats gain more
language roles. Hexal already defines IEC 60559 arithmetic, comparison,
conversion, printing, interpolation, bit casting, and constant folding. The
remaining work is to prove those paths agree and to decide whether floats ever
belong in equality-sensitive collections such as Dict.

This RFC does not make Float a Dict key. RFC 0223 continues rejecting Float32
and Float64 keys.

## Existing contract

The current normative contract states:

- Float32 and Float64 use IEC 60559 binary32 and binary64 representations;
- arithmetic follows IEC 60559;
- NaN comparisons follow IEC rules;
- conversions round nearest, ties-to-even where applicable;
- float-to-integer conversion rejects NaN, infinity, and out-of-range values;
- printing preserves signed zero, infinity, and NaN spellings; and
- `bit_cast<T>()` permits equal-width fixed integer/Float32/Float64 pairs.

The checker separately implements constant folding over stored IEEE bits, while
generated C performs runtime arithmetic. Both routes must produce the same
observable result.

## Problems to settle

### 1. Classification surface

Hexal currently has no settled user-facing classification operations such as:

```text
Float32.is_nan() -> Bool
Float32.is_finite() -> Bool
Float32.is_infinite() -> Bool
Float32.sign_bit() -> Bool
```

Equivalent Float64 operations would follow. C23 already provides `isnan`,
`isfinite`, `isinf`, and `signbit`; generated C would use them directly with no
compiler-owned wrappers that merely delegate.

Open decision: whether Hexal needs this surface now, and whether these are
methods or compiler-known free functions. Recommendation: add methods only
when a concrete language feature needs classification; do not add them merely
for completeness.

### 2. Equality and signed zero

Ordinary equality remains IEC equality:

- `NaN == NaN` is false;
- `NaN != NaN` is true;
- `+0.0 == -0.0` is true.

No change is proposed. Any future hash compatible with ordinary equality must
canonicalize both signed zeros to one hash input and must decide how NaN is
treated before Float becomes a key.

### 3. Ordering

Ordinary `<`, `<=`, `>`, and `>=` remain IEC comparisons and therefore do not
form a total order in the presence of NaN. Sorting, ordered maps, canonical
serialization, and deterministic build metadata may later need a total-order
operation, but ordinary operators must not silently acquire one.

Open decision: whether a future explicit operation equivalent to IEC 60559
`totalOrder` is useful. Recommendation: defer until an ordered collection or
canonical serialization format requires it.

### 4. Dict eligibility

Float32 and Float64 remain invalid Dict keys. Admitting them would require one
of three explicit policies:

| Policy | NaN | Signed zero | Assessment |
| --- | --- | --- | --- |
| ordinary IEC equality | unfindable after insertion | equal, so hashes must canonicalize | invalid |
| reject NaN at every key operation | runtime trap/error surface in every operation | canonicalize | possible but high ceremony |
| canonical bitwise key identity | NaN payloads and signed zeros become distinguishable by bits | distinct | conflicts with ordinary `==` |

Recommendation: retain rejection. Reconsider only if real programs need Float
keys or Hexal introduces an explicit hash/equality context. A Float value can
already be converted to an application-defined canonical integer or inline
String key.

### 5. Serialization boundaries

Formats decide independently whether non-finite values are representable.
JSON continues rejecting NaN and infinity because JSON cannot encode them.
Foreign C calls retain the target ABI representation. A format-specific
rejection must not redefine ordinary Hexal Float arithmetic.

### 6. Compile-time/runtime conformance

Constant-folded and runtime-computed forms must agree for:

- positive and negative zero;
- quiet NaN results and comparisons;
- positive and negative infinity;
- Float32 rounding at every operation;
- Float32-to-Float64 widening;
- Float64-to-Float32 finite overflow;
- integer-to-float ties-to-even rounding;
- float-to-integer truncation, range checks, NaN, and infinity;
- printing and interpolation; and
- integer/float bit casts.

The contract compares observable classification, comparison, conversion,
formatted output, and exact bits where `bit_cast` exposes them. It does not
require different platforms to preserve a particular NaN payload unless the
reference later establishes that requirement.

## Direction

1. Keep existing IEC arithmetic and comparison semantics.
2. Keep Float32 and Float64 excluded from Dict.
3. Add no total-order or classification surface without a concrete consumer.
4. Build one conformance matrix proving checker folding and generated C agree.
5. Record every format-specific non-finite rule at that format boundary.
6. Use C23 `<math.h>` facilities directly wherever they express the required
   behavior.

## Non-goals

- Decimal floating-point types.
- Arbitrary precision floating point.
- Changing default literal inference from Float64.
- Fast-math semantics or reassociation.
- A new numeric protocol or operator overloading.
- Float Dict keys in this RFC.
- Normalizing every NaN payload to one exact bit pattern.

## Implementation plan after decisions are authorized

1. Inventory the reference, checker folding, generator arithmetic,
   conversions, printing, interpolation, JSON, C interop, and bit-cast paths.
2. Add paired folded/runtime fixtures for the conformance matrix before
   changing behavior.
3. Reconcile any reproduced mismatch at the earliest owning phase; do not add
   a separate analyzer pass.
4. If classification methods are approved, add their authoritative method
   records, direct `<math.h>` lowering, and exact generated-C tests.
5. Keep `types.IsDictKey` rejecting Float and add a regression covering both
   widths.
6. Synchronize `docs/reference.md` only for behavior that actually changes.
7. Run ordinary tests, tagged C23 conformance on every qualified target, and
   review any generated-C manifest movement by artifact family.

## Validation required before implementation-ready

1. Every existing Float statement in `docs/reference.md` has a paired folded
   and runtime test or an explicit reason it is compile-time-only.
2. Float32 operations prove rounding occurs at Float32 width rather than after
   a hidden Float64 evaluation.
3. Positive/negative zero, NaN, and infinities have comparison, conversion,
   printing, interpolation, and bit-level cases where applicable.
4. Float-to-integer conversion rejects NaN, infinity, and both bounds just
   outside every destination range.
5. Float64-to-Float32 conversion distinguishes finite rounding from finite
   overflow to infinity.
6. JSON and every other format-specific boundary retain their own documented
   non-finite policy.
7. Both Float key types remain rejected by Dict without generating hash or NaN
   guard helpers.
8. Ordinary and tagged C23 suites pass on every qualified target.

## Open decisions

1. Add Float classification methods now, or wait for a concrete consumer?
2. Is an explicit total-order operation needed before an ordered collection or
   canonical serializer exists?
3. What evidence would justify revisiting Float Dict keys?
