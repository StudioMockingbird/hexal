# RFC 0094: Anonymous Function Literals

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-08-19
- Updated: 2026-08-19
- Scope: add non-capturing anonymous function literals that produce concrete `Fun<...>` values
- Depends on: implemented RFC 0093, the existing `Fun<...>` rules, and `docs/reference.md`
- Coordinates with: RFC 0112 (order-independent function visibility), lexer,
  parser, checker, generator, integration tests, workbench snippets, the
  snippet manifest, and `docs/status.md`
- Does not change: function-pointer representation, source-order visibility,
  or the no-closures rule

## Summary

Add non-capturing anonymous function expressions:

```hexal
fun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32 do
    return callback(value)
end

result := apply(fun (value: Int32): Int32 do
    return value * value
end, 5)
```

Anonymous and named functions share one parameter, result, body, return-flow, closed-scope, call, generic-specialization, and C-function contract. A named declaration is declaration sugar over the same function form:

```hexal
fun square(value: Int32): Int32 do
    return value * value
end
```

The declaration layer additionally owns `export` and the named spelling. A
named function remains code rather than mutable storage, but its name is a
first-class non-capturing function value when used in value position. Its
declaration name and the receiving name of a direct fixed function-literal
binding are both bound before the body and receive the same scope,
source-order visibility, and direct self-recursion semantics.

A fixed declaration whose initializer is directly a function literal is semantically a function declaration, not executable data initialization. It emits the helper function and no function-pointer storage. At module scope it is therefore valid in an imported declaration-only module. It remains private because `export` prefixes the named function-declaration form; use `export fun name(...)` for an exported function. A mutable binding or a binding initialized from an existing function value remains ordinary runtime data and does not gain declaration semantics.

`Fun<...>` is an ordinary non-capturing function-pointer value. Named
functions can be used in value position, and anonymous literals can be stored,
passed, returned, and placed in copyable aggregates. No current built-in
requires a callback; callback collection operations are not part of this RFC.

## Syntax

Add this normative production and include it in `primary-expression`:

```ebnf
anonymous-function-literal = "fun" , [ generic-parameter-list ]
                             , signature , "do" , block , "end" ;
primary-expression = anonymous-function-literal | ? existing alternatives ? ;
```

Forms:

```hexal
fun (p1: T1, p2: T2): R do
    body
end

fun (p1: T1, p2: T2) do
    body
end
```

- The first form has type `Fun<(T1, T2) : R>`.
- The second has type `Fun<(T1, T2)>` and returns no value.
- Parameters always have explicit types and use the named-function parameter rules.
- Omitting the result clause means no result. A value-returning `return` is invalid.
- The literal is a postfix base. Member and index suffixes remain invalid by type; a same-line call suffix invokes the literal directly.
- A call expression beginning with a parenthesized or anonymous function may be a call statement. This permits direct no-result invocation.
- A literal's explicit signature determines its concrete `Fun<...>` type; it does not require an expected type.
- A generic literal may declare type parameters between `fun` and its signature.
- A literal declares no source name and cannot use `export`.
- Nested anonymous literals are valid expressions.

`fun` followed by an identifier in module declaration position remains a named function declaration. In an expression position, `fun` followed by `(` starts a concrete anonymous literal and `fun` followed by `<` starts a generic anonymous literal. No other token may follow expression-position `fun`.

`<` immediately after the reserved word `fun` is unambiguously the opening generic-parameter delimiter. The identifier-only generic/operator lookahead rule does not apply: no value expression can end with the `fun` token, so `<` cannot be a relational operator there.

## Shared function semantics

Named and anonymous functions use the same implementation for:

- signature resolution and position eligibility;
- parameter binding and immutability;
- fresh closed function scope;
- statement and control-flow checking;
- `return`, fall-through, `defer`, and `errdefer` behavior;
- call arity, argument assignability, and result typing;
- function-pointer representation;
- body rendering and `#line` source mapping.

Do not add a separate analyzer, capture-analysis pass, or anonymous-only body checker. Check a literal body in the same fresh function scope used by a named function. Existing lookup behavior then rejects captures.

Only the named declaration layer adds export and the named spelling. Generic parameters, binding scope, source-order visibility, and direct self-recursion belong to the shared function form.

A direct fixed function-literal binding has the visibility of its declaration position:

- At module scope it is a function binding visible to following module statements and later function bodies, exactly like the equivalent named function declaration.
- In a function body it is a local function binding visible only from that declaration onward in the containing lexical scope.
- Its own body receives a self binding for that function only. It does not receive the containing lexical scope, so other outer locals remain captures and are invalid.

An otherwise anonymous literal gains a recursion name only when it is the direct initializer of a fixed binding:

```hexal
factorial := fun (value: Int32): Int32 do
    if value == 0 then
        return 1
    end
    return value * factorial(value - 1)
end
```

The checker binds `factorial` before checking the body and recursive calls lower directly to the generated helper. At module scope, later function bodies may call `factorial`; at local scope, only following statements in that lexical scope may call it. A literal passed or invoked directly has no recursion name. A mutable receiving binding is runtime-replaceable and cannot be a stable self identity; referring to it from the literal remains an invalid capture.

## Closed scope and captures

Anonymous functions capture no runtime environment. Their bodies may use their own parameters, bindings declared inside their bodies, named functions visible at the literal's source position, and function values received or declared inside the body.

They cannot use an enclosing root binding, local binding, parameter, `self`, or function-value binding, except for the direct fixed receiving binding used as the literal's recursion name.

```hexal
fun calculate(factor: Int32): Int32 do
    operation := fun (value: Int32): Int32 do
        return value * factor
    end
    return operation(2)
end
```

The example is rejected because `factor` belongs to the enclosing function. No environment object, environment pointer, heap allocation, or capture ABI is generated.

## Function-value rules

Anonymous functions use the existing `Fun<...>` position matrix:

- Valid: binding, function parameter, function result, parameter inside another
  `Fun`, object member, ADT payload, Array element, View element, List element,
  Dict value, Task argument or result, Channel element, and union member.
- `Fun<...>` remains invalid as a `Ptr`/`MutPtr` pointee and `ref` target. A
  function value is copied as a function pointer; it never becomes an owned
  allocation and cannot be used as an allocator element requiring destruction.
- A named function identifier in value position produces its exact `Fun<...>`
  value. A direct call remains a call, so no address-taking operator is needed.
- Calls require exact arity and assignable arguments. No-result calls are statements only.
- Function values have no equality, ordering, member access, or index operation.

A literal may inject into a union containing its exact `Fun<...>` type. A nullable function value must be narrowed before calling it, as today.

## Generics

Anonymous and named functions use the same generic parameter, inference, explicit-argument, specialization, recursion, and position rules. The named form is declaration sugar:

```hexal
fun identity<T>(value: T): T do
    return value
end
```

for:

```hexal
identity := fun<T>(value: T): T do
    return value
end
```

An unspecialized generic literal is a compile-time function template, not a runtime `Fun<...>` pointer. A direct inferred declaration whose initializer is that literal creates the same open generic-function binding used by a named generic declaration. This is the named-function sugar boundary, not an ordinary runtime value binding. It is fixed and non-assignable and emits no C storage. Calls infer or accept explicit type arguments exactly like named generic calls:

```hexal
integer := identity(10)
decimal := identity(1.5)
explicit := identity<Int32>(20)
```

The following are alternative spellings that create the same kind of open generic-function binding and have the same call, reference, specialization, and recursion behavior:

```hexal
-- Named spelling.
fun identity<T>(value: T): T do
    return value
end
```

```hexal
-- Equivalent anonymous spelling.
identity := fun<T>(value: T): T do
    return value
end
```

This equivalence does not introduce generic-template copying. Reading an unspecialized generic function without an exact expected `Fun<...>` type remains invalid, so `alias := identity` still reports `cannot infer generic parameter for identity`. Only a direct generic-literal initializer declares an open template binding.

Every open generic function has a compiler-owned template identity distinct from its source binding name. Scope bindings refer to that identity; specialization caches and recursion guards key by it. Module-level and local generic literals can therefore reuse the same source name in disjoint scopes without sharing specializations or colliding. Named generic declarations use the same identity path; do not retain a second source-name-only registry path.

An exact expected `Fun<...>` type specializes a generic literal to one concrete function pointer:

```hexal
integer_identity: Fun<(Int32) : Int32> := fun<T>(value: T): T do
    return value
end
```

The same contextual specialization applies when a generic literal is passed directly to a parameter expecting an exact `Fun<...>` type. A concretely specialized result is an ordinary function value and follows the existing fixed or mutable binding rules.

A generic literal may also be invoked directly. Its arguments and expected result participate in the existing generic inference rules.

A literal inside a generic named function or method may use an enclosing type parameter in its signature. It is checked again for every reachable concrete specialization and produces one concrete helper per specialization:

```hexal
fun apply<T>(value: T): T do
    operation := fun (input: T): T do
        return input
    end
    return operation(value)
end
```

The generated helper identities must differ across concrete enclosing specializations. Type parameters are compile-time names, not runtime captures.

An anonymous generic body may use its own type parameters and enclosing type parameters. Neither is a runtime capture. Existing argument-changing recursive-specialization rejection applies unchanged.

## Checked representation

- Add an explicit anonymous-function expression node.
- The node owns resolved parameters, optional result, checked body, deferred actions, concrete `Fun<...>` type, source range, and generated helper identity.
- Reuse the checked function body representation shared with named functions; do not encode a literal as a source declaration or ordinary value binding.
- Nested literals remain explicit child expressions and are discovered recursively.
- Generic templates retain the source literal. Each reachable concrete enclosing specialization receives its own checked concrete literal node.
- A direct fixed literal declaration creates a function binding at the declaration's module or local scope. Its checked body receives only the function's self binding in addition to ordinary closed-function names.
- Classify a module-level direct fixed literal declaration as declaration-only. Emit no initializer statement or function-pointer object; imported modules may contain it. Keep mutable function bindings and bindings initialized from existing function values as ordinary data.
- Generalize the existing open-generic function binding to carry a compiler-owned template identity rather than relying on its source name as identity. Named and anonymous generic functions share that representation, specialization engine, and cache model.
- Do not introduce a runtime template representation or a second generic engine.

## Generated C

- Lower every reachable concrete literal to one file-scope `static` C function with the ordinary function ABI.
- Lower the literal expression directly to that function pointer.
- Assign identities with the existing deterministic counter convention, not source coordinates. Use module-local checked-tree preorder and `hex_fun_<ordinal>`.
- Traverse ordinary module statements in source order, then concrete specializations in their existing deterministic order. Traverse nested literal bodies in preorder.
- A literal repeated through generic specialization receives a separate ordinal and concrete C signature.
- Emit static prototypes for all anonymous helpers before ordinary function definitions.
- Emit ordinary functions and concrete specializations in their existing order, then anonymous helper definitions, then the root `main` body. Prototypes let an enclosing function reference its helper; placing helper definitions afterward lets a helper call its enclosing named function.
- Emit nested helpers exactly once and definitions in ordinal order.
- Preserve literal signature and body locations through `#line` directives.
- Emit no closure structure, environment parameter, allocation, dispatcher, or delegating wrapper.

Whitespace changes that preserve checked-tree order do not rename helpers. Inserting or removing an earlier literal may renumber later helpers; that is permitted and deterministic.

## Diagnostics

Diagnostics remain owned by the earliest proving phase:

- Expression-position `fun` not followed by `(` or `<`: Syntax Error, `anonymous function requires '(' or '<' after 'fun'`.
- `<` after expression-position `fun` must contain a valid generic-parameter list and must be followed by the literal signature. Reuse the named generic-parameter and signature diagnostics.
- Missing parameter type: reuse `function parameters require type annotations`.
- Missing `do`, malformed result type/body, invalid `return`, and result fall-through: reuse named-function diagnostics.
- Enclosing local or parameter reference: ordinary unknown-variable diagnostic.
- Root data or root Fun binding reference: existing closed-function diagnostic, `function <generated-name> cannot access module data binding <name>; pass it as a parameter`.
- `self` reference: existing self-not-bound diagnostic.
- Forbidden position, incompatible signature, call arity, and argument type: reuse existing `Fun<...>` diagnostics.

Diagnostics may display `anonymous function` instead of the generated C name. Generated helper names are not source names and must not be suggested as callable identifiers.

## Migration

1. Finish and commit RFC 0093 before establishing the 0094 baseline.
2. Add the grammar and parser disambiguation above.
3. Introduce the checked literal node and share named-function signature/body checking.
4. Check every literal in a fresh closed function scope; add no capture pass.
5. Discover nested and specialized literals in the deterministic order above.
6. Give direct fixed literal bindings their declaration-position scope and seed their checked bodies with only their self binding.
7. Classify a direct fixed literal declaration as function code rather than runtime data; apply declaration-only imported-module validation and omit pointer storage.
8. Generalize open generic-function registration so scope bindings, caches, and recursion guards use compiler-owned template identities. Route named and anonymous generics through that one path.
9. Emit prototypes, definitions, function pointers, and source mappings as specified.
10. Replace `emitModulePair`'s CARE comment claiming that no prototype region is needed. State the new contract: anonymous-helper prototypes precede ordinary definitions; function source visibility remains checker-owned and unchanged.
11. Update `docs/reference.md` after behavior stabilizes: grammar, named-function sugar, closed scope, module classification, ordinary function-value placement, generics, and generated-C contract.
12. Add the four positive workbench snippets required by Validation.
13. Record the post-0093 snippet manifest before implementation. Existing entries must not change; add entries only for new 0094 snippets.

## Non-goals

- Lexical closures, captured values, or closure environments.
- Runtime function allocation or environment management.
- Built-in map/filter/fold/sort callback APIs.
- Async functions, coroutines, overloading, default arguments, named arguments, or variadic parameters.

## Validation

This section is exhaustive. RFC 0094 is complete only when every item below passes:

- Named functions retain source-order visibility, named self-recursion, export,
  generic specialization, and non-assignability. Their identifiers produce
  non-capturing `Fun<...>` values in value position.
- Stored, directly passed, no-result, multi-parameter, mutable-reassigned, and nested anonymous literals compile with exact `Fun<...>` types.
- Result-producing and no-result literals can be invoked directly. A fixed binding containing a literal cannot be reassigned.
- Literal parameters are fixed; missing parameter types, malformed result forms, invalid returns, fall-through results, wrong call arity, incompatible signatures, and incompatible arguments are rejected with the specified diagnostics.
- A direct fixed function-literal binding supports self-recursion and lowers recursive calls directly to its helper. Enclosing local, enclosing parameter, root data, root Fun binding, `self`, and mutable receiving-binding references are rejected without a capture environment.
- A module-level fixed literal binding is callable from a later function body; an earlier function body cannot see it. The equivalent named declaration has identical visibility and diagnostics.
- A module-level direct fixed literal declaration is accepted in an imported declaration-only module, emits no module-initializer statement or function-pointer storage, and remains private. `export` remains valid only on the equivalent named function form.
- A local fixed literal binding is callable only by following statements in its lexical scope. Its body sees itself but cannot see any other binding from the containing function scope.
- A mutable literal binding and a fixed binding initialized from an existing function value remain runtime data; they do not gain module function visibility or declaration-only classification.
- A literal can call a visible named function, its own Fun parameter, and a Fun binding declared inside its own body.
- Literals and named function values inject into `Fun<...> | Nil`; calling the
  nullable value still requires narrowing.
- Literal and named-function values are accepted as function results, object
  members, ADT payloads, Array/View/List elements, Dict values, Task arguments
  and results, Channel elements, and union members. They remain rejected as
  Dict keys (function values have no equality/hash contract), Ptr/MutPtr
  pointees, heap-allocation types, and `ref` targets.
- Function values in every accepted position lower to ordinary C function
  pointers; no environment object, ownership operation, or allocation is
  emitted.
- A generic named function containing one literal is instantiated with two concrete types; helpers have distinct identities and correct signatures. Named generic function values retain current behavior.
- `identity := fun<T>(value: T): T do ... end` creates a fixed,
  non-assignable template binding and emits no C storage before
  specialization.
- The named and anonymous spellings of the same generic function produce equivalent open-template behavior; `alias := identity` remains rejected without an exact expected `Fun<...>` type.
- Two local generic literal bindings with the same source name in disjoint scopes have distinct template identities, specialization caches, generated helpers, and diagnostics.
- Generic literals support inferred calls, explicit type-argument calls, expected-result inference, and direct invocation.
- Assigning a generic literal to an exact `Fun<...>` binding and passing one directly to an exact `Fun<...>` parameter each emit the required concrete specialization.
- A mutable binding with an exact `Fun<...>` type may hold a concretely specialized generic literal; an unspecialized generic template cannot be mutable.
- Generic literals diagnose duplicate or unresolved type parameters, conflicting inference, invalid explicit arguments, and argument-changing recursive specialization exactly like named generic functions.
- Two identical compilations produce byte-identical files. Distinct literals and concrete specializations receive distinct deterministic helper names.
- Generated C text verifies: prototypes precede uses; definitions are file-scope `static`; an enclosing named function and its helper may call each other; nested helpers emit once; direct function pointers are used; and no environment object, environment parameter, allocation, dispatcher, or delegating wrapper is emitted. The updated `emitModulePair` CARE comment describes this prototype contract accurately.
- Literal helper signatures and bodies retain correct `#line` mapping.
- Four workbench snippets cover stored, directly passed, no-result, and multi-parameter literals through the public compiler API.
- Against the recorded post-0093 manifest, no existing artifact hash changes; only the four new snippets add entries.
- `docs/reference.md` contains final grammar and semantic/C contracts.
- `go test ./compiler/...`, `go test ./workbench/snippets`, and `go test ./...` pass without an external toolchain.

## Handoff

After testable acceptance conditions pass, rebuild the workbench into `bin/` and restart it. This is an operational handoff step, not a language acceptance test.
