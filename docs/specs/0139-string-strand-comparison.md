# RFC 0139: String and Strand Comparison

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-08-27
- Origin: RFC 0103 finding F5
- Coordinates with: `docs/reference.md`

## Summary

Allow direct comparison between String and Strand without allocating a String
copy of the Strand.

The two types already define equality and ordering by UTF-8 bytes. Their
different storage forms currently force identical operand types, so mixed text
comparison needs `strand.to_string(heap)` and an avoidable allocation.

## Semantics

1. String and Strand are the one non-numeric mixed-type comparison pair.
2. `==`, `!=`, `<`, `<=`, `>`, and `>=` are valid in both operand orders.
3. Comparison uses unsigned-byte lexicographic order with the shorter equal
   prefix first, identical to existing String and Strand ordering.
4. Strand's logical byte length is the first zero byte or 32 bytes. Its
   canonical representation already forbids embedded zero after the content.
5. Comparison performs no allocation, conversion, mutation, or ownership
   transfer.
6. Same-type String and Strand comparison remains unchanged.
7. No other non-numeric mixed canonical types become comparable.

## C23 lowering

- Add one mixed text comparison operation that receives a pointer-and-length
  pair for each operand.
- Reuse `memcmp`; do not create a temporary `hex_string` or heap allocation.
- The checker records both operand types in checked metadata; the generator
  must not infer the right type from the left.
- Emit any mixed helper only when a mixed comparison is reachable. If a direct
  expression is clearer than a helper and does not duplicate evaluation, use
  the direct expression.
- Each source operand evaluates exactly once, left before right.

## Implementation plan

### Phase 1: checker

1. Extend deep comparison admission with the exact String/Strand pair before
   the general identical-type rejection.
2. Preserve operator eligibility and construct checked metadata containing both
   operand types.
3. Keep constant folding unchanged; no new literal-only shortcut is required.

### Phase 2: checked representation and generation

4. Extend or add one text-comparison node whose validation records left type,
   right type, operator, and Bool result.
5. Render each operand once in source order.
6. Obtain String data/length directly and Strand data/bounded length directly.
7. Lower equality and ordering with `memcmp` plus the existing shorter-prefix
   rule.
8. Fail closed on invalid mixed metadata.

### Phase 3: component discovery

9. Select the String component and any required helper exactly when a mixed
   comparison is reachable.
10. Keep same-type Strand comparison independent of the String ordering helper.
11. Audit unions, aggregates, generics, and module emission for the new node.

### Phase 4: tests and documentation

12. Add the exhaustive Validation cases below.
13. Update `docs/reference.md` after behavior is stable by replacing the current
    mutual-comparison prohibition with this exact exception.
14. Regenerate snippet hashes only for newly added examples or artifacts whose
    generated comparison legitimately changes.

## Validation

This section is exhaustive.

- Equality and inequality work in both String/Strand operand orders.
- All four ordering operators work in both operand orders.
- Equal ASCII and multi-byte UTF-8 content compare equal.
- Unequal prefixes and equal prefixes of unequal length order correctly.
- A 32-byte Strand compares without reading beyond its storage.
- Each side-effecting operand evaluates once, left before right.
- Generated C contains no String allocation, Strand-to-String conversion, or
  cleanup for mixed comparison.
- Same-type String and Strand comparisons retain their current lowering.
- An unrelated mixed pair remains rejected by the identical-canonical-type
  rule.
- Generic code specializing to String/Strand receives the same behavior.
- `go test ./...` and `go vet ./...` pass.

## Non-goals

- Implicit assignment or argument conversion between String and Strand.
- Mixed text concatenation.
- Text hashing or Dict<String, V>.
- Locale-aware or normalized Unicode comparison.
