# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted; the Unowned section below is the staging area for items whose
meaning or home is undetermined, and each entry there commits to one or the
other.

## Open TODOs

### Design not started

| Work | Spec |
|---|---|
| Compiler property testing and fuzzing — reject-path oracles over arbitrary input, accept-path metamorphic properties over generated valid programs | [0124](specs/0124-compiler-fuzzing.md) |
| External C23 validation — dual GCC/Clang tagged suite closing the generated-C coverage gap | [0125](specs/0125-external-c23-validation.md) |

### Design decisions required

| Work | Spec |
|---|---|
| C interoperability — compiler core | [0039](specs/0039-c-interop-compiler-core.md) |
| Target profiles and representation evidence | [0052](specs/0052-target-profiles.md) |
| Filesystem, build, and validation driver | [0055](specs/0055-filesystem-and-build-driver.md) |
| Language surface audit dispositions — 34 findings; promote/reject/accept per finding | [0103](specs/0103-language-surface-audit.md) |
| Affine ownership and Arena/Pool lifetimes | [0110](specs/0110-affine-ownership-and-arenas.md) |
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
- **Parser expression-start classification is scattered.** Raised during the
  refactor audit alongside RFC 0077's literal table as the same class of
  scattered classification. It shares no code with the literal table and is not
  covered by RFC 0074. Recorded here rather than left implied-homed. Assign it a
  spec or delete it.

## Known coverage gaps

Not bugs — deliberate limits worth remembering when reading a green test run.

- Runtime traps are unverifiable. The trap rules in `reference.md` (empty `pop`,
  missing Dict key, out-of-bounds index, zero divisor, shift count, float
  overflow, allocation failure, malformed UTF-8, close failure, Mutex misuse,
  task stack overflow, and others) and `print`'s exact output forms fire only
  in an executed generated binary. The runtime templates carry **45 distinct
  `[Runtime Error]` messages**, derived by regex over every
  `compiler/generator/packages/*.c` and `*.h` template (71 occurrences total).
  RFC 0123 removed two of them with the allocation header: `double
  deallocation` and `deallocation used the wrong allocator`. The count this
  entry carried before that measurement was 39 distinct and 70 occurrences,
  which was already stale against a tree measuring 47 and 79. An earlier
  revision said thirteen, which was never sourced and is wrong. Re-measure
  rather than adjusting the number by hand, and do not re-introduce a count
  without deriving it. Nothing executes one yet: the retained
  `compiler/tests/c23validation/c23_*_test.go` files have no runnable entry
  points, so trap firing and exact runtime output stay unverified. RFC 0125
  owns closing this; the earlier prohibition on invoking an external toolchain
  has been lifted, so the remaining obstacle is that the suite does not exist,
  not that it is disallowed.
- **Defects in generated C are invisible to the whole suite.** No test invokes a
  toolchain and the c23 canaries are dormant, so generated C that is wrong — in
  either of two ways — passes `go test ./...`, `go vet ./...`, and
  `go vet -tags c23` alike.

  *Does not compile:* 0073 D2 (handle types reachable only through a
  declaration) and D33 (`uint64_t` in a Size-only program), each found by a
  different external review.

  *Compiles and behaves wrongly:* RFC 0084's C1 (a `try` in a nested block
  ran its operand twice, and `try spawn` in a loop spawned one task too many
  and leaked it) and C3 (POSIX fiber stacks lacked the guard page
  `reference.md` promises, so an overflow corrupted the heap silently). Both
  were found while hand-writing one example program and are fixed; the class
  stays open. The guard-page fault itself remains unverified: no test
  executes generated C, so nothing observes a fiber overflow hitting the
  `PROT_NONE` page.

  RFC 0085's runnable validation also remains unverified for the same reason:
  resident cost per Task before and after the stack rework, 10,000
  concurrently live Tasks, the `[Runtime Error] task stack overflow` trap
  firing and process exit on both platforms, the unchanged re-raise of a
  fault outside any Task guard page, a 64 KiB reserve overflowing sooner than
  1 MiB, and a POSIX Task using more than the 8 KiB initial commit without
  faulting. What is verified: the reserve and commit spellings reach both
  platform allocation sites, the POSIX site documents the commit as unused
  there, and the snippet manifest moved only the ten task-spawning snippets —
  all by textual assertion, which is the only mechanism available.

  RFC 0087's runtime validation is unverified for the same reason: that a
  slice over multi-byte input is byte-identical to the scanning version, and
  that a concatenated count matches an independent scan of the result. What is
  verified textually: every one of the five construction paths sets
  `rune_length`, a multi-byte literal gets a rune count rather than a byte
  count, `length()` and `slice` read the field instead of scanning, and the
  `List<String>` element is still one pointer.

  An earlier revision of this entry also claimed the generated concurrency
  runtime compiles under an external `-std=c23` toolchain. That claim was
  withdrawn because producing it was a manual one-off that nothing in the
  repository reproduces. Invoking a toolchain is no longer prohibited, but the
  claim stays withdrawn on the same grounds: it is reproducible evidence or it
  is not evidence. RFC 0125 makes it reproducible under both GCC and Clang.
  Until then the compile status of that runtime is unverified, like every
  other generated artifact.

  The second kind is the worse one: a textual assertion can catch an undeclared
  identifier, but nothing short of running the binary catches a task that is
  spawned twice. Each instance is fixed narrowly; the class stays open until
  generated C is compiled and executed.
- The generator emits helper families wholesale — equality, print, union,
  heap, io — so a small program's C contains many unused `static` helpers.
  Demand-driven helper emission would remove the dead code. The C23 harness
  tolerates the resulting `unused-*` warnings; RFC 0125 keeps those
  suppressions pointed at this entry as their owning gap, so narrowing them is
  this item's job rather than the suite's.
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
