# Spec 0013: Test Suite Organization

- Kind: project convention
- Status: implemented
- Scope: test file layout only. No assertion logic changes, no compiler changes.

## Problem

Integration tests are named after the document that prompted them, not the
behavior they cover:

```
compiler/rfc0003_test.go   compiler/rfc0006_test.go
compiler/rfc0004_test.go   compiler/rfc0007_test.go
compiler/rfc0005_test.go   compiler/c23_test.go
compiler/compile_test.go
```

RFCs are design history. They close and become immutable; the behavior they
describe keeps changing. A file named for one is unfindable ("where do I add a
pointer test?") and misleading once the next RFC amends it — `rfc0007_test.go`
already holds the current pointer rules, and `rfc0003_test.go` already holds an
RFC 0009 constant-folding test.

Root cause is `AGENTS.md`:

> Grow `compiler/compile_test.go` with every language facet.

One file for every facet is unworkable, so each RFC spawned a sibling instead.
`compile_test.go` still grew to 606 lines mixing literals, pointers, operators,
and syntax. The convention must change or this recurs on RFC 0008.

## Rules

1. Test files are named for the language facet they cover. Never for an RFC, a
   plan, or an ADR.
2. RFC provenance goes in a header comment, where it stays useful:
   `// Pointer mutability: ref typing, weakening, recursive members. RFC 0007.`
3. Test function names carry no RFC number and no `Compile` prefix — every test
   in the package calls `Compile`.
4. A test belongs to the facet it exercises, not the document that introduced
   it.
5. No external toolchain in `go test`. See below.

## Target layout

`compiler` package — integration, currently 1558 lines across 7 files:

| File | Sources |
|---|---|
| `literals_test.go` | `rfc0003` minus the fold test; `compile_test` Int32/Bool/numeric/hex/range cases |
| `naming_test.go` | `rfc0004` |
| `aliases_test.go` | `rfc0005` |
| `objects_test.go` | `rfc0006` |
| `pointers_test.go` | `rfc0007`; `compile_test` pointer/aliasing/nesting/diagnostics cases |
| `operators_test.go` | `compile_test` RFC0009 cases; `rfc0003`'s `NegativeZeroUnaryFolding`; surviving fragment assertions from `c23_test.go` |
| `syntax_test.go` | `compile_test` comments, whitespace, semicolon, statement sequencing, parse diagnostics |
| `helpers_test.go` | `withoutLineDirectives` and any other shared helper |

Stage packages: `checker/rfc0005_test.go` → `checker/aliases_test.go`. Every
other stage file is already facet-named.

`compile_test.go` and `c23_test.go` cease to exist.

## Dropping the GCC dependency

`c23_test.go` shells out to `gcc -std=c23 -Wall -Werror` and, in two cases, runs
the resulting binary. That leaves the suite unrunnable without a C toolchain on
PATH, for a payoff we do not need yet.

Most of those tests do not depend on GCC for their assertion. They already check
generated-C fragments as strings and then call `compileGeneratedC23` as an extra
step. Those string assertions move to `operators_test.go` and `pointers_test.go`
intact; only the GCC call is deleted.

**What is actually lost.** Two tests verify runtime behavior that no string
assertion can reach:

- `TestCompileRFC0007SignedWrappingBoundariesAsC23` — patches the generated
  `main` to return `EXIT_FAILURE` unless `Int8` and `Int64` overflow wrap as
  specified, then runs it. This is the only empirical proof that the wrapping
  lowering is correct rather than merely as-designed.
- `TestCompileRFC0009ShortCircuitRuntimeAsC23` — runs a program whose
  right-hand operand divides by zero, proving `and`/`or` genuinely short-circuit.

Also lost: the guarantee that generated C compiles at all. A generator change
that emits invalid C will now pass `go test`.

**Accepted.** Both risks are cheap to catch by hand and expensive to carry in
CI. Recorded here so restoring them later is a lookup, not a rediscovery.

**Restore as** `compiler/c23_toolchain_test.go` behind `//go:build c23`, run
explicitly with `go test -tags c23 ./compiler`, once the generator is stable
enough that "does this compile" is a real question. Not before.

## Sequence

Each step ends with `go test ./...` green. Steps 1–3 are pure moves, so an
unchanged pass/fail set is the verification.

1. `git mv` the five RFC-named files to facet names. Nothing else.
2. Delete the GCC calls: remove `compileGeneratedC23`, its `os`/`exec`/
   `filepath` imports, and the two runtime tests. Move surviving fragment
   assertions into their facet files. Delete `c23_test.go`.
3. Split `compile_test.go` into `pointers_test.go`, `operators_test.go`,
   `literals_test.go`, and `syntax_test.go`. Shared helpers to
   `helpers_test.go`.
4. Rename test functions per rules 2–3. The compiler catches collisions.
5. Replace the `AGENTS.md` Testing bullet:

   > Grow `compiler/compile_test.go` with every language facet.

   with:

   > Integration tests live in `compiler/`, one file per language facet, named
   > for the facet. Never name a test file after an RFC, plan, or ADR — cite the
   > document in a header comment instead. `go test ./...` must pass with no
   > external toolchain installed.

Separate commits per step, so review can confirm each move was behavior-neutral.

## As executed

Two things this spec did not anticipate:

**A second GCC dependency lived in `generator_test.go`**, not just `c23_test.go`.
Three tests there shelled out to gcc. `TestGenerateUInt16MultiplicationCompilesAsC23`
was deleted outright — its string assertion was a strict subset of
`TestRenderUnsignedNarrowMultiplicationUsesUInt32Intermediate` directly above it,
so it existed only for the gcc call. The other two kept their assertions.

`TestFloatTargetAssertionsFailClosed` lost the most: it undefined
`FLT_IS_IEC_60559` in a synthetic header and required gcc to reject it, which is
the only way to prove the `#error` guards actually fire. It now asserts that a
header emits guards only for the float kinds the program uses — weaker, but not
nothing. Add to the restored `c23` suite.

**Six tests were duplicates**, all in `c23_test.go`, each asserting fragments
already covered verbatim in `pointers_test.go`. Deleted rather than moved:
recursive-object lowering aside, they were `FixedMutPtrPointeeWrite`,
`FixedMutPtrObjectMemberWrite`, `FixedObjectConstReference`, `WeakeningHasNoCast`,
`CompleteProgram`, and `MutableObjectReplacement`.

Result: 332 → 326 test cases, all passing; `compiler` package runtime 11.9s →
0.6s.

## Out of scope

Merging `checker/operators_test.go` into `compiler/operators_test.go`. Different
packages, different jobs — `AGENTS.md` calls that unit/integration overlap
intentional.
