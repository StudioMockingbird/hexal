# RFC 0177: libuv Dynamic Libraries

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: integrate dynamic-library loading, symbol lookup, and unloading into
  Hexal C interoperability through libuv
- Depends on: ADR 0145, RFC 0168, deferred RFC 0039, and unsafe-operation rules
- Does not replace: normal static C imports and link-time object/library input

## Summary

Use `uv_dlopen`, `uv_dlsym`, `uv_dlclose`, and `uv_dlerror` as the sole
portable runtime dynamic-library backend.

Dynamic loading is an unsafe C-interoperability capability, not a general
reflection system. The compiler still needs a declared foreign signature
before a symbol can be called.

Conceptual source shape:

```hexal
unsafe do
    library := try DynamicLibrary.open("plugin")
    run := try library.function<Fun<(Int32): Int32>>("plugin_run")
    result := run(13)
    try library.close()
end
```

Exact syntax depends on the C-interoperability type model.

## Semantic direction

- Opening and lookup return owned Hexal `Error` values on failure.
- A symbol has no inferred ABI or signature.
- Calling a data symbol and calling a function symbol remain distinct checked
  operations.
- A library cannot close while any live symbol value may be used unless the
  unsafe contract explicitly transfers that responsibility to the programmer.
- Symbol values do not outlive their library by default.
- Loading does not execute callbacks through libuv or require the event loop.
- Static linking remains the preferred path for ordinary dependencies.

## Libuv ownership

Libuv exclusively owns portable open, lookup, error retrieval, and close.
Hexal owns ABI declarations, unsafe gating, typed symbol conversion, library
lifetime, and diagnostics.

No direct `LoadLibrary` or `dlopen` backend remains on libuv-qualified targets.

## Detailed implementation outline

1. Settle RFC 0039's raw C type and ABI model first.
2. Define DynamicLibrary ownership and typed function/data symbol values.
3. Require unsafe scope for operations the checker cannot prove.
4. Add the four libuv dynamic-library operations and Error translation.
5. Add library/symbol lifetime tracking within the chosen manual-memory model.
6. Delete direct platform dynamic-loader alternatives.
7. Validate a real test library, missing library, missing symbol, wrong kind,
   premature close, and unload behavior.

## Open design questions

1. What exact foreign function-pointer and external-data types cross lookup?
2. Can the checker prevent close with live symbols, or is that wholly unsafe?
3. Are platform filename conventions resolved by Hexal or passed verbatim?
4. Is symbol lookup always unsafe, or only conversion and invocation?
5. Can a loaded library be shared across parallel Tasks?

## Validation direction

The final exhaustive Validation section must cover ABI declaration, unsafe
gating, function and data symbols, library lifetime, all failure paths,
parallel access, static-link non-regression, no event-loop requirement, and no
direct platform loader backend.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation coordinates its syntax and semantics with the C-interoperability
reference update.
