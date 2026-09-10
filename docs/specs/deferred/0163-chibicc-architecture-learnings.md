# RFC 0163: chibicc Architecture Learnings Review

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; review not started
- Created: 2026-09-10
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

## Deliverables

1. Spike report under `.tmp/` summarizing findings with evidence (file/line
   references into the reviewed chibicc revision, not folklore).
2. Curated list of 5–10 actionable learnings, each with adoption disposition
   and owning spec or `docs/status.md` follow-up.
3. No changes to `docs/reference.md` or generated artifacts from this spec
   alone; follow-ups open their own specs.

## Validation

This section is exhaustive.

- chibicc revision reviewed is pinned (commit/tag) and recorded in the report.
- Report covers each scope bullet with at least one cited observation.
- Every actionable learning has a disposition (adopt/adapt/reject) and, if
  adopted, a linked follow-up spec or `docs/status.md` entry.
- No language or code-generation change lands under this spec.
- `go test ./...` and `go vet ./...` pass.
