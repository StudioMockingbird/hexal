# RFC 0220: Entry-Script Top-Level Capture

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; blocked on RFCs 0218 and 0219, not scheduled
- Created: 2026-09-18
- Updated: 2026-09-19
- Scope: allow named functions and methods in the selected entry module to
  access earlier top-level entry bindings through a stack-owned environment
- Depends on: RFC 0219 top-level `let` classification and removal of `static`
- Coordinates with: entry source-order execution, direct call graphs, generated
  function signatures, Tasks, C lowering, and concurrency
- Does not add: imported-module mutable state, general closures, escaping
  environments, implicit cleanup, or a source-level `main`

## Summary

The entry source remains an implicit script whose named helper functions may use
earlier top-level bindings:

```hexal
let heap = Heap()
defer heap.free()

let mut count: Int32 = 0

fun process() do
    work(heap)
    count = count + 1
end

process()
print(count)
```

`process` captures `heap` and `count` by reference. Its source parameter list
does not expose an environment parameter. The compiler groups physically
captured bindings in one stack-owned entry-environment record and passes its
address through every direct call that needs it.

This is deliberately not general closure support. Anonymous functions and local
function-literal bindings remain unable to capture function locals:

```hexal
fun outer() do
    let value: Int32 = 1
    let callback = fun (): Int32 do
        return value -- rejected
    end
end
```

Imported modules remain free of runtime top-level state. They receive mutable
state explicitly through parameters and pointers.

## Rationale and ROI

Without capture, splitting a script into a helper requires threading every root
binding through source parameters. With C file-scope mutable storage, the same
convenience becomes hidden program-lifetime global state. A stack-owned entry
environment supplies the script ergonomics without either cost.

The environment has one obvious owner: generated `main`. Its lifetime is the
entry execution, and existing root `defer` statements remain the complete
cleanup story. No heap allocation, reference counting, destructor, module
constructor, or runtime closure representation is introduced.

The restriction to compiler-controlled direct calls keeps the initial feature
small. It avoids closure-pair function values, C trampolines, callback lifetime
contracts, and escaping environments.

## Semantics

### Eligible declarations

Only a named module-level function or method in the selected entry module may
capture an entry binding. A binding is visible for capture only when its
declaration appears textually before the function or method declaration.

Entry bindings retain source-order visibility. Function and method names retain
their existing order-independent visibility.

Capture is implicit and by reference:

- reads observe the binding's current value;
- assignment requires `let mut`;
- parameters, `self`, and locals resolve before entry captures under existing
  shadowing rules; and
- capture neither copies the value nor changes ownership or cleanup.

A direct fixed inferred function-literal declaration classified as named-function
sugar by RFC 0219 follows the same capture rule as the equivalent written named
function. Local function literals remain non-capturing.

A generic named function or method may capture. The checker computes its direct
capture set and statically resolved call edges once on the open template. Lexical
name resolution does not depend on type substitution, so every specialization
inherits the same capture summary and environment dependence; capture is not
rediscovered or varied per specialization. Every generated environment-dependent
specialization receives the same hidden environment-pointer form as a
non-generic declaration.

Anonymous functions, local function literals, imported-module functions, and
nested lexical scopes otherwise remain non-capturing. A method is eligible only
when its declaration belongs to the selected entry module; its receiver and local
bindings resolve before entry bindings, and only statically resolved method calls
can propagate the environment.

### Environment-dependent call graph

A function or method is environment-dependent when it directly captures an
entry binding or directly calls another environment-dependent entry declaration.
The checker computes the least fixed point across recursion and mutual recursion.

Every direct call between entry declarations passes the environment when the
callee is environment-dependent. This propagation is generated C plumbing, not
a source parameter and not part of the Hexal function signature.

### Initialization safety

A binding becomes initialized only after its source initializer completes. A
direct root operation that could enter an environment-dependent call graph is
valid only after every captured binding in that graph is initialized. This rule
applies to direct calls in every root expression, including declaration
initializers, conditions, arguments, and final-result expressions; it is not
limited to standalone call statements.

```hexal
tick() -- rejected: count is not initialized

let mut count: Int32 = 0

fun tick() do
    count = count + 1
end
```

Required diagnostic:

```text
function tick may access entry binding count before count is initialized
```

Capture summaries contain direct captures plus the transitive union of captures
from statically resolved entry callees. Recursive groups use their least fixed
point. No runtime initialized flag, default read, temporal-dead-zone trap, or
lazy initialization is generated.

A direct call wrapped in `try` is an ordinary immediate direct call. A `defer`,
or a function-body `errdefer`, of an environment-dependent declaration is also a
direct call, not a function-value exposure. At root, every binding in a `defer`
summary must be initialized when the deferred action is registered, because
cleanup may begin on any later exit; `errdefer` remains invalid at root under its
existing rule. The deferred action retains the environment pointer and supplies it
when the call executes. Inside a function or method, the ordinary transitive-
summary rule makes the containing declaration environment-dependent. Existing
reverse cleanup order and freed-state rules remain unchanged; capture does not
reorder cleanup or make an unsafe defer order valid.

### Non-escaping restriction

An environment-dependent function or method is valid only as the statically
resolved target of an ordinary direct call or method call inside the selected
entry module.

It cannot be:

- named in an export block;
- converted, assigned, stored, returned, or passed as a `Fun` value;
- passed to foreign code as a callback;
- used as an indirect-call target; or
- used as a Task/spawn entry.

These restrictions also apply to declarations that capture only transitively.
“Function-value exposure” covers every conversion or placement that requires a
`Fun` value, including bindings, arguments, results, aggregate or collection
members, conditional selection, method values, and foreign callbacks. Direct
callee position is the only exception.
A named entry function with no direct or transitive environment dependency
retains every existing function-value, export, callback, and Task capability.

Required diagnostic:

```text
function <name> uses the entry environment and is valid only as a direct entry-module call
```

The restriction can be relaxed only by a later RFC that defines an explicit
environment-bearing function representation and lifetime contract.

### Ownership and concurrency

Capture does not extend an allocation's lifetime and does not schedule cleanup.
A captured `Heap`, pointer, collection, or other resource remains governed by
its existing owner and the root program's written cleanup. Moving the binding
into an environment field preserves its binding identity, provenance, mutability,
and entry lifetime; it must not be reclassified as global or program-lifetime
storage by escape, view-return, freed-state, or alias checks.

Taking the address of a captured binding or member follows the same rules as
taking the address of the original entry binding. A synchronous imported or
foreign call may receive it under the existing foreign boundary. A Task may
receive it only when the existing structured-join, lifetime, and shared-mutation
rules allow the equivalent pointer to the original root binding. No new pointer
escape rule is introduced merely because the storage is an environment field.

The initial non-escaping rule forbids spawning an environment-dependent function.
Concurrency therefore gains no implicit shared entry state. Tasks continue to
receive shared state explicitly and remain subject to the existing Atomic,
Mutex, pointer-lifetime, and structured-join rules.

## C23 lowering

The generator emits exactly one private, owner-qualified environment struct type
for the selected entry module when at least one binding is captured. Independent
capture groups and recursive groups share it. The type is emitted in the entry C
artifact before every prototype or definition that names it and never appears in
a generated public header. The environment instance is one automatic local in
generated `main`; no mutable C file-scope object is emitted.

Only captured bindings need environment fields. Uncaptured root bindings remain
ordinary automatic locals at their source positions. This storage distinction is
not observable in Hexal.

Each environment-dependent generated function or method remains private and
receives one mutable compiler-owned environment pointer as its first C parameter,
before every lowered source receiver or parameter. Every generated direct call,
including recursion and mutual recursion, supplies that pointer. Prototypes use
the identical parameter order. Existing source signatures and call syntax do not
change, no C consumer can name the changed ABI, and existing `#line` mapping of
source declarations and bodies is preserved.

Initializers still execute at their original source positions and assign the
corresponding environment fields. Fixed bindings need writable C fields for that
single initialization; the Hexal checker continues to reject later assignment.
Root `defer` cleanup remains emitted at the same logical scope and in the same
order.

Conceptually:

```c
typedef struct {
    hex_heap *heap;
    int32_t count;
} hex_entry_env;

static void hex_process(hex_entry_env *env) {
    hex_work(env->heap);
    env->count += 1;
}

int main(void) {
    hex_entry_env env;
    env.heap = hex_heap_new();
    env.count = 0;
    hex_process(&env);
    hex_heap_free(env.heap);
    return 0;
}
```

The environment type and hidden parameter are compiler-owned C details. They do
not create a source type, value, module instance, allocator requirement, or ABI
available to Hexal or foreign code.

## Imported modules

RFC 0219 remains unchanged: imported modules admit fixed statically initialized
top-level constants but no mutable or runtime-initialized top-level state.
Imported functions never receive the entry environment implicitly.

```hexal
fun render(window: Ptr<mut Window>) do
    -- explicit library state
end
```

Entry functions may call imported functions with captured values as ordinary
arguments. This feature creates entry-script convenience, not hidden library
singletons or import-time execution.

## Implementation plan

### Phase 1: checker visibility and captures

1. Make earlier entry-root bindings visible while checking named entry
   functions and methods, after parameters, `self`, and locals.
2. Record direct captures by binding identity and required mutability. For an
   open generic declaration, record them once on the template and make every
   specialization inherit that summary.
3. Compute deterministic transitive summaries across the statically resolved
   entry call graph, including ordinary and generic-template call edges and
   recursive strongly connected components.
4. Classify every declaration with a non-empty transitive summary as
   environment-dependent.
5. Preserve the original binding identity, provenance, mutability, and lifetime
   on every captured use.

### Phase 2: initialization and exposure

1. Track root initialization in source order.
2. At each direct root call in any expression, and at root `defer` registration,
   require the callee summary to be initialized. Root `errdefer` remains invalid.
3. Run exposure validation only after the transitive fixed point is complete.
   Reject export, function-value conversion/exposure, callback use, indirect
   calls, and Task/spawn use of environment-dependent declarations with the
   exact diagnostic above.
4. Preserve all existing behavior for declarations with empty summaries.
5. Lower direct `defer`, `errdefer`, and `try` calls with the same hidden
   environment propagation as immediate direct calls.
6. Apply exposure rejection to an environment-dependent open generic declaration
   and every specialization that inherits its summary.

### Phase 3: generated C

1. Emit the single private entry-environment struct only when captures exist,
   before every prototype or definition that names it.
2. Emit one field per captured binding in deterministic binding-source order.
3. Declare one automatic environment instance in generated `main`.
4. Lower each captured initializer and use to its field.
5. Add the hidden pointer to every environment-dependent definition,
   declaration, specialization, and direct call.
6. Preserve uncaptured binding output and root cleanup ordering.
7. Keep the environment type and every environment-dependent symbol out of
   generated public headers.

### Phase 4: documentation and regression artifacts

1. Update entry binding, function visibility, non-capture, function-value,
   Tasks, generated-artifact, and lifetime rules in `docs/reference.md`.
2. Add focused integration tests and generated-C text assertions.
3. Add or revise one workbench entry-script example and rebuild only its
   reviewed snippet-manifest artifacts.
4. Update `docs/status.md`; archive the RFC only after every validation case
   passes.

## Validation (exhaustive)

1. A named entry function, method, and non-generic direct function-literal
   declaration sugar may read a fixed earlier entry binding and read/write a
   mutable earlier entry binding.
2. Parameters, `self`, and locals shadow same-named entry bindings without
   capture.
3. A later entry binding is unavailable to an earlier function or method;
   anonymous/local function literals remain unable to capture any enclosing
   entry or lexical binding.
4. Direct and transitive capture summaries are deterministic through forward
   calls, recursion, and mutual recursion.
5. A root direct call in any expression, or root `defer` registration, before
   any binding in its transitive summary is initialized reports `function <name> may access entry binding <binding> before <binding>
   is initialized`; the same call after initialization succeeds.
6. An environment-dependent declaration used in an export, binding, argument,
   result, aggregate or collection member, conditional function selection,
   method value, foreign callback, indirect call, or Task/spawn position reports
   `function <name> uses the entry environment and is valid only as a direct
   entry-module call`.
7. A declaration with an empty capture summary retains existing export,
   function-value, callback, indirect-call, and Task/spawn behavior.
8. The generated environment contains exactly the captured bindings, in source
   order, and is an automatic local in `main`; no mutable C file-scope storage
   is emitted.
9. The private owner-qualified environment type precedes every prototype and
   definition that names it and appears in no public header. Every environment-
   dependent declaration and direct call has exactly one matching mutable first
   environment pointer; imported and independent declarations do not.
10. Captured initialization occurs at the original script position. Fixed
    captured bindings reject later assignment, and mutable ones accept it.
11. Uncaptured roots retain byte-identical automatic-local lowering. Direct
    `defer`, function-body `errdefer`, and `try` calls propagate the environment;
    root `errdefer` remains invalid, and root error, return/final-result, reverse
    cleanup order, and freed-state behavior remain unchanged.
12. Imported module constants remain directly readable, while imported mutable
    or runtime top-level state remains rejected under RFC 0219.
13. Ordinary tests remain pure Go; focused generated-C assertions verify struct
    placement, field order, signatures, calls, initialization, and absence of
    mutable file-scope storage.
14. Captured uses retain the original entry binding's identity, provenance,
    mutability, and lifetime. Address-taking, synchronous foreign calls, views,
    and structured Tasks receive the same acceptance or rejection as the
    equivalent uncaptured root binding.
15. A generic entry function or method may capture directly or transitively. Its
    open template owns one deterministic capture summary; every specialization
    inherits it, receives the hidden environment pointer when dependent, and is
    subject to the same direct-call-only exposure rule. Different substitutions
    cannot produce different capture sets.
16. The compiler remains string-in/string-out; `go test ./...`, `go vet ./...`,
    `go vet -tags c23 ./...`, the complete tagged C23 suite under Clang, and
    `gofmt -l` all pass.

## Decisions

1. Capture is limited to named functions and methods in the selected entry
   module.
2. Only earlier top-level entry bindings are visible for capture.
3. Capture is implicit, by reference, and does not change source call syntax.
4. Captured storage lives in one stack-owned entry environment, never mutable C
   file-scope storage.
5. Only proven-captured bindings become fields; uncaptured bindings remain
   ordinary locals.
6. Environment-dependent declarations are direct-call-only in the initial
   feature and cannot escape, export, become callbacks, or spawn.
7. Imported modules gain no runtime state or implicit environment.
8. Capture adds no ownership, cleanup, synchronization, or lifetime extension.
9. Generic declarations compute capture once on the open template; every
   specialization inherits the same summary and hidden-parameter requirement.

## Non-goals

- Capturing parameters or locals from another function.
- Anonymous or heap-allocated closures.
- Environment-bearing function values or foreign callback trampolines.
- Spawning an environment-dependent function.
- Imported-module runtime state, initialization, or implicit instances.
- Implicit cleanup, synchronization, ownership, or lifetime extension.
- A source-level `main`, `run`, or entry block.
