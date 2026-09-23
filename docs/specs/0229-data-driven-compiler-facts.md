# RFC 0229: Data-Driven Compiler Facts

- Kind: Architecture Decision Record (ADR)
- Status: Partially implemented. Slices 0-4 of the implementation plan have
  landed: `compiler/specdata` holds records for runtime components and
  dependencies, target facts, core-library modules, type constructors, and
  methods, with `Validate()` and the conformance guards enforced. The registry
  is not yet the sole authority — the repository-wide duplicate-owner audit
  recorded in `docs/status.md` names each consumer that still owns a migrated
  fact, and the Validation section holds only when those close. RFC 0230 is
  closed, so this is no longer blocked on it.
- Created: 2026-09-21
- Scope: make compiler-owned language and runtime facts declarative, typed,
  centrally registered, validated, and reusable across checking, generation,
  diagnostics, and build integration
- Origin: follow-up to RFC 0228; configuration centralization removes tunable
  values from scattered packages, while this RFC removes duplicated compiler
  knowledge from handwritten registries and dispatch tables
- Depends on: RFC 0230 (arc foundations), the forward-only compiler pipeline,
  fail-closed dispatch rules, the in-memory compiler boundary, RFC 0228
  (central compiler configuration),
  and the current type, core-library, runtime-component, target, diagnostic,
  and generated-C contracts
- Coordinates with: RFC 0052 (C backend), RFC 0055 (build driver), RFC 0158
  (allocation tracking), RFC 0213/RFC 0214 (runtime packs), RFC 0224
  (byte-oriented strings, implemented and archived), RFC 0227 (vendored
  utf8proc), and every specification that
  introduces a compiler-owned type, method, operator, diagnostic, component,
  or C layout
- Does not update: Hexal syntax or semantics by itself, user configuration,
  the compiler pipeline shape, or the explicit control flow of parser,
  checker, and generator behavior

## Decision summary

Introduce a typed declarative fact registry under:

```text
compiler/specdata
```

The registry becomes the single source of truth for compiler-owned facts that
are currently repeated across `compiler/types`, `compiler/corelib`, checker
dispatch, generator dispatch, runtime-component selection, diagnostics,
foreign C mappings, target records, and generated-C layouts.

The registry contains facts, not arbitrary executable behavior. It is compiled
into the compiler as immutable typed data. It does not require a runtime
reflection system, a user-editable manifest, dynamic scripting, or a second
compiler stage.

The compiler continues to use explicit switches and direct functions for
semantic behavior and lowering. Data selects and describes supported cases;
missing or inconsistent data is a compiler error, never an implicit fallback.

## Why this is separate from RFC 0228

RFC 0228 centralizes values such as:

```go
const ErrorHeaderByteCapacity = 64
const MaxSyntaxDepth = 128
const RuntimeABIVersion = 1
```

This RFC centralizes relationships and schemas such as:

```text
String has byte_length() -> Size
String uses the string runtime component
String is an owning pointer-sized handle
String equality compares logical bytes
String.free requires a Heap
```

The distinction is:

```text
compiler/config  = tunable values, limits, defaults, ABI numbers
compiler/specdata = language, runtime, target, and generator facts
```

`specdata` may consume configuration values, but `config` must not depend on
checker, generator, or `specdata` packages. Neither package reads files,
environment variables, or host state.

## Data-driven principles

### Facts are declarative

A type, method, operator, runtime component, or target record should be
described once:

```go
type BuiltinTypeSpec struct {
	Name             string
	CSpelling        string
	Representation   Representation
	Ownership        Ownership
	CopyMode         CopyMode
	Comparable       bool
	Hashable         bool
	HasFree          bool
	RuntimeComponent ComponentID
}
```

Consumers query the record instead of reproducing the same facts in separate
maps or switches.

### Behavior remains explicit

This RFC does not replace semantic code with an interpreter for metadata.
Complex behavior remains direct and fail-closed:

```go
switch node := expression.(type) {
case parser.BinaryExpression:
	return emitBinary(node)
case parser.CallExpression:
	return emitCall(node)
default:
	return unknownExpressionDiagnostic(node)
}
```

A table may describe which operators or methods are legal, but the checker and
generator still explicitly handle every supported syntax node and report an
unknown case as an `Unknown Error` owned by the compiler.

### One owner per fact

The following is forbidden:

```go
// checker
var stringMethods = ...

// generator
var stringMethods = ...

// corelib
var stringMethods = ...
```

There must be one registry record. Derived views may exist, but they must be
constructed from that record and must not be independently edited.

### Deterministic typed data

Fact records use typed identifiers and values. Names are not loosely typed
maps of `any` values. Registry construction must be deterministic, and all
lookup results must be immutable to callers.

Data-driven does not mean runtime-configurable. A maintainer changes a fact in
the compiler source, reviews the generated-C and diagnostic impact, and runs
the owning validation. Programs cannot change compiler semantics by supplying
data at runtime.

## Blocking: resolved by RFC 0230

RFC 0230 is a precondition for this RFC. It settles six things this RFC leaves
open, and two of its decisions change this RFC's design materially:

| This RFC's open question | RFC 0230's decision |
|---|---|
| Q1 package split | one `specdata`; a target/ABI split only if target facts grow validation |
| Q3 diagnostic templates | **no templates** — wording stays in the owning phase; `specdata` holds identity and stability only |
| Q5 demand predicates or functions | **Go functions** — component demand stays the 24 explicit builders; only metadata becomes data |
| Q6 generic specialization | **constructor records, never specializations** — see below |
| Q7 which changes bump the ABI | the shared impact table; the owning change declares its row |
| first slice | runtime components, because the record needs no `Type` values |

Two consequences for the domains below.

**`specdata` stores primitive identifiers, never `Type` values.** This is
forced by an import cycle, not chosen: `compiler/corelib` imports
`compiler/types` and stores live `Type` values, so a `types` that consumed a
`Type`-bearing `specdata` would cycle. A consumer needing a `Type` asks
`types` to resolve an ID.

**Generic types get constructor records, not specialization records.**
`String<N>` makes a per-specialization registry impossible, since N is
unbounded — but `String`'s invariant facts are the same at every capacity. A
record carries what is invariant (representation, copy mode, free mode,
component) and derives what is not (C name, layout, size). Domain 1 cannot be
implemented without this model and domain 8's layout records depend on it.

Domain 7 (diagnostics) is reduced to identity metadata. Domain 6's
`TargetSpec.Qualified` field is withdrawn — qualification is driver state, not
a language fact.

## The ownership conflict and import cycle RFC 0230 resolves

RFC 0228's *Blocking: the ownership map does not exist* applies here
unchanged: both RFCs claim target facts, runtime dependencies, diagnostics,
and the Error relationship, and the tree already has owners for each. That
table must be agreed before either package is created.

One conflict is sharper than an ownership question, because it is a
compile-time impossibility rather than a design preference:

```text
compiler/corelib imports compiler/types and stores live Type values
   (compiler/corelib/corelib.go:9)

if compiler/types consumes specdata for builtin type facts, and
specdata needs Type to express those facts, then

compiler/types -> specdata -> compiler/types     import cycle
```

The registry therefore cannot hold `compilerTypes.Type` values. It must store
primitive identifiers, and some adapter must resolve those identifiers to
canonical types after interning. That adapter — where it lives, when it runs,
and what it does when an identifier has no type — is not in this RFC and is a
precondition for domain 1.


## Registry domains

### 1. Built-in type specifications

The registry describes compiler-known types:

```go
type BuiltinTypeSpec struct {
	ID               TypeID
	SourceName       string
	CSpelling        string
	Representation   Representation
	Ownership        Ownership
	CopyMode         CopyMode
	FreeMode         FreeMode
	Comparable       ComparisonMode
	Hashable         bool
	RuntimeComponent ComponentID
}
```

It covers types such as:

```text
Int32, Int64, Byte, String, String<N>
Array, Slice, List, Dict
Heap, Pool, Stash
Error, Task, Channel, Mutex, Atomic
```

`Strand` no longer exists — RFC 0224 removed it — and `Rune` is removed
pending RFC 0227, which restores it together with `Grapheme` and three
cursors. Any registry landing before RFC 0227 must not carry records for
types the language does not have; any landing after must carry the ones it
adds.

The record drives facts such as:

- canonical name and C spelling;
- inline versus handle representation;
- shallow versus value copying;
- ownership and `free` eligibility;
- equality, ordering, and hashing eligibility;
- runtime-component demand; and
- foreign ABI visibility.

Canonical type identity remains owned by `compiler/types`. The registry does
not replace type interning or type operations.

RFC 0224's bounded text types must be represented explicitly in this model:

```text
String       owning dynamic handle
String<N>    inline value family with capacity N
```

The capacity-dependent portion must not be hidden in unrelated generator
conditionals.

### 2. Method and constructor specifications

Compiler-owned methods and constructors should have one signature record:

```go
type MethodSpec struct {
	Owner          TypePattern
	Name           string
	Parameters     []ParameterSpec
	Result         ResultSpec
	Failure        FailureMode
	RuntimeSymbol  string
	Component      ComponentID
	Allocation     AllocationMode
	Receiver       ReceiverMode
}
```

Example:

```go
MethodSpec{
	Owner:         ExactType("String"),
	Name:          "byte_length",
	Result:        ResultType("Size"),
	RuntimeSymbol: "hex_string_byte_length",
	Component:     ComponentString,
	Allocation:    NoAllocation,
}
```

The registry covers:

- method names and receiver kinds;
- parameter and result types;
- checked failure behavior;
- allocation and ownership behavior;
- emitted runtime symbols; and
- runtime component demand.

The checker remains responsible for source expressions, argument evaluation,
mutability, flow facts, and diagnostic locations. The generator remains
responsible for expression evaluation order and C lowering.

### 3. Core-library and standard-library contracts

The existing core-library table becomes a typed registry rather than a partial
ad hoc declaration table:

```go
type FunctionSpec struct {
	Module        ModuleID
	Name          string
	Parameters    []ParameterSpec
	Result        ResultSpec
	ErrorBehavior ErrorBehavior
	Builtin       BuiltinID
	RuntimeSymbol string
	Components    []ComponentID
}
```

One record can then drive:

- module import resolution;
- canonical signatures;
- builtin operation lookup;
- result/error union behavior;
- runtime demand; and
- generated entry-point selection.

This must not turn ordinary user functions into registry entries. The registry
is for compiler-owned and standard-library contracts only.

### 4. Operator and conversion matrices

Operator legality and implicit conversions are naturally declarative:

```go
type OperatorSpec struct {
	Operator        OperatorID
	Left            TypePattern
	Right           TypePattern
	Result          ResultSpec
	RuntimeHelper   string
	RequiresRuntime bool
}

type ConversionSpec struct {
	From          TypePattern
	To            TypePattern
	Implicit      bool
	Lossless      bool
	Allocation    AllocationMode
	RuntimeHelper string
}
```

This covers:

- numeric widening;
- pointer mutability weakening;
- slice mutability weakening;
- union injection;
- arithmetic and comparison operators;
- bitwise and shift rules; and
- explicit conversion helpers.

The checker must reject a missing matrix entry with a type diagnostic. The
generator must reject an impossible checked rule as a compiler error. No
unknown operator may silently acquire behavior from a default table entry.

### 5. Runtime-component specifications

Runtime components and their dependency demand become records:

```go
type ComponentSpec struct {
	ID                  ComponentID
	Files               []RuntimeFile
	RequiredCHeaders    []string
	RuntimeDependencies []DependencyID
	ABIRequirements     []ABIRequirement
	Demand              DemandRule
	PublicHeaderExports []string
}
```

Example:

```go
ComponentSpec{
	ID:                  ComponentString,
	Files:               []RuntimeFile{"hexal/string.h", "hexal/string.c"},
	RuntimeDependencies: []DependencyID{"utf8proc"},
	RequiredCHeaders:    []string{"stdint.h", "stddef.h"},
}
```

This makes RFC 0227's utf8proc demand a data fact instead of another
dependency-specific branch.

The registry can describe component metadata, but component source behavior
remains explicit C templates and Go lowering code. A component record cannot
execute arbitrary code or bypass demand checks.

### 6. Target and foreign-ABI specifications

Target identities and their semantic facts should have one declarative source:

```go
type TargetSpec struct {
	ID              TargetID
	OS              string
	Architecture    string
	ToolchainTriple string
	PointerWidth    uint8
	SizeWidth       uint8
	Endianness      Endianness
	ThreadModel     string
	NativeFeatures  FeatureSet
	Qualified       bool
}
```

This registry is shared by:

- compiler target validation;
- C scalar normalization;
- generated-C target branches;
- runtime-pack selection; and
- native driver qualification.

The registry does not own host paths, archive bytes, SDK locations, or process
state. Those remain driver-owned runtime-pack inputs.

Foreign C scalar mappings become target-qualified records:

```go
type CScalarMapping struct {
	CSpelling string
	HexalType TypeID
	Width     uint8
	Signed    bool
	Exact     bool
}
```

LP64 and LLP64 differences must be represented as target data, not scattered
conditionals in the importer.

### 7. Diagnostics and error contracts

Stable compiler diagnostics should have typed specifications where they are
cross-phase or contract-owned:

```go
type DiagnosticSpec struct {
	ID         DiagnosticID
	Category   DiagnosticCategory
	OwnerStage DiagnosticStage
	Template   string
	Stable     bool
}
```

Error-kind layout and runtime message contracts should likewise be registered:

```go
type ErrorKindSpec struct {
	ID             ErrorKindID
	Payload        []FieldSpec
	HeaderType     TypePattern
	RuntimeMessage MessageID
	Component      ComponentID
}
```

RFC 0224's header capacity remains owned by RFC 0228 configuration, while the
fact that `ErrorKind.Other` uses that configured type belongs in `specdata`.

The registry must not replace earliest diagnostic ownership. A diagnostic
record identifies the owning stage; it does not allow a later stage to report
an earlier-stage error.

### 8. Generated-C layout specifications

Observable C layouts should be described once:

```go
type LayoutSpec struct {
	ID         LayoutID
	CName      string
	Fields     []FieldSpec
	Alignment  AlignmentRule
	CopyMode   CopyMode
	FreeMode   FreeMode
	ABIVisible bool
}
```

Examples include:

```text
String
String<N>
Error
List<T>
Dict<K,V>
Slice<T>
```

`RuneCursor` was removed by RFC 0224 and is restored only by RFC 0227.

The generator renders declarations from layout facts, but specialized bodies,
checked arithmetic, evaluation order, and control flow remain explicit.

This is the mechanism that prevents one change from updating a Go type record
while leaving a conflicting C template or `size_of` rule.

## Registry validation

The package must validate itself before consumers use it. Validation includes:

- unique IDs, names, C spellings, runtime symbols, and component IDs;
- no method referring to an unknown owner or type;
- no operator referring to an unknown type pattern;
- no conversion cycle that is not explicitly permitted;
- no runtime component referring to an unknown dependency;
- no component dependency cycle;
- no generated symbol collision;
- every ABI-visible layout has a copy/free contract;
- every error kind has a valid payload and owning stage;
- every target has one identity and one foreign-ABI record;
- every supported compiler-owned builtin has a registry record; and
- every registry record is reachable from at least one consumer or is marked
  intentionally reserved.

Validation failures are compiler-development failures and must fail tests or
initialization deterministically. They must not become user diagnostics or
silently remove a feature.

## Consumer interfaces

Consumers should depend on narrow queries rather than the entire registry:

```go
specdata.BuiltinType(id)
specdata.Method(owner, name)
specdata.Operator(operator, left, right)
specdata.Conversion(from, to)
specdata.Component(id)
specdata.Target(id)
specdata.Layout(id)
specdata.Diagnostic(id)
```

Queries return immutable values or copies. A consumer must not mutate registry
records to customize behavior for one compilation.

The checker and generator may build per-compilation derived state from these
records. Derived state belongs to that compilation and is not global.

## Source representation

The first implementation uses typed Go declarations in `compiler/specdata`.
It does not introduce JSON, YAML, a schema compiler, reflection, or a build
step merely to express tables.

After the records and validation stabilize, a later spec may consider a
checked-in data format and generated Go source. That is not part of this RFC.
The source of truth must remain reviewable, deterministic, and available to
the in-memory compiler without filesystem access.

## Migration inventory

The implementation must audit and classify current declarations in:

```text
compiler/types
compiler/corelib
compiler/parser
compiler/lexer
compiler/checker
compiler/generator
compiler/profile.go
compiler/runtime_dependency.go
compiler/config
internal/backend
internal/driver
```

`compiler/runtimeabi.go` was inventoried but does not exist: the runtime ABI
version is a scalar policy value owned by RFC 0228 at `compiler/config`, and
`internal/driver/runpack.go` is its consumer.

Initial migration targets include:

- builtin type declarations and representation facts;
- core-library modules and function signatures;
- builtin methods and constructors;
- numeric widening and operator matrices;
- runtime component metadata and dependency demand;
- target profile facts and C scalar mappings;
- error-kind payload and header relationships;
- generated-C layout facts;
- stable runtime message ownership; and
- foreign dialect and required-header policy tables.

The migration must delete duplicated declarations after consumers switch. An
alias is acceptable only when it preserves an existing exported Go API while
leaving `specdata` as the sole storage owner.

## Interaction with RFC 0228

RFC 0228 owns scalar policy values:

```go
config.ErrorHeaderByteCapacity
config.MaxSyntaxDepth
config.RuntimeABIVersion
```

RFC 0229 owns their semantic relationships:

```go
specdata.ErrorKind("Other").HeaderType
specdata.Layout("Error")
specdata.Component("string").RuntimeDependencies
```

Changing a configuration value must automatically affect all consumers that
read the related fact. A consumer must not copy the default into specdata or
vice versa.

## Generated output and build identity

Data-driven records do not make generated output less contractual. When a
record changes:

- generated C text is asserted directly;
- declaration/use ordering remains validated;
- snippet manifest movement is reviewed by artifact family;
- C layout or ownership changes increment the runtime ABI when required;
- runtime dependency changes update pack manifests and build identity; and
- language-visible changes update `docs/reference.md` after behavior settles.

The registry itself is not embedded into generated programs unless a selected
runtime component needs a rendered value.

## Non-goals

- Replacing parser, checker, or generator control flow with generic dispatch.
- Adding reflection or runtime metadata to compiled Hexal programs.
- Making user programs able to register types, methods, operators, or runtime
  components.
- Making compiler semantics editable through JSON, YAML, environment variables,
  or project files.
- Moving ownership, lifetime, dataflow, or memory-safety analysis into tables.
- Moving generated C implementation bodies into a generic template engine.
- Adding a new analyzer pass.
- Replacing explicit unsupported-syntax diagnostics with table misses.

## Settled questions

1. Settled by RFC 0230 Decision 10: one `compiler/specdata` package, with its
   files split by domain. Cross-domain reference checking is the registry's
   purpose and wants one `Validate()`.
2. Settled by RFC 0230's migration order: type and method facts are **slice
   4**, last, precisely because they are the domain still moving. Components
   go first.
3. Settled by RFC 0230 Decision 7: **IDs, owning stage, and stability
   metadata only.** No templates; wording stays with its phase.
4. Settled as a **procedure rather than an answer**, because it is a
   judgement about reviewability that prose cannot decide. In slice 4, render
   one real layout from a record and place it beside the template it would
   replace; adopt records for layouts only where that comparison is favourable.
   It affects domain 8 alone and blocks no earlier slice.

   The expectation, recorded so it can be proved wrong: **not much layout
   should move.** `error.h` is 73 lines of which the struct declarations are
   about 16; the remaining 57 are 18 comment lines carrying design rationale a
   record cannot hold, three `static inline` helpers with real logic, plus
   include guards and headers. And `size_of<T>()` lowers directly to C
   `sizeof` (`generator/render.go:1210`), so the Go side never computes a
   layout and cannot disagree with the C — which removes the usual reason to
   model layout in data at all.

   RFC 0231 makes the comparison cheap: once all C emission is template-based,
   rendering a layout from a record means passing a field list to an existing
   template rather than replacing a mechanism.
5. Settled by RFC 0230 Decision 6: **explicit Go functions.** The 24 component
   builders keep their demand logic; only their metadata becomes data.
6. Settled by RFC 0230 Decision 4: **constructor records, never
   specializations.** Invariant facts live in the record; C name, layout, and
   size are derived from the arguments.
7. Settled by RFC 0230 Decision 8's impact table: a registry change bumps the
   ABI when it alters C layout, calling convention, ownership, or a helper
   contract — and the owning change declares that row.

## Validation

This section is exhaustive. The implementation is complete only when:

- `compiler/specdata` exists and imports no compiler stage, filesystem,
  environment, host, or process package.
- Every registry domain in this RFC has typed records and deterministic
  validation.
- Every compiler-owned type, method, constructor, core-library function,
  operator, conversion, runtime component, target fact, diagnostic contract,
  and ABI-visible layout in the migration scope has exactly one owner.
- No consumer keeps a duplicate authoritative table or hard-coded fact for a
  migrated record.
- Registry queries return immutable values or defensive copies.
- Registry validation rejects duplicate identities, missing references,
  cycles, symbol collisions, and incomplete ABI records.
- Missing registry data produces a compiler-development failure or structured
  `Unknown Error`; it never silently disables a feature or invents behavior.
- Parser, checker, and generator dispatch remains explicit and fail-closed.
- The core compiler remains string-in/string-out and performs no host access.
- RFC 0228 configuration values are consumed through their single owner and
  are not copied into independent registries.
- RFC 0224's Error header relationship has one data path from configuration to
  type identity, C layout, generated helpers, and validation.
- RFC 0227's utf8proc dependency demand is represented by component metadata,
  while archive paths and payloads remain driver-owned.
- Generated-C text assertions cover every migrated layout, method symbol,
  component include, dependency demand, and diagnostic contract.
- Snippet-manifest movement is confined to artifacts affected by the changed
  registry fact.
- `docs/reference.md` is reviewed and synchronized, or explicitly verified
  unchanged, before implementation is marked complete.
- Ordinary `go test ./...` passes without an external C toolchain.

## Implementation plan

Four slices, ordered so that each is independently shippable and the slice
touching the import cycle comes last. RFC 0230 fixes the order; this is its
detail.

### Slice 0 — the package and its validator

Create `compiler/specdata` with `Validate() error` and a test that calls it.
No records yet. Files are named by domain from the start
(`components.go`, `targets.go`, `methods.go`, `constructors.go`) even while
empty, so the structure is established before content arrives.

*Verify:* all eight conformance guards in `architecture_policy_test.go` stop
skipping and pass. That is the gate — the architecture is enforced before any
fact moves into it.

### Slice 1 — runtime components

Chosen first because a `ComponentSpec` needs no `Type` values —
`ComponentID`, `DependencyID`, and file names are all primitives — so it
exercises the whole registry without touching Decision 2's cycle.

1. Introduce `ComponentID`. There is **no component identity today**:
   `renderComponentArtifacts` holds a slice of 24 builder functions and keys
   artifacts by strings like `"hexal/runtime.c"`. The ID is new surface.
2. Move each component's **metadata** into a record: owned files, runtime
   dependencies, required C headers.
3. Leave every builder's **demand logic** in Go, unchanged (Decision 6).
4. Move `RuntimeDependency` identities from `compiler/runtime_dependency.go`
   into the registry, keeping the exported Go names as thin aliases until
   slice 3 removes the last consumer.

*Verify:* generated artifacts byte-identical; manifest unmoved; a test asserts
the 24 builders still own their demand; no component's dependency is named in
two places.

### Slice 2 — target facts

Move `compiler/profile.go`'s private `targetProfile` fields — `os`,
`architecture`, `littleEndian`, `pointerWidth`, `sizeWidth`, `windowsTarget`,
`threading`, `tls`, `fibers`, `nativeIO` — into `specdata.TargetFacts`.

`TargetProfileID` stays in `compiler/types` (language-visible identity), and
qualification stays in `internal/driver`. `TestSpecdataDeclaresNoDriverFact`
enforces that boundary mechanically.

Carry forward `profile.go`'s own rule: a new fact enters only when checking or
generation consumes it.

*Verify:* generated C byte-identical for every target; the driver's
qualification records are untouched; no triple or pack path appears in
`specdata`.

### Slice 3 — core-library contracts

Replace `corelib.Modules` — today an exported mutable `map[string]Module` any
importer can rewrite — with immutable registry records and a query.

This is the slice that removes the arc's one genuinely rewritable global.

*Verify:* the exported mutable map is gone; import resolution, signatures, and
generated entry points are unchanged; artifacts byte-identical.

### Slice 4 — types, methods, and layouts

Last, because it is the only slice that touches the cycle and the only one
whose domain is still moving.

1. `TypeConstructorSpec` records per Decision 4 — one per constructor, never
   per specialization.
2. `specdata.TypeID` plus `types.ResolveSpecID`, the adapter that keeps
   primitive identifiers on one side of the boundary and `Type` values on the
   other.
3. `MethodSpec` records with parameters referring to constructor parameters.
4. **The layout experiment**: render one real layout from a record, place it
   beside the template it would replace, and adopt records for layouts only
   where that comparison is favourable. This is the procedure that settles
   Settled questions 4, and it is the last thing in the arc rather than an
   assumption underneath it.

*Verify:* `TestSpecdataImportsNoCompilerPackage` and
`TestSpecdataRecordsNameNoSpecialization` pass with real records present;
generated C byte-identical; manifest unmoved.

### Rollback boundary

Slices 0-3 have byte-identical generated output as their proof, so any one
reverts to a known-good tree. Slice 4 is the first that could legitimately
move an artifact, and only through the layout experiment — which is why that
experiment is scoped to one layout and reviewed before it spreads.

## Implementation readiness

The architecture is ready for staged implementation, but the migration is not
ready to begin as one unbounded rewrite. The first implementation slice should
be registry validation plus one narrow domain—runtime components or built-in
methods—followed by a repository-wide duplicate-owner audit.

Each subsequent domain must preserve the existing behavior and add its own
text, integration, and C-output validation before the next domain moves.

