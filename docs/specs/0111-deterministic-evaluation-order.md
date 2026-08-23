# RFC 0111: Deterministic Evaluation Order

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Draft; one design decision required. The evaluation-order rules are
  settled and implementation-ready; the mixed-operator rejection bundled with
  them is a separate language-surface change awaiting a decision -- see Scope.
- Updated: 2026-08-23
- Features: left-to-right expression, call, aggregate, and assignment
  evaluation
- Created: 2026-08-22
- Depends on: RFC 0014 (expressions and operators), RFC 0061 (structured
  bodies), and `docs/reference.md`
- Coordinates with: RFC 0083 (text and collection surface), RFC 0094
  (anonymous function literals), RFC 0108 (descriptor and memory streams), and
  generated-C lowering

## Scope

Hexal source evaluation is fully ordered. C23's unspecified operand and
argument order is not observable Hexal behavior and must not be inherited by
generated C.

## Blocking question — the mixed-operator rule is a second, separate change

This RFC currently also rejects unparenthesized mixed binary operators, making
`a + b * c`, `a + b - c`, `a and b or c`, and `a < b == c` Syntax Errors. That
is a language-surface change of a different kind from everything else here, and
it should be decided separately. Three reasons.

**It does not serve determinism.** Grouping and evaluation order are different
properties. `a + b * c` evaluates `a`, then `b`, then `c` under either grouping;
so does `(a + b) * c`. Parenthesizing changes *what is computed*, not *in what
order operands are evaluated*. The order rules below already make evaluation
fully ordered without it, so the rule's stated justification — "so every
evaluation order is syntactically explicit" — does not hold.

**It deletes a normative documented grammar.** `docs/reference.md:118-126`
encodes precedence as an EBNF cascade — `relational-expression` over
`shift-expression` over `additive-expression` over `multiplicative-expression`
over `unary-expression`. Grouping is therefore already specified and
unambiguous. The mixing rule replaces a documented precedence structure with
mandatory parentheses; that is a legibility argument, which may well be a good
one, but it is not this RFC's argument.

**Its weakest case is `a + b - c`.** Same precedence, left-associative by the
cascade's `{ additive-operator , multiplicative-expression }` repetition. There
is no ambiguity for a reader or for the grammar, and the rule rejects it anyway.

Blast radius: 11 mixed-operator sites in the workbench catalog alone, plus
every future expression written in the language.

**Recommendation: split.** Ship the evaluation-order rules below, which are
uncontroversial and pin down what C leaves unspecified. Move the mixed-operator
rejection to its own RFC where it is argued on legibility grounds against the
documented precedence cascade it would replace. If the author decides to keep
both here, the Scope sentence must say the RFC changes operator grouping as
well as evaluation order, and the precedence cascade in `reference.md` must be
listed for deletion rather than left standing in contradiction.

## Semantic rules

- A complete statement evaluates before the next complete statement.
- A binary expression evaluates its left operand before its right operand.
- A unary expression evaluates its operand before applying the operator.
- A receiver is evaluated before a method's arguments.
- Call arguments evaluate left to right in written order, after the callee or
  receiver is evaluated.
- Array elements evaluate left to right in written order.
- Object and ADT member initializers evaluate left to right in written order;
  storage layout order does not alter evaluation order.
- A union initializer evaluates its source once before selecting or writing the
  active member.
- An assignment evaluates the target place, including its receiver and index,
  before evaluating the source value, then performs the write.
- A match scrutinee evaluates once before arm selection; the selected arm alone
  evaluates.
- `and` and `or` retain left-to-right short-circuit behavior.
- `try`, `spawn`, `defer`, and `errdefer` evaluate each source expression once
  according to their dedicated contracts; they do not create an alternate
  operand-order rule.
- Parentheses define evaluation grouping. An unparenthesized binary chain must
  use a single operator: `a + b + c` and `a * b * c` are valid, but mixing
  distinct operators or distinct precedence levels without parentheses is a
  Syntax Error, for example `a + b * c`, `a + b - c`, `a and b or c`, or
  `a < b == c`. The valid spellings are `(a + b) * c` or `a + (b * c)` and
  `(a and b) or c` or `a and (b or c)`. The same rule applies to shift
  (`<<`, `>>`), bitwise (`&`, `^`, `|`), relational, and equality operators.
  Unary and postfix operators bind tighter than binary operators and do not
  trigger this rule.

The order applies to calls through named functions, methods, function values,
and anonymous function literals. It also applies after generic specialization.
Parentheses required by the mixing rule are the only way to specify cross-
precedence order; operator precedence does not implicitly group mixed operators.

## Compiler obligations

- Constant folding is allowed only when it preserves values, traps, and
  observable effects.
- The checker and generator must not rely on map iteration order for source
  evaluation.
- Generated C must use temporaries or separate statements whenever a C
  expression could otherwise evaluate subexpressions in an unspecified order.
- A generated helper may reorder operations only when the reordered operations
  are provably free of observable effects and cannot change a defined trap.
- `#line` mappings remain attached to the source operation that evaluates.

## Diagnostics

- Mixing distinct binary operators or distinct precedence levels in one
  unparenthesized chain is a Syntax Error at the second distinct operator:
  `mixed operators without parentheses; use '(' and ')' to specify order`.
  The diagnostic points at the operator that introduces the second kind.
- A compiler internal failure to preserve order is an Unknown Error and must
  prevent output.

## Non-goals

- Parallel evaluation of independent expressions.
- Making data races safe. Concurrent conflicting access remains governed by the
  task, atomic, mutex, channel, and foreign-runtime contracts.
- Changing short-circuit behavior.
- Requiring every expression to allocate a temporary in generated C.

## Validation

This section is exhaustive. RFC 0111 is complete only when every item below
passes:

- Nested binary operands are evaluated left before right.
- Method receivers evaluate before arguments.
- Function arguments evaluate left to right for named, method, function-value,
  generic, and anonymous calls.
- Array, object, ADT, and union initializers evaluate left to right.
- Assignment targets evaluate before assignment sources.
- Match scrutinees evaluate once and only the selected arm evaluates.
- Short-circuit expressions do not evaluate skipped operands.
- `try` and `spawn` operands evaluate once in the specified position.
- `a + b + c` compiles and evaluates left to right; `a + b * c`, `a + b - c`,
  `a and b or c`, `a < b == c`, and `a << b >> c` are rejected with
  `mixed operators without parentheses` at the second distinct operator, while
  `(a + b) * c` and `a + (b * c)` compile with the parenthesized order.
- Repeated compilations produce identical generated files.
- Generated C text contains the required temporaries or statement boundaries
  for every source form whose direct C expression would be unordered.
- Existing arithmetic, bounds, conversion, allocation, and concurrency traps
  retain their specified order relative to surrounding expressions.
- `ref` places, `is` checks, and `View.from_pointer(p, len)` evaluate their
  operands left to right (`p` before `len`, target before type).
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes, replace the C23-unspecified evaluation rule in
`docs/reference.md` with this contract and update every affected generated-C
lowering rule.
