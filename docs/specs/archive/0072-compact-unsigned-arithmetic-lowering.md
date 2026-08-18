# RFC 0072: Compact Unsigned Arithmetic Lowering

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-18. `renderUnsignedArithmetic` is
  deleted and `compiler/generator/render.go` renders each maximal ring tree
  through `renderUnsignedRingTree`: one `uintmax_t` seed at the tree's leftmost
  boundary, one narrowing cast at its root, and no helper or new artifact. The
  packet-header example lowers exactly as the Required example states.
  Redundant-parenthesis removal is folded into operand construction rather than
  run as a separate textual phase — see the note below — and
  `ringKeepEveryGrouping` lets a test render the maximally parenthesized form
  and assert the two differ only in punctuation. Coverage lives in
  `compiler/generator/unsigned_ring_test.go`, the ring tests in
  `compiler/generator/generator_test.go`, and
  `compiler/tests/integration/unsigned_ring_test.go`. `docs/reference.md` was
  reviewed and requires no edit: `:572` states the modulo-width operator
  contract and prescribes no cast structure, exactly as this RFC predicted.
  D33's `<stdint.h>` registration landed with RFC 0073, so this RFC inherited a
  working mechanism and added only the Size-only assertion.

  **Deviation, recorded.** The RFC requires the removal to be a separate
  precedence-aware pass over rendered text. It is instead a decision made per
  operand as the operand is built, from two locally decidable facts: the ring
  precedence of a ring child, and the existing `renderExpressionNode` atomicity
  flag for a boundary child. Both only ever drop a pair whose removal leaves an
  identical C parse, so the RFC's actual guarantee — a bug produces noisier C,
  never wrong C — holds. A textual pass would have required a C expression
  parser to decide the same thing. The visible cost is that a boundary rendering
  to a helper call keeps one redundant pair, as in
  `(uintmax_t)hex_v_a * (hex_div_uint32_t(hex_v_b, hex_v_c))`.
- Created: 2026-08-16
- Updated: 2026-08-18
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
    "(" wide(subtree) ")"

wide-left(boundary):
    "(uintmax_t)(" render boundary as T ")"

wide-right(ring subtree):
    "(" wide(subtree) ")"

wide-right(boundary):
    "(" render boundary as T ")"
```

This emits one promotion-seeding cast for a left-associated chain. A nested
right-hand ring subtree receives its own seed because it evaluates before the
parent operation converts its result. A parenthesized subexpression already has
type `uintmax_t`, so the seed still propagates outward through the parentheses.

**Every operand is parenthesized, and that is a correctness rule rather than
formatting.** Omitting them violates this spec's own "preserve AST grouping"
requirement below, in four distinct ways — one for each production above:

```c
/* wide-right(ring subtree), a * (b - c) */
(uintmax_t)a * (uintmax_t)b - c     /* parses as (a*b)-c */

/* wide-left(ring subtree), (a + b) * c */
(uintmax_t)a + b * c                /* parses as a+(b*c) */

/* wide-right(boundary), a * (b / c) */
(uintmax_t)a * b / c                /* parses as (a*b)/c */

/* wide-right(boundary), a + (b << c) */
(uintmax_t)a + b << c               /* parses as (a+b)<<c */
```

The last two matter because a boundary operand is frequently composite: a
division, remainder, shift, bitwise expression, or comparison is a boundary by
definition, and each has a precedence that can lose against its ring parent.

Parenthesize unconditionally rather than consulting precedence to decide.
Correctness then never depends on a precedence table being right — the only way
to be wrong is to emit too few parentheses, and the construction emits the
maximum. The required test "mixed `+`, `-`, and `*` trees preserve their AST
grouping" tests this construction, not the formatter.

### The prettifier is required, not optional

Uniform parenthesization nests one pair per level, so a left-associated chain
constructs as `(((uintmax_t)(v)) + (low)) + (high)` and does not match the
Required example below. The example is therefore the **post-prettification**
output, and a precedence-aware pass that removes redundant parentheses is a
required component of this RFC rather than a nicety.

That pass cannot introduce a defect: it only removes a pair whose removal
leaves an identical C parse tree, which is a local decidable check. A bug in it
produces noisier C, never wrong C. This is the whole reason correctness lives in
the construction and readability lives in the pass.

## Required example

The motivating Hexal expression lowers, after the redundant-parenthesis pass, to
the equivalent of:

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

Preserve AST grouping. The construction emits a pair around every operand; the
required prettification pass omits only parentheses whose removal leaves the
identical C parse tree. Never reassociate subtraction or multiplication/addition
mixtures merely to reduce punctuation.

## Type coverage

Apply the lowering to:

- UInt8 and Byte;
- UInt16;
- UInt32;
- UInt64; and
- Size.

Rune remains excluded from arithmetic. Bool and every non-integer type remain
ineligible. Signed integers continue through their existing wrapping helpers.

`uintmax_t` comes from `<stdint.h>`. Existing requirement discovery **does not**
select it for a Size-only program: discovery is type-driven and Size selects
`<stddef.h>` alone, so nothing registers the header for a type no source type
spells. This is RFC 0073 D33, a live defect on current `main` — today's per-node
lowering already emits `uint64_t` under the same conditions. This RFC does not
introduce it and must not inherit it.

Whichever spec lands first owns the fix: registering `<stdint.h>` when an
unsigned arithmetic intermediate is *rendered*, not when a type is spelled. If
0073 lands first, this RFC inherits a working mechanism and only needs the test
below.

## Generator design

- Replace per-node `renderUnsignedArithmetic` narrowing with a renderer for a
  maximal unsigned ring tree.
- Enter tree rendering only after existing node/type validation succeeds.
- Carry three pieces of explicit renderer state: T, whether rendering is
  currently *inside* a ring tree, and whether the current position requires a
  `uintmax_t` seed. Descending into a boundary clears the in-tree flag, so a
  ring subtree under a boundary starts its own maximal tree with its own seed —
  in `a + ((x + y) / z)` the division terminates the outer tree and `x + y` is a
  separate tree, not a continuation. Clearing on boundary descent is also what
  makes the recursion well-founded: boundary rendering re-enters the ordinary
  per-node renderer exactly once.
- Reuse the existing expression renderer for every boundary expression.
- Do not search rendered strings to discover tree membership or required
  headers.
- Do not add a normalization AST, optimizer pass, or generalized expression
  framework for this bounded transformation.
- Continue failing closed on missing children, mismatched operand/result types,
  unsupported operators, or invalid metadata.

- Emit a parenthesis pair around every operand during construction, and remove
  redundant pairs in a separate pass. Never decide during construction whether
  a pair is needed.

The precedence-aware rendering helper is required, not optional. It must not
rewrite checked nodes or mutate shared checker data.

## Generated-support impact

- No new helper declaration or definition is emitted.
- No new artifact is added to `CompilationResult.Files`.
- `hexal/wrap.h` remains signed-only under ADR 0071.
- `<stdint.h>` is the sole standard prerequisite of this lowering, and it is not
  new — the current per-node lowering already depends on it. See D33.
- Scalar-only programs do not select `hexal/wrap.h` merely because they use
  unsigned arithmetic.

## Required tests

### Exact rendering

- The packet-header example contains one outer `uint32_t` narrowing and one
  `uintmax_t` seed, not one widening/narrowing pair per addition.
- Left-associated chains of each covered type receive one seed and one final
  narrowing.
- A right-nested ring subtree receives its own seed.
- Mixed `+`, `-`, and `*` trees preserve their AST grouping. Cover all four
  regrouping shapes named under Required lowering: a ring subtree on the right,
  a ring subtree on the left, a composite boundary of equal precedence
  (`a * (b / c)`), and a composite boundary of lower precedence
  (`a + (b << c)`). Each asserts the parsed grouping, not the punctuation.
- Removing the redundant-parenthesis pass changes only punctuation: the
  construction's output and the prettified output evaluate identically.
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

Size additionally asserts textually that `hexal.h` contains
`#include <stdint.h>`. Size is the only covered type that does not select that
header through its own spelling, and no test invokes a toolchain, so an
undeclared `uintmax_t` is invisible to the suite without this assertion. See
RFC 0073 D33.

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

