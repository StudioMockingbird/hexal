# RFC 0228: Central Compiler Configuration

- Kind: Architecture Decision Record (ADR)
- Status: Implemented. All four phases landed, and every Validation bullet was
  verified on the tree: `compiler/config` exists and imports only the standard
  library; the eleven declarations the RFC 0230 inventory classified are moved
  into it, each with its old declaration deleted rather than aliased, so no
  name has two owners; the fourteen generated-C spellings were already removed
  by RFC 0231, which this RFC's own Phase 3 anticipated; the six mutable
  exports are resolved, with the two `snippets` lists left exported exactly as
  the plan permits; every migrated generated value has a generated-C text
  assertion; and `docs/reference.md` was reviewed and is unchanged, which is
  what this RFC's own "Does not update" line requires
- Created: 2026-09-21
- Scope: move every compiler-owned tunable policy, ABI fact, generated-runtime
  contract, limit, default, and target/build identity into one authoritative
  configuration package
- Origin: RFC 0224's fixed `Error` capacities exposed the wider problem:
  values that change generated layout or compiler behavior are scattered
  across parser, checker, generator, compiler, and driver packages.
  **Corrected 2026-09-21 against the implemented tree:** the Go-side
  capacities are *not* scattered — see Problem — but their generated-C
  spellings are, in fourteen places
- Depends on: RFC 0230 (arc foundations), the in-memory compiler boundary, target-profile contract,
  runtime-pack contract, Project defaults, and the current `Error`, String,
  generated-C, and diagnostic contracts
- Coordinates with: RFC 0224 (byte-oriented strings, implemented and
  archived), RFC 0213/RFC 0214
  (target-qualified runtime packs), RFC 0158 (allocation tracking), and all
  active specifications that define a numeric limit, default, ABI value, or
  stable diagnostic
- Does not update: Hexal syntax or semantics by itself, `docs/reference.md`,
  or user-facing Project configuration beyond preserving existing behavior

## Decision summary

Create one compiler-owned package at:

```text
compiler/config
```

The package is the sole source of truth for compiler and generated-runtime
values that are intentionally adjustable by maintainers. It contains no
filesystem access, environment reads, host probing, tool execution, generated
source, or dependency on compiler stages.

The package exposes immutable constants and value-returning accessors. It must
not expose mutable package maps, slices, registries, or configuration objects.
The compiler and driver consume the package; they do not duplicate its values
in local `const`, `var`, literals, or generated templates.

The package is not a user configuration file and does not turn every internal
constant into a `Project` field. `Project` remains the explicit build-time
input API. Configuration values provide its defaults and define compiler-owned
invariants; a user may override a value only when an existing language or
build contract already authorizes that override.

## Problem

Values that can change behavior or generated artifacts currently live in many
places. Examples include:

```go
// compiler/project.go
const configPageSize = 4096

// compiler/runtimeabi.go
const RuntimeABIVersion uint32 = 1

// compiler/parser/parser.go
const maxSyntaxDepth = 128

// internal/driver/frontend.go
const inspectionByteLimit = 64 << 20
const inspectionTimeout = 30 * time.Second
```

Other values are hidden in generator defaults, target registries, runtime
dependency names, generated C layouts, diagnostic strings, C header lists,
foreign-language defaults, mode tables, and runtime component policies.

### The motivating example, corrected

RFC 0224 is implemented, and its capacities are **already single-owned** on the
Go side. `compiler/types/collections.go:156-166`:

```go
const (
	ErrorHeaderCapacity  = 128
	ErrorMessageCapacity = 256
)

var (
	ErrorHeaderText  = builtinInlineString(ErrorHeaderCapacity)
	ErrorMessageText = builtinInlineString(ErrorMessageCapacity)
)
```

The values are 128 and 256, not 64, and each sits adjacent to the type it
defines. An earlier revision of this RFC cited `64` and proposed moving it to
config; that example is withdrawn, because the problem it describes was solved
when RFC 0224 landed.

### The real duplication, which is on the C side

The generated **C spelling** has no single owner. `hex_string_128` and
`hex_string_256` appear as hard-coded literal strings in **fourteen places**
across `compiler/generator/print.go` and six runtime templates
(`error.h`, `file.c`, `io.c`, `network.c`, `process.c`, `signal.c`):

```go
// compiler/generator/print.go:162
result.WriteString("    hex_string_128 header = hex_error_kind_header(value->hex_m_kind);\n")
```

Change `ErrorHeaderCapacity` to 192 and all fourteen are silently wrong. This
is the actual instance of "generated C must be rendered from the same fact
rather than repeating it in a template", it is verified, and it is the
strongest concrete case this RFC has.

Whether it needs a new `config` package to fix is a separate question — see
Open questions 7.

## What counts as configuration

A declaration belongs in `compiler/config` when changing it intentionally can
change one or more of:

- accepted or rejected source programs;
- a stable diagnostic or diagnostic budget;
- a default exposed through `Project` or the build driver;
- generated C or header text;
- generated C layout, size, alignment, calling convention, or ownership;
- runtime ABI compatibility;
- runtime dependency demand, identity, or link order;
- target qualification or foreign ABI interpretation;
- a resource limit, timeout, capacity, or safety bound; or
- reproducibility, build identity, or runtime-pack selection.

Every such declaration must have one named configuration entry and one owner.
The name must express its unit and semantic role:

```go
ErrorHeaderByteCapacity // bytes, not a vague ErrorHeaderSize
MaxSyntaxDepth          // nesting depth, not MaxDepth
ForeignInspectionTimeout
RuntimeABIVersion
```

The package must not contain values merely because they are exported, global,
or easy to reach. A value is not configuration solely because it uses `const`
or `var`.

## What does not count as configuration

These remain local to the implementation unless a specification proves that
they are a policy or contract:

- loop counters and temporary variables;
- local builder capacities and allocation hints;
- compiled regular expressions;
- AST traversal state and caches;
- internal sentinel values with no cross-package or generated-C meaning;
- test fixtures, benchmark inputs, and test timeouts;
- lookup tables that implement a fixed algorithm rather than a language rule;
- generated strings assembled from configuration values; and
- values whose only purpose is to avoid repeating a nearby expression.

For example, a local `strings.Builder` capacity is not configuration, while a
header-inspection byte budget is configuration. A `MatchScalarTag` sentinel is
not configuration unless it is part of a generated representation or an
inter-package contract.

The migration must classify **every** package-level `const` and `var` in the
compiler and driver. It must either move the declaration, document its local
classification, or delete it as duplicated/stale. “Move every const” is not a
license to turn implementation mechanics into public policy.

## Blocking: resolved by RFC 0230

RFC 0230 settles the arc-wide contracts this RFC cannot settle alone: the
ownership map, the import graph, the target-semantics split, the impact
classification, the migration order, and the inventory ledger format. It is a
precondition for this RFC, not a companion to it.

What RFC 0230 decides that changes this RFC:

- **Scope narrows to tunable scalars.** Target facts move to `specdata`, not
  config. Diagnostic wording moves nowhere. Type identity stays in
  `compiler/types`.
- **Diagnostics are settled** — Open questions 3 is closed: wording stays in
  the owning phase.
- **Target facts are settled** — Open questions 2 is closed: identity in
  `types`, semantic facts in `specdata`, qualification in the driver.
- **The inventory is a checked-in ledger**, `docs/specs/0230-inventory.md`,
  with a fixed column set and eight classifications. Only `tunable`
  automatically belongs to config.
- **The impact table is shared**, so this RFC's own table is superseded by
  RFC 0230's.

The record below is retained because it names the concrete conflicts RFC 0230
resolves.

## The ownership conflict RFC 0230 resolves

This RFC and RFC 0229 both claim the same domains, and the tree already has
multiple owners for several of them. Verified on 2026-09-21:

| Domain | Owner today | RFC 0228 claims | RFC 0229 claims |
|---|---|---|---|
| Target identity | `compiler/types/target.go` | config | `TargetSpec` |
| Target semantic facts | `compiler/profile.go` (private `targetProfile`) | config | `TargetSpec` |
| Target qualification | `internal/driver/profile.go` (`qualifiedTriple`) | config | `TargetSpec.Qualified` |
| Runtime dependency identity | `compiler/runtime_dependency.go` | config | `ComponentSpec` |
| Runtime ABI version | `compiler/runtimeabi.go` | config | "relationship" |
| Core-library contracts | `compiler/corelib` (exported mutable map) | — | `FunctionSpec` |
| Error capacities | `compiler/types/collections.go` | config | `ErrorKindSpec` |

Two things follow.

**The same fact cannot have two new homes.** Without an agreed table, this
refactor replaces today's scattered duplication with a new duplication between
`config`, `specdata`, and the packages that keep their own copies. That is a
worse outcome than the status quo, because the duplication would then be
sanctioned by two specifications.

**Some of these splits are deliberate and should survive.**
`compiler/profile.go` carries a stated rule — *"a new fact enters here only
when checking or generation consumes it"* — and it is private. Target
*qualification* is not a language fact at all: it depends on the installed
backend, runtime-pack availability, and host state, so it belongs to the
driver whatever happens to target identity. A migration that collapses these
because they share the word "target" would lose a boundary that is currently
correct.

An ownership table agreed across both RFCs is a precondition for either.

## Package design

### Dependency direction

`compiler/config` imports only the Go standard library, if required for types
such as `time.Duration`. It imports no compiler package, parser, checker,
generator, driver, target type, or filesystem package.

The intended direction is:

```text
compiler/config
    ^
    |
types, lexer, parser, checker, generator, compiler, internal/driver
```

This prevents configuration from inheriting stage behavior and avoids import
cycles. If a configuration value currently depends on a compiler type, the
type must move to config or the dependency must be represented by a primitive
configuration identity.

### Immutable access

Scalar values may be constants:

```go
package config

const (
	ErrorHeaderByteCapacity uint64 = 64
	PageSizeBytes           uint64 = 4096
	MaxSyntaxDepth          uint32 = 128
	RuntimeABIVersion       uint32 = 1
)
```

Structured policy must be returned by value or through a copy-producing
function:

```go
type TargetProfile struct {
	Identity      string
	ToolchainTriple string
	PointerWidth  uint8
	SizeWidth     uint8
	LittleEndian  bool
}

func TargetProfileFor(identity string) (TargetProfile, bool)
func TargetProfiles() []TargetProfile // returns a new slice
```

The package must not expose this:

```go
var TargetProfiles map[string]TargetProfile // forbidden
```

No caller may mutate compiler policy accidentally or race with another
compilation. Returned slices, maps, or byte buffers must be fresh copies, or
the API must use fixed-size arrays and values.

### Naming and units

Every numeric configuration value has an explicit unit in its name or type:

```go
TaskStackReserveBytes
TaskStackCommitBytes
ForeignInspectionByteLimit
ForeignInspectionTimeout
ErrorHeaderByteCapacity
```

Names such as `Size`, `Limit`, `Count`, `Timeout`, and `Version` alone are not
permitted for new configuration entries. Existing ambiguous names are renamed
only with a compatibility audit of diagnostics, generated text, and tests.

### No mutable runtime configuration

The package does not provide setters, global initialization hooks, environment
overrides, or `SetConfig` functions:

```go
// forbidden
config.SetMaxSyntaxDepth(256)
config.Current.MaxSyntaxDepth = 256
```

Compiler behavior must be deterministic for equal source maps, entrypoint, and
Project values. A maintainer changes the package source and reviews the
resulting generated-C, ABI, diagnostic, and manifest impact.

## Initial configuration inventory

The implementation must begin with an inventory generated from the tree. The
following entries are the initial known migration set; the audit may discover
additional entries, but may not silently omit these classes.

### Core compiler and generated-runtime contracts

| Current class | Configuration responsibility |
|---|---|
| `RuntimeABIVersion` | generated-runtime/pack ABI version |
| `configPageSize` | stack reserve/commit page-multiple rule |
| RFC 0224 error header/message capacities | already single-owned as `types.ErrorHeaderCapacity` (128) and `types.ErrorMessageCapacity` (256); the open item is their **generated-C spellings**, not the Go values |
| fixed runtime message/header capacities | named byte capacities, where a fixed layout uses them |
| runtime dependency names | canonical logical dependency identities |
| generated C helper/layout constants | C representation and helper contract values |
| runtime component demand rules | dependency/component policy, not duplicated string tests |

### Defaults and limits

| Current class | Configuration responsibility |
|---|---|
| Task stack reserve default | `DefaultTaskStackReserveBytes` |
| Task stack commit default | `DefaultTaskStackCommitBytes` |
| parser recursion/syntax budget | `MaxSyntaxDepth` |
| interpolation recursion budget | `MaxInterpolationDepth` |
| foreign-header byte budget | `ForeignInspectionByteLimit` |
| foreign-header process timeout | `ForeignInspectionTimeout` |
| staging directory convention | driver staging-name policy, if it remains a contract |
| default foreign C dialect | `DefaultForeignDialect` |

### Target and foreign ABI policy

Target identity, target facts, toolchain triples, pointer/size widths, endian
facts, supported native facilities, foreign C scalar spellings, and qualified
target registries must have one authoritative data source. The compiler and
driver may derive different views from that source, but may not maintain
independent target registries that can disagree.

The package must distinguish:

```text
target identity       compiler-owned semantic key
target facts          pointer width, endian, ABI properties
toolchain facts       Clang/Zig triple and backend qualification
runtime-pack facts    archive paths, hashes, and pack contents
```

Host paths and archive contents remain driver-owned. They do not move into the
core compiler configuration package.

### Tables and stable strings

The migration must classify and centralize policy tables such as:

- C standard-header and facility requirements;
- foreign dialect acceptance;
- mode defaults and mode option policy;
- runtime dependency identities and ordering;
- stable generated-runtime error messages;
- compiler-owned moved/removed namespace diagnostics;
- target qualification records; and
- canonical type or operation names when changing them changes language
  behavior or diagnostics.

Large implementation tables may remain in their owning package when they are
algorithm data, but the spec must record why they are not configurable.

## Project and external build settings

`Project` remains the only core compiler input for caller-selected build-time
settings. The zero value continues to select defaults:

```go
compiler.Compile(sources, entrypoint, compiler.Project{})
```

The effective-value logic moves to config-backed helpers so defaulting is not
duplicated:

```go
reserve := project.TaskStackReserve
if reserve == 0 {
	reserve = config.DefaultTaskStackReserveBytes
}
```

The driver may have command-line settings such as compiler path, target, mode,
source root, and runtime-pack root. Those are user inputs or driver state, not
global compiler configuration. Their defaults and policy limits may use
`compiler/config`; their filesystem paths and process state remain in
`internal/driver`.

The config package must never receive or store a `Project`, source map,
entrypoint, host path, environment, backend executable, or runtime-pack
filesystem.

## Generated C contract

Every configurable value used by generated C has one rendering path. For
example, RFC 0224 must not contain independent layout literals:

```go
capacity := config.ErrorHeaderByteCapacity
```

```c
typedef struct {
    uint8_t data[HEX_ERROR_HEADER_BYTES];
} hex_error_header;
```

The generated C may use a literal when the value is rendered into a complete
translation unit, but the generator must obtain it from config. A helper must
not reintroduce a second hard-coded value in a template.

When a value changes generated C, validation must assert the emitted text and
its declaration/use ordering. When it changes a C layout or runtime contract,
the runtime ABI and target-pack identity rules apply.

The config package does not emit a universal header containing every setting.
Only values required by a selected generated component are rendered.

## Versioning and build identity

Configuration changes are classified by impact:

| Change | Required action |
|---|---|
| diagnostic wording only | update diagnostic tests and owning spec |
| source acceptance or rejection | update checker/parser contract and `docs/reference.md` |
| generated C text only | update text assertions and snippet manifest after review |
| C layout, calling convention, ownership, or runtime helper contract | increment `RuntimeABIVersion` and refresh packs |
| target/toolchain qualification | update target-pack identity and native probes |
| runtime dependency or link input | update manifest, demand tests, and build identity |
| resource default/limit | update validation, diagnostics, and benchmark expectations |

The compiler must not silently derive a configuration version from Go package
build metadata. The owning change records the impact explicitly.

Any value that participates in generated output, runtime dependency selection,
or build identity must be included through the existing deterministic artifact
or manifest identity path. Equal source and Project inputs must not produce
different behavior because of mutable config state.

## Migration rules

1. Inventory all package-level `const` and `var` declarations in compiler,
   generator, parser, checker, types, corelib, backend, driver, and runtime
   support packages. Measured on 2026-09-21: **107 top-level `const`/`var`
   declarations**, concentrated in `compiler/generator` (132 entries),
   `compiler/types` (87), and `internal/driver` (63). The inventory must be a
   **checked-in ledger** with one row per declaration — current symbol, file,
   classification, new owner, new name, impact class, migration phase — not a
   prose commitment to audit. A classification that exists only in a
   reviewer's head cannot satisfy the validation item that requires it.
2. Classify each as configuration, fixed algorithm data, mutable state, test
   data, generated asset, or obsolete duplication.
3. Move every configuration item into `compiler/config`.
4. Replace consumers with config references or typed accessors.
5. Delete old declarations and duplicated literals; do not leave aliases that
   permit two values to drift.
6. Preserve public names only where they are already part of the exported Go
   API; otherwise the config package becomes the single owner.
7. Keep the core compiler in-memory and host-neutral.
8. Update affected specs and `docs/reference.md` only when semantics or a
   language-visible C contract changes.

The migration is complete only when a repository search finds no second owner
for each inventory item. A search-based audit is part of validation, not a
manual confidence claim.

## Non-goals

- User-editable configuration files or project manifests.
- Environment-variable configuration of compiler semantics.
- Runtime mutation of compiler policy.
- Moving local variables, loop state, caches, or every implementation constant
  into a global package.
- Moving host paths, filesystem discovery, process execution, or toolchain
  selection out of the driver.
- A new analyzer pass or compiler pipeline stage.
- Automatically versioning every config change as a runtime ABI change.

## Settled questions

1. Settled by RFC 0230 Decision 9: the package is `compiler/config`, at that
   exact path, with no `internal/` segment. A `compiler/internal/config` would
   be unimportable by `internal/driver`, which reads these values.
2. Settled by RFC 0230 Decisions 1 and 3: **no** target fact goes to config.
   Identity stays in `compiler/types`, semantic facts move to
   `compiler/specdata`, and qualification stays in `internal/driver`.
3. Settled by RFC 0230 Decision 7: diagnostic wording stays in the phase that
   emits it. Config holds no strings.
4. Settled by RFC 0230 Decision 1 and the ledger: the only layout-affecting
   values that move are the two `Error` capacities. C layouts themselves are
   `generated-C fact`s and stay in `compiler/generator`; the ledger classifies
   all three such declarations.
5. Settled by RFC 0230 Decision 8: the owning change declares its impact row,
   and review checks that declaration against the diff. A generated-text-only
   change rebuilds the snippet manifest and is reviewed by artifact family; it
   does not touch the ABI version.
6. Settled by RFC 0230's migration order: slices, with an invariant after
   each. The ledger is produced first; a slice is complete when every row it
   claims is struck through.

**No open questions remain.** Scope is now the eleven declarations the ledger
identifies, plus the six mutable exports and the fourteen hard-coded
`hex_string_128`/`hex_string_256` C spellings.

## Validation

This section is exhaustive. The implementation is complete only when:

- `compiler/config` exists and has no filesystem, environment, process,
  compiler-stage, or generated-source dependency.
- Every initial inventory item has exactly one configuration owner.
- Every package-level `const` and `var` in the scoped compiler and driver tree
  is classified, and every classified configuration item is in `compiler/config`.
- No exported mutable map, slice, registry, or global configuration object is
  exposed by the package. Note one instance that exists today and must be
  resolved by whoever owns it: `compiler/corelib/corelib.go:58` declares
  `var Modules = map[string]Module{...}`, an exported mutable registry any
  importer can rewrite.
- Structured values returned by accessors are **deeply** immutable to callers.
  A shallow struct copy whose fields are slices or maps is still mutable
  through those fields, so records either use fixed-size arrays, unexported
  fields with accessor methods, or explicit deep copies.
- Returned structured configuration values cannot mutate global compiler
  policy.
- `Project{}` preserves all current defaults and deterministic behavior.
- Explicit Project overrides preserve their current validation and diagnostics.
- RFC 0224's error-header capacity is read from config in type, checker,
  generator, and generated-C layout paths; no duplicate capacity literal
  remains.
- Parser, checker, generator, compiler, backend, and driver consumers no
  longer duplicate migrated values.
- Target identities, target facts, and driver qualification records cannot
  silently disagree for a qualified target.
- Generated C text uses the configured values only in components that need
  them.
- Configuration changes affecting generated output move the correct snapshot
  and snippet-manifest artifacts; unrelated artifact families do not move.
- Configuration changes affecting C layout or runtime ABI increment the ABI
  and reject stale runtime packs.
- Ordinary `go test ./...` passes without an external C toolchain.
- Generated-C text assertions cover every migrated generated value.
- The compiler remains string-in/string-out and performs no host inspection.
- `docs/reference.md` is reviewed and either synchronized or explicitly
  verified unchanged before the spec is marked implemented.

## Implementation plan

Four phases. Scope is fixed by `docs/specs/0230-inventory.md`: eleven
declarations move, six mutable exports are resolved, and fourteen hard-coded C
spellings lose their second owner. Nothing else is in scope.

### Phase 1 — create the package, move nothing

Create `compiler/config` with no declarations, and a doc comment stating the
one rule: it imports only the Go standard library, and holds tunable policy —
not type identity, not diagnostics, not target facts.

*Verify:* `TestArcPackagesAreAtDecidedPaths` stops skipping and passes;
`go build ./...` unchanged; no artifact moves.

### Phase 2 — move the eleven

One declaration per commit, in dependency order so no intermediate state has
two owners:

| Order | Symbol | From | Impact row |
| --- | --- | --- | --- |
| 1 | `RuntimeABIVersion` | `compiler/runtimeabi.go` | implementation only |
| 2 | `configPageSize` | `compiler/project.go` | resource limit |
| 3 | `maxSyntaxDepth` | `compiler/parser/parser.go` | resource limit |
| 4 | `maxInterpolationDepth` | `compiler/lexer/lexer.go` | resource limit |
| 5 | `inspectionByteLimit`, `inspectionTimeout` | `internal/driver/frontend.go` | resource limit |
| 6 | `DefaultTaskStackReserve`, `DefaultTaskStackCommit` | `compiler/generator/concurrency_component.go` | resource default |
| 7 | `MaxInlineStringCapacity` | `compiler/types/collections.go` | resource limit |
| 8 | `ErrorHeaderCapacity`, `ErrorMessageCapacity` | `compiler/types/collections.go` | **C layout** |

Entries 7 and 8 move the *value* only. `ErrorHeaderText`, `ErrorMessageText`,
and the inline-string constructor stay in `compiler/types`, because they are
type identity and Decision 1 keeps identity there.

Names gain their unit per the naming rule: `PageSizeBytes`,
`ForeignInspectionByteLimit`, `DefaultTaskStackReserveBytes`.

*Verify per commit:* the old declaration is deleted, not aliased — a grep for
the old name returns only the config definition. Generated C is byte-identical
and the snippet manifest does not move, for every entry except 8, which
declares the C-layout row and is reviewed against its artifact diff.

### Phase 3 — remove the fourteen duplicate C spellings

`hex_string_128` and `hex_string_256` appear as literal text in
`compiler/generator/print.go` and six runtime templates. Replace each with a
value derived from the capacity:

```go
func inlineStringCName(capacity uint64) string {
	return fmt.Sprintf("hex_string_%d", capacity)
}
```

*Verify:* no `hex_string_128` or `hex_string_256` literal remains in Go
source; generated C byte-identical; manifest unmoved. This phase is where the
RFC's original drift evidence is actually fixed.

If RFC 0231 has already landed, this phase is smaller: the spellings move into
template models rather than into Go string building.

### Phase 4 — resolve the six mutable exports

Not a move. Each is decided individually:

| Symbol | Resolution |
| --- | --- |
| `corelib.Modules` | unexport behind an accessor; RFC 0229 slice 3 replaces it entirely |
| `types.ErrorKindVariantNames` | fixed-size array, or unexported with an accessor |
| `backend.RequiredHeaders`, `RequiredFacilities` | return a copy, or unexport |
| `snippets.RequiredReservedWords`, `RequiredFeatures` | workbench tooling; lowest priority, may stay |

*Verify:* no caller can rewrite compiler policy through an exported map or
slice; `go test ./...` passes.

### Rollback boundary

Every phase is independently revertible. Phases 1-3 have byte-identical output
as their proof, so a revert restores a known-good tree. Phase 4 changes Go API
shape within the module and is the only phase whose revert touches callers.

## Implementation readiness

The architecture is ready for implementation after the package ownership and
open-question decisions are closed. The inventory and classification audit is
the first implementation task; no individual constant should be moved before
its contract owner and impact class are recorded.

