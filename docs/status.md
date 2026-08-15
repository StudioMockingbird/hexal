# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted.

## Open TODOs

### Design not started

| Work | Spec |
|---|---|
| Arena and Pool allocators | [0027](specs/0027-arena-and-pool-allocators.md) |
| Target profiles and representation evidence | [0052](specs/0052-target-profiles.md) |
| Filesystem and build driver | [0055](specs/0055-filesystem-and-build-driver.md) |

### Design decisions required

| Work | Spec |
|---|---|
| C interoperability — compiler core | [0039](specs/0039-c-interop-compiler-core.md) |
| Typed runtime I/O over `FILE *` | [0065](specs/0065-typed-io.md) |
| Default-Heap runtime collapse — direct checked `malloc`/`free`, needs a follow-up ADR (RFC 0069 audit finding; coordinates with 0027) | [0069](specs/archive/0069/0069-c23-backed-compiler-simplification.md) |

### Implementation-ready

| Work | Spec |
|---|---|
| Direct scalar lowering and source-faithful generated C | [0068](specs/0068-direct-safe-conversion-lowering.md) |
| Code comment contract and cleanup | [0070](specs/0070-code-comment-contract.md) |

## Unowned

One item survived the previous follow-up list without a determinable meaning:

- **Terminating self-recursive object construction.** Pointer-indirect
  self-recursion compiles and is valid per `reference.md`, so what this tracked
  is unclear. Assign it a spec or delete it.

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
- The generator emits helper families wholesale — equality, print, union,
  heap, io — so a small program's C contains many unused `static` helpers.
  Demand-driven helper emission would remove the dead code. (The old C23
  harness that tolerated `unused-*` warnings is retained but has no entry
  point.)
