# RFC 0131: Generated-C Conformance Bug Sweep

- Kind: Execution Plan
- Status: Closed; implemented 2026-08-27. All five targeted probes now
  compile clean under GCC 16.1, Clang 22.1.8, and Zig 0.16 with
  `-std=c23 -Wall -Wextra -Werror`, no warning suppressed: `concurrency-cpu-
  saturating-fibers` (`List<Task<Int64>>`) and a hand-probed
  `List<Channel<Int32>>` case; `collections-handle-elements`;
  `collections-nested-list`; `streams-generic-drain`; `types-shape-area`.
  Fixes, by required-change section: (1) centralize value spelling --
  `array_component.go` and `view_component.go` spelled their element storage
  through `pointerSpelling`, which walks a type's own `.Element` chain and
  is wrong for String (String sets `.Element` to UInt8 for unrelated byte-
  view support, so this produced `const uint8_t *` storage for a `String`
  array/view element); switched both to `typeSpelling`, already correct in
  `list_component.go` and `dict_component.go`. `hexal/concurrency.h` (owning
  the per-instantiation `hex_task_T`/`hex_chan_T` pointer typedefs) was
  included after `list`/`dict`/`array` in the fixed per-module include
  order, so a container specializing over Task or Channel referenced an
  undeclared type name; moved it immediately after `seek` and before the
  generic containers. (2) representation-aware equality --
  `writeEqualityComparisons`'s List/Array/View/Object/Adt/Union recursion
  cases always emitted `.`-field access for a compared member, which is
  wrong when the member is itself a stored handle (List, Dict, Mutex): the
  member is spelled as a pointer in its parent, exactly like a source
  binding of that type, so a nested `List<List<T>>`'s inner list needs
  `->`. Added `equalityOperand`, wrapping every recursive member expression
  with one dereference when the member type is a handle (String passes
  through unchanged: its dedicated `hex_equal_hex_string` case already wants
  the pointer directly). The same value/handle confusion existed in
  `for.go`'s List and Dict binder declarations (`valueBinder.Type.CName` /
  `keyType.CName` / `valueType.CName`, always a bare, non-pointer spelling)
  and in `types-shape-area`'s `-Wmaybe-uninitialized`: GCC's dataflow
  couldn't correlate a match arm's payload read on the original scrutinee
  variable with the tag check performed on the separately hoisted
  `hex_match_scrutinee_N` copy of the same value, so `renderMatchStatement`
  now temporarily rebinds the scrutinee's own binding name to the hoisted
  temp for the duration of the arms, making every read within one match
  resolve to the same C value the tag was checked against -- not a
  suppression, and confirmed by GCC's warning disappearing once tag check
  and payload read are provably the same object. (3) generic prototype
  ordering -- concrete specialization prototypes (`writeSpecializedPrototypes`)
  were emitted after every ordinary function/method definition in
  `emitModulePair`, so an ordinary function whose body was the first to call
  a given specialization (as `run` is, ahead of `drain`'s own two call sites,
  in `streams-generic-drain`) compiled with no declaration in scope, a C23
  hard error; moved the call immediately after `writeModulePrototypes`, so
  every prototype exists before any definition, while specialization
  definitions still follow every ordinary definition as before. (4) flow
  narrowing at contextual boundaries -- two related but distinct defects.
  First, `try`'s own operand: `hoistTry` declared its hoisted temp using the
  checker's (possibly flow-narrowed) `OperandType`, but the real C variable
  a narrowed read refers to keeps its original wider declared type, so the
  temp's initializer was a struct-type mismatch whenever narrowing had
  shrunk the union (reproduced by `streams-seek-and-eos`'s `try` after a
  prior `is EoS` check). Added `OperandStorageType` to the checker's
  `TryExpression` node (from a new `checkedExpression.storageType`,
  populated by `checkPlace` and now propagated through `valueFromPlace`),
  and `hoistTry` declares its temp with that real type when set; tag values
  and payload field names are shared across every union containing a given
  member, so every other read off the now-correctly-typed temp is
  unaffected. Second, the general case RFC 0131 actually named --
  `streams-generic-drain`'s `return n` after `if n is EoS then return end`
  hit the same physical-vs-logical mismatch at a return boundary, where
  `injectIntoUnion`'s existing widen machinery didn't fire because the
  checker's narrowed type already equaled the declared destination (only
  the physical storage differed). Added `reconcilePhysicalRepresentation`,
  called from `checkInitializer` (the one shared function behind
  declarations, assignments, call arguments, and returns) after
  `injectIntoUnion`: when a checked value's `storageType` is a union
  differing from the destination even though its logical `typ` already
  matches, it emits a `UnionWidenExpression` sourced from the real storage
  type, letting a member with no destination counterpart (proven
  unreachable by the narrowing that produced the binding) fall through to
  the existing widen helper's own `default: abort()` -- required relaxing
  `validateUnionWiden`'s member-map check to accept that intentional `-1`
  entry rather than rejecting it as malformed. (5) strict-C ADT lowering --
  the ADT equality catch-all `abort()` this worktree's `-Wswitch` fix added
  earlier the same day never triggered `<stdlib.h>` selection because
  `unionEqualityUsed` (renamed `abortingEqualityUsed`) checked only for
  union equality, not ADT; `types-shape-area`'s subsequent genuine
  `-Wmaybe-uninitialized` is the match-scrutinee-rebinding fix above, not a
  suppression.
  Two further defects surfaced only once this closure filled in
  previously-missing manifest entries for three `try`-related fixtures added
  earlier the same day but never before compiled under a real toolchain:
  `hoistTry`'s multi-success-member path declared its `hex_try_result_N`
  temp `const` while assigning it inside every `switch` case (always
  invalid, regardless of any narrowing) -- fixed by declaring it mutable,
  matching `renderMatchStatement`'s identical `hex_match_result_N` pattern.
  Fixing that first exposed a second, older defect literally two lines away:
  the multi-success path's `builder.Reset()` -- which discards the
  single-return-shape prologue already accumulated above so a
  union-return-shape prologue can be rebuilt from scratch -- was mistaken
  for dead code during the `OperandStorageType` edit and deleted, which
  duplicated the entire operand-hoist-and-Error-check block verbatim into
  the output; restored the reset. While rebuilding that prologue, also
  found and fixed the rebuilt copy's defer-unwind statement ordering, which
  wrote the `return` before calling `unwindAllDefers` (unlike the correct,
  first-written copy above it), silently making any pending `defer`/
  `errdefer` on this path dead code after the return -- reordered to unwind
  before returning, matching the working copy and the "the unwind must
  precede the return so it executes" invariant already documented there.
  Validation: all five targeted probes plus thirteen related snippets
  (every snippet whose generated C changed under these fixes -- the Task/
  Channel/Mutex/Atomic-using snippets from the concurrency-include reorder,
  the ADT/union-switch snippets from the earlier same-day `-Wswitch` fix,
  and the three new `try` fixtures) were re-verified individually and
  together under all three toolchains; `gofmt`, `go build`, `go vet`,
  `go vet -tags c23`, and `go test ./...` (unit, integration, fuzz) all
  pass; the snippet manifest was rebuilt once and its diff reviewed
  artifact-by-artifact against exactly this set, no unexpected change.
  Not independently re-confirmed: a full, untargeted
  `go test -tags c23 -run TestC23SnippetCatalogCompiles` sweep of the
  complete ~150-snippet catalog did not complete on the authoring host --
  it ran past 50 minutes compiling roughly 450 external toolchain
  invocations without asserting a single failure before being killed by the
  test runner's own timeout, consistent with the process-spawn contention
  this same host showed during RFC 0125's own authoring, not a defect in
  this RFC's changes. `docs/reference.md` already states the value-
  representation contract these fixes restore ("`String`, List, Dict, Task,
  Channel, Mutex copy their handle representation") and required no edit.
- Created: 2026-08-27
- Updated: 2026-08-27
- Implements: the current `docs/reference.md` contracts without changing the
  language surface
- Coordinates with: RFC 0125 (external C23 validation)
- Does not own: scheduler startup or runtime scheduling semantics; RFC 0132
  owns that architectural defect

## Summary

Fix the remaining bounded generator defects exposed by compiling the snippet
catalog as strict C23. The compiler already accepts each source program; its
generated C is invalid or incomplete.

This RFC changes no Hexal syntax, types, APIs, representations, or semantics.
It makes emitted C implement already-checked programs correctly.

## Verified current failures

The targeted external command is:

```text
go test -tags c23 -run TestC23SnippetCatalogCompiles/<snippet> -count=1 ./compiler/tests/c23validation
```

On 2026-08-27, GCC 16.1, Clang 22.1.8, and Zig 0.16 reproduced:

1. `concurrency-cpu-saturating-fibers`
   - `List<Task<Int64>>` emits the unknown storage type `hex_task_Int64`.
   - The same missing value-representation rule applies to
     `List<Channel<T>>`.
2. `collections-handle-elements`
   - `Array<String, 2>` stores `hex_string` objects instead of String handles.
   - String equality and `List<String>.push` consequently receive values where
     `const hex_string *` is required.
3. `collections-nested-list`
   - nested List equality uses `.` on stored List pointers;
   - iteration drops or adds pointer indirection inconsistently.
4. `streams-generic-drain`
   - ordinary function `run` calls concrete `drain<IO>` and
     `drain<MutPtr<Bytes>>` specializations before their prototypes;
   - returning a flow-narrowed `Size | Error` binding emits the original
     `Size | EoS | Error` C wrapper without the checked union widening.
5. `types-shape-area`
   - the current worktree's ADT equality catch-all calls `abort()` without
     selecting `<stdlib.h>`;
   - after that prerequisite is corrected, the existing
     `-Wmaybe-uninitialized` payload-access finding must be re-probed and fixed
     rather than suppressed.

## Re-verified exclusions

These status findings do not belong in this RFC:

- `streams-bytes-memory` and `streams-seek-and-eos` both pass all three C23
  toolchains in the current worktree. The built-in Seek ownership and payload
  mismatch no longer reproduces.
- `text-protocol-parser` passes all three toolchains. The recorded match-switch
  warning no longer reproduces.
- pointer-indirect self-recursive object construction is valid per the
  reference; no terminating-construction bug is defined.
- scattered parser expression-start classification is a possible refactor,
  not a reproduced compiler defect.

If an excluded external probe fails again before implementation begins, stop
and refresh this RFC rather than silently absorbing it.

## Invariants

1. One authoritative C value-spelling operation covers every source value
   position: bindings, parameters, results, aggregate storage, collection
   helpers, iteration binders, equality operands, and nested values.
2. Handle types retain their source value representation in aggregates:
   - String: `const hex_string *`;
   - List/Dict/Mutex: pointer to the concrete control type;
   - Task: `hex_task *`;
   - Channel: `hex_chan *`.
3. Task and Channel storage does not append another pointer to a typedef that
   already denotes a pointer and does not depend on a specialization typedef
   being declared before a shared collection component.
4. C naming identity remains separate from C value spelling. Collection
   suffixes still encode the complete Hexal type even when two handle families
   share one runtime pointer representation.
5. Equality recursion uses the representation of the value being traversed.
   A stored handle is dereferenced as a handle; an inline value is accessed
   inline.
6. Every concrete generic specialization prototype precedes every ordinary or
   specialized definition that may call it.
7. A flow-narrowed union used at an assignment, argument, return, or other
   contextual boundary receives the same explicit checked union
   injection/widening node. The generator never relies on two distinct C
   wrappers being assignment-compatible.
8. A generated `switch` over program-wide `hex_tag` either enumerates the
   relevant states and has a compiler-internal catch-all, or otherwise avoids
   `-Wswitch`. A direct `abort()` catch-all selects `<stdlib.h>` through normal
   demand discovery, as permitted by the reference.
9. Valid ADT payload reads compile under strict warnings without warning
   suppression. Generated initialization or control flow must make the
   checker's exhaustiveness fact visible to the C compiler.
10. No external warning is disabled to make a fixture pass.

## Required changes

### 1. Centralize value representation spelling

- Replace family-specific assumptions spread across `declaration`,
  `typeSpelling`, `pointerSpelling`, collection render models, and loop
  binders with one value-representation classification.
- Keep declarator construction separate where C syntax requires it; all such
  construction consumes the same classification.
- Make Array, View, and List element models consume value spelling, not nominal
  `CName` or pointee spelling.
- Spell Task and Channel stored values through the base runtime pointer types.
- Do not change canonical type identity, helper suffixes, or public Hexal type
  checking.

### 2. Make recursive equality representation-aware

- Preserve structural equality semantics.
- Pass whether each recursive operand is inline or a handle, or delegate a
  nested comparable value to its already-generated equality helper.
- String equality receives String handles directly.
- Nested List equality follows stored List pointers before reading length or
  data.
- Emit no pointer-identity comparison for List or String contents.

### 3. Order generic prototypes before consumers

- Emit ordinary module prototypes first.
- Emit every concrete generic function and method prototype next.
- Only then emit ordinary definitions, specialized definitions, local helper
  definitions, adapters, and root statements.
- Preserve deterministic specialization ordering and existing linkage.

### 4. Preserve flow narrowing at C representation boundaries

- Reuse `injectIntoUnion` after the initializer has been checked with current
  flow facts.
- Ensure the checked operand stored in `ReturnStatement` and equivalent
  contextual boundaries contains `UnionInjectionExpression` or
  `UnionWidenExpression` whenever its narrowed type differs from the declared
  destination.
- Do not add generator-side type inference or reconstruct checker flow.

### 5. Complete strict-C ADT lowering

- Include `<stdlib.h>` whenever a newly reachable header-local catch-all calls
  `abort()`.
- Re-probe `types-shape-area` after that fix.
- If GCC still reports `-Wmaybe-uninitialized`, make the generated match result
  or payload path definitely initialized by construction. Do not add
  `-Wno-maybe-uninitialized`, a volatile read, or a fake runtime branch.

## Required sweep

- Inventory every use of `CName`, `pointerSpelling`, and `typeSpelling` in
  Array/View/List storage and element operations; retain no second spelling
  table for the same value-position question.
- Cover Task, Channel, Mutex, String, List, Dict, pointer, inline aggregate,
  and nested collection values in the spelling table tests.
- Remove tests that encode the defective struct-versus-handle spellings.
- Keep source-order and deterministic-output tests; only the declaration
  regions necessary for C23 correctness may move.
- Keep the external suite's warning flags unchanged.

## Detailed implementation plan

### Phase 0: freeze evidence

1. Record the current snippet manifest and ordinary Go test/vet baseline.
2. Run the five failing targeted probes above and retain their exact compiler
   diagnostics.
3. Run the three excluded probes and require them to remain green.
4. Inventory all representation-spelling callers before changing a shared
   helper.

### Phase 1: value spelling and collection storage

1. Add the single value-representation classification and focused unit tests.
2. Route declarations and parameter/result spelling through it without changing
   ordinary scalar or pointer output.
3. Route Array/View/List component records, accessors, push/pop, iteration, and
   literal lowering through it.
4. Fix Task/Channel base-pointer storage without changing type-identity suffixes.
5. Run the Task/Channel, String-handle, and nested-List targeted probes.

### Phase 2: equality

1. Make recursive equality use inline-versus-handle operand facts.
2. Assert exact String call arguments and nested-List pointer access in emitted
   C.
3. Verify equality helpers remain demand-driven and defined once.

### Phase 3: generic ordering and narrowing

1. Move concrete specialization prototypes ahead of all possible consumers.
2. Add text assertions that each call is preceded by exactly one compatible
   prototype.
3. Insert checker-owned union injection/widening at narrowed contextual
   boundaries.
4. Re-run `streams-generic-drain` and add a smaller focused narrowing return
   fixture.

### Phase 4: ADT strict-C completion

1. Repair standard-header demand for header-local `abort()`.
2. Re-run `types-shape-area` to expose the payload-initialization result.
3. Make definite initialization visible in generated C if the warning remains.
4. Assert that no warning suppression was added.

### Phase 5: conformance and handoff

1. Run every Validation item and the full C23 snippet catalog under all three
   toolchains.
2. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
3. Rebuild the snippet manifest once. Review every changed artifact; no source
   semantics changed.
4. Review `docs/reference.md`. No semantic edit is expected; if the existing
   representation contract is incomplete, request explicit approval before
   changing it.
5. Remove RFC 0131's status row only after every targeted and catalog-wide
   external probe is green.

## Validation

This section is exhaustive.

- All five currently failing targeted snippet probes compile under GCC, Clang,
  and Zig with `-std=c23 -Wall -Wextra -Werror`.
- The two Seek probes and `text-protocol-parser` remain green.
- Array and List storage for String uses String handles; String equality and
  push calls receive pointer values of the declared parameter type.
- List storage for Task and Channel uses exactly one runtime-handle pointer
  level and does not require a later specialization typedef.
- Nested List storage, iteration, access, and equality use consistent pointer
  depth and structural equality.
- Each used concrete generic specialization has exactly one compatible
  prototype before its first call and exactly one definition.
- Returning a flow-narrowed union emits the required union conversion and never
  returns one C wrapper as another.
- Every emitted direct `abort()` has a visible declaration through demand-driven
  standard-header selection.
- `types-shape-area` produces no `-Wmaybe-uninitialized` warning.
- No warning suppression, semantic checker relaxation, placeholder, or hidden
  fallback is added.
- Existing ordinary integration tests retain their assertions.
- The full external snippet catalog compiles under all configured toolchains.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

This RFC is intended to restore current contracts, not change them. After the
implementation stabilizes, verify `docs/reference.md` against the final value
representations, structural equality, union conversion, and C23 output. Record
that no edit is required when it already states those contracts exactly.
