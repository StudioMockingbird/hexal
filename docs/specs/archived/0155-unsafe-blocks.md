# RFC 0155: Explicit `unsafe do ... end` Blocks

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented as written, with no documented deviation.
  `unsafe do ... end` parses as one statement with ordinary nesting and
  `end` recovery; the checker carries a lexical unsafe-depth counter,
  inherited by nested block frames and reset per function body, that
  gates one shared `requireUnsafe` predicate; `Slice<T>.from_pointer` and
  `Slice<mut T>.from_pointer` are the first, and so far only, classified
  consumers, checked after their ordinary pointer-mode, element,
  nullability, and Size validation. The block lowers as its enclosed
  statements in source order with no C artifact of its own. Verified by
  `go test ./...`, `go vet ./...`, and the tagged C23 suite running under
  GCC, Clang, and `zig cc`, including a fixture exercising the migrated
  `Slice.from_pointer` bridge inside an explicit block. `docs/reference.md`
  is not yet updated: that edit awaits explicit user approval per this
  RFC's own text
- Created: 2026-09-09
- Updated: 2026-09-15
- Coordinates with: RFC 0039 (future foreign operations), RFC 0156 (pointer
  arithmetic and casts), and implemented RFC 0161 (current Ptr/Slice model)

## Summary

Add one explicit lexical boundary for operations whose safety cannot be proved
locally:

```hexal
unsafe do
    bytes := Slice<Byte>.from_pointer(pointer, length)
    consume(bytes)
end
```

The block grants permission only to operations individually classified as
unsafe-capable. It does not disable parsing, typing, bounds,
exhaustiveness, or generated-C checks.

For each operation this mechanism eventually gates, ordinary syntax retains
that operation's safe contract. Inside unsafe, the programmer asserts its
documented preconditions; a false assertion may produce undefined behavior.
This RFC does not claim to eliminate every existing manual-lifetime hazard.

## Grammar

```ebnf
statement = non-control-statement | return-statement
            | if-statement | while-statement | for-statement
            | local-function-declaration | unsafe-statement ;
unsafe-statement = "unsafe" , "do" , block , "end" ;
```

- `unsafe` is a reserved word.
- The form uses `do` like every other Hexal block opener.
- Unsafe is a statement, not an expression, modifier, function effect, type,
  annotation, or value.

## Semantics

- The checker carries a lexical unsafe-depth counter.
- Entering the block increments the counter; every exit restores the enclosing
  value.
- An unsafe-capable operation is accepted only when the counter is non-zero.
- Nested unsafe blocks are valid and add no capability beyond the outer block.
- Ordinary invalid programs remain invalid inside unsafe.
- Safe wrappers may contain a small unsafe block internally; callers do not
  inherit or need an unsafe context.
- Unsafe scope is lexical only. It cannot be returned, stored, captured,
  inferred, or propagated through a call.
- The block is an ordinary lexical source scope: bindings declared inside are
  unavailable afterward. Generated C need not add braces solely for the unsafe
  permission when its existing unique-name and control-flow lowering preserves
  that source scope.
- A value needed afterward is declared outside and assigned inside, or is
  consumed inside the block. Unsafe does not introduce a second expression
  form solely to return one value from the region.
- Unsafe permission is checked where an operation is written, including inside
  a deferred action. A deferred unsafe-capable operation written inside the
  block is authorized even though it executes later; its author asserts that
  the operation's preconditions will hold at execution.

## Initial unsafe-capable operation

- Every `Slice<T>.from_pointer` and `Slice<mut T>.from_pointer` call requires an
  active unsafe block. The compiler cannot prove the supplied region's length,
  lifetime, alignment, initialization, provenance, or future validity.
- Pointer-mode and argument-shape checks remain ordinary checks: a writable
  Slice still requires `Ptr<mut T>`, length is still Size, and nullable or
  incompatible pointers remain invalid inside unsafe.
- `Slice.empty`, Array/List/String slicing, indexing, and re-slicing do not
  require unsafe.
- Stash reset/destroy and Pool operations retain their current compile-time and
  runtime contracts and do not change in this RFC.
- RFC 0156 and RFC 0039 may add further individually classified consumers later.

The block does not itself add pointer arithmetic, unchecked indexing,
arbitrary casts, manual union-tag writes, or any other operation. Each new
unsafe operation requires its own specification and validation.

## Diagnostics

- `Slice.from_pointer` outside the block reports Type Error:
  `Slice.from_pointer requires an unsafe do ... end block`.
- An operation that is invalid regardless of safety reports its ordinary
  earliest-phase diagnostic.
- An unsafe block containing no unsafe-capable operation is valid; no warning is
  emitted.

## C23 lowering

- The block emits only its statements in source order.
- No C scope, helper, flag, runtime check, marker, metadata, or performance cost
  is required solely because the source used `unsafe`.
- Existing source mapping remains attached to the enclosed statements.

## Non-goals

- Preserving the no-undefined-behavior guarantee after an unsafe precondition is
  violated. Unsafe is the explicit opt-out required for full C expressiveness.
- Disabling ordinary local cleanup or lifetime diagnostics wholesale.
- Unsafe functions, unsafe types, capability tokens, or effect propagation.
- Inferring that a function is unsafe from its body.
- Treating every raw-pointer use as unsafe in this first version.

## Required sweep

- parser keywords, statement dispatch, recovery, and block termination;
- checked-statement representation and traversal;
- current Slice construction and pointer-mode checks;
- foreign/raw-operation gates owned by RFC 0039;
- generator statement dispatch and source mapping;
- reserved-word, formatting, snippet, and diagnostic tests.

## Validation

This section is exhaustive.

- `unsafe do ... end` parses as one statement and supports nesting.
- A binding declared inside is rejected after the block; an outer mutable
  binding assigned inside remains available afterward.
- Read-only and writable `Slice.from_pointer` fail outside and succeed inside
  the block when their ordinary type/shape preconditions hold.
- The outside-block failure uses the exact diagnostic above.
- A writable Slice from `Ptr<T>`, a mismatched element pointer, a nullable
  pointer before narrowing, and a non-Size length retain their existing earlier
  diagnostics inside unsafe.
- `Slice.empty`, Array/List/String slicing, indexing, and re-slicing compile
  outside unsafe unchanged.
- Stash and Pool operations gain no unsafe requirement.
- An unsafe-capable operation inside a safe wrapper does not make its caller
  unsafe.
- Type errors, locally proved repeated release, invalid construction,
  out-of-bounds constants, and unsupported operations remain rejected inside
  unsafe with their ordinary diagnostics.
- Unsafe context does not escape through a call: a function containing
  `Slice.from_pointer` must contain its own unsafe block, while a safe caller
  may call a wrapper whose body contains that block.
- A deferred unsafe-capable operation is accepted when written inside unsafe
  and rejected when written outside it, regardless of where the defer later
  executes.
- Empty and capability-free unsafe blocks compile without warning.
- Generated C for a valid unsafe block equals the lowering of its enclosed
  statements except for normal source-location movement; no unsafe runtime
  artifact is emitted.
- Existing `Slice.from_pointer` tests and snippets migrate to explicit unsafe
  blocks. Manifest movement is limited to artifacts whose source mapping moves
  because of that syntax and to the new unsafe snippet.

## Detailed implementation plan

### Phase 1: syntax and checked representation

1. Reserve `unsafe` and parse the block with existing `end` recovery.
2. Add `unsafe-statement` to the normative statement alternatives and reuse the
   existing `block` production; do not create a parallel statement-list rule.
3. Add an explicit checked unsafe statement so parser/checker/generator
   dispatch remains fail-closed.

### Phase 2: capability context

1. Add lexical unsafe depth to checker context with balanced enter/leave on
   every normal and diagnostic path.
2. Add one shared predicate for operations whose owning RFC marks them unsafe-
   capable; do not scatter keyword checks through unrelated code.
3. Preserve ordinary validation before granting the narrow unsafe exemption.
4. Reuse ordinary lexical-scope entry, exit, local cleanup, and deferred-action
   handling for the enclosed block.

### Phase 3: Slice bridge

1. Require active unsafe depth in the shared `Slice.from_pointer` checker path
   after ordinary callee/type resolution and before checked-node construction.
2. Preserve every pointer mode, element identity, nullability, and Size check.
3. Migrate all existing accepted `Slice.from_pointer` tests and snippets to
   explicit unsafe blocks; add focused rejection outside the block.
4. Confirm no other operation changes behavior.

### Phase 4: lowering and conformance

1. Lower enclosed statements directly with existing source mapping.
2. Implement every Validation case in focused parser/checker and
   integration tests.
3. Add one compact workbench snippet and update the manifest only for that
   deliberate addition.
4. Run ordinary and tagged C23 suites.
5. Once behavior stabilizes, synchronize `docs/reference.md` only with explicit
   user approval; update status and close only when code and canonical docs
   agree.

## Open questions

None.
