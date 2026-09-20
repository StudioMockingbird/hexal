# RFC 0135: Scalar Value Match

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented
- Created: 2026-08-27
- Updated: 2026-09-20
- Origin: RFC 0103 finding F2
- Depends on: RFC 0133 (closed; its exhaustiveness and pattern-identity model is available)
- Coordinates with: `docs/reference.md`

## Summary

Extend value-mode `match` beyond Bool so scalar constants can be dispatched
without rewriting the operation as an `if` chain.

This is a language feature, not a compiler bug. The current reference and
grammar deliberately define Bool as the only value-mode scalar domain.

RFC 0133's former dependency is complete: the checker already has the ordered
closed-domain coverage table, canonical union identity, exact-type coverage,
neutral dotted patterns, duplicate/unreachable-arm diagnostics, and
exhaustive-`else` rules. Those facilities serve Bool value mode and type-mode
matches. They do not accept scalar literal patterns, and the coverage table is
closed-domain only: an integer scrutinee today yields an open domain with no
case table, so the open-domain duplicate tracking this RFC needs does not exist
yet and is part of the work below.

## Motivation

Hexal targets C23 and must express switch-like dispatch directly. Restricting
value mode to Bool makes ordinary integer and enum-shaped protocols need
repeated comparisons:

```hexal
let result: Int32 = match opcode
  | 1 then handle_open()
  | 2 then handle_close()
  | else then -1
end
```

## Recommended scope

- Retain Bool and admit fixed-width integers, `Size`, `Byte`, and `Rune` values
  whose arm patterns are literal constants of the scrutinee type.
- Keep Float32/Float64 out: NaN and signed-zero make exact coverage and
  duplicate detection unintuitive.
- Keep String and Strand out initially: text matching needs a separate cost and
  lowering decision and does not map to a C `switch`.
- Require a final `else` for every open integer-like domain. No integer domain
  is exhaustively enumerated, regardless of its machine width.
- Continue to reject duplicate and unreachable arms. Closed domains reuse RFC
  0133's ordered table; open domains use a seen-set.
- Reuse the existing ordered branch lowering in the first version. C23 `switch`
  lowering is deferred behind a measurement.

## Admitted domains and typing

| Scrutinee | Pattern literal | Domain | Final `else` | Duplicate key |
| --- | --- | --- | --- | --- |
| `Bool` | `true` / `false` | closed, two cases | optional (existing rule) | case name |
| `EoS` | `eos` | closed, one singleton | optional; an `else` after `eos` is unreachable | singleton |
| `Int8`..`Int64` | integer literal | open | required | contextual constant |
| `UInt8`..`UInt64`, `Byte` | integer literal | open | required | contextual constant |
| `Size` | integer literal | open | required | contextual constant |
| `Rune` | rune literal | open | required | contextual constant |

`Byte` is the canonical transparent alias of `UInt8`
(`compiler/types/types.go`), so it shares one identity and one domain; the table
names it for spelling only. `Rune` lowers to `uint32_t` and shares the unsigned
machinery, but its pattern is a rune literal and its values exclude surrogates by
the existing Rune rule. `Size` follows the existing `Size`-literal rule and is
never widened from another numeric type.

A scalar pattern literal is contextually typed to the scrutinee type through the
existing literal path (`compiler/checker/literals.go`, `conversions.go`): the
same `go/constant` value, the same range check, and the same diagnostic the
language already uses for a literal in an annotated declaration. A value outside
the scrutinee's range is rejected at the pattern with the existing range
diagnostic. A rune pattern naming a surrogate is rejected by the existing Rune
validity rule.

Float32/Float64 and String/Strand are not admitted. A value-mode match whose
scrutinee is one of those types and whose arm is a scalar literal is rejected
with `match value mode does not support <type> scrutinees; use Bool, EoS, or an
integer-like type`, not left as a parse error.

## Open-domain coverage and duplicate identity

Closed domains keep the existing `matchCoverage` table
(`compiler/checker/adt.go`): Bool's two cases, an ADT's variants, a union's
canonical members, and the one exact type in type mode. Those are unchanged.

An integer-like scrutinee is an open domain. Its coverage is a seen-set, not a
fixed table, layered beside the closed table:

- the checker derives each scalar pattern's contextual constant and a canonical
  key; the key is the constant's exact value after typing to the scrutinee type,
  so `1`, `0x1`, and `01` are one key;
- a repeated key is `duplicate or unreachable match pattern`, reported at the
  repeated pattern;
- the domain stays open, so a final `else` is required; a match without one is
  `match on <type> requires a final else`;
- arms remain in source order and the first matching arm wins;
- a scalar pattern selects an arm and introduces no narrowing fact: the matched
  value keeps its declared scalar type inside the arm.

The seen-set is keyed by a string so it reuses the existing `find`/`cover`
coverage shape; the exact Go type is implementation-owned.

## Pattern grammar

`MatchPattern` gains scalar literals and `eos` without disturbing the existing
type/variant interpretation:

```
MatchPattern = "else" | "true" | "false" | "eos"
               | ScalarPattern
               | QualifiedVariantPattern | PrimaryTypeExpression .
ScalarPattern = [ "-" ] ( integer_literal | byte_literal | rune_literal ) .
```

- `integer_literal` is the existing production; a leading `-` admits negative
  signed patterns. A negative pattern against an unsigned scrutinee is rejected
  by the ordinary range check.
- `byte_literal` and `rune_literal` are the existing literal productions, so a
  Byte or Rune pattern reuses the spelling the expression grammar already has.
- `eos` is added explicitly because `eos` is the `Eos` token, not an
  `Identifier`, so it cannot arrive through `PrimaryTypeExpression`.
- A bare identifier arm is still a type or variant pattern; a numeric or rune
  literal arm is a scalar pattern. The alternatives are disjoint on the first
  token, so no lookahead beyond the token kind is needed.
- The open `dotted-match-pattern` EBNF ambiguity recorded in `docs/status.md`
  (owned by RFC 0133) remains and is fixed there; this RFC adds only the
  disjoint scalar alternative and does not widen it.

## Lowering

The checked match carries each scalar arm's contextual constant. The generator
reuses the existing ordered lowering (`compiler/generator/adt.go`,
`renderMatchStatement`): the scrutinee is evaluated once into a temporary, the
scalar arms chain as `if (<temp> == <constant>) { <result> = ...; } else if ...`
so the first matching arm wins, and the required `else` becomes the final
`else`. Bool, EoS, ADT, and union arms keep their current tag comparisons and
generated text. `#line` mapping is unchanged.

C23 `switch` lowering is explicitly deferred. The ordered lowering is already
correct, is shared with Bool and the union/ADT paths, and needs no second code
path; a `switch` optimization is justified only by a measurement (see Open
questions).

## Rejection diagnostics

- Duplicate scalar constant after contextual typing:
  `duplicate or unreachable match pattern`.
- Out-of-range scalar constant: the existing literal range diagnostic, at the
  pattern.
- Missing final `else` on an open integer-like domain:
  `match on <type> requires a final else`.
- An `else` after `eos` (closed singleton already covered):
  `duplicate or unreachable match pattern`.
- Float scrutinee: `match value mode does not support Float32 scrutinees`.
- String/Strand scrutinee: `match value mode does not support String scrutinees`.

## Current implementation state

Already present and reused:

- `compiler/parser/ast.go` has `MatchPattern`, `BoolPattern`, `ElsePattern`,
  type patterns, variant patterns, and neutral dotted patterns;
- `compiler/parser/expressions.go` already parses match-arm boundaries and
  preserves ambiguous dotted patterns until checking;
- `compiler/checker/adt.go` already builds the closed ordered coverage table,
  canonicalizes union-member identity, checks exact types, rejects duplicate and
  unreachable patterns, and reports first-missing cases;
- `compiler/checker/literals.go`, `conversions.go`, and related checker files
  already use `go/constant` for contextual numeric values, range checks,
  conversions, and constant folding; and
- `compiler/generator/adt.go` already evaluates the scrutinee once and emits
  deterministic ordered match branches with source mapping support.

Not present (the work):

- scalar literal pattern parsing (`ScalarPattern` and `eos`);
- contextual scalar pattern typing and range diagnostics;
- the open-domain seen-set and duplicate identity;
- the mandatory-`else` rule for open domains;
- Float and String rejection diagnostics; and
- the parser, checker, generator, integration, and artifact tests below.

The current normative reference remains Bool-only in value mode. The reference
and grammar are updated when implementation begins and the accepted syntax,
diagnostics, and generated-C contract change together.

## Settled design decisions

1. Scalar patterns are literals only. Named compile-time constants remain future
   work for RFC 0117; the interim limitation is that `match` dispatches on a
   literal, not on a `const` or a named value.
2. EoS is a closed singleton domain: `| eos` covers its only value, so a final
   `else` is optional and, if present, unreachable and rejected.
3. Open integer-like domains (fixed-width integers, `Byte`/`UInt8`, `Size`,
   `Rune`) require a final `else`; machine width never implies exhaustive
   enumeration.
4. Scalar patterns introduce no narrowing fact.
5. The first version reuses the existing ordered lowering; C23 `switch` is
   deferred.
6. Bool value-mode lowering is unchanged, byte-for-byte.

## Non-goals

- Pattern alternation, guards, ranges, destructuring, or unqualified ADT
  variants.
- Named constant patterns and scalar narrowing facts.
- Changing type-mode match.
- General compile-time evaluation.
- Float and text scrutinees.
- C23 `switch` lowering in the first version.

## Validation

Every case below is a test the implementation must add; this section is the
definition of done.

Parser:

1. `| 1`, `| -1`, `| 0x1`, `| 'a'`, `| b'a'`, and `| eos` parse as scalar or
   EoS patterns.
2. A bare identifier arm still parses as a type or variant pattern.
3. Float and string literal arms remain outside `ScalarPattern`.

Checker, accepted:

4. One match per admitted family (`Int8`, `Int16`, `Int32`, `Int64`,
   `UInt8`/`Byte`, `UInt16`, `UInt32`, `UInt64`, `Size`, `Rune`) with a final
   `else` checks.
5. `| eos` checks against an `EoS` scrutinee with no `else`.
6. Bool value mode is unchanged.

Checker, rejected:

7. `| 1` and `| 0x1` (or `| 0o1`) in one match: duplicate.
8. A constant outside the scrutinee range: range diagnostic at the pattern.
9. An open integer-like match without `else`: missing-`else` diagnostic.
10. `| eos` followed by `else`: unreachable.
11. A negative pattern against an unsigned scrutinee: range diagnostic.
12. A rune pattern naming a surrogate: Rune-validity diagnostic.
13. Float and String scrutinees: the explicit rejection diagnostic.
14. Duplicate, unreachable, and exact-type behavior for Bool, ADT, union, and
    type mode is unchanged (RFC 0133 tests still pass).

Generator:

15. Scalar matches evaluate the scrutinee once, preserve arm order, and emit
    deterministic C with correct `#line`.
16. Bool, ADT, and union matches generate byte-identical C to before.

Artifacts:

17. The workbench catalog gains a scalar-match snippet only if one is added; the
    generated-artifact manifest is rebuilt only for that snippet, reviewed, and
    reported.

## Open questions

1. Is a C23 `switch` lowering worth a second generator path? Decide with a
   benchmark of a many-arm integer match; if the ordered lowering is within
   noise, keep one path.
2. Is admitting `EoS` value-mode `match` worth the surface? It is a singleton,
   so `| eos` alone is a no-op, and union EoS dispatch already works through
   type mode (`match value is | EoS then ...`).
3. Should a later revision admit ranges (`1...5`) and multi-value arms
   (`1, 2`)? Zig and Odin have them; Hexal's `if`/`elseif` covers the need
   today.
4. Should named constant patterns arrive with RFC 0117, and do they require a
   new pattern node or a resolved-constant arm?

## Current gaps

As of this revision, a program such as:

```hexal
let result: Int32 = match opcode
  | 1 then 10
  | 2 then 20
  | else then 0
end
```

is not part of the accepted language. The parser's `MatchPattern` grammar has no
scalar-literal production, the checker has no open-domain coverage case, and the
generator carries no scalar arm constant. These are expected gaps, not
regressions in RFC 0133's completed implementation.
