# RFC 0135: Scalar Value Match

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design decisions required
- Created: 2026-08-27
- Origin: RFC 0103 finding F2
- Blocked by: RFC 0133 (one correct exhaustiveness and pattern-identity model)
- Coordinates with: `docs/reference.md`

## Summary

Extend value-mode `match` beyond Bool so finite scalar constants can be
dispatched without rewriting the operation as an `if` chain.

This is a language feature, not a compiler bug. The current reference
deliberately defines Bool as the only value-mode scrutinee.

## Motivation

Hexal targets C23 and must express switch-like dispatch directly. Restricting
value mode to Bool makes ordinary integer and enum-shaped protocols need
repeated comparisons:

```hexal
result: Int32 := match opcode is
| 1 then handle_open()
| 2 then handle_close()
| else then -1
end
```

## Recommended scope

- Admit Bool, fixed-width integers, Size, Byte, Rune, and EoS values whose arm
  patterns are compile-time constants of the scrutinee type.
- Keep Float32/Float64 out: NaN and signed-zero make exact coverage and
  duplicate detection unintuitive.
- Keep String and Strand out initially: text matching needs a separate cost and
  lowering decision and does not map to a C `switch`.
- Require a final `else` for every domain the checker cannot enumerate fully.
- Continue to reject duplicate and unreachable arms through RFC 0133's one
  ordered coverage model.
- Lower integral value mode to a C23 `switch` when legal; otherwise fail closed
  rather than silently changing evaluation or matching semantics.

## Open decisions

1. Whether named compile-time constants may be patterns in the first version,
   or only literals. Recommendation: literals first; RFC 0117 can later add
   named constants through its one evaluation mechanism.
2. Whether EoS needs value-mode syntax at all. Recommendation: omit it unless a
   second non-union EoS use appears; matching EoS alone has no useful branch.

## Required design work

1. Extend the match-pattern grammar with scalar constant patterns without
   weakening the existing type/variant interpretation rules.
2. Define contextual literal typing, conversion, and range diagnostics.
3. Define duplicate identity after contextual typing.
4. Define which domains are closed and which require `else`.
5. Define deterministic C23 lowering and source mapping.
6. Synchronize the grammar and match contract in `docs/reference.md` during
   implementation, only after the design closes.

## Validation required before implementation-ready

- Accepted and rejected cases for every admitted scalar family.
- Duplicate constants after contextual typing are rejected.
- Out-of-range constants are rejected at the pattern.
- Open integer domains require `else`.
- Bool behavior remains unchanged.
- Float and text scrutinees remain rejected unless this RFC explicitly admits
  them before implementation.
- Existing match programs and snippet hashes remain unchanged.

## Non-goals

- Pattern alternation, guards, ranges, destructuring, or unqualified ADT
  variants.
- Changing type-mode match.
- General compile-time evaluation.
