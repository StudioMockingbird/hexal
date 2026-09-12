# ADR 0055: Filesystem and Build Driver

- Kind: Architecture Decision Record (ADR)
- Status: Closed; every Validation item is implemented and covered by
  pure-Go tests plus the non-skipping `c23` qualification gate
- Created: 2026-08-14
- Updated: 2026-09-12
- Scope: filesystem discovery, artifact materialization, backend invocation,
  and executable publication outside the core compiler
- Depends on: RFC 0052 and the reference's module/artifact contract
- Coordinates with: RFC 0039 for future foreign C support and ADR 0166 for
  toolchain version presentation

## Decision

`cmd/hexal` uses one internal filesystem/build driver. v1 builds a
self-contained Hexal project on x86-64 Windows for the qualified
`x86_64-windows-gnu` profile through an installed Zig 0.16.0 found on `PATH`.

The core compiler remains string-in/string-out and process-free. It receives
all source contents plus an explicit compiler-owned target identity and returns
all generated contents. The driver alone reads and writes files or starts
processes.

The current `driver/` and `cmd/hexal/` tree proves the basic pipeline but is not
the final contract: it does not enforce the exact Zig version and facility
contract, passes `Project{}`, combines compilation and linking, and has
incomplete output-path and diagnostic handling.

## v1 scope

In scope:

- discover `.hex` sources under one source root;
- assign normalized logical keys without parsing imports;
- call `compiler.Compile` with the explicit qualified profile;
- materialize every generated artifact in an isolated staging tree;
- compile every generated `.c` separately as C23;
- link the resulting objects;
- atomically publish one executable;
- `hexal build`, `hexal doctor`, `hexal version`, `hexal play`, and
  `hexal help`; and
- stage-specific, reproducible build diagnostics.

Out of scope:

- other hosts or targets and cross-compilation;
- foreign C sources, headers, objects, libraries, definitions, or build
  systems;
- project manifests and package management;
- object caching and incremental compilation;
- static linkage;
- multiple compiler versions; and
- a stable public Go driver API.

## Compiler boundary

The driver calls:

```text
Compile(sources map[string]string,
        entrypoint string,
        project Project{Target: TargetX86_64WindowsGNU})
    CompilationResult
```

The driver discovers candidate source files only. The compiler parses imports,
resolves relative module paths, chooses reachability, checks the program, and
generates artifacts. The driver never implements a second import resolver.

Every `.c` entry in `CompilationResult.Files` is compiled, including component
translation units under `hexal/`; compilation is not limited to `modules/`.

## Internal API

The build driver remains internal to the CLI until its API stabilizes. Move or
keep its implementation under an `internal` package boundary; do not advertise
`Backend`, `BuildOptions`, or `Doctor` as a supported embedding API in v1.

Conceptual internal result:

```go
type BuildResult struct {
    Executable string
    HexalVersion string
    Commands   []CommandResult
}

type CommandResult struct {
    Stage            BuildStage
    Tool             string
    Arguments        []string
    WorkingDirectory string
    Stdout           string
    Stderr           string
    ExitCode         int
}

type BuildError struct {
    Stage   BuildStage
    Message string
    Command *CommandResult // present only for an invoked external command
}
```

`BuildStage` distinguishes configuration, filesystem, Hexal compilation,
C compilation, and linking. Compiler diagnostics pass through unchanged.
External command records preserve the tool, complete argument vector, working
directory, and separated stdout/stderr. They do not capture the complete
process environment or secrets.

The CLI exits `1` for every failed build. A child process's actual exit status
remains available in its `CommandResult`. A failed build returns its populated
`BuildResult` with every command completed before failure plus one `BuildError`;
callers do not lose diagnostic evidence because the final executable was not
published.

## Configuration

v1 has no project manifest. `hexal build` accepts:

```text
-root <directory>       source root; default current directory
-entry <logical-key>    entrypoint; default main.hex
-out <file>             final executable; default <root>/build/<entry>.exe
```

The source root and entrypoint defaults are conventions, not host discovery in
the compiler.

The default intermediate directory is `<root>/build/.hexal/`. Its location may
later receive a flag, but v1 needs no second output option.

## Source discovery

- Resolve the source root to an absolute canonical directory before walking.
- Reject a missing or non-directory source root.
- Do not follow source-tree directory symlinks or junctions in v1.
- Read regular files whose extension is exactly `.hex`.
- Convert relative host paths to `/`-separated logical keys.
- Reject two paths whose logical keys collide under the qualified Windows
  profile's case-folding rules.
- Skip only the exact resolved intermediate/staging directory. A different
  source directory named `build` remains valid.
- Pass every discovered source to the compiler; do not determine reachability.

## Materialization and publication

All generated C, headers, and objects live inside a fresh staging directory
under `<root>/build/.hexal/`.

For every compiler artifact:

- validate the logical key again as a driver backstop;
- reject absolute paths, traversal, host separators, symlink escapes, and
  case-folded collisions;
- create only directories contained by the canonical staging root; and
- write the complete supplied content without altering line endings or `#line`
  mappings.

An explicit `-out` authorizes only the final executable to be published outside
the intermediate directory. It does not authorize intermediate files there.

Publication rules:

- resolve and validate the destination's parent directory;
- link to a temporary sibling file in that directory;
- close the linker process successfully before publication;
- atomically replace an existing destination with the native Windows replace
  operation, or atomically rename the sibling when no destination exists;
- a failed build removes its staging tree and temporary executable; and
- a successful build removes its staging tree once publication completes,
  so repeated builds cannot accumulate stale trees; and
- a failed build leaves any previously published executable unchanged.

An existing regular output file may be replaced by a successful explicit
build. A directory, symlink, junction, or non-regular destination is rejected.

## Backend invocation

The driver resolves `zig` through `PATH`, requires exactly version `0.16.0`,
and validates its reported `lib_dir`. It maps the explicit
`TargetX86_64WindowsGNU` profile to Zig's `x86_64-windows-gnu` target spelling
and dynamic-UCRT policy, and rejects any other host or profile before starting
Zig.

Every command record identifies the resolved executable and reported tool
version. The installed backend is non-cacheable until a later distribution
design supplies a stable backend identity.

Generated translation units are sorted by normalized logical filename. For
each `.c`, invoke one object compilation:

```text
zig cc -std=c23 -target x86_64-windows-gnu ... -c <source> -o <object>
```

After every object succeeds, sort objects by the corresponding logical source
key and invoke a separate link command. Go map iteration never determines a
command argument order.

The driver does not choose or invoke LLD directly. Zig owns linker selection.
The Windows profile links dynamically against UCRT; v1 exposes no static-link
option.

## Required facility inventory

Generated C selects headers on demand. Backend qualification covers the union
of all headers and C23 facilities reachable from supported generated programs:

```text
Portable: <errno.h> <inttypes.h> <limits.h> <math.h> <stdatomic.h>
          <stdckdint.h> <stddef.h> <stdint.h> <stdio.h> <stdlib.h>
          <string.h>
Windows:  <windows.h> <process.h>
```

`<threads.h>` is not required. The Windows runtime uses native threading
primitives. A missing facility is a toolchain failure; the compiler does not
emit a fallback implementation merely to support an unqualified backend.

The inventory is derived mechanically from the generator requirement collector
and package templates. A guard test fails when production begins using a header
or language facility absent from the qualification fixture.

## `hexal doctor`

`doctor` checks the backend, not the current project. It never looks for
`main.hex` and therefore needs no note/warning severity solely for project
absence.

It reports every independently checkable problem:

- resolved Zig executable;
- exact Zig version;
- contained readable `lib_dir`;
- full generated facility/header probe; and
- compile-link-run probe for `x86_64-windows-gnu`.

If the backend prerequisite is missing or cannot run, dependent checks are
skipped and that prerequisite failure is reported once. Success prints
`doctor: all checks passed`; any failure prints one report and exits `1`.

## CLI

```text
hexal build [options]
hexal doctor
hexal version
hexal --version
hexal play
hexal help
```

Dispatch remains a flat match. Commands return errors; one top-level site
prints `hexal: <message>` and chooses the process exit status. Do not add a CLI
framework or reserve commands that have no behavior.

`play` invokes the separate workbench package specified by ADR 0166. It does
not place HTTP routes, embedded assets, or snippet logic in `cmd/hexal` or the
build driver.

Exact command-dispatch failures:

```text
unknown command: hexal: unknown command "<name>"       exit 1
missing command: print usage, then hexal: expected a command   exit 1
```

## Error ownership

- Configuration: invalid flags, roots, entrypoint, host, profile, or output.
- Filesystem: discovery, read, staging, containment, collision, cleanup, or
  publication failure.
- Hexal compilation: pass `CompilationResult.Stderr` through unchanged.
- C compilation: name the failing logical translation unit and retain command,
  stdout, stderr, and exit status.
- Link: retain the link command, stdout, stderr, and exit status.

Do not collapse C compilation and link failure into one message.
For a Hexal-compilation failure the CLI prints the compiler's rendered
diagnostics and no additional generic `compilation failed` line.

## Validation

This section is exhaustive for ADR 0055.

- The core compiler performs no filesystem access or process execution.
- v1 rejects a non-x86-64-Windows host or non-qualified profile before invoking
  Zig.
- Backend resolution uses `PATH`, accepts exactly Zig 0.16.0, validates
  `lib_dir`, and records the resolved executable.
- The installed backend is non-cacheable.
- Source discovery produces normalized logical keys and does not parse imports.
- Discovery skips only the exact intermediate directory, not every directory
  named `build`.
- Source symlinks/junctions and case-folded logical-key collisions are rejected.
- The driver passes `TargetX86_64WindowsGNU`, never `Project{}`, for a binary
  build.
- Every generated `.c`, including each selected `hexal/` component, is compiled
  exactly once in deterministic logical-key order.
- C compilation and linking use separate commands and separate error stages.
- Link object order is deterministic and independent of Go map iteration.
- Command records contain tool, arguments, working directory, separated
  stdout/stderr, child exit status, and stage without capturing the complete
  environment.
- Generated artifacts remain inside the canonical staging root and preserve
  their exact content and `#line` mappings.
- Absolute, traversal, separator, symlink, junction, and case-collision artifact
  escapes are rejected.
- Default output is `<root>/build/<entry>.exe`.
- Explicit `-out` may place only the final executable elsewhere.
- Successful publication atomically replaces an existing regular executable;
  failure preserves the previous executable and removes temporary output.
- `doctor` performs no project discovery and exercises the complete facility
  probe plus a real compile-link-run probe.
- `version`, `--version`, and `help` exit `0`; unknown and missing commands exit
  `1` with stable messages.
- `play` starts only the modular loopback workbench server; no standalone
  workbench executable remains.
- The project-level build result records the Hexal version once; command
  records do not repeat it.
- The driver API is internal and `cmd/hexal` contains only argument handling and
  wiring.
- Ordinary `go test ./...` remains pure Go and needs no backend.
- The official external qualification gate does not skip a missing or invalid
  installed backend.

## Implementation plan

Implementation ownership:

```text
internal/backend   supplied by RFC 0052; backend identity and tool operations
internal/driver    discovery, staging, command orchestration and publication
cmd/hexal          argument parsing and presentation only
compiler/          unchanged here except consuming RFC 0052's Project profile
```

### Phase 1: internal boundary and deterministic commands

1. Move or narrow `driver` behind an internal package boundary.
2. Add `BuildResult`, `CommandResult`, `BuildError`, and stage-aware errors.
3. Replace `CombinedOutput` with separated stdout/stderr capture and exact
   argument recording.
4. Sort generated translation units and resulting objects by logical key.
5. Compile each translation unit to an object, then link separately.

### Phase 2: profile/backend integration

1. Consume RFC 0052's exact-version `PATH` backend resolution.
2. Reject unqualified hosts before invoking Zig.
3. Pass `TargetX86_64WindowsGNU` through `Project`.
4. Add the facility-inventory guard and full qualification probe.

### Phase 3: filesystem safety

1. Replace the broad `build`-directory skip with exact staging-root exclusion.
2. Implement canonical root, symlink/junction, containment, and case-collision
   checks.
3. Materialize into a fresh per-build staging tree.
4. Implement sibling-temporary final linking and atomic executable replacement.
5. Preserve the previous executable and clean partial output on every failure.

### Phase 4: CLI and doctor cleanup

1. Remove project discovery from `doctor`.
2. Keep only `build`, `doctor`, `version`, the top-level `--version` alias,
   `play`, and `help`.
3. Make the CLI render structured driver failures at one site.
4. Sweep `driver/`, `cmd/hexal/`, and their tests under the CARE comment policy
   in `AGENTS.md` (Contract, Architecture, Rationale, or Edge): remove RFC/ADR
   provenance and non-ASCII comments while preserving local contracts.

### Phase 5: conformance

1. Implement every Validation item.
2. Run pure-Go tests without a toolchain.
3. Run the non-skipping official Zig qualification gate on x86-64 Windows.
4. Run deterministic command/artifact comparisons and executable fixtures.
5. Review `docs/reference.md` only for the compiler-visible `Project.Target`
   contract; request approval before editing it.

## Deferred work

Dedicated future specifications own:

- content-addressed C object caching (ADR 0164);
- incremental Hexal compilation;
- additional hosts, targets and cross-compilation;
- project manifests and compiler-version selection;
- C sources, headers, objects, libraries and build-system adapters;
- static linkage; and
- a supported public driver API.

## Reference synchronization

This ADR changes no Hexal syntax. With explicit approval, update
`docs/reference.md` only for the compiler-visible `Project.Target` and qualified
target contract introduced by RFC 0052. Filesystem paths, CLI commands, backend
installation and publication behavior remain in this ADR and code-local
contracts.
