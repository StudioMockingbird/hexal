# ADR 0256: Validation Scopes and Parallel C23 Fixtures

- Kind: Architecture Decision Record (ADR)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-30
- Updated: 2026-09-30
- Scope: `compiler/tests/c23validation`, its documented commands, and the
  testing guidance in `AGENTS.md`
- Owner of the package: closed RFC 0125 (external C23 validation); this ADR
  changes how that package is run, not what it asserts
- Depends on: closed RFCs 0233 and 0234; their Linux/WSL qualification is
  complete. The smoke set includes their JSON and regex fixtures.
- Coordinates with: deferred ADR 0164 (content-addressed C object cache)
- Does not update `docs/reference.md`: no Hexal syntax, semantics, generated
  artifact, runtime behavior, or target support changes

## Summary

Keep the exhaustive C23 gate exactly as it is, but stop using it as the
inner implementation loop:

1. **Four validation scopes**: ordinary (pure Go), focused (named
   fixtures), short (an eight-fixture smoke set), and exhaustive (unchanged).
2. **Parallel fixture subtests** in the five fixture runners, bounded by Go's
   own `go test -parallel`, with the value chosen by measurement.

"Scope" here is distinct from the existing *tiers* of `TestC23Suite`
(compile, run, trap per fixture), which this ADR does not change.

No fixture, snippet, build mode, target-profile run, trap assertion, UBSan
run, or LeakSanitizer run is removed from the exhaustive gate.

## Evidence

Verified against the tree on 2026-09-30:

- `fixtureCatalog` has 165 fixtures; the snippet manifest has 161 snippets in
  17 categories; the package has 20 top-level tests.
- Only the two snippet catalog loops call `t.Parallel()`
  (`runner_test.go:84`, `modes_test.go:267`). The fixture loops of
  `TestC23Suite`, `TestC23SuiteQualifiedProfile`, `TestReleaseLaneFixtures`,
  `TestC23SuiteUBSan`, and `TestC23SuiteLeak` are sequential.
- Build ceiling: 165 fixtures x 4 builds (host-neutral, explicit profile,
  debug, release) = 660, plus 161 snippets x 3 = 483; 1,143 Clang
  compile/link operations before cache hits.
- The compile cache is keyed by artifact hash, toolchain, flags, and the
  owning top-level test's `buildRoot` (a `t.TempDir()`), and deduplicates
  concurrent callers of one key with `sync.Once`
  (`c23_harness_test.go:111-135`). It is already safe for parallel callers.
  Parent `t.TempDir()` cleanup runs only after its parallel subtests finish.
- UBSan and LeakSanitizer lanes skip on Windows (`ubsan_test.go:142`,
  `leak_test.go:45`), so a Windows full run contains no sanitizer lanes.
- `runProcess` sets no working directory (`c23_harness_test.go:64`): every
  generated program runs in the package source directory. Today only a
  compile-only fixture opens relative files, and the two fixtures binding
  fixed TCP ports use distinct ports (18734, 18744).

Historical motivation, not a baseline: one Windows run of the full package
(`go test -tags c23 ./compiler/tests/c23validation -count=1 -timeout=30m`) took
1418.334 s. It did not record the host metadata needed to reproduce it, so
Phase 0 replaces it.

## Decisions

### Validation scopes

**Ordinary**: `go test ./...`, `go vet ./...`, `go build ./...`. No C
toolchain. The default after every edit.

**Focused**: during work on one feature, run only the lanes and fixtures that
can observe it. Anchor every level, because `-run` matches each
slash-separated level as an unanchored pattern (without `$`,
`^(TestC23Suite)` also matches `TestC23SuiteUBSan`):

```text
go test -tags c23 ./compiler/tests/c23validation -run '^(TestC23Suite|TestReleaseLaneFixtures|TestC23SuiteQualifiedProfile)$/^(json-.*|regex-.*)$'
```

**Short**: `go test -short -tags c23 ./compiler/tests/c23validation`. It is a
smoke gate, never evidence for closing a spec or qualifying a release. The
fixture set is exactly:

| Fixture | Contract represented |
| --- | --- |
| `list-compiles` | compile-only generated-C acceptance |
| `list-runs` | successful execution; mimalloc-backed allocation |
| `division-by-zero-traps` | exact runtime-trap behavior |
| `concurrency-spawn-channel-runs` | libuv-linked concurrency runtime |
| `normalize-runs` | utf8proc-linked Unicode runtime |
| `json-reader-writer-conformance-runs` | yyjson-linked JSON adapter |
| `regex-unicode-byte-spans-and-captures-runs` | PCRE2-linked regex adapter |
| `foreign-call-runs` | foreign C declaration and call boundary |

The list is test-harness policy, one ordered slice beside the runners.
`TestCatalogIsWellFormed` gains a subtest checking that every name exists in
`fixtureCatalog` exactly once, that every one applies to the running host, and
that their compilation results together demand all five `RuntimeDependency`
values.

Under `testing.Short()`, top-level tests behave as follows:

| Top-level test | Short behavior |
| --- | --- |
| `TestC23Suite`, `TestC23SuiteQualifiedProfile`, `TestReleaseLaneFixtures` | the eight smoke fixtures only |
| `TestC23SnippetCatalogCompiles`, `TestReleaseLaneSnippetCatalog`, `TestC23SuiteUBSan`, `TestC23SuiteLeak` | `t.Skip` with an explicit short-mode reason |
| `TestLazyTraversalPipelinesRun`, `TestLazyTraversalPipelineEffectOrderRuns`, `TestPipelineReturnRunsErrorDefers`, `TestPipelineEvaluatesSourceCallbacksAndTerminalArgumentsOnce`, `TestPipelineForPreservesBreakContinueAndDeferredCleanup`, `TestProgramAndEntropyMeasurements`, `TestReleaseExecutablesAreSmallerAndUndebuggable`, `TestFloatingOutputIsModeIndependent` | unchanged; they build and run C (about 15 builds) and count toward the short budget |
| `TestCatalogIsWellFormed`, `TestTrapInventoryIsFullyClassified`, `TestTrapInventoryExecutableFixturesExist`, `TestTrapInventoryGuardRejectsUnclassifiedLiteral`, `TestModeCacheKeysDoNotCollideWithTheTieredHarness` | unchanged; no C build |

Short mode changes selection only, never assertions. Without `-short`,
selection is byte-for-byte independent of the smoke list.

**Exhaustive**: the authoritative gate, unchanged in content:

```text
go test -count=1 -timeout=60m -tags c23 ./compiler/tests/c23validation
```

The timeout is a hang backstop, not a performance target: 60 minutes
leaves room for a Linux run, which adds the sanitizer lanes absent from the
Windows figure. A spec that changes generated C, runtime behavior, target
lowering, native dependencies, or driver modes closes only after this gate
passes on every target that spec requires.

### Parallel fixtures through `go test -parallel`

- The fixture subtests of the five runners above call `t.Parallel()`.
  Top-level tests stay sequential, so lanes never overlap one another.
- Concurrency is bounded by Go's own `-parallel` (default `GOMAXPROCS`). It
  bounds whole subtests, both the Clang build and the executed program, so
  build processes and generated programs share one limit. No custom flag,
  semaphore, or `TestMain` is added.
- `runProcess` sets `command.Dir` to a fresh `t.TempDir()` of the calling
  subtest. Parallel programs then never share a working directory or write
  into the source tree.
- `TestCatalogIsWellFormed` gains a subtest rejecting two fixtures whose
  sources bind the same literal loopback port, so parallel network fixtures
  cannot collide.
- Loops need no loop-variable shadowing: the module's Go version gives each
  iteration its own variable.

### Choosing `-parallel`

Measure on the workload the setting governs, the fixture lanes:

```text
go test -count=1 -tags c23 -run '^TestC23Suite$' ./compiler/tests/c23validation -parallel=N
```

- Candidates: `1`, `2`, `4`, `8`, and the default (omitted). Candidates above
  `GOMAXPROCS` are valid; subtests mostly wait on external processes.
- Three runs per candidate after one unrecorded warm-up. Record each wall
  time and the median, plus host CPU, cores/threads, `GOMAXPROCS`, RAM, OS,
  Clang identity, and target profile.
- Select the smallest candidate whose median is within 10 percent of the
  fastest. If the default qualifies, documented commands omit `-parallel`. A
  candidate that times out or fails a test is ineligible.
- Windows and WSL are measured separately. If they select different values,
  `AGENTS.md` gives one command per host.
- The record lives in this ADR's Implementation state, and `AGENTS.md`
  states the chosen value. Remeasure when the fixture count changes by 25
  percent or more, the supported Clang major version changes, or a new host
  family is qualified.

### Deferred

- **CI sharding** (shard expressions, a test-name partition guard, and an
  `AGENTS.md` text guard). The repository has no CI configuration, and
  shards give no local benefit. Write it as its own spec when CI exists.
- **A custom Clang-only worker limit** that keeps compiling while programs
  run. Add it only if the `-parallel` measurement shows the CPU idle during
  program execution.
- **Cross-lane or persistent executable reuse.** Profile, mode, and sanitizer
  builds differ, and temporary directories have per-lane lifetimes.
  Per-translation-unit object reuse belongs to ADR 0164.

### Performance acceptance

Closure requires the functional work and measured evidence, not a fixed
speed-up. The fixture lanes are the part this ADR parallelizes; the snippet
lanes already were, so the full-package gain is bounded by the fixture share
of the Phase 0 per-test times.

- **Required:** the exhaustive median does not exceed the Phase 0 median; the
  `TestC23Suite` lane median is lower than its Phase 0 median; the short gate
  completes cold in at most 180 seconds.
- **Target, reported but not blocking:** the exhaustive median is at most 70
  percent of the Phase 0 median. A miss is recorded with the per-test
  breakdown and points to ADR 0164's object reuse as the next lever.

## Implementation plan

All paths are under `compiler/tests/c23validation/`. Each phase leaves the
ordinary gate (`go test ./...`, `go vet ./...`, `go build ./...`) green.

### Phase 0: reproducible baseline (no code change)

1. Record fixture, snippet, category, and top-level test counts in
   Implementation state.
2. On Windows, run the exhaustive command three times after one unrecorded
   warm-up, each with `-json` redirected under `.tmp/`. From each run,
   extract every top-level test's elapsed time (the `pass`/`fail` event's
   `Elapsed` for that test). Record the three total wall times, their
   median, the per-test medians, and the host metadata listed under Choosing
   `-parallel`. Delete the `.tmp/` captures afterwards.
3. "Cold" means a fresh `go test` process with `-count=1`. The OS file cache
   and antivirus scanning of freshly linked executables are not controlled;
   note them as confounders in the record.
4. Repeat steps 2-3 on WSL. RFC 0233 and RFC 0234 Linux qualification is now
   complete, so this baseline can include their JSON and regex fixtures.

### Phase 1: scopes

1. `catalog_test.go`: add `var smokeFixtures = []string{...}` holding the
   eight names in table order, and one helper beside `appliesToHost`:

   ```go
   // inScope reports whether f runs in this invocation: every fixture
   // normally, only the smoke set under -short.
   func (f fixture) inScope() bool {
       return !testing.Short() || slices.Contains(smokeFixtures, f.name)
   }
   ```

2. Call `f.inScope()` next to the existing `appliesToHost()` filter in the
   three fixture loops: `runner_test.go:42` (`TestC23Suite`),
   `target_qualification_test.go:59` (`TestC23SuiteQualifiedProfile`), and
   `modes_test.go:239` (`TestReleaseLaneFixtures`).
3. Add `if testing.Short() { t.Skip("short mode: smoke fixtures only; run without -short for the exhaustive gate") }`
   as the first statement of `TestC23SnippetCatalogCompiles`,
   `TestReleaseLaneSnippetCatalog`, `TestC23SuiteUBSan`, and
   `TestC23SuiteLeak`.
4. `TestCatalogIsWellFormed`: add subtest `smoke set` that fails when a
   smoke name is absent from `fixtureCatalog`, duplicated in
   `smokeFixtures`, or not `appliesToHost()`. It then compiles the eight with
   `f.resolve(t)` (pure Go) and fails unless the union of their
   `Dependencies` contains all five `compiler.RuntimeDependency` values.
5. `AGENTS.md` Testing: replace the tagged-lane guidance with the four
   scopes, one command each (ordinary, the focused example, short,
   exhaustive). State that only the exhaustive gate on every required target
   closes a generated-C or runtime spec. Leave the `-parallel` value as a
   placeholder sentence until Phase 2 selects it.
6. Run the short command and the focused example once; confirm the focused
   example selects only JSON and regex subtests (`-v` output).

### Phase 2: parallel fixtures

1. `c23_harness_test.go` `runProcess`: set `command.Dir = t.TempDir()`
   before `command.Run()`. The executable path is already absolute under
   `buildRoot`.
2. `TestCatalogIsWellFormed`: add subtest `unique ports` that scans every
   fixture's Hexal sources, including snippet-backed fixtures' snippet
   sources, for loopback port literals (the `Net.parse_address("127.0.0.1", N)`
   form) and fails when two fixtures share one. Feed it one synthetic
   two-fixture catalog sharing a port to prove it rejects.
3. Add `t.Parallel()` as the first statement inside the `t.Run(f.name, ...)`
   closure of the five fixture loops: `runner_test.go:46`,
   `target_qualification_test.go:63`, `modes_test.go:243`,
   `ubsan_test.go:150`, and `leak_test.go:55`. Do not add it to any
   top-level test.
4. Run the exhaustive command once at the default `-parallel` to prove no
   ordering dependency, shared-directory, or port failure appears.
5. Run the `-parallel` candidate matrix on Windows, then WSL; record every
   figure in Implementation state and select per the rule.
6. `AGENTS.md`: replace the Phase 1 placeholder with the selected value
   (or the statement that the default is used), per host if they differ.

### Phase 3: measure and close

1. Run the short gate cold on each host; record elapsed time.
2. Run the exhaustive command three times per host with the selected
   `-parallel`, capturing `-json` as in Phase 0; record totals, medians, and
   per-test medians.
3. Check the Required items under Performance acceptance; report the
   Target.
4. Verify `docs/reference.md` needs no edit.
5. Remove this ADR's `docs/status.md` row, set Status to Closed, and move the
   file to `docs/specs/archived/`.

## Validation

This section is exhaustive.

Functional preservation:

- `go test ./...`, `go vet ./...`, and `go build ./...` pass without invoking
  a C compiler.
- The exhaustive command passes on each host with every pre-change fixture,
  snippet, mode, qualified-profile, UBSan, and LeakSanitizer case selected on
  its applicable host.
- No expectation, warning flag, toolchain flag, generated source, runtime-pack
  input, or assertion is weakened.
- `go test -race -short -tags c23 ./compiler/tests/c23validation` passes on
  Windows. The full parallel fixture matrix is not run under `-race`; its
  shared state is the already `sync.Once`-guarded compile cache.

Short scope:

- The short command passes on each host.
- The smoke subtest proves the eight names exist exactly once, apply to the
  host, and together demand mimalloc, libuv, utf8proc, yyjson, and PCRE2.
- Under `-short`, the three fixture runners run only those eight; the four
  skipped tests report an explicit short skip; the others run unchanged.
- Without `-short`, no skip occurs and selection ignores the smoke list.

Parallelism:

- Every fixture subtest of the five runners calls `t.Parallel()`; no
  top-level test does.
- Every generated program runs in its own temporary working directory.
- The port subtest rejects a synthetic catalog with two fixtures binding the
  same literal port.
- The documented focused example runs and selects only JSON and regex
  fixtures in the three named lanes.

Performance evidence:

- Phase 0 and Phase 3 records are complete for each host.
- Every Required item under Performance acceptance holds on each host; the
  70 percent Target is reported either way.

## Implementation state

Not started.
