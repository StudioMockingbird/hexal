# RFC 0024: Equality, Ordering, and Hashability

- Kind: Language Semantics Specification (ISO/IEC Language Standard Format)
- Status: Implemented; conformance verified 2026-08-11
- Features: lossless numeric comparison widening, value and identity equality,
  ordering, compiler-internal dictionary hashing, and specialization-time
  generic operation checking
- Created: 2026-08-09
- Depends on: RFC 0003 (core scalar types), RFC 0006 (objects), RFC 0009
  (core operators), RFC 0014 (unions), RFC 0016 (numeric conversions and
  lossless widening), RFC 0018 (String and Rune values), RFC 0019 (generics),
  RFC 0020 (collections), RFC 0022 (ADTs)
- Coordinates with: RFC 0023 (truthiness),
  and the future ownership and FFI specifications

## Summary

Equality is currently defined in separate feature specifications. This RFC
defines the cross-cutting rules for `==`, `!=`, ordering operators, and the
compiler-internal equality, ordering, and dictionary-hash operations used by
generic specialization.

Non-numeric values remain strict. Numeric comparisons may use only the
lossless widening relation defined below:

```seawitch
left: Int32 = 1
right: Int64 = 1

same: Bool = left == right       // valid: left widens to Int64
```

Equality never performs implicit narrowing, value-losing conversion,
dereference, text conversion, sequence conversion, or representation-based
comparison.

## Design principles

1. Equality is symmetric and produces `Bool`.
2. `!=` is the logical complement of `==`.
3. Non-numeric operands must have identical canonical types except for the
   explicit `Nil` comparison rules. Numeric operands must have identical types
   or one unique common type under lossless widening.
4. Value types compare values; pointers compare identity.
5. Padding bytes, allocator addresses, inactive union payloads, and generated C
   layout are never observable through equality.
6. Ordering is available only where one coherent ordering exists.
7. A hash must agree with equality: equal values always have equal hashes.

## Equality eligibility

The initial equality matrix is:

| Type category | `==` and `!=` | Meaning |
|---|---|---|
| `Bool` | yes | Boolean value equality |
| integers | yes | Mathematical integer value equality |
| floats | yes | IEC 60559 equality |
| `Rune` | yes | Unicode scalar value equality |
| `String` | yes | Exact UTF-8 byte-sequence equality |
| `Strand` | yes | Exact logical payload-byte equality, excluding the terminator |
| `Nil` | yes | Two `nil` values are equal |
| objects | conditional | Recursive member-wise equality |
| pointers | conditional | Pointer identity equality |
| structural unions | conditional | Same active member and equal payload |
| ADTs | conditional | Same variant and equal payload |
| arrays, views, lists | conditional | Element-wise sequence equality |
| dictionaries | no in v1 | Collection equality is deferred by RFC 0020 |
| allocator handles | no | Allocation identity is not a source value comparison |
| functions | no | Function values are not equality-comparable |

"Conditional" means every recursively compared component must itself support
the required equality operation. Otherwise the checker reports why equality is
unavailable.

## Scalar equality

Boolean and rune equality compares values only with the identical canonical
type. `Rune` remains distinct from `UInt32`; its Unicode-scalar invariant does
not participate in implicit numeric widening. `Byte` is RFC 0018's transparent
alias of `UInt8` and therefore follows `UInt8` rules.

Numeric equality and ordering use RFC 0016's exact lossless widening table and
unique least-common-type algorithm. This RFC does not define a second promotion
relation. If no unique common type exists, the comparison is rejected and one
operand requires an explicit destination conversion method from RFC 0016.

Examples:

```seawitch
i32: Int32 = read_i32()
i64: Int64 = read_i64()
u32: UInt32 = read_u32()
u64: UInt64 = read_u64()
f32: Float32 = read_f32()

i32 == i64       // valid; compare as Int64
i32 == u32       // valid; compare as Int64
i32 < f32        // valid; compare as Float64
i64 == u64       // Error: no lossless common built-in type
```

The checker inserts only RFC 0016's proven lossless conversions. No runtime
range test is required because validity follows from the source and destination
type ranges.

Floating equality follows RFC 0009 and IEC 60559:

- any comparison involving NaN is false except `!=`;
- `NaN != NaN` is true;
- positive and negative zero compare equal; and
- infinity compares equal only to infinity with the same sign.

`String` compares exact UTF-8 byte sequences. Text comparison does not
normalize, case-fold, or use locale-sensitive comparison. Different Unicode
encodings of canonically equivalent text may compare unequal. `Strand` compares
its logical payload bytes and excludes its representation-only terminating NUL.
`String` and `Strand` are different canonical text types and are not directly
equality-comparable.

RFC 0020's static, owning, parameter-borrowed, and collection-borrowed String
provenance does not affect equality or ordering. The operation borrows both
String operands for its duration, compares their immutable payloads, and does
not allocate, clone, transfer, retain, or free either operand.

`Nil` is comparable to itself and to a type that explicitly contains `Nil`:

```seawitch
value: Int32 | Nil = read_value()
missing: Bool = value == nil
present: Bool = value != nil
```

A non-null type cannot be compared to `nil`. `nil` comparison does not make two
non-null pointer values comparable with `Nil`; it only tests the active member
of a nullable union. Two values of the same nullable union type compare through
the ordinary union-equality rules:

```seawitch
left: Ptr<Node> | Nil = read_left()
right: Ptr<Node> | Nil = read_right()

both_missing_or_same: Bool = left == right
missing: Bool = left == nil
```

## Object equality

Nominal objects compare recursively by declared members when every member is
equality-comparable:

```seawitch
type Point = { x: Int32, y: Int32 }

same: Bool = left == right
```

Member declaration order determines the comparison sequence but not the
mathematical result. Read-only and mutable member modes do not affect equality.
Padding, unused storage, and C representation bytes are ignored.

An object containing a pointer member compares that member by pointer identity,
not by dereferencing the pointee. This keeps equality finite and prevents
cyclic pointer graphs from causing nontermination. An object containing an
unsupported member type is not equality-comparable.

## Pointer equality

Pointers compare identity, not pointee values. Two non-null pointers are equal
when they refer to the same address. The operands must have identical canonical
pointer types; an outermost `MutPtr<T>` weakening is not silently inserted for
equality. RFC 0010 makes `Ptr<T>` and `MutPtr<T>` non-null. A nullable
`Ptr<T> | Nil` or `MutPtr<T> | Nil` value therefore compares through union
equality rather than through this direct pointer rule.

```seawitch
left: Ptr<Int32> = ref value
right: Ptr<Int32> = left
same: Bool = left == right
```

Pointer equality does not dereference memory, prove pointee validity, or compare
pointed-to objects. Equality with `nil` remains the focused nullable test.

Function values are not comparable, even where the target C representation
would permit function-pointer comparison.

## Union and ADT equality

Structural union equality follows RFC 0014. Both operands must have the same
canonical union identity and every member must support equality:

1. different active members compare unequal;
2. matching active members compare their payloads recursively; and
3. a matching `Nil` member compares equal without reading payload storage.

ADT equality follows RFC 0022. Both operands must have the same nominal ADT
identity:

1. different variants compare unequal;
2. two unit variants with the same owner and name compare equal; and
3. matching record variants compare their payload fields recursively.

Neither union nor ADT equality widens operands or compares only their C tags.
Variant tags select the comparison path; payload values determine equality when
the tags match.

## Collection equality

### Arrays, views, and lists

`Array<T, N>`, `View<T>`, and `List<T>` compare element-wise when their element
type is equality-comparable. Both operands must have the identical canonical
collection type. Arrays therefore require the same `T` and `N`; Views and Lists
require the same `T`. There is no cross-family or cross-element conversion for
sequence equality. Views and Lists additionally require equal runtime lengths.

```seawitch
same: Bool = first_list == second_list
```

The comparison observes only the visible sequence. View backing addresses,
capacity, and ownership are ignored.

List equality borrows both collections for the complete operation. It performs
no mutation or ownership transfer. `List<String>` compares the existing nested
String payloads in place and never creates owning String copies.

### Dictionaries

Dictionary equality is not available in v1. RFC 0020 deliberately defers it
until the collection ownership, lookup, and value-lifecycle rules support a
non-mutating unordered comparison. Bucket order, capacity, hash seed, and
tombstones therefore have no equality semantics yet.

## Ordering

The ordering operators `<`, `<=`, `>`, and `>=` are available for:

- integer and floating scalar types with an identical type or one lossless
  common type under the numeric relation above;
- `Byte`, through its canonical `UInt8` identity;
- `Rune`, using scalar numeric order; and
- `String`, using lexicographic order over UTF-8 bytes; and
- `Strand`, using lexicographic order over its logical payload bytes.

Ordering is rejected for `Bool`, `Nil`, allocator handles, pointers, functions, objects, ADTs,
structural unions, arrays, views, lists, and dictionaries in this RFC. Ordering
between `String` and `Strand` is also rejected because they are different
canonical text types.

Floating ordering remains partial. Any ordering comparison involving NaN is
false, including `NaN <= NaN` and `NaN >= NaN`. Availability of the ordering
operators does not promise a total order for floating values.

`String` and `Strand` ordering are bytewise and locale-independent. They are not
Unicode collation, case-insensitive ordering, or grapheme ordering.

Lexicographic text ordering compares logical payload bytes as unsigned values
from index zero. The first unequal byte determines the result. If every byte in
the shorter payload matches the corresponding byte in the longer payload, the
shorter payload sorts first. Equal-length equal payloads are equal. An embedded
NUL in `String` is an ordinary payload byte; `Strand`'s representation-only
terminating NUL and zero-filled tail are never compared.

## Hashability

V1 exposes no source-level `hash(value)`, `.hash()`, `Hashable` constraint, or
hash result. Hashing is a compiler-internal operation used only by RFC 0020's
dictionary implementation. The only v1 dictionary key types, and therefore the
only v1 hash specializations, are exactly `Int32` and `Strand`.

Every generated dictionary hash operation must obey:

```text
a == b  =>  internal_hash(a) == internal_hash(b)
```

`internal_hash` above is specification notation, not source syntax.

The converse is not required. `Int32` hashes its mathematical signed value.
`Strand` hashes exactly its logical payload bytes and excludes its terminating
NUL and zero-filled tail, matching Strand equality.

The algorithm and any seed are implementation details. Hash values are not
observable Seawitch values, are not stable between compiler or runtime
versions, and are not serialization or ABI contracts. No randomized seed is
required in v1. An implementation may add one without changing source
semantics if the dictionary helper receives it consistently.

Hashing `Bool`, other integer widths, floats, `Rune`, `String`, pointers,
objects, ADTs, unions, sequences, functions, allocator handles, and
dictionaries is deferred because no v1 source feature consumes those hashes.

## Generic operation requirements

RFC 0019 v1 does not expose user-written generic constraint syntax. This RFC
does not introduce source names such as `Equatable`, `Ordered`, or `Hashable`.
Equality and ordering requirements arise only from the concrete operators used
in a generic body. Dictionary hashing is an internal requirement created only
when specializing `Dict<Int32,V>` or `Dict<Strand,V>`.

When a generic body uses `==`, `!=`, `<`, `<=`, `>`, or `>=` with a type
parameter, the open generic check records that exact dependent operator. The
concrete specialization rechecks the operator, including any lossless common
numeric type, before C generation. An unsupported specialization is a
compile-time error at the use that requests it; there is no runtime dispatch or
fallback implementation.

Operation availability adds only the lossless numeric comparison widening
defined by this RFC. It adds no narrowing, dereferencing, structural
compatibility, text or collection conversion, or dynamic dispatch.

## Evaluation and lowering

Equality and ordering evaluate each operand exactly once. Relative ordering of
independent operands remains unspecified under RFC 0009. `and` and `or` retain
their short-circuit rules under RFC 0023.

After both top-level operands have been evaluated, recursive equality proceeds
deterministically and stops at the first unequal component:

- object members are compared in declaration order;
- matching ADT and structural-union payload fields are compared in declaration
  order;
- arrays, views, and lists compare lengths where applicable, then elements in
  increasing index order; and
- a different union member or ADT variant is unequal without reading either
  inactive payload.

`!=` is the logical complement of this one `==` operation. It does not evaluate
either operand or compare any member or element a second time.

A lossless numeric comparison converts each evaluated operand once to the
checked common type before comparing. Generated C must not rely on C's usual
arithmetic conversions to choose that type. String comparison borrows each
String for the operation. List comparison borrows both Lists and therefore
cannot transfer, free, or mutate either collection while comparison is active.

These comparison borrows follow RFC 0020's existing complete-expression and
sibling-expression invalidation rules. If evaluating one operand could mutate,
free, replace, or otherwise invalidate storage borrowed by the other operand,
the checker rejects the comparison because operand evaluation order is
unspecified:

```seawitch
same: Bool = names.at(0) == mutate_and_return_name(names)
// Error: a sibling operation may invalidate the String borrowed from names.
```

Comparison creates no exception that permits an otherwise rejected alias,
borrow escape, mutation, ownership transfer, or cleanup operation.

The generator must lower semantic equality rather than compare generated C
storage:

- scalar equality uses the checked scalar operation;
- objects compare declared members, never padding;
- pointers compare addresses without dereferencing;
- unions and ADTs test the active tag before reading a payload;
- sequences compare length and elements in order; and
- dictionary key helpers hash only `Int32` or the logical Strand payload.

Generated equality helpers must be emitted once per concrete type and must
preserve source `#line` mappings for source-visible comparisons. An unsupported
equality, ordering, or hash node reaching generation is an `Unknown Error`.

## Diagnostics

Representative diagnostics are:

```text
[Type Error] comparison has no lossless common numeric type
[Type Error] equality requires identical canonical non-numeric operand types
[Type Error] equality is unavailable because member Point does not support ==
[Type Error] pointer equality requires identical pointer types
[Type Error] function values are not equality-comparable
[Type Error] ordering is unavailable for ADT values
[Type Error] dictionary key type must be Int32 or Strand
[Type Error] hash is not a source-level operation
```

The checker owns operator eligibility, recursive operation-availability checks,
and dependent generic specialization checks. The generator must not replace a
rejected operation with a C
`memcmp`, pointer dereference, raw tag comparison, or implementation-defined
cast.

## Compatibility and supersession

This RFC centralizes and extends the comparison rules already established by
RFC 0009, RFC 0014, RFC 0018, RFC 0020, and RFC 0022. Those specifications
remain the historical source of their feature-local rules; this RFC owns the
cross-cutting eligibility and operation-availability contract once accepted.

RFC 0016 owns the exact supersession of RFC 0003 and RFC 0009's no-widening
rules. This RFC applies that one language-wide relation to equality and
ordering. It supersedes RFC 0018's broader claim that String and every listed
text type are generally hashable: v1 exposes only the internal `Int32` and
`Strand` dictionary hashes defined here.

It does not change truthiness. Boolean contexts and falsey values are owned by
RFC 0023.

## Deferred

- User-defined equality, ordering, and hash implementations.
- Any public hash expression, method, result, capability, or constraint.
- Internal hashing beyond the exact `Int32` and `Strand` dictionary keys.
- Float hashing, NaN canonicalization, and signed-zero hash policy.
- Pointer hashing and identity semantics across foreign boundaries.
- Locale-aware and normalized text collation.
- Ordering for objects, ADTs, unions, and collections.
- Serialization-compatible stable hash algorithms.
- Approximate equality and tolerance-based floating comparisons.
- Equality of external or opaque foreign values.

## Acceptance criteria

Implementation is complete when focused end-to-end tests prove that:

1. numeric equality and ordering select the unique least lossless common type,
   reject every narrowing or value-losing pair, and non-numeric comparison
   preserves strict canonical typing;
2. floating NaN, infinity, and signed-zero behavior matches the stated rules;
3. `Nil` comparisons work only in permitted nullable contexts;
4. nullable unions compare `Nil` and pointer members through union equality,
   while direct pointer equality applies only to identical non-null pointer
   types;
5. objects compare declared members in order, stop at the first inequality,
   and never read padding;
6. pointers compare identity without dereferencing;
7. unions and ADTs compare tags and matching payloads safely and stop at the
   first unequal payload field;
8. arrays, views, and lists compare lengths and elements in order, short-circuit
   on inequality, and obey RFC 0020's borrow-invalidation rules, while
   dictionary equality is rejected in v1;
9. String and Strand ordering uses unsigned payload-byte order, shorter-prefix
   ordering, and the specified treatment of NUL bytes;
10. unsupported ordering and function equality receive focused diagnostics;
11. equality and ordering operators are checked at concrete generic
   specialization before generation, without public capability syntax;
12. only internal `Int32` and logical-payload `Strand` dictionary hashes are
     generated, and both preserve the equality/hash contract;
13. generated C contains no storage `memcmp`, inactive union read, or unsafe
     comparison fallback; and
14. every new checked comparison, internal dictionary hash, dependent
     specialization, and generator case is handled explicitly under the
     fail-closed architecture.
