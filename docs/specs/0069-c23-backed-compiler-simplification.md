# RFC 0069: C23-Backed Compiler Simplification

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; base RFC and Amendments 1 and 2 implementation-ready.
  The deferred Heap finding requires its named follow-up decision.
- Created: 2026-08-15
- Updated: 2026-08-15
- Scope: generated runtime checked integer arithmetic (base RFC); signed
  wrapping, signed reconstruction, bit-cast, signed-right-shift, and trap
  lowering (Amendment 1); standard memory primitives, direct C operation
  lowering, centralized traps, and C23 null spelling (Amendment 2)
- Depends on: RFC 0062 (demand-driven `hexal.h`, and its GCC/Clang toolchain
  contract, which Amendment 1 Items A and B rely on directly)
- Coordinates with: RFC 0052 (target profiles), RFC 0055 (filesystem/build
  driver), `docs/reference.md`, generated-C fixtures, and workbench snippets

## Summary

Prefer a C23 facility over compiler-owned generated machinery whenever it
implements the exact Hexal contract. As the first bounded application of this
policy, replace hand-written generated overflow predicates with the C23 checked
integer macros from `<stdckdint.h>`:

```c
size_t total;
if (ckd_add(&total, offset, size)) {
    fputs("[Runtime Error] allocation size is not representable\n", stderr);
    abort();
}
```

Do not generate:

```c
size_t total = offset + size;
if (total < size) {
    /* trap */
}
```

This RFC covers arithmetic whose overflow must be detected. It does not change
Hexal arithmetic semantics. Amendment 1 re-lowers modulo-width wrapping
operators through the same standard facility, and Amendment 2 removes
hand-written byte loops and delegating wrappers where standard C already
implements the complete operation.

## Motivation

The generated runtime currently implements checked addition and multiplication
with several unrelated patterns:

- post-operation wrap comparisons;
- `SIZE_MAX - value` prechecks;
- `SIZE_MAX / value` prechecks; and
- unchecked compound allocation-size expressions.

These patterns duplicate a C23 facility, are easy to get wrong, obscure the
operation being protected, and distribute overflow policy across allocator,
String, List, Dict, and Channel generation.

Some existing expressions can overflow before a lower-level allocator sees
them. For example, passing `sizeof(header) + length + 1` into an allocator
cannot be repaired after the argument has already wrapped. The check must own
the complete arithmetic chain before the allocation call.

The base work also fixes two latent correctness defects:

- `align == 0` currently passes `(align & (align - 1)) != 0`, after which
  alignment rounding produces an offset of zero and lets the payload overwrite
  its allocation header. Zero alignment must trap before `align - 1` is
  evaluated.
- List and Dict capacity fields are `size_t`, but their generated growth
  temporaries are `uint64_t`. Keeping all capacity arithmetic in `size_t`
  preserves the language's target-sized `Size` contract on 32-bit targets.

`ckd_add`, `ckd_sub`, and `ckd_mul` perform the mathematical operation, store
the destination-width result, and report whether that mathematical result was
not representable. Current GCC and Clang C23 toolchains provide them through
`<stdckdint.h>`; Clang's resource header maps them to its checked-overflow
builtins when the selected hosted C library does not provide the header.

## Policy

1. Use a standard C23 facility when it exactly implements the generated
   runtime contract.
2. Do not reproduce that facility with generated predicates, compiler-owned C
   helpers, or target-width reasoning.
3. Keep the use local and explicit. Do not add an abstraction that only
   delegates to one `ckd_*` macro.
4. A C23 facility must be supported by Hexal's pinned GCC and Clang toolchains
   and compatible C library before it becomes required generated output.
5. Toolchain/library qualification belongs to RFC 0052 and the future driver;
   the core compiler remains string-in/string-out and performs no host probe.
6. Each later C23-backed simplification requires its own bounded specification
   or an explicit amendment to this RFC. This RFC is not authorization for an
   open-ended generator rewrite.

## Checked-arithmetic contract

Use:

```c
#include <stdckdint.h>
```

and the type-generic macros:

```text
ckd_add(result_pointer, left, right) -> bool overflow
ckd_sub(result_pointer, left, right) -> bool overflow
ckd_mul(result_pointer, left, right) -> bool overflow
```

Rules:

- The result object has the exact generated destination type.
- Each input expression evaluates exactly once.
- The macro call occurs before any use of its stored result.
- A `true` result follows the existing operation-specific trap or Error path.
- A `false` result continues with the stored value.
- Do not recompute the operation after a successful check.
- Do not inspect or use the stored wrapped value after an overflow path that
  must trap or return Error.
- Use an in-place result only when taking its address cannot alter input
  evaluation or aliasing semantics.
- Ignore a `ckd_*` return value only when wrapping is intentionally required.
  The base RFC introduces no such use; Amendment 1 Item A introduces the sole
  permitted use.

## Required replacements

The implementation must inventory every generated runtime arithmetic operation
whose overflow affects allocation size, capacity, indexing metadata, or a
runtime safety decision. At minimum, replace the following current families.

### Heap allocation

- Check header/alignment rounding with `ckd_add` before masking.
- Reject zero alignment explicitly before evaluating `align - 1`.
- Check `offset + size` with `ckd_add` before `malloc`.
- Preserve the existing allocation-size and allocation-failure diagnostics.

Required shape:

```c
size_t padded;
if (align == 0 || ckd_add(&padded, sizeof(hex_heap_header), align - 1)) {
    /* existing representability trap */
}
size_t offset = padded & ~(align - 1);

size_t total;
if (ckd_add(&total, offset, size)) {
    /* existing representability trap */
}
```

### String

- Check every `sizeof(storage) + payload + terminator` chain before calling the
  raw allocator.
- Use `ckd_add` while accumulating encoded UTF-8 byte length.
- Use `ckd_add` for concatenated byte lengths.
- Check the terminator and storage-header additions independently or as a
  sequential checked chain; no intermediate may wrap.
- Preserve existing invalid-UTF-8 and invalid-Rune diagnostics.
- Any payload accumulation, terminator, or storage-header overflow in
  `String.from_bytes` or `String.from_runes` traps with exactly
  `[Runtime Error] string allocation size overflow\n`.
- Any combined-payload, terminator, or storage-header overflow in String
  concatenation traps with exactly
  `[Runtime Error] string concatenation length overflow\n`.

### List

- Represent runtime length and capacity calculations as `size_t`.
- Use `ckd_mul` for capacity doubling.
- Use `ckd_mul` for `capacity * sizeof(element)` before allocation.
- Preserve the existing unrepresentable-capacity trap.

### Dict

- Represent runtime length and capacity calculations as `size_t`.
- Use `ckd_mul` for capacity doubling and bucket-region byte size.
- Check `length + 1`, load-factor multiplication, and every other arithmetic
  operand used to decide growth before comparison.
- An unrepresentable growth decision traps as unrepresentable capacity; it
  must not wrap and skip required growth.
- Preserve existing hashing, probing, replacement, and key-missing behavior.

### Channel

- Use `ckd_mul` for `capacity * element_size` before allocation.
- Preserve the existing constructor failure behavior and zero-capacity rule.
- Do not change scheduler, queue, send, receive, or close semantics.

### Inventory boundary

Heap, String, List, Dict, and Channel above are the complete current inventory
of hand-written generated checked add/subtract/multiply relevant to the base
RFC. Do not add another replacement during implementation without amending
this RFC with its operation, failure behavior, header requirement, and focused
tests. Ordinary Hexal wrapping arithmetic belongs only to Amendment 1.

## Header discovery

- `<stdckdint.h>` is a portable standard prerequisite discovered from the
  complete generated program under RFC 0062's umbrella-header model.
- Emit it in `hexal.h` exactly once when any reachable generated C or generated
  header uses `ckd_add`, `ckd_sub`, or `ckd_mul`.
- Do not emit it for a program with no selected checked arithmetic.
- Do not depend on another standard header including it transitively.
- The same semantic discovery that selects a `ckd_*` call selects the header;
  do not maintain an unrelated text scan.

## Toolchain boundary

- Generated output requires C23 mode.
- The pinned GCC and Clang distributions plus their selected compatible C
  library must provide conforming `<stdckdint.h>` behavior.
- The core compiler does not probe the host compiler or header set.
- RFC 0052 must eventually record header availability as qualified target
  evidence.
- RFC 0055 must compile generated Hexal C in C23 mode and report a missing or
  unusable standard header as a toolchain/target failure, not a Hexal source
  diagnostic.
- Do not emit a private fallback definition of `ckd_add`, `ckd_sub`, or
  `ckd_mul`. Toolchain qualification owns availability.

## What remains compiler-owned

`<stdckdint.h>` does not replace:

- division/remainder zero checks or signed `MIN / -1` behavior;
- shift-count range checks;
- checked numeric conversion range, Rune, NaN, or infinity validation;
- bounds, union-tag, UTF-8, ownership, allocation-failure, or scheduler checks;
  or
- compile-time constant diagnostics and folding.

The base RFC leaves the signed wrapping implementation unchanged. **Amendment 1
supersedes that exclusion** for wrapping `+`, `-`, `*`, and unary `-`; shift-count
range checks remain compiler-owned under both.

## Required tests

### Heap

- Alignment rounding uses `ckd_add` and rejects zero alignment.
- Allocation total uses `ckd_add`; the old post-wrap comparison is absent.
- Allocation-size and allocation-failure diagnostics remain distinct.

### String

- From-bytes checks storage header, payload, and terminator arithmetic.
- From-runes checks byte accumulation and total allocation size.
- Concatenation checks combined length and complete allocation size.
- No allocation argument contains an unchecked addition chain.
- From-bytes/from-runes overflow selects exactly the String allocation message;
  concatenation overflow at any stage selects exactly the concatenation
  message.

### List and Dict

- Growth doubling and byte sizing use `ckd_mul`.
- Dict growth-decision arithmetic cannot wrap before comparison.
- Existing growth and unrepresentable-capacity behavior remains selected.

### Channel

- Slot-region byte sizing uses `ckd_mul`.
- Constructor behavior and diagnostics remain unchanged.

### Discovery and regression

- A program selecting checked runtime arithmetic includes `<stdckdint.h>` once
  through `hexal.h`.
- A scalar-only program requiring none emits no `<stdckdint.h>`.
- Generated checked arithmetic contains no equivalent manual `SIZE_MAX -`,
  `SIZE_MAX /`, or post-wrap comparison for a replaced operation.
- Each `ckd_*` input expression evaluates once.
- Ordinary Hexal signed wrapping output is byte-for-byte unchanged except for
  unrelated header ordering forced by a selected runtime feature.
- Conversion, division, shift, bounds, UTF-8, ownership, and scheduler tests
  remain unchanged in behavior.

Ordinary tests remain pure Go and do not invoke GCC or Clang. Actual header
compilation belongs to the future toolchain lifecycle or a manual validation
pass, consistent with RFC 0062 and repository testing policy.

Before closure, record one manual GCC run and one manual Clang run in C23 mode
that compile and execute the generated signed wrapping helpers at every signed
width boundary and the signed-right-shift helpers at counts `0`, `1`, and
`width - 1`. This is release/toolchain validation, not an ordinary Go test and
not a new runnable C23 test package.

## Test and fixture updates

Update:

- allocator generator tests;
- String, Strand, List, Dict, Channel, Mutex, and Atomic
  generator/integration tests;
- bit-cast, equality/ordering, trap, union, and nullable-pointer tests;
- header-requirement discovery tests;
- exact generated-C expectations;
- workbench generated-C hashes; and
- dormant C23 canary source expectations where they inspect these helpers.

Do not create a new test package or runnable C23 tests.

## Reference synchronization

During implementation, update `docs/reference.md` to state:

- in the existing C23 output contract, `<stdckdint.h>` is selected
  demand-first through `hexal.h` when checked runtime arithmetic or a selected
  signed wrapping specialization uses it; do not restate the already-normative
  general rule that generated C uses an exact standard facility directly;
- the qualified GCC/Clang plus compatible-C-library target provides
  `<stdckdint.h>` and the pinned compilers' overflow builtins provide Hexal's
  required signed modulo-width stored result; do not claim that C23 §7.20.1
  alone resolves WG14 C23 issue 1063;
- same-width unsigned-to-signed casts used by signed shift, bitwise,
  complement, and endian lowering are modular under the qualified GCC/Clang
  target contract;
- standard byte copy, comparison, and initialization use `<string.h>` directly
  only where bytewise behavior is the complete semantic contract;
- generated Atomic operations use C23 `<stdatomic.h>` directly at their use
  sites except where compare-exchange needs by-value adaptation;
- generated diagnostic traps share one `[[noreturn]]` implementation without
  changing diagnostic text or Error selection; and
- compiler-owned null pointer constants use the C23 `nullptr` keyword.

Update the existing Generated artifact split rules in place:

- `hexal.h` owns the demand-driven signed wrapping operation/type helpers and
  the `hex_runtime_trap` declaration;
- module headers no longer claim delegating Atomic, Channel, or Mutex wrappers;
  they retain only adapters that perform representation, union, evaluation, or
  API work, including compare-exchange and free adapters; and
- the selected root module C file owns the one selected
  `hex_runtime_trap` definition.

The reference update is mandatory before this RFC closes. Do not document
individual generated helper names or reproduce implementation walkthroughs.

During the same implementation, update open RFC 0052 with the exact
`<stdckdint.h>`, overflow-builtin stored-result, modular
unsigned-to-signed-conversion, and signed-representation evidence. Update RFC
0055 with the corresponding C23 compiler-mode and toolchain-failure
responsibilities.

## Non-goals

- Changing Hexal arithmetic semantics. Amendment 1 changes how wrapping is
  *lowered*; the wrapping contract itself is unchanged and its boundary values
  are covered by regression tests.
- Adding an overflow-checking operator or public checked-integer type.
- Changing runtime diagnostic text or Error/trap selection.
- Using `-fwrapv`, `-ftrapv`, sanitizers, or optimizer behavior as semantics.
- Adding private compatibility implementations of C23 facilities.
- Target-conditional codegen from `<stdbit.h>` endianness macros or
  `__builtin_bswap*`. See the audit table below; RFC 0052 owns these.
- Further C23 or builtin substitution beyond the base RFC and Amendments 1 and
  2. Additional audit findings below are not implementation authority: the
  Heap finding is deferred and the conversion candidate is rejected.
- Collapsing the default-Heap runtime or changing allocation provenance and
  stale-pointer guarantees.
- Adopting `fromfp`/`ufromfp`; the audit below rejects them for checked
  conversion lowering.
- Invoking an external compiler from ordinary tests.

## Acceptance criteria

1. Selected generated runtime checked add/subtract/multiply operations use
   `ckd_add`, `ckd_sub`, or `ckd_mul` directly.
2. No replaced operation retains a manual overflow formula or delegating
   wrapper.
3. Complete allocation-size chains are checked before the allocation call.
4. Existing trap/Error selection and diagnostic text remain unchanged.
5. `<stdckdint.h>` is emitted exactly when required under RFC 0062 discovery.
6. Ordinary Hexal wrapping arithmetic and every unrelated safety contract are
   unchanged by the base RFC.
7. Every checked operand evaluates exactly once.
8. Tests and generated-C/workbench fixtures are updated.
9. `go test ./...`, `go vet ./...`, `go test -tags c23 ./...`, and
   `go vet -tags c23 ./...` pass without invoking an external toolchain.
10. `docs/reference.md`, RFC 0052, and RFC 0055 are synchronized before
    closure.

Additionally, for Amendment 1:

11. Signed wrapping `+`, `-`, `*`, and unary `-` lower through `ckd_*`; the
    implementation notes cite C23 §7.20.1 paragraph 5, WG14 C23 issue 1063,
    and the pinned GCC/Clang stored-result behavior (Item A).
12. `renderSignedReconstruct` no longer exists. Shift, bitwise, complement,
    and endian reconstruction emit direct casts; bit-cast copies directly
    into the destination object (Item B).
13. Generated wrapping results are identical at every signed width boundary.
14. Signed right shift uses an exact-width unsigned sign-fill mask and contains
    no out-of-range C shift; generated trap functions carry `[[noreturn]]`
    (Items B and C).
15. Manual GCC and Clang C23 validation executes the generated signed wrapping
    helpers at every signed width boundary and signed-right-shift helpers at
    counts `0`, `1`, and `width - 1`, and records the successful result;
    ordinary repository tests remain pure Go.

Additionally, for Amendment 2:

16. Selected List/String copies, String/Strand comparisons, Strand Dict-key
    comparisons, and Dict-region initialization use the exact standard memory
    operations defined in Item A; the replaced loops and delegating equality
    helpers are absent.
17. Atomic new/load/store/exchange/fetch, Channel close/length/capacity/
    is_closed, and Mutex lock/unlock lower directly as specified; only adapters
    with real representation, union, evaluation, or API work remain. Direct
    and deferred Mutex free each evaluate and pass the Heap argument once.
18. One selected `[[noreturn]]` runtime diagnostic trap preserves every
    existing message and replaces subsystem-specific `fputs`/`abort` helpers.
19. Compiler-owned null constants use `nullptr`, `NULL` is absent, and Nil does
    not select `<stddef.h>` without an actual `<stddef.h>` consumer.
20. `<string.h>`, `<stdio.h>`, and `<stdlib.h>` discovery follows the final
    operation/trap owners and remains demand-driven and program-wide.

## Amendment 1 — Signed scalar lowering and trap attributes

Added under Policy item 6, which permits extension by explicit amendment.

The base RFC replaces **checked** arithmetic, where overflow must trap and the
stored result is discarded. This amendment covers **wrapping** arithmetic,
where overflow is defined behavior and the stored result *is* the answer;
deletes shared signed reconstruction; fixes exact-width signed-right-shift
masking; simplifies bit-cast; and adds one trap attribute. The items share a
toolchain contract but may land as separately reviewable commits in Item B,
Item A, then Item C order.

### Evidence

`renderSignedReconstruct` (`compiler/generator/bitwise.go`) emits:

```c
((EXPR) <= (INT32_MAX) ? (int32_t)(EXPR) : INT32_MIN + (int32_t)((EXPR) - (INT32_MAX) - 1))
```

`EXPR` appears three times, so the pattern triples its operand's generated text
and compounds through nested arithmetic. It is reached from **seven** generator
call sites:

| Site | Operation |
|---|---|
| `renderSignedWrap` unary site in `render.go` | signed unary negation |
| `renderSignedWrap` binary site in `render.go` | signed `+`, `-`, `*` |
| signed-shift site in `bitwise.go` | signed right shift |
| signed-bitwise site in `bitwise.go` | signed bitwise operation |
| signed-complement site in `bitwise.go` | signed complement |
| `writeBitCastDefinitions` in `bitwise.go` | `bit_cast` |
| endian-conversion site in `bitwise.go` | endian conversion |

Every signed arithmetic, shift, bitwise, complement, bit-cast, and endian
operation in generated C therefore carries this ternary.

### Item A — `ckd_*` for signed wrapping `+`, `-`, `*`, unary `-`

Replace `renderSignedWrap`'s unsigned-intermediate construction with one
demand-driven `static inline` helper per selected operation/type pair:

```c
static inline int32_t hex_wrap_add_int32(int32_t a, int32_t b) {
    int32_t r;
    ckd_add(&r, a, b);
    return r;
}
```

Hexal's contract is modulo-width wrapping with defined two's-complement results;
the overflow flag is intentionally discarded.

**Toolchain confirmation:** C23 §7.20.1 paragraph 5 says that an overflowing
`ckd_*` operation returns `true` and assigns the mathematical result "wrapped
around to the width of `*result`." WG14 C23 issue 1063 records an open defect
in how that wording applies to an out-of-range signed result. Do not present
this use as unqualified ISO portability.

It is nevertheless exact under Hexal's pinned toolchain contract:

- Clang's `<stdckdint.h>` maps `ckd_*` to `__builtin_*_overflow`; Clang
  documents the stored result as the unique value congruent modulo two to the
  destination width.
- GCC implements `ckd_*` through the same overflow builtins. Its documentation
  defines infinite-precision arithmetic followed by conversion to the result
  type. RFC 0069 requires the pinned GCC target's same-width signed result to
  be modular and assigns the corresponding evidence to RFC 0052; RFC 0062 did
  not previously state that narrower rule.

Item A is implementation-ready for the supported GCC/Clang targets, not for an
arbitrary C23 implementation. RFC 0052 must record both facts before this RFC
closes.

Rules:

- Ignoring the `ckd_*` return value is permitted here and only here. This is the
  intentional-wrapping exception the base RFC's checked-arithmetic contract
  reserves.
- Binary `+`, `-`, and `*` use `ckd_add`, `ckd_sub`, and `ckd_mul`,
  respectively. Unary `-value` uses `ckd_sub(&result, 0, value)`.
- Emit only the selected `add`, `sub`, `mul`, and `neg` helpers for each of
  `Int8`, `Int16`, `Int32`, and `Int64`. The helpers are program-wide built-in
  specializations in `hexal.h`, after `<stdckdint.h>` is included; they do not
  belong to a source module.
- These helpers are the explicit exception to Policy item 3: they adapt a
  result-pointer macro and intentionally ignored overflow flag into an
  expression-valued Hexal operation. Checked runtime operations continue to
  call `ckd_*` locally without a wrapper.
- Each operand evaluates exactly once, as in the base RFC.
- Diagnostics, constant folding, and compile-time overflow rejection are
  unchanged; this item changes lowering only.
- No unsigned intermediate type is introduced or retained for these operations.

### Item B — Delete `renderSignedReconstruct`

`renderSignedReconstruct` exists to avoid the implementation-defined
out-of-range unsigned-to-signed conversion in C23 §6.3.1.3. RFC 0062
established the general supported-toolchain boundary:

> GCC or Clang plus the selected compatible C library form the supported C23
> toolchain contract.

RFC 0069 adds the narrower requirement that the pinned GCC and Clang targets
perform an out-of-range same-width unsigned-to-signed conversion modulo the
destination width. Do not attribute that specific rule to RFC 0062. RFC 0052
must record evidence for both compilers before closure.

Under that qualified contract, shift, bitwise, complement, and endian call
sites reduce to a plain cast to the signed type. Bit-cast copies directly from
the checked source object into the exact destination object:

```c
static inline int32_t hex_bitcast_float_int32(float value) {
    int32_t result;
    memcpy(&result, &value, sizeof(result));
    return result;
}
```

Bit-cast performs no signed-source cast, unsigned intermediate, signed
reconstruction, numeric conversion, or post-copy conversion. Its existing
same-width and fixed-representation eligibility checks remain unchanged.
`<string.h>` discovery remains demand-driven as it already is for bit-cast.
With this change, every caller is removed and `renderSignedReconstruct` is
deleted entirely within Amendment 1.

This follows RFC 0062's architecture: target facts are qualified once at the
toolchain boundary rather than re-derived in every generated expression. It
does not retroactively add the conversion fact to that closed RFC.

Item B depends on no C23 facility and is independent of Item A. It may land
first.

Rules:

- Signed shift, bitwise, complement, and endian conversion emit a direct cast
  to the signed C type. `writeBitCastDefinitions` uses the direct object-copy
  form above.
- Bit patterns, trap behavior, and evaluation order are unchanged except that
  signed Int64 right shift is corrected as specified below; all other changes
  alter only conversion spelling.
- Shift-count range checks, division guards, and every other check in those
  helpers are retained exactly.
- Signed right shift retains its explicit portable sign-fill algorithm, but
  replaces the current hard-coded `uint32_t` mask with the exact unsigned type
  paired with the operand. The `count == 0` branch remains separate, so no C
  shift uses a count equal to the type width. In particular, Int64 uses
  `uint64_t`; valid Int64 shifts must never shift a 32-bit `1u` by 32 or more.
- RFC 0052 must record modular unsigned-to-signed conversion as qualified target
  evidence, alongside the representation facts RFC 0062 assigned to the
  target-profile layer.

### Item C — `[[noreturn]]` on generated trap functions

Generated C contains 38 `fputs(...); abort();` trap sites and **zero** C23
attributes. `hex_numeric_trap` and its siblings are plain `static void`.

```c
[[noreturn]] static void hex_numeric_trap(void) { ... }
```

This lets the compiler drop unreachable code after trap calls and removes
"not all control paths return a value" diagnostics from the guarded helpers.
Amendment 2 supersedes the per-family example with one program-wide trap; this
item supplies the attribute and control-flow contract for that final helper.

Rules:

- Apply to generated functions that unconditionally terminate the process.
- Do not apply to functions that may return.
- Use the C23 `[[noreturn]]` attribute spelling, not `_Noreturn` or
  `<stdnoreturn.h>`.
- This item changes no runtime behavior and requires no header.

### Amendment tests

- Signed wrapping `+`, `-`, `*`, and unary `-` lower through `ckd_*` helpers,
  and no unsigned intermediate or reconstruction ternary remains for them.
- Wrapping results at every signed width boundary are unchanged: `INT32_MAX + 1`
  yields `INT32_MIN`, `INT32_MIN - 1` yields `INT32_MAX`, and the existing
  boundary tests pass with identical expected values.
- For each of Int8/16/32/64, manual GCC and Clang execution covers maximum plus
  one, minimum minus one, minimum negation, and an overflowing multiplication;
  every result equals the mathematical value modulo that type's width.
- `renderSignedReconstruct` no longer exists, and no generated expression
  repeats an operand three times for conversion purposes. `bit_cast` has no
  unsigned intermediate or signed-result conversion.
- Shift, bitwise, complement, `bit_cast`, and endian operations produce the
  existing Hexal contract values. Previously correct cases remain unchanged;
  signed Int64 right shift now avoids the current 32-bit-mask defect.
- Negative signed-right-shift tests cover counts `0`, `1`, and `width - 1` for
  every signed width; Int64 output contains no 32-bit sign-fill shift and no C
  shift by or beyond the promoted left operand's width.
- Generated trap functions carry `[[noreturn]]`; no returning function does.
- `<stdckdint.h>` discovery under RFC 0062 covers Item A's helpers.

## Amendment 2 — Standard memory and direct operation lowering

The generator still emits hand-written byte loops and specialization-specific
helpers for operations that ISO C already defines completely. This amendment
uses the standard operations directly and deletes wrappers whose only purpose
is to delegate to a standard or already-typed runtime operation.

This amendment changes generated-C structure only. It does not change Hexal
copy depth, equality, ordering, atomic memory order, Channel/Mutex behavior,
diagnostics, or evaluation order.

### Item A — `<string.h>` memory primitives

Use `memcpy`, `memcmp`, and `memset` where bytewise behavior is the complete
Hexal contract. Use the standard function name, not `__builtin_memcpy`,
`__builtin_memcmp`, or another compiler spelling; GCC and Clang already
recognize the standard calls as builtins.

#### Copying

- List growth shallow-copies its initialized prefix with `memcpy` after the
  destination byte count has passed the base RFC's checked arithmetic.
- Guard List relocation with `list->length != 0`; ISO C requires valid pointer
  arguments even when the copy count is zero, and an empty List may have a
  null data pointer.
- `String.from_bytes` copies the validated payload with `memcpy` when its
  length is nonzero.
- String concatenation copies the left and right byte ranges with separate
  `memcpy` calls when their respective lengths are nonzero. The newly
  allocated destination cannot overlap either immutable input.
- Do not replace a copy loop when source and destination can overlap; use
  `memmove` only if a future operation has overlapping-copy semantics.

#### Comparison

- String equality first compares byte lengths, then uses `memcmp` for the
  equal-length byte sequences. A zero-length String returns equal without
  calling `memcmp` on a possibly invalid pointer.
- String ordering uses `memcmp` over the shorter nonzero byte length, then
  compares lengths when that result is zero. The sign of `memcmp` is the
  ordering result; its magnitude is irrelevant.
- Strand equality and ordering use `memcmp(left.data, right.data, 32)`
  directly. Strand's NUL-free payload and mandatory zero-filled tail make the
  complete 32-byte representation canonical and lexicographically ordered.
- Delete the global Strand equality and ordering helpers; emit the direct
  `memcmp` expression at top-level comparisons and inside recursive semantic
  equality bodies. String helpers remain because they perform length and
  zero-length handling in addition to `memcmp`.
- Dict probing with a Strand key compares the two 32-byte key arrays directly
  with `memcmp`; do not emit one per-Dict Strand equality wrapper.
- Recursive object, ADT, union, Array, View, List, and Dict value equality
  remains semantic and member/element-wise. Do not use storage `memcmp` for
  values that can contain padding, NaNs, pointers, unused union bytes, or
  capacity/backing-storage state.

#### Initialization

- A newly allocated Dict bucket region is initialized with `memset(region, 0,
  byte_count)` after the byte count is checked and allocation succeeds.
- An inactive bucket's key and value are never read. Insertion fully assigns
  them before setting `active`; zeroing those bytes therefore adds no value
  semantics.
- Do not generalize zero-byte initialization to arbitrary Hexal values. It is
  valid here because the region is private Dict storage whose inactive fields
  are unobservable.

#### Header discovery

- `<string.h>` is selected program-wide whenever any emitted `memcpy`,
  `memcmp`, or `memset` use requires it.
- Equality and Dict discovery contribute the requirement when String/Strand
  comparison is emitted; List/String discovery contributes it for copying;
  Dict growth contributes it for initialization. Bit-cast's existing
  `<string.h>` requirement is owned by Amendment 1 Item B.
- Emit `<string.h>` once through `hexal.h`; do not depend on another standard
  header declaring these functions.

### Item B — Direct Atomic, Channel, and Mutex operations

Delete specialization helpers that only forward already-typed operands to a
standard or runtime function.

#### Atomic

- Retain only the `typedef _Atomic(T) hex_atomic_*` declarations and any
  adaptation that cannot be expressed directly.
- `Atomic<T>.new(initial)` lowers to the checked initializer value itself; the
  destination `_Atomic(T)` binding or member performs the initialization.
- Lower `load`, `store`, `exchange`, `fetch_add`, and `fetch_sub` directly to
  `atomic_*_explicit` with `memory_order_seq_cst`.
- Preserve one compare-exchange adaptation because Hexal accepts `expected`
  by value while C requires a writable pointer and may replace its pointee on
  failure. The adaptation evaluates receiver, expected, and desired once and
  exposes only the Bool result.
- Do not introduce `_Generic`, `typeof`, or an Atomic wrapper that only calls
  the corresponding standard operation.

#### Channel

- Lower `close`, `length`, `capacity`, and `is_closed` directly to their
  non-generic `hex_chan_*` core operations.
- Retain the typed `new`, `send`, and `receive` adapters because they pass
  element storage and construct Hexal result unions.
- Retain `free` adaptation until its Heap argument and allocator contract are
  resolved by the deferred Heap decision below; do not drop evaluation of a
  source argument merely because the current runtime ignores its identity.

#### Mutex

- Lower `lock` and `unlock` directly to the scheduler-aware `hex_mutex_*` core
  operations.
- Retain constructor/Error adaptation.
- Retain `free` adaptation until the Heap argument and allocator contract are
  resolved with Channel and Heap allocation. The adapter must evaluate and
  accept the source Heap identity even though the current runtime ignores it:

```c
static inline void hex_mutex_free_hex_mutex(
    uintptr_t heap_identity,
    hex_mutex *mutex
) {
    (void)heap_identity;
    hex_mutex_free(mutex);
}
```

- Direct `mutex.free(heap)` renders both receiver and Heap exactly once.
  Deferred cleanup captures both at registration and passes both captured
  values to the same adapter. Do not preserve the current direct-call bug that
  drops the checked Heap argument.

### Item C — One generated runtime trap

Amendment 1 marks non-returning trap functions with `[[noreturn]]`. The final
form is one demand-driven program-wide diagnostic trap, not one helper family
per subsystem:

```c
[[noreturn]] void hex_runtime_trap(const char *message) {
    fputs(message, stderr);
    abort();
}
```

Rules:

- When any reachable generated code has a diagnostic trap path, declare it
  once in `hexal.h` and define it once in the selected root module C file.
  Do not place a private definition in every translation unit.
- Bounds, allocation, arithmetic, conversion, UTF-8, print, and live-resource
  diagnostic traps call it directly with their existing exact message.
- Scheduler-fatal constants call it with the complete existing prefix,
  message, and newline. Delete `hex_sched_fatal`; do not move its three-write
  formatting into the shared trap or change any resulting bytes.
- Delete `hex_numeric_trap`, `hex_print_failure`, and any other function whose
  only behavior is the same `fputs`/`abort` sequence.
- Do not route Error-returning paths through it.
- An impossible compiler-internal union tag may retain direct `abort()`; it is
  not a user runtime diagnostic. Remove a second `abort()` after a switch whose
  `default` already cannot return.
- The definition owns `<stdio.h>` and `<stdlib.h>` requirements. A family that
  only calls it does not independently claim those headers; RFC 0062 discovery
  selects the trap declaration, root definition, and prerequisites from
  semantic state.

### Item D — Uniform C23 null spelling

- Emit `nullptr` for every compiler-owned null pointer constant, including
  runtime state, comparisons, initializers, scheduler arguments, and platform
  API arguments. Do not emit `NULL`.
- Nil remains tag-only in general unions and the null niche in nullable
  pointer unions. Its internal `CName` may remain a mangling component; that
  string does not itself require a C declaration.
- Remove `<stddef.h>` requirements contributed solely by Nil, `NULL`, or a
  non-emitted `nullptr_t` spelling. Retain `<stddef.h>` for `size_t`,
  `max_align_t`, `offsetof`, or another actual declaration.
- This item does not change pointer nullability, truthiness, equality, or C
  interoperability.

### Amendment 2 tests

- List growth uses guarded `memcpy`; empty growth never passes a null source
  pointer to a standard memory function.
- String construction and concatenation use guarded `memcpy`; UTF-8 validation
  and checked allocation sizing are unchanged.
- String equality/ordering and Strand equality/ordering produce identical
  results through `memcmp`, including empty, prefix, non-ASCII UTF-8, maximum
  Strand payload, and differing payload lengths with canonical zero-filled
  tails.
- Strand Dict probing uses direct `memcmp` and emits no per-Dict key-equality
  wrapper.
- Dict growth uses `memset` only for a fresh inactive bucket region; insert,
  replace, remove, rehash, and missing-key behavior are unchanged.
- Atomic new/load/store/exchange/fetch operations contain direct standard
  operations and no delegating helpers. Compare-exchange retains by-value
  expected semantics and single evaluation.
- Channel close/length/capacity/is_closed and Mutex lock/unlock call their core
  operations directly; typed union/storage adapters remain.
- Direct and deferred Mutex free evaluate the Heap argument once and pass its
  identity to the retained adapter; neither path drops the argument.
- Exactly one selected `hex_runtime_trap` declaration and root definition
  exist, both carry `[[noreturn]]`, and every diagnostic is preserved
  byte-for-byte. No redundant post-switch `abort()` remains.
- Generated compiler-owned null constants use `nullptr`; `NULL` is absent.
- Nil alone does not select `<stddef.h>`; each remaining `<stddef.h>` inclusion
  has an actual declaration or macro consumer.
- `<string.h>` is emitted exactly once when a selected memory operation needs
  it and is absent when no selected generated code uses it.
- Ordinary tests remain pure Go; dormant C23 canaries remain dormant.

## Additional audit findings

These findings are recorded here but are not authorized implementation work in
this RFC. The Heap finding changes a contract and requires a follow-up ADR;
the conversion candidate is rejected and exposes a separate bug for a focused
conversion RFC.

### Default-Heap runtime collapse

The current Heap generator carries allocator identity, payload size, alignment
offset, an offset marker, and a live flag through approximately 47 generator
sites. Only the default allocator exists: `Heap.new()` performs no allocation,
all identities are equivalent, and Channel/Mutex already allocate with the C
runtime while ignoring the supplied Heap identity.

All currently generated heap-eligible types have fundamental alignment: Hexal
has no `alignas` surface, and direct/transitive Atomic heap allocation is
rejected. ISO C guarantees `malloc` storage is suitable for every fundamental
alignment. A future ADR should therefore decide whether the current runtime is
reduced to direct checked `malloc(size)`/`free(pointer)` while the public Heap
argument remains.

Potential deletion if approved:

- `hex_heap_header`, `size`, `offset`, `marker`, and `live`;
- raw allocator `allocator` and `align` parameters;
- alignment rounding and header recovery;
- List/Dict allocator fields and default-allocator mismatch checks; and
- String's duplicate header-recovery/free body, which should in any event
  delegate to the single raw free operation.

This decision must address all of the following before implementation:

1. RFC 0027 will later introduce Arena/Pool allocation; accepting this cleanup
   means reintroducing a real allocator representation only when that feature
   lands.
2. `h.free(ref stackValue)` currently passes the checker because `free` proves
   pointer type but not allocation provenance. Header probing before a stack
   pointer is undefined behavior; a simpler `free` cannot solve it either.
3. The current `live` check does not safely diagnose a second free: it reads
   metadata after the allocation was released, which is already undefined
   behavior. Retaining the field does not provide the claimed safety.
4. If Hexal requires every stale/foreign-pointer deallocation to trap, it needs
   static ownership/provenance or a live allocation registry. Both expand the
   implementation and belong outside this simplification RFC.
5. If the language instead leaves stale or non-allocation deallocation outside
   its runtime guarantees, `docs/reference.md` must say so precisely before the
   metadata is removed.

### C23 `fromfp`/`ufromfp`: rejected for checked conversion

C23 §7.12.9.10 does not give these functions a directly testable success
result. An infinite, NaN, zero-width, or out-of-range input causes a domain
error and returns an unspecified integer value. Reliably detecting that error
requires `errno` and/or floating-environment management according to the
target's `math_errhandling`; it can also disturb ambient floating-point state.
That is more machinery than an exact range guard followed by a cast.

Do not use `fromfp` or `ufromfp` for Hexal's checked Float-to-integer
conversion. They also require target-libc qualification because they are
runtime library functions rather than frontend-only facilities.

The audit nevertheless found a separate correctness defect. The current
`writeFloatToIntegerConversion` compares a floating value with integer limit
macros. For 64-bit destinations, converting `INT64_MAX` or `UINT64_MAX` to
Float64 rounds the bound to `2^63` or `2^64`; that can admit the first
out-of-range value before an undefined C cast. Fix this in a separate
conversion RFC with exact power-of-two floating thresholds and an explicit
rule for negative fractional input to an unsigned destination. Do not retain
rounded integer-limit comparisons and do not fold that semantic decision into
this simplification RFC.

### Small changes not selected

- Direct `fprintf` could replace numeric `snprintf` buffers followed by
  `fwrite`, but it removes little generator logic and must first prove identical
  formatting, locale, write-failure, and sequencing behavior.
- `memchr` could locate Strand's mandatory terminator, but it replaces only one
  bounded loop and does not justify another special lowering rule.
- C23 `calloc` detects multiplication overflow for Channel slots, but it also
  zeroes a potentially large region that Channel overwrites before reading.
  Keep checked sizing plus `malloc`; do not trade runtime work for a few source
  lines.

### Checked and rejected

Recorded so they are not reproposed. This closes the C23/builtin audit for the
current generator; anything further needs new evidence.

| Candidate | Finding |
|---|---|
| `__builtin_bswap16/32/64` for endian | Generated C is already portable and both compilers pattern-match it. Only the generator's Go loop is verbose. Defer to RFC 0052, which owns target-conditional codegen. |
| `<stdbit.h>` `__STDC_ENDIAN_NATIVE__` | Would reduce `to_le_bytes` to `memcpy` on matching targets, but that is target-conditional output — RFC 0052. |
| `<stdbit.h>` bit utilities | Nothing to replace: Hexal exposes no popcount, leading-zeros, or bit-width operation. Recorded as a rule — if such operations are ever added, they map to `<stdbit.h>` rather than hand-rolled loops. |
| `<uchar.h>` for UTF-8 | `mbrtoc8` is locale-dependent. `hex_utf8_next` stays compiler-owned. |
| C23 `bit_cast` | Does not exist in C; that is C++20. `memcpy` remains the canonical idiom and both compilers elide it. |
| Checked division | `ckd_*` covers add, subtract, and multiply only. Zero-divisor and `MIN / -1` guards stay. |
| Checked shift | No C23 facility. Shift-count range checks stay. |
| GNU/Clang `cleanup` attribute for `defer` | It cannot express Hexal's direct-call capture time, exit-time expression evaluation, conditional `errdefer`, cleanup restrictions, and every structured exit without generated frames and additional control machinery. |
| `unreachable()` / `__builtin_unreachable` | Reaching either has undefined behavior. Impossible states remain fail-closed through `abort()` or a diagnostic trap. |
| `__builtin_trap` | It does not preserve Hexal's required diagnostic text and its mechanism is target-dependent. |
| `__builtin_alloca` | Dynamic stack allocation adds stack-exhaustion and lifetime hazards and cannot back spawned Task state. |
| `__builtin_object_size` | Physical object extent cannot replace logical Array/View/List/String bounds or provenance. |
| `__builtin_expect`, assume, likely/unlikely attributes | Optimization hints remove no semantic checks or generator architecture. |
| `_Generic`, `typeof`, C23 `auto` | The checked program already contains exact types; hiding them in generated C reduces readability without deleting checking logic. |
| `__builtin_clear_padding` plus storage comparison | Padding cleanup cannot make memberwise equality correct for NaNs, pointer identity, inactive unions, collection capacity, or backing state. |
| `free_sized` / `free_aligned_sized` | They require retaining exact allocation metadata and do not simplify the current ownership contract. |
| `realloc` for List growth | It does not fit future allocator passing uniformly and saves little generator logic after direct `memcpy`; retain allocate-copy-free until an allocator resize contract exists. |
| C23 `strdup` / `strndup` | They bypass explicit Heap selection and String UTF-8 validation/storage layout. |
| C23 threads as a fiber replacement | `<threads.h>` supplies worker threads, mutexes, and conditions, not portable stackful fibers, Task parking, or M:N scheduling. |

## Readiness

**Base RFC: implementation-ready.** The selected C23 facility, replacement
inventory, header discovery, toolchain boundary, exclusions, tests, and
documentation updates are defined. Zero-alignment behavior, target-sized
capacity arithmetic, complete String overflow chains, and their exact
diagnostics are explicit; no language-design decision remains.

**Amendment 1: implementation-ready for Hexal's pinned targets.** Item A's
stored value is guaranteed by the shared GCC/Clang overflow-builtin behavior;
the RFC explicitly does not rely on C23's defect-affected signed wording
alone. Item B adds a same-width modular conversion requirement within RFC
0062's general shipped-toolchain boundary; it does not claim RFC 0062 already
specified that conversion. Item C changes no runtime behavior.
Wrapping-helper ownership, direct bit-cast copying, and exact-width
signed-right-shift masking are fully specified. RFC 0052 must record the target
evidence before closure.

**Amendment 2: implementation-ready.** Every selected memory operation has an
exact representation/lifetime precondition, every removable wrapper has been
distinguished from adapters that perform real work, trap ownership and header
discovery are defined, and the required exclusions and regression tests are
explicit. Retained Channel/Mutex free adapters preserve Heap evaluation in
both direct and deferred paths.

The default-Heap collapse is not part of the implementation-ready work and
needs an allocator/provenance ADR. `fromfp`/`ufromfp` are rejected for checked
conversion; the independently discovered 64-bit boundary defect needs a
conversion RFC. The smaller `fprintf`, `memchr`, and Channel-`calloc`
possibilities are intentionally not selected.

Land the base RFC first. Land Amendment 1 separately because it touches every
signed numeric expression and owns the direct bit-cast change required to
delete `renderSignedReconstruct`. Land Amendment 2 after Amendment 1 so its
standard-memory discovery starts from the final bit-cast inventory and its
centralized trap consumes the final base/Amendment-1 trap inventory. Do not
combine their generated-C baseline regeneration into one unreviewable change.
