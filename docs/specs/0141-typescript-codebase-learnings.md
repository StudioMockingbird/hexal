# RFC 0141: TypeScript Codebase Learnings Review

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed, 2026-09-24. Every Validation item passes. The review examined
  the current TypeScript Go main branch at
  `df1a31e6d5c4aa4485f276fdfb4218a7bfcdf348`, TypeScript 6.0.3 at
  `050880ce59e30b356b686bd3144efe24f875ebc8` and TypeScript Go 7.0.2 at
  `2bd066d87f5bafd315be9f40889d0a60b9e58e0b`. Its ten dispositions are recorded
  durably in RFC 0241; adapted future work is owned by deferred RFCs 0232 and
  0242, RFC 0243 (diagnostic identity), and RFC 0244 (measured policy and
  export fingerprints). No Hexal language, compiler, generated-C, or reference
  change landed under this RFC. Both gates are clean: `go vet ./...` passes, and
  `go test ./...` passes — the two failures this status previously recorded as
  blocking were unrelated and are resolved, the `cmd/hexal` target-qualification
  tests by the Windows host-gate work and the workbench loopback test by the
  port no longer being held
- Created: 2026-08-28
- Updated: 2026-09-24
- Findings recorded by: RFC 0241
- Coordinates with: RFC 0232, RFC 0241, RFC 0242, `docs/reference.md`,
  `docs/status.md`

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

## Deliverable note

RFC 0241 is the durable review report. The temporary `.tmp/` spike was deleted
after its evidence and dispositions were transferred there, as the repository
requires scratch state to be empty between tasks.

The research work is complete. The RFC can move to the archive unchanged once
the ordinary suite is green in the settled worktree; its exhaustive Validation
section does not permit calling the current failed run a pass merely because
the failures are unrelated.
