# RFC 0051: Stream Extensions

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; cancellation approved by RFC 0064
- Features: fallible stream steps, additional sources, terminal operations,
  producer cleanup callbacks, and allocator-backed nodes
- Created: 2026-08-13
- Depends on: RFC 0031 (`Stream<T>`, closed), RFC 0029 (Error and `try`), RFC
  0040 (File I/O), RFC 0027 (Arena and Pool, draft), and RFC 0035 (C-style
  copying and manual lifetimes)
- Coordinates with: `docs/reference.md`

## Purpose

RFC 0031 shipped `Stream<T>` with a deliberately small surface: empty `new`,
`produce`, `List<T>.stream`, `next`, `filter`, `map`, `take`, `for` iteration,
and `free`. Its Deferred section listed the rest.

RFC 0031 is closed and immutable, so its deferred list has no owner. This RFC
takes ownership. It proposes nothing yet — it records what was deferred, why,
and what each item must settle before it can be designed.

`Channel<T>` also appeared on that deferred list and is **already implemented**
by RFC 0037. It is not part of this RFC.

## Current surface

Unchanged and authoritative in `reference.md`:

```text
Stream<T>.new() -> Stream<T>
Stream<T>.produce(heap, state, callback) -> Stream<T>
List<T>.stream(heap) -> Stream<T>
next() -> T | EoS
filter(heap, predicate) -> Stream<T>
map(heap, mapper) -> Stream<U>
take(heap, count) -> Stream<T>
free(heap)
```

`Stream<T>` is lazy, single-pass, single-threaded, and has no length, capacity,
random access, or rewind. Adapters own their upstream by convention. One chain
uses one Heap.

## 1. Fallible steps

A producer that can fail has no way to report it. Today a fallible source must
make `Error` an ordinary element (`Stream<Config | Error>`), which forces every
consumer to narrow each value and gives adapters no way to stop on failure.

Must settle:

- whether `next()` gains a third outcome or the element union carries it;
- whether `filter`/`map`/`take` short-circuit on a failed step or propagate it
  as an ordinary value;
- how a failed step interacts with `for`, which currently stops only at `eos`;
  and
- whether a failed stream is exhausted, resumable, or unspecified afterward.

The ordinary-`Error`-element form must stay valid; this cannot become the only
way to express a fallible sequence.

## 2. Additional sources

Deferred: File lines and chunks, sockets, `String` runes, `Dict` entries, and
numeric ranges.

Must settle:

- file and socket sources require item 1, since both fail mid-iteration;
- a `String`-rune source overlaps `RuneCursor` and direct `for` iteration, so it
  needs a reason to exist beyond adapter composition;
- a `Dict`-entry source must state its behavior under the existing rule that
  every Dict mutation invalidates traversal; and
- ranges are listed under Excluded features in `reference.md`, so a range source
  is blocked until that decision changes.

Each source must also state who keeps the underlying resource alive, since
`produce` has no cleanup callback (item 4).

## 3. Terminal operations

Deferred: `collect`, `fold`, `find`, `any`, `count`, plus `flat_map`, `chain`,
`zip`, chunks, windows, and deduplication.

Must settle:

- `collect` needs a destination and an allocator, and must state whether it
  frees the chain it drains;
- `fold` needs an accumulator that is copyable under `reference.md`'s rules and
  a non-capturing function, since Hexal has no closures;
- `find`/`any` terminate early, so they must state whether the partially drained
  chain remains usable; and
- `zip` and `chain` consume two chains with independent Heaps, which the
  one-chain-one-Heap rule currently forbids.

Every one of these is expressible today with an explicit `for` loop. Each needs
a reason it earns language surface.

## 4. `produce_with_cleanup`

RFC 0031 sketched this and deferred it:

```text
Stream<T>.produce_with_cleanup(heap, state, next, destroy) -> Stream<T>
```

It would let producer State own an external resource — a File, a socket — by
running `destroy` exactly once on first exhaustion or on `free`.

Must settle:

- whether cleanup runs on exhaustion, on `free`, or on whichever comes first;
- what happens when cleanup itself fails, given `close` currently traps; and
- whether an adapter chain propagates cleanup down to its upstream producer.

RFC 0031 required that a concrete I/O stream justify this first. Item 2 is that
justification, so items 2 and 4 should be designed together.

## 5. Allocator-backed nodes

Deferred until RFC 0027 defines Arena and Pool. Stream nodes are per-chain and
freed together, which is the shape Arena serves well.

Must settle: the reset interaction. An Arena reset invalidates every node at
once, which conflicts with the current rule that `free` releases the chain
exactly once. Blocked on RFC 0027.

## Non-goals

- Making `Stream<T>` concurrent, buffered, or rewindable. `Channel<T>` owns
  concurrent communication.
- Generator or `yield` syntax.
- Closures. Every callback stays a named non-capturing function.
- Pipeline fusion, which remains an optimization that must preserve produced
  values, pull order, and cleanup responsibility.
- Changing the existing eight operations.

## Readiness

Not ready for design. Item 1 gates items 2 and 3; item 4 depends on item 2;
item 5 is blocked on RFC 0027. Nothing here should be implemented until a
concrete program needs it — the current surface has no demonstrated gap.

## Open questions

Everything above. This RFC is a placeholder with a scope, not a design.
