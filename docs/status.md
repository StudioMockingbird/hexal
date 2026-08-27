# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted; the Unowned section below is the staging area for items whose
meaning or home is undetermined, and each entry there commits to one or the
other.

## Open TODOs

### Design decisions required

| Work | Spec |
|---|---|
| C interoperability — compiler core | [0039](specs/0039-c-interop-compiler-core.md) |
| Target profiles and representation evidence | [0052](specs/0052-target-profiles.md) |
| Filesystem, build, and validation driver | [0055](specs/0055-filesystem-and-build-driver.md) |
| Language surface audit dispositions — 34 findings; promote/reject/accept per finding | [0103](specs/0103-language-surface-audit.md) |
| Affine ownership and Arena/Pool lifetimes — destructors rejected and cleanup obligations settled; `share`, Arena reset scope, Pool slot syntax, and handle classification remain | [0110](specs/0110-affine-ownership-and-arenas.md) |
| Native module storage and linkage | [0116](specs/0116-native-module-storage-and-linkage.md) |
| Restricted compile-time evaluation | [0117](specs/0117-compile-time-evaluation.md) |
| Concurrency safety and task lifetimes | [0118](specs/0118-concurrency-safety-and-task-lifetimes.md) |

### Implementation-ready

| Work | Spec |
|---|---|
| Arena and Pool allocators; Heap-only library boundary | [0027](specs/0027-arena-and-pool-allocators.md) |

### Design settled; implementation blocked

| Work | Blocked by | Spec |
|---|---|---|

## Open bugs

| Bug | Owning spec |
|---|---|

## Unowned

Items without a determinable meaning, staged here until each gets a spec or is
deleted:

- **Terminating self-recursive object construction.** Pointer-indirect
  self-recursion compiles and is valid per `reference.md`, so what this tracked
  is unclear. Assign it a spec or delete it.
- **Parser expression-start classification is scattered.** Raised during a
  refactor audit alongside the literal-interning table as the same class of
  scattered classification. It shares no code with the literal table. Recorded
  here rather than left implied-homed. Assign it a spec or delete it.
- **The scheduler never runs the root Task's own code: every concurrency
  program hangs at startup.** `hex_scheduler_init` creates the root Task and
  every worker, then switches from the root fiber into worker zero's dispatch
  loop and never pushes the root Task onto the ready queue. Every worker
  (verified with a debug build compiled and run under GCC on Windows: worker
  zero plus eleven others, matching the host's logical processor count) finds
  the ready queue empty, calls `hex_cond_wait`, and blocks forever; nothing
  ever signals it, because nothing has run the root statements that would
  spawn a Task, and nothing runs the root statements because
  `hex_scheduler_init` never returns to its caller. The process hangs on
  every program that reaches `hex_scheduler_init`, unconditionally. This
  predates and is independent of the native-threading-primitive migration:
  the reproduction used `hex_mutex_raw`/`hex_cond` (SRWLOCK/CONDITION_VARIABLE)
  with call sites and ordering otherwise unchanged from the prior C11
  `<threads.h>` implementation, so the same hang exists there too. It was
  found only because that migration required actually compiling and running
  generated concurrency C for the first time; no test before it ever executed
  generated C. Fixing this needs a considered addition to the park/commit/wake
  protocol (how root's own fiber gets scheduled without a chicken-and-egg
  dependency on the ready queue it isn't in), not a one-line patch, and
  belongs in its own spec rather than improvised inside an unrelated one.
  Assign it a spec; this is not a documentation gap. The external C23 suite's
  Tier 2/3 runners bound every process they execute to a 10-second timeout for
  exactly this reason: a fixture that spawns, joins, or locks a Task would
  otherwise hang the whole tagged run rather than failing cleanly. The three
  such fixtures (`concurrency-spawn-channel-compiles`, `concurrency-task-join-compiles`,
  `concurrency-mutex-compiles`) are consequently compile-only, never run.
- **`List<Task<T>>` and `List<Channel<T>>` do not compile.** `hexal/list.h`'s
  generic specialization spells the element type through `typeSpelling`, which
  returns Task's and Channel's bare `CName` (`hex_task_Int64`,
  `hex_channel_Int32`) for a plain binding -- correct there, because binding
  declarations for Task and Channel are rendered through a separate,
  already-correct path elsewhere that knows they are pointer-sized handles.
  `list.h`'s own generated storage (`hex_task_Int64 *data`, its growth
  helpers, `push`/`pop`/`at`) needs that same pointer knowledge and does not
  have it, so gcc/clang/zig cc all reject the result
  (`unknown type name 'hex_task_Int64'`, later `use of undeclared identifier`
  once storage is declared without a defining include in scope). A fix
  attempted during the external C23 suite's own construction -- adding a
  `typ.Task != nil || typ.Channel != nil` case to `typeSpelling`/`declaration`
  mirroring Mutex's -- was reverted: Task's own bare-variable rendering
  path already assumes `typeSpelling` returns the unwrapped `CName`, so the
  change produced `hex_task_Int32 **` (a double pointer) at every ordinary
  Task binding, breaking `concurrency-task-join-compiles`. The real fix needs
  to reconcile the two rendering paths, not extend one to match the other's
  assumption. Reproduced by `tasks-and-concurrency/concurrency-cpu-saturating-fibers`
  under `go test -tags c23 -run TestC23SnippetCatalogCompiles`. Assign it a
  spec; this is not a documentation gap.
- **The built-in `Seek` ADT does not compile when a program uses `Bytes.seek`.**
  `hexal/equality.h` references `hex_t_Seek` before anything defines it
  (`unknown type name 'hex_t_Seek'`), and the payload field names the seek
  dispatch in `hexal/bytes.c` reads (`.payload.hex_m_position`,
  `.payload.hex_m_offset`) do not match the names the ADT's own generated
  union actually carries (`no member named 'hex_m_position'`), so every
  toolchain rejects it. Reproduced by
  `streams/streams-seek-and-eos` and `streams/streams-bytes-memory` under
  `go test -tags c23 -run TestC23SnippetCatalogCompiles`. Not investigated
  past confirming the mismatch; assign it a spec or fold it into the
  generator's `Seek`/`Bytes.seek` owning area.
- **A generic function specialized only through another module's call site
  is used before it is declared.** `streams/streams-generic-drain` calls a
  generic `drain<S>` from `app.hex` with `S` bound to `IO` and to
  `MutPtr<Bytes>`; the generated C calls
  `hex_f_m3_app_drain_IO(...)`/`hex_f_m3_app_drain_MutPtr_Bytes_(...)` before
  either specialization's definition or a forward declaration exists
  (`implicit declaration of function`, C23 makes this a hard error). Every
  other generic-specialization fixture in the corpus apparently specializes
  from a call site the definition-ordering pass already accounts for; this
  is the first case reached where the caller precedes the specialization
  textually. Reproduced under `go test -tags c23 -run TestC23SnippetCatalogCompiles`.
  Assign it a spec; this is not a documentation gap.
- **A `match` lowered to a C `switch` does not enumerate every tag the
  program-wide `hex_tag` enum defines, so `-Wswitch` fires under `-Werror`.**
  `hex_tag` is one enum spanning every ADT and union tag in the whole
  program, not just the ones a given `match` discriminates; gcc, clang, and
  zig cc all warn when a `switch` over it does not mention every enumerator,
  including ones structurally unreachable at that specific match (a
  same-program but semantically unrelated ADT's tags, or `EoS`). Reproduced
  by `text/text-protocol-parser` under
  `go test -tags c23 -run TestC23SnippetCatalogCompiles`. The likely fix is a
  `default: __builtin_unreachable();`-style catch-all on every generated
  match switch, not enumerating the unrelated cases; not attempted. Assign it
  a spec or fold it into match lowering's owning area.
- **Two more collection/equality generator defects surfaced by the same
  suite, not yet root-caused**: `collections/collections-handle-elements`
  passes an argument of the wrong type to `hex_equal_hex_string` from
  generated equality code (`incompatible type for argument 1 of
  'hex_equal_hex_string'`); `collections/collections-nested-list` dereferences
  a pointer where `->` was needed (`'*(left->data + ...)' is a pointer; did
  you mean to use '->'?`) in `hexal/equality.h`'s nested-list comparison, then
  fails a downstream initializer. Both reproduced under
  `go test -tags c23 -run TestC23SnippetCatalogCompiles`; assign a spec or
  fold into equality generation's owning area.
- **A real `-Wmaybe-uninitialized` finding, not yet fixed.**
  `types-and-matching/types-shape-area` reads
  `hex_v_shape.payload.Rect.hex_m_height` in a path gcc's flow analysis
  cannot prove always follows a `Rect` construction, even though Hexal's own
  match exhaustiveness guarantees it does. `-Wno-maybe-uninitialized` was
  never added to the external C23 suite's flag set in the first place (the
  suite carries only the four `unused-*` suppressions, each labelled Debt),
  so this finding was never hidden; the fix -- proving the flow to the
  compiler, or restructuring the generated access -- has not been attempted.
  Assign it a spec or fold it into ADT payload access's owning area.

## Known coverage gaps

Not bugs — deliberate limits worth remembering when reading a green test run.

- **Runtime traps are verified for a curated dozen, not the full derived
  inventory.** `compiler/tests/c23validation` now runs a real, tagged
  (`go test -tags c23`) suite: `TestC23Suite` compiles every fixture under
  GCC, Clang, and `zig cc`, runs the ones with a zero-exit expectation and
  asserts their exact stdout, and runs the ones with a non-zero expectation
  and asserts their exact `[Runtime Error]` stderr text. It currently covers
  division/remainder-by-zero, conversion overflow, empty-list pop, missing
  Dict key, list/array index-out-of-bounds, array/list/string slice bounds,
  malformed UTF-8, and RuneCursor exhaustion, plus exact-output coverage for
  List/Dict/String round-trips, `print`'s output forms and evaluation order,
  `try`/`errdefer`/`defer` unwind ordering, float-to-integer truncation,
  signed-MIN overflow wrapping, text/Strand/RuneCursor conformance, and
  `Atomic<T>`'s full operation set. `TestC23SnippetCatalogCompiles` separately
  Tier-1-compiles every workbench snippet under all three toolchains with no
  hand-listed fixture per snippet. None of this executes a Task, Channel, or
  Mutex operation: the scheduler-hang bug above means doing so would hang the
  process, so those three fixtures are compile-only. What remains unverified:
  the rest of `reference.md`'s trap inventory (shift count, close failure,
  Mutex misuse, task stack overflow, and others) has no fixture yet, and
  `print`'s output forms are not exhaustive over every printable type. The
  sanitizer tier (UBSan on every runnable host fixture, ASan where the
  artifact set excludes `hexal/concurrency.c`) does not exist yet.
- **Compiling real generated C surfaced seven distinct, previously-unknown
  generator defects across the snippet catalog**, recorded individually
  above (this section's opening bullets): the built-in `Seek` ADT does not
  compile, `List<Task<T>>`/`List<Channel<T>>` do not compile, a
  cross-call-site generic specialization can be used before its forward
  declaration exists, a match-lowered `switch` can fail `-Wswitch` over
  `hex_tag` values structurally unrelated to that match, and two further
  equality-generation defects (a wrong-typed `hex_equal_hex_string` argument,
  a `nested-list` dereference needing `->`) are reproduced but not yet
  root-caused. Eight further, similarly-shaped defects reached the same way
  in this same pass were fixed and verified compiling and running clean
  under all three toolchains rather than left open: `hexal/list.h` and
  `hexal/array.h` omitting `hexal/string.h` for a String element (and
  `hexal/view.h`'s equivalent forward-declaration, needed instead of a full
  include because `hexal/string.h` itself unconditionally needs
  `hex_view_UInt8`); `print`'s selection of `hexal/io.c` not carrying the
  same error/list/view/heap dependency propagation a direct stream operation
  gets; a Go-template `{{0}}` rendering as literal `0` instead of the
  intended C `{0}` struct initializer in `hexal/io.c`; `Mutex` declared by
  value instead of by pointer wherever it is not a bare local (object
  members, union payloads); `Atomic<T>.new`'s constructor casting its
  argument to the `_Atomic` type inside a plain-typed return (tolerated by
  gcc, rejected by Clang and zig cc); a redundant `const` doubling when
  `hex_dict_find` returns a value type that is itself already a pointer
  (`Dict<K, String>`); and every `if`/`while` condition that is itself a bare
  comparison or logical expression being wrapped in one redundant extra pair
  of parentheses, which is flagged, not merely stylistic, under
  `-Werror=parentheses-equality`. The corpus-wide sweep that found all of
  this was `go test -tags c23 -run TestC23SnippetCatalogCompiles`, run once
  over the full ~150-snippet catalog; it had never been run to completion
  under a real toolchain before.
- The generator emits helper families wholesale — equality, print, union,
  heap, io — so a small program's C contains many unused `static` helpers.
  Demand-driven helper emission would remove the dead code. The external C23
  suite's four `unused-*` warning suppressions are labelled Debt against this
  entry; narrowing them is this item's job, not the suite's.
- **The redesigned Task park/commit/wake protocol is unverified at runtime**,
  for the same reason as every other generated-C claim: no test executes
  generated C. What is verified textually (exact generated-C structure,
  asserted in `compiler/generator/concurrency_component_test.go`): `hex_task`
  carries exactly one atomic park phase, one nullable pending link, and one
  lifecycle mutex with no superseded `state`/`wake_error` field; the three
  transition helpers (`hex_task_wake`, `hex_task_commit_park`,
  `hex_task_resume_commit`) are defined exactly once each; every wait-family
  registration writes its pending link before its release phase store; and a
  Mutex waiter's generated code returns directly on a transferred
  `wake_result` instead of re-entering acquisition. Unverified at runtime:
  immediate yield, join completion, Channel wake, and Mutex wake never run
  one fiber on two workers and never lose a wake; completion before and
  after dispatcher park commit each publish exactly once; Channel close
  wakes every registered waiter exactly once, including waiters whose fiber
  switches are not yet committed; a resumed Channel operation can recheck
  and re-park without stale-waker ABA; a contended Mutex transfers ownership
  without trapping its selected waiter; a join cannot reclaim the target
  fiber before its completion switch returns to the dispatcher; and join
  during `completing`, join after `done`, detach completion, and root
  shutdown each use their defined destruction owner.
- **The scheduler-aware blocking pool is unverified at runtime**, for the same
  reason as every other generated-C claim: no test executes generated C. What
  is verified textually (asserted in
  `compiler/generator/concurrency_component_test.go` and
  `compiler/tests/integration/concurrency_test.go`): the pool selects only
  when the scheduler runtime reaches a native descriptor transfer
  (`IO.read`/`write`/`seek`/`close`) or print's descriptor write-all sink,
  and selects for no other combination (IO alone, print alone, Task alone,
  Atomic beside IO, Bytes beside Task); `hex_blocking_worker`,
  `hex_blocking_init`, and `hex_blocking_call` are defined exactly once;
  `hex_blocking_call` submits under the required pending-link-then-parking-
  store-then-registration order; `hex_current_task` stays private to
  `hexal/concurrency.c` and is never read from `hexal/io.c`; and the platform
  IO cores are extracted once and shared by both the direct and pooled call
  paths. Unverified at runtime: N workers concurrently blocked on N native
  operations make progress; overflow growth and later retirement behave
  correctly under sustained and bursty demand; a `thrd_create` failure during
  growth truly falls back to existing workers rather than stalling the job;
  an immediately-completing native call does not double-publish its waiter's
  wake; the caller observes its job's result only after its own resume,
  never a stale or torn value; a baseline pool that fails to start traps
  before any user code runs; and root shutdown with detached Tasks still
  blocked on a native call reclaims correctly rather than leaking or racing
  the pool's own worker threads.
