# RFC 0097: Comment Provenance Cleanup

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-20
- Scope: bring existing Go comments into line with the CARE rule, and add the
  guard that keeps them there
- Depends on: nothing. Touches no behaviour
- Coordinates with: `AGENTS.md` (which already states the rule),
  `docs/status.md`
- Does not change: any code, any test assertion, any generated output

## Summary

`AGENTS.md` states the comment rule already:

> Comments are self-contained, present-tense, and never cite an internal RFC,
> ADR, plan, spec number, spec title, or `docs/specs/` path: closed specs are
> historical records, not the language authority. Comments contain only ASCII
> characters.

The codebase does not follow it, and nothing enforces it, so every
implementation pass adds more. Measured by parsing every `.go` file under
`compiler/` and `workbench/` and inspecting comment text only:

| Violation | Count | Files |
|---|---|---|
| Comment cites a spec number or `docs/specs/` path | 43 | 29 |
| Non-ASCII character in a comment | 54 | 31 |

The non-ASCII set is narrow: 50 em-dashes, and one each of `…`, `é`, `🦀`, `§`.

The C templates under `compiler/generator/packages/` are already clean — zero of
either — so this RFC covers Go source only.

**The cleanup is the smaller half. The guard is the point.** Without a test,
this returns within a few sessions; it already has.

## Why the citations exist, and why they go

A spec number in a comment is a pointer to a document that is, by the time
anyone reads the comment, a historical record superseded by `docs/reference.md`.
It answers "where did this come from", which git already answers, and it does
not answer "what does this guarantee", which is the only question a reader at
that line has.

The rule is not "delete the comment". Most of these sit beside real Rationale:

```go
// are in, and the loop is where that is known (RFC 0074 R11).
// safe; RFC 0088 promises only the literal case.
```

The first states an architectural fact and cites for no reason — drop the
parenthetical. The second attributes a deliberate narrowing to a spec; the
narrowing itself is the Rationale and must survive, restated without the
attribution:

```go
// safe; only the literal case is promised, because the checker's constant
// proof is wider than the generator's view.
```

**Deleting the sentence loses the reason. That is the failure mode to avoid,
and it is why this is a rewrite rather than a regex.**

## The guard

One test, parsing the Go AST and inspecting `*ast.Comment` text:

- fails on any comment matching a spec-citation pattern — `RFC` followed by
  digits, `ADR` followed by digits, or `docs/specs/`;
- fails on any comment containing a rune above `unicode.MaxASCII`;
- reports file, line, and the offending text so the fix is immediate.

It walks `compiler/` and `workbench/`, and it must inspect comments rather than
raw file bytes: the ASCII clause is scoped to comments, and test data containing
`é` or `🦀` is legitimate. A byte-level scan would reject the text-conformance
tests, which exist precisely to carry multi-byte input.

The scanner is the same shape as the existing `context.expected` reader guard in
`compiler/checker/contextual_forms_test.go`, which is the precedent for a test
that reads the tree it belongs to.

## The change

1. Rewrite all 43 citing comments: remove the citation, keep or restate the
   Contract, Architecture, Rationale, or Edge fact it sat beside. A comment left
   with nothing to say after the citation is removed is deleted.
2. Replace all 54 non-ASCII characters in comments with ASCII: em-dash to `--`
   or a restructured clause, `…` to `...`, `§` to `section`, and the two
   characters appearing in prose examples reworded.
3. Add the guard test.

## Invariants

1. No behaviour changes. No test assertion, no generated output, no exported
   name.
2. Every comment that carried a Rationale still carries it. The citation goes;
   the reason stays.
3. The guard fails on a newly added violation of either kind.
4. Test data is untouched. Non-ASCII in string literals stays legitimate.

## Validation

This section is exhaustive.

- The guard reports zero violations across `compiler/` and `workbench/`.
- The guard fires: a comment added with `RFC 0074` fails it, and a comment added
  with an em-dash fails it, each naming file and line.
- The guard does not fire on non-ASCII inside a string literal, verified against
  the text-conformance tests, which contain multi-byte source data.
- `git diff` on the cleanup touches only comment lines. No statement, signature,
  literal, or assertion changes.
- No comment is left as a bare restatement of the line below it; a comment whose
  only content was provenance is gone rather than emptied.
- `go build ./...`, `go vet ./...`, `gofmt -l` silent.
- `go test ./...` passes, and the snippet manifest is byte-identical — this RFC
  changes no generated output.

## Non-goals

- The C templates under `compiler/generator/packages/`, already clean.
- Markdown under `docs/`. Specs are the historical record and cite each other
  deliberately; the rule is about code.
- Rewriting comments that comply with CARE but could be better. Only the two
  measured violation classes are in scope.
- Enforcing the rest of CARE — that every comment carries a Contract,
  Architecture, Rationale, or Edge fact. That is a judgement no test can make,
  and attempting it would turn a mechanical cleanup into a review of every
  comment in the compiler.

## Drawbacks

- A wide, entirely mechanical diff across 38 files, which will conflict with any
  concurrent branch touching the same lines. Best landed when nothing else is in
  flight.
- The guard rejects em-dashes in comments, which are the natural punctuation for
  the parenthetical style used throughout this codebase. `--` is uglier. That is
  the cost of an ASCII rule, and the rule exists so a comment renders identically
  in every editor and terminal the generated C might be read in.
- It removes the only in-code pointer to the reasoning behind some decisions.
  The mitigation is that the reasoning is restated in place rather than deleted;
  where the full argument matters, git blame reaches the commit, which names the
  spec.
