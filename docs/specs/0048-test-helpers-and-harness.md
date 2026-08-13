# Spec 0048: Test Helpers, Subtests, and Pure-Go Testing

- Kind: project convention
- Status: Draft; pending approval
- Scope: test-side only. No compiler, language, or generated-C changes.
- Supersedes: spec 0013's rule that toolchain-dependent tests sit behind the
  `c23` build tag, and the matching clause in AGENTS.md's Testing section. Both
  are removed rather than replaced: no test may require a C toolchain.
- Related: spec 0013 (test file layout), `docs/status.md` (conformance gaps)

## Problem

The suite is large and well organised — 69 files, 841 test functions, ~13,500
lines, one integration file per language facet — but three mechanical problems
have accumulated.

### 1. Assertion boilerplate is copied, not shared

Nearly every integration test asserts one of two things: this source compiles,
or it fails with a particular diagnostic. Both are written out longhand at
every call site.

| Pattern | Occurrences |
|---|---|
| `t.Fatalf` | 1374 |
| `ExitSuccess` check | 404 |
| `strings.Contains` on stderr | 395 |
| `ExitFailure` check | 316 |

The rejection shape repeats verbatim:

```go
result := Compile(testCase.source)
if result.ExitCode != ExitFailure || len(result.Stderr) == 0 ||
    !strings.Contains(result.Stderr[0], testCase.want) {
    t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
}
```

`helpers_test.go` already exists but holds one function, so the convention is
established and unused.

### 2. Most table-driven tests do not name their cases

28 files build `[]struct{...}` tables; only 10 wrap the body in `t.Run`. **30
tables run without subtests**, so a failure reports the parent test name and the
reader must match the printed source back to a row by eye.

### 3. Toolchain-dependent tests dominate the run

Nine `c23_*_test.go` files invoke `gcc` to compile, and in four cases link and
execute, the generated C. They are 16 of roughly 401 tests in the package but
account for most of its wall time:

| Test group | Count | Time |
|---|---|---|
| toolchain-dependent | 16 | 3.3s |
| pure Go | ~385 | 0.6s |

They also carry maintenance weight the project is not ready to pay: two
different gating mechanisms (`//go:build c23` on three files, `LookPath` +
`t.Skip` on six), four near-identical copies of the compile-and-run sequence,
and flag drift — one probe already compiles without `-Wall -Wextra` and so
cannot fail on a warning the other twelve would catch.

The project is too early to carry a toolchain dependency in its test loop. It is
removed rather than centralized.

## Decision 1: domain assertion helpers

Add to `compiler/helpers_test.go`:

```go
func assertCompiles(t *testing.T, source string) CompileResult
func assertRejects(t *testing.T, source, want string)
func assertEmits(t *testing.T, source string, wants ...string)
```

- Each calls `t.Helper()` so failures report the caller's line.
- `assertCompiles` fails when `ExitCode != ExitSuccess` and returns the result
  for further inspection.
- `assertRejects` fails unless the compile fails **and** the first diagnostic
  contains `want`. It keeps the existing message shape, which prints the source
  and the full `Stderr` slice.
- `assertEmits` compiles, then requires every `wants` substring in `MainC`.

Helpers encode the domain, which is why they beat a general assertion library:
the call site drops from four lines to one and still prints the offending
source.

Stage-level packages (`checker`, `parser`, `lexer`, `types`, `generator`) get
their own helpers only where the same duplication is demonstrable. Do not add a
helper with one caller.

## Decision 2: subtests for every table

Every `for ... range []struct{...}` table wraps its body in `t.Run(name, ...)`,
and every table row carries a distinct `name` field. A failing row then reports
as `TestX/case_name`, and `go test -run 'TestX/case_name'` selects it.

This is stdlib behavior and needs no dependency.

## Decision 3: no test may require a C toolchain

Delete every toolchain-dependent test. All nine files go:

```text
compiler/c23_bitwise_smoke_test.go        compiler/c23_print_smoke_test.go
compiler/c23_concurrency_smoke_test.go    compiler/c23_smoke_test.go
compiler/c23_error_smoke_test.go          compiler/c23_stream_smoke_test.go
compiler/c23_io_smoke_test.go             compiler/c23_text_smoke_test.go
compiler/c23_numeric_smoke_test.go
```

After deletion:

- No test file imports `os/exec`, and no test invokes `gcc`, `clang`, or `cc`.
- No `//go:build c23` tag remains anywhere.
- No test writes to a temp dir to hand files to another process.
- The whole suite is pure Go and runs in roughly 0.6s.

Generated C is still tested, at the string level only, through `assertEmits`
from Decision 1. That verifies the generator emits the intended constructs. It
does not verify the result is valid C.

### What this gives up, and when to revisit

Recorded so the decision stays traceable rather than looking like neglect. At
deletion time these 16 tests passed against gcc 15 (mingw64) under
`-std=c23 -Wall -Wextra` with no warnings, and four of them linked and ran the
produced binary, asserting on its stdout — covering `print`, file I/O, text, and
the M:N scheduler.

Deleting them removes the only check that generated C compiles at all. String
assertions cannot catch a missing declaration, a malformed initializer, a
type error in a generated helper, or a warning-level defect. That class of bug
becomes invisible until someone compiles by hand.

This is an accepted, temporary trade for a fast pure-Go loop. Add a
`docs/status.md` follow-up recording that generated C is no longer verified to
compile. Reintroduce toolchain coverage when the C output stabilizes and a CI
job can carry the cost outside the developer loop — as a separate job, never as
part of the default `go test ./...`.

## Decision 4: conformance-gap tripwires

`docs/status.md` records four gaps where the compiler does not match
`reference.md`. They are documented but unguarded, so closing one produces no
signal and the doc silently rots.

Add `compiler/conformance_gaps_test.go` asserting the **current, incorrect**
behavior of each gap, with a header comment pointing at the `status.md` entry
and a per-case comment naming the rule in `reference.md` that is not yet met.

- match scrutinee and arm results reject unparenthesized `and`/`or`
- `ref` rejects `ref rows[0].field` while accepting `ref pair.values[0]`
- the lexer accepts a raw newline inside a String literal

The 16/32-bit `Size` profile gap has no source-level probe and stays
documentation-only.

When a gap is fixed the tripwire fails, which is the intended signal: the fixer
deletes the case and strikes the `status.md` entry in the same change. This file
is the one deliberate exception to testing intended behavior.

Spec 0049 fixes several of these gaps, so the file may be short-lived. That is
the mechanism working, not wasted effort: each tripwire converts into an
ordinary positive test as its gap closes.

## Decision 5: coverage derived from `reference.md`

`reference.md` is the normative contract, so test coverage should be auditable
against it. Its 181 normative bullets divide into four classes:

| Class | Approx. count | Test form |
|---|---|---|
| Rejection rules | 56 | compile, assert failure and message |
| Acceptance and emission rules | 90 | compile, assert success or `MainC` content |
| Runtime traps | 13 | requires executing the binary |
| Deliberately unspecified | 32 | none, by design |

Two consequences follow.

**Not every statement can become a test, and some must not.** The fourth class —
"operand order is C23-unspecified", "iteration order is unspecified", "runtime
metadata may catch double-free", "resize invalidation is not tracked" — states
the *absence* of a guarantee. A test pinning any of them down would freeze an
implementation detail the language explicitly refuses to promise. These stay
untested deliberately.

**The trap class is unverifiable after Decision 3.** Thirteen rules — empty
`pop`, missing Dict key, out-of-bounds index, zero divisor, shift count, float
overflow, allocation failure, malformed UTF-8, close failure, Mutex misuse, and
`print`'s exact output forms — can only be observed by running the generated
program. String assertions can confirm a trap call is emitted, not that it
fires. Record this in the `docs/status.md` follow-up from Decision 3.

Add three things:

1. **A diagnostic-class sweep.** One test compiles a corpus of known-invalid
   sources and fails if any produces `Unknown Error`. AGENTS.md reserves that
   class for compiler defects, never user errors, and nothing currently enforces
   it. This directly guards the fail-closed architecture rule.
2. **A storability matrix test.** `reference.md` defines 15 positions and three
   exception types (`Atomic`, `Fun`, `Unknown`). Generate the combinations from
   a table rather than hand-writing samples, so a position added later cannot
   quietly go unchecked.
3. **Reference-anchored citations.** Facet files currently cite RFC numbers in
   their header comments. RFCs are archived history; `reference.md` is the
   contract. Cite the reference section instead, so coverage can be audited by
   grepping which sections no test names.

## Expected scale

Measured before implementation so the outcome can be checked against it. The
baseline is 841 top-level test functions and 1,168 reported units, where a
reported unit is a test function or a named subtest.

| Change | Functions | Reported units |
|---|---|---|
| Delete nine `c23_*` files (Decision 3) | −15 | −15 |
| Bug regressions (spec 0049) | +5 | +22 |
| Conformance tripwires (Decision 4) | +3 | +3 |
| Diagnostic-class sweep (Decision 5) | +1 | +1 |
| Storability matrix (Decision 5) | +2 | ~+40 |
| Subtests for existing tables (Decision 2) | 0 | ~+131 |
| **Net** | **≈ −4** | **≈ +182** |

Landing near 837 functions and ~1,350 reported units.

Three cautions on reading those numbers:

- **Only ~65 assertions are genuinely new**: ~22 bug regressions, ~40 matrix
  combinations, 3 tripwires. The ~131 from Decision 2 are existing table rows
  gaining names, which improves failure reporting and adds no coverage. Do not
  count it as progress.
- **The 3 tripwires are deliberately temporary.** Each dies when spec 0049 fixes
  its gap and converts into an ordinary positive test. Their disappearance is
  not lost coverage.
- **The 13 runtime trap rules stay unverifiable** under every line above. No
  item here addresses them; Decision 3 explains why and `docs/status.md` records
  it.

The storability matrix is the largest single addition and the most mechanical:
15 positions x 3 exception types, generated from a table so that adding a
sixteenth position without extending the table becomes a visible omission
rather than a silent gap.

## Non-goals

- **A test framework, including `testify/suite`.** `suite` exists for lifecycle
  and shared fixtures. `Compile` is a pure function of a source string, and the
  only setup in the entire suite is the temp dir in the C23 tests, which
  Decision 3 already centralizes. Adopting it would add the module's first
  dependency, break `go test -run TestName` selection by hiding tests behind a
  single entry point, and contradict AGENTS.md's preference for plain loops and
  direct data over frameworks.
- **A general assertion library.** It cannot express `Compile` + `ExitFailure` +
  first-diagnostic-contains in fewer lines than Decision 1, and it drops the
  source from the failure message.
- **A replacement for the deleted toolchain coverage.** No embedded C parser, no
  vendored compiler, no CI job in this spec. Decision 3 accepts the gap.
- Golden-file or snapshot testing of generated C.
- Benchmarks, fuzzing, and coverage thresholds.
- Renaming or resplitting test files; spec 0013 owns layout and is unchanged
  apart from the build-tag clause.
- Any change to compiler behavior. A test that fails during migration indicates
  a migration error, not a compiler bug.

## Migration

Mechanical and incremental. Each step keeps the suite green.

1. Delete the nine `c23_*_test.go` files. Confirm no test file still imports
   `os/exec` and that `//go:build c23` appears nowhere.
2. Add the `docs/status.md` follow-up recording the lost coverage.
3. Add the helpers with no call-site changes.
4. Convert integration files to the helpers, one facet file per change.
5. Add `t.Run` to the 30 tables that lack it.
6. Add the tripwire file.
7. Update AGENTS.md's Testing section: drop the `c23` build-tag rule, state that
   no test may require a C toolchain, and name the helpers as the expected
   assertion style.

Step 1 stands alone and can land immediately. Step 4 is the bulk and may lag the
others without blocking them.

Before deleting, check whether any assertion in the nine files covers a
behavior no pure-Go test covers. Where one does, add the equivalent string-level
`assertEmits` case in the matching facet file rather than losing the case
outright.

## Acceptance criteria

1. `assertCompiles`, `assertRejects`, and `assertEmits` exist, call
   `t.Helper()`, and print the source on failure.
2. No integration test writes the `ExitFailure` + `len(Stderr)` +
   `strings.Contains` sequence inline.
3. Every table-driven test uses `t.Run` with a distinct case name.
4. No test file invokes `gcc`, `clang`, or `cc`, imports `os/exec`, or carries a
   `//go:build c23` tag.
5. `go test ./...` behaves identically with and without a C toolchain installed,
   and the `compiler` package runs in under one second.
6. `docs/status.md` records that generated C is no longer verified to compile.
7. `conformance_gaps_test.go` covers the three probeable gaps and cites its
   `status.md` entries.
8. A diagnostic-class sweep exists and no invalid source in it yields
   `Unknown Error`.
9. The storability matrix is generated from a table covering every position in
   `reference.md`, not hand-written samples.
10. Facet files cite `reference.md` sections rather than RFC numbers.
11. `go.mod` still declares no dependencies.
12. Apart from the deliberate removal in Decision 3, coverage does not fall: the
    migration changes how assertions are written, never which behaviors are
    asserted.
13. Final counts land within a reasonable margin of "Expected scale". A
    materially lower reported-unit count means tables were converted to helpers
    without subtests; a materially lower function count means cases were dropped
    rather than rewritten.

## Open questions

None. Every decision is test-side and reversible.
