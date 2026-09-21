# RFC 0226: Inline Bounded Sequences

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; exploratory proposal; not scheduled. One blocking
  issue may be fatal to the proposal in safe code — see Blocking issues 1
- Created: 2026-09-21
- Updated: 2026-09-21
- Origin: split out of RFC 0224, which originally proposed replacing both
  `Strand` and `Array<T, N>` under one storage pattern. The text half survived
  that audit and the sequence half did not, because inline text has a valid
  representation for its unused tail and an inline sequence does not
- Depends on: the current `Array<T, N>` and `List<T>` contracts in
  `docs/reference.md`
- Coordinates with: RFC 0224 (byte-oriented strings), which settled the
  size-parameter syntax this RFC inherits; RFC 0160 (memory-bug inventory), two of
  whose solved rows this proposal touches; RFC 0165 (local alias diagnosis)
- Does not update `docs/reference.md`

## Summary

Hexal has two sequence types:

```text
Array<T, N>   inline, exactly N elements, no resize
List<T>       heap-backed, dynamically sized, growable
```

There is no inline sequence with a *runtime* length — a buffer with capacity
N that starts empty and fills up without allocating. RFC 0224 originally
proposed adding one under the spelling `List<T>[N]` and using it to replace
`Array<T, N>` entirely.

This RFC asks the two questions that proposal conflated:

1. **Does Hexal want an inline bounded sequence at all?**
2. **If so, can it replace `Array<T, N>`, or must it sit beside it?**

The audit below answers the second question: **it cannot replace
`Array<T, N>`.** The first question is open and is the real decision.

## The central problem: inactive slots have no valid value

This is the issue that separated this RFC from RFC 0224, and it may be fatal
to the proposal in safe code.

Hexal has **no default-value or zero-initialization concept**. Initialization
is mandatory everywhere, by construction. Probed:

- `let n: Int32` with no initializer is rejected;
- `h.allocate<Int32>()` is rejected with `allocation requires an explicit
  initializer`;
- an `Array<T, N>` literal must contain exactly N elements.

An inline sequence with capacity N and logical length below N therefore has
`N - length` slots holding no valid T. Because the value is inline and copied
by value, every copy copies those slots:

```hexal
let mut a: List<String>[10] = []
a.push(some_string)
let b: List<String>[10] = a    # copies 9 indeterminate String handles
```

For `Int32` the inactive slots are merely indeterminate. For `String`, a
pointer, an ADT whose tag must be a valid discriminant, or a nested bounded
sequence, the copy produces values that are not merely unspecified but
invalid — indeterminate bytes the rest of the language is entitled to treat as
a live handle.

That is goal 16 (no undefined behavior), and it is the bug class RFC 0160
records as "Uninitialized read — Solved by construction. Mandatory
initializers". RFC 0160's Goals require any proposal touching initialization
to show the row stays solved, and unlike RFC 0157 this cannot be confined
behind an `unsafe` gate, because it would be ordinary safe code.

Inline *text* does not have this problem: a `String[N]`'s unused tail is zero
fill, and zero is a valid byte. There is no equivalent universally-valid
element for an arbitrary T.

### The available resolutions, none free

| Resolution | Cost |
|---|---|
| Require every slot to hold a valid initialized T | Makes the type a fixed array again; removes the point of the proposal |
| Add a default-value or zeroability concept | A large new language feature needing its own RFC; changes what "complete type" means |
| Store elements as aligned opaque bytes; copy only the first `length` slots | Copying stops being a plain struct copy; interacts with C interop and `size_of` |
| Carry per-slot initialization metadata | Runtime metadata, against goal 15 |
| Restrict T to types with a safe inactive representation | Needs a new type predicate; excludes `String`, handles, and most ADTs — which is most of the useful cases |

A promoted version of this RFC must pick one and carry it through the C
representation, copy semantics, and element eligibility. Until then the
proposal is not implementable in safe code.

## Why it cannot replace `Array<T, N>`

Even setting aside the initialization problem, the two types have different
contracts and the substitution loses guarantees.

### Fixed length is a guarantee, not a limitation

`Array<Int32, 3>` is always exactly three elements, so `a[0]`, `a[1]`, and
`a[2]` can never trap. A bounded sequence with capacity 3 and length `0..3`
makes `values[2]` trap until three pushes have happened. The type stops
implying the length.

The compile-time rejection survives in weakened form — an index `>= N` is
still statically wrong — but the guarantee that an in-range index is *valid*
does not:

```text
[Type Error] array index 5 is out of bounds for Array<Int32, 3>
```

This touches RFC 0160's Out-of-bounds row and moves behavior away from goal 17
("if it compiles, it runs").

### `Array<Byte, N>` uses require exactly N

`Array<Byte, N>` appears in shipped contracts where the fixed length is the
point:

- numeric byte conversion — `T.to_le_bytes()`/`to_be_bytes()` return, and
  `T.from_le_bytes(array)`/`from_be_bytes(array)` require, an **exact**
  `Array<Byte, N>` (probed: `let b: Array<Byte, 4> = v.to_le_bytes()`
  compiles);
- socket addresses — `IPv4 as bytes: Array<Byte, 4>` and
  `IPv6 as bytes: Array<Byte, 16>`.

A bounded sequence whose length may be `0..4` cannot express
`from_le_bytes`'s precondition without a runtime check the current API does
not have.

### The C representation is strictly larger

An `Array<T, N>` needs no length because its length is always N. Today's
output is exactly:

```c
typedef struct hex_array_Int32_3 {
    int32_t data[3];
} hex_array_Int32_3;
```

A bounded sequence must carry its length at runtime:

```c
typedef struct { size_t length; T data[3]; } hex_bounded_T_3;
```

So every replacement grows by one `size_t` per value, in every aggregate that
embeds one, against goal 15.

### Conclusion

`Array<T, N>` stays. If an inline bounded sequence is wanted, it is a **third**
sequence form beside `Array<T, N>` and `List<T>`, not a replacement for
either. That is also how Zig and Odin arrange it: Zig keeps `[N]T` and
`std.BoundedArray(T, N)` as separate things, and Odin keeps `[N]T` and
`[dynamic]T`, because "exactly N" and "up to N" are different contracts and
code wanting the first should not pay for the second.

## If it is adopted: candidate semantics

Recorded so the shape of the decision is concrete. Every item here is
conditional on Blocking issues 1 being resolved.

- storage is inline and capacity is exactly N;
- logical length starts at zero and ranges from zero through N;
- `push` appends while length is less than N and traps at capacity;
- `pop` removes the last element and traps when empty;
- `clear` removes all logical elements without changing capacity;
- indexing and slicing use the current logical length, not N;
- elements copy by value using ordinary shallow-copy rules;
- no heap allocation occurs, and there is no `free` — calling `free` on an
  inline sequence is a compile-time error.

**Naming.** `List<T>[N]` is not recommended. `List<T>` is a heap-backed
handle, and reusing its name for an inline value repeats the
representation-follows-ownership problem recorded in RFC 0224's Blocking
issues 2. A distinct name — `BoundedList<T, N>`, or `Buffer<T, N>` — keeps
the name predicting the representation, and uses the existing angle-bracket
convention instead of the postfix form RFC 0224's Blocking issues 1 questions.

## Blocking issues

Each item was probed against the tree on 2026-09-21.

### 1. Inactive slots have no valid value

See The central problem above. This is the issue most likely to invalidate the
proposal outright in safe code, and it must be resolved before anything else
in this RFC is worth specifying.

### 2. Mutation through an immutable binding does not carry over

RFC 0224's original example was:

```hexal
let values: List<Int32>[10] = []
values.push(1)
```

That does not compile and cannot. Mutation through an immutable binding works
for `List<T>` **because it is a handle** — the mutation goes through the
pointer, not the binding. An inline value has no such indirection, and inline
mutation requires a writable place. Probed, this is exactly how `Array` and
`List` differ today:

```text
let a: Array<Int32, 3> = [1,2,3]      a[0] = 9   rejected:
                                       "cannot write through a read-only pointer a[...]"
let mut a: Array<Int32, 3> = [1,2,3]  a[0] = 9   accepted
let l: List<Int32> = List<Int32>(h)   l.push(1)  accepted
```

So an inline bounded sequence must require `let mut` while `List<T>` does not.
If the two share a name, that is two mutability rules behind one name — a
further argument for the distinct name recommended above.

This RFC must also state what happens when a bounded sequence is passed by
value: `push` inside the callee mutates only the callee's copy, silently,
which is the opposite of `List<T>`'s behavior through the same method name.

### 3. `[]` is not a valid literal

Every example of an empty bounded sequence requires a grammar and checker
change. Probed, the empty form is rejected:

```text
[Type Error] an array literal requires at least one element
```

`ArrayLiteral` currently requires at least one expression. This RFC must also
say whether `[1, 2]` is valid for a capacity-10 sequence, and whether a
constructor call form exists.

### 4. Spelling — settled by RFC 0224, and it settles it here too

No longer open. RFC 0224 selected angle brackets (`String<N>`) over the
postfix form, because `IndexSuffix = "[" Expression "]"` already claims
postfix brackets and every compiler-known parameterized type uses `<...>`.

The same answer applies here: a bounded sequence is spelled with angle
brackets, which is what the recommended `BoundedList<T, N>` naming in Naming
already assumes. `List<T>[N]` is withdrawn on syntax grounds independently of
the naming argument.

### 5. The size parameter is a new concept

Shared with RFC 0224's Blocking issues 4. `docs/reference.md` states "User
parameters are types only. Compiler-owned `Array<T, N>` uses a positive
integer literal N." This RFC must say whether the parameter stays
compiler-owned-only, and must bound it: maximum N, literal-versus-alias,
digit separators, and `N * size_of<T>()` overflow. The bound matters more here
than for text: a `BoundedList<T, N>` is `N * size_of<T>()` bytes, so a modest
N over a large T is a much larger inline value than any `String<N>`.

## Unspecified surface

Contracts a promoted version must state:

- **Traversal invalidation.** `List<T>` versions its structure and traps on
  modification during iteration; `Array` needs no such rule. A bounded
  sequence has a runtime length but non-relocating storage, so it must pick
  one behavior and say what `push`, `pop`, and `clear` do during an active
  traversal.
- **`mut_slice`.** `Array<T, N>` exposes both `slice` and `mut_slice`; the
  writable form must be specified or excluded.
- **Slice validity** after `push`, `pop`, `clear`, element replacement,
  whole-value copy, and whole-value reassignment. The backing address never
  moves, but logical element lifetime still changes, so a slice into a popped
  or cleared region must not silently stay valid.
- **Equality.** Does capacity participate, or only the logical elements? Are
  two sequences of different capacity comparable?
- **Conversions** to and from `List<T>` and `Array<T, N>`, each explicit and
  without implicit allocation.
- **C ABI**: passing and returning by value, address-of, use as a `Ptr`
  pointee, `size_of`/`align_of`, and foreign declarations.
- **Element eligibility**, which follows directly from whichever resolution
  Blocking issues 1 selects.

## Non-goals

- Replacing `Array<T, N>`. The audit above concludes it stays.
- Ownership, borrowing, lifetimes, affine moves, or automatic cleanup.
- Implicit heap allocation in any conversion.
- Changing `List<T>`'s representation, growth, or invalidation rules.
- A default-value or zeroability concept. If Blocking issues 1 is resolved
  that way, it needs its own RFC and this one depends on it.

## Open questions

1. **Does Hexal want this type at all?** The honest case for it is a
   stack-allocated buffer that fills at runtime without touching the heap —
   a parser token buffer, a fixed-capacity work queue, a small string builder.
   The case against is that `Array<T, N>` plus an explicit length variable
   already expresses it, at the cost of the two being unbundled. No workload
   in the tree currently demands it, and adding a third sequence form is a
   real cost against goal 3 (small surface) and goal 2 (one obvious way).
2. If adopted, which resolution of Blocking issues 1?
3. If adopted, what is the name and spelling? `BoundedList<T, N>` is
   recommended over `List<T>[N]` for the reasons in Naming.
4. Should it be a compiler-owned type at all, or could it be expressed in
   Hexal once the language has enough to write it? A library answer would
   avoid every syntax question in this RFC.

## Validation sketch

Not attempted. A promoted version must replace this with an exhaustive
Validation section, and cannot do so before Blocking issues 1 is resolved,
because that resolution determines the representation, the copy semantics, and
which element types are eligible.
