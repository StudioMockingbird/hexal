# RFC 0112: Order-Independent Function Visibility

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Implementation-ready; design settled, implementation not started
- Updated: 2026-08-23
- Features: forward function references and mutual recursion within modules
- Created: 2026-08-22
- Depends on: RFC 0008 (functions and function pointers), RFC 0034 (modules),
  RFC 0094 (anonymous function literals), and `docs/reference.md`
- Coordinates with: generic specialization, generated module prototypes, and
  RFC 0103 language-surface audit finding F1

## Scope

All module-level function and method signatures are visible throughout their
defining module after declaration collection, and every local function signature
is visible throughout its enclosing lexical block after block collection.
Function bodies may therefore refer to later declarations in the same
collection scope and may form mutually recursive groups.

This RFC changes declaration visibility only. Executable bindings, initializer
expressions, and module imports retain their existing source-order rules.

## Required sweep — RFC 0094 states the opposite rule

RFC 0094 is written against source-order visibility. Its header says it does
not change source-order visibility, and its Validation asserts the rule this
RFC removes:

> A module-level fixed literal binding is callable from a later function body;
> an earlier function body cannot see it.
>
> A local fixed literal binding is callable only by following statements in its
> lexical scope.
>
> Earlier local function declarations are callable from later local function
> bodies in the same block.

Under this RFC all three become order-independent within their collection
scope, so those assertions are not merely extended — the first two are
**inverted**, and the third's "earlier" qualifier becomes meaningless.

Whichever RFC lands second owns the reconciliation. Because 0094 introduces the
declarations whose visibility this RFC changes, the order is 0094 first, then
this RFC, and this RFC must then:

- rewrite those three 0094 Validation cases to assert order-independence,
  keeping the negative cases that remain true — a local is still invisible
  outside its block, and a module-level function still cannot call a local;
- remove 0094's "source-order visibility" disclaimer from its header, since it
  will no longer hold;
- retain 0094's self-recursion and closed-scope cases unchanged; neither
  depends on source order.

Landing this RFC first is also possible but costs more: 0094 would then be
written against a rule that no longer exists, and every visibility case in it
would need rewriting before implementation rather than after.

## Function and method visibility

- A module collects every function and method signature before checking any
  function body or root executable statement.
- A function may call a later function in the same module.
- Two or more functions may call one another recursively.
- A method may call a later method or function visible in the same module.
- A lexical block (function body, `if`/`while`/`for` branch, or bare block)
  collects every local function signature in that block before checking any
  statement or nested local-function body inside the same block. Locals are
  therefore order-independent within their block, exactly like module-level
  functions are order-independent within their module. Locals remain invisible
  outside their block and cannot be forward-referenced from an enclosing or
  sibling block.
- Two or more local functions in the same block may call one another
  recursively, and a local may call a later local as well as an earlier one.
- A local function may call any visible module-level function regardless of
  source order, and a module-level function may not call a local.
- A function name and method name remain subject to existing duplicate,
  receiver, visibility, and export rules. A duplicate local name in the same
  block is a diagnostic even when forward referencing would otherwise be valid.
- A declaration's body may not read a root value binding, local value binding,
  or initializer merely because its function signature was collected.
- A function may call an imported exported function regardless of source order
  in the importing module.
- Cross-module import cycles remain Module Errors. This RFC does not make
  module dependency cycles legal.

## Anonymous functions

Anonymous function literals use the same collected named-function visibility
set as the declaration position in which they occur: the module-level set plus
the local-function set of the block that contains the literal. Their own
receiving binding still supplies direct self-recursion when the existing RFC
0094 rules permit it. A literal cannot capture a later or earlier lexical
value binding.

## Generics and recursion

- Generic declarations enter the signature environment before body checking.
- A generic function may call another generic function declared later.
- Same-argument recursive specialization remains valid.
- Argument-changing specialization cycles remain rejected.
- A recursive cycle that cannot resolve a concrete signature is a diagnostic,
  not an Unknown Error.

## Generated C

- Every module header emits prototypes only for module-level functions and
  methods whose declarations are visible across the module boundary. Private
  module-level functions and all local functions never appear in a module
  header.
- Every module C file emits a module-local prototype region before any function
  definition that may be referenced before its definition. Private
  module-level functions and methods use `static` prototypes in this region.
  Exported module-level prototypes are supplied by the module header and are
  not duplicated in the C file solely for this RFC.
- Every local-function prototype region is emitted in the containing C scope,
  as specified by RFC 0094; it is not moved into a module header.
- Function definitions are emitted after the applicable prototype region and
  exactly once.
- Private functions and all local functions retain internal (`static`) linkage;
  exported module-level functions retain their existing external-linkage
  contract.
- Prototype ordering is deterministic by module source position for
  module-level functions, then by block preorder source position for locals.
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
- A local function calls a later local function in the same block, and two
  locals in the same block call each other recursively.
- A local function cannot be called from outside its lexical block, and a
  forward local in a sibling or enclosing block remains invisible.
- Duplicate functions, duplicate methods, duplicate locals in the same block,
  invalid receivers, and visibility violations retain structured diagnostics.
- A function cannot read a root value binding, local value binding, or local
  function outside its collection scope solely because signatures were
  collected.
- Imported exported functions remain callable independent of their local
  textual position.
- Import cycles remain Module Errors.
- Generic forward calls and same-argument recursive specialization compile;
  argument-changing cycles fail with the existing specialization diagnostic.
  The same holds for generic local functions within a block.
- Anonymous literals see only the collected module plus enclosing-block local
  function set and still reject lexical value captures.
- Generated C prototypes precede every mutually recursive use (module and
  local). Private module-level prototypes occur only in the module C file;
  cross-module prototypes occur only in the module header; local prototypes
  occur only in their containing C scope. Each definition is emitted exactly
  once with deterministic ordering.
- A private module-level function called by a later function has a `static`
  prototype before the caller in the module C file and no private prototype in
  the module header.
- A module header contains no local-function prototype, even when a local
  function's address is taken or its signature is used by generated code.
- Repeated compilations produce identical module artifacts and symbol names.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes, remove the source-order restriction for
function and method declarations from `docs/reference.md` and update the
generated artifact contract for prototype collection.
