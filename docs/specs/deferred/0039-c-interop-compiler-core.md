# RFC 0039: C Interoperability — Compiler Core

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; initial compiler-core design proposed
- Features: foreign binding modules, qualified C declarations, ABI checking,
  direct C calls, callbacks, explicit pointer/text boundaries, and C exports
- Created: 2026-08-11
- Updated: 2026-09-08
- Depends on: RFC 0003 (scalars), RFC 0007 (pointer mutability), RFC 0008
  (functions and function pointers), RFC 0010 (nullability), RFC 0018 (text),
  RFC 0026 (allocation and cleanup), RFC 0033 (no
  source pointer arithmetic), RFC 0034 (modules), RFC 0035 (copying and manual
  lifetimes), RFC 0036 (`Size`), RFC 0038 (conversion), and RFC 0044
  (String/Byte conformance)
- Coordinates with: RFC 0052 (C compiler backend), RFC 0110
  (affine ownership and Stashes), RFC 0149 (`Box<T>` and call-scoped
  references), RFC 0153 (`Slice<T>`/`Slice<mut T>`), RFC 0152 (generic
  `Strand<N>`), RFC 0151 (Open Discussion alternative for String-only text and
  `[N]T` fixed arrays), and ADR 0055 (filesystem and build driver)

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

- Input remains `Compile(sources map[string]string, entrypoint string,
  project Project)`.
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
- Make the C boundary an explicit unsafe/foreign boundary rather than implying
  that imported C operations inherit safe Hexal guarantees.
- Preserve a controlled escape hatch for raw layouts, pointer arithmetic,
  pointer casts, foreign globals, and target-specific instructions.
- Fail closed for every unsupported or unverified foreign operation.
- Keep unsafe capabilities explicit and local; do not make the safe native
  language model grow C's unrestricted object model.

## Foreign trust boundary and unsafe capabilities

C interoperability is the first explicit unsafe boundary in Hexal. A foreign
declaration is not a proof that the foreign implementation is memory-safe,
data-race-free, ABI-correct, or valid for every target. It is a typed contract
that permits selected operations to cross into code whose implementation the
Hexal checker cannot inspect.

The boundary has three levels:

1. **Checked foreign calls.** A binding supplies a complete signature, ABI,
   nullability, layout, and ownership contract. Hexal performs every check it
   can prove from that contract, but foreign behavior remains outside the
   native safety guarantee.
2. **Explicit unsafe representations.** Raw C unions, bit fields, flexible
   array members, address integers, pointer arithmetic, pointer casts, foreign
   globals, and target-specific layout may be represented only by an explicit
   unsafe declaration or unsafe operation. They never become ordinary native
   types by import alone.
3. **Backend escape hatches.** Inline assembly and compiler-specific
   extensions are target-qualified foreign operations. They require an
   explicit target/profile contract and are never portable Hexal expressions.

The exact spelling of `unsafe` remains a grammar question, but the semantic
boundary is settled by this RFC: a checked foreign binding may expose only the
contracted operations; anything requiring facts not represented by that
contract fails closed or requires an unsafe declaration. Unsafe code may call
safe code, but safe code cannot silently acquire an unsafe pointer, layout, or
ownership capability.

Foreign ownership annotations must compose with the affine ownership and stash
rules introduced by RFC 0110. A foreign allocator may transfer ownership only
through a declared deallocator contract; Hexal `Heap`, `Stash<T>`, and `Pool<T>`
never reclaim foreign storage by accident, and foreign deallocators never
receive Hexal-managed storage without an explicit compatibility contract.

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
- String never converts implicitly to a C character pointer;
- callbacks have compatible signatures and calling conventions;
- callbacks carry no captured environment;
- required target and layout evidence is present and consistent; and
- C `void` produces no Hexal result and never becomes `Nil`.

ABI checking must also classify each declaration by trust level. A checked
foreign declaration may expose only representation and ownership facts that
its contract states. A declaration using raw union layout, bit fields,
flexible array members, address integers, pointer arithmetic, pointer casts,
foreign globals, inline assembly, or compiler-specific extensions is unsafe
and must carry the explicit unsafe marker required by the final grammar.

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

export extern c type Handle is opaque

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

handle: MutPtr<Widget.Handle> | Nil := Widget.open()
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
    export type Handle is opaque
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
- ABI-dependent scalar aliases resolve from trusted target evidence. The
  binding retains the original C spelling even when Hexal exposes a native
  scalar with the same representation.

## Foreign type inventory and priority

This inventory covers C type categories and standard/platform typedef
families, not every library-defined typedef. Library types are classified into
the same foreign scalar, enum, record, union, or opaque categories.

Priority is implementation order:

- **P0:** required for basic C and raylib interoperability;
- **P1:** required for broad system-library interoperability;
- **P2:** specialized numerical, atomic, or machine APIs;
- **P3:** opaque/wrapper-only until demonstrated demand.

Hexal does not gain a global builtin for every C spelling. The binding model
uses three representations:

1. **Native mapping:** an ABI-stable C type maps to an existing Hexal type.
2. **Target-resolved alias:** trusted target facts select a native
   representation while generated C retains the original C spelling.
3. **Foreign-only type:** a module-qualified nominal/layout identity exists
   only at the C boundary and does not enlarge ordinary Hexal semantics.

### Fundamental and standard scalar types

| C type or family | Hexal today | Recommendation / priority |
| --- | --- | --- |
| `void` result | no-result function | Existing native mapping; never Nil. **P0** |
| `void` object/pointee | `Unknown` behind pointers | Preserve C `void` metadata. **P0** |
| `bool`/`_Bool` | `Bool` | Existing native mapping. **P0** |
| plain `char` | no exact identity | Target-resolved foreign scalar; do not assume signedness. **P0** |
| `signed char`, `unsigned char` | `Int8`, `UInt8`/`Byte` | Map after 8-bit-byte target validation. **P0** |
| signed/unsigned `short` | fixed integers only | Target-resolved alias, normally Int16/UInt16. **P0** |
| signed/unsigned `int` | fixed integers only | Target-resolved alias, normally Int32/UInt32. **P0** |
| signed/unsigned `long` | no portable equivalent | Target-resolved alias; LP64 and Windows LLP64 differ. **P0** |
| signed/unsigned `long long` | `Int64`/`UInt64` | Target-resolved alias. **P0** |
| `intN_t`/`uintN_t` | matching fixed integers | Existing exact-width mappings. **P0** |
| `int_leastN_t`/`uint_leastN_t` | no distinct identity | Target-resolved aliases. **P1** |
| `int_fastN_t`/`uint_fastN_t` | no distinct identity | Target-resolved aliases. **P1** |
| `intmax_t`/`uintmax_t` | fixed integers only | Target-resolved aliases. **P1** |
| `size_t` | `Size` | Existing native mapping after target validation. **P0** |
| `intptr_t`, `ptrdiff_t`, `ssize_t` | no signed pointer-width type | Add native `ISize`; preserve each C spelling. **P1** |
| `uintptr_t` | `Size` has intended width | Map to Size after target validation. **P1** |
| `off_t` | no stable equivalent | Target-resolved alias; it may be wider than a pointer. **P1** |
| `_BitInt(N)` and unsigned form | absent | Foreign arbitrary-width scalar; pass/store first, arithmetic later. **P2** |
| `nullptr_t` | Nil is not ABI-stable alone | Foreign-only scalar metadata; ordinary APIs use nullable pointers. **P2** |

`ISize` is the one recommended native addition. It is the signed counterpart
to Size and prevents portable bindings from choosing Int32 or Int64 in source.
It does not make arbitrary C typedefs interchangeable: target/layout evidence
still records the original identity.

### Floating and character types

| C type or family | Hexal today | Recommendation / priority |
| --- | --- | --- |
| `float` | `Float32` | Map when target evidence confirms binary32. **P0** |
| `double` | `Float64` | Map when target evidence confirms binary64. **P0** |
| `long double` | absent | Foreign scalar with target size/alignment; pass/store first. **P1** |
| `_Float16`, `__fp16`, bfloat forms | absent | Target-qualified foreign scalar. **P2** |
| `_Float32`, `_Float64`, `_Float128` | partial representation matches | Target-qualified aliases/scalars. **P2** |
| decimal floating types | absent | Reject by value initially; opaque/wrapper use only. **P3** |
| float/double/long-double `_Complex` | absent | Foreign complex values; add arithmetic only on demand. **P2** |
| `float_t`, `double_t` | absent | Header/target-resolved aliases. **P2** |
| `char8_t` | `Byte` representation | Target-resolved alias to Byte. **P1** |
| `wchar_t` | Rune is not ABI-compatible | Foreign scalar; width/signedness are target-specific. **P1** |
| `char16_t` | no code-unit type | Foreign alias to UInt16; never Rune. **P1** |
| `char32_t` | no unrestricted code-unit type | Foreign alias to UInt32; never silently Rune. **P1** |
| `char*`, wide/UTF pointer strings | String is incompatible | Explicit foreign pointers plus conversion/ownership contract. **P0/P1** |

### Derived, aggregate, and nominal types

| C type or construct | Hexal today | Recommendation / priority |
| --- | --- | --- |
| `const T*`, `T*`, `void*` | Ptr/MutPtr and Unknown | Existing recursive mapping, nullable unless contracted non-null. **P0** |
| pointer top-level `const` | fixed binding | Declaration metadata, not a new type. **P0** |
| `volatile` pointer/value | limited volatile scalar operations | Preserve qualifier; add checked foreign access. **P1** |
| `restrict` pointer | absent | Foreign parameter metadata, not a value type. **P1** |
| address-space pointer | absent | Target-qualified foreign pointer. **P2** |
| function type/pointer | functions and `Fun<...>` | Preserve exact ABI and nullability. **P0** |
| complete `struct` | native object is not an ABI proof | Foreign nominal record owned by its header. **P0** |
| incomplete `struct`/opaque handle | only generic Unknown | Named foreign opaque type, usable only where complete layout is unnecessary. **P0** |
| C `enum` | ADT is not a C enum | Foreign nominal enum plus qualified constants. **P0** |
| raw C `union` | tagged Hexal unions are incompatible | Unsafe foreign nominal union. **P0** |
| anonymous struct/union | absent | Stable private binding identity and promoted-field metadata. **P1** |
| packed/aligned aggregate | absent | Preserve target layout/attribute evidence. **P1** |
| integer/Bool bit-field | absent | Foreign field metadata and generated access; never addressable. **P1** |
| unnamed/zero-width bit-field | absent | Layout metadata only. **P1** |
| `T[N]` and nested arrays | `Array<T,N>`; RFC 0151 discusses `[N]T` as an unselected alternative | Preserve every fixed extent and inline shape. **P0** |
| array function parameter | Array semantics differ | Apply C parameter adjustment, then map pointer/slice. **P0** |
| incomplete array `T[]` | absent | Foreign metadata plus separately known extent. **P1** |
| flexible array member | absent | Foreign tail metadata and checked slice accessor. **P1** |
| variable-length array | absent | Do not add general VLA values; import parameter forms as pointer/slice. **P2** |
| `T a[static N]` parameter | absent | Preserve minimum-length contract and check when provable. **P1** |
| transparent `typedef` | transparent aliases | Qualified alias retaining C spelling. **P0** |
| opaque-handle `typedef` | no foreign nominal identity | Qualified opaque identity. **P0** |

Imported aggregate definitions remain owned by their C headers. The compiler
uses supplied field/layout facts for checking and emits the original C type;
it never recreates a foreign definition from guessed offsets.

### Functions, platform types, and extensions

| C type or family | Hexal today | Recommendation / priority |
| --- | --- | --- |
| callback | noncapturing functions/Fun | Direct mapping with lifetime and thread-entry contract. **P0** |
| nullable callback | Fun union Nil | Verify and preserve. **P0** |
| variadic function `...` | unsupported | Typed foreign variadic arguments with C promotions. **P1** |
| `va_list` | absent | Target-specific opaque/pass-through foreign type. **P2** |
| calling-convention-qualified function | absent | ABI metadata in function and function-pointer identity. **P0** |
| `noreturn`/`returns_twice` function | partial control-flow concepts | Preserve attributes; reject unsafe unsupported flow. **P1/P3** |
| `FILE`, `fpos_t`, `mbstate_t` | IO hides native representation | Foreign opaque/pass-through types. **P1/P2** |
| `time_t`, `clock_t`, `sig_atomic_t` | absent | Target-resolved aliases. **P1/P2** |
| `max_align_t` | absent | Foreign complete type for layout/allocation APIs. **P1** |
| POSIX `pid_t`, `uid_t`, `gid_t`, `mode_t` | absent | Qualified platform aliases. **P1** |
| POSIX thread/sync typedefs | Task/Mutex are not ABI-compatible | Opaque foreign values; never merge with native concurrency. **P2** |
| Windows integer aliases | representable but unnamed | Generated target aliases to exact Hexal scalars. **P1** |
| Windows handles | no nominal handle identities | Qualified opaque foreign handle types. **P1** |
| compiler fixed SIMD/vector | absent | Target-qualified foreign nominal value. **P2** |
| scalable SVE/RVV vector | absent | Opaque or target-only pass-through. **P3** |
| `jmp_buf`/`sigjmp_buf` | absent | Opaque only; non-local jumps do not enter safe Hexal flow. **P3** |
| arbitrary library typedef | no foreign system | Classify as alias, enum, record, union, scalar, or opaque. **P0** |

`const`, `volatile`, `restrict`, `_Atomic`, packing, alignment, symbol linkage,
visibility, and calling convention are not all standalone value types, but
they are ABI-significant type/declaration metadata and must survive binding
normalization.

### Atomics

| C type | Hexal today | Recommendation / priority |
| --- | --- | --- |
| `_Atomic(T)` | limited native `Atomic<T>` | Foreign atomic metadata; never assume native ABI identity. **P2** |
| `atomic_*` typedefs | partial conceptual match | Preserve exact header typedef and operations. **P2** |
| qualified atomic combinations | absent | Preserve qualifier layers as foreign metadata. **P2** |

Foreign atomics should normally remain behind wrapper functions. Their ABI,
lock-freedom, and representation are target/library properties.

### Required non-type ABI facts

The type inventory is unusable without these declaration facts:

| Fact | Compiler treatment | Priority |
| --- | --- | --- |
| size, alignment, field offsets, packing | trusted target evidence; never host inference | **P0** |
| symbol spelling and linkage | part of foreign declaration identity | **P0** |
| calling convention | part of function/Fun ABI identity | **P0** |
| nullability | explicit binding contract | **P0** |
| ownership, retention, deallocator | explicit binding contract | **P0** |
| pointer/count relationship | explicit slice bridge contract | **P0** |
| variadic promotions | checked at each foreign call | **P1** |
| `errno`/last-error convention | wrapper/binding metadata | **P1** |
| ABI-affecting attributes | preserved; unsupported ones fail closed | **P1** |
| conditional target declarations | resolved by binding generator for Project target | **P0** |

### Initial implementation cut

The first usable C boundary includes every P0 row:

1. target-resolved fundamental integers and exact-width scalars;
2. Bool, Float32/64, Size, pointers, nullability, and `void`;
3. foreign functions, function pointers, callbacks, and calling conventions;
4. foreign enums, complete records, opaque records, and raw unions;
5. fixed/nested arrays and C parameter adjustment;
6. transparent typedefs, constants, and external variables;
7. explicit String/C-string and pointer/count conversions; and
8. ownership, retention, deallocator, target, and layout evidence.

P1 follows for broad OS/library coverage. P2/P3 land only from a concrete
library requirement. No specialized C type becomes a native Hexal feature
solely because C can spell it.

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
- An unsafe foreign binding may expose pointer arithmetic, address conversion,
  or raw casts only through an explicit unsafe operation. Such an operation
  does not make the resulting value safe or infer ownership.
- Pointer-plus-length buffers map through RFC 0153's explicit
  `Slice<T>`/`Slice<mut T>` bridge or deliberate copying.
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
  and compiler-specific layout attributes are unsafe foreign representations.
  They are not valid checked native values, but an explicit unsafe binding may
  describe them when the selected target profile supplies the required layout
  evidence.
- Recommended C-enum representation is a qualified foreign nominal integer
  type with qualified constants. Its underlying ABI comes from target evidence;
  it is never inferred from a Hexal ADT.
- External variables require explicit foreign declarations. Thread-local
  variables, volatile globals, and C atomics require explicit target/profile
  and unsafe-boundary rules. A foreign global is never a native mutable global
  merely because it is imported.
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
- C inline assembly and compiler-specific builtins are target-qualified unsafe
  foreign operations; the core compiler does not interpret their bodies.
- `setjmp`/`longjmp`, foreign exceptions, signals, and asynchronous re-entry do
  not map to Hexal Error.

## Text and buffers

- String never converts implicitly to a C character pointer.
- A C-string borrow must be explicit, read-only, allocation-free, scoped to one
  direct foreign call, and rejected when embedded NUL would change meaning.
- Foreign code must not retain a borrowed String pointer.
- Mutable C text never receives immutable String storage.
- Binary buffers use pointer plus explicit Size and bridge explicitly to
  `Slice<Byte>` or `Slice<mut Byte>`.
- C output buffers are copied or wrapped deliberately after the call.
- The exact source spelling for call-scoped C-string borrowing remains open.

## Ownership, cleanup, and errors

- A foreign call transfers ownership only when its binding contract says so.
- The compiler performs no automatic free, retain, release, destructor, or
  allocator translation.
- Foreign allocations must be released through their matching foreign
  deallocator.
- Heap, Stash, Pool, and collection cleanup never release foreign allocations
  unless a future explicit allocator-compatibility contract permits it.
- Imported C deallocators never receive Hexal-managed storage by default.
- Foreign pointers and records follow the affine ownership rules once RFC 0110
  lands. Until then, this RFC's implementation must reject ownership claims it
  cannot represent rather than silently applying shallow-copy semantics.
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
- Methods, generics, ADTs, structural unions, String, collections,
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
- C++, Objective-C, variadic calls, arbitrary macros, or unchecked foreign
  operations in the initial implementation.
- Adding unrestricted source pointer arithmetic, address integers, unchecked
  casts, or implicit unsafe operations. Explicit unsafe foreign declarations
  and operations are in scope; their exact syntax is a readiness question.

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
4. approval of the recommended target-resolved scalar aliases, native `ISize`,
   and nominal C-enum identity;
5. trusted target/layout evidence supplied by generated bindings;
6. complete foreign-record field, union, bit-field, flexible-array, and array
   representation, including which cases remain unsafe-only;
7. trusted non-null, ownership, retention, and deallocator annotations;
8. call-scoped String-to-`const char *` borrowing syntax;
9. callback calling conventions, retention, context recovery, and foreign
   thread entry;
10. external variables, TLS, volatile values, `errno`, C atomics, inline
    assembly, and target-specific builtins;
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
- add the explicit unsafe-boundary contract and its interaction with affine
  ownership, Stashes, foreign globals, raw layouts, and target-specific code;
- remove only implemented C-interoperability items from Excluded features.
