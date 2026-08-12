# RFC 0002: Default-Const Bindings and Mutable Access

- Status: Implemented
- Features: default-const bindings, `mut`, `ref`
- Created: 2026-08-06
- Supersedes on implementation: RFC 0001 address-taking and binding semantics

## Summary

Seawitch declarations are constant by default. The `mut` keyword before a
declaration makes the declared binding assignable. For reference-like values,
a plain right-hand side grants read access while `mut` on the right-hand side
explicitly requests write access.

```seawitch
mut y: Int32 = 42

reader: Ptr<Int32> = ref y
writer: Ptr<Int32> = mut ref y

value: Int32 = reader.value
writer.value = 100
```

`reader` and `writer` are both constant bindings because neither declaration
uses declaration-side `mut`. They cannot store a different address. They
differ in their compile-time access capability: `reader` may only read its
pointee, while `writer` may read or write its pointee.

The generated C23 is equivalent to:

```c
int32_t y = 42;
const int32_t *const reader = &y;
int32_t *const writer = &y;

const int32_t value = *reader;
*writer = 100;
```

The access capability has no runtime representation. It controls which
operations the checker accepts and which C qualifiers the generator emits.

## Motivation

Seawitch needs to distinguish two independent forms of mutation:

1. changing the value stored in a binding; and
2. changing data reached through a pointer or another reference-like value.

Using `mut` in the position of the permission it grants keeps both choices
visible without introducing `var`, `const T`, `Ptr<const T>`, or a second
`ReadPtr<T>` type. The same access model can later be considered for slices,
lists, dictionaries, and method receivers.

The design must retain direct C23 lowering, add no runtime ownership system,
and avoid implying Rust-style lifetime or exclusive-borrow guarantees.

## Amendment to RFC 0001

RFC 0001 remains the implemented language until this RFC is implemented. On
implementation, this RFC makes the following deliberate changes:

1. Prefix `ref place` replaces the built-in `place.addr` property.
2. `.addr` is no longer a built-in pointer property and is rejected by the
   currently supported grammar.
3. The `.value` dereference property and all raw-pointer safety rules remain.
4. `ref` is no longer an ordinary identifier; `ref` and `mut` are reserved
   words.
5. Declarations become constant by default instead of freely assignable.

The canonical grammar, language notes, status, examples, and compiler tests
must be updated together when this RFC is implemented. They continue to
describe RFC 0001 while this RFC has Proposed status.

## Guide-level explanation

### Constant declarations

A declaration without `mut` creates a constant binding:

```seawitch
x: Int32 = 42
x = 43 // Error: x is constant.
```

This is runtime-initialized, read-only storage. It is not necessarily a
compile-time constant. Seawitch does not add `const` or `var` declaration
keywords.

The generator applies outermost `const` to the generated C object:

```c
const int32_t x = 42;
```

### Mutable declarations

The `mut` keyword before a declaration creates an assignable binding:

```seawitch
mut x: Int32 = 42
x = 43
```

```c
int32_t x = 42;
x = 43;
```

Declaration-side `mut` is not repeated on reassignment.

Every declaration continues to require an explicit type annotation:

```seawitch
mut x: Int32 = 42 // Declaration.
x = 43            // Reassignment.
y = 44            // Error: y has not been declared with a type.
```

### Read access through a pointer

The prefix `ref` operator takes the address of a place. Without `mut`, it
produces a pointer with read access:

```seawitch
mut y: Int32 = 42
reader: Ptr<Int32> = ref y

value: Int32 = reader.value
reader.value = 100 // Error: reader has read access only.
```

Plain access is the default even when the addressed storage was declared with
`mut`.

### Mutable access through a pointer

`mut` on the right-hand side explicitly requests write access:

```seawitch
mut y: Int32 = 42
writer: Ptr<Int32> = mut ref y

writer.value = 100
```

The request is valid only when the source place permits mutation:

```seawitch
y: Int32 = 42
writer: Ptr<Int32> = mut ref y // Error: y is constant.
```

`mut` does not modify `y` when evaluated. It creates or propagates permission
for later writes through the resulting reference-like value.

### Access through derived places

Whether a place is writable follows the access path used to reach it, not the
ultimate storage declaration.

For `pointer.value`, the place is writable exactly when `pointer` has write
capability at that pointer layer:

```seawitch
mut y: Int32 = 42
reader: Ptr<Int32> = ref y
writer: Ptr<Int32> = mut ref y

read_alias: Ptr<Int32> = ref reader.value
bad_alias: Ptr<Int32> = mut ref reader.value
// Error: reader.value is read-only through reader.

write_alias: Ptr<Int32> = mut ref writer.value // Valid.
```

The mutable declaration of `y` cannot be recovered through `reader`. This
prevents read access from being laundered into write access.

### Binding mutation and pointee mutation are independent

The four useful pointer combinations are expressed without distinct pointer
types:

```seawitch
fixed_reader: Ptr<Int32> = ref y
mut moving_reader: Ptr<Int32> = ref y
fixed_writer: Ptr<Int32> = mut ref y
mut moving_writer: Ptr<Int32> = mut ref y
```

| Declaration | Rebind pointer | Write pointee | Equivalent C shape |
|---|---:|---:|---|
| `p: Ptr<T> = ref source` | No | No | `const T *const p` |
| `mut p: Ptr<T> = ref source` | Yes | No | `const T *p` |
| `p: Ptr<T> = mut ref source` | No | Yes | `T *const p` |
| `mut p: Ptr<T> = mut ref source` | Yes | Yes | `T *p` |

A constant pointer binding with write access may modify its pointee but may not
store a different address:

```seawitch
fixed_writer: Ptr<Int32> = mut ref y
fixed_writer.value = 100 // Valid.
fixed_writer = ref z     // Error: fixed_writer is constant.
```

A variable pointer binding with read access may store a different readable
address but may not write through it:

```seawitch
mut moving_reader: Ptr<Int32> = ref y
moving_reader = ref z         // Valid.
moving_reader.value = 100     // Error: read access only.
```

### Pointer copies

Using a pointer without `mut` creates or assigns a read-access copy:

```seawitch
writer: Ptr<Int32> = mut ref y
reader: Ptr<Int32> = writer
```

Using `mut` explicitly propagates write access:

```seawitch
writer: Ptr<Int32> = mut ref y
alias: Ptr<Int32> = mut writer
alias.value = 100
```

`mut writer` is valid only when `writer` already has write access. It does not
upgrade a read-access pointer:

```seawitch
reader: Ptr<Int32> = ref y
alias: Ptr<Int32> = mut reader // Error: reader has no write access.
```

Both pointer copies alias the same storage. `mut` does not establish exclusive
access and does not invalidate the original pointer.

### Nested pointers

`Ptr<T>` remains recursive. Each pointer layer has its own access capability.
The ordered capabilities of a nested pointer are its **capability shape**.

```seawitch
mut y: Int32 = 42
writer: Ptr<Int32> = mut ref y
writer_pointer: Ptr<Ptr<Int32>> = ref writer
```

`writer` has capability shape `[write]`. `writer_pointer` has shape
`[read, write]`:

- `read` controls access to the immediate pointer slot containing `writer`;
- `write` controls access through the stored pointer to `y`.

Address-taking adds a new outer capability and preserves the complete shape
of a pointer-valued place underneath it. A plain pointer use attenuates only
the immediate capability exposed by that expression. `mut` explicitly
propagates write capability at that immediate layer when it is available.

Consequently, both of these copies are defined:

```seawitch
reader: Ptr<Int32> = writer_pointer.value
// reader has [read]: plain use attenuates writer's immediate capability.

alias: Ptr<Int32> = mut writer_pointer.value
// alias has [write]: the stored pointer already carried write capability.
```

Reading the pointer slot through `writer_pointer` requires only its outer read
capability. Writing a different pointer into that slot requires outer write
capability:

```seawitch
mut y: Int32 = 42
writer: Ptr<Int32> = mut ref y
mut other_value: Int32 = 7
mut slot: Ptr<Int32> = mut writer
slot_pointer: Ptr<Ptr<Int32>> = mut ref slot
other_writer: Ptr<Int32> = mut ref other_value

slot_pointer.value = mut other_writer // Valid when shapes match.
```

When the target place is itself pointer-valued, assignment must preserve its
complete established capability shape. The same compatibility rules apply
whether the target is a named pointer binding or a pointer-valued `.value`
place.

### Reassignment preserves access capability

A binding's pointer access capability is established by its declaration and
does not change during reassignment.

More precisely, a pointer declaration initializer establishes the binding's
complete capability shape. The written annotation supplies the nominal type;
the initializer supplies this additional static capability metadata. Both are
fixed after the declaration.

A read-access pointer binding accepts plain pointer sources:

```seawitch
mut reader: Ptr<Int32> = ref y
reader = ref z
```

A write-access pointer binding requires `mut` on each new source:

```seawitch
mut writer: Ptr<Int32> = mut ref y
writer = mut ref z
writer = ref z // Error: a writable pointer requires mutable access.
```

Applying `mut` when assigning to a read-access binding is rejected rather than
silently discarding the requested permission:

```seawitch
mut reader: Ptr<Int32> = ref y
reader = mut ref z // Error: reader was declared with read access.
```

## Reference-level explanation

### Terminology

A **binding mode** is either:

- `constant`: the binding cannot be an assignment target; or
- `mutable`: the binding can be an assignment target.

An **access capability** for a reference-like value is either:

- `read`: the referenced value may be observed but not changed; or
- `write`: the referenced value may be observed or changed.

A **place mode** is either `read-only` or `writable`. A variable place is
writable exactly when its binding is mutable. A dereference place is writable
exactly when the pointer used to reach it has write capability at its immediate
layer.

A **capability shape** is the ordered access capability at every layer of a
possibly nested pointer. A `Ptr<T>` where `T` is not a pointer has one layer.
A `Ptr<Ptr<T>>` has two layers.

Binding mode, place mode, and pointer capability are distinct. None describes
ownership or lifetime.

### Grammar

The relevant proposed grammar is:

```ebnf
statement             = declaration | assignment ;
declaration           = [ "mut" ] , identifier , ":" , type_expression
                      , "=" , expression ;
assignment            = assignment_target , "=" , expression ;
assignment_target     = place_expression ;

expression            = [ "mut" ] , unary_expression ;
unary_expression      = reference_expression | postfix_expression ;
reference_expression  = "ref" , place_expression ;
place_expression      = identifier , { "." , "value" } ;
postfix_expression    = primary_expression , { "." , "value" } ;
primary_expression    = identifier
                      | integer_literal
                      | decimal_literal
                      | byte_literal
                      | "true"
                      | "false" ;
```

`mut` and `ref` are reserved words. At the start of a declaration, `mut`
modifies the declared binding. In an expression position, `mut` modifies the
immediate access capability exposed by the right-hand side. The colon in a
declaration makes the two uses syntactically unambiguous.

`ref` is a prefix address-taking operator. It maps directly to C `&` and does
not allocate, copy, extend a lifetime, or create ownership. `mut ref place`
parses as `mut (ref place)`: first take the address, then request write access
through the resulting pointer.

The optional expression-side `mut` takes the complete following unary
expression, including its postfix `.value` chain. Thus `mut writer.value`
parses as `mut (writer.value)`. The parser rejects a second leading `mut` or a
second `ref` where a place must begin. It accepts syntactically valid `mut`
expressions regardless of operand type; the checker then requires a
reference-like result whose immediate layer can supply write access.

`place_expression` describes syntactic place candidates. The checker owns the
addressability and place-mode decision. `.addr` is not part of this grammar.

### Binding rules

1. A declaration without `mut` creates a constant binding.
2. A declaration with `mut` creates a mutable binding.
3. A constant binding is not an assignment target.
4. A mutable binding is an assignment target of its declared type.
5. Declaration-side `mut` does not recursively make referenced data writable.
6. Binding mode does not change after declaration.
7. A pointer declaration's initializer establishes its complete capability
   shape.
8. A pointer binding's capability shape does not change after declaration.

Constness is shallow. For an aggregate, it prevents mutation of storage that
is part of the aggregate itself. Mutation of separately referenced storage is
governed by that reference's access capability. Detailed aggregate and
collection rules belong to their respective RFCs.

### Pointer access rules

1. `ref place` produces a `Ptr<T>` value with a new outer read capability when
   `place` has type `T`.
2. `mut ref place` produces a `Ptr<T>` value with write capability when
   `place` is a writable place of type `T`.
3. `mut ref place` is rejected when `place` is read-only through the access
   path used to reach it.
4. Reading `pointer.value` requires either read or write capability.
5. Assigning to `pointer.value` requires write capability.
6. A plain pointer expression exposes read capability at its immediate layer
   and preserves all deeper layers.
7. `mut pointer_expression` propagates write capability at its immediate layer
   only when the operand already carries it; it preserves all deeper layers.
8. A read-capability binding cannot become write-capable through
   reassignment.
9. A write-capability binding cannot accept a read-only source.
10. Address-taking a pointer-valued place adds an outer capability and
    preserves the place's existing nested capability shape.
11. Reading a pointer-valued `.value` place exposes the stored pointer's
    capability shape, subject to rules 6 and 7 at the newly exposed layer.
12. Assigning to a pointer-valued binding or `.value` place must preserve the
    target's complete capability shape.

The capability shape participates in static compatibility even though it is
not written inside `Ptr<T>`. The compiler records it on checked pointer values,
bindings, and pointer-valued places.

### Raw-pointer safety

`mut` grants write permission; it does not prove safety. In particular, it
does not guarantee:

- exclusive access;
- a valid lifetime;
- non-nullness;
- correct alignment;
- correct allocation provenance;
- freedom from data races; or
- bounds safety.

Multiple write-capability pointers may alias the same storage. Invalid pointer
use retains the undefined-behavior risks specified by RFC 0001.

### C23 lowering

Binding mode controls the outermost C `const` qualification. Pointer access
capability at each layer controls whether that layer's immediate pointee object
is C-qualified as `const`.

| Seawitch property | C23 effect |
|---|---|
| constant scalar binding | `const T name` |
| mutable scalar binding | `T name` |
| constant pointer binding | `... *const name` |
| mutable pointer binding | `... *name` |
| read pointer capability | `const T *...` |
| write pointer capability | `T *...` |

The table describes one pointer layer; it must not be applied by textual
substitution when `T` is itself a pointer. The generator constructs the C
declarator from the inside out and places each qualifier at its corresponding
pointer layer.

For example:

```seawitch
mut y: Int32 = 42
writer: Ptr<Int32> = mut ref y
writer_pointer: Ptr<Ptr<Int32>> = ref writer
```

lowers to the equivalent of:

```c
int32_t y = 42;
int32_t *const writer = &y;
int32_t *const *const writer_pointer = &writer;
```

The `const` adjacent to the inner `*` makes the immediate `writer` pointer slot
read-only through `writer_pointer`; it does not make `y` read-only. This is
distinct from `const int32_t **`, which qualifies the final integer instead.
Valid recursive qualification conversions are emitted directly. The generator
must not insert casts or discard qualifiers to make a declaration compile.

`mut` itself emits no token or runtime operation.

### Diagnostics

The parser owns forms excluded by the grammar, including repeated leading
`mut`, `ref` where a place must begin, and the removed `.addr` property. The
checker owns syntactically valid `mut` expressions with invalid operand types,
plus binding, place-mode, and access violations that require resolved
declarations and types.

Representative diagnostics:

```text
[Type Error] cannot assign to constant x
[Type Error] cannot request mutable access to constant y
[Type Error] cannot take mutable access through read-only place reader.value
[Type Error] cannot write through read-only pointer reader
[Type Error] reader was declared with read access; omit mut
[Type Error] writer requires a mutable pointer source
[Type Error] mut requires a reference-like value
```

Diagnostic wording may be refined during implementation, but every failure
must remain structured and source-located.

## Collections and other reference-like types

Assuming a future collection has reference semantics, this RFC establishes
terminology and syntax that its RFC may reuse:

```seawitch
items: List<Item> = source
editable: List<Item> = mut source
```

It does not define `List<T>` behavior. A collection RFC must separately state
which operations require write capability, whether element and structural
mutation differ, how owned values produce access capabilities, and how the
capability appears in fields, parameters, and returns.

## Drawbacks

The most consequential property of a pointer binding is not visible inside
its mandatory `Ptr<T>` annotation. Readers and writers differ at the
initializer or use site:

```seawitch
reader: Ptr<Int32> = ref y
writer: Ptr<Int32> = mut ref y
```

Capability is therefore an additional static dimension beside the nominal
type. Diagnostics and future tooling must display it clearly. Nested pointers
require a capability at every pointer layer, and future functions, fields,
generics, and C imports will require contracts that preserve those shapes.

Expression-side `mut` also has two related roles: it establishes capability
when initializing a new pointer binding and must match an already established
capability when reassigning an existing place. The syntax is uniform, but the
declaration or assignment context affects the compatibility check.

## Alternatives considered

### General `const T` qualifiers

Rejected. This would spread qualifier propagation and compatibility rules
through every ordinary type, aggregate, generic, and method. Seawitch needs
read versus write access without reproducing C++-style const qualification.

### `Ptr<T>` and `ReadPtr<T>`

Rejected in favor of one pointer type plus an access capability. The two-type
model represents C pointer constness directly but does not generalize as
naturally to lists, dictionaries, slices, and method receivers.

### Infer writable access from declaration-side `mut`

Rejected. Binding reassignment and mutation through a reference are different
operations. Coupling them cannot express a fixed writable pointer or a
rebindable read-only pointer.

### Mutable access by default

Rejected. Plain right-hand-side expressions should provide the least
authority. Explicit `mut` makes the creation and propagation of writable
aliases visible.

### Rust-style exclusive mutable borrowing

Rejected. This RFC grants a write capability but does not add ownership,
lifetimes, moves, reborrowing, or exclusivity. Those features would impose a
substantially larger language and compiler model.

## Unresolved questions

The local declaration, assignment, and pointer behavior is fully specified.
The following require later RFCs when the corresponding language constructs
exist:

- access capabilities in function parameters and return values;
- access capabilities stored in struct fields;
- method receiver access;
- collection element versus structural mutation;
- generic propagation of reference-like access;
- imported C pointer access contracts; and
- diagnostics across module boundaries.

Future designs should keep capability adjacent to the declaration, parameter,
or expression whose permission it changes. This RFC does not choose syntax for
positions that have no initializer. A later RFC may revise these constraints
if the hidden-capability model cannot express function or storage contracts
consistently; it must evaluate that problem explicitly rather than silently
introducing `const T`, `Ptr<const T>`, or a second pointer type.

## Implementation acceptance criteria

Implementation is complete for the currently supported language when
end-to-end compiler tests prove that:

1. declarations are constant unless prefixed by `mut`;
2. only declaration-side `mut` bindings accept reassignment;
3. all declarations still require explicit type annotations;
4. `.addr` is removed and `ref` and `mut` are reserved words;
5. plain `ref` creates a new outer read pointer capability;
6. `mut ref` creates write pointer access only from a writable place;
7. mutable access cannot be recovered through a read-only access path;
8. `.value` reads through either pointer capability;
9. `.value` writes only through write-capability pointers;
10. pointer declaration initializers establish a fixed capability shape;
11. pointer copies default to read access at the immediate layer;
12. `mut` propagates, but never invents, immediate write access;
13. pointer reassignment preserves the target's established capability shape;
14. nested address-taking and dereferencing preserve deeper capabilities;
15. pointer-valued `.value` assignment preserves the complete target shape;
16. generated C places binding and access `const` at the correct recursive
    pointer layers without casts or discarded qualifiers;
17. `mut` adds no runtime helpers or hidden allocation; and
18. all rejected forms produce structured diagnostics and failure output.
