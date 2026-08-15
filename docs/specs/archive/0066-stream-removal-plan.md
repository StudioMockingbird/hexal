# Execution Plan 0066: Stream Removal

- Kind: Execution Plan
- Status: Implemented; conformance verified 2026-08-15
- Created: 2026-08-15
- Implements: RFC 0064 Item 1 (approved direction), closes RFC 0051 without
  implementation
- Depends on: RFC 0031 (Stream type), RFC 0062 (EoS representation)

## Summary

Remove the built-in `Stream<T>` concept end to end: the type constructor, its
nine operations, Stream-specific `for` iteration, eligibility and closure
rules, deferred-free capture, generated type families, expression kinds,
validation, tests, and snippets. `EoS` remains because Channel receive uses
it. RFC 0051 closes without implementation.

## Removed surface

- `Stream<T>`, `Stream<T>.new()`, `Stream<T>.produce(heap, state, callback)`,
  `Stream<T>.next()`, `Stream<T>.filter(heap, predicate)`,
  `Stream<T>.map<U>(heap, mapper)`, `Stream<T>.take(heap, count)`,
  `Stream<T>.free(heap)`, `List<T>.stream(heap)`.

## Implementation steps

1. Remove the Stream expression kinds and type identity.
2. Remove checker dispatch, method tables, eligibility, closure, and
   `for`-iteration support.
3. Remove generator discovery, rendering, validation, deferred capture, walk
   cases, and the Stream requirement families from `hexal.h` (the EoS
   requirement for Stream step unions goes with it; Channel receive keeps
   EoS).
4. Delete Stream tests, the dormant c23 canary, and workbench snippet
   category `09-streams.json`.
5. Update `docs/reference.md` and the workbench syntax highlighter.
6. Regenerate the snippet manifest, run full validation, rebuild the
   workbench.

## Validation

- `go test ./...`, `go vet ./...`, `go test/vet -tags c23 ./...`.
- No `Stream` spelling remains outside immutable archived specs.
- Regenerated manifest committed as a deliberate change.

## Coordination

Stream and File removal are executed in parallel by separate agents on
disjoint file sets. Shared compiler plumbing (dispatch, walk, validation,
types, render, defer, for, index.html) is removed once for both features by
the coordinating session before the agents run.
