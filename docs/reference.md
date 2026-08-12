# Hexal Language Reference

This is the sole normative semantic reference for Hexal. It distills RFCs
0001-0045 after applying their explicit supersession rules, and adds the
decisions recorded in RFC 0046, which is a draft. Historical specs explain why;
this file defines what Hexal means now. Compiler behavior that disagrees with
this file is a conformance bug, not a new language rule.

## Status and design boundary

- Hexal is a statically typed, high-level systems language that lowers to
  readable C23 with `#line` mappings.
- Compilation is forward-only and fail-closed. Invalid or unsupported source
  must produce a structured diagnostic; it must never be silently omitted.
- Values and lifetimes follow C: copying is shallow unless an operation
  explicitly says otherwise, allocation is explicit, and cleanup is manual.
- Native modules, C interop, Arena, and Pool are not current language features.
  Their RFCs (0027, 0034, and 0039) remain drafts.
- Closed specs are historical records. When they disagree with this reference,
  the later implemented decision summarized here is authoritative.
- `grammar.ebnf` is the formal syntax companion and `status.md` tracks delivery;
  neither defines semantics.
- Execution plans, compiler-internal architecture, and test organization are
  intentionally omitted unless they impose a visible language guarantee.

Applied resolution chain:

- RFC 0007 replaces RFCs 0001-0002 pointer/mutability behavior.
- RFC 0010 makes pointers non-null and RFC 0014 generalizes `|` unions.
- RFC 0016 replaces identical-only numeric typing; RFC 0017 closes C integer
  undefined-behavior gaps; RFC 0038 makes `to<T>()` the sole conversion syntax.
- RFC 0023 replaces Bool-only conditions/logical operands with truthiness.
- RFC 0028 replaces delimiter-free `while` and adds `for ... in ... do`.
- RFC 0035 removes affine ownership and borrow checking in favor of C-style
  copying/manual lifetimes. This is total: there is no deep-copy or
  nested-cleanup exception for any element type.
- RFC 0036 replaces `UInt64` in-memory sizes with `Size`.
- RFC 0044 restores `Byte`, completes Rune/text behavior, and fixes String/
  Strand representation after RFCs 0018 and 0020.
- RFC 0045 changes live spelling and generated namespaces from Seawitch/`sw_`
  to Hexal/`hex_`; archived specs retain their historical wording.

Conflicts between specs, resolved here:

- RFC 0031 wins over RFC 0036 on `Stream`. RFC 0036 lists `Stream<T>.length()`,
  `Stream<T>.capacity()`, and a capacity argument to `new`; RFC 0031 removes all
  three. Stream has no length or capacity.
- RFC 0008 wins over RFC 0006 on object-literal initializers, which it
  explicitly supersedes: their relative evaluation order is unspecified, not
  written order. Any observed order is an implementation choice, not a
  guarantee.
- RFC 0020 is labelled Implemented but its own readiness section still calls
  itself Draft, and most of its ownership model is superseded by RFCs 0035,
  0036, and 0044. Read it only through this chain.
- Several specs contain syntax that the language does not have: `:=` inference
  (RFCs 0016, 0017, 0029, 0036) and `if cond then` (RFCs 0029, 0037, 0043).
  Neither exists. Do not reintroduce them.
- RFC 0020's rejection of `String`, `List`, and `Dict` as union members existed
  only to serve its affine ownership model, which RFC 0035 removed. They are
  ordinary union members. Only `View` stays excluded, for its own reason.
- RFC 0020 left the Array/List union element class undecided. Any union whose
  members all qualify as elements is eligible.
- RFC 0020's remaining placement bans — handles out of Arrays, nested
  collections out of Lists and Dicts, and Views out of objects and collections —
  existed only to keep its borrow and ownership tracking decidable. RFC 0035
  removed that tracking, so they are removed too and replaced by the single
  storability rule under "Value classification". Only `Atomic`, `Fun<...>`, and
  `Unknown` keep exceptions, each for its own independent reason. `View` keeps
  none: RFC 0043 restated RFC 0020's View bans as a coordination note rather
  than a fresh decision, and they fall with the tracking that motivated them.
- RFC 0029's `main.seawitch` and the source extension are superseded: files use
  `main.hex` as the synthetic filename, anticipating `.hex` as the source
  extension once file loading is specified.
- RFC 0020's direct-`String` List/Dict slots deep-copied on insertion and were
  destroyed on `set`/`clear`/`free`. That existed to serve its affine ownership
  model and contradicts RFC 0035's shallow rule, so it is removed: `String`
  elements copy and discard like every other element.
- RFC 0020 and RFC 0043 reject returning a `View` outright. Returning one is
  permitted when its root outlives the call; only a local-rooted return is
  rejected.

Known conformance gaps, where the compiler does not yet match this file. This
file is authoritative; each is an implementation bug:

- Only a 64-bit `Size` target profile exists, so RFC 0036's 16- and 32-bit
  profiles are unimplemented rather than merely untested. RFC 0046 explicitly
  leaves them out of scope.
- Match scrutinees and arm results are parsed at restricted precedence levels,
  so unparenthesized `and` and `or` expressions are rejected where this file
  permits them.
- `ref` accepts member steps followed by index steps, but not a general mixed
  place such as `ref rows[0].field`.
- Raw newlines inside String literals are accepted by the lexer although the
  lexical rules reject them.

RFC 0046 resolved the migration gaps above; only the 16- and 32-bit `Size`
profiles and the three syntax gaps remain.

## Lexical rules

- Identifiers are case-sensitive and match `[A-Za-z][A-Za-z0-9_]*`.
  Leading `_` and leading digits are invalid.
- `main`, C keywords, and C macro names are valid Hexal identifiers.
- Whitespace is insignificant. Statements have no terminator.
- A call's opening `(` must be on the same source line as the final token of
  its callee. Arguments may span lines.
- A `return` value must begin on the same source line as `return`; otherwise the
  statement is a bare return.
- `--` starts a line comment. `--[ ... ]--` is a multiline comment. `---` is
  consumed as a line comment.
- Integer literals:
  - decimal, `0x` hexadecimal, `0b` binary, or `0o` octal;
  - lowercase base prefixes only;
  - `_` only between digits;
  - no suffixes or implicit C-style octal;
  - nonzero decimal values cannot have leading zeroes.
- Floating literals are decimal `digits.digits` with an optional exponent, or
  `digits` with a required exponent. `.5`, `1.`, and hexadecimal floats are
  not source forms.
- String escapes: `\'`, `\"`, `\\`, `\n`, `\t`, `\r`, `\0`, and
  `\u{HEX}`. Raw newlines are invalid inside a String literal.
- Byte literals use `b'...'`; Rune literals use `'...'`.
- Operator tokens use maximal munch: `!=`, `==`, `<=`, `>=`, `<<`, and `>>`
  are single tokens. In nested type arguments only, `>>` may close two levels.
  `&&`, `||`, `++`, `--`, and every compound-assignment spelling are invalid.
- Reserved words/literals are `true`, `false`, `nil`, `eos`, `mut`, `ref`,
  `type`, `and`, `or`, `is`, `fun`, `impl`, `end`, `return`, `if`, `elseif`,
  `else`, `while`, `break`, `continue`, `defer`, `try`, `errdefer`, `spawn`,
  `as`, `match`, `then`, `self`, `for`, `in`, and `do`.

## Program structure and names

- A source file contains ordered type, function, method, and executable
  declarations/statements.
- Executable top-level statements belong only to the root program and lower to
  automatic locals inside generated `main`; they are not C globals.
- Hexal has no native global values, global constants, `global`, or `static`.
  State is local, heap allocated, or passed explicitly.
- Functions and methods are file-scope declarations. They do not capture
  root locals or surrounding lexical state; nested functions and closures do
  not exist.
- Declarations become visible in source order. A function may call itself or
  an earlier declaration; forward calls and mutual recursion are unavailable.
- Type and value names share one visible namespace. Built-in and protected
  names cannot be redeclared or shadowed.
- Protected type names include every scalar plus `Size`, `Byte`, `Rune`,
  `String`, `Strand`, `Nil`, `EoS`, `Unknown`, `Heap`, `Error`, `File`,
  `FileMode`, `RuneCursor`, `Mutex`, and the constructors `Ptr`, `MutPtr`,
  `Fun`, `Array`, `View`, `List`, `Dict`, `Stream`, `Task`, `Channel`, and
  `Atomic`. `print`, `size_of`, `align_of`, and the intrinsic qualifier
  `Stdio` are protected value/operation names.
- Generated private C names use the `hex_` namespace (`HEX_` for macros). One
  fixed prefix is applied to the complete source spelling:

| Declaration kind | Prefix | Example |
| --- | --- | --- |
| value binding | `hex_v_` | `score` → `hex_v_score` |
| nominal type | `hex_t_` | `Point` → `hex_t_Point` |
| object member | `hex_m_` | `x` → `hex_m_x` |
| function or method | `hex_f_` | `add` → `hex_f_add` |

- The mapping is unconditional. `int` becomes `hex_v_int`, `INT32_MAX` becomes
  `hex_v_INT32_MAX`, and an already-prefixed `hex_v_score` becomes
  `hex_v_hex_v_score`. Names are never conditionally escaped, hashed, or
  truncated, and foreign C names are outside the rule.
- The synthetic source filename, used by diagnostics, `#line` output, and
  `Error.file` when no name is supplied, is `main.hex`. `.hex` is the intended
  source extension; file loading is not yet specified, so nothing currently
  reads or writes files.

## Bindings, places, and copying

```hexal
answer: Int32 = 42
mut score: Int32 = 0
score = answer
```

- Every ordinary storage declaration and written parameter has an explicit
  type. `self` and `for` binders are compiler-typed exceptions. There is no
  `:=` syntax.
- Bindings and object members are fixed by default. `mut` permits replacement
  and appears only before a binding or an object member declaration.
- Parameters, `self`, and `for` binders are always fixed. They cannot be
  declared `mut`, assigned, or shadowed within their own scope.
- A writable member assignment requires a writable root place and `mut` at
  every object-member step. Pointer dereference writability comes from the
  pointer type.
- Assignment, arguments, returns, object/ADT construction, and collection
  insertion all copy the C representation. There is no exception for any type.
  The source remains usable.
- Scalars and inline aggregates, including `Strand`, `Array`, objects, ADTs,
  and `File`, copy their value. Pointers, function values, `String`, `List`,
  `Dict`, `Stream`, `Task`, `Channel`, and `Mutex` copy a handle. `View` copies
  its pointer-length descriptor. `Heap` copies a compile-time allocator
  identity and creates no runtime object. Copying is recursive and shallow.
- There are no moves, owner states, borrow states, retain counts, implicit
  destructors, or compiler-enforced exactly-once cleanup.
- Reassigning or clearing the last handle can leak. Freeing through one alias
  leaves every other alias dangling. The programmer owns these lifetime rules.
- Full statements execute in source order. Unless a feature states otherwise,
  operator operands, call arguments, method receivers versus arguments, and
  object-literal initializers follow C23's unspecified relative order. Use
  separate statements when order matters.

## Built-in types

### Scalars and singletons

| Hexal | Meaning | C23 |
| --- | --- | --- |
| `Bool` | `false` or `true` | `bool` |
| `UInt8`, `UInt16`, `UInt32`, `UInt64` | exact-width unsigned integers | `uint*_t` |
| `Int8`, `Int16`, `Int32`, `Int64` | exact-width signed integers | `int*_t` |
| `Float32`, `Float64` | IEC 60559 binary32/binary64 | `float`, `double` |
| `Size` | target-sized unsigned lengths and indices | `size_t` |
| `Byte` | transparent alias of `UInt8` | `uint8_t` |
| `Rune` | Unicode scalar value | `uint32_t` |
| `Nil` | singleton type whose value is `nil` | `nullptr_t` where needed |
| `EoS` | stream/channel completion singleton `eos` | compiler-defined |

- `Size` is exactly the selected target's `size_t`: its width, alignment, range,
  and C representation are platform-determined. Supported target widths are 16,
  32, and 64 bits; any other width is rejected before checking source that uses
  `Size`. The target profile fixes the width before type checking, and generated
  C asserts that the compiling toolchain's `size_t` matches. Which target
  profiles ship today is a delivery question, not a language rule.
- `Size` is canonically distinct from fixed-width integers even when widths
  match, so `Size` and `UInt64` are different types on a 64-bit target.
- `Rune` is distinct from `UInt32` and excludes surrogate values.
- `Int`, `UInt`, `Float`, `Double`, `Char`, `Long`, `ISize`, and `Void` are not
  built-ins.
- A function with no result omits `: Type`; that is distinct from returning
  `Nil`. Use `: Nil` and `return nil` when a first-class result is required.

### Value classification

Three independent properties, used throughout this file instead of repeating
type lists. They are independent: a value may have any combination.

**Representation.** Every value is stored inline where it is placed, and every
copy is shallow. A `String` or `List` is a pointer-sized handle, so copying one
copies the handle — the value is still stored inline. Hexal has no
by-reference storage and no deep copy anywhere.

**External state.** Some values refer to state that outlives an individual
copy: `String`, `List`, `Dict`, `Stream`, `Task`, `Channel`, `Mutex`, `File`,
`RuneCursor`, and `View`, plus any `Array`, object, ADT, or union that
transitively contains one. Copying such a value produces another alias to the
same state, never a second copy of it. Cleanup is per-type and explicit, and is
described at each type's own surface: `free` for most, `join` or `detach` for
`Task`, `close` for `File`, and nothing at all for a `String` literal, a
`RuneCursor`, or a `View`, none of which own what they refer to.

**Copyability.** Every value is shallow-copyable except `Atomic<T>` and any
value transitively containing one.

Storability is one rule: **any complete, finitely sized, copyable value may be
stored in any position that stores a value** — object and ADT members, union
members, `Array`, `View`, and `List` elements, and `Dict` values. An aggregate
is storable when its members are, so an object holding a `String` or a `List`
is an ordinary storable value and `Error` is an ordinary union member.

Three exceptions, each for its own reason rather than an ownership one:

- `Atomic<T>` is not copyable, so it never moves into or out of any position.
  It exists only where `Atomic<T>.new(...)` directly initializes its final
  binding or object member; see "Atomic".
- `Fun<...>` keeps the narrower placement rules under "Function values".
- `Unknown` is incomplete and is storable only as a pointee.

`View<T>` has no placement exception. It is a plain read-only descriptor, so it
is storable wherever any other value is, including as a union member and as
another View's element. Keeping the storage it borrows alive is your
responsibility, exactly as it is for a local View.

Storing a value that refers to external state stores the reference only. A
container frees its own region; whatever its elements refer to stays yours.

### Contextual literals

- An integer literal is an exact mathematical value until context selects an
  integer type. Without context it defaults to `Int32` and must fit.
- A decimal floating literal uses an expected `Float32` or `Float64`; otherwise
  it defaults to `Float64`.
- A direct negative literal is negated before range checking, so signed minima
  are expressible. `-0.0` is IEEE negative zero. Negating a literal in an
  unsigned context is invalid, including `-0`.
- Literals may adopt an expected type directly; this is not an implicit
  conversion of an already typed value.
- An expected type reaches untyped literals transitively through arithmetic
  operators, but only literals: a typed operand is never retyped by context.
- A comparison or logical context supplies no expected type, because `Bool` is
  not an arithmetic operand type. Operands there fall back to `Int32`/`Float64`,
  so `5_000_000_000 > 1` is an error while `threshold: Int64 = 5_000_000_000`
  followed by `threshold > 1` is valid.

### Aliases and objects

```hexal
type Count = Int32
type Point = { mut x: Int32, y: Int32, }
point: Point = Point { y = 2, x = 1, }
```

- `type Alias = ExistingType` creates a transparent alias with identical
  canonical identity, representation, and operations. It emits no C typedef.
- Alias targets are resolved in source order. Recursive aliases are invalid.
- Object types are nominal, ordered, inline values and must declare at least
  one member. Structurally identical declarations are still different types.
- Object literals name every member exactly once, in any order; a trailing
  comma is allowed. Missing, unknown, and duplicate members are errors.
- Pointer-indirect self-recursion is valid. Direct or mutually recursive
  by-value layouts are invalid.
- Pointer member access auto-dereferences (`node.next`); `.value` remains the
  explicit whole-pointee access and is required for non-object pointees.

### Pointers and nullability

```hexal
reader: Ptr<Int32> = ref answer
writer: MutPtr<Int32> = ref score
mut maybe: Ptr<Int32> | Nil = nil
```

- `Ptr<T>` is a non-null, non-owning pointer to a read-only `T`.
- `MutPtr<T>` is a non-null, non-owning pointer to a writable `T`.
- `ref place` is the only address-taking form. It yields `MutPtr<T>` for a
  writable place and `Ptr<T>` for a fixed place.
- `MutPtr<T>` weakens implicitly to `Ptr<T>` at the outermost layer only.
  Upgrading or weakening a nested pointer layer is invalid.
- `.value` dereferences. A `MutPtr` pointee is writable; a `Ptr` pointee is not.
- Nullability is explicit: `P | Nil`, where `P` is a data or function pointer.
  A nullable data pointer must be narrowed by `== nil`, `!= nil`, or `match`
  before dereference. `is` is unavailable here: see the union rules below. The
  union uses the pointer null niche and adds no tag or allocation.
- `Unknown` is an incomplete pointee used only behind `Ptr`/`MutPtr`. One
  pointer layer may erase to or recover from `Unknown`; it cannot be stored or
  dereferenced by value.
- `String`, `List`, `Dict`, `Stream`, and `View` are already handle/descriptor
  types and cannot be wrapped in `Ptr` or `MutPtr`.
- Hexal pointers name one object. Pointer arithmetic, pointer indexing,
  ordering, subtraction, integer conversion, `bit_cast`, one-past values,
  `++`, `--`, and compound assignment are unavailable.

### Function values, functions, and methods

```hexal
fun add(left: Int32, right: Int32): Int32
    return left + right
end

callback: Fun<(Int32, Int32) : Int32> = add

impl MutPtr<Point>.move(dx: Int32)
    self.x = self.x + dx
end
```

- `fun` declares a function; it is not a storage binding and cannot be `mut`.
- `Fun<(P1, P2) : R>` is a function-pointer type. Omit `: R` for no result.
- `Fun<...>` is valid as a parameter, a local binding, a root-level binding, a
  parameter inside another `Fun<...>`, and a union member, so
  `Fun<(Int32) : Int32> | Nil` works. It cannot be returned from a function, be
  an object or ADT member, be an Array, View, List, or Dict element, be wrapped
  in `Ptr`/`MutPtr`, or be addressed with `ref`, and a function declaration
  itself is not addressable.
- These are the one exception to the general storability rule that is deferred
  work rather than a decision: they await the C declarator, addressability, and
  FFI rules RFC 0008 postponed.
- Calls require exact arity and assignable arguments. Parameters give literals
  their expected type.
- `return` must match the declared result. Result-producing bodies cannot fall
  through; no-result bodies may.
- A call to a no-result function is a statement only. It is rejected wherever a
  value is required, including as an initializer, argument, or operand.
- `impl Receiver.method(...)` declares a method with implicit `self`. Methods
  add no field or runtime dispatch to the receiver.
- A user `impl` target is a nominal object `T`, `Ptr<T>`, or `MutPtr<T>`.
  `self` is fixed and cannot be declared, shadowed, or assigned. A value target
  receives a copy; `Ptr<T>` reads caller storage; `MutPtr<T>` may write its
  `mut` members.
- Method calls adapt the receiver in order: exact target; outermost
  `MutPtr<T>` weakening; pointer dereference to a copied `T`; or implicit
  `ref` from an addressable `T` when the place capability permits it.
- One method name exists at most once across an object's receiver forms. A
  method cannot share an object member's name or be extracted as a function
  value.
- User-defined overloading, default/named/variadic arguments, static methods,
  function literals, and closures do not exist.

### Generics

```hexal
type Box<T> = { value: T }
fun identity<T>(value: T): T
    return value
end
```

- User generic parameters are types only. `Array<T, N>` is a compiler-owned
  exception whose `N` is a positive integer literal.
- Concrete specializations are invariant and keyed by declaration identity plus
  ordered canonical type arguments. Repeated requests reuse one specialization.
- Explicit argument lists must be complete. Otherwise calls and constructors
  infer deterministically from typed arguments, the expected result, and
  initializer fields. Conflicts or unresolved parameters are errors.
- A balanced `<...>` suffix is parsed as generic arguments only when followed
  immediately by call arguments, a qualified constructor/member, or an object
  literal. Otherwise `<`, `>`, and `>>` retain their operator meanings.
- A generic function value is inferred only from an exact expected `Fun` type.
- Generic methods inherit receiver arguments and infer or explicitly receive
  their own arguments.
- Generic bodies are checked structurally when declared; type-dependent
  operations are rechecked for every concrete specialization.
- Recursive specialization may reuse the same active arguments. A recursive
  cycle that changes arguments is rejected.
- Only reachable concrete specializations emit C. There is no runtime generic
  representation, type object, erasure, or dynamic dispatch.

### Structural unions

```hexal
type Number = Int32 | Float64
mut value: Number = 1
if value is Int32
    print(value)
end
```

- A union contains exactly one active member. Member injection is implicit and
  allocation-free.
- Unions are flattened, duplicate-free, structural, and order-independent in
  identity. Written order only selects among contextual initializer candidates.
- A union widens only when every source member fits the destination. It never
  narrows implicitly, and declarations do not infer a union type.
- `is` tests one exact active member and never asks whether one member converts
  to another. Branch narrowing applies to direct local reads; assignment or
  writable address escape invalidates the proof.
- `is` cannot test `Nil`, and a two-member `T | Nil` rejects `value is T`
  because it is the same proof as `value != nil`. Null checks have one
  spelling. A larger union such as `Int32 | Float64 | Nil` still admits
  `value is Int32`, and `match ... is` may use a `Nil` pattern in any union.
- General unions use a tag plus inline payload. Exactly one pointer-like member
  plus `Nil` uses the null niche.
- Member-specific operations require narrowing. `==`/`!=` require identical
  canonical union types and equality-capable members; ordering is unavailable.
- Valid members are any storable value under "Value classification", including
  objects and ADTs that hold handles, which is why `T | Error` works. A nested
  union is flattened before validation.
- `Unknown` has no value representation and is a member only behind a pointer.
  `Atomic` is non-copyable and is never a member.

### Algebraic data types and `match`

```hexal
type Shape =
    | Circle as { r: Int32 }
    | Square as { side: Int32 }

shape: Shape = Shape.Circle { r = 4 }
area: Int32 = match shape is
    | Shape.Circle then shape.r * shape.r
    | Shape.Square then shape.side * shape.side
end
```

- An ADT is a nominal closed sum with at least two distinct variants.
- Variants are qualified by their owner. Unit variants are values; record
  variants require exhaustive named payload initialization.
- Payload fields are fixed. Direct by-value recursion is invalid; pointer-
  indirect recursion is valid. Generic ADTs use ordinary specialization.
- `match` is an expression and evaluates its scrutinee exactly once.
- Value mode matches `true`/`false`. Type mode (`match value is`) matches exact
  complete types, individual union members, `Nil`, or qualified ADT variants;
  a union expression is not one pattern.
- Arms are single expressions: `| pattern then expression`. `else` is the final
  catch-all. Every match must be exhaustive and all arm results must agree,
  unless an expected result type makes each arm assignable.
- In match context, an unparenthesized `|` starts the next arm. Parenthesize a
  scrutinee or arm result that uses bitwise-or.
- Arms are tested in source order. Duplicate patterns and patterns that cannot
  match any remaining value or member are rejected, not ignored.
- An `is` immediately after the scrutinee is the type-mode marker, so a
  scrutinee that itself uses the `is` operator must be parenthesized:
  `match (value is Int32) | true then ... end`.
- A named scrutinee is narrowed only inside its arm. ADT variant arms expose
  only that variant's payload.

## Numeric conversions and operators

### Lossless widening

- Typed numeric values widen implicitly only when every source value is exactly
  representable in the destination.
- Widening applies to initialization, assignment, arguments, returns, fields,
  collection insertion, and binary common-type selection.

| Source | Implicit destinations, excluding identity |
| --- | --- |
| `Int8` | `Int16`, `Int32`, `Int64`, `Float32`, `Float64` |
| `Int16` | `Int32`, `Int64`, `Float32`, `Float64` |
| `Int32` | `Int64`, `Float64` |
| `Int64` | none |
| `UInt8` / `Byte` | `UInt16`, `UInt32`, `UInt64`, `Int16`, `Int32`, `Int64`, `Float32`, `Float64` |
| `UInt16` | `UInt32`, `UInt64`, `Int32`, `Int64`, `Float32`, `Float64` |
| `UInt32` | `UInt64`, `Int64`, `Float64` |
| `UInt64` | none |
| `Float32` | `Float64` |
| `Float64` | none |

- The table above lists fixed-width destinations only; `Size` is separate
  because its range is target-determined.
- `Size` participates by concrete target range: use `Size` when the other
  unsigned range fits it; use the fixed-width type when all `Size` values fit
  it; prefer `Size` for equal ranges; otherwise reject. Signed/`Size` mixes
  usually have no common type.
- Widening with `Size` is therefore directional by range, and both directions
  are lossless when the ranges are equal. Where `Size` is 64 bits, `Size` and
  `UInt64` widen to each other implicitly and a mixed operation selects `Size`
  by the equal-range tie-break. Where `Size` is narrower, `UInt64` to `Size`
  requires `to<Size>()`. The two types stay canonically distinct either way.
- Binary numeric operations use the unique least type reachable losslessly
  from both operands. If none or more than one least candidate exists, an
  explicit conversion is required. The surrounding result type does not
  change the operation's common type.
- `Rune` is not an ordinary numeric operand and never widens implicitly.

### Explicit conversion

```hexal
narrow: Int8 = wide.to<Int8>()
```

- `value.to<T>()` is the only explicit scalar conversion. `T` is mandatory,
  never inferred, and the call takes no value arguments.
- Every conversion is checked. Invalid constants fail compilation; invalid
  runtime values trap before an unsafe C conversion.
- Integer-to-integer preserves the mathematical value and range-checks.
- Integer-to-float and float-to-float round to nearest, ties to even; finite
  overflow traps. Float-to-integer truncates toward zero, then range-checks;
  NaN and infinities are invalid.
- Integer/Rune conversions additionally validate the Unicode scalar range.
- Identity conversions are valid no-ops. `Byte` canonicalizes to `UInt8`.
- Bool/numeric and all pointer conversions are invalid.
- Wrapping, saturating, unchecked, destination-encoded (`to_int32`) and
  mode-selecting conversions do not exist.
- `to<T>()` converts a value; `bit_cast<T>()` reinterprets same-width bits.

### Operators and precedence

Highest to lowest:

1. postfix member access, indexing, generic arguments, and calls;
2. prefix `-`, `!`, `~`, `try`, `ref`, and `spawn`;
3. `*`, `/`, `%`;
4. `+`, `-`;
5. `<<`, `>>`;
6. `<`, `<=`, `>`, `>=`;
7. `is`;
8. `==`, `!=`;
9. `&`;
10. `^`;
11. `|`;
12. `and`;
13. `or`.

- Binary operators are left-associative; prefix operators are right-associative.
- Integer `+`, `-`, `*`, unary `-`, and left shift wrap modulo the result
  width with defined two's-complement results. Folding uses the same rule at
  every arithmetic node; immutable known overflow is not a compile-time error.
- Unary `-` requires a signed integer or a floating operand. Negating a typed
  unsigned value is an error; wrapping does not make it valid.
- Integer division truncates toward zero; remainder has the dividend's sign.
  A statically known zero divisor in an evaluated expression is a compile-time
  error; an unevaluated one under a decisive constant `and`/`or` is not. A
  runtime zero divisor traps. `IntN_MIN / -1` yields `IntN_MIN` and
  `IntN_MIN % -1` yields zero.
- Floating arithmetic follows IEC 60559. `%` is integer-only. Floating NaN
  comparisons follow IEC rules.
- `~`, `&`, `^`, `|`, `<<`, and `>>` accept fixed-width integers and `Size`.
  Shift counts must be in `0..width-1`; bad constants are compile errors and
  bad runtime counts trap. Signed right shift is arithmetic; unsigned is zero-
  filling.
- `bit_cast<T>()` preserves bits between equal-width fixed integers and
  `Float32`/`Float64`. It excludes pointers, `Size`, `Rune`, and aggregates.
- Fixed-width integers support `to_le_bytes()`/`to_be_bytes()` and
  `T.from_le_bytes(array)`/`T.from_be_bytes(array)` using exact
  `Array<Byte, N>` widths.

### Equality, ordering, and truthiness

- Numeric equality and ordering use the lossless common type.
- Non-numeric operands must have identical canonical types. `Bool`, `Rune`,
  `Nil`, and `EoS` compare by value; pointers by identity; text by UTF-8 bytes;
  objects by members; ADTs by tag then active payload; unions by active member;
  Array/View/List by length then elements.
- `String` and `Strand` are different canonical types and are not comparable
  with each other, by equality or ordering.
- Functions, allocators, Files, and Dicts are not equality-comparable. An
  aggregate is comparable only when every recursively compared component is.
- Ordering exists for numeric scalars, `Rune` (by scalar value), and
  `String`/`Strand`; text ordering is unsigned-byte lexicographic with the
  shorter prefix first. Ordering is unavailable for `Bool`, `Nil`, pointers,
  functions, objects, ADTs, unions, and every collection.
- Only `false` and `nil` are falsey. Zero, NaN, empty text/collections, and all
  other values are truthy.
- Truthiness applies only to `if`/`elseif`/`while` and `!`, `and`, `or`. It is
  not a conversion to `Bool`, and it does not narrow unions.
- `!`, `and`, and `or` return `Bool`. `and`/`or` short-circuit left to right;
  both operands must still be valid expressions.

## Control flow and cleanup

```hexal
while condition do
    if stop
        break
    elseif skip
        continue
    end
end
```

- `if`/`elseif`/`else` and pre-tested `while` end with `end`. `elseif` is one
  keyword. Every loop header requires `do`; delimiter-free `while` is invalid.
- `break` and `continue` target the nearest loop.
- Branches and each loop iteration are lexical scopes. Locals may shadow outer
  values; assignments may reach accessible outer mutable bindings.
- Exact `is`/nil narrowing follows branch control flow: facts from a branch
  that terminates with `return`, `break`, or `continue` may remain on the only
  continuing path.
- A result-producing function returns only if every continuing path returns.
  A loop always counts as able to fall through, even a literal `while true`,
  because its condition may be false first or its body may `break`. `break` and
  `continue` never satisfy a function's return requirement.
- `defer expression` registers cleanup in the current lexical scope. Actions
  run in reverse registration order on fallthrough, return, break, or continue.
- A direct deferred call captures callee, receiver, and arguments when
  registered. Any other deferred expression evaluates when the scope exits.
- `errdefer` has the same capture, scope, and ordering rules, but runs only
  while exiting the function with an active `Error`; it is discarded on
  success, fallthrough, break, and continue. Mixed `defer`/`errdefer` actions
  share one reverse-registration order on an Error exit.
- `try` is invalid inside a `defer` or `errdefer` action. A cleanup action that
  produces a value has that value discarded.
- An unrecoverable trap terminates the process. Deferred and error-deferred
  actions are not guaranteed to run after it, so cleanup must not be relied on
  for trap paths.

### `for ... in`

```hexal
for value in values do ... end
for index, value in values do ... end
for key, value in dict do ... end
for index, key, value in dict do ... end
```

- Supported sources: `Array`, `View`, `List`, `String`, `Strand`, `Dict`, and
  `Stream`.
- The optional first binder is a zero-based `Size` index. Sequence/Stream
  iteration then binds a value; Dict binds key and value.
- String/Strand iterate decoded Runes and count Runes, not bytes. Dict order is
  unspecified and its index counts produced entries. Stream pulls until `eos`.
- The source and traversal boundary are captured once. Array places iterate in
  place; temporary Arrays and Strands materialize once; handles copy shallowly.
- Binders are fresh, immutable, and copied each iteration. Every binder name in
  one loop header must be distinct. A union or nullable source must be narrowed
  to one concrete iterable type first.
- Array/List element replacement is allowed during iteration. Structural List
  changes and every Dict mutation invalidate traversal; this is the
  programmer's responsibility.

## Errors

- `Error` is a protected nominal value with fixed fields:
  `file: String`, `line: Size`, `column: Size`, `header: Strand`, and
  `message: String`. All fields are readable and immutable.
- `Error.new(header, message)` is its only constructor. The compiler injects
  the construction site's source filename, one-based line, and one-based UTF-8
  byte column. Propagation never rewrites them.
- Fallible functions return ordinary structural unions such as `T | Error` or
  `Nil | Error`; there are no exceptions or hidden result channels.
- Error copying is shallow. A runtime String used as `message` must remain live
  while any Error that refers to it may be inspected or printed.
- `try expression` requires a union containing exactly one `Error` member and
  at least one success member. It evaluates once, returns Error unchanged from
  the enclosing fallible function, or yields the success value/normalized
  success union.
- `try` is valid only inside a function whose result accepts Error. It does not
  catch bounds, allocation, arithmetic, UTF-8, or cleanup traps.

## Allocation and memory lifetime

```hexal
h: Heap = Heap.new()
p: MutPtr<Int32> = h.allocate<Int32>(0)
defer h.free(p)
```

- `Heap.new()` selects the default allocator and creates no runtime allocation.
- Heap operations are thread-safe.
- `h.allocate<T>(initial)` allocates and initializes one complete finite `T`,
  returning non-owning `MutPtr<T>`. Failure or unrepresentable size traps.
- `h.free(ptr)` accepts `Ptr<T>` or `MutPtr<T>`. The allocator must match.
- Heap-backed library values take an explicit `Heap` and expose explicit
  cleanup.
- Freeing a container releases only storage the container itself owns — its
  header and backing region, however many allocations that happens to be.
  Anything else is still yours. So `List<Int32>` and
  `List<Strand>` need only `free`, because their elements are the region, while
  `List<Ptr<T>>`, `List<String>`, and any element containing a handle leave
  their referenced allocations behind for you to free first.
- This holds at every depth. The compiler never synthesizes a per-element copy
  or destroy operation, so nesting a handle inside an object or ADT does not
  make a container start cleaning it up.
- Runtime metadata may diagnose allocator mismatch or a live double-free, but
  detection after invalid lifetime use is not guaranteed.

## Collections

### Common rules

- Lengths, capacities, iteration indices, and normalized bounds use `Size`.
  Index arguments may be any integer and are checked when normalized.
- A known bad bound is a compile-time error. Dynamic bounds trap before access.
- `length()`, `is_empty()`, `at(index)`, indexing, and `slice(start, end)` use
  zero-based, end-exclusive ranges where available.
- `Array`, `View`, and `List` equality compares length then elements. Dict
  equality and ordering are unavailable.

### `Array<T, N>`

- Fixed-size inline sequence; `N` is a positive integer literal.
- `[a, b, c]` requires an expected Array type and exactly `N` elements.
  Elements evaluate left to right.
- Whole assignment, argument passing, and return copy the inline region.
- Indexing/`at` are bounds checked. Element writes require a writable Array
  place. `slice` returns `View<T>`.
- Elements follow the general storability rule, including nested Arrays and any
  union whose members all qualify. `Array<Ptr<Node> | Nil, 4>`,
  `Array<Int32 | Bool, 4>`, and `Array<String, 4>` are all valid.
- An Array whose elements refer to external state copies only those references
  when the Array is assigned, passed, or returned, and frees nothing.
  `Array<String, 4>` needs the same explicit element cleanup a `List<String>`
  does.

### `View<T>`

- Non-owning, read-only, contiguous pointer-plus-length descriptor. `T` follows
  the general storability rule with no exception, so `View<View<T>>` is valid.
  A `MutPtr` element retains its pointee capability.
- Created by `slice`, `View<T>.from_pointer(pointer, length)`, or
  `View<T>.empty()`.
- `from_pointer` accepts a statically non-null `Ptr<T>`/`MutPtr<T>`, evaluates
  pointer then length once, weakens `MutPtr`, and only builds the descriptor.
- `from_pointer` rejects a pointer the checker can trace to `ref` inside the
  current function, since that names inline storage that dies with its scope.
  Heap-allocated and opaque pointers are accepted. A pointer passed in as a
  parameter is opaque, so `wrap(ref local, 1)` is not caught.
- It allocates, copies, owns, mutates, and performs pointer arithmetic on
  nothing. The caller guarantees contiguous initialized aligned storage and a
  sufficient lifetime.
- A View may be stored like any other storable value: object and ADT members,
  union members, Arrays, Lists, Dict values, and other Views. Keeping the
  borrowed storage alive is your responsibility.
- A View cannot be rooted in a temporary Array/List or addressed with `ref`.
  Bounds checking remains active after construction. A root-level View binding
  is an ordinary local like every other root binding; there is no module data
  for it to be excluded from.
- A View may be returned when its root outlives the call: a parameter, storage
  reached through a parameter, or a `from_pointer` region. Returning a View
  rooted in a local of the returning function is rejected, because that storage
  dies at the return.

```hexal
fun payload(packet: Ptr<Packet>): View<Byte>
    return packet.bytes.slice(0, 4)   -- root is caller storage
end

fun head(): View<Int32>
    fixed: Array<Int32, 4> = [1, 2, 3, 4]
    return fixed.slice(0, 2)          -- Error: root is a local
end
```

- This is the only lifetime rule the compiler enforces for Views. It does not
  track resize invalidation or the lifetime of a `from_pointer` region.
  Copying or retaining a View is safe only while its source remains valid.

### `List<T>`

```text
List<T>.new(heap) -> List<T>
length() -> Size                 is_empty() -> Bool
at(index) -> T                   [index] -> T/place
slice(start, end) -> View<T>     push(value)
pop() -> T                       set(index, value)
clear()                          free(heap)
stream(heap) -> Stream<T>
```

- Growable heap-backed sequence. A fixed handle can mutate its referenced List;
  `mut` is needed only to reassign the handle.
- `pop` traps when empty. `at`, indexing, and `set` trap out of bounds.
- Element types follow the general storability rule, so `List<String>`,
  `List<List<Int32>>`, and `List<View<Int32>>` are all valid.
- `slice` returns `View<T>` and is available for every element type.
- Every element, `String` included, uses shallow copy and discard. `push` and
  `set` store a copy of the handle, not of the referenced allocation. `at`,
  indexing, and `pop` hand back that same handle. `set`, `clear`, and `free`
  drop slots without freeing anything they point to.
- A `String` obtained from a List is an ordinary alias, carrying neither a
  cleanup obligation nor protection from one. Free each element String yourself
  before dropping the last handle to it.
- `free` releases only the List header and backing storage.

```hexal
names: List<String> = List<String>.new(h)
defer names.free(h)   -- registered first, so it runs last

names.push(text)      -- stores the handle; text stays valid and yours

for name in names do
    name.free(h)      -- elements first
end
```

- The element loop is correct only when every element is a distinct allocation
  this code owns. Insertion stores an alias, so pushing one handle twice puts
  the same allocation in two slots and the loop double-frees it. The rule is
  per allocation, not per slot: free each distinct allocation exactly once,
  through any live alias, after which every other alias is dangling.
- Order matters only when the container is the last way to reach an element.
  Freeing it first releases the region holding the handles, so anything not
  also reachable elsewhere leaks. Nothing diagnoses that, because it is a leak
  rather than a use-after-free.
- `defer` runs in reverse registration order, so a `defer container.free(h)`
  written at construction naturally runs after any element cleanup registered
  later.

### `Dict<K, V>`

```text
Dict<K, V>.new(heap) -> Dict<K, V>
insert(key, value)                 get(key) -> V
contains(key) -> Bool              remove(key) -> V
free(heap)
```

- Heap-backed open-addressing dictionary. `K` is exactly `Int32` or `Strand`.
  `V` follows List element eligibility.
- `insert` replaces an existing value. `get` and `remove` trap on a missing key.
- Keys and values, `String` included, copy shallowly. `insert` stores a handle
  copy; `get` and `remove` hand that handle back. Replacement and `free` drop
  entries without freeing anything they reference. `free` releases only the
  bucket region and header.
- A `String` from `get` or `remove` is an ordinary alias with no cleanup
  obligation attached. Replacing an entry without first preserving or freeing
  its old value leaks that allocation.
- Hashing is compiler-internal, infallible, and defined only for `Int32` and
  `Strand`. Equal values hash equally; `Strand` hashes its logical payload and
  excludes the terminator and zero tail. The algorithm and any seed are
  implementation details, are not observable, and are not stable across compiler
  versions. There is no source-level hash operation. Iteration order is
  unspecified.

## Text

### Literals and representation

- `Byte` is exactly `UInt8`. A byte literal contains one printable ASCII byte
  or one byte escape: `\\`, `\'`, `\n`, `\r`, `\t`, `\0`, or `\xHH`.
- A Rune literal contains exactly one Unicode scalar and also accepts
  `\"` and `\u{HEX}` escapes. It is not a grapheme cluster.
- `String` is immutable UTF-8 behind a non-null pointer-sized handle. Runtime
  values use one header-plus-bytes allocation; literals use static storage.
- `Strand` is immutable, literal-only, inline, and exactly 32 bytes: at most 31
  UTF-8 payload bytes, one NUL terminator, then zero fill. Embedded NUL, invalid
  UTF-8, and longer payloads are rejected.
- String/Strand indexing and length count Runes. Byte Views count bytes.

### String surface

```text
length() -> Size                    is_empty() -> Bool
at(index) -> Rune                   [index] -> Rune
bytes() -> View<Byte>               slice(start, end) -> View<Byte>
rune_cursor() -> RuneCursor         to_string(heap) -> String
concat(heap, other) -> String       free(heap)
String.from_bytes(heap, View<Byte>) -> String
String.from_runes(heap, View<Rune>) -> String
```

- `slice` uses Rune bounds but returns the corresponding zero-copy UTF-8 bytes.
- `from_bytes` validates before allocation and traps on invalid UTF-8.
- `RuneCursor` borrows the String and provides `has_next()` and `next()`.
- `next()` advances one Rune and traps after exhaustion. Copying a RuneCursor
  copies its current position; the copies then advance independently over the
  same borrowed String storage.
- Copying a String copies its handle. Runtime-created strings require exactly
  one `free` through the matching Heap; every alias becomes invalid afterward.
  A String literal has static storage and must never be freed. "Borrowed" is no
  longer a tracked state: a handle read out of a collection is an ordinary
  alias, so freeing it frees the allocation the collection still points at.

### Strand surface

- `length()`, `is_empty()`, `at(index)`, `[index]`, and `to_string(heap)`.
- String and Strand method dispatch is separate; Strand never exposes a View
  into its inline bytes.

## Streams

- `Stream<T>` is a lazy, single-pass, single-threaded pull handle with no
  length, random access, rewind, or concurrent communication.
- `T` and producer State must be complete, finite, and shallow-copyable.
  `T` cannot itself be `EoS` or a top-level union containing it.
- `Stream<T>.new()` is allocation-free and empty.
- `Stream<T>.produce(heap, state, callback)` stores a shallow state copy and a
  named `Fun<(MutPtr<State>) : T | EoS>` callback.
- `List<T>.stream(heap)` is a non-owning source over the existing List.
- `next()` returns `T | EoS`. One call produces at most one public value; a
  filter may make several internal upstream pulls.
- `filter(heap, predicate)`, `map(heap, mapper)`, and `take(heap, count)` are
  lazy allocating adapters. Each owns its upstream by API convention.
- After successful adapter construction, every upstream alias must be treated
  as consumed: do not pull, adapt, or free it separately. Every node in a
  chain uses the same Heap.
- `for` stops at `eos` but never frees the Stream; breaking permits later pulls.
- An indexed Stream loop counts produced values from zero; rejected filter
  inputs do not increment it. Unlike finite collections, a Stream captures no
  initial traversal boundary and pulls until `eos`.
- `free(heap)` releases the adapter chain. Exhaustion alone does not. External
  resources stored in producer state remain caller-owned and must outlive it.
- A List-backed Stream captures the List length but not its elements. Keep the
  List alive and structurally unchanged until the Stream chain is freed;
  same-length element replacement is visible to later pulls.
- Aliases share one producer cursor. Operations are non-reentrant.

## Output and files

### `print`

- `print(arg, ...)` is a protected builtin, requires at least one argument,
  inserts no separator/newline, and returns no value.
- Arguments evaluate exactly once, left to right. No bytes are emitted until
  all arguments finish evaluating.
- Printable values: `Bool`, every integer, `Size`, `Byte`, the floats, `Rune`,
  `String`, `Strand`, `Nil`, `Error`, objects, ADTs, Array/View/List, and Dict
  when their contents are recursively printable. Pointers, functions, unions,
  `EoS`, allocators, Files, Streams, and resources are rejected; a union must be
  narrowed or matched first.
- Direct text/Rune is raw; nested text/Rune is quoted and escaped. Byte prints
  numerically in both contexts. The structural forms are fixed and one line:

```text
Point { x = 10, y = 20 }        object: defining name, declaration order
Direction.North                 unit variant
Shape.Circle { r = 10 }         record variant, active payload only
[10, 20, 30]                    Array, View, and List share one form; [] if empty
{"Ada": 10, "Lin": 20}          Dict; {} if empty; order unspecified
```
- `Float32` uses `%g` precision 9; `Float64` precision 17. Signed zero and fixed
  `inf`, `-inf`, `nan` spellings are preserved.
- A direct Error prints `file:line:column: header: message` with no trailing
  newline. Nested, it uses the declaration-ordered structural form with quoted
  text fields.
- A whole print call is atomic relative to other prints and standard text
  writes. It does not flush per call; normal shutdown performs the final flush.
  Root defers finish before the output gate closes; final stdout and applicable
  stderr flushes happen afterward. Detected output failure is an unrecoverable
  runtime error.

### `File`, `FileMode`, and `Stdio`

- `FileMode` has `Read` (`rb`), `Write` (`wb`), and `Append` (`ab`). Files
  opened by Hexal use binary modes on every platform.
- FileMode values copy, pass, return, compare, and match like ordinary unit-
  variant ADT values. Its variants are qualified, so an unrelated `Read` name
  remains valid.
- `File.open(path, mode) -> File | Error`; v1 paths are non-empty, non-NUL
  ASCII. Known-invalid literals fail compilation; runtime invalid paths return
  Error.
- `read_bytes(heap) -> List<Byte> | Error` reads to EOF.
- `read_text(heap) -> String | Error` reads to EOF and reports malformed UTF-8
  recoverably. This differs from trapping `String.from_bytes`.
- `write(View<Byte>) -> Nil | Error` and
  `write_text(String) -> Nil | Error` attempt the full payload. Partial external
  effects remain if an error occurs.
- `flush() -> Nil | Error` is output-only and does not promise physical-media
  durability. `close()` has no value; owned close failure traps.
- Runtime mode mismatches return Error before an incompatible C call. Every I/O
  Error uses the Strand header `"I/O Error"` with a static portable message; no
  errno or host error code is exposed.
- `Stdio.stdin()`, `.stdout()`, and `.stderr()` return borrowed text-mode Files.
  They cannot be closed. Direct invalid operations are compile-time errors;
  copied standard handles retain runtime checks.
- File handles copy shallowly and alias one C stream. Closing one invalidates
  all aliases. Containers never close stored Files.
- File and Stream are separate; File I/O is synchronous and may block a worker.

## Tasks and synchronization

### Tasks and scheduler

- `spawn named_function(args)` evaluates arguments once, left to right, shallow-
  copies them, and returns `Task<R> | Error`. Creation failure starts no task.
- `R` may be any complete shallow-copyable result type, including `Nil`, an
  object, an ADT, or a union such as `Config | Error`. `Atomic<T>` and any
  aggregate recursively containing one are excluded. `Task<R>` adds no Error
  member to `R`; the spawn Error and a returned Error are separate failures.
- `Task<R>.join() -> R` waits, copies the exact result, and reclaims storage.
  `detach()` discards the result and arranges reclamation. Exactly one successful
  join or detach is allowed across all aliases.
- Task stacks, control blocks, and scheduler queues are runtime-owned. `spawn`,
  `join`, and `detach` take no allocator; only user payloads use an explicit
  `Heap`.
- `Task.yield()` is the explicit scheduling point. Tasks are stackful fibers in
  one cooperative M:N scheduler over C23 worker threads.
- Supported runtime targets are Windows x64 and POSIX x86-64. Missing verified
  C23 `<threads.h>` support is an Unsupported Error; there is no fallback.
- The root Task is pinned to worker zero. Root return does not implicitly join
  source tasks. Task stacks reserve 1 MiB including a guard page.
- Every repeating path through task-reachable literal `while true` must visibly
  execute `Task.yield()`; otherwise compilation fails.
- Spawn, join, Mutex, Channel, and sequentially consistent Atomic operations
  establish the documented C23 synchronization edges. Unsynchronized
  conflicting access is a data race with no guaranteed behavior.

### `Channel<T>`

```text
Channel<T>.new(heap, capacity: Size) -> Channel<T> | Error
send(value) -> Nil | Error           receive() -> T | EoS
close() -> Nil                       free(heap) -> Nil
length() -> Size                     capacity() -> Size
is_closed() -> Bool
```

- Bounded MPMC FIFO. Capacity zero is a compile error when known, otherwise an
  Error. Full send and empty receive park the Task, not its worker.
- Send after close returns Error. Close is idempotent, keeps queued values, and
  wakes waiters. Closed-and-drained receive returns `eos`. `receive` has no
  recoverable Error, so `Error` is a valid element type.
- `free` requires closed, empty, unused state and releases only Channel storage.
- Elements copy shallowly. Top-level `EoS` and recursively contained `Atomic`
  are invalid element types.

### `Mutex`

- `Mutex.new(heap) -> Mutex | Error`; `lock()`, `unlock()`, `free(heap)`.
- Heap-backed, scheduler-aware, non-recursive, and owned by Task identity, not
  worker identity. Waiting parks the Task.
- Recursive lock, wrong-owner/double unlock, or freeing a locked/waited Mutex is
  programmer error; cheaply detectable live misuse traps.

### `Atomic<T>`

- Supported `T`: `Bool`, `Int32`, `UInt32`, `Int64`, `UInt64`, or `Size`.
- Inline, allocator-free, sequentially consistent operations:
  `new`, `load`, `store`, `exchange`, strong `compare_exchange`, and numeric
  `fetch_add`/`fetch_sub` (not Bool).
- `Atomic<T>` is non-copyable, and so is any value recursively containing one.
  It cannot be initialized from an existing Atomic, assigned, passed or returned
  by value, addressed with `ref`, used in ordinary arithmetic, or stored in an
  Array, View, List, Dict, Stream, Channel, or Task argument or result.
- The one exemption is direct construction: `Atomic<T>.new(value)` initializes
  fresh storage rather than copying. That storage is a binding or an object
  member and nothing else — not an ADT payload, not a union member, not a
  collection element — because every other position acquires its value by
  copying. The enclosing object is then itself non-copyable and is shared by
  pointer. Nesting follows: each level is an object member initialized in
  place.

```hexal
type Shared = { count: Atomic<Int32>, }
shared: Shared = Shared { count = Atomic<Int32>.new(0) }
pointer: Ptr<Shared> = ref shared    -- share by pointer, never by copy
```
- Lock-freedom is not guaranteed.

## Layout and volatile access

- `size_of<T>()` and `align_of<T>()` require one explicit complete finite type
  and return `Size` C constant expressions. Reference-like types report their
  source handle size. They do not make arbitrary constant expressions valid as
  Array lengths.
- `read_volatile()` is available on `Ptr<T>` and `MutPtr<T>`;
  `write_volatile(value)` requires `MutPtr<T>`.
- Volatile `T` is one of the fixed-width integers, `Byte`, or `Size`. Receiver
  and value evaluate once. Nullable pointers must first be narrowed.
- Volatile access supplies C volatile observability only: no atomicity,
  synchronization, fence, device-order, address exposure, or pointer arithmetic.

## C23 contract and diagnostics

- Generated C must preserve Hexal's semantics rather than inherit C undefined
  behavior for signed overflow, invalid shifts, division edges, bounds, union
  payloads, or unsafe numeric conversion.
- Fixed-width integer and IEC floating assumptions are checked in generated
  headers. `Size` use checks the target `size_t` width.
- Objects and ADTs lower to source-ordered structs; unions to checked tagged
  values except nullable-pointer niches; generics are monomorphized. Each object
  emits a source-ordered forward `typedef` region and then a source-ordered
  definition region, so recursive and non-recursive objects share one shape.
- Pointee qualification derives from the type chain alone: a `Ptr` layer adds
  `const` to its pointee, a `MutPtr` layer does not, and a fixed binding adds a
  trailing `const`. No qualifier-discarding cast is ever emitted.

| Hexal | C23 |
| --- | --- |
| `Ptr<Int32>` | `const int32_t *` |
| `MutPtr<Int32>` | `int32_t *` |
| `Ptr<Ptr<Int32>>` | `const int32_t *const *` |
| `MutPtr<Ptr<Int32>>` | `const int32_t **` |
| `Ptr<MutPtr<Int32>>` | `int32_t *const *` |
| `MutPtr<MutPtr<Int32>>` | `int32_t **` |
| `Ptr<Unknown>` | `const void *` |
| `MutPtr<Unknown>` | `void *` |

- Object members are unqualified whatever their member mode; only the pointer
  type contributes pointee `const`.
- Generated operations retain structured checked identities until generation;
  the checker never hands opaque source-derived C fragments to the generator.
- Syntax errors belong to lexing/parsing; type and semantic errors belong to
  the earliest checking phase that can prove them; runtime traps cover invalid
  dynamic state. An unclassifiable compiler inconsistency is an `Unknown Error`.

## Deliberately absent or pending

- Draft, not normative: `Arena`, `Pool<T>`, native `module`/`import`/`pub`, C
  header imports, C exports, and foreign ABI mapping.
- No source pointer arithmetic, pointer casts, `unsafe` block, mutable View,
  ranges, counted-loop syntax, user iterator protocol, or mutable iteration
  binders.
- No exceptions, closures, async/await, coroutines, user threads, task groups,
  `select`, unbounded/rendezvous Channels, nonblocking Channel operations, or
  memory-order arguments.
- No operator overloading, user truthiness/display/hash protocols, generic
  constraints, reflection, serialization schema, or runtime type objects.
- No compound assignment, increment/decrement, conditional operator, numeric
  suffixes, wrapping/saturating conversions, or selectable arithmetic modes.
- No ReadWrite/seek File mode, Path type, sockets, asynchronous I/O, File
  Streams, builders, or in-memory output streams.
