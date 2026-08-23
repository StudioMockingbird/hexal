# RFC 0115: Iterator Invalidation

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Closed; implemented and verified 2026-08-23
- Updated: 2026-08-23
- Implemented: 2026-08-23
- Closed: 2026-08-23
- Features: defined mutation behavior during built-in collection traversal
- Created: 2026-08-22
- Depends on: RFC 0020 (collections), RFC 0063 (collection surface), RFC 0087
  (cached text length), and `docs/reference.md`
- Coordinates with: generated collection runtimes and language-surface audit
  finding F6
- Accepted cost: one `size_t` field on List and Dict, one increment per
  structural mutation, and one compare per iteration where safety is not proven

## Problem

The current reference permits List element replacement while iterating, but
leaves structural List changes and every Dict mutation as programmer
responsibility. That is an undefined-behavior hole because aliases can mutate
the collection through a different binding.

The hole has two severities, and this RFC closes both:

```hexal
for v in xs do
    xs.push(v)          -- length grows every iteration: an infinite loop.
end                     -- Wrong, but memory-safe and visible to the author.

for v in xs do
    xs.free(h)          -- the next iteration reads freed memory.
end                     -- Memory-unsafe.

for k, v in table do
    table.insert(k2, v) -- rehashing invalidates the bucket cursor: entries are
end                     -- skipped or repeated, and the freed bucket array can
                        -- be read. Memory-unsafe.
```

**Structural growth traps as well, and that is deliberate.** Some languages
define append-during-iteration; Hexal does not, because the traversal boundary
is captured from a length that the append changes, and defining it would mean
choosing between "see the new elements" and "do not" for every collection
operation. Trapping is one rule instead of a table of them. This is the
behavior a reader is most likely to be surprised by, so it is stated here
rather than left to be discovered.

## Why a runtime check is the right cost

Goal 15 says no runtime overhead and goal 16 says no undefined behavior. They
appear to conflict here. They do not, because **the language already made this
exact trade** — `hex_list_at_<T>` traps today:

```c
static inline const int32_t *hex_list_at_Int32(const hex_list_Int32 *list, size_t index) {
    if (index >= list->length) {
        hex_runtime_trap("[Runtime Error] list index out of bounds\n");
    }
    return &list->data[index];
}
```

Goal 15 has never meant "no runtime checks". It means no garbage collector, no
reference counting, no vtables, no hidden allocation. Hexal already pays a
compare per index access to buy defined behavior, and RFC 0088 exists precisely
to *elide* the checks that provably cannot fire while keeping the ones that can.
The proven-safe elision rule below is that same shape applied to the same kind
of check.

The per-iteration cost is also smaller than it appears: the version field sits
beside `length`, which the loop condition already loads on every iteration, so
the check is a warm load and a predictable compare.

**The zero-cost alternative is not available.** A traversal that borrows its
source exclusively would need alias exclusivity, but the current manual-memory
model permits copied handles to survive and mutate. A checker-only rule
therefore leaves the case that matters undefined:

```hexal
fun helper(ys: List<Int32>, h: Heap) do
    ys.free(h)
end
fun demo(h: Heap) do
    xs: List<Int32> := List<Int32>.new(h)
    for v in xs do
        helper(xs, h)   -- the checker cannot see through the call
    end                 -- use-after-free on the next iteration
end
```

Accepting that would require deleting goal 16's unqualified claim, not merely
this rule.

## Generated C, before and after

Today:

```c
const hex_list_Int32 *const hex_for_1 = hex_v_xs;
for (size_t hex_for_1_index = 0; hex_for_1_index < hex_for_1->length; hex_for_1_index++) {
    const int32_t hex_v_v = *hex_list_at_Int32(hex_for_1, (size_t)(hex_for_1_index));
```

After, for a traversal whose safety is not proven:

```c
const hex_list_Int32 *const hex_for_1 = hex_v_xs;
const size_t hex_for_1_version = hex_for_1->version;
for (size_t hex_for_1_index = 0; hex_for_1_index < hex_for_1->length; hex_for_1_index++) {
    if (hex_for_1->version != hex_for_1_version) {
        hex_runtime_trap("[Runtime Error] collection modified during iteration\n");
    }
    const int32_t hex_v_v = *hex_list_at_Int32(hex_for_1, (size_t)(hex_for_1_index));
```

A proven-safe traversal emits the first form unchanged.

## Rule

Safe Hexal never continues a built-in traversal after its source's structure
has changed.

- Array and View traversal has a fixed boundary. Element replacement is valid;
  there is no structural resize operation.
- List traversal captures the source's structural version. `push`, `pop`,
  `clear`, `free`, and any operation that changes storage or length invalidate
  the traversal.
- Dict traversal captures the source's structural version. `insert`,
  replacement, `remove`, `free`, and any bucket/topology change invalidate the
  traversal.
- Mutation through any alias observes and updates the same version because
  copied collection handles refer to the same collection state.
- A traversal checks its version immediately before each iteration body. The
  next body's check covers the transition to the next element; no duplicate
  check is required at the loop increment.
- A mismatch is a defined runtime trap with the message
  `[Runtime Error] collection modified during iteration`.
- The checker may reject a mutation at compile time when local ownership or
  provenance proves that an active traversal would be invalidated.
- When the checker cannot prove that a structural mutation is safe, the
  program remains valid and the generated version check supplies the defined
  runtime behavior. It must not silently become unsafe.
- Freeing the traversed List or Dict, or an alias that refers to it, is always
  rejected while the traversal is active. The checker must reject a direct or
  locally traceable free before it can invalidate the captured loop state.
- A traversed List or Dict, or an alias that refers to it, may be passed to a
  call during the traversal only when the checker proves that the call cannot
  structurally mutate or free that collection. An unproven call is rejected;
  the checker must not rely on a version check after a possible free.

The checker may elide a version check only when it proves that no operation in
the traversing scope or any reachable call can mutate the source (proven-safe
elision only). Absence of a locally visible mutation is not sufficient when a
called function could mutate through an alias; in that case the check is
retained. Elision is an optimization, not a semantic distinction.

## Interaction with `for ... in`

- The source expression is still evaluated once before the first iteration.
- The captured boundary and structural version are independent: a stable
  length does not make a changed List or Dict topology valid.
- Nested traversals capture independent versions.
- `break`, `continue`, `return`, `try`, and cleanup preserve the ordinary
  traversal lifetime rules.
- A traversal over a temporary source retains that source for the traversal's
  duration; it cannot observe mutation after the temporary becomes unreachable.

## C23 lowering

- List and Dict runtime state carries a monotonic structural version of type
  `Size` (the target's `size_t`). The version is incremented on every
  structural change. `Size` is the correct type because it is already the
  collection length/capacity type and is the only target-sized counter
  available without adding a new runtime type.
- Generated traversal state stores the captured `Size` token and checks it
  immediately before each body. No generated path checks a freed collection.
- The version need not be exposed as a Hexal field or be ABI-stable.
- Version overflow wraps modulo `2^N` where `N` is `sizeof(size_t)*8`. **A
  wrapped version that coincides with a live traversal's captured token is an
  accepted false negative, not a detected condition.** Detecting it would
  require each collection to enumerate its live traversals — a per-collection
  registry of active tokens, with registration and deregistration on every
  traversal entry and exit, including through `break`, `return`, `try`, and
  cleanup paths. That is materially more runtime state than one `Size` field,
  and it would be paid by every traversal to catch a case the same paragraph
  says never occurs: `2^32` structural mutations during one live traversal on a
  32-bit target, `2^64` on a 64-bit one.

  The runtime cost is therefore exactly one `Size` compare per check and one
  increment per structural mutation. Nothing else is added, which is what goal
  15 requires.

  An earlier revision of this section required the wrap to trap. That is
  withdrawn: it specified an observable behavior whose implementation the
  lowering did not describe and whose cost the RFC did not account for.
- A proven-safe traversal may omit the check, but only under the proven-safe
  elision rule above. The generated C must retain the same iteration order and
  element semantics.

## Non-goals

- User-defined iterators.
- Making concurrent mutation safe; data races remain governed by concurrency
  rules.
- Rejecting Array element replacement.
- Specifying a stable Dict iteration order.
- Allowing a traversal to retain a freed or reset source.

## Validation

This section is exhaustive. RFC 0115 is complete only when every item below
passes:

- Array and View element replacement during traversal remains valid.
- List `push`, `pop`, and `clear` during traversal are rejected when locally
  provable and otherwise produce the defined runtime trap. Free of the List,
  or an alias to it, is always rejected while the traversal is active.
- Dict insert, replacement, and remove during traversal are rejected when
  locally provable and otherwise produce the defined runtime trap. Free of the
  Dict, or an alias to it, is always rejected while the traversal is active.
- Passing the traversed collection or an alias to an unproven call is rejected
  rather than relying on a post-call runtime check.
- Mutation through a copied collection handle invalidates the original
  traversal.
- `push` during a List traversal traps rather than extending or terminating the
  traversal, and the trap message is the collection-modified one rather than a
  bounds message. This is the case a reader is most likely to expect to work.
- A mutation after `break` or after the traversal's scope exits is valid when
  no separate lifetime rule rejects it.
- Nested traversals maintain independent captured versions.
- Generated C emits the version state and one check immediately before each
  iteration body, or omits checks only for a proven-safe traversal. No
  generated path checks a freed collection.
- No live-traversal registry, traversal list, or per-traversal registration is
  emitted. A version wrap coinciding with a captured token is an accepted false
  negative; nothing detects it.
- `hex_list_<T>` and `hex_dict_<K>_<V>` each gain exactly one `size_t` field.
  The snippet manifest therefore moves for every snippet reaching a List or
  Dict, whether or not it iterates one, because the struct layout changes.
  Confirm the movement is confined to those snippets.
- Repeated compilations produce identical generated artifacts.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes, replace the current “programmer
responsibility” iterator rule in `docs/reference.md` with the compile-time and
runtime contract above, including the trap message and generated collection
state requirements.
