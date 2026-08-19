# RFC 0039: C Interoperability — Compiler Core

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; initial compiler-core design proposed
- Features: foreign binding modules, qualified C declarations, ABI checking,
  direct C calls, callbacks, explicit pointer/text boundaries, and C exports
- Created: 2026-08-11
- Updated: 2026-08-14
- Depends on: RFC 0003 (scalars), RFC 0007 (pointer mutability), RFC 0008
  (functions and function pointers), RFC 0010 (nullability), RFC 0018 (text),
  RFC 0020 (Array and View), RFC 0026 (allocation and cleanup), RFC 0033 (no
  source pointer arithmetic), RFC 0034 (modules), RFC 0035 (copying and manual
  lifetimes), RFC 0036 (`Size`), RFC 0038 (conversion), RFC 0043
  (pointer-length View bridge), and RFC 0044 (String/Byte conformance)
- Coordinates with: RFC 0052 (target profiles) and ADR 0055 (filesystem and
  build driver)

## Author note for the detailed design pass

When this RFC is next detailed, remind the author to design and validate C
interop incrementally:

1. import one minimal C program;
2. import a more complex C program;
3. import a header-only library; and
4. integrate a complete external project such as raylib.

Each stage may receive its own subordinate specification or ADR. The design
must also decide how the driver and binding pipeline accept supported projects
written for older C language versions rather than assuming all foreign source
is C23.

## Scope

This RFC defines only behavior implemented directly in the in-memory compiler.

- Input remains `Compile(sources map[string]string, entrypoint string)`.
- Every compiler input is a complete source string under a logical key.
- Every compiler output is generated text in `CompilationResult.Files`.
- The compiler performs no filesystem access, process execution, header
  discovery, preprocessing, C-project build, object inspection, or linking.
- ADR 0055 owns files, external tools, C projects, objects, libraries, and final
  artifact materialization.

## Goals

- Represent supported C declarations as typed Hexal compiler input.
- Keep foreign names qualified through the native module system.
- Check C ABI eligibility before generation.
- Lower compatible calls and values without allocation or marshalling.
- Keep nullability, ownership, text conversion, and cleanup explicit.
- Support callbacks with no hidden closure environment.
- Export ABI-safe Hexal functions and declarations to C.
- Fail closed for every unsupported or unverified foreign operation.
- Make the foreign trust boundary visible without adding a general `unsafe`
  language feature.

## Required compiler additions

This RFC requires the following work directly in the core compiler. Later
sections define each contract in detail.

### 1. Foreign declarations

The compiler must parse, represent, resolve, and check binding-module
declarations for:

- C functions;
- opaque types;
- complete structs;
- enums and typed constants;
- external variables;
- function pointers;
- calling conventions; and
- nullability, ownership, retention, and deallocator annotations.

Foreign declarations remain nominal members of their binding module and expose
only explicitly exported names to native importers.

### 2. Foreign type model

The compiler type system must represent:

- foreign opaque types;
- foreign complete records;
- distinct C integer identities where fixed Hexal scalar mapping is
  insufficient;
- foreign enums;
- C-compatible function pointers;
- ABI-qualified functions; and
- external variables.

Every foreign nominal type and declaration identity includes its defining
binding-module identity. Native import aliases never create a new foreign type.

### 3. ABI checking

Before generation, the checker must verify:

- every foreign-call parameter and result is C-compatible;
- every C export has a fully settled C ABI signature;
- nullable C pointers remain nullable until narrowed or covered by a trusted
  non-null contract;
- opaque values appear only in permitted pointer positions;
- unsupported Hexal values never cross the C boundary;
- String and Strand never convert implicitly to C character pointers;
- callbacks have compatible signatures and calling conventions;
- callbacks carry no captured environment;
- required target and layout evidence is present and consistent; and
- C `void` produces no Hexal result and never becomes `Nil`.

### 4. C lowering

The generator must:

- emit required `#include` directives without resolving the headers;
- call original C symbols directly;
- preserve declared C symbol names and calling conventions;
- emit compatible function-pointer calls and callback thunks only when
  required;
- emit exported Hexal wrappers and C declarations;
- pass ABI-compatible values directly; and
- perform no allocation or marshalling when representations already agree.

### 5. C exports

The compiler must support ABI-safe Hexal functions exposed to C once final
syntax is settled. For each export it must:

- validate the complete ABI signature;
- assign or accept one stable C symbol;
- emit the C-linkage definition or required wrapper;
- emit a matching declaration; and
- add the generated declaration header to `CompilationResult.Files`.

Conceptual notation only:

```hexal
extern c export fun add(left: Int32, right: Int32): Int32
    return left + right
end
```

### 6. Diagnostics

The compiler must own structured diagnostics for:

- unknown foreign declarations;
- unsupported ABI types or calling conventions;
- nullable-pointer misuse;
- invalid callback signatures or statically provable lifetime misuse;
- invalid C exports;
- opaque-type misuse;
- String/C-pointer mismatches;
- contradictory symbol or layout contracts; and
- impossible checked foreign operations reaching lowering.

Missing files, failed header processing, failed C builds, and linker errors are
driver diagnostics under ADR 0055, never core compiler diagnostics.

## Binding-module boundary

The proposed compiler/driver boundary is a generated or handwritten Hexal
binding module.

- A future driver reads and preprocesses C headers using an external C
  frontend.
- The driver converts supported declarations into a deterministic Hexal binding
  module string.
- The binding string is added to `sources` under an ordinary logical `.hex`
  key.
- Application source imports it using RFC 0034's normal qualified module form.
- The core compiler never parses raw `.h` or `.c` text.
- Handwritten binding modules use the same syntax and semantics as generated
  ones.

Conceptual driver output; exact foreign-declaration grammar remains open:

```hexal
extern c header "widget.h"

export extern c type Handle = opaque

export extern c fun open(): MutPtr<Handle> | Nil
    symbol "widget_open"
end

export extern c fun close(handle: MutPtr<Handle>)
    symbol "widget_close"
end
```

Application source remains ordinary Hexal:

```hexal
module Widget = import "./bindings/widget"

handle: MutPtr<Widget.Handle> | Nil = Widget.open()
```

The conceptual binding notation is not accepted syntax until this RFC settles
its grammar.

### The open grammar question

**This is the first gate on the rest of the RFC and is deliberately unresolved.**
Everything downstream — the declaration model, ABI checking, lowering, exports —
assumes a notation exists; none of it depends on which one.

The sketch above puts three modifiers before `fun` (`export extern c fun`) and
gives the symbol name a clause that looks like a body but is not. An alternative
worth evaluating in the same pass, recorded so it is not lost: since a binding
module is *wholly* foreign and normally machine-generated, foreign-ness can be
structural rather than repeated per declaration.

```hexal
foreign "widget.h" do
    export type Handle = opaque
    export fun open(): MutPtr<Handle> | Nil = "widget_open"
    export fun close(handle: MutPtr<Handle>) = "widget_close"
end
```

One new keyword instead of a modifier chain, and the C symbol becomes a value
rather than a pseudo-body. Goal 3 keeps the language surface small, and
`extern c` repeated on every declaration works against it.

Neither form is adopted here. Whichever pass settles this must also answer the
question the author note raises — how binding generation accepts C projects
written against older C standards rather than assuming C23 — because that
constrains what the notation has to express.

## Foreign declaration model

The compiler needs checked representations for:

- external functions and their exact C symbols;
- external variables;
- foreign scalar identities;
- foreign enums and constants;
- complete foreign records;
- incomplete/opaque foreign records;
- C function pointers and calling conventions; and
- header/include requirements copied into generated C.

Rules:

- A foreign declaration belongs to its binding module.
- Native import aliases never change foreign declaration identity.
- Imported foreign names remain qualified like every other module export.
- Hexal visibility and C linkage are separate properties.
- Two foreign declarations with the same C symbol must have one compatible ABI
  contract; conflicting declarations are ABI Errors.
- Static/private header declarations are absent unless a binding generator
  deliberately creates a supported wrapper declaration.

## Scalar mapping

Settled direct mappings:

| C ABI type | Hexal type |
|---|---|
| `_Bool` / C23 `bool` | `Bool` |
| `int8_t`, `int16_t`, `int32_t`, `int64_t` | `Int8`, `Int16`, `Int32`, `Int64` |
| `uint8_t`, `uint16_t`, `uint32_t`, `uint64_t` | `UInt8`, `UInt16`, `UInt32`, `UInt64` |
| IEC binary32 `float` | `Float32` |
| IEC binary64 `double` | `Float64` |
| `size_t` | `Size` |
| C `void` result | no Hexal result |
| C `void` pointee | `Unknown` |

- C `void` never maps to `Nil`.
- C integer promotions do not become Hexal implicit conversions.
- Hexal widening and explicit `to<T>()` rules remain authoritative before an
  ABI call.
- Plain `char`, `short`, `int`, `long`, `long long`, unsigned variants,
  `wchar_t`, and enum representations require a settled foreign identity and
  target-evidence model before implementation.

## Pointers and nullability

Default mapping without a trusted non-null contract:

| C ABI type | Hexal type |
|---|---|
| `const T *` | `Ptr<T> | Nil` |
| `T *` | `MutPtr<T> | Nil` |
| `const void *` | `Ptr<Unknown> | Nil` |
| `void *` | `MutPtr<Unknown> | Nil` |
| `T **` | recursively mapped pointer layers |

- C pointer syntax alone never proves non-null.
- A non-null Hexal pointer may pass to a nullable C parameter with no ABI
  conversion.
- A foreign null value never enters a bare Hexal pointer without an explicit
  trusted contract.
- Imported pointers retain RFC 0033 restrictions: no arithmetic, indexing,
  subtraction, ordering, integer conversion, or bit-cast.
- Pointer-plus-length buffers require the explicit RFC 0043 View bridge or
  deliberate copying.
- No pointer gains ownership from its type alone.

## Records, opaque types, enums, and globals

- A complete foreign record is a qualified nominal type with externally
  supplied layout evidence.
- Its order, padding, alignment, and ABI follow the verified C declaration.
- Field mutability follows the checked foreign declaration and pointer
  constness; it never changes layout.
- An opaque type may appear behind Ptr/MutPtr but cannot be constructed, stored
  by value, sized, copied by value, or dereferenced to fields.
- C unions, bit fields, flexible array members, vector types, complex types,
  and compiler-specific layout attributes are unsupported initially.
- C enums remain foreign nominal integer types or canonical mapped integers
  plus qualified constants; the exact choice is open.
- External variables require explicit foreign declarations. Thread-local
  variables, volatile globals, and C atomics require separate settled rules.
- Imported object-like constants must already be reduced by the binding
  generator to an exact typed value; the core compiler does not evaluate C
  preprocessor expressions.

## Functions and callbacks

- A supported foreign prototype becomes a qualified callable declaration.
- Parameter order, ABI types, calling convention, and C symbol are preserved.
- A C `void` result is a no-result Hexal call.
- Compatible C function pointers map to `Fun<...>` with an explicit foreign ABI
  contract.
- Nullable callbacks map to `Fun<...> | Nil`.
- Hexal callbacks have no hidden environment because Hexal has no closures.
- Stateful callbacks use the C API's explicit context pointer.
- The program must keep callback code and context storage alive for the entire
  foreign retention period.
- Calls from foreign threads require a settled runtime-entry contract.
- C variadic functions are unsupported until every promoted argument has a
  statically representable contract.
- `setjmp`/`longjmp`, foreign exceptions, signals, and asynchronous re-entry do
  not map to Hexal Error.

## Text and buffers

- String and Strand never convert implicitly to C character pointers.
- A C-string borrow must be explicit, read-only, allocation-free, scoped to one
  direct foreign call, and rejected when embedded NUL would change meaning.
- Foreign code must not retain a borrowed String/Strand pointer.
- Mutable C text never receives immutable String/Strand storage.
- Binary buffers use pointer plus explicit Size and may be bridged to View only
  through RFC 0043.
- C output buffers are copied or wrapped deliberately after the call.
- The exact source spelling for call-scoped C-string borrowing remains open.

## Ownership, cleanup, and errors

- A foreign call transfers ownership only when its binding contract says so.
- The compiler performs no automatic free, retain, release, destructor, or
  allocator translation.
- Foreign allocations must be released through their matching foreign
  deallocator.
- Heap, Arena, Pool, and collection cleanup never release foreign allocations
  unless a future explicit allocator-compatibility contract permits it.
- Imported C deallocators never receive Hexal-managed storage by default.
- Foreign pointers and records follow Hexal's settled shallow-copy rules.
- C status returns, nullable results, `errno`, and out-parameters retain their
  declared shapes; the compiler does not synthesize Error values.
- Native wrapper functions may translate a foreign convention into `T | Error`.
- Foreign undefined behavior, termination, memory corruption, and long jumps
  cannot be converted reliably into Hexal Error.

## Exports to C

- The compiler may expose a non-generic Hexal function under stable C linkage
  when every parameter and result has a settled C ABI mapping.
- C linkage and native-module `export` visibility remain independent.
- The compiler emits the C definition, stable symbol, and declaration text.
- Export declarations are returned as generated header content in
  `CompilationResult.Files`.
- Methods, generics, ADTs, structural unions, String, Strand, collections,
  Error, Stream, Task, and runtime handles are not initially exportable.
- Exact syntax, symbol naming, output-header ownership, visibility attributes,
  and foreign-thread entry remain open.

Conceptual notation only:

```hexal
extern c export fun add(left: Int32, right: Int32): Int32
    return left + right
end
```

## Lowering contract

- Foreign header requirements become deterministic `#include` directives in
  generated C; the compiler never resolves those headers.
- Imported calls lower to their exact C symbols or a required tiny adapter.
- ABI-compatible arguments pass directly with no wrapper allocation.
- Nullable pointer unions use the C null-pointer niche and add no tag.
- Complete foreign records use verified layout metadata; opaque records remain
  incomplete declarations.
- Callbacks lower to ordinary C function pointers with no hidden environment.
- Compiler-generated C-export headers are returned as strings.
- Unsupported foreign declarations fail before generation and are never
  silently omitted.
- Generated adapters retain `#line` mapping where Hexal expressions execute.

## Diagnostics

The compiler owns structured diagnostics for:

- invalid foreign declaration syntax;
- unknown or private foreign names;
- conflicting C symbol contracts;
- unsupported ABI types or calling conventions;
- absent or contradictory layout evidence;
- nullable-pointer misuse;
- opaque-type construction, sizing, copying, or field access;
- String/C-pointer mismatches;
- invalid callbacks or callback lifetimes provable statically;
- invalid C exports; and
- impossible checked foreign operations reaching generation.

Filesystem, header-frontend, C-project, object, library, compiler, and linker
failures belong to ADR 0055's driver. They are not compiler diagnostics.

## Pure-Go conformance

- Ordinary compiler tests use handwritten binding-module strings.
- Tests cover parsing, checking, diagnostics, identity, lowering, generated
  includes, direct C symbols, callbacks, and export headers without invoking an
  external tool.
- Generated C execution remains outside ordinary `go test ./...`.
- Target ABI facts used by the checker require explicit trusted evidence; the
  compiler never probes the host.

## Compiler non-goals

- Reading `.h` or `.c` files.
- Implementing a C preprocessor or general C23 parser.
- Running Clang, GCC, a build system, or a linker.
- Building C projects.
- Parsing or linking `.o`, `.obj`, `.a`, `.lib`, `.so`, `.dll`, or `.dylib`.
- Resolving include roots, library paths, packages, or system frameworks.
- C++, Objective-C, variadic calls, arbitrary macros, unions, bit fields, or
  compiler extensions in the initial implementation.
- Adding source pointer arithmetic, address integers, unchecked casts, or a
  general `unsafe` block.

## Driver handoff

ADR 0055 will eventually:

- read and preprocess headers;
- generate binding-module strings;
- add those strings to the `sources` map;
- read and compile C sources;
- build configured C projects;
- resolve objects and libraries;
- materialize `CompilationResult.Files`; and
- compile and link the final program.

Objects, libraries, and C projects require no direct compiler representation.
The binding module supplies the types and symbols; the driver supplies the
linked implementation.

## Readiness questions

Before implementation, settle:

1. exact `extern c` declaration grammar and interaction with native `export`;
2. binding-module header/include declaration syntax;
3. C symbol spelling and alias syntax;
4. plain C integer, `char`, `wchar_t`, and enum identities;
5. trusted target/layout evidence supplied by generated bindings;
6. complete foreign-record field and array representation;
7. trusted non-null, ownership, retention, and deallocator annotations;
8. call-scoped String/Strand-to-`const char *` borrowing syntax;
9. callback calling conventions, retention, context recovery, and foreign
   thread entry;
10. external variables, TLS, volatile values, `errno`, and C atomics;
11. exact C-export syntax, stable symbol rules, supported types, and generated
    header paths; and
12. whether generated binding modules are the final compiler/driver boundary
    or an additional normalized manifest format is necessary.

## Reference synchronization

Implementation updates `docs/reference.md` after behavior stabilizes and before
this RFC closes:

- add final foreign-declaration grammar;
- add binding-module identity, qualification, visibility, and trust-boundary
  rules;
- add foreign scalar, pointer, record, enum, callback, text, ownership, and
  error contracts;
- add C export and generated-header contracts;
- add foreign diagnostics and C23 lowering rules; and
- remove only implemented C-interoperability items from Excluded features.
