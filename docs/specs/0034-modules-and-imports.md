# RFC 0034: Modules and Imports

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; initial design proposed
- Features: named modules, explicit imports, private-by-default declarations,
  qualified access, dependency ordering, and incremental compilation identity
- Created: 2026-08-11
- Depends on: RFC 0004 (identifiers), RFC 0005 (type identity and declaration
  order), RFC 0008 (functions), RFC 0019 (generics)
- Coordinates with: RFC 0039 (C interop), RFC 0041 (no module globals), the
  future build-system, package-management, and exported-ABI specifications

## Summary

Each Seawitch source file declares one module. Modules import other modules by
name and access public declarations through an explicit qualifier:

```seawitch
module app.main

import app.math

result: Int32 = math.square(10)
```

Declarations are private by default. `pub` exposes a declaration to importing
modules:

```seawitch
module app.math

pub fun square(value: Int32): Int32
    return value * value
end
```

This RFC deliberately does not define C importing or exporting. Native module
identity and dependency behavior are settled first; FFI remains later work.

## Source model

V1 uses one source file per module. The first non-comment item must be exactly
one module declaration:

```ebnf
module-declaration = "module", module-path ;
module-path        = identifier, { ".", identifier } ;
```

The module declaration creates no runtime value. Its path is the stable source
identity used by imports, diagnostics, generated private names, caches, and
incremental dependencies.

Two source files in one build may not declare the same module path. Multi-file
modules and directory-wide implicit packages are deferred.

## Imports

Imports appear after the module declaration and before every other top-level
item:

```ebnf
import-declaration = "import", module-path
                     , [ "as", identifier ] ;
```

Without `as`, the final path segment is the local qualifier:

```seawitch
import app.math          // qualifier: math
import app.text as text  // qualifier: text
```

There are no wildcard imports, selective imports, re-exports, implicit parent
imports, or unqualified imported names in v1. Every foreign declaration use is
visibly qualified:

```seawitch
point: geometry.Point = geometry.origin()
```

An import qualifier occupies the module namespace and cannot collide with a
local value, type, function, method owner, or another import qualifier.

## Visibility

`pub` may prefix module-level type, function, and method declarations. It is
rejected on locals, statements, object members, and implementation details that
have no importable declaration identity.

Private declarations are visible throughout their own module subject to the
existing source-order rules. Public declarations additionally become visible
through an import qualifier. Import does not bypass source visibility inside
the defining module and does not make private dependencies public.

V1 has no `pub import` or re-export. A public API names a foreign public type by
its defining module identity, and downstream compilation must retain that
dependency explicitly.

## Name resolution

The parser treats `qualifier.identifier` as ordinary dotted syntax. Resolution
first determines whether the leftmost identifier names an imported module in
the current scope. Module qualification is permitted only from the module-level
import namespace and cannot be shadowed by a local declaration.

Qualified type and value namespaces remain distinct after the module is
resolved:

```seawitch
value: math.Number = math.make_number()
```

The canonical identity of a public nominal type includes its defining module
path and declaration identity. Two modules declaring the same source name still
define different nominal types.

## Dependency graph

The compiler resolves the complete import graph before checking module bodies.
An import cycle is a compile-time error in v1:

```text
app.a -> app.b -> app.a
```

Rejecting cycles keeps initialization, declaration order, diagnostics, and
incremental invalidation deterministic. A later interface-only cycle mechanism
would require a separate proposal.

Imports are idempotent. Importing the same canonical module twice under one or
two aliases is rejected rather than creating duplicate identities.

## Top-level execution

Only one build-selected root module may contain executable top-level
statements. Bindings created there are locals of the generated entry body, not
module globals. Imported modules contain declarations only. Under RFC 0041,
they have no native value bindings, runtime globals, hidden initialization
function, or import-time side effects.

The root module's existing top-level statements remain the program entry body.
A future explicit `main` requirement may supersede this rule when the build and
FFI models are settled.

## Generics and specialization

Public generic declarations are checked openly in their defining module.
Concrete specializations are requested by reachable uses in importing modules
and rechecked under RFC 0019.

The specialization key includes the defining module identity. Generated C may
emit a needed specialization in one deterministic owning compilation unit or a
shared generated unit; the final choice must prevent duplicate definitions and
remain stable under incremental builds.

Diagnostics for an invalid specialization identify both the defining generic
declaration and the importing use that requested the concrete specialization.

## Incremental compilation contract

Each checked module exposes a deterministic public-interface fingerprint based
on:

- public declaration names and kinds;
- canonical public signatures and type layouts;
- relevant generic bodies and dependent operations;
- imported public identities used by that interface; and
- language/compiler version information affecting semantics.

Changing a private implementation recompiles that module but does not
invalidate dependents when its public-interface fingerprint is unchanged.
Changing a public interface invalidates direct dependents and propagates only
where their own checked interface or generated code changes.

Cache keys must never depend on process addresses, file discovery order, or
unstable generated C text.

## Filesystem and build resolution

The build tool supplies one or more module roots mapping module paths to source
files. The language does not infer a module's canonical identity from an
absolute host path. A proposed default mapping is:

```text
app.math -> <module-root>/app/math.sw
```

The exact source extension, root configuration file, package download model,
and standard-library prefix remain build-system decisions. Resolution must
reject ambiguous matches rather than choosing the first filesystem result.

## C23 lowering direction

- Every generated public and private C name includes a deterministic encoding
  of the defining module identity.
- Native module boundaries do not imply a C ABI; the compiler may use private
  generated declarations between its own C translation units.
- Dependency-safe declarations are emitted before definitions.
- Generated headers, if used internally, are compiler artifacts rather than a
  source-level FFI promise.
- `#line` directives preserve the original module file and source line.
- Unsupported cycles, missing modules, duplicate identities, or unresolved
  public types fail before C generation.

## Deferred and open design work

- Multi-file modules and directory packages.
- Package manifests, versions, registries, and dependency downloads.
- C imports, C exports, linkage, and ABI annotations.
- Re-exports, selective imports, and prelude modules.
- Interface-only import cycles.
- Conditional compilation and target-specific source selection.
- Resource embedding.
- Tests and benchmarks as module members.
- An explicit `main` function contract.

## Draft readiness questions

Before implementation readiness, this RFC must settle:

1. whether one-file modules are sufficient or directory packages should be the
   first model;
2. whether `pub` is the preferred visibility spelling;
3. the stable ownership location for cross-module generic specializations;
4. the exact module-root configuration and source extension; and
5. whether every source file must declare its module path or may derive it from
   the build mapping.
