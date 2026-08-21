# Implementation Plan: RFC 0100 + RFC 0101

## Overview

Implement two related RFCs that move stateless helpers from per-module headers
to program-level component artifacts, eliminating duplicate emission.

- **RFC 0100**: Numeric, Print, and String equality/order components
- **RFC 0101**: Equality component for program-owned aggregate types

## RFC 0100: Program-Wide Stateless Helper Components

### Step 1: Numeric Component (`hexal/numeric.h`)

Create `generator/numeric_component.go` with:
- `numericComponents(merged *programEmission) ([]componentArtifact, error)`:
  merges per-module numeric state into one program set, builds render model
- `moduleNumericComponent(emission *moduleEmission) []string`:
  selects `hexal/numeric.h` when module uses any numeric helper
- Render model struct carrying: conversionSpecs, divisionTypes, shiftSpecs,
  bitCastSpecs, endianSpecs, and the hexal/array.h dependency flag

Create `generator/packages/numeric.h` template:
- Header guard, `#include "hexal.h"`
- Conditional `#include "hexal/array.h"` when endian specs present
- One `static inline` helper per spec, reusing existing body builders:
  - `writeConversionHelper`, `writeDivisionHelper`, `writeShiftHelper`,
    `writeBitCastDefinitions`, `writeEndianHelper`

Create `generator/numeric_component_test.go`:
- Two modules using same conversion emit one definition
- Module not using numeric helpers doesn't include component
- Direct-only conversion program emits no numeric.h

Wire into `renderComponentArtifacts` families list and `moduleComponentHeaders`.

### Step 2: String Equality/Order in String Component

Extend `generator/string_component.go`:
- Add `needEquality bool` and `needOrdering bool` to `stringRenderModel`
- Build model from merged equality state: `state.needString` → equality,
  `state.compareNeed` → ordering
- Extend `packages/string.h` template with conditional equality/ordering
  declarations
- Extend `packages/string.c` template with conditional equality/ordering
  definitions
- Move `writeStringEqualityHelper` and `writeStringCompare` bodies into
  template rendering (or keep as Go helpers writing to the template output)

Update `stringComponents` to populate the new model fields from
`merged.equalityState`.

### Step 3: Print Component (`hexal/print.h/.c`)

Create `generator/print_component.go` with:
- `printComponents(merged *programEmission) ([]componentArtifact, error)`
- `modulePrintComponent(emission *moduleEmission) []string`
- Render model carrying: the full `generatedPrintState` and tags

Create `generator/packages/print.h` template:
- Header guard, `#include "hexal.h"`, `#include <stdio.h>`, etc.
- Declarations for all `hex_print_*` functions (bytes, text, bool, nil,
  int8/16/32/64, uint8/16/32/64, size, float32, float64, rune,
  quoted_text, quoted_rune, nested helpers)

Create `generator/packages/print.c` template:
- `#include "hexal/print.h"`
- Definitions for all `hex_print_*` functions (bodies currently in
  `writePrintDefinitions`)
- Error print helpers and nested aggregate adapters

Create `generator/print_component_test.go`.

Wire into `renderComponentArtifacts` and `moduleComponentHeaders`.

### Step 4: Remove Per-Module Emission

In `emission.go` `moduleHeader`:
- Remove calls to `writeConversionDefinitions`, `writeDivisionDefinitions`,
  `writeShiftDefinitions`, `writeBitCastDefinitions`, `writeEndianDefinitions`
- Remove `writePrintDefinitions` call (print core moves to component; keep
  only module-owned aggregate adapters after type definitions)
- Remove `writeStringEqualityHelper` / `writeStringCompare` from equality
  definitions path

Clean up unused state fields from `moduleEmission` that become redundant
after the component split (keep only local demand flags).

### Step 5: Module Header Assembly

Update `moduleComponentHeaders` to include the new component selectors:
```go
components = append(components, moduleNumericComponent(emission)...)
components = append(components, modulePrintComponent(emission)...)
```

Ensure dependency order: wrap → heap → view → string → error → list → dict →
array → numeric → print → concurrency.

### Step 6: Standard Header Requirements

Update `computeHeaderRequirements` in `emission.go`:
- Numeric component needs `<string.h>` for memcpy/bitcast, `<math.h>` for
  float conversions
- Print component needs `<stdio.h>`, `<inttypes.h>`, `<math.h>`
- These are already covered by the existing trap/math/stdint requirements;
  verify no gap

## RFC 0101: Program-Wide Equality Helpers

### Step 7: Fix `typeIsModuleEmitted` Predicate

In `module_collections.go`, correct the union branch:
```go
if typ.Union != nil {
    return true  // all unions are module-emitted
}
if typ.NullableBase != nil {
    return typeIsModuleEmitted(*typ.NullableBase)
}
```

Current code recursively inspects union members, which incorrectly
classifies builtin-only unions as program-owned.

### Step 8: Equality Component (`hexal/equality.h`)

Create `generator/equality_component.go` with:
- `equalityComponents(merged *programEmission) ([]componentArtifact, error)`
- `moduleEqualityComponent(emission *moduleEmission) []string`
- Ownership predicate: `typeIsProgramOwned(typ)` — the inverse of the
  corrected `typeIsModuleEmitted`, excluding String (owned by string
  component), scalars, pointers, and Strand
- Partition logic: split each module's `generatedEqualityState` into
  program-owned and module-owned sets
- Merge program-owned sets by canonical identity, sort by stable key
- Render model: the merged program equality state plus tags

Create `generator/packages/equality.h` template:
- Header guard, `#include "hexal.h"`
- Conditional includes for components defining compared types
  (`hexal/string.h`, `hexal/array.h`, `hexal/view.h`, `hexal/list.h`)
- One `static inline` helper per program-owned type, reusing existing
  body builders

Create `generator/equality_component_test.go`:
- Two modules comparing same `Array<Int32, N>` emit one helper in equality.h
- User object retains module-owned helper
- Builtin-only union stays module-owned
- String equality owned by string component, not equality.h
- Recursive composition emits one helper per type in dependency order

### Step 9: Wire Equality Component

Wire into `renderComponentArtifacts` families list (after error, list, array
components) and `moduleComponentHeaders`.

Update `writeEqualityDefinitions` to exclude the program-owned partition
when the component exists.

### Step 10: Equality Component Dependencies

`equality.h` includes:
- `hexal.h` (base)
- `hexal/string.h` when String equality needed
- `hexal/array.h` when Array equality needed
- `hexal/view.h` when View equality needed
- `hexal/list.h` when List equality needed

Standard header requirements: `<stddef.h>` for size_t, `<string.h>` for
memcmp.

## Docs & Manifest

### Step 11: Reference and Status

Update `docs/reference.md`:
- Component ownership section: numeric.h, print.h/.c, equality.h
- Demand-driven include rules per component

Update `docs/status.md`:
- Mark 0100 and 0101 as implemented

### Step 12: Manifest Rebuild

Rebuild snippet manifest via temporary test (per AGENTS.md process):
- Write temp test that walks snippets, compiles each, SHA-256s artifacts
- Write manifest to `workbench/snippets/testdata/generated-c-sha256.json`
- Review diff for moved artifacts
- Delete temp test

## File Inventory

### New Files
- `compiler/generator/numeric_component.go`
- `compiler/generator/numeric_component_test.go`
- `compiler/generator/packages/numeric.h`
- `compiler/generator/print_component.go`
- `compiler/generator/print_component_test.go`
- `compiler/generator/packages/print.h`
- `compiler/generator/packages/print.c`
- `compiler/generator/equality_component.go`
- `compiler/generator/equality_component_test.go`
- `compiler/generator/packages/equality.h`

### Modified Files
- `compiler/generator/string_component.go` (equality/order model fields)
- `compiler/generator/packages/string.h` (equality/order declarations)
- `compiler/generator/packages/string.c` (equality/order definitions)
- `compiler/generator/emission.go` (remove per-module emission calls,
  update moduleComponentHeaders, update computeHeaderRequirements)
- `compiler/generator/module_collections.go` (fix typeIsModuleEmitted)
- `compiler/generator/equality.go` (exclude program-owned from module path)
- `compiler/generator/render.go` (if module header assembly changes)
- `docs/reference.md`
- `docs/status.md`
- `workbench/snippets/testdata/generated-c-sha256.json`

## Verification

1. `go test ./compiler/...` — all existing tests pass
2. `go vet ./...` — clean
3. `go test -tags c23 ./compiler/...` — C23 canaries clean
4. New component tests pass
5. Manifest rebuilt and reviewed
6. Workbench rebuilt and restarted
