# RFC 0050: Reference Contract Cleanup

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; reference update verified 2026-08-13
- Features: reference cohesiveness, explicit type eligibility, complete API
  contracts, uniform command results, exact print forms, syntax deduplication,
  and separation of language contracts from compiler internals
- Created: 2026-08-13
- Depends on: RFC 0008 (functions), RFC 0016 (numeric widening), RFC 0020
  (collections), RFC 0024 (comparison), RFC 0030 (`print`), RFC 0031
  (`Stream<T>`), RFC 0037 (concurrency), RFC 0040 (I/O), RFC 0044 (text), RFC
  0046 (storability and copyability)
- Coordinates with: RFC 0049 (Rune operator conformance) and
  `docs/reference.md`

## 1. Purpose

`docs/reference.md` is the sole normative syntax and semantic contract. It is
optimized for agentic development and exact rule lookup. The current document
is structurally sound but retains several ambiguous phrases, incomplete API
surfaces, duplicated syntax descriptions, and compiler-internal statements.

This RFC makes the reference:

- **cohesive:** every statement belongs to syntax, language semantics, runtime
  behavior, diagnostics, or observable C23 output;
- **coherent:** every type class, eligibility rule, result type, lifetime rule,
  and invalidation condition is explicit;
- **consistent:** local sections narrow shared rules and never silently broaden
  them; and
- **uniform:** equivalent APIs use equivalent signatures, result conventions,
  allocator placement, bounds rules, and lifecycle terminology.

## 2. Scope

This RFC authorizes one file change: `docs/reference.md`.

The cleanup records the intended language contract even where the current
compiler may disagree. Compiler, runtime, tests, `AGENTS.md`, `status.md`, and
archived specs are not changed. Any compiler disagreement remains a conformance
bug under the reference charter and is handled separately.

Part A clarifies settled behavior and closes incomplete reference rules. It
does not require conformance work in this RFC.

Part B normalizes infallible command operations to no result and restricts Nil
to union membership. These are approved language contracts. Compiler
conformance is outside this RFC.

Part C restructures the reference without removing any normative rule.

The resulting reference remains an agent-oriented contract:

- only precise grammar, semantic, API, runtime, diagnostic, and observable
  C23 contracts;
- no tutorials, illustrative programs, rationale, implementation status, or
  historical narrative; and
- abstract signatures and output templates only where they are themselves
  normative rules.

## 3. Reference writing contract

Every reference statement must satisfy one category:

| Category | Required content |
| --- | --- |
| Grammar | complete EBNF production or parser boundary constraint |
| Static semantics | acceptance, rejection, inference, conversion, or placement rule |
| Dynamic semantics | evaluation order, result, mutation, trap, lifetime, or invalidation rule |
| API contract | exact receiver, parameters, result, availability, and ownership effect |
| Runtime contract | scheduling, synchronization, I/O, allocation, or cleanup guarantee |
| Output contract | exact observable text or generated-C representation guarantee |
| Diagnostic contract | failure class and earliest externally relevant failure boundary |

The reference must not contain:

- RFC history or supersession narrative;
- implementation progress or known conformance gaps;
- tutorials, walkthroughs, motivation, or illustrative programs;
- compiler data structures, internal pass boundaries, or test strategy;
- an undefined type class such as “resource” or “pointer-like”; or
- a broad `any T` rule that ignores the shared position model.

Each rule appears once. Feature sections reference shared rules instead of
restating weaker or broader variants.

---

# Part A — semantic clarification

## 4. Numeric widening table

### Problem

The widening table lists `UInt64 -> none`, while the immediately following
target-range rule permits `UInt64 -> Size` when Size has the same range. The
table intends to list fixed-width destinations only, but does not say so.

### Required rule

Rename the table heading to:

```text
Fixed-width implicit destinations, excluding identity
```

Add this contract before the table:

- The table lists fixed-width destinations only.
- Size destinations are defined exclusively by the target-range rule below.
- A row containing `none` means no fixed-width destination, not no destination
  of any kind.

The Size rule remains:

- `U -> Size` when every value of unsigned fixed-width U fits Size.
- `Size -> U` when every Size value fits U.
- Equal ranges permit both directions.
- Equal-range binary common-type selection chooses Size.
- Canonical identities remain distinct.

Replace “signed/Size mixes generally have no common type” with the deterministic
target-range rule already established by RFC 0036:

- A signed fixed-width type never widens to Size because its domain contains
  negative values.
- Size widens to a signed fixed-width type S exactly when every Size value fits
  S.
- A Size/S binary operation uses S when that condition holds; otherwise it is
  rejected for lack of a lossless common type.
- Thus a 64-bit Size has no signed common type, while a narrower Size may have
  one with a wider signed type.

No widening pair changes.

## 5. Rune operator domain

### Problem

“Rune never widens implicitly” does not reject `Rune + Rune`; identical
operands require no widening. The operator domain must be explicit.

### Required rule

Add one rule to the operator section:

- Rune supports equality, ordering, and checked `to<T>()` conversion.
- Rune is invalid for `+`, `-`, `*`, `/`, `%`, unary `-`, `~`, `&`, `^`, `|`,
  `<<`, and `>>`.

RFC 0050 adds only the complete reference statement. RFC 0049 remains the
separate conformance specification.

## 6. Position eligibility must compose

### Problem

The shared position model correctly restricts `Fun<...>`, but the Task and
Stream sections use broader local rules:

- Task says R may be any complete copyable result.
- Stream says T and State need only be complete, finite, and copyable.

Both descriptions appear to admit Fun even though Fun is prohibited in Task
results, Stream elements, and Stream state.

### Required rule

Every generic built-in uses this validation order:

1. substitute concrete type arguments;
2. validate the type in the named storage position;
3. validate completeness and finite size;
4. validate copyability when the operation copies; and
5. apply feature-specific exclusions.

The shared position registry must include `Pointee` and `HeapAllocation`; it
must not present the existing list as exhaustive while defining those
positions elsewhere. Their rules are:

- `Pointee` requires a type permitted behind Ptr/MutPtr. Unknown is permitted
  there despite being incomplete. Existing handle/descriptor and Fun
  exclusions remain explicit.
- `HeapAllocation` requires a complete, finite type whose initializer can be
  passed to `Heap.allocate` under the copyability rules.

Replace the Task result rule with:

- R must be valid in both `FunctionResult` and `TaskResult` positions, complete,
  finite, and copyable.
- Atomic-containing and Fun results are therefore invalid through the shared
  rules; the Task section does not restate them as independent exceptions.

Replace the Stream rule with:

- T must be valid in `StreamElement`; State must be valid in `StreamState`.
- Both must be complete, finite, and copyable.
- T additionally cannot be EoS or a top-level union containing EoS.

Apply the same composition form to Array elements, View elements, List
elements, Dict values, Channel elements, function parameters/results, Task
arguments/results, and union members.

View wording must distinguish placement from provenance:

- View has no storage-position exception: it may occupy every otherwise valid
  storable position.
- The bans on rooting a View in temporary Array/List storage, addressing a
  View with `ref`, and returning a local-rooted View are provenance, address,
  and lifetime rules; they are not placement restrictions.

## 7. Define pointer-like

### Problem

The nullable-union layout says one pointer-like member plus Nil uses the null
niche, but “pointer-like” is undefined. Pointer-sized handles could be
misclassified.

### Required rule

Define a **pointer type** as a canonical type whose value is:

- `Ptr<T>`;
- `MutPtr<T>`; or
- `Fun<...>`.

Transparent aliases inherit canonical classification.

String, List, Dict, Stream, Task, Channel, Mutex, RuneCursor, and View are not
pointer types for union layout, even when their C representation contains one
or more pointers.

Nullable layout becomes:

- A normalized union containing exactly Nil and one pointer type uses the C
  null-pointer niche and has no tag.
- Every other union, including `String | Nil`, uses the general tagged-union
  representation unless another accepted rule explicitly defines a niche.

The Pointers section must separately state that Ptr/MutPtr nullability narrows
before dereference. The Function section must state that `Fun<...> | Nil` is a
nullable function pointer. Handle-plus-Nil unions are ordinary unions.

## 8. Atomic placement wording

### Problem

The shared position model implies that Atomic parameter/result types are
invalid, while the Atomic section describes only the attempted copy operation.
An agent could interpret such signatures as valid but uncallable.

### Required rule

- `Atomic<T>` and every inline aggregate containing Atomic are valid only in
  `Binding` and `ObjectMember` construction positions.
- They are invalid as declared function parameters/results, ADT payloads,
  union members, collection elements/values, Task arguments/results, Channel
  elements, Stream elements/state, direct Ptr/MutPtr pointees, and direct Heap
  allocation payloads.
- Direct `Atomic<T>.new(...)` construction initializes its final binding or
  object-member storage in place.
- Every operation that would copy existing Atomic state is invalid.
- Containment traversal stops at pointer and handle indirection.

An object containing Atomic remains addressable as an object; Ptr/MutPtr then
points to the enclosing object, not directly to the Atomic member. `ref` of the
Atomic value or member remains invalid. Direct `Heap.allocate<Atomic<T>>` is
invalid because Atomic construction is restricted to bindings and object
members.

`Ptr<Atomic<T>>` and `MutPtr<Atomic<T>>` are invalid type expressions wherever
written, including aliases, declarations, parameters, members, unions, and
generic arguments. Independently, `ref` of an Atomic value or Atomic member is
invalid even when no expected pointer type is present. A construct containing
both violations need not report both.

This wording distinguishes invalid type placement from invalid value copying.

## 9. Exact printability set

### Problem

The print section rejects “resources,” but the category is undefined.

### Required rule

Define printability as a closed set:

- directly printable: Bool, every integer type, Size, Byte, Float32, Float64,
  Rune, String, Strand, and Error;
- recursively printable: objects, ADTs, Array, View, List, and Dict when every
  recursively visited component is printable; and
- every other canonical type is non-printable.

The final clause rejects Ptr, MutPtr, Fun, unions, Nil, EoS, Heap, File, Stream,
Task, Channel, Mutex, Atomic, RuneCursor, and future types by default without
an undefined umbrella term. Nil has no standalone printable value; unions
containing Nil remain non-printable until narrowed to a printable non-Nil
member.

Failure identifies the first non-printable member path in declaration order.

## 10. Complete built-in API contracts

### 10.1 Signature notation

Reference signature blocks are contracts, not source examples. They use:

- `receiver.operation(parameter: Type) -> Result`;
- `-> no value` for no-result operations;
- `Integer` as a documentation metavariable meaning any Hexal integer type;
- generic names T, U, K, V, State, and R as declared type metavariables; and
- `place<T>` and `read-only-place<T>` only as documentation result categories,
  never source syntax.

Every operation states receiver, parameter names/types, result, traps/errors,
allocation, ownership, and invalidation where applicable.

Every built-in family uses the same presentation:

- fenced `text` signature blocks grouped by built-in family;
- one fully qualified operation per entry, with continuation lines only when a
  signature does not fit legibly on one line;
- explicit parameter names and types;
- no comma-separated signature prose; and
- prose below the block only for semantic conditions not expressible in a
  signature.

This applies equally to Array, View, List, Dict, String, Strand, RuneCursor,
Stream, Task, Channel, Mutex, Atomic, File, Error, Heap, layout intrinsics, and
volatile operations.

### 10.2 Contiguous collections

Array, View, and List use fully qualified signatures even where their surfaces
coincide:

```text
Array<T,N>.length() -> Size
Array<T,N>.is_empty() -> Bool
Array<T,N>.at(index: Integer) -> T
Array<T,N>[index: Integer] -> place<T>
Array<T,N>.slice(start: Integer, end: Integer) -> View<T>

View<T>.length() -> Size
View<T>.is_empty() -> Bool
View<T>.at(index: Integer) -> T
View<T>[index: Integer] -> read-only-place<T>
View<T>.slice(start: Integer, end: Integer) -> View<T>

List<T>.length() -> Size
List<T>.is_empty() -> Bool
List<T>.at(index: Integer) -> T
List<T>[index: Integer] -> place<T>
List<T>.slice(start: Integer, end: Integer) -> View<T>
```

Contracts:

- Array indexing is writable only through a writable Array place.
- View indexing is read-only.
- List indexing is writable through every live List handle.
- `at` returns a value copy and never a place.
- Index and slice bounds normalize to Size; invalid known bounds fail checking
  and invalid dynamic bounds trap.
- Slice ranges are zero-based and end-exclusive.
- Array and View have no capacity operation.

List's complete additional surface is:

```text
List<T>.new(heap: Heap) -> List<T>
List<T>.push(value: T) -> no value
List<T>.pop() -> T
List<T>.set(index: Integer, value: T) -> no value
List<T>.clear() -> no value
List<T>.free(heap: Heap) -> no value
List<T>.stream(heap: Heap) -> Stream<T>
```

### 10.3 Dictionary

```text
Dict<K,V>.new(heap: Heap) -> Dict<K,V>
Dict<K,V>.insert(key: K, value: V) -> no value
Dict<K,V>.get(key: K) -> V
Dict<K,V>.contains(key: K) -> Bool
Dict<K,V>.remove(key: K) -> V
Dict<K,V>.free(heap: Heap) -> no value
```

K is exactly Int32 or Strand. Missing `get`/`remove` trap. Insert replaces.
All values copy shallowly; cleanup releases only Dict storage.

### 10.4 Text

```text
String.length() -> Size
String.is_empty() -> Bool
String.at(index: Integer) -> Rune
String[index: Integer] -> Rune
String.bytes() -> View<Byte>
String.slice(start: Integer, end: Integer) -> View<Byte>
String.rune_cursor() -> RuneCursor
String.to_string(heap: Heap) -> String
String.concat(heap: Heap, other: String) -> String
String.free(heap: Heap) -> no value
String.from_bytes(heap: Heap, bytes: View<Byte>) -> String
String.from_runes(heap: Heap, runes: View<Rune>) -> String

Strand.length() -> Size
Strand.is_empty() -> Bool
Strand.at(index: Integer) -> Rune
Strand[index: Integer] -> Rune
Strand.to_string(heap: Heap) -> String

RuneCursor.has_next() -> Bool
RuneCursor.next() -> Rune
```

`to_string`, `concat`, `from_bytes`, and `from_runes` allocate one independent
runtime String through the supplied Heap. `concat` does not mutate either
operand. `from_bytes` validates UTF-8 before allocation. Strand exposes no
View.

### 10.5 Streams

```text
Stream<T>.new() -> Stream<T>
Stream<T>.produce(
    heap: Heap,
    state: State,
    callback: Fun<(MutPtr<State>) : T | EoS>,
) -> Stream<T>
List<T>.stream(heap: Heap) -> Stream<T>

Stream<T>.next() -> T | EoS
Stream<T>.filter(heap: Heap, predicate: Fun<(T) : Bool>) -> Stream<T>
Stream<T>.map<U>(heap: Heap, mapper: Fun<(T) : U>) -> Stream<U>
Stream<T>.take(heap: Heap, count: Size) -> Stream<T>
Stream<T>.free(heap: Heap) -> no value
```

The reference must state that successful filter/map/take construction owns its
upstream, every chain uses one Heap, and no alias may subsequently pull, adapt,
or free consumed upstream state.

### 10.6 Tasks

```text
spawn function(args) -> Task<R> | Error
Task<R>.join() -> R
Task<R>.detach() -> no value
Task.yield() -> no value
```

Spawn eligibility uses section 6 rather than the phrase “any complete copyable
result.”

### 10.7 Channels

```text
Channel<T>.new(heap: Heap, capacity: Size) -> Channel<T> | Error
Channel<T>.send(value: T) -> Nil | Error
Channel<T>.receive() -> T | EoS
Channel<T>.close() -> no value
Channel<T>.free(heap: Heap) -> no value
Channel<T>.length() -> Size
Channel<T>.capacity() -> Size
Channel<T>.is_closed() -> Bool
```

`length` and `is_closed` are synchronized snapshots. Capacity is immutable.
Neither snapshot predicts whether a later send/receive parks.

### 10.8 Mutex

```text
Mutex.new(heap: Heap) -> Mutex | Error
Mutex.lock() -> no value
Mutex.unlock() -> no value
Mutex.free(heap: Heap) -> no value
```

Successful unlock synchronizes with the later lock that acquires the Mutex.
Waiting parks the Task, not the worker.

Replace “cheaply detectable live misuse” with the accepted exact boundary:

- the runtime traps invalid states detectable from a live Mutex control block,
  including recursive lock and wrong-owner unlock; and
- it need not retain freed control blocks solely to diagnose stale aliases, so
  use after free is not guaranteed to trap.

### 10.9 Atomic

```text
Atomic<T>.new(initial: T) -> Atomic<T>
Atomic<T>.load() -> T
Atomic<T>.store(value: T) -> no value
Atomic<T>.exchange(value: T) -> T
Atomic<T>.fetch_add(value: T) -> T
Atomic<T>.fetch_sub(value: T) -> T
Atomic<T>.compare_exchange(expected: T, desired: T) -> Bool
```

- `fetch_add` and `fetch_sub` are unavailable for Bool.
- Every operation is sequentially consistent.
- Compare-exchange is strong and non-spurious.
- On equality, compare-exchange stores desired and returns true.
- On inequality, it leaves the Atomic unchanged and returns false.
- Expected is an input value and is never rewritten.

### 10.10 Files

```text
File.open(path: String, mode: FileMode) -> File | Error
File.read_bytes(heap: Heap) -> List<Byte> | Error
File.read_text(heap: Heap) -> String | Error
File.write(bytes: View<Byte>) -> Nil | Error
File.write_text(text: String) -> Nil | Error
File.flush() -> Nil | Error
File.close() -> no value
Stdio.stdin() -> File
Stdio.stdout() -> File
Stdio.stderr() -> File
```

Retain the current File result model. `File.close()` remains `-> no value`;
fallible operations use `Nil | Error` where no success payload exists. Preserve
the existing path validation, mode restrictions, borrowed-standard-file rules,
partial write effects, flush durability limit, alias invalidation, and close
trap contracts below the signature block.

## 11. Grammar and semantic synchronization

### 11.1 `try` in statement position

#### Problem

The semantic contract and fallible command APIs require a caller to propagate
an Error while discarding the successful Nil payload. Existing accepted specs
use forms such as `try file.flush()`, but the embedded grammar permits only a
call expression beginning with an identifier or `self` as a call statement.
A `try` expression therefore cannot currently occupy statement position under
the grammar.

#### Required rule

Add a dedicated statement production:

```ebnf
non-control-statement = declaration | assignment | call-statement
                        | try-statement
                        | "break" | "continue"
                        | defer-statement | errdefer-statement ;
try-statement = "try" , unary-expression ;
```

Contracts:

- A try statement has the same Error propagation and operand eligibility as a
  try expression.
- Its normalized success value is discarded.
- It is valid only inside a function whose declared result accepts Error and
  outside every cleanup action.
- It does not make arbitrary value-producing expressions valid as statements.
- This is a grammar conformance correction for already-intended behavior, not
  a new error-handling construct.

The Grammar section owns this as a distinct statement production. It is not a
call statement: its first token is `try`, and its semantic contract includes
Error propagation.

### 11.2 `for` binder arity

#### Problem

The grammar permits one, two, or three binders for every `for` source. The
semantic prose describes an optional index plus source-specific values but does
not state a closed source/arity matrix. A reader cannot derive which
grammatically valid combinations the checker rejects.

#### Required rule

Retain the broad source-shape grammar and add this semantic matrix:

| Source | Binders | Binder types and order |
| --- | ---: | --- |
| Array, View, List, String, Strand, Stream | 1 | value |
| Array, View, List, String, Strand, Stream | 2 | `index: Size`, value |
| Dict | 2 | key, value |
| Dict | 3 | `index: Size`, key, value |

Every other source/arity combination is a static error. Existing
source-specific value types, copy rules, traversal rules, and mutation
invalidation rules remain unchanged.

### 11.3 Root-scope `return`

#### Problem

`return` is invalid at root scope. The compiler enforces this and reports it
well:

```text
[Syntax Error] return is only valid inside a function or method body
```

The reference states the rule **only in the grammar**, through a family of five
productions that exist for no other purpose:

```ebnf
non-return-statement       non-return-block
non-return-if-statement    non-return-while-statement
non-return-for-statement
```

These roughly double the statement section of the EBNF. No prose rule mentions
the restriction, so a reader of the semantic sections never learns it.

This inverts the convention this reference already states — the grammar defines
shape, and semantic rules may reject a grammatically valid form. `errdefer`
follows that convention correctly: the grammar admits it at root and the
Errors section rejects it. Root `return` does the opposite, encoding a semantic
restriction structurally where prose readers cannot see it.

#### Required rule

Add to the Programs, names, and bindings section:

> `return` is valid only inside a function or method body. A root executable
> statement cannot return; the root program has no declared result.

Then delete the five `non-return-*` productions and let `top-level-item` use
`statement` directly:

```ebnf
top-level-item = type-declaration | function-declaration
                 | implementation-declaration | statement ;
```

The grammar becomes shape-only and loses about ten lines; the restriction moves
to the section that owns it. Compiler behavior is unchanged — this records an
already-implemented decision in the correct place.

---

# Part B — Nil and command results

## 12. Problem

Nil represents the empty alternative of a union; it is not a valid standalone
declared type. Current APIs nevertheless use Nil results for infallible
commands that carry no payload:

| No result | Nil result |
| --- | --- |
| List/Dict/String `free` | Task `detach` |
| List `push`, `set`, `clear` | Task `yield` |
| File `close` | Channel `close`, `free` |
| Heap `free` | Mutex `lock`, `unlock`, `free` |
| Stream `free` | Atomic `store` |

This conflicts with the type model and with equivalent no-result commands.

## 13. Proposed convention

Operations use these result classes:

| Operation class | Result |
| --- | --- |
| infallible command with no payload | no value |
| fallible command with no payload | `Nil | Error` |
| infallible query | concrete payload |
| fallible query/constructor | `T | Error` |

Nil contracts:

- Nil is valid only as a member of a union containing at least one non-Nil
  member.
- A written standalone `Nil` type is invalid in bindings, parameters, function
  results, members, payloads, collection positions, aliases, and generic
  arguments.
- The literal `nil` requires a contextual union type containing Nil; it cannot
  initialize or produce a standalone Nil value.
- A function returning no value omits its result annotation and uses bare
  `return` when an explicit return is needed.
- Fallible no-payload operations use `Nil | Error`: Nil is their successful
  union member, not a standalone result type.

Under this convention:

- Task `detach` and `yield` return no value.
- Channel `close` and `free` return no value.
- Mutex `lock`, `unlock`, and `free` return no value.
- Atomic `store` returns no value.
- Channel `send` remains `Nil | Error`.
- File `write`, `write_text`, and `flush` remain `Nil | Error`.
- File `close`, collection cleanup, Heap cleanup, and Stream cleanup remain no
  value.

No-result calls remain valid as statements and direct defer/errdefer actions.
They are invalid as initializers, operands, arguments, or return values.

## 14. Compatibility

Part B rejects every written standalone Nil type and every attempt to consume
the former Nil result of an infallible command. An infallible command is used as
a statement. If a surrounding expression requires a value, its design must be
changed rather than manufacturing a standalone Nil value.

Part B changes no runtime operation, failure mode, scheduling behavior,
synchronization edge, or C representation.

---

# Part C — reference structure

## 15. Syntax has one authoritative location

The Grammar section remains at the top of `reference.md`. This is an explicit
project invariant so syntax is encountered before dependent semantic rules and
is less likely to drift from them. Moving it to an appendix is outside this
cleanup.

The embedded Grammar section owns:

- identifier and literal token shapes;
- reserved words;
- statement and declaration forms;
- type forms;
- expression precedence and associativity implied by productions; and
- same-line parser constraints represented by `same-line`.

Delete repeated prose that merely restates those productions. Retain prose only
for behavior EBNF cannot express, including:

- maximal-munch interpretation of `>>` in nested type arguments;
- context-dependent `|` interpretation;
- semantic restrictions on grammatically valid forms;
- literal typing and range validation;
- match arm delimiter behavior encoded by prose productions; and
- evaluation order.

The Operators section defines domains, result types, overflow, traps, and
evaluation. It does not repeat the precedence ladder already encoded by the
grammar.

### 15.1 Consolidate related semantic rules

Some rules are split across feature sections. Keep one complete rule at the
semantic owner and use a short cross-reference at the dependent location:

- Structural unions own `is` narrowing creation and invalidation. Control flow
  owns propagation: a fact established by a branch condition survives after
  the branch only on the sole continuing path when every alternative path
  terminates with `return`, `break`, or `continue` as valid for that context.
- Replace “may survive” with the preceding deterministic condition.
- Replace “a loop always may fall through” with “a loop is always treated as
  able to fall through, including `while true`.”
- Errors owns the complete scope restrictions for `try` and `errdefer`.
  Control flow states only their cleanup execution behavior and links to the
  Errors rule; it does not restate a partial validity rule.

## 16. Replace illustrative output with templates

The print section currently uses concrete names and values. Replace them with
abstract output templates:

| Value category | Exact template |
| --- | --- |
| object | `<Type> { <member> = <value>, ... }` |
| unit ADT variant | `<ADT>.<Variant>` |
| record ADT variant | `<ADT>.<Variant> { <field> = <value>, ... }` |
| Array/View/List | `[<value>, ...]` |
| Dict | `{<key>: <value>, ...}` |

The surrounding rules define declaration order, active payload only, empty
forms, separators, quoting, escaping, and unspecified Dict order. Templates
are contracts, not examples.

## 17. Remove compiler internals from the reference

Delete these statements from the reference because `AGENTS.md` already owns
compiler architecture:

- the compiler pipeline is forward-only;
- checked operations retain internal structured identities;
- the checker does not pass source-derived opaque C fragments to generation;
  and
- internal phase ownership beyond externally observable diagnostic class.

Retain these observable contracts in the reference:

- invalid or unsupported source fails with a structured diagnostic;
- unsupported forms are never silently omitted or partially generated;
- syntax failures, static semantic failures, dynamic traps, and Unknown Error
  remain distinct externally visible classes;
- generated identifier mappings;
- generated-C layout and qualification;
- `#line` source mapping; and
- defined behavior at C undefined-behavior edges.

## 18. Consolidate generated-C contracts

Move generated identifier prefix mapping from “Programs, names, and bindings”
to “C23 output contract.” Keep source namespace protection in the Programs
section.

The output section then owns:

- `hex_v_`, `hex_t_`, `hex_m_`, and `hex_f_` mapping;
- `HEX_` macro namespace;
- pointer qualification;
- aggregate and union lowering;
- monomorphization;
- source mapping; and
- target representation assertions.

Split the current combined “Layout, volatile access, and C23 contract” section
into three neighboring sections: layout intrinsics, volatile operations, and
C23 output. Their small size does not justify mixing unrelated language
surfaces with lowering contracts.

Replace the vague Nil/EoS C entries (`where needed`, `compiler-defined`) with
an explicit non-ABI contract:

- Nil and EoS are zero-state singleton language values. Nil exists only as a
  union member; EoS remains a valid standalone type.
- A Nil union member and an EoS value carry no payload. Their storage may be
  elided; no stable foreign ABI representation is promised.
- Nil uses a null pointer only in the accepted pointer-plus-Nil niche.
- In general unions, Nil and EoS are represented by distinct active-member
  tags under the ordinary union-lowering rule.

This is relocation only; no generated spelling changes.

## 19. Terminology

Use one term for each concept:

| Concept | Required term |
| --- | --- |
| C representation copied without recursion through referents | shallow copy |
| stored address-bearing runtime value | handle |
| Ptr/MutPtr/Fun union-layout class | pointer type |
| operation returning no Hexal value | no result / `-> no value` |
| fallible operation succeeding without payload | Nil success member / `Nil | Error` |
| invalid dynamic state terminating execution | trap |
| allocation or state referenced by a handle | referent |
| operation making an address/descriptor unusable | invalidation |

Avoid “resource” unless a closed type set is defined at that location. Avoid
“any T” when a position-specific eligibility rule exists.

## 20. Reference update sequence

1. Update the Grammar section, retaining it at the top and as the sole
   syntax-shape authority.
2. Apply the settled Part A rules, including the position registry, View and
   Atomic wording, printability set, API contracts, and `for` binder matrix.
3. Apply the Nil restriction and no-result command signatures consistently
   across core types, unions, functions, APIs, and dependent rules.
4. Apply Part C relocation, cross-references, terminology, and syntax
   deduplication.
5. Compare the result against the pre-cleanup backup and this RFC. Retain every
   normative contract not explicitly replaced here.
6. Verify `docs/reference.md` only: EBNF and semantic syntax agree; every type
   class is closed or defined; every built-in signature is exact; every
   cross-reference resolves; no illustrative program, tutorial, history,
   implementation status, compiler-internal rule, or duplicated contract
   remains.

## 21. Acceptance criteria

RFC 0050 is complete when:

1. the widening table cannot be read as rejecting target-dependent Size
   widening;
2. Rune's complete operator domain is stated once;
3. Task and Stream eligibility compose with the shared position model;
4. pointer type and nullable niche eligibility are closed and explicit;
5. Atomic type placement and copy rejection are separately explicit;
6. printability is a closed recursive predicate with no undefined category;
7. every built-in operation has an exact receiver, parameter, result, and
   lifecycle contract;
8. Nil is restricted to union membership, fallible no-payload success uses
   `Nil | Error`, and infallible no-payload commands return no value;
9. the Grammar section is the sole syntax-shape authority;
10. print formats are abstract exact templates rather than examples;
11. compiler internals are absent and observable contracts remain in
    reference.md;
12. generated-C contracts are consolidated without spelling changes;
13. no accepted behavior is lost during backup comparison;
14. Pointee and HeapAllocation eligibility are derivable from the shared
    position model;
15. narrowing propagation, loop fallthrough, and cleanup-context validity use
    deterministic language;
16. View placement is not conflated with provenance or lifetime;
17. Atomic direct allocation rejection and the approved direct-pointee rule are
    explicit while pointers to enclosing Atomic-containing objects remain
    valid;
18. every built-in family uses one uniform signature format;
19. Mutex misuse detection and Nil/EoS lowering contain no implementation
    hedges;
20. the Grammar and Errors sections define try statements consistently,
    including Error propagation and discarded success values;
21. `try` remains invalid at root and in cleanup actions;
22. every `for` source accepts exactly the documented binder arities and rejects
    all other arities;
23. every rule is stated once at its semantic owner with only necessary
    cross-references elsewhere;
24. the document contains no tutorials, illustrative programs, rationale,
    history, implementation status, compiler internals, or test strategy; and
25. comparison with the backup confirms that no unreplaced normative contract
    was lost.

## 22. Non-goals

- Changing pointer lifetime or ownership semantics.
- Reintroducing affine ownership, borrow checking, or implicit cleanup.
- Expanding Fun placement.
- Adding new built-in operations.
- Changing collection, String, Stream, File, Task, Channel, Mutex, or Atomic
  runtime behavior beyond the command-result rules in Part B.
- Changing grammar or accepted syntax beyond correcting the missing
  `try-statement` production.
- Allowing arbitrary value-producing expressions as statements.
- Moving the Grammar section away from the top of the reference.
- Replacing the reference with generated documentation.

## 23. Approved decisions

All work is confined to `docs/reference.md`. The following decisions are final
for this RFC.

### 23.1 Command results

- Nil is valid only as a union member, never as a standalone declared type.
- Infallible commands with no payload return no value.
- Fallible commands with no success payload return `Nil | Error`.

### 23.2 Direct Atomic pointees

- `Ptr<Atomic<T>>` and `MutPtr<Atomic<T>>` are invalid type expressions.
- `ref` of an Atomic value or member is independently invalid.
- Direct Heap allocation of Atomic is invalid.
- A pointer to an enclosing object containing Atomic remains valid.

Compiler conformance to these contracts is outside this RFC.
