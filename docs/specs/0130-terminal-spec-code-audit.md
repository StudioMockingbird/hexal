# RFC 0130: Terminal Specification Code Audit

- Kind: Architecture Decision Record
- Status: Implementation-ready; audit complete, remediation partially landed
  (the `docs/status.md` trap record and its derivation method are already
  corrected in the tree; the remaining phases have not run)
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: whether the implementation claims made by terminal specifications
  still hold against the tree, which drift is real, and which is the process
  working as intended
- Depends on: `docs/status.md`, `docs/reference.md`, and the current tree
- Coordinates with: RFC 0129 (delete closed specifications), which owns
  knowledge migration and citation rewriting; this RFC owns code truth
- Changes no Hexal grammar, type, function signature, or result contract

## Summary

RFC 0129 deletes the terminal specifications and protects the *knowledge* in
them. This RFC protects the other half: whether what those specifications claim
about the implementation is still true.

The two questions are different. A spec can be perfectly preserved as knowledge
and still describe code that no longer behaves that way, and a claim that
nobody checks before deletion is a claim that silently becomes folklore.

The audit found one real defect, three claims worth recording as verified, and
two classes of apparent drift that are not drift.

## Why this is separate from RFC 0129

RFC 0129 asks "what facts must survive, and where do they go". This RFC asks
"were the facts true".

Neither subsumes the other. RFC 0129's knowledge disposition assumes each
extracted claim is correct before it is relocated; relocating a stale claim
into `docs/reference.md` would make the normative contract wrong, which is
worse than deleting the spec unread. This audit is therefore an input to RFC
0129's Phase 1, and its Findings are the claims that must not be migrated
as-is.

One audit result — an active RFC containing an instruction to edit a terminal
one — is a link-graph problem rather than a code problem, and lives in RFC 0129
where the rest of the link-graph work is.

## Audit coverage, stated honestly

This audit did **not** re-verify all forty implemented specifications'
Validation sections line by line. That is several thousand assertions and would
be its own project.

Complete and mechanical:

- every `[Runtime Error]` message named in a terminal spec, compared against
  every message the tree emits from both emission sites;
- every code-shaped identifier the terminal specs name, checked against the
  tree;
- every citation from a live document into a terminal spec, enumerated.

Targeted, on the interactions most likely to have drifted: recent specs, specs
that changed generated C, and specs whose subject a later spec revisited.

A claim absent from this document was not proven correct. It was not examined.
Implementation must not read this RFC as a clean bill of health for the other
thirty-seven.

## Findings

### F1. The runtime trap record is derived by a method that cannot see every trap — FIX

`docs/status.md` records the runtime trap inventory as "derived by regex over
every `compiler/generator/packages/*.c` and `*.h` template". Traps are emitted
from **two** places and that regex sees one. Two messages exist only in Go
generator code:

- `[Runtime Error] collection modified during iteration`, from
  `compiler/generator/for.go`;
- `[Runtime Error] numeric operation failed`.

The true count is **47 distinct messages**, not the 45 recorded.

The count is the smaller problem, and it is cited here only as evidence that
the method is blind to one source. The *method* is wrong, so any number it
produces drifts again for the same reason, and the entry instructs future
readers to re-derive using the method that produced the error.

The response is not to correct the number. See Decision below: no count is
retained at all, because a number that must be re-derived to be trusted is not
worth recording between derivations.

The record was last corrected during the stateless-Heap migration by this
author, who reproduced the flawed method rather than questioning it. That is
the failure mode this RFC exists to catch: a number checked against the wrong
denominator looks verified.

### F2. Terminal specs describe traps the code no longer has — ACCEPT

Several name `[Runtime Error] double deallocation` and `[Runtime Error]
deallocation used the wrong allocator`, both removed with the allocation header
during the stateless-Heap migration.

This is not drift. A terminal spec records what was decided and built at the
time, and a later spec superseding it is the process working. No action, and
the question does not survive deletion.

The distinction matters for RFC 0129's sweep: a claim that is *historically*
true and *currently* false must reach Git history, never `docs/reference.md`.

### F3. Specs name identifiers absent from the tree — ACCEPT

A mechanical sweep flagged many, for example `hex_union_7_int32_t9_nullptr_t`,
`hex_f_m1_a_identity_Int32`, and `hex_list_Point_m1_s`.

Inspection shows these are illustrative — worked examples of a naming scheme,
and in several cases deliberately *rejected* spellings shown to explain why a
scheme was replaced. Others are test files and helpers renamed or absorbed by
later refactors, which is the code improving on the spec.

No action. Recorded so the same sweep is not re-run and re-triaged.

## Verified sound

Recorded because a later reader should not re-derive them, and because two of
the three looked like defects before they were checked. These are the claims
RFC 0129 may migrate with confidence.

- **Iterator invalidation is fully implemented.** `hex_list_<T>` and
  `hex_dict_<K>_<V>` each carry one `size_t version` field, every structural
  mutation increments it, and the traversal check and trap are emitted from
  `compiler/generator/for.go` for traversals whose safety is not proven.
  This initially read as unimplemented because its trap appears in no runtime
  template — the same wrong denominator as F1.
- **The String `rune_length` invariant survived the stateless-Heap
  migration.** Rewriting `string.c`'s allocation path did not disturb the rule
  that every String construction sets `rune_length`; all four construction
  sites still do, and the two delegating constructors inherit it.
- **The comment policy is enforced.** `comment_policy_test.go` exists at the
  repository root and the tree contains zero RFC citations in code comments. An
  earlier step of this audit reported the guard missing; that search was scoped
  to `compiler/` and the guard is at the root.

## Decision

The findings verified here are safe inputs to RFC 0129; they are not a clean
bill of health for claims this audit did not examine. RFC 0129 still verifies
every current-behavior claim it considers migrating. A true claim is better
owned by current code or authority, while a false historical claim remains in
Git history only.

What this audit cannot say is that the other thirty-seven specs' Validation
sections all pass. That is the standing generated-C coverage gap, and RFC 0125
owns closing it by compiling and running generated programs. Deleting the specs
does not widen that gap: nothing was executing those assertions anyway.

## Decision: do not retain an exact trap count

`docs/status.md` records only that runtime traps arise from package templates
and non-test Go generator emission. The number is neither a language contract
nor a useful temporary invariant, and it has already drifted repeatedly.
RFC 0125 derives the current message set directly from both production sources
when building executable fixtures. No source test parses prose or preserves an
intermediate count.

## Required sweep

Invariants the implementation holds throughout, distinct from the ordered steps
below:

- Derive the trap set from the tree during remediation. No number in this RFC
  is authoritative over the code, including 47, and no derived count is
  retained afterward.
- Change no runtime message, trap site, check order, or generated output. This
  RFC corrects a record about the code, never the code.
- Correct the derivation method wherever it appears, not only its result. A
  right number produced by a method that cannot see every trap is the state
  this RFC exists to end.
- Land before RFC 0129 deletes anything, so its Phase 1 consumes verified
  findings rather than re-deriving them.
- Leave `docs/reference.md` untouched. The trap inventory is open-work tracking,
  not canonical language meaning.

## Implementation plan

### Phase 0: re-derive, do not copy

1. Record the green test/vet baseline.
2. Use a temporary Go probe under `.tmp/` to derive the distinct trap set from
   production emission sources only:
   - `.c` and `.h` templates directly under
     `compiler/generator/packages/`; and
   - non-`_test.go` Go files directly under `compiler/generator/`.
   Test fixtures, generated-output assertions, archive text, and checker files
   are not emission sources and are excluded. Unquote Go string literals before
   extracting their generated runtime text rather than counting source escapes.
3. Compare the derived set with the claims recorded in `docs/status.md` and
   this RFC only to confirm the audit scope. The tree wins on any mismatch; do
   not edit code to match a historical count.

### Phase 1: correct the record and the method

1. Remove the exact count and correction history; state only that the
   unexecuted trap set is emitted by runtime templates and non-test Go generator
   code and will be derived by RFC 0125.
2. Delete the template-only derivation instruction and the
   completed history of thirteen, thirty-nine, and forty-five. `docs/status.md`
   records the current gap, not its correction narrative.

### Phase 2: hand off to RFC 0129

1. Add to RFC 0129's Phase 1 the rule that a claim which is historically true
   and currently false reaches Git history only, never `docs/reference.md`.
2. Confirm RFC 0129's Phase 1 consumes this RFC's Verified sound section rather
   than re-deriving those three claims.
3. Confirm RFC 0127 now owns its native-initialization fact directly and RFC
   0129 uses that corrected case as the precedent for finding similar dangling
   instructions.

### Phase 3: conformance

1. Implement every validation item below and no additional behavior.
2. Run `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...`.
3. Verify the snippet manifest is unchanged; this RFC touches no generated
   output.
4. Remove this RFC's `docs/status.md` row once every item passes. This RFC is
   then **terminal but not yet deleted**.

   Its own completion does not depend on its deletion. RFC 0129 is blocked on
   this RFC's remediation, so requiring deletion here would make each RFC wait
   on the other. Deletion happens later, on RFC 0129's ordinary sweep, which
   treats this RFC exactly like any other terminal specification.

## Non-goals

- Re-verifying every terminal specification's Validation section. The coverage
  limit is stated rather than papered over.
- Executing any generated program. RFC 0125 owns that.
- Migrating knowledge, rewriting citations, or deleting files. RFC 0129 owns
  all three.
- Changing any language rule, generated output, or compiler behavior.

## Validation

This section is exhaustive.

- This RFC becomes terminal when its remediation items pass. Deletion is not a
  completion condition here and is not listed below: RFC 0129 owns it, is
  blocked on this remediation, and would otherwise be waiting on a step that
  waits on it.
- The production trap set is re-derived from package templates and non-test Go
  generator files only; test fixtures and checker files contribute nothing.
- `docs/status.md` names both production emission sources and no longer carries
  the old template-only method or count-correction history.
- `docs/status.md` carries no exact trap count and no `trap_inventory_test.go`
  is added; RFC 0125 derives the set for fixtures.
- RFC 0129's Phase 1 states that a historically-true, currently-false claim
  reaches Git history only, and consumes this RFC's Verified sound section
  rather than re-deriving it.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.
- The snippet manifest is unchanged; this RFC alters no generated output.

## Reference synchronization

None. This RFC changes no language rule and adds nothing to
`docs/reference.md`. Its one documentation change is to `docs/status.md`'s
coverage-gap record, which is open-work tracking rather than canonical
language meaning.
