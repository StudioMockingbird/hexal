# RFC 0245: Codebase Refactoring Audit

- Kind: Feature Specification (Rust-Style RFC)
- Status: Audit complete; implementation-ready only for the bounded pass below. Additional findings require separate scope decisions; implementation not started
- Created: 2026-09-25
- Coordinates with: RFC 0239 (generator scope invariant), RFC 0244 (measured policy and export fingerprints), deferred RFC 0243 (stable diagnostic identity), deferred RFC 0232 (incremental compilation)
- Does not change: Hexal syntax or semantics, diagnostic wording or ordering, the public compiler API, generated C, the in-memory compiler boundary, or `docs/reference.md`

## Summary

An audit across twenty refactoring lenses found a clean Go formatting baseline
and four small changes worth making now. Independent audits also exposed
reproducible correctness defects and documentation drift. Those findings are
recorded below, but are not silently added to this behavior-preserving pass.
Some proposed pass names do not justify work of their own: some are already
implemented, some need a separate design, and some have no reproduced defect.

The executable scope is deliberately narrow:

1. Index supplied source keys by canonical module identity once per import
   discovery/compilation, replacing repeated full-map scans.
2. Make the ordinary resolver-diagnostic helper delegate to its categorized
   counterpart, retaining one implementation of location resolution.
3. Reuse the existing tagged C23 compile assertion in the qualified-profile
   harness instead of keeping a second copy.
4. Rename one spec-provenance test name to the behavior it protects.

No generator mega-function, type identity, ownership fact, or diagnostic
representation changes in this pass.

## Evidence and dispositions

The audit ran against the 2026-09-25 working tree. `go test ./...` and
`go vet ./...` passed. `gofmt -l .` and `git diff --check` produced no findings;
tracked Go files had no CRLF. The earlier claim that runtime templates also
had no CRLF was false: `compiler/corelib/runtime/program.c` and `entropy.c`
are `i/lf w/crlf` on this Windows checkout, and `program.c` reaches generated
`hexal/program.c` with CRLF. A tagged `list-runs` fixture compiled and ran
under Clang, including the qualified profile; the full tagged suite was not
run in this audit.

The checked-in complexity report measured 1,877 function bodies. The most
complex were `validateExpressionNode` (516 lines, cyclomatic 341, cognitive
360), `writeStatementsAt` (311 lines, cognitive 240), and
`computeHeaderRequirements` (303 lines, cognitive 207). These figures identify
where a *separate* structural pass should begin; they do not prove that
generator preflight validation is redundant.

| # | Audit lens | Disposition and evidence |
| ---: | --- | --- |
| 1 | Line endings and `gofmt` | Go is clean. Two embedded core-library `.c` templates are not pinned to LF and produce host-dependent generated-C bytes; see finding A4. Do not normalize unrelated files. |
| 2 | Dead code | Several private functions and constants have no production callers; see finding B1. `legacyUTF8Valid` remains a test oracle. Exported symbols need an API decision before deletion. |
| 3 | Legacy APIs | `lexer.Token` and `types.Diagnostic` retain `Span` plus legacy `Line`/`Column`; consumers still use both. A dedicated migration is required, not a blind deletion. `deprecatedDeclaration` deliberately reports old declaration syntax. |
| 4 | Determinism | Existing integration coverage is incomplete: capture diagnostics and foreign-header selection both vary across runs; see A1 and A2. Preserve exact output only for the bounded pass. |
| 5 | Diagnostic centralization | `types.Diagnostic` owns rendering and ordering, but several generator errors are constructed without source spans; see B5. The resolver's `record`/`recordCategory` duplication is selected below. Stable identities remain deferred RFC 0243. |
| 6 | Naming | `PixelSubtotal` is an odd exported stats/JSON name, but renaming it would change an API. Defer. `TestHeapFreeRejectsRFCBoundaries` names provenance rather than behavior and is selected below. |
| 7 | Giant files | `generator/render.go`, `emission.go`, and `validation.go` are large. Split around the measured complex responsibilities after RFC 0239, not by arbitrary line count and not in this pass. |
| 8 | Parameter bundling | Several driver operations repeatedly pass backend, staging, target/options, and result. A focused context might help; no blanket argument-struct rewrite is authorized without a concrete call-chain design. |
| 9 | Exported API docs | `go doc hexal/compiler` describes the intended in-memory entry point and result. No exported-API documentation change is selected in this pass. |
| 10 | Error handling | Compilation failures use structured diagnostics and a single failure-result path. A string comparison against `"EOF"` is independently worth replacing; see B6. Deduplicate only the resolver's repetition below. |
| 11 | Standard library | Existing code uses `slices`, `maps`, and standard string facilities. No custom utility was proved worth replacing. |
| 12 | Phase dispatch | Grammar and several dispatcher guards exist, but the expression sequencing classifier omits effectful kinds; see A3. Other syntax families still lack completeness guards; see B3. Do not infer a miscompile from an unguarded switch alone. |
| 13 | Import resolution | Reachability owns path resolution and the checker consumes resolved graph edges; do not build another authority. `sourceKeyFor` scans every source key for each module and is selected for indexing below. |
| 14 | Literal registry | Generated strings use one `literalRegistry`, but builtin type/fact/protected-name registries are separate and have no completeness invariant; see B2. Do not confuse these registries. |
| 15 | Builtin interning | `ProcessOptions.arguments` has a `List<String>` identity distinct from an ordinary `List<String>` binding; a direct equality probe fails; see A5. A shared arena does not cover this prebuilt field. |
| 16 | Ownership/cleanup | Pointer, alias, branch, free, and deferred-cleanup coverage is extensive. No safe ownership simplification was established. A returned mutable-slice aliasing risk needs a concrete caller and mutation probe before action; see C4. |
| 17 | Test helpers | The qualified-profile tagged C23 assertion repeats the same compile/success check as `assertCompilesProject`; selected below. Other proposed consolidations need per-site evidence; cross-package duplication is not itself a defect. |
| 18 | Reference sync | `reference.md` contradicts itself on implemented C interop, `unsafe`, File, and sockets; see B7. The status board and spec placement also drift; see B8. No reference edit belongs in this behavior-preserving bounded pass. |
| 19 | Hot paths | The driver runs `DiscoverCImports` then `Compile`, parsing a no-C-import project twice. The benchmark document's corpus allocation number describes an older corpus; its apparent 3.16x difference is not a valid same-workload regression; see B9. Parser sharing and hot-path work require separate measurements. |
| 20 | Conformance | Ordinary tests and vet passed; one tagged generated-C fixture compiled and ran. Full tagged C23, sanitizer, and cross-target qualification remain outside this audit's claim. |

The module-scale probe called `DiscoverCImports` with one root importing
100, 200, 400, then 800 simple modules. It reported approximately 1.1, 3.3,
9.7, and 33.0 ms respectively. That measures the whole discovery operation,
not `sourceKeyFor` alone. The source establishes the repeated scan: `visit`
calls `sourceFor` per module, and `sourceFor` calls `sourceKeyFor`, which loops
over all supplied source keys. The algorithm is O(reachable modules x supplied
keys) before parsing costs. Another 200-round probe measured 3.19 and 3.77 ms
for discovery against 31.66 and 36.91 ms for full compilation of the same
one-module source. These are local directional measurements, not a performance
contract or a claim about a production project.

## Independent-audit addendum

Three independent reports were checked against the same working tree. The
following findings supersede conflicting rows above. They are an inventory,
**not additions to the bounded implementation plan or its exhaustive
Validation**. A correctness change needs its own specified behavior, tests,
and manifest assessment before implementation; a cleanup needs a precise
caller/compatibility proof. The reported line counts and performance samples
are snapshots, not targets.

| ID | Finding and evidence | Problem a follow-up would solve | Disposition | RoI |
| --- | --- | --- | --- | --- |
| A1 | `checker/calls.go` ranges `envCaptures[name]`. A 100-compile probe of one source blamed `alpha` 90 times and `beta` 10 times. | Make diagnostic content deterministic, not merely diagnostic order. | Confirmed bug; sort capture names before choosing the reported uninitialized one. Separate correctness scope. | Very high |
| A2 | `generator/foreign.go` overwrites a symbol's header while ranging a module map. A 100-compile probe with `same_add` declared in both `a.h` and `b.h` included `a.h` 12 times and `b.h` 88 times in `modules/app.h`. | Keep the header for a referenced C symbol stable; decide whether conflicting declarations should instead be diagnosed. | Confirmed bug; the duplicate-symbol semantic choice must be specified before fixing. | Very high |
| A3 | `expressionMayObserve` omits `CorelibCallExpression`, `StringFromRunesExpression`, and `VolatileReadExpression`. `Prog.available_parallelism() + Prog.available_parallelism()` emits both calls in one C expression with no `hex_seq_` temporaries. | Preserve Hexal's written evaluation order for observable expressions and avoid potential conflicting unsequenced effects. | Confirmed ordering gap; audit all expression kinds, then add a generated-C regression. No undefined-behavior claim is made without a conflicting-effects probe. | Very high |
| A4 | `.gitattributes` pins `generator/packages/*` but not `corelib/runtime/*.c`. `git ls-files --eol` reports `i/lf w/crlf` for `program.c` and `entropy.c`; compiling `std.program` embeds CRLF in `hexal/program.c`. | Make generated-C bytes and snippet hashes independent of host checkout line endings. | Confirmed cross-host reproducibility defect. Pin only the affected runtime templates to LF and measure manifest impact. | High |
| A5 | `types/process.go:builtinListType` intentionally creates a global identity distinct from `Environment.ListType`. A program that constructs `ProcessOptions(arguments = arguments)` then checks `options.arguments == arguments` fails with `equality requires identical canonical non-numeric operand types`. | Give identically typed built-in fields and ordinary values the same type identity. | Confirmed checker defect; separately specify where built-in field types are rebound to the compilation arena. | Very high |
| B1 | Repo-wide reference search finds no callers for private `topLevelItemToken`, its `assignmentTargetToken`, `allocatorSourceBinding`, `flowState.mergeBranch`, or `hexalHeaderPrefix`. `requiredFacilities` is defined but never read. | Remove misleading dead paths and unused policy data. | Low-risk deletion candidate after a per-symbol test/API sweep. Do not delete exported `TypeConstructors`, `WideningPairs`, `CheckError`, or `IsDevelopment` solely because this repo has no caller. | High |
| B2 | Builtin type records, protected names, spec IDs, and type facts have distinct registries with no completeness cross-check; `builtinTypes`' comment describes a broader set than its entries. | Prevent a new builtin from silently missing one registration or using fallback facts. | Add a cross-registry invariant/negative test only after spelling the intended domain of each registry; do not collapse distinct concerns blindly. | High |
| B3 | Expression/statement dispatch guards exist, but `typeExpressionNode`, `topLevelItemNode`, `matchPatternNode`, and `externDeclarationNode` lack equivalent whole-family coverage. `resolveTypeUse`'s default reports a user `Type Error` for an unsupported checked node. | Preserve fail-closed classification when syntax grows. | Design a small coverage guard and distinguish intentional no-op walkers from exhaustive dispatch. No present miscompile was proved by this inventory. | High |
| B4 | Production constructs `expressionValidation` both fully and partially; the partial states make many nil-map guards load-bearing. | Make validation state total by construction, shrinking guard clutter without panics. | Coordinate with RFC 0239's scope-state invariant; do not remove guards first or silently enlarge this RFC. | Medium-high |
| B5 | Several generator errors are inline `Diagnostic{}` values without a source span; `types.Diagnostic` itself already owns common rendering. | Improve source attribution for genuine generation failures. | Inventory which sites have a checked span available; do not replace phase-local wording with a central message catalog. | Medium |
| B6 | `internal/driver/runpack.go` tests `err.Error() == "EOF"` and `driver.go` recognizes an `Unknown Error` by substring. | Avoid control flow keyed to changeable error text. | Use `errors.Is(err, io.EOF)` for the manifest decoder; review the diagnostic representation before changing driver behavior. | Medium-high |
| B7 | `reference.md` says C interop is a draft at line 66 and excludes `unsafe`/pointer operations and File/sockets near the end, despite implemented sections describing them. | Keep the normative language contract internally consistent. | Confirmed document drift. Repair only with explicit consent to edit `docs/reference.md`; no such edit is part of this pass. | Very high |
| B8 | `status.md` lists deferred 0209 as implementation-ready, gives an open bug an archived 0143 owner, and retains completed-work prose; closed 0141/0241 and deferred 0232/0242/0243 remain in the active spec directory. Its 140-snippet account is historical, not a claim about the current 160-snippet manifest, but does not belong on an open-work-only board. | Make status and spec location reflect actual lifecycle without falsifying historical test results. | Documentation housekeeping with exact disposition/ownership checks; moving terminal specs must not edit their contents. | High |
| B9 | `docs/benchmarks.md` records 80,192 `BenchmarkCorpus` allocations for an older corpus; a current local run measured about 253,000. The catalog now has 160 snippets. | Establish a comparable current baseline before claiming a regression or optimizing. | Re-run on fixed inputs and record corpus/version; the ratio alone is not a regression because workload changed. | High |
| C1 | `render.go`, `emission.go`, and `validation.go` each exceed roughly 1,900 lines; `validateExpressionNode`, `writeStatementsAt`, and `computeHeaderRequirements` dominate complexity. | Make phase responsibilities easier to reason about. | After RFC 0239, extract by responsibility with no semantic or artifact change; line count alone is not the target. | Medium-high |
| C2 | Repeated checker/generator parameter groups and driver build arguments recur across many call chains. | Reduce argument-order mistakes and clarify state ownership. | Bundle only a proven repeated group inside one responsibility; no blanket signature rewrite. | Medium |
| C3 | The tagged C23 compile assertion is duplicated inside one package; additional proposed helper mergers cross package or execution boundaries. One test name carries RFC provenance. | Cut fixture maintenance without changing test expectations. | The bounded pass owns the same-package assertion and test rename; other candidates need equivalence proof. | Medium |
| C4 | Some exported type/union accessors and driver result paths expose mutable slices. | Avoid accidental mutation of compiler-owned state across calls. | Candidate only: identify an actual caller and mutation path before copying every slice. | Medium |
| C6 | `types.Diagnostic` carries authoritative `Span` plus legacy `Line`/`Column`; rendering still consumes the integers. | Eliminate dual location state once all producers and consumers agree. | Dedicated migration with exact output checks, not a dead-field deletion. | Medium |
| C7 | `DiscoverCImports` followed by `Compile` parses the same no-C-import project twice, and `sourceKeyFor` scans every supplied key per reached module. | Reduce repeat work on module-heavy projects. | Only the source-key index is in this bounded pass; parser-output reuse requires a separate API and benchmark design. | Medium-high |
| C8 | `reachState.record` duplicates the location-resolution logic in `recordCategory`. | Keep resolver diagnostic locations and categories consistent. | Selected for the bounded pass: delegate with no rendered-diagnostic change. | Medium |
| C9 | `PixelSubtotal` is an unusual exported stats name; one integration test carries RFC provenance in its name. | Make public and test names easier to understand without historical context. | Only the private test rename is selected; an exported stats rename requires a compatibility decision. | Low-medium |

### Claims excluded or narrowed by verification

- Eight identical catalog runs did **not** cover A1 or A2. Their fixtures do
  not contain the necessary capture or duplicate foreign symbol. Broad green
  determinism tests therefore do not refute those probes.
- The tagged `c23validation` package is live, not dormant. An earlier report's
  description of it as dormant is historical and must not drive cleanup.
- An unguarded `containsPointerType` walker was suspected of rejecting
  recursive pointer types; independent probes accepted the suspected forms.
  Record this as a refuted bug hypothesis, not a reason to change the walker.
- Package-init panics from invalid compiler-owned embedded templates are not
  user-source errors and do not by themselves violate `Compile`'s fail-closed
  contract. Converting them to runtime diagnostics needs a separate reason.
- No evidence here makes every unused exported function or same-named test
  helper in another package safe to remove. Likewise, an apparent benchmark
  allocation ratio across different corpora cannot establish a performance
  regression.

## Structural split inventory

An additional function inventory identified the following responsibility
boundaries. These are **candidates for later move-only passes**, not steps in
this RFC's bounded implementation plan. The reported line ranges are evidence
from one snapshot, not move instructions: use declaration names and re-inventory
after earlier edits. A file move must preserve package, exported names, test
cases, diagnostics, and generated artifacts; it does not by itself simplify a
large function.

| Current file | Candidate ownership split | Assessment | RoI |
| --- | --- | --- | --- |
| `generator/render.go` | `render_statements.go` owns `writeStatements*` and statement/control rendering; `render_state.go` owns `expressionValidation` and `generatedBinding`; `spelling.go` owns `pointerSpelling`, `declaration`, `typeSpelling`, `funDeclaration`, and `standaloneResultSpelling`; `render_operations.go` owns expression/operator rendering; `ring.go` owns `renderSignedWrap` through `ringPrecedence`. Keep a small dispatcher/literal/operand core in `render.go`. | Strongest split candidate. Confirm that recursive helpers move with their owner; do not turn `render.go` into a residual dumping ground. Move one responsibility at a time. | High |
| `generator/validation.go` | Separate program/function entry validation, generated-type validation, constant validation, expression validators, and place metadata (`checkedPlaceMetadata`); keep the package-level fail-closed entry coherent. | Strong seam, but `validateExpressionNode` remains a large function after a file move. Any internal decomposition is a separate, behavior-preserving design. | High |
| `generator/emission.go` | Separate discovery (`discoverModuleEmission`), merge/header requirements (`mergeProgramEmission`, `computeHeaderRequirements`), module-pair/root emission (`emitModulePair`), header models/builders, and object/nominal bodies. | Strong seam; preserve ordering and program-wide ownership of emitted declarations. No arbitrary extraction by line number. | High |
| `checker/scope.go` | Keep scope/binding/capture core; put `flowState` machinery in `flow.go` and diagnostic constructors in `diagnostics.go`. | **Coordination check, not new work:** at this audit's snapshot these exact files exist as uncommitted, in-progress changes. Verify that pass's result and tests before scheduling any remainder. | Already underway |
| `types/types.go` | Keep core `Type` definitions; put `Diagnostic` and rendering/comparison in `diagnostic.go`, builtin identities and protected names in `builtins.go`, and `isCanonical*` in `canonical.go`. | **Coordination check, not new work:** at this audit's snapshot the three destination files exist as uncommitted, in-progress changes. Use their landed ownership for the B2 completeness test rather than repeating the move. | Already underway |
| `checker/generics.go` | Keep generic registration separate from type specialization, function/method specialization, inference/unification, and call checking. Place `specializeADTType` beside the other type-specialization path. | Worth a focused pass after current higher-risk bugs; clarify which shared table and diagnostics each owner retains. | Medium |
| `internal/driver/normalize.go` | Separate importer/AST records, C type parsing (`parseCType` and resolution), macro/scalar mapping, and final ordering/deduplication. | Useful for C interop maintenance; preserve target-specific mappings and deterministic generated bindings. | Medium-high |
| `checker/operator_checking.go` | Move `foldUnary`, `foldBinary`, and related constant-folding helpers to `constant_folding.go`; keep operator checking and type eligibility together. | Small, coherent move. Do not alter overflow or constant evaluation while relocating. | Medium |

Test-file candidates are lower risk but still require a no-case-lost inventory:

| Current file | Candidate split | Caution |
| --- | --- | --- |
| `generator/generator_test.go` | Group declarations, unions, rendering, and operation/ring tests into facet files. | Keep shared fixtures in one obvious owner; tests remain in the same Go package. |
| `parser/parser_test.go` | Group declaration, expression, and recovery tests. | Preserve each source and expected diagnostic exactly. |
| `tests/integration/pointers_test.go` | Move heap-free and cross-allocator cases, including the long `TestHeapFreeRejectsRFCBoundaries`, to `allocators_test.go`; keep pointer syntax cases in `pointers_test.go`. | The bounded pass first renames that provenance-based test; a later move must not duplicate or weaken its roughly 360-line case set. |

Do not split `lexer.go` solely for size: scanning is one linear responsibility.
`generator/concurrency.go` is one coherent domain despite several internal
phases. Delete the dead checker dispatch helpers before considering a
`checker.go` split. Keep the C23 `fixtures_test.go` catalog together unless
retrieval or test ownership actually degrades. These are scope decisions, not
claims that their line counts are ideal.

For a later split sequence, first verify whether the concurrent `scope.go`
and `types.go` extractions landed; do not re-execute them. Then move one
generator responsibility per change, followed by generics/importer/operators
and optional test organization. Run `gofmt`, the ordinary full suite, and
the snippet-manifest comparison after each move. Tagged generated-C coverage
is a separate gate when the move touches emission semantics; a pure relocation
must not change any artifact hash.

## Bounded changes

### 1. Index source keys once

Build a compilation-local map from `canonicalFromLogicalKey(key)` to source
keys when constructing each `reachState`, including the discovery-only state.
Sort each bucket once. `sourceKeyFor` then reads that map; it never scans
`s.sources`. The index is derived data, not a second module-identity rule.

Preserve all current behavior:

- `sourceFor` still prefers embedded standard-library sources and reports a
  user attempt to claim a reserved standard-library key.
- Prepared `hexalc` binding keys retain their reserved-path exception.
- An absent key still reports the same missing-import diagnostic.
- If several supplied keys have the same canonical identity, the first
  lexicographic key and existing ambiguity/error behavior remain unchanged,
  including malformed input keys. Do not assume the map's insertion order.
- `sourceTable` still contains every supplied source under its original
  logical key; the index never rewrites or validates those keys early.
- `DiscoverCImports` and `Compile` share the identical index rule but keep
  their existing public signatures and separate lifecycles.

Do not replace the scan with a filesystem lookup or cache index state across
compiler invocations.

### 2. Deduplicate resolver diagnostic construction

`reachState.record` delegates to `recordCategory` with `ModuleError`. Keep
the current 1-based position normalization, `LogicalKey` lookup/fallback,
stage, category, message, and append order byte-for-byte. No general
diagnostic factory or centralized wording table is introduced.

### 3. Reuse the tagged compile assertion

The qualified-profile helper in `target_qualification_test.go` delegates to
`assertCompilesProject` in `catalog_test.go`, or its callers use that helper
directly. Remove the duplicate `compiler.Compile`/success check. Preserve
the qualified `Project` and every fixture's compile/run/trap expectation.
The failure message may be unified with the existing helper; that message
is test-harness output, not a Hexal diagnostic contract.

### 4. Name the test for behavior

Rename `TestHeapFreeRejectsRFCBoundaries` in the integration pointer tests to
`TestHeapFreeRejectsInvalidStorageAndRepeatedRelease` or another equally
specific behavior name. Do not change its cases or assertions.

## Implementation plan

### Bounded behavior-preserving pass

This sequence implements only the four Bounded changes. Complete each stage
before starting the next so an unexpected output change has one owner. Work
in the existing checkout: other changes in the shared worktree belong to
their authors and must not be reset, overwritten, or folded into this pass.

1. **Inventory and baseline.** Record `git status --short`, the current
   source-key resolution and resolver-diagnostic call sites, the two tagged
   compile-assertion helpers, and the pointer-test name. Record `gofmt -l .`,
   `go test ./...`, `go vet ./...`, and the exact bytes of
   `workbench/snippets/testdata/generated-c-sha256.json`. Distinguish failures
   already present in another agent's in-progress edits from failures this
   pass introduces. Do not regenerate the manifest.
2. **Specify the source-index invariant before editing.** Inventory every
   `reachState` constructor and `sourceKeyFor`/`sourceFor` caller, including
   discovery-only construction. Enumerate the current precedence for an
   embedded stdlib source, a user-supplied reserved stdlib key, a prepared
   `hexalc` key, an absent key, and colliding canonical keys. The derived
   index maps each canonical identity to its sorted original logical keys;
   it never changes `sourceTable`, import resolution authority, or accepted
   input at construction time.
3. **Implement the index.** Populate it once per `reachState` from the
   supplied map, without host-filesystem access. Replace only the repeated
   full-map lookup, not the diagnostic or path rules. Apply the same index
   rule in `DiscoverCImports` and `Compile` while retaining their separate
   lifecycles. Keep original logical keys in all results and diagnostics.
4. **Prove source behavior.** Add the focused module-resolution cases named
   in Validation: shuffled map insertion, missing import, malformed colliding
   keys, reserved stdlib key, and prepared C binding. Compare requests,
   dependencies, files, exit code, and diagnostic strings with the pre-change
   baseline. Run the existing determinism tests before touching diagnostics.
5. **Collapse resolver location logic.** Make `reachState.record` call
   `recordCategory` with `ModuleError`. Remove only the duplicate normalization
   and append body. Compare exact rendered diagnostics for the existing
   resolver fixtures; a different position, category, stage, fallback key, or
   order is a failure, not a reason to refresh expectations.
6. **Unify the C23 assertion.** Make the qualified-profile helper delegate to
   `assertCompilesProject`, or use that helper directly at its call sites.
   Preserve the passed `Project`, fixture program, compile/run distinction,
   and expected stdout, trap, and exit behavior. Run `go vet -tags c23 ./...`
   and the one qualified-profile compile-and-run fixture named in Validation.
   Do not infer from vet that generated C compiled.
7. **Rename the integration test.** Change only the declaration name of
   `TestHeapFreeRejectsRFCBoundaries` to the behavior-based name specified
   above. Count its cases and assertions before and after. Do not split or
   rewrite the 360-line test during this bounded step.
8. **Close the pass.** Run the complete Validation section. Compare the
   manifest byte-for-byte and inspect the final diff for unrelated file moves
   or formatting churn. Read the affected contracts in `docs/reference.md`;
   record why no normative edit is needed, and do not edit it under this
   behavior-preserving spec. Report any pre-existing failures separately.

### Later move-only file splits

This is an implementation route for the Structural split inventory, **not
additional acceptance work for the bounded pass**. Before authorizing a
split, re-inventory the current tree: during this audit, separate work began
extracting `checker/flow.go`, `checker/diagnostics.go`,
`types/diagnostic.go`, `types/builtins.go`, `types/canonical.go`, and several
generator, checker, driver, and test files. If a responsibility has already
been moved, verify that work and skip it; never create a second owner.

1. **Freeze each move's surface.** Record every declaration in the source
   responsibility, its existing tests, the relevant generated-file hashes,
   and whether another in-progress change overlaps it. Use declaration names
   from the inventory above, not old line numbers. Confirm the destination
   stays in the same Go package and that the move adds no public API, helper
   layer, or changed diagnostic text.
2. **Finish or verify scope and type splits first.** Check that flow state,
   checker diagnostic constructors, type diagnostics, builtins, and canonical
   checks each have exactly one owning file. If the existing in-progress
   extraction is complete, run its tests and mark these five destinations
   done without repeating the edit. Do not combine a builtin-registry
   completeness change with the move; that is finding B2, with its own scope.
3. **Split generator rendering one responsibility at a time.** Move statement
   writers, rendering state, C spelling, expression operations, then ring
   helpers to the proposed owners. After each move, remove its old
   declaration from `render.go`, format, compile/test the package and full
   suite, and compare snippet hashes. Keep expression dispatch complete and
   retain the current sequencing behavior; finding A3 is a separate fix.
4. **Split generator validation next.** Move program/function entry, type,
   constant, expression, and place-metadata helpers by responsibility.
   Preserve validation order and the fail-closed `Unknown Error` behavior.
   Moving `validateExpressionNode` intact is acceptable for a move-only pass;
   refactoring its internal branches requires a separate design and tests.
5. **Split generator emission next.** Move discovery, merge/requirements,
   module-pair/root emission, header models/builders, and nominal bodies.
   Check declaration/include order and once-per-program owners through the
   existing text assertions and manifest. Do not use a split as cover for
   fixing foreign-header nondeterminism or changing generated-C layout.
6. **Move the smaller phase seams.** Put generic type specialization beside
   `specializeADTType`; separate generic registration, function/method
   specialization, inference, and calls. Separate C importer records,
   parsed C types, scalar/macro mapping, and ordering in the driver. Move
   constant-folding helpers out of `operator_checking.go`. Do one source file
   at a time and retain each package's existing tests and error outputs.
7. **Organize tests last.** Split generator and parser tests by their
   existing facets. After the bounded test rename lands, move the heap-free
   and cross-allocator integration cases to `allocators_test.go`, leaving
   pointer syntax tests in `pointers_test.go`. Compare test-function names,
   fixture strings, cases, and assertions before and after; do not introduce
   a new test package or weaken a case.
8. **Gate every move independently.** Run `gofmt`, `go test ./...`, and
   `go vet ./...`; compare the full snippet manifest without regeneration.
   If a move changes generated bytes or Hexal diagnostics, stop and treat it
   as a behavior change requiring its own explanation and validation. The
   tagged C23 lane may be used for additional confidence, but a pure move
   must first pass the ordinary textual and manifest gates. Do not start the
   next move while the current one's ownership or baseline is unresolved.

## Validation

This section is the complete definition of done for the bounded pass. The
larger audit dispositions above do not add hidden implementation work.

- `gofmt -l .` is empty; `git diff --check` has no finding; Go line endings
  remain LF. No unrelated formatting churn appears.
- `go test ./...` and `go vet ./...` pass.
- Both `DiscoverCImports` and `Compile` produce the same requests, files,
  dependencies, exit status, and diagnostics as the pre-change baseline for
  every existing fixture. New focused cases cover shuffled source-map
  insertion, missing imports, malformed colliding keys, reserved stdlib keys,
  and prepared C bindings.
- The existing integration determinism tests pass. Identical source maps with
  different insertion orders emit byte-identical artifacts and ordered
  diagnostic strings.
- The entire existing snippet manifest is byte-identical. No existing
  generated-artifact hash changes and no blanket regeneration occurs.
- The renamed integration test has the same source cases and assertions and
  contains no spec provenance in its test name.
- The tagged C23 package type-checks with `go vet -tags c23 ./...` and at
  least one qualified-profile compile-and-run fixture passes under Clang.
  A full tagged-suite claim requires a separate full tagged run; this item
  makes no such claim.
- No public compiler signature, Hexal diagnostic, or generated C artifact
  changes. `docs/reference.md` is checked against the result; because no
  language or C23 contract changes, it is not edited.

## Follow-up boundaries

The complexity hotspots deserve a dedicated structural proposal after RFC
0239. That proposal must name extraction seams inside the existing generator
validation/rendering files and preserve both the checker-to-generator
fail-closed boundary and `TestExpressionDispatchersCoverEveryConcreteKind`.
Moving functions between files without reducing responsibility or cognitive
load is not the goal.

The `Span`/line/column migration, public `PixelSubtotal` rename, diagnostic
codes, driver argument bundling, and parser-output reuse each have a distinct
API or architectural consequence. They are findings, not implied tasks here.
Incremental compilation and content-addressed caching keep their existing
owners. No benchmark result in this audit authorizes a hot-path rewrite.

The five A-series findings are higher-priority follow-up candidates than the
bounded pass, but each changes behavior or generated bytes. Specify and land
them separately with focused regressions, and do not combine their manifest
changes with the source-key-index baseline. The B-series documentation work
does not authorize editing `docs/reference.md` without the user's explicit
consent. No current behavior is defined by this inventory; the reference and
working compiler remain the authorities until a follow-up is implemented.
