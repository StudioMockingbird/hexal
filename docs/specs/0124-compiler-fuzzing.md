# RFC 0124: Compiler Fuzzing

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: property-based fuzzing of the in-memory compiler against its
  fail-closed, deterministic, panic-free contract
- Depends on: the string-in/string-out compiler surface in `compiler/compile.go`
  and the snippet catalog in `workbench/snippets`
- Coordinates with: RFC 0111 (deterministic evaluation order), RFC 0125
  (external C23 validation), and `docs/status.md` known coverage gaps
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

## Non-goals

- Fuzzing generated C, executing it, or differential testing against a C
  toolchain. RFC 0125 owns external validation; a later RFC may connect the two.
- libFuzzer, AFL, or any non-stdlib fuzzing engine.
- Coverage percentage targets, mutation scores, or a CI time budget.
- Grammar-aware or type-aware input generation.
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
- Ordinary tests remain pure Go.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

None. This RFC adds no language rule and changes no generated output.
`docs/reference.md` is untouched. Defects the fuzzer finds are fixed under
their own owning specs, and any that cannot be fixed immediately are recorded
in `docs/status.md` open bugs like any other finding.
