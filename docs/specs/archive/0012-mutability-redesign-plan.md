# Spec 0012: Mutability Redesign — Execution Plan

- Kind: execution plan
- Implements: `docs/specs/0007-mutability-redesign.md`
- Related: spec 0011 (structured checked expressions, formerly ADR 0001)
- Status: implemented; all seven phases landed, including phase 7 documentation

## Scope

Replace per-value pointer access capability with two pointer type constructors.

| Removed | Added |
|---|---|
| `AccessCapability` and capability slices | `MutPtr<T>` constructor |
| expression-side `mut`, `mut ref` | `ref` typed by place writability |
| clone / compare / attenuate helpers | outermost-layer weakening rule |
| generator capability argument | per-layer pointee `const` from the type |
| RFC 0006's ban on pointer members | pointer members, self-recursive objects |

Member `mut` is retained. Binding `mut` is unchanged.

## Sequencing

**Delete before adding.** Phase 1 removes the capability system, leaving a
compiler in which every pointer writes. That state is internally coherent and
compiles, so it is a safe checkpoint — but it is *more permissive* than either
the old or the new model, so phases 1–5 land together on one branch and are
never shipped separately.

Adding `MutPtr` first would leave two pointer models coexisting inside the
checker, which is the configuration most likely to produce subtle
capability-versus-constructor bugs.

## Phase 0 — Baseline

1. `go test ./...` green.
2. Capture current generated C for the pointer cases in `compiler/compile_test.go`
   into the plan branch as reference output. These change in phase 5; keeping
   the before/after diff reviewable is the point.
3. Inventory the migration surface:

```bash
grep -rn "mut ref\|Capability\|MutableExpression\|MutableRequested" compiler/
```

Known call sites: `checker.go:45,67,276,351-359,427-434,664-666,704-705,734-735,760-773,806,813-845,852-881,918-933`; `operands.go:9-16`; `generator.go:77-85,122-168`; `parser/expressions.go:9-11,40-41`; `parser/ast.go:147-155`.

## Phase 1 — Remove capability and expression-side `mut`

> **This phase deliberately builds a model the spec rejects.** At its end every
> pointer writes and `ref` is unrestricted, which is RFC 0007's §Alternatives
> entry "Make every pointer writable." That is scaffolding, not the target.
> Phases 2–5 replace it. Do not review Phase 1's output against the spec's end
> state, and do not ship it alone.

**Tests first.** Delete or invert:

- `checker_test.go:169` — capability assertion on a `Ptr<Int32>` declaration
- `checker_test.go:201–203` — `mut ref` and `mut reader` diagnostics
- any `compile_test.go` case asserting a read-only-pointer error

Replace each with the phase-4 expectation written as a skipped test, so the
intended end state is visible during phases 2–4.

**Parser** (`ast.go`, `expressions.go`)

- delete `MutableExpression` and its `expressionNode()`
- delete the `lexer.Mut` branches in expression parsing (lines 9–11, 40–41)
- keep the `Mut` token: declarations and member declarations still use it
- add a focused diagnostic for the removed form rather than a bare parse error:
  `mut is not valid on the right-hand side; use ref value`

**Checker** (`checker.go`, `operands.go`)

- delete `AccessCapability`, `ReadAccess`, `WriteAccess`
- delete `Capability` from `binding` and `Operand`, and `MutableRequested`
- delete `cloneCapability`, `sameCapability`, `attenuateCapability`
- delete `checkMutable` and the `parser.MutableExpression` case
- `checkReference` loses its `access` parameter
- assignment compatibility (`checker.go:427`) drops the vector comparison and
  its two special-cased diagnostics; ordinary `types.Equal` remains
- place walk: `.value` becomes unconditionally writable for now

**Generator**

- `declaration()` loses `capability []checker.AccessCapability`; the pointer
  branch becomes plain star repetition plus the binding's trailing `const`
- `Generate` (lines 77–85) stops threading capability

**Exit criteria**

- `go test ./...` green
- `grep -rn "Capability\|MutableExpression" compiler/` returns nothing
- generated C for pointers contains no pointee `const`

## Phase 2 — Add the `MutPtr` constructor

**Type system** (`compiler/types/types.go`) — the delicate part.

`Type` needs to distinguish the two constructors. Add one field rather than a
second struct:

```go
type Type struct {
    // ...
    Element         *Type
    PointeeWritable bool   // false: Ptr<T>   true: MutPtr<T>
}
```

`Environment.pointerTypes` is keyed by `element.identity` alone
(`types.go:191`). It must become a composite key, or the two constructors will
alias each other:

```go
type pointerKey struct {
    element  *typeIdentity
    writable bool
}
```

Then:

- `Environment.PtrType(element)` → `pointerType(element, false)`
- `Environment.MutPtrType(element)` → `pointerType(element, true)`
- both build `Name` as `"Ptr<…>"` / `"MutPtr<…>"` and set `PointeeWritable`
- `CName` stays `element.CName + "*"`; qualification is the generator's job in
  phase 5, not part of the interned spelling
- `IsProtectedTypeName` (`types.go:248`) adds `"MutPtr"`
- `Equal` needs no change — separately interned types have distinct identities

**Parser** (`type_expressions.go`)

- accept `MutPtr` wherever `Ptr` is accepted, producing the same node shape
  with a constructor discriminator
- reject `Ptr<mut T>` with `mut is not allowed inside Ptr<...>; use MutPtr<...>`

**Tests**

- `types_test.go`: `PtrType(Int32)` and `MutPtrType(Int32)` are not `Equal`;
  each is stable across repeated calls; both are rejected across environments
- `type_expressions_test.go`: `MutPtr<Int32>`, `MutPtr<MutPtr<Int32>>`,
  `Ptr<MutPtr<Int32>>` all parse; `Ptr<mut Int32>` produces the focused error
- `rfc0005_test.go`: `type MutPtr = Int32` is rejected as protected

**Exit criteria** — `MutPtr<T>` parses, resolves, and interns distinctly, while
behaving identically to `Ptr<T>` at every use site. No behavior change yet.

## Phase 3 — `ref` typing and place rule case 3

**Checker**

- `checkReference` returns `MutPtrType(T)` when the place is writable and
  `PtrType(T)` otherwise. No writability *requirement* — a fixed place yields
  a usable `Ptr<T>`.
- place walk case 3 (`checker.go:760`): `.value` is writable exactly when the
  receiver's type has `PointeeWritable`. Read the type, not a place mode.
- place walk cases 1 and 2 are unchanged: binding `mut`, then receiver
  writability `&&` member `mut`.

**Tests**

```seawitch
mut score: Int32 = 0
answer: Int32 = 42

writer: MutPtr<Int32> = ref score     // exact match
look: Ptr<Int32> = ref answer         // exact match
writer.value = 1                      // valid
look.value = 1                        // Error: Ptr<Int32> cannot write
```

Plus the fixed-binding-with-MutPtr case, which must still write its pointee and
still reject repointing.

**Exit criteria** — read-only pointers reject pointee assignment; the
`bad: MutPtr<Int32> = ref answer` case fails as an ordinary type mismatch, not a
bespoke diagnostic.

## Phase 4 — Weakening

One rule in assignability: `MutPtr<T>` is acceptable where `Ptr<T>` is expected,
**outermost layer only**, every layer below identical.

```go
// conceptual
func assignable(target, source Type) bool {
    if types.Equal(target, source) { return true }
    return target.Element != nil && !target.PointeeWritable &&
           source.Element != nil && source.PointeeWritable &&
           types.Equal(*target.Element, *source.Element)
}
```

Route every expected-type site through it: declaration initializers, assignment
sources, and **object member initializers** — the last is the one RFC 0006's
identical-type rule would otherwise reject.

**Tests**

```seawitch
observer: Ptr<Int32> = writer               // valid
promoted: MutPtr<Int32> = look              // Error
ok: Ptr<MutPtr<Int32>> = outer              // valid: outermost only
no: Ptr<Ptr<Int32>> = outer                 // Error: inner layer
c: Config = Config { name = ref buffer }    // valid through a member
```

**Exit criteria** — the four cases above behave as specified; the deep-weakening
rejection has its own diagnostic.

## Phase 5 — Generator declarators

`declaration()` reads qualification from the type chain alone:

- walk the chain outermost to innermost
- at each layer, emit `const` on the pointee when that layer is `Ptr`
- the binding contributes the trailing `const` when not `mut`

**Tests** — one C23 compilation case per combination, since these are the shapes
most likely to produce invalid C:

| Seawitch | Expected C |
|---|---|
| `Ptr<Int32>` | `const int32_t *` |
| `MutPtr<Int32>` | `int32_t *` |
| `Ptr<Ptr<Int32>>` | `const int32_t *const *` |
| `MutPtr<Ptr<Int32>>` | `const int32_t **` |
| `Ptr<MutPtr<Int32>>` | `int32_t *const *` |
| `MutPtr<MutPtr<Int32>>` | `int32_t **` |

Plus three cases the table does not cover:

- a fixed object binding lowers to `const` storage —
  `const sw_t_Point sw_v_origin = (sw_t_Point){ … };`
- `ref` on that fixed binding produces `const sw_t_Point *const` with no
  discarded qualifier
- weakening emits no cast

**Exit criteria** — all six compile under the project's C23 settings with
warnings-as-errors.

## Phase 6 — Pointer members and self-recursion

RFC 0006 rejects pointer-valued members; this phase lifts that.

**Checker**

- allow a member whose canonical type is `Ptr<T>` or `MutPtr<T>`
- during `type N = { ... }`, publish a provisional nominal identity visible only
  to its own member declarations
- permit a path back to `N` only through at least one pointer layer; reject
  every by-value path with the existing size-cycle diagnostic
- discard the provisional identity if any member fails

**Generator**

- split every object into a forward `typedef` region and a definition region,
  both in source declaration order, so recursive and non-recursive objects share
  one shape

**Tests** — `type Node = { value: Int32, mut next: MutPtr<Node> }` compiles and
lowers; `type Impossible = { value: Impossible }` is rejected; `type A = { b:
Ptr<B> }` before `B` is rejected as unknown.

## Phase 7 — Documentation

Per AGENTS.md, only after behavior stabilizes:

- `docs/grammar.md` — `pointer-constructor`, member `mut`, no expression `mut`
- `docs/language.md` — the two constructors, weakening, place walk
- `docs/status.md` — completed work and remaining follow-ups
- rebuild `bin/hexal-workbench.exe` and restart the workbench

## Risks

**Phase 1 is temporarily more permissive than either model.** Every pointer
writes and `ref` is unrestricted. Do not ship phases 1–5 separately.

**The interning key is the highest-risk change.** If `pointerTypes` keeps its
single-identity key, `Ptr<Int32>` and `MutPtr<Int32>` silently become the same
type and every phase-3 test passes for the wrong reason. Write the
`types_test.go` non-equality test *first*.

**`PtrType` package-level helper** (`types.go:216`) has a compatibility path
that constructs a fresh environment. It needs a `MutPtrType` sibling, or callers
must move to the environment methods. Prefer the latter.

**Weakening must not leak into deeper layers.** The rejection test is as
important as the acceptance test; a permissive implementation produces C that
compiles today and is unsound.

## Verification

1. `go test ./...`
2. Generated C for all six pointer combinations compiles clean under C23
3. `grep -rn "Capability\|MutableExpression\|mut ref" compiler/` returns nothing
4. Workbench rebuilt, restarted, and a manual pointer example checked end to end

## Estimate

Phases 1–5 are one cohesive change and should land together. Phase 6 is
separable and can follow. Phase 7 is bookkeeping after both.
