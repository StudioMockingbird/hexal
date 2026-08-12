# RFC 0039: C Interoperability

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; initial design proposed
- Features: importing C headers and libraries, qualified foreign declarations,
  C ABI calls, callbacks, explicit text and pointer boundaries, and exporting
  Seawitch functions to C
- Created: 2026-08-11
- Depends on: RFC 0003 (scalars), RFC 0007 (pointer mutability), RFC 0008
  (functions and function pointers), RFC 0010 (nullable and Unknown pointers),
  RFC 0018 (String, Strand, Byte, and Rune), RFC 0020 (Array and View), RFC
  0026 (allocation and defer), RFC 0033 (no source pointer arithmetic), RFC
  0034 (modules and imports), RFC 0035 (C-style copying and manual lifetimes),
  RFC 0036 (`Size`), and RFC 0038 (checked conversions)
- Coordinates with: RFC 0043 (pointer-plus-length Views), RFC 0044 (String and
  Byte cleanup), and the future build, package, target-profile, exported-layout,
  and platform API specifications

## Summary

Seawitch should import a C header into one qualified namespace:

```seawitch
import c "stdlib.h" as libc

result: Int32 = libc.abs(-42)
```

The import is compile-time only. It does not execute code or create a runtime
module object. Include paths, preprocessor definitions, C language options, and
libraries come from the build target rather than arbitrary source-level flags.

Imported C functions call the platform C ABI directly. Seawitch inserts no
marshalling when the source and C representations already agree. Conversions,
text borrowing, nullability, and ownership changes must remain explicit.

C interop is a trust boundary. Foreign code may violate Seawitch's normal
memory, nullability, concurrency, and no-undefined-behavior guarantees. V1
should make the boundary visible without adding a general `unsafe` block
language until a concrete need is demonstrated.

## Goals

1. Make ordinary C libraries callable with little Seawitch wrapper code.
2. Keep every imported name qualified by its C import alias.
3. Preserve the target C ABI rather than inventing an interop ABI.
4. Map compatible scalars, pointers, records, and function pointers without
   runtime overhead.
5. Keep allocation, ownership, and cleanup the programmer's responsibility.
6. Preserve RFC 0033: imported pointers support reference and dereference, not
   source-level arithmetic or address conversion.
7. Allow simple Seawitch functions to be exported with stable C linkage.
8. Fail closed when the compiler cannot represent or verify a C declaration.

## Non-goals for the first implementation

- C++ or Objective-C interop.
- Automatic ownership inference, destruction, or lifetime recovery from C.
- Automatic conversion between String and C strings.
- Source-level pointer arithmetic, address integers, or unchecked pointer
  casts.
- Calling C variadic functions such as `printf` directly.
- Importing arbitrary function-like macros as normal Seawitch functions.
- Automatic bindings for C unions, bit fields, flexible array members, vector
  extensions, complex numbers, or compiler-specific attributes.
- Exporting generics, methods, ADTs, structural unions, collections, String,
  Stream, Task, or other Seawitch runtime representations as a C ABI.
- Reproducing C undefined behavior as defined Seawitch behavior.

## Proposed import syntax

The initial source form is:

```ebnf
c-import-declaration = "import", "c", string-literal, "as", identifier ;
```

```seawitch
import c "math.h" as math
import c "vendor/widget.h" as widget
```

The alias is mandatory. A C declaration is never injected unqualified into the
module namespace:

```seawitch
result: Int32 = math.abs(-10) // Qualified on current Windows/POSIX targets.
result: Int32 = abs(-10)      // Name Error.
```

C imports occupy the same qualifier namespace as RFC 0034 module imports. An
alias cannot collide with a module, local, type, function, or another import.
The same canonical header imported twice in one module is an error.

The spelling inside quotes is a C header name, not an arbitrary filesystem path
opened relative to the importing source file. The build configuration supplies
system and project include roots and resolves the header for the selected
target. Ambiguous or missing headers fail before source type checking.

Whether angle-bracket and quoted C include lookup need distinct source forms is
still open.

## Qualified C namespace

An imported header may contribute supported:

- external function declarations;
- typedef names;
- complete record types;
- incomplete or opaque record types usable only behind pointers;
- enumeration constants and types;
- external variables; and
- object-like compile-time constants that the importer can evaluate exactly.

```seawitch
handle: MutPtr<widget.Handle> | Nil = widget.open()
mode: Int32 = widget.MODE_READ
```

Imported C tags, typedefs, functions, variables, and constants are accessed
through the Seawitch alias even when C places them in different namespaces.
If two imported C declarations would collide after qualification, the importer
reports the collision rather than selecting one.

Static functions and private declarations from a header are not imported.
Support for `static inline` header APIs and macro-generated declarations is an
open implementation question.

## Proposed scalar mapping

The selected target profile determines C scalar width, signedness, alignment,
and calling convention before a header is imported.

The straightforward fixed mappings are:

| C type | Seawitch type |
|---|---|
| `_Bool` / C23 `bool` | `Bool` |
| `int8_t`, `int16_t`, `int32_t`, `int64_t` | `Int8`, `Int16`, `Int32`, `Int64` |
| `uint8_t`, `uint16_t`, `uint32_t`, `uint64_t` | `UInt8`, `UInt16`, `UInt32`, `UInt64` |
| `float` | `Float32` when the target confirms IEC binary32 |
| `double` | `Float64` when the target confirms IEC binary64 |
| `size_t` | `Size` |
| `void` return | no-result Seawitch function |
| `void` pointee | `Unknown` |

Plain C `char`, `short`, `int`, `long`, `long long`, their unsigned forms,
`wchar_t`, and C enum representation are target-dependent. The draft does not
yet decide whether they:

1. map directly to the matching fixed-width Seawitch scalar for that target;
   or
2. retain qualified foreign identities such as `c.Int` and require explicit
   `to<T>()` at native boundaries.

The first choice is smaller. The second prevents target-dependent source type
identity and preserves distinctions such as plain `char` versus `signed char`.

No C integer conversion becomes implicit merely because C would perform it.
Seawitch applies RFC 0016 widening and RFC 0038 `to<T>()` before the ABI call.

## Pointer and nullability mapping

Plain C object pointer syntax does not reliably state whether null is allowed.
The conservative imported mapping is proposed as:

| C type | Seawitch type |
|---|---|
| `const T *` | `Ptr<T> | Nil` |
| `T *` | `MutPtr<T> | Nil` |
| `const void *` | `Ptr<Unknown> | Nil` |
| `void *` | `MutPtr<Unknown> | Nil` |
| `T **` | recursively mapped pointer layers |

A trusted header annotation may later strengthen an imported pointer to
non-null. Without one, the caller narrows the nullable result before
dereference:

```seawitch
node: MutPtr<widget.Node> | Nil = widget.find(id)

if node is MutPtr<widget.Node>
    print(node.value)
end
```

Passing a non-null Seawitch pointer to a nullable C parameter changes no ABI
bits. A foreign null value must never enter a non-null Seawitch pointer type
without an explicit trusted contract.

RFC 0033 remains authoritative after import. A C pointer cannot be indexed,
advanced, subtracted, ordered, converted to an integer, or bit-cast in
Seawitch. A library exposing a pointer-plus-length buffer uses RFC 0043's
explicit `View<T>.from_pointer` bridge or imported C operations.

## Records, opaque types, unions, and enums

A complete imported C `struct` should become a qualified nominal foreign record
whose field order, padding, alignment, and ABI are fixed by the selected target
header. Read and write access follows the imported field's C const and pointer
context rather than changing layout.

An incomplete declaration such as `struct FILE;` creates an opaque qualified
type. It may appear behind Ptr or MutPtr but cannot be stored by value,
constructed, sized, copied by value, or dereferenced to fields.

C unions and bit fields are deferred from v1. Their active-member and layout
rules do not map to Seawitch ADTs, and an ADT must never be silently treated as
a C union.

C enums need a target ABI representation but also a source-level identity
decision. The draft must choose between a foreign nominal integer type and a
plain mapped integer plus qualified constants. Imported C enums are never
Seawitch ADTs.

## Functions and callbacks

An ordinary supported C prototype becomes a qualified callable declaration:

```c
int widget_read(struct widget *widget, void *buffer, size_t length);
```

Conceptually imports as:

```seawitch
widget.read(
    value: MutPtr<widget.Widget> | Nil,
    buffer: MutPtr<Unknown> | Nil,
    length: Size,
): Int32
```

Exact C parameter order and calling convention are preserved. A C `void`
return produces no Seawitch value; it does not return Nil.

Compatible C function pointers should map to `Fun<...>` and allow callbacks
whose ABI is exactly representable. Seawitch has no closures, so callbacks
carry no hidden environment. Nullable C callbacks map to `Fun<...> | Nil`.

Callbacks that require a user context use the C library's explicit `void *`
parameter. The program is responsible for keeping the pointed-to state alive
and recovering the correct concrete type. Callback calling conventions,
thread entry from foreign code, and foreign exceptions or long jumps require
separate readiness decisions.

C variadic calls are deferred. Seawitch must not lower a call to `printf` or a
similar function without a typed contract for every promoted argument.

## C strings and byte buffers

String and Strand are not implicitly convertible to C pointers. A String is
length-aware, may contain embedded NUL, and has Seawitch ownership. A C
`char *` carries none of those facts.

Both String and Strand currently have a trailing NUL in their physical
representation, but that fact alone is insufficient: embedded NUL is valid in
String, pointer lifetime must be bounded, and mutable C text must not receive
immutable storage.

The likely v1 bridge is an explicit call-scoped read-only borrow for String and
Strand plus an explicit pointer-and-length View bridge for binary buffers. The
exact source spelling is deliberately not fixed in this draft. Candidate text
shape:

```seawitch
name.with_c_string(fun (bytes: Ptr<Byte>)
    libc.consume_name(bytes)
end)
```

That example is design notation, not accepted syntax: Seawitch currently has no
function literals or closures. A practical v1 spelling may instead be a
compiler-owned borrow operation valid only as a direct foreign-call argument.
The final design must not allocate silently, permit a retained pointer, accept
embedded NUL unknowingly, or expose mutable access to String or Strand.

C output buffers should use MutPtr plus an explicit Size, then be wrapped or
copied into a Seawitch value deliberately. No imported pointer becomes a View
without a programmer-supplied length.

## Ownership, allocation, and cleanup

Foreign calls do not transfer ownership unless their imported contract says
so. V1 performs no automatic free, retain, release, destructor, or allocator
translation.

```seawitch
handle: MutPtr<widget.Handle> | Nil = widget.open()

if handle is MutPtr<widget.Handle>
    defer widget.close(handle)
    widget.use(handle)
end
```

The programmer must pair each C allocation with the library's matching C
deallocator. `Heap.free`, Arena cleanup, Pool cleanup, and collection `free`
must not release foreign allocations. Conversely, an imported C `free` must
not receive Seawitch-managed storage unless a later contract explicitly makes
the allocators compatible.

Copying imported pointers and records follows RFC 0035's C-style shallow copy
rules. Header annotations may eventually document ownership for diagnostics,
but v1 should not invent an ownership checker at the FFI boundary.

## Error behavior

C status returns, null pointers, `errno`, and out-parameters retain their C
shape. The importer does not automatically convert them into Error:

```seawitch
result: Int32 = widget.run(handle)

if result != 0
    return Error.new("Widget Error", "widget.run failed")
end
```

A wrapper function may translate the C convention into `T | Error`. Foreign
undefined behavior, process termination, signals, long jumps, and memory
corruption cannot be converted reliably into Seawitch Error values.

## Proposed exports to C

The draft reserves a visibly explicit export form, tentatively:

```seawitch
export c fun add(left: Int32, right: Int32): Int32
    return left + right
end
```

An exported function must have a non-generic signature containing only types
with a settled C ABI mapping. It receives one stable unmangled C symbol, emits
a generated C declaration, and uses the target C calling convention.

Exporting methods, overloaded names, generic specializations, closures, ADTs,
general unions, String, Strand, collections, Error, or runtime handles is
deferred. Name collision, symbol spelling, header generation, visibility
attributes, and entry from foreign threads remain open.

The exact `export c fun` syntax is not settled. It must coordinate with RFC
0034's `pub` visibility: public-to-Seawitch and exported-to-C are distinct
properties.

## Preprocessing, headers, and build integration

Headers must be interpreted using the selected target's include paths,
preprocessor definitions, integer model, ABI, and calling convention. Import
results become part of the importing module's deterministic dependency and
cache fingerprint.

The build configuration, not ordinary source, supplies:

- include roots;
- preprocessor definitions;
- target triple and ABI options;
- linked libraries and search paths; and
- optional generated-binding cache locations.

The compiler must record every transitively included header used by the import.
Changing one invalidates dependent bindings. Cache keys cannot depend on file
discovery order or process addresses.

The draft has not selected the header frontend. Options include using a
target-compatible Clang frontend, consuming a generated binding manifest, or
supporting a smaller manual foreign-declaration subset. Writing a complete C23
preprocessor and declarator parser inside the Seawitch compiler is not the
preferred small implementation.

## C23 lowering direction

- A C import emits or reuses the required `#include` in generated C.
- Imported declarations are checked from target-specific binding metadata and
  lower to direct C names or tiny generated adapters.
- Compatible arguments pass directly with no wrapper allocation.
- Nullable pointer unions use the C null-pointer niche and add no ABI tag.
- Imported complete records use verified target layout; opaque records remain
  incomplete C declarations.
- Callbacks lower to ordinary C function pointers with no hidden environment.
- Unsupported declarations fail before C generation rather than being omitted.
- Generated adapters retain `#line` mappings where user expressions are
  evaluated.
- The generated build links only libraries explicitly selected by the build
  configuration.

## Diagnostics direction

Representative future diagnostics include:

```text
[Import Error] C header stdlib.h was not found for the selected target
[Import Error] C declaration uses unsupported variadic parameters
[Import Error] C union layout is not supported in v1
[Type Error] imported pointer may be Nil; narrow it before dereference
[Type Error] String does not implicitly convert to a C character pointer
[Type Error] pointer arithmetic remains unavailable for imported pointers
[ABI Error] imported record layout disagrees with the selected target metadata
[Link Error] unresolved C symbol widget_open
```

The header frontend owns preprocessing and unsupported-declaration errors. The
Seawitch checker owns source calls, conversions, nullability, and pointer use.
The build driver owns library and unresolved-symbol failures. An impossible or
incomplete checked foreign operation reaching generation is an Unknown Error.

## Draft readiness questions

Before this RFC is Ready for Implementation, it must settle:

1. whether direct header import uses a Clang-compatible frontend, generated
   manifests, or explicit manual declarations;
2. the exact mapping and source identity of plain C integer and enum types;
3. whether system and quoted include lookup need distinct import syntax;
4. which object-like constants and `static inline` functions are imported;
5. the explicit, allocation-free String/Strand-to-`const char *` borrow syntax
   and embedded-NUL rule;
6. callback calling conventions, nullable callbacks, and calls from foreign
   threads;
7. the exact C export syntax, symbol naming, generated header, and supported
   exported type set;
8. whether imported declarations require an `unsafe` marker or whether the
   explicit `import c` qualifier is the complete trust-boundary marker;
9. build-file syntax for include paths, defines, libraries, and target ABI;
10. the initial policy for external variables, thread-local storage, `errno`,
    and C atomics; and
11. the conformance strategy for Windows and POSIX ABIs without requiring an
    external C toolchain in ordinary `go test ./...`.

## Initial direction

- C names remain qualified behind a mandatory import alias.
- C calls and supported records use the selected target ABI directly.
- Plain C pointers import as nullable unless a trusted contract proves
  otherwise.
- `const T *` maps to read-only Ptr and `T *` maps to writable MutPtr.
- Ownership and cleanup remain explicit C-style programmer responsibilities.
- Imported pointers gain no arithmetic, indexing, ordering, or address casts.
- String and Strand gain no implicit C pointer conversion.
- General C variadics, unions, bit fields, and compiler extensions begin
  unsupported.
- Seawitch generics and runtime-managed types are not directly exported.
- Unsupported or uncertain declarations fail closed.
