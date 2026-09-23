# RFC 0160: Memory-Bug Coverage Without Ownership Semantics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion (proposal); not scheduled. Items graduate
  independently: checker items to focused checker RFCs, tooling items to
  RFC 0158's vehicle, foreign-write items to RFC 0039
- Created: 2026-09-10
- Updated: 2026-09-20
- Depends on: nothing; this RFC records implemented behavior plus
  small deltas against it
- Coordinates with: RFC 0155 (unsafe posture for the uninit escape), RFC
  0156 (arithmetic and foreign-write hazards), archived RFC 0157 (explicit
  uninit allocation, discarded on its benchmark gate), RFC 0158 (leak detection
  on the Linux lane), RFC 0165 (alias
  diagnosis), RFC 0225 (cross-allocator release), RFC 0039 (foreign
  contracts)
- Does not update `docs/reference.md`

## Summary

Hexal's compiler follows a specific philosophy: **report locally, trap
loudly, never fix silently** — flow facts and runtime traps, with no lifetime
system. This document is an inventory, not an implementation vehicle. It
records current behavior and routes remaining work to focused specs. The
governing constraint is unchanged: no item may introduce borrow checking,
lifetime inference, region variables, or move semantics.

## Problem

Memory-safety discussions must not import Rust's model wholesale. The current
compiler already prevents several capabilities by construction (mandatory
initialization, checked indexing, length-carrying text) and rejects some local
cleanup misuse. The remaining holes are deliberately conservative: copied
pointer bindings, escaped values, opaque calls, and physical leaks are not
fully decided statically. This inventory must distinguish implemented behavior
from proposed work so that a completed row is not re-opened accidentally.

This document is a coordination map. It is not an implementation vehicle and
must not be implemented from: every row's real definition of done lives in the
owning spec named in its Delta column. A row marked solved means **solved for
the checked safe-language operations** — unsafe entry, foreign writes, and
escaped aliases stay outside every such claim, and the row wording says so
rather than relying on the reader to remember it.

## Goals

- Fix the inventory as the shared picture of memory safety in safe Hexal:
  every row states its mechanism, not its aspiration.
- Keep every delta local and syntactic, or runtime, or test-time — never a
  new type-system dimension.
- Protect solved rows: any proposal touching allocation, slicing, text, or
  initialization must show its row stays solved.
- Graduate items independently; no item blocks another.

## Non-goals

- Any ownership, borrow, lifetime, region, or move machinery.
- Whole-program or interprocedural analysis in the compiler.
- Changing trap behavior, text representation, or initialization defaults.
- Foreign-memory safety: foreign-write hazards are RFC 0039's, gated by
  RFC 0155 in the meantime.

## Coverage matrix

"Today" means the implemented compiler only. File coordinates name the
mechanism, not the whole implementation.

| Bug | Status today | Delta (this RFC) |
|---|---|---|
| Use-after-free | Direct locally tracked bindings are rejected. A pointer copied to a second binding is intentionally not tracked, and the current reference documents that limitation. Since RFC 0156 closed, `p.offset(n)` and `p.cast<U>()` inside `unsafe` are two further untracked alias sources | RFC 0165 may add conservative intraprocedural alias facts for the `let q = p` form. Whether `.offset`/`.cast` results inherit their base's identity is deliberately left open by RFC 0165 and wants its own spec. Debug fill-after-free belongs to RFC 0158's runtime-debugging scope |
| Memory leak | No static or production leak diagnosis. A process-exit report is not currently implemented; mimalloc statistics in measurement tests are not a leak tracker | RFC 0158 owns opt-in physical-allocation tracking. Static non-escaping leak diagnosis is deferred |
| Double free | Direct locally tracked bindings are rejected. Copies are intentionally not tracked | RFC 0165 may add conservative intraprocedural alias facts. Debug tombstones belong to RFC 0158 |
| Null dereference | Solved for the checked safe-language operations. Nullable must be narrowed before member access and method dispatch (`checker/methods.go`) | None. Row is closed; proposals must not reopen it |
| Invalid free | Implemented: `Heap.free` of an argument traceable to `@` of local storage is rejected (`free does not accept a pointer into this function's local storage`). Unknown or foreign addresses remain outside local proof. **Cross-allocator release is not rejected**: `h.free(pool_ptr)`, `h.free(stash_ptr)`, and `pool.free(heap_ptr)` are all accepted today | RFC 0225 owns cross-allocator rejection, extending the provenance edge `checker/pool.go` already reads. Foreign-pointer ownership remains RFC 0039's |
| Stack buffer overflow | Safe Array indexing and slicing are checked; foreign writes remain outside the language proof | None in-language. Foreign writes through exported pointers are RFC 0039's |
| Heap buffer overflow | Safe List/Array indexing and slicing are checked; escaped or foreign writes remain outside the language proof | None in-language. Same foreign carve-out |
| Out-of-bounds read | Solved for the checked Array/Slice/List/String paths. Constant Array indices are already rejected; unknown cases trap at runtime | Broader constant folding is optional. It must not be described as entirely unimplemented. Guardrail: `Array<T, N>`'s contribution to this row is that N *is* the length, so every in-range index is valid by construction. A proposal replacing it with a capacity-plus-runtime-length form (RFC 0226) weakens that to "an index `>= N` is still statically wrong", and must show the row stays solved or record the regression |
| Off-by-one | Half solved. `for...in` is exact by construction; checked indexing converts mistakes to traps | Compiler-side advisory warning for suspicious `<=` or `==` against `.length()`. It remains non-fatal and may have false positives |
| Uninitialized read | Solved by construction for safe code. Mandatory initializers; `allocate<T>(initial)`; complete aggregate construction | None, plus a guardrail: RFC 0157's uninit escape must **confine** this row rather than preserve it. An unsafe-gated uninitialized allocation creates exactly the bug this row calls solved; what must be preserved is that safe code cannot spell it. RFC 0157 owns the write-before-read contract as a programmer assertion, not a checker proof, and adds no fill in any build mode. A second guardrail applies to any proposal adding a partially-filled inline container (RFC 0226): because Hexal has no default-value concept, capacity beyond the logical length holds no valid T, and a by-value copy of the container reads it. Such a proposal must show this row stays solved in **safe** code, where RFC 0157's unsafe gate is not available to confine it |
| Missing NUL terminator | Solved for the checked safe-language operations, by two different mechanisms. `String` carries its length: `hex_string` is `{data, byte_length, storage_kind}` (`generator/packages/string.h`), never a sentinel. It is a byte count, not a scalar count — RFC 0224 made text byte-oriented and removed the cached `rune_length` field, so `rune_length()` decodes on demand and no length in the header can disagree with the bytes. `Strand` is **not** length-carrying — it is 32 inline bytes holding at most 31 UTF-8 payload bytes, a NUL, then zero fill, and `Strand.length()` scans bounded by those 31 bytes. Safe code still cannot spell the bug, because the terminator and the bound are both structural and neither is caller-supplied | Confine NUL creation to C-boundary conversion ops (single audited sites each). Any proposal that generalizes `Strand`'s capacity must keep the bound structural: a scanned terminator is safe at one fixed width and stops being obviously safe once the width is a parameter |

## Detection phases

The inventory uses three distinct outcomes:

| Outcome | Meaning |
|---|---|
| Compile-time | The checker proves a violation from local syntax, type, provenance, or flow facts and rejects it |
| Runtime/debug | The program or selected debug backend detects a dynamic violation or surviving physical allocation |
| Undecided | The value escaped, entered foreign/unsafe code, or requires facts unavailable to the checker; the program remains accepted |

Compile-time checks include null narrowing, direct invalid/local frees,
direct locally tracked use-after-free and double-free, constant bounds cases,
and unsafe-operation gates. RFC 0165 may extend only the local alias portion.

Runtime/debug checks include dynamic collection bounds, dynamic Pool slot
validation, allocation failure, RFC 0158 leak reports, and optional debug
freed-state, quarantine, tombstone, and sanitizer checks. None of these
runtime facilities introduces ownership or lifetime syntax.

Uninitialized reads from a future `allocate_uninit` remain RFC 0157's unsafe
contract. Foreign-pointer correctness, raw-pointer bounds after unsafe entry,
and cross-task correctness remain conservative/contract-based rather than
being silently promoted to compile-time guarantees.

## Graduation

- Checker items (copy-propagated freed facts, literal-index const-fold,
  loop-bound lint): one focused checker RFC, or two if the lint's
  advisory-only nature wants separation from hard errors. Cross-allocator
  release has already graduated to RFC 0225. Rejecting `Heap.free` of an
  address taken with `@` is **not** on this list: it is implemented and
  verified, and listing it invites re-opening a closed row.
- Runtime items (fill-after-free, free-list tombstones): the debug-backend
  vehicle alongside RFC 0158's leak lane.
- Tooling items (leak reports): RFC 0158's vehicle, now a LeakSanitizer gate on
  the Linux lane rather than a tracking allocator. The allocator was abandoned
  after a sweep found the vendored mimalloc cannot enumerate the runtime's own
  allocations.
- Guardrail items (0157 preserves the uninit row; 0153 preserves the
  out-of-bounds row): acceptance criteria on those RFCs, not work here.
- Foreign items: RFC 0039's vehicle.

## Acceptance sketch

Non-exhaustive: each graduating vehicle promotes its items to an exhaustive
Validation section.

- `let q = p; free(p)` marks every provenance-sharing copy freed; use or
  second free through any of them fails at compile time.
- `Heap.free` of a pointer whose local provenance names a Stash or Pool fails
  at compile time, and the mirror case (`pool.free` of a Heap pointer) fails
  the same way.
- A program leaking under the tracking backend reports every outstanding
  allocation with the allocation stack trace LeakSanitizer supplies under RFC
  0158, which is stronger attribution than the native-stack/debug-symbol
  compromise the tracking-allocator design had settled for;
  a clean program reports nothing.
- Literal out-of-range indices fail at compile time where folded, trap
  otherwise; no previously compiling program changes meaning.
- The loop-bound lint fires only on the stated syntactic pattern and is
  advisory; it blocks nothing.
- Every row marked solved above has a regression test naming the mechanism;
  RFCs 0153 and 0157 each carry an item showing their rows stay solved.

## Current verification boundary

The following claims were verified against the compiler on 2026-09-20:

- direct use-after-free is rejected;
- a local/ref-derived argument to `Heap.free` is rejected;
- a constant out-of-range Array index is rejected;
- copied-pointer use-after-free and copied-pointer double-free are accepted;
- a leaked allocation is accepted;
- the checker and integration packages pass their pure-Go tests.

These observations describe the current implementation. They do not promote
any future delta to complete.

## Open questions

1. Whether copy-propagated freed facts should survive one function boundary
   for same-module private callees, or stay strictly intraprocedural.
   Strictly intraprocedural is recommended; anything wider is analysis
   creep toward the model this RFC refuses.
2. **The loop-bound lint has no channel to report on, and that is a larger
   question than the lint.** `CompilationResult` carries `Files`,
   `Dependencies`, `Stderr`, `ExitCode`, and `Stats`; `Stderr` is documented
   as "every diagnostic of the failing stage, already rendered and ordered; it
   is empty on success." There is no non-fatal diagnostic path anywhere in
   the string-in/string-out boundary, so an advisory warning cannot be
   expressed without adding one. That addition touches the compiler's public
   API, the driver, the workbench, and the fail-closed architecture rule, and
   it is not this RFC's to make. Until a warnings channel exists, the lint
   cannot graduate. Its exact pattern, text, location, suppression mechanism,
   and enable/disable policy remain unspecified behind that blocker.

   The lint is also the weakest item in this inventory on its own merits:
   `i <= values.length()` is not wrong in general, checked indexing already
   converts the mistake to a trap rather than corruption, and a warning that
   fires on correct code is the kind of noise language goal 13 argues
   against. Consider whether the cross-allocator free rejection is the better
   use of the same graduation slot.
3. Whether fill-after-free and tombstones should be one debug backend with
   RFC 0158's tracking or three independent toggles. One backend is simpler;
   toggles compose better with targeted performance work.
4. Static non-escaping leak diagnosis is deferred. RFC 0158's opt-in runtime
   tracker owns leak reporting until Hexal has an explicit syntax for
   intentional process-lifetime allocations.
