# RFC 0234: Regex Support via PCRE2

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started. The PCRE2 engine,
  dependency boundary, v1 API, capture model, UTF behavior, ownership, JIT
  exclusion, error shape, resource policy, exhaustive Validation, and phased
  implementation plan are settled
- Created: 2026-09-22
- Updated: 2026-09-28
- Origin: requested regex support by integrating PCRE2 v10.48
- Depends on: RFC 0052 (C backend), RFC 0055 (build and runtime-pack inputs),
  and the current String contract in `docs/reference.md`
- Coordinates with: RFC 0227 (utf8proc) and RFC 0233 (yyjson), the sibling
  vendored dependencies this RFC mirrors in layout and boundary
- Updates `docs/reference.md`: yes — one `std/regex` module row and a regex
  section carrying the settled surface and limits. Grammar
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

Hexal accepts PCRE2's backtracking semantics behind compiler-owned source-
pattern, compiled-pattern, match-work, depth, and heap limits. It adds no
arbitrary subject-length ceiling: the subject is already an allocated String,
while the match-work and heap limits bound the additional work caused by it.
Exceeding a limit returns `ResourceExhausted`; it never silently changes
matching semantics. JIT is
compiled out in v1: the interpreter is the one execution path on every target,
which keeps memory, thread-safety, qualification, and diagnostics uniform.

The Hexal surface is the `std/regex` module defined below. A private
generated adapter is the only code including `pcre2.h` or naming a
`pcre2_*` symbol; no PCRE2 type, option flag, or error code appears in Hexal
or in any public generated header.

## Why this dependency

A correct regex engine is the largest algorithmic component Hexal could
otherwise never reasonably hand-write: pattern compilation, backtracking with
capture state, Unicode property classes under UCP, and — since 10.48 —
security fixes for malformed patterns and invalid-UTF input that a
self-written engine would absorb one advisory at a time. One pinned backend
supplies compile and match/capture execution; Hexal keeps the
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
dependency: source commit, target-specific compile command, archive size,
SHA-256. Linux and Windows use their own existing pack compiler, target, PIC,
threading, and archiver identity; one host command is not copied across both.
Every adapter translation unit defines `PCRE2_CODE_UNIT_WIDTH 8` before
including `pcre2.h`. Two
upstream build facts are Phase 0 checks, not assumptions: the sljit
submodule/checkouts the source tree requires, and the exact mechanism that
selects the 8-bit width when compiling the library sources. The archive is
built with the pack's existing target-specific identity and with JIT disabled.

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
siblings, `system_libraries` empty unless the combined-archive probe proves
otherwise, `format_version` stays 1, release version in the directory name.
Append `pcre2` to the authoritative
`compiler/specdata/dependencyRegistry`; the driver derives validation and link
order from that registry. Add the entry and real payload hashes to both packs
in the same change. Do not recreate a driver-local list, alphabetically reorder
existing entries, or make this RFC's implementation order depend on RFC 0233.

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

Every reachable `compile`, `test`, `find`, `capture`, or `free` operation
selects `pcre2`; naming only `Span`, `Match`, or `Pattern`, and `free_match`
alone, selects no dependency because it only releases the Hexal capture List.
Programs with no adapter operation keep byte-identical artifacts.
Selection is program-wide, deterministic, static-link only, and contributes
to the build identity.

## Language surface

`std/regex` exports three types and six module functions:

```hexal
type Span is struct
    start: Size,
    end: Size,
end

type Match is struct
    whole: Span,
    captures: List<Span | Nil>,
end

Regex.compile(heap: Heap, source: String) -> Pattern | Error
Regex.test(heap: Heap, pattern: Pattern, subject: String) -> Bool | Error
Regex.find(heap: Heap, pattern: Pattern, subject: String) -> Span | Nil | Error
Regex.capture(heap: Heap, pattern: Pattern, subject: String) -> Match | Nil | Error
Regex.free(heap: Heap, pattern: Pattern)
Regex.free_match(heap: Heap, match: Match)
```

`Pattern` is the owning compiled PCRE2 program. `Span` is a half-open UTF-8
byte range into the subject. `find` returns only the whole-match span and
allocates no Hexal capture List. `capture` returns the whole span plus one
entry for each numbered capture group, excluding the whole match; an optional
group that did not participate is `Nil`. It allocates only the capture List,
never copies subject substrings, and `free_match` releases that List. Named
capture lookup, repeated-match iteration, replacement, split, and escaping are
deferred rather than widening v1.

The following contracts apply:

- `pattern` and subjects enter as validated UTF-8 `String` (or the explicit
  `copy(heap)` route from `String<N>`); no raw-pointer or `Slice<Byte>` entry.
- `Pattern` owns a heap allocation, follows the owning-type rules (handle
  semantics, one release, aliasing after free), and has an explicit cleanup
  operation like every other owning type.
- Failures are Hexal-owned `Error`s. Pattern-compilation failure includes the
  normalized byte offset reported by PCRE2 and a stable Hexal-owned reason
  category. Native error codes and dependency-owned English strings never
  cross the boundary. The fallback is `invalid regular expression at byte N`.
- `PCRE2_UTF | PCRE2_UCP` are always enabled. There is no byte-mode regex.
  Compile options use PCRE2's pattern-embedded forms such as `(?im)`; v1 adds
  no flags type or parallel options API.
- Match calls allocate private PCRE2 call state from the supplied Heap and
  release it before return. They never retain the subject. Separate per-call
  state makes concurrent read-only use of one `Pattern` safe.
- `Pattern` and `Match` have no equality, ordering, printing, or Dict-key
  eligibility. `Span` is an ordinary comparable value struct.
- No implicit compilation, caching, or matching anywhere: every compile and
  every match is written at its call site.

## Allocation and ownership boundary

PCRE2 runs on the caller's Heap through a `pcre2_general_context` whose
allocation slots adapt the existing Heap allocate-or-null and free primitives;
no C `malloc` participates. Compile releases its temporary general/compile
contexts after PCRE2 has created the owning code object; Phase 0 verifies the
code object's documented allocator metadata and `pcre2_code_free` path. Each
test/find/capture call creates and releases its own match data and match context
through the supplied Heap. Capture spans are
copied into an ordinary Hexal List and no PCRE2 pointer becomes a Hexal value.
Every PCRE2 allocation is released through its creating context on success and
failure, so the cross-allocator rule holds by construction.

## Resource limits and configuration ownership

Every PCRE2 policy value lives exactly once in `compiler/config`; neither the
checker, generator, generated adapter, driver, nor runtime-pack scripts may
repeat a numeric literal. The generator renders the required C constants from
these Go values, and the adapter passes them to PCRE2's context setters. The
runtime-pack identity records the resulting library configuration.

```go
const (
	RegexMaxPatternBytes         = 64 << 10
	RegexMaxCompiledPatternBytes = 64 << 10
	RegexMaxParenthesisDepth     = 250
	RegexMatchLimit              = 10_000_000
	RegexMatchDepthLimit         = 10_000
	RegexMatchHeapLimitKiB       = 8 << 10
)
```

They are compiler configuration, not language arguments or per-call options:

| Limit | Value | Enforcement | Rationale |
| --- | ---: | --- | --- |
| source pattern | 64 KiB of UTF-8 code units | `pcre2_set_max_pattern_length` | A regular expression approaching 64 KiB is already outside ordinary program use. The bound prevents attacker-controlled input from requesting unbounded compiler work. |
| compiled pattern | 64 KiB | `pcre2_set_max_pattern_compiled_length` | The 8-bit library's default two-byte internal link size already limits compiled patterns to approximately this size. Hexal retains that smaller and faster representation instead of widening links for extreme patterns. |
| parenthesis nesting | 250 | `pcre2_set_parens_nest_limit` | This pins PCRE2's documented default, which exists to protect the system stack during compilation. |

Pattern directives such as `(*LIMIT_MATCH=...)`, `(*LIMIT_DEPTH=...)`, and
`(*LIMIT_HEAP=...)` may reduce Hexal's limits but can never increase them.
There is no independent subject-length limit. `String` and `PCRE2_SIZE` already
carry the subject length, and a second arbitrary ceiling would reject valid
searches without bounding any resource not already bounded by the match-work
and heap limits.

Every `test`, `find`, and `capture` call receives a 10,000,000-loop match
limit, 10,000 nested-backtracking depth limit, and 8 MiB PCRE2 heap limit.
The match count retains PCRE2's established default work allowance while the
finite depth and heap values replace defaults that are effectively unlimited
for Hexal's purposes. These are conservative safety ceilings, not measured
optima; adjacent implementation comments must not invent benchmark evidence
for them. A later measured workload may justify revising `compiler/config`
without changing the language API.

## Implementation plan

1. **Phase 0 — vendor and qualify.** Add the submodule at `pcre2-10.48`; build the
   8-bit archive separately with each target pack's recorded command shape;
   append the registry entry and add both pack payloads atomically; update
   `lib/BUILD.md`, license files, and the combined-archive probe; verify every
   PCRE2 API name against the vendored `pcre2.h`; retain the default two-byte
   internal link size and verify the source-length, compiled-length, and
   parenthesis-nesting setters.
2. **Phase 1 — central configuration and dependency plumbing.** Add the six
   `Regex*` constants to `compiler/config/config.go`. Append `pcre2` to
   `compiler/specdata/dependencyRegistry`, add its logical compiler dependency
   fact and demand predicate, extend manifest validation and `hexal doctor`,
   and prove that this plumbing alone moves no generated artifact.
3. **Phase 2 — checked language surface.** Add the compiler-owned `Pattern`,
   `Span`, and `Match` identities and the six `std/regex` declarations through
   the existing specdata/core-library registration path. Apply the stated
   storable, equality, printing, ordering, and Dict-key rules in the checker;
   add focused checker and full-pipeline tests for signatures and rejected
   operations.
4. **Phase 3 — private adapter.** Add `pcre2.c`/`pcre2.h` templates under the
   generated component package directory. Implement Heap-backed general,
   compile, and per-call match contexts; configure UTF/UCP and all six central
   limits; implement compile/test/find/capture/free; normalize compile errors;
   map no-match separately from engine/resource failures; and make every
   success and failure path release the context that allocated its objects.
5. **Phase 4 — demand and emission.** Select the component and logical
   dependency only for the operations in Dependency demand, emit one private
   adapter pair program-wide, keep PCRE2 declarations out of public headers,
   and verify deterministic ordering and byte-identical output for programs
   with no regex operation.
6. **Phase 5 — exhaustive tests.** Add ordinary textual/component tests for
   ownership, demand, includes, constants, and error mapping. Add integration
   programs for Unicode/UCP, embedded options, optional captures, byte spans,
   shared-pattern concurrency, and every rejection below. Add tagged C23
   fixtures for actual matching and each resource-limit result against every
   qualified pack. Rebuild the snippet manifest only for genuinely affected
   artifacts and review its family-level diff.
7. **Phase 6 — synchronize and hand off.** Update `docs/reference.md` once,
   remove the owning `docs/status.md` entry, run ordinary tests and vet, run
   the tagged C23 lane and pack probes, rebuild `bin/hexal`, and restart the
   workbench through `hexal play`.

## Validation

**Exhaustive.** This section is the complete definition of done:

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
- Every PCRE2 error surface is a Hexal-owned `Error` carrying no native code or
  dependency-owned English text. Compile failure includes the normalized byte
  offset; match/resource failures use stable Hexal-owned messages.
- A source pattern through 64 KiB is admitted to compilation subject to the
  independent compiled-size and syntax rules; the first byte beyond that
  limit returns `ResourceExhausted`. A pattern whose compiled representation
  exceeds 64 KiB and a pattern nested beyond 250 parentheses also return
  `ResourceExhausted` with stable Hexal-owned messages.
- Subjects have no separate Hexal length ceiling. A fixture with a subject
  larger than 64 KiB reaches matching rather than being rejected for size.
- Pattern-embedded `LIMIT_*` directives can lower but cannot raise the three
  selected per-match limits. Every operation passes 10,000,000 match steps,
  10,000 depth, and 8,192 KiB heap from `compiler/config` to PCRE2. Focused
  catastrophic-backtracking, nesting, and heap-heavy fixtures cross each
  boundary and return `ResourceExhausted` rather than hanging, trapping, or
  silently reporting no match.
- The six PCRE2 policy constants have one owner in `compiler/config`; generated
  C contains their rendered values but no independently maintained duplicate.
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

**Ready.** The dependency and allocation boundaries, language surface,
diagnostics, Unicode behavior, ownership, absence of an arbitrary subject cap,
six centrally owned limits, exhaustive Validation, and ordered implementation
work are settled. Start at Phase 0; no remaining design decision must be made
by the implementer.
