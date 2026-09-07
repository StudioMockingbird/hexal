# RFC 0142: Bringing Over Snacc Learnings

- Kind: Language Semantics
- Status: Closed; implemented 2026-09-07. The lexer replaced `impl` with
  `struct`/`union`/`method`; the parser now dispatches `type Name is ...` five
  ways (struct, structural union, payload ADT, all-unit ADT shorthand, plain
  alias), parses `method Receiver.name(...)`, and replaced brace/`.new()`
  construction with one labeled call-argument grammar
  (`compiler/parser/{statements,type_expressions,expressions,ast}.go`). The
  checker's `checkBareConstructorCall` (`compiler/checker/calls.go`) dispatches
  bare `Type(...)` to a user struct/generic or one of the nine canonical
  constructors (Heap, Stash, Pool, List, Dict, Channel, Mutex, Atomic, Error),
  reusing the existing `checkStructConstructorCall`/`ObjectValue` machinery
  unchanged; qualified `Owner.Variant(...)` reuses `buildVariantConstructor`.
  Empty structs (`type Marker is struct end`) needed zero checker changes
  beyond admitting the zero-member parse: `compiler/generator/emission.go`
  emits a private `unsigned char hex_empty` field,
  `compiler/generator/validation.go`'s `validateGeneratedType` was fixed to
  gate on `object.Incomplete` instead of `len(Members) == 0` (the latter
  wrongly rejected every empty struct), and `compiler/generator/print.go`
  emits the exact `Name {}` form (fixing a latent double-space bug in the
  naive brace-with-empty-member-list path). Construction and equality needed
  no new code: both already reduce correctly on zero members. Verified
  end-to-end by compiling and running real programs through GCC 16.1 under
  `-std=c23 -Wall -Wextra -Werror`. Phase 7 migrated ~140 test-fixture files
  and the full 143-snippet workbench catalog to the new grammar (many via
  parallel subagents), added a `values-empty-struct` snippet, updated
  `AGENTS.md`'s historical-syntax note and `docs/reference.md`'s grammar and
  semantics sections, and fixed one stale user-facing diagnostic
  (`compiler/checker/scope.go`'s "self is not bound outside a method body",
  previously still saying "impl"). The rebuilt snippet manifest
  (`workbench/snippets/testdata/generated-c-sha256.json`) is additive only:
  zero pre-existing entries changed. Full gate green: `gofmt`, `go build`,
  `go vet` (plain and `-tags c23`), `go test -count=1 ./...`, and
  `go test -tags c23 -count=1 ./...` (real GCC/Clang/Zig compilation of the
  whole tagged snippet catalog, ~5 minutes, zero warnings).
- Created: 2026-09-06
- Updated: 2026-09-07
- Coordinates with: `docs/reference.md`
- Does not change: transparent-alias identity, structural-union identity,
  existing object/ADT layout, explicit method receiver semantics, generics,
  visibility, descriptive compiler-owned type operations, or existing
  generated C

## Summary

Adopt three cohesive parts of Snacc's declaration surface while retaining
Hexal's existing semantics:

1. One `type Name is ...` family for aliases, structural sums, structs, ADTs,
   and enum-style unit ADTs.
2. `method Receiver.name(...) do ... end` in place of `impl`.
3. One direct type-call construction form for structs, ADT variants, and
   compiler-owned constructible types; `.new()` constructors are removed.

Also admit nominal empty structs and an abbreviated all-unit ADT form:

```hexal
type Marker is struct end

type Dir is East | West | North | South end

type Point is struct
    mut x: Int32,
    y: Int32,
end

method Ptr<Point>.length_squared(): Int32 do
    return (self.x * self.x) + (self.y * self.y)
end
```

`type Score is Int32` remains a transparent alias. This RFC does not import
Snacc's nominal represented/newtype semantics.

## Goals

- Give every named type one recognizable declaration head.
- Make record, structural-sum, and nominal-sum definitions explicit.
- Give unit-only ADTs a compact spelling without adding a separate enum
  category or observable integer discriminant.
- Name instance behavior with `method` rather than the indirect `impl` term.
- Preserve Hexal's explicit receiver capability model and static dispatch.
- Use one call-shaped construction syntax for every canonical constructor.
- Remove the redundant `new` name without removing descriptive operations such
  as `String.from_bytes`.
- Keep all new forms as direct, allocation-free C23 constructs.

## Type declaration grammar

Replace the current type-declaration, object-definition, and ADT productions
with:

```ebnf
type-declaration = "type" , identifier , [ generic-parameter-list ]
                   , "is" , type-definition ;

type-definition = alias-definition
                  | structural-union-definition
                  | struct-definition
                  | adt-definition
                  | unit-adt-definition ;

alias-definition = alias-target ;
alias-target = named-type | generic-type | array-type
               | pointer-type | function-type-expression ;

structural-union-definition = "union" , primary-type-expression
                              , "|" , primary-type-expression
                              , { "|" , primary-type-expression } , "end" ;

struct-definition = "struct"
                    , [ member-declaration
                        , { "," , member-declaration } , [ "," ] ]
                    , "end" ;
member-declaration = [ "mut" ] , identifier , ":" , type-expression ;

adt-definition = "union" , adt-variant , { adt-variant } , "end" ;
adt-variant = "|" , identifier , [ adt-payload ] ;
adt-payload = "as" , payload-member
              , { "," , payload-member } , [ "," ] , "end" ;
payload-member = identifier , ":" , type-expression ;

unit-adt-definition = identifier , "|" , identifier
                      , { "|" , identifier } , "end" ;
```

`struct` and `union` become reserved words. `is`, `as`, and `end` are already
reserved.

### Unambiguous selection

The first token after `is` selects or begins the definition:

| Form after `is` | Meaning |
| --- | --- |
| `struct` | Nominal struct, including an empty struct |
| `union` followed by a type | Transparent structural-sum name |
| `union` followed by `|` | Nominal payload-capable ADT |
| one alias target with no following `|` | Transparent alias |
| identifier followed by `|` | Nominal all-unit ADT shorthand |

The shorthand's identifiers always declare new unit variants. They never
refer to existing types, even when an existing type has the same spelling:

```hexal
type East is Int32
type Dir is East | West end
```

Here `Dir.East` is a new unit variant. It is not the transparent alias `East`.
Structural sums of existing types use the explicit form:

```hexal
type Value is union East | String end
```

This asymmetry is deliberate. The explicit `union` keyword makes every name
that follows a structural member reference; without it, the identifiers are
variant declarations. Requiring a leading `|` for the compact unit form would
add syntax without changing that decision, so the Snacc form remains
`type Dir is East | West end`.

An alias whose written target directly contains `|` is unavailable. An alias
may still name an existing structural sum:

```hexal
type MaybeScore is union Int32 | Nil end
type OptionalScore is MaybeScore
```

Parentheses do not bypass the explicit structural-sum form:

```hexal
type MaybeScore is (Int32 | Nil)  -- rejected
```

Commas separate struct and ADT-payload members because line breaks are lexical
separation, not grammar. One trailing comma is allowed. A payload must contain
at least one field. A payload-capable or all-unit ADT must contain at least two
distinct variants. A structural union must contain at least two written
members and remains subject to canonical duplicate rejection.

Definition blocks are valid only as the direct body of a type declaration.
They are not general type expressions.

## Construction grammar

Structs and ADT variants use call-shaped named construction:

```ebnf
argument-list = [ call-argument , { "," , call-argument } , [ "," ] ] ;
call-argument = [ identifier , "=" ] , expression ;
```

The general call grammar admits an optional trailing comma so multiline calls
and constructors share one delimiter rule. Argument labels have meaning only
after the callee resolves:

- A non-empty struct constructor requires every argument to be named.
- A payload ADT-variant constructor requires every argument to be named.
- Empty structs and unit ADT variants require empty parentheses.
- Compiler-owned canonical constructors take the positional arguments in their
  signatures. Ordinary functions, methods, function values, and descriptive
  compiler-owned operations remain positional and reject every named argument.
- Constructor arguments may appear in any order. Every declared field must
  appear exactly once; unknown and duplicate labels are rejected.
- Constructor argument expressions evaluate once, left to right in written
  argument order. Field storage follows declaration order.
- A type or qualified variant name without its call is a constructor name,
  not a value.

Examples:

```hexal
point := Point(
    x = 10,
    y = 20,
)

circle := Shape.Circle(radius = 10)
marker := Marker()
east := Dir.East()
```

The `=` in a named constructor argument initializes one field. It is not an
assignment expression and does not introduce general named function arguments.

### Canonical compiler-owned constructors

Remove every compiler-owned `.new()` form and replace it with a direct type
call:

| Removed | Replacement | Result |
| --- | --- | --- |
| `Error.new(header, message)` | `Error(header, message)` | `Error` |
| `Heap.new()` | `Heap()` | `Heap` |
| `Stash<T>.new()` | `Stash<T>()` | `Stash<T>` |
| `Pool<T>.new(capacity)` | `Pool<T>(capacity)` | `Pool<T>` |
| `List<T>.new(heap)` | `List<T>(heap)` | `List<T>` |
| `Dict<K,V>.new(heap)` | `Dict<K,V>(heap)` | `Dict<K,V>` |
| `Channel<T>.new(heap, capacity)` | `Channel<T>(heap, capacity)` | `Channel<T> | Error` |
| `Mutex.new(heap)` | `Mutex(heap)` | `Mutex | Error` |
| `Atomic<T>.new(initial)` | `Atomic<T>(initial)` | `Atomic<T>` |

- The replacement retains the removed form's exact parameter types,
  evaluation order, result type, failure behavior, ownership, placement rules,
  and C lowering.
- A fallible constructor returns its existing union and therefore still needs
  ordinary `try`, narrowing, or propagation at the call site.
- A transparent alias of a constructible type invokes that canonical type's
  constructor and introduces no new identity or generated symbol.
- Imported nominal structs and ADT variants construct through their qualified
  type or variant name.
- `T(...)` is construction, never numeric conversion or a user-overloadable
  call. Explicit numeric conversion remains `value.to<T>()`.
- A constructor cannot be extracted as a `Fun` value.
- Type and value names already share one namespace, so a constructible type and
  a callable value cannot create an ambiguous `Name(...)` expression.
- `new` remains an ordinary identifier with no reserved or compiler-known
  meaning. This RFC removes `.new()` as a constructor convention; it does not
  prohibit an ordinary instance method whose source name happens to be `new`.

Descriptive operations retain their names because the suffix identifies a
specific source or behavior rather than repeating “construct”:

```hexal
String.from_bytes(heap, bytes)
String.from_runes(heap, runes)
View<Int32>.from_pointer(pointer, length)
IO.stdin()
IO.stdout()
```

Compiler-owned type-qualified operations remain closed and cannot be declared
by users.

## Type semantics

### Transparent aliases

`type Alias is T` preserves the current transparent-alias contract:

- `Alias` and `T` have one canonical identity, representation, operation set,
  assignability relation, and method set.
- The alias emits no C typedef.
- Targets resolve in source order; recursive alias cycles remain invalid.
- Generic aliases remain transparent after substitution.
- No wrapper construction, conversion, distinct method namespace, or extra
  generic specialization is introduced.

### Named structural sums

`type Name is union A | B end` preserves the current named structural-union
contract:

- `Name` transparently names the canonical structural union.
- Nested members flatten, canonical duplicates are rejected, and identity is
  order-independent.
- Injection, widening, narrowing, Nil rules, pointer niches, equality, and C
  representation are unchanged.
- `union` and `end` do not make this form nominal.

### Structs

`type Name is struct ... end` preserves the current nominal object contract,
except that zero members are now valid:

- Members remain ordered and independently fixed or `mut`.
- Member uniqueness, source-order type resolution, access,
  equality, printing, methods, recursion checks, and layout remain unchanged.
- Construction is `Name(member = value)` and uses the rules in `Construction
  grammar`.
- An empty struct is constructed as `Name()` and has exactly one value.
- All values of one empty struct type compare equal; distinct empty struct
  types remain nominally distinct and cannot compare or assign.
- Printing an empty struct uses the existing object format with no members:
  `Name {}`.
- An empty struct supports methods and can appear in every position already
  valid for an object type.

C23 has no portable zero-sized object type. An empty Hexal struct therefore
lowers to a nominal C struct containing one private `unsigned char` field:

```c
typedef struct hex_t_m3_app_Marker {
    unsigned char hex_empty;
} hex_t_m3_app_Marker;
```

Its initializer is zero, `size_of<Marker>()` is 1, and
`align_of<Marker>()` is 1 on every supported target. Hexal performs no
zero-size optimization in arrays, objects, ADTs, unions, parameters, or
results. The private field is never addressable from Hexal.

`struct` is the source keyword. Compiler internals may retain the semantic
term `Object` where renaming would add churn without clarity, but no comment or
syntax-tree field may describe removed brace-definition syntax.

### Payload-capable ADTs

`type Name is union | ... end` preserves the current nominal ADT contract:

- Unit variants remain singleton values constructed as `Name.Variant()`.
- Payload variants use `| Variant as fields end` and construct as
  `Name.Variant(field = value)`.
- Payload fields remain fixed and reject `mut`.
- Variant and payload-field uniqueness, matching, exhaustiveness, layout,
  equality, printing, module ownership, and generic specialization are
  unchanged.
- The `end` after a payload closes that payload; the final `end` closes the
  ADT.

### All-unit ADT shorthand

`type Dir is East | West | North | South end` is exactly shorthand for:

```hexal
type Dir is union
    | East
    | West
    | North
    | South
end
```

It introduces one nominal ADT and four unit variants. It does not introduce an
enum type, integer constants, top-level `East`, or four independently storable
struct types.

- Values are constructed as `Dir.East()`, `Dir.West()`, `Dir.North()`, and
  `Dir.South()`.
- `Dir.East` without the call is a constructor name, not a value.
- Variants are qualified outside patterns wherever the existing ADT rules
  require qualification.
- Matching, equality, printing, tags, layout, exports, and module identity are
  identical to the expanded ADT.
- No variant has an integer value and no integer conversion is available.
- Payload and unit syntax cannot be mixed in the shorthand. Use the expanded
  `union | ... end` form when any variant carries data.

The empty call is intentional despite appearing at every use: one uniform
constructor rule is more valuable than minimizing punctuation only for unit
variants. `Dir.East()` visibly constructs a value; bare `Dir.East` remains a
non-value rather than adding a second value-producing form.

## Method declarations

Replace:

```hexal
impl MutPtr<Point>.translate(dx: Int32, dy: Int32) do
    self.x = self.x + dx
    self.y = self.y + dy
end
```

with:

```hexal
method MutPtr<Point>.translate(dx: Int32, dy: Int32) do
    self.x = self.x + dx
    self.y = self.y + dy
end
```

Grammar:

```ebnf
method-declaration = "method" , type-expression , "." , identifier
                     , [ generic-parameter-list ] , signature
                     , "do" , block , "end" ;
```

`method` becomes reserved and `impl` stops being reserved. The old `impl` form
is removed without a compatibility parser.

This is a syntax and terminology change, not adoption of Snacc's inferred
receiver effects:

- `method T.name` retains Hexal's value receiver and copies `T`.
- `method Ptr<T>.name` retains a non-owning read-only receiver.
- `method MutPtr<T>.name` retains a non-owning writable receiver and may write
  only `mut` members.
- Receiver adaptation order, implicit `self`, static dispatch, evaluation
  order, generic methods, recursion, module ownership, exports, method/member
  name exclusion, and non-extractability remain unchanged.
- Existing methods remain restricted to locally owned nominal structs and
  their valid pointer receiver forms. This RFC does not add methods to ADTs,
  ADT variants, built-ins, structural unions, or imported types.

Public declarations use `export method`. Checked and generated terminology
must say method rather than impl. Existing method C symbols and generated C
remain byte-identical after source migration.

## No user-defined associated functions

User-defined `static Type.name(...)` declarations remain unavailable. This is
an explicit scope decision, not an omitted half of the method syntax:

- A module-level free function expresses the same computation and can already
  be private, exported, generic, recursive, or mutually recursive.
- Adding associated functions would require another declaration category,
  owner/name namespace, export rule, generic lookup path, generated-symbol
  family, and exhaustive dispatch solely to change call-site qualification.
- That cost has low value for a language committed to a small surface and one
  obvious construction syntax.
- The compiler-owned type-qualified operation set remains closed because its
  members implement intrinsic or library contracts, not a general user
  extension mechanism.

This RFC does not reserve `static` or establish a deferred syntax. A later RFC
must independently justify any user-defined associated-function feature.

## Exports and modules

`export` prefixes the complete declaration:

```hexal
export type Point is struct
    x: Int32,
end

export method Point.x_value(): Int32 do
    return self.x
end
```

Type visibility, qualified imported names, nominal module ownership, and the
prohibition on type and method declarations outside module scope are unchanged.

## Removed syntax

There is no compatibility period. These forms become syntax errors:

```hexal
type Score = Int32
type MaybeScore = Int32 | Nil
type Point = { x: Int32 }
type Shape as | Circle { radius: Float64 } | Empty end

point := Point { x = 10, y = 20 }
circle := Shape.Circle { radius = 10 }
marker := Marker {}
east := Dir.East

heap := Heap.new()
values := List<Int32>.new(heap)
channel := try Channel<Int32>.new(heap, 4)

impl Point.read(): Int32 do
    return self.x
end
```

Use these diagnostics at the first decisive token while it remains reserved:

- `=` after a type name: `type declarations use 'is', not '='`
- `as` after a type name: `ADT declarations use 'type Name is union ... end'`
- `{` after `is`: `struct declarations use 'struct ... end'`
- a parenthesized or non-identifier top-level union after `is`:
  `structural sum declarations use 'union ... end'`
- `{` after an ADT variant: `ADT payloads use 'as ... end', not braces`
- `{` after a struct type or qualified ADT variant in an expression:
  `constructors use named arguments in parentheses`
- `.new` after a compiler-owned constructible type:
  `constructors use 'Type(...)', not '.new(...)'`
- a named argument passed to a non-constructor:
  `named arguments are valid only for struct and ADT constructors`

Because `impl` ceases to be a keyword, its removed form receives the ordinary
syntax or name diagnostic produced for that token sequence; no dormant legacy
branch is retained solely to customize its error. The parser must never
translate a legacy declaration into a current syntax-tree node.

## Existing generated-output contract

Equivalent migrated programs without newly admitted empty structs produce
byte-identical generated artifacts:

- Type identities, layouts, declarations, and ordering do not change.
- Existing ADT shorthand expands to the same checked ADT as its long form.
- Existing method C names and call lowering do not change.
- Constructor syntax reaches the existing checked initializer representation;
  field ordering, initialization, and generated C do not change.
- Alias and structural-union names remain compile-time-only.
- Source migration preserves line structure used by `#line` mappings.

Only programs exercising an empty struct add a new C declaration.

## Implementation plan

### Phase 1: baseline and inventory

1. Run the ordinary suite and record the snippet manifest before changes.
2. Inventory every live current type definition, `impl` declaration, brace
   constructor, bare unit variant, and compiler-owned `.new()` call in
   lexer, parser, checker, generator, integration, workbench, and active-spec
   fixtures. Record the set rather than a fixed count.
3. Classify type declarations as alias, structural sum, object, or ADT before
   rewriting. Do not apply one regex across all forms.
4. Record byte baselines for representative alias, structural union, object,
   ADT, generic object, generic ADT, private/exported method, cross-module
   method, and every compiler-owned constructor family.

### Phase 2: tokens and syntax tree

5. Add `Struct`, `Union`, and `Method` token kinds and keyword recognition.
   Remove `Impl` from the keyword map and token dispatch.
6. Add focused keyword, maximal-munch, and reserved-name lexer tests.
7. Rename parser `ImplDeclaration` to `MethodDeclaration`, its entrypoint, and
   every parser comment and diagnostic that exposes `impl` terminology.
8. Revise type-definition delimiter metadata. Retain the existing semantic
   target node categories, but rename brace-specific fields so one member-body
   node can honestly represent `struct ... end` and `as ... end`.
9. Extend call arguments with an optional source label and label position.
    Keep the expression unchanged so unlabeled calls retain their current
    syntax-tree shape and evaluation path.

### Phase 3: type parsers

10. Make a type declaration consume `is` after its optional generic parameters
    and dispatch according to the table in `Unambiguous selection`.
11. Parse a plain alias target without accepting a written top-level union.
12. Parse `union` followed by a type as a structural sum and require at least
    one `|` plus its `end`.
13. Parse `struct ... end`, including the empty body, through a shared
    comma-delimited member-body parser.
14. Parse `union` followed by `|` as a payload-capable ADT. Parse each payload
    with its own `as ... end`, then consume the ADT's final `end`.
15. Parse `Identifier | Identifier ... end` as an all-unit ADT and lower it to
    the same `AdtDefinitionExpression` used by the long form.
16. Parse `method` through the renamed existing method path while retaining
    every accepted receiver type and generic form.
17. Parse optional `identifier =` labels and one trailing comma in call
    argument lists. Do not decide in the parser whether the callee is a
    constructor or an ordinary callable.
18. Add the specified removed-form diagnostics before ordinary recovery can
    reinterpret a legacy type declaration.

### Phase 4: checker type changes

19. Send aliases, structural unions, non-empty structs, and expanded ADTs
    through their existing checker paths with no semantic conversion.
20. Admit empty object-member lists only for `struct` definitions. Define the
    nominal singleton's equality, printing, placement, layout-query, generic,
    and recursive-layout behavior as specified.
21. Send the all-unit shorthand through the existing ADT declaration path so
    it cannot acquire separate identity, validation, or generation rules.
22. Verify that the shorthand's identifiers always declare variants and never
    resolve as structural-union member types.
23. Resolve a called nominal struct type or qualified ADT variant as a
    constructor before ordinary callable lookup. Reuse the existing checked
    object/record-variant initializer representation and validation.
24. Require labels for every non-empty constructor argument; validate unknown,
    duplicate, missing, and misplaced labels through the existing field table.
    Empty constructors accept no arguments.
25. Reject labels after every non-constructor callee resolves, while preserving
    positional arity, generic inference, receiver adaptation, and evaluation
    order for ordinary calls.
26. Replace each compiler-owned `.new()` checker entry with direct construction
    of that protected type. Reuse its existing signature and checked node; do
    not create a general static-function mechanism.
27. Resolve transparent aliases through their canonical constructible type.
    Reject direct construction of non-constructible types and constructor
    extraction before ordinary call resolution can reinterpret them.

### Phase 5: method migration

28. Rename parser, checker, and generator surface terminology from impl to
    method where it denotes this declaration category. Preserve internal names
    only when they do not leak into comments, diagnostics, tests, or public
    structures and renaming would be pure churn.
29. Retain current receiver checking, adaptation, method lookup, exports,
    generics, recursion, and lowering.
30. Re-run the recorded method C baselines and require byte identity.

### Phase 6: empty-struct and constructor generation

31. Emit the private byte member and zero initializer for an empty struct.
    Reuse ordinary object spelling everywhere else.
32. Make empty-struct equality a constant true after evaluating each operand
    once left-to-right; emit no field comparison. Reuse object printing with
    an empty member list.
33. Lower checked constructors through the existing object, ADT, and
    compiler-owned constructor renderers. Do not add a constructor function,
    wrapper, temporary, allocation, or new C symbol solely for the new source
    syntax.

### Phase 7: repository migration

34. Rewrite every live compiler and workbench source fixture, preserving line
    breaks and semantics.
35. Update test names, AST assertions, comments, and diagnostics that state the
    removed type or impl syntax. Do not weaken semantic coverage.
36. Add small workbench snippets for a transparent alias, structural sum,
    empty struct, non-empty struct, all-unit ADT, payload ADT, value/read/write
    receiver methods, and construction. Reuse a snippet when it demonstrates
    multiple constructs meaningfully.
37. Update active specs whose executable examples or future instructions rely
    on current syntax. Never edit archived specs.
38. Update `AGENTS.md`'s closed-spec warning to identify `=`, brace object
    definitions/construction, bare unit variants, `.new()` constructors,
    `type Name as`, and `impl` as historical syntax.

### Phase 8: canonical documentation and gates

39. Update `docs/reference.md` grammar and exact alias, structural-union,
    object, ADT, method, construction, naming, layout, and C23 contracts
    after behavior stabilizes.
40. Preserve the current rule that user-defined static methods are unavailable
    while distinguishing the closed compiler-owned operation set.
41. Add only the new snippets' artifact hashes. No pre-existing manifest entry
    may change.
42. Run every Validation item below.
43. Remove this RFC's status row, close it, and archive it only after code,
    tests, snippets, `AGENTS.md`, active specs, and `docs/reference.md` agree.

## Required sweep

- Removed `type Name = ...`, `type Name = { ... }`, and `type Name as ... end`
  parsing and recovery branches.
- Removed `impl` token, parser branch, diagnostics, comments, and live source
  examples.
- Brace-named AST fields and parser paths used for definitions, struct
  construction, or ADT payload construction.
- Every object-literal and qualified record-variant construction fixture,
  diagnostic, snippet, and active-spec example. Printed object and ADT text
  retains its brace format.
- Call-argument consumers that assume every argument is positional or that a
  closing `)` must immediately follow the last expression.
- Every compiler-owned `.new()` parser/checker branch, signature, diagnostic,
  test, snippet, and active-spec example for Error, Heap, Stash, Pool, List,
  Dict, Channel, Mutex, and Atomic.
- Type-qualified operation dispatch that must distinguish direct canonical
  construction from retained descriptive operations such as `from_bytes`,
  `from_runes`, `from_pointer`, `stdin`, and `stdout`.
- Empty-object rejection in parser, checker, generator validation, equality,
  printing, layout, and tests.
- Reserved-word inventories in compiler, workbench, snippets, tests, and
  canonical documentation.
- Live fixtures and active specs containing removed forms.

The sweep must not alter printed object/ADT formats, member access,
module-import `=`, value assignment `=`, declaration `:=`, `is` type tests,
match-pattern `as`, descriptive compiler-owned operations, ordinary instance
methods named `new`, or archived specs.

## Validation

This section is exhaustive.

### Type syntax and identity

- Scalar, generic, pointer, function, qualified imported, and named-union
  targets work as transparent aliases.
- Generic aliases remain transparent after multiple specializations.
- Structural sums compile only through `type Name is union ... end`; aliases
  and nested unions flatten, order-independent identities intern once, and
  duplicate canonical members retain their current diagnostic.
- `type Dir is East | West end` produces the same checked ADT identity,
  variants, C representation, equality, printing, and match behavior as its
  expanded unit-ADT form.
- A shorthand variant whose name matches an existing type still declares a
  new qualified unit variant.
- `Dir.East()` and `Dir.West()` are accepted values; `East()`, bare
  `Dir.East`, integer conversion, and assigning `Dir.East()` to another ADT
  are rejected.
- The unit shorthand requires at least two unique identifiers and rejects
  payload syntax.
- Exported unit shorthand preserves qualified module access and nominal module
  identity.

### Structs and ADTs

- Non-empty structs retain fixed/mutable members, construction,
  nominal identity, recursive-layout checks, generics, exports, methods,
  equality, and printing.
- `Point(x = 1, y = 2)` and the same labels in reversed order produce the same
  value; arguments evaluate once in written left-to-right order.
- Struct and payload-variant construction rejects positional, missing,
  duplicate, and unknown arguments with the existing field-specific
  diagnostics where applicable.
- One trailing comma is accepted in empty and non-empty multiline calls and
  constructors; two trailing commas are rejected.
- Named arguments to free functions, methods, function values, and
  descriptive compiler-owned operations receive the specified
  non-constructor diagnostic. Compiler-owned canonical constructors accept
  only their specified positional arguments.
- `type Marker is struct end` and `Marker()` compile in bindings, parameters,
  results, object members, ADT payloads, arrays, lists, unions, and generics.
- Empty constructors with an argument are rejected; omitting `()` is rejected
  because a type name is not a value.
- Two empty values of one type compare equal; two distinct empty struct types
  remain non-assignable and non-comparable.
- Empty-struct print output is exactly `Marker {}`.
- Empty-struct generated C has one private unsigned-byte field, initializes it,
  and reports size 1 and alignment 1.
- Arrays and aggregates containing empty structs retain one byte per empty
  value and never emit a zero-sized C object.
- Payload-capable ADTs accept unit and `as ... end` record variants; payload
  fields reject `mut`, and duplicate/recursive checks remain unchanged.
- Payload variants construct as `Name.Variant(field = value)`; unit variants
  construct as `Name.Variant()`.
- Legacy `Name { ... }`, `Name.Variant { ... }`, and bare unit-variant value
  forms are rejected and never reach checked initializer nodes.
- `Point(x: 1)`, positional `Point(1)`, and a field assignment used as an
  ordinary call argument are rejected; named-constructor `=` never becomes a
  general assignment expression.

### Compiler-owned constructors

- Every row in `Canonical compiler-owned constructors` accepts its replacement
  spelling with the exact former parameter, result, failure, ownership,
  placement, and evaluation-order contract.
- Inferred declarations work for every direct constructor, including
  `heap := Heap()`, `values := List<Int32>(heap)`, and
  `counter := Atomic<Int32>(0)`.
- `try Channel<Int32>(heap, 4)` and `try Mutex(heap)` retain their fallible
  result and propagation behavior.
- A transparent alias of a user struct or compiler-owned constructible type
  invokes the canonical constructor and has no distinct runtime identity.
- Qualified imported struct and ADT constructors work through the defining
  module's nominal identity.
- Direct calls of scalars, structural unions, pointer types, `Fun`, Array,
  View, Task, String, Strand, IO, and other types without a declared canonical
  constructor are rejected as non-constructible types.
- Constructor names cannot be stored, passed, returned, compared, or otherwise
  extracted as function values.
- Every removed compiler-owned `.new()` spelling receives the exact migration
  diagnostic and no compatibility checker path remains.
- `new` remains an ordinary identifier and an ordinary instance method named
  `new` retains normal method behavior without constructor meaning.
- `String.from_bytes`, `String.from_runes`, `View<T>.from_pointer`,
  `IO.stdin`, and `IO.stdout` retain their current syntax and behavior.

### Methods

- Value, Ptr, and MutPtr methods compile under `method` with their existing
  copy/read/write capabilities and receiver adaptation.
- Generic methods, aliases of a receiver, direct/mutual recursion, forward
  calls, private/exported linkage, and imported calls retain current behavior.
- Method/member conflicts and duplicate methods retain their behavior with
  method terminology in diagnostics.
- `self` remains unavailable outside method bodies.
- `impl` is an ordinary identifier and no live source uses it as a declaration
  keyword.
- Representative migrated method programs produce byte-identical C.
- Representative migrated struct and ADT constructor programs produce
  byte-identical C.

### Removed and misplaced forms

- Each legacy type form receives the specified diagnostic.
- `type Name is (A | B)` and other direct written aliases of structural sums
  are rejected in favor of `union ... end`.
- `type Name is union T end`, an empty ADT payload, and either ADT form with
  fewer than two variants are rejected.
- Missing commas and missing payload, struct, structural-union, or ADT `end`
  tokens are diagnosed at the earliest decisive token.
- `struct`, `union`, and `method` cannot be used as any identifier.
- Type-definition blocks are rejected in annotations, parameters, results,
  collection arguments, pointer pointees, and nested type expressions.
- Type and method declarations remain module-level only.

### Artifact and repository gates

- No pre-existing workbench manifest hash changes. The manifest gains entries
  only for snippets newly required by this RFC.
- Generated-C comparisons find no change for the recorded pre-RFC alias,
  structural union, non-empty object, ADT, generic, method, and cross-module
  baselines.
- Focused parser tests prove that every new type form feeds the intended
  existing semantic target kind and that the unit shorthand feeds the ordinary
  ADT target.
- No live test, workbench snippet, active spec, `AGENTS.md` current-syntax
  statement, or reference rule presents removed syntax as current.
- Archived specifications remain byte-identical.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.
- The complete tagged snippet catalog compiles under every available supported
  C23 toolchain without changing an existing manifest hash.

## Handoff

After implementation and validation, rebuild the workbench binary into `bin/`
and restart the running workbench. This is an operational handoff requirement,
not a language acceptance test.

## Consequences

- Types and instance behavior gain the direct keywords `type` and `method`.
- `struct`, `union`, and `method` become reserved; `impl` becomes an ordinary
  identifier.
- Empty structs consume one byte rather than relying on a non-portable
  zero-sized C object.
- The compact all-unit form is an ADT shorthand, not a second enum type system.
- Every canonical constructor is a direct type call. Structs and payload ADT
  variants use named `field = value` arguments; compiler-owned constructors
  retain positional signature parameters.
- `.new()` carries no constructor meaning; descriptive type-qualified
  operations remain closed compiler-owned APIs.
- Existing source migrates immediately; no compatibility syntax remains.

## Non-goals

- Snacc-style represented/newtype identity.
- Making aliases nominal or adding wrap/unwrap constructors.
- Integer-backed enums, explicit discriminant values, flags, or integer
  conversion.
- Treating ADT variants as independently declared nested types.
- Adding methods to ADTs, ADT variants, built-ins, structural unions, or
  imported types.
- Replacing Hexal's explicit value/Ptr/MutPtr receiver semantics with inferred
  method mutation.
- Virtual dispatch, overloading, extension methods, traits, or inheritance.
- Extracting methods as `Fun` values.
- User-defined static methods or associated functions. Compiler-owned
  type-qualified operations such as `String.from_bytes()` remain available and
  are not generalized by this RFC.
- User-defined constructors, constructor overloading, optional constructor
  parameters, or implicit field defaults.
- Anonymous struct or union definitions in general type-expression positions.
- General named function arguments or parameter labels.
- Changing match, narrowing, member access, type inference, generic inference,
  existing ABI, or existing C names.
- Preserving removed declarations as deprecated alternatives.
