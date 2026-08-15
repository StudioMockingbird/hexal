# RFC 0060: Root-Owned C Entrypoint

- Kind: Rust-Style RFC
- Status: Implemented; conformance verified 2026-08-15
- Created: 2026-08-15
- Updated: 2026-08-15
- Scope: generated-artifact layout and compiler result contract
- Depends on: RFC 0034 (modules), RFC 0057 (Item 1 generated-artifact baseline
  and Item 6 header input structs), RFC 0059 (generator file organization)
- Coordinates with: RFC 0039 (C interoperability), ADR 0055 (filesystem and
  build driver), `AGENTS.md`, `docs/reference.md`, `docs/status.md`
- Supersedes: the `main.c`/`main.h`, `MainC`/`MainH`, failure-artifact, and
  `hex_module_root_run` contracts in `docs/reference.md`

## Summary

Stop generating `main.c` and `main.h`.

- Generate one shared program-support header named `hexal.h`.
- Every reachable module retains its existing C/header artifact path.
- The selected root module's C file additionally owns the process entrypoint
  and all once-per-process runtime definitions.
- Every module header includes `hexal.h`.
- `CompilationResult.Files` becomes the only generated-artifact surface.
- Failed compilation returns diagnostics and no generated files.

This removes a translation unit whose only program-specific action is calling
the root module, while retaining the useful single-copy shared-header boundary
under an accurate name.

## Motivation

The selected entrypoint already identifies the root module. A fixed
`main.c` filename therefore provides no additional routing information.
`main()` can execute the root statements in the root module translation unit
without an intermediate `hex_module_root_run()` function.

`MainC` and `MainH` duplicate entries in `Files` and create two artifact APIs.
The authoritative map is sufficient for the in-memory string-in/string-out
compiler and for a future filesystem driver.

The current `main.h` is not merely a wrapper: it carries program-wide runtime
types, builtin specializations, static inline helpers, literal objects, and
extern declarations used by every module. This RFC preserves that role as
`hexal.h`; it does not duplicate the content into every module header.

## Compiler API

`CompilationResult` becomes:

```go
type CompilationResult struct {
    Files    map[string]string
    Stderr   []string
    ExitCode int
    Stats    CompilationStats
}
```

Rules:

- Remove `MainC` and `MainH`; do not retain deprecated mirrors or replacement
  root-file fields.
- `Files` is non-nil on every result.
- Successful compilation returns `hexal.h` plus exactly one `.c` and one `.h`
  entry for each reachable module.
- Failed compilation returns an empty `Files` map. No failure C program or
  partial module artifact is emitted.
- `failureResult` does not invoke the generator. Generation duration is zero
  for failures before generation and preserves elapsed generation time for a
  generator failure.
- Diagnostics, exit-code meanings, reachable-source statistics, and the
  string-in/string-out boundary are otherwise unchanged.

Remove the unused compatibility surfaces:

- `generator.Generate`, whose two-string result assumes `main.c`/`main.h`;
- `generator.GenerateFailure`;
- helpers that exist only to construct or mirror those artifacts.

`generator.GenerateChecked` remains the generator entry used by
`compiler.Compile` and returns the authoritative artifact map.

## Successful artifact set

For dependency-first reachable module order `M`:

```text
Files = { "hexal.h" } union over m in M of {
    "modules/" + canonical(m) + ".c",
    "modules/" + canonical(m) + ".h"
}
```

Contracts:

- No key is `main.c` or `main.h`; `hexal.h` is the only compiler-support
  artifact.
- Artifact keys remain normalized logical strings; the compiler performs no
  filesystem operation.
- Unreachable sources produce no artifact.
- Each canonical module produces one pair, including diamond dependencies.
- A root source named `hexal.hex` retains the pair
  `modules/hexal.c/.h`; the separate root-level support artifact is
  `hexal.h`, so the keys do not collide.
- `hexal.h` is reserved only in the generated artifact map; it does not reserve
  or reject a logical Hexal source name.
- Dependency order, canonical identity, case sensitivity, module-owner symbol
  encoding, header guards, and `#line` source filenames remain unchanged.
- Case-distinct logical modules remain distinct `Files` keys. Detecting whether
  a host filesystem can materialize both belongs to the future filesystem
  driver, not the core compiler.
- The root pair is identified from the supplied entrypoint using the existing
  canonical module mapping. It is not renamed to a fixed filename.

## Shared `hexal.h`

Rename the useful shared-header role from `main.h` to `hexal.h`.

Rules:

- Build `hexal.h` once from the merged program-wide discovery state.
- Use the file guard `HEXAL_H`; remove `HEXAL_MAIN_H`.
- Every module header includes `hexal.h`; remove every
  `#include "main.h"`.
- `hexal.h` contains all content previously owned by `main.h`: target
  includes and `static_assert` checks, builtin runtime types, program-wide
  builtin specializations, canonical literal objects, stateless inline
  helpers, and extern declarations for stateful runtime operations.
- Content order, dependency order, linkage, and storage duration remain
  unchanged.
- No additional support, runtime, or umbrella header is generated.

Each module header remains self-contained through `hexal.h` and does not
include the root module's header or another module header. Module-specific
content remains inside the module guard: module-owned types and stateless
helpers, spawn argument frames, exported declarations, and foreign prototypes.

## Root module C file

Every module C file includes only its own module header.

The selected root module C file additionally owns:

- the external runtime definitions and process-wide state previously emitted
  in `main.c`, including the scheduler, channel/mutex runtime, and I/O gate
  when required by merged discovery;
- `int main(void)`;
- direct execution of the root module's executable statements.

Rules:

- Emit process-wide runtime definitions exactly once and only in the root C
  file.
- Keep runtime definition order and initialization order unchanged.
- Emit the root module's ordinary functions, methods, specializations, and
  spawn adapters under their existing linkage and ordering contracts.
- Render root statements directly inside `main()` with their existing lexical
  scope, defer behavior, diagnostics, and `#line` directives.
- Remove the declaration, definition, and calls of
  `hex_module_root_run()`.
- Without concurrency, `main()` executes root statements and returns
  `EXIT_SUCCESS`.
- With concurrency, `main()` initializes the scheduler, executes root
  statements as the root task, completes the root task, then returns
  `EXIT_SUCCESS`.
- Non-root modules never declare or define `main()` or process-wide runtime
  state.

The root module header does not declare `main()`. C consumers include the
module header for exported Hexal declarations; `main` is the process entry,
not part of the module API.

## Failure behavior

Compilation failure is represented only by `CompilationResult`:

- `ExitCode == ExitFailure`;
- `Stderr` contains the structured diagnostics;
- `Files` is empty;
- `Stats` reports work completed before failure.

Do not emit a compilable program that returns `EXIT_FAILURE`. A failed source
program has no valid generated project, and the compiler result already carries
the failure status.

## C and driver boundary

- A driver writes every entry in `Files` and compiles every emitted `.c`
  translation unit.
- A driver materializes generated artifacts in an output root that cannot
  overwrite supplied Hexal or foreign C inputs.
- The entrypoint's canonical module C file supplies `main()`; the driver does
  not synthesize or search for `main.c`.
- C code consumes a module's exported API through that module's generated
  header, which includes `hexal.h`. There is no `main.h` compatibility header.
- A future library compilation mode must explicitly omit `main()` and define
  its exported library surface. It must not reuse or preserve `main.h`
  speculatively.
- C source/header discovery, file writing, compiler invocation, and linker
  behavior remain outside the core compiler under ADR 0055.

## Required implementation changes

### Compiler

- Remove `MainC` and `MainH` from `CompilationResult` and all construction
  paths.
- Return only `Files` on success and an empty map on failure.
- Remove failure-C generation and its generation-time accounting.

### Generator

- Perform the generator changes in `compiler/generator/emission.go`, which owns
  pair emission and header construction after RFC 0059.
- Replace main-pair emission with root-module entrypoint emission.
- Rename RFC 0057 Item 6's `mainHeaderInput` and `mainHeader` to
  `hexalHeaderInput` and `hexalHeader`; emit their result as `hexal.h` without
  changing the shared program-support role.
- Emit process-wide runtime definitions into the root module C file.
- Render root statements directly in `main()`.
- Remove `moduleHeaderInput.rootRun` and all root-run declarations.
- Remove `Generate`, `GenerateFailure`, and dead main-pair helpers.

### Workbench

- Remove `mainC` and `mainH` from the compile response.
- Continue returning and displaying `files` as the complete output.
- On failure, display diagnostics with no generated-file sections.

### Tests

- Remove all reads and assertions of `MainC` and `MainH`.
- Make shared integration helpers inspect `Files` and the selected root module
  pair only.
- Update exact generator expectations so root C contains `main()` directly and
  `hexal.h` contains the guarded program-support contract.
- Assert successful artifact keys equal `hexal.h` plus the existing reachable
  module paths exactly.
- Assert every failure class returns a non-nil empty `Files` map.
- Assert no generated content includes `main.h`, `HEXAL_MAIN_H`, or
  `hex_module_root_run`.
- Assert every module header includes `hexal.h` and no module header includes
  another module header.
- Assert process-wide runtime definitions and `main()` occur only in the root
  C file.
- Update dormant C23 canary helpers to materialize `Files` and compile every C
  artifact; keep them dormant with no runnable test entrypoints.
- Intentionally regenerate the RFC 0057 snippet SHA-256 manifest only after all
  new artifact and semantic assertions pass.

Ordinary tests remain pure Go and invoke no external C toolchain.

## Reference update

After implementation stabilizes, update `docs/reference.md` once:

- replace the generated-artifact contract in the modules overview paragraph
  near the start of the semantic rules;
- replace the complete `Generated artifact split` subsection;
- define `Files` as the sole artifact result and remove `MainC`/`MainH`;
- define success as `hexal.h` plus one pair per reachable module and failure as
  no artifacts;
- rename the shared program-support header to `hexal.h`;
- retain the `modules/<canonical-path>.c/.h` mapping;
- assign process-wide runtime definitions and `main()` to the root module C;
- remove the thin-entry, `main.h`, and `hex_module_root_run` contracts;
- retain the in-memory boundary, canonical module mapping, module visibility,
  symbol identity, ordering, and source mapping contracts.

Do not retain compatibility wording for the removed layout.

## Required order

1. Add focused failing tests for the new result and artifact contracts.
2. Remove the mirrored compiler result fields and failure artifacts.
3. Rename the shared program-support artifact to `hexal.h` and update module
   includes.
4. Move process-wide definitions and direct root execution into root C.
5. Remove obsolete main-pair and root-run code.
6. Update integration helpers, workbench responses, dormant canaries, and all
   exact-output tests.
7. Regenerate and verify the snippet artifact manifest.
8. Update `docs/reference.md` and verify no stale current contract remains.

## Validation

- `gofmt` only touched Go files.
- `go test ./compiler/generator`.
- `go test ./compiler/tests/integration`.
- `go test ./workbench/...`.
- `go test ./...`.
- `go vet ./...`.
- `go test -tags c23 ./...` and `go vet -tags c23 ./...` type-check the dormant
  canaries without adding runnable entrypoints.
- Search active code, tests, workbench data, and `docs/reference.md` for stale
  `MainC`, `MainH`, exact artifact literals `"main.c"` and `"main.h"`,
  `HEXAL_MAIN_H`, and `hex_module_root_run`; retained occurrences must be
  historical comments in closed specs only. The exact-literal search must not
  match the retained logical source name `main.hex`.
- Rebuild the workbench into `bin/` and restart it.
- Remove RFC 0060 from `docs/status.md` and mark it
  `Implemented; conformance verified YYYY-MM-DD`.

## Non-goals

- A library compilation target.
- More than one compiler-support header.
- Filesystem reads, writes, discovery, caching, or incremental compilation.
- Changing module syntax, import resolution, visibility, or type identity.
- Changing C symbol mangling for module-owned declarations.
- Demand-driven runtime helper generation.
- Changing runtime, concurrency, allocation, error, or I/O semantics.
- Changing the entrypoint function signature from `int main(void)`.
- Preserving source or binary compatibility for the removed Go result fields
  or generated artifact names.

## Drawbacks

- Removing `MainC` and `MainH` is a breaking Go API change.
- Tools that assumed fixed `main.c`/`main.h` names must consume `Files` and the
  supplied entrypoint instead.
- The RFC 0057 exact-output manifest must be intentionally replaced.

## Expected result

- The root module pair is the complete executable root.
- Non-root pairs remain ordinary module translation units.
- `hexal.h` is the single shared program-support header.
- No generated artifact exists solely to forward execution to another file.
- `CompilationResult` has one authoritative artifact surface.
