# Execution Plan 0209: Raylib External-Package Conformance

- Kind: Execution Plan for RFC 0039, RFC 0192, and RFC 0193
- Status: Implementation-ready after RFC 0039, RFC 0192, and RFC 0193; implementation not started
- Created: 2026-09-15
- Scope: prove end to end that an unmodified external Raylib distribution can
  be imported, compiled, linked, and called from Hexal without handwritten
  bindings for the supported API subset
- Depends on: RFC 0039 (C ABI surface), RFC 0192 (physical C inputs), RFC 0193
  (automatic header bindings), and closed RFC 0052's qualified
  `x86_64-windows-gnu` Zig backend
- Coordinates with: deferred RFC 0191 (advanced C interop) and RFC 0183
  (cross-target generated-C qualification)
- Does not add: a package manager, project manifest, Raylib-specific compiler
  logic, network access during tests/builds, vendored Raylib binaries, automatic
  foreign build-system execution, or new Hexal syntax

## Summary

Raylib is the first complete external-package conformance target for Hexal's C
interop arc. The initial proof imports `raylib.h` directly, constructs a
Raylib `Color`, calls the basic window/drawing API, links a compatible static
Raylib archive and its Windows system libraries, and produces one executable.

The proof must use the same general mechanisms any C package uses:

```text
raylib.h                 -> Alias from c "raylib.h"
include directory        -> -c-include
static Raylib archive    -> -archive
Windows dependencies     -> -system-library
header/build definitions -> -c-define and -c-env when required
```

Passing this plan demonstrates the initial interop architecture. It does not
mean that every declaration in `raylib.h` is representable or that Raylib has
become a Hexal builtin.

## Why Raylib

- It is a real, substantial C library rather than a purpose-built fixture.
- Its public header contains ordinary functions, scalars, records, pointers,
  aliases, enums, macros, and surfaces intentionally deferred by RFC 0191.
- Its basic window API needs only the initial RFC 0039 subset.
- Its static build exercises transitive operating-system libraries.
- Its explicit load/unload functions exercise manual foreign-resource cleanup.
- Raylib officially supports Windows, Linux, and macOS and permits static
  linking under its zlib/libpng-style license.

The official basic-window example uses `InitWindow`, `WindowShouldClose`,
`BeginDrawing`, `ClearBackground`, `EndDrawing`, and `CloseWindow`. This plan
keeps that recognizable flow while avoiding macro-specific shortcuts.

## Package meaning

"External package" means one coherent, versioned set of:

- public headers;
- ABI-compatible source, objects, or static archives;
- required definitions and environment;
- target-specific system-library dependencies; and
- license/version metadata.

It does not introduce a Hexal package format. Until a future manifest and
package manager exist, users pass those facts through RFC 0192's command-line
surface.

## Qualified dependency

- Begin with the official Raylib 6.0 release for the
  `x86_64-windows-gnu` target.
- Phase 0 records the immutable upstream tag commit, distribution filename,
  archive SHA-256, `raylib.h` SHA-256, archive format, target ABI, and included
  license. A tag name alone is not sufficient evidence.
- Use a GNU-compatible x86-64 Windows static archive built for the same ABI as
  RFC 0052's Zig target. An MSVC `.lib` is not silently treated as equivalent.
- If the official artifact is incompatible, qualify a static archive built
  from the same pinned source revision with Zig's C frontend. Record the exact
  source list, definitions, arguments, hashes, and license; do not silently
  switch revisions or ABI families.
- The initial expected Windows link dependencies are `opengl32`, `gdi32`, and
  `winmm`, matching Raylib's ordinary MinGW desktop recipe. Phase 0's plain-C
  probe must confirm the exact closed set for the pinned archive before the
  Hexal fixture is committed. Any difference updates this plan rather than
  being hidden in a raw link argument.
- Raylib is not a repository submodule or compiler dependency in this plan.
  The conformance runner receives a provisioned package root through
  `HEXAL_RAYLIB_ROOT`.
- No test downloads Raylib. CI or a maintainer provisions the verified bundle
  before explicitly enabling the Raylib gate.

## Canonical package layout

`HEXAL_RAYLIB_ROOT` resolves to:

```text
<raylib-root>/
    include/
        raylib.h
    lib/
        libraylib.a
    LICENSE
```

The gate validates every pinned hash and required path before invoking Hexal.
With the Raylib test tag enabled, a missing variable, missing file, wrong hash,
or incompatible archive is a setup failure, never a skipped passing test.

## Canonical Hexal program

`app.hex`:

```hexal
import
    Raylib from c "raylib.h"
end

title := "Hexal + Raylib"
background := Raylib.Color(
    r = 245,
    g = 245,
    b = 245,
    a = 255,
)
mut close_requested := false

unsafe do
    Raylib.InitWindow(800, 450, title.c_pointer())
    close_requested = Raylib.WindowShouldClose()
    if !close_requested then
        Raylib.BeginDrawing()
        Raylib.ClearBackground(background)
        Raylib.EndDrawing()
    end
    Raylib.CloseWindow()
end
```

This program deliberately constructs `Color` instead of using `RAYWHITE`.
`RAYWHITE` is a struct-valued preprocessor macro: RFC 0193 does not import
macros, and RFC 0039's handwritten `constant` supports scalar constants only.
Using `Raylib.RAYWHITE` must therefore produce the automatic-import guidance
diagnostic. A user can construct `Color` as above or expose a small C wrapper.

Every foreign call remains inside `unsafe do ... end`. Header import is typed
and automatic; it does not assert that Raylib's implementation is safe.

## Canonical build

With placeholders expanded by the invoking shell or test harness, not Hexal:

```text
hexal build \
  -root ./raylib-demo \
  -entry app.hex \
  -out ./raylib-demo/build/raylib-demo.exe \
  -c-include <raylib-root>/include \
  -archive <raylib-root>/lib/libraylib.a \
  -system-library opengl32 \
  -system-library gdi32 \
  -system-library winmm
```

If the pinned Raylib artifact requires build-environment values, add explicit
RFC 0192 overrides:

```text
-c-env NAME=VALUE
```

The same effective environment reaches header inspection, generated-C
compilation, archive linking, and the linker. Hexal performs no placeholder or
environment expansion itself.

## End-to-end flow

1. The driver reads `app.hex` and resolves the explicit CLI paths.
2. RFC 0039 discovers the reachable quoted C import for `raylib.h`.
3. RFC 0193 invokes the qualified frontend with the target, include directory,
   definitions, and effective environment.
4. The importer exports supported declarations owned by `raylib.h`, including
   `Color` and the six functions used above. Unsupported unrelated declarations
   are omitted whole.
5. The normalized binding source is inserted under its deterministic in-memory
   key and checked through the ordinary Hexal pipeline.
6. Generated C includes `raylib.h`, constructs the native C `Color`, and calls
   the original Raylib symbols directly. No Raylib-specific wrapper, lookup,
   allocation, or dispatch is emitted.
7. RFC 0192 compiles the generated C23 translation units.
8. RFC 0192 links the generated objects, the pinned Raylib archive, and the
   required Windows libraries in deterministic order.
9. The driver atomically publishes one executable.
10. The opt-in runtime gate launches it, observes successful window lifecycle,
    and requires exit status zero.

## Expected automatic surface

The importer must recover at least these declarations from the unmodified
header:

```c
typedef struct Color {
    unsigned char r;
    unsigned char g;
    unsigned char b;
    unsigned char a;
} Color;

void InitWindow(int width, int height, const char *title);
void CloseWindow(void);
bool WindowShouldClose(void);
void BeginDrawing(void);
void ClearBackground(Color color);
void EndDrawing(void);
```

The exact generated RFC 0039 binding text is pinned as test data. It must map:

- C `int` through the qualified target's resolved `Int32` representation;
- `unsigned char` to `UInt8`;
- C `bool` to `Bool`;
- `const char *` to the read-only nullable byte-pointer form selected by RFC
  0039; and
- `Color` to one complete foreign record whose field order and C spelling are
  retained.

The C frontend remains the authority for those C facts. This plan does not
hard-code Raylib declarations in the compiler.

## Import coverage report

The Raylib gate records a deterministic importer report for the pinned header:

- supported exported root declarations by kind;
- omitted root declarations grouped by the first unsupported reason;
- required transitive types retained to close supported interfaces; and
- macros observed only by the dedicated macro inventory probe, not imported as
  declarations.

The report is test evidence, not a language contract or generated project
artifact. Counts are pinned only to detect importer drift for the selected
Raylib revision; changing Raylib requires intentional review and rebaselining.

An omitted declaration does not automatically justify expanding Hexal. Promote
one capability from deferred RFC 0191 only when a useful Raylib program cannot
be expressed cleanly through the supported subset or a small C wrapper.

## Test lifecycle

### Ordinary gate

- `go test ./...` performs no download, does not require Raylib, and launches
  no window.
- Pure-Go RFC 0039/RFC 0193 fixtures remain responsible for parser, checker,
  normalizer, diagnostic, and generated-text behavior.

### External package gate

- Raylib tests live in `compiler/tests/c23validation` with build constraints
  requiring both `c23` and `raylib`.
- Run them explicitly with:

```text
go test -tags "c23 raylib" ./compiler/tests/c23validation
```

- The gate validates `HEXAL_RAYLIB_ROOT` and hashes before use.
- Compile/link tests always run when the tag is selected and never require an
  interactive desktop.
- These tests assert the prepared binding, generated C, complete linker command,
  absence of handwritten bindings, and successful executable production.

### Runtime window gate

- Running a graphical executable is a separate explicit subtest enabled by
  `HEXAL_RAYLIB_RUN_WINDOW=1` in addition to the tags.
- Without that variable, the compile/link test still passes or fails normally;
  the result must say that graphical execution was not requested, not that it
  passed.
- The canonical program opens, draws one frame, closes, and exits without user
  input. It must not use `WindowShouldClose` as an unbounded interactive wait.
- The harness enforces a short timeout, kills a hung child, and reports its
  stdout, stderr, and exit status.
- Exit status zero proves lifecycle completion. Screenshot correctness and GPU
  rendering fidelity are outside this compiler conformance plan.

## Failure classification

- Missing root, files, hashes, or wrong archive ABI: Raylib gate setup failure.
- Header lookup, preprocessing, AST decoding, or normalization process failure:
  RFC 0193 C-compilation-stage failure.
- Unsupported required ABI declaration: RFC 0039 Hexal compilation diagnostic.
- Missing `Raylib.<name>`: automatic-import guidance diagnostic.
- Missing Raylib or Windows symbol, incompatible archive, or wrong link order:
  RFC 0192 link-stage failure.
- Window creation failure or timeout: runtime conformance failure, distinct from
  compilation and linking.

The gate first compiles and links Raylib's equivalent plain-C program with the
same Zig target, archive, and system libraries. If that control fails, the
package/toolchain setup is invalid and no Hexal-specific conclusion is drawn.

## Non-goals

- Automatically acquiring or updating Raylib.
- Running Raylib's Make, CMake, Zig, or project-builder files.
- Supporting shared Raylib DLL loading.
- Importing `RAYWHITE` or other macros automatically.
- Claiming all Raylib functions are available.
- Adding callbacks, function-pointer values, variadics, raw unions, bit-fields,
  flexible arrays, or other RFC 0191 capabilities.
- Asset packaging, hot reload, game loops, input abstractions, resource RAII,
  or a Hexal game framework.
- Qualifying Linux, macOS, AArch64, or another backend in this plan.

## Required sweep

Inventory and reconcile:

- RFC 0039's Raylib-shaped fixtures and unsupported-surface diagnostics;
- RFC 0192 archive, system-library, environment, link-order, and failure tests;
- RFC 0193 declaration-origin filtering, omission reasons, macro guidance, and
  complete-record normalization;
- tagged C23 harness setup/failure conventions and external dependency gates;
- generated-C assertions for foreign records, String byte pointers, and direct
  calls; and
- status entries that imply Raylib or all C libraries are already qualified.

Do not put Raylib names, declarations, paths, or system libraries into the core
compiler. Do not add Raylib to ordinary tests or download it during any build.

## Detailed implementation plan

### Phase 0: qualify the external package

1. Resolve the official Raylib 6.0 tag to its immutable commit.
2. Select or build one GNU-compatible `x86_64-windows-gnu` static archive.
3. Record distribution, header, archive, and license hashes in focused test
   metadata.
4. Compile and link the equivalent plain-C one-frame program with Zig 0.16.0.
5. Confirm the exact required Windows system-library set and archive order.
6. If qualification fails, stop and revise this plan; do not patch Hexal around
   an incompatible package.

### Phase 1: pin importer behavior

1. Run RFC 0193 over the unmodified `raylib.h`.
2. Confirm the six required functions and complete `Color` record.
3. Pin their normalized RFC 0039 declarations and exact ABI spellings.
4. Produce the deterministic supported/omitted report.
5. Confirm `RAYWHITE` is unavailable and receives the exact guidance
   diagnostic.
6. Confirm unrelated unsupported declarations do not reject the required
   subset.

### Phase 2: compile and link

1. Add the canonical Hexal fixture with no handwritten binding.
2. Invoke the public `hexal build` path with include, archive, and named system
   libraries.
3. Assert generated C contains the exact include, native `Color` construction,
   and six direct calls without wrappers.
4. Assert deterministic command/environment/link ordering.
5. Assert one executable is atomically published and no Raylib DLL is required.

### Phase 3: external gate

1. Add the combined `c23 && raylib` test constraints.
2. Validate the provisioned root and hashes before starting a child process.
3. Keep compilation/linking noninteractive and mandatory under the tag.
4. Add the separately enabled one-frame runtime subtest and timeout cleanup.
5. Preserve complete failure-stage and command evidence without exposing
   environment values.

### Phase 4: documentation and handoff

1. Keep this plan's canonical source, command, and pipeline synchronized with
   the implemented CLI.
2. Update `docs/status.md` when the gate lands.
3. Review `docs/reference.md`; make no edit unless the user explicitly approves
   one and an implemented language contract is missing.
4. Rebuild `hexal` and restart `hexal play` before handoff.

## Validation

This list is exhaustive.

- The provisioned dependency matches the pinned version, target, ABI, file
  layout, and hashes; invalid setup fails rather than skips under the Raylib tag.
- The plain-C control compiles and links with the same backend, archive, and
  system-library set.
- The unmodified header automatically exposes `Color`, `InitWindow`,
  `CloseWindow`, `WindowShouldClose`, `BeginDrawing`, `ClearBackground`, and
  `EndDrawing` with the expected RFC 0039 types.
- The importer emits a stable supported/omitted report and retains no unrelated
  transitive exports.
- `Raylib.RAYWHITE` reports the exact automatic-import guidance diagnostic;
  constructing `Raylib.Color(...)` succeeds without a binding or wrapper.
- The canonical program contains no handwritten `extern c` declaration.
- Generated C includes `raylib.h`, uses the native `Color`, and calls original
  Raylib symbols directly without forwarding wrappers or marshalling
  allocations.
- The public build command links the pinned archive followed by the verified
  Windows libraries and atomically publishes one executable.
- Compile/link conformance runs without an interactive desktop whenever both
  tags are selected.
- With `HEXAL_RAYLIB_RUN_WINDOW=1`, the executable opens, draws one frame,
  closes, exits zero within the timeout, and leaves no child process.
- Without that variable, graphical execution is explicitly reported as not
  requested and is never represented as passing runtime coverage.
- Ordinary tests require no Raylib installation, network, window, or GPU.
- No compiler package contains a Raylib-specific name or branch.
- No existing snippet-manifest hash changes; this external fixture is not a
  workbench snippet and adds no generated artifact to the ordinary catalog.

## Settled decisions

- Raylib is the first complete third-party C package conformance target.
- The initial proof is static and `x86_64-windows-gnu` only.
- The dependency is provisioned externally and pinned by hashes; no build or
  test downloads it.
- Direct automatic header import is mandatory for the canonical program.
- Struct-valued macros are avoided through native foreign-record construction.
- Compile/link coverage is mandatory under explicit tags; window execution is
  separately opt-in.
- An omitted Raylib surface creates evidence, not automatic pressure to enlarge
  Hexal.

## Open questions

None.

## Reference synchronization

This plan adds no language contract. Do not edit `docs/reference.md` merely
because Raylib passes. If implementation exposes a missing general C-interop
rule, amend its owning RFC before implementation and update the reference only
after behavior stabilizes and the user explicitly approves the edit.
