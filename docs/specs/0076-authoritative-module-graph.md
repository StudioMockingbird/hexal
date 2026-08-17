# RFC 0076: Authoritative Module Graph

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-16
- Scope: module resolution ownership — one immutable graph built during
  reachability and consumed by the checker and generator
- Depends on: nothing. Independent of RFCs 0072–0079; see Sequencing for the one
  textual-conflict caution.
- Coordinates with: RFC 0074 (R11 diagnostic module field, R19 placeholder),
  `AGENTS.md`, `docs/reference.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, generated C, or the
  `compiler.Compile` contract

## Summary

Module resolution is computed once during reachability, thrown away, and then
re-derived — partially and by a second implementation — in the checker.

Build one immutable `moduleGraph` during reachability holding, per module: the
canonical id, the exact logical source key, the parsed program, and its resolved
ordered imports. Pass it to the checker and generator. Delete the second
implementation and the four hand-rolled key reconstructions.

This is a consolidation with one correctness consequence: it removes the class
of defect where `order` and `programs` can disagree.

## Evidence

### Two functions named `canonicalModuleID`, in two packages, doing different jobs

```go
// compiler/compile.go:112 — trivial suffix strip
func canonicalModuleID(logicalKey string) string {
    return strings.TrimSuffix(logicalKey, ".hex")
}

// compiler/compile.go:132 — the validating authority
func resolveImportPath(fromModule, rawPath string) (string, error)

// compiler/checker/modules.go:542 — a silent mirror of resolveImportPath
func canonicalModuleID(fromModule, payload string) string {
    path := payload
    if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' { ... }
    if !strings.HasPrefix(path, "./") && !strings.HasPrefix(path, "../") { ... }
```

The checker's version re-implements the same path arithmetic without the
validation, and its own comment says it mirrors the compiler's. **They agree
today only because `compile.go` rejects malformed paths before the checker
runs** — a coupling nothing enforces and no test asserts.

The name collision is independent of the logic duplication and is its own
hazard: a reader who greps `canonicalModuleID` finds two answers.

### The resolution result is computed and discarded

`resolveImport` (`compile.go:285`) resolves each import to a canonical `target`
and uses it only to recurse and to record diagnostics. The alias is never paired
with the target:

```go
target, err := resolveImportPath(fromModule, rawPath)
...
if imported[target] { /* duplicate */ }
```

`reachableModules` then returns `([]string, map[string]parser.Program, error)` —
order and programs, with every edge dropped. The checker rebuilds the edges from
source text.

### The canonical-to-logical mapping is reconstructed four times

`reachState.parsed` is keyed by **logical source key**; `reachState.order` holds
**canonical ids**. The mapping between them lives only inside `sourceKeyFor`, so
every consumer re-derives it:

| Site | Code |
|---|---|
| `compiler/checker/checker.go:257` | `key := moduleID + ".hex"` |
| `compiler/checker/checker.go:283` | `key := moduleID + ".hex"` |
| `compiler/checker/modules.go:71` | `key := moduleID + ".hex"` |
| `compiler/generator/generator.go:25` | `key := canonical + ".hex"` |

Each has a bare-name fallback for the extensionless case. Four copies of a rule
that reachability already knows exactly.

### One alias table is dead

`scope.imports` (`compiler/checker/scope.go:31`, `alias -> canonical module id`)
is written and never read. The live table is `moduleEntry.imports`
(`compiler/checker/modules.go:39`), read at `:296`.

## The change

### The graph

```go
// moduleGraph is the resolved module structure of one compilation. It is built
// once during reachability and never mutated afterwards.
type moduleGraph struct {
    Order   []string              // dependency-first canonical ids
    Modules map[string]moduleNode // canonical id -> node; keys match Order exactly
    Root    string                // the entrypoint's canonical id
}

type moduleNode struct {
    Canonical  string        // "graphics/shapes"
    LogicalKey string        // "graphics/shapes.hex" — the exact supplied map key
    Program    parser.Program
    Imports    []moduleEdge  // source order, already resolved and validated
}

type moduleEdge struct {
    Alias  string // the binding name in the importing module
    Target string // canonical id of the imported module
}
```

`LogicalKey` is the key the caller supplied in `sources`. It is never derived,
never reconstructed, and never a host path.

### Construction

`reachState.resolveImport` already computes `target`; it additionally records
`moduleEdge{Alias: importDecl.Alias.Lexeme, Target: target}` on the importing
node. `reachableModules` returns `(*moduleGraph, error)` instead of
`([]string, map[string]parser.Program, error)`.

No resolution logic changes. Cycle detection, duplicate-import detection,
ambiguous-key detection, and missing-module detection keep their current single
owners in `reachState`.

### Consumption

`CheckModules` and `GenerateChecked` take the graph:

```go
func CheckModules(graph *moduleGraph, ...) (map[string]Program, error)
func GenerateChecked(graph *moduleGraph, checked map[string]Program) (map[string]string, error)
```

`buildModuleRegistry` reads `node.Imports` instead of re-parsing import
declarations. The four `+ ".hex"` sites become `graph.Modules[id].LogicalKey`.

### Deletions

- `compiler/checker/modules.go:542` `canonicalModuleID` — the mirror.
- `compiler/checker/scope.go:31` `scope.imports` — the dead table.
- The four `moduleID + ".hex"` reconstructions and their fallbacks.

`compile.go:112`'s `canonicalModuleID` survives as the single entrypoint
normalization. Rename it `canonicalFromLogicalKey` so no two functions in the
project share the name.

## What this fixes beyond consolidation

**RFC 0073 D28 stops being possible.** `GenerateChecked` currently does:

```go
program, ok = programs[key]
if !ok {
    continue     // silently skips a module named in order
}
```

With one structure, `Order` and `Modules` cannot disagree — the lookup that can
miss no longer exists. D28's guard should still be added under RFC 0073, because
this RFC lands later; this removes the class rather than the instance.

**RFC 0074 R11 gets its source.** `Diagnostic` needs a `Module` field, and the
graph is where module identity lives. R11 stays in 0074; this makes it cheap.

**The silent mirror cannot diverge**, because there is one implementation.

## Invariants

1. Generated C is byte-identical for every program. The snippet SHA-256 manifest
   must not move.
2. Diagnostics, categories, positions, ordering, and aggregation are unchanged.
3. Resolution behavior is unchanged: the same programs resolve, the same
   programs are rejected, with the same messages.
4. The graph is immutable after construction. No consumer mutates it, and it
   carries no checker or generator state.
5. **The compiler performs no filesystem access.** `LogicalKey` is a caller
   supplied map key. Nothing in this RFC reads, probes, or normalizes a host
   path.
6. No exported API changes. `moduleGraph` is unexported and never crosses the
   `compiler.Compile` boundary.

## Sequencing

**No spec blocks this one.** The module graph carries no type identity, so RFC
0073's `CanonicalKey` and `Environment` split are logically independent of it.

One caution, textual rather than logical: RFC 0073's D25 restructures per-module
state in `checker/`, and this RFC restructures per-module resolution in the same
package. Running both concurrently makes a merge conflict likely and a regression
harder to attribute. Land one, then the other — in either order.

Within this RFC:

1. Add `moduleGraph` and populate it in `reachState`, returning it **alongside**
   the existing values. Nothing consumes it yet; tests prove it agrees with what
   the current code derives.
2. Switch `GenerateChecked` to the graph. Smallest consumer, one `+ ".hex"` site.
3. Switch `CheckModules` and `buildModuleRegistry`. Delete the mirror.
4. Delete `scope.imports` and the remaining reconstructions.
5. Remove the old return values from `reachableModules`.

Step 1 is worth keeping as its own commit: an agreement test between the graph
and the existing derivation is the cheapest possible proof that the change is
behavior-preserving.

## Validation

- `go test ./...`, `go vet ./...`,
  `go vet -tags c23 ./compiler/tests/c23validation`.
- The snippet catalog compiles and the manifest is **unchanged** — this RFC
  changes no output.
- A test asserts `Order` and `Modules` have identical membership.
- A test asserts every `moduleEdge.Target` is present in `Modules`.
- A test asserts `LogicalKey` round-trips: for every node, the supplied `sources`
  map contains that exact key.
- Existing module tests — resolution, visibility, identity, generation,
  artifacts — pass unmodified. If any needs changing, the change is not
  behavior-preserving and must be justified.
- `grep -rn 'canonicalModuleID' compiler/` returns one definition.
- `grep -rn '+ "\.hex"' compiler/ --include='*.go'` returns no non-test hits.

## Non-goals

- Changing resolution rules: relative-path arithmetic, cycle detection,
  duplicate detection, visibility, or import ordering.
- Filesystem access of any kind, including path normalization. RFC 0055 owns
  the host layer.
- Incremental compilation, caching, or reusing a graph across `Compile` calls.
  The graph is per-compilation and discarded with it.
- Adding the `Module` field to `Diagnostic` — RFC 0074 R11 owns that.
- Exporting the graph or any part of it.
- Changing `CompilationResult`, `CompilationStats`, or artifact naming.

## Drawbacks

- Three stage signatures change (`reachableModules`, `CheckModules`,
  `GenerateChecked`), so the diff touches all four compiler packages even though
  no algorithm changes.
- A single structure passed through the pipeline invites future accretion. The
  immutability invariant and the "carries no checker or generator state" rule
  exist to prevent that; enforce them in review.
- The staged approach in Sequencing means the graph and the old derivation both
  exist for two commits. That is deliberate — it is what makes the agreement
  test possible.

## Expected result

- One implementation of import-path resolution.
- One place that knows a canonical id's logical source key.
- One alias table.
- No function name shared by two packages with different meanings.
- `Order` and `Modules` cannot disagree, so a module named in the order cannot be
  silently skipped during generation.
