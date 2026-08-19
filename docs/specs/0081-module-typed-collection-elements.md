# RFC 0081: Module-Typed Collection Elements

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready. Option A is decided; see Decided.
- Created: 2026-08-18
- Scope: where a collection specialization over a module-owned element type is
  emitted, so the generated C declares every type it spells
- Depends on: nothing
- Coordinates with: RFC 0027 (Arena and Pool need the same answer),
  `docs/reference.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, or the
  `compiler.Compile` contract

## Summary

`List<M.Point>` compiles cleanly and emits C that references an undeclared
type. It has no owning spec since ADR 0071 was archived.

The cause is a layering inversion: **program-wide component artifacts spell
module-owned C type names, and a component cannot include a module header.**

## Evidence

Probed against current `main`:

```hexal
-- m.hex
export type Point = { x: Int32, y: Int32 }
-- app.hex
module M = import "./m"
h: Heap = Heap.new()
pts: List<M.Point> = List<M.Point>.new(h)
```

```
exit = 0, no diagnostics
```

`hexal/list.h` spells the module type throughout — as a value, not a handle:

```c
#include "hexal.h"
#include "hexal/heap.h"
#include "hexal/view.h"          /* and nothing else */

typedef struct hex_list_Point { hex_t_m1_m_Point *data; ... } hex_list_Point;
static inline void hex_list_push_Point(hex_list_Point *, hex_t_m1_m_Point value);
static inline hex_t_m1_m_Point hex_list_pop_Point(hex_list_Point *);
    if (ckd_mul(&bytes, next, sizeof(hex_t_m1_m_Point))) { ... }
```

`modules/app.h` includes `hexal/list.h` and never includes `modules/m.h`.
`hex_t_m1_m_Point` is therefore undeclared in the translation unit.

### The control changes the diagnosis

Direct use of the same type is **correct**, and reveals the existing strategy:

```hexal
fun f(p: M.Point): Int32 do return p.x end
```

```c
/* modules/app.h */
#include "hexal.h"
typedef struct hex_t_m1_m_Point hex_t_m1_m_Point;   /* re-emitted locally */
struct hex_t_m1_m_Point { ... };
static bool hex_equal_hex_t_m1_m_Point(...);
```

**Module headers never include one another.** A consuming module re-emits the
definitions it needs into its own header. That is why direct use works and
collection use does not: `hexal/list.h` is one program-wide artifact shared by
every module, so it cannot re-emit a per-module definition, and it has no
include path to the module that owns one.

`docs/status.md` described this as an ordering failure — the component header
spelling the name "before the module header defines it". That is not the
mechanism. The module header is never included at all, and no ordering fixes
it.

## Decided — Option A

**A specialization is emitted wherever its element type is available.** The
alternatives are recorded below because RFC 0027 faces the same question for
Arena and Pool collections and should not re-derive it.

### Option A — emit the specialization where the element type is defined

`hex_list_Point` moves out of `hexal/list.h` and into each consuming module
header, immediately after that module's re-emitted `hex_t_m1_m_Point`. The
component header retains only specializations over builtin element types, which
have no owning module.

Rule: *a specialization is emitted wherever its element type is available.*

- Consistent with the strategy already proven by the control above — consumers
  re-emit what they need, and module headers do not include each other, so
  duplication across translation units cannot collide.
- No indirection, no ABI change, no `void *`. Generated C stays typed and direct,
  per `AGENTS.md`'s "no runtime overhead" and "as plain as the compiler source".
- Cost: the component/module split stops being clean. `hexal/list.h` becomes
  "builtin-element specializations" rather than "all list specializations".

### Option B — handle-based collection storage

The component stores opaque storage plus an element size; element access goes
through per-module accessors. No module type name appears in any component.

- Restores the layering cleanly and is what `docs/status.md` guessed at.
- Cost: `void *` plus `memcpy` where a typed array is used today. That is a
  readability regression in generated C and plausibly a performance one, against
  a defect that Option A fixes without either.

### Option C — components include module headers

Simplest diff, and wrong: it inverts the dependency ADR 0071 established, and a
program-wide artifact including per-program module headers cannot be emitted
demand-driven without cycles.

### Why A, and not B or C

It matches the mechanism the control revealed rather than inventing a second
one, keeps generated C typed, and changes no representation. Option B is a
representation change priced against a problem that does not require one.

### Name collisions are already solved — do not re-solve them

`uniqueCollectionCName` (`compiler/types/arena.go:46`) already appends the
module encoding when a base collection name is taken, with a numeric tail if
that also collides. So `List<M.Point>` and `List<S.Point>` in one program become
`hex_list_Point` and `hex_list_Point_m1_s` deterministically, resolved once at
interning, and its comment states the property this RFC depends on: *"every
derivation site reads the stored name, so a typedef and its helper suffixes stay
paired."*

This RFC is only about **where** a specialization is emitted, never about what
it is called. The two-module probe below will not hit a naming wall.

### The include becomes conditional

Option A means a module whose only list, dict, or view specializations are
module-typed has nothing left in the shared component — so it must **not**
include `hexal/list.h` at all.

This is the same predicate RFC 0082 introduces: a component is included when the
including artifact actually uses something from it. Whichever spec lands second
inherits a working mechanism; if this one lands first, it must add the
conditionality itself rather than leaving an include for an artifact that is now
empty for that module.

### First implementation step — the two-module case

**Probe `Dict<M.Point, S.Color>` before writing the fix.** A specialization whose
element types come from two different modules needs both definitions to precede
it. The consuming module re-emits both types already, so emitting the
specialization after both is expected to work — but that is an expectation, not
an observation, and it is the one shape that could invalidate Option A.

If it holds, the rule stands as written. If it does not, the fallback is to
order re-emission by the element types' owning-module dependency order within
the consuming header, which is available and deterministic. Either way this is
settled by a probe on day one, not by design debate.

## Invariants

1. Every C type name spelled in generated output is declared in the same
   translation unit before use.
2. Builtin-element specializations (`List<Int32>`, `Dict<String, Bool>`) keep
   their current artifact and their current C, byte-identical.
3. No collection gains indirection, allocation, or a runtime size field.
4. The compiler performs no filesystem access.

## Validation

- The Evidence program compiles and every type it spells is declared.
- The same shape for `Dict`, `Array`, `View`, and nested `List<List<M.Point>>`.
- Two modules each using `List<M.Point>`: both headers are self-contained.
- `Dict<M.Point, S.Color>` — the two-module case above.
- The snippet manifest moves only for programs that use a module type as a
  collection element; every other program is byte-identical.
- A textual assertion per shape that the element typedef precedes the
  specialization. **No test compiles generated C**, so ordering must be asserted
  on the text — this defect class is otherwise invisible to the suite
  (`docs/status.md`, Known coverage gaps).

## Non-goals

- Changing collection representation, growth, or allocator handling.
- Making module headers include one another.
- Generic monomorphization strategy beyond placement.
- Arena and Pool collections — RFC 0027, which should adopt whatever this
  decides.

## Drawbacks

- Option A blurs the component/module boundary ADR 0071 drew. The boundary was
  already false for this case; this makes the exception explicit rather than
  silent.
- Specializations duplicate across consuming modules, as type definitions
  already do. That is a code-size cost in generated C, not a correctness one.
