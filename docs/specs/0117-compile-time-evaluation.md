# RFC 0117: Restricted Compile-Time Evaluation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Created: 2026-08-22
- Scope: compile-time expressions required by types, layout, and assertions
- Depends on: RFC 0019 (generics), RFC 0042 (layout intrinsics), RFC 0052
  (target profiles), RFC 0116 (module storage), and the existing literal,
  numeric-conversion, and diagnostic contracts
- Coordinates with: RFC 0039 (C interoperability), RFC 0055 (build driver),
  RFC 0110 (affine ownership), `docs/reference.md`, and `docs/status.md`

## Summary

Add a small constant evaluator. It is a compiler capability implemented in
Go, not an escape hatch for executing Go code and not general source-level
metaprogramming.

The evaluator makes the following ordinary systems patterns possible:

```hexal
const page_shift: UInt32 := 12
const page_size: Size := 1 << page_shift
buffer: Array<Byte, page_size> := [0, ...]
static_assert(size_of<Header>() <= page_size)
```

The initial facility is expression-only. It does not evaluate arbitrary
functions, blocks, loops, allocations, I/O, or user reflection.

## Constant-expression contexts

The checker must evaluate a constant expression whenever the language requires
a value before runtime code can exist:

- the `N` argument of `Array<T, N>`;
- `const` module storage initializers under RFC 0116;
- `static` and `export static` initializers under RFC 0116;
- `static_assert(condition)`;
- alignment, mask, offset, count, and ABI declaration arguments added by a
  later specification; and
- generic or declaration arguments explicitly marked compile-time by a later
  specification.

The first implementation permits `Array<T, N>` where `N` is any positive
compile-time `Size` value representable on the selected target. The old
positive-integer-literal restriction is removed when this RFC is adopted.

## Evaluation domain

The evaluator accepts:

- integer, Boolean, rune, and byte literals;
- immutable `const` bindings whose initializers are themselves constant;
- `size_of<T>()` and `align_of<T>()` for complete finite types;
- arithmetic, comparison, equality, Boolean, shift, and bitwise operators
  whose operands are constant;
- explicit scalar conversions that satisfy the ordinary checked-conversion
  rules; and
- parenthesized expressions and the existing type and generic syntax needed to
  name the preceding operations.

The evaluator uses Hexal's numeric types and overflow rules. A result that
cannot be represented, a negative unsigned value, a zero or negative array
length, an invalid shift count, or an invalid conversion is a compile-time
diagnostic. It never wraps merely because the eventual C type might wrap.

`size_of` and `align_of` remain target-profile facts. They are evaluated only
after the complete type and selected RFC 0052 profile are known. Incomplete,
recursive-by-value, unresolved, or otherwise non-finite types are rejected.

## Prohibited operations

A constant expression must not:

- call a runtime function or a function whose constant contract is not part of
  this RFC;
- read mutable storage, a pointer, a View, a foreign global, or a volatile
  location;
- allocate, free, clone, share, perform I/O, access the scheduler, or observe
  the host filesystem or environment;
- depend on source-map positions, declaration order, hash iteration order, or
  host integer widths; or
- execute arbitrary Go code, load a Go package, or invoke a compiler plugin.

The implementation may use Go data structures and algorithms to evaluate the
expression, but the accepted result must be determined solely by Hexal source,
the selected target profile, and the specified compiler inputs.

## Layout and ABI use

`size_of` and `align_of` are the initial layout operations. A later layout
extension may add `offset_of<T>(field)` and target-profile queries, but each
operation must define its completeness, alignment, packing, and failure rules
before it becomes a constant-expression primitive.

Compile-time values may size arrays, build masks and lookup-table dimensions,
and feed ABI declarations. They do not make a target-dependent value portable:
the selected profile remains part of the compilation identity and the C driver
must validate the same profile before linking.

## Static assertions

`static_assert(condition)` requires a constant Boolean condition. A false
condition is a source diagnostic naming the assertion's source location; it
does not emit a runtime check or a C statement. A future message argument may
be added only with a grammar and diagnostic contract of its own.

## Implementation model

- Constant evaluation occurs in the checker beside the type and representability
  facts it consumes; no separate analyzer pass is introduced.
- Evaluation is deterministic and memoized by canonical expression identity,
  target profile, and generic substitution where useful. Memoization must not
  change diagnostics or observable compiler output.
- Cyclic constant bindings are rejected with a cycle diagnostic.
- The evaluator is fail-closed: unsupported syntax in a required constant
  context is a diagnostic, never a runtime fallback or a guessed value.
- Generated C may use C23 constant expressions and `_Static_assert` for
  corroboration, but C evaluation is not a substitute for Hexal checking.

## Non-goals

- General compile-time functions or arbitrary compile-time loops.
- Macros, source generation, reflection, compiler plugins, or access to Go
  implementation internals.
- A second runtime execution mode.
- Replacing RFC 0052 target profiles with host probing.

## Validation

This section is exhaustive. RFC 0117 is complete only when every item below
passes:

- Literal arithmetic and the accepted operator set evaluate in every required
  constant context.
- `const` bindings compose transitively and cyclic bindings are rejected.
- `Array<T, N>` accepts a positive constant `Size` expression and rejects
  non-constant, zero, negative, overflowing, or target-invalid lengths.
- `size_of` and `align_of` work for complete finite types and reject incomplete
  or unresolved types.
- Overflow, invalid shifts, unsigned negatives, and invalid conversions produce
  compile-time diagnostics with no generated runtime expression.
- Mutable, pointer, foreign, volatile, allocator, I/O, scheduler, filesystem,
  and arbitrary function operations are rejected in constant contexts.
- `static_assert(true)` emits no runtime operation and `static_assert(false)`
  fails compilation at the assertion location.
- Different target profiles may produce different valid constants, but the
  selected profile is included in the build identity and generated C text.
- Generated C uses only corroborating constant expressions and emits no
  duplicate runtime evaluator or hidden initialization path.
- Ordinary tests remain pure Go and assert checked values, diagnostics, and
  generated-C text.

## Open questions

1. Whether later versions add conditional expressions or bounded compile-time
   loops without turning this into general metaprogramming.
2. Whether compile-time strings and fixed byte arrays need a separate literal
   contract.
3. The exact `offset_of` spelling and whether it is safe for all representable
   object layouts.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` with the exact
constant-expression grammar, allowed operations, failure rules, `Array<T,N>`
acceptance rule, `static_assert` syntax, and target-profile dependency. Remove
the current positive-literal-only and “size_of does not unlock Array lengths”
rules in the same change.
