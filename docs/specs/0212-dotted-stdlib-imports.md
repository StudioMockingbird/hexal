# RFC 0212: Dotted Standard-Library Imports

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-16
- Updated: 2026-09-16
- Scope: make compiler-owned and embedded standard-library imports visibly
  distinct from relative source-file imports
- Depends on: the current import grammar and module-resolution rules
- Does not add: direct item imports, wildcard imports, absolute filesystem
  paths, package discovery, arbitrary module collections, or a second
  module-alias namespace

## Summary

Standard-library modules use an unquoted dotted reference:

```hexal
import
    Prog from std.program,
    Io from std.io
end
```

Relative source modules retain quoted paths:

```hexal
import
    Math from "./math",
    Parser from "../parser"
end
```

The alias remains file-local. Imported declarations remain qualified:

```hexal
import
    Io from std.io
end

out: Io.IO := try Io.stdout()
```

This is a clean replacement. Quoted `"std/..."` collection paths are rejected
with a migration diagnostic; the compiler does not retain two source spellings
for one standard-library identity.

## Motivation

The current resolver gives quoted non-relative `"std/..."` paths virtual
standard-library meaning. The behavior is deterministic, but the spelling
looks like a source-file path even though it never consults the supplied
source map.

The new spelling makes the distinction lexical:

```text
std.program       -> compiler-owned or compiler-embedded standard library
"./program"       -> supplied source-map module
```

The `std` canonical prefix is reserved. A user source-map module cannot claim
`std/program`, including through the relative spelling `"./std/program"`.
This RFC therefore separates two existing, disjoint identity spaces; it does
not resolve a source-module/stdlib collision.

## Syntax

Replace the import production with:

```ebnf
import-entry = identifier , "from" , import-module-reference ;
import-module-reference = relative-module-path-literal
                        | stdlib-module-reference ;
relative-module-path-literal = ? a quoted module path whose payload starts
                                 with "./" or one or more "../" ? ;
stdlib-module-reference = "std" , "." , identifier
                         , { "." , identifier } ;
```

`std` is contextual. It is recognized as the leading component of a
standard-library reference only after `from` in an import entry. It remains a
legal declaration name, member name, and import alias elsewhere.

The module reference must begin on the same source line as `from`, preserving
the current module-path rule:

```hexal
import
    Io from std.io       -- accepted
end
```

```hexal
import
    Io from
        std.io           -- rejected
end
```

Every dotted component after `std.` must be an ordinary Identifier token.
Reserved words are not module components, so `std.for` is invalid. Multiple
components are admitted because the current canonical collection model already
supports nested paths and the parser and resolver need no additional semantic
case for them:

```hexal
import
    Hash from std.crypto.hash
end
```

That form is syntactically valid. Resolution succeeds only when
`std/crypto/hash` is an actual compiler-owned or embedded module; this RFC does
not add such a module.

## AST contract

An import entry stores a tagged module reference rather than overloading one
quoted-path token:

```text
ImportReference
    Kind             Relative | StandardLibrary
    Token            first token used for diagnostics
    DisplaySpelling  normalized source-form spelling
    Components       dotted stdlib components; empty for Relative
    RelativePath     raw quoted token; empty for StandardLibrary
```

- The parser classifies syntax and preserves a normalized dotted display
  spelling such as `std.crypto.hash`; discarded whitespace and comments are
  not reconstructed.
- The parser does not construct a canonical module identity.
- `Token` is the opening quote for a relative or legacy quoted path and the
  `std` token for a dotted reference.
- The resolver is the sole owner of canonicalization.
- A resolved graph edge continues to contain only alias plus canonical target;
  downstream checker and generator APIs do not gain a second identity form.

Equivalent field names or a small tagged Go representation are allowed; these
facts are the contract.

## Resolution

The resolver handles the tagged forms separately:

- `Relative` reuses the existing lexical `./` and `../` path arithmetic over
  the supplied source map.
- `StandardLibrary` joins the components after the fixed `std` root with `/`.

```text
std.program      -> std/program
std.crypto.hash  -> std/crypto/hash
```

The resulting slash-separated identity remains authoritative for module graph
membership, duplicate detection, nominal identity, generated names, source
ownership, demand selection, and artifact keys. Existing compiler-owned and
embedded-source lookup runs on that identity unchanged.

The reference kind, not inspection of a raw string prefix, decides whether the
resolver consults the standard-library tables. A relative import resolving to
a `std/...` canonical identity remains invalid because source-map logical keys
may not use the reserved `std` prefix.

## Diagnostics

Syntax and lookup diagnostics name the spelling the user wrote. Diagnostics
whose subject is explicitly a canonical identity retain slash spelling.

Required diagnostics:

| Source | Category and message | Anchor |
| --- | --- | --- |
| `Io from "std/io"` | Syntax Error: `standard-library imports use dotted paths; write std.io` | opening quote |
| `X from "vendor/x"` | Syntax Error: `quoted import paths must begin with ./ or ../` | opening quote |
| `X from std` | Syntax Error: `standard-library import requires a component after std.` | `std` |
| `X from std/io` | Syntax Error: `standard-library imports use dots between components` | `/` |
| `X from std..io` | Syntax Error: `expected a standard-library module component after '.'` | second `.` |
| `X from std.for` | Syntax Error: `expected a standard-library module component after '.'` | `for` |
| `Io from` followed by `std.io` on the next line | Syntax Error: `module reference must begin on the same line as 'from'` | `std` |
| `X from std.missing` | Module Error: `unknown stdlib module std.missing` | `std` |

Duplicate-import diagnostics continue to name the canonical identity, for
example `duplicate import of canonical module std/io`. Diagnostics produced by
an embedded source module retain its established logical key, such as
`stdlib/std/ascii.hex`.

## Compatibility and migration

The change is intentionally source-breaking:

```hexal
-- before
Io from "std/io"

-- after
Io from std.io
```

The compiler accepts no compatibility alias and emits no deprecation warning.
A clean break preserves one obvious spelling, keeps the grammar small, and
prevents an indefinite second path through parsing and resolution.

The migration changes token counts because one path token becomes identifier
and dot tokens. That expected `CompilationResult.Stats.TokenCount` change is
not a semantic or generated-artifact change.

## Required sweep

- import grammar, parser AST, and parser tests;
- module-graph resolution and tests, including removal of collection detection
  by inspecting the quoted raw path;
- the old quoted-collection branch in `resolveImportPath` and
  `resolveCollectionPath` after their remaining relative logic is retained in
  one authoritative path;
- every compiler migration hint that recommends `Alias from "std/..."`;
- active checker, generator, integration, C23-validation, benchmark, and
  module-graph fixtures containing quoted stdlib imports;
- workbench snippet sources and their source catalog;
- current CARE comments that describe quoted stdlib import syntax;
- the normative grammar and module rules in `docs/reference.md` after behavior
  stabilizes and with explicit user approval.

Closed specifications are immutable and are not migrated.

## Detailed implementation plan

### Phase 1: lock the contract

1. Add parser tests for every accepted and rejected syntax form in Validation.
2. Add module-graph tests proving dotted references produce the existing
   slash-separated canonical targets.
3. Capture the snippet manifest and representative generated artifacts before
   source migration; dotted spelling must not change generated output.

### Phase 2: represent import references

1. Add the tagged import-reference AST representation.
2. Parse quoted relative paths into `Relative` references.
3. Parse `std.<component>{.<component>}` into `StandardLibrary` references,
   enforcing same-line start and ordinary-Identifier components.
4. Implement the exact parser diagnostics above, including the quoted legacy
   migration diagnostic.
5. Keep `std` contextual outside import-reference position.

### Phase 3: resolve one canonical identity

1. Route `Relative` references through the existing lexical relative-path
   algorithm.
2. Route `StandardLibrary` references through one dot-to-slash conversion and
   the existing core-library/embedded-source lookup.
3. Remove raw-string prefix inference from graph resolution.
4. Preserve canonical duplicate detection, cycle handling, graph order, source
   ownership, and module provenance.
5. Remove collection-path code that has no remaining caller.

### Phase 4: migrate active sources

1. Rewrite active Hexal sources and test fixtures from quoted stdlib paths to
   dotted references.
2. Rewrite compiler-produced migration hints to recommend dotted syntax.
3. Update current CARE comments without changing runtime or checker behavior.
4. Migrate workbench snippet sources. Do not edit manifest hashes merely
   because source spelling changed.

### Phase 5: conformance

1. Implement every Validation item.
2. Compare generated artifacts and the snippet manifest with the Phase 1
   baseline; no hash may move.
3. Run ordinary and tagged C23 suites.
4. Synchronize `docs/reference.md` only after behavior stabilizes and with
   explicit user approval.

## Validation

This list is exhaustive:

- `std.io` and every currently defined one-component stdlib module parse and
  resolve to their existing `std/<name>` canonical identities;
- a resolver unit test proves `std.crypto.hash` canonicalizes exactly to
  `std/crypto/hash`; compiling that import while no such embedded module exists
  reports the exact unknown-module diagnostic above;
- quoted `"std/io"`, quoted non-relative `"vendor/x"`, bare `std`, `std/io`,
  `std..io`, `std.for`, and a module reference beginning on the line after
  `from` report the exact diagnostics and anchors above;
- `"./math"`, `"./math.hex"`, and `"../math"` retain their existing lexical
  source-map resolution, validation, and above-root diagnostics;
- attempting to reach a supplied `std/program.hex` through
  `"./std/program"` retains the reserved-prefix Module Error;
- core-library `std.io` and embedded-source `std.ascii` both resolve through
  the same dotted syntax while retaining their distinct existing artifact
  behavior;
- importing one standard-library canonical identity twice reports the existing
  duplicate-canonical-module diagnostic;
- `std` remains legal as an ordinary declaration name, member name, and import
  alias outside the import-reference position;
- every compiler-produced standard-library migration hint uses dotted syntax;
- valid migrated programs produce byte-identical `Files` and identical
  `Dependencies`; the snippet manifest is byte-identical;
- no generated-C component, runtime template, C symbol, nominal identity, or
  artifact key changes; and
- ordinary and tagged C23 suites pass.

## Non-goals

- Importing individual functions or types directly into the file namespace.
- Wildcard or re-export imports.
- Changing the alias binding or `Alias.member` access model.
- Treating arbitrary dotted names as filesystem paths or package names.
- Adding any collection other than `std`.
- Reserving `std` outside import-reference position.
- Changing the standard-library API, embedded sources, or runtime components.

## Open questions

None.

## Reference synchronization

Do not edit `docs/reference.md` from this RFC draft. After implementation
stabilizes and with explicit user approval, update the EBNF first, then replace
the quoted collection-path semantics and examples with the dotted stdlib rules
above. Relative source-path, alias, visibility, canonical-identity, graph, and
artifact contracts remain unchanged.
