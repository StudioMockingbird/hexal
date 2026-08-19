# RFC 0094: Anonymous Function Literals

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design settled, implementation not started
- Created: 2026-08-19
- Scope: add non-capturing anonymous function literals that produce `Fun<...>`
  values
- Depends on: RFC 0093 (value declarations use `:=`), the existing `Fun<...>`
  function-value rules, and `docs/reference.md`
- Coordinates with: the lexer, parser, checker, generator, function-value
  integration tests, workbench snippets, and `docs/status.md`
- Does not change: named function syntax, function-value placement rules,
  function-pointer representation, or the no-closures rule

## Summary

Hexal can currently use a named function as a `Fun<...>` value, but it cannot
write the function body at the point where the value is needed. Add an
anonymous function expression:

```hexal
square := fun (x: Int32): Int32 do
    return x * x
end
```

The literal has the exact `Fun<(Int32) : Int32>` type determined by its
parameter and result annotations. Anonymous functions are ordinary function
values with compiler-generated identities; they do not capture lexical state,
so this RFC adds function literals but not closures.

## Syntax

An anonymous function literal has one of these forms:

```hexal
fun (p1: T1, p2: T2): R do
    body
end

fun (p1: T1, p2: T2) do
    body
end
```

The first form produces `Fun<(T1, T2) : R>`. The second produces the
no-result form `Fun<(T1, T2)>`.

Rules:

1. Anonymous function parameters always have explicit types and follow the
   same parameter eligibility, arity, and mutability rules as named functions.
2. A result type is explicit when the literal returns a value. Omitting the
   result clause means the body returns no value; a value-returning `return`
   is rejected.
3. The literal body uses the same statement and control-flow grammar as a
   named function body. `return` returns from the literal.
4. Anonymous function literals are expressions. They may appear in a value
   binding initializer, a function argument, or another position already
   permitted for a `Fun<...>` value.
5. A literal with an explicit signature determines its own `Fun<...>` type and
   does not require an expected `Fun<...>` type for ordinary non-generic use.
6. Generic anonymous functions are not introduced. A literal has no type
   parameters; generic behavior remains available through named generic
   functions.

## No closures

Anonymous function literals do not capture root bindings, local bindings,
parameters, `self`, or any other lexical environment. The body may read its
own parameters and call named functions, but an outer local name is not in
scope for the literal.

```hexal
fun make(h: Heap): Fun<(Int32) : Int32> do
    factor: Int32 := 2
    return fun (value: Int32): Int32 do
        return value * factor
    end
end
```

The example is rejected because `factor` is not a parameter of the literal.
Supporting captured environments is a separate closure specification and is
not part of RFC 0094.

## Function-value rules

Anonymous function values use the existing `Fun<...>` placement and call
rules:

- They may be stored in a binding or passed to a function parameter.
- They may be members of a union only where an ordinary `Fun<...>` value is
  already legal.
- They cannot be returned, placed in object or ADT members, stored in a
  collection, passed as a task or channel element, placed behind `Ptr` or
  `MutPtr`, or used as a `ref` target.
- Calls require exact arity and assignable arguments. No-result calls remain
  statements only.
- A literal is not addressable and has no equality or ordering operation.
- A literal may call named functions and other function values, subject to the
  existing recursion, visibility, and type rules.

The generated C uses the existing function-pointer representation. Each
literal receives a unique compiler-generated function name and carries no
environment pointer or allocation.

## Interaction with declarations and assignment

RFC 0093 applies to bindings containing literals:

```hexal
square := fun (x: Int32): Int32 do
    return x * x
end

mut operation: Fun<(Int32) : Int32> := square
operation = fun (x: Int32): Int32 do
    return x + 1
end
```

The `=` tokens inside object literals or other grammar-defined forms retain
their existing meanings under RFC 0093. The `fun` literal itself is introduced
only by `:=` or by an existing expression position; it is never a declaration
with a name.

## Diagnostics

Diagnostics must be owned by the earliest phase that can prove the error:

- A `fun` expression without `(` after `fun` reports the existing named-
  function syntax error or the dedicated anonymous-function syntax error.
- A missing parameter type or required result type reports a syntax/type
  diagnostic at the literal signature.
- A value-returning or fall-through body that violates the declared result
  reports the same body/result diagnostic as a named function.
- A reference to an outer local, parameter, `self`, or root binding reports an
  ordinary name-resolution error; it must not silently become a capture.
- A literal used in a forbidden `Fun<...>` position reports the existing
  function-value placement diagnostic.
- A literal passed to a function parameter with an incompatible signature
  reports the existing expected-`Fun<...>` type diagnostic.

## Migration

Implementation must:

1. Parse `fun` followed by `(` as an expression while retaining `fun name(...)`
   as the named function declaration form.
2. Add an explicit checked expression/node for anonymous functions rather than
   encoding the literal as a variable declaration or a named source function.
3. Reuse named-function parameter, result, body, return-flow, and placement
   checks wherever the semantics are identical.
4. Generate one static function body per literal with a deterministic unique
   name based on module identity and source position. The literal value points
   directly to that function.
5. Reject captures during checking. Do not add an environment object,
   heap allocation, closure ABI, or runtime capture support.
6. Update `docs/reference.md` after behavior stabilizes, including the literal
   grammar, no-capture rule, placement restrictions, and generated-C contract.
7. Add workbench snippets demonstrating a stored literal, a literal passed to a
   named function, a no-result literal, and capture rejection coverage in the
   compiler integration suite. Negative cases do not belong in the workbench
   catalog.

## Non-goals

- Lexical closures or captured variables.
- Generic anonymous functions.
- Async functions, coroutines, or a new task model.
- Function overloading, default arguments, named arguments, or variadic
  parameters.
- New `Fun<...>` placement permissions.
- Runtime function allocation or environment management.

## Validation

This section is exhaustive. RFC 0094 is complete only when every item below
passes:

- A named function declaration remains valid and an anonymous literal assigned
  with `square := fun (x: Int32): Int32 do ... end` compiles.
- A no-result literal and a multi-parameter result-producing literal compile
  with the expected `Fun<...>` types.
- A literal can be passed directly to a named function parameter requiring the
  matching `Fun<...>` type and can be called through a `Fun<...>` binding.
- A mutable `Fun<...>` binding can be reassigned with `=` to another compatible
  anonymous literal; a fixed binding cannot be reassigned.
- Missing parameter types, invalid result forms, fall-through result bodies,
  wrong arity, and incompatible literal signatures are rejected.
- Literals that reference an outer local, parameter, `self`, or root binding
  are rejected as captures; no capture environment is generated.
- A literal used in each currently forbidden `Fun<...>` position is rejected
  with the existing placement rule.
- Generic anonymous literals are rejected; named generic function values retain
  their current behavior.
- Two compilations of the same source produce byte-identical generated files,
  and distinct literal source positions receive distinct deterministic C names.
- The generated C uses direct function pointers and contains no closure
  environment allocation or hidden capture parameter.
- At least four workbench snippets cover stored, passed, no-result, and
  multi-parameter anonymous literals; all compile through the public compiler
  API and are added to the generated-artifact manifest.
- `docs/reference.md` contains the final anonymous-function contract and its
  no-closure restriction.
- `go test ./compiler/...`, `go test ./workbench/snippets`, and `go test ./...`
  pass without an external toolchain.
- The workbench binary is rebuilt into `bin/` and the running workbench is
  restarted before handoff.
