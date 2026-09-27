# RFC 0226: Unified Lists with Inline Capacity

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented.
- Created: 2026-09-21
- Updated: 2026-09-27
- Replaces the earlier RFC 0226 proposal to add a third `BoundedList<T, N>` type
- Depends on: the current `List<T>`, `Slice<T>`, and `String<N>` contracts in `docs/reference.md`
- Coordinates with: the current centralized compiler-diagnostic architecture,
  the reference's `String`/`String<N>` storage split, and its existing pointer,
  Slice, traversal-invalidation, and local alias rules

## Summary

Hexal will have one sequence family with two storage forms:

```text
List<T>       allocated, runtime length, growable
List<T, N>    inline, runtime length from 0 through capacity N
```

`Array<T, N>` is removed. Existing fixed arrays migrate to `List<T, N>`.
The second argument is a capacity, not a statically guaranteed length.

This follows the text family:

```text
String        allocated, variable-size text
String<N>     inline text holding at most N bytes
```

It avoids a third sequence name, keeps one method vocabulary, and makes the
storage choice visible in the type. The cost is deliberate: Hexal no longer
has a type that proves an exact sequence length. Code that requires exactly N
elements checks the logical length statically where the checker can prove it
and dynamically otherwise.

## Motivation

The current sequence surface divides two independent properties across two
names:

```text
Array<T, N>   inline and exactly N elements
List<T>       allocated and growable
```

That leaves no allocation-free sequence which starts empty and fills up to a
known bound. Adding `BoundedList<T, N>` would solve that absence by adding a
third collection concept. The preferred design instead makes capacity the
optional second parameter of the existing `List` family:

```hexal
let mut packet: List<Byte, 1500> = []
packet.push(0x45)
packet.push(0x00)
```

The type says both important facts: this is a list, and its storage is bounded
and inline. The user does not choose among Array, List, and BoundedList for the
same indexing, slicing, traversal, and mutation operations.

### Verified migration inventory

The 2026-09-26 baseline contains 77 Array-related matches in 32 non-test Go
files, 191 in 49 Go test files, one snippet-catalog match, and 17 matches in
`GRAMMAR.ebnf`, `docs/reference.md`, and `docs/status.md`. The search includes
`Array<`, `TypeArray`, `ArrayType`, `ArrayLiteral`, and `ArrayExpression`; the
literal node remains useful but is renamed to describe a bracket/list literal.
These figures are execution inventory, not permanent acceptance counts. Phase
5 repeats and classifies the search because concurrent work may change them.

## Goals

1. Replace `Array<T, N>` with `List<T, N>`.
2. Keep `List<T>` unchanged as the allocated, growable form.
3. Give both forms the same logical sequence operations wherever ownership and
   representation permit.
4. Permit an inline list to start empty or partially initialized without
   exposing an uninitialized Hexal value.
5. Preserve left-to-right evaluation, bounds checks, deterministic generated
   C, and the current shallow element-copy rules.
6. Keep allocation explicit: `List<T, N>` never allocates and `List<T>` still
   receives a Heap.
7. Remove Array-specific compiler and generated-runtime machinery rather than
   retaining aliases or compatibility paths.

## Non-goals

- A type-level proof that a sequence contains exactly N elements.
- Default values or source-visible zero initialization for arbitrary types.
- Ownership, borrowing, lifetimes, affine moves, or automatic element cleanup.
- Implicit conversion between inline capacities or between inline and
  allocated lists.
- A raw C array ABI. Supported foreign declarations do not currently expose C
  array declarators; pointer-plus-length remains the ordinary C boundary.
- Changing `Slice<T>` or `Slice<mut T>` representation.
- Adding a source-visible `capacity()` method. N is a type argument; the
  existing List surface does not expose allocation capacity either.
- Implicit widening between `List<T, N>` capacities. A later proposal may add
  an explicit conversion if real programs demonstrate the need.

## Surface syntax

### Type forms

```text
List<T>       one type argument
List<T, N>    one type argument and one positive integer capacity
```

N follows the existing compiler-owned capacity syntax used by `String<N>`:

- it is a positive decimal integer literal;
- digit separators are accepted and do not affect type identity;
- a name, expression, or generic value parameter cannot stand for N;
- the inline List's estimated complete storage must be strictly smaller than
  the compiler-owned 64 KiB budget;
- N is part of canonical type identity.

Therefore:

```hexal
List<Int32, 4>      -- valid
List<Int32, 0>      -- error
List<Int32, -1>     -- syntax error: '-' cannot begin a capacity
List<Int32, N>      -- error even inside fun f<N>
List<Byte, 65_000>  -- valid under the estimated-storage budget
List<Byte, 65_536>  -- error
```

`List<T, 4>` and `List<T, 8>` are distinct types. There is no implicit
widening, narrowing, assignment, argument conversion, or equality comparison
between them.

The normative grammar already permits compiler-owned capacity arguments in a
`TypeArgumentList`. This RFC removes `ArrayType` from
`PrimaryTypeExpression`; it does not add a second overlapping List production:

```ebnf
PrimaryTypeExpression = NamedType | GenericType
                          | PointerType | SliceType | FunctionTypeExpression
                          | "(" TypeExpression ")" .
```

The parser routes `List<...>` through the existing mixed type/capacity argument
parser already used by compiler-owned forms. The checker then accepts only the
one-type and type-plus-positive-decimal shapes. A leading minus cannot begin a
type argument and is a parser-owned syntax error. Hexadecimal, binary, octal,
floating, and zero literals reach the checker; only positive decimal integers
are capacities.

A type parameter may still occupy T:

```hexal
fun first<T>(values: List<T, 4>): T do
    return values[0]
end
```

### Internal type identities

One source name does not mean one internal representation. The compiler keeps
two constructor identities selected by written arity:

```text
TypeList        List<T>       handle, shallow copy, managed, free available
TypeInlineList  List<T, N>    value, full copy, unmanaged, no free method
```

`TypeInlineList` replaces `TypeArray`; it is not a second source-visible type.
The constructor registry permits one source name to have multiple records only
when their parameter shapes differ, and resolves by `(source name, parameter
shape)`. Duplicate shapes remain an integrity error. Bare construction asks
whether the selected record is constructible rather than accepting the first
record with that name.

The method registry likewise owns separate records. The inline identity owns
`length`, `push`, `pop`, `clear`, `slice`, and `mut_slice`; the allocated
identity keeps those methods plus `free`. Inline `push` records no allocation.
This makes representation, copy, managed-state, allocation, and `free`
availability consequences of the selected identity instead of scattered
checker exceptions.

### Estimated inline-storage budget

The budget is a compiler resource guardrail, not a C ABI calculation and not a
proof that a complete function frame fits on a Task stack. Several inline
values, recursion, C temporaries, and ordinary locals can still exhaust a
finite stack. Its purpose is to reject one obviously excessive inline value
without making Hexal reproduce the C compiler's exact padding and alignment
rules.

The single policy owner is:

```go
const MaxInlineListEstimatedBytes uint64 = 64 * 1024
```

in `compiler/config`. It is not a `Project` setting or source-language option.
Changing the compiler policy later changes this one constant and its boundary
tests. Its CARE rationale records that 64 KiB leaves substantial headroom
inside the default 1 MiB Task stack; it is a conservative resource ceiling,
not a measured optimum.

The checker computes one target-independent deterministic
`estimatedStorageBytes(T)`. Width categories use the largest width among the
qualified target profiles rather than the selected target, so the same source
does not cross the 64 KiB boundary merely because it is cross-compiled:

- integer, floating, Boolean, Rune, EoS, and Size values use their largest
  qualified-profile width, with a minimum of one byte;
- Ptr and function values use one target pointer width;
- opaque or handle-shaped compiler-owned values, including heap String, Heap,
  allocated List, Dict, Stash, Pool, Task, Channel, Mutex, and File, use four
  target pointer widths; this deliberately overestimates small handles rather
  than maintaining an ABI-layout replica;
- Slice uses two pointer-sized words;
- `String<N>` uses one Size plus N bytes;
- `List<T, N>` uses two Size values plus `N * estimatedStorageBytes(T)`;
- a struct uses the checked sum of its member estimates plus 16 bytes per
  member as a padding allowance, rounded up to 16 bytes;
- an ADT or general structural union, including compiler-owned values such as
  Error whose checked representation is a value, uses 16 bytes for
  tag/alignment plus its largest payload estimate, rounded up to 16 bytes;
- a complete foreign record uses its imported member types under the same
  struct rule; an incomplete foreign record is already ineligible by value.

The current target matrix is entirely 64-bit, so pointer and Size estimates
are eight bytes. A future narrower profile does not reduce the estimate; a
future wider profile updates the shared maximum-width fact and revalidates the
boundary tests.

Direct by-value recursion is already invalid, so the estimator terminates.
Every addition, multiplication, and rounding operation is checked. Arithmetic
overflow is the same diagnostic as exceeding the budget.

The categories above are closed for this RFC. Adding a new representation kind
requires adding its estimate and a focused estimator test; it must not fall
through to zero or host `unsafe.Sizeof`.

An open generic containing `List<T, N>` defers this one estimate until T is
substituted, exactly as dependent layout checks are deferred today. Each
concrete specialization must satisfy the budget; rejecting one specialization
does not make other element substitutions unavailable.

An inline List is accepted only when:

```text
2 * estimated Size width + N * estimatedStorageBytes(T) <
    MaxInlineListEstimatedBytes
```

Nested inline Lists naturally include their own length, version, and payload
estimate. The estimate intentionally may exceed or fall short of C `sizeof`;
generated C carries no redundant size assertion. Hexal continues to let the C
compiler own exact layout.

### Construction

An inline List has two construction forms.

The zero-argument canonical constructor creates an empty value:

```hexal
let mut values = List<Int32, 4>()
```

A contextual bracket literal creates a value whose logical length is its
element count:

```hexal
let empty: List<Int32, 4> = []
let pair: List<Int32, 4> = [10, 20]
let full: List<Int32, 4> = [10, 20, 30, 40]
```

The elements evaluate exactly once, left to right. A literal containing more
than N elements is a compile-time error. Unlike the former Array literal, it
need not contain exactly N elements and the empty literal is valid.

An uncontextual literal remains invalid because neither its element type nor
its capacity is complete:

```hexal
let values = [1, 2]  -- error: an inline List literal requires List<T, N> context
let empty = []       -- same error
```

`List<T, N>(a, b)` is not another construction spelling. Use `[a, b]` when
elements are present. This keeps one obvious literal form and avoids adding a
variadic compiler-known constructor.

The allocated constructor is unchanged:

```hexal
let values: List<Int32> = List<Int32>(heap)
```

`List<T>()`, `List<T, N>(heap)`, and `List<T, N>(values...)` are rejected with
diagnostics naming the correct constructor for that storage form.

## Operations

Both forms support:

```text
length() -> Size
[index: Integer] -> place<T>
slice(start: Integer, end: Integer) -> Slice<T>
mut_slice(start: Integer, end: Integer) -> Slice<mut T>
push(value: T) -> no value
pop() -> T
clear() -> no value
```

The allocated form additionally supports:

```text
free(heap: Heap) -> no value
```

Calling `free` on `List<T, N>` is a compile-time error because inline storage
owns no allocation.

For both forms:

- indexing traps unless `0 <= index < length`;
- slicing uses logical length, not inline capacity;
- `pop` traps when empty;
- `push` on `List<T, N>` traps when `length == N`;
- `push` on `List<T>` grows using its existing checked allocation path;
- `clear` changes length to zero without freeing element referents;
- element reads, writes, push, pop, copy, and discard use the existing shallow
  element semantics.

Every generated inline-list operation preserves `length <= N`. Only unsafe
foreign mutation of the generated struct can violate that invariant, and such
mutation remains outside the safe-language contract.

A constant inline-list index greater than or equal to N is rejected at compile
time. A smaller constant still needs a logical-length check unless the checker
can prove the constructed value has enough elements. This RFC does not require
new persisted flow facts solely to remove that runtime check. Inspecting a
direct literal at its consuming AST node is local syntax classification, not a
fact carried through bindings or control flow.

## Mutability and copying

`List<T>` is a handle. A fixed binding can mutate the allocation it names:

```hexal
let values: List<Int32> = List<Int32>(heap)
values.push(10)
```

`List<T, N>` is the inline value itself. Structural mutation and element
replacement require a writable place:

```hexal
let mut values: List<Int32, 4> = []
values.push(10)
values[0] = 20
```

The same operations through a fixed inline binding are rejected:

```hexal
let values: List<Int32, 4> = []
values.push(10)  -- error: inline List mutation requires a writable receiver
```

Assignment, argument passing, and returns copy the complete inline value.
Parameters are fixed under the existing language rule, so a function which
wants to mutate its local copy first assigns it to a mutable local:

```hexal
fun append_locally(values: List<Int32, 4>) do
    let mut local: List<Int32, 4> = values
    local.push(10)
end

let mut caller: List<Int32, 4> = []
append_locally(caller)
-- caller remains empty
```

This differs intentionally from copying `List<T>`, whose handles continue to
refer to the same allocated header.

Caller-visible inline mutation uses the existing explicit pointer model; this
RFC adds no reference parameter or implicit in-out convention:

```hexal
fun append(values: Ptr<mut List<Int32, 4>>, value: Int32) do
    (^values).push(value)
end

let mut caller: List<Int32, 4> = []
append(@caller, 10)
```

The explicit dereference produces the writable inline-List place. Passing an
inline List normally still passes an independent value copy.

## Traversal and slices

Inline and allocated Lists use the same structural-version rule:

- `push`, `pop`, and `clear` are structural mutations and increment version;
- replacing an existing element is not structural;
- structurally mutating the traversed List during `for` iteration traps;
- the check applies to both storage forms.

The inline version is necessary despite value-copy semantics because the
existing pointer model can mutate the same storage through an alias:

```hexal
let mut values: List<Int32, 4> = [1, 2]
let alias: Ptr<mut List<Int32, 4>> = @values
for value in values do
    (^alias).push(value)
end
```

Without a version check, the loop's storage changes behind the active
traversal. The check is not retained for copies: a copied inline List has its
own version and storage.

`slice` and `mut_slice` cover only the current logical prefix. An inline List's
storage does not relocate, but logical element lifetime still changes.
Therefore `push`, `pop`, `clear`, and whole-value reassignment invalidate every
existing Slice derived from that inline List. Element replacement does not.
As with every Slice, using an invalidated descriptor is the programmer's
responsibility; this RFC adds no lifetime metadata.

Copying an inline List produces independent storage. A Slice into the source
continues to name the source, never the copy.

Creating a Slice requires an addressable backing place. A temporary inline
List cannot outlive the call expression which produced it, so this is rejected:

```hexal
make_values().slice(0, 1)
```

The same rule applies to `mut_slice`; assigning the List to a local first makes
the backing place explicit.

## Equality, printing, and iteration

Two inline Lists are equality-comparable only when they have the same canonical
`List<T, N>` type and T supports equality. Equality compares logical length and
then the active elements in order. Inactive capacity never participates.

Different capacities are different types and do not compare:

```hexal
let a: List<Int32, 4> = [1, 2]
let b: List<Int32, 8> = [1, 2]
let same: Bool = a == b  -- type error
```

Printing and `for` iteration visit only `0 .. length`. They never inspect
inactive capacity. Ordering remains unavailable, matching the existing
collection contract.

## C23 representation

The allocated representation remains unchanged:

```c
typedef struct hex_list_Int32 {
    int32_t *data;
    size_t length;
    size_t capacity;
    size_t version;
} hex_list_Int32;
```

Each reachable inline specialization emits one value type:

```c
typedef struct hex_list_Int32_4 {
    size_t length;
    size_t version;
    int32_t data[4];
} hex_list_Int32_4;
```

The exact C name remains subject to the program-wide generated-name registry;
the examples show the family, not a bypass around collision handling.

### Inactive storage

`data[length .. N)` is private inactive storage, not a sequence of
source-visible T values. It is never indexed, traversed, sliced, printed,
compared, freed, or otherwise interpreted by generated code.

Every constructor and literal C-zero-initializes the complete struct before
storing active elements. C aggregate initialization recursively initializes
scalars, pointers, arrays, structs, and unions; it does not read indeterminate
storage. A partial literal therefore has a complete C object representation:

```c
const hex_list_Int32_4 values = {
    .length = 2,
    .version = 0,
    .data = {10, 20},
};
```

The omitted tail is initialized by C, but Hexal does not expose those values
or acquire a general default-value operation. After `pop` or `clear`, inactive
slots may retain former initialized C values; they remain unreachable through
the Hexal sequence contract. This matches allocated List's existing shallow
discard behavior and introduces no destructor work.

Push writes `data[length]` before publishing the incremented length. Pop reads
the final active element before decrementing length. Structural version changes
after the operation's element evaluation succeeds. No per-slot initialization
bitmap, heap allocation, or tagged optional wrapper is emitted.

Retaining bytes in a removed slot does not retain Hexal ownership. Elements
already follow shallow copy/discard semantics: `pop` returns the shallow value
to its caller, while `clear` discards it just as allocated List does today.
Inactive bytes are semantically unreachable and no destructor or collector
observes them. Zeroing removed slots would add work without closing a defined
memory-safety hole, so this RFC does not require it.

### Ownership and placement

`List<T, N>` follows the former Array element and placement eligibility:

- T must be a complete storable value;
- nested inline Lists are valid;
- an inline List may appear in bindings, aggregates, unions, other
  collections, parameters, results, Tasks, Channels, and allocation pointees
  wherever the former Array was valid;
- copying is by value and freeing the inline List does not exist;
- external-state elements remain shallow references.

Specializations whose element C spelling is program-wide live in
`hexal/list.h`. A specialization naming a module-owned element remains in the
owning module header after that element declaration. The existing dependency
ordering and generated-name registry remain authoritative.

## Exact-length consumers

Removing Array deliberately removes exact length from the type system.
Consumers which require exactly N elements use the following uniform rule:

1. If the checker sees a direct literal with the wrong logical length, it
   reports the error at compile time.
2. Otherwise generated code checks `length == N` immediately before the value
   is consumed and traps with an operation-specific Runtime Error on mismatch.
3. Capacity is never treated as proof of logical length.

### Numeric byte conversion

Fixed-width integers return a full inline List. In the signatures below W is
spec meta-notation for the concrete scalar width in bytes (`T.Bits / 8`), not a
source-language `sizeof` expression:

```text
T.to_le_bytes() -> List<Byte, W>
T.to_be_bytes() -> List<Byte, W>
```

For example, `Int32.to_le_bytes()` returns `List<Byte, 4>`.

The reverse operations keep their scalar result type:

```text
T.from_le_bytes(bytes: List<Byte, W>) -> T
T.from_be_bytes(bytes: List<Byte, W>) -> T
```

They trap with `[Runtime Error] byte list length does not match numeric width`
when a non-literal argument does not contain exactly the required number of
bytes. A direct literal of the wrong length is a Type Error.

### Network addresses

The built-in payloads become:

```text
IPv4 as bytes: List<Byte, 4>, port: UInt16 end
IPv6 as bytes: List<Byte, 16>, port: UInt16, scope: UInt32 end
```

Construction rejects a direct literal of the wrong length. A dynamic List is
checked by the built-in variant construction path and traps with
`[Runtime Error] network address byte length does not match address family`
before an invalid Address value is produced.

The built-in payload types are created through the same canonical inline-List
interner as source `List<Byte, 4>` and `List<Byte, 16>`. The separate
`builtinArrayType` path is deleted. This also resolves the current defect where
an ordinary source Array and the identically-spelled built-in Address Array can
have distinct canonical identities.

## C interoperability

`List<T, N>` is a Hexal-generated struct passed and returned by value. It is
not ABI-compatible with `T[N]`, because it also carries logical length and
version. `size_of`, `align_of`, address-of, and pointer use follow ordinary
complete generated structs.

The currently supported C-import subset already rejects C array declarators,
so removing `Array<T, N>` does not remove a supported direct array mapping.
Foreign pointer-plus-length APIs use `Slice<T>` or `Slice<mut T>`. A writable
inline List reaches such an API through `mut_slice`; a read-only call uses
`slice`. No implicit C bridge copies or allocates.

If direct raw C arrays become supported later, they require their own foreign
type or explicit bridge; they must not silently use `List<T, N>`.

## Diagnostics

The implementation adds or updates centralized compiler diagnostics with
descriptive identities. Required messages include:

```text
unknown type Array; use List<T, N> for inline bounded storage
List requires one element type and optionally one positive integer capacity
List capacity must be a positive integer literal
inline List estimated storage reaches the compiler limit of <limit> bytes
an inline List literal requires an expected List<T, N> destination type
List<T, N> literal has <count> elements but capacity is <N>
inline List mutation requires a writable receiver
inline List storage cannot be freed
List<T, N>() takes no arguments; use a contextual [value, ...] literal
List<T> construction requires exactly one Heap
```

Operation-specific existing diagnostics are renamed from Array to inline List
where their contract survives. No compatibility diagnostic accepts Array.
`<limit>` is formatted from `config.MaxInlineListEstimatedBytes`; the message
does not duplicate the numeric policy.

## Required sweep

This RFC changes the collection invariant and therefore removes the machinery
which existed only for Array:

- `parser.ArrayTypeExpression` and Array-specific type parsing;
- `types.ArrayInfo`, `Type.Array`, `ArrayType`, `builtinArrayType`,
  `TypeArray`, `PositionArrayElement`, and Array canonicality branches;
- checker Array resolution, exact-count literal rules, Array-only diagnostics,
  and Array-specific place handling;
- generator `generatedArrayState`, `arrays.go`, `array_component.go`,
  `packages/array.h`, array discovery, helper naming, component facts, and
  include ordering;
- `hex_array_*` generated symbols and test assertions;
- separate built-in network Array identities;
- Array-specific equality, printing, traversal, slice, pointer, module
  ownership, and layout branches where the inline-List branch replaces them;
- the old empty-array-literal rejection;
- comments that describe Array as the fixed inline collection;
- every active source fixture and snippet using `Array<T, N>`;
- every current `docs/status.md` entry which mentions Array: remove a resolved
  bug or rewrite an independently open item to the replacement List spelling.

Do not retain `Array` as an alias. A compatibility alias would preserve two
ways to spell the same concept and keep the removed parser/type/generator path
alive.

The sweep does not rewrite archived specifications. They are immutable
historical records.

## Reference and grammar synchronization

Implementation updates `GRAMMAR.ebnf` and `docs/reference.md` once behavior is
stable. The reference change must:

1. remove `Array<T, N>` from generics, collections, numeric byte conversion,
   networking, placement, C interoperability, and all type matrices;
2. define the one- and two-argument List forms, construction, mutability,
   copying, traversal, slicing, equality, and capacity rules;
3. state that `List<T, N>` capacity is not an exact-length proof;
4. state the exact-length consumer rule and runtime traps;
5. update Slice backing-store and invalidation language;
6. preserve `List<T>`'s Heap-passing and shallow element contracts;
7. remove the open Address Array identity bug from `docs/status.md` when its
   regression test passes.

## Validation

The following list is exhaustive.

### Syntax and type identity

- `List<Int32>` resolves to the existing allocated specialization.
- `List<Int32, 4>` resolves to an inline specialization distinct from
  `List<Int32>` and `List<Int32, 8>`.
- The source name `List` resolves by parameter shape to separate `TypeList`
  and `TypeInlineList` records; constructor-registry integrity rejects two
  records with the same source name and parameter shape.
- `List<T, 4>` resolves inside a generic declaration and specialization.
- An open `List<T, 4>` defers its storage estimate; a small concrete
  specialization succeeds and an over-budget specialization reports once at
  the requesting use.
- `List<Int32, 0>`, a negative capacity, a non-decimal literal, a name, an
  expression, and a generic parameter are rejected. Zero reaches the checker
  and reports the positive-capacity diagnostic; a leading minus is a
  parser-owned syntax error; other numeric forms, names, expressions, and
  generic parameters report the mixed-argument diagnostic.
- `List<Byte, 65_000>` is below the estimated budget;
  `List<Byte, 65_536>` is rejected.
- `List<Byte, 1_024>` and `List<Byte, 1024>` are one canonical type.
- Nested inline Lists and large structs include their recursive estimate;
  checked-estimate overflow and an estimate exactly equal to 64 KiB are
  rejected.
- Complete foreign records use the documented conservative member estimate,
  not host layout; incomplete foreign records remain ineligible.
- Focused estimator cases cover a scalar, pointer, Slice, allocated List,
  `String<N>`, nested inline List, object, ADT, structural union, Error, and a
  complete foreign record; no category silently estimates as zero.
- `Array<Int32, 4>` reports the migration diagnostic and never resolves.

### Construction and evaluation

- `List<Int32, 4>()`, contextual `[]`, `[1, 2]`, and a full four-element
  literal compile with lengths 0, 0, 2, and 4.
- A five-element literal for `List<Int32, 4>` is rejected.
- An uncontextual empty or nonempty bracket literal is rejected.
- An allocated `List<T>` destination does not contextualize `[]`; bracket
  literals construct only `List<T, N>`.
- Inline literal elements evaluate exactly once from left to right.
- `List<Int32, 4>(1)`, `List<Int32, 4>(heap)`, `List<Int32>()`, and
  `List<Int32>(heap, heap)` report the form-specific constructor diagnostic.
- The generated empty and partial C initializers zero-initialize the complete
  struct and never leave indeterminate tail storage.

### Mutation, copying, and bounds

- A mutable inline binding accepts push, pop, clear, and element replacement.
- A fixed inline binding rejects every structural mutation and element write.
- Push at capacity, pop when empty, dynamic out-of-bounds indexing, and invalid
  slicing trap with the corresponding List Runtime Error.
- A constant index greater than or equal to capacity is a compile-time error;
  a constant below capacity still uses logical-length safety.
- Passing, returning, assigning, and nesting an inline List copy by value.
- Structural mutation of a mutable local copied from a by-value parameter does
  not change the caller; the fixed parameter itself remains non-writable.
- `Ptr<mut List<T, N>>` plus explicit dereference permits caller-visible
  mutation; `Ptr<List<T, N>>` remains read-only.
- Copying allocated `List<T>` retains the existing shared-handle behavior.
- `free` works unchanged on `List<T>` and is rejected on `List<T, N>`.
- Built-in method lookup obtains `free` only from the allocated identity;
  inline method metadata records no heap allocation for `push`.
- String and external-state elements retain shallow-copy and shallow-discard
  behavior through push, pop, clear, assignment, and whole-value copy.

### Traversal, slices, and equality

- Both List forms iterate only their logical elements.
- Push, pop, or clear during active traversal traps for either form; element
  replacement remains allowed.
- Inline `slice` and `mut_slice` use logical length; the writable form requires
  a writable inline receiver.
- `slice` and `mut_slice` on a temporary inline List are rejected because the
  result would outlive its addressable backing place.
- Slices into an inline List address its embedded data rather than a copy.
- Copying an inline List does not retarget a Slice derived from the source.
- Equality compares length and active elements, ignores inactive capacity, and
  emits no comparison of tail slots.
- Different inline capacities cannot be compared or assigned.
- Printing emits only active elements.

### Representation and generated artifacts

- Reachable inline specializations emit exactly once, deterministically, with
  `length`, `version`, and `T data[N]`; unreachable specializations emit
  nothing.
- Program-wide element spellings emit through `hexal/list.h`; module-owned
  element spellings emit after their owning declarations.
- Generated push stores the element before incrementing length; generated pop
  reads the element before decrementing length.
- Inactive storage is absent from indexing, slicing, traversal, equality,
  printing, and cleanup code.
- Nested inline Lists, inline Lists of String, pointers, ADTs, Files, Tasks,
  and other currently Array-eligible complete values generate valid C text.
- No generated artifact contains `hex_array_`, includes `array.h`, or emits an
  Array definition/helper.
- The snippet manifest changes only for artifacts whose reachable program uses
  Array, inline/allocated List machinery affected by their unification,
  numeric byte conversion, or network Address.

### Exact-length consumers

- Every `to_le_bytes`/`to_be_bytes` result is a full `List<Byte, N>` with
  logical length N.
- A correct-length inline List round-trips through both reverse byte orders.
- A direct wrong-length literal passed to `from_le_bytes`/`from_be_bytes` is a
  Type Error; a dynamic wrong-length value traps before reading an element.
- Source `List<Byte, 4>` and the IPv4 payload type are one canonical identity;
  the corresponding IPv6 assertion holds at capacity 16.
- Address construction accepts correct source bindings, not only literals.
- A direct wrong-length Address literal is a Type Error; a dynamic wrong-length
  binding traps before producing the Address.
- Address equality and native submission use only validated active bytes.
- Full-width `to_*_bytes` emits no redundant length branch; dynamic
  `from_*_bytes` and Address consumption each emit one check immediately before
  consumption.

### Regression and conformance

- Every former active Array integration case is either migrated to
  `List<T, N>` with its new runtime-length expectation or deleted when it
  asserted removed exact-length behavior; no assertion is weakened merely to
  make the migration pass.
- Parser, checker, generator, type-constructor, built-in-method, component,
  equality, pointer, Slice, module-identity, layout, networking, numeric,
  snippet, and tagged C23 coverage exercise the migrated form.
- `TestGrammarIsVerifiable` passes.
- `go test ./...` and `go vet ./...` pass.
- The tagged C23 lane compiles and runs representative empty, partial, full,
  nested, copied, structurally mutated, exact-width, and network cases.
- `docs/reference.md`, `GRAMMAR.ebnf`, generated templates, central
  diagnostics, tests, snippets, and `docs/status.md` contain no current claim
  that Array remains a language type.

## Implementation plan

### Phase 1: establish the unified type model

1. Rename the internal `TypeArray`/Array metadata path to `TypeInlineList` and
   inline-list metadata, retaining it temporarily beside allocated `TypeList`.
   Give both records source name `List`, select them by parameter shape, and
   preserve the registry's duplicate-shape integrity check.
2. Add `config.MaxInlineListEstimatedBytes` and the checked recursive storage
   estimator; reuse the existing String numeric-capacity parser but not its
   byte-count limit.
3. Give `TypeInlineList` value/copy/unmanaged facts and its own method records;
   keep `TypeList` handle/shallow/managed facts and `free` record unchanged.
4. Resolve the two records through one protected source name without changing
   user-defined generic arity rules. Make bare construction inspect the
   selected record rather than registration order.
5. Enforce the estimated-storage budget and integrate it with existing
   dependent-layout deferral so open generics remain checkable and concrete
   specializations own failures.
6. Teach canonicality, eligibility, equality capability, dependency walking,
   module ownership, and type spelling about the inline form.
7. Add the focused type, method-registry, estimate-category, overflow, and
   boundary tests before removing Array.

### Phase 2: parse and check construction

1. Route `List<T, N>` through the special mixed type/integer argument path.
2. Remove `ArrayType` from `GRAMMAR.ebnf`; route `List<...>` through the
   existing mixed argument parser and preserve parser ownership of a leading
   minus.
3. Generalize the bracket-literal node/checker path from exact Array
   construction to contextual inline-List construction, including `[]`.
4. Add the empty constructor and form-specific constructor diagnostics.
5. Preserve left-to-right checked operands and the existing declaration
   inference boundary.
6. Add writable-place requirements for inline structural methods and reject
   Slice derivation from temporary inline Lists.

### Phase 3: generate inline List C

1. Extend List discovery and render models with demanded inline
   specializations.
2. Add value-layout definitions and inline push/pop/clear/index/slice helpers
   to the existing List component/module ownership paths.
3. Render empty and partial initializers with complete C zero initialization.
4. Add versioned traversal, equality, printing, copying, and pointer/place
   lowering.
5. Prove through text assertions that no operation inspects inactive slots.

### Phase 4: migrate exact-length built-ins

1. Change numeric byte conversion type facts and helpers from Array to inline
   List; add static literal and dynamic length checks.
2. Replace the network package's private built-in Array construction with the
   shared inline-List interner.
3. Add Address construction length enforcement before an Address value is
   produced.
4. Close the source-versus-builtin Address collection identity regression.

### Phase 5: remove Array completely

1. Migrate active sources, snippets, tests, and benchmark fixtures. The
   pre-change inventory is 77 non-test Go matches, 191 Go-test matches, one
   snippet match, and 17 grammar/reference/status matches; repeat the search
   and explain drift before deletion.
2. Delete every parser, checker, type, generator, template, component, and
   diagnostic item named in Required sweep.
3. Add the targeted `Array<T, N>` migration diagnostic without retaining an
   alias or type record.
4. Search production source and active tests for `Array`, `hex_array_`, and
   `array.h`; classify every remaining match. Historical archived specs remain
   untouched.

Phases 1 through 4 may temporarily keep the old internal Array path so each
commit remains green. No intermediate commit exposes both `Array<T,N>` and
`List<T,N>` as supported public spellings. Phase 5 deletes the old spelling
and machinery only after all built-in and test consumers use the new path.

### Phase 6: synchronize contracts and baselines

1. Update `GRAMMAR.ebnf` and verify it mechanically.
2. Update every affected rule in `docs/reference.md` once.
3. Remove the owned Address identity bug from `docs/status.md` after its
   regression passes.
4. Regenerate the snippet manifest and verify that every changed artifact is
   within the Validation blast radius.

### Phase 7: complete validation

1. Run the focused parser, checker, generator, integration, and snippet tests.
2. Run the full ordinary test and vet gates.
3. Run tagged C23 compilation/execution for the representation cases named in
   Validation; ordinary Go success is not generated-C evidence.
4. Rebuild `bin/hexal`. Do not start the workbench; the user manages it.

## Accepted costs

- Every inline List carries `length` and `version`; the former Array carried
  neither. This is the necessary cost of a runtime-length sequence with the
  same traversal safety as List.
- Empty and partial construction zero-initializes the complete inline C object.
  At the 64 KiB ceiling that can write nearly 64 KiB even when logical length
  is zero. The predictable initialization cost avoids indeterminate C storage;
  callers should not use maximum-capacity inline Lists as cheap temporaries.
- Passing, returning, or assigning an inline List can copy its complete
  capacity, up to the same ceiling. Performance-sensitive code passes a Slice
  for a range or an explicit pointer when it needs shared mutation; the
  language adds no hidden reference conversion.
- The 64 KiB estimate rejects some values whose exact C layout might fit and
  can admit a value whose target layout is slightly larger. It deliberately
  avoids duplicating C ABI layout in the Hexal compiler and is only an
  individual-value stack-risk guardrail.
- Capacity no longer proves exact length. Exact-width consumers add a static
  or dynamic check. Full-width producers need no check; dynamic consumers pay
  one branch immediately before use.
- Array source is intentionally breaking and has no compatibility alias.
- Inline and allocated Lists share a name but differ in binding mutability and
  copy behavior because one is a value and one is a handle. The capacity in
  the written type makes that representation difference visible.
- The migration moves a broad generated-C manifest because Array is used by
  collection, numeric, networking, pointer, Slice, equality, and module tests.
- `String<N>` keeps its existing 4 KiB text-payload ceiling while inline List
  uses a 64 KiB whole-value estimate. These limits protect different resources:
  String's bound is its text API contract; List's bound is a conservative
  per-value stack-risk guardrail.

These costs buy a smaller public language surface and the allocation-free
bounded sequence that the former two-type model could not express.

## Review dispositions

The implementation follows these reviewed conclusions:

| Proposal or concern | Disposition | Reason |
| --- | --- | --- |
| Remove the inline `version` field because values do not alias | Rejected | `Ptr<mut List<T,N>>` can mutate the same inline storage during traversal; the alias example above reproduces the need. |
| Zero every slot after `pop` or `clear` | Rejected | Inactive slots are unreachable and do not own element referents under the existing shallow collection contract. |
| Keep exact-length Array because replacement consumers add branches | Rejected | Removing Array is the chosen surface simplification. Direct literals reject statically, full-width producers add no branch, and dynamic consumers check once. |
| Expose Array and inline List together during migration | Rejected | Internal paths may coexist to keep commits green; the supported language never exposes two spellings. |
| Force pointer passing for large inline values | Rejected | That adds a special calling rule. Ordinary value semantics remain uniform; performance-sensitive code chooses Slice or an explicit pointer. |
| Use exact target C layout for the 64 KiB decision | Rejected | The bound is a conservative compiler guardrail, not an ABI claim; exact layout remains the C compiler's job. |
