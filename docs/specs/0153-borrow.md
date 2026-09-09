# RFC 0153: `Borrow<T>` and `Borrow<mut T>`

- Kind: Language Semantics
- Status: Implementation-ready; implementation not started. Sequenced
  second in the 0154 -> 0153 -> 0149 chain: depends on RFC 0154's Core for
  the `<mut T>` grammar, owes nothing to RFC 0149
- Created: 2026-09-09
- Updated: 2026-09-09
- Origin: user decision to revisit RFC 0150's Open Decision 1 the other way:
  named generic types instead of bracket grammar, specifically so the name
  can carry more capability later — raised alongside, but distinct from, the
  still-unresolved question of whether `Borrow` eventually gains full
  borrow-checking (see Non-goals)
- Renamed by RFC 0154: originally drafted as `View<T>`/`MutView<T>`; renamed
  to `Borrow<T>`/`Borrow<mut T>` to fit the unified `<T>`/`<mut T>` mutability
  convention RFC 0154 establishes across `Ptr`, `Ref`, and this type. This
  document already reflects that renaming — the content below was never
  implemented under the old names, so this is the current design, not a
  historical record of one
- Supersedes: RFC 0150 (`[]T`/`[mut]T` bracket grammar). Keeps 0150's actual
  substance almost entirely — representation, construction rules, weakening,
  the kept return-safety check, the Migration destinations — changing only
  the spelling and what that spelling implies about future extensibility
- Coordinates with: RFC 0149 (its "Extension: block-scoped references"
  section is the current best answer for the storable-borrow ergonomics a
  named `Borrow` might eventually want, without paying for general lifetime
  tracking), RFC 0137 (superseded return-safety origin)
- Does not own: whether `Borrow<T>`/`Borrow<mut T>` ever gain the fuller
  Aliasing-XOR-Mutability / Parent-Lifetime-Bound model discussed alongside
  this RFC — that remains a separate, open, undecided question. Choosing a
  nameable type here makes that extension *possible* later; it does not
  decide it now

## Summary

`Borrow<T>` (read-only) and `Borrow<mut T>` (mutable) are ordinary generic
types, resolved through the same name-lookup path `List<T>`/`Ptr<T>` already
use — reversing RFC 0150's choice to make slices dedicated bracket grammar
specifically to deny them a name. The representation, construction
semantics, weakening rule, and the one safety check RFC 0150 kept are
unchanged from that RFC; only the spelling changes, and with it, a
reopened door: a named type can have methods and capabilities added to it
later without inventing new grammar, which bracket syntax structurally
cannot. Whether anything is actually hung off that door — a fuller borrow
checker, RFC 0149's block-scoped `with` extension, or nothing at all beyond
what's specified here — is not decided by this RFC.

## Naming and syntax

```text
Borrow<T>       read-only borrow of a contiguous range: non-owning, non-null, {pointer, length}
Borrow<mut T>   mutable borrow of a contiguous range: the same shape, writable through the pointer
```

- Ordinary angle-bracket generic syntax, the same form `Array<T, N>`,
  `List<T>`, and `Ptr<T>`/`Ptr<mut T>` already use. `Borrow` is a protected
  type name, resolved by lookup like any other built-in generic constructor
  — there is no bracket-grammar special case to parse.
- `mut` marks the writable variant as a modifier on the type argument itself
  (`Borrow<mut T>`), per RFC 0154's unified convention across `Ptr`, `Ref`,
  and `Borrow` — not a differently-spelled type name. This replaces what an
  earlier draft of this RFC called `MutView<T>`.
- RFC 0151 (`[N]T` fixed-array spelling replacing `Array<T, N>`) remains
  paused and not in effect; this RFC is written against today's actual
  `Array<T, N>`, not 0151's proposal.

## Semantics

1. **Grammar.** `Borrow<T>`/`Borrow<mut T>` are resolved exactly like
   `List<T>`: one name, one type argument (optionally `mut`-marked), looked
   up through the ordinary generic-type path. `T` follows the same
   inline-element eligibility this type used under its earlier name
   (`Eligible(T, PositionBorrowElement)`). Borrows of borrows, and borrows of
   other managed/owning values (`String`, `List`, `Dict`), are rejected —
   unchanged from every prior draft of this rule.

2. **Representation.** `Borrow<T>` lowers to `{ const T* data; size_t
   length; }`; `Borrow<mut T>` lowers to `{ T* data; size_t length; }`. One
   monomorphized C struct per instantiated T either way, generated names
   `hex_borrow_*`/`hex_mut_borrow_*`.

3. **Construction.**
   - `Array<T, N>.slice(start: Integer, end: Integer) -> Borrow<T>` (today's
     existing signature, unchanged in behavior).
   - `Array<T, N>.slice_mut(start: Integer, end: Integer) -> Borrow<mut T>`,
     valid only on a `mut` receiver — the capability RFC 0148 first
     identified as missing, carried forward through every draft since.
   - `List<T>.slice`/`.slice_mut` mirror Array's, same mutability gating.
   - `Borrow<T>.from_pointer(pointer: Ptr<T> | Ptr<mut T>, length: Size) ->
     Borrow<T>` and `Borrow<mut T>.from_pointer(pointer: Ptr<mut T>, length:
     Size) -> Borrow<mut T>` — a static call against the type name, since
     `Borrow` is a lookup-able identifier (RFC 0150's `Ptr<T>.to_slice()`
     workaround existed only because bracket grammar wasn't callable
     against, which doesn't apply here). Same preconditions carried through
     every prior draft: the pointer must be statically non-null, and a
     pointer that traces to a local `ref` within the same function is
     rejected (`fromPointerRefTrace`, unchanged).
   - `Borrow<T>.empty() -> Borrow<T>` and `Borrow<mut T>.empty() ->
     Borrow<mut T>`.

4. **Indexing and re-slicing.**
   - `Borrow<T>[index: Integer] -> read-only-place<T>`; `Borrow<mut
     T>[index: Integer] -> place<T>`.
   - `Borrow<T>.slice(start, end) -> Borrow<T>`; `Borrow<mut T>.slice(start,
     end) -> Borrow<mut T>` (re-slicing preserves the receiver's own
     mutability — no separate `slice_mut` needed here, unlike `Array`/
     `List`'s construction methods, because dispatch is on the borrowed
     value's own type, not a separate binding-mutability signal).
   - `Borrow<T>.length() -> Size`; same for `Borrow<mut T>`.

5. **Weakening.** `Borrow<mut T>` weakens implicitly to `Borrow<T>` at the
   outermost layer only, no upgrade, no nested weakening — the same rule
   already stated for `Ptr<mut T>` -> `Ptr<T>` (RFC 0154), unchanged in
   substance from every prior draft of this type.

6. **Return safety (kept, narrowly).** The existing direct-local-root
   return-safety machinery (already generalized once this session, from
   this type only to also cover `Ptr`) extends to `Borrow<mut T>`
   directly-returned values the same way it already covers `Borrow<T>`: a
   borrow built from `ref`-adjacent local storage within the same function
   and returned directly is rejected. Whether the underlying Go identifiers
   in the checker (currently `ViewRoots`/`RootKind`/`viewReturnDiagnostic`)
   get renamed to match is an implementation-detail question, not a
   language-design one — they are not user-visible either way. This
   remains the only safety net either type gets by default: not sound, a
   borrow returned inside an object/ADT/Array, stored in a `List`, or passed
   across a `Task` boundary is not tracked, matching how `Ptr` is not
   tracked in those positions either.

7. **Equality, printing, storability.** Unchanged from every prior draft:
   length-then-elements equality; printable when the element is; general
   storability positions (binding, object member, ADT payload, Array
   element, List element, Dict value, function parameter/result, Task
   argument/result, Channel element) match the existing position matrix
   exactly.

## Migration

Identical destinations to RFC 0150's Migration table, respelled:

```text
Array<T,N>.slice(start, end)      -> Borrow<T>       (unchanged signature)
Array<T,N>.slice_mut(start, end)  -> Borrow<mut T>    (new, RFC 0148's gap)
List<T>.slice / .slice_mut                            (same pattern)
String.bytes()                    -> Borrow<Byte>     (unchanged behavior)
String.slice(start, end)          -> Borrow<Byte>     (unchanged behavior)
String.from_bytes(heap, bytes: Borrow<Byte>)          (unchanged behavior)
String.from_runes(heap, runes: Borrow<Rune>)          (unchanged behavior)
IO.write(from: Borrow<Byte>)                          (unchanged behavior)
Ptr<mut Bytes>.write(from: Borrow<Byte>)              (unchanged behavior)
```

Two entries are genuinely new rather than respellings: `Array<T,N>.slice_mut`
and `List<T>.slice_mut`, both returning `Borrow<mut T>` and both valid only
on a `mut` receiver (Semantics 3). Every other entry is an existing
read-only signature under a new name, unchanged in direction and capability.
An earlier draft of this table claimed nothing here needed `Borrow<mut T>`,
which contradicted its own two `slice_mut` rows.

This is also not a documentation-only migration. `View<T>` is shipped --
`ViewType`, the `hex_view_*` component family, `hexal/view.h`, and every
signature above -- so implementing this RFC renames a live type through the
checker, the generator, `docs/reference.md`, and the snippet catalog, with one
manifest rebaseline. RFC 0154's `Ptr`/`Ptr<mut T>` rename touches the same
files and is sequenced into the same pass.

## Non-goals

- **The fuller ownership/borrow model** (Aliasing XOR Mutability, Parent
  Lifetime Bound, No Field Moves While Borrowed) discussed alongside this
  RFC. Adopting a nameable type is necessary groundwork for that model to
  ever attach cleanly, but this RFC does not adopt it. If it's wanted, it
  needs its own spec built on top of this one, with the actual lifetime
  computation and move/borrow coupling that discussion identified as the
  real cost — see RFC 0149's "Extension: block-scoped references" for the
  cheaper, call/block-scoped alternative currently on the table instead.
- RFC 0137's nested-aggregate return walk. A borrow hidden inside a returned
  object/ADT/union/Array is not tracked, same as `Ptr` today.
- Any List/Dict-mediated, pointer-pointee-mediated, interprocedural, or
  cross-Task escape tracking — matches `Ptr`'s current (non-)coverage, not
  RFC 0110/0118's eventual scope.
- Anything from RFC 0151 (`[N]T` fixed-array spelling) — still paused, not
  in effect.
- Reopening RFC 0149's Box/Ref status beyond the block-scoped extension
  already added there; it otherwise stays an open, undecided draft.

## Open decisions

None. Every design question this RFC's own scope raises is resolved above;
the one adjacent question (the fuller borrow-checking model) belongs to a
different, not-yet-written RFC by its own Non-goals entry, not to this one.

One consequence is worth stating rather than leaving to inference. Because
this RFC adopts no aliasing model (see Non-goals), two overlapping
`Borrow<mut T>` values over the same storage are permitted: `slice_mut(0, 4)`
and `slice_mut(2, 6)` may coexist and alias. That matches `Ptr<mut T>`'s
current behaviour exactly and is not a regression, but a named type invites
the assumption that it carries more guarantee than a raw pointer, so the
absence is recorded here.
