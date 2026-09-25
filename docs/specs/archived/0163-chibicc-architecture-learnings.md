# RFC 0163: chibicc Architecture Learnings Review

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed, 2026-09-24. The review was carried out against pinned chibicc
  revision `90d1f7f199cc55b13c7fdb5839d1409806633fdb` (branch `main`,
  2020-12-07) by two independent passes, which agreed on every disposition they
  shared. Every Validation item below passes. The findings are recorded in
  **RFC 0239**, which supersedes the `.tmp/` spike reports this RFC asked for:
  `.tmp/` is cleared between tasks, so a numbered spec is where a durable record
  belongs. Twelve findings, one actionable — F1, the generator's scope
  invariant, owned by RFC 0239 itself. No language, grammar, or code-generation
  change landed under this RFC, as its Non-goals required
- Created: 2026-09-10
- Updated: 2026-09-24
- Superseded by: RFC 0239 (chibicc Review Findings) for every finding and
  disposition. This RFC is the request; RFC 0239 is the answer
- Coordinates with: RFC 0039 (C interoperability — compiler core), RFC 0052
  (C compiler backend), `docs/reference.md`, `docs/status.md`

## Summary

Review chibicc (Rui Ueyama's small, self-hosting C11 compiler) to extract
portable learnings for Hexal — preprocessor/lexer design, C type layout and
conversion rules, hand-written recursive-descent parsing of C's declarator
grammar, and low-level code-generation decisions — and record which ideas to
adopt, adapt, or explicitly reject. Same shape of exercise as RFC 0141's
TypeScript review, aimed at a different, much smaller and lower-level
codebase whose relevance to Hexal is different in kind: less about
tooling/DX at scale, more about C semantics and compiler internals directly
adjacent to Hexal's own C-interop and C-emission work.

## Goals

- Inventory chibicc's implementation of the C semantics Hexal must itself
  get right for C interoperability and C23 emission:
  - preprocessor: macro expansion, token pasting/stringizing, conditional
    compilation, include handling
  - type representation: struct/union/array layout, alignment and size
    computation, C's implicit conversion and usual-arithmetic-conversion
    rules
  - the declarator grammar ("declaration follows use") and how a
    hand-written recursive-descent parser handles it cleanly
- Compare chibicc's direct-to-assembly code-generation choices against
  Hexal's higher-level C-emission target, specifically for constructs both
  must lower correctly: structs passed/returned by value, varargs, bitfields,
  array-to-pointer decay.
- Note chibicc's incremental, always-working, commit-by-commit development
  history as a process reference point, separate from any architectural
  takeaway.
- Produce a short decision log: adopt / adapt / reject per finding, with
  rationale.

## Non-goals

- No Hexal grammar, type, or code-generation changes in this review itself.
- No wholesale port of chibicc implementation details.
- No new runtime or syntax without a separate owning spec.
- Not a self-hosting goal for Hexal — chibicc's self-hosting milestone is
  noted only as a fact about the reviewed codebase, not adopted as a target.

## Review scope

- Preprocessor and lexer (macro expansion, conditional compilation,
  token-paste) — directly adjacent to RFC 0039's header-parsing needs.
- Type layout: struct/union/array size and alignment computation, bitfields,
  implicit conversions — adjacent to Hexal's own layout intrinsics and
  RFC 0039's C-type mapping.
- Parser structure: single-pass recursive-descent handling of C's declarator
  grammar, comparable in spirit to Hexal's own hand-written parser.
- Code-generation strategy: direct x86-64 emission with no real IR, and how
  it lowers by-value structs, varargs, and bitfields — a contrast case
  against Hexal's C-as-target emission, not a model to copy wholesale.
- Test suite design: chibicc's self-contained C test suite driving its own
  incremental development, as a comparison point for Hexal's own
  snippet-catalog/C23-validation approach.
- Diagnostic reporting: source-span-accurate error messages.
- Overall architecture, information flow. How create a compiler which is simple and elegant and maintanable.

## Deliverables

1. Spike report under `.tmp/` summarizing findings with evidence (file/line
   references into the reviewed chibicc revision, not folklore).
2. Curated list of 5–10 actionable learnings, each with adoption disposition
   and owning spec or `docs/status.md` follow-up.
3. No changes to `docs/reference.md` or generated artifacts from this spec
   alone; follow-ups open their own specs.

## Validation

This section is exhaustive. Each item's outcome is recorded beside it, verified
on 2026-09-24.

- chibicc revision reviewed is pinned (commit/tag) and recorded in the report.
  **Pass:** `90d1f7f199cc55b13c7fdb5839d1409806633fdb`, branch `main`,
  2020-12-07, recorded in RFC 0239's header and reproducible by cloning.
- Report covers each scope bullet with at least one cited observation.
  **Pass:** all seven bullets are covered in RFC 0239 — preprocessor and lexer
  (`chibicc.h:74-92`, `preprocess.c:15-23,647-672`), type layout and bitfields
  (`parse.c:2686-2733`), parser structure and declarators
  (`parse.c:114-131,681-705`), code generation
  (`codegen.c:7,31-39,445,456,1568`), test-suite design (`test/arith.c:5`,
  `test/common:5-12`, `test/driver.sh`), diagnostics (`tokenize.c:28-68`), and
  overall architecture.
- Every actionable learning has a disposition (adopt/adapt/reject) and, if
  adopted, a linked follow-up spec or `docs/status.md` entry.
  **Pass:** twelve findings, each with a disposition. The single ADOPT — F1,
  the generator's scope invariant — is owned by RFC 0239, which carries its
  Validation and implementation plan. F2 names deferred RFC 0191, F7 names
  `docs/status.md` and deferred RFC 0209.
- No language or code-generation change lands under this spec.
  **Pass:** nothing outside `docs/specs/` changed. F1 is specified, not
  implemented.
- `go test ./...` and `go vet ./...` pass. **Pass**, both clean on the tree at
  closure.

### Deliverable note

Deliverable 1 asked for the spike report under `.tmp/`. Two were produced there
by independent passes and both were consumed into RFC 0239. `.tmp/` is scratch
and is cleared between tasks, so the durable record is the numbered spec rather
than the scratch file — the deliverable is met in substance and relocated on
purpose.

### One item deliberately not opened

F7 (linking reference-compiler-built objects into the test suite, as
chibicc's `test/common` does) is dispositioned ADAPT with an open question, not
accepted. No `docs/status.md` entry was added for it, because an entry there
needs a spec to point at and this has none yet. The question is recorded in RFC
0239 instead, which is where it can be answered.
