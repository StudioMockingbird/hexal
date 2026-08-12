# RFC 0044: String, Strand, Byte, and Rune Conformance Cleanup

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Features: `Byte` restoration, text literal completion, UTF-8 validation,
  inline Strand layout, and completion of the existing String surface
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0016 (numeric conversions), RFC 0018 (String, Strand, Byte,
  and Rune), RFC 0020 (String layout, Array, and View), RFC 0028 (`for`
  iteration), RFC 0035 (C-style lifetimes), and RFC 0036 (`Size`)
- Coordinates with: RFC 0030 (`print`), RFC 0039 (C interop), RFC 0040 (I/O),
  RFC 0042 (low-level operations), and RFC 0043 (pointer-plus-length Views)
- Supersedes on implementation: current compiler behavior and canonical
  documentation that remove `Byte`, reject byte literals, expose
  `View<UInt8>` in byte-oriented APIs, omit required text operations, or use a
  pointer-and-length Strand representation

## Summary

This RFC makes the implementation conform to the closed text decisions in RFC
0018 and the later revisions in RFC 0020, RFC 0035, and RFC 0036.

It does not redesign text. The settled model is:

- `Byte` is a transparent source alias of `UInt8`;
- `Rune` is a distinct Unicode scalar type;
- String is immutable UTF-8 behind one pointer-sized source handle;
- Strand is an immutable, literal-only, inline 32-byte value;
- byte access uses read-only `View<Byte>`;
- rune access decodes UTF-8 without storing a second Rune array; and
- runtime String allocations are copied and freed explicitly under C-style
  manual lifetime rules.

## Why cleanup is required

The current implementation contains a useful subset of String support, but it
does not conform fully:

1. `Byte` and `b'...'` literals are rejected even though RFC 0018 defines them.
2. String byte APIs and diagnostics use `UInt8` instead of the source role
   `Byte`.
3. `String.from_bytes` accepts any View element type and copies without UTF-8
   validation.
4. Rune literals, `String.from_runes`, and `String.rune_cursor` are incomplete
   or absent.
5. Strand lowers as a pointer and length instead of one inline 32-byte value.
6. String and Strand share checker dispatch that can expose methods on the
   wrong receiver type.
7. literal escape and Unicode validation is incomplete.
8. generated text helpers still contain `UInt64` length remnants after RFC
   0036 made `Size` authoritative.

This RFC fixes those mismatches without adding a second text representation,
mutable text, automatic cleanup, or a new error model.

## Authoritative predecessor rules

Where the closed and later RFCs differ, the later implemented decision wins:

- RFC 0020 supersedes RFC 0018's earlier by-value String header. A source
  String is a pointer-sized handle to immutable storage.
- RFC 0020 supersedes RFC 0018's earlier String-returning slice. String slice
  returns a rune-bounded `View<Byte>`.
- RFC 0035 supersedes affine String ownership and compiler borrow tracking.
  Handles copy shallowly and the programmer manages lifetime as in C.
- RFC 0036 supersedes `UInt64` text lengths and indices with `Size`.

All other RFC 0018 text and literal decisions remain authoritative.

## `Byte`

### Identity

`Byte` is a built-in transparent alias of `UInt8`:

```text
Byte == UInt8
size_of<Byte>() == size_of<UInt8>()
align_of<Byte>() == align_of<UInt8>()
```

It introduces no runtime tag, conversion, generated C type, or distinct
generic specialization:

```text
View<Byte> == View<UInt8>
List<Byte> == List<UInt8>
Array<Byte, N> == Array<UInt8, N>
```

Assignments are ordinary same-type assignments:

```seawitch
byte: Byte = b'A'
number: UInt8 = byte
again: Byte = number
```

`Byte` is the preferred source spelling for raw storage, encoded text,
serialization, and byte I/O. `UInt8` remains the normal spelling for an
explicitly numeric unsigned 8-bit value.

The compiler canonicalizes both names to its existing UInt8 identity. Generic
monomorphization emits only one specialization.

### Formatting

Because Byte and UInt8 are one type, Byte uses numeric formatting:

```seawitch
print(b'A') // 65
print('A')  // A
```

Printing a Byte as text requires an explicit text or I/O operation. Formatting
does not depend on whether the source spelling was `Byte` or `UInt8`.

## `Rune`

Rune remains a distinct unsigned 32-bit scalar type with this invariant:

```text
0 <= value <= 0x10FFFF
value is not in 0xD800..0xDFFF
```

Rune is not canonically equal to UInt32. Conversion between Rune and integers
uses RFC 0016's checked `to<T>()` operation. Arithmetic and bitwise operations
remain unavailable on Rune.

Every operation that creates a Rune must preserve the Unicode scalar
invariant. UTF-8 decoding never returns a surrogate, an overlong encoding, an
invalid continuation sequence, or a value above `U+10FFFF`.

## Literal grammar and decoding

The implementation must support the exact lexical forms fixed by RFC 0018:

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

A Byte literal contains exactly one byte. A direct source character must be
printable one-byte ASCII:

```seawitch
ascii: Byte = b'A'
newline: Byte = b'\n'
raw: Byte = b'\xFF'
zero: Byte = b'\0'
```

These are rejected:

```seawitch
bad: Byte = b'é'       // More than one UTF-8 byte.
bad: Byte = b'ab'      // More than one byte.
bad: Byte = b'\u{41}' // Unicode escapes are not Byte escapes.
```

A Rune literal contains exactly one Unicode scalar:

```seawitch
letter: Rune = 'é'
emoji: Rune = '🦀'
newline: Rune = '\n'
escaped: Rune = '\u{1F980}'
```

A String literal contains zero or more Unicode scalars and stores their UTF-8
encoding. String literals may contain an escaped NUL. Raw newlines and invalid
escapes are rejected.

The compiler validates literals before C generation. It rejects malformed
UTF-8, overlong encodings, surrogates, truncated sequences, invalid
continuation bytes, and values above `U+10FFFF`.

## String representation

RFC 0020's representation remains unchanged. A source String is one
pointer-sized handle:

```c
typedef struct sw_string {
    const uint8_t *data;
    size_t byte_length;
} sw_string;

const sw_string *text;
```

### Runtime-created String

A runtime-created String uses one combined allocation containing its public
header followed immediately by UTF-8 bytes and one trailing NUL:

```c
typedef struct sw_string_storage {
    sw_string header;
    uint8_t bytes[];
} sw_string_storage;
```

`header.data` points to `bytes`. `byte_length` excludes the trailing NUL and is
authoritative. Embedded NUL is valid String content. The trailing NUL exists
for representation convenience and does not define logical length.

The complete object becomes immutable after construction. The allocator's
existing hidden metadata records the allocator needed by explicit `free`.

### String literal

A String literal performs no runtime allocation. Generated C emits one
read-only fixed-size static aggregate containing the header, UTF-8 bytes, and
trailing NUL. Its layout follows the same header-then-data shape as a runtime
String, without using a flexible array member.

The empty literal may reuse one shared static empty object. Literal storage is
never writable.

### Copying and cleanup

RFC 0035 is authoritative. Copying a String copies its pointer-sized handle;
it does not copy the allocation:

```seawitch
first: String = String.from_bytes(h, bytes)
alias: String = first
defer first.free(h)
```

After one alias frees the allocation, every other alias is dangling. The
programmer is responsible for one valid cleanup and for not freeing literals,
borrowed storage, or the same allocation twice. This RFC adds no owner state,
move state, borrow checker, retain count, or automatic destructor.

Operations documented as constructing a new String allocate and copy. Ordinary
assignment, argument passing, and return remain shallow C-style handle copies.

## Strand representation

Strand is a distinct immutable, literal-only value containing exactly 32
inline bytes:

```c
typedef struct sw_strand {
    uint8_t data[32];
} sw_strand;
```

There is no pointer, length header, or separate payload allocation. Copying a
Strand copies all 32 bytes. A Strand stored in an object, Array, or Dict is
inline in that containing value.

The payload may contain at most 31 UTF-8 bytes. Its representation is:

```text
data[0..payload_length)  = payload
data[payload_length]     = 0
remaining bytes          = 0
```

The first zero byte determines logical byte length. Embedded NUL is therefore
rejected. The terminator and zero-filled tail do not participate in logical
length, equality, ordering, or hashing.

A Strand is created only by contextual checking of a String literal:

```seawitch
name: Strand = "Seawitch"
label: Strand = "café"
empty: Strand = ""
```

The compiler rejects payloads over 31 UTF-8 bytes, embedded NUL, interpolation,
and invalid UTF-8. There is no runtime Strand constructor and no Strand heap
cleanup.

## String operations

The complete v1 String surface after RFC 0020 and RFC 0036 is:

```text
length()                              -> Size
is_empty()                            -> Bool
at(index)                             -> Rune
[index]                               -> Rune
bytes()                               -> View<Byte>
slice(start, end)                     -> View<Byte>
rune_cursor()                         -> RuneCursor
to_string(heap: Heap)                 -> String
concat(heap: Heap, other: String)     -> String
free(heap: Heap)                      -> no value
String.from_bytes(heap, View<Byte>)   -> String
String.from_runes(heap, View<Rune>)   -> String
```

`length`, `at`, indexing, and slice bounds are rune-oriented. They may scan
UTF-8 and are not promised constant time.

`bytes()` returns a zero-copy read-only View over the complete UTF-8 payload.
`bytes().length()` is the byte length. No separate public `byte_length()` method
is added.

`slice(start, end)` uses a zero-based, end-exclusive rune range, finds the
corresponding UTF-8 boundaries, and returns a zero-copy `View<Byte>`. The View
contains valid UTF-8 because the operation never splits a scalar. It remains
non-owning under RFC 0020 and RFC 0035.

`to_string`, `concat`, `from_bytes`, and `from_runes` create independent
runtime Strings using the supplied Heap. They use one combined allocation and
return an ordinary shallow-copyable String handle.

`free` releases a runtime String allocation created with the matching Heap.
Invalid cleanup is programmer error under RFC 0035.

## Strand operations

The complete Strand surface is:

```text
length()                 -> Size
is_empty()               -> Bool
at(index)                -> Rune
[index]                  -> Rune
to_string(heap: Heap)    -> String
```

Length and indexing decode the inline UTF-8 payload in rune units.
`to_string` creates one independent runtime String allocation.

Strand does not provide `bytes`, `slice`, `rune_cursor`, `concat`, `free`,
`from_bytes`, or `from_runes`. In particular, it never returns a View into its
inline bytes because a copied temporary Strand could make that pointer escape
the actual value being viewed.

The checker dispatches String and Strand methods separately and rejects a
method outside the receiver's exact surface.

## Rune traversal

String does not store contiguous Rune values and does not return `View<Rune>`.
Rune traversal decodes the existing UTF-8 bytes.

Direct `for` iteration follows RFC 0028:

```seawitch
for rune in text do
    print(rune)
end
```

`rune_cursor()` is the explicit cursor form:

```seawitch
cursor: RuneCursor = text.rune_cursor()

while cursor.has_next() do
    rune: Rune = cursor.next()
end
```

RuneCursor is a non-owning descriptor containing the source byte pointer, byte
length, and current byte offset. It allocates nothing. `next()` traps when no
Rune remains. No additional `.runes()` method is added.

Its complete method surface is:

```text
has_next() -> Bool
next()     -> Rune
```

`has_next` does not advance the cursor. `next` decodes one Rune and advances
the byte offset to the following UTF-8 boundary. Copying a RuneCursor copies
its current position and creates an independently advancing cursor over the
same borrowed String storage. RuneCursor has no cleanup operation. Its source
String must remain live until the cursor's final use; using a cursor after that
String is freed is programmer error under RFC 0035.

## Constructors and UTF-8 validation

### `String.from_bytes`

```seawitch
text: String = String.from_bytes(h, bytes)
```

The second argument must be `View<Byte>`; another View specialization is a type
error. Because Byte aliases UInt8, `View<UInt8>` is canonically the same
accepted type.

The operation validates the complete sequence before allocating. On success,
it allocates and copies the bytes. Invalid UTF-8 produces the existing defined
runtime trap:

```text
[Runtime Error] byte sequence is not valid UTF-8
```

It does not repair, truncate, replace, or return `String | Error`. This keeps
the closed RFC 0018 contract and avoids adding a second constructor model.

### `String.from_runes`

```seawitch
text: String = String.from_runes(h, runes)
```

The argument must be `View<Rune>`. The operation validates each scalar,
calculates the UTF-8 byte count with checked `Size` arithmetic, performs one
allocation, and encodes directly into it. An invalid scalar produces the same
defined Unicode runtime trap used by checked Rune creation.

### Allocation failure

Allocation failure uses the existing Heap runtime trap. Constructors do not
return Error merely because allocation can fail.

## Size and overflow

RFC 0036 is authoritative:

- String byte length is `Size` and lowers to `size_t`;
- Strand, String, View, and RuneCursor indices and counts use `Size`;
- generated loops use `size_t`; and
- allocation and concatenation overflow checks use `SIZE_MAX`.

Generated text code must not use `UInt64`, `uint64_t`, or `UINT64_MAX` for a
length, capacity, index, or byte offset. Rune values lower to `uint32_t`.

## C interoperability spelling

An imported C `uint8_t` is displayed as `UInt8`, because a C declaration does
not express whether the integer represents text, binary data, or a number.

Native byte-oriented Seawitch APIs are documented and diagnosed with `Byte`:

```text
String.bytes() -> View<Byte>
String.from_bytes requires View<Byte>
```

This distinction is presentational only. Both lower to `uint8_t` and have one
canonical type identity.

## C23 lowering

- Byte lowers exactly as UInt8 to `uint8_t`.
- Rune lowers to `uint32_t` after scalar validation.
- String lowers to `const sw_string *`.
- Runtime String header and bytes occupy one allocation.
- Static String header and bytes occupy one read-only generated aggregate.
- Strand lowers to exactly `struct { uint8_t data[32]; }`.
- String byte Views lower to the existing read-only pointer-plus-Size
  descriptor.
- RuneCursor lowers to a read-only byte pointer, Size byte length, and Size
  offset.
- all text helpers operate on explicit lengths and never use `strlen` for a
  String;
- helper operands are evaluated exactly once; and
- malformed or unsupported checked text nodes never reach generation.

## Diagnostics

Representative compile-time diagnostics are:

```text
[Syntax Error] invalid Byte literal escape
[Syntax Error] Byte literal must contain exactly one byte
[Syntax Error] Rune literal must contain exactly one Unicode scalar
[Syntax Error] Unicode surrogate is not a valid Rune
[Type Error] String.from_bytes requires View<Byte>; got View<Int32>
[Type Error] String.from_runes requires View<Rune>; got View<Byte>
[Type Error] Strand literal exceeds 31 UTF-8 bytes
[Type Error] Strand literal cannot contain NUL
[Type Error] Strand has no method bytes
[Type Error] String has no method runes
```

Defined runtime traps include:

```text
[Runtime Error] byte sequence is not valid UTF-8
[Runtime Error] invalid Unicode scalar value
[Runtime Error] String allocation failed
[Runtime Error] String index is outside its bounds
[Runtime Error] String slice bounds are invalid
[Runtime Error] RuneCursor has no next value
```

The lexer owns literal structure, escape syntax, decoded Byte and Rune
cardinality, and Unicode-scalar validity inside literals. The checker owns
contextual String-versus-Strand typing, Strand payload size and NUL exclusion,
method availability, View element types, and constant bounds when provable.
Runtime helpers own dynamic UTF-8 and Rune validation, allocation failure, and
dynamic bounds traps.

## Required implementation cleanup

Implementation includes one focused audit of compiler code, tests, and
canonical documentation:

1. add `Byte` to the builtin type registry as the canonical UInt8 alias;
2. replace Byte-removal diagnostics with Byte literal scanning and checking;
3. implement Rune literal scanning and checking using the shared text decoder;
4. use one literal decoder for String, Strand, Byte, and Rune where their
   escape sets overlap;
5. validate Strand payload size, NUL exclusion, and UTF-8;
6. lower Strand to one zero-filled 32-byte inline C value;
7. split String and Strand method checking;
8. make byte-oriented String APIs report `Byte` while retaining UInt8
   canonical identity;
9. require the exact View element identity in both String constructors;
10. validate `from_bytes` before allocation and implement `from_runes`;
11. implement RuneCursor and retain RFC 0028's direct String iteration;
12. replace remaining UInt64 text lengths and overflow constants with Size;
13. preserve the combined runtime and static String storage layouts;
14. remove stale migration text from `docs/grammar.md`, `docs/language.md`, and
    `docs/status.md`; and
15. add focused end-to-end and generated-C tests for every acceptance item.

## Non-goals

This RFC does not add:

- mutable String or Strand storage;
- `MutView<T>`;
- a nominal Byte type distinct from UInt8;
- `String<Byte>` or `String<Rune>`;
- a contiguous Rune representation for String;
- `.runes()` or `.byte_length()` convenience methods;
- implicit String, Strand, View, or C-pointer conversions;
- Unicode normalization, grapheme segmentation, locale behavior, or case
  folding;
- String interpolation;
- a String builder;
- automatic ownership, retain counts, or destructors; or
- recoverable Error returns from the existing text constructors.

## Acceptance criteria

This RFC is implemented when:

1. Byte and UInt8 have one canonical identity and C representation;
2. Byte, Rune, String, and contextual Strand literals implement the exact
   grammar, escapes, and Unicode validation above;
3. byte-oriented APIs expose `Byte` in source contracts without duplicate
   UInt8 generic specializations;
4. every String value contains valid UTF-8;
5. `String.from_bytes` accepts only View<Byte>, validates before allocation,
   returns String, and traps on invalid UTF-8;
6. `String.from_runes` accepts only View<Rune>, validates scalars, calculates
   size safely, and performs one String allocation;
7. String and Strand expose exactly their listed method sets;
8. String indexing, length, slicing, direct iteration, and RuneCursor all
   decode the same Rune sequence;
9. String bytes and rune-bounded slices return allocation-free read-only
   View<Byte> values;
10. runtime and static Strings use the RFC 0020 combined header-and-data shape;
11. Strand is exactly 32 inline bytes with no pointer or payload allocation;
12. Strand literals enforce the 31-byte, NUL, zero-tail, and UTF-8 invariants;
13. text lengths, indices, offsets, loops, and overflow checks use Size and
    `size_t` consistently;
14. String handles retain RFC 0035's shallow C copying and manual cleanup;
15. generated C uses no unchecked C string function to determine String
    length; and
16. stale Byte-removal behavior and documentation no longer exist.

## Readiness

The language semantics needed by this cleanup are already fixed by the cited
RFCs. No new syntax, ownership rule, text representation, recoverable error
choice, or implementation-blocking design question remains open. This RFC is
Ready for Implementation.
