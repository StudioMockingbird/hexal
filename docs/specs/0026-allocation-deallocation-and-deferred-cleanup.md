# RFC 0026: Allocation, Deallocation, and Deferred Cleanup

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-10
- Features: compile-time heap handles, explicitly initialized allocation,
  explicit deallocation, lexical deferred cleanup, and the ownership boundary
  for allocator-backed storage
- Created: 2026-08-10
- Revised: 2026-08-10
- Depends on: RFC 0001 (raw pointers), RFC 0007 (mutability redesign), RFC
  0008 (functions and methods), RFC 0010 (nil and explicit nullability), RFC
  0015 (structured control flow), and RFC 0019 (generic types and functions)
- Coordinates with: RFC 0018 (String and Rune values), RFC 0020 (collections),
  RFC 0022 (algebraic data types), and the future concurrency and FFI
  specifications

## Summary

Seawitch currently has pointer types but no language-level allocation,
deallocation, or deferred-cleanup operation. This RFC proposes an explicit
allocator handle, paired allocation and deallocation, lexical `defer`, and the
ownership rules needed by allocator-backed values.

The intended manual pattern is:

```seawitch
h: Heap = Heap.new()
p: MutPtr<Int32> = h.allocate<Int32>(0)
p.value = 42
defer h.free(p)
```

Read-only allocated storage may use `Ptr<T>` and still be explicitly freed:

```seawitch
p: Ptr<Int32> = h.allocate<Int32>(0)
defer h.free(p)
```

`Heap.new()` creates a zero-cost allocation handle selected at compile time; it
does not allocate runtime storage. `h.allocate<T>(initial)` allocates storage
for one `T` and initializes it from the explicit `initial` value.
`h.free(value)` releases the corresponding allocation.
`defer expression` registers an expression for later evaluation or call
invocation when its cleanup scope exits.

The current language has no owning `Ref<T>` type. This RFC therefore does not
introduce one. Automatic cleanup and ownership-bearing allocation remain open
design work; the concrete manual form is explicit `free`, normally registered
with `defer`. `defer` remains useful for external resources, streams, temporary
collection cleanup during the transition, and custom cleanup calls.

The user-facing sketch used `Ptr<T>`. RFC 0007 defines `Ptr<T>` as read-only
and non-owning, so allocation returns `MutPtr<T>` for writable storage. This
RFC fixes the v1 result as a non-owning `MutPtr<T>`; read-only use may weaken it
to `Ptr<T>`. No ownership token or owning allocation value is introduced.

## Goals

1. Make allocation provenance explicit in source.
2. Provide deterministic, paired deallocation.
3. Run deferred cleanup on every language-level scope exit.
4. Preserve the existing read-only `Ptr<T>` and writable `MutPtr<T>`
   distinction.
5. Define an ownership boundary without giving arbitrary raw pointers implicit
   ownership.
6. Give strings and dynamic collections a common allocator boundary.
7. Lower to readable C23 without unchecked omission of cleanup or undefined
   pointer behavior.

## Non-goals

This RFC does not yet define:

- garbage collection or tracing;
- thread-safe allocation or cross-thread ownership transfer;
- the concrete implementations of arenas, pools, sacks, or regions;
- foreign-library allocation and deallocation contracts;
- exception recovery or resumable cleanup failures;
- automatic ownership inference for arbitrary `Ptr<T>` values;
- automatic owning allocation values, cloning, and move-based ownership
  transfer;
- a general iterator or `for` statement.

## 1. Core allocation model

### 1.1 `Heap`

`Heap` is a built-in allocation handle. `Heap.new()` is a compile-time-known
construction that creates the handle and does not request runtime memory:

```seawitch
h: Heap = Heap.new()
```

The handle identifies the allocator used by subsequent operations. A program
must not silently fall back to a process-global heap when an explicit handle is
required. In v1, `Heap.new()` selects the default heap allocator; repeated
handles selecting that allocator have the same allocator identity. Distinct
allocator identities belong to future arena, pool, and sack specifications.

### 1.2 `allocate`

The proposed operation is:

```seawitch
h.allocate<T>(initial) -> MutPtr<T>
```

It reserves one contiguous region with the size and alignment required by `T`,
initializes it from `initial`, and returns a writable pointer associated with
`h`. No uninitialized `T` value is observable by the program.

Allocation requires all of the following:

- `T` is complete and has a finite size;
- the requested size and alignment are representable for the target;
- `initial` has type `T` and is accepted by the language's existing ordinary
  value-initialization and copying rules;
- `h` refers to a valid allocator handle.

Allocation failure is a defined runtime allocation error. It must not emit a
null pointer that the program is expected to check unless a later revision
explicitly introduces a result-returning allocation API.

The compiler must reject an initializer that omits members, has the wrong type,
or requires value semantics not already defined for `T`. Generic allocation
checks run after `T` is specialized. V1 allocation does not introduce new
move, copy, or destructor semantics; types requiring those future features are
not valid allocation targets yet.

The no-argument `h.allocate<T>()` form is not defined in this first design. A
future `allocate_zeroed<T>()` operation may be added after Seawitch defines a
formal zeroable-type rule.

The result must not implicitly convert to an integer or an unrelated pointer
type. The existing RFC 0007 outermost weakening from `MutPtr<T>` to `Ptr<T>`
remains valid.

Every allocation carries compiler-invisible metadata containing a validation
marker, allocator identity, size, alignment, and live/freed state. The metadata
is not part of `T`'s source-visible layout and is used by `free` validation.

### 1.3 `free`

The initial proposed operation is:

```seawitch
h.free(value)
```

`free` accepts either `Ptr<T>` or `MutPtr<T>` when the value identifies an
allocation created by the same `Heap`. It does not require a writable pointer
or a `mut` binding: deallocation changes the allocation state, not the
pointee's mutability.

The pointer types remain non-owning. Passing one to `free` is an explicit,
privileged allocator operation based on the allocation's provenance; it does
not make the pointer an owning type or enable automatic cleanup.

A successful deallocation makes that allocation invalid. All aliases to the
allocation become invalid as well. A pointer registered with `defer` must not
also be freed manually before the deferred call runs; that is a double-free
condition.

The following are errors:

- freeing through an allocator with a different allocator identity;
- freeing stack, static, inline, or foreign storage;
- freeing an already-freed allocation;
- freeing a value that is not a pointer returned by `allocate`;
- accessing storage after it has been freed is a lifetime violation; this RFC
  rejects it when statically provable but does not promise runtime detection
  for arbitrary raw-pointer aliases.

The implementation must enforce allocator pairing at runtime where static
proof is not possible. At `free`, the allocator must preserve enough
provenance and live-state information to distinguish a valid `Ptr<T>` or
`MutPtr<T>` from a pointer to a different, freed, or non-heap allocation. The
exact representation and amount of static checking remain open because the
existing raw pointer types are non-owning and may be aliased.

## 2. Deferred cleanup

### 2.1 Syntax

The syntax accepts any user expression. The expression's result is discarded:

```ebnf
defer-statement = "defer" , user-expression ;
```

Examples include calls, values, and side-effecting expressions:

```seawitch
defer h.free(p)
defer cleanup()
defer value
```

`user-expression` means any expression form defined by the language, including
an expression with side effects. Declarations, standalone control-flow
statements, and nested `defer` statements are not expressions and remain
invalid after `defer`. The deferred expression is type-checked in action
context and its result is discarded. A no-return call is valid in this context
even though it is not valid where a value expression is required; this is what
allows `defer h.free(p)` and `defer cleanup()`.

### 2.2 Registration and capture

`defer` does not execute its expression immediately. When control reaches the
statement, it registers the expression with the current lexical scope. If the
statement is inside an unvisited branch, it is never registered.

When the top-level deferred expression is a direct call, the callee, receiver,
heap handle, and arguments are evaluated when the `defer` statement is reached;
the call itself runs when the scope exits. Mutation of a source variable after
registration does not change a captured call argument:

```seawitch
mut value: Int32 = 1
defer record(value)
value = 2
-- record receives 1
```

This also means that `defer h.free(p)` captures the current pointer value. If
`p` is later reassigned, the deferred call still targets the allocation that
was captured at registration.

Every other deferred expression uses exit-time evaluation. Its complete
expression tree, including nested calls, is registered but reads and side
effects occur when the scope exits:

```seawitch
mut value: Int32 = 1
defer value
value = 2
-- the deferred expression evaluates value as 2; its result is discarded
```

The distinction is based only on the top-level expression:

```seawitch
defer record(value)       -- captures value now
defer record(value) + 1   -- evaluates the complete expression at exit
```

An implementation must reject a deferred action that captures a value which
has already been moved, freed, or otherwise cannot remain valid until the
action runs. A call must not manually free an allocation and later free the
same allocation again through a deferred call.

### 2.3 Scope-exit order

Each lexical scope has a deferred-action stack. Actions execute in reverse order
of registration (last registered, first executed):

```seawitch
defer first()
defer second()
-- exits as: second(), then first()
```

Deferred actions run when control leaves the scope by:

- normal fallthrough;
- the end of the selected `if`, `elseif`, or `else` branch;
- the end of each completed loop-body iteration;
- `return`, after the return value has been evaluated;
- `break`;
- `continue`;
- exit from the script's outermost scope.

Every `if`, `elseif`, and `else` branch is a lexical cleanup scope. Only the
selected branch is entered, and its registered defers run at the branch's
`end` before control continues after the whole conditional. Every `while` body
is a fresh lexical cleanup scope for each iteration. A body defer runs after
that iteration, before the next condition test; it does not wait for function
exit.

For structured control flow, only scopes being exited are unwound. A `break`
or `continue` unwinds the current iteration's loop-body scopes, then transfers
to the loop target; it does not run defers belonging to the enclosing function
scope. A `return` unwinds all active nested scopes before leaving the function.
Nested scopes therefore clean up from inner to outer scope:

```seawitch
while condition
    open_inner()
    defer close_inner()
    break
end
-- close_inner() has run; the function scope remains active
```

The same rule applies to normal branch and iteration completion:

```seawitch
if failed
    defer rollback()
else
    defer commit()
end
-- exactly one of rollback() or commit() has run here
```

A `defer` that is never reached is never registered and never runs. If a
deferred action itself traps, execution terminates with a deferred-cleanup
failure. This draft does not define recovery or continuation after a cleanup
failure. Process termination or an unrecoverable runtime failure is not
promised to run deferred actions.

### 2.4 Control-flow lowering requirement

The compiler must route every supported exit edge through the appropriate
cleanup actions. It must not implement `defer` only on the normal fallthrough
path and must not silently omit cleanup on branch completion, loop iteration
completion, `return`, `break`, or `continue`.

## 3. Ownership boundary: no owner type is defined yet

The current language defines only two pointer types:

- `Ptr<T>` is read-only and non-owning;
- `MutPtr<T>` is writable and non-owning.

This RFC must not introduce `Ref<T>` as if it were already part of Seawitch.
If automatic ownership is added later, it needs an explicitly specified
representation and syntax.

`mut` is not the ownership bit under the existing language rules. It makes a
binding's storage slot replaceable. `MutPtr<T>` controls whether the pointee is
writable. Neither rule says who must eventually deallocate the allocation;
either pointer type may be passed to the matching `Heap.free` explicitly.

```seawitch
p: MutPtr<Int32> = h.allocate<Int32>(0)
p.value = 42                 // writable because the type is MutPtr<T>
defer h.free(p)               // valid without a `mut` binding

mut q: MutPtr<Int32> = h.allocate<Int32>(0)
q = h.allocate<Int32>(0)      // reassignable because the binding is `mut`
```

Making ownership depend on `mut` would create an unsafe ambiguity: assigning
to `q` could either leak the old allocation or implicitly free it while aliases
or a previously registered `defer` still refer to it. A fixed binding can own
storage, and a mutable binding can merely hold a non-owning pointer. Ownership
and binding mutability should therefore remain separate concepts.

V1 has no automatic owner type. The allocation remains live until an explicit
`free` call runs, normally through `defer`. Future ownership, move, borrow, and
automatic destruction features must define a separate representation rather
than changing the meaning of `Ptr<T>`, `MutPtr<T>`, or `mut`.

## 4. Interaction with pointers and mutability

Allocated storage is writable only through a `MutPtr<T>`. A `Ptr<T>` may read
the allocation and may be passed to the matching `Heap.free` for explicit
deallocation. Neither pointer type owns the allocation or keeps it alive.

Under the current pointer rules, the manual example is therefore:

```seawitch
h: Heap = Heap.new()
p: MutPtr<Int32> = h.allocate<Int32>(0)
p.value = 42
defer h.free(p)
```

The v1 allocation result is always `MutPtr<T>`; callers may weaken it to
`Ptr<T>` when they only need read access.

## 5. Diagnostics

The compiler should report invalid forms at the earliest phase that can prove
the error. Suggested diagnostics are:

```text
[Type Error] allocation requires a complete finite type
[Type Error] allocation requires an explicit initializer
[Type Error] deferred expression is not valid in this scope
[Type Error] deferred call captures a moved value
[Type Error] value is not an allocation produced by this Heap
```

Defined runtime diagnostics include:

```text
[Runtime Error] heap allocation failed
[Runtime Error] allocation size is not representable
[Runtime Error] deallocation used the wrong allocator
[Runtime Error] double deallocation
[Runtime Error] deferred cleanup failed
```

Unsupported allocation, ownership, or deferred-control-flow cases must produce
a structured diagnostic. The compiler must not emit plain C `free`, omit a
registered cleanup, or continue after an unknown ownership state.

## 6. C23 lowering

The generated C23 should remain readable and preserve source locations with
`#line` directives.

- `Heap.new()` lowers to a compile-time-selected allocator descriptor or
  context; it performs no allocation by itself.
- `h.allocate<T>(initial)` lowers to a checked helper that validates the size,
  obtains storage from `h`, and initializes it from `initial` before exposing
  the resulting pointer.
- `h.free(value)` lowers to the matching allocator helper after validating
  provenance and allocation state. The helper accepts the generated C pointer
  representation for either `Ptr<T>` or `MutPtr<T>`; source-level pointee
  writability does not affect deallocation.
- `defer` lowers to cleanup blocks or a compiler-managed cleanup stack, with
  all fallthrough, branch-completion, loop-iteration, return, break, and
  continue edges routed through the required actions.
- Future automatic ownership is outside this v1 lowering. The generated C
  must preserve allocator metadata for explicit `free` validation.

The generated code must not rely on unchecked C pointer validity or on a
platform-specific destructor extension.

## 7. Implementation phases

1. Define allocator provenance, explicit construction, allocation failure, and
   the existing value-copying rules; reject allocation targets that require
   unspecified move or destruction semantics.
2. Implement `Heap` and checked allocation for the finalized result type.
3. Implement matching `free`, invalid-state detection, and diagnostics.
4. Add `defer` parsing, scope registration, capture, and cleanup edges.
5. Add integration tests for allocation, deallocation, capture, and every
   cleanup exit edge.
6. Coordinate implementation of String `free` with RFC 0018's owning and
   borrowed-slice rules.
7. Coordinate implementation of collection `free` with RFC 0020's element
   storage and view rules.

## 8. Managed allocator-backed values

`Heap.free` in this RFC frees raw allocations identified by `Ptr<T>` or
`MutPtr<T>`. It does not free a `String`, `List<T>`, or `Dict<K,V>` header
directly. Those values use a type-owned `free(h)` operation because they own
backing storage and may contain nested values that require type-specific
destruction.

The v1 type-owned cleanup surface is:

```seawitch
text: String = String.from_bytes(h, bytes)
defer text.free(h)

values: List<Int32> = List<Int32>.new(h)
defer values.free(h)

scores: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)
defer scores.free(h)
```

Type-owned `free(h)` must validate allocator identity, destroy owned elements,
and free backing storage. A static literal or borrowed String slice must not
free storage it does not own. RFC 0018 and RFC 0020 define the element and
view rules for their respective values.

`defer h.free(value)` remains the raw-pointer form. Managed values use their
type-owned `value.free(h)` form.

## 9. Future extensions and implementation details

The v1 language decisions are fixed by this RFC:

- `h.allocate<T>(initial)` returns `MutPtr<T>`;
- `Ptr<T>` and `MutPtr<T>` remain non-owning;
- `mut` controls binding reassignment only;
- `free` validates allocator identity and live state;
- managed `String`, `List`, and `Dict` values use explicit type-owned
  `value.free(h)` cleanup;
- direct deferred calls capture arguments at registration;
- all other deferred expressions evaluate at scope exit;
- conditional expressions are not part of this RFC;
- `then` remains coordinated with RFC 0022's match-arm syntax.

The following are explicitly deferred and do not block v1 implementation:

- automatic owner types, cloning, move-based ownership, and borrow syntax;
- automatic destruction and owner types beyond explicit type-owned `free`;
- arenas, pools, sacks, and region-wide destruction;
- cross-thread ownership transfer and synchronization;
- imported C allocation contracts;
- cleanup recovery after a trap;
- exact C23 helper names and private allocation-header layout.

## 10. Acceptance criteria

An implementation conforming to this RFC must demonstrate that:

1. `Heap.new()` performs no runtime allocation.
2. Valid complete types with explicit initializers allocate correctly
   initialized storage.
3. Invalid, incomplete, or unsupported construction types fail before C
   generation.
4. Allocation failure and unrepresentable sizes have defined diagnostics.
5. Deallocation requires matching allocator provenance.
6. Both `Ptr<T>` and `MutPtr<T>` can be explicitly freed through their matching
   `Heap`; double free and invalid free are rejected or produce the specified
   runtime error.
7. Any valid expression can be deferred and its result is discarded.
8. Deferred call arguments are captured at registration time, while deferred
   non-call expressions evaluate at scope exit.
9. Deferred actions execute in reverse order on normal scope exit, branch
   completion, loop-iteration completion, `return`, `break`, and `continue`.
10. Nested scopes unwind from inner to outer.
11. Generic allocation validates the specialized `T`'s layout, initializer,
    and existing value-initialization rules.
12. V1 introduces no automatic owner type or implicit destruction; pointer and
    managed-value allocations are freed explicitly through `free`/`defer`.
13. No cleanup edge is omitted and no unchecked plain C deallocation is
    emitted.
14. Every parser, checker, lowering, and generator path is explicit and
    fail-closed.

## 11. Pending follow-ups

- Implement the default Heap allocator, allocation metadata, and C23 runtime
  helpers.
- Add static diagnostics for directly provable invalid pointer lifetimes.
- Keep the canonical grammar and language documentation synchronized with the
  finalized allocation and `defer` spellings.
- Add integration tests for allocation, deallocation, cleanup ordering, and
  control-flow exits.
