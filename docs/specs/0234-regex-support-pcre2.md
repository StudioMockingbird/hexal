# RFC 0234: Regex Support via PCRE2

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; skeleton only, implementation not started.
  **Deliberately under-specified**: the dependency boundary is settled because
  it copies an existing contract, while the language surface, capture model,
  and matching semantics are named but not decided — they are fleshed out in a
  later revision of this RFC, which also upgrades Validation to exhaustive and
  Status to Implementation ready
- Created: 2026-09-22
- Updated: 2026-09-22
- Origin: requested regex support by integrating PCRE2 v10.48
- Depends on: RFC 0052 (C backend), RFC 0055 (build and runtime-pack inputs),
  and the current String contract in `docs/reference.md`
- Coordinates with: RFC 0227 (utf8proc) and RFC 0233 (yyjson), the sibling
  vendored dependencies this RFC mirrors in layout and boundary
- Updates `docs/reference.md`: yes, eventually — one `std/regex`-shaped module
  row and a regex section, written when the surface is settled. Grammar
  unchanged: no new syntax
- Swept code: none. Nothing in the tree exists because regex support or a
  pcre2-facing defense was absent

## Decision summary

Hexal vendors one exact PCRE2 release as a target-qualified static library in
each shipped runtime pack — the same contract as libuv, mimalloc, utf8proc,
and yyjson: the compiler records a logical `pcre2` dependency, the build
driver materializes header, archive, and license from the selected pack and
links the archive, and the compiler never discovers, builds, or loads PCRE2.

The pinned release is PCRE2 **10.48** (2026-08-31): a security-fix release
carrying several GHSA-advisory fixes (out-of-bounds read/write, invalid-UTF
matching), matching-correctness fixes, and Unicode 17.0.
Only the **8-bit** library (`libpcre2-8`) is built; Hexal text is UTF-8 bytes,
and 16-/32-bit widths are not vendored.

The Hexal surface is a std module tentatively named `std/regex`. A private
generated adapter is the only code including `pcre2.h` or naming a
`pcre2_*` symbol; no PCRE2 type, option flag, or error code appears in Hexal
or in any public generated header. The exact surface — module name, function
set, capture/match result model — is the principal open area under Open
questions.

## Why this dependency

A correct regex engine is the largest algorithmic component Hexal could
otherwise never reasonably hand-write: pattern compilation, backtracking with
capture state, Unicode property classes under UCP, and — since 10.48 —
security fixes for malformed patterns and invalid-UTF input that a
self-written engine would absorb one advisory at a time. One pinned backend
supplies compile, match, and (provisionally) substitution; Hexal keeps the
value model, diagnostics, and allocation rules.

## Upstream qualification

```text
upstream:       https://github.com/PCRE2Project/pcre2
pinned release: 10.48 (tag pcre2-10.48, released 2026-08-31)
license:        BSD-2-Clause AND BSD-3-Clause WITH PCRE2-exception
static archive: pcre2.a (8-bit library only)
API header:     pcre2.h
Unicode:        17.0
```

Build facts are recorded in `lib/BUILD.md` as for every other pack
dependency: source commit, compile command, archive size, SHA-256. Two
upstream build facts are Phase 0 checks, not assumptions: the sljit
submodule/checkouts the source tree requires, and the exact mechanism that
selects the 8-bit width when compiling the library sources. The archive is
built with the pack's existing build identity (Clang, `-O2 -DNDEBUG -fPIC
-pthread`, target-portable). JIT is **not** decided — see Open questions.

## Runtime-pack layout

```text
lib/<hexal-target>/
  manifest.json
  pcre2_v10.48/
    include/pcre2.h
    pcre2.a
    LICENSE
```

One entry in the existing closed `dependencies` array, same shape as its
siblings, `system_libraries` empty unless the four-archive probe proves
otherwise, `format_version` stays 1, release version in the directory name.
The `validateRuntimeManifest` ordered list in `internal/driver/runpack.go`
grows to `libuv, mimalloc, utf8proc, pcre2, yyjson` (alphabetical) **in the
same change that adds the entry to both checked-in packs** — the list is
enforced on every manifest load, so a pack missing the name fails
structurally for every dependency-demanding program on that target.

No host path, environment value, system-installed PCRE2, pkg-config result,
or network lookup participates in compilation.

## Compiler and driver boundary

`Compile` may return the logical dependency fact `pcre2` and nothing else
changes: no vendor-tree reads, no C compilation, no host inspection, no PCRE2
source in generated files. The driver owns pack selection, manifest and hash
validation, materialization, link ordering, probes, and corrupt-input
reporting.

Generated public headers contain no `pcre2.h` include and no `pcre2_`
symbol; exactly one private generated adapter unit includes the header, and
Hexal source never names a PCRE2 function, type, or option. Existing C
interoperability is untouched: a user may still write their own
`from c <pcre2.h>` bindings through the ordinary foreign path.

## Dependency demand

Any reachable `std/regex` operation that crosses the adapter selects `pcre2`;
programs with no regex operation select nothing and keep byte-identical
artifacts. The precise per-operation table (e.g. whether a hypothetical
pattern-inspection-only function could avoid the dependency) belongs to the
flesh-out. Selection is program-wide, deterministic, static-link only, and
contributes to the build identity.

## Language surface (provisional)

Sketch only; every signature below may change:

```text
std/regex (name provisional) exports:

    Regex                               the compiled pattern, owning its
                                        pcre2 program
    compile(heap, pattern) -> Regex | Error
    ...match/search/capture/replace — undecided, see Open questions
```

Constraints the settled parts of the language already impose, regardless of
final shape:

- `pattern` and subjects enter as validated UTF-8 `String` (or the explicit
  `copy(heap)` route from `String<N>`); no raw-pointer or `Slice<Byte>` entry.
- `Regex` owns a heap allocation, follows the owning-type rules (handle
  semantics, one release, aliasing after free), and has an explicit cleanup
  operation like every other owning type.
- Failures are Hexal-owned `Error`s with fixed, allocation-free messages;
  PCRE2 error codes, English strings, and offset numbers never cross the
  boundary.
- No implicit compilation, caching, or matching anywhere: every compile and
  every match is written at its call site.

## Allocation and ownership boundary

Provisional direction, consistent with RFC 0233: PCRE2 runs on the caller's
Heap through a PCRE2 general context (`pcre2_general_context`) whose
allocation slots are the default Heap's entry points, so no C `malloc`
participates, allocation failure traps with the standard heap message, every
PCRE2 buffer is released through that context on every success and failure
path, and no PCRE2-allocated pointer ever becomes a Hexal value — RFC 0225
holds by construction. The final choice (general context vs. bounded
copy-out) is confirmed in the flesh-out.

## Open questions

The flesh-out decides, at minimum:

1. **Module and function set.** `std/regex` vs another name; the exact
   functions (compile, test, find, iterate, capture extraction,
   replace/substitute) and their signatures.
2. **Match and capture result model.** How a match reports: `Bool`, an ADT
   over match/no-match with payload, byte offsets as `Size` pairs, captured
   substrings as heap `String`s, named captures keyed how (Dict keys are
   `String<N>`; heap `String` is not a Dict key), and who frees what.
3. **Subject and offset semantics.** Whole string only vs start/end offsets,
   byte vs rune positions (text is byte-indexed today), iteration over
   successive matches including empty-match advancement rules.
4. **Unicode mode.** Whether `PCRE2_UTF` + `PCRE2_UCP` are always on for
   validated UTF-8 subjects (expected default) and what non-UTF byte-wise
   matching, if any, is exposed.
5. **Options surface.** Which compile/match options (caseless, multiline,
   dotall, ...) are exposed and how — fixed function parameters, flags value,
   pattern-embedded only — keeping the surface small.
6. **JIT.** On, off, or compile-time-excluded from the vendored build; if on,
   its thread-safety, memory context, and interaction with the 10.48
   match-mode security fix.
7. **Limits and hardening.** Match, depth, heap, and pattern-length limits
   against adversarial patterns and subjects; the fixed error messages and
   `ErrorKind` mapping for compile failure vs match failure vs limit
   exceeded.
8. **Substitution.** Whether `pcre2_substitute` is exposed at all in v1, and
   if so its replacement-string syntax (PCRE2's `$`/`\g` dialect) or a
   Hexal-owned replacement model.
9. **Value consequences.** Equality, ordering, printing, Dict-key
   eligibility (expected: owning handle, none of these) and thread-safety of
   concurrent matches through one `Regex` handle.
10. **Reference and validation.** The `docs/reference.md` section shape, and
    upgrading this RFC's Validation to the exhaustive definition of done.

## Implementation plan (coarse)

Fleshed out with the surface; the shape follows RFC 0227/0233:

1. **Phase 0 — vendor and qualify**: submodule at `pcre2-10.48`, build the
   8-bit archive, pack entries in both packs plus the ordered-list extension
   atomically, `lib/BUILD.md` record, license files, five-archive probe,
   verify every PCRE2 API name against the vendored `pcre2.h`.
2. **Phase 1 — dependency plumbing, no caller**: `RuntimePcre2` dependency
   fact, `pcre2Selected` predicate, `hexal doctor` probe extension; no
   artifact moves.
3. **Phase 2 — language surface**: the settled `std/regex` module, type, and
   functions plus the adapter; integration tests and snippets.
4. **Phase 3 — reference sync** and **Phase 4 — full gate** (`go test ./...`,
   `go vet`, tagged C23 suite, snippet-manifest rebuild with reviewed diff,
   pack probes, rebuild `hexal` and restart `hexal play`).

## Validation

**Not yet exhaustive** — this section becomes the complete definition of done
when the spec is fleshed out and its Status changes. The durable invariants
that survive whatever the surface becomes:

- Both shipped pack manifests declare `pcre2` in the closed ordered list with
  complete file hashes; the driver rejects missing, mismatched, corrupt, or
  target-incompatible payloads; the compiler needs no PCRE2 installation and
  performs no host discovery.
- A program with no regex operation selects no `pcre2` dependency and keeps
  every existing snippet hash; a regex program's dependency list contains
  `pcre2` and links.
- No public generated header contains a `pcre2` include or symbol; exactly
  one private adapter unit includes `pcre2.h`; generated C never calls
  `pcre2_*` elsewhere; no PCRE2 option flag or error code appears in Hexal.
- No PCRE2-allocated pointer is ever reachable as a Hexal value or passed to
  `Heap.free`.
- Every PCRE2 error surface is a Hexal-owned `Error` with a fixed,
  allocation-free message carrying no native code or offset number.
- The ordinary Go suite passes with no C toolchain installed; runtime
  behavior is verified by external C23 fixtures against each qualified pack.

## Open implementation inputs

Produced by Phase 0: exact source file list and sljit handling, the 8-bit
width-selection build mechanism, archive URL/size/SHA-256 and pinned commit
per pack, symbol/ABI evidence from the five-archive probe, size/link deltas,
license texts, and confirmation of every PCRE2 API the fleshed-out surface
calls. A signature or default differing from this RFC is a spec correction
before the dependent phase starts.

## Implementation readiness

**Not ready — skeleton by request.** The dependency boundary, pack layout,
and compiler/driver contract are settled because they copy the shipped
pattern verbatim; the language surface and matching semantics are open under
Open questions. Start the flesh-out there, then upgrade Status, pin the
surface, and make Validation exhaustive — Phase 0 may proceed independently
of the flesh-out, since qualifying the archive depends on nothing open.
