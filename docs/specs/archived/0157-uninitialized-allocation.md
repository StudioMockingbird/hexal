# RFC 0157: Explicit Uninitialized Allocation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Discarded, 2026-09-23, on the evidence its own Motivation gate
  demanded. The gate required a benchmark showing a real workload materially
  paying for dummy initialization, and said to discard the RFC if ordinary
  initialized allocation met the workload without material overhead. It does:
  the initialization is a single `*pointer = initial` store whose cost is
  unmeasurable below roughly a kilobyte, and the kilobyte case is excluded by
  this RFC's own Non-goals. `Heap.allocate<T>(initial)` remains the only heap
  allocation; nothing in the language changes. See The gate was attempted.
  Retained as the record of the measurement so it is not re-run from intuition
- Created: 2026-09-10
- Updated: 2026-09-23
- Depends on: the implemented `unsafe do ... end` contract from archived RFC
  0155
- Coordinates with: RFC 0118 (task boundaries are not escape boundaries), RFC
  0158 (debug allocation tracking), RFC 0160 (memory-bug inventory), RFC 0165
  (intraprocedural alias diagnosis), RFC 0187 (build modes do not change
  generated C), and RFC 0189 (independent aligned allocation)
- Does not update `docs/reference.md`

## Summary

Consider an explicitly unsafe allocation that leaves one T object
uninitialized:

```text
Heap.allocate_uninit<T>() -> Ptr<mut T>
```

Initialized allocation remains the default. This proposal no longer includes
alignment control; RFC 0189 owns that independently useful, safe operation.

This RFC has two separate decisions:

1. The allocation feature is gated on measured demand and remains deferred.
2. If the feature is eventually adopted, its pointer participates in the
   existing memory-diagnostics boundaries. RFC 0165 may track its allocation
   identity while it remains locally known; RFC 0118 establishes that task and
   channel transfer are *not* escape boundaries, so a locally tracked fact
   survives them; RFC 0158 may report the physical allocation if it survives
   process shutdown.

## Motivation gate

Do not schedule this RFC from intuition alone. Before promotion, a benchmark
must show a real Hexal workload materially paying for dummy initialization
immediately overwritten by the program. Record allocation size, repetition,
generated C, compiler/toolchain, target, and measured time.

If ordinary initialized allocation, List growth, or a typed foreign API meets
the workload without material overhead, discard this RFC.

### The gate was attempted on 2026-09-23, and it fails

**Generated C.** `Heap.allocate<T>(initial)` lowers to one specialized helper
per `T`, and the initialization this RFC would remove is a single store:

```c
static int32_t * hex_heap_allocate_Int32(hex_heap h, int32_t initial) {
    (void)h;
    int32_t *pointer = hex_heap_allocate(sizeof(int32_t));
    *pointer = initial;
    return pointer;
}
```

Not a `memset`, not a loop. Note also that `initial` arrives **by value**: the
caller has already constructed and copied it, so `allocate_uninit<T>()` removes
this store and nothing else unless the caller's own construction also
disappears.

**Measurement.** The store was benchmarked directly against the `mi_malloc` it
follows, using the vendored mimalloc 3.5.1 archive, GCC 16.1.0
(MinGW-W64 x86-64 UCRT), `-O2 -std=c23`, 2,000,000 iterations per case, four
runs:

| T | `sizeof(T)` | allocation alone | allocation + store | delta |
| --- | --- | --- | --- | --- |
| `Int32` | 4 | 5.0-15.3 ns | 4.6-16.2 ns | **noise: -0.8 to +3.2 ns, sign changes between runs** |
| 4x`Int64` struct | 32 | 5.6-15.7 ns | 4.4-12.8 ns | **noise: -2.9 to +3.5 ns, sign changes between runs** |
| 1 KiB struct | 1024 | 6.8-19.0 ns | 26.4-39.0 ns | **+17.5 to +21.2 ns, consistent across all four runs** |

**Conclusion: the gate fails, and this RFC is discarded by its own rule.** The
cost is proportional to `sizeof(T)` and is unmeasurable below roughly a
kilobyte — for a scalar or a small struct the store disappears into the
allocator's own 5-15 ns, swinging negative as often as positive.

The one size where the cost is real is excluded by this RFC's own Non-goals.
At a kilobyte the initialization is a `memcpy`, and this RFC scopes itself to
"one uninitialized T", explicitly not "raw byte-count allocation" or "arrays of
uninitialized T". A program wanting a kilobyte of uninitialized storage wants a
buffer, which Hexal serves with `List`, `Array`, and `Slice` — and it would
still have to construct the 1 KiB `initial` value to call `allocate<T>` at all,
so the by-value parameter means the saving is not even the full memcpy unless
the caller's construction is elided too.

That is exactly the discard condition stated above: ordinary initialized
allocation meets the workload without material overhead at every size this RFC
covers.

**The gate as scoped is very likely to fail, and that is worth saying
plainly.** One uninitialized `T` saves exactly one dummy store, which no
measurement will show as material. The case that actually motivates this
feature in Zig and Odin is the uninitialized *counted buffer* — and a counted
buffer is on this RFC's non-goal list. Either the gate should be re-aimed at
the buffer form, or this RFC should be understood as a deliberate parking
decision rather than a live benchmark plan.

## Candidate contract

- T has the same complete finite allocation eligibility as `Heap.allocate<T>`.
- The call requires active lexical unsafe permission.
- Allocation failure retains the existing allocation trap.
- The returned `Ptr<mut T>` is ordinary non-owning pointer state and is released
  explicitly through the matching Heap operation.
- The returned pointer has an uninitialized-state fact distinct from its
  allocation/freed-state fact. Copying or escaping it does not establish that
  any byte has been written.
- The programmer asserts that every observed byte of T receives a valid value
  before any read, copy, comparison, print, call argument, or return that
  semantically reads it.
- **What the compiler actually checks is the unsafe permission, and nothing
  else.** It does not prove write-before-read, does not track which fields
  have been written, and emits no diagnostic for a partially initialized T.
  Stated as accepted behavior rather than left implied: given
  `type Point is struct x: Int32, y: Int32, end`, writing `p.x` and then
  reading `^p` compiles. The read is the programmer's responsibility and
  produces an indeterminate `y`.
- The compiler performs no field-level definite-initialization analysis.
- **Release does not read T.** `Heap.free(p)` releases storage and inspects no
  byte of the object, so freeing a never-written or partially written
  allocation is valid. Hexal has no destructors, so there is no path by which
  release could observe the value. This removes the ambiguity in "release path
  that semantically reads it": there is no such path for `free`.
- **Copying the pointer is always valid.** `let q = p` copies an address, not
  the pointee, so it observes no byte and carries the uninitialized fact to
  `q`. What is invalid is dereferencing either name before the write. Copying
  the *pointee* (`let v: T = ^p`) is a full-width read and is subject to the
  assertion above.
- The semantic reads a promoted spec must enumerate include the ones the
  compiler generates rather than the programmer writes: eager equality-helper
  generation, `print`, and any collection store that copies T by value.
- RFC 0165's alias tracking, if implemented, may diagnose alias-derived
  use-after-free or double-free for this pointer; it must not claim to prove
  initialization.
- Task creation, Channel transfer, and opaque or foreign calls are not escape
  boundaries for local cleanup facts (RFC 0118), so an uninit pointer's
  tracking survives them exactly as an initialized pointer's does. Transfer
  never establishes initialization either way.
- RFC 0158 tracks the physical allocation under its selected runtime backend;
  it does not report whether the allocation was fully initialized.
- A future debug backend may poison or instrument the allocation, but a byte
  pattern is only a diagnostic aid and never establishes initialization. A
  poisoned allocation does not make an uninitialized read memory-safe or
  defined; casts, byte-level access, foreign code, and optimization can all
  bypass or obscure the pattern. Poisoning is optional debug instrumentation,
  never a language guarantee — the same wording RFC 0160 uses for its
  uninitialized-read row.
- No build mode adds a poison fill. A recognizable byte pattern is neither
  initialization nor reliable detection and would change generated C, which
  closed RFC 0187 forbids across modes.
- **This feature therefore ships with no detection of its own bug class.**
  That is the honest consequence of the two rules above and should be weighed
  in the promotion decision rather than discovered during it.
- No implicit drop or destructor is introduced.

## Non-goals

- Aligned allocation; RFC 0189 owns it.
- Raw byte-count allocation, realloc, zeroed allocation, or arrays of
  uninitialized T.
- Flow-sensitive partial-initialization tracking.
- Debug allocation tracking or runtime provenance.
- Stash or Pool uninitialized variants.
- Static leak diagnosis or an ownership/borrow requirement.
- Always-on initialization metadata or runtime overhead in ordinary builds.

## What the discard leaves behind

Nothing to migrate. Every other spec that names this one does so
conditionally — RFC 0160's "Guardrail items (0157 preserves the uninit row)",
RFC 0118's "an uninitialized allocation from RFC 0157, *if that feature is ever
promoted*", RFC 0226's comparison, and RFC 0165's coordinates line. Each says
what must remain true *if* `allocate_uninit` exists. It does not, so those
obligations are discharged rather than orphaned, and their wording stays correct
without edits.

The uninitialized-read row in RFC 0160's coverage matrix stays solved by
construction, which is the outcome those guardrails existed to protect.

## Promotion gate

1. A benchmark must identify the workload that justifies the surface. Record
   allocation size, repetition, generated C, compiler/toolchain, target, and
   measured time.
2. The measured workload must establish whether one uninitialized T is
   sufficient or whether it requires a counted buffer. Do not add both
   speculatively.
3. The promoted spec must define which operations are semantic reads for
   aggregate values without implying field-level checker guarantees.

### An alternative surface, recorded but not evaluated

Not acted on, and deliberately not designed here — noted only so the option is
not lost if this RFC is ever picked up.

Zig and Odin both make uninitialized-ness an *initializer* (`undefined`,
`---`) rather than an allocator method, so it composes with every allocation
form instead of needing one variant each. The Hexal shape would be
`h.allocate<T>(undefined)` inside `unsafe do`, reusing the shipped
`allocate<T>(initial: T)` signature and extending without new surface to
`allocate_aligned`, Stash, Pool, and `let x: T = undefined`. The method-variant
form in this RFC instead needs an `_uninit` twin per entry point, which is
what the Non-goals above are already flinching from when they exclude Stash
and Pool variants.

Which form is right depends on how many allocation entry points eventually
want it, and that is unknown until the motivation gate is attempted. A
promoted spec should compare the two rather than inherit this one.

The recommended safety split is:

- safe initialized allocation remains compile-time valid by construction;
- `allocate_uninit` requires the existing lexical `unsafe` gate;
- no field-level initialization proof is attempted initially;
- selected debug builds may use poison/instrumentation to catch exercised
  reads, but cannot guarantee detection of every uninitialized read.

Until this gate passes, initialized allocation remains the only supported
operation and RFC 0165/0118 must test only compatibility boundaries, not
`allocate_uninit` syntax.

## Promotion requirements

A promoted RFC must replace this section with an exhaustive Validation section
and detailed implementation plan covering:

- exact unsafe and allocation diagnostics;
- complete type eligibility;
- write-before-read examples for scalar, struct, and collection-containing T;
- generated C using non-zeroing allocation with no poison fill;
- release behavior and existing local freed-state checks;
- alias-derived freed-state behavior and task/opaque escape behavior from
  RFCs 0118 and 0165;
- the fact that leak reporting is runtime-only through RFC 0158;
- manifest impact; and
- reference synchronization after explicit approval.
