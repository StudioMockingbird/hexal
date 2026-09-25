# RFC 0235: HTML-safe template rendering and text assembly

- Kind: Feature Specification (Rust-Style RFC)
- Status: **Deferred; design not settled.** The original `StringBuilder`
  proposal below is retained for its review evidence, but it is not an
  approved implementation plan. The deferred direction in the next section
  supersedes its decision summary, implementation plan, and readiness claim.
- Created: 2026-09-22
- Updated: 2026-09-25
- Origin: cheap text and HTML template assembly without repeatedly copying an
  immutable String, while keeping untrusted content from becoming browser
  markup or script.
- Current reference: `docs/reference.md` defines the existing
  `String.interpolate`, `List<Byte>`, `Slice<Byte>`, and `String.from_bytes`
  contracts. This deferred RFC does not change them.

## Deferred direction

- **Solve HTML safety first.** Current `String.interpolate` inserts formatted
  values as raw text. In an HTML response, untrusted values can therefore
  become markup or script. A future HTML rendering facility must distinguish
  trusted template markup from inserted data, escape supported holes for their
  HTML context, and reject contexts it cannot handle safely. Ordinary
  `String.interpolate` retains its general-purpose text meaning.
- **Do not add template syntax.** Reuse the existing interpreted-literal
  `{{ expression }}` form. A future API may give it an explicitly HTML-aware
  destination, but this RFC does not choose that API or add another template
  grammar. The compiler must know the context of each hole; a single generic
  HTML escape applied to every hole is insufficient for attributes, URLs,
  scripts, and styles.
- **Keep the cheap path.** One `String.interpolate` call already measures its
  segments, allocates one result, and copies each segment once. An HTML-aware
  equivalent should measure escaped lengths and write directly to the final
  output, without allocating an escaped String for every hole. For repeated
  rows, use a linear-time append destination rather than concatenating an
  ever-growing immutable String. `List<Byte>` already has geometric growth;
  evaluate a generic bulk append operation before introducing another owning
  buffer type.
- **Keep rich user-authored HTML separate.** Escaping makes user text display
  literally. Preserving selected user markup requires HTML parsing and a
  maintained sanitization policy; it is not implied by safe interpolation.
  A C HTML parser such as Lexbor may help if that use case becomes concrete,
  but parsing alone does not make HTML safe.

Before implementation, settle the HTML contexts supported in the first
version, the API and append destination, URL-bearing attributes, and whether
trusted HTML fragments are permitted at all. Specify exact diagnostics and
end-to-end security and performance validation. No `StringBuilder` builtin or
new template syntax is approved by this RFC.

## Superseded original proposal and review

The material below preserves the initial buffer proposal and the audit that
found its design and correctness gaps. Its language surface, implementation
plan, validation, and "Ready" statement are historical, not instructions to
implement them.

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

---

# Review findings (2026-09-22)

Two reviews, merged. Claims marked **probed** were run against
`compiler.Compile` or read from the tree; the rest are reasoning from the
spec's own text. Nothing above this line was edited.

## A. The blocking question: must this be a new type?

**`List<Byte>` is already most of `StringBuilder`.** Probed — this compiles
today:

```hexal
let h: Heap = Heap()
let mut b: List<Byte> = List<Byte>(h)
b.push(b'<')
b.push(b't')
let view: Slice<Byte> = b.slice(0, b.length())
let s: String | Error = String.from_bytes(h, view)
b.free(h)
```

Mapping the proposed surface onto what exists:

| RFC 0235 proposes | `List<Byte>` today |
| --- | --- |
| `StringBuilder(heap)` | `List<Byte>(heap)` |
| `.length() -> Size` | `.length() -> Size` |
| `.bytes() -> Slice<Byte>` | `.slice(0, len) -> Slice<Byte>` |
| `.free(heap)` | `.free(heap)` |
| `.push(from: Slice<Byte>)` | `.push(value: Byte)` — **one element only** |

`List` already grows geometrically (`packages/list.h`: `ckd_mul(&next,
list->capacity, 2)`), which is the amortized-growth contract this RFC presents
as its contribution.

**The entire gap is bulk append.** Probed: `push_all` and `extend` are both
rejected with `List<UInt8> has no method push_all`.

So the proportionate change may be one generic method —
`List<T>.push_all(values: Slice<T>)` — which closes the same gap, serves every
element type rather than bytes alone, and adds no type, constructor,
diagnostics, reference section, or rejection arms.

This is also what the neighbours do. **Zig has no StringBuilder**:
`std.ArrayList(u8)` with `appendSlice` is the string builder, deliberately,
because a byte buffer is a container of bytes rather than a distinct concept.
Odin's `strings.Builder` exists but is a *library* type over `[dynamic]u8`,
not a builtin. Adopting this RFC would make Hexal the only one of the three
with a compiler-owned fundamental type for the job.

Against the language goals: goal 2 (one obvious way) gains a second way, goal
3 (small, clean surface) gains a builtin for a method's worth of capability,
and the Simplify rule's second question — *"Does this already exist in the
codebase? Reuse it; do not rewrite it."* — is never asked in Rejected choices,
which considers `build()`, `reserve`, `clear`, overloads, and inline variants
but never `List<Byte>`.

**This must be answered before anything else in the spec matters.**

## B. Correctness gaps that would produce wrong generated C

These are the findings that make the spec unsafe to implement as written, not
merely incomplete.

1. **Self-append is permitted by the signature and undefined by the spec.**
   `b.push(b.bytes())` typechecks under `push(from: Slice<Byte>)`. If the push
   triggers growth, the source view is freed mid-operation; even without
   growth, a naive `memcpy` of overlapping regions is undefined in C. Decide
   whether self-append is supported, rejected, or defined — and if supported,
   say how the source survives reallocation.

2. **The aliasing promise requires shared mutable state, which is not
   stated.** "Handle copies alias one buffer" means a copy must observe the
   *new* pointer, length, and capacity after another copy grows the buffer.
   That is only possible through a heap control record; a value struct holding
   pointer/length/capacity would leave every copy stale after the first
   growth. The RFC promises the behavior without specifying the representation
   that makes it possible, and validates nothing about it.

3. **`free` releasing "only the growth buffer" contradicts that record.** If
   construction allocates a control record, `free` must release it too, or it
   leaks on every builder. The two statements cannot both hold.

4. **Allocator ownership is stated three different ways** — construction
   "captures" a Heap, growth uses "the default Heap", `free(heap)` takes
   another. Probed mitigation: the reference says *"There is exactly one
   default allocator: Heap is a value token with no runtime state"*, so no
   Heap can be the wrong Heap — but the spec should say that rather than leave
   three phrasings to reconcile.

5. **The lifetime rule contradicts the storability rule.** `bytes()` is said
   to dangle "when the builder otherwise leaves scope", while the builder is
   simultaneously valid as a function result, container value, and Task
   argument. A local handle going out of scope cannot end the lifetime of an
   explicitly-released shared allocation. View validity should be tied to
   release and to reallocating mutation, not to scope.

6. **`Slice<Byte>` read-only has a basis — cite it.** Probed: the reference
   states *"`Slice<T>` permits element reads only. `Slice<mut T>` permits
   element reads and writes."* So `bytes()` returning `Slice<Byte>` **is**
   read-only by type. The promise is sound; the spec should point at the rule
   instead of asserting the property.

## C. Factual corrections

7. **The motivating example does not compile.** The spec calls the quadratic
   concat loop "probe-verified" as compiling. Probed, as written it is
   rejected:

   ```text
   [Type Error] concat requires Slice<Byte>; got String
   ```

   Post-RFC 0224, `concat` takes `Slice<Byte>` and returns `String | Error`.
   The corrected form — `try acc.concat(h, "<tr></tr>".bytes())` — does
   compile, so the quadratic gap is real; but the evidence for it was not
   re-verified after 0224 landed.

8. **A leak checker does run in this repository's gate.** The spec says the
   release claim rests "by inspection — no leak checker runs in this
   repository's gate". Probed: `compiler/tests/c23validation/leak_test.go`
   defines `TestC23SuiteLeak`, which rebuilds each leak-checked fixture with
   `-fsanitize=leak` and requires a clean run. The builder's `free` behavior
   can and should be leak-checked by adding its fixture to
   `leakCheckedFixtures`. This makes the validation *stronger* than the spec
   assumed.

9. **"Follows the `List` template exactly" is contradicted three times.**
   Probed: `List<Byte> == List<Byte>` is **accepted**. The RFC makes builders
   non-comparable, non-orderable, and non-printable by citing the reference's
   *"Functions, allocators, and Dicts have no equality"* list — which does not
   contain `List`. A builder would be strictly less capable than the
   `List<Byte>` it claims to copy, and the divergence is never justified.
   `clear()` is rejected as unnecessary on the same grounds, yet `List.clear()`
   exists and is probed working.

10. **The three rejection messages do hold up.** Probed Heap diagnostics are
    `allocator handles are not equality-comparable`, `ordering is unavailable
    for Heap values`, and `print does not support Heap`. The proposed wording
    matches that shape exactly. The Dict-key claim also holds: probed, the
    existing message is `dictionary key type must be Int32 or String<N>`.

## D. Contract imprecision

11. **The complexity claim is wrong as stated.** A `push` of m bytes cannot be
    "amortized O(1)"; it is at least O(m). The intended promise is amortized
    O(m) per push and O(n) for n total bytes under a geometric capacity
    policy.

12. **"Consumed" is the wrong word for the input slice.** The described
    behavior is borrowed for the call and copied before return — the caller
    may free or reuse the source afterwards, which "consumed" denies.

13. **An empty `bytes()` needs a C-level contract.** Specify the
    pointer/length representation before the first push, so no zero-length
    view passes an invalid or null pointer into a C operation.

14. **`build()` was rejected against the wrong alternative.** The rejection
    says it duplicates `String.from_bytes(heap, b.bytes())`, which validates
    and **copies**. A finishing operation could validate and **transfer**
    storage, avoiding a second full copy of a large document — materially
    different peak memory, not a second spelling. Whether transfer fits
    `String`'s representation is a real question; it was not asked.

15. **`clear()` was rejected against the wrong cost.** "Free plus a fresh
    constructor" discards capacity on every reset, which matters precisely for
    the repeated-render case this RFC exists to serve. Deferring it is
    defensible; calling it equivalent is not.

16. **The streaming claim is overstated.** `write(b.bytes())` avoids
    materializing a `String`, but the whole document is already in memory.
    That is a contiguous-buffer write, not streaming during rendering, and no
    Validation row exercises a sink at all.

## E. Validation gaps

Under this repository's rule that a Validation section is the exhaustive
definition of done, these must be added before implementation if the behavior
is required:

17. No case where one handle copy grows the buffer and another observes the
    new length and bytes — the aliasing promise is untested.
18. No `b.push(b.bytes())` case, although the public signature permits it.
19. `==` and `<` are covered; `!=` and the remaining ordering operators are
    not.
20. Function *result* storage is validated; function *parameter* storage is
    claimed but not.
21. The catalog snippet assembles "a small document from literal chunks",
    which does not exercise the loop assembly that motivates the feature. A
    repeated-row document with dynamic fields is the case that matters.
22. Overflow and allocation-failure rows require exact traps but give no
    deterministic way to reach either condition without enormous allocations.
23. The growth-overflow trap is claimed to reuse `string allocation size
    overflow`, but a `List`-backed buffer traps with `list capacity is not
    representable` (`packages/list.h`). Whichever container backs the builder,
    the trap inventory test requires every literal to carry a disposition, and
    the accounting here is incomplete.
24. "Each qualified pack" is not an executable target; name the gate or make
    the row conditional.
25. The dependency assertion mixes two ideas — a builder may require generated
    string-component code while adding no runtime-pack dependency. Name which
    list is meant.
26. The public-header `#include` prohibition is an implementation constraint
    with no user-visible behavior behind it, and may obstruct declaring the
    type with valid C23 types.

## F. Fit for the stated template goal

The originating need is text and HTML template assembly. A byte buffer is the
right *primitive* for that, but it is not the feature.

**Bulk append gives the capability, not the syntax.** With `push_all` alone, a
row is several calls with `.bytes()` on each, and the HTML is shredded across
them:

```hexal
b.push_all("<tr><td>".bytes())
b.push_all(row.name.bytes())
b.push_all("</td></tr>".bytes())
```

What makes templates read as templates is interpolation with a buffer
destination — the missing third target of a construct that already exists:

```text
String.interpolate(heap, template)   -> String              exists
String<N>.interpolate(template)      -> String<N> | Error   exists
List<Byte>.interpolate(template)     -> no value            the gap
```

The lowering is already the right lowering. `hoistStringInterpolate` plans
each segment's byte source and length, checked-sums them, allocates once, and
copies in source order; retargeting it to append into a buffer changes the
destination, not the segment machinery. One constraint: `{{ expr }}` is
currently *"a Type Error anywhere except exactly `String.interpolate`'s second
argument"*, so a buffer target means widening that rule to a small closed set
of receivers — deliberately, not incidentally.

**HTML escaping is unaddressed and is not optional.** Interpolating user data
into HTML without escaping is XSS, and it is silent — the code looks correct.
Escaping is also context-dependent: text content, attribute values, URLs, and
script content do not share one safe transformation. That decision belongs to
a template spec, but it must be made before anything renders HTML.

**This RFC should stop asserting that future renderers will target this
builder.** Their output interface has not been designed; committing a concrete
type as their sink before that is settled is the coupling this arc otherwise
avoids.

## G. One review claim that does not hold

One review treated the tagged C23 suite as dormant, citing AGENTS.md, and
concluded RFC 0125 must be a prerequisite. Probed: the suite is **live**.
`compiler/tests/c23validation` declares fifteen exported test functions
including `TestC23Suite`, `TestC23SuiteUBSan`, `TestC23SuiteLeak`, and
`TestC23SnippetCatalogCompiles`. RFC 0125 is closed and archived.

AGENTS.md's statement that "the c23 canaries are currently dormant — their
entry points are named in lower camel case, so Go collects none of them" is
stale and should be corrected separately; it is not this RFC's problem, but it
misled a review and will mislead others.

## What a revision would need

1. Answer A: why `List<Byte>` plus `push_all` cannot supply this. If it can,
   this RFC becomes that method.
2. If a distinct type survives: settle the control-record representation,
   self-append, allocator ownership, `free`'s scope, and view lifetime.
3. Correct the cost claim, the "consumed" wording, and the motivating example.
4. Justify each divergence from `List` — equality, ordering, print, `clear` —
   or drop it.
5. Reconsider `build()` as storage transfer rather than a second copy.
6. Close the Validation gaps, including a leak-checked `free` fixture now that
   the leak gate is known to exist.
7. Leave template rendering, formatting, and escaping to a template spec, and
   stop naming this type as their sink in advance.
