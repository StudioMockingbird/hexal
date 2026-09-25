# RFC 0244: Measured Compiler Policy

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-09-24
- Updated: 2026-09-25
- Origin: RFC 0241 finding T2; the unrelated fingerprint design moved to
  deferred RFC 0246
- Coordinates with: RFC 0241, deferred RFC 0246, and `AGENTS.md`
- Does not change: Hexal syntax or semantics, compiler policy values,
  generated C, the public compiler API, or `docs/reference.md`

## Summary

Keep the evidence for empirically tuned compiler policy beside the policy it
justifies, where a maintainer will see it after the originating spec is
archived. Correct unsupported performance claims in current policy comments.
This RFC does not tune any value or add measurement machinery.

## Goals

- Make measured choices reproducible and identify when to remeasure them.
- Distinguish an empirical optimum from a contract, platform fact, proof-based
  safety bound, or deliberately conservative resource ceiling.
- Remove unsupported performance rationale rather than invent a benchmark to
  defend an existing value.

## Non-goals

- Changing any policy value or creating a benchmark suite.
- Requiring numbers in comments for non-empirical decisions.
- Fingerprints, incremental compilation, or caching; deferred RFC 0246 owns
  the fingerprint proposal.
- Rewriting unrelated comments throughout the compiler.

## Rule

When a measurement selects a non-obvious threshold, ordering, capacity, or
heuristic, its adjacent CARE rationale records:

1. the workload or corpus measured;
2. the alternatives compared;
3. the figures supporting the selected policy; and
4. the condition that requires remeasurement.

Do not label a safety bound, ABI constant, platform constraint, or conservative
ceiling as a measured optimum. Such a value needs an honest Contract,
Architecture, Rationale, or Edge comment only when the name and type do not
already convey the reason. The four-part rule applies only when empirical
comparison actually selected a value.

Add this rule to `AGENTS.md` under its CARE or Simplify guidance so future
agents apply it beyond this one audit. Comments must still satisfy CARE:
present-tense, self-contained, ASCII-only, and no internal spec citations.

## Current-policy audit

Audit every declaration in `compiler/config/config.go`, the compiler-owned
home for policy shared by multiple phases. Keep each value unchanged:

| Policy | Classification | Required comment outcome |
| --- | --- | --- |
| `RuntimeABIVersion` | generated/runtime contract | Keep the ABI compatibility rule |
| `PageSizeBytes` | platform/layout requirement | Keep the guard-page rationale |
| `MaxSyntaxDepth` | recursive-stack safety bound | State the headroom rationale without claiming benchmark tuning |
| `MaxInterpolationDepth` | lexer/parser consistency bound | Keep its relationship to syntax depth explicit |
| `ForeignInspectionByteLimit` | conservative hostile-output ceiling | State that it is a resource limit, not a measured optimum |
| `ForeignInspectionTimeout` | conservative external-process ceiling | State that it is a resource limit and identify its process boundary |
| task stack reserve and commit | public runtime policy plus platform constraint | Keep the contract and backend distinction |
| `MaxInlineStringCapacity` | language/runtime storage ceiling | Remove the unsupported claim that one page is the cost crossover; state the bounded-value and stack-layout policy actually enforced |
| Error capacities | public runtime representation | Keep the representation contract |

Do not fabricate measurements. If a future change selects a different value
for performance, measure that change and record its evidence then.

## Required sweep

- Review every declaration and adjacent rationale in
  `compiler/config/config.go` against the table.
- Review the surrounding `AGENTS.md` CARE and Simplify rules so the new rule
  neither duplicates nor contradicts them.
- Search the edited comments for unsupported empirical claims or stale source
  coordinates. Do not expand this into a repository-wide comment rewrite.

## Validation

This section is exhaustive.

- `AGENTS.md` states the four-part evidence rule and distinguishes empirical
  tuning from contracts, platform facts, proofs, and conservative ceilings.
- Every declaration in `compiler/config/config.go` retains an accurate
  classification rationale; no comment invents benchmark evidence.
- `MaxInlineStringCapacity` no longer claims that one page is an empirically
  proven cost crossover.
- No policy value, public compiler API, or generated artifact changes.
- The existing snippet manifest is byte-identical.
- `go test ./...`, `go vet ./...`, `gofmt -l`, and `git diff --check` pass.
- `docs/reference.md` is reviewed and confirmed unchanged because this RFC
  changes implementation rationale, not language or generated-C behavior.

## Implementation plan

### Phase 1: establish the documentation rule

1. Add the four-part empirical-policy rule to `AGENTS.md` beside CARE or
   Simplify; retain its existing comment constraints.
2. Confirm that the rule does not require measured figures for non-empirical
   values.

### Phase 2: audit current policy

1. Classify every declaration in `compiler/config/config.go` using the table.
2. Correct unsupported or ambiguous comments, especially the claimed cost
   crossover for `MaxInlineStringCapacity`.
3. Preserve every declaration's value, type, and name. Add no benchmark result
   unless an actual recorded measurement supports it.

### Phase 3: verify and close

1. Run the exhaustive Validation gate and compare the snippet manifest.
2. Confirm no reference update is needed, then close this RFC and remove its
   `docs/status.md` row only after the gate passes.

## Implementation readiness

Implementation-ready. The rule, finite audit scope, prohibited changes, and
validation gate are explicit. The future fingerprint has no role in this work.
