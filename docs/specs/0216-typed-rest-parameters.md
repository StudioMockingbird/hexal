# RFC 0216: Typed Rest Parameters

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; not scheduled
- Created: 2026-09-17
- Updated: 2026-09-17
- Scope: homogeneous typed rest parameters for Hexal functions, methods,
  anonymous function literals, and function values
- Coordinates with: `Slice<T>`, generic inference, function values, call
  evaluation order, `defer`, and `spawn`
- Does not authorize: direct C variadic declarations or calls

## Summary

Allow one final typed rest parameter:

```hexal
fun sum(values: Int32...): Int32 do
    mut total := 0
    for value in values do
        total = total + value
    end
    return total
end

result := sum(10, 20, 30)
```

`T...` is call-boundary sugar for accepting zero or more explicit `T`
arguments. Inside the function, the parameter is a fixed, read-only
`Slice<T>`. The invocation owns the compiler-created backing region and
reclaims that region automatically when the invocation ends. Rest elements
are shallow copies; ownership of storage or resources referenced by an element
is not transferred.

Generated C uses the ordinary pointer-and-length Slice representation. This
RFC never uses C `...`, `va_list`, an implicit `List<T>`, or a hidden Heap
allocation.

## Motivation

Homogeneous aggregation APIs should not require callers to construct a named
array, Slice, or List when the values exist only as inputs to one call:

```hexal
log("request", method, path, status)
minimum(8, 3, 5, 1)
```

Typed rest parameters retain static element checking, ordinary contextual
typing, deterministic evaluation, and a count. C variadics provide none of
those properties.

## Syntax

The lexer recognizes `...` as one `ellipsis` token by longest match. Existing
`.` member selection is unchanged.

```ebnf
parameter = identifier , ":" , type-expression , [ "..." ] ;

function-type-parameter-list = function-type-parameter ,
                               { "," , function-type-parameter } ;
function-type-parameter = type-expression , [ "..." ] ;
```

The existing call grammar is unchanged:

```ebnf
call-arguments = same-line "(" , [ argument-list ] , ")" ;
argument-list = argument , { "," , argument } , [ "," ] ;
argument = [ identifier , "=" ] , expression ;
```

There is no call-site spread syntax. An ellipsis after a call argument is a
syntax error. Every rest element is written as an explicit argument
expression.

Examples of function types:

```hexal
consumer: Fun<(String...)>
parser: Fun<(String, Byte...) : Int32>
```

The first function returns no value. `Fun<(String...) : Nil>` instead denotes
a function that returns the `Nil` value, following the existing distinction
between no result and a `Nil` result.

## Declaration semantics

- A signature has at most one rest parameter, and it is final.
- A rest parameter always has an explicit element type `T`; parameter type
  inference remains unavailable.
- `T` must be a complete, shallow-copyable type valid in both Slice-element
  and function-parameter positions. A type containing `Atomic` is invalid.
- Inside the body, the rest name is a fixed `Slice<T>` binding. It supports the
  existing read-only Slice operations, indexing, slicing, and iteration.
- The rule applies uniformly to module-level functions, methods, anonymous
  function literals, generic functions, and generic function literals.
- Hexal has no local named function declarations; this RFC does not add them.
- Foreign declarations, constructors, and compiler-owned operations do not
  acquire rest parameters. `print` keeps its existing independent contract.

For a method, the receiver is not part of the written parameter list and does
not affect which written parameter must be final.

## Call semantics

For a signature with `m` fixed parameters followed by `rest: T...`, a valid
call supplies at least `m` arguments. Arguments `m + 1` onward are rest
elements. Supplying exactly `m` arguments produces an empty `Slice<T>`.

- Fixed arguments use their existing expected parameter types.
- Every rest argument is checked in expected `T` context and must be assignable
  to `T` under the ordinary argument-assignment rules.
- No implicit heterogeneous tuple, union construction, array conversion, or
  List construction occurs.
- The callee and every written argument evaluate exactly once, left to right.
- Packing occurs only after all values that precede an element have completed
  evaluation. It introduces no additional source-visible evaluation.
- Direct function calls, method calls, anonymous-literal calls, and indirect
  `Fun` calls have identical arity, typing, packing, and lifetime semantics.
- Named arguments remain invalid for ordinary functions and methods under the
  existing rule. This RFC does not assign labels to rest elements.

An ordinary non-rest signature retains exact arity.

## Backing-region lifetime and ownership

Each rest invocation owns one compiler-created contiguous backing region. The
region contains shallow copies of the supplied rest values and remains valid
through the complete dynamic invocation, including cleanup actions executed
before that invocation returns. The region is reclaimed automatically; it has
no user-visible allocator and runs no element cleanup.

The rest `Slice<T>` is a non-owning descriptor over that invocation-owned
region. Copying the descriptor does not transfer or extend the region's
lifetime. A rest element is an ordinary shallow-copied `T`; copying an element
out of the Slice follows the same ownership and aliasing rules as any other
by-value `T` parameter.

### Rest-backed provenance

The checker records compiler-internal rest-backed provenance on the rest Slice,
fixed local aliases of it, and Slices derived from either. This provenance is
not a new type, qualifier, ownership mode, or general borrow system.

The following uses are valid:

- reading `length`, indexing, slicing, and iteration;
- binding the descriptor or a derived Slice to another fixed local;
- copying an element into any position where an ordinary `T` value is valid;
- returning or storing such an element when the ordinary rules for `T` permit
  it.

The following uses are rejected because they could retain an invocation-owned
address:

- returning the rest Slice or a Slice derived from it;
- initializing a mutable binding from either descriptor;
- assigning either descriptor to an existing binding;
- placing either descriptor in an object, ADT payload, union, Array, List,
  Dict, Channel, Task argument, module binding, or other aggregate/storage
  position;
- passing either descriptor as an argument or receiver to a user function,
  method, function value, foreign declaration, or retaining compiler-owned
  operation;
- taking an address of the descriptor, an element, or a place derived from an
  element;
- capturing either descriptor in `defer`, `errdefer`, or `spawn`.

Existing non-retaining Slice operations are the only calls that may receive a
rest-backed descriptor. If the checker cannot classify a use as one of the
valid operations above, it rejects the use. It does not allocate to extend the
lifetime.

This rule is intentionally narrower than the ordinary Slice contract, under
which backing-storage lifetime remains the programmer's responsibility. It
does not alter ordinary Slice values.

## Generic inference

```hexal
fun first<T>(values: T...): T | Nil do
    if values.length() == 0 then
        return nil
    end
    return values[0]
end
```

- Inference considers every explicit rest argument against the one rest
  element type.
- Fixed and rest arguments contribute to the same substitution and must
  reconcile under the existing generic-inference rules.
- Conflicting rest candidates produce the ordinary conflicting-inference
  diagnostic; they do not infer a union.
- If zero rest arguments leave a type parameter unresolved, the call produces
  the ordinary cannot-infer diagnostic unless another fixed argument or
  existing contextual rule resolves it.
- Specialization preserves whether the final parameter is rest.

## Function values and type identity

Rest mode is part of `Fun` identity:

```hexal
Fun<(String...)>
Fun<(Slice<String>)>
```

These types are distinct and are not assignable to one another. The first is
called with zero or more explicit `String` values; the second is called with
exactly one `Slice<String>` value. Function references, anonymous literals,
expected function-literal types, generic specialization, imported signatures,
and indirect calls preserve this distinction.

At the C boundary, both may use the same final pointer-and-length Slice
parameter representation. C representation equivalence does not imply Hexal
type equivalence.

## `defer` and `errdefer`

A deferred rest call has ordinary defer timing:

```hexal
defer log("exit", code(), message())
```

- The callee and every explicit argument evaluate and are shallow-captured
  exactly once, left to right, when the defer statement executes.
- The backing region is assembled from the captured rest values when the
  deferred action executes.
- The region remains valid for that invocation and is reclaimed when the
  deferred call returns.
- A rest-backed descriptor from an enclosing rest parameter cannot itself be
  captured, as specified by the provenance rules.

`errdefer` uses the same capture and packing rules and differs only in its
existing execution condition.

## `spawn`

A spawned rest call has ordinary spawn timing and task isolation:

```hexal
task := spawn classify(prefix, first, second)
```

- The callee and every explicit argument evaluate exactly once, left to right,
  before task creation.
- Fixed arguments and shallow copies of all rest elements are copied into
  scheduler-owned task argument storage.
- The task entry adapter constructs the rest Slice over storage inside that
  task frame. It never copies a descriptor that points into the spawning
  function's stack.
- The backing region remains valid until the spawned function returns and is
  reclaimed with the task argument frame.
- Existing task-argument eligibility applies independently to `T` and every
  fixed parameter.
- A rest-backed descriptor from an enclosing invocation cannot be transferred
  to the task.

Because rest count is a call-site property, generated task argument-frame and
entry-adapter identity includes the concrete target specialization and rest
count. Multiple spawn sites with different rest counts do not share an
incompatible frame layout.

## C23 lowering

Every rest declaration lowers its final parameter as one ordinary read-only
Slice value. Conceptually:

```hexal
result := sum(10, 20, 30)
```

becomes:

```c
int32_t hex_rest_1[] = {10, 20, 30};
int32_t result = hex_sum((hex_slice_Int32){
    .data = hex_rest_1,
    .length = 3,
});
```

The concrete lowering obeys these rules:

- zero rest elements use the canonical empty Slice (`nullptr`, length zero)
  and declare no zero-length array;
- one or more elements use fixed-size automatic storage whose length is the
  statically known number of written rest arguments;
- effectful callees and arguments are first captured in typed temporaries as
  required by the existing left-to-right sequencing pass;
- the array initializer uses those checked values in source order;
- nested calls receive independent backing regions;
- non-scalar elements use ordinary shallow C value initialization;
- direct, method, deferred, indirect, and spawned calls use the same Slice ABI;
- spawned frames store elements inline and construct their Slice descriptor in
  the task entry adapter;
- no variable-length array, `alloca`, C variadic declaration, `va_list`, Heap
  allocation, List allocation, or rest-specific runtime helper is emitted.

The scheduler's existing task-frame allocation is not an implicit allocation
introduced by rest packing; rest elements become part of the frame already
required by `spawn`.

## Separation from C interoperability

Foreign declarations cannot use Hexal rest syntax. Direct C variadic support
requires separate rules for default promotions, admissible ABI types, format
contracts, and target qualification. RFC 0191 retains that problem.

## Diagnostics

The earliest proving phase owns each failure:

| Condition | Required diagnostic |
| --- | --- |
| A declaration or `Fun` type has a parameter after `T...` | Syntax Error: `rest parameter must be final` |
| `...` follows a call argument | Syntax Error: `spread arguments are not supported; pass explicit values` |
| `...` occurs outside a final parameter/type position | Syntax Error naming the expected parameter or function-type syntax |
| `T` is not complete and shallow-copyable in the required positions | Type Error: `<T> is not a valid rest element type` |
| A call supplies fewer than the fixed parameter count | Type Error: `<name> expects at least <m> arguments; got <n>` |
| Rest argument `i` is not assignable to `T` | Type Error: `rest argument <i> requires <T>; got <U>` |
| A rest-backed descriptor enters a prohibited position | Type Error: `rest-backed Slice cannot escape its function invocation` |
| A mutable binding is initialized from a rest-backed descriptor | Type Error: `rest-backed Slice requires a fixed local alias` |
| Rest and non-rest function identities are mixed | Existing exact function-type mismatch diagnostic, displaying both `Fun` spellings |

`i` is the one-based written call-argument position, including fixed
arguments. Diagnostics use Hexal names and types and never mention generated C
arrays, frames, or adapters.

## Compiler representation and implementation sequence

Implementation follows the existing forward-only pipeline:

1. Add one longest-match `Ellipsis` lexer token without changing `Dot`.
2. Add `Rest bool` to parsed parameters and the final function-type parameter;
   calls gain no spread field or alternate argument node.
3. Add final-rest metadata to canonical `Fun` signatures and their keys,
   display names, equality, substitution, imported/exported records, and
   canonical validation.
4. Resolve a declared rest element `T` once, validate its positions, and bind
   the body name as `Slice<T>` while retaining rest metadata on the checked
   declaration parameter.
5. Extend direct, qualified, generic, method, literal, and indirect call
   checking to split fixed and rest arguments, context-check each rest element,
   and record the fixed/rest boundary on the checked call.
6. Add rest-backed provenance to checked operands/bindings, propagate it only
   through fixed aliases and Slice derivation, and reject the exhaustive escape
   positions above in the checker.
7. Extend the existing sequencing pass and call renderer to emit fixed-size
   backing storage and one Slice argument without changing source evaluation
   order.
8. Extend deferred-call captures to retain each explicit rest value and pack at
   execution.
9. Key spawn frames/adapters by target specialization plus rest count; store
   rest values inline and construct the descriptor inside the task entry.
10. Extend generator validation to fail closed on inconsistent rest metadata,
    arity, element types, provenance, frame layout, or Slice ABI.
11. Synchronize `docs/reference.md` once behavior is stable, then update this
    RFC's status and archive it only after every Validation item passes.

No analyzer package, runtime ownership registry, general lifetime system,
implicit allocator, or List lowering is introduced.

## Required implementation sweep

The implementation must inventory and update every existing path that carries
or consumes function signatures and call arguments:

- lexer token names and parser synchronization;
- named functions, methods, anonymous literals, generic templates, and their
  checked declaration records;
- canonical `Fun` construction, display, equality, substitution, position
  eligibility, module export/import, and function-reference checking;
- direct, qualified, method, indirect, generic, deferred, errdeferred, and
  spawned calls;
- left-to-right sequencing, local helper discovery, walkers, generator
  preflight validation, declarations, definitions, and call rendering;
- Slice type creation, read-only operations, indexing/slicing/iteration, and
  checked provenance propagation;
- diagnostic token ownership and source coordinates.

Foreign signatures, constructors, compiler-owned operations, and `print` must
be inspected and remain non-rest.

## Reference synchronization

After implementation behavior stabilizes, update `docs/reference.md` in one
change:

- extend `parameter` and function-type parameter grammar with final `...`;
- state that call grammar has no spread expression;
- replace exact arity with fixed/rest arity for rest signatures while retaining
  exact arity for all others;
- define body `Slice<T>`, element eligibility, shallow-copy packing, evaluation
  order, invocation-owned lifetime, and exhaustive escape restrictions;
- define generic inference, `Fun` identity, `defer`, `errdefer`, `spawn`, and
  C23 lowering;
- retain the prohibition on C variadic declarations and calls.

No reference edit is made by this proposal-only revision.

## Validation

This section is the exhaustive definition of done for RFC 0216.

### Lexer and parser

1. `...` lexes as one token while `a.b` still lexes with one `Dot`.
2. A named function, method, anonymous function literal, and `Fun` type parse
   one final rest parameter and preserve its marker.
3. A parameter after a rest parameter is rejected with `rest parameter must be
   final`; this also covers attempts to declare two rest parameters.
4. `call(value...)` is rejected with `spread arguments are not supported; pass
   explicit values`, and ellipsis in any other expression position is rejected
   as syntax.

### Checker and canonical types

5. A rest-only function accepts zero, one, and three exact elements; a function
   with fixed parameters accepts those same rest counts after all fixed
   arguments, and fewer than the fixed count is rejected.
6. The body binding is fixed `Slice<T>`; read, length, indexing, slicing,
   iteration, and a fixed local alias check successfully.
7. One mismatched rest element reports its one-based written position and the
   required and actual types. Contextual integer and decimal literals use `T`.
8. An incomplete element and an element containing `Atomic` are rejected as
   invalid rest element types.
9. Named functions, methods, anonymous literals called directly, and matching
   function values use rest arity. `Fun<(T...) : R>` is distinct from
   `Fun<(Slice<T>) : R>`, and assignment between them is rejected.
10. Generic inference succeeds from one and multiple agreeing rest elements,
    rejects conflicting candidates without creating a union, and reports the
    ordinary unresolved inference error for zero elements when no fixed
    argument resolves `T`.
11. An exported rest function called from another source module retains its
    rest signature and checks successfully.
12. Returning a rest-backed descriptor, returning a derived Slice, creating a
    mutable alias, assigning it, storing it in each aggregate/storage family,
    passing it to an ordinary Slice parameter, taking an address into it, and
    capturing it in defer, errdefer, or spawn each produce the required escape
    diagnostic.
    Returning and storing a copied element remains valid when ordinary `T`
    rules permit it.
13. Existing exact-arity behavior and diagnostics for non-rest functions remain
    unchanged. Foreign declarations and calls remain non-rest.

### Generated C text

14. A rest declaration has one final Slice ABI parameter and contains no C
    variadic syntax.
15. Zero rest elements pass the canonical empty Slice and declare no array;
    one and three elements emit fixed-size automatic regions with exact lengths
    and source-ordered initializers.
16. Effectful fixed/rest arguments and nested rest calls appear in generated C
    temporaries in exact source order and execute once. A non-scalar eligible
    element is initialized by shallow value copy.
17. A deferred rest call captures explicit values at registration and creates
    its backing region at execution before invoking the target.
18. A spawned rest call stores elements inline in scheduler-owned frame storage;
    its entry adapter constructs the Slice from that copied frame, and generated
    task storage contains no pointer into the spawning stack. Two spawn sites
    targeting the same function with different rest counts use compatible,
    distinct layouts.
19. Rest lowering emits no VLA, `alloca`, `va_list`, Heap/List allocation, or
    rest-specific runtime helper.
20. Generator preflight rejects a forged checked tree whose rest boundary,
    arity, element type, or spawn-frame metadata disagrees with its canonical
    signature.

### End-to-end gates

21. Integration tests compile direct, method, generic, anonymous-literal,
    indirect-function-value, deferred, and spawned rest calls through the
    exported `compiler.Compile` API and assert the required C text.
22. The qualified Linux/Clang C23 gate compiles and runs one fixture covering
    zero/one/many rest values, generic inference, an indirect call, a deferred
    call, and two spawned calls with different rest counts; observed results
    prove element order and exactly-once evaluation.
23. `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass. The
    snippet SHA manifest remains byte-identical unless an existing catalog
    snippet is deliberately changed to use rest syntax; any deliberate change
    follows the manifest rebuild and artifact-review procedure.

## Non-goals

- Call-site spread or forwarding an existing Slice as multiple arguments.
- Heterogeneous tuples or parameter packs.
- Optional, default, or named function arguments.
- Implicit union construction from unlike rest arguments.
- A mutable rest Slice.
- Element ownership transfer or element cleanup at function return.
- Heap-owned variadic storage or implicit `List<T>` construction.
- C variadic declarations or calls.
- Changing compiler-owned `print`.
