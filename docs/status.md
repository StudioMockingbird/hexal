# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted.

## Open bugs

Conformance defects where the compiler disagrees with `reference.md`, which is
normative. All are verified reproductions.

| Bug | Spec |
|---|---|
| `Rune` binary arithmetic (`+ - * / %`) is accepted; bitwise, shift, and unary `-` correctly reject | [0049](specs/0049-conformance-bug-batch.md) §1 |
| Root-scope `errdefer` reports `Unknown Error` instead of a Type Error | [0049](specs/0049-conformance-bug-batch.md) §2 |
| `match` scrutinees and arm results reject unparenthesized `and`/`or` | [0049](specs/0049-conformance-bug-batch.md) §3 |
| `ref rows[0].field` rejected; `ref pair.values[0]` accepted | [0049](specs/0049-conformance-bug-batch.md) §4 |
| Lexer accepts a raw newline inside a String literal | [0049](specs/0049-conformance-bug-batch.md) §5 |

## Open TODOs

### Approved, ready to implement

| Work | Spec |
|---|---|
| Reference contract cleanup — cohesiveness, eligibility, API completeness, syntax deduplication | [0050](specs/0050-reference-contract-cleanup.md) |

### Pending approval

| Work | Spec |
|---|---|
| 16- and 32-bit `Size` target profiles | [0049](specs/0049-conformance-bug-batch.md) §6 |
| Consolidate the duplicated generator program traversal (refactor) | [0049](specs/0049-conformance-bug-batch.md) §7 |
| Test helpers, subtests, pure-Go testing, delete C23 toolchain tests | [0048](specs/0048-test-helpers-and-harness.md) |

**Sequencing:** 0049 §7 must land before 0048 removes the C23 toolchain tests.
Those 16 tests are the only check that a 15-file generator refactor still
produces compilable C.

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

- Runtime traps are unverifiable. Thirteen trap rules in `reference.md` (empty
  `pop`, missing Dict key, out-of-bounds index, zero divisor, shift count,
  float overflow, allocation failure, malformed UTF-8, close failure, Mutex
  misuse) and `print`'s exact output forms can only be observed by executing a
  generated binary. String assertions confirm a trap call is emitted, never
  that it fires. Recorded by [0048](specs/0048-test-helpers-and-harness.md)
  Decision 3.
- Generated C is not verified to compile once 0048 removes the toolchain tests.
