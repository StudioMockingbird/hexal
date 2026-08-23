# RFC 0109: Explicit Dispatch Tables

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-08-22
- Updated: 2026-08-23
- Scope: permit `Fun<...>` members in ordinary objects so programs can build explicit dispatch tables
- Depends on: the existing `Fun<...>` rules, generic types, object values, and
  RFC 0094
- Coordinates with: parser-independent type checking, object layout, function-value generation, C emission, and `docs/reference.md`
- Does not change: generic monomorphization, existing method resolution,
  ownership or lifetime rules, the no-closure rule, or the compiler's
  string-in/string-out boundary

This RFC is specified against Hexal's current manual memory-management model.
Function pointers are non-owning values, object copying remains the existing
shallow copy, and pointer or handle lifetime remains the programmer's
responsibility. RFC 0110 is future work and is not a prerequisite for this
RFC; if a later ownership design changes object copyability, that design must
revisit function-valued members separately.

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

## What this RFC owns, and what RFC 0094 owns

RFC 0094 expands the `Fun<...>` position matrix and removes the object-member
rejection. **After 0094 lands, storing a function value in an object already
works.** This RFC does not repeat that change and must not re-derive it.

What remains, and what this RFC is actually for:

1. **Calling through a function-valued member**, which does not work today and
   is not covered by 0094 — see Syntax.
2. The dispatch-table pattern itself: generic operation tables, the
   no-implicit-context rule, and the static-versus-indirect dispatch contract.

If 0094 is withdrawn or descoped, this RFC inherits the object-member
admission; otherwise it depends on it. Sequencing: 0094 first.

## Syntax

No new syntax is added. Existing object declarations and literals are used:

```hexal
table.operation(arguments)
```

**This form does not resolve to a member today, and the obstacle is the
checker, not the parser.** Probed:

```
type T = { x: Int32, }
t.x(1)      ->  [Type Error] T has no method named x
```

`receiver.name(arguments)` is routed to method resolution, which never
considers object members. The parser already produces the shape.

**Resolution rule.** After the existing receiver typing, builtin dispatch,
import visibility, and nominal method lookup have run, if no method `name` is
found and the receiver's type has a member `name` whose type is `Fun<...>`,
the call resolves to an indirect call through that member. The member's
signature governs arity and argument assignability, and the existing
`Fun<...>` call diagnostics apply unchanged. A method remains a method call;
the member fallback is used only when method lookup finds no method.

**The rule is unambiguous, because a name cannot be both.** Probed:

```
type T = { read: Int32, }
impl T.read(): Int32 do ... end
            ->  [Type Error] T already has a member named read
```

Members and methods already share one namespace per type. The declaration
checker rejects a type that declares a member with the same name as a method,
so no precedence rule is introduced. A valid method call retains its existing
checked representation and lowering.

When neither a method nor a `Fun<...>` member matches, the existing
`has no method named` diagnostic is retained. When a member matches but is not
`Fun<...>`, the diagnostic is
`member <name> is not callable; its type is <type>`.

No special dispatch-table AST node is introduced. A successful indirect member
call uses the existing checked `CallExpression`: its `Operand` is the checked
`MemberExpression` for `receiver.name`, its `OperandType` is the resolved
non-nullable `Fun<...>` type, and its `ResultType` is the function result.
It must not use `MethodCallExpression`, because no receiver adaptation or
method symbol is involved. The existing member-expression renderer supplies
the C function-pointer field expression used as the call callee.

## Type rules

### Object members

`Fun<...>` is valid as an object member after RFC 0094 admits that position.
The member's exact function signature is part of the containing nominal
object's canonical type and generated layout. Placement of function values
elsewhere is governed by RFC 0094; this RFC does not reintroduce the older
blanket restrictions on function results, ADT payloads, collections, Tasks,
or Channels.

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

Under the current manual memory model, object copying copies the function
pointer and every other copyable member shallowly. Copying a table does not
copy or manage the state referenced by a context or state field. The presence
of a `Fun<...>` member adds no cleanup, move, clone, share, or lifetime rule;
the containing object's existing copyability and explicit-lifetime rules
remain authoritative.

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

1. Land RFC 0094's object-member admission and retain its position matrix as
   the sole owner of `Fun<...>` placement eligibility.
2. Extend `checkMethodCall` only after ordinary method lookup fails: resolve a
   matching `Fun<...>` member, check it with the existing function-call rules,
   and build the existing `CallExpression`/`MemberExpression` shape defined
   above. Preserve the existing method path unchanged.
3. Reuse existing function-value initializer, assignment, and nullability
   checking for object members.
4. Reuse the existing C function-pointer declarator for object fields and
   render the checked member expression as the indirect-call callee.
5. Extend canonical-type, generic-specialization, and module-visibility
   validation so a concrete function-member signature remains complete and
   accessible after specialization and import.
6. Update `docs/reference.md` after behavior stabilizes, including the
   position matrix, function rules, object layout, member-call rule, and
   generated-C contract.
7. Add focused integration coverage for fixed, mutable, nullable, generic,
   imported, and nested table members, plus generated-C text assertions.

## Validation

This section is exhaustive. RFC 0109 is complete only when every item below
passes:

- A nominal object with one fixed `Fun<...>` member is accepted and its member
  can be called. Acceptance of the member itself is RFC 0094's Validation item;
  this RFC asserts only that the call resolves.
- `receiver.name(arguments)` resolves to a `Fun<...>` member when the receiver's
  type has no method `name`, with the member's signature governing arity and
  argument types.
- A valid method remains a `MethodCallExpression` and retains its existing
  receiver adaptation and lowering. A type declaring a member with the same
  name as a method is rejected with the existing `T already has a member named
  read` diagnostic; therefore no precedence rule is needed.
- `receiver.name(arguments)` where `name` is a member of a non-`Fun` type is
  rejected with `member <name> is not callable; its type is <type>`, not with
  `has no method named`.
- `receiver.name(arguments)` where `name` is neither a method nor a member
  retains the existing `has no method named` diagnostic unchanged.
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
- An exported object containing a function member can cross a module boundary,
  and the importing module can call that member through the ordinary object
  value; existing object-type visibility rules still apply.
- A copied table copies its function pointer and copyable fields shallowly;
  no function cleanup, hidden context, or state duplication is emitted.
- Direct `Fun<...>` use follows RFC 0094's placement and ownership rules; this
  RFC adds no second restriction matrix.
- A function literal that captures an enclosing binding remains rejected.
- The checked indirect call uses `CallExpression` with a `MemberExpression`
  operand rather than `MethodCallExpression`.
- Generated C contains a compatible function-pointer field and an indirect
  member call through that field, with no generated vtable, tag, context
  field, allocation, dispatcher, or wrapper. The field declaration and call
  are asserted as generated-C text, including the concrete specialized
  signature.
- Existing generic calls and statically resolved methods retain their current
  specialization and receiver behavior.
- Existing programs that do not use function-valued object members produce no
  semantic or generated-C change.
