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
| Arena and Pool allocators; Default-Heap runtime collapse — direct checked `malloc`/`free` decision (RFC 0069 audit finding) | [0027](specs/0027-arena-and-pool-allocators.md) |

### Design decisions required

| Work | Spec |
|---|---|
| Deterministic evaluation order — order rules settled; the bundled mixed-operator rejection needs a decision | [0111](specs/0111-deterministic-evaluation-order.md) |
| C interoperability — compiler core | [0039](specs/0039-c-interop-compiler-core.md) |
| Target profiles and representation evidence | [0052](specs/0052-target-profiles.md) |
| Filesystem, build, and validation driver | [0055](specs/0055-filesystem-and-build-driver.md) |
| Blocking-syscall boundary — what the scheduler does at a blocking call; gates sockets and readiness-based I/O | [0091](specs/0091-blocking-syscall-boundary.md) |
| Language surface audit dispositions — 34 findings; promote/reject/accept per finding | [0103](specs/0103-language-surface-audit.md) |
| Affine ownership and Arena/Pool lifetimes | [0110](specs/0110-affine-ownership-and-arenas.md) |
| Order-independent function visibility — inverts three RFC 0094 Validation cases; land 0094 first | [0112](specs/0112-order-independent-function-visibility.md) |
| Library end-of-stream value replacing builtin EoS | [0113](specs/0113-library-end-of-stream.md) |
| Sum-type surface simplification | [0114](specs/0114-sum-type-surface.md) |
| Iterator invalidation contract | [0115](specs/0115-iterator-invalidation.md) |
| Native module storage and linkage | [0116](specs/0116-native-module-storage-and-linkage.md) |
| Restricted compile-time evaluation | [0117](specs/0117-compile-time-evaluation.md) |
| Concurrency safety and task lifetimes | [0118](specs/0118-concurrency-safety-and-task-lifetimes.md) |

### Implementation-ready

| Work | Spec |
|---|---|
| Function literals, local functions, and Fun value placement | [0094](specs/0094-anonymous-function-literals.md) |
| Explicit dispatch tables — depends on 0094 for member admission; owns the member-call rule | [0109](specs/0109-explicit-dispatch-tables.md) |
| Refactoring audit residue — R21 coverage matrix, R25 checker context bundle, R47 API docs; R22-R24 split-or-drop decided here | [0119](specs/0119-refactor-audit-residue.md) |

### Design settled; implementation blocked

| Work | Blocked by | Spec |
|---|---|---|

## Open bugs

| Bug | Owning spec |
|---|---|
| Array, View, and List equality for an unsupported element type reports an empty member name instead of an element path | [0094](specs/0094-anonymous-function-literals.md) |

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
  in an executed generated binary. The runtime templates carry **39 distinct
  `[Runtime Error]` messages**, derived by regex over every
  `compiler/generator/packages/*.c` and `*.h` template (70 occurrences total).
  An earlier revision of this entry said thirteen, which was never sourced and
  is wrong. Do not re-introduce a count without deriving it. Per policy, no
  test may execute one: the retained
  `compiler/tests/c23validation/c23_*_test.go`
  files are pure Go and have no runnable entry points, so trap firing and
  exact runtime output stay unverified.
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
  runtime compiles under an external `-std=c23` toolchain. That claim is
  withdrawn, not because it was false but because producing it required
  invoking a compiler, which this project does not do. It was a manual
  one-off, nothing in the repository reproduces it, and leaving it recorded
  would present a prohibited action as an available one. The compile status of
  that runtime is unverified, like every other generated artifact.

  The second kind is the worse one: a textual assertion can catch an undeclared
  identifier, but nothing short of running the binary catches a task that is
  spawned twice. Each instance is fixed narrowly; the class stays open until
  generated C is compiled and executed.
- The generator emits helper families wholesale — equality, print, union,
  heap, io — so a small program's C contains many unused `static` helpers.
  Demand-driven helper emission would remove the dead code. (The old C23
  harness that tolerated `unused-*` warnings is retained but has no entry
  point.)
