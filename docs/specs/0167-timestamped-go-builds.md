# ADR 0167: Timestamped Go Build Workflows

- Kind: Architecture Decision Record (ADR)
- Status: Implemented; closure pending
- Created: 2026-09-12
- Supersedes: the plain `go install` and plain `go build` decision in ADR 0166
- Coordinates with: ADR 0055 (filesystem and build driver) and ADR 0166
- Does not update `docs/reference.md`: this versions the Hexal toolchain, not
  the Hexal language or generated-program ABI

## Decision

Every supported Hexal toolchain build and install workflow exposes one UTC
timestamp through `hexal/internal/version`.

After installation with the ordinary Go command:

```text
hexal version
hexal 2026-Sep-12-18-05
```

The timestamp format remains `YYYY-MMM-DD-HH-MM`, with an invariant English
month abbreviation. The executable reads its own artifact timestamp and never
reads the wall clock at runtime.

## Artifact timestamp

The Go command does not expose the actual build instant to a program compiled
by plain `go install` or `go build`. The version package therefore reads the
executable's modification timestamp and formats it as the visible Hexal
identity. This is the timestamp users see for the installed artifact.

If the executable path or file metadata cannot be read, the package falls back
to Go's embedded `vcs.time`; without either source it reports `development`.
The value remains fixed for the executable lifetime.

## Requirements

- Plain `go build` and `go install` expose the executable artifact timestamp.
- The version package uses invariant English month formatting.
- Generated C receives no toolchain version.
- `hexal version`, `hexal --version`, help, doctor, and workbench startup use
  the build value through the existing `internal/version` owner.
- Builds without VCS metadata retain the explicit `development` fallback.

## Validation

This section is exhaustive.

- The canonical build workflow produces a timestamp matching the required
  grammar.
- The canonical install workflow produces a timestamp matching the required
  grammar.
- `hexal version` and `hexal --version` report the same timestamp.
- Help begins with the same timestamped Hexal identity.
- The timestamp is fixed for the executable lifetime.
- The generated C and headers remain free of the toolchain timestamp.
- A failed Go build or install propagates a non-zero status.
- Raw `go install` behavior is documented as unsupported and is not used as
  evidence for canonical build conformance.

## Implementation plan

1. Read the executable artifact timestamp in the single version owner.
2. Fall back to Go's embedded `vcs.time` and then `development` when needed.
3. Run `go install ./cmd/hexal` and query the resulting executable through
   `version` and `--version`.
4. Run the existing pure-Go and CLI tests.
5. Close and archive this ADR after the ordinary Go install output is verified.
