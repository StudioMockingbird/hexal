# RFC 0124: Compiler Property Testing and Fuzzing

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; blocked on RFC 0126
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: two generated-input tiers over the in-memory compiler -- unstructured
  fuzzing of the reject path, and structured generation of valid programs for
  the accept path
- Depends on: RFC 0126 (compiler boundary hardening), the string-in/string-out
  compiler surface in `compiler/compile.go`, and the snippet catalog in
  `workbench/snippets`
- Coordinates with: the deterministic-evaluation-order contract in
  `docs/reference.md`, RFC 0125 (external C23 validation), and
  `docs/status.md` known coverage gaps
- Prior art: the Pixel compiler at `Forge/agents/pixel`, a prior attempt at
  this language, shipped a working version of much of this. Findings adopted
  from it are attributed inline.
- Changes no Hexal grammar, type, function signature, or result contract
- Accepted cost: one new test package, one seed corpus derived from existing
  snippets, and committed crasher inputs as permanent regression seeds

## Summary

`compiler.Compile(sources, entrypoint, project)` is a pure function: no
filesystem, no network, no process-global state, no clock-dependent output. A
fuzzer can therefore assert total properties over arbitrary input rather than
merely hunting for crashes.

Add Go native fuzz targets that assert five invariants the compiler already
claims. Add no external fuzzing engine, no coverage target, no mutation
testing, and no new compiler behavior.

## Motivation

Every existing test states an expected output for a chosen input. That finds
defects in paths someone thought of. The compiler's own contract is stated in
absolute terms -- it fails closed, it never emits output on failure, it is
deterministic -- and absolute claims are exactly what property testing checks.

Three of this project's live defects were found by hand-reading generated C or
by an external reviewer, not by the suite. Two of them (a union name collision
and an order-dependent union spelling) are input-shape defects: a differently
spelled but equivalent program produced different or invalid output. That is a
fuzzable class, and nothing in the suite currently searches it.

## Invariants

These are the oracles. A fuzz failure means one of them is false.

RFC 0126 adds panic containment at the public boundary, converting a panic into
a failed result. That is containment; these invariants are detection, and the
two are complementary. **No invariant here is relaxed because containment
exists.** A recovered panic still fails the target: an Unknown Error produced by
recovery is a compiler defect reported through the ordinary channel, not an
acceptable outcome. Containment bounds the blast radius for embedders; it does
not lower this bar.

### 1. No panic

`Compile` returns a `CompilationResult` for every input. A panic is a compiler
defect. Fatal Go runtime failures such as stack overflow cannot be observed by
a fuzz oracle, so RFC 0126's parser-depth bound must land before **any** of the
four targets is enabled -- `FuzzLex` and `FuzzParse` included. Both reach the
same recursive productions, so neither is exempt because it stops before
checking; a mutated input deep enough to overflow the stack kills the test
process from the lexer or parser exactly as it does from `Compile`.

The compiler's dispatch is fail-closed by design: unreachable metadata reports
an Unknown Error rather than crashing. A panic means one dispatch path missed
that discipline.

### 2. Fail-closed output

`ExitCode != ExitSuccess` implies `Files` is non-nil and empty. `compile.go`
states this; nothing currently tests it over arbitrary input.

The converse also holds: `ExitSuccess` implies `Files` contains `hexal.h` and
at least one `modules/*.c` entry.

### 3. Determinism

Compiling identical input twice produces byte-identical `Files` and an
identical diagnostic sequence.

This is the highest-value invariant. `docs/reference.md` requires that for
identical source and entrypoint, generated file contents are deterministic and
`Files` map iteration order has no meaning; the generator deduplicates
constructed types, tags, and components through maps throughout. A map-order
leak is invisible to a single-run assertion and shows up as a flaky artifact
hash months later.

### 4. No Unknown Error

An `[Unknown Error]` diagnostic reports a violated internal invariant, not a
user mistake. Any input reaching one is a compiler defect, so the fuzzer treats
it as a failure rather than an accepted rejection.

This oracle is what makes fuzzing worthwhile here: the fail-closed design
converts what would be a crash in another compiler into a distinctive,
machine-checkable string.

### 5. Diagnostic well-formedness

Every diagnostic in a failing result is non-empty and carries a source
position within the supplied input. A rejection that cannot say where is a
defect even though it fails closed.

For a source-attributed diagnostic, `Module` names one supplied logical key and
line/column fall within that source. Compilation-level diagnostics that do not
belong to source text, such as a missing entrypoint or invalid `Project`, may
leave `Module` empty but still carry normalized 1-based coordinates.

The public result exposes rendered `Stderr`, not structured diagnostics. The
compile-target oracle therefore parses only the renderer's anchored location
suffix: the final ` at ` followed by either `line:column` or
`module:line:column`. It parses the two final colon-delimited fields as positive
decimal integers; any preceding field is the complete module key and must equal
one supplied source key. RFC 0126's key allowlist excludes `:`, making this
suffix unambiguous. The helper is test-only and round-trips focused diagnostics
whose messages themselves contain ` at `; it does not change the public result
contract.

## Tier 0: corpus-wide guards, no fuzzing required

Two of the five invariants can be asserted over the existing 145-snippet
catalog today, in pure Go, with no fuzzing engine and no generator. They are
the first deliverable because they cost nothing and are currently absent:
Hexal tests invariant 4 per feature but nowhere across the corpus, and its
leak checks are ad hoc per feature rather than a marker list.

- **No Unknown Error for any snippet.** An Unknown Error means the compiler is
  at fault, so it must never fire for a program in the catalog.
- **Dispatch tripwire.** No generated `.c` or `.h` contains any exact byte
  substring in this closed list: `= ;`, `/* Cannot generate`, `List<`,
  `Dict<`, `Fun<`, `.push(`, `.new()`. A lowering path that emits Hexal
  spelling instead of failing closed produces C that cannot compile, and
  nothing detects it today. Changing the list is a deliberate test-policy
  change; implementations do not append ad hoc markers while executing this
  RFC.

### The principle behind every guard here

Pixel's most transferable idea is not any single test but the rule they share:
**a coverage claim is asserted, not conventional.** Its generator checklist, its
fixture-registration gate, and its test-suite registry are three applications of
one principle -- "which tests cover this?" must be a checked fact, because when
it is tribal knowledge it silently goes wrong. Pixel records the concrete
failure: a test needing Clang sat in a group that ran without it.

Hexal needs less machinery than Pixel did for the gate half of that, because
`//go:build c23` is compiler-enforced -- a toolchain test physically cannot run
in the default suite, which is the failure their registry existed to prevent.
What does not follow for free is completeness: that every guard this RFC adds is
actually reached by some runner. Every new guard therefore carries its own
firing test, below, rather than relying on a registry to notice it was never
wired up.

### The funnel contract

A tripwire nobody has ever seen fire is indistinguishable from a broken one.
Each guard is therefore asserted in two halves, and neither half alone is
sufficient:

- **Corpus half.** The guard stays dormant for every snippet.
- **Injection half.** The guard demonstrably fires when fed deliberately
  invalid checked metadata.

This pairing applies to every guard this RFC adds, not only these two.

## Tier 1: arbitrary-input fuzzing

### Targets

One target per stage so a failure attributes itself without bisection.

| Target | Input | Asserts |
|---|---|---|
| `FuzzLex` | one source string | invariants 1 and 5; the token stream terminates and positions are non-decreasing |
| `FuzzParse` | one source string | invariants 1 and 5; parsing terminates with a program or diagnostics |
| `FuzzCompile` | one source string as `app.hex` | all five |
| `FuzzCompileMultiModule` | several sources plus an entrypoint | all five, plus import-graph handling |

`FuzzCompileMultiModule` takes three independent fuzz strings and assigns them
to fixed positional keys: `app.hex`, `a.hex`, and `b.hex`; the entrypoint is
always `app.hex`. Every byte may occur in a Go fuzz string, so no delimiter or
escaping convention can encode this safely. Fixed arguments exercise missing,
duplicate, cyclic, malformed, and successful imports without adding a
serialization format or filesystem behavior.

Both compile targets use `Project{}`. Project-setting fuzzing is a separate
domain and remains a Non-goal.

## Seed corpus

The accepted seed corpus is derived, not duplicated:

- every snippet in `workbench/snippets` supplies its sources through the
  importable `workbench/snippets` package, so adding a snippet adds a seed
  without editing this package; and
- a small committed rejected corpus is added directly with `f.Add`. Existing
  Go test literals are not runtime fixture data and are not scraped from Go
  source files merely to avoid repeating focused fuzz seeds.

Go's native fuzzing runs the seed corpus on an ordinary `go test ./...` run.
That is the point: the corpus becomes a permanent property-regression suite
over every snippet at no additional gate, and extended mutation only happens
when explicitly requested.

## Running

- `go test ./...` runs every seed input against every invariant. It must stay
  fast enough not to change the ordinary gate's character.
- `go test ./compiler/tests/fuzz/ -fuzz=FuzzCompile -fuzztime=...` runs
  extended mutation. It is never part of the default gate.
- A crasher is committed to `testdata/fuzz/<Target>/` as a permanent seed. It
  is never deleted after the fix; the seed is the regression test.

Fuzz targets live in `compiler/tests/fuzz` (`package fuzz`), matching the
existing `compiler/tests/integration` and `compiler/tests/c23validation`
placement, so no compiler package acquires test-only dependencies.

## Expected first findings

Recorded so they are recognized as predicted rather than alarming.

- **Nesting depth.** A recursive-descent parser overflows its stack on deeply
  nested parentheses, blocks, or type constructors. The fix is a depth limit
  producing an ordinary diagnostic, not a larger stack. This is invariant 1 and
  will almost certainly be the first failure.

  This one is already reproduced: 100,000 nested parentheses terminates the
  process with `fatal error: stack overflow`. RFC 0126 owns the depth limit.
  Note that a Go stack overflow is a fatal runtime error rather than a panic,
  so invariant 1 cannot be satisfied by recovering -- the target will die with
  the process, and only the depth limit fixes it.
- **Unknown Error on malformed-but-parseable input.** Checked metadata
  combinations that no hand-written test produces. Each is a real fail-closed
  gap.
- **Determinism under many same-named constructed types.** The exact shape that
  produced the union and ADT name collisions found earlier by review.

## Tier 2: structured generation over valid programs

Tier 1 above guards the reject path. It cannot guard the accept path, because
**byte mutation almost never produces a program that typechecks.** Seeded from
the snippet corpus it produces near-misses that mostly break, so tier 1 will
exercise the lexer, parser, and rejection paths hard and reach the generator
almost never.

The generator is where the defect history lives. Every defect found by review
rather than by the suite -- the union name collision, the ADT collision across
two modules, union order-dependence, the empty member name in Array equality,
`Error.new` recording the wrong module, an accessor-demand miss -- was
downstream of a program that typechecked cleanly.

So tier 2 generates *valid* programs from a structured model and asserts
properties that relate two compilations rather than inspecting one.

**This conclusion has independent support.** The prior Pixel compiler built the
same thing and recorded the same reason: its generator "aims for programs the
compiler ACCEPTS", because "only accepted programs reach code generation, and
code generation is where every invalid-C defect this project has found has
lived." Two projects reaching that from separate defect histories is the
strongest evidence available that the accept path is where the generator budget
belongs.

### Why a hand-written domain is not enough

`compiler/types/union_test.go` already carried the right property --
definition-keying C names are injective over canonical type identity -- with a
hand-listed domain: registry builtins, nine constructed types, and every
binary union of the two, all in one module.

That domain misses three dimensions, each of which carried a shipped defect:
two modules declaring one source name, both member orders, and depth three. It
is a correct property whose domain could not reach the bugs it was written for.

Widening that one test to generate those dimensions is the first deliverable
and is independent of the rest of this RFC.

### Properties

Metamorphic, not single-run. Each compares two artifacts that must agree.

- **C-name injectivity.** Distinct canonical types never share a
  definition-keying C name.
- **Order independence.** A union's canonical identity and generated name do
  not depend on the order its members were written. This is the complement of
  injectivity: one keeps distinct types apart, the other keeps two spellings of
  one type together, and a domain that builds each pair in one direction only
  can satisfy the first while violating the second.
- **Rename invariance.** Renaming a module renames its C symbols consistently
  and changes nothing else structurally.
- **Monomorphization uniqueness.** One generic instantiated through different
  paths yields exactly one specialization.

### Generator and shrinking

The generator produces programs over the constructs where canonical identity
matters: nominal objects, ADTs, unions, generics, collections, and several
modules with deliberately colliding source names. It is not a full-language
generator and does not need to be.

Shrinking is hand-written against that model -- drop a declaration, drop a
union member, shorten a name -- and adds no dependency. A generic shrinking
library is the wrong tool here: shrinking a Hexal AST toward smaller inputs
mostly produces programs that no longer compile, which reports nothing. A
model-aware shrinker shrinks toward *still-valid* programs.

### The generator is a coverage claim, and the claim is asserted

A generator silently narrowing its output is the characteristic failure of this
approach: the suite stays green, the search shrinks, and nothing says so. Three
meta-tests guard the generator itself. They are required, not optional.

- **Construct coverage.** A closed checklist names every construct this
  identity-focused generator claims to emit: nominal objects, ADTs, unions,
  generic declarations and specializations, constructed collections, imports,
  and the declaration/reference forms required to compose them. It does not
  claim every operator, control form, or collection operation. A corpus of
  generated programs must contain every listed construct; anything outside the
  list is outside this generator's coverage claim.

  A marker that cannot fail is worse than no marker. `==` as a substring is
  satisfied by any comparison at all, so a checklist entry for String equality
  written that way keeps passing after the generator stops emitting String
  equality entirely. Entries whose construct is not identifiable by a fixed
  substring use a pattern that pins the operand types instead.

- **Acceptance rate.** A floor on the fraction of generated programs the
  compiler accepts, failing below it and reporting the first rejection. A
  generator whose output is mostly rejected is testing diagnostics, not code
  generation, and code generation is the point. This is the guard that keeps
  tier 2 honest about the reason it exists.

- **Generation determinism.** The same seed produces the same program. Without
  it a failing seed is not a bug report.

### A generated domain must be probed, not trusted

A property test that passes because its domain is degenerate is worse than no
test. Two failure modes are specific to this domain and both were observed
while widening the existing test:

- An incomplete nominal object is not a valid union member, so a domain built
  with `BeginObject` alone contributes inert bases and silently loses the
  cross-module dimension. Objects must be completed.
- A base that no constructor accepts produces a zero Type that the domain skips
  without comment.

Every generated domain therefore reports its own composition -- total types,
counts per depth, and how many names carry a module owner -- and every property
is confirmed to fail against a deliberate mutation of the mechanism it guards
before it is trusted.

## Test policy

- Tier 2 generates candidates from monotonically increasing unsigned seeds
  starting at zero. It retains the shortest candidate prefix whose *accepted
  subset* exercises every construct in its closed checklist. This pins both
  iteration order and the unique shortest prefix without map iteration or a
  separately maintained seed list.
- At least 90 percent of the candidate prefix must compile successfully. Its
  accepted subset is the valid-program corpus used by the properties. A
  lower rate means the generator is spending too much ordinary-suite time on
  invalid programs and fails the test with its first rejection.
- The median duration of five warm `go test ./...` runs after implementation
  may be at most 10 percent above the median of five warm baseline runs on the
  same machine. This is an implementation acceptance measurement, not a
  permanently timing-sensitive test.
- The dispatch-tripwire list above is closed and every entry is an exact byte
  substring.

Extended local fuzz duration is caller-selected through `-fuzztime`; this RFC
does not impose one CI duration because extended mutation is not a default
gate.

## Required sweep

- Do not retain the impossible delimiter-based multi-module encoding.
- Do not add source scraping, `go generate`, or a second copy of the snippet
  catalog to obtain seeds.
- Keep fail-closed artifact assertions at the public `Compile` targets; lexer
  and parser targets assert only contracts observable at those stages.
- Keep the structured generator's checklist equal to its stated domain rather
  than widening it into a full-language coverage claim.

## Implementation plan

### Phase 0: safety and baseline

1. Confirm RFC 0126's recursive-parser bound is implemented and its deep-input
   regression passes before enabling any of the four arbitrary-input targets.
2. Record the green test/vet baseline and measure the current ordinary-suite
   duration.
3. Record the fixed tripwire list, 90 percent acceptance floor, and 10 percent
   ordinary-suite median-duration budget in the test helpers that own them.

### Phase 1: shared oracles and Tier 0

1. Add test-only helpers for fail-closed results, determinism, Unknown Error,
   anchored rendered-diagnostic positions, and generated-source tripwires.
2. Run the Unknown Error and tripwire corpus halves over every snippet.
3. Add focused injected-metadata tests that prove each Tier 0 guard fires.

### Phase 2: arbitrary-input targets

1. Add `compiler/tests/fuzz` and the four independently runnable targets.
2. Load accepted seeds from `workbench/snippets` and add the focused rejected
   seeds directly with `f.Add`.
3. Implement the fixed three-source multi-module target.
4. Commit minimized crashers under the matching Go fuzz corpus directories.

### Phase 3: structured valid-program generation

1. Widen the existing union property domain to cover module ownership, member
   order, and depth three.
2. Add the deterministic identity-focused program model and model-aware
   shrinker.
3. Add construct-coverage, domain-composition, acceptance-rate, and generation-
   determinism guards.
4. Test each property implementation through an explicit test seam containing
   a deliberately broken comparator or naming function; ordinary tests do not
   rewrite production source to perform mutation testing.

### Phase 4: conformance

1. Measure the ordinary-suite cost and keep it within the resolved budget.
   Record the five raw before durations, five raw after durations, and both
   medians in the implementation handoff; timing is evidence for this change,
   not a permanent repository baseline.
2. Implement every Validation item below and no additional behavior.
3. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.

## Non-goals

- Fuzzing generated C, executing it, or differential testing against a C
  toolchain. RFC 0125 owns external validation; a later RFC may connect the
  two. When they are connected, the unit of work is one *distinct generated C
  artifact*, not one fuzz input: thousands of seeds collapse onto the same
  output, so a compiler-invoking tier keys on a hash of the generated C and
  compiles each distinct output once. Without that rule the tier is
  unaffordable, which is how the prior Pixel project made this viable.
- A full-language program generator, or generating programs that exercise
  runtime behavior rather than naming and identity.
- Any third-party property-testing or shrinking library.
- libFuzzer, AFL, or any non-stdlib fuzzing engine.
- Source-coverage percentage targets, mutation scores, or an extended-fuzz CI
  duration. The Tier 2 acceptance-rate floor is a generator-quality guard, not
  a source-coverage target.
- Grammar-aware or type-aware input generation **in tier 1**. Tier 1 is byte
  mutation and stays that way; tier 2 is type-aware by construction, which is
  the whole reason it exists.
- Fuzzing the workbench, the snippet loader, or `Project` configuration.
- Changing any compiler behavior except defects the fuzzer finds.

## Validation

This section is exhaustive. RFC 0124 is complete only when every item passes:

- Each of the four targets exists, is independently runnable, and fails on a
  deliberately injected violation of each invariant it asserts.
- The public compile targets assert failure results. Tier 2 supplies accepted
  programs and asserts successful results; together they exercise both sides
  of invariant 2 and determinism on the accept path.
- The determinism check compares complete `Files` maps and the full diagnostic
  slice, not a summary or a count.
- The Unknown Error oracle fails the target rather than accepting the input.
- Accepted seeds are derived from the snippet catalog, so a new snippet becomes
  a seed with no edit here. Rejected seeds are a small committed `f.Add` corpus;
  no test source is scraped.
- `go test ./...` passes with no external toolchain and without invoking
  extended fuzzing.
- Committed crashers under `testdata/fuzz/` run as ordinary seeds.
- **Tier 2:** the union-naming domain in `compiler/types` is generated and
  covers two modules declaring one source name, both member orders, and depth
  three. Nominal objects in it are completed, so they are valid union members
  rather than inert bases.
- **Tier 2:** each property is observed failing through its explicit test seam
  when given a deliberately broken naming or ordering implementation. Tests do
  not edit or rewrite production source.
- **Tier 2:** every generated domain reports its own composition, so a
  degenerate domain is visible rather than passing silently.
- **Tier 2:** no third-party property-testing or shrinking dependency is added.
- **Tier 0:** the Unknown Error guard and the dispatch tripwire each run over
  the whole snippet catalog, and each is paired with an injection test proving
  it fires. A guard with only a corpus half does not satisfy this.
- **Tier 0:** the tripwire's marker set is a named list, not a per-feature
  check, so a new leak class is added in one place.
- **Generator:** the construct checklist exists, names every construct the
  generator claims to emit, and fails when the generator stops emitting one.
  No checklist entry is a substring that a different construct also satisfies.
- **Generator:** monotonically increasing seeds starting at zero produce one
  deterministic candidate sequence; its shortest prefix has an accepted
  subset covering the closed construct checklist, at least 90 percent of the
  prefix compiles, and the guard reports the first rejection on failure.
- **Generator:** the same seed produces the same program.
- **Diagnostics:** the test-only anchored-suffix parser accepts both rendered
  location forms, rejects zero/malformed coordinates and unknown modules, and
  still parses a diagnostic whose message contains ` at `.
- Five warm before/after measurements on the same machine show that the median
  duration of `go test ./...` increases by no more than 10 percent.
- Ordinary tests remain pure Go.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

None. This RFC adds no language rule and changes no generated output.
`docs/reference.md` is untouched. Defects the fuzzer finds are fixed under
their own owning specs, and any that cannot be fixed immediately are recorded
in `docs/status.md` open bugs like any other finding.
