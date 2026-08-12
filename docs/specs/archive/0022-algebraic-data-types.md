# RFC 0022: Algebraic Data Types and Match Expressions

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-10
- Features: nominal closed ADTs, qualified variant constructors, unit
  variants, named record payloads, indirect recursion, exact type-pattern
  matching, value-pattern matching, implicit arm narrowing, and exhaustive
  expression-only `match`
- Created: 2026-08-09
- Revised: 2026-08-10
- Depends on: RFC 0005 (type identity and aliases), RFC 0006 (objects), RFC
  0014 (general type expressions and unions), RFC 0019 (generic types)
- Coordinates with: RFC 0010 (`Nil` and explicit nullability), RFC 0015
  (structured control flow), RFC 0018 (String and Rune values), and RFC 0024
  (equality, ordering, and hashability)

## Summary

RFC 0014 defines structural unions such as `Int32 | Float32`. Structural
unions combine existing types but do not provide named variants, qualified
constructors, or a closed nominal domain.

This RFC adds algebraic data types (ADTs) and the `match` expression that
consumes their closed variant set:

```seawitch
type Shape =
    | Circle as { r: Int32 }
    | Square as { a: Int32 }

shape: Shape = Shape.Circle { r = 10 }

area: Int32 = match shape is
    | Shape.Circle then shape.r * shape.r
    | Shape.Square then shape.a * shape.a
end
```

Variants are namespaced by their containing ADT. `Shape.Circle` is a
qualified variant constructor and pattern. It is not a globally visible value
named `Circle`, and it is not a subtype in the object-oriented inheritance
sense.

An ADT may contain unit variants with no payload:

```seawitch
type Direction =
    | East
    | West
    | North
    | South

heading: Direction = Direction.East
```

An all-unit ADT provides enum-like behavior without introducing a separate
C-style enum feature. Its values have no implicit integer representation or
conversion.

## Goals

1. Add one nominal, closed variant mechanism for sum types and enum-like
   domains.
2. Make constructors and patterns unambiguous through `Owner.Variant` names.
3. Reuse named record field syntax for variant payloads.
4. Define exhaustive `match` expressions in the same feature as their ADTs.
5. Distinguish exact type/variant matching from value matching with `is`.
6. Narrow a named scrutinee automatically inside each selected arm.
7. Support unit and named-record variants without empty structs or dummy
   payloads.
8. Support indirect recursive ADTs without runtime reflection.
9. Lower ADTs to a checked tagged representation without reading inactive
   payloads.

## Non-goals

This RFC does not define:

- C-style implicit integer enums;
- inheritance or subtype polymorphism;
- positional or tuple variants;
- direct field or positional destructuring in patterns;
- `as` payload bindings in match arms;
- wildcard `_` match arms; `else` is the only default arm;
- guards, OR-patterns, ranges, or negated patterns;
- numeric, floating, `Rune`, or `String` value patterns;
- union expressions as one match pattern; union members are matched separately;
- multi-statement match arms or match statements;
- variant-specific methods or implementation blocks;
- user-controlled numeric tags or ABI layout attributes;
- ownership, move, drop, allocator, or lifetime behavior for payloads;
- ordinary empty object types;
- runtime reflection or serialization schemas; or
- equality, ordering, or hashability rules owned by RFC 0024.

## ADT declaration syntax

An ADT declaration contains at least two distinct variants. A variant is either
unit-like or carries one named record payload:

```ebnf
adt-definition    = "|" , variant , { "|" , variant } ;
variant           = identifier , [ "as" , variant-payload ] ;
variant-payload   = "{" , member-declaration
                         , { "," , member-declaration } , [ "," ] , "}" ;
```

The declaration is the right-hand side of an existing type declaration:

```ebnf
type-declaration  = "type" , identifier , [ generic-parameters ] , "=" ,
                    adt-definition ;
```

The canonical spelling uses one leading pipe and one pipe before every
following variant. A variant payload must contain at least one field. An
empty payload is written as a unit variant instead.

```seawitch
type State =
    | Ready
    | Running as { progress: UInt8 }
```

Payload field declarations use RFC 0006's named-field syntax and type rules.
Payload fields are immutable in this RFC; a `mut` field modifier in an ADT
payload is deferred until mutation of variant payloads has a complete update
and ownership design.

Variant names must be unique within their owner. Different ADTs may each have
a variant with the same name:

```seawitch
type Shape = | Circle as { r: Int32 } | Square as { a: Int32 }
type Drawing = | Circle as { color: String } | Line as { length: Int32 }
```

`Shape.Circle` and `Drawing.Circle` remain distinct qualified names.

`type` declarations that are not ADT declarations retain the existing RFC
0005 and RFC 0014 meaning. A one-variant declaration is not an ADT and is
rejected by this RFC; use an object type when no sum is required.

## Nominal identity and variant meaning

An ADT is one nominal type. Every variant constructor produces a value of the
containing ADT type:

```seawitch
type Shape =
    | Circle as { r: Int32 }
    | Square as { a: Int32 }

shape: Shape = Shape.Circle { r = 10 }
```

The following rules apply:

- `Shape` is a type; `Shape.Circle` is a qualified variant name.
- `Circle` is not a type and is not available as an unqualified constructor.
- `Shape.Circle` is not assignable to `Shape.Square` or to an unrelated type.
- A variant has no independently assignable subtype identity.
- A variant never implicitly converts to its numeric tag, payload, or another
  variant.
- ADTs with identical declarations remain distinct nominal types.

The checker retains the active variant identity in a narrowed match arm, but
the narrowed view remains a view of the same ADT storage rather than a new
source-level subtype.

## Construction

### Unit variants

A unit variant is constructed by its qualified name:

```seawitch
direction: Direction = Direction.North
```

`Direction.North` is a value expression of type `Direction`. It is not a
function, integer constant, or empty object literal.

### Record variants

A record variant is constructed with a qualified owner, variant, and named
payload initializer:

```ebnf
variant-constructor  = qualified-variant
                     | qualified-variant , payload-initializer ;
payload-initializer = "{" , field-initializer
                         , { "," , field-initializer } , [ "," ] , "}" ;
field-initializer    = identifier , "=" , expression ;
qualified-variant    = identifier , "." , identifier ;
```

```seawitch
shape: Shape = Shape.Circle { r = 20 }
```

The initializer must name every payload field exactly once. Missing, duplicate,
unknown, or incorrectly typed fields are errors:

```seawitch
bad: Shape = Shape.Circle { a = 20 }
// Type Error: Circle has no field named a
```

Payload initializers are evaluated using existing expression and declaration
rules. The resulting value contains exactly one active variant and its
payload.

Unqualified construction is always rejected:

```seawitch
shape: Shape = Circle { r = 20 }
// Error: ADT variants must be qualified by their owner
```

## Generic and recursive ADTs

### Generic ADTs

An ADT may have generic parameters using RFC 0019 syntax:

```seawitch
type Result<T, E> =
    | Ok as { value: T }
    | Err as { error: E }

success: Result<Int32, String> = Result.Ok { value = 42 }
```

Generic arguments are part of the nominal identity. `Result<Int32, E>` and
`Result<Int64, E>` are distinct specializations and may have distinct C
layouts. Variant qualification uses the instantiated owner for resolution,
while source spelling remains `Result.Ok`.

This RFC adds no generic inference, specialization, or monomorphization rules,
and adds no explicit generic constraint syntax. Those rules come from RFC 0019.
In v1, dependent operations on generic ADTs are checked at concrete
specialization time. Generic ADTs are implemented only after the required RFC
0019 behavior exists.

### Recursive ADTs

An ADT may refer to itself only through an existing indirection that gives the
representation finite size:

```seawitch
type Expr =
    | Literal as { value: Int32 }
    | Add as { left: Ptr<Expr>, right: Ptr<Expr> }
```

Direct by-value recursion is rejected because it has no finite layout. This
RFC does not add ownership or lifetime guarantees to pointer payloads.

Recursive generic substitutions that expand without reaching an indirect edge
are rejected.

## Match expressions

`match` is an expression. It evaluates its scrutinee once, selects one arm,
and produces that arm's result. It has two modes:

- value mode, written `match expression`, matches supported literal values;
- type mode, written `match expression is`, matches exact type members and ADT
  variants.

```ebnf
value-match-expression  = "match" , expression , value-match-arm
                          , { value-match-arm } , "end" ;
type-match-expression   = "match" , expression , "is" , type-match-arm
                          , { type-match-arm } , "end" ;
value-match-arm         = "|" , value-pattern , "then" , expression ;
type-match-arm          = "|" , type-pattern , "then" , expression ;
value-pattern           = boolean-pattern | "else" ;
type-pattern            = primary-type-expression
                          | qualified-variant
                          | "else" ;
boolean-pattern         = "true" | "false" ;
qualified-variant       = identifier , "." , identifier ;
```

Type patterns reuse RFC 0014's `primary-type-expression` production, extended
by RFC 0019 for generic type constructors. A type pattern must resolve to one
complete canonical member type. A union expression is not itself a pattern;
its members are matched in separate arms. An ADT owner type is matched through
its qualified variants rather than as one broad type pattern.

Examples:

```seawitch
area: Int32 = match shape is
    | Shape.Circle then shape.r * shape.r
    | Shape.Square then shape.a * shape.a
end

ready_label: Int32 = match ready
    | true then 1
    | false then 0
end
```

`match` and `then` are reserved syntax. `is` is already reserved by RFC 0014.
The leading `|` is the match-arm delimiter in a match expression. `else` is
the only default arm spelling and must be written after `|`:

```seawitch
result: Int32 = match value is
    | Nil then 0
    | else then 1
end
```

Type mode uses exact active-member identity. It does not use conversion,
pointer weakening, subtype relationships, or value equality. Value mode is
currently limited to Boolean literals; numeric, `Rune`, and `String` value
patterns are deferred to their owning equality/literal specifications.

An `is` token immediately before the first match arm selects type mode. An
`is` operator inside the scrutinee must be parenthesized:

```seawitch
match (value is Int32)
    | true then 1
    | false then 0
end
```

The parser recognizes an `is` immediately before the first arm as the match
mode marker. A scrutinee expression that contains the RFC 0014 `is` operator
must be parenthesized. The leading `|` starts an arm only at match-arm depth
zero. A `|` inside parentheses or nested type arguments remains part of that
nested expression. A nested `match` consumes its own `end` before the outer
match resumes. `case` is not reserved by this RFC.

Match arms are expressions, not statement blocks. A no-result expression is
not a valid arm result. Parentheses and nested `match` expressions provide
composition; multi-statement arms are deferred.

## Scrutinee evaluation and arm selection

The scrutinee is evaluated exactly once, even when it is not a stable binding:

```seawitch
result: Int32 = match read_shape() is
    | Shape.Circle then 1
    | Shape.Square then 2
end
```

When the scrutinee is a named binding, type-mode arms refine that binding for
the duration of the arm:

```seawitch
value: Ptr<Node> | Nil = read_node()

result: Int32 = match value is
    | Nil then 0
    | else then value.value
end
```

The `Nil` arm sees `value` as `Nil`. The `else` arm sees `value` as the exact
remaining member or union of members. For an ADT, a variant arm exposes only
that variant's payload fields:

```seawitch
shape: Shape = read_shape()

radius: Int32 = match shape is
    | Shape.Circle then shape.r
    | Shape.Square then 0
end
```

The narrowing rules are:

- the binding identity, declared type, and mutability remain unchanged;
- the refined view exists only inside the selected arm;
- payload fields are read-only under this RFC;
- assigning to a mutable scrutinee invalidates its refined view for subsequent
  expressions in that arm;
- a non-identifier scrutinee is still evaluated once but has no source name for
  member access; and
- a unit variant exposes no payload fields.

Variant-specific methods and implementation blocks are deferred. Methods
already available on the containing ADT or on a payload field's type retain
their existing resolution rules under the refined view.

Patterns are tested in source order. Once a pattern matches, no later pattern
or arm expression is evaluated. A pattern test itself has no user-defined
side effects. The selected arm is evaluated exactly once.

Independent expression evaluation retains RFC 0009 sequencing rules. The
match construct does not add a sequencing guarantee for unrelated expressions.

## Pattern semantics

### ADT variant patterns

A variant pattern is valid only in type mode and matches the exact active
variant of the exact owner ADT:

```seawitch
label: Int32 = match shape is
    | Shape.Circle then 1
    | Shape.Square then 2
end
```

`Shape.Circle` does not match `Drawing.Circle`, even when both names have the
same spelling. A variant pattern does not match a value that merely converts
to the written owner or variant.

### Exact type patterns

An exact type pattern is valid only in type mode:

```seawitch
value: Int32 | Float32 | Nil = read_value()

label: Int32 = match value is
    | Int32 then 1
    | Float32 then 2
    | Nil then 0
end
```

The pattern matches only the exact active canonical member. `MutPtr<T>` does
not match `Ptr<T>` through weakening, and a type pattern never performs a
conversion.

### Boolean value patterns

Boolean patterns are valid only in value mode:

```seawitch
label: Int32 = match ready
    | true then 1
    | false then 0
end
```

`true` and `false` are values, not type patterns. Use `Bool` only as a type
pattern if a future use requires matching the complete Boolean member.

### `else` pattern

`else` matches every value or type member remaining after previous arms. It is
valid in either mode, must be the final arm, and no later arm is permitted.
There is no `_` match pattern in this RFC.

Duplicate patterns and patterns that cannot match any remaining value or type
member are rejected rather than ignored.

## Exhaustiveness

The checker requires every possible scrutinee value to be covered:

- an ADT in type mode requires every qualified variant or `else`;
- a structural union in type mode requires every canonical member or `else`;
- `Bool` in value mode requires `true` and `false` or `else`;
- a non-union type pattern that covers the complete scrutinee makes a later
  `else` unreachable; and
- every other value-mode match requires `else`.

```seawitch
bad: Int32 = match shape is
    | Shape.Circle then 1
end
// Error: match is not exhaustive; missing Shape.Square
```

Exhaustiveness is based on canonical nominal identity and active variant
identity, never on conversion ranking, truthiness, or source spelling. A type
alias does not create additional cases.

## Arm result typing and scope

Every arm is checked with the match expression's expected type. If the match
has no expected type, all arm expressions must have one identical canonical
type:

```seawitch
result: Int32 = match shape is
    | Shape.Circle then 1
    | Shape.Square then 2
end
```

Untyped literals in arms receive the expected result type under the existing
contextual typing rules. This RFC does not infer a union merely because arm
types differ.

Each arm has a lexical child scope for flow facts and declarations. A
scrutinee refinement is visible only inside its arm. Declarations inside one
arm do not escape to another arm or the enclosing scope. The binding identity
and declared mutability of the scrutinee are unchanged by matching.

## Equality and other operations

This RFC does not define equality, ordering, or hashability. Those rules are
owned by RFC 0024.

Until the applicable operator specification permits them, ADT values do not
support arithmetic, ordering, or arbitrary operator overloads. Pattern
matching on ADTs uses variant identity and does not depend on `==`.

## Empty structs and unit variants

Unit variants are the ADT mechanism for values with no payload:

```seawitch
type Direction = | East | West | North | South
```

They do not create an empty object or empty C struct. Ordinary empty object
types remain outside this RFC because their layout, initialization, pointer
identity, and ABI rules are not defined. A future empty object feature must
specify an explicit non-zero representation rather than relying on an empty C
struct extension.

## C23 lowering

Each ADT specialization lowers to one nominal tagged representation:

```c
typedef enum {
    sw_shape_circle,
    sw_shape_square
} sw_shape_tag;

typedef struct {
    sw_shape_tag tag;
    union {
        struct { int32_t sw_m_r; } circle;
        struct { int32_t sw_m_a; } square;
    } payload;
} sw_shape;
```

The generated C enum and tag values are compiler-owned implementation details.
They are not source-level integers and are not part of a C ABI contract.

Generated code must:

- initialize the tag and matching payload together;
- read a payload only after checking its active tag;
- never read an inactive union member;
- preserve payload field layout and source `#line` mappings; and
- handle an impossible invalid tag without reading a payload or emitting
  undefined behavior.

Unit variants require only a tag. The payload union may use an implementation-
owned representation with no dummy source field.

For `match`, the generator evaluates a non-stable scrutinee into one checked
temporary. It lowers exact type and variant patterns to tag/member tests,
Boolean patterns to Boolean comparisons, and selected arms to readable
conditional control flow. A type-mode `else` covers the remaining checked
members.

Generated control flow must preserve the source-visible narrowed view. A
payload field is read only after the generated tag test proves its variant is
active.

The generator must distinguish two failure classes:

1. an invalid checked ADT or match representation is an `Unknown Error` during
   generation, and no invalid C is emitted; and
2. an impossible runtime tag in generated C invokes a compiler-owned,
   non-returning invalid-tag trap, without reading a payload or relying on
   undefined behavior.

## Diagnostics and fail-closed behavior

Representative diagnostics are:

```text
[Syntax Error] expected a variant after `|`
[Syntax Error] expected `then` after match pattern
[Syntax Error] expected `is` before a type or variant pattern
[Syntax Error] an `is` operator in a match scrutinee must be parenthesized
[Type Error] ADT declarations require at least two variants
[Type Error] ADT variant name is duplicated
[Type Error] unknown qualified variant Shape.Triangle
[Type Error] ADT variant must be qualified by its owner
[Type Error] variant constructor requires the payload field r
[Type Error] ADT payload fields cannot be mutable in this RFC
[Type Error] ADT recursion has no finite representation
[Type Error] match pattern does not belong to the scrutinee type
[Type Error] value patterns are not valid in type mode
[Type Error] type and variant patterns are not valid in value mode
[Type Error] match is not exhaustive; missing Shape.Square
[Type Error] duplicate or unreachable match pattern
[Type Error] match arm does not produce a value
[Type Error] match arm result types do not agree
```

The parser owns malformed declarations, constructors, match modes, and match
arms. The checker owns nominal identity, payload validation, recursion, pattern
classification, arm narrowing, reachability, exhaustiveness, and result
typing. The generator owns impossible checked-state errors. It must never emit
a placeholder type, read an inactive payload, or lower a variant to an
unvalidated integer.

## Deferred

- Mutable ADT payload fields and variant update operations.
- Positional and tuple variants.
- Numeric, floating, `Rune`, and `String` value patterns.
- Guards, OR-patterns, ranges, and negated patterns.
- Multi-statement match arms and match statements.
- Variant-specific methods and implementation blocks.
- Explicit C representation, numeric tags, and FFI enum/union contracts.
- Ownership, move, drop, allocator, and lifetime semantics for payloads.
- Ordinary empty object types.
- Runtime reflection and serialization schemas.

## Acceptance criteria

The nongeneric implementation is complete when focused end-to-end tests prove
that:

1. ADT declarations parse with unit and named-record variants;
2. ADTs require at least two distinct variants;
3. variant names are qualified by their owning ADT and do not enter the global
   value namespace;
4. constructors require qualification and validate every payload field;
5. unit variants construct values without empty structs or dummy payloads;
6. direct recursive layouts are rejected and indirect recursion is accepted;
7. `match expression` parses value mode and `match expression is` parses type
   mode;
8. variant and exact type patterns require type mode;
9. Boolean patterns require value mode;
10. `Nil` and exact union-member patterns narrow the named scrutinee;
11. ADT variant patterns expose the correct payload members through the
    narrowed scrutinee;
12. `else` covers the remaining members, is final, and replaces wildcard
    behavior;
13. the scrutinee evaluates exactly once and only the selected arm evaluates;
14. mutation invalidates a scrutinee's refined view;
15. duplicate, unreachable, and non-exhaustive patterns are diagnosed;
16. arm result types receive the expected context and reject incompatible or
    no-result arms;
17. type and variant matching does not depend on equality or truthiness;
18. generated C uses a deterministic tag-and-payload layout and never reads an
    inactive payload;
19. impossible runtime tags reach the compiler-owned fatal trap without
    undefined behavior; and
20. every new declaration, constructor, pattern, checked node, narrowing fact,
    and generator case is handled explicitly under the fail-closed
    architecture.

Generic integration is complete separately when RFC 0019's specialization
rules are implemented and generic ADT declarations, constructors, layouts,
recursive substitutions, and match patterns pass the same guarantees.

## Implementation handoff

The phased implementation plan must identify:

1. lexer tokens for `match` and `then`, with `is` reused from RFC 0014 and
   `end` behavior preserved;
2. parser support for ADT declarations, qualified constructors, value-mode
   matches, type-mode matches, and structural delimiter recovery;
3. the rule that `|` starts an arm only at match-arm depth zero, while `|`
   inside nested type arguments or parenthesized expressions remains part of
   that nested expression;
4. nominal ADT and qualified-variant resolution without global variant names;
5. payload validation, immutable payload views, and indirect-recursion checks;
6. exact type-pattern resolution against canonical union members without
   conversions or pointer weakening;
7. scrutinee temporaries, named-binding flow facts, arm scopes, assignment
   invalidation, reachability, and exhaustiveness checking;
8. expected-type propagation for arm results;
9. deterministic tagged C representation, narrowed member lowering, inactive-
   payload protection, and the invalid-tag runtime trap;
10. the nongeneric implementation phase and the later RFC 0019 generic
    integration phase; and
11. focused integration coverage in ADT/match facet-named test files.

Canonical grammar, language, and status documents are updated once behavior
stabilizes.
