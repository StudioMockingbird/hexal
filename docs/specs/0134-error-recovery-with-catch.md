# RFC 0134: Error Recovery with `catch`

- Kind: Language Semantics
- Status: Implementation-ready; implementation not started
- Created: 2026-08-27
- Updated: 2026-08-27
- Extends: the existing `T | Error` result and `try` propagation contract
- Does not change: `try`, `errdefer`, `Error`, sum-type representation, or
  error propagation

## Summary

Add a `catch` expression for recovering from an `Error` result with a local
fallback value.

```hexal
fun read_count(): Int32 | Error do
    return Error.new("Read Error", "bad")
end

fun run(): Int32 do
    count: Int32 := read_count() catch 0
    return count
end
```

`try` remains the operation for propagating an error to the enclosing
function. `catch` consumes the error and supplies a success value instead.

## Motivation

The current `try` contract handles the common propagation path:

```hexal
count: Int32 := try read_count()
```

Callers that want a local default currently need to write explicit branching
or match logic. A small expression operator provides the common fallback case
without introducing a second error type system or changing the existing
sum-type model.

This proposal intentionally does not add typed error sets, a new error-union
type, error payload binding, or catch blocks. Those would be separate language
decisions.

## Grammar

Add `catch` as a reserved word and as a binary-tail operator:

```ebnf
binary-tail = binary-operator , unary-expression
            | "is" , type-expression
            | "catch" , unary-expression ;
```

The existing no-mixed-binary-chain rule applies. A fallback expression that
also uses another operator must be parenthesized when required by that rule:

```hexal
value: Int32 := read_count() catch (base + 1)
```

`catch` is an expression form. It is not a standalone statement. Its result
must be used in a declaration, assignment, return, argument, or another
expression context supported by the existing grammar.

## Semantics

For `left catch fallback`:

1. Type-check `left` as an expression.
2. Require `left` to be a union containing exactly one `Error` member and at
   least one non-Error success member, using the same validity rule as `try`.
3. Define the result type as `left` with its `Error` member removed. If more
   than one success member remains, the result remains that success union.
4. Type-check `fallback` against the result type. It must be assignable to
   that type and must not itself produce an `Error` member.
5. Evaluate `left` exactly once.
6. If `left` contains a success member, yield that success value or success
   union and do not evaluate `fallback`.
7. If `left` contains `Error`, discard that error and evaluate `fallback`,
   yielding its value.

The enclosing function does not need to return `Error`, because `catch`
handles both result paths locally:

```hexal
fun read_count(): Int32 | Error do
    return Error.new("Read Error", "bad")
end

fun with_default(): Int32 do
    return read_count() catch 42
end
```

`catch` does not catch traps, compiler diagnostics, or errors raised by code
outside its left operand. It only handles an `Error` member produced by the
left operand.

The fallback has no access to the discarded `Error`. Code that needs to
inspect or transform the error must continue to use the existing union and
match facilities.

`try` is unchanged:

```hexal
fun propagate(): Int32 | Error do
    count: Int32 := try read_count()
    return count
end
```

`try` propagates `Error`; `catch` consumes `Error` and produces a value.

## Rejection rules

The checker rejects `catch` when:

- the left operand is not a union;
- the left operand has no `Error` member;
- the left operand has more than one `Error` member;
- the left operand has no success member;
- the fallback is not assignable to the normalized success type; or
- the fallback can produce `Error`.

The diagnostic for an invalid left operand should mirror the existing `try`
diagnostic and identify that `catch` requires a union containing `Error` and a
success member. The fallback diagnostic should identify the fallback and the
required success type.

`catch` is valid at root scope and inside cleanup actions because it does not
propagate an error. `try` retains its existing restrictions in those contexts.

## Lowering contract

The generator must:

- evaluate the left operand once into the same physical union representation
  used by `try`;
- test the union tag against the checked `Error` member identity;
- render the success path without evaluating the fallback;
- render the fallback only on the `Error` path; and
- produce no hidden error return, cleanup registration, or second evaluation.

The generated C must preserve the source evaluation order of the left operand
and fallback expression.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` in one change to:

- add `catch` to the reserved-word list;
- add the final `catch` grammar production and its chain rule;
- define the operand, fallback, evaluation, and result-type rules;
- record that `catch` is allowed at root scope and in cleanup actions; and
- record that `try` remains propagation-only.

No change to `Error` fields, union layout, `try`, `errdefer`, or generated
component ownership is required.

## Validation

The implementation is complete only when all of these cases are covered by
focused integration or generator-text tests:

1. A successful `T | Error` left operand yields `T` and does not evaluate the
   fallback.
2. An `Error` left operand evaluates the fallback and yields its value.
3. The left operand is evaluated exactly once.
4. A `catch` expression can be used in a declaration, assignment, return, and
   call argument.
5. `catch` works in a function whose result does not accept `Error`.
6. `catch` works at root scope and inside both `defer` and `errdefer` actions.
7. A union with multiple success members preserves that success union and
   accepts a compatible fallback.
8. A non-union left operand is rejected.
9. A union without `Error` is rejected.
10. A union with more than one `Error` member is rejected.
11. A union containing only `Error` is rejected.
12. An incompatible fallback is rejected.
13. A fallback that can produce `Error` is rejected.
14. Parenthesized fallback expressions obey the existing binary-chain rule.
15. A standalone `catch` statement is rejected because `catch` is expression
   syntax only.
16. Existing `try` propagation, `errdefer`, and all catalog snippets retain
   their prior behavior and generated-artifact manifest hashes.

## Non-goals and future work

- No Zig-style `!T` or explicit error-set type.
- No error binding such as `catch |err| ...`.
- No fallback block syntax.
- No error transformation or logging primitive.
- No change to the representation or lifetime rules of `Error`.
