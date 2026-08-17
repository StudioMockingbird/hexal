# RFC 0072: Compact Unsigned Arithmetic Lowering

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-16
- Scope: generated C for unsigned `+`, `-`, and `*` expression trees
- Depends on: RFC 0068 direct scalar lowering, RFC 0069 C23-backed compiler
  simplification
- Coordinates with: RFC 0052 target profiles, ADR 0071 generated runtime
  components, `docs/reference.md`, workbench snippets, generated-C manifests

## Summary

Render a maximal same-type unsigned `+`, `-`, and `*` expression tree in one
promotion-safe `uintmax_t` evaluation domain and narrow once at the tree's
result boundary.

Remove the current widening and narrowing around every binary AST node. Do not
add unsigned arithmetic helper functions.

This changes generated C only. Hexal types, evaluation rules, constant
folding, diagnostics, and modulo-width arithmetic semantics remain unchanged.

## Motivation

The current renderer independently protects every unsigned binary node from C
integer promotions. A left-associated expression therefore nests the complete
cast sequence once per operator.

For:

```hexal
total: UInt32 = version.to<UInt32>() + low.to<UInt32>() + high.to<UInt32>() + mode.to<UInt32>() + port.to<UInt32>()
```

the current output is equivalent to:

```c
const uint32_t total =
    (uint32_t)((uint64_t)(uint32_t)(
        (uint64_t)(uint32_t)(
            (uint64_t)(uint32_t)(
                (uint64_t)(uint32_t)version +
                (uint64_t)(uint32_t)low) +
            (uint64_t)(uint32_t)high) +
        (uint64_t)(uint32_t)mode) +
    (uint64_t)(uint32_t)port);
```

The output is correct but obscures the source operation, repeats casts, and
makes ordinary generated C difficult to inspect.

## Goals

- Preserve exact modulo-width Hexal semantics.
- Preserve every operand's single evaluation and the existing C-unspecified
  operand order.
- Emit compact C that resembles the Hexal expression.
- Remain portable across supported exact-width C integer representations.
- Use standard C integer facilities rather than generated helpers.
- Keep lowering uniform across UInt8, UInt16, UInt32, UInt64, Byte, and Size.

## Non-goals

- No arithmetic semantic change.
- No signed-arithmetic change.
- No change to division, remainder, shifts, bitwise operations, comparisons,
  or unary operators.
- No checked, saturating, or trapping arithmetic mode.
- No range analysis used to erase explicit conversions.
- No reassociation, reordering, distribution, constant combination, or other
  algebraic optimization.
- No dependence on a platform-specific assumption that `uint32_t` is
  `unsigned int` or that `int` is 32 bits.
- No unsigned `ckd_*` wrapper.

## Semantic basis

For an unsigned Hexal type T of width N:

- Hexal `+`, `-`, and `*` produce the mathematical result modulo `2^N`.
- `uintmax_t` can represent every supported unsigned integer type and performs
  unsigned arithmetic modulo `2^M` for some M greater than or equal to N.
- Reduction modulo `2^N` after arithmetic modulo `2^M` produces the same result
  as reducing to `2^N` after every `+`, `-`, or `*` node.

Therefore a same-type tree formed only from those three operations may execute
in `uintmax_t` and narrow once to T at its boundary.

This equivalence does not extend through division, remainder, comparison,
conversion, or shifts. Those operations remain boundaries.

## Terminology

### Ring operation

One binary `+`, `-`, or `*` whose operand and result type are the same unsigned
Hexal type.

### Ring tree

A connected expression tree containing only ring operations of one type T.

### Maximal ring tree

A ring tree whose parent is absent or is not a ring operation of the same type.

### Boundary expression

Any child of a ring operation that is not itself part of the same ring tree,
including a binding read, literal, call, conversion, division, remainder,
shift, bitwise expression, comparison, or differently typed expression.

## Required lowering

For each maximal ring tree of type T:

1. Preserve the checked AST structure and operator spellings.
2. Evaluate every binary operation in `uintmax_t`.
3. Narrow the complete tree exactly once with `(T)`.
4. Preserve every semantic conversion at a boundary.
5. Evaluate each source operand exactly once.

Conceptually:

```text
lower(tree: unsigned ring tree<T>) = (T)(wide(tree))
```

`wide` renders each binary subtree so its left operand is already
`uintmax_t`. C's usual arithmetic conversions then convert that node's right
operand to `uintmax_t` before the operation:

```text
wide(left op right):
    wide-left(left) op wide-right(right)

wide-left(ring subtree):
    wide(subtree)

wide-left(boundary):
    (uintmax_t)(render boundary as T)

wide-right(ring subtree):
    wide(subtree)

wide-right(boundary):
    render boundary as T
```

This emits one promotion-seeding cast for a left-associated chain. A nested
right-hand ring subtree receives its own seed because it evaluates before the
parent operation converts its result.

## Required example

The motivating Hexal expression lowers to the equivalent of:

```c
const uint32_t hex_v_total = (uint32_t)(
    (uintmax_t)(uint32_t)hex_v_version +
    (uint32_t)hex_v_low +
    (uint32_t)hex_v_high +
    (uint32_t)hex_v_mode +
    (uint32_t)hex_v_port
);
```

The `(uint32_t)` operand casts preserve the explicit Hexal conversions. The
first `uintmax_t` cast prevents integer promotion from selecting a signed
intermediate. The final cast establishes UInt32's modulo-width result.

Do not emit the simpler form below as the portable baseline:

```c
const uint32_t hex_v_total =
    (uint32_t)hex_v_version +
    (uint32_t)hex_v_low +
    (uint32_t)hex_v_high +
    (uint32_t)hex_v_mode +
    (uint32_t)hex_v_port;
```

On an implementation where `uint32_t` has lower rank than `int`, those operands
may promote to signed `int`. RFC 0052 may later authorize the simpler form for
a target profile that proves the required rank/range relationship.

## Expression boundaries

### Wider-to-narrower conversion

An explicit conversion to T occurs before the ring operation and must retain
its narrowing cast:

```text
UInt64 value -> value.to<UInt32>() + other
```

```c
(uint32_t)((uintmax_t)(uint32_t)value + other)
```

Do not lift `value` to `uintmax_t` before converting it to UInt32.

### Division and remainder

Division and remainder observe the already-wrapped value of their operands.
They terminate a ring tree:

```text
(a - b) / divisor
```

The subtraction narrows to T before division. Conversely, in:

```text
a + (b / divisor)
```

the division produces T before its result enters the outer ring tree.

### Comparisons

A comparison consumes completed T values. A ring tree used as a comparison
operand narrows before the comparison.

### Calls and side effects

Calls are boundary expressions. Rendering must neither duplicate nor reorder
them. The generated C retains Hexal's existing C23-unspecified operand order;
this RFC does not impose left-to-right evaluation.

### Parentheses

Preserve AST grouping. A precedence-aware formatter may omit only parentheses
whose removal leaves the identical C parse tree. Never reassociate subtraction
or multiplication/addition mixtures merely to reduce punctuation.

## Type coverage

Apply the lowering to:

- UInt8 and Byte;
- UInt16;
- UInt32;
- UInt64; and
- Size.

Rune remains excluded from arithmetic. Bool and every non-integer type remain
ineligible. Signed integers continue through their existing wrapping helpers.

`uintmax_t` comes from `<stdint.h>`. Existing requirement discovery must select
that header whenever an affected unsigned runtime expression is emitted.

## Generator design

- Replace per-node `renderUnsignedArithmetic` narrowing with a renderer for a
  maximal unsigned ring tree.
- Enter tree rendering only after existing node/type validation succeeds.
- Carry T and whether the current position requires a `uintmax_t` seed as
  explicit renderer state.
- Reuse the existing expression renderer for every boundary expression.
- Do not search rendered strings to discover tree membership or required
  headers.
- Do not add a normalization AST, optimizer pass, or generalized expression
  framework for this bounded transformation.
- Continue failing closed on missing children, mismatched operand/result types,
  unsupported operators, or invalid metadata.

The implementation may use a small precedence-aware rendering helper. It must
not rewrite checked nodes or mutate shared checker data.

## Generated-support impact

- No new helper declaration or definition is emitted.
- No new artifact is added to `CompilationResult.Files`.
- `hexal/wrap.h` remains signed-only under ADR 0071.
- `<stdint.h>` remains the sole standard prerequisite introduced by this
  lowering.
- Scalar-only programs do not select `hexal/wrap.h` merely because they use
  unsigned arithmetic.

## Required tests

### Exact rendering

- The packet-header example contains one outer `uint32_t` narrowing and one
  `uintmax_t` seed, not one widening/narrowing pair per addition.
- Left-associated chains of each covered type receive one seed and one final
  narrowing.
- A right-nested ring subtree receives its own seed.
- Mixed `+`, `-`, and `*` trees preserve their AST grouping.
- Explicit wider-to-narrower conversions remain inside the lifted tree.
- Calls appear exactly once.

### Semantic boundaries

- Ring trees narrow before division, remainder, comparison, conversion, shift,
  and bitwise consumers.
- Division or remainder used as a ring operand completes before lifting.
- Signed arithmetic output is byte-for-byte unchanged.
- Constant-folded expressions retain the existing folded values and emit no
  runtime tree.

### Width boundaries

For UInt8, UInt16, UInt32, UInt64, and Size, cover:

- maximum plus one;
- zero minus one;
- overflowing multiplication;
- a mixed tree whose intermediate value wraps more than once; and
- a nested right-hand subtree.

Expected results must match reduction modulo the type width.

### Regressions

- Existing integration tests pass with expected generated-C assertions updated
  to the compact form.
- Workbench expected output and affected snippet snapshots are regenerated.
- No test weakens or removes a semantic assertion merely because formatting
  changes.
- `go test ./...`
- `go vet ./...`
- `go vet -tags c23 ./compiler/tests/c23validation`
- Ordinary tests invoke no external compiler.

## Reference synchronization

No `docs/reference.md` edit is expected: its modulo-width operator contract
already states the unchanged semantics and does not prescribe the current
per-node cast structure.

Implementation must still review the reference before closure and record that
verification. If an exact generated-C lowering rule is judged normative, stop
and request explicit user approval before editing `docs/reference.md`.

## Acceptance criteria

- Every runtime unsigned `+`, `-`, and `*` ring tree preserves modulo-width
  Hexal results.
- No affected tree repeats the current widening/narrowing sequence at every
  binary node.
- A left-associated chain uses one `uintmax_t` seed and one final T narrowing.
- Division, remainder, comparison, conversion, shift, and bitwise boundaries
  retain their required T value.
- Operand expressions are neither duplicated nor reordered.
- No unsigned wrapping helper or new generated artifact exists.
- Generated C is deterministic and visibly corresponds to the source tree.
- Tests, workbench snapshots, and the reviewed reference agree before closure.

## Consequences

Positive:

- Common unsigned expressions become substantially shorter and readable.
- The compiler emits fewer redundant casts and parentheses.
- Exact modulo behavior remains independent of C's narrow-integer promotions.
- No runtime helper or call overhead is introduced.

Costs:

- Unsigned expression rendering becomes context-aware across a bounded tree
  instead of rendering every binary node independently.
- Expressions containing division, remainder, conversions, or differently
  typed children require explicit boundaries.
- Generated-C snapshot expectations change.

