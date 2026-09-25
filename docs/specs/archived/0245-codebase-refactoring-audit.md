# RFC 0245: Codebase Refactoring Audit

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented. Every A-, B-, and C-series finding closed with its
  per-finding Validation result. File splitting (`30d576a`) verified; the four
  first-wave changes (C3, C7 index, C8, C9 test) landed; A1-A5 fixed; B1 dead
  code removed; B2 registry invariant; B3 dispatch guards; B4 total
  `expressionValidation` state; B5 source-anchored generator diagnostics; B6
  structural EOF and compiler-defect bit; B7 `reference.md` reconciled; B8
  `status.md` and spec lifecycle corrected; B9 current benchmark record; C1
  splits verified; C2 `methodCall` bundle; C4 defensive foreign-record copy;
  C6 `Diagnostic` carries one authoritative `Span` + `Position` with the legacy
  integer fields removed; C7 source-key index landed and parse-sharing closed
  as a measured no-change; C9 `PhaseSubtotal`/`phaseSubtotalMs`. `go test ./...`
  and `go vet ./...` pass, the snippet manifest is unchanged, and the tagged
  C23 fixtures compile and run
- Created: 2026-09-25
- Coordinates with: RFC 0239 (generator scope invariant), RFC 0244 (measured policy), deferred RFC 0246 (export fingerprints), RFC 0243 (central diagnostic wording and stable keys), deferred RFC 0232 (incremental compilation)
- Preserves: Hexal syntax, the string-in/string-out compiler boundary, and supported language behavior. Correctness fixes may change erroneous diagnostics, generated C, and type identity; the stats rename changes the public Go/JSON surface. The normative reference must be reconciled.

## Summary

An audit across twenty refactoring lenses found reproducible correctness
defects, maintenance debt, documentation drift, and measured cleanup
candidates. **Every A-, B-, and C-series finding in the table below belongs to
this program.** The four small changes formerly called the bounded pass are
only its first behavior-preserving phase, not the whole scope. The structural
file split landed in commit `30d576a` during review; verify that result and
do not replay it.

"Fix" means one of two explicit outcomes: implement the named correction and
its validation, or run the required probe and record evidence that the
suspected problem is absent or a proposed optimization has insufficient
benefit. Do not silently drop a row. Code correctness, generated-C, public-API,
documentation, and performance work land in separate phases so their diffs
and manifest changes remain attributable. The removed proposal to move large
exported implementation packages under `internal/` is not part of this RFC.

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
| 3 | Legacy APIs | `lexer.Token` and `types.Diagnostic` retain `Span` plus legacy `Line`/`Column`; consumers still use both. Migrate deliberately under C6, not by deleting fields first. `deprecatedDeclaration` deliberately reports old declaration syntax and stays. |
| 4 | Determinism | Existing integration coverage is incomplete: capture diagnostics and foreign-header selection both vary across runs; fix A1 and A2. |
| 5 | Diagnostic centralization | `types.Diagnostic` owns rendering and ordering, but several generator errors are constructed without source spans; address B5 and C8. Central wording and stable identities belong to RFC 0243. |
| 6 | Naming | Rename `PixelSubtotal` with its Go and JSON consumers and rename the provenance test under C9. |
| 7 | Giant files | The responsibility splits landed in `30d576a`. Verify the committed ownership, tests, and unchanged generated artifacts; do not repeat moves. Large individual functions remain separate complexity work under C1. |
| 8 | Parameter bundling | Inventory repeated argument groups and apply one focused bundle where it demonstrably clarifies one responsibility; no blanket signature rewrite. |
| 9 | Exported API docs | Document the changed stats field and any structured compiler-defect signal after the final public API settles; no package-visibility move is proposed. |
| 10 | Error handling | Compilation failures use structured diagnostics and a single failure-result path. Replace the `"EOF"` comparison and text-based compiler-defect classification under B6. |
| 11 | Standard library | Existing code uses `slices`, `maps`, and standard string facilities. No custom utility was proved worth replacing. |
| 12 | Phase dispatch | Grammar and several dispatcher guards exist, but the expression sequencing classifier omits effectful kinds; fix A3. Add B3 coverage only to exhaustive dispatch, not intentional no-op walkers. |
| 13 | Import resolution | Reachability owns path resolution and the checker consumes resolved graph edges; retain that authority while indexing `sourceKeyFor` under C7. |
| 14 | Literal registry | Generated strings use one `literalRegistry`, but builtin type/fact/protected-name registries need a scoped completeness invariant under B2. |
| 15 | Builtin interning | `ProcessOptions.arguments` has a `List<String>` identity distinct from an ordinary `List<String>` binding; fix A5 without a second type scheme. |
| 16 | Ownership/cleanup | Pointer, alias, branch, free, and deferred-cleanup coverage is extensive. Prove or dismiss the returned mutable-slice alias under C4 rather than weakening ownership checks. |
| 17 | Test helpers | The qualified-profile tagged C23 assertion repeats the same compile/success check as `assertCompilesProject`; unify it under C3. Other candidates need equivalence proof. |
| 18 | Reference sync | Reconcile `reference.md` contradictions B7 and status/spec lifecycle drift B8 after behavior stabilizes. |
| 19 | Hot paths | The driver runs `DiscoverCImports` then `Compile`, parsing a no-C-import project twice. Establish a comparable baseline B9, index source keys C7, then measure whether parser sharing pays for its API cost. |
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
following findings supersede conflicting rows above and are all accounted for
in the implementation and validation phases below. A correctness change needs
specified behavior, a focused test, and a manifest assessment; a cleanup
needs a caller/compatibility proof. A suspected risk may close with a
reproducing negative probe and an explicit no-change disposition, never by
being forgotten. The reported line counts and performance samples are
snapshots, not targets.

| ID | Finding and evidence | Problem a follow-up would solve | Disposition | RoI |
| --- | --- | --- | --- | --- |
| A1 | `checker/calls.go` ranges `envCaptures[name]`. A 100-compile probe of one source blamed `alpha` 90 times and `beta` 10 times. | Make diagnostic content deterministic, not merely diagnostic order. | Sort capture names before choosing the first uninitialized one; do not reorder unrelated diagnostics. | Very high |
| A2 | `generator/foreign.go` overwrites a symbol's header while ranging a module map. A 100-compile probe with `same_add` declared in both `a.h` and `b.h` included `a.h` 12 times and `b.h` 88 times in `modules/app.h`. | Keep the header for a referenced C declaration stable. | Resolve cross-module references by their checked defining module plus C name, not C name alone. A use of `A.c_add` includes A's declared header even if B binds the same C symbol; local declarations retain their own header. No new global duplicate-symbol rejection. | Very high |
| A3 | `expressionMayObserve` omits `CorelibCallExpression`, `StringFromRunesExpression`, and `VolatileReadExpression`. `Prog.available_parallelism() + Prog.available_parallelism()` emits both calls in one C expression with no `hex_seq_` temporaries. | Preserve Hexal's written evaluation order for observable expressions and avoid potential conflicting unsequenced effects. | Add those effects, audit every checked expression kind, and test the emitted sequence. No undefined-behavior claim is made without a conflicting-effects probe. | Very high |
| A4 | `.gitattributes` pins `generator/packages/*` but not `corelib/runtime/*.c`. `git ls-files --eol` reports `i/lf w/crlf` for `program.c` and `entropy.c`; compiling `std.program` embeds CRLF in `hexal/program.c`. | Make generated-C bytes and snippet hashes independent of host checkout line endings. | Confirmed cross-host reproducibility defect. Pin only the affected runtime templates to LF and measure manifest impact. | High |
| A5 | `types/process.go:builtinListType` intentionally creates a global identity distinct from `Environment.ListType`. A program that constructs `ProcessOptions(arguments = arguments)` then checks `options.arguments == arguments` fails with `equality requires identical canonical non-numeric operand types`. | Give identically typed built-in fields and ordinary values the same type identity. | Rebind built-in aggregate field types through the compilation arena before field reads and use the same identity in constructors and comparisons. | Very high |
| A6 | `types/network.go` reserves `Address`'s `bytes` payload fields as globally interned arrays whose canonical key (`builtin-array:address-ipv4-bytes`) cannot equal the arena's `Array<Byte, 4>` key. An ordinary binding passed as `Net.Address.IPv4(bytes = binding, port = ...)` is rejected with `expected Array<UInt8, 4> initializer; got Array<UInt8, 4>`, and a narrowed `addr.bytes == binding` fails with the same canonical-identity error A5 names. `types/network.go` documents literal-only filling; `reference.md` says IPv4/IPv6 construct and match through the general ADT rules. | Give the bytes fields one identity story without a second type scheme, reconcile the comment with the reference, and keep generated-C types coherent (today the binding renders `hex_array_UInt8_4` while the payload field renders `hex_addr_ipv4_bytes`). | Recorded as a finding, deliberately deferred beyond this audit by decision: A5's implementation and Validation cover the List-typed builtin fields only. A follow-up must choose between arena-pre-seeding the builtin arrays (which renames array C names across generated C) and another scheme, then settle the comment/reference contradiction. | High |
| B1 | Repo-wide reference search finds no callers for private `topLevelItemToken`, its `assignmentTargetToken`, `allocatorSourceBinding`, `flowState.mergeBranch`, or `hexalHeaderPrefix`. `requiredFacilities` is defined but never read. | Remove misleading dead paths and unused policy data. | Re-inventory after the landed file split and delete proven private dead code. Audit exported candidates for supported external use; no automatic deletion from a repo-only search. | High |
| B2 | Builtin type records, protected names, spec IDs, and type facts have distinct registries with no completeness cross-check; `builtinTypes`' comment describes a broader set than its entries. | Prevent a new builtin from silently missing one registration or using fallback facts. | Define each registry's intended domain and test the required relation between them. Reuse current facts, not a duplicate hand-maintained list. | High |
| B3 | Expression/statement dispatch guards exist, but `typeExpressionNode`, `topLevelItemNode`, `matchPatternNode`, and `externDeclarationNode` lack equivalent whole-family coverage. `resolveTypeUse`'s default reports a user `Type Error` for an unsupported checked node. | Preserve fail-closed classification when syntax grows. | Guard exhaustive dispatch, classify unsupported checked nodes as compiler `Unknown Error`, and document intentional no-op walkers separately. | High |
| B4 | Production constructs `expressionValidation` both fully and partially; the partial states make many nil-map guards load-bearing. | Make validation state total by construction, shrinking guard clutter without panics. | Reconcile RFC 0239's landed state, route production construction through one total initializer, and remove guards only after every path is covered. | Medium-high |
| B5 | Several generator errors are inline `Diagnostic{}` values without a source span; `types.Diagnostic` itself already owns common rendering. | Improve source attribution for genuine generation failures. | Attach the checked span where one exists; preserve zero-span whole-compilation failures and phase-local wording. | Medium |
| B6 | `internal/driver/runpack.go` tests `err.Error() == "EOF"` and `driver.go` recognizes an `Unknown Error` by substring. | Avoid control flow keyed to changeable error text. | Use `errors.Is(err, io.EOF)`; carry an explicit compiler-defect bit in `CompilationResult` for the driver instead of parsing rendered messages. | Medium-high |
| B7 | `reference.md` says C interop is a draft at line 66 and excludes `unsafe`/pointer operations and File/sockets near the end, despite implemented sections describing them. | Keep the normative language contract internally consistent. | Reconcile only these verified contradictions against current compiler/tests, preserving the authoritative feature sections. The user's request to fix all findings authorizes this reference edit during implementation, not as an unrelated cleanup now. | Very high |
| B8 | `status.md` lists deferred 0209 as implementation-ready, gives an open bug an archived 0143 owner, and retains completed-work prose; closed 0141/0241 and deferred 0232/0242 remain in the active spec directory. RFC 0243 was promoted to active implementation-ready work after this audit. Its 140-snippet account is historical, not a claim about the current 160-snippet manifest, but does not belong on an open-work-only board. | Make status and spec location reflect actual lifecycle without falsifying historical test results. | Correct the board and move terminal/deferred files without editing archived contents. Give the still-open interpolation bug an active owner or close it only after a reproducing negative probe. | High |
| B9 | `docs/benchmarks.md` records 80,192 `BenchmarkCorpus` allocations for an older corpus; a current local run measured about 253,000. The catalog now has 160 snippets. | Establish a comparable current baseline before claiming a regression or optimizing. | Re-run on fixed inputs, record corpus and version, and separate workload growth from same-input regression. Optimize only a measured hot path. | High |
| C1 | `render.go`, `emission.go`, and `validation.go` were each over roughly 1,900 lines; `validateExpressionNode`, `writeStatementsAt`, and `computeHeaderRequirements` dominated complexity. | Make phase responsibilities easier to reason about. | The file splits landed in `30d576a`. Verify their responsibility boundaries and unchanged tests/artifacts; decompose any still-giant function only behind an independent complexity/behavior check. | Medium-high |
| C2 | Repeated checker/generator parameter groups and driver build arguments recur across many call chains. | Reduce argument-order mistakes and clarify state ownership. | Bundle a repeated tuple only after a call-site inventory proves one coherent owner; report a no-change result if no focused bundle improves clarity. | Medium |
| C3 | The tagged C23 compile assertion is duplicated inside one package; additional proposed helper mergers cross package or execution boundaries. One test name carries RFC provenance. | Cut fixture maintenance without changing test expectations. | Unify the same-package assertion and rename the test; merge any other helper only after case/expectation equivalence is proved. | Medium |
| C4 | `ForeignRecordMembers` returns a shared member slice; other accessors and driver result paths may expose mutable slices. | Avoid accidental mutation of compiler-owned state across calls. | Make the exported foreign-record getter defensive, test mutation isolation, then probe the remaining candidates and fix only demonstrated aliases. | Medium |
| C6 | `types.Diagnostic` carries authoritative `Span` plus legacy `Line`/`Column`; rendering still consumes the integers. | Eliminate inconsistent dual location state. | Inventory producers and consumers, select one authoritative location representation, migrate in stages, and preserve exact rendered positions. Never delete fields before their replacement is total. | Medium |
| C7 | `DiscoverCImports` followed by `Compile` parses the same no-C-import project twice, and `sourceKeyFor` scans every supplied key per reached module. | Reduce repeat work on module-heavy projects. | Index source keys now; profile duplicate parsing on a fixed workload and share parse state only if a small API preserving the in-memory boundary gives a measured win. | Medium-high |
| C8 | `reachState.record` duplicates the location-resolution logic in `recordCategory`. | Keep resolver diagnostic locations and categories consistent. | Delegate with no rendered-diagnostic change. | Medium |
| C9 | `PixelSubtotal` is an unusual exported stats name; one integration test carries RFC provenance in its name. | Make public and test names easier to understand without historical context. | Rename the stats field to `PhaseSubtotal` and the workbench JSON field to `phaseSubtotalMs`; rename the private test for behavior. This is an intentional Go/JSON compatibility change. | Low-medium |

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
boundaries. Commit `30d576a` landed broad move-only splits while this audit
was being revised. The table records the intended ownership for a
post-commit verification pass, **not instructions to replay already-landed
moves**. The reported old line ranges are evidence from one snapshot, not
move instructions. A file move must preserve package, exported names, test
cases, diagnostics, and generated artifacts; it does not by itself simplify a
large function.

| Current file | Candidate ownership split | Assessment | RoI |
| --- | --- | --- | --- |
| `generator/render.go` | `render_statements.go` owns `writeStatements*` and statement/control rendering; `render_state.go` owns `expressionValidation` and `generatedBinding`; `spelling.go` owns `pointerSpelling`, `declaration`, `typeSpelling`, `funDeclaration`, and `standaloneResultSpelling`; `render_operations.go` owns expression/operator rendering; `ring.go` owns `renderSignedWrap` through `ringPrecedence`. Keep a small dispatcher/literal/operand core in `render.go`. | Landed; verify declaration ownership and inspect the remaining large dispatchers. | High |
| `generator/validation.go` | Separate program/function entry validation, generated-type validation, constant validation, expression validators, and place metadata (`checkedPlaceMetadata`); keep the package-level fail-closed entry coherent. | Landed; verify the fail-closed entry and measure whether `validateExpressionNode` still needs an internal decomposition. | High |
| `generator/emission.go` | Separate discovery (`discoverModuleEmission`), merge/header requirements (`mergeProgramEmission`, `computeHeaderRequirements`), module-pair/root emission (`emitModulePair`), header models/builders, and object/nominal bodies. | Landed; verify ordering and program-wide ownership of declarations. | High |
| `checker/scope.go` | Keep scope/binding/capture core; put `flowState` machinery in `flow.go` and diagnostic constructors in `diagnostics.go`. | Landed; verify exactly one owner for each declaration. | High |
| `types/types.go` | Keep core `Type` definitions; put `Diagnostic` and rendering/comparison in `diagnostic.go`, builtin identities and protected names in `builtins.go`, and `isCanonical*` in `canonical.go`. | Landed; verify ownership and use the resulting `builtins.go` for the B2 completeness check. | High |
| `checker/generics.go` | Keep generic registration separate from type specialization, function/method specialization, inference/unification, and call checking. Place `specializeADTType` beside the other type-specialization path. | Landed; verify call and specialization coverage. | Medium |
| `internal/driver/normalize.go` | Separate importer/AST records, C type parsing (`parseCType` and resolution), macro/scalar mapping, and final ordering/deduplication. | Landed; verify target mappings and deterministic generated bindings. | Medium-high |
| `checker/operator_checking.go` | Move `foldUnary`, `foldBinary`, and related constant-folding helpers to `constant_folding.go`; keep operator checking and type eligibility together. | Landed; verify constant-folding results. | Medium |

Test-file candidates are lower risk but still require a no-case-lost inventory:

| Current file | Candidate split | Caution |
| --- | --- | --- |
| `generator/generator_test.go` | Group declarations, unions, rendering, and operation/ring tests into facet files. | Landed; verify no test function, case, or fixture was lost. |
| `parser/parser_test.go` | Group declaration, expression, and recovery tests. | Landed; verify each source and expected diagnostic. |
| `tests/integration/pointers_test.go` | Move heap-free and cross-allocator cases to the dedicated heap-free test file; keep pointer syntax cases in `pointers_test.go`. | Landed; rename the still-provenance-based test under C9 and verify its roughly 360-line case set. |

Do not split `lexer.go` solely for size: scanning is one linear responsibility.
`generator/concurrency.go` is one coherent domain despite several internal
phases. Delete the dead checker dispatch helpers before considering a
`checker.go` split. Keep the C23 `fixtures_test.go` catalog together unless
retrieval or test ownership actually degrades. These are scope decisions, not
claims that their line counts are ideal.

For the landed split, compare pre/post test-function inventories and generated
hashes, run `gofmt` and the ordinary full suite, and inspect responsibility
owners. Do not create a second version of a file that `30d576a` already added.
Tagged generated-C coverage is a separate gate when emission semantics change;
a pure relocation must not change any artifact hash.

## First-wave mechanical changes

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

Rename `TestHeapFreeRejectsRFCBoundaries` in the integration heap-free tests to
`TestHeapFreeRejectsInvalidStorageAndRepeatedRelease` or another equally
specific behavior name. Do not change its cases or assertions.

## Implementation plan

Execute the phases in order when they touch the same compiler path; independent
documentation and benchmark work may proceed after its baseline is pinned.
Each finding ID must close with the corresponding Validation item, including
an explicit evidence-backed no-change result where a probe disproves a
candidate or a measured optimization misses its value gate. Keep one phase's
generated-C manifest change separate from the next phase's; never refresh
hashes merely because they failed. The split work in `30d576a` is existing
work to verify, not a request to move the same declarations again.

### Phase 0: rebaseline the live checkout

1. Record the current commit, `git status --short`, all in-progress edits,
   `gofmt -l .`, `go test ./...`, `go vet ./...`, the snippet-manifest bytes,
   and the current catalog membership. Preserve unrelated changes; do not
   reset, overwrite, or absorb another agent's work.
2. Re-run the five A-series probes and the C4 returned-slice mutation probe
   against this exact baseline. Record pass/fail and generated excerpts, not
   old line numbers. If a committed fix already closed a finding, validate it
   and mark that ID complete rather than re-implementing it.
3. Record a fixed-input benchmark corpus for B9/C7 before any optimization.
   The historical 80,192-allocation number is not a comparable baseline for
   the current 160-snippet catalog.

### Phase 1: first-wave behavior-preserving work (C3, C7 index, C8, C9 test)

Complete each step before starting the next so an unexpected output change
has one owner. This phase contains the four mechanical changes specified
above; it does not close the other findings by itself.

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
   rewrite the 360-line test during this first-wave step.
8. **Close the phase.** Run the Phase 1 portions of Validation. Compare the
   manifest byte-for-byte and inspect the final diff for unrelated file moves
   or formatting churn. Read the affected contracts in `docs/reference.md`;
   record why no normative edit is needed for these four mechanical changes.
   The verified reference contradictions are corrected in Phase 10. Report
   any pre-existing failures separately.

### Phase 2: verify the landed structural splits (C1)

Commit `30d576a` moved the responsibilities and test facets listed in the
Structural split inventory. Verify that commit against its parent and the
current tree. Do not move a declaration twice or count a move as a reduction
in function complexity.

1. Compare the old and new declaration inventories for scope, type identity,
   generator rendering/validation/emission, generics, C normalization, and
   constant folding. Every moved declaration has exactly one new owner and
   retains its visibility and package. Do not reintroduce one merely because
   its old file became shorter.
2. Compare pre/post test-function names, fixture sources, case counts, and
   assertions for generator, parser, and heap-free integration tests. The
   provenance-based heap-free test rename remains Phase 1 work; verify its
   cases survived the move.
3. Run the ordinary suite, vet, grammar and dispatch-coverage guards, and a
   byte comparison of the existing snippet manifest. A pure move must not
   change any generated artifact hash or diagnostic string. Investigate and
   repair a regression before treating C1 as complete.
4. Re-measure the three named complexity hotspots. If the file split only
   relocated a giant function, extract a focused helper only where the
   measured responsibility and branch structure improve; preserve every
   diagnostic and generated byte. This is separate from A3/B4/B5 behavior
   work even if it touches a nearby function.

### Phase 3: deterministic diagnostics and foreign includes (A1, A2)

1. In the root-call capture check, iterate capture names in lexical order and
   report the first name that is not initialized. Do not sort the entire
   diagnostic stream or change which root calls are valid. Add the two-capture
   source from the audit as an exact-message repeated-compilation test.
2. Replace `foreignIndex.symbols[CName]` with an index keyed by the checked
   defining module identity and C name. Thread graph/module identity into
   index construction; `node.Module` selects the defining module for a
   cross-module reference. Local declarations already bring their own module
   header and keep that behavior. Two modules may bind the same C symbol from
   different headers; a use of `A.c_add` requires A's header, a use of
   `B.c_add` requires B's, and use of both requires both in deterministic
   first-use order. Do not add a global symbol-conflict diagnostic.
3. Probe function, constant, and global references with the same C spelling
   in separate modules. Leave record lookup by canonical foreign-record type
   unless a separate record collision is reproduced; do not conflate it with
   the symbol bug. Update the now-false map-order comment in `foreign.go`.

### Phase 4: generated-C ordering and host bytes (A3, A4)

1. Compare every checked expression kind with `expressionMayObserve` and the
   generated renderer. Mark `CorelibCallExpression`,
   `StringFromRunesExpression`, and `VolatileReadExpression` observable.
   Preserve conditional/short-circuit guards while hoisting observable
   siblings in written order. Add a text assertion that the two
   `Prog.available_parallelism()` calls become two ordered temporaries;
   add a runtime fixture with conflicting effects if one can be expressed
   safely. Do not claim undefined behavior from the current text probe alone.
2. Pin only `compiler/corelib/runtime/*.c` and any verified sibling embedded
   runtime templates to LF in `.gitattributes`. Normalize the affected
   working-tree template bytes through the repository's approved mechanical
   rewrite route. Confirm the embedded `program.c` and `entropy.c` output
   contains no CRLF. Review changed snippet hashes as a deliberate
   cross-host output correction, not a blanket baseline refresh.

### Phase 5: built-in type identity and registry invariants (A5, B2)

1. Trace construction, field lookup, equality, and C lowering for every
   package-level builtin field containing a constructed type. Bind its
   checked field type through the current compilation arena before returning
   it from a read or matching a constructor initializer. An ordinary
   `List<String>` and `ProcessOptions.arguments` must share one canonical
   identity in that compilation; no global identity may bypass the arena.
2. Inventory the intended domains of `builtinTypes`, protected names,
   `ResolveSpecID`, and type facts. Write the relation each actually promises:
   a protected constructor name need not be a scalar `Type`, while every
   resolvable builtin identity needs its intended facts. Add a focused
   invariant test derived from existing registration data, not a copied
   second list. Correct comments that overstate a registry's domain.

### Phase 6: dead code, dispatch, and validator state (B1, B3, B4)

1. Re-run declaration/caller searches after `30d576a`. Delete each still
   uncalled private candidate in B1, plus comments and tests that exist only
   for that candidate. Review exported candidates individually for consumers
   outside this repository; lack of a local caller alone does not authorize
   deletion. Preserve `legacyUTF8Valid` as a test oracle.
2. Extend the existing AST/dispatcher guard pattern to exhaustive type,
   top-level, match-pattern, and extern-declaration dispatch. Record which
   recursive walkers intentionally skip forms. An unsupported checked kind
   reaching `resolveTypeUse` is a compiler `Unknown Error`, not a user
   `Type Error`; existing invalid user syntax keeps its earliest diagnostic.
3. Inventory every production `expressionValidation` constructor. Use one
   initializer that creates all maps required by production rendering and
   validation; migrate partial construction sites first, then remove only
   guards proved redundant on every path. Keep test-only malformed-state
   fixtures explicit. Coordinate with the landed RFC 0239 scope invariant;
   do not weaken fail-closed behavior to reduce line count.

### Phase 7: diagnostic and error representation (B5, B6, C6)

1. Inventory direct generator `Diagnostic{}` construction. Where a checked
   node carries a source span and the compilation table can resolve it,
   construct the diagnostic through the shared source-aware path. Preserve
   zero-span whole-compilation/compiler-invariant failures; do not invent a
   location. Keep wording owned by its phase.
2. Migrate source diagnostics to one authoritative location representation:
   `Span` plus a `span.Position` resolved at construction. Replace the two
   free-standing `Line` and `Column` fields only after every producer and
   consumer uses the position field. For each source-anchored diagnostic,
   assert the cached position equals `Table.Position(Span)`; preserve exact
   rendered module, line, and column. Whole-compilation diagnostics have a
   zero span and zero position. Update exported Go API documentation for the
   intentional field migration.
3. Replace the manifest decoder's `err.Error() == "EOF"` with
   `errors.Is(err, io.EOF)`, including wrapped-EOF and malformed-input tests.
   Add one explicit compiler-defect boolean to `CompilationResult`, derived
   from structured `UnknownError` diagnostics before rendering. Have the
   driver use that bit to attach the Hexal version rather than searching
   `Stderr` strings; keep ordinary rendered messages unchanged.

### Phase 8: focused API and maintenance cleanup (C2, C3, C4, C9)

1. Inventory repeated parameter tuples. Bundle one group only when at least
   three call sites share the same coherent state owner and the struct makes
   the call clearer; otherwise record why no bundle is useful. Do not create
   an all-purpose context or hide a phase dependency.
2. Change `ForeignRecordMembers` to return a defensive member-slice copy and
   assert a caller cannot mutate the stored record through the returned
   slice. Probe the other reported accessors and `CommandResult` paths with
   actual mutation/reuse; copy only where a reachable alias is demonstrated.
3. Compare the remaining test helpers by input, expected outputs, package,
   and execution lifecycle. Merge only exact same-package equivalents; keep
   distinct C23 and pure-Go harnesses distinct. Preserve every rejection
   assertion and fixture case.
4. Rename `CompilationStats.PixelSubtotal` to `PhaseSubtotal` and workbench
   JSON `pixelSubtotalMs` to `phaseSubtotalMs`. Update tests, consumers, and
   exported documentation in one change. This is an intentional Go/JSON API
   break, not a silent alias that keeps two names. The private integration
   test rename is performed in Phase 1.

### Phase 9: measured hot paths and benchmark record (B9, C7 remainder)

1. Rebuild a benchmark record on the *same fixed source set* before and after
   any optimization. Record Go version, target, catalog identity, time,
   bytes, and allocations. Keep the old 80,192 figure labelled as historical;
   do not call a different corpus a 3.16x regression.
2. Profile the driver path that calls `DiscoverCImports` then `Compile` on a
   small source and a module-heavy source. If duplicated parsing consumes at
   least 10% of median end-to-end build time and at least 5 ms per build on
   the representative project, design one reusable prepared in-memory state
   that the driver can pass through without filesystem access or a second
   module-resolution authority. Otherwise record a measured no-change
   disposition; a new public session API is not justified by mere duplicate
   work in source reading.
3. Apply the same measurement rule to proposed template, lexer, arena, and
   flow-clone hot paths. Optimize one proven bottleneck at a time, retaining
   generated bytes and diagnostics. Use repeated benchmark comparison rather
   than a one-iteration speed claim.

### Phase 10: canonical documentation and lifecycle (B7, B8)

1. Once behavior and public API settle, reconcile `docs/reference.md`'s
   Language boundary and Excluded features with its implemented C interop,
   `unsafe`/pointer, File, and socket sections. The correction states current
   rules only; it does not introduce syntax or pull a deferred capability
   into the language. Check grammar if any syntax rule is touched (none is
   intended). This RFC's explicit all-findings scope includes this edit.
2. Reconcile `docs/status.md` with actual spec states. Remove deferred 0209
   from implementation-ready work; move terminal and deferred specs to their
   required directories without editing terminal file contents. Reproduce
   the still-open interpolation bug and assign an active owning spec, or
   remove the bug row only if a negative probe proves it closed. Remove
   completed-work narrative from the open-work board without changing the
   historical claims inside immutable archived specs.
3. Update the status entry for this RFC as phases complete; close and archive
   this RFC only after every Validation item below has an implementation or
   recorded no-change disposition. Do not claim a full C23 or cross-target
   run that was not executed.

### Phase 11: final conformance

Run the complete Validation section, including ordinary tests, vet, grammar,
snippet-manifest comparison, generated-C text assertions, and the tagged
Clang fixtures affected by A2-A5. Rebuild and restart the workbench through
`hexal play` after the code work, because its stats JSON changed. Report
which target/toolchain lanes ran and which did not. No cleanup phase may
regenerate the manifest to conceal an unexplained output change.

## Validation

This section is the complete definition of done for the full refactoring
program. Every finding ID in the inventory above appears below. A suspected
risk or measured optimization may close without a code change only when its
named probe and evidence are recorded as a negative/no-benefit result. The
first-wave unchanged-output assertions apply to Phase 1 and the move-only
verification, **not** to subsequent correctness fixes.

- `gofmt -l .` is empty; `git diff --check` has no finding; Go line endings
  remain LF. No unrelated formatting churn appears.
- `go test ./...` and `go vet ./...` pass.
- During Phase 1, both `DiscoverCImports` and `Compile` produce the same
  requests, files, dependencies, exit status, and diagnostics as the
  pre-change baseline for every existing fixture. New focused cases cover shuffled source-map
  insertion, missing imports, malformed colliding keys, reserved stdlib keys,
  and prepared C bindings.
- The existing integration determinism tests pass. Identical source maps with
  different insertion orders emit byte-identical artifacts and ordered
  diagnostic strings.
- After Phase 1 and the verification of `30d576a`, the entire existing
  snippet manifest is byte-identical. Later A-series corrections may change
  only explained artifact hashes; review each phase's diff and regenerate
  only for a legitimate output change, never to make a failure disappear.
- The renamed integration test has the same source cases and assertions and
  contains no spec provenance in its test name.
- The tagged C23 package type-checks with `go vet -tags c23 ./...`, and
  affected qualified-profile fixtures compile and run under Clang. Record
  exactly which fixtures ran; a full tagged-suite claim requires a separate
  full tagged run.
- Phase 1 changes no public compiler signature, Hexal diagnostic, or
  generated C artifact. Subsequent phase changes match the explicit rules
  and tests below.

### Per-finding gates

| ID | Required result |
| --- | --- |
| A1 | The two-uninitialized-capture program produces one exact, lexically first capture diagnostic across at least 100 compilations; existing single-capture diagnostics remain unchanged. |
| A2 | With the same C symbol declared from `a.h` and `b.h`, a use through A includes `a.h`, a use through B includes `b.h`, and both uses include each header exactly once in first-use order across repeated compilations. Function, constant, and global paths are covered. |
| A3 | Generated C evaluates sibling `CorelibCallExpression`, `StringFromRunesExpression`, and `VolatileReadExpression` in source order; the two `Prog.available_parallelism()` calls get ordered temporaries. A guard enumerates every checked expression kind and classifies its observability. |
| A4 | `git ls-files --eol` shows LF in index and working tree for embedded runtime C templates on the validating checkout; selected generated `hexal/program.c` and `hexal/entropy.c` contain no CRLF. Any hash movement is limited to those affected artifacts and explained. |
| A5 | `options.arguments == arguments` compiles after initializing `ProcessOptions.arguments` from the ordinary `List<String>`. Other builtin aggregate fields with constructed types pass the same canonical-identity check. Affected generated C compiles in the tagged lane. |
| B1 | Every named private candidate is either absent with no callers or retained with a demonstrated caller/reason. Exported symbols are not deleted solely because this repository lacks callers; `legacyUTF8Valid` remains the test oracle. |
| B2 | A test covers the intended builtin-type, protected-name, spec-ID, and type-fact relations, including a deliberately missing registration that fails the guard; no second hand-maintained builtin list is introduced. |
| B3 | Exhaustive type, top-level, match-pattern, and extern dispatch guards cover all concrete kinds. Intentional no-op walkers are documented as such. An unsupported checked type expression reports `Unknown Error`; ordinary invalid source retains its earlier diagnostic. |
| B4 | Every production `expressionValidation` state is created through the total initializer. Removed nil guards are covered by existing or focused tests, including formerly partial paths; no panic replaces a diagnostic. |
| B5 | Every audited generator diagnostic with an available checked span reports the correct logical source and line/column. Whole-compilation failures retain no invented source position; phase-local wording is preserved except where another named fix changes it. |
| B6 | Wrapped `io.EOF` terminates manifest decoding normally; a malformed token reports a malformed-manifest error. The driver distinguishes a structured compiler defect from an ordinary Hexal diagnostic without searching rendered `Stderr`, and only the defect message carries the version. |
| B7 | `docs/reference.md` no longer says implemented C interop, `unsafe`/pointer operations, File, or sockets are absent. Its single authoritative rule for each remains consistent with code, tests, and `GRAMMAR.ebnf`. |
| B8 | `docs/status.md` contains only open work, deferred 0209 is not marked implementation-ready, each open bug names an active owner, terminal/deferred specs occupy the correct directory, and archived contents remain unchanged. Historical 140-snippet results are not rewritten as current 160-snippet results. |
| B9 | `docs/benchmarks.md` records a current fixed-input corpus and Go/target metadata, labels the old allocation figure historical, and makes no unsupported regression claim. Any implemented optimization has repeated before/after measurements on identical inputs. |
| C1 | Commit `30d576a` passes a declaration-owner and test-case inventory, ordinary tests/vet, dispatch guards, and an unchanged manifest. Any later giant-function extraction reduces measured responsibility/complexity without changing generated bytes or diagnostics. |
| C2 | A repeated parameter group has one focused owner and at least three call sites, with unchanged call results; or the audit records why no such bundle improves clarity and makes no change. |
| C3 | The same-package tagged compile assertion has one implementation, the provenance test has a behavior name, all original fixture/test cases remain, and cross-lifecycle helpers are not merged without equivalence evidence. |
| C4 | Mutating a slice returned by `ForeignRecordMembers` cannot mutate the stored record. Each other reported accessor/result path has a caller mutation probe and either a defensive fix or a recorded negative result. |
| C6 | All source-anchored diagnostics carry a span and derived `span.Position` that agree under the compilation's source table; whole-compilation diagnostics have neither location. Legacy free-standing `Line`/`Column` fields are removed only after all producers/consumers migrate, and existing rendered locations remain byte-identical except named bug fixes. |
| C7 | Source-key indexing is covered by the Phase 1 collision/reserved/missing cases and removes the O(reachable modules x supplied keys) scan. The parse-sharing probe reports its fraction of end-to-end time and absolute latency; a below-threshold result closes with no new session API, while an implemented reuse path preserves `Compile`/`DiscoverCImports` results and the in-memory boundary. |
| C8 | `reachState.record` delegates to `recordCategory`; exact module, position, category, stage, wording, and append order remain unchanged. |
| C9 | Go callers use `PhaseSubtotal`, workbench JSON uses `phaseSubtotalMs`, old names are absent, the value remains Lex+Check+Generate duration, and the renamed test retains all cases. The workbench is rebuilt and restarted through `hexal play` before handoff. |

## Boundaries and completion

This RFC does not add central diagnostic wording or stable keys (RFC 0243), an
incremental compiler (deferred RFC 0232), content-addressed caching, a new
analyzer phase, or a global C-symbol conflict rule. It does not move exported
implementation packages under `internal/`; that finding was explicitly
removed. It does not optimize a hot path merely because a profile mentions
it. No source-file or host-filesystem access enters the core compiler.

Each phase is independently reviewable. A correctness fix with a deliberate
generated-C change records the exact artifact families that moved; a
move-only or formatting phase keeps the manifest unchanged. A negative probe
or below-threshold benchmark closes a conditional finding only when its
evidence is recorded in the phase result. The RFC closes only when all A-,
B-, and C-series rows above have a corresponding per-finding Validation
result, the canonical reference is synchronized, and no required work remains.
