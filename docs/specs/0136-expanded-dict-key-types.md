# RFC 0136: Expanded Dict Key Types

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design decisions required
- Created: 2026-08-27
- Origin: RFC 0103 finding F4
- Coordinates with: RFC 0117 (compile-time evaluation), `docs/reference.md`

## Summary

Expand `Dict<K, V>` beyond its current `Int32` and `Strand` key families while
keeping key hashing, equality, storage, and generated C explicit.

This is a feature proposal, not a bug. The current restriction is implemented
and documented consistently.

## Recommended scope

- Add Bool, Byte/UInt8, every other fixed-width integer, Size, and Rune.
- Preserve exact key identity; numeric widening must not make differently
  declared Dict key types interchangeable.
- Hash the canonical C representation using one compiler-owned operation per
  scalar family; equality must match that representation exactly.
- Keep String excluded initially. An owning, heap-backed key would require a
  copy/borrow/lifetime policy that scalar keys and Strand do not need.
- Keep Float32/Float64 excluded. NaN and signed zero require an explicit
  equality-and-hash policy and are poor default keys.
- Keep objects, ADTs, unions, pointers, functions, allocators, and containers
  excluded until a separate structural-hashing design exists.

## Open decisions

1. Whether Bool is useful enough to justify a specialization. Recommendation:
   include it for uniform scalar completeness; its implementation is trivial.
2. Whether Rune hashes its numeric scalar value or its UTF-8 encoding.
   Recommendation: numeric scalar value, matching Rune equality and C storage.

## Required design work

1. Replace the checker key allowlist with one authoritative key-eligibility
   predicate shared by resolution, specialization, and validation.
2. Define stable hash formulas for each admitted C representation.
3. Ensure generated helper identity includes the exact canonical key type.
4. Audit Dict construction, lookup, insertion, removal, iteration, generic
   specialization, and module component discovery for the expanded set.
5. Synchronize `docs/reference.md` during implementation after behavior is
   stable.

## Validation required before implementation-ready

- One end-to-end insert/find/remove case per newly admitted representation
  width and signedness class.
- Bool, Size, and Rune have explicit cases.
- Distinct scalar key types remain distinct Dict types.
- Generic Dict code specializes for every admitted key family.
- String, Float, pointer, aggregate, and owning keys remain rejected with the
  existing key-position diagnostic class.
- Generated C uses no allocation or conversion merely to hash a scalar key.
- Existing Int32 and Strand behavior remains byte-identical.

## Non-goals

- User-defined Hash/Eq protocols.
- Structural keys.
- Owning String keys.
- Cross-type numeric lookup.
