# RFC 0160: Memory-Bug Coverage Without Ownership Semantics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion (proposal); not scheduled. Items graduate
  independently: checker items to focused checker RFCs, tooling items to
  RFC 0158's vehicle, foreign-write items to RFC 0039
- Created: 2026-09-10
- Updated: 2026-09-10
- Depends on: nothing; this RFC records implemented behavior plus
  small deltas against it
- Coordinates with: RFC 0155 (unsafe posture for the uninit escape), RFC
  0156 (arithmetic and foreign-write hazards), RFC 0157 (explicit uninit
  allocation), RFC 0158 (test-time tracking allocator), RFC 0039 (foreign
  contracts)
- Does not update `docs/reference.md`

## Summary

Hexal's compiler already follows a specific philosophy that was never
written down: **report locally, trap loudly, never fix silently** — flow
facts and runtime traps, no lifetimes anywhere. This proposal inventories
eleven classic memory bugs against the implemented compiler, records which
are already solved (and how), and proposes the smallest delta for each open
one. The governing constraint: no item may introduce borrow checking,
lifetime inference, region variables, or move semantics. The most
sophisticated mechanism proposed here is intraprocedural propagation of
facts the checker already computes.

## Problem

Memory-safety discussions default to importing Rust's model wholesale. That
would contradict the language goals (small surface, low ceremony) and is
unnecessary: most of the eleven bugs below are already solved by
construction (no arithmetic, mandatory initialization, length-based text) or
by existing checker machinery (freed-state facts, narrowing, index traps).
The residual holes are narrow — an untracked copy here, an ungated argument
position there, whole-program facts exiled to nowhere — and each has a
narrow fix. Without this inventory, future proposals risk re-solving solved
bugs with heavy machinery or, worse, regressing solved rows (uninit
allocation must not reopen uninit reads; storable slices must not reopen
out-of-bounds reads).

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
| Use-after-free | Mostly solved. `flowState` freed-facts with versions and branch merges (`checker/scope.go`, `checker/alloc.go`) reject use-after-free on locally tracked bindings | Propagate freed facts through copies sharing provenance instead of dropping them on copy (`declarations.go` `dropFreed` is the hole: `q := p; free(p); use(q)` compiles). Still intraprocedural, still syntactic. Debug fill-after-free as dev backstop |
| Memory leak | Open. No detection anywhere | No language change. Test-time tracking allocator per RFC 0158: record allocations, report sites at exit. The compiler "tells" through the test report |
| Double free | Mostly solved. Same freed-state machinery rejects it (`checker/alloc_test.go`); Pool adds runtime live-slot checks | Same copy-propagation fix as use-after-free. Debug-heap free-list tombstones so a missed case traps instead of corrupting |
| Null dereference | Solved. Nullable must be narrowed before calls, method dispatch, and View construction (`checker/methods.go`, `checker/views_bridge.go`) | None. Row is closed; proposals must not reopen it |
| Invalid free | Partial. Structurally impossible for Stash; `Heap.free` provenance unchecked | Reuse the existing ref-trace provenance (already rejecting ref-derived View construction) at `Heap.free` argument position: reject `free(ref x)` at compile time |
| Stack buffer overflow | Solved structurally. No arithmetic exists; `Array` index/slice ops trap (`generator/packages/array.h`) | None in-language. Foreign writes through exported pointers are RFC 0039's |
| Heap buffer overflow | Solved structurally. Same traps on `List` (`generator/packages/list.h`) | None in-language. Same foreign carve-out |
| Out-of-bounds read | Solved. Every index and slice path across Array/List/String/View traps | Optional const-fold for literal indices (pure checker win). Runtime traps already make every case loud |
| Off-by-one | Half solved. `for...in` is exact by construction; checked indexing converts mistakes to traps | Advisory syntactic lint only: flag `<=` / `==` against `.length()` in loop conditions. Tells, doesn't fix; zero false positives by construction |
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

- `q := p; free(p)` marks every provenance-sharing copy freed; use or
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

## Open questions

1. Whether copy-propagated freed facts should survive one function boundary
   for same-module private callees, or stay strictly intraprocedural.
   Strictly intraprocedural is recommended; anything wider is analysis
   creep toward the model this RFC refuses.
2. Whether the loop-bound lint belongs in the compiler at all or in the
   workbench as editor feedback. Compiler placement reaches every driver;
   workbench placement keeps diagnostics purely advisory without a flag.
3. Whether fill-after-free and tombstones should be one debug backend with
   RFC 0158's tracking or three independent toggles. One backend is simpler;
   toggles compose better with targeted performance work.
