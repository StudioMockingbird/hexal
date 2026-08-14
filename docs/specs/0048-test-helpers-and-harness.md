# Spec 0048: Reference-Derived Test Conformance

- Kind: project convention and execution plan
- Status: Implemented
- Scope: tests and test infrastructure only; no compiler or language changes
- Source of truth: `docs/reference.md`
- Related: specs 0013, 0049, 0050; `docs/status.md`

## Objective

- Make the test suite an auditable executable projection of every testable rule
  and semantic contract in `reference.md`.
- Replace tests that assert obsolete behavior.
- Add missing compile-time, generated-C, and runtime coverage.
- Do not create tests for behavior the reference deliberately leaves
  unspecified.

## Baseline

- 69 test files, 841 test functions, approximately 13,500 lines.
- `go test ./...` passes.
- `go test -tags c23 ./compiler` passes.
- Passing is not conformance: current tests accept standalone `Nil`,
  `Task<Nil>`, and implicit `Size`/`UInt64` mixing, and one test rejects valid
  `Array<String,N>` storage.

## Test layers

- Lexer tests own tokenization, maximal munch, literal shape, source locations,
  comments, and raw-character rejection.
- Parser tests own grammar shape, precedence, associativity, same-line rules,
  recovery, and syntax diagnostics. A parser may accept a form later rejected
  semantically.
- Checker tests own canonical types, placement, copyability, narrowing,
  assignability, result presence, and earliest static diagnostics.
- Generator tests own complete fail-closed traversal, C declarators,
  qualifiers, source mapping, and emitted semantic guards.
- Integration tests in `compiler/<facet>_test.go` own accepted/rejected public
  compiler behavior end to end.
- C23-tagged tests own generated-C validity and observable runtime contracts.
- A rule normally has one stage-level test at its earliest owner and one
  integration test. Add runtime coverage only where observation requires it.

## Test infrastructure

Add domain helpers to `compiler/helpers_test.go`:

```go
func assertCompiles(t *testing.T, source string) CompileResult
func assertRejects(t *testing.T, source, want string)
func assertEmits(t *testing.T, source string, wants ...string)
```

- Each helper calls `t.Helper()` and prints the source plus full diagnostics on
  failure.
- `assertRejects` requires failure and a matching first diagnostic.
- Stage packages add local helpers only after repeated use is demonstrated.
- Every table uses named `t.Run` subtests with unique, stable names.
- Facet headers cite the relevant `reference.md` section, not archived RFCs.
- Add a corpus sweep requiring known-invalid programs to produce a classified
  source diagnostic, never `Unknown Error`.

## C23 harness

- The default `go test ./...` suite remains pure Go.
- Retain all `compiler/c23_*_test.go` files behind `//go:build c23`.
- Run them with `go test -tags c23 ./compiler`.
- Add the build tag to every C23 file and remove `LookPath`/`t.Skip` gating; an
  explicitly requested tagged run fails when its toolchain is missing.
- Centralize compile/run behavior in one helper using `-std=c23 -Wall -Wextra
  -Werror`; warnings fail the test.
- The generator emits helper families wholesale (equality, print, union, heap,
  io), and Hexal permits unused bindings, so the harness additionally passes
  `-Wno-unused-function -Wno-unused-variable -Wno-unused-parameter`. Every
  other warning class still fails. The unused-helper emission is recorded as
  debt in `docs/status.md`.
- String assertions do not replace execution for traps, exact output,
  synchronization, or runtime state transitions.

## Existing tests to update

### Standalone `Nil`

- `compiler/nullability_test.go`
  - Replace `TestNilValueAndBindingLowerToNullptr`; `nothing: Nil = nil` rejects.
  - Preserve null-pointer lowering through `Ptr<Int32> | Nil`.
- `compiler/checker/nullability_test.go`
  - Remove accepted `nothing: Nil = nil` from the resolution test.
  - Add it to the standalone-Nil rejection matrix.
- `compiler/checker/functions_test.go`
  - Replace accepted `fun absent(): Nil` with a no-result function.
  - Make the call-in-value diagnostic prove absence of a result type.
- `compiler/checker/unions_test.go` and `compiler/unions_test.go`
  - Replace `missing: Nil = value` in Nil-narrowed branches with permitted
    direct use such as `print(value)`.
- `compiler/print_test.go`
  - Change `TestPrintNoResult` so its destination is otherwise valid; failure
    must prove that `print` has no result, not that standalone Nil is invalid.
- `compiler/io_test.go`
  - `TestStdioCompileTimeRejections` includes `fun f(): Nil` wrapping
    `Stdio.stdout().close()`. The case still fails after RFC 0049 §8.1, but for
    the wrong reason: standalone Nil, not the borrowed-standard-File rejection
    it exists to prove. Change the wrapper to a no-result `fun f()`.
  - Its other entries use `Nil | Error` results and `result: Nil | Error`
    bindings, which stay valid. Change only the one standalone case.
- Parser tests may retain standalone Nil syntax cases but must state that they
  test syntax only.

The complete affected set, verified by fixed-string search, is:
`compiler/nullability_test.go`, `compiler/unions_test.go`,
`compiler/print_test.go`, `compiler/io_test.go`,
`compiler/concurrency_test.go`, `compiler/checker/nullability_test.go`,
`compiler/checker/functions_test.go`, `compiler/checker/unions_test.go`,
`compiler/parser/type_expressions_test.go`, and
`compiler/c23_concurrency_smoke_test.go`.

`compiler/types/nullability_test.go` and `compiler/types/types_test.go`
reference the canonical `Nil` type in Go-level tables, not Hexal source. RFC
0049 §8.1 leaves the internal representation unchanged, so both stay as they
are. `compiler/c23_io_smoke_test.go` uses only `Nil | Error` unions and is
likewise unaffected.

### `Task<Nil>` and Nil-result workers

- In `compiler/concurrency_test.go`, replace `TestSpawnNilResultCompiles` with a
  rejection test for spawning a no-result function.
- Change workers in Channel, Mutex, and scheduler-yield tests to return a real
  copyable result such as Bool or Int32.
- Make the same change in `compiler/c23_concurrency_smoke_test.go`.
- Remove every `fun worker(): Nil` and `Task<Nil>` acceptance case.

### `Size`

- Replace `TestSizeAndUInt64WidenBidirectionally` in
  `compiler/conversion_test.go` with the RFC 0049 portable rule: Size has no
  implicit conversion or binary common type with a distinct numeric type.
- Change API result bindings from UInt64 to Size in:
  - `compiler/array_test.go`
  - `compiler/list_test.go`
  - `compiler/view_test.go`
- Keep explicit `to<Size>()` and `size.to<T>()` coverage.
- Land this group with RFC 0049's Size implementation and reference update.

### Storability and terminology

- Replace `TestStringInArrayIsRejected` in `compiler/string_test.go` with an
  acceptance and shallow-copy test; `Array<String,N>` is valid.
- Rename `TestViewFromPointerRejectsManagedElements` in
  `compiler/view_bridge_test.go`; the source fails because `Ptr<String>` is an
  invalid pointee, not because `View<String>` is invalid.
- Remove stale affine/move/destruction terminology from String, List, Dict, and
  C23 smoke-test comments. Use shallow copy, alias, explicit cleanup, and
  container-only cleanup.
- `TestListOfStrings` and `TestDictStringValues` pop/remove a String literal
  handle and free it — a runtime trap under shallow semantics that compile-only
  assertions mask. Rewrite with runtime Strings (`"...".to_string(h)`) freed
  correctly, and keep a case that a collection never frees a stored literal.
- `TestGeneratedListCCompiles` claims removed "copy-in/move-out/destruction"
  behavior in its comment and frees a literal-derived handle in its source; fix
  both and consider switching from compile-only to compile-and-run.

## New compile-time conformance tests

### RFC 0049 regression set

- Rune arithmetic:
  - Reject Rune with `+`, `-`, `*`, `/`, and `%`.
  - Cover Rune/Rune and Rune/fixed-integer in both orders.
  - Cover literal and binding operands.
  - Preserve Rune equality, ordering, and explicit conversion.
- Root `errdefer`:
  - Parser accepts the statement form.
  - Checker rejects it at root at the keyword with the specified Type Error.
  - Valid function-scoped `errdefer` remains accepted.
- Match boundaries:
  - Accept `and`/`or` in scrutinees and arm results.
  - Accept parenthesized bitwise-or and parenthesized scrutinee `is` tests.
  - Cover nested matches with independent arm boundaries.
- Mixed `ref` places:
  - Cover member-then-index, index-then-member, and arbitrary mixed chains.
  - Derive pointer writability from the final place.
- Raw String newlines:
  - Reject LF, CRLF, CR, and backslash followed by a physical newline.
  - Report the newline location; treat CRLF as one newline.
  - Preserve escaped `\n` acceptance.
- Generator traversal:
  - Visit every checked node and nested type-bearing child.
  - Include initializer, binary expression, defer, errdefer, try statement,
    and match.
  - Unknown nodes produce structured `Unknown Error`, never omission.

### Standalone-Nil matrix

Reject direct use and use through aliases or generic substitution in:

- alias, binding, parameter, function result, object member, ADT payload;
- Array/View/List element and Dict key/value;
- generic argument, Ptr/MutPtr pointee, Fun parameter/result;
- Task result, Channel element, Stream element/state, and Heap allocation;
- a binding initialized from a value narrowed to Nil.

Accept:

- unions containing Nil plus a scalar, pointer, handle, aggregate, or multiple
  non-Nil members;
- bare `nil` only under such a contextual union or as a `print` argument;
- direct printing of a union narrowed to Nil.

### No-result matrix

Inspect checked nodes and prove zero result type, not canonical Nil, for:

- user no-result function and `print`;
- List `push`, `set`, `clear`, `free`;
- Dict `insert`, `free`;
- String and Stream `free`; File `close`;
- Task `detach`, `yield`;
- Channel `close`, `free`;
- Mutex `lock`, `unlock`, `free`;
- Atomic `store`; MutPtr `write_volatile`.

For every family:

- accept as a call statement and direct cleanup action where cleanup validity
  permits it;
- reject as a binding initializer, argument, return value, condition, operator
  operand, collection element, or object initializer.

Retain `Nil | Error` for fallible no-payload File write/write_text/flush and
Channel send operations.

### Position eligibility

Build a table-generated matrix for every reference position:

- Binding, ObjectMember, ADTPayload, UnionMember;
- ArrayElement, ViewElement, ListElement, DictValue;
- FunctionParam, FunctionResult, TaskArgument, TaskResult;
- ChannelElement, StreamElement, StreamState, Pointee, HeapAllocation.

Test representative scalar, object, ADT, Array, String/handle, View, Fun,
Unknown, Nil, Atomic, and Atomic-containing aggregate types. Apply completeness,
finite-size, copyability, and feature exclusions after aliases and generic
substitution.

### Pointers and Atomic

- Independently reject `Ptr<Atomic<T>>` and `MutPtr<Atomic<T>>` at type
  composition, including aliases and generic substitution.
- Independently reject `ref atomicBinding` and `ref object.atomicMember`.
- Accept Ptr/MutPtr to an enclosing object containing Atomic.
- Pointee matrix:
  - reject String, List, Dict, Stream, View, Fun, Nil, and direct Atomic;
  - accept Task, Channel, Mutex, ordinary complete types, and Unknown;
  - reject Unknown by value and dereference until recovered through one pointer
    layer.

### Function-type placement

- Accept Fun as a binding, parameter, nested Fun parameter, and union member.
- Reject Fun as result, object/ADT member, collection position, Task
  argument/result, Channel element, Stream element/state, pointee, Heap
  allocation, and `ref` target.
- Repeat critical cases through aliases and generic substitution.

### Numeric rules

- Table every accepted and rejected lossless widening pair.
- Table binary common-type selection and rejection for every numeric family.
- Prove surrounding result context does not alter common-type selection.
- Prove expected numeric types propagate through arithmetic but comparisons and
  logical contexts use default literal types.
- Cover signed minima, negative zero, unsigned `-0`, and Rune's lack of implicit
  widening.
- Cover explicit conversion identity, aliases, constants, dynamic guards,
  ties-to-even, truncation, Rune scalar validation, and prohibited Bool/pointer
  conversions.

### Evaluation order

Add observable compile/emission tests only where order is normative:

- Array literal elements: once, left-to-right.
- Print arguments: once, left-to-right, with output after evaluation.
- Spawn arguments: once, left-to-right.
- View.from_pointer: pointer then length, each once.
- Match scrutinee and for source: once.

Do not pin operand, ordinary call-argument, receiver/argument, or object
initializer order.

### Control flow, cleanup, and errors

- Add lexer/parser/checker/generator/integration coverage for try statements.
- Accept try statements over Nil-success and payload-success unions; discard
  success values.
- Reject try statements at root, in non-Error-result functions, and inside
  cleanup actions.
- Cover defer/errdefer shared reverse order on Error exit and result discard.
- Cover sole-continuing-path narrowing after terminating return, break, and
  continue alternatives.
- Cover narrowing invalidation after assignment and writable address escape.

### Remaining static surfaces

- Union formation requires at least two distinct canonical members after
  flattening and duplicate removal. Reject `Int32 | Int32`, `Nil | Nil`, the
  same duplicate through a transparent alias, and the same through generic
  substitution; a collapsing union is a Type Error, never an alias for the
  surviving member. Accept `Int32 | Float64` and a union written with a
  redundant member alongside two distinct ones. The compiler accepts the
  collapsing forms today, so these are new rejection tests landing with RFC
  0049 §8.1.
- Complete the exact `for` source/binder matrix and reject every other arity.
- Complete protected-name coverage for all types, constructors, operations, and
  `Stdio`.
- Complete collection API arity/type diagnostics and equality exclusions.
- Complete Stream element/state/callback restrictions through aliases and
  generics.
- Complete printability recursion and require the diagnostic to name the first
  non-printable member path in declaration order.
- Print rejects functions, Heap, File, Task, Channel, Mutex, and Atomic, naming
  the first non-printable member path in declaration order.
- Array/List element replacement during iteration is accepted.
- `eos` is truthy; only `false` and `nil` are falsey.
- Complete File mode/static-path/Stdio restrictions and no-result close rules.
- Complete layout intrinsic and volatile type/receiver/value restrictions.

## New C23 runtime tests

### Numeric and bounds traps

- Dynamic division/remainder by zero and invalid shift count.
- Signed `MIN / -1` and `MIN % -1` results.
- Dynamic conversion overflow, NaN/infinity to integer, and invalid Rune scalar.
- Dynamic Array/View/List indexing and slice bounds.
- Empty List pop and missing Dict get/remove.

### Collections and lifetime surface

- Dict insertion replacement and supported-key lookup/hash consistency.
- Array/View/List equality by length and elements.
- Container free releases only container storage.
- Handle copies observe the same List/Dict state.
- Strand hashing ignores terminator/zero tail.
- Do not attempt to prove absence of leaks or diagnose every dangling alias.

### Text

- String/Strand length and indexing count Runes, not bytes.
- String slice translates Rune bounds to zero-copy byte bounds.
- `String.from_bytes` traps on malformed UTF-8.
- RuneCursor copies advance independently; next after exhaustion traps.
- Strand rejects embedded NUL, malformed UTF-8, and more than 31 UTF-8 bytes.
- Strand exposes no byte View; String and Strand dispatch separately.

### Streams

- Construction is lazy; callbacks run only when pulled.
- One next returns at most one public value; filter may pull repeatedly.
- Take stops at its exact count; breaking for permits later pulls.
- Exhaustion does not free adapters.
- List source captures length and observes same-length replacement.
- Aliases share one non-reentrant cursor.
- Adapter construction consumes upstream and one chain uses one Heap.

### Print

- Assert exact output for objects, unit/record ADTs, Array, View, List, Dict,
  nested Nil, direct/nested text and Rune, numeric Byte, and direct/nested Error.
- Cover signed zero, infinities, NaN, empty forms, separators, and absence of an
  implicit newline.
- Cover argument evaluation-before-output and atomic calls under concurrent
  Tasks.

### Files

- Mode mismatches return Error before C access.
- Malformed UTF-8 from read_text returns Error.
- Errors expose the specified header and portable message.
- Closing one File alias invalidates all; copied Stdio handles retain borrowed
  restrictions.
- Error.new records one-based line and UTF-8 byte column, and the synthetic
  `main.hex` file field.

### Tasks, Channel, Mutex, and Atomic

- Spawn arguments evaluate once left-to-right; join returns the exact copied
  result; one join or detach succeeds across aliases.
- Detach discards/reclaims the result.
- Channel covers FIFO, capacity, length, is_closed, dynamic zero capacity,
  send-after-close Error, idempotent close, queued values after close, and EoS
  only after closed/drained.
- Mutex covers recursive lock, wrong-owner/double unlock, and freeing
  locked/waited live state.
- Atomic covers every supported T and operation, Bool fetch rejection, strong
  compare_exchange, and shared-counter synchronization.

### Layout, volatile, and C23 output

- size_of/align_of cover complete type families and source handle sizes.
- Volatile receiver/value evaluate once and emit volatile access without atomic
  or synchronization machinery.
- C name prefixes cover bindings, types, members, functions/methods, and
  already-prefixed names.
- `HEX_` is reserved for generated macros: assert `HEX_HEAP_DEFAULT`,
  `HEX_FILE_READ`/`HEX_FILE_WRITE`/`HEX_FILE_APPEND`, `HEX_TASK_*`, and
  `HEX_NUMERIC_TRAP_DEFINED` are emitted.
- Cover every nested Ptr/MutPtr qualifier row from `reference.md`, trailing
  binding const, unqualified object members, and absence of qualifier-discarding
  casts.
- Only pointer-plus-Nil uses the null niche; general Nil/EoS unions retain
  distinct tags.
- Representative generated output compiles cleanly under the retained C23
  harness.

## Deliberately untested guarantees

Do not add tests that impose behavior where the reference states none:

- unspecified ordinary evaluation order;
- Dict iteration order, hash algorithm, or seed;
- detection of every leak, stale alias, double-free, or use-after-free;
- cleanup during traps;
- physical-media durability after flush;
- OS scheduling fairness;
- caller-maintained View lifetime and structural-stability obligations unless
  the implementation explicitly detects the violation;
- excluded features beyond focused fail-closed syntax/semantic rejection.

## Migration order

1. Add helpers and named subtests without changing coverage.
2. Normalize all C23 tests behind one tag and harness.
3. Fix obsolete/contradictory tests: Nil, Task<Nil>, String storage, pointee
   wording, and stale terminology.
4. Land RFC 0049/0050 regression tests with their implementations; do not add
   known-failure tripwires.
5. Add standalone-Nil, zero-result, position, pointer/Atomic, and Fun matrices.
6. Add numeric, control-flow, evaluation-order, and remaining static matrices.
7. Add runtime suites by facet.
8. Add the diagnostic-class sweep and reference-section citations.
9. Run `go test ./...` and `go test -tags c23 ./compiler`.

Each step keeps the applicable suite green. A semantic mismatch found while
adding a test is a compiler conformance bug and is not hidden by weakening the
test.

## Acceptance criteria

1. No test accepts standalone Nil, Nil-result functions, or Task<Nil>.
2. No test relies on implicit Size/fixed-width mixing after RFC 0049 lands.
3. No test rejects a type or position permitted by the reference.
4. Every reference position is present in a generated eligibility matrix.
5. Nil, no-result, Fun, Unknown, Atomic, pointee, and copyability exclusions are
   tested directly and through aliases/generic substitution.
6. Every specified API has success, arity/type rejection, result-type, and
   placement coverage appropriate to its contract.
7. Every specified trap or runtime state transition has a C23-tagged execution
   test unless listed under Deliberately untested guarantees.
8. Every table row is a named subtest; repeated integration assertions use the
   domain helpers.
9. Known-invalid source never produces Unknown Error.
10. Facet tests cite `reference.md` sections and contain no stale ownership or
    archived-behavior claims.
11. `go test ./...` passes without a C toolchain.
12. `go test -tags c23 ./compiler` compiles with warnings enabled and passes on
    a supported C23 toolchain.
13. No test pins deliberately unspecified behavior.

## Open questions

None.
