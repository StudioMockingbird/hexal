# RFC 0007: Mutability Redesign

- Status: Implemented
- Features: declaration-only `mut`, `Ptr<T>` and `MutPtr<T>` raw pointer
  constructors, single `ref` with contextual capability, pointer-valued object
  members, finite self-recursive objects, uniform split C structures
- Created: 2026-08-07
- Revised: 2026-08-07
- Depends on: RFC 0001 (raw pointers), RFC 0002 (default constants and mutable
  access), RFC 0005 (type declarations and aliases), RFC 0006 (core objects)
- Followed by: RFC 0008 (functions and methods)
- Supersedes when accepted: RFC 0002's pointer access capabilities,
  expression-side `mut`, and `mut ref`; RFC 0005's single-constructor
  type-expression production; RFC 0006's capability-based place-mode
  composition, identical-type pointer compatibility, and combined C structure
  lowering

## Summary

`mut` marks a storage slot as replaceable. It appears in exactly two positions,
both declarations, and means the same thing in both: before a binding name and
before an object member name.

```seawitch
answer: Int32 = 42       // fixed storage
mut score: Int32 = 0     // writable storage
```

Pointee access is a property of the pointer *type*, not of the pointer value.
There are two raw pointer constructors:

```seawitch
reader: Ptr<Int32> = ref score        // may read the pointee
writer: MutPtr<Int32> = ref score     // may read and write the pointee

current: Int32 = reader.value         // Valid
reader.value = 10                     // Error: Ptr<Int32> cannot write
writer.value = 10                     // Valid
```

`ref` is the only address-taking form, and there is no expression-side marker.
It yields `MutPtr<T>` from a writable place and `Ptr<T>` from a fixed one, and
`MutPtr<T>` weakens to `Ptr<T>` on assignment. Every case then falls out of
ordinary type checking:

```seawitch
mut score: Int32 = 0
answer: Int32 = 42

writer: MutPtr<Int32> = ref score     // exact match
reader: Ptr<Int32> = ref score        // MutPtr weakens to Ptr
view: Ptr<Int32> = ref answer         // exact match: a read-only view
bad: MutPtr<Int32> = ref answer       // Error: ref answer is Ptr<Int32>
```

An object type declares which of its members are replaceable. Object literals
supply values only:

```seawitch
type Player = {
    id: UInt64,
    mut health: Int32,
}

mut player: Player = Player {
    id = 1,
    health = 100,
}

player.health = 90    // Valid
player.id = 2         // Error: id is a fixed member
```

Every removed form:

```seawitch
mut ref value              // Syntax Error
alias: Ptr<Int32> = mut p  // Syntax Error
Ptr<mut Int32>             // Syntax Error
T { mut x = 1 }            // Syntax Error
```

Pointer members are now ordinary, so finite self-recursive objects work:

```seawitch
type Node = {
    value: Int32,
    mut next: MutPtr<Node>,   // mut: the link may be repointed
}                             // MutPtr: the next node may be written
```

The two markers on that member are independent and neither implies the other.

## Motivation

RFC 0002 gives two values of one written type different hidden capabilities:

```seawitch
reader: Ptr<Int32> = ref value
writer: Ptr<Int32> = mut ref value
```

That distinction must propagate through copies, branches, nested pointers,
object members, parameters, returns, and collections. It buys
const-correctness, and it prevents nothing else: not null access, dangling
pointers, out-of-bounds reads, double frees, or aliasing.

It also has no C structure representation. C gives one declarator to every
instance of a struct:

```c
struct Holder { int32_t *pointer; };
```

The same member cannot be `const int32_t *` in one `Holder` and `int32_t *` in
another without two distinct C types. Any per-value capability therefore forces
discarded qualifiers, generated casts, or duplicated structures.

Naming the capability in the type fixes exactly that. `Ptr<Int32>` and
`MutPtr<Int32>` are different types, so a `Holder` whose member is `Ptr<Int32>`
has one C declarator and one contract for every instance. Nothing travels with
a value, so nothing needs propagating, intersecting at a control-flow merge, or
re-deriving at a function boundary.

RFC 0002's §Alternatives rejected a two-constructor model in favour of one
pointer type plus a tracked capability, on the grounds that the capability
generalised better to future collections. That capability system is removed
here, so the comparison is no longer two constructors versus one plus tracking —
it is two constructors versus no way to express a read-only pointer at all.
The earlier rejection does not carry over.

## Changes to implemented behavior

`Ptr<T>` changes meaning. It currently denotes a pointer whose pointee access
was established by its initializer; it now denotes a read-only pointer. Every
writable pointer in existing source becomes `MutPtr<T>`:

```seawitch
// before
mut x: Int32 = 0
reader: Ptr<Int32> = ref x
writer: Ptr<Int32> = mut ref x

// after
mut x: Int32 = 0
reader: Ptr<Int32> = ref x
writer: MutPtr<Int32> = ref x
```

The mechanical migration is: a declaration whose initializer used `mut ref`
becomes `MutPtr<T>` with plain `ref`; a declaration that used `mut pointer` to
propagate write access becomes `MutPtr<T>` with a plain copy; everything else
keeps `Ptr<T>`.

Generated C changes for every pointer that remains `Ptr<T>`, which now
qualifies its pointee:

```c
// before: Ptr<Int32> with write capability
int32_t *const sw_v_reader = &sw_v_x;

// after: Ptr<Int32>
const int32_t *const sw_v_reader = &sw_v_x;
```

The following implemented behavior is removed rather than renamed:

- expression-side `mut`, including `mut ref place` and `mut pointer`;
- per-value access capability on bindings and checked operands, with its clone,
  compare, and attenuate helpers;
- silent attenuation of a pointer copy, which is now an explicit weakening rule
  that only moves `MutPtr<T>` to `Ptr<T>`; and
- the generator's separate capability argument, since pointee qualification now
  comes from the type.

Existing stage tests that assert capability state or `mut ref` diagnostics move
or are deleted with this change, and generated-C snapshots are updated as one
intentional migration.

## Guide-level explanation

### One marker, one meaning

`mut` on a binding is the outer gate. Without it nothing in that storage can be
written; with it, a slot is writable when its own declaration also allows:

```seawitch
player: Player = Player { id = 1, health = 100 }
player.health = 90    // Error: player is read-only
player = other        // Error: player is read-only

mut rival: Player = Player { id = 2, health = 100 }
rival.health = 90     // Valid: binding is mut, health is mut
rival.id = 5          // Error: id is a fixed member
rival = other         // Valid: replaces the whole binding
```

Scalars and objects follow the same binding rule:

```seawitch
x: Int32 = 5
x = 6                 // Error

mut y: Int32 = 5
y = 6                 // Valid
```

Nothing in an object *literal* can grant or withhold write access. A `Player`
obtained from a fixed binding is fixed; the same `Player` copied into a `mut`
binding is writable:

```seawitch
mut copy: Player = player
copy.health = 50      // Valid
```

Mutability is a property of storage, never of a value. It cannot be copied,
merged, or lost.

### Fixed members

A member declaration without `mut` names a slot that is never replaceable, in
any instance of that type:

```seawitch
type Player = {
    id: UInt64,
    mut health: Int32,
}

mut player: Player = Player { id = 1, health = 100 }
player.health = 90    // Valid: binding is mut, member is mut
player.id = 2         // Error: id is a fixed member
player = other        // Valid: replaces the whole binding
```

Both gates must pass. A `mut` binding does not unlock a fixed member, and a
`mut` member is not writable through a fixed binding:

```seawitch
rival: Player = Player { id = 2, health = 100 }
rival.health = 90     // Error: rival is read-only
```

Member modes belong to the nominal type, so every `Player` has the same
contract. Nothing is carried by a value, copied, or re-derived at a boundary —
`Ptr<Player>` conveys the member modes automatically because they are a
property of `Player`.

Replacing the whole binding remains legal even when the type contains fixed
members, because that assignment targets the binding, not a member path.

### Two pointer constructors

`Ptr<T>` reads its pointee. `MutPtr<T>` reads and writes it.

```seawitch
mut score: Int32 = 10

reader: Ptr<Int32> = ref score
writer: MutPtr<Int32> = ref score

current: Int32 = reader.value    // Valid: both constructors read
reader.value = 20                // Error: Ptr<Int32> cannot write its pointee
writer.value = 20                // Valid
```

Repointing the pointer *binding* is a separate question, answered by `mut` as
everywhere else:

```seawitch
mut first: Int32 = 1
mut second: Int32 = 2

fixed: MutPtr<Int32> = ref first
fixed.value = 10          // Valid: MutPtr writes its pointee
fixed = ref second        // Error: fixed is a read-only binding

mut moving: MutPtr<Int32> = ref first
moving = ref second       // Valid
```

Four independent combinations, all spelled with the two markers already in the
language:

| Declaration | Repoint | Write pointee |
|---|---|---|
| `p: Ptr<T> = ref x` | no | no |
| `mut p: Ptr<T> = ref x` | yes | no |
| `p: MutPtr<T> = ref x` | no | yes |
| `mut p: MutPtr<T> = ref x` | yes | yes |

### Address-taking

`ref` is the only address-taking form, and the *place* decides what it produces:
`MutPtr<T>` from a writable place, `Ptr<T>` from a fixed one. Nothing about the
surrounding declaration is consulted.

```seawitch
mut total: Int32 = 42
answer: Int32 = 42

ref total     // MutPtr<Int32>
ref answer    // Ptr<Int32>
```

Every case then falls out of ordinary type checking, with weakening covering
the one direction that is safe:

```seawitch
q: MutPtr<Int32> = ref total         // exact match
view: Ptr<Int32> = ref total         // MutPtr weakens to Ptr
look: Ptr<Int32> = ref answer        // exact match
bad: MutPtr<Int32> = ref answer      // Error: expected MutPtr<Int32>,
                                     // got Ptr<Int32>
```

The same walk applies through members, using both gates from the earlier
sections:

```seawitch
mut rival: Player = Player { id = 2, health = 100 }

hp: MutPtr<Int32> = ref rival.health   // health is writable: MutPtr<Int32>
tag: Ptr<UInt64> = ref rival.id        // id is fixed: Ptr<UInt64>
badge: MutPtr<UInt64> = ref rival.id   // Error: expected MutPtr<UInt64>,
                                       // got Ptr<UInt64>
```

No dedicated "cannot take a mutable pointer" diagnostic is needed; the
mismatch reports it.

`ref` does not allocate, copy, extend a lifetime, create ownership, or add a
runtime check.

### Copying and weakening a pointer

Copying a pointer of the same type copies an address and nothing else:

```seawitch
alias: MutPtr<Int32> = writer
alias.value = 100
```

A `MutPtr<T>` may also initialize or be assigned to a `Ptr<T>`, giving up write
access:

```seawitch
observer: Ptr<Int32> = writer     // Valid: weakening
promoted: MutPtr<Int32> = reader  // Error: Ptr<Int32> has no write access
```

This is the one implicit conversion in the language and it moves in one
direction only. Nothing is attenuated at runtime; the two types have identical
representation.

### Recursive objects

A nominal object may reach itself behind at least one pointer layer. Both
constructors qualify, and the choice states what the link permits:

```seawitch
type Node = {
    value: Int32,
    mut next: MutPtr<Node>,   // a mutable list: relink, and write later nodes
}

type Frozen = {
    value: Int32,
    next: Ptr<Frozen>,        // a read-only chain: walk it, change nothing
}
```

A by-value cycle has no finite size and remains invalid:

```seawitch
type Impossible = {
    value: Impossible,
}
```

Mutual recursion is still rejected, because RFC 0005 resolves declarations in
source order:

```seawitch
type A = { b: Ptr<B> }   // Error: B is not declared yet
type B = { a: Ptr<A> }
```

This RFC makes recursive layout legal. Constructing a terminating node awaits
`Nil`, allocation, or a foreign pointer source.

## Reference-level explanation

### Grammar

Only the two declaration productions carry `mut`:

```ebnf
declaration        = [ "mut" ] , identifier , ":" , type-expression
                   , "=" , expression ;

member-declaration = [ "mut" ] , identifier , ":" , type-expression ;
member-initializer = identifier , "=" , expression ;

type-expression    = identifier
                   | pointer-constructor , "<" , type-expression , ">" ;
pointer-constructor = "Ptr" | "MutPtr" ;

unary-expression   = "ref" , unary-expression
                   | postfix-expression ;
```

There is no expression-side `mut` production, no `mut` in a type expression,
and no `mut` in a member initializer. Both remaining positions precede the name
of a storage slot.

`MutPtr` joins `Ptr` as a built-in type constructor recognised only in type
position. Both are protected names under RFC 0005 and cannot be redeclared.

### Place rules

Place checking is a left-to-right walk with three cases:

1. a variable place is writable exactly when its binding is declared `mut`;
2. `.member` is writable exactly when its receiver place is writable and the
   member is declared `mut`; and
3. `.value` is writable exactly when its receiver's type is `MutPtr<T>`.

Assignment requires a writable place. `ref place` requires an addressable
place; writability is not required, because a read-only place still yields a
usable `Ptr<T>`.

Case 3 is what keeps slot and pointee independent. It reads the receiver's
*type*, not its place mode, so a fixed `MutPtr<T>` binding still yields a
writable pointee and a `mut Ptr<T>` binding still does not.

### Pointer rules

1. `Ptr<T>` and `MutPtr<T>` are nullable, non-owning raw pointers to `T` with
   identical representation, size, and alignment.
2. Both permit reading `pointer.value`. Only `MutPtr<T>` permits assigning it.
3. `ref place` requires an addressable place and produces `MutPtr<T>` when the
   place is writable and `Ptr<T>` otherwise. There is no second address-taking
   form and no expression-side marker.
4. `MutPtr<T>` initializes or is assigned to `Ptr<T>`. The reverse is a type
   error. This weakening is permitted only at the outermost pointer layer,
   with every layer below identical. It applies wherever an expected type is
   supplied, including object member initializers, and is the one exception to
   RFC 0006's rule that a typed value must have the identical expected type:

   ```seawitch
   type Config = { name: Ptr<UInt8> }

   mut buffer: UInt8 = 0
   c: Config = Config { name = ref buffer }   // MutPtr<UInt8> weakens
                                              // to Ptr<UInt8>
   ```
5. Apart from rule 4, pointer declarations and assignments require identical
   canonical types.
6. Addressing a pointer-valued place produces a pointer whose element is that
   place's pointer type, under rule 3.
7. Neither constructor carries per-value state. Two values of one pointer type
   always have the same pointee access.

Rule 4 is deliberately shallow. Weakening at a deeper layer is the classic C
hole — `int **` does not convert to `const int **` — and Seawitch rejects it
for the same reason:

```seawitch
inner: MutPtr<Int32> = ref score
outer: MutPtr<MutPtr<Int32>> = ref inner

ok: Ptr<MutPtr<Int32>> = outer     // Valid: outermost layer only
no: Ptr<Ptr<Int32>> = outer        // Error: cannot weaken an inner layer
```

These rules prove nothing about validity, lifetime, bounds, alignment,
provenance, allocation state, or exclusive access.

### Object rules

1. An object type expression contains member names, types, and member modes.
2. An object literal contains member names and values only.
3. Member access writability requires both a writable receiver place and a
   `mut` member declaration. It never comes from a value.
4. Object copying copies member values. Member modes belong to the type, so
   nothing mutability-related is carried by the copy.
5. Complete-object assignment requires only that the target place be writable.
   Member modes do not participate; the incoming value is already complete.
6. Member modes emit nothing into generated C. A `mut` member and a fixed
   member produce identical structure members.

### Nominal self-resolution

For `type N = { ... }`, the checker:

1. rejects `N` if unavailable under RFC 0005;
2. allocates a provisional nominal identity;
3. makes only that identity visible while checking its members;
4. permits a path back to it only through at least one `Ptr` layer;
5. rejects every by-value path back to it;
6. discards the provisional identity if checking fails; and
7. finalizes and binds it only after every member succeeds.

No module-wide forward-name collection is added.

### C23 lowering

Binding mutability maps directly onto C `const`, and because a fixed binding
forbids member writes too, the C type now enforces the same rule Seawitch does:

```seawitch
answer: Int32 = 42
mut score: Int32 = 0
origin: Point = Point { x = 0, y = 0 }
mut cursor: Point = Point { x = 0, y = 0 }
```

```c
const int32_t sw_v_answer = INT32_C(42);
int32_t sw_v_score = INT32_C(0);
const sw_t_Point sw_v_origin = (sw_t_Point){ .sw_m_x = INT32_C(0), .sw_m_y = INT32_C(0) };
sw_t_Point sw_v_cursor = (sw_t_Point){ .sw_m_x = INT32_C(0), .sw_m_y = INT32_C(0) };
```

Taking the address of a fixed binding is legal and yields `Ptr<T>`, whose
pointee is `const` in C, so the generator never discards a qualifier:

```seawitch
origin: Point = Point { x = 0, y = 0 }
view: Ptr<Point> = ref origin
```

```c
const sw_t_Point *const sw_v_view = &sw_v_origin;
```

Each pointer layer contributes its own pointee qualification, taken from the
constructor at that layer. The binding contributes the trailing `const`:

| Seawitch | C23 |
|---|---|
| `Ptr<Int32>` | `const int32_t *` |
| `MutPtr<Int32>` | `int32_t *` |
| `Ptr<Ptr<Int32>>` | `const int32_t *const *` |
| `MutPtr<Ptr<Int32>>` | `const int32_t **` |
| `Ptr<MutPtr<Int32>>` | `int32_t *const *` |
| `MutPtr<MutPtr<Int32>>` | `int32_t **` |
| fixed pointer binding | `… *const name` |
| mutable pointer binding | `… *name` |

```seawitch
mut value: Int32 = 42
writer: MutPtr<Int32> = ref value
reader: Ptr<Int32> = ref value
```

```c
int32_t sw_v_value = INT32_C(42);
int32_t *const sw_v_writer = &sw_v_value;
const int32_t *const sw_v_reader = &sw_v_value;
```

The declarator builder reads qualification from the type chain alone. It takes
no separate capability argument, because nothing about pointee access lives
outside the type.

Weakening emits no cast. `MutPtr<T>` to `Ptr<T>` is a C qualification
conversion the compiler performs implicitly:

```c
const int32_t *const sw_v_observer = sw_v_writer;   // int32_t * → const int32_t *
```

Rule 4's outermost-layer restriction is exactly what keeps that assignment
legal C. A deeper weakening would produce `int32_t **` to `const int32_t **`,
which C rejects.

Object members are ordinary members qualified by their own pointer type,
whatever their member mode.
Every object uses a source-ordered forward typedef region followed by a
source-ordered definition region, so recursive and non-recursive objects share
one shape:

```c
typedef struct sw_t_Node sw_t_Node;

struct sw_t_Node {
    int32_t    sw_m_value;
    sw_t_Node *sw_m_next;      /* MutPtr<Node> */
};
```

### Foreign `const T *`

Both directions have a type, so no boundary-only pointer form is needed later:

```c
size_t      strlen(const char *s);
const char *SDL_GetError(void);
struct Config { const char *name; };
void        on_event(const Event *e);
```

```seawitch
Ptr<UInt8>      // const char *
MutPtr<UInt8>   // char *
Ptr<Event>      // const Event *
```

An imported `const T *` maps to `Ptr<T>`, so Seawitch cannot write through a
pointer C declared read-only — including one into a string literal or
`.rodata`. Outbound calls still work from either constructor, since `MutPtr<T>`
weakens to `Ptr<T>` and C accepts `T *` where `const T *` is expected.

The FFI specification still owns declaration syntax, name mapping, and ABI
rules. It no longer has to invent a pointer form.

### Diagnostics and phase ownership

The parser owns removed and misplaced `mut` forms. The checker owns resolved
place, type, member, and recursion failures.

```text
[Syntax Error] mut is not valid on the right-hand side; use ref value
[Syntax Error] mut is not allowed inside Ptr<...>; use MutPtr<...>
[Syntax Error] mut is not allowed in an object literal
[Type Error] expected MutPtr<Int32> initializer, got Ptr<Int32>
[Type Error] cannot write through Ptr<Int32>; MutPtr<Int32> is required
[Type Error] cannot weaken MutPtr<MutPtr<Int32>> below its outermost layer
[Type Error] cannot assign to read-only place rival.health
[Type Error] cannot assign to fixed member player.id
[Type Error] cannot replace read-only binding pointer
[Type Error] object type Impossible contains itself by value
[Type Error] unknown type B; forward and mutually recursive objects are not supported
```

Taking `MutPtr<T>` of a read-only place needs no dedicated diagnostic: `ref`
yields `Ptr<T>` there, and the ordinary initializer mismatch above reports it.

Unknown checked pointer, object, or place forms fail closed.

## Drawbacks

Two pointer constructors are more surface than one. They are not two ways to do
one thing — each expresses a different contract — but a reader must learn both,
and every pointer-typed declaration now carries a choice.

A fixed member protects only its own slot. Whatever it holds by value is
unreachable through it, but a fixed `MutPtr<T>` member still permits writes to
its pointee. `mut` is shallow at every level, and so is the pointer
constructor.

Weakening is the language's only implicit conversion, which sits awkwardly
beside RFC 0003's rule that typed values never convert implicitly. It is
restricted to one direction and one layer, and it changes no representation,
but it is a genuine exception.

The outermost-layer restriction on weakening will surprise anyone who expects
`MutPtr<MutPtr<T>>` to convert to `Ptr<Ptr<T>>`. The restriction is inherited
from C's aliasing rule rather than chosen, and rejecting it is the only sound
option.

Neither constructor proves anything about validity, lifetime, bounds, or
aliasing. `Ptr<T>` documents and enforces intent; it is not a safety proof, and
RFC 0001 raw pointers never offered one.

## Alternatives considered

### Retain `ref` and `mut ref`

```seawitch
reader: Ptr<Int32> = ref value
writer: Ptr<Int32> = mut ref value
```

Rejected. The capability attaches to a binding and is established by its
initializer, so every position without an initializer — parameters, returns,
object members — cannot state it at all. Those are exactly the positions C
interoperation needs. No addition to the right-hand side repairs that; the
capability has to live in the type.

It also gives two values of one written type different hidden contracts, which
must then propagate through copies, branches, and boundaries, and which has no
uniform C structure-member representation.

### Spell the capability as `Ptr<mut T>`

```seawitch
Ptr<mut Int32>
```

Rejected, though it is equivalent in expressiveness, C mapping, and
implementation cost. Two reasons prefer `MutPtr<T>`:

`mut` would then mean two different things in one declaration — slot
replaceable and pointee writable:

```seawitch
mut next: Ptr<mut Node>     // rejected spelling
mut next: MutPtr<Node>      // adopted spelling
```

And it puts a keyword inside type expressions, which then propagates into
aliases and any later generic or collection type:

```seawitch
type Link = Ptr<mut Node>
List<mut Item>
```

Keeping `mut` out of type expressions leaves it with exactly one meaning
everywhere it appears.

### Make every pointer writable

```seawitch
Ptr<Int32>   // sole constructor, always writable
```

Rejected. It is the smallest pointer surface, and it was this RFC's earlier
direction. It cannot represent an imported `const T *` in any position, so the
FFI specification would have to introduce a boundary-only pointer form — paying
the same cost later, in a worse place, after code exists. It also forces `ref`
to reject read-only places, which makes fixed storage unaddressable and pushes
read-only sharing onto by-value copies.

### Remove member mutability as well

```seawitch
type Holder = { pointer: Ptr<Int32> }   // every member always replaceable
```

Rejected, and this is the decision most worth revisiting deliberately if the
model is reopened.

Removing it would leave `mut` in exactly one position, which is marginally
simpler. The cost is that a nominal type could then carry no invariant: an
identity field, a capacity, a file descriptor, and a cursor would all be equally
rewritable, and the type system would provide nominal identity with nothing to
protect.

Keeping member modes costs one optional token in `member-declaration` and one
conjunct in place-rule case 2. It creates none of the problems that motivated
this RFC: the mode is fixed by the nominal type, identical in every instance,
never carried by a value, never re-derived at a boundary, and conveyed through
`Ptr<Player>` automatically. It emits nothing into C.

To reverse this decision, delete the `mut` option from `member-declaration`,
drop the second conjunct in place-rule case 2, and restore the
`mut is not allowed in an object member declaration` syntax error.

### Put member mutability in object literals

```seawitch
Holder { mut pointer = ref value }
```

Rejected, and more strongly. Per-value member modes must be carried by every
copy, re-derived at every function boundary, and *intersected at control-flow
merges* — which requires flow analysis the compiler does not have and the
language does not otherwise need.

### Let a fixed binding still permit member writes

```seawitch
player: Player = ...
player.health = 90   // permitted?
```

Rejected. It makes `mut` on an object binding mean only "may be replaced
wholesale", which the same code achieves member by member. It also breaks the
match with scalars and prevents lowering a fixed object binding to a C `const`
object.

### Make all storage writable

Rejected. It is the smallest model, but it removes the distinction that maps
fixed Seawitch bindings to C `const` storage. Seawitch keeps
immutable-by-default storage with `mut` as the explicit opt-in.

## Outside this RFC

- function and method parameter mutability semantics;
- null pointer construction, `Nil`, null checks, and non-null guarantees;
- ownership, borrowing, lifetimes, and automatic memory management;
- pointer arithmetic, bounds, alignment, provenance, and allocation;
- C imports, ABI annotations, casts, and foreign read-only pointers;
- mutually recursive and forward-declared nominal objects;
- unions, packed structures, bit-fields, and anonymous structures; and
- arrays, slices, collections, strings, and their mutation policies.

## Design acceptance criteria

Before implementation, the design must establish that:

1. `mut` is accepted only before a binding name or a member name, and rejected
   everywhere else;
2. a fixed binding rejects both whole replacement and member assignment;
3. a `mut` binding permits whole replacement, and permits member assignment for
   `mut` members only;
4. scalars and objects follow the same binding rule;
5. both constructors read `pointer.value`, and only `MutPtr<T>` assigns it;
6. `ref` is the only address-taking form, requires only addressability, and
   yields `MutPtr<T>` from a writable place and `Ptr<T>` otherwise;
7. `MutPtr<T>` weakens to `Ptr<T>` at the outermost layer only, the reverse is
   rejected, and a deeper weakening is rejected;
8. a fixed pointer binding still permits pointee writes when its type is
   `MutPtr<T>`, and rejects repointing;
9. object literals carry no mutability marker, while object types declare
   member modes that are identical in every instance;
10. object copying transfers no mutability metadata, and a fixed member is
    fixed in the copy for the same reason it was fixed in the source;
11. a fixed member rejects assignment and `MutPtr` address-taking, while a
    fixed `MutPtr<T>` member still permits pointee writes;
12. member modes emit nothing into generated C;
13. self-recursive pointer members are finite while by-value cycles fail;
14. mutually recursive names remain rejected;
15. fixed bindings lower to C `const` storage, including whole objects;
16. each pointer layer's C pointee qualification comes from its own
    constructor, and weakening emits no cast; and
17. generated objects use deterministic split forward and definition regions.

## Implementation handoff requirements

The plan must identify:

1. removal of expression-side `mut`, `mut ref`, and all per-value capability
   state from the lexer, parser, and checked IR — `AccessCapability`, the
   capability slices on bindings and operands, and the clone, compare, and
   attenuate helpers all go;
2. `MutPtr` as a second built-in constructor: type-expression parsing, a
   canonical-identity interner entry alongside `Ptr`, and protection from
   redeclaration under RFC 0005;
3. retention of RFC 0006 member modes while its capability-based place-mode
   composition is removed;
4. the three-case place walk, with `.value` writability read from the
   receiver's constructor rather than a place mode;
5. `ref` typing by place writability, so the read-only case is an ordinary type
   mismatch rather than a bespoke diagnostic;
6. the single outermost-layer weakening rule in assignability;
7. pointer-valued and provisionally self-recursive member checking;
8. `const` lowering for fixed object bindings;
9. a declarator builder that reads pointee qualification from the type chain and
   takes no capability argument — `declaration()` loses its
   `capability []AccessCapability` parameter;
10. deterministic forward-typedef and definition generation;
11. focused parser, checker, type, and generator tests;
12. end-to-end coverage in `compiler/compile_test.go`, including a C23
    compilation case for each of the four nesting combinations; and
13. canonical grammar, language, and status updates after behavior stabilizes.

No analyzer pass is required. Keeping member modes on the nominal type rather
than on values is what keeps it unnecessary: nothing has to be propagated
through copies or intersected at a control-flow merge.
