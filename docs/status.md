# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted.

## Open TODOs

### Implementation-ready

| Work | Spec | Effort | ROI |
| --- | --- | --- | --- |

### Under review

| Work | Spec | Effort | ROI |
| --- | --- | --- | --- |

## Deferred ideas

Open ideas under discussion live in `docs/specs/deferred/`, with a README
explaining the directory. They are deliberately not listed here: this board
is scheduled work, and enumerating unscheduled ideas alongside it is what made
them read as commitments. Nothing there is authoritative, and a deferred spec
that disagrees with `docs/reference.md` is wrong.

## Open bugs

A bug is real whether or not its owning spec is scheduled.

| Bug | Owning spec | Effort | ROI |
| --- | --- | --- | --- |
| `try String<N>.interpolate(...)` fails at generation with `[Unknown Error] String<N>.interpolate expression reached generation without hoisting`; the same call as a plain `let x: String<N> \| Error = ...` assignment hoists and compiles. Fail-closed, no miscompile. | unassigned (needs a hoisting-order spec) | Medium | High |
| `Net.Address.IPv4/IPv6` `bytes` payload fields reject an ordinary `Array<Byte, 4>`/`Array<Byte, 16>` binding both at construction (`expected Array<UInt8, 4> initializer; got Array<UInt8, 4>`) and at equality (canonical-identity mismatch): the globally interned builtin array and the arena's array are distinct identities. `types/network.go` documents literal-only filling while `reference.md` says IPv4/IPv6 construct through the general ADT rules. Fail-closed, no miscompile. | unassigned (needs a builtin-array identity spec) | Medium | High |

## Known coverage gaps

Not bugs — deliberate limits worth remembering when reading a green test run.

- **The Tier 3 transforms' failure path is not executed by any fixture
  ([0227](specs/archived/0227-utf8proc-vendor-static-library.md)).** `String.normalize`
  and `String.casefold` release the `malloc` buffer `utf8proc_map` returns and
  report a failed transform as `InvalidInput`. That failure cannot be reached
  from a checked program -- text is always validated UTF-8, so `utf8proc_map`
  cannot report malformed input, and allocator exhaustion is not injectable
  here -- and it cannot leak either: `utf8proc_map_custom` sets `*dstptr` to
  NULL on entry and assigns it only on success, releasing its own buffer on
  every error path, so the caller's defensive release never has anything to
  free. The success path, the only one that can hand back a `malloc` buffer,
  is covered by a dedicated LeakSanitizer run (`TestC23SuiteLeak`). No other
  component gets one: every other fixture deliberately leaves bindings
  unfreed, so a program-wide leak check would report their intentional leaks.
- **Program paths and secure entropy ([0178](specs/0178-libuv-os-services.md))**
  have executable argument-independent fixtures that compile, run, and pass
  UBSan under the qualified Linux/Clang gate, and
  `TestProgramAndEntropyMeasurements` records generated size, build-and-link
  time, runtime allocation statistics, and per-run latency for both the
  program and entropy components.
- **Program entry and exit ([0182](specs/0182-program-entry-and-exit.md))**
  has executable argument, non-owning-free, and status fixtures that compile,
  run, and pass UBSan under the qualified Linux/Clang gate, and its
  argument-snapshot runtime allocation and status behavior is measured with
  RFC 0178's program component.
- **Networking (closed RFC 0172) serializes contending TCP callers by
  rejection, not by a FIFO wait queue.** A second concurrent read, write, or
  accept on one connection or listener while the first is still in flight
  returns Error with kind `Busy` immediately, instead of joining the queue
  RFC 0172's text specifies. Building genuine FIFO Task-parking
  synchronization for this was judged disproportionate to a first
  implementation; the common case (one caller per connection or listener at
  a time) is unaffected and is exercised end to end -- including a real TCP
  loopback connect/listen/accept/read/write/close exchange -- by the tagged
  C23 suite.
- **Processes and IPC (closed RFC 0173) makes the identical Busy-not-FIFO
  trade Networking makes, for the identical reason.** A second concurrent
  read or write on one Pipe while the first is still in flight returns Error
  with kind `Busy` immediately rather than joining a queue; the common case
  (one caller per Pipe at a time) is exercised end to end -- a real
  `uv_spawn` child, exit-status wait, and piped-stdout read/write/close --
  by the tagged C23 suite (`process-spawn-wait-runs`, `process-pipe-echo-runs`).
- **Signals (closed RFC 0176) routes any collection specialized over `Signal` (List, Array, Slice,
  Dict, Pool) to the consuming module's own header instead of the shared collection component, and
  excludes such a collection from eager equality-helper generation.** `hexal/signal.h` needs
  `hexal/error.h`, which needs `hexal/string.h`, which needs `hex_slice_UInt8` -- but
  `hexal/slice.h` is positioned ahead of all three specifically so they can rely on it already
  being complete, so no shared component can include `hexal/signal.h` without inverting that
  dependency direction. Routing the specialization to module-owned rendering (where
  `hexal/signal.h` is already `#include`d) sidesteps the conflict entirely, at the cost of
  `List<Signal> == List<Signal>` and its Array/Slice/Dict/Pool equivalents not being generated;
  bare `Signal == Signal` is unaffected. File, TcpConnection, and Process have the identical
  layering conflict for their own element collections and are not fixed here. Exercised end to end
  -- a real subscription racing a concurrent `close()` against a parked `next()` -- by the tagged
  C23 suite (`signals-subscribe-compiles`, `signals-close-wakes-waiter-runs`).
- **Every `[Runtime Error]` literal the generator can emit now carries a
  checked disposition; four are verified only by inspection, not
  execution.** `compiler/tests/c23validation/trap_inventory_test.go` derives
  every distinct `[Runtime Error] ...` literal directly from the production
  tree (the embedded `compiler/generator/packages` templates plus the
  non-test Go source under `compiler/generator`) and requires each one to
  carry exactly one of three dispositions: `executable` (an exact fixture
  exists and is cross-checked to exist by
  `TestTrapInventoryExecutableFixturesExist`), `structural` (unreachable from
  any checker-accepted program, verified by inspection rather than
  execution), or `nondeterministic` (real but not reliably reachable inside
  a bounded process timeout). `TestTrapInventoryIsFullyClassified` fails the
  moment a new trap literal appears anywhere in the generator without a
  disposition, so this stays in sync automatically. Every `executable`
  literal now has a real tagged fixture, including the previously-unverified
  Pool family (exhaustion, destroy-with-live-slots, double-free, foreign
  pointer, non-positive capacity), Mutex misuse (recursive lock, unlock by a
  non-owner, free while locked), Channel free-not-closed, Task
  already-joined-or-detached, Duration/Instant overflow and underflow, sleep
  duration too large, close of a borrowed stream, `Slice` index and slice
  bounds, `List` modified during iteration, and Task stack overflow. Four
  literals remain `structural`/`nondeterministic` rather than `executable`,
  each with its reasoning recorded in `trapLedger`: `cannot join the current
  task` (no `Task.current()`/self-reference API exists, so no
  checker-accepted program can pass a task its own handle), `channel free
  while tasks are blocked on it` (requires winning a race narrower than any
  synchronization primitive can deterministically arrange), and `dictionary
  capacity is not representable` / `list capacity is not representable`
  (require holding close to 2^63 entries). `print`'s output forms are still
  not exhaustive over every printable type.
- **UBSan (`-fsanitize=undefined -fno-sanitize-recover=all`) runs every
  runnable fixture in `compiler/tests/c23validation` under the one required
  Clang** (`go test -tags c23 -run TestC23SuiteUBSan`), asserting the fixture's
  own expectation still holds and that no undefined behavior was reported. The
  diagnostic-UBSan track is Linux-only: Clang ships no MinGW UBSan runtime, so
  on Windows the track skips and Windows debug's UB backstop is trap mode
  (exercised by the driver's own mode tests). On Linux the release gate fails
  if the selected Clang cannot link and execute the sanitizer-instrumented
  probe; there is no GCC or Zig capability probe, skip, marker, or alternate
  report path. The function-type-mismatch check
  (`-fsanitize=function`) stays disabled: Hexal's Task entry points and libuv
  work-queue callbacks are intentionally type-erased (stored generically, cast
  back to their real signature before calling), the exact pattern that check
  exists to flag. A checked-in ignorelist
  (`compiler/tests/c23validation/testdata/ubsan-ignorelist.txt`) excludes
  reports attributed to platform SDK headers -- the file currently names the
  Windows SDK's own `winnt.h`, inert on Linux; every other undefined-behavior
  category, including all of Hexal's own generated C, is still checked. ASan
  remains separately deferred under RFC 0185 because Task fibers lack sanitizer
  switch annotations. TSan is out of scope entirely; user-space fibers need
  their own feasibility decision. LeakSanitizer (`TestC23SuiteLeak`) is
  likewise Linux-only and skips on Windows, where no LSan runtime exists.
- **Equality, print, and union widening/truthiness are now demand-driven; the
  four `-Wno-unused-*` suppressions in `compiler/tests/c23validation`'s Tier 1
  build stay in place, but for a different, disclosed reason than the one
  they used to name.** `discoverEqualityTypes` (`equality.go`) collected an
  equality helper for every Object/ADT/Array/Slice/List/union type merely
  *mentioned* anywhere in the program, whether or not `==`/`!=` was ever
  applied to it; it now collects one only from an actual
  `DeepEqualityExpression`/`UnionEqualityExpression` site, plus the one real
  dependency writeUnionEquality itself calls by name (a union's own List
  member). `discoverGeneratedUnions` emitted every discovered union's
  `_truthy` helper unconditionally; it now emits one only for a union
  actually evaluated in a boolean context (an if/while condition, or an
  operand of not/and/or). `discoverGeneratedPrint` emitted every print-reachable
  type's `hex_print_nested_*` helper unconditionally; it now skips it for a
  type (String, Strand, a scalar, Error) that only ever appears as a bare
  top-level print argument, since `writePrintArgument` formats those
  directly and never calls their nested form. Heap, Stash, and IO were
  already demand-driven before this was checked; no change was needed
  there. An unsuppressed run of the complete snippet catalog under
  `-Wall -Wextra -Werror` confirms no helper-family over-emission remains:
  every warning it still reports is a top-level workbench-snippet pattern
  wholly unrelated to generator emission -- a `demo()` function or a
  `code := f()` binding a snippet declares purely to demonstrate that one
  construct compiles, never calling or reading it again on purpose. None of
  the four suppressions can be removed without either rewriting every such
  snippet to consume its own demonstration values (defeating their purpose
  as minimal examples) or emitting a `(void)` discard for every checked
  binding and parameter program-wide (a correctness-neutral but pervasive
  codegen change with no connection to this gap), so each remains, with its
  rationale in `c23_harness_test.go` corrected to name the real cause.
- **The fixture and snippet catalog now also compiles, links, and runs under
  the host's qualified profile** (`x86_64-linux-gnu` on linux/amd64,
  `x86_64-windows-gnu-ucrt` on windows/amd64), not only the host-neutral
  `Project{}` every other tagged test uses. `Project{}` keeps both platform
  branches and lets the C compiler's own target macros select one at
  C-compile time; an explicit profile instead has the Hexal compiler select
  the platform (`TestExplicitLinuxProfileSelectsPosixEntrypoint` and
  `TestExplicitProfileOmitsPosixBranches`,
  `compiler/tests/integration/target_test.go`) -- a materially different code
  path through every runtime component with concurrency or IO code that,
  before `TestC23SuiteQualifiedProfile`
  (`compiler/tests/c23validation/target_qualification_test.go`), had real but
  far narrower real-toolchain execution coverage. Separately, RFC 0052's own
  qualification text calls for one foreign target-object link fixture proving
  the selected backend can consume an object it did not produce alongside
  Hexal-generated ones; `TestForeignTargetObjectLinksWithHexalObjects`
  (`internal/driver/target_qualification_c23_test.go`) now compiles a prepared
  probe source, links it with a full Hexal build's own objects, and runs the
  result. Neither test extends the driver's build API to accept an
  externally-supplied object; that general capability remains owned by RFC
  0039/0192, separately blocked.
- **The Task park/commit/wake protocol's structural generated-C assertions
  (`compiler/generator/concurrency_component_test.go`) are now joined by
  runtime evidence (RFC 0183 Track 2), but not every named scenario is
  independently isolated.** `hex_task` carries exactly one atomic park
  phase, one nullable pending link, and one lifecycle mutex with no
  superseded `state`/`wake_error` field; the three transition helpers
  (`hex_task_wake`, `hex_task_commit_park`, `hex_task_resume_commit`) are
  defined exactly once each; every wait-family registration writes its
  pending link before its release phase store; and a Mutex waiter's
  generated code returns directly on a transferred `wake_result` instead of
  re-entering acquisition -- all still textual, not runtime, claims. At
  runtime, the tagged C23 suite now exercises repeated real transitions with
  an exact-total assertion that a single lost or duplicate wake anywhere in
  the run would change: 1,000 `Task.yield()`/Atomic-increment pairs across
  four concurrently joined Tasks (`concurrency-yield-join-stress-runs`),
  1,000 Channel messages through a bounded buffer between one producer and
  the root consumer (`concurrency-channel-stress-runs`), 1,000 contended
  Mutex-protected increments across four Tasks
  (`concurrency-mutex-stress-runs`), a join reached before the target has
  had any scheduling opportunity and one reached only after twenty parent
  yields (`concurrency-join-before-completion-runs`,
  `concurrency-join-after-completion-runs`), and twenty full native
  connect/accept/read/write/close round trips over one TCP loopback
  listener (`network-tcp-loopback-stress-runs`), each run under three
  toolchains and confirmed stable across five repeated `go test` invocations
  during development. Not yet given an equivalent repeated stress fixture:
  Process wait and Pipe read/write/close, Task-aware File work, and Signal
  next/close -- each has single-shot runtime coverage from its own closed
  RFC's fixtures, but not the specific 100-repetition bar Track 2 sets for
  native scenarios. Also not independently isolated by any fixture: Channel
  close waking *multiple simultaneously parked* waiters at once (every
  stress fixture here has exactly one consumer), a resumed Channel operation
  re-parking without stale-waker ABA, and the exact destruction-owner
  distinctions between join-during-`completing`, join-after-`done`, detach
  completion, and root shutdown -- these remain structural-assertion-only
  claims pending either finer black-box fixtures or internal
  instrumentation neither of which this pass added.
