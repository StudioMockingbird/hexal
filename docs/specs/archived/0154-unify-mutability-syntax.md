# RFC 0154: Unify Mutability Syntax as `<T>` / `<mut T>`

- Kind: Language Semantics
- Status: Superseded by RFC 0161 on its implementation, 2026-09-10. RFC 0161 adopts
  the `<T>` / `<mut T>` spelling for `Ptr` and `Slice` with none of the call/lexical
  capability semantics this RFC sequenced. Do not implement.
- Created: 2026-09-09
- Updated: 2026-09-10
- Origin: user decision to replace the `Mut`-prefix naming convention
  (`MutPtr`, `MutRef`, `MutView`) with a single, uniform pattern: the plain
  name takes `T`, the writable variant takes `mut T` as the same type
  argument — "this makes the semantics more clear"
- Renames: `View<T>`/`MutView<T>` (RFC 0153) to `Slice<T>`/`Slice<mut T>`;
  `Ref<T>`/`MutRef<T>` (RFC 0149) to `Ref<T>`/`Ref<mut T>`;
  `Ptr<T>`/`MutPtr<T>` (implemented today) to `Ptr<T>`/`Ptr<mut T>`. All
  three spec-text renames are applied. `Ref` needs no code change; `Slice`
  and `Ptr` both do, and land together (see Scope)
- Supersedes: RFC 0153's capability naming only; RFC 0153 separately changes
  View placement and lifetime to the selected scoped model
- Amends: RFC 0149 by renaming `MutRef<T>` to `Ref<mut T>` throughout
- No dependency on RFC 0149 or RFC 0153's semantics: this RFC defines only the
  shared capability-type spelling

## Scope

This RFC has two pieces. Both are confirmed; they differ in *when*, not
*whether*.

1. **The grammar convention** (Summary and Grammar below): `<T>`/`<mut T>`
   itself, and its application to `Ref` (RFC 0149) and `Slice` (RFC 0153).
   Zero cost beyond parser grammar. It requires only the settled type names.
2. **The `Ptr<T>`/`Ptr<mut T>` migration** (Migration below): renaming the
   real, implemented `Ptr<T>`/`MutPtr<T>`.

**Cost is not uniform across the three renames, but it is not the two-tier
split an earlier draft of this RFC assumed.** That draft treated `Ref` and
`Slice` alike as free documentation edits and `Ptr` alone as real migration.
That is wrong for `Slice`:

- **`Ref` is genuinely free.** RFC 0149 has never been implemented and `Ref`
  exists in no code. Renaming it was a find-and-replace in one spec document.
- **`Slice` is not free.** RFC 0153 has never been implemented *as a
  document*, but its subject has: `View<T>` is shipped today — the `ViewType`
  constructor, `hex_view_*` generated components, `hexal/view.h`, and every
  `String.bytes()`, `.slice()`, and `IO.write()` signature that names it.
  RFC 0153 renames generated C to `hex_slice_*`/`hex_mut_slice_*`.
  Implementing it is a shipped-type migration.
- **`Ptr` is not free either**, for the same reasons, in the same files.

Because `Slice` and `Ptr` are the same class of change touching the same
places — parser, checker, generator names, every snippet, `docs/reference.md`,
one manifest rebaseline — they are done in **one migration pass**, sequenced
with RFC 0153. Splitting them would pay that cost twice over the same files.

## Summary

One convention replaces three ad hoc `Mut`-prefixed names: for any type
family in this group, the plain form `Name<T>` is read-only/non-owning over
`T`, and the writable form is the *same* type constructor with `mut` marking
the type argument itself — `Name<mut T>` — rather than a differently-spelled
type. This applies to exactly three families, all of which exist to grant
some capability *over* a target rather than to own or contain one:

```text
Ptr<T>       / Ptr<mut T>       (was Ptr<T> / MutPtr<T>)
Ref<T>       / Ref<mut T>       (was Ref<T> / MutRef<T>, RFC 0149)
Slice<T>     / Slice<mut T>     (was View<T> / MutView<T>, RFC 0153)
```

It does not apply to `Array<T, N>`, `List<T>`, `Dict<K, V>`, or any other
owning/containing generic — those already govern mutability through the
*binding's* own `mut`-ness, not the type argument, and this RFC does not
change that split.

## Grammar

`mut` becomes valid immediately before a type argument, for exactly these
three constructors:

```ebnf
ptr-type-argument   = "mut" , type-expression | type-expression ;
ref-type-argument   = "mut" , type-expression | type-expression ;
slice-type-argument = "mut" , type-expression | type-expression ;
```

This is the same shape of extension RFC 0150 already used for `[mut]T`
(a keyword immediately preceding what follows, resolved before the parser
descends into the type expression itself) — `Ptr<mut T>` resolves `mut`
before parsing `T`, the same way. It is not a general grammar change: `mut`
inside `<...>` is meaningless and rejected for `Array`, `List`, `Dict`, and
every other generic constructor's type arguments.

Nesting is unchanged in behavior, only spelling: today's `Ptr<MutPtr<Int32>>`
(a read-only pointer to a writable pointer) becomes `Ptr<Ptr<mut Int32>>`.
Weakening still applies at the outermost layer only, no upgrade, no nested
weakening — `Ptr<mut T>` weakens to `Ptr<T>` exactly where `MutPtr<T>`
weakened to `Ptr<T>` before this RFC, and nowhere else.

## Migration

### `Ref<T>` / `Ref<mut T>` (RFC 0149) — free, already applied

RFC 0149 has never been implemented. Its document uses `Ref<T>` and
`Ref<mut T>` throughout. Both forms are parameter/receiver or lexical-block
capabilities; RFC 0149 owns the `borrow ... do ... end` statement independently
of this spelling-only RFC.

### `Slice<T>` / `Slice<mut T>` (RFC 0153) — spec text applied, code migration real

The *document* edit is done: every `MutView<T>` in RFC 0153 now reads
`Slice<mut T>`, every `View<T>` reads `Slice<T>`, across its text and its
Migration table's destinations (`String.bytes() -> Slice<Byte>`, etc.).
RFC 0153 additionally makes both Slice modes call- or lexical-block-scoped; that
placement change belongs to RFC 0153 rather than this spelling-only RFC.

The *implementation* is not free, and an earlier draft of this RFC wrongly
said it was. `View<T>` is shipped: `ViewType` in `compiler/types`, the
`hex_view_*` component family, `hexal/view.h`, and its appearances throughout
`docs/reference.md` and the snippet catalog. Renaming it carries the same
sweep as `Ptr` below, over largely the same files, which is why the two land
together.

### `Ptr<T>` / `Ptr<mut T>` — not free

`Ptr<T>`/`MutPtr<T>` are real today: parsed, checked, and code-generated,
with working programs depending on the current spelling. This needs the
same kind of required sweep RFC 0151 wrote for its own real-code migrations,
not just a text edit:

- parser: type-expression grammar for `Ptr<...>` gains the `mut`-argument
  form; `MutPtr` stops being recognized as a distinct name;
- checker: every `MutPtrType(...)` construction site, every
  `PointeeWritable` check, every place currently branching on "is this
  spelled `MutPtr`" moves to "does this `Ptr<...>` argument carry `mut`";
  `checkReference` (`ref place` yielding `MutPtr<T>` for a writable place)
  now yields `Ptr<mut T>`;
- generator: `hex_ptr_*`/`hex_mut_ptr_*` naming, if any exists per-element
  today, is unaffected in representation (still `const T*` vs `T*`), only
  in which source spelling selects which;
- every existing test, integration fixture, and workbench snippet that
  writes `MutPtr<T>` needs updating to `Ptr<mut T>`;
- `docs/reference.md`'s Pointers section, and every other reference to
  `MutPtr` throughout it (the Ptr/MutPtr pointee-exclusion list, the
  weakening rule text, `Heap.allocate`/`.free` signatures, `Stash`/`Pool`
  signatures, `read_volatile`/`write_volatile`, byte-conversion signatures —
  `MutPtr` appears pervasively, this is not a one- or two-line change);
- this session's own recent work is directly affected: the `Ptr`/`MutPtr`
  direct-local-root return check (`ptrReturnDiagnostic`) branches on
  `value.typ.PointeeWritable` already, which is representation-based, not
  name-based, so that specific check likely needs no logic change — only
  the diagnostic text ("a MutPtr cannot be returned..." becomes "a
  Ptr<mut T> cannot be returned...", or similar).

This is a real, compiler-wide rename, not a documentation pass. Treat it
with the same weight as RFC 0151's `Strand`/`Array<T,N>` migration, not as
"the same kind of change" as the other two renames in this RFC.

## Non-goals

- Extending `mut`-inside-`<...>` to `Array<T, N>`, `List<T>`, `Dict<K, V>`,
  `Task<R>`, `Channel<T>`, `Stash<T>`, or `Pool<T>` — these govern
  mutability through the binding, not the type argument, and nothing about
  this RFC's reasoning (uniform spelling for capability-over-a-target types)
  applies to owning/containing types.
- Any change to what `Ptr`, `Ref`, or `Slice` actually mean, check, or
  generate as C — this RFC is spelling-only for all three.
- Any lifetime model beyond RFC 0153's and RFC 0149's scoped capability
  rules.

## Validation

This section is exhaustive.

- `Ptr<T>`, `Ptr<mut T>`, `Ref<T>`, `Ref<mut T>`, `Slice<T>`, and
  `Slice<mut T>` parse with exactly one complete T.
- `mut` inside generic arguments is rejected for every other constructor.
- Writable-to-read-only weakening remains outermost-only; upgrades and nested
  weakening fail.
- Every accepted former `MutPtr<T>` program compiles after the mechanical
  `Ptr<mut T>` rewrite with equivalent checked behavior and C output.
- `MutPtr`, `MutRef`, `View`, and `MutView` are rejected as unknown type
  names. `MutSlice` was never a Hexal type name and receives the ordinary
  unknown-type diagnostic if written.
- Diagnostics use source-level unified spellings and no active snippet or
  canonical document retains the removed names after implementation.
- Manifest hashes move only where the source spelling or deliberate Slice
  migration changes generated artifacts.

## Detailed implementation plan

1. Extend capability-type parsing so only Ptr, Ref, and Slice consume optional
   `mut` before their single type argument.
2. Preserve one internal capability identity plus a writability flag; internal
   Go helper names may remain when accurate and non-user-visible.
3. Rewrite Ptr construction, weakening, eligibility, diagnostics, and source
   formatting from the separate MutPtr spelling to the writability flag.
4. Perform the test/snippet/source migration in the same implementation unit as
   RFCs 0149 and 0153; remove every active source use of the old names.
5. Verify equivalent generated C representation and update only intentional
   manifest entries.
6. Run ordinary and tagged C23 suites. Once behavior stabilizes, synchronize
   `docs/reference.md` only with explicit user approval; then update status and
   close.

## Open decisions

None. The one decision this RFC carried — whether `Ptr<T>`/`Ptr<mut T>` was
wanted given its migration cost — is resolved: yes, sequenced into RFC 0153's
migration pass. The question was posed as though `Slice` were free and `Ptr`
alone were expensive; both are shipped-type migrations over the same files, so
doing them separately would pay that cost twice rather than saving it once.
