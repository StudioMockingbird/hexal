# RFC 0140: C23 Catalog Sweep Performance

- Kind: Execution Plan
- Status: Implementation-ready; implementation not started
- Created: 2026-08-27
- Updated: 2026-08-27
- Implements: no language, checker, or generator change; a test-harness
  correctness and performance fix confined to `compiler/tests/c23validation`,
  plus one new race-safety test in `workbench/snippets`
- Coordinates with: RFC 0125 (external C23 validation; owns
  `compiler/tests/c23validation` and `TestC23SnippetCatalogCompiles`)
- Does not own: any generator or checker defect. If completing this sweep
  surfaces a real compile failure in some snippet, that failure gets its own
  RFC (or an existing owner); this RFC's job ends at making the sweep finish
  and report a real result.

## Summary

`TestC23SnippetCatalogCompiles` (`compiler/tests/c23validation/runner_test.go`)
Tier-1-compiles every workbench snippet (exactly 140, see Measured facts)
under all three real toolchains (GCC, Clang, `zig cc`) with no hand-listed
fixture per snippet. On the authoring host, a full run of this test did not
complete: it ran past 50 minutes, asserted zero failures, and was killed by
an external timeout. RFC 0131 (closed,
`docs/specs/archive/0131-generated-c-conformance-bug-sweep.md`) closed against
every *targeted* probe passing and explicitly left this catalog-wide sweep
"not independently re-confirmed." This RFC exists to make that sweep complete
in a reasonable, defined time so it can actually run to a real pass or fail.

Two review passes over the first draft of this RFC found that the obvious fix
(cache toolchain discovery, parallelize the snippet loop) is unsafe against
the *current* code as written: the discovery cache would silently corrupt
error reporting, and the existing compile cache has a genuine data race and a
use-after-cleanup bug that today's 140 snippets happen not to trigger. This
revision folds both reviews' verified findings into the plan below; every
claim in "Blocking correctness issues" was independently confirmed against
the source before being accepted.

## Measured facts

- The catalog contains exactly 140 snippets (verified by loading
  `workbench/snippets` and counting; not "approximately 150" as the first
  draft of this RFC said).
- All 140 compile through Hexal successfully today (verified: 0 failures).
- All 140 currently produce distinct `canonicalArtifactHash` values (verified:
  140 distinct hashes, 0 collisions) — the compile-cache race described below
  is real but not reachable through this test alone today; it would be
  reachable the moment two snippets (or a `TestC23Suite` fixture and a
  catalog snippet) happen to generate identical C, or the moment
  `TestC23Suite` and `TestC23SnippetCatalogCompiles` run in the same process
  and share a cache key (see "Blocking correctness issues" #3 below, which
  does not depend on a hash collision at all).
- A combined run of 3 snippets x 3 toolchains (9 real compiles plus 3 rounds
  of toolchain discovery) completed in 16.31s.
- A full run (140 snippets x 3 toolchains = 420 real compiles, plus up to 420
  redundant discovery subprocess spawns today) did not complete inside 50+
  minutes and was killed without asserting a single failure.
- `discoverAllToolchains` (`compiler/tests/c23validation/toolchain_test.go:206`)
  is called once per snippet inside `compileGeneratedC`
  (`c23_harness_test.go:210-217`), and is not memoized anywhere: each call
  re-runs `resolveExecutable`'s PATH/fallback-glob search and spawns a fresh
  `gcc --version` / `clang --version` / `zig version` subprocess
  (`toolchain_test.go:180`), regardless of whether a previous snippet in the
  same test run already resolved the identical toolchain. This repetition is
  presently *intentional*, per the existing comment at
  `toolchain_test.go:200-205`: a naive cache would report the failure clearly
  for the first fixture that hits a missing toolchain and confusingly (a
  zero-value toolchain) for every fixture after. Any fix must preserve that
  contract — see "Blocking correctness issues" #1.
- `TestC23SnippetCatalogCompiles`'s snippet loop (`runner_test.go:74-82`) has
  no `t.Parallel()` anywhere and runs strictly sequentially.
- `buildGeneratedC`'s compile cache (`c23_harness_test.go:136-165`) is keyed
  by `(artifactHash, toolchain, flags)` behind a mutex (`compileCacheMu`),
  but the mutex covers only the map lookup and the map write — the actual
  `os.MkdirAll`/`os.WriteFile`/`exec.Command` build runs *outside* the lock
  (`c23_harness_test.go:150-196`). Two callers that miss the cache for the
  same key run the build concurrently against the same directory and the
  same output path. See "Blocking correctness issues" #2.
- The cache key does not include `buildRoot`, but the cached executable path
  lives under it (`buildRoot/<hash>/<toolchain>/hexal.exe`), and `buildRoot`
  is a fresh `t.TempDir()` created separately by each top-level test
  (`TestC23Suite` and `TestC23SnippetCatalogCompiles` each call it once). Go
  deletes a `t.TempDir()` when the top-level test that created it returns. If
  both top-level tests run in the same process and a cache key collides
  across them, the second test can be handed a path already deleted by the
  first test's cleanup. See "Blocking correctness issues" #3.
- `buildGeneratedC`'s `exec.Command(tc.Command[0], args...)` (the actual
  compiler invocation) has no timeout of any kind: a hung `gcc`/`clang`/`zig`
  or linker process blocks forever, distinct from `runProcess`'s existing
  10-second bound on *running the generated binary*
  (`runProcessTimeout`, `c23_harness_test.go:50`), which does not cover
  compiling it. See "Blocking correctness issues" #4.
- No existing test establishes that concurrent `compiler.Compile` calls are
  race-free — `t.Parallel()` on the snippet loop would call it from multiple
  goroutines for the first time. A source audit of package-level mutable
  state in `compiler.Compile`'s call graph found exactly two mutated
  package-level variables (`panicSeam` in `compiler/compile.go`, mutated only
  by `compiler/compile_test.go`; `ringKeepEveryGrouping` in
  `compiler/generator/render.go`, mutated only by
  `compiler/generator/unsigned_ring_test.go`), both confined to their own
  package's own test binary and never reachable from
  `compiler/tests/c23validation`'s separate process. This is reassuring but
  not a substitute for an explicit, permanent regression test — see
  "Required changes" #4.

## Root cause (best current explanation, not yet proven sufficient)

1. Toolchain discovery — a filesystem search plus three external `--version`
   subprocess spawns — repeats on every one of 140 snippet subtests instead
   of once per test run, adding up to 420 extra subprocess spawns on top of
   the 420 real compiles.
2. The snippet loop is sequential, so none of this work overlaps even though
   the compile cache's *lookup* was already built to be concurrency-safe
   (its *build* step, as measured above, is not — see "Blocking correctness
   issues" #2).

This is a plausible, well-justified explanation given what is measured above,
not a confirmed root cause: the 3-snippet timing sample is too small to
attribute the full 50+-minute run to these two factors alone (linear scaling
of the 16.31s sample to 420 compiles projects roughly 13 minutes, well short
of 50+). Antivirus scanning of freshly written executables, `zig cc`'s own
cold-start cost, or disk I/O under `%TEMP%` are plausible contributors to the
remainder. The plan below re-measures after the two safe fixes land rather
than assuming they are sufficient, and names a concrete target so "re-measure
and see" has a pass/fail answer.

## Blocking correctness issues

These were not part of the first draft. Both independent reviews caught real
bugs in what looked like a purely additive perf change; each was confirmed
against the source before being accepted here.

1. **A naive `sync.Once` around `discoverAllToolchains` breaks error
   reporting.** `discoverToolchain` (`toolchain_test.go:167-198`) calls
   `t.Fatalf` directly. Wrapping the existing function in `sync.Once` means
   the *first* caller to hit a missing toolchain reports it correctly, but
   `sync.Once.Do` still marks itself complete even when its function calls
   `t.Fatalf` (which unwinds via `runtime.Goexit`, not a normal return) —
   `sync.Once`'s internal `doSlow` marks its `done` flag via a `defer`, so it
   fires regardless. Every later caller then receives a zero-value
   `toolchain{}` with no error at all, which is silently wrong rather than
   loudly wrong: exactly the failure mode the existing code comment says to
   avoid. The fix must cache a `(toolchain, toolchain, toolchain, error)`
   tuple from a version of discovery that takes no `*testing.T` and never
   calls `t.Fatalf` itself, and have the public wrapper — called by every
   subtest, cache hit or not — report the cached error through its own `t`.
2. **The compile cache has a check-then-act data race.** `buildGeneratedC`
   locks, checks the cache, unlocks, and only *then* builds
   (`c23_harness_test.go:142-196`); the build itself is not synchronized at
   all. Two subtests that miss the cache for the same key — not reachable
   through today's 140 snippets alone (all hashes are distinct), but
   directly reachable once `t.Parallel()` is added and any two builds ever
   share a key (a future snippet duplicate, or a `TestC23Suite` fixture that
   overlaps a catalog snippet) — will concurrently `os.MkdirAll` the same
   directory, `os.WriteFile` the same files, and `exec.Command` into the same
   output path. This is a latent bug in the current code, independent of
   parallelism; parallelism only makes it reachable. Fix before adding
   `t.Parallel()`, not after.
3. **The compile cache outlives the directory it points at.** The cache key
   excludes `buildRoot`, but `compileCache` is a package-level map shared by
   every top-level test in the process, while each top-level test's
   `buildRoot` is its own `t.TempDir()`, deleted when that top-level test
   returns. If `TestC23Suite` and `TestC23SnippetCatalogCompiles` both run in
   the same `go test` invocation (the normal case — neither is excluded by
   `-run` in ordinary use) and a cache key collides between them, the second
   test can receive a path already deleted by the first test's cleanup. This
   does not require a hash collision between two *catalog* snippets — a
   `TestC23Suite` fixture sourced from a catalog snippet, or one that happens
   to generate identical C, is enough. Fix: fold `buildRoot` into the cache
   key, so a cache entry can never outlive the `buildRoot` it names.
4. **Compiler subprocesses have no timeout.** A hung `gcc`, `clang`, `zig`,
   or linker invocation blocks its `exec.Command` call forever; the only
   thing that would eventually stop it is `go test`'s own outer `-timeout`,
   which kills the *entire test binary*, discarding every already-passing
   result. Add a bounded timeout scoped to one build, distinct from and in
   addition to the existing 10-second `runProcessTimeout` (which bounds
   running the generated binary, not compiling it).

None of these four require new dependencies or architectural change — each
is a same-file, same-function fix. See "Detailed implementation plan" for
the exact shape of each.

## Required changes

1. **Fix the toolchain discovery cache correctly** (Blocking issue #1):
   `sync.Once` around a `*testing.T`-free resolution function that returns
   `(toolchain, toolchain, toolchain, error)`; the existing public
   `discoverAllToolchains(t)` wrapper checks the cached error and calls
   `t.Fatal` on every call, cache hit or not. `TestC23Suite` and
   `TestC23SnippetCatalogCompiles` each resolve gcc/clang/zig exactly once
   *combined*, however many subtests either runs. The override environment
   variables (`HEXAL_GCC`/`HEXAL_CLANG`/`HEXAL_ZIG`) are read once, at first
   resolution, which already matches their intended per-process semantics —
   `go test -count=N` re-runs test functions within the same process and
   does not change environment variables between iterations, so this is not
   a behavior change for that flag.
2. **Fix the compile cache's two correctness bugs together** (Blocking
   issues #2 and #3): fold `buildRoot` into `compileCacheKey`, and replace
   the check-then-act lock pattern with a per-key `sync.Once` so exactly one
   caller performs a given build and every caller (that one included) reads
   the same published result.
3. **Add a bounded timeout to the compiler subprocess itself** (Blocking
   issue #4): wrap the build's `exec.Command` in `exec.CommandContext` with a
   `buildProcessTimeout` (2 minutes, matching the reviewed recommendation —
   long enough for a real C23 compile+link, short enough that one hang does
   not consume the whole run), returning an error naming the subtest, the
   toolchain, and the limit. `doBuild` has no `*testing.T`, but
   `buildGeneratedC` still does — pass `t.Name()` (the full subtest path,
   e.g. `TestC23SnippetCatalogCompiles/streams-seek-and-eos/gcc`) down as a
   plain string so the timeout error identifies the actual failing snippet
   or fixture, not just an opaque artifact hash.
4. **Add a permanent concurrent-`compiler.Compile` race regression test**,
   independent of any external toolchain, in `workbench/snippets` (alongside
   the existing `TestCatalogProgramsCompile`): compile every catalog snippet
   from its own goroutine and assert success, run under `go test -race`.
   This is the guard the source audit above could not fully replace — it
   catches a *future* package-level mutable variable this RFC's manual audit
   cannot.
5. **Parallelize `TestC23SnippetCatalogCompiles`'s snippet loop.** Call
   `t.Parallel()` at the top of each `t.Run(snippet.ID, ...)` closure. No
   loop-variable capture fix is needed: `go.mod` pins Go 1.26, and Go 1.22+
   gives each `for` iteration its own variable. Leave `TestC23Suite` (the
   hand-written fixture suite, with its Tier 2/3 run and exact-stdout
   comparison across toolchains) and the inner per-toolchain
   `t.Run(tc.Name, ...)` loop inside `compileGeneratedC` both sequential — a
   subtest that never calls `t.Parallel()` itself always runs synchronously
   within its parent, so the three toolchain builds for one snippet stay
   ordered relative to each other while different snippets overlap. Actual
   concurrency is bounded by `-parallel` (default `GOMAXPROCS`), not by the
   snippet count.
6. **Re-measure against a defined target, not a vague one.** See Validation.

## Detailed implementation plan

Ordered by dependency: each phase's fix is a prerequisite for the next
phase being safe, not just a nice sequencing.

1. **Fix toolchain discovery caching** (`toolchain_test.go`). Split
   `discoverToolchain` into a `*testing.T`-free `resolveToolchain(spec) (toolchain, error)`
   and keep a thin `discoverAllToolchains(t)` wrapper:

   ```go
   var (
       toolchainsOnce      sync.Once
       cachedGCC           toolchain
       cachedClang         toolchain
       cachedZig           toolchain
       cachedToolchainsErr error
   )

   // discoverAllToolchains resolves and version-checks all three required
   // toolchains exactly once per test binary run. resolveToolchain takes no
   // *testing.T so it is safe to run inside sync.Once; every caller here --
   // the one that ran Once.Do and every one that didn't -- reports the same
   // cached error by name through its own t, matching the original
   // per-fixture failure contract instead of the zero-value silently-wrong
   // one a naive sync.Once around the old t.Fatalf-calling function would
   // produce.
   func discoverAllToolchains(t *testing.T) (gcc, clang, zig toolchain) {
       t.Helper()
       toolchainsOnce.Do(func() {
           cachedGCC, cachedToolchainsErr = resolveToolchain(gccSpec)
           if cachedToolchainsErr == nil {
               cachedClang, cachedToolchainsErr = resolveToolchain(clangSpec)
           }
           if cachedToolchainsErr == nil {
               cachedZig, cachedToolchainsErr = resolveToolchain(zigSpec)
           }
       })
       if cachedToolchainsErr != nil {
           t.Fatal(cachedToolchainsErr)
       }
       return cachedGCC, cachedClang, cachedZig
   }

   // resolveToolchain is discoverToolchain's old body with every t.Fatalf
   // replaced by a returned error, so it can run inside sync.Once.
   func resolveToolchain(spec toolchainSpec) (toolchain, error) {
       path, err := resolveExecutable(spec)
       if err != nil {
           return toolchain{}, fmt.Errorf("c23 suite requires %s: %w", spec.name, err)
       }
       command := []string{path}
       if spec.subcommand != "" {
           command = append(command, spec.subcommand)
       }
       output, err := exec.Command(path, spec.versionArgs...).CombinedOutput()
       if err != nil {
           return toolchain{}, fmt.Errorf("%s found at %s but failed to report its version: %v\n%s", spec.name, path, err, output)
       }
       match := spec.versionPattern.FindStringSubmatch(string(output))
       if match == nil {
           return toolchain{}, fmt.Errorf("%s at %s produced an unrecognized version banner: %s", spec.name, path, output)
       }
       major, convErr := strconv.Atoi(match[1])
       if convErr != nil {
           return toolchain{}, fmt.Errorf("%s at %s produced a non-numeric version %q", spec.name, path, match[1])
       }
       if major < spec.minimumMajor {
           return toolchain{}, fmt.Errorf("%s at %s is version %d, below the required minimum %d", spec.name, path, major, spec.minimumMajor)
       }
       return toolchain{Name: spec.name, Command: command, Version: strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0]), DefaultTarget: spec.defaultTarget}, nil
   }
   ```

   `discoverToolchain` itself is deleted (no remaining caller). Verify:
   `go build -tags c23 ./...`, then a targeted run against a deliberately
   broken `HEXAL_GCC` override confirms the Fatal message still names the
   right tool on every subtest, not just the first.

2. **Fix the compile cache** (`c23_harness_test.go`). Add `buildRoot` to the
   key, switch to a per-key `sync.Once`, and split the build itself into a
   `*testing.T`-free function:

   ```go
   type compileCacheKey struct {
       artifactHash string
       toolchain    string
       flags        string
       buildRoot    string // scopes a cache entry to the top-level test that owns this buildRoot's t.TempDir() lifetime
   }

   type buildResult struct {
       exe string
       err error
   }

   type compileCacheEntry struct {
       once   sync.Once
       result buildResult
   }

   var (
       compileCacheMu sync.Mutex
       compileCache   = map[compileCacheKey]*compileCacheEntry{}
   )

   func buildGeneratedC(t *testing.T, tc toolchain, result compiler.CompilationResult, buildRoot string) string {
       t.Helper()
       flags := []string{"-std=c23", "-Wall", "-Wextra", "-Werror"}
       flags = append(flags, warningFlags...)
       key := compileCacheKey{artifactHash: canonicalArtifactHash(result.Files), toolchain: tc.Name, flags: strings.Join(flags, " "), buildRoot: buildRoot}

       compileCacheMu.Lock()
       entry, ok := compileCache[key]
       if !ok {
           entry = &compileCacheEntry{}
           compileCache[key] = entry
       }
       compileCacheMu.Unlock()

       // Exactly one caller per key runs doBuild; sync.Once.Do's own
       // happens-before guarantee means every other caller here sees the
       // fully-written entry.result with no further locking needed.
       entry.once.Do(func() {
           entry.result = doBuild(tc, result.Files, flags, buildRoot, key.artifactHash, t.Name())
       })
       if entry.result.err != nil {
           t.Fatalf("%s rejected generated C: %v", tc.Name, entry.result.err)
       }
       return entry.result.exe
   }

   // doBuild materializes one artifact set and compiles it under tc. It
   // takes no *testing.T: sync.Once may run it from any one of several
   // waiting subtests' goroutines, and only buildGeneratedC's own caller
   // should report failure through its own t. subtestName is t.Name() from
   // whichever caller happened to run the build (identifying, not
   // necessarily the specific caller that later reads the cached result --
   // an accepted imprecision of any dedup cache, same as the existing
   // cached-vs-fresh error message already was).
   func doBuild(tc toolchain, files map[string]string, flags []string, buildRoot, artifactHash, subtestName string) buildResult {
       dir := filepath.Join(buildRoot, artifactHash, tc.Name)
       if err := os.MkdirAll(dir, 0755); err != nil {
           return buildResult{err: err}
       }
       for name, content := range files {
           path := filepath.Join(dir, name)
           if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
               return buildResult{err: err}
           }
           if err := os.WriteFile(path, []byte(content), 0644); err != nil {
               return buildResult{err: err}
           }
       }
       exe := filepath.Join(dir, "hexal.exe")
       args := append([]string{}, tc.Command[1:]...)
       args = append(args, flags...)
       args = append(args, "-I", dir)
       names := make([]string, 0, len(files))
       for name := range files {
           names = append(names, name)
       }
       slices.Sort(names)
       for _, name := range names {
           if strings.HasSuffix(name, ".c") {
               args = append(args, filepath.Join(dir, name))
           }
       }
       if runtime.GOOS != "windows" && strings.Contains(strings.Join(names, " "), "concurrency.c") {
           args = append(args, "-lpthread")
       }
       args = append(args, "-o", exe)

       ctx, cancel := context.WithTimeout(context.Background(), buildProcessTimeout)
       defer cancel()
       output, err := exec.CommandContext(ctx, tc.Command[0], args...).CombinedOutput()
       if ctx.Err() == context.DeadlineExceeded {
           return buildResult{err: fmt.Errorf("%s did not finish within %s (artifact %s)", subtestName, buildProcessTimeout, artifactHash)}
       }
       if err != nil {
           return buildResult{err: fmt.Errorf("%v\n%s", err, output)}
       }
       return buildResult{exe: exe}
   }
   ```

   Add `const buildProcessTimeout = 2 * time.Minute` next to the existing
   `runProcessTimeout` constant, with a comment distinguishing it (compiling
   the artifact, not running it).

3. **Add the concurrent-compile race test** in
   `workbench/snippets/compile_test.go` (no `c23` build tag — this exercises
   only `compiler.Compile`, no external toolchain, so it belongs in the
   normal untagged gate):

   ```go
   // TestCatalogProgramsCompileConcurrently establishes that independent
   // compiler.Compile calls share no mutable state. Run under `go test
   // -race`, this is the prerequisite RFC 0140 needs before
   // compiler/tests/c23validation is allowed to call Compile from parallel
   // snippet subtests.
   func TestCatalogProgramsCompileConcurrently(t *testing.T) {
       catalog, err := snippets.Load()
       if err != nil {
           t.Fatal(err)
       }
       var wg sync.WaitGroup
       for _, category := range catalog {
           for _, snippet := range category.Snippets {
               wg.Add(1)
               go func(snippet snippets.Snippet) {
                   defer wg.Done()
                   // t.Errorf (never t.Fatalf) is the documented safe
                   // testing.T call from a non-test goroutine.
                   result := compiler.Compile(snippet.Sources, snippet.Entrypoint, compiler.Project{})
                   if result.ExitCode != compiler.ExitSuccess {
                       t.Errorf("snippet %s did not compile concurrently: %v", snippet.ID, result.Stderr)
                   }
               }(snippet)
           }
       }
       wg.Wait()
   }
   ```

4. **Parallelize the snippet loop** (`runner_test.go`):

   ```go
   func TestC23SnippetCatalogCompiles(t *testing.T) {
       buildRoot := t.TempDir()
       for _, snippet := range allSnippets(t) {
           snippet := snippet
           t.Run(snippet.ID, func(t *testing.T) {
               t.Parallel()
               result := assertCompilesSources(t, snippet.Sources, snippet.Entrypoint)
               compileGeneratedC(t, result, buildRoot)
           })
       }
   }
   ```

   (The `snippet := snippet` line is redundant under Go 1.26's per-iteration
   loop variables but costs nothing and removes any doubt for a future
   reader who doesn't have the Go version in mind; drop it if that redundancy
   is unwanted.) Do not add `t.Parallel()` inside `compileGeneratedC`'s inner
   `t.Run(tc.Name, ...)` loop — the three toolchain builds for one snippet
   stay sequential by simply not calling it there.

5. **Re-measure and record the result in this RFC** (see Validation). If the
   target is missed, add `t.Logf` timestamps around `doBuild`'s
   materialize/compile split and `discoverAllToolchains`' first resolution to
   attribute the remainder before proposing anything further — do not add
   more machinery speculatively.

## Deferred, not required for this RFC

Raised by review but not code this RFC should write:

- **Windows Defender / antivirus exclusion** for the `t.TempDir()` build
  tree, if step 5's re-measurement finds AV scanning is the dominant residual
  cost. This is a machine-configuration change (e.g.
  `Add-MpPreference -ExclusionPath`), not something a test should apply to
  itself — leaving it as an operator follow-up if measurement points there.
- **`%TEMP%` disk pressure** from up to `GOMAXPROCS` concurrent linker
  outputs (each artifact set is small, but not free) is a plausible future
  concern on a constrained disk; no limit is needed at today's scale (140
  snippets, default parallelism), so this is a note for later, not a required
  bound now.
- **Parallel execution as an incidental determinism canary**: RFC 0125
  already asserts generation determinism via `canonicalArtifactHash`'s
  sorted-names, length-prefixed encoding; running snippet compiles
  concurrently is a free additional check that generation has no
  goroutine-order dependency, not something this RFC needs to add explicit
  coverage for.

## Validation

- **Performance target**: `go test -count=1 -timeout=10m -parallel=8 -tags c23 -run TestC23SnippetCatalogCompiles ./compiler/tests/c23validation -v` completes uncached (the `-count=1` forces this) within 5 minutes on the authoring host. This target is a projection from the measured 16.31s/9-compile sample (≈1.8s/compile; 420 compiles sequential ≈13 minutes, divided by up to 8-way parallelism ≈2 minutes, with the 5-minute figure leaving headroom for the still-unattributed remainder from "Root cause") — record the actual elapsed time achieved in this section once implemented. If missed, follow Detailed implementation plan step 5 rather than declaring success anyway.
- `discoverAllToolchains` resolves each of gcc/clang/zig exactly once for the whole run (verify via a counter or by confirming exactly one `t.Logf`/log line per toolchain, not 140).
- All 140 snippets reach all three toolchains and the run reports a real pass or fail for every one — not killed by an external timeout.
- Any snippet that fails this newly-completed sweep is filed as a finding for a separate RFC (or an existing owner) to fix — this RFC's own validation is that the sweep completes and reports a real result, not that every snippet passes.
- `go test -race ./workbench/snippets/...` passes, including the new `TestCatalogProgramsCompileConcurrently`.
- `go test -tags c23 -run TestC23Suite ./compiler/tests/c23validation/...` still passes — the discovery-cache change touches its call path too, not just the catalog sweep.
- `go test ./...` (untagged) neither compiles nor runs anything in `compiler/tests/c23validation`, by the existing `//go:build c23` tag — no test invokes an external toolchain outside a `-tags c23` run.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` continue to pass.
- `gofmt -l .` reports nothing.
- No Go implementation change lands outside `compiler/tests/c23validation` and the one new test in `workbench/snippets`; this RFC's own lifecycle edits (this file's `Status:` header, its move to `docs/specs/archive/`, and its `docs/status.md` entry) and any separately-owned finding a completed sweep surfaces are exempt from that scope, not violations of it.
