# RFC 0039: C Interoperability - Compiler Core

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented. The compiler core implements the `Alias from c <header>`
  import reference in both forms, the pure `DiscoverCImports` helper, the
  deterministic `hexalc/h<sha256>` prepared-binding key, prepared-module
  header-identity validation, the reserved `hexalc` user-key rejection, and the
  exact Configuration Errors. It implements the `extern c from <header> do ...
  end` block and all four declaration forms, the initial ABI set (direct and
  target-resolved scalars, pointers and nullability, complete and opaque
  records, transparent aliases, constants, and globals), the program-wide
  target-qualified foreign-record identity and coalescing, the unsafe gate on
  calls and global access, the `String.c_pointer` and `Slice.pointer` bridges,
  deterministic per-module C include emission, exact-symbol lowering with
  representation-preserving boundary casts, and the exact diagnostics. It is
  covered by parser tests, pure-Go integration tests through the exported
  `Compile` API, and target-qualified tagged C23 fixtures that compile, link,
  and run under gcc, clang, and zig, including direct scalars, a complete
  record passed and returned by value, an opaque type, constants, a global, the
  String/Slice address bridges with a mutable buffer, a header-only library,
  and a library-shaped binding surface.
  One Validation item remains open and is not a compiler-core gap: the
  `hex_cvar_` automatic local-name escaping is the prepared-module normalizer's
  responsibility and is owned by RFC 0193.
- Created: 2026-08-11
- Updated: 2026-09-15
- Scope: add direct C-header import syntax, the prepared-source protocol, and
  the normalized typed foreign-declaration layer needed for Hexal code to call
  ordinary C libraries while preserving the in-memory compiler boundary
- Depends on: closed RFC 0155 (`unsafe do ... end`) and the current module,
  pointer, Slice, String, target-profile, and generated-artifact contracts in
  `docs/reference.md`
- Coordinates with: RFC 0156 (raw pointer operations), RFC 0186 (`std` module
  boundary), RFC 0192 (command-line C build inputs), RFC 0193 (automatic
  C-header binding generation), closed RFC 0052 (installed Zig C23 backend),
  and closed ADR 0055 (filesystem/build driver)
- Does not add: filesystem access or a C parser/preprocessor to the core
  compiler, automatic ownership, C project configuration, or the advanced
  foreign surfaces collected in deferred RFC 0191

## Author roadmap

Exercise the completed boundary in this order:

1. one C function and one header;
2. a library using structs, constants, opaque pointers, and mutable buffers;
3. a header-only library; and
4. raylib as the first complete external project.

Each step may receive a driver-facing child specification. Foreign source may use an older C dialect: the driver chooses the dialect used to compile that source. Hexal-generated translation units remain C23. A header included by both must be accepted in the relevant compilation modes.

## Problem

The backend can already compile Hexal-generated C23 and link a compatible object, but Hexal source cannot describe the object's functions or types. Importing raw headers in the core compiler would require a second C frontend, host filesystem access, preprocessing, target probing, and build-system policy. Those concerns do not belong in the forward-only string-in/string-out compiler.

The ordinary source form imports a header directly. RFC 0193 owns asking the
selected C frontend to describe the header and preparing an ordinary logical
`.hex` binding-module string. The core compiler consumes that prepared binding
without reading the header. A user may still write the same normalized binding
module by hand when automatic import cannot express or should deliberately
override the foreign contract.

## Goals

- Make `Alias from c <header>` the low-ceremony path for ordinary C APIs.
- Reuse normal module qualification and identity for the prepared binding.
- Preserve exact C type and symbol spellings at the ABI boundary.
- Let the selected C compiler own C layout and calling-ABI details.
- Type-check every fact represented by the binding before generation.
- Mark every operation that executes or directly accesses foreign state as unsafe without turning `unsafe` into a type or function effect.
- Lower calls and values directly, with no allocation or wrapper when their representations agree.
- Keep ownership, cleanup, nullability, and String conversion explicit.
- Fail before generation when the compiler cannot represent a declaration.
- Keep the initial surface sufficient for ordinary libraries and basic raylib use while deferring specialized C machinery.
- Keep handwritten foreign declarations as an explicit fallback, not required
  duplication for representable headers.

## Compiler boundary

The core API remains:

```text
Compile(sources map[string]string,
        entrypoint string,
        project Project) CompilationResult
```

- `sources` contains complete Hexal source strings, including handwritten or
  driver-generated binding modules at deterministic reserved logical keys.
- `Project` supplies the already-selected target profile; it contains no host paths, object files, libraries, or C compiler process.
- Every compilation containing a C import or handwritten foreign declaration
  requires a nonempty qualified target profile. Host-default ABI inference is
  rejected because `long`, plain `char`, layout, and calling ABI are
  target-dependent.
- `CompilationResult.Files` contains only generated text.
- The compiler never reads a header, searches an include directory, invokes a preprocessor, inspects an object, or links a program.
- The driver owns C files, headers, include roots, defines, source dialects, objects, archives, libraries, frameworks, build systems, and final linking.
- `DiscoverCImports(sources, entrypoint)` is a pure compiler helper that reuses
  normal parsing and module reachability to return reachable C-header requests
  without touching the filesystem. The driver prepares those requests, adds
  their binding strings to a copy of `sources`, then calls `Compile` normally.
- A C import maps to the reserved legal logical key
  `hexalc/h<sha256(target NUL header-form NUL header-payload)>.hex`. The leading
  `h` makes the digest component a Hexal identifier. The key
  is deterministic within every target build; the compiler verifies that the
  prepared module names the requested header and reports Configuration Error
  `prepared C binding missing for <header>` when the source-map entry is absent
  or mismatched. A digest collision between different header identities is a
  Configuration Error, never an alias.
- `hexalc` is an internal prepared-binding namespace. User source discovery
  rejects that top-level component, so prepared bindings cannot collide with a
  project file while `Compile` retains its one `sources` map.

No second normalized manifest is introduced. The binding module is the one compiler/driver interchange format.

## Source syntax

The ordinary form extends the leading import block:

```hexal
import
    Adder from c "adder.h",
    CMath from c <math.h>
end
```

```ebnf
import-entry = identifier , "from"
               , ( module-path-literal
                 | "c" , c-header-literal ) ;
```

- `c` is contextual immediately after `from`.
- A C import is top-level and leading because it is an ordinary import entry.
- The alias exposes supported declarations by their exact C identifier when it
  is a usable Hexal name: `Adder.adder_add(...)` calls `adder_add`. Otherwise
  the deterministic `hex_cvar_` local name still lowers to the exact C name.
- The prepared binding automatically exports every supported declaration made
  visible by preprocessing the requested header, including its transitive
  include surface, and coalesces compatible redeclarations by canonical C
  identity. Compiler-predefined declarations with no owning header are omitted.
- Two equal header identities share one prepared binding and one module
  identity regardless of local aliases. System and quoted forms are distinct.
- Calls and foreign-global accesses retain the unsafe rules below.
- Automatic import keeps an exact C identifier when it is a legal,
  non-protected, non-colliding Hexal identifier. Otherwise it derives a local
  name with the `hex_cvar_` prefix under the rules below. A native wrapper
  supplies an ergonomic name; a handwritten binding may use `as` when exact
  contract curation is required.
- A handwritten binding is an ordinary `.hex` module imported through the
  normal module form. It replaces one direct C import alias; automatic and
  handwritten declarations are never implicitly merged.

### Normalized and handwritten binding form

Foreign blocks appear after the optional import block and before every ordinary top-level item. A module may contain multiple foreign blocks, then native wrappers, then its optional final export block.

```hexal
extern c from <raylib.h> do
    type TraceLevel is Int32

    type Vector2 is struct
        mut x: Float32,
        mut y: Float32,
    end

    type Window as "struct Window" is opaque

    fun init_window as "InitWindow"(
        width: Int32 as "int",
        height: Int32 as "int",
        title: Ptr<Byte> | Nil as "const char *",
    )
    fun window_should_close as "WindowShouldClose"(): Bool
    fun close_window as "CloseWindow"()

    constant log_info as "LOG_INFO": TraceLevel
end

fun open_window(width: Int32, height: Int32, title: String) do
    unsafe do
        init_window(width, height, title.c_pointer())
    end
end

export
    Vector2,
    open_window
end
```

Both header forms are supported:

```hexal
extern c from <stdio.h> do
    fun c_puts as "puts"(
        text: Ptr<Byte> | Nil as "const char *",
    ): Int32 as "int"
end

extern c from "vendor/widget.h" do
    type Widget as "struct widget" is opaque
end
```

They emit `#include <stdio.h>` and `#include "vendor/widget.h"` respectively.

Every imported header is included from generated C23. A header may describe an
implementation compiled under C11 or C17, but the header itself must also be
accepted in C23 mode. A legacy header that conflicts with C23 requires a small
user-supplied compatibility wrapper header; Hexal does not rewrite third-party
headers.

### Grammar

```ebnf
program = lexical-separation , [ import-block ] , { extern-block }
          , { top-level-item } , [ export-block ] ;

extern-block = "extern" , "c" , "from" , c-header-literal , "do"
               , { extern-declaration } , "end" ;
c-header-literal = c-system-header | c-quoted-header ;
c-system-header = ? nonempty `<...>` payload valid under the header rules ? ;
c-quoted-header = ? nonempty quoted payload valid under the header rules ? ;

extern-declaration = extern-type | extern-function
                     | extern-constant | extern-global ;
extern-type = "type" , identifier , "is" , alias-target
              | "type" , identifier , [ "as" , c-record-name-literal ]
                , "is" , ( "opaque" | extern-struct-definition ) ;
extern-struct-definition = "struct" , [ extern-member
                           , { "," , extern-member } , [ "," ] ] , "end" ;
extern-member = [ "mut" ] , identifier , [ "as" , c-identifier-literal ]
                , ":" , type-expression ;
extern-function = "fun" , identifier , [ "as" , c-identifier-literal ]
                  , extern-signature ;
extern-signature = "(" , [ extern-parameter-list ] , ")"
                   , [ ":" , type-expression , [ "as" , c-type-literal ] ] ;
extern-parameter-list = extern-parameter , { "," , extern-parameter } ;
extern-parameter = identifier , ":" , type-expression
                   , [ "as" , c-type-literal ] ;
extern-constant = "constant" , identifier , [ "as" , c-identifier-literal ]
                  , ":" , type-expression ;
extern-global = "global" , [ "mut" ] , identifier
                , [ "as" , c-identifier-literal ]
                , ":" , type-expression ;

c-record-name-literal = ? quoted C typedef or tag spelling ? ;
c-type-literal = ? quoted restricted scalar or pointer C type spelling ? ;
c-identifier-literal = ? quoted C identifier ? ;
```

- `extern`, `c`, `constant`, `global`, and `opaque` are contextual inside this grammar; no new word is globally reserved except `extern` at statement start.
- A foreign block is top-level only. It cannot follow a native declaration or executable statement, appear in a function, or appear after `export`.
- Foreign declarations have no bodies, initializers, generic parameters, methods, or nested declarations.
- A local foreign name defaults to the same C spelling. `as` supplies the exact C spelling when they differ.
- An alias declaration inside the block is an ordinary transparent Hexal alias.
  It carries no C spelling and adds no foreign type family. Generated enum and
  platform aliases use this form.
- A complete or opaque record's optional `as` contains one C typedef name or
  one tag spelling (`struct X` or `union X`). It cannot contain pointers,
  brackets, parentheses, attributes, preprocessor tokens, comments, newlines,
  or arbitrary C expressions.
- A foreign parameter or result may add `as "C type"` when the C type differs
  from its Hexal checking type. The accepted subset is qualified fundamental,
  typedef, tag, and object-pointer types; arrays, function declarators,
  attributes, expressions, and abstract declarator nesting are rejected. The
  checker accepts only compiler-known fundamental, exact-width, tag/record, and
  recursively qualified pointer spellings whose ABI mapping follows directly
  from the selected target. An arbitrary library typedef spelling is rejected
  with `C spelling <spelling> cannot be proven in a handwritten binding; use an
  automatic C import or expose a C wrapper`. This is not a general C declarator
  parser.
- Function, constant, global, and field C names are one ordinary C identifier. Assembly labels and decorated linker names are deferred.
- A system-header payload excludes whitespace, quotes, `<`, `>`, backslash, and line breaks. A quoted-header payload excludes quotes, backslash, and line breaks. Empty, absolute, and parent-walking header names are rejected. Include search and existence remain driver concerns.
- A binding uses the normal final `export` block. Foreign declarations are
  private unless named there; `export` never changes C linkage. An automatic
  binding with no supported exported declaration omits the export block
  entirely; the language never emits an empty `export ... end` block.

## Foreign declaration model

The checked tree adds explicit identities for a foreign header requirement,
complete or incomplete foreign record, foreign function, foreign constant, and
fixed or writable foreign global. Transparent aliases inside the block reuse
the existing alias identity.

Functions, constants, and globals retain their defining binding module.
Foreign records instead have one program-wide identity keyed by the qualified
target and canonical C record identity: tag namespace plus exact tag spelling,
or the canonical typedef spelling for an anonymous typedef-owned record.
Therefore the same `struct Common` reached through two headers is one Hexal
type. An incomplete declaration and a compatible complete definition coalesce
to the complete definition. Multiple incomplete declarations coalesce. Two
complete definitions must agree on kind, fields, qualification, and mapped ABI
facts or report a Type Error. A struct/union kind mismatch also reports a Type
Error. Renaming an import alias changes no identity. Two declarations may name
the same C symbol when their mapped Hexal contracts agree; every contributing
header remains included and the C compiler remains authoritative for the
original declarations.

### Automatic local names

- A legal non-protected C ordinary identifier keeps its exact name when that
  name is unambiguous in the prepared Hexal module.
- An otherwise unspellable, protected, or colliding ordinary identifier maps
  to `hex_cvar_<C-name>`.
- A C tag with no usable typedef maps to `hex_cvar_struct_<tag>` or
  `hex_cvar_union_<tag>`. Thus C's `struct stat` and function `stat` can coexist
  as `hex_cvar_struct_stat` and `stat`.
- Leading underscores remain inside the prefixed tail, so `_internal` becomes
  `hex_cvar__internal`.
- All exact names are reserved before escaped names. If an escaped base still
  collides, canonical C identity order keeps the first base and appends `_0`,
  `_1`, and so on to later names.
- The exact original C spelling remains in checked metadata and generated C;
  `hex_cvar_` changes only the Hexal member name.

The checked declaration retains two different facts:

1. the Hexal type used for source checking; and
2. the exact C spelling used at the ABI boundary.

The generator uses C spelling only for foreign parameter/result conversion and
foreign record, symbol, constant, and global positions; ordinary Hexal storage
keeps its normal spelling.

### Automatic-import normalization

- The prepared module is ordinary RFC 0039 source. The checker and generator
  have no separate automatic-binding representation or trusted fast path.
- The driver emits declarations only from the supported initial ABI set in this
  RFC. Unrelated unsupported declarations in a real header do not make the
  entire import fail; they are absent from the prepared module. Attempting to
  use one receives the automatic-C-import binding-guidance diagnostic. A
  handwritten binding may expose it only when RFC 0039 can represent its ABI;
  otherwise the user needs a compatible C wrapper or the later
  advanced-interop surface.
- A supported declaration whose own signature or layout depends on an
  unsupported type is omitted as a unit; the driver never emits a partial or
  guessed contract.
- Functions, complete and opaque records, typedefs, enums/enumerators, and
  supported globals visible through the requested header are normalized.
  Required transitive record and typedef dependencies are included only to
  close those exported interfaces.
- Object-like C preprocessor macros are not present in the selected frontend's
  declaration AST and are not automatically imported in this version. The
  handwritten `constant` form remains their explicit bridge.
- Internal-linkage objects and functions are omitted, except callable
  header-defined `static inline` functions whose definition is supplied by the
  included header. Variadic or otherwise unsupported functions are omitted.
- Pointer nullability defaults, qualification, integer mapping, record
  completeness, foreign safety, and exact C spelling are normalized by the
  rules below. Automatic import never guesses ownership or non-nullness.
- Declaration selection and normalized source emission are deterministic under
  arbitrary frontend traversal order. Declarations are ordered by source
  location, kind, and exact C name after dependency ordering.

## Initial ABI type set

### Direct and target-resolved scalars

| C family | Hexal checking type | Rule |
| --- | --- | --- |
| `void` result | no result | Never `Nil` |
| C23 `bool` / `_Bool` | `Bool` | Direct |
| exact-width signed/unsigned integers | matching Hexal integer | Direct |
| `float`, `double` | `Float32`, `Float64` | Qualified targets guarantee binary32/binary64 |
| `size_t` | `Size` | Direct |
| `char *`, `const char *` buffers | mutable/read-only `Ptr<Byte> | Nil` | Signature records the C spelling and emits one boundary cast |
| fundamental integer and `char` types | fixed Hexal integer selected by the binding generator | Target-resolved |
| C enum type | generated transparent alias to its resolved integer type | Original declaration remains in the included header |
| `void *`, `const void *` | mutable/read-only `Ptr<Unknown> | Nil` | Direct |

- No `ISize`, C-integer family, C-enum family, or platform typedef becomes a protected native Hexal type.
- A generated binding resolves `short`, `int`, `long`, `long long`, plain `char`, pointer-width integers, and library typedefs for the selected target. C `long` maps to `Int32` on Windows LLP64 and `Int64` on an LP64 target; an `as` clause retains `long` when exact boundary spelling is required.
- C integer promotion does not enter Hexal expression typing. Arguments satisfy the declared Hexal checking type before the call.
- A C enum remains open like its underlying C integer. It is not a Hexal ADT, adds no exhaustiveness guarantee, and accepts any value of the resolved integer type. Enumerator and object-like macro names are foreign constants.
- Long double, decimal/binary extension floats, complex values, `_BitInt`, SIMD, atomics, and non-default calling conventions are rejected in this version.

### Pointers and nullability

- C pointer syntax does not prove non-null. Automatic bindings always use
  `Ptr<T> | Nil` or `Ptr<mut T> | Nil`; this version does not consume
  nullability annotations as trusted proof.
- A handwritten binding may write a bare pointer and thereby assert non-null. The assertion is part of its unsafe foreign contract.
- `const T *` maps to `Ptr<T>`; `T *` maps to `Ptr<mut T>`. Pointer layers map recursively and retain qualification at each layer.
- Passing a bare pointer to a nullable parameter needs no representation change. A nullable result must be narrowed before dereference.
- A pointer imports no ownership. Copying aliases the same storage. The user calls the foreign library's matching release function explicitly.
- RFC 0156 owns pointer arithmetic, indexing, and casts. Foreign declarations do not implicitly grant those operations.

### Complete and opaque records

- `type X ... is struct ... end` declares a nominal foreign record. The named C header, not generated Hexal C, defines its layout.
- The binding lists every named field in order. Each ordinary non-const field
  is `mut`; a const-qualified field is fixed. Volatile or atomic fields are
  unsupported. Omitted, anonymous, bit-field, union, flexible-array, or other
  unsupported fields make the record ineligible for complete import in this
  version.
- The selected C compiler owns size, alignment, padding, parameter passing, and result passing. Hexal never copies numeric offsets from the host or recreates the C definition.
- Construction, copying, field access, `size_of`, and `align_of` lower using the original C type and field names. Normal Hexal initialization and mutability rules apply.
- `type X ... is opaque` names an incomplete C type. It may occur only behind a pointer. It cannot be constructed, copied by value, sized, aligned, allocated as a value, dereferenced, or accessed by field.
- Automatic import may expose a complete C record with unsupported fields as
  opaque only to satisfy pointer-only interfaces. A declaration that passes or
  returns that record by value is omitted whole. A handwritten complete record
  with an unsupported or missing field is rejected. The compiler never guesses
  a partial layout.
- Foreign records have no Hexal equality, ordering, printing, or Dict-key
  contract in this version. These operations are rejected even when every
  visible field would otherwise support them; C record compatibility does not
  imply those Hexal semantics.

### Constants and globals

- A foreign constant is a typed, non-addressable scalar expression whose C
  spelling is an enumerator or object-like macro identifier. The included
  header supplies its definition. The declaration asserts that the C frontend
  classifies it as a side-effect-free constant expression representable by the
  stated Hexal scalar or enum alias; other macros are not constants in this
  version.
- The compiler does not evaluate C preprocessor expressions. A constant may be used wherever an ordinary expression of its Hexal checking type is accepted, but it is not a Hexal compile-time literal.
- A foreign global names C storage. `global name` permits reads only; `global mut name` permits reads and writes through Hexal.
- The binding may expose a writable C object as fixed, but never expose a const C object as writable. The future binding generator validates that direction.
- Reading or writing a foreign global requires `unsafe do ... end`; synchronization and lifetime are outside Hexal's local analysis.
- Thread-local variables and volatile or atomic globals are deferred.
- An automatically imported non-const global is `global mut`; a const global
  is fixed. A handwritten binding may expose a writable C object as fixed, but
  never a const C object as mutable.

## Foreign calls and unsafe

Every direct foreign function call requires lexical permission from RFC 0155:

```hexal
unsafe do
    init_window(800, 450, title.c_pointer())
end
```

- The checker first validates the callee, arguments, result, nullability, and ABI compatibility, then applies the unsafe gate. `unsafe` never suppresses an ordinary diagnostic.
- The requirement belongs to the call expression, including calls written in a deferred action or through a native wrapper. It is not inherited by callers.
- A normal Hexal wrapper may contain the unsafe block and expose an ordinary checked API. The wrapper author owns the foreign preconditions.
- C termination, undefined behavior, memory corruption, long jumps, data races, and asynchronous re-entry cannot be translated into Hexal Error.
- C status returns, null results, and out-parameters retain their declared shapes. The compiler never invents `Error`; a native wrapper may translate a foreign convention into `T | Error`.

## Text and buffer bridge

The first version adds these unsafe-capable operations:

```text
String.c_pointer() -> Ptr<Byte>
Slice<T>.pointer() -> Ptr<T> | Nil
Slice<mut T>.pointer() -> Ptr<mut T> | Nil
```

- Each operation requires `unsafe do ... end` and exposes the address already present in the value; it allocates and copies nothing.
- `String.c_pointer()` points at immutable UTF-8 bytes followed by the String allocation's existing terminal zero. An embedded zero ends a C string early. The operation does not scan, reject, or rewrite the String.
- The returned String pointer remains valid only while the String allocation is live. Foreign code must not mutate or retain it beyond that lifetime.
- A Slice pointer shares the Slice's lifetime and may be Nil exactly when the Slice has no backing address. Length is never implicit; pass `slice.length()` separately when the C API requires it.
- A foreign `char *` parameter is checked as a Byte pointer and lowered with the exact C pointer spelling recorded on that parameter. This representation-preserving boundary cast does not make plain C `char` a Hexal Byte scalar.
- Mutable C output never receives `String.c_pointer()`. It receives a pointer from `Slice<mut Byte>`, a writable Array/List region, or explicit foreign allocation.
- These operations may return pointers that outlive the source value in syntax; correctness is the programmer's unsafe assertion. No lifetime or retention annotation is added.

## Advanced interop handoff

Deferred RFC 0191 owns callbacks into Hexal, function-pointer values, variadic
calls, raw C unions, bit-fields, flexible-array members, C atomics, extended
numeric types, native error-convention translation, non-default calling
conventions, dynamic libraries, and C exports. None is an implied extension of
this RFC's implementation. Each returns through its own focused child RFC after
the foundation is implemented and a concrete library demonstrates demand.

## Lowering and artifact ownership

- A module that uses a foreign declaration records its defining header as a deterministic dependency. Its generated module header emits each required include once in first checked-use order, after `hexal.h` and compiler-owned component headers but before declarations that name the foreign type.
- The module C file continues to include only its own generated header.
- Foreign binding modules retain ordinary module C/header artifacts; no foreign function or type definition is emitted into them.
- Imported calls use the exact C identifier. No forwarding wrapper is generated solely to rename a function.
- Arguments and results with identical representation pass directly. The generator emits a direct cast only where a checked foreign signature records a different but representation-compatible C spelling, such as Byte storage passed as `const char *`.
- Foreign records use the original C type and field spellings. Generated C never emits their `struct`, `enum`, or typedef definitions.
- Foreign constants lower to their C identifier. Foreign globals lower to the original C object identifier.
- Nullable pointers use C23 `nullptr`; no tagged wrapper is introduced.
- Every generated expression retains `#line` mapping to the Hexal call or access. Header contents are not source-mapped or copied.
- Unsupported foreign declarations fail during checking. The generator has no placeholder or best-effort path.

## Diagnostics

The earliest proving phase owns these exact forms:

```text
Syntax Error: extern blocks must precede ordinary top-level items
Syntax Error: foreign declaration requires a C header
Syntax Error: invalid C header name <name>
Syntax Error: invalid C spelling <spelling>
Configuration Error: C interoperability requires a qualified target
Type Error: unsupported foreign declaration <declaration>
Type Error: C spelling <spelling> cannot be proven in a handwritten binding; use an automatic C import or expose a C wrapper
Type Error: foreign type <type> is incomplete in <position>
Type Error: foreign call <name> requires an unsafe do ... end block
Type Error: foreign global <name> requires an unsafe do ... end block
Type Error: conflicting foreign declarations for C symbol <symbol>
Type Error: <type> has no supported C ABI mapping for target <target>
Name Error: C import <header> has no automatically imported declaration <name>; check the C name, use a handwritten binding, or expose a C wrapper
```

Ordinary privacy, export-closure, argument, result, mutability, nullability,
name-resolution, and duplicate-declaration diagnostics retain ownership. The
last diagnostic replaces only a missing qualified member lookup through a
direct automatic C-import alias; it does not claim that the physical header
contains that name. Missing headers, include search, C compilation, C project,
object, archive, library, framework, and linker failures are driver
diagnostics.

## Driver handoff

RFC 0193 owns automatic binding preparation. It may:

1. call the pure `DiscoverCImports` helper over the supplied source strings;
2. read and preprocess each reachable requested header using the installed
   Zig/Clang frontend with the selected target, include roots, and definitions;
3. normalize supported declarations into the syntax above for that target;
4. add each binding module string under its deterministic reserved key in a
   copied `sources` map;
5. return the augmented source map to the ordinary compiler call.

RFC 0192 separately owns compiling foreign C sources, providing include roots
and definitions, accepting objects/archives/libraries, materializing generated
artifacts, and linking the complete program.

Importing a C project, object, or library adds no object representation to the
core compiler. The binding module supplies types and symbols; the driver
supplies their implementation at link time. RFC 0192 initially receives every
physical build input through `hexal build` options; a project manifest remains
future work.

## Required sweep

Inventory and reconcile:

- program grammar, keyword handling, top-level ordering, recovery, and export closure;
- pure reachable C-import discovery, deterministic reserved binding keys, and
  prepared-module validation;
- parsed and checked declaration kinds and fail-closed traversal;
- module identity, import aliases, visibility, duplicate names, and defining-module ownership;
- foreign signature C-spelling preservation and representation checks;
- target-profile scalar resolution and record eligibility;
- pointer qualification, nullability, opaque-type placement, and foreign-state access;
- RFC 0155's shared unsafe-capability predicate;
- String/Slice raw-address extraction and every existing storage-lifetime rule;
- generator dependency discovery, include ordering, declaration spelling, source mapping, and deterministic artifact output;
- pure-Go binding tests, integration tests, tagged generated-C fixtures, snippets, and manifest entries; and
- current excluded-feature and C23 contracts in `docs/reference.md` after explicit approval.
- RFC 0186's import-form table and grammar so `from c` is the explicit third
  import source beside user and `std` modules.

Do not add a C parser, preprocessor, ownership checker, C package manager, or
any surface owned by deferred RFC 0191 to the core compiler. RFC 0193 owns
frontend invocation and normalization; RFC 0192 owns physical build inputs.

## Detailed implementation plan

### Phase 0: prerequisites and baselines

1. Land RFC 0155 and verify its lexical unsafe context and shared gate.
2. Record parser, module, pointer, String, Slice, generated-include, diagnostic, snippet-manifest, and tagged-C baselines.
3. Add minimal checked-in C headers for external validation; do not introduce a filesystem read in the compiler.

### Phase 1: syntax and parsed declarations

1. Extend an import entry with contextual `from c <header>` and retain its
   alias, header form, payload, and source location.
2. Add pure reachable C-import discovery using the ordinary parser and module
   graph; do not duplicate import scanning in the driver.
3. Define and test the reserved `hexalc/h<digest>.hex` binding-key derivation,
   user-key exclusion, equal-request deduplication,
   collision rejection, and missing/mismatched prepared-binding diagnostics.
4. Add contextual tokens and parse zero or more handwritten foreign blocks in
   the one legal top-level region.
5. Parse the four declaration forms and restricted header/C-name literals.
6. Reuse native type expressions, signatures, members, and final export blocks.
7. Add explicit AST nodes for every foreign declaration and reject bodies,
   generics, methods, initializers, misplaced blocks, and unsupported spellings.
8. Add parser recovery at the foreign block's `end` without accepting a partial
   declaration.

### Phase 2: types, modules, and ABI checking

1. Add module-owned foreign function, constant, and global identities plus the
   program-wide target-qualified foreign-record registry; retain header, Hexal
   type, canonical C identity, and required C spelling.
2. Preserve checked parameter/result C spelling through call resolution.
3. Resolve target-dependent scalar representations from `Project.Target`; never inspect the host.
4. Reuse native module privacy, export closure, and alias qualification; do not
   reuse module-owned nominal identity for a foreign record.
5. Check pointer layers, nullable defaults, complete/opaque placement, full record members, constant/global types, and duplicate C-symbol compatibility.
6. Check foreign calls and global access normally, then require lexical unsafe.
7. Add checked identities for the three raw-address bridge operations and route them through the same unsafe gate.

### Phase 3: generation

1. Discover foreign header demand from checked declarations used by each generated module.
2. Emit deterministic system/quoted includes before their first type use.
3. Render original C type, field, function, constant, and global spellings only at recorded ABI positions.
4. Lower calls and accesses directly; add only representation-preserving boundary casts and no forwarding helper.
5. Lower String/Slice address extraction from their existing representations with no allocation or runtime component.
6. Keep every new generator dispatch fail-closed and retain source mapping.

### Phase 4: conformance

1. Implement every Validation item in focused parser/checker tests and exported-API integration tests.
2. Add tagged C23 fixtures for a minimal C function, scalar aliases, a complete record passed and returned by value, an opaque pointer, constants, globals, String input, and mutable buffers.
3. Add a header-only fixture and a raylib-shaped fixture; the first RFC does not require the external raylib dependency itself.
4. Run ordinary and tagged gates, review generated text, and update only intentional snippet-manifest entries.
5. Synchronize `docs/reference.md` only after behavior stabilizes and with explicit user approval; then update status and close.
6. Rebuild and restart `hexal play` before handoff.

## Validation

This list is exhaustive.

### Syntax and modules

- Quoted and system `Alias from c <header>` imports resolve a prepared binding
  under the deterministic reserved key and expose exact or deterministically
  `hex_cvar_`-escaped names through the local alias.
- Equal header identities imported under different aliases share one prepared
  module identity; quoted and system forms remain distinct.
- Prepared keys use `hexalc/h<digest>.hex`; a user module under `hexalc` is
  rejected and a digest beginning with a decimal digit remains legal after the
  mandatory `h`.
- The selected target participates in prepared module identity because one C
  header may normalize platform integers and conditional declarations
  differently on different targets.
- Missing, mismatched, or colliding prepared bindings fail with the exact
  Configuration Error before checking ordinary declarations.
- A C import or handwritten foreign block with an empty or unqualified target
  fails with Configuration Error before foreign declaration checking.
- `DiscoverCImports` returns only reachable requests in deterministic module
  order and performs no filesystem or process operation.
- Quoted and system header forms parse and emit their exact include form.
- A legacy foreign source may use C11/C17 while its imported header is accepted
  independently from a generated C23 translation unit; an incompatible header
  requires a compatibility wrapper.
- Empty, absolute, parent-walking, escaped, multiline, or delimiter-breaking headers fail with the exact syntax diagnostic.
- Foreign blocks work only after imports and before every ordinary top-level item; nested, late, and post-export forms fail with the exact ordering diagnostic.
- Multiple blocks in one module work; identical header demand is emitted once.
- Every declaration is private by default and uses the normal final export block. Qualified import aliases preserve defining identity.
- An automatic binding with no supported declarations contains no empty export
  block and remains a valid module.
- A missing member through an automatic C-import alias reports the exact
  binding-guidance diagnostic; an ordinary Hexal module retains its ordinary
  unknown-export diagnostic.
- Replacing the direct import with an ordinary import of a handwritten binding
  permits explicit renaming and curation but cannot bypass an unsupported ABI
  diagnostic.
- Unsupported automatic declarations and unprovable handwritten C spellings
  diagnose the valid next action: add a representable handwritten binding, use
  automatic import, or expose a C wrapper as appropriate.
- Generics, methods, bodies, initializers, and unsupported declaration kinds are rejected rather than ignored.

### Types and ABI

- Every direct scalar row maps in argument, result, constant, global, and record field positions.
- The same compatible foreign record reached through two headers has one
  target-qualified identity and crosses either API without a conversion;
  incompatible redeclarations fail before generation.
- Exact usable C names remain unchanged. Keywords, protected names,
  leading-underscore names, tag/ordinary namespace collisions, and collisions
  with an existing escaped name receive deterministic `hex_cvar_` spellings
  while generated C retains the exact original spelling.
- Target-resolved `char`, `short`, `int`, `long`, `long long`, pointer-width integer, enum, and typedef inputs resolve to the correct Hexal integer on each qualified target; an explicit signature `as` spelling is checked and retained.
- A handwritten arbitrary library typedef in a parameter/result `as` spelling
  fails with the exact diagnostic directing the user to automatic import or a
  C wrapper; compiler-known fundamental, record/tag, and pointer spellings pass.
- C `void` produces no result and never `Nil`; `void *` maps through Unknown.
- Pointer qualification is preserved recursively. Nullable pointers require ordinary narrowing; a trusted bare pointer remains bare.
- A complete scalar/pointer record constructs, copies, accesses fields, passes by value, returns by value, and uses C-owned size/alignment without emitting a duplicate definition.
- An opaque type succeeds behind pointers and fails in every forbidden value, layout, allocation, dereference, and member-access position with the exact incomplete-type diagnostic.
- Unsupported arrays-in-records, unions, bit-fields, flexible members, atomics, extended scalars, calling conventions, and incomplete layouts fail closed.
- Compatible duplicate C declarations are accepted once; incompatible ones report the exact ABI conflict.

### Calls, constants, globals, and bridges

- A valid direct foreign call succeeds inside unsafe, fails outside with the exact diagnostic, evaluates arguments once in source order, and lowers to the original symbol without a wrapper.
- Invalid arguments, results, nullability, or names retain their earlier ordinary diagnostics inside unsafe.
- Foreign constants are readable without unsafe, lower to their C identifiers, and are non-addressable and non-assignable.
- Fixed globals permit reads only; writable globals permit reads and writes. Every global access requires unsafe and lowers to the original object.
- `String.c_pointer` preserves the current UTF-8 bytes and terminal zero, makes no allocation, and requires unsafe. An embedded zero is preserved.
- Both Slice pointer forms preserve access mode, return Nil exactly for a Slice without a backing address, allocate nothing, and require unsafe.
- Mutable C output rejects a String pointer and a read-only Slice pointer.
- Foreign records reject equality, ordering, printing, and Dict-key use.
- A char-pointer signature receives the explicit Byte pointer through one direct boundary cast; no general implicit pointer conversion is introduced.

### Artifacts and boundaries

- A driver-generated normalized module uses the same parser, checker, and
  generator path as a handwritten module. Under the same logical binding
  identity, equivalent declarations produce identical includes, calls, and C
  ABI spellings; a separately named handwritten module retains its own ordinary
  nominal module identity.
- Foreign headers appear once, deterministically, before every emitted C use and never when no checked declaration requires them.
- Binding modules emit no duplicate C type/function definition.
- `Compile` performs no filesystem or process operation and remains deterministic under randomized source and map insertion order.
- Missing headers and unresolved symbols do not become compiler diagnostics; tagged driver/toolchain validation owns them.
- Minimal C, record/opaque, header-only, and raylib-shaped fixtures compile, link, and run through the tagged external gate.
- Existing snippet hashes do not move; new interop snippets add only their own artifact entries.
- Ordinary and tagged C23 suites pass.

## Settled decisions

- **Boundary:** one ordinary binding-module string; the driver may generate it,
  while the compiler has no raw-header parser and no second manifest.
- **Ordinary syntax:** `Alias from c <header>` in the leading import block.
- **Manual syntax:** grouped `extern c from <...> do ... end` declarations with
  normal final exports remain the curation and automatic-import escape hatch.
- **Selection:** one alias uses either a prepared automatic module or an
  ordinary handwritten binding module; the compiler performs no implicit merge.
- **Safety:** calls and foreign-global access require lexical unsafe; constants and local foreign-record operations do not.
- **Ownership:** raw pointers are non-owning and cleanup is manual. No affine, retain/release, allocator, or destructor annotation is added.
- **Nullability:** C pointers default nullable; a bare pointer is an explicit binding assertion.
- **Layout:** the C compiler owns it. Hexal stores field/type facts but no copied host offsets.
- **Enums:** transparent resolved integer aliases plus constants, not ADTs or a new native enum family.
- **C integer names:** binding-local aliases; no native `ISize` or global C type catalog.
- **Text:** explicit zero-copy raw addresses; no implicit String conversion, hidden scan, allocation, or `CString` type.
- **Initial functions:** named, non-variadic, default-C-ABI calls only.
- **Advanced interop:** deferred RFC 0191 owns every excluded advanced surface;
  this implementation does not partially introduce one.
- **Driver:** source dialects, headers, projects, objects, archives, libraries, frameworks, and linking remain outside the compiler.

## Open questions

None.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation updates the grammar, modules, pointers/Slices/String, unsafe consumers, generated artifacts, C23 lowering, diagnostics, and excluded-feature list only after the implementation stabilizes and with explicit user approval.
