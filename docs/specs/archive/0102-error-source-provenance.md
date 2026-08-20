# RFC 0102: Error Source Provenance

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented
- Created: 2026-08-20
- Scope: make every compiler-constructed Error record the current logical
  source-map key instead of the fixed string `main.hex`
- Coordinates with: checker module scopes, concurrency discovery, the
  program-wide literal registry, `docs/reference.md`, `docs/status.md`, the
  snippet manifest
- Does not change: Error syntax, layout, line/column rules, diagnostics,
  module identity, filesystem behavior, or user-provided Error fields

## Summary

`Error.new` and generated concurrency failures currently source their file
literal from package constants spelling `main.hex`. This is wrong for ordinary
single-module input such as entrypoint `app.hex`, not only for imports: no
source in that compilation is named `main.hex`. In a multi-module compilation,
an Error constructed in `lib.hex` records the same unrelated filename.

The logical source-map key already owns diagnostics and `#line` directives. It
must also own `Error.file`.

## Evidence

A single-module program supplied as `app.hex` records bytes for `main.hex` in
its Error value even though its generated `#line` directives name `app.hex`.
Therefore every existing Error-producing snippet whose logical key is not
literally `main.hex` has generated-output movement under this RFC.

Focused compilation:

```hexal
-- lib.hex
export fun make(): Error do
    return Error.new("Oops", "failed")
end
```

Generated literal zero:

```c
const uint8_t hex_lit_0_bytes[9] = {
    109, 97, 105, 110, 46, 104, 101, 120, 0
};
```

The bytes decode to `main.hex`. Generated `modules/lib.c` assigns that object
to `.hex_m_file` even though its `#line` directives correctly name `lib.hex`.

## Contract

- Every compiler-constructed Error records the exact logical source-map key of
  the module containing the source operation.
- `Error.new` records the key of its call site.
- Spawn, Channel, Mutex, and other generated recoverable concurrency Errors
  record the key of the operation or generated adapter source site they report.
- Line and column remain the source token's one-based coordinates.
- Canonical module identity does not replace the logical key: `graphics/math`
  is an identity; `graphics/math.hex` is the source filename.
- No host path, working directory, path discovery, or filesystem lookup occurs.
- Equal logical-key strings continue to deduplicate through the program-wide
  literal registry.

## Implementation

- Add the current logical source key to the module scope created by
  `CheckModules`; every child scope inherits it by reference/value with the
  existing module metadata.
- Pass `ModuleNode.LogicalKey` into `checkModule` and `moduleScope`.
- Make `checkErrorNewCall` build its file operand from the scope key.
- Pass each module's logical key into concurrency discovery and intern that key
  for generated Error objects.
- Delete checker and generator constants whose only purpose is the fixed
  `main.hex` fallback.
- Do not rewrite checked trees during generation; provenance is established at
  the phase that constructs each Error.
- Keep `Check(program)` on its existing synthetic single-module key `app.hex`.

## Invariants

1. Error file, diagnostic module, and `#line` filename use one logical source
   key for one source operation.
2. Canonical module IDs and logical source keys remain separate concepts.
3. No compiler-owned Error embeds an unrelated fixed filename.
4. Literal interning remains program-wide and deterministic.
5. The core compiler remains string-in/string-out.

## Validation

This section is exhaustive.

- `Error.new` in `app.hex` records `app.hex`.
- `Error.new` in imported `lib.hex` records `lib.hex`; its generated Error file
  literal and `#line` directive agree.
- Two modules constructing Errors record their respective keys and do not
  share the wrong filename literal.
- A nested logical key such as `graphics/math.hex` is preserved exactly.
- Repeated Error construction within one module interns the logical filename
  once.
- Generated spawn, Channel, and Mutex failure Errors use the logical key of
  their owning source module.
- No compiler-generated literal spells `main.hex` unless the supplied logical
  source key itself is exactly `main.hex` and is otherwise valid input.
- Single-module `checker.Check` uses `app.hex`.
- Repeated compilation produces byte-identical files.
- `docs/reference.md` states that compiler-constructed Error values record the
  logical source-map key.
- The snippet manifest moves only for snippets whose generated Error file
  literal changes.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Reading filesystem paths or resolving symlinks.
- Embedding absolute paths.
- Changing diagnostic rendering or sorting.
- Changing Error layout or exposing a foreign ABI.
- Replacing the program-wide literal registry.
