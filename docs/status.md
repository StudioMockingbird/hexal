# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted.

## Open TODOs

### Implementation-ready

| Work | Spec |
| --- | --- |
| Pinned, vendored mimalloc v3 backend for Hexal-owned dynamic allocation, compiled and statically linked by installed Zig for `x86_64-windows-gnu` | [0146](specs/0146-mimalloc-allocation-backend.md) |
| Explicit `unsafe do ... end` blocks for individually classified unprovable operations; incorrect unsafe assertions may permit C undefined behavior | [0155](specs/0155-unsafe-blocks.md) |

### Design settled; implementation blocked

| Work | Blocked by | Spec |
| --- | --- | --- |
| Project-local content-addressed C object cache with deterministic keys, generated-header dependency closure, concurrent-build locking, integrity checks, and atomic publication | ADR 0055 and a future stable backend identity | [0164](specs/0164-content-addressed-c-object-cache.md) |
| Rescope `Box<T>` and scoped references after RFC 0165 invalidated the Ref and Box designs; do not implement as written | RFC 0165 | [0149](specs/0149-box-and-call-scoped-references.md) |
| Rescope ownership and lifetime work after RFC 0165 rejected affine ownership, implicit moves, and automatic cleanup; do not implement as written | RFC 0165 | [0110](specs/0110-affine-ownership-and-stashes.md) |

### Revisit later; not scheduled

| Work | Spec |
| --- | --- |
| Extract and revisit RFC 0158's debug tracking allocator as a standalone test/workbench facility; do not schedule its consuming-receiver design | [0158](specs/deferred/0158-consuming-receivers-and-tracking-alloc.md) |
| Extract and revisit RFC 0157's aligned allocation separately from uninitialized allocation | [0157](specs/deferred/0157-uninit-allocation-and-alignment.md) |

## Deferred ideas

Open ideas under discussion live in `docs/specs/deferred/`, with a README
explaining the directory. They are deliberately not listed here: this board
is scheduled work, and enumerating unscheduled ideas alongside it is what made
them read as commitments. Nothing there is authoritative, and a deferred spec
that disagrees with `docs/reference.md` is wrong.

## Open bugs

A bug is real whether or not its owning spec is scheduled. The final entry is
owned by an unrelated deferred spec and therefore has no route to a fix today;
it remains visible because hiding it would not make it less true.

| Bug | Owning spec |
| --- | --- |
| Removing one Dict entry can make a later colliding entry unreachable because deletion clears a bucket inside the probe chain | **deferred, and misowned** -- [0151](specs/deferred/0151-remove-strand-and-modernize-arrays.md) is about removing `Strand` and array spelling, not Dict probing. This is a live correctness defect in shipped code and needs a real owner |

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
   `Atomic<T>`'s full operation set, plus exact-output coverage for root yield
   (single and repeated), spawn/join, Channel send/receive/close, Mutex
   contention, and root completion with no child, joined children, and a
   detached CPU child. `TestC23SnippetCatalogCompiles` separately
   Tier-1-compiles every workbench snippet under all three toolchains with no
   hand-listed fixture per snippet. What remains unverified:
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
  full root-cause account. A full untargeted sweep of the complete 140-snippet
  catalog now runs to completion and passes: `go test -count=1 -timeout=10m
  -parallel=8 -tags c23 -run TestC23SnippetCatalogCompiles
  ./compiler/tests/c23validation` finishes in 151s with 140/140 snippets
  passing under all three toolchains — before RFC 0140 (closed 2026-08-27)
  fixed a redundant-toolchain-discovery-per-snippet cost and parallelized the
  loop (plus two related correctness bugs the fix required: a compile-cache
  data race and a cache-entry-outliving-its-`t.TempDir()` bug, both latent
  and not triggered by today's catalog, per the archived spec), this same
  sweep ran past 50 minutes without asserting a single failure and never
  completed. Earlier defects fixed and verified
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
  over the full 140-snippet catalog; it had never been run to completion
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
