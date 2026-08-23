# RFC 0111: Deterministic Evaluation Order

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Draft; design proposed, implementation not started
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

The order applies to calls through named functions, methods, function values,
and anonymous function literals. It also applies after generic specialization.

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

No new source diagnostic is required for ordinary ordering. A compiler
internal failure to preserve order is an Unknown Error and must prevent output.

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
- Repeated compilations produce identical generated files.
- Generated C text contains the required temporaries or statement boundaries
  for every source form whose direct C expression would be unordered.
- Existing arithmetic, bounds, conversion, allocation, and concurrency traps
  retain their specified order relative to surrounding expressions.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes, replace the C23-unspecified evaluation rule in
`docs/reference.md` with this contract and update every affected generated-C
lowering rule.
