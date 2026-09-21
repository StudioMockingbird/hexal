# RFC 0228: Central Compiler Configuration

- Kind: Architecture Decision Record (ADR)
- Status: Open Discussion; proposed; implementation not started
- Created: 2026-09-21
- Scope: move every compiler-owned tunable policy, ABI fact, generated-runtime
  contract, limit, default, and target/build identity into one authoritative
  configuration package
- Origin: RFC 0224's fixed `Error` header capacity exposed the wider problem:
  values that change generated layout or compiler behavior are scattered
  across parser, checker, generator, compiler, and driver packages
- Depends on: the in-memory compiler boundary, target-profile contract,
  runtime-pack contract, Project defaults, and the current `Error`, String,
  generated-C, and diagnostic contracts
- Coordinates with: RFC 0224 (inline bounded text), RFC 0213/RFC 0214
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

This makes a change such as RFC 0224's:

```text
ErrorKind.Other(header: String<64>)
```

easy to implement inconsistently. The `64` can affect type identity, C layout,
error size, generated helpers, foreign ABI, and manifests. It must have one
named owner:

```go
const ErrorHeaderByteCapacity uint64 = 64
```

Generated C must be rendered from that same fact rather than repeating `64` in
a template.

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
| RFC 0224 error header capacity | `ErrorHeaderByteCapacity` |
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
   support packages.
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

## Open questions

1. Should the package be `compiler/config` as proposed, or should its API be
   hidden under an `internal` path while still being shared by the driver?
2. Which target facts should be canonicalized in config versus retained in
   `compiler/types` as language type identity?
3. Should stable diagnostic strings live in config, or in the earliest phase
   that owns the diagnostic with config holding only size and budget values?
4. Which runtime layout values are truly configurable, and which must be
   represented by concrete types so changing them requires a dedicated spec?
5. How should configuration changes be represented in build identity when they
   alter generated text but not runtime ABI?
6. Should the migration land as one cross-package change or as dependency,
   target, parser, runtime, and driver slices with an invariant after each
   slice?

## Validation

This section is exhaustive. The implementation is complete only when:

- `compiler/config` exists and has no filesystem, environment, process,
  compiler-stage, or generated-source dependency.
- Every initial inventory item has exactly one configuration owner.
- Every package-level `const` and `var` in the scoped compiler and driver tree
  is classified, and every classified configuration item is in `compiler/config`.
- No exported mutable map, slice, registry, or global configuration object is
  exposed by the package.
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

## Implementation readiness

The architecture is ready for implementation after the package ownership and
open-question decisions are closed. The inventory and classification audit is
the first implementation task; no individual constant should be moved before
its contract owner and impact class are recorded.

