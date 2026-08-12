# RFC 0018: String and Rune Values

- Kind: Feature Specification (Rust-Style RFC)
- Status: Accepted (Closed); implementation coordinated with RFC 0020
- Features: UTF-8 `String`, bounded literal-only `Strand`, Unicode-scalar
  `Rune`, `Byte`, string literals, character literals, and core text operations
- Created: 2026-08-09
- Depends on: RFC 0003 (core scalar types), RFC 0006 (core object values),
  RFC 0008 (functions and methods), RFC 0016 (explicit numeric conversions),
  RFC 0019 (generic types and functions), RFC 0020 (collections), and RFC
  0024 (equality, ordering, and hashability), and RFC 0026 (allocation,
  deallocation, and deferred cleanup)
- Coordinates with: RFC 0023 (truthiness) and the future FFI specification

## Summary

RFC 0003 deliberately leaves text and character values out of the scalar
language. This RFC adds four text-oriented types:

- `Byte`, a transparent alias of `UInt8` for byte-oriented APIs;
- `Rune`, a Unicode scalar value in the range `U+0000` through `U+10FFFF`,
  excluding UTF-16 surrogate code points;
- `String`, an immutable UTF-8 text value with rune-oriented access and an
  explicit raw-byte view;
- `Strand`, an immutable, literal-only UTF-8 value with a maximum 31-byte
  payload and a 32-byte NUL-terminated representation.

```seawitch
letter: Rune = 'A'
emoji: Rune = '\u{1F600}'
message: String = "hello, world"
newline: String = "line 1\nline 2"
```

`Rune` is the language spelling for a Unicode character value. `Char` is not
introduced as a second name. A rune is a Unicode scalar value, not a grapheme
cluster: a user-visible character may consist of multiple runes.

## Goals

1. Make ordinary UTF-8 text readable and type-safe.
2. Keep byte data distinct in source while retaining `UInt8` compatibility.
3. Make invalid Unicode scalar values unrepresentable through literals and
   checked construction.
4. Define equality and basic length behavior without choosing a locale.
5. Keep C string interoperability explicit and separate.
6. Make ordinary text rune-oriented while exposing exact UTF-8 code units
   through an explicit `View<Byte>`.
7. Provide a bounded, allocation-free key type for fixed-size dictionary keys.

## Non-goals

This RFC does not define:

- locale-sensitive collation, case conversion, or normalization;
- grapheme-cluster segmentation;
- regular expressions or text formatting;
- mutable string builders;
- in-place mutation of `String` or `Strand` values;
- runtime construction of `Strand` values;
- C string importing or exporting;
- general-purpose ownership transfer for arbitrary `String` bindings;
- implicit conversion between `String`, `View<Byte>`, and raw pointers.

## Types

### `Byte`

`Byte` is a transparent alias of `UInt8`. It adds a source-level name for
raw bytes and UTF-8 code units and does not introduce a new representation,
conversion, or C type:

```seawitch
byte: Byte = 0xFF
number: UInt8 = byte
```

The alias is usable in generic and collection type arguments. `Byte` is a
character/code-unit type in source APIs, but it is not a Unicode character and
does not claim to be valid UTF-8 until it crosses an API that validates an
encoding boundary. Because it is an alias, `Byte` and `UInt8` have identical
type identity, assignment rules, numeric operators, conversions, and
truthiness behavior. `Byte` is therefore a source-level role name, not a
distinct type-safety boundary.

RFC 0003's experimental byte literal behavior was removed. This RFC
reintroduces `Byte` and a new `b'...'` literal as text/binary facilities; it does
not reopen the closed RFC.

### `Byte` literals

A byte literal contains exactly one byte:

```seawitch
ascii: Byte = b'A'
newline: Byte = b'\n'
raw: Byte = b'\xFF'
```

Direct characters in a byte literal must be one-byte ASCII characters. The
literal accepts `\\`, `\'`, `\n`, `\r`, `\t`, `\0`, and `\xNN`, where `NN` is
exactly two hexadecimal digits. Unicode `\u{...}` escapes are not accepted in
byte literals. A non-ASCII source character such as `b'Ã©'` is rejected rather
than silently encoding to multiple bytes.

### `Rune`

`Rune` is a built-in scalar type with a 32-bit unsigned representation and a
Unicode scalar invariant:

```text
0 <= rune <= 0x10FFFF
rune is not in 0xD800..0xDFFF
```

`Rune` is distinct from `UInt32`; equal representation does not make the two
types interchangeable. A character literal directly creates a valid `Rune`.
An integer-to-`Rune` conversion must use the checked conversion rules of RFC
0016 and rejects a value outside the scalar range. Wrapping and saturating
integer conversion to `Rune` are rejected because neither operation preserves
the Unicode invariant.

`Rune` has scalar equality and ordering according to RFC 0024. It is not a
numeric operand for arithmetic; numeric conversion must be explicit and use
RFC 0016's checked conversion rules. RFC 0018 extends that conversion form for
`Rune`: an integer converted with `as Rune` must be a valid Unicode scalar,
and `Rune as T` is a checked conversion to an integer type `T`. Wrapping and
saturating modes are rejected for either direction. Its truthiness is owned by
RFC 0023 and does not represent numeric zero/nonzero conversion.

### `String`

`String` is the single built-in immutable text type. It always contains one
valid UTF-8 byte sequence and is rune-oriented for ordinary access:

```seawitch
text: String = "café"
first: Rune = text[0]
bytes: View<Byte> = text.bytes()
last: Rune = text[3]
```

`String` has no type parameter or public byte/rune specializations. Its
physical representation is always UTF-8; Unicode scalar values are decoded
when rune indexing or iteration is requested.

The initial design deliberately does not store `String` as a contiguous `Rune`
array. That representation is possible, but it would replace the single
Go-style UTF-8 layout with a second physical representation:

```c
// Alternative design, not used by this RFC:
struct sw_string_runes { const uint32_t *data; uint64_t rune_length; };
```

The rune-backed representation would use more memory for common ASCII text,
require encoding before byte-oriented or C interoperability, and make the
canonical storage no longer match the UTF-8 representation used by files and
protocols. UTF-8 is therefore the single source of truth. A future cached rune
index or decoded representation may be considered separately, but it must not
change the observable UTF-8 storage and byte-view guarantees.

The empty string is valid and is different from a null pointer or absent
value. `String` is not a `List<Rune>` and is not implicitly convertible to one.

The proposed abstract representation is:

```c
typedef struct {
    const uint8_t *data;
    uint64_t byte_length;
} sw_string;
```

`sw_string` is a two-field, by-value header containing a pointer and a byte
length, matching the layout model used by Go strings. The header does not own
an inline byte buffer. `data` always points to one contiguous sequence of
immutable UTF-8 bytes, and `byte_length` is the byte length of that sequence;
it is not computed from a terminating NUL. The empty string uses a valid
non-null zero-length storage location, normally the generated empty literal
storage; `data` is never a null sentinel for a valid `String`.

String literals point to compiler-generated static read-only storage. Every
runtime operation that produces an owning `String` receives an explicit
allocator. The resulting `String` owns one contiguous immutable heap-backed
allocation, and that allocation must be freed through `text.free(h)` using the
same allocator identity.
Heap-backed immutable storage is not C `.rodata`; the language exposes it only
through `const uint8_t *` access.

Owning constructors copy their input into the result's contiguous storage.
They do not retain a caller's `View<Byte>` or `View<Rune>` backing storage.
Allocation failure, or a requested byte length that cannot be represented by the
target allocation size, produces a defined runtime allocation trap.

The header's physical placement is not a language guarantee. A local header
will normally be an automatic C value. Non-owning headers may be copied into
other non-owning storage, but an owning header may not be shallow-copied into a
second owner; it must be transferred or copied through the explicit operation
for that context. The backing bytes remain valid independently of the header's
physical placement.

An owning runtime-created `String` is freed explicitly:

```seawitch
text: String = String.from_bytes(h, bytes)
defer text.free(h)
```

String literals and borrowed slices do not own their backing bytes and must not
be passed to `free`.

The checker tracks String storage provenance even though `String` remains one
source-level type. A literal or a `slice` result is static or borrowed and has
no cleanup obligation. `from_bytes`, `from_runes`, `to_string`, `concat`, and
the collection element-copy operation produce an owning String and therefore
produce one cleanup obligation. An owning header must never be shallow-copied
into a second owner. General assignment, function-parameter, and return
transfer for arbitrary String bindings remain outside this RFC; collection
operations use the explicit copy and move rules in RFC 0020.

`String` is immutable. For collection storage, the compiler provides a
type-owned copy operation that copies the logical UTF-8 bytes into the target
collection's allocator, and a matching `free` operation for those bytes. A
collection may therefore store `String` values without sharing mutable
storage. A collection `get`/`at` produces a new owning String copy; a collection
`pop`/`remove` transfers the stored String payload to the returned value. The
containing collection destroys any remaining String payloads exactly once.
`Strand` is immutable and needs no heap cleanup. Any future operation
that changes text must produce a distinct new value and leave its source
unchanged; the exact text-transformation API is outside this RFC.

`String.bytes()` returns a zero-copy read-only `View<Byte>` over the exact
UTF-8 bytes. `String` is inherently rune-oriented, so direct indexing and
iteration provide rune access without a separate rune view or handle. A
borrowed string handle produced by `slice` cannot outlive its source storage.

`RuneCursor` is a built-in borrowed helper value produced by
`String.rune_cursor()`. It contains a source-bounded byte position and decodes
one `Rune` at a time. It is not a `View<Rune>` and does not own or materialize
the source text.

### `Strand`

`Strand` is a distinct immutable value for short, literal-only UTF-8 text. Its
logical payload is at most 31 UTF-8 bytes. Its fixed representation is exactly
32 bytes: the first 31 bytes contain the payload and the final byte is always
an implicit terminating `\0`.

```c
typedef struct {
    uint8_t data[32];
} sw_strand;
```

`sw_strand` has no separate header and no pointer to backing storage; the 32
bytes are the complete value. It never performs a separate heap allocation for
its payload. A local `Strand` therefore has inline storage in the local value,
but a `Strand` stored in an object, array, or `Dict<Strand, V>` is inline in
that containing value and lives wherever the containing value lives. The
guarantee is no separate backing allocation, not literal stack placement.

The storage invariant is:

```text
data[0..payload_length)   = the UTF-8 payload
data[payload_length]      = 0
data[payload_length..32)  = 0
```

The first zero byte determines the logical payload length. All unused bytes
after that first zero are also zero so copies have deterministic contents.
The terminator and zero-filled tail are representation-only; they are not
included in logical length, equality, hashing, or the typed text operations.

`Strand` values can only be created from a string literal with a `Strand`
destination type:

```seawitch
name: Strand = "Seawitch"
label: Strand = "café"
empty: Strand = ""
```

The compiler validates the literal as UTF-8 and rejects it when its encoded
payload exceeds 31 bytes. Explicit NUL characters, including `\0`, are
rejected because `Strand` is NUL-terminated. Interpolation is not permitted in
a `Strand` literal; any future interpolated text expression produces a
`String`, never a `Strand`.

`Strand` has no constructor from `String`, `View<Byte>`, `View<Rune>`, or any
other runtime value. Concatenation, formatting, and mutation never produce a
`Strand`. Copying, passing, returning, and storing an existing `Strand` value
are permitted.

`Strand` provides bounded, rune-oriented read-only access:

```seawitch
text: String = name.to_string(allocator)
```

`Strand` provides `length()`, `is_empty()`, `at(index)`, and `[index]`.
Indexing and `at` return one `Rune` and use the same bounds-trap semantics as
`String`. `Strand` does not provide `bytes()` or `slice()`: its payload is inline
in the value, so returning a pointer into a copied or moved `Strand` could
outlive the storage it references. Use `to_string(allocator)` when a borrowed
or independently owned text value is required.

`Strand.length()` returns `UInt64` and counts Unicode scalar values.
`Strand.is_empty()` returns `Bool` and is true exactly when the logical payload
has zero UTF-8 bytes.

`Strand.to_string(allocator)` copies the logical payload into a new owning
`String` using the supplied allocator. Both `String` and `Strand` remain
immutable; text transformations must return distinct values.

### Typed text access

`String` provides rune-oriented read-only indexing:

```seawitch
text: String = "café"
first: Rune = text[0]
part: String = text.slice(0, 3)
bytes: View<Byte> = text.bytes()
first_byte: Byte = bytes[0]
byte_part: View<Byte> = bytes.slice(0, 3)
```

`String.length()` returns `UInt64`, counts Unicode scalar values, and may scan
the UTF-8 bytes. Its worst-case work is linear in the stored byte length.
`String.bytes().length()` counts UTF-8 code units and is the stored byte length.
Neither operation is promised to be constant time for rune counts.

`String[index]` and `String.at(index)` decode and return one `Rune`; they never
return a partial UTF-8 sequence. Both forms trap on an out-of-range index and
use RFC 0020's integer-index rules. `String` has no direct byte index operation;
use `String.bytes()[index]` for a `Byte`. Because the backing representation is
UTF-8, rune indexing is not generally constant-time: the worst-case work is
linear in the number of bytes before the requested rune.

`String.slice(start, end)` uses a zero-based, end-exclusive rune range and
returns a zero-copy source-tied `String` handle. It traps unless
`0 <= start <= end <= length()`. The implementation finds the corresponding
UTF-8 byte boundaries before creating the handle and may scan linearly.

`View<Byte>.slice(start, end)` uses a zero-based, end-exclusive byte range and
returns a non-owning `View<Byte>`. The resulting view may contain an incomplete
or invalid UTF-8 sequence; use `String.from_bytes` for explicit validation.

`String` handles do not allocate when created by `slice`. They remain immutable
and cannot be used as mutation targets. A borrowed handle cannot outlive the
source storage from which it was obtained.

`String.rune_cursor()` returns a borrowed cursor for linear rune traversal. The
cursor is source-bounded, does not allocate, and does not materialize a
`List<Rune>`:

```seawitch
cursor: RuneCursor = text.rune_cursor()
while cursor.has_next()
    rune: Rune = cursor.next()
end
```

`RuneCursor.has_next()` returns `Bool`. `RuneCursor.next()` decodes exactly one
Unicode scalar, advances to the next UTF-8 boundary, and traps if no rune
remains. A future `for` form may lower to this cursor without changing String
semantics.

## Literals

### Character literals

A character literal contains exactly one Unicode scalar value:

```seawitch
ascii: Rune = 'A'
accent: Rune = 'Ã©'
newline: Rune = '\n'
smile: Rune = '\u{1F600}'
```

The following are errors:

```seawitch
empty: Rune = ''              // Error: expected one Unicode scalar
many: Rune = 'ab'             // Error: character literal has multiple scalars
surrogate: Rune = '\u{D800}'  // Error: surrogate is not a Unicode scalar
```

The initial escape set is `\\`, `\'`, `\"`, `\n`, `\r`, `\t`, `\0`, and
`\u{1..6 hexadecimal digits}`. The Unicode escape must name one valid scalar
value. Numeric byte escapes are not accepted in character literals.

### String literals

A string literal contains zero or more Unicode scalar values and is encoded as
UTF-8:

```seawitch
empty: String = ""
text: String = "Seawitch"
unicode: String = "café \u{1F600}"
```

The same escapes as character literals are accepted, except that a string may
contain any number of escaped or direct scalars. A raw newline, an unescaped
double quote, or an invalid escape is a syntax error. The compiler validates
the literal before generation and emits exact UTF-8 bytes.

String literals are immutable and never lower as writable C arrays.

## Literal grammar and contextual typing

The text literal forms are lexical forms, not ordinary identifier or operator
expressions:

```ebnf
byte-literal   = "b" , "'" , byte-literal-body , "'" ;
rune-literal   = "'" , rune-literal-body , "'" ;
string-literal = '"' , { string-literal-body } , '"' ;
byte-literal-body = byte-source-character | byte-escape ;
rune-literal-body = rune-source-character | rune-escape ;
string-literal-body = string-source-character | string-escape ;
byte-escape = "\\" , ( "\\" | "'" | "n" | "r" | "t" | "0"
                         | "x" , hex-digit , hex-digit ) ;
rune-escape = "\\" , ( "\\" | "'" | '"' | "n" | "r" | "t" | "0"
                         | "u{" , hex-digit , { hex-digit } , "}" ) ;
string-escape = "\\" , ( "\\" | "'" | '"' | "n" | "r" | "t" | "0"
                           | "u{" , hex-digit , { hex-digit } , "}" ) ;
```

`byte-source-character` is one printable ASCII scalar other than `'` or
`\\`. `rune-source-character` is one Unicode scalar other than `'`, `\\`, or
a line terminator. `string-source-character` is one Unicode scalar other than
`"`, `\\`, or a line terminator. The semantic rules still require a byte
literal to decode to one byte and a rune literal to decode to one scalar.

The `b` in a byte literal must be immediately followed by the opening quote;
`b 'A'` is not a byte literal. A byte literal decodes to exactly one byte. A
rune literal decodes to exactly one Unicode scalar. A string literal decodes
to zero or more scalars and then encodes them as UTF-8.

String and rune literal decoding uses the UTF-8 validity rules of RFC 3629:
overlong encodings, surrogate encodings, truncated sequences, invalid
continuation bytes, and code points above `U+10FFFF` are rejected. A literal's
payload length is measured after escape decoding and UTF-8 encoding.

`Byte` and `Rune` literals have their respective intrinsic types. A string
literal with no usable expected type has type `String`; an expected `Strand`
type checks the same literal against the 31-byte payload and NUL restrictions.
Interpolation syntax is not defined by this RFC; when a future interpolation
form exists, its result type is `String` and it cannot satisfy a `Strand`
destination.

## Core operations

### Equality

RFC 0024 owns operator eligibility and cross-cutting comparison rules. This
RFC defines the value semantics used by those operators:

- `String == String` and `String != String` compare exact UTF-8 byte sequences;
- `Strand == Strand` and `Strand != Strand` compare exact logical payload bytes,
  excluding the terminating NUL;
- `Rune == Rune` and `Rune != Rune` compare scalar values;
- `String` uses lexicographic UTF-8 byte ordering; and
- `Strand` uses lexicographic logical-payload-byte ordering.

All text comparison is independent of Unicode normalization and locale. Two
canonically equivalent but differently encoded sequences may be unequal.
`String` and `Strand` do not compare directly.

Rune arithmetic remains rejected without an explicit numeric conversion.

`String.is_empty()` returns true exactly when `byte_length == 0`. Empty
`String` and `Strand` values are still truthy under RFC 0023; only `false` and
`nil` are falsey.

### Decoding and encoding

The following core operations are proposed:

```seawitch
first: Rune = message[0]
part: String = message.slice(0, 5)
encoded: String = part.to_string(allocator)
```

`String.at` and `String.slice` operate in rune units and therefore never split
a UTF-8 sequence. They trap on out-of-range indices. A later result/error
specification may add recoverable forms. `String.to_string(allocator)`
materializes an owning `String` copy using the supplied allocator; it is the
explicit way to detach a borrowed handle from its source.

`String.from_runes(allocator, values: View<Rune>)` is a built-in type-qualified
constructor form, not an ordinary RFC 0008 method call. It validates every rune
and encodes it as UTF-8, returning an owning `String` whose storage uses the
supplied allocator.

`String.from_bytes(allocator, bytes: View<Byte>)` is the corresponding built-in
type-qualified constructor. It validates the complete byte sequence as UTF-8
and returns a new owning `String` whose storage uses the supplied allocator.
Invalid input produces a defined runtime UTF-8 trap; it is not silently repaired
or truncated. No method accepts arbitrary bytes as text without crossing this
validation boundary.

There is no `Strand.from_bytes`, `Strand.from_runes`, or other runtime
constructor. A `Strand` is created only by a valid string literal.

Concatenation is provided by a named operation rather than overloading RFC
0009's arithmetic `+`:

```seawitch
joined: String = left.concat(allocator, right)
```

`String.concat` is called as `left.concat(allocator, right)`. It accepts another
`String` and returns a new owning `String`. It copies both logical byte
sequences into one contiguous backing allocation owned by the result and does
not mutate either operand. An allocation failure produces the same defined
runtime allocation trap as the other runtime String constructors.

## Byte buffers and typed text handles

RFC 0020 provides `View<Byte>` as a non-owning read-only contiguous view for
arbitrary byte storage. This RFC defines the typed text operations but not the
final ownership and lifetime contract:

- `View<Rune>` is valid for actual contiguous rune storage, such as an
  `Array<Rune, N>` or `List<Rune>` backing region;
- `String` does not produce a `View<Rune>` because its runes are decoded from
  UTF-8 rather than stored contiguously;
- `String.bytes()` returns a zero-copy `View<Byte>`;
- `String` and `Strand` cannot be mutated through a view or operation; changing
  text requires producing a distinct new value;
- constructing `String` from bytes validates UTF-8 before producing an owning
  value;
- constructing `String` from runes validates scalar values before encoding;
- no borrowed `String` handle or `View<T>` can outlive its source;

There is no implicit `String` to `View<Byte>` or `List<Byte>` conversion and no
implicit C pointer conversion. A `View<Byte>` remains the correct type for
an arbitrary byte range that may not be valid UTF-8.

## Type checking and diagnostics

The checker owns literal decoding, Unicode scalar validation, string operation
result types, and the distinction between `Byte`, `Rune`, `String`, and
`Strand`.
Representative diagnostics are:

```text
[Syntax Error] invalid escape in string literal
[Syntax Error] invalid byte literal
[Type Error] Strand literal payload exceeds 31 UTF-8 bytes
[Type Error] Strand literal cannot contain NUL
[Type Error] Strand literal cannot contain interpolation
[Syntax Error] character literal must contain exactly one Unicode scalar
[Type Error] byte literal must contain exactly one byte
[Type Error] Unicode surrogate is not a valid Rune
[Type Error] String and Strand are not directly comparable
[Type Error] String cannot be used as a numeric operand
```

Runtime traps are distinct from compile-time diagnostics:

```text
[Runtime Error] byte sequence is not valid UTF-8
[Runtime Error] String allocation failed
```

The generator must reject an unsupported text node as an `Unknown Error`; it
must not emit an unchecked C string operation or silently omit the expression.

## C23 lowering

- `Byte` lowers exactly as `UInt8`.
- `Rune` lowers to `uint32_t` with checker-enforced scalar validity.
- `String` lowers to the two-field `sw_string` header. Its data pointer refers
  either to generated static read-only literal storage or to one contiguous
  immutable heap-backed allocation.
- `Strand` lowers to an inline value containing exactly 32 bytes: up to 31
  checked UTF-8 payload bytes followed by a generated NUL byte. It has no
  separate backing allocation.
- `String.bytes()` lowers to a read-only pointer-plus-count `View<Byte>`.
- `String.rune_cursor()` lowers to a borrowed cursor containing a read-only
  byte pointer, byte length, and current byte offset. It does not allocate or
  own the backing storage.
- String literals emit generated static UTF-8 storage and a length that does
  not include an implicit terminating NUL.
- C NUL bytes may occur inside a `String` only when represented by the escaped
  scalar `\0`; C string APIs must not be used to infer its length.
- Text methods lower to generated helpers or explicit loops that operate on
  lengths, never on NUL termination except when finding a `Strand` payload
  length from its guaranteed zero-filled representation.

Generated C must not expose a writable pointer to literal storage. Foreign C
string ABI rules are deferred to the FFI specification.

## Interaction with generics and collections

`String`, `Strand`, and `Rune` are valid generic type arguments. `String` and
`Strand` equality are available as built-in operations for collection
operations whose concrete specialization requires equality. Both text types
support hashing under RFC 0024's equality/hash consistency rule; their hash
algorithm and seed are not a stable public contract.

RFC 0020 currently permits only `Int32` and `Strand` as dictionary key types.
`Strand` is the text key type for short, immutable, literal-defined text:

```seawitch
mut labels: Dict<Strand, Int32>
```

Dictionary lookup compares and hashes the logical payload bytes, excluding the
terminating NUL. A `Dict<Strand, V>` never converts keys to `String` or allocates
an unbounded text representation merely to perform lookup.

Dictionary operations are typed by `K`. A `String` expression cannot be passed
to a `Dict<Strand, V>` operation, and `String` is not a valid dictionary key
type in the current version. Runtime-created text keys therefore cannot be
used with `Dict` yet; a future revision may add `String` after its ownership
and hashing costs are specified.

Collections may use `Byte` and `Rune`, but they do not gain implicit encoding or
decoding behavior. A `List<Rune>` is a sequence of scalar values and need not
be normalized or encoded. `String` is a UTF-8-backed text value, not an alias
for `View<Rune>` or `List<Rune>`.

## Cross-spec ownership and required coordination

- This RFC owns text literal decoding, UTF-8 validity, the single `String`
  representation, rune-oriented access, byte views, and `Strand` construction
  restrictions.
- RFC 0016 owns the syntax of explicit numeric conversion; this RFC owns the
  Unicode-scalar validity checks for conversion to and from `Rune`.
- RFC 0020 owns `View<Byte>` and `View<Rune>` indexing, bounds, and the lifetime
  contract for arbitrary contiguous views.
- RFC 0023 owns truthiness. Empty `String` and `Strand` values remain truthy.
- RFC 0024 owns equality, ordering, and hashability. It must include `Strand`
  as equality-comparable, bytewise-orderable, and hashable, and must retain
  exact text-family checking for `String` versus `Strand`.
- RFC 0008's type-qualified method restriction does not apply to the two
  built-in `String.from_bytes` and `String.from_runes` constructor forms; this
  RFC defines those forms as compiler-owned intrinsics rather than ordinary
  user-declared methods. The allocator is their first explicit argument.
- RFC 0026 owns allocator identity and explicit cleanup. This RFC owns String
  storage provenance and the compiler-defined String copy, move, and `free`
  operations used by collections, while borrowed views/handles remain
  source-bounded.

## Deferred

- General allocator interfaces, general String assignment transfer, and string
  capacity.
- `StringBuilder` or mutable text buffers.
- Grapheme clusters, normalization, locale, collation, case mapping, and
  Unicode version policy.
- Formatting and interpolation syntax.
- Regular expressions and text search APIs.
- C strings, `char8_t`, wide characters, and foreign text contracts.
- Exact hash behavior and text ordering.
- Recoverable indexing and decoding errors.
- `for` syntax and general iterator protocols beyond `String.rune_cursor()`.
- General fixed-capacity strings and runtime `Strand` construction.

## Implementation phases

The feature is intentionally implemented in gated phases:

1. **Scalar and literal core.** Implement `Byte`, `Rune`, byte/rune/string
   literals, Unicode validation, and literal-only `Strand`. `Strand` includes
   inline storage, equality, ordering, hashing, length, emptiness, indexing,
   and `at`; it has no borrowed byte view or slice operation.
2. **Static and borrowed String access.** Implement static String literals,
   rune indexing, `at`, `length`, `is_empty`, `slice`, `bytes`, and
   `rune_cursor` after RFCs 0019 and 0020 and the ownership rules for borrowed
   views and handles are available. Rune indexing remains a rune operation even
   though its worst-case complexity is linear in the preceding UTF-8 bytes.
3. **Owning String operations.** Implement `to_string(allocator)`,
   `String.from_bytes`, `String.from_runes`, and `concat` using the allocator
   provenance and explicit cleanup rules in RFC 0026. These operations must
   produce result-owned immutable storage. Collection element copy, move, and
   destruction use the specialized rules in RFC 0020.
4. **Collection integration.** Implement `Dict<Strand, V>` and other generic
   collection uses after RFCs 0019, 0020, and 0024 provide specialization,
   view, equality, and hashing behavior. The implementation handoff must also
   remove the obsolete `Byte` and `b'...'` migration diagnostics from the
   current status surface when those forms are enabled.

## Acceptance criteria

Implementation is complete when focused end-to-end tests prove that:

1. `Byte`, `Rune`, `String`, and `Strand` resolve with the specified type
   identities;
2. valid `Rune`, `Byte`, `String`, and `Strand` literals decode with the
   specified rules;
3. invalid escapes, invalid byte literals, surrogate values, malformed Unicode
   escapes, empty or multi-scalar character literals, and invalid UTF-8 fail
   with focused syntax, type, or runtime errors at the owning phase;
4. the literal grammar distinguishes byte, rune, and string literals and
   applies expected-type checking for `String` versus `Strand`;
5. `String`, `Strand`, and `Rune` equality and ordering have the specified exact
   semantics;
6. `Strand` literals reject payloads over 31 UTF-8 bytes, embedded NULs, and
   interpolation, while accepting the empty literal;
7. `String.bytes()` exposes exact UTF-8 code units as a zero-copy
   `View<Byte>`, while String and Strand indexing and `at()` expose decoded
   Unicode scalars;
8. rune indexing and String slicing never split a UTF-8 sequence, byte views
   use explicit bounds without implying UTF-8 validity, and
   `String.rune_cursor()` traverses the same UTF-8 source linearly;
9. `String.to_string(allocator)` produces valid immutable UTF-8 from a borrowed
   handle using the supplied allocator;
10. `String` and `Strand` remain immutable; text-changing operations produce
    distinct values and never writable string or literal storage;
11. `String.from_bytes`, `String.from_runes`, and `String.concat` receive an
    explicit allocator, copy or encode into result-owned contiguous backing
    storage, and report invalid UTF-8 and allocation failures through defined
    runtime traps;
12. every `Strand` value has a terminating NUL and zero-filled tail, with no
    separate payload allocation;
13. `Byte` has no new runtime representation;
14. `Dict<Strand, V>` supports exact payload equality and hashing without
    converting keys to `String`;
15. `String` uses a two-field header with one contiguous UTF-8 backing byte
    sequence, while `Strand` stores its complete 32-byte value inline without a
    separate payload allocation;
16. no implicit text, numeric, pointer, or collection conversion is introduced;
17. generated C uses explicit length-based operations; and
18. every new token, syntax node, checked text operation, and generator case is
    handled explicitly under the fail-closed architecture.
