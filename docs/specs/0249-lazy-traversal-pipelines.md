# RFC 0249: Lazy Traversal Pipelines

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; direction proposed, surface decisions not authorized
- Created: 2026-09-27
- Updated: 2026-09-27
- Origin: Dict key/value traversal and fluent collection transforms
- Coordinates with: RFC 0223 (Dict Correctness and Completeness Updates),
  archived RFC 0226 (Unified Lists with Inline Capacity), and
  `docs/reference.md`

## Summary

Add allocation-free lazy traversal pipelines for the built-in finite
collections. The initial surface is deliberately small:

```hexal
let labels = users.keys()
    .filter(is_visible)
    .map(label_for)
    .collect(heap)
defer labels.free(heap)

for label in users.values().map(display_name) do
    print(label)
end
```

`keys`, `values`, `map`, and `filter` allocate nothing. `collect` is the
explicit materialization boundary and returns an owning `List<T>` allocated
from the supplied Heap. A pipeline used directly by `for` allocates nothing.

This RFC does not add closures, a user-defined iterator protocol, dynamic
dispatch, coroutine generators, or hidden allocation.

## Motivation

Dict currently supports pair iteration only:

```hexal
for key, value in users do
    // Both values are available even when only one is wanted.
end
```

An ordinary `Slice<K>` cannot represent `users.keys()`: an open-addressed Dict
stores active and inactive buckets, with keys and values interleaved. Returning
a Slice would either lie about contiguity or require a hidden allocation and
copy.

Materializing each transformation also performs unnecessary work:

```hexal
let keys = users.keys(heap)
defer keys.free(heap)
let visible = keys.filter(heap, is_visible)
defer visible.free(heap)
let labels = visible.map(heap, label_for)
```

A lazy pipeline instead fuses the operations into one traversal and allocates
only the requested final collection.

## Goals

1. Permit fluent `map`, `filter`, and `collect` composition.
2. Make `Dict.keys()` and `Dict.values()` allocation-free and honest about the
   Dict's sparse representation.
3. Preserve source evaluation order and existing mutation-invalidation rules.
4. Lower to direct, human-readable C loops without virtual dispatch or heap
   allocation before `collect`.
5. Keep the language surface small by supporting compiler-owned finite sources
   before considering a general iterator protocol.

## Proposed surface

### Sources

The first version admits these lazy sources:

| Source expression | Element type | Traversal |
| --- | --- | --- |
| `list` where `list` is `List<T>` or `List<T,N>` | `T` | logical element order |
| `slice` where `slice` is `Slice<T>` or `Slice<mut T>` | `T` | index order |
| `dict.keys()` | `K` | Dict iteration order |
| `dict.values()` | `V` | Dict iteration order |

List and Slice may start a pipeline directly:

```hexal
let positive = values.filter(is_positive).collect(heap)
let names = records.map(record_name).collect(heap)
```

Dict must select the traversed projection explicitly:

```hexal
let names = users.keys().map(name_for).collect(heap)
let scores = users.values().filter(is_passing).collect(heap)
```

The method is `keys()`, not `key()`, because it represents a sequence.

### Adaptors

```text
source.map<R>(transform: Fun<(T): R>) -> lazy R pipeline
source.filter(predicate: Fun<(T): Bool>) -> lazy T pipeline
```

Adaptors are evaluated in written order for every element:

```hexal
values.filter(is_valid).map(convert)
```

calls `is_valid` first and calls `convert` only for accepted values. Reversing
the chain calls `convert` first and applies the predicate to the converted
result.

Callbacks receive the same immutable shallow copy that an ordinary `for`
binder receives. They are ordinary non-capturing Hexal function values. This
remains invalid:

```hexal
let minimum: Int32 = 10
let selected = values.filter(
    fun (value: Int32): Bool do
        return value > minimum // Error: anonymous functions do not capture.
    end,
)
```

No callback receives an implicit index. Index-aware adaptors, explicit callback
context, fallible adaptor sugar, and mutation through an element place are
separate possible extensions.

### Terminals

```text
pipeline.collect(heap: Heap) -> List<T>
for value in pipeline do ... end
```

`collect` is eager and is the only allocation in the initial pipeline surface.
It returns an ordinary owning allocated List. The caller owns and frees that
List under the existing allocation contract.

`collect` performs ordinary shallow element copies. A pipeline element must
therefore be complete, finite, and copyable wherever the existing List element
and `for` binder rules require those properties. A callback returning
`R | Error` produces a `List<R | Error>`; `map` does not implicitly apply
`try` or otherwise reinterpret the callback result.

## Pipeline representation

The initial pipeline is an ephemeral checked-expression category, not a new
source-spellable type family. It may appear only:

1. as the receiver of another pipeline adaptor;
2. as the receiver of `collect`; or
3. as a `for` source.

It cannot be bound, passed, returned, stored in an object or collection,
captured by `defer` or `spawn`, compared, printed, or named in a type
annotation:

```hexal
let pipeline = users.keys().filter(is_visible) // rejected initially
```

This restriction avoids public `MapIterator<...>` and `FilterIterator<...>`
types, lifetime-bearing stored descriptors, iterator copying semantics, and
function-pointer dispatch. It still supports the intended fluent expression:

```hexal
let labels = users.keys().filter(is_visible).map(label_for).collect(heap)
```

The checker retains the source, ordered adaptor list, and current element type.
The generator lowers the whole expression as one loop. No runtime iterator
object exists.

## Evaluation and mutation semantics

1. The source expression is evaluated exactly once.
2. Callback expressions are evaluated exactly once, from left to right, before
   traversal starts.
3. Source elements are visited in the source's existing order. Dict order
   remains unspecified.
4. Each element passes through adaptors in written order. A failed filter skips
   the remaining adaptors and the terminal action for that element.
5. `collect` preserves the order of accepted results.
6. Empty sources call no callback and collect to an empty List.
7. Traps and ordinary callback effects occur in written traversal order.

List and Dict pipelines capture the source's structural version and apply the
same before-body and after-body checks as ordinary `for`. Structural mutation
or free during a callback traps with the existing `collection modified during
iteration` behavior or is rejected where the checker can prove it. Element
replacement retains the existing List rule. Slice pipelines capture their
pointer and logical length once and retain Slice's programmer-managed lifetime
contract.

## Generated C shape

The compiler emits one direct traversal loop. For example:

```hexal
let result = values.filter(is_even).map(double).collect(heap)
```

lowers conceptually to:

```c
hex_list_int32_t result = hex_list_int32_t_make(heap);
size_t length = values->length;
size_t version = values->version;
for (size_t index = 0; index < length; index++) {
    if (values->version != version) hex_runtime_trap(
        "[Runtime Error] collection modified during iteration\n");
    int32_t value = values->data[index];
    if (is_even(value)) {
        hex_list_int32_t_push(heap, &result, double(value));
    }
    if (values->version != version) hex_runtime_trap(
        "[Runtime Error] collection modified during iteration\n");
}
```

The exact helper names follow the generated program's existing names. The
important contract is one fused loop, no intermediate collection, no runtime
iterator allocation, and no generic next-function dispatch.

## Rejected alternatives

| Alternative | Rejection |
| --- | --- |
| `keys(heap) -> List<K>` snapshots | allocates and copies before the caller asks to materialize |
| `keys() -> Slice<K>` | Dict keys are neither contiguous nor stored separately |
| Public `Iterator<T>` with a `next` function pointer | adds dynamic dispatch and hides concrete traversal state |
| Public concrete `MapIterator<Source,T,R>` families | large type surface and lifetime/storage rules before demand exists |
| Coroutine or generator functions | new control-flow and frame-lifetime machinery far beyond this feature |
| `map_keys`, `filter_keys`, `map_values`, and similar Dict-only methods | duplicates every operation for each projection and does not compose uniformly |
| Packed Dict key/value arrays | replaces the settled Dict representation to solve an API concern |

## Non-goals

- Closures or callback capture.
- User-defined iterable or iterator protocols.
- Infinite sources.
- Parallel traversal.
- Async traversal.
- `flat_map`, `zip`, `enumerate`, `take`, `skip`, `fold`, `reduce`, `find`,
  `any`, `all`, or `count` in the first version.
- In-place mutable element traversal.
- Guaranteed Dict order.
- A hidden default Heap.

## Required implementation work

1. Add checked expression forms for Dict key/value projections, ordered lazy
   adaptors, and pipeline terminals without exposing them as general storable
   values.
2. Extend postfix method checking so the compiler-owned pipeline operations
   participate only after a supported source or pipeline expression.
3. Reuse ordinary function-value checking for callback arity, parameter, and
   result types. Do not introduce a second callback type system.
4. Extend `for` checking to accept a pipeline source with one binder whose type
   is the pipeline's final element type.
5. Extend generator expression validation and observability coverage for every
   new checked form.
6. Lower List, Slice, Dict-key, and Dict-value sources through their existing
   traversal rules, including version checks and source evaluation once.
7. Fuse ordered filters and maps into one generated loop and append only final
   accepted values during `collect`.
8. Select no runtime component merely for creating or traversing a pipeline;
   `collect` selects the ordinary allocated-List requirements for its result.
9. Update `GRAMMAR.ebnf` only if the implementation proves new grammar is
   necessary. The proposed surface is ordinary postfix calls and should need no
   grammar production.
10. Synchronize the Functions, collection, `for`, evaluation-order, allocation,
    and generated-C sections of `docs/reference.md` after behavior stabilizes.

## Implementation plan after surface approval

### Phase 1: checked representation

1. Inventory the current method-call and `for` dispatch points and add coverage
   guards for the new checked forms before lowering them.
2. Represent a pipeline as its evaluated base source, current element type, and
   ordered adaptor records.
3. Reject every escape or storage position with one pipeline-specific
   diagnostic owned by the checker.
4. Check `keys()`/`values()` only on Dict and record the selected projection.

### Phase 2: type checking and evaluation facts

1. Check `map` callbacks as exact one-parameter function values and derive the
   next element type from their result.
2. Check `filter` callbacks as exact `Fun<(T): Bool>` values.
3. Check `collect(heap)` against the existing Heap and allocated-List element
   eligibility rules.
4. Admit one-binder `for` over a pipeline and reuse ordinary binder typing.
5. Record source and callback evaluation order explicitly in the checked tree.

### Phase 3: fused lowering

1. Reuse the existing List, Slice, and Dict traversal emitters rather than
   creating a parallel iteration implementation.
2. Emit each callback expression once before the loop.
3. Emit ordered filter branches and map temporaries inside one source loop.
4. Lower `for` to its body action and `collect` to one result List plus pushes.
5. Preserve all existing List and Dict version guards.

### Phase 4: sweep and documentation

1. Remove no existing pair-iteration path; it remains the direct way to consume
   both Dict fields.
2. Search for assumptions that only the current finite sources can reach `for`
   lowering and make pipeline handling explicit and fail-closed.
3. Update the grammar only if parser syntax changed, then synchronize the
   canonical reference.
4. Regenerate the snippet manifest only for newly added snippets or artifacts
   whose generated C legitimately changes.
5. Rebuild `hexal` and restart the workbench through `hexal play` for handoff.

## Validation required before implementation-ready

Validation must become exhaustive only after the open surface decisions below
are settled. At minimum it must cover:

1. `keys()` and `values()` traverse only active Dict buckets and allocate
   nothing.
2. List, inline List, Slice, mutable Slice, Dict keys, and Dict values each feed
   `map`, `filter`, chained combinations, `collect`, and direct `for` use.
3. Source and callback expressions evaluate exactly once and in written order.
4. `filter().map()` and `map().filter()` demonstrate their distinct per-element
   order.
5. Empty and all-rejected sources collect to empty Lists and invoke only the
   callbacks required by chain order.
6. `collect` returns the exact inferred `List<R>` type and preserves accepted
   result order.
7. A callback returning a union containing Error remains ordinary element data;
   no implicit `try` occurs.
8. Structural List or Dict mutation and free during traversal are rejected or
   trap under the existing rules, including through aliases.
9. Dict order remains unspecified while one traversal preserves the order in
   which it visits accepted elements.
10. A pipeline binding, parameter, result, aggregate member, collection
    element, Task capture, `defer` capture, comparison, or print is rejected.
11. Capturing anonymous callbacks retain the existing no-closure diagnostic.
12. Unsupported sources and unknown adaptor names fail in the checker, never in
    generation.
13. Generated C contains one source loop, no intermediate List, no iterator
    allocation, and no next-function dispatch before `collect`.
14. Existing ordinary collection and Dict pair iteration remain byte-stable for
    programs that use no pipeline operation.
15. Ordinary tests pass; tagged C23 fixtures compile and execute representative
    pipelines and mutation traps.

## Open design questions

1. Should the initial pipeline remain ephemeral as recommended, or should users
   be able to bind, pass, and return a first-class iterator value?
2. Should direct `List.map(...)` and `Slice.map(...)` start a pipeline, as
   proposed, or should all sources require an explicit `.iter()`?
3. Should `collect` use ordinary List growth, preallocate the source length as
   an upper bound, or distinguish exact-size map-only pipelines from filtered
   pipelines?
4. Is the first version intentionally limited to `map`, `filter`, `collect`, and
   direct `for`, or is another terminal essential enough to justify inclusion?
