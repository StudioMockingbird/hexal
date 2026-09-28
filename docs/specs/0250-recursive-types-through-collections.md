# RFC 0250: Recursive Types Through Indirect Storage

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation ready. The compiler crashes, invalid C ordering, and
  valid cyclic-value behavior were reproduced against the current tree on
  2026-09-28. This revision replaces the earlier syntax-based fix with a
  representation-aware design and bounds structural printing at 16 aggregate
  levels
- Created: 2026-09-28
- Updated: 2026-09-28
- Origin: a verification pass on RFC 0233 found that a nominal type recursing
  through `List<T>` or `Dict<K, T>` can crash the compiler and can generate C
  that does not compile. Minimization exposed the same omissions in layout
  validation, atomic discovery, and `Slice<T>` declaration ordering
- Depends on: nothing
- Unblocks: RFC 0233 (JSON), whose `Json.Value` declaration uses recursive
  `List<Value>` and `Dict<String<256>, Value>` storage
- Coordinates with: `compiler/diagnostics`, whose existing
  `type.equality-unavailable-because` diagnostic remains the equality error;
  RFC 0233, whose `Json.stringify` remains the strict JSON serializer rather
  than a fallback inside `print`
- Updates `docs/reference.md`: yes — recursive layouts, recursive equality,
  bounded structural printing, and the structural-print depth limit
- Swept code: the syntax-only `containsTypeName` / `containsPointerType`
  recursion checks after their remaining callers are migrated; stale comments
  claiming collection bodies may always follow nominal bodies; the
  `docs/status.md` bug row owned by this RFC

## Summary

Hexal permits a nominal type to reach itself through indirect storage:

```hexal
type T is union
    | Leaf
    | Node as kids: List<T> end
end
```

That layout is finite because `List<T>` is a pointer handle. The current
compiler nevertheless has five defects around it:

| # | Defect | Observable result |
| --- | --- | --- |
| D1 | Equality and printability predicates revisit the same nominal type without a path set | `fatal error: stack overflow`; the compiler process dies |
| D2 | `ContainsAtomic` revisits by-value structural members without a path set | A recursive inline `List<T, N>` can kill the compiler before the invalid layout is diagnosed |
| D3 | Module-owned collection declarations are emitted in one late block | Valid recursive `List`, `Dict`, `Pool`, and `Slice` programs use a C type before it is declared or complete |
| D4 | Recursion validity is inferred from source syntax instead of resolved storage | The checker accepts invalid by-value recursion and can reject valid indirect recursion |
| D5 | Generated equality recursively expands member comparisons | Accepting recursive structural equality would hang code generation |

This RFC establishes one storage rule and two operation rules:

1. Recursion is legal only when every cycle crosses an indirect-storage edge.
2. Recursive structural equality is rejected.
3. Recursive structural printing is accepted and stops at 16 aggregate levels,
   printing the unquoted marker `...` instead of descending farther.

No new Hexal syntax or type is introduced.

## Verified failures

### Equality and printability crash

For:

```hexal
type T is union
    | Leaf
    | Node as kids: List<T> end
end

let h = Heap()
let kids = List<T>(h)
let a: T = T.Node(kids = kids)
```

both `a == a` and `print(a)` terminate the compiler process with:

```text
runtime: goroutine stack exceeds 1000000000-byte limit
fatal error: stack overflow
```

The equality path loops through `checker.EqualityAvailable` and
`structuralEqualityAvailable`; the print path loops through
`checker.printable`. A Go stack overflow is not recoverable by the compiler's
panic seam.

The same equality failure is reachable through `List<T>` and through a union
containing `T`. Printing also fails through `List<T>` and `Dict<K, T>`.
Equality through a Dict already returns unavailable before revisiting `T`, and
pointer equality remains pointer identity; neither of those paths crashes.

### Atomic discovery has a separate crash

This invalid layout currently reaches `types.ContainsAtomic`, which repeatedly
walks the inline element and nominal payload until the Go stack overflows:

```hexal
type T is union
    | Leaf
    | Node as children: List<T, 1> end
end
```

This disproves the former RFC's claim that exactly two predicate families were
affected. `ContainsAtomic` must terminate even though the earlier layout check
will reject this program after the fix; otherwise a future ordering regression
restores a process-killing failure.

### Generated declarations are ordered incorrectly

A recursive allocated List is emitted as a pointer member before the handle
type has been declared:

```c
typedef struct hex_t_m3_app_T hex_t_m3_app_T;

struct hex_t_m3_app_T {
    hex_tag tag;
    union {
        struct { hex_list_T *hex_m_kids; } Node;
    } payload;
};

typedef struct hex_list_T {
    hex_t_m3_app_T *data;
    /* ... */
} hex_list_T;
```

Clang reports `unknown type name 'hex_list_T'`. A forward typedef fixes
allocated List, Dict, and Pool because Hexal stores their handles as pointers.

`Slice<T>` is different. A Slice descriptor is stored **by value**, so this is
insufficient:

```c
typedef struct hex_slice_T hex_slice_T;
struct hex_t_m3_app_T {
    hex_slice_T hex_m_children; /* incomplete type used by value: invalid */
};
```

Its complete descriptor must precede the nominal body. The descriptor itself
only contains a pointer to `T`, so it can be completed after `T` is
forward-declared and before `T` is defined.

### Cyclic values are constructible

The runtime problem is not limited to deep acyclic trees. A List handle may be
copied into an ADT value and then receive that value as an element:

```hexal
let h = Heap()
let mut kids = List<T>(h)
let root: T = T.Node(kids = kids)
kids.push(root)
```

The current compiler accepts this construction. Once declaration ordering is
fixed, an unbounded recursive print would exhaust the native C stack. A
compile-time printability answer therefore needs a run-time traversal bound.

## Recursive layout contract

Recursion validity is a property of **resolved representation**, not spelling.
The checker must stop using the question "does this type expression contain a
pointer anywhere?" That question accepts a by-value recursive branch merely
because some unrelated branch also contains a pointer.

For every cycle from a nominal type back to itself, the walk must cross at
least one indirect-storage edge:

| Edge in the resolved type graph | Classification | Result on a recursion cycle |
| --- | --- | --- |
| Object member, ADT payload, structural-union member | By value | Continue walking |
| `List<T, N>` inline element | By value | Continue walking |
| Allocated `List<T>` | Indirect handle | Cycle is finite; stop this path |
| `Dict<K, V>` | Indirect handle | Cycle is finite; stop this path |
| `Pool<T>` | Indirect handle | Cycle is finite; stop this path |
| `Stash<T>` | Type-erased indirect handle | Cycle is finite; stop this path |
| `Task<T>` or `Channel<T>` | Type-erased indirect handle | Cycle is finite; stop this path |
| `Slice<T>` or `Slice<mut T>` | By-value descriptor with indirect element storage | Cycle is finite; stop this path |
| `Ptr<T>` or `Ptr<mut T>` | Pointer | Cycle is finite; stop this path |
| `Fun<...>` | Function pointer | Cycle is finite; stop this path |
| Other scalar, opaque, or non-generic runtime-handle types | No embedded nominal value | Stop this path |

The implementation uses a direct switch over the current canonical type kinds,
not a new storage-class framework. A future constructed type must be classified
in that switch before it can participate in a nominal layout; an unknown kind
fails closed.

Examples:

Accepted through an allocated List handle:

```hexal
type Tree is union
    | Leaf
    | Branch as children: List<Tree> end
end
```

Accepted through indirect Slice element storage:

```hexal
type Window is struct
    children: Slice<Window>,
end
```

Rejected because inline List elements are stored by value:

```hexal
type Node is struct
    children: List<Node, 2>,
end
```

Rejected because an unrelated pointer does not legalize the direct branch:

```hexal
type Broken is struct
    next: Broken,
    escape: Ptr<Broken>,
end
```

Direct and mutual by-value cycles use the existing finite-representation
diagnostics. This RFC does not create an alternative spelling-specific error.

## Equality contract

A type that reaches the same nominal type again while structural equality is
being classified is not equality-comparable. The diagnostic uses the existing
`type.equality-unavailable-because` key and preserves the innermost recursive
reason:

```text
equality is unavailable because recursive type T does not support ==
```

The recursive reason must propagate unchanged through Object, ADT, union,
inline List, Slice, and allocated List callers. A caller must not replace it
with an empty member name, `member kids`, or `element type T`.

This is the conservative rule because current equality generation expands
member comparisons recursively. Supporting equality later would require named,
forward-declared, mutually recursive equality helpers. Rejection now is
reversible and costs JSON nothing because `Json.Value` already contains a Dict,
which is not comparable.

Pointer equality remains pointer identity. A nominal type whose only return
edge is behind a pointer can therefore still participate in a comparison that
does not structurally descend through that pointer.

## Structural printing contract

Recursive types are printable when every non-recursive component is printable.
A nominal revisit during printability classification succeeds, allowing the
generated named print helpers to refer to one another.

Run-time structural printing is bounded by:

```go
const MaxStructuralPrintDepth = 16
```

This compiler-owned safety ceiling lives in `compiler/config`; it is not a
measured optimum. It prevents cyclic or adversarial values from exhausting the
native stack while retaining enough depth for ordinary diagnostics.

Depth has one exact meaning:

- Each top-level argument to `print` starts at aggregate depth `0`.
- Object, ADT, structural union, inline List, Slice, allocated List, and Dict
  each count as one aggregate level.
- An aggregate entered with depth `0` through `15` renders normally.
- Before entering an aggregate at depth `16`, its helper prints the unquoted
  marker `...` and returns without reading its members or elements.
- Scalar, pointer, String, Strand, and other leaf values do not consume depth
  and render normally when they are fields of the last rendered aggregate.
- Separate top-level arguments have separate budgets; printing one argument
  cannot consume another argument's depth.

Example for a cycle:

```text
T.Node { kids = [T.Node { kids = [...]}] }
```

The precise number of repeated outer fragments depends on the value's
aggregate shape, but there are never more than 16 entered aggregate helpers on
one traversal path. The marker is syntax owned by diagnostic printing, not a
String value; a nested actual String containing three periods remains quoted.

`print` keeps its existing return type and cannot fail merely because the
display was truncated. Returning `Error` would force `try print(...)` across
ordinary debugging code and would require buffering to avoid emitting a
partial line before discovering excessive depth.

This rule applies to all structural printing, not only types that the checker
can prove recursive. The same bound therefore protects unexpectedly deep
acyclic values.

### JSON is deliberately separate

`Json.stringify` must either produce complete valid JSON or return an Error;
it must never insert `...`. It operates on `Json.Value`, not arbitrary Hexal
objects, and cyclic JSON values are not representable as valid JSON. Users who
need strict machine-readable output choose `Json.stringify`; structural
`print` remains a bounded human-facing diagnostic operation.

## Generated-C declaration order

Module headers use four deterministic regions:

1. **Nominal forward declarations.** Existing Object and ADT forward typedefs.
2. **Pre-nominal indirect descriptors.** Complete module-owned Slice
   descriptors whose element spelling needs only a nominal pointer. This also
   includes Slice descriptors discovered through an allocated List or another
   collection.
3. **Nominal bodies.** Objects, ADTs, and structural-union wrappers in existing
   by-value dependency order.
4. **Post-nominal collection bodies.** Allocated List, Dict, Pool, inline List,
   and any other specialization whose body embeds or otherwise requires a
   complete nominal type.

Before region 3, emit forward typedefs for every post-nominal collection handle
that a nominal body may mention through a pointer:

```c
typedef struct hex_list_T hex_list_T;
typedef struct hex_dict_Int32_T hex_dict_Int32_T;
typedef struct hex_pool_T hex_pool_T;
```

The later definition may repeat the same typedef where C23 permits it; avoid a
second spelling only if the existing writer can omit it without introducing a
separate path.

Ordering is derived from the already-discovered module collection set. It does
not add a second discovery pass or a parallel registry.

## Implementation requirements

### Checker layout validation

1. Replace the syntax-only self-reference checks in object, ADT, and generic
   nominal declaration handling with one resolved-type layout walker.
2. Track nominal identities on the current by-value path. Remove an identity
   when leaving that path so independent branches are fully checked.
3. Stop at the closed allowlist of indirect edges in the table above.
4. Diagnose a revisit reached only through by-value edges with the existing
   Object or ADT finite-representation diagnostic at the declaration that
   closes the cycle.
5. Apply the same rules after generic substitution. Same-argument generic
   recursion through an indirect edge remains valid; an inline recursive
   instantiation is rejected.

### Terminating type predicates

- `EqualityAvailable` keeps its exported signature and delegates to a walker
  with current-path Object and ADT identity sets. A revisit returns unavailable
  with `recursive type <Name>`.
- `printable` keeps its existing caller-facing signature and delegates to a
  current-path walker. A revisit returns printable; non-recursive branches are
  still checked.
- `ContainsAtomic` delegates to a current-path walker. A revisit returns false
  for that path, then the caller continues checking other branches. Invalid
  by-value recursion is owned by layout validation, not by atomic discovery.
- Structural union identity need not be entered separately: an interned union
  can return to itself only through a nominal or indirect type. If that
  invariant is false in the implementation, include union identity in the
  same path set rather than relying on it.

### Diagnostic propagation

The recursive equality reason is a completed diagnostic reason. Aggregate and
collection callers propagate it unchanged. Ordinary non-recursive failures
retain their existing member- or element-specific detail.

### Print generation

1. Thread an aggregate-depth value through every generated structural print
   helper and its call sites.
2. Guard before dereferencing or iterating an aggregate at the limit.
3. Emit literal `...` and return from that helper on truncation.
4. Pass `depth + 1` only when descending into another structural aggregate;
   leaf helpers receive no budget or preserve the current depth.
5. Reset depth to `0` for every top-level print argument.
6. Keep helper discovery memoized by concrete type. Depth is runtime state,
   never part of helper identity.

### Equality generation defense

`writeEqualityComparisons` carries a current-path nominal identity set. A
revisit returns an ordinary Go `error` from the helper instead of recursing.
The generator boundary converts that contract break into the existing
structured generator/Unknown Error path. This defense is unreachable after a
correct checker decision but makes a regression fail closed rather than hang.

### Header emission

Split module collection emission by representation need:

- emit handle forward typedefs before nominal bodies;
- emit complete Slice descriptors before nominal bodies when their element
  needs only an already-forward-declared nominal pointer;
- retain by-value collection bodies after nominal bodies;
- preserve the existing deterministic discovery order within each region.

## Non-goals

- Structural equality for recursive values.
- Cycle detection, object identity, or a `seen address` set at run time.
- User-configurable print depth. The compiler-owned constant can be revised
  later with evidence without adding project or language surface.
- Turning print truncation into an Error or runtime trap.
- Using JSON serialization as an implicit print implementation.
- Cross-module type cycles. Module imports remain acyclic.
- A general C declaration dependency solver; this RFC establishes only the
  four regions required by current Hexal representations.

## Reference synchronization

After behavior stabilizes, update `docs/reference.md` once:

- Replace the direct/pointer recursion sentence with the resolved-storage
  rule: every recursive cycle must cross an indirect edge; explicitly classify
  allocated List, Dict, Pool, Stash, Task, Channel, Slice, Ptr, Fun, and inline
  List.
- State that recursive structural types are not equality-comparable and that
  the diagnostic names the revisited nominal type.
- State that recursive types are printable when their non-recursive components
  are printable.
- Define `MaxStructuralPrintDepth = 16`, its aggregate-counting rule, and the
  unquoted `...` truncation marker.
- Keep `Json.stringify` strict and separate; do not describe JSON as a fallback
  for arbitrary structural values.

Remove the `docs/status.md` bug row only after all Validation items pass.

## Required sweep

- Remove `containsPointerType` and any `containsTypeName` callers used only for
  nominal layout validation after aliases retain their independent recursion
  check.
- Replace comments in `headers.go` and collection emission that claim every
  specialization must follow its element definition.
- Audit all callers of `EqualityAvailable`, `printable`, and `ContainsAtomic`
  so none independently recurse around the guarded walker.
- Audit all structural print helper signatures and invocations; no unbounded
  legacy entrypoint may remain.
- Do not preserve a recursive-print special case once the uniform depth rule is
  in place.

## Validation

This section is exhaustive.

### Layout validation

- Direct Object and ADT self-recursion by value remains rejected with the
  existing finite-representation diagnostic.
- Mutual Object/ADT by-value recursion is rejected rather than overflowing a
  checker or generator walk.
- `List<T, 1>` recursion is rejected before `ContainsAtomic` can recurse.
- A type with one direct recursive member and an unrelated pointer member is
  rejected; the pointer does not legalize the direct branch.
- Recursion through allocated `List<T>`, `Dict<Int32, T>`, `Pool<T>`,
  `Stash<T>`, `Task<T>`, `Channel<T>`, `Slice<T>`, `Ptr<T>`, and a `Fun`
  signature is accepted, subject to each type's existing placement rules.
- Same-argument generic recursion through allocated `List<Node<T>>` is
  accepted; the equivalent inline `List<Node<T>, 1>` recursion is rejected.
- Every accepted layout completes compilation without a crash or hang.

### Predicate termination and diagnostics

- `a == a` and `a != a` for a type recursive through allocated `List<T>` are
  rejected with key `type.equality-unavailable-because` and exact text
  `equality is unavailable because recursive type T does not support ==`.
- Equality on `List<T>` and on a structural union containing `T` reports the
  same recursive reason; outer walkers do not replace it.
- Equality through `Dict<Int32, T>` terminates with its existing unavailable
  reason; the exact reason is not asserted because Dict and recursion are both
  sufficient.
- Equality on a type recursive only through `Ptr<T>` remains accepted as
  pointer identity.
- `print` accepts recursive Object/ADT values, `List<T>`, `Slice<T>`, and
  `Dict<Int32, T>` when every non-recursive component is printable.
- A recursive type with a non-printable ordinary component is rejected with
  the existing print diagnostic naming that component.
- A type reached twice on independent non-recursive paths is fully checked on
  both paths.
- Focused type tests call `ContainsAtomic` on recursive valid and invalid
  shapes and prove termination; a non-recursive Atomic on a sibling path is
  still found.
- Existing non-recursive equality, printability, atomic, and diagnostic tests
  remain unchanged.

### Generated-C ordering

- Text assertions prove each allocated List, Dict, and Pool forward typedef
  precedes the nominal body that names it, while its complete body follows the
  nominal body.
- Text assertions prove a complete `Slice<T>` descriptor precedes a nominal
  body that stores it by value; a forward typedef alone is not accepted by the
  assertion.
- The ordering cases cover a Slice discovered directly and a Slice discovered
  as a companion of another collection.
- Tagged C23 fixtures construct, inspect, and destroy recursive-through-List,
  Dict, Pool, and Slice values successfully.

### Bounded structural printing

- An acyclic recursive value below the limit retains its exact existing output.
- A value whose next aggregate would enter at depth 16 prints `...` at that
  position and completes successfully.
- A genuinely cyclic List/ADT value prints a finite line containing `...` and
  completes without stack exhaustion.
- Scalars and Strings at depth 15 render normally; an actual String value
  `"..."` remains quoted and distinguishable from the marker.
- Two top-level print arguments each receive a fresh depth-0 budget.
- Object, ADT, structural union, inline List, Slice, allocated List, and Dict
  all obey the same depth rule.
- `Json.stringify` tests remain unchanged and never emit the truncation marker.

### Generator failure and conformance

- A generator unit test invokes `writeEqualityComparisons` with a recursive
  type and asserts that the helper returns an error rather than recursing.
- A full compiler-boundary regression that deliberately reaches the defensive
  generator path renders the existing structured Unknown Error; the unit test
  does not pretend the internal Go error is itself a diagnostic.
- `go test ./...`, `go vet ./...`, and `gofmt -l` pass.
- Tagged C23 validation compiles and runs the accepted recursive-layout and
  bounded-print fixtures.
- The snippet manifest changes only where collection declaration regions or
  structural print helper signatures/calls move. The diff is reviewed by
  artifact family; this RFC does **not** claim a forward-typedef-only blast
  radius because Slice ordering and uniform print-depth plumbing are broader.
- `docs/reference.md` matches the four synchronized contracts, and the owned
  status bug row is removed.

## Implementation plan

### Phase 1 — establish resolved layout validity

1. Add the direct resolved-type recursion walker beside nominal declaration
   checking, with a closed switch over current type representations.
2. Route Object, ADT, and generic nominal layouts through it after type
   resolution and before committing the declaration body.
3. Preserve alias-cycle handling separately; remove only the syntax helpers
   made obsolete by nominal layout validation.
4. Land the direct, mutual, mixed-branch, inline List, indirect handle, Slice,
   pointer, and generic validation cases.

### Phase 2 — make every recursive predicate terminate

1. Add current-path identity sets behind `EqualityAvailable` and `printable`.
2. Add the guarded `ContainsAtomic` walker in `compiler/types`.
3. Preserve recursive equality reasons through aggregate callers.
4. Land the predicate and exact-diagnostic tests before changing generation.

At this point invalid layouts and compile-time queries terminate, but accepted
recursive types can still emit invalid C. Do not close the RFC here.

### Phase 3 — order generated declarations by representation

1. Partition the existing discovered module collection set into pre-nominal
   descriptors, handle forwards, and post-nominal bodies.
2. Emit handle forwards and complete Slice descriptors before nominal bodies.
3. Keep bodies requiring complete element types after nominal bodies.
4. Add header-text assertions before the tagged compiler is used as evidence.
5. Run the tagged List, Dict, Pool, and Slice construction fixtures.

### Phase 4 — bound structural printing

1. Add `config.MaxStructuralPrintDepth = 16` with a CARE rationale identifying
   it as a conservative safety ceiling, not a measured optimum.
2. Thread depth through structural print helper declarations, definitions, and
   calls; keep leaf helpers unchanged where possible.
3. Add the guard before every aggregate read or iteration.
4. Reset depth at each top-level print argument.
5. Add exact shallow, boundary, cycle, quoted-ellipsis, and per-argument tests.

### Phase 5 — fail closed and sweep

1. Add current-path recursion defense to `writeEqualityComparisons`.
2. Test its internal error and the compiler-boundary diagnostic at their
   respective layers.
3. Perform the Required sweep and review every affected CARE comment.
4. Rebuild the snippet manifest and inspect all changed artifact families.

### Phase 6 — synchronize and hand off

1. Run ordinary tests, vet, formatting, and tagged C23 validation.
2. Update `docs/reference.md` once with the stabilized contracts.
3. Remove the owned row from `docs/status.md`.
4. Rebuild the `hexal` binary and restart the workbench through `hexal play`.

## Implementation readiness

Ready. The semantics and representation choices are closed:

| Question | Decision |
| --- | --- |
| Which recursive layouts are valid? | Every cycle must cross an indirect-storage edge |
| Is recursive structural equality available? | No; diagnose the revisited nominal type |
| Is recursive structural printing available? | Yes, when ordinary components are printable |
| How is cyclic/deep printing made safe? | Uniform aggregate-depth limit 16; print `...` |
| Does print return Error on truncation? | No |
| Does JSON inherit truncation? | No; JSON remains complete-or-Error |
| How are C declarations ordered? | Nominal forwards, complete Slice descriptors/handle forwards, nominal bodies, remaining collection bodies |

The earlier RFC's probe patches showed that visited sets resolve the immediate
predicate crashes and that handle forwards repair List and Dict. This revision
does not overclaim those probes: it separately requires the unproven Slice,
Pool, mutual-recursion, atomic, cyclic-print, and boundary cases in Validation.
