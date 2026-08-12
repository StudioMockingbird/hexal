# RFC 0008: Functions and Methods

- Status: Implemented
- Features: module-level function declarations, `Fun<…>` function-pointer
  types, calls, `impl` methods with implicit `self`, pointer auto-dereference,
  closed function scopes
- Created: 2026-08-07
- Revised: 2026-08-09
- Depends on: RFC 0004 (identifiers), RFC 0005 (type declarations), RFC 0006
  (objects), RFC 0007 (mutability redesign), RFC 0009 (core operators)
- Supersedes when accepted: RFC 0006's requirement that pointer member access
  always spell `.value`, and RFC 0006's source-order evaluation rule for
  object-literal initializers
- Replaces: the earlier RFC 0008 function-literal drafts

## Summary

A function declaration defines executable code. It is a statement, not a
storage declaration:

```seawitch
fun adder(dx: Int32, dy: Int32): Int32
    return dx + dy
end

total: Int32 = adder(2, 3)
```

`Fun<…>` is the type of a stored function pointer. A declared function name
produces a compatible `Fun<…>` value when used in a value position:

```seawitch
handler: Fun<(Int32, Int32) : Int32> = adder
mut active_handler: Fun<(Int32, Int32) : Int32> = adder

active_handler = alternative_adder
result: Int32 = active_handler(2, 3)
```

The distinction maps directly to C23: `fun adder` is a C function definition;
`handler` is a C object containing a function pointer. There is no function
literal, hidden function body, `mut fun`, or `:=` declaration.

A function with no return clause produces no value:

```seawitch
fun reset(counter: MutPtr<Counter>)
    counter.count = 0
end
```

`impl` is the only method declaration form. It defines code associated with a
nominal object without adding a function pointer to the object's layout:

```seawitch
impl Point.length_squared(): Int32
    return self.x * self.x + self.y * self.y
end

impl MutPtr<Point>.translate(dx: Int32, dy: Int32)
    self.x = self.x + dx
    self.y = self.y + dy
end

mut here: Point = Point { x = 0, y = 0 }
here.translate(5, 5)
distance: Int32 = here.length_squared()
```

Function and method declarations exist only at module scope. They do not
capture. A body may use its parameters, its locals, `self` where applicable,
its own declaration, and previously declared module-level functions or methods.
It cannot access a module-level data binding.

## Changes to implemented behavior

### Function declarations are statements

Every parameter carries a type, and a returned value carries a return type in
the declaration header:

```seawitch
fun scale(value: Int32, factor: Int32): Int32
    return value * factor
end
```

Ordinary storage declarations retain the language's mandatory annotation rule:

```seawitch
result: Int32 = scale(6, 7)
callback: Fun<(Int32, Int32) : Int32> = scale
```

`fun` cannot be preceded by `mut`. Executable code is not a replaceable storage
slot. A user who needs a replaceable callable declares a separate `mut Fun<…>`
binding.

### Function-pointer storage is explicit

The following declarations create storage:

```seawitch
fixed: Fun<(Int32) : Int32> = identity
mut selected: Fun<(Int32) : Int32> = identity
```

`fixed` cannot be reassigned. `selected` can be assigned another compatible
function pointer. Neither declaration defines a new function body.

A function declaration itself is not an addressable storage object. Its name
already produces the function-pointer value expected by `Fun<…>`:

```seawitch
callback: Fun<(Int32) : Int32> = identity // Valid
bad: Fun<(Int32) : Int32> = ref identity  // Error: identity is not storage
```

### Pointer member access auto-dereferences

RFC 0006 required the explicit form and rejected auto-dereference: "Explicit
`.value` remains the one obvious dereference spelling for data access." This
RFC reverses that:

```seawitch
writer: MutPtr<Point> = ref point
writer.x = 5                       // means writer.value.x = 5
```

`.value` remains required for a non-object pointee and for whole-value access:

```seawitch
number: MutPtr<Int32> = ref score
number.value = 10

writer.value = other_point
```

### Function bodies introduce the first nested scope

Parameters, `self`, and body locals live in a function scope. A local may
shadow a module-level value, but RFC 0005's rule that a value may never shadow
a visible type still holds.

This lexical scope does not make a closure. Function and method declarations
cannot appear inside a body, and a body cannot read a module-level data binding.

### New reserved words

`fun`, `return`, `end`, `impl`, and `self`. `Fun` joins `Ptr` and `MutPtr` as a
protected type name under RFC 0005. This RFC does not add `:=`.

## Guide-level explanation

### Free functions

A returning function states every parameter type and its return type:

```seawitch
fun minimum(left: Int32, right: Int32): Int32
    return left
end
```

A function that produces no value omits the return clause:

```seawitch
fun log_value(value: Int32)
    return
end
```

A bare `return` is valid only in a no-return function. `return expression` is
valid only when the declared return type accepts the expression. Until control
flow exists, a value-returning body must end with `return expression`.

### Function-pointer types

```seawitch
Fun<(Int32, Int32) : Int32>    // two parameters, returns Int32
Fun<() : Int32>                // no parameters, returns Int32
Fun<(MutPtr<Counter>)>         // one parameter, returns nothing
Fun<()>                        // nothing in, nothing out
```

The return clause is the type after `:`. Omitting it means that a call produces
no value. No `Void`, `Unit`, or `Nil` type is introduced.

A `Fun<…>` type describes a C function-pointer value. Parameter names and
parameter binding mutability are not part of its identity.

### Passing callbacks

A declared function name converts to its function-pointer value in an expected
`Fun<…>` position:

```seawitch
fun square(value: Int32): Int32
    return value * value
end

fun apply(callback: Fun<(Int32) : Int32>, value: Int32): Int32
    return callback(value)
end

result: Int32 = apply(square, 5)
```

No `ref square` is used. `ref` takes the address of storage; `square` names
code and already supplies the appropriate callable pointer.

### Replaceable callbacks

A replaceable function pointer is ordinary mutable storage:

```seawitch
fun default_handler(value: Int32): Int32
    return value
end

fun replacement_handler(value: Int32): Int32
    return value + 1
end

mut handler: Fun<(Int32) : Int32> = default_handler
handler = replacement_handler

result: Int32 = handler(10)
```

There is no hidden permanent body associated with `handler`. The two declared
functions contain code; `handler` contains only a pointer.

### Supported function-pointer positions

This RFC supports `Fun<…>` in these positions:

- a function or method parameter;
- a local binding inside a function or method;
- a parameter type inside another `Fun<…>` signature; and
- an ordinary module-level binding, with the limitation below.

A module-level `Fun<…>` binding follows ordinary module-data rules: it is
runtime storage emitted in `main`, so like every other module binding it is
**not visible from any function body**. It is usable only from module level.
Passing a callback into a function means declaring a `Fun<…>` parameter, not
reading a module-level one.

This RFC does not support:

- returning `Fun<…>` from a function;
- using `Fun<…>` as an object member;
- constructing `Ptr<Fun<…>>` or `MutPtr<Fun<…>>`;
- taking `ref` of a function declaration or `Fun<…>` binding.

Those positions require broader C declarator, addressability, object-member,
and FFI rules and are deferred. A `Fun<…>` parameter may itself appear inside
another function signature, which is required for ordinary callback-taking
functions such as `apply` above.

### Calls

Arguments are checked against parameter types. A parameter is an expected-type
position, so RFC 0003 and RFC 0009's contextual literal rules apply:

```seawitch
fun small(value: UInt8): UInt8
    return value
end

ok: UInt8 = small(200)
bad: UInt8 = small(300) // Error: 300 is outside UInt8
```

A no-return call is a statement and cannot initialize storage:

```seawitch
reset(ref counter)             // Valid
result: Int32 = reset(ref counter) // Error: reset produces no value
```

Argument evaluation follows C23. Every argument is evaluated before the called
function begins, but their relative order is unspecified. Code whose result
depends on order uses separate statements.

### Methods

`impl` declares a method on a nominal object. The target states the type of the
implicit `self` binding:

```seawitch
type Point = {
    mut x: Int32,
    mut y: Int32,
}

impl Point.length_squared(): Int32
    return self.x * self.x + self.y * self.y
end

impl Ptr<Point>.is_origin(): Bool
    return self.x == 0 and self.y == 0
end

impl MutPtr<Point>.translate(dx: Int32, dy: Int32)
    self.x = self.x + dx
    self.y = self.y + dy
end
```

`self` occupies no written parameter slot and cannot be declared, shadowed, or
assigned. It is a fixed binding for every target, including a value target, so
a method that returns a modified copy builds one explicitly:

```seawitch
impl Point.moved(dx: Int32): Point
    self.x = self.x + dx     // Error: self is fixed
    return self
end

impl Point.moved(dx: Int32): Point
    mut result: Point = self
    result.x = result.x + dx
    return result
end
```

| Target | `self` is | The method |
|---|---|---|
| `Point` | a copy | cannot affect the caller |
| `Ptr<Point>` | a read pointer | reads caller-owned storage |
| `MutPtr<Point>` | a write pointer | may write the caller's `mut` members |

Methods are not object members and add nothing to the C structure layout. A
future `Fun<…>` object member would be callback data, not a method.

### Calling methods

A method is reached through a value, never through its type name:

```seawitch
mut point: Point = Point { x = 0, y = 0 }

point.translate(5, 5)
distance: Int32 = point.length_squared()
```

The receiver is adapted by the first applicable rule:

- an exact target type is passed directly;
- `MutPtr<T>` weakens to a `Ptr<T>` target;
- `Ptr<T>` or `MutPtr<T>` dereferences to a copied `T` target; or
- an addressable `T` uses `ref` for a `Ptr<T>` or `MutPtr<T>` target, subject
  to RFC 0007.

A fixed object therefore cannot reach a `MutPtr<T>` method under RFC 0007:

```seawitch
origin: Point = Point { x = 0, y = 0 }
origin.translate(5, 5)
// Error: translate needs MutPtr<Point>; ref origin is Ptr<Point>
```

### Constructors are ordinary functions

Constructors have no receiver and use `fun`:

```seawitch
fun point_origin(): Point
    return Point { x = 0, y = 0 }
end

origin: Point = point_origin()
```

There is no `Point.origin` form. Receiver-free functions occupy the module
value namespace.

### Recursion and declaration order

A function or method declaration is visible in its own body, permitting direct
self-recursion:

```seawitch
fun factorial(value: Int32): Int32
    return value * factorial(value - 1)
end
```

Other functions and methods become visible in source order. A call to a later
declaration is an error, so mutual recursion is unavailable:

```seawitch
fun is_even(value: Int32): Bool
    return is_odd(value - 1) // Error: is_odd is not declared yet
end

fun is_odd(value: Int32): Bool
    return is_even(value - 1)
end
```

Methods are not exempt. A type's methods do not form a unit that sees itself;
each one sees only what precedes it:

```seawitch
impl Point.magnitude(): Int32
    return self.length_squared() // Error: length_squared is not declared yet
end

impl Point.length_squared(): Int32
    return self.x * self.x + self.y * self.y
end
```

Reordering fixes that pair. A genuinely mutual pair must be restructured, for
example by extracting the shared computation into an earlier free function.

Source ordering is a consequence of single-pass checking rather than a
preference. Resolving a forward call requires the callee's signature before the
caller's body is checked, which needs either a module-wide signature pass or
source-level forward declarations. Both are deferred.

### Closed function scopes

A body may read only:

- its parameters;
- `self`, for a method;
- its local bindings;
- its own function or method declaration; and
- previously declared module-level functions and methods.

It cannot access a module-level data binding:

```seawitch
mut count: Int32 = 0

fun read_count(): Int32
    return count // Error: pass count as a parameter
end
```

Functions and methods cannot be nested, so there is no enclosing local scope to
capture. Required state is passed explicitly, using `Ptr<T>` or `MutPtr<T>`
when caller-owned storage must be observed or changed.

## Reference-level explanation

### Grammar

```ebnf
top-level-item      = type-declaration | declaration | function-declaration
                    | impl-declaration | assignment | call-statement ;

function-declaration
                    = "fun" , identifier , "(" , [ parameter-list ] , ")"
                    , [ ":" , type-expression ]
                    , { body-statement } , "end" ;

impl-declaration    = "impl" , self-type , "." , identifier
                    , "(" , [ parameter-list ] , ")"
                    , [ ":" , type-expression ]
                    , { body-statement } , "end" ;

self-type           = identifier
                    | pointer-constructor , "<" , identifier , ">" ;

parameter-list      = parameter , { "," , parameter } ;
parameter           = identifier , ":" , type-expression ;

body-statement      = declaration | assignment | call-statement
                    | return-statement ;
return-statement    = "return" , [ expression ] ;
call-statement      = postfix-expression ;

type-expression     = identifier
                    | pointer-constructor , "<" , type-expression , ">"
                    | function-type ;
function-type       = "Fun" , "<" , "(" , [ type-list ] , ")"
                    , [ ":" , type-expression ] , ">" ;
type-list           = type-expression , { "," , type-expression } ;

postfix-expression  = primary-expression
                    , { "." , identifier | call-arguments } ;
call-arguments      = "(" , [ expression , { "," , expression } ] , ")" ;
primary-expression  = identifier | object-literal
                    | integer-literal | decimal-floating-literal
                    | "true" | "false" | "(" , expression , ")" ;
```

A `call-statement` is a `postfix-expression` whose final postfix operation is a
call. This is a constraint on the chain rather than a separate production,
because a trailing `call-arguments` cannot be factored out of the repetition
without making `point.translate(5, 5)` ambiguous. A chain ending in member
selection is an expression, never a statement.

The ordinary `declaration` production remains RFC 0007's mandatory annotated
form. This RFC adds no inference declaration and no `:=` token.

Function and method parameters are always annotated. Omitting the return clause
means the declaration returns no value.

Function and method declarations are accepted only as `top-level-item`. A
`fun` or `impl` token in a body produces a dedicated module-scope diagnostic.

**A call's `(` must not be separated from its callee by a newline.** This keeps
the terminator-free grammar from merging:

```seawitch
result: Int32 = compute
(value)
```

Line-breaking inside the argument list is unaffected.

When `return` carries an expression, the expression's first token must be on
the same line as `return`. Otherwise `return` is bare. This keeps the optional
expression deterministic in a terminator-free body:

```seawitch
return
cleanup()
```

The example is a bare return followed by a call statement, never
`return cleanup()`.

### Function declaration rules

1. A function declaration introduces one callable name in the module value
   namespace.
2. Its name cannot conflict with any visible type or value under RFC 0005.
3. Every parameter has an explicit type. Parameter names are unique within the
   function scope.
4. Parameters are fixed bindings in this RFC. They cannot be assigned. `mut`
   parameters are deferred.
5. The optional return clause determines whether calls produce a value.
6. The function name is bound after its complete signature is resolved and
   before its body is checked, permitting direct self-recursion.
7. A function declaration is not a storage place, cannot be assigned, cannot be
   preceded by `mut`, and cannot be the operand of `ref`.
8. In a value context, its name has the corresponding `Fun<…>` type.

### Function-pointer rules

1. A `Fun<…>` type is identified by its parameter types in order and its return
   type, or the absence of one. Parameter names are not part of the type.
2. Two `Fun<…>` types are identical when their canonical parameter and return
   types are identical after RFC 0005 alias resolution.
3. `Fun<…>` storage follows the ordinary binding rule: without `mut` its pointer
   slot is fixed; with `mut` the slot may be assigned another identical
   function-pointer type.
4. A declared function name is assignable to the identical `Fun<…>` type.
5. A `Fun<…>` value is callable with the same argument and return rules as a
   declared function.
6. This RFC permits `Fun<…>` only in the positions listed under Supported
   function-pointer positions. Any other constructed position is a type error.
7. Function declarations and `Fun<…>` bindings are not addressable in this RFC.

### Call rules

1. A callee is either a declared function name, a supported `Fun<…>` binding,
   or a method selected through a receiver.
2. The argument count must equal the parameter count.
3. Each argument must be assignable to its parameter under RFC 0007's outermost
   `MutPtr<T>` to `Ptr<T>` weakening rule.
4. Parameter positions provide expected types to literals under RFCs 0003 and
   0009.
5. A returning call has the declared return type.
6. A no-return call is valid only as a call statement and has no value type.
7. `return expression` requires an enclosing returning declaration and an
   expression assignable to its return type.
8. Bare `return` requires an enclosing no-return declaration.
9. Until general control flow exists, a returning body must end with a return
   statement.

### Auto-dereference

For a receiver whose type is `Ptr<T>` or `MutPtr<T>`, where `T` resolves to an
object with member `m`, `pointer.m` means `pointer.value.m`.

1. It applies one layer at a time. `Ptr<Ptr<Point>>` does not directly reach
   `x`; write `pointer.value.x`.
2. The built-in `value` property wins on a pointer receiver. An object member
   named `value` is reached as `pointer.value.value`.
3. It is language-wide and applies to reads, writes, and `ref` place walks.
4. Writability follows RFC 0007: `pointer.m` is writable exactly when the
   inserted `.value` is writable and `m` is declared `mut`.

### Declaration order

The checker processes module-level items once in source order:

1. resolve a function or method's complete signature;
2. bind that declaration;
3. check its body using declarations visible at that source position; and
4. continue to the next item.

This admits self-recursion but rejects later-function calls and mutual
recursion. No module-wide signature pass exists.

### Method rules

1. An `impl` target is `T`, `Ptr<T>`, or `MutPtr<T>`, where `T` is a declared
   nominal object type after alias resolution. Built-in, pointer, and function
   types cannot be targets.
2. The method is associated with `T`, whichever receiver form is used.
3. `self` is a fixed implicit binding with the target type. It occupies no
   written parameter slot and cannot be declared, shadowed, or assigned.
4. At most one method of a given name exists on `T` across all three target
   forms.
5. A method name cannot equal a member name of `T`.
6. Methods are not values and cannot initialize a `Fun<…>` binding.
7. Receiver adaptation follows the ordered rules in Calling methods.
8. An `impl` declaration is visible in its own body and then to later source
   items only.

### Scope rules

1. Parameters, `self`, and body locals belong to the function or method scope.
2. A local may shadow a module-level value but never a visible type.
3. Duplicate declarations within one scope are errors.
4. A body may reference its parameters, `self`, locals, its own declaration,
   and previously declared module-level functions or methods.
5. A body cannot reference a module-level data binding, including a module-level
   `Fun<…>` binding.
6. Function and method declarations cannot appear inside a body.

### Evaluation order

Seawitch inherits the sequencing rules of the equivalent generated C23
expression. It adds no general left-to-right guarantee.

1. Full statements execute in source order.
2. `and` and `or` retain RFC 0009's left-to-right short-circuit semantics and
   lower to C's `&&` and `||`.
3. Function arguments may evaluate in any order. Every argument is evaluated
   before the called function begins, and function executions do not interleave.
4. A method receiver and its arguments have the same unspecified relative order
   as the generated C operands.
5. Ordinary operator operands may evaluate in any order except where another
   RFC explicitly says otherwise.
6. Object-literal initializer expressions may evaluate in any order, matching
   C23 initialization lists. This supersedes RFC 0006's source-order rule now
   that calls make effects observable.

Code whose result depends on evaluation order uses separate statements. The
generator does not introduce temporaries merely to impose an order absent from
C23.

### C23 lowering

A free function declaration lowers to one `static` C function:

```seawitch
fun adder(dx: Int32, dy: Int32): Int32
    return dx + dy
end
```

```c
static int32_t sw_f_adder(const int32_t sw_v_dx, const int32_t sw_v_dy) {
    return (int32_t)(uint32_t)((uint32_t)sw_v_dx + (uint32_t)sw_v_dy);
}
```

A no-return declaration lowers to `void`.

A method lowers to a `static` C function whose first parameter is `self`:

```seawitch
impl MutPtr<Point>.translate(dx: Int32)
    self.x = self.x + dx
end
```

```c
static void sw_f_Point_translate(
    sw_t_Point *const sw_v_self,
    const int32_t sw_v_dx
) {
    sw_v_self->sw_m_x =
        (int32_t)(uint32_t)((uint32_t)sw_v_self->sw_m_x + (uint32_t)sw_v_dx);
}
```

A `Point` target passes a fixed structure copy. A `Ptr<Point>` target passes
`const sw_t_Point *const`; a `MutPtr<Point>` target passes
`sw_t_Point *const`. Parameters are fixed bindings, so generated parameter
declarators carry top-level `const` where C permits it.

A declared function name renders as its private C function symbol in a value
position. A stored `Fun<…>` value lowers to a C function-pointer object:

```seawitch
callback: Fun<(Int32) : Int32> = identity
mut selected: Fun<(Int32) : Int32> = identity
```

```c
int32_t (*const sw_v_callback)(int32_t) = sw_f_identity;
int32_t (*sw_v_selected)(int32_t) = sw_f_identity;
```

The pointer type carries unqualified parameters even though `sw_f_identity`
declares `const int32_t`. That assignment is valid: C ignores top-level
parameter qualifiers when comparing function types, so the two are compatible.
The generator must not "correct" this by emitting `int32_t (*)(const int32_t)`
— the qualifier belongs to the definition's local binding, not to the type.

Module-level storage declarations remain inside generated `main`. Function and
method definitions are emitted at file scope in source order, after object type
definitions and before `main`. A body cannot reference module storage, so no
storage-duration promotion or initialization function is required.

Function-pointer parameters use ordinary C pointer declarators:

```c
static int32_t sw_f_apply(
    int32_t (*const sw_v_callback)(int32_t),
    const int32_t sw_v_value
) {
    return sw_v_callback(sw_v_value);
}
```

Bodies preserve `#line` mappings. No cross-function C prototype region is
emitted because only self-recursion and calls to earlier definitions are valid.

**Method names use a checked injective namespace.** `_` is legal in Seawitch
identifiers, so no separator distinguishes every source pair by spelling alone:

```text
impl Point.translate  -> sw_f_Point_translate
Point_translate       -> sw_f_Point_translate
```

The checker rejects free functions and methods that produce the same private C
spelling, and it also rejects collisions between distinct object/method pairs.
Accepted declarations therefore have unique `sw_f_` names while retaining the
simple source-derived spelling above.

### Diagnostics

```text
[Syntax Error] a call's ( must follow its callee on the same line
[Syntax Error] a return value must begin on the same line as return
[Syntax Error] expected end to close function adder
[Syntax Error] function declarations are module-level only
[Syntax Error] impl declarations are module-level only
[Syntax Error] function parameters require type annotations
[Syntax Error] mut cannot modify a function declaration; declare a mut Fun binding
[Type Error] adder expects 2 arguments, got 3
[Type Error] reset produces no value
[Type Error] return requires a value; adder declares Int32
[Type Error] adder must end with a return statement
[Type Error] cannot assign to parameter value; parameters are fixed bindings
[Type Error] cannot assign to self; self is a fixed binding
[Type Error] handler requires Fun<(Int32) : Int32>, got Fun<(UInt32) : UInt32>
[Type Error] function declarations are not addressable; use adder as a Fun value
[Type Error] Ptr<Fun<(Int32) : Int32>> is not supported
[Type Error] Fun<…> object members are not supported
[Type Error] function read_count cannot access module data binding count; pass it as a parameter
[Type Error] unknown function is_odd; functions must be declared before use
[Type Error] self is not bound outside an impl body
[Type Error] Int32 is not a nominal object type; impl requires an object
[Type Error] Point already has a method named translate
[Type Error] Point already has a member named x
[Type Error] Point has no method named rotate
[Type Error] translate needs MutPtr<Point>; ref origin is Ptr<Point>
[Type Error] free function Point_translate collides with impl Point.translate
```

The lexer owns the new keywords. The parser owns malformed headers and bodies,
the same-line call rule, mandatory parameter annotations, and module-only
placement of `fun` and `impl`. The checker owns declaration order, supported
`Fun<…>` positions, function-pointer compatibility, calls, returns, scope,
module-data access, and method rules.

## Deferred

- parameter `mut` and its binding-only semantics;
- nested functions, function literals, anonymous functions, and closures;
- returning `Fun<…>` values;
- `Fun<…>` object members;
- `Ptr<Fun<…>>`, `MutPtr<Fun<…>>`, and addressable function-pointer storage;
- methods as first-class values and method callbacks;
- module/global data storage visible from functions;
- forward function declarations and mutual recursion;
- overloading, default arguments, named arguments, and variadics;
- generic functions and generic methods;
- methods on built-in, pointer, alias, and function types;
- a namespace for receiver-free associated functions;
- multiple return values and error returns;
- separate compilation;
- exported C names, foreign functions, and C calling-convention declarations;
  and
- compile-time evaluation.

## Alternatives considered

### Define functions with storage declarations and literals

```seawitch
adder: Fun<(Int32, Int32) : Int32> = fun (dx, dy)
    return dx + dy
end
```

Rejected. A C function definition is executable code, not an object containing
the function. Treating both as one declaration created hidden body symbols,
three signature-placement forms, unclear addressability, and a special mutable
function lowering. `fun name(…) … end` maps directly to a C function definition;
`name: Fun<…> = function` separately maps to function-pointer storage.

### Use `:=` to infer ordinary declarations

Rejected. Seawitch retains mandatory type annotations for storage. The function
declaration header already states its signature, so functions do not need an
inference declaration either.

### Namespace functions under the type as `Point.origin`

Rejected. A constructor has no receiver and remains an ordinary free function.
`impl` is reserved for methods with an implicit receiver.

### Declare the receiver as a written `self` parameter

```seawitch
impl Point.translate(self: MutPtr<Point>, dx: Int32)
```

Rejected. The impl target already states `self`'s type. Repeating it adds no
information and makes receiver adaptation less explicit in the declaration.

### Give every method a fixed receiver kind

Rejected. `Point`, `Ptr<Point>`, and `MutPtr<Point>` state three materially
different contracts: copy, read caller storage, and write caller storage.

### Keep `.value` mandatory on pointer member access

Rejected here, having been accepted by RFC 0006. Every mutating pointer method
would read `self.value.x = self.value.x + dx`. The rule replacing it is one
automatic object-member dereference layer.

### Introduce `Unit`, `Void`, or `Nil` for no-return functions

Rejected. Omitting the return clause introduces no value type and makes binding
a no-return call a natural error.

### Infer parameter or return types from the body

Rejected. It requires unification, makes signatures depend on implementations,
and prevents binding a function before checking its body.

### Allow nested functions without closures

Rejected. C23 has no nested function definitions. Hoisting even non-capturing
nested functions requires synthesized scope-qualified names and another
declaration-placement model.

### Collect signatures for forward calls and mutual recursion

Rejected. It adds a module-wide prepass and C prototype region. A function may
call itself or any earlier function; other cycles must be restructured.

### Guarantee left-to-right expression evaluation

Rejected. C23 leaves most operand and argument order unspecified. Imposing an
order would require temporary and control-flow lowering throughout expression
trees. Order-dependent code uses separate statements.

### Expose module-level data as C globals

Deferred. Current module data executes as storage local to generated `main`.
Moving it to file scope changes storage duration and requires non-constant
initialization rules. A later globals RFC can introduce that explicitly.

## Acceptance criteria

Implementation is complete when end-to-end tests prove that:

1. `fun name(parameters) : Return … end` declares one module-level function and
   every parameter requires an explicit type;
2. a function declaration cannot use `mut`, `=`, `:=`, or appear inside a body;
3. returning and no-return declarations enforce their corresponding `return`
   forms, and binding a no-return call is rejected;
4. calls check arity and argument types, including RFC 0007 pointer weakening
   and RFC 0003 contextual literals;
5. a declared function name initializes or is passed to an identical `Fun<…>`
   position without `ref`;
6. a function declaration is not assignable or addressable;
7. fixed and `mut` `Fun<…>` bindings respectively reject and accept pointer-slot
   reassignment without defining another body;
8. a `Fun<…>` parameter is callable and lowers to a C function-pointer
   parameter;
9. unsupported `Fun<…>` returns, members, pointer constructions, and `ref`
   operations fail with structured diagnostics;
10. self-recursion resolves, calls to earlier functions resolve, and calls to
    later functions fail without signature hoisting;
11. a body may use parameters, `self`, locals, its own declaration, and earlier
    functions or methods, while module data access is rejected;
12. assigning to a parameter or to `self` is rejected, including for a value
    receiver, while a local copy of `self` is assignable;
13. `impl T.name`, `impl Ptr<T>.name`, and `impl MutPtr<T>.name` bind the stated
    implicit receiver and add no object-layout member;
14. duplicate methods across receiver forms and member/method conflicts are
    rejected;
15. receiver adaptation covers exact, weakening, copy, and address-taking cases;
16. auto-dereference resolves one object-pointer layer and preserves RFC 0007
    writability, while explicit `.value` remains available;
17. a call whose `(` follows a newline is a syntax error, and a return
    expression begins on the same line as `return`;
18. evaluation inherits C23 order, preserves full-statement sequencing and
    `and`/`or` short-circuiting, and emits no ordering temporaries;
19. free functions and methods lower to source-ordered `static` C definitions,
    with fixed parameter bindings reflected by top-level `const`;
20. fixed and mutable `Fun<…>` storage lowers respectively to fixed and mutable
    C function-pointer objects inside the ordinary generated scope, and a
    function-pointer type's unqualified parameters remain compatible with a
    definition whose parameters are `const`;
21. every new token, syntax node, checked node, and unsupported `Fun<…>` position
    is handled explicitly and fails closed; and
22. representative free functions, callback parameters, mutable callback
    selection, all receiver forms, and generated C lowering pass Go-level
    end-to-end assertions. External C toolchain testing is out of scope.

## Implementation handoff

The plan must identify:

1. lexer additions for `fun`, `return`, `end`, `impl`, and `self`, without a
   `:=` token;
2. parser nodes for function declarations, method declarations, parameters,
   calls, and returns, with `fun` and `impl` accepted only at module scope;
3. mandatory parameter annotations, the same-line rule for a call's opening
   parenthesis, and the same-line distinction between bare and valued returns;
4. a canonical `Fun<…>` type identity containing parameter types and optional
   return type;
5. distinct checked representations for a function declaration, a function
   reference, and an ordinary binding whose stored type is `Fun<…>`;
6. explicit validation of the supported and deferred `Fun<…>` positions;
7. single-pass source-order checking that binds a function or method immediately
   before its body and does not collect later signatures;
8. a scoped environment with fixed parameters, implicit `self`, local
   declarations, and rejection of module-data reads;
9. call and return checking shared by declared functions, stored `Fun<…>`
   values, and methods;
10. a method table keyed by canonical nominal identity, including receiver-form
    and member-name conflicts;
11. receiver adaptation and pointer auto-dereference reusing RFC 0007 place,
    `ref`, weakening, and dereference operations;
12. generator support for source-ordered file-scope function definitions,
    function-pointer bindings and parameters, fixed parameter declarators,
    `void` returns, calls, and `->` member access;
13. direct C23 expression lowering without ordering temporaries;
14. focused lexer, parser, checker, type, and generator tests; and
15. end-to-end coverage in the facet-named `compiler/` tests for every
    acceptance criterion. Generated C is checked through Go-level output
    assertions; external C toolchains are out of scope.

No analyzer pass or statement buffer is required. Checked calls remain
structured until the generator renders them directly into C23 expressions.
Introduce an analyzed representation only when a later feature needs one that
is genuinely distinct from checked syntax.
