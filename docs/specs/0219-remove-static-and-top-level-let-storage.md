# RFC 0219: Remove `static`; Define Top-Level `let` Storage

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; blocked on RFC 0218, not scheduled
- Created: 2026-09-18
- Updated: 2026-09-19
- Scope: remove the `static` keyword without adding a replacement keyword;
  classify ordinary top-level `let` declarations by module role
- Depends on: RFC 0218's final `let ... =` declaration grammar
- Coordinates with: RFC 0220 entry-environment capture, module imports and
  exports, generated C storage, `docs/reference.md`, and the checked root
  `GRAMMAR.ebnf`
- Does not add: `const`, `var`, `global`, module initialization/finalization,
  mutable imported-module state, implicit cleanup, or a source-level `main`

## Summary

Hexal removes `static` and adds no replacement keyword. `let` remains the one
binding introducer; `mut` remains the one mutability marker.

At the selected entry module's root, `let` and `let mut` declare entry-script
bindings with runtime, source-order initialization:

```hexal
let heap = Heap()
defer heap.free()

let mut count: Int32 = 0
work(heap, @count)
```

In an imported module, a fixed top-level `let` declares a module constant. Its
initializer must belong to the existing closed static-initializer set:

```hexal
-- limits.hex
let MaxTouchPoints: Int32 = 10
let DefaultWidth: Int32 = 1280

fun valid_touch_count(count: Int32): Bool do
    return count <= MaxTouchPoints
end

export
    MaxTouchPoints,
    DefaultWidth,
    valid_touch_count
end
```

An imported module cannot declare mutable runtime state:

```hexal
-- rejected in an imported module
let mut request_count: Int32 = 0
```

Applications represent reusable mutable state with an ordinary explicit type
owned by the caller. Importing a module never constructs a hidden singleton or
module instance.

## Rationale and ROI

Replacing `static` with `const` or `var` would rename or enlarge the surface
instead of simplifying it. The declaration already communicates the necessary
facts:

- `let` means the binding is fixed after initialization;
- `let mut` means the binding may be assigned;
- top-level position supplies file scope;
- the selected entry role permits runtime script initialization; and
- the imported-module role permits only fixed, statically initialized values.

This retains useful named constants without retaining hidden mutable library
state. It also preserves the entry program's script-like form and avoids a
source-level `main`, module constructor, module destructor, or first-class
module-instance concept.

The same source can be selected as an entry module in one compilation and used
as an imported module in another. That role already determines whether root
execution is permitted. This RFC makes the corresponding binding rule explicit:
runtime root bindings belong to an entry; an imported module admits only fixed
module constants.

This role dependence is deliberate. For example, a fixed literal declaration is
a source-ordered entry binding when its file is selected as the program, but an
order-independent module constant when another file imports it. Classifying by
initializer shape instead would silently turn some entry-script locals into
program-lifetime objects, while classifying every top-level `let` as module
storage would forbid runtime script initialization. Hexal chooses the visible
entry/import boundary over either hidden classification. Moving a file between
those roles can therefore require moving reusable constants into a library
module; the validation below fixes this behavior as part of the language.

This intentionally differs from Zig and Odin, which permit mutable namespace or
package state. Hexal takes only their useful fixed-constant model and keeps
mutable library state caller-owned.

C-owned globals remain available through `extern c global` under the existing
foreign/unsafe contract. They are storage owned and initialized outside Hexal,
not a way to declare Hexal-owned mutable module state.

## Syntax

RFC 0218 supplies the sole value-declaration spelling. In the repository's
checked EBNF dialect, RFCs 0218 and 0219 together make these exact changes:

```ebnf
Program = LexicalSeparation  [ ImportBlock ]  { ExternBlock }
          { TopLevelItem }  [ ExportBlock ] .
TopLevelItem = DeclarationItem | Statement .
Declaration = "let"  [ "mut" ]  identifier
              ( ":"  TypeExpression  "=" | "=" )  Expression .
```

`StaticModuleValue` is deleted from the normative grammar in
`docs/reference.md`, the checked root `GRAMMAR.ebnf`, the parser, and the AST.
`static` is removed from `reserved_word`, `let` is added by RFC 0218, and
`static` becomes an ordinary identifier in every identifier position. Whether
a parsed top-level `Declaration` is allowed in an imported module is a semantic
module-role rule, not a second grammar production.

The parser adds no contextual migration production. Former `static` declarations
fail through the ordinary grammar. These uses are ordinary and valid subject to
normal name rules:

```hexal
let static: Int32 = 1
```

The identifier is equally valid in other declaration roles:

```hexal
fun static(): Int32 do
    return 1
end
```

The checker, not the parser, classifies a direct top-level `Declaration` from
the selected module role. It first recognizes direct fixed inferred
function-literal declaration sugar. That form is always a module-level named
function, never stored constant data: it remains order-independent, exportable
as a function, valid in an imported module, and able to read module constants.
A written function type, mutable receiving binding, suffix on the literal, or
initializer copied from an existing `Fun` value remains ordinary data and is
subject to the entry/import rules below.

## Entry-module bindings

A direct top-level declaration in the selected entry module is an entry binding:

- `let` and `let mut` are both accepted;
- the initializer may be any initializer valid for an ordinary binding;
- initialization executes in source order;
- root statements observe the binding from its declaration onward;
- root `defer`, error, cleanup, and final-result behavior remain unchanged;
- the binding cannot be exported; and
- its lifetime ends when entry execution and its root cleanup complete.

RFC 0219 does not itself make these bindings visible inside named functions or
methods. RFC 0220 defines that entry-environment capture and its lowering. Until
RFC 0220 is implemented, the existing no-capture rule remains.

The physical C placement is not source semantics. An uncaptured entry binding
may remain an automatic local in generated `main`; RFC 0220 may place a captured
binding in a stack-owned entry-environment record.

## Imported-module constants

A direct top-level declaration in a reachable non-entry Hexal module is a module
constant only when all of these conditions hold:

1. the declaration is fixed (`let`, not `let mut`);
2. its initializer is valid under the closed static-initializer set; and
3. it is not ordinary direct function-literal declaration sugar.

The closed set is exhaustive:

- a checked literal/constant operand, including numeric, Boolean, Rune, text,
  `Nil`, and `EoS` literals supported by its contextual type;
- `Array` literals, struct construction, and ADT variant construction whose
  complete initializer trees are in this set; and
- structural-union injection and widening applied to a value in this set.

Nested arrays and aggregates are accepted recursively. Transparent aliases do
not change classification; a generic construction is accepted only after
specialization produces an otherwise accepted concrete construction.

Every binding reference, including a reference to an earlier or later module
constant, is excluded from an initializer. Address-taking, pointer-producing
expressions, calls, allocation, I/O, Tasks, runtime `Heap`, and other expression
kinds are excluded. Constant dependency cycles therefore cannot be expressed
and require no ordering graph. A module constant's type may not contain
`Atomic` directly or transitively; the former direct fixed
`Atomic<T>(literal)` acceptance exception is replaced by a targeted rejection.

“Module constant” means an immutable program-lifetime runtime object. It is not
a compile-time substitution, a type-level value, or permission for the compiler
to duplicate the object at source uses.

A module constant's annotation and every type named by its initializer may use
only builtins, imported types, and defining-module types declared earlier in
source. Collecting all type declarations before the constant pass does not make a
later type visible. This is the same source-order type rule as an entry binding;
only the successfully checked constant's value name becomes order-independent for
later function and method body checking.

A module constant:

- has program lifetime and immutable Hexal access;
- is visible throughout its defining module regardless of source position;
- may be read by its module's functions and methods;
- is private unless named in the module's export block;
- is accessed by importers through their chosen module alias; and
- denotes one stable, program-lifetime immutable object that may be copied or
  addressed under the existing fixed-value rules.

Taking its address yields a read-only pointer to that one object. Taking the
address of a member yields a pointer into the same object. Such pointers may be
passed as ordinary runtime arguments, including to imported functions, under the
existing pointer and foreign-call rules. Every local and qualified reference
observes the same address; no source-level use substitutes a separate object.
This stable identity is part of the language contract even when the C optimizer
removes physical loads. An exported C declaration is `extern const`, so a C
consumer may likewise take the address of the single owning definition.

The static-initializer classification is structural after alias resolution and
generic specialization. Pointer indirection does not embed pointee storage in the
constant: a nil pointer member is eligible, while address-taking and every other
pointer-producing initializer remain excluded. `Atomic` containment traverses
aliases, arrays, struct members, ADT payloads, union members, and specialized
generic value fields, but stops at pointer, view, and function indirection.
Foreign constants remain separate foreign declarations and cannot be referenced
from a Hexal module-constant initializer.

Imported modules otherwise remain declarations-only. Importing one runs no user
code, allocates no runtime resource, starts no Task, performs no I/O, and creates
no initialization or finalization order.

A top-level `let mut` in an imported module is rejected even when its initializer
is static. `export` controls visibility only; it never supplies storage, changes
mutability, or converts an entry binding into a module constant.

## Explicit mutable library state

Library state remains an ordinary value owned by the application:

```hexal
-- logger.hex
type Logger is struct
    count: Int32,
end

fun log(logger: Ptr<mut Logger>, message: String) do
    -- update logger through the explicit pointer
    print(message)
end

export
    Logger,
    log
end
```

The language adds no implicit `Module.init()`, module environment type, hidden
allocator, or module cleanup protocol. Multiple independent instances use the
same ordinary type/value mechanisms as every other Hexal object.

## Exports and imports

An export block may retain the existing eligible declarations: native types,
functions, methods, module constants, and exportable foreign C declarations.
An entry binding is not a module declaration and cannot be exported.

The module registry retains a fixed module-constant entry and qualified constant
resolution. It removes mutable module-value metadata and assignment paths.
The entry records the defining module identity, complete `TypeUse`, fixed and
addressable status, and generated symbol identity.

An exported constant's complete type must close over builtins and exported types
under the existing interface-closure rule. A constant is an ordinary value in
expressions and as a runtime argument, including to a specialized generic
function. Hexal has no value-generic or other type-level constant arguments, so
a module constant is not valid where a type is required. Its exported generated
header declaration is part of the C ABI and is available to a C consumer that
includes that module header.

Unknown export names continue to report:

```text
unknown declaration <name> in this module
```

Foreign constants and globals retain their existing export/import behavior.

## C23 lowering

- Entry bindings follow their existing runtime initialization until RFC 0220
  classifies captured storage.
- Every module constant emits exactly one owner-qualified immutable object
  definition in its owning module's C file; source-level inlining is forbidden
  because the object has stable address identity.
- A private definition has internal linkage: `static const T symbol = value;`.
- An exported definition has external linkage: `const T symbol = value;`. Its
  owning header and every generated consumer declaration use
  `extern const T symbol;`; no header contains a definition.
- Scalars, arrays, structs, strings/strands, ADTs, unions, zero-payload values,
  and niche-represented values use their existing compiler-owned C type and
  initializer spelling. Each receives exactly one addressable module object even
  when it is only copied. A string or strand may reference existing immutable
  literal bytes internally, but its module-level wrapper is still that one
  object.
- A module constant never emits writable native storage.
- The direct fixed Atomic module-storage path is deleted.
- No Hexal declaration emits mutable C file-scope storage.
- C `static` remains available for generated private functions, helpers, and
  immutable implementation data. Only the Hexal source keyword is removed.
- Foreign C globals retain their declared linkage and unsafe-access behavior.
- Generated output remains deterministic and the compiler remains strictly
  string-in/string-out.

## Diagnostics

| Condition | Required diagnostic |
| --- | --- |
| `let mut name = value` at an imported module's top level | Module Error: `imported module <module> cannot declare mutable top-level binding <name>; pass explicit state instead` |
| Fixed imported top-level initializer outside the closed set | Type Error: `module constant <name> must be statically initialized` |
| Imported constant whose type contains `Atomic` directly or transitively | Type Error: `module constant <name> cannot contain mutable Atomic state; pass explicit state instead` |
| Export block names an entry binding | Name Error: `unknown declaration <name> in this module` |
| Assignment to a module constant | existing fixed-binding assignment diagnostic |
| Former `static` declaration | ordinary earliest parser/name diagnostic; no contextual migration rule |

For the mutable-top-level diagnostic, `<module>` is the defining module's
canonical logical source key, `<name>` is the source identifier spelling, and the
diagnostic is anchored at that identifier. It is emitted before the initializer
is checked, so initializer diagnostics are suppressed. The Atomic diagnostic is
also anchored at the binding name after its complete declared type is resolved.

After RFC 0218, `static` lexes as an identifier. No valid contextual migration
production is added. Ordinary parser recovery owns rejection of `static let x =
1`, `static mut x = 1`, `static x := 1`, and `static x: Int32 := 1` before checker
classification; that earlier malformed statement masks RFC 0218's `:=` migration
diagnostic. `static = 1` is not an
obsolete declaration: it is an ordinary assignment to a binding named `static`
and succeeds or receives the ordinary assignment/unknown-name diagnostic.

## Implementation plan

### Phase 1: coordinate RFC 0218 and remove syntax

1. Remove every `static let` production, example, question, decision, and test
   from RFC 0218 before implementation.
2. Delete the lexer `Static` token and keyword entry.
3. Delete parser `StaticModuleValue`/`ModuleValueDeclaration` syntax and dispatch.
4. Accept `static` as an ordinary identifier without a migration parser.

### Phase 2: checker and module registry

1. Recognize direct fixed function-literal sugar before value classification.
2. Preserve entry declarations as source-ordered root runtime bindings.
3. In a dedicated imported-module constant pass at the current pass-1.5
   position, collect and check every remaining direct fixed top-level
   declaration before any function or method body. Resolve its annotation and
   initializer type names against the source-position type boundary, even though
   the pass runs after type collection. Register successful constants before body
   checking so their value visibility to functions and methods is
   order-independent.
4. Reject imported top-level `let mut` before initializer checking and before
   the generic imported-executable-item diagnostic.
5. Retain the exhaustive initializer allowlist, exclude every binding reference,
   and replace the Atomic acceptance exception with the direct/transitive type
   rejection above.
6. Convert the fixed module-value registry/place paths to constants sourced from
   ordinary declarations; delete mutable metadata and assignment paths while
   preserving read-only addressability.
7. Apply interface closure to exported constant types and preserve qualified
   import resolution for fixed constants only.

### Phase 3: generator

1. Preserve entry-root lowering pending RFC 0220 capture classification.
2. Convert module-value generation to the single-object linkage rules above.
3. Delete writable definitions and every direct or aggregate Atomic
   module-storage branch.
4. Preserve owner-qualified symbol identity, stable address lowering, and exact
   owning-header/consumer `extern const` declarations.

### Phase 4: sources and documentation

1. Rewrite the active static-module-value snippet as a fixed top-level `let`
   constant example; remove its `static` reserved-word metadata.
2. Rewrite the concurrency fixture's shared Atomic as explicit entry-owned
   state passed to workers; do not replace it with imported mutable state.
   Remove or rewrite every exported/imported Atomic module-value, address-taking,
   equality/printing, registry, component-selection, generated-definition,
   example, and documentation path found by the required repository sweep.
3. Split integration coverage between entry bindings, imported constants, and
   rejected imported mutable bindings.
4. Remove `static` from `RequiredReservedWords` and update the snippet manifest
   only for reviewed generated-output changes.
5. Update every affected grammar, module, binding, export, constant, and
   generated-artifact rule in `docs/reference.md`, the checked root grammar,
   and `docs/status.md`.

## Required implementation sweep

- lexer keyword/token tables, token names/string mappings, reserved-word tests,
  parser recovery/synchronization sets, import/export identifiers, type/member/
  function/method identifier tests, generated-name collision tests, and
  `workbench/snippets/catalog.go` reserved-word coverage;
- parser static declaration node, dispatch, recovery, and tests;
- checker module prepass, declaration classification, static initializer,
  scopes, module registry, exports, places, operands, addressability,
  assignment, Atomic acceptance/rejection paths, diagnostics, and checked-tree
  validation;
- generator immutable definitions, declarations, component selection, symbol
  naming, rendering, emission, metadata validation, and tests;
- `workbench/snippets/catalog.go`, the modules catalog, snippet SHA manifest,
  integration module-global tests, tagged concurrency fixture, packages,
  standard-library sources, and driver fixtures;
- the grammar, Programs/names/bindings, Modules, anonymous-function visibility,
  and Generated artifact split rules in `docs/reference.md`, plus the checked
  root `GRAMMAR.ebnf` and `docs/status.md`.

## Validation (exhaustive)

1. Entry-root `let` and `let mut` accept runtime initializers and retain
   source-order execution, root cleanup, and entry lifetime.
2. An imported fixed top-level `let` with every form in the exhaustive closed set
   checks as a module constant; binding references, address-taking, non-nil
   pointer production, calls, allocation, I/O, Tasks, and other excluded
   expressions report `module constant <name> must be statically initialized`.
   A contextually typed nil pointer, including as an aggregate member, remains an
   eligible literal.
3. An imported top-level `let mut` reports the exact Module Error above whether
   its initializer is static or dynamic.
4. A constant whose type embeds `Atomic` directly or through an alias, array,
   aggregate, ADT, union, or specialized generic value field reports the exact
   Atomic Type Error above; pointer, view, and function indirection stop the
   containment walk, and no Atomic module-storage acceptance path remains.
5. Module functions and methods read fixed constants regardless of declaration
   order. They cannot assign them.
6. A private constant is unavailable to importers; an exported constant resolves
   only through the import alias, retains defining-module identity, and rejects
   when its complete type exposes a private nominal type.
7. Entry bindings remain ineligible for export; eligible native and foreign
   declarations retain their existing export behavior.
8. Direct fixed inferred function-literal declaration sugar remains an
   order-independent, exportable function declaration in imported and entry
   modules, may read module constants, and is never stored constant data. A
   written function type, mutable receiver, suffix, or copied `Fun` value is
   ordinary data and rejects in an imported module when not statically
   initialized.
9. `static` is accepted under normal name rules as a binding, parameter, member,
   function, method, type, import alias, and export name. Each obsolete form
   listed under Diagnostics is parser-owned, while `static = value` follows
   ordinary assignment rules.
10. Every scalar, array, struct, string/strand wrapper, ADT, union, zero-payload,
    and niche-represented module constant has one stable address. A member address
    points into that same object and may be passed under ordinary pointer rules.
    Private constants emit one `static const` definition; exported constants emit
    one external `const` definition plus matching `extern const` owning-header and
    consumer declarations that allow a C consumer to take its address. No accepted
    Hexal declaration emits mutable C file-scope storage.
11. Constant initializers cannot reference earlier, later, local, imported, or
    self constants; dependency cycles are consequently impossible. Constants
    remain runtime values and are rejected in type/value-generic positions the
    language does not support.
12. The same fixed literal top-level `let` is a source-ordered entry binding when
    its file is the selected entry and an order-independent module constant when
    imported; the entry form is not exportable and the imported form may be.
13. RFC 0219 lands after RFC 0218 and before RFC 0220. Against the reviewed
    post-0218 artifact baseline, programs using neither obsolete `static` syntax
    nor module constants retain byte-identical generated C.
14. The rewritten concurrency fixture shares entry-owned Atomic state through
    explicit parameters and preserves its expected result.
15. A repository audit finds no `Static` token, static declaration AST node,
    mutable module registry metadata, Atomic module acceptance, writable module
    definition, Atomic-specific exported/imported/address/equality/printing path,
    reserved-word entry, or stale semantic rule.
16. The compiler remains string-in/string-out and ordinary tests invoke no
    external compiler.
17. A module constant whose annotation or initializer names a defining-module
    type declared later is rejected by the existing source-order type diagnostic;
    moving the type earlier accepts it. Constant value visibility to functions
    and methods remains order-independent.
18. `go test ./...`, `go vet ./...`, `go vet -tags c23 ./...`, the complete
    tagged C23 suite under Clang, and `gofmt -l` all pass.

## Decisions

1. Hexal adds no replacement keyword for `static`; `const` and `var` are not
   introduced.
2. `let` is the sole value-binding introducer and `mut` is the sole mutability
   marker.
3. Entry top-level declarations are runtime entry bindings.
4. Imported fixed top-level declarations are statically initialized module
   constants and may be exported.
5. Imported mutable top-level declarations are forbidden.
6. Export controls visibility only and never changes storage or mutability.
7. Mutable library state is an explicit caller-owned value, not an implicit
   module instance or singleton.
8. RFC 0220 alone defines named entry-function access to entry bindings.
9. Module constants always have one stable immutable object and are never
   source-level inline substitutions.
10. Module constant initializers cannot reference bindings; no constant
    dependency graph or cycle rule exists.
11. A module constant obeys source-order type visibility even though its checked
    value name is visible to every function and method in the module.

## Non-goals

- Adding `const`, `var`, `global`, `module`, `shared`, or another storage keyword.
- General compile-time execution beyond the existing closed initializer set.
- Mutable imported-module state, module constructors, module destructors, lazy
  globals, or import-time execution.
- General closures, heap environments, or capture of function locals.
- Changing foreign C global semantics, ownership, or the in-memory compiler
  boundary.
