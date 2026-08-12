# RFC 0006: Core Object Values

- Status: Implemented
- Features: nominal object types, named object literals, member access,
  read-only and mutable members, value copying, C23 structure lowering
- Created: 2026-08-07
- Revised: 2026-08-07
- Depends on: RFC 0004 (source and generated identifiers), RFC 0005 (type
  declarations and transparent aliases), ADR 0001 (structured checked
  expressions)
- Related proposals: RFC 0001 (`Ptr<T>`), RFC 0002 (`mut` and access
  capabilities), RFC 0003 (core scalar types), RFC 0007 (declaration-position
  pointer capabilities), RFC 0008 (functions and methods)

## Summary

An object type is a nominal, complete value type declared with a `type`
declaration and an object type expression:

```seawitch
type Point = {
    mut x: Int32,
    mut y: Int32,
}
```

An object value is created with an exhaustive named literal:

```seawitch
origin: Point = Point {
    x = 0,
    y = 0,
}
```

Members are read-only after construction unless declared with `mut`. Writing
a mutable member also requires a writable place at every embedded object step:

```seawitch
mut position: Point = origin
position.x = 10
```

Objects are inline C-like values. Construction, copying, assignment, nesting,
and address-taking add no allocation, ownership, hidden metadata, cleanup, or
runtime dispatch.

This first object RFC intentionally permits only scalar and previously
declared object member types. Pointer-valued members and recursive object
graphs are deferred to RFC 0007. Pointers may still refer to complete objects
and object members, so the core object model is useful and implementable
without deciding declaration-position pointer capability syntax.

Functions and methods are outside this RFC.

## Motivation

Seawitch needs a small aggregate model for game state, coordinates, colors,
configuration, protocol values, and later C structure interchange. The model
should preserve these principles:

1. every stored value has an explicit static type;
2. read-only storage is the default;
3. member mutation is explicit;
4. unrelated object types never mix because their fields happen to match;
5. object values have ordinary C value semantics;
6. generated C remains direct and readable; and
7. basic objects do not wait on recursive-layout, function, or method design.

## Guide-level explanation

### Declaring an object type

An object type expression is valid only as the direct right-hand side of a
module-level `type` declaration:

```seawitch
type Point = {
    x: Int32,
    y: Int32,
}
```

Every member has a name and an explicit type. The object must contain at least
one member. Members are stored in source order.

The following are not object declarations in this RFC:

```seawitch
point: { x: Int32 } = value       // Error: anonymous annotation.
type Box = { value: { x: Int32 } } // Error: anonymous nested type.
```

Name the nested type first:

```seawitch
type Point = {
    x: Int32,
    y: Int32,
}

type Box = {
    point: Point,
}
```

### Nominal identity

Each object type declaration creates a fresh nominal identity:

```seawitch
type Point = {
    x: Int32,
    y: Int32,
}

type Offset = {
    x: Int32,
    y: Int32,
}
```

`Point` and `Offset` are different types even though their members match.

```seawitch
point: Point = Point { x = 10, y = 20 }
offset: Offset = point // Error: expected Offset, got Point.
```

The declaration creates the identity; the source name is its binding and
display name, not the identity itself. An alias does not create another
identity:

```seawitch
type Position = Point

position: Position = Point { x = 10, y = 20 }
```

### Allowed member types

This RFC permits a member whose canonical type is:

- a core scalar type from RFC 0003; or
- a complete object type declared earlier under this RFC.

Transparent aliases of those types are permitted.

Pointer-valued members are rejected for now:

```seawitch
type Link = {
    next: Ptr<Link>, // Error: pointer-valued members await RFC 0007.
}
```

By-value recursive containment is also rejected because it has no finite
size:

```seawitch
type Impossible = {
    child: Impossible,
}
```

Declaration-order member resolution means a longer by-value cycle also fails
at its first forward reference. RFC 0007 adds the pointer link and forward
declaration rules needed for finite recursive structures.

### Constructing an object

An object literal names its type and supplies every member exactly once:

```seawitch
type Color = {
    red: UInt8,
    green: UInt8,
    blue: UInt8,
}

black: Color = Color {
    red = 0,
    green = 0,
    blue = 0,
}
```

Initializers may appear in any order. They are evaluated once in the written
order. A trailing comma is permitted.

Missing, duplicate, and unknown members are errors. Omitted members are not
implicitly zero-initialized. Positional and anonymous literals do not exist.
The explicit type name remains required when the expected type is already
known:

```seawitch
point: Point = Point { x = 1, y = 2 } // Valid.
point: Point = { x = 1, y = 2 }       // Error.
```

Each initializer is checked with its declared member type as the expected
type. This provides contextual numeric checking from RFC 0003.

An alias may name the constructed object:

```seawitch
type Position = Point
position: Position = Position { x = 1, y = 2 }
```

Both `Point { ... }` and `Position { ... }` construct the same nominal type.

### Reading members

The postfix `.` operator selects an object member:

```seawitch
x: Int32 = point.x
```

Lookup uses the receiver's canonical nominal type. It does not search
unrelated structurally similar types or global declarations.

Member selection on a temporary object value is valid:

```seawitch
x: Int32 = Point {
    x = 10,
    y = 20,
}.x
```

The literal is evaluated and the selected value is read. The literal is a
value, not a place: it cannot be assigned through or passed to `ref` or
`mut ref`. This non-addressability also prevents the automatic-storage
lifetime of its C compound literal from escaping through Seawitch.

### Read-only and mutable members

Members are read-only after initialization unless their declaration begins
with `mut`:

```seawitch
type Player = {
    maximum_health: Int32,
    mut health: Int32,
}
```

Both members are initialized normally:

```seawitch
mut player: Player = Player {
    maximum_health = 100,
    health = 80,
}
```

After construction, only `health` is a member assignment target:

```seawitch
player.health = 100         // Valid.
player.maximum_health = 200 // Error: member is read-only.
```

Member mode and binding mode are independent. A mutable member cannot be
written through a constant object binding:

```seawitch
fixed: Player = Player {
    maximum_health = 100,
    health = 80,
}

fixed.health = 100 // Error: fixed is a read-only place.
```

The member's `mut` is part of the nominal declaration, not per-instance state.
It emits no flag or qualifier into the stored object.

Expression-side `mut` does not make an object or its members mutable. An object
value is not itself reference-like, so `mut point` and `mut Point { ... }` are
type errors under RFC 0002. Mutation comes only from a writable place, mutable
member declarations, and explicit pointer capability where a pointer is used.

### Replacing a complete object

A read-only member prevents assignment through that member. It does not
prevent replacing a complete writable object place:

```seawitch
mut player: Player = first
player = second               // Valid.
player.maximum_health = 200   // Error.
```

Complete replacement targets `player`, whose binding is mutable. The incoming
`Player` is already fully initialized. The same rule applies to a complete
object place reached through a writable pointer.

### Nested objects

Objects may contain previously declared objects:

```seawitch
type Point = {
    mut x: Int32,
    mut y: Int32,
}

type Rectangle = {
    mut top_left: Point,
    bottom_right: Point,
}
```

Nested construction stays explicit:

```seawitch
mut area: Rectangle = Rectangle {
    top_left = Point { x = 0, y = 0 },
    bottom_right = Point { x = 100, y = 100 },
}
```

Every embedded member step on a write path must be mutable:

```seawitch
area.top_left.x = 5     // Valid.
area.bottom_right.x = 5 // Error: bottom_right is read-only.
```

### Copying

Object initialization and assignment copy the value of every member as C
structure value operations do:

```seawitch
mut destination: Point = source
destination = other
```

There is no allocation, object identity, reference count, retain, move,
destructor, or hidden callback.

The language does not promise that C padding bytes are copied, initialized,
stable, comparable, or observable. Padding is target-native non-value state.
Future byte-reinterpretation or foreign-layout features must define their own
rules without changing ordinary object assignment.

### Pointers to objects and members

An object or member place may be addressed using RFC 0002:

```seawitch
mut point: Point = Point { x = 1, y = 2 }

reader: Ptr<Point> = ref point
writer: Ptr<Point> = mut ref point
x_pointer: Ptr<Int32> = mut ref point.x
```

Pointer dereferencing remains explicit:

```seawitch
x: Int32 = reader.value.x
writer.value.x = 10
```

There is no automatic field dereference. RFC 0008 may add automatic
dereference for method calls only.

This support does not permit storing a pointer inside an object; that requires
the fixed declaration-position capability contract in RFC 0007.

## Reference-level explanation

### Grammar

RFC 0005's type declaration grammar becomes:

```ebnf
type-declaration           = "type" , identifier , "="
                           , type-definition-expression ;
type-definition-expression = type-expression
                           | object-type-expression ;

object-type-expression     = "{" , member-declaration
                           , { "," , member-declaration }
                           , [ "," ] , "}" ;
member-declaration         = [ "mut" ] , identifier , ":"
                           , type-expression ;

object-literal             = identifier , "{"
                           , [ member-initializer
                             , { "," , member-initializer }
                             , [ "," ] ]
                           , "}" ;
member-initializer         = identifier , "=" , expression ;

postfix-expression         = primary-expression
                           , { "." , identifier } ;
place-expression           = identifier , { "." , identifier } ;
primary-expression         = identifier
                           | object-literal
                           | integer-literal
                           | decimal-floating-literal
                           | "true"
                           | "false" ;
```

The numeric productions remain those in RFC 0003 and the canonical grammar.

An object type expression is allowed only directly after `type Name =`. It is
not a general annotation, member type, pointer element type, or anonymous
nested type.

Commas are required because newlines are whitespace rather than terminators.
An empty object type expression is invalid. An empty object literal is
syntactically valid and reaches the checker, which reports missing members.

`identifier { ... }` remains unambiguous while Seawitch uses Lua-like keyword
blocks (`then`, `do`, `end`). Any future brace-delimited statement block or
other expression-followed-by-brace form must revisit this grammar before
adoption.

### Object identity and member metadata

Checking a valid object declaration creates one compilation-scoped nominal
identity containing ordered member metadata:

- source name and location;
- stable member identity;
- canonical member type;
- member mode (`read-only` or `mutable`).

Identity equality uses a compiler-created declaration identity, not the
display name or member list. Aliases point to this canonical identity.

Member metadata must not make type equality depend on recursively comparing
complete object values. Pointer construction uses the canonical identity as
its interning key under RFC 0005.

Checked member selections and assignment paths retain the resolved member
identity under ADR 0001. They do not contain `sw_m_...` spellings or
pre-rendered C member expressions. The generator applies RFC 0004's
member-name mapping when it renders the structured access path.

### Object construction checking

For a literal `N { ... }`, the checker:

1. resolves `N` in the type namespace;
2. requires its canonical target to be a nominal object and otherwise reports
   that `N` is not an object type;
3. records each written initializer in source order;
4. rejects unknown or duplicate member names;
5. checks each initializer using the member's canonical expected type;
6. reports every required member that is absent; and
7. produces a complete object value with the object's canonical identity.

Initializer order does not change type identity or layout. Evaluation order is
the written literal order, not declaration order.

### Place-mode composition

RFC 0002 defines place mode as a left-to-right property of the access path.
This RFC adds one transition:

- selecting object member `m` produces a writable place exactly when the
  receiver place is writable and `m` is declared `mut`.

Existing transitions remain unchanged:

- a variable begins writable exactly when its binding has declaration-side
  `mut`; and
- `.value` begins the pointee place mode from that pointer layer's access
  capability, independent of whether the pointer slot itself can be rebound.

Consequently:

```seawitch
writer: Ptr<Point> = mut ref point
writer.value.x = 10
```

may be valid even though the `writer` binding is constant. `.value` obtains a
writable pointee place from `writer`'s write capability, then `.x` preserves
writability only if `x` is mutable.

For an embedded chain such as `area.top_left.x`, each member transition must
preserve writability. A read-only member makes the embedded storage below that
step read-only.

This rule is deliberately shallow with respect to separately referenced
storage. RFC 0007 must preserve that distinction when it introduces pointer
members: a read-only pointer slot cannot by itself freeze the independently
stored pointee.

`ref place` requires addressability. `mut ref place` additionally requires the
final composed place mode to be writable.

### Dotted-name resolution and `.addr` migration

The parser accepts any identifier after `.`. The checker resolves the meaning
from the receiver type:

- on `Ptr<T>`, `value` is the built-in dereference property;
- on an object, every name, including `value` or `addr`, is an ordinary member
  if declared; and
- otherwise the selection is invalid.

The implemented parser currently owns the migration diagnostic for the
removed `.addr` address-taking syntax. General member parsing moves that
decision to the checker and changes its category from `Syntax Error` to `Type
Error`.

The migration diagnostic is intentionally narrow:

- failed `.addr` on a scalar or pointer receiver reports
  `'.addr' is no longer supported; use 'ref'`, because those receiver
  categories supported the old syntax;
- an object with a declared `addr` member selects that member; and
- an object without an `addr` member reports the ordinary missing-member
  error.

This preserves useful migration help without treating a new object's likely
member typo as use of an older language feature. The migration diagnostic may
be removed in a later cleanup RFC after the transition period.

### C23 lowering

Object definitions are emitted at file scope in `main.h`, after required
includes and target-profile assertions and before executable uses:

```seawitch
type Point = {
    x: Int32,
    mut y: Int32,
}
```

lowers conceptually to:

```c
typedef struct sw_t_Point {
    int32_t sw_m_x;
    int32_t sw_m_y;
} sw_t_Point;
```

File-scope object definitions are emitted in source declaration order. That
order is dependency-safe because RFC 0006 permits members to use only built-in
types and previously declared aliases or objects. The generator must not emit
definitions by map iteration or another unstable order.

RFC 0007 preserves source-order emission of complete object definitions. It
emits any required incomplete C structure declarations before those
definitions rather than reordering them. Forward declarations remain outside
RFC 0006 because none of its accepted object layouts require them.

Read-only members do not become C `const` members. C member `const` would
prevent valid complete-object assignment and would not express Seawitch's
path-based rule. Member checks are complete before generation.

Every object literal lowers uniformly to a C compound literal with designated
members. Designators are emitted in the object's member declaration order,
independent of the order written in the literal:

```seawitch
Point { y = 2, x = 1 }
```

```c
(sw_t_Point){
    .sw_m_x = INT32_C(1),
    .sw_m_y = INT32_C(2),
}
```

Uniform compound literals are required because Seawitch literals are valid in
declaration initializers, assignment right-hand sides, nested expressions,
and temporary member selection. A bare C braced initializer would be invalid
in several of those contexts.

At block scope, the C compound literal has automatic storage duration for its
enclosing block. Seawitch treats the literal as a non-addressable value, so a
pointer to this lowering artifact cannot escape through valid Seawitch source.

If every initializer is proven effect-free, the generator may place those
expressions directly into the declaration-ordered designators. Otherwise it
emits typed temporaries immediately before the containing operation, evaluates
them exactly once in written source order, and places the temporary references
into declaration-ordered designators. It must not rely on C's unspecified
ordering among initializer expressions.

Object initialization and assignment use ordinary C structure value
operations. The generated structure has no hidden members, mutability flags,
type tags, ownership state, or method pointers.

### Header dependency discovery

Before emitting any header content, the generator walks all checked type
declarations and executable statement types. It recursively follows object
members and pointer elements using a visited set keyed by canonical identity.

This discovery determines all scalar target-profile assertions and declaration
dependencies. For example, a `Float32` used only inside an object member still
requires RFC 0003's floating target assertion before the object definition.

The traversal is cycle-safe even though RFC 0006 rejects recursive object
layouts, because RFC 0007 will add pointer cycles and the generic type graph
walker must not later need replacement.

### Source mapping

Generated file-scope object definitions and their structure-member declaration
lines receive `#line` mappings to the corresponding Seawitch declarations.
Generated scaffolding restores the generated C file mapping afterward.

An executable statement containing an object literal receives the statement's
ordinary single `#line` mapping before the complete lowering sequence. The
generator does not insert per-initializer directives inside a C compound
literal. Any source-ordered temporaries and the final compound literal remain
part of that containing statement's mapped lowering. Object lowering must not
make debugger and C diagnostics point only to generator internals.

### Diagnostics and phase ownership

The lexer owns tokenization of braces and commas. The parser owns malformed
object declarations and literals. The checker owns every failure requiring a
resolved type, member set, nominal identity, or place mode.

Representative diagnostics are:

```text
[Type Error] object type Point declares member x more than once
[Type Error] object type Point must declare at least one member
[Type Error] pointer-valued object members are not supported by RFC 0006
[Type Error] Coordinate is not an object type
[Type Error] Point literal is missing member y
[Type Error] Point has no member z
[Type Error] Point literal initializes member x more than once
[Type Error] expected Point, got Offset
[Type Error] cannot assign to read-only member player.maximum_health
[Type Error] cannot take mutable access through read-only member player.maximum_health
[Type Error] '.addr' is no longer supported; use 'ref'
```

Unknown checked object nodes fail closed in generation. No invalid object may
become an empty definition, zero initializer, placeholder comment, or omitted
statement.

## C23 correspondence

| Seawitch | Generated C23 |
|---|---|
| nominal object type | file-scope `typedef struct sw_t_Name { ... } sw_t_Name` |
| read-only member | ordinary structure member; enforced by checker |
| mutable member | ordinary structure member; enforced by checker |
| object literal | `(sw_t_Name){ .sw_m_field = value, ... }` |
| member read | `object.sw_m_member` |
| pointer-to-object member read | `pointer->sw_m_member` or equivalent explicit lowering |
| complete object assignment | C structure assignment |
| transparent object alias | canonical nominal C type at use sites |

Target-native structure padding, alignment, and member representation apply.
This RFC does not promise a stable foreign ABI layout contract.

## Drawbacks

The first implementation cannot store pointers in objects and therefore
cannot express linked lists, trees, graphs, parent links, intrusive data
structures, or recursive foreign structures. RFC 0007 owns that extension.
Keeping it separate allows useful flat and acyclic objects to ship without
guessing how pointer capabilities appear in declarations with no initializer.

The `type Name = { ... }` syntax is less visibly C-like than `struct Name`.
It preserves one type-naming mechanism for aliases and future type
constructors.

Named exhaustive literals repeat the type in an annotated declaration. The
repetition gives construction one context-independent spelling and keeps bare
braces available for future features.

Read-only members are enforced in Seawitch rather than encoded as C `const`
members. Manually edited generated C can violate the source rule, just as it
can violate other completed checker guarantees.

## Alternatives considered

### Block all objects on pointer-member capability syntax

Rejected. Pointers to objects and acyclic by-value aggregates are coherent
without pointer-valued members. The limitation is explicit and fail-closed.

### Add a dedicated `struct` declaration

Rejected. RFC 0005's `type` form already names type expressions and provides a
single extension point for later type constructors.

### Make objects structural

Rejected. Structural compatibility would mix unrelated domain types and make
future C ABI and method association less explicit.

### Make members mutable by default

Rejected. Read-only default matches bindings. `mut` is the one visible opt-in.

### Infer member writability from the containing binding

Rejected. A mutable binding may be replaced while selected identity-like
members remain read-only. Binding and member modes answer different questions.

### Forbid complete assignment when any member is read-only

Rejected. This would make binding assignability recursively type-dependent and
would reject ordinary C value replacement. Member mode governs a path through
that member, not every future byte change in its enclosing storage.

### Add positional, anonymous, or inferred literals

Rejected. Named, type-explicit construction remains readable under member
reordering and has one meaning in every context.

### Emit bare C designated initializers

Rejected. Bare braces are not expressions and cannot lower assignment
right-hand sides or temporary member selection. Compound literals work in all
specified contexts.

### Emit C `const` members

Rejected. They prevent complete structure assignment and do not model
Seawitch's path-based member access.

## Unresolved questions and separate RFCs

This RFC deliberately excludes:

- pointer-valued members, recursive objects, and forward declarations
  (RFC 0007);
- functions, methods, receivers, and method-call auto-dereference (RFC 0008);
- generic, sum, union, enum, packed, aligned, bit-field, flexible-array, and
  anonymous object forms;
- constructors, defaults, update literals, destructuring, and pattern matching;
- visibility, modules, imports, exports, and stable C ABI names;
- arrays, slices, lists, dictionaries, strings, and ownership-bearing members;
- equality, ordering, hashing, serialization, and byte reinterpretation; and
- allocation, cleanup, destructors, and move semantics.

## Implementation acceptance criteria

Implementation is complete when end-to-end tests prove that:

1. a direct nonempty `{ ... }` type right-hand side creates a fresh nominal
   object type;
2. identical member lists in separate declarations remain incompatible;
3. aliases preserve the original nominal identity;
4. object definitions follow source declaration order, while member order and
   generated C field order match member declaration order;
5. member names are unique and every member has an explicit allowed type;
6. scalar, aliased scalar, previous object, and aliased object members work;
7. pointer-valued, anonymous nested, forward, and recursive members fail with
   focused diagnostics;
8. literals require a resolved nominal object name and every member exactly
   once, reject non-object type names with the focused diagnostic, accept any
   initializer order and a trailing comma, and reject missing, duplicate, and
   unknown members;
9. each initializer is checked with its member's expected type and evaluated
   exactly once in written order;
10. object initialization and assignment require identical canonical nominal
    types and copy member values without allocation or hidden runtime action;
11. read-only members reject assignment and mutable members require the
    complete composed place to remain writable;
12. a complete writable object place may be replaced even when it contains
    read-only members;
13. nested member paths compose member modes as specified;
14. object and member places support `ref` and writable paths support
    `mut ref`;
15. `Ptr<Object>` supports explicit `.value` followed by member selection;
16. a constant write-capable pointer binding may modify a mutable pointee
    member;
17. temporary object member reads work and the temporary remains
    non-addressable;
18. a member named `value` or `addr` works as an ordinary object member;
19. failed `.addr` uses the migration diagnostic only for legacy receiver
    categories and otherwise uses ordinary member diagnostics;
20. all object literals lower to legal C compound literals whose designators
    follow member declaration order;
21. potentially effectful initializers lower through typed, source-ordered
    temporaries rather than relying on C initializer ordering;
22. definitions appear at file scope in source declaration order with C-safe
    type and member names, structure members receive declaration mappings, and
    object literals use only their containing statement's `#line` mapping;
23. checked member reads and writes follow ADR 0001 by carrying stable member
    identities and structured access paths rather than generated C names or
    expressions;
24. recursive type-graph discovery finds scalar requirements reachable only
    through object members before header emission;
25. generated structures contain no hidden runtime state; and
26. every syntax and checked node is explicitly handled or fails closed.

## Implementation handoff requirements

The implementation plan must identify:

1. lexer additions for braces and commas;
2. parsed nodes for object types, members, literals, initializers, and general
   dotted selection;
3. checker representation for nominal and member identities, ordered member
   metadata, and structured member access paths compatible with ADR 0001;
4. declaration-order resolution and rejection of unsupported member types;
5. expected-type checking and exhaustive literal validation;
6. the RFC 0002 place-mode fold extended by object member selection;
7. migration of restricted-property parser tests to checker and end-to-end
   coverage with the narrowed `.addr` rule;
8. a generator header/file-scope definition region that preserves source
   declaration order;
9. canonical compound-literal lowering with declaration-ordered designators
   and written-order temporary evaluation;
10. recursive, visited-set type dependency discovery before header output;
11. generated-name use from RFC 0004 and the distinct `#line` rules for
    structure declarations and executable object literals; and
12. focused unit tests plus end-to-end cases in `compiler/compile_test.go`.

Canonical grammar, language, and status documents are updated once after
implemented behavior stabilizes. This feature does not require an analyzer
pass unless effectful expression lowering has already created a checked-to-
analyzed representation by implementation time.
