# RFC 0152: Generic `Strand<N>` and Capacity Widening

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Design settled; implementation blocked by RFC 0153
- Created: 2026-09-08
- Updated: 2026-09-09
- Origin: user request — `Strand` is Hexal's `char str[N] = "Hello";`; the
  language should let the programmer choose `N` instead of hard-coding one
  size, and a smaller-capacity value should widen to a larger one. Extended:
  `Strand` and `String` should be capability-analogous — the same
  byte/Unicode API surface, both internally immutable — differing only in
  storage (`Strand<N>`: inline, stack, bounded by `N`; `String`: heap,
  unbounded)
- Supersedes nothing; the selected direction does not adopt RFC 0151
  (removing `Strand` entirely). RFC 0151 remains an Open Discussion recording
  the competing alternative, is not scheduled, and does not amend or block
  this RFC
- Coordinates with: RFC 0139 (String/Strand comparison), whose specification
  is already amended for generic capacity — see Semantics 11
- Coordinates with: RFC 0039 (C interoperability), whose fixed-layout mapping
  must recognize each concrete `Strand<N>` as a distinct inline type
- Depends on: RFC 0153 implementing the `Borrow<T>` read-only range type used
  by this RFC's public constructor and view signatures (and therefore on RFC
  0154's Core, RFC 0153's own prerequisite)
- Does not own: `Array<T, N>`'s own widening (see Non-goals — raised in the
  originating request, found not coherently achievable in the same shape as
  `Strand`'s, and left as an open, separate question, not solved here)

## Summary

Make `Strand` generic over its inline byte capacity: `Strand<N>` reserves `N`
usable UTF-8 payload bytes (backed by an `N+1`-byte buffer: payload plus the
always-present zero terminator/fill byte), replacing today's single
hard-coded `Strand`. `Strand<N>` widens implicitly to `Strand<M>` when `N <=
M`; the reverse requires no path in this RFC.

Beyond capacity, give `Strand<N>` the same byte-level and Rune-level API
`String` already has (`.bytes()`, `.slice()`, `.rune_cursor()`), and the same
ability to be *built* from computed content at runtime (`.from_bytes()`,
`.from_runes()`, `.interpolate()`, `.concat()`) rather than remaining
literal-only. The one capability that does not transfer is anything tied to
heap ownership (`String.free`) — `Strand<N>` never allocates, so there is
nothing to free, which is the actual, permanent difference between the two
types: storage location and boundedness, not what you can do with the text.
Every new runtime constructor traps if the content it's given doesn't fit in
`N` bytes, matching how every other Hexal operation that discovers a runtime
value violates a type's built-in bound already behaves (index-out-of-bounds,
slice-bounds, malformed-UTF-8 all trap; none of them return a `V | Nil` or
`T | Error` instead).

This mirrors `Array<T, N>`'s generic-arity syntax (a compile-time
positive-integer parameter, one monomorphized C struct per instantiation),
but it is not an ordinary generic underneath: `Array<T, 4>` and `Array<T, 8>`
have zero relationship to each other, while `Strand<4>` and `Strand<8>` are
related by implicit widening and direct comparison. Those cross-instantiation
rules are this RFC's actual content, specified below — copying Array's
generic machinery gets the syntax and the per-N struct generation for free,
not the widening/comparison/construction behavior.

A compiler probe against the current implementation confirms there is no
migration defect: a 31-byte literal compiles today, a 32-byte one fails with
`[Type Error] Strand literal exceeds 31 UTF-8 bytes`, against a generated
`uint8_t data[32]`. Today's `Strand` is exactly `Strand<31>` under this RFC's
naming (`N` = payload bytes, buffer = `N+1` bytes) — the analogous C
declaration is `char data[N + 1]`, not `char data[N]`.

## What `N` means

`N` is the payload capacity **in UTF-8 bytes**, not Unicode scalars/Runes and
not grapheme clusters. This is the only choice that keeps `Strand<N>`
statically sized: UTF-8 is variable-width (1-4 bytes per code point), so "room
for N Runes" cannot be a fixed byte count known at compile time — the same
content-dependent-length problem `docs/reference.md` already documents for
why `String`/`Strand` indexing doesn't exist at all ("reaching the nth Rune of
UTF-8 walks from the start"). A byte-capacity guarantee is the only one a
stack buffer can make.

Considered and rejected: a two-parameter `Strand<Byte, N>` / `Strand<Rune, N>`
split, where the `Rune` form reserves a worst-case `4*N + 1` bytes so N Runes
of any width are guaranteed to fit. This is technically buildable (the
reservation is still a static, compile-time-computable size), but it buys an
ergonomic convenience — "give me room for N characters" — at a real,
usually-wasted 4x memory cost for typical (mostly-ASCII) text, and it adds a
second type parameter and a second sizing convention to explain. Recommend
against it: `Strand<N>` always means N bytes; a caller who wants headroom for
worst-case-width Runes computes `4*N` themselves. The convenience form is
rejected by this RFC and would require a separate future proposal.

## Semantics

1. **Grammar.** `Strand<N>` is written with the same angle-bracket generic
   syntax `Array<T, N>` and `List<T>` already use. Strand is a fixed text
   buffer with NUL-scan length semantics, not a pointer-length structural
   primitive like RFC 0153's `Borrow<T>`. `N` is a positive decimal integer
   literal in the inclusive range `1..4096`, exactly like `Array<T, N>`'s
   literal parameter syntax but with this additional fixed ceiling. Decimal
   separators are accepted and normalized, so `Strand<4_096>` and
   `Strand<4096>` are the same canonical type. Zero, a value above 4096, or
   any non-literal is a Type Error rather than a runtime or generation-time
   failure. The maximum generated buffer is therefore 4097 bytes including
   the terminator.

   **Bare `Strand` (no argument) is invalid**, not an alias for any default
   capacity — every use spells `Strand<N>` explicitly, matching
   `Array<T, N>` always requiring both `T` and `N`. An uncontextualized
   string literal (`label := "ready"`, no declared type) remains invalid:
   string literals are contextual initializers under `docs/reference.md`, so
   `:=` needs an annotation on one side. This RFC neither infers a Strand
   capacity nor introduces default-to-String inference.

2. **Representation.** `Strand<N>` lowers to `struct { uint8_t data[N+1]; }`
   (today's fixed `hex_strand` becomes `hex_strand_N`, one monomorphized
   struct per distinct N — the same code-generation shape
   `hex_array_<T>_<N>` already uses for `Array<T, N>`). The existing
   zero-fill/NUL-scan convention is unchanged, just parameterized: content is
   the bytes up to the first zero byte, bounded by N; every byte after the
   content, up to and including index N, is required to be zero.

3. **Literal construction.** Unchanged from today's Strand other than the
   size check: a string or raw-string literal in a `Strand<N>`-expected
   context becomes a `Strand<N>` value if its UTF-8 payload is at most N
   bytes and contains no embedded NUL; otherwise it's a Type Error
   ("Strand<N> literal exceeds N UTF-8 bytes"). This is checked entirely at
   compile time and never traps.

4. **Runtime construction — the capability String already has.** Unlike
   today's Strand, `Strand<N>` gains constructors that accept computed,
   runtime-known content, mirroring `String`'s constructors minus the `Heap`
   parameter (nothing here allocates):

   ```text
   Strand<N>.from_bytes(bytes: Borrow<Byte>) -> Strand<N>
   Strand<N>.from_runes(runes: Borrow<Rune>) -> Strand<N>
   Strand<N>.interpolate(template: InterpolationTemplate) -> Strand<N>
   Strand<N>.concat(other: Strand<M>) -> Strand<N>
   Strand<N>.concat(other: String) -> Strand<N>
   ```

   Each traps, rather than returning `Strand<N> | Nil` or `Strand<N> | Error`,
   when the runtime content doesn't fit in `N` bytes — consistent with every
   other Hexal operation that discovers a runtime value violates a type's own
   built-in bound (list/array index-out-of-bounds, slice bounds,
   `String.from_bytes`'s malformed-UTF-8 trap). This is a capacity-planning
   error on the caller's part, the same category as those, not a recoverable
   condition worth a `Nil`/`Error` path. `from_bytes` separately traps on
   malformed UTF-8 (reusing `String.from_bytes`'s exact trap) and on embedded
   NUL (`Strand`'s own existing rule), as two distinct conditions from the
   capacity trap, matching how literal construction already reports "embedded
   NUL," "invalid UTF-8," and "overflow" as three separate diagnoses.
   `from_bytes` validates the complete input before writing, then
   zero-initializes one `Strand<N>` result and copies the validated payload.
   `from_runes` first walks every Rune and sums its UTF-8 encoded byte length
   with checked arithmetic; the check is against encoded bytes, not Rune
   count. Only after the full length fits does a second pass encode into one
   zero-initialized result. Neither operation can leave a partially
   initialized observable value.

   `concat`'s two overloads dispatch on the argument's checked static type, the same pattern
   `Heap.free(Ptr<T>)`/`Heap.free(MutPtr<T>)` already establishes for
   parameter-type overloading. A typed `Strand<M>` argument selects the first
   overload; a `String` argument or an unforced string literal selects the
   second. A generic argument selects only after specialization gives it one
   of those concrete types; a still-open type parameter is not an overload
   candidate. Bare `Strand` does not exist after Semantics 1. Both overloads
   evaluate the receiver before the argument, each exactly once, measure the *actual* runtime length of
   `other` (its own declared capacity, if it has one, is irrelevant — only
   how much of it is actually filled matters), so no capacity arithmetic
   (`N + M`) is needed anywhere in this RFC.

   `interpolate` evaluates its embedded expressions exactly once from left to
   right, computes the complete UTF-8 byte length with checked arithmetic,
   and traps before writing if that length exceeds `N`. It then writes
   directly into one zero-initialized `Strand<N>` result. It never allocates
   and never constructs an intermediate `String`.

   **Recommended usage: bound the input yourself, don't rely on the trap as
   control flow.** A well-written call site slices its dynamic `String`/bytes
   down to the target capacity before calling, rather than treating the trap
   as an expected outcome. One caution: a naive byte-offset slice can land in
   the middle of a multi-byte UTF-8 character, producing invalid UTF-8 that
   then trips `from_bytes`'s malformed-UTF-8 trap instead of the capacity one
   — "sliced defensively" doesn't mean "sliced at a raw byte offset."
   `String.slice(start, end)` already uses Rune bounds, not byte bounds
   (`docs/reference.md`), so it never splits a character mid-encoding — but a
   Rune count doesn't map directly to a byte count (each Rune is 1-4 bytes
   encoded), so guaranteeing a fit within `N` bytes via `.slice()` alone means
   either budgeting conservatively (`N / 4` Runes, guaranteed to fit but
   wasteful for ASCII-heavy text) or walking `.rune_cursor()` to find the
   exact largest prefix that fits. This RFC does not add a single-call
   "largest valid-UTF-8 prefix within a byte budget" utility — it's a
   caller-side pattern to document, not new language surface, unless a
   concrete recurring need for one shows up.

5. **Byte and Rune views — the capability String already has.** `Strand<N>`
   gains the same dual access `String` has: a byte-level slice and a
   Rune-level cursor over the same one underlying representation, not two
   different storage forms:

   ```text
   Strand<N>.length() -> Size
   Strand<N>.bytes() -> Borrow<Byte>
   Strand<N>.slice(start: Integer, end: Integer) -> Borrow<Byte>
   Strand<N>.rune_cursor() -> RuneCursor
   Strand<N>.to_string(heap: Heap) -> String
   ```

   `slice` uses Rune indices, exactly like `String.slice`: it validates
   `0 <= start <= end <= length()` in Rune space, walks UTF-8 to find the two
   byte boundaries, and returns the zero-copy bytes between them. It never
   accepts raw byte offsets and therefore cannot split a valid UTF-8 encoding.
   `bytes()` is the operation for byte-indexed access.

   Signatures match `String`'s post-RFC-0153 surface. These Borrow values are
   ordinary borrows of the `Strand<N>` value's
   own inline storage: construction records the Strand binding as their root,
   bindings preserve that root, and the existing direct-local-root return
   check rejects returning one rooted in a local Strand exactly as it does
   for a local Array. There is no mutable variant (`Borrow<mut Byte>`) for either
   type's view, because Strand stays immutable — see Semantics 6.

   `RuneCursor` physically contains a byte pointer, length, and offset. A
   cursor over a Strand therefore records the same Strand root as a Borrow;
   cursor bindings preserve that root, and directly returning a cursor rooted
   in a local Strand is rejected. Parameter-rooted, self-rooted, and rootless
   cursors remain returnable. Nested aggregate, collection, interprocedural,
   and Task escape tracking stays outside this RFC, matching RFC 0153's
   explicitly accepted safety envelope for Borrow and Ptr.

6. **Immutability, restated.** Both `String` and `Strand<N>` remain
   internally immutable — this RFC does not reopen that. "Same capabilities"
   means the same *read* and *construct-a-new-value* operations, not shared
   mutability; there is no in-place byte-level mutation of either type before
   or after this RFC.

7. **What does not transfer: heap ownership.** `String.free(heap)` has no
   `Strand<N>` counterpart, and none of the new constructors take a `Heap`
   argument. This isn't an oversight — `Strand<N>` never allocates, so there
   is nothing for it to own or release. This is the one genuine, permanent
   difference between the two types this RFC preserves rather than erases:
   storage location and boundedness, exactly as stated in this RFC's Origin.

8. **Widening.** `Strand<N>` converts implicitly to `Strand<M>` wherever `N
   <= M`, at initialization, assignment, argument passing, return, object or
   ADT field construction, collection insertion, and union injection (see
   Semantics 10) — the contextual destination positions numeric widening
   (`Int32` to `Int64`) already uses. Contextual `match` arms inherit the
   destination of the enclosing position. Strand does not participate in
   numeric binary common-type selection; cross-capacity comparison is the
   dedicated rule in Semantics 11. The conversion copies the source's `N+1` bytes into
   the destination's larger buffer and zero-fills the remaining `M-N` bytes;
   it never truncates and never loses content, matching the lossless-only
   rule every other implicit Hexal conversion already follows. `N` and `M`
   are always compile-time constants, so the `N <= M` eligibility check is
   fully static.

   The reverse (`Strand<M>` to `Strand<N>`, `M > N`) has no implicit path —
   see Non-goals. This mirrors `MutPtr<T>` weakening to `Ptr<T>` in spirit
   (asymmetric, safe direction only), though the mechanism differs: pointer
   weakening is free, while Strand widening is a real copy of up to `M`
   bytes. That cost is bounded statically and practically: both sizes are
   compile-time constants no greater than 4096, so one widening copies at most
   4097 bytes including the terminator.

9. **Generation.** C has no cast between two differently-sized structs, so
   each reachable `(N, M)` widening pair needs a generated helper, emitted
   demand-driven (only for pairs the program actually exercises, matching
   Hexal's existing practice of not emitting unreachable generic
   specializations):

   ```c
   static inline hex_strand_8 hex_strand_widen_4_8(hex_strand_4 source) {
       hex_strand_8 result = {0};
       memcpy(result.data, source.data, 5); /* N+1 = 5 */
       return result;
   }
   ```

   The source operand evaluates exactly once (passed by value into the
   helper); `result = {0}` establishes the required zero tail, and the
   `memcpy` of `N+1` bytes carries the source's own terminator into the
   destination, which is why only `N+1` bytes (not `M+1`) need copying. The
   new runtime constructors (Semantics 4) similarly demand-drive one helper
   per reachable `N`, each doing a length check against `N` before copying
   and trapping on failure.

10. **Union selection.** A `Strand<N>` value assigned into a union type with
    multiple `Strand<M>` members (e.g. `Strand<4> | Strand<8>`) resolves by:
    exact-capacity match first; otherwise the unique smallest capacity that
    can hold the source (`M >= N`, minimal such `M`). Member declaration
    order never affects the result. The selected destination member is the
    value's active union member: when its capacity differs from the source,
    the widening operation runs first and the resulting `Strand<M>` is then
    injected under that member's tag. A context "specifically forces a
    Strand" only when the immediate expected type is one exact `Strand<N>`;
    examples are a typed binding, parameter, return, object/ADT field, or
    collection element. A plain string literal in a union
    containing both `String` and one or more `Strand<N>` members keeps
    resolving to `String` unless the context specifically forces a `Strand`
    — this RFC changes nothing about that existing default, including inside
    a union.

11. **Cross-capacity comparison.** `Strand<N>` and `Strand<M>` (including `N
    != M`) compare (`==`, `!=`, `<`, `<=`, `>`, `>=`) as:

    ```c
    memcmp(left.data, right.data, (N < M ? N : M) + 1)
    ```

    No scan is needed. Every Strand's canonical form has a mandatory
    terminator and a fully zero-filled tail, and Hexal already rejects
    embedded NUL in Strand content, so byte `0x00` exclusively marks
    "content ends here" and never appears inside real content. Comparing
    `min(N, M) + 1` bytes therefore always includes either the shared content
    or the point where a shorter, fully-contained value's terminator (`0x00`)
    correctly compares less than any real continuation byte the longer value
    has at that position — which is exactly "shorter, equal prefix first."
    Same-capacity comparison (today's only case) is the `N == M` instance of
    this rule.

    Each source operand evaluates exactly once, left before right; generation
    must materialize operands before `memcmp` whenever embedding them directly
    would inherit C's unspecified function-argument evaluation order.

    This is simpler than what an earlier draft of this RFC proposed (a
    NUL-scan). **RFC 0139 is already amended for this rule**: its Semantics
    point 4 generalizes the former hardcoded bound to `N` and does not introduce its own hardcoded
    Strand-comparison helper that this RFC would immediately need to
    generalize or replace. String-vs-Strand comparison (0139's actual
    remaining scope) still needs its own length-aware path, since `String`
    carries an explicit stored byte length rather than relying on a
    terminator scan or a fixed bound; Strand-vs-Strand does not need 0139's
    mechanism at all, per the `memcmp` rule above.

12. **Dict keys, hashing, printing, and iteration.** Every current Strand-specific rule
    (`IsDictKey` accepting Strand, `hex_hash_Strand`, direct printability)
    generalizes from "the one Strand identity" to "any `Strand<N>`,"
    parameterized the same way `Array<T,N>`'s equality/printing already is.
    Text iteration remains decoded-Rune iteration and works identically for
    every capacity. A `Dict<Strand<M>, V>` operation accepts a `Strand<N>` key
    when `N <= M`, widening it to the Dict's exact key type before hashing or
    equality; a larger-capacity key is a compile-time type mismatch, never a
    runtime missing-key result. Dict key types remain one exact Strand
    capacity and do not become union-key types under this RFC.

13. **`Error` owns bounded message text while its static file stays String.**
    Reversing this RFC's earlier position, now that Semantics 4 gives Strand
    a runtime construction path:

    ```text
    Error(header: Strand<128>, message: Strand<1024>) -> Error
    ```

    - `header: Strand<128>` (up from today's `Strand<31>`, and from this
      RFC's own earlier `Strand<31>` recommendation).
    - `message: Strand<1024>`. A literal constructs this type contextually;
      an existing computed `String` requires the explicit runtime conversion
      described below. There is no implicit String-to-Strand conversion.
    - `file: String`, unchanged. It is not a constructor parameter: the
      compiler injects the module's logical source key as permanently
      static-backed String storage. It therefore needs no cleanup and cannot
      dangle through Error propagation; copying its pointer-sized handle is
      both safe and substantially cheaper than adding another 1,025 inline
      bytes to every Error.
    - On an ordinary 64-bit C ABI this layout is approximately 1,184 bytes,
      rather than approximately 2,208 bytes with an inline `file` field.
      Error and unions containing it remain by-value types, so propagation
      copies that bounded representation; this cost is accepted by this RFC.
    - This is an intentional source-breaking signature change with no
      deprecation period. Callers with a literal message keep working
      unchanged because the constructor context builds `Strand<1024>`
      directly. Existing callers passing a computed `String` must migrate by
      converting its bytes explicitly. Today's
      built-in error text is already string-literal-only (`docs/reference.md`
      documents IO failures using "a static message such as `read failed`"),
      so those call sites just get a bigger, generic-parameterized type,
      nothing else. A caller building a message from runtime-computed
      content (`.concat`/`.interpolate` results) now routes it through one of
      Semantics 4's constructors instead of passing a `String` directly:
      `Error("MyError", Strand<1024>.from_bytes(computed.bytes()))`.
      That call traps under Decision 4's policy if the computed content
      exceeds 1024 bytes — a generous bound, not expected to be hit by
      ordinary error text, but a real, new trap path at error-construction
      sites that did not exist before this RFC. Per Semantics 4's
      recommended usage, well-written call sites bound `computed` to fit
      before calling rather than relying on the trap — mind the UTF-8
      boundary caution there when doing so.
    - A useful side effect, not the motivation: `String`'s "runtime `message`
      storage must remain live while any alias can be inspected or printed"
      lifetime concern (`docs/reference.md`) disappears for `message` once it
      becomes an inline Strand value. `file` remains safe because its String
      is compiler-owned static storage. Neither field gives Error a cleanup
      obligation; this RFC does not otherwise change Error's ownership model.

## Required sweep

Search code, templates, tests, snippets, `docs/reference.md`, status, and active specs for
`Strand`, `StrandType`, `hex_strand`, `NeedStrand`, and `IsStrand`. Every
result needs to become N-parameterized or explicitly confirmed unaffected;
none may keep assuming there is exactly one Strand identity. Known-affected
areas:

- protected-type registration and canonical type identity (today: one
  `StrandType`; after: one family member per instantiated `N`, same pattern
  `ArrayType`/`arena.arrayTypes` already uses);
- literal contextual typing (`checkedStringLiteralValue`'s `IsStrand`
  branch and its 31-byte/NUL checks, generalized to the expected type's `N`);
- `IsStrand` itself and every placement/eligibility rule keyed off it;
- `Error`'s canonical object definition (`header` changes from the old fixed
  Strand to `Strand<128>`, `message` changes from `String` to
  `Strand<1024>`, and compiler-injected `file` remains `String`);
- every `Error(header, message)` construction site currently passing a
  `String` built from computed content for `message` — these need
  rewriting through a Semantics 4 constructor, not just a type-signature
  update; a literal-message call site needs no change beyond the wider type;
- String component discovery (`NeedStrand`-gated generation, currently an
  on/off flag, becoming per-`N` demand, and now also gating the new
  `from_bytes`/`from_runes`/`interpolate`/`concat`/`bytes`/`slice`/
  `rune_cursor` helper families per reachable `N`);
- literal rendering in generated C;
- `length()` and `to_string()`;
- the new runtime constructors and views (Semantics 4-5): checker dispatch,
  generated helper emission, their two-pass validation/write paths where
  required, zero-initialized canonical results, and their trap diagnostics;
- `RuneCursor`'s borrow-source generalization, provenance recording and
  propagation, and direct-local-root return rejection;
- equality and ordering (same-`N` case unchanged; cross-`N` case is new, see
  Semantics 11);
- Dict hashing and probe equality for `Strand<N>` keys;
- printing and nested printing (an aggregate containing a `Strand<N>`
  member);
- string interpolation's checker/generator path, extended to a
  `Strand<N>`-typed result alongside its existing `String`-typed one;
- IO and scheduler-generated Errors: retain the static String file pointer,
  replace String message pointers with the exact inline Strand field, format
  dynamic native text into zero-initialized bounded buffers only after checked
  length calculation, and trap before returning a partially written Error on
  impossible overflow;
- validation metadata and generated helper naming (`hex_strand_N`,
  `hex_strand_widen_N_M`, `hex_strand_from_bytes_N`,
  `hex_strand_concat_N_M`, `hex_strand_concat_N_string`, `hex_hash_Strand_N`,
  etc., must not collide with each other or with unrelated generated names).

Generation replaces the single `NeedStrand` Boolean with canonical-key-sorted
sets recording reachable capacities, operations per capacity, and widening or
concat capacity pairs. `hexal/string.h` emits one typedef and the selected
small inline accessors per capacity; `hexal/string.c` emits only selected
out-of-line construction bodies and retains the shared UTF-8 primitives once.
Interpolation remains call-site lowering because its template and value
expressions are unique to the call: it uses the shared formatting primitives
but has no monolithic `hex_strand_interpolate_N` helper. Module emission owns
the sequencing temporaries and final `Strand<N>` value.

## Non-goals

- **`Array<T, N>` widening.** The originating request asked for the same
  `N <= M` widening on `Array<T, N>`. It does not transfer coherently:
  `Strand<N>`'s widening works because Strand already has a logical
  length-vs-capacity split baked into its definition (a terminator bounds
  content within a larger buffer) and because its element (byte) has an
  obvious, safe zero/pad value. `Array<T, N>` has neither property today —
  every one of its N slots is always a real, meaningfully-initialized T
  value, with no "unused tail" concept, and Hexal has no general notion of a
  default/zero value for arbitrary T. Concretely, it can't work for `T =
  MutPtr<X>`: `docs/reference.md` already states `MutPtr<T>` is non-null, so
  a zero-filled "pad" pointer would silently violate that invariant.
  Widening `Array<T, N>` to `Array<T, M>` would need either a new
  default-value mechanism for T (a new language concept, contrary to the
  direction settled earlier in this conversation: types describe structure,
  not an ownership/lifetime or defaultability contract bolted on) or a
  redesign giving `Array<T, N>` its own logical-length-vs-capacity split,
  which is a materially different, bigger type — closer to a fixed-capacity
  vector than today's Array. Left open as a separate question, not attempted
  here.
- An explicit narrowing/truncating conversion (`Strand<M>` to `Strand<N>`,
  `M > N`). Not requested; if wanted later, it needs its own fallible-call
  design (truncate vs. reject on overflow) rather than being folded into this
  RFC's implicit-widening-only scope.
- A capacity-growing `concat` (`Strand<N>.concat(Strand<M>) -> Strand<N+M>`).
  Would need compile-time arithmetic over type parameters, which nothing in
  Hexal's generic system does today; Semantics 4's `concat` avoids this
  entirely by writing into the receiver's own fixed `N` and trapping on
  overflow instead.
- A fallible (`Strand<N> | Nil` or `Strand<N> | Error`) variant of any new
  constructor, alongside the trapping one — the `.get`/`.find` dual-API
  pattern Hexal uses elsewhere (Dict, primarily) is not adopted here by
  default. Add one later, separately, only if a real need for a recoverable
  path shows up; trapping alone is this RFC's complete answer.
- The `Strand<Byte, N>`/`Strand<Rune, N>` two-parameter form (see "What N
  means") — open decision, not adopted by default.
- Capacities above 4096. The fixed ceiling bounds inline object size and every
  widening copy independently of the eventual C target's `size_t` width;
  `Strand<4097>` is a Type Error.
- Anything from RFC 0151 (removing Strand, `[N]T` array spelling) — that RFC
  remains a non-normative Open Discussion and is not in effect; this RFC
  assumes today's `Strand` and `Array<T, N>` remain as named, generic-arity
  types.

## Open decisions

None. RFC 0151 remains a non-normative Open Discussion of an alternative, not
an unresolved decision inside this RFC.

## Validation

This section is exhaustive.

- Capacity arguments: missing, zero, negative, non-literal, and an `N` whose
  value exceeds 4096 are all rejected at compile time; every positive literal
  through 4096 is accepted. Decimal
  separators are normalized, so `Strand<1_024>` denotes the same canonical
  type as `Strand<1024>`.
- Bare `Strand` (no type argument) is rejected everywhere a type is expected.
- An unannotated contextual literal such as `label := "ready"` remains
  rejected; `label: String := "ready"` and `label: Strand<N> := "ready"`
  select their written destinations.
- Literal construction: a 0-byte literal (`""`), an exactly-`N`-byte literal,
  and an `(N+1)`-byte literal (rejected) all behave correctly at a boundary
  `N`; a multi-byte UTF-8 character is never split across the boundary check
  (the check is on total encoded bytes, not code point count).
- Embedded NUL and invalid UTF-8 are rejected in `Strand<N>` literal
  construction for every `N`, matching today's Strand.
- `from_bytes`: content that fits constructs correctly; content exceeding `N`
  bytes traps; malformed UTF-8 traps with the same diagnostic
  `String.from_bytes` uses; embedded NUL traps with its own diagnostic,
  distinct from the capacity trap.
- Runtime capacity failures use exactly `[Runtime Error] Strand capacity
  exceeded\n`; embedded runtime NUL uses exactly `[Runtime Error] Strand
  cannot contain NUL\n`; malformed UTF-8 retains exactly `[Runtime Error]
  invalid UTF-8 in string\n`.
- `from_runes`: encoded content that fits constructs correctly; content whose
  encoded length exceeds `N` traps.
- `interpolate`: a template whose expansion fits constructs correctly; one
  whose expansion exceeds `N` traps.
- `concat`: both overloads (`Strand<M>`, `String`) succeed when the combined
  actual content fits in the receiver's `N` and trap when it doesn't,
  regardless of the argument's own declared capacity; each operand evaluates
  exactly once.
- `bytes()`/`slice()`/`rune_cursor()` on `Strand<N>` produce results
  identical in content to the equivalent `String` operations over the same
  logical text; a slice rooted in a local `Strand<N>` binding is rejected on
  direct return, the same as one rooted in a local `Array`.
- A RuneCursor rooted in a local Strand is rejected on direct return, including
  through an intermediate cursor binding; a parameter-rooted cursor may be
  returned. The nested/interprocedural escape envelope remains exactly RFC
  0153's documented envelope for Borrow and Ptr.
- `slice` interprets both bounds as Rune indices; ASCII and multi-byte inputs
  return the corresponding byte span, invalid order/out-of-range bounds trap,
  and no successful result splits an encoded Rune.
- `length()` counts Runes, `to_string(heap)` allocates one String with
  identical content, and one- and two-binder `for` loops decode identical
  Runes for small, exact-capacity, temporary, and multi-byte Strands.
- No in-place mutation exists for `Strand<N>` before or after this RFC;
  `Borrow<mut Byte>` is never produced by any Strand view.
- `Strand<N>` constructed identically in two different modules resolves to
  the same canonical type identity.
- Widening fires in every implicit position: initialization, assignment,
  argument, return, object/ADT field, collection insertion, contextual match
  arm, and union injection (Semantics 10); the source's content and terminator
  survive exactly, and the destination's extra tail is all zero.
- Widening evaluates its source expression exactly once, including when the
  source is itself side-effecting.
- No path exists, implicit or explicit, from `Strand<M>` to `Strand<N>` when
  `M > N` (narrowing is rejected, not merely undocumented).
- Union injection picks the exact-capacity member when present, otherwise
  the unique smallest fitting capacity, independent of member declaration
  order; a bare string literal targeting a `String | Strand<N>` union still
  resolves to `String` absent a forcing context.
- All six comparison operators produce correct results for `Strand<N>` vs.
  `Strand<M>` in both operand orders, for equal content at different
  capacities, unequal-prefix content, and one value being a strict prefix of
  the other.
- Each comparison operand evaluates exactly once, left before right.
- `Dict<Strand<N>, V>` and `Dict<Strand<M>, V>` (different capacities) both
  work as key types. Insert/get/find/contains/remove accept a smaller-capacity
  key by widening it before hashing and comparison, accept an exact key
  unchanged, and reject a larger-capacity key at compile time. Dict does not
  accept a union of Strand capacities as its key type.
- `Error.header` at `Strand<128>`: exact-capacity construction, a smaller
  `Strand<N>` widened into it, and an over-capacity literal rejected at
  compile time.
- `Error.message` at `Strand<1024>`: a literal message compiles unchanged; a
  message built via `.from_bytes`/`.interpolate`/`.concat` from computed
  content constructs correctly when it fits and traps when it doesn't
  (Decision 4).
- `Error.file` remains a compiler-injected static-backed String built once per
  module from that module's logical source key. It is copied as a handle,
  never freed, and remains valid through every Error copy and propagation.
- Every generated `hex_strand_N`, `hex_strand_widen_N_M`, and new
  constructor/view helper is demand-driven: no helper is emitted for an
  `(N, M)` pair, a capacity `N`, or an operation the compiled program never
  reaches; no accepted snippet's generated output changes outside the
  manifest scope this RFC's changes justify.
- `go test ./...` and `go vet ./...` pass; tagged C23 fixtures compile and
  run representative construction (literal and runtime), widening,
  comparison, view, Dict-key, and Error-header cases on every qualified
  toolchain, including at least one trap case per new constructor.

## Decisions

1. `N` = payload bytes, not Unicode scalars/Runes and not the
   `Strand<Byte, N>`/`Strand<Rune, N>` worst-case-width form — confirmed.
   For plain ASCII content, byte count and character count coincide exactly;
   the two only diverge for multi-byte UTF-8 content, where a fixed-size
   buffer cannot make a "N characters of any width" guarantee regardless.
2. `Error.header` becomes `Strand<128>`, `message` becomes `Strand<1024>`,
   and compiler-injected `file` remains a permanently static-backed `String`
   — confirmed. This removes dynamic message lifetime/cleanup obligations
   without paying for another 1,025 inline file bytes; see Semantics 13.
3. No narrowing conversion ships in this RFC — confirmed. Add one later,
   separately, only if a real need shows up.
4. Every new runtime constructor (`from_bytes`/`from_runes`/`interpolate`/
   `concat`) traps on overflow rather than returning a fallible
   `Strand<N> | Nil` or `Strand<N> | Error` — confirmed. Matches every
   existing Hexal operation in this category (bounds checks,
   malformed-UTF-8).
5. `Strand<N>` accepts `N` only in `1..4096` — confirmed. The ceiling is a
   language rule, not a target-size query.
6. `Strand<N>.rune_cursor()` records and propagates Strand borrow roots and
   rejects a direct return rooted in local Strand storage — confirmed. Its
   broader escape envelope matches RFC 0153 rather than adding a separate
   lifetime model.
