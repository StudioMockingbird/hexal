# RFC 0141: TypeScript Codebase Learnings Review

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; review not started
- Created: 2026-08-28
- Coordinates with: `docs/reference.md`, `docs/status.md`

## Summary

Review the existing TypeScript codebase to extract portable learnings for Hexal — language design, compiler architecture, error handling, generics/monomorphization, module and build tooling, testing strategy, and developer experience — and record which ideas to adopt, adapt, or explicitly reject.

## Goals

- Inventory TypeScript compiler and tooling patterns that succeeded or failed at scale.
- Identify concrete, Hexal-applicable takeaways for:
  - type-system ergonomics and diagnostics
  - incremental compilation and language-server performance
  - module resolution and declaration-file boundaries
  - code generation and source maps
  - testing, fuzzing, and conformance suites
- Produce a short decision log: adopt / adapt / reject per finding, with rationale.

## Non-goals

- No Hexal grammar, type, or code-generation changes in this review itself.
- No wholesale port of TypeScript implementation details.
- No new runtime or syntax without a separate owning spec.

## Review scope

- `tsc` architecture (scanner, parser, binder, checker, emitter, language service)
- Error reporting and diagnostic codes
- Structural vs nominal typing tradeoffs and their UX impact
- Declaration files and API compatibility surfaces
- Build modes: `tsc --build`, incremental program reuse, watch
- Tooling: language server, formatter, linter integrations

## Deliverables

1. Spike report under `.tmp/` summarizing findings with evidence (file/line references, not folklore).
2. Curated list of 5–10 actionable learnings, each with adoption disposition and owning spec or `docs/status.md` follow-up.
3. No changes to `docs/reference.md` or generated artifacts from this spec alone; follow-ups open their own specs.

## Validation

This section is exhaustive.

- TypeScript codebase revision reviewed is pinned (commit/tag) and recorded in the report.
- Report covers each scope bullet with at least one cited observation.
- Every actionable learning has a disposition (adopt/adapt/reject) and, if adopted, a linked follow-up spec or `docs/status.md` entry.
- No language or code-generation change lands under this spec.
- `go test ./...` and `go vet ./...` pass.
