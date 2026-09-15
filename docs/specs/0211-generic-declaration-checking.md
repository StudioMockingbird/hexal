# RFC 0211: Generic Declaration-Time Checking

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-15
- Updated: 2026-09-15
- Scope: check every open generic body once at its declaration, rejecting every
  error that holds for all possible type arguments, whether or not the generic
  is ever specialized
- Depends on: the current generic, checker, and diagnostic contracts in
  `docs/reference.md`
- Coordinates with: RFC 0190, which specializes imported generics in their
  defining module and relies on this RFC for declaration-time diagnostics
- Does not add: syntax, constraints/bounds, a new type kind, an analyzer pass,
  or any change to generated C

## Problem

The reference states: "Bodies are checked structurally at declaration and
rechecked after substitution." The compiler performs only the second check.
Verified: each of these compiles without a diagnostic when never specialized:

```hexal
fun bad<T>(value: T): T do
    return nope                    -- unknown name
end

fun wrong<T>(value: T): T do
    x: Int32 := "text"             -- type error independent of T
    return value
end
```

A broken generic in a library therefore compiles until some later program
requests it, which contradicts "if it compiles, it runs" and makes a source
standard library (RFC 0186) ship latent errors.

## Rule

An open generic body is checked once, in its defining module, with each type
parameter bound to its existing type-parameter type.

An expression, statement, or declaration is **type-parameter dependent** when a
type involved in its check contains an open type parameter, directly or through
a constructed type (`List<T>`, `Ptr<T>`, `T | Nil`, `Box<T>`, `Fun<(T) : T>`).

- Every check whose operands and expected types are all independent runs at
  declaration and reports its ordinary diagnostic there.
- Every check involving a dependent type is deferred to specialization, where
  it already runs after substitution.
- A check is never run at declaration if some substitution could make it
  succeed. Declaration-time checking rejects only programs that are invalid for
  every possible type argument.

### Checked at declaration

| Category | Examples rejected at declaration |
| --- | --- |
| Name resolution | unknown variable, function, type, module alias, or member of an independent type |
| Independent typing | `x: Int32 := "text"`; calling `f(Int32)` with a String |
| Arity and argument count | wrong argument count to any callee, including a generic callee |
| Control flow | missing return on a continuing path, misplaced `break`/`continue`, `try` or `errdefer` in a function without an Error result |
| Mutability of independent places | assignment to a fixed binding |
| Generic parameter use | undeclared type parameter, duplicate parameter, wrong explicit type-argument count |
| Local cleanup facts on independent values | locally proved use after free of an independent allocation |

### Deferred to specialization

| Category | Example accepted at declaration |
| --- | --- |
| Operators on dependent operands | `value + value` |
| Member access or method call on a dependent receiver | `value.x`, `value.len()` |
| Assignment, initialization, return, or argument passing between a dependent and a different type | `x: Int32 := value` |
| Equality, ordering, printing, hashing, or placement eligibility of a dependent type | `value == other`, `print(value)`, `List<T>` element rules |
| Conversion and widening involving a dependent type | `Int64(value)` |
| Nested generic specialization with dependent arguments | `inner<T>(value)` |

Identity between the same open parameter is independent: `return value` where
`value: T` and the result is `T` is checked at declaration and accepted.

## Scope of declarations

The rule applies to every open generic body:

- module-level generic functions;
- generic methods, including methods of generic types;
- generic anonymous function literals and module-level inferred fixed generic
  literals; and
- exported generics, checked in their defining module whether or not any
  importer requests them.

- generic type layouts: member, payload, and alias types are resolved at
  declaration, so `type Box<T> is struct item: Nope end` is rejected even when
  `Box` is never specialized. Layout checks involving `T` (placement and
  eligibility of `T` itself) remain deferred. Verified: that declaration
  currently compiles without a diagnostic.

## Diagnostics

- Diagnostics use the existing exact messages and the defining module's logical
  key and coordinates.
- An error reported at declaration is not reported again by a later
  specialization of the same body.
- Specialization continues to report dependent errors at the requesting
  specialization, anchored as today.
- Compilation still stops at the first failing phase; no generated C is
  produced for a program with a declaration-time generic error.

## Required sweep

- checker generic registration, which currently records bodies without checking
  them;
- a shared dependency predicate built on the existing
  `compilerTypes.ContainsTypeParameter`, used at each checker site that must
  defer; no new type kind or parallel checker;
- specialization re-check, so declaration-time diagnostics are not duplicated;
- existing tests, snippets, and fixtures containing never-specialized invalid
  generic bodies, which become failures and must be corrected or removed;
- reference text "checked structurally at declaration", which is refined to this
  RFC's dependency rule after explicit user approval.

Do not introduce an analyzer pass. Declaration checking runs inside the checker
beside the specialization logic it complements.

## Detailed implementation plan

### Phase 1: baseline

1. Add failing tests for the two Problem programs and one example per
   declaration-time category.
2. Add passing tests for one example per deferred category, unspecialized.
3. Inventory existing tests, snippets, and fixtures with invalid unspecialized
   generic bodies.

### Phase 2: dependency predicate

1. Add one checker helper deciding whether a check's types are dependent, using
   `ContainsTypeParameter` over every operand and expected type.
2. Route each deferred category's checker site through that helper: when
   dependent, record nothing and continue; otherwise run the existing check.

### Phase 3: declaration checking

1. Check each open generic body at registration with parameters bound to their
   type-parameter types, in the defining module's environment.
2. Record that the template passed declaration checking; skip re-reporting
   independent diagnostics during specialization.
3. Keep specialization's full concrete check for dependent operations.

### Phase 4: conformance

1. Implement every Validation item.
2. Fix or remove every inventoried invalid body; regenerate the snippet manifest
   only if a snippet's source had to change, and review the diff.
3. Run ordinary and tagged C23 suites.
4. Synchronize the reference's generic-checking sentence only with explicit user
   approval.

## Validation

This list is exhaustive:

- `bad<T>` and `wrong<T>` above are rejected at declaration with their existing
  messages and defining-module coordinates, without any specialization;
- one unspecialized example per declaration-time category is rejected;
- one unspecialized example per deferred category is accepted, and the same body
  specialized with an invalid argument (`add<P>` with a struct `P`) reports the
  existing concrete diagnostic;
- `return value` for `value: T` and result `T` is accepted at declaration;
- generic methods, methods of generic types, generic anonymous literals, and
  module-level inferred generic literals follow the rule;
- an unspecialized generic type whose layout names an unknown type is rejected
  at declaration, while a layout using `T` in a position valid for some `T` is
  accepted;
- an exported, never-imported generic with an independent error is rejected in
  its defining module;
- an independent error is reported once, not again per specialization;
- valid generics, local and imported, produce byte-identical generated C and
  unchanged snippet hashes; and
- ordinary and tagged C23 suites pass.

## Open questions

None.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. After behavior stabilizes and
with explicit user approval, replace "Bodies are checked structurally at
declaration" with the dependency rule above.
