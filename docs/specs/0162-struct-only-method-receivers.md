# RFC 0162: Struct-Only Method Receivers

- Kind: Language Semantics
- Status: Implementation-ready; implementation not started
- Created: 2026-09-10
- Depends on: the current method declaration and call contracts in `docs/reference.md`
- Coordinates with: RFC 0165 (pointer spelling and slice migration)
- Supersedes on implementation: the pointer-receiver portion of RFC 0165
- Does not update `docs/reference.md`: synchronize only after implementation
  stabilizes and the user explicitly approves the reference edit

## Summary

Methods are declared only on nominal struct types:

```hexal
type Point is struct
    x: Int32,
    y: Int32,
end

method Point.read(): Int32 do
    return self.x
end
```

Pointer receiver declarations are not part of the language. `Ptr<T>` and
`Ptr<mut T>` remain ordinary pointer types for bindings, parameters, results,
members, allocation, foreign interfaces, and explicit functions.

This RFC deliberately chooses one method form. A method receiver is a fixed
value copy; it is never an alias to caller storage and never grants mutation of
the caller's value.

## Goals

- Keep one obvious method declaration form: `method T.name(...)`.
- Make method receiver semantics independent of pointer writability.
- Preserve autoderef at member access and preserve useful method calls through
  pointers by copying the pointed-to struct.
- Keep pointer-based mutation available through explicit functions.
- Remove pointer receiver dispatch, pointer receiver adaptation, and their
  associated method namespace complexity.
- Preserve ordinary method visibility, generics, recursion, source-order
  independence, and generated-C value-parameter lowering.

## Non-goals

- Removing `Ptr<T>` or `Ptr<mut T>` from the type system.
- Adding ownership, borrowing, lifetime, destructor, or receiver-move rules.
- Adding method overloading, static methods, closures, method values, or
  runtime dispatch.
- Adding implicit mutation of a caller's struct.
- Changing free-function pointer parameters or explicit address-taking.

## Grammar

The method declaration grammar remains:

```ebnf
method-declaration = "method" , struct-receiver , "." , identifier
                     , [ generic-parameter-list ] , signature
                     , "do" , block , "end" ;
struct-receiver = type-expression ;
```

After type resolution, `struct-receiver` must denote a local nominal struct
type declared with `type Name is struct ... end`. A transparent alias may name
that same struct but does not create another method owner or namespace.

The following are invalid method receivers:

```hexal
method Ptr<Point>.read(): Int32 do ... end
method Ptr<mut Point>.set_x(value: Int32) do ... end
method Point | Nil.read(): Int32 do ... end
method Int32.read(): Int32 do ... end
```

The invalid receiver diagnostic is:

```text
method receiver must be a struct type; got <type>
```

The diagnostic is produced by the checker, before method-body checking or
code generation.

## Receiver semantics

For:

```hexal
method Point.read(): Int32 do
    return self.x
end
```

- `self` is an implicit fixed binding of type `Point`.
- The receiver value is copied when the method is entered.
- Assigning to `self` is rejected because the binding is fixed.
- Writing `self.field` is rejected because the receiver copy is read-only.
- A method may make a mutable local copy and return it:

```hexal
method Point.with_x(value: Int32): Point do
    mut result: Point := self
    result.x = value
    return result
end

point = point.with_x(9)
```

Any mutation of `result` affects only that copy. A method cannot mutate the
caller's original struct through its receiver.

## Calls and autoderef

A method call on a struct value uses the value directly:

```hexal
point.read()
```

A call on `Ptr<T>` or `Ptr<mut T>` may autoderef one pointer layer, copy the
pointee as the method receiver, and then invoke the same `T` method:

```hexal
point: Point := Point(x = 3, y = 4)
reader: Ptr<Point> := @point
writer: Ptr<mut Point> := @point

reader.read()
writer.read()
```

Neither pointer mode changes the method's value semantics. In particular,
`writer.read()` cannot mutate `point`, and no pointer receiver is synthesized.
Nullable pointers must be narrowed before the autoderef call. More than one
pointer layer is not implicitly dereferenced for method dispatch.

Autoderef remains an access rule, not a capability upgrade: it makes `self.x`
and `pointer.x` convenient, but it does not turn a value receiver into an
alias or a read-only pointer into a writable one.

## Mutation through explicit functions

In-place mutation remains expressible with an explicit writable pointer:

```hexal
fun set_x(point: Ptr<mut Point>, value: Int32) do
    point.x = value
end

set_x(@point, 9)
```

This distinction is intentional: methods provide value-oriented struct
behavior; functions make caller-storage mutation explicit at the call site.

## Method ownership and lookup

- Only the defining module of a nominal struct may declare its methods.
- Imported structs may call exported methods but cannot receive local methods.
- A transparent alias never creates a second method table.
- A struct has one namespace shared by members and methods.
- A method name cannot equal a member name.
- A method name may be declared at most once for a struct; pointer receiver
  variants do not provide additional overload slots.
- Method signatures remain visible throughout their module regardless of source
  order, including recursive and mutually recursive method calls.
- Methods are resolved before callable members with the same lookup spelling.
- Methods are not first-class values.
- There are no overloads, default arguments, static methods, closures, or
  runtime method dispatch.

## C23 lowering

- Every method receives one hidden first parameter containing the struct value,
  with the same C representation used for a normal `Point` value parameter.
- No method definition or prototype is emitted with a pointer receiver.
- A call through `Ptr<T>` or `Ptr<mut T>` emits one pointee load/copy before
  entering the value-receiver method, subject to the generator's existing
  aggregate-copy lowering.
- No receiver object, vtable, ownership metadata, or runtime dispatch state is
  emitted.
- Existing source mapping, visibility, symbol naming, and declaration-order
  contracts remain unchanged.

## Migration

```text
method Ptr<T>.read(...)       -> method T.read(...)
method Ptr<mut T>.mutate(...) -> fun mutate(target: Ptr<mut T>, ...)
```

Read-only pointer methods become value methods, accepting the explicit cost of
copying the pointee on pointer calls. Mutating pointer methods become functions
whose writable pointer parameter makes the mutation visible in the signature
and call site.

No compatibility alias or hidden pointer-receiver form is introduced.

## Required sweep

Inventory and reconcile:

- parser and checker acceptance of `T`, pointer, nullable, alias, generic,
  union, primitive, and imported receivers;
- receiver adaptation and method-call typing, retaining only exact value
  dispatch and one-layer pointer-to-value copying;
- `self` binding writability, field mutation diagnostics, and explicit mutable
  local-copy behavior;
- method declaration collection, ownership, duplicate-name checks, generic
  specialization, recursion, and imported-method lookup;
- generator prototypes, definitions, receiver operands, aggregate copies,
  source mapping, and absence of pointer receiver lowering;
- integration tests, generated-C text assertions, workbench snippets, and
  manifest entries covering pointer receiver declarations and calls; and
- `docs/reference.md`, which must be synchronized after behavior stabilizes.

Delete checker, generator, and test code whose sole purpose is to support
pointer receiver declarations or pointer receiver adaptation. Retain pointer
types, pointer member autoderef, explicit `@`, writable pointer parameters,
and all non-method pointer operations.

## Validation

This section is exhaustive.

- `method Point.read()` is accepted when `Point` is a local nominal struct.
- Methods on an empty struct are accepted.
- Methods with parameters, results, generic parameters, recursion, and calls
  to later methods retain existing behavior.
- A transparent alias resolves to its underlying struct and does not create a
  second method namespace.
- Methods for imported structs are rejected by the existing ownership rule.
- `Ptr<T>`, `Ptr<mut T>`, nullable receivers, unions, primitives, builtin
  generic types, and non-struct nominal types are rejected with the specified
  receiver diagnostic.
- `self` has the declared struct type and is a fixed read-only binding.
- Assignment to `self` is rejected.
- Assignment to a `mut` member through `self` is rejected.
- Copying `self` into a mutable local permits mutation of that local and
  returning it as a new struct value.
- A method call on a `T` value uses the value receiver directly.
- A method call on `Ptr<T>` or `Ptr<mut T>` autodereferences one layer and
  invokes the `T` method on a copy.
- A nullable pointer cannot be autodereferenced for a method call before
  narrowing.
- A pointer-to-pointer is not autodereferenced twice for method dispatch.
- A pointer mode never enables mutation through a value-receiver method.
- Explicit functions accepting `Ptr<mut T>` can still mutate the pointee.
- A method cannot be extracted as a `Fun` value.
- No pointer receiver prototype or definition is emitted in generated C.
- Pointer calls emit value-receiver copy lowering and preserve source mapping.
- Existing method visibility, member-name conflicts, duplicate-name diagnostics,
  module ownership, generic specialization, and call arity rules remain green.
- The ordinary Go suite, tagged checks, generated-C text assertions, and the
  snippet manifest pass with only the specified method-receiver changes.

## Detailed implementation plan

### Phase 1: baseline and inventory

1. Record all production, test, snippet, reference, and generated-C uses of
   pointer receiver declarations and receiver adaptation.
2. Capture the current snippet manifest and generated-C baseline.
3. Classify each pointer receiver use as a read-only method migration or an
   explicit mutable-pointer function migration.

### Phase 2: checker and AST contract

1. Restrict collected method receivers to canonical nominal struct types.
2. Remove pointer receiver declarations from method ownership and duplicate
   receiver-form handling.
3. Retain one-layer pointer autoderef to a copied `T` for method calls.
4. Preserve fixed, read-only `self` semantics and existing local-copy rules.
5. Add focused diagnostics for every invalid receiver category.

### Phase 3: generator and migration

1. Remove pointer receiver prototype and definition paths.
2. Lower every method receiver as the struct value type.
3. Update affected source programs and snippets to value methods or explicit
   pointer-parameter functions.
4. Add generated-C assertions for value receiver copies and absence of pointer
   receiver definitions.

### Phase 4: conformance and reference

1. Run the exhaustive Validation cases with focused integration coverage.
2. Review every manifest movement against the allowed receiver changes.
3. Synchronize `docs/reference.md` once implementation behavior stabilizes and
   the user approves the edit.
4. Update status only after code, tests, generated output, and reference agree.
5. Rebuild and restart the workbench before handoff.
