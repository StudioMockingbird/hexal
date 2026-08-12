# RFC 0029: Error Values, Try, and Errdefer

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Features: ordinary Error values, `T | Error` results, `try` propagation, and
  error-only deferred cleanup
- Created: 2026-08-11
- Depends on: RFC 0006 (objects), RFC 0008 (functions), RFC 0014 (unions), RFC
  0015 (structured control flow), RFC 0022 (match), RFC 0026 (defer), and RFC
  0036 (`Size`)
- Coordinates with: RFC 0018 (String and Strand), RFC 0027 (allocators), RFC
  0030 (`print`), and RFC 0031 (`Stream<T>`)

## Summary

An error is an ordinary value returned by a function:

```seawitch
fun read_count(): Int32 | Error
    return Error.new("Read Error", "could not read count")
end
```

Callers may inspect it with `match` or propagate it with `try`:

```seawitch
count: Int32 = try read_count()
```

`errdefer` registers cleanup that runs only when the current function exits by
returning an Error:

```seawitch
resource := try open_resource()
errdefer resource.close()
```

Together, these features provide the normal fallible-function pattern:

```seawitch
fun load_config(path: String): Config | Error
    file := try open_file(path)
    errdefer file.close()

    config := try parse_config(file)
    file.close()
    return config
end
```

`try` returns an encountered Error from `load_config` immediately. Before that
return completes, `errdefer file.close()` runs. On success, the errdefer is
discarded and the explicit `file.close()` performs the cleanup.

There are no exceptions, stack unwinding objects, hidden result channels, or
automatic allocation by Error itself. Error remains a value in an ordinary
structural union.

## Error type

V1 defines one built-in nominal object with ordinary value layout:

```seawitch
type Error = {
    file: String,
    line: Size,
    column: Size,
    header: Strand,
    message: String,
}
```

This declaration is specification notation; user source does not redeclare the
built-in. `Error` is a reserved type name. It cannot be declared, shadowed, or
used as the name of a generic type parameter. An arbitrary user object with
members of the same names is not Error and receives no special behavior.

Error is a value struct. Copying or returning Error copies its fields using the
ordinary value and shallow-handle rules of their field types. Error introduces
no exception object, reference counting, automatic destruction, or ownership
rule of its own.

### Fields

- `file` is the source-unit name supplied to the compiler. Its bytes reside in
  compiler-generated static read-only storage.
- `line` is the one-based source line containing the start of the `Error.new`
  call.
- `column` is the one-based UTF-8 byte column at which the `Error` token starts.
  It equals one plus the number of source bytes before that token on its line.
  An ASCII character or tab therefore advances the column by one; a non-ASCII
  source character advances it by its encoded UTF-8 byte count. This matches
  the compiler's lexer and keeps source tracking as simple as C-style byte
  offsets. `column` is preferred over the ambiguous name `location`.
- `header` is a short, stable error category supplied by the program. Its
  `Strand` type limits it to a literal-derived 31-byte UTF-8 payload.
- `message` is the detailed text supplied by the program. It is a `String`, so
  it is not restricted to 31 bytes.

All fields are readable and fixed after construction. `file`, `line`, and
`column` identify the construction site. Propagating the Error through `try`,
return, a union, or another variable never rewrites them.

The source-unit name is the same logical filename used by diagnostics and C
`#line` output. An in-memory source with no caller-provided name uses the
canonical synthetic name `main.seawitch` consistently in all three places.

### Built-in construction

Error is somewhat magical only at its construction boundary. It can be created
only by this compiler-provided associated function:

```seawitch
Error.new(header: Strand, message: String): Error
```

For example:

```seawitch
err: Error = Error.new("File Error", "file not found")
```

When checking the call, the compiler supplies `file`, `line`, and `column` from
the `Error` token that begins `Error.new`. The generated program does not
discover them through runtime stack inspection. A helper function records the
helper's construction site, not the helper's call site:

```seawitch
fun missing_file(): Error
    return Error.new("File Error", "file not found")
end
```

Only `header` and `message` are source arguments. Supplying source-location
arguments, using a raw object initializer such as `Error { ... }`, or calling
`Error.new` with the wrong argument types is rejected. `Error.new` is built in;
programs cannot replace, overload, or call it indirectly as a first-class
function value.

A literal message uses String's static literal storage and therefore requires
no allocation. A runtime String message is copied as an ordinary String handle;
its backing-storage lifetime remains the programmer's responsibility under the
language's C-style manual lifetime rules. `Error.new` does not copy, allocate,
or free message storage.

Every Error value represents failure. There is no numeric success/error code
inside Error. Aliases preserve Error's canonical identity.

## Fallible results

A fallible value result uses an ordinary structural union containing exactly
one Error member:

```seawitch
fun parse_count(text: String): Int32 | Error
    ...
end
```

A no-value success uses `Error | Nil`, retaining the language's conventional
final `Nil` spelling:

```seawitch
fun flush(): Error | Nil
    ...
end
```

The function may return either member with the existing return statement. The
caller may use ordinary `match ... is` to inspect both paths. This RFC adds no
`Result<T,E>` wrapper and no second runtime representation beyond RFC 0014's
existing union layout.

## Try expression

```ebnf
try-expression = "try", unary-expression ;
```

`try` becomes a reserved word and binds like the existing prefix unary
operators. Its operand must produce a union containing Error and at least one
non-Error member.

```seawitch
value: Int32 = try parse_count(text)
```

The operand evaluates exactly once:

1. if its active member is Error, `try` returns that Error from the current
   function immediately;
2. otherwise, `try` yields the active non-Error value to its surrounding
   expression.

The enclosing function's declared result type must accept Error. Propagation
injects the exact Error value into that declared result without allocation,
conversion, or message rewriting.

If more than one non-Error member remains, the `try` expression's result is the
normalized union of those members:

```seawitch
value: Int32 | Float32 = try read_number() // source is Int32 | Float32 | Error
```

`try` is rejected at script/module scope because there is no enclosing function
result to receive Error. It is also rejected on exact Error, non-union values,
and unions without Error.

`try` does not catch traps. Allocation traps, bounds traps, division traps, and
cleanup failures remain unrecoverable until their owning specifications adopt
Error-returning APIs.

RFC 0026 allocation continues to trap in v1. A future allocator may separately
offer an operation returning `T | Error`; such a result uses ordinary `try`
without changing this RFC or the existing trapping operation.

`try` is valid only inside a function. It is an expression rather than a
statement, so its successful value may be assigned, passed, returned, or used
inside another expression:

```seawitch
count: Int32 = try read_count()
total: Int32 = base + try read_count()
consume(try read_count())
```

`try` is rejected anywhere inside a `defer` or `errdefer` action expression.
Cleanup is already running on an exit path, so starting another early return
from inside cleanup would complicate C lowering and error ownership for little
benefit:

```seawitch
defer try flush()       // Error
errdefer try rollback() // Error
```

A cleanup call may itself return Error, but the cleanup action discards that
result under the ordinary `defer` action-context rule.

## Conceptual propagation

`try` performs the following compiler operation:

```text
temporary = evaluate operand once
if temporary's active member is Error:
    evaluate and preserve the function return value
    run eligible defer and errdefer actions from inner scope to outer scope
    return the unchanged Error payload through the declared result union
otherwise:
    yield the active non-Error value or remaining success union
```

This is semantic pseudocode, not valid Seawitch source. In particular, match
arms are expressions and cannot contain a `return` statement.

`try` is the one concise spelling for "if Error, return it; otherwise yield the
success value." It does not use truthiness and does not inspect Error fields.

## Errdefer

```ebnf
errdefer-statement = "errdefer", user-expression ;
```

`errdefer` becomes a reserved word. Registration and capture follow the same
rules as `defer`:

- a direct call captures its callee, receiver, and arguments when reached;
- another expression evaluates when its cleanup scope exits; and
- unvisited branches register nothing.

`errdefer` is valid only inside a function whose declared return type accepts
Error. It does not create or catch an Error. It only registers an action to run
if an Error return exits through the action's still-active lexical scope.

An errdefer action runs only when its lexical scope is exited as part of the
current function returning Error, whether through `try` or an explicit return.
It is discarded when the scope exits normally, through `break` or `continue`,
or as part of a successful return.

A nested-scope errdefer does not remain registered after that scope completes
normally:

```seawitch
if condition then
    errdefer cleanup_inner()
end
-- cleanup_inner is now discarded; a later Error does not run it.
```

This is the same lexical lifetime as ordinary `defer`. The only difference is
that `errdefer` runs solely while unwinding an Error return.

```seawitch
handle := try open_handle()
errdefer handle.close()

configure := try configure_handle(handle)
return configure
```

When ordinary `defer` and `errdefer` actions are registered in one scope, they
share registration order. On an Error exit, all eligible actions execute in
reverse registration order. On a non-Error exit, only ordinary defer actions
execute.

An errdefer action that itself returns Error has its result discarded like an
ordinary deferred expression. A trap during errdefer terminates cleanup under
RFC 0026; this draft defines no recovery from cleanup failure.

## Return classification

An explicit return of exact Error is an Error exit. Returning a union that may
contain Error requires a runtime tag test at the function exit: errdefer runs
only when Error is active. Returning a non-Error member is a successful exit.

Branch and loop scopes propagate the same exit classification outward. Inner
eligible cleanup runs before outer cleanup.

## C23 lowering direction

- Error lowers as an ordinary generated C struct containing a String handle,
  two `size_t` values, the inline Strand representation, and another String
  handle, in declared field order.
- Each `Error.new` call lowers to direct Error value construction. The compiler
  emits its source-unit String handle and constant line and column values beside
  the two evaluated source arguments; there is no runtime location lookup.
- `header` and `message` are each evaluated exactly once, in source order,
  before the Error value is assembled.
- `T | Error` reuses RFC 0014's deterministic tag-and-payload representation.
- `try` evaluates its operand into one temporary, checks its tag, performs the
  existing deferred cleanup path on Error, and returns the payload unchanged.
- Successful extraction reads only the active non-Error payload.
- Errdefer uses the existing cleanup-edge machinery with an additional
  success/error exit condition; it is not a C exception mechanism.

## Required diagnostics

The checker must reject invalid uses at the earliest phase that can prove them.
Diagnostics must identify at least these cases:

```text
[Name Error] Error is a reserved built-in type
[Type Error] Error must be created with Error.new(header, message)
[Type Error] Error.new expects header: Strand and message: String
[Type Error] try requires a union containing Error and a success member
[Type Error] try requires an enclosing function whose result accepts Error
[Type Error] try is not permitted inside defer or errdefer
[Type Error] errdefer requires an enclosing function whose result accepts Error
```

Normal fixed-member diagnostics handle attempts to assign `file`, `line`,
`column`, `header`, or `message` after construction. Normal arity diagnostics
handle missing or extra `Error.new` arguments. Unsupported Error, `try`, or
`errdefer` states must fail closed; they must never silently omit cleanup,
return an inactive union payload, or manufacture a source location.

## Required conformance coverage

Implementation is complete only when focused tests establish all of the
following:

1. `Error` has canonical nominal identity and the specified five-field layout.
2. `Error.new` injects the source-unit name and exact one-based line and byte
   column while evaluating `header` and `message` once in source order.
3. Raw Error object initialization, reserved-name declarations, wrong
   constructor arguments, and field mutation are rejected.
4. Literal messages require no allocation; runtime String messages are passed
   as ordinary shallow handles without an implicit copy or free.
5. Error works as an ordinary member of `T | Error` and `Error | Nil`.
6. `try` evaluates its operand exactly once, propagates the unchanged Error,
   and yields the active success member.
7. Removing Error from a union with multiple success members produces their
   normalized union.
8. Invalid `try` operands, top-level `try`, incompatible enclosing returns, and
   `try` inside either cleanup form are rejected.
9. `errdefer` uses the same registration, direct-call capture, lexical scopes,
   and nested-scope order as `defer`.
10. Error return through either `try` or explicit `return` runs eligible mixed
    `defer` and `errdefer` actions in reverse registration order.
11. Normal fallthrough, successful return, `break`, and `continue` discard
    eligible errdefers while still running ordinary defers as RFC 0026 requires.
12. Returning a runtime union checks its active tag and runs errdefer only when
    Error is active, without reading an inactive payload.
13. An Error result from a cleanup call is discarded, while a cleanup trap
    retains RFC 0026's unrecoverable behavior.
14. Generated C uses plain structs, tagged unions, branches, and cleanup labels;
    Error creation performs no runtime stack inspection or hidden allocation.

## Deferred and open design work

- User-defined error types or generic `Result<T,E>`.
- Error chaining, causes, and stack traces.
- Automatic copying or ownership of runtime Error messages.
- Recoverable allocation, bounds, conversion, and arithmetic failures.
- Catch expressions and retry loops.
- Error sets, numeric namespaces, and OS error mapping.
- Top-level script Error results.

## Finalized decisions

- Compilation without a caller-provided source-unit name uses
  `main.seawitch`.
- `try` may yield a normalized union containing multiple success members.
- A runtime union return uses its active tag to classify an Error exit.
- `errdefer` has the same lexical scope lifetime as `defer`.
- RFC 0026 allocation continues to trap; future Error-returning allocation
  operations use ordinary union results and `try`.
