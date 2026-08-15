# RFC 0052: Target Profiles and Representation Evidence

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design not started
- Features: a named target profile, representation evidence beyond C constant
  expressions, and the trusted-metadata boundary for cross-compilation
- Created: 2026-08-13
- Updated: 2026-08-15
- Depends on: RFC 0003 (scalar layouts), RFC 0036 (`Size`), RFC 0037 (task
  runtime targets), RFC 0042 (layout queries), and RFC 0062 (supported
  toolchain contract; generated probes removed)
- Coordinates with: RFC 0039 (C interop, draft) and RFC 0049 item 6, which
  implements the `size_t` width this RFC generalizes

## Purpose

Several settled decisions assume a *target* exists as a first-class concept,
but nothing names or carries one.

- RFC 0036 requires the `size_t` width before type checking, and rejects widths
  other than 16, 32, or 64.
- RFC 0037 supports Windows x64 and POSIX x86-64 and must reject others.
- RFC 0042 makes `size_of`/`align_of` C constant expressions whose values come
  from the selected target.
- RFC 0062 replaced the generated-header verification of fixed-width integer,
  IEC float, and byte-width assumptions with a supported
  GCC-or-Clang-plus-C-library toolchain contract; only source-dependent
  target assertions (target-sized `Size` literals) remain in generated C.

Today each is a separate hardcoded assumption. RFC 0049 item 6 removed one of
them — the `size_t` width — by making Size fully target-driven instead of
introducing a profile field: Size lowers directly to the selected compiler's
`size_t`, conversions are portable, and generated C guards literal fits against
`SIZE_MAX`. This RFC generalizes target knowledge into one concept rather than
accumulating per-feature target flags.

## Scope

This RFC owns the "target probes and trusted target metadata for representation
evidence beyond C constant-expression checks" follow-up, which previously had no
spec home.

It does not implement anything. RFC 0049 item 6 made Size fully target-driven
and deliberately chose no compiler-side width selection; this RFC decides what
the profile becomes afterward.

## 1. What a profile must carry

At minimum, from existing decisions:

| Property | Required by | Today |
|---|---|---|
| `size_t` width | RFC 0036, RFC 0049 item 6 | none — Size lowers to the target `size_t`; generated C asserts literal fits against `SIZE_MAX` |
| pointer width and alignment | RFC 0042 layout queries | inferred from host C |
| scalar alignments | RFC 0042 | inferred from host C |
| IEC 60559 binary32/64 availability | RFC 0062 | supported-toolchain contract; no generated probe |
| endianness | RFC 0032 byte conversion | not needed — byte order is by significance, not host |
| OS and ABI family | RFC 0037 context backends | hardcoded probe |

Must settle: whether a profile is a closed enumeration of supported targets, an
open record of properties, or a name resolving to a record.

## 2. Evidence versus assertion

Two mechanisms exist for establishing a representation fact, and the boundary
between them is undecided.

**Generated C assertions.** `static_assert(sizeof(size_t) == 8, ...)` fails at
C compile time. Cheap and exact, but the Hexal checker has already finished by
then — it cannot use the result to make a checking decision.

**Compiler-side evidence.** The checker needs the width *before* checking any
`Size`-using source, per RFC 0036. That value must come from somewhere the
checker can read.

Must settle:

- which facts the checker must know before checking, versus which may be
  asserted at C compile time;
- whether the compiler may run a probe program to discover facts, which
  requires a C toolchain and conflicts with the rule that `go test ./...` needs
  none; and
- whether a declared profile is trusted without verification, and what happens
  when generated C then contradicts it.

The current answer is implicitly "trust the host, assert in C." That works only
for native builds.

## 3. Cross-compilation

RFC 0037 already states the requirement: a cross-compilation profile must name
a previously verified C23 thread runtime for its exact toolchain and target, or
task features fail before generated-program compilation.

Generalizing that: a cross profile cannot probe the host, so every fact must be
declared. Must settle what makes a declaration trustworthy — a signed manifest,
a verified-profile registry, or nothing beyond the user's word.

## 4. Interaction with C interop

RFC 0039 requires explicit trusted target and layout evidence for foreign
bindings without probing the host or requiring an external toolchain in
ordinary testing. That is the same question as item 2 here, arrived at from the
other side.

Neither RFC should answer it alone. Whichever is designed first should settle
the evidence model and the other should cite it.

## 5. Resolution so far (RFC 0062)

RFC 0062 settled the generated-assertion side of sections 1 and 2:

- Supported targets are a GCC-or-Clang plus compatible-C-library contract,
  qualified once outside generated source. Generic toolchain facts — 8-bit
  bytes, exact-width integer meanings, IEC 60559 binary32/binary64 — are not
  generated probes; `hexal.h` emits no target-profile assertions for them.
- The only retained generated assertion is source-dependent: a target-sized
  `Size` literal is guarded against the target's actual `SIZE_MAX`.
- This RFC therefore treats those representation facts as qualified target
  metadata (a build-driver/target-profile concern) and must not reintroduce
  them as generated C assertions. RFC 0055's future driver and this RFC's
  profile mechanism own toolchain selection and qualification.

## Non-goals

- General target-feature introspection exposed to Hexal source. `size_of` and
  `align_of` remain the only visible layout operations.
- A build system, toolchain discovery, or dependency management.
- Adding target families beyond RFC 0037's Windows x64 and POSIX x86-64.
- `ISize`, pointer-width integers, or address-space concepts.
- Changing byte-order rules. RFC 0032 defines conversion by significance, so
  host endianness stays invisible.

## Readiness

Not ready for design, and deliberately so. RFC 0049 item 6 will show what a
minimal profile actually needs; designing the general mechanism first would be
speculative. Revisit once that lands, or when RFC 0039 forces the evidence
question.

## Open questions

Everything in sections 1 through 4. This RFC records the problem and its owner,
not a solution.
