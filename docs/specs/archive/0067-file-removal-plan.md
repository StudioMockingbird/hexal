# Execution Plan 0067: File Builtin Removal

- Kind: Execution Plan
- Status: Implemented; conformance verified 2026-08-15
- Created: 2026-08-15
- Implements: RFC 0064 Item 2 (approved direction)
- Depends on: RFC 0040 (File API), RFC 0062 (demand-driven `hexal.h`)

## Summary

Remove the compiler-owned `File`, `FileMode`, and `Stdio` builtins end to end:
types, checked-expression kinds, method dispatch, runtime helpers, tests, and
snippets. `print` remains the minimum built-in output facility. Library-level
File APIs return later through C interop; the unbounded gap is accepted by
RFC 0064.

## Removed surface

- `File`, `FileMode`, `Stdio`, `File.open`, `File.read_bytes`,
  `File.read_text`, `File.write`, `File.write_text`, `File.flush`,
  `File.close`.

## Implementation steps

1. Remove the File/FileMode/Stdio type identities and expression kinds.
2. Remove checker dispatch, method tables, and validation.
3. Separate the retained output-gate behavior from removed File machinery in
   the generator; remove File rendering, deferred-close capture, walk cases,
   and the File requirement families from `hexal.h`; keep only what `print`
   needs.
4. Delete File tests, the dormant c23 canary, and File/Stdio snippet content.
5. Update `docs/reference.md` and the workbench syntax highlighter.
6. Regenerate the snippet manifest, run full validation, rebuild the
   workbench.

## Validation

- `go test ./...`, `go vet ./...`, `go test/vet -tags c23 ./...`.
- No `File`, `FileMode`, `Stdio`, or `hex_io_gate` spelling remains outside
  immutable archived specs, except a retained gate that `print` genuinely
  uses.
- Regenerated manifest committed as a deliberate change.

## Coordination

Stream and File removal are executed in parallel by separate agents on
disjoint file sets. Shared compiler plumbing (dispatch, walk, validation,
types, render, defer, index.html) is removed once for both features by the
coordinating session before the agents run.
