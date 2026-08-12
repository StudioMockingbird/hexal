# RFC 0031: Pull Streams

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-11
- Features: lazy single-pass `Stream<T>`, explicit pull with `next`, `EoS`,
  custom producers, List sources, lazy `filter`/`map`/`take`, `for` iteration,
  and explicit cleanup
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0008 (functions and methods), RFC 0014 (unions), RFC 0019
  (generics), RFC 0020 (collections), RFC 0026 (allocation and defer), RFC 0028
  (`for` iteration), RFC 0035 (C-style copying and manual lifetimes), and RFC
  0036 (`Size`)
- Coordinates with: RFC 0022 (match and narrowing), RFC 0029 (Error values),
  and the future I/O and concurrency specifications
- Replaces within this draft: the earlier bounded concurrent-channel proposal;
  concurrent communication is deferred to a distinct future `Channel<T>` type

## Summary

`Stream<T>` is a lazy, single-pass pull sequence. It stores a producer and the
state required to produce the next value:

```seawitch
step: Int32 | EoS = numbers.next()
```

The primitive operation is:

```text
stream.next(): T | EoS
```

`EoS` means normal end of stream. A Stream is not a collection: it has no
random access, mutation, length, capacity, rewind, or repeated traversal.

Direct collection iteration remains preferable for one eager pass:

```seawitch
for value in values do
    inspect(value)
end
```

Streams are intended for lazy adapters, procedural sources, parsers,
tokenizers, external input, large sequences, and potentially infinite sources.

## Goals

1. Provide one lazy, pull-based `Stream<T>` abstraction.
2. Keep the public API small: construction, `next`, `filter`, `map`, `take`,
   iteration, and explicit `free`.
3. Use ordinary named non-capturing functions instead of closures, generators,
   or coroutine syntax.
4. Follow RFC 0035's C-style shallow copying and programmer-managed lifetimes.
5. Require explicit allocator passing for every operation that allocates.
6. Lower to readable C23 with one pointer-sized Stream handle.

## Non-goals

- Thread safety, concurrent send/receive, buffering, close, or synchronization.
- Random access, length, capacity, mutation, or rewind.
- Compiler-enforced ownership, moves, borrows, or automatic destruction.
- Generator or `yield` syntax.
- Fallible Streams, asynchronous pulling, cancellation, or timeouts.
- Pipeline fusion in v1.
- Automatically freeing allocations referenced by produced values or producer
  state.

## End-of-stream value

`EoS` is a built-in singleton type whose sole value is `eos`:

```seawitch
step: Int32 | EoS = stream.next()
```

It is distinct from `Nil`, Error, and every element value, so a Stream may
produce optional values without making completion ambiguous:

```seawitch
stream: Stream<Int32 | Nil>
step: Int32 | Nil | EoS = stream.next()
```

`EoS` participates in ordinary type expressions, unions, `is` tests, match type
patterns, and narrowing. Two direct `EoS` values compare equal under ordinary
equality; this RFC adds no special comparison between a union and one of its
members. Stream introduces no special result-union machinery:

```seawitch
while true do
    step: Int32 | EoS = stream.next()

    if step is EoS
        break
    end

    consume(step) // narrowed to Int32
end
```

`Stream<EoS>` and a Stream whose normalized top-level element union already
contains `EoS` are rejected because the produced-value and completion
alternatives would be indistinguishable:

```seawitch
bad: Stream<Int32 | EoS> // Error
```

An object or ADT payload may contain an EoS member because the outer value still
has a distinct T tag:

```seawitch
type Packet = { marker: EoS }
valid: Stream<Packet>
```

`eos` is a reserved literal token. `EoS` is a protected built-in type name and
cannot be redeclared or shadowed. Like every non-Nil value, `eos` is truthy;
programs test its type rather than relying on Boolean context.

Standalone `EoS` lowers to a one-byte compiler-owned C value. In `T | EoS`,
the EoS alternative has a tag and no payload field. EoS supports equality with
EoS but is not ordered, hashable, printable by default, or valid as a Dict key.

The first exhausted pull marks the Stream exhausted. Every later `next()` call
returns `eos` without invoking producer code again.

## Type and representation

`Stream<T>` is a built-in generic reference type. A source value is one
pointer-sized handle to a heap object. This matches String, List, and Dict
rather than the earlier Pixel stack-header design.

Every allocated Stream object begins with a common generated header:

```c
typedef struct sw_stream_ops_Int32 {
    bool (*next)(void *object, int32_t *out);
    void (*destroy)(void *object);
} sw_stream_ops_Int32;

typedef struct sw_stream_Int32 {
    const sw_stream_ops_Int32 *ops;
    uintptr_t allocator;
    bool exhausted;
    /* source- or adapter-specific state follows */
} sw_stream_Int32;
```

The concrete object combines the header and producer or adapter state in one
allocation. Operations tables are immutable `static const` data shared by all
instances of one generated source or adapter shape.

The internal pull ABI is:

```c
bool next(void *object, T *out);
```

`object` points to the complete concrete Stream allocation whose first fields
are the common header. `true` initializes `out` with one shallow C-style `T`
copy. `false` leaves `out` uninitialized and means `eos`. The public `T | EoS`
value is an ordinary inline tagged union and never allocates.

## C-style copying and lifetime

Stream handles follow RFC 0035:

```seawitch
stream: Stream<Int32> = make_numbers(h)
alias: Stream<Int32> = stream

first: Int32 | EoS = stream.next()
second: Int32 | EoS = alias.next() // pulls from the same producer
```

Assignment, argument passing, return, and aggregate storage copy the handle.
All aliases share one cursor and exhausted state. The compiler tracks no owner
and inserts no cleanup.

The programmer must call `free` exactly once for each allocated Stream object
or final adapter chain and must stop using every alias afterward:

```seawitch
defer stream.free(h)
```

Use after free, repeated free, wrong-allocator free, and freeing while another
call uses the Stream are programmer errors under RFC 0035.

Stream operations copy values shallowly. If `T` or producer State contains a
String, List, pointer, or another reference-like value, Stream copies only that
handle. It never implicitly clones or frees the referenced allocation.

## Empty Stream

An empty Stream uses:

```seawitch
empty: Stream<Int32> = Stream<Int32>.new()
```

`new()` allocates nothing and returns a canonical type-specialized empty
handle. `next()` always returns `eos`. `empty.free(h)` is a no-op; the allocator
argument is accepted so generic cleanup code uses one Stream cleanup spelling.

Calling `new` with an allocator or capacity is rejected. Stream is not a
channel and has no buffer capacity.

## Custom producer

A custom producer combines inline State with a named callback:

```seawitch
type Counter = {
    mut current: Int32,
    limit: Int32,
}

fun counter_next(state: MutPtr<Counter>): Int32 | EoS
    if state.current >= state.limit
        return eos
    end

    result: Int32 = state.current
    state.current = state.current + 1
    return result
end

initial: Counter = Counter { current = 0, limit = 10 }
numbers: Stream<Int32> = Stream<Int32>.produce(h, initial, counter_next)
defer numbers.free(h)
```

The compiler-owned signature is:

```text
Stream<T>.produce(
    allocator: Heap,
    initial_state: State,
    next: Fun<(MutPtr<State>) : T | EoS>
): Stream<T>
```

`State` is inferred from `initial_state`. It must be complete, finite-sized,
and copyable under RFC 0035. `produce` makes one shallow copy of State into one
combined Stream allocation. The callback receives mutable access to that stored
copy for the duration of one pull.

Callbacks are named, non-capturing functions because Seawitch has no closures.
Values needed by a callback belong explicitly in State. The callback must not
retain or return its temporary State pointer and must not recursively call
`next` on the same Stream.

Generated Stream nodes may store the concrete callback as private C runtime
data. This does not add source-level `Fun<...>` object members or relax RFC
0008's restrictions on where users may store function values.

Freeing the Stream releases its Stream allocation. It does not recursively free
allocations referenced by State fields; the programmer must manage those
resources separately, normally with outer `defer` actions whose registration
order keeps them alive until after the Stream is freed.

V1 `produce` currently has no cleanup callback. A producer whose State refers
to an external resource may not safely outlive that programmer-managed
resource:

```seawitch
file: File = open_file("events.log")
defer close_file(file)

state: LineState = LineState { file = file }
lines: Stream<String> = Stream<String>.produce(h, state, read_line)
defer lines.free(h)
```

Reverse defer order frees the Stream node before closing the file. Returning
`lines` alone would be a programmer error because the file would not accompany
it. V1 deliberately provides no producer cleanup callback.

Allocation failure follows RFC 0026's existing unrecoverable allocation trap.

## List source

A List may provide a non-owning lazy Stream over its existing elements:

```seawitch
values: List<Int32> = List<Int32>.new(h)
defer values.free(h)

source: Stream<Int32> = values.stream(h)
defer source.free(h)
```

`List<T>.stream(h)` allocates one Stream object containing a shallow List handle,
an index, and the List length captured at construction. It does not copy or own
the List elements or List allocation.

The programmer must keep the List alive and must not structurally mutate it
until the Stream and every adapter above it have been freed. Element replacement
that preserves the List's storage and length is visible to later pulls.

Direct List iteration remains allocation-free and should be preferred when no
lazy adapter chain is needed. Consuming `into_stream` is not included because
Seawitch does not have compiler-enforced moves; a non-owning source maps more
directly to C.

## Primitive operations

The compiler-owned v1 API is:

```text
Stream<T>.new(): Stream<T>
Stream<T>.produce(Heap, State, Fun<(MutPtr<State>) : T | EoS>): Stream<T>
List<T>.stream(Heap): Stream<T>

stream.next(): T | EoS
stream.filter(Heap, Fun<(T) : Bool>): Stream<T>
stream.map(Heap, Fun<(T) : U>): Stream<U>
stream.take(Heap, Size): Stream<T>
stream.free(Heap)
```

These names resolve as protected compiler built-ins before user methods and
cannot be redeclared for Stream or List.

There is deliberately no `length`, `capacity`, `send`, `receive`, `close`,
`reset`, or indexed operation.

## `next`

`next()` mutates producer state behind the reference-like handle and therefore
does not require a `mut` binding:

```seawitch
stream: Stream<Int32> = make_numbers(h)
step: Int32 | EoS = stream.next()
```

One call performs at most one successful public pull. A filter may invoke its
upstream repeatedly internally while rejecting values. The result is a shallow
value copy. If that value references an allocation, cleanup responsibility is
determined by the producer API and remains with the programmer.

`next()` is single-threaded and non-reentrant in v1. Concurrent or recursive
pulls on the same Stream object are invalid.

## Adapter ownership convention

Every adapter allocates a new Stream object and stores the upstream Stream
handle. The new adapter takes responsibility, by documented C-style API
convention, for freeing that upstream Stream object when the final chain is
freed:

```seawitch
source: Stream<Int32> = values.stream(h)
selected: Stream<Int32> = source.filter(h, is_even)
defer selected.free(h)
```

After successful adapter construction, the programmer must not call `next`, an
adapter, or `free` through `source` or any other upstream alias. The compiler
does not create a moved-from state or diagnose every violation. This is the
same ownership-by-API-contract convention used by C libraries.

Every node in one chain must use the same Heap in v1. Passing a different Heap
to an adapter or final `free` is a programmer error. Arena-backed Stream nodes
are deferred until RFC 0027 is implemented and its reset interaction is
specified.

## `filter`

```text
stream.filter(h, predicate: Fun<(T) : Bool>): Stream<T>
```

For each public pull, filter repeatedly pulls upstream until:

- the predicate returns true, in which case the shallow `T` value is returned;
- upstream returns `eos`, which is propagated; or
- an ordinary producer trap terminates execution.

A rejected value is discarded as a shallow C value. Stream does not free an
allocation referenced by it. A program producing newly allocated handles must
arrange cleanup explicitly in its producer or predicate contract.

## `map`

```text
stream.map(h, mapper: Fun<(T) : U>): Stream<U>
```

Map pulls one `T`, passes a shallow copy to the mapper, and returns the mapper's
`U` result. `U` is inferred from the named mapper's declared result type.
`eos` propagates without calling the mapper.

Map neither frees the input nor deep-copies the result. Any allocation cleanup
performed or transferred by the mapper follows the programmer-documented API
contract.

## `take`

```text
stream.take(h, count: Size): Stream<T>
```

Take requests at most `count` values from upstream. `take(h, 0)` returns `eos`
without pulling upstream. Reaching the count marks the adapter exhausted.
Upstream storage remains allocated until the final chain is explicitly freed.

## Manual pulling

Manual pulling uses ordinary union narrowing:

```seawitch
while true do
    step: Event | EoS = events.next()

    if step is EoS
        break
    end

    handle(step)
end
```

No special equality rule, special `match`, or implicit Error propagation is
added. A fallible source may use `Stream<Error | T>` and yield Error as an
ordinary element; a dedicated fallible-stream design is deferred.

## `for` iteration

RFC 0031 extends RFC 0028's iterable-source table:

| Source | Without index | With index |
|---|---|---|
| `Stream<T>` | `value` | `i, value` |

Iteration repeatedly calls `next()` until it receives `eos`:

```seawitch
for value in stream do
    consume(value)
end

for i, value in stream do
    print(i, ": ", value)
end
```

The optional `i: Size` counts produced values starting at zero. Filtered-out
upstream values do not increment it.

The Stream source expression evaluates exactly once and its handle is captured
once. Unlike RFC 0028's finite collections, Stream captures no initial length
or traversal boundary; it continues until `next()` returns `eos`. This is the
only RFC 0028 traversal-boundary rule superseded for Stream sources.

The loop does not consume or free the Stream handle automatically. On normal
exhaustion, `break`, `continue`, or `return`, ordinary RFC 0026 defers run, but
the programmer remains responsible for one explicit Stream cleanup:

```seawitch
stream: Stream<Int32> = make_numbers(h)
defer stream.free(h)

for value in stream do
    if done(value)
        break
    end
end

next: Int32 | EoS = stream.next() // valid: continues after the break
```

An allocating temporary used directly as the loop source cannot be freed after
the loop and therefore leaks under the C-style model:

```seawitch
for value in values.stream(h) do // programmer error: allocated Stream is lost
    consume(value)
end
```

Programs bind the final chain and register cleanup before iterating it. The
compiler does not insert hidden cleanup or ownership analysis to repair an
unbound allocating Stream expression.

Loop binders use RFC 0035's shallow C-style copy. Stream iteration is invalid
if another alias pulls or frees the same Stream during the loop body.
`continue` proceeds to the next pull. Per-iteration defer, `break`, and `return`
otherwise retain RFC 0028 and RFC 0026 behavior.

## Cleanup

`stream.free(h)` releases the complete Stream node chain exactly once:

1. call the outer node's generated destroy operation;
2. recursively free adapter-owned upstream Stream nodes;
3. release each Stream allocation through the matching Heap; and
4. perform no recursive cleanup of T values, producer State fields, or the
   source List allocation.

An empty Stream's cleanup is a no-op. An exhausted allocated Stream still needs
`free`; exhaustion stops producer calls but does not release its allocation.

This cleanup is explicit API behavior, not a general destructor mechanism.
Shallow aliases to any freed node become invalid under RFC 0035.

## Ordering and evaluation

- Each source and adapter expression evaluates once.
- Values preserve upstream order.
- Filter preserves the relative order of accepted values.
- Map preserves one output position per input position.
- Take preserves the first requested positions.
- A callback runs only when its node is pulled.
- Callback arguments and results follow ordinary function evaluation rules.
- Producer and adapter traps are not caught by Stream.

## C23 lowering

- Emit one handle and `T | EoS` tagged result representation per concrete T.
- Emit one `static const` operations table and focused helper family per
  concrete producer or adapter shape.
- Allocate each non-empty producer, List source, or adapter as one combined
  header-and-state object.
- Keep the canonical empty Stream allocation-free.
- Lower `next` through the type-specialized Boolean-plus-output internal ABI.
- Lower Stream `for` to a plain pull loop with a `Size` ordinal.
- Generate direct concrete copies; do not erase T behind `void *`.
- Use `void *` only for private producer-state dispatch inside generated C.
- Preserve source `#line` mappings for constructor, callback, adapter, pull,
  loop, and cleanup failures.
- Unsupported Stream nodes or callback shapes fail closed before emitting C.

V1 always uses the node path. Pipeline fusion may later remove nodes only as an
optimization preserving produced values, pull order, explicit cleanup
responsibility, and callback behavior.

## Diagnostics

Required focused diagnostics include:

```text
[Type Error] Stream element type cannot be EoS or include EoS as a top-level union member
[Type Error] Stream producer callback must return T | EoS
[Type Error] Stream producer callback must accept MutPtr<State>
[Type Error] Stream State must be complete and finite-sized
[Type Error] Stream predicate must have type Fun<(T) : Bool>
[Type Error] Stream mapper must accept T
[Type Error] Stream take count must be representable as Size
[Type Error] Stream is single-threaded and cannot be pulled recursively
[Type Error] Stream does not provide length or capacity
```

Wrong cleanup order, shallow-alias use after transfer/free, referenced-resource
lifetime errors, and concurrent pulls remain programmer errors under RFC 0035;
they are not ownership-checker diagnostics.

The parser owns malformed `Stream<T>` syntax only through the existing generic
grammar. The checker owns built-in operation recognition, callback signatures,
`EoS` exclusion, concrete types, and `for` source classification. Code
generation receives fully checked Stream facts and never infers semantics from
method names.

## Deferred

- `Channel<T>` for bounded concurrent communication, send/receive, close,
  synchronization, and blocking.
- Fallible Stream steps distinct from ordinary Error elements.
- File, socket, directory, String-rune, Dict-entry, and Range sources.
- Arena-backed nodes.
- `collect`, `fold`, `find`, `any`, `count`, `flat_map`, `chain`, `zip`, chunks,
  windows, and deduplication.
- Mutable element references and borrowed callback syntax.
- Async pulling, cancellation, timeouts, and selection.
- Generator and `yield` syntax.
- Pipeline fusion and devirtualization.
- User-defined Stream implementations outside `produce`.
- Producer-owned external-resource cleanup callbacks such as
  `produce_with_cleanup`; concrete I/O Streams should establish the need and
  exact contract first.

## Settled implementation contract

1. `Stream<T>` is a lazy single-threaded pull sequence. Future concurrent
   communication uses a distinct `Channel<T>` type.
2. Stream is a pointer-sized handle to one fully heap-allocated combined header
   and state object; the canonical empty Stream allocates nothing.
3. `EoS`/`eos` marks completion and uses ordinary union `is` narrowing.
4. List sources are non-owning and use `List<T>.stream(h)`; consuming
   `into_stream` is absent.
5. Adapters take upstream Stream cleanup responsibility by documented C API
   convention, without compiler-enforced moves.
6. Every allocating constructor and adapter accepts an explicit Heap, and all
   nodes in one chain use that Heap.
7. Stream iteration never frees the Stream automatically and permits continued
   pulling after `break`.
8. V1 includes exactly empty `new`, `produce`, List source, `next`, `filter`,
   `map`, `take`, `for`, and `free`.
9. Values and State use shallow C-style copying; Stream performs no implicit
   pointee cleanup.
10. Exhaustion marks an allocated Stream exhausted but does not release its
    allocation; explicit `free` remains required.
11. `produce` has no cleanup callback in v1. External resources referenced by
    State remain caller-owned and must outlive the Stream.

## Producer resource cleanup decision

Pixel could automatically destroy owned State. Seawitch has no destructors, so
v1 keeps `produce` small and provides no cleanup callback:

```seawitch
file: File = open_file(path)
defer close_file(file)

lines: Stream<String> = Stream<String>.produce(h, LineState { file = file }, read_line)
defer lines.free(h)
```

This is closest to C: the caller owns the file and must keep it alive. The
Stream cannot be returned alone because it does not own or close that file.

The following alternative is deferred rather than included in v1:

```seawitch
lines: Stream<String> = Stream<String>.produce_with_cleanup(
    h,
    state,
    read_line,
    close_line_state,
)
```

Its cleanup callback would need to run exactly once on first exhaustion or
early `free`, allowing the Stream to own external State resources by API
convention. It adds another callback contract, cleanup state, and constructor.
A concrete I/O Stream must justify and specify that addition later.
