# Hexal Language Reference

This file is the sole normative semantic reference. `grammar.ebnf` defines syntax. `status.md`
tracks implementation. Compiler behavior that disagrees with this file is a conformance bug.

## Language boundary

- Hexal is a statically typed systems language that lowers to readable C23 with `#line` mappings.
- Compilation is forward-only and fail-closed: invalid or unsupported source produces a structured
  diagnostic and is never omitted or partially generated.
- Values follow C-style shallow copying. Allocation and cleanup are explicit; there are no moves,
  borrow states, retain counts, implicit destructors, or compiler-enforced exactly-once cleanup.
- Native modules, C interop, Arena, and Pool remain draft features and are not part of this language.
- When no source name is supplied, the compilation unit uses the synthetic filename `main.hex` in
  diagnostics, generated `#line` directives, and `Error.file`. `.hex` is the intended source
  extension; file loading is unspecified.

## Lexical rules

- Identifiers are case-sensitive `[A-Za-z][A-Za-z0-9_]*`; leading `_` and digits are invalid.
  C keywords, macro names, and `main` are valid Hexal identifiers.
- Whitespace is insignificant and statements have no terminator. A call's `(` must share the
  callee's final source line. A return value must begin on the `return` line; otherwise it is bare.
- `--` starts a line comment, `--[ ... ]--` a multiline comment, and `---` is a line comment.
- Integers are decimal, `0x`, `0b`, or `0o`, with lowercase prefixes and `_` only between digits.
  There are no suffixes or implicit octal; nonzero decimal values cannot have leading zeroes.
- Floats are decimal `digits.digits` with optional exponent, or `digits` with required exponent.
  `.5`, `1.`, and hexadecimal floats are invalid.
- String escapes are `\'`, `\"`, `\\`, `\n`, `\t`, `\r`, `\0`, and `\u{HEX}`. Raw newlines are
  invalid. Byte literals use `b'...'`; Rune literals use `'...'`.
- Maximal munch makes `!=`, `==`, `<=`, `>=`, `<<`, and `>>` single tokens. In nested type
  arguments only, `>>` may close two levels. `&&`, `||`, `++`, `--`, and compound assignments are
  invalid.
- Reserved words/literals: `true false nil eos mut ref type and or is fun impl end return if
  elseif else while break continue defer try errdefer spawn as match then self for in do`.

## Programs, names, and bindings

- A source file contains ordered type, function, method, and executable declarations/statements.
  Executable statements occur only in the root program and lower to automatic locals in `main`.
- Hexal has no native globals, global constants, `global`, or `static`. State is local, allocated,
  or passed explicitly.
- Functions and methods are file-scope declarations. Nested functions and closures do not exist;
  functions cannot capture root or lexical locals.
- Declarations become visible in source order. A function may call itself or an earlier function;
  forward calls and mutual recursion are unavailable.
- Type and value names share one namespace. Protected names cannot be redeclared or shadowed.
  Protected types are every scalar plus `Size`, `Byte`, `Rune`, `String`, `Strand`, `Nil`, `EoS`,
  `Unknown`, `Heap`, `Error`, `File`, `FileMode`, `RuneCursor`, `Mutex`, and constructors `Ptr`,
  `MutPtr`, `Fun`, `Array`, `View`, `List`, `Dict`, `Stream`, `Task`, `Channel`, `Atomic`.
  Protected operations are `print`, `size_of`, `align_of`, and qualifier `Stdio`.
- Every binding and written parameter has an explicit type. Compiler-typed `self` and
  `for` binders are the exceptions; `:=` does not exist.
- Bindings and object members are fixed by default. `mut` permits replacement and appears only on
  their declarations. Parameters, `self`, and `for` binders are fixed and cannot be shadowed in
  their own scopes.
- Member assignment requires a writable root and `mut` at every object-member step. Dereference
  writability comes from the pointer type.

Generated private C identifiers use one unconditional prefix on the full source spelling:

| Declaration | Prefix |
| --- | --- |
| binding | `hex_v_` |
| type | `hex_t_` |
| member | `hex_m_` |
| function/method | `hex_f_` |

`HEX_` is reserved for generated macros. Names are never conditionally escaped, hashed, or
truncated; an existing prefix is prefixed again. Foreign C names are outside this rule.

## Values, copying, and evaluation

- Every value is stored inline. Every copy copies the C representation. Scalars and
  inline aggregates (`Strand`, Array, objects, ADTs, File) copy all inline bytes. Pointers and
  `String`, List, Dict, Stream, Task, Channel, Mutex copy their handle representation. View copies
  its pointer-length descriptor. Heap copies a compile-time allocator identity.
- Assignment, arguments, returns, object/ADT construction, collection insertion, union injection,
  and Task capture are shallow copies. Copying does not invalidate the source.
- Values referring to external state include String, List, Dict, Stream, Task, Channel, Mutex,
  File, RuneCursor, View, and aggregates containing them. Copies alias the same state. Freeing one
  alias leaves others dangling; losing the last handle can leak.
- Every value is copyable except `Atomic<T>` and inline aggregates transitively containing one.
  Atomic containment traversal stops at every pointer and handle indirection.
- Storability and copyability are separate. Any complete, finitely sized value may occupy these
  positions, subject to the exceptions below:

```text
binding          object member    ADT payload      union member
Array element    View element     List element     Dict value
function param   function result  Task argument    Task result
Channel element  Stream element   Stream state
```

- Assignment, argument passing, return, insertion, union injection, and spawn capture require a
  copyable value. Direct in-place initialization exists only for bindings and object members;
  non-copyable Atomic state is restricted to those positions.
- `Atomic<T>` is restricted as described under Atomic; `Fun<...>` has its own placement rules;
  `Unknown` exists only as an incomplete pointee. View has no placement exception.
- Full statements execute in source order. Unless stated otherwise, operand order, call-argument
  order, receiver-versus-argument order, and object-initializer order are C23-unspecified.

## Core types

| Hexal | Meaning | C23 |
| --- | --- | --- |
| `Bool` | `false` or `true` | `bool` |
| `UInt8`, `UInt16`, `UInt32`, `UInt64` | exact-width unsigned | `uint*_t` |
| `Int8`, `Int16`, `Int32`, `Int64` | exact-width signed | `int*_t` |
| `Float32`, `Float64` | IEC 60559 binary32/64 | `float`, `double` |
| `Size` | target-sized unsigned length/index | `size_t` |
| `Byte` | transparent alias of `UInt8` | `uint8_t` |
| `Rune` | Unicode scalar value | `uint32_t` |
| `Nil` | singleton `nil` | `nullptr_t` where needed |
| `EoS` | completion singleton `eos` | compiler-defined |

- `Size` exactly matches the selected target's `size_t` width, range, alignment, and representation.
  Supported widths are 16, 32, and 64; others are rejected before checking Size-using source.
  Generated C asserts the chosen width. Size remains canonically distinct from fixed integers.
- Rune is distinct from UInt32 and excludes surrogates. `Int`, `UInt`, `Float`, `Double`, `Char`,
  `Long`, `ISize`, and `Void` are not built-ins.
- A function with no result omits `: Type`; it does not return Nil. Use `: Nil` and `return nil` for
  a first-class Nil result.

### Contextual literals

- Integer literals remain exact until context selects an integer type; without context they default
  to Int32 and must fit. Floats use an expected Float32/64 or default to Float64.
- A direct negative literal is negated before range checking, allowing signed minima. `-0.0` is
  negative zero. Any negative literal in unsigned context, including `-0`, is invalid.
- Expected types reach untyped literals transitively through arithmetic and never retype a typed
  value. Comparisons and logical contexts provide no arithmetic expected type; untyped operands use
  the Int32/Float64 defaults.

### Aliases and objects

- `type Alias = T` is transparent: identical canonical type, representation, and operations, with
  no C typedef. Targets resolve in source order; recursive aliases are invalid.
- Objects are nominal, ordered inline values with at least one member. Identical layouts remain
  distinct. Object literals name every member exactly once in any order; trailing comma is allowed.
  Initializer evaluation order is unspecified.
- Direct and mutual by-value recursive layouts are invalid; pointer-indirect recursion is valid.
- Pointer member access auto-dereferences. `.value` explicitly accesses the whole pointee and is
  required for non-object pointees.

### Pointers and nullability

- `Ptr<T>` is non-null, non-owning, and read-only through the pointer. `MutPtr<T>` is non-null,
  non-owning, and writable through the pointer.
- `ref place` is the only address-taking form: writable places yield MutPtr, fixed places Ptr.
- MutPtr weakens implicitly to Ptr at the outermost layer only. No upgrade or nested weakening.
- `.value` dereferences. Nullability is explicit `P | Nil`; nullable data pointers must be narrowed
  with `== nil`, `!= nil`, or match before dereference. The null niche adds no tag or allocation.
- `Unknown` is incomplete and valid only behind Ptr/MutPtr. One pointer layer may erase to or recover
  from Unknown; Unknown cannot be stored or dereferenced by value.
- String, List, Dict, Stream, and View are already handles/descriptors and cannot be Ptr/MutPtr
  pointees.
- Pointers name one object. Arithmetic, indexing, ordering, subtraction, integer conversion,
  `bit_cast`, one-past values, increment/decrement, and compound assignment are unavailable.

### Functions and methods

- `fun` declares a function, not mutable storage. `Fun<(P1, P2) : R>` is a function-pointer type;
  omit `: R` for no result.
- Fun is valid only as a binding, function parameter, parameter inside another Fun, or union member.
  It is invalid as a result, object/ADT member, collection element/value, Task argument/result,
  Channel element, Stream element/state, Ptr/MutPtr pointee, or `ref` target. Function declarations
  are not addressable.
- Calls require exact arity and assignable arguments. No-result calls are statements only. Results
  must match their declarations; result-producing bodies cannot fall through.
- `impl Receiver.method(...)` adds an implicit fixed `self`, no fields or runtime dispatch. User
  targets are nominal `T`, `Ptr<T>`, or `MutPtr<T>`. Value receivers copy; Ptr reads caller storage;
  MutPtr may write its `mut` members.
- Receiver adaptation order: exact target; outermost MutPtr weakening; pointer dereference to copied
  `T`; implicit `ref` from a capability-compatible addressable `T`.
- One method name exists at most once across an object's receiver forms. It cannot equal a member
  name or be extracted as a function value.
- There is no overloading, default/named/variadic argument syntax, static method, function literal,
  or closure.

### Generics

- User parameters are types only. Compiler-owned `Array<T, N>` uses a positive integer literal N.
- Specializations are invariant and keyed by declaration identity plus canonical arguments; repeated
  requests reuse one. Only reachable concrete specializations emit C; there is no erasure or runtime
  generic representation.
- Explicit type arguments must be complete. Otherwise inference uses typed arguments, expected
  result, and initializer fields; conflicts or unresolved parameters are errors.
- A balanced `<...>` is generic syntax only when immediately followed by call arguments, a qualified
  constructor/member, or object literal. Otherwise `<`, `>`, and `>>` are operators.
- A generic function value needs an exact expected Fun type. Generic methods inherit receiver
  arguments and infer or explicitly receive their own.
- Bodies are checked structurally at declaration and rechecked after substitution. Same-argument
  recursive specialization is allowed; argument-changing recursive cycles are rejected.

### Structural unions

- A union holds exactly one active member; injection is implicit and allocation-free. Unions are
  flattened, duplicate-free, structural, and order-independent. Written order only chooses among
  contextual initializer candidates.
- Widening is allowed only when every source member fits the destination; implicit narrowing and
  declaration-time union inference do not exist.
- `is` tests an exact active member. Narrowing applies to direct local reads; assignment or writable
  address escape invalidates it.
- `is Nil` is invalid, and `T | Nil` also rejects `is T`; use `== nil`/`!= nil`. Larger nullable
  unions may test non-Nil members, and match type patterns may name Nil.
- General unions use tag plus inline payload. Exactly one pointer-like member plus Nil uses the null
  niche. Member operations require narrowing.
- Union equality requires identical canonical union types and equality-capable members; ordering is
  unavailable. Members may be any storable value. Atomic and Unknown cannot be members.

### Algebraic data types and match

- An ADT is a nominal closed sum with at least two distinct qualified variants. Unit variants are
  values; record variants require exhaustive named payload initialization. Payload fields are fixed.
- Direct by-value recursion is invalid; pointer-indirect recursion and generic
  specialization are valid.
- `match` is an expression and evaluates its scrutinee once. Value mode matches `true`/`false`.
  Type mode (`match value is`) matches exact complete types, individual union members, Nil, or ADT
  variants; a union type itself is not one pattern.
- Arms are `| pattern then expression`; optional final `else` is catch-all. Match is exhaustive;
  duplicates and patterns unable to match remaining values are errors. Arms run in source order.
- Arm result types agree unless an expected result accepts every arm. A named scrutinee narrows only
  inside its arm; ADT arms expose only that variant's payload.
- Unparenthesized `|` starts another arm. Bitwise-or scrutinees/results require parentheses. An `is`
  following the scrutinee marks type mode; a scrutinee containing `is` requires parentheses.

## Numeric conversions and operators

### Lossless widening

Typed numeric values widen implicitly only when every source value is exactly representable:

| Source | Destinations excluding identity |
| --- | --- |
| `Int8` | `Int16 Int32 Int64 Float32 Float64` |
| `Int16` | `Int32 Int64 Float32 Float64` |
| `Int32` | `Int64 Float64` |
| `Int64` | none |
| `UInt8`/`Byte` | `UInt16 UInt32 UInt64 Int16 Int32 Int64 Float32 Float64` |
| `UInt16` | `UInt32 UInt64 Int32 Int64 Float32 Float64` |
| `UInt32` | `UInt64 Int64 Float64` |
| `UInt64` | none |
| `Float32` | `Float64` |
| `Float64` | none |

- Widening applies to initialization, assignment, arguments, returns, fields, collection insertion,
  and binary common-type selection.
- Size common-type selection is range-based: select Size when the other unsigned range fits Size;
  select the fixed type when every Size value fits it; select Size for equal ranges; otherwise
  reject. Signed/Size mixes generally have no common type. On 64-bit Size targets, Size and UInt64
  widen both ways and mix to Size. Narrower Size requires explicit UInt64-to-Size conversion.
  Canonical identities remain distinct.
- Binary numeric operations choose the unique least type losslessly reachable from both operands.
  Surrounding result context does not change that choice. Rune never widens implicitly.

### Explicit conversion

- `value.to<T>()` is the only explicit scalar conversion; T is mandatory and the call has no value
  arguments. Identity conversions are no-ops and Byte canonicalizes to UInt8.
- Constants outside the destination domain fail compilation. Dynamic invalid values trap before an
  unsafe C conversion.
- Integer conversion preserves the mathematical value. Integer/float and float/float round nearest,
  ties-to-even; finite overflow traps. Float/integer truncates toward zero then checks range; NaN and
  infinities are invalid. Rune conversions also check Unicode scalar validity.
- Bool/numeric and pointer conversions are invalid. Wrapping, saturating, unchecked,
  destination-named, and mode-selecting conversions do not exist.
- `bit_cast<T>()` reinterprets same-width bits; it is not a value conversion.

### Operators

Precedence, highest first: postfix; prefix `- ! ~ try ref spawn`; `* / %`; `+ -`; `<< >>`;
`< <= > >=`; `is`; `== !=`; `&`; `^`; `|`; `and`; `or`. Binary operators associate left and
prefix operators right.

- Integer `+`, `-`, `*`, unary `-`, and left shift wrap modulo width with defined two's-complement
  results. Constant folding uses the same rule. Unary `-` rejects typed unsigned values.
- Integer division truncates toward zero; remainder follows the dividend sign. Evaluated known zero
  divisors are compile errors; dynamic zero traps. A signed type's `MIN / -1` yields MIN and
  `MIN % -1` yields zero.
- Floating arithmetic follows IEC 60559; `%` is integer-only and NaN comparisons follow IEC rules.
- Bitwise operations accept fixed integers and Size. Shift counts must be `0..width-1`; bad constants
  fail and bad dynamic counts trap. Signed right shift is arithmetic, unsigned zero-filling.
- `bit_cast<T>()` supports equal-width fixed integers and Float32/64, excluding pointers, Size, Rune,
  and aggregates. Fixed integers provide `to_le_bytes()`/`to_be_bytes()` and
  `T.from_le_bytes(array)`/`T.from_be_bytes(array)` through exact `Array<Byte, N>`.

### Equality, ordering, and truthiness

- Numeric comparison uses the lossless common type. Other comparisons require identical canonical
  types. Bool, Rune, Nil, EoS compare by value; pointers by identity; text by UTF-8 bytes; objects by
  members; ADTs by tag/payload; unions by member; Array/View/List by length then elements.
- String and Strand are not mutually comparable. Functions, allocators, Files, and Dicts have no
  equality. An aggregate is comparable only when all recursively compared components are.
- Ordering exists only for numeric scalars, Rune, String, and Strand. Text uses unsigned-byte
  lexicographic order with shorter prefix first.
- Only `false` and `nil` are falsey. Truthiness applies to conditions and `!`, `and`, `or`; it is not
  Bool conversion or union narrowing. Logical operators return Bool and short-circuit left-to-right,
  while both operands must still be valid expressions.

## Control flow and cleanup

- `if`/`elseif`/`else` and pre-tested `while` end with `end`; loops require `do`. `break` and
  `continue` target the nearest loop.
- Branches and loop iterations are scopes. Locals may shadow outer names; assignments may reach
  accessible outer mutable bindings.
- `is`/nil facts follow control flow and may survive a branch ending in return/break/continue.
- Every continuing path in a result-producing function must return. A loop always may fall through,
  including `while true`; break/continue never satisfy a return requirement.
- `defer expression` registers cleanup in the current scope. Actions run in reverse registration
  order on fallthrough, return, break, or continue. A direct call captures callee, receiver, and
  arguments at registration; other expressions evaluate on exit.
- `errdefer` uses the same rules but runs only while the function exits with active Error. It shares
  reverse order with defer on Error exit and is discarded otherwise.
- `try` is invalid in cleanup actions; cleanup result values are discarded. Process traps need not
  run cleanup.

### `for ... in`

- Sources: Array, View, List, String, Strand, Dict, Stream. Optional first binder is zero-based Size.
  Sequences/Stream then bind value; Dict binds key and value.
- Text iterates decoded Runes; Dict order is unspecified; Stream pulls through eos.
- Finite-source traversal boundaries are captured once. Array places iterate in place; temporary
  Arrays and Strands materialize once; handles copy shallowly. Stream captures its source but no
  boundary and keeps pulling until eos.
- Binders are fresh immutable copies each iteration and names in one header are distinct. Nullable or
  union sources must first narrow to one iterable type.
- Array/List element replacement during iteration is allowed. Structural List changes and every Dict
  mutation invalidate traversal; this is programmer responsibility.

## Errors

- Protected nominal `Error` has fixed immutable fields `file: String`, `line: Size`, `column: Size`,
  `header: Strand`, `message: String`.
- `Error.new(header, message)` is the only constructor and injects filename plus one-based line and
  UTF-8 byte column. Propagation preserves the location.
- Fallible functions return structural unions containing Error; there are no exceptions or hidden
  result channels. Error copying is shallow. Runtime `message` String storage must remain live while
  any alias can be inspected or printed.
- `try expression` requires exactly one Error member and at least one success member. It evaluates
  once, returns Error unchanged, or yields the normalized success value/union. It does not catch
  traps.
- `try` and `errdefer` are valid only inside a function whose declared result accepts Error; both are
  invalid at root scope. `try` is additionally invalid inside any cleanup action.

## Allocation and lifetime

- `Heap.new()` selects the default allocator without runtime allocation; Heap operations are
  thread-safe.
- `h.allocate<T>(initial)` allocates and initializes one complete finite T, returning non-owning
  `MutPtr<T>`. Failure or unrepresentable size traps.
- `h.free(ptr)` accepts Ptr/MutPtr and requires the matching allocator.
- Heap-backed library values receive their Heap explicitly; allocation and cleanup never choose a
  hidden allocator.
- Freeing a container releases only its own header/backing region. It never frees allocations its
  elements or nested handles refer to. Referenced owned allocations require cleanup before loss of
  reachability, exactly once per distinct allocation rather than per alias or slot.
- The shallow rule applies at every depth. Replacing/dropping the last handle may leak; freeing one
  alias dangles all others. Runtime metadata may catch live mismatch or double-free, but later
  lifetime misuse is not guaranteed to be detected.

## Collections

### Common rules

- Lengths, capacities, indices, and normalized bounds use Size. Index arguments may be any integer
  and are normalized with compile-time rejection or dynamic traps.
- Ranges are zero-based and end-exclusive. `length`, `is_empty`, `at`, indexing, and `slice` use the
  same bounds where available.
- Array/View/List equality compares length then elements. No collection ordering; no Dict equality.

### `Array<T, N>`

- Fixed inline sequence; N is a positive integer literal. A contextual `[a, ...]` must contain
  exactly N elements, evaluated left-to-right.
- Assignment, arguments, and returns copy the inline region. Element writes require a writable Array
  place. Indexing/at are checked; slice returns View.
- Elements follow general storability, including nested Arrays and eligible unions/handles. Arrays
  free nothing; external-state elements copy only their references.

### `View<T>`

- Non-owning read-only contiguous pointer-length descriptor. T follows general storability; MutPtr
  elements retain pointee capability.
- Constructors: `slice`, `View<T>.from_pointer(pointer, length)`, `View<T>.empty()`.
  `from_pointer` accepts statically non-null Ptr/MutPtr, evaluates pointer then length once, weakens
  MutPtr, and performs no allocation, copy, mutation, or pointer arithmetic.
- `from_pointer` requires contiguous initialized aligned storage with sufficient lifetime. It rejects
  pointers locally traceable to `ref` and accepts heap or opaque parameter pointers. Interprocedural
  provenance from a caller argument is not checked.
- Views may occupy every storable position. They cannot root in temporary Array/List storage or be
  addressed with `ref`. Root-level View bindings are locals. Bounds checks remain active after
  construction.
- A View may return when rooted in a parameter, parameter-reached storage, `from_pointer` region, or
  empty View. A directly returned local-rooted View is rejected. Direct View return analysis does not
  inspect Views nested in returned objects, ADTs, unions, or collections.
- Resize invalidation and `from_pointer` region lifetime are not tracked. View validity requires a
  valid source.

### `List<T>`

```text
List<T>.new(heap) -> List<T>      length() -> Size       is_empty() -> Bool
at(index) -> T                    [index] -> T/place     slice(start,end) -> View<T>
push(value)                       pop() -> T             set(index,value)
clear()                           free(heap)             stream(heap) -> Stream<T>
```

- Growable allocated sequence. A fixed handle can mutate its List; `mut` only reassigns the handle.
  `pop` traps when empty; access and set are bounds-checked.
- T follows general storability. Every operation copies/discards T shallowly, including String.
  `set`, `clear`, and `free` drop slots without freeing referents; free releases only List storage.
- Values read or popped are aliases. Each distinct referenced owned allocation requires exactly one
  cleanup before loss of reachability. Repeated aliases must not be freed per slot. Reverse defer
  order runs later-registered element cleanup before earlier-registered container cleanup.

### `Dict<K, V>`

```text
Dict<K,V>.new(heap) -> Dict<K,V>  insert(key,value)      get(key) -> V
contains(key) -> Bool             remove(key) -> V       free(heap)
```

- Open-addressing allocated dictionary. K is exactly Int32 or Strand; V follows List eligibility.
  Missing get/remove trap; insert replaces.
- Keys and values copy shallowly. Reads/removal return aliases; replacement/free drop entries without
  freeing referents. Free releases only buckets/header. Overwriting the final reachable handle leaks
  its referent.
- Hashing is internal and infallible for supported keys. Equal values hash equally; Strand hashes
  logical payload excluding terminator/zero tail. Algorithm, seed, and iteration order are unstable
  and unspecified; no source hash operation exists.

## Text

- Byte is UInt8. A byte literal contains exactly one printable ASCII byte or one of
  `\\ \' \n \r \t \0 \xHH`.
- A Rune literal contains one Unicode scalar and also supports `\"` and `\u{HEX}`; it is not a
  grapheme cluster.
- String is immutable UTF-8 behind a non-null pointer-sized handle. Runtime values use one
  header-plus-bytes allocation; literals use static storage. Strand is immutable literal-only inline
  32 bytes: at most 31 UTF-8 bytes, NUL, then zero fill; embedded NUL/invalid UTF-8/overflow reject.
- String/Strand indexing and length count Runes; byte Views count bytes.

```text
String: length() -> Size, is_empty() -> Bool, at(index)/[index] -> Rune
bytes() -> View<Byte>, slice(runeStart,runeEnd) -> View<Byte>
rune_cursor() -> RuneCursor, to_string(heap)/concat(heap,other) -> String, free(heap)
String.from_bytes(heap,View<Byte>) -> String
String.from_runes(heap,View<Rune>) -> String
Strand: length() -> Size, is_empty() -> Bool, at(index)/[index] -> Rune
to_string(heap) -> String
```

- String slice uses Rune bounds and returns the corresponding zero-copy UTF-8 bytes. `from_bytes`
  validates before allocation and traps on malformed UTF-8.
- RuneCursor borrows String and has `has_next() -> Bool` and `next() -> Rune`; next traps after
  exhaustion. Copies hold independent positions over the same storage.
- Runtime String allocations require one matching free; all aliases then dangle. Literals must never
  be freed. Collection reads produce aliases without ownership transfer or lifetime protection.
- String and Strand dispatch separately; Strand exposes no View into inline bytes.

## Streams

- `Stream<T>` is lazy, single-pass, single-threaded pull state with no length, capacity, random
  access, rewind, or concurrent communication. T and producer State are complete finite copyable;
  T cannot be EoS or a top-level union containing EoS.
- `Stream<T>.new()` is allocation-free empty. `produce(heap,state,callback)` stores shallow State and
  named `Fun<(MutPtr<State>) : T | EoS>`. `List<T>.stream(heap)` borrows the List.
- `next()` returns `T | EoS`; one call yields at most one public value, though filtering may pull
  upstream repeatedly. `filter(heap,predicate)`, `map(heap,mapper)`, and `take(heap,count)` allocate
  lazy adapters that own their upstream. Successful construction conventionally consumes upstream:
  do not pull, adapt, or free another alias. One chain uses one Heap.
- `for` pulls until eos and does not free; breaking permits later pulls. Indexed loops count produced
  values, not rejected filter inputs.
- `free(heap)` releases the adapter chain; exhaustion does not. External producer resources remain
  caller-owned. A List source captures length, sees later same-length replacements, and requires the
  List alive and structurally unchanged until Stream free. Aliases share one non-reentrant cursor.

## Output and files

### `print`

- `print(arg, ...)` is protected, requires at least one argument, inserts no separator/newline, and
  returns no value. Arguments evaluate once left-to-right; output starts only after all evaluation.
- Printable: Bool, integers, Size, Byte, floats, Rune, String, Strand, Nil, Error, objects, ADTs,
  Array/View/List, and Dict when recursively printable. Pointers, functions, unions, EoS, allocators,
  Files, Streams, and resources are rejected; unions must narrow first.
- Direct text/Rune is raw; nested text/Rune is quoted/escaped; Byte is numeric. Structural forms are
  fixed, one line, and exactly:

```text
Point { x = 10, y = 20 }   object: defining name, declaration order, ` = ` separator
Direction.North            unit variant: qualified name
Shape.Circle { r = 10 }    record variant: qualified name, active payload only
[10, 20, 30]               Array/View/List share one form; `[]` when empty
{"Ada": 10, "Lin": 20}     Dict uses `: ` and unspecified order; `{}` when empty
```

- Float32/64 use `%g` precision 9/17; signed zero and `inf`, `-inf`, `nan` are preserved. A direct
  Error prints `file:line:column: header: message` with no trailing newline; nested, it uses the
  object form with declaration-ordered fields and quoted text.
- A whole call is atomic relative to print and standard text writes. It does not flush per call.
  Root defers finish before the output gate closes; shutdown then flushes stdout/applicable stderr.
  Detected output failure is unrecoverable.

### Files

- FileMode variants: `Read`=`rb`, `Write`=`wb`, `Append`=`ab`; opened files are binary on all
  platforms. FileMode is a qualified unit-variant ADT.
- `File.open(path,mode) -> File | Error`; v1 paths are nonempty non-NUL ASCII. Bad literals fail
  compilation; dynamic failures return Error.
- `read_bytes(heap) -> List<Byte> | Error`; `read_text(heap) -> String | Error`, with malformed UTF-8
  recoverable. `write(View<Byte>)`, `write_text(String)`, and `flush()` return `Nil | Error`; writes
  attempt the full payload but may leave partial effects. `close()` returns nothing; owned close
  failure traps. Flush does not promise physical-media durability.
- Runtime mode mismatch returns Error before C access. I/O Errors use header `"I/O Error"` and static
  portable message; no errno/host code is exposed.
- `Stdio.stdin()`, `Stdio.stdout()`, and `Stdio.stderr()` return borrowed text-mode Files that cannot
  close. Direct invalid operations fail compilation; copied handles retain runtime checks.
- File handles shallow-alias one C stream; closing one invalidates all aliases. Containers never
  close Files. File and Stream are separate; File I/O is synchronous and may block a worker.

## Tasks and synchronization

### Tasks

- `spawn named_function(args)` evaluates arguments once left-to-right, shallow-copies them, and
  returns `Task<R> | Error`; failure starts no task. R may be any complete copyable result including
  Nil/aggregate/union. Recursive Atomic content is excluded. Spawn Error is separate from returned R.
- `join() -> R` waits, copies the exact result, and reclaims storage. `detach()` discards result and
  arranges reclamation. Exactly one successful join or detach is allowed across aliases.
- Scheduler-owned stacks/control/queues need no allocator. `Task.yield()` is the explicit scheduling
  point in one cooperative M:N scheduler over C23 worker threads.
- Targets are Windows x64 and POSIX x86-64 with verified C23 `<threads.h>`; otherwise Task features
  produce Unsupported Error. Root is pinned to worker zero; root return does not join tasks. Stacks
  reserve 1 MiB including guard page.
- Every repeating path through task-reachable literal `while true` visibly executes `Task.yield()` or
  compilation fails.
- Spawn, join, Mutex, Channel, and sequentially consistent Atomic operations provide their specified
  C23 synchronization edges. Unsynchronized conflicting access is a data race with no guarantee.

### `Channel<T>`

```text
Channel<T>.new(heap,capacity) -> Channel<T> | Error
send(value) -> Nil | Error    receive() -> T | EoS    close() -> Nil
free(heap) -> Nil             length() -> Size         capacity() -> Size
is_closed() -> Bool
```

- Bounded MPMC FIFO; capacity zero fails at compile time when known, otherwise with Error. Full send
  and empty receive park Task, not worker.
- Send after close returns Error. Close is idempotent, preserves queued values, and wakes waiters;
  closed/drained receive returns eos. Error is a valid T; receive adds no Error result member.
- Free requires closed, empty, unused state and releases only Channel storage. Elements copy
  shallowly; top-level EoS and recursive Atomic content are invalid.

### `Mutex`

- `Mutex.new(heap) -> Mutex | Error`; operations `lock`, `unlock`, `free(heap)`.
- Allocated scheduler-aware non-recursive lock owned by Task identity. Waiting parks Task. Recursive
  lock, wrong-owner/double unlock, or freeing locked/waited Mutex is programmer error; cheaply
  detectable live misuse traps.

### `Atomic<T>`

- T is Bool, Int32, UInt32, Int64, UInt64, or Size. Inline allocator-free sequentially consistent
  operations: `new`, `load`, `store`, `exchange`, strong `compare_exchange`, and numeric
  `fetch_add`/`fetch_sub` except Bool. Lock-freedom is not guaranteed.
- Atomic and inline aggregates containing one are non-copyable. They cannot initialize from existing
  Atomic state, assign, pass/return, use `ref`, arithmetic, or storage in ADT/union,
  Array/View/List/Dict/Stream/Channel, Task argument/result.
- `Atomic<T>.new(value)` directly initializes fresh binding or object-member storage; these are its
  only placements. Nested object construction initializes each member in place. The resulting object
  is non-copyable but may be shared through Ptr/MutPtr; `ref` of the Atomic member remains invalid.

## Layout, volatile access, and C23 contract

- `size_of<T>()` and `align_of<T>()` require one explicit complete finite type and return Size C
  constant expressions. Reference-like types report source handle size. These operations do not make
  arbitrary Array lengths valid.
- `read_volatile()` exists on Ptr/MutPtr; `write_volatile(value)` requires MutPtr. T is a fixed-width
  integer, Byte, or Size. Receiver/value evaluate once; nullable pointers narrow first. Volatile adds
  only C observability: no atomicity, synchronization, fence, device ordering, address exposure, or
  pointer arithmetic.
- Generated C preserves Hexal semantics instead of inheriting C undefined behavior for overflow,
  shifts, division edges, bounds, union payloads, or conversions. Headers verify fixed-width integer,
  IEC float, and selected size_t assumptions.
- Objects/ADTs lower to source-ordered structs; unions to checked tagged values except pointer-null
  niches; generics are monomorphized. Object forward typedefs precede source-ordered definitions.
- Pointer qualification follows type layers only: Ptr adds pointee `const`, MutPtr does not, and a
  fixed binding adds trailing `const`. Object members themselves are unqualified. No
  qualifier-discarding cast is emitted.

| Hexal | C23 |
| --- | --- |
| `Ptr<Int32>` | `const int32_t *` |
| `MutPtr<Int32>` | `int32_t *` |
| `Ptr<Ptr<Int32>>` | `const int32_t *const *` |
| `MutPtr<Ptr<Int32>>` | `const int32_t **` |
| `Ptr<MutPtr<Int32>>` | `int32_t *const *` |
| `MutPtr<MutPtr<Int32>>` | `int32_t **` |
| `Ptr<Unknown>` / `MutPtr<Unknown>` | `const void *` / `void *` |

- Checked operations retain structured identities through generation; source-derived opaque C
  fragments are never passed through the checker.
- Lexing/parsing own syntax errors; checking owns type/semantic errors at the earliest proving phase;
  runtime traps own invalid dynamic state. An unclassifiable compiler inconsistency is Unknown Error.

## Excluded features

- Modules/FFI: native module syntax, C imports/exports.
- Memory: Arena, Pool, source pointer arithmetic/casts, `unsafe`, mutable View.
- Control/iteration: ranges, counted loops, user iterators, mutable iteration binders, exceptions.
- Functions/concurrency: closures, async/await, coroutines, user threads, task groups, `select`,
  unbounded/rendezvous Channels, nonblocking Channel operations, memory-order arguments.
- Extensibility: operator overloading, user truth/display/hash protocols, generic constraints,
  reflection, serialization schemas, runtime type objects.
- Expressions: compound assignment, increment/decrement, conditional operator, numeric suffixes,
  wrapping/saturating conversion or arithmetic modes.
- I/O: ReadWrite/seek File modes, Path, sockets, asynchronous I/O, File Streams, builders, in-memory
  output streams.
