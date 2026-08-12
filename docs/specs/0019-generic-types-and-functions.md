# RFC 0019: Generic Types, Functions, and Methods

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-10
- Features: parametric type declarations, generic functions, generic methods,
  constructor and call inference, specialization-time operation checking, and
  C23 monomorphization
- Created: 2026-08-09
- Revised: 2026-08-10
- Depends on: RFC 0005 (type identity and aliases), RFC 0006 (objects), RFC
  0007 (mutability redesign), RFC 0008 (functions and methods), RFC 0014
  (general type expressions and unions)
- Coordinates with: RFC 0017 (checked and saturating arithmetic), RFC 0018
  (String and Rune values), RFC 0020 (collections), RFC 0022 (algebraic data
  types), RFC 0024 (equality, ordering, and hashability), and the future
  ownership and FFI specifications

## Summary

Seawitch currently supports nominal objects, functions, methods, and
constructed pointer types, but it cannot abstract over a type. This RFC adds
generic type declarations and generic functions without adding runtime type
objects or dynamic dispatch.

```seawitch
type Box<T> = {
    value: T,
}

fun identity<T>(value: T): T
    return value
end

number: Box<Int32> = Box { value = 42 }
same: Int32 = identity(number.value)
```

Generic definitions are specialized for concrete type arguments at compile
time. The generated C contains only concrete instantiations. Type arguments
in user-declared generics are types only in this RFC; arbitrary compile-time
values are not generic parameters. A built-in type may define a separate,
owner-specific argument form, such as RFC 0020's positive literal length for
`Array<T, N>`. That exception is not available to user-declared generics.

## Goals

1. Provide one parametric mechanism for types, functions, and methods.
2. Preserve nominal type identity after specialization.
3. Keep generated C readable and free of runtime generic machinery.
4. Reuse RFC 0005 canonical type identity and RFC 0014 type expressions.
5. Check operations involving type parameters against concrete types when a
   specialization is created.
6. Permit deterministic inference where the call or constructor provides
   enough type information, without introducing overload-style deduction.

## Non-goals

This RFC does not define:

- user-defined traits, interfaces, or protocol dispatch;
- runtime reflection over type arguments;
- dynamic dispatch or boxed generic values;
- generic closures;
- higher-kinded types;
- general compile-time value parameters, including user-declared static array
  lengths;
- arbitrary compile-time execution or reflection;
- explicit generic capability or constraint syntax;
- variance or subtyping for generic types;
- generic C ABI or foreign declarations;
- specialization priorities or overlapping implementations.

## Declaration syntax

### Generic types

Type parameters follow the declared name:

```seawitch
type Box<T> = {
    value: T,
}

type Pair<Left, Right> = {
    left: Left,
    right: Right,
}

type Link<T> = {
    value: T,
    mut next: MutPtr<Link<T>>,
}
```

The existing object body remains nominal. `Pair<Int32, Bool>` and
`Pair<UInt32, Bool>` are different canonical object types. A type parameter
must be used only in a position permitted for a complete type; an unbounded
parameter cannot by itself create an incomplete by-value cycle.

Open generic checking treats a type parameter as an abstract type placeholder;
it does not assume that the placeholder has a concrete C layout. A generic
declaration may therefore store `T` by value or use `T` in a by-value function
signature when that position is structurally valid. Every concrete
specialization must recheck completeness and layout after substitution. An
incomplete concrete argument is rejected in a by-value field, union member,
array element, parameter, or return position. Pointer indirection remains
subject to the existing incomplete-pointee rules.

Generic transparent aliases use the same syntax:

```seawitch
type Pointer<T> = Ptr<T>
```

The generic declaration and type-argument grammar is:

```ebnf
generic-parameter-list = "<" , generic-parameter
                         , { "," , generic-parameter } , ">" ;
generic-parameter      = identifier ;
type-argument-list      = "<" , type-expression
                         , { "," , type-expression } , ">" ;
```

RFC 0019 extends the existing declaration and type-expression productions as
follows:

```ebnf
generic-type-declaration       = "type" , identifier
                              , generic-parameter-list , "="
                              , type-expression ;
generic-object-declaration     = "type" , identifier
                              , generic-parameter-list , "="
                              , object-type-expression ;
function-declaration          = "fun" , identifier
                              , [ generic-parameter-list ]
                              , "(" , [ parameter-list ] , ")"
                              , [ ":" , type-expression ]
                              , { body-statement } , "end" ;
impl-declaration              = "impl" , receiver-type , "." , identifier
                              , [ generic-parameter-list ]
                              , "(" , [ parameter-list ] , ")"
                              , [ ":" , type-expression ]
                              , { body-statement } , "end" ;
generic-type                  = identifier , type-argument-list ;
generic-function-reference    = identifier ;
generic-object-literal        = identifier , [ type-argument-list ]
                              , "{" , [ member-initializer
                              , { "," , member-initializer } , [ "," ] ]
                              , "}" ;
receiver-type                 = type-expression ;
postfix-expression            = primary-expression
                              , { "." , identifier
                                | call-arguments
                                | generic-call-suffix } ;
generic-call-suffix           = type-argument-list , call-arguments ;
```

RFC 0019 adds the optional generic parameter list to RFC 0005's existing
`type-declaration` production. The `function-declaration` and
`impl-declaration` productions above replace RFC 0008's versions by adding
generic parameters and generic receiver types. In doing so, RFC 0019 renames
RFC 0008's `self-type` nonterminal to `receiver-type` and widens its accepted
type expression. The `postfix-expression` production extends RFC 0008 so a
generic call can occur after an identifier or member expression.
`generic-object-literal` is an additional `primary-expression` form.

RFC 0019 also extends RFC 0014's `primary-type-expression` with
`generic-type`. Built-in constructors such as `Ptr`, `MutPtr`, and `Fun`, plus
RFC 0020's `Array<T, N>` form, retain their owner-specific grammar and are not
parsed as ordinary user generic types. The right-hand-side alternatives remain
owned by the relevant RFC: RFC 0005 for type expressions, RFC 0006 for object
type expressions, and RFC 0022 for ADT definitions. `parameter-list`,
`body-statement`, and `member-initializer` retain their existing productions.
RFC 0022's `generic-parameters` spelling is an alias for this RFC's
`generic-parameter-list` production.
A generic type constructor is valid in every type position that accepts
the underlying non-generic type expression. A receiver still must resolve to
a nominal object or one of RFC 0008's permitted pointer receiver forms; a
union, scalar, function type, or `Nil` is not a method receiver merely because
it can be written as a type expression.

After an identifier or member expression, a balanced `type-argument-list` is
recognized as a generic call suffix only when it is immediately followed by a
call argument list. Otherwise the `<` and `>` tokens retain their relational
meaning. A type-argument list followed by `{` is parsed by the separate generic
object-literal production. The checker owns the final validation of the callee
and generic argument count. The right-hand side of an `as` conversion is parsed
by the type-expression grammar and does not use generic-call lookahead.

The parameter names in one declaration must be unique and must obey the
ordinary identifier and protected-name rules of RFCs 0004 and 0005. A type
parameter is in scope only in the declaration's type expressions and body. It
cannot be shadowed by another type parameter, local type declaration, or
nested generic declaration. A method may use receiver parameters and
introduce additional parameters, but the names must still be unique within the
method's complete generic parameter scope.

Generic type declarations are module-level and source-ordered like existing
type declarations. A use before the generic declaration is rejected under the
current declaration-order rule. A generic declaration's own name is visible
while its body is resolved so an indirect self-reference can be checked. A
generic alias may not recursively refer to itself, and a by-value object or
ADT cycle remains invalid after substitution.

### Generic functions

Function type parameters follow the function name and precede its parameter
list:

```seawitch
fun identity<T>(value: T): T
    return value
end
```

Generic methods use parameters on the method declaration and may also use the
receiver's parameters:

```seawitch
impl Box<T>.get(): T
    return self.value
end
```

The receiver's type parameters are inherited into the method's complete
generic scope; they are not repeated in the method's own parameter list. Thus
the `T` in `impl Box<T>.get` is bound by `Box`'s declaration, while `U` below
is a method parameter. Methods may introduce additional parameters:

```seawitch
impl Box<T>.same<U>(other: U): Bool
    return false
end
```

The method body remains a closed function scope. Generic parameters do not
capture local bindings.

Generic functions and methods retain RFC 0008's visibility rules. A declaration
is visible in its own body, allowing self-recursion. Other declarations become
visible only in source order; generic declarations do not introduce a separate
forward-declaration pass.

The complete generic argument list of a method contains both the receiver's
arguments and the method's own arguments. For example, the specialization of
`Box<Int32>.same<Bool>` is keyed by `Int32` followed by `Bool`.

Receiver arguments are determined from the receiver expression after RFC
0008's ordinary receiver adaptation. They are not written at the call site;
only the method's own arguments may be explicit:

```seawitch
box.same(other)          // infer U from other
box.same<Bool>(other)     // explicitly select U
```

Inference for a generic method first resolves the concrete receiver type, then
infers the method's own parameters from the call arguments and expected result
using the ordinary generic inference rules. A method generic argument list
must contain all method parameters when present; partial lists and placeholder
arguments are not supported.

An `impl` receiver for a generic owner must use the owner's generic parameters
once each, in declaration order, as distinct bare type parameters. For
example, `impl Pair<Left, Right>.method` and `impl Ptr<Box<T>>.method` are
valid receiver families. Concrete, reordered, repeated, or nested receiver
arguments are rejected in v1 because they could overlap another implementation:

```seawitch
impl Box<Int32>.get(): Int32       // Error: specialized receiver is unsupported
impl Pair<T, T>.same(): Bool       // Error: overlapping receiver pattern
```

The existing RFC 0008 rule remains: one method name may be declared only once
for an owner across its value, `Ptr`, and `MutPtr` receiver forms. Generic and
non-generic declarations with the same owner and method name cannot coexist.

### Generic function references

A generic function is not itself a runtime polymorphic value. When its name
appears in a function-value position, the concrete type arguments are inferred
from the expected `Fun<...>` type when every parameter is determined
unambiguously:

```seawitch
callback: Fun<(Int32) : Int32> = identity
```

Inference from a `Fun<...>` target is exact and non-variant: parameter and
return types must match canonically after alias resolution. Existing pointer
weakening is not used to infer a different function type. Explicit type
arguments on a function reference are not supported in v1; they are
unnecessary when the target type determines a specialization.

If the target type cannot determine every generic argument, the reference is
rejected. Generic methods remain subject to RFC 0008's deferred first-class
method-value rules; invoking a method through an ordinary receiver is
supported.

### Generic object construction

A generic object constructor may provide explicit type arguments:

```seawitch
number: Box<Int32> = Box<Int32> { value = 42 }
```

Type arguments may be omitted when the expected type or the initializer
values uniquely determine every parameter:

```seawitch
number: Box<Int32> = Box { value = 42 }
inferred: Box<Bool> = Box { value = true }
```

Constructor inference uses the same canonical type unification rules as
function-call inference. An explicit type argument list and initializer types
must agree. A generic constructor with no inference evidence, conflicting
evidence, or an unresolved parameter is rejected; it is never assigned a
default type argument.

Generic ADT variant construction uses the same rules. The owner arguments may
be explicit:

```seawitch
success: Result<Int32, Bool> = Result<Int32, Bool>.Ok { value = 42 }
```

or inferred from the expected owner type and payload:

```seawitch
success: Result<Int32, Bool> = Result.Ok { value = 42 }
```

The qualified owner form is an extension of RFC 0022's `qualified-variant`
production. If an owner argument is not determined by the expected type,
payload fields, or literal defaults, construction fails rather than selecting
a default specialization. Unit variants with an underconstrained generic
owner require explicit owner arguments.

The production is extended as follows:

```ebnf
qualified-variant = generic-owner , "." , identifier ;
generic-owner    = identifier , [ type-argument-list ] ;
```

### Built-in generic forms

RFC 0019 supplies the common type-argument, identity, reachability, and
specialization machinery for built-in generic types such as `List<T>`,
`View<T>`, and `Dict<K, V>`. Their operations and layouts remain owned by RFC
0020.

`Array<T, N>` is a deliberate built-in exception. RFC 0020 owns its argument
grammar and permits a positive decimal integer literal for `N`:

```seawitch
values: Array<Int32, 4>
```

`N` is not a user-declarable generic parameter and cannot be used in a generic
function or user-defined type declaration. The compiler still includes `T`
and `N` in the array's canonical specialization identity and concrete C
layout.

## Type arguments and inference

Generic type arguments are written in type positions:

```seawitch
pair: Pair<Int32, Bool>
items: List<Bool>
```

Generic function calls infer type arguments from their arguments and expected
result type:

```seawitch
answer: Int32 = identity(42)
```

An explicit type-argument list is available when inference is insufficient:

```seawitch
answer: Int64 = identity<Int64>(42)
```

The parser uses the generic postfix rules above. It recognizes a balanced
`<...>` after an identifier or member expression as generic call syntax only
when the following token begins the call's `(`. Otherwise, `<` and `>` retain
their relational meaning. Nested closing brackets are parsed as part of
type-argument context, so `Box<List<Int32>>` is one nested type. An `as`
conversion parses its right-hand side through the type-expression grammar and
does not use generic-call lookahead.

An explicit type-argument list supplies every argument. Partial lists and
placeholder arguments are not supported in v1:

```seawitch
fun make_pair<Left, Right>(left: Left, right: Right): Pair<Left, Right>
    return Pair<Left, Right> { left = left, right = right }
end

pair: Pair<Int32, Bool> = make_pair<Int32, Bool>(1, true)
bad: Pair<Int32, Bool> = make_pair<Int32>(1, true)
// Error: explicit generic argument count does not match declaration
```

The same complete-list rule applies to generic type constructors and generic
ADT owners. The only omission form is a completely omitted list whose
arguments can be inferred.

Inference is deterministic and proceeds in this order:

1. Validate any explicit type-argument count and bind those arguments.
2. Unify generic parameter occurrences in explicitly typed call arguments or
   constructor initializer values.
3. Unify generic parameters appearing in the expected result or destination
   type.
4. For an untyped literal, use the expected parameter type under RFC 0003 and
   RFC 0009. If no concrete expected type exists, use the literal's default
   type under RFC 0003 as inference evidence.
5. Require every parameter to resolve to one canonical type and require
   repeated occurrences to agree exactly.
6. Instantiate the declaration only after the complete argument list is
   known.
7. Check operations involving substituted type parameters against the
   concrete types.

Inference does not use overload ranking, numeric magnitude, implicit generic
conversions, or a change to the type of a typed argument. Existing
argument conversions are checked only after inference has selected the
specialization. If inference produces multiple candidates, conflicting
candidates, or an unresolved parameter, compilation fails. A contextual union
type does not cause generic inference to try every member: inference succeeds
only when one member uniquely determines all generic arguments.

### Structural unification

Unification normalizes transparent aliases before comparing types and uses
these rules:

1. An unbound bare parameter binds to the complete canonical type at that
   occurrence.
2. A repeated parameter must bind to the same canonical type; no implicit
   conversion changes an existing binding.
3. Two constructed types unify only when their constructors and arities match,
   after which their arguments unify recursively. `Array<T, N>` additionally
   requires the built-in positive literal `N` values to match exactly.
4. A union argument binds as its complete canonical union. It is not silently
   narrowed to one member. An expected union may provide inference evidence
   only when exactly one member produces a complete, unambiguous argument list.
5. `MutPtr<S>` may provide inference for a `Ptr<T>` parameter by binding `T`
   from `S`; RFC 0007's outermost pointer weakening is then checked as ordinary
   assignability. The reverse strengthening is never inferred.
6. Function types unify by exact canonical parameter and return types. No
   function variance or pointer weakening is used during function-type
   inference.
7. `nil` can bind a bare parameter directly to `Nil`, but it cannot infer an
   unknown pointee type inside `Ptr<T> | Nil` or another constructed context.

After unification, ordinary assignability is checked for every argument and
initializer. Generic constructed types remain invariant; RFC 0007 pointer
weakening is an assignability rule, not generic variance.

```text
[Type Error] cannot infer generic parameter T for identity
[Type Error] conflicting inferred types for generic parameter T
[Type Error] explicit generic argument count does not match declaration
```

Generic types are invariant in this RFC. No subtype, variance, or new implicit
conversion is introduced by generic inference; RFC 0007's existing pointer
weakening remains available during post-inference assignability checking.

## Specialization-time operation checking

Generic parameters do not declare capabilities in v1. An operation whose
meaning depends on an unknown type parameter is represented as a dependent
operation and checked when the generic declaration is specialized with
concrete type arguments.

### Open generic checking

When a generic declaration is checked before specialization, the checker:

1. resolves generic parameters, scopes, declarations, argument counts, and
   known nominal members normally;
2. checks expression shape, operand count, result shape, control-flow rules,
   and all type-independent errors; and
3. records a dependent operation when a supported operation needs a concrete
   substituted type to determine whether it is valid.

A dependent operation retains its source span, operation kind, checked
operands, expected result type, and the generic parameters it depends on. It
is not a generator placeholder and cannot reach C lowering unresolved.

The following are dependent when their operand or result types contain a
generic parameter:

- equality, ordering, and arithmetic operators;
- indexing and collection operations;
- object, ADT, and collection construction whose substituted layout or field
  operation depends on a generic parameter;
- calls whose parameter or return types contain a generic parameter; and
- pointer operations whose complete pointee type is not known until
  substitution.

Member lookup on a known generic declaration is performed during open checking.
For example, `self.value` is valid for `Box<T>` because `Box` declares
`value`. A bare type parameter has no known members:

```seawitch
fun name<T>(value: T): Bool
    return value.name // Error: T has no known member named name
end
```

Non-dependent operations are checked immediately. This includes malformed
syntax, unknown names, invalid scopes, invalid return structure, and member
lookup where the receiver's nominal type is already known.

### Closed specialization checking

When a concrete specialization is requested, the compiler:

1. substitutes every generic parameter with its canonical concrete type;
2. rechecks every recorded dependent operation using the ordinary concrete
   type rules of the owning specification;
3. applies ordinary constant-reachability rules after substitution; and
4. passes only fully checked concrete nodes to the generator.

An operation that is unreachable after concrete constant folding follows the
existing unreachable-expression rules. An operation in a runtime-reachable
branch must be valid for that specialization.

The primary diagnostic span is the dependent operation in the generic body.
The diagnostic also identifies the concrete instantiation that exposed it:

```text
[Type Error] operator `>` is unavailable for Bool
  in specialization maximum<Bool>
```

The body below is accepted as an open generic body because equality depends on
`T`:

```seawitch
fun equal<T>(left: T, right: T): Bool
    return left == right
end
```

Each concrete use is checked separately:

```seawitch
same: Bool = equal(1, 1) // valid; equality is available for Int32
```

An invalid specialization is a compile-time error:

```seawitch
fun maximum<T>(left: T, right: T): T
    if left > right
        return left
    else
        return right
    end
end

largest: Int32 = maximum(10, 20) // valid
bad: Bool = maximum(true, false) // Type Error: `>` is unavailable for Bool
//   in specialization maximum<Bool>
```

The compiler must never generate a runtime exception or unchecked C operation
for an unsatisfied dependent operation. If a generic declaration is never
specialized, its dependent operations are not instantiated and no executable
C body is generated for it.

The compiler may record inferred internal requirements for diagnostics and
implementation bookkeeping:

```text
equal<T> requires equality for T
maximum<T> requires ordering for T
```

These requirements are not user-written constraints, runtime interfaces, or
dispatch tables. A future trait or protocol specification may add explicit
constraint syntax when it is needed for public API contracts or overload
selection.

The operation contracts themselves remain owned by their feature
specifications:

- equality and ordering follow RFC 0024;
- arithmetic follows RFC 0017;
- hashing and collection operations follow RFC 0020 and RFC 0024.

No v1 generic declaration uses `T: Equatable`, `T: Ordered`, or
`T: Hashable`. Generic feature specifications must express such requirements
as specialization-time operation checking before their implementation plans are
approved.

The checker must reject a dependent operation at specialization time if the
substituted concrete type does not support it.

For example, the generic method below is valid because it only returns a
stored value:

```seawitch
impl Box<T>.get(): T
    return self.value
end
```

An operation on the stored `T` value is deferred until specialization:

```seawitch
impl Box<T>.equals(other: Box<T>): Bool
    return self.value == other.value
end
```

`Box<Int32>.equals` is valid. `Box<SomeUncomparableType>.equals` is rejected
when that method is specialized.

## Canonical identity

Every generic declaration has a declaration identity and arity. Each concrete
instantiation has a canonical identity composed from the declaration identity
and the ordered canonical identities of its type arguments:

```text
Box<Int32> != Box<UInt32>
Pair<Int32, Bool> != Pair<Bool, Int32>
```

Aliases normalize before identity comparison. Repeated requests for the same
instantiation reuse one canonical type and one generated C declaration.

Generic type arguments may be scalar, object, pointer, function, union, `Nil`,
or another supported constructed type, but the substituted type must be valid
in the position where it is used. For example, a function type may be used as
a generic function parameter type, but `Box<Fun<...>>` is rejected while RFC
0008 does not permit function-pointer object members. Unsupported incomplete
or recursive by-value substitutions fail before generation.

After substitution, every generic object and ADT is checked using the complete
layout rules of RFC 0006, RFC 0007, and RFC 0022. A generic object's own name
may reappear through a permitted pointer indirection; a by-value expansion is
rejected. Generic aliases do not receive this self-reference exception.

Type specialization tracks active `(declaration identity, canonical argument
list)` pairs while resolving layout. A recursive reference through an approved
pointer indirection may reuse an active type specialization only when its
canonical argument list is unchanged. A recursive reference that changes the
arguments is rejected rather than expanded without bound. The rule applies
through aliases and nested constructed types as well as direct self-reference:

```seawitch
type Link<T> = {
    mut next: MutPtr<Link<T>>,
}

type Nest<T> = {
    mut inner: MutPtr<Nest<Pair<T, T>>>,
}
// Error: recursive type specialization changes generic arguments
```

## Specialization

The compiler monomorphizes each reachable concrete generic function, method,
and type. A type specialization is keyed by its declaration identity and
canonical type arguments. A function specialization is keyed by its function
declaration identity and canonical type arguments. A method specialization is
keyed by its method declaration identity, receiver type arguments, and method
type arguments. Keys use canonical identities, not source spelling or display
text.

```text
identity<Int32> -> one C function
identity<Int64> -> a distinct C function
```

Specialization recursion is intentionally conservative in v1. A recursive
call may reuse an active specialization with the same canonical argument list:

```seawitch
fun same<T>(value: T): T
    return same<T>(value)
end
```

RFC 0008's source-order rule makes mutual recursion unavailable: a later
generic function is not visible while an earlier body is checked. The
unchanged-argument rule therefore applies to direct self-recursion and any
recursive generic method call that is otherwise visible under the existing
declaration-order rules. A recursive cycle that changes the canonical
arguments is rejected rather than expanded without bound:

```seawitch
fun expand<T>(value: T): T
    return expand<Ptr<T>>(ref value).value
end
// Error: recursive specialization changes generic arguments
```

The same-arguments rule applies to recursive generic method calls and type
specializations. These v1 restrictions avoid arbitrary compiler recursion
limits and defer type-level argument transformation to a future specification.

Reachability begins at checked top-level executable statements, supported
export roots once exports exist, and explicit references that select a concrete
generic function value. It follows calls, method calls, generic function
references, constructed field types, and signature types transitively. A
generic function body is emitted only when a concrete function specialization
is reachable. A generic type required by a reachable signature receives the
declarations needed to describe its concrete layout even when no constructor
is called.

Unused generic declarations produce no generated C body. An instantiated type
used only in a signature still receives the declarations needed by that
signature.

## C23 lowering

Generic C names include a deterministic specialization suffix derived from the
canonical declaration identity and type argument identities. The suffix must be
collision-free after deterministic resolution, stable across compilations of the
same module source, and independent of memory addresses or declaration
discovery order.

```c
sw_fn_identity__Int32(...)
sw_fn_identity__Int64(...)
```

The generator may choose readable abbreviations, but the specialization key
must be serialized injectively as length-delimited fields containing the
declaration kind, stable declaration identity, receiver arguments, method
arguments, and type arguments. Type identities use tagged recursive encoding:
each scalar, pointer, function, union, nominal type, and constructed generic
type has a distinct tag and recursively encoded arguments. Union members use
their canonical order.

The serialization must not contain memory addresses, insertion indexes, or
discovery order. If an internal canonical identity is compilation-local, the
emitted-name serialization must use its stable source declaration identity
instead. Any shortening or hash-based name scheme must retain a deterministic
collision suffix derived from the complete encoded key; collision handling must
not depend on insertion order. The result must preserve the source-name prefix
rules of RFC 0004. Generated C contains concrete structs, function signatures,
and bodies only. No C macro template, `void *` erasure, runtime type tag, or
unchecked cast represents a generic parameter.

Generic objects use the same field mutability and pointer declarator rules as
their substituted RFC 0006 object. A `Ptr<T>` or `MutPtr<T>` substitution
preserves its complete pointee access contract.

Generic functions cannot be exported or imported as foreign declarations until
the FFI specification defines how a concrete instantiation becomes an ABI.

Generic declarations do not execute arbitrary compile-time code. There is no
`comptime` or `static` generic parameter in this RFC, and no type reflection is
needed to specialize a declaration.

## Interaction with unions and match

Union types are valid type arguments. Generic code sees the union as one
canonical type and cannot access a member-specific operation without an RFC
0014 narrowing proof. RFC 0022 match patterns may narrow an instantiated union
in the same way as a non-generic union. A generic ADT variant pattern may use
an explicit owner:

```seawitch
| Result<Int32, Bool>.Ok then 1
```

or omit the owner arguments when the concrete scrutinee type determines them:

```seawitch
| Result.Ok then 1
```

Pattern owner arguments are resolved from the scrutinee before the arm body is
checked. An underconstrained owner or a pattern whose owner does not match the
scrutinee's canonical ADT type is rejected.

Generic parameters themselves are not runtime type patterns. A pattern such as
`T` is not accepted in a generic body because the type argument is erased from
runtime semantics after specialization.

## Diagnostics and fail-closed behavior

The parser owns malformed parameter, argument, constructor, and generic-owner
lists. The checker owns arity, scope, inference, substitution, specialization
recursion, and dependent-operation errors. A dependent-operation diagnostic
uses the operation's source location and includes the concrete specialization
that exposed it. The generator must explicitly handle every specialized type,
dependent operation, and expression node.

An unresolved type parameter, missing specialization, or malformed substituted
type reaching generation is an `Unknown Error`. It must never emit a placeholder
type, an empty function, or a `void *` fallback.

Representative diagnostics include:

```text
[Syntax Error] malformed generic argument list
[Type Error] explicit generic argument count does not match declaration
[Type Error] cannot infer generic parameter U for method same
[Type Error] generic receiver pattern overlaps another implementation
[Type Error] incomplete type argument is not valid in this by-value position
[Type Error] recursive specialization changes generic arguments
```

The checker must report the generic declaration and, when applicable, the
concrete specialization or receiver pattern that caused the error. A later
generator phase must not reinterpret any of these errors.

## Deferred

- User-defined traits, interfaces, and associated types.
- Explicit generic capability and constraint syntax for public contracts or
  overload selection.
- Explicit type arguments on generic function-value references; expected
  `Fun<...>` types provide the v1 inference context.
- Generic variance, subtyping, and coercion rules.
- Specialization overrides and partial specialization.
- Generic closures and captured type parameters.
- Runtime reflection and dynamic generic values.
- Compile-time value parameters, general const generics, and type-level
  arithmetic.
- Generic FFI declarations and exported ABI names.
- Ownership, move, drop, and allocator constraints for managed `T` values.

## Implementation handoff

RFC 0019 is intended to be implemented in phases after review:

1. Extend the lexer and parser for generic type declarations, function
   declarations, method receivers, generic postfix calls, inferred generic
   function references, generic object literals, generic type expressions, and
   generic ADT variant owners and match patterns.
2. Resolve generic parameters in type positions and implement canonical
   substitution, including complete-type and self-reference checks.
3. Implement deterministic structural inference for function, method,
   object-constructor, ADT-constructor, and supported generic-function-reference
   use, including pointer weakening, unions, `Nil`, nested constructed types,
   and exact function types.
4. Add distinct open-generic and closed-specialization checked nodes for
   dependent operations, with source-spanned specialization diagnostics.
5. Add specialization discovery, unchanged-argument recursion checks, generic
   receiver-overlap rejection, and stable canonical C23 names.
6. Add built-in generic integration for RFC 0020, including the separate
   `Array<T, N>` literal-length form.
7. Lower each reachable specialization to concrete C23 declarations and
   definitions in deterministic dependency-safe order.
8. Add end-to-end tests for grammar, inference, dependent operations, generic
   function references, ADT constructors, recursive pointer types, collection
   integration, diagnostics, and generated C uniqueness.

The generic core may be implemented before all operation specifications are
complete. Operation-dependent specialization tests and lowering must wait
until the relevant equality, arithmetic, hashing, and collection contracts
are stable.

RFC 0014 is a prerequisite for the complete RFC 0019 implementation because
generic type parsing, canonical substitution, and union-aware inference use
its accepted type representation. Non-union generic core work may proceed
earlier, but RFC 0019 cannot be marked implemented until the RFC 0014 type
system and its generic integration points are available.

## Acceptance criteria

Implementation is complete when focused end-to-end tests prove that:

1. generic types, aliases, functions, and methods parse with the specified
   parameter syntax;
2. type parameters resolve only in their declared scope;
3. canonical specialization identity distinguishes argument order and type;
4. type inference succeeds for unambiguous calls and constructors, including
   expected-type and initializer-based inference, and diagnoses ambiguous or
   incomplete cases;
5. explicit type arguments work in nested call, constructor, and type
   contexts, while function-value references use expected-type inference;
6. generic postfix calls are parsed without changing relational-expression
   behavior;
7. generic methods infer receiver arguments from the receiver, infer their own
   arguments from call arguments or expected results, and accept explicit
   method arguments without exposing receiver arguments at the call site;
8. overlapping generic receiver implementations and duplicate owner methods
   are rejected before specialization;
9. open generic declarations defer layout-dependent completeness checks, while
   concrete substitutions reject incomplete by-value types and invalid cycles;
10. structural inference handles nested types, pointer weakening, unions,
    `Nil`, literals, and exact function types as specified;
11. operations involving type parameters are checked against concrete
   operation availability at specialization time, with the generic-body span
   and concrete specialization included in diagnostics;
12. generic object and ADT constructors infer owner arguments only when one
   complete argument list is determined;
13. generic function references select one concrete specialization from an
    unambiguous `Fun<...>` target and reject underconstrained targets;
14. generic ADT match patterns resolve explicit or scrutinee-inferred owner
    arguments and reject a non-matching canonical owner;
15. generic objects preserve mutability, pointer capability, and union identity;
16. recursive but finite pointer-based substitutions are accepted while
    by-value cycles are rejected;
17. direct recursive function, method, and type specialization cycles with
    unchanged arguments reuse active specializations, mutual function recursion
    remains source-order restricted, and cycles that transform arguments are
    rejected;
18. built-in collection forms use RFC 0019 specialization machinery and
    `Array<T, N>` accepts only RFC 0020's positive literal length;
19. each reachable specialization generates exactly one concrete C definition
    with a stable, collision-safe name;
20. unused generic declarations generate no executable placeholder;
21. generated C contains no runtime generic erasure or unchecked fallback;
22. every new syntax node, substituted type, dependent operation, and
    generator case is handled explicitly under the fail-closed architecture;
    and
23. no arbitrary compile-time execution, unsupported generic value parameter,
    or runtime generic fallback is accepted through an unspecified path.
