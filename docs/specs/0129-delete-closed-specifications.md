# RFC 0129: Delete Closed Specifications

- Kind: Architecture Decision Record (ADR)
- Status: Implementation-ready; blocked on RFC 0130 remediation
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: every terminal specification, the `docs/specs/archive/` directory,
  canonical-document provenance, active-spec dependencies, and the permanent
  closed-spec lifecycle
- Depends on: RFC 0130 (terminal-spec code audit) completing first,
  `docs/reference.md` as the sole language authority,
  `docs/status.md` as the open-work board, Git history as the historical record,
  and the CARE comment policy in `AGENTS.md`
- Does not change language behavior or generated artifacts

## Decision

Delete every terminal specification from the working tree after its surviving
knowledge has been verified and moved to the correct current owner.

- Delete every file under `docs/specs/archive/`, regardless of whether its
  terminal status says Closed, Implemented, Discarded, Superseded, or Rejected.
- Delete any terminal-status specification found outside the archive.
- Never move a completed specification into an archive again.
- When an active specification becomes terminal, perform the knowledge sweep,
  remove its `docs/status.md` entry, and delete the specification in the same
  completion change.
- Git history is the historical record. The working tree contains only active
  proposals and current authorities.
- Specification numbers remain permanently reserved after deletion. A new
  number is greater than every number ever assigned in the repository; gaps are
  never reused. Git history resolves the highest deleted number when the current
  tree is insufficient.
- This ADR deletes itself last after every validation condition passes. Its
  number remains reserved, so the next specification number is at least 0131.

## Rationale

Closed specifications currently duplicate four different things:

1. current language rules already owned by `docs/reference.md`;
2. implementation facts already expressed by code, tests, names, or CARE
   comments;
3. historical reasoning preserved by Git; and
4. discarded or superseded designs that must not influence future work.

Keeping those files makes an agent choose among multiple plausible authorities.
Deleting them makes the repository's ownership model literal:

- current language meaning: `docs/reference.md`;
- open work and known defects: `docs/status.md` plus its active specification;
- local implementation constraints: code, tests, types, names, and CARE
  comments;
- history: Git.

## Verified baseline

The 2026-08-26 inventory found:

- 48 files under `docs/specs/archive/`;
- 14 active Markdown specifications under `docs/specs/`, none with a terminal
  status;
- `docs/specs/doc.go`, which is package infrastructure rather than a
  specification and remains;
- no RFC, ADR, `docs/specs/`, or archive citation in production Go, generated
  package templates, or workbench source;
- provenance citations to deleted specifications in `docs/reference.md`,
  `docs/status.md`, `AGENTS.md`, and multiple active specifications.

Implementation snapshots the exact terminal-file set before migration rather
than relying on the counts above; concurrent specification work may change the
counts without changing this decision.

## Audit finding already reconciled

RFC 0130 audits archived-spec implementation claims. Its link-graph pass found
that RFC 0127 instructed an edit to a terminal Task-parking spec. RFC 0127 now
owns that native-initialization fact directly and no longer instructs the edit.
The migration must still search every other active spec for the same class: an
instruction targeting a deleted document is worse than a historical citation.

## Knowledge disposition

Every candidate fact from every terminal specification receives exactly one
disposition.

| Fact kind | Destination |
|---|---|
| Current source syntax or grammar | The EBNF at the start of `docs/reference.md` and its one semantic owner. |
| Current language semantic contract | One precise rule in `docs/reference.md`. |
| Current public built-in API or restriction | The relevant API/type section of `docs/reference.md`. |
| Current generated-C or ABI contract visible to consumers | The C23 output contract in `docs/reference.md`. |
| Open defect, missing validation, or unfinished feature | One active specification plus one `docs/status.md` entry. |
| Local implementation invariant not inferable from code | The narrowest owning code location as a CARE comment, preferably replaced by a guard, type, test, or clearer name. |
| Executable invariant whose regression would compile silently | A focused existing test family, with a comment only if the test cannot explain the edge through its name and assertions. |
| Rule already stated by its authoritative owner | No change; record the existing owner during the sweep. |
| Historical rationale, migration steps, measurements, rejected alternatives, or superseded behavior | Git history only. |
| Claim that no longer matches code or `docs/reference.md` | Discard after recording the contradicting evidence. |
| Unresolved current conflict | Stop deletion of that specification and ask for a semantic decision in simple language; after resolution, migrate the result before deletion. |

No terminal specification text is copied wholesale. Examples become general
rules or tests. Historical prose, dates, stage plans, manifest arithmetic,
review dialogue, and implementation coordinates are not migrated.

## Reference rules

The reference sweep is semantic, not textual.

- Read each terminal specification completely.
- Extract only claims that purport to describe current syntax, semantics,
  public APIs, restrictions, diagnostics, generated-C representation, or ABI.
- Verify each claim against both current `docs/reference.md` and the current
  implementation. A contradicting probe defeats the historical claim.
- Add a rule only when it is current, externally meaningful, and absent from
  the reference.
- Update grammar and semantics together when a surviving claim affects syntax.
- Keep every rule in one location; merge with the existing owner instead of
  adding a second explanation.
- Remove RFC/ADR provenance parentheticals from `docs/reference.md`. Preserve
  the rule itself when current; delete historical explanation when it adds no
  contract.
- Do not add tutorials, examples, migration history, implementation details,
  or references to deleted specification numbers.

The current reference contains provenance citations for implemented rules such
as structured block delimiters, collection surface removal, supported-target
assertions, C23 checked arithmetic, successful process return, generated C
naming, program-wide discriminants, and removed File APIs. Their rules remain;
only historical attribution is removed unless the audit finds a missing
contract.

## Code-comment rules

The comment sweep follows CARE without turning source files into a new archive.

- Add a comment only for a current Contract, Architecture, Rationale, or Edge
  fact that the code, type, name, assertion, or test does not already express.
- Language semantics never move into code comments; they belong only in
  `docs/reference.md`.
- Historical origin, authorship, dates, specification numbers, alternatives,
  and migration stories never move into comments.
- Prefer a stronger name, type, guard, assertion, or focused test over a prose
  comment.
- Comments remain self-contained, present-tense, ASCII-only, and adjacent to
  the operation they constrain.
- Do not touch a source file merely to prove that no comment is required.

The verified baseline contains no production citation to remove. This RFC may
still add or improve a CARE comment when the full archive audit identifies a
load-bearing implementation fact with no current owner.

## Active-spec reconciliation

Active specifications must remain usable after their historical dependencies
disappear.

- Preserve references between two active specifications where the dependency
  remains real.
- Replace every dependency on a deleted specification with one of:
  - the exact current rule in `docs/reference.md`;
  - a current implementation invariant, named without historical provenance;
  - an active specification that actually owns unfinished work; or
  - deletion when the reference is merely historical.
- Do not copy a closed specification's prose into an active specification.
- Rewrite ancestry and supersession narratives into present-tense assumptions
  or remove them.
- An active specification must not require a reader to recover a deleted file
  from Git to understand or implement it.

Known active examples include the Arena/Pool RFC's dependency on the already
implemented stateless Heap, target-profile discussions that cite completed
compiler simplifications, and concurrency/ownership drafts that cite completed
IO and scheduler work. These become direct present-tense contracts rather than
historical cross-references.

## Status and repository-policy reconciliation

- Rewrite `docs/status.md` historical citations to terminal specifications as
  self-contained current facts. Keep citations to active owning specs.
- Do not retain completed-work narratives in `docs/status.md`; keep only the
  information needed to understand an open TODO, open bug, or known coverage
  gap.
- Update `AGENTS.md` so terminal specs are knowledge-swept and deleted rather
  than archived, deleted identifiers remain reserved, and active specs cannot
  depend on deleted records.
- Replace `AGENTS.md` examples that cite deleted specifications with general
  current rules. Active-spec citations, such as an active owner of a test
  lifecycle, remain until that spec closes and undergoes the same sweep.
- Remove every instruction to move a completed spec to `archive/` from active
  specs.
- Remove the empty `docs/specs/archive/` directory after its final file is
  deleted.
- Keep `docs/specs/doc.go` and the active flat spec directory.

## Safety and review boundary

- Deletion begins only after the terminal-file inventory and knowledge ledger
  are complete.
- Each file is deleted only after every candidate fact has a disposition and
  every required destination edit has landed.
- Discarded and superseded specifications still receive the same review; a
  rejected feature may contain a current exclusion or rationale that has not
  yet reached its proper owner.
- No language decision is inferred from majority, recency, or repetition among
  historical specs. `docs/reference.md` and verified current behavior win; a
  real conflict is escalated.
- Existing unrelated user changes are preserved. This migration does not
  reformat or normalize unaffected files.
- Generated code, parser/checker behavior, public APIs, diagnostics, and
  manifest hashes do not change merely because documents are deleted.

## Implementation plan

### Phase 0: snapshot and ledger

1. Record the green repository baseline and current working-tree state.
2. Enumerate every file under `docs/specs/archive/` and every terminal-status
   spec elsewhere; save the exact list under `.tmp/` for the duration of the
   migration.
3. Enumerate every reference to those identifiers or paths in active specs,
   `docs/reference.md`, `docs/status.md`, `AGENTS.md`, source, templates, tests,
   workbench files, and repository tooling.
4. Create a temporary ledger with one row per terminal spec and columns for
   current-language claims, implementation claims, open work, conflicts,
   destinations, evidence, and deletion readiness.

### Phase 1: full knowledge audit

1. Read every terminal specification completely in numeric order.
2. Extract candidate facts using the disposition table; do not summarize the
   entire document.
3. Verify current-behavior claims with focused repository inspection or a probe
   under `.tmp/`. Quote the evidence in the temporary ledger.
4. Mark contradicted or superseded claims explicitly so the same claim is not
   rediscovered and migrated later in the pass.
5. A claim that is historically true and currently false reaches Git history
   only. It never reaches `docs/reference.md`. Migrating a stale claim into the
   normative contract is worse than deleting the specification unread, because
   the reference is what every later reader trusts.
6. Consume RFC 0130's Verified sound section rather than re-deriving those
   claims. Two of the three read as defects on first inspection, and the
   evidence for each is recorded there.
7. Look for instructions, not only references. RFC 0127's corrected case is the
   precedent: an active specification cannot tell a reader to edit a terminal
   one after deletion.
8. Stop and ask the user when a conflict would require choosing new language
   behavior rather than documenting existing behavior.

### Phase 2: preserve current authority

1. Apply the minimal `docs/reference.md` changes for verified missing current
   rules, keeping grammar and semantics synchronized and removing historical
   provenance citations.
2. Add or strengthen a CARE comment only for a verified local invariant that
   already holds and otherwise has no current owner. A discovered behavior bug,
   missing guard, refactor, or test requirement receives a new active spec and
   `docs/status.md` row; this deletion migration does not implement it.
3. Create or update an active spec and `docs/status.md` row for verified open
   work that would otherwise disappear.
4. Re-run focused tests for every code or test change made during preservation.

### Phase 3: remove historical coupling

1. Rewrite active specs so no dependency, coordination rule, ancestry note, or
   implementation instruction requires a terminal spec.
2. Rewrite RFC 0125's load-bearing generated-C coverage backlog in particular:
   its terminal-spec citations become present-tense runnable contracts without
   losing any required fixture.
3. Rewrite `docs/status.md` and `AGENTS.md` according to the policy above.
4. Search all non-archive tracked files for every terminal identifier and
   archive path. Classify every remaining match as an error; terminal specs have
   no valid live citation after deletion.
5. Re-read every modified active spec to ensure the replacement is complete and
   does not silently broaden or narrow its design.

### Phase 4: deletion

1. Delete every ledger-approved terminal specification.
2. Verify the deletion set exactly equals the Phase 0 snapshot plus any spec
   that became terminal during the migration; do not delete an active spec.
3. Remove `docs/specs/archive/` after it is empty.
4. Run the full validation/search/test set once while this ADR still exists and
   its ledger can be inspected. Then remove this ADR's `docs/status.md` row,
   mark its ledger complete, and delete this ADR last in the same change.
5. Delete all scratch inventories and probes; `.tmp/` is empty at handoff.

### Phase 5: final conformance

1. After the final deletion, repeat the complete tracked-tree search for
   terminal identifiers, archive paths,
   dangling Markdown links, and instructions to archive completed specs.
2. Verify that every remaining spec has a nonterminal status and appears in the
   correct `docs/status.md` section.
3. Verify that reference rules are provenance-free, cohesive, and contain no
   duplicated rule introduced by the migration.
4. Verify that source comments satisfy CARE and contain no historical
   specification reference.
5. Run `gofmt` only if Go files changed, then `go test ./...`, `go vet ./...`,
   and `go vet -tags c23 ./...`.
6. Run the snippet-manifest test and verify every hash is unchanged.
7. Rebuild and restart the workbench only if code, tests, workbench assets, or
   generated output changed. Documentation-only deletion does not require a
   restart.

## Validation

This section is exhaustive.

- No active specification instructs an edit to a deleted specification. RFC
  0127 states the Windows unreachability of lifecycle-mutex initialization
  failure in its own text.
- Every Phase 0 terminal spec has one complete temporary ledger row and is
  deleted only after all extracted claims receive a verified disposition.
- No file remains under `docs/specs/archive/`; the directory is absent.
- No terminal-status specification remains under `docs/specs/`, including this
  ADR. `docs/specs/doc.go` remains.
- Every remaining specification is active, understandable without Git history,
  and present in exactly one matching `docs/status.md` section.
- No tracked live file references a deleted specification number, deleted spec
  path, `docs/specs/archive/`, or an archive-on-close workflow.

  The check runs against the **deleted** set from the Phase 0 snapshot, never
  against every four-digit number. References between surviving active
  specifications are correct and must not be flagged; a check that cannot tell
  an active number from a deleted one fails on legitimate text and gets
  weakened or ignored, which is worse than not having it.

  Two live-text classes are explicitly preserved rather than swept:

  - `AGENTS.md`'s `//go:build c23`, `compiler/tests/c23validation/`, and
    `go vet -tags c23` material documents RFC 0125's **active** lifecycle. It is
    not terminal-spec provenance and does not name a deleted number.
  - `docs/reference.md`'s C23 output contract -- `<stdckdint.h>`, `nullptr`,
    `typeof`, target qualification -- states live language behavior. Provenance
    removal strips citations, never contracts.
- `docs/reference.md` contains every verified surviving language/ABI contract,
  contains each rule once, and contains no RFC/ADR provenance citation,
  tutorial, migration narrative, or historical example.
- `docs/status.md` contains only open work, open bugs, unowned items, and known
  coverage gaps; none requires a deleted spec to understand its current fact.
- `AGENTS.md` requires knowledge-sweep-plus-deletion for terminal specs,
  permanently reserves deleted numbers, and contains no instruction to retain
  or consult an on-disk closed spec.
- Production comments contain no RFC/ADR/spec provenance. Every comment added
  by this migration satisfies CARE and records a verified current
  implementation fact unavailable from code structure alone.
- No parser, checker, generator, runtime, public API, diagnostic, artifact set,
  or generated-C text changes solely because historical documents were removed.
- Every pre-existing snippet manifest hash is unchanged.
- `.tmp/` is empty.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Expected result

The working tree contains only active specifications. Current language truth is
fully held by `docs/reference.md`; open work is fully held by active specs and
`docs/status.md`; implementation-only facts are expressed locally; and Git is
the sole archive. No historical document remains available to contradict or
mislead agentic development.
