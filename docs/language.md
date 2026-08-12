# Seawitch Language Notes

## Core scalar types

Seawitch has eleven fixed-width scalar types. The integer ranges are exact and
target-independent; byte order and ABI placement remain target properties.

| Type | Range or format | C23 |
| --- | --- | --- |
| `Bool` | `false`, `true` | `bool` |
| `UInt8` | 0 .. 255 | `uint8_t` |
| `UInt16` | 0 .. 65,535 | `uint16_t` |
| `UInt32` | 0 .. 4,294,967,295 | `uint32_t` |
| `UInt64` | 0 .. 18,446,744,073,709,551,615 | `uint64_t` |
| `Int8` | -128 .. 127 | `int8_t` |
| `Int16` | -32,768 .. 32,767 | `int16_t` |
| `Int32` | -2,147,483,648 .. 2,147,483,647 | `int32_t` |
| `Int64` | -9,223,372,036,854,775,808 .. 9,223,372,036,854,775,807 | `int64_t` |
| `Float32` | IEC 60559 binary32 | `float` |
| `Float64` | IEC 60559 binary64 | `double` |

`Byte` is a transparent alias of `UInt8` (see the Byte and text conformance
section); it may hold an ordinary integer literal, for example
`letter: Byte = 0x41`. `Int`, `UInt`, `Float`, `Double`, `Char`, and `Long`
are not built-ins.

## Contextual numeric literals

Declarations remain explicitly typed. An integer literal is an exact
mathematical value first. An expected integer destination selects its type;
without one it defaults to `Int32` and must fit. Decimal floating literals use
an expected `Float32` or `Float64`, otherwise they default to `Float64`.

Decimal, hexadecimal, binary, and explicit-octal integers are equivalent by
value. Lowercase `0x`, `0b`, and `0o` prefixes and between-digit underscores
are supported. Decimal values such as `007` are rejected; C-style implicit
octal is not used.

Typed scalar values require identical types. There are no implicit numeric or
boolean conversions. A literal may take the destination type directly, but a
typed `UInt32` cannot initialize an `Int32` without a future explicit
conversion feature.

## Core operators

The core operator set is unary `-` and `!`, arithmetic `+`, `-`, `*`, `/`, and
`%`, comparison `==`, `!=`, `<`, `<=`, `>`, and `>=`, logical `and` and `or`,
and parenthesized grouping. `and` and `or` are reserved words and
short-circuit. `!` requires `Bool`; unary `-` requires a signed integer or a
floating type. Arithmetic returns its operand type. Comparisons and logical
operators return `Bool`.

Short-circuit reachability only controls constant folding. Both operands are
type-checked. If a known constant left operand determines the result, the
unreachable right operand is not folded and static diagnostics from its
unevaluated operations, such as a zero divisor, are not emitted:

```seawitch
always: Bool = true or (1 / 0 == 0)  // valid; RHS is unreachable
never: Bool = false and (1 / 0 == 0) // valid; RHS is unreachable
bad: Bool = true or (1 and 2)        // Error: and requires Bool operands
```

A mutable or otherwise unknown guard leaves the right operand reachable to the
checker, so a statically known zero divisor is still diagnosed.

Every binary operator requires identical canonical operand types after alias
resolution. Typed values are never promoted or converted, so mixed widths and
mixed signedness are errors:

```seawitch
small: Int16 = 1
large: Int32 = 2
bad: Int32 = small + large // Error: Int16 and Int32 do not mix
```

`%` is integer-only. Ordering comparisons accept integer and floating types;
equality additionally accepts `Bool`. There is no numeric truthiness. Object
and pointer comparison remains deferred.

### Operator precedence

Operators bind from highest to lowest in this order. Operators on one row are
left-associative; unary operators are right-associative.

| Level | Operators |
| --- | --- |
| 1 | `.` member access |
| 2 | unary `-`, `!`, place-only `ref` |
| 3 | `*`, `/`, `%` |
| 4 | `+`, `-` |
| 5 | `<`, `<=`, `>`, `>=` |
| 6 | `==`, `!=` |
| 7 | `and` |
| 8 | `or` |

Parentheses override precedence:

```seawitch
value: Int32 = 2 + 3 * 4       // 14
grouped: Int32 = (2 + 3) * 4   // 20
check: Bool = a + 1 > b and !done
```

`ref` remains RFC 0007's place-only address-taking form. It accepts an
identifier/member path such as `ref value` or `ref object.member`, but not a
literal, parenthesized value, or arbitrary expression.

### Contextual literals in operations

An untyped literal beside a typed operand takes that operand's type and is
range-checked against it. When all operands are untyped, a usable expected type
from the surrounding declaration propagates through the operation tree before
range checking:

```seawitch
count: UInt8 = 200
next: UInt8 = count + 1       // 1 is UInt8; exact result is 201
over: UInt8 = count + 100     // Error: exact result 300 is out of range

total: Int64 = 5_000_000_000 + 1 // both literals are Int64
ratio: Float32 = 1.5 * 2.0       // both literals are Float32
```

The expected type reaches literals only. A `Bool` result context is not an
arithmetic operand type, so `big: Bool = 5_000_000_000 > 1` uses the `Int32`
fallback and is rejected for being out of range. Without a usable context,
integers fall back to `Int32` and decimal literals to `Float64`.

Unary `-` folds a direct untyped literal as one exact mathematical value before
the destination range check. This keeps signed minima writable:

```seawitch
minimum: Int8 = -128
minimum64: Int64 = -9_223_372_036_854_775_808
```

Negating an unsigned value is an error, not an unsigned wrap.

### Exact constants and runtime wrapping

An immutable binding retains its exact checked constant value. Operations whose
operands are statically known are folded exactly and range-checked against the
declared result type before runtime wrapping is considered:

```seawitch
count: UInt8 = 200
valid: UInt8 = count + 1       // folded to 201
invalid: UInt8 = count + 100   // Error: exact result 300 is out of range
```

An operation depending on a mutable or otherwise unknown value is left for
runtime. Integer runtime arithmetic wraps modulo `2^n` for every width and
signedness:

```seawitch
mut value: Int8 = 127
wrapped: Int8 = value + 1      // runtime result is -128
```

This is defined two's-complement wrapping, not C signed-overflow behavior.
Checked, saturating, and trapping arithmetic remain future forms.

### Division and remainder

For integer `/` and `%`, the checker rejects every statically provable zero
divisor in an evaluated/reachable expression, including a folded constant
expression. A zero divisor inside the unreachable right operand of a decisive
constant short-circuit is not evaluated, folded, or diagnosed, while the right
operand is still type-checked. The checker also rejects a statically known
signed minimum divided or remaindered by `-1`, because that result is not
representable:

```seawitch
mut total: Int32 = 10
bad: Int32 = total / (2 - 2) // Error: division by zero
minimum: Int8 = -128
alsoBad: Int8 = minimum / -1 // Error: signed minimum cannot be divided by -1
safe: Bool = true or (1 / 0 == 0) // valid; the RHS is unreachable
```

An unknown runtime divisor is accepted and must be non-zero at runtime. A
runtime zero divisor, or a runtime `INT*_MIN / -1` or `INT*_MIN % -1`, remains
the documented undefined-behavior gap until trapping division exists. Integer
division truncates toward zero and remainder takes the sign of the dividend.
Floating division follows the floating rules below; floating remainder is not
implemented.

### Floating behavior

`Float32` and `Float64` arithmetic and comparison follow the existing IEC 60559
contract with round-to-nearest, ties-to-even. Overflow produces infinity and
underflow produces a subnormal or signed zero. NaN comparison follows IEC/C23:
every comparison with NaN is false except `!=`, which is true. This applies to
equality and ordering.

### C23 operator lowering

Generated operation expressions are explicitly parenthesized. Floating,
comparison, and logical operations lower directly; `and` and `or` use C23
`&&` and `||`, preserving short-circuit evaluation.

Signed integer `+`, `-`, `*`, and unary `-` never reach a C signed arithmetic
operator. The approved lowering casts operands to a promotion-safe `uint64_t`,
performs modular arithmetic there, casts to the target-width unsigned result,
and then converts through a representability guard:

```c
uint32_t reduced = (uint32_t)((uint64_t)left + (uint64_t)right);
int32_t result =
    (uint64_t)reduced <= (uint64_t)INT32_MAX
        ? (int32_t)reduced
        : INT32_MIN + (int32_t)((uint64_t)reduced - (uint64_t)INT32_MAX - (uint64_t)1);
```

The `(int32_t)` conversions above are guarded by representable ranges. Unary
`-` uses the same guard after computing `(uint64_t)0 - (uint64_t)value`. The
same shape uses `uint8_t` or `uint16_t` for the target-width cast. Unsigned
arithmetic uses a promotion-safe `uint32_t` intermediate for `UInt8` and
`UInt16`, and `uint64_t` for `UInt32` and `UInt64`, then casts back to the
target unsigned width:

```c
(uint8_t)((uint32_t)left + (uint32_t)right)
(uint32_t)((uint64_t)left + (uint64_t)right)
```

This avoids C integer promotions changing narrow arithmetic and defines the
intended modular result in the C23 output contract. Signed division and
remainder retain the documented C operator gap; unsigned division and
remainder use the same promotion-safe unsigned intermediate.

### Bitwise and shift operators (RFC 0032)

`~`, `&`, `^`, `|`, `<<`, and `>>` accept the fixed-width integer types and
`Size`; `Byte` is canonical `UInt8` and behaves exactly as it. Rune, Bool,
floating types, pointers, and every aggregate, union, and managed value are
rejected. Binary bitwise operations use RFC 0016's unique least lossless
common integer type — each operand converts exactly once and the result has
that type. Shifts keep the left operand's exact canonical type and accept any
signed or unsigned integer count, which does not join common-type selection.

A shift count must lie in `0 .. width - 1` for the `width`-bit left operand.
A constant count outside that range is a Type Error; a runtime count outside
it enters the shared unreachable numeric trap before generated C shifts
anything, so invalid C shift counts never execute. Left shift wraps the exact
bit pattern for signed and unsigned values alike. Unsigned right shift
zero-fills; signed right shift is permanently arithmetic and fills with the
sign bit. Unary `~` inverts the fixed-width pattern. Generated C operates on
promotion-safe unsigned representations, reduces to the selected width, and
reconstructs signed results with RFC 0017's two's-complement value rule, so
C integer promotions and C signed-shift behavior never change a result.

`value.bit_cast<T>()` is a protected compiler-owned generic method that
reinterprets representation bits between same-width eligible scalars: the
8-, 16-, 32-, and 64-bit integer widths plus `Float32` at 32 bits and
`Float64` at 64 bits. It preserves every bit including NaN payloads and
signed zero, performs no numeric conversion, and never accepts a pointer,
Size, Rune, or aggregate on either side. Every fixed-width integer type also
provides `to_le_bytes()` and `to_be_bytes()` returning `Array<Byte, N>` for
its exact width, plus the type-qualified `T.from_le_bytes(a)` and
`T.from_be_bytes(a)` constructors; byte order is defined by significance,
independent of host endianness. `Size` and `Rune` are excluded from endian
conversion.

The complete operator precedence, highest to lowest, is: postfix access,
indexing, generic arguments, and calls; unary `-`, `!`, `~`, and `ref`;
`*`, `/`, `%`; `+`, `-`; `<<`, `>>`; relational `<`, `<=`, `>`, `>=`; `is`;
equality `==`, `!=`; `&`; `^`; `|`; `and`; `or`. This table extends the one
in the Core operators section above.

## Structured control flow

Conditions follow RFC 0023 truthiness (see the next section), so `if count`
is valid for any value-producing `count`; `false` and `nil` are falsey and
every other value is truthy.

```seawitch
mut remaining: Int32 = 3
while remaining > 0 do
    if remaining == 1
        break
    end
    remaining = remaining - 1
end
```

`if` tests its branches in source order. `elseif` is a single keyword and an
`else` branch is optional; empty branches are valid. `while` is pre-tested,
and `break` and `continue` target only the nearest enclosing loop. These
statements may appear at module level or inside function and method bodies;
type, function, and `impl` declarations remain module-level only.

Since RFC 0028, `do` is the mandatory delimiter between a loop header and
its body: `while condition do ... end`. The former delimiter-free `while`
form is removed rather than retained as an alias. `for` adds compiler-owned
iteration over Array, View, List, String, Strand, Dict, and Stream sources:

```seawitch
for value in values do
    total = total + value
end

for i, value in values do
    total = total + value + i.to_int32()
end

for key, value in scores do
    print(key, ": ", value)
end
```

The optional leading binder is a `Size` index starting at zero. String and
Strand produce decoded `Rune` values in UTF-8 order (the index counts Runes,
not bytes); Dict produces entries in unspecified order (the index counts
produced entries, not buckets); Stream pulls until `eos`. The source
expression and traversal boundary are captured once: Array places iterate in
place, temporary Arrays and Strands materialize into one inline copy, and
reference-like sources copy their handle. Bindings are fresh, immutable,
and per-iteration; `break`, `continue`, `return`, and per-iteration `defer`
behave as in `while`. When an `if` branch terminates the current path
(`break`, `continue`, or `return`), the continuing branch's type narrowing
propagates, so `if step is EoS break end` followed by an element use works
(RFC 0031).

Every branch and loop body is a lexical scope. A declaration is visible in its
own body, may shadow an outer binding, and does not escape to a sibling or the
enclosing scope. Assignments still reach an accessible mutable outer binding.
The checker assigns each declaration a binding identity, allowing generated C
names to remain distinct even when source names are reused in sibling blocks.

`defer` registers any user expression with the current lexical scope and
discards its result. Calls capture their callee, receiver, and arguments when
the statement is reached; non-call expressions evaluate when the scope exits.
Deferred actions execute in reverse registration order. Branch bodies are
cleanup scopes, and each loop iteration has a fresh cleanup scope, so defers
run at branch completion and before the next loop condition test, including on
`break` and `continue`. An unreachable branch registers no defers.

Functions and methods with a result type must not fall through. A body returns
only when every possible path returns: an `if` requires an `else` and all
branches must return, while a `while` is conservatively treated as fall-through
because its condition may be false on entry. A return inside a loop or branch
does not change the enclosing scope's binding visibility.

Control flow lowers directly to readable C23 braces, `else if`, `while`,
`break`, and `continue` statements, with source `#line` mappings on each
generated construct. When `defer` is present, every applicable exit edge also
passes through the registered cleanup actions.

## Truthiness and Boolean contexts

Truthiness is contextual (RFC 0023): `false` and `nil` are falsey, and every
other value is truthy — zero, NaN, empty text, and empty collections included.
Truthiness exists only in Boolean-required contexts: `if`/`elseif`/`while`
conditions and the `and`, `or`, and `not` operators. There is no implicit
conversion: an integer still cannot initialize a `Bool` binding or pass to a
`Bool` parameter.

`and` and `or` accept operands of any value-producing types, mixed freely, and
short-circuit exactly as before: the right operand is evaluated only when the
left operand's truthiness requires it. `not` negates any value's truthiness.
Constant operands fold through their truthiness (`0 and nil` is `false`, `!0`
is `false`, `!nil` is `true`), and a constant falsey left operand removes the
right side entirely — its call is not evaluated, so it must still type-check
the same way a skipped `false and f()` already did.

C23 lowering keeps Bool as-is, renders the `nil` literal as `false`, renders a
nullable `P | Nil` value as `(value != NULL)`, and renders every other value
as an evaluated comma expression `(value, true)`. The comma expression
evaluates the value once for its effects and yields the constant truthy
result, so short-circuit behavior composes with C's `&&`/`||` at no runtime
cost.

The RFC 0009 rule that logical operators require `Bool` operands and the RFC
0015 rule that conditions require `Bool` are superseded; RFC 0023 is the
current rule.

## Errors, try, and errdefer (RFC 0029)

An error is an ordinary value, not an exception. `Error` is a built-in
nominal struct with the fixed five fields `file`, `line`, `column`,
`header`, and `message`; it cannot be declared, shadowed, or constructed by
a raw object literal. `Error.new(header: Strand, message: String)` is the
only constructor: the compiler injects the source-unit name and the
one-based line and UTF-8 byte column of the `Error` token, so a helper
records its own construction site, never the caller's. Error copies as an
ordinary value with no allocation, reference counting, or hidden
destruction; propagating it through `try`, return, a union, or a variable
never rewrites its fields.

A fallible function returns an ordinary structural union containing exactly
one Error member, conventionally `T | Error` or `Error | Nil`. Callers
inspect both paths with the existing `match`/`is` machinery; there is no
`Result<T, E>` wrapper and no second runtime representation.

`try` is a prefix expression: its operand must be a union containing Error
and at least one non-Error member, and the enclosing function's declared
result must accept Error. The operand evaluates exactly once. On an Error
active member, `try` returns that Error unchanged from the function, running
eligible `defer` and `errdefer` actions first; otherwise it yields the
active success value, or the normalized union of several success members.
`try` is an expression and composes anywhere a value is used, but is
rejected at module/script scope and inside any `defer` or `errdefer` action.
A cleanup call may return Error; its result is discarded under the ordinary
action-context rule.

`errdefer` registers an action with the same syntax, capture rules, and
lexical lifetime as `defer`, but the action runs only when its still-active
scope is exited as part of an Error return — through `try` or an explicit
Error return, classified by the active tag of a runtime union result. It is
discarded on normal fallthrough, `break`, `continue`, and successful return.
Mixed `defer` and `errdefer` actions in one scope share registration order
and execute in reverse order on an Error exit. `errdefer` requires an
enclosing function whose result accepts Error. A trap during cleanup keeps
RFC 0026's unrecoverable behavior.

## Source and generated names

Source identifiers are case-sensitive and use the letter-led grammar
`[A-Za-z][A-Za-z0-9_]*`. Leading underscores and digit-led names are
rejected. `main`, C keywords, and C macro spellings remain ordinary
Seawitch identifiers.

Seawitch-owned private C names apply one fixed prefix to the complete source
spelling:

| Declaration kind | Generated prefix | Example |
| --- | --- | --- |
| value binding | `sw_v_` | `score` → `sw_v_score` |
| nominal type | `sw_t_` | `Point` → `sw_t_Point` |
| object member | `sw_m_` | `x` → `sw_m_x` |

The mapping is unconditional: `int` becomes `sw_v_int`,
`INT32_MAX` becomes `sw_v_INT32_MAX`, and `sw_v_score` becomes
`sw_v_sw_v_score`. Names are never hashed, truncated, or conditionally
escaped. Foreign C names are outside this rule.

```seawitch
main: Int32 = 1
int: Int32 = 2
mut pointer: MutPtr<Int32> = ref int
value: Int32 = pointer.value
```

```c
const int32_t sw_v_main = 1;
const int32_t sw_v_int = 2;
int32_t *sw_v_pointer = &sw_v_int;
const int32_t sw_v_value = *sw_v_pointer;
```

Checked expressions retain declaration identities and structured address-of
and dereference operations. The generator is the only phase that chooses
private C identifiers and renders those operations.

## Transparent type aliases

Top-level aliases introduce a second spelling for an already declared type:

```seawitch
type Coordinate = Int32
type CoordinatePtr = Ptr<Coordinate>

mut value: Coordinate = 1
pointer: CoordinatePtr = ref value
read: Coordinate = pointer.value
```

Aliases retain the target's canonical identity. Alias chains and constructed
pointer aliases therefore add no conversion, runtime representation, or C
`typedef`; each use lowers through the canonical C declarator. Alias targets
must appear earlier in the source file, and a transparent alias cannot refer
to itself, directly or through `Ptr<T>`.

Type names and value names share one visible source namespace. Built-in scalar
names and `Ptr` cannot be redeclared. A source file containing only aliases is
valid and emits the normal empty `main`; aliases never become executable
statements.

## Core object values

An object type is a nominal, ordered collection of scalar or previously
declared object members:

```seawitch
type Point = { mut x: Int32, y: Int32, }
type Box = { point: Point, }

mut point: Point = Point { y = 2, x = 1, }
point.x = 10
read: Int32 = point.y
```

Object identity is nominal. Two separately declared object types are different
types even when their members have identical names, types, and mutability.
Transparent aliases preserve the identity of their target. Members are
read-only by default; `mut` grants write access only when every place in a
dotted path is writable. A writable object binding may be replaced as a whole.

Object literals name every member exactly once and may initialize members in
any order, with an optional trailing comma. Missing, duplicate, and unknown
members are errors. Pointer-valued members are supported, including pointer
members that reach the declaring object behind at least one pointer layer;
recursive or forward by-value object members are rejected.

The C23 representation is a file-scope `typedef struct` with members in
declaration order. Object literals lower to typed C compound literals with
designators in that same order. Aliases do not emit additional C structs.
`ref` can take the address of an object or writable object member; an object
member read from a temporary literal is valid but is not addressable.

For pointers, `.value` remains the built-in dereference operation. `value` and
`addr` are ordinary object-member names. The former scalar/pointer `.addr`
spelling is rejected with a migration diagnostic directing users to `ref`.

The sign in `-128` is a separate token. Unary `-` applies to signed numeric
values as well as direct integer and decimal floating literals. A direct
untyped literal is negated as one exact value before range checking, so
`Int64 = -9223372036854775808` is valid. Negating an unsigned value, including
`UInt8 = -0`, is an error. Negative floating constants are rounded after
applying the sign, and negative zero is preserved.

`Float32` and `Float64` use round-to-nearest, ties-to-even. Finite values that
round to infinity are rejected; finite subnormal and zero underflow is valid.
Generated C uses exact hexadecimal floating constants (`f` is added for
`Float32`) so the checked value does not depend on C decimal translation.

## Default-constant bindings

Every declaration creates a constant binding unless it is prefixed by `mut`:

```seawitch
x: Int32 = 42
mut y: Int32 = 42

x = 43 // Error: x is constant.
y = 43 // Valid.
```

## Pointer types and pointee writability

There are two pointer type constructors. `Ptr<T>` is a nullable, non-owning raw
pointer whose pointee is read-only; `MutPtr<T>` is the same shape whose pointee
is writable. They are distinct types with distinct C spellings, and the
distinction is carried entirely by the type.

```seawitch
answer: Int32 = 42
mut score: Int32 = 0

look: Ptr<Int32> = ref answer
writer: MutPtr<Int32> = ref score

writer.value = 1    // Valid: MutPtr pointee is writable
look.value = 1      // Error: Ptr pointee is read-only
```

`ref place` is the only address-taking form. It is typed by the place's
writability: a writable place yields `MutPtr<T>`, a fixed place yields
`Ptr<T>`. Unlike the former capability model, `ref` never rejects a read-only
place; a fixed place simply produces a read-only pointer.

```seawitch
mut value: Int32 = 42
writer: MutPtr<Int32> = ref value   // writable place, writable pointee
reader: Ptr<Int32> = ref value      // weakening: MutPtr<T> accepts Ptr<T>
```

`MutPtr<T>` may be assigned or passed where `Ptr<T>` is expected, outermost
layer only, with every layer below identical. The reverse, upgrading a `Ptr<T>`
to `MutPtr<T>`, is rejected as an ordinary type mismatch. Weakening never leaks
into deeper pointer layers.

```seawitch
observer: Ptr<Int32> = writer              // valid: outermost weakening
promoted: MutPtr<Int32> = look             // Error: cannot upgrade
ok: Ptr<MutPtr<Int32>> = outer             // valid: outermost only
no: Ptr<Ptr<Int32>> = outer                // Error: inner layer mismatch
```

Place writability follows a three-case walk: a variable is writable exactly
when its binding is declared `mut`; a `.member` is writable when its receiver
place is writable and the member is declared `mut`; and `.value` is writable
exactly when its receiver's pointer type is `MutPtr`. There is no expression
side `mut`, no `mut ref`, and no capability that travels with a value.

Member `mut` is retained. An object type declares which of its members are
replaceable; object literals supply values only. `mut` on a binding marks the
storage slot replaceable; it does not change a `Ptr<T>`'s pointee read-only
contract.

```seawitch
type Player = {
    id: UInt64,
    mut health: Int32,
}

mut player: Player = Player { id = 1, health = 100 }
player.health = 90    // Valid: binding is mut, member is mut
player.id = 2         // Error: id is a fixed member
```

Pointer-valued object members are supported. A nominal object may reach itself
behind at least one pointer layer:

```seawitch
type Node = {
    value: Int32,
    mut next: MutPtr<Node>,   // the link may be repointed
}
```

A by-value cycle has no finite size and remains invalid. Mutual recursion is
still rejected because RFC 0005 resolves declarations in source order.

## No source-level pointer arithmetic (RFC 0033)

Each `Ptr<T>` or `MutPtr<T>` value refers to one typed object. Seawitch
source exposes reference, dereference, ordinary copying and reassignment,
outermost weakening, identity equality under RFC 0024, RFC 0010 nullable
narrowing and Unknown erasure/recovery, and explicit deallocation — and
nothing else. The checker rejects pointer offset and distance arithmetic,
pointer indexing, pointer ordering comparisons, pointer-to-integer and
integer-to-pointer conversion, and a pointer on either side of
`bit_cast<T>()`, all as source-located Type Errors. `++`, `--`, `+=`, and
`-=` remain absent from the grammar for every type.

There is no source-visible one-past state and no ordinary constructor that
turns `Ptr<T>` plus a count into a `View<T>`. Multiple elements use the
bounds-carrying Array, View, and List types; dereferencing a pointer to such
a value and then indexing that value remains valid. Trusted compiler-owned
generated and runtime C may step pointers internally, but a rejected source
pointer operation never reaches generated C — reaching generation with one
is an Unknown Error. A future C FFI may import an address but cannot
advance, order, convert, or index it in Seawitch source.

## C23 lowering and target profile

Scalar types lower directly to their canonical C23 names. Integer constants are
reconstructed from checked exact values; `INT64_C` and `UINT64_C` protect wide
values, and signed minima use `INT*_MIN` without constructing an overflowing
positive magnitude. Octal values lower to decimal; hexadecimal and binary
radices remain readable in C23 output.

The two pointer constructors lower to ordinary C pointer declarators with
pointee qualification derived from the type chain alone. A `Ptr` layer adds
`const` to its pointee; a `MutPtr` layer does not. The binding contributes a
trailing `const` when it is not `mut`:

| Seawitch | C23 |
|---|---|
| `Ptr<Int32>` | `const int32_t *` |
| `MutPtr<Int32>` | `int32_t *` |
| `Ptr<Ptr<Int32>>` | `const int32_t *const *` |
| `MutPtr<Ptr<Int32>>` | `const int32_t **` |
| `Ptr<MutPtr<Int32>>` | `int32_t *const *` |
| `MutPtr<MutPtr<Int32>>` | `int32_t **` |

Because `ref` never writes through a fixed place and weakening only reads, the
generator never emits a qualifier-discarding cast. A fixed binding of `Ptr<T>`
lowers to `const T * const`; a `mut` binding of `MutPtr<T>` lowers to `T *`.

```seawitch
mut value: Int32 = 42
writer: MutPtr<Int32> = ref value
```

```c
int32_t sw_v_value = INT32_C(42);
int32_t *sw_v_writer = &sw_v_value;
```

Object members are unqualified members whatever their member mode; only the
pointer type contributes pointee `const`. Every object uses a source-ordered
forward `typedef` region followed by a source-ordered definition region, so
recursive and non-recursive objects share one shape:

```c
typedef struct sw_t_Node sw_t_Node;

struct sw_t_Node {
    int32_t sw_m_value;
    sw_t_Node *sw_m_next;
};
```

### Nullability

`Ptr<T>` and `MutPtr<T>` never hold `nil`; the only way to express absence is
the explicit nullable union `P | Nil`, which the checker accepts only over a
pointer-like base. `nil` initializes `P | Nil`, and a nullable value may be
reassigned from `nil` to a pointer and back, but never implicitly loses
`Nil`:

```seawitch
mut maybe: Ptr<Int32> | Nil = nil
if maybe != nil
    result: Int32 = maybe.value
end
```

`== nil` and `!= nil` tests commute and produce `Bool`; in each branch the
checker narrows the tested binding to the base pointer type or to `Nil`, and
dereferencing a possibly-nullable value outside a narrowing branch is a
checked diagnostic. A nullable pointer union reuses the base pointer's C
representation with no tag or wrapper, so nullability adds no runtime
overhead. `Nil` lowers to the C23 `nullptr_t` type, `nil` lowers to the
`nullptr` constant, null tests lower to `== nullptr` / `!= nullptr`, and
`<stddef.h>` is included only when the program uses `nil` or a null test.

`Unknown` is an incomplete pointee type: it may appear only behind `Ptr` or
`MutPtr`, cannot be stored or dereferenced by value, and one layer of pointer
erasure and recovery converts between `Ptr<T>` and `Ptr<Unknown>` (and the
`MutPtr` forms) as explicit widening or narrowing. `Ptr<Unknown>` lowers to
`const void *` and `MutPtr<Unknown>` lowers to `void *`:

| Seawitch | C23 |
|---|---|
| `Ptr<Unknown>` | `const void *` |
| `MutPtr<Unknown>` | `void *` |

### General Unions

Union types are structural and transparent:

```seawitch
type Number = Int32 | Float64
mut value: Number = 1
value = 2.5

if value is Int32
    integer: Int32 = value
else
    floating: Float64 = value
end
```

Nested unions are flattened, duplicate members are removed, and `A | B` has the
same canonical type as `B | A`. Written order remains the contextual priority
for an untyped initializer: the checker tries members from left to right and
selects the first complete candidate. Already-typed values prefer an exact
member, then use an ordinary permitted conversion. A source union widens only
when every source member is accepted by the destination; unions never narrow
implicitly and declarations never infer a wider type.

Member values inject without source constructors or allocation. The exact
`is` operator tests one non-`Nil` active member and narrows direct local reads
inside `if`, `elseif`, and `while` branches. False paths carry the normalized
remainder. Assignment and writable address escape invalidate narrowing; the
declared storage type remains the type used by assignment and `ref`.

`==` and `!=` are available for identical canonical union types only when every
member supports equality. Generated comparison switches on the active tag and
never reads an inactive payload. Ordering and member-specific operations remain
invalid until a branch proves one member active.

`Int32 | Float64` lowers to a value struct containing a deterministic tag and C
union payload. The compiler emits one helper type per canonical union, uses
compound literals for injection, and uses by-value helpers for widening and
equality so side-effecting expressions are evaluated once. `Nil` has a tag but
no payload. A one-pointer-plus-`Nil` union keeps RFC 0010's null niche instead
of using a tag. RFC 0023 truthiness is type-directed: Nil is falsey, an active
Bool member supplies the result, and every other active member is truthy.

### Generic Types, Functions, and Methods

Generic declarations abstract over types:

```seawitch
type Box<T> = { value: T }

fun identity<T>(value: T): T
    return value
end

box: Box<Int32> = Box<Int32> { value = 42 }
same: Int32 = identity(box.value)
```

A concrete specialization is keyed by the declaration plus the ordered
canonical argument types, so `Box<Int32>` and `Box<UInt32>` are different
types. Repeated requests reuse one canonical type and one generated C
definition. Generic constructed types are invariant.

Calls and constructors infer type arguments deterministically: explicit
argument lists must be complete, typed arguments unify against the signature,
the expected result type contributes evidence, and untyped literals use their
defaults. Repeated occurrences must agree exactly; conflicts, unresolved
parameters, and underconstrained function-value references are errors. A
generic function name in a value position infers its arguments from an exact
`Fun<...>` target. Generic methods inherit the receiver's arguments and infer
or accept their own.

Open generic bodies are checked with parameters as abstract placeholders:
operations whose validity depends on the substituted type are deferred and
rechecked at each concrete specialization, so `maximum<Bool>` reports the
operator failure with the concrete instantiation. Recursive specializations
reuse an active specialization with unchanged arguments and reject cycles that
change arguments. Unused generic declarations generate no C.

Reachable specializations are monomorphized: generic objects become concrete
structs with deterministic sanitized names such as `sw_t_Box_Int32_`, and
generic functions become concrete definitions with specialization suffixes
such as `sw_f_identity_Int32`. A prototype region precedes the specialized
definitions so cross-references remain dependency-safe. No runtime type tags,
`void *` erasure, or unchecked casts represent a type parameter.

### Allocation and Deferred Cleanup

`Heap` is the built-in allocation handle. `Heap.new()` selects the default
allocator at compile time and performs no runtime allocation. Allocation is
explicit and checked:

```seawitch
h: Heap = Heap.new()
p: MutPtr<Int32> = h.allocate<Int32>(0)
p.value = 42
defer h.free(p)
```

`h.allocate<T>(initial)` allocates and initializes storage for one complete,
finite `T` and returns `MutPtr<T>`; the RFC 0007 weakening to `Ptr<T>` remains
valid for read-only use. Allocation failure and unrepresentable sizes are
defined runtime diagnostics, never a nullable result. `h.free(value)` releases
an allocation through either pointer type; the runtime validates allocator
identity and live state, so a wrong-allocator or double free is a defined
runtime error. `Ptr<T>` and `MutPtr<T>` remain non-owning, and `mut` controls
binding reassignment only.

`defer expression` registers an action for the current lexical scope:

- a direct call captures its callee, receiver, and arguments at registration;
  later reassignment does not change the captured values;
- every other expression evaluates at scope exit and its result is discarded.

Actions run in reverse registration order when the scope exits: normal
fallthrough, the end of the selected `if`/`elseif`/`else` branch, the end of
each `while` body iteration, `return` (after the return value is evaluated),
`break`, and `continue`. `break` and `continue` unwind only the current
iteration's scopes; `return` unwinds every active scope from inner to outer.
A `defer` that is never reached is never registered.

Allocation and deallocation lower through checked helpers: a header records
allocator identity, size, payload offset, and live state, and a per-type
`sw_heap_allocate_<T>` helper validates and initializes before exposing the
pointer. No plain C `free` and no omitted cleanup are emitted.

### Algebraic Data Types and Match

Algebraic data types are nominal closed sums with unit and named-record
variants:

```seawitch
type Shape =
    | Circle as { r: Int32 }
    | Square as { a: Int32 }

type Direction = | East | West | North | South

shape: Shape = Shape.Circle { r = 10 }
heading: Direction = Direction.North
```

An ADT is one nominal type; variants are qualified by their owner and never
enter the global value namespace. Record constructors validate every payload
field exactly once. Payload fields are immutable. Recursion is allowed only
through a pointer layer, so a direct by-value self-reference is rejected.

Generic ADTs use RFC 0019 syntax and specialization: `type Result<T, E> =
| Ok as { value: T } | Err as { error: E }`. Construction resolves the
instantiated owner — `Result.Ok { value = 42 }` infers its arguments from the
expected type, and `Result<Int32, Bool>.Ok { value = 42 }` supplies them
explicitly. Match patterns accept both spellings (`| Result.Ok then ...` and
`| Result<Int32, Bool>.Ok then ...`) against the specialized scrutinee.

`match` is an expression that evaluates its scrutinee once and selects one
arm. Value mode (`match ready | true then ... | false then ... end`) matches
Boolean literals; type mode (`match shape is | Shape.Circle then ... end`)
matches exact union members, `Nil`, and qualified ADT variants. `else` is the
final default arm and covers the remaining members. The checker requires
exhaustiveness: every ADT variant, every union member, both Booleans, or
`else`. A named scrutinee is narrowed inside each arm, exposing only the
selected variant's payload fields; the narrowed view never survives the arm.

ADTs lower to deterministic tag-and-payload structs: one tag enum, one payload
union with a struct per record variant, and no dummy field for unit variants.
Construction sets the tag and payload together; match lowers to if/else chains
over tag or Boolean comparisons, and a payload field is read only after its
tag test. An impossible runtime tag reaches the compiler-owned fatal trap.

Generated headers include checks for 8-bit bytes, exact-width integer types,
and the binary32/binary64 value-set properties used by the program. A target
must provide the canonical C23 integer types and compatible IEC 60559 float
formats; a C assertion or target validation failure must not silently change a
Seawitch type's meaning.

Allocation provenance, alignment, bounds, aliasing, casts, arithmetic,
conversions, slices, and imported C contracts remain future features.

## Collections

RFC 0020 defines four collection types with one shared affine ownership
model. `Array<T, N>` and `View<T>` are inline values; `String`, `List<T>`,
and `Dict<K, V>` are pointer-sized handles to heap objects.

### Fixed arrays and views

`Array<T, N>` is a fixed inline sequence of `N` inline elements
(`Array<Int32, 3> = [10, 20, 30]`). The literal must contain exactly `N`
elements; a trailing comma is allowed. Elements are read and written through
`arr[i]` and `arr.at(i)`, both bounds-checked; a constant index outside the
length is a compile-time error and every dynamic access traps before forming
a data pointer. `arr.length()` is a compile-time constant and `is_empty()` is
`false`. Arrays are ordinary values: whole-array assignment, member storage,
parameter passing, and return copy the complete inline region. Only the
inline element class (scalars, pointers, nested inline arrays, inline
unions, and aggregates of those) is allowed; `String`, `List`, `Dict`,
`View`, and function values are rejected.

`View<T>` is a non-owning read-only contiguous view over stable source
storage: an Array local/parameter/member, a live List, or another View.
`source.slice(start, end)` requires `0 <= start <= end <= length` (constant
bounds against a known length are checked at compile time, everything else
traps), and the result is tied to its root by a lexical lifetime: the view
dies with its scope, and reassigning any binding in its root chain is
rejected while the view is live. List `push`/`pop`/`clear`/`free` are
rejected while a derived view is live; `set` and element writes stay valid.
A view is read-only (its element place is never writable, though a `MutPtr`
element's pointee keeps its capability), may not be returned, stored, placed
in module data, or pointed to, and cannot be rooted in a temporary Array.

### Strings

A String value is a `const sw_string *` handle to an immutable
pointer-plus-count header. Runtime strings occupy one combined
header-and-bytes allocation; literals are allocation-free static objects and
may live at module level. Under RFC 0035 every String binding copies its
handle: assignment, argument passing, return, aggregate storage, and
collection insertion are shallow C-style copies with no ownership state,
moved-from status, or compiler-enforced cleanup. The programmer pairs each
owning construction with exactly one `free(h)` — `defer` is convenient but
optional — and every alias to a freed allocation becomes invalid without
diagnosis.
`bytes()` and rune-bounded `slice(start, end)` return zero-copy
`View<UInt8>`; `to_string(h)`, `concat(h, other)`, and
`String.from_bytes(h, view)` create distinct owning objects; `free(h)`
validates the allocator identity before releasing. String is rejected in
unions, arrays, and pointers, and may not be an element of View, List, or
Dict; objects and ADTs may freely contain String handles.

### Lists

`List<T>` is an explicitly freed growable sequence (`List<T>.new(h)`),
mutated through any live reference — a fixed binding's pointee changes
without `mut`. `push`/`pop`/`set`/`clear`/`free` plus `length`/`is_empty`/
`at`/indexing/`slice` follow the Array contracts with runtime lengths and
bounds traps. Handles copy shallowly under RFC 0035; parameters mutate the
caller's object. `List<String>` keeps its deep-copy-on-`push`/`set` and
destroy-on-`set`/`pop`/`clear`/`free` contract, and `pop` moves the stored
String out for the programmer to free. `values.stream(h)` builds a
non-owning lazy Stream over the List's existing elements. Growth relocates
only pointer slots.

### Dictionaries

`Dict<K, V>` is an explicitly freed open-addressing dictionary whose key
type is exactly `Int32` or `Strand` (`Dict<Strand, Int32>.new(h)`).
`Strand` is an inline literal-only value: a string literal in a Strand
position, or a binding initialized from one. `insert` copies inline keys
and values and copy-in Strings; `get` traps on a missing key and returns a
shallow String handle; `contains` does not expose values; `remove` traps on
a missing key and returns a String handle for the programmer to free;
`free` destroys every stored String and the bucket region. Under RFC 0035,
read handles are shallow copies and the programmer keeps the dictionary
valid while using them. Hashing is infallible (splitmix for `Int32`,
FNV-1a for `Strand`), and equality of `Strand` keys compares bytes.
Dictionary equality is deferred.

### C-style copying and manual lifetimes (RFC 0035)

Values copy by C representation everywhere: assignment, argument passing,
return, object and ADT construction, and collection insertion. Bool,
numeric scalars, Byte, Rune, and Nil copy as scalars; Strand, Array, and
objects copy member by member; ADTs copy tag and active payload; pointers
and function values copy the pointer; String, List, Dict, Arena, Pool, and
Stream copy their pointer-sized handle; View copies its descriptor; Heap
copies its allocator identity. Copying is recursively shallow — an object
containing a List copies the List handle, never the allocation. The
compiler tracks no owner, emits no retain count or hidden destructor, and
performs no cleanup-path analysis. The programmer arranges exactly one
explicit cleanup per allocation (`defer` optional); use after free, double
free, and wrong-allocator free are programmer errors whose detection is not
guaranteed. `free` validates the allocator identity and traps on mismatch.
Aggregates may freely contain reference-like handles.

### Size, conversions, and defined arithmetic (RFC 0036/0016/0017)

`Size` is the unsigned target-sized integer mapped to C's `size_t`; it is
the canonical type of collection and text lengths and of iteration indices.
A fixed-width unsigned integer widens losslessly to `Size`, and `Size`
widens to nothing wider; mixed `Size` arithmetic with signed integers is
rejected. Numeric conversion methods are destination-named and explicit:
`to_int8` ... `to_uint64`, `to_float32`/`to_float64`, and `to_size`, each
with `_wrapping` and `_saturating` forms. Checked conversions trap on
out-of-range values at runtime and fold exactly at compile time; wrapping
conversions reduce modulo 2^n; saturating conversions clamp to the
destination range. Mixed-type integer arithmetic computes in the unique
lossless common type (RFC 0024's rule, extended to `+`, `-`, `*`, `/`, and
`%`) and wraps at that type; `IntN_MIN / -1` is defined and folds to
`IntN_MIN`. Integer division and remainder always trap on a zero divisor,
and `IntN_MIN % -1` yields 0.

### Explicit conversion syntax (RFC 0038)

The one explicit scalar conversion is the generic method call
`source.to<Destination>()`, which reuses RFC 0019's generic-call grammar and
supersedes the destination-encoded method names listed above (`to_int32`,
`to_float64`, `to_size`, and so on). It requires exactly one explicit type
argument, accepts no value arguments, requires final call parentheses,
evaluates its receiver exactly once, and never infers the destination from
an expected result type. On an eligible scalar receiver, `to` is a
compiler-owned method resolved before ordinary lookup; unrelated nominal
objects may still declare their own method named `to`.

`to<T>()` converts the mathematical value under RFC 0016's checked rules:
fixed integers and `Size` range-check the destination, floats convert with
round-to-nearest ties-to-even and trap on finite overflow, float-to-integer
truncates toward zero before the range check, and Rune participates only in
checked integer pairs with Unicode scalar validity. Constants fold or fail
at compile time; an invalid runtime value traps before C performs an unsafe
conversion. Byte is canonical UInt8 and accepts `to<Byte>()` without a
distinct runtime conversion; transparent aliases canonicalize. Every other
pair — including any pointer source or destination — is rejected. Wrapping,
saturating, unchecked, and mode-selecting conversions do not exist; former
and invented destination names receive ordinary missing-method diagnostics
with no compatibility aliases. Value conversion is distinct from the
representation-reinterpreting `bit_cast<T>()` of RFC 0032. Generic
conversions are dependent operations rechecked at every closed
specialization.

### Pull streams (RFC 0031)

`Stream<T>` is a lazy single-pass pull sequence: one pointer-sized handle
to a fully heap-allocated header-and-state node. `Stream<T>.new()` is the
canonical allocation-free empty Stream; `Stream<T>.produce(h, state, next)`
stores one shallow State copy and pulls through a named
`Fun<(MutPtr<State>) : T | EoS>` callback; `List<T>.stream(h)` is a
non-owning lazy source over existing elements. `next()` returns `T | EoS`;
`EoS` is a built-in singleton (`eos`) that uses ordinary union `is`
narrowing and folds equal to itself. `filter(h, pred)`, `map(h, mapper)`,
and `take(h, count)` allocate one adapter node that owns its upstream by
API convention, and `free(h)` releases the whole chain exactly once through
the matching allocator. `for value in stream` pulls until `eos` without
freeing the handle, so `break` may be followed by further pulls. Streams
are single-threaded, non-reentrant, and not collections: no length, random
access, or rewind.

## Print builtin (RFC 0030)

`print` is a compiler-owned builtin that uses ordinary call syntax and adds
no grammar production. It is a protected unqualified name — no binding,
parameter, import, or free function may use it, while object members and
methods named `print` remain ordinary — and it cannot be overloaded,
referenced without a call, or taken as a `Fun<...>` value. The builtin
requires at least one argument, produces no value, and takes no type
arguments. `print()` is a Type Error; `print("\n")` emits a blank line.

Arguments evaluate exactly once from left to right, and no bytes for the
call are written until every argument has finished evaluating. Accepted
types are Bool; every fixed-width integer, `Size`, `Byte` through its
canonical UInt8 identity, Float32, Float64, Rune, String, Strand, and Nil;
the reserved `Error` type; and nominal objects, ADTs, `Array<T,N>`,
`View<T>`, `List<T>`, and `Dict<K,V>` whose contents are recursively
printable. Unions, `EoS`, pointers, functions, allocators, Files, Streams,
and other resources are rejected, and an aggregate containing an
unsupported member is rejected as a whole. `print` borrows everything for
the call only and performs no allocation, copy, or free.

The textual rules: no separator or implicit newline; signed integers in
canonical decimal and unsigned integers without sign; Byte prints its
numeric value; a top-level Rune prints its exact UTF-8 encoding; String
prints its logical payload bytes including embedded NUL; floats use the
fixed non-finite spellings `inf`, `-inf`, and `nan`, preserve `-0`, and use
C-locale `%g` formatting with precision 9 for Float32 and 17 for Float64;
a direct Error prints exactly `file:line:column: header: message`.
Nested text is quoted and escaped (`\"`, `\\`, `\0`, `\n`, `\r`, `\t`,
`\xHH`) so aggregate boundaries stay readable; nested Runes are
single-quoted with `\u{HEX}` escapes. Objects print `Name { member = value,
... }` in declaration order; ADTs print their qualified active variant and
only its active payload; sequences print `[a, b, c]` in index order; Dicts
print `{key: value, ...}` in the existing unspecified iteration order.
Aggregate output is one line; this is readable structural output, not
serialization.

One complete `print` call is atomic relative to every other print and RFC
0040 standard text write when the scheduler runtime is linked: a process-
wide lock is acquired only after all arguments evaluate, and it parks the
Task, not its worker. Programs without scheduler features initialize no
lock. `print` does not flush per call; at normal completion the runtime
closes the shared output gate after root cleanup, waits for in-flight
operations, and checks a final flush. A detected formatting or write
failure is an unrecoverable Runtime Error to stderr, never a recoverable
compiler diagnostic. Generic `print` arguments are dependent operations
rechecked at every closed specialization.


## Equality and ordering

RFC 0024 owns the cross-cutting comparison rules. Numeric operands compare
through the unique least lossless common type: a typed value widens only
when every value of its type is exactly representable by the destination,
and the comparison uses the minimum of the candidate intersection (for
example `Int32 == UInt32` compares as `Int64`, `Int32 < Float32` compares as
`Float64`, and `Int64 == UInt64` is an error). No narrowing, value-losing
conversion, or implicit pointer weakening is ever inserted. Identical
non-numeric operand types compare strictly: pointers by identity without
dereferencing, `String` and `Strand` by exact payload bytes, objects by
declared members in order, ADTs by variant tag then payload, unions by
active member, and `Array`/`View`/`List` by length then elements in order.
Function values, allocator handles, and dictionaries are never
equality-comparable; an object or sequence is comparable only when every
recursively compared component is. Ordering exists only for numeric
scalars (with the same widening) and for `String` and `Strand` (unsigned
bytewise lexicographic order, shorter-prefix first). `Nil` comparisons
remain the focused nullable tests of RFC 0010. Equality lowers to per-type
C helpers that never read padding, capacity, or backing addresses, never
use `memcmp`, and never read an inactive union or ADT payload. Comparison
operators in generic bodies are rechecked with concrete types at
specialization.

## Tasks, concurrency, and parallelism (RFC 0037)

`Task<R>` is a built-in reference-like handle to one spawned execution of an
ordinary named function returning `R`. `spawn` is a reserved prefix
expression whose operand is a direct call to a named, non-capturing
function: arguments evaluate exactly once, left to right, and shallow-copy
into the new task's frame; the expression has type `Task<R> | Error`, and
creation failure starts no task. `Task.yield()` is an explicit scheduling
point; `join()` parks only the calling task, returns exactly `R`, and
reclaims runtime storage; `detach()` discards the result. Exactly one
successful join or detach is permitted; anything else is programmer error
under RFC 0035's C-style lifetime model. `Task`, `Channel`, `Mutex`, and
`Atomic` are protected built-in names.

The runtime owns one process M:N scheduler: tasks are stackful fibers with
guarded 1 MiB virtual stack reservations, scheduled over C23 `<threads.h>`
worker threads (Windows x64 Fibers, POSIX x86-64 System V context switch).
The Seawitch entry point is the root Task pinned to worker zero; returning
from it does not implicitly join live source tasks. Scheduler-owned storage
never takes a user allocator, and no scheduler is linked when no task
feature is used. Scheduling is cooperative: a CPU loop that neither blocks
nor yields can monopolize one worker, so every repeating path through a
task-reachable literal `while true` must execute a visible `Task.yield()`
or the program fails at compile time; a yield inside a conditional or
helper does not satisfy the rule.

`Channel<T>` is a bounded, scheduler-aware, multi-producer/multi-consumer
FIFO over a fixed-capacity ring: `new` fails on zero capacity, `send` parks
the current task on full and returns Error on close, `receive` parks on
empty and returns `T | EoS` (closed-and-drained), `close()` is idempotent,
and `free(h)` requires a closed, empty, unused Channel. Recursively
contained `Atomic<T>` values and a top-level `EoS` union member are rejected
as element types. `Mutex` is a heap-backed, non-recursive,
scheduler-aware lock: waiting parks the task, ownership follows Task
identity across worker migration, unlock-to-lock synchronizes, and
detectable misuse traps. `Atomic<T>` is an inline wrapper over C23
`_Atomic(T)` for Bool, the fixed-width integers, and `Size`; every operation
is sequentially consistent, values cannot be copied, assigned, or stored in
collections, and `compare_exchange` is the strong, non-spurious form.

Shared memory follows the C23 memory model. Synchronization edges are:
spawn publishing copied arguments, join publishing task writes, Mutex
unlock-to-lock, Channel send-to-receive, and the sequentially consistent
atomics. List, Dict, String handles, pointers, Arena, and Pool are not
automatically synchronized; Heap allocation and free are safe to call
concurrently from different tasks. Arbitrary C calls are opaque to the
scheduler and block their worker.

## File I/O and standard files (RFC 0040)

`File` is a small inline shallow-copyable handle over a C `FILE *` with mode
and ownership flags; copies alias the same stream, container cleanup never
closes a stored File, and cleanup stays explicit under RFC 0035. `FileMode`
is a protected compiler-injected ADT with exactly `Read` (`rb`), `Write`
(`wb`), and `Append` (`ab`) — binary C modes on every platform.
`File.open(path, mode)` validates a non-empty, non-NUL ASCII path (literal
violations fail at compile time) and returns `File | Error`. `read_bytes(h)`
returns an owning `List<Byte>` read to EOF; `read_text(h)` validates UTF-8
and returns an owning String (a recoverable Error, unlike
`String.from_bytes`'s trap); `write(View<Byte>)` and `write_text(String)`
loop over short C writes, write every payload byte (never the trailing
NUL), and report partial external effects on Error; `flush()` is recoverable
and output-only; `close()` returns no value, closes only owned Files, and
traps on `fclose` failure. A runtime mode mismatch fails with Error before
any incompatible C call.

`Stdio.stdin()`, `Stdio.stdout()`, and `Stdio.stderr()` are borrowed,
text-only standard Files with fixed modes; `Stdio` is a protected intrinsic
qualifier, not a type or module. The checker rejects operations known
invalid on a direct Stdio result at compile time; a copied, stored, or
passed standard File falls back to the runtime ownership and mode checks.
When the RFC 0037 scheduler is linked, standard text writes and flushes
share RFC 0030's process-wide output lock and shutdown gate, so one
`write_text` call is atomic relative to every other standard write and
`print`; at shutdown the gate closes after root defers, waits for in-flight
operations, and flushes stdout and stderr before `main` returns. File and
`Stream<T>` remain separate facilities with no implicit conversion.

## No module globals (RFC 0041)

Modules contain declarations only. An imported module may hold type,
function, and method declarations but no value bindings, native constants,
runtime statements, or import-time side effects; there is no `global`,
`static`, or native module-constant syntax. Only the root module carries
executable top-level statements, and those lower as automatic locals inside
the generated entry body — they are not globals, and functions cannot
capture them. State is passed explicitly as parameters or through heap
storage; imported C globals remain visible only as qualified foreign
accesses under RFC 0039. No accepted native declaration emits user value
storage at C file scope.

## Layout queries and volatile access (RFC 0042)

`size_of<T>()` and `align_of<T>()` are protected type-qualified intrinsics
that require exactly one explicit, complete, finite-sized type argument and
return `Size` C constant expressions (`(size_t)sizeof(T)` and
`(size_t)alignof(T)`); inside generics they validate and lower only after
specialization. Reference-like types report their source handle size, not
their allocation. `read_volatile()` and `write_volatile(value)` are
per-access operations on `Ptr<T>` and `MutPtr<T>` for the fixed-width
integers, Byte, and `Size`: `MutPtr` may read or write, `Ptr` may read
only, nullable pointers must be narrowed first, and receivers and written
values each evaluate exactly once. Lowering adds volatile qualification
without casting away pointee constness. Volatile provides C volatile-
observable behavior only — no atomicity, synchronization, fence, or
device ordering; shared state uses RFC 0037's Atomic or Mutex, and
contiguous regions use RFC 0043's View bridge.

## View bridge (RFC 0043)

`View<T>.from_pointer(pointer, length)` is the explicit trust-boundary
constructor: it turns a statically non-null `Ptr<T>` or `MutPtr<T>` (with
MutPtr-to-Ptr weakening) plus a `Size` element count into a read-only View
descriptor, and `View<T>.empty()` builds an allocation-free zero-length
View. The pointer expression evaluates first, the length second, each
exactly once, and lowering is one descriptor initialization with no extent
multiplication, allocation, copy, or ownership token. The caller asserts
contiguous initialized storage, alignment, lifetime, and freedom from
concurrent mutation; the compiler checks pointer type, non-nullability,
RFC 0020 element eligibility, and the length type. Later indexing and
slicing keep their bounds checks against the recorded length. The
constructor is the standard adaptation for C APIs that return a pointer
plus a separate count; RFC 0033's no-pointer-arithmetic rule is unchanged,
and no address or stepping operation becomes available.

## Byte, Rune, and text conformance (RFC 0044)

`Byte` is a built-in transparent alias of `UInt8`: one canonical identity,
one generated C type, one generic specialization, and numeric formatting
under `print`. It is the preferred source spelling for raw storage, encoded
text, serialization, and byte I/O, while `UInt8` remains the numeric
spelling. Byte literals `b'...'` contain exactly one byte (printable ASCII
or an escape, including `\xHH`); Rune literals `'...'` contain exactly one
Unicode scalar and accept `\u{...}`; String literals share the extended
escape set, may contain an escaped NUL, and store UTF-8. `Rune` remains a
distinct scalar type with the invariant `0 <= value <= 0x10FFFF` outside
`0xD800..0xDFFF`; every decoder and constructor preserves it. Strand is an
immutable, literal-only inline 32-byte value (31 payload bytes, terminator,
zero fill); the checker rejects payloads over 31 bytes, embedded NUL, and
invalid UTF-8, and Strand never returns a View into its inline bytes.

The completed String surface is `length`/`is_empty`/`at`/indexing in rune
units, `bytes()` and rune-bounded `slice` returning zero-copy
`View<Byte>`, `to_string`/`concat`/`free`, `String.from_bytes(h,
View<Byte>)` (which validates before allocating and traps on invalid
UTF-8), `String.from_runes(h, View<Rune>)`, and `rune_cursor()` returning a
non-owning `RuneCursor` with `has_next()`/`next()` over the same borrowed
String storage. Strand's surface is `length`/`is_empty`/`at`/indexing and
`to_string`. String and Strand method dispatch is separate and never
crosses. All text lengths, indices, offsets, and loops use `Size`/`size_t`;
runtime text helpers never use `strlen` for String length or UInt64 length
remnants, and Rune values lower to `uint32_t`.
