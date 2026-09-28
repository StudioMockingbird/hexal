# RFC 0249: Lazy Traversal Pipelines

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented
- Created: 2026-09-27
- Updated: 2026-09-27
- Origin: Dict key/value/entry traversal and fluent collection transforms
- Builds on: archived RFC 0223 (settled Dict representation and traversal),
  archived RFC 0226 (settled allocated and inline List forms), archived RFC
  0243 (central diagnostics and stable keys), and `docs/reference.md`

## Summary

Add a small, compiler-owned pipeline expression for finite built-in
collections:

```hexal
let labels = users.keys()
    .filter(is_visible)
    .map(label_for)
    .to_list(heap)
defer labels.free(heap)

for label in users.values().map(display_name) do
    print(label)
end

let entry_labels = users.entries().map(format_entry).to_list(heap)
```

`keys`, `values`, `entries`, `map`, `filter`, and `reduce` add no pipeline-owned
allocation. `to_list(heap)` is the explicit materialization boundary and
returns an owning allocated `List<T>`. Its ordinary List construction and
growth may perform multiple allocations. A pipeline used directly by `for`
adds none.

The first version is deliberately ephemeral. It adds no source-spellable
`Iterator<T>` type, closures, coroutine frames, user-defined iteration
protocol, hidden Heap, or runtime iterator object. It is ergonomic sugar for a
plain fused loop, not a new collection abstraction.

## Why this is worth considering

Dict stores active and inactive buckets with keys and values interleaved. A
`Slice<K>` cannot honestly represent `dict.keys()`: the keys are not
contiguous. A snapshot `List<K>` would allocate and copy even when the caller
only wants to traverse.

The existing language can already express every transform with a loop:

```hexal
let labels = List<String>(heap)
for key, user in users do
    if is_visible(key) then
        labels.push(label_for(key))
    end
end
```

The proposed chain removes this ceremony while retaining the same allocation
boundary and one-loop C shape. It is an ergonomic feature, not a capability
required for Zig or Odin parity. Zig normally exposes concrete iterator values
for selected containers; Odin centres direct collection loops. Hexal takes the
smaller middle ground: fluent composition without public iterator types.

The feature has meaningful compiler cost: a non-value checked category,
statement-producing `to_list`, fused control flow, and iteration-safety
integration. It is justified only if these remain one small fixed surface and
do not grow into a general query framework.

## Verified current baseline

1. RFCs 0223, 0226, and 0243 are implemented and archived. `List<T, N>`, the
   current Dict representation, and centralized diagnostic keys are baselines,
   not blockers.
2. A multiline chain beginning with `.` already parses as ordinary postfix
   calls. No grammar production is required; the current failure is the
   expected unknown-method type diagnostic.
3. The generated allocated-List ABI is pointer-shaped:
   `hex_list_Int32 *`, `hex_list_new_Int32(heap)`, and
   `hex_list_push_Int32(list, value)`. Earlier value-shaped examples were
   stale.
4. An entry-environment function cannot become a `Fun` value. The current
   diagnostic is:

   ```text
   [Type Error type.entry-environment-function-value] function release uses the entry environment and is valid only as a direct entry-module call
   ```

   Therefore a pipeline callback cannot capture the source indirectly through
   such a function under the existing `Fun` rules.
5. A separate live defect exists in ordinary `for`: a direct call in the body
   can invoke an entry-environment function that frees the traversed List. A
   focused probe compiled with no diagnostics and emitted a call to the
   freeing function followed by reads of `hex_for_1->version`. The later
   version guard therefore dereferences freed storage instead of making the
   program safe. Pipeline `for` must not inherit this defect.

## Author decisions

1. Callbacks remain context-free. A transform that needs caller state uses an
   ordinary loop; this RFC adds neither closures nor `map_with`/`filter_with`.
2. Loop-producing terminals (`to_list` and `reduce`) are initially valid only
   as a complete binding initializer, assignment source, or return expression.
   Nesting either inside another expression is rejected.
3. A traversal conservatively rejects a direct call to an entry-environment
   function or method whose precomputed capture set contains the traversed
   collection. No new effect system or runtime traversal pin is added.
4. The materialization spelling is `to_list(heap)`, not the earlier draft's
   `collect(heap)`. There is one obvious spelling for converting a pipeline or
   Slice into an allocated List.
5. `Dict<K, V>.to_list(heap)` returns `List<K | V>` in alternating key, value
   order. The element union is canonicalized; when `K` and `V` are the same
   type, the result is `List<K>` rather than an invalid `K | K` union.
6. `Dict<K, V>.entries()` is a third lazy projection whose element is a
   compiler-owned `DictEntry<K, V>` value. It preserves pairing for pipelines
   without replacing the shorter ordinary `for key, value in dict` form.

## Settled design

### 1. Sources

The first version admits exactly these sources:

| Source | Element | Traversal |
| --- | --- | --- |
| `List<T>` | `T` | logical element order |
| `List<T, N>` | `T` | logical element order |
| `Slice<T>` | `T` | index order |
| `Slice<mut T>` | `T` immutable shallow copy | index order |
| `Dict<K, V>.keys()` | `K` | active buckets in existing Dict iteration order |
| `Dict<K, V>.values()` | `V` | active buckets in existing Dict iteration order |
| `Dict<K, V>.entries()` | `DictEntry<K, V>` | active buckets in existing Dict iteration order |

List and Slice start directly:

```hexal
let positive = values.filter(is_positive).to_list(heap)
let names = records.map(record_name).to_list(heap)
```

Dict must choose a projection because it has two independently useful scalar
views and one paired view:

```hexal
let names = users.keys().map(name_for).to_list(heap)
let scores = users.values().filter(is_passing).to_list(heap)
let labels = users.entries().map(format_entry).to_list(heap)
```

No `.iter()` marker is added. Requiring it would add ceremony without resolving
an ambiguity for List or Slice. `keys()`, `values()`, and `entries()` are
explicit because Dict is the only multi-projection source.

The projections are themselves complete pipeline sources and may be iterated
without an adaptor:

```hexal
for key in users.keys() do
    print(key)
end

for value in users.values() do
    print(value)
end

for entry in users.entries() do
    print(entry.key)
    print(entry.value)
end
```

The entry form visits the same buckets as the shorter existing pair loop:

```hexal
for key, value in users do
    print(key)
    print(value)
end
```

A Slice also materializes directly through the same terminal spelling:

```hexal
let copied = window.to_list(heap)
```

This performs ordinary shallow element copies into an allocated `List<T>`.

A Dict may materialize both projections into one alternating union List:

```hexal
let flattened: List<String | Int32> = users.to_list(heap)
```

For every active bucket encountered in Dict traversal order, `to_list` pushes
the key first and its value second. Pairs therefore stay adjacent even though
the order between pairs remains unspecified. The key and value are injected
into the canonical `K | V` union. If both Dict parameters are the same type,
the result is `List<K>` and the two values are pushed directly.

The source expression is evaluated once. An allocated List or Dict pipeline
captures its one handle and structural version. An inline List place is
iterated in place; an inline List temporary is materialized once in compiler
temporary storage, matching ordinary `for`. Slice captures its data and length
once. Dict scans the captured bucket region once and runs adaptors only for
`active` buckets; it does not snapshot keys or values.

Text is excluded. `String` and `String<N>` have distinct useful byte, rune, and
grapheme traversals, so treating one of those as the implicit `map` element
would be arbitrary. A later text pipeline must begin from an explicit text
projection if concrete demand justifies it.

#### `DictEntry<K, V>`

`DictEntry<K, V>` is a protected, source-spellable, compiler-owned generic
value type with exactly two type arguments and two fixed fields:

```text
DictEntry<K, V> {
    key: K
    value: V
}
```

It exists so an entry callback can state its parameter type:

```hexal
fun has_positive_score(entry: DictEntry<String, Int32>): Bool do
    return entry.value > 0
end

let positive = users.entries()
    .filter(has_positive_score)
    .to_list(heap)
```

Users cannot redeclare, shadow, or construct `DictEntry` directly in the first
version. Dict traversal is its only producer. A bare or explicitly specialized
constructor call is rejected by the existing non-constructible-type path;
this RFC adds no second construction diagnostic. Once produced, an entry is an
ordinary shallow value: it may be bound, passed, returned, or stored in
`List<DictEntry<K, V>>`. Its fields are fixed. Equality and printing follow the
ordinary aggregate rule and are available only when both field types support
the operation. It is not newly admitted as a Dict key.

The generated entry value uses a separate two-field C struct. It never aliases
or exposes the runtime Dict bucket, whose `active` field and layout remain an
implementation detail. The struct name is reserved through the existing
program-wide generated-definition registry rather than introducing an
independent naming scheme. Its definition follows the same recursive ownership
classification as a structural collection: a specialization whose field C
spelling names a module-owned type remains module-owned; otherwise its one
program-wide definition is emitted with the shared generated types.

### 2. Adaptors

The only adaptors are:

```text
pipeline.map(transform: Fun<(T): R>) -> pipeline of R
pipeline.filter(predicate: Fun<(T): Bool>) -> pipeline of T
```

They execute in written order per element:

```hexal
values.filter(is_valid).map(convert)
```

calls `is_valid` first and calls `convert` only for an accepted value. Reversing
the calls converts first and filters the converted result.

Callbacks obey the ordinary `Fun` contract rather than a second callback type
system:

- exactly one fixed parameter;
- a concrete non-rest signature;
- `filter` returns exactly `Bool`;
- `map` has a result; a no-result function is rejected;
- function literals remain non-capturing;
- methods are not function values and cannot be passed as callbacks;
- a nullable or union-held function value must first be narrowed to one exact
  `Fun` type;
- an open generic function must first be contextually specialized to an exact
  `Fun` binding when its result cannot be inferred from the source alone.

```hexal
let convert: Fun<(Int32): String> = stringify
let text = values.map(convert).to_list(heap)
```

This RFC adds no special callback inlining. A runtime `Fun` value uses the
existing function-pointer call lowering, so one adaptor can add one indirect
call per visited element. The pipeline itself adds no `next` dispatch or
iterator allocation. Code that requires the minimum possible call overhead
can use an ordinary loop.

Callbacks may perform effects and allocations of their own. Only the pipeline
machinery is allocation-free before `to_list`.

Callbacks remain context-free in this RFC. When a predicate or transform needs
caller state, use an ordinary loop:

```hexal
let selected = List<Int32>(heap)
for value in values do
    if value >= minimum then
        selected.push(value)
    end
end
```

`filter` does not refine the element type. Filtering `T | Nil` with a predicate
that happens to reject `Nil` still yields a `T | Nil` pipeline. This avoids
embedding predicate interpretation or a second narrowing system in the
feature.

No callback receives an index. Pipeline `for` accepts one binder only. The
original source index is intentionally unavailable after filtering or mapping.

### 3. Terminals

The first version has exactly three terminal forms:

```text
pipeline.to_list(heap: Heap) -> List<T>
pipeline.reduce(initial: R, combine: Fun<(R, T): R>) -> R
for value in pipeline do ... end
Slice<T>.to_list(heap: Heap) -> List<T>
Slice<mut T>.to_list(heap: Heap) -> List<T>
Dict<K, V>.to_list(heap: Heap) -> List<K | V>
```

No `count`, `find`, `fold`, `any`, or `all` is included. An ordinary
loop remains the simple spelling until repeated demand justifies another
terminal.

`to_list` uses ordinary allocated-List growth. It does not add an exact-size
analysis, reserve API, or upper-bound preallocation policy. That is the
simplest correct implementation and avoids trading a filtered pipeline's
unused capacity for an unmeasured optimization.

Direct Slice and Dict `to_list` calls obey the same complete-expression
position restriction as pipeline terminals. Dict materialization performs two
pushes per active bucket, key then value. All forms perform ordinary shallow
copies and select only the allocated-List runtime support required by their
result type.

The two Dict materialization forms intentionally differ:

```hexal
let flat: List<String | Int32> = users.to_list(heap)
let paired: List<DictEntry<String, Int32>> =
    users.entries().to_list(heap)
```

The first preserves the requested alternating union representation. The second
preserves the key/value relationship in each List element.

The pipeline's current element must meet ordinary `for` binder requirements
when the terminal is `for`. `to_list` additionally requires that element to be
legal in `List<T>`. These are separate checks; direct traversal is not rejected
merely because materializing the same type would be illegal.

`reduce` always takes an explicit initial accumulator. Empty sources return it
unchanged, so no `Nil`, Error, or non-empty precondition is needed:

```hexal
fun add(total: Int32, value: Int32): Int32 do
    return total + value
end

let total = values.reduce(0, add)
```

The combiner is a concrete, non-rest, context-free `Fun<(R, T): R>`. Its
concrete signature determines `R`, after which the initial expression is
checked in that context; `values.reduce(0, add)` therefore gives `0` the
`Int32` type from `add`. The accumulator type may differ from the element type,
but the initial value, first parameter, and result are exactly the same `R`.
The initial value is evaluated once before the combiner expression. The
combiner then runs once for each
element that survives preceding filters and maps, in traversal order. A
combiner result containing Error remains ordinary accumulator data; `reduce`
does not apply `try` or terminate early.

`map` does not treat `Error` specially. A callback returning `R | Error`
produces elements of `R | Error`; materializing it produces `List<R | Error>`.
There is no implicit `try` and no hidden early exit. Use an ordinary loop when
an error must immediately leave the enclosing function:

```hexal
for value in values do
    let converted = try convert(value)
    output.push(converted)
end
```

`to_list` performs shallow element copies. The result owns its List header and
backing region, not allocations referred to by its elements. If a transform
creates owning Strings, freeing the List does not free those Strings; their
existing cleanup contract remains in force.

### 4. Ephemeral checked representation

A pipeline is not a `compiler/types.Type` and is not source-spellable. The
checker adds optional pipeline metadata beside the normal value fields of
`checkedExpression`:

```text
checkedPipeline {
    base source and source kind
    selected Dict projection, when any
    current element type
    ordered adaptor records
    source collection root, when statically known
}
```

A checked expression carries either an ordinary value or a pipeline, never
both. A pipeline may be consumed only by another pipeline adaptor,
`to_list(heap)`, `reduce(initial, combine)`, or a `for` source. It is never
converted to `Operand`, because there is no runtime value to render or store.

Every other consumer rejects it at the checker boundary: binding, assignment,
argument, result, aggregate member, collection element, `match` scrutinee,
comparison, print, interpolation, address-taking, dereference, place use,
`defer`, `errdefer`, `spawn`, and any attempt inside `unsafe`. `unsafe` does not
turn compiler-only metadata into a runtime value.

This is intentionally less capable than a storable Zig-style iterator. It
avoids public iterator identity, copy state, lifetime rules, nested iterator
types, and user-visible function-pointer fields. First-class iterators require
a separate RFC with demonstrated demand.

### 5. Evaluation order

For one chain, evaluation occurs in this order:

1. evaluate the base source once;
2. evaluate each callback expression once, left to right;
3. evaluate terminal arguments once from left to right: the Heap for `to_list`,
   or the initial accumulator followed by the combiner for `reduce`;
4. create the result List when materializing, or initialize the accumulator
   when reducing;
5. traverse source elements in source order;
6. run adaptors in written order for each element;
7. perform the terminal action.

An empty source still evaluates the source, adaptor callbacks, and terminal
arguments, but invokes no per-element callback. A failed filter skips later
adaptors and the terminal action for that element. Traps and callback effects
occur in this traversal order. Dict order remains unspecified.

### 6. Mutation, lifetime, and control flow

Allocated List, inline List, and Dict pipelines preserve their source
`CollectionRoot` and structural version. Pipeline `for` passes that root into
the existing iteration-mutation scanner rather than creating an untracked
source temporary. The scanner applies to the whole body regardless of
`break`, `continue`, `return`, `defer`, or nested control flow.

Direct mutation/free that the checker can associate with the active source is
rejected. Remaining copied-handle structural mutation is detected by the same
before-body and after-body version guards as ordinary `for`. A version guard
is not a lifetime guard: freeing the source and then reading its version is
undefined C. Direct calls to entry-environment functions or methods whose
transitive capture includes the active source are conservatively rejected,
including read-only captures, so pipeline `for` cannot reach that invalid
post-free version read through a known call.

Slice pipelines retain Slice's programmer-managed lifetime contract. A
`Slice<mut T>` source does not make the callback parameter mutable; callbacks
receive immutable shallow copies. This RFC adds no thread safety. Concurrent
mutation from another Task remains an unsynchronized conflict; structural
version fields are not atomics.

Pipeline `for` otherwise inherits ordinary loop control flow. `break`,
`continue`, `return`, and per-iteration deferred cleanup use the same lowering
and semantics as an ordinary collection loop.

## Generated C contract

The compiler emits one source loop, not a runtime iterator. For an allocated
List:

```hexal
let result = values.filter(is_even).map(double).to_list(heap)
```

the conceptual current-ABI shape is:

```c
hex_list_Int32 *hex_pipeline_result = hex_list_new_Int32(hex_v_heap);
hex_list_Int32 *hex_pipeline_source = hex_v_values;
const size_t hex_pipeline_version = hex_pipeline_source->version;
for (size_t hex_pipeline_index = 0;
     hex_pipeline_index < hex_pipeline_source->length;
     hex_pipeline_index++) {
    if (hex_pipeline_source->version != hex_pipeline_version) {
        hex_runtime_trap("[Runtime Error] collection modified during iteration\n");
    }
    const int32_t hex_pipeline_value =
        *hex_list_at_Int32(hex_pipeline_source, hex_pipeline_index);
    if (hex_v_is_even(hex_pipeline_value)) {
        hex_list_push_Int32(hex_pipeline_result,
            hex_v_double(hex_pipeline_value));
    }
    if (hex_pipeline_source->version != hex_pipeline_version) {
        hex_runtime_trap("[Runtime Error] collection modified during iteration\n");
    }
}
```

Exact generated names follow the normal deterministic naming layer. The
contract is the pointer-shaped List ABI, one fused loop, no intermediate List,
and no iterator allocation or `next` dispatch.

A Dict key source scans the sparse table rather than pretending it is a
contiguous sequence:

```c
const hex_dict_Int32_Int32 *const hex_pipeline_source = hex_v_users;
const size_t hex_pipeline_version = hex_pipeline_source->version;
for (size_t hex_pipeline_bucket = 0;
     hex_pipeline_bucket < hex_pipeline_source->capacity;
     hex_pipeline_bucket++) {
    if (!hex_pipeline_source->buckets[hex_pipeline_bucket].active) continue;
    if (hex_pipeline_source->version != hex_pipeline_version) {
        hex_runtime_trap("[Runtime Error] collection modified during iteration\n");
    }
    const int32_t hex_pipeline_key =
        hex_pipeline_source->buckets[hex_pipeline_bucket].key;
    /* fused adaptors and terminal action */
    if (hex_pipeline_source->version != hex_pipeline_version) {
        hex_runtime_trap("[Runtime Error] collection modified during iteration\n");
    }
}
```

An `entries()` source reads both fields from the same active bucket into one
independent value before running adaptors:

```c
hex_t_DictEntry_String_Int32 hex_pipeline_entry = {
    .key = hex_pipeline_source->buckets[hex_pipeline_bucket].key,
    .value = hex_pipeline_source->buckets[hex_pipeline_bucket].value,
};
```

The illustrative name above is resolved through the normal generated-name
registry. The representation contract is the independent `{ key, value }`
value, not that exact unsuffixed spelling.

## Diagnostics

The checker owns pipeline errors at the earliest boundary that can prove them.
Existing function mismatch, unknown method, List-element eligibility,
entry-environment function-value, and `for` control-flow diagnostics remain
authoritative. This RFC adds only the identities that describe genuinely new
conditions:

| Key | Exact text | Use |
| --- | --- | --- |
| `type.lazy-pipeline-must-be-consumed` | `lazy pipeline must end in to_list(heap), reduce(initial, combine), or direct for iteration` | any escape or non-terminal use |
| `type.lazy-pipeline-callback-signature` | `pipeline callback must be a concrete non-rest Fun value` | callback category is invalid before ordinary parameter/result matching |
| `type.lazy-pipeline-for-binder-count` | `pipeline iteration requires exactly one binder` | zero or multiple pipeline binders |
| `type.lazy-pipeline-terminal-position` | `pipeline terminal must be a complete binding initializer, assignment source, or return expression` | nested `to_list` or `reduce` use |

The centralized `compiler/diagnostics` package owns these keys and messages.
Parser or generator fallback errors must never replace them.

## Rejected alternatives

| Alternative | Reason |
| --- | --- |
| Eager `keys(heap) -> List<K>` snapshots | allocates and copies before materialization is requested |
| `keys() -> Slice<K>` | Dict keys are sparse and interleaved with values |
| Public `Iterator<T>` plus `next` function pointer | adds runtime state, dispatch, storage, and lifetime rules |
| Public `MapIterator<Source, T, R>` families | expands the type surface and nested generic identities before demand exists |
| Coroutine/generator functions | requires frame and suspension lifetime machinery |
| Dict-only `map_keys`, `filter_keys`, and parallel value methods | duplicates every adaptor and composes poorly |
| Mandatory `.iter()` for every source | ceremony without ambiguity reduction |
| Automatic exact-size/preallocation analysis | unmeasured complexity; ordinary List growth is already correct |
| Implicit `try` in `map` | changes ordinary function-value and error semantics |
| Special direct-call callback optimization | creates callback semantics separate from ordinary `Fun` lowering |
| Both `collect(heap)` and `to_list(heap)` | two names for the same List materialization operation |

## Non-goals

- Closures or implicit callback capture.
- User-defined iterable protocols.
- First-class or storable iterator values.
- User construction or mutation of `DictEntry<K, V>`.
- Text pipelines.
- Index-aware adaptors or multiple pipeline `for` binders.
- Type refinement through `filter`.
- Fallible adaptor sugar.
- Infinite, parallel, or asynchronous traversal.
- In-place mutable element traversal.
- Guaranteed Dict order.
- Hidden default allocation.
- `flat_map`, `zip`, `enumerate`, `take`, `skip`, `fold`, `find`,
  `any`, `all`, or `count`.

## Fail-closed implementation map

The implementation must update these boundaries explicitly; a default branch
must not silently reinterpret a pipeline as an ordinary value:

1. checker postfix/method dispatch for source start, adaptor chaining, and
   terminal recognition;
2. `checkedExpression` and initializer diagnostics, keeping pipeline metadata
   disjoint from `Operand`;
3. every ordinary value consumer listed under Ephemeral checked
   representation;
4. `for` source classification, binder typing, collection-root propagation,
   and iteration-mutation scanning;
5. checked-tree walkers used by defer, control flow, observability, resource
   discovery, and source-span validation;
6. generator checked-form validation and dispatch;
7. statement-producing lowering for the three allowed complete-expression
   positions of `to_list` and `reduce`;
8. List, inline List, Slice, Dict-key, Dict-value, and Dict-entry traversal
   lowering;
9. runtime component discovery, where only `to_list` adds allocated-List
   requirements beyond those already required by source and callback types;
10. grammar and dispatch coverage guards, including a negative guard proving
    an unhandled pipeline category fails closed.

## Detailed implementation plan

### Phase 0: safety prerequisite and baseline

1. Resolve the recorded direct-call mutation/free defect under the selected
   conservative capture policy.
2. Reuse `analyzeEntryEnvironment`'s already-transitive `envCaptures` facts:
   for each direct function or method call in the body, resolve every captured
   name to its binding and reject when its `CollectionRoot` equals the active
   traversal root. Do not infer read/write effects.
3. Add ordinary-`for` regressions for direct and copied-handle mutation/free,
   including entry-environment function calls.
4. Freeze no-pipeline snippet hashes and ordinary iteration output as the
   behavioral baseline.

### Phase 1: central diagnostics and checked representation

1. Add the four diagnostic constructors and registry records above.
2. Add optional `checkedPipeline` metadata to `checkedExpression`; do not add a
   public Type or parser node.
3. Add canonical `DictEntry<K, V>` specialization to the existing type
   environment, protected-name registry, generic-arity registry, and
   generated-definition registry, with fixed `key` and `value` members and no
   constructor. Reuse the existing non-constructible-type rejection.
4. Recognize List/Slice starts and Dict `keys()`/`values()`/`entries()`
   projections in postfix checking.
5. Implement one consumption gate used by every ordinary value boundary.
6. Extend checker dispatch-coverage guards before adding successful lowering.

### Phase 2: callback and terminal checking

1. Check each callback expression once, in written order, through ordinary
   `Fun` rules plus the concrete/non-rest constraint.
2. Derive `map` result types; preserve the element type through `filter`.
3. Reject no-result, method, entry-environment, unresolved generic, nullable,
   union-held, and mismatched callbacks with the owning central diagnostic.
4. Check `to_list`'s Heap and allocated-List element eligibility separately
   from direct-`for` binder eligibility.
5. Admit exactly one binder for pipeline `for` and preserve the base
   collection root.
6. Check `reduce`'s initial value and exact `Fun<(R, T): R>` combiner without
   adding empty-source or implicit-error semantics.
7. Apply ordinary aggregate equality and print eligibility to `DictEntry`, and
   keep it excluded from Dict key eligibility. Extend builtin-registry
   completeness guards so the protected type cannot be added to only one
   registry.

### Phase 3: source traversal lowering

1. Refactor only the minimum existing ordinary-`for` traversal helpers needed
   to share List, inline List, Slice, and Dict scanning without copying their
   semantics.
2. Evaluate and capture each source once. Iterate inline places in place and
   materialize only inline temporaries.
3. Scan Dict capacity, skip inactive buckets, and select only the requested
   key or value field, or copy both fields into one independent `DictEntry`.
4. For direct Dict `to_list`, inject and push each active key followed by its
   value into the canonical `K | V` result element; use `K` directly when both
   parameters are the same type.
5. Preserve before-body and after-body structural version guards.

### Phase 4: fusion and terminals

1. Evaluate adaptor callbacks once left-to-right, then terminal arguments in
   written order.
2. Emit map temporaries and filter branches in written order inside one loop.
3. Lower direct `for` through ordinary body/control-flow emission.
4. Lower `to_list` using one ordinary allocated List and ordinary push/growth,
   only in a complete binding initializer, assignment source, or return.
5. Lower `reduce` to one accumulator local updated after all preceding
   adaptors accept an element, under the same position restriction.
6. Select no iterator runtime component and emit no intermediate collection.

### Phase 5: sweep and documentation

1. Add checker and generator completeness guards for every new checked form.
2. Add deterministic discovery and emission coverage for each used
   `DictEntry<K, V>` specialization, including the existing recursive
   module-owned/shared ownership classification; emit none when `entries()` is
   unused.
3. Delete no ordinary collection iteration path; pair iteration remains the
   direct Dict key/value operation.
4. Verify `GRAMMAR.ebnf` remains unchanged because the syntax is existing
   postfix calls. Update it only if that verification fails.
5. Synchronize the canonical reference's Functions, List, Slice, Dict,
   `DictEntry`, `for`,
   evaluation-order, allocation, diagnostics, and generated-C contracts.
6. Regenerate the snippet manifest only for new pipeline snippets; no existing
   snippet hash may change unless the chosen safety prerequisite legitimately
   changes its diagnostics or output.
7. Rebuild `hexal` and restart the workbench through `hexal play`.

## Exhaustive validation

1. Allocated List, inline List place, inline List temporary, Slice,
   `Slice<mut T>`, Dict keys, Dict values, and Dict entries each feed `map`,
   `filter`, both chain orders, `to_list`, `reduce`, and direct `for`.
2. `for key in dict.keys()`, `for value in dict.values()`, and
   `for entry in dict.entries()` compile and visit only the requested active
   projection. Entry iteration observes the same bucket order and key/value
   pairing as ordinary `for key, value in dict`.
3. `Slice<T>.to_list(heap)` and `Slice<mut T>.to_list(heap)` return exact
   `List<T>` shallow copies.
4. `Dict<String, Int32>.to_list(heap)` returns exact
   `List<String | Int32>` with each visited key immediately followed by its
   value. `Dict<Int32, Int32>.to_list(heap)` returns `List<Int32>` without a
   duplicate-member union.
5. `Dict<String, Int32>.entries().to_list(heap)` returns exact
   `List<DictEntry<String, Int32>>`; every entry has the key and value copied
   from one active bucket.
6. `DictEntry<K, V>` requires exactly two type arguments, is protected from
   redeclaration and shadowing, has fixed `key` and `value` fields, cannot be
   constructed or used as a Dict key, follows ordinary aggregate
   equality/print eligibility, and exposes no bucket `active` state.
7. Dict projections scan one captured handle once, visit active buckets only,
   and allocate no snapshot.
8. Source, adaptor callbacks, and terminal arguments each evaluate exactly once
   in the specified order.
9. Empty and all-rejected sources call only the callbacks required by written
   order and materialize to an empty List.
10. `to_list` uses ordinary List growth, returns exact `List<R>`, and preserves
   accepted order.
11. `to_list` and `reduce` are accepted as complete binding initializers,
   assignment sources, or return expressions and reject with
   `type.lazy-pipeline-terminal-position` when nested in a call, operation,
   constructor, `match`, `try`, or interpolation.
12. `reduce` returns the explicit initial value for an empty source, accepts an
   accumulator type different from the element type, preserves traversal
   order, and requires exact `Fun<(R, T): R>` parameter/result agreement.
13. A mapped or reduced `R | Error` remains data; no implicit `try` occurs. The ordinary
   loop plus explicit `try` idiom compiles.
14. Direct `for` accepts element types permitted by ordinary binders even when
   `to_list` rejects the same final type as an illegal List element.
15. No-result, rest, method, entry-environment, unresolved generic, nullable,
   union-held, wrong-parameter, and wrong-result callbacks fail with their
   owning stable diagnostic.
16. A concretely specialized generic `Fun` binding is accepted.
17. Capturing anonymous functions retain their existing no-closure diagnostic.
18. `filter` over `T | Nil` remains typed `T | Nil`.
19. Pipeline machinery allocates nothing before `to_list`; an allocating
    callback remains allowed and its allocation is not attributed to the
    pipeline.
20. Structural mutation/free through direct names, copied handles, arguments,
    and nested control flow never reaches undefined C. A direct call to an
    entry-environment function or method that captures the active source is
    rejected, including when that helper is read-only.
21. Pipeline `for` preserves collection roots and ordinary `break`,
    `continue`, `return`, and deferred-cleanup behavior.
22. Concurrent source mutation is not presented as supported or made safe by
    version fields.
23. Binding, assignment, argument, return, object member, collection element,
    `match`, comparison, print, interpolation, address/place use, cleanup,
    Task capture, and `unsafe` each reject an unconsumed pipeline with
    `type.lazy-pipeline-must-be-consumed`.
24. Multiple pipeline binders reject with
    `type.lazy-pipeline-for-binder-count`; no index is exposed.
25. Text sources and unknown adaptors fail in the checker, never generation.
26. Generated List C uses the current pointer ABI, one source loop, no
    intermediate List, and no iterator allocation or `next` dispatch.
27. Generated Dict C scans capacity, checks `active`, selects the requested
    projection, copies entries into an independent two-field value, preserves
    version guards, and emits each entry specialization once in the owner
    selected by the existing recursive structural-type classification.
28. Callback calls use ordinary `Fun` lowering; the spec makes no direct-call
    or inlining claim.
29. Programs with no pipeline operation preserve existing generated artifacts,
    except any diagnostic-only fixture changed by the safety prerequisite.
30. Ordinary tests pass. Tagged C23 fixtures compile and execute each source,
    all three terminals, loop control flow, sparse Dict traversal, callback
    effects, and mutation traps/rejections.

## Open questions

None.
