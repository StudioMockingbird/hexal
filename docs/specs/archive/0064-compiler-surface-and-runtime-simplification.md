# RFC 0064: Compiler Surface and Runtime Simplification

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-15 (Items 1 and 2 via
  execution plans 0066 and 0067; Items 3-6 retained decisions with no code
  change)
- Created: 2026-08-15
- Scope: remove low-return compiler-owned runtime features and reduce
  cross-cutting language special cases
- Coordinates with: RFC 0039 (C interop), RFC 0051 (Stream extensions), RFC
  0052 (target profiles), RFC 0055 (filesystem/build driver), RFC 0062
  (`hexal.h` cleanup), RFC 0063 (method-surface reduction), `AGENTS.md`,
  `docs/reference.md`, and `docs/status.md`

## Summary

Hexal's largest simplification opportunities are whole compiler-owned runtime
subsystems, not convenience-method aliases.

The preferred direction is:

1. Remove built-in `Stream<T>`.
2. Remove built-in `File`, `FileMode`, and `Stdio`.
3. Retain the cooperative M:N fiber scheduler, `Task.yield()`, parking
   semantics, and starvation analysis as intentional concurrency capabilities.
4. Retain `Atomic<T>` and its copyability model.
5. Reassess `Strand` and recursive structural `print` only after the accepted
   Stream and File decisions stabilize.
6. Revise RFC 0063 so minor surface cleanup does not remove unique or
   asymptotically better operations.

This RFC records the approved direction. It is not one combined implementation
plan: each accepted major removal requires a focused implementation spec or
execution plan.

## Evidence

The current compiler contains approximately 32,275 production Go lines. The
following dedicated subsystem files contain:

| Subsystem | Dedicated production lines | Production files mentioning the feature |
|---|---:|---:|
| Task/Channel/Mutex/Atomic concurrency and starvation analysis | ~2,498 | 27 |
| Stream | ~887 | 16 |
| File builtins and shared output generation | ~763 | 13 |
| Text implementation | ~771 | Strand appears in 16 |
| Print checking/generation | ~494 | recursive type integration |

These figures are evidence of relative cost, not deletion estimates. Shared
files contain unrelated behavior: in particular, the shared output generator
also owns print/output-gate behavior that survives File removal. Accepted
removals would also delete branches from common type resolution, eligibility,
modules, cleanup, rendering, validation, tests, and workbench snippets.

Ordinary tests do not execute generated C. Dormant C23 canaries type-check Go
packages but have no runnable entrypoints. Therefore scheduler, Stream, File,
trap, and exact output runtime behavior currently carry substantially more
implementation risk than their green test suites demonstrate.

## Design criteria

A removal has high architectural return when it:

- deletes a complete compiler-owned concept rather than one switch case;
- removes dedicated checked-expression kinds or type constructors;
- removes branches from several compiler stages;
- removes generated runtime machinery or platform-specific code;
- can later live in an ordinary library or runtime without privileged syntax;
- has no current replacement requirement from Hexal's core systems-language
  model; and
- reduces contracts that ordinary tests cannot execute.

Operation count alone is not a useful optimization target. A familiar O(1)
method may cost less than a single complex owning type.

## Item 1 — Remove built-in `Stream<T>`

Decision: approved for removal.

Remove:

```text
Stream<T>
Stream<T>.new()
Stream<T>.produce(heap, state, callback)
Stream<T>.next()
Stream<T>.filter(heap, predicate)
Stream<T>.map<U>(heap, mapper)
Stream<T>.take(heap, count)
Stream<T>.free(heap)
List<T>.stream(heap)
```

Also remove Stream-specific `for` iteration, positions, type eligibility,
module closure, deferred-free capture, generated type families, expression
kinds, validation, tests, and snippets.

### Rationale

The surface is small but requires:

- a distinct generic owning handle;
- producer State eligibility and `MutPtr<State>` callback contracts;
- special `T | EoS` rules;
- consuming adapter ownership and alias restrictions;
- heap allocation for adapters;
- special List borrowing and mutation restrictions;
- special `for` lowering and indexed iteration;
- special cleanup capture; and
- per-specialization generated C families.

The absence of closures makes common `filter` and `map` use require named
functions. This reduces the ergonomic return of privileged compiler support.

Stream pipelines can return as an ordinary generic library after C interop and
user-defined generic libraries stabilize. `EoS` remains while Channel receive
uses it.

### Consequences

- RFC 0051 becomes unnecessary and should close without implementation.
- Lazy pipelines temporarily disappear; Array/View/List loops remain.
- No replacement empty-stream constructor is required because the complete
  Stream concept is removed.
- This is a breaking removal with no compatibility alias.

## Item 2 — Remove the File builtins

Decision: approved for immediate removal. No replacement is required to land
first.

Remove compiler-owned:

```text
File
FileMode
Stdio
File.open
File.read_bytes
File.read_text
File.write
File.write_text
File.flush
File.close
```

Retain `print` as the minimum built-in output facility.

### Rationale

The current builtin fixes library policy in the language and compiler:

- three file modes;
- nonempty, NUL-free ASCII paths;
- binary opened files but text-only standard handles;
- borrowed standard-handle ownership;
- runtime mode checks;
- partial-write and flush behavior;
- fixed portable error messages; and
- UTF-8 validation policy.

These are library contracts, not syntax or static type-system primitives. C
interop can expose C or portable-library file operations without dedicated
parser/checker/generator expression families.

This item does not change the in-memory compiler boundary. The compiler still
accepts a source map and returns generated artifacts as strings. RFC 0055 owns
host source discovery and artifact writing; a future core library owns program
runtime File APIs.

### Consequences

- Remove File-specific types, qualifiers, checked-expression kinds,
  validation, deferred-close capture, runtime helpers, tests, and snippets.
- Separate the retained print/output gate from removed File machinery.
- RFC 0039 must be capable of expressing the ABI needed by the future File
  library.
- The approved immediate removal accepts an unbounded gap with no File API and
  no scheduled restoration date.

## Item 3 — Retain cooperative M:N fiber concurrency

Retain the complete concurrency surface:

```text
spawn function(args) -> Task<R> | Error
Task<R>.join() -> R
Task<R>.detach() -> no value
Task.yield() -> no value
Channel<T>
Mutex
Atomic<T>
```

The following remain normative product requirements rather than simplification
candidates:

- many lightweight Tasks multiplex over fewer C23 worker threads;
- a blocked Channel or Mutex operation parks its Task without consuming the
  worker thread;
- `Task.yield()` supplies an explicit scheduling point for cooperative
  computation;
- task-reachable literal `while true` loops retain starvation analysis;
- Task argument and result frames remain scheduler-owned;
- join and detach preserve their exact-one-consumer and reclamation rules;
- Task stacks retain their specified guarded-stack behavior and size contract;
  and
- supported platform fiber/context backends remain part of the runtime target
  contract.

### Rationale

One-thread-per-Task lowering is not an equivalent simplification. It changes
the defining scale property: hundreds of parked Tasks would become hundreds of
blocked OS threads, C23 `thrd_create` offers no portable stack-size control,
and generic Task results require a replacement marshalling/control-block
design.

M:N fibers are deliberate Hexal concurrency and parallelism infrastructure,
not an incidental implementation detail. Their cost is justified by cheap
Tasks, cheap parking, controlled stacks, and scheduler-aware synchronization.

### Permitted future refactoring

The implementation may later move stable scheduler, Channel, and Mutex runtime
code out of generator logic into a separately maintained shipped runtime
source/library. Such a refactor must preserve the source surface and every
contract above. It is architecture work, not language-surface reduction, and
requires its own ADR or refactor spec.

## Item 4 — Retain `Atomic<T>`

Decision: retain Atomic and the copyability axis.

`Atomic<T>` has modest dedicated implementation but exceptional type-system
reach. It is currently the only non-copyable Hexal value and the sole reason
storability and copyability are separate eligibility axes.

Removing Atomic would eliminate:

- the non-copyable-value category;
- recursive Atomic-containment traversal through inline aggregates;
- copyability checks across assignment, calls, returns, ADTs, unions,
  collections, Tasks, Channels, Streams, and Heap allocation;
- direct-Atomic pointee and `ref` exclusions;
- construction-only placement rules; and
- seven builtin operations and their C23 helper generation.

The exact current placement rule is:

- direct fresh Atomic construction is valid in bindings and object members;
- Atomic and inline aggregates containing it cannot cross a copying boundary;
- direct `Ptr<Atomic<T>>`, `MutPtr<Atomic<T>>`, and `ref atomic` are invalid;
  and
- an enclosing object containing Atomic may be addressed and shared through a
  pointer.

Do not describe Atomic as forbidden from object members; that claim is false.

### Why removal is not currently recommended

Mutex can reproduce mutually exclusive access to a value, but is not an
equivalent replacement for Atomic:

- Atomic is inline and allocator-free; Mutex is an allocated handle.
- Atomic maps directly to C23 `_Atomic(T)` and foreign atomic layouts.
- Atomic provides load, store, exchange, fetch-add/subtract, and
  compare-exchange without a separate lock object.
- Mutex introduces lock ownership, deadlock, misuse, and cleanup behavior.
- Replacing Atomic with Mutex loses representation, interoperability, and
  performance capabilities even when the resulting program remains race-free.

Hexal's systems-language, C-interop, and no-runtime-overhead goals currently
justify this complexity. The copyability axis may also serve future foreign or
ownership types that cannot be copied safely.

### Possible later review

Atomic may be reconsidered after Stream and File simplification, but removal is
not part of this RFC. Remove it
only if Hexal deliberately prefers uniform copyable values over inline C23
atomics. If removed, define how foreign atomic storage and operations remain
accessible; do not claim that Mutex preserves the same capability.

Snippet count is not evidence for this decision. The workbench catalog is
coverage inventory, not usage telemetry.

## Item 5 — Defer `Strand` removal

`Strand` is a legitimate later simplification target because it creates:

- contextual literal behavior;
- a visible 31-byte payload limit;
- separate equality, ordering, hashing, printing, iteration, and dispatch;
- special Dict key support; and
- a separate Error-header type.

Do not remove it in the first implementation pass. Replacing it with String
does not automatically remove the need for static/literal lifetime rules, and
may transfer complexity into String ownership and Dict key lifetime.

A later RFC must first define:

- Error header representation;
- String literal ownership and whether literal aliases may be freed;
- Dict String-key lifetime and hashing; and
- whether short inline text remains useful as a value type.

## Item 6 — Defer narrowing structural `print`

Recursive printing of objects, ADTs, Array, View, List, and Dict requires
recursive eligibility checking and per-concrete-type generated helpers.
Restricting print to scalar, Rune, text, Nil, and Error would simplify the
checker, generator, and generated C.

Retain structural print until Hexal has an ordinary formatting/display
protocol or library replacement. Removing it first would materially harm
debugging and workbench usability for a comparatively smaller compiler saving.

## RFC 0063 reconciliation

RFC 0063 performs surface hygiene, not architectural simplification. Do not
implement it unchanged before resolving this RFC.

### Retain from RFC 0063

- Remove `.at(index)` where `[index]` has identical semantics.
- Remove `Array<T,N>.is_empty()` because N is always positive.
- Add the Byte/UInt8 canonical-spelling rule.
- Correct the stale `AGENTS.md` warning that rejects mandatory `if ... then`.
- Move `Stdio` methods onto File only if File remains after this RFC.

### Reconsider or remove from RFC 0063

- Remove `View.is_empty()` and `List.is_empty()`; both are O(1) tests of a
  stored length and exactly equal `length() == 0`.
- Retain `String.is_empty()` and `Strand.is_empty()`; both are O(1), while
  `length()` counts UTF-8 Runes and is O(n).
- Do not remove `String.bytes()`. It constructs a whole-byte View in O(1);
  `slice(0, string.length())` scans UTF-8 to obtain and validate Rune bounds and
  is not performance-equivalent.
- Do not separately remove `Stream.new()`. Remove the complete Stream concept
  under Item 1 or retain its unique allocation-free empty value.
- Retain `Channel.capacity()`. Capacity is immutable, and a function receiving
  a Channel did not necessarily create it or otherwise know its capacity.
- Treat `Channel.length()` as an observational snapshot unsuitable for
  synchronization, not as inherently useless. Retain it for diagnostics and
  metrics; removing it saves negligible architecture.

RFC 0063 must lose its implementation-ready status until these conflicts are
resolved.

## Implementation structure

With the decisions resolved, split execution into independent stages:

1. Stream removal.
2. File builtin removal or library migration.
3. Revised minor method-surface cleanup.
4. Optional later Atomic, Strand, and structural-print reviews.

Each stage must independently compile and pass tests. Do not combine all
removals into one change or one unreviewable generated-C baseline update.

## Validation requirements

For every accepted removal:

- Remove grammar, protected names, types, type positions, checked-expression
  kinds, checker dispatch, generator discovery/rendering/validation, generated
  runtime support, tests, snippets, and syntax highlighting as applicable.
- Search for stale feature names outside immutable archived specs.
- Add focused negative tests for removed spellings only when the diagnostic is
  intentionally part of the migration contract; do not preserve permanent
  feature-specific branches merely for friendly legacy diagnostics.
- Preserve all unaffected evaluation, copying, ownership, module, C artifact,
  and source-mapping contracts.
- Keep ordinary tests pure Go and external-toolchain-free.
- Run `go test ./...`, `go vet ./...`, `go test -tags c23 ./...`, and
  `go vet -tags c23 ./...`.
- Regenerate workbench snippet manifests only for deliberate catalog changes,
  then rebuild and restart the workbench.
- Update `docs/reference.md` once after each accepted stage stabilizes and
  before that stage's spec closes.
- Update `docs/status.md`; close or revise superseded open RFCs.

## Non-goals

- Removing Array, View, List, Dict, generics, modules, structural unions, ADTs,
  match, Error/try/cleanup, pointers, explicit Heap passing, layout intrinsics,
  volatile operations, or endian conversions.
- Changing the core compiler's in-memory source/artifact boundary.
- Implementing C interop, the filesystem driver, or target profiles in this
  RFC.
- Removing or weakening M:N fibers, Task parking, explicit yielding,
  starvation analysis, controlled Task stacks, or scheduler-aware
  synchronization.
- Removing Atomic in the preferred concurrency design.
- Removing Strand or structural print in the first implementation pass.
- Optimizing for builtin operation count or reference-document line count.

## Evaluated and retained leaf features

Retain these even though their individual usage counts are small:

- `Ptr.read_volatile` and `MutPtr.read_volatile`/`write_volatile`;
- `size_of<T>()` and `align_of<T>()`;
- `bit_cast`; and
- fixed-integer endian conversion operations.

They are isolated leaf features with limited architectural coupling and expose
direct systems/C capabilities. Removing them produces little compiler
simplification while weakening Hexal's ability to express low-level programs.
Keep ordinary bitwise and shift operators independently of any later intrinsic
review.

Retain user-defined generics. Their checker implementation is large, but they
are a headline language capability rather than incidental runtime policy.
ADTs, structural unions, Error, `try`, `defer`, and `errdefer` remain
load-bearing language features and are not simplification candidates.

## Resolved decisions

1. Remove built-in Stream and close RFC 0051 without implementation.
2. Remove File, FileMode, and Stdio immediately. Accept an unbounded period
   without a runtime File API.
3. Retain inline `Atomic<T>` and the non-copyable-value/copyability model.
4. Retain cooperative M:N fibers and their existing Task, parking, yielding,
   stack, result-marshalling, and scheduler-aware synchronization contracts.

## Readiness

The design direction is approved. Do not implement RFC 0064 as one change.
Create focused implementation documents for Stream removal and File builtin
removal, then revise RFC 0063 against the resolved decisions. Atomic and M:N
fiber concurrency require no implementation change from this RFC.
