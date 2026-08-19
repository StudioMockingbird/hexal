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
| Target profiles and representation evidence | [0052](specs/0052-target-profiles.md) |
| Filesystem and build driver | [0055](specs/0055-filesystem-and-build-driver.md) |

### Design decisions required

| Work | Spec |
|---|---|
| C interoperability — compiler core | [0039](specs/0039-c-interop-compiler-core.md) |
| Typed runtime I/O over `FILE *` | [0065](specs/0065-typed-io.md) |

### Implementation-ready

| Work | Spec |
|---|---|
| Module-typed collection elements — emit specializations where the element type is available | [0081](specs/0081-module-typed-collection-elements.md) |
| Demand-driven component dependencies — no hollow `hexal/*.h` artifacts | [0082](specs/0082-demand-driven-component-dependencies.md) |
| Text and collection surface — drop `List.set`, add `Dict.length`/`Dict.find`, remove text indexing | [0083](specs/0083-text-and-collection-surface.md) |
| Cached rune length — O(1) `String.length()`; supersedes 0083 Part B rename | [0087](specs/0087-cached-rune-length.md) |
| Provably dead bounds checks — elide Array checks the compiler already proved | [0088](specs/0088-provably-dead-bounds-checks.md) |
| Composed name encoding — stop re-embedding `hex_` inside union names | [0089](specs/0089-composed-name-encoding.md) |

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

## Open bugs

- **Module-owned object elements in List/Dict/Array/View specializations
  generate uncompilable C.** `List<M.Point>` compiles clean and emits C
  referencing an undeclared type: the program-wide component header
  `hexal/list.h` spells `hex_t_m1_m_Point` but never includes the module that
  defines it, and module headers never include one another. Direct use of the
  same type is correct — a consuming module re-emits the definitions it needs —
  so this is a layering inversion specific to shared component artifacts, not
  the include-order failure previously recorded here. No test, c23 canary, or
  snippet exercises it. Owned by
  [0081](specs/0081-module-typed-collection-elements.md).

## Known coverage gaps

Not bugs — deliberate limits worth remembering when reading a green test run.

- Runtime traps are unverifiable. The trap rules in `reference.md` (empty `pop`,
  missing Dict key, out-of-bounds index, zero divisor, shift count, float
  overflow, allocation failure, malformed UTF-8, close failure, Mutex misuse,
  task stack overflow, and others) and `print`'s exact output forms fire only
  in an executed generated binary. The runtime templates carry **36 distinct
  `[Runtime Error]` messages**, derived by regex over every
  `compiler/generator/packages/*.c` and `*.h` template (62 occurrences total).
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
  faulting. What is verified without execution: the whole generated
  concurrency runtime compiles and links under a strict `-std=c23` glibc
  x86-64 toolchain (zig cc) at both the default and a 64 KiB reserve, the
  reserve and commit spellings reach both platform allocation sites, the
  POSIX site documents the commit as unused there, and the snippet manifest
  moved only the ten task-spawning snippets.

  The second kind is the worse one: a textual assertion can catch an undeclared
  identifier, but nothing short of running the binary catches a task that is
  spawned twice. Each instance is fixed narrowly; the class stays open until
  generated C is compiled and executed.
- The generator emits helper families wholesale — equality, print, union,
  heap, io — so a small program's C contains many unused `static` helpers.
  Demand-driven helper emission would remove the dead code. (The old C23
  harness that tolerated `unused-*` warnings is retained but has no entry
  point.)
