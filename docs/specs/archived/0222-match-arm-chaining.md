# RFC 0222: Match Arm Chaining

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented
- Created: 2026-09-20
- Origin: found while implementing RFC 0135; recorded in `docs/status.md`
- Coordinates with: `docs/reference.md`

## Summary

The generator lowers a `match` expression to statement-level control flow by
emitting one plain `if` per explicit arm followed by a trailing `else`. When two
or more explicit arms precede an `else`, the `else` binds to the last `if`, so an
earlier matching arm's result is overwritten by the `else` result. This RFC
chains the explicit arms with `else if` so the first matching arm wins, which is
the semantics `reference.md` already states.

This is a generator defect, not a language change: `reference.md` says arms run
in source order.

## Motivation

`reference.md` states "Arms run in source order." The current lowering violates
it. For:

```hexal
type Shape is union | A as x: Int32 end | B as y: Int32 end | C as z: Int32 end end
fun f(s: Shape): Int32 do
    return match s is
    | Shape.A then 1
    | Shape.B then 2
    | else then 3
    end
end
```

the generator emits:

```c
if (scrutinee.tag == hex_tag_A) { result = 1; }
if (scrutinee.tag == hex_tag_B) { result = 2; }
else { result = 3; }
```

For tag `A`, the first `if` sets `1`, then the second `if` is false and its `else`
sets `3`, so `f` returns `3` instead of `1`. A C probe of the exact pattern
confirms `t=0` yields `3`. The same shape affects value-mode union, type-mode,
`ErrorKind`, and every multi-arm match with an `else`.

## Design

The lowering changes only when the match has a final `else`:

- With no `else` arm, every explicit arm stays a separate plain `if`. A closed
  domain's arms are mutually exclusive, so the existing text is already correct
  and remains byte-identical.
- With an `else` arm, the explicit arms chain as
  `if (cond0) { result = v0; } else if (cond1) { result = v1; } ... else { result = vElse; }`.

The `else` arm remains the final `else`. The scrutinee is still evaluated once
into a temporary. Arm order, `#line` mapping, and result typing are unchanged.

Scalar value-mode arms (RFC 0135) already chain with `else if` and are
unaffected.

## Validation

1. A type-mode ADT match with two or more variants and an `else` returns the
   first matching arm's result at runtime.
2. A type-mode union match with two or more members and an `else` returns the
   first matching arm's result.
3. An `ErrorKind` match with two or more explicit variants and an `else` returns
   the first matching arm's result.
4. A match with no `else` arm generates byte-identical C to before.
5. A match with one explicit arm and an `else` generates byte-identical C to
   before.
6. Scalar value-mode matches (RFC 0135) are unchanged.
7. The workbench snippet manifest is rebuilt and the changed artifacts reviewed.

## C23 switch decision

RFC 0135 Open Question 1 asked whether a C23 `switch` lowering is worth a
second generator path. A clang `-O2` benchmark of the ordered if-chain against
a hand-written `switch` over 8, 32, and 256 integer arms measured 1.50/1.49,
1.49/1.48, and 1.51/1.49 ns per call: clang already lowers the chain to
equivalent dispatch. The ordered lowering is within noise, so one path is kept
and no `switch` lowering is added.

## Non-goals

- Changing match syntax, pattern grammar, or exhaustiveness rules.
- Changing the generated C for matches without an `else`.
- A C23 `switch` lowering (RFC 0135 Open Question 1).
