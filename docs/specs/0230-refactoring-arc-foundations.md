# RFC 0230: Refactoring Arc Foundations

- Kind: Architecture Decision Record (ADR)
- Status: Implementation ready; awaiting acceptance. Eleven decisions, the
  ledger, and the conformance tests that keep them true. This ADR must be
  accepted *before* RFC 0228 or RFC 0229 begins, because both depend on
  decisions neither can make alone
- Created: 2026-09-22
- Scope: fix the ownership map, import graph, generic-specialization model,
  registry validation contract, impact classification, and migration order
  shared by RFC 0221, RFC 0228, and RFC 0229
- Origin: a review of the three specs as one arc found that they claim
  overlapping ownership of the same domains, that RFC 0229's first domain
  would create an import cycle, and that neither downstream spec can settle a
  contract the other also claims
- Depends on: nothing. Every fact below was verified against the tree on
  2026-09-22
- Coordinates with: RFC 0221 (Go library adoption), RFC 0228 (central
  configuration), RFC 0229 (data-driven facts), RFC 0231 (unified C emission,
  independent of the others and able to land in any order), RFC 0227
  (utf8proc), which adds a runtime dependency and components the arc must
  accommodate
- Does not update: `docs/reference.md`, Hexal syntax or semantics, or the
  compiler boundary

## Why this ADR exists

RFC 0228 and RFC 0229 both claim target facts, runtime dependencies,
diagnostics, and the `Error` relationship. RFC 0221 independently refactors
three of the same areas. None of the three can settle a shared contract,
because each would be settling it for the others.

Without these decisions, the arc's likely outcome is not less duplication but
*sanctioned* duplication: the same fact owned by `config`, by `specdata`, and
by the package that kept its copy, with two specifications blessing the split.

This ADR makes eleven decisions. It adds no package and writes no code.

## Decision 1: the ownership map

Verified current owners on the left; target owners on the right.

| Fact | Owner today | Target owner | Why |
|---|---|---|---|
| Tunable limits and defaults (`maxSyntaxDepth`, `inspectionByteLimit`, task stack defaults, page size) | scattered across `parser`, `driver`, `compiler` | **`compiler/config`** | genuinely tunable policy; no semantic content |
| `RuntimeABIVersion` | `compiler/runtimeabi.go` | **`compiler/config`** | a version number, changed by maintainers |
| Type identity, interning, canonical names | `compiler/types` | **`compiler/types`** (unchanged) | identity cannot move; see Decision 2 |
| `Error` capacities | `compiler/types/collections.go` | **`compiler/config`**, read by `types` | already single-owned; moves only so the C spelling can read it too |
| Target identity | `compiler/types/target.go` | **`compiler/types`** (unchanged) | it is a language-visible type |
| Target semantic facts | `compiler/profile.go` (private) | **`compiler/specdata`** | consumed by both checker and generator |
| Target qualification | `internal/driver/profile.go` | **`internal/driver`** (unchanged) | not a language fact; see Decision 3 |
| Runtime dependency identity | `compiler/runtime_dependency.go` | **`compiler/specdata`** | a relationship to components |
| Component metadata | none — 24 builder functions | **`compiler/specdata`** | see Decision 6 |
| Component demand | inside those 24 functions | **`compiler/generator`** (unchanged) | see Decision 6 |
| Core-library contracts | `compiler/corelib` (exported mutable map) | **`compiler/specdata`**, immutable | removes an exported mutable registry |
| Diagnostic text | owning phase | **owning phase** (unchanged) | see Decision 7 |
| Host paths, archives, toolchain | `internal/driver` | **`internal/driver`** (unchanged) | |

Three entries deliberately do not move. `compiler/profile.go` carries a stated
rule — *"a new fact enters here only when checking or generation consumes
it"* — and that discipline is worth preserving wherever the facts live;
Decision 3 keeps it by splitting the file rather than dissolving it.

## Decision 2: the import graph, and the cycle

`compiler/specdata` **imports no compiler package.** It sits below
`compiler/types`, not beside it:

```text
                internal/driver
                      |
        +-------------+-------------+
        |                           |
   compiler/generator        compiler/checker
        |                           |
        +-------------+-------------+
                      |
              compiler/corelib
                      |
               compiler/types
                      |
              compiler/specdata
                      |
               compiler/config
                      |
              Go standard library
```

This is forced, not chosen. `compiler/corelib` imports `compiler/types` and
stores live `Type` values (`corelib.go:9`). If `types` consumed a `specdata`
that needed `Type` to express its facts, the result is:

```text
compiler/types -> compiler/specdata -> compiler/types     cycle
```

**Therefore `specdata` stores primitive identifiers, never `Type` values.**

```go
// in specdata: an identifier, not a type
type TypeID string

// in types: resolution, after interning
func (e *Environment) ResolveSpecID(id specdata.TypeID) (Type, bool)
```

A consumer that needs a `Type` asks `types` to resolve an ID. An ID with no
type is a compiler-development failure, not a user diagnostic.

This has a consequence worth stating plainly: **`specdata` cannot express a
fact whose meaning requires a `Type`.** Facts about *relationships between
named things* are expressible; facts that are really type construction are
not, and stay in `types`.

## Decision 3: target semantics versus qualification

Three different things currently share the word "target". They stay separate.

```text
compiler/types      TargetProfileID          language-visible identity
compiler/specdata   TargetFacts              pointer width, endianness, OS,
                                             threading model, fibers, native IO
internal/driver     qualification            Clang triple, pack availability,
                                             host probe results, release state
```

`Qualified` is **not** a language fact. It depends on the installed backend,
runtime-pack availability, SDK presence, and host state — none of which the
compiler may observe. RFC 0229's `TargetSpec.Qualified` field is withdrawn;
qualification stays in the driver.

The facts in `compiler/profile.go`'s private `targetProfile` — `os`,
`architecture`, `littleEndian`, `pointerWidth`, `sizeWidth`, `windowsTarget`,
`threading`, `tls`, `fibers`, `nativeIO` — move to `specdata.TargetFacts`
unchanged, including the rule that a new fact enters only when checking or
generation consumes it.

## Decision 4: generic specialization

This was RFC 0229's hardest open question and it blocks the type domain.

**Records describe type *constructors*, never specializations.** A
specialization is derived by applying a constructor record to concrete
arguments; there is no record for `List<Int32>` or `String<128>`.

```go
type ParamKind uint8

const (
	ParamType    ParamKind = iota // List<T>, Slice<T>
	ParamInteger                  // String<N>, Array<T, N>
)

type TypeConstructorSpec struct {
	ID         TypeID
	SourceName string          // "List", "String", "Array"
	Params     []ParamKind     // Array is {ParamType, ParamInteger}
	Invariant  ConstructorFacts // facts equal across every specialization
}

type ConstructorFacts struct {
	Representation Representation // handle or inline value
	CopyMode       CopyMode
	FreeMode       FreeMode
	Comparable     ComparisonMode
	Hashable       bool
	Component      ComponentID
}
```

The split that makes this work: **invariant facts live in the record; derived
facts are computed.**

| Fact | Invariant across specializations? |
|---|---|
| Representation, copy mode, free mode | yes — every `List<T>` is a handle |
| Component demand | yes — every `List<T>` needs the list component |
| C name, layout, size, alignment | **no** — derived from arguments |
| Element eligibility | **no** — a predicate over the argument |

`String<N>` is the case that proves the model is needed: N is unbounded, so a
per-specialization record is impossible, while `String`'s invariant facts
(inline value, by-value copy, no free, byte-comparable) are the same at every
capacity.

Methods are declared on the constructor with parameters referring to its own
type parameters, so one record covers every specialization:

```go
MethodSpec{
	Owner:  Constructor("List"),
	Name:   "push",
	Params: []ParameterSpec{{Type: Param(0)}},   // T
}
```

## Decision 5: registry validation

**A `Validate() error` function, called from a test. Not `init()`.**

```go
// compiler/specdata/validate.go
func Validate() error

// compiler/specdata/validate_test.go
func TestRegistryIsValid(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
}
```

Rejected alternatives, and why:

- **`init()` panic** — Go's `init` has no error path, so a malformed registry
  would crash every consumer of the compiler including tooling that never
  touches the bad record. A compiler-development mistake must not become a
  runtime crash in someone else's program.
- **Lazy validation at first query** — makes validity depend on which query
  ran first, so a broken record can hide until an unrelated feature reaches it.
- **A user-visible `Unknown Error`** — blurs a maintainer's mistake into a
  user diagnostic, against the fail-closed contract's intent.

Registry validity is a property of the source tree, checked where source-tree
properties are checked: the test suite. A missing record encountered at
compile time is a separate matter and remains a structured `Unknown Error`.

## Decision 6: component metadata is data; component demand is code

Verified: there is **no component identity today.** `renderComponentArtifacts`
holds a slice of 24 builder functions, each
`func(*programEmission) ([]componentArtifact, error)`, and artifacts are keyed
by strings like `"hexal/runtime.c"`. Demand is the body of each function.

The split:

```go
// data: what a component is
ComponentSpec{
	ID:                  ComponentString,
	Files:               []string{"hexal/string.h", "hexal/string.c"},
	RuntimeDependencies: []DependencyID{DependencyUtf8proc},
	RequiredCHeaders:    []string{"stdint.h", "stddef.h"},
}
```

```go
// code: whether this compilation needs it — unchanged
func stringComponents(merged *programEmission) ([]componentArtifact, error)
```

RFC 0229 asks whether demand rules should be declarative predicates or Go
functions. **They stay Go functions.** The 24 builders are explicit,
readable, and already correct; replacing them with a predicate language means
inventing a DSL to re-express working code, which is the "mini interpreter"
outcome every spec in this arc says it does not want. AGENTS.md's rule that
dispatch stays explicit and fail-closed applies directly.

What data buys here is the part that is currently *implicit*: which files a
component owns, which runtime dependency it pulls, and which C headers it
needs. That is exactly the drift RFC 0227's utf8proc demand would otherwise
add another branch to.

## Decision 7: diagnostics stay in their owning phase

RFC 0228 leaves this open; RFC 0229 proposes `DiagnosticSpec.Template`.
**Neither moves diagnostic text.**

Diagnostic wording stays where the diagnostic is produced, because Hexal's
earliest-diagnostic-ownership rule ties a message to the phase that can prove
it, and a shared template table would let a later phase reach an earlier
phase's wording. A template also cannot express what real diagnostics need —
typed interpolation arguments, source spans, and per-argument formatting —
without becoming a formatting language of its own.

`specdata` may hold diagnostic **identity and stability metadata** (an ID, its
owning stage, whether it is contractual) when a cross-phase consumer needs to
refer to a diagnostic without reproducing its text. It holds no wording.

This also removes the collision with RFC 0221 Track 4, which restructures the
source locations diagnostics carry.

## Decision 8: impact classification

One table, used by all four specs. Any change to a configuration value or a
registry record declares its highest applicable row.

| Impact | Required action |
|---|---|
| Implementation only | ordinary tests |
| Diagnostic wording | update diagnostic tests and the owning spec |
| Source acceptance or rejection | update checker/parser contract **and `docs/reference.md`** |
| Generated C text only | update text assertions; rebuild snippet manifest and review the artifact diff by family |
| C layout, calling convention, ownership, or helper contract | increment `RuntimeABIVersion`; refresh runtime packs; reject stale packs |
| Runtime dependency or link input | update pack manifest, demand tests, and build identity |
| Target qualification | update driver qualification records and native probes |
| Resource default or limit | update validation, diagnostics, and benchmark expectations |

"Increment when required" is not a contract, so: **the owning change declares
its row, and review checks the declaration against the diff.** A change whose
snippet-manifest movement does not match its declared row is wrong about one
of the two.

## Migration order

```text
0221 Tracks 1, 2, 6          mechanical, verified, no shared contracts
        |
0230 (this ADR)              ownership, graph, models settled
        |
0228 config                  tunable scalars only
        |
0229 slice 1: components     metadata only; demand stays code
        |
0229 slice 2: dependencies + targets
        |
0229 slice 3: corelib        removes the exported mutable map
        |
0229 slice 4: types + methods    needs Decision 4's constructor model
        |
0221 Track 3 (graph) and Track 4 (spans)     broadest contracts, last
```

RFC 0231 (unified C emission) sits outside this chain. It touches no contract
the others redesign, and its verification condition is byte-identical
generated C, so it can land at any point. Landing it early removes the
fourteen hard-coded C spellings RFC 0228 cites, narrowing that RFC to the
eleven tunable declarations.

Two placements are deliberate. **0221 Tracks 1/2/6 go first** because they are
verified, mechanical, and touch nothing the arc redesigns. **0221 Track 4
goes last** because source spans restructure every diagnostic location, and
doing that while diagnostics are also being inventoried would make both
changes unreviewable.

RFC 0227 may land at any point. Its component and dependency records are
written as data if 0229 slice 1 is done, and as another builder function if
not; neither blocks the other.

### First slice: runtime components

Components are the right first domain because the record needs **no `Type`
values** — `ComponentID`, `DependencyID`, and file names are all primitives —
so it exercises the whole architecture without touching the cycle in
Decision 2. It also has external validation already: the pack manifest, the
dependency demand tests, and the snippet manifest.

Its completion invariant: every component's files, runtime dependencies, and
required C headers come from one record; the 24 builder functions keep their
demand logic and lose their metadata literals; and no component's dependency
is named in two places.

## The inventory ledger

RFC 0228 requires classifying every package-level `const` and `var`, and RFC
0229 requires one owner per fact. Neither is checkable as prose.

**The inventory is a checked-in file**, `docs/specs/0230-inventory.md`, with
one row per declaration:

```text
| Symbol | File | Classification | New owner | Impact row | Phase |
```

Classifications are exactly: `tunable`, `language fact`, `ABI fact`,
`generated-C fact`, `driver fact`, `algorithm data`, `mutable state`,
`obsolete`. Only `tunable` moves to `config` automatically; everything else
names its owner explicitly.

**The ledger is generated and checked in at
`docs/specs/0230-inventory.md`.** Measured scope on 2026-09-22: **476
package-level `const`/`var` declarations across 64 files** — the earlier
figure of 107 counted declaration *blocks*, not declarations.

Its headline result reframes RFC 0228's scope:

| Classification | Count | Moves? |
|---|---:|---|
| algorithm data (`iota` enums, token kinds, node kinds) | 282 | no |
| owning phase (diagnostic and message strings) | 102 | no |
| language fact | 60 | mostly no |
| driver fact | 12 | no |
| **tunable** | **10** | **yes** |
| mutable state | 6 | resolve individually |
| generated-C fact | 3 | no |
| ABI fact | 1 | yes |

**Eleven declarations move. The other 465 stay.** Configuration is 2.1% of
package-level declarations, not a large migration, and the remaining 97.9% is
internal representation that RFC 0228's own rule forbids turning into public
policy. The classification was still worth doing once, because it is what
proves the 465 stay put.

A migration slice is complete only when every row it claims is struck through.

## Non-goals

- Creating any package. This ADR decides; RFC 0228 and RFC 0229 build.
- Moving diagnostic wording, type identity, or driver host facts.
- A predicate language, template engine, or interpreter for compiler behavior.
- Changing Hexal semantics, generated C, or the compiler boundary.

## Decision 9: package paths

**`compiler/config` and `compiler/specdata`, both at ordinary paths.** Neither
is placed under an `internal/` directory.

`compiler/internal/config` was considered and rejected on a mechanical
ground: Go's internal rule would make it importable only by
`hexal/compiler/...`, which excludes `internal/driver` — and the driver reads
these values. A module-root `hexal/internal/config` would work, but adding a
second `internal/` tree is not wanted.

The usual cost of a public path — exported names becoming compatibility
commitments — does not apply here, for the reason in Decision 11.

## Decision 10: one `specdata` package, files split by domain

```text
compiler/specdata/
    components.go     ComponentSpec records
    targets.go        TargetFacts records
    methods.go        MethodSpec records
    constructors.go   TypeConstructorSpec records
    validate.go       Validate() — sees every domain
```

The registry's whole value is checking references *across* domains: a
component naming an unknown dependency, a method naming an unknown
constructor. Splitting packages would put that check in a coordinator
importing all of them — the same coupling with extra indirection, and one more
place for a reference to hide.

File-per-domain gives the readability that package-splitting was meant to
give. Revisit only if one domain grows validation the others do not share.

## Decision 11: no exported Go name is a compatibility commitment

`go.mod` declares `module hexal` — not a fetchable path, so nothing outside
this repository can import the compiler. Verified: the only importers of
`hexal/compiler` are `internal/driver` and tests inside the module.

Therefore every exported name in the migration scope is **movable**, including
`compiler.RuntimeABIVersion`, `compiler.RuntimeMimalloc`,
`compiler.RuntimeLibuv`, `types.TargetProfileID`,
`types.TargetX86_64LinuxGNU`, and `corelib.Modules`. No slice needs a
name-compatibility audit.

The stable surface is `Compile`'s signature plus `CompilationResult` and
`Project` — which is what the in-memory compiler boundary rule actually
protects. If the module ever takes a fetchable path, freezing names becomes a
decision of its own; until then it is not one.

## Conformance: how each decision is enforced

An ADR that writes no code is only as good as the invariants it can hold. The
repository already treats architectural rules as tests — `comment_policy_test.go`
walks the tree with `go/ast` to enforce a comment policy, and
`expression_dispatch_coverage_test.go` and `error_inventory_test.go` enforce
dispatch and trap coverage the same way. These decisions follow that pattern
wherever they can.

| Decision | Enforced by |
| --- | --- |
| 1. Ownership map | The ledger, plus review. Not mechanically testable: "one owner per fact" is a claim about meaning, not about symbols |
| 2. Import graph / no `Type` in `specdata` | **Test** — parse `specdata`'s imports; assert none is a `hexal/compiler/...` path |
| 3. Target split | **Test** — assert `specdata` declares no toolchain triple, pack path, or qualification field |
| 4. Constructor records | **Test** — assert no record's identity names a concrete specialization |
| 5. `Validate()` not `init()` | **Test** — assert `specdata` declares no `init` function, and that a test calls `Validate()` |
| 6. Demand stays code | **Test** — assert `specdata` declares no predicate or function-typed field |
| 7. Diagnostics stay put | **Test** — assert no `config` or `specdata` string constant is a diagnostic message |
| 8. Impact classification | Review, against the declared impact row and the artifact diff |
| 9. Package paths | **Test** — assert both packages exist at those paths and that `internal/driver` imports them |
| 10. One `specdata` package | **Test** — assert no subdirectory under `compiler/specdata` declares a package |
| 11. No name commitments | Settled by `go.mod`; a test asserting the module path is unfetchable would be noise |

Eight of eleven are testable. The three that are not are decisions about
meaning, and they are reviewed against the ledger rather than asserted.

### The test that matters most

Decision 2 exists because of a real import cycle, so it gets the concrete
shape rather than a description:

```go
// compiler/specdata/architecture_test.go
package specdata_test

// TestSpecdataImportsNoCompilerPackage keeps the registry below
// compiler/types in the import graph. corelib already imports types and
// stores live Type values, so a specdata that imported types would close a
// cycle. Identifiers cross this boundary; types do not.
func TestSpecdataImportsNoCompilerPackage(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if strings.HasPrefix(path, "hexal/compiler/") {
				t.Errorf("%s imports %s; specdata must import no compiler package",
					entry.Name(), path)
			}
		}
	}
}
```

`parser.ParseFile` over a directory read, rather than `parser.ParseDir`, which
Go deprecated in 1.22 — and it matches the walk `comment_policy_test.go`
already uses.

A future change that reaches for a `Type` fails here rather than at a build
that cannot be untangled.

## Validation

This ADR writes no code, so "implemented" means its decisions are recorded,
its evidence is produced, and its downstream specs reflect it. That is true
when:

- every decision is stated with the evidence that decided it, and none is
  left as a preference;
- `docs/specs/0230-inventory.md` exists and classifies **all 476**
  package-level declarations, each with a target owner;
- RFC 0228 and RFC 0229 carry no open question this ADR settles, and their
  text reflects the decisions rather than restating the questions;
- RFC 0221's tracks are sequenced against the arc, with Track 4 last;
- the migration order names one first slice.

All five hold as of 2026-09-22.

### Conformance the downstream slices must satisfy

These are not conditions on this ADR; they are the checks its decisions
impose on RFC 0228 and RFC 0229 as they implement:

- `compiler/config` and `compiler/specdata` exist at those exact paths, with
  no `internal/` segment, and `internal/driver` imports both;
- `compiler/specdata` is one package, files split by domain, and `Validate()`
  resolves references across all of them;
- the eight testable rows in the Conformance table have their tests, and the
  tests pass;
- every generic type is one constructor record; no record names a concrete
  specialization;
- the eleven ledger rows marked `tunable` and `ABI fact` move, and the other
  465 do not;
- the six mutable exported declarations are each resolved, not merely moved;
- each slice declares its impact row, and its snippet-manifest movement
  matches that declaration;
- `go test ./...` and `go vet ./...` pass after every slice, with no slice
  leaving two owners for one fact.

## Implementation readiness

**Ready.** The eleven decisions are made, the evidence for each is in the
tree, the ledger is generated and checked in, and the downstream specs are
updated to consume the decisions rather than re-ask them.

What acceptance unblocks, immediately:

- RFC 0228 — scope is the eleven ledger rows plus the six mutable exports;
- RFC 0229 — slice 1 is runtime components, which needs no `Type` values and
  therefore does not touch the cycle Decision 2 describes.

RFC 0221 Tracks 1, 2, 3 and 6, and RFC 0231, depend on none of this and can
proceed regardless.

### The conformance tests are written

`architecture_policy_test.go` at the repository root holds all eight, in
package `hexal_test`, following the walk `comment_policy_test.go` established.
They were written before the packages they guard, so each enforces from the
moment its package appears:

```text
TestSpecdataImportsNoCompilerPackage      Decision 2
TestSpecdataDeclaresNoDriverFact          Decision 3
TestSpecdataRecordsNameNoSpecialization   Decision 4
TestSpecdataValidatesFromTestNotInit      Decision 5
TestSpecdataDeclaresNoBehaviorField       Decision 6
TestConfigAndSpecdataHoldNoDiagnosticText Decision 7
TestArcPackagesAreAtDecidedPaths          Decision 9
TestSpecdataIsOnePackage                  Decision 10
```

Six skip today with a message naming the invariant they will enforce; two
already run, because a misplaced `config` package or stray diagnostic string
is detectable before either package exists.

Each was verified against a deliberate violation before being accepted — a
`specdata` importing `compiler/types`, declaring a `ToolchainTriple` field, a
`func`-typed field, an `init()`, a `"List<Int32>"` literal, a sentence-shaped
string, a nested subpackage, and a `config` placed under `internal/`. All
eight failed with the offending file and line, then passed once the violation
was removed. A guard that has never failed is not known to work.
