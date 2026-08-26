# RFC 0124: Compiler Property Testing and Fuzzing

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: two generated-input tiers over the in-memory compiler -- unstructured
  fuzzing of the reject path, and structured generation of valid programs for
  the accept path
- Depends on: the string-in/string-out compiler surface in `compiler/compile.go`
  and the snippet catalog in `workbench/snippets`
- Coordinates with: RFC 0111 (deterministic evaluation order), RFC 0125
  (external C23 validation), and `docs/status.md` known coverage gaps
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

### 1. No panic

`Compile` returns a `CompilationResult` for every input. A panic, including a
stack overflow from unbounded recursion, is a compiler defect.

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

This is the highest-value invariant. RFC 0111 requires that "the checker and
generator must not rely on map iteration order for source evaluation", and the
generator deduplicates constructed types, tags, and components through maps
throughout. A map-order leak is invisible to a single-run assertion and shows
up as a flaky artifact hash months later.

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

## Tier 0: corpus-wide guards, no fuzzing required

Two of the five invariants can be asserted over the existing 145-snippet
catalog today, in pure Go, with no fuzzing engine and no generator. They are
the first deliverable because they cost nothing and are currently absent:
Hexal tests invariant 4 per feature but nowhere across the corpus, and its
leak checks are ad hoc per feature rather than a marker list.

- **No Unknown Error for any snippet.** An Unknown Error means the compiler is
  at fault, so it must never fire for a program in the catalog.
- **Dispatch tripwire.** No generated `.c` or `.h` contains source-language
  text. A lowering path that emits Hexal spelling instead of failing closed
  produces C that cannot compile, and nothing detects it today. The marker set
  is at minimum `= ;`, `/* Cannot generate`, `List<`, `Dict<`, `Fun<`,
  `.push(`, `.new()`.

### The funnel contract

A tripwire nobody has ever seen fire is indistinguishable from a broken one.
Each guard is therefore asserted in two halves, and neither half alone is
sufficient:

- **Corpus half.** The guard stays dormant for every snippet.
- **Injection half.** The guard demonstrably fires when fed deliberately
  invalid checked metadata.

This pairing applies to every guard this RFC adds, not only these two.

## Targets

One target per stage so a failure attributes itself without bisection.

| Target | Input | Asserts |
|---|---|---|
| `FuzzLex` | one source string | invariants 1 and 5; the token stream terminates and positions are non-decreasing |
| `FuzzParse` | one source string | invariants 1, 2, and 5 |
| `FuzzCompile` | one source string as `app.hex` | all five |
| `FuzzCompileMultiModule` | several sources plus an entrypoint | all five, plus import-graph handling |

`FuzzCompileMultiModule` needs structured input. Encode the module set as one
fuzzed string split on a delimiter that cannot occur in Hexal source, and
derive module names positionally. Do not add a serialization format, a
generator library, or a grammar-aware input model.

## Seed corpus

The seed corpus is derived, not written:

- every snippet in `workbench/snippets` supplies its sources; and
- the rejected-source strings already embedded in checker and integration
  diagnostic tests supply the near-miss inputs that reach the most interesting
  rejection paths.

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
`Error.new` recording the wrong module, the RFC 0088 accessor-demand miss --
was downstream of a program that typechecked cleanly.

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

- **Construct coverage.** A checklist names every construct the generator must
  emit -- each declaration form, each operator, each control form, each
  collection operation, each identity-bearing declaration -- and a corpus of
  several hundred generated programs must contain all of them. A construct the
  generator cannot emit is a construct this fuzzing never tests, so the claim
  is asserted rather than assumed.

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
- Coverage percentage targets, mutation scores, or a CI time budget.
- Grammar-aware or type-aware input generation **in tier 1**. Tier 1 is byte
  mutation and stays that way; tier 2 is type-aware by construction, which is
  the whole reason it exists.
- Fuzzing the workbench, the snippet loader, or `Project` configuration.
- Changing any compiler behavior except defects the fuzzer finds.

## Validation

This section is exhaustive. RFC 0124 is complete only when every item passes:

- Each of the four targets exists, is independently runnable, and fails on a
  deliberately injected violation of each invariant it asserts.
- `ExitSuccess` and failure results are both asserted; a target that only ever
  observes rejection is not exercising invariants 2 and 3.
- The determinism check compares complete `Files` maps and the full diagnostic
  slice, not a summary or a count.
- The Unknown Error oracle fails the target rather than accepting the input.
- The seed corpus is derived from the snippet catalog rather than duplicated,
  so a new snippet becomes a new seed with no edit here.
- `go test ./...` passes with no external toolchain and without invoking
  extended fuzzing.
- Committed crashers under `testdata/fuzz/` run as ordinary seeds.
- **Tier 2:** the union-naming domain in `compiler/types` is generated and
  covers two modules declaring one source name, both member orders, and depth
  three. Nominal objects in it are completed, so they are valid union members
  rather than inert bases.
- **Tier 2:** each property fails against a deliberate mutation of the
  mechanism it guards -- removing the arena's union-name disambiguation must
  fail injectivity, and removing canonical member ordering must fail order
  independence.
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
- **Generator:** the acceptance-rate floor exists, is asserted against a corpus
  of several hundred programs, and reports the first rejection on failure.
- **Generator:** the same seed produces the same program.
- Ordinary tests remain pure Go.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

None. This RFC adds no language rule and changes no generated output.
`docs/reference.md` is untouched. Defects the fuzzer finds are fixed under
their own owning specs, and any that cannot be fixed immediately are recorded
in `docs/status.md` open bugs like any other finding.
