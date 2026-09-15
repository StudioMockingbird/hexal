# RFC 0156: Fenced Pointer Arithmetic and Casts

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-10
- Updated: 2026-09-15
- Depends on: RFC 0155 (`unsafe do ... end`)
- Coordinates with: RFC 0039 (foreign declarations and ownership metadata)
- Does not update `docs/reference.md`: synchronize only after implementation
  stabilizes and the user explicitly approves the reference edit

## Summary

Add the minimum raw-pointer operations needed for allocators, binary formats,
and C interoperability:

```hexal
unsafe do
    next: Ptr<mut Byte> := bytes.offset(1)
    value: Byte := bytes[1]
    words: Ptr<mut UInt32> := bytes.cast<UInt32>()
end
```

All three operations require an enclosing `unsafe do ... end` block. They map
directly to C23 pointer arithmetic, indexing, and casts. Hexal adds no bounds,
provenance, lifetime, alignment, ownership, or allocation metadata.

## Goals

- Make low-level address traversal and representation reinterpretation
  expressible without adding pointer arithmetic operators.
- Keep safe Hexal free of raw pointer arithmetic and casts.
- Preserve `Ptr<T>` versus `Ptr<mut T>` access mode exactly.
- Generate plain, readable C23 with no compiler-owned helper or runtime cost.
- Keep foreign allocation and deallocation policy in RFC 0039.

## Non-goals

- Integer-to-pointer or pointer-to-integer conversion.
- Pointer subtraction, ordering, signed offsets, or backward traversal.
- Bounds-checked pointer arithmetic; use `Slice<T>` for checked ranges.
- Pointer ownership, automatic cleanup, lifetime tracking, or provenance
  tracking.
- Converting `Ptr<T>` to `Ptr<mut T>`.
- Pointer arithmetic over incomplete or erased pointee types.
- User-defined methods on pointer types or operator overloading.

## Surface

```text
Ptr<T>.offset(count: Size) -> Ptr<T>
Ptr<mut T>.offset(count: Size) -> Ptr<mut T>
Ptr<T>.cast<U>() -> Ptr<U>
Ptr<mut T>.cast<U>() -> Ptr<mut U>
Ptr<T>[index: Size] -> read-only place T
Ptr<mut T>[index: Size] -> writable place T
```

- `offset` is non-mutating. It returns a new pointer and never changes the
  receiver binding.
- `count` and `index` are forward-only `Size` values in this version.
- Indexing produces a place. Reading it copies T; assignment is permitted only
  through `Ptr<mut T>`.
- Pointer indexing is rejected when T is itself Array, Slice, or List. Use
  `(^pointer)[index]` to index the pointed-to collection, or `offset(index)` to
  advance between collection objects. This avoids making `pointer[index]` look
  like ordinary collection indexing while meaning "the next collection".
- `cast<U>()` changes only the pointee type. It preserves the outer access mode
  and therefore cannot upgrade read-only access.
- U must be a type permitted as a Ptr pointee under the existing Ptr rules.
  The cast does not require T and U to have the same size or alignment.
- A cast to or from an erased or incomplete pointee is permitted, because the
  cast itself does not inspect an object. `offset`, indexing, and `^` remain
  invalid until the pointer has a complete pointee type.
- No implicit pointer cast is introduced. Existing exact pointer identity and
  outermost `Ptr<mut T>` to `Ptr<T>` weakening remain the only implicit pointer
  conversions.

## Unsafe contract

Every `offset`, pointer index, and pointer cast requires active lexical unsafe
permission from RFC 0155.

The programmer asserts all facts C requires but the checker cannot prove:

- `offset` and indexing stay within one live C array object or produce exactly
  its one-past pointer;
- every dereferenced result is not one-past and names a live T object;
- the address satisfies the target pointee's alignment;
- the storage is initialized before reading;
- a cast followed by access respects the C object-representation and effective-
  type rules;
- the pointer remains valid for every later use.

Violating one of these assertions may cause undefined behavior. Unsafe
permission does not suppress ordinary type, nullability, use-after-free, or
mutability diagnostics that the compiler can still prove.

## Evaluation

- Receiver and arguments are evaluated exactly once.
- Normal call/index evaluation order applies: receiver first, then count or
  index.
- `pointer[index]` has the same address meaning as `^pointer.offset(index)` but
  does not construct a separately observable intermediate pointer.
- Producing one-past is allowed. Indexing or dereferencing one-past is not.
- C pointer arithmetic does not wrap. Overflow or movement outside the same
  array object violates the unsafe contract.
- A nullable pointer must be narrowed before any of these operations. Unsafe
  does not make Nil a valid address.

## C23 lowering

For a pointer expression `p` and a Size expression `n`:

```c
p + n
p[n]
(target_pointer_type)p
```

- Parentheses are added only as required by surrounding C precedence.
- `Ptr<T>` lowers with pointee `const`; `Ptr<mut T>` does not. A cast preserves
  that qualification according to the result Ptr mode.
- No helper, checked arithmetic formula, trap, metadata, or runtime component
  is emitted solely for these operations.
- Existing source mapping identifies the Hexal operation.

## Diagnostics

- `offset` outside unsafe reports Type Error:
  `Ptr.offset requires an unsafe do ... end block`.
- pointer indexing outside unsafe reports Type Error:
  `pointer indexing requires an unsafe do ... end block`.
- `cast` outside unsafe reports Type Error:
  `Ptr.cast requires an unsafe do ... end block`.
- Writing through indexed `Ptr<T>` reports the existing read-only pointer
  diagnostic.
- Arithmetic or indexing on an incomplete pointee reports Type Error:
  `pointer arithmetic requires a complete pointee type; got <type>`.
- Indexing a pointer whose pointee is Array, Slice, or List reports Type Error:
  `pointer indexing of <type> is ambiguous; use (^pointer)[index] to index the collection or pointer.offset(index) to advance the pointer`.
- A nullable receiver, wrong Size argument, invalid Ptr element, or use of
  released storage keeps its existing earlier diagnostic.

## Required sweep

Inventory and reconcile:

- parser indexing and generic-call parsing, including `cast<U>()`;
- checked expression and place representations;
- pointer element eligibility, nullability, access-mode weakening, and freed-
  state checks;
- checker and generator dispatch for every new checked node;
- constant and non-constant Size operands and evaluation-order preservation;
- generated pointer spelling for nested, erased, imported, and nominal types;
- pointer tests, Slice bridge tests, snippets, and manifest entries; and
- `docs/reference.md` grammar, pointer semantics, unsafe consumers, and C23
  lowering after explicit approval.

Do not add a generic conversion system, an integer-address type, a foreign
deallocator, or runtime provenance support while implementing this RFC.

## Validation

This section is exhaustive.

- `offset` accepts `Ptr<T>` and `Ptr<mut T>` inside unsafe and preserves the
  exact access mode.
- The same calls fail outside unsafe with the exact diagnostic above.
- Pointer indexing reads through both modes inside unsafe.
- Pointer indexing of `Ptr<Array<T, N>>`, `Ptr<Slice<T>>`, and `Ptr<List<T>>`
  is rejected with the exact ambiguity diagnostic; `(^pointer)[index]` retains
  ordinary collection bounds checks and `pointer.offset(index)` retains raw
  pointer-offset semantics.
- Assignment through indexed `Ptr<mut T>` succeeds; assignment through indexed
  `Ptr<T>` retains the read-only diagnostic.
- Pointer indexing fails outside unsafe with the exact diagnostic above.
- `cast<U>()` accepts both pointer modes inside unsafe, changes the pointee to
  U, and preserves the outer access mode.
- `cast<U>()` fails outside unsafe with the exact diagnostic above.
- A cast on `Ptr<mut T>` has exact result `Ptr<mut U>`; that result may later
  use the existing outermost weakening to `Ptr<U>`. A cast on `Ptr<T>` never
  produces `Ptr<mut U>`.
- Casting to and from `Ptr<Unknown>` succeeds inside unsafe, but offset,
  indexing, and dereference remain rejected while the pointee is Unknown.
- Pointer arithmetic/indexing on any other incomplete pointee uses the exact
  complete-pointee diagnostic.
- Nullable pointers require ordinary narrowing before offset, index, or cast.
- Count/index requires Size; no signed or implicit numeric operand is admitted.
- Receiver and operand side effects occur once and in source order.
- A one-past result may be formed and compared for equality but is never
  accepted as a valid dereference by any new static claim; runtime validity
  remains the unsafe assertion.
- Existing `^`, `@`, pointer equality, outermost weakening, Slice indexing,
  and `Slice.from_pointer` behavior remain unchanged.
- Generated C contains direct `+`, `[]`, and cast expressions with no new
  helper or runtime component.
- Existing snippet hashes do not move. New focused snippets add only their own
  manifest entries.
- Ordinary and tagged C23 suites pass.

## Detailed implementation plan

### Phase 1: baseline and checked forms

1. Record pointer parser, checker, generator, test, snippet, and manifest
   baselines after RFC 0155.
2. Add distinct checked nodes for pointer offset, pointer indexing as a place,
   and pointer cast; do not infer them from rendered method names later.
3. Keep every new dispatch fail-closed.

### Phase 2: checking

1. Recognize only the exact compiler-owned `offset` and `cast` surfaces on Ptr.
2. Reuse existing Ptr pointee, mode, nullability, and released-state facts.
3. Require lexical unsafe permission after ordinary receiver/type resolution.
4. Require Size for offset/index and a complete pointee for offset/index.
5. Reject pointer-index syntax for Array, Slice, and List pointees while
   retaining explicit dereference-then-index and `offset` as the two distinct
   operations.
6. Preserve mode through offset/cast and place writability through index.
7. Permit erased/incomplete types only at the cast boundary; do not permit
   dereference or arithmetic until the result pointee is complete.

### Phase 3: lowering

1. Render direct C pointer addition, indexing, and casts with each operand
   evaluated once.
2. Reuse the canonical nested pointer spelling so C `const` qualification is
   preserved at the correct type layer.
3. Emit no helper or runtime component solely for this feature.
4. Add text assertions for exact readable lowering and source mapping.

### Phase 4: conformance

1. Implement every Validation case in focused parser/checker and integration
   tests.
2. Add compact workbench snippets only for genuinely new surfaces and update
   only their manifest entries.
3. Run ordinary and tagged C23 suites and review the manifest diff.
4. Synchronize `docs/reference.md` only after behavior stabilizes and the user
   explicitly approves that edit.
5. Update status and close only after code, tests, generated C, and canonical
   documentation agree.
6. Rebuild and restart the workbench before handoff.

## Open questions

None.
