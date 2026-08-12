# RFC 0014: General Type Expressions and Union Types

- Status: Implemented; conformance verified 2026-08-10
- Features: general type expressions, structural union types, union
  normalization, member injection, union widening, exact `is` type tests,
  equality for identical union types, tagged value lowering, and structured
  control-flow narrowing
- Created: 2026-08-09
- Revised: 2026-08-10
- Depends on: RFC 0005 (type declarations and transparent aliases), RFC 0006
  (core object values), RFC 0007 (mutability redesign), RFC 0010 (`Nil` and
  explicit pointer nullability), and RFC 0015 (structured control flow)
- Coordinates with: RFC 0008 (functions) and RFC 0009 (operators)
- Extends when accepted: RFC 0009's equality rules to identical union operand
  types whose every member supports equality
- Extends when accepted: RFC 0010's `P | Nil` type form to unions of arbitrary
  complete value types
- Supersedes when accepted: RFC 0010's restrictions that `|` accepts only one
  pointer-like member followed by `Nil`, that `Nil` must be written last, and
  that duplicate written members are errors
- Does not delay RFC 0010: pointer-like `P | Nil` has a complete null-niche
  representation and may be implemented before this RFC

## Summary

Seawitch type positions accept type expressions. The `|` operator combines
existing types into a structural union type:

```seawitch
type Number = Int32 | Float64
type MaybeCount = Int32 | Nil

number: Number = 42
missing: MaybeCount = nil
```

A union value contains exactly one member at runtime. A value of a member type
injects into the union without a source-level constructor:

```seawitch
mut value: Int32 | Bool = 42
value = true
```

The declaration annotation fixes the binding's type. Reassignment never
widens it implicitly:

```seawitch
mut value: Int32 = 42
value = true
// Type Error: expected Int32, got Bool
```

Union identity is structural, flattened, duplicate-free, independent of
member order, and based on canonical member identities:

```text
Int32 | Bool                    == Bool | Int32
(Int32 | Bool) | Nil            == Int32 | Bool | Nil
Int32 | Int32                   == Int32
```

Written order nevertheless supplies contextual-expression priority. When an
untyped expression can be checked against more than one union member, the
checker tries the written members from left to right and selects the first
member under which the complete expression is valid:

```seawitch
small: UInt8 | UInt16 = 7
// The active member is UInt8.

wide: Int64 | Int32 = 7
// The active member is Int64.

sum: Bool | Int64 = 2 + 2
// Bool cannot type integer addition; the active member is Int64.
```

Thus order does not change which values the union type can contain, its
canonical identity, or its C layout. It can change the active member selected
for a context-dependent initializer or assignment.

RFC 0010's nullable pointer form remains a specialized union representation:

```seawitch
MutPtr<Node> | Nil
```

It uses the C pointer's null representation and requires no tag. Other unions
use an inline tag and payload. They do not allocate.

This RFC uses Crystal's union model as a reference: `A | B` composes existing
types and `T | Nil` is ordinary type composition. Seawitch deliberately keeps
mandatory declaration annotations and initially requires explicit narrowing
before member-specific operations. It also deliberately differs from Crystal's
ambiguous-autocast rule: a context-dependent expression tries written union
members from left to right.

The `is` operator asks whether one exact member is active:

```seawitch
value: Int32 | Float64 = read_value()
integer_active: Bool = value is Int32
```

Two values of the same canonical union type support `==` and `!=` when every
member type supports equality:

```seawitch
left: Int32 | Bool = read_left()
right: Bool | Int32 = read_right()
same: Bool = left == right
```

## Terminology

This RFC calls the feature a **union type**, not a named sum type.

```seawitch
Int32 | Bool
```

The alternatives are existing types. The source does not declare case names
or payload constructors such as `Some(Int32)`, `Ok(Int32)`, or
`Error(Int32)`. Named variants, if Seawitch later needs them, are a distinct
feature.

For a union type `U`:

- a **member** is one canonical type admitted by `U`;
- the **active member** is the one member whose value is stored at runtime;
- a **candidate order** is the left-to-right member order retained from a
  written union type expression for contextual expression checking;
- **injection** converts a member value into a union containing that member;
- **widening** converts a union to another union with a superset of its
  members; and
- **narrowing** proves which member is active and exposes that member's type.

`Nil` is an ordinary union member. The phrase **nullable type** remains useful
for a union containing `Nil`, but nullability is not a second type mechanism.

## Motivation

### One composition syntax

Seawitch needs to express values that may have one of several known types:

```seawitch
type NumericValue = Int32 | Float64
type LoadResult = Asset | LoadError
type OptionalScore = Int32 | Nil
```

Adding separate `Optional<T>`, `Result<T, E>`, and anonymous variant syntax
would create overlapping mechanisms. A structural union is the smaller
foundation. Named object types can provide domain names where desired:

```seawitch
type Loaded = {
    asset: Asset,
}

type Failed = {
    error: LoadError,
}

type LoadResult = Loaded | Failed
```

### Explicit binding contracts

Crystal commonly derives a variable's union from assignments in different
branches. Seawitch does not. A new binding already requires an explicit type,
and that annotation remains its stable contract:

```seawitch
mut result: Loaded | Failed = load_asset()
```

Stable binding types keep source meaning local, avoid whole-program widening,
and fit Seawitch's incremental compiler. Expression checking may infer an
expression's already-determined type, but assignment does not change the
declared destination type.

### Nullability remains independently implementable

RFC 0010 intentionally handles only pointer-like `P | Nil`. Such a union has a
spare null representation and therefore needs no general union layout.

This RFC builds the larger algebra around that working special case. It does
not make RFC 0010 wait for tagged unions, structured narrowing, or union
widening.

## Guide-level explanation

### Declaring a union alias

Use the existing transparent `type` declaration:

```seawitch
type Number = Int32 | Float64
type MaybeNumber = Number | Nil
```

Aliases do not make nominal wrappers. `Number` is another spelling for
`Int32 | Float64`, and `MaybeNumber` resolves to
`Int32 | Float64 | Nil`.

A transparent union alias retains its right-hand side's candidate order when
used as an expected type:

```seawitch
type Small = UInt16 | UInt8

value: Small = 7
// The active member is UInt16.
```

Aliases remain transparent for canonical type identity. Retaining candidate
order does not create a new nominal type.

### Declaring union-valued bindings

Every new binding retains its mandatory annotation:

```seawitch
score: Int32 | Nil = nil
mut input: Int32 | Float64 = 1
```

Any member value can initialize or be assigned to the union:

```seawitch
mut input: Int32 | Float64 = 1
input = 2.5
```

The active member changes from `Int32` to `Float64`; the binding's static type
remains `Int32 | Float64`.

### Contextual expressions use written order

RFC 0003 numeric literals and RFC 0009 operations over untyped literals can
receive an expected numeric type. A union destination supplies its members as
candidate expected types in written order. The first candidate under which the
whole expression checks successfully becomes the active member:

```seawitch
a: UInt8 | Nil = 7
// UInt8 succeeds; Nil does not accept an integer literal.

b: Int32 | Int64 = 7
// Int32 succeeds first.

c: UInt8 | UInt16 = 7
// Both could hold 7, but UInt8 is written first.

d: Int64 | Bool = 2 + 2
// Int64 supplies one type to both operands and the result.
```

This is strict priority, not ambiguity resolution. Reversing viable members
can change the selected active member:

```seawitch
first: Float32 | Float64 = 0.1
second: Float64 | Float32 = 0.1

first_is_float32: Bool = first is Float32   // true
second_is_float64: Bool = second is Float64 // true
```

The same candidate order remains attached to the declared destination place
for later contextual assignments:

```seawitch
mut value: UInt8 | UInt16 = 7
value = 8
// UInt8 is selected both times.
```

An expression whose static type is already determined does not undergo this
candidate search. It uses ordinary injection: an identical member is preferred
before a permitted conversion such as pointer weakening.

### No implicit declaration widening

This is invalid:

```seawitch
mut input: Int32 = 1
input = 2.5
// Type Error: expected Int32, got Float64
```

The compiler does not silently change `input` into `Int32 | Float64`. The
source must state that contract:

```seawitch
mut input: Int32 | Float64 = 1
```

This is a permanent language rule, not a limitation deferred by the first
union implementation. Seawitch does not infer binding declarations, collect
assigned types, or retroactively widen storage.

### Pointer grouping stays explicit

Type constructors accept complete type expressions:

```seawitch
MutPtr<Int32> | Nil
// A nullable pointer to an Int32.

MutPtr<Int32 | Nil>
// A non-null pointer to a slot containing Int32 or Nil.

MutPtr<MutPtr<Int32> | Nil> | Nil
// A nullable pointer to a nullable pointer slot.
```

Angle brackets and parentheses determine grouping. `|` never moves across a
type-constructor boundary.

### Function positions

Where RFC 0008 permits a type, that position accepts a union type expression:

```seawitch
type ReadError = {
    code: Int32,
}

fun read_number(): Int32 | ReadError
    -- body
end

handler: Fun<(Int32 | Nil) : Bool> = accepts_optional
```

This rule generalizes type parsing; it does not by itself enable a `Fun<...>`
position that RFC 0008 currently defers.

### Narrow before member-specific use

A union does not implicitly expose operations from any one member:

```seawitch
value: Loaded | Failed = load_asset()
value.asset
// Type Error: narrow Loaded | Failed before member access
```

Use the implemented structured conditionals to narrow before access:

```seawitch
if value is Loaded
    use_asset(value.asset)
else
    -- Loaded was removed by the false path, so value is Failed here.
    report_error(value.error)
end
```

An `elseif` chain can narrow larger unions one exact member at a time. A final
`else` receives the normalized remainder, so an explicit exhaustive chain
needs no separate pattern syntax.

RFC 0010's focused `Nil` checks remain available:

```seawitch
maybe: Int32 | Nil = read_score()

if maybe != nil
    -- maybe is Int32 here
end
```

### Testing one member with `is`

Use `is` when code needs to ask whether one exact non-`Nil` member is active:

```seawitch
value: Int32 | Float64 = read_value()

if value is Int32
    -- value is Int32 here
end
```

The false branch removes the tested member:

```seawitch
value: Int32 | Float64 = read_value()

if value is Int32
    -- value is Int32
else
    -- value is Float64
end
```

`is` is an exact runtime-type test. It does not ask whether one type converts
to another:

```seawitch
pointer: MutPtr<Node> | Ptr<Node> = read_pointer()

if pointer is Ptr<Node>
    -- Only the Ptr<Node> member matches. An active MutPtr<Node> does not.
end
```

Nullability keeps its existing spelling:

```seawitch
if maybe == nil
    -- absent
end
```

`maybe is Nil` is rejected. `Nil` is tested with `== nil` or `!= nil`, giving
null checks one source spelling.

For a union containing only `T | Nil`, `value is T` is also rejected because
it is exactly the same proof as `value != nil`. General unions may still test a
specific non-`Nil` member:

```seawitch
value: Int32 | Float64 | Nil = read_optional_number()

if value is Int32
    -- value is Int32
end
```

### Equality between identical union types

`==` and `!=` are available when both operands have the same canonical union
type and every member supports equality:

```seawitch
left: Int32 | Bool = read_left()
right: Bool | Int32 = read_right()

same: Bool = left == right
different: Bool = left != right
```

Written member order and transparent aliases do not affect whether the operand
types are identical. Candidate order can still affect which active member an
earlier contextual initializer selected.

Two union values are equal exactly when:

1. they have the same active member; and
2. their active payload values are equal under that member type's `==` rule.

Different active members compare unequal even if their payload bits happen to
match:

```seawitch
integer: Int32 | Float64 = 1
floating: Float64 | Int32 = 1.0
same: Bool = integer == floating // false: different active members
```

Equality does not widen either operand:

```seawitch
small: Int32 | Bool = read_value()
wide: Int32 | Bool | Nil = read_optional_value()
same: Bool = small == wide
// Type Error: union equality requires identical operand types
```

Ordering remains unavailable:

```seawitch
ordered: Bool = left < right
// Type Error: union types are not ordered
```

### Widening and narrowing

A union may widen to a union containing every possible source member:

```seawitch
small: Int32 | Bool = read_value()
wide: Int32 | Bool | Nil = small
```

The reverse conversion is not implicit:

```seawitch
wide: Int32 | Bool | Nil = read_optional_value()
small: Int32 | Bool = wide
// Type Error: Int32 | Bool | Nil is not assignable to Int32 | Bool
```

Narrowing requires control-flow proof. A cast that merely asserts the active
member is outside this RFC.

## Reference-level specification

### Type-expression grammar

The common grammar is extended conceptually as follows:

```ebnf
type-expression
    = union-type-expression ;

union-type-expression
    = primary-type-expression
    , { "|" , primary-type-expression } ;

primary-type-expression
    = identifier
    | pointer-type-expression
    | function-type-expression
    | "(" , type-expression , ")" ;

pointer-type-expression
    = "Ptr" , "<" , type-expression , ">"
    | "MutPtr" , "<" , type-expression , ">" ;
```

RFC 0008 supplies `function-type-expression`. Future generic type constructors
use the same rule: every type argument is a `type-expression` unless that
constructor explicitly declares a different argument kind.

`|` has the lowest precedence in type grammar. Constructor delimiters and
parentheses group nested expressions:

```seawitch
Ptr<Int32> | Nil
Ptr<Int32 | Nil>
(Int32 | Float64) | Nil
```

The third spelling is accepted and normalizes to `Int32 | Float64 | Nil`.

### `is` expression grammar

RFC 0009's equality grammar gains one tighter type-test level:

```ebnf
equality-expression
    = type-test-expression
    , { ( "==" | "!=" ) , type-test-expression } ;

type-test-expression
    = relational-expression
    , [ "is" , type-expression ] ;
```

`is` becomes a reserved word. It is not a method name and takes a type
expression on its right, not a runtime expression.

The optional production prevents an unparenthesized chain of type tests:

```seawitch
value is Int32 is Bool
// Syntax Error: an is test cannot be chained
```

`is` binds more tightly than `==`, `!=`, `and`, and `or`, and less tightly than
relational, arithmetic, unary, and postfix operations:

```seawitch
tested: Bool = value is Int32 == expected
// (value is Int32) == expected
```

Parentheses may always make the grouping explicit.

### Object type-expression restriction

RFC 0006's object body remains a nominal definition form allowed only as the
direct right-hand side of `type Name =`:

```seawitch
type Point = {
    x: Int32,
    y: Int32,
}
```

This RFC does not turn `{ ... }` into an anonymous structural type:

```seawitch
point: { x: Int32, y: Int32 }
// Syntax Error: expected a type name
```

This is a deliberate language decision, not a feature deferred by the union
implementation. Every object type has a declared nominal identity and every
object value names that type. Two object declarations remain different types
even when their member names, types, order, and mutability are identical.

Anonymous object syntax is invalid in annotations, aliases, pointer elements,
function signatures, object members, and union members:

```seawitch
type Pair = { x: Int32 } | Nil
// Syntax Error: an object type body must be the complete right-hand side

value: Ptr<{ x: Int32 }>
// Syntax Error: expected a type name
```

These are parser-owned errors. `object-type-expression` is deliberately absent
from `primary-type-expression`; the parser accepts it only as the complete
right-hand side of one `type Name = { ... }` declaration. The checker never
receives an anonymous object type node in another type position.

Once named, the nominal object type may be a union member:

```seawitch
type MaybePoint = Point | Nil
```

### Valid union members

A union member must be a complete, storable value type at the point where the
union is formed. Current valid categories are:

- scalar types;
- `Nil`;
- complete nominal object types;
- `Ptr<T>` and `MutPtr<T>` when their pointer construction is valid;
- stored `Fun<...>` values in positions supported by RFC 0008; and
- another union, which is flattened before validation completes.

`Unknown` is incomplete and is never a value member:

```seawitch
value: Unknown | Nil
// Type Error: Unknown has no value representation; use a pointer to Unknown
```

`Ptr<Unknown>` and `MutPtr<Unknown>` are complete pointer values and are valid
members.

A no-result function call produces no value and therefore cannot be a union
member. RFC 0010's distinction between `Nil`, no result, and `Unknown` remains
unchanged.

### Formation, candidate order, and normalization

Resolving a written union type use produces both:

1. an ordered contextual view retained by that type use; and
2. an order-independent canonical type used for identity and representation.

To form the contextual candidate order:

1. walk written members from left to right;
2. recursively flatten nested union expressions in that order;
3. expand a transparent union alias using the retained candidate order of its
   right-hand side; and
4. when the same canonical member occurs more than once, retain its first
   occurrence and remove later occurrences.

For example:

```seawitch
type Integer = Int64 | Int32
type OptionalInteger = Nil | Integer

value: OptionalInteger = 7
```

The candidate order is `Nil`, `Int64`, `Int32`. `Nil` cannot accept the
integer expression, so `Int64` is selected.

To form the canonical union type from those members:

1. resolve every member expression to its canonical type;
2. recursively flatten every resolved union member;
3. replace transparent aliases with their canonical targets;
4. remove duplicate canonical member identities;
5. validate every remaining member as a complete storable value type; and
6. if one member remains, use that member type directly; otherwise intern the
   canonical member set as one union type.

Member order does not participate in identity:

```text
U(A, B) == U(B, A)
```

It does participate in contextual candidate selection:

```seawitch
left: A | B = contextual_expression()
right: B | A = contextual_expression()
```

`left` tries `A` before `B`; `right` tries `B` before `A`. Their static types
still have one canonical identity.

Grouping does not participate in identity:

```text
U(U(A, B), C) == U(A, U(B, C)) == U(A, B, C)
```

Duplicate membership has no effect:

```text
U(A, A) == A
```

This includes aliases:

```seawitch
type Score = Int32
type ScoreOrInteger = Score | Int32
```

`ScoreOrInteger` is exactly `Int32`, not a two-member union.

RFC 0010's canonical nullable spelling remains preferred in documentation:

```seawitch
Ptr<Node> | Nil
```

After this RFC, `Nil | Ptr<Node>` is also accepted and denotes the same type.

### Canonical identity and interning

A union's canonical identity is determined only by the duplicate-free set of
canonical member identities. Display spellings, source order, aliases, and
generated C names never participate in equality or interning.

Candidate order is therefore not stored as part of the interned canonical
union. A resolved type use carries its canonical identity plus the contextual
candidate view needed by that written destination. This view is retained
recursively through constructed type uses and is stored for declared places
such as bindings, object members, parameters, returns, and pointer pointees.
For example, assignment through `MutPtr<UInt16 | UInt8>` tries `UInt16` before
`UInt8` when checking a contextual value for `.value`.

Union interning is compilation-scoped, following RFC 0005. A long-running
compiler or workbench must not reuse a union containing a stale nominal type
identity from an earlier compilation.

Constructed types use the union's canonical identity normally:

```seawitch
type A = Int32 | Float64
type B = Float64 | Int32

first: Ptr<A>
second: Ptr<B>
```

`A` and `B` resolve to one union identity, so `Ptr<A>` and `Ptr<B>` resolve to
one pointer identity.

### Display spelling

Diagnostics print a deterministic normalized member order. This display order
is independent of both contextual candidate order and canonical identity.

The compiler must use one stable ordering rule within a compilation and across
repeated compilations of unchanged source. The order is:

1. built-ins in this fixed order: `Bool`, `UInt8`, `UInt16`, `UInt32`,
   `UInt64`, `Int8`, `Int16`, `Int32`, `Int64`, `Float32`, `Float64`;
2. nominal types in declaration order;
3. constructed types by constructor order `Ptr`, `MutPtr`, then `Fun`, with
   equal constructors ordered recursively by their canonical components; and
4. `Nil` last.

Future built-in or constructed type RFCs must place their type category in
this display order. That addition affects diagnostic spelling only, never
candidate selection or canonical union identity.

Examples:

```text
Int32 | Float64 | Nil
Point | LoadError
MutPtr<Node> | Nil
```

The implementation must not sort by generated C text or an alias spelling.

### Assignability and injection

There are two source-expression cases.

For a context-dependent expression whose type cannot be completed without an
expected type, the destination's retained candidate order is authoritative:

1. check the complete expression using the first member as its expected type;
2. if checking succeeds, select that member and stop;
3. otherwise discard only that candidate's expected-type-dependent provisional
   diagnostics and try the next member; and
4. if no member succeeds, report one union-context type error, with the
   candidate failures available as supporting diagnostic detail.

Before or during candidate checking, an error whose truth does not depend on
the expected member remains authoritative. Unknown names, malformed calls,
invalid member access, unsupported syntax, and equivalent context-independent
errors are reported once at their ordinary earliest phase. They are never
replaced by a generic union mismatch:

```seawitch
result: UInt8 | UInt16 = missing + 1
// Type Error: unknown value missing
```

Literal range failures, an operator rejecting a candidate operand type, and
other failures caused specifically by the candidate expected type are
provisional. The checker must not commit partial checked nodes, bindings,
conversions, or provisional diagnostics from a failed candidate attempt. If
all candidates fail, it reports one union-context error and may attach the
per-candidate failures as detail.

RFC 0003's `Int32` and `Float64` fallbacks apply only when no usable expected
type exists; they do not outrank a viable union member written earlier.

For an expression whose static source type is already determined independently
of the destination, ordinary injection and widening rules apply instead.

Let `members(T)` be `{T}` when `T` is not a union and the canonical member set
when `T` is a union.

A source type `S` is union-assignable to destination union `D` when every
member of `S` is assignable to at least one member of `D` under the ordinary
non-union assignability rules.

Destination selection for an already-typed source uses this fixed ranking:

1. an identical canonical member is preferred; then
2. an ordinary permitted conversion, currently pointer weakening, is used.

Two candidates at the same best rank are ambiguous. Exact-type preference
prevents a written candidate order from silently weakening an already-typed
pointer:

```seawitch
writer: MutPtr<Node> = ref node
choice: Ptr<Node> | MutPtr<Node> = writer
// The active member is MutPtr<Node>, the exact match.
```

To select the weakened member, first establish that member type explicitly:

```seawitch
reader: Ptr<Node> = writer
choice: MutPtr<Node> | Ptr<Node> = reader
// The active member is Ptr<Node>.
```

This produces two common cases:

1. **member injection:** `S` is one member assignable to `D`; and
2. **union widening:** every member of union `S` is accepted by union `D`.

Ordinary pointer weakening may happen while matching a source member to a
destination member:

```seawitch
writer: MutPtr<Node> = ref node
maybe_reader: Ptr<Node> | Nil = writer
```

This applies RFC 0007's `MutPtr<Node>` to `Ptr<Node>` weakening and then
injects the result. Union formation does not add any new strengthening rule.

A union never implicitly converts to one member merely because that member is
the expected type. The source must first be narrowed by control flow.

### No inferred union declarations

Every new binding requires an explicit annotation under the existing language
rule. Union types do not introduce an exception:

```seawitch
value = 42
// Missing Type Error: declaration requires a type annotation
```

The annotation fixes the binding's complete static type before its initializer
is checked:

```seawitch
mut value: Int32 = 42
value = true
// Type Error: expected Int32, got Bool
```

The checker must not revisit the declaration and change it to
`Int32 | Bool`. Assignments in control-flow branches follow the same rule:

```seawitch
mut value: Int32 = 42

if condition
    value = true
    // Type Error: expected Int32, got Bool
end
```

The intended union must be written at the declaration:

```seawitch
mut value: Int32 | Bool = 42

if condition
    value = true
end
```

The compiler may determine the static type of an expression while checking it
against an explicit destination:

```seawitch
result: Int32 | Bool = choose_value()
```

That expression checking does not infer or widen the destination declaration.
Likewise, this RFC does not infer union-valued function returns, parameters,
or object members; their existing explicit type requirements remain.

### Ambiguous already-typed member conversion

Injection of an already-typed source must select exactly one best destination
member under the ranking above. If a future conversion system makes one source
member assignable to multiple destination members at the same best rank,
injection is ambiguous and fails unless that future specification defines a
more specific ranking. This does not affect contextual expressions, whose
written first-success rule is intentionally unambiguous.

### Mutation

Binding `mut` controls replacement of the whole union value exactly as it
controls replacement of any other value:

```seawitch
fixed: Int32 | Nil = nil
fixed = 1
// Mutability Error: fixed is not mutable

mut changing: Int32 | Nil = nil
changing = 1
```

`mut` is not part of union identity and does not recursively change union
members, object members, or pointer pointees.

A control-flow narrowing changes only the binding's visible type within that
branch or loop body. It does not create a second binding or change whether the
original binding is mutable.

### Operations on union values

The initial rule is deliberately uniform: an operation defined for a member
type is unavailable on the union until narrowing proves that member active.

This includes:

- object member access;
- `.value` pointer dereference;
- arithmetic;
- ordering;
- calls when only some members are callable; and
- method dispatch.

Three union-level operations do not require prior narrowing:

- RFC 0010's `== nil` and `!= nil` tests for unions containing `Nil`;
- this RFC's exact `is` active-member test; and
- this RFC's `==` and `!=` between identical union types when every member
  supports equality.

These inspect the union representation itself. They do not dispatch one source
operation independently across every member.

This RFC does not adopt Crystal's rule that a call is allowed directly on a
union when every member responds. A later method or interface RFC may add
common-operation dispatch without changing union identity or representation.

### Exact `is` type testing

For `expression is T`:

1. `expression` must have a canonical multi-member union type `U`;
2. `T` is resolved and normalized as an ordinary type expression;
3. `T` must resolve to exactly one non-union canonical type;
4. that canonical type must be an exact member of `U`;
5. the test evaluates `expression` exactly once;
6. the result has type `Bool`;
7. the result is `true` exactly when `T` is the active member; and
8. assignability, pointer weakening, aliases, and representation compatibility
   do not make a different active member match `T`.

A transparent alias resolving to one member is accepted:

```seawitch
type Count = Int32
value: Int32 | Float64 = read_value()
integer_active: Bool = value is Count
```

An alias resolving to a union is not one exact member:

```seawitch
type Number = Int32 | Float64
value: Number | Bool = read_value()
number_active: Bool = value is Number
// Type Error: is requires one exact member type; Number is a union
```

Testing a type outside the static union is an error rather than a constant
`false` result:

```seawitch
value: Int32 | Float64 = read_value()
boolean_active: Bool = value is Bool
// Type Error: Bool is not a member of Int32 | Float64
```

Testing a non-union value is an error rather than a constant `true` or `false`
result:

```seawitch
value: Int32 = 1
integer_active: Bool = value is Int32
// Type Error: is requires a union value
```

`T` may not be `Nil`. RFC 0010's value comparisons are the sole null-test
syntax:

```seawitch
maybe: Int32 | Nil = read_score()
missing: Bool = maybe == nil
present: Bool = maybe != nil
```

If `U` contains exactly `T` and `Nil`, `expression is T` is rejected as a
duplicate spelling of `expression != nil`. This restriction applies after
alias normalization.

An `is` expression can test any expression, but control-flow narrowing is
available only when the condition tests a stable local binding directly:

```seawitch
if value is Int32
    -- value is Int32 here
end
```

In the true branch, the binding's visible type is `T`. In the false branch,
its visible type is `U` with `T` removed and normalized. Reassigning the
binding invalidates that narrowing. Member paths, pointer dereferences, and
calls are not stable narrowing subjects; snapshot them into a local binding
first, following RFC 0010's nullability rule.

RFC 0015 `elseif` clauses accumulate the false-path narrowing from preceding
clauses:

```seawitch
value: Int32 | Float64 | Nil = read_optional_number()

if value is Int32
    use_integer(value) -- value is Int32
elseif value != nil
    use_float(value)   -- value is Float64
else
    -- value is Nil: the two non-Nil members were removed.
    report_missing()
end
```

Each condition retains RFC 0015's source-order, single-evaluation semantics.
RFC 0010 null tests participate in the same accumulated narrowing: after the
first false path above, `value` is `Float64 | Nil`, so `value != nil` narrows
the second body to `Float64`. A repeated type test is rejected when preceding
false paths have already removed that member from the binding's visible type.

An exact test may also be a `while` condition. The loop body sees the tested
member because the condition was true on entry to that iteration:

```seawitch
mut state: Running | Stopped = current_state()

while state is Running
    tick(state) -- state is Running here
    state = next_state()
end
```

No narrowing survives after the loop. RFC 0015 permits zero iterations and
`break`, so reaching the following statement does not prove the condition
false. Reassignment inside the body invalidates its earlier narrowing from the
assignment onward.

### Narrowing, storage, and address escape

Narrowing changes how a binding is read; it never changes that binding's
declared storage type. For a binding declared as `Loaded | Failed`:

- a read in a `Loaded` branch has visible type `Loaded`;
- assignment still targets `Loaded | Failed` and may install either member;
- `ref binding` addresses the complete `Loaded | Failed` storage, never only
  its currently active payload; and
- a successful assignment restores the full declared type for later reads in
  that branch.

```seawitch
mut result: Loaded | Failed = load_asset()

if result is Loaded
    result = Failed { error = make_error() }
    -- result is Loaded | Failed again from this statement onward.
end
```

A direct local binding is narrowable only while its storage cannot be replaced
through a writable alias. The checker records whether the binding's address has
escaped with write capability:

1. producing or storing a `MutPtr` to the binding marks it mutably
   address-escaped;
2. weakening the address to a read-only `Ptr` before it escapes does not mark
   it mutably address-escaped;
3. a mutably address-escaped binding cannot later be narrowed; and
4. creating a writable address escape while the binding is narrowed
   immediately invalidates that narrowing.

This rejects the unsound case where another pointer replaces the active member
after the tag test:

```seawitch
mut result: Loaded | Failed = load_asset()
writer: MutPtr<Loaded | Failed> = ref result

if result is Loaded
    // Type Error: result cannot be narrowed after its mutable address escapes
end
```

Use a fixed snapshot when aliasable mutable storage must be inspected:

```seawitch
snapshot: Loaded | Failed = result

if snapshot is Loaded
    use_asset(snapshot.asset)
end
```

A read-only pointer does not invalidate narrowing because it cannot replace the
observed storage. The generated code must never reinterpret the address of the
whole tagged union as the address of one payload member.

Narrowing is branch-local. At the closing `end`, the binding returns to its
declared type even when an early `return`, `break`, or `continue` would make a
stronger post-block inference possible. The first implementation performs no
post-conditional join narrowing.

### Equality for identical union types

For `left == right` or `left != right` where either operand is a union:

1. both operands must have the same canonical union identity;
2. neither operand is widened or narrowed for the comparison;
3. every canonical member must support the requested equality operation under
   its existing specification;
4. each operand is evaluated exactly once under RFC 0009's binary operand
   evaluation rule;
5. different active tags compare unequal under `==` and equal under `!=`;
6. matching active tags compare their payloads with that member's equality
   rule; and
7. matching `Nil` members compare equal without reading a payload.

`!=` is the logical complement of `==`; a member type does not need a separate
union-specific implementation.

Scalar members inherit RFC 0009 equality, including IEC 60559 NaN behavior for
floating members. An object member prevents union equality until object
equality is specified. A pointer member prevents union equality until general
pointer equality is specified. RFC 0010's focused comparison against `nil`
does not by itself make two pointer values equality-comparable.

Example:

```seawitch
first: Int32 | Float64 = read_first()
second: Float64 | Int32 = read_second()
same: Bool = first == second
```

If both active members are `Int32`, their `Int32` payloads are compared. If
both are `Float64`, RFC 0009 floating equality is used. If their active members
differ, the result is `false` without comparing payloads.

Union ordering is always rejected by this RFC, even if every member is
individually ordered. There is no canonical ordering between different active
member types.

### Exhaustive structured narrowing

An `if`/`elseif`/`else` chain is exhaustive because RFC 0015 executes exactly
one selected branch and the final `else` covers the normalized remainder. No
new pattern or branch syntax is required by this RFC.

This does not add a compile-time requirement that every conditional over a
union be exhaustive. A caller may intentionally test one member and continue
with the original union after the conditional. Exhaustiveness is obtained when
the source writes a final `else` and uses the branch-local narrowed views.

### Recursive aliases

RFC 0005's prohibition on self-referential transparent aliases remains:

```seawitch
type Json = Nil | Bool | List<Json>
// Type Error: transparent alias Json cannot reference itself
```

Crystal permits recursive union aliases, but Seawitch defers them. Their
validity depends on generic container representation, finite-layout analysis,
and recursion checking that do not yet exist.

Nominal pointer recursion remains valid under RFC 0007:

```seawitch
type Node = {
    mut next: MutPtr<Node> | Nil,
}
```

Direct by-value layout cycles remain invalid whether or not a union appears in
the cycle.

## C23 lowering

### Representation selection

After normalization, the generator chooses exactly one of three forms:

1. one remaining member: use that member's existing representation;
2. exactly one pointer-like member plus `Nil`: use RFC 0010's null niche; or
3. every other multi-member union: use an inline tagged representation.

The niche rule does not apply to a union with two different pointer members:

```seawitch
Ptr<First> | Ptr<Second> | Nil
```

Non-nullness distinguishes either pointer from `Nil`, but the pointer bits do
not identify whether the active non-null member is `Ptr<First>` or
`Ptr<Second>`. The union therefore needs a tag.

### Tagged representation

Conceptually, this Seawitch type:

```seawitch
Int32 | Float64
```

lowers to one file-scope C23 tag and payload type:

```c
typedef enum {
    sw_internal_union_1_int32,
    sw_internal_union_1_float64
} sw_internal_union_tag_1;

typedef struct {
    sw_internal_union_tag_1 tag;
    union {
        int32_t member_1;
        double member_2;
    } payload;
} sw_internal_union_1;
```

The example spellings are illustrative. The generated-name implementation
must follow RFC 0004's separation between source-derived names and
compiler-created helper names.

Compiler-created union names use deterministic per-compilation ordinals
assigned by first encounter during structured type dependency discovery. They
must not contain hashes, truncated source names, collision retries, or
generator state that survives the compilation.

A comment may print the normalized Seawitch type above the generated helper so
the C remains readable:

```c
/* Seawitch: Int32 | Float64 */
```

### Tag and payload rules

Every tagged union has:

- one tag value for every canonical member;
- one payload field for every member that carries data;
- no payload field for `Nil`; and
- alignment and padding determined by the target C23 implementation from the
  emitted struct and union.

Tag values and payload fields follow the deterministic normalized display
order, never a destination's contextual candidate order. Two differently
ordered spellings of one canonical union therefore use the same tags and C
layout.

The compiler writes a payload member before or as part of setting the
corresponding active tag. Generated code reads a payload field only on a path
that has checked the matching tag. It never reads an inactive C union member.

Generated exhaustive switches must fail closed if an invalid internal tag is
observed. They must not use C undefined behavior as the fallback. The concrete
runtime failure helper belongs to the implementation plan.

### Member injection

Injection constructs the destination representation directly. It performs no
allocation.

For a tagged destination, conceptual lowering is:

```c
(sw_internal_union_1){
    .tag = sw_internal_union_1_int32,
    .payload.member_1 = INT32_C(42),
}
```

For nullable pointer injection, RFC 0010 applies and the pointer bits are used
unchanged.

### Union widening

If source and destination tagged layouts differ, widening evaluates the source
once, switches on its active tag, and constructs the corresponding destination
member. Pointer weakening is performed inside the selected member when
required.

The source order of evaluation is preserved. Widening does not evaluate a
payload expression more than once.

If source and destination have the same canonical identity, no conversion is
emitted.

### `is` lowering

An accepted `is` test compares the evaluated union tag with the canonical tag
for the queried member:

```seawitch
integer_active: Bool = value is Int32
```

Conceptually:

```c
const bool sw_v_integer_active =
    sw_v_value.tag == sw_internal_union_1_int32;
```

If the source expression is not already a stable value, lowering first stores
it in one compiler-created temporary so it is evaluated exactly once. The
queried payload is not read.

RFC 0010 nullable-only unions do not need `is` lowering because their accepted
tests remain `== nil` and `!= nil`.

### Union equality lowering

Equality evaluates both operands once, compares their tags, and compares a
payload only when the tags match. Conceptually:

```c
if (left.tag != right.tag) {
    result = false;
} else {
    switch (left.tag) {
    case sw_internal_union_1_int32:
        result = left.payload.member_1 == right.payload.member_1;
        break;
    case sw_internal_union_1_bool:
        result = left.payload.member_2 == right.payload.member_2;
        break;
    default:
        /* Fail closed through the compiler's invalid-tag runtime path. */
    }
}
```

A `Nil` case assigns `true` without reading payload storage. `!=` negates the
complete `==` result. The generator reuses the member type's existing equality
lowering rather than duplicating scalar or future object rules.

Lowering may inline this control flow or call one compiler-created helper per
canonical union type. Whichever representation the implementation plan
selects must preserve single evaluation, deterministic generated C, existing
source mapping, and fail-closed invalid-tag handling.

### Header discovery and emission

Structured type dependency discovery must find unions used by:

- module and local bindings;
- object members;
- supported function parameters and returns;
- supported `Fun<...>` values;
- pointer element types; and
- other union members.

Discovery is cycle-safe and keyed by canonical type identity, not display
text. Each tagged union helper is emitted exactly once before C declarations
that require its complete layout.

The generated helper order is deterministic for unchanged source. Iterating a
Go map is not a valid emission-order rule.

### Source mapping

Compiler-created union helper definitions have no direct source statement and
remain generated scaffolding. Source-originating declarations, injections,
widenings, and union tests retain the containing statement's `#line` mapping
under the existing generator rules.

The generator restores generated-file mapping after every source-mapped
statement before emitting additional scaffolding.

## C interoperability

### Nullable pointers

RFC 0010's mappings remain exact:

| Seawitch | C23 |
|---|---|
| `Ptr<T> | Nil` | `const T *` |
| `MutPtr<T> | Nil` | `T *` |
| `Ptr<Unknown> | Nil` | `const void *` |
| `MutPtr<Unknown> | Nil` | `void *` |

These are ABI-transparent because the union uses the pointer's null niche.

### General unions

A general Seawitch tagged union has a Seawitch-owned layout. It is not
automatically compatible with a C `union`:

```c
union Number {
    int32_t integer;
    double floating;
};
```

A C union carries no runtime tag. Guessing its active member would violate
Seawitch's fail-closed and no-undefined-behavior goals.

Therefore:

- a general Seawitch union is not accepted directly by an imported C function;
- a general Seawitch union is not exported as a plain C union;
- importing C unions is a separate FFI feature with explicit active-member
  rules; and
- C-compatible tagged records may be modeled explicitly as nominal objects
  once layout and representation attributes exist.

The current compiler has no C import or export boundary at which to test these
rejections. They are normative obligations on the future FFI specification,
not acceptance criteria for this implementation. RFC 0010 nullable pointers
retain their already specified C pointer ABI independently.

## Diagnostics and phase ownership

The parser owns malformed type-expression syntax. The checker owns member
resolution, completeness, normalization, identity, assignability, widening,
and narrowing legality. The generator receives only valid structured union
operations and owns representation rendering.

Representative diagnostics are:

```text
[Syntax Error] expected a type after |
[Syntax Error] an object type body must be the complete right-hand side
[Syntax Error] expected a type name
[Type Error] unknown type LoadError
[Type Error] Unknown has no value representation; use a pointer to Unknown
[Type Error] expected Int32, got Bool
[Type Error] Int32 | Bool | Nil is not assignable to Int32 | Bool
[Type Error] narrow Loaded | Failed before member access
[Type Error] is requires a union value; got Int32
[Type Error] Bool is not a member of Int32 | Float64
[Type Error] is requires one exact member type; Number is a union
[Type Error] result cannot be narrowed after its mutable address escapes
[Type Error] use == nil or != nil to test the Nil member
[Type Error] use != nil to test the non-Nil member of Int32 | Nil
[Type Error] union equality requires identical operand types;
             got Int32 | Bool and Int32 | Bool | Nil
[Type Error] equality is unavailable because member Point does not support ==
[Type Error] union types are not ordered
[Type Error] expression does not match any member of UInt8 | Bool
[Type Error] transparent alias Json cannot reference itself
```

An unsupported or malformed checked union reaching generation is an `Unknown
Error`. The generator must not emit a placeholder type, raw C union, or
silently omitted arm.

## Interaction with existing specifications

### RFC 0005: aliases

Union aliases are transparent and compilation-scoped. Recursive transparent
aliases remain invalid. Canonical identity never depends on an alias name.

### RFC 0006: objects

Nominal object types may be union members. Object bodies remain declaration-only
nominal forms; anonymous structural objects remain unsupported. By-value
layout cycles remain invalid.

### RFC 0007: pointers and mutability

Pointer types may contain unions and may themselves be union members. Ordinary
outermost `MutPtr<T>` to `Ptr<T>` weakening may precede union injection.
Binding `mut`, member `mut`, and pointer constructor choice remain independent
of union membership.

### RFC 0008: functions

Union type expressions are accepted only in function positions RFC 0008
otherwise supports. This RFC does not create closures, nested functions,
addressable function declarations, or new `Fun<...>` storage positions.

### RFC 0009: operators

Existing arithmetic and ordering operators do not distribute over a union.
The operand must be narrowed first. This RFC extends `==` and `!=` to operands
with one identical canonical union type when every member supports equality.
`Nil` comparison retains the focused RFC 0010 rule. `is` adds a type-test
precedence level immediately above RFC 0009 equality.

### RFC 0010: Nil

`Nil` becomes an ordinary member of any complete value union. The nullable
pointer niche and non-null meaning of plain pointer types remain unchanged.
RFC 0010 may ship first; this RFC later removes only its temporary restrictions
on the `|` type form. General-union narrowing retains RFC 0010's direct-local,
declared-storage, writable-address-escape, and branch-local restoration rules.

### RFC 0015: structured control flow

RFC 0015 supplies the `if`/`elseif`/`else` and `while` blocks used for
narrowing. This RFC adds branch-local type refinement to direct stable-local
`is` conditions without changing RFC 0015's scopes, condition evaluation
order, loop execution, `break`, or `continue` behavior.

## Drawbacks

General unions require a tag and maximum-sized inline payload when no safe
niche exists. This increases value size and introduces a tag branch when the
active member is consumed or widened.

Structural union identity requires compilation-scoped canonical member sets
and deterministic helper discovery. It is more compiler work than a dedicated
`Optional<T>` wrapper.

Order-independent identity plus written first-success selection requires a
resolved type-use view in addition to the canonical type. Contextual candidate
checking must isolate expected-type-dependent failures, and sound narrowing of
mutable locals requires one writable-address-escape fact per binding. These are
deliberate checker costs of retaining concise source syntax without permitting
inactive-payload reads.

Requiring explicit narrowing is more verbose than Crystal's common-operation
dispatch. It provides one initial rule and avoids implicitly multiplying calls
across unrelated member types.

Transparent aliases cannot distinguish two domain cases with the same payload
type. Users must wrap those cases in nominal objects:

```seawitch
type Success = { value: Int32 }
type Failure = { value: Int32 }
type Result = Success | Failure
```

## Alternatives considered

### Keep `|` permanently limited to pointer nullability

Rejected. It would make the syntax look general while forcing every other
choice type into a different mechanism.

### Add `Optional<T>` and `Result<T, E>` first

Rejected. They overlap with `T | Nil` and `T | E`, expanding the core surface
before a need for distinct container behavior exists.

### Infer binding unions from assignments

Rejected permanently. A binding's explicit annotation is its stable contract.
Its initializer, later assignments, and assignments in control-flow branches
must not make the type depend on use sites or retroactively change its storage
representation.

### Make written order part of canonical identity

Rejected. `A | B` and `B | A` still describe the same set of possible runtime
types and use one representation. Making them different canonical types would
make order affect assignability, pointer identity, equality eligibility, and
ABI. This RFC instead retains written order only as contextual candidate
priority.

### Reject contextual expressions accepted by multiple members

Rejected. Crystal's ambiguity rule is defensible, but Seawitch already reads
`or`-like choices from left to right and values a simple deterministic rule.
First-success selection makes the annotation state its preference directly:

```seawitch
value: UInt8 | UInt16 = 7
// UInt8 is selected.
```

Changing that priority requires changing the written order, which is visible
at the declaration rather than delegated to a global numeric ranking.

### Require explicit member constructors

Rejected for structural unions. The source member's static type already
identifies the case. Named constructors belong to a possible future nominal
variant feature.

### Allow common operations directly on unions

Deferred. Crystal permits a method when every member responds and unions the
result types. Seawitch initially requires narrowing, which is simpler to read,
check, and lower. A method or interface RFC may revisit this without changing
the type model.

### Represent every union as a pointer

Rejected. It would allocate or impose indirection, violate value semantics,
and add runtime overhead unrelated to the source types.

### Use C `union` without a tag

Rejected. C provides no reliable active-member information. Reading the wrong
member can be undefined behavior and cannot implement exhaustive narrowing.

### Hash member names into generated C identifiers

Rejected. Deterministic compilation-local ordinals plus readable comments are
sufficient. Hashing adds machinery and obscures generated C.

### Implement recursive Crystal-style aliases immediately

Rejected. Recursive union aliases depend on generic representation and finite
layout rules not established by this RFC.

### Add anonymous structural object types

Rejected. Seawitch objects are nominal. If two written object shapes were the
same type, the language would need structural identity, structural
assignability, member-mutability compatibility, anonymous C type naming, and
rules for attaching methods. If each occurrence instead created a fresh type,
identical-looking annotations would be incompatible and difficult to name in
diagnostics.

The named form is explicit and maps directly to one C structure:

```seawitch
type Point = {
    x: Int32,
    y: Int32,
}

point: Point = Point {
    x = 10,
    y = 20,
}
```

A future tuple feature may provide deliberately structural product values. It
must not silently change object types from nominal to structural.

### Permit `value is Nil`

Rejected. RFC 0010 already gives null tests the explicit value spellings
`value == nil` and `value != nil`. A second spelling would add no capability.

### Widen operands for union equality

Rejected. Equality must not silently construct a larger tagged value merely to
compare it. Identical canonical operand types keep the operation visible and
mechanical; callers can assign both values to an explicitly chosen wider type
first when that is genuinely intended.

## Outside this RFC

- named algebraic variants and user-declared case constructors;
- recursive transparent aliases;
- common-method or common-member dispatch across unions;
- user-visible runtime type reflection;
- user-controlled tag values, payload layout, or ABI attributes;
- automatic compatibility with C `union` declarations;
- niche optimization beyond RFC 0010's pointer-like `P | Nil` case;
- ownership, destruction, and drop behavior for future managed members;
- generic variance and specialization over union members; and
- a future dedicated `match` expression or general pattern syntax.

## Open items before implementation

No known semantic design item remains open. Written first-success order applies
when an expression needs an expected type to complete checking. An already-
typed source instead prefers an exact union member before any permitted
conversion, preventing pointer weakening caused only by union order.

RFC 0015 supplies the control-flow surface required for narrowing; a future
`match` feature is not a dependency.

## Acceptance criteria

Implementation is complete when tests prove that:

1. ordinary supported type positions parse the same nested type-expression
   grammar;
2. `A | B`, `B | A`, and differently grouped forms resolve to one canonical
   union identity;
3. aliases and nested unions expand left to right for contextual candidate
   order, and duplicate candidates retain their first occurrence;
4. resolved type uses retain contextual candidate order recursively through
   aliases, constructed types, and declared destination places without putting
   that order into canonical identity;
5. an untyped contextual expression is checked against candidates from left to
   right and selects the first member under which the complete expression is
   valid, including literals and operator trees;
6. reversing two viable written candidates can change the selected active
   member but never canonical identity, layout, or generated tag assignment;
7. a failed candidate attempt commits no checked state or expected-type-
   dependent diagnostic, context-independent failures retain their earliest
   focused diagnostic, and no successful candidate produces one focused
   union-context type error;
8. an already-typed source prefers an identical destination member before an
   ordinary permitted conversion regardless of written candidate order;
9. aliases are expanded and duplicate canonical members are removed from
   canonical identity;
10. a one-member normalized union collapses to that member type;
11. scalar, object, pointer, stored function, and `Nil` members are accepted in
   otherwise supported positions, while the parser rejects anonymous object
   syntax in annotations, aliases, pointer elements, signatures, members, and
   union alternatives with a focused syntax diagnostic;
12. incomplete `Unknown`, no-result forms, and unsupported type positions fail
   with focused diagnostics;
13. a member value injects into a containing union without source syntax or
   allocation;
14. a source union widens only when every source member is assignable to a
   destination member;
15. pointer weakening may precede union injection but pointer strengthening is
   never introduced;
16. a union never implicitly narrows to one member;
17. every binding retains its mandatory explicit annotation, and neither its
    initializer, later assignments, nor branch assignments infer or widen its
    declared type;
18. binding `mut` controls whole-union replacement and does not alter union
    identity or member mutability;
19. member-specific access and operators other than the equality defined here
    fail until the value is narrowed;
20. direct stable-local `is` conditions narrow `if` and `elseif` true branches
    to the tested member, carry normalized false remainders into later clauses
    and `else`, compose with RFC 0010 null-test narrowing, reject members
    already removed by preceding clauses, and obey RFC 0015's evaluation and
    scope rules; reads use the narrowed type, assignment and `ref` use the
    declared storage type, assignment and writable address escape invalidate
    narrowing, a prior writable address escape prevents narrowing, and no
    narrowing survives the closing `end`;
21. a `while` condition using a direct stable-local `is` test narrows the loop
    body for each entered iteration, invalidates that view after reassignment,
    and provides no post-loop narrowing;
22. exactly `P | Nil` for one pointer-like `P` uses RFC 0010's one-word null
    niche, with no tag or wrapper;
23. every other multi-member union emits one deterministic inline tag-and-
    payload helper and performs no allocation;
24. tagged lowering never reads an inactive C union member and fails closed on
    an invalid internal tag;
25. union widening evaluates its source once and preserves member values and
    pointer capability;
26. structured dependency discovery finds nested unions and emits each helper
    once in deterministic dependency-safe order;
27. long-running compilations never reuse stale union or nominal identities;
28. generated C23 remains human-readable, contains no hash-based names, and
    preserves source `#line` mappings for source-originating statements;
29. every new syntax node, checked operation, type category, and generation
    case is handled explicitly or fails with a structured diagnostic;
30. `is` parses at the stated precedence, takes a type expression on its right,
    and cannot be chained without parentheses;
31. `is` accepts exactly one canonical non-`Nil` member of a static union,
    evaluates its expression once, and produces `Bool` from the active tag;
32. `is` rejects non-union subjects, union-valued queried types, non-member
    types, `Nil`, and the redundant non-`Nil` test of a two-member `T | Nil`;
33. `==` and `!=` accept two identical canonical union types exactly when every
    member supports equality, evaluate both operands once, compare tags first,
    and never read an inactive payload;
34. different active members compare unequal, matching `Nil` members compare
    equal, and matching payload members inherit their own equality semantics;
    and
35. union equality rejects implicit widening, unsupported member equality, and
    every ordering operator with focused diagnostics.

## Implementation handoff

Implementation should proceed in this order:

1. generalize the parser's RFC 0010 nullable type node into a recursive union
   type expression;
2. reserve `is` and add its expression-precedence level;
3. add compilation-scoped canonical union interning over member identities;
4. add resolved type-use views that retain contextual candidate order
   separately from canonical identity, including through aliases and nested
   constructed types;
5. add normalization and deterministic diagnostic display ordering;
6. add isolated left-to-right contextual candidate checking, then extend
   ordinary already-typed assignability with member injection and subset
   widening; preserve context-independent diagnostics outside speculative
   candidate failures;
7. add structured checked operations for injection, widening, `is`, equality,
   tag tests, and payload extraction;
8. implement representation selection, preserving RFC 0010's niche path;
9. add deterministic helper dependency discovery and C23 emission, including
   single-evaluation `is` and equality lowering;
10. extend RFC 0015 flow checking with direct-local `is` narrowing for
    `if`/`elseif`/`else` and `while`, declared-storage typing for assignment and
    `ref`, branch-local restoration, and per-binding writable-address-escape
    tracking;
11. add focused lexer/parser/checker/generator tests and behavior-named
   end-to-end compiler tests; and
12. only after behavior stabilizes, update `docs/grammar.md`,
    `docs/language.md`, and `docs/status.md` once.
