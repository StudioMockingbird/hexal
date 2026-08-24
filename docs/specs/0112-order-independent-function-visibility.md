# RFC 0112: Order-Independent Function Visibility

- Kind: Language Semantics (ISO/IEC Language Standard Format)
- Status: Implementation-ready; design settled, implementation not started
- Updated: 2026-08-24
- Features: module-level forward function references and mutual recursion;
  removal of nested named function declarations
- Created: 2026-08-22
- Depends on: RFC 0008 (functions and function pointers), RFC 0034 (modules),
  RFC 0094 (anonymous function literals), and `docs/reference.md`
- Coordinates with: generic specialization, generated module prototypes, and
  RFC 0103 language-surface audit finding F1

## Scope

All module-level function and method signatures are visible throughout their
defining module after declaration collection. Function bodies may therefore
refer to later module-level declarations and may form mutually recursive
groups.

This RFC changes declaration visibility only. Executable bindings and
initializer expressions retain their existing source-order rules. Imports
retain their stronger existing rule: they form one contiguous module prefix and
must precede every type, function, method, value, or executable item.

Nested named function declarations are removed. `fun name(...)` is valid only
at module scope. A function literal may still appear inside a function as an
ordinary non-capturing `Fun<...>` value, but a local binding initialized by one
is runtime data: it is source-ordered, receives no implicit recursion name, and
creates no lexical function declaration.

## Required sweep of RFC 0094 behavior

RFC 0094 introduces local named functions, local storage-free function-literal
declarations, and source-order module function visibility. This RFC supersedes
those implemented behaviors. Its module-level fixed function-literal binding
becomes order-independent. Its local declaration forms are removed rather than
made order-independent: nested named functions cease to be syntax, and a local
function-literal binding remains an ordinary source-ordered value.

RFC 0094 is closed and archived. It owns function literals and their C
representation; this RFC removes its local declaration layer and changes
module-level visibility. Do not edit archived RFC 0094. Reconcile the
implemented parser, checker, generator, tests, `docs/reference.md`, and
`docs/status.md` instead.

The required sweep removes:

- parsing and checked nodes used only by local `fun name(...)` declarations;
- lexical local-function binding frames and local open-template declarations;
- local self-recursion and local mutual-recursion machinery;
- declaration-only classification for local `name := fun ...` bindings;
- tests asserting visibility, shadowing, recursion, generic specialization, or
  helper identity for nested named declarations; and
- any generated-C path or prototype ownership used only by named local
  functions.

Anonymous literal helper emission remains because ordinary local `Fun<...>`
values still use it.

## Function and method visibility

- After the import prefix, a module resolves all of its type declarations in
  their existing source order. It then collects every function and method
  signature before checking any function body or root executable statement.
- A function or method signature may name a module type declared later because
  signature collection uses the completed module type environment. This does
  not make type declarations mutually recursive or otherwise change the
  existing source-order rules between type declarations themselves.
- Function and method bodies also use that completed module type environment.
  Root executable statements retain source-order type-name visibility; this RFC
  gives them the completed function set, not a completed root value/type scope.
- A function may call a later function in the same module.
- Two or more functions may call one another recursively.
- A method may call a later method or function visible in the same module.
- A root executable statement may call a module-level function or method
  declared later. Root statements still execute in source order.
- A function name and method name remain subject to existing duplicate,
  receiver, visibility, and export rules. Precollection must not change
  diagnostic ownership: when a function or method collides with another
  declaration, the declaration later in source order owns the diagnostic.
- A declaration's body may not read a root value binding, local value binding,
  or initializer merely because its function signature was collected.
- An import after any other module item remains a Syntax Error under the
  existing `imports must precede all other top-level items` diagnostic. Because
  imports form the prefix, every successfully collected signature and body may
  use every import alias in that prefix. Export position inside the defining
  module remains irrelevant.
- Cross-module import cycles remain Module Errors. This RFC does not make
  module dependency cycles legal.

## Anonymous functions

Anonymous function literals use the collected module-level named-function set.
A literal inside a function remains closed: it cannot capture parameters,
`self`, or earlier or later lexical value bindings. A local binding receiving a
literal is ordinary runtime data and does not provide an implicit self name.

A module-level direct inferred fixed function-literal binding remains the
storage-free function declaration sugar defined by RFC 0094 and follows the
same module-wide visibility rule as the equivalent named function declaration.

## Generics and recursion

- Generic declarations enter the signature environment before body checking.
- A generic function may call another generic function declared later.
- Same-argument recursive specialization remains valid.
- Argument-changing specialization cycles remain rejected.
- A recursive cycle that cannot resolve a concrete signature is a diagnostic,
  not an Unknown Error.
- No local open generic-function template exists. A generic literal inside a
  function requires an exact contextual `Fun<...>` type and becomes one
  concrete runtime function-pointer value.

## Settled decisions

- Module-level functions and methods are order-independent and may be mutually
  recursive.
- Root executable statements see the complete collected module function and
  method set.
- Imports remain a mandatory contiguous prefix and are therefore available to
  every later module item.
- Function and method signatures may name module types declared later; type
  declarations retain their own existing source-order resolution rules.
- Duplicate diagnostics follow textual declaration order, never internal
  collection order.

## Generated C

- Every module header emits prototypes only for module-level functions and
  methods whose declarations are visible across the module boundary. Private
  module-level functions never appear in a module header.
- Every module C file emits a module-local prototype region before any function
  definition that may be referenced before its definition. Private
  module-level functions and methods use `static` prototypes in this region.
  Exported module-level prototypes are supplied by the module header and are
  not duplicated in the C file solely for this RFC.
- Function definitions are emitted after the applicable prototype region and
  exactly once.
- Private functions retain internal (`static`) linkage; exported module-level
  functions retain their existing external-linkage contract.
- Prototype ordering is deterministic by module source position.
- Function bodies retain their source `#line` mappings.

## Non-goals

- Forward references to executable value bindings.
- Cross-module import cycles.
- Implicit declarations or inferred function signatures.
- Changing generic inference or method receiver adaptation.
- Making closures legal.
- Nested named function declarations, local function-declaration hoisting, or
  local mutually recursive function groups.

## Validation

This section is exhaustive. RFC 0112 is complete only when every item below
passes:

- A function calls a later function in the same module.
- Two functions call each other recursively.
- A method calls a later method and a later module-level function.
- A root executable statement calls a function declared later; root executable
  statements still execute in written order.
- A function and method signature each name a module type declared later. A
  type declaration that requires an unavailable later type retains its existing
  source-order diagnostic.
- A function body may use a module type declared later. A root declaration that
  explicitly names a later type retains its existing source-order diagnostic,
  even though a root call to a later function is valid.
- Imports remain a contiguous prefix. An import following a type, function,
  method, value, or executable statement retains the exact existing `imports
  must precede all other top-level items` diagnostic. Functions may use any
  alias in the valid prefix, and export position in the imported module remains
  irrelevant.
- `fun name(...)` inside a function, branch, loop, or bare block reports Syntax
  Error, `named function declarations are only valid at module scope`.
- A local binding initialized by a function literal is ordinary source-ordered
  runtime data, receives no implicit self-recursion name, and cannot be used
  before its declaration. An exact contextual generic literal specializes to
  one concrete `Fun<...>` value; an inferred local open-template declaration is
  rejected.
- Duplicate functions, duplicate methods, invalid receivers, and visibility
  violations retain structured diagnostics.
- A function/value or function/type collision reports the declaration later in
  source order, independent of collection order; reversing the declarations
  reverses which declaration owns the diagnostic.
- A function cannot read a root or local value binding solely because module
  signatures were collected.
- Imported-function visibility matches the settled import-position rule, while
  export position inside the defining module remains irrelevant.
- Import cycles remain Module Errors.
- Generic forward calls and same-argument recursive specialization compile;
  argument-changing cycles fail with the existing specialization diagnostic.
- Anonymous literals see the collected module-level function set and still
  reject lexical value captures.
- Module-level direct inferred fixed function-literal declarations participate
  in forward calls and mutual recursion exactly like equivalent named function
  declarations. Local literal bindings remain ordinary source-ordered values.
- Generated C prototypes precede every mutually recursive module-level use.
  Private module-level prototypes occur only in the module C file;
  cross-module prototypes occur only in the module header. Each definition is
  emitted exactly once with deterministic ordering.
- A private module-level function called by a later function has a `static`
  prototype before the caller in the module C file and no private prototype in
  the module header.
- No named-local declaration, checked binding, prototype, definition, generic
  template, or C symbol remains after the required sweep. Anonymous literal
  helpers remain file-scope `static` functions under RFC 0094's ownership.
- Repeated compilations produce identical module artifacts and symbol names.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Reference synchronization

After implementation stabilizes:

- remove the source-order restriction for module-level function and method
  declarations;
- state that root executable statements see the complete collected function
  set while still executing in source order;
- state that signatures use the completed module type environment while type
  declarations retain their own source-order resolution;
- remove nested named functions and local storage-free function-literal
  declarations, retaining ordinary local concrete `Fun<...>` values;
- preserve the existing import-prefix rule unchanged; and
- update the generated artifact contract for prototype collection.

## Implementation plan

### Stage 0 — baseline and sweep inventory

1. Run the full Go suite and snapshot the snippet manifest before behavior
   changes.
2. Inventory every parser node, checked node, scope binding, generic-template
   path, helper identity, prototype, definition, test, and reference rule added
   solely for RFC 0094's local named declarations or local storage-free literal
   declarations.
3. Record the existing exact import-prefix diagnostic and its tests; this RFC
   preserves them byte-for-byte.

Gate: the baseline is green and the removal inventory accounts for every
named-local representation without including anonymous literal helpers.

### Stage 1 — remove nested declarations

1. Reject `fun name(...)` in every non-module statement position with the exact
   diagnostic required by Validation.
2. Remove local named-function parser and checked forms, lexical
   function-declaration frames, self bindings, open generic templates, and
   declaration-only classification.
3. Keep anonymous literals as expressions. A local declaration receiving one
   is ordinary runtime `Fun<...>` storage and follows normal source order.
4. Require an exact contextual `Fun<...>` type for a generic literal inside a
   function; do not retain a local open-template path.

Gate: all named-local rejection and ordinary local-literal tests pass; no
named-local checked or generated artifact remains.

### Stage 2 — module declaration collection

1. Preserve parser and module-graph enforcement of the contiguous import
   prefix.
2. Inventory the shared module namespace in textual order and diagnose every
   collision at the later declaration before internal collection order can
   affect ownership.
3. Resolve module type declarations in their existing source order and retain
   all existing recursive/unavailable-type diagnostics.
4. From the completed module type environment, collect signatures for named
   functions, methods, generics, and module-level direct inferred fixed
   function-literal declarations in textual order.
5. Store signature identities separately from executable value bindings. Do
   not add an analyzer or retry/fixed-point checker.

Gate: later module types are valid in signatures, forward type dependencies
between type declarations remain invalid, and both orders of every named
collision blame the later declaration.

### Stage 3 — body and root checking

1. Check every module-level function, method, generic body, and module-level
   storage-free literal body against the complete collected signature set.
2. Admit forward calls and mutual recursion for named and equivalent
   module-level literal declarations.
3. Check root executable statements with the complete signature set while
   retaining ordinary source-order value and explicit type-name visibility and
   execution.
4. Preserve capture rejection: collection exposes declarations only, never root
   initializers or local runtime values.
5. Preserve imported-export ownership and import-cycle diagnostics.

Gate: all semantic Validation cases pass, including root forward calls, mutual
recursion, generics, imported calls, and retained capture errors.

### Stage 4 — deterministic C23 prototypes

1. Emit exported and otherwise cross-module prototypes only in the owning
   module header under existing linkage rules.
2. Emit file-scope `static` prototypes for private module functions and methods
   before any possible forward use in the owning module C file.
3. Emit every definition exactly once in deterministic source order and retain
   source `#line` mappings.
4. Keep anonymous literal helpers under RFC 0094's existing file-scope helper
   ownership. Emit no named-local prototype or definition.

Gate: generated-C text proves every prototype owner, linkage, order, unique
definition, and absence of named-local artifacts; repeated compilations are
byte-identical.

### Stage 5 — exhaustive tests, documentation, and handoff

1. Add only the parser, checker, generator, integration, and determinism cases
   named by Validation; ordinary tests invoke no external compiler.
2. Assert generated C text, not compilation success alone.
3. Recompile every existing snippet. Existing manifest hashes may move only
   where function ordering/prototypes or removal of nested named declarations
   legitimately changes generated artifacts; inspect and record the complete
   artifact breakdown before regenerating the baseline.
4. Synchronize `docs/reference.md` exactly as listed above, remove RFC 0112 from
   `docs/status.md`, close and archive this RFC, then run all repository gates.
5. Rebuild and restart the workbench after implementation completes.

Gate: Validation is exhaustive and green, canonical documentation matches the
implementation, the manifest diff is explained, and the workbench is running
the rebuilt binary.
