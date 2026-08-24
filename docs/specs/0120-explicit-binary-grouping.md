# RFC 0120: Explicit Binary Grouping

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Implementation-ready; design settled, implementation not started
- Features: rejection of mixed unparenthesized binary operators
- Created: 2026-08-24
- Depends on: RFC 0111 (deterministic evaluation order) and
  `docs/reference.md`

## 1. Scope

Hexal shall have no binary-operator precedence rules. An unparenthesized
expression region may contain only one binary operator token kind. That
operator may repeat. Parentheses create an explicit nested expression region.

This rule changes syntax acceptance only. It does not change operator types,
operator semantics, evaluation order, short-circuiting, overflow behavior, or
generated C for an equivalent explicitly grouped expression.

## 2. Motivation

Conventional precedence assigns meaning that is not visible in the source:

```text
result: Int32 := a + b * c
```

Hexal currently interprets that as:

```text
result: Int32 := a + (b * c)
```

The language instead requires the author to state the intended tree. This
removes precedence knowledge from the language contract and makes grouping
visible at every mixed-operator boundary.

## 3. Normative terminology

### 3.1 Binary operator token kind

Each spelling below is a distinct binary operator token kind:

```text
*  /  %  +  -  <<  >>  <  <=  >  >=  ==  !=  &  ^  |  and  or  is
```

Operators in the same conventional precedence family are still different
kinds. Therefore `+` mixed with `-`, or `*` mixed with `/`, requires grouping.

### 3.2 Expression region

An expression region is one expression parsed without crossing into a nested,
explicitly delimited expression.

The following start independent nested regions:

- grouping parentheses;
- each call argument;
- each index expression;
- each array element;
- each object or variant member initializer;
- a match scrutinee and each match-arm result;
- each expression belonging to another syntactically delimited construct.

Operators inside a nested region do not mix with operators in its containing
region.

Parentheses used for a call or another language construct delimit their
contained expressions naturally. They need no extra grouping parentheses.

## 4. Syntax constraint

Within one expression region:

- zero binary operators are valid;
- one binary operator is valid;
- any number of repetitions of the exact same binary operator token kind is
  syntactically valid; and
- encountering a different binary operator token kind is a syntax error at
  that second kind.

Valid:

```text
sum: Int32 := a + b + c + d
product: Int32 := a * b * c
ready: Bool := a and b and c

explicit: Int32 := a + ((b * c) / d)
combined: Bool := (a < b) and (c != d)
masked: Bool := (value & mask) == expected
value: Int32 := outer(a + b, c * d)
indexed: Int32 := values[offset + stride] * scale
```

Invalid:

```text
a + b * c
a + b - c
a * b / c
a << b | c
a < b and ready
a == b != c
value & mask == expected
value is Int32 == true
```

The valid explicit equivalent of the last form is:

```text
(value is Int32) == true
```

This is an exact-token rule, not a conventional-precedence-family rule.

## 5. Unary and postfix forms

Unary and postfix operations are not binary operators and do not participate
in the one-kind constraint.

These remain valid:

```text
result: Int32 := -a + -b
ready: Bool := !left and !right
total: Int32 := values[i].amount() + values[j].amount()
```

The existing restrictions and semantics for `try`, `spawn`, `ref`, calls,
member access, and indexing remain unchanged.

Assignment `=`, declaration `:=`, type-union `|`, import `=`, object-member
initializer `=`, and type-declaration punctuation are not expression binary
operators. They are outside this constraint.

## 6. Associativity

Repeated occurrences of one operator retain the existing left-associative
tree unless that operator's existing semantics state otherwise.

```text
a - b - c
```

retains the tree:

```text
(a - b) - c
```

Short-circuit operators retain their existing left-to-right behavior:

```text
a and b and c
a or b or c
```

The constraint does not make otherwise ill-typed chains valid. Parsing and
type checking remain separate. For example, a repeated comparison chain may
still fail because an intermediate comparison produces `Bool`.

## 7. Type tests

`is` participates in the rule as an infix operator even though its right side
is a type expression rather than a value expression.

- `value is Int32` remains valid.
- `value is Int32 == true` is rejected as mixed `is` and `==`.
- `(value is Int32) == true` is valid syntax.
- Existing rejection of chained `is` tests remains unchanged.

The `|` tokens inside the right-hand type expression are union separators and
do not mix with expression operators:

```text
value is Int32 | String
```

## 8. Match boundaries

Existing match parsing remains authoritative:

- an unparenthesized `|` at a match boundary starts the next arm;
- a bitwise-or scrutinee or arm result must already be parenthesized; and
- `is` following an unparenthesized scrutinee retains its match type-mode
  meaning.

After those boundary rules select an expression region, the one-operator-kind
constraint applies normally within that region.

## 9. Diagnostic

The parser shall reject the second distinct binary operator in one expression
region with the diagnostic message:

```text
mixed binary operators require parentheses; found '<later>' after '<first>'
```

`<first>` is the first binary operator kind consumed in source order in the
region. `<later>` is the first later kind that differs from it. The diagnostic
uses the ordinary `[Syntax Error] ... at <logical-key>:<line>:<column>` envelope
and locates the `<later>` token.

Examples:

```text
a + b * c
        ^ mixed binary operators require parentheses; found '*' after '+'

a * b / c
      ^ mixed binary operators require parentheses; found '/' after '*'
```

The parser owns this diagnostic because token kinds and explicit grouping are
sufficient to prove the error. The checker and generator shall never receive a
mixed unparenthesized expression.

## 10. Parser architecture

- Keep the existing expression node kinds. Do not add a grouping node solely
  for this rule.
- Each root expression and each independently delimited nested expression owns
  one operator-kind record.
- Every binary-operator parser path, including `is`, records its token before
  parsing beyond that operator.
- Recording the first operator sets the region's allowed kind.
- Recording the same kind succeeds.
- Recording another kind reports the diagnostic immediately.
- Recursively parsing an explicitly delimited nested expression pushes a fresh
  record and restores the containing record afterward.
- Match scrutinees and arm results require explicit region scopes because the
  current match parser enters the precedence ladder directly rather than
  calling the ordinary expression entrypoint.
- Internal precedence-ladder functions may remain as a recognition
  implementation, but precedence shall no longer distinguish any valid source
  program. No precedence contract remains in the reference.
- Match-boundary state remains independent of operator-kind state.

The implementation should use one small parser-owned stack or scoped record;
it shall not walk the completed AST to reconstruct parentheses, because the
current AST intentionally discards grouping-only parentheses.

## 11. Checked representation and C lowering

- Valid expressions continue to produce the existing `BinaryExpression` and
  `TypeTestExpression` trees.
- Parentheses select the tree during parsing and remain absent from the checked
  representation when they serve only grouping.
- The checker retains all operator compatibility, result-type, constant-fold,
  overflow, division, shift, equality, ordering, and union-test rules.
- RFC 0111's left-to-right evaluation contract is unchanged.
- The generator retains its fully parenthesized C spelling.
- A formerly valid precedence expression and its newly required explicitly
  grouped replacement produce the same checked tree and equivalent generated C,
  apart from source positions where applicable.

## 12. Required sweep

- Replace the precedence ladder in the normative EBNF with an expression rule
  plus the one-operator-kind constraint.
- Remove reference prose that assigns precedence to binary operators.
- Replace parser tests that accept mixed precedence with rejection and explicit
  grouping tests.
- Preserve focused tests for every individual binary operator.
- Migrate active integration fixtures and workbench snippets containing mixed
  unparenthesized operators by adding parentheses that preserve their current
  AST and behavior.
- Do not edit archived or closed specs.
- Update parser comments that describe precedence as a source-language
  contract. Internal function names may remain when they accurately describe
  parser organization rather than language semantics.
- Review the snippet manifest only after every source migration is complete.

## 13. Implementation plan

### 13.1 Baseline and inventory

1. Record the green test baseline and current snippet manifest.
2. Inventory every active parser test, integration fixture, snippet, and
   reference rule that relies on mixed precedence.
3. Record each source migration and its intended explicit tree before editing.
4. Confirm that grouping-only parentheses are discarded from the parser AST.

### 13.2 Parser enforcement

1. Add parser state for the current expression region's first binary operator
   token kind and token.
2. Push a fresh region on entry to an independently parsed nested expression
   and restore the containing region on exit, including error exits.
3. Scope match scrutinees and every match-arm result explicitly; do not rely on
   the ordinary expression entrypoint to do so.
4. Route `*`, `/`, `%`, `+`, `-`, shifts, comparisons, equality, bitwise,
   logical operators, and `is` through one recording helper.
5. Report the exact diagnostic at the first differing later operator.
6. Preserve match boundary selection and malformed-operator diagnostics.

### 13.3 Tests and source migration

1. Add focused parser acceptance for every operator repeated in its own chain.
2. Add focused rejection for different operators within each current parser
   level and across different levels.
3. Add nested-region acceptance for grouping, arguments, indexing, aggregate
   elements, member initializers, matches, and type tests.
4. Migrate every recorded active source using the minimum parentheses that
   preserve its current tree.
5. Retain all checker assertions after source migration; do not weaken an
   assertion to accommodate a different tree.

### 13.4 Generated-output verification

1. Compare each migrated program's checked tree with its pre-migration tree.
2. Confirm representative generated C remains equivalent and fully
   parenthesized.
3. Regenerate snippet hashes only where added source parentheses alter emitted
   source positions or artifact text.
4. Review every changed artifact family; no helper, representation, include,
   or runtime change is permitted.

### 13.5 Canonical synchronization and closure

1. After behavior stabilizes, update the expression EBNF and binary-operator
   contracts in `docs/reference.md` once.
2. Preserve RFC 0111's evaluation-order rules while removing precedence
   wording.
3. Remove this RFC's open status entry, mark it implemented, and archive it
   only after all validation passes.
4. Rebuild and restart the workbench for handoff.

## 14. Validation

This section is exhaustive. RFC 0120 is complete only when every item below
passes.

### 14.1 Accepted syntax

- A single use of every binary operator compiles when its operands satisfy
  existing semantic rules.
- Repeated `+`, `-`, `*`, `/`, `%`, `<<`, `>>`, `<`, `<=`, `>`, `>=`, `==`,
  `!=`, `&`, `^`, `|`, `and`, and `or` parse without a mixed-operator
  diagnostic.
- Parentheses permit every pair of different binary operators in both nesting
  directions.
- Different operators in separate call arguments, index expressions, array
  elements, object and variant initializers, match regions, and other nested
  delimited expressions do not conflict.
- Unary and postfix forms mixed with one binary operator kind remain accepted.
- A parenthesized type test may participate in another binary expression.
- A union type on the right of `is` does not count its `|` as an expression
  operator.

### 14.2 Rejected syntax

- Every ordered pair of different binary operator kinds is rejected when both
  occur in one unparenthesized expression region and the token sequence is
  otherwise grammatically meaningful.
- Same-family mixtures `+`/`-`, `*`/`/`, `*`/`%`, comparisons, equality, and
  shifts are rejected exactly like cross-family mixtures.
- Logical `and`/`or`, bitwise operators, and their mixtures with comparisons
  or equality are rejected without grouping.
- `is` mixed with any expression binary operator is rejected without grouping.
- The diagnostic names the first operator, names the first different later
  operator, and points at the later token.
- Existing malformed-expression and chained-`is` diagnostics retain earliest
  ownership when they occur before a mixed-operator violation can be proven.

### 14.3 Preserved semantics and artifacts

- Repeated same-operator chains retain their current associativity, checking,
  evaluation order, short-circuiting, folding, and lowering.
- Every migrated mixed expression's explicitly grouped form retains its
  previous checked tree and behavior.
- Generated C remains fully parenthesized and introduces no new helper or
  runtime support.
- No non-expression generated representation or component changes.
- Manifest changes are limited to snippets whose source required grouping and
  are explained by the corresponding source or source-map change.
- Repeated compilations produce identical artifacts.
- Ordinary tests remain pure Go.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## 15. Reference synchronization

Implementation shall update `docs/reference.md` after behavior stabilizes:

- remove the conventional binary-precedence ladder as a language contract;
- define the one-binary-operator-kind rule and expression-region boundaries;
- retain repeated-operator associativity and short-circuit semantics;
- retain unary, postfix, match-boundary, type-test, and evaluation-order rules;
  and
- state the exact mixed-operator diagnostic.

No canonical documentation is changed while this RFC remains unimplemented.
