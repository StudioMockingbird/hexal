# RFC 0221: Go Library Adoption and Compiler Infrastructure

- Kind: Architecture Decision Record (ADR)
- Status: Open Discussion
- Created: 2026-09-19
- Scope: reduce compiler-owned utility code by adopting suitable Go standard
  library packages, isolate module traversal behind a small internal graph
  utility, and establish a source-span model compatible with deterministic
  diagnostics and generated `#line` mappings
- Does not authorize: replacing Hexal's lexer, parser, checker, or C
  generator with Go compiler packages; adding filesystem access to the core
  compiler; parallel compilation before a separate concurrency decision; or
  adding a dependency solely to shorten a trivial loop

## Context and current facts

The module declares Go 1.26.4. It already depends on:

- `golang.org/x/exp/ebnf` for verification of the checked `GRAMMAR.ebnf`;
- `github.com/fzipp/gocyclo` and `github.com/uudashr/gocognit` for benchmark
  and complexity reporting; and
- `golang.org/x/tools` only through the existing dependency graph.

The compiler is an in-memory string-in/string-out pipeline. `Compile` receives
logical source keys and source contents, resolves reachable modules, checks
them, and returns generated C/header contents as strings. Any adopted library
must preserve that boundary.

The current implementation already uses `slices` in module traversal,
diagnostic ordering, generated-component ordering, and type-union handling.
The current implementation uses `go/constant` in literal parsing, integer and
floating-point conversion, bounds checks, slice bounds, pool and concurrency
capacity checks, boolean starvation analysis, and compiler-generated source
coordinates. These existing uses are the starting point; this ADR does not
propose replacing them with a second constant representation.

The largest relevant custom areas are:

- `compiler/compile.go`: `reachState` performs reachable-module DFS, cycle
  detection, canonical-key resolution, post-order construction, and
  deterministic diagnostic collection;
- `compiler/types/arena.go` and `compiler/types/types.go`: compilation-wide
  type interning, nominal identity, aliases, generated C names, and source
  coordinates;
- `compiler/checker`: scopes, bindings, generic specialization, control-flow
  facts, constant evaluation, diagnostics, and Hexal-specific semantics;
- `compiler/parser` and `compiler/lexer`: Hexal syntax, contextual keywords,
  interpolation, `do`/`end` blocks, and custom source positions; and
- `compiler/generator`: checker-driven C23 lowering, generated declarations,
  source mapping, runtime components, and output ordering.

Beyond those large areas, three small hand-rolled parsers remain:
`internal/version` recovers the executable timestamp with a regexp, a month
table, and a leap-year function; `internal/backend` extracts the Clang major
version with a regexp and a digit loop; and `workbench` dispatches HTTP methods
by hand.

## Decision summary

Adopt libraries only where their contract exactly matches an existing compiler
invariant. Prefer the Go standard library. Use `golang.org/x/sync` only when
parallel or incremental compilation is separately authorized and the required
concurrency boundary is explicit.

The work is divided into six independent tracks:

1. use `slices`, `maps`, and `cmp` for straightforward collection boilerplate;
2. expand the existing `go/constant` use across constant evaluation;
3. extract module traversal into a small internal graph utility;
4. introduce a `go/token`-inspired, Hexal-owned source-span model;
5. defer `golang.org/x/sync` until parallel or incremental compilation needs
   it; and
6. replace small hand-rolled parsing and HTTP-method dispatch with standard
   library equivalents (`time.Parse`, `strings`, `strconv`, `filepath.WalkDir`,
   and `net/http` method patterns) where the contract matches.

Each track may land independently. No track changes Hexal source semantics.

## Track 1: collection utilities

### Adopt

Use the standard packages where they make the existing intent clearer:

- `slices` for membership, cloning, filtering, sorting, stable sorting, and
  index lookup;
- `maps` for cloning and copying maps when ownership is already explicit; and
- `cmp` for simple ordered comparisons and comparator composition.

The compiler must keep explicit loops when they express semantic work,
short-circuit diagnostics, preserve a deliberate order, or avoid an
allocation. A migration is not justified merely because a loop can be written
as a helper call.

### Current targets

Review, without changing behavior:

- repeated map-copy and map-merge code in `compiler/types/types.go`,
  `compiler/types/arena.go`, and `compiler/checker/scope.go`;
- repeated membership and deduplication code in checker registries and module
  export tables;
- repeated sorting and comparator closures in checker and generator output
  assembly; and
- hand-written min/max or comparison helpers that have no compiler-specific
  diagnostic behavior.

Existing `slices` calls in `compiler/compile.go`, `compiler/types`, and
`compiler/generator` are canonical examples of the intended level of use.

### Quantified migration targets

Fifteen `sort` call sites in non-test code across nine files. Four collect map
keys and then sort, which collapses to the `slices.Sorted(maps.Keys(...))`
pattern already used in the checker and generator
(`compiler/checker/modules.go:645`, `compiler/generator/concurrency.go:375`).
That pattern appears **10 times in non-test code and twice in tests**; the
production count is the one a migration matches:

| File | Lines | Current | Replacement | Lines saved |
| --- | --- | --- | --- | --- |
| `internal/driver/mode.go` | 175-179 | key slice + `sort.Strings` | `slices.Sorted(maps.Keys(files))` | 3 |
| `internal/driver/driver.go` | 530-534 | key slice + `sort.Strings` | `slices.Sorted(maps.Keys(files))` | 3 |
| `internal/driver/foreign.go` | 255-259 | key slice + `sort.Strings` | `slices.Sorted(maps.Keys(byKey))` | 3 |
| `internal/driver/normalize.go` | 706-710 | key slice + `sort.Strings` | `slices.Sorted(maps.Keys(importer.macroTypes))` | 3 |

The remaining eleven sites are direct replacements with no line-count change:

| File | Lines | Current | Replacement |
| --- | --- | --- | --- |
| `internal/driver/frontend.go` | 212 | `sort.Strings` | `slices.Sort` |
| `internal/driver/runpack.go` | 187 | `sort.Strings` | `slices.Sort` |
| `internal/driver/normalize.go` | 889 | `sort.Strings` | `slices.Sort` |
| `internal/driver/normalize.go` | 904, 907, 910, 913, 916 | `sort.SliceStable` | `slices.SortStableFunc` |
| `compiler/runtime_dependency.go` | 30 | `sort.Slice` | `slices.SortFunc` |
| `compiler/generator/local_helpers.go` | 69 | `sort.Slice` | `slices.SortFunc` |
| `compiler/checker/captures.go` | 452 | `sort.SliceStable` | `slices.SortStableFunc` |

Expected benefit: about 12 lines removed, the `sort` import leaves nine files,
comparators become type-safe, and the map-key idiom matches the ten existing
non-test `slices.Sorted(maps.Keys(...))` uses. The five `normalize.go` stable sorts stay
stable; the two `sort.Slice` sites become the equivalent unstable
`slices.SortFunc`. Complexity is unchanged.

### Constraints

- Never replace an ordered diagnostic merge with map iteration.
- Never use `maps` to hide ownership or mutation of checker state.
- Do not introduce a generic helper when the direct loop is shorter and clearer.
- Preserve all deterministic output and source-order guarantees.

## Track 2: constant evaluation

### Adopt

Extend the existing `go/constant` representation rather than introducing a
second evaluator. Constant-producing checker nodes should continue to carry
`constant.Value` and the Hexal type separately.

The review should consolidate operations currently distributed across
`compiler/checker/literals.go`, `conversions.go`, `operator_checking.go`,
`expressions.go`, `slices.go`, `pool.go`, `concurrency.go`, and
`starvation.go` where `go/constant` already provides the exact required
operation:

- integer, unsigned, rational, and floating arithmetic;
- comparisons and sign checks;
- unary operations and bit operations;
- exact-to-inexact conversion checks;
- literal normalization and representability checks; and
- boolean constant propagation used by reachability diagnostics.

### Hexal-owned semantics remain outside the library

The checker must retain ownership of:

- Hexal target widths and signed/unsigned ranges;
- wraparound rules where Hexal deliberately defines them;
- float32/float64 rounding and bit-preserving conversion policy;
- contextual typing and inference rejection;
- pointer, allocation, resource, Atomic, and function-value restrictions;
- C23 representation decisions; and
- diagnostics, source positions, and earliest-phase ownership.

`go/constant` is an arithmetic value engine, not a replacement for the Hexal
checker.

### Required result

Equivalent constant expressions must use one constant representation and one
diagnostic path. No migration may change whether an expression is folded,
which Hexal type it receives, or which error category and location it reports.

## Track 3: internal module graph utility

### Adopt

Extract the graph mechanics from `compiler/compile.go` into a small internal
package. The utility should be generic over node identity or use a narrow
compiler-owned node identifier. It must provide only the mechanics needed by
the current module resolver:

- reachable traversal;
- cycle detection with the active DFS path;
- dependency-first post-order;
- deterministic neighbor/result ordering when requested; and
- no implicit file or directory access.

The existing resolver remains responsible for Hexal meaning:

- logical-key validation;
- relative and standard-library import syntax;
- C-binding preparation;
- duplicate imports;
- module diagnostics and source locations;
- embedded stdlib lookup; and
- construction of `checker.ModuleGraph` and `ModuleEdge` values.

The utility must not know about Hexal syntax, filesystem paths, C imports, or
diagnostic wording.

### Proposed shape

The exact API is implementation-owned, but it should be no larger than the
actual contract, approximately:

```go
type Graph[N comparable] struct {
    Edges map[N][]N
}

func (g Graph[N]) Reachable(root N) []N
func (g Graph[N]) PostOrder(root N) ([]N, error)
```

The current `reachState` has additional resolver state and should not be
collapsed into this utility. The extraction is successful only if it removes
graph mechanics without moving module semantics into a generic package.

## Track 4: Hexal source spans

### Adopt

Introduce a Hexal-owned source-span model inspired by `go/token`:

```go
type Span struct {
    File  string
    Start int // byte offset in the logical source string
    End   int // exclusive byte offset
}
```

A compilation-owned file table converts offsets to line and column. Tokens,
parser nodes, checker diagnostics, and generator source mappings may retain
derived line/column values at API boundaries during migration, but the span is
the authoritative identity of a source location.

The model must support:

- logical source keys rather than host paths;
- byte offsets and UTF-8-safe line/column conversion;
- zero-width insertion locations for diagnostics;
- spans covering multi-token syntax;
- deterministic `#line` mapping back to the logical Hexal source; and
- diagnostics from embedded stdlib and prepared C-binding sources.

### Migration boundary

First add a source-position/file-table package and adapt lexer tokens. Then
adapt parser nodes and diagnostics. Only after those contracts stabilize should
checker and generator structs stop carrying `SourceLine`/`SourceColumn`
independently.

The public `Compile` API remains unchanged. It continues to return rendered
diagnostic strings and generated files as strings.

### Constraints

- A span must never contain an absolute host path.
- Line and column conventions must be specified once and tested at EOF,
  multiline comments, Unicode text, and zero-width errors.
- Generated C must preserve the current source-mapping contract.
- Span migration must not silently alter diagnostic ordering or ownership.

## Track 5: deferred concurrency helpers

`golang.org/x/sync` may be added only when a separate implementation requires
parallel module checking, specialization deduplication, or incremental cache
coordination.

Potential uses are:

- `errgroup` for bounded concurrent work with cancellation;
- `singleflight` for deduplicating identical in-flight module or
  specialization work; and
- a semaphore or equivalent bounded worker mechanism if the chosen design
  needs one.

No `x/sync` package is justified for the current sequential compiler. Adding
it before the ownership and determinism model exists would make races and
diagnostic ordering harder to reason about.

When this track becomes active, its specification must define:

- which compiler state is immutable and shareable;
- which state is per-module or per-specialization;
- cancellation and failure propagation;
- deterministic diagnostic and artifact ordering;
- duplicate-work behavior; and
- cache lifetime and invalidation for incremental compilation.

The core compiler remains filesystem-free. Any persistent cache belongs to a
future driver layer, not to `compiler.Compile`.

## Track 6: standard-library parsing and serving replacements

### Adopt

Three small hand-rolled areas have exact standard-library equivalents.

**Executable timestamp.** `internal/version/version.go` (117 lines) recovers
the build identity with `timestampPattern` (line 20), a `months` table
(lines 22-27), `validTimestamp` (lines 81-101), and `daysIn` (lines 103-117).
`time.Parse("2006-Jan-02-15-04", text)` replaces all four: it enforces the same
four-digit year and two-digit day, hour, and minute, rejects a trailing
remainder, and validates the calendar including leap years.

Expected benefit: 43 lines removed and one `time.Parse` call added, for a net
reduction of about 41 lines of the 117-line file; `validTimestamp` (cyclomatic
complexity 7) and `daysIn` (cyclomatic complexity 6) are deleted, both currently
in the `<=10` band of the complexity report; and the `regexp` and `strconv`
imports leave the file. One behavior change is explicit: `time.Parse` matches
month names case-insensitively, so `2026-jUl-01-00-00` becomes valid where the
case-sensitive `months` table rejects it. If canonical case is contractual, add
a three-byte case check, reducing the net saving to about 38 lines. A probe
confirms the rest of the shape is unchanged: `2026-Jul-1-00-00`,
`2026-Jul-01-24-00`, `2026-Feb-29-00-00`, and `2026-Feb-30-00-00` are all
rejected.

**Clang major version.** `internal/backend/backend.go` (133 lines) extracts the
major version with `clangVersionPattern` (line 45) and a hand-rolled digit loop
(lines 65-68). A `strings` scan for `clang version ` followed by `strconv.Atoi`
replaces both.

Expected benefit: about 6 lines removed; the `regexp` import and its init-time
compile leave the file; and `strconv.Atoi` reports overflow, which the digit
loop silently wraps. The fail-closed behavior on a non-Clang banner is
unchanged.

**HTTP method dispatch.** `workbench/main.go` (184 lines) checks
`request.Method` by hand in `snippetsHandler` (lines 108-112) and
`compileHandler` (lines 117-121), and rejects a non-root path in `serveIndex`
(lines 83-86). Go 1.22 method patterns on `http.ServeMux`
(`"GET /api/snippets"`, `"POST /api/compile"`, `"GET /{$}"`) replace all three.

Expected benefit: about 14 lines and three branches removed; `ServeMux` returns
the 405 status and `Allow` header that the handlers set by hand. This is a
simplification of a debug component with no hardening policy, not a behavior
change.

### Adopt: directory walking

`internal/driver/driver.go` line 482 uses `filepath.Walk`, which stats every
entry. `filepath.WalkDir` reads the directory entry type that `ReadDir` already
returned, so it avoids a stat per file. This is the one production `Walk` call;
the tests already use `WalkDir`.

Expected benefit: one fewer `lstat` per discovered entry; line count is
unchanged. The symlink rejection and case-fold collision checks in `discover`
(lines 487-508) move from `os.FileInfo` to `fs.DirEntry` unchanged.

### Constraints

- The `internal/version` month-name case decision is recorded explicitly, not
  silently widened.
- `internal/backend` continues to fail closed on a non-Clang banner and on an
  overflowing major version.
- No new dependency: every replacement is the standard library.

## Evaluated and rejected

Each of these was proposed and rejected after checking the code. They are
recorded so the same proposal does not return without new evidence.

- **`compiler/lexer/scanNumber` to `strconv`.** `scanNumber`
  (`compiler/lexer/lexer.go:1157-1237`) and its helpers (lines 1239-1287) own
  lexeme boundaries and malformed-separator diagnostics. Numeric range and
  overflow are the checker's `go/constant` concern (Track 2), not the lexer's.
  `strconv` would duplicate the separator logic and cannot produce the existing
  `malformed ... literal` diagnostics.
- **`filepath.IsLocal` or `os.Root` in the driver.** Containment is already
  enforced: `materialize` rejects an escaping key
  (`internal/driver/driver.go:539`), a symlinked target (lines 546-549), and a
  case-fold collision (lines 542-544); `runpack` rejects escaping pack paths
  (`internal/driver/runpack.go:300,314`). Tests cover both
  (`internal/driver/driver_test.go:198`, `internal/driver/runpack_test.go:179`).
  There is no gap to close, and `os.Root` is a broad refactor for no behavior
  change.
- **`golang.org/x/mod/module`.** Logical-key and pack-path validation already
  exist and are tested; the package would add a dependency without changing
  behavior.
- **`unique.Make` for arena keys.** Arena keys are per-family canonical strings.
  Cross-family duplication is not demonstrated, so `unique` adds a `Handle`
  indirection for no measured memory reduction.
- **A generic interner for the arena.** `compiler/types/arena.go` declares
  thirteen identical `map[string]Type` maps (lines 14-26) and initializes all
  sixteen maps in `NewArena` (lines 50-65). Each family's interning is a
  three-line lookup-and-insert (`compiler/types/collections.go:156-168`,
  `compiler/types/types.go:606-630`), and the key construction and type
  construction differ per family. A generic `intern(key, build)` helper replaces
  three lines with a closure of comparable size, so the measured benefit is
  about zero; Track 1's own rule forbids a helper that is not shorter or
  clearer. Revisit only if the map set grows.
- **`unicode` for identifier classification.** Hexal identifiers are ASCII by
  grammar (`isIdentifierStart`/`isIdentifierPart`,
  `compiler/lexer/lexer.go:1304-1310`). Unicode classification is wider and
  slower.

## Non-goals

- replacing the Hexal lexer with `go/scanner`;
- replacing the parser or AST with `go/parser` or `go/ast`;
- replacing Hexal type checking with `go/types`;
- using `go/format` or Go templates to format generated C;
- importing a large graph framework for the module resolver;
- adding concurrency only to reduce wall-clock time in small builds; and
- changing language behavior, generated C ABI, diagnostics, or compiler
  boundary semantics.

## Implementation order

1. Apply the standard-library parsing, serving, and walking replacements
   (Track 6): self-contained and mechanically verifiable, with the one
   month-name case decision stated up front.
2. Inventory and adopt the `slices`, `maps`, and `cmp` cases, including the
   quantified `sort` targets in Track 1.
3. Consolidate constant folding around the existing `go/constant` values.
4. Extract and test the internal graph mechanics without changing resolver
   diagnostics.
5. Design and land the source-span/file-table package, then migrate consumers
   in stages.
6. Write a separate concurrency/incremental-compilation specification before
   adding `golang.org/x/sync`.

The order is deliberate: source spans and graph extraction affect broad
contracts, while the parsing, collection, and constant-library changes are
smaller and can provide evidence before those changes.

## Validation

This ADR is implemented only when all of the following are true:

1. Existing `slices` uses remain behaviorally identical, and newly migrated
   collection code preserves deterministic order, allocation expectations
   where tested, and all diagnostics.
2. The compiler has no duplicate constant representation for expressions that
   `go/constant` can represent, and existing constant, conversion, range,
   Boolean, and starvation tests pass unchanged in meaning.
3. Module traversal has focused tests for reachable-only traversal, dependency
   post-order, duplicate imports, cycles, deterministic ordering, missing
   modules, stdlib modules, and prepared C bindings.
4. The graph utility performs no filesystem, process, or source parsing work.
5. Source spans have focused tests for single-line, multiline, EOF,
   zero-width, UTF-8, embedded-stdlib, and prepared-binding locations.
6. Existing diagnostic text, category, module identity, line/column output,
   generated `#line` mappings, generated artifacts, and statistics remain
   unchanged unless a separate approved spec explicitly changes them.
7. `golang.org/x/sync` is absent until a separate concurrency or incremental
   compilation specification authorizes it; once authorized, its validation
   covers cancellation, duplicate work, deterministic output, and race safety.
8. `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass without
   an external C toolchain.
9. The Track 6 replacements preserve behavior except for the one stated
   `internal/version` month-name case decision; `internal/backend` still fails
   closed on a non-Clang banner and on a major version that overflows `int`;
   and `internal/driver` still rejects an escaping artifact key, a symlinked
   target, and a case-fold collision after the `WalkDir` migration.

## Open questions

1. Should the source-span file table live in `compiler`, `lexer`, or a new
   narrow internal package?
2. Should public checker and generator structs expose spans, or should spans
   remain internal until diagnostics are rendered?
3. Should graph post-order accept caller-supplied sorted neighbors, or should
   the utility sort node IDs itself?
4. Which existing constant-folding operations intentionally differ from
   `go/constant` and therefore require Hexal wrappers?
5. Should `maps.Clone` be permitted for checker state, or must state copying
   remain explicit to expose semantic ownership?
6. What measurable build size or latency threshold justifies adding
   `golang.org/x/sync`?
7. Does incremental compilation require a separate cache specification before
   any `singleflight` work begins?
