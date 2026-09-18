# RFC 0219: Module-Scope Values and Exported Storage

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: proposal recorded;
  initialization and entry-module semantics remain unresolved
- Created: 2026-09-18
- Updated: 2026-09-18
- Scope: make module scope the lifetime boundary for module-owned value
  bindings, make `export` the visibility boundary, and remove the `static`
  declaration keyword
- Coordinates with: RFC 0218, module imports and exports, declaration scope,
  module initialization, generated C storage, and `docs/reference.md`
- Does not change: lexical local scope, closure support, function parameters,
  function captures, the string-in/string-out compiler boundary, or the
  semantics of `let mut` within a scope

## Summary

`static` is removed from the language. A value binding declared at module scope
is module-owned storage with program lifetime. A value binding declared inside a
function, method, branch, loop, or other local block remains a local lexical
binding.

`export` controls whether a module-scope declaration is visible through the
module's import alias. It does not introduce storage; the declaration's scope
does that.

```hexal
-- counter.hex
let mut count: Int32 = 0

fun increment() do
    -- Access to module values from a function is unresolved; this function
    -- must receive or otherwise qualify the value under the final scope rules.
end

export count, increment
```

```hexal
-- caller.hex
import counter from "counter"

counter.increment()
print(counter.count)
```

The example deliberately does not assume that a function may access `count`
without a parameter or module qualification. Hexal does not add closures or
implicit function captures.

## Motivation

The current `static` spelling duplicates the distinction already represented
by declaration scope and export visibility:

```hexal
static let mut count: Int32 = 0
export count
```

The proposed form makes those responsibilities explicit and separate:

- module scope determines ownership and lifetime;
- `export` determines external visibility;
- `let` or `let mut` determines replacement mutability.

This gives private and public module values the same declaration form and
removes a storage keyword that is unnecessary when module scope already owns
the value.

## Proposed syntax

The exact canonical grammar must be updated in `GRAMMAR.ebnf` and
`docs/reference.md` after the open questions below are resolved.

```ebnf
module-value = "let" , [ "mut" ] , identifier
               , ( ":" , type-expression , "=" | "=" ) , expression ;

local-value = "let" , [ "mut" ] , identifier
              , ( ":" , type-expression , "=" | "=" ) , expression ;

export-block = "export" , export-entry , { "," , export-entry } , "end" ;
```

The declaration spelling is identical in both scopes; scope determines its
storage behavior. `static let` and `static mut` are removed.

## Module-scope semantics

- Every accepted module-scope value binding has module ownership and
  program-lifetime storage.
- A module-scope value is private unless its name appears in the module's
  export block.
- An exported value is accessed only through the importing module's alias.
  Wildcard and unqualified access remain invalid.
- Whether a module-scope function may access a same-module value without a
  parameter or explicit module qualification is unresolved below.
- `let` creates a fixed module value; `let mut` creates a replaceable module
  value.
- Assignment to an exported mutable value follows the ordinary place and
  mutability rules. Whether importers may mutate an exported value directly is
  unresolved below.
- Module-scope functions and types retain their existing module-level
  identity. This proposal changes value storage and visibility rules, not
  function capture or closure behavior.

## Local semantics

- A local `let` or `let mut` remains a lexical binding in its containing block.
- A local binding is not module-owned and cannot be exported.
- A function cannot access a local binding from its caller or enclosing source
  scope through implicit capture; values required by a function are passed
  through parameters or accessed as module declarations under the resolved
  module-scope rules.

## Initialization

Imported modules are currently declarations-only and cannot perform arbitrary
import-time execution. This proposal therefore requires an explicit decision
about how module-scope initializers are evaluated.

The candidate design is that module-scope initializers use the existing closed
static-initializer set and produce deterministic program-start storage. The
proposal does not yet decide whether a future initializer may call a function,
allocate runtime objects, or perform I/O.

Module initialization must define behavior for:

- source-order dependencies between module values;
- dependencies on values from imported modules;
- cycles between imported modules;
- values that are read before initialization completes;
- the entry module's root declarations.

## Export semantics

`export` is a visibility declaration, not a storage declaration:

```hexal
let private_limit: Int32 = 16
let public_limit: Int32 = 64

export public_limit
```

The language must decide whether exported mutable values are directly writable
through an import alias or are read-only to importers. The current proposal
does not silently introduce a second mutability mode; that choice belongs in
the resolved module API contract.

## C23 lowering

Module-scope values lower to one owner-module definition and, when exported,
the corresponding imported declaration. Private values remain inaccessible to
other modules. Local bindings retain their existing lowering.

The final design must preserve deterministic artifact contents and the
string-in/string-out compiler boundary. The compiler must not discover module
storage through host files or directories.

## Required implementation sweep

- remove `static` from value-declaration grammar, parser paths, diagnostics,
  tests, and active source fixtures;
- classify every value binding by module scope versus local lexical scope;
- update export checking and import-alias access for module values;
- define and implement module initialization order and cycle behavior;
- update checker facts for persistent mutable module values;
- update generated C definitions, declarations, and access paths;
- update `GRAMMAR.ebnf`, `docs/reference.md`, `docs/status.md`, and active
  examples;
- preserve the in-memory compiler boundary and test generated C text rather
  than invoking external tools in ordinary tests.

## Validation

Validation is intentionally blocked until the open questions are resolved. The
eventual implementation must at minimum verify:

1. Private and exported module-scope values have program-lifetime storage.
2. Module-scope `let` and `let mut` have distinct replacement behavior.
3. Local bindings remain lexical and cannot be exported.
4. Functions do not gain implicit closure capture.
5. Imported module values are reachable only through the module alias.
6. Exported functions, types, and values retain their existing visibility
   rules unless explicitly changed by the resolved design.
7. All invalid `static` spellings receive defined diagnostics.
8. Module initialization order, cycles, and failed initialization have defined
   diagnostics and deterministic behavior.
9. Generated C has one deterministic owner for every module-scope value.
10. The compiler remains string-in/string-out and all ordinary tests remain
    free of external compiler invocations.

## Open questions

1. Does every module-scope value binding become program-lifetime storage, or
   only declarations named in an `export` block?
2. Can private module-scope values be declared when a module exports nothing?
3. Are entry-module root `let` bindings module storage or execution-time local
   bindings?
4. May a module-scope function access a same-module value without receiving it
   as a parameter or qualifying it through a module namespace?
5. Are imported modules still strictly declarations-only, or may module-value
   initialization execute generated initialization code?
6. Which initializer forms are permitted for module-scope values? Is the
   existing closed static-initializer set exhaustive?
7. What is the initialization order across declarations in one module and
   across imported modules?
8. What happens for cyclic module imports or cyclic module-value dependencies?
9. What happens when a module value is read before its initialization finishes?
10. May an initializer call a function, allocate runtime storage, perform I/O,
   or access task/concurrency facilities?
11. May an importer assign directly to an exported `let mut` value, or are
   imported values read-only regardless of their defining declaration?
12. Does exporting a mutable value expose shared writable state, a setter-like
   operation, or only a readable view?
13. Are exported values, functions, and types all governed by one export
   namespace, or do values retain a separate namespace?
14. What exact diagnostics replace `static let`, `static mut`, and other
   obsolete static spellings?
15. Does removing `static` supersede the static-module-value portion of RFC
   0218, and should RFC 0218 retain any module-value syntax at all?
16. Which generated-C storage qualifiers and header declarations represent
   private versus exported module values?

## Non-goals

- Adding closures or implicit function captures.
- Making local bindings exportable.
- Adding a general runtime initialization or dependency-injection system.
- Changing function, method, parameter, member, ADT-payload, result, or
  `for`-binder syntax.
- Changing the in-memory compiler boundary.
