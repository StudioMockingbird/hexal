# RFC 0049: Compiler Conformance and Cleanup Batch

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-13
- Features: reject `Rune` binary arithmetic, classify root-scope `errdefer`,
  admit full match expressions at arm boundaries, admit mixed member/index
  `ref` places, reject raw newlines in String literals, make `Size` fully
  C-target-driven, consolidate generator traversal, and implement RFC 0050
  compiler conformance
- Created: 2026-08-13
- Depends on: RFC 0018 and RFC 0044 (`Rune`), RFC 0022 (match), RFC 0029
  (`errdefer`), RFC 0033 (`ref` places), RFC 0036 (`Size`)
- Coordinates with: `docs/reference.md`, RFC 0048, RFC 0050, and RFC 0052

## Readiness verdict

All eight items are implementation-ready. The remaining gate is implementation
approval, not a semantic or technical decision.

| # | Item | Kind | Owner | Ready |
|---:|---|---|---|---|
| 1 | Reject `Rune` binary arithmetic | defect | checker | yes |
| 2 | Classify root-scope `errdefer` | defect | checker | yes |
| 3 | Admit `and`/`or` in match positions | defect | parser | yes |
| 4 | Admit index-then-member `ref` places | defect | parser | yes |
| 5 | Reject raw newlines in String literals | defect | lexer | yes |
| 6 | Make `Size` fully C-target-driven | semantic correction | types/checker/generator/reference | yes |
| 7 | Consolidate generator traversal | refactor | generator | yes |
| 8 | Implement RFC 0050 compiler conformance | conformance | parser/checker/generator/tests | yes |

Items 1-5 and 8 make the compiler match `docs/reference.md`. Item 6 corrects
the reference and compiler so C `size_t`, not a duplicate Hexal profile,
remains authoritative. Item 7 must preserve behavior exactly.

## 1. Reject `Rune` binary arithmetic

### Contract

- `Rune` accepts `==`, `!=`, ordering, and checked `to<T>()` conversion.
- Either operand being `Rune` makes `+`, `-`, `*`, `/`, or `%` invalid.
- Existing rejection of Rune by unary `-`, bitwise, and shift operators remains.
- No implicit Rune/integer conversion is introduced.

```hexal
'a' < 'b'                 // valid
'a'.to<UInt32>() + 1      // valid
'a' + 'b'                 // Type Error
'a' + 1                   // Type Error
```

### Implementation

- In `checker/checker.go`, arithmetic eligibility must exclude Rune before
  constant folding or lossless-common-type selection.
- Apply the check after both operands are resolved so one deterministic
  diagnostic owns mixed and same-type cases.
- Do not rely only on `operatorAllowsType`: mixed arithmetic has a separate
  common-type path and must reject Rune there too.

Required message shape:

```text
[Type Error] operator + requires numeric operands; got Rune and Rune
```

Substitute the written operator and resolved operand names. Report one
diagnostic at the operator.

### Tests

In `compiler/operators_test.go`:

- reject `Rune` with every binary arithmetic operator;
- reject mixed Rune/fixed-integer arithmetic in both operand orders;
- retain Rune equality, ordering, and explicit conversion;
- cover a constant Rune and a Rune binding so folding cannot bypass the rule.

No reference edit is required: RFC 0050 already moved the complete Rune rule
into `docs/reference.md`.

## 2. Classify root-scope `errdefer`

### Contract

- The grammar admits `errdefer` as a statement at root.
- Semantics reject it unless the enclosing function result accepts `Error`.
- A root use is a user Type Error, never `Unknown Error`.

```hexal
errdefer cleanup()         // Type Error at root

fun run(): Int32 | Error
    errdefer cleanup()     // valid
    return 1
end
```

### Implementation

- The parser already produces `parser.ErrdeferStatement`; no parser change is
  required.
- Add an explicit `parser.ErrdeferStatement` arm to the root switch in
  `checker.Check`.
- Route that arm through `checkErrdeferStatement`. Its existing enclosing-
  function check owns the diagnostic.
- Do not append an invalid root cleanup to checked statements or defers.

Required diagnostic:

```text
[Type Error] errdefer requires an enclosing function whose result accepts Error
```

### Tests

In `compiler/error_test.go`:

- root `errdefer` reports the required Type Error at the keyword;
- the result contains no `Unknown Error`;
- valid function-scope `errdefer` remains accepted;
- root `defer` retains its existing behavior.

The old claim that `errdefer` failed in the parser was stale. The failure is
the checker's missing root dispatch arm.

## 3. Admit full expressions in match positions

### Contract

- Match scrutinees and arm results accept the ordinary expression grammar.
- At match-expression depth zero, an unparenthesized `|` begins the next arm.
- At scrutinee depth zero, an unparenthesized `is` selects type mode.
- Grouping removes those boundary meanings inside the parentheses.

```hexal
match ready and enabled
| true then left or right
| false then false
end

match (mask | flag)
| 1 then true
| else then false
end
```

`match mask | flag ...` treats the first unparenthesized `|` as the first arm,
not as bitwise-or. `match (value is Int32)` is a value-mode scrutinee; `match
value is | Int32 ...` is type mode.

### Implementation

- Replace the current `relationalExpression` scrutinee and
  `bitwiseXorExpression` arm-result entry points with the full `orExpression`
  precedence chain under a match-boundary context.
- The context stops the top-level chain before:
  - `is` or `|` while parsing the scrutinee;
  - `|` or the matching `end` while parsing an arm result.
- Parenthesized expressions invoke the ordinary expression parser with the
  boundary suspended. Nested match expressions own their own boundaries.
- Do not identify an arm separator by speculative pattern parsing. The
  reference defines every unparenthesized depth-zero `|` as a separator.
- Preserve the existing one-scrutinee evaluation and arm-order semantics.

### Tests

In `compiler/adt_test.go` and focused parser tests:

- accept `and` and `or` in scrutinees and arm results;
- accept comparison and type-test expressions where the boundary permits them;
- accept parenthesized bitwise-or in both positions;
- retain the unparenthesized `|` arm boundary;
- distinguish grouped value-mode `is` from the ungrouped type-mode marker;
- cover a nested match so the inner boundary does not terminate the outer arm.

No grammar or reference edit is required.

## 4. Admit mixed member/index `ref` places

### Contract

- A place is an addressable root followed by any ordered sequence of member and
  index suffixes.
- Capability is derived from the complete place:
  - a writable place yields `MutPtr<T>`;
  - an addressable non-writable place yields `Ptr<T>`.
- Existing member mutability, collection writability, addressability, bounds,
  and `ref` eligibility rules remain authoritative.

```hexal
ref pair.values[0]         // member, then index
ref rows[0].field          // index, then member
ref grid[0].cells[1].value // arbitrary mixed chain
```

### Implementation

- Change `parser.place` from two consecutive loops (all members, then all
  indexes) to one loop dispatching on either `.` or `[` until neither follows.
- Keep index expressions on the ordinary expression parser and require the
  closing `]` before continuing the place chain.
- No checker redesign is required: `checkPlace` already recursively handles
  `PropertyExpression` and `IndexExpression` in either order.
- Do not add pointer indexing or pointer arithmetic. Only collection indexing
  already accepted by `checkIndexPlace` is in scope.

### Tests

In parser tests and `compiler/pointers_test.go`:

- accept member-index, index-member, and a chain containing both twice;
- prove fixed and writable roots produce the correct pointer capability;
- prove member mutability can downgrade the final place to `Ptr`;
- retain index bounds diagnostics and rejection of non-addressable roots;
- retain rejection of call syntax after `ref`.

No grammar or reference edit is required.

## 5. Reject raw newlines in String literals

### Contract

- A raw LF, CR, or CRLF inside a String literal is invalid.
- `\n` and `\r` escapes remain valid.
- A backslash immediately followed by a physical newline is invalid; Hexal has
  no line-continuation escape.
- One malformed literal produces one lexer-owned Syntax Error and no
  `StringLiteral` token.

```hexal
text: String = "a\nb"      // valid escaped LF
text: String = "a
b"                    // Syntax Error
```

### Implementation

- In the lexer String scanner, detect CR/LF before generic escape skipping.
- Record the newline's own line and column for the diagnostic.
- Consume the invalid literal through its closing quote, or EOF, solely for
  recovery; do not emit a token for it.
- Treat CRLF as one source newline and one diagnostic.
- Preserve subsequent token positions and avoid a second "unterminated String"
  diagnostic for the same literal.

Required diagnostic:

```text
[Syntax Error] String literal cannot contain a raw newline; use \n
```

### Tests

In `compiler/lexer/lexer_test.go` and `compiler/syntax_test.go`:

- reject LF, CRLF, and bare CR;
- reject backslash followed by a physical newline;
- accept escaped `\n` and `\r`;
- verify a later valid declaration retains the correct source line;
- verify exactly one diagnostic for a closed multiline literal.

No grammar or reference edit is required.

## 6. Make `Size` fully C-target-driven

### Decision

Hexal does not select or duplicate the width of `size_t`.

- `Size` always lowers directly to C `size_t`.
- The selected C compiler and its target determine width, range, alignment,
  and representation.
- Hexal has no `SizeBits`, target-width option, target-width type identity, or
  width assertion.
- Generated C does not reject a target merely because `sizeof(size_t)` is 2,
  4, 8, or another conforming value.
- `Size` remains canonically distinct from fixed-width integers.

The previous reference rule naming supported widths 16/32/64 and requiring a
chosen-width assertion is removed. This is an intentional reference correction
and the sole reference edit in RFC 0049.

### Portable implicit conversions

Hexal checking must produce the same result without knowing the C target.
Therefore an implicit conversion involving `Size` is valid only when it is
lossless on every conforming target supported by Hexal.

- No fixed-width integer or float implicitly converts to `Size`.
- `Size` does not implicitly convert to any fixed-width integer or float.
- `Size` with any distinct numeric type has no implicit binary common type.
- Identity `Size -> Size` remains implicit.
- An untyped non-negative integer literal may be contextually typed as Size.
  This is literal typing, not an implicit fixed-width conversion. A value whose
  fit depends on the C target emits a C `static_assert(value <= SIZE_MAX, ...)`.
  Negative literals remain invalid in unsigned context.
- Explicit `value.to<Size>()` and `size.to<T>()` are the portable routes and
  preserve the checked-conversion contract: target-independent failures are
  diagnosed by the Hexal checker; target-dependent constants are guarded by a
  generated C `static_assert`; dynamic out-of-range values trap before casting.

```hexal
count: Size = values.length()
wide: UInt64 = count.to<UInt64>()    // explicit and checked
count = wide.to<Size>()
total: Size = count + offset         // invalid when offset is UInt32
total: Size = count + offset.to<Size>()
```

This deliberately gives up target-dependent implicit conveniences so the C
compiler can remain the sole target authority.

### Width-sensitive operations

- Size arithmetic, bitwise operations, shifts, wrapping, and comparisons emit
  ordinary `size_t` operations.
- The checker must not fold a Size operation whose result depends on
  `SIZE_WIDTH`, `SIZE_MAX`, or C conversion behavior.
- A known Size constant may still be folded when the mathematical result is
  identical on every admitted target and remains within the source operands'
  proven portable range.
- A constant Size shift count whose validity depends on the target emits a C
  `static_assert(count < sizeof(size_t) * CHAR_BIT, ...)`; a count proven
  invalid for every C target fails in the Hexal checker.
- Dynamic Size shift counts use `sizeof(size_t) * CHAR_BIT` in emitted checks.
- Explicit conversion to Size compares against `SIZE_MAX` before casting.
- Explicit conversion from Size uses the destination limit before casting.
- `size_of<T>()`, `align_of<T>()`, collection lengths/indices, and Error source
  positions remain `Size`/`size_t` without compiler-side width knowledge.

### Implementation

- Remove the hardcoded `Bits: 64` meaning from Size. `Type.Bits` must not be
  consulted for Size; use `IsSize` branches before fixed-width logic.
- Remove the unconditional `UInt64 -> Size` widening edge.
- Implement the portable conversion/common-type rules above in
  `types/widening.go`, conversion checking, constant folding, and operator
  checking.
- Remove `static_assert(sizeof(size_t) == 8, ...)`. Emit no assertion restricting
  the target's Size width.
- Include the standard headers defining `SIZE_MAX` and fixed-width limits where
  checked conversion guards require them.
- Do not add a target profile or change `Compile(source)`.

### Tests

In type, conversion, operator, layout, and generator tests:

- reject every distinct Size/fixed-width implicit assignment and binary pair;
- accept contextual non-negative Size literals and assert target-dependent
  literal fit against `SIZE_MAX`;
- retain explicit conversions in both directions;
- assert target-dependent narrowing emits a `SIZE_MAX` guard;
- assert target-dependent constant Size shifts are checked by generated C;
- assert Size shift validation uses `sizeof(size_t) * CHAR_BIT`;
- assert generated C contains no fixed `sizeof(size_t) == N` assumption;
- assert generated C contains no fixed width or Size-versus-UInt64
  representation restriction;
- compile representative Size output with the retained C23 toolchain tests.

RFC 0052 no longer owns Size width selection. It may still address OS/ABI
feature selection and representation evidence for features that genuinely
need compiler-side target knowledge.

## 7. Consolidate generator traversal

### Problem

Fifteen generator files duplicate traversal of module statements, specialized
functions, specialized methods, nested statements, operands, expressions, and
reachable types. The copies already differ in covered statement and expression
kinds. A new checked node can therefore be silently missed by one collector.

### Contract

Add one fail-closed traversal in `compiler/generator/walk.go`:

```go
type programVisitor struct {
    Type       func(compilerTypes.Type) error
    Expression func(checker.Expression) error
}

func walkProgram(program checker.Program, visitor programVisitor) error
```

- Traversal order is deterministic and pre-order.
- Nil callbacks are ignored.
- `walkProgram` owns structural traversal; visitors inspect nodes but do not
  recurse through statements, operands, or expression children.
- A visitor may recursively inspect the internals of a delivered type when its
  collector needs type-graph dependency order. The shared walker owns only the
  program/tree traversal, not collector-specific type-graph ordering.
- Any unknown checked statement kind or structurally invalid child returns a
  structured generator `Unknown Error`; it is never skipped.

### Required coverage

The walker visits:

- every `program.TypeDeclarations[].Type`;
- every module statement;
- every specialized function's Fun type, parameter types, result type, and
  body;
- every specialized method's self type, parameter types, result type, and body;
- every nested `if` branch, loop body, return, call, assignment, declaration,
  `defer`, and `errdefer` expression;
- every operand type and expression;
- every expression's operand/result/test/element types, object value,
  constant operand, unary/binary children, and argument operands.

The traversal must explicitly handle all current `checker.Statement` and
checked expression child shapes. Adding a new shape requires updating this one
walker before generation can succeed.

### Migration

- Migrate the named collectors in `adt.go`, `alloc.go`, `arrays.go`,
  `bitwise.go`, `concurrency.go`, `conversions.go`, `dicts.go`, `division.go`,
  `equality.go`, `io.go`, `lists.go`, `print.go`, `streams.go`, `strings.go`,
  and `views.go`.
- Preserve each collector's discovery criteria, deduplication keys, dependency
  order, and final C-name sorting.
- Remove only traversal duplication. Do not merge collectors or change emitted
  C.
- Migrate one collector family at a time and run its existing tests before the
  next migration.

### Tests and sequencing

- Add focused walker unit tests using a checked program containing module code,
  a specialized function, a specialized method, nested control flow, an object
  initializer, a binary expression, `defer`, and `errdefer`.
- Assert visit coverage and deterministic order; do not snapshot internal
  pointer identities.
- Existing generator and integration outputs must remain unchanged.
- Run the existing C23 toolchain tests after the refactor.
- Item 7 must land before RFC 0048 deletes those tests. If RFC 0048 has already
  landed, temporarily restore equivalent C23 compile coverage for this refactor
  and remove it afterward.

## 8. Implement RFC 0050 compiler conformance

RFC 0050 changed the normative reference only. This item makes the compiler,
checked representation, generator, and tests conform to every executable rule
introduced or tightened there. Pure documentation relocation and wording
changes require no compiler work.

### 8.1 Restrict Nil to union membership

#### Contract

- Written `Nil` is valid only as one member of a union containing at least one
  distinct non-Nil member.
- It is invalid as a standalone alias target, binding, parameter, result,
  object member, ADT payload, collection type argument, Task result, Channel
  element, Stream element/state, pointer pointee, Heap allocation type, or any
  other generic argument.
- `nil` requires an expected union containing Nil. The only standalone literal
  exception is a direct `print` argument.
- Flow narrowing to Nil does not make Nil a declarable type. The narrowed value
  may be tested or printed directly but cannot initialize `x: Nil`.
- A union must still contain at least two distinct canonical members. Repeated
  Nil does not satisfy that requirement, and neither does any other repeated
  member. Flattening and duplicate removal run first; a written union that
  yields fewer than two distinct members is a Type Error, never an alias for the
  surviving member. Alias resolution and generic substitution precede the count,
  so a duplicate reached through an alias or a type argument is rejected the
  same way.

```hexal
type U = Int32 | Int32         // Type Error: collapses to one member
type A = Int32
type V = A | Int32             // Type Error: same after alias resolution
```

The compiler currently accepts both, silently treating the result as a plain
`Int32` alias. Required message shape:

```text
[Type Error] a union requires at least two distinct members; Int32 | Int32 has one
```

```hexal
value: Int32 | Nil = nil       // valid
value: Nil = nil               // Type Error
type Empty = Nil               // Type Error
pointer: Ptr<Nil> = nil        // Type Error
print(nil)                     // valid sole exception
```

#### Implementation

- Make type-position validation reject canonical Nil by default.
- Union construction is the sole type resolver path that may admit Nil, and
  only while forming a valid multi-member union.
- Validate after alias and generic substitution so aliases cannot bypass the
  rule.
- The Nil literal checker accepts standalone Nil only under an explicit
  `allowStandaloneNil` context set by `checkPrintCall`; do not infer the
  exception from the eventual type.
- Existing internal Nil union tags remain unchanged. This restriction concerns
  source-level standalone values, not the representation of a Nil union member.

Required message shapes:

```text
[Type Error] Nil is valid only as a member of a union with a non-Nil type
[Type Error] nil requires an expected union containing Nil
```

Use the first for written type positions and the second for a literal without
the required context.

#### Tests

- Reject standalone Nil in every named position above, including through an
  alias and generic substitution.
- Accept Nil in pointer, Fun, handle, scalar, aggregate, and multi-member
  unions.
- Accept `print(nil)` and printing a union narrowed to Nil.
- Reject a standalone Nil binding in a Nil-narrowed branch.
- Reject a union collapsing to one member: `Int32 | Int32`, `Nil | Nil`, the
  same duplicate reached through a transparent alias, and the same reached
  through generic substitution. Confirm the diagnostic names the collapse
  rather than reporting an unrelated failure.
- Accept `Int32 | Float64` and other genuinely distinct two-member unions, and
  accept a union written with a redundant member alongside two distinct ones so
  deduplication itself still succeeds.
- Preserve nullable layout, comparison, narrowing, and union injection tests.

### 8.2 Normalize no-value commands

#### Contract

These operations produce no Hexal value:

```text
Task<R>.detach()
Task.yield()
Channel<T>.close()
Channel<T>.free(heap)
Mutex.lock()
Mutex.unlock()
Mutex.free(heap)
Atomic<T>.store(value)
MutPtr<T>.write_volatile(value)
```

- They are valid as call statements and direct `defer`/`errdefer` actions.
- They are invalid as initializers, operands, arguments, object fields, return
  values, or any other value position.
- Fallible no-payload commands remain `Nil | Error`: Channel `send`, File
  `write`, `write_text`, and `flush` are unchanged.
- Existing no-value List, Dict, String, Stream, File, and Heap commands remain
  no-value.

```hexal
mutex.lock()                    // valid
result: Nil = mutex.lock()      // Type Error: lock produces no value
```

#### Implementation

- Represent no result with the zero `compilerTypes.Type`, exactly like a
  user function with no result. Do not use canonical Nil as an internal unit
  substitute.
- Set both the checked expression's `typ` and node `ResultType` to zero for all
  listed operations.
- Generator validation and emission must accept these nodes only in statement
  or cleanup contexts and must emit no fabricated value.
- Reuse the existing `"<callee> produces no value"` diagnostic path for value
  contexts.
- Update checked-expression comments that currently describe these operations
  as yielding Nil.

#### Tests

- For every listed command, accept statement and direct-defer use and reject an
  initializer.
- Inspect checked nodes to prove zero result type rather than Nil.
- Assert emitted C remains the same operation statement with no dummy result.
- Retain `Nil | Error` for every fallible no-payload command.

### 8.3 Add `try` statements

#### Contract

- `try <unary-expression>` is a statement as well as an expression.
- A try statement uses the same operand eligibility and unchanged Error
  propagation as a try expression, then discards the normalized success value.
- It is valid only inside a function whose declared result accepts Error and
  outside `defer`/`errdefer` actions.
- It does not admit any other arbitrary value expression as a statement.

```hexal
fun flush(file: File): Nil | Error
    try file.flush()
    return nil
end
```

#### Implementation

- Add `parser.TryStatement` and parse it in `parser.statement` before the
  identifier-led statement path. Its operand uses the same unary-expression
  boundary as prefix `try`.
- Add a distinct checked `TryStatement`; do not mislabel it as a call statement
  or introduce a general expression statement.
- Reuse `checkTryExpression` for validation and propagation metadata, then mark
  its success result as discarded in the statement node.
- Generator statement lowering must execute the existing try propagation and
  ignore the success value without declaring a temporary solely for that value.
- Shared generator traversal from item 7 must visit the statement operand.

#### Tests

- Accept `try file.flush()` in a compatible function.
- Accept try statements over both Nil-success and payload-success unions.
- Reject root and cleanup-action uses with the existing scope diagnostics.
- Reject operands without exactly one Error member or without a success member.
- Preserve try-expression behavior and reject a bare non-call value statement.

### 8.4 Enforce position eligibility composition

#### Contract

Every concrete type is checked in its named position after alias resolution and
generic substitution. Completeness, finite size, copyability, and local
feature exclusions all apply; a broad local check must not bypass the shared
position model.

Required compiler coverage includes:

- function parameters use `FunctionParam`;
- function results use `FunctionResult`;
- spawn arguments use `TaskArgument` in addition to their parameter position;
- spawned R is valid in both `FunctionResult` and `TaskResult`;
- Stream element/state, Channel element, collection element/value, union
  member, ADT payload, object member, Pointee, and HeapAllocation use their
  matching positions.

`Fun`, Nil, Unknown, and Atomic restrictions follow the reference's shared
model. Open generic declarations defer eligibility that depends on a type
parameter; every concrete specialization rechecks it.

#### Implementation

- Extend the position registry with `Pointee` and `HeapAllocation`.
- Centralize position validation so function signatures and spawn do not use
  weaker ad hoc completeness checks.
- Preserve Pointee's explicit Unknown exception.
- Keep View placement separate from provenance/lifetime checks; do not add a
  storage ban for View.
- Ensure diagnostic tokens identify the written type argument or annotation,
  not an internal specialization site where source information exists.

#### Tests

- Build one eligibility matrix over every position and the exceptional types
  Fun, Nil, Unknown, View, Atomic, and an aggregate containing Atomic.
- Test direct declarations and generic specializations.
- Retain valid View placements while preserving temporary-root, `ref`, and
  return-lifetime rejection.

### 8.5 Enforce Atomic placement and direct-pointee rules

#### Contract

- Direct `Atomic<T>` and every inline aggregate containing Atomic are valid for
  in-place construction only as bindings or object members.
- `Ptr<Atomic<T>>` and `MutPtr<Atomic<T>>` are invalid type expressions,
  including through transparent aliases and generic substitution.
- `ref atomicValue` and `ref object.atomicMember` are independently invalid.
- `Heap.allocate<Atomic<T>>` and allocation of any Atomic-containing value are
  invalid because Heap allocation copies an initializer.
- `Ptr<EnclosingObject>` remains valid when the enclosing object contains
  Atomic; containment stops at pointer and handle indirection.

```hexal
counter: Atomic<Int32> = Atomic<Int32>.new(0) // valid binding
pointer: Ptr<Atomic<Int32>>                    // invalid type
pointer: Ptr<CounterObject>                    // valid enclosing object
```

#### Implementation

- Reject a direct Atomic element in Ptr/MutPtr construction before interning
  the pointer type. Check the resolved canonical element, not its spelling.
- Keep `ref` rejection in `checkReference` even when no expected pointer type
  is present.
- Reuse transitive `ContainsAtomic` for every copy-requiring position and Heap
  allocation.
- Do not reject pointers to objects merely because their inline layout contains
  Atomic.
- A construct containing both an invalid pointer type and invalid `ref` need
  report only the earliest provable error.

#### Tests

- Cover direct and aliased Ptr/MutPtr Atomic pointees, generic substitution,
  direct `ref`, member `ref`, direct allocation, and containing-object
  allocation.
- Accept Ptr/MutPtr to a containing object and use Atomic operations through
  its member.
- Retain direct binding/member construction and reject copying afterward.

### 8.6 Consequence for Task results

Standalone Nil removal eliminates `Task<Nil>` and functions declared `: Nil`.
It does not create a hidden unit type.

- `spawn` requires its named function to declare a valid result R.
- Spawning a no-result function is a Type Error because no `Task<R>` can be
  formed.
- A task intended only for effects must return an explicit meaningful payload,
  commonly Bool or an application-defined result. Hexal does not manufacture
  Nil for it.
- Existing tests and examples using `fun worker(): Nil` / `Task<Nil>` must be
  rewritten; they must not be mechanically changed to another fake unit value.

Required diagnostic:

```text
[Type Error] spawn requires a function with a result
```

### 8.7 Already-conforming RFC 0050 rules

Verify rather than redesign these existing behaviors:

- the exact `for` source/binder-arity matrix;
- root `return` rejection;
- pointer-plus-Nil niche classification excludes handles;
- closed printability and first unsupported member-path diagnostics;
- Task/Stream/Channel eligibility after the shared position fixes;
- built-in receiver, parameter, result, ownership, trap, and invalidation
  surfaces not changed by section 8.2.

If a focused test disproves conformance, fix the behavior under this item. Do
not edit the reference to preserve an older implementation.

## Coordination and repository updates

- RFC 0048 tripwires for items 3-5 are deleted when those fixes land. If RFC
  0048 has not created them yet, do not create already-obsolete tripwires.
- On completion, remove only the `docs/status.md` rows owned by this RFC. Remove
  a now-empty heading; do not remove unrelated status entries.
- Update only the Size rules in `docs/reference.md` as specified by item 6.
  Items 1-5 and 8 otherwise implement the current reference without editing it.
- Update RFC 0052's coordination text so it no longer claims RFC 0049 selects
  a `size_t` width. Do not attempt to design RFC 0052 in this work.
- Set this RFC to `Implemented` only when all eight items are complete.

## Non-goals

- Changing Rune equality, ordering, or conversion.
- Allowing unparenthesized bitwise-or inside a match position.
- Adding pointer indexing, pointer arithmetic, or new `ref` capabilities.
- Reworking diagnostic categories generally.
- General target metadata, probing, toolchain discovery, or cross-compilation
  evidence; RFC 0052 owns those questions where compiler-side target knowledge
  is actually required.
- Generator restructuring beyond shared traversal.
- Reference-only presentation, terminology, or relocation work already
  completed by RFC 0050.

## Acceptance criteria

1. Items 1-5 and 8 satisfy their contracts and focused tests.
2. No conformance reproduction produces `Unknown Error`.
3. Size always lowers to `size_t`; Hexal carries no target width and emits no
   fixed-width Size assertion.
4. Every implicit Size conversion and common-type decision is target-independent;
   explicit conversions retain compile-time or runtime range checks.
5. One shared fail-closed walker replaces the named duplicated traversals
   without changing generated output.
6. C23 output still compiles after item 7 under the retained toolchain tests.
7. RFC 0048 tripwires and this RFC's status rows are removed as applicable.
8. Every RFC 0050 executable rule is either covered by an implementation change
   or a focused test proving the compiler already conforms.
9. `go test ./...` passes without an external toolchain.
10. The retained C23-tagged tests pass with the supported toolchain before RFC
    0048 removes them.
11. The workbench binary is rebuilt into `bin/` and the running workbench is
   restarted before handoff, as required by `AGENTS.md`.

## Approved decisions

- Size lowers directly to `size_t`; the C compiler and selected C target own
  its width, range, alignment, and representation.
- Hexal has no Size-width target profile and emits no fixed-width Size
  assertion.
- Size mixes with no distinct numeric type implicitly. Source must use an
  explicit checked conversion, giving every expression one portable Hexal type.
- RFC 0050 compiler conformance is part of RFC 0049.

## Open questions

None.
