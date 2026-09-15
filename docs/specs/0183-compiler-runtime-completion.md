# RFC 0183: Compiler and Runtime Completion Pass

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; six independent tracks are settled and must
  land as separately attributable changes
- Created: 2026-09-14
- Updated: 2026-09-15
- Scope: close six verified correctness, runtime-validation, generated-size,
  undefined-behavior-sanitizer, and target-qualification gaps
- Coordinates with: RFC 0184 before Track 5 changes the print-helper family;
  RFC 0185 owns AddressSanitizer and fiber-switch coverage; RFC 0187 extends
  Track 6 qualification from the current canonical build to both build modes
- Does not add: language syntax, semantics, public APIs, new target profiles,
  or permission to regenerate unrelated artifacts

## Purpose

This plan owns six independent completion tracks. They share the external C23
harness and final conformance goal, but not implementation state. Implement and
validate one track at a time; a failure or manifest change must remain
attributable to that track.

If implementation discovers that a correction requires a language contract or
representation choice not written here, stop that track and give it a new
focused specification. Continue only independent tracks.

## Tracks

| Order | Track | Outcome | Contract change |
| ---: | --- | --- | --- |
| 1 | Dict deletion correctness | Removing a key preserves the remaining probe chain. | None; fixes current behavior. |
| 2 | Task park/wake runtime verification | Current transition protocol executes without lost or duplicate wakeups. | None. |
| 3 | Generated-C trap coverage | Every deterministic source-reachable trap path is executed and guarded by inventory. | None. |
| 4 | UBSan coverage | UndefinedBehaviorSanitizer covers every runnable fixture on a capable execution toolchain. | None. |
| 5 | Demand-driven helper emission | Selected helper families emit only reachable operations. | Generated C only. |
| 6 | Current target qualification | Every compiler-declared qualified profile passes its complete runtime gate. | Support claims only after evidence. |

The order prioritizes the live Dict bug and leaves target qualification last so
it validates the resulting runtime. Tracks 2 through 5 are otherwise
independent, but must not run concurrently in one worktree.

## Track 1: Dict deletion correctness

### Decision

Keep the current linear-probing representation and repair the occupied cluster
after deletion. Do not add tombstones, a second bucket state, allocation, or a
new public rule.

For the removed bucket:

1. save the returned value;
2. mark the bucket inactive;
3. scan forward, wrapping by the capacity mask, until the first inactive
   bucket;
4. for each active bucket encountered, copy its entry, mark its old slot
   inactive, find its first valid slot with the existing probe helper, and copy
   it there; and
5. decrement length once and increment version once for the source-visible
   removal. Internal relocation changes neither.

The table always has an inactive bucket because growth keeps load below one.
Relocation copies keys and values shallowly and neither frees nor duplicates
their referents. Capacity and allocation stay unchanged.

### Implementation plan

1. Add a focused failing generated-C test using the current Int32 collision
   chain `12`, `17`, `35` at capacity eight.
2. Change only `hex_dict_remove_<K,V>` and a private cluster-repair helper in
   `compiler/generator/packages/dict.h`.
3. Add the equivalent Strand collision chain `k2`, `k11`, `k19` and cases for
   wraparound, head/middle/tail deletion, replacement, reinsertion, and growth.
4. Run the Dict integration tests, full Go suite, snippet manifest, and tagged
   C23 fixtures before removing the open-bug row.

## Track 2: Task park/wake runtime verification

### Decision

Do not change the scheduler protocol or add test-only runtime APIs. Combine the
existing structural generated-C assertions with repeated executable fixtures.
Runtime stress supplies behavioral evidence; structural tests continue to pin
the exact release/acquire and pending-link transitions that timing alone cannot
force deterministically.

### Required scenarios

- root and child `Task.yield`;
- join before completion, during completion, and after completion;
- Channel send, receive, and close wakeups;
- uncontended and transferred Mutex ownership;
- Task-aware File/native work;
- TCP connect/read/write/close;
- Process wait and Pipe read/write/close; and
- Signal next/close.

For each applicable wait source, cover a wake already notified when the
dispatcher commits the park and a wake after the Task is observably parked.
Use source-level synchronization to bias those two orders, repeat the transition
at least 1,000 times for in-memory primitives, and retain the harness timeout as
a failure boundary. Repeat each native networking, process, filesystem, and
signal scenario 100 times because every repetition starts real host work.

### Implementation plan

1. Inventory the existing structural assertions for `pending_park`, phase
   stores, wake publication, ready-queue insertion, and destruction ownership.
   Add only missing structural assertions.
2. Add c23validation fixtures in the existing catalog, reusing its compile
   cache and process timeout.
3. Use Atomic counters and exact final totals to detect duplicate execution;
   use joins and handshakes so completion cannot be mistaken for abandonment.
4. Run each fixture repeatedly on the qualified host. Release qualification
   includes a host reporting at least two available logical processors; the
   existing structural tests retain the one-worker FIFO/fairness proof because
   Hexal exposes no worker-count control.
5. A hang, wrong total, trap, or duplicate completion is a reproduced bug. Stop
   and create a focused fix spec if correcting it changes the protocol.

## Track 3: Generated-C trap coverage

### Inventory rule

Derive distinct `[Runtime Error]` literals from production generator inputs:

- embedded `compiler/generator/packages/*.c` and `*.h`; and
- Go string literals in non-`_test.go` production files of
  `compiler/generator` that emit `hex_runtime_trap` or direct emergency trap
  text.

A repository-relative guard locates the root from its own source path, parses
Go with `go/parser`, reads package templates, and compares the derived set with
the fixture ledger. It stores no independent expected count.

Each derived literal has exactly one disposition:

- executable: one fixture names the generated path and asserts exact stderr and
  non-zero termination;
- structurally verified: asynchronous-signal/exception text or another path
  unsafe to invoke in-process has an exact generated-text assertion; or
- nondeterministic resource failure: the ledger states why ordinary execution
  cannot trigger it and names its structural assertion.

No literal may be silently absent, and two source spellings using the same
runtime path share one fixture.

### Implementation plan

1. Add the derivation guard to `compiler/tests/c23validation` and populate the
   initial disposition ledger from the current production tree.
2. Reconcile existing trap fixtures by generated runtime path, not by old spec
   or snippet provenance.
3. Add missing deterministic fixtures for numeric, bounds, iteration,
   collection, allocator, String/RuneCursor, IO, scheduler, and handle misuse.
4. Add exact text assertions for signal/exception and resource-failure paths
   that cannot be executed reliably.
5. Remove the curated-subset coverage note only when the guard is complete and
   its scanner unit test rejects a synthetic unclassified production literal.

## Track 4: UBSan coverage

### Decision

- Run UBSan over every runnable host fixture with
  `-fsanitize=undefined -fno-sanitize-recover=all`.
- Do not claim or attempt ASan in this track. The installed Zig 0.16.0 Windows
  backend emits ASan instrumentation but cannot link its runtime, and custom
  fiber stacks require balanced sanitizer switch annotations. RFC 0185 owns
  both prerequisites.
- Keep TSan out of scope; user-space fibers require a separate feasibility
  decision.

UBSan is an additional tagged lifecycle. Ordinary `go test ./...` remains pure
Go and needs no external toolchain.

### Implementation plan

1. Add a UBSan capability probe per external toolchain and flag set. Cache it
   with the existing toolchain identity.
2. Add a tagged UBSan runner that reuses the fixture catalog, build cache,
   timeout, dependencies, and expected stdout/stderr.
3. A developer-local run may skip an unavailable sanitizer toolchain, but a
   release/CI gate fails if no capable executing toolchain runs UBSan.
4. Set deterministic UBSan options that abort on the first report and preserve
   stderr separately from expected Hexal trap stderr.
5. Keep ASan flags and leak-check claims absent. Deferred RFC 0185 is
   reactivated only after its toolchain and fiber gates pass.

## Track 5: Demand-driven helper emission

### Scope

This pass covers only the five families named by the current warning debt:

| Family | Existing owner | Required demand |
| --- | --- | --- |
| Equality | `discoverEqualityTypes` / `writeEqualityDefinitions` | Exact canonical types used by `==` plus recursive structural dependencies. |
| Print | `discoverGeneratedPrint` / `writePrintDefinitions` | Exact types passed to print plus recursive formatting dependencies. |
| Union operations | `discoverGeneratedUnions` / `writeUnionDefinitions` | Wrapper declarations remain type-driven; truth, widen, inject, and extract helpers are operation-driven. |
| Heap/Stash typed adapters | heap and stash discovery/writers | Exact allocated or released canonical type and operation. |
| IO adapters | `discoverGeneratedStreams` / `writeStreamInlineHelpers` | Exact reachable IO constructor or method operation. |

Do not move package templates back into Go strings, merge components, redesign
types, or extend the pass to collection APIs that do not cause the recorded
warning debt.

RFC 0184 lands before this track. Its print buffer and revised helper signatures
form this track's baseline; do not optimize the fragment-writing helpers that
RFC 0184 deletes.

### Implementation plan

1. Produce a baseline table for every emitted helper in these families: owning
   writer, checked operation that requires it, and current over-emission sites.
2. Extend each existing discovery state with deterministic operation/type sets;
   do not add a second whole-program walker when `walkProgram` already exposes
   the checked identity.
3. Filter declarations and definitions from the same state so no call can be
   emitted without exactly one declaration/definition.
4. Preserve canonical sort order and byte-identical non-helper declarations.
5. Run the full snippet manifest and external C23 catalog after each family,
   reviewing artifact movement before starting the next.
6. Remove each `-Wno-unused-*` debt entry only when the unsuppressed full catalog
   proves it unnecessary. A suppression caused by a different family remains
   with a corrected rationale.

## Track 6: Current target qualification

### Scope decision

Qualification derives its complete set from the compiler-owned target-profile
registry. Today that set is exactly `x86_64-windows-gnu`. This track adds no
Linux, macOS, AArch64, ARM32, or RISC-V profile; each future profile requires a
focused spec covering its ABI, libc/SDK, dependencies, execution host, and
runtime branches.

`Project{}` is host-neutral output and is not a qualified target profile.
The current reference sentence claiming POSIX x86-64 Task support is unsupported
and must be removed only with explicit user approval; it is not evidence that a
second profile exists.

### Qualification gate

For every registry profile:

1. compile sources with that exact `Project.Target`, never `Project{}`;
2. compile every generated translation unit as C23 through the pinned driver
   backend and target options;
3. link the complete declared runtime dependency set;
4. inspect the output architecture/ABI;
5. run representative fixtures on a compatible host; and
6. assert exact status, stdout, stderr, and source mapping.

The representative set selects every generated runtime component and every
C23/toolchain facility currently emitted. The gate is non-skipping for release
qualification. A cross-compile without execution does not qualify a profile.

### Implementation plan

1. Add one authoritative enumerator for qualified registry profiles; tests and
   driver consume it rather than duplicating a list.
2. Make the external harness compile the qualification corpus with each exact
   profile and verify emitted platform branches.
3. Drive the installed pinned Zig backend end to end through ADR 0055, including
   one foreign target-object link fixture already required by RFC 0052.
4. Compare the passing registry set with reference support claims. Request
   explicit approval before removing the unsupported POSIX claim.
5. Keep a new profile rejected until its own spec and complete gate land in the
   same change.

## Required sweep

- the Dict open-bug row after Track 1 passes;
- stale scheduler-runtime coverage prose after Track 2 passes;
- curated trap counts and unclassified trap fixtures after Track 3 passes;
- UBSan-absent prose after Track 4 passes; ASan prose remains owned by RFC 0185;
- only warning suppressions proven unnecessary by Track 5; and
- unsupported or duplicated target-support claims after Track 6 passes and the
  required reference edit is approved.

Do not edit archived specifications. Present-tense code comments and active
status prose are updated in the owning track.

## Detailed execution plan

1. Before each track, record the exact focused tests, full-suite state, tagged
   C23 state, and snippet manifest.
2. Implement only that track's plan and Validation subsection.
3. Run focused tests, `go test ./...`, `go vet ./...`, tagged C23 compilation
   and runtime tests, and the snippet manifest as applicable.
4. Review every changed artifact hash and warning suppression.
5. Commit or otherwise freeze the passing track before starting another. Do not
   share one manifest regeneration across tracks.
6. Remove only that track's status row when complete. The RFC remains active
   until all six rows close.
7. Run Track 6 last as the final conformance gate.
8. If Track 6 lands before RFC 0187, qualify the current canonical driver mode.
   RFC 0187 must later rerun the same corpus in both debug and release before
   either mode becomes part of a target's qualification claim.

## Validation

This section is exhaustive.

### Dict deletion

- Int32 collision chain `12`, `17`, `35` and Strand chain `k2`, `k11`, `k19`;
- head, middle, tail, and wraparound deletion;
- every surviving `get`, `find`, and `contains` result;
- missing and repeated removal traps;
- replacement, remove/reinsert, growth-before-remove, length, and one version
  increment per public removal;
- shallow key/value identity and unchanged allocation capacity; and
- exact generated-C compilation and runtime behavior.

### Task transitions

- every Required scenario above reaches immediate-notified and delayed-wake
  paths where applicable;
- exact final counters and joins show no lost wake, duplicate publication,
  simultaneous execution, premature reclamation, or stale pending-link reuse;
- Channel close wakes every waiter once, Mutex ownership reaches only its
  selected waiter, and re-parking clears prior wake state; and
- every fixture completes within the existing timeout under repeated runs.

### Trap inventory

- every derived literal has exactly one executable, structural, or
  nondeterministic disposition;
- every executable trap asserts exact stderr and non-zero termination;
- the inventory guard fails when a temporary unclassified production literal
  is injected; and
- no independent expected count or archived-spec dependency exists.

### UBSan

- UBSan runs every runnable eligible fixture without a sanitizer report;
- ASan and leak-check coverage are not claimed by this track, and TSan is never
  claimed;
- unsupported local toolchains skip explicitly, while the release gate requires
  at least one executing UBSan toolchain; and
- ordinary tests remain external-toolchain-free.

### Helper demand

- each reachable helper call has exactly one declaration and definition;
- unused operations in each scoped family emit no helper or state;
- deterministic order and non-helper declarations remain stable;
- manifest movement is confined to programs selecting the changed family; and
- each removed warning suppression is proven unnecessary by an unsuppressed
  complete catalog run.

### Target qualification

- registry enumeration and driver enumeration are identical;
- every current profile is compiled with its exact non-empty Project target;
- all runtime components and emitted C23 facilities compile, link, and execute;
- architecture, ABI, dependencies, status, output, traps, and source mapping
  match the profile; and
- no unqualified target appears in the compiler registry or normative reference.

## Completion rule

This plan closes only when all six status rows are removed. A track moved to a
new focused active owner no longer blocks this plan, but its status item remains
open under that new owner. Completion of one track grants no authority over
another.

## Reference synchronization

Tracks 1 through 5 should not change language contracts. If a correction proves
otherwise, report it before editing the reference. Track 6 changes support
claims only after its complete gate passes and only with explicit user approval.
