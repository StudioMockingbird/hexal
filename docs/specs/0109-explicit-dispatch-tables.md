# RFC 0109: Explicit Dispatch Tables

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-08-22
- Updated: 2026-08-22
- Scope: permit `Fun<...>` members in ordinary objects so programs can build explicit dispatch tables
- Depends on: the existing `Fun<...>` rules, generic types, object values, RFC
  0094, and RFC 0110
- Coordinates with: parser-independent type checking, object layout, function-value generation, C emission, and `docs/reference.md`
- Does not change: generic monomorphization, method resolution, the no-closure rule, or the compiler's string-in/string-out boundary

## Decision

Add explicit dispatch tables as ordinary nominal objects containing function
values. Do not add interfaces, inheritance, virtual methods, automatic method
lookup, runtime type tags, type erasure, or a new dispatch keyword.

Static generic dispatch remains the default. A dispatch table is used only when
the selected implementation must be replaceable or selected at runtime.

The table stores operations as ordinary `Fun<...>` values. Any state or context
is an ordinary field or an explicit function argument. No function is bound to
an implicit receiver and no environment is generated.

Example:

```hexal
type ReaderOps<S> = {
    read: Fun<(MutPtr<S>, MutPtr<Byte>, Size)>,
    close: Fun<(MutPtr<S>)>,
}

type Reader<S> = {
    state: MutPtr<S>,
    ops: ReaderOps<S>,
}

type FileState = {
    position: Size,
}

fun read_file(state: MutPtr<FileState>, destination: MutPtr<Byte>, count: Size) do
    return
end

fun close_file(state: MutPtr<FileState>) do
    return
end

state: FileState := FileState { position = 0, }
reader: Reader<FileState> := Reader<FileState> {
    state = ref state,
    ops = ReaderOps<FileState> { read = read_file, close = close_file, },
}

fun use_reader(reader: Reader<FileState>, destination: MutPtr<Byte>, count: Size) do
    reader.ops.read(reader.state, destination, count)
end
```

The function table is explicit in the source, has a statically known type, and
incurs an indirect call only at the operation selected through the table.

## Motivation

Hexal already has static generic dispatch and function values as parameters.
Those mechanisms cannot express a reusable C-style operation table because a
`Fun<...>` value cannot currently be stored in an object. That prevents direct
expression of callback tables, pluggable backends, device operations, and
runtime-selected algorithms without falling through to C interop.

The missing capability is storage of a function value, not an object-oriented
runtime. Making the storage explicit preserves C interoperability and keeps the
runtime model visible.

## Syntax

No new syntax is added. Existing object declarations and literals are used.
The existing member-call form calls a function-valued member:

```hexal
table.operation(arguments)
```

The parser must therefore retain member access followed by a call as an
ordinary call expression. No special dispatch-table AST node is introduced.

## Type rules

### Object members

`Fun<...>` is valid as an object member after this RFC is implemented. The
member's exact function signature is part of the containing nominal object's
canonical type and generated layout. Placement of function values elsewhere
is governed by RFC 0094; this RFC does not reintroduce the older blanket
restrictions on function results, ADT payloads, collections, Tasks, or
Channels. Affine ownership of a containing object still follows RFC 0110.

An object containing a function member is itself an ordinary storable object.
It may therefore be used wherever that object's other members and the normal
copyability rules permit, including as a function parameter or result.

Generic object members are resolved after specialization. An open generic
object may contain a function member whose signature refers to its type
parameters; every concrete specialization must produce a complete finite
`Fun<...>` type.

### Initialization and assignment

An object function member is initialized by a compatible named function,
anonymous function literal, existing function value, or a union containing the
exact function type. Existing function-signature assignability and nullable
function narrowing rules apply.

Object members remain fixed unless declared `mut`. A mutable function member may
be replaced with another compatible function value. A function member has no
equality, ordering, address-taking, or indexing operation.

Object copying copies the function pointer and every other member shallowly.
Copying a table does not copy or manage the state referenced by a context or
state field.

### Calls

A direct call through a function-valued member evaluates the receiver and all
arguments once, then calls the function value stored in that member. A nullable
function member must be narrowed before the call.

The language adds no implicit context argument. If an operation needs state,
the table must store that state or the caller must provide it explicitly:

```hexal
table.step(table.state, input)
```

The compiler does not infer or synthesize a context convention.

### Static and dynamic dispatch

Calls through a concrete generic parameter remain static dispatch and continue
to monomorphize. Calls through a `Fun<...>` object member are indirect calls.
The observable semantics do not depend on whether a compiler later devirtualizes
an indirect call after proving the stored value constant.

Method calls retain their existing statically resolved receiver rules. A method
is not automatically converted into a table member, and a table does not gain
methods based on the functions it stores.

## Representation and generated C

For a concrete object specialization, a `Fun<...>` member lowers to a C
function-pointer field with the existing function ABI. The containing object
lowers to the same field order as its Hexal declaration.

The call through a member lowers to a direct C member call through that stored
function pointer. The compiler emits:

- no vtable header;
- no runtime type tag;
- no hidden receiver or context field;
- no closure environment;
- no allocation for the table itself; and
- no dispatcher or wrapper function unless ordinary source code declares one.

Generated C for a table is therefore structurally equivalent to a C struct of
function pointers and ordinary data fields. Function-pointer fields use the
same compatibility and prototype rules as existing `Fun<...>` parameters.

## Safety and lifetime

This RFC does not add ownership, borrowing, reference counting, or automatic
cleanup. A table containing a pointer or handle follows the existing shallow
copy and explicit-lifetime rules.

The compiler must reject:

- an incompatible function signature in a table member;
- a call through a nullable function member without narrowing;
- a direct function value in any still-forbidden position;
- a captured local or parameter in an anonymous function literal; and
- a table whose generic specialization leaves an incomplete or non-finite
  function signature.

The compiler need not prove that a table's state pointer remains live. Existing
local lifetime diagnostics apply where they can prove a violation; unresolved
alias and escape cases retain the current language behavior.

## C interoperability

This RFC makes Hexal-defined callback tables representable without requiring a
foreign ABI. C interoperability may later map compatible imported C structs of
function pointers to the same object representation, but that mapping is not
part of this RFC.

No implicit conversion exists between unrelated table types. A C callback table
with an erased `void *` context remains an explicit interop boundary; Hexal does
not introduce a general safe type-erasure mechanism here.

## Non-goals

- Interfaces, traits, inheritance, or virtual methods.
- Automatic dispatch based on the dynamic type of an object.
- Runtime type tags or reflection.
- Type-erased values such as `Any`, `Some<T>`, or `dyn T`.
- Closures, captured variables, environment objects, or hidden context pointers.
- A standard stream, allocator, device, or plugin interface.
- Replacing static generics with runtime dispatch.
- Changing the existing `Fun<...>` function-pointer ABI.
- Adding a new keyword or parser construct.

## Compatibility

Programs that currently compile retain their meaning. Programs that currently
reject `Fun<...>` object members become valid only when they provide a complete
function member initializer and satisfy all other object rules. No existing
valid placement becomes invalid.

RFC 0094 owns the complete function-value placement table, including ordinary
function-pointer storage in object members and the other positions it permits.
This RFC adds no closure, environment, virtual dispatch, or implicit receiver.

## Implementation outline

1. Remove the checker-only `Fun<...>` object-member rejection.
2. Extend object layout and canonical-type validation to retain function-member
   signatures after generic specialization.
3. Reuse existing function-value initializer, assignment, nullability, and call
   checking.
4. Reuse the existing C function-pointer declarator for object fields.
5. Emit member calls as indirect calls through the rendered field expression.
6. Update `docs/reference.md` after behavior stabilizes, including the position
   matrix, function rules, object layout, and generated-C contract.
7. Add focused integration coverage for fixed, mutable, nullable, generic, and
   nested table members, plus generated-C text assertions.

## Validation

This section is exhaustive. RFC 0109 is complete only when every item below
passes:

- A nominal object with one fixed `Fun<...>` member is accepted and its member
  can be called.
- A mutable function member can be reassigned only to a compatible function
  value; incompatible assignment is rejected.
- A named function, non-capturing anonymous function literal, and existing
  function binding can initialize a compatible member.
- A nullable function member rejects an unnarrowed call and accepts a call
  after the existing null test or match narrowing.
- A generic object containing a function member specializes with the correct
  concrete signature and can be passed to and returned from a function.
- A nested object containing function members remains callable through the
  complete member chain.
- Direct `Fun<...>` use follows RFC 0094's placement and ownership rules; this
  RFC adds no second restriction matrix.
- A function literal that captures an enclosing binding remains rejected.
- Generated C contains a compatible function-pointer field and an indirect
  member call, with no generated vtable, tag, context field, allocation,
  dispatcher, or wrapper.
- Existing generic calls and statically resolved methods retain their current
  specialization and receiver behavior.
- Existing programs that do not use function-valued object members produce no
  semantic or generated-C change.
