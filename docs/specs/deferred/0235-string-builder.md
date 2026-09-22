# RFC 0235: StringBuilder

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation ready. The surface is settled and every surrounding
  fact it depends on was probe-verified against the tree on 2026-09-22; no
  upstream inputs, no open design questions
- Created: 2026-09-22
- Updated: 2026-09-22
- Origin: large text/HTML template assembly. Immutable-string concat in a
  loop is quadratic, and the language has no append primitive to build large
  documents incrementally
- Depends on: nothing new — the current String, Slice, List, Heap, view, and
  shallow-aliasing contracts in `docs/reference.md`
- Coordinates with: the future compiled-template rendering and streaming
  sinks, which will target this builder as their output; RFC 0143's
  interpolation lowering already provides the one-shot half of the same need
- Updates `docs/reference.md`: yes — one new `StringBuilder` section beside
  `String` and `List`, and the equality rule's non-comparable list gains
  builders. The grammar is untouched: no new syntax, only a builtin type with
  ordinary method calls
- Swept code: none. No defense in the tree exists because an append
  primitive was absent

## Decision summary

Hexal gains a builtin, heap-owning, append-only byte buffer named
`StringBuilder`, declared beside `List` and `Dict` as a fundamental type
rather than placed in a std module. It accumulates bytes with amortized O(1)
`push`, exposes its content as an O(1) read-only `Slice<Byte>`, and hands
off to text and stream operations that already exist: `String.from_bytes`
validates and materializes one heap `String`, and every `write(from:
Slice<Byte>)` sink streams the buffer without materializing anything.

The type follows the `List` template exactly: a canonical constructor
capturing the Heap at creation, mutation without a heap argument, `free(heap)`
releasing only the buffer the type owns, handle-copy aliasing with
exactly-one-release, and no equality, ordering, or printing.

```text
StringBuilder(heap: Heap) -> StringBuilder
StringBuilder.length() -> Size
StringBuilder.push(from: Slice<Byte>) -> no value
StringBuilder.bytes() -> Slice<Byte>             O(1), byte bounds
StringBuilder.free(heap: Heap) -> no value
```

There is no `build()` method: `String.from_bytes(heap, b.bytes())` already
validates the whole buffer and copies it once, and one obvious way beats a
second spelling of the same operation.

## Why now: the status quo cost model

Probe-verified against `compiler.Compile` on 2026-09-22:

- **One-shot templates are already optimal.** `String.interpolate` is lowered
  at compile time: each segment's length is checked-summed, the result is
  allocated **once**, and every segment is `memcpy`'d in source order
  (`compiler/generator/interpolation.go`, `hoistStringInterpolate`). One
  render of a large literal template is one allocation and one pass — not a
  chain of concats. For a whole-page render from a literal, the status quo is
  the right tool and stays that way.
- **Multi-line templates compile today, by composition.** An interpreted
  literal may not contain a raw newline (`String literal cannot contain a raw
  newline; use \n`), and a raw string may span lines but never interpolates.
  Both routes work: `\n` escapes inside an interpreted template, or a
  multi-line raw static part pushed through an interpolate hole
  (`String.interpolate(h, "{{ static_part }}<span>{{ 1 }}</span>")` with
  `static_part` a multi-line raw literal) — the latter probe-verified as
  compiling.
- **Loop assembly is the quadratic path.** `concat` returns a fresh String,
  so appending row n to an assembly of total size m allocates m+n bytes and
  copies both; k rows cost O(k²) copying. The program compiles — probe-verified —
  which is exactly the problem: nothing stops it and nothing better exists.
- **The gap is incremental assembly**, not interpolation. That gap is filled
  by an append buffer, which is also the output target future template
  rendering (render a compiled template repeatedly into a builder) and
  future streaming (`write(builder.bytes())` straight to a socket or file)
  will need. This spec lays that fundamental; those two come later.

## Language surface

```text
StringBuilder(heap: Heap) -> StringBuilder
StringBuilder.length() -> Size
StringBuilder.push(from: Slice<Byte>) -> no value
StringBuilder.bytes() -> Slice<Byte>
StringBuilder.free(heap: Heap) -> no value
```

- **Construction and growth.** `StringBuilder(heap)` mirrors
  `List<T>(heap)`: the Heap argument is the capability token evaluated at
  creation, and growth during `push` uses the default Heap exactly as
  `List.push` does — no heap argument on mutation. The buffer grows
  geometrically, so a push is amortized O(1) and assembling n bytes is O(n)
  total; the growth factor and initial capacity are implementation facts,
  not contract. Size-overflow during growth traps with the existing
  `string allocation size overflow` message; allocation failure traps with
  the standard heap-failure trap. Neither is a new trap literal.
- **`push(from: Slice<Byte>)` copies the bytes immediately.** The slice is
  consumed, not retained: after `push`, the caller may free or overwrite the
  source with no effect on the buffer. This is the `write`/`concat` shape —
  one parameter type for every byte source — and every text form reaches it
  in O(1) through `.bytes()`: a literal (`b.push("<tr>".bytes())`), a heap
  `String`, or a `String<N>`. Probe-verified: `.bytes()` compiles on all
  three forms.
- **`bytes()` is an O(1) read-only view** of the accumulated content, empty
  before the first push. It is a view under the existing view contract: it
  dangles after a subsequent `push` that reallocates, after `free`, or when
  the builder otherwise leaves scope. Consuming it immediately —
  `from_bytes`, `write` — is the pattern; holding it across a push is not.
- **No UTF-8 validation at `push`.** A scalar's bytes may legitimately split
  across two pushes (`push` the two bytes of `é` separately), so validation
  can only run over the whole buffer, where the existing `String.from_bytes`
  already performs it. Arbitrary bytes may sit in the buffer indefinitely;
  only conversion to `String` requires them valid, and its Error — kind,
  message, everything — is `from_bytes`'s existing one, unchanged. The
  builder never produces an Error itself.
- **`length()` counts bytes**, consistently with `String.length()` on every
  form; it is not a rune count.
- **`free(heap)` releases only the growth buffer** — the container-storage
  rule `List.free` already states. It frees no referent, because the builder
  retains nothing it was pushed: every payload was copied in. Handle copies
  alias one buffer; each distinct builder allocation is released exactly
  once, aliases must not be freed per copy, and double free is not diagnosed
  — the standard shallow rules, unchanged.
- **Storability** follows the general rule for pointer-indirect owning
  handles, as `String` and `List` do: valid as a Dict value, List element,
  function parameter/result, ADT payload, and Task argument. Not
  Dict-key-eligible — the existing key rule rejects it with its existing
  message, needing no builder-specific arm.

### Value consequences

`StringBuilder` is a work handle, not a value. Three rejections are part of
this contract, each an explicit checker/generator arm with a pinned message
mirroring the `Heap` diagnostics probe-verified on 2026-09-22:

| Operation | Diagnostic |
| --- | --- |
| `a == b` / `a != b` | `builder handles are not equality-comparable` |
| `a < b` (any ordering) | `ordering is unavailable for StringBuilder values` |
| `print(b)` | `print does not support StringBuilder` |

Compare or print what you build: `String.from_bytes(h, b.bytes())` produces
an ordinary `String` with the ordinary `==`, ordering, and `print`. (The
`docs/reference.md` equality rule — "Functions, allocators, and Dicts have
no equality" — gains builders in that list.)

## Rejected choices

- **`String.Builder` as a nested spelling.** Hexal has no nested type
  declarations; types are module-scoped or builtin. The builtin placement
  also matches `List`/`Dict`/`Heap` and needs no grammar work.
- **A `build()` convenience method.** Redundant with
  `String.from_bytes(heap, b.bytes())`, which already validates once and
  copies once. A second spelling of one operation violates one-obvious-way
  and can be added later without breakage if ergonomics demand it.
- **`push` overloads for text forms.** `Slice<Byte>` is the single parameter
  type every byte sink in the language already uses; `.bytes()` is O(1) and
  universal. Variadic or typed push variants multiply the surface for no
  capability gain.
- **`reserve`/`grow`.** Geometric growth already gives O(n) assembly;
  a pre-sizing method is an optimization to add only with a measurement
  that asks for it.
- **`clear()`.** `free(heap)` plus a fresh constructor is one obvious
  equivalent; capacity loss on reset is not worth a method.
- **A fixed-capacity inline variant.** `String<N>` plus
  `String<N>.interpolate` already cover bounded inline text; the builder's
  reason to exist is unbounded growth.
- **Equality, ordering, printing on the handle.** Pinned to rejections
  above; the built `String` carries all three.
- **Prepend, insert, remove.** Append-only is the whole contract;
  prepend composes as "other builder first, then this one".
- **A formatting/interpolation sink on the builder.** Render-a-template-into-
  a-builder is the future template spec's decision, not a byte-buffer's;
  this spec stays at bytes.
- **Streaming `write-through` as a builder mode.** `bytes()` already lets
  every existing stream sink consume the buffer; a dedicated sink parameter
  belongs to the streaming spec when it lands.

## Implementation plan

### Phase 1 — surface and diagnostics

1. Declare `StringBuilder` as a canonical builtin type in
   `compiler/types` with the five members and their exact signatures.
2. Register the constructor and methods in the checker's validation and
   method dispatch, alongside `List`/`Dict`; growth reuses the string
   component's existing allocation and overflow paths.
3. Emit the buffer struct and its operations in the string component
   (`compiler/generator/packages/string.c`/`.h`), demand-driven like every
   other component feature.
4. Add the three explicit rejection arms — equality, ordering, print —
   carrying the pinned messages, so no fail-closed Unknown Error can arise
   from the new type reaching them.

*Verify:* pure-Go integration tests for the compile-time surface below, plus
generated-C text assertions — builder declarations present exactly once in
the artifacts of a program that uses them, public headers gaining no new
`#include`, and the dependency list of a builder-only program identical to
the same program without the builder.

### Phase 2 — runtime behavior and catalog

1. Tagged C23 fixtures for the behavioral Validation rows: growth across
   capacity boundaries, copy-in-at-push, split multi-byte acceptance,
   invalid-buffer rejection reusing `from_bytes`'s Error, empty builder,
   free.
2. Add one catalog snippet assembling a small document from literal chunks;
   rebuild the snippet manifest and review the diff — only the new
   snippet's entries appear, every pre-existing hash unchanged.

### Phase 3 — reference synchronization and full gate

1. `docs/reference.md`: the new `StringBuilder` section (signatures,
   contracts, the three rejections, the view-dangling rule), the
   equality-rule list gaining builders, and verification that the EBNF and
   every other section are untouched — this change adds no syntax.
2. Full gate: `go test ./...`, `go vet ./...`, `go vet -tags c23`, the
   tagged C23 suite, the snippet-manifest review — and per the workbench
   rule, rebuild the `hexal` binary and restart the running workbench
   through `hexal play` before handoff.

## Validation

This section is exhaustive.

Compile-time surface (pure Go, `compiler/tests/integration`):

- The constructor, all five members, and their exact signatures typecheck;
  `push` accepts `literal.bytes()`, heap-`String.bytes()`, and
  `String<N>.bytes()` (probe-verified baseline, 2026-09-22).
- `push` with a non-slice argument (bare `String`, `Int32`) fails ordinary
  parameter type checking; no builder-specific acceptance exists.
- `a == b` rejects with exactly `builder handles are not
  equality-comparable`; `a < b` with exactly `ordering is unavailable for
  StringBuilder values`; `print(b)` with exactly `print does not support
  StringBuilder`.
- Using a builder as a Dict key fails with the existing key-type
  diagnostic — no builder-specific arm is added there.
- A builder is accepted as a Dict value, List element, function result,
  ADT payload, and Task argument.
- Generated-C text assertions: builder declarations emitted once, no new
  include in public headers, no new dependency fact for a builder-only
  program.

Runtime behavior (tagged C23 fixtures against each qualified pack):

- Growth correctness: pushing chunks whose total exceeds the initial
  capacity (several thousand pushes across reallocations) leaves `bytes()`
  equal to the ordered concatenation and `length()` equal to the summed byte
  lengths.
- Copy-in: push `source.bytes()`, then `source.free(heap)`; `from_bytes`
  over the builder still yields the original content.
- Split sequence: the two bytes of `é` pushed in separate `push` calls
  then `from_bytes` succeed and equal `"é"`; validation ran once, whole.
- Invalid buffer: a buffer holding a byte that cannot appear in UTF-8
  fails `from_bytes` with exactly the kind and message `String.from_bytes`
  produces over the same bytes — no new Error, kind, or message enters the
  language.
- Empty builder: `length()` 0, `bytes()` an empty slice, `from_bytes`
  yields the empty String, `free` is safe.
- `free` releases only the growth buffer; every payload pushed was already
  copied in and is never double-released (release claim by inspection — no
  leak checker runs in this repository's gate).
- Growth overflow and allocation failure trap with the two existing
  literals named above, each classified in the trap inventory as its
  existing disposition — no new trap literal appears.

Demand, pack, and conformance:

- A builder-only program's dependency list is byte-identical to the same
  program without the builder: no runtime dependency, pack entry, manifest
  change, doctor probe, or dependency-demand predicate is introduced by
  this spec.
- The snippet manifest rebuild moves only the new snippet's entries; every
  pre-existing hash is unchanged, and the artifact diff shows movement only
  in the string component the builder shares.
- The ordinary Go suite passes with no C toolchain installed; the runtime
  rows run under the tagged C23 suite.

## Open implementation inputs

None design-level. The growth factor, reallocation strategy, and initial
capacity are implementation facts deliberately left unconstrained: the
contract promises amortized O(1) push and byte-exact results, not a
particular geometric series. The invalid-buffer Error is not an input — it
is `from_bytes`'s existing one by reuse.

## Implementation readiness

**Ready.** The surface is five members copied from the `List` template, the
one-shot and multi-line template paths were probe-verified to work today
(only loop assembly is quadratic, which is precisely what this closes), the
three rejection messages are pinned from probe-verified `Heap` diagnostics,
and no upstream, pack, or dependency work exists at all. Start at Phase 1.
