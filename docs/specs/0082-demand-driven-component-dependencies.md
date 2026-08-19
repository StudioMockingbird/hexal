# RFC 0082: Demand-Driven Component Dependencies

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-19
- Scope: component artifact selection — a component is emitted only when
  something references it
- Depends on: nothing
- Coordinates with: `docs/status.md`, the snippet SHA-256 manifest
- Does not change: Hexal syntax, semantics, diagnostics, or the
  `compiler.Compile` contract

## Summary

A program that uses `Array` or `List` without slicing emits
`hexal/view.h` containing an include guard, one `#include`, and nothing else.
**8 of the 98 catalog snippets** produce this hollow artifact.

The cause is that component headers declare dependencies as static template
text while emitting content on demand. The declaration is worst-case, the
content is actual, and the selector believes the declaration.

## Evidence

`control-filter-continue` uses one `Array<Int32, 5>` and a `for` loop. Emitted
`hexal/view.h`, in full — 70 bytes:

```c
#ifndef HEXAL_VIEW_H
#define HEXAL_VIEW_H

#include "hexal.h"

#endif
```

`hexal/array.h` for the same program contains the struct and `at`/`at_mut`, and
mentions "view" exactly once — in its own include line.

### The template already has the demand signal

`compiler/generator/packages/array.h` guards the only View-using helper, and
declares the dependency unconditionally three lines above it:

```
#include "hexal/view.h"          ← unconditional
...
{{if .ViewCName}}                ← the demand signal, already present
static inline {{.ViewCName}} hex_array_slice_{{.Suffix}}(...)
{{end}}
```

`packages/list.h` has the identical shape at line 6.

### The selector trusts the declaration

`compiler/generator/emission.go:170-174`:

```go
if len(listState.order) > 0 || len(arrayState.order) > 0 {
    // The list and array component headers declare their View
    // dependency, so the view component must exist.
    viewState.required = true
}
```

The comment states the mechanism exactly: the component *declares* the
dependency, therefore the component *must exist*. Presence of any array or list
selects View, whether or not a slice is ever taken.

### The defect is bounded — verified, not assumed

Surveyed by probe across component shapes:

| Program | Hollow artifact |
|---|---|
| `Array`, no slice | **`hexal/view.h`, 70 bytes** |
| `List`, no slice | **`hexal/view.h`, 70 bytes** |
| `Array` with slice | none |
| `String` only | none |
| `Dict` | none |
| scalars only | none |

The `String` path is correct and must not be changed: `ensureViewUInt8` registers
a `View<UInt8>` because `String.bytes()` returns one, so the component is both
declared and populated. Only the array/list rule over-declares.

## The change

### 1. The include follows the same signal as the helper

Add a top-level `NeedsView` to the array and list component models
(`array_component.go:24`, `list_component.go:27` already carry the per-
specialization `ViewCName`). It is true when any specialization in the model has
a non-empty `ViewCName`:

```
{{if .NeedsView}}#include "hexal/view.h"{{end}}
```

### 2. The selector follows the same signal as the include

`emission.go:170-174` tests the same predicate rather than mere presence:

```go
if arraysNeedView(arrayState) || listsNeedView(listState) {
    viewState.required = true
}
```

Both helpers reduce over the same `ViewCName` field, so declaration, selection,
and emission share one source of truth instead of three.

## The rule this establishes

**A component's declared dependencies must be exactly the dependencies its
emitted content uses.**

This is RFC 0073's D2 and D33 inverted. Those under-declared — generated C
referencing a type no header declared, which does not compile. This
over-declares — a header declaring a dependency nothing uses, which compiles but
ships a dead artifact. Both come from dependency declarations being decoupled
from emitted content.

The tell in the source is the phrase "declares its X dependency", which appears
in four comments in `emission.go`'s requirement block. Three of the four are
correct — `stringUsed` genuinely populates View via `ensureViewUInt8`, `dictState`
genuinely uses String, `concurrencyState` genuinely uses Heap — and were verified
by the survey above. Only the array/list rule is wrong. Do not "fix" the others.

## Invariants

1. No emitted component artifact is hollow: every `hexal/*.h` contains at least
   one declaration beyond its include guard and includes.
2. Every C type spelled in generated output remains declared before use. This
   RFC removes artifacts; it must not remove a needed one.
3. Programs that slice an Array or List emit byte-identical C.
4. The `String` → `View<UInt8>` dependency is unchanged.
5. No new artifact is introduced and no artifact is renamed.

## Validation

- The snippet catalog compiles; the manifest moves for exactly the 8 affected
  snippets and no others: `control-search-break`, `control-filter-continue`,
  `control-nested-coordinate-scan`, `control-indexed-total`,
  `numeric-endian-round-trip`, `collections-list-builder`,
  `collections-nested-list`, `errors-cleanup-helper`.
- An `Array` without slicing emits no `hexal/view.h`.
- An `Array` with slicing emits `hexal/view.h` containing the view typedef, and
  `hexal/array.h` still includes it.
- Same pair for `List`.
- A `String`-only program still emits a populated `hexal/view.h`.
- **A catalog-wide assertion that no emitted `hexal/*.h` is hollow.** This is the
  regression guard for the class, not just the instance; without it the next
  unconditional include reintroduces the defect silently.
- `go test ./...`, `go vet ./...`.

## Sequencing with RFC 0081

**If RFC 0081 lands first, re-derive the eight-snippet list before relying on
it.** 0081 moves module-typed specializations out of the shared component
headers, which changes which programs have an empty component — a module whose
only list was `List<M.Point>` stops populating `hexal/list.h` entirely. The
defect and the fix are unaffected; only the enumeration is.

The two specs are otherwise independent, and 0081's conditional-include work is
the same predicate this RFC establishes. Landing either first is fine; landing
them together is fine. Only the manifest expectation is order-sensitive.

## Non-goals

- Demand-driven emission of individual helpers within a component. The generator
  emits helper families wholesale; that is a separate known coverage gap in
  `docs/status.md`.
- Changing component granularity, naming, or the `hexal/` artifact layout.
- Removing the include guards or the `hexal.h` include, which every component
  legitimately needs.
- Revisiting the String, Dict, or concurrency dependency rules, which the survey
  confirmed are correct.

## Drawbacks

- The artifact set becomes program-dependent in one more way, so a build driver
  must read `CompilationResult.Files` rather than assume a fixed component list.
  That is already true — components are already demand-driven — but this widens
  the variation.
- The manifest moves for 8 snippets, which is churn in a regression baseline for
  a change that removes output rather than altering it.
