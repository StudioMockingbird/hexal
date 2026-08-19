# RFC 0088: Provably Dead Bounds Checks

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-19
- Scope: do not emit an `Array<T,N>` bounds check the compiler has already
  proven cannot fire, and do not emit an accessor nothing calls
- Depends on: nothing. Coordinates with RFC 0082, which establishes the same
  demand-driven emission rule for component includes.
- Coordinates with: `docs/reference.md`, `docs/status.md`
- Does not change: Array representation, value semantics, trap behaviour, or
  any language surface

## Summary

`for row in grid` over an `Array<T,N>` emits a checked accessor call whose check
can never fire: the loop bound is the literal `N` and the index is a counter the
compiler emitted itself. A program that only iterates arrays emits a full set of
accessor functions purely to service checks that are dead by construction.

Emit the check only where it can fire. Emit the accessor only where it is called.

## Evidence

`control-nested-coordinate-scan` in the catalog:

```hexal
grid: Array<Array<Int32, 3>, 2> = [[1, 2, 3], [4, 5, 6]]
for row in grid do
    for cell in row do
```

Generated today:

```c
for (size_t hex_for_1_index = 0; hex_for_1_index < (size_t)(2); hex_for_1_index++) {
    const hex_array_Int32_3 hex_v_row =
        *hex_array_at_Array_Int32__3__2(hex_for_1, hex_for_1_index);
    /*   ^ tests index >= 2 inside a loop bounded by 2 */
```

and `hexal/array.h` carries four accessor functions — about 24 of its 38
lines — although **the snippet never indexes an array**. Every one exists to
service `for-in`.

## What is provable, and why

### Case 1 — `for-in` over `Array<T,N>`

Four facts make this unconditional, none of them new analysis:

1. `N` is a positive integer literal in the type (`reference.md`, `Array<T,N>`),
   so the bound is a compile-time constant.
2. The loop emits its own counter over `[0, N)`.
3. A `for` binder is *fresh and immutable* (`checker.binding.loopBinder`), so
   the body cannot alter the index.
4. An Array cannot be resized, so `N` cannot change mid-loop. Reassigning a
   `mut` Array binding replaces the region but not its length.

The index is therefore in `[0, N)` at every iteration, by construction.

### Case 2 — a constant index

**The checker already proves this and rejects the failing half.** Probed:

```
a[0]  on Array<Int32,5>   accepted
a[4]                      accepted
a[5]                      rejected: array index 5 is out of bounds for Array<Int32, 5>
a[7]                      rejected: array index 7 is out of bounds for Array<Int32, 5>
```

Any constant index that survives checking is in range. The generator does not
need to re-derive that — it needs only to notice the operand is a constant and
skip the check.

### Consequence — accessors become demand-driven

An accessor is emitted only when a program performs an access whose check
survives. A program that only iterates arrays emits none, and the grid snippet's
`hexal/array.h` collapses from ~38 lines to two typedefs, an include guard, and
`hexal.h`.

This is RFC 0082's rule applied one level down: emit what is used, not what
might be.

## The change

Where an access is proven in range, emit direct member access instead of a call:

```c
/* for-in binder */
const hex_array_Int32_3 hex_v_row = hex_for_1->data[hex_for_1_index];

/* constant index */
const int32_t x = hex_v_a.data[0];
```

Accessor emission keys off whether any surviving access needs one, per array
specialization and per direction (`at` versus `at_mut`) — a program that only
reads emits no `at_mut`.

## Not doing: replacing the struct with a raw C array

Recorded because the question recurs. `Array<Array<Int32,3>,2>` **cannot** lower
to `int32_t grid[2][3]`.

A bare C array decays to a pointer on every pass, which contradicts
`reference.md:727` — *"Assignment, arguments, and returns copy the inline
region."* Three operations depend on the struct:

| Operation | Struct | Bare C array |
|---|---|---|
| pass by value | copies the region | decays to a pointer, aliases the caller |
| return | legal | illegal in C |
| assign `a = b` | legal, copies | illegal in C |

Removing array-to-pointer decay is a deliberate goal, and the single-member
struct is the mechanism that removes it. Its layout is identical to the bare
array, so it costs a typedef and nothing at runtime. The verbosity in the
generated C comes from the dead checks, not from the wrapper.

## Not doing: inlining the check as an expression

Also recorded because it is the natural first idea. Replacing the accessor call
with a ternary and a comma-operator trap spells the same work less clearly, and
`AGENTS.md` requires generated C to stay *"as plain as the compiler source."*
The `static inline` accessor already optimizes away; the win is in not emitting
a check, not in respelling one.

## Invariants

1. Every check that can fire is still emitted. This RFC removes checks that are
   proven dead, never checks that are merely unlikely.
2. Trap behaviour, messages, and positions are unchanged for every access that
   keeps its check.
3. Array representation, layout, `sizeof`, and value semantics are unchanged.
4. No language surface changes; no program's acceptance changes.
5. Runtime-indexed access — a variable index not proven constant — keeps its
   accessor and its check, including inside a `for-in` body.

## Validation

- The grid snippet emits no `hex_array_at_*` call and no accessor definition;
  `hexal/array.h` contains the two typedefs and nothing else.
- `a[0]` on `Array<Int32,5>` emits direct member access, no call.
- `a[i]` with a runtime `i` still emits the checked accessor and still traps out
  of range.
- A `for-in` body that *also* indexes the same array with a runtime index emits
  the accessor for that access and direct access for the binder.
- A read-only program emits no `at_mut`; a writing program emits it.
- Nested `Array<Array<T,N>,M>` elides at both levels.
- `List` and `View` output is byte-identical — this RFC does not touch them.
- `go test ./...`, `go vet ./...`; the manifest moves for array-using snippets
  and no others.

## Non-goals

- `List` and `View` bounds elision. Their length is a runtime field, not a
  literal, so proving a check redundant requires proving the body does not
  resize. Decidable in cases, and out of scope here — this RFC takes only what
  is unconditional.
- General range analysis, loop-invariant reasoning, or induction variables.
  Both cases here are read directly off the type and the operand.
- A `-fbounds-check` style mode, or any way for a program to disable checks that
  can fire.
- Changing Array representation — see above.

## Drawbacks

- Two access shapes in the generated C for one source operation: a call where
  the check lives, direct member access where it does not. A reader must know
  the rule to see why. The alternative is emitting a check the compiler has
  already proven dead, which is worse for both size and honesty.
- Accessor emission becomes conditional on access shape, so a small source edit
  can add or remove a function from `hexal/array.h`. That is already true of
  every component artifact after RFC 0082.
