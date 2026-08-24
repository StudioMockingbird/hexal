# RFC 0114: ADT Block Syntax

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented 2026-08-24. Stage 1's parser change moves the
  ADT-vs-object/alias dispatch into `typeDeclaration` itself (checking `as`
  before consuming `=`), replaces `adtDefinitionExpression` with `adtBlock`
  (consumes `as`, loops variants, requires `end`), and adds all three
  required diagnostics; `typeDefinitionExpression` now rejects a leading `|`
  with the exact obsolete-header message instead of dispatching to it.
  Stage 2 required zero checker changes: `checkADTDeclaration` and every
  other consumer already reads `AdtDefinitionExpression`/`AdtVariantDeclaration`
  by field, never by syntax origin, confirmed by reading `compiler/checker/adt.go`
  end to end. Stage 3 migrated every fixture using the obsolete syntax (30
  occurrences across 15 test files plus the five catalog snippets and
  `text-protocol-parser`'s `Token` ADT, found via a repo-wide sweep that
  caught two files — `compiler/tests/integration/io_test.go` and
  `unions_test.go` — the initial inventory missed) via a small reviewed Go
  script under `.tmp/`, plus new parser tests for the new grammar and each
  diagnostic. Because every migrated line kept its declaration on the same
  source line, no `#line` movement occurred and the snippet manifest needed
  no rebuild — every generated artifact is byte-identical to before the
  migration, stronger than Stage 4's own "permit `#line`-only differences"
  bar. Stage 5 replaces the ADT EBNF in `docs/reference.md` (`type-declaration`,
  `type-definition-expression`, the new `adt-block`/`adt-variant` rules) and
  updates the one prose ADT example (`Seek`) to the new opening/closing
  tokens; structural-union, `Nil`/`Error`/`EoS`, and `try`/`errdefer`
  contracts are untouched. `docs/status.md`'s stale open entries for this
  RFC and the already-closed RFC 0111 are both removed.
- Features: delimited ADT declarations and removal of per-variant `as`
- Created: 2026-08-22
- Updated: 2026-08-24 — closed after implementation, migration, and
  reference synchronization.
- Depends on: RFC 0019 (generics), RFC 0034 (modules), and
  `docs/reference.md`
- Coordinates with: C interop RFC 0039

## Summary

Change only the source syntax for nominal algebraic data type declarations:

```text
type Shape as
    | Circle { radius: Int32 }
    | Rectangle { width: Int32, height: Int32 }
end
```

The `as` after the type header opens the ADT block. `end` closes it. A variant
payload follows the variant name directly; variants no longer use their own
`as` token.

This RFC does not simplify Hexal by removing a sum-type model. Hexal has two
complementary models, and both remain:

- Structural unions describe anonymous alternatives by type.
- ADTs describe nominal alternatives by named variant.

`Nil`, `Error`, and `EoS` are types that may participate in structural unions;
they are not separate sum mechanisms.

## Problem

The current ADT syntax is visually open-ended:

```text
type Shape =
    | Circle as { radius: Int32 }
    | Rectangle as { width: Int32, height: Int32 }
```

Its declaration has no closing delimiter, and every payload repeats `as`.
This is inconsistent with Hexal's other multi-line forms, which open once and
close with `end`.

The original version of this RFC also proposed removing general structural
unions. That proposal solved no demonstrated duplication:

- structural unions and ADTs provide different identities and construction
  models;
- existing compiler APIs use arbitrary structural unions;
- `try` depends on structural unions containing the protected `Error` type;
  and
- removing structural unions would replace a small, composable feature with
  extra named declarations.

The RFC is therefore limited to the ADT syntax cleanup.

## Goals

- Give every multi-line ADT declaration an explicit closing delimiter.
- Remove the redundant per-variant `as` token.
- Preserve all existing ADT and structural-union semantics.
- Preserve generated C identities, layouts, tags, and helper behavior.
- Keep the grammar unambiguous and the migration mechanical.

## Non-goals

- Restricting or removing structural unions.
- Adding `T?` syntax.
- Adding compiler-owned `Option` or `Result` types.
- Changing `Nil`, `Error`, or `EoS` semantics.
- Changing `try` or `errdefer` semantics.
- Changing ADT construction, matching, equality, generics, layout, or
  ownership.
- Defining foreign C union syntax or representation; RFC 0039 owns that work.

## Grammar

Replace the current ADT branch of `type-declaration` with:

```ebnf
type-declaration =
    "type", identifier, [ generic-parameter-list ],
    ( "=", type-definition-expression
    | adt-block );

type-definition-expression =
    object-type-expression
  | type-expression;

adt-block =
    "as", adt-variant, { adt-variant }, "end";

adt-variant =
    "|", identifier, [ adt-payload ];

adt-payload =
    "{", payload-member, { ",", payload-member }, [ "," ], "}";
```

The existing object and transparent-alias forms continue to use `=`:

```text
type Point = { x: Int32, y: Int32 }
type Count = Int32
```

`=` is not reserved for aliases. It continues to introduce every non-ADT type
definition currently accepted by `type-definition-expression`.

## ADT contracts

- An ADT declaration contains at least two variants.
- Variant names are unique within the ADT.
- A variant is either unit-like or has one fixed record payload.
- Payload fields retain their existing name, type, mutability, and placement
  rules. This RFC does not admit mutable payload fields.
- A trailing comma in a non-empty payload remains accepted.
- ADTs retain nominal identity owned by their declaring module.
- Generic parameters and specialization retain their existing rules.
- Export, source-order visibility, recursive-type, construction, matching,
  exhaustiveness, equality, printing, and ownership rules are unchanged.
- Construction retains the existing qualified spelling:

  ```text
  shape: Shape := Shape.Circle { radius = 4 }
  direction: Direction := Direction.North
  ```

- Narrowing remains available through the existing `match` semantics. This
  RFC does not add a standalone variant-test operator.

## Structural-union contracts

Structural unions remain a general source-language feature.

- Any combination of otherwise valid types is accepted when every canonical
  member is distinct:

  ```text
  value: Int32 | String | Error := 13
  ```

- Union identity remains flattened, structural, duplicate-free, and
  independent of written member order.
- Repeating a canonical member remains an error after alias resolution and
  generic substitution:

  ```text
  type Number = Int32
  value: Int32 | Number | Nil := nil  -- rejected: Int32 is repeated
  ```

- `Nil` has no standalone value type. It remains valid only as a union member.
- `Error` and `EoS` retain their protected identities and existing union
  behavior.
- `try` and `errdefer` recognize only the exact protected `Error` type as the
  error member of a structural union.
- User declarations named `Option` or `Result` are ordinary ADTs. They receive
  no compiler-owned propagation, narrowing, representation, or construction
  behavior:

  ```text
  type Result<T, E> as
      | Ok { value: T }
      | Err { error: E }
  end
  ```

  Applying `try` to this ADT is rejected even when `E` is `Error`.
- No `T?`, builtin `Option<T>`, or builtin `Result<T, E>` form exists.

## Diagnostics

The parser owns obsolete ADT spelling because it can identify it before type
checking.

- `type Name = | ...` reports `Syntax Error: ADT declarations use 'type Name as ... end'`.
- `| Variant as { ... }` inside an ADT block reports
  `Syntax Error: ADT payload follows the variant name directly; remove 'as'`.
- An unterminated ADT block reports
  `Syntax Error: expected 'end' after ADT declaration`.
- Existing checker diagnostics remain authoritative for fewer than two
  variants, duplicate variants, invalid payloads, and all semantic errors.
- `try` or `errdefer` applied to a custom ADT retains the ordinary existing
  diagnostic for an operand that is not a structural union containing the
  protected `Error` type.

## Checked representation and C lowering

- Parsing the new form produces the existing ADT checked representation.
- The checker must not add a second ADT kind or a syntax-origin flag.
- Equivalent old and new declarations have identical nominal identities,
  variant order, payload types, specialization keys, and generated C names.
- C structs, discriminants, payload fields, constructors, match lowering,
  equality helpers, and print helpers are unchanged.
- No structural-union parser, checker, registry, narrowing, representation, or
  generator code is removed or restricted.
- Source-map line numbers may change when migrated source gains `end` or loses
  per-variant `as`; no other generated-C change is permitted.

## Required sweep

- Remove the parser path for `type Name = | ...`.
- Remove parsing of per-variant `as` payload introducers.
- Reuse `AdtDefinitionExpression` and `AdtVariantDeclaration`; do not create a
  parallel AST or checked representation.
- Update parser tests, checker source fixtures, integration tests, diagnostics,
  and workbench snippets that use the obsolete spelling.
- Mechanically migrate the five catalog ADTs: `Direction`, `Command`, `Shape`,
  `Protocol`, and `Event`.
- Preserve every structural-union implementation and test except where an ADT
  fixture's spelling changes.
- Remove comments that describe the obsolete ADT grammar; replacement comments
  must satisfy CARE.

## Implementation plan

### Baseline findings

Probed against the parser at HEAD before writing this plan:

- **Dispatch has no separate ADT parser entry.** `typeDeclaration`
  (`compiler/parser/statements.go:11`) unconditionally consumes `=`
  (`parser.consume(lexer.Equal, "'='")`) then calls
  `typeDefinitionExpression` (`compiler/parser/type_expressions.go:287`),
  which branches on the *current* token: `{` to `objectTypeExpression`, `|`
  to `adtDefinitionExpression`, anything else to the generic
  `typeExpression()` (covering aliases and named types). Since the new `as`
  form uses a different opening token than `=` entirely, the branch point
  for RFC 0114 must move up into `typeDeclaration`, choosing `as` vs `=`
  before either is consumed.
- **`adtDefinitionExpression`** (`type_expressions.go:299-324`) is a single
  loop: consume `|`, consume the variant identifier, optionally consume `as`
  then an `objectTypeExpression()` payload (rejecting `mut` fields with
  `"ADT payload fields cannot be mutable"`), loop while another `|` follows.
  There is no closing delimiter today; the loop just stops when the next
  token isn't `|`.
- **`AdtDefinitionExpression`/`AdtVariantDeclaration`** (`compiler/parser/ast.go:64-72`)
  need no field changes: `AdtVariantDeclaration{Name, Payload *ObjectTypeExpression}`
  and `AdtDefinitionExpression{Variants []AdtVariantDeclaration}` already
  carry exactly what the new grammar produces.
- **`As` and `End` are already keywords** (`compiler/lexer/lexer.go`: kinds
  at lines 174/186, keyword table at lines 220/232). `As` is referenced in
  exactly one place in the whole parser today (`type_expressions.go:308`),
  so detecting a bare `as` inside the new ADT block is unambiguous evidence
  of the obsolete per-variant introducer. `End` already closes `if`/`while`/`for`/`match`
  via the established `parser.consume(lexer.End, "'end' to close X")` /
  `"'end' after X"` pattern (`statements.go:425,454,497`, `expressions.go:717`),
  which is reused verbatim for the ADT block's own diagnostic.
- **Diagnostic rendering**: `errorAt`/`errorAtCurrent` (`parser.go:455-462`)
  wrap a literal message into a `SyntaxError` diagnostic; `consume` prefixes
  `"expected "` automatically. The RFC's three required messages therefore
  map directly: the obsolete-header and obsolete-payload messages are full
  sentences passed to `errorAtCurrent` directly (not through `consume`,
  which would double as `"expected ADT declarations use..."`), and the
  missing-`end` message is the noun phrase `"'end' after ADT declaration"`
  passed to `consume(lexer.End, ...)`, matching the existing `"expected 'end' after a match expression"` convention exactly.
- **A leading `|` is never valid for `typeExpression()` either**: a
  structural union is written `Int32 | String` (infix), never `| Int32`, so
  rejecting a leading `|` in `typeDefinitionExpression` with the exact
  obsolete-header message cannot shadow any legitimate type expression.
- **Migration inventory** (old-syntax fixtures, verified via repo-wide
  search; every hit is a genuine ADT declaration since `as {` has no other
  meaning in the grammar): `compiler/parser/adt_test.go` (4),
  `compiler/checker/adt_test.go` (~19), `compiler/checker/io_test.go` (2),
  `compiler/checker/modules_visibility_test.go` (2),
  `compiler/checker/nullability_test.go` (1),
  `compiler/generator/adt_test.go` (3),
  `compiler/tests/c23validation/c23_smoke_test.go` (1),
  `compiler/tests/integration/adt_test.go` (~15),
  `compiler/tests/integration/collections_test.go` (1),
  `compiler/tests/integration/concurrency_test.go` (1),
  `compiler/tests/integration/equality_test.go` (1),
  `compiler/tests/integration/evaluation_order_test.go` (1, added by RFC
  0111's own work in this session),
  `compiler/tests/integration/modules_visibility_test.go` (1),
  `compiler/tests/integration/inferred_declaration_test.go` (2),
  `workbench/snippets/categories/04-types-and-matching.json` (the five
  catalog ADTs: `Direction`, `Command`, `Shape`, `Protocol`, `Event`).
- **Discrepancy resolved**: `workbench/snippets/categories/08-text.json`
  (snippet `text-protocol-parser`) also declares an old-syntax ADT (`Token`)
  not among the RFC's named "five catalog ADTs." Since the old syntax is
  removed outright rather than deprecated, this snippet would fail to parse
  after the change regardless. It is migrated as a hard compile requirement;
  "no unrelated snippet source" in the Required sweep means no unrelated
  snippet's *content* changes, not that a syntax-breaking fixture is left
  broken.

### Stage 0: baseline and inventory

1. Record the current test result and snippet manifest state before editing
   (done above; full suite is green at HEAD).
2. Search all compiler tests, integration tests, snippets, and active docs for
   `type` declarations whose definition begins with `|` (done above; see
   Baseline findings for the complete file list).
3. Record the complete migration set before changing the grammar (done; see
   Baseline findings, including the resolved `text-protocol-parser`
   discrepancy).
4. Confirm the current checked ADT node and generator inputs so the syntax
   change cannot create a parallel semantic path (done: `AdtDefinitionExpression`/`AdtVariantDeclaration`
   need no field changes; the checker and generator consume them by field,
   never by provenance).

### Stage 1: parser grammar

1. In `typeDeclaration` (`statements.go:11`), after `genericParameterList()`,
   check `parser.check(lexer.As)` before the existing `consume(lexer.Equal, "'='")`:
   on `as`, delegate to a new `adtBlock()` method and return its
   `AdtDefinitionExpression` as `Target`; otherwise keep the existing `=`
   path completely unchanged.
2. Add `adtBlock()` (`type_expressions.go`, beside the removed
   `adtDefinitionExpression`): consume `as`, then loop while `|` is present
   (consume `|`, consume the variant identifier, reject a following `as`
   with the exact obsolete-payload message, parse an optional `{ ... }`
   payload directly via the existing `objectTypeExpression()` and the
   existing mutable-field rejection), then require `end` via
   `consume(lexer.End, "'end' after ADT declaration")`.
3. In `typeDefinitionExpression` (`type_expressions.go:287`), replace the
   `case lexer.Pipe: return parser.adtDefinitionExpression()` branch with an
   explicit rejection: `errorAtCurrent("ADT declarations use 'type Name as ... end'")`.
   Delete `adtDefinitionExpression` (fully superseded by `adtBlock`).
4. Continue producing the existing `AdtDefinitionExpression` and
   `AdtVariantDeclaration` nodes verbatim (no field changes, per Baseline
   findings).
5. Keep `=` dispatch, `objectTypeExpression`, and every other
   `type-definition-expression` branch (object, alias, named type) completely
   unchanged.

### Stage 2: checker preservation

1. Route the existing ADT node through the existing declaration, resolution,
   specialization, recursion, construction, and match paths unchanged (no
   checker source edits are expected; verify by reading `compiler/checker/adt.go`
   end to end after Stage 1 lands, confirming nothing there depends on
   parser-internal state beyond the `AdtDefinitionExpression`/`AdtVariantDeclaration`
   fields).
2. Retain existing semantic diagnostics for variant count, duplicate names,
   payload fields, placement, and recursion.
3. Add no `Option`, `Result`, nullable-sugar, or propagation special case.
4. Verify that `try` and `errdefer` still require the protected `Error` member
   of a structural union.

### Stage 3: migration

1. Add new parser tests for the `as ... end` form (unit and record variants),
   and for each of the three new diagnostics (obsolete header, obsolete
   per-variant `as`, missing `end`), before touching any existing fixture,
   so the new grammar is proven independently of the migration.
2. Convert every fixture recorded in Baseline findings from
   `type Name = | Variant as {...}` (and `type Name = | UnitVariant`) to
   `type Name as | Variant {...} ... end`, using a small Go migration
   program under `.tmp/` per this repo's convention for changes too broad
   for exact-match edits, then reviewing every diff by hand rather than
   trusting the script blindly.
3. Update exact parser-diagnostic assertions without weakening unrelated
   assertions.
4. Migrate the five catalog snippets plus `text-protocol-parser` (see
   Baseline findings) and no unrelated snippet source.
5. Do not edit archived or closed specs; they are historical records.

### Stage 4: generator verification

1. Confirm the checked ADT representation reaching code generation is
   unchanged.
2. Compare representative unit, payload, generic, exported, and cross-module
   ADT artifacts before and after migration.
3. Permit differences only in `#line` locations caused by source-line movement.
4. Confirm structural-union artifacts are byte-identical.

### Stage 5: canonical documentation and closure

1. After behavior stabilizes, update only the affected grammar and ADT syntax
   contracts in `docs/reference.md`.
2. Verify that the structural-union, `Nil`, `Error`, `EoS`, `try`, and
   `errdefer` contracts remain unchanged. Do not add `?`, builtin `Option`, or
   builtin `Result` rules.
3. Remove this RFC's open entry from `docs/status.md`, mark the RFC implemented,
   and archive it only after every validation item passes.
4. Rebuild and restart the workbench for handoff.

## Validation

This section is exhaustive. RFC 0114 is complete only when every item below
passes.

### Syntax

- Unit, record-payload, generic, exported, and cross-module ADTs compile with
  `type Name as ... end`.
- A payload permits its existing field syntax and optional trailing comma.
- Object declarations and transparent aliases continue to compile with `=`.
- `type Name = | ...` is rejected with the exact obsolete-header diagnostic.
- Per-variant `as` is rejected with the exact obsolete-payload diagnostic.
- A missing `end` is rejected with the exact unterminated-block diagnostic.
- One-variant ADTs, duplicate variants, empty payloads where currently invalid,
  mutable payload fields, and invalid recursive ADTs retain their existing
  checker diagnostics.

### ADT behavior

- Unit and record variants retain their existing qualified construction forms.
- Match exhaustiveness, payload binding, equality availability, printing
  availability, source-order visibility, exports, and nominal module identity
  are unchanged.
- Generic ADTs specialize deterministically and emit no duplicate definition.
- Equivalent migrated declarations retain variant order and generated tag
  values.

### Structural-union preservation

- `Int32 | String | Error` compiles.
- Reversed spellings of the same members share one structural-union identity.
- Repeated canonical members are rejected after direct spelling, alias
  resolution, nested-union flattening, and generic substitution.
- `Nil` remains invalid alone and valid as a union member.
- Existing `EoS` completion unions retain their checked behavior.
- `try` and `errdefer` accept only structural unions containing the exact
  protected `Error` member.
- A user-defined `Result<T, E>` ADT compiles as an ordinary generic ADT, but
  `try` applied to its specialization is rejected.
- `T?`, builtin `Option`, and builtin `Result` receive no grammar or compiler
  support.

### Generated artifacts and gates

- Representative migrated ADTs preserve C identities, layouts, discriminants,
  payload fields, constructors, match lowering, and helpers.
- Structural-union generated artifacts are byte-identical to the baseline.
- No existing non-ADT snippet manifest entry changes.
- The five migrated ADT snippets may change hashes only for artifacts whose
  source maps move; every such diff is reviewed, and no representation or
  helper spelling changes.
- Repeated compilations produce identical artifacts.
- Ordinary tests remain pure Go.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

Implementation updates `docs/reference.md` once, after behavior stabilizes:

- replace the ADT EBNF with the `type Name as ... end` block;
- remove per-variant `as` from the ADT syntax contract;
- retain `=` for objects, transparent aliases, and every existing non-ADT type
  definition;
- preserve all structural-union and protected `Nil`/`Error`/`EoS` contracts;
- preserve the exact-`Error` requirement for `try` and `errdefer`; and
- add no nullable shorthand or compiler-owned `Option`/`Result` type.

No canonical documentation is changed while the RFC remains unimplemented.
