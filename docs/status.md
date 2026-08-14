# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted.

## Open TODOs

### Approved, ready to implement

| Work | Spec |
|---|---|
| Reference contract cleanup — cohesiveness, eligibility, API completeness, syntax deduplication | [0050](specs/0050-reference-contract-cleanup.md) |

### Pending approval

| Work | Spec |
|---|---|

### Design not started

| Work | Spec |
|---|---|
| Arena and Pool allocators | [0027](specs/0027-arena-and-pool-allocators.md) |
| Modules, imports, and visibility | [0034](specs/0034-modules-and-imports.md) |
| C interoperability | [0039](specs/0039-c-interop.md) |
| Stream extensions — fallible steps, sources, terminal ops, producer cleanup | [0051](specs/0051-stream-extensions.md) |
| Target profiles and representation evidence | [0052](specs/0052-target-profiles.md) |

## Unowned

One item survived the previous follow-up list without a determinable meaning:

- **Terminating self-recursive object construction.** Pointer-indirect
  self-recursion compiles and is valid per `reference.md`, so what this tracked
  is unclear. Assign it a spec or delete it.

## Known coverage gaps

Not bugs — deliberate limits worth remembering when reading a green test run.

- Most runtime traps now have execution tests via the C23 harness's
  `trapGeneratedC` (zero divisor, bounds, empty `pop`, missing Dict key,
  conversion overflow, malformed UTF-8, String index, RuneCursor exhaustion).
  A few trap rules cannot be induced portably and remain execution-unverified:
  allocation failure, float overflow, shift count, close failure, Mutex
  misuse. Recorded by [0048](specs/0048-test-helpers-and-harness.md)
  Decision 3.
- The generator emits helper families wholesale — equality, print, union,
  heap, io — so a small program's C contains many unused `static` helpers.
  The C23 harness tolerates `unused-function`, `unused-variable`, and
  `unused-parameter` warnings; all other warnings still fail. Demand-driven
  helper emission would remove the dead code. Recorded by
  [0048](specs/0048-test-helpers-and-harness.md) (C23 harness section).
