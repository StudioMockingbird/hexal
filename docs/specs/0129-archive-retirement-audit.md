# RFC 0129: Archive Retirement Audit

- Kind: Architecture Decision Record
- Status: Implementation-ready; audit complete, remediation not started
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: audit the 48 archived specs against the tree, triage every gap found,
  and state what must be remediated before the archive is deleted
- Depends on: `docs/status.md`, `docs/reference.md`, and `AGENTS.md`'s
  spec-process rules
- Coordinates with: RFC 0125 (external validation) and RFC 0127 (native
  threading primitives), both of which cite archived specs
- Changes no Hexal grammar, type, function signature, or result contract

## Summary

`docs/specs/archive/` holds 48 specs and 17,324 lines. Forty are implemented;
eight were discarded or rejected and describe decisions not to build something.

Deleting them is reasonable — implemented specs are superseded by the code and
by `reference.md`, which is the normative contract. But the archive is not
isolated. Eleven live documents carry more than thirty citations into it,
including `reference.md` itself, and one live RFC contains an instruction to
edit an archived spec.

This RFC records what the audit found, triages each finding into fix or accept,
and states the remediation that must land before deletion.

## Audit coverage, stated honestly

This audit did **not** re-verify all forty implemented specs' Validation
sections line by line against the code. That is several thousand assertions and
would be its own project.

What it did do, mechanically and completely:

- compared every `[Runtime Error]` message named in an archived spec against
  every message the tree emits, from both the runtime templates and the Go
  generator;
- extracted every code-shaped identifier the archived specs name and checked it
  against the tree;
- enumerated every citation from a live document into an archived spec.

And, in a targeted way, verified the interactions most likely to have drifted:
recent specs, specs that changed generated C, and specs whose subject a later
spec revisited.

Findings below are therefore sound but not exhaustive. A claim absent from this
document was not proven correct; it was not examined.

## Findings

### F1. Live documents cite specs that are about to be deleted — FIX

Eleven live documents reference archived specs:

| Document | Cites |
|---|---|
| `docs/reference.md` | 0087, 0095, 0099 |
| `docs/status.md` | 0074, 0077, 0084, 0085, 0087, 0123 |
| RFC 0125 | 0073, 0084, 0085, 0087, 0115, 0122, 0123 |
| RFC 0118 | 0091, 0108, 0121, 0122 |
| RFC 0127 | 0085, 0121, 0122 |
| RFC 0110 | 0108, 0123 |
| RFC 0103 | 0094, 0095 |
| RFC 0124 | 0088, 0111 |
| RFC 0027, RFC 0052 | 0123 |
| RFC 0116 | 0094 |

`docs/reference.md` is the worst case. It is the sole normative statement of
what the language means, and after deletion it would cite three documents that
do not exist.

`docs/status.md`'s Known coverage gaps are the second worst. Those entries
record exactly which generated-C behavior is unverified, and each is anchored to
the spec that discovered it. They are load-bearing, not decorative.

Citations divide into two kinds, and they need different treatment:

- **Attribution** — "RFC 0084's C1 found this". Historical colour. The fact
  survives without the citation; rewrite as "an earlier audit found".
- **Load-bearing** — a citation the reader must follow to understand or act on
  the sentence. These must be discharged by inlining the fact before the target
  disappears.

### F2. RFC 0127 instructs an edit to an archived spec — FIX

RFC 0127 requires that "RFC 0122's Failure behavior gains one sentence" noting
that lifecycle-mutex initialization failure becomes statically unreachable on
Windows, because `InitializeSRWLock` cannot fail.

That instruction becomes undischargeable the moment 0122 is deleted, and the
fact it carries is real: it describes live behavior of the code 0127 will
produce. The fact must move into RFC 0127 itself, and the cross-spec
instruction must be removed. Its Required sweep item goes with it.

### F3. The trap-message count in `status.md` uses a methodology that cannot see every trap — FIX

`docs/status.md` records the runtime trap inventory as "derived by regex over
every `compiler/generator/packages/*.c` and `*.h` template". That regex cannot
see traps emitted from Go generator code, and two exist:

- `[Runtime Error] collection modified during iteration`, emitted from
  `compiler/generator/for.go` — RFC 0115's iterator-invalidation trap;
- `[Runtime Error] numeric operation failed`.

The true count is **47 distinct messages**, not the 45 currently recorded. More
importantly the *methodology* is wrong, so the number will drift again for the
same reason.

This audit found it while checking whether 0115 had actually been implemented.
The check initially suggested it had not, because 0115's trap message appears
nowhere in the templates. It is implemented; the search was looking in one of
the two places traps come from.

The entry must name both emission sites. The count was last corrected during
RFC 0123 by this author, who reproduced the flawed methodology rather than
questioning it.

### F4. Archived specs describe behavior the code no longer has — ACCEPT

Several archived specs specify runtime traps that no longer exist, notably
`[Runtime Error] double deallocation` and `[Runtime Error] deallocation used
the wrong allocator`, both removed by RFC 0123.

This is not drift. An archived spec records what was decided and built at the
time; a later spec superseding it is the process working. No action, and the
question does not survive deletion anyway.

### F5. Specs name identifiers that do not exist in the tree — ACCEPT

A mechanical sweep flagged many spec-named identifiers as absent, for example
`hex_union_7_int32_t9_nullptr_t`, `hex_f_m1_a_identity_Int32`, and
`hex_list_Point_m1_s`.

Inspection shows these are illustrative — worked examples of a naming scheme,
not claims that a specific symbol exists. Some are deliberately *rejected*
spellings shown to explain why a scheme was replaced. No action.

The sweep also flagged names of test files and helper functions that were
renamed or absorbed during later refactors. Those are equally uninteresting:
the spec described an implementation that has since been improved, which is the
outcome the process wants.

## Verified sound

Recorded because a later reader should not have to re-derive them, and because
two of the three looked like defects before they were checked.

- **RFC 0115 is fully implemented.** `hex_list_<T>` and `hex_dict_<K>_<V>` each
  carry one `size_t version` field, every structural mutation increments it,
  and the traversal check plus trap are emitted from `compiler/generator/for.go`
  for unproven traversals.
- **RFC 0087's invariant survived RFC 0123.** Rewriting `string.c`'s allocation
  path to the stateless Heap did not disturb the rule that every String
  construction sets `rune_length`; all four construction sites still do, and
  the two delegating constructors inherit it.
- **RFC 0097 is fully implemented and enforcing.** `comment_policy_test.go`
  exists at the repository root, and the tree currently contains zero RFC
  citations in code comments. An earlier step of this audit reported the guard
  missing; that search was scoped to `compiler/` and the guard is at the root.

## Decision

Delete the archive, after the remediation below and not before.

The reasoning: an implemented spec's content lives in three more durable
places — the code, `reference.md`, and the commit that landed it. Git retains
the files, so deletion loses nothing that cannot be recovered by path. What
deletion genuinely destroys is the *link graph*, and that is exactly what F1
and F2 are about.

Discarded specs are treated the same way. Their value is the record of a
decision not to build something, and where that record still matters it is
already cited from a live document — which F1's remediation inlines.

## Required remediation

Before any file is deleted:

- Rewrite every citation in `docs/reference.md` to state its fact directly. The
  normative contract must not depend on a document outside itself.
- Rewrite `docs/status.md`'s Known coverage gaps so each entry states what is
  unverified without requiring the reader to open an archived spec. Preserve
  every gap; only the attribution changes.
- Move RFC 0127's Windows-unreachability fact into RFC 0127 and delete its
  instruction to amend RFC 0122, along with the matching Required sweep item.
- Correct `docs/status.md`'s trap record to 47 distinct messages and change the
  methodology to name both emission sites: the runtime templates and the Go
  generator.
- Rewrite the remaining citations in RFC 0027, 0052, 0103, 0110, 0116, 0118,
  0124, and 0125 so each sentence stands alone. Where a citation is pure
  attribution, drop it; where it is load-bearing, inline the fact.
- Confirm no live document references `docs/specs/archive/` by path.

## Non-goals

- Re-verifying all forty implemented specs' Validation sections. The coverage
  limit is stated above rather than papered over.
- Preserving archived specs in another form. Git history is the archive.
- Changing any language rule, generated output, or compiler behavior. This RFC
  is documentation remediation only.
- Deciding whether the flat spec numbering continues after deletion. Numbers
  are never reused regardless, so deletion does not free any.

## Validation

This section is exhaustive.

- No live document under `docs/` contains a reference to a spec number that has
  no file, and none references `docs/specs/archive/` by path.
- `docs/reference.md` contains no RFC citation at all.
- Every Known coverage gap in `docs/status.md` present before this change is
  present after it, stating the same unverified behavior.
- `docs/status.md` records 47 distinct `[Runtime Error]` messages and names
  both the runtime templates and the Go generator as emission sites. The count
  is re-derived at remediation time rather than copied from this RFC.
- RFC 0127 states the Windows unreachability of lifecycle-mutex initialization
  failure in its own text, and instructs no edit to any archived spec.
- `docs/specs/archive/` does not exist.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass
  unchanged; this RFC touches no code.

## Reference synchronization

`docs/reference.md` is edited by this RFC only to remove citations, never to
change a rule. Verify after remediation that every rule it states reads
identically in substance, and record that no semantic change was made.
