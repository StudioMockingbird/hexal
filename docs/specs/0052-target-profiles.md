# RFC 0052: Target Profiles and Representation Evidence

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Features: a named target profile, representation evidence beyond C constant
  expressions, and the trusted-metadata boundary for cross-compilation
- Created: 2026-08-13
- Updated: 2026-08-26
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

## 4A. Minimum C/ABI profile contract

The profile is the evidence boundary for both native lowering and C
interoperability. A profile must identify a concrete target/ABI/toolchain
combination, not merely an operating-system name or CPU family.

At minimum, a usable C-interoperability profile carries:

- pointer size, alignment, and representation assumptions;
- `size_t` size, alignment, and maximum value;
- alignment and layout rules for every directly representable scalar;
- byte order;
- representation and calling rules for `bool`, fixed-width integers,
  floating-point values, pointers, enums, and function pointers;
- struct padding, alignment, bit-field, flexible-array, and union rules;
- supported calling conventions and symbol/linkage rules;
- atomic widths, lock-free guarantees where promised, and memory-order
  support;
- volatile/device-memory semantics;
- thread, TLS, signal, and process-entry capabilities;
- available C23 headers and builtins relied on by generated code;
- compiler family and version, with a minimum of GCC 15 or Clang 18 for the
  supported `-std=c23` frontend contract; a profile may require a newer version
  for its selected target or library;
- the operating-system facilities required by selected runtime components; and
- the fundamental alignment guaranteed by `malloc` and `calloc`, which must be
  at least the strictest alignment of any representation the profile admits.
  RFC 0123 removed the generated alignment argument from default allocation
  because no current Hexal type is over-aligned; a profile that admits an
  over-aligned representation must either supply aligned allocation or reject
  that representation, and cannot rely on a generated assertion to make the
  fact safe.

Unsupported or unverified facts are not silently assigned a host default. A
profile either supplies the fact or the compiler rejects the feature that needs
it. This is especially important for foreign records, foreign function
pointers, external variables, atomics, inline assembly, and raw unsafe layout.

The profile is selected by build-time configuration and is not visible as a
source-language value. The in-memory compiler consumes the selected profile's
trusted metadata through `Project`; filesystem discovery, compiler probing,
and profile installation belong to the driver. A zero-value `Project` may
select a documented native default, but cross-compilation must select a named
profile explicitly.

Generated C assertions are corroboration, not a substitute for checker input.
The checker must have every fact required to type-check and lower a program
before generation. A generated `_Static_assert` may detect that the selected
toolchain contradicts the profile, but it cannot make an unknown fact safe.

Profiles are immutable, versioned inputs. A generated artifact records the
profile identity used to produce it so that a driver cannot accidentally link
objects produced under incompatible layout or ABI evidence.

## 4B. Profile and unsafe-boundary interaction

Safe native code may use only representations whose profile contract is
complete. Unsafe foreign declarations may use additional target-specific
facts, but those facts must still be present in the profile; `unsafe` relaxes
the native checker, not the target ABI requirement.

Raw unions, bit fields, flexible array members, address integers, pointer
arithmetic, pointer casts, foreign globals, inline assembly, and compiler
extensions are therefore profile-qualified unsafe features. A profile that
does not describe one of them causes an Unsupported or ABI diagnostic before
generation.

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
- The supported frontend floor is GCC 15 or Clang 18. Version alone is not
  sufficient: the profile must also qualify every C23 header, builtin, target,
  runtime, and linker facility selected by the generated program. RFC 0055's
  bundled distribution provides the default qualified toolchain; an external
  override must satisfy the identical profile.

## 6. RFC 0069 evidence

RFC 0069 (C23-backed compiler simplification) assigned additional qualified
target facts that this RFC must record before that RFC closes:

- `<stdckdint.h>` availability: the pinned GCC and Clang distributions plus
  their selected compatible C library must provide conforming
  `ckd_add`/`ckd_sub`/`ckd_mul` behavior; Clang's resource header maps the
  macros to its checked-overflow builtins when the hosted library lacks the
  header. The core compiler emits no fallback definitions.
- Overflow-builtin stored result: `ckd_*` on an out-of-range signed result
  stores the unique value congruent modulo two to the destination width. This
  is the pinned GCC/Clang stored-result behavior (Clang documents it; GCC
  documents infinite-precision arithmetic followed by conversion), and it
  backs Hexal's signed wrapping lowering. C23 §7.20.1 paragraph 5 alone is
  defect-affected (WG14 C23 issue 1063) and is not presented as unqualified
  ISO portability.
- Same-width unsigned-to-signed conversion: the pinned GCC and Clang targets
  perform an out-of-range same-width conversion modulo the destination width,
  used by signed shift, bitwise, complement, and endian lowering. This is a
  narrower rule than RFC 0062's general contract and is not attributed to that
  closed RFC.
- Signed representation: Hexal's generated two's-complement reconstruction
  relies on the pinned compilers' modular same-width behavior; no generated
  probe asserts it.

The toolchain boundary stays: the core compiler performs no host probe, and
this RFC's profile mechanism plus RFC 0055's driver own qualifying and
reporting these facts.

## Non-goals

- General target-feature introspection exposed to Hexal source. `size_of` and
  `align_of` remain the only visible layout operations.
- A build system, toolchain discovery, or dependency management.
- Adding target families beyond RFC 0037's Windows x64 and POSIX x86-64.
- `ISize`, pointer-width integers, or address-space concepts.
- Changing byte-order rules. RFC 0032 defines conversion by significance, so
  host endianness stays invisible.

## Readiness

The minimum evidence set is proposed above. Implementation remains blocked
until RFC 0039 settles the foreign declaration grammar and RFC 0055 settles how
the driver selects, validates, and records profile identities. Do not add
host-probing fallback code to the core compiler while those boundaries remain
open.

## Open questions

The exact profile record schema, profile registry format, trust/signature
mechanism, native default selection, and the complete per-target capability
matrix remain open. This RFC now fixes the minimum evidence categories and the
core/driver trust boundary; the successor design must not remove those
categories without coordinating with RFC 0039.
