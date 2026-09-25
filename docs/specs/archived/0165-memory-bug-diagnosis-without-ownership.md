# RFC 0165: Memory-Bug Diagnosis Without Ownership Semantics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented 2026-09-24. Phases 1 and 2 landed with the
  Validation suite green and `docs/reference.md`'s cleanup-misuse paragraph
  synchronized. Phase 3 (interprocedural summaries) remains optional and
  deliberately unscheduled. RFC 0110 and RFC 0149 stay Blocked citing this RFC
- Created: 2026-09-10
- Updated: 2026-09-20
- Origin: an audit of eleven classical memory-bug classes against the shipped
  compiler, asking which are already solved, which are not, and what the
  cheapest remaining fix is
- Depends on: nothing. Every mechanism below extends code that ships today
- Coordinates with: RFC 0110 and RFC 0149 (the two surviving ownership-arc
  proposals, both Blocked/deferred), RFC 0155 (`unsafe do ... end`, closed and
  implemented), RFC 0156 (fenced pointer arithmetic, closed and implemented),
  RFC 0161 (pointer mutability and Slice, implemented; supersedes RFC 0153 and
  RFC 0154), RFC 0157 (uninitialized allocation), RFC 0158 (debug allocation
  tracking), RFC 0225 (cross-allocator release rejection)
- Consumed by: RFC 0225, which keys its allocator-kind fact by the allocation
  identity this RFC introduces. Sequencing is settled — this RFC lands first —
  so Phase 1 must leave the identity reachable to a second fact keyed the same
  way, rather than private to the freed-state checks
- Does not update `docs/reference.md`: synchronize only after implementation
  is approved and behavior stabilizes

## Summary

Hexal already rejects several locally provable classical memory-bug cases, and does
so without any ownership, lifetime, or borrow machinery. The confirmed alias
gaps are use-after-free and double-free through copied bindings. They share
one root cause: the shipped cleanup analysis tracks a *binding*, not an
*allocation*, so a single assignment turns both checks off. Leaks are a
separate runtime-tracking concern owned by RFC 0158.

This RFC proposes closing the alias portion of that gap in place, by extending
the flow state the checker already maintains. It adds no keyword, no type, no
move semantics, and no lifetime model. Runtime physical leak detection remains
RFC 0158's responsibility. Static leak rejection is deliberately not selected
by this revision; the decision is recorded below.

It is deliberately a **bug finder, not a guarantee**: it reports what local
facts prove and stays silent otherwise, which is the policy `docs/reference.md`
already states for cleanup misuse ("An undecided case is always accepted").

## Evidence

Every row below was probed against the current tree by compiling a source
program through `compiler.Compile` and reading the returned diagnostics. The
Result column is observed behavior, not a reading of specification text.
Re-probed 2026-09-20 in current syntax: address-of is `@`, dereference is
`^p`, and the writable pointer spelling is `Ptr<mut T>`.

### Current locally-proven coverage

| Bug class | Probe | Result |
| --- | --- | --- |
| Null pointer dereference | `^p` on an unnarrowed nullable pointer | rejected: "Ptr<mut Int32> \| Nil may be Nil; narrow it before dereferencing" |
| Out-of-bounds read (constant) | `a[7]` on `Array<Int32, 3>` | rejected: "array index 7 is out of bounds for Array<Int32, 3>" |
| Stack/heap buffer overflow, safe code | `p + 1` outside `unsafe` | rejected: "operator + requires numeric operands; got Ptr<mut Int32>" |
| Pointer type confusion | `let q: Ptr<Bool> = p` from `Ptr<Int32>` | rejected: "expected Ptr<Bool> initializer; got Ptr<Int32>" |
| Off-by-one (as memory safety) | runtime `l[i]` | accepted, then bounds-checked at runtime; traps rather than corrupting |
| Uninitialized memory read | `let n: Int32` with no initializer | rejected by the current `let` declaration syntax |
| Uninitialized heap allocation | `h.allocate<Int32>()` | rejected: "allocation requires an explicit initializer" |
| Missing string null-terminator | `s[0]` on a String | rejected: "cannot index String; use bytes() for indexed byte access"; String is pointer-plus-byte-length with the count in its header and is never NUL-terminated |
| Invalid / arbitrary free | `h.free(p)` where `let p: Ptr<mut Int32> = @n` names a local `n` | rejected: "free does not accept a pointer into this function's local storage" |

The mechanism is worth naming, because it is the cheap one and it is already
this project's habit: **these were solved by removing the capability from safe
code, not by analyzing it.** No pointer arithmetic in safe code, so no
overflow. No casts, so no type confusion. Nullability in the type, so no null
dereference. Mandatory initializers, so no garbage reads. Length-carrying
strings, so no terminator bug. Removal costs nothing at compile time and
cannot be unsound.

RFC 0156 has since returned two of those capabilities fenced rather than
absent: inside `unsafe do ... end`, `Ptr<T>.offset`, pointer indexing, and
`Ptr<T>.cast<U>()` are all available. That preserves the pattern rather than
breaking it — safe Hexal keeps the guarantee and the capability is named at
its use site — but it does mean the alias surface this RFC must consider is
larger than `let q = p`. See Alias-forming operations below.

### Confirmed remaining gaps

| Bug class | Probe | Result |
| --- | --- | --- |
| Use-after-free, direct | `h.free(p)` then `^p` | rejected: "this pointer's storage was released on every path to this point" |
| Use-after-free, **through an alias** | `let q = p`, `h.free(p)`, then `^q` | **accepted** |
| Double free, direct | `h.free(p)` twice | rejected: "free releases storage already released on every path to this point" |
| Double free, **through an alias** | `let q = p`, `h.free(p)`, `h.free(q)` | **accepted** |
| Use-after-free, **through `.offset`** | `let q = p.offset(0)` in `unsafe`, `h.free(p)`, `^q` | **accepted** |
| Double free, **through `.cast`** | `let q = p.cast<UInt32>()` in `unsafe`, `h.free(p)`, `h.free(q)` | **accepted** |
| Use-after-free, **across a call** | `release(h, p)` then `^p` | **accepted** |
| Memory leak | `let p = h.allocate<Int32>(1)` and nothing else | **accepted** |

The direct forms already work. Every failure is an alias or a call boundary.

### What does *not* abandon tracking today

This matters because it is easy to assume it does. Probed on the same tree:
after `use(p)`, after `return p`, after `l.push(p)`, after `Box(p = p)`,
after `spawn worker(p)`, and after `c.send(p)`, a subsequent local
`h.free(p); h.free(p)` is **still rejected**. The only operation that
abandons a pointer's cleanup fact is a writable `@` of the binding
(`compiler/checker/places.go`), which is `flowState.escape`'s single
production call site.

The practical consequence is a rule this RFC must not violate: **`freed` is
only ever set by a local `free` in this function.** A callee or a spawned
task releasing the pointer never sets it, so there is no false positive at
those boundaries to suppress — and calling `escape` there would delete six
diagnostics that work today. Escape is for facts that a foreign write can
falsify (narrowing, capability), not for facts this function established
about its own calls.

Bindings sourced from a non-variable expression — `let p = t.join()`,
`let q = b.p`, `let q = l[0]` — are not aliases of anything the checker can
name, so they are tracked as fresh facts. That is already correct and
unchanged by this RFC: a local double free of a joined or field-read pointer
is a real bug and is rejected today.

## Root cause, in the code

`compiler/checker/scope.go` defines the per-function fact table:

```go
type flowState struct {
	facts           map[BindingID]flowFact
	tracked         map[BindingID]bool
	released        map[BindingID]map[uint64]bool
	provenance      map[BindingID]BindingID
	releasedSources map[BindingID]bool
	stringOrigins   map[BindingID]stringOriginSet
	stringPlaces    map[stringPlaceKey]stringOriginSet
}
```

Its own comment states the design: "tracked distinguishes a known cleanup
state from an intentionally unknown state after a copy or escape."

`flowFact` additionally carries a `version`, and `released` keys freed state
by `(binding, version)`. That pair is the cleanup-action path: a `defer`
records a versioned release so a deferred free and a straight-line free are
compared against the same allocation. Any change to how `freed` is keyed has
to change how `released` is keyed in the same step, or the two paths
disagree.

The abandonment happens in the pointer-copy path in
`compiler/checker/declarations.go`:

```go
if sourceBinding := directPointerBinding(initializer.source, declaredType); sourceBinding != 0 {
	ctx.names.flow.dropFreed(sourceBinding)
	ctx.names.flow.dropFreed(declaredBinding.id)
}
```

On `let q = p`, **both** bindings lose tracking. The same behavior appears for
pointer assignment.

The consuming check, `checkTrackedHeapFreeInState`
(`compiler/checker/alloc.go`), then requires a directly named, still
tracked binding:

```go
if state == nil || value.Node.Kind != VariableExpression || value.Node.Binding == 0 || !state.tracked[value.Node.Binding] {
	return nil
}
```

So the analysis is not missing; it is *switched off* by aliasing. The
existing branch merge in `compiler/checker/scope.go` already carries these
maps across control flow, and `flowFact.freed` already means "released on
every path to this point" — the machinery for path-sensitive, no-false-
positive reasoning is present and working.

## Proposal

One focused addition is proposed. It is independently shippable and useful.

### 1. Track allocations, not bindings

Replace the drop-both behavior with an **allocation identity**: a fresh opaque
id minted at each tracked allocation site, and a per-binding map naming which
identity a binding currently denotes.

```go
allocation  map[BindingID]allocationID  // which allocation this binding names
freedAlloc  map[allocationID]bool       // released on every path to this point
```

- `trackFreed` mints a fresh `allocationID` and maps the binding to it.
- On `let q = p` (and on pointer assignment) where `directPointerBinding`
  identifies a source, `q` is mapped to **`p`'s current identity**. No set is
  built and nothing is untracked.
- `markFreed(q)` marks `q`'s identity; `freed(q)` reads `q`'s identity. Every
  binding mapped to that identity sees the result, in either direction.

A union-find is deliberately **not** used. It is the wrong shape for this
problem in three ways: it cannot un-union, so escape cannot remove one member;
it cannot be intersected cheaply at a join; and it models a binding-to-binding
relation when the fact being tracked is a binding-to-allocation one. Identity
labelling is O(1) for every operation this RFC needs, clones by copying two
small maps, and intersects by comparing labels.

**Re-assignment re-labels, it does not follow the name.** This is the rule a
binding-relation representation gets wrong:

```hexal
let p: Ptr<mut Int32> = h.allocate<Int32>(1)
let mut q: Ptr<mut Int32> = p            # q denotes p's allocation
q = h.allocate<Int32>(2)                 # q denotes a FRESH allocation
h.free(p)                                # marks p's allocation only
h.free(q)                                # accepted: different allocation
```

Assigning a binding a value from any source other than a tracked pointer
binding re-maps it to a fresh identity. `p`'s identity is untouched. Without
this rule the program above is reported as a double free, which is a false
positive against a program that is correct today.

**Merging at control-flow joins is by identity agreement.** A binding keeps
its identity mapping across a join only when every incoming path maps it to
the same identity; otherwise the mapping is dropped and the binding becomes
untracked. This matches the existing meaning of `freed` ("on every path") and
produces no false positives: a `q` that denotes `p`'s allocation on only one
branch has no agreed identity at the join, so nothing is reported.

Only the must-alias case is represented. There is no may-alias state, and no
four-point lattice: a fact that only *might* hold cannot be reported without
false positives, and cannot be relied on to stay silent either, so
representing it would buy nothing and would cost exactly the checker
complexity that language goal 18's "without disproportionate checker
complexity" qualifier rules out.

This fixes both alias rows in the evidence table.

### 1a. Alias-forming operations, exhaustively

The join site is `directPointerBinding`
(`compiler/checker/declarations.go`), which requires a source that is a bare
variable of a pointer type. That yields exactly one alias form:

- `let q = p` and `q = p`, where both sides are `Ptr<...>`. **In scope.**

Every other way to obtain a pointer produces a binding the checker cannot
prove aliases anything, and is therefore tracked as a fresh identity — the
existing, correct, fail-open behavior:

- `let q = b.p`, `let q = l[0]`, `let q = t.join()`, `let q = make(h)`;
- `let q: Ptr<mut Int32> | Nil = p` — a union-typed target has no `Element`,
  so no alias is recorded and `q` is untracked;
- `let q = p.offset(n)` and `let q = p.cast<U>()` inside `unsafe do ... end`.

The last pair is a genuine known gap, not an oversight: `.offset` and `.cast`
are the operations allocator and binary-format code actually uses to make
aliases, and both are accepted today (see the evidence table). Covering them
requires deciding whether an offset pointer shares its base's identity, which
in turn requires deciding what `free` of an interior pointer means. That is a
separate design question and is explicitly **out of scope for this RFC**; it
is recorded in Open questions.

### 2. Deferred decision: static leak diagnosis

Static diagnosis of non-escaping leaks is not part of this RFC. RFC 0158 owns
opt-in runtime physical-allocation tracking, which can report leaks after
values escape through calls or containers. Static leak rejection is deferred
until Hexal has an intentional process-lifetime allocation convention.

RFC 0158 may additionally detect escaped or copied-pointer failures at runtime
when the allocation remains inside the debug backend's known allocation
boundary. RFC 0165 does not require that metadata in ordinary builds and does
not promise detection for foreign or arbitrarily forged addresses.

### 3. Optional, later: per-function summaries

Infer, bottom-up over the module graph, whether a function frees a pointer
parameter and whether it returns a fresh allocation. Feed those summaries
into alias checks to reach the cross-call row in the evidence table.

Deliberately sequenced last. Alias checks need no interprocedural analysis
at all; this one does, and should be built only if the first two prove
insufficient in practice.

## Non-goals

- Ownership, affinity, moves, borrows, or lifetimes.
- Automatic cleanup insertion. This RFC diagnoses; it never frees.
- New keywords, new types, or any change to the `Ptr<T>` / `Ptr<mut T>`
  spelling.
- `Stash<T>` and `Pool<T>` **handle** semantics — the handle values
  themselves, their destroy/reset state, and their region model. Those need a
  separate invalidation design. Pointers *allocated from* a Stash or Pool are
  not excluded: they are ordinary `Ptr<mut T>` and are covered. See Stash and
  Pool.
- Soundness. Undecidable cases stay accepted, matching the current contract.
- Runtime leak detection, which RFC 0158 covers with a LeakSanitizer gate on
  the Linux lane.
- Runtime generation, tombstone, quarantine, and sanitizer checks supplied by
  RFC 0158 are complementary diagnostics, not ownership or lifetime rules.
- Cycle detection. Nothing here detects cyclic garbage; nothing here creates
  the ability to build a cycle either.

## Relationship to the ownership arc

The arc has since resolved in three directions, and this section is a
comparison against what is actually left of it:

- **Shipped without ownership.** RFC 0155 (`unsafe do ... end`) and RFC 0156
  (fenced pointer arithmetic and casts) are closed and implemented. RFC 0161
  implemented pointer mutability and `Slice`, superseding RFC 0153 and
  RFC 0154; `View` and `MutPtr` are retired names that denote no type, and the
  View-escape bugs those RFCs were to close are no longer open in
  `docs/status.md`.
- **Blocked on this RFC.** RFC 0110 (affine ownership) and RFC 0149
  (`Box<T>` and scoped references) are both Blocked/deferred, each recording
  that RFC 0165 invalidates part of its design.
- **Still unbuilt.** No affine move, automatic drop, or exactly-once cleanup
  exists anywhere in the tree.

So the comparison below is between this RFC and RFC 0110/0149 only.

The arc pursues two distinct goals that are easy to conflate:

1. **Diagnose memory misuse.** This RFC addresses alias-derived use-after-free
   and double-free at roughly the cost of one alias-set structure.
2. **Delete the cleanup calls.** The arc turns the `List<String>` cleanup in
   `workbench/snippets/categories/07-collections.json` from N+1 correctly
   ordered calls into zero. **This RFC does not do that at all.** Under this
   proposal the author still writes every free, and is merely told when the
   count or the order is wrong.

Goal 2 is the arc's real prize, and it is unreachable from here: automatic
drop requires affine moves, because two shallow copies of one `String` handle
would otherwise both be dropped. There is no cheap version of goal 2.

Consequences of choosing this RFC *instead of* the arc, stated plainly so the
trade stays visible:

- affine iteration stays unaffected — `for name in names` over a
  `List<String>` keeps working, where RFC 0110 currently rejects it;
- the roughly 90 consuming `defer` sites across snippets and tests keep
  working, where RFC 0110 currently rejects deferred consuming cleanup;
- `docs/reference.md`'s statement that Hexal has "no moves, borrow states,
  retain counts, implicit destructors, or compiler-enforced exactly-once
  cleanup" remains true and needs no rewrite;
- cleanup composition stays manual, which is the cost.

The two are not mutually exclusive. Alias checking is useful whether or not
the arc lands, because raw `Heap.allocate`/`free` survives the arc unchanged
as the C-interoperation layer, and nothing in the arc diagnoses misuse there.

**Not adopted here: `Box<T>` with automatic drop.** An earlier revision of
this section floated it as an opt-in type beside `Heap.allocate`. It is
recorded as a rejected alternative, not a recommendation: automatic drop
requires affine moves, which is precisely the machinery this RFC's Non-goals
refuse, and admitting it for one type would make this document a partial
ownership proposal. It belongs to RFC 0149, which owns it and is Blocked.

## Diagnostics

Reuse the three shipped messages verbatim where they apply; an alias-derived
rejection is the same bug as a direct one and should not read differently:

- `free releases storage already released on every path to this point`
- `this pointer's storage was released on every path to this point`
- `free does not accept a pointer into this function's local storage`

Diagnostics name source bindings and operations, never `BindingID` values,
alias-set identities, or generated C names.

## Required sweep

- `flowState`'s allocation-identity maps, `clone`, and the identity-agreement
  merge in `compiler/checker/scope.go`;
- the two `dropFreed` alias sites in `compiler/checker/declarations.go`
  (declaration at the `let q = p` path, assignment at the `q = p` path);
- the re-label-on-reassignment rule at the same assignment path, which today
  calls `clearFreed` and must instead mint a fresh identity;
- `checkTrackedHeapFreeInState` (`compiler/checker/alloc.go`) to mark and read
  the allocation identity instead of a single binding;
- **the versioned freed path, which the first revision of this RFC missed
  entirely**: `flowFact.version`, `released[BindingID][version]`,
  `nextFreedVersion`, `markFreedVersion`, `freedAt`, and
  `checkTrackedHeapFreeVersion`. This is the `defer`/`errdefer` duplicate of
  the same check and it works today — `defer h.free(p)` followed by
  `h.free(p)` is rejected. Version state must key on the allocation identity
  for the same reason the flag does, or an aliased free inside a cleanup
  action is silently not diagnosed;
- `validateDeferredActions` and `names.returnFlows`, which clone `flowState`
  per return path and must carry the identity maps;
- `flowState.escape` to drop the escaping binding's identity mapping. Escape
  keeps its single existing call site (writable `@`); this RFC adds none;
- the explicit decision that static leak diagnosis is deferred to RFC 0158;
- `compiler/checker/alloc_test.go` and `io_test.go`, which need cases added;
- `compiler/tests/integration/pointers_test.go`, whose `"pointer copy"` case
  asserts that `let q = p; h.free(p); h.free(q)` is **accepted**. That case
  must be **changed**, not merely supplemented — it encodes exactly the
  behavior this RFC reverses. Its neighbouring `"reallocation after free"` and
  `"one branch free"` cases must keep passing unchanged;
- `compiler/checker/pointer_arithmetic.go`, to confirm that `.offset` and
  `.cast` results continue to receive a fresh identity rather than inheriting
  their base's, per the out-of-scope decision in Alias-forming operations.

### The handle-kind invariant

`trackablePointerBinding` (`compiler/checker/declarations.go`) shares this
freed machinery with IO, File, Stash, Pool, TcpConnection, TcpListener,
Process, Pipe, and Signals bindings. This change is safe for those only
because `directPointerBinding` requires a type with an `Element`, which none
of them has — so each keeps a singleton identity and behaves exactly as it
does today. That invariant is load-bearing and currently accidental. State it,
and add a regression that a copied handle binding forms no alias.

It does **not** hold for pointers obtained from Stash and Pool, which are
ordinary `Ptr<mut T>` and therefore do alias. See Stash and Pool below.

## Stash and Pool

The first revision of this RFC asserted that Stash and Pool behavior is
unchanged. That assertion is false, and the correction matters enough to
state separately.

`directPointerBinding` keys on `Type.Element`, not on which allocator
produced the pointer. A Pool slot and a Stash allocation are both
`Ptr<mut T>`, so `let q = p` joins them exactly as it joins a Heap pointer.
Two probed programs change behavior:

```hexal
let mut pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
let q: Ptr<mut Int32> = p
pl.destroy()          # accepted today; rejected after this RFC
```

```hexal
let mut st: Stash<Int32> = Stash<Int32>()
let p: Ptr<mut Int32> = st.allocate(1)
let q: Ptr<mut Int32> = p
st.reset()
let v: Int32 = ^q     # accepted today; rejected after this RFC
```

**Decision: both are in scope and both become rejected.** They are correct
diagnostics, missed today only because the copy switched tracking off, and
suppressing them would mean writing extra code whose only purpose is to hide
real bugs. The Non-goals exclusion covers Stash and Pool *handles* — the
`Stash<T>`/`Pool<T>` values themselves, their reset/destroy state, and their
region semantics — not the ordinary `Ptr<mut T>` values allocated from them.
Those are pointers and are treated as pointers.

Two consequences the implementation must carry:

- Each program above gets a Validation case, and they are the acceptance
  evidence that the decision was implemented rather than stumbled into.
- `checkHandleNotDestroyed` still reads its receiver binding directly. It is
  the handle precondition, it has no pointer alias to follow, and routing it
  through the identity would widen this RFC into the handle semantics its
  Non-goals do exclude.

## Validation

This section is exhaustive.

- Every current locally-proven evidence row continues to produce its exact current
  diagnostic; this RFC changes none of them.
- Use-after-free and double free through a single alias are rejected with the
  existing messages.
- An alias chain of length three or more is rejected at every link.
- Freeing through the alias and using through the original is rejected, in
  both directions: `let q = p; h.free(q); ^p` and `let q = p; h.free(p); ^q`.
- An alias formed on only one branch of an `if` is **accepted** after the
  join; no false positive is produced by a conditional alias.
- An alias formed on every branch is rejected after the join.
- A loop that aliases on the back edge preserves the loop-head state and
  reports no false positive.
- **Re-assignment re-labels**: `let q = p; q = h.allocate<Int32>(2);
  h.free(p); h.free(q)` is accepted. Re-assigning the *original* is equally
  accepted: `let q = p; p = h.allocate<Int32>(2); h.free(q); h.free(p)`.
- A binding sourced from a member read, collection read, call result, or
  `join()` receives a fresh identity: `let q = b.p; h.free(q); h.free(q)` is
  rejected as a direct double free, and `h.free(p); h.free(q)` for the same
  `b.p` is accepted.
- A union-typed target (`let q: Ptr<mut Int32> | Nil = p`) forms no alias and
  leaves `p`'s own tracking intact.
- `.offset` and `.cast` results receive a fresh identity; the two evidence
  rows naming them remain **accepted**, unchanged by this RFC.
- An aliased free inside `defer` is diagnosed on the versioned path:
  `let q = p; defer h.free(p); h.free(q)` is rejected.
- A writable `@` of a tracked pointer binding drops that binding's identity
  mapping and restores today's accept-everything behavior for it. This is the
  only operation that does so.
- `spawn`, Channel `send`, a call argument, a `return`, a member store, and a
  collection store each **preserve** the identity mapping. `spawn worker(p);
  h.free(p); ^p` and `c.send(p); h.free(p); h.free(p)` remain rejected exactly
  as they are today. No diagnostic that exists before this RFC is removed.
- A copied handle binding of each non-pointer tracked kind (IO, File, Stash,
  Pool, TcpConnection, TcpListener, Process, Pipe, Signals) forms no alias and
  behaves exactly as it does today.
- A Pool slot aliased before `destroy` is rejected: `let p = pl.allocate(1);
  let q = p; pl.destroy()` fails with "Pool cannot be destroyed while a
  locally tracked slot is live". The unaliased form already fails this way and
  continues to.
- A Stash allocation aliased before `reset` is rejected: `let p =
  st.allocate(1); let q = p; st.reset(); ^q` fails with "this pointer's
  storage was released on every path to this point". The unaliased form
  already fails this way and continues to.
- `checkHandleNotDestroyed` is unchanged: a copied `Stash<T>` or `Pool<T>`
  *handle* forms no alias, and double-destroy through a copied handle remains
  accepted exactly as it is today.
- Generated C is byte-identical for every program that compiled before this
  RFC, and the snippet manifest moves no hash. The claim is checkable rather
  than aspirational because the identity maps are read by no generation path:
  they must not be consulted by, merged with, or written into `flowFact`'s
  `capability` or narrowing fields, which *do* reach lowering. A manifest run
  showing zero artifact movement is the acceptance evidence.
- Ordinary tests remain pure Go.

RFC 0165 requires nothing from RFC 0158. It uses no runtime allocation
metadata and its diagnostics are complete without a debug backend linked. A
selected RFC 0158 backend may independently diagnose an escaped or
copy-derived failure at runtime; that is a separate best-effort facility, not
a dependency of this RFC and not a compile-time guarantee.

## Detailed implementation plan

### Phase 1: allocation identity

1. Add the `allocation` and `freedAlloc` maps to `flowState` beside `tracked`,
   with `clone` support and an identity-agreement join merge.
2. Mint a fresh identity in `trackFreed`.
3. Replace the paired `dropFreed` calls in `declarations.go` with an identity
   copy at the declaration path, and with an identity copy or a fresh mint at
   the assignment path depending on whether the source is a tracked pointer
   binding.
4. Route `markFreed` and `freed` through the identity.
5. Route the versioned path — `markFreedVersion`, `freedAt`, and
   `checkTrackedHeapFreeVersion` — through the identity, so `defer` and
   `errdefer` cleanup sees the same facts as straight-line code.
6. Drop the identity mapping in `escape`. Add no new `escape` call site.
7. Leave `checkHandleNotDestroyed` reading the binding directly. It is the
   Stash/Pool precondition read and has no pointer alias to follow; routing it
   through the identity would silently widen this RFC into the handle
   semantics its Non-goals exclude.
8. Add the alias, chain, branch, loop, re-assignment, fresh-source, defer, and
   handle-kind Validation cases.

### Phase 2: conformance

1. Confirm no generated-C or manifest movement.
2. Add compact workbench snippets only where a snippet demonstrates a
   diagnostic; this RFC otherwise adds no snippet surface.
3. Run ordinary and tagged C23 suites.
4. Synchronize `docs/reference.md`'s cleanup-misuse paragraph — which
   currently states that a pointer "copied to a second binding is not tracked"
   and that "leaks are not diagnosed" — only after behavior stabilizes.

### Phase 3 (optional): interprocedural summaries

1. Compute per-function free-parameter and returns-allocation summaries.
2. Extend alias checks to consume them, keeping fail-open behavior wherever a
   summary is unavailable. Leak summaries remain outside this RFC unless the
   static-leak question is separately resolved.

## Decisions and remaining questions

1. **Settled: this replaces that part of the arc.** The question was written
   while RFC 0110 and RFC 0149 were live alternatives. They are not: both are
   `Blocked`, each citing *this* RFC as the reason, and `docs/reference.md`'s
   Language boundary now states shallow copying with explicit cleanup as the
   model. So this RFC is the cleanup story, not one of two candidates, and its
   priority follows from that rather than from an undecided comparison. The
   design is unchanged either way, which is why this was never a blocker.
2. Static leak rejection is deferred, and this is a decision rather than a
   sequencing note. A compile-time leak error requires an explicit convention
   for intentional process-lifetime allocations, which Hexal does not have.
   RFC 0158's LeakSanitizer gate on the Linux lane is the selected mechanism
   for leak reporting. Nothing in this RFC implies that non-escaping leaks
   become hard errors later; that would be a new design decision, not a phase
   of this one.
3. Settled: pointers allocated from Stash and Pool are in scope and gain the
   improvement, because they are pointers. See Stash and Pool.
4. **Open: should `.offset` and `.cast` results inherit their base's
   identity?** Doing so would close the two `unsafe` alias rows in the
   evidence table, and needs a prior decision on what `free` of an interior
   pointer means. Out of scope here; it wants its own spec.
5. Handle tracking for `String`, `List`, `Dict`, `Stash`, and `Pool` *handles*
   (as distinct from pointers allocated from them) remains outside this RFC
   and requires a separate invalidation model.
