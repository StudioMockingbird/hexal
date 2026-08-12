# RFC 0005: Type Declarations and Transparent Aliases

- Status: Implemented
- Features: type declarations, transparent aliases, shared declaration names,
  compilation-scoped type identity
- Created: 2026-08-06
- Revised: 2026-08-07
- Depends on: RFC 0004 (source and generated identifiers)
- Related proposals: RFC 0001 (`Ptr<T>`), RFC 0002 (`mut` and access
  capabilities), RFC 0003 (core scalar types), RFC 0006 (core object values)

## Summary

Seawitch gains one top-level form for naming a type:

```seawitch
type Coordinate = Int32
type CoordinatePtr = Ptr<Coordinate>
```

In this RFC, the right-hand side must resolve to an already declared type.
The declaration creates a transparent alias: both names denote the same
canonical type, use the same values, and require no conversion.

RFC 0006 extends the same form with an object type expression:

```seawitch
type Point = {
    x: Int32,
    y: Int32,
}
```

This RFC also establishes the type-system foundations shared by all future
user-defined types:

1. visible type names and value names cannot share a source spelling;
2. built-in type names cannot be redeclared;
3. type environments and constructed-type interning live for one compilation;
4. top-level type declarations have an explicit checked representation; and
5. a source file containing only type declarations is a valid program.

Aliases are source-level names. They do not require C `typedef` declarations.
Every use lowers through the canonical C type.

## Motivation

Aliases make domain vocabulary readable without creating wrappers:

```seawitch
type EntityId = UInt64
type PositionPtr = Ptr<Position>
```

The same declaration form can later name nominal objects, sum types, and
generic constructions. Defining the naming rules first keeps the initial
object implementation focused on aggregate semantics rather than mixing it
with namespace and compiler-lifetime decisions.

## Changes to implemented behavior

The current parsed and checked program representations also store only
executable statements. This RFC adds explicit non-executable top-level type
declarations and permits a type-declarations-only source file.

## Guide-level explanation

### Transparent aliases

An alias is another spelling of an existing type:

```seawitch
type Coordinate = Int32

x: Coordinate = 10
y: Int32 = x
```

`Coordinate` and `Int32` are identical. The alias adds no range, validation,
layout, allocation, ownership, or runtime behavior.

Alias chains are permitted:

```seawitch
type Coordinate = Int32
type HorizontalCoordinate = Coordinate
```

Both aliases resolve to the canonical `Int32` type.

Aliases may name constructed pointer types:

```seawitch
type Int32Ptr = Ptr<Int32>
type Int32PtrPtr = Ptr<Int32Ptr>
```

The alias does not determine a pointer capability shape. Under RFC 0002, a
pointer declaration's initializer still establishes that binding's shape:

```seawitch
mut number: Int32 = 42

reader: Int32Ptr = ref number
writer: Int32Ptr = mut ref number
```

### Declaration order

Alias targets must already be declared:

```seawitch
type Coordinate = Int32 // Valid: Int32 is built in.
type Distance = Coordinate // Valid: Coordinate appears first.
```

Forward references are rejected:

```seawitch
type Distance = Coordinate // Error: Coordinate is not declared yet.
type Coordinate = Int32
```

An alias cannot contain its own declaration name anywhere in its right-hand
type expression:

```seawitch
type Coordinate = Coordinate         // Focused self-reference error.
type CoordinatePtr = Ptr<CoordinatePtr> // The same error.
```

The checker detects this before ordinary name lookup so it does not misleadingly
report the just-declared name as unknown. Declaration-order resolution prevents
longer alias cycles because the first forward edge fails before an invalid
declaration enters the type environment.

This rule avoids a separate forward-declaration pass for aliases. RFC 0007 may
add narrowly scoped forward declarations for recursive object layouts without
changing ordinary alias resolution.

### Type and value declaration names

Type positions and value positions remain syntactically distinct, but visible
type and value declarations share one source-name constraint. A spelling may
name a type or a value in a scope, never both:

```seawitch
type Coordinate = Int32
Coordinate: Int32 = 10
// Error: Coordinate is already declared as a type.
```

The reverse order is also invalid:

```seawitch
distance: Int32 = 10
type distance = Int32
// Error: distance is already declared as a value.
```

Built-in and protected type names participate in the same rule:

```seawitch
Int32: UInt32 = 10 // Error: Int32 is already a built-in type.
Ptr: Int32 = 10    // Error: Ptr is a built-in type constructor.
```

The compiler may keep kind-specific lookup tables internally. Before accepting
a type or value declaration, it must check every visible declaration kind that
could claim the spelling. In `x: Coordinate`, `Coordinate` is still required
to resolve to a type; in a value expression, a type-only name produces an
unknown-value or wrong-kind diagnostic rather than becoming a value.

This RFC introduces module-level type declarations. Future modules, imports,
local types, functions, and nested scopes must preserve the rule: a value may
not shadow a visible type, and a type may not shadow a visible value.

### Protected type names

The following names cannot be introduced by `type`:

- every built-in scalar type from RFC 0003;
- `Ptr`; and
- every future built-in type constructor or reserved type name.

For example:

```seawitch
type Int32 = UInt32 // Error: Int32 is built in.
type Ptr = UInt64   // Error: Ptr is a built-in type constructor.
```

Keywords are not identifiers and therefore cannot be declaration names in any
namespace.

### Type-only programs

A source file does not need an executable statement:

```seawitch
type Coordinate = Int32
type CoordinatePtr = Ptr<Coordinate>
```

This is a valid program. Aliases emit no C declarations, so its `main.c` is the
normal successful empty program:

```c
#include "main.h"

int main(void) {
    return EXIT_SUCCESS;
}
```

`main.h` contains the normal generated header scaffolding required by the
target. The type declarations are still parsed and checked even though neither
alias emits a `typedef` or executable statement.

## Reference-level explanation

### Grammar

The grammar added by this RFC is:

```ebnf
program          = { top-level-item } ;
top-level-item   = type-declaration | statement ;

type-declaration = "type" , identifier , "=" , type-expression ;
type-expression  = identifier
                 | "Ptr" , "<" , type-expression , ">" ;
```

RFC 0006 extends the right-hand side of `type-declaration`; it does not add a
second declaration keyword.

`type` becomes a reserved word. `Ptr` remains an identifier recognized as a
built-in constructor in type position; it is protected from declaration in
the type namespace.

### Resolution and canonicalization

The checker processes top-level items in source order.

For `type A = R` where `R` is a transparent-alias type expression defined by
this RFC:

1. reject `A` if it is already declared as a type or value, or protected in the
   type namespace;
2. if `R` contains a reference to `A`, report a self-reference error and fail
   without inserting `A`;
3. resolve every name in `R` against the current type environment;
4. fail without inserting `A` if resolution or construction fails; and
5. bind `A` to the resolved canonical type.

The self-reference check in step 2 is alias-only. A transparent alias creates
no canonical identity of its own, so a self-reference has no finite canonical
expansion:

```seawitch
type Node = Ptr<Node> // Invalid transparent alias.
```

This algorithm does not govern a nominal type expression introduced by a later
RFC. A nominal declaration creates a fresh canonical identity before its body
is resolved, so a reference to that identity through a pointer is finite:

```seawitch
type Node = {
    next: Ptr<Node>,
}
```

RFC 0006 currently rejects that example because pointer-valued members are not
yet supported. RFC 0007 may admit the same recursive relationship using its
eventual explicit pointer-capability syntax. Each nominal-type RFC owns its
recursive-layout and forward-resolution rules; it does not inherit the
transparent-alias self-reference rejection.

A canonical type identity is an opaque stable value. Built-in types have
predefined identities. Each nominal type introduced by a later RFC receives a
fresh identity within its compilation. An implementation may represent the
identity as a compilation-local index or a pointer to a compilation-owned
canonical type record; display names are metadata and never participate in
identity.

An alias retains its target's canonical identity rather than creating a new
one. Equality, assignment compatibility, member lookup, pointer construction,
and C lowering use that identity. Diagnostics may retain the written alias
spelling for clarity.

Constructed types such as `Ptr<T>` are interned by canonical component
identity, never by a display string such as `"Ptr<Point>"`. Conceptually, the
interner key for a pointer is `(Ptr, canonical-identity-of-T)`. Therefore:

```seawitch
type Coordinate = Int32
type A = Ptr<Coordinate>
type B = Ptr<Int32>
```

`A` and `B` resolve to the same canonical pointer type because `Coordinate`
retains `Int32`'s canonical identity.

### Compilation lifetime

The following state belongs to one compilation invocation:

- the type environment;
- alias bindings;
- nominal identities introduced by later RFCs;
- constructed-type interning;
- pointer capability metadata.

No package-level mutable cache may retain source-defined types between
compilations. This is required for the long-running workbench: recompiling a
changed declaration must not retrieve a constructed pointer whose element is
the previous compilation's stale type.

Built-in immutable type descriptors may be shared globally if they contain no
compilation-specific state.

### Checked top-level representation

The parsed and checked program representations must model top-level items
explicitly. A type declaration is not an executable statement and must not be
inserted into a statement list or emitted inside `main`.

The implementation may use either:

- one ordered list of tagged top-level items; or
- separate declaration and statement collections plus preserved source-order
  information where later semantics require it.

The representation must support a program containing no statements. Unknown
top-level node kinds fail closed with a compiler diagnostic.

### C lowering of aliases

Aliases emit no required `typedef`:

```seawitch
type Coordinate = Int32
x: Coordinate = 10
```

lowers through the canonical scalar type:

```c
const int32_t sw_v_x = INT32_C(10);
```

This avoids C qualifier-placement traps for pointer aliases. For example,
Seawitch can lower a constant pointer binding with read access directly to the
appropriate canonical declarator instead of applying `const` to an opaque C
pointer typedef.

### Diagnostics and phase ownership

The lexer recognizes `type`. The parser owns malformed declaration and type-
expression syntax. The checker owns name resolution and type construction.

Representative diagnostics are:

```text
[Type Error] type Coordinate is already declared
[Type Error] built-in type Int32 cannot be redeclared
[Type Error] built-in type constructor Ptr cannot be redeclared
[Type Error] unknown type Coordinate
[Type Error] type alias Coordinate cannot reference itself
```

An invalid declaration is not entered into the environment. Later uses may
therefore report that its name is unknown, but code generation never receives
a partially resolved alias.

## C23 correspondence

| Seawitch construct | Generated C23 |
|---|---|
| transparent scalar alias | canonical scalar spelling at each use |
| transparent pointer alias | canonical pointer declarator at each use |

Aliases have no runtime representation and no standalone outgoing C type.
All emitted identifiers follow RFC 0004.

## Drawbacks

The shared declaration-name constraint forbids a local value from reusing a
visible type name even though syntactic position could distinguish them. This
slightly reduces shadowing freedom in exchange for one obvious meaning per
spelling and simpler generated-C interoperability.

Not emitting C typedefs for aliases means the generated C does not preserve
every source alias spelling. It does preserve the canonical representation and
avoids misleading ABI distinctions that do not exist in Seawitch.

Declaration-order resolution excludes forward alias references. This keeps
the first implementation small; recursive nominal types are addressed
separately by RFC 0007.

## Alternatives considered

### Allow type and value declarations to share a spelling

Rejected. Syntactic position could distinguish the declarations and RFC 0004
would generate different C prefixes, but the source would assign two unrelated
meanings to one visible word. Generated naming must not broaden the language's
declaration semantics.

### Emit aliases as C typedefs

Rejected as a requirement. Pointer typedefs obscure which pointer layer a C
qualifier applies to. The generator may emit a readability-only alias in a
future debug mode only if doing so cannot affect semantics or ABI.

### Make aliases nominal newtypes

Rejected. `type Coordinate = Int32` is a transparent alternate spelling.
Nominal scalar wrappers need construction and conversion rules and belong to
a future newtype feature.

### Keep constructed-type interning global

Rejected. A long-running compiler process can compile different source types
with the same display name. Global name-keyed interning can silently reuse a
stale component type from an earlier compilation.

## Unresolved questions and separate RFCs

This RFC does not define:

- nominal object type expressions and object values (RFC 0006);
- declaration-position pointer capabilities and recursive objects (RFC 0007);
- functions, methods, or receiver behavior (RFC 0008);
- module-qualified type names, imports, exports, or visibility;
- generic type declarations;
- nominal scalar newtypes; or
- stable C ABI names and foreign declaration import.

## Implementation acceptance criteria

Implementation is complete when end-to-end tests prove that:

1. `type` is reserved and introduces a top-level declaration;
2. every declaration requires a valid explicit right-hand type expression;
3. aliases and alias chains resolve to one canonical type;
4. alias use adds no conversion or runtime representation;
5. aliases of nested `Ptr<T>` constructions lower through canonical C
   declarators;
6. duplicate, protected, unknown, and forward names fail with structured
   diagnostics, while direct or nested self-reference in a transparent alias
   receives the focused self-reference diagnostic before ordinary lookup;
7. an invalid alias never enters the type environment;
8. a type and value with the same visible spelling are rejected in either
   declaration order, including collisions with built-in types;
9. a type-only source file emits the normal generated header and the specified
   successful empty `main`;
10. parsed and checked programs represent type declarations outside the
    executable statement list;
11. aliases emit no required C typedef;
12. aliases retain their target identity and constructed types are interned by
    canonical component identity rather than display names;
13. compiling changed source twice in one process cannot retrieve types from
    the first compilation; and
14. every new or rejected node is handled explicitly and fail-closed in every
    compiler stage.

## Implementation handoff requirements

The implementation plan must identify:

1. lexer and parser additions for `type` and top-level items;
2. the parsed and checked representation of non-executable declarations;
3. a compilation-owned type environment and constructed-type interner;
4. opaque canonical identities, alias target-identity sharing, the alias-only
   self-reference check, and component-identity interner keys without copied
   identity anchors;
5. protected built-in names, kind-specific lookup, and the shared
   type/value declaration-name check;
6. the deterministic header and empty-`main` output for a type-only program;
7. focused stage tests and end-to-end cases in `compiler/compile_test.go`; and
8. updates to the canonical grammar, language, and status documents only after
   implemented behavior stabilizes.

The pipeline remains forward-only and fail-closed. This feature does not
require an analyzer pass.
