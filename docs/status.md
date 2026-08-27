# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted.

## Open TODOs

### Design decisions required

| Work | Spec |
|---|---|
| C interoperability — compiler core | [0039](specs/0039-c-interop-compiler-core.md) |
| C compiler backend, target packs, and trusted target profiles | [0052](specs/0052-target-profiles.md) |
| Filesystem, build, and validation driver | [0055](specs/0055-filesystem-and-build-driver.md) |
| Scalar value matching beyond Bool | [0135](specs/0135-scalar-value-match.md) |
| Expanded scalar Dict key types | [0136](specs/0136-expanded-dict-key-types.md) |
| Affine ownership and Stash/Pool lifetimes — destructors rejected and cleanup obligations settled; `share`, Stash reset scope, Pool slot syntax, and handle classification remain | [0110](specs/0110-affine-ownership-and-stashes.md) |
| Native module storage and linkage | [0116](specs/0116-native-module-storage-and-linkage.md) |
| Restricted compile-time evaluation | [0117](specs/0117-compile-time-evaluation.md) |
| Concurrency safety and task lifetimes | [0118](specs/0118-concurrency-safety-and-task-lifetimes.md) |

### Implementation-ready

| Work | Spec |
|---|---|
| Typed Stash and Pool allocators; Heap-only library boundary | [0027](specs/0027-stash-and-pool-allocators.md) |
| Allocation-free String/Strand mixed comparison | [0139](specs/0139-string-strand-comparison.md) |
| Local fallback recovery with `catch` | [0134](specs/0134-error-recovery-with-catch.md) |
| `TestC23SnippetCatalogCompiles` does not complete a full run on the authoring host (toolchain rediscovery per snippet, no parallelism) | [0140](specs/0140-c23-catalog-sweep-performance-plan.md) |

### Design settled; implementation blocked

| Work | Blocked by | Spec |
|---|---|---|

## Open bugs

| Bug | Owning spec |
|---|---|
| Scheduler initialization enters worker zero before root statements run, leaving every worker asleep on an empty ready queue | [0132](specs/0132-root-task-scheduler-bootstrap.md) |
| Match misclassifies imported dotted patterns, keys union coverage by short type name, and accepts duplicate exact-type arms or unreachable final `else` arms | [0133](specs/0133-match-exhaustiveness-and-qualified-patterns.md) |
| Returned inline aggregates can hide a View that borrows a local of the returning function | [0137](specs/0137-nested-view-return-safety.md) |
| Mutable List/Dict storage can retain a local-rooted View beyond that local's lifetime; safe handling needs container mutation and alias rules | [0110](specs/0110-affine-ownership-and-stashes.md) |
| `String.free` accepts literal-backed static storage and passes it to the heap deallocator | [0138](specs/0138-string-literal-free-safety.md) |

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
  Mutex operation: RFC 0132's scheduler-startup bug would hang the process, so
  those three fixtures are compile-only. What remains unverified:
  the rest of `reference.md`'s trap inventory (shift count, close failure,
  Mutex misuse, task stack overflow, and others) has no fixture yet, and
  `print`'s output forms are not exhaustive over every printable type. The
  sanitizer tier (UBSan on every runnable host fixture, ASan where the
  artifact set excludes `hexal/concurrency.c`) does not exist yet.
- **Compiling real generated C surfaced multiple generator defects across the
  snippet catalog.** Every failure reproduced so far is now fixed and
  re-verified compiling clean under all three toolchains, most recently (RFC
  0131, closed 2026-08-27) handle-valued Array/View element storage,
  handle-aware nested equality and for-in binders, generic specialization
  prototype ordering, flow-narrowed union returns and `try` operands, and the
  ADT equality `abort()` missing `<stdlib.h>`; see the archived spec for the
  full root-cause account. That closure's targeted and combined probes are
  green, but a full untargeted sweep of the entire ~150-snippet catalog was
  not re-run to completion afterward: on the authoring host it exceeded 50
  minutes and roughly 450 external toolchain invocations without asserting a
  single failure before the test runner's own timeout killed it. Making that
  sweep completable is now [0140](specs/0140-c23-catalog-sweep-performance-plan.md)'s
  job; any snippet it turns up failing gets filed as a fresh finding once it
  runs. Earlier defects fixed and verified
  compiling and running clean under all three toolchains include `hexal/list.h` and
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
