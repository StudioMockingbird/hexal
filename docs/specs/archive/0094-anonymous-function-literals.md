# RFC 0094: Function Literals and Local Functions

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-08-19
- Updated: 2026-08-23
- Scope: add non-capturing anonymous function literals, local named functions,
  and complete value-position support for concrete `Fun<...>` pointers
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

Anonymous and named functions share one parameter, result, body, return-flow, closed-scope, call, generic-specialization, and C-function contract. A named declaration is declaration sugar over the same function form at module or local scope:

```hexal
fun square(value: Int32): Int32 do
    return value * value
end
```

The named declaration spelling additionally owns `export` at module scope. A
named function remains code rather than mutable storage, but its name is a
first-class non-capturing function value when used in value position. Its
declaration name and the receiving name of a direct inferred fixed function-literal
binding are both bound before the body and receive the same scope,
source-order visibility, and direct self-recursion semantics.

An inferred fixed declaration (`name := ...`) whose initializer is directly a function literal, after ignoring grouping-only parentheses, is semantically a function declaration rather than executable data initialization. It emits the helper function and no function-pointer storage. At module scope it is therefore valid in an imported declaration-only module. It remains private because `export` prefixes only the module-level named function-declaration form; use `export fun name(...)` for an exported function. A typed declaration, mutable declaration, contextual generic specialization, or binding initialized from an existing function value remains ordinary runtime data and does not gain declaration semantics.

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
- A call expression beginning with a parenthesized or anonymous function is valid only where an expression is expected. It cannot be a call statement. A no-result literal must first receive a name and is then invoked through that identifier.
- A literal's explicit signature determines its concrete `Fun<...>` type; it does not require an expected type.
- A generic literal may declare type parameters between `fun` and its signature.
- A literal declares no source name and cannot use `export`.
- Nested anonymous literals are valid expressions.

`fun` followed by an identifier in module declaration position remains a named function declaration. In an expression position, `fun` followed by `(` starts a concrete anonymous literal and `fun` followed by `<` starts a generic anonymous literal. No other token may follow expression-position `fun`.

`<` immediately after the reserved word `fun` is unambiguously the opening generic-parameter delimiter. The identifier-only generic/operator lookahead rule does not apply: no value expression can end with the `fun` token, so `<` cannot be a relational operator there.

Add local named function declarations to `statement`:

```ebnf
local-function-declaration = "fun" , identifier
                             , [ generic-parameter-list ]
                             , signature , "do" , block , "end" ;
statement = local-function-declaration | ? existing alternatives ? ;
```

- Local named functions use the same syntax as module functions without `export`.
- `export fun` remains module-level only.
- At statement start, `fun` followed by an identifier parses as a local named declaration. `fun` followed by `(` or `<` begins an anonymous expression and is rejected there because anonymous and parenthesized calls cannot be statements.
- The existing `call-statement` production remains restricted to call expressions whose first token is an identifier or `self`.

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

Only the module-level named declaration layer adds export. The named spelling, generic parameters, binding scope, source-order visibility, and direct self-recursion belong to the shared function form.

A direct inferred fixed function-literal binding has the visibility of its declaration position:

- At module scope it is a function binding visible to following module statements and later function bodies, exactly like the equivalent named function declaration.
- In a function body it is a local function binding visible only from that declaration onward in the containing lexical scope, exactly like the equivalent local named function declaration.
- Its own body receives a self binding for that function only. It does not receive the containing lexical scope, so other outer locals remain captures and are invalid.

Local named functions and direct inferred fixed literal bindings are code declarations rather than runtime storage. Either spelling:

- is visible from its declaration onward in the containing lexical block;
- is visible inside later local function bodies in that block;
- is hidden outside that block;
- receives its own self binding before its body is checked;
- cannot capture parameters, `self`, data bindings, or runtime function-value bindings from an enclosing function; and
- emits one file-scope static C helper and no block-local function-pointer object.

A nested block owns a lexical function-binding frame layered over its containing visible functions. Function and data bindings share the ordinary value namespace; same-scope duplicates are rejected and nested shadowing follows the ordinary lexical declaration rules. Local function declarations do not introduce C nested functions.

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

They cannot use an enclosing root binding, local binding, parameter, `self`, or function-value binding, except for the direct inferred fixed receiving binding used as the literal's recursion name.

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

This RFC expands the `Fun<...>` position matrix for named and anonymous function values:

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

All newly admitted positions store or copy one ordinary C function pointer. A function value has no destruction, ownership, or environment operation. Dict keys remain invalid because function values have no equality or hash contract. Direct `Heap.allocate<Fun<...>>`, Ptr/MutPtr pointees, and `ref` remain invalid even though collections may allocate storage containing function-pointer elements.

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

This equivalence does not introduce generic-template copying. Reading an unspecialized generic function without an exact expected `Fun<...>` type remains invalid, so `alias := identity` still reports `cannot infer generic parameter for identity`. Only a direct inferred fixed generic-literal initializer declares an open template binding. A typed initializer contextually specializes the literal and stores the resulting concrete function pointer.

The same equivalence applies inside a function body. A local named generic function and a direct inferred fixed generic-literal binding create the same lexical open-template binding, are visible from their declaration onward, and emit no runtime storage. Later local function bodies in the same lexical block may specialize either spelling. Neither may capture enclosing runtime values.

An inner function's generic parameter names must be distinct from every active enclosing generic parameter. Redeclaring an enclosing name is a duplicate-parameter error rather than lexical shadowing. Enclosing generic parameters remain usable when the inner function does not redeclare them.

Every open generic function has a compiler-owned template identity distinct from its source binding name. Scope bindings refer to that identity; specialization caches and recursion guards key by it. Module-level and local generic literals can therefore reuse the same source name in disjoint scopes without sharing specializations or colliding. Named generic declarations use the same identity path; do not retain a second source-name-only registry path.

An imported module-level generic literal follows the existing imported-generic ownership rule: the defining module's registry owns and emits each requested concrete specialization; the importer owns only its declaration and use. Declaration-only validation does not force an unspecialized template to emit C storage.

An exact expected `Fun<...>` type specializes a generic literal to one concrete function pointer:

```hexal
integer_identity: Fun<(Int32) : Int32> := fun<T>(value: T): T do
    return value
end
```

The same contextual specialization applies when a generic literal is passed directly to a parameter expecting an exact `Fun<...>` type. A concretely specialized result is an ordinary function value and follows the existing fixed or mutable binding rules.

A result-producing generic literal may also be invoked directly inside an expression. Its arguments and expected result participate in the existing generic inference rules. A no-result generic literal must first receive a name.

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
- A direct inferred fixed literal declaration creates a function binding at the declaration's module or local scope. Its checked body receives only the function's self binding in addition to ordinary closed-function names. Strip grouping-only expression nodes before this classification; reject the classification when the declaration has a written type or `mut`.
- Parse and check local named function declarations through the same checked function form and lexical function-binding path. Do not encode them as executable statements or C nested functions.
- A fresh local function body receives module functions visible at its declaration, earlier local function declarations visible through its lexical function namespace, and its own self binding. It receives no enclosing runtime value bindings.
- Classify a module-level direct inferred fixed literal declaration as declaration-only. Emit no initializer statement or function-pointer object; imported modules may contain it. Keep typed declarations, mutable function bindings, contextual generic specializations, and bindings initialized from existing function values as ordinary data.
- Generalize the existing open-generic function binding to carry a compiler-owned template identity rather than relying on its source name as identity. Named and anonymous generic functions share that representation, specialization engine, and cache model.
- Do not introduce a runtime template representation or a second generic engine.

## Generated C

- Lower every reachable concrete literal and local named function to one file-scope `static` C function with the ordinary function ABI.
- Lower the literal expression directly to that function pointer.
- Assign identities with the existing deterministic counter convention, not source coordinates. Local named functions and literals share module-local checked-tree preorder and `hex_fun_<ordinal>`; a local source name is checker metadata and never a module-level C symbol. Module-level named functions retain their existing source-derived C names.
- Traverse ordinary module statements in source order, then concrete specializations in their existing deterministic order. Traverse nested local-function and literal bodies in preorder.
- A literal repeated through generic specialization receives a separate ordinal and concrete C signature.
- Emit static prototypes for all local and anonymous helpers before ordinary function definitions.
- Emit ordinary module functions and concrete specializations in their existing order, then local and anonymous helper definitions, then the root `main` body. Prototypes let an enclosing function reference its helper; placing helper definitions afterward lets a helper call its enclosing named function.
- Emit nested helpers exactly once and definitions in ordinal order.
- Preserve literal signature and body locations through `#line` directives.
- Emit no closure structure, environment parameter, allocation, dispatcher, or delegating wrapper.

Concrete `Fun<...>` values use the C23 `typeof` type specifier wherever C needs a standalone type specifier rather than the complete named declarator already produced by `declaration`. Do not create compiler-owned function-pointer typedef families merely to make C declarators easier:

```c
typeof(int32_t (*)(int32_t)) select(void);

typedef struct {
    typeof(int32_t (*)(int32_t)) callback;
} hex_t_m3_app_Handler;
```

- The spelling is recursive: a parameter or result that is itself `Fun<...>` uses its own `typeof` spelling inside the outer function-pointer type.
- Named and anonymous function definitions remain ordinary C functions. Existing binding, parameter, and union-member declarators may retain their current direct C spelling; do not churn an existing artifact merely to replace an already-correct complete declarator with `typeof`.
- Fixed and mutable binding qualification applies to the pointer value represented by `typeof`, exactly as for the existing direct function-pointer declarator.
- Arrays, Views, Lists, Dict values, objects, ADTs, unions, Tasks, and Channels reuse the same type spelling; no wrapper object or ABI conversion is introduced.
- A generated collection specialization containing `Fun<...>` is module-owned when any parameter or result type in the signature recursively requires a module-owned C declaration. Extend the existing module-ownership walk through `Type.Signature`; builtin-only signatures retain program-component ownership.
- `typeof` is a C23 language feature and selects no header.
- Local named functions and literals share one ordinal stream. Their helpers interleave in checked-tree source preorder; do not maintain separate local-function and literal counters.

Whitespace changes that preserve checked-tree order do not rename helpers. Inserting or removing an earlier literal may renumber later helpers; that is permitted and deterministic.

## Diagnostics

Diagnostics remain owned by the earliest proving phase:

- Expression-position `fun` not followed by `(` or `<`: Syntax Error, `anonymous function requires '(' or '<' after 'fun'`.
- A statement beginning with `fun (` or `fun<`: Syntax Error, `anonymous functions cannot begin statements; bind the function first`.
- A statement beginning with `(` retains the existing statement-start diagnostic; parenthesized calls do not become call statements.
- `<` after expression-position `fun` must contain a valid generic-parameter list and must be followed by the literal signature. Reuse the named generic-parameter and signature diagnostics.
- Missing parameter type: reuse `function parameters require type annotations`.
- Missing `do`, malformed result type/body, invalid `return`, and result fall-through: reuse named-function diagnostics.
- Enclosing local or parameter reference: ordinary unknown-variable diagnostic.
- Root data or root Fun binding reference: existing closed-function diagnostic, `function <generated-name> cannot access module data binding <name>; pass it as a parameter`.
- `self` reference: existing self-not-bound diagnostic.
- Forbidden position, incompatible signature, call arity, and argument type: reuse existing `Fun<...>` diagnostics.
- Equality and print rejection propagate through every aggregate newly allowed to contain `Fun<...>`. Object and ADT diagnostics name the first offending field. Array, View, and List diagnostics name the unsupported element type; Dict print diagnostics name the unsupported value type. Never emit an empty member name.

Diagnostics may display `anonymous function` instead of the generated C name. Generated helper names are not source names and must not be suggested as callable identifiers.

## Required sweep

The expanded `Fun<...>` position contract removes defenses that exist only because standalone C function-pointer type spelling was unavailable:

The sweep also owns one pre-existing diagnostic defect independent of function literals. `Array<Heap, 2>`, `View<Heap>`, and `List<Heap>` equality currently each report `equality is unavailable because member  does not support ==`. Collection equality must describe an element path, never manufacture an empty member name.

- Replace `compiler/types.Storable`'s Fun whitelist with the new accepted-position matrix while retaining the Pointee and HeapAllocation exclusions.
- Remove the explicit Fun-result rejection from `checkResultType` and the nested-Fun-result rejection from `resolveFunctionTypeUse`.
- Remove the explicit Fun object-member and ADT-payload rejections from object and ADT declaration checking; keep ordinary completeness and copyability checks.
- Remove collection, Task, and Channel element/result rejections that are implied only by the old `Storable` whitelist. Do not weaken their unrelated lifetime or copyability rules.
- Replace generator result guards in ordinary function, method, and specialization declaration writing with the `typeof` type-only spelling.
- Make generated-type validation recurse through Fun parameters and results without rejecting a nested Fun result.
- Extend `typeIsModuleEmitted` and its tests through Fun signature parameters and results.
- Extend equality-unavailability and print-path tests through every newly reachable Fun-containing aggregate. Replace the empty-reason fallback for Array/View/List equality with an element-path diagnostic.
- Replace tests that assert the removed placement diagnostics with positive checked-tree and generated-C assertions. Retain Ptr/MutPtr, `ref`, direct heap-allocation, Dict-key, equality, and ordering rejection tests.
- Preserve existing complete function-pointer declarators where they are already correct; the sweep must not cause unrelated generated-artifact churn.

## Settled classification decisions

### Typed direct-literal binding

An explicit Fun annotation prevents declaration sugar:

```hexal
operation: Fun<(Int32) : Int32> := fun (value: Int32): Int32 do
    return value
end
```

- Only an inferred fixed direct initializer (`operation := fun ...`) is a code declaration.
- The typed form above stores one concrete function pointer and has no implicit self name.
- `mut` forms and contextual generic specializations are also runtime storage.
- `:=` already determines the literal's exact Fun type; writing `: Fun<...>` deliberately requests a value binding.
- Grouping-only parentheses do not change the classification.

### Nested generic-parameter names

A local function cannot redeclare an enclosing generic name:

```hexal
fun outer<T>(value: T): T do
    fun inner<T>(value: T): T do
        return value
    end
    return inner(value)
end
```

- Reject the inner `T` as a duplicate of an active enclosing generic parameter.
- Reusing the spelling supplies no capability that a distinct name does not provide and makes signatures ambiguous to readers.

## Implementation plan

Execute the stages in order. Stages may be developed separately, but the expanded `Fun<...>` checker matrix, recursive `typeof` lowering, and generated-type validation must become externally visible atomically: never leave a revision that accepts a program the generator cannot lower.

### Stage 0 — baseline and invariant inventory

1. Confirm RFC 0093 is implemented and establish the clean pre-0094 source and snippet-manifest baseline.
2. Run the existing parser, checker, generator, integration, snippet, and full Go suites before editing.
3. Record the current generated artifacts for every existing snippet. No existing manifest hash may move during 0094; only the four new snippets may add entries.
4. Inventory, by stable declaration or test name rather than line number:
   - parser `fun` dispatch in `topLevelItem`, `statement`, `functionDeclaration`, `primaryExpression`, and postfix parsing;
   - checker function declarations, scopes, declarations, generic registration/specialization, expression dispatch, body checking, return-flow, equality, print, and declaration-only module classification;
   - type-position eligibility in `compiler/types.Storable` and `Eligible`;
   - generator declarations, type spelling, checked-tree walking, module collection ownership, generated-type validation, concurrency discovery, and `emitModulePair`;
   - tests asserting old Fun placement failures or function-declaration module-only behavior.
5. Reproduce and record the existing Array/View/List empty-member equality diagnostic before changing it.

Gate: the baseline suites pass, the manifest snapshot is retained for final comparison, and every old rejection to be removed has an identified owning test.

### Stage 1 — parser syntax and source representation

1. In `compiler/parser/ast.go`, add an explicit anonymous-function expression containing the `fun` token, generic parameters, parameters, optional result type, `do`, body, and `end`. Reuse the existing parameter and body node types.
2. In `compiler/parser/statements.go`, factor the shared named-function signature/body parser so module and local declarations cannot drift.
3. In `compiler/parser/parser.go`:
   - keep module-position `fun identifier` as the existing named declaration;
   - parse statement-position `fun identifier` as a local named declaration;
   - reject statement-position `fun (` and `fun<` with the specified focused diagnostic;
   - retain the identifier/`self`-headed call-statement boundary and the existing `(` statement diagnostic.
4. In `compiler/parser/expressions.go`, parse expression-position `fun (` and `fun<...>(` as postfix bases. Reuse generic-parameter parsing, including nested `>` handling, and allow ordinary call suffixes only after the complete literal.
5. Preserve grouping semantics precisely. The parser currently erases redundant parenthesized-expression nodes, so `name := ((fun ... end))` still exposes the literal as the initializer root. A call suffix produces a CallExpression and therefore does not qualify as a direct literal initializer.
6. Add parser tests only for the syntax and diagnostics named in Validation: concrete/generic literals, local named functions, postfix calls in value context, forbidden statement starts, missing annotations/delimiters, and nested forms.

Gate: parser tests pass; no checker or generator behavior is exposed yet; existing named-function parses remain structurally unchanged.

### Stage 2 — one checked function form and lexical function bindings

1. In `compiler/checker/functions.go`, extract or extend one shared signature/body checker used by module named functions, local named functions, concrete literals, and concrete generic specializations. Keep parameter binding, result checking, fall-through, deferred actions, and source mapping in that shared path.
2. In the checked expression model (`compiler/checker/operands.go` and the nearest existing checked-function declarations), add an explicit function-literal expression carrying the resolved Fun type, checked parameters/result/body, defers, source range, template identity when applicable, and later-assigned helper ordinal.
3. In `compiler/checker/scope.go`, add lexical function bindings without creating a second source namespace:
   - data and function names still collide in one lexical frame;
   - child control-flow blocks inherit visible module and earlier local functions;
   - leaving a child block removes its local functions;
   - a fresh function body receives visible function declarations and its own self binding, but no enclosing runtime data, parameter, `self`, or Fun-value binding.
4. In `compiler/checker/control_flow.go`, admit local named declarations as non-executable checked declarations. Register each declaration before checking its own body, then expose it only to following statements in the same lexical block.
5. In `compiler/checker/declarations.go`, classify declaration sugar after removing grouping-only syntax:
   - `name := fun ...` is code, fixed, self-recursive, and storage-free;
   - a written type, `mut`, a call or other suffix, contextual specialization, or existing function value makes the declaration runtime storage;
   - typed and mutable forms receive no implicit self binding.
6. Preserve source-order visibility. Self and earlier local functions resolve; later local functions do not. Do not add a forward declaration collection pass from RFC 0112.
7. In module declaration-only checking, treat only module-level direct inferred fixed literals as declarations. Typed/mutable/runtime forms remain executable module data and retain existing rejection.
8. Add checker tests for exact named/anonymous parity, recursion, captures, nested block lifetime, duplicate names, shadowing, source order, typed-vs-inferred classification, grouping, and declaration-only imports.

Gate: all Stage 2 checker tests pass; checked trees contain no executable statement or storage node for local named functions and declaration-sugar literals; runtime forms still contain ordinary bindings.

### Stage 3 — generic template identity and nested generic scopes

1. In `compiler/checker/generics.go`, give every open function template a compiler-owned identity independent of its source name. Store that identity in generic function bindings, specialization keys, caches, active-recursion guards, and checked references.
2. Route module named, local named, and direct inferred anonymous generic declarations through the same registration and specialization engine. Delete the source-name-only function-template path rather than maintaining two registries.
3. Maintain an active generic-parameter-name stack covering enclosing generic functions, methods, local named functions, and literals. Validate an inner parameter against both its own list and every active enclosing name; report the duplicate at the inner token.
4. Permit an inner signature/body to use an enclosing generic parameter when it does not redeclare that name. Recheck the literal/local function for every reachable enclosing concrete specialization.
5. Keep typed contextual generic literals as runtime concrete function-pointer values. Keep direct inferred fixed generic literals as storage-free open-template declarations.
6. Preserve imported-generic ownership: the defining module's specialization registry caches and emits importer-requested concrete instances; the importer owns only declarations and uses.
7. Add tests for inferred, explicit, expected-result, exact-Fun contextual, imported, same-name disjoint-scope, enclosing-type-parameter, duplicate-name, and argument-changing recursive specialization cases named in Validation.

Gate: existing named generic behavior and generated names are unchanged; local/anonymous templates have distinct stable identities; repeated specializations reuse exactly one cached instance.

### Stage 4 — Fun position matrix and recursive diagnostics

Apply the checker and generator eligibility changes as one coordinated stage.

1. In `compiler/types/collections.go`, replace the Fun whitelist in `Storable` with the accepted matrix from this RFC. Retain Ptr/MutPtr pointee, direct HeapAllocation, Dict-key, `ref`, equality, ordering, completeness, and Atomic/copyability exclusions.
2. In checker function/type resolution:
   - remove the direct Fun-result rejection from `checkResultType`;
   - remove the nested-Fun-result rejection from `resolveFunctionTypeUse`;
   - retain Ptr/MutPtr construction rejection.
3. Remove the explicit Fun object-member and ADT-payload diagnostics. Let the common position/copyability model decide them.
4. Audit Array, View, List, Dict value, Task argument/result, Channel element, union, and nullable injection paths so no family keeps a private old whitelist.
5. In equality checking, replace the single member-name result with a path classification capable of describing object/ADT fields and Array/View/List elements. Preserve direct Fun rejection and nullable Fun-vs-Nil pointer-niche equality. Fix the existing empty-member diagnostic for all unsupported collection element types, not only Fun.
6. In print checking, preserve recursive rejection and require the first field, payload, element, or Dict-value path. Ensure failed print checking does not create print-component demand or emit a helper.
7. Add direct position-matrix tests and the aggregate equality/print cases listed in Validation. Do not broaden Dict equality or add function-pointer equality/hash semantics.

Gate: every newly accepted position reaches a checked tree; every retained rejection still reports its established diagnostic; Array/View/List equality never contains an empty member name.

### Stage 5 — C23 type spelling, ownership, and validation

1. In `compiler/generator/render.go`, add a recursive standalone Fun type spelling using C23 `typeof`:
   - nested Fun parameters/results recurse through the same spelling;
   - a fixed binding requiring this path spells `typeof(...) const name`;
   - a mutable binding spells `typeof(...) name`;
   - ordinary named function definitions remain ordinary C functions.
2. Keep existing complete binding, parameter, and union-member declarators byte-identical where they already represent the type correctly. Do not introduce a generated function-pointer typedef family.
3. In generator declaration/prototype writing, replace Fun-result fail-closed branches with the standalone `typeof` spelling for ordinary functions, methods, exported/foreign declarations where applicable, and concrete specializations.
4. In `validateGeneratedType`, recurse through every Fun parameter and optional result, including nested Fun results; retain canonicality and unsupported-pointee checks.
5. In `typeIsModuleEmitted`, recurse through Fun parameters and result. A collection containing any recursively module-owned signature type remains module-owned; a builtin-only signature remains program-component-owned.
6. Audit package templates and generated collection declarations for Fun elements/values. Reuse the shared spelling; do not fork templates or add wrappers.
7. Add generated-C text tests for results, fields, each collection family, Task/Channel use, nested Fun signatures, qualifiers, module-owned signatures, builtin-only signatures, and absence of typedef families.

Gate: all newly accepted programs emit complete C text with declarations preceding uses; existing programs retain their prior artifact bytes; no external C compiler is added to ordinary tests.

### Stage 6 — helper discovery, ordinals, and emission

1. Extend checked-tree walking in `compiler/generator/walk.go` so local named functions and literals are explicit children. Walk ordinary module statements in source order, concrete specializations in their existing deterministic order, and nested local/literal bodies in preorder.
2. Assign one shared module-local `hex_fun_<ordinal>` stream to local named and anonymous helpers. Do not derive local helper names from source names or maintain separate counters.
3. Ensure all discovery passes traverse the new bodies exactly once: string literals, collection/component demand, equality/print demand, defers, spawn targets, concurrency frames, source maps, and generated-type validation. Seen sets prevent duplicate emission; no pass silently skips the new expression kind.
4. In `emitModulePair` and declaration writing:
   - emit static prototypes for every local/anonymous helper before ordinary definitions;
   - retain ordinary and specialized definition order;
   - emit helper definitions once in ordinal order before root `main` statements;
   - keep local helpers at file scope and static linkage;
   - lower recursive/local references directly to the assigned helper symbol.
5. Replace the stale `emitModulePair` CARE comment with the present prototype/definition ownership contract.
6. Preserve literal signature/body `#line` mappings and use source names, never helper symbols, in user diagnostics.
7. Add determinism and generated-text tests for interleaving, insertion renumbering, same-name disjoint scopes, nesting, self-recursion, earlier-local calls, rejected later-local calls, enclosing-helper call cycles permitted by source visibility, spawn/defer single discovery, and no closure/environment artifacts.

Gate: two identical compilations are byte-identical; each helper has exactly one prototype and definition; every generated helper use has a preceding declaration.

### Stage 7 — exhaustive public behavior tests

1. Add focused parser/checker unit coverage only where a stage invariant cannot be observed through the public compiler.
2. Add or update active integration tests under the existing language-facet files; do not create RFC-named tests or cite this RFC in comments.
3. Implement every Validation bullet, including:
   - concrete, generic, local, nested, stored, passed, returned, mutable, nullable, and direct-expression forms;
   - every accepted and retained-rejected position;
   - typed-vs-inferred sugar and grouping;
   - capture, scope, source-order, declaration-only, duplicate-generic, and imported-specialization behavior;
   - equality/print propagation and no-helper-on-error;
   - recursive `typeof`, qualifiers, ownership, prototype order, ordinals, source maps, and absence of closure/runtime machinery.
4. Assert generated C text for every generator contract; compilation success alone is insufficient.

Gate: targeted parser, checker, generator, integration, and package tests pass with no external toolchain.

### Stage 8 — snippets and manifest

1. Add exactly four workbench snippets: stored literal, directly passed literal, bound no-result literal, and multi-parameter literal.
2. Compile every catalog snippet through the public in-memory API.
3. Regenerate the snippet manifest using the repository-prescribed temporary test, then delete that test.
4. Compare against the Stage 0 snapshot:
   - no existing artifact hash changes;
   - only the four new snippets add entries;
   - any other movement is a defect or an unrecorded blast radius and blocks completion.

Gate: `go test ./workbench/snippets` passes and the manifest diff has exactly the allowed additions.

### Stage 9 — canonical documentation, status, and handoff

1. After behavior stabilizes, update `docs/reference.md` once and atomically:
   - grammar for literals and local named declarations;
   - the inferred-fixed declaration-sugar boundary;
   - closed scope, capture rejection, local visibility, and source order;
   - expanded Fun position matrix and retained exclusions;
   - generic template identity, enclosing parameters, and duplicate-name rule;
   - recursive C23 `typeof`, helper naming/prototypes, module ownership, and no-closure ABI;
   - equality/print propagation through Fun-containing aggregates.
2. Verify every implemented rule has exactly one normative home in the reference and remove the old `no function literal` and narrow Fun-position statements.
3. Run `go test ./compiler/...`, `go test ./workbench/snippets`, and `go test ./...` without an external toolchain.
4. Remove 0094's open TODO and empty-member diagnostic bug from `docs/status.md`, set the RFC status to implemented/closed as appropriate, and move it to the archive only after all gates pass.
5. Rebuild the workbench binary into `bin/` and restart the running workbench for handoff.

Gate: code, tests, generated artifacts, `docs/reference.md`, RFC status, and `docs/status.md` agree; the workbench runs the completed compiler.

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
- A result-producing literal can be invoked directly only inside an expression. Anonymous and parenthesized calls are rejected as call statements. A no-result literal must be bound before invocation. A fixed binding containing a literal cannot be reassigned.
- Statement-position `fun (` and `fun<` report `anonymous functions cannot begin statements; bind the function first`; a statement-position `(` retains the existing diagnostic.
- Literal parameters are fixed; missing parameter types, malformed result forms, invalid returns, fall-through results, wrong call arity, incompatible signatures, and incompatible arguments are rejected with the specified diagnostics.
- A direct inferred fixed function-literal binding supports self-recursion and lowers recursive calls directly to its helper. Enclosing local, enclosing parameter, root data, root Fun binding, `self`, and mutable receiving-binding references are rejected without a capture environment.
- A module-level inferred fixed literal binding is callable from a later function body; an earlier function body cannot see it. The equivalent named declaration has identical visibility and diagnostics.
- A module-level direct inferred fixed literal declaration is accepted in an imported declaration-only module, emits no module-initializer statement or function-pointer storage, and remains private. `export` remains valid only on the equivalent named function form.
- A local inferred fixed literal binding is callable only by following statements in its lexical scope. Its body sees itself but cannot see any other binding from the containing function scope.
- Local named functions and direct inferred fixed literal bindings have identical lexical visibility, self-recursion, closed-scope capture rejection, generic specialization, checked representation, and file-scope static lowering. Earlier local function declarations are callable from later local function bodies in the same block; neither spelling is visible outside that block.
- Source order remains authoritative: a local function may call itself and earlier visible local functions, but a call to a later local function in the same block is rejected. Mutual recursion through a later declaration remains unavailable until RFC 0112 changes the shared visibility rule.
- A local function in a nested conditional or loop block is hidden after that block. Same-named local functions in disjoint blocks remain distinct and deterministic.
- A local named function may be used as a `Fun<...>` value. `export fun` remains rejected outside module scope, and no local function emits block-local C storage or a C nested function.
- A typed literal binding, mutable literal binding, contextual generic specialization, and fixed binding initialized from an existing function value remain runtime data; they do not gain a self name, module function visibility, or declaration-only classification. Grouping-only parentheses preserve direct inferred declaration classification.
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
- Equality rejects an object and ADT containing a Fun field by naming that field. Equality rejects Array, View, and List Fun elements by naming the element type; no diagnostic contains an empty member name. Nullable Fun equality against Nil retains its existing pointer-niche behavior.
- Print rejects object, ADT, Array, View, List, and Dict-value aggregates containing Fun with the first offending field, element, or value path. No print helper is emitted for a rejected aggregate.
- Function results, aggregate fields, and component templates that require a standalone C type specifier use recursive C23 `typeof` spellings. Existing complete function-pointer declarators remain byte-identical. No generated function-pointer typedef family is emitted.
- A collection over a function signature containing a module-owned parameter or result is module-owned; a builtin-only signature retains program-component ownership.
- Direct position-matrix tests cover View and List Fun elements and prove that ordinary Fun pointer copies remain eligible while Ptr/MutPtr pointees and direct Heap allocation remain rejected.
- A generic named function containing one literal is instantiated with two concrete types; helpers have distinct identities and correct signatures. Named generic function values retain current behavior.
- `identity := fun<T>(value: T): T do ... end` creates a fixed,
  non-assignable template binding and emits no C storage before
  specialization.
- The named and anonymous spellings of the same generic function produce equivalent open-template behavior; `alias := identity` remains rejected without an exact expected `Fun<...>` type.
- Two local generic literal bindings with the same source name in disjoint scopes have distinct template identities, specialization caches, generated helpers, and diagnostics.
- Generic literals support inferred calls, explicit type-argument calls, expected-result inference, and direct invocation inside an expression.
- Local named generic functions and direct inferred fixed generic-literal bindings produce equivalent lexical open-template bindings, including distinct identities for same-named declarations in disjoint scopes.
- A nested local or anonymous generic function that redeclares an active enclosing generic parameter name is rejected as a duplicate; a distinct inner parameter and an unshadowed use of the enclosing parameter both compile.
- `try`, `errdefer`, and return-flow checks inside a literal or local function use that function's own result type, never the enclosing function's. `spawn` discovery and deferred actions traverse the local checked body exactly once.
- Assigning a generic literal to an exact `Fun<...>` binding and passing one directly to an exact `Fun<...>` parameter each emit the required concrete specialization.
- A mutable binding with an exact `Fun<...>` type may hold a concretely specialized generic literal; an unspecialized generic template cannot be mutable.
- Generic literals diagnose duplicate or unresolved type parameters, conflicting inference, invalid explicit arguments, and argument-changing recursive specialization exactly like named generic functions.
- Two identical compilations produce byte-identical files. Distinct literals and concrete specializations receive distinct deterministic helper names.
- Generated C text verifies: prototypes precede uses; local named and anonymous definitions are file-scope `static`; local helpers use `hex_fun_<ordinal>` rather than source-derived module symbols; an enclosing named function and its helper may call each other; nested helpers emit once; direct function pointers are used; and no C nested function, environment object, environment parameter, allocation, dispatcher, or delegating wrapper is emitted. The updated `emitModulePair` CARE comment describes this prototype contract accurately.
- Local named and anonymous helpers interleave in one source-preorder `hex_fun_<ordinal>` stream. Fixed `typeof` bindings qualify the pointer value (`typeof(...) const name`); mutable bindings omit that qualifier.
- A declaration-only imported module may provide an unspecialized direct inferred fixed generic literal without emitting storage. A specialization requested by an importer is cached and emitted by the defining module's existing imported-generic specialization registry; the importer emits only its required declaration/use surface.
- Literal helper signatures and bodies retain correct `#line` mapping.
- Four workbench snippets cover stored, directly passed, bound no-result, and multi-parameter literals through the public compiler API.
- Against the recorded post-0093 manifest, no existing artifact hash changes; only the four new snippets add entries.
- `docs/reference.md` contains final grammar and semantic/C contracts.
- `go test ./compiler/...`, `go test ./workbench/snippets`, and `go test ./...` pass without an external toolchain.

## Handoff

After testable acceptance conditions pass, rebuild the workbench into `bin/` and restart it. This is an operational handoff step, not a language acceptance test.
