# RFC 0020: Core Collections

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-11
- Features: fixed arrays, read-only views, growable lists,
  dictionaries, indexing, bounds-safe collection operations, and the revised
  heap-backed String representation used by collections
- Created: 2026-08-09
- Depends on: RFC 0001 (raw pointers), RFC 0006 (core object values), RFC 0007
  (mutability redesign), RFC 0014 (unions), RFC 0019 (generic types, functions,
  and methods), RFC 0024 (equality, ordering, and hashability), and RFC 0026
  (allocation, deallocation, and deferred cleanup)
- Coordinates with: RFC 0016 (explicit numeric conversions), RFC 0018 (String
  and Rune values), and the future iteration specification
- Supersedes on implementation: the exact RFC 0014, RFC 0018, RFC 0022, and
  RFC 0026 clauses listed below. Those predecessor specs remain immutable; this
  RFC records the later replacement design.

The supersession is limited and exact. On implementation, this RFC replaces:

- RFC 0018's `sw_string` by-value header and its corresponding C23-lowering
  bullet;
- RFC 0018's rule that `String.slice` returns a borrowed `String`;
- RFC 0018's collection rules that place a String header inline in a
  collection slot; and
- RFC 0018's deferral of general String assignment transfer and its assignment
  of collection String copy, move, and `free` ownership to RFC 0018;
- RFC 0018 acceptance criteria 8, 9, and 15 only where they require the old
  slice result or physical representation;
- RFC 0014's general complete-value union-member rule only for the managed
  `String`, `List`, `Dict`, and `View` forms that this RFC rejects from unions
  in v1;
- RFC 0022's allowance of String payload fields, including its `Drawing`
  example, until a later RFC defines ownership-bearing aggregate payloads; and
- RFC 0026 references to a borrowed String slice. Under this RFC,
  `String.slice` returns `View<Byte>`, while collection reads may produce a
  borrowed String value.

All other RFC 0018 syntax, UTF-8 validity, rune access, literals, equality,
ordering, `Strand`, `Byte`, and `Rune` decisions remain authoritative.
RFC 0014's rules for every union whose members remain eligible, RFC 0022's ADT
and match rules for inline payloads, and RFC 0026's raw allocation and cleanup
rules remain authoritative.

## Summary

Seawitch currently has scalar values, nominal objects, and raw pointers but no
standard collections. This RFC defines four core collection families:

```seawitch
fixed: Array<Int32, 4>
view: View<Int32>
values: List<Int32>
lookup: Dict<Strand, Int32>
```

- `Array<T, N>` is a fixed-size inline sequence;
- `View<T>` is a non-owning read-only contiguous view;
- `List<T>` is a growable contiguous sequence;
- `Dict<K, V>` is a hash table with explicit key requirements.

All collection accesses are bounds-safe. Invalid indexing and invalid
collection state produce a defined runtime trap rather than C undefined
behavior.

## Scope and dependencies

This RFC defines collection types, semantic operations, generic operation
requirements, layout obligations, their v1 allocator boundary, and the later
String representation required for collection integration. `Array<T,
N>` is inline and needs no allocator. `View<T>` is borrowed and non-owning.
`List<T>` and `Dict<K,V>` are heap-backed reference types. Each local owning
binding stores one pointer-sized handle; both the collection header and its
backing storage are allocated through an explicit `Heap` and are freed
explicitly through that same allocator. Owning collection bindings have only
`live` and `freed` source-level states. They cannot be copied or generally
transferred; the sole ownership handoff is a terminal function return:

```seawitch
h: Heap = Heap.new()
values: List<Int32> = List<Int32>.new(h)
defer values.free(h)
```

This follows RFC 0026. It does not introduce `Ref<T>`, implicit destruction,
or ownership based on `mut`. `mut` controls only whether a binding may be
reassigned. It does not control writes to the separately allocated collection
object. Consequently, `push`, `set`, `clear`, `insert`, and `remove` may be
called through a fixed live collection binding. This is the same distinction
as a fixed `MutPtr<T>` binding whose pointee remains writable.

## Grammar owned by this RFC

RFC 0019 owns ordinary generic type arguments. RFC 0020 adds the special
numeric array argument, indexing, array literals, and type-qualified
collection constructors:

```ebnf
array-type                    = "Array" , "<" , type-expression
                              , "," , positive-decimal-literal , ">" ;
positive-decimal-literal     = nonzero-decimal-digit
                              , { decimal-digit | "_" , decimal-digit } ;
index-suffix                  = "[" , expression , "]" ;
array-literal                 = "[" , expression
                              , { "," , expression } , [ "," ] , "]" ;
collection-constructor        = collection-constructor-type
                              , "." , "new" , call-arguments ;
collection-constructor-type   = "List" , type-argument-list
                              | "Dict" , type-argument-list
                              | identifier ;
postfix-expression            = primary-expression
                              , { "." , identifier
                                | call-arguments
                                | generic-call-suffix
                                | index-suffix } ;
place-expression              = identifier
                              , { "." , identifier | index-suffix } ;
primary-expression            = identifier | object-literal
                              | integer-literal | decimal-floating-literal
                              | "true" | "false" | "nil"
                              | array-literal
                              | collection-constructor
                              | "(" , expression , ")" ;
```

The productions above extend RFC 0019's ordinary generic grammar. The parser
recognizes `Array<T,N>` as this built-in form only when the second argument is
a positive decimal literal; it is not a general value parameter for user
generics. `collection-constructor` is a compiler-owned intrinsic form; it is
not an ordinary user-declared static method. Its `identifier` form is accepted
only when that identifier resolves in the type namespace to a transparent alias
whose canonical type is one concrete `List<T>` or `Dict<K,V>` specialization:

```seawitch
type Numbers = List<Int32>
numbers: Numbers = Numbers.new(h)
```

This follows RFC 0005's rule that a transparent alias denotes the same
canonical type. An ordinary value named `Numbers`, an alias to another type,
and module-qualified aliases are not collection constructors in v1. The checker
rejects a numeric second argument for every generic type other than `Array`. Array literals
require an expected `Array<T,N>` type and are checked for exactly `N` elements.
The bracket form is disambiguated by grammar position: after a primary
expression it is an `index-suffix`; at the beginning of a value expression it
is an `array-literal`.

All collection operations listed by this RFC are compiler-owned built-ins.
They are resolved from the receiver's canonical built-in type before ordinary
user method lookup, cannot be redeclared with `impl`, and do not rely on RFC
0008 permitting user methods on these receiver families. Unsupported method
names fail normally; they do not fall through to generated C members.

## Type forms

### Fixed arrays

`Array<T, N>` has one element type and one positive compile-time length:

```seawitch
numbers: Array<Int32, 4>
```

`N` is a positive decimal integer literal in this built-in form. RFC 0019 owns
user-declared generic parameters; this is an explicit built-in exception and
does not introduce general constant generic parameters. `N` must be positive
in the first version so every array has a C-compatible element region;
zero-length collections use `List<T>` or `View<T>`.

An array stores its elements inline and contiguously in declaration order. Its
size is `N * sizeof(T)` after substitution, and the product must fit the
target's representable object-size range. `Array<T, N>` is distinct from
`Array<U, N>` whenever `T != U`. There is no separate heap-backed data region
and no array header. A local array will normally occupy automatic C storage,
just as a local `Strand` is inline, but stack placement is not a language
guarantee: an array stored in an object, another inline aggregate, or static
storage lives with that containing value.

The v1 inline element class is recursive:

- scalar values, `Byte`, `Rune`, `Strand`, `Ptr<T>`, and `MutPtr<T>`;
- `Nil`-containing unions whose alternatives are all inline elements;
- `Array<U,M>` when `U` is an inline element; and
- objects and ADTs whose members and variant payloads are all inline elements.

`String`, `List<T>`, `Dict<K,V>`, `View<T>`, function values, and any object or
ADT containing one of those values are rejected as array elements in v1. This
keeps array copying ordinary value copying and requires no hidden cleanup for
an inline array. A recursive by-value type or any type whose substituted size
is incomplete, infinite, or not representable is rejected before generation.

An array literal is valid only with an expected `Array<T, N>` type and must
contain exactly `N` elements. Elements are evaluated left-to-right and each is
checked against `T`:

```seawitch
fixed: Array<Int32, 3> = [10, 20, 30]
```

The bracket form is an array literal only; it is not a list literal and does
not create an implicit conversion to `List<T>` or `View<T>`. Array assignment
and parameter passing use ordinary value copying of the complete inline
region. The destination must be a writable place for assignment; the array
element type is always copied as a whole, subject to the existing object and
ADT value-copy rules.

### Views

`View<T>` is a non-owning, read-only contiguous view:

```seawitch
view: View<Int32>
```

Its abstract representation is a pointer to the first element and a count.
The view does not own or resize storage. An empty view is valid and has zero
length; its data pointer is never dereferenced.

`View<T>` provides the same read-only contiguous access operations as the
contiguous collection sources:

```seawitch
view.length()
view.is_empty()
view.at(index)
view[index]
view.slice(start, end)
```

The view's lifetime is source-tied, not inferred from the C pointer. A view may
be held in a local binding or passed to a parameter, but it may not be returned,
stored in an object or ADT, used as a union alternative, placed in module data,
or stored in another owning collection in v1. `ref` cannot take the address of
a View binding, and `Ptr<View<T>>` and `MutPtr<View<T>>` are rejected. The
checker records the root source place for every view. V1 uses lexical view
lifetimes: a view remains live until the end of the lexical scope containing
its binding. Last-use or non-lexical lifetime inference is deferred.

Every view must resolve to stable source storage. Valid roots are an Array
local, parameter, or member place; a live List; a static or live owning String;
a valid String parameter or collection-derived String borrow; or another View
that already has one of those roots. Slicing a temporary View preserves its
original stable root.
A view may not be rooted in a temporary Array or an unbound owning result:

```seawitch
view: View<Int32> = make_array().slice(0, 2) // Error: temporary Array root
```

An unbound View used directly as a function argument lives through that
complete call expression and cannot be retained. A static String literal is a
stable root for its complete program lifetime.

While a view is live, operations that can invalidate its root storage are
rejected:

- freeing or reassigning a source with a live derived view is rejected;
- list `push`, `pop`, `clear`, and `free` are rejected while a view into the
  list's contiguous element region is live;
- list `set` and indexed assignment remain valid and the view observes the
  replacement element;
- whole-array reassignment is rejected while a view into that array is live,
  while array element or member assignment remains valid and observable; and
- freeing or replacing an owning String is rejected while a view returned by
  `bytes()` or `slice()` is live.

A `RuneCursor` is not a `View<Rune>`, but it is another source-tied immutable
borrow. Freeing or replacing its source String is rejected while the cursor is
live, and a cursor cannot be returned, stored, or captured by `defer`.

Passing a list to a user function is rejected while any view derived from that
list is live because every v1 list parameter permits structural mutation. A
single call therefore cannot receive both a list and a view rooted in that
same list. The checker also rejects a view returned from a function or captured
in a deferred expression. A view copied
to another local remains tied to the same root source. A view sliced from
another view is tied to the original root source, not to the temporary view
header. A view passed to a parameter is borrowed for the duration of that call
and cannot be retained by the callee.

There is no `MutView<T>`. Array mutation requires a mutable array place.
List mutation writes through its heap reference and therefore does not require
a `mut` handle binding. Mutation never occurs through a view.

`View<Byte>` and `View<Rune>` are ordinary specializations of this same
contiguous view type. The element type describes the values physically present
in the backing region; `View<Rune>` therefore requires storage containing actual
`Rune` values and is not a decoded UTF-8 string view.

In v1, `T` for `View<T>` must be an inline element. `View<String>`,
`View<List<U>>`, `View<Dict<K,V>>`, `View<View<U>>`, and views of aggregates
containing those values are rejected. A view read therefore never creates or
transfers an owning payload, and a view cannot accidentally expose or copy an
owning String pointer.

Indexing a view produces a read-only element place. Replacing the element or
mutating an object member through that place is rejected. If the element itself
is a pointer with an explicitly writable pointee, that pointee capability is
preserved:

```seawitch
nodes: View<MutPtr<Node>> = ...
nodes[0] = another       // Error: the view is read-only
nodes[0].value = 42      // Valid: MutPtr<Node> permits pointee mutation
```

An empty view has length zero and may use a null data pointer in generated C;
the pointer is never dereferenced. A view created by slicing another view is
source-tied to the original storage, not to the temporary view header.

`View<T>` itself is not a collection element type in v1. This prevents a
borrowed view from being hidden inside an owning value whose source lifetime
the checker cannot expose at the use site.

### String representation revision

This RFC revises the collection-facing String decision and, when implemented,
supersedes RFC 0018's earlier by-value String header. `String` becomes an
immutable heap-backed reference type with the same broad ownership shape as
`List` and `Dict`, while retaining UTF-8 and rune-oriented operations.

A source-level String value is one pointer-sized handle:

```c
typedef struct sw_string {
    const uint8_t *data;
    uint64_t byte_length;
} sw_string;

typedef struct sw_string_storage {
    sw_string header;
    uint8_t bytes[];
} sw_string_storage;

const sw_string *text;
```

For a runtime-created String, the header and UTF-8 bytes occupy one allocation.
The bytes immediately follow the header, `data` points at that trailing region,
and one trailing NUL follows the logical payload:

```c
storage->header.data = storage->bytes;
```

`byte_length` excludes the trailing NUL and is authoritative; embedded NUL
bytes remain valid String content. Allocation size is checked as
`offsetof(sw_string_storage, bytes) + byte_length + 1`. The `data` field
is intentionally retained even though runtime data is trailing: it gives runtime and static
Strings one ordinary C23-safe access path without relying on dereferencing
storage beyond a declared C object. The complete object is immutable after
construction. RFC 0026's hidden allocation metadata retains allocator identity
and validates `free(h)`, so the public String header needs no allocator field.
Because `header` is the first storage member, its address is also the allocation
payload address used by the type-owned free helper.

Runtime constructors, `to_string`, concatenation, and collection String-copy
operations each create one combined header-and-data allocation. `free(h)` frees
that one object. `defer text.free(h)` captures the stable pointer-sized owning
handle using RFC 0026's ordinary direct-call capture rule.

String literals remain allocation-free at runtime. The generator emits a
read-only static aggregate whose header is first and whose UTF-8 bytes and
trailing NUL immediately follow it; the header's `data` points to that byte
member. A literal uses a generated fixed-size storage struct because C does not
initialize flexible array members. Runtime and literal helpers always read
through `text->data`. The empty String uses a shared static empty object. Every
valid String handle is non-null; `Nil` therefore remains
representation-distinct. `String | Nil` is rejected in v1 because conditional
ownership of a runtime String is not defined. Static literal handles have no
cleanup obligation, may be copied freely, and cannot be freed.

Passing a String to a function creates a call-duration immutable borrow of the
same object. The caller retains any cleanup obligation. A String parameter
cannot free or retain the object, and an owning String handle cannot be
shallow-copied into another owner. Explicit owning-copy operations allocate a
distinct combined object.

A runtime-created String is an affine owner. An owning String may exist only
as a script/function local, a terminal owning function result, or a direct
String element owned internally by `List` or `Dict` as specified by this RFC.
A non-owning String may exist as a borrowed parameter or as a source-tied local
borrow obtained from a collection read. String is rejected in object and ADT
members, union alternatives, Arrays, module data, and pointer or address forms
such as `Ptr<String>` and `MutPtr<String>`. The direct String
collection-element case is compiler-managed and does not permit a user-visible
owner alias.

The checker assigns every String binding one provenance class at initialization:

- `static`, for a literal or a copy of another static String reference;
- `owning`, for a runtime constructor, explicit owning copy, move-out, or
  owning function result;
- `parameter-borrow`, for a String function parameter; or
- `collection-borrow(root)`, for a non-removing String read from one specific
  List or Dict root.

Provenance is not inferred from the shared C pointer representation and never
changes implicitly. A `mut` static binding may be reassigned only from another
static String. An owning binding follows the freed-to-fresh-owner reassignment
rule below and cannot become static or borrowed. A parameter borrow cannot be
reassigned. A collection-derived borrowed local cannot be reassigned in v1;
use a nested lexical scope for a different borrow. Copying a static reference
remains static, while copying a collection borrow creates another borrow with
the identical root.

Initializing or assigning from an existing runtime owner is rejected because
it would create two handles with one cleanup obligation. A second owner must
be created explicitly with `to_string(h)`. A `mut` String owner may be reused
only after its current object has been freed, and only with a fresh owning
constructor, explicit owning copy, or owning function result:

```seawitch
owned: String = String.from_bytes(h, bytes)
defer owned.free(h)

other: String = owned                 // Error: owning handle cannot be copied
copy: String = owned.to_string(h)      // valid: distinct allocation
defer copy.free(h)
```

A user function may return an owning String. The return expression must be a
fresh owning constructor or conversion result, an owning String result from
another call, or a live local runtime owner without an active deferred
cleanup. The return is a terminal ownership handoff; the caller must directly
initialize a local owner, replace a freed `mut` owner, or return it again.
Discarding or nesting an owning result is rejected. A borrowed parameter and
a static literal cannot be returned as an owning String; a literal must first
be copied into runtime storage through an explicit allocator:

```seawitch
fun make_text(h: Heap): String
    return "ready".to_string(h)        // fresh owner handed to caller
end

text: String = make_text(h)
defer text.free(h)
```

Runtime String owners use the same `uninitialized`, `live`, and `freed`
forward ownership states, exact control-flow merge rules, stable deferred
cleanup capture, and exactly-once cleanup requirements defined below for List
and Dict owners. A static String reference has no owner state or cleanup
obligation. A String parameter is a call-duration borrow and cannot be stored,
returned, freed, or captured by `defer`. A collection-derived String borrow is
governed by the source-tied rules under collection reads below.

`String.bytes()` remains a zero-copy `View<Byte>` over the complete payload.
`String.slice(start, end)` now returns a zero-copy source-tied `View<Byte>`, not
a borrowed String handle. Its indices remain a zero-based, end-exclusive rune
range. The implementation finds rune boundaries, so the selected bytes are
valid UTF-8, although `View<Byte>` does not encode that guarantee in its type.
The view becomes invalid when its source owning String is freed or replaced.
Creating an independently owned String from the view requires
`String.from_bytes(h, view)`, which validates and copies it.

The revised String operation contracts affected by this decision are:

```text
bytes()                       -> View<Byte>; complete byte range
slice(start, end)             -> View<Byte>; rune-bounded byte range
to_string(heap)               -> String; new owning combined object
concat(heap, other: String)   -> String; new owning combined object
free(heap)                    -> no value; owning runtime String only
```

### Lists

`List<T>` is an explicitly freed, growable contiguous sequence:

```seawitch
values: List<Int32> = List<Int32>.new(h)
```

Its abstract state is a pointer-sized owning handle to a heap-allocated header.
The header contains a data pointer, length, capacity, and allocator identity.
The elements live in a separate contiguous heap-backed region so that growth
may replace that region without relocating the stable header. `List<T>` may be
empty and grows when elements are added. It is a nominal built-in generic
reference type, not an alias for `View<T>`.

The v1 list element class is `CollectionElement<T>`:

- every inline element class allowed for `Array<T,N>`;
- `String`, stored as a pointer-sized owning handle to a separately allocated
  immutable combined String object; and
- no `List`, `Dict`, `View`, function value, or object/ADT containing one of
  those non-inline values.

The list's element slots remain contiguous. A `List<String>` slot contains one
`const sw_string *` handle; the referenced immutable String header and UTF-8 bytes
occupy one nested allocation made through the list's retained allocator. The
list owns and frees those nested String objects. These String objects are the
only nested allocations permitted by the v1 collection element class.

`List<T>` does not support owning-handle copying or assignment transfer in v1.
An owning local must be initialized by `List<T>.new(h)` or by an owning
collection result returned from a function. Assignment from another list,
initialization from another local list, and storing a list in an
aggregate are rejected because each would create or relocate ownership without
a terminal handoff.
A `mut` local may be reused only after its current collection has been freed,
and only with a fresh constructor or owning function result:

```seawitch
mut values: List<Int32> = List<Int32>.new(h)
values.free(h)
values = List<Int32>.new(h)  // valid: fresh owner replaces a freed owner
defer values.free(h)

other: List<Int32> = values  // Error: owning handles cannot be copied
```

The compiler rejects replacement of a live owning binding because that would
leak its collection. A freed fixed binding remains unusable. A freed `mut`
binding may be assigned a fresh constructor or owning function result, after
which it is live again. Constructor results cannot be discarded, passed directly to a function,
stored, or otherwise escape without being installed in a local owner or
returned as an owning result.

The states are:

| Operation | Required state | Result state |
|---|---|---|
| direct local initialization with `List<T>.new(h)` | uninitialized | live empty list |
| initialization with an owning function result | uninitialized | live returned list |
| fresh owning-result assignment to a `mut` owner | freed | live list |
| `free(h)` | live owner | freed owner |
| `push`, `pop`, `set`, `clear` | live owner or borrow | unchanged |

A `List<T>` function parameter is a non-owning borrow of the caller's same heap
object for the duration of the call. Passing it copies only a non-owning C
pointer at the ABI boundary; it does not copy the source-level collection or
transfer ownership. The callee may read and mutate the collection but may not
free it, return it, store it, capture it in `defer`, or initialize another
owning binding from it. Passing a borrowed parameter onward creates another
call-duration borrow. Lists and dictionaries cannot be object/ADT members in
v1.

A function may return an owning `List<T>` or `Dict<K,V>` that it created. The
return expression must be either a direct collection constructor, an owning
collection result from another call, or a live owning local without an active
deferred cleanup. Returning performs a terminal ownership handoff to the
caller. The returning function exits immediately, so the source binding has no
subsequent `moved` state. A borrowed parameter can never be returned:

```seawitch
fun make_values(h: Heap): List<Int32>
    values: List<Int32> = List<Int32>.new(h)
    values.push(1)
    return values                  // ownership handed to caller
end

h: Heap = Heap.new()
values: List<Int32> = make_values(h)
defer values.free(h)
```

Every path leaving a collection-returning function must either return exactly
one live owner or clean up every owner created on that path. A returned owning
result must immediately initialize a local owner, replace a freed `mut` owner,
or be returned again. Discarding it or passing it directly to a borrowed
parameter is rejected because no binding would hold its cleanup obligation.

V1 has no read-only collection-reference type. Every `List<T>` or `Dict<K,V>`
parameter permits the operations defined for that collection, including
mutation. A caller that needs to expose only contiguous read access passes a
`View<T>` instead. Read-only dictionary borrowing is deferred.

V1 permits a List or Dict handle only as a script/function local owner, a
borrowed parameter, or an owning function result governed above. It rejects
List and Dict in object or ADT members, union alternatives, arrays, other
collections, and module data. `ref` cannot take the address of a List or Dict
binding, and `Ptr<List<T>>`, `MutPtr<List<T>>`, `Ptr<Dict<K,V>>`, and
`MutPtr<Dict<K,V>>` are rejected. These restrictions prevent a borrowed or
owning handle from escaping the compiler-visible ownership boundary.

### Dictionaries

`Dict<K, V>` maps keys to values. In v1, the key type is deliberately limited
to exactly `Int32` or `Strand`:

```seawitch
scores: Dict<Int32, String> = Dict<Int32, String>.new(h)
labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)
```

`Dict<K, V>` uses RFC 0019's generic specialization machinery, but a concrete
dictionary specialization is valid only when `K` is `Int32` or `Strand`.
Other types are rejected even if RFC 0024 defines equality or hashing for them.
`V` must be a `CollectionElement<V>`: an inline element or direct `String`,
with no nested `List`, `Dict`, `View`, function value, or aggregate containing
one of those values. Dictionary iteration order is unspecified and is not part
of equality or generated C output.

Like `List`, a dictionary local is a pointer-sized owning handle to a
heap-allocated header. Its buckets and stored keys/values occupy a separate
contiguous heap-backed region selected by the retained allocator. The header
contains a bucket pointer, length, capacity, and allocator identity. A stored
`String` value has one pointer-sized handle in the bucket region. Its immutable
header and UTF-8 bytes occupy one combined nested allocation owned by the
dictionary. The primary bucket region remains contiguous; nested String
objects are the only additional allocations permitted in v1.

The initial dictionary does not expose bucket layout, hash seed, load factor,
or tombstone representation as language semantics. The implementation must
still maintain an explicit active-entry state and must never read or destroy
an inactive bucket's key or value.

`Dict<K,V>` uses the same owning-local, borrowed-parameter, and terminal-return
rules as `List<T>`. It cannot be copied, assignment-transferred, or stored in
an object or ADT in v1.

## Construction and allocation

Dynamic collections use explicit allocator handles:

```seawitch
h: Heap = Heap.new()
values: List<Int32> = List<Int32>.new(h)
scores: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)
```

`List<T>.new(h)` and `Dict<K,V>.new(h)` immediately allocate and initialize the
collection header. They create valid empty collections with null primary
storage pointers, zero length, and zero capacity; the first growth operation
allocates the separate element or bucket region. The allocator identity is
retained in the header, and all later growth and free operations must use that
same identity. An implementation must not silently substitute another
allocator.

RFC 0026 v1 defines only `Heap.new()`, and every such handle selects the same
default allocator identity. Therefore `sw_heap_id` is a validation and dispatch
token for that default allocator; generated growth helpers invoke the default
checked allocator selected by that token. This RFC does not claim that an
identity alone is sufficient for future Arena, Pool, or user allocator
contexts. A later allocator RFC must replace or extend the retained header
field with the complete callable allocator descriptor before those allocators
can construct List or Dict values.

A dynamic collection created by `new` must be freed exactly once on every
normal and early exit path. The checker accepts either a direct `free(h)` on
every path or a deferred cleanup:

```seawitch
values: List<Int32> = List<Int32>.new(h)
defer values.free(h)
```

For a type-owned collection `free` call, the deferred action captures a
copy of the pointer-sized owning handle at registration, following RFC 0026's
ordinary direct-call capture rule. The handle points to the stable heap header,
so later mutation or backing-region reallocation needs no special cleanup
reference. The captured cleanup owns no second source-level collection and is
usable only by the registered cleanup action.

A binding with an active deferred `free` may be mutated, but may not be
reassigned or freed a second time before scope exit. A deferred
cleanup is registered only when control reaches the statement. The checker
tracks cleanup obligations through branches, lexical scopes, loops, `return`,
`break`, and `continue`; every live owning collection must have exactly one
active cleanup obligation or direct free at each exit. A
collection freed on one branch cannot also be freed by a deferred action on
that same path. Forgetting to free a dynamic collection is a compile-time
error.

`free(h)` transitions a live collection to `freed`, destroys stored elements
from the last storage slot toward the first, destroys nested String objects,
frees the primary backing region, and finally frees the heap header through the
retained allocator. An empty live collection still owns a header and must be
freed exactly once. Reading, mutating, or freeing a freed owner is rejected. A
freed `mut` owner may only receive a fresh constructor or owning function
result.

### Ownership flow and merge rules

Collection ownership is checked as forward dataflow over `uninitialized`,
`live`, and `freed` owner states plus the presence of one deferred cleanup.
Every reachable control-flow merge must agree exactly on each owner's state and
cleanup obligation. If one continuing branch leaves an owner live and another
leaves it freed, compilation fails; the compiler does not invent a nullable or
conditional owner:

```seawitch
values: List<Int32> = List<Int32>.new(h)

if release_now
    values.free(h)
end

values.push(1) // Error: continuing branches disagree on values
```

A terminal `return` has no outgoing state. Before returning, every other live
owner on that path must be freed or covered by exactly one deferred cleanup.
Returning one owner discharges that owner's obligation into the result; it does
not discharge unrelated owners.

Each loop has a state invariant. The state and cleanup obligations at every
back edge and `continue` must equal the state at the start of that iteration.
The zero-iteration path participates in the state after the loop. Owners
created inside an iteration must be freed, returned, or covered by an
iteration-scoped defer before a back edge, `continue`, or `break`. Freeing an
outer owner only on a possibly executed iteration is rejected when continuing
control would merge live and freed states.

Each `if`, `elseif`, `else`, and `match` arm is checked independently and then
merged by the same exact-state rule. Collection-valued `match` expressions are
rejected in v1; ownership-bearing match results are deferred rather than
introducing an additional conditional handoff form. Match may still inspect or
mutate a borrowed collection under the ordinary parameter and view rules.

String provenance participates in every continuing control-flow merge. Static
String references merge only with static references. Collection borrows merge
only when every continuing path has the same root collection; different roots
and mixed static, owning, parameter-borrow, or collection-borrow provenance are
rejected. A String-valued `match` may return only static references, or
collection borrows whose arms all have the same root. An owning String result
from `match` is rejected in v1:

```seawitch
label: String = match condition
    | true then "yes"
    | else then "no"
end                                  // valid: static on every arm

name: String = match condition
    | true then first.at(0)
    | else then second.at(0)
end                                  // Error: different borrow roots
```

Owner state and cleanup obligations inside match arms are still checked. The
restriction concerns the resulting ownership-bearing expression, not ordinary
mutation or cleanup performed within an arm.

Arrays are initialized by their expected-type aggregate literal:

```seawitch
fixed: Array<Int32, 4> = [1, 2, 3, 4]
```

There is no general list or dictionary literal in v1.

## Indexing and bounds

The proposed indexing expression is postfix. It supports an assignment place
for a mutable array binding and for a live owning or borrowed list reference:

```seawitch
mut values: Array<Int32, 2> = [10, 20]
first: Int32 = values[0]
values[1] = 30
```

Indexing requires an integer index. An unsigned index greater than or equal to
the collection length traps. A signed negative index also traps, as does a
non-negative signed index greater than or equal to the collection length.

The direct index operation is a place expression. In a read/value context it
produces the collection element according to the element rules below; in an
assignment context it replaces the selected slot when the receiver is a
mutable array place or a list reference. It never returns a nullable value and
never silently clamps an index.

For an inline element, a read copies the value. For a `String` element, a read
returns a non-owning reference to the immutable combined String object owned by
the collection; it does not allocate or copy that object.
Assignment to a String slot prepares the new owned object before destroying the old one.
The same copy-before-destroy rule applies to `set` and dictionary replacement.
An indexed place may expose members of an inline object, ADT, or pointer value
under the existing place and pointer rules; the view restrictions still apply
when the source is a `View`.

The following operations are proposed for all contiguous collections:

```seawitch
count: UInt64 = values.length()
last: Int32 = values.at(values.length() - 1)
part: View<Int32> = values.slice(0, 1)
```

`at` has the same bounds behavior as indexing. `slice(start, end)` requires
`0 <= start <= end <= length` and traps otherwise. The returned view is
non-owning.

When the checker knows both bounds and the source length, an invalid index or
slice is a compile-time error at the earliest provable phase. This includes
constant Array indices and slices and constant rune indices or slices of a
String literal. Dynamic Array, List, View, and String bounds use the runtime
traps below.

The operation contracts for `Array`, `View`, and `List` are:

```text
length() -> UInt64
is_empty() -> Bool
at(index) -> T
[index] -> read place; writable for a mutable Array or any live List reference
slice(start, end) -> View<T>
```

For `List<T>`, `slice` is available only when `T` is an inline element, because
the result is `View<T>`. `List<String>.slice(...)` is rejected in v1; callers
must borrow an individual String with `at` or indexing, explicitly copy it with
`to_string(h)`, or remove it with `pop`.

`Dict` does not support the index suffix in v1. Dictionary access uses the
named `get`, `contains`, `insert`, and `remove` operations only.

The index and slice arguments may be any integer type. The built-in operation
normalizes them to the non-negative `UInt64` range with a checked conversion;
negative values and unrepresentable values trap. No ordinary implicit numeric
conversion is added.

## List operations

List methods operate on the heap object referenced by the handle. Mutating
methods therefore require a live owning or borrowed list, but not a `mut`
handle binding:

```seawitch
values.push(10)
values.push(20)
last: Int32 = values.pop()
values.set(0, 11)
values.clear()
```

`push` may grow or reallocate. `pop` returns the removed final element and
traps when the list is empty. `set` and `at` trap when their index is outside
the current length. `clear` destroys all elements, leaves the list valid and
empty, and retains its allocator handle and capacity.

`push` and `set` prepare a collection-owned copy of the incoming value before
changing the list. For inline elements this is an ordinary value copy. For a
`String` this makes one combined header-and-data allocation through the list's
allocator and stores its pointer in the slot. The source expression remains
valid after `push` or `set`.

`at` and a read of `[index]` copy an inline element. For `String`, they return a
source-tied immutable borrow of the existing nested String object without
allocation. An independent owner requires an explicit `to_string(h)` copy.
`pop` moves the selected value out of the list: an inline value is returned
directly, while a `String` element's owning pointer is removed from the slot and
becomes the returned affine String owner without allocation. The returned
owner is the caller's cleanup obligation and must be freed with a matching
`Heap`. The vacated slot is cleared without freeing the moved String object,
and the length decreases.
A specialization requiring an unavailable copy, move, or nested cleanup
operation is rejected before generation.

Growth and element replacement use a prepare/commit sequence. Checked capacity
arithmetic, allocation, hashing, relocation preparation, and creation of any
new nested String object happen before commit. Failure during preparation
destroys only temporary regions and temporary String objects; the original
collection remains unchanged.

Commit begins only after every fallible preparation step has succeeded. Pointer
and scalar writes, slot-state changes, and destruction through the already
validated retained allocator are infallible internal operations. No operation
may report a recoverable failure after commit begins. A runtime corruption or
deferred-cleanup trap remains unrecoverable under RFC 0026 and does not promise
rollback. No partially initialized element is observable.

`List<T>` preserves element order. A view derived from a list remains valid
across `set` and observes the changed element. `push`, `pop`, `clear`, and
`free` structurally change or invalidate the backing storage; the checker
must reject those operations while a derived view is live.

A String borrowed from a list remains valid across `push`, because growth moves
only the contiguous pointer slots and does not move or destroy existing nested
String objects. While a bound String borrow is live, any `set`, indexed
replacement, `pop`, `clear`, `free`, or owner reassignment for its source list
is rejected conservatively because it could destroy the borrowed object. The
checker does not attempt to prove that a particular index differs. Passing the
source list to a user function is also rejected while such a borrow is live
because every v1 List parameter permits mutation.

The same invalidation set applies to a `View<Byte>` or `RuneCursor` derived from
that borrowed String. Such a byte view is physically rooted in the stable
nested String object, not in the List's pointer-slot region, so `push` remains
valid. This is the specific exception to the ordinary List-element View rule.

`clear` destroys list elements from the final element toward the first, frees
their nested combined String objects, leaves the list live and empty, and retains its
allocator and capacity. A dictionary frees every active key and value exactly
once; bucket destruction order is unspecified.

`free(h)` frees every stored element, the backing region, and the heap header
through the matching allocator identity, then invalidates the owning list
binding. Using or freeing the owner again is an error. Calling `free` through a
borrowed parameter is rejected.

The list method contracts are:

```text
List<T>.new(heap)      -> List<T>
push(value: T)         -> no value; mutates referenced collection
pop()                  -> T; mutates referenced collection
set(index, value: T)   -> no value; mutates referenced collection
clear()                -> no value; mutates referenced collection
free(heap)             -> no value; requires and consumes an owning local
```

At a user-function boundary, a `List<T>` or `Dict<K,V>` parameter borrows the
caller's collection reference. The caller remains the owner and remains
responsible for its eventual cleanup:

```seawitch
fun append_default(values: List<Int32>)
    values.push(0)             // mutates the caller's heap object
end

h: Heap = Heap.new()
values: List<Int32> = List<Int32>.new(h)
defer values.free(h)
append_default(values)
```

The caller may continue using `values` after the call. A collection may be
passed while its deferred cleanup is active because the call only borrows it.
The callee cannot call `free`, register `defer values.free(h)`, return the
borrow as an owning result, or retain it after the call. A collection parameter
has no cleanup obligation. A `View<T>` parameter remains a read-only borrow.

The same owning handle may be supplied to more than one parameter of one call.
Those parameters alias the same collection. This is defined because each
method operation completes before the next source operation begins and the
callee receives no raw element pointers. Any call receiving a list invalidates
previously derived views conservatively, whether or not that execution
actually grows or clears the list.

A user call may not receive both a collection and a String, byte view, or rune
cursor derived from that same collection. The callee could otherwise mutate or
destroy the nested String while another parameter still refers to it:

```seawitch
inspect(names.at(0), names) // Error: derived borrow aliases mutable collection
```

## Dictionary operations

The initial dictionary API is:

```seawitch
scores.insert("alice", 10)
score: Int32 = scores.get("alice")
present: Bool = scores.contains("alice")
removed: Int32 = scores.remove("alice")
```

`insert` replaces the value for an existing key. `get` traps when the key is
absent in the first version; a later result type may add `try_get`. `contains`
does not expose a value reference. `remove` returns the removed value and traps
when the key is absent. A string literal used with `Dict<Strand, V>` is checked
using the expected `Strand` type and therefore retains Strand's literal-only
construction restrictions.

`free(h)` frees all stored keys, all stored values, the bucket region, and the
heap header through the matching allocator identity, then invalidates the
owning dictionary binding. Using or freeing it again is an error, and calling
`free` through a borrowed parameter is rejected. A dictionary stores its own
key and value copies; later rebinding of the source expressions cannot change
an entry.

`insert` prepares collection-owned copies of the key and value before changing
the table. Keys are copied as inline values. An inline value is copied directly;
a `String` value creates one combined String allocation through the dictionary's
allocator and stores its pointer in the bucket. If an equal key already exists,
the existing key remains and its value is replaced only after the new value is
ready. `get` copies an inline
value. For `String`, it returns a source-tied immutable borrow of the existing
nested String object without allocation; an independent owner requires
`to_string(h)`.
`remove` moves the stored value out, transferring its owning String pointer to
the returned value, destroys the entry, and then returns it. Any returned
owning String is the caller's cleanup obligation and must be freed with a
matching `Heap`.
Allocation, rehash preparation, or value-preparation failure leaves the table
unchanged, including all old key and value payloads. Hashing `Int32` and
`Strand` is an infallible computation; it cannot fail after commit begins.

While a bound String returned by `get` is live, `insert`, `remove`, `free`,
owner reassignment, and passing the source dictionary to a user function are
rejected conservatively because they could replace or destroy the borrowed
object. `get` and `contains` remain valid. The checker does not attempt to
prove that a particular key differs from the borrowed entry.

A `View<Byte>` or `RuneCursor` derived from that String borrow has the same
root and invalidation restrictions. A user call cannot receive the dictionary
and any such derived value in the same call.

Dictionary equality and ordering are deferred. Hash and equality must agree:
equal keys must produce equal hashes. The v1 key set is only `Int32` and
immutable `Strand`, so no mutable key can invalidate its bucket position while
stored.

The dictionary method contracts are:

```text
Dict<Int32, V>.new(heap)       -> Dict<Int32, V>
Dict<Strand, V>.new(heap)      -> Dict<Strand, V>
insert(key: K, value: V)       -> no value; mutates referenced collection
get(key: K)                    -> V; read-only receiver
contains(key: K)               -> Bool; read-only receiver
remove(key: K)                 -> V; mutates referenced collection
free(heap)                     -> no value; requires and consumes an owning local
```

## Mutability and element access

Array mutation is a property of the receiver place. List and dictionary
mutation is a property of their referenced heap object:

```seawitch
fixed: Array<Int32, 2> = [1, 2]
fixed[0] = 3                    // Error: fixed is not mutable

mut writable: Array<Int32, 2> = [1, 2]
writable[0] = 3                 // Valid

h: Heap = Heap.new()
values: List<Int32> = List<Int32>.new(h)
defer values.free(h)
values.push(3)                  // Valid: binding stays fixed; pointee changes
```

A fixed list or dictionary binding cannot be reassigned, but its referenced
collection may be mutated. `View<T>` is always read-only and cannot be used as
an assignment target. Array elements can be changed only through a writable
array place.

## Equality and copying

`Array<T,N>`, `View<T>`, and `List<T>` use RFC 0024's `==` and `!=` sequence
semantics when their element type is equality-comparable. Equality compares
length and then elements in order; it never compares generated C storage,
capacity, allocator identity, or backing addresses. Dictionaries do not support
equality in v1.

`Array<T,N>` has ordinary value copying because its element class is inline.
`View<T>` copies only its non-owning header and never its elements. Owning
`List<T>` and `Dict<K,V>` handles are never copied or assignment-transferred.
Passing one as an argument creates a call-duration borrow represented by the
same C pointer; it does not create another owner. Returning a locally owned
collection is the sole terminal ownership handoff.

The compiler derives the following internal operations for each concrete
`CollectionElement<T>` specialization:

1. inline copy for inline elements;
2. combined String-object allocation/copy for a direct `String` element;
3. move-out for `pop` and `remove`;
4. slot destruction, including nested String-object destruction; and
5. equality when RFC 0024 permits it.

These are compiler-internal operations, not user-visible capabilities or
traits. `List<List<Int32>>`, `List<View<Int32>>`, and collections containing an
object or ADT with one of those values are rejected in v1. RFC 0019 checks the
derived operations after specialization and must reject a missing operation
before C generation.

## C23 lowering

- `Array<T, N>` lowers to a generated wrapper struct containing an inline C
  array of the substituted concrete element type. The wrapper is mandatory
  because Seawitch arrays support whole-value assignment, parameter passing,
  and return, while a bare C array does not.
- `View<T>` lowers to a generated read-only pointer-plus-count struct and never
  exposes unchecked pointer arithmetic to source code.
- `String` lowers to `const struct sw_string *`; runtime objects use the
  `sw_string_storage` allocation defined above and literals use fixed-size
  read-only storage objects with the same header.
- `List<T>` lowers to a pointer to a concrete heap-allocated header containing
  data, length, capacity, and allocator identity, plus monomorphized helpers.
- `Dict<K,V>` lowers to a pointer to a concrete heap-allocated header and
  bucket helpers after RFC 0019 monomorphization. Only `Int32` and `Strand` key
  helpers are emitted in v1.
- Every access checks bounds before forming a C element lvalue.
- Reallocation checks capacity and uses the retained allocator helper;
  allocation failure produces the defined runtime allocation trap from RFC
  0026.
- Dictionary access never reads an inactive or uninitialized bucket payload.
- `free` destroys stored elements, including nested String objects, then frees
  both backing storage and the collection header through the matching allocator
  helper; it never emits unchecked C `free`.

The v1 C layouts are:

```c
struct sw_array_T_N {
    T data[N];
};

struct sw_view_T {
    const T *data;
    uint64_t length;
};

struct sw_list_T {
    T *data;
    uint64_t length;
    uint64_t capacity;
    sw_heap_id allocator;
};

struct sw_dict_K_V {
    sw_bucket_K_V *buckets;
    uint64_t length;
    uint64_t capacity;
    sw_heap_id allocator;
};

/* Source List<T> and Dict<K,V> values lower as pointers to these headers. */
struct sw_list_T *values;
struct sw_dict_K_V *scores;
```

For `List<String>` and `Dict<K,String>`, the concrete element or bucket slot
contains a `const sw_string *`. Each referenced String header and its immutable
UTF-8 bytes occupy one combined nested allocation owned by the containing
collection. Copy-in creates a new combined object, a non-removing read borrows
the existing object, move-out transfers the pointer, and destruction frees the
object. The collection's primary element or bucket region
remains contiguous because it contains contiguous pointer slots.

When collection growth or rehashing relocates slots containing String handles,
the generated helper transfers each pointer to the new primary region and
clears the old slot. It does not clone or free the String object during that
internal relocation. Failure before commit leaves all owning pointers in the
original region.

An empty `View`, `List`, or `Dict` has length zero. Empty views may use a null
data pointer. Empty lists and dictionaries still have an allocated header, but
use null data/bucket pointers and zero capacity. No primary-storage pointer is
dereferenced in an empty value. All capacity growth uses checked addition and
multiplication before allocation.

Generated names must include concrete type identities and must not collide
between `List<Int32>` and `List<String>`. Owning locals and borrowed parameters
use the same C pointer representation; their ownership distinction exists only
in checked compiler state. Generated helpers receive header pointers. The
implementation may add a runtime validity marker while the header is live, but
source-level checks must prevent all use after the header is freed.

## Diagnostics and fail-closed behavior

Representative compile-time diagnostics are:

```text
[Type Error] Array length must be a positive integer literal
[Type Error] collection index requires an integer type
[Type Error] constant array index is outside the array bounds
[Type Error] constant collection slice bounds are invalid
[Type Error] dictionary key type must be Int32 or Strand
[Type Error] array receiver is not mutable
[Type Error] collection element type is incomplete or has no finite layout
[Type Error] collection value is non-copyable
[Type Error] collection must be freed on every exit path
[Type Error] collection owner requires a constructor or owning function result
[Type Error] live collection owner cannot be replaced before it is freed
[Type Error] borrowed collection cannot be freed, returned as an owner, stored, or retained
[Type Error] collection cannot be used after it is freed
[Type Error] collection view outlives or invalidates its source
[Type Error] view element type is not inline-manageable
[Type Error] collection element contains an owning or borrowed collection
[Type Error] collection cannot be stored in an object or ADT member
[Type Error] collection owner cannot be reassigned while deferred cleanup is active
[Type Error] returned collection is not a live owning value
[Type Error] owning collection result must be bound, reassigned, or returned
[Type Error] collection index is not supported for dictionaries
[Type Error] array size is not representable for the target
[Type Error] owning String handle cannot be shallow-copied
[Type Error] static String storage cannot be freed
[Type Error] String view outlives or invalidates its source
[Type Error] owning String result must directly initialize a cleanup obligation
[Type Error] borrowed String outlives or invalidates its source collection
[Type Error] borrowed String cannot be freed, returned, stored, or captured by defer
[Type Error] borrowed String binding cannot be reassigned
[Type Error] String provenance disagrees at control-flow merge
[Type Error] view requires stable source storage
[Type Error] call aliases a collection with its derived String borrow
[Type Error] collection owner states disagree at control-flow merge
[Type Error] collection-valued match expression is not supported in v1
[Type Error] owning collection handle cannot be addressed or stored indirectly
[Type Error] list and its derived view cannot be passed to the same call
```

Representative runtime diagnostics are:

```text
[Runtime Error] collection index is outside the collection bounds
[Runtime Error] collection slice bounds are invalid
[Runtime Error] cannot pop an empty List
[Runtime Error] dictionary key is absent
[Runtime Error] collection capacity is not representable
[Runtime Error] collection allocation failed
[Runtime Error] collection deallocation used the wrong allocator
[Runtime Error] collection was already freed
```

A constant out-of-bounds index or slice into a fixed Array is rejected by the
checker at the earliest provable phase. The same applies to a constant rune
index or slice of a String literal. Dynamic Array indices and all dynamic
List, View, and String bounds are runtime checks. `Dict.get` and `Dict.remove`
use the same absent-key runtime diagnostic. Allocation and deallocation
diagnostics remain coordinated with RFC 0026; these collection-specific
spellings identify the operation that failed without changing its allocator
semantics.

The checker owns type arguments, lengths, exact dictionary key restrictions,
view and derived-cursor lifetimes, owner-versus-borrow context, live/freed states, cleanup
obligations, control-flow merge invariants, allowed owner storage positions,
String storage provenance for the supported forms, element copy/move/free
specialization, mutability, and operation signatures. The generator must reject
unsupported collection nodes as `Unknown Error`; it must not fall back to raw
pointer arithmetic or omit a collection operation.

## String reads from collections

`List<String>.at`, String list indexing, and `Dict<K,String>.get` return an
immutable String borrow tied to the source collection. Borrowing does not
allocate, copy, or transfer the nested String object. The binding has ordinary
source type `String`; borrow provenance is compiler-tracked and does not add a
second public reference or view type:

```seawitch
name: String = names.at(0)          // borrowed from names
copy: String = name.to_string(h)    // independent affine owner
defer copy.free(h)
```

A bound collection-derived borrow uses the same lexical lifetime policy as a
`View<T>` binding. It remains live until the end of its lexical scope, cannot
outlive or be returned independently of its source collection, and cannot be
stored, freed, captured by `defer`, or used to initialize an owning binding.
Copying the borrowed handle into another local creates another borrow tied to
the same root and does not create an owner. Passing it to a String parameter
creates a call-duration borrow nested within its existing lifetime. Calling
`bytes()` or `slice()` on the borrowed String creates a `View<Byte>` tied to
the same nested String object and root collection. Calling `rune_cursor()`
creates an equivalent source-tied `RuneCursor`. Neither derived value can
outlive the String borrow or permit an operation that could destroy its nested
String object.

An unbound borrow used directly as an argument lives through the complete call
expression. Compiler-owned `push`, `set`, and `insert` may consume a directly
nested borrow rooted in the same collection because they prepare their new
collection-owned String copy before committing any replacement or structural
change:

```seawitch
print(names.at(0))
names.push(names.at(0))
names.set(0, names.at(0))
scores.insert("copy", scores.get("original"))
```

This narrow prepare-before-commit exception does not permit an already bound
borrow to survive an invalidating operation. It also does not apply to user
function calls, whose mutation behavior is conservatively unknown.

Outside that exact exception, the complete call expression may not contain a
sibling operation that can invalidate the temporary borrow's root. Seawitch
inherits C23's unspecified relative argument-evaluation order, so the checker
must reject the conflict rather than select an order. A user call also cannot
receive both the root collection and a directly derived temporary borrow:

```seawitch
inspect(names.at(0), names)                       // Error
use(names.at(0), operation_that_mutates(names))   // Error
```

An unbound collection-derived String borrow exists through the complete call
expression and then ends. It cannot be returned, stored, or captured by the
callee.

`pop` and `remove` are different: they remove the nested object and return its
affine owner without allocation. Such a result must directly initialize a
local cleanup obligation, replace a freed `mut` owner, or be returned from a
String-returning function. It cannot be discarded, passed directly to a
borrowed parameter, or nested in another expression:

```seawitch
removed: String = names.pop()
defer removed.free(h)

print(names.pop())                  // Error: owning result has no cleanup owner
```

`View<T>` remains a contiguous-range type. A borrowed String is not represented
as `View<Byte>` because it retains String semantics, and it is not represented
as `View<String>` because a single List or Dict element is not a range. This
RFC continues to reject `View<String>` in v1.

## Readiness decisions still open

Two language choices remain open and keep this RFC in Draft:

1. RFC 0024 is still Draft even though this RFC depends on its sequence
   equality rules. The project must either finish RFC 0024 before implementing
   this RFC or remove Array, View, and List equality from this RFC's v1 scope.
2. The inline element class currently admits only `Nil`-containing structural
   unions. The project must decide whether every structural union whose
   normalized members are all inline elements is permitted:

```seawitch
values: List<Int32 | Bool> = List<Int32 | Bool>.new(h)
```

The recommended rule is to permit this specialization because its tagged union
is inline, copyable, and requires no cleanup. Restricting it to nullable unions
would need an explicit language rationale.

## Deferred

- Explicit cloning, assignment transfer, and owning parameters for `List<T>`
  and `Dict<K,V>`; v1 permits only terminal-return ownership handoff.
- Read-only borrowed dictionary references and a general const-reference form
  for heap-backed collections.
- Automatic destruction and owner types beyond the explicit `free`/`defer`
  model in RFC 0026.
- General const generics and zero-length arrays.
- Iterators, `for` loops, comprehensions, and lazy views.
- Sets, ordered maps, queues, deques, and specialized numeric arrays.
- Safe result-returning lookup APIs such as `try_get` and `try_at`.
- Collection serialization, collection-wide hashing, ordering, and C ABI
  contracts.
- Concurrent collection access and synchronization.
- Implicit conversions between text, byte, view, and collection values.

## Acceptance criteria

Implementation is complete when focused end-to-end tests prove that:

1. `Array<T, N>`, `View<T>`, `List<T>`, and `Dict<K,V>` resolve with stable
   canonical identities;
2. array lengths and element types are checked before generation;
3. indexing, `at`, and slicing enforce the stated bounds and never emit an
   unchecked C access;
4. array writes require a mutable array place, while list and dictionary
   mutation writes through a live heap reference without requiring a `mut`
   handle binding;
5. list mutation preserves order and dictionary insertion replaces equal keys;
6. dictionaries accept exactly `Int32` and `Strand` keys and reject every
   other concrete key type at specialization time;
7. empty lists and views are valid values;
8. `List<T>` and `Dict<K,V>` use pointer-sized local handles, heap-allocated
   headers, and separate contiguous heap-backed primary storage, with
   `new(heap)` and exactly-once `free(heap)` cleanup of both allocations;
9. owning collection handles cannot be copied or assignment-transferred,
   function arguments create call-duration borrows of the same heap object,
   and terminal returns hand ownership to a caller without a surviving moved
   binding;
10. views use lexical source-tied lifetimes; invalidating operations and calls
   receiving both a list and its derived view are rejected while the view is
   in scope, while element replacement remains observable through the view;
11. failed pre-commit growth, rehash, and element preparation destroy
   temporaries and leave the original collection unchanged, and no fallible
   operation occurs after commit begins;
12. collection specializations generate concrete C layouts and helpers exactly
   once, Arrays use value-semantic wrapper structs, and runtime Strings use the
   declared header-plus-flexible-storage C23 layout;
13. allocation and invalid-state failures have a defined path;
14. collection values do not silently convert to strings, pointers, or other
   collection forms; and
15. live and freed owner states, exact branch/match/loop merge invariants,
   borrowed-parameter restrictions, direct constructor initialization,
   terminal ownership returns, reassignment, and deferred cleanup are checked;
16. the allowed inline and direct-String element classes are checked
   recursively, including nested combined String-object cleanup and rejection of
   nested collections and views;
17. collection views are source-tied through branches, loops, calls, slices,
   reassignment, and invalidating operations;
18. sequence equality uses the RFC 0024 operator rules and dictionary equality
   is rejected in v1; and
19. every new syntax node, collection type, built-in operation, and generator
   case is handled explicitly under the fail-closed architecture;
20. runtime-created Strings use one combined header-and-data allocation,
   literals use allocation-free static objects, and source String values lower
   as pointer-sized immutable references; and
21. `String.slice` returns an allocation-free rune-bounded `View<Byte>`, while
   `List<String>` and `Dict<K,String>` store contiguous owning String-pointer
   slots with copy-in, borrowed read, move-out, relocation, and destruction
   behavior matching this RFC; and
22. compile-time ownership/type failures and runtime bounds, lookup,
   allocation, capacity, and deallocation traps use the diagnostic boundary
   defined by this RFC; and
23. non-removing String collection reads create source-tied lexical borrows
   without allocation, invalidating source operations are rejected, direct
   prepare-before-commit copy-in expressions remain valid, and String-valued
   `pop` and `remove` results carry an affine cleanup obligation;
24. String provenance is fixed at initialization, exact at control-flow
   merges, and rejects owning match results, mixed provenance, different borrow
   roots, and borrowed-local reassignment;
25. Views require stable roots, reject temporary Array and unbound owning
   sources, and give unbound argument Views only a full-call-expression
   lifetime;
26. byte views and rune cursors derived from collection-borrowed Strings use
   the nested String object's invalidation set, and calls cannot alias them
   with their mutable root collection; and
27. transparent aliases of concrete List and Dict specializations construct
   the canonical built-in type, and every statically provable invalid index or
   slice is rejected before generation.
