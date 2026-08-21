# RFC 0101: Program-Wide Equality Helpers

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed
- Created: 2026-08-20
- Scope: emit equality helpers for program-owned concrete types once while
  retaining helpers for module-owned complete types in module headers
- Depends on: RFC 0100 moving String equality into the String component
- Coordinates with: component partitioning, generator equality discovery,
  `docs/reference.md`, `docs/status.md`, the snippet manifest
- Does not change: equality semantics, type eligibility, evaluation order,
  representation, or accepted programs

## Summary

Equality discovery currently mixes program-owned component types and
module-owned types in one per-module state. Two modules comparing the same
builtin collection therefore emit the same recursive helper twice.

Partition equality by C definition ownership:

```text
hexal/equality.h       program-owned equality specializations
modules/<name>.h       module-owned equality specializations
```

`equality.h` is header-only and demand-generated. Static inline definitions
preserve current call and optimization behavior.

## Ownership classification

Only aggregate types receive helpers in this component. Program-owned equality
helpers are equality-capable concrete aggregates whose complete definitions
are emitted by program components:

- builtin-element `Array<T, N>`;
- builtin-element `View<T>`;
- builtin-element `List<T>`;
- recursively composed program-owned forms of those types;
- the compiler-owned `Error` object.

Module-owned equality helpers remain local for:

- user objects;
- user ADTs;
- every structural union, including unions containing only builtin members,
  because every union wrapper is currently emitted in module headers;
- collections recursively containing a structural union or any other
  module-owned type;
- any aggregate whose complete definition is unavailable to a program
  component.

String equality is owned by `hexal/string.h/.c` under RFC 0100 and is not
redefined in `equality.h`.

Classification uses the corrected shared complete-type ownership predicate:

- an object or ADT is module-owned exactly when its `ModuleID` is non-empty;
- every structural union is module-owned;
- `Array`, `View`, and `List` inherit ownership recursively from their element;
- every other helper-capable aggregate is program-owned.

This deliberately differs from treating a builtin-only structural union as a
program component type. Its definition still lives in a module header, so a
program header cannot legally name it. Scalar, pointer, Strand, and String
comparisons do not create `equality.h` helpers and are outside this partition.

The structural-union rule depends on the current wrapper owner, not an
intrinsic property of unions. RFCs 0095 and 0099 keep wrappers in module
headers. Any later change that moves wrappers into a program component must
revisit this ownership predicate and its recursive collection classification.

Direct pointer equality remains scalar identity comparison and creates no
helper. A collection containing `Ptr<ModuleType>` or `MutPtr<ModuleType>` is
nevertheless module-owned: its generated declaration spells the pointee's
module-owned typedef, so the collection cannot be defined before that typedef.
This placement rule is about C declaration completeness, not pointer
dereference or equality semantics.

### Required complete-type ownership correction

The shared collection predicate currently claims that every structural union
is module-emitted, but its implementation recursively inspects the members and
therefore classifies a builtin-only union as program-owned. A focused probe of
`Array<Int32 | Bool, 2>` produced this order:

```c
/* hexal/array.h */
typedef struct hex_array_Bool___Int32_2 {
    hex_union_4_bool7_int32_t data[2];
} hex_array_Bool___Int32_2;

/* modules/app.h, included later */
typedef struct hex_union_4_bool7_int32_t { /* ... */ }
    hex_union_4_bool7_int32_t;
```

The component header names a complete inline element before its definition and
cannot compile. Correct the shared predicate as part of this RFC:

- `typ.Union != nil` is always module-emitted;
- `typ.NullableBase != nil` returns
  `typeIsModuleEmitted(*typ.NullableBase)` directly. It never enters the
  general-union branch and never creates a tag or payload wrapper;
- Array, View, List, and Dict continue to inherit ownership from their inline
  element or value type;
- component builders exclude the corrected module-owned collection set, and
  module headers emit it after their union definitions.

This is a prerequisite sweep, not a new collection representation or equality
rule. It makes the implementation match the predicate's existing contract and
the actual location of structural-union definitions.

## Discovery and emission

- Discover equality requirements per module as today, recursively including
  member and element dependencies.
- Use the shared deterministic pre-order program walker. Its structural type
  descent visits aggregate members and collection elements and terminates
  recursive object/ADT graphs with the existing seen sets; no fixed-point pass
  is added. Partition the discovered records, deduplicate the program partition
  by canonical identity, then canonical-key sort it before emission.
- Partition each discovered type with the corrected shared ownership predicate
  above; do not infer ownership from source spelling.
- Keep the module-owned partition in the originating module equality state.
- Merge program-owned requirements into one program equality state by
  canonical identity and sort them by stable canonical key.
- Emit one `hexal/equality.h` containing the merged program-owned helpers.
- `equality.h` includes `hexal.h`, then the exact component headers defining
  its helper parameter and member types, in component dependency order.
  Include the String component when a recursive comparison calls String
  equality.
- Program requirement discovery supplies `<stddef.h>` for generated `size_t`
  loops and `<string.h>` for direct Strand comparison. Do not depend on an
  unrelated standard header providing either declaration.
- A module includes `equality.h` when its expressions or module-owned helpers
  call a program-owned equality helper.
- Emit no equality component when the merged set is empty.
- Module-owned helper generation excludes the program-owned partition. A
  module includes and may call the program component when required.
- Keep helper names unchanged in this RFC.

Equality bodies continue to inline most recursive member and element
comparisons. This RFC does not generally replace them with calls to separately
generated aggregate helpers. Preserve the existing exceptions: String calls
the String component helper, and a structural-union List member calls the
List's equality helper. The latter is the module-owned-to-program-owned edge
that requires `equality.h` in the union's module header.

### Generator structure

- Keep per-module equality discovery, then partition each state before module
  rendering.
- Add a program equality state to `programEmission`; merge only the
  program-owned records into it by canonical identity.
- Add `generator/equality_component.go` for partitioning, model construction,
  component selection, and module include selection.
- Add `generator/packages/equality.h` for the header guard, includes, and
  rendered helper-definition records. Dynamic type selection, ordering, C
  spelling, and helper-body semantics remain Go responsibilities; the template
  owns presentation only.
- Register the equality component after the defining Error, View, String,
  List, and Array component families. Its generated includes use the same
  dependency order.
- Retain `writeEqualityDefinitions` for the module-owned partition. The
  program component renderer consumes only the merged program state.
- Correct `typeIsModuleEmitted` rather than adding a second collection
  ownership implementation. General structural unions return true directly;
  nullable pointer niches return the ownership of their `NullableBase`
  directly.

Program-level ownership means one generated source artifact. As a header-only
component, the C preprocessor may expose its static inline definitions to
several translation units; this RFC promises no external symbol or single
machine-code body.

## Invariants

1. Every equality helper has exactly one generated ownership class.
2. No program component names an incomplete module-owned type.
3. Every complete type definition and called component declaration precedes
   the helper that uses it.
4. String equality has one owner: the String component.
5. Equality behavior and eligibility remain unchanged.
6. Component and module output remain deterministic.

## Validation

This section is exhaustive.

- Two modules comparing the same `Array<Int32, N>` emit one helper in
  `hexal/equality.h`; both modules include it and neither re-emits it.
- Repeat for builtin-element View and List equality.
- A recursively composed program-owned collection emits one helper per
  discovered compared aggregate, each exactly once and in stable canonical-key
  order; recursive comparisons remain inline.
- A program-owned helper comparing String calls the String component equality
  function and does not define another String helper.
- Two modules comparing `Error` emit one `hex_equal_hex_t_Error` helper in
  `hexal/equality.h`; it uses the String component helper and is not re-emitted
  in either module.
- A user object, user ADT, structural union, and collection containing a user
  type each retain their module-owned helper after the complete type
  definition.
- A builtin-only structural union and an Array, View, or List recursively
  containing it remain module-owned; `equality.h` names neither incomplete
  type.
- Array, View, List, and Dict specializations containing a builtin-only
  structural union are absent from program component headers and are emitted
  after the union definition in each consuming module header. This Dict
  assertion validates only the shared collection-definition ownership
  correction; Dict equality remains unsupported and no Dict equality helper is
  emitted.
- A collection over `Ptr<Int32> | Nil` remains program-owned and creates no
  general-union wrapper. A collection over `Ptr<Module.Point> | Nil` remains
  module-owned only because its nullable base spells the module-owned pointee
  typedef; it likewise creates no tag or payload wrapper.
- Direct equality of pointers to a module-owned object emits no equality
  helper. Array, View, or List over such a pointer remains module-owned and its
  helper follows the pointee typedef and collection definition.
- A module-owned helper may call a program-owned helper through
  `hexal/equality.h`; a structural union containing a builtin-element List is
  the required case, and the include precedes that call.
- A module not using or depending on program-owned equality does not include
  `equality.h`.
- A program with no program-owned aggregate equality emits no equality
  component.
- Every helper name appears in exactly one ownership artifact per program.
- `equality.h` contains the required generated component includes in stable
  dependency order and independently requests `<stddef.h>` and `<string.h>`
  when its emitted bodies use `size_t` and `memcmp` respectively.
- Repeated compilation produces byte-identical files.
- `docs/reference.md` records generated equality-helper ownership only.
- The snippet manifest moves for snippets comparing affected program-owned
  aggregate types and for any snippet whose collection specialization moves
  from a component header to a module header under the corrected union rule;
  no other artifact family moves.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Centralizing structural-union wrappers or module-owned types.
- Changing which types support equality.
- Adding Dict, Task, Channel, Mutex, Atomic, or function equality.
- Creating an external equality ABI.
- External C compilation.

## Drawbacks

- Adds one component and a partition step to equality discovery.
- Some module-owned helpers remain duplicated when the same foreign nominal
  type is re-emitted in several consuming headers; removing that requires a
  module-header topology redesign.
