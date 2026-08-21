# RFC 0100: Program-Wide Stateless Helper Components

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed
- Created: 2026-08-20
- Scope: emit module-independent numeric, print-core, and String comparison
  helpers once as demand-generated program components
- Coordinates with: generator discovery/emission, `generator/packages/`,
  `docs/reference.md`, `docs/status.md`, the snippet manifest
- Does not change: Hexal syntax or semantics, helper behavior, evaluation
  order, traps, generated module ownership, or runtime representation

## Summary

Several stateless helper families depend only on canonical builtin types but
are discovered and emitted independently in every module header. Two modules
using the same operation therefore generate the same helper twice.

Move those families into program-level component artifacts while retaining
module-local demand: a module includes a component only when its code calls a
helper from it.

Generated artifacts:

```text
hexal/numeric.h       checked numeric and bit helpers; header-only
hexal/print.h         primitive print-core declarations
hexal/print.c         primitive print-core definitions
hexal/string.h/.c     existing String component plus demanded equality/order
```

Nothing moves into `hexal.h` except standard prerequisites already selected by
the program-wide requirement set.

## Evidence

A two-module full-pipeline probe used the same five scalar operations in both
modules:

```text
                    conversion division shift bitcast endian
hexal.h                 0         0       0      0      0
modules/app.h            1         1       1      1      1
modules/m.h              1         1       1      1      1
```

A second probe called `print` from both modules:

```text
                    primitive print core
hexal.h                       0
modules/app.h                  1
modules/m.h                    1
```

The duplicates are legal `static` helpers, but their module placement carries
no semantic information.

## Numeric component

`hexal/numeric.h` owns every demanded specialization of:

- checked scalar conversion;
- guarded integer division and remainder;
- guarded left and right shift;
- same-width `bit_cast`;
- fixed-width endian encode and decode.

Rules:

- Merge each module's demand into one program set, keyed by operation and
  canonical source/target type identity.
- Sort each family by stable canonical key before rendering.
- Emit each specialization once in `hexal/numeric.h` as `static inline`.
- Keep direct casts and identity conversions inline at their expression sites;
  they create no component demand.
- Include `hexal/array.h` from `numeric.h` only when an endian helper names an
  `Array<UInt8, N>` specialization. Endian discovery must select that Array
  component first.
- Select standard headers through the existing program-wide prerequisite
  model; `numeric.h` does not depend on incidental transitive includes.
- A module includes `hexal/numeric.h` exactly when that module calls at least
  one numeric helper. The header contains the complete program-wide helper set.
- Emit no `numeric.h` when the merged set is empty.
- Remove the five numeric helper-definition regions from module-header
  emission: `writeConversionDefinitions`, `writeDivisionDefinitions`,
  `writeShiftDefinitions`, `writeBitCastDefinitions`, and
  `writeEndianDefinitions`. Their semantic body builders may be reused by the
  program component; their definitions and module-header call sites do not
  remain duplicated.

Header-only ownership preserves the current inline opportunity and introduces
no external call boundary.

## Print component

`hexal/print.h/.c` owns the fixed primitive runtime currently repeated whenever
one module uses `print`:

- byte/text writes;
- Bool and Nil;
- fixed-width signed and unsigned integers;
- Size;
- Float32 and Float64;
- Rune;
- quoted byte text and quoted Rune formatting.

Rules:

- Emit the pair when any reachable module uses `print`.
- `print.h` declares the internal `hex_print_*` functions; `print.c` defines
  them once with external linkage inside the generated program.
- The selected generated project contains exactly one `hexal/print.h` and one
  `hexal/print.c`. Selection is atomic: neither artifact is emitted without the
  other, and no module can include `print.h` in a successful result whose
  `Files` map lacks the matching `print.c`.
- `print.c` includes `print.h`; every externally linked definition has exactly
  one matching declaration, and no module header defines the same symbol.
- `hex_print_*` is a compiler-reserved internal symbol family. External
  linkage permits calls across generated translation units but does not make
  the functions a supported foreign ABI.
- A module using `print`, or emitting a module-owned aggregate print adapter,
  includes `hexal/print.h`.
- Keep adapters for module-owned objects, ADTs, structural unions, and
  module-owned collection specializations in the module header after their
  complete type definitions.
- Keep Error-specific aggregate formatting module-local in this RFC; it depends
  on the complete Error object and composes the program print primitives.
- Keep exact formatting, left-to-right evaluation, single evaluation, write
  checking, and trap text unchanged.
- Remove primitive print-core definitions from module headers.

## String equality and ordering

The helpers currently named `hex_equal_hex_string` and
`hex_compare_hex_string` depend only on the String representation.

- Add independent equality and ordering demand flags to the program String
  component model.
- Emit the equality declaration/definition in `hexal/string.h/.c` only when any
  reachable comparison or recursive equality helper needs it.
- Emit ordering only when a reachable String ordering expression needs it.
- Preserve helper names and bodies in this RFC.
- Module-owned aggregate equality helpers call the component function; they do
  not re-emit it.
- A String program with neither equality nor ordering emits neither function.

## Program and module state

- `programEmission` owns the merged numeric sets, the print-core flag, and the
  String equality/order flags.
- `moduleEmission` retains only the local include-demand facts and module-owned
  print/equality adapter state.
- Component builders consume only the program aggregate.
- Module-header assembly selects component includes in dependency order:
  String and Array before Numeric where required, then Print before local
  aggregate adapters.
- Existing artifact-key collision checks apply unchanged.

## Required sweep

- Route the outputs of `writeConversionDefinitions`,
  `writeDivisionDefinitions`, `writeShiftDefinitions`,
  `writeBitCastDefinitions`, and `writeEndianDefinitions` exclusively through
  `hexal/numeric.h`; delete their module-header emission calls and state fields
  that become unused.
- Split `writePrintDefinitions` by ownership: primitive `hex_print_*` core
  definitions move to `hexal/print.c`; module-owned aggregate adapters remain
  after their complete types. Delete no adapter merely because its primitive
  callees moved.
- Move `writeStringEqualityHelper` and `writeStringCompare` into the demanded
  String component model and remove their module-header definitions.
- Remove imports, local demand flags, and helper-writing branches left with no
  production caller. Do not retain delegating wrappers around the component
  builders.

## Invariants

1. A module-independent helper specialization has one generated owner.
2. `hexal.h` remains a small prerequisite/ABI header, not a helper dump.
3. Module-owned complete types and their typed adapters remain module-local.
4. Helper semantics and call-site evaluation order do not change.
5. Component selection is demand-driven and deterministic.
6. No external tool executes during ordinary tests.

## Validation

This section is exhaustive.

- Two modules using one identical checked conversion emit one definition in
  `hexal/numeric.h`, and both module headers include it.
- Repeat the preceding assertion for division/remainder, left/right shift,
  `bit_cast`, and endian encode/decode.
- A module not using a numeric helper does not include `numeric.h`, even when
  another module selects it.
- A direct- or identity-conversion-only program emits no `numeric.h`.
- An endian-only program emits `array.h` before `numeric.h`; `numeric.h` is
  self-contained through an explicit Array dependency.
- No numeric helper definition survives in any module header.
- Two modules using `print` emit one `print.h/.c` pair; neither module header
  contains a primitive print-core definition.
- Every `hex_print_*` declaration has exactly one definition in `print.c`;
  `print.c` includes `print.h`, and no successful artifact set contains only
  one member of the pair.
- A module-owned object/ADT aggregate print adapter remains after the complete
  type definition and calls declarations from `print.h`.
- A program without `print` emits no print component.
- String equality in two modules emits one equality function in the String
  component; String ordering does the same independently.
- String use without equality or ordering emits neither helper.
- Module-owned aggregate equality calls the String component helper without
  re-emitting it.
- Program aggregation and every rendered family are deterministic under
  repeated compilation.
- Generated-C text assertions verify component declarations precede all uses,
  each helper is emitted once, and every required direct include is present.
- `docs/reference.md` records component ownership and demand rules without
  restating language semantics.
- The snippet manifest moves only for snippets selecting one of these helper
  families; review numeric, print, String, and module artifacts separately.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Moving module-owned aggregate print or equality helpers.
- Changing numeric lowering or using new C facilities.
- Combining all helper families into `hexal.h`.
- Guaranteeing one machine-code body for `static inline` numeric helpers.
- Filesystem access or external C compilation.

## Drawbacks

- Adds two component families and more typed render models.
- `numeric.h` contains the program-wide superset when included by one module.
- Print primitives gain external linkage within the generated program instead
  of module-local `static` linkage; names remain compiler-reserved and are not
  public C ABI.
