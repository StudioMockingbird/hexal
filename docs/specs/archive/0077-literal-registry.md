# RFC 0077: Literal Registry

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-18. `literalRegistry` carries exactly
  the specified API — `Intern`, `CName`, `Lookup`, `All` — over an opaque
  `literalHandle`, and one registry is created with the emission state and
  interned into directly in dependency order, so no per-module table exists and
  nothing needs rebasing. `literalObjectName` and `rebaseLiteralNames` are gone
  (`grep` returns nothing); `messageLiteral` takes a handle and its
  filename-substitution fallback is deleted rather than guarded;
  `generatedConcurrencyState` stores `fileLiteral`, `headerLiteral`, and the
  four failure-message handles, all minted at discovery. Generated C is
  unchanged and the snippet manifest never moved for this work. Coverage:
  `TestLiteralRegistryInternsPayloadOnce` and
  `TestLiteralRegistryPreservesRegistrationOrder` in
  `compiler/generator/strings_test.go`, plus `TestModuleGenerationLiteralOrder`
  and `TestModuleGenerationConcurrencyLiteralHandles` — the latter being the
  specific test this RFC demanded for the single-registry decision: concurrency
  in a non-entry module, behind a literal that shifts every index.

  RFC 0073's D9 and D10 are resolved by deletion rather than by guards: the two
  functions those guards would have protected no longer exist.
- Created: 2026-08-16
- Updated: 2026-08-18
- Scope: generated String literal interning and lookup in the generator
- Depends on: nothing. Independent of RFCs 0072–0079.
- Coordinates with: RFC 0073 (D9 and D10 guard the same lookups tactically),
  `docs/reference.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, generated C, or any
  exported API

## Summary

String literal interning is an ad-hoc map with a lookup that can miss, and two
call sites paper over a miss with a fabricated value instead of failing.

Replace it with a registry that **cannot fabricate**: every lookup either
returns a registered literal or fails closed, and state that holds a reference
to a literal holds a handle rather than a payload string.

An earlier revision claimed "nothing looks a literal up by payload at render
time." That is not achievable and the claim is withdrawn — see Payload lookup
survives, by necessity. The defect being fixed is fabrication, not lookup.

**This RFC is independent of every other open spec.** It can land before, after,
or instead of RFC 0073's tactical guards for the same sites.

## Evidence

The literal table is a slice plus a payload map:

```go
// compiler/generator/strings.go
type generatedStringState struct {
    used       bool
    needStrand bool
    literals   []string
    seen       map[string]int // payload -> literal index + 1
}
```

Lookup re-derives a C name from a payload string and returns an empty string on
a miss:

```go
// compiler/generator/emission.go
func literalObjectName(stringState *generatedStringState, payload string) string {
    if index, ok := stringState.seen[payload]; ok {
        return stringLiteralCName(index - 1)
    }
    return ""              // ← emits `&,` into generated C
}
```

The concurrency state holds two literal payloads as bare strings
(`concurrency.go:54-55`, registered at `:183-184`) and its `messageLiteral`
(`:261`) substitutes the **source-filename literal** when a failure message is
not found — a wrong string rather than a diagnostic.

Both misses are unreachable today: discovery registers every payload before
rendering. That is a property of the current call graph, not of the data
structure, and nothing enforces it.

### Every consumer of `seen`

The two fabricating sites above are the defect. They are not the only consumers,
and an implementer must change all of these:

| Site | Kind | Behaviour on miss |
|---|---|---|
| `emission.go:637` `literalObjectName` (2 callers at `:628-629`) | lookup | **fabricates `""`** — D9 |
| `concurrency.go:261` `messageLiteral` (3 callers) | lookup | **fabricates the filename literal** — D10 |
| `render.go:963` `StringLiteralExpression` | lookup | fails closed, correct |
| `concurrency.go:635` `errorMessageLiteral` (4 callers: `:624`, `:679`, `:706`, `:743`) | lookup | fails closed, correct |
| `strings.go:40-42`, `concurrency.go:208-213` | interners | — |
| `emission.go:544-547` `mergeProgramEmission` | merge | — |

Eleven sites, nine of them lookups. `render.go:963` is the **primary** path —
every string literal in every program renders through it.

### Payload lookup survives, by necessity

`render.go:963` receives a `checker.Expression` whose `StringLiteralExpression`
node carries the payload in `node.Name` and nothing else. There is no handle on
the checked node to hand forward. Eliminating the payload lookup there would
require propagating handles into the checked AST — a checker change, well
outside this RFC, and one that would make the checker aware of a generator
concern.

So the registry keeps a payload lookup. The rule it enforces is narrower and
still worth having:

> A lookup returns a registered literal or an error. No lookup returns a value
> that was never registered.

`render.go:963` and `errorMessageLiteral` already satisfy that — they fail
closed today and keep their current shape, changing only to the registry's
method. The two fabricating sites do not, and they are what this RFC removes.

## The change

### A handle, not a name lookup

```go
// literalHandle identifies one interned String literal. It is obtainable only
// from Intern or a successful Lookup, so a handle always denotes a registered
// literal.
type literalHandle struct{ index int }

type literalRegistry struct {
    payloads []string
    seen     map[string]literalHandle
    used     bool // carried over from generatedStringState: String machinery needed
    strand   bool
}

// Intern registers payload if new and returns its handle. Interning the same
// payload twice returns the same handle.
func (r *literalRegistry) Intern(payload string) literalHandle

// CName returns the generated C object name for a handle. A handle is only
// obtainable from Intern or Lookup, so CName cannot fail.
func (r *literalRegistry) CName(h literalHandle) string

// Lookup resolves an already-registered payload. It never registers, and the
// false result is the only miss signal — there is no fabricated return.
// Required by the render path, whose checked node carries a payload and no
// handle.
func (r *literalRegistry) Lookup(payload string) (literalHandle, bool)

// All returns every payload in registration order.
func (r *literalRegistry) All() []string
```

The registry is never nil — construct it with the emission state, not lazily.
That removes `errorMessageLiteral`'s nil guard (`concurrency.go:636-638`) and
`render.go:960-962`'s, which exist only because the table is lazily created.

### Callers hold handles

`generatedConcurrencyState` stores `literalHandle` values instead of payload
strings:

```go
fileLiteral   literalHandle   // was: string
headerLiteral literalHandle   // was: string
```

The five failure-message payloads (`channelCreationFailed`, `channelSendFailed`,
`mutexCreationFailed`, and their siblings) are package-level constants interned
during concurrency discovery. Their handles are stored on
`generatedConcurrencyState` alongside `fileLiteral`, so `messageLiteral` takes a
handle and cannot fail — its filename-substitution fallback is deleted rather
than converted to a diagnostic. Its three call sites
(`concurrency.go:417`, `:427`, `:461`) pass the stored handle instead of the
payload constant.

### `rebaseLiteralNames` disappears

This is the change's real payoff and the clearest evidence that a single
registry is the right shape.

`literalObjectName`'s only two callers are both inside `rebaseLiteralNames`
(`emission.go:625-632`, called once at `:304`). That function exists **solely**
to re-derive program-wide literal names from per-module ones — its own comment
says so:

> discovery registered the payloads into each module's own table, but every
> emitted reference must name the aggregated index

That is exactly the per-module-index hazard, already present, already worked
around by a payload round-trip through a lookup that can miss. With one
program-wide registry the handles are program-wide when minted, nothing needs
rebasing, and both `rebaseLiteralNames` and `literalObjectName` are deleted
outright.

An earlier revision of this RFC claimed those two callers "have a handle
available." They do not — they hold payload strings, which is why the round-trip
exists. The correction does not change the fix; it strengthens the case for it.

### One program-wide registry, not per-module registries merged

**This is the decision that makes handles safe, and it must be made
explicitly.** A handle is an index. Today, per-module discovery interns into a
per-module table and `mergeProgramEmission` (`emission.go:544-547`) folds those
into the program-wide table with first-occurrence-wins. If handles were minted
against per-module tables, a handle stored in `generatedConcurrencyState` would
carry a **per-module index** and `CName` would emit the wrong
`hex_literal_N` after the merge — silently, and only for multi-module programs
with concurrency.

Therefore: **create one registry with the emission state and intern into it
directly, in dependency order.** Per-module registries do not exist, so there
are no handles to remap. `mergeProgramEmission`'s literal half becomes nothing —
the interning already happened in the right order.

The rejected alternative is keeping per-module registries and remapping handles
at merge. It preserves the current shape but reintroduces exactly the
index-arithmetic class the handle is meant to remove.

**Byte-identity rests on one unverified premise, and step 1 of the
implementation is to check it.** The current program-wide order is: per-module
discovery in module order, then `mergeProgramEmission` folding each module's
`literals` slice in that same module order, first-occurrence-wins. A single
registry interned during per-module discovery reproduces that order **if and
only if** modules are discovered in the same sequence the merge folds them.

That is almost certainly true — both walk the module list — but it is the whole
of invariant 1 and must not be assumed. Before changing anything, add a test
asserting the current program-wide `literals` slice for a multi-module program,
then confirm the single-registry order matches it exactly. If the orders differ,
the fix is to intern in the merge's order, not to accept a manifest change.

## Relationship to RFC 0073

RFC 0073's D9 and D10 add fail-closed guards to `literalObjectName` and
`messageLiteral`. This RFC removes the functions those guards would protect.

**The two are independent and both are correct to do.** If 0073 lands first, this
RFC deletes its guards along with their call sites and says so in the commit. If
this RFC lands first, D9 and D10 become no-ops and 0073 should record them as
resolved rather than re-adding guards to functions that no longer exist.

Neither blocks the other. Do not sequence them.

## Invariants

1. Generated C is byte-identical. Literal object names, their contents, their
   order, and the emission order of the literal table do not change.
2. The snippet SHA-256 manifest does not move.
3. Cross-module literal merge semantics are unchanged: first occurrence wins,
   program-wide index is authoritative.
4. No exported API changes; the registry is unexported.
5. A handle cannot be constructed outside `Intern` or a successful `Lookup`, so
   a rendered literal is always a registered one. The zero-value handle is not a
   valid literal reference; construct `generatedConcurrencyState` handles at
   discovery, never leave them zero.

## Validation

- `go test ./...`, `go vet ./...`.
- The snippet catalog compiles and the manifest is **unchanged**.
- `grep -rn 'literalObjectName\|rebaseLiteralNames' compiler/` returns nothing.
- A test pins the current program-wide literal order for a multi-module program
  **before** the change, and passes unchanged after it. This is the order premise
  above; write it first.
- A test asserts interning the same payload twice yields the same handle and one
  table entry.
- A test asserts a multi-module program with literals shared across modules
  produces one definition per distinct payload, in the existing order.
- **A multi-module program with concurrency in a non-entry module** asserts that
  `fileLiteral`, `headerLiteral`, and every failure message resolve to the
  correct `hex_literal_N`. This is the specific test for the single-registry
  decision: a per-module-index handle surviving the merge is wrong *only* in
  this shape, and no existing test covers it.
- Existing String, concurrency, and error-message tests pass unmodified.

## Non-goals

- Changing literal storage, linkage, or C naming.
- Deduplicating literals more aggressively, or interning Strand separately.
- Extending the registry to types, symbols, or diagnostics — this RFC covers
  String literals only.
- Parser expression-start classification. Raised alongside this item as the same
  class of scattered classification. It is a genuine finding, but it is in the
  parser and shares no code with the literal table. **It is not an RFC 0074 item
  — an earlier revision said it was, and no such item exists there.** It is
  recorded under "Unowned" in `docs/status.md` so it is neither implied to be
  homed nor lost.

## Drawbacks

- Eleven sites touch the literal table, nine of them lookups. This is a wider
  diff than "swap a map for a struct" suggests, and it reaches into concurrency
  emission and the module merge, not just strings.
- Moving interning to a single program-wide registry changes **when** literals
  are registered relative to per-module discovery. That is the one place a
  byte-identity regression can hide, which is why the order test comes first.
- A handle type is more ceremony than a string. The gain is that the ceremony is
  what makes fabrication inexpressible at the two sites that fabricate today.
- Two states change field types (`fileLiteral`, `headerLiteral`), so concurrency
  emission is touched by a change that is otherwise about strings.

## Expected result

- No lookup in the generator can return a fabricated literal name.
- The two silent fallbacks are deleted rather than guarded.
- `rebaseLiteralNames` and `literalObjectName` are gone: with program-wide
  handles there is no per-module index to rebase.
- Generated output is identical.
