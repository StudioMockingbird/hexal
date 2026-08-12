# RFC 0001: Raw Typed Pointers

- Status: Implemented; address-taking and binding semantics superseded by RFC 0002
- Feature: `Ptr<T>`
- Created: 2026-08-06
- Related proposal: RFC 0002 (default-const bindings and mutable access)

## Summary

Replace the owning `Ref<T>` type with `Ptr<T>`, a nullable, non-owning typed
pointer that maps directly to C's `T *`. Taking an address uses the built-in
`.addr` property. Reading or writing through a pointer uses the built-in
`.value` property.

```seawitch
x: Int32 = 42
p: Ptr<Int32> = x.addr
p.value = 100
y: Int32 = p.value
```

The equivalent generated C23 is:

```c
int32_t x = 42;
int32_t *p = &x;
*p = 100;
int32_t y = *p;
```

`Ptr<T>` does not allocate, free, retain, validate, or otherwise manage the
memory it addresses. Pointer validity and lifetime are the programmer's
responsibility.

## Motivation

Seawitch needs a pointer model that:

1. maps directly to C23;
2. can represent pointers used by C libraries;
3. works with explicit allocator APIs;
4. adds no ownership runtime or hidden cleanup; and
5. keeps address-taking and dereferencing readable.

The existing `Ref<T>` does not meet these requirements. It is lowered to an
owning wrapper struct, allocates with `calloc`, changes assignment into either
a pointee store or an ownership rebind, and inserts `free` automatically.
Those semantics do not describe a C pointer and conflict with manual memory
management.

This RFC deliberately does not introduce separate safe and unsafe pointer
types. A lifetime-checked pointer would require ownership, borrowing, aliasing,
and escape rules that are outside the current language and compiler. Safe
containers such as slices can be built over raw pointers later.

## Guide-level explanation

### Existing declaration syntax

This RFC does not change declaration or reassignment syntax. As already
required by Seawitch, every new variable declaration has a type annotation:

```seawitch
x: Int32 = 42
p: Ptr<Int32> = x.addr
y: Int32 = p.value
```

A reassignment omits the annotation:

```seawitch
x = 43          // Reassign x.
x: Int32 = 43   // Error: x is already declared.
```

### Taking an address

The postfix property `.addr` takes the address of an addressable value:

```seawitch
x: Int32 = 42
p: Ptr<Int32> = x.addr
```

If an addressable expression has type `T`, its `.addr` property has type
`Ptr<T>`. Taking the address of a pointer therefore produces a pointer to a
pointer:

```seawitch
p: Ptr<Int32> = x.addr
pp: Ptr<Ptr<Int32>> = p.addr
```

Literals and other temporary values are not addressable:

```seawitch
p: Ptr<Int32> = 42.addr // Error: the value is not addressable.
```

### Dereferencing a pointer

For an expression `p` of type `Ptr<T>`, `p.value` denotes the addressed `T`.
It can be read as a value or used as an assignment destination:

```seawitch
y: Int32 = p.value
p.value = 100
```

Dereferencing is explicit. Seawitch does not implicitly dereference pointers
for assignment or member access.

Dereferencing a pointer-to-pointer removes one pointer layer:

```seawitch
pp: Ptr<Ptr<Int32>> = p.addr
q: Ptr<Int32> = pp.value
z: Int32 = pp.value.value
```

### Pointer assignment and aliasing

Assigning a pointer copies its address:

```seawitch
p: Ptr<Int32> = x.addr
q: Ptr<Int32> = p
q.value = 100
```

After the assignment, `p` and `q` alias the same `Int32`. Mutating through
either pointer is observable through the other pointer and through `x`.

Pointer reassignment changes the address stored in the pointer. It does not
modify or free the previously addressed value:

```seawitch
p = other.addr
```

Pointee assignment changes the addressed value without changing the pointer:

```seawitch
p.value = 100
```

### Null and invalid pointers

`Ptr<T>` uses the representation of C's `T *` and may contain the zero address.
This RFC does not add a null literal or null-testing syntax; those require a
separate language feature. C imports and future pointer-producing operations
may nevertheless produce a null pointer.

The compiler does not prove that a pointer is non-null, initialized, live,
aligned, or derived from a particular allocation. Dereferencing an invalid
pointer has the same preconditions and potential undefined behavior as the
corresponding generated C operation.

Examples of programmer errors that the type system does not promise to catch
include:

- null dereferences;
- dangling pointers;
- use after free;
- double free through aliases;
- freeing stack or static storage;
- freeing memory with the wrong allocator; and
- data races through aliased pointers.

This raw-pointer surface is an explicit exception to the general language goal
that every compiled program is memory-safe.

### Allocation and ownership

`Ptr<T>` has no ownership semantics. In particular:

- `.addr` does not allocate;
- copying a pointer does not transfer ownership;
- overwriting a pointer does not free its previous pointee;
- leaving scope does not free its pointee; and
- dereferencing does not validate the pointee.

Allocator APIs, allocation failure, deallocation, arenas, pools, and deferred
cleanup are separate features. An allocator may eventually return `Ptr<T>`,
but the allocator contract, not the pointer type, will define who must release
the allocation and how.

## Reference-level explanation

### Grammar

The relevant grammar additions are:

```ebnf
type_expression = identifier
                | "Ptr" "<" type_expression ">" ;

postfix_expression = primary_expression
                   , { "." , ( "addr" | "value" ) } ;

assignment_target = identifier
                  | postfix_expression ;
```

The final expression grammar may factor postfix parsing differently when
general member access is introduced. The required behavior is that `.addr`
and `.value` bind more tightly than any future unary or binary operator.

`Ptr` is recognized as a type constructor only in a type expression. `addr`
and `value` are built-in postfix property names rather than ordinary fields.

### Type construction

For every resolved type `T`, `Ptr<T>` is a resolved type with element type `T`.
Pointer construction is recursive, so `Ptr<Ptr<T>>` is valid.

Two pointer types are identical exactly when their element types are
identical. There is no implicit conversion between different pointer element
types:

```seawitch
x: Int32 = 42
p: Ptr<Int32> = x.addr
flag_pointer: Ptr<Bool> = p // Error: incompatible pointer types.
```

This RFC introduces no pointer casts, type erasure, `Void`, const-qualified
pointers, volatile pointers, alignment-qualified pointers, or function
pointers.

### Addressability

An expression is addressable when it denotes stable storage rather than a
temporary value. In this RFC, the following expressions are addressable:

1. a declared variable; and
2. the `.value` property of a `Ptr<T>` expression.

Future language features may add addressable array elements, fields, and other
places. An expression does not become addressable merely because its value has
a named type.

Applying `.addr` to an addressable expression of type `T` produces a value of
type `Ptr<T>`. Applying `.addr` to a non-addressable expression is a type error.

### Dereference places

Applying `.value` to an expression of type `Ptr<T>` produces an addressable
place of type `T`. In a value context, the place is read. In an assignment
target context, the place is written.

Applying `.value` to a non-pointer expression is a type error. Static type
checking does not validate the pointer's runtime address.

### Evaluation

The receiver of `.addr` or `.value` is evaluated exactly once. Chained postfix
properties evaluate from left to right.

This requirement prevents a future expression with side effects from being
duplicated during C generation.

### C23 representation and lowering

`Ptr<T>` has the size, alignment, and value representation of a C pointer to
the generated representation of `T`.

Required lowering:

| Seawitch | C23 |
|---|---|
| `Ptr<T>` | `T *` |
| `Ptr<Ptr<T>>` | `T **` |
| `x.addr` | `&x` |
| `p.value` | `*p` |
| `p.value = value` | `*p = value` |
| `p = x.addr` | `p = &x` |

The generator must parenthesize lowered expressions whenever required to
preserve postfix chaining and evaluation order. Generated declarations must
use valid C declarators rather than assuming that every type can be emitted as
a simple prefix string.

Generated code retains the existing `#line` mapping back to Seawitch source.
No pointer wrapper struct, allocation helper, or automatic cleanup is emitted.

### Diagnostics

Diagnostics belong to the earliest phase that can prove the error:

- The lexer owns malformed or unsupported punctuation.
- The parser owns malformed `Ptr<T>` and postfix syntax.
- The checker owns unknown element types, incompatible pointer types,
  non-addressable `.addr`, `.value` on a non-pointer, invalid assignment
  targets, and assignments to undeclared names. Existing declaration and
  reassignment diagnostics are unchanged by this RFC.

Representative diagnostics:

```text
[Type Error] cannot take the address of a non-addressable value
[Type Error] cannot access .value on Int32; expected Ptr<T>
[Type Error] expected Ptr<Int32> initializer, got Ptr<Bool>
[Type Error] expected Int32 initializer, got Bool
```

Diagnostic wording may be refined during test-driven implementation, but each
case must remain structured, source-located, and owned by the stated phase.

## Removal of `Ref<T>`

Implementing this RFC removes all language behavior associated with the old
reference feature:

- `Ref<T>` is no longer a recognized type constructor.
- `ref` is no longer an allocation expression or reserved keyword.
- Reference wrapper structs are no longer generated.
- The compiler no longer emits an allocation helper for declarations.
- Assignment never implicitly changes between store-through and ownership
  rebind behavior.
- The compiler never inserts `free` for pointer variables.

After removal, the spelling `ref` is available as an ordinary identifier.

## Drawbacks

1. Pointer code can introduce undefined behavior that the compiler cannot
   diagnose in general.
2. A pointer value does not communicate ownership, length, nullability, or
   lifetime.
3. C APIs that use `T *` for arrays will need slices, explicit lengths, or a
   future many-item/C-pointer facility.
4. C qualifiers and opaque pointers require follow-up design.
5. The language's absolute memory-safety goals must explicitly exclude raw
   pointer operations and foreign code.

## Alternatives considered

### Keep `Ref<T>`

Rejected because its wrapper representation, hidden allocation, ownership
rebind, and automatic cleanup do not map directly to C pointers or manual
memory management.

### Add both `Ptr<T>` and `UnsafePtr<T>`

Rejected for this feature. A genuinely safe pointer requires lifetime and
alias analysis rather than a second name for the same address. Adding both
types without those semantics would provide false confidence.

### Add a lifetime-checked pointer

Deferred. This requires a coherent ownership and borrowing model across
functions, aggregates, control flow, generics, threads, allocators, and C
calls. The current compiler and language do not yet have the constructs needed
to justify that complexity.

### Use C's `&` and `*` syntax

Rejected in favor of `.addr` and `.value`. The property syntax reads from left
to right, composes with future member access, avoids overloading `*` as both a
type and expression operator, and agrees with the language goal that every
value behaves like an object.

## Unresolved questions

None for the core pointer semantics in this RFC.

The following require separate RFCs rather than amendments to this feature:

- null literal and null testing;
- allocator interfaces and allocator passing;
- explicit deallocation and deferred cleanup;
- slices and C array pointers;
- `Void` and opaque C types;
- const, volatile, restrict, and alignment qualifiers;
- pointer casts and arithmetic;
- function pointers and callbacks;
- C header importing; and
- optional `unsafe` syntax or safety-mode boundaries.

## Implementation acceptance criteria

Implementation is complete when end-to-end compiler tests prove that:

1. `Ptr<T>` and nested pointer types resolve correctly;
2. `.addr` lowers to C address-taking without allocation;
3. `.value` supports both reads and assignment targets;
4. pointer copying aliases rather than cloning or transferring ownership;
5. pointer reassignment and pointee assignment remain distinct;
6. incompatible pointer element types are rejected;
7. non-addressable address-taking and non-pointer dereference are rejected;
8. `Ref<T>` and `ref` allocation syntax are rejected;
9. generated C contains no reference wrapper, allocator helper, or automatic
    `free`; and
10. all failures produce structured diagnostics and failure output.
