# RFC 0112: Order-Independent Function Visibility

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Draft; design proposed, implementation not started
- Features: forward function references and mutual recursion within modules
- Created: 2026-08-22
- Depends on: RFC 0008 (functions and function pointers), RFC 0034 (modules),
  RFC 0094 (anonymous function literals), and `docs/reference.md`
- Coordinates with: generic specialization, generated module prototypes, and
  RFC 0103 language-surface audit finding F1

## Scope

All module-level function and method signatures are visible throughout their
defining module after declaration collection. Function bodies may therefore
refer to later declarations and may form mutually recursive groups.

This RFC changes declaration visibility only. Executable bindings, initializer
expressions, and module imports retain their existing source-order rules.

## Function and method visibility

- A module collects every function and method signature before checking any
  function body or root executable statement.
- A function may call a later function in the same module.
- Two or more functions may call one another recursively.
- A method may call a later method or function visible in the same module.
- A function name and method name remain subject to existing duplicate,
  receiver, visibility, and export rules.
- A declaration's body may not read a root value binding or initializer merely
  because its function signature was collected.
- A function may call an imported exported function regardless of source order
  in the importing module.
- Cross-module import cycles remain Module Errors. This RFC does not make
  module dependency cycles legal.

## Anonymous functions

Anonymous function literals use the same collected named-function visibility
set as the declaration position in which they occur. Their own receiving
binding still supplies direct self-recursion when the existing RFC 0094 rules
permit it. A literal cannot capture a later or earlier lexical value binding.

## Generics and recursion

- Generic declarations enter the signature environment before body checking.
- A generic function may call another generic function declared later.
- Same-argument recursive specialization remains valid.
- Argument-changing specialization cycles remain rejected.
- A recursive cycle that cannot resolve a concrete signature is a diagnostic,
  not an Unknown Error.

## Generated C

- Every module header emits prototypes for all functions and methods whose
  declarations are visible across the module boundary.
- Every module C file emits definitions after the prototype region.
- Private functions retain internal linkage; exported functions retain their
  existing external-linkage contract.
- Prototype ordering is deterministic by module identity and source position.
- Function bodies retain their source `#line` mappings.

## Non-goals

- Forward references to executable value bindings.
- Cross-module import cycles.
- Implicit declarations or inferred function signatures.
- Changing generic inference or method receiver adaptation.
- Making closures legal.

## Validation

This section is exhaustive. RFC 0112 is complete only when every item below
passes:

- A function calls a later function in the same module.
- Two functions call each other recursively.
- A method calls a later method and a later module-level function.
- Duplicate functions, duplicate methods, invalid receivers, and visibility
  violations retain structured diagnostics.
- A function cannot read a root value binding solely because signatures were
  collected.
- Imported exported functions remain callable independent of their local
  textual position.
- Import cycles remain Module Errors.
- Generic forward calls and same-argument recursive specialization compile;
  argument-changing cycles fail with the existing specialization diagnostic.
- Anonymous literals see only the collected function set and still reject
  lexical captures.
- Generated C prototypes precede every mutually recursive use and each
  definition is emitted exactly once.
- Repeated compilations produce identical module artifacts and symbol names.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes, remove the source-order restriction for
function and method declarations from `docs/reference.md` and update the
generated artifact contract for prototype collection.
