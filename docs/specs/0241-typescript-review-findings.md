# RFC 0241: TypeScript Review Findings

- Kind: Feature Specification (Rust-Style RFC)
- Status: Research complete and closed, 2026-09-24. The repository gates that
  previously blocked RFC 0141's closure are clean, so nothing blocks this
  record either. This RFC authorizes no implementation; its actionable findings
  are owned by RFC 0243 (T1) and RFC 0244 (T2, T4)
- Created: 2026-09-24
- Origin: durable deliverable for RFC 0141. It consolidates three independent
  reviews; scratch research belongs in `.tmp/`, so evidence that must survive
  lives here
- Reviewed revisions:
  - TypeScript monorepo `main` at
    `df1a31e6d5c4aa4485f276fdfb4218a7bfcdf348`
  - TypeScript 6.0.3 at
    `050880ce59e30b356b686bd3144efe24f875ebc8`
  - TypeScript Go 7.0.2 at
    `2bd066d87f5bafd315be9f40889d0a60b9e58e0b`
- Coordinates with: RFC 0141, deferred RFC 0232 (parallel and incremental
  compilation), deferred RFC 0242 (language tooling architecture), RFC 0244
  (measured policy and export-interface fingerprints),
  `docs/reference.md`
- Does not update `docs/reference.md`: no syntax, semantics, signature,
  diagnostic, or generated-C contract changes

## Summary

The current TypeScript repository contains both a mature classic compiler and
the Go-native TypeScript 7 implementation. That makes it unusually useful to
Hexal: it shows which concepts survived a language rewrite and exposes their
cost in the same implementation language Hexal uses.

The Go implementation measured about 316,726 non-test lines at the pinned
revision. Its language-service and LSP packages alone measured about 63,754
lines, roughly one fifth of that total. The lesson is not to imitate its size.
Hexal should retain its small, forward-only compiler and adopt only mechanisms
that solve a measured problem.

The strongest future lessons are:

1. invalidate dependents from exported semantic shape, not every source edit;
2. publish immutable project snapshots above the core compiler boundary;
3. give diagnostics machine identity before building long-lived tooling;
4. preserve source-location fidelity explicitly; and
5. make measurement, cancellation, and deterministic testing part of any
   future incremental or editor architecture from the beginning.

## Evidence map

### Classic compiler

Representative entrypoints in TypeScript 6.0.3:

- scanner: `src/compiler/scanner.ts:1022`
- parser: `src/compiler/parser.ts:1344`
- binder: `src/compiler/binder.ts:502`
- checker: `src/compiler/checker.ts:1486`
- emitter: `src/compiler/emitter.ts:752`
- builder API: `src/compiler/builderPublic.ts:159-218`
- language service: `src/services/services.ts:1627`
- formatter: `src/services/formatting/`

Builder state records source versions separately from exported declaration
signatures (`src/compiler/builderState.ts:62-109`), hashes declaration shape
(`:399-459`), and walks reverse references to find affected dependents
(`:462-511`). Module-resolution caches have explicit keys and separate cache
owners (`src/compiler/moduleNameResolver.ts:867-880`, `:1080-1109`,
`:1276-1324`).

### Go-native compiler and service

Representative entrypoints in the pinned Go revisions:

- pull scanner and parser rewind: `internal/parser/parser.go:341-382`,
  `internal/scanner/scanner.go:292-298`
- parser: `internal/parser/parser.go:137`
- binder: `internal/binder/binder.go:96`
- checker: `internal/checker/checker.go:902`
- emitted transform pipeline: `internal/transformers/chain.go:15-61`
- fail-closed printer dispatch: `internal/printer/printer.go:5264-5274`
- immutable project snapshots: `internal/project/snapshot.go:39-60`,
  `:397-431`
- dirty copy-on-write collections:
  `internal/project/dirty/map.go:52-55`
- ref-counted caches and lifetimes:
  `internal/project/refcountcache.go:44-116`
- guarded single-file program reuse:
  `internal/compiler/program.go:332-470`
- serial-or-parallel work group: `internal/core/workgroup.go:11-89`
- graph-partitioned checker pool:
  `internal/compiler/checkerpool.go:38-106`
- structured diagnostics: `internal/ast/diagnostic.go:33-63`, `:138-192`
- generated diagnostic catalog: `internal/diagnostics/generate.go:84-144`
- source-span fidelity: `internal/spanmap/spanmap.go:70-100`
- request cancellation: `internal/lsp/server.go:978-1002`, `:1341-1366`
- trace events and profiling: `internal/tracing/tracing.go:113-187`,
  `internal/pprof/pprof.go:22-66`
- golden-baseline orphan detection:
  `internal/testutil/baseline/baseline.go:23-80`
- marker-driven tooling tests:
  `internal/fourslash/test_parser.go:114-169`

The snapshot code also records a boundary failure worth avoiding:
`internal/project/snapshot.go:473-481` describes retaining the construction
host into later use as a design problem. Persistent state must not retain a
temporary filesystem or request host implicitly.

## Architectural observations, not recommendations

TypeScript Go uses one mutable AST across parser and binder work. Nodes use a
flat header plus per-kind payload, and generated `As...` accessors use unchecked
type assertions. The binder attaches symbols, flow data, and scope tables;
checker facts largely live in side tables. Its checker is a large stateful
object with many manually keyed caches, while emission runs an AST transform
chain before printing.

These are coherent responses to TypeScript's compatibility burden, but they
are contrary examples for Hexal:

- do not add a binder or analyzer pass merely to mirror a mature compiler;
- do not replace explicit checked-node dispatch with unchecked assertions;
- do not grow one checker god-object when a fact belongs beside the checker
  rule that consumes it;
- do not add a transform pipeline between checking and C generation without a
  feature that cannot be expressed in the existing forward-only pipeline.

TypeScript generics are inferred and substituted rather than monomorphized into
native definitions. That design follows JavaScript emission and is not a model
for Hexal's C23 lowering.

## Ten dispositions

| # | Learning | Disposition | Owner |
| --- | --- | --- | --- |
| T1 | Stable diagnostic identity separated from wording | Adapt when structured tooling exists | deferred RFC 0243 |
| T2 | Measurement, tracing, and calibration beside heuristics | Adopt the practice; existing observability is sufficient | RFC 0244 |
| T3 | Immutable snapshots, dirty maps, ref-counted state, and guarded single-file reuse | Adapt for a future long-lived driver/service | RFCs 0232 and 0242 |
| T4 | Exported-shape fingerprints and reverse-dependency invalidation | Implement fingerprints now; defer invalidation | RFC 0244, then RFC 0232 |
| T5 | Serial-or-parallel work groups and graph-partitioned checkers | Reject now; preserve as a future implementation pattern | RFC 0232 |
| T6 | Cached subtree fact bitmasks | Adapt only after traversal profiling shows value | Future measured optimization |
| T7 | Explicit source-span fidelity | Adapt to Hexal's `#line` and future tooling boundary | RFC 0242 |
| T8 | Golden baselines with orphan detection and marker-driven tooling tests | Adapt when the matching artifact/tooling exists | Existing manifest and RFC 0242 |
| T9 | Structural typing, binder/analyzer layering, flat unchecked AST, and checker monolith | Reject | Existing Hexal architecture |
| T10 | Declaration files, core filesystem caches, source maps, and a first-class language-service stack | Reject now or keep outside the core | RFCs 0232 and 0242 |

## T1: diagnostic identity, not a central wording regime

TypeScript diagnostics carry a stable numeric code, string key, category,
parameterized text, primary location, and optional related locations. The
string key survives serialization through build information and APIs.

The central JSON catalog has costs. Go identifiers and localization keys are
derived from English text, placeholder mistakes can fail at runtime, and the
generated catalog is large. Hexal should therefore preserve its current single
renderer and phase-local diagnostic ownership. When a real LSP, incremental
store, or structured consumer exists, diagnostics should gain stable machine
identity, parameters, and related spans without requiring wording to move into
one catalog.

Migrating roughly 1,423 current emission sites now would be high churn with no
present user benefit. Whether codes appear in CLI output and where templates
live remain decisions for the future implementation RFC.

Deferred RFC 0243 now owns that work. It was split out because deferred RFC
0242 names stable identity as a prerequisite for an editor protocol while its
"Does not authorize" line excludes stable diagnostic-code migration, so nothing
owned the gap between them. RFC 0243 records the measured cost, the constraint
that wording stays with the emitting phase, and the five decisions an
implementation must make.

## T2: measurements belong beside tuned policy

The checker partitioner records its algorithm, citations, alternatives,
project timings, and constant sweeps beside the chosen heuristic. Trace-event
and pprof hooks make those decisions reproducible.

Hexal should follow that CARE practice: a threshold chosen by measurement says
what was tested and what numbers selected it. Hexal's existing observability
harness is sufficient; this finding does not justify another framework.

RFC 0244 owns the immediate rule, the focused current-policy audit, and the
requirement that absent evidence is described honestly rather than invented.

## T3: immutable snapshots live above `compiler.Compile`

TypeScript Go advances immutable snapshots with explicit change records, uses
copy-on-write dirty maps, shares retained state through ref-counted lifetimes,
and takes a guarded single-file clone fast path when its invariants hold.

This is the right family of ideas for a future watcher, incremental driver, or
language service. It is wrong inside Hexal's stateless
`map[string]string`-in/generated-files-out compiler contract. Persistent state
must be owned by a long-lived caller, replaced atomically, cancellable, and
unable to retain a temporary filesystem host accidentally.

Ref counting is an implementation technique, not a language concept. A Hexal
design may choose ordinary Go ownership if it proves the same snapshot
lifetime more simply.

## T4: invalidate by exported semantic shape

Classic TypeScript separates a source version from the signature of its public
declaration shape. An implementation-only edit rebuilds that file but need not
invalidate all dependents when the exported contract is unchanged. Reverse
references identify the affected closure.

Hexal should adapt this without adding `.d.hex` files. A future incremental
layer can fingerprint compiler-owned checked exports: names, visibility,
nominal identities, signatures, generic parameters, and other facts importers
consume. Users continue to maintain one source of truth.

RFC 0244 owns the deterministic fingerprint metadata. RFC 0232 still owns the
later snapshot, cache, reverse-dependency walk, and decision to skip work.

## T5: one algorithm may run serially or in parallel, but not yet

TypeScript's `WorkGroup` lets one algorithm execute serially in deterministic
contexts or concurrently at scale. Its checker pool then adds file affinity,
graph partitioning, ownership rules for checker-produced types, cancellation,
and deterministic result merging.

The small abstraction is attractive only after Hexal actually needs parallel
work. Adding it now would create an unused concurrency seam. RFC 0232 must first
show that checking—not external C compilation—is the bottleneck and must retain
a serial mode as the determinism oracle.

## T6: subtree facts are a measured optimization

TypeScript caches bitmasks describing whether a subtree contains constructs a
later transform cares about. A pass can skip an entire subtree when the needed
fact is absent.

This can reduce repeated discovery walks, but it adds cache invalidation and
memory to every relevant node. Hexal should introduce it only if traversal
instrumentation identifies repeated full-tree walks as material. The initial
implementation, if ever justified, should cache facts already computed during
construction rather than add another analysis pass.

## T7: source mapping needs fidelity, not another file format

TypeScript distinguishes exact, atomic, approximate, and unmappable spans and
drops diagnostics that have no valid original source counterpart. That is a
useful semantic distinction for generated or transformed syntax.

Hexal should keep C23 `#line` rather than adopt JavaScript source maps. If
future synthetic nodes or editor transforms need richer mapping, the internal
span API may gain an explicit fidelity value. Approximate locations must never
be silently presented as exact.

## T8: test the artifact inventory, not only expected content

TypeScript baselines detect both changed expected output and orphaned baseline
files. FourSlash tests place named markers and ranges in small source programs,
then drive the real language-service path for completion, definition, rename,
diagnostics, and formatting.

Hexal's snippet SHA-256 manifest already supplies broad generated-artifact
regression coverage. Any future text baselines should add orphan detection.
Marker-driven tests belong only with the future tooling they exercise; a marker
DSL is not useful compiler infrastructure by itself.

## T9: preserve Hexal's smaller architecture

TypeScript's structural relation requires global memoization, recursion-cycle
states, depth and work budgets, variance probing, and residual nominal rules
for private/protected members. The relation engine alone is thousands of lines.

Hexal keeps nominal type identity. Identical layouts remain distinct unless a
declared language relationship connects them. The same simplicity test rejects
a separate binder/analyzer, unchecked per-kind AST assertions, a monolithic
checker cache object, and an emitter transform chain without a concrete need.

## T10: tooling is a separate product surface

TypeScript's language service uses immutable programs, checker affinity,
request-scoped lifetimes, cancellation, virtual/original span maps, formatting,
and thousands of end-to-end tests. This is not a thin wrapper.

Hexal should not add that surface speculatively. RFC 0242 preserves the useful
boundaries:

- `compiler.Compile` remains stateless and filesystem-free;
- filesystem resolution and caches live in a driver/service;
- a formatter remains separate from type checking and generation;
- lint is not compiler correctness;
- user-authored declaration files are not a second interface language; and
- editor state never changes nominal identity or generated C.

## Explicit rejections

- No binder or analyzer pass is added.
- No structural or gradual typing is introduced.
- No flat AST with unchecked node-kind assertions is introduced.
- No declaration-file language is introduced.
- No persistent cache or filesystem observation enters the core compiler.
- No JavaScript-style source-map format supplements C23 `#line`.
- No parallel checker, language server, formatter, or linter is authorized.
- No central diagnostic wording catalog is authorized by this research.

## Validation record

This section is exhaustive.

- Three reviewed revisions are pinned above.
- All RFC 0141 scope areas have cited evidence: compiler phases, diagnostics,
  typing and generics, declaration/API boundaries, incremental/module state,
  emission/source mapping, testing, and tooling.
- Exactly ten learnings have an adopt, adapt, reject, or already-present
  disposition and an owner where future work remains.
- Findings from all three reviews are either represented in a disposition or
  recorded as an architectural observation/rejection.
- No Hexal language, compiler, generated-C, snippet-manifest, or
  `docs/reference.md` change lands under RFC 0141 or this record.
- `go vet ./...` passes on 2026-09-24.
- `go test ./...` was run on 2026-09-24 and does not pass in the current dirty
  worktree: two pre-existing `cmd/hexal` tests expect a host-qualification
  diagnostic but receive `target profile is not qualified`, and the workbench
  test cannot bind `127.0.0.1:8080` because the user's running workbench owns
  it. The research must not change those unrelated paths or terminate that
  process merely to close its gate.

## Completion

The research and durable record are complete. RFC 0141 and this record can be
closed and archived once RFC 0141's ordinary-suite gate passes in a settled
worktree. No implementation follows directly from this review; adapted future
work is owned by RFC 0244 or remains deliberately deferred to RFC 0232,
RFC 0242, or RFC 0243.
