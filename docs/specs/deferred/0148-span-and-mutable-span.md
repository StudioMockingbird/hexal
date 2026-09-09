# RFC 0148: `Span<T>` and `MutSpan<T>`

- Kind: Language Semantics
- Status: Superseded by RFC 0150; not implemented
- Created: 2026-09-08
- Updated: 2026-09-08
- Superseded because: an attempt to remove `View<T>` outright (made and
  reverted the same day) surfaced that `IO.write`, `MutPtr<Bytes>.write`, and
  `String.bytes`/`.slice`/`.from_bytes`/`.from_runes` all depend on it with no
  replacement in this RFC's design, and separately, comparing how C, Zig, and
  Odin represent a borrowed byte range settled on a different shape entirely:
  dedicated slice grammar (`[]T`/`[mut]T`) rather than a generic type
  constructor. RFC 0150 also drops this RFC's Semantics 5 exclusivity
  checker outright — bolting lifetime/aliasing tracking onto an ordinary
  storable type was the position that produced RFC 0150 in the first place.
  This document is kept for its naming discussion (`Reader`/`Writer` rejected)
  and the `Mut`-prefix-marks-writable precedent, both of which RFC 0150 reuses.
- Origin: user request, following the `View<T>` design discussion that preceded
  this RFC
- Renames: `View<T>` to `Span<T>`, mechanical and behavior-preserving
- Coordinates with: RFC 0137 (return-safety machinery this RFC extends), RFC
  0110 (owns heap/pointer/List/Dict-mediated aliasing), RFC 0118 (owns
  Task/Channel transfer aliasing)
- Does not own: interprocedural provenance, heap/pointer/List/Dict aliasing,
  cross-task aliasing, runtime borrow tracking, new lifetime/borrow syntax

## Summary

Rename `View<T>` to `Span<T>` and add `MutSpan<T>`: a non-owning, non-null,
writable, contiguous pointer-length descriptor. `MutSpan<T>` weakens
implicitly to `Span<T>` at the outermost layer only, exactly like `MutPtr<T>`
weakens to `Ptr<T>`. `Array<T,N>.slice_mut` and `List<T>.slice_mut` construct
one from a `mut` receiver.

Read-only aliasing is always safe, which is why `Span<T>` today has no
exclusivity rule at all — any number of Spans over the same storage can
coexist. Introducing a *writable* span is not just "the same struct with a
non-const pointer": it introduces a genuinely new hazard that Ptr/MutPtr
mostly sidestep because a raw pointer has no bundled length and is normally
used once, locally, not stored and re-sliced. This RFC's real content is
Semantics 5, the exclusivity rule — everything else is mechanical.

## Naming

The user proposed `Reader<T>`/`Writer<T>` in place of `View`/`MutView`.
Recommend against it: Hexal already has a real, load-bearing "byte stream"
concept (`docs/reference.md` § Byte streams; descriptor and memory streams
are implemented per RFC 0110/0118's status notes), and `Reader`/`Writer` is
the name every mainstream language (Go's `io.Reader`/`io.Writer`, Rust's
`Read`/`Write`, Java's `Reader`/`Writer`) uses for exactly that: a
*sequential*, *stateful*, cursor-and-partial-progress byte interface, not a
*random-access* window over memory already in hand. Hexal's own network work
(RFC 0144/0145) will very likely want a generic Reader/Writer-shaped
abstraction over sockets and streams later. Spending the name now on a
span type would collide with that future use in spirit even where the
token doesn't collide today, and it is the kind of name that is expensive to
walk back once `Reader<T>`/`Writer<T>` methods (`.read(into:, max:)`) exist
elsewhere with different semantics.

Recommended: **`Span<T>` / `MutSpan<T>`**. This is the C#/C++20 term for
exactly this concept (`Span<T>`/`ReadOnlySpan<T>`, `std::span`), it pairs
cleanly with the existing `Ptr`/`MutPtr` naming convention (plain name is
read-only, `Mut`-prefix is writable), and it does not collide with anything
else in `docs/reference.md`.

Alternative considered: `Slice<T>` / `MutSlice<T>` (Rust/Swift/Zig term).
Equally legitimate, marginally more familiar to a Rust-adjacent audience
given Hexal's borrow-flavored safety rules, but it reads oddly beside the
existing `.slice(start, end)` *method* name — `Slice<T>.slice(...)` is a noun
and verb collision that `Span<T>.slice(...)` doesn't have. Rejected for that
reason, not a strong one; easy to switch if the user prefers it.

Rejected: `Window`/`MutWindow` (collides in spirit with future
stream/temporal windowing), `Range`/`MutRange` (already strongly implies a
bare `start..end` interval, not a pointer into memory), `Extent` (not
idiomatic in any language this codebase draws from).

This spec is written using `Span`/`MutSpan`. Renaming later, before
implementation starts, is a mechanical find-replace across
`docs/reference.md`, the generator (`hex_view_*` → `hex_span_*`), the
checker, and the snippet catalog — cheap to defer, cheap to redo.

## Problem

Hexal has no way to hand a callee bounded write access to an existing
buffer without transferring ownership. Today the only options are:

1. Pass `MutPtr<T>` and a separate `Size`, unbundled — precisely the raw C
   idiom (`memcpy(dst, n)`-style) that `Span<T>` exists to avoid on the
   read side. Nothing ties the pointer and length together; a caller can
   pass a pointer and the wrong length.
2. Pass a `List<T>` handle, which transfers far more capability than
   "write these N elements" (growth, ownership, heap lifetime) and forces
   allocation even when the caller already has a fixed buffer (an `Array`,
   or storage reached through a parameter).

`MutSpan<T>` fills exactly the gap `Span<T>` filled on the read side.

## Semantics

1. **Rename.** `View<T>` becomes `Span<T>` everywhere: the type, its methods,
   generated C identifiers (`hex_view_*` → `hex_span_*`), diagnostics, and
   every reference in `docs/reference.md` and the snippet catalog. No
   behavior changes.

2. **`MutSpan<T>` methods**, mirroring `Span<T>`:

   ```text
   MutSpan<T>.from_pointer(pointer: MutPtr<T>, length: Size) -> MutSpan<T>
   MutSpan<T>.empty() -> MutSpan<T>
   MutSpan<T>.length() -> Size
   MutSpan<T>[index: Integer] -> place<T>
   MutSpan<T>.slice(start: Integer, end: Integer) -> MutSpan<T>
   ```

   `from_pointer` accepts only `MutPtr<T>` — a `MutSpan` cannot be
   manufactured from a `Ptr<T>`, the same way `ref` on a fixed place cannot
   yield a `MutPtr`. Indexing yields `place<T>` (writable), not
   `read-only-place<T>`.

3. **Construction from a receiver requires `mut`**, mirroring every other
   mutating collection operation:

   ```text
   Array<T,N>.slice_mut(start: Integer, end: Integer) -> MutSpan<T>
   List<T>.slice_mut(start: Integer, end: Integer) -> MutSpan<T>
   ```

   Named `slice_mut`, not an overload of `slice`, because the two forms
   aren't distinguished by any argument type — only by the receiver's own
   mutability, and Hexal's overloading (e.g. `Heap.free<T>(Ptr<T>)` /
   `Heap.free<T>(MutPtr<T>)`) always dispatches on a parameter type, not the
   receiver's binding. This also matches Rust's own `split_at`/`split_at_mut`
   naming for the identical situation.

4. **Weakening.** `MutSpan<T>` weakens implicitly to `Span<T>` at the
   outermost layer only, no upgrade, no nested weakening — the exact rule
   `docs/reference.md` already states for `MutPtr<T>` → `Ptr<T>`.

5. **Exclusivity (the new part).** Read-only aliasing needs no rule: any
   number of `Span`s over the same storage are always safe, same as any
   number of `Ptr` aliases. A live `MutSpan` changes that, because it grants
   write access nothing else is allowed to observe or contend with while it
   is held.

   - Tracks the exact same "root" concept RFC 0137 already builds for
     `Span` return-safety (`ViewRoots`/`RootKind`): a local `Array`/`List`
     binding, `self`-reached storage, or a parameter's own inline storage.
     Roots reached through a heap allocation, a `Ptr`/`MutPtr` pointee, or
     `List`/`Dict` element storage are **not tracked** — same boundary RFC
     0137 already draws, deferred to RFC 0110 for the same reason.
   - Rule: while a `MutSpan` derived from tracked root `R` is live,
     constructing any other `Span` or `MutSpan` from `R` is rejected. While
     a `Span` derived from `R` is live, constructing a `MutSpan` from `R` is
     rejected. Construction order doesn't matter — whichever conflicting
     construction happens second is the one rejected.
   - Diagnostics: `"a Span cannot be constructed while a MutSpan over the
     same storage is live"` and `"a MutSpan cannot be constructed while
     another Span or MutSpan over the same storage is live"`.
   - "Live" means **last-use**, Rust NLL-style: a `MutSpan`'s hold ends at
     its last use (an index, a method call, or being passed as a call
     argument), not at its binding's enclosing block end. A `Span` or
     `MutSpan` over the same root can be constructed immediately after that
     last use, even earlier in lexical position than the end of the
     original binding's scope. Rejected lexical-scope-end as the simpler
     alternative: it would force awkward nested blocks for a common
     "mutate, then read back to verify" pattern within one function, likely
     pushing real programs back toward `MutPtr<T>` + a manual length and
     defeating the point. RFC 0137 already tracks per-binding flow state
     across branches for provenance root-sets; computing a liveness
     interval alongside it is an extension of that machinery, not a new
     pass.
   - Passing a `MutSpan` as a call argument counts as a use at the call
     site; nothing about what happens after the call returns is claimed,
     matching RFC 0137's opaque-call boundary (non-goal 6 there).
   - **Granularity is per-root, not per-range.** `arr.slice_mut(0, 2)` and
     `arr.slice_mut(2, 4)` do not overlap in memory but share a root, so v1
     rejects taking both live at once. This is a deliberate simplification,
     not an oversight — proving two independently-computed sub-ranges are
     disjoint is a materially harder analysis than proving they share a
     root. `ponytail: root-granularity exclusivity; upgrade path is a
     dedicated split_at_mut-style operation (Semantics 7) if disjoint
     concurrent sub-spans turn out to matter in practice.`

6. **Return safety.** Extend RFC 0137's nested-return provenance walk to
   also carry and check `MutSpan` roots. This is additive to work RFC 0137
   already plans, not a new analysis — a `MutSpan` is exactly as
   escape-prone as a `Span`, more urgently so since escaping one leaks a
   write capability into the caller's storage after that storage is gone.

## Implementation plan

### Phase 1: rename `View` to `Span`

1. Generator: rename `hex_view_*` C identifiers to `hex_span_*` in
   `compiler/generator/packages/view.h` and `views.go`/`view_component.go`
   (file names may stay or move to `span.h`/`spans.go` at implementation
   time — no behavioral stake either way).
2. Types/checker: rename `ViewInfo` (`compiler/types/collections.go`) and
   every `View`-named function, diagnostic string, and identifier that
   refers to the type, not to unrelated concepts (e.g. `viewpoint`-style
   false positives don't exist here, but check diagnostics text carefully).
3. Update `docs/reference.md`'s `View<T>` section heading and every
   cross-reference (`Array.slice`, `List.slice`, `Ptr`/`MutPtr` non-goals
   list, the Ptr/MutPtr pointee-exclusion note) to `Span<T>`.
4. Regenerate snippet hashes: every snippet using `View` changes its
   generated-C hash (identifier text only, structurally identical output),
   the same magnitude of diff as any prior identifier-rename pass.

### Phase 2: add `MutSpan<T>`

5. Add a `MutSpanInfo` type alongside the renamed `SpanInfo` in
   `compiler/types/collections.go`, same shape, tracking the writable
   capability.
6. Generator: emit `hex_mut_span_<Suffix>` as `{ T *data; size_t length; }`
   (non-const pointee, unlike `Span`'s `const T *data`), plus
   `hex_mut_span_at_*` (returns a non-const pointer) and
   `hex_mut_span_slice_*`, structurally mirroring the existing
   `hex_span_at_*`/`hex_span_slice_*` helpers.
7. Checker: type-check `MutSpan<T>` construction (`from_pointer` accepting
   only `MutPtr<T>`, `empty`, indexing yielding a writable place, `.slice`
   returning `MutSpan<T>`), and extend the existing `MutPtr<T>` → `Ptr<T>`
   weakening mechanism to also cover `MutSpan<T>` → `Span<T>`.
8. Add `Array<T,N>.slice_mut` and `List<T>.slice_mut`, each requiring a
   `mut` receiver, beside the existing `.slice` implementations.

### Phase 3: return safety

9. Extend RFC 0137's root-tracking metadata (renamed `SpanRoots`/`RootKind`
   per Phase 1) to also carry `MutSpan` values through the same recursive
   walk over object members, ADT payloads, union payloads, Array elements,
   bindings, and match/try results.
10. Extend the nested-return diagnostic to name `MutSpan` explicitly when
    it is the escaping component, keeping `Span`'s existing wording
    otherwise.

### Phase 4: exclusivity liveness tracking

11. Extend the per-binding flow state RFC 0137 already tracks (used there
    for provenance root-set merging across branches) to also compute a
    last-use liveness interval for any binding whose value carries a
    `Span`/`MutSpan` root. A use is: indexing, any method call on the
    value, or passing it as a call argument.
12. At each `Span`/`MutSpan` construction site (`.slice`, `.slice_mut`,
    `.from_pointer`), check the constructed value's tracked roots against
    any currently-live `MutSpan` hold over the same root (and, symmetrically,
    check a new `MutSpan`'s roots against any currently-live `Span` or
    `MutSpan` hold). Reject per Semantics 5 with the diagnostics specified
    there.
13. A rejected return (Phase 3) does not additionally need a liveness
    answer — return-safety and exclusivity are independent checks over the
    same root metadata.

### Phase 5: tests and documentation

14. Add a `MutSpan<T>` section to `docs/reference.md` documenting
    construction, weakening, `slice_mut`, and the exclusivity rule
    (including the root-granularity limitation).
15. Checker-unit tests for the exclusivity accept/reject matrix below, and
    return-safety tests extending RFC 0137's existing suite to `MutSpan`.
16. Generator tests asserting the renamed `hex_span_*` and new
    `hex_mut_span_*` C structs/helpers.
17. Regenerate snippet hashes for the Phase 1 rename, add new snippets
    exercising `MutSpan`/`slice_mut` to the catalog, and run the full
    catalog under `go test -tags c23 ./compiler/tests/c23validation` to
    confirm real-toolchain compilation.

## Validation

This section is exhaustive.

Accept:

- Two `Span`s over the same local `Array` or `mut` `List` coexisting.
- A `MutSpan`, fully used to its last use, followed by a `Span` constructed
  over the same root afterward (even before the original binding's
  lexical scope ends).
- A `MutSpan` passed as a call argument, with a new `Span` or `MutSpan`
  constructed over the same root after the call returns.
- `MutSpan<T>` weakened to `Span<T>` at a call boundary or assignment.
- `Array<T,N>.slice_mut` from a `mut` Array parameter.
- `List<T>.slice_mut` from a `mut` List handle.
- `MutSpan.from_pointer` given a `MutPtr<T>`.
- A `MutSpan` rooted in a parameter, `self`-reached storage, or
  `from_pointer` region, returned safely (mirrors RFC 0137's `Span`
  return-safety accept list).
- An empty `MutSpan` returned regardless of root.

Reject:

- A `Span` constructed while a `MutSpan` over the same tracked root is
  live.
- A `MutSpan` constructed while a `Span` or another `MutSpan` over the
  same tracked root is live.
- Two `MutSpan`s over the same tracked root live simultaneously, including
  disjoint sub-ranges (root-granularity, Semantics 5).
- `MutSpan.from_pointer` given a `Ptr<T>` (ordinary type error).
- `Array<T,N>.slice_mut` or `List<T>.slice_mut` on a non-`mut` receiver.
- A returned local-rooted `MutSpan`, directly or nested in an object, ADT,
  union, or Array (mirrors RFC 0137's `Span` reject list, extended to
  `MutSpan`).
- Constructing a `Span<T>` directly from a `MutSpan<T>` value's storage
  bypassing weakening (no such conversion exists beyond outermost-layer
  weakening).

Both:

- No accepted snippet hash changes beyond the Phase 1 rename (name-only
  diff; structurally identical output).
- `go test ./...` and `go vet ./...` pass.
- `go test -tags c23 ./compiler/tests/c23validation` passes for the full
  catalog plus new `MutSpan` snippets, compiling clean under GCC, Clang,
  and `zig cc`.

## Non-goals

- Heap-allocated storage, `Ptr`/`MutPtr` pointee storage, and
  `List`/`Dict` element-mediated aliasing — RFC 0110's problem.
- Cross-task/Channel transfer aliasing — RFC 0118's problem.
- Interprocedural aliasing or provenance propagation.
- Runtime borrow tracking or dynamic exclusivity checks.
- New lifetime/borrow syntax.
- Range-granularity (disjoint sub-range) exclusivity — see Semantics 5's
  `split_at_mut`-style escape hatch as a possible future RFC, not this one.
