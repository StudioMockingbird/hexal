# RFC 0009: Core Operators (Interim)

- Status: Implemented; interim scope, intended to make the language executable
- Features: unary `-` and `!`, binary arithmetic, comparison, short-circuit
  logical operators, parenthesized expressions
- Created: 2026-08-08
- Depends on: RFC 0003 (core scalar types), RFC 0007 (mutability redesign)
- Interim: overflow is defined as wrapping and division has two accepted runtime
  cases of undefined behavior. A later arithmetic RFC owns checked, saturating,
  and trapping forms.

## Summary

Seawitch currently has no way to compute anything. This RFC adds the smallest
operator set that makes programs meaningful.

```seawitch
mut total: Int32 = 0
total = total + 1

ready: Bool = total > 0 and total < 100
```

Added:

| Category | Operators |
|---|---|
| Unary | `-` (numeric negation), `!` (boolean) |
| Arithmetic | `+` `-` `*` `/` `%` |
| Comparison | `==` `!=` `<` `<=` `>` `>=` |
| Logical | `and` `or` (short-circuit) |
| Grouping | `( … )` |

Both operands of a binary operator must have the identical type, per RFC 0003.
Arithmetic yields that type; comparison and logical operators yield `Bool`.

Deliberately excluded: bitwise operators, shifts, pointer arithmetic, pointer
and object comparison, compound assignment, increment, and a conditional
operator.

## Motivation

Eight RFCs have specified how values are declared, named, laid out, and
addressed. None let a program produce a result. RFC 0003 anticipated this and
fixed two constraints an operator RFC must satisfy:

> An operator RFC must define result types and overflow behavior before code
> generation emits arithmetic. Generated C must not expose Seawitch signed
> operations to C signed-overflow undefined behavior.

This RFC satisfies both for the operators it adds, and states plainly the one
case it cannot.

## Changes to implemented behavior

### The restricted negation production is replaced

RFC 0003 added a narrow production so that signed minima stayed writable before
operators existed:

```ebnf
negated-numeric-literal = "-" , ( integer-literal | decimal-floating-literal ) ;
```

This RFC replaces that alternative with general unary `-`, so `-` now applies
to any operand:

```seawitch
mut delta: Int32 = 5
back: Int32 = -delta      // previously a syntax error
```

**RFC 0003's constant rule must survive the generalisation.** When `-` is
applied directly to an untyped literal, the checker still negates the exact
mathematical value *before* range checking it against the destination:

```seawitch
minimum: Int8 = -128      // still valid: -128 is checked, never 128
```

Checking the positive operand first would make every signed minimum
unwritable, including `Int64`'s, which has no positive counterpart. The
implementation keeps that folding path and reaches the general operator only
when the operand is not an untyped literal.

RFC 0003's rejection of a negated literal against an unsigned destination is
subsumed by typing rule 9, which accepts signed operands only.

### `and` and `or` become reserved words

Both stop being usable as identifiers. `!` is punctuation and reserves nothing.

### Existing tests

Parser tests asserting that `-` is valid only before a literal, and any test
using `and` or `or` as an identifier, change with this RFC.

## Guide-level explanation

### Arithmetic

Both operands must already have the same type. There are no implicit
conversions and no promotions:

```seawitch
a: Int32 = 10
b: Int32 = 3

sum: Int32 = a + b        // 13
difference: Int32 = a - b // 7
product: Int32 = a * b    // 30
quotient: Int32 = a / b   // 3, truncated toward zero
remainder: Int32 = a % b  // 1, sign follows the dividend
```

Mixing widths or signedness is an error, not a promotion:

```seawitch
small: Int16 = 1
large: Int32 = 2
total: Int32 = small + large   // Error: Int16 and Int32 do not mix
```

`/` and `%` apply to integers and `/` also to floats. `%` is integer-only;
floating remainder is deferred.

### Untyped literals in operators

Untyped literals take a type from their context; typed values never convert.
Two rules cover every case.

**A literal beside a typed operand takes that operand's type**, extending RFC
0003's contextual rule:

```seawitch
count: UInt8 = 200
next: UInt8 = count + 1      // 1 is UInt8
over: UInt8 = count + 100    // Error: 300 is outside the UInt8 range
```

**An expected type reaches untyped literals through operators.** When both
operands are untyped, the surrounding expected type selects their type before
range checking:

```seawitch
x: Int64 = 2 + 2                 // both literals are Int64
total: Int64 = 5_000_000_000 + 1 // both are Int64; no Int32 narrowing
ratio: Float32 = 1.5 * 2.0       // both are Float32
```

An expected type is *usable* only when it is admissible in the operand position
it would reach. A comparison or logical context therefore supplies nothing to
its operands — `Bool` is not an arithmetic operand type — so the fallbacks
govern instead:

```seawitch
big: Bool = 5_000_000_000 > 1   // Error: 5_000_000_000 is outside Int32
```

That follows RFC 0003, which deliberately refuses to pick a type from a
literal's magnitude. The value needs a typed context:

```seawitch
threshold: Int64 = 5_000_000_000
big: Bool = threshold > 1       // 1 takes Int64 from threshold
```

Without a usable expected type, RFC 0003's fallbacks apply — `Int32` for
integers, `Float64` for decimals.

**A typed operand is never converted by context.** The expected type reaches
literals only, so an operation between typed values keeps its own type and
mismatches are reported rather than widened:

```seawitch
a: Int32 = 2000000000
doubled: Int64 = a + a    // Error: Int32 + Int32 is Int32, not Int64
```

That rejection is the point. In C the same code adds in `int32_t`, overflows,
and only then widens — the bug family behind `int64_t ns = seconds * 1000000000`.
Seawitch requires the operands to already be the width you meant.

### Negation

Unary `-` negates a signed numeric value:

```seawitch
mut delta: Int32 = 5
back: Int32 = -delta
```

It requires a signed operand. Negating an unsigned value is an error rather
than a wrap:

```seawitch
count: UInt32 = 5
bad: UInt32 = -count         // Error: negation requires a signed type
```

RFC 0003's negated numeric literal is subsumed by this operator. `-128` in an
`Int8` context still checks as one exact constant, so every signed minimum
remains writable.

### Comparison

Comparison requires identical operand types and produces `Bool`:

```seawitch
a: Int32 = 1
b: Int32 = 2

lower: Bool = a < b
same: Bool = a == b
```

`==` and `!=` apply to every scalar including `Bool`. Ordering applies to
numeric scalars only:

```seawitch
flag: Bool = true
other: Bool = false
mixed: Bool = flag < other   // Error: Bool is not ordered
```

Object and pointer comparison are deferred. Floating comparison follows IEC
60559: any comparison with NaN except `!=` is false, and `!=` with NaN is true.

### Logical operators

`and` and `or` take `Bool` operands, produce `Bool`, and short-circuit. `!`
inverts a `Bool`.

```seawitch
ready: Bool = true
loaded: Bool = false

both: Bool = ready and loaded
either: Bool = ready or loaded
neither: Bool = !ready and !loaded
```

Short-circuit reachability affects constant folding, not type checking. Both
operands are always type-checked. When a known constant left operand determines
the result, the checker does not fold the unreachable right operand or emit
static diagnostics from its unevaluated operations:

```seawitch
always: Bool = true or (1 / 0 == 0)       // valid; RHS is unreachable
never: Bool = false and (1 / 0 == 0)      // valid; RHS is unreachable
bad: Bool = true or (1 and 2)             // Error: and requires Bool operands
```

An unknown or mutable guard does not establish reachability, so its right
operand is checked and statically known divisor errors remain errors:

```seawitch
mut guard: Bool = true
badGuard: Bool = guard or (1 / 0 == 0)    // Error: division by zero
```

There is no truthiness. A numeric value is not a condition:

```seawitch
count: Int32 = 1
bad: Bool = count and ready   // Error: and requires Bool operands
```

The binary operators are keywords and the unary operator is punctuation,
deliberately. Infix words separate their operands and read as prose; a prefix
symbol binds visually to the operand it negates:

```seawitch
alive: Bool = !dead and health > 0
```

`&&` and `||` are not operators. `~`, not `!`, is reserved for the deferred
bitwise complement.

`!` binds tighter than comparison, following Lua:

```seawitch
ok: Bool = !ready == done     // parses as (!ready) == done
```

Python binds its `not` below comparison instead. Seawitch follows Lua because
`and` and `or` already do, and because prefix punctuation reads as tightly
bound. Parenthesize when the other reading is meant.

### Precedence and grouping

Highest to lowest:

| Level | Operators | Associativity |
|---|---|---|
| 1 | `.` member and `.value` | left |
| 2 | unary `-`, `!`, `ref` | right |
| 3 | `*` `/` `%` | left |
| 4 | `+` `-` | left |
| 5 | `<` `<=` `>` `>=` | left |
| 6 | `==` `!=` | left |
| 7 | `and` | left |
| 8 | `or` | left |

```seawitch
value: Int32 = 2 + 3 * 4              // 14
grouped: Int32 = (2 + 3) * 4          // 20
check: Bool = a + 1 > b and !done
```

Comparison is non-associative in practice because its operands are numeric and
its result is `Bool`; `a < b < c` fails on the second comparison rather than
chaining.

## Reference-level explanation

### Typing rules

1. Every binary operator requires operands of one identical canonical type
   after RFC 0005 alias resolution.
2. An untyped literal operand takes the other operand's type when that type is
   admissible for the operator, and is range-checked against it under RFC 0003.
3. When every operand of an operation is untyped, a usable expected type from
   the surrounding context selects their type, transitively through nested
   operators. Range checking happens against the selected type. An expected
   type is usable only when it is admissible in the operand position it would
   reach, so comparison and logical contexts supply none.
4. Without a usable expected type, RFC 0003's `Int32` and `Float64` fallbacks
   select it.
5. An expected type reaches untyped literals only. A typed operand is never
   converted, so an operation between typed values keeps its own type and a
   mismatch with the destination is an error rather than a widening.
6. `+ - * /` accept integer and floating types. `%` accepts integer types only.
7. Ordering comparison accepts integer and floating types. Equality
   additionally accepts `Bool`.
8. `and`, `or`, and `!` accept `Bool` only and produce `Bool`.
9. Unary `-` accepts signed integer and floating types.
10. Arithmetic produces its operand type. Comparison and logical operators
    produce `Bool`.

### Overflow

Integer arithmetic **wraps** using two's-complement semantics, for every width
and both signednesses. Wrapping is defined behavior and produces the
mathematical result reduced modulo 2ⁿ.

```seawitch
mut edge: Int8 = 127
edge = edge + 1        // -128, defined
```

This is the interim decision. It is deterministic, costs nothing at runtime,
and keeps generated C free of signed-overflow undefined behavior. It is not the
end state: silently wrong arithmetic is a poor default for a language whose
goals include "if it compiles, it runs." A later RFC adds checked, saturating,
and trapping forms and decides which becomes the default.

Floating arithmetic follows IEC 60559 with round-to-nearest, ties-to-even.
Overflow yields an infinity; underflow yields a subnormal or signed zero.
Neither is an error.

### Division: the one accepted gap

Two integer cases have no defined result:

```seawitch
a / 0                  // division by zero
INT32_MIN / -1         // quotient is not representable
```

Both are undefined behavior in C, and Seawitch cannot define them without a
runtime trap, which does not exist yet.

Every statically known zero divisor in an evaluated expression is a compile
error. The checker already holds exact literal values under RFC 0003, so the
test covers constant expressions, not just a written `0`. A zero divisor inside
an unreachable right operand of a constant short-circuit is not evaluated or
diagnosed, but the right operand is still type-checked:

```seawitch
bad: Int32 = total / 0          // Error: division by zero
folded: Int32 = total % (2 - 2) // Error: division by zero
safe: Bool = true or (1 / 0 == 0) // valid; the RHS is unreachable
```

A divisor the checker cannot evaluate is accepted, and dividing by zero at
runtime is undefined:

```seawitch
risky: Int32 = total / n        // accepted; n must be non-zero at runtime
```

Rejecting this case as well would require proving a divisor non-zero, which
needs flow analysis the compiler does not have and the language does not
otherwise need. The signed-minimum quotient is undefined for the same reason:
its operands are usually not constants.

So the accepted gap is exactly:

1. a runtime zero divisor; and
2. `INT*_MIN / -1` and `INT*_MIN % -1` with non-constant operands.

This is the only place in the language where a compiling program can invoke
undefined behavior without using a raw pointer. It is recorded here rather than
buried, and it is the strongest argument for prioritising the arithmetic RFC,
whose trapping division closes both cases.

### Grammar

The expression grammar gains precedence levels and grouping:

```ebnf
expression          = or-expression ;
or-expression       = and-expression , { "or" , and-expression } ;
and-expression      = equality-expression
                    , { "and" , equality-expression } ;
equality-expression = relational-expression
                    , { ( "==" | "!=" ) , relational-expression } ;
relational-expression
                    = additive-expression
                    , { ( "<" | "<=" | ">" | ">=" ) , additive-expression } ;
additive-expression = multiplicative-expression
                    , { ( "+" | "-" ) , multiplicative-expression } ;
multiplicative-expression
                    = unary-expression
                    , { ( "*" | "/" | "%" ) , unary-expression } ;
unary-expression    = ( "-" | "!" ) , unary-expression
                    | reference-expression
                    | postfix-expression ;
reference-expression
                    = "ref" , place-expression ;
place-expression    = identifier , { "." , identifier } ;
postfix-expression  = primary-expression , { "." , identifier } ;
primary-expression  = identifier
                    | object-literal
                    | integer-literal
                    | decimal-floating-literal
                    | "true" | "false"
                    | "(" , expression , ")" ;
```

`and` and `or` become reserved words. The lexer gains `!`, `!=`, `==`, `<`,
`<=`, `>`, `>=`, `+`, `*`, `/`, `%`, `(`, and `)`. `-` and `.` already exist,
and `<` and `>` are already produced for `Ptr<…>` type expressions.

`!` and `!=` require maximal munch: on `!`, the lexer consumes a following `=`
before emitting a token. The same applies to `<=`, `>=`, and `==`.

`-` is now both prefix and infix. They are distinguished by grammar position,
never by spacing, as RFC 0003 already specified: prefix `-` appears where an
operand begins, infix `-` follows a completed operand.

The statement-level invariant RFC 0003 recorded still holds, and parentheses
extend it. Every statement begins with an identifier, `mut`, or `type`, so no
statement can begin with `-` or `(`, and the terminator-free grammar needs no
newline-sensitivity rule:

```seawitch
x: Int32 = a
(b)          // not a statement today, so `a` and `(b)` cannot merge
```

Any future construct that lets a statement begin with `-` or `(` — expression
statements, or a call syntax reached from statement position — must revisit
this before adoption. A call would make `a` newline `(b)` ambiguous in exactly
the way JavaScript's automatic semicolon insertion is.

### C23 lowering

Comparison, logical, and floating operators lower directly, because both
operands already share one type and no promotion can change a result:

```seawitch
lower: Bool = a < b
both: Bool = ready and loaded
scaled: Float32 = ratio * 2.0
```

```c
const bool sw_v_lower = sw_v_a < sw_v_b;
const bool sw_v_both = sw_v_ready && sw_v_loaded;
const float sw_v_scaled = sw_v_ratio * 0x1p+1f;
```

Signed integer arithmetic must not reach C's signed operators. Every signed
`+`, `-`, `*`, and unary `-` computes in promotion-safe `uint64_t`, reduces the
result through the target-width unsigned type `U`, and then uses a
representability guard before converting to the signed type `S`:

```
u = (U)((uint64_t)left OP (uint64_t)right)
u <= (uint64_t)S_MAX
    ? (S)u
    : S_MIN + (S)((uint64_t)u - (uint64_t)S_MAX - (uint64_t)1)
```

`U` is the unsigned type of the same width as `S`. Both `(S)` conversions are
therefore applied only to representable values; the target-width cast remains
the modular reduction step:

```seawitch
sum: Int32 = a + b
```

```c
const int32_t sw_v_sum =
    ((uint64_t)(uint32_t)((uint64_t)sw_v_a + (uint64_t)sw_v_b) <= (uint64_t)INT32_MAX
        ? (int32_t)(uint32_t)((uint64_t)sw_v_a + (uint64_t)sw_v_b)
        : INT32_MIN + (int32_t)((uint64_t)(uint32_t)((uint64_t)sw_v_a + (uint64_t)sw_v_b) - (uint64_t)INT32_MAX - (uint64_t)1));
```

For unary `-`, the same rule uses
`u = (U)((uint64_t)0 - (uint64_t)value)` before the guarded conversion. The
`uint64_t` intermediate prevents C integer promotions from selecting a signed
arithmetic type. Casting through `U` applies the target-width reduction, and
the guard prevents an out-of-range unsigned-to-signed conversion, so the
generated expression preserves Seawitch's target-width wrapping semantics
without evaluating signed arithmetic in C.

Unsigned integer arithmetic uses a promotion-safe unsigned intermediate and
casts the result back to the target width. `UInt8` and `UInt16` use
`uint32_t`; `UInt32` and `UInt64` use `uint64_t` where required:

```seawitch
narrow: UInt8 = first + second
wide: UInt32 = firstWide + secondWide
```
```c
const uint8_t sw_v_narrow = (uint8_t)((uint32_t)sw_v_first + (uint32_t)sw_v_second);
const uint32_t sw_v_wide = (uint32_t)((uint64_t)sw_v_firstWide + (uint64_t)sw_v_secondWide);
```

This preserves the target-width modular result while avoiding C integer
promotions. Unsigned division and remainder use the same promotion-safe
intermediate.

Signed `/` and `%` lower directly to C's operators, which already truncate
toward zero and take the dividend's sign. They carry the undefined cases
recorded above.

Generated expressions are parenthesized to preserve Seawitch precedence
exactly. The generator must not rely on C's precedence matching Seawitch's.

### Evaluation order

`and` and `or` evaluate their right operand only when the left does not
determine the result, matching C's `&&` and `||`. This is a Seawitch guarantee,
not an inherited one, and it survives every later RFC.

Every other operator evaluates both operands exactly once, in an unspecified
relative order. Within this RFC the order is unobservable, because no
expression can have an effect yet.

RFC 0008 makes it observable and keeps it unspecified, inheriting C23's
sequencing rather than imposing an order. Once calls exist:

```seawitch
r := bump(ref hits) - bump(ref hits)   // -1 or 1; both are correct
```

Code whose result depends on an order writes the operands as separate
statements:

```seawitch
first := bump(ref hits)
second := bump(ref hits)
r := first - second                    // always -1
```

This is unspecified behavior, not undefined. Every permitted order produces a
well-defined result, so RFC 0003's no-undefined-behavior guarantee is intact —
but a program can have more than one correct answer, which was not previously
true of any Seawitch construct.

### Analyzer

No analyzed pass is required. Operator expressions are pure, so the checked
form remains generator-ready under ADR 0001 with one additional structured
node for a binary or unary operation.

### Diagnostics

```text
[Type Error] operator + requires identical operand types; got Int16 and Int32
[Type Error] operator % requires integer operands; got Float64
[Type Error] operator < requires ordered operands; got Bool
[Type Error] operator and requires Bool operands; got Int32
[Type Error] negation requires a signed type; got UInt32
[Type Error] given value is outside the UInt8 range
[Type Error] division by zero
[Syntax Error] expected an expression after operator +
```

The lexer owns the new tokens, the parser owns malformed operator placement,
and the checker owns operand typing, literal range checking, and the literal
zero divisor.

## Deferred

- bitwise `& | ^ ~` and shifts, whose shift-count rules need their own UB
  analysis, and whose right shift inherits a lexical constraint:
  `Ptr<Ptr<Int32>>` currently closes with two `>` tokens, so a `>>` operator
  must either never be lexed as one token or be split in type context, as C++
  had to do;
- checked, saturating, and trapping arithmetic, and the default choice;
- trapping division, which closes this RFC's undefined cases;
- pointer arithmetic and pointer comparison, which need `Nil`;
- object equality, which needs a member-wise or opaque decision;
- floating remainder, `**`, and mathematical functions;
- compound assignment, increment, and a conditional operator;
- operator overloading, which is not planned; and
- explicit numeric conversions, still owned by their own future RFC.

## Alternatives considered

### Guarantee left-to-right evaluation of operands

Rejected, reversing this RFC's own earlier position.

An earlier draft required the functions RFC to fix left-to-right order, on the
principle that Seawitch "must not inherit unspecified behavior accidentally
from C." RFC 0008 inherits it deliberately instead, and the distinction between
those two is the whole argument.

Imposing an order costs a temporary for every effectful operand and a
statement-buffering pass in the generator, so that:

```c
sw_t_Pair sw_v_p = (sw_t_Pair){ .sw_m_first  = sw_f_bump(&sw_v_hits),
                                .sw_m_second = sw_f_bump(&sw_v_hits) };
```

becomes:

```c
int32_t sw_internal_0 = sw_f_bump(&sw_v_hits);
int32_t sw_internal_1 = sw_f_bump(&sw_v_hits);
sw_t_Pair sw_v_p = (sw_t_Pair){ .sw_m_first  = sw_internal_0,
                                .sw_m_second = sw_internal_1 };
```

The generated C stops mirroring its source, every expression becomes a
candidate for lifting, and the language buys a guarantee that ordinary code
never depends on. Sequencing is also invisible in Seawitch source — nothing the
programmer writes selects an order — so there is nothing being silently
inherited. That is what separates it from precedence, which *is* determined by
what the programmer wrote and therefore cannot be inherited at all.

Short-circuit is the exception and stays a Seawitch guarantee, because `and`
and `or` do have source-visible sequencing.

### Trap on overflow instead of wrapping

Deferred rather than rejected. Trapping matches "if it compiles, it runs"
better than wrapping does, but it needs a runtime abort path, a diagnostic
channel, and a decision about what a trap means in a freestanding target. None
exist. Wrapping is definable today and can be superseded without changing any
program that never overflows.

### Leave overflow undefined and emit plain C operators

Rejected. RFC 0003 forbids it explicitly, and it would put undefined behavior
into ordinary arithmetic rather than confining it to raw pointers.

### Allow implicit widening in mixed-width arithmetic

Rejected. RFC 0003 makes typed values never convert implicitly; adding a
promotion ladder for operators would reintroduce C's usual arithmetic
conversions through the back door.

### Use `&&` and `||` for the binary logical operators

Rejected. Seawitch's surface is Lua-like, and infix words separate their
operands more legibly than doubled punctuation. Keeping them as keywords also
leaves `&` and `|` free for the deferred bitwise operators.

Unary `!` is punctuation for the opposite reason: it binds to a single operand
and reads as tightly bound. Taking it costs nothing, because C's bitwise
complement is `~`, not `!`.

### Spell the unary operator `not`

Rejected. It is more consistent with `and` and `or`, but `not ready == done`
reads as though the whole comparison is negated when it is not. `!ready == done`
makes the tight binding visible at the same precedence.

### Let operands always settle their own type, ignoring context

```seawitch
x: Int64 = 2 + 2   // check operands as Int32, then fail on the declaration?
```

Rejected. This is C's rule, and it reads well until the fallback narrows a
value that fits the destination perfectly:

```seawitch
total: Int64 = 5_000_000_000 + 1
// both literals fall back to Int32; 5_000_000_000 is out of range
```

Nothing about that program is wrong, and no conversion is required to accept
it. Flowing the expected type into untyped literals — and only into literals —
fixes it without weakening the rule that matters: typed operands still never
convert, so `a + a` on two `Int32` values is `Int32` regardless of destination.

### Let an expected type convert typed operands

```seawitch
a: Int32 = 2000000000
doubled: Int64 = a + a   // widen the operands to Int64 first?
```

Rejected. That is a promotion ladder by another name, and it reintroduces C's
usual arithmetic conversions. It also hides the decision that matters: if the
sum needs 64 bits, the operands should already be 64 bits, and saying so is the
programmer's call once explicit conversions exist.

## Acceptance criteria

Implementation is complete when end-to-end tests prove that:

1. each listed operator parses at its specified precedence and associativity;
2. parentheses override precedence and generated C preserves Seawitch grouping;
3. binary operators reject non-identical operand types, including mixed widths
   and mixed signedness;
4. an untyped literal beside a typed operand takes that type and is
   range-checked against it;
5. an expected type reaches untyped literals transitively through operators, so
   `total: Int64 = 5_000_000_000 + 1` is accepted, while two untyped literals
   without an expected type use RFC 0003's fallbacks;
6. an expected type never converts a typed operand, so `doubled: Int64 = a + a`
   on two `Int32` values is rejected;
7. `%` rejects floating operands and ordering rejects `Bool`;
8. logical operators reject non-`Bool` operands and short-circuit;
9. unary `-` rejects unsigned operands, applies to typed operands as well as
   literals, and still folds `-<literal>` to one exact constant before range
   checking, so `minimum: Int8 = -128` and the `Int64` minimum stay writable;
10. signed arithmetic wraps at every width with the defined two's-complement
      result, and generated C computes `+`, `-`, `*`, and unary `-` in
      promotion-safe `uint64_t`, casts back through the target-width unsigned
      type, and uses a representability guard for the signed conversion;
11. unsigned arithmetic uses `uint32_t` intermediates for `UInt8` and `UInt16`
     and `uint64_t` intermediates for `UInt32` and `UInt64` where required,
     casting the result back to the target width;
 12. every statically evaluable zero divisor in an evaluated/reachable
     expression is rejected, including a folded constant expression such as
     `total / (2 - 2)`; a zero divisor in the unreachable right-hand side of a
     decisive constant short-circuit is not evaluated or diagnosed, and
     statically evaluable signed-minimum `/ -1` and `% -1` cases are rejected;
13. floating arithmetic and comparison follow IEC 60559, including NaN
    comparison results;
14. generated C is asserted textually for every operator and width. Compiling
    it under the project's C23 settings is **deferred**: spec 0013 removed the
    toolchain dependency from `go test`, so this is verified only by the
    `c23`-tagged suite described there, which does not exist yet; and
15. every new token, syntax node, and checked node is handled explicitly and
    fails closed.

## Implementation handoff

The plan must identify:

1. lexer additions for the new operator tokens and the two keywords, with
   maximal munch so `!=`, `<=`, `>=`, and `==` are never split;
2. a precedence-climbing expression parser replacing the current single-level
   expression production;
3. one structured checked node for unary and binary operations under ADR 0001,
   carrying the resolved operator, operand type, and result type;
4. operand typing, literal contextual typing, and range checking shared with
   RFC 0003's literal path — including the two-pass shape needed to push an
   expected type into an operand tree before either side is range-checked, and
   retention of RFC 0003's `-<literal>` folding so signed minima survive the
   move to a general unary operator;
5. constant folding sufficient to reject every statically evaluable zero
   divisor, reusing the checker's existing `go/constant` values;
6. the unsigned-wrapping lowering helper, applied per width;
7. explicit parenthesization in generated expressions;
8. focused lexer, parser, checker, and generator tests; and
9. end-to-end cases in `compiler/compile_test.go`, including a program that
   exercises every operator and compiles as C23.

Canonical grammar, language, and status documents are updated once behavior
stabilizes.
