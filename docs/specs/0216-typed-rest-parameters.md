# RFC 0216: Typed Rest Parameters

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Direction proposed;
  exact grammar and escape checking require re-verification before activation
- Created: 2026-09-17
- Scope: homogeneous typed rest parameters for ordinary Hexal functions,
  methods, function literals, and function values
- Coordinates with: `Slice<T>`, generic functions, function values, call
  evaluation order, temporary-root escape checking, and deferred RFC 0191
- Does not authorize: implementation, `docs/reference.md` changes, or direct
  calls to C variadic functions

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

The parameter is a read-only, call-scoped `Slice<T>`. The generated C uses an
ordinary pointer-and-length slice, never C `...` or `va_list`.

## Motivation

- Variadic APIs such as logging, formatting, aggregation, and combinators are
  useful beyond compiler-owned `print`.
- Requiring every caller to construct a named collection adds ceremony when
  the values already exist only for one call.
- A homogeneous typed rest parameter preserves static checking and the
  ordinary Hexal value model.
- C variadics erase types, apply default promotions, carry no count, and do
  not meet Hexal's safety or generic-composition goals.

This feature should advance only after real user APIs show that an explicit
`Slice<T>` parameter is materially too cumbersome. `print` alone is not enough
justification because it is already a compiler-owned variadic operation.

## Proposed syntax

```ebnf
parameter = identifier , ":" , type-expression , [ "..." ] ;

call-argument = expression , [ "..." ] ;
```

Examples:

```hexal
fun log(prefix: String, values: String...) do
    -- values has type Slice<String>
end

fun forward(values: String...) do
    log("forwarded", values...)
end
```

`...` is a rest marker in a declaration and a spread marker in a call. It is
not a general expression operator.

## Proposed semantics

### Declarations

- A declaration has at most one rest parameter.
- The rest parameter must be last.
- A rest parameter requires an explicit element type `T`.
- Inside the body, the parameter has type `Slice<T>` and is a fixed binding.
- Zero trailing arguments are valid and produce an empty slice.
- Functions, methods, local functions, anonymous function literals, and
  generic functions use the same rule.
- Constructors and compiler-owned operations do not gain rest parameters
  automatically.

### Calls

- Every ordinary trailing argument must be assignable to `T`.
- The callee and all supplied arguments evaluate exactly once, left to right.
- The compiler forms one call-scoped contiguous region containing shallow
  copies of the trailing values, then passes a read-only `Slice<T>` over it.
- A call may contain at most one spread argument, and it must be the final
  argument.
- A spread operand must have type `Slice<T>` and contributes its elements in
  order without changing their element type.
- Arguments before the spread may also contribute rest values:

```hexal
log("prefix", "first", remaining...)
```

- A mutable slice is not implicitly writable through the callee; spreading
  it produces the same read-only call view as ordinary rest arguments.

### Lifetime

- The rest slice and any temporary backing region are valid only for the
  dynamic call.
- The callee may read elements and pass the slice to another call that cannot
  retain it.
- The rest slice, a slice derived from it, or a pointer into it must not escape
  through a result, object or ADT member, collection insertion, module binding,
  task, channel, deferred action, or captured external state.
- Activation must reuse the compiler's existing temporary-root escape rules;
  it must not introduce a general lifetime or ownership system solely for
  rest parameters.
- If the existing local analysis cannot prove a use non-escaping, compilation
  fails rather than heap-allocating the argument region implicitly.

### Generics

```hexal
fun first<T>(values: T...): T | Nil do
    if values.length() == 0 then
        return nil
    end
    return values[0]
end
```

- Generic inference considers every ordinary rest argument and the element
  type of the final spread argument.
- Conflicting inferred element types use the ordinary generic-inference
  diagnostic.
- Rest arguments do not introduce heterogeneous tuple inference or implicit
  union construction.

### Function values

The proposed type spelling is:

```hexal
callback: Fun<(String...) : Nil>
```

- Rest-parameter identity is part of a function type; it is not identical to
  `Fun<(Slice<String>) : Nil>` at the Hexal call surface.
- The C representation may use the same pointer-and-length parameter ABI as a
  function accepting `Slice<String>`.
- Direct and indirect calls retain the same rest packing, spread, evaluation,
  and escape rules.

## C23 lowering direction

Hexal:

```hexal
result := sum(10, 20, 30)
```

Conceptual C23:

```c
int32_t hex_rest_values[] = {10, 20, 30};
int32_t result = hex_sum((hex_slice_Int32){
    .data = hex_rest_values,
    .length = 3,
});
```

The concrete lowering may use a compound literal when that keeps the generated
C readable and preserves evaluation order. Zero arguments use the canonical
empty-slice representation and require no allocation.

The lowering must not:

- emit a C variadic declaration;
- use `va_list`, default argument promotions, or format-string inference;
- allocate from Heap merely to assemble one call;
- hide element copies or cleanup behind a runtime helper when direct C is
  sufficient.

## Separation from C interoperability

This RFC does not permit:

```c
int printf(const char *format, ...);
```

Direct C variadic calls require separate rules for default promotions,
admissible ABI types, format contracts, and target qualification. Deferred RFC
0191 retains that problem and currently recommends a typed C wrapper unless a
real library demonstrates that wrappers are materially worse.

## Required diagnostics if activated

- Rest parameter is not final.
- More than one rest parameter is declared.
- Rest parameter has no explicit element type.
- Rest argument is not assignable to the declared element type.
- More than one spread argument appears.
- Spread argument is not final.
- Spread operand is not a `Slice<T>` compatible with the rest element type.
- A rest slice, derived slice, or pointer into its backing region escapes the
  call.
- A non-variadic function receives a spread argument.

Diagnostics should identify the declaration or call and state the violated
rest/spread rule directly; they should not expose the generated Slice ABI.

## Activation work

Before moving this RFC into active work:

1. Re-verify the current lexer, parser, checker, function-type, Slice, and
   temporary-root implementations.
2. Record at least two real non-`print` APIs that become materially clearer
   with rest parameters than with an explicit `Slice<T>`.
3. Settle the exact `Fun<(T...) : R>` grammar against the current function-type
   grammar.
4. Prove that the existing escape checker can reject every listed escape
   without a new ownership or lifetime concept.
5. Specify exact diagnostics and exhaustive validation cases.
6. Specify deterministic readable C for zero, one, many, and spread calls,
   including non-scalar element types and nested calls.
7. Audit overload-free call resolution, generic inference, methods, local
   functions, anonymous literals, `defer`, `spawn`, and evaluation ordering.
8. Update `docs/reference.md` only after implementation behavior stabilizes.

## Non-goals

- Heterogeneous tuples or parameter packs.
- Optional or default parameters.
- Named call arguments.
- Arbitrary-position spread.
- Implicit union construction from unlike rest arguments.
- Heap-owned variadic argument storage.
- C variadic declarations or calls.
- Changing compiler-owned `print` as part of this proposal.
