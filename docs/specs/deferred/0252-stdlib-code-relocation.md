# RFC 0252: Independent Standard-Library Packages

- Kind: Architecture Decision Record (ADR)
- Status: Deferred; sound extraction boundary, insufficient near-term RoI
- Created: 2026-09-29
- Updated: 2026-09-30
- Origin: repository-layout review of the RFC 0186 code-location table
- Depends on: the in-memory compiler contract, the current immutable
  `compiler/specdata` registry, and the current embedded-component machinery
- Coordinates with: RFC 0233 (JSON), RFC 0234 (regular expressions), archived
  RFC 0186 (standard-library boundary), archived RFC 0229 and RFC 0230
  (immutable records and validation from tests), archived RFC 0238
  (declarative generated-C fact guards), and archived RFC 0245 (LF-pinned
  generated-C templates)
- Updates `docs/reference.md`: no. Package layout is not language surface and
  the reference contains no affected repository path.
- Swept code: standard-library declaration literals in
  `compiler/specdata/corelib.go`; `compiler/corelib/runtime.go` and
  `compiler/corelib/runtime/`; standard-library-owned templates in
  `compiler/generator/packages/`; all live path scans over those templates;
  their `.gitattributes` rule; comments that assign standard-library assets to
  a compiler package

## Decision

The standard library becomes an independent root-level package tree:

```text
stdlib/
  stdlib.go             Hexal source-module embedding
  std/*.hex             Hexal source modules
  manifest/
    manifest.go         compiler-neutral public declarations
    declarations.go     immutable standard-library declaration data
    manifest_test.go    library-local integrity checks
  runtime/
    assets.go           compiler-neutral C/header embedding
    *.c, *.h            standard-library-owned runtime templates
```

Packages under `stdlib/` import no `hexal/compiler/...` or
`hexal/internal/...` package. They may import Go's standard library and sibling
packages under `hexal/stdlib/...` only. This is the extraction boundary: the
tree must build and test without importing the compiler.

The compiler depends on that tree through narrow adapters:

```text
stdlib/manifest  <- compiler/specdata <- compiler/types
stdlib/runtime   <- compiler/generator
stdlib           <- compiler/compile.go
```

Compiler-only knowledge stays in the compiler:

- `compiler/corelib/corelib.go` remains the checked-type adapter because it
  constructs `compiler/types` values.
- `compiler/corelib/hints.go` remains compiler diagnostic policy, not library
  content.
- `compiler/specdata` retains `CoreModule`, `CoreFunction`, `CoreTypeID`,
  `TypeID`, component records, and the unified cross-domain integrity graph.
- Checker and generator files remain in their stage packages.

The standard-library manifest is the authoritative neutral declaration list.
`compiler/specdata/corelib.go` converts that manifest into its existing
compiler records. There is one manually maintained declaration list, not one
copy in each package.

## Summary

Today the standard library is spread across root `stdlib/`,
`compiler/corelib/runtime/`, `compiler/specdata/corelib.go`, and selected
templates under `compiler/generator/packages/`. The previous version of this
RFC moved compiler record types and integrity logic into one broad `stdlib` Go
package. That would have made the library import compiler internals, weakened
the unified integrity graph, and made later repository extraction harder.

This decision instead separates **library-owned portable data and assets** from
**compiler-owned interpretation**:

| Concern | Owner after this RFC |
| --- | --- |
| Hexal standard-library source | `stdlib` |
| Neutral module/type/function declarations | `stdlib/manifest` |
| Standard-library C/header templates | `stdlib/runtime` |
| Mapping declarations to compiler IDs and types | `compiler/specdata` |
| Checked-type construction and migration diagnostics | `compiler/corelib` |
| Cross-domain symbol, component, ABI, and cycle validation | `compiler/specdata` |
| Checker and generator stage behavior | their existing compiler packages |

No language behavior, diagnostic, generated-C byte, module identity, or
artifact key changes.

## Rationale

The user-facing library should not know how one compiler represents types,
components, or checked syntax. A future standalone repository must be usable
as a dependency of the compiler rather than importing the compiler back.

The neutral manifest provides that seam. For example, the library may describe
one function using its own data vocabulary:

```go
manifest.Function{
    Name:       "parse",
    Runtime:    "hex_json_parse",
    Parameters: []manifest.ParameterKind{
        manifest.HeapParameter,
        manifest.StringParameter,
    },
    Result:     manifest.ValueResult,
    Fallible:   true,
    Components: []string{"json"},
}
```

The compiler adapter maps those neutral values to `specdata.TypeID`,
`specdata.ComponentID`, error behavior, and checked types. Unknown manifest
values fail compiler-data validation; the library never imports those compiler
types.

This follows the useful part of the Zig and Odin layout: library source and
runtime assets form a distinct library tree while compiler glue remains in the
compiler. It does not pretend that compiler adapters are library code merely
because they describe the library.

## Goals

1. `go test ./stdlib/...` builds and tests the standard-library tree without a
   dependency on any compiler or internal Hexal package.
2. Every standard-library-owned Hexal source, neutral declaration, and C/header
   asset lives below root `stdlib/`.
3. Compiler-only type mapping, diagnostics, stage state, and cross-domain
   validation stay in compiler packages.
4. A later repository extraction requires dependency and import-path changes,
   not an architectural redesign.
5. Generated artifacts, diagnostics, module identities, and behavior remain
   byte-for-byte unchanged.
6. All content remains embedded at Go build time; the core compiler remains
   string-in/string-out and performs no host-filesystem discovery.

## Non-goals

- No separate Go module, Git submodule, package manager, version protocol, or
  independent release in this RFC. A focused package boundary is enough now.
- No dynamic loading or filesystem lookup of a standard library.
- No rewrite of C-backed facilities into Hexal merely to populate `std/`.
- No grammar, public language API, or `docs/reference.md` change.
- No renaming of compiler-domain identifiers solely for cosmetics.
- No removal of the compiler adapter. Decoupling means one-way dependency, not
  that the compiler understands the library without an adapter.
- No promise that internal Go package paths are compatibility APIs.

## Neutral manifest contract

`stdlib/manifest` owns plain immutable data types sufficient to describe the
current standard-library surface:

- module ID and canonical `std/...` path;
- exported type names;
- function names and runtime symbols;
- parameter and result kinds;
- required component names;
- fallibility and error behavior already present in the declaration records.

A function implemented by ordinary generated-C runtime code names its C symbol
and signature data. A function requiring special checker behavior names a
manifest-owned intrinsic key such as `file.open` or `time.sleep`. The manifest
does not copy the compiler's current `file_open`-style dispatch spelling. The
compiler adapter owns the exhaustive intrinsic-key-to-checker-operation map;
validation rejects a manifest intrinsic with no compiler implementation and a
compiler mapping with no manifest declaration.

The manifest uses strings and manifest-owned enums. It contains no
`compiler/types.Type`, `specdata.TypeID`, `specdata.ComponentID`, checker node,
generator model, function value, callback, or mutable registration hook.

`Modules()` returns defensive copies. No caller can mutate the authoritative
registry. `Validate()` checks facts internal to the library:

- unique module IDs and paths;
- unique exported names within a module;
- unique runtime symbols where the library itself requires uniqueness;
- valid parameter/result enum values;
- non-empty component names and other required fields;
- references between manifest-owned declarations.

Compiler integration remains validated after adaptation by the existing
`specdata.Validate()` graph. It continues to check all domains together,
including:

- core-function references to concrete types, constructors, and components;
- runtime-symbol ownership across builtin methods and library functions;
- component ABI completeness;
- dependency cycles and missing targets.

The unified graph is not split. This preserves the strongest existing check
and avoids a new coordinator or exported integrity-node protocol.

`CoreTypeID` constants stay in `compiler/specdata` beside the `TypeID` aliases
that consume them. The adapter maps manifest type names to those compiler IDs.
This avoids the `specdata -> stdlib -> specdata` cycle that the earlier design
would have created.

## Runtime asset ownership

`stdlib/runtime` owns templates implementing public standard-library modules.
`compiler/generator/packages` owns language-core and genuinely shared runtime
infrastructure.

A C template's owner is determined by the abstraction it implements, not by
which generated C header includes it. Core-generated C may include a
standard-library header as an ordinary consumer without changing that header's
owner.

The settled move set is:

| Move to `stdlib/runtime` | Reason |
| --- | --- |
| Existing `compiler/corelib/runtime/*` (`entropy`, `program`, JSON, regex) | already standard-library-owned assets |
| `file` | implements `std/fs` |
| `network` | implements `std/net` |
| `process` | implements `std/process` |
| `signal` | implements `std/signal` |
| `terminal` | implements `std/terminal` |

The settled stay set includes:

| Stay in `compiler/generator/packages` | Reason |
| --- | --- |
| `handle` | owns program-wide handle registry and root initialization |
| `time` | shared by core event/scheduler behavior |
| `seek` | shared by core `Bytes`/equality behavior and stdlib IO |
| `io` | core print sink and common IO machinery |
| collections, heap, text, numeric, equality, scheduler, concurrency, module, error, types, runtime | language-core implementation |

RFC 0233 and RFC 0234 are allowed to add JSON/regex files to the first row
before this RFC starts. Phase 0 inventories their final names and bytes; it
does not reopen the ownership decision.

The `stdlib/runtime` directory declares package `runtimeassets`, avoiding a
collision with Go's `runtime` package. `runtimeassets.Templates()` returns a
defensive `map[string]string` of the embedded files. The generator merges that
map with its core templates and rejects duplicate names exactly as today.

## Dependency and architecture guards

The implementation adds permanent tests for these rules:

1. Every non-test Go file below `stdlib/` imports only Go standard-library
   packages or another `hexal/stdlib/...` package.
2. No `stdlib` package imports `hexal/compiler/...` or `hexal/internal/...`.
3. `compiler/specdata` may import `hexal/stdlib/manifest`, but no standard
   library package imports `compiler/specdata`.
4. The generator may import `hexal/stdlib/runtime`; runtime assets import no
   generator package.
5. Root `stdlib` remains pinned at `stdlib/` by the decided-path guard.
6. Every embedded template name is unique across compiler-core and stdlib
   roots.
7. Every template has one settled owner. The ownership test consumes the
   compiler's authoritative component data rather than maintaining a second
   list of component semantics.

These guards test dependency direction, not merely the absence of the old
directory name.

## Path and test sweep

The relocation updates every content scan in the same change:

- `.gitattributes` replaces the old corelib-runtime rule with
  `stdlib/runtime/* text eol=lf`.
- `compiler/tests/c23validation/trap_inventory_test.go` reads stdlib runtime
  bytes through `runtimeassets.Templates()` instead of a repository-relative
  glob.
- `compiler/generator/error_kind_tag_test.go` reads the same API rather than
  `../corelib/runtime`.
- `compiler/generator/error_inventory_test.go` includes standard-library C
  templates in its runtime-header size check; moving files must not silently
  reduce the checked population.
- `compiler/generator/components_test.go` verifies completeness and uniqueness
  across the two embedded owners and rewrites its packages-only comment.
- architecture-policy comments and tests describe the new one-way graph.
- RFC 0233 and RFC 0234 are closed and archived. They remain immutable; update
  `docs/reference.md` and active specifications for any current-path changes.

The scans assert their expected non-empty populations so a wrong root cannot
pass vacuously.

## Rejected alternatives

| Alternative | Rejection |
| --- | --- |
| One broad `stdlib` package importing `compiler/types` and `compiler/specdata` | couples the library to this compiler and prevents clean extraction |
| Move compiler record types and integrity code into `stdlib` | splits cross-domain validation and creates an import cycle through `TypeID` aliases |
| Keep all declarations only in `compiler/specdata` | leaves the public library contract compiler-owned and forces a future extraction redesign |
| Duplicate declarations in stdlib and compiler | two manually maintained truths inevitably drift |
| Move checked-type adapters or migration hints into stdlib | those are compiler interpretation and diagnostic policy |
| A mutable registration callback from stdlib into specdata | introduces initialization order and mutable global state |
| Make `stdlib` a nested Go module now | adds workspace/release ceremony without helping the current extraction boundary |
| Load source or templates from disk | violates the in-memory compiler boundary and makes compiler behavior depend on installation layout |
| Classify template ownership by include direction | a consumer include does not own the included abstraction |
| Move `handle`, `time`, `seek`, or `io` merely because stdlib uses them | they implement shared or language-core behavior |

## Implementation plan

### Phase 0 - stable baseline and inventory

1. Wait until RFC 0233 and RFC 0234 have stopped modifying
   `compiler/specdata/corelib.go`, `compiler/corelib/runtime/`, component
   records, and generator adapters.
2. Record every standard-library module, type, function, parameter/result
   kind, runtime symbol, component, and runtime template from the settled
   tree. Do not rely on the stale import/file counts from earlier reviews.
3. Record every importer of `compiler/corelib`, every live path literal naming
   `compiler/corelib/runtime`, and every test that scans
   `generator/packages/*.{c,h}`.
4. Record SHA-256 by template name, `git ls-files --eol`, the snippet manifest,
   diagnostics inventory, and generated artifacts for representative programs
   using each standard-library module.
5. Run `go test ./...` and `go vet ./...`.

*Gate:* the inventory names every affected declaration and file; the working
tree contains no concurrent edits in the move set.

### Phase 1 - create the independent manifest

1. Add `stdlib/manifest` with compiler-neutral record and enum types,
   immutable declaration data, defensive-copy accessors, and `Validate()`.
2. Move the authoritative declaration literals out of
   `compiler/specdata/corelib.go` into that manifest. Keep compiler record
   types, IDs, lookups, and unified integrity logic in `specdata`.
3. Turn `compiler/specdata/corelib.go` into an explicit adapter from manifest
   values to the existing internal records. Map every manifest enum through an
   exhaustive switch; map stable manifest intrinsic keys to the existing
   checker dispatch keys; an unknown value fails validation.
4. Add the stdlib import-boundary guard and a test that calls
   `manifest.Validate()`.
5. Preserve the existing `CoreModule*` compiler APIs so checker/generator code
   does not change merely because the data source moved.

*Gate:* the compiler's adapted record inventory is byte-for-byte/equality
equivalent to the Phase 0 record inventory; `specdata.Validate()` still runs
the unified graph and all existing integrity tests pass.

### Phase 2 - move existing corelib runtime assets

1. Add `stdlib/runtime/assets.go` with `//go:embed *.c *.h` and a defensive
   `Templates()` accessor.
2. Move every file from `compiler/corelib/runtime/` to `stdlib/runtime/`.
3. Delete `compiler/corelib/runtime.go`; update the generator to consume
   `runtimeassets.Templates()` directly.
4. Update `.gitattributes` and every path/content scan named above in the same
   change. Do not leave a repository-relative glob that silently scans an
   empty directory.

*Gate:* template name-to-SHA-256 inventory is unchanged; `git ls-files --eol`
reports `i/lf w/lf` for every moved asset; the snippet-manifest diff is empty.

### Phase 3 - move stdlib-owned generator templates

1. Move `file`, `network`, `process`, `signal`, and `terminal` C/header files
   to `stdlib/runtime/`.
2. Keep `handle`, `time`, `seek`, `io`, and all language-core templates in
   `compiler/generator/packages/`.
3. Update the component completeness/ownership test and all runtime diagnostic
   scans to consume both embedded roots.
4. Preserve one merged template namespace and the existing duplicate-name
   failure.

*Gate:* template names and hashes are conserved, each name has one owner,
generated artifacts are byte-identical, and no guard's checked population
shrinks.

### Phase 4 - sweep and conformance

1. Delete the empty `compiler/corelib/runtime/` directory and all live
   references to it. Keep `compiler/corelib` itself: its remaining files are
   compiler adapters and diagnostics.
2. Verify the standard-library tree has no compiler/internal dependency with
   `go list -deps ./stdlib/...` and the permanent architecture test.
3. Run `go test ./stdlib/...`, `go test ./...`, `go vet ./...`, and the tagged
   C23 suite. `go vet -tags c23` alone is not generated-C evidence.
4. Rebuild the snippet manifest and require an empty diff.
5. Verify `docs/reference.md` needs no change and update `docs/status.md` only
   for this RFC's lifecycle.

### Handoff

After all validation passes, rebuild `bin/hexal` and restart the workbench
through `hexal play`. This is a handoff operation, not a language acceptance
test.

## Validation

1. `go test ./stdlib/...` succeeds without importing or building any package
   below `compiler/` or `internal/`.
2. The permanent import guard fails when a test fixture adds a
   `hexal/compiler/...` or `hexal/internal/...` import below `stdlib/`.
3. `stdlib/manifest.Modules()` returns defensive copies and its validation
   rejects duplicate module IDs, paths, exported names, runtime symbols, and
   unknown enum values.
4. The compiler adapter reproduces every Phase 0 module, type, function,
   parameter/result kind, runtime symbol, intrinsic, component, and error
   behavior.
5. Every manifest intrinsic has exactly one compiler mapping and every mapping
   is used by exactly one or more declared functions; an unknown intrinsic
   fails the test-owned compiler-data integrity validation.
6. `compiler/specdata.Validate()` retains one graph containing library
   functions plus builtin methods, constructors, components, dependencies,
   targets, error kinds, operators, ranks, widenings, and conversions.
7. Crafted validation fixtures still reject a runtime symbol shared by a
   builtin method and a standard-library function with incompatible ownership
   or behavior.
8. Crafted validation fixtures still reject a standard-library function whose
   component lacks the required ABI declaration/definition files.
9. `CoreTypeID` and `TypeID` remain compiler-owned; neither `compiler/types`
   nor `compiler/specdata` is imported by the standard library.
10. Runtime template names and SHA-256 hashes are identical before and after
   relocation, with exactly one owner per name.
11. `file`, `network`, `process`, `signal`, and `terminal` reside only under
    `stdlib/runtime`; `handle`, `time`, `seek`, and `io` reside only under
    `compiler/generator/packages`.
12. `git ls-files --eol` reports `i/lf w/lf` for every standard-library runtime
    asset and `.gitattributes` pins `stdlib/runtime/* text eol=lf`.
13. Trap, ErrorKind, and runtime-header guards consume both template owners,
    reject an injected bad stdlib template, and assert non-empty checked
    populations.
14. The snippet-manifest rebuild is an empty diff after every relocation
    phase; generated C/header contents, artifact keys, and diagnostics are
    unchanged.
15. `std/...` module identities and `stdlib/std/<path>` artifact keys are
    unchanged; existing import-resolution behavior passes untouched.
16. A program using no standard-library module emits byte-identical artifacts.
17. `go test ./...` and `go vet ./...` pass without an external C toolchain;
    the tagged C23 suite compiles and runs the same fixtures with the same
    output, exit status, and trap text.
18. No live comment or active specification assigns standard-library runtime
    assets to `compiler/corelib/runtime`; archived specs remain unchanged.
19. `docs/reference.md` remains unchanged after explicit review because no
    syntax or semantics changed.

## Extraction readiness

This RFC does not create another repository, but its end state deliberately
makes that later operation mechanical:

1. move `stdlib/` to the new repository;
2. give its Go packages a versioned module path;
3. update the compiler's three import paths;
4. pin a compatible library version in the compiler build.

No standard-library source, declaration schema, runtime asset, or validation
rule must be redesigned at that point. Version negotiation and independent
release policy require a future specification.

## Implementation readiness

**Deferred.** The design has no open language or architecture decisions, but
its current benefit is organizational rather than user-visible. Reconsider it
when the standard library is scheduled to move into a separate repository. At
that point, first re-verify the declaration and runtime-template inventories
against the current tree; any generated artifact or manifest hash change is a
defect, not a baseline update.
