# RFC 0155: Explicit `unsafe do ... end` Blocks

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started. The first-consumer
  sequencing below (RFCs 0149/0153) lapsed with RFC 0165, which removed every
  unsafe-capable operation: Phase 3 must not be implemented as written. Scheduling
  needs a new classified consumer. Later RFCs extend the operation
  set without changing this lexical mechanism
- Created: 2026-09-09
- Updated: 2026-09-10
- Coordinates with: RFC 0039 (foreign operations), RFC 0110 (unproven owner
  invalidation), RFC 0149 (ownership and references), and RFC 0153 (slices)
- Does not update `docs/reference.md`: synchronize only after implementation
  stabilizes and the user explicitly approves the reference edit

## Summary

Add one explicit lexical boundary for operations whose safety cannot be proved
locally:

```hexal
unsafe do
    stash.reset()
end
```

The block grants permission only to operations individually classified as
unsafe-capable. It does not disable parsing, typing, ownership, bounds,
exhaustiveness, or generated-C checks.

Hexal's no-undefined-behavior guarantee applies to safe Hexal. Inside unsafe,
the programmer asserts the documented preconditions of each unsafe-capable
operation. If that assertion is false, generated C may have undefined behavior.
An unsafe block does not imply that an invalid operation will trap.

## Grammar

```ebnf
unsafe-statement = "unsafe" , "do" , statement-list , "end" ;
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
- A deferred action is checked in the context in which it will execute, not
  merely where it is registered. Consequently, placing `defer` or `errdefer`
  inside an unsafe block does not authorize an unsafe-capable operation that
  runs after that block has ended.

## Initial unsafe-capable operations

- `Slice<T>.from_pointer` and `Slice<mut T>.from_pointer` when the pointer's
  live provenance cannot be proved locally. The capability remains confined to
  the direct call or lexical borrow block required by RFC 0153.
- Stash reset/destroy when local provenance cannot prove that no live Ref,
  Slice, raw pointer, or foreign retention depends on the region.
- Pool destruction and `pool.free(slot)` retain RFC 0110's runtime ownership,
  range, alignment, and live-slot validation. Neither requires unsafe solely
  because provenance is unknown.
- Foreign/raw-memory operations that RFC 0039 explicitly classifies as unsafe.

The block does not itself add pointer arithmetic, unchecked indexing, arbitrary
casts, manual union-tag writes, or any other operation. Each new unsafe
operation requires its own specification and validation.

## Diagnostics

- An unsafe-capable operation outside the block reports that the operation
  requires an `unsafe do ... end` block and names the unproved condition.
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
- Disabling ownership or lifetime tracking wholesale.
- Unsafe functions, unsafe types, capability tokens, or effect propagation.
- Inferring that a function is unsafe from its body.
- Treating every raw-pointer use as unsafe in this first version.

## Required sweep

- parser keywords, statement dispatch, recovery, and block termination;
- checked-statement representation and traversal;
- Stash/Pool provenance diagnostics owned by RFC 0110;
- unknown-provenance Slice construction owned by RFC 0153;
- foreign/raw-operation gates owned by RFC 0039;
- generator statement dispatch and source mapping;
- reserved-word, formatting, snippet, and diagnostic tests.

## Validation

This section is exhaustive.

- `unsafe do ... end` parses as one statement and supports nesting.
- Unknown-provenance Slice.from_pointer and unproved Stash reset/destroy fail
  outside and succeed inside the block when every ordinary type/shape
  precondition holds.
- Pool.free retains its runtime slot validation and gains no blanket unsafe
  requirement.
- An unsafe-capable operation inside a safe wrapper does not make its caller
  unsafe.
- Type errors, use-after-move, duplicate cleanup, invalid construction,
  out-of-bounds constants, and unsupported operations remain rejected inside
  unsafe with their ordinary diagnostics.
- Unsafe context does not escape through a call, return, deferred statement,
  nested function, Task, or Channel.
- A deferred Stash reset/destroy remains valid when its safety is locally
  proved. If the operation needs unsafe permission, registering it inside an
  unsafe block does not authorize its later execution and is rejected; the
  operation must execute directly within an active unsafe block.
- Empty and capability-free unsafe blocks compile without warning.
- Generated C for a valid unsafe block equals the lowering of its enclosed
  statements except for normal source-location movement; no unsafe runtime
  artifact is emitted.
- Existing manifest hashes outside snippets deliberately adding unsafe syntax
  do not move.

## Detailed implementation plan

### Phase 1: syntax and checked representation

1. Reserve `unsafe` and parse the block with existing `end` recovery.
2. Add an explicit checked unsafe statement so parser/checker/generator
   dispatch remains fail-closed.

### Phase 2: capability context

1. Add lexical unsafe depth to checker context with balanced enter/leave on
   every normal and diagnostic path.
2. Add one shared predicate for operations whose owning RFC marks them unsafe-
   capable; do not scatter keyword checks through unrelated code.
3. Preserve ordinary validation before granting the narrow unsafe exemption.

### Phase 3: first consumers

1. Integrate RFC 0110's unproven Stash invalidation gates while preserving
   Pool's runtime-validated safe operations.
2. Integrate RFC 0153's unknown-provenance from_pointer gate.
3. Integrate only the foreign/raw operations explicitly selected by RFC 0039.
4. Confirm no other operation changes behavior.

### Phase 4: lowering and conformance

1. Lower enclosed statements directly with existing source mapping.
2. Implement every Validation case in focused parser/checker and integration
   tests.
3. Add one compact workbench snippet and update the manifest only for that
   deliberate addition.
4. Run ordinary and tagged C23 suites.
5. Once behavior stabilizes, synchronize `docs/reference.md` only with explicit
   user approval; update status and close only when code and canonical docs
   agree.

## Open questions

None.
