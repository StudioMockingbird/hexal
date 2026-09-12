# ADR 0166: Timestamp Toolchain Versioning

- Kind: Architecture Decision Record (ADR)
- Status: Closed; every Validation item is implemented and covered by
  pure-Go tests, CLI checks, and live injection probes
- Created: 2026-09-12
- Updated: 2026-09-12
- Coordinates with: RFC 0052 (C backend), ADR 0055 (CLI/build driver), and ADR
  0164 (future object cache)
- Does not update `docs/reference.md`: this versions the Hexal toolchain, not
  the Hexal language or generated-program ABI

## Goal

Give every Hexal toolchain build one short, visible identity without adopting
semantic versioning before Hexal has compatibility promises that semantic
versions could describe.

## Version format

Versioned builds use exactly:

```text
YYYY-MMM-DD-HH-MM
```

Example:

```text
2026-Sep-12-18-05
```

Rules:

- The timestamp is UTC.
- Year, day, hour, and minute are zero-padded as shown.
- `MMM` is the locale-independent English abbreviation `Jan` through `Dec`.
- Seconds, timezone suffixes, prefixes, build counters, commit hashes, and
  dirty markers are absent.
- At most one official version is published for one UTC minute. A duplicate is
  a release-process error, not a suffixing opportunity.
- The spelling is an opaque version identifier. Consumers must parse the month
  table before ordering versions; plain lexical order is not chronological
  across months.
- An unversioned developer build reports exactly `development`.
- The version is fixed when the executable is built. Reading the wall clock at
  runtime is forbidden: one binary never changes identity.

## Single owner

One internal package owns the value and formatting:

```go
package version

var value = "development"

func String() string
func IsDevelopment() bool
```

No CLI, driver, compiler package, test, or generated artifact carries a second
version constant. `String` returns either `development` or a value validated
against the exact timestamp grammar.

The value is immutable after process startup. The CLI validates it before
command dispatch. Invalid injected text is a configuration failure; runtime
code never silently repairs or reformats it.

## Recommended visible surfaces

### Direct query

Provide both conventional spellings:

```text
hexal version
hexal --version
```

Both print exactly one line and exit successfully without discovering a
project or Zig:

```text
hexal 2026-Sep-12-18-05
```

`hexal --version` is a top-level alias, not a flag duplicated independently by
every subcommand. `hexal build --version` is therefore not another interface.

### Help

The first line of `hexal help`, `hexal -h`, and `hexal --help` is:

```text
Hexal 2026-Sep-12-18-05
```

The remaining usage text stays stable.

### Doctor

`hexal doctor` reports the Hexal version before backend information:

```text
Hexal: 2026-Sep-12-18-05
Zig: 0.16.0
Target: x86_64-windows-gnu
```

The Hexal version remains available even when Zig is missing.

### Workbench server

The workbench starts through the main Hexal command:

```text
hexal play
```

It prints one startup line containing the toolchain version and loopback URL,
then serves until the process stops:

```text
Hexal 2026-Sep-12-18-05 workbench: http://127.0.0.1:8080
```

`play` performs no Zig discovery and produces no executable. Compilation
requests continue to call the in-memory compiler directly.

The standalone workbench executable is removed. Workbench server, route,
asset, and snippet-catalog code remain outside `cmd/hexal` in a dedicated
`workbench` package. `cmd/hexal` contains only command dispatch and invokes the
package entrypoint. The workbench package is a modular debugging component, not
a supported public embedding API.

v1 retains the existing fixed `127.0.0.1:8080` address. It does not add host or
port flags, bind a non-loopback interface, open a browser automatically, or
start in the background. Bind and server failures return through the CLI's one
error-rendering site.

### Build records

ADR 0055's internal `BuildResult` records the Hexal version once at project
level. Individual C command records do not repeat it. This makes retained build
evidence and bug reports attributable without changing command lines.

### Internal compiler failures

An `Unknown Error` caused by a compiler defect includes the Hexal version in
the surrounding CLI report. Ordinary syntax, type, and runtime diagnostics do
not include it; their exact messages remain stable and concise.

## Surfaces deliberately excluded

- Normal `hexal build` output does not print an unconditional startup banner.
  The version is available through `version`, help, doctor, and the build
  record without adding noise to every successful build.
- Ordinary user diagnostics do not contain the version.
- Generated C and headers contain no version comment, string, macro, symbol,
  or timestamp. Equivalent compiler inputs remain byte-identical across
  toolchain builds when their generated semantics are unchanged.
- A compiled Hexal program does not expose the compiler version as a language
  constant or runtime API.
- The produced program's product/file version is not the Hexal compiler
  version. User-program versioning is a separate future feature.
- The workbench web UI receives no permanent version surface; `hexal play`'s
  startup line is sufficient for the temporary debugging tool.

## Future display options

These are useful only after the corresponding distribution features exist:

- release archive and installer filenames;
- Windows executable `ProductVersion` metadata for `hexal.exe`;
- package-manager metadata;
- CI build summaries and downloadable-artifact manifests;
- crash-report bundles; and
- a persistent object-cache namespace after ADR 0164 gains a stable backend
  identity.

None justifies putting the toolchain version into generated programs.

## Settled decisions

### Build-time source

Versioned build tooling captures the current UTC minute once and injects it at
link time:

```text
go build -ldflags "-X hexal/internal/version.value=2026-Sep-12-18-05"
```

- Means exactly when this executable was built.
- Works naturally once CI/CD exists.
- Local plain `go build` and `go install` report `development`.
- Rebuilding the same commit at another time creates a different version.

Do not derive the version from VCS metadata or maintain a checked-in timestamp
constant.

### Targeted presentation

Show the version in `version`, help, doctor, the `play` startup line, retained
build records, and internal-error reports only. Normal commands print no
unconditional banner.

## Validation

This section is exhaustive for ADR 0166.

- The accepted version grammar accepts every English month and rejects wrong
  width, casing, separators, impossible calendar fields, and trailing text.
- `development` is the only non-timestamp value.
- The version is immutable for the process lifetime and independent of the
  runtime clock and timezone.
- `hexal version` and `hexal --version` print the same exact one-line result and
  invoke neither project discovery nor Zig.
- Help and doctor use the single internal owner.
- Doctor reports the Hexal version even when backend discovery fails.
- `hexal play` starts the modular workbench server on
  `http://127.0.0.1:8080`, reports the Hexal version once, and performs no
  project or Zig discovery.
- No standalone workbench `main` package or executable remains.
- Workbench routes, assets, and snippet loading remain outside `cmd/hexal`;
  command wiring does not absorb their implementation.
- A workbench bind or server failure reaches the normal top-level CLI error
  renderer.
- One project-level build record retains the version; command records do not
  duplicate it.
- Ordinary compiler diagnostics remain byte-identical.
- Generated artifacts and the snippet manifest remain byte-identical.
- A plain unversioned developer build reports `hexal development`.
- Invalid injected version text fails before command dispatch.
- Ordinary `go test ./...` remains pure Go and needs no Zig.
- `docs/reference.md` is unchanged.

## Implementation plan

### Phase 1: identity owner

1. Add `internal/version` with the development default, exact parser, and
   immutable accessors.
2. Support UTC timestamp injection through the one private link-time variable;
   plain builds retain `development`.
3. Add table-driven grammar and calendar validation tests.

### Phase 2: CLI surfaces

1. Add `hexal version` and the top-level `--version` alias before ordinary
   command dispatch.
2. Add the version to help and doctor.
3. Add `hexal play` and its versioned startup line.
4. Keep normal commands free of an unconditional version banner.
5. Preserve the existing one-site CLI error rendering.

### Phase 3: modular workbench entrypoint

1. Convert `workbench` from a standalone `main` package into an importable
   workbench package while retaining its routes, embedded assets, fixed
   loopback address, and snippet dependency there.
2. Expose only the minimum server entrypoint required by `cmd/hexal`.
3. Remove the standalone workbench executable build path.
4. Update workbench tests to exercise the package entrypoint and handlers
   without starting a second command binary.
5. Update `AGENTS.md` workbench validation to rebuild `hexal` and restart it
   through `hexal play`, not `bin/hexal-workbench.exe`.

### Phase 4: attribution

1. Add one Hexal-version field to ADR 0055's project-level build result.
2. Include it in CLI reports for internal compiler failures.
3. Keep it out of generated files, ordinary diagnostics, and child command
   arguments.

### Phase 5: conformance

1. Implement every finalized Validation item.
2. Verify pure-Go tests and CLI golden output.
3. Compile the snippet catalog and prove that no generated hash moved.
4. Confirm `docs/reference.md` needs no change.

## Required coordination

ADR 0055 admits `version` and `--version` alongside `build`, `doctor`, and
`help`, and admits `play` as the sole workbench launch command. Its
`BuildResult` gains the single project-level version field. No backend,
target-profile, compiler API, or language contract changes.
