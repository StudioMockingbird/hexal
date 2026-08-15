# RFC 0062: Demand-Driven `hexal.h`

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-15
- Features: remove redundant target assertions, emit standard headers and EoS
  support on demand, and make generated-header dependencies explicit
- Created: 2026-08-15
- Depends on: RFC 0034 (modules), RFC 0049 (`Size` target behavior), RFC 0059
  (generator ownership), and RFC 0060 (shared `hexal.h` and root-owned entrypoint)
- Coordinates with: RFC 0052 (target profiles), RFC 0055 (filesystem/build
  driver), and `docs/reference.md`

## Summary

`hexal.h` currently emits a full fixed-integer target probe and the `hex_eos`
typedef for every program. It also supplies standard-library includes to other
generated artifacts whether their features need them or not.

This RFC makes generated support demand-driven:

- GCC or Clang plus the selected compatible C library form the supported C23
  toolchain contract.
- Toolchain and target qualification occurs once outside generated source,
  eventually in the build driver/target-profile layer.
- Generated C contains no unconditional target-profile assertions.
- Program-specific C constant assertions remain when the Hexal checker cannot
  decide them without target information.
- `hexal.h` is the deliberate program-wide umbrella for portable standard
  headers and support declarations required by the reachable generated
  program.
- Module C files receive those portable prerequisites through their own module
  header. Conditional platform/runtime include blocks remain local to the C
  file that owns them.

## Motivation

For:

```hexal
x: Int32 = 13
```

the current shared header emits assertions for every signed and unsigned
fixed-width type plus an unused EoS representation. Only `int32_t` participates
in the generated program.

The assertions provide no additional representation evidence when a supported
C library exposes `int32_t`, `uint64_t`, and the other exact-width typedefs:
their existence is already the C library's exact-width contract. Repeating a
global toolchain probe in every generated project and translation unit adds
noise without changing runtime behavior.

The same distinction applies to floating point. Supported Hexal targets are
qualified once for the promised binary32/binary64 representations. Per-program
checks remain appropriate only when their truth depends on both source and the
selected target, such as a target-sized `Size` literal.

## Supported toolchain contract

Generated output assumes all of the following:

- GCC or Clang accepts the generated C23 language surface.
- The selected target and C library provide every standard header, type,
  constant, and function required by the features used by the program.
- Exact-width `<stdint.h>` typedefs used by the program have their standard C
  meanings.
- Hexal-supported targets use 8-bit bytes and the representations promised for
  `Float32` and `Float64`.
- Compiler flags preserve Hexal semantics; flags that relax floating-point,
  aliasing, overflow, or synchronization behavior are outside the supported
  toolchain contract.
- A bundled or portable C library is acceptable when it implements the same
  required C23 surface for the selected target.

The core compiler remains string-in/string-out and does not locate, invoke, or
probe GCC, Clang, a C library, or the host filesystem. RFC 0052/0055 will own
toolchain selection, target qualification, probing, and user-facing toolchain
diagnostics. Until that layer exists, consumers compiling `Files` are
responsible for satisfying this contract.

## Removed generated assertions

Remove these target-profile assertions from `hexal.h`:

- `CHAR_BIT == 8`;
- width/range assertions for UInt8/16/32/64;
- width/range assertions for Int8/16/32/64;
- `FLT_RADIX == 2`;
- binary32 representation assertions for `float`; and
- binary64 representation assertions for `double`.

Remove the associated `#error` checks for `FLT_IS_IEC_60559` and
`DBL_IS_IEC_60559`.

The integer checks and `CHAR_BIT` check are currently unconditional; the float
checks are currently conditional on reachable float use. The generated project
must not recreate any of them in a module header or C file. They are
toolchain-profile facts, not program facts.

## Retained generated assertions

Retain source-dependent, target-dependent constant assertions that the
in-memory compiler cannot decide portably. The current required case is:

```c
static_assert(<Size literal> <= SIZE_MAX,
              "Size literal <value> requires a size_t target wide enough");
```

Rules:

- Target-independent invalid constants remain checker errors.
- A `Size` literal known to fit every conforming supported target emits no
  assertion.
- A target-dependent `Size` literal emits one deduplicated assertion in
  `hexal.h`.
- Future generated assertions must establish a source/program-specific fact;
  generic toolchain certification does not belong in generated artifacts.

## Standard-header ownership

`hexal.h` is the explicit, program-wide umbrella for portable standard-header
dependencies. Every module header includes it, and every module C file includes
its own module header. This is an intentional generated-project dependency, not
an accidental dependency on one standard header including another.

Do not rely on a standard header being transitively included by another
standard header. A required declaration or macro must cause emission of the
standard header that owns it.

### `hexal.h`

Build one program-wide set of portable standard headers required by all
reachable generated declarations, definitions, helpers, and runtime families.
Discovery covers the complete generated program, not only declarations written
to module headers. Emit the set once in `hexal.h`, immediately after the
`HEXAL_H` guard, in deterministic lexical order.

Current umbrella-header families and principal triggers are:

| Header | Emit when the reachable generated program uses |
|---|---|
| `<stddef.h>` | `size_t`, `nullptr_t`, `nullptr`, or other definitions owned by this header |
| `<stdint.h>` | exact-width integers, `uintptr_t`, `SIZE_MAX`, Rune, Byte, or EoS representation |
| `<stdlib.h>` | allocation, deallocation, `abort`, or another declaration from this header |
| `<stdio.h>` | `FILE`, standard streams, formatted output, or trap reporting |
| `<inttypes.h>` | `PRI*` formatting macros |
| `<math.h>` | `isfinite`, `isnan`, `isinf`, `signbit`, or another math declaration |
| `<string.h>` | `memcpy` or another string/memory declaration |
| `<stdatomic.h>` | `_Atomic`, atomic operations, or memory-order constants |

Rules:

- `<stdbool.h>`, `<limits.h>`, and `<float.h>` are not emitted by this RFC's
  generated C. C23 supplies `bool` as a keyword; removed target probes were
  their remaining unconditional reason.
- Feature writers declare requirements through semantic discovery state; they
  do not insert `#include` directives into the middle of generated bodies.
- Include discovery is based on checked types and selected helper/runtime
  families, never by searching rendered C text.
- One required header appears once in `hexal.h` even when several reachable
  modules or helper families require it. The same spelling may separately
  occur inside a conditional platform/runtime include block owned by a module
  C file.
- A module header continues to include `hexal.h` and no other module header.
  Portable standard prerequisites for both that header and its corresponding C
  file are satisfied by the program-wide set.
- `size_t` use independently requires `<stddef.h>`. Do not preserve the current
  `Nil`-only `<stddef.h>` trigger or depend on `<stdlib.h>` providing `size_t`.

The dependency inventory must cover at least:

- `NULL` -> `<stddef.h>`;
- `size_t`, `nullptr_t`, and `nullptr` -> `<stddef.h>`;
- exact-width integer types, `uintptr_t`, `SIZE_MAX`, integer range macros, and
  `INT*_C`/`UINT*_C` macros -> `<stdint.h>`;
- allocation, deallocation, and `abort` -> `<stdlib.h>`;
- trap reporting and formatted I/O -> `<stdio.h>`;
- `PRI*` formatting macros -> `<inttypes.h>`;
- float classification and other math APIs -> `<math.h>`;
- `memcpy` and other memory/string APIs -> `<string.h>`; and
- atomic types, operations, and ordering constants -> `<stdatomic.h>`.

Requirement discovery must explicitly audit the existing emitters
`writeBitCastDefinitions`, `writePrintDefinitions`,
`writeConcurrencyTypePrelude`, `writeConcurrencyInlineHelpers`,
`writeIOInlineHelpers`, and `hexalHeader`, plus every writer currently relying
on the fixed prefix or another standard header's transitive contents. This is
an inventory requirement, not a requirement to preserve those exact helper
names.

### Module C files

- A module C file includes its own module header.
- Conditional platform/runtime include blocks remain direct includes of the C
  file that owns them and do not pollute `hexal.h`.
- The generated process entrypoint returns integer constant `0`, the C-defined
  successful termination status. Entry-point generation does not itself
  require `<stdlib.h>`.
- Existing concurrency platform headers remain owned by the root runtime C
  implementation.

## Demand-driven EoS representation

Remove unconditional:

```c
typedef uint8_t hex_eos;
```

Emit it exactly once, before the first type that references it, when any
reachable generated declaration or selected support family requires EoS.

Required triggers include:

- a written `EoS` type or `eos` value;
- a generated union containing EoS;
- Stream completion machinery; and
- Channel receive machinery that represents EoS.

Channel construction, send, close, query, and free operations do not require
EoS unless another selected declaration or helper independently uses it.

Merely having EoS available as a protected builtin does not emit its C type.
EoS discovery must operate over the complete reachable program and generated
support requirements so cross-module uses produce one shared typedef.

## Expected minimal output

For a program whose only generated declaration is `Int32`, `hexal.h` contains
only its guard and required exact-width header:

```c
#ifndef HEXAL_H
#define HEXAL_H

#include <stdint.h>

#endif
```

The root module C file needs no standard header merely to report success:

```c
#include "modules/app.h"
```

No assertion or `hex_eos` declaration appears for that program.

## Implementation design

### Requirement state

Introduce one small program-wide requirement value owned by emission, for
example:

```go
type cHeaderRequirements struct {
    headers map[string]bool
    eos     bool
}
```

The exact Go representation is not normative. It must:

- merge requirements from every reachable module deterministically;
- receive requirements from checked types and already-selected generated
  definitions, helper/runtime states, and entrypoint lowering;
- render each standard header once at the top of `hexal.h`;
- decide EoS emission without rendering or parsing C strings; and
- remain an emission concern rather than leaking target flags into lexer,
  parser, checker, or public compiler input.

### Generator changes

- Replace the fixed `hexalHeaderPrefix` include list with guard-only prologue
  plus the computed include block.
- Delete fixed-integer and floating-representation probe emission.
- Keep and order deduplicated `Size` literal assertions after includes.
- Move helper-writer `#include` emission into requirement discovery.
- Gate the `hex_eos` typedef on the aggregated EoS requirement.
- Change the process entrypoint from `return EXIT_SUCCESS;` to `return 0;`.
- Replace every dependency that currently arrives through the fixed
  `<stdlib.h>` prefix with an explicit owning-header requirement; in
  particular, discover `<stddef.h>` from every generated `size_t` use.
- Preserve all helper/type ordering after the include/assertion prelude.
- Update stale generator comments that describe a fixed target-profile
  preamble or a fixed 64-bit `size_t` profile assertion.
- Do not change `CompilationResult.Files`, artifact names, module ownership,
  linkage, or runtime state placement.

### Tests and fixtures

Update tests that currently require the unconditional prelude, especially:

- generator header-profile tests;
- integration literal tests;
- Float representation tests;
- EoS/Stream/Channel tests;
- exact root-C expectations;
- module artifact/header tests; and
- workbench generated-C hashes.

Dormant C23 canaries remain dormant. Their source/build helpers may be updated
to the new artifact content, but this RFC must not add runnable test entrypoints
or invoke an external toolchain from ordinary tests.

## Required tests

- Int32-only: `hexal.h` contains `<stdint.h>` and no target assertion,
  `hex_eos`, `<stdbool.h>`, `<limits.h>`, or `<float.h>`.
- Bool-only: no `<stdbool.h>` and no integer/float target assertion.
- Float32/Float64 use: no representation assertion or `<float.h>`; `<math.h>`
  appears only when emitted operations call its API.
- Small `Size` literal: no target assertion.
- Target-dependent `Size` literal: exactly one deduplicated `SIZE_MAX`
  assertion and all declarations required for it.
- EoS literal/type: exactly one `hex_eos` typedef.
- Stream and Channel completion: exactly one shared `hex_eos` typedef across
  one or several modules.
- Program without EoS support: no `hex_eos` spelling.
- Heap/String/List/Dict/trapping helpers: required `<stdlib.h>`/`<stdio.h>`
  umbrella includes are present exactly once in `hexal.h`.
- View/Array/List/String and every other `size_t` user require `<stddef.h>` even
  when `Nil`, allocation, and `<stdlib.h>` are absent.
- Print, conversions, bitcasts, Atomic, I/O, and concurrency: each selected
  helper family contributes its required umbrella headers exactly once in
  `hexal.h`; conditional platform/runtime includes remain local.
- Unselected helper families contribute no standard headers.
- Every `#include` in `hexal.h` precedes its first declaration and no helper
  writer inserts a later include.
- Root C returns `0`, includes its module header, and does not require
  `<stdlib.h>` solely for successful process termination.
- Multi-module requirement aggregation is deterministic and independent of
  source-map iteration order.
- Generated artifact keys, linkage, source mapping, and runtime semantics are
  unchanged.
- `go test ./...`, `go vet ./...`, `go test -tags c23 ./...`, and
  `go vet -tags c23 ./...` pass without invoking GCC or Clang.

Ordinary tests validate requirement discovery and rendered dependency sets in
pure Go. Actual GCC/Clang header-self-containment compilation belongs to the
future build-driver/toolchain lifecycle or a manual validation pass; this RFC
does not make an external compiler part of ordinary tests.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` once:

- replace the claim that generated headers verify fixed-width and IEC-float
  assumptions with the supported GCC/Clang/C-library contract;
- state that only source/program-specific target assertions are emitted;
- make `hexal.h` standard includes and EoS representation demand-driven; and
- state that a root entrypoint returns C's successful integer status directly;
- preserve the existing artifact ownership and `Size` contracts.

Update the still-open RFC 0052 during this RFC's implementation so it treats
these representation facts as qualified target metadata rather than
reintroducing assertions into generated code.

## Non-goals

- Selecting, downloading, or invoking GCC, Clang, or a C library.
- Implementing RFC 0052 target profiles or RFC 0055 build-driver behavior.
- Adding a target/profile parameter to `Compile`.
- Changing Hexal scalar, Float, Size, EoS, ABI, or runtime semantics.
- Removing source-dependent `Size` literal guards.
- Demand-driving every generated helper family; this RFC demand-drives header
  dependencies and EoS only.
- Adding or enabling external-toolchain tests.

## Acceptance criteria

1. `hexal.h` contains no generic integer, byte-width, or float target probe.
2. Source-dependent target assertions remain correct and deduplicated.
3. Umbrella standard headers are emitted once in `hexal.h`, deterministically,
   and only when the reachable generated program requires them; conditional
   platform/runtime include blocks remain locally owned.
4. `hex_eos` is emitted exactly when generated C requires it.
5. The root C entrypoint returns `0` and introduces no standard-header
   dependency solely for successful termination.
6. Generated headers are self-contained under their explicit includes.
7. On supported toolchains, no compiler API, language semantics, artifacts,
   linkage, or runtime behavior changes.
8. Tests, workbench hashes, and `docs/reference.md` are synchronized before
   closure.

## Readiness

Implementation-ready. The bundled GCC/Clang plus compatible-C-library premise
settles target certification; RFC 0052/0055 may later automate qualification
without changing this generated-output contract.
