# Hexal Status

> This file tracks implementation status, not language semantics. The normative
> language contract is [`reference.md`](reference.md).

## Implemented

- RFC 0003 core scalar types and canonical C23 mappings.
- Contextual integer and floating literals with exact range checking.
- Decimal, hexadecimal, binary, and explicit-octal integer syntax.
- General unary `-` and `!`, including exact signed-minimum literals and
  negative-zero preservation.
- Experimental `Byte` type and `b'...'` literals removed with migration
  diagnostics recommending `UInt8`.
- Exact integer and hexadecimal floating C23 literal generation.
- Generated target checks for 8-bit bytes, exact-width integer types, and
  binary32/binary64 value-set properties.
- Default-constant bindings and declaration-side `mut`.
- Recursive `Ptr<T>` construction and place-only prefix `ref`.
- Read/write pointer capabilities with nested capability shapes and `.value`
  access.
- Structured fail-closed lexer, parser, checker, and generator diagnostics.
- RFC 0004 source identifier rules and deterministic private C names for values.
- Structured checked expressions for generated variable, address-of, and
  dereference references.
- RFC 0005 transparent type aliases, ordered top-level type declarations, and
  compilation-scoped canonical pointer interning.
- RFC 0006 nominal core object values, exhaustive named literals, read-only and
  mutable member paths, and C23 struct/compound-literal lowering.
- RFC 0007 pointer-constructor redesign: `Ptr<T>` (read-only pointee) and
  `MutPtr<T>` (writable pointee), `ref` typed by place writability,
  outermost-layer weakening from `MutPtr<T>` to `Ptr<T>`, pointee `const`
  declarators derived from the type chain, and removal of per-value access
  capabilities, expression-side `mut`, and `mut ref`.
- Pointer-valued object members and self-recursive objects, with split
  forward-typedef and definition regions.
- Object member initializer type checking routed through the same
  assignability rule as declarations and assignments.
- RFC 0008 functions and methods: declarations, closed function scopes,
  callbacks, receiver adaptation, method calls, and source-ordered C23
  lowering.
- RFC 0009 core operators: full precedence and grouping, unary negation and
  logical-not, arithmetic, comparisons, and short-circuit `and`/`or`.
- Identical operand typing, contextual literal propagation, exact immutable
  folding, defined mutable integer wrapping, static divisor diagnostics, IEC
  60559 floating behavior, and promotion-safe C23 arithmetic lowering.
- RFC 0010 nil and explicit nullability: canonical `Nil` singleton and
  `nil` literal, non-null raw pointers, explicit `P | Nil` nullable pointer
  unions reusing the base pointer's null niche, commuting `== nil`/`!= nil`
  tests with flow narrowing, incomplete `Unknown` pointee with one-layer
  erasure and recovery, and C23 lowering to `nullptr`/`nullptr_t` with
  `<stddef.h>` included only when null is used.
- RFC 0014 general type expressions and structural unions: recursive grouped
  type grammar, canonical flattening and interning, contextual member
  selection, injection and widening, exact `is` tests, tagged C23 values,
  union equality, branch-local narrowing, and deterministic fail-closed
  lowering. Pointer-plus-`Nil` unions retain the RFC 0010 null niche.
- RFC 0019 generic types, functions, and methods: generic type and alias
  declarations, generic functions and methods with inherited receiver
  parameters, deterministic call/constructor/function-value inference,
  open-generic checking with specialization-time operation rechecking,
  canonical specialization identity, unchanged-argument recursion reuse,
  and C23 monomorphization with deterministic specialization names.
- RFC 0026 allocation, deallocation, and deferred cleanup: the built-in
  `Heap` handle, checked `h.allocate<T>(initial)` returning `MutPtr<T>`,
  provenance-validated `h.free(value)`, lexical `defer` with
  registration-time capture for direct calls and exit-time evaluation for
  other expressions, reverse-order cleanup on every scope-exit edge, and
  checked C23 helpers with allocation-state validation.
- RFC 0022 algebraic data types and match expressions: nominal closed ADTs
  with unit and named-record variants, qualified variant constructors,
  generic ADT templates with specialization and expected-type inference,
  indirect recursion, exhaustive `match` expressions in Boolean value mode
  and exact-type/variant type mode, scrutinee narrowing with arm-local
  payload views, and deterministic tagged C23 lowering with inactive-payload
  protection.
- RFC 0020 core collections:
  - `Array<T, N>` fixed inline arrays: literal syntax with exact-length
    checking, index and `at` access with compile-time constant bounds
    diagnostics and runtime guards, `length`/`is_empty`, member and
    parameter passing, and wrapper-struct C23 lowering with checked element
    accessors.
  - `View<T>` non-owning read-only views: `slice` over Array, View, and
    List sources, runtime `length`/`is_empty`, read-only element places
    preserving `MutPtr` pointee capability, lexical source-tied lifetimes
    with root-chain tracking, and rejection of temporary roots, module
    data, storage, unions, pointers, and `ref`.
  - String revision: allocation-free static literals with escapes, the
    `hex_string` pointer handle and combined `hex_string_storage` runtime
    allocation, affine ownership (static/owning/borrow provenance),
    `bytes`/rune-bounded `slice` returning `View<UInt8>`,
    `to_string`/`concat`/`String.from_bytes` owning copies, `free`, and
    exactly-once cleanup with deferred-free coverage.
  - `List<T>` growable owning sequences: `new`, `push`, `pop`, `set`,
    `clear`, `free`, indexing through any live reference, `slice` views,
    borrowed parameters with mutation, terminal return handoff,
    freed-to-fresh-owner reuse, and `List<String>` with nested-String
    copy-in, borrowed reads, move-out `pop`, and destruction on `set`,
    `clear`, and `free`.
  - `Dict<K, V>` owning dictionaries with exactly `Int32` and `Strand`
    keys: `new`, `insert`, `get`, `contains`, `remove`, `free`, inline
    `hex_strand` literal values, open-addressing C23 lowering with
    infallible hashing, and `Dict<K, String>` borrow and move-out rules.
  - A shared affine ownership flow: `uninitialized`/`live`/`freed` states,
    exact control-flow merge agreement, loop back-edge invariants, scope
    exit leak checks, deferred-cleanup obligation tracking, and
    view/borrow invalidation sets. Superseded by RFC 0035 (C-style copying
    and manual lifetimes), which removes this flow and the provenance
    classes; the collection APIs themselves are unchanged.
- RFC 0024 equality, ordering, and hashability: lossless numeric widening
  with the unique least common type for `==`, `!=`, and the ordering
  operators, strict canonical typing for non-numeric operands, pointer
  identity equality, recursive member-wise object and ADT equality, tag-safe
  union equality, element-wise Array/View/List sequence equality, exact
  UTF-8 byte equality and lexicographic ordering for `String` and `Strand`,
  dictionary and function equality rejection, generic-dependent comparisons
  rechecked at specialization, and per-type C23 equality helpers with no
  storage `memcmp` or inactive-payload reads. Dictionary hashing remains
  internal to the exact `Int32` and `Strand` keys.
- RFC 0036 target-sized `Size` values: the canonical `size_t`-mapped unsigned
  `Size` type, lossless widening between fixed-width unsigned integers and
  `Size`, collection and text lengths returning `Size`, `size_t` field and
  index-cast lowering, and a generated 64-bit `size_t` target assertion when
  the collection or conversion machinery requires it.
- RFC 0016 explicit numeric conversions: destination-named
  `to_int8`..`to_uint64`/`to_float32`/`to_float64`/`to_size` methods with
  checked, `_wrapping`, and `_saturating` forms, exact constant folding with
  truncation toward zero for float sources, and guarded C23 conversion
  helpers that trap before any invalid C conversion.
- RFC 0017 defined integer arithmetic: mixed numeric arithmetic computed in
  the unique lossless common type, constant folding with modular wrapping at
  the result width (including `IntN_MIN / -1` folding to `IntN_MIN`), and
  runtime-guarded `hex_div_*`/`hex_rem_*` helpers for every integer division
  and remainder.
- RFC 0035 C-style copying and manual lifetimes: assignment, argument
  passing, return, object construction, ADT construction, and collection
  insertion copy values by C representation; String, List, Dict, and Stream
  copy pointer-sized handles; the affine ownership machinery (owner states,
  provenance classes, borrows, and tracked view lifetimes) is removed;
  objects and ADTs may freely contain reference-like handles; cleanup is
  the programmer's explicit responsibility with no compiler-enforced
  exactly-once checking.
- RFC 0028 for-in iteration and loop body delimiters: mandatory `do` after
  every `while` condition and `for` source, `for`/`in` over Array, View,
  List, String, Strand, and Dict with optional leading `Size` indices,
  rune-ordered text iteration, produced-entry dict ordinals, source
  stabilization (Array places iterate in place, temporaries and Strands
  materialize once), fresh immutable per-iteration binders, and the `Rune`
  named type for decoded scalars.
- RFC 0031 pull streams: lazy single-pass `Stream<T>` pointer handles with
  the `EoS`/`eos` singleton completion marker, canonical allocation-free
  empty `Stream<T>.new()`, `Stream<T>.produce(heap, state, callback)` with
  named `Fun<(MutPtr<State>) : T | EoS>` producers, non-owning
  `List<T>.stream(heap)` sources, `next` returning `T | EoS`, lazy
  `filter`/`map`/`take` adapters with recursive free, `for` iteration, and
  explicit `free`; one combined header-and-state allocation per node with
  immutable ops tables.
- RFC 0029 error values, `try`, and `errdefer`: built-in five-field `Error`
  value type with compiler-injected construction-site fields, `Error.new`
  construction, `T | Error` and `Error | Nil` fallible results over the RFC
  0014 union representation, the prefix `try` expression with
  single-evaluation and unchanged-Error propagation, normalized success
  unions, and `errdefer` cleanup that runs only on Error exits with the
  same capture and lexical-lifetime rules as `defer`.
- RFC 0030 `print` builtin: protected non-keyword name with ordinary call
  syntax, exactly-once left-to-right argument evaluation, static recursive
  printability of scalars, Error, objects, ADTs, and core collections,
  direct-versus-nested text quoting, compact and structural Error forms,
  `%g` precision-9/17 float formatting with fixed non-finite spellings,
  Task-atomic complete-call output through one process-wide lock and
  shutdown gate, and unrecoverable Runtime Error on detected output
  failure.
- RFC 0032 low-level integer and bit operations: `~`, `&`, `^`, `|`, `<<`,
  and `>>` over fixed-width integers and `Size` with RFC 0016 common types,
  wrapping left shift, arithmetic signed and zero-filling unsigned right
  shift, constant and runtime-validated shift counts, promotion-safe C23
  lowering, protected same-width scalar `bit_cast<T>()`, and endian
  `to_le_bytes`/`to_be_bytes` plus `T.from_le_bytes`/`from_be_bytes` over
  exact `Array<Byte, N>` values.
- RFC 0033 no source-level pointer arithmetic: pointers are references to
  one typed object; offset, distance, indexing, ordering, pointer/integer
  conversion, and pointer `bit_cast` are rejected as Type Errors, `++`/`--`
  and compound assignments stay absent from the grammar, bounds-carrying
  collections own sequence access, and only trusted generated or runtime C
  may step pointers.
- RFC 0038 generic conversion syntax: `source.to<Destination>()` is the one
  explicit checked scalar conversion, reusing RFC 0019's generic-call
  grammar, folding invalid constants and trapping invalid runtime values
  under RFC 0016's matrix, and superseding the former destination-encoded
   conversion method names listed under RFC 0016.
- RFC 0037 M:N tasks, concurrency, and parallelism: stackful `Task<T>`
  fibers scheduled M:N over C23 `<threads.h>` workers (Windows x64 Fibers,
  POSIX x86-64 System V context switch), the reserved `spawn` prefix
  expression over direct named-function calls with exactly-once left-to-
  right shallow-copied arguments, `Task.yield()`, join/detach lifecycle and
  reclamation, bounded multi-producer/multi-consumer `Channel<T>` with
  send-on-close Error and `eos` on closed-and-drained receive,
  scheduler-aware non-recursive Mutex with Task-identity ownership across
  worker migration, sequentially consistent inline `Atomic<T>` over C23
  `_Atomic` for Bool, the fixed-width integers, and `Size`, the fatal
  `while true` repeating-path `Task.yield()` starvation rule, scheduler-
  owned allocation, and a shutdown gate that never implicitly joins live
  tasks.
- RFC 0040 synchronous File I/O: protected `File` and `FileMode` names, the
  `Read`/`Write`/`Append` binary-mode ADT, `File.open` with compile-checked
  non-empty ASCII paths, `read_bytes`/`read_text`/`write`/`write_text`/
  `flush`/`close` with source-located recoverable Error and close traps,
  borrowed text-only `Stdio.stdin`/`stdout`/`stderr` standard Files with
  compile-time rejection of direct invalid operations, standard writes
  sharing RFC 0030's output lock and shutdown gate, and full
  `Size`/`size_t` lowering.
- RFC 0041 no module globals: imported modules hold only type, function,
  and method declarations; root executable statements lower as entry-body
  locals; functions cannot capture root locals; there is no `global`,
  `static`, or native module-constant syntax; and no accepted declaration
  emits user value storage at C file scope.
- RFC 0042 layout and volatile operations: `size_of<T>()` and
  `align_of<T>()` returning `Size` C constant expressions over complete
  finite-sized types with specialization-time generic rechecking, and
  per-access `read_volatile`/`write_volatile` on Ptr/MutPtr over the
  fixed-width integers, Byte, and `Size` with exactly-once operand
  evaluation and constness-preserving C23 volatile lowering.
- RFC 0043 pointer-plus-length View bridge: `View<T>.from_pointer(ptr,
  length)` over Ptr or MutPtr with MutPtr weakening and statically non-null
  pointers only, plus `View<T>.empty()`; one descriptor initialization with
  no extent multiplication, allocation, or pointer arithmetic, and RFC 0020
  bounds checks retained.
- RFC 0044 String, Strand, Byte, and Rune conformance: `Byte` restored as a
  transparent canonical alias of UInt8, `b'...'` byte and `'...'` rune
  literals with the RFC 0018 escape sets and Unicode-scalar validation,
  split String and Strand method dispatch, Strand lowered to one inline
  zero-filled 32-byte value, `String.from_bytes` requiring `View<Byte>`
  with validate-before-allocate traps, `String.from_runes`, `RuneCursor`,
  and all text lengths lowered through `Size`/`size_t` without UInt64
  remnants.
- RFC 0045 project rename to Hexal: every live reference to the former name
  "Seawitch" (prose, source comments, generated-C diagnostics and
  static-assert texts, and workbench UI) now reads "Hexal", live-doc code
  fences use the ```hexal tag, and the private C namespace prefix is `hex_`
  (uppercase macros `HEX_`) for every emitted helper, type CName, and guard
  macro. Closed specs retain their original spellings and are untouched.

## Known follow-ups

### Reference-audit conformance gaps

Tracked by draft RFC 0046, Post-Migration Conformance Cleanup.

- Atomic non-copyability is enforced for Task/Channel boundaries but not yet
  for ordinary copy, assignment, `ref`, Array storage, or Stream elements and
  producer state.
- `Stream` is not yet rejected as a redeclared or shadowed protected type name.
- On the required 64-bit target, `UInt64` does not yet widen implicitly to the
  equal-range `Size`; mixed operations already prefer `Size` correctly.

- RFC 0015 structured control flow is implemented and conforming:
  `if`/`elseif`/`else`, `while`, `break`, `continue`, lexical block scopes,
  definite-return analysis, delimiter-aware parser recovery, generator
  loop-context fail-closed validation, and direct C23 lowering with
  source-line mappings. Its exact-Boolean condition rule is superseded by RFC
  0023 (truthiness), which is now the current rule for conditions. See [ADR
  0025, the structured-control-flow conformance update](specs/archive/0025-structured-control-flow-conformance-update.md).
- RFC 0023 truthiness and boolean contexts is implemented: `false` and `nil`
  are falsey, every other value is truthy, conditions accept any
  value-producing expression, `and`/`or`/`not` accept any value-producing
  operands (mixed types included) and fold through truthiness, and the C23
  lowering keeps Bool as-is, renders `nil` as `false`, a `P | Nil` value as
  `(value != NULL)`, and every other value as an evaluated comma `(value,
  true)`. The RFC 0009 rule that logical operators require `Bool` operands is
  superseded.
- External C toolchain testing is intentionally out of scope. Generated C23
  lowering is covered by Go-level structural and end-to-end assertions and
  by optional gcc smoke tests behind the `c23` build tag.
- Target probes/trusted target metadata for representation evidence beyond C
  constant-expression checks.
- Explicit trapping arithmetic beyond the defined division and remainder
  guards, and trapping conversion controls.
- RFC 0044 completed RFC 0018's remaining String surface: byte and rune
  literals, `String.from_bytes` validation, `String.from_runes`, and
  `RuneCursor`; rune iteration and the `Rune` named type are implemented by
  RFC 0028 with the full scalar range. The exact `Rune` scalar bound
  `U+10FFFF` remains a focused follow-up.
- `String`/collection-borrow invalidation refinements: non-lexical
  lifetimes and cross-call borrow retention (RuneCursor is implemented by
  RFC 0044).
- `Stream<T>` follow-ups: `Channel<T>`, fallible steps, file/socket/range
  sources, `collect`/`fold`/`find`/`count`, `produce_with_cleanup`, and
  arena-backed nodes.
- Numeric ranges and counted loops, mutable element-reference iteration, and
  user-defined iterator protocols.
- Pointer casts, arithmetic, qualifiers, and function pointers.
- Terminating self-recursive object construction.
- Slices, arrays, aggregates, and C header importing (array and view
  collection forms are implemented by RFC 0020).
- Read-only foreign pointer returns and imported `const T *` contracts.
- Access capabilities in functions, fields, methods, collections, and generics.
- Lifetime or `unsafe` boundaries for raw pointer operations.
- RFC 0037's task runtime requires verified C23 `<threads.h>`; a toolchain
  that omits it (`__STDC_NO_THREADS__`) or an unverified thread runtime
  receives an Unsupported Error for task features, with no pthread or
  Win32 worker-thread fallback.
- RFC 0030's process-wide standard-output lock and shutdown gate predate
  the RFC 0037 scheduler; `print` and RFC 0040 standard text writes flush
  only at that shutdown gate, never per call, and `File.flush()` promises
  no physical-media durability.
