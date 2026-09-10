# RFC 0159: Zig and Odin Learnings — Adoptions and Freezes

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion (proposal); not scheduled. This RFC adopts
  nothing by itself: each item graduates into its own implementation-ready
  RFC (or an explicit wont-do record) when scheduled
- Created: 2026-09-10
- Updated: 2026-09-10
- Depends on: nothing; this RFC is comparative analysis, not a design change
- Coordinates with: RFC 0152 (text-surface caution), RFC 0153 (storable
  read-only slices), RFC 0155 (unchecked-performance posture), RFC 0156
  (C-import ambition), RFC 0158 (test-time leak checking), RFC 0039
  (foreign blocks and deallocator contracts)
- Does not update `docs/reference.md`

## Summary

Review Hexal strictly as implemented against Zig and Odin under one rule:
treat convergent Zig+Odin decisions as proven, split decisions as judgment
calls, and flag anything Hexal does that neither does. The output is two
lists with opposite dispositions:

1. **Adopt, in value order**: C-import ambition, storable read-only slices,
   colocated tests with tracking allocation, a scratch-allocator idiom, and
   opportunistic `using`-style embedding plus `when`-style conditional
   compilation.
2. **Freeze**: the concurrency surface, text types, builtin collections, and
   the shared refusal fence (classes, exceptions, GC, operator overloading,
   closures, async/await, text macros).

The fence needs no action and is recorded here only so no future proposal
re-litigates it without naming this RFC.

## Problem

Hexal's language goals demand a small surface with one obvious way to do
things, plus doing "everything with Hexal that can be done with C." Without
an external anchor, every gap invites a bespoke Hexal invention and every
invention compounds. Zig and Odin are the two closest production anchors —
no-GC, C-targeting, safety-conscious systems languages — and they agree with
each other (and usually with Hexal) far more often than they differ. Where
Hexal diverges from both at once, that divergence is either product strategy
or bloat, and it should be named as one or the other before more surface
grows around it.

## Goals

- Record exactly which proven features Hexal is missing, ordered by value,
  with a graduation path for each (own RFC or explicit wont-do).
- Record exactly which surfaces are frozen, with the condition that would
  unfreeze each.
- Keep the shared refusal fence stable: no proposal should reintroduce
  classes, inheritance, exceptions, GC, operator overloading, closures,
  coroutines, or text macros without superseding this RFC's reasoning.
- Change no implemented behavior and introduce no syntax by itself.

## Non-goals

- Designing any adopted feature: adoption items graduate to their own RFCs,
  which own spelling, semantics, diagnostics, and validation.
- Re-ranking Hexal's existing goals or scheduler strategy: the M:N
  concurrency divergence is recorded as deliberate, not decided here.
- Tracking split decisions beyond the two opportunistic items below; Zig-only
  or Odin-only features without a Hexal-side need stay out.

## Adopt list

Ordered by value. Each item names its graduation vehicle where one exists.

### 1. C-import ambition (highest value)

Zig's `@cImport` is the standard for trivial C-library import; Odin's
`foreign` blocks are the explicit steady state. Both invest heavily while
Hexal's FFI remains draft. Adopt the two-tier ambition: explicit `extern`
blocks now (RFC 0039), header auto-import as the stated endgame. Graduation:
RFC 0039 owns the near tier; the auto-import tier needs its own proposal
once 0039 lands. This is Hexal goal #9 ("trivial import of C libraries")
stated as a borrowing plan rather than an invention.

### 2. Storable read-only slices; scope only the mutable ones

Both languages store slices freely (`[]T` across calls and inside structs)
and accept documented invalidation on reallocation as the programmer's
problem, with debug-mode detection as backstop. A call-only Slice is
stricter than both — and the strictness buys no safety they lack, only
missing functionality both ecosystems use daily. Adopt: stored `Slice<T>` as
a bounds-checked `{data, length}` snapshot (the current `View` shape,
renamed), with the scoped rule kept only for `Slice<mut T>`, and the
stored-slice/List-growth hazard handled by documented invalidation plus
debug detection (composing with RFC 0158's tracking allocator), not a
placement ban. Graduation: RFC 0153's placement section; this item is a
constraint on its final shape.

### 3. Colocated tests with tracking allocation

Zig's `test` blocks with the allocator-leak-on-`deinit` idiom and Odin's
`core:testing` prove tests live next to code and allocators self-audit.
Adopt both halves in order: RFC 0158's tracking backend first (zero language
surface, test-time signal), in-language `test` blocks second under their own
proposal. No existing test story changes until the second half lands.

### 4. Scratch-allocator idiom

Odin's `context.temp_allocator` with per-scope `free_all` and Zig's
arena/fixed-buffer allocators prove short-lived garbage wants bulk release
with caller-chosen policy. Hexal owns the arenas (`Stash` reset) but no
idiom binds them to scopes. Adopt the idiom before any new allocator
surface: documented `Stash` plus `defer reset` patterns, then (only if the
idiom strains) a dedicated scratch convention. Graduation: reference
documentation first; a proposal only if documentation proves insufficient.

### 5. Opportunistic `using`-style embedding (Odin-only, conditional)

Odin's `using` gives composition-with-promotion without inheritance and fits
a method-table language. Neither Zig nor Hexal has it. Adopt if and when
composition pain appears in real programs — not before. Graduation: a
proposal triggered by evidence, not by this RFC.

### 6. Minimal `when`-style conditional compilation (conditional)

Odin `when` and Zig's comptime target branches prove some compile-time
branch is eventually mandatory. Hexal has `Project` settings but no
in-language conditional. Adopt the smallest form that unblocks the second
target when it arrives; explicitly never full comptime (a second interpreter
contradicts the compiler's architecture and size goals). Graduation: a
proposal scoped to the failing target case, at the time it fails.

## Freeze list

Each freeze names its unfreezing condition.

### F1. Concurrency surface: frozen

Hexal ships a cooperative M:N scheduler with 1 MiB stacks, pinned root,
yield-checking, plus `Channel<T>`. Zig removed exactly this class of
scheduler after years of maintenance pain; Odin never attempted it, and
neither language has channels. Hexal's goal #12 (trivial concurrency) is the
counter-argument and stands — but the surface compounds fast. Frozen:
`select`, rendezvous/unbounded channels, nonblocking channel operations,
task groups, and any further primitive. Unfreezing condition: a program that
needs the primitive and cannot be written with threads, mutexes, and the
existing channel — with the Zig-removal history addressed in the proposal,
not dismissed.

### F2. Text types: frozen at three

Zig has no string type; Odin has one thin `string`. Hexal's `String`,
`Strand`, and `Rune` plus compiler-owned methods and UTF-8 validation is
already the richest surface of the three, defended as Unicode-correctness
differentiation. Frozen: no fourth text type, ever, and RFC 0152's widening
program is weighed as more text surface neither language wanted. Unfreezing
condition: none stated; a fourth text type is a goal change, not a feature.

### F3. Builtin collections: frozen

Zig implements `ArrayList`/`HashMap` as allocator-aware library code; Odin
bakes `map` and `[dynamic]` into the language with no user-visible
per-specialization machinery. Hexal's per-monomorphization generator
components are the heaviest of the three approaches and they work — which is
why no `Set`, `Deque`, priority queue, or further builtin follows.
Unfreezing condition: none; further collections are library-or-nothing, and
a standard-library proposal is the vehicle, not a builtin RFC.

### F4. Compiler-known utility types: library-or-nothing

`RuneCursor`, `Bytes`, `EoS`, and `Seek`-shaped helpers are currently
compiler-known. Odin and Zig both carry this weight in packages. Tolerate
what exists; new utility types default to library-or-nothing. Unfreezing
condition: a type the compiler must know to check or lower (niche layout,
position eligibility) — convenience alone never qualifies.

## Kept positions (no action)

- Always-checked arithmetic, bounds, and conversions with traps: Odin's
  default-checked performance proves viability; Zig's ReleaseFast proves
  demand for opt-out exists but does not obligate one. Any future opt-out is
  scoped to explicit `unsafe` (RFC 0155's direction); the default is never
  re-litigated without superseding this section.
- Explicit allocator passing stays Zig-like: Odin's ambient `context`
  allocator is rejected as a model — it conflicts with the existing no-globals
  rule and the explicitness goals. Scratch idioms (item 4) must not smuggle
  ambient state back in.
- Single-return unions over multi-return tuples: Odin's multi-return is the
  one split decision Hexal declines — the union plus `Error` machinery covers
  the cases, and tuples would be a new type family for one feature.
- No comptime, no runtime reflection, no generic constraints: all three
  languages converge on keeping these out or confined; Hexal's exclusions
  stand.

## Acceptance sketch

Non-exhaustive: this RFC never reaches Implementation-ready, so it carries
no exhaustive Validation section. It is done when:

- Every adopt item has either graduated to its own scheduled RFC or an
  explicit wont-do note recorded in that item's vehicle (or here, if no
  vehicle exists yet).
- No scheduled RFC contradicts a freeze without naming the freeze and its
  unfreezing condition.
- The kept positions above remain true of the implemented language; any
  deviation is proposed as a superseding RFC, not drifted into.

## Open questions

1. Whether the C-import auto-import tier should be proposed now (as a
   distant scheduled item) or left until RFC 0039 lands. Leaving it avoids
   designing against an unsettled boundary; scheduling it states intent.
2. Whether item 2's storable-slice recommendation should be folded into RFC
   0153 now or wait until 0153's call-scoped form ships and proves
   insufficient. Folding in risks rework of in-flight design; waiting risks
   shipping a restriction both anchors reject.
3. Whether the concurrency freeze (F1) should additionally forbid scheduler
   *options* growth (stack knobs, policy flags) or only new primitives and
   types. Options growth is the quieter compounding path.
