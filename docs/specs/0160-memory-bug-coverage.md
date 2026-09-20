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
  0156 (arithmetic and foreign-write hazards), RFC 0157 (explicit uninit
  allocation), RFC 0158 (test-time tracking allocator), RFC 0039 (foreign
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

## Goals

- Fix the inventory as the definition of done for memory safety in safe
  Hexal: every row states its mechanism, not its aspiration.
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
| Use-after-free | Direct locally tracked bindings are rejected. A pointer copied to a second binding is intentionally not tracked, and the current reference documents that limitation | RFC 0165 may add conservative intraprocedural alias facts. Debug fill-after-free belongs to RFC 0158's runtime-debugging scope |
| Memory leak | No static or production leak diagnosis. A process-exit report is not currently implemented; mimalloc statistics in measurement tests are not a leak tracker | RFC 0158 owns opt-in physical-allocation tracking. Static non-escaping leak diagnosis is deferred |
| Double free | Direct locally tracked bindings are rejected. Copies are intentionally not tracked | RFC 0165 may add conservative intraprocedural alias facts. Debug tombstones belong to RFC 0158 |
| Null dereference | Solved. Nullable must be narrowed before calls, method dispatch, and View construction (`checker/methods.go`, `checker/views_bridge.go`) | None. Row is closed; proposals must not reopen it |
| Invalid free | Local/ref-derived `Heap.free` arguments are rejected by the checker. Unknown or foreign addresses remain outside local proof | Keep the local rejection. Foreign-pointer ownership remains RFC 0039's responsibility; do not describe this row as wholly solved |
| Stack buffer overflow | Safe Array indexing and slicing are checked; foreign writes remain outside the language proof | None in-language. Foreign writes through exported pointers are RFC 0039's |
| Heap buffer overflow | Safe List/Array indexing and slicing are checked; escaped or foreign writes remain outside the language proof | None in-language. Same foreign carve-out |
| Out-of-bounds read | Solved for the checked Array/List/String/View paths. Constant Array indices are already rejected; unknown cases trap at runtime | Broader constant folding is optional. It must not be described as entirely unimplemented |
| Off-by-one | Half solved. `for...in` is exact by construction; checked indexing converts mistakes to traps | Compiler-side advisory warning for suspicious `<=` or `==` against `.length()`. It remains non-fatal and may have false positives |
| Uninitialized read | Solved by construction. Mandatory initializers; `allocate<T>(initial)`; complete aggregate construction | None, plus a guardrail: RFC 0157's uninit escape must preserve this row — unsafe-gated allocation with write-before-read/move/drop preconditions and debug fill, default untouched |
| Missing NUL terminator | Solved structurally. `hex_string` is `{data, byte_length, rune_length}` (`generator/packages/string.h`); `Strand` is inline data — length, never sentinel | Confine NUL creation to C-boundary conversion ops (single audited sites each). Safe code cannot spell the bug |

## Graduation

- Checker items (copy-propagated freed facts, `free(ref x)` rejection,
  literal-index const-fold, loop-bound lint): one focused checker RFC, or
  two if the lint's advisory-only nature wants separation from hard errors.
- Runtime items (fill-after-free, free-list tombstones): the debug-backend
  vehicle alongside RFC 0158's tracking allocator.
- Tooling items (tracking allocator, leak reports): RFC 0158's vehicle.
- Guardrail items (0157 preserves the uninit row; 0153 preserves the
  out-of-bounds row): acceptance criteria on those RFCs, not work here.
- Foreign items: RFC 0039's vehicle.

## Acceptance sketch

Non-exhaustive: each graduating vehicle promotes its items to an exhaustive
Validation section.

- `let q = p; free(p)` marks every provenance-sharing copy freed; use or
  second free through any of them fails at compile time.
- `free` of a ref-derived or stack address fails at compile time; Stash
  behavior unchanged.
- A program leaking under the tracking backend reports every outstanding
  allocation with its source site; a clean program reports nothing.
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
2. The loop-bound lint belongs in the compiler as a non-fatal advisory warning.
   Its exact warning text, suppression mechanism, and enable/disable policy
   remain to be specified.
3. Whether fill-after-free and tombstones should be one debug backend with
   RFC 0158's tracking or three independent toggles. One backend is simpler;
   toggles compose better with targeted performance work.
4. Static non-escaping leak diagnosis is deferred. RFC 0158's opt-in runtime
   tracker owns leak reporting until Hexal has an explicit syntax for
   intentional process-lifetime allocations.
