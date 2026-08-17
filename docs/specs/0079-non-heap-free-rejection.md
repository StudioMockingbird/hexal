# RFC 0079: Rejecting `free` of a Non-Heap Pointer

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design decision required
- Created: 2026-08-16
- Scope: one static check — `Heap.free` applied to a pointer statically known
  not to be heap-derived
- Depends on: nothing. Independent of RFCs 0072–0079.
- Coordinates with: `docs/reference.md`, `AGENTS.md`, `docs/status.md`
- Does not change: allocation, cleanup, ownership, aliasing, or copying
  semantics

## Summary

`h.free(ref stackLocal)` compiles today. The generated runtime then reads
allocation metadata from below a stack address.

This is a **category** error, not a lifetime error: the argument is statically
known not to come from an allocator. That distinction is what makes it decidable
where double-free and use-after-free are not, and it is the entire scope of this
RFC.

## What is already settled

`AGENTS.md` goal 18 states the compiler-enforced memory properties — bounds,
nullability narrowing, union tags, initialization — and states plainly that
allocation and cleanup are the programmer's responsibility, with double-free,
use-after-free, and leaks undiagnosed. `docs/reference.md` agrees:

> there are no moves, borrow states, retain counts, implicit destructors, or
> compiler-enforced exactly-once cleanup

Verified accepted today, and **all correct by that model**:

```hexal
h.free(p); h.free(p)        -- double free
s.free(h)                   -- s is a String literal
```

Those are not defects and this RFC does not address them. An earlier audit
proposed a broad ownership model; that is out of scope and, given the settled
design, unwanted.

## The one open case

```hexal
h: Heap = Heap.new()
x: Int32 = 5
h.free(ref x)               -- ACCEPTED today
```

`docs/reference.md` says `h.free(ptr)` "accepts Ptr/MutPtr and requires the
matching allocator", and that "Runtime metadata may catch live mismatch or
double-free". A stack address has no metadata to read — the runtime consults
memory that was never an allocation header.

Unlike a lifetime error, the compiler already knows the answer: `ref x` on a
local produces a pointer whose provenance is a stack binding, and no allocator
returned it.

## The decision

**Option A — reject statically.** `Heap.free` requires an argument whose
provenance is not a `ref` to a local or parameter binding, using the same
provenance tracking `View.from_pointer` already performs.

**Option B — leave it.** Consistent with "cleanup is the programmer's
responsibility", and avoids a check whose completeness cannot be guaranteed.

**Option C — trap at runtime.** Emit a provenance check in the generated free.
Costs runtime work on every free and cannot distinguish a stack address
reliably.

### Recommendation: A

The provenance machinery already exists. `from_pointer` rejects pointers locally
traceable to `ref` (`compiler/checker/views_bridge.go`), and RFC 0073's D4 fixes
its propagation through copies and assignment. `Heap.free` can consume the same
fact.

Rejecting C: it pays at runtime for something knowable at compile time, and
cannot be made reliable.

Rejecting B: it conflates two different things. "Cleanup is the programmer's
responsibility" is about *when* to free, and Hexal deliberately does not track
that. *What* may be freed is a type-level question the compiler can already
answer, and the reference already implies an answer by saying free "requires the
matching allocator."

### Why this is `design decision required`

Option A is only correct if the check is **sound in the direction that matters**:
it must never reject a valid free. The provenance analysis is deliberately local
— `docs/reference.md` states interprocedural provenance from a caller argument
is not checked — so a pointer arriving as a parameter must remain acceptable
even though its origin is unknown.

Confirm before implementing:

1. Does rejecting only *locally traceable* `ref` provenance catch the motivating
   case without rejecting heap pointers passed through parameters, object
   members, or collections? The probe programs in the Validation section decide
   this.
2. Should the same rule extend to `String.free`, `List.free`, `Dict.free`, and
   `Channel.free`, which take a `Heap` and free their own storage? Their
   receivers are handles, not `ref`-derived pointers, so the case may not arise
   — verify rather than assume.

## Proposed rule

If Option A is accepted, `docs/reference.md` gains one sentence under Allocation
and lifetime:

> `h.free(ptr)` rejects a pointer locally traceable to `ref`. Provenance is
> tracked with the same local analysis `View.from_pointer` uses: a pointer
> arriving as a parameter, read from a member, or returned by an allocator is
> accepted, and interprocedural provenance is not checked.

Diagnostic:

> `free` does not accept a pointer into this function's local storage

matching the existing `from_pointer` wording so the two read as one rule.

## Invariants

1. No currently valid program becomes invalid. Every accepted `free` of a
   heap-derived pointer keeps compiling, including through parameters, members,
   collection elements, and returns.
2. Double-free, use-after-free, and freeing a literal remain accepted. This RFC
   does not add lifetime tracking.
3. Generated C is unchanged. This is a checker-only rule; no runtime check is
   emitted.
4. `docs/reference.md` gains exactly the sentence above.

## Validation

Must reject:

```hexal
h.free(ref x)                       -- direct
p: MutPtr<Int32> = ref x; h.free(p) -- one binding
p = ref x; q = p; h.free(q)         -- two bindings (needs RFC 0073 D4)
```

Must continue to accept:

```hexal
p: MutPtr<Int32> = h.allocate<Int32>(5); h.free(p)   -- allocator
fun release(h: Heap, p: MutPtr<Int32>) do h.free(p) end   -- parameter
h.free(obj.pointerMember)                             -- member
h.free(list[0])                                       -- collection element
```

Plus: `go test ./...`, `go vet ./...`, snippet manifest unchanged, and a
negative test per rejected form.

## Sequencing

Independent of every other open spec, with one note: the two-binding rejection
case above only works once RFC 0073's D4 fixes `fromRef` propagation through
copies. If this RFC lands first, that case is expected to be accepted and the
test should record it as a known gap referencing D4 rather than be omitted.

## Non-goals

- Lifetime tracking, ownership, moves, borrow states, or exactly-once cleanup.
- Diagnosing double-free, use-after-free, or leaks.
- Interprocedural provenance.
- A runtime provenance check.
- Changing `docs/reference.md`'s allocation model beyond the one sentence above.
- Revisiting `AGENTS.md` goal 18, which is settled.

## Drawbacks

- A partial check invites the reading that `free` is generally validated. The
  proposed reference wording states the limit explicitly for that reason.
- It adds a rule to a language that has deliberately kept its ownership model
  minimal. The counterargument is that this rule is about argument *category*,
  which the type system already governs, not about lifetime, which it does not.
