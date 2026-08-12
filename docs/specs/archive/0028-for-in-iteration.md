# RFC 0028: For-In Iteration and Loop Body Delimiters

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-11
- Features: `for`/`in`/`do`, mandatory loop-body delimiters, optional
  zero-based iteration indices, collection iteration, text rune iteration,
  dictionary key/value iteration, and iterator invalidation rules
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0015 (structured control flow), RFC 0018 (String and Rune),
  RFC 0020 (collections), RFC 0026 (deferred cleanup), RFC 0035 (C-style
  copying and manual lifetimes), and RFC 0036 (`Size`)
- Coordinates with: RFC 0029 (Error values) and RFC 0031 (`Stream<T>`)
- Supersedes on implementation: RFC 0015's exact
  `while-statement = "while", expression, block, "end"` production, replacing
  it with the mandatory `do` form defined below; all other RFC 0015 behavior
  remains unchanged

## Summary

Seawitch adds one compiler-provided `for ... in` statement for built-in
collections and text:

```seawitch
for number in numbers do
    print(number)
end

for i, number in numbers do
    print(i, ": ", number)
end
```

The index is optional and always appears first. Dictionary iteration may
similarly expose an iteration index before its key and value:

```seawitch
for key, value in scores do
    print(key, ": ", value)
end

for i, key, value in scores do
    print(i, ": ", key, " = ", value)
end
```

The construct is direct compiler sugar. It adds no iterator protocol,
ownership transfer, hidden allocation, or source-level pointer arithmetic.

## Syntax

```ebnf
for-statement = "for", for-binders, "in", expression
                , "do", block, "end" ;
for-binders   = identifier
              | identifier, ",", identifier
              | identifier, ",", identifier, ",", identifier ;
while-statement = "while", expression, "do", block, "end" ;
```

This RFC adds `for-statement` as an alternative to both canonical
`top-level-item` and `statement`, in the same positions as `while-statement`.
`for`, `in`, and `do` are reserved words. `do` is the mandatory boundary
between a loop header expression and its body for both loop kinds:

```seawitch
while condition do
    work()
end

for value in values do
    work(value)
end
```

There is no optional delimiter form and no `then` spelling for loops. Making
`do` mandatory provides one syntax and lets the parser stop the header at an
explicit token instead of inferring its end from the first body statement.

Binder arity is determined by the source type:

| Source | Without index | With index |
|---|---|---|
| `Array<T,N>` | `value` | `i, value` |
| `View<T>` | `value` | `i, value` |
| `List<T>` | `value` | `i, value` |
| `String` | `rune` | `i, rune` |
| `Strand` | `rune` | `i, rune` |
| `Dict<K,V>` | `key, value` | `i, key, value` |

Every binder name in one loop must be distinct.

## Supported sources

V1 supports only:

- `Array<T,N>` in increasing element-index order;
- `View<T>` in increasing visible-index order;
- `List<T>` in increasing element-index order;
- `String` and `Strand` in decoded Rune order; and
- `Dict<K,V>` in unspecified entry order.

Ranges, integer counting loops, user-defined iterators, Streams, and raw
pointers are not iterable in v1.

A nullable or union source must first be narrowed to one iterable concrete
type:

```seawitch
values: List<Int32> | Nil = read_values()

if values is List<Int32>
    for value in values do
        print(value)
    end
end
```

## Iteration index

An index binder has type `Size`, starts at zero, and increases by one after
each completed or continued iteration. `continue` does not skip the increment.
The index counts values produced by the loop; it is not necessarily a physical
storage offset.

```seawitch
for i, value in values do
    // i is 0, 1, 2, ...
end
```

For String and Strand, `i` is the Rune ordinal, not the UTF-8 byte offset:

```seawitch
for i, rune in "café" do
    // i is 0, 1, 2, 3
end
```

`Size` is the unsigned target-sized integer corresponding to C's `size_t`. It
can represent every valid in-memory collection length and index. This RFC does
not introduce `ISize`: an iteration index is never negative and does not
represent a signed offset or pointer difference.

RFC 0018 and RFC 0020 currently expose `length()` as `UInt64`. They are closed
and remain immutable. A later conformance specification will supersede those
signatures so in-memory String, Array, View, and List lengths return `Size`.
Iteration indices and collection lengths therefore use one canonical type.
Explicit conversion to a fixed-width integer remains available for storage,
serialization, protocols, and FFI. Before that conformance update is
implemented, lowering may checked-convert the old internal `UInt64` length to
`Size`; existing allocation-size limits guarantee that a valid in-memory
collection cannot exceed the target's `Size` range.

For Dict, `i` is the zero-based ordinal of the current produced entry in that
particular traversal. It is not a bucket number, hash value, or stable entry
identity. Because Dict order is unspecified, the same dictionary may associate
different indices with keys in another run or build.

There is no index-only form. A sequence's two binders mean `index, value`; a
Dict's two binders mean `key, value`.

## Source evaluation and traversal boundary

The source expression evaluates exactly once before iteration. Its initial
length or traversal boundary is also captured once:

```seawitch
for value in load_values() do
    print(value)
end
```

`load_values()` is called once. Empty List and View values execute zero
iterations. RFC 0020 permits only positive Array lengths, so `Array<T,0>` adds
no special case.

Source stabilization follows the closest direct C lowering:

- an Array place, such as a local variable or object member, is evaluated to
  its address once and iterated in place; the complete Array is not copied;
- a temporary Array expression is materialized once in a compiler-created C
  temporary and iterated there;
- View is copied as its pointer-and-length descriptor;
- String, List, and Dict are copied as their pointer-sized handles;
- Strand is copied once into a compiler-created inline temporary; its maximum
  32-byte representation makes this ordinary value copy bounded and avoids a
  source-lifetime dependency; and
- the captured source is never reevaluated by an iteration.

Consequently, replacing an element in an Array place during iteration is
visible to later iterations. Iterating a temporary does not create an
independent cleanup obligation.

The source storage must remain valid for the complete loop. Under RFC 0035,
the compiler does not introduce an owner, move, or tracked borrow for it.

## Binder values and scope

Every iteration creates fresh immutable binders in a fresh lexical body scope.
They are visible only inside that iteration and may shadow outer names under
the ordinary block rules:

```seawitch
value: Int32 = 100

for value in values do
    current: Int32 = value
end

print(value) // outer value: 100
```

Assigning a binder is rejected:

```seawitch
for value in values do
    value = 10 // Error: loop binder is immutable
end
```

Element, key, and value binders use RFC 0035's ordinary C-style copy rule.
Inline values are copied inline. Reference-like values copy only their handle
or descriptor; they do not move ownership or perform a deep copy.

V1 has no `for mut value` form and exposes no mutable element reference.

## Mutation and iterator invalidation

Array and List element replacement is permitted when it preserves storage,
length, and layout. Later iterations observe replacement values:

```seawitch
for i, value in values do
    values[i] = value * 2
end
```

The following invalidate an active List iteration:

- `push`, `pop`, `insert`, `remove`, or `clear`;
- `free`;
- allocator reset or destruction; and
- any operation that changes length, capacity, or backing storage.

All Dict mutation during iteration is invalid, including replacement of an
existing value. This single rule avoids depending on whether one implementation
rehashes or relocates an entry:

```seawitch
for key, value in scores do
    scores.insert(key, value + 1)
    // Programmer error: Dict mutation invalidates iteration.
end
```

Invalidation through the source name, an alias, or a called function has the
same meaning. Following RFC 0035's C model, detecting it is not a required
static ownership or alias analysis. An invalidated traversal has no defined
continuation behavior; the programmer must keep it valid.

## Text iteration

String and Strand iteration produces decoded Rune values, never UTF-8 bytes:

```seawitch
for rune in text do
    print(rune)
end
```

Byte iteration is explicit:

```seawitch
for byte in text.bytes() do
    consume(byte)
end
```

String and Strand traversal uses RFC 0018's sequential UTF-8 cursor behavior
and decodes each Unicode scalar exactly once. It must not repeatedly perform
rune indexing, which could make a complete traversal quadratic.

String is immutable, but freeing its allocation through any shallow alias while
iteration is active invalidates the traversal and is a programmer error.
Strand is an inline value; the evaluated loop source contains its own copy.

## Dictionary iteration

Dictionary iteration visits each active entry exactly once when the dictionary
remains valid and unmodified:

```seawitch
for key, score in scores do
    print(key, ": ", score)
end
```

Order is deliberately unspecified and must not be used for deterministic
serialization. It may change with insertion history, capacity, compiler or
runtime version, target, or hash-table implementation.

No bucket index, tombstone, stored hash, capacity, or internal address is
exposed. The optional `Size` index counts produced entries only.

## Control flow and defer

`break` and `continue` target the nearest enclosing loop. `return` exits the
containing function. There is no `for ... else` and no labelled break in v1.

The body is a lexical scope. A defer registered in it runs at the end of that
iteration, including before `continue`, `break`, or `return` leaves the body:

```seawitch
for path in paths do
    temporary: MutPtr<Int32> = h.allocate<Int32>(0)
    defer h.free(temporary)

    if skip(path)
        continue
    end

    process(path)
end
```

## Diagnostics

Required focused diagnostics include:

```text
[Type Error] value of type Node is not iterable
[Type Error] sequence iteration requires one value binder or index and value binders
[Type Error] dictionary iteration requires key and value binders or index, key, and value binders
[Name Error] duplicate loop binder name
[Type Error] loop binder is immutable
```

An iterable union receives the ordinary narrowing diagnostic rather than
runtime dispatch. Directly visible iterator invalidation may be diagnosed as a
quality-of-implementation aid, but this RFC does not require alias or lifetime
analysis.

The parser owns malformed binder lists and missing `in`, `do`, or `end`. A
`for` header without `do` reports `expected 'do' after for source`; a `while`
header without it reports `expected 'do' after while condition`. The checker
owns source iterability, source-specific binder arity, binder types, duplicate
names, and binder assignment. Code generation receives only a checked loop and
must fail closed if its concrete iterable kind is unsupported.

## C23 lowering direction

- The source is stabilized once: Array places use one generated address, Array
  temporaries are materialized, Strand is copied inline, and reference-like
  sources copy their C handle or descriptor.
- Array, View, and List loops capture the initial length and lower to a plain C
  index loop.
- The optional source index binder receives the current checked `Size`
  iteration counter.
- String and Strand loops lower to a byte offset, Rune ordinal, and generated
  UTF-8 decoder.
- Dict loops lower to a bucket scan plus a separate produced-entry ordinal;
  the public index never exposes the bucket index.
- Element, key, and value binders use concrete generated C assignments.
- Generated helpers may use private C pointer operations under RFC 0033, but no
  pointer arithmetic becomes available in Seawitch source.
- Generated names are deterministic and `#line` mappings identify the `for`,
  binder initialization, and source body.

## Deferred

- Numeric ranges and counted loops.
- Mutable element-reference iteration.
- User-defined iterator protocols.
- Reverse iteration, filtering, mapping, and comprehensions.
- Stream iteration and fallible iterators.
- Stable or sorted dictionary iteration.
- Destructuring binders.
- Loop labels and multi-level break.
- `for ... else`.

## Settled implementation contract

1. Dict iteration and its unspecified order are in v1.
2. The optional index is first and has type `Size`.
3. String and Strand indices count Runes; Dict indices count produced entries,
   not buckets.
4. Element, key, and value binders use shallow C-style copying under RFC 0035.
5. Array and List element replacement is permitted. Structural List mutation
   and every Dict mutation invalidate iteration.
6. Iterator invalidation remains programmer responsibility without required
   ownership or alias analysis.
7. The source and traversal boundary are captured once at loop entry.
8. Existing Array places are not silently copied; Strand receives one bounded
   inline copy.
9. Mutable binders, custom iterators, ranges, and Stream iteration are deferred.
10. In-memory collection and text lengths use `Size`; closed RFC 0018 and RFC
    0020 `UInt64` signatures will be superseded through a conformance update.
11. `do` is mandatory after every `for` source and `while` condition; the old
    delimiter-free `while` form is removed rather than retained as an alias.

## Implementation dependencies

RFC 0035 and RFC 0036 are Ready for Implementation. They must be implemented
before RFC 0028 or included earlier in the same approved execution plan:

1. RFC 0036 supplies the canonical `Size` index and length type.
2. RFC 0035 removes the compiler's old affine collection behavior so loop
   binders and captured handles use the shallow C-style copies required here.
