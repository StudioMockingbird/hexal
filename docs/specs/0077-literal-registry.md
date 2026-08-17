# RFC 0077: Literal Registry

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-16
- Scope: generated String literal interning and lookup in the generator
- Depends on: nothing. Independent of RFCs 0072–0079.
- Coordinates with: RFC 0073 (D9 and D10 guard the same lookups tactically),
  `docs/reference.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, generated C, or any
  exported API

## Summary

String literal interning is an ad-hoc map with a lookup that can miss, and two
call sites paper over a miss with a fabricated value instead of failing.

Replace it with a registry whose lookup cannot silently miss: interning returns
a handle, and rendering consumes handles rather than re-deriving names from
payload strings.

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

## The change

### A handle, not a name lookup

```go
// literalHandle identifies one interned String literal. It is only obtainable
// from Intern, so a handle always denotes a registered literal.
type literalHandle struct{ index int }

type literalRegistry struct {
    payloads []string
    seen     map[string]literalHandle
    strand   bool
}

// Intern registers payload if new and returns its handle. Interning the same
// payload twice returns the same handle.
func (r *literalRegistry) Intern(payload string) literalHandle

// CName returns the generated C object name for a handle.
func (r *literalRegistry) CName(h literalHandle) string

// All returns every payload in registration order.
func (r *literalRegistry) All() []string
```

The registry is never nil — construct it with the emission state, not lazily.

### Callers hold handles

`generatedConcurrencyState` stores `literalHandle` values instead of payload
strings:

```go
fileLiteral   literalHandle   // was: string
headerLiteral literalHandle   // was: string
```

`messageLiteral` takes a handle and cannot fail, so its filename-substitution
fallback is deleted rather than converted to a diagnostic.

`literalObjectName` is deleted. Nothing looks a literal up by payload at render
time; discovery interns and hands the handle forward.

### Ordering and merge are unchanged

Registration order is the existing canonical program-wide order, and the
cross-module merge keeps first-occurrence-wins semantics. Generated literal
object names and their emission order must be byte-identical.

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
5. A handle cannot be constructed outside `Intern`, so a rendered literal is
   always a registered one.

## Validation

- `go test ./...`, `go vet ./...`.
- The snippet catalog compiles and the manifest is **unchanged**.
- `grep -n 'literalObjectName' compiler/` returns nothing.
- A test asserts interning the same payload twice yields the same handle and one
  table entry.
- A test asserts a multi-module program with literals shared across modules
  produces one definition per distinct payload, in the existing order.
- Existing String, concurrency, and error-message tests pass unmodified.

## Non-goals

- Changing literal storage, linkage, or C naming.
- Deduplicating literals more aggressively, or interning Strand separately.
- Extending the registry to types, symbols, or diagnostics — this RFC covers
  String literals only.
- Parser expression-start classification. That was raised alongside this item as
  the same class of scattered classification, and it is a genuine finding, but it
  is in the parser and shares no code with the literal table. It stays with RFC
  0074's naming and structure work.

## Drawbacks

- A handle type is more ceremony than a string at the ~6 call sites involved.
  The gain is that the ceremony is what makes the miss impossible.
- Two states change field types (`fileLiteral`, `headerLiteral`), so concurrency
  emission is touched by a change that is otherwise about strings.

## Expected result

- No lookup in the generator can return a fabricated literal name.
- The two silent fallbacks are deleted rather than guarded.
- Generated output is identical.
