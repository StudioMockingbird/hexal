# RFC 0111: Deterministic Evaluation Order

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Closed; implemented 2026-08-24. Stage 1's ADT payload field-order
  bug is fixed and tested. Stage 2's object and ADT literal written-order
  evaluation is implemented, sharing the general sequencing mechanism rather
  than a standalone fix. Stage 3's shared `expressionMayObserve` predicate
  and `hoistSequenceSlots`/`hoistEvaluationOrderInStatement` hoisting
  mechanism land in `compiler/generator/sequencing.go`. Stage 4 wires the
  mechanism into call arguments, method receivers, binary operands, array
  elements, index expressions, and assignment targets exactly as planned,
  plus DeepEqualityExpression, StringCompareExpression, and
  UnionEqualityExpression (still binary expressions from the source
  language's point of view, discovered during the exhaustive Stage 6
  sweep), and ChannelConstructorExpression, AtomicMethodCallExpression's
  compare_exchange, and StreamMethodCallExpression's read (each verified
  and updated to consult the hoisted-value map before being included).
  UnionInjectionExpression was confirmed to need no change. Stage 5's
  cross-hoist, nested-innermost-first, and determinism cases are covered by
  permanent tests; the nested case required redesigning the hoist to
  interleave each sibling's own recursion with its hoist, one sibling at a
  time in written order, rather than recursing into every sibling before
  hoisting any of them, after a probe caught `a() + b() * c()` hoisting b()
  and c() ahead of a(). A second probe caught a receiver-hoisting bug where
  a pure receiver (e.g. a mutable array in `arr[idx()] = value()`) hoisted
  by value would address a throwaway copy instead of the original storage,
  breaking the write silently and in one case producing invalid C (writing
  through a pointer to a `const` temporary); the fix excludes a pure
  receiver from hoisting entirely, since it has no observable
  evaluation-order effect of its own, and a dedicated
  `forceHoistAssignmentTargetIndex` closes the remaining gap where an
  assignment's target index and source land in one C statement with no
  sibling of its own to trigger the ordinary group hoist. Stage 6's
  reference sync replaces the C23-unspecified evaluation rule in
  `docs/reference.md` with the deterministic contract, and the full
  Validation section is covered by tests in
  `compiler/tests/integration/evaluation_order_test.go` plus the Stage 1
  checker tests. Two narrow gaps are accepted and documented rather than
  closed: a sibling argument that mutates a receiver's storage through an
  explicit `ref` alias (rather than through a value it returns) can still
  observe the receiver before the mutation, since a pure receiver is never
  hoisted; and an array's own bounds-check trap can still race an
  independently effectful source in one assignment statement, since only
  the index (not the whole address computation) is hoisted ahead of the
  source. Both are exercised by no known snippet and would require hoisting
  a receiver's address into a pointer temporary rather than its value to
  close fully.
- Features: left-to-right expression, call, aggregate, and assignment
  evaluation
- Created: 2026-08-22
- Updated: 2026-08-24 — closed after implementation, an exhaustive
  validation sweep, and reference synchronization.
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
- Existing operator precedence and associativity remain unchanged; mixed
  operators are valid. For example, `a + b * c` parses as `a + (b * c)` and
  evaluates `a`, then `b`, then `c` before applying the operators.
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
- `try source` evaluates `source` once before propagating an `Error` or
  producing its value.
- `spawn callee(args)` evaluates the callee, then arguments left to right,
  before creating the task.
- `defer` and `errdefer` evaluate the registered callee and arguments at the
  registration statement, in ordinary call order; the registered call runs at
  its existing cleanup point.

The order applies to calls through named functions, methods, function values,
and anonymous function literals. It also applies after generic specialization.

## Compiler obligations

- Constant folding is allowed only when it preserves values, traps, and
  observable effects. Observable effects are exactly: a defined trap
  (out-of-bounds index, empty pop, missing Dict key, zero divisor, bad shift
  count, float overflow, allocation failure, malformed UTF-8, close failure,
  Mutex misuse, task stack overflow, and the other trap rules in
  `reference.md`), `print` output, `Heap.allocate`/`free` and every
  heap-backed container or String allocation that routes through it, `spawn`
  task creation, and a write to a `mut` place or binding (assignment or
  `ref` producing `MutPtr`). Pure value computation with none of these is
  not observable and may be reordered or folded. Folding `10 / 0` remains the
  existing compile-time zero-divisor diagnostic, not an eliminated trap, and
  `sideEffect() + 1 + 2` must not fold `1 + 2` before `sideEffect()` when
  `sideEffect()` is observable.
- Member, index, and `ref` place evaluation follows receiver/index before
  load: `obj.member`, `arr[idx()]`, and `ref place` evaluate the receiver or
  index (including any calls inside it) before the load or address-taking.
  `spawn getFunc()(a(), b())` evaluates `getFunc()` as the callee, then `a()`
  then `b()`, before task creation.
- The checker and generator must not rely on map iteration order for source
  evaluation.
- Generated C must use temporaries or separate statements whenever a C
  expression could otherwise evaluate subexpressions in an unspecified order.
- A generated helper may reorder operations only when the reordered operations
  are provably free of observable effects and cannot change a defined trap.
- `#line` mappings remain attached to the source operation that evaluates.

## Diagnostics

No new source diagnostic is required for ordinary ordering or for mixed
operators. A compiler internal failure to preserve order is an Unknown Error
and must prevent output.

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
- Mixed operators preserve existing precedence and associativity, while their
  operands still evaluate left to right; explicit grouping preserves its
  existing meaning.
- Method receivers evaluate before arguments.
- Function arguments evaluate left to right for named, method, function-value,
  generic, and anonymous calls.
- Array, object, ADT, and union initializers evaluate left to right.
- Assignment targets evaluate before assignment sources.
- Match scrutinees evaluate once and only the selected arm evaluates.
- Short-circuit expressions do not evaluate skipped operands.
- `try` evaluates its source once before propagation or value production;
  `spawn` evaluates its callee and arguments left to right before task
  creation; `defer` and `errdefer` evaluate registration operands immediately.
- Repeated compilations produce identical generated files.
- Generated C text contains the required temporaries or statement boundaries
  for every source form whose direct C expression would be unordered.
- Existing arithmetic, bounds, conversion, allocation, and concurrency traps
  retain their specified order relative to surrounding expressions.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Implementation plan

### Baseline findings

Probed against the generator at HEAD before writing this plan, because the
scope was not obvious from the spec text alone:

- **A correctness bug, not just an ordering gap.** An ADT payload variant
  constructor renders its fields by positional index
  (`for index, member := range variant.Payload { ...node.Arguments[index]... }`
  in `renderAdtConstruct`), but the checker stores `Arguments` in *written*
  order (`buildVariantConstructor` appends in the order `*expression.Payload`
  iterates, with no reordering to declaration order). Writing payload fields
  out of declaration order therefore assigns the wrong value to the wrong
  field. Verified: `type W = | A as { first: Int32, second: Int32 }`
  constructed as `W.A { second = 20, first = 10 }` generates
  `.hex_m_first = 20, .hex_m_second = 10` — swapped. Reproduces identically
  for a generic ADT. This is silent miscompilation, independent of C's
  evaluation-order looseness, and is fixed first.
- **Object literals get the value right but the order wrong.** `objectLiteralWithState`
  matches values to fields by member pointer (`byMember`), so the *value* is
  never misassigned, but it iterates `value.Type.Object.Members` (declaration
  order) to render, not `value.Initializers` (written order, already carried
  by the checked tree for exactly this reason per its own doc comment).
  Verified: `Foo { b = sideB(), a = sideA() }` emits `sideA()` before
  `sideB()` in the generated C, the reverse of source order.
- **C's unspecified evaluation order is fully unaddressed.** Verified for four
  representative shapes — call arguments (`f(a(), b())`), binary operands
  (`a() + b()`), array literal elements (`[a(), b()]`), and assignment
  target-vs-source (`arr[idx()] = source()`, where C sequences the *write*
  after both operands but not the operands relative to each other) — each
  lowers straight into one C expression with more than one potentially
  effectful sub-expression and no temporary boundary. Nothing in the
  generator computes or enforces order for these today; the current output
  order is whatever registers GCC/Clang happens to pick, not a language
  guarantee.
- `try`, `spawn`, and `Dict.find` already have dedicated per-statement
  hoisting (`hoistTryInStatement`, `hoistConcurrencyInStatement`,
  `hoistDictFindInStatement` in `errors.go`, `concurrency.go`, `find.go`),
  run in that fixed order ahead of statement rendering, each replacing its
  own node via a `state.hoisted*` map keyed by the checked node's own
  pointer. This is the mechanism stage 2 below generalizes; it is not
  touched or re-ordered by this plan.

### Stage 1 — ADT payload field-order correctness fix

Fix the bug on its own, ahead of and independent of the sequencing work
below, because it is a wrong-program bug rather than an ordering nicety.

1. In `compiler/checker/adt.go`, change `buildVariantConstructor` to build
   `Arguments` in declaration order (indexed by position in
   `variant.Payload`, written value looked up by field name — the same
   shape `variantField` already resolves), not accumulation order. The
   checked tree then carries positionally-correct arguments the way
   `renderAdtConstruct` already assumes.
2. Add a `checker.AdtConstructExpression`-adjacent record of the *written*
   evaluation order for stage 4 (a parallel slice or an evaluation-order
   index per argument), so stage 4 can still sequence side effects in
   written order even though `Arguments` itself is now declaration-ordered.
3. Add a checker test proving the checked tree assigns each field the value
   its own name was initialized with, regardless of written order, for a
   plain and a generic ADT.

Gate: the reproduction case above generates `.hex_m_first = 10,
.hex_m_second = 20`; full suite green; no other generated artifact changes
(this is a pure bugfix, not a rendering-shape change).

### Stage 2 — written-order evaluation for object and ADT literals

1. In `compiler/generator/render.go` (`objectLiteralWithState`), evaluate
   each initializer into a named temporary in `value.Initializers` order
   (written order, already available), then assemble the designated
   initializer list in `value.Type.Object.Members` order using those
   temporaries. Skip the temporary for an initializer with no observable
   effect (stage 3's predicate) to avoid needless churn on the common case.
2. Apply the same shape to `renderAdtConstruct` for payload fields, using
   the written-order record from stage 1.
3. Add generated-C text tests: an object and an ADT literal with fields
   written out of declaration order, each asserting the temporaries appear
   in written order and the final compound literal assigns each field from
   the correct temporary.

Gate: full suite green; snippet manifest checked for movement (expected: none,
since no existing snippet writes aggregate fields out of order or with more
than one effectful initializer).

### Stage 3 — shared effect predicate and sequencing-hoist mechanism

One mechanism, reused by every remaining construct, modeled directly on the
existing `hoistTry`/`hoistSpawn`/`hoistDictFind` shape (per-statement walk,
prologue emitted before the statement, node replaced via a
`state.hoisted*` map keyed by the node's own pointer).

1. Add `expressionMayObserve(node *checker.Expression) bool` in a new
   `compiler/generator/sequencing.go`: true for any call-shaped kind
   (`CallExpression`, `MethodCallExpression`, `SpawnExpression`,
   `TaskMethodCallExpression`, `TaskYieldExpression`,
   `ChannelMethodCallExpression`, `MutexMethodCallExpression`,
   `AtomicMethodCallExpression`, `HeapAllocateExpression`,
   `HeapFreeExpression`, `StreamMethodCallExpression`,
   `ListNewExpression`, `DictNewExpression`, `StringFromBytesExpression`,
   `StringFromRunesExpression`, `VolatileReadExpression`,
   `VolatileWriteExpression`, `TryExpression`), or already present in
   `state.hoistedTries`/`state.hoistedSpawns`-equivalent/
   `state.hoistedDictFinds` (already hoisted elsewhere — treat as an
   effect boundary, not as pure, so sequencing relative to it is still
   enforced); recurses through `Operand`, `Left`, `Right`, and `Arguments`
   otherwise. Every other kind (variable, member, constant, nil/eos,
   address-of, dereference, unary/binary *shape* itself — its operands are
   checked recursively, not the operator) is a leaf for this predicate.
2. Add `hoistOperandSequence(operands []checker.Operand, body *strings.Builder, state *expressionValidation, indent string) error`:
   if fewer than two operands, or none satisfies `expressionMayObserve`,
   do nothing (no temporaries for the common pure case). Otherwise emit
   every operand — including ones that look pure, per the RFC's own
   aliasing concern (`f(mutateX(), x)` must not read `x` after
   `mutateX()` if source order says otherwise) — as one `const <Type>
   hex_seq_<n> = <rendered>;` per operand, strictly in written
   (left-to-right) order, and register `state.hoistedSequencing[&operands[i].Node] = "hex_seq_<n>"`.
3. Add the lookup: wherever an operand is about to be rendered, check
   `state.hoistedSequencing[&operand.Node]` first (mirroring
   `renderTryExpression`'s lookup) and use the temporary name directly
   when present.

Gate: the new file compiles standalone against the existing `checker`/
`expressionValidation` types; no caller wired yet, so no behavior change and
the full suite stays green.

### Stage 4 — wire the mechanism into every construct RFC 0111 names

Call `hoistOperandSequence` (or the binary/index/assignment-specific two-
operand form) from the per-statement hoisting sequence
(`compiler/generator/render.go`'s statement loop, alongside the existing
three hoist calls, running after them so an already-hoisted try/spawn/
dict-find node is seen as a resolved leaf by stage 3's predicate), for:

1. **Call arguments** — `CallExpression` and `MethodCallExpression` in
   `renderExpressionUncheckedWithState`: hoist `node.Arguments` (method
   calls: receiver is evaluated before arguments per the existing rule and
   is not itself reordered relative to them, so hoist the receiver first if
   it may observe, then the argument list).
2. **Binary operator operands** — `BinaryOperationExpression`: hoist the
   pair `[Left, Right]` together so `Left` always precedes `Right`.
3. **Array literal elements** — `ArrayLiteralExpression`: hoist
   `node.Arguments` (the element list).
4. **Index expressions** — `IndexExpression` in `renderCollectionExpression`:
   hoist `[receiver-as-operand, node.Arguments[0]]` together, receiver
   first, matching `arr[idx()]`.
5. **Assignment** — in the statement renderer: hoist `[Target, Source]`
   together, target first, so `arr[idx()] = source()` sequences `idx()`
   before `source()` regardless of C's own assignment-operand looseness.
6. **Union injection source** — `UnionInjectionExpression`: single operand,
   no sibling to order against; confirm by reading the render path that
   this is already inherently ordered and needs no change, rather than
   assuming it.
7. Leave `spawn` argument ordering, `try`, and `Dict.find` untouched: probed
   working in RFC 0094's Stage 6 work and unrelated to this plan's gap.

Each item lands as its own reviewable change with a before/after generated-C
text test, per the file it touches, so a mistake in one construct doesn't
block the others.

Gate per item: the construct's own reproduction case (two effectful
siblings) now emits ordered temporaries; a construct with zero or one
effectful sibling emits unchanged, temporary-free C (verified against the
existing snippet manifest, which must not move for any snippet that never
exercises two effectful siblings in one position); full suite green after
each item.

### Stage 5 — cross-hoist interaction and nesting

1. Verify a statement containing both a `try`/`spawn`/`Dict.find` node and
   an unrelated multi-operand call sequences correctly: the existing hoists
   still run first (unchanged order: concurrency, then try, then dict-find),
   stage 3's predicate treats their resolved nodes as effect boundaries
   rather than recursing into them a second time, and stage 4's hoisting
   only concerns itself with the *remaining*, not-yet-hoisted operands.
2. Verify nested compound expressions hoist innermost-first:
   `f(g(a(), b()), c())` must emit temporaries for `a()` and `b()` (from
   `g`'s own argument list) before the temporary for `g(...)` itself, which
   in turn precedes the temporary for `c()`, before the outer call to `f`.
   Add a generated-C text test for this exact shape.
3. Add a determinism test: compile a program exercising every hoisted
   construct twice and assert byte-identical output.

Gate: both probes above pass as permanent tests; full suite green.

### Stage 6 — exhaustive validation and reference sync

1. Work through this RFC's own Validation section line by line, adding the
   one generated-C-text or checked-tree test each bullet names that stage
   1-5 did not already cover incidentally.
2. Rebuild the snippet manifest via the repository-prescribed temporary-test
   procedure only if any existing snippet's output moved; review and record
   what moved and why before accepting it.
3. Update `docs/reference.md` per the existing Reference synchronization
   section below.
4. Run `go test ./...`, `go vet ./...`, `go vet -tags c23
   ./compiler/tests/c23validation`, `gofmt -l` empty.

Gate: every Validation bullet has a named, passing test; full repository
gate green; RFC status updated to Implemented and archived.

## Reference synchronization

After implementation stabilizes, replace the C23-unspecified evaluation rule in
`docs/reference.md` with this contract and update every affected generated-C
lowering rule. Keep the existing operator precedence, associativity, and
short-circuit rules; mixed operators are not a rejection category.
