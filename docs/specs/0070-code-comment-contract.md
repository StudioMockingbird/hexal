# RFC 0070: Code Comment Contract and Cleanup

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-16
- Updated: 2026-08-16
- Scope: comments in maintained source, tests, workbench code, embedded test
  fixtures, and generated C/header output
- Coordinates with: `AGENTS.md`, all compiler packages, compiler tests, the
  workbench, and open RFCs 0065, 0068, and 0069

## Summary

Replace historical, procedural, and code-narrating comments with concise
comments that preserve a useful contract, architectural boundary, rationale,
or edge condition.

No code comment may refer to a Hexal RFC, ADR, implementation plan, spec
number, spec title, or `docs/specs/` path. Comments must be self-contained: a
reader should understand the relevant fact without finding a historical
document.

This RFC establishes the **CARE framework** for deciding whether a comment
belongs and what it must say:

- **Contract** — behavior, precondition, postcondition, ownership, or result a
  caller may rely on.
- **Architecture** — which phase or component owns a decision, and where a
  boundary must remain.
- **Rationale** — why a non-obvious implementation exists or why the
  apparently simpler alternative is wrong.
- **Edge** — a safety, lifetime, ordering, portability, representation, or
  failure condition that is easy to violate.

A comment must add at least one CARE fact not already clear from the code,
type, name, or immediately enclosing documentation.

## Motivation

Comments throughout the repository cite the document that introduced a rule:

```go
// RFC 0037: the task control block is compiler-owned.
```

That attribution does not explain the present contract. It also becomes stale
when later work supersedes the cited document. Closed specs are historical
records and are not the language authority, so source comments must not make
them appear normative.

The useful form states the fact and its consequence directly:

```go
// The compiler owns each task control block until join or detach releases it.
```

Other comments narrate the next line, preserve migration history, name an
implementation phase, or use a large paragraph where better naming would be
clearer. These comments increase reading cost without protecting a contract.

A review snapshot found approximately 637 candidate reference sites across 127
files:

| Surface | Candidate sites |
|---|---:|
| `compiler/` non-test source | 474 |
| `compiler/` tests | 159 |
| `workbench/` | 4 |
| **Total** | **approximately 637** |

Nine of the non-test compiler sites are Go string literals emitted as generated
C comments; they are included in the 474 and are not additional sites. The
snapshot is a scale estimate, not a frozen acceptance count: open compiler work
may add, remove, or move sites before a batch begins. Each batch records its
exact starting inventory.

The cleanup must cover all of these surfaces; changing Go comments alone would
leave the same history embedded in compiler output. The scale also makes one
repository-wide rewrite unreviewable, so implementation is explicitly batched.

## Goals

1. Remove every internal RFC, ADR, plan, and spec reference from code comments.
2. Preserve every non-obvious rule currently carried only by a comment.
3. Make retained comments concise, accurate, local, and useful without an
   external document.
4. Establish one repeatable framework for future comments.
5. Improve generated C comments to the same standard as compiler comments.
6. Change no language behavior, diagnostics, API, or generated executable
   behavior.
7. Apply the useful parts of literate programming: reader-first structure,
   progressive disclosure, and reasoning beside the code it protects.

## Non-goals

- Rewriting historical specs or removing their cross-references.
- Moving language semantics out of `docs/reference.md` and into source.
- Adding comments to every declaration, branch, or test.
- Enforcing a comment count, density, or minimum length.
- Adding a custom linter, analyzer, package, or CI subsystem in this pass.
- Adding literate-programming preprocessors, tangling/weaving tools, generated
  source assembly, or another authoring format.
- Turning source files into essays, tutorials, or copies of
  `docs/reference.md`.
- Refactoring working code solely to make a comment unnecessary, except for a
  trivial local rename that is safer and clearer than retaining the comment.
- Changing user-visible generated C other than its comments and any fixture or
  hash that records those comments.

## Scope

The no-internal-spec-reference rule applies to:

- Go line comments, block comments, package comments, and declaration docs;
- HTML, CSS, and JavaScript comments in the workbench;
- comments inside embedded Hexal or C fixtures maintained by the repository;
- Go string literals whose contents are emitted as comments in C or headers;
- generated C/header comments and expected generated-output fixtures; and
- test comments, including file headers and regression explanations.

It does not apply to:

- Markdown prose in `docs/specs/`, which must identify its dependencies and
  history;
- the spec links required by `docs/status.md`;
- references in `docs/reference.md` or `AGENTS.md` where those documents must
  describe documentation governance; or
- stable external technical authorities, such as a C23 clause, Unicode rule,
  or platform ABI, when the exact external contract materially explains the
  implementation.

External references supplement a self-contained explanation; they do not
replace it.

## Comment decision ladder

For each existing or proposed comment, apply these steps in order:

1. **Make the code clear.** Prefer an accurate name when a trivial local rename
   removes the need for explanation. If clarity requires changing control flow,
   introducing declarations, or restructuring working code, retain the useful
   comment and leave that refactor to separate work.
2. **Delete narration.** Remove a comment that merely restates the next line,
   repeats a type, labels a plainly named case, or describes syntax visible in
   the code.
3. **Identify the CARE fact.** If no Contract, Architecture, Rationale, or Edge
   remains, delete the comment.
4. **Place it narrowly.** Put the fact on the declaration, block, or operation
   whose future change could violate it.
5. **Write the present rule.** State what is true now and, when useful, why it
   matters or what breaks if it changes.
6. **Check durability.** Remove document numbers, dates, task phases, line
   numbers, temporary counts, and other coordinates likely to become stale.

Do not mechanically delete a whole comment because it contains an RFC
reference. First preserve any CARE fact it carries.

## Literate structure

Hexal adopts literate-programming principles only where they make ordinary Go,
workbench code, or generated C easier to read. Source remains directly
compiled, idiomatic code; prose never becomes an alternate implementation.

### Narrative spine

A complex file or subsystem should expose a short narrative spine at its
natural entrypoint. The spine states only what a maintainer needs before
reading details:

- the responsibility owned here;
- the important input and output;
- the major stages in execution order; and
- the invariants that cross those stages.

Do not add a narrative spine when the public entrypoint and helper names
already communicate the flow. Do not repeat declaration-level contracts in the
overview.

Example shape:

```go
// Generation validates the checked program, discovers program-wide support,
// emits dependency modules, and emits the root entrypoint. Discovery must
// finish before emission so every file observes the same support set.
```

### Reader-first ordering

Within a cohesive file, prefer this reading order where Go dependencies and a
safe edit permit it:

1. public or architectural entrypoint;
2. major phases in execution order;
3. phase-owned helpers; and
4. low-level formatting, predicates, and data helpers.

Conceptual ownership takes precedence over alphabetical or historical order.
Do not perform broad declaration moves merely to satisfy this RFC. During the
comment cleanup, reorder declarations only when the move is mechanical,
behavior-neutral, and materially clarifies the flow; otherwise record the
flow in the narrative spine and leave the code stable.

Section comments are not decorative banners. Use one only when a reader could
not infer a meaningful group from its declarations and placement.

### Proof-adjacent comments

Place safety reasoning beside the operation that depends on it. Overflow,
ownership, lifetime, aliasing, evaluation order, target representation, and
fail-closed guarantees must not be explained only in a distant file overview.

The comment states the invariant and consequence; the code immediately below
must visibly enforce it:

```go
// Capture the operand once so cleanup cannot evaluate an effectful expression
// a second time.
```

If several operations share one proof, place it on the smallest function or
type that owns all of them rather than duplicating it at every operation.

### Progressive disclosure

Comments reveal detail in this order:

1. subsystem responsibility and flow at the architectural entrypoint;
2. caller-visible contracts on declarations; and
3. local rationale or edge conditions beside surprising code.

Each level adds information. It must not paraphrase the level above it.

Tests provide the executable form of a contract: names and table cases state
the rule, assertions prove it, and comments explain only non-obvious edges.
Generated C preserves recognizable source structure and comments only the
runtime, ABI, ownership, or representation details a C reader cannot derive.

## Comment forms

### Declaration comments

Use a declaration comment when callers or maintainers need a non-obvious
contract. For exported Go declarations, retain idiomatic Go documentation that
starts with the declared name.

Good:

```go
// Compile checks every module reachable from entrypoint and returns no files
// when any source diagnostic is present.
```

Unhelpful:

```go
// Compile compiles the program.
```

Private declarations do not require comments when their name, type, and body
already establish their role.

### Boundary comments

Use a file-, type-, or function-level comment to record architectural
ownership that is not evident locally.

```go
// Import resolution is lexical over the supplied source map. This package
// never consults the host filesystem.
```

Do not preserve refactor history such as which file previously owned a helper
or which implementation item moved it.

### Inline comments

Use an inline comment immediately before a surprising operation, guard, or
branch. Explain the protected invariant or consequence, not the mechanics.

Good:

```go
// Check zero before align-1 so unsigned underflow cannot bypass validation.
```

Unhelpful:

```go
// Check whether align is zero.
```

Trailing comments are reserved for short unit, representation, or field facts
that cannot be expressed in the name. Prefer a preceding comment for rationale.

### Test comments

Test names, table-case names, and assertions should express behavior. Add a
comment only when the reason for a case or its expected invariant is not
obvious from those elements.

Replace spec-attribution headers with a facet-level contract, or delete them
when the file and test names are sufficient.

Good:

```go
// Failed compilation returns an allocated empty file map, never a nil map.
```

Unhelpful:

```go
// Regression test for RFC 0060 rule 3.
```

No test name, case name, or comment may encode an internal spec number merely
to preserve provenance. Git and the spec archive own provenance.

### Generated C comments

Generated comments are part of the maintained C output and follow the same
CARE test. Retain them only when they help a C reader identify:

- a generated section whose boundary is otherwise unclear;
- ABI, ownership, lifetime, or initialization requirements;
- why a non-obvious standard facility or lowering is required; or
- the source relationship not already conveyed by a symbol or `#line`.

Section labels must describe their contents:

```c
/* Task handle declarations. */
```

They must not describe provenance:

```c
/* RFC 0037 handle typedefs. */
```

Delete generated comments that only repeat a plainly named declaration. Do not
replace every historical label with another label by default.

## Writing rules

- Use present tense and active voice.
- Lead with the invariant or purpose; add the reason only when it is not
  evident.
- Prefer one or two short sentences. Longer explanations belong at the
  narrowest declaration that owns the whole contract, not repeated at each
  call site.
- Use exact project terms and identifiers. Avoid vague subjects such as “it,”
  “this logic,” “the above,” or “the spec.”
- State ownership and lifetime endpoints explicitly.
- State evaluation-order, overflow, nil, trap, and failure behavior exactly
  when the code depends on them.
- Distinguish source-language rules from generated-C implementation facts.
- Describe why a standard C facility does not fit when compiler-owned generated
  machinery is necessary, as required by `AGENTS.md`.
- Keep comments synchronized in the same change as the code they explain.
- Prefer one authoritative comment over repeated paraphrases.
- Give complex subsystems a narrative spine when their execution flow is not
  evident from their entrypoint and helper names.
- Keep proof comments adjacent to the guard, ownership transition, or lowering
  whose correctness depends on them.
- Organize new code in reader-first conceptual order. Treat broad reordering of
  existing declarations as a separate refactor when it is not mechanically
  safe.

## Prohibited comment content

Comments must not contain:

- `RFC`, `ADR`, or an internal spec/plan identifier or title;
- a `docs/specs/` link or direction to consult an internal design document;
- implementation chronology: “added by,” “after migration,” “phase 2,” “item
  5,” “old path,” or equivalent history;
- source line numbers or temporary inventory counts;
- commented-out code;
- tutorials or walkthroughs;
- claims contradicted by the current code or `docs/reference.md`;
- task tracking that belongs in `docs/status.md`; or
- restatements of an immediately visible operation.

The words “specification” or “standard” remain valid when referring to a stable
external authority such as ISO C or Unicode. Generic identifiers containing
`spec`, such as an internal Go variable named `conversionSpec`, are unaffected.

## Coordination with open work

The policy change and cleanup have different sequencing requirements:

- Update `AGENTS.md` first. From that point, every new or modified comment must
  follow this RFC even while the cleanup remains incomplete.
- RFCs 0065, 0068, and 0069 were open when this RFC was written and touch
  generator code. Their implementations must adopt the comment contract for
  every comment they add or modify; they must not introduce new historical
  attribution.
- Do not execute a comment-cleanup batch concurrently with a behavior-changing
  implementation in the same file. Finish or pause the behavior change, review
  its resulting comments, and only then clean that file.
- This RFC need not wait for every open feature to close. Files untouched by
  active work may be cleaned after the policy batch; future work must preserve
  the cleaned state.
- Regenerate generated-C expectations only after the behavior-changing work
  affecting those outputs is stable. Keep comment-only hash changes
  attributable to the generated-comment batch.

This rule generalizes to any spec opened after this RFC: the named RFC list is
the creation-time collision inventory, not an exemption for later work.

## Implementation batches

Do not combine the entire cleanup into one change or one generated-C baseline
update. Each batch is a separate review unit with its own inventory, diff
review, formatting, tests, and mechanical reference scan.

Execute in this order:

1. **Repository policy.** Update `AGENTS.md` before changing any source
   comments. This prevents new work from correctly following the old,
   conflicting test-comment instruction.
2. **Compiler packages outside the generator.** Clean the root `compiler`
   package and then `lexer`, `parser`, `checker`, and `types`, one package per
   review unit. Unit-test comments may travel with their owning package.
3. **Generator source comments.** Split `compiler/generator` by coherent
   responsibility family; do not combine all generator files merely because
   they share one Go package. Keep core emission/rendering, numeric lowering,
   allocation/collection/runtime families, and concurrency/control-flow
   families reviewable independently. Record the exact files in each batch
   before editing.
4. **Full-pipeline and dormant tests.** Clean
   `compiler/tests/integration` by related language facets, then clean
   `compiler/tests/c23validation`. Preserve the dormant canaries' lifecycle.
5. **Generated C/header comments.** Rewrite or delete generated-comment string
   literals as their own batch, then regenerate all affected exact-output
   fixtures and hashes together. Do not mix unrelated generator behavior into
   this baseline change.
6. **Workbench.** Clean workbench source and snippet comments, regenerate
   affected expectations through the compiler, rebuild, and restart the
   workbench.
7. **Final repository audit.** Re-run the complete negative scan and semantic
   review after all batches.

If a listed package has no candidate or needs only a trivial deletion, it may
share a review unit with the immediately adjacent small package. Generator and
generated-output batches remain separate regardless of size because their
review risks differ.

The repository-policy batch updates `AGENTS.md` to:

- adopt the CARE framework and decision ladder;
- adopt narrative spines, reader-first ordering, proof-adjacent comments, and
  progressive disclosure as the permitted literate-programming practices;
- prohibit internal RFC, ADR, plan, and spec references in all code comments
  and generated comments;
- require comments to be self-contained and present-tense;
- replace the current testing instruction to “cite the spec in a header
  comment” with an instruction to state the behavior or edge condition without
  provenance; and
- retain the rule that complex code explains both what it protects and why.

## Per-batch procedure

### 1. Record the exact inventory

Inventory comments and generated-comment literals under `compiler/` and
`workbench/`. Search at minimum for:

```text
RFC
ADR
docs/specs/
spec <four digits>
plan <four digits>
```

The search is a candidate inventory, not an instruction to alter identifiers,
diagnostics, ordinary string data, or documentation prose that is not a code
comment. Record the files and candidate count for the current batch; do not use
the approximate repository snapshot as its acceptance count.

### 2. Classify every candidate

Assign each candidate exactly one action:

- **Delete** — attribution, narration, history, or duplicate information only.
- **Rewrite** — contains a durable CARE fact mixed with prohibited content.
- **Relocate** — durable fact is repeated or placed below the declaration that
  owns it.
- **Keep unchanged** — false positive outside comment scope.

Review complete comment blocks, not only matched lines, so a rewrite remains
grammatical and does not leave a detached continuation.

### 3. Review adjacent comments

For each touched file, inspect adjacent comments for stale narration,
duplication, inaccurate wording, and missing CARE facts. This is a bounded
comment-quality pass, not authorization to restructure the file.

For a complex touched file, also verify that a reader can discover its
responsibility, execution flow, and cross-stage invariants from the entrypoint
and nearby comments. Add one concise narrative spine only when that information
is otherwise missing. Move a safety proof beside its mechanism when the
current placement makes the relationship unclear.

### 4. Regenerate affected expectations

When an emitted C/header comment changes, update exact-output tests, fixture
hashes, and workbench expected output through the normal compiler generation
path. Do not hand-edit generated expectations to conceal a compiler mismatch.

## Validation

Validation separates the mechanically detectable prohibition from the
semantic quality review.

### Mechanical negative gate

After each batch, scan its touched source for at least:

```text
\bRFC\b
\bADR\b
docs/specs/
\b(spec|plan)[ _:#-]*[0-9]{4}\b
```

After the final batch, run the same scan across all of `compiler/` and
`workbench/`. Every match must be classified. No match may remain in an
in-scope comment, embedded fixture comment, or generated-comment literal.

This is a mechanical absence gate. It does not prove comment quality and does
not replace comment-aware inspection.

### Semantic preservation gate

Review the complete diff for every batch. For each deleted or rewritten
candidate, verify that any non-obvious CARE fact was retained locally or was
already stated at the smallest authoritative declaration. Do not use sampling:
the per-batch diff is the review record.

Then perform the following validation:

1. Perform a comment-aware audit of Go source under `compiler/` and
   `workbench/`; no in-scope comment contains an internal document reference.
2. Inspect HTML, CSS, JavaScript, embedded fixtures, and generated-comment
   string literals separately; Go parsing alone cannot prove those surfaces.
3. Compile representative programs that exercise generated section comments
   and verify their C/header output contains no internal document reference.
4. Confirm every removed attribution that carried a non-obvious rule was
   rewritten as a self-contained CARE fact or was already authoritative at a
   narrower location.
5. Inspect each complex touched subsystem for a discoverable narrative spine,
   reader-first flow, and safety proofs adjacent to their mechanisms. Do not
   require broad declaration movement to pass this check.
6. Confirm no production behavior, diagnostics, exported Go API, Hexal syntax,
   or C executable semantics changed.
7. Run `gofmt` on touched Go files.
8. Run `go test ./...`.
9. Run `go vet ./...`.
10. Run `go vet -tags c23 ./compiler/tests/c23validation` to ensure dormant
   generated-C canaries still type-check.
11. Rebuild and restart the workbench because its source and generated snippet
    expectations are in scope.

A plain text scan proves only the targeted negative patterns. It can match
identifiers and documentation strings while missing constructed generated
comments; the classification, comment-aware inspection, representative
generation, and semantic diff review close those gaps. Do not add permanent
lint infrastructure unless later regressions show that repository guidance
and review are insufficient.

## Documentation synchronization

- Update `AGENTS.md` as specified above.
- Update `docs/status.md` while this RFC is open and remove its entry when
  closed.
- Review `docs/reference.md` during implementation and explicitly record that
  no edit is required. This RFC changes documentation quality and generated C
  comments, not language syntax or semantics.
- Do not add comment guidance to `docs/reference.md`; it is the language
  contract, not a contributor guide.

## Acceptance criteria

- No in-scope code or generated-output comment refers to an internal RFC, ADR,
  plan, spec identifier, spec title, or spec path.
- The cleanup is delivered in the defined review units, with the generated-C
  comment and fixture baseline isolated from unrelated behavior changes.
- `AGENTS.md` is updated before source cleanup begins, and all overlapping
  feature work follows the new policy immediately.
- `AGENTS.md` defines and requires the CARE comment framework.
- `AGENTS.md` also requires narrative spines where needed, reader-first
  ordering for new code, proof-adjacent safety comments, and progressive
  disclosure without literate-programming tooling.
- The conflicting instruction to cite specs in test header comments is gone.
- Each retained or rewritten non-documentation comment contributes a Contract,
  Architecture boundary, Rationale, or Edge fact.
- Comments are self-contained, present-tense, narrowly placed, and consistent
  with the code and `docs/reference.md`.
- Pure narration, migration history, duplicated explanations, and stale
  coordinates are removed.
- Complex touched subsystems expose their responsibility and flow without
  source-file essays, repeated contracts, or mandatory broad reordering.
- Safety reasoning is adjacent to the mechanism it protects or placed on the
  smallest declaration that owns the complete proof.
- Generated C/header comments describe present output rather than its design
  history.
- No custom comment-lint package or framework is added.
- Generated-output fixtures are regenerated where emitted comments changed.
- All validation commands pass.
- `docs/reference.md` is verified unchanged unless an independent semantic
  mismatch is discovered and separately authorized.

## Implementation readiness

Implementation-ready. The scope, framework, migration decisions, generated-C
handling, execution batches, in-flight-work coordination, repository-guidance
change, exclusions, and separate mechanical and semantic validation gates are
defined. No language or architecture decision remains open.
