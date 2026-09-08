# RFC 0147: utf8proc Unicode Backend

- Kind: Architecture Decision Record (ADR)
- Status: Draft; architecture proposed, dependency qualification not started
- Created: 2026-09-08
- Scope: use a pinned utf8proc build for runtime UTF-8 scalar processing and as
  the implementation foundation for later explicit Unicode operations
- Depends on: RFC 0052 (C compiler backend) and RFC 0055 (filesystem/build
  driver)
- Coordinates with: the String, Strand, Rune, and RuneCursor contracts in
  `docs/reference.md`
- Does not change: String or Strand representation, Rune identity, bytewise
  text equality/order, allocation ownership, literals, iteration results, or
  malformed-UTF-8 diagnostics

## Summary

Use one pinned, statically linked utf8proc build as the generated runtime's
Unicode algorithm backend.

The first integration replaces compiler-owned UTF-8 scalar validation,
decoding, and encoding inside `hexal/string.c` with utf8proc's non-allocating
operations. Hexal retains its adapters because they own index advancement,
Hexal types, and exact runtime traps.

Do not normalize, case-fold, or otherwise transform text implicitly. Future
normalization, case-folding, grapheme, category, and display-width APIs must be
explicit ordinary library operations specified separately. This RFC qualifies
utf8proc as their shared backend but does not add those language surfaces.

## Motivation

The current runtime manually implements UTF-8 width validation, scalar
decoding, and scalar encoding. The implementation is bounded, but every future
Unicode operation would require substantially more tables and algorithms:

- canonical and compatibility normalization;
- full Unicode case folding;
- grapheme-cluster boundaries;
- Unicode general categories;
- character display width; and
- synchronization with new Unicode releases.

utf8proc is a small C library with a regularly updated Unicode data set and
provides these operations over UTF-8. Adopting one backend avoids independent
Unicode tables and subtly incompatible definitions across future packages.

Using the dependency only for today's scalar stepping may increase binary size
for a modest code deletion. This RFC therefore requires a size and performance
record and keeps higher Unicode features demand-driven. Correctness and one
authoritative Unicode version are the primary benefits.

## Decision

### 1. Dependency and version

- Pin one exact utf8proc release and its Unicode-data version in each target
  pack.
- Build it as a static library under its supported source dialect and flags.
- Qualify all six initial profiles independently.
- Ship its MIT/Unicode license material.
- Do not silently use a system-installed utf8proc.
- Treat a utf8proc or Unicode-data upgrade as a target-pack identity change.

### 2. Existing text representation remains authoritative

Keep the current generated representations unchanged:

- String remains an immutable non-null pointer-sized handle to a header holding
  `const uint8_t *data`, byte length, and Rune length.
- Runtime String storage remains one header-plus-bytes allocation with one
  trailing NUL; literals remain static storage.
- Strand remains 32 inline bytes: at most 31 UTF-8 bytes, one NUL, then zero
  fill.
- Rune remains a distinct `uint32_t` Unicode scalar value excluding surrogate
  code points.
- RuneCursor remains a borrowed byte pointer, byte length, and independent byte
  offset.

Do not change storage to `char *`, `char8_t *`, `utf8proc_uint8_t *`, UTF-16,
or UTF-32. The utf8proc byte-pointer type is adapted privately at calls.

### 3. Scalar validation and decoding

Use `utf8proc_iterate` for one scalar step.

The Hexal adapter must:

- pass `min(remaining bytes, 4)` as `utf8proc_ssize_t`; one UTF-8 scalar is at
  most four bytes, so this avoids narrowing an arbitrary `size_t` String length
  while preserving truncated-sequence detection;
- reject every negative result with exactly
  `[Runtime Error] invalid UTF-8 in string\n`;
- advance the caller's byte index by the positive returned width;
- return the decoded scalar where required;
- never read beyond the supplied byte length; and
- preserve empty-input and exhausted-cursor behavior owned by the caller.

Retain a Hexal adapter rather than exposing `utf8proc_iterate` directly because
the adapter implements Hexal's index, result-type, and diagnostic contract.
Delete the compiler-owned lead-byte, continuation-byte, overlong, surrogate,
and maximum-scalar formula once every caller uses the qualified adapter.

### 4. Scalar encoding

Use `utf8proc_codepoint_valid` followed by `utf8proc_encode_char` to encode a
Rune into caller-owned storage. `utf8proc_encode_char` does not validate the
Unicode scalar by itself.

- The caller supplies at least four writable bytes.
- A non-positive result for a value that reached the runtime is an invalid
  Unicode scalar trap, not a utf8proc diagnostic.
- String construction retains its checked total-size arithmetic, single
  Hexal-owned allocation, header initialization, and trailing NUL.
- Do not use an allocating utf8proc convenience operation for
  `String.from_runes`.

### 5. Operations intentionally unchanged

utf8proc does not participate in:

- String or Strand equality and ordering, which remain unsigned UTF-8 byte
  comparison;
- hashing, which remains defined by Hexal's Dict contract;
- byte slicing or byte Views;
- implicit normalization of literals or constructed Strings;
- parser or lexer handling in the Go compiler;
- String allocation and `free`; or
- interpolation formatting.

Canonically equivalent Unicode sequences remain distinct unless a future
explicit normalization operation is invoked.

### 6. Future Unicode operations

The following utf8proc facilities are approved backend candidates, not APIs
introduced here:

| Facility | Backend capability | Requirement before exposure |
| --- | --- | --- |
| NFC/NFD/NFKC/NFKD | normalization and compatibility mapping | focused API, allocation, error, and Unicode-version spec |
| Case folding | locale-independent Unicode case folding | focused API and naming spec |
| Grapheme boundaries | extended user-perceived character iteration | cursor/lifetime and boundary-version spec |
| Categories | Unicode general-category lookup | stable Hexal enum/value representation |
| Character width | terminal-oriented display width | explicit platform/Unicode limitation contract |

No future API may return memory allocated by utf8proc as a Hexal String.
Prefer its caller-buffer/non-allocating interfaces. If an unavoidable internal
temporary is used, it must be copied into Heap-owned String storage and released
with `utf8proc_free` on every path; that design requires its own specification.

### 7. Generated-component and compiler boundary

- The existing `hexal/string.h`/`hexal/string.c` pair remains the owner of
  String, Strand, RuneCursor, and scalar UTF-8 adaptation.
- `hexal/string.h` exposes no `utf8proc_*` type and does not include
  `<utf8proc.h>`.
- `hexal/string.c` may include `<utf8proc.h>` and call the qualified API.
- No generated module header exposes utf8proc.
- The compiler emits dependency metadata and calls; it does not copy utf8proc
  source or Unicode tables into `CompilationResult.Files`.
- RFC 0055 supplies the selected target pack's include root and static archive.
- The core compiler remains string-in/string-out and performs no library
  discovery, file access, native compilation, linking, or runtime Unicode
  probing.

## Demand and linkage

- A program selecting `hexal/string.c` selects utf8proc linkage.
- A program using no runtime text component selects no utf8proc dependency.
- A later explicit Unicode component may select utf8proc independently under
  its own specification.
- Selection is program-wide and deterministic.

Literal-only programs that require no runtime text operation should retain the
current component-selection result. This RFC must not broaden String-component
demand merely because a literal contains non-ASCII bytes.

## Diagnostics

- Preserve the exact malformed-UTF-8 and invalid-Unicode-scalar Hexal runtime
  messages.
- Do not expose utf8proc numeric errors or English error strings as stable
  language diagnostics.
- Parser diagnostics continue to use one-based UTF-8 byte columns.
- A utf8proc internal assertion or allocation failure is not substituted for a
  Hexal contract.

## Accepted costs

- Programs with runtime text operations link a static Unicode dependency.
- Executable and target-pack size increase, including Unicode data needed by
  selected facilities.
- Each target requires independent build and runtime qualification.
- Unicode-data upgrades may change future normalization, category, grapheme,
  case-folding, and width results. The version must therefore be pinned and
  observable through backend metadata, not inferred from the host.
- A dependency does not remove Hexal's obligation to adapt errors, allocation,
  lifetime, and evaluation order.

## Rejected alternatives

### Continue implementing all Unicode internally

Reasonable for scalar UTF-8 stepping alone, but poor for normalization,
graphemes, categories, case folding, and Unicode-version maintenance.

### Replace String with utf8proc-owned memory

Breaks Hexal layout, one-allocation storage, Heap ownership, literal storage,
and `free` contracts.

### Normalize every String automatically

Changes byte identity, hashing, ordering, slicing, foreign interoperability,
allocation cost, and source fidelity. Normalization must be explicit.

### Use `utf8proc_map` for every conversion

Introduces hidden temporary allocation and allocator-family cleanup where the
existing operations can use caller-owned buffers.

### Expose utf8proc types and errors

Leaks a replaceable dependency into the language and foreign ABI.

## Required measurements

Compare the current runtime and utf8proc-backed runtime for:

- ASCII, two-byte, three-byte, and four-byte traversal;
- malformed sequences at every byte position;
- String construction from bytes and Runes;
- RuneCursor iteration;
- String slicing by Rune bounds;
- executable size for ASCII-only and Unicode-using programs;
- target-pack size; and
- compile/link time.

Record results; do not alter observable semantics to win a benchmark.

## Detailed implementation plan

### Phase 1: qualify and package

1. Select an exact utf8proc release and record its Unicode-data version and
   license files.
2. Build one static archive for each RFC 0052 target profile.
3. Record headers, archive digest, compile definitions, consumer flags, target
   identity, library version, and Unicode version in target-pack metadata.
4. Compile, link, and run probes for `utf8proc_iterate` and
   `utf8proc_encode_char` on every runnable target.
5. Probe valid boundary scalars, surrogates, overlong sequences, truncated
   sequences, bare continuations, and values above U+10FFFF.
6. Capture the required size and performance baseline before changing emitted
   runtime code.

### Phase 2: add dependency demand

1. Add one utf8proc dependency fact alongside String-component selection.
2. Select it exactly when `hexal/string.c` is selected.
3. Keep programs without runtime text support free of utf8proc includes and
   link inputs.
4. Add target-pack dependency metadata without putting host paths into
   `Project` or generated artifacts.

### Phase 3: replace scalar stepping

1. Add the private `<utf8proc.h>` include to `hexal/string.c`.
2. Rewrite the internal scalar-step path around `utf8proc_iterate`, passing at
   most four remaining bytes so conversion to `utf8proc_ssize_t` is exact.
3. Preserve index advancement, decoded Rune values, call evaluation, and exact
   Hexal traps.
4. Route validation in `String.from_bytes`, String/Strand Rune length, slicing,
   RuneCursor, and text iteration through the same semantic adapter.
5. Delete the superseded manual UTF-8 validation and decoding formulas.

### Phase 4: replace scalar encoding

1. Route `String.from_runes` through `utf8proc_codepoint_valid` and
   `utf8proc_encode_char` with caller-owned output storage.
2. Retain pre-allocation scalar validation and checked byte-total calculation.
3. Retain one Hexal allocation, the String header, Rune length, byte length,
   and trailing NUL.
4. Delete the superseded manual scalar-encoding formula.

### Phase 5: validate and synchronize

1. Update focused generator and runtime-text tests for the dependency and
   generated call shape without weakening semantic assertions.
2. Run every existing valid/malformed UTF-8, String, Strand, Rune, RuneCursor,
   iteration, slicing, comparison, hash, interpolation, and free test.
3. Run the ordinary pure-Go suite.
4. Run external C23 compile/link/run fixtures against each runnable target
   pack.
5. Compare the snippet manifest and accept movement only for artifacts that
   select `hexal/string.c`.
6. Update RFCs 0052 and 0055 with package, version, license, and link ownership.
7. With explicit user approval, review `docs/reference.md` after behavior
   stabilizes. No semantic edit is expected for the initial substitution; add
   no dependency history to the language reference.

## Validation

This section is exhaustive. The RFC is complete only when all items pass.

- All six initial target packs contain one pinned, statically linkable
  utf8proc archive, matching headers, license material, build flags, digest,
  target identity, library version, Unicode version, and ABI evidence.
- Every runnable target passes scalar iteration and encoding probes for ASCII,
  multibyte boundaries, surrogate rejection, overlong encodings, truncation,
  bare continuation bytes, and values above U+10FFFF.
- `hexal/string.h` and every module header expose no `utf8proc_*` name and do
  not include `<utf8proc.h>`.
- `hexal/string.c` uses `utf8proc_iterate` for scalar validation/decoding and
  `utf8proc_codepoint_valid` plus `utf8proc_encode_char` for scalar encoding.
- Every `utf8proc_iterate` call receives a non-negative length no greater than
  four; no arbitrary `size_t` String length narrows to `utf8proc_ssize_t`.
- The superseded lead-byte, continuation-byte, overlong, surrogate, maximum-
  scalar, decode, and encode formulas no longer exist in generated templates.
- String, Strand, Rune, and RuneCursor generated representations remain
  byte-identical.
- Valid text produces the same Rune sequence, Rune counts, byte Views, slice
  boundaries, cursor state, equality, ordering, hashing, printing, and
  interpolation behavior as before.
- Every malformed sequence retains exactly
  `[Runtime Error] invalid UTF-8 in string\n`.
- Invalid Rune construction retains exactly
  `[Runtime Error] invalid Unicode scalar value\n`.
- `String.from_runes` retains checked size arithmetic, one Hexal-owned
  allocation, and one trailing NUL and performs no utf8proc allocation.
- No implicit normalization, case folding, grapheme segmentation, category
  mapping, or display-width transformation occurs.
- A canonically equivalent but byte-distinct String pair remains unequal and
  retains byte ordering.
- A program without `hexal/string.c` selects no utf8proc dependency and keeps
  existing generated artifacts byte-identical.
- A non-ASCII literal alone does not broaden component demand.
- Ordinary `go test ./...` requires no installed utf8proc or C toolchain.
- External C23 fixtures compile, link, and run with the pinned archive.
- Manifest changes are confined to artifacts selecting `hexal/string.c` and
  are reviewed by artifact family.

## Open implementation inputs

- Exact utf8proc release and Unicode-data version.
- Per-target static-build definitions and archive form.
- Driver dependency-metadata representation.
- Binary-size and performance measurement record.

These inputs do not reopen String representation, bytewise comparison,
explicit-only normalization, private dependency types, or use of
non-allocating scalar APIs.

## Implementation readiness

The architecture is ready for dependency qualification. Code implementation is
blocked on selecting and qualifying the pinned release in RFCs 0052 and 0055.
No existing language-surface decision remains; future Unicode APIs require
separate focused specifications.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. During implementation, and
only with explicit user approval, verify that the existing String, Strand,
Rune, RuneCursor, byte-comparison, and malformed-input rules remain exact. The
backend library and Unicode package instructions do not belong in the language
reference.
