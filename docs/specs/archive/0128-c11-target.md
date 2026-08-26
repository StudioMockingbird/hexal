# RFC 0128: C11 Target

- Kind: Feature Specification (Rust-Style RFC)
- Status: Discarded; C23 retained, no implementation performed
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: retarget generated C from C23 to C11, and decide whether the latest-C
  policy in `AGENTS.md` survives contact with what that policy actually buys
- Depends on: `AGENTS.md`'s generated-C policy and `docs/reference.md`
- Coordinates with: RFC 0052 (target profiles), RFC 0055 (build driver), RFC
  0125 (external validation), and RFC 0127 (native threading primitives),
  which is independent of this one
- Changes no Hexal grammar, type, function signature, or result contract

## Summary

Generated C targets C23 because `AGENTS.md` says to use "the latest features
released for C, as long as the latest gcc & clang versions support that". A
measurement of what that policy currently buys finds that roughly seven in ten
C23 uses are spellings of things C11 already had.

Retargeting to C11 costs one mechanical substitution pass and one small
compatibility header. It gains the ability to compile under essentially every
gcc and clang shipped in the last decade, which is the guarantee a bundled
compiler was being considered to provide.

This RFC proposes that change and states the decision it forces.

## Disposition

Discarded. Hexal-generated translation units remain ISO C23. The proposed
fallback was not portable ISO C11: checked arithmetic, `typeof`, and
unreachable lowering became GCC/Clang extensions, excluding toolchains cited
as the portability rationale. With Hexal shipping a pinned compiler, linker,
and compatible C library, supporting older GCC/Clang versions does not justify
the compatibility layer.

ADR 0055 owns the bundled-toolchain rationale and foreign-source dialect
separation. RFC 0052 owns the minimum supported GCC/Clang versions and selected
C23 facility evidence. RFC 0125 enforces that floor. The measurement below is
retained as historical evidence, not as an implementation plan.

## Motivation

### What C23 is actually used for

Measured over `compiler/generator/packages/*.c`, `*.h`, and the Go generator:

| Feature | Sites | C11 equivalent | Semantic content |
|---|---|---|---|
| `nullptr` | 147 | `NULL` | none |
| `ckd_add` / `ckd_mul` / `ckd_sub` | 72 | `__builtin_*_overflow` | **real** |
| `constexpr` | 10 | `static const` at file scope | none |
| `static_assert` without include | 8 | `_Static_assert` | none |
| `[[noreturn]]` | 5 | `_Noreturn` | none |
| `typeof` | 4 | `__typeof__` | **real**, GNU-only |
| `unreachable()` | 1 | `__builtin_unreachable()` | **real** |
| `enum : uint8_t` | 1 | plain enum, explicit field width | **real** |
| `bool` / `true` / `false` keywords | ~10 | `<stdbool.h>` | none |

Nothing uses `#embed`, `auto`, `_BitInt`, binary literals, digit separators, or
any attribute other than `[[noreturn]]`.

`nullptr` alone is 59% of the total. Adding `constexpr`, `static_assert`,
`[[noreturn]]`, and the `bool` keywords brings the share of C23 usage carrying
no semantic content whatsoever to roughly 70%.

### What that means for the toolchain argument

The case for bundling a C compiler rests on the claim that Hexal's dialect is
too new to rely on what is installed. That claim is measurably weak. Four
features carry real content, and three of them have exact GNU builtins that gcc
and clang have supported for a decade.

C11 is the floor for MinGW-w64, `zig cc`, tcc, every supported gcc and clang,
and much of MSVC. C23 is recent enough that RFC 0125's own matrix needed
current releases of all three toolchains to compile anything.

A dialect requirement that is 70% cosmetic is a poor justification for
absorbing a hundred-megabyte dependency and a build-and-extract subsystem.
Bundling may still be worth doing — for reproducibility, hermetic
cross-compilation, or the linker — but it should be justified by those, not by
a spelling preference.

### What this RFC does not fix

`<threads.h>` availability. That header is optional in C11 and something an
implementation may omit, so no choice of `-std=` affects it. RFC 0127 owns that
fix, and the two RFCs are independent: either may land first, and neither
changes the other's work.

## Decision required

`AGENTS.md` states the latest-C policy as a rule. This RFC cannot silently
contradict it, so the decision is explicit and belongs to the project owner:

**Option A — retarget to C11.** Generated C targets `-std=c11`. The policy
sentence in `AGENTS.md` is rewritten to name C11 as the floor and to require
that any newer feature earn its place by carrying semantic content unavailable
in C11. The precise dialect is **GCC/Clang C11 with the two documented GNU
facilities below**, not implementation-independent ISO C11 and not MSVC C.

**Option B — stay on C23.** The policy stands, and the bundling RFC must
justify itself on reproducibility and cross-compilation alone, with the
measurement above recorded so the dialect argument is not reused.

The remainder of this RFC specifies Option A. It is written to be
implementation-ready the moment that option is chosen, and to be archived
unimplemented if Option B is.

## Open question

Choose exactly one:

- **A:** adopt the GCC/Clang C11 dialect specified by the remainder of this
  RFC, rename the external validation lifecycle to a standard-neutral name,
  and replace the latest-C policy; or
- **B:** retain C23, keep the current policy, and discard this RFC without code
  changes.

No implementation or manifest rebaseline starts before that choice.

## Substitutions

Each is mechanical and total. None is conditional on a compiler test.

- **`nullptr` to `NULL`.** 107 sites in templates, 40 in the Go generator.
  `NULL` comes from `<stddef.h>`. Demand that header wherever selected output
  spells `NULL`; pointer-only programs do not necessarily use `size_t`, so the
  existing Size-triggered demand is not sufficient.
- **`bool` / `true` / `false`.** Add `<stdbool.h>` to the include demand of
  every component that spells them. C99 and later provide it.
- **`static_assert` to `_Static_assert`.** Same semantics, same operands, no
  include.
- **`[[noreturn]]` to `_Noreturn`.** Same semantics; the placement moves to the
  declaration-specifier position.
- **`constexpr` to `static const`.** All ten are file-scope integer constants
  in `hexal/io.c`, read in comparisons, casts, and call arguments. None is used
  where an integer constant expression is required, so file-scope `static const`
  is exact. Implementation must confirm that property still holds rather than
  assume it: a `constexpr` that later reaches a case label or array bound needs
  `#define` or an enumerator instead. The fallback rule is exact: a value used
  in a case label, array bound, bit-field width, enumerator, or static assertion
  becomes a compiler-owned macro or enumerator rather than `static const`.
- **`unreachable()` to `__builtin_unreachable()`.** One site.

### Checked arithmetic

72 sites, and the only substitution needing a home. Add `hexal/checked.h`
providing:

```c
#define hex_ckd_add(result, left, right) __builtin_add_overflow((left), (right), (result))
#define hex_ckd_sub(result, left, right) __builtin_sub_overflow((left), (right), (result))
#define hex_ckd_mul(result, left, right) __builtin_mul_overflow((left), (right), (result))
```

Argument order matches `ckd_*`, so every call site changes name only. The
builtins are supported by gcc and clang, produce identical codegen to `ckd_*`,
and have been available far longer.

`<stdckdint.h>` is dropped from include demand and `hexal/checked.h` replaces
it wherever it was demanded.

This is the one place C11 costs something real: the header is a GNU extension
rather than a standard one. MSVC provides neither `ckd_*` nor the builtins, so
a future MSVC target needs hand-written pre-checks under either standard. That
cost is unchanged by this RFC and is not incurred by it.

### The enum with a fixed underlying type

`hex_io_status` is declared `enum hex_io_status : uint8_t` and stored as a
field in four result structs. C11 leaves an enum's underlying type
implementation-defined, so a plain enum would likely widen the field from one
byte to four and change every one of those layouts.

Declare the enum plainly and give each field an explicit `uint8_t` type,
keeping the enumerators as the source of the constants. Layout is preserved
exactly and no result contract moves.

### `typeof`

Four sites in the Go generator, wrapping the result type of a Fun-typed
declarator. `__typeof__` is the GNU spelling, supported by gcc and clang and
reserved, so it does not collide with a user identifier.

This is the one substitution that is not standard C11. It is accepted here
because the alternative — synthesizing a named typedef per Fun result shape —
is materially more generator machinery for no generated-code benefit. A future
MSVC target must revisit it, and the same is true today under C23.

## Non-goals

- Moving to C99. `<stdatomic.h>` and `_Atomic` have no C99 replacement, and
  `Atomic<T>` is a language-visible Hexal feature; C99 would require a
  hand-written per-platform atomics subsystem. `_Thread_local` and
  `_Static_assert` would also need replacing. C11 costs a substitution pass;
  C99 costs a subsystem. That line is not crossed.
- Removing `<stdatomic.h>`. It is C11, not C23, and every toolchain in RFC
  0125's matrix provides it.
- Fixing `<threads.h>`. RFC 0127 owns it, independently.
- Adding MSVC or `clang-cl` as a target. Two substitutions above note their
  MSVC consequences; neither introduces a new obstacle.
- Deciding whether to bundle a C compiler. This RFC supplies a measurement that
  bears on that decision and takes no position on it.
- Lowering any Hexal source construct differently. Only the C spelling of
  existing lowerings changes.

## Required sweep

- Replace every listed C23 spelling in `compiler/generator/packages/*.c`,
  `*.h`, and the Go generator. No C23-only spelling survives outside historical
  specs.
- Add `hexal/checked.h`; remove `<stdckdint.h>` from every include demand.
- Add `<stddef.h>` to every demand path that can spell `NULL`, including
  pointer-only output with no Size use.
- Add `<stdbool.h>` to the demand of every component spelling `bool`.
- Preserve every trap message, check order, and overflow behavior exactly.
  A substitution that changes when a trap fires is a defect, not a migration.
- Preserve `hex_io_status` field layout.
- Change the standard flag in RFC 0125's harness and in every documented
  compile command from `-std=c23` to `-std=c11`.
- Rename `compiler/tests/c23validation`, its package, build tag, helper names,
  and documented commands to a standard-neutral external-C validation
  lifecycle. Do not retain `c23` as a historical alias.
- Rewrite the generated-C policy sentence in `AGENTS.md` to name C11 as the
  floor and require newer features to carry semantic content.
- Keep runtime C in `compiler/generator/packages/*.c` and `*.h`, not Go
  strings.

## Implementation plan

### Phase 0: baseline and confirmation

1. Record the green test/vet baseline and snippet manifest.
2. Re-derive the feature table above against the current tree. A count that
   has moved changes the argument and must be corrected before proceeding.
3. Classify every `constexpr` use. Apply the macro/enumerator fallback above to
   any integer-constant-expression consumer rather than forcing `static const`.
4. Compile one generated program per family under `-std=c23` with each RFC 0125
   toolchain and keep the results as the before state.

### Phase 1: the compatibility header

1. Add `packages/checked.h` with the three macros.
2. Add its component model, include demand, and selection; it is demanded
   exactly where `<stdckdint.h>` was.
3. Add component tests for the macros, their argument order, and the absence of
   `<stdckdint.h>`.

### Phase 2: mechanical substitution

1. `nullptr` plus `<stddef.h>` demand, `bool` include demand, `static_assert`,
   `[[noreturn]]`,
   `constexpr`, `unreachable()`, `typeof`, in that order, each as its own
   reviewable step.
2. `ckd_*` to `hex_ckd_*` across all 72 sites.
3. `hex_io_status` and its four fields.

### Phase 3: standard flag and policy

1. Change `-std=c23` to `-std=c11` in the RFC 0125 harness and every documented
   command.
2. Rename the external suite, package, build tag, and every repository command
   to the standard-neutral name settled with RFC 0125.
3. Rewrite the `AGENTS.md` policy sentence.
4. Update `docs/reference.md` where it names C23 as the output standard.

### Phase 4: conformance

1. Implement every validation item below and no additional behavior.
2. Rebuild the snippet manifest once. Only artifacts that select a migrated
   spelling may move; every unaffected artifact hash remains byte-identical. A
   minimal Int32-only program is an explicit unchanged control.
3. Compile one generated program per family under `-std=c11` with each RFC 0125
   toolchain, on the host and on at least one cross target.
4. Run `gofmt`, `go test ./...`, `go vet ./...`, and the renamed external-suite
   vet command.
5. Rebuild and restart the workbench.
6. Remove this RFC from open status only after code, tests, artifacts, and
   canonical docs agree.

## Validation

This section is exhaustive.

### Compiler and generated-text validation

- No generated artifact contains `nullptr`, `constexpr`, `unreachable()`,
  `[[noreturn]]`, `<stdckdint.h>`, `ckd_add`, `ckd_mul`, `ckd_sub`, a bare
  `static_assert`, or an enum with a fixed underlying type.
- `NULL`, `_Static_assert`, `_Noreturn`, `static const`,
  `__builtin_unreachable`, and `__typeof__` appear in their place.
- Every artifact spelling `NULL` obtains `<stddef.h>` through its owning
  program/component include demand, including pointer-only programs.
- `_Noreturn` appears in the C11 declaration-specifier position before the
  function return type; a substring-only replacement does not satisfy this.
- `hexal/checked.h` is emitted exactly where `<stdckdint.h>` was demanded, and
  nowhere else.
- Every `hex_ckd_*` call has the same operand order and destination as the
  `ckd_*` call it replaced.
- Every component spelling `bool` demands `<stdbool.h>`.
- `hex_io_status` fields are `uint8_t`; `hex_io_open`, `hex_io_transfer`,
  `hex_io_position`, and `hex_io_status_only` keep their exact layouts.
- Every trap message and every check-before-allocate ordering is unchanged.
- `<stdatomic.h>` and `_Thread_local` are untouched.
- Repeated compilation produces byte-identical artifacts.
- Ordinary tests remain pure Go.
- `go test ./...`, `go vet ./...`, and the renamed external-suite vet command
  pass; no active package, tag, directory, or command retains `c23` as a stale
  lifecycle name.
- Manifest hashes for C11/C23-neutral artifacts remain unchanged.

### External compilation

- Every generated program family compiles and links under `-std=c11` with GCC,
  Clang, and `zig cc`, with no warning under `-Wall -Wextra -Werror`.
- At least one cross target compiles and links.
- A program that compiled under `-std=c23` before the migration compiles under
  `-std=c11` after it, for every family.

### Runtime behavior retained as a coverage gap

Ordinary tests cannot execute generated C:

- Overflow detection through `hex_ckd_*` traps on exactly the inputs `ckd_*`
  trapped on, with the same message.
- `__builtin_unreachable()` at the one migrated site is still unreachable.
- IO result values round-trip through the narrowed status field unchanged.

## Reference synchronization

After implementation, update `docs/reference.md` wherever it names C23 as the
generated output standard, and update `AGENTS.md`'s generated-C policy. Add no
substitution table to the reference: it records what the language means, not
how the generator spells it.
