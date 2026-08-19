# Compiler complexity baseline

Two metrics over the compiler's own Go source: **cyclomatic** (McCabe) is the
actual branching a test would have to cover, **cognitive** (Sonar) is the
perceived difficulty of reading it. Neither substitutes for the other.

```bash
go test -run TestComplexityReport -v ./compiler/tests/benchmarks
```

Nothing here is a threshold and nothing gates a build, matching RFC 0075's
policy: a measurement authorizes a change, and "nothing measurable is wrong" is a
valid result.

**Append, never overwrite**, the same discipline as `docs/benchmarks.md`: each
re-measurement gets a dated entry under Measurement history with the Go version
that produced it and one line on what moved, so a trend is readable without
digging through git.

## Current baseline

**2026-08-18 22:39 IST** — Go 1.26.4 windows/amd64, 989 functions with a body
under `compiler/`.

## Distribution

| Cyclomatic band | Functions |
|---|---|
| > 50 | 15 |
| 21–50 | 49 |
| 11–20 | 119 |
| ≤ 10 | 806 |

82% of the compiler sits at cyclomatic 10 or below. The tail is the story.

## Worst by cyclomatic complexity

| Function | Cyclomatic | Cognitive | Lines | File |
|---|---|---|---|---|
| `validateExpressionNode` | 267 | 288 | 389 | `generator/validation.go` |
| `validateConcurrencyExpression` | 118 | 146 | 185 | `generator/concurrency.go` |
| `Lex` | 107 | 179 | 401 | `lexer/lexer.go` |
| `renderExpressionUncheckedWithState` | 107 | 113 | 280 | `generator/render.go` |
| `validateCollectionExpression` | 106 | 135 | 155 | `generator/arrays.go` |
| `checkBinaryExpression` | 98 | 90 | 199 | `checker/operator_checking.go` |
| `validateTextExpression` | 82 | 115 | 124 | `generator/strings.go` |
| `validateStatements` | 74 | 179 | 200 | `generator/validation.go` |
| `checkMethodCall` | 72 | 71 | 224 | `checker/methods.go` |
| `String` | 71 | **1** | 146 | `lexer/lexer.go` |

## Worst by cognitive complexity

| Function | Cognitive | Cyclomatic | Lines | File |
|---|---|---|---|---|
| `validateExpressionNode` | 288 | 267 | 389 | `generator/validation.go` |
| `Lex` | 179 | 107 | 401 | `lexer/lexer.go` |
| `validateStatements` | 179 | 74 | 200 | `generator/validation.go` |
| `validateConcurrencyExpression` | 146 | 118 | 185 | `generator/concurrency.go` |
| `validateCollectionExpression` | 135 | 106 | 155 | `generator/arrays.go` |
| `renderCollectionExpression` | 134 | 62 | 187 | `generator/arrays.go` |
| `checkMatchExpression` | 131 | 62 | 188 | `checker/adt.go` |
| `writeStatementsAt` | 130 | 58 | 207 | `generator/render.go` |
| `walkProgram` | 119 | 62 | 218 | `generator/walk.go` |
| `resolveTypeUse` | 83 | 42 | 147 | `checker/type_resolution.go` |

## Worst function per package

| Package | Cyclomatic | Cognitive | Function |
|---|---|---|---|
| `generator` | 267 | 288 | `validateExpressionNode` |
| `lexer` | 107 | 179 | `Lex` |
| `checker` | 98 | 90 | `checkBinaryExpression` |
| `parser` | 35 | 30 | `statement` |
| `types` | 25 | 54 | `containsTypeParameter` |
| `compiler` | 18 | 24 | `resolveImportPath` |

## Why two metrics and not one

The two disagree by design, and the disagreements are the point:

- `String` (`lexer`) is **cyclomatic 71, cognitive 1** — a flat switch returning
  a string per case. Genuinely trivial to read; cognitive complexity says so.
  Reporting only cyclomatic would send someone to refactor `String()` methods.
- `validateStatements` is **cyclomatic 74, cognitive 179** — deep nesting, which
  cyclomatic understates by more than half.
- `renderCollectionExpression`, `checkMatchExpression`, and `writeStatementsAt`
  appear in the cognitive top ten and not the cyclomatic one, all for the same
  reason.

Across all 989 functions the two correlate at r = 0.931 (r² = 0.87). The
remaining 13% is what neither metric can report alone.

Halstead measures and the Maintainability Index were prototyped over the same
functions and dropped: Halstead volume correlated with line count at r = 0.957
(r² = 0.92), and MI is a formula over volume, cyclomatic, and lines, so it added
nothing beyond inputs already reported here. See RFC 0080, "Why only two".

## Measurement history

Newest first. The headline numbers only — the full tables live under Current
baseline for the newest entry, and in git for the rest.

| When | Functions | Worst cyclomatic | Worst cognitive | > 50 cyc | What changed |
|---|---|---|---|---|---|
| 2026-08-18 22:39 | 989 | 267 `validateExpressionNode` | 288 `validateExpressionNode` | 15 | first measurement (RFC 0080) |

There is one entry because this is the first time the compiler's complexity has
been measured at all. For reference, the pre-RFC-0074 shape of the two functions
R13 split is knowable from git but was never measured: `validateExpressionNode`
was 876 lines and `renderExpressionUncheckedWithState` 665, against 389 and 280
today. Their branch counts before the split are unrecorded, which is the gap this
file closes going forward.

## Standing finding

`validateExpressionNode` is the compiler's most complex function on both
metrics, by a factor of two over the next one. RFC 0074 R13 cut it from 876 lines
to 389 — a real 55% reduction — but the branches moved into four new functions
rather than leaving: `validateConcurrencyExpression` (118), `validateCollectionExpression`
(106), and `validateTextExpression` (82) all entered this table as a direct
result. Line count went down; branching did not.

Recorded as a finding, not a task. Acting on it needs its own spec.
