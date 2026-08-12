# RFC 0041: No Module Globals

- Kind: Architecture Decision Record (ADR)
- Status: Implemented; conformance verified 2026-08-12
- Decision: Seawitch modules contain declarations, never native runtime or
  compile-time global values
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0008 (closed function scopes) and RFC 0034 (modules)
- Coordinates with: RFC 0037 (concurrency), RFC 0039 (C interop), and the
  future program-entry contract

## Context

Global values create initialization order, hidden shared state, import side
effects, and synchronization questions. Seawitch does not need them. State can
remain local, be allocated explicitly, and be passed to functions as in C.

## Decision

Seawitch has no native module-global or process-global values.

An imported module may contain type, function, and method declarations. It may
not contain:

- mutable or immutable value bindings;
- named native compile-time constants;
- runtime initialization statements;
- hidden initialization functions;
- import-time side effects; or
- function-local static storage.

There is no `global`, `static`, or native module-constant syntax.

## Root module

RFC 0034 permits one build-selected root module to contain executable
top-level statements. Those statements become the generated entry body.
Bindings declared there are ordinary locals:

```seawitch
mut counter: Int32 = 0
run(ref counter)
```

They are not globals and declared functions cannot capture them:

```seawitch
mut counter: Int32 = 0

fun increment()
    counter = counter + 1 // Error: function cannot capture a root local.
end
```

The rule is unchanged if a future entry specification replaces the root script
body with an explicit `main`: entry bindings remain function locals.

## Explicit state passing

Functions receive state through parameters:

```seawitch
type App = {
    count: Int32,
}

fun increment(app: MutPtr<App>)
    app.count = app.count + 1
end

mut app: App = App { count = 0 }
increment(ref app)
```

Heap-backed state may instead be passed through its ordinary pointer or handle.
This RFC adds no ownership or lifetime mechanism.

## Constants

V1 adds no native named module constants. Programs use literals, enum or ADT
variants, or a zero-argument function when a named computed value is needed:

```seawitch
fun max_retries(): Int32
    return 3
end
```

This avoids introducing a constant evaluator merely to preserve global names.
A future constant-expression RFC may add declaration-only constants without
adding runtime storage or initialization, but that is outside this decision.

## C boundary

Imported C external variables may exist because they are foreign C state. They
remain visibly qualified through RFC 0039:

```seawitch
value: Int32 = libc.foreign_counter
```

Their declaration, initialization, lifetime, thread safety, and cleanup belong
to C. Accessing one does not permit declaring a native Seawitch global.

Seawitch cannot export a native variable as a C global. Exported functions
receive state through parameters or foreign-owned pointers.

Imported C enumeration values and object-like constants are foreign
declarations, not native module bindings. RFC 0039 defines accepted forms.

## Concurrency

Shared state is explicit through passed pointers or handles. Synchronization
uses RFC 0037's Atomic, Mutex, Channel, or other explicit runtime objects.

No implicit global scheduler, allocator, or user-visible runtime state is
introduced by this RFC. Compiler-private C data is permitted only when needed
to implement another specified runtime feature and is not addressable as a
Seawitch declaration.

## C23 lowering

- Root executable statements lower inside the generated entry function.
- Root bindings lower to automatic C locals unless another owning RFC requires
  explicit heap storage.
- Imported modules emit declarations and definitions for types and functions,
  but no native user value at C file scope.
- Function bodies receive every accessed runtime value through a parameter or
  local expression.
- Imported C globals remain direct qualified foreign accesses.

## Diagnostics

The parser accepts ordinary bindings in the root executable body. Module
classification and name resolution reject bindings or statements in an
imported module and reject function capture of root locals.

Representative diagnostics are:

```text
[Module Error] imported module app.config cannot contain a value binding
[Module Error] imported module app.config cannot contain executable statements
[Name Error] function increment cannot capture root local counter; pass it as a parameter
[Syntax Error] Seawitch has no global or static declaration
```

The compiler must not silently lower a rejected binding to C file-scope
storage.

## Consequences

- imports have no runtime initialization order;
- module loading has no side effects;
- shared mutable state is visible in function signatures;
- generated native code has no user-declared C globals; and
- named native compile-time constants are deferred.

## Acceptance criteria

This decision is implemented when:

1. imported modules reject every value binding and executable statement;
2. root top-level bindings lower as entry-body locals;
3. functions cannot capture root locals;
4. native module constants, `global`, and `static` remain unavailable;
5. no accepted native declaration emits user value storage at C file scope;
6. imported qualified C globals remain possible only through RFC 0039; and
7. all failures are structured and source-located.

## Final decision

Native Seawitch state is local or explicitly passed. Program-entry syntax may
change later without changing this rule.

