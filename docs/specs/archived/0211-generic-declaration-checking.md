# RFC 0211: Generic Declaration-Time Checking

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented. A post-signature-collection pass structurally checks
  every open generic function, method, alias, object, and ADT with its
  parameters bound to placeholders, using condition-specific deferral and a
  diagnostic-only state transaction. Every Validation item is mapped to a
  focused test, and the snippet SHA-256 manifest is byte-identical.
- Created: 2026-09-15
- Updated: 2026-09-16
- Scope: check every open generic body once at its declaration, rejecting every
  error that holds for all possible type arguments, whether or not the generic
  is ever specialized
- Depends on: the current generic, checker, and diagnostic contracts in
  `docs/reference.md`
- Coordinates with: RFC 0190's defining-module specialization behavior; this
  RFC supplies the declaration-time diagnostics that behavior now requires
- Does not add: syntax, constraints/bounds, a new language type, an analyzer
  pass, or any change to generated C

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
parameter bound to its existing type-parameter placeholder. The check runs
after the module's complete signature-collection pass, not while the
declaration is being registered. This preserves order-independent visibility
of module-level functions and methods.

An expression, statement, or declaration is **type-parameter dependent** when a
type involved in its check contains an open type parameter, directly or through
a constructed type (`List<T>`, `Ptr<T>`, `T | Nil`, `Slice<T>`,
`Fun<(T) : T>`).

Each diagnostic condition is classified independently. A condition runs at
declaration when substitution cannot change whether it applies, even if the
expression also carries a dependent value. It is deferred only when some
substitution could make the condition succeed. Declaration-time checking
therefore rejects only programs invalid for every possible type argument.

The checker carries a checker-only dependent fact through intermediate
expressions and type uses. This is metadata, not a language type and not an
analyzer pass. A deferred operation may still check independent subexpressions
inside it; for example, an unknown argument name is reported even when the
receiver's method lookup is deferred.

### Checked at declaration

| Category | Examples rejected at declaration |
| --- | --- |
| Name resolution | unknown variable, function, type, module alias, or member of an independent type |
| Independent typing | `x: Int32 := "text"`; calling `f(Int32)` with a String |
| Arity and argument count | wrong argument count to a callee whose concrete signature is already known |
| Control flow | missing return on a continuing path, misplaced `break`/`continue`, `try` or `errdefer` in a function without an Error result |
| Mutability of places | assignment to a fixed binding, including a binding of type `T` |
| Generic parameter use | undeclared type parameter, duplicate parameter, wrong explicit type-argument count |
| Local cleanup facts | locally proved use after free, double free, or invalid cleanup; these facts do not depend on the pointee's substituted type |

### Deferred to specialization

| Category | Example accepted at declaration |
| --- | --- |
| Operators on dependent operands | `value + value` |
| Member access or method call on a dependent receiver | `value.x`, `value.len()` |
| Assignment, initialization, return, or argument passing between a dependent and a different type | `x: Int32 := value` |
| Equality, ordering, printing, hashing, or placement eligibility of a dependent type | `value == other`, `print(value)`, `List<T>` element rules |
| Conversion and widening involving a dependent type | `Int64(value)` |
| Nested generic specialization with dependent arguments | `inner<T>(value)` |
| Callability and arity of a dependent callee | `callee(1, 2)` where `callee: T` |

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
  declaration, so `type Holder<T> is struct item: Nope end` is rejected even
  when `Holder` is never specialized. Layout checks involving `T` (placement
  and eligibility of `T` itself) remain deferred. Open object and ADT layouts
  use provisional objects/variants whose members contain the existing
  placeholders; they are never emitted or inserted as concrete specializations.
  Pointer-indirected self-reference remains legal, while by-value self-reference
  is rejected by the existing layout rule.

### Declaration-check timing and visibility

- Module-level function and method signatures are collected before any generic
  body is structurally checked. Forward calls, mutual recursion, and calls to
  later methods therefore resolve exactly as they do in non-generic bodies.
- A generic anonymous literal encountered inside an already checked body is
  checked at that expression's normal point, with the complete module
  signature set already available and with the enclosing generic frame merged
  into its own placeholder frame.
- A generic template is registered before its body check so its own name and
  recursive calls resolve, but it is marked invalid if the structural check
  fails. An invalid template is not exported, specialized, or used to satisfy
  another declaration.
- Only reachable source-map modules are checked. “Never specialized” means a
  generic in a reachable module that receives no concrete request; unreachable
  source-map entries remain ignored.

### State isolation

Structural checking is diagnostic-only. It must not change generated output or
specialization demand. The implementation must isolate or roll back:

- binding IDs and compiler-generated function-literal/helper ordinals;
- generic specialization caches and emitted-specialization demand;
- temporary generic-literal registrations and provisional type records; and
- generic frame/open-mode state, including nested generic checks.

A valid program with an unused generic must produce the same artifacts and
generated names as it would without structural checking. A concrete call made
inside an unused template may be validated, but it must not cause that concrete
specialization to be emitted solely because the template was inspected.

## Diagnostics

- Diagnostics use the existing exact messages and the defining module's logical
  key and coordinates.
- A template that fails declaration checking is unavailable for specialization,
  so its independent diagnostic cannot be repeated by a later specialization.
- Valid templates are fully rechecked after substitution; no diagnostic
  suppression flag is used. Dependent errors are reported at the defining
  generic body's source module and coordinates, even when an importer triggered
  the specialization.
- Compilation still stops at the first failing phase; no generated C is
  produced for a program with a declaration-time generic error.

## Required sweep

- checker generic registration, which currently records bodies without checking
  them;
- the existing `compilerTypes.ContainsTypeParameter` as one input to a
  check-specific substitution-dependence decision; it is not a blanket gate;
- checker-only dependent metadata on intermediate expressions/type uses so
  independent subexpressions continue to be checked;
- a post-signature-collection structural-check pass for functions, methods,
  literals, aliases, objects, and ADTs;
- transactional/isolated bookkeeping for IDs, caches, provisional layouts,
  nested generic frames, and specialization demand;
- specialization re-check without suppression; invalid templates are not
  published;
- the existing generic-call, member, indexing, conversion, placement,
  cleanup, equality, ordering, printing, constructor, and control-flow sites,
  each audited for whether its own diagnostic is substitution-dependent;
- existing tests, snippets, and fixtures containing never-specialized invalid
  generic bodies, which become failures and must be corrected or removed;
- reference text "checked structurally at declaration", which already states
  the required behavior and needs no semantic change.

Do not introduce an analyzer pass. Declaration checking runs inside the checker
beside the specialization logic it complements.

## Detailed implementation plan

### Phase 1: baseline and signature schedule

1. Add failing tests for the two Problem programs and one example per
   declaration-time category.
2. Add passing tests for one example per deferred category, unspecialized.
3. Inventory existing tests, snippets, and fixtures with invalid unspecialized
   generic bodies.
4. Preserve the existing module order: register templates and collect every
   module-level signature first; do not check a body from the registration
   callback.

### Phase 2: checker dependency facts

1. Add checker-only dependent metadata to the intermediate expression/type-use
   path; do not add a language type or analyzer package.
2. Define the substitution-dependence decision per diagnostic condition, using
   `ContainsTypeParameter` plus the checker facts that are independent of type
   substitution (name binding, mutability, flow state, control context).
3. Route every audited checker site through its condition-specific decision.
   Deferred operations must still check independent nested operands and names.

### Phase 3: isolated declaration checking

1. After all signatures are collected, check each reachable open generic body
   with its parameters bound to the existing placeholders in the defining
   module's environment.
2. Use a diagnostic-only transaction or equivalent isolated bookkeeping. On
   success, commit only the template-valid fact; on failure, retain diagnostics
   and mark the template unavailable.
3. Build provisional open layouts for generic aliases, objects, and ADTs so
   independent members and unknown names are checked without concrete
   specialization. Never emit or retain those provisional layouts as concrete
   records.
4. Save and restore generic frame/open mode around every nested check and
   specialization request.
5. Keep specialization's full concrete check for dependent operations, with no
   independent-diagnostic suppression path.

### Phase 4: conformance and migration

1. Implement every Validation item, including the state-isolation and
   order-independent-visibility cases.
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
- a call through `callee: T` is accepted unspecialized and reports callability or
  arity only after specialization, while a wrong-arity call to a known function
  is rejected during declaration checking;
- assignment to a fixed `value: T` is rejected during declaration checking;
- `return value` for `value: T` and result `T` is accepted at declaration;
- a forward call and mutual recursion between generic and non-generic
  module-level functions and methods resolve exactly as in ordinary bodies;
- generic methods, methods of generic types, generic anonymous literals, and
  module-level inferred generic literals follow the rule;
- generic aliases, object layouts, ADT payloads, and legal pointer-recursive
  layouts are each checked at declaration with the stated dependent behavior;
- an exported generic in a reachable module that is never specialized is still
  checked; an unreachable source-map module remains ignored;
- a failing template is not exported or specialized, and no independent
  diagnostic is duplicated;
- checking an unused generic does not emit a nested concrete specialization,
  consume a helper/binding ordinal, or change any later generated name;
- nested generic checks restore the enclosing parameter frame and open mode;
- valid generics, local and imported, produce byte-identical generated C and
  unchanged snippet hashes; and
- ordinary and tagged C23 suites pass.

## Open questions

None at the language-contract level. The implementation must choose the
  transaction mechanism (snapshot/restore or isolated checker state), but both
  are required to satisfy the same observable contract in state isolation.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. After behavior stabilizes and
with explicit user approval, replace "Bodies are checked structurally at
declaration" with the dependency rule above.
