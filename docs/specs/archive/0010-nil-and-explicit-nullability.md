# RFC 0010: Nil and Explicit Nullability

- Status: Implemented; FFI nullability annotations remain deferred until C
  imports exist
- Features: `Nil`, `nil`, `P | Nil` for pointer-like `P`, non-null raw pointer
  types, explicit null tests, incomplete `Unknown` pointee type,
  nullable-pointer niche lowering, C pointer nullability
- Created: 2026-08-09
- Revised: 2026-08-09
- Depends on: RFC 0001 (raw pointers), RFC 0005 (type declarations and
  transparent aliases), RFC 0007 (mutability redesign), RFC 0009 (core
  operators), and RFC 0015 (structured control flow)
- Coordinates with: RFC 0008 (functions and function-pointer values) and RFC
  0014 (general type expressions and union types)
- Supersedes when accepted: RFC 0001 and RFC 0007 statements that `Ptr<T>` and
  `MutPtr<T>` are themselves nullable, and RFC 0009's complete deferral of
  pointer comparison
- Does not depend on general unions: every type introduced here has a complete
  representation today. RFC 0014 later extends `|` to nullable non-pointer
  members without changing this RFC's pointer null niche
- Implementation status: implemented end to end. FFI nullability annotations
  remain deferred until C imports exist

## Summary

Nullability is explicit in the type system:

```seawitch
type MaybeNode = MutPtr<Node> | Nil

node: MutPtr<Node> = ref first       // cannot be null
maybe: MaybeNode = node              // may be a pointer or Nil
missing: MaybeNode = nil
```

`Nil` is a singleton type. Its only value is `nil`.

The `|` type form is introduced here only for pointer-like members, which have
a null niche and therefore need no tag, no wrapper, and no sum-type layout
decision. `Int32 | Nil` and `Point | Nil` are not part of this RFC.

`Ptr<T>` and `MutPtr<T>` become non-null raw pointer types. A pointer that may
be null is written `Ptr<T> | Nil` or `MutPtr<T> | Nil`. The compiler rejects
dereference until a nullable pointer has been narrowed to its pointer member.

```seawitch
maybe.value
// Error: MutPtr<Node> | Nil may be Nil; narrow it before using .value

if maybe != nil
    value: Node = maybe.value       // maybe is MutPtr<Node> here
end
```

RFC 0015 supplies the `if` grammar and checked representation. This RFC owns
nullable-pointer refinement; RFC 0014 later reuses it inside general union
condition chains.

Nullable pointer unions have the same one-word representation as their C
pointer type. `nil` uses that pointer type's null representation, so explicit
nullability adds no tag, allocation, or runtime overhead:

```seawitch
MutPtr<Node> | Nil
```

```c
sw_t_Node *
```

`Nil` does not represent a function that returns no value, an empty parameter
list, or C's erased `void *`. Those are three separate concepts. C's erased
object pointers use `Ptr<Unknown>` and `MutPtr<Unknown>`.

## Motivation

RFC 0007 currently gives `Ptr<T>` two meanings at once:

1. the value provides read-only access to a `T`; and
2. the stored address may be null.

Only the first property is visible in the type. Code cannot distinguish a
pointer that is guaranteed to exist from one that must be checked:

```seawitch
required: Ptr<Node>
optional: Ptr<Node>       // same type, despite a different contract
```

That prevents the checker from ruling out null dereference and loses useful
information at function, object, allocator, and foreign-library boundaries.

Absence should be spelled as ordinary type composition rather than as
`Nullable<T>`, `Optional<T>`, a postfix `?`, or nullable state hidden inside
every pointer:

```seawitch
Ptr<Node> | Nil
```

The design has five goals:

1. a source type states whether absence is possible;
2. a non-null pointer can be dereferenced without a null check;
3. nullable pointers retain the exact C pointer ABI;
4. C imports remain mechanical even though C headers usually omit nullability
   contracts; and
5. nothing here waits on a general sum-type design.

Goal 5 sets the scope. A pointer-like representation already has a null value
that no valid pointer uses, so `P | Nil` is exactly `P` in C. A non-pointer
member such as `Int32 | Nil` has no such spare value and would force a tag,
a layout rule, and an ABI decision — that is a sum-type problem, and it is
deferred to the RFC that solves it properly. Null pointers are the case that
actually blocks Seawitch code today, and they are separable.

## Guide-level explanation

### `Nil` is a real type

`Nil` has exactly one value:

```seawitch
nothing: Nil = nil
```

`Nil` is a built-in type name and cannot be redeclared. `nil` is a reserved
literal keyword and cannot be used as an identifier.

Reserving `nil` is source-breaking. `docs/grammar.md` currently promises that
C keywords and C macro spellings remain valid Seawitch identifiers, and `nil`
is neither, so no C-derived name is affected — but any existing Seawitch
binding named `nil` stops parsing. `Nil` and `Unknown` join `Ptr` and `MutPtr`
as protected type names and cannot be redeclared. `Unknown` was previously a
legal user type name and is also source-breaking for the same reason.

`Nil` means absence of a value. It is not numeric, ordered, addressable, or
implicitly truthy.

```seawitch
bad_number: Int32 = nil             // Error
bad_condition: Bool = nil           // Error
```

### Nullable values use a union

This RFC introduces the nullable union form for pointer-like members:

```seawitch
type MaybeNode = MutPtr<Node> | Nil
type MaybeReader = Ptr<Config> | Nil
type MaybeHandler = Fun<(Int32) : Int32> | Nil   // once RFC 0008 lands
```

The canonical source spelling places `Nil` last. This RFC accepts exactly one
pointer-like member followed by `Nil`.

A non-pointer member is rejected:

```seawitch
type MaybeScore = Int32 | Nil
// Error: Int32 | Nil requires general unions from RFC 0014
```

The restriction is about representation, not about taste. `Int32` uses every
bit pattern it has, so `Int32 | Nil` needs a tag and a layout rule that no
Seawitch RFC has settled. A pointer has a spare null value already, so
`MutPtr<Node> | Nil` needs nothing. RFC 0014 settles the general
case. It extends `|` to arbitrary member sets without changing any meaning
defined here.

These are different types:

```seawitch
MutPtr<Node>
MutPtr<Node> | Nil
```

A `P` value and `nil` both initialize `P | Nil`:

```seawitch
node: MutPtr<Node> = ref first
present: MutPtr<Node> | Nil = node
absent: MutPtr<Node> | Nil = nil
```

The reverse conversion is never implicit:

```seawitch
maybe: MutPtr<Node> | Nil = nil
node: MutPtr<Node> = maybe
// Error: expected MutPtr<Node>, got MutPtr<Node> | Nil
```

### Pointer types become non-null

`ref place` always produces an address of an existing place, so its result is
naturally non-null:

```seawitch
mut score: Int32 = 10

writer: MutPtr<Int32> = ref score
reader: Ptr<Int32> = writer
```

The pointer constructors keep RFC 0007's access meanings:

- `Ptr<T>` reads a `T` and cannot write it;
- `MutPtr<T>` reads and writes a `T`.

This RFC changes only nullability: neither pointer type contains a null address
in a valid checked Seawitch value.

Nullable pointers say so explicitly:

```seawitch
required: MutPtr<Node> = ref node
optional: MutPtr<Node> | Nil = nil
```

`nil` cannot initialize a non-null pointer:

```seawitch
bad: MutPtr<Node> = nil
// Error: expected MutPtr<Node>, got Nil
```

Slot mutability remains independent:

```seawitch
optional: Ptr<Node> | Nil = nil       // fixed nullable slot
mut current: Ptr<Node> | Nil = nil   // replaceable nullable slot
```

The first binding cannot be reassigned. The second may move between `nil` and
non-null `Ptr<Node>` values.

### Nullable object links

Recursive object links become honest about termination:

```seawitch
type Node = {
    value: Int32,
    mut next: MutPtr<Node> | Nil,
}

tail: Node = Node {
    value = 3,
    next = nil,
}
```

A member without `| Nil` is required:

```seawitch
type Child = {
    parent: Ptr<Parent>,
}
```

A member with `| Nil` is optional:

```seawitch
type Child = {
    parent: Ptr<Parent> | Nil,
}
```

### Narrowing before use

An operation available only on `P` cannot be applied to `P | Nil`:

```seawitch
maybe: MutPtr<Node> | Nil = find_node()
value: Node = maybe.value
// Error: MutPtr<Node> | Nil may be Nil; narrow it before using .value
```

An explicit null test narrows the value:

```seawitch
if maybe != nil
    value: Node = maybe.value
end
```

The two tests have symmetric branch meanings:

| Test | `do` branch | `else` branch |
|---|---|---|
| `value != nil` | `P` | `Nil` |
| `value == nil` | `Nil` | `P` |

There is no truthiness shortcut:

```seawitch
if maybe                            // Error: expected Bool
end
```

Narrowing is a checked fact, not a runtime wrapper. It emits only the C null
test already written by the user.

RFC 0015 supplies the block structure. Nullable narrowing is branch-local:
assignment to the binding or writable address escape through `ref` invalidates
the narrowing because the slot may contain `nil` afterward. A binding whose
mutable address escaped before the condition is not narrowable. Assignment and
`ref` use the binding's declared storage type, while ordinary reads use the
narrowed pointer type. No narrowing survives the closing `end`.

### No-result functions do not return `Nil`

RFC 0008's no-result syntax remains unchanged:

```seawitch
fun reset()
end
```

```c
static void sw_f_reset(void) {
}
```

The empty parameter list means zero parameters. The absent return annotation
means the call produces no value. Neither introduces a source `Void`, `Unit`,
or implicit `Nil` result.

```seawitch
reset()                             // Valid call statement
result: Nil = reset()               // Error: reset produces no value
```

A function that deliberately returns absence says so:

```seawitch
fun find_node(): MutPtr<Node> | Nil
    return nil
end
```

That function produces a value; `reset` does not.

### Function-pointer nullability

When RFC 0008 function-pointer values are available, the same rule applies:

```seawitch
handler: Fun<(Int32) : Int32>              // callable, non-null
optional_handler: Fun<(Int32) : Int32> | Nil
```

A nullable function pointer must be narrowed before it is called. Its nullable
union uses the C function pointer's null representation and adds no tag.

### Erased object pointers

C uses `void`, `void *`, and null pointers for separate jobs. This RFC keeps
them separate:

- C `void` return maps to an omitted Seawitch return annotation;
- C `(void)` parameters map to an empty Seawitch parameter list;
- C null pointer values map to `Nil` inside a nullable pointer union; and
- C `void *` and `const void *` map to `MutPtr<Unknown> | Nil` and
  `Ptr<Unknown> | Nil` respectively.

`Unknown` is a built-in incomplete pointee type. It means that an object exists
at an address while its concrete type, size, alignment, layout, and operations
are erased:

```seawitch
Ptr<Unknown>             // non-null erased read pointer
MutPtr<Unknown>          // non-null erased write pointer

Ptr<Unknown> | Nil       // nullable const void *
MutPtr<Unknown> | Nil    // nullable void *
```

`Unknown` is not a dynamically typed value and carries no runtime type
information. Because its layout is unknown, it cannot be stored by value:

```seawitch
x: Unknown
// Type Error: Unknown has no known size or layout;
// it may only be used behind a pointer
```

Pointers to it are ordinary pointer values and reuse every `Ptr<T>` and
`MutPtr<T>` rule:

```seawitch
reader: Ptr<State> = ref fixed_state
writer: MutPtr<State> = ref state

erased_reader: Ptr<Unknown> = reader
erased_writer: MutPtr<Unknown> = writer
```

An erased pointer cannot be dereferenced because its pointee has no known value
layout:

```seawitch
value: State = erased_writer.value
// Error: MutPtr<Unknown> cannot be dereferenced;
// recover a concrete pointer type first
```

Typed object pointers erase at one pointer layer:

```seawitch
reader: Ptr<State> = ref fixed_state
writer: MutPtr<State> = ref state

read_erased: Ptr<Unknown> = reader
write_erased: MutPtr<Unknown> = writer
weakened_erased: Ptr<Unknown> = writer
```

Erased pointers recover a concrete object pointer at one layer. This is a raw
pointer operation and the programmer remains responsible for choosing the
correct type and alignment:

```seawitch
read_again: Ptr<State> = read_erased
write_again: MutPtr<State> = write_erased
```

Access cannot be strengthened:

```seawitch
bad: MutPtr<State> = read_erased
// Error: Ptr<Unknown> cannot recover writable access as MutPtr<State>
```

The same conversions preserve explicit nullability:

```seawitch
maybe_typed: MutPtr<State> | Nil = nil
maybe_erased: MutPtr<Unknown> | Nil = maybe_typed
maybe_restored: MutPtr<State> | Nil = maybe_erased
```

These conversions emit no C instruction or pointer cast. They change only the
checked Seawitch type.

## Reference-level specification

### Type formation

`Nil` is a complete singleton value type.

A type is *pointer-like* when its representation is a single C pointer with a
reserved null value. Exactly three constructors are pointer-like:

1. `Ptr<T>` for any admissible pointee `T`, including `Unknown`;
2. `MutPtr<T>` for any admissible pointee `T`, including `Unknown`; and
3. RFC 0008's stored `Fun<…>` values, once that RFC lands.

Until RFC 0008 lands, the first two are the whole list, and nothing in this RFC
blocks on the third.

For every pointer-like type `P`, `P | Nil` is a nullable union type. In this
RFC:

1. `P | Nil` is the only accepted `|` type expression;
2. the non-`Nil` member must be pointer-like;
3. `Nil` must be the final member in source;
4. a written type expression may not spell `Nil` twice; and
5. aliases do not introduce a second identity.

Rule 3 is enforced by the grammar, which admits nothing but `Nil` after `|`, so
it never reaches the checker. Rules 1, 2, and 4 are checker rules over the
written form, before aliases are resolved. Rule 5 governs what happens after. The two must not be confused, because an alias can
legitimately produce a second `Nil` that the source never spelled:

```seawitch
type MaybeNode = MutPtr<Node> | Nil
type StillMaybeNode = MaybeNode | Nil
```

`MaybeNode | Nil` spells `Nil` once, so rule 4 permits it. Alias resolution
then yields `MutPtr<Node> | Nil | Nil`, which normalizes to
`MutPtr<Node> | Nil`. `StillMaybeNode`, `MaybeNode`, and
`MutPtr<Node> | Nil` are one type with one canonical identity.

Rejecting that would make a nullable alias unusable in the one position where
its nullability is not already obvious, and would force callers to know whether
a name is nullable before they could safely write `| Nil`. Normalization is
also what RFC 0014 does with duplicate members.

Rule 2 rejects `Int32 | Nil`, `Bool | Nil`, and `Point | Nil` as needing sum
types. It also rejects `Nil | Nil`, since `Nil` is not pointer-like.
`Unknown | Nil` is rejected for a separate reason: `Unknown` is incomplete and
is never a value type, so it is not pointer-like either — `Ptr<Unknown> | Nil`
is the nullable erased pointer.

Every rejection is a checker rule with a diagnostic naming RFC 0014, so the
general-union restriction is lifted by deleting a check rather than by
redesigning anything.

RFC 0014 makes member order irrelevant to canonical identity, admits
non-pointer members, and accepts unions with more than two members. That
extension must preserve the meaning, canonical identity, and representation of
every valid nullable union defined here.

### Value and assignability rules

For `N = P | Nil` with pointer-like `P`:

1. a value of type `P` initializes or is assigned to `N`;
2. `nil` initializes or is assigned to `N`;
3. a value of `N` does not initialize or assign to `P` without proven
   narrowing;
4. `Nil` does not convert to `P` directly;
5. aliases are resolved before these rules are applied; and
6. ordinary RFC 0007 pointer weakening may occur before union injection.

Rule 6 permits:

```seawitch
writer: MutPtr<Node> = ref node
maybe_reader: Ptr<Node> | Nil = writer
```

It does not permit removing `Nil` or strengthening `Ptr<T>` to `MutPtr<T>`.

### Pointer guarantees

After this RFC is accepted:

1. every valid `Ptr<T>` value has a non-null address;
2. every valid `MutPtr<T>` value has a non-null address;
3. `ref place` produces a non-null pointer;
4. `.value` therefore requires no null check when its receiver has one of those
   pointer types and `T` has a value representation;
5. a nullable pointer union has no `.value` until narrowed; and
6. non-null says nothing about lifetime, bounds, alignment, provenance,
   allocation state, aliasing, or data races.

Foreign code and unchecked pointer conversions may violate pointer contracts.
Their boundary specification must state how a claimed non-null foreign value
is validated or trusted.

### The incomplete `Unknown` type

`Unknown` is a built-in incomplete object type. It has no known size, alignment,
layout, value representation, initializer, or operations. It cannot be used in
any position that requires a value representation, including a binding, a
by-value member, a parameter, a return type, an object literal, or a nullable
union by itself.

A transparent alias may name `Unknown`, but it retains the same incomplete
identity and restrictions. Aliasing never makes the type storable:

```seawitch
type Erased = Unknown

pointer: Ptr<Erased>                 // Valid
value: Erased                        // Error: no known size or layout
```

`Unknown` is admissible as the element of `Ptr<T>` and `MutPtr<T>`. Consequently
`Ptr<Unknown>` and `MutPtr<Unknown>` are ordinary complete pointer value types:
they may be bound, copied, assigned, stored in objects, returned, passed,
placed in `| Nil`, nested behind another pointer, and addressed with `ref`.

The pointer constructors retain their existing access meaning:

1. `Ptr<Unknown>` preserves only read-only access when a concrete pointer is
   recovered;
2. `MutPtr<Unknown>` preserves writable access when a concrete pointer is
   recovered;
3. RFC 0007 already weakens `MutPtr<Unknown>` to `Ptr<Unknown>`;
4. `Ptr<Unknown>` never strengthens to `MutPtr<Unknown>`; and
5. both are raw pointers and prove neither the erased object's concrete type
   nor any validity property other than non-nullness.

Neither erased pointer supports `.value` or object-member access because the
element type has no value representation. Pointer arithmetic remains deferred
for all pointer types.

For any non-function object type `T`, the following one-layer conversions are
allowed:

| Source | Destination | Meaning |
|---|---|---|
| `Ptr<T>` | `Ptr<Unknown>` | erase type, preserve read-only access |
| `MutPtr<T>` | `MutPtr<Unknown>` | erase type, preserve writable access |
| `MutPtr<T>` | `Ptr<Unknown>` | erase type and weaken access |
| `MutPtr<Unknown>` | `Ptr<Unknown>` | ordinary RFC 0007 weakening |
| `Ptr<Unknown>` | `Ptr<T>` | recover a read-only typed pointer |
| `MutPtr<Unknown>` | `MutPtr<T>` | recover a writable typed pointer |
| `MutPtr<Unknown>` | `Ptr<T>` | recover and weaken access |

Recovery is intentionally implicit to preserve C's simple `void *` object
pointer interchange at FFI boundaries. It is unsafe in the raw-pointer sense:
recovering the wrong `T` may later violate alignment, effective-type,
provenance, lifetime, or bounds requirements.

#### The relation is the table, not its closure

The table is the complete erasure and recovery relation. It is not closed under
composition, and a checker must never chain two of its rows to satisfy one
conversion. At most one row applies per conversion, and `Unknown` must be the
immediate pointee of the outermost pointer constructor on the source side, the
destination side, or both.

"Immediate pointee of the outermost constructor" is the same layer the
shallowness rule below describes. `MutPtr<MutPtr<Unknown>>` mentions `Unknown`
but its outermost pointee is `MutPtr<Unknown>`, so it is not a table source or
destination and converts to nothing.

Without the non-composition rule, erasure followed by recovery would silently
punt any pointer to any other pointer:

```seawitch
mut small_value: Int8 = 0
small: MutPtr<Int8> = ref small_value

big: MutPtr<Int64> = small
// Error: expected MutPtr<Int64>, got MutPtr<Int8>
```

That assignment is rejected. `MutPtr<Int8>` to `MutPtr<Unknown>` and
`MutPtr<Unknown>` to `MutPtr<Int64>` are each individually allowed, but the
pair is not a conversion. A programmer who wants that reinterpretation names
the erased type in source and accepts the two visible steps:

```seawitch
erased: MutPtr<Unknown> = small
big: MutPtr<Int64> = erased
```

Erasure to a named `Unknown` type is the audit point. Grepping for `Unknown`
finds every place a pointer type was reinterpreted.

Non-composition is scoped to this table. It does not restrict the two
conversions that are not table rows:

1. RFC 0007 pointer weakening; and
2. nullable union injection.

Those still compose with a table row and with each other, which is what
§Value and assignability rule 6 already permits. `MutPtr<State>` to
`Ptr<Unknown> | Nil` is one erase-and-weaken row followed by injection, and it
is allowed. What is forbidden is reaching a *different pointee type* without
naming `Unknown` in source.

#### Where recovery applies

Erasure and recovery apply in exactly these contexts, all of which state the
destination type in source:

1. a declaration with a type annotation;
2. an assignment to a place whose type is already known;
3. a `return` expression in a function with a declared result type;
4. a call argument matching a declared parameter type; and
5. an object-literal member initializer matching a declared member type.

Contexts 4 and 5 mean that passing `Ptr<Unknown>` to a `Ptr<State>` parameter
recovers silently. That is deliberate and matches C, where `void *` converts to
any object pointer at a call. It is also the sharpest edge in this RFC: the
destination type is in the callee's signature, not at the call site. An
implementation should consider a warning, not an error, when recovery occurs at
a call argument.

#### Shallowness

The conversion is shallow. It applies to the erased pointer value itself and
never recursively changes a nested pointer element, object member, collection
element, or pointed-to slot. In particular, no conversion exists between
`MutPtr<MutPtr<T>>` and `MutPtr<MutPtr<Unknown>>`.

There is no cast expression, so recovery happens only where the five contexts
above supply a destination type. It never happens because a surrounding
operator or member access would prefer some type: `erased.value` is always an
error even when the intended pointee is obvious, because a `.value` receiver
has no declared type to recover into. The pointer must first be bound at a
concrete type.

For nullable forms, the corresponding conversion is applied to the non-`Nil`
member while `Nil` remains `Nil`. A nullable source remains nullable; erasure
or recovery never removes `Nil`.

### Null tests

RFC 0009 equality is extended with the two tag tests:

```seawitch
nullable == nil
nullable != nil
```

where `nullable` has type `P | Nil`. The result is `Bool`. These operations
inspect only the active union member; they do not require equality support from
`P`. In lowered C they are the ordinary null pointer comparison.

Stated precisely, `==` and `!=` accept an operand pair when one side has type
`Nil` and the other has type `Nil` or `P | Nil`. The operands commute:
`nil == nullable` means exactly what `nullable == nil` means and narrows
identically.

Both operands may be `Nil`. `nil == nil` is `true` and `nil != nil` is `false`,
because `Nil` has one value. This is a degenerate case, not a useful one, but
excluding it would make the singleton type the only type without equality.

The remaining pair — one operand `Nil`, the other neither `Nil` nor nullable —
is a type error rather than a constant result:

```seawitch
node: MutPtr<Node> = ref first
if node != nil
end
// Error: MutPtr<Node> is never Nil; the test is always true
```

The C habit of null-checking every pointer will produce this diagnostic often
during migration, so it names the reason rather than reporting a generic
mismatch. Silently accepting the test would hide exactly the information this
RFC adds to the type system.

The diagnostic also fires on a redundant second test inside a narrowed branch,
where the binding already has type `P`. That is intended: the inner test is
dead, and reporting it is how a reader learns the outer test already did the
work.

Comparing two arbitrary pointers or two arbitrary nullable unions remains
outside this RFC.

### Narrowing

A checker may treat an expression of static type `P | Nil` as `P` only where a
dominating checked condition proves that the expression is not `nil` and no
intervening operation can replace the observed storage.

#### What can be narrowed

This RFC narrows **local bindings only**. The narrowed expression must be a
bare identifier naming a binding in the current function.

A member path is not narrowable in this RFC:

```seawitch
node: MutPtr<Node> = ref first

if node.value.next != nil
    tail: Node = node.value.next.value
    // Error: only a local binding can be narrowed;
    // bind node.value.next before testing it
end
```

The workaround is one line, and the binding must be introduced *before* the
test, not inside the branch — a binding declared inside the branch would still
need the narrowing it is trying to obtain:

```seawitch
next: MutPtr<Node> | Nil = node.value.next

if next != nil
    tail: Node = next.value
end
```

The restriction is not conservatism for its own sake. `node.value.next` is
reached
through a raw pointer, so any write through any aliasing pointer — including
inside a call the checker cannot see through — may replace it between the test
and the use. Proving otherwise needs an alias analysis that Seawitch does not
have and, given RFC 0001's raw pointers, may never have. A binding is different:
its storage is named, and the only ways to replace it are assignment to that
name and address escape, both of which are syntactically visible.

The workaround copies the pointer value out of the aliasable slot, which is why
it is sound: `next` is a local whose storage nothing else can reach.

Extending narrowing to member paths is a later change. It must not weaken any
guarantee stated here.

#### Structured control-flow rules

Together with RFC 0015, nullable narrowing follows these rules:

1. `== nil` and `!= nil` narrow direct local bindings in their true and false
   branches according to the table above;
2. ordinary reads use the branch-local narrowed type;
3. assignment and `ref` use the binding's declared storage type;
4. assignment or writable address escape through `ref` invalidates a current
   narrowing;
5. a binding whose mutable address previously escaped cannot be narrowed;
6. a read-only address does not invalidate narrowing;
7. no narrowing survives the closing `end`, including when early `return`,
   `break`, or `continue` would permit stronger post-block inference; and
8. this RFC adds no short-circuit right-operand narrowing for `and` or `or`.

An implementation fails closed rather than dereference a nullable union
without one of these valid proofs. RFC 0014 applies the same storage and escape
rules to general unions.

### Representation

`Nil` lowers to C23 `nullptr_t`, and `nil` lowers to the C23 `nullptr`
predefined constant. Generated C includes `<stddef.h>` when the written
`nullptr_t` name is needed.

For every pointer-like `P`, `P | Nil` uses a null niche:

1. the representation, size, and alignment are exactly those of `P`;
2. the `Nil` member is represented by a null `P` value;
3. the `P` member is represented by every valid non-null `P` value;
4. no explicit tag or wrapper structure is emitted; and
5. conversion between `P` and `P | Nil` emits no instruction.

Because §Type formation admits only pointer-like members, the niche rule above
is total: every nullable union this RFC can form has a complete, tag-free
representation. No union in this RFC requires the general layout, tag,
discriminant, or ABI decisions owned by RFC 0014.

`Ptr<Unknown>` has the representation, size, and alignment of C `const void *`.
`MutPtr<Unknown>` has the representation, size, and alignment of C `void *`.
Their non-null source invariant emits no wrapper or runtime field.

`Nil` used on its own — as in `nothing: Nil = nil` — is a `nullptr_t` value.
It is the singleton type's own representation and is unrelated to the niche.

### C FFI defaults

C object pointer types do not generally state whether null is permitted. A
mechanical importer therefore defaults them to nullable:

| Imported C | Seawitch |
|---|---|
| `T *` | `MutPtr<T> \| Nil` |
| `const T *` | `Ptr<T> \| Nil` |
| `void *` | `MutPtr<Unknown> \| Nil` |
| `const void *` | `Ptr<Unknown> \| Nil` |

This default is conservative. A binding declaration, trusted annotation, or
wrapper may strengthen a particular contract to non-null. Passing a non-null
Seawitch pointer to a nullable imported parameter requires only union injection
and changes no ABI bits.

Exports erase the source-only guarantee:

| Exported Seawitch | C |
|---|---|
| `MutPtr<T>` | `T *` |
| `MutPtr<T> \| Nil` | `T *` |
| `Ptr<T>` | `const T *` |
| `Ptr<T> \| Nil` | `const T *` |
| `MutPtr<Unknown>` | `void *` |
| `MutPtr<Unknown> \| Nil` | `void *` |
| `Ptr<Unknown>` | `const void *` |
| `Ptr<Unknown> \| Nil` | `const void *` |

C callers cannot observe the Seawitch distinction in the C type. Optional
nonnull annotations are a later FFI concern.

### C `void **` shape

C `void **` imports conservatively as:

```text
MutPtr<MutPtr<Unknown> | Nil> | Nil
```

The inner `| Nil` represents the nullable `void *` stored in the slot. The
outer `| Nil` represents the possibility that the `void **` argument itself is
null. The outer `MutPtr` means the called function may replace the slot.

For a local output slot:

```seawitch
mut erased: MutPtr<Unknown> | Nil = nil
foreign_call(ref erased)
```

`ref erased` has the non-null type `MutPtr<MutPtr<Unknown> | Nil>` and injects
into the imported outer nullable union without changing its `void **`
representation.

Nested pointer erasure must not be implicit. A pointer to a typed pointer slot
is not a pointer to an erased-pointer slot, matching C's rejection of treating
`T **` as `void **`.

The main qualified erased-pointer shapes import as:

| C | Seawitch |
|---|---|
| `void **` | `MutPtr<MutPtr<Unknown> \| Nil> \| Nil` |
| `void *const *` | `Ptr<MutPtr<Unknown> \| Nil> \| Nil` |
| `const void **` | `MutPtr<Ptr<Unknown> \| Nil> \| Nil` |
| `const void *const *` | `Ptr<Ptr<Unknown> \| Nil> \| Nil` |

### Grammar

This is a delta against the grammar in `docs/grammar.md`, which currently reads:

```ebnf
type-expression          = identifier
                         | pointer-constructor , "<" , type-expression , ">" ;
```

It becomes:

```ebnf
type-expression          = primary-type-expression
                         , [ "|" , "Nil" ] ;

primary-type-expression  = "Nil"
                         | "Unknown"
                         | identifier
                         | pointer-constructor , "<" , type-expression , ">" ;
```

`Nil` and `Unknown` are separate productions rather than plain identifiers
because both are protected built-in names that cannot be redeclared.

Two things are deliberately **not** added here:

1. `object-type-expression` stays reachable only from
   `type-definition-expression`. Anonymous inline object types remain illegal
   in a binding, member, parameter, or element position, exactly as today. This
   RFC does not legalize `x: { a: Int32 } | Nil`.
2. `function-type-expression` belongs to RFC 0008 and does not exist yet. When
   RFC 0008 lands, adding it to `primary-type-expression` is what makes
   `Fun<(Int32) : Int32> | Nil` parse; no change to this RFC is needed.

The nullable suffix binds outside the whole primary type, so
`MutPtr<Node> | Nil` is a nullable pointer and never a pointer to a nullable
type. Nested nullable element types are written inside the angle brackets,
where `type-expression` recurses:

```seawitch
MutPtr<MutPtr<Node> | Nil>          // non-null pointer to a nullable slot
MutPtr<MutPtr<Node>> | Nil          // nullable pointer to a non-null slot
MutPtr<MutPtr<Node> | Nil> | Nil    // both
```

`nil` is added to `primary-expression` as a literal, alongside `true` and
`false`.

The incomplete-type restrictions on `Unknown`, the pointer-like member rule,
and duplicate-`Nil` rejection are checker rules, not grammar restrictions. The
grammar accepts `Int32 | Nil` and the checker rejects it, so the diagnostic can
explain the sum-type limitation instead of reporting a parse failure.

### Diagnostic ownership

- The lexer owns the `nil` keyword and `|` token.
- The parser owns malformed nullable type expressions.
- The checker owns nullable union formation, injection, narrowing, and invalid
  dereference.
- The generator owns niche selection and exact C representation.
- The FFI importer owns conservative C pointer nullability.

Required diagnostics include:

```text
[Syntax Error] Nil must be the final member of a nullable type

[Type Error] expected MutPtr<Node>, got Nil
[Type Error] expected MutPtr<Node>, got MutPtr<Node> | Nil
[Type Error] MutPtr<Node> | Nil may be Nil; narrow it before using .value
[Type Error] Unknown has no known size or layout; it may only be used behind a pointer
[Type Error] Ptr<Unknown> cannot be dereferenced; recover a concrete pointer type first
[Type Error] Ptr<Unknown> cannot recover writable access as MutPtr<State>
[Type Error] cannot erase a nested pointer slot as MutPtr<MutPtr<Unknown>>
[Type Error] Int32 | Nil requires sum types, which are not supported yet;
             only pointer types may be combined with Nil
[Type Error] Unknown | Nil is not a value type; use Ptr<Unknown> | Nil
[Type Error] Nil may not be written twice in one type expression
[Type Error] MutPtr<Node> is never Nil; the test is always true
[Type Error] only a local binding can be narrowed;
             bind node.value.next before testing it
[Type Error] expected MutPtr<Int64>, got MutPtr<Int8>;
             erasure and recovery do not compose, bind MutPtr<Unknown> first
```

`Nil` in a non-final position is a syntax error rather than a type error
because the grammar admits only `Nil` after `|`, so the parser rejects
`Nil | MutPtr<Node>` before any type exists to report. Every other rejection
above parses successfully and is a checker rule.

## Interaction with language guarantees

Explicit nullability removes null dereference from checked non-null pointer
operations. It does not make raw pointers memory-safe. A non-null pointer may
still be dangling, out of bounds, misaligned, incorrectly typed, or involved
in a data race.

This RFC strengthens the useful guarantee without pretending to solve the rest
of raw-pointer validity:

```text
Ptr<T> proves non-nullness and read access.
MutPtr<T> proves non-nullness and read/write access.
Neither proves that the address remains otherwise valid.
```

## Deferred questions

RFC 0015 and the structured control-flow rules above resolve local-binding,
branch-join, and early-exit narrowing for this RFC. No `match` construct is
required.

The following remain deferred until their owning features exist and do not
block the core implementation:

1. nullable foreign callbacks;
2. trusted nonnull annotations at C import boundaries; and
3. unsafe pointer conversions that may manufacture a null value while claiming
   a non-null pointer type.

Nullable non-pointer types are not an open question here. They are out of
scope, and RFC 0014 owns the member set, ordering, tag, layout,
and ABI. That RFC must keep the pointer forms defined here tag-free.

Allocation, ownership, lifetimes, bounds, and automatic cleanup remain outside
this RFC.

## Alternatives considered

### Keep every pointer nullable

Rejected. It preserves RFC 0007 but makes required and optional addresses
indistinguishable and prevents the checker from excluding null dereference.

### Add `Nullable<T>` or `Optional<T>`

Rejected. A dedicated constructor is a second optional-value mechanism that
RFC 0014 would otherwise immediately duplicate. `P | Nil` is the spelling that
extension will already produce, so writing it now costs nothing and avoids a
migration.

### Wait for the general-union RFC before specifying any of this

Rejected, and this RFC is scoped specifically to avoid it. Null pointers block
real Seawitch programs today; general sum types do not. The two problems only
look coupled because they share the `|` token. They are separable because a
pointer already reserves a null value, so the pointer case needs no tag, no
layout, and no ABI decision — the exact things RFC 0014 later settles for
general unions.

The cost of the split is one checker rule rejecting non-pointer members and one
diagnostic pointing at RFC 0014. The benefit is that non-null pointers,
`Unknown`, and conservative C pointer import can ship as a smaller prerequisite
phase.

### Add postfix `T?`

Rejected. It is shorter but creates an alias syntax for `T | Nil`, violating
the goal of one obvious spelling.

### Make no-result functions return `Nil`

Rejected. A no-result call produces no value. A function returning `Nil`
produces a real singleton value and may participate in expressions and unions.

### Use `Nil` as C `void`

Rejected. Nullness, lack of a function result, and erased object type are
different properties. In particular, a non-null `void *` points at an object;
it does not point at `Nil`.

### Add `AnyPtr` and `MutAnyPtr`

Rejected. The names are shorter than `Ptr<Unknown>` and `MutPtr<Unknown>`, but
they create a parallel pointer family. The checker and generator would need
separate rules for access, null-niche classification, nesting, `ref`, C
declarators, erasure, recovery, and pointer-to-pointer composition.

`Ptr<Unknown>` and `MutPtr<Unknown>` reuse the existing constructors and every
pointer-layer rule mechanically. The one incomplete-pointee restriction is not
arbitrary: Seawitch also needs the same model for named incomplete foreign
types whose values cannot be stored before their layout is known.

### Name the incomplete pointee `Opaque`

Rejected. `Opaque` is conventional systems terminology, but it is often used
for a specific named foreign handle whose implementation is hidden.
`Unknown` states the property needed here more directly: the pointer exists,
but the compiler does not know its concrete object type or layout.

`Unknown` is not a dynamic top type and cannot store arbitrary values. Its
incomplete-type restriction keeps that meaning precise.

### Name the incomplete pointee `Any`

Rejected. `Any` conventionally suggests a universal value type capable of
holding values such as integers and objects, usually with boxing or runtime
type information. The erased pointee has neither. Reserving `Any` also leaves
that name available for a future generic constraint or true universal type.

### Add `ErasedPtr` and `MutErasedPtr`

Rejected for the same parallel-family reason. The names are also unnecessarily
long at C boundaries.

### Spell erased pointers as `RawPtr` and `MutRawPtr`

Rejected. RFC 0001 already makes every Seawitch pointer raw. The name would
incorrectly suggest that `Ptr<T>` and `MutPtr<T>` are managed or lifetime
checked.

### Wrap nullable pointers in tagged structures

Rejected. Every supported C target already has a null pointer representation.
Using it as the `Nil` niche preserves the exact pointer ABI with no tag or
runtime overhead.

### Treat imported C pointers as non-null by default

Rejected. Plain C pointer syntax carries no reliable nonnull contract. Assuming
one would let unchecked foreign null values enter a Seawitch non-null type.

### Close the erasure and recovery relation under composition

Rejected, and the closure is specifically forbidden. Composing erasure with
recovery makes every pointer type implicitly assignable to every other pointer
type, which silently reintroduces unchecked type punning through a feature
whose entire purpose is to make pointer contracts visible. Requiring a named
`Unknown` binding between the two steps keeps every reinterpretation greppable
at a cost of one line.

### Require a cast expression for recovery

Considered and deferred rather than rejected. An explicit `as` or similar form
would let `erased.value` be written inline and would mark recovery at call
sites, where the destination type is otherwise invisible. It is the better
design. It is not in this RFC because Seawitch has no cast syntax at all, and
inventing one for this single case would prejudge a general conversion form.
Until then, the annotated-binding requirement is the explicit step.

### Narrow member paths as well as bindings

Rejected for now. `node.value.next` is reached through a raw pointer, so
proving it unchanged between test and use requires alias analysis that Seawitch
does not have. Narrowing bindings only costs one extra line per tested path and
never produces an unsound narrowing.

## Design acceptance criteria

Before implementation, the final design must establish that:

1. `Nil` is a singleton type whose only value is `nil`;
2. `P | Nil` is the only nullable spelling, `P` is pointer-like, and `Nil`
   appears last;
3. `P` and `nil` inject into `P | Nil`, while `P | Nil` never implicitly loses
   `Nil`;
4. `Ptr<T>` and `MutPtr<T>` are non-null and retain RFC 0007's read/write
   distinction;
5. `ref place` produces a non-null pointer;
6. nullable pointers reject `.value` until narrowed;
7. `== nil` and `!= nil` test the active member without requiring `P`
   equality;
8. narrowing applies to local bindings only; reads use the narrowed type,
   assignment and `ref` use the declared storage type, prior writable address
   escape prevents narrowing, assignment and new writable escape invalidate
   it, and no narrowing survives the closing `end`;
9. nullable pointer and function-pointer unions use the null niche with exactly
   the underlying C pointer's representation, size, alignment, and ABI;
10. native non-null and nullable pointer exports both retain the ordinary C
    pointer ABI;
11. imported unannotated C pointers default to nullable;
12. C `void` return and `(void)` parameters remain function syntax rather than
    `Nil` types;
13. `Unknown` is incomplete and valid only where no value representation is
    required, while `Ptr<Unknown>` and `MutPtr<Unknown>` are ordinary non-null
    pointer value types that cannot be dereferenced;
14. one-layer typed/erased pointer conversion preserves read/write access,
    nullable conversion preserves `Nil`, and erased recovery never strengthens
    access;
15. `Ptr<Unknown> | Nil` and `MutPtr<Unknown> | Nil` lower exactly to
    `const void *` and `void *` respectively;
16. `void **` maps to `MutPtr<MutPtr<Unknown> | Nil> | Nil` and preserves
    separate inner-slot and outer-pointer nullability;
17. nested typed-pointer-to-erased-pointer conversion is not implicit;
18. the erasure and recovery table is not closed under composition, so no pair
    of rows converts one typed pointer to a differently typed pointer;
19. erasure and recovery apply only in the five contexts that state a
    destination type in source;
20. `== nil` and `!= nil` commute, and a non-nullable operand is a diagnostic
    rather than a constant result;
21. invalid or unsupported nullable forms fail closed; and
22. every type this RFC can form has a complete representation, so no part of
    it depends on a sum-type design, and a non-pointer member is rejected with
    a diagnostic naming that future RFC.

## Implementation handoff requirements

The core design is representationally and semantically complete. Deferred FFI
annotations have no current syntax or boundary and are not core acceptance
criteria. The implementation plan must identify:

1. `Nil`, `nil`, `Unknown`, and `|` additions in the lexer, parser, built-in type
   environment, and checked representation, including incomplete-type
   validation for `Unknown`;
2. canonical nullable-union identity, alias normalization, and the pointer-like
   member check that rejects non-pointer `|` members;
3. the revised non-null invariant for `Ptr<T>` and `MutPtr<T>`;
4. union injection plus interaction with outermost `MutPtr<T>` to `Ptr<T>`
   weakening;
5. structured null-test checked nodes, operand commutation, and the
   non-nullable-operand diagnostic;
6. narrowing restricted to local bindings, declared-storage typing for
   assignment and `ref`, branch-local restoration, plus invalidation after
   assignment and writable address escape;
7. null-niche lowering for data and function pointers with no wrapper C type;
8. `nullptr_t`/`nullptr` lowering and `<stddef.h>` inclusion;
9. the one-layer typed/erased conversion matrix, access preservation, nullable
   lifting, explicit rejection of nested erasure, the non-composition rule, and
   the five contexts where recovery applies;
10. conservative FFI import types and exact export ABI mappings, including the
    qualified `void **` family;
11. focused lexer, parser, checker, type-identity, narrowing, generator, and FFI
    tests;
12. end-to-end cases for required pointers, nullable pointers, recursive
    nullable members, callbacks, `Ptr<Unknown>` recovery, access rejection, and
    a `void **` output slot;
13. rejection cases for each new diagnostic, including composed erasure and
    recovery, member-path narrowing, a non-nullable `!= nil` test, a
    non-pointer `|` member, and a doubly written `Nil`; and
14. canonical grammar, language, and status updates only after behavior
    stabilizes.
