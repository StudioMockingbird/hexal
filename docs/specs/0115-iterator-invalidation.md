# RFC 0115: Iterator Invalidation

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Draft; design proposed, implementation not started
- Features: defined mutation behavior during built-in collection traversal
- Created: 2026-08-22
- Depends on: RFC 0020 (collections), RFC 0063 (collection surface), RFC 0087
  (cached text length), and `docs/reference.md`
- Coordinates with: affine ownership RFC 0110, generated collection runtimes,
  and language-surface audit finding F6

## Problem

The current reference permits List element replacement while iterating, but
leaves structural List changes and every Dict mutation as programmer
responsibility. That is an undefined-behavior hole because aliases can mutate
the collection through a different binding.

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
- A traversal checks its version before each iteration body and before any
  operation that advances the traversal.
- A mismatch is a defined runtime trap with the message
  `[Runtime Error] collection modified during iteration`.
- The checker may reject a mutation at compile time when local ownership or
  provenance proves that an active traversal would be invalidated.
- When the checker cannot prove safety, the program remains valid and the
  generated version check supplies the defined runtime behavior. It must not
  silently become unsafe.
- Collection free, Arena reset, Pool destruction, or affine move invalidation
  during an active traversal is rejected when locally decidable and otherwise
  traps through the corresponding lifetime rule before traversal continues.

The checker may elide a version check only when it proves that no operation in
the traversing scope or any reachable call can mutate the source. Elision is an
optimization, not a semantic distinction.

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

- List and Dict runtime state carries a monotonic structural version or an
  equivalent invalidation token.
- Generated traversal state stores the captured token and checks it at the
  defined points.
- The version need not be exposed as a Hexal field or be ABI-stable.
- Version overflow must not make an old traversal appear valid. The runtime
  uses a checked generation strategy or treats exhaustion as a trap.
- A proven-safe traversal may omit the check, but the generated C must retain
  the same iteration order and element semantics.

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
- List `push`, `pop`, `clear`, and free during traversal are rejected when
  locally provable and otherwise produce the defined runtime trap.
- Dict insert, replacement, remove, and free during traversal are rejected
  when locally provable and otherwise produce the defined runtime trap.
- Mutation through a copied collection handle invalidates the original
  traversal.
- A mutation after `break` or after the traversal's scope exits is valid when
  no separate lifetime rule rejects it.
- Nested traversals maintain independent captured versions.
- Generated C emits the version state and checks exactly where required, or
  omits checks only for a proven-safe traversal.
- Repeated compilations produce identical generated artifacts.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes, replace the current “programmer
responsibility” iterator rule in `docs/reference.md` with the compile-time and
runtime contract above, including the trap message and generated collection
state requirements.
