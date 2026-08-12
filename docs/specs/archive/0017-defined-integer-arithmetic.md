# RFC 0017: Defined Integer Arithmetic

- Kind: Language Semantics Specification (ISO/IEC Language Standard Format)
- Status: Implemented; conformance verified 2026-08-11
- Features: one wrapping integer-arithmetic model, defined signed division and
  remainder boundaries, and zero-divisor traps
- Created: 2026-08-09
- Revised: 2026-08-11
- Depends on: RFC 0003 (core scalar types), RFC 0009 (core operators), RFC
  0016 (lossless numeric widening and explicit conversions)
- Coordinates with: RFC 0019 (generic specialization) and RFC 0026
  (deferred cleanup and unrecoverable termination)

## Summary

Seawitch has one integer-arithmetic behavior. Addition, subtraction,
multiplication, and negation wrap to the result type. Integer division
truncates toward zero and remainder follows the dividend's sign. A zero divisor
is a compile-time error when known and an unrecoverable runtime trap otherwise.

```seawitch
high: Int8 = 127 + 1       // -128
low: Int8 = -128 - 1       // 127
counter: UInt8 = 255 + 1   // 0
```

There is no `checked`, `saturating`, or `wrapping` expression mode. Arithmetic
behavior follows directly from the operand and result types, leaving one
obvious spelling for each operation.

This RFC preserves RFC 0009's wrapping arithmetic and replaces its two
remaining integer undefined-behavior allowances with defined behavior. It does
not rely on C signed overflow, invalid division, implementation-defined
narrowing, or the target's native signed representation.

## Goals

1. Give every supported integer operation one deterministic result or one
   defined unrecoverable failure.
2. Preserve the existing ordinary operator syntax and wrapping behavior.
3. Add no arithmetic mode, operator, method, or reserved word.
4. Use RFC 0016's one lossless common-type relation before arithmetic.
5. Prevent generated C from executing signed overflow or invalid division.
6. Preserve exact constant folding and fail-closed generation.

## Non-goals

- Checked, saturating, selectable, or program-wide configurable overflow modes.
- Recoverable arithmetic errors.
- Bitwise operators, shifts, exponentiation, or floating remainder.
- Pointer arithmetic.
- User-defined arithmetic operators.

## Surface syntax

This RFC adds no syntax. The arithmetic operators remain:

```text
unary:  -
binary: +  -  *  /  %
```

`checked`, `saturating`, and `wrapping` remain ordinary identifiers rather than
reserved words. Forms such as these have no language meaning:

```seawitch
checked (left + right)     // ordinary call/name syntax, not an arithmetic mode
saturating (left + right)  // ordinary call/name syntax, not an arithmetic mode
```

No grammar change is required.

## Type selection

RFC 0009 continues to own operator eligibility. RFC 0016 selects the result
type for mixed numeric operands before this RFC applies arithmetic behavior.
The selected type determines which existing arithmetic model applies:

- an integer result uses this RFC's wrapping, division, remainder, and
  zero-divisor rules; and
- a floating result uses RFC 0009's IEC 60559 arithmetic, including its
  infinity, NaN, signed-zero, and floating-division behavior.

```seawitch
integer: Int32 = 10
floating: Float32 = 2.5

sum := integer + floating       // Float64, using floating arithmetic
quotient := integer / floating  // Float64, using floating division
```

The integer zero-divisor trap does not apply after common-type selection has
produced a floating type. Binary `%` remains integer-only under RFC 0009.

For binary integer arithmetic:

1. both operands must have identical canonical integer types or one unique
   least lossless common integer type under RFC 0016;
2. each operand is converted exactly once to that selected type;
3. the operation executes in that type; and
4. the result has that type.

```seawitch
small: Int8 = read_small()
wide: Int16 = read_wide()

result := small + wide     // operands and result are Int16
```

An expected destination may widen the completed result under RFC 0016, but it
does not change the common type selected inside the operation.

Untyped literals retain RFC 0003 and RFC 0009 contextual typing. The wrapping
rule never supplies a second literal-inference mechanism. A union must be
narrowed to one concrete numeric member before arithmetic; the narrowed type
then selects integer or floating behavior normally.

For integer operands, unary negation retains RFC 0009's signed-only eligibility
rule. Wrapping does not make unsigned unary negation valid:

```seawitch
value: UInt8 = 1
bad: UInt8 = -value
// Error: unary negation requires a signed integer
```

Floating unary negation remains valid and retains RFC 0009's IEC 60559
behavior:

```seawitch
value: Float64 = 1.5
negative: Float64 = -value      // -1.5
```

Floating arithmetic does not use the integer wrapping or zero-divisor rules in
this RFC.

## Wrapping definition

Let `T` be an `n`-bit integer result type and let `m` be the exact mathematical
result of an operation. First reduce `m` modulo `2^n` to the unique residue `r`
in the range `0` through `2^n - 1`.

For an unsigned `T`, the result is `r`.

For a signed `T`, the result is:

```text
r,       when r <= 2^(n-1) - 1
r-2^n,   otherwise
```

This defines two's-complement-style wrapping as a value rule without requiring
the generated C implementation to perform signed overflow.

### Addition, subtraction, and multiplication

Binary `+`, `-`, and `*` compute their mathematical result and map it to the
selected result type using the wrapping definition above:

```seawitch
add: Int8 = 127 + 1          // -128
subtract: Int8 = -128 - 1    // 127
multiply: UInt8 = 20 * 20    // 144
```

Wrapping applies at every arithmetic node, not once to the whole expression:

```seawitch
value: Int8 = (100 + 100) - 100
```

Evaluation is:

```text
100 + 100  -> -56
-56 - 100  -> 100
```

Parentheses affect grouping but never select another overflow behavior.

### Unary negation

Unary `-` computes the mathematical negative and wraps it to the same signed
type. Negating the signed minimum therefore returns that minimum:

```seawitch
minimum: Int8 = -128
same: Int8 = -minimum       // -128
```

## Division and remainder

For a nonzero divisor, integer division computes the mathematical quotient and
truncates it toward zero. Integer remainder is defined by:

```text
remainder = dividend - truncated_quotient * divisor
```

The remainder is zero or has the dividend's sign. These rules apply to signed
and unsigned integers after RFC 0016 common-type selection.

### Zero divisor

Division or remainder by zero has no mathematical result and cannot wrap. A
statically known zero divisor in an evaluated expression is a compile-time
error:

```seawitch
bad_division: Int32 = value / 0
bad_remainder: Int32 = value % 0
```

When the divisor is known only at runtime, the generated program tests it
before executing C division or remainder and enters the defined runtime trap
when it is zero.

### Signed minimum divided by `-1`

For a signed `n`-bit type, the mathematical quotient of its minimum value and
`-1` is `2^(n-1)`, one above the signed maximum. The ordinary wrapping rule
maps that quotient back to the signed minimum:

```seawitch
minimum: Int8 = -128
quotient: Int8 = minimum / -1   // -128
```

The generator must detect this pair before C division and produce the signed
minimum directly. It must never execute the corresponding undefined C
division.

The matching remainder is mathematically zero:

```seawitch
remainder: Int8 = minimum % -1  // 0
```

The generator must also detect this pair before C remainder and produce zero
directly, because evaluating the corresponding C operation may inherit the
invalid quotient case.

## Constant folding and reachability

The checker folds integer operations whenever their operands form a compile-
time constant expression under the existing RFC 0003 and RFC 0009 rules.
Wrapping overflow is a value, not a diagnostic:

```seawitch
wrapped: Int8 = 127 + 1          // folded to -128
quotient: Int8 = -128 / -1       // folded to -128
remainder: Int8 = -128 % -1      // folded to 0
```

A non-`mut` binding initialized from runtime data is not a compile-time
constant merely because it cannot be reassigned.

A statically known zero divisor is diagnosed only when the operation is in an
evaluated expression under the language's existing compile-time reachability
rules. For example, an unreachable right operand of a decisive constant `and`
or `or` remains type-checked but is neither folded nor diagnosed for its zero
divisor:

```seawitch
safe: Bool = true or (1 / 0 == 0)
```

Operations depending on runtime values remain explicit type-checked arithmetic
nodes for generation even though ordinary overflow itself requires no runtime
failure path.

## Evaluation

Each operand is evaluated exactly once. Wrapping reconstruction, zero-divisor
checks, and signed-minimum special cases reuse those evaluated values and must
not repeat an operand's side effects.

The relative evaluation order of independent operands remains unspecified
under RFC 0009. `and` and `or` retain their short-circuit behavior. If a zero-
divisor trap occurs, the enclosing expression produces no result and no later
dependent arithmetic operation executes.

## Runtime zero-divisor trap

A runtime zero-divisor trap terminates the current program without returning
an invalid value. The generated support region uses the defined numeric trap
model shared with RFC 0016. Its helper may call the C23 standard `abort`
facility, but it must not use C23 `unreachable`, execute the invalid operation,
or trigger another undefined or implementation-defined operation.

The trap is unrecoverable in this RFC. Consistent with RFC 0026, process
termination or another unrecoverable runtime failure is not promised to run
deferred actions. Recoverable arithmetic failure remains deferred.

## Interaction with generics

Arithmetic involving a type parameter is a dependent operation under RFC
0019. Open generic checking records the operator and source span. Closed
specialization substitutes concrete types and applies RFC 0016 common-type
selection. An integer result then uses this RFC; a floating result uses RFC
0009:

```seawitch
fun add<T>(left: T, right: T): T
    return left + right
end

sum: Int8 = add<Int8>(127, 1)       // -128
fraction: Float64 = add<Float64>(1.5, 2.5) // 4.0
bad: Bool = add<Bool>(true, false)  // specialization error
```

Generic inference does not widen arguments to make type parameters agree.
Widening is considered only after inference selects a concrete specialization,
as required by RFC 0016. No unresolved arithmetic operation may reach C
generation.

## C23 lowering

Generated C must preserve the checked Seawitch result without relying on C
signed overflow, implementation-defined narrowing, or invalid division.

- Unsigned `+`, `-`, and `*` use RFC 0009's promotion-safe unsigned
  intermediates and explicitly reduce the result back to the selected width at
  every arithmetic node. Generated C must not assume that `uint8_t` or
  `uint16_t` arithmetic stays narrow, because C promotes those operands to
  `int`.
- Signed `+`, `-`, `*`, and unary `-` use promotion-safe unsigned arithmetic
  and explicit reconstruction of the signed value, following RFC 0009.
- Mixed operand types are explicitly converted to the RFC 0016 common type;
  generated C's usual arithmetic conversions never choose the result type.
- Division and remainder evaluate operands once, test the divisor for zero,
  and test the signed-minimum/`-1` pair before any C operation.
- The signed-minimum/`-1` division helper returns the signed minimum; the
  corresponding remainder helper returns zero.
- All other division and remainder operations use C23's truncation-toward-zero
  semantics after the guards establish that the operation is valid.
- Generated expressions remain parenthesized according to Seawitch precedence
  and retain source `#line` mappings.

The generator must explicitly handle every type-checked arithmetic operation. An
unsupported operator, unresolved common type, or malformed arithmetic node
reaching generation is an `Unknown Error`, never an omitted expression,
placeholder, or raw C fallback.

## Diagnostics

Representative diagnostics are:

```text
[Type Error] integer division has a zero divisor
[Type Error] integer remainder has a zero divisor
[Type Error] integer operands have no unique lossless common type
[Type Error] unary negation requires a signed integer
```

The checker owns operator eligibility, common-type selection, constant folding,
and statically provable zero-divisor diagnostics. Runtime zero divisors use the
defined trap and are not checker diagnostics.

## Compatibility and supersession

RFC 0009 remains the historical specification of operator syntax, precedence,
eligibility, and ordinary wrapping arithmetic. RFC 0016 supersedes its
identical-operand rule and supplies mixed numeric common-type selection.

This RFC supersedes RFC 0009's compile-time rejection and runtime undefined-
behavior allowance for signed-minimum division and remainder. At both compile
time and runtime, signed-minimum division by `-1` now wraps to the minimum and
the corresponding remainder is zero. It also replaces RFC 0009's runtime
zero-divisor gap with a defined trap before C evaluates the operation.

Earlier drafts of RFC 0017 proposed `checked (...)` and `saturating (...)`
expression modes. Those modes are rejected by this revision and have no source
or runtime meaning.

## Deferred

- Recoverable zero-divisor handling.
- Bitwise operators and shifts.
- Floating remainder, exponentiation, and mathematical functions.
- Pointer arithmetic.
- Compound assignment and increment operators.
- Operator overloading.

Checked, saturating, and selectable wrapping modes are rejected rather than
deferred. A future proposal must establish a new use case and justify expanding
the arithmetic surface before reconsidering them.

## Acceptance criteria

Implementation is complete when focused end-to-end tests prove that:

1. this RFC adds no token, reserved word, grammar production, arithmetic mode,
   or alternative operator spelling;
2. integer `+`, `-`, `*`, and signed unary `-` wrap every overflow to the
   selected result type;
3. wrapping occurs independently at every arithmetic node;
4. binary integer arithmetic uses RFC 0016's unique least lossless common type
   before applying its operation;
5. mixed integer and floating arithmetic uses RFC 0016's selected floating
   common type and retains RFC 0009's IEC arithmetic, including floating
   division by zero;
6. an expected destination may widen a completed result but does not change an
   operation's selected common type;
7. unsigned unary negation and every other RFC 0009-ineligible operand remain
   rejected, while floating unary negation remains valid;
8. integer division truncates toward zero and remainder follows the dividend's
   sign;
9. statically known evaluated zero divisors are diagnosed and runtime zero
   divisors trap before C division or remainder;
10. signed minimum divided by `-1` folds or executes as the signed minimum
    without invalid C division;
11. signed minimum remainder by `-1` folds or executes as zero without invalid C
    remainder;
12. constant folding produces the same wrapping, division, and remainder
    values as runtime execution and respects short-circuit reachability;
13. every operand is evaluated exactly once and trap checks do not duplicate
    side effects;
14. generic arithmetic is rechecked after concrete specialization, integer and
    floating specializations select their corresponding arithmetic model, and
    no widening participates in generic inference;
15. narrow unsigned operations use explicit promotion-safe intermediates and
    reduce to their selected width at every arithmetic node;
16. generated C contains no signed-overflow, implementation-defined narrowing,
    zero-divisor, or signed-minimum/`-1` undefined-behavior path; and
17. every arithmetic node, conversion, guard, special case, and generator path
    is handled explicitly under the fail-closed architecture.
