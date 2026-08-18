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
| Audit refactor batch | [0074](specs/0074-audit-refactor-batch.md) |

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
  generate uncompilable C.** A specialization whose element is a module
  object (e.g. `List<Point>`) spells the module's C typedef name inside the
  program-wide component header, before the module header defines it. This
  defect is pre-existing (the pre-split `hexal.h` had the same include-order
  failure) and predates ADR 0071's component split; no test, c23 canary, or
  snippet exercises it. Needs a representation RFC (e.g. handle-based
  collection storage) before module objects can be collection elements. Formerly
  owned by 0071, which was archived and removed; it needs a new owner RFC.

## Known coverage gaps

Not bugs — deliberate limits worth remembering when reading a green test run.

- Runtime traps are unverifiable. Thirteen trap rules in `reference.md` (empty
  `pop`, missing Dict key, out-of-bounds index, zero divisor, shift count,
  float overflow, allocation failure, malformed UTF-8, close failure, Mutex
  misuse) and `print`'s exact output forms fire only in an executed generated
  binary. Per policy, no test may execute one: the retained
  `compiler/tests/c23validation/c23_*_test.go`
  files are pure Go and have no runnable entry points, so trap firing and
  exact runtime output stay unverified.
- **Undeclared identifiers in generated C are invisible to the whole suite.** No
  test invokes a toolchain and the c23 canaries are dormant, so generated C that
  references a type no header declares passes `go test ./...`, `go vet ./...`,
  and `go vet -tags c23` alike. Two instances are known — 0073 D2 (handle types
  reachable only through a declaration) and D33 (`uint64_t` in a Size-only
  program) — each found by a different external review rather than by the suite.
  Both are fixed narrowly with textual include/reference assertions; the class
  stays open until generated C is compiled.
- The generator emits helper families wholesale — equality, print, union,
  heap, io — so a small program's C contains many unused `static` helpers.
  Demand-driven helper emission would remove the dead code. (The old C23
  harness that tolerated `unused-*` warnings is retained but has no entry
  point.)
