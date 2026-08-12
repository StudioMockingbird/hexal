# RFC 0023: Truthiness and Boolean Contexts

- Kind: Language Semantics Specification (ISO/IEC Language Standard Format)
- Status: Implemented
- Features: Crystal-style truthiness, Boolean contexts, and truthiness-aware
  logical operators
- Created: 2026-08-09
- Depends on: RFC 0003 (core scalar types), RFC 0009 (core operators), RFC
  0010 (`Nil` and explicit nullability), RFC 0014 (unions), RFC 0015
  (structured control flow)
- Coordinates with: RFC 0018 (String and Rune values), RFC 0020 (collections),
  and RFC 0022 (algebraic data types and match expressions)

## Summary

Seawitch uses Crystal-style truthiness in Boolean-required contexts:

- `false` is falsey;
- `nil` is falsey; and
- every other value is truthy.

```seawitch
if count              // true even when count is 0
    process(count)
end

if text               // true even when text is empty
    process_text(text)
end

if maybe              // false only when maybe is nil or false
    use(maybe)
end
```

Truthiness is contextual. It does not create an implicit conversion that lets
an integer initialize `Bool` or pass to a parameter declared as `Bool`.

This RFC changes which values are accepted in Boolean contexts. It does not
add syntax, a new runtime type, a user-defined truthiness protocol, or a new
union-narrowing mechanism.

## Falsey and truthy values

The complete falsey set is:

| Value | Truthiness |
|---|---|
| `false` | falsey |
| `nil` | falsey |

Every other valid Seawitch value is truthy, including:

- `true`;
- zero and nonzero integers;
- positive and negative floating values, including zero and `NaN`;
- `Rune` values;
- empty and non-empty `String` values;
- empty and non-empty arrays, slices, lists, and dictionaries;
- objects and ADTs, including unit variants; and
- non-null pointers and function values.

There is no empty-value, zero-value, null-pointer, or collection-length rule
that adds another falsey value. A nullable value such as `T | Nil` is falsey
only when its active member is `Nil`. A non-null pointer type is always
truthy.

Truthiness is shallow. For example, an object containing a zero integer or an
empty collection is still truthy because the object itself is not `false` or
`nil`.

## Boolean contexts

The following contexts require a truthiness-capable expression:

- `if` conditions;
- `elseif` conditions;
- `while` conditions; and
- the operands of `!`, `and`, and `or`.

A truthiness-capable expression is any valid expression that produces a value.
A no-result function call, a type declaration, an invalid checked expression,
or another non-value expression is rejected before truthiness is considered.

```seawitch
if count
    work()
end

flag: Bool = count       // Type Error: truthiness is not conversion
takes_flag(count)        // Type Error: truthiness is not conversion
```

The condition is not a declaration context and does not create a new binding.
All branch and loop bodies are still checked, including bodies whose condition
is statically known to be false.

## Logical operators

`!`, `and`, and `or` use truthiness but retain `Bool` result types from RFC
0009:

```seawitch
not_text: Bool = !text
both: Bool = left and right
either: Bool = left or right
```

- `!value` evaluates the operand's truthiness and returns the opposite `Bool`.
- `left and right` evaluates `right` only when `left` is truthy and returns a
  `Bool` representing the conjunction.
- `left or right` evaluates `right` only when `left` is falsey and returns a
  `Bool` representing the disjunction.

Unlike Ruby or Crystal's operand-returning logical operators, Seawitch's
`and` and `or` return `Bool`. Unlike C, numeric zero is truthy. `&&` and `||`
remain invalid spellings.

Both operands are checked as expressions. Existing RFC 0009 constant
short-circuit rules remain unchanged: a decisive constant left operand may
make the right operand unevaluated for constant diagnostics, but does not
make malformed syntax or an invalid expression generally acceptable.

`!` remains a prefix operator at the RFC 0009 unary precedence level. The
precedence of `!`, `and`, and `or` is unchanged; parentheses remain the way to
make a larger truthiness expression explicit.

## Evaluation and control flow

Truthiness does not change evaluation frequency or sequencing:

- an `if` condition is evaluated once before selecting a branch;
- each `elseif` condition is evaluated once, in source order, until one is
  truthy;
- a `while` condition is evaluated before every iteration;
- `and` and `or` preserve left-to-right short-circuit evaluation; and
- an expression used only because its type is statically always truthy or
  falsey must still be evaluated for its effects exactly once at its normal
  evaluation point.

The checker must type-check every branch body. It may use a statically known
truthiness result for diagnostics such as definite-return analysis only when
that analysis does not suppress an independent syntax or type diagnostic.
Constant branch removal is an optimization, not a semantic requirement.

## Union narrowing

This RFC does not add truthiness-based flow narrowing. Truthiness answers only
whether a value is falsey at runtime. It does not change the static type of a
binding.

Existing explicit narrowing remains unchanged:

```seawitch
value: Int32 | Nil = read_value()

if value != nil
    // Existing explicit Nil narrowing applies here.
    use(value)
end

if value is Int32
    // Existing explicit union-member narrowing applies here.
    use(value)
end
```

In particular, this RFC does not make `if value` expose `Int32` from
`Int32 | Nil`, and it does not define special branch facts for unions
containing `Bool`. A future flow-analysis RFC may add that behavior without
changing the falsey set.

## C23 lowering

The generator must not use C's ordinary numeric condition rules because C
zero is false while Seawitch zero is true.

Truthiness lowering is type-directed:

- `Bool` lowers to its Boolean value;
- `Nil` lowers to false;
- a statically always-truthy expression is evaluated at each required
  evaluation point and then produces a true condition;
- a union containing `Nil` performs the appropriate active-member, tag, or
  null-niche test;
- a union containing `Bool` tests the Boolean payload when `Bool` is active;
  every other non-`Nil` member is truthy; and
- `!`, `and`, and `or` preserve single evaluation and short-circuit behavior.

The generator must not determine truthiness by comparing an integer with zero,
reading a string or collection length, inspecting object padding, or guessing
from a representation field. A representation field may be inspected only
when it is the checked representation of a nullable or tagged value.

When an expression has a statically known truthiness result, lowering may
replace only the condition test with a constant. It must not remove the
expression itself unless a later optimization proves that doing so is safe.

## Diagnostics

Representative diagnostics are:

```text
[Type Error] expression does not produce a value in a Boolean context
[Type Error] truthiness does not convert a value to Bool here
[Type Error] malformed expression in a Boolean context
[Type Error] malformed nullable value cannot be tested for truthiness
```

The parser owns malformed operator and condition syntax. The checker owns
value-production checks, truthiness classification, and Boolean-context
conversion rejection. The generator must reject an unknown truthiness
category as an `Unknown Error`; it must not emit a C condition with different
falsey behavior.

## Compatibility and supersession

This RFC supersedes:

- the exact-`Bool` condition rule proposed by RFC 0015; and
- the Bool-only operand rule for `!`, `and`, and `or` in RFC 0009.

It does not change the result type, precedence, reserved-word status, or
short-circuit guarantees of those logical operators. It does not add implicit
numeric, pointer, string, collection, object, ADT, or function conversions.

RFC 0015's lexical scopes, loop semantics, `break`/`continue` behavior, and
return analysis remain unchanged except that its conditions use this RFC's
truthiness rules.

## Deferred

- Truthiness-based union or nullable flow narrowing.
- User-defined truthiness methods or protocols.
- Recoverable condition-evaluation errors for foreign or invalid values.
- Ruby/Crystal-style operand-returning `and` and `or`.
- Truthiness-dependent pattern guards beyond RFC 0022.
- Implicit truthiness in FFI calls or generated public C interfaces.
- Required constant-branch elimination and other optimization policy.

## Acceptance criteria

Implementation is complete when focused end-to-end tests prove that:

1. `false` and `nil` are falsey;
2. every other supported value, including zero, `NaN`, empty text, empty
   collections, ADTs, and non-null pointers, is truthy;
3. `if`, `elseif`, and `while` accept value-producing expressions;
4. no-result expressions are rejected in Boolean contexts;
5. typed `Bool` assignments and parameters do not accept truthiness as an
   implicit conversion;
6. `!` accepts every supported value and returns `Bool`;
7. `and` and `or` short-circuit truthy and falsey operands while returning
   `Bool`;
8. condition and operand evaluation frequency is preserved, including for
   statically always-truthy expressions;
9. explicit `!= nil` and `is` narrowing behavior remains unchanged;
10. generated C does not accidentally treat numeric zero or empty storage as
    false; and
11. every new truthiness classification and generator case is handled
    explicitly under the fail-closed architecture.

## Implementation handoff

The implementation plan must identify:

1. the replacement of exact-`Bool` condition checks with a shared
   truthiness-context check;
2. the extension of `!`, `and`, and `or` operand checking while retaining
   their `Bool` result types and short-circuit lowering;
3. the type-directed truthiness lowering for `Bool`, `Nil`, nullable unions,
   tagged unions, and statically always-truthy values;
4. preservation of expression evaluation when a condition's truthiness is
   statically known;
5. diagnostics for no-result expressions and invalid Boolean conversions;
6. focused lexer, parser, checker, and generator tests; and
7. end-to-end tests in the facet-named control-flow/operator test files.

Canonical grammar, language, and status documents are updated once behavior
stabilizes.
