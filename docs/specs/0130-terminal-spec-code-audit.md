# RFC 0130: Terminal Specification Code Audit

- Kind: Architecture Decision Record
- Status: Implementation-ready; audit complete, remediation not started
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

The count is the smaller problem. The *method* is wrong, so the number will
drift again for the same reason, and the entry instructs future readers to
re-derive it using the method that produced the error. Both the count and the
method must be corrected, the method naming both emission sites, and the number
re-derived at remediation time rather than copied from this RFC.

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

Terminal specifications may be deleted without further code verification beyond
the remediation below.

The reasoning: an implemented spec's claims are either already true of the
code, in which case the code is the better record, or they are false, in which
case preserving them is harmful. This audit found no instance of the second
kind other than F1, which is a defect in a live document rather than in a
terminal one.

What this audit cannot say is that the other thirty-seven specs' Validation
sections all pass. That is the standing generated-C coverage gap, and RFC 0125
owns closing it by compiling and running generated programs. Deleting the specs
does not widen that gap: nothing was executing those assertions anyway.

## Required remediation

Before RFC 0129 deletes anything:

- Correct `docs/status.md`'s trap record to 47 distinct messages, re-derived at
  the time of the change, and rewrite its method to name both the runtime
  templates and the Go generator as emission sites.
- Record in RFC 0129's Phase 1 that a claim which is historically true and
  currently false reaches Git history only, never `docs/reference.md`.

## Non-goals

- Re-verifying every terminal specification's Validation section. The coverage
  limit is stated rather than papered over.
- Executing any generated program. RFC 0125 owns that.
- Migrating knowledge, rewriting citations, or deleting files. RFC 0129 owns
  all three.
- Changing any language rule, generated output, or compiler behavior.

## Validation

This section is exhaustive.

- `docs/status.md` records 47 distinct `[Runtime Error]` messages, and the
  count is re-derived from the tree at the time of the change rather than
  copied from this RFC.
- That record names both the runtime templates and the Go generator as
  emission sites, and no longer instructs re-derivation by a template-only
  regex.
- Re-deriving the count by the corrected method reproduces the recorded number.
- RFC 0129's Phase 1 states that a historically-true, currently-false claim
  reaches Git history only.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass
  unchanged; this RFC touches no code.

## Reference synchronization

None. This RFC changes no language rule and adds nothing to
`docs/reference.md`. Its one documentation change is to `docs/status.md`'s
coverage-gap record, which is open-work tracking rather than canonical
language meaning.
