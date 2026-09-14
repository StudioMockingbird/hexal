# RFC 0177: libuv Dynamic Libraries

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; blocked on the foreign ABI model and a concrete plugin use case
- Created: 2026-09-13
- Updated: 2026-09-14
- Scope: integrate dynamic-library loading, symbol lookup, and unloading into Hexal C interoperability through libuv
- Depends on: RFC 0168, deferred RFC 0039, RFC 0155, and the implemented RFCs 0180 and 0181
- Does not replace: normal static C imports and link-time object/library input

## Summary

Use `uv_dlopen`, `uv_dlsym`, `uv_dlclose`, and `uv_dlerror` as the sole portable runtime dynamic-library backend if Hexal later adopts runtime plugin loading.

Dynamic loading is not required for ordinary C interoperability or the single-binary goal. Static headers, objects, and libraries come first. The proposal cannot settle its source API until RFC 0039 defines C function pointers, data pointers, calling conventions, and ABI-compatible declarations.

## Constraints preserved for reconsideration

- A Hexal `Fun<...>` type is not by itself a foreign C ABI declaration.
- `uv_dlsym` returns a pointer and cannot prove whether a symbol is code or data, or whether the declared signature is correct.
- Lookup, conversion, invocation, and close ordering therefore require an implemented `unsafe do ... end` boundary.
- The v1 manual-memory direction should make the programmer responsible for ensuring that no symbol is used after library close rather than introducing a lifetime system solely for dynamic loading.
- A path is passed verbatim as UTF-8. Relative and bare names use platform loader search rules and their security consequences; Hexal adds no suffix or normalization.
- Errors use portable ErrorKind plus fixed operation messages. `uv_dlerror` text is transient backend evidence and is not retained in public Error.
- Foreign function thread safety belongs to the imported API's contract, not libuv or Hexal.
- Dynamic loading selects libuv and its native bootstrap but no event loop.
- No direct `LoadLibrary` or `dlopen` backend currently exists to remove.

## Re-entry condition

Promote this RFC from `deferred/` only after RFC 0039 settles the foreign ABI model and a concrete runtime-plugin use case exists. The promoted RFC must define the typed function/data symbol representation, unsafe obligations, copied-library handle lifecycle, exact errors, exhaustive Validation section, and detailed implementation plan.
