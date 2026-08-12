# RFC 0030: Print Builtin

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Features: typed `print`, deterministic standard-output emission, defined
  scalar and structural formatting, Error formatting, and Task-safe call
  ordering
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0003 (scalars), RFC 0008 (functions and calls), RFC 0018
  (String, Strand, Byte, and Rune), RFC 0019 (generics), RFC 0020 (String
  representation, collections, and borrowing), RFC 0022 (ADTs), RFC 0029
  (Error values), RFC 0035 (C-style copying and lifetimes), RFC 0036 (`Size`),
  and implemented RFC 0006 (nominal objects)
- Coordinates with: RFC 0031 (`EoS`), RFC 0037 (concurrency), RFC 0040 (I/O),
  and the future C FFI and module specifications

## Summary

Seawitch provides one compiler-owned basic output function:

```seawitch
print("count = ", count, "\n")
```

`print` writes the textual form of each argument to standard output in source
order. It inserts no separator and no newline. Programs spell punctuation,
spacing, and line endings explicitly.

Objects, ADTs, and core collections use one compiler-defined structural form.
This RFC adds no formatting language, interpolation, stream hierarchy,
user-defined display protocol, runtime reflection, or general
variadic-function feature.

## Goals

1. Make simple program and diagnostic output require no formatting ceremony.
2. Give every supported value one compiler-defined format; Dict entry order
   remains intentionally unspecified under RFC 0020.
3. Preserve embedded NUL bytes and UTF-8 payload lengths without relying on
   NUL termination.
4. Avoid C format-string injection and variadic type mismatches.
5. Keep output behavior common across Windows and POSIX through C23 stdio.
6. Pretty-print objects, ADTs, and core collections recursively without
   reflection, user hooks, or hidden allocation.
7. Keep one complete `print` call contiguous across Seawitch Tasks.

## Non-goals

- `println`, standard-error output, or explicit flushing.
- Format strings, interpolation, radix, width, precision, alignment, or padding.
- Printing `EoS`, structural unions, pointers, functions, allocators, Files,
  Streams, Channels, Tasks, Mutexes, or Atomics.
- User-defined display or debug methods.
- Width-sensitive layout, indentation, multiline output, or cycle detection.
- Recoverable output errors.
- Asynchronous or nonblocking host I/O.
- A source-level `stdout` object.

## Syntax and name resolution

`print` uses ordinary call syntax. It adds no grammar production:

```seawitch
print(expression, expression)
```

The checker recognizes an unqualified call whose callee is the protected
built-in name `print`. `print` is not a reserved keyword, but no module binding,
local, parameter, free function, generic parameter, or imported value may use
that unqualified name. A receiver-qualified object member or method named
`print` remains valid because it does not shadow the builtin:

```seawitch
logger.print(message) // Ordinary method, unrelated to the builtin.
```

The builtin is resolved before ordinary free-function lookup. It cannot be
overloaded, replaced, imported, referenced without a call, or converted to a
`Fun<...>` value.

At least one argument is required by the builtin's v1 signature. `print()` is a
Type Error; use `print("\n")` to emit a blank line. There is no
language-specific maximum argument count beyond the compiler's ordinary
resource limits.

`print` takes no generic type arguments. A spelling such as
`print<Int32>(value)` is rejected rather than treated as an overload.

`print` has no result value. It is valid as a call statement and as the direct
call action of `defer` or `errdefer`. RFC 0026's action context explicitly
permits a no-result call and discards a result when one exists; this does not
make `print` a value expression under RFC 0008. It is invalid wherever an
expression value is required:

```seawitch
print("done")
defer print("leaving\n")

bad: Nil = print("done") // Error: print produces no value.
```

## Supported argument types

V1 accepts exactly:

- `Bool`;
- `Int8`, `Int16`, `Int32`, and `Int64`;
- `UInt8`, `UInt16`, `UInt32`, and `UInt64`;
- `Size`;
- `Float32` and `Float64`;
- `Byte` through its canonical UInt8 identity;
- `Rune`;
- `String`;
- `Strand`;
- `Nil`;
- the reserved `Error` type from RFC 0029;
- a nominal object whose members are all printable;
- an ADT whose variant payload fields are all printable;
- `Array<T,N>`, `View<T>`, and `List<T>` when `T` is printable; and
- `Dict<K,V>` when `K` and `V` are printable.

Transparent aliases of a supported canonical type are supported. Every other
type is rejected.

RFC 0031's `EoS` remains non-printable by default. `EoS` marks Stream or
Channel completion rather than user data; a program that wants to display that
state narrows it and prints explicit text:

```seawitch
step: Int32 | EoS = stream.next()

if step is EoS
    print("eos")
else
    print(step)
end
```

A union is not printable even when every member would be printable separately.
The program must narrow or match it first:

```seawitch
value: Int32 | Error = read_count()

if value is Int32
    print("count = ", value)
else
    print("error = ", value)
end
```

This avoids adding hidden runtime display dispatch to structural unions.

## Recursive printability

Aggregate printability is decided completely from the static canonical type:

- an object is printable only when every declared member is printable;
- an ADT is printable only when every payload field of every variant is
  printable;
- an Array, View, or List is printable only when its element type is
  printable; and
- a Dict is printable only when both its key and value types are printable.

The requirement is recursive. An aggregate containing a pointer, function,
allocator, File, Stream, Task, or another unsupported value is rejected as one
unsupported argument:

```seawitch
type Node = {
    value: Int32,
    next: Ptr<Node>,
}

print(node) // Type Error: print does not support Node because next is Ptr<Node>.
```

The compiler reports the first unsupported member path in declaration order.
It does not print only the supported subset of an object or ADT.

Static recursive classification and generated type-specific helpers are used;
there is no runtime reflection, type metadata walk, formatting callback, or
user-defined display method. Printable values cannot contain a printable
pointer edge, and Seawitch rejects infinitely sized by-value types. Print
therefore needs no runtime cycle detector or cycle marker.

## Argument evaluation

All arguments evaluate exactly once from left to right. Every evaluation
finishes before the builtin writes any bytes for that call. The resulting typed
values remain live until the call completes or traps.

```seawitch
print(first(), second())
```

`first()` completes before `second()` starts. If either evaluation traps, the
outer print call writes nothing. Any output explicitly produced inside
`first()` or `second()` remains ordinary earlier output.

This left-to-right rule is a deliberate builtin exception to RFC 0008's
unspecified relative ordering of ordinary call arguments. It makes visible
output deterministic and allows the runtime to acquire its output lock only
after arbitrary user code has finished evaluating, avoiding lock reentrancy.

After evaluation, arguments are formatted and written left to right. Formatting
helpers execute no Seawitch source and do not evaluate argument expressions
again.

## Ownership and borrowing

Printing borrows its argument values and every recursively visited member or
element only for the call. It stores no pointer, handle, View, String, member,
or element after returning and performs no implicit allocation, copy, move, or
free.

A static, owning, parameter-borrowed, or collection-borrowed live String binding
may be printed under RFC 0020's existing rules. Passing an owning binding does
not transfer its cleanup obligation. A fresh owning String result still cannot
be nested or discarded merely because `print` borrows values:

```seawitch
text: String = make_text(h)
defer text.free(h)
print(text)

print(make_text(h)) // Error: owning String result must initialize an owner.
```

Error fields are read without changing ownership of their String handles.
Object fields, ADT payloads, and collection contents are likewise read in
place. Printing never extends a borrowed value's lifetime or invalidates a
View. No collection is mutated while its print helper is traversing it because
the helper invokes no Seawitch source code.

For `defer print(...)` and `errdefer print(...)`, RFC 0026's direct-call capture
rule applies: arguments evaluate and are captured when cleanup is registered;
the actual output occurs if and when the cleanup action runs.

## Textual representations

No representation below includes a trailing newline unless the value itself
contains one.

### Bool and Nil

- Bool prints exactly `true` or `false`.
- Nil prints exactly `nil`.

### Integers, Size, and Byte

- Signed integers print canonical base-ten notation.
- Zero prints `0`.
- A negative value has one leading `-` and no other sign.
- Unsigned integers, Size, and Byte print canonical unsigned base-ten notation.
- Positive values have no leading `+`.
- No value has leading zeroes, digit separators, radix prefixes, or suffixes.

Byte prints its numeric value, not an encoded character:

```seawitch
value: Byte = b'A'
print(value) // 65
```

### Rune

Rune prints the exact UTF-8 encoding of its Unicode scalar value. It does not
print quotes, escapes, a `U+` prefix, or its numeric code point. A newline Rune
therefore submits a newline byte to the C text stream.

### String and Strand

String prints exactly its logical UTF-8 payload bytes and uses the stored byte
length rather than NUL termination. Embedded NUL bytes are written as bytes and
do not truncate output. C text-stream newline translation may occur afterward
as described under standard-output behavior.

Strand prints its logical payload bytes. Its terminating NUL and zero-filled
inline tail are not written.

Neither text type gains interpolation, escaping, quoting, or allocation through
direct top-level `print`.

When String or Strand appears inside an object, ADT, or collection, it is
surrounded by double quotes so aggregate boundaries remain readable. The
nested form escapes `"`, `\\`, NUL, newline, carriage return, and tab as
`\"`, `\\`, `\0`, `\n`, `\r`, and `\t`. Every other byte below `0x20`, plus
`0x7f`, prints as `\xHH` with two uppercase hexadecimal digits. All remaining
UTF-8 payload bytes print unchanged.

This context rule keeps direct text useful while making nested text
unambiguous:

```seawitch
names: Array<Strand, 2> = ["hello", "sea"]
print("hello") // hello
print(names)   // ["hello", "sea"]
```

A nested Rune is surrounded by single quotes. It uses `\'`, `\\`, `\0`, `\n`,
`\r`, and `\t` for those scalar values; another Unicode control scalar prints
as `\u{HEX}` with uppercase hexadecimal digits and no leading zeroes. A
top-level Rune retains the raw UTF-8 behavior defined above. Byte remains
numeric in both contexts.

### Error

As a direct argument, Error prints its five RFC 0029 fields in this exact
compact form:

```text
file:line:column: header: message
```

For example:

```text
main.seawitch:12:5: File Error: file not found
```

`file` and `message` use String's exact top-level payload rule, `header` uses
Strand's top-level logical payload, and `line` and `column` use Size decimal
formatting. There is no numeric error code, stack trace, allocation, quoting,
or trailing newline.

Inside an object, ADT, or collection, Error uses this structural form so its
String fields cannot hide aggregate boundaries:

```text
Error { file = "main.seawitch", line = 12, column = 5, header = "File Error", message = "file not found" }
```

Fields remain in RFC 0029 declaration order and text fields use nested quoting
and escaping. This is display syntax only; RFC 0029 continues to forbid a
source-level `Error { ... }` constructor.

### Objects and ADTs

An object prints its defining nominal type name, then its members in declaration
order using source object-initializer punctuation:

```seawitch
type Point = {
    x: Int32,
    y: Int32,
}

point: Point = Point { x = 10, y = 20 }
print(point)
```

Output:

```text
Point { x = 10, y = 20 }
```

The defining type name is used even when the argument is written through a
transparent alias. A generic specialization includes its canonical type
arguments, such as `Box<Int32> { value = 10 }`. Empty objects do not exist.
Member values use nested formatting.

A unit ADT variant prints its qualified name. A record variant adds its active
payload fields in declaration order:

```seawitch
print(Direction.North)
print(Shape.Circle { r = 10 })
```

Output:

```text
Direction.North
Shape.Circle { r = 10 }
```

Only the active ADT payload is read. No numeric tag is exposed.

### Collections

Array, View, and List share one sequence form. Elements print in increasing
index order, separated by comma-space and enclosed in square brackets:

```seawitch
numbers: Array<Int32, 3> = [10, 20, 30]
points: Array<Point, 2> = [
    Point { x = 1, y = 2 },
    Point { x = 3, y = 4 },
]
print(numbers)
print(points)
```

Output:

```text
[10, 20, 30]
[Point { x = 1, y = 2 }, Point { x = 3, y = 4 }]
```

The empty sequence is `[]`. The output intentionally does not identify whether
the source was an Array, View, or List; these types all represent sequences.

Dict prints entries as `key: value`, separated by comma-space and enclosed in
braces:

```seawitch
print(scores)
```

Possible output:

```text
{"Ada": 10, "Lin": 20}
```

The empty dictionary is `{}`. Keys and values use nested formatting, so Strand
keys are quoted. Dict uses `: ` rather than the object field separator ` = ` to
distinguish map entries from named fields and to follow common map display
notation; this does not add a Dict literal syntax. Entries follow RFC 0020's
existing unspecified dictionary iteration order. `print` does not sort entries,
preserve insertion order, or allocate temporary storage. Programs must not
compare dictionary output as a stable serialized representation.

All aggregate formatting is one line. There is no indentation, line wrapping,
depth option, or alternate compact form. This is readable structural output,
not serialization: no parser, round-trip guarantee, or stable dictionary order
is implied.

### Float32 and Float64

Float formatting is locale-independent at the Seawitch level and uses these
exact special spellings:

- positive infinity: `inf`;
- negative infinity: `-inf`;
- every NaN, regardless of sign or payload: `nan`;
- positive zero: `0`; and
- negative zero: `-0`.

Finite nonzero Float32 values use C-locale `%g`-style formatting with precision
9. Float64 uses precision 17. The output therefore has at most that many
significant digits after `%g` removes trailing zeroes. These precisions
guarantee round-trip recovery of the same binary value when parsed as the same
type.

The `%g`-style rules are normative:

1. use decimal notation when the decimal exponent is at least `-4` and less
   than the significant-digit precision;
2. otherwise use scientific notation;
3. remove trailing fractional zeroes and remove a now-empty decimal point;
4. use `.` as the decimal separator;
5. use lowercase `e` for an exponent;
6. include the exponent sign and at least two exponent digits; and
7. never include a leading `+` on the complete value.

Seawitch starts in C's required `C` locale and exposes no locale or floating
environment mutation in v1. The runtime classifies non-finite values and signed
zero from their representation bits and normalizes their spellings itself
rather than accepting implementation-selected libc spellings. A future FFI or
locale specification must preserve this contract if foreign code can change
process-global numeric formatting state.

The representation is stable and round-trippable, but it is not promised to be
the shortest possible decimal spelling.

## Standard-output behavior

`print` submits bytes to the process's C23 `stdout` text stream. Within one call,
each argument is submitted completely before the next begins. C text-stream
rules may translate a submitted newline to the host line-ending representation.
No other source-level formatting changes. Seawitch does not switch Windows to a
binary console mode or maintain separate Windows/POSIX output implementations.
RFC 0040 preserves this text-stream contract for Stdio.stdout and Stdio.stderr;
exact binary I/O is limited to Files opened in binary mode.

The call is not transactional: if a host write fails after earlier bytes were
accepted, those earlier bytes may remain visible.

`print` does not flush after each call. On normal generated-program completion,
the runtime flushes `stdout`, checks the result, and then returns from C `main`.
An unrecoverable trap or host termination does not promise to flush buffered
output.

The runtime detects failures reported by C formatting, writing, or the final
normal flush. It writes this message to `stderr` on a best-effort basis and then
terminates through the ordinary unrecoverable runtime trap path:

```text
[Runtime Error] standard output write failed
```

If writing the diagnostic also fails, the process still terminates. `print`
does not return Error; RFC 0040 File writes provide recoverable output.
An external host event that terminates the process before C reports an I/O
result remains outside the language runtime's control.

## Concurrency

When RFC 0037's scheduler runtime is linked, one complete Seawitch `print` call
is atomic relative to every other `print` call and every RFC 0040 standard-File
text write:

```seawitch
print("task ", id, ": started\n")
```

Another Task's print or Stdio.stdout/Stdio.stderr text write cannot interleave
bytes between those arguments. The runtime evaluates all source arguments
first, then acquires one process-wide scheduler-aware standard-output lock,
formats and writes the complete call, and releases the lock. Waiting for that
lock parks only the Task, not its worker.

Argument evaluation occurs outside the output lock and may run concurrently
with another Task's print. Any nested print performed while evaluating an
argument is its own atomic call. Atomicity covers the outer call's emission
phase after all of its arguments have been evaluated.

The actual C stdio write may still block its current worker because v1 has no
asynchronous host-I/O backend. This matches RFC 0037's rule for opaque blocking
operations and does not change print syntax.

Programs without scheduler features initialize no scheduler or output lock.
They use the same formatting and writing helpers directly. The implementation
has no pthread-versus-Win32 output path; C23 stdio is shared across targets and
the optional Task lock belongs to the common scheduler runtime.

Seawitch `print`, Stdio.stdout/Stdio.stderr `write_text`, and standard output
`flush` operations participate in this atomicity and shutdown gate. Opened
Files, foreign C writes, other processes sharing a destination, and host
terminals do not participate and may interleave or transform bytes. A root Task
that exits without joining another output Task retains RFC 0037's process-exit
behavior; output from the abandoned Task is not guaranteed.

At root completion, every active `defer` in the root function or script scope
runs under RFC 0026 before scheduler shutdown closes the output gate. A root
`defer print("done\n")` therefore emits normally. Only after root cleanup has
completed does shutdown close the gate before final flushing. No Task may begin
another print, standard text write, or standard flush after that point. The
root waits for any call already holding the output lock, acquires that lock,
checks the final stdout flush and, when RFC 0040 standard error support is
linked, the final stderr flush, and then returns from C `main`. Tasks still
evaluating arguments or waiting for output are abandoned under RFC 0037 and
never touch a standard output stream after the gate closes. This prevents final
flushing from racing any Seawitch standard-output operation without suppressing
root cleanup output.

## Generics

A `print` argument whose type is an open generic parameter is a dependent
operation under RFC 0019. The checker records the argument and source span.
Every closed specialization must resolve the argument to one supported
canonical type before generation:

```seawitch
fun show<T>(value: T)
    print(value)
end

show<Int32>(10)     // Valid.
show<Point>(p)      // Valid when every Point member is printable.
show<Ptr<Point>>(q) // Specialization Type Error.
```

No unresolved print operation may reach C generation. Generic inference does
not add a display constraint or convert an unsupported value to String.

## C23 lowering

The generator emits private static helpers only when `print` is used. The
helpers use C23 standard facilities and are shared by Windows and POSIX:

- Every argument is first evaluated into one correctly typed temporary in
  source order.
- Static byte fragments use length-aware writes.
- String and Strand use `fwrite` with their logical byte lengths; generated
  code never calls `strlen` on a String payload.
- Rune is encoded into a fixed four-byte local buffer and written by length.
- Integer helpers format exact values without signed-minimum overflow. They may
  use `<inttypes.h>` macros or private decimal conversion, but every C variadic
  argument must exactly match its format.
- Size uses `%zu` or an equivalent private decimal helper; it is never passed
  to a mismatched fixed-width format.
- Float helpers inspect representation bits to handle every NaN, infinity, and
  signed zero explicitly, then use a fixed local buffer and the normative 9-
  or 17-digit finite format under the C locale contract.
- Error delegates to the same field helpers in direct compact form or nested
  structural form, always in fixed field order.
- Each printable object specialization gets a private helper that visits fields
  in declaration order.
- Each printable ADT specialization gets a private tag switch; every case
  visits only that variant's active payload fields in declaration order.
- Array, View, and List helpers use plain increasing-index loops over their
  existing storage. Dict helpers use a plain bucket scan and emit only occupied
  entries, matching RFC 0020's unspecified iteration order.
- Nested String, Strand, and Rune helpers emit the fixed quoting and escaping
  rules without allocating a temporary String.
- Recursive aggregate helpers call the same generated helper for each static
  member or element type. They invoke no Seawitch function and perform no
  reflection or cycle tracking.
- No source-controlled bytes are ever used as a C format string.
- Every formatting or write result is checked. Partial writes continue until
  complete or until C reports failure.
- A normal generated `main` closes the shared standard-output gate after root
  cleanup, waits for an in-flight operation, checks `fflush(stdout)`, and also
  checks `fflush(stderr)` when RFC 0040 standard-error support is linked.

The optional concurrency wrapper and RFC 0040 standard text-output helpers use
the same scheduler output lock and gate. Formatting has no Windows/POSIX
branch.

An unsupported type, unresolved generic print, malformed builtin call, or
unknown print node reaching generation is Unknown Error. It is never omitted,
lowered through raw `printf(source_text)`, or replaced with a placeholder.

## Diagnostics

The parser owns malformed ordinary call syntax. The checker owns protected-name
collisions, arity, argument eligibility, no-result use, ownership rules, and
generic specialization.

Required representative diagnostics are:

```text
[Name Error] print is a protected built-in name
[Type Error] print expects at least 1 argument
[Type Error] print does not take type arguments
[Type Error] print does not support Node because next is Ptr<Node>
[Type Error] print does not support Int32 | Error; narrow or match it first
[Type Error] print does not support File
[Type Error] print produces no value
[Type Error] owning String result must initialize an owner before it can be printed
```

Equivalent source-located wording from existing name, ownership, union, or
no-value diagnostics is acceptable when it identifies the same violation.
Detected runtime output failure uses the fixed Runtime Error message above and
is not a recoverable compiler diagnostic.

## Deferred

- `println` and `eprint`; RFC 0040 supplies recoverable standard-error writes
  and explicit flush without adding another print form.
- Format strings, interpolation, radix, width, precision, alignment, and
  padding.
- Output redirection beyond RFC 0040's explicit File operations.
- Structural-union, pointer, and runtime-resource formatting.
- User-defined display and debug protocols.
- Multiline pretty-printing, indentation, width limits, cycle markers, and
  stable or sorted Dict output.
- Asynchronous host I/O and dedicated blocking workers.
- Locale selection and foreign mutation of C's process-global locale or
  floating environment.

## Implementation direction

1. Implement outstanding dependencies required for Byte, Rune, Strand, Error,
   Size, and the final String representation.
2. Protect the unqualified builtin name and recognize ordinary `print(...)`
   calls without changing the parser grammar.
3. Add a checked print statement carrying ordered, typed argument operands and
   recursive borrow information.
4. Implement recursive supported-type classification, generic
   dependent-operation recording, and exact member-path diagnostics.
5. Emit shared length-aware text, Rune, integer, Size, Float, Error, aggregate,
   write, and final-flush helpers only when needed.
6. Evaluate every argument into a temporary from left to right before invoking
   output helpers.
7. Add the optional process-wide scheduler-aware lock integration from RFC
   0037 without adding a second platform I/O implementation.
8. Add fail-closed generator dispatch for every checked print form.
9. Add focused parser, checker, generator, ownership, generic, aggregate,
   nested-escaping, root-defer ordering, runtime-failure, embedded-NUL,
   float-edge, and concurrent-output tests.
10. Update canonical grammar, language, and status documents after behavior is
    implemented and verified.

## Required conformance coverage

Implementation is complete only when focused tests establish all of the
following:

1. `print` uses ordinary call syntax, requires at least one argument, remains a
   protected non-keyword name, and is not a first-class function value;
2. every listed canonical type, printable aggregate, and transparent alias is
   accepted, while EoS, every other excluded type, every recursively
   unprintable aggregate, and every union receives a source-located Type Error;
3. arguments evaluate exactly once from left to right and no outer-call bytes
   are written until all evaluations succeed;
4. arguments format and write in source order with no separator or implicit
   newline;
5. every signed and unsigned integer boundary, Size target width, Byte value,
   Bool, and Nil uses the exact spelling defined here;
6. every valid Rune submits its exact UTF-8 bytes before permitted C text-stream
   newline translation;
7. String preserves embedded NUL and Strand excludes its terminator and tail;
8. direct Error emits exactly `file:line:column: header: message` with no
   newline, while nested Error emits its exact declaration-ordered structural
   form with quoted text fields;
9. objects print their defining name and declaration-ordered fields, while
   ADTs print their qualified active variant and only its active payload;
10. Array, View, and List print increasing-index sequence forms; Dict output,
    checked without assuming an order, contains every occupied entry exactly
    once with exact `key: value` punctuation and separators and performs no
    sorting or allocation;
11. nested String, Strand, Rune, and Error values use their exact contextual
    forms while direct text, Rune, and Error arguments retain their compact or
    raw behavior;
12. Float32 and Float64 cover ordinary values, decimal/scientific thresholds,
   round-trip boundaries, infinities, every NaN class, and positive and negative
   zero under the exact spelling rules;
13. printing recursively borrows values without copying, moving, retaining,
    allocating, or changing cleanup obligations, and direct fresh owning String
    results remain rejected;
14. deferred print and RFC 0040 standard-output actions capture and run under
    their owning rules, and every root defer runs before the shared output gate
    closes;
15. detected formatting, write, partial-write, and final-flush failures terminate
    with the fixed Runtime Error behavior;
16. concurrent Seawitch print calls are complete-call atomic and wait through
    the scheduler without blocking another worker; their output lock and gate
    are common runtime facilities that RFC 0040 standard text writes join when
    linked, close only after root cleanup, and prevent participating operations
    from racing the final standard-stream flushes;
17. programs without scheduler features initialize no output lock or scheduler;
18. generic print arguments are rechecked for every closed specialization;
19. generated C uses no source-controlled format string, mismatched variadic
    type, NUL-terminated String assumption, or unchecked helper result; and
20. every unsupported or impossible compiler state fails closed at its earliest
    proving phase.

## Finalized decisions

- `print` requires at least one argument and has no result.
- Arguments evaluate left to right before any bytes for the call are written.
- Direct Error uses compact `file:line:column: header: message` formatting;
  nested Error uses a declaration-ordered structural form.
- Objects and ADTs print one-line nominal structural forms in declaration order.
- Array, View, and List print one sequence form in index order.
- Dict prints one structural form in existing unspecified iteration order; it
  does not sort or allocate.
- Nested text and Rune values are quoted and escaped; direct ones remain raw.
- Float32 uses `%g` precision 9 and Float64 uses precision 17 with fixed
  non-finite and signed-zero spellings.
- Detected output failure is an unrecoverable Runtime Error.
- `print` does not flush per call; normal program completion checks one final
  flush.
- One complete Seawitch print is atomic across Tasks; RFC 0040 standard text
  writes join the same common lock and become atomic relative to every
  participating standard-output operation when linked.
- Root defers complete before scheduler shutdown closes the shared output gate;
  final stdout and applicable stderr flushes occur after the gate closes.
- No warning, formatting language, interpolation, or user display protocol is
  introduced.
